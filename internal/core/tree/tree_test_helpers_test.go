package tree

import (
	"encoding/json"
	"errors"
	"os"
	"syscall"
	"time"

	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/gcustom"
	"github.com/onsi/gomega/types"
	"github.com/perber/wiki/internal/core/markdown"
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

var errExpectedFrontmatter = errors.New("expected frontmatter")
var errExpectedSectionIndex = errors.New("expected section index")
var errExpectedContentMatch = errors.New("expected content match")
var errExpectedContentDifference = errors.New("expected content difference")
var errExpectedLegacyContentMissing = errors.New("expected legacy content missing")
var errExpectedLegacyContentPresent = errors.New("expected legacy content present")
var errExpectedDirectoryEntries = errors.New("expected directory entries")
var errExpectedDirectoryEmpty = errors.New("expected directory to be empty")
var errExpectedCleanPathMatch = errors.New("expected clean paths to match")
var errExpectedCleanPathDifference = errors.New("expected clean paths to differ")

func parseRequiredFrontmatter(raw string) (markdown.Frontmatter, string, error) {
	ginkgo.GinkgoHelper()
	frontmatter, body, hasFrontmatter, err := markdown.ParseFrontmatter(raw)
	if err != nil {
		return markdown.Frontmatter{}, body, err
	}
	if !hasFrontmatter {
		return markdown.Frontmatter{}, body, errExpectedFrontmatter
	}
	return frontmatter, body, nil
}

func sectionIndexPathInDirResult(store *NodeStore, dirPath string) (string, error) {
	ginkgo.GinkgoHelper()
	path, exists, err := store.sectionIndexPathInDir(dirPath)
	if err != nil {
		return path, err
	}
	if !exists {
		return path, errExpectedSectionIndex
	}
	return path, nil
}

func directoryFileContentsMatch(sourceDir string, targetDir string) error {
	ginkgo.GinkgoHelper()
	matches, err := directoryFileContentMatches(sourceDir, targetDir)
	if err != nil {
		return err
	}
	if !matches {
		return errExpectedContentMatch
	}
	return nil
}

func directoryFileContentsDiffer(sourceDir string, targetDir string) error {
	ginkgo.GinkgoHelper()
	matches, err := directoryFileContentMatches(sourceDir, targetDir)
	if err != nil {
		return err
	}
	if matches {
		return errExpectedContentDifference
	}
	return nil
}

func filesHaveMatchingContent(sourceFile string, targetFile string) error {
	ginkgo.GinkgoHelper()
	matches, err := filesHaveSameContent(sourceFile, targetFile)
	if err != nil {
		return err
	}
	if !matches {
		return errExpectedContentMatch
	}
	return nil
}

func filesHaveDifferentContent(sourceFile string, targetFile string) error {
	ginkgo.GinkgoHelper()
	matches, err := filesHaveSameContent(sourceFile, targetFile)
	if err != nil {
		return err
	}
	if matches {
		return errExpectedContentDifference
	}
	return nil
}

func directoryHasEntriesResult(path string) error {
	ginkgo.GinkgoHelper()
	hasEntries, err := directoryHasEntries(path)
	if err != nil {
		return err
	}
	if !hasEntries {
		return errExpectedDirectoryEntries
	}
	return nil
}

func directoryHasNoEntriesResult(path string) error {
	ginkgo.GinkgoHelper()
	hasEntries, err := directoryHasEntries(path)
	if err != nil {
		return err
	}
	if hasEntries {
		return errExpectedDirectoryEmpty
	}
	return nil
}

func configuredRootMissingLegacyContentResult(svc *TreeService, legacy *PageNode) error {
	ginkgo.GinkgoHelper()
	missing, err := svc.configuredRootMissingLegacyContent(legacy)
	if err != nil {
		return err
	}
	if !missing {
		return errExpectedLegacyContentMissing
	}
	return nil
}

func configuredRootHasLegacyContentResult(svc *TreeService, legacy *PageNode) error {
	ginkgo.GinkgoHelper()
	missing, err := svc.configuredRootMissingLegacyContent(legacy)
	if err != nil {
		return err
	}
	if missing {
		return errExpectedLegacyContentPresent
	}
	return nil
}

func legacyTargetMatchesNodeResult(path legacyContentPath) error {
	ginkgo.GinkgoHelper()
	matches, err := legacyTargetMatchesNode(path)
	if err != nil {
		return err
	}
	if !matches {
		return errExpectedContentMatch
	}
	return nil
}

func legacyTargetDiffersFromNodeResult(path legacyContentPath) error {
	ginkgo.GinkgoHelper()
	matches, err := legacyTargetMatchesNode(path)
	if err != nil {
		return err
	}
	if matches {
		return errExpectedContentDifference
	}
	return nil
}

func cleanPathsMatch(pathA string, pathB string) error {
	ginkgo.GinkgoHelper()
	if !sameCleanPath(pathA, pathB) {
		return errExpectedCleanPathMatch
	}
	return nil
}

func cleanPathsDiffer(pathA string, pathB string) error {
	ginkgo.GinkgoHelper()
	if sameCleanPath(pathA, pathB) {
		return errExpectedCleanPathDifference
	}
	return nil
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
	return gcustom.MakeMatcher(func(err error) (bool, error) {
		return errors.As(err, target), nil
	}).WithMessage("match error type")
}

func matchErrorIs(target error) types.GomegaMatcher {
	return gcustom.MakeMatcher(func(err error) (bool, error) {
		return errors.Is(err, target), nil
	}).WithMessage("match error sentinel")
}

func matchPathError() types.GomegaMatcher {
	return matchErrorAs(new(*os.PathError))
}

func matchJSONSyntaxError() types.GomegaMatcher {
	return matchErrorAs(new(*json.SyntaxError))
}

func matchSymlinkLoopError() types.GomegaMatcher {
	return matchErrorIs(syscall.ELOOP)
}

func matchExistingSectionIndexPath(path string) types.GomegaMatcher {
	return gcustom.MakeMatcher(func(actual sectionIndexPathLookup) (bool, error) {
		return actual.Path == path && actual.Exists, nil
	}).WithMessage("resolve an existing section index path")
}

func matchMissingSectionIndexPath(path string) types.GomegaMatcher {
	return gcustom.MakeMatcher(func(actual sectionIndexPathLookup) (bool, error) {
		return actual.Path == path && !actual.Exists, nil
	}).WithMessage("resolve a missing section index path")
}

func matchExistingWorkspaceContentPath(path types.GomegaMatcher, err types.GomegaMatcher) types.GomegaMatcher {
	return gcustom.MakeMatcher(func(actual workspaceContentPathLookup) (bool, error) {
		pathOK, pathErr := path.Match(actual.Path)
		if pathErr != nil || !pathOK {
			return false, pathErr
		}
		errOK, matchErr := err.Match(actual.Err)
		if matchErr != nil || !errOK {
			return false, matchErr
		}
		return actual.Exists, nil
	}).WithMessage("resolve an existing workspace content path")
}

func matchMissingWorkspaceContentPath(path types.GomegaMatcher, err types.GomegaMatcher) types.GomegaMatcher {
	return gcustom.MakeMatcher(func(actual workspaceContentPathLookup) (bool, error) {
		pathOK, pathErr := path.Match(actual.Path)
		if pathErr != nil || !pathOK {
			return false, pathErr
		}
		errOK, matchErr := err.Match(actual.Err)
		if matchErr != nil || !errOK {
			return false, matchErr
		}
		return !actual.Exists, nil
	}).WithMessage("resolve a missing workspace content path")
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

func matchResolvedNode(kind NodeKind, hasContent bool, filePath types.GomegaMatcher) types.GomegaMatcher {
	return gcustom.MakeMatcher(func(actual *ResolvedNode) (bool, error) {
		if actual == nil || actual.Kind != kind || actual.HasContent != hasContent {
			return false, nil
		}
		if filePath == nil {
			return true, nil
		}
		return filePath.Match(actual.FilePath)
	}).WithMessage("describe resolved tree node storage")
}

func matchSkippedWorkspaceMarkdownRoute(reason string) types.GomegaMatcher {
	return gcustom.MakeMatcher(func(actual WorkspaceMarkdownRoute) (bool, error) {
		return actual.Skip && actual.SkipReason == reason, nil
	}).WithMessage("skip workspace markdown route")
}

func matchMissingPathLookup(segments types.GomegaMatcher) types.GomegaMatcher {
	return gcustom.MakeMatcher(func(actual *PathLookup) (bool, error) {
		if actual == nil || actual.Exists {
			return false, nil
		}
		return segments.Match(actual.Segments)
	}).WithMessage("describe missing path lookup")
}

func matchExistingPathLookup(segments types.GomegaMatcher) types.GomegaMatcher {
	return gcustom.MakeMatcher(func(actual *PathLookup) (bool, error) {
		if actual == nil || !actual.Exists {
			return false, nil
		}
		return segments.Match(actual.Segments)
	}).WithMessage("describe existing path lookup")
}

func matchExistingEnsurePathResult(page types.GomegaMatcher) types.GomegaMatcher {
	return gcustom.MakeMatcher(func(actual *EnsurePathResult) (bool, error) {
		if actual == nil || !actual.Exists || len(actual.Created) != 0 {
			return false, nil
		}
		return page.Match(actual.Page)
	}).WithMessage("return an existing ensured path without creating nodes")
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
	return gcustom.MakeMatcher(func(segment PathSegment) (bool, error) {
		return segment.Exists &&
			segment.ID != nil &&
			*segment.ID == id, nil
	}).WithMessage("describe an existing path segment")
}

func matchMissingPathSegment() types.GomegaMatcher {
	return gcustom.MakeMatcher(func(segment PathSegment) (bool, error) {
		return !segment.Exists && segment.ID == nil, nil
	}).WithMessage("describe a missing path segment")
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
