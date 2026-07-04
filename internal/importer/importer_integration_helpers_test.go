package importer_test

import (
	"os"
	"path/filepath"

	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/gcustom"
	"github.com/onsi/gomega/types"
)

func integTempDir() string {
	ginkgo.GinkgoHelper()

	dir, err := os.MkdirTemp("", "leafwiki-importer-integration-*")
	Expect(err).To(Succeed())
	ginkgo.DeferCleanup(os.RemoveAll, dir)
	return dir
}

func integFixturePathForT(rel string, candidates ...string) string {
	ginkgo.GinkgoHelper()

	wd, err := os.Getwd()
	Expect(err).To(Succeed())
	for _, candidate := range candidates {
		abs := filepath.Join(wd, candidate, rel)
		if info, err := os.Stat(abs); err == nil && info.IsDir() {
			return abs
		}
	}

	Expect(candidates).To(ContainElement(matchIntegrationFixtureDirectory(wd, rel)), "fixture path %q should exist below one candidate from %q", rel, wd)
	return filepath.Join(wd, candidates[0], rel)
}

func matchIntegrationFixtureDirectory(wd string, rel string) types.GomegaMatcher {
	return gcustom.MakeMatcher(func(candidate string) (bool, error) {
		abs := filepath.Join(wd, candidate, rel)
		info, err := os.Stat(abs)
		return err == nil && info.IsDir(), nil
	}).WithMessage("resolve an importer integration fixture directory")
}

func integWrapCloseWithErrorCheck(closer func() error) {
	ginkgo.GinkgoHelper()

	ginkgo.DeferCleanup(func() {
		Expect(closer()).To(Succeed())
	})
}
