//go:build windows

package projectdaemon

import (
	"fmt"
	"os"

	"golang.org/x/sys/windows"
)

func validateDescriptorOwner(path string, _ os.FileInfo) error {
	sd, err := windows.GetNamedSecurityInfo(path, windows.SE_FILE_OBJECT, windows.OWNER_SECURITY_INFORMATION)
	if err != nil {
		return fmt.Errorf("read project daemon descriptor owner: %w", err)
	}
	if sd == nil {
		return fmt.Errorf("project daemon descriptor owner is unavailable")
	}
	owner, _, err := sd.Owner()
	if err != nil {
		return fmt.Errorf("read project daemon descriptor owner SID: %w", err)
	}
	if owner == nil {
		return fmt.Errorf("project daemon descriptor owner is unavailable")
	}
	currentUser, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		return fmt.Errorf("read current process owner SID: %w", err)
	}
	if currentUser == nil || currentUser.User.Sid == nil {
		return fmt.Errorf("current process owner SID is unavailable")
	}
	if !windows.EqualSid(owner, currentUser.User.Sid) {
		return fmt.Errorf("project daemon descriptor owner does not match current user")
	}
	return nil
}
