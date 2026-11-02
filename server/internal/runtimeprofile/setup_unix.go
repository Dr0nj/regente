//go:build !windows

package runtimeprofile

import (
	"errors"
	"os"
)

func ProtectConfig(path string) error { return os.Chmod(path, 0600) }
func CheckPrivateConfig(_ string, info os.FileInfo) error {
	if info.Mode().Perm()&0077 != 0 {
		return errors.New("configuration must have owner-only permissions (0600)")
	}
	return nil
}
