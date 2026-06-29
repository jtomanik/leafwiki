package importer_test

import (
	"os"
	"path/filepath"

	ginkgo "github.com/onsi/ginkgo/v2"
)

type importerIntegrationTestT interface {
	Helper()
	TempDir() string
	Fatalf(format string, args ...interface{})
}

func integFixturePathForT(t importerIntegrationTestT, rel string, candidates ...string) string {
	t.Helper()

	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for _, candidate := range candidates {
		abs := filepath.Join(wd, candidate, rel)
		if info, err := os.Stat(abs); err == nil && info.IsDir() {
			return abs
		}
	}

	t.Fatalf("fixture path not found for %q from working directory %q", rel, wd)
	return ""
}

func integWrapCloseWithErrorCheck(closer func() error, t importerIntegrationTestT) {
	t.Helper()
	ginkgo.DeferCleanup(func() {
		if err := closer(); err != nil {
			t.Fatalf("failed to close resource: %v", err)
		}
	})
}
