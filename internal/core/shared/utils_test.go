package shared

import (
	"bytes"
	cryptorand "crypto/rand"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("atomic file utilities", func() {
	It("TestWriteFileAtomic_WritesToTargetFile", func() {
		tmp := GinkgoT().TempDir()
		target := filepath.Join(tmp, "page.md")

		Expect(WriteFileAtomic(target, []byte("hello"), 0o644)).To(Succeed())

		raw, err := os.ReadFile(target)
		Expect(err).NotTo(HaveOccurred())
		Expect(string(raw)).To(Equal("hello"))
	})

	It("TestWriteStreamAtomic_WritesToTargetFile", func() {
		tmp := GinkgoT().TempDir()
		target := filepath.Join(tmp, "asset.bin")

		Expect(WriteStreamAtomic(target, bytes.NewBufferString("hello stream"), 1024)).To(Succeed())

		raw, err := os.ReadFile(target)
		Expect(err).NotTo(HaveOccurred())
		Expect(string(raw)).To(Equal("hello stream"))
	})
})

type atomicWriteDirCase struct {
	path string
	want string
}

var _ = DescribeTable("TestAtomicWriteDir_WindowsPath",
	func(tc atomicWriteDirCase) {
		got := strings.ReplaceAll(atomicWriteDir(tc.path), `\`, `/`)

		Expect(got).To(Equal(tc.want))
	},
	Entry("markdown page", atomicWriteDirCase{
		path: `C:\wiki\data\root\page.md`,
		want: `C:/wiki/data/root`,
	}),
	Entry("asset file", atomicWriteDirCase{
		path: `C:\wiki\data\assets\a7b3\image.png`,
		want: `C:/wiki/data/assets/a7b3`,
	}),
)

var _ = Describe("shared utility edge coverage", func() {
	It("CopyWithLimit succeeds exactly at the byte cap", func() {
		var dst bytes.Buffer

		err := CopyWithLimit(&dst, strings.NewReader("hello"), 5)

		Expect(err).NotTo(HaveOccurred())
		Expect(dst.String()).To(Equal("hello"))
	})

	It("CopyWithLimit returns ErrFileTooLarge when the cap is exceeded", func() {
		var dst bytes.Buffer

		err := CopyWithLimit(&dst, strings.NewReader("hello!"), 5)

		Expect(err).To(MatchError(ContainSubstring("file too large")))
		Expect(errors.Is(err, ErrFileTooLarge)).To(BeTrue())
	})

	It("CopyWithLimit preserves reader copy errors", func() {
		copyErr := errors.New("read failed")

		err := CopyWithLimit(io.Discard, errorReader{err: copyErr}, 5)

		Expect(errors.Is(err, copyErr)).To(BeTrue())
	})

	It("GenerateRandomPassword returns the requested length using the allowed charset", func() {
		password, err := GenerateRandomPassword(24)

		Expect(err).NotTo(HaveOccurred())
		Expect(password).To(HaveLen(24))
		for _, ch := range password {
			Expect(charset).To(ContainSubstring(string(ch)))
		}
	})

	It("GenerateRandomPassword returns entropy reader errors", func() {
		entropyErr := errors.New("entropy unavailable")
		previousReader := cryptorand.Reader
		cryptorand.Reader = errorReader{err: entropyErr}
		DeferCleanup(func() {
			cryptorand.Reader = previousReader
		})

		password, err := GenerateRandomPassword(1)

		Expect(errors.Is(err, entropyErr)).To(BeTrue())
		Expect(password).To(BeEmpty())
	})

	It("WriteFileAtomic honors nonzero permissions", func() {
		target := filepath.Join(GinkgoT().TempDir(), "page.md")

		Expect(WriteFileAtomic(target, []byte("hello"), 0o600)).To(Succeed())
		info, err := os.Stat(target)
		Expect(err).NotTo(HaveOccurred())
		Expect(info.Mode().Perm()).To(Equal(os.FileMode(0o600)))
	})

	It("GenerateUniqueID returns a non-empty id", func() {
		id, err := GenerateUniqueID()

		Expect(err).NotTo(HaveOccurred())
		Expect(id).NotTo(BeEmpty())
	})

	It("GenerateUniqueID returns short id generator errors", func() {
		generateErr := errors.New("short id unavailable")
		DeferCleanup(restoreSharedUtilitySeams())
		generateShortID = func() (string, error) {
			return "", generateErr
		}

		id, err := GenerateUniqueID()

		Expect(errors.Is(err, generateErr)).To(BeTrue())
		Expect(id).To(BeEmpty())
	})

	It("atomicReplace removes Windows targets before rename and returns remove failures", func() {
		DeferCleanup(restoreSharedUtilitySeams())
		runtimeGOOS = "windows"
		removed := ""
		renamed := false
		removeFile = func(path string) error {
			removed = path
			return nil
		}
		renameFile = func(src string, dst string) error {
			renamed = true
			Expect(src).To(Equal("src"))
			Expect(dst).To(Equal("dst"))
			return nil
		}

		Expect(atomicReplace("src", "dst")).To(Succeed())
		Expect(removed).To(Equal("dst"))
		Expect(renamed).To(BeTrue())

		removeErr := errors.New("remove failed")
		removeFile = func(string) error {
			return removeErr
		}
		err := atomicReplace("src", "dst")
		Expect(errors.Is(err, removeErr)).To(BeTrue())
		Expect(err).To(MatchError(ContainSubstring("remove existing file")))
	})

	It("WriteFileAtomic reports temp-file creation failures", func() {
		blockedParent := filepath.Join(GinkgoT().TempDir(), "blocked")
		Expect(os.WriteFile(blockedParent, []byte("not a directory"), 0o600)).To(Succeed())

		err := WriteFileAtomic(filepath.Join(blockedParent, "page.md"), []byte("hello"), 0o644)

		Expect(err).To(MatchError(ContainSubstring("create temp file")))
	})

	It("WriteFileAtomic reports rename failures after closing the temp file", func() {
		if runtime.GOOS == "windows" {
			Skip("windows removes an empty target directory before rename")
		}
		target := filepath.Join(GinkgoT().TempDir(), "target-dir")
		Expect(os.Mkdir(target, 0o755)).To(Succeed())

		err := WriteFileAtomic(target, []byte("hello"), 0o644)

		Expect(err).To(MatchError(ContainSubstring("rename temp file")))
	})

	It("WriteFileAtomic reports chmod, write, sync, and close failures from the temp file", func() {
		cases := []struct {
			name      string
			perm      os.FileMode
			configure func(*fakeAtomicTempFile, error)
			want      string
		}{
			{
				name: "chmod",
				perm: 0o600,
				configure: func(file *fakeAtomicTempFile, err error) {
					file.chmodErr = err
					file.closeErr = errors.New("close after chmod failed")
				},
				want: "chmod temp file",
			},
			{
				name: "write",
				configure: func(file *fakeAtomicTempFile, err error) {
					file.writeErr = err
					file.closeErr = errors.New("close after write failed")
				},
				want: "write temp file",
			},
			{
				name: "sync",
				configure: func(file *fakeAtomicTempFile, err error) {
					file.syncErr = err
					file.closeErr = errors.New("close after sync failed")
				},
				want: "sync temp file",
			},
			{
				name: "close",
				configure: func(file *fakeAtomicTempFile, err error) {
					file.closeErr = err
				},
				want: "close temp file",
			},
		}

		for _, tc := range cases {
			tc := tc
			By(tc.name)
			func() {
				restore := restoreSharedUtilitySeams()
				defer restore()
				failure := errors.New(tc.name + " failed")
				fakeFile := &fakeAtomicTempFile{name: filepath.Join(GinkgoT().TempDir(), ".tmp-shared")}
				tc.configure(fakeFile, failure)
				createTempFile = func(string, string) (atomicTempFile, error) {
					return fakeFile, nil
				}

				err := WriteFileAtomic(filepath.Join(GinkgoT().TempDir(), "target"), []byte("hello"), tc.perm)

				Expect(errors.Is(err, failure)).To(BeTrue())
				Expect(err).To(MatchError(ContainSubstring(tc.want)))
				Expect(fakeFile.closeCalls).To(BeNumerically(">=", 1))
			}()
		}
	})

	It("WriteStreamAtomic reports temp-file creation failures", func() {
		blockedParent := filepath.Join(GinkgoT().TempDir(), "blocked")
		Expect(os.WriteFile(blockedParent, []byte("not a directory"), 0o600)).To(Succeed())

		err := WriteStreamAtomic(filepath.Join(blockedParent, "asset.bin"), strings.NewReader("hello"), 1024)

		Expect(err).To(HaveOccurred())
	})

	It("WriteStreamAtomic preserves reader errors and removes the target", func() {
		target := filepath.Join(GinkgoT().TempDir(), "asset.bin")
		copyErr := errors.New("stream read failed")

		err := WriteStreamAtomic(target, errorReader{err: copyErr}, 1024)

		Expect(errors.Is(err, copyErr)).To(BeTrue())
		Expect(target).NotTo(BeAnExistingFile())
	})

	It("WriteStreamAtomic rejects streams over the byte cap", func() {
		target := filepath.Join(GinkgoT().TempDir(), "asset.bin")

		err := WriteStreamAtomic(target, strings.NewReader("hello!"), 5)

		Expect(errors.Is(err, ErrFileTooLarge)).To(BeTrue())
		Expect(target).NotTo(BeAnExistingFile())
	})

	It("WriteStreamAtomic reports rename failures after closing the temp file", func() {
		if runtime.GOOS == "windows" {
			Skip("windows removes an empty target directory before rename")
		}
		target := filepath.Join(GinkgoT().TempDir(), "target-dir")
		Expect(os.Mkdir(target, 0o755)).To(Succeed())

		err := WriteStreamAtomic(target, strings.NewReader("hello"), 1024)

		Expect(err).To(HaveOccurred())
	})

	It("WriteStreamAtomic reports temp-file write, sync, and close failures", func() {
		cases := []struct {
			name      string
			configure func(*fakeAtomicTempFile, error)
		}{
			{
				name: "write",
				configure: func(file *fakeAtomicTempFile, err error) {
					file.writeErr = err
					file.closeErr = errors.New("deferred close failed")
				},
			},
			{
				name: "sync",
				configure: func(file *fakeAtomicTempFile, err error) {
					file.syncErr = err
				},
			},
			{
				name: "close",
				configure: func(file *fakeAtomicTempFile, err error) {
					file.closeErr = err
				},
			},
		}

		for _, tc := range cases {
			tc := tc
			By(tc.name)
			func() {
				restore := restoreSharedUtilitySeams()
				defer restore()
				failure := errors.New(tc.name + " failed")
				fakeFile := &fakeAtomicTempFile{name: filepath.Join(GinkgoT().TempDir(), ".tmp-stream")}
				tc.configure(fakeFile, failure)
				createTempFile = func(string, string) (atomicTempFile, error) {
					return fakeFile, nil
				}

				err := WriteStreamAtomic(filepath.Join(GinkgoT().TempDir(), "target"), strings.NewReader("hello"), 1024)

				Expect(errors.Is(err, failure)).To(BeTrue())
				Expect(fakeFile.closeCalls).To(BeNumerically(">=", 1))
			}()
		}
	})

	It("LogClose invokes the closer and suppresses successful closes", func() {
		called := false

		LogClose(func() error {
			called = true
			return nil
		}, "close resource")

		Expect(called).To(BeTrue())
	})

	It("LogClose logs close errors without returning them", func() {
		previousLogger := slog.Default()
		slog.SetDefault(slog.New(slog.NewTextHandler(io.Discard, nil)))
		DeferCleanup(func() {
			slog.SetDefault(previousLogger)
		})
		called := false

		LogClose(func() error {
			called = true
			return errors.New("close failed")
		}, "close resource")

		Expect(called).To(BeTrue())
	})
})

type errorReader struct {
	err error
}

func (r errorReader) Read([]byte) (int, error) {
	return 0, r.err
}

type fakeAtomicTempFile struct {
	name       string
	chmodErr   error
	writeErr   error
	syncErr    error
	closeErr   error
	closeCalls int
}

func (f *fakeAtomicTempFile) Name() string {
	return f.name
}

func (f *fakeAtomicTempFile) Chmod(os.FileMode) error {
	return f.chmodErr
}

func (f *fakeAtomicTempFile) Write(p []byte) (int, error) {
	if f.writeErr != nil {
		return 0, f.writeErr
	}
	return len(p), nil
}

func (f *fakeAtomicTempFile) Sync() error {
	return f.syncErr
}

func (f *fakeAtomicTempFile) Close() error {
	f.closeCalls++
	return f.closeErr
}

func restoreSharedUtilitySeams() func() {
	previousGenerateShortID := generateShortID
	previousCreateTempFile := createTempFile
	previousRemoveFile := removeFile
	previousRenameFile := renameFile
	previousRuntimeGOOS := runtimeGOOS
	return func() {
		generateShortID = previousGenerateShortID
		createTempFile = previousCreateTempFile
		removeFile = previousRemoveFile
		renameFile = previousRenameFile
		runtimeGOOS = previousRuntimeGOOS
	}
}
