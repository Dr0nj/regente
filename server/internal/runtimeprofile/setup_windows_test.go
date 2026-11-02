//go:build windows

package runtimeprofile

import (
	"golang.org/x/sys/windows"
	"os"
	"path/filepath"
	"testing"
)

func TestPublicConfigurationACLRejected(t *testing.T) {
	path := filepath.Join(t.TempDir(), "private.env")
	if e := os.WriteFile(path, []byte("REGENTE_SETUP_PUBLIC=never-load"), 0600); e != nil {
		t.Fatal(e)
	}
	sd, e := windows.SecurityDescriptorFromString("D:P(A;;FA;;;WD)")
	if e != nil {
		t.Fatal(e)
	}
	dacl, _, e := sd.DACL()
	if e != nil {
		t.Fatal(e)
	}
	if e = windows.SetNamedSecurityInfo(path, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION, nil, nil, dacl, nil); e != nil {
		t.Fatal(e)
	}
	if LoadEnvironment(path) == nil {
		t.Fatal("Everyone-readable configuration accepted")
	}
	if e = ProtectConfig(path); e != nil {
		t.Fatal(e)
	}
	info, _ := os.Stat(path)
	if e = CheckPrivateConfig(path, info); e != nil {
		t.Fatal(e)
	}
}
