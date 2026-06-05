//go:build !windows

package projectdaemon

import (
	"fmt"
	"os"
	"syscall"
)

func validateDescriptorOwner(_ string, info os.FileInfo) error {
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return fmt.Errorf("project daemon descriptor owner is unavailable")
	}
	if stat.Uid != uint32(os.Geteuid()) {
		return fmt.Errorf("project daemon descriptor owner uid = %d, want %d", stat.Uid, os.Geteuid())
	}
	return nil
}
