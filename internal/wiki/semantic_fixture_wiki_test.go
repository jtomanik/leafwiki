package wiki

import (
	"os"

	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/perber/wiki/internal/core/tree"
	"github.com/perber/wiki/internal/workspaceid"
	"github.com/perber/wiki/internal/workspacesync"
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

func newFixtureRevisionID[T ~string](raw T) tree.RevisionID {
	return tree.NewRevisionIDUnchecked(string(raw))
}

func newFixtureUserID[T ~string](raw T) tree.UserID {
	return tree.NewUserIDUnchecked(string(raw))
}

func newFixtureSlug[T ~string](raw T) tree.Slug {
	return tree.NewSlugUnchecked(raw)
}

func newFixtureRoutePath[T ~string](raw T) tree.RoutePath {
	return tree.NewRoutePathUnchecked(string(raw))
}

func newFixtureAssetName[T ~string](raw T) tree.AssetName {
	return tree.AssetNameFromString(raw)
}

func newFixtureWorkspaceID[T ~string](raw T) workspaceid.WorkspaceID {
	return workspaceid.WorkspaceID(raw)
}

func newFixtureCommitHash[T ~string](raw T) workspacesync.CommitHash {
	return workspacesync.CommitHashFromString(raw)
}
