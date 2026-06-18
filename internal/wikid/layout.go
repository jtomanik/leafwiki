package wikid

import (
	"path/filepath"
	"strings"
)

type Layout struct {
	HomeDir     string
	WikidDir    string
	RuntimeDir  string
	DBPath      string
	HomeRootDir string
}

func GlobalLayout(homeDir string) Layout {
	homeDir = filepath.Clean(strings.TrimSpace(homeDir))
	wikidDir := filepath.Join(homeDir, "wikid")
	return Layout{
		HomeDir:     homeDir,
		WikidDir:    wikidDir,
		RuntimeDir:  filepath.Join(homeDir, "runtime"),
		DBPath:      filepath.Join(wikidDir, "wikid.db"),
		HomeRootDir: filepath.Join(homeDir, "root"),
	}
}
