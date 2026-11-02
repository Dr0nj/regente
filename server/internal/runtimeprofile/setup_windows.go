//go:build windows

package runtimeprofile

import (
	"errors"
	"golang.org/x/sys/windows"
	"os"
	"os/user"
	"unsafe"
)

func ProtectConfig(path string) error {
	u, err := user.Current()
	if err != nil {
		return err
	}
	sd, err := windows.SecurityDescriptorFromString("D:P(A;;FA;;;" + u.Uid + ")(A;;FA;;;SY)")
	if err != nil {
		return err
	}
	dacl, _, err := sd.DACL()
	if err != nil {
		return err
	}
	owner, err := windows.StringToSid(u.Uid)
	if err != nil {
		return err
	}
	return windows.SetNamedSecurityInfo(path, windows.SE_FILE_OBJECT, windows.OWNER_SECURITY_INFORMATION|windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION, owner, nil, dacl, nil)
}
func CheckPrivateConfig(path string, _ os.FileInfo) error {
	sd, err := windows.GetNamedSecurityInfo(path, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION|windows.OWNER_SECURITY_INFORMATION)
	if err != nil {
		return err
	}
	u, err := user.Current()
	if err != nil {
		return err
	}
	owner, _, ownerErr := sd.Owner()
	if ownerErr != nil {
		return ownerErr
	}
	dacl, _, err := sd.DACL()
	if err != nil || dacl == nil {
		return errors.New("private configuration ACL required")
	}
	for i := uint32(0); i < uint32(dacl.AceCount); i++ {
		var ace *windows.ACCESS_ALLOWED_ACE
		if err = windows.GetAce(dacl, i, &ace); err != nil {
			return err
		}
		if ace.Header.AceType != windows.ACCESS_ALLOWED_ACE_TYPE {
			return errors.New("unsupported configuration ACL entry")
		}
		sid := (*windows.SID)(unsafe.Pointer(&ace.SidStart)).String()
		if sid != u.Uid && sid != "S-1-5-18" && !(u.Uid == "S-1-5-18" && sid == owner.String()) {
			return errors.New("configuration ACL must allow only the current user and SYSTEM")
		}
	}
	return nil
}
