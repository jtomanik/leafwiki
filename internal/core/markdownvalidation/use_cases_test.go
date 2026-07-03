package markdownvalidation

import (
	ginkgo "github.com/onsi/ginkgo/v2"
	"os"
	"path/filepath"

	. "github.com/onsi/gomega"
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
	ginkgo.It("resolves section directory routes from workspace markdown links", func() {
		rootDir := markdownValidationTempDir()
		docsDir := filepath.Join(rootDir, "docs")
		Expect(os.MkdirAll(docsDir, 0o755)).To(Succeed())
		Expect(os.WriteFile(filepath.Join(docsDir, "child.md"), canonicalValidationMarkdown("docs-child", "Docs Child", "# Docs Child\n\n[Docs section](/docs)\n"), 0o644)).To(Succeed())

		result := ValidateWorkspaceMarkdownFiles(WorkspaceMarkdownValidationOptions{RootDir: rootDir})

		Expect(result.OK).To(BeTrue())
	})

	ginkgo.It("allows root index markdown to validate as the root route", func() {
		rootDir := markdownValidationTempDir()
		Expect(os.WriteFile(filepath.Join(rootDir, "index.md"), canonicalValidationMarkdown("root-page", "Root Page", "# Root Page\n\nRoot index content\n"), 0o644)).To(Succeed())

		result := ValidateWorkspaceMarkdownFiles(WorkspaceMarkdownValidationOptions{RootDir: rootDir})

		Expect(result.OK).To(BeTrue())
	})

	ginkgo.It("allows root README markdown to validate as the root fallback route", func() {
		rootDir := markdownValidationTempDir()
		Expect(os.WriteFile(filepath.Join(rootDir, "README.md"), canonicalValidationMarkdown("root", "Root Page", "# Root Page\n\nRoot README content\n"), 0o644)).To(Succeed())

		result := ValidateWorkspaceMarkdownFiles(WorkspaceMarkdownValidationOptions{RootDir: rootDir})

		Expect(result.OK).To(BeTrue())
	})

	ginkgo.It("allows root section links even when the root has no page content", func() {
		rootDir := markdownValidationTempDir()
		docsDir := filepath.Join(rootDir, "docs")
		Expect(os.MkdirAll(docsDir, 0o755)).To(Succeed())
		Expect(os.WriteFile(filepath.Join(docsDir, "page.md"), canonicalValidationMarkdown("docs-page", "Docs Page", "# Docs Page\n\n[Root](/)\n"), 0o644)).To(Succeed())

		result := ValidateWorkspaceMarkdownFiles(WorkspaceMarkdownValidationOptions{RootDir: rootDir})

		Expect(result.OK).To(BeTrue())
	})

	ginkgo.It("reports duplicate canonical page IDs with page metadata wording", func() {
		rootDir := markdownValidationTempDir()
		Expect(os.WriteFile(filepath.Join(rootDir, "first.md"), canonicalValidationMarkdown("duplicate-id", "First", "# First\n"), 0o644)).To(Succeed())
		Expect(os.WriteFile(filepath.Join(rootDir, "second.md"), canonicalValidationMarkdown("duplicate-id", "Second", "# Second\n"), 0o644)).To(Succeed())

		result := ValidateWorkspaceMarkdownFiles(WorkspaceMarkdownValidationOptions{RootDir: rootDir})

		Expect(result).To(matchValidationResult(BeFalse(), HaveExactElements(
			matchDuplicatePageIDIssue(),
		)))
	})

	ginkgo.It("resolves canonical page, section, index, and README links", func() {
		rootDir := markdownValidationTempDir()
		Expect(os.MkdirAll(filepath.Join(rootDir, "docs", "sync"), 0o755)).To(Succeed())
		Expect(os.MkdirAll(filepath.Join(rootDir, "guides"), 0o755)).To(Succeed())
		Expect(os.WriteFile(filepath.Join(rootDir, "docs", "a.md"), []byte("---\nleafwiki_id: docs-a\nleafwiki_title: Docs A\n---\n# Docs A\n\n[B](/docs/b.md)\n[Sync](/docs/sync)\n[Sync Index](/docs/sync/index.md)\n[Guide Readme](/guides/README.md)\n"), 0o644)).To(Succeed())
		Expect(os.WriteFile(filepath.Join(rootDir, "docs", "b.md"), []byte("---\nleafwiki_id: docs-b\nleafwiki_title: Docs B\n---\n# Docs B\n"), 0o644)).To(Succeed())
		Expect(os.WriteFile(filepath.Join(rootDir, "docs", "sync", "index.md"), []byte("---\nleafwiki_id: docs-sync\nleafwiki_title: Docs Sync\n---\n# Docs Sync\n"), 0o644)).To(Succeed())
		Expect(os.WriteFile(filepath.Join(rootDir, "guides", "README.md"), []byte("---\nleafwiki_id: guides\nleafwiki_title: Guides\n---\n# Guides\n"), 0o644)).To(Succeed())

		result := ValidateWorkspaceMarkdownFiles(WorkspaceMarkdownValidationOptions{RootDir: rootDir})

		Expect(result.OK).To(BeTrue())
	})

	ginkgo.It("resolves markdown links beneath a configured root prefix", func() {
		repoRoot := markdownValidationTempDir()
		rootDir := filepath.Join(repoRoot, "docs")
		Expect(os.MkdirAll(filepath.Join(rootDir, "sync"), 0o755)).To(Succeed())
		Expect(os.WriteFile(filepath.Join(rootDir, "index.md"), canonicalValidationMarkdown("root", "Root", "# Root\n\n[Glossary](/docs/sync/glossary.md)\n"), 0o644)).To(Succeed())
		Expect(os.WriteFile(filepath.Join(rootDir, "sync", "glossary.md"), canonicalValidationMarkdown("glossary", "Glossary", "# Glossary\n"), 0o644)).To(Succeed())

		result := ValidateWorkspaceMarkdownFiles(WorkspaceMarkdownValidationOptions{
			RootDir:                rootDir,
			MarkdownLinkRootPrefix: "/docs",
		})

		Expect(result.OK).To(BeTrue())
	})

	ginkgo.It("resolves prefixed assets through the markdown link root prefix", func() {
		seenDestination := ""
		result := ValidateMarkdownContentWithOptions(tree.RoutePath("source"), "![Logo](/docs/assets/logo.png)", ContentValidationOptions{
			ExistingPageID:         "source",
			MarkdownLinkRootPrefix: "/docs",
			AssetExists: func(destination string) bool {
				seenDestination = destination
				return destination == "/assets/logo.png"
			},
		})

		Expect(result.OK).To(BeTrue())
		Expect(seenDestination).To(Equal("/assets/logo.png"))
	})

	ginkgo.It("reports missing prefixed markdown assets after root-prefix normalization", func() {
		seenDestination := ""
		result := ValidateMarkdownContentWithOptions(tree.RoutePath("source"), "[Manual](/docs/assets/manual.md)", ContentValidationOptions{
			ExistingPageID:         "source",
			MarkdownLinkRootPrefix: "/docs",
			AssetExists: func(destination string) bool {
				seenDestination = destination
				return false
			},
		})

		Expect(result).To(matchValidationResult(BeFalse(), HaveExactElements(
			matchValidationIssueCode(IssueCodeMissingAsset),
		)))
		Expect(seenDestination).To(Equal("/assets/manual.md"))
	})

	ginkgo.It("normalizes workspace routes for plan-style filenames", func() {
		rootDir := markdownValidationTempDir()
		Expect(os.MkdirAll(filepath.Join(rootDir, "plans"), 0o755)).To(Succeed())
		Expect(os.WriteFile(filepath.Join(rootDir, "plans", "index.md"), canonicalValidationMarkdown("plans", "Plans", "# Plans\n"), 0o644)).To(Succeed())
		Expect(os.WriteFile(filepath.Join(rootDir, "plans", "agent_hooks.PLAN.md"), canonicalValidationMarkdown("agent-hooks-plan", "Agent Hooks Plan", "# Agent Hooks Plan\n"), 0o644)).To(Succeed())
		Expect(os.WriteFile(filepath.Join(rootDir, "source.md"), canonicalValidationMarkdown("source", "Source", "# Source\n\n[Plan](/plans/agent-hooks-plan.md)\n"), 0o644)).To(Succeed())

		result := ValidateWorkspaceMarkdownFiles(WorkspaceMarkdownValidationOptions{RootDir: rootDir})

		Expect(result).To(matchValidationResult(BeTrue(), Not(ContainElement(
			matchValidationIssueAtPath(IssueCodeInvalidSlug, "plans/agent_hooks.PLAN.md"),
		))))
	})

	ginkgo.It("rejects source markdown links that use raw unnormalized routes", func() {
		rootDir := markdownValidationTempDir()
		Expect(os.MkdirAll(filepath.Join(rootDir, "plans"), 0o755)).To(Succeed())
		Expect(os.WriteFile(filepath.Join(rootDir, "plans", "index.md"), canonicalValidationMarkdown("plans", "Plans", "# Plans\n"), 0o644)).To(Succeed())
		Expect(os.WriteFile(filepath.Join(rootDir, "plans", "agent_hooks.PLAN.md"), canonicalValidationMarkdown("agent-hooks-plan", "Agent Hooks Plan", "# Agent Hooks Plan\n"), 0o644)).To(Succeed())
		Expect(os.WriteFile(filepath.Join(rootDir, "source.md"), canonicalValidationMarkdown("source", "Source", "# Source\n\n[Plan](/plans/agent_hooks.PLAN.md)\n"), 0o644)).To(Succeed())

		result := ValidateWorkspaceMarkdownFiles(WorkspaceMarkdownValidationOptions{RootDir: rootDir})

		Expect(result).To(matchValidationResult(BeFalse(), HaveExactElements(
			matchValidationIssueAtPath(IssueCodeNonCanonicalMarkdownPath, "source"),
		)))
	})

	ginkgo.It("reports normalized route collisions instead of invalid slug errors", func() {
		rootDir := markdownValidationTempDir()
		Expect(os.MkdirAll(filepath.Join(rootDir, "plans"), 0o755)).To(Succeed())
		Expect(os.WriteFile(filepath.Join(rootDir, "plans", "foo_bar.md"), canonicalValidationMarkdown("foo-bar-a", "Foo Bar A", "# Foo Bar A\n"), 0o644)).To(Succeed())
		Expect(os.WriteFile(filepath.Join(rootDir, "plans", "foo-bar.md"), canonicalValidationMarkdown("foo-bar-b", "Foo Bar B", "# Foo Bar B\n"), 0o644)).To(Succeed())

		result := ValidateWorkspaceMarkdownFiles(WorkspaceMarkdownValidationOptions{RootDir: rootDir})

		Expect(result).To(matchValidationResult(BeFalse(), SatisfyAll(
			ContainElement(matchNormalizedRouteConflictIssue("plans/foo-bar.md", "plans/foo_bar.md")),
			Not(ContainElement(matchValidationIssueAtPath(IssueCodeInvalidSlug, "plans/foo_bar.md"))),
		)))
	})

	ginkgo.It("treats protocol-relative and schemed URLs as external links", func() {
		rootDir := markdownValidationTempDir()
		Expect(os.WriteFile(filepath.Join(rootDir, "source.md"), []byte("---\nleafwiki_id: source\nleafwiki_title: Source\n---\n# Source\n\n[CDN](//cdn.example.com/lib.md)\n[Obsidian](obsidian://open?vault=wiki)\n"), 0o644)).To(Succeed())

		result := ValidateWorkspaceMarkdownFiles(WorkspaceMarkdownValidationOptions{RootDir: rootDir})

		Expect(result.OK).To(BeTrue())
	})

	ginkgo.It("requires workspace links to resolve to filesystem targets", func() {
		rootDir := markdownValidationTempDir()
		Expect(os.WriteFile(filepath.Join(rootDir, "source.md"), []byte("---\nleafwiki_id: source\nleafwiki_title: Source\n---\n# Source\n\n[Stale](/stale-target)\n"), 0o644)).To(Succeed())

		result := ValidateWorkspaceMarkdownFiles(WorkspaceMarkdownValidationOptions{RootDir: rootDir})

		Expect(result).To(matchValidationResult(BeFalse(), HaveExactElements(
			matchValidationIssueAtPath(IssueCodeBrokenLink, "source"),
		)))
	})

	ginkgo.It("reports duplicate content page IDs with page metadata wording", func() {
		result := ValidateMarkdownContentWithOptions(tree.RoutePath("docs/page"), string(canonicalValidationMarkdown("duplicate-id", "Page", "# Page\n")), ContentValidationOptions{
			ExistingPageID: "current-id",
			PageIDExists: func(pageID tree.PageID) bool {
				return pageID == "duplicate-id"
			},
		})

		Expect(result).To(matchValidationResult(BeFalse(), HaveExactElements(
			matchDuplicatePageIDIssue(),
		)))
	})

	// - Malformed percent-encoding reports an invalid link instead of panicking
	ginkgo.It("reports invalid canonical links without panicking", func() {
		workspaceParent := markdownValidationTempDir()
		rootDir := filepath.Join(workspaceParent, "workspace")
		Expect(os.MkdirAll(rootDir, 0o755)).To(Succeed())
		Expect(os.WriteFile(filepath.Join(workspaceParent, "outside.md"), []byte("---\nleafwiki_id: outside\nleafwiki_title: Outside\n---\n# Outside\n"), 0o644)).To(Succeed())
		Expect(os.WriteFile(filepath.Join(rootDir, "source.md"), []byte("---\nleafwiki_id: source\nleafwiki_title: Source\n---\n# Source\n\n[Bad Encoding](/docs/%zz)\n[Escape](../outside.md)\n"), 0o644)).To(Succeed())

		result := ValidateWorkspaceMarkdownFiles(WorkspaceMarkdownValidationOptions{RootDir: rootDir})

		Expect(result).To(matchValidationResult(BeFalse(), HaveExactElements(
			matchValidationIssueAtPath(IssueCodeInvalidLink, "source"),
			matchValidationIssueAtPath(IssueCodeInvalidLink, "source"),
		)))
	})

	ginkgo.It("requires a page file for explicit section markdown links", func() {
		rootDir := markdownValidationTempDir()
		Expect(os.MkdirAll(filepath.Join(rootDir, "docs", "sync"), 0o755)).To(Succeed())
		Expect(os.WriteFile(filepath.Join(rootDir, "docs", "a.md"), []byte("---\nleafwiki_id: docs-a\nleafwiki_title: Docs A\n---\n# Docs A\n\n[Sync page](/docs/sync.md)\n"), 0o644)).To(Succeed())
		Expect(os.WriteFile(filepath.Join(rootDir, "docs", "sync", "index.md"), []byte("---\nleafwiki_id: docs-sync\nleafwiki_title: Docs Sync\n---\n# Docs Sync\n"), 0o644)).To(Succeed())

		result := ValidateWorkspaceMarkdownFiles(WorkspaceMarkdownValidationOptions{RootDir: rootDir})

		Expect(result).To(matchValidationResult(BeFalse(), HaveExactElements(
			matchValidationIssueCode(IssueCodeBrokenLink),
		)))
	})

	ginkgo.It("allows canonical same-basename page and section links without ambiguity", func() {
		rootDir := markdownValidationTempDir()
		Expect(os.MkdirAll(filepath.Join(rootDir, "docs", "sync"), 0o755)).To(Succeed())
		Expect(os.WriteFile(filepath.Join(rootDir, "docs", "a.md"), []byte("---\nleafwiki_id: docs-a\nleafwiki_title: Docs A\n---\n# Docs A\n\n[Sync section](/docs/sync)\n[Sync page](/docs/sync.md)\n"), 0o644)).To(Succeed())
		Expect(os.WriteFile(filepath.Join(rootDir, "docs", "sync.md"), []byte("---\nleafwiki_id: page-sync\nleafwiki_title: Sync Page\n---\n# Sync Page\n"), 0o644)).To(Succeed())
		Expect(os.WriteFile(filepath.Join(rootDir, "docs", "sync", "index.md"), []byte("---\nleafwiki_id: section-sync\nleafwiki_title: Sync Section\n---\n# Sync Section\n"), 0o644)).To(Succeed())

		result := ValidateWorkspaceMarkdownFiles(WorkspaceMarkdownValidationOptions{RootDir: rootDir})

		Expect(result).To(matchValidationResult(BeTrue(), SatisfyAll(
			Not(ContainElement(matchValidationIssueCode(IssueCodePathConflict))),
			Not(ContainElement(matchValidationIssueCode(IssueCodeAmbiguousLegacyLink))),
		)))
	})

	ginkgo.It("accepts canonical section links when a same-basename page also exists", func() {
		rootDir := markdownValidationTempDir()
		Expect(os.MkdirAll(filepath.Join(rootDir, "docs", "sync"), 0o755)).To(Succeed())
		Expect(os.WriteFile(filepath.Join(rootDir, "docs", "a.md"), []byte("---\nleafwiki_id: docs-a\nleafwiki_title: Docs A\n---\n# Docs A\n\n[Sync section](/docs/sync)\n[Sync page](/docs/sync.md)\n"), 0o644)).To(Succeed())
		Expect(os.WriteFile(filepath.Join(rootDir, "docs", "sync.md"), []byte("---\nleafwiki_id: page-sync\nleafwiki_title: Sync Page\n---\n# Sync Page\n"), 0o644)).To(Succeed())
		Expect(os.WriteFile(filepath.Join(rootDir, "docs", "sync", "index.md"), []byte("---\nleafwiki_id: section-sync\nleafwiki_title: Sync Section\n---\n# Sync Section\n"), 0o644)).To(Succeed())

		result := ValidateWorkspaceMarkdownFiles(WorkspaceMarkdownValidationOptions{RootDir: rootDir})

		Expect(result.OK).To(BeTrue())
	})

	// - Case mismatch is invalid
	ginkgo.It("uses exact case-sensitive target matching for workspace links", func() {
		rootDir := markdownValidationTempDir()
		Expect(os.MkdirAll(filepath.Join(rootDir, "docs"), 0o755)).To(Succeed())
		Expect(os.WriteFile(filepath.Join(rootDir, "docs", "a.md"), []byte("---\nleafwiki_id: docs-a\nleafwiki_title: Docs A\n---\n# Docs A\n\n[Sync](/docs/sync.md)\n"), 0o644)).To(Succeed())
		Expect(os.WriteFile(filepath.Join(rootDir, "docs", "Sync.md"), []byte("---\nleafwiki_id: docs-sync\nleafwiki_title: Docs Sync\n---\n# Docs Sync\n"), 0o644)).To(Succeed())

		result := ValidateWorkspaceMarkdownFiles(WorkspaceMarkdownValidationOptions{RootDir: rootDir})

		Expect(result.Issues).To(ContainElement(matchValidationIssueAtPath(IssueCodeBrokenLink, "docs/a")))
	})

	// - Old extensionless page link is reported as non-canonical when migration cannot resolve it
	ginkgo.It("rejects unmigrated extensionless page links as non-canonical", func() {
		rootDir := markdownValidationTempDir()
		Expect(os.MkdirAll(filepath.Join(rootDir, "docs"), 0o755)).To(Succeed())
		Expect(os.WriteFile(filepath.Join(rootDir, "docs", "a.md"), []byte("---\nleafwiki_id: docs-a\nleafwiki_title: Docs A\n---\n# Docs A\n\n[B](/docs/b)\n"), 0o644)).To(Succeed())
		Expect(os.WriteFile(filepath.Join(rootDir, "docs", "b.md"), []byte("---\nleafwiki_id: docs-b\nleafwiki_title: Docs B\n---\n# Docs B\n"), 0o644)).To(Succeed())

		result := ValidateWorkspaceMarkdownFiles(WorkspaceMarkdownValidationOptions{RootDir: rootDir})

		Expect(result).To(matchValidationResult(BeFalse(), HaveExactElements(
			matchValidationIssueAtPath(IssueCodeNonCanonicalLink, "docs/a"),
		)))
	})

	ginkgo.It("uses the markdown link resolver without falling back to legacy route resolution", func() {
		result := ValidateMarkdownContentWithOptions(tree.RoutePath("docs/a"), "[B](/docs/b)\n", ContentValidationOptions{
			ResolveMarkdownLink: func(destination string) (tree.PageID, tree.NodeKind, bool, IssueCode) {
				if destination == "/docs/b" {
					return "docs/b", tree.NodeKindPage, true, ""
				}
				return "", "", false, IssueCodeBrokenLink
			},
		})

		Expect(result).To(matchValidationResult(BeFalse(), HaveExactElements(
			matchValidationIssueCode(IssueCodeNonCanonicalLink),
		)))
	})
})
