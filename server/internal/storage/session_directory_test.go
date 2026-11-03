package storage

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSessionDirectoryPreflight(t *testing.T) {
	root := filepath.Join(t.TempDir(), "data", "sessions")
	for range 2 {
		if err := CheckSessionDirectory(root); err != nil {
			t.Fatal(err)
		}
	}
	entries, err := os.ReadDir(root)
	if err != nil || len(entries) != 0 {
		t.Fatalf("probe left files: %v, %v", entries, err)
	}
	blocked := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(blocked, []byte("preserve"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := CheckSessionDirectory(filepath.Join(blocked, "sessions")); err == nil {
		t.Fatal("unusable storage accepted")
	}
}
