package locking

import (
	"errors"
	"fmt"
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

func TestAcquireRootDirLockRejectsSecondOwnerAndReleases(t *testing.T) {
	rootDir := filepath.Join(t.TempDir(), "content")

	first, err := AcquireRootDirLock(rootDir)
	if err != nil {
		t.Fatalf("first AcquireRootDirLock failed: %v", err)
	}
	if first.Path() == "" {
		t.Fatalf("root lock path is empty")
	}

	second, err := AcquireRootDirLock(rootDir)
	if err == nil {
		_ = second.Release()
		t.Fatalf("second AcquireRootDirLock succeeded, want root-dir in-use error")
	}
	if !strings.Contains(err.Error(), "root directory is already in use") {
		t.Fatalf("second AcquireRootDirLock error = %v, want in-use message", err)
	}

	if err := first.Release(); err != nil {
		t.Fatalf("Release failed: %v", err)
	}
	again, err := AcquireRootDirLock(rootDir)
	if err != nil {
		t.Fatalf("AcquireRootDirLock after release failed: %v", err)
	}
	if err := again.Release(); err != nil {
		t.Fatalf("second Release failed: %v", err)
	}
}

func TestLockHeldPredicatesRecognizeWrappedSentinelErrors(t *testing.T) {
	dataDir := filepath.Join(t.TempDir(), "data")
	dataLock, err := AcquireDataDirLock(dataDir)
	if err != nil {
		t.Fatalf("first AcquireDataDirLock failed: %v", err)
	}
	defer dataLock.Release()
	_, dataErr := AcquireDataDirLock(dataDir)
	if dataErr == nil {
		t.Fatalf("second AcquireDataDirLock succeeded, want held error")
	}
	wrappedDataErr := fmt.Errorf("acquire data directory lock: %w", dataErr)
	if !IsDataDirLockHeld(wrappedDataErr) || !IsLockHeld(wrappedDataErr) {
		t.Fatalf("data lock predicates rejected wrapped held error: %v", wrappedDataErr)
	}
	if IsRootDirLockHeld(wrappedDataErr) {
		t.Fatalf("root lock predicate accepted data lock error: %v", wrappedDataErr)
	}

	rootDir := filepath.Join(t.TempDir(), "content")
	rootLock, err := AcquireRootDirLock(rootDir)
	if err != nil {
		t.Fatalf("first AcquireRootDirLock failed: %v", err)
	}
	defer rootLock.Release()
	_, rootErr := AcquireRootDirLock(rootDir)
	if rootErr == nil {
		t.Fatalf("second AcquireRootDirLock succeeded, want held error")
	}
	wrappedRootErr := fmt.Errorf("acquire root directory lock: %w", rootErr)
	if !IsRootDirLockHeld(wrappedRootErr) || !IsLockHeld(wrappedRootErr) {
		t.Fatalf("root lock predicates rejected wrapped held error: %v", wrappedRootErr)
	}
	if IsDataDirLockHeld(wrappedRootErr) {
		t.Fatalf("data lock predicate accepted root lock error: %v", wrappedRootErr)
	}

	otherErr := errors.New("open lock file: permission denied")
	if IsLockHeld(otherErr) || IsDataDirLockHeld(otherErr) || IsRootDirLockHeld(otherErr) {
		t.Fatalf("lock predicates accepted non-held error: %v", otherErr)
	}
}
