package storage

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Dr0nj/regente-server/internal/domain"
)

// O poll periódico não pode custar proporcional ao tamanho do workspace quando
// nada mudou (regressão de produção, 2026-11-13): ele só relata mudança — e só
// audita o worktree arquivo por arquivo — quando há o que fazer.
func TestGitPollReportsChangeOnlyWhenWorktreeMoves(t *testing.T) {
	bare, seed := initRemote(t)
	seed.commit("definitions/f/job.yaml", "id: job\nteam: f\nlabel: original\n", "definition")
	seed.push()
	ws := t.TempDir()
	g := NewGitOps(ws, bare, testBranch)
	if err := g.EnsureClone(); err != nil {
		t.Fatal(err)
	}
	// Primeira rodada: nunca auditou, então confere o worktree (sem mudança).
	changed, err := g.SyncFromRemotePoll()
	if err != nil || changed {
		t.Fatalf("poll inicial sem commit novo: changed=%v err=%v", changed, err)
	}
	audited := g.lastWorktreeAudit
	if audited.IsZero() {
		t.Fatal("a primeira rodada do poll precisa auditar o worktree")
	}
	// Rodadas seguintes dentro do intervalo: sem auditoria e sem mudança.
	for range 3 {
		if changed, err = g.SyncFromRemotePoll(); err != nil || changed {
			t.Fatalf("poll ocioso: changed=%v err=%v", changed, err)
		}
	}
	if !g.lastWorktreeAudit.Equal(audited) {
		t.Fatal("poll ocioso voltou a auditar o worktree inteiro")
	}
	// Commit remoto novo: muda, e o disco reflete.
	seed.commit("definitions/f/job.yaml", "id: job\nteam: f\nlabel: remote\n", "advance")
	seed.push()
	if changed, err = g.SyncFromRemotePoll(); err != nil || !changed {
		t.Fatalf("poll com commit novo: changed=%v err=%v", changed, err)
	}
	// Adulteração local de arquivo versionado: o poll barato não vê (é o preço),
	// a sincronização explícita vê, e o poll volta a ver quando o intervalo vence.
	path := filepath.Join(ws, "definitions", "f", "job.yaml")
	mustWrite(t, path, "id: job\nteam: f\nlabel: tampered\n")
	if changed, err = g.SyncFromRemotePoll(); err != nil || changed {
		t.Fatalf("poll dentro do intervalo não audita: changed=%v err=%v", changed, err)
	}
	g.lastWorktreeAudit = time.Now().Add(-2 * worktreeAuditInterval)
	if changed, err = g.SyncFromRemotePoll(); err != nil || !changed {
		t.Fatalf("auditoria vencida precisa restaurar o arquivo: changed=%v err=%v", changed, err)
	}
	mustWrite(t, path, "id: job\nteam: f\nlabel: tampered-again\n")
	if err = g.SyncFromRemote(); err != nil {
		t.Fatal(err)
	}
	store := NewFileStore(ws, false)
	defs, err := store.List()
	if err != nil || len(defs) != 1 || defs[0].Label != "remote" {
		t.Fatalf("sincronização explícita não restaurou o arquivo: %v %v", defs, err)
	}
}

// A reconciliação periódica só relê o conteúdo quando o carimbo de stat muda
// (ou na auditoria de conteúdo a cada N rodadas).
func TestDefinitionReconcileSkipsUnchangedWorkspace(t *testing.T) {
	s := NewFileStore(t.TempDir(), false)
	if err := s.Save(domain.JobDefinition{ID: "one", Team: "folder", Label: "a"}); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := s.StartDefinitionCache(ctx, time.Hour); err != nil {
		t.Fatal(err)
	}
	s.reconcileDefinitions(ctx) // grava o carimbo
	first, err := s.readyDefinitionSnapshot()
	if err != nil {
		t.Fatal(err)
	}
	for range 3 {
		s.reconcileDefinitions(ctx)
	}
	same, err := s.readyDefinitionSnapshot()
	if err != nil || same != first {
		t.Fatal("reconciliação sem mudança trocou o snapshot", err)
	}
	// Edição por fora do server (tamanho diferente): o carimbo muda e o snapshot avança.
	mustWrite(t, filepath.Join(s.definitionsDir(), "folder", "one.yaml"), "id: one\nteam: folder\nlabel: edited-outside\n")
	s.reconcileDefinitions(ctx)
	next, err := s.readyDefinitionSnapshot()
	if err != nil || next == first || next.definitions[0].Label != "edited-outside" {
		t.Fatal("edição externa não foi reconciliada", err)
	}
	// Edição que preserva tamanho e mtime escapa do carimbo; a auditoria de
	// conteúdo periódica pega.
	path := filepath.Join(s.definitionsDir(), "folder", "one.yaml")
	info := mustStat(t, path)
	mustWrite(t, path, "id: one\nteam: folder\nlabel: EDITED-outside\n")
	if err = chtimes(path, info); err != nil {
		t.Fatal(err)
	}
	for range definitionContentAuditEvery {
		s.reconcileDefinitions(ctx)
	}
	audited, err := s.readyDefinitionSnapshot()
	if err != nil || audited.definitions[0].Label != "EDITED-outside" {
		t.Fatal("auditoria de conteúdo não pegou edição com mesmo tamanho e mtime", err)
	}
}

func mustStat(t *testing.T, path string) fileStamp {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	return fileStamp{info.ModTime()}
}

type fileStamp struct{ mod time.Time }

func chtimes(path string, s fileStamp) error { return os.Chtimes(path, s.mod, s.mod) }
