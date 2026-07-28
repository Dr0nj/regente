package storage

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	git "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
)

// Bootstrap de instalação NOVA: o operador cria um repositório VAZIO no GitHub e
// aponta o server para ele. Não há o que clonar — quem escreve o conteúdo inicial
// somos nós. Antes isto era erro fatal no boot (crash-loop com Restart=always).
//
// Mesma bancada dos outros testes de GitOps: um bare local faz de remote, sem rede
// e sem o binário `git`.

// initEmptyRemote devolve um bare SEM nenhum commit (o "New repository" do GitHub).
func initEmptyRemote(t *testing.T) string {
	t.Helper()
	bare := t.TempDir()
	if _, err := git.PlainInit(bare, true); err != nil {
		t.Fatalf("init bare: %v", err)
	}
	return bare
}

func writeWorkspaceFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// remoteHasFile confirma que o arquivo chegou ao REMOTE (não só ao disco local):
// resolve refs/heads/<branch> no bare e procura o caminho na árvore do commit.
func remoteHasFile(t *testing.T, bare, branch, path string) bool {
	t.Helper()
	repo, err := git.PlainOpen(bare)
	if err != nil {
		t.Fatalf("open bare: %v", err)
	}
	ref, err := repo.Reference(plumbing.NewBranchReferenceName(branch), true)
	if err != nil {
		t.Fatalf("resolve %s on the remote: %v", branch, err)
	}
	c, err := repo.CommitObject(ref.Hash())
	if err != nil {
		t.Fatalf("commit object: %v", err)
	}
	if _, err := c.File(path); err != nil {
		return false
	}
	return true
}

func TestEnsureClone_EmptyRemote_Bootstraps(t *testing.T) {
	bare := initEmptyRemote(t)
	ws := t.TempDir()

	g := NewGitOps(ws, bare, "main")
	if err := g.EnsureClone(); err != nil {
		t.Fatalf("EnsureClone against an empty remote: %v", err)
	}

	// Disco: o scaffold mínimo que torna o workspace utilizável.
	for _, rel := range []string{"definitions/.gitkeep", ".gitignore", "README.md"} {
		if !exists(filepath.Join(ws, filepath.FromSlash(rel))) {
			t.Errorf("scaffold file missing on disk: %s", rel)
		}
	}
	// Remote: o commit inicial foi publicado e criou a branch pedida.
	if !remoteHasFile(t, bare, "main", "definitions/.gitkeep") {
		t.Error("the initial commit did not reach the remote on branch main")
	}
	// A DB do server nunca pode ser versionada — o exclude local entra ANTES do add.
	if !strings.Contains(readFile(t, filepath.Join(ws, ".git", "info", "exclude")), "*.db") {
		t.Error(".git/info/exclude does not carry the SQLite patterns")
	}

	st := g.Status()
	if !st.Configured || st.SHA == "" {
		t.Errorf("status after bootstrap = %+v; want configured with a SHA", st)
	}
	if st.Error != "" {
		t.Errorf("status.Error = %q; want empty after a successful bootstrap", st.Error)
	}

	// Idempotência: o segundo boot é fetch+reset normal, sem duplicar nada.
	if err := g.EnsureClone(); err != nil {
		t.Fatalf("EnsureClone (second boot): %v", err)
	}
	if sha := g.Status().SHA; sha != st.SHA {
		t.Errorf("SHA changed on the second boot: %s -> %s", st.SHA, sha)
	}
}

func TestEnsureClone_EmptyRemote_AdoptsExistingWorkspace(t *testing.T) {
	// Rodou offline primeiro (jobs no disco, sem .git) e só depois configurou o
	// GitOps apontando para um repo vazio: o disco vira o commit inicial.
	bare := initEmptyRemote(t)
	ws := t.TempDir()
	writeWorkspaceFile(t, filepath.Join(ws, "definitions", "job-a.yaml"), "name: job-a\n")

	g := NewGitOps(ws, bare, "main")
	if err := g.EnsureClone(); err != nil {
		t.Fatalf("EnsureClone adopting the workspace: %v", err)
	}
	if !exists(filepath.Join(ws, "definitions", "job-a.yaml")) {
		t.Fatal("the pre-existing definition was destroyed")
	}
	if !remoteHasFile(t, bare, "main", "definitions/job-a.yaml") {
		t.Error("the pre-existing definition was not published to the remote")
	}
}

func TestEnsureClone_RecoversAfterAFailedFirstBoot(t *testing.T) {
	// Regressão encontrada rodando o server de verdade: no boot com o clone
	// falhando, o próprio server cria <workspace>/definitions. Com a checagem
	// antiga de "vazio" (qualquer entrada conta), a pasta deixada para trás fazia
	// TODAS as tentativas seguintes caírem no guard de "not empty" — o retry em
	// background nunca se recuperava e só um rm -rf manual resolvia.
	bare, _ := initRemote(t)
	ws := t.TempDir()
	if err := os.MkdirAll(filepath.Join(ws, "definitions"), 0o755); err != nil {
		t.Fatal(err)
	}

	g := NewGitOps(ws, bare, testBranch)
	if err := g.EnsureClone(); err != nil {
		t.Fatalf("EnsureClone with an empty definitions/ left over: %v", err)
	}
	if !exists(filepath.Join(ws, "README.md")) {
		t.Error("the remote content was not checked out")
	}
	if g.Status().Error != "" {
		t.Errorf("status.Error = %q; want empty after recovering", g.Status().Error)
	}
}

func TestEnsureClone_NonEmptyRemote_StillRefusesToClobber(t *testing.T) {
	// A proteção contra perda de dados continua valendo quando o remote TEM
	// conteúdo: aí o clone sobrescreveria o disco, e isso segue proibido.
	bare, _ := initRemote(t)
	ws := t.TempDir()
	writeWorkspaceFile(t, filepath.Join(ws, "definitions", "job-a.yaml"), "name: job-a\n")

	g := NewGitOps(ws, bare, testBranch)
	err := g.EnsureClone()
	if err == nil {
		t.Fatal("EnsureClone should refuse a non-empty workspace against a populated remote")
	}
	if !strings.Contains(err.Error(), "refuse to clobber") {
		t.Errorf("unexpected error: %v", err)
	}
	if g.Status().Error == "" {
		t.Error("status.Error should carry the reason the workspace is not synced")
	}
}

func TestEnsureClone_MissingBranch_FallsBackToRemoteDefault(t *testing.T) {
	// O repo do operador usa `master` (ou o nome foi digitado errado) e a config
	// pede `main`: seguimos a branch default do repositório em vez de derrubar o
	// boot com "reference not found".
	bare, _ := initRemote(t) // o seed publica em `master`
	ws := t.TempDir()

	g := NewGitOps(ws, bare, "main")
	if err := g.EnsureClone(); err != nil {
		t.Fatalf("EnsureClone with a missing branch: %v", err)
	}
	if g.Branch() != testBranch {
		t.Errorf("branch = %q; want the remote default %q", g.Branch(), testBranch)
	}
	if g.Status().SHA == "" {
		t.Error("no SHA after falling back to the default branch")
	}
	if !exists(filepath.Join(ws, "README.md")) {
		t.Error("the remote content was not checked out")
	}
}

func TestEnsureClone_BadSource_RecordsErrorInsteadOfDying(t *testing.T) {
	// Repo inexistente (o typo clássico em REGENTE_GIT_SOURCE): EnsureClone
	// falha, mas o motivo fica no status — é ele que a UI mostra enquanto o
	// server segue no ar tentando de novo em background.
	g := NewGitOps(t.TempDir(), filepath.Join(t.TempDir(), "does-not-exist"), "main")
	if err := g.EnsureClone(); err == nil {
		t.Fatal("EnsureClone should fail against a non-existent source")
	}
	st := g.Status()
	if !st.Configured {
		t.Error("status should stay Configured — the source IS set, it just does not work")
	}
	if st.Error == "" {
		t.Error("status.Error is empty; the UI would have nothing to show")
	}
}
