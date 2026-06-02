package locking

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

var errDataDirLockHeld = errors.New("data directory is already in use")

type DataDirLock struct {
	file *os.File
	path string
}

func AcquireDataDirLock(dataDir string) (*DataDirLock, error) {
	lockPath := filepath.Join(dataDir, ".leafwiki", "leafwiki.lock")
	if err := os.MkdirAll(filepath.Dir(lockPath), 0o755); err != nil {
		return nil, fmt.Errorf("create lock directory: %w", err)
	}

	file, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open data directory lock: %w", err)
	}
	if err := lockDataDirFile(file); err != nil {
		_ = file.Close()
		if errors.Is(err, errDataDirLockHeld) {
			return nil, fmt.Errorf("%w: %s", errDataDirLockHeld, dataDir)
		}
		return nil, fmt.Errorf("acquire data directory lock: %w", err)
	}

	return &DataDirLock{file: file, path: lockPath}, nil
}

func (l *DataDirLock) Path() string {
	if l == nil {
		return ""
	}
	return l.path
}

func (l *DataDirLock) Release() error {
	if l == nil || l.file == nil {
		return nil
	}
	file := l.file
	l.file = nil
	var unlockErr error
	if err := unlockDataDirFile(file); err != nil {
		unlockErr = fmt.Errorf("release data directory lock: %w", err)
	}
	if err := file.Close(); err != nil && unlockErr == nil {
		unlockErr = fmt.Errorf("close data directory lock: %w", err)
	}
	return unlockErr
}
