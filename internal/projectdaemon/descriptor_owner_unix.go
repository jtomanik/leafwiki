//go:build !windows

package projectdaemon

import (
	"errors"
	"fmt"
	"os"
	"syscall"
)

var (
	errDescriptorOwnerUnavailable = errors.New("project daemon descriptor owner is unavailable")
	errDescriptorOwnerMismatch    = errors.New("project daemon descriptor owner mismatch")
)

func validateDescriptorOwner(_ string, info os.FileInfo) error {
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return errDescriptorOwnerUnavailable
	}
	if stat.Uid != uint32(os.Geteuid()) {
		return fmt.Errorf("%w: uid = %d, want %d", errDescriptorOwnerMismatch, stat.Uid, os.Geteuid())
	}
	return nil
}
