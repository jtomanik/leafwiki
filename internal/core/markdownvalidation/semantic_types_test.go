package markdownvalidation

import (
	"errors"
	ginkgo "github.com/onsi/ginkgo/v2"
	"os"
	"path/filepath"

	. "github.com/onsi/gomega"
	"github.com/perber/wiki/internal/core/markdownlinks"
	"github.com/perber/wiki/internal/core/tree"
)

var (
	errWorkspaceRouteAbsent            = errors.New("workspace route absent")
	errWorkspaceMarkdownLinkUnresolved = errors.New("workspace markdown link unresolved")
)

type workspaceMarkdownLinkResolution struct {
	PageID tree.PageID
	Kind   tree.NodeKind
	Code   IssueCode
}

func issuePathString(issue Issue) string {
	if issue.SourcePath != "" {
		return issue.SourcePath.FilesystemPath()
	}
	return issue.RoutePath.FilesystemPath()
}

func workspaceLinkPageIDForRouteResult(filesByRoute map[workspaceValidationRouteKey]tree.PageID, routePath tree.RoutePath, kind tree.NodeKind) (tree.PageID, error) {
	ginkgo.GinkgoHelper()
	pageID, found := workspaceLinkPageIDForRoute(filesByRoute, routePath, kind)
	if !found {
		return "", errWorkspaceRouteAbsent
	}
	return pageID, nil
}

func resolveWorkspaceMarkdownLinkResult(
	resolver func(string) (tree.PageID, tree.NodeKind, bool, IssueCode),
	destination string,
) (workspaceMarkdownLinkResolution, error) {
	ginkgo.GinkgoHelper()
	pageID, kind, resolved, code := resolver(destination)
	result := workspaceMarkdownLinkResolution{
		PageID: pageID,
		Kind:   kind,
		Code:   code,
	}
	if !resolved {
		return result, errWorkspaceMarkdownLinkUnresolved
	}
	return result, nil
}

var _ = ginkgo.Describe("semantic types", ginkgo.Label("unit"), func() {
	ginkgo.It("keeps validation callbacks typed with semantic page IDs", func() {
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
		Expect(opts.ResolvePageID).NotTo(BeNil())
		Expect(workspaceOpts.PageIDExists).NotTo(BeNil())
		Expect(workspaceOpts.AssetExists).NotTo(BeNil())
	})

	ginkgo.It("carries semantic paths and page IDs on validation issues", func() {
		sourceIssue := Issue{
			Severity:     IssueSeverityError,
			Code:         IssueCodeBrokenLink,
			SourcePath:   newFixtureMarkdownPath("docs/source.md"),
			RoutePath:    newFixtureRoutePath("docs/source"),
			PageID:       newFixturePageID("source-page-id"),
			TargetPageID: newFixturePageID("target-page-id"),
			Message:      "broken link",
		}

		var _ tree.MarkdownPath = sourceIssue.SourcePath
		var _ tree.RoutePath = sourceIssue.RoutePath
		var _ tree.PageID = sourceIssue.PageID
		var _ tree.PageID = sourceIssue.TargetPageID

		Expect(issuePathString(sourceIssue)).To(Equal("docs/source.md"))

		routeIssue := Issue{RoutePath: newFixtureRoutePath("docs/source"), PageID: newFixturePageID("source-page-id")}
		Expect(issuePathString(routeIssue)).To(Equal("docs/source"))
	})

	ginkgo.It("returns metadata page IDs for workspace markdown link routes", func() {
		filesByRoute := map[workspaceValidationRouteKey]tree.PageID{
			workspaceValidationRouteConflictKey(newFixtureRoutePath("docs/target"), tree.NodeKindPage):     newFixturePageID("target-page-id"),
			workspaceValidationRouteConflictKey(newFixtureRoutePath("docs/section"), tree.NodeKindSection): newFixturePageID("section-page-id"),
		}

		pageID, err := workspaceLinkPageIDForRouteResult(filesByRoute, newFixtureRoutePath("docs/target"), tree.NodeKindPage)
		Expect(err).To(Succeed())
		Expect(pageID).To(Equal(newFixturePageID("target-page-id")))
		sectionID, err := workspaceLinkPageIDForRouteResult(filesByRoute, newFixtureRoutePath("docs/section"), tree.NodeKindSection)
		Expect(err).To(Succeed())
		Expect(sectionID).To(Equal(newFixturePageID("section-page-id")))
		missingID, err := workspaceLinkPageIDForRouteResult(filesByRoute, newFixtureRoutePath("docs/missing"), tree.NodeKindPage)
		Expect(err).To(MatchError(errWorkspaceRouteAbsent))
		Expect(missingID).To(BeEmpty())
	})

	ginkgo.It("resolves workspace markdown links to metadata page IDs", func() {
		rootDir := markdownValidationTempDir()
		Expect(os.MkdirAll(filepath.Join(rootDir, "docs", "sync"), 0o755)).To(Succeed())
		Expect(os.WriteFile(filepath.Join(rootDir, "docs", "source.md"), canonicalValidationMarkdown("source-page-id", "Source", "# Source\n\n[Target](/docs/target.md)\n[Sync](/docs/sync)\n"), 0o644)).To(Succeed())
		Expect(os.WriteFile(filepath.Join(rootDir, "docs", "target.md"), canonicalValidationMarkdown("target-page-id", "Target", "# Target\n"), 0o644)).To(Succeed())
		Expect(os.WriteFile(filepath.Join(rootDir, "docs", "sync", "index.md"), canonicalValidationMarkdown("sync-section-id", "Sync", "# Sync\n"), 0o644)).To(Succeed())
		linkIndex, err := markdownlinks.NewIndexFromRoot(rootDir)
		Expect(err).NotTo(HaveOccurred())
		filesByRoute := map[workspaceValidationRouteKey]tree.PageID{
			workspaceValidationRouteConflictKey(newFixtureRoutePath("docs/target"), tree.NodeKindPage):  newFixturePageID("target-page-id"),
			workspaceValidationRouteConflictKey(newFixtureRoutePath("docs/sync"), tree.NodeKindSection): newFixturePageID("sync-section-id"),
		}
		resolver := newWorkspaceMarkdownLinkResolver(newFixtureMarkdownPath("docs/source.md"), linkIndex, filesByRoute)

		pageResolution, err := resolveWorkspaceMarkdownLinkResult(resolver, "/docs/target.md")
		Expect(err).To(Succeed())
		Expect(pageResolution).To(SatisfyAll(
			HaveField("PageID", newFixturePageID("target-page-id")),
			HaveField("Kind", tree.NodeKindPage),
			HaveField("Code", BeEmpty()),
		))
		Expect(pageResolution.PageID).NotTo(Equal(newFixturePageID("docs/target")))

		sectionResolution, err := resolveWorkspaceMarkdownLinkResult(resolver, "/docs/sync")
		Expect(err).To(Succeed())
		Expect(sectionResolution).To(SatisfyAll(
			HaveField("PageID", newFixturePageID("sync-section-id")),
			HaveField("Kind", tree.NodeKindSection),
			HaveField("Code", BeEmpty()),
		))
		Expect(sectionResolution.PageID).NotTo(Equal(newFixturePageID("docs/sync")))
	})

	ginkgo.It("attaches resolved metadata page IDs to non-canonical link issues", func() {
		rootDir := markdownValidationTempDir()
		Expect(os.MkdirAll(filepath.Join(rootDir, "docs"), 0o755)).To(Succeed())
		Expect(os.WriteFile(filepath.Join(rootDir, "docs", "source.md"), canonicalValidationMarkdown("source-page-id", "Source", "# Source\n\n[Target](/docs/target)\n"), 0o644)).To(Succeed())
		Expect(os.WriteFile(filepath.Join(rootDir, "docs", "target.md"), canonicalValidationMarkdown("target-page-id", "Target", "# Target\n"), 0o644)).To(Succeed())

		result := ValidateWorkspaceMarkdownFiles(WorkspaceMarkdownValidationOptions{RootDir: rootDir})
		var issue *Issue
		for i := range result.Issues {
			if result.Issues[i].Code == IssueCodeNonCanonicalLink {
				issue = &result.Issues[i]
				break
			}
		}
		Expect(issue).NotTo(BeNil())
		Expect(*issue).To(SatisfyAll(
			HaveField("RoutePath", newFixtureRoutePath("docs/source")),
			HaveField("PageID", newFixturePageID("source-page-id")),
			HaveField("TargetPageID", newFixturePageID("target-page-id")),
			Not(HaveField("TargetPageID", newFixturePageID("docs/target"))),
		))
	})
})
