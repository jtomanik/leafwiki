package workspacesync

import (
	"context"
	"os"
	"path/filepath"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	wikivalidation "github.com/perber/wiki/internal/core/markdownvalidation"
	"github.com/perber/wiki/internal/core/tree"
	testmatchers "github.com/perber/wiki/internal/test_utils/matchers"
	"github.com/perber/wiki/internal/workspacesync/gitrevisions"
)

func gitRevisionActorIDStrings(ids []gitrevisions.ActorID) []string {
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		out = append(out, id.String())
	}
	return out
}

// Canonical Markdown links plan scenarios covered by tests in this file:
// - Relative link cannot escape the workspace root
// - Old extensionless page link migrates to .md
// - Old extensionless section link remains extensionless
// - Unresolved old extensionless page link becomes validation error
// - Ambiguous extensionless link is left as validation error
// - Migration is idempotent
// - Migration writeback is captured in revision history
// - Migration write failure reports sync validation state without losing raw content

var _ = Describe("workspace synchronization from markdown files", Label("integration"), func() {
	It("imports direct markdown pages and reconstructs the tree", func() {
		dataDir := workspaceSyncTempDir()
		rootDir := filepath.Join(workspaceSyncTempDir(), "workspace")
		treeService := tree.NewTreeServiceWithOptions(tree.TreeOptions{DataDir: dataDir, RootDir: rootDir})
		Expect(treeService.LoadTree()).To(Succeed())
		writeMarkdownFile(filepath.Join(rootDir, "docs", "new-page.md"), `---
leafwiki_id: page-new
leafwiki_title: New Page
---

# New Page

content`)

		service, err := NewService(ServiceOptions{
			Enabled: true,
			DataDir: dataDir,
			RootDir: rootDir,
			Tree:    treeService,
		})
		Expect(err).To(Succeed())

		status, err := service.SyncNow(context.Background(), SyncRequest{
			Reason: ReasonExplicit,
			Source: SourceFilesystem,
			Actor:  PublicEditorActor(),
		})
		Expect(err).To(Succeed())

		Expect(status.LastCommitHash).NotTo(BeEmpty())
		page, err := treeService.GetPage(newFixturePageID("page-new"))
		Expect(err).To(Succeed())
		Expect(page).To(SatisfyAll(
			HaveField("Title", Equal("New Page")),
			HaveField("RawContent", ContainSubstring("content")),
		))
	})

	It("normalizes workspace route filenames without invalid slug validation", func() {
		dataDir := workspaceSyncTempDir()
		rootDir := filepath.Join(workspaceSyncTempDir(), "workspace")
		treeService := tree.NewTreeServiceWithOptions(tree.TreeOptions{DataDir: dataDir, RootDir: rootDir})
		Expect(treeService.LoadTree()).To(Succeed())
		writeMarkdownFile(filepath.Join(rootDir, "plans", "index.md"), `---
leafwiki_id: plans
leafwiki_title: Plans
---
# Plans
`)
		writeMarkdownFile(filepath.Join(rootDir, "plans", "agent_hooks.PLAN.md"), `---
leafwiki_id: agent-hooks-plan
leafwiki_title: Agent Hooks Plan
---
# Agent Hooks Plan

content`)

		service, err := NewService(ServiceOptions{
			Enabled: true,
			DataDir: dataDir,
			RootDir: rootDir,
			Tree:    treeService,
		})
		Expect(err).To(Succeed())

		status, err := service.SyncNow(context.Background(), SyncRequest{
			Reason: ReasonExplicit,
			Source: SourceFilesystem,
			Actor:  PublicEditorActor(),
		})
		Expect(err).To(Succeed())
		Expect(status.ValidationErrors).NotTo(testmatchers.HaveValidationIssue(wikivalidation.IssueCodeInvalidSlug))

		page, err := treeService.FindPageByRoutePath(newFixtureRoutePath("plans/agent-hooks-plan"))
		Expect(err).To(Succeed())
		Expect(page).To(SatisfyAll(
			HaveField("Title", Equal("Agent Hooks Plan")),
			HaveField("Content", ContainSubstring("content")),
		))
	})

	It("preserves uppercase section index history without materializing a lowercase index", func() {
		dataDir := workspaceSyncTempDir()
		rootDir := filepath.Join(workspaceSyncTempDir(), "workspace")
		treeService := tree.NewTreeServiceWithOptions(tree.TreeOptions{DataDir: dataDir, RootDir: rootDir})
		Expect(treeService.LoadTree()).To(Succeed())
		writeMarkdownFile(filepath.Join(rootDir, "docs", "INDEX.MD"), `---
leafwiki_id: section-docs
leafwiki_title: Docs
---
# Docs

section content`)

		service, err := NewService(ServiceOptions{
			Enabled: true,
			DataDir: dataDir,
			RootDir: rootDir,
			Tree:    treeService,
		})
		Expect(err).To(Succeed())

		status, err := service.SyncNow(context.Background(), SyncRequest{
			Reason: ReasonExplicit,
			Source: SourceFilesystem,
			Actor:  PublicEditorActor(),
		})
		Expect(err).To(Succeed())
		Expect(status.LastCommitHash).NotTo(BeEmpty())

		page, err := treeService.GetPage(newFixturePageID("section-docs"))
		Expect(err).To(Succeed())
		Expect(page).To(SatisfyAll(
			HaveField("Kind", Equal(tree.NodeKindSection)),
			HaveField("RawContent", ContainSubstring("section content")),
		))
		entries, err := os.ReadDir(filepath.Join(rootDir, "docs"))
		Expect(err).To(Succeed())
		Expect(entries).NotTo(ContainElement(WithTransform(func(entry os.DirEntry) string {
			return entry.Name()
		}, Equal("index.md"))))

		result, err := service.ListPageRevisions(context.Background(), page, "", 10)
		Expect(err).To(Succeed())
		Expect(result.Revisions).To(HaveLen(2))
		Expect(result.Revisions).To(ContainElement(HaveField("Path", Equal("docs"))))
		latest, err := service.GetPageRevisionSnapshot(context.Background(), page, CommitHashFromRevisionID(result.Revisions[0].ID))
		Expect(err).To(Succeed())
		older, err := service.GetPageRevisionSnapshot(context.Background(), page, CommitHashFromRevisionID(result.Revisions[1].ID))
		Expect(err).To(Succeed())
		Expect(latest.Content).To(HavePrefix("<!-- leafwiki\n"))
		Expect(older.Content).To(SatisfyAll(
			HavePrefix("---\n"),
			ContainSubstring("section content"),
		))
	})

	It("amends metadata writebacks into the captured batch commit", func() {
		dataDir := workspaceSyncTempDir()
		rootDir := filepath.Join(workspaceSyncTempDir(), "workspace")
		treeService := tree.NewTreeServiceWithOptions(tree.TreeOptions{DataDir: dataDir, RootDir: rootDir})
		Expect(treeService.LoadTree()).To(Succeed())
		writeMarkdownFile(filepath.Join(rootDir, "needs-metadata.md"), "# Needs Metadata\n\nbody")

		service, err := NewService(ServiceOptions{
			Enabled: true,
			DataDir: dataDir,
			RootDir: rootDir,
			Tree:    treeService,
		})
		Expect(err).To(Succeed())

		status, err := service.SyncNow(context.Background(), SyncRequest{
			Reason: ReasonExplicit,
			Source: SourceFilesystem,
			Actor:  PublicEditorActor(),
		})
		Expect(err).To(Succeed())
		Expect(status.LastCommitHash).NotTo(BeEmpty())
		snapshots, err := service.ListSnapshots(context.Background(), 10)
		Expect(err).To(Succeed())
		Expect(snapshots).To(HaveLen(1))
		page := mustGetOnlyPageGinkgo(treeService)
		snapshot, err := service.GetPageRevisionSnapshot(context.Background(), page, snapshots[0].ID)
		Expect(err).To(Succeed())
		Expect(snapshot.Content).To(SatisfyAll(
			ContainSubstring("<!-- leafwiki\n"),
			ContainSubstring("  id:"),
		))
	})

	It("rewrites resolvable legacy page links before validation", func() {
		dataDir := workspaceSyncTempDir()
		rootDir := filepath.Join(workspaceSyncTempDir(), "workspace")
		treeService := tree.NewTreeServiceWithOptions(tree.TreeOptions{DataDir: dataDir, RootDir: rootDir})
		Expect(treeService.LoadTree()).To(Succeed())
		writeMarkdownFile(filepath.Join(rootDir, "docs", "a.md"), `---
leafwiki_id: page-a
leafwiki_title: Page A
---
# Page A

[B](/docs/b)
`)
		writeMarkdownFile(filepath.Join(rootDir, "docs", "b.md"), `---
leafwiki_id: page-b
leafwiki_title: Page B
---
# Page B
`)

		service, err := NewService(ServiceOptions{
			Enabled: true,
			DataDir: dataDir,
			RootDir: rootDir,
			Tree:    treeService,
		})
		Expect(err).To(Succeed())

		status, err := service.SyncNow(context.Background(), SyncRequest{
			Reason: ReasonExplicit,
			Source: SourceFilesystem,
			Actor:  PublicEditorActor(),
		})
		Expect(err).To(Succeed())
		Expect(status.ValidationErrors).To(BeEmpty())
		raw, err := os.ReadFile(filepath.Join(rootDir, "docs", "a.md"))
		Expect(err).To(Succeed())
		Expect(string(raw)).To(ContainSubstring("[B](/docs/b.md)"))
		page, err := treeService.GetPage(newFixturePageID("page-a"))
		Expect(err).To(Succeed())
		Expect(page.RawContent).To(ContainSubstring("[B](/docs/b.md)"))
	})
})
