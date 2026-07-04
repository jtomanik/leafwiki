package tree

import (
	"encoding/json"
	"errors"
	. "github.com/onsi/gomega"
	"os"
	"syscall"
	"time"

	"github.com/onsi/gomega/gcustom"
	"github.com/onsi/gomega/types"
)

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
