package locking

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func tempLockingDir() string {
	GinkgoHelper()

	dir, err := os.MkdirTemp("", "leafwiki-locking-*")
	Expect(err).NotTo(HaveOccurred())
	DeferCleanup(os.RemoveAll, dir)
	return dir
}

func setLockingEnv(key, value string) {
	GinkgoHelper()

	previous, hadPrevious := os.LookupEnv(key)
	Expect(os.Setenv(key, value)).To(Succeed())
	DeferCleanup(func() {
		if hadPrevious {
			Expect(os.Setenv(key, previous)).To(Succeed())
			return
		}
		Expect(os.Unsetenv(key)).To(Succeed())
	})
}

func createTempLockFile(pattern string) *os.File {
	GinkgoHelper()

	file, err := os.CreateTemp(tempLockingDir(), pattern)
	Expect(err).NotTo(HaveOccurred())
	DeferCleanup(func() {
		_ = file.Close()
	})
	return file
}

var _ = Describe("directory locking", func() {
	It("creates the data lock parent and releases ownership for a later acquisition", func() {
		dataDir := filepath.Join(tempLockingDir(), "data")

		lock, err := AcquireDataDirLock(dataDir)
		Expect(err).NotTo(HaveOccurred())
		Expect(lock.Path()).To(Equal(filepath.Join(dataDir, ".leafwiki", "leafwiki.lock")))
		Expect(lock.Release()).To(Succeed())

		lock, err = AcquireDataDirLock(dataDir)
		Expect(err).NotTo(HaveOccurred())
		Expect(lock.Release()).To(Succeed())
	})

	It("rejects a second data-directory owner while the first lock is held", func() {
		dataDir := filepath.Join(tempLockingDir(), "data")

		first, err := AcquireDataDirLock(dataDir)
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(first.Release)

		second, err := AcquireDataDirLock(dataDir)
		if err == nil {
			_ = second.Release()
		}
		Expect(err).To(MatchError(errDataDirLockHeld))
	})

	It("rejects a second root-directory owner and allows acquisition after release", func() {
		rootDir := filepath.Join(tempLockingDir(), "content")

		first, err := AcquireRootDirLock(rootDir)
		Expect(err).NotTo(HaveOccurred())
		Expect(first.Path()).NotTo(BeEmpty())

		second, err := AcquireRootDirLock(rootDir)
		if err == nil {
			_ = second.Release()
		}
		Expect(err).To(MatchError(errRootDirLockHeld))

		Expect(first.Release()).To(Succeed())
		again, err := AcquireRootDirLock(rootDir)
		Expect(err).NotTo(HaveOccurred())
		Expect(again.Release()).To(Succeed())
	})

	It("recognizes wrapped data and root lock contention errors", func() {
		dataDir := filepath.Join(tempLockingDir(), "data")
		dataLock, err := AcquireDataDirLock(dataDir)
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(dataLock.Release)
		_, dataErr := AcquireDataDirLock(dataDir)
		Expect(dataErr).To(MatchError(errDataDirLockHeld))
		wrappedDataErr := fmt.Errorf("acquire data directory lock: %w", dataErr)
		Expect(IsDataDirLockHeld(wrappedDataErr)).To(BeTrue())
		Expect(IsLockHeld(wrappedDataErr)).To(BeTrue())
		Expect(IsRootDirLockHeld(wrappedDataErr)).To(BeFalse())

		rootDir := filepath.Join(tempLockingDir(), "content")
		rootLock, err := AcquireRootDirLock(rootDir)
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(rootLock.Release)
		_, rootErr := AcquireRootDirLock(rootDir)
		Expect(rootErr).To(MatchError(errRootDirLockHeld))
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

var _ = Describe("directory lock edge behavior", func() {
	It("returns an empty path for a nil lock receiver", func() {
		var lock *DataDirLock

		Expect(lock.Path()).To(BeEmpty())
	})

	It("treats release on a nil lock receiver as a no-op", func() {
		var lock *DataDirLock

		Expect(lock.Release()).To(Succeed())
	})

	It("treats release as idempotent after a successful release", func() {
		lock, err := AcquireDataDirLock(filepath.Join(tempLockingDir(), "data"))
		Expect(err).NotTo(HaveOccurred())

		Expect(lock.Release()).To(Succeed())
		Expect(lock.Release()).To(Succeed())
	})

	It("stores root-dir locks under the user cache root instead of the content root", func() {
		rootDir := filepath.Join(tempLockingDir(), "content")
		cacheDir, err := os.UserCacheDir()
		if err != nil || cacheDir == "" {
			cacheDir = os.TempDir()
		}

		lock, err := AcquireRootDirLock(rootDir)
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(lock.Release)

		Expect(lock.Path()).To(HavePrefix(filepath.Join(cacheDir, "leafwiki", "locks", "roots") + string(os.PathSeparator)))
		Expect(lock.Path()).NotTo(HavePrefix(rootDir + string(os.PathSeparator)))
	})

	It("canonicalizes symlinked root lock subjects to their resolved target", func() {
		tempDir := tempLockingDir()
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
		Expect(err).To(MatchError(absErr))
	})

	It("returns a create-directory error when the lock parent path is blocked by a file", func() {
		blockedParent := filepath.Join(tempLockingDir(), "blocked")
		Expect(os.WriteFile(blockedParent, []byte("not a directory"), 0o600)).To(Succeed())

		lock, err := acquirePathLock(filepath.Join(blockedParent, "leafwiki.lock"), "subject", "custom", errDataDirLockHeld)

		Expect(lock).To(BeNil())
		Expect(err).To(MatchError(syscall.ENOTDIR))
	})

	It("returns an open-lock error when the requested lock path is a directory", func() {
		lockPath := filepath.Join(tempLockingDir(), "lock-dir")
		Expect(os.Mkdir(lockPath, 0o755)).To(Succeed())

		lock, err := acquirePathLock(lockPath, "subject", "custom", errDataDirLockHeld)

		Expect(lock).To(BeNil())
		Expect(err).To(MatchError(syscall.EISDIR))
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

		lock, err := acquirePathLock(filepath.Join(tempLockingDir(), "leafwiki.lock"), "subject", "custom", errDataDirLockHeld)

		Expect(lock).To(BeNil())
		Expect(err).To(MatchError(lockErr))
	})

	It("reports release errors with the default data-directory label", func() {
		file := createTempLockFile("closed-lock-*")
		unlockErr := errors.New("unlock failed")
		previousUnlock := unlockDataDirFileFn
		unlockDataDirFileFn = func(*os.File) error {
			return unlockErr
		}
		DeferCleanup(func() {
			unlockDataDirFileFn = previousUnlock
		})
		lock := &DataDirLock{file: file}

		err := lock.Release()

		Expect(err).To(MatchError(unlockErr))
		Expect(lock.Release()).To(Succeed())
	})

	It("reports close errors when unlocking succeeds", func() {
		file := createTempLockFile("close-error-lock-*")
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

		err := lock.Release()

		Expect(err).To(MatchError(closeErr))
		Expect(lock.Release()).To(Succeed())
	})

	It("falls back to temp storage when the user cache directory is unavailable", func() {
		setLockingEnv("HOME", "")
		setLockingEnv("XDG_CACHE_HOME", "")
		if cacheDir, err := os.UserCacheDir(); err == nil && cacheDir != "" {
			Skip("platform still provides a user cache dir without HOME or XDG_CACHE_HOME")
		}

		rootDir := filepath.Join(tempLockingDir(), "content")
		lock, err := AcquireRootDirLock(rootDir)

		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(func() {
			Expect(lock.Release()).To(Succeed())
		})
		Expect(lock.Path()).To(HavePrefix(filepath.Join(os.TempDir(), "leafwiki", "locks", "roots") + string(os.PathSeparator)))
	})

	It("returns non-contention lock errors from invalid lock files", func() {
		file := createTempLockFile("closed-lock-*")
		Expect(file.Close()).To(Succeed())

		err := lockDataDirFile(file)

		Expect(err).To(MatchError(syscall.EBADF))
		Expect(err).NotTo(MatchError(errDataDirLockHeld))
	})
})
