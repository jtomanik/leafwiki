package locking

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

var errDataDirLockHeld = errors.New("data directory is already in use")
var errRootDirLockHeld = errors.New("root directory is already in use")

type DataDirLock struct {
	file  *os.File
	path  string
	label string
}

func AcquireDataDirLock(dataDir string) (*DataDirLock, error) {
	lockPath := filepath.Join(dataDir, ".leafwiki", "leafwiki.lock")
	return acquirePathLock(lockPath, dataDir, "data directory", errDataDirLockHeld)
}

func AcquireRootDirLock(rootDir string) (*DataDirLock, error) {
	subject, err := canonicalLockSubject(rootDir)
	if err != nil {
		return nil, fmt.Errorf("resolve root directory lock subject: %w", err)
	}
	cacheDir, err := os.UserCacheDir()
	if err != nil || cacheDir == "" {
		cacheDir = os.TempDir()
	}
	sum := sha256.Sum256([]byte(subject))
	lockPath := filepath.Join(cacheDir, "leafwiki", "locks", "roots", hex.EncodeToString(sum[:])+".lock")
	return acquirePathLock(lockPath, subject, "root directory", errRootDirLockHeld)
}

func canonicalLockSubject(path string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	if resolved, err := filepath.EvalSymlinks(abs); err == nil {
		abs = resolved
	}
	return filepath.Clean(abs), nil
}

func acquirePathLock(lockPath, subject, label string, heldErr error) (*DataDirLock, error) {
	if err := os.MkdirAll(filepath.Dir(lockPath), 0o755); err != nil {
		return nil, fmt.Errorf("create lock directory: %w", err)
	}

	file, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open %s lock: %w", label, err)
	}
	if err := lockDataDirFile(file); err != nil {
		_ = file.Close()
		if errors.Is(err, errDataDirLockHeld) {
			return nil, fmt.Errorf("%w: %s", heldErr, subject)
		}
		return nil, fmt.Errorf("acquire %s lock: %w", label, err)
	}

	return &DataDirLock{file: file, path: lockPath, label: label}, nil
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
	label := l.label
	if label == "" {
		label = "data directory"
	}
	var unlockErr error
	if err := unlockDataDirFile(file); err != nil {
		unlockErr = fmt.Errorf("release %s lock: %w", label, err)
	}
	if err := file.Close(); err != nil && unlockErr == nil {
		unlockErr = fmt.Errorf("close %s lock: %w", label, err)
	}
	return unlockErr
}
