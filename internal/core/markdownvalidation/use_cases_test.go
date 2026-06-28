package markdownvalidation

import (
	ginkgo "github.com/onsi/ginkgo/v2"
	"os"
	"path/filepath"
	"strings"

	"github.com/perber/wiki/internal/core/tree"
)

// Canonical Markdown links plan scenarios covered by tests in this file:
// - Malformed percent-encoding reports an invalid link instead of panicking
// - Case mismatch is invalid
// - Old extensionless page link is reported as non-canonical when migration cannot resolve it

func canonicalValidationMarkdown(id, title, body string) []byte {
	return []byte("<!-- leafwiki\nversion: 1\npage:\n  id: " + id + "\n  title: " + title + "\n-->\n\n" + body)
}

var _ = ginkgo.Describe("use cases", func() {
	ginkgo.It("TestValidateWorkspaceMarkdownFilesResolvesSectionDirectoryRoutes", func() {
		t := ginkgo.GinkgoT()
		rootDir := t.TempDir()
		docsDir := filepath.Join(rootDir, "docs")
		if err := os.MkdirAll(docsDir, 0o755); err != nil {
			t.Fatalf("create docs dir: %v", err)
		}
		if err := os.WriteFile(filepath.Join(docsDir, "child.md"), canonicalValidationMarkdown("docs-child", "Docs Child", "# Docs Child\n\n[Docs section](/docs)\n"), 0o644); err != nil {
			t.Fatalf("write child markdown: %v", err)
		}

		result := ValidateWorkspaceMarkdownFiles(WorkspaceMarkdownValidationOptions{RootDir: rootDir})

		if !result.OK {
			t.Fatalf("validation = %#v, want link to section directory route to resolve", result)
		}
	})

	ginkgo.It("TestValidateWorkspaceMarkdownFilesAllowsRootIndexRoute", func() {
		t := ginkgo.GinkgoT()
		rootDir := t.TempDir()
		if err := os.WriteFile(filepath.Join(rootDir, "index.md"), canonicalValidationMarkdown("root-page", "Root Page", "# Root Page\n\nRoot index content\n"), 0o644); err != nil {
			t.Fatalf("write root index markdown: %v", err)
		}

		result := ValidateWorkspaceMarkdownFiles(WorkspaceMarkdownValidationOptions{RootDir: rootDir})

		if !result.OK {
			t.Fatalf("validation = %#v, want root index.md to validate as root route", result)
		}
	})

	ginkgo.It("TestValidateWorkspaceMarkdownFilesAllowsRootReadmeFallbackRoute", func() {
		t := ginkgo.GinkgoT()
		rootDir := t.TempDir()
		if err := os.WriteFile(filepath.Join(rootDir, "README.md"), canonicalValidationMarkdown("root", "Root Page", "# Root Page\n\nRoot README content\n"), 0o644); err != nil {
			t.Fatalf("write root README markdown: %v", err)
		}

		result := ValidateWorkspaceMarkdownFiles(WorkspaceMarkdownValidationOptions{RootDir: rootDir})

		if !result.OK {
			t.Fatalf("validation = %#v, want root README.md fallback to validate as root route", result)
		}
	})

	ginkgo.It("TestValidateWorkspaceMarkdownFilesAllowsRootSectionLinkWithoutRootContent", func() {
		t := ginkgo.GinkgoT()
		rootDir := t.TempDir()
		docsDir := filepath.Join(rootDir, "docs")
		if err := os.MkdirAll(docsDir, 0o755); err != nil {
			t.Fatalf("create docs dir: %v", err)
		}
		if err := os.WriteFile(filepath.Join(docsDir, "page.md"), canonicalValidationMarkdown("docs-page", "Docs Page", "# Docs Page\n\n[Root](/)\n"), 0o644); err != nil {
			t.Fatalf("write markdown: %v", err)
		}

		result := ValidateWorkspaceMarkdownFiles(WorkspaceMarkdownValidationOptions{RootDir: rootDir})

		if !result.OK {
			t.Fatalf("validation = %#v, want root section link to resolve without root content", result)
		}
	})

	ginkgo.It("TestValidateWorkspaceMarkdownFiles_DuplicateCanonicalPageIDMessageUsesPageID", func() {
		t := ginkgo.GinkgoT()
		rootDir := t.TempDir()
		if err := os.WriteFile(filepath.Join(rootDir, "first.md"), canonicalValidationMarkdown("duplicate-id", "First", "# First\n"), 0o644); err != nil {
			t.Fatalf("write first markdown: %v", err)
		}
		if err := os.WriteFile(filepath.Join(rootDir, "second.md"), canonicalValidationMarkdown("duplicate-id", "Second", "# Second\n"), 0o644); err != nil {
			t.Fatalf("write second markdown: %v", err)
		}

		result := ValidateWorkspaceMarkdownFiles(WorkspaceMarkdownValidationOptions{RootDir: rootDir})

		if result.OK {
			t.Fatalf("validation = %#v, want duplicate page.id issue", result)
		}
		if len(result.Issues) != 1 || result.Issues[0].Code != "duplicate_leafwiki_id" {
			t.Fatalf("issues = %#v, want one duplicate_leafwiki_id", result.Issues)
		}
		if !strings.Contains(result.Issues[0].Message, "page.id") {
			t.Fatalf("message = %q, want canonical page.id wording", result.Issues[0].Message)
		}
		if strings.Contains(result.Issues[0].Message, "leafwiki_id") {
			t.Fatalf("message = %q, should not name legacy leafwiki_id", result.Issues[0].Message)
		}
	})

	ginkgo.It("TestValidateWorkspaceMarkdownFiles_ResolvesCanonicalPageMdAndSectionLinks", func() {
		t := ginkgo.GinkgoT()
		rootDir := t.TempDir()
		if err := os.MkdirAll(filepath.Join(rootDir, "docs", "sync"), 0o755); err != nil {
			t.Fatalf("create sync dir: %v", err)
		}
		if err := os.MkdirAll(filepath.Join(rootDir, "guides"), 0o755); err != nil {
			t.Fatalf("create guides dir: %v", err)
		}
		if err := os.WriteFile(filepath.Join(rootDir, "docs", "a.md"), []byte("---\nleafwiki_id: docs-a\nleafwiki_title: Docs A\n---\n# Docs A\n\n[B](/docs/b.md)\n[Sync](/docs/sync)\n[Sync Index](/docs/sync/index.md)\n[Guide Readme](/guides/README.md)\n"), 0o644); err != nil {
			t.Fatalf("write source markdown: %v", err)
		}
		if err := os.WriteFile(filepath.Join(rootDir, "docs", "b.md"), []byte("---\nleafwiki_id: docs-b\nleafwiki_title: Docs B\n---\n# Docs B\n"), 0o644); err != nil {
			t.Fatalf("write page target markdown: %v", err)
		}
		if err := os.WriteFile(filepath.Join(rootDir, "docs", "sync", "index.md"), []byte("---\nleafwiki_id: docs-sync\nleafwiki_title: Docs Sync\n---\n# Docs Sync\n"), 0o644); err != nil {
			t.Fatalf("write section index markdown: %v", err)
		}
		if err := os.WriteFile(filepath.Join(rootDir, "guides", "README.md"), []byte("---\nleafwiki_id: guides\nleafwiki_title: Guides\n---\n# Guides\n"), 0o644); err != nil {
			t.Fatalf("write README fallback markdown: %v", err)
		}

		result := ValidateWorkspaceMarkdownFiles(WorkspaceMarkdownValidationOptions{RootDir: rootDir})

		if !result.OK {
			t.Fatalf("validation = %#v, want canonical page and section links to resolve", result)
		}
	})

	ginkgo.It("TestValidateWorkspaceMarkdownFiles_ResolvesMarkdownLinkRootPrefix", func() {
		t := ginkgo.GinkgoT()
		repoRoot := t.TempDir()
		rootDir := filepath.Join(repoRoot, "docs")
		if err := os.MkdirAll(filepath.Join(rootDir, "sync"), 0o755); err != nil {
			t.Fatalf("create sync dir: %v", err)
		}
		if err := os.WriteFile(filepath.Join(rootDir, "index.md"), canonicalValidationMarkdown("root", "Root", "# Root\n\n[Glossary](/docs/sync/glossary.md)\n"), 0o644); err != nil {
			t.Fatalf("write root markdown: %v", err)
		}
		if err := os.WriteFile(filepath.Join(rootDir, "sync", "glossary.md"), canonicalValidationMarkdown("glossary", "Glossary", "# Glossary\n"), 0o644); err != nil {
			t.Fatalf("write glossary markdown: %v", err)
		}

		result := ValidateWorkspaceMarkdownFiles(WorkspaceMarkdownValidationOptions{
			RootDir:                rootDir,
			MarkdownLinkRootPrefix: "/docs",
		})

		if !result.OK {
			t.Fatalf("validation = %#v, want /docs prefix link to resolve inside docs root", result)
		}
	})

	ginkgo.It("TestValidateMarkdownContent_ResolvesPrefixedAssetWithMarkdownLinkRootPrefix", func() {
		t := ginkgo.GinkgoT()
		seenDestination := ""
		result := ValidateMarkdownContentWithOptions(tree.RoutePath("source"), "![Logo](/docs/assets/logo.png)", ContentValidationOptions{
			ExistingPageID:         "source",
			MarkdownLinkRootPrefix: "/docs",
			AssetExists: func(destination string) bool {
				seenDestination = destination
				return destination == "/assets/logo.png"
			},
		})

		if !result.OK {
			t.Fatalf("validation = %#v, want prefixed asset to resolve", result)
		}
		if seenDestination != "/assets/logo.png" {
			t.Fatalf("AssetExists destination = %q, want /assets/logo.png", seenDestination)
		}
	})

	ginkgo.It("TestValidateMarkdownContent_ReportsMissingPrefixedMarkdownAssetWithMarkdownLinkRootPrefix", func() {
		t := ginkgo.GinkgoT()
		seenDestination := ""
		result := ValidateMarkdownContentWithOptions(tree.RoutePath("source"), "[Manual](/docs/assets/manual.md)", ContentValidationOptions{
			ExistingPageID:         "source",
			MarkdownLinkRootPrefix: "/docs",
			AssetExists: func(destination string) bool {
				seenDestination = destination
				return false
			},
		})

		if result.OK {
			t.Fatalf("validation = %#v, want missing prefixed markdown asset", result)
		}
		if len(result.Issues) != 1 || result.Issues[0].Code != "missing_asset" {
			t.Fatalf("issues = %#v, want one missing_asset issue", result.Issues)
		}
		if seenDestination != "/assets/manual.md" {
			t.Fatalf("AssetExists destination = %q, want /assets/manual.md", seenDestination)
		}
	})

	ginkgo.It("TestValidateWorkspaceMarkdownFiles_NormalizesWorkspaceRoutes", func() {
		t := ginkgo.GinkgoT()
		rootDir := t.TempDir()
		if err := os.MkdirAll(filepath.Join(rootDir, "plans"), 0o755); err != nil {
			t.Fatalf("create plans dir: %v", err)
		}
		if err := os.WriteFile(filepath.Join(rootDir, "plans", "index.md"), canonicalValidationMarkdown("plans", "Plans", "# Plans\n"), 0o644); err != nil {
			t.Fatalf("write plans index markdown: %v", err)
		}
		if err := os.WriteFile(filepath.Join(rootDir, "plans", "agent_hooks.PLAN.md"), canonicalValidationMarkdown("agent-hooks-plan", "Agent Hooks Plan", "# Agent Hooks Plan\n"), 0o644); err != nil {
			t.Fatalf("write plan markdown: %v", err)
		}
		if err := os.WriteFile(filepath.Join(rootDir, "source.md"), canonicalValidationMarkdown("source", "Source", "# Source\n\n[Plan](/plans/agent-hooks-plan.md)\n"), 0o644); err != nil {
			t.Fatalf("write source markdown: %v", err)
		}

		result := ValidateWorkspaceMarkdownFiles(WorkspaceMarkdownValidationOptions{RootDir: rootDir})

		if !result.OK {
			t.Fatalf("validation = %#v, want normalizable plan filename and normalized link to validate", result)
		}
		for _, issue := range result.Issues {
			if issue.Code == "invalid_slug" && strings.Contains(issuePathString(issue), "agent_hooks.PLAN.md") {
				t.Fatalf("issues = %#v, want no invalid_slug for normalizable plan filename", result.Issues)
			}
		}
	})

	ginkgo.It("TestValidateWorkspaceMarkdownFiles_RejectsRawNormalizedSourceMarkdownLinks", func() {
		t := ginkgo.GinkgoT()
		rootDir := t.TempDir()
		if err := os.MkdirAll(filepath.Join(rootDir, "plans"), 0o755); err != nil {
			t.Fatalf("create plans dir: %v", err)
		}
		if err := os.WriteFile(filepath.Join(rootDir, "plans", "index.md"), canonicalValidationMarkdown("plans", "Plans", "# Plans\n"), 0o644); err != nil {
			t.Fatalf("write plans index markdown: %v", err)
		}
		if err := os.WriteFile(filepath.Join(rootDir, "plans", "agent_hooks.PLAN.md"), canonicalValidationMarkdown("agent-hooks-plan", "Agent Hooks Plan", "# Agent Hooks Plan\n"), 0o644); err != nil {
			t.Fatalf("write plan markdown: %v", err)
		}
		if err := os.WriteFile(filepath.Join(rootDir, "source.md"), canonicalValidationMarkdown("source", "Source", "# Source\n\n[Plan](/plans/agent_hooks.PLAN.md)\n"), 0o644); err != nil {
			t.Fatalf("write source markdown: %v", err)
		}

		result := ValidateWorkspaceMarkdownFiles(WorkspaceMarkdownValidationOptions{RootDir: rootDir})

		if result.OK {
			t.Fatalf("validation = %#v, want raw normalized source link to fail", result)
		}
		if len(result.Issues) != 1 || result.Issues[0].Code != "non_canonical_markdown_path" {
			t.Fatalf("issues = %#v, want one non_canonical_markdown_path", result.Issues)
		}
		if issuePathString(result.Issues[0]) != "source" {
			t.Fatalf("issue path = %q, want source", issuePathString(result.Issues[0]))
		}
	})

	ginkgo.It("TestValidateWorkspaceMarkdownFiles_ReportsNormalizedRouteCollision", func() {
		t := ginkgo.GinkgoT()
		rootDir := t.TempDir()
		if err := os.MkdirAll(filepath.Join(rootDir, "plans"), 0o755); err != nil {
			t.Fatalf("create plans dir: %v", err)
		}
		if err := os.WriteFile(filepath.Join(rootDir, "plans", "foo_bar.md"), canonicalValidationMarkdown("foo-bar-a", "Foo Bar A", "# Foo Bar A\n"), 0o644); err != nil {
			t.Fatalf("write first markdown: %v", err)
		}
		if err := os.WriteFile(filepath.Join(rootDir, "plans", "foo-bar.md"), canonicalValidationMarkdown("foo-bar-b", "Foo Bar B", "# Foo Bar B\n"), 0o644); err != nil {
			t.Fatalf("write second markdown: %v", err)
		}

		result := ValidateWorkspaceMarkdownFiles(WorkspaceMarkdownValidationOptions{RootDir: rootDir})

		if result.OK {
			t.Fatalf("validation = %#v, want normalized route collision", result)
		}
		var found bool
		for _, issue := range result.Issues {
			if issue.Code == "path_conflict" &&
				(issuePathString(issue) == "plans/foo-bar.md" || issuePathString(issue) == "plans/foo_bar.md") &&
				(strings.Contains(issue.Message, "plans/foo-bar.md") || strings.Contains(issue.Message, "plans/foo_bar.md")) {
				found = true
			}
			if issue.Code == "invalid_slug" && strings.Contains(issuePathString(issue), "foo_bar.md") {
				t.Fatalf("issues = %#v, want path_conflict instead of invalid_slug for normalizable filename", result.Issues)
			}
		}
		if !found {
			t.Fatalf("issues = %#v, want path_conflict for normalized duplicate route", result.Issues)
		}
	})

	ginkgo.It("TestValidateWorkspaceMarkdownFiles_IgnoresProtocolRelativeAndSchemedURLs", func() {
		t := ginkgo.GinkgoT()
		rootDir := t.TempDir()
		if err := os.WriteFile(filepath.Join(rootDir, "source.md"), []byte("---\nleafwiki_id: source\nleafwiki_title: Source\n---\n# Source\n\n[CDN](//cdn.example.com/lib.md)\n[Obsidian](obsidian://open?vault=wiki)\n"), 0o644); err != nil {
			t.Fatalf("write source markdown: %v", err)
		}

		result := ValidateWorkspaceMarkdownFiles(WorkspaceMarkdownValidationOptions{RootDir: rootDir})

		if !result.OK {
			t.Fatalf("validation = %#v, want protocol-relative and schemed URLs to be external", result)
		}
	})

	ginkgo.It("TestValidateWorkspaceMarkdownFilesRequiresFilesystemTargets", func() {
		t := ginkgo.GinkgoT()
		rootDir := t.TempDir()
		if err := os.WriteFile(filepath.Join(rootDir, "source.md"), []byte("---\nleafwiki_id: source\nleafwiki_title: Source\n---\n# Source\n\n[Stale](/stale-target)\n"), 0o644); err != nil {
			t.Fatalf("write source markdown: %v", err)
		}

		result := ValidateWorkspaceMarkdownFiles(WorkspaceMarkdownValidationOptions{RootDir: rootDir})

		if result.OK {
			t.Fatalf("validation = %#v, want missing filesystem target to fail", result)
		}
		if len(result.Issues) != 1 || result.Issues[0].Code != "broken_link" {
			t.Fatalf("issues = %#v, want one broken_link", result.Issues)
		}
		if issuePathString(result.Issues[0]) != "source" {
			t.Fatalf("issue path = %q, want source page path", issuePathString(result.Issues[0]))
		}
	})

	ginkgo.It("TestValidateMarkdownContent_DuplicateCanonicalPageIDMessageUsesPageID", func() {
		t := ginkgo.GinkgoT()
		result := ValidateMarkdownContentWithOptions(tree.RoutePath("docs/page"), string(canonicalValidationMarkdown("duplicate-id", "Page", "# Page\n")), ContentValidationOptions{
			ExistingPageID: "current-id",
			PageIDExists: func(pageID tree.PageID) bool {
				return pageID == "duplicate-id"
			},
		})

		if result.OK {
			t.Fatalf("validation = %#v, want duplicate page.id issue", result)
		}
		if len(result.Issues) != 1 || result.Issues[0].Code != "duplicate_leafwiki_id" {
			t.Fatalf("issues = %#v, want one duplicate_leafwiki_id", result.Issues)
		}
		if !strings.Contains(result.Issues[0].Message, "page.id") {
			t.Fatalf("message = %q, want canonical page.id wording", result.Issues[0].Message)
		}
		if strings.Contains(result.Issues[0].Message, "leafwiki_id") {
			t.Fatalf("message = %q, should not name legacy leafwiki_id", result.Issues[0].Message)
		}
	})

	// - Malformed percent-encoding reports an invalid link instead of panicking
	ginkgo.It("TestValidateWorkspaceMarkdownFilesReportsInvalidCanonicalLinks", func() {
		t := ginkgo.GinkgoT()
		workspaceParent := t.TempDir()
		rootDir := filepath.Join(workspaceParent, "workspace")
		if err := os.MkdirAll(rootDir, 0o755); err != nil {
			t.Fatalf("create workspace root: %v", err)
		}
		if err := os.WriteFile(filepath.Join(workspaceParent, "outside.md"), []byte("---\nleafwiki_id: outside\nleafwiki_title: Outside\n---\n# Outside\n"), 0o644); err != nil {
			t.Fatalf("write outside markdown: %v", err)
		}
		if err := os.WriteFile(filepath.Join(rootDir, "source.md"), []byte("---\nleafwiki_id: source\nleafwiki_title: Source\n---\n# Source\n\n[Bad Encoding](/docs/%zz)\n[Escape](../outside.md)\n"), 0o644); err != nil {
			t.Fatalf("write source markdown: %v", err)
		}

		result := ValidateWorkspaceMarkdownFiles(WorkspaceMarkdownValidationOptions{RootDir: rootDir})

		if result.OK {
			t.Fatalf("validation = %#v, want invalid links to fail", result)
		}
		if len(result.Issues) != 2 {
			t.Fatalf("issues = %#v, want two invalid_link issues", result.Issues)
		}
		for _, issue := range result.Issues {
			if issue.Code != "invalid_link" {
				t.Fatalf("issue = %#v, want invalid_link", issue)
			}
			if issuePathString(issue) != "source" {
				t.Fatalf("issue path = %q, want source", issuePathString(issue))
			}
		}
	})

	ginkgo.It("TestValidateWorkspaceMarkdownFiles_DistinguishesSectionMdPageFromSectionDefault", func() {
		t := ginkgo.GinkgoT()
		rootDir := t.TempDir()
		if err := os.MkdirAll(filepath.Join(rootDir, "docs", "sync"), 0o755); err != nil {
			t.Fatalf("create sync dir: %v", err)
		}
		if err := os.WriteFile(filepath.Join(rootDir, "docs", "a.md"), []byte("---\nleafwiki_id: docs-a\nleafwiki_title: Docs A\n---\n# Docs A\n\n[Sync page](/docs/sync.md)\n"), 0o644); err != nil {
			t.Fatalf("write source markdown: %v", err)
		}
		if err := os.WriteFile(filepath.Join(rootDir, "docs", "sync", "index.md"), []byte("---\nleafwiki_id: docs-sync\nleafwiki_title: Docs Sync\n---\n# Docs Sync\n"), 0o644); err != nil {
			t.Fatalf("write section index markdown: %v", err)
		}

		result := ValidateWorkspaceMarkdownFiles(WorkspaceMarkdownValidationOptions{RootDir: rootDir})

		if result.OK {
			t.Fatalf("validation = %#v, want /docs/sync.md to require a page file, not alias a section", result)
		}
		if len(result.Issues) != 1 || result.Issues[0].Code != "broken_link" {
			t.Fatalf("issues = %#v, want one broken_link", result.Issues)
		}
	})

	ginkgo.It("TestValidateWorkspaceMarkdownFiles_AllowsSameBasenamePageAndSectionCanonicalLinks", func() {
		t := ginkgo.GinkgoT()
		rootDir := t.TempDir()
		if err := os.MkdirAll(filepath.Join(rootDir, "docs", "sync"), 0o755); err != nil {
			t.Fatalf("create sync dir: %v", err)
		}
		if err := os.WriteFile(filepath.Join(rootDir, "docs", "a.md"), []byte("---\nleafwiki_id: docs-a\nleafwiki_title: Docs A\n---\n# Docs A\n\n[Sync section](/docs/sync)\n[Sync page](/docs/sync.md)\n"), 0o644); err != nil {
			t.Fatalf("write source markdown: %v", err)
		}
		if err := os.WriteFile(filepath.Join(rootDir, "docs", "sync.md"), []byte("---\nleafwiki_id: page-sync\nleafwiki_title: Sync Page\n---\n# Sync Page\n"), 0o644); err != nil {
			t.Fatalf("write sync page markdown: %v", err)
		}
		if err := os.WriteFile(filepath.Join(rootDir, "docs", "sync", "index.md"), []byte("---\nleafwiki_id: section-sync\nleafwiki_title: Sync Section\n---\n# Sync Section\n"), 0o644); err != nil {
			t.Fatalf("write sync section markdown: %v", err)
		}

		result := ValidateWorkspaceMarkdownFiles(WorkspaceMarkdownValidationOptions{RootDir: rootDir})

		if !result.OK {
			t.Fatalf("validation = %#v, want canonical same-basename page and section links to resolve", result)
		}
		for _, issue := range result.Issues {
			if issue.Code == "path_conflict" || issue.Code == "ambiguous_legacy_link" {
				t.Fatalf("issues = %#v, want no conflict or ambiguity for canonical same-basename links", result.Issues)
			}
		}
	})

	ginkgo.It("TestValidateWorkspaceMarkdownFiles_AcceptsCanonicalSectionLinkWithSameBasenamePage", func() {
		t := ginkgo.GinkgoT()
		rootDir := t.TempDir()
		if err := os.MkdirAll(filepath.Join(rootDir, "docs", "sync"), 0o755); err != nil {
			t.Fatalf("create sync dir: %v", err)
		}
		if err := os.WriteFile(filepath.Join(rootDir, "docs", "a.md"), []byte("---\nleafwiki_id: docs-a\nleafwiki_title: Docs A\n---\n# Docs A\n\n[Sync section](/docs/sync)\n[Sync page](/docs/sync.md)\n"), 0o644); err != nil {
			t.Fatalf("write source markdown: %v", err)
		}
		if err := os.WriteFile(filepath.Join(rootDir, "docs", "sync.md"), []byte("---\nleafwiki_id: page-sync\nleafwiki_title: Sync Page\n---\n# Sync Page\n"), 0o644); err != nil {
			t.Fatalf("write sync page markdown: %v", err)
		}
		if err := os.WriteFile(filepath.Join(rootDir, "docs", "sync", "index.md"), []byte("---\nleafwiki_id: section-sync\nleafwiki_title: Sync Section\n---\n# Sync Section\n"), 0o644); err != nil {
			t.Fatalf("write sync section markdown: %v", err)
		}

		result := ValidateWorkspaceMarkdownFiles(WorkspaceMarkdownValidationOptions{RootDir: rootDir})

		if !result.OK {
			t.Fatalf("validation = %#v, want canonical same-basename page and section links to resolve", result)
		}
	})

	// - Case mismatch is invalid
	ginkgo.It("TestValidateWorkspaceMarkdownFiles_UsesExactCaseSensitiveTargetMatching", func() {
		t := ginkgo.GinkgoT()
		rootDir := t.TempDir()
		if err := os.MkdirAll(filepath.Join(rootDir, "docs"), 0o755); err != nil {
			t.Fatalf("create docs dir: %v", err)
		}
		if err := os.WriteFile(filepath.Join(rootDir, "docs", "a.md"), []byte("---\nleafwiki_id: docs-a\nleafwiki_title: Docs A\n---\n# Docs A\n\n[Sync](/docs/sync.md)\n"), 0o644); err != nil {
			t.Fatalf("write source markdown: %v", err)
		}
		if err := os.WriteFile(filepath.Join(rootDir, "docs", "Sync.md"), []byte("---\nleafwiki_id: docs-sync\nleafwiki_title: Docs Sync\n---\n# Docs Sync\n"), 0o644); err != nil {
			t.Fatalf("write target markdown: %v", err)
		}

		result := ValidateWorkspaceMarkdownFiles(WorkspaceMarkdownValidationOptions{RootDir: rootDir})

		found := false
		for _, issue := range result.Issues {
			if issue.Code == "broken_link" && issuePathString(issue) == "docs/a" {
				found = true
			}
		}
		if !found {
			t.Fatalf("issues = %#v, want source broken_link for exact-case mismatch", result.Issues)
		}
	})

	// - Old extensionless page link is reported as non-canonical when migration cannot resolve it
	ginkgo.It("TestValidateWorkspaceMarkdownFiles_RejectsUnmigratedExtensionlessPageLink", func() {
		t := ginkgo.GinkgoT()
		rootDir := t.TempDir()
		if err := os.MkdirAll(filepath.Join(rootDir, "docs"), 0o755); err != nil {
			t.Fatalf("create docs dir: %v", err)
		}
		if err := os.WriteFile(filepath.Join(rootDir, "docs", "a.md"), []byte("---\nleafwiki_id: docs-a\nleafwiki_title: Docs A\n---\n# Docs A\n\n[B](/docs/b)\n"), 0o644); err != nil {
			t.Fatalf("write source markdown: %v", err)
		}
		if err := os.WriteFile(filepath.Join(rootDir, "docs", "b.md"), []byte("---\nleafwiki_id: docs-b\nleafwiki_title: Docs B\n---\n# Docs B\n"), 0o644); err != nil {
			t.Fatalf("write target markdown: %v", err)
		}

		result := ValidateWorkspaceMarkdownFiles(WorkspaceMarkdownValidationOptions{RootDir: rootDir})

		if result.OK {
			t.Fatalf("validation = %#v, want extensionless page link to fail", result)
		}
		if len(result.Issues) != 1 || result.Issues[0].Code != "non_canonical_link" {
			t.Fatalf("issues = %#v, want one non_canonical_link", result.Issues)
		}
		if issuePathString(result.Issues[0]) != "docs/a" {
			t.Fatalf("issue path = %q, want source page path", issuePathString(result.Issues[0]))
		}
	})

	ginkgo.It("TestValidateMarkdownContent_UsesResolveMarkdownLinkWithoutLegacyResolver", func() {
		t := ginkgo.GinkgoT()
		result := ValidateMarkdownContentWithOptions(tree.RoutePath("docs/a"), "[B](/docs/b)\n", ContentValidationOptions{
			ResolveMarkdownLink: func(destination string) (tree.PageID, tree.NodeKind, bool, IssueCode) {
				if destination == "/docs/b" {
					return "docs/b", tree.NodeKindPage, true, ""
				}
				return "", "", false, IssueCodeBrokenLink
			},
		})

		if result.OK {
			t.Fatalf("validation = %#v, want non-canonical page link issue", result)
		}
		if len(result.Issues) != 1 || result.Issues[0].Code != "non_canonical_link" {
			t.Fatalf("issues = %#v, want one non_canonical_link", result.Issues)
		}
	})
})
