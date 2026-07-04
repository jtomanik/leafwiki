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

type treeErrorRecognition string

const (
	treeErrorRecognized   treeErrorRecognition = "recognized"
	treeErrorUnrecognized treeErrorRecognition = "unrecognized"
)

type treeLookupState string

const (
	treeLookupExisting    treeLookupState = "existing"
	treeLookupMissing     treeLookupState = "missing"
	treeLookupUnavailable treeLookupState = "unavailable"
)

type sectionIndexPathObservation struct {
	Path  string
	State treeLookupState
}

type workspaceContentPathObservation struct {
	Path  string
	State treeLookupState
	Err   error
}

type workspaceMarkdownRouteState string

const (
	workspaceMarkdownRouteSkipped workspaceMarkdownRouteState = "skipped"
	workspaceMarkdownRouteActive  workspaceMarkdownRouteState = "active"
)

type workspaceMarkdownRouteObservation struct {
	State  workspaceMarkdownRouteState
	Reason string
}

type pathLookupObservation struct {
	State    treeLookupState
	Segments []PathSegment
}

type ensurePathResultState string

const (
	ensurePathResultExistingUnchanged ensurePathResultState = "existing without created nodes"
	ensurePathResultExistingCreated   ensurePathResultState = "existing with created nodes"
	ensurePathResultMissing           ensurePathResultState = "missing"
	ensurePathResultUnavailable       ensurePathResultState = "unavailable"
)

type ensurePathResultObservation struct {
	State ensurePathResultState
	Page  *PageNode
}

type pathSegmentState string

const (
	pathSegmentExisting pathSegmentState = "existing"
	pathSegmentMissing  pathSegmentState = "missing"
)

type pathSegmentObservation struct {
	State pathSegmentState
	ID    PageID
}

type pageVersionBypassState string

const (
	pageVersionBypassUnchecked pageVersionBypassState = "unchecked bypass sentinel"
	pageVersionNormal          pageVersionBypassState = "normal page version"
)

type markdownPathRole string

const (
	markdownPathIndexFile markdownPathRole = "index markdown file"
	markdownPathLeafFile  markdownPathRole = "leaf markdown file"
)

type markdownPathFormat string

const (
	markdownPathFormatMarkdown markdownPathFormat = "markdown"
	markdownPathFormatOther    markdownPathFormat = "non-markdown"
)

type markdownPathObservation struct {
	Role   markdownPathRole
	Format markdownPathFormat
}

type routePathKind string

const (
	routePathRoot routePathKind = "root route"
	routePathLeaf routePathKind = "leaf route"
)

type missingContentErrorState string

const (
	missingContentErrorRecognized   missingContentErrorState = "recognized missing content error"
	missingContentErrorUnrecognized missingContentErrorState = "unrecognized content error"
)

type childSlugState string

const (
	childSlugAvailable childSlugState = "child slug available"
	childSlugTaken     childSlugState = "child slug taken"
)

type childMembershipState string

const (
	childMembershipPresent childMembershipState = "child membership present"
	childMembershipAbsent  childMembershipState = "child membership absent"
)

type childMembershipTraversal string

const (
	childMembershipDirect    childMembershipTraversal = "direct child"
	childMembershipRecursive childMembershipTraversal = "recursive descendant"
)

type fileInfoKind string

const (
	fileInfoDirectory fileInfoKind = "directory"
	fileInfoFile      fileInfoKind = "file"
)

type treeServiceLoadState string

const (
	treeServiceLoaded    treeServiceLoadState = "loaded"
	treeServiceNotLoaded treeServiceLoadState = "not loaded"
)

type treeServiceLoadObservation struct {
	Reported treeServiceLoadState
	Root     treeServiceLoadState
}

type treeServicePageState string

const (
	treeServicePagesUnavailable treeServicePageState = "pages unavailable before load"
	treeServicePagesEmpty       treeServicePageState = "loaded tree has no pages"
	treeServicePagesPresent     treeServicePageState = "loaded tree has pages"
)

type treeServicePageObservation struct {
	Reported treeServicePageState
	Root     treeServicePageState
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

func sectionIndexPathObservationFrom(actual sectionIndexPathLookup) sectionIndexPathObservation {
	state := treeLookupMissing
	if actual.Exists {
		state = treeLookupExisting
	}
	return sectionIndexPathObservation{
		Path:  actual.Path,
		State: state,
	}
}

func workspaceContentPathObservationFrom(actual workspaceContentPathLookup) workspaceContentPathObservation {
	state := treeLookupMissing
	if actual.Exists {
		state = treeLookupExisting
	}
	return workspaceContentPathObservation{
		Path:  actual.Path,
		State: state,
		Err:   actual.Err,
	}
}

func workspaceMarkdownRouteObservationFrom(actual WorkspaceMarkdownRoute) workspaceMarkdownRouteObservation {
	if actual.Skip {
		return workspaceMarkdownRouteObservation{
			State:  workspaceMarkdownRouteSkipped,
			Reason: actual.SkipReason,
		}
	}
	return workspaceMarkdownRouteObservation{State: workspaceMarkdownRouteActive}
}

func pathLookupObservationFrom(actual *PathLookup) pathLookupObservation {
	if actual == nil {
		return pathLookupObservation{State: treeLookupUnavailable}
	}
	state := treeLookupMissing
	if actual.Exists {
		state = treeLookupExisting
	}
	return pathLookupObservation{
		State:    state,
		Segments: actual.Segments,
	}
}

func ensurePathResultObservationFrom(actual *EnsurePathResult) ensurePathResultObservation {
	if actual == nil {
		return ensurePathResultObservation{State: ensurePathResultUnavailable}
	}
	if !actual.Exists {
		return ensurePathResultObservation{State: ensurePathResultMissing, Page: actual.Page}
	}
	if len(actual.Created) == 0 {
		return ensurePathResultObservation{State: ensurePathResultExistingUnchanged, Page: actual.Page}
	}
	return ensurePathResultObservation{State: ensurePathResultExistingCreated, Page: actual.Page}
}

func pathSegmentObservationFrom(segment PathSegment) pathSegmentObservation {
	if segment.Exists && segment.ID != nil {
		return pathSegmentObservation{
			State: pathSegmentExisting,
			ID:    *segment.ID,
		}
	}
	return pathSegmentObservation{State: pathSegmentMissing}
}

func pageVersionBypassStateFrom(version PageVersion) pageVersionBypassState {
	if version.IsUnchecked() {
		return pageVersionBypassUnchecked
	}
	return pageVersionNormal
}

func matchPageVersionBypassState(state pageVersionBypassState) types.GomegaMatcher {
	return WithTransform(pageVersionBypassStateFrom, Equal(state))
}

func markdownPathObservationFrom(path MarkdownPath) markdownPathObservation {
	role := markdownPathLeafFile
	if path.IsIndexFile() {
		role = markdownPathIndexFile
	}
	format := markdownPathFormatOther
	if path.IsMarkdown() {
		format = markdownPathFormatMarkdown
	}
	return markdownPathObservation{
		Role:   role,
		Format: format,
	}
}

func matchMarkdownPathSemantics(role markdownPathRole, format markdownPathFormat) types.GomegaMatcher {
	return WithTransform(markdownPathObservationFrom, Equal(markdownPathObservation{
		Role:   role,
		Format: format,
	}))
}

func routePathKindFrom(path RoutePath) routePathKind {
	if path.IsRoot() {
		return routePathRoot
	}
	return routePathLeaf
}

func matchRoutePathKind(kind routePathKind) types.GomegaMatcher {
	return WithTransform(routePathKindFrom, Equal(kind))
}

func matchMissingContentErrorState(isMissing func(error) bool, state missingContentErrorState) types.GomegaMatcher {
	return WithTransform(func(err error) missingContentErrorState {
		if isMissing(err) {
			return missingContentErrorRecognized
		}
		return missingContentErrorUnrecognized
	}, Equal(state))
}

func haveChildSlugState(slug Slug, state childSlugState) types.GomegaMatcher {
	return WithTransform(func(node *PageNode) childSlugState {
		if node != nil && node.ChildAlreadyExists(slug) {
			return childSlugTaken
		}
		return childSlugAvailable
	}, Equal(state))
}

func haveChildMembership(childID PageID, traversal childMembershipTraversal, state childMembershipState) types.GomegaMatcher {
	return WithTransform(func(node *PageNode) childMembershipState {
		recursive := traversal == childMembershipRecursive
		if node != nil && node.IsChildOf(childID, recursive) {
			return childMembershipPresent
		}
		return childMembershipAbsent
	}, Equal(state))
}

func matchFileInfoKind(kind fileInfoKind) types.GomegaMatcher {
	return WithTransform(func(info os.FileInfo) fileInfoKind {
		if info != nil && info.IsDir() {
			return fileInfoDirectory
		}
		return fileInfoFile
	}, Equal(kind))
}

type metadataTimestampState string

const (
	metadataTimestampRecorded metadataTimestampState = "recorded"
	metadataTimestampMissing  metadataTimestampState = "missing"
)

func metadataTimestampStateFrom(actual time.Time) metadataTimestampState {
	if actual.IsZero() {
		return metadataTimestampMissing
	}
	return metadataTimestampRecorded
}

func haveRecordedMetadataTimestamps() types.GomegaMatcher {
	return SatisfyAll(
		HaveField("CreatedAt", WithTransform(metadataTimestampStateFrom, Equal(metadataTimestampRecorded))),
		HaveField("UpdatedAt", WithTransform(metadataTimestampStateFrom, Equal(metadataTimestampRecorded))),
	)
}

func haveRecordedCreatedAt() types.GomegaMatcher {
	return HaveField("CreatedAt", WithTransform(metadataTimestampStateFrom, Equal(metadataTimestampRecorded)))
}

func matchReconstructedNode(kind NodeKind, id types.GomegaMatcher) types.GomegaMatcher {
	matchers := []types.GomegaMatcher{
		HaveField("Kind", Equal(kind)),
		HaveField("Metadata", haveRecordedCreatedAt()),
	}
	if id != nil {
		matchers = append(matchers, HaveField("ID", id))
	}

	return SatisfyAll(
		Not(BeNil()),
		WithTransform(func(actual *PageNode) PageNode {
			if actual == nil {
				return PageNode{}
			}
			return *actual
		}, SatisfyAll(matchers...)),
	)
}

func treeServiceLoadObservationFrom(svc *TreeService) treeServiceLoadObservation {
	if svc == nil {
		return treeServiceLoadObservation{
			Reported: treeServiceNotLoaded,
			Root:     treeServiceNotLoaded,
		}
	}
	reported := treeServiceNotLoaded
	if svc.IsLoaded() {
		reported = treeServiceLoaded
	}
	root := treeServiceNotLoaded
	if svc.GetTree() != nil {
		root = treeServiceLoaded
	}
	return treeServiceLoadObservation{
		Reported: reported,
		Root:     root,
	}
}

func haveTreeServiceLoadState(state treeServiceLoadState) types.GomegaMatcher {
	return WithTransform(treeServiceLoadObservationFrom, Equal(treeServiceLoadObservation{
		Reported: state,
		Root:     state,
	}))
}

func treeServicePageObservationFrom(svc *TreeService) treeServicePageObservation {
	if svc == nil || !svc.IsLoaded() {
		return treeServicePageObservation{
			Reported: treeServicePagesUnavailable,
			Root:     treeServicePagesUnavailable,
		}
	}
	reported := treeServicePagesEmpty
	if svc.HasPages() {
		reported = treeServicePagesPresent
	}
	root := treeServicePagesEmpty
	if tree := svc.GetTree(); tree != nil && len(tree.Children) > 0 {
		root = treeServicePagesPresent
	}
	return treeServicePageObservation{
		Reported: reported,
		Root:     root,
	}
}

func haveTreeServicePageState(state treeServicePageState) types.GomegaMatcher {
	return WithTransform(treeServicePageObservationFrom, Equal(treeServicePageObservation{
		Reported: state,
		Root:     state,
	}))
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
	return WithTransform(func(err error) treeErrorRecognition {
		if errors.As(err, target) {
			return treeErrorRecognized
		}
		return treeErrorUnrecognized
	}, Equal(treeErrorRecognized))
}

func matchErrorIs(target error) types.GomegaMatcher {
	return WithTransform(func(err error) treeErrorRecognition {
		if errors.Is(err, target) {
			return treeErrorRecognized
		}
		return treeErrorUnrecognized
	}, Equal(treeErrorRecognized))
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
	return WithTransform(sectionIndexPathObservationFrom, Equal(sectionIndexPathObservation{
		Path:  path,
		State: treeLookupExisting,
	}))
}

func matchMissingSectionIndexPath(path string) types.GomegaMatcher {
	return WithTransform(sectionIndexPathObservationFrom, Equal(sectionIndexPathObservation{
		Path:  path,
		State: treeLookupMissing,
	}))
}

func matchExistingWorkspaceContentPath(path types.GomegaMatcher, err types.GomegaMatcher) types.GomegaMatcher {
	return WithTransform(workspaceContentPathObservationFrom, SatisfyAll(
		HaveField("Path", path),
		HaveField("State", Equal(treeLookupExisting)),
		HaveField("Err", err),
	))
}

func matchMissingWorkspaceContentPath(path types.GomegaMatcher, err types.GomegaMatcher) types.GomegaMatcher {
	return WithTransform(workspaceContentPathObservationFrom, SatisfyAll(
		HaveField("Path", path),
		HaveField("State", Equal(treeLookupMissing)),
		HaveField("Err", err),
	))
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
	return WithTransform(workspaceMarkdownRouteObservationFrom, Equal(workspaceMarkdownRouteObservation{
		State:  workspaceMarkdownRouteSkipped,
		Reason: reason,
	}))
}

func matchMissingPathLookup(segments types.GomegaMatcher) types.GomegaMatcher {
	return WithTransform(pathLookupObservationFrom, SatisfyAll(
		HaveField("State", Equal(treeLookupMissing)),
		HaveField("Segments", segments),
	))
}

func matchExistingPathLookup(segments types.GomegaMatcher) types.GomegaMatcher {
	return WithTransform(pathLookupObservationFrom, SatisfyAll(
		HaveField("State", Equal(treeLookupExisting)),
		HaveField("Segments", segments),
	))
}

func matchExistingEnsurePathResult(page types.GomegaMatcher) types.GomegaMatcher {
	return WithTransform(ensurePathResultObservationFrom, SatisfyAll(
		HaveField("State", Equal(ensurePathResultExistingUnchanged)),
		HaveField("Page", page),
	))
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
	return WithTransform(pathSegmentObservationFrom, Equal(pathSegmentObservation{
		State: pathSegmentExisting,
		ID:    id,
	}))
}

func matchMissingPathSegment() types.GomegaMatcher {
	return WithTransform(pathSegmentObservationFrom, Equal(pathSegmentObservation{
		State: pathSegmentMissing,
	}))
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
