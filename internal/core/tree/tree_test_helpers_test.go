package tree

import (
	"errors"
	"os"
	"time"

	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/types"
)

type sectionIndexPathLookup struct {
	Path   string
	Exists bool
}

type workspaceContentPathLookup struct {
	Path   string
	Exists bool
	Err    error
}

type contentMatchResult struct {
	Matches bool
	Err     error
}

type legacyContentMissingResult struct {
	Missing bool
	Err     error
}

type directoryEntriesResult struct {
	HasEntries bool
	Err        error
}

type pageLookupResult struct {
	Page *Page
	Err  error
}

func pointToValue[T any](matcher types.GomegaMatcher) types.GomegaMatcher {
	return SatisfyAll(
		Not(BeNil()),
		WithTransform(func(actual *T) T {
			if actual == nil {
				var zero T
				return zero
			}
			return *actual
		}, matcher),
	)
}

func matchErrorAs(target any) types.GomegaMatcher {
	return WithTransform(func(err error) bool {
		return errors.As(err, target)
	}, BeTrue())
}

func matchErrorIs(target error) types.GomegaMatcher {
	return WithTransform(func(err error) bool {
		return errors.Is(err, target)
	}, BeTrue())
}

func matchSectionIndexPath(path string, exists bool) types.GomegaMatcher {
	return SatisfyAll(
		HaveField("Path", Equal(path)),
		HaveField("Exists", Equal(exists)),
	)
}

func matchWorkspaceContentPath(path types.GomegaMatcher, exists bool, err types.GomegaMatcher) types.GomegaMatcher {
	return SatisfyAll(
		HaveField("Path", path),
		HaveField("Exists", Equal(exists)),
		HaveField("Err", err),
	)
}

func matchContentComparison(matches types.GomegaMatcher, err types.GomegaMatcher) types.GomegaMatcher {
	return SatisfyAll(
		HaveField("Matches", matches),
		HaveField("Err", err),
	)
}

func matchLegacyContentMissing(missing types.GomegaMatcher, err types.GomegaMatcher) types.GomegaMatcher {
	return SatisfyAll(
		HaveField("Missing", missing),
		HaveField("Err", err),
	)
}

func matchDirectoryEntries(hasEntries types.GomegaMatcher, err types.GomegaMatcher) types.GomegaMatcher {
	return SatisfyAll(
		HaveField("HasEntries", hasEntries),
		HaveField("Err", err),
	)
}

func matchTreeNode(kind NodeKind, id PageID, title string) types.GomegaMatcher {
	return SatisfyAll(
		HaveField("Kind", Equal(kind)),
		HaveField("ID", Equal(id)),
		HaveField("Title", Equal(title)),
	)
}

func haveParentPageID(id PageID) types.GomegaMatcher {
	return SatisfyAll(
		Not(BeNil()),
		HaveField("ID", Equal(id)),
	)
}

func matchTreeNodePointer(kind NodeKind, id types.GomegaMatcher) types.GomegaMatcher {
	return SatisfyAll(
		Not(BeNil()),
		WithTransform(func(actual *PageNode) PageNode {
			if actual == nil {
				return PageNode{}
			}
			return *actual
		}, SatisfyAll(
			HaveField("Kind", Equal(kind)),
			HaveField("ID", id),
		)),
	)
}

func matchManagedFrontmatter(id PageID, title string) types.GomegaMatcher {
	return SatisfyAll(
		HaveField("LeafWikiID", BeEquivalentTo(id)),
		HaveField("LeafWikiTitle", Equal(title)),
	)
}

func matchFrontmatterTimestamps(createdAt string, updatedAt string) types.GomegaMatcher {
	return SatisfyAll(
		HaveField("LeafWikiCreatedAt", Equal(createdAt)),
		HaveField("LeafWikiUpdatedAt", Equal(updatedAt)),
	)
}

func matchFrontmatterAuthors(creatorID any, lastAuthorID any) types.GomegaMatcher {
	return SatisfyAll(
		HaveField("LeafWikiCreatorID", BeEquivalentTo(creatorID)),
		HaveField("LeafWikiLastAuthorID", BeEquivalentTo(lastAuthorID)),
	)
}

func matchPageMetadataAuthors(creatorID any, lastAuthorID any) types.GomegaMatcher {
	return SatisfyAll(
		HaveField("CreatorID", BeEquivalentTo(creatorID)),
		HaveField("LastAuthorID", BeEquivalentTo(lastAuthorID)),
	)
}

func matchPageMetadataTimestamps(createdAt string, updatedAt string) types.GomegaMatcher {
	return SatisfyAll(
		HaveField("CreatedAt", WithTransform(func(actual time.Time) string {
			return actual.UTC().Format(time.RFC3339)
		}, Equal(createdAt))),
		HaveField("UpdatedAt", WithTransform(func(actual time.Time) string {
			return actual.UTC().Format(time.RFC3339)
		}, Equal(updatedAt))),
	)
}

func matchResolvedNode(kind NodeKind, hasContent types.GomegaMatcher, filePath types.GomegaMatcher) types.GomegaMatcher {
	return SatisfyAll(
		HaveField("Kind", Equal(kind)),
		HaveField("HasContent", hasContent),
		HaveField("FilePath", filePath),
	)
}

func haveChildPageIDs(ids ...PageID) types.GomegaMatcher {
	return HaveField("Children", WithTransform(func(children []*PageNode) []PageID {
		out := make([]PageID, 0, len(children))
		for _, child := range children {
			out = append(out, child.ID)
		}
		return out
	}, Equal(ids)))
}

func haveChildPositions(positions ...int) types.GomegaMatcher {
	return HaveField("Children", WithTransform(func(children []*PageNode) []int {
		out := make([]int, 0, len(children))
		for _, child := range children {
			out = append(out, child.Position)
		}
		return out
	}, Equal(positions)))
}

func matchExistingPathSegment(id PageID) types.GomegaMatcher {
	return SatisfyAll(
		HaveField("Exists", BeTrue()),
		HaveField("ID", WithTransform(func(actual *PageID) PageID {
			if actual == nil {
				return ""
			}
			return *actual
		}, Equal(id))),
	)
}

func matchMissingPathSegment() types.GomegaMatcher {
	return SatisfyAll(
		HaveField("Exists", BeFalse()),
		HaveField("ID", BeNil()),
	)
}

func matchRootSection() types.GomegaMatcher {
	return SatisfyAll(
		Not(BeNil()),
		HaveField("ID", Equal(RootPageID)),
		HaveField("Slug", Equal(newFixtureSlug("root"))),
		HaveField("Title", Equal("root")),
		HaveField("Kind", Equal(NodeKindSection)),
		HaveField("Parent", BeNil()),
		HaveField("Children", BeEmpty()),
	)
}

func tempTreeDir() string {
	ginkgo.GinkgoHelper()
	dir, err := os.MkdirTemp("", "leafwiki-tree-*")
	Expect(err).NotTo(HaveOccurred())
	ginkgo.DeferCleanup(os.RemoveAll, dir)
	return dir
}
