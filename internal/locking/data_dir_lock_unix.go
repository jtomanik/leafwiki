//go:build !windows

package locking

import (
	"errors"
	"os"

	"golang.org/x/sys/unix"
)

func lockDataDirFile(file *os.File) error {
	if err := unix.Flock(int(file.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		if errors.Is(err, unix.EWOULDBLOCK) || errors.Is(err, unix.EAGAIN) {
			return errDataDirLockHeld
		}
		return err
	}
	return nil
}

func unlockDataDirFile(file *os.File) error {
	return unix.Flock(int(file.Fd()), unix.LOCK_UN)
}
