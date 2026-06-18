package wikid

import (
	"path/filepath"
	"testing"
)

func TestGlobalLayoutPaths(t *testing.T) {
	homeDir := filepath.Join(t.TempDir(), ".leafwiki")

	layout := GlobalLayout(homeDir)

	if layout.HomeDir != homeDir {
		t.Fatalf("HomeDir = %q, want %q", layout.HomeDir, homeDir)
	}
	if layout.WikidDir != filepath.Join(homeDir, "wikid") {
		t.Fatalf("WikidDir = %q", layout.WikidDir)
	}
	if layout.RuntimeDir != filepath.Join(homeDir, "runtime") {
		t.Fatalf("RuntimeDir = %q", layout.RuntimeDir)
	}
	if layout.DBPath != filepath.Join(homeDir, "wikid", "wikid.db") {
		t.Fatalf("DBPath = %q", layout.DBPath)
	}
	if layout.HomeRootDir != filepath.Join(homeDir, "root") {
		t.Fatalf("HomeRootDir = %q", layout.HomeRootDir)
	}
}
