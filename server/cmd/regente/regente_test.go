package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Dr0nj/regente-server/internal/domain"
	"github.com/Dr0nj/regente-server/internal/scheduler"
	"github.com/go-git/go-git/v5"
	gitcfg "github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
)

// ─── regente test (D-7) ─────────────────────────────────────────────────────

func TestFindCycles(t *testing.T) {
	defs := []domain.JobDefinition{
		{ID: "a", Upstream: []domain.Upstream{{From: "c"}}},
		{ID: "b", Upstream: []domain.Upstream{{From: "a"}}},
		{ID: "c", Upstream: []domain.Upstream{{From: "b"}}}, // a→c→b→a
		{ID: "solto"},
	}
	cycles := findCycles(defs)
	if len(cycles) != 1 {
		t.Fatalf("esperava 1 ciclo, veio %d: %v", len(cycles), cycles)
	}
	if len(defs[3].Upstream) != 0 && len(cycles) > 1 {
		t.Fatal("job solto não participa de ciclo")
	}
}

// REGRA: o parse do `test` é ESTRITO — typo de campo é erro, não silêncio.
func TestParseStrictDocs_CatchesTypo(t *testing.T) {
	rep := &testReport{}
	defs := parseStrictDocs("id: a\nlabel: A\njobType: COMMAND\nretriez: 3\n", rep, "job.yaml")
	if len(rep.Errors) == 0 {
		t.Fatalf("typo 'retriez' deveria virar erro, veio defs=%v", defs)
	}
}

// Integração: um workspace com 2 jobs (b depende de a) simula RUN+WAIT com o
// engine REAL; um terceiro job com condition sem emissor sai BLOCKED.
func TestSimulate_UsesRealEngine(t *testing.T) {
	root := t.TempDir()
	write := func(team, id, body string) {
		dir := filepath.Join(root, "definitions", team)
		_ = os.MkdirAll(dir, 0o755)
		if err := os.WriteFile(filepath.Join(dir, id+".yaml"), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("fin", "a", "id: a\nlabel: A\nteam: fin\njobType: COMMAND\nschedule:\n  enabled: true\n")
	write("fin", "b", "id: b\nlabel: B\nteam: fin\njobType: COMMAND\nschedule:\n  enabled: true\nupstream:\n  - from: a\n    condition: on-success\n")
	write("fin", "c", "id: c\nlabel: C\nteam: fin\njobType: COMMAND\nschedule:\n  enabled: true\nconditionsIn: [NUNCA_SETADA]\n")

	sim, err := simulate(root, time.Now().Format("2006-01-02"))
	if err != nil {
		t.Fatal(err)
	}
	if sim.Counts.Run != 1 || sim.Counts.Wait != 1 || sim.Counts.Blocked != 1 {
		t.Fatalf("esperava 1 RUN, 1 WAIT, 1 BLOCKED; veio %+v", sim.Counts)
	}
	for _, j := range sim.Jobs {
		if j.DefID == "c" && j.Outcome != scheduler.DryRunBlocked {
			t.Fatalf("c deveria ser BLOCKED (condition sem emissor), veio %s", j.Outcome)
		}
	}
}

// ─── regente promote (D-9) ──────────────────────────────────────────────────

// monta um "remoto" BARE local (como o GitHub: aceita push em qualquer branch)
// com main e dev divergentes.
func seedPromoteRepo(t *testing.T) string {
	t.Helper()
	remote := t.TempDir()
	if _, err := git.PlainInit(remote, true); err != nil {
		t.Fatal(err)
	}
	work := t.TempDir()
	repo, err := git.PlainInit(work, false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repo.CreateRemote(&gitcfg.RemoteConfig{Name: "origin", URLs: []string{remote}}); err != nil {
		t.Fatal(err)
	}
	wt, _ := repo.Worktree()
	commit := func(msg string) plumbing.Hash {
		_ = wt.AddWithOptions(&git.AddOptions{All: true})
		h, err := wt.Commit(msg, &git.CommitOptions{
			Author: &object.Signature{Name: "t", Email: "t@t", When: time.Now()},
		})
		if err != nil {
			t.Fatal(err)
		}
		return h
	}
	writeF := func(rel, content string) {
		p := filepath.Join(work, filepath.FromSlash(rel))
		_ = os.MkdirAll(filepath.Dir(p), 0o755)
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	// main
	writeF("definitions/fin/a.yaml", "id: a\nlabel: A v1\nteam: fin\njobType: COMMAND\n")
	writeF("definitions/fin/velho.yaml", "id: velho\nlabel: Só existe em main\nteam: fin\njobType: COMMAND\n")
	base := commit("estado inicial")
	if err := repo.Storer.SetReference(plumbing.NewHashReference(plumbing.NewBranchReferenceName("main"), base)); err != nil {
		t.Fatal(err)
	}

	// dev: muda a.yaml, remove velho.yaml, adiciona novo.yaml
	if err := wt.Checkout(&git.CheckoutOptions{Branch: plumbing.NewBranchReferenceName("dev"), Create: true}); err != nil {
		t.Fatal(err)
	}
	writeF("definitions/fin/a.yaml", "id: a\nlabel: A v2 PROMOVIDA\nteam: fin\njobType: COMMAND\n")
	writeF("definitions/fin/novo.yaml", "id: novo\nlabel: Novo em dev\nteam: fin\njobType: COMMAND\n")
	_ = os.Remove(filepath.Join(work, "definitions", "fin", "velho.yaml"))
	commit("trabalho em dev")

	// publica os dois branches no bare
	if err := repo.Push(&git.PushOptions{RemoteName: "origin", RefSpecs: []gitcfg.RefSpec{
		"refs/heads/main:refs/heads/main", "refs/heads/dev:refs/heads/dev",
	}}); err != nil {
		t.Fatal(err)
	}
	return remote
}

// REGRA: a promoção faz o snapshot da ORIGEM virar o estado do destino nos
// paths — updates, adds E deletes; commit fica no branch destino.
func TestPromote_SnapshotSemantics(t *testing.T) {
	remote := seedPromoteRepo(t)

	if err := cmdPromote([]string{"-repo", remote, "-from", "dev", "-to", "main", "-message", "promo teste"}); err != nil {
		t.Fatal(err)
	}

	// clona main de novo e confere o estado promovido
	check := t.TempDir()
	repo, err := git.PlainClone(check, false, &git.CloneOptions{
		URL: remote, ReferenceName: plumbing.NewBranchReferenceName("main"), SingleBranch: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	head, _ := repo.Head()
	c, _ := repo.CommitObject(head.Hash())
	if !strings.Contains(c.Message, "promo teste") {
		t.Fatalf("HEAD de main deveria ser o commit de promoção, veio %q", c.Message)
	}
	mustContain := func(rel, want string) {
		b, err := os.ReadFile(filepath.Join(check, filepath.FromSlash(rel)))
		if err != nil {
			t.Fatalf("%s deveria existir pós-promoção: %v", rel, err)
		}
		if !strings.Contains(string(b), want) {
			t.Fatalf("%s deveria conter %q, veio %q", rel, want, b)
		}
	}
	mustContain("definitions/fin/a.yaml", "A v2 PROMOVIDA")
	mustContain("definitions/fin/novo.yaml", "Novo em dev")
	if _, err := os.Stat(filepath.Join(check, "definitions", "fin", "velho.yaml")); !os.IsNotExist(err) {
		t.Fatal("velho.yaml (ausente em dev) deveria ter sido REMOVIDO de main — promoção é snapshot, não merge aditivo")
	}
}

// REGRA: -dry-run não move nada; promover ambientes idênticos é no-op limpo.
func TestPromote_DryRunAndNoop(t *testing.T) {
	remote := seedPromoteRepo(t)

	if err := cmdPromote([]string{"-repo", remote, "-from", "dev", "-to", "main", "-dry-run"}); err != nil {
		t.Fatal(err)
	}
	repo, _ := git.PlainOpen(remote)
	ref, _ := repo.Reference(plumbing.NewBranchReferenceName("main"), true)
	c, _ := repo.CommitObject(ref.Hash())
	if strings.Contains(c.Message, "promote(") {
		t.Fatal("dry-run não podia commitar")
	}

	// promove de verdade e repete: segunda vez é no-op sem erro
	if err := cmdPromote([]string{"-repo", remote, "-from", "dev", "-to", "main"}); err != nil {
		t.Fatal(err)
	}
	if err := cmdPromote([]string{"-repo", remote, "-from", "dev", "-to", "main"}); err != nil {
		t.Fatalf("promoção idempotente não podia falhar: %v", err)
	}
}
