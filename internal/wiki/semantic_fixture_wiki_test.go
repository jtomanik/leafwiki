package wiki

import (
	"os"

	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/perber/wiki/internal/core/tree"
)

func wikiTestTempDir() string {
	ginkgo.GinkgoHelper()

	dir, err := os.MkdirTemp("", "leafwiki-wiki-test-*")
	Expect(err).To(Succeed())
	ginkgo.DeferCleanup(os.RemoveAll, dir)
	return dir
}

func newFixturePageID[T ~string](raw T) tree.PageID {
	return tree.NewPageIDUnchecked(raw)
}

func newFixturePageVersion[T ~string](raw T) tree.PageVersion {
	return tree.NewPageVersionUnchecked(raw)
}

func newFixtureSlug[T ~string](raw T) tree.Slug {
	return tree.NewSlugUnchecked(raw)
}

func newFixtureUserID[T ~string](raw T) tree.UserID {
	return tree.NewUserIDUnchecked(string(raw))
}
