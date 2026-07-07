package tree

import (
	"errors"

	ginkgo "github.com/onsi/ginkgo/v2"
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

type asciiFoldMatchState string

const (
	asciiFoldMatched    asciiFoldMatchState = "ASCII fold match"
	asciiFoldMismatched asciiFoldMatchState = "ASCII fold mismatch"
)

type workspaceIndexFileState string

const (
	workspaceIndexFilePresent workspaceIndexFileState = "workspace index file present"
	workspaceIndexFileAbsent  workspaceIndexFileState = "workspace index file absent"
)

type workspaceSectionRouteMatchState string

const (
	workspaceSectionRouteMatched    workspaceSectionRouteMatchState = "same workspace section route"
	workspaceSectionRouteMismatched workspaceSectionRouteMatchState = "different workspace section route"
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

func observeASCIIFoldMatch(left string, right string) asciiFoldMatchState {
	if equalFoldASCII(left, right) {
		return asciiFoldMatched
	}
	return asciiFoldMismatched
}

func observeWorkspaceIndexFile(dirPath string) workspaceIndexFileState {
	if workspaceDirHasIndexFile(dirPath) {
		return workspaceIndexFilePresent
	}
	return workspaceIndexFileAbsent
}

func observeWorkspaceSectionRouteMatch(left WorkspaceMarkdownRoute, right WorkspaceMarkdownRoute) workspaceSectionRouteMatchState {
	if sameWorkspaceSectionRouteEntry(left, right) {
		return workspaceSectionRouteMatched
	}
	return workspaceSectionRouteMismatched
}

func observeDirectoryFlag(isDir bool) fileInfoKind {
	if isDir {
		return fileInfoDirectory
	}
	return fileInfoFile
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
