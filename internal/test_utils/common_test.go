package test_utils

import (
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"os"
	"path/filepath"

	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

const wrapCloseFailurePrefix = "failed to close resource"

var errFixtureCloseFailed = errors.New("close failed")

var _ = ginkgo.Describe("test utilities", func() {
	ginkgo.It("CreateMultipartFile returns an opened file and original filename", func() {
		file, filename, err := CreateMultipartFile("upload.txt", []byte("hello"))
		Expect(err).NotTo(HaveOccurred())
		ginkgo.DeferCleanup(func() {
			Expect(file.Close()).To(Succeed())
		})

		raw, err := io.ReadAll(file)
		Expect(err).NotTo(HaveOccurred())
		Expect(string(raw)).To(Equal("hello"))
		Expect(filename).To(Equal("upload.txt"))
	})

	ginkgo.It("CreateMultipartFile reports writer, reader, empty form, and open errors", func() {
		cases := []struct {
			name      string
			configure func(error)
			wantErr   error
			filename  string
		}{
			{
				name: "create form file",
				configure: func(err error) {
					newMultipartWriter = func(io.Writer) multipartFormWriter {
						return &fakeMultipartWriter{createErr: err}
					}
				},
			},
			{
				name: "write part",
				configure: func(err error) {
					newMultipartWriter = func(io.Writer) multipartFormWriter {
						return &fakeMultipartWriter{part: errorWriter{err: err}}
					}
				},
			},
			{
				name: "close writer",
				configure: func(err error) {
					newMultipartWriter = func(io.Writer) multipartFormWriter {
						return &fakeMultipartWriter{closeErr: err}
					}
				},
			},
			{
				name: "read form",
				configure: func(err error) {
					newMultipartReader = func(io.Reader, string) multipartFormReader {
						return &fakeMultipartReader{err: err}
					}
				},
			},
			{
				name: "empty form",
				configure: func(error) {
					newMultipartReader = func(io.Reader, string) multipartFormReader {
						return &fakeMultipartReader{form: &multipart.Form{File: map[string][]*multipart.FileHeader{}}}
					}
				},
				wantErr: errMultipartFormFileRequired,
			},
			{
				name: "open file",
				configure: func(err error) {
					openMultipartFile = func(*multipart.FileHeader) (multipart.File, error) {
						return nil, err
					}
				},
				filename: "upload.txt",
			},
		}

		for _, tc := range cases {
			tc := tc
			ginkgo.By(tc.name)
			restore := restoreTestUtilsSeams()
			func() {
				defer restore()
				failure := errors.New(tc.name + " failed")
				tc.configure(failure)
				wantErr := failure
				if tc.wantErr != nil {
					wantErr = tc.wantErr
				}

				file, filename, err := CreateMultipartFile("upload.txt", []byte("hello"))

				Expect(file).To(BeNil())
				Expect(filename).To(Equal(tc.filename))
				Expect(err).To(MatchError(wantErr))
			}()
		}
	})

	ginkgo.It("WriteFile creates parent directories and writes content", func() {
		tb := &fakeTestHelper{}
		base := tempTestUtilsDir()

		path := WriteFile(tb, base, "nested/file.txt", "hello")

		Expect(path).To(Equal(filepath.Join(base, "nested", "file.txt")))
		raw, err := os.ReadFile(path)
		Expect(err).NotTo(HaveOccurred())
		Expect(string(raw)).To(Equal("hello"))
		Expect(tb.helperCalls).To(Equal(1))
	})

	ginkgo.It("WriteFile reports mkdir and write failures through the test helper", func() {
		cases := []struct {
			name      string
			configure func(error)
			want      string
		}{
			{
				name: "mkdir",
				configure: func(err error) {
					mkdirAll = func(string, os.FileMode) error { return err }
				},
				want: "mkdir: mkdir failed",
			},
			{
				name: "write",
				configure: func(err error) {
					writeFile = func(string, []byte, os.FileMode) error { return err }
				},
				want: "write: write failed",
			},
		}

		for _, tc := range cases {
			tc := tc
			ginkgo.By(tc.name)
			restore := restoreTestUtilsSeams()
			func() {
				defer restore()
				tc.configure(errors.New(tc.name + " failed"))
				tb := &fakeTestHelper{panicOnFatal: true}

				Expect(func() {
					WriteFile(tb, tempTestUtilsDir(), "file.txt", "hello")
				}).To(PanicWith(ContainSubstring(tc.want)))
			}()
		}
	})

	ginkgo.It("FixturePath returns the first matching fixture directory", func() {
		tb := &fakeTestHelper{}
		base := tempTestUtilsDir()
		Expect(os.MkdirAll(filepath.Join(base, "fixtures", "pages"), 0o755)).To(Succeed())
		restore := restoreTestUtilsSeams()
		ginkgo.DeferCleanup(restore)
		getwd = func() (string, error) {
			return base, nil
		}

		path := FixturePath(tb, "pages", "missing", "fixtures")

		Expect(path).To(Equal(filepath.Join(base, "fixtures", "pages")))
	})

	ginkgo.It("FixturePath reports getwd and missing fixture failures", func() {
		restore := restoreTestUtilsSeams()
		getwd = func() (string, error) {
			return "", errors.New("wd failed")
		}
		Expect(func() {
			FixturePath(&fakeTestHelper{panicOnFatal: true}, "pages", "fixtures")
		}).To(PanicWith(ContainSubstring("getwd: wd failed")))
		restore()

		restore = restoreTestUtilsSeams()
		ginkgo.DeferCleanup(restore)
		getwd = func() (string, error) {
			return "/tmp/wiki", nil
		}
		stat = func(string) (os.FileInfo, error) {
			return nil, os.ErrNotExist
		}
		Expect(func() {
			FixturePath(&fakeTestHelper{panicOnFatal: true}, "pages", "fixtures")
		}).To(PanicWith(ContainSubstring("fixture path not found")))
	})

	ginkgo.It("WrapCloseWithErrorCheck accepts successful closes and fails on close errors", func() {
		tb := &fakeTestHelper{}
		WrapCloseWithErrorCheck(func() error { return nil }, tb)
		Expect(tb.helperCalls).To(Equal(1))

		Expect(func() {
			WrapCloseWithErrorCheck(func() error { return errFixtureCloseFailed }, &fakeTestHelper{panicOnFatal: true})
		}).To(PanicWith(SatisfyAll(
			ContainSubstring(wrapCloseFailurePrefix),
			ContainSubstring(errFixtureCloseFailed.Error()),
		)))
	})
})

func tempTestUtilsDir() string {
	ginkgo.GinkgoHelper()

	dir, err := os.MkdirTemp("", "leafwiki-test-utils-*")
	Expect(err).NotTo(HaveOccurred())
	ginkgo.DeferCleanup(os.RemoveAll, dir)
	return dir
}

type fakeMultipartWriter struct {
	part      io.Writer
	createErr error
	closeErr  error
}

func (w *fakeMultipartWriter) CreateFormFile(string, string) (io.Writer, error) {
	if w.createErr != nil {
		return nil, w.createErr
	}
	if w.part != nil {
		return w.part, nil
	}
	return io.Discard, nil
}

func (w *fakeMultipartWriter) Boundary() string {
	return "boundary"
}

func (w *fakeMultipartWriter) Close() error {
	return w.closeErr
}

type fakeMultipartReader struct {
	form *multipart.Form
	err  error
}

func (r *fakeMultipartReader) ReadForm(int64) (*multipart.Form, error) {
	if r.err != nil {
		return nil, r.err
	}
	return r.form, nil
}

type errorWriter struct {
	err error
}

func (w errorWriter) Write([]byte) (int, error) {
	return 0, w.err
}

type fakeTestHelper struct {
	helperCalls  int
	panicOnFatal bool
}

func (t *fakeTestHelper) Helper() {
	t.helperCalls++
}

func (t *fakeTestHelper) Fatalf(format string, args ...any) {
	message := fmt.Sprintf(format, args...)
	if t.panicOnFatal {
		panic(message)
	}
}

func restoreTestUtilsSeams() func() {
	previousNewMultipartWriter := newMultipartWriter
	previousNewMultipartReader := newMultipartReader
	previousOpenMultipartFile := openMultipartFile
	previousMkdirAll := mkdirAll
	previousWriteFile := writeFile
	previousGetwd := getwd
	previousStat := stat
	return func() {
		newMultipartWriter = previousNewMultipartWriter
		newMultipartReader = previousNewMultipartReader
		openMultipartFile = previousOpenMultipartFile
		mkdirAll = previousMkdirAll
		writeFile = previousWriteFile
		getwd = previousGetwd
		stat = previousStat
	}
}
