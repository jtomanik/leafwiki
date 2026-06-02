package locking

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestAcquireDataDirLockCreatesParentAndReleases(t *testing.T) {
	dataDir := filepath.Join(t.TempDir(), "data")

	lock, err := AcquireDataDirLock(dataDir)
	if err != nil {
		t.Fatalf("AcquireDataDirLock failed: %v", err)
	}
	if got, want := lock.Path(), filepath.Join(dataDir, ".leafwiki", "leafwiki.lock"); got != want {
		t.Fatalf("lock path = %q, want %q", got, want)
	}
	if err := lock.Release(); err != nil {
		t.Fatalf("Release failed: %v", err)
	}

	lock, err = AcquireDataDirLock(dataDir)
	if err != nil {
		t.Fatalf("AcquireDataDirLock after release failed: %v", err)
	}
	if err := lock.Release(); err != nil {
		t.Fatalf("second Release failed: %v", err)
	}
}

func TestAcquireDataDirLockRejectsSecondOwner(t *testing.T) {
	dataDir := filepath.Join(t.TempDir(), "data")

	first, err := AcquireDataDirLock(dataDir)
	if err != nil {
		t.Fatalf("first AcquireDataDirLock failed: %v", err)
	}
	defer first.Release()

	second, err := AcquireDataDirLock(dataDir)
	if err == nil {
		_ = second.Release()
		t.Fatalf("second AcquireDataDirLock succeeded, want data-dir in-use error")
	}
	if !strings.Contains(err.Error(), "data directory is already in use") {
		t.Fatalf("second AcquireDataDirLock error = %v, want in-use message", err)
	}
}
