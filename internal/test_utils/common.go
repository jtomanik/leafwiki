package test_utils

import (
	"bytes"
	"errors"
	"io"
	"mime/multipart"
	"os"
	"path/filepath"
)

type testHelper interface {
	Helper()
	Fatalf(format string, args ...any)
}

type multipartFormWriter interface {
	CreateFormFile(fieldname string, filename string) (io.Writer, error)
	Boundary() string
	Close() error
}

type multipartFormReader interface {
	ReadForm(maxMemory int64) (*multipart.Form, error)
}

var (
	errMultipartFormFileRequired = errors.New("no file found in form")
	newMultipartWriter           = func(w io.Writer) multipartFormWriter {
		return multipart.NewWriter(w)
	}
	newMultipartReader = func(r io.Reader, boundary string) multipartFormReader {
		return multipart.NewReader(r, boundary)
	}
	openMultipartFile = func(header *multipart.FileHeader) (multipart.File, error) {
		return header.Open()
	}
	mkdirAll  = os.MkdirAll
	writeFile = os.WriteFile
	getwd     = os.Getwd
	stat      = os.Stat
)

// CreateMultipartFile simulates a real file upload using multipart encoding
func CreateMultipartFile(filename string, content []byte) (multipart.File, string, error) {
	body := &bytes.Buffer{}
	writer := newMultipartWriter(body)

	part, err := writer.CreateFormFile("file", filename)
	if err != nil {
		return nil, "", err
	}
	if _, err := part.Write(content); err != nil {
		return nil, "", err
	}
	if err := writer.Close(); err != nil {
		return nil, "", err
	}

	reader := newMultipartReader(body, writer.Boundary())
	form, err := reader.ReadForm(10 << 20)
	if err != nil {
		return nil, "", err
	}

	files := form.File["file"]
	if len(files) == 0 {
		return nil, "", errMultipartFormFileRequired
	}

	f, err := openMultipartFile(files[0])
	return f, files[0].Filename, err
}

func WriteFile(t testHelper, base, rel, content string) string {
	t.Helper()
	abs := filepath.Join(base, filepath.FromSlash(rel))
	if err := mkdirAll(filepath.Dir(abs), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := writeFile(abs, []byte(content), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	return abs
}

func FixturePath(t testHelper, rel string, candidates ...string) string {
	t.Helper()

	wd, err := getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}

	for _, candidate := range candidates {
		abs := filepath.Join(wd, candidate, rel)
		if info, err := stat(abs); err == nil && info.IsDir() {
			return abs
		}
	}

	t.Fatalf("fixture path not found for %q from working directory %q", rel, wd)
	return ""
}

func WrapCloseWithErrorCheck(closer func() error, t testHelper) {
	t.Helper()
	err := closer()
	if err != nil {
		t.Fatalf("failed to close resource: %v", err)
	}
}
