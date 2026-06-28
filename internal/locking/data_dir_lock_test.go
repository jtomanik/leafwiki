package locking

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("directory locking", func() {
	It("TestAcquireDataDirLockCreatesParentAndReleases", func() {
		dataDir := filepath.Join(GinkgoT().TempDir(), "data")

		lock, err := AcquireDataDirLock(dataDir)
		Expect(err).NotTo(HaveOccurred())
		Expect(lock.Path()).To(Equal(filepath.Join(dataDir, ".leafwiki", "leafwiki.lock")))
		Expect(lock.Release()).To(Succeed())

		lock, err = AcquireDataDirLock(dataDir)
		Expect(err).NotTo(HaveOccurred())
		Expect(lock.Release()).To(Succeed())
	})

	It("TestAcquireDataDirLockRejectsSecondOwner", func() {
		dataDir := filepath.Join(GinkgoT().TempDir(), "data")

		first, err := AcquireDataDirLock(dataDir)
		Expect(err).NotTo(HaveOccurred())
		defer first.Release()

		second, err := AcquireDataDirLock(dataDir)
		if err == nil {
			_ = second.Release()
		}
		Expect(err).To(MatchError(ContainSubstring("data directory is already in use")))
	})

	It("TestAcquireRootDirLockRejectsSecondOwnerAndReleases", func() {
		rootDir := filepath.Join(GinkgoT().TempDir(), "content")

		first, err := AcquireRootDirLock(rootDir)
		Expect(err).NotTo(HaveOccurred())
		Expect(first.Path()).NotTo(BeEmpty())

		second, err := AcquireRootDirLock(rootDir)
		if err == nil {
			_ = second.Release()
		}
		Expect(err).To(MatchError(ContainSubstring("root directory is already in use")))

		Expect(first.Release()).To(Succeed())
		again, err := AcquireRootDirLock(rootDir)
		Expect(err).NotTo(HaveOccurred())
		Expect(again.Release()).To(Succeed())
	})

	It("TestLockHeldPredicatesRecognizeWrappedSentinelErrors", func() {
		dataDir := filepath.Join(GinkgoT().TempDir(), "data")
		dataLock, err := AcquireDataDirLock(dataDir)
		Expect(err).NotTo(HaveOccurred())
		defer dataLock.Release()
		_, dataErr := AcquireDataDirLock(dataDir)
		Expect(dataErr).To(HaveOccurred())
		wrappedDataErr := fmt.Errorf("acquire data directory lock: %w", dataErr)
		Expect(IsDataDirLockHeld(wrappedDataErr)).To(BeTrue())
		Expect(IsLockHeld(wrappedDataErr)).To(BeTrue())
		Expect(IsRootDirLockHeld(wrappedDataErr)).To(BeFalse())

		rootDir := filepath.Join(GinkgoT().TempDir(), "content")
		rootLock, err := AcquireRootDirLock(rootDir)
		Expect(err).NotTo(HaveOccurred())
		defer rootLock.Release()
		_, rootErr := AcquireRootDirLock(rootDir)
		Expect(rootErr).To(HaveOccurred())
		wrappedRootErr := fmt.Errorf("acquire root directory lock: %w", rootErr)
		Expect(IsRootDirLockHeld(wrappedRootErr)).To(BeTrue())
		Expect(IsLockHeld(wrappedRootErr)).To(BeTrue())
		Expect(IsDataDirLockHeld(wrappedRootErr)).To(BeFalse())

		otherErr := errors.New("open lock file: permission denied")
		Expect(IsLockHeld(otherErr)).To(BeFalse())
		Expect(IsDataDirLockHeld(otherErr)).To(BeFalse())
		Expect(IsRootDirLockHeld(otherErr)).To(BeFalse())
	})
})

var _ = Describe("locking edge coverage", func() {
	It("returns an empty path for a nil lock receiver", func() {
		var lock *DataDirLock

		Expect(lock.Path()).To(BeEmpty())
	})

	It("treats release on a nil lock receiver as a no-op", func() {
		var lock *DataDirLock

		Expect(lock.Release()).To(Succeed())
	})

	It("treats release as idempotent after a successful release", func() {
		lock, err := AcquireDataDirLock(filepath.Join(GinkgoT().TempDir(), "data"))
		Expect(err).NotTo(HaveOccurred())

		Expect(lock.Release()).To(Succeed())
		Expect(lock.Release()).To(Succeed())
	})

	It("stores root-dir locks under the user cache root instead of the content root", func() {
		rootDir := filepath.Join(GinkgoT().TempDir(), "content")
		cacheDir, err := os.UserCacheDir()
		if err != nil || cacheDir == "" {
			cacheDir = os.TempDir()
		}

		lock, err := AcquireRootDirLock(rootDir)
		Expect(err).NotTo(HaveOccurred())
		defer lock.Release()

		Expect(lock.Path()).To(HavePrefix(filepath.Join(cacheDir, "leafwiki", "locks", "roots") + string(os.PathSeparator)))
		Expect(lock.Path()).NotTo(HavePrefix(rootDir + string(os.PathSeparator)))
	})

	It("canonicalizes symlinked root lock subjects to their resolved target", func() {
		tempDir := GinkgoT().TempDir()
		targetDir := filepath.Join(tempDir, "target")
		linkDir := filepath.Join(tempDir, "link")
		Expect(os.Mkdir(targetDir, 0o755)).To(Succeed())
		Expect(os.Symlink(targetDir, linkDir)).To(Succeed())

		targetSubject, err := canonicalLockSubject(targetDir)
		Expect(err).NotTo(HaveOccurred())
		linkSubject, err := canonicalLockSubject(linkDir)
		Expect(err).NotTo(HaveOccurred())

		Expect(linkSubject).To(Equal(targetSubject))
	})

	It("wraps canonical root lock subject failures", func() {
		previousAbs := filepathAbsFn
		absErr := errors.New("absolute path failed")
		filepathAbsFn = func(string) (string, error) {
			return "", absErr
		}
		DeferCleanup(func() {
			filepathAbsFn = previousAbs
		})

		lock, err := AcquireRootDirLock("content")

		Expect(lock).To(BeNil())
		Expect(err).To(MatchError(ContainSubstring("resolve root directory lock subject")))
		Expect(errors.Is(err, absErr)).To(BeTrue())
	})

	It("returns a create-directory error when the lock parent path is blocked by a file", func() {
		blockedParent := filepath.Join(GinkgoT().TempDir(), "blocked")
		Expect(os.WriteFile(blockedParent, []byte("not a directory"), 0o600)).To(Succeed())

		lock, err := acquirePathLock(filepath.Join(blockedParent, "leafwiki.lock"), "subject", "custom", errDataDirLockHeld)

		Expect(lock).To(BeNil())
		Expect(err).To(MatchError(ContainSubstring("create lock directory")))
	})

	It("returns an open-lock error when the requested lock path is a directory", func() {
		lockPath := filepath.Join(GinkgoT().TempDir(), "lock-dir")
		Expect(os.Mkdir(lockPath, 0o755)).To(Succeed())

		lock, err := acquirePathLock(lockPath, "subject", "custom", errDataDirLockHeld)

		Expect(lock).To(BeNil())
		Expect(err).To(MatchError(ContainSubstring("open custom lock")))
	})

	It("returns non-contention errors from the lock syscall", func() {
		previousLock := lockDataDirFileFn
		lockErr := errors.New("lock syscall failed")
		lockDataDirFileFn = func(*os.File) error {
			return lockErr
		}
		DeferCleanup(func() {
			lockDataDirFileFn = previousLock
		})

		lock, err := acquirePathLock(filepath.Join(GinkgoT().TempDir(), "leafwiki.lock"), "subject", "custom", errDataDirLockHeld)

		Expect(lock).To(BeNil())
		Expect(err).To(MatchError(ContainSubstring("acquire custom lock")))
		Expect(errors.Is(err, lockErr)).To(BeTrue())
	})

	It("reports release errors with the default data-directory label", func() {
		file, err := os.CreateTemp(GinkgoT().TempDir(), "closed-lock-*")
		Expect(err).NotTo(HaveOccurred())
		Expect(file.Close()).To(Succeed())
		lock := &DataDirLock{file: file}

		err = lock.Release()

		Expect(err).To(MatchError(ContainSubstring("release data directory lock")))
		Expect(lock.Release()).To(Succeed())
	})

	It("reports close errors when unlocking succeeds", func() {
		file, err := os.CreateTemp(GinkgoT().TempDir(), "close-error-lock-*")
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(func() {
			_ = file.Close()
		})
		closeErr := errors.New("close failed")
		previousUnlock := unlockDataDirFileFn
		previousClose := closeDataDirLockFileFn
		unlockDataDirFileFn = func(*os.File) error {
			return nil
		}
		closeDataDirLockFileFn = func(*os.File) error {
			return closeErr
		}
		DeferCleanup(func() {
			unlockDataDirFileFn = previousUnlock
			closeDataDirLockFileFn = previousClose
		})
		lock := &DataDirLock{file: file, label: "custom"}

		err = lock.Release()

		Expect(err).To(MatchError(ContainSubstring("close custom lock")))
		Expect(errors.Is(err, closeErr)).To(BeTrue())
		Expect(lock.Release()).To(Succeed())
	})

	It("falls back to temp storage when the user cache directory is unavailable", func() {
		home, hadHome := os.LookupEnv("HOME")
		xdgCacheHome, hadXDGCacheHome := os.LookupEnv("XDG_CACHE_HOME")
		Expect(os.Unsetenv("HOME")).To(Succeed())
		Expect(os.Unsetenv("XDG_CACHE_HOME")).To(Succeed())
		DeferCleanup(restoreEnv, "HOME", home, hadHome)
		DeferCleanup(restoreEnv, "XDG_CACHE_HOME", xdgCacheHome, hadXDGCacheHome)
		if cacheDir, err := os.UserCacheDir(); err == nil && cacheDir != "" {
			Skip("platform still provides a user cache dir without HOME or XDG_CACHE_HOME")
		}

		rootDir := filepath.Join(GinkgoT().TempDir(), "content")
		lock, err := AcquireRootDirLock(rootDir)

		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(func() {
			Expect(lock.Release()).To(Succeed())
		})
		Expect(lock.Path()).To(HavePrefix(filepath.Join(os.TempDir(), "leafwiki", "locks", "roots") + string(os.PathSeparator)))
	})

	It("returns non-contention lock errors from invalid lock files", func() {
		file, err := os.CreateTemp(GinkgoT().TempDir(), "closed-lock-*")
		Expect(err).NotTo(HaveOccurred())
		Expect(file.Close()).To(Succeed())

		err = lockDataDirFile(file)

		Expect(err).To(HaveOccurred())
		Expect(errors.Is(err, errDataDirLockHeld)).To(BeFalse())
	})
})

func restoreEnv(key, value string, present bool) {
	GinkgoHelper()
	if present {
		Expect(os.Setenv(key, value)).To(Succeed())
		return
	}
	Expect(os.Unsetenv(key)).To(Succeed())
}
