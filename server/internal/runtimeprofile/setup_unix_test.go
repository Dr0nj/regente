//go:build !windows

package runtimeprofile

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPublicConfigurationPermissionsRejected(t *testing.T) {
	path := filepath.Join(t.TempDir(), "private.env")
	if e := os.WriteFile(path, []byte("REGENTE_SETUP_PUBLIC=never-load"), 0600); e != nil {
		t.Fatal(e)
	}
	for _, mode := range []os.FileMode{0644, 0640, 0604} {
		if e := os.Chmod(path, mode); e != nil {
			t.Fatal(e)
		}
		if LoadEnvironment(path) == nil {
			t.Fatal("group/other readable configuration accepted", mode)
		}
	}
	if e := ProtectConfig(path); e != nil {
		t.Fatal(e)
	}
	info, _ := os.Stat(path)
	if e := CheckPrivateConfig(path, info); e != nil {
		t.Fatal(e)
	}
}
