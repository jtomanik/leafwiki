package gitrevisions

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func internalGitDir(dataDir string) string {
	return filepath.Join(dataDir, ".leafwiki", "git")
}

func removeInternalRootGitFile(rootDir string, internalGitDir string) error {
	gitPath := filepath.Join(rootDir, ".git")
	info, err := gitRevisionLstat(gitPath)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("stat root .git: %w", err)
	}
	if info.IsDir() {
		return nil
	}
	raw, err := gitRevisionReadFile(gitPath)
	if err != nil {
		return fmt.Errorf("read root .git file: %w", err)
	}
	target, ok := parseGitDirFile(string(raw), rootDir)
	if !ok || !sameFilesystemPath(target, internalGitDir) {
		return nil
	}
	if err := gitRevisionRemove(gitPath); err != nil {
		return fmt.Errorf("remove root .git file: %w", err)
	}
	return nil
}

func parseGitDirFile(raw string, rootDir string) (string, bool) {
	line := strings.TrimSpace(strings.Split(raw, "\n")[0])
	target, ok := strings.CutPrefix(line, "gitdir:")
	if !ok {
		return "", false
	}
	target = strings.TrimSpace(target)
	if target == "" {
		return "", false
	}
	if filepath.IsAbs(target) {
		return filepath.Clean(target), true
	}
	return filepath.Clean(filepath.Join(rootDir, target)), true
}

func sameFilesystemPath(a string, b string) bool {
	absA, errA := gitRevisionAbs(a)
	absB, errB := gitRevisionAbs(b)
	if errA == nil {
		a = absA
	}
	if errB == nil {
		b = absB
	}
	return filepath.Clean(a) == filepath.Clean(b)
}
