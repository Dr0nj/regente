package storage

import (
	"os"
	"path/filepath"
	"testing"
)

// UI-3 — override de grade por folder persistido no stub .regente-folder.yaml.
// Round-trip completo: set → List devolve; set(nil) limpa; stub antigo (só o
// marker de CreateFolder) segue compatível (Layout nil, name preservado).
func TestFolderLayout_RoundTrip(t *testing.T) {
	store := NewFileStore(t.TempDir(), false)
	if err := store.CreateFolder("PIX"); err != nil {
		t.Fatalf("CreateFolder: %v", err)
	}

	// Stub recém-criado (marker antigo) → sem layout.
	fs, err := store.ListFolders()
	if err != nil {
		t.Fatalf("ListFolders: %v", err)
	}
	if len(fs) != 1 || fs[0].Layout != nil {
		t.Fatalf("esperava 1 folder sem layout, veio %+v", fs)
	}

	// Set → List devolve o override.
	if err := store.SetFolderLayout("PIX", &FolderLayout{Columns: 6, MaxRows: 20}); err != nil {
		t.Fatalf("SetFolderLayout: %v", err)
	}
	fs, _ = store.ListFolders()
	if fs[0].Layout == nil || fs[0].Layout.Columns != 6 || fs[0].Layout.MaxRows != 20 {
		t.Fatalf("layout não persistiu: %+v", fs[0].Layout)
	}

	// O stub preserva o name (marker continua rastreável pelo git).
	meta := readFolderMeta(filepath.Join(store.Root(), "definitions", "PIX"))
	if meta.Name != "PIX" {
		t.Fatalf("stub perdeu o name: %+v", meta)
	}

	// Parcial: só columns (maxRows herda o global → 0/omitido no YAML).
	if err := store.SetFolderLayout("PIX", &FolderLayout{Columns: 4}); err != nil {
		t.Fatalf("SetFolderLayout parcial: %v", err)
	}
	fs, _ = store.ListFolders()
	if fs[0].Layout == nil || fs[0].Layout.Columns != 4 || fs[0].Layout.MaxRows != 0 {
		t.Fatalf("layout parcial não persistiu: %+v", fs[0].Layout)
	}

	// nil limpa o override (folder volta a herdar o global).
	if err := store.SetFolderLayout("PIX", nil); err != nil {
		t.Fatalf("SetFolderLayout(nil): %v", err)
	}
	fs, _ = store.ListFolders()
	if fs[0].Layout != nil {
		t.Fatalf("layout devia ter sido removido: %+v", fs[0].Layout)
	}

	// Folder inexistente → erro (não cria dir fantasma).
	if err := store.SetFolderLayout("NOPE", &FolderLayout{Columns: 2}); err == nil {
		t.Fatal("esperava erro para folder inexistente")
	}

	// O stub não conta como job e o layout não interfere na contagem.
	if err := os.WriteFile(filepath.Join(store.Root(), "definitions", "PIX", "j1.yaml"), []byte("id: j1\nteam: PIX\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	fs, _ = store.ListFolders()
	if fs[0].JobCount != 1 {
		t.Fatalf("jobCount esperado 1, veio %d", fs[0].JobCount)
	}
}
