package tree

import (
	"testing"

	"github.com/perber/wiki/internal/core/identity"
)

func TestParseRoutePathReturnsSemanticRoutePath(t *testing.T) {
	t.Parallel()

	routePath, err := ParseRoutePath("docs/guide")
	if err != nil {
		t.Fatalf("ParseRoutePath: %v", err)
	}
	if routePath != RoutePath("docs/guide") {
		t.Fatalf("RoutePath = %q, want docs/guide", routePath)
	}
	if routePath.String() != "docs/guide" {
		t.Fatalf("RoutePath.String = %q, want docs/guide", routePath.String())
	}
}

func TestSemanticPageValuesKeepDistinctTypes(t *testing.T) {
	t.Parallel()

	pageID := PageID("page-1")
	version := PageVersion("v2")
	slug := Slug("guide")
	markdownPath := MarkdownPath("docs/guide.md")

	if pageID.String() != "page-1" || version.String() != "v2" || slug.String() != "guide" || markdownPath.String() != "docs/guide.md" {
		t.Fatalf("semantic values = %q/%q/%q/%q", pageID, version, slug, markdownPath)
	}
}

func TestPageVersionBypassIsOwnedByTreeOperations(t *testing.T) {
	t.Parallel()

	if !pageVersionUnchecked.IsUnchecked() {
		t.Fatalf("internal bypass sentinel should identify itself as unchecked")
	}
	if got := newFixturePageVersion(versionUnchecked); got.IsUnchecked() || got != "" {
		t.Fatalf("client parser leaked bypass sentinel: got %q", got)
	}
}

func TestTreeServiceVersionBypassUsesConstrainedOperations(t *testing.T) {
	t.Parallel()

	var _ func(*TreeService, UserID, PageID, bool) error = (*TreeService).DeleteNodeUncheckedVersion
	var _ func(*TreeService, UserID, PageID, string, Slug, *string, bool) error = (*TreeService).UpdateNodeUncheckedVersion
	var _ func(*TreeService, UserID, PageID, string, Slug, *string) error = (*TreeService).UpdateNodeReplacingMetadataUncheckedVersion
	var _ func(*TreeService, UserID, PageID, PageID) error = (*TreeService).MoveNodeUncheckedVersion
	var _ func(*TreeService, UserID, PageID, NodeKind) error = (*TreeService).ConvertNodeUncheckedVersion
}

func TestTreeIdentityAliasesNeutralIdentityTypes(t *testing.T) {
	t.Parallel()

	var _ identity.UserID = newFixtureUserID("user-1")
	var _ identity.RevisionID = newFixtureRevisionID("rev-1")
	var _ identity.CommitHash = CommitHash("abc123")
}

func TestWorkspaceSourcePathUsesSemanticType(t *testing.T) {
	t.Parallel()

	sourcePath := WorkspaceSourcePath("Plans/Agent Hooks.PLAN.md")
	if sourcePath.String() != "Plans/Agent Hooks.PLAN.md" {
		t.Fatalf("WorkspaceSourcePath.String = %q", sourcePath.String())
	}

	node := PageNode{WorkspaceSourcePath: sourcePath}
	var _ WorkspaceSourcePath = node.WorkspaceSourcePath

	route := WorkspaceMarkdownRoute{SourcePath: sourcePath}
	var _ WorkspaceSourcePath = route.SourcePath
}

func TestCorePageIdentityUsesSemanticTypes(t *testing.T) {
	t.Parallel()

	node := PageNode{
		ID: PageID("page-1"),
		Metadata: PageMetadata{
			CreatorID:    newFixtureUserID("user-1"),
			LastAuthorID: newFixtureUserID("user-2"),
		},
	}
	var _ PageID = node.ID
	var _ UserID = node.Metadata.CreatorID
	var _ UserID = node.Metadata.LastAuthorID

	var _ func(*PageNode, PageID, bool) bool = (*PageNode).IsChildOf
}

func TestTreeServiceWriteBoundariesUseSemanticTypes(t *testing.T) {
	t.Parallel()

	var _ func(*TreeService, UserID, *PageID, string, Slug, *NodeKind) (*PageID, error) = (*TreeService).CreateNode
	var _ func(*TreeService, UserID, PageID, *PageID, string, Slug, NodeKind, string, PageMetadata) (*Page, error) = (*TreeService).RestoreNode
	var _ func(*TreeService, UserID, []BulkContentUpdate) []error = (*TreeService).BulkUpdateContent

	update := BulkContentUpdate{ID: PageID("page-1"), Content: "content"}
	var _ PageID = update.ID
}

func TestTreeServiceReadBoundariesUseSemanticTypes(t *testing.T) {
	t.Parallel()

	var _ func(*TreeService, PageID) (*Page, error) = (*TreeService).GetPage
	var _ func(*TreeService, PageID) (string, error) = (*TreeService).ReadPageRaw
	var _ func(*TreeService, []PageID) ([]*Page, []error) = (*TreeService).GetPages
	var _ func(*TreeService, PageID) (*PageNode, error) = (*TreeService).FindPageByID
	var _ func(*TreeService, PageID) (*PermalinkTarget, error) = (*TreeService).ResolvePermalinkTarget
	var _ func(*TreeService, RoutePath) (*Page, error) = (*TreeService).FindPageByRoutePath
	var _ func(*TreeService, RoutePath, NodeKind) (*Page, error) = (*TreeService).FindPageByRoutePathAndKind
	var _ func(*TreeService, RoutePath) (*PathLookup, error) = (*TreeService).LookupPagePath
	var _ func(*TreeService, RoutePath, NodeKind) (*PathLookup, error) = (*TreeService).LookupPagePathForKind
}
