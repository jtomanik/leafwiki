package markdown

import (
	"os"
	"path/filepath"
)

type markdownTestT interface {
	Helper()
	Fatalf(format string, args ...interface{})
}

func writeMarkdownTestFile(t markdownTestT, base string, rel string, content string) string {
	t.Helper()
	path := filepath.Join(base, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	return path
}
