package storage

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestDesignSessionDirtyProtection — Dirty() reflete o working tree do clone;
// o sweep do Create e o GC por TTL só removem sessions LIMPAS. Session com
// trabalho não publicado é imune a qualquer remoção automática (2026-07-02).
func TestDesignSessionDirtyProtection(t *testing.T) {
	bare, _ := initRemote(t)
	root := t.TempDir()
	m := NewSessionManager(root, bare, testBranch, "", nil, nil)

	dirty, err := m.Create("alice", []string{"f1"}, nil)
	if err != nil {
		t.Fatalf("create dirty: %v", err)
	}
	if dirty.Dirty() {
		t.Fatal("session recém-clonada deveria estar limpa")
	}
	// Edição não publicada → working tree suja.
	p := filepath.Join(dirty.Path, "definitions", "f1", "job.json")
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(`{"id":"job"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if !dirty.Dirty() {
		t.Fatal("session com arquivo novo deveria estar suja")
	}

	clean, err := m.Create("alice", nil, nil)
	if err != nil {
		t.Fatalf("create clean: %v", err)
	}

	// Ambas "esquecidas" há 1h (F5 sem retomar, aba fechada).
	old := time.Now().Add(-time.Hour)
	dirty.LastTouch = old
	clean.LastTouch = old

	// Sweep do Create: remove só a limpa; a suja é trabalho protegido.
	m.sweepCleanIdle("alice", 10*time.Minute)
	if _, ok := m.Get(dirty.ID); !ok {
		t.Fatal("sweep removeu session suja (trabalho não publicado perdido)")
	}
	if _, ok := m.Get(clean.ID); ok {
		t.Fatal("sweep não removeu session limpa idle")
	}
	if exists(clean.Path) {
		t.Fatal("diretório da session limpa não foi apagado do disco")
	}

	// GC por TTL: session suja expirada também é imune (Get acima tocou o
	// LastTouch, re-envelhece antes de varrer).
	dirty.LastTouch = time.Now().Add(-time.Hour)
	m.ttl = 10 * time.Minute
	m.gcSweep()
	if _, ok := m.Get(dirty.ID); !ok {
		t.Fatal("GC removeu session suja (trabalho não publicado perdido)")
	}
}
