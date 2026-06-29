package markdownvalidation

import (
	ginkgo "github.com/onsi/ginkgo/v2"
	"os"
	"path/filepath"

	"github.com/perber/wiki/internal/core/markdownlinks"
	"github.com/perber/wiki/internal/core/tree"
)

func issuePathString(issue Issue) string {
	if issue.SourcePath != "" {
		return issue.SourcePath.FilesystemPath()
	}
	return issue.RoutePath.FilesystemPath()
}

var _ = ginkgo.Describe("semantic types", func() {
	ginkgo.It("TestContentValidationOptionsUseSemanticPageIDs", func() {
		t := ginkgo.GinkgoT()
		opts := ContentValidationOptions{
			ExistingPageID: newFixturePageID("page-1"),
			ResolvePageID: func(tree.RoutePath) (tree.PageID, bool) {
				return newFixturePageID("page-2"), true
			},
			ResolveLinkPageID: func(tree.RoutePath) (tree.PageID, bool) {
				return newFixturePageID("page-3"), true
			},
			ResolveLinkTarget: func(tree.RoutePath) (tree.PageID, tree.NodeKind, bool) {
				return newFixturePageID("page-4"), tree.NodeKindPage, true
			},
			ResolveMarkdownLink: func(string) (tree.PageID, tree.NodeKind, bool, IssueCode) {
				return newFixturePageID("page-5"), tree.NodeKindPage, true, ""
			},
			PageIDExists: func(tree.PageID) bool {
				return true
			},
		}
		var _ tree.PageID = opts.ExistingPageID

		workspaceOpts := WorkspaceMarkdownValidationOptions{
			PageIDExists: func(tree.PageID) bool {
				return true
			},
			AssetExists: func(tree.PageID, string) bool {
				return true
			},
		}
		if opts.ResolvePageID == nil || workspaceOpts.PageIDExists == nil || workspaceOpts.AssetExists == nil {
			t.Fatal("typed page ID callbacks were not assigned")
		}
	})

	ginkgo.It("TestIssueCarriesSemanticPathsAndPageIDs", func() {
		t := ginkgo.GinkgoT()

		sourceIssue := Issue{
			Severity:     IssueSeverityError,
			Code:         IssueCodeBrokenLink,
			SourcePath:   tree.MarkdownPath("docs/source.md"),
			RoutePath:    tree.RoutePath("docs/source"),
			PageID:       newFixturePageID("source-page-id"),
			TargetPageID: newFixturePageID("target-page-id"),
			Message:      "broken link",
		}

		var _ tree.MarkdownPath = sourceIssue.SourcePath
		var _ tree.RoutePath = sourceIssue.RoutePath
		var _ tree.PageID = sourceIssue.PageID
		var _ tree.PageID = sourceIssue.TargetPageID

		if issuePathString(sourceIssue) != "docs/source.md" {
			t.Fatalf("source issue path = %q, want docs/source.md", issuePathString(sourceIssue))
		}

		routeIssue := Issue{RoutePath: tree.RoutePath("docs/source"), PageID: newFixturePageID("source-page-id")}
		if issuePathString(routeIssue) != "docs/source" {
			t.Fatalf("route issue path = %q, want docs/source", issuePathString(routeIssue))
		}
	})

	ginkgo.It("TestWorkspaceMarkdownLinkRouteMappingReturnsSemanticPageID", func() {
		t := ginkgo.GinkgoT()
		filesByRoute := map[workspaceValidationRouteKey]tree.PageID{
			workspaceValidationRouteConflictKey(tree.RoutePath("docs/target"), tree.NodeKindPage):     newFixturePageID("target-page-id"),
			workspaceValidationRouteConflictKey(tree.RoutePath("docs/section"), tree.NodeKindSection): newFixturePageID("section-page-id"),
		}

		pageID, ok := workspaceLinkPageIDForRoute(filesByRoute, tree.RoutePath("docs/target"), tree.NodeKindPage)
		if !ok || pageID != newFixturePageID("target-page-id") {
			t.Fatalf("page route mapped to %q, ok=%v; want target-page-id", pageID, ok)
		}
		sectionID, ok := workspaceLinkPageIDForRoute(filesByRoute, tree.RoutePath("docs/section"), tree.NodeKindSection)
		if !ok || sectionID != newFixturePageID("section-page-id") {
			t.Fatalf("section route mapped to %q, ok=%v; want section-page-id", sectionID, ok)
		}
		missingID, ok := workspaceLinkPageIDForRoute(filesByRoute, tree.RoutePath("docs/missing"), tree.NodeKindPage)
		if ok || missingID != "" {
			t.Fatalf("missing route mapped to %q, ok=%v; want no page ID", missingID, ok)
		}
	})

	ginkgo.It("TestWorkspaceMarkdownLinkResolverReturnsMetadataPageIDs", func() {
		t := ginkgo.GinkgoT()
		rootDir := t.TempDir()
		if err := os.MkdirAll(filepath.Join(rootDir, "docs", "sync"), 0o755); err != nil {
			t.Fatalf("create workspace dirs: %v", err)
		}
		if err := os.WriteFile(filepath.Join(rootDir, "docs", "source.md"), canonicalValidationMarkdown("source-page-id", "Source", "# Source\n\n[Target](/docs/target.md)\n[Sync](/docs/sync)\n"), 0o644); err != nil {
			t.Fatalf("write source page: %v", err)
		}
		if err := os.WriteFile(filepath.Join(rootDir, "docs", "target.md"), canonicalValidationMarkdown("target-page-id", "Target", "# Target\n"), 0o644); err != nil {
			t.Fatalf("write target page: %v", err)
		}
		if err := os.WriteFile(filepath.Join(rootDir, "docs", "sync", "index.md"), canonicalValidationMarkdown("sync-section-id", "Sync", "# Sync\n"), 0o644); err != nil {
			t.Fatalf("write section page: %v", err)
		}
		linkIndex, err := markdownlinks.NewIndexFromRoot(rootDir)
		if err != nil {
			t.Fatalf("build markdown link index: %v", err)
		}
		filesByRoute := map[workspaceValidationRouteKey]tree.PageID{
			workspaceValidationRouteConflictKey(tree.RoutePath("docs/target"), tree.NodeKindPage):  newFixturePageID("target-page-id"),
			workspaceValidationRouteConflictKey(tree.RoutePath("docs/sync"), tree.NodeKindSection): newFixturePageID("sync-section-id"),
		}
		resolver := newWorkspaceMarkdownLinkResolver("docs/source.md", linkIndex, filesByRoute)

		pageID, kind, ok, code := resolver("/docs/target.md")
		if !ok || code != "" || kind != tree.NodeKindPage || pageID != newFixturePageID("target-page-id") {
			t.Fatalf("page resolution = pageID %q kind %q ok=%v code=%q; want target-page-id page true", pageID, kind, ok, code)
		}
		if pageID == newFixturePageID("docs/target") {
			t.Fatalf("page resolution returned route path %q instead of metadata page ID", pageID)
		}

		sectionID, kind, ok, code := resolver("/docs/sync")
		if !ok || code != "" || kind != tree.NodeKindSection || sectionID != newFixturePageID("sync-section-id") {
			t.Fatalf("section resolution = pageID %q kind %q ok=%v code=%q; want sync-section-id section true", sectionID, kind, ok, code)
		}
		if sectionID == newFixturePageID("docs/sync") {
			t.Fatalf("section resolution returned route path %q instead of metadata page ID", sectionID)
		}
	})

	ginkgo.It("TestValidateWorkspaceMarkdownFilesLinkIssueCarriesResolvedMetadataPageID", func() {
		t := ginkgo.GinkgoT()
		rootDir := t.TempDir()
		if err := os.MkdirAll(filepath.Join(rootDir, "docs"), 0o755); err != nil {
			t.Fatalf("create workspace dirs: %v", err)
		}
		if err := os.WriteFile(filepath.Join(rootDir, "docs", "source.md"), canonicalValidationMarkdown("source-page-id", "Source", "# Source\n\n[Target](/docs/target)\n"), 0o644); err != nil {
			t.Fatalf("write source page: %v", err)
		}
		if err := os.WriteFile(filepath.Join(rootDir, "docs", "target.md"), canonicalValidationMarkdown("target-page-id", "Target", "# Target\n"), 0o644); err != nil {
			t.Fatalf("write target page: %v", err)
		}

		result := ValidateWorkspaceMarkdownFiles(WorkspaceMarkdownValidationOptions{RootDir: rootDir})
		var issue *Issue
		for i := range result.Issues {
			if result.Issues[i].Code == IssueCodeNonCanonicalLink {
				issue = &result.Issues[i]
				break
			}
		}
		if issue == nil {
			t.Fatalf("non-canonical link issue not found: %#v", result.Issues)
		}
		if issue.RoutePath != tree.RoutePath("docs/source") {
			t.Fatalf("issue route path = %q, want docs/source", issue.RoutePath)
		}
		if issue.PageID != newFixturePageID("source-page-id") {
			t.Fatalf("issue page ID = %q, want source-page-id", issue.PageID)
		}
		if issue.TargetPageID != newFixturePageID("target-page-id") {
			t.Fatalf("issue target page ID = %q, want target-page-id", issue.TargetPageID)
		}
		if issue.TargetPageID == newFixturePageID("docs/target") {
			t.Fatalf("issue target page ID used route path %q instead of metadata page ID", issue.TargetPageID)
		}
	})
})
