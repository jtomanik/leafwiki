package importer

import (
	"os"
	"path/filepath"
)

type importerTestT interface {
	Helper()
	TempDir() string
	Fatal(args ...interface{})
	Fatalf(format string, args ...interface{})
	Error(args ...interface{})
	Errorf(format string, args ...interface{})
}

func importerWriteFile(t importerTestT, base, rel, content string) string {
	t.Helper()
	abs := filepath.Join(base, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(abs, []byte(content), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	return abs
}
