package workspacesync

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/perber/wiki/internal/core/markdown"
	"github.com/perber/wiki/internal/core/markdownlinks"
	wikivalidation "github.com/perber/wiki/internal/core/markdownvalidation"
	"github.com/perber/wiki/internal/core/revision"
	"github.com/perber/wiki/internal/core/tree"
	"github.com/perber/wiki/internal/links"
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

var _ = Describe("workspace synchronization from markdown files", func() {
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
		page, err := treeService.GetPage("page-new")
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

		page, err := treeService.FindPageByRoutePath("plans/agent-hooks-plan")
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

		page, err := treeService.GetPage("section-docs")
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
		page, err := treeService.GetPage("page-a")
		Expect(err).To(Succeed())
		Expect(page.RawContent).To(ContainSubstring("[B](/docs/b.md)"))
	})
})

var _ = Describe("canonical markdown link migration", func() {
	It("adds the configured root prefix to absolute markdown links without repeat revisions", func() {
		dataDir := workspaceSyncTempDir()
		repoRoot := workspaceSyncTempDir()
		rootDir := filepath.Join(repoRoot, "docs")
		treeService := tree.NewTreeServiceWithOptions(tree.TreeOptions{DataDir: dataDir, RootDir: rootDir})
		Expect(treeService.LoadTree()).To(Succeed())
		writeMarkdownFile(filepath.Join(rootDir, "a.md"), `---
leafwiki_id: page-a
leafwiki_title: Page A
---
# Page A

[Glossary](/sync/glossary.md)
`)
		writeMarkdownFile(filepath.Join(rootDir, "sync", "glossary.md"), `---
leafwiki_id: glossary
leafwiki_title: Glossary
---
# Glossary
`)

		service, err := NewService(ServiceOptions{
			Enabled:                true,
			DataDir:                dataDir,
			RootDir:                rootDir,
			Tree:                   treeService,
			MarkdownLinkRootPrefix: "/docs",
		})
		Expect(err).To(Succeed())
		for i := 0; i < 2; i++ {
			status, err := service.SyncNow(context.Background(), SyncRequest{
				Reason: ReasonExplicit,
				Source: SourceFilesystem,
				Actor:  PublicEditorActor(),
			})
			Expect(err).To(Succeed())
			Expect(status.ValidationErrors).To(BeEmpty())
		}
		raw, err := os.ReadFile(filepath.Join(rootDir, "a.md"))
		Expect(err).To(Succeed())
		Expect(string(raw)).To(ContainSubstring("[Glossary](/docs/sync/glossary.md)"))
		snapshots, err := service.ListSnapshots(context.Background(), 10)
		Expect(err).To(Succeed())
		Expect(snapshots).To(HaveLen(2))
	})

	It("migrates relative legacy page links while preserving canonical relative links", func() {
		dataDir := workspaceSyncTempDir()
		rootDir := filepath.Join(workspaceSyncTempDir(), "workspace")
		treeService := tree.NewTreeServiceWithOptions(tree.TreeOptions{DataDir: dataDir, RootDir: rootDir})
		Expect(treeService.LoadTree()).To(Succeed())
		writeMarkdownFile(filepath.Join(rootDir, "docs", "source", "a.md"), `---
leafwiki_id: page-a-relative
leafwiki_title: Page A Relative
---
# Page A Relative

[Legacy](../b)
[Canonical](../b.md)
`)
		writeMarkdownFile(filepath.Join(rootDir, "docs", "b.md"), `---
leafwiki_id: page-b-relative
leafwiki_title: Page B Relative
---
# Page B Relative
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
		raw, err := os.ReadFile(filepath.Join(rootDir, "docs", "source", "a.md"))
		Expect(err).To(Succeed())
		content := string(raw)
		Expect(strings.Count(content, "../b.md")).To(Equal(2))
		Expect(content).NotTo(ContainSubstring("](../b)"))
	})

	It("indexes duplicate legacy and canonical syntaxes as a single healthy target", func() {
		dataDir := workspaceSyncTempDir()
		rootDir := filepath.Join(workspaceSyncTempDir(), "workspace")
		treeService := tree.NewTreeServiceWithOptions(tree.TreeOptions{DataDir: dataDir, RootDir: rootDir})
		Expect(treeService.LoadTree()).To(Succeed())
		linkStore, err := links.NewLinksStore(dataDir)
		Expect(err).To(Succeed())
		DeferCleanup(func() {
			Expect(linkStore.Close()).To(Succeed())
		})
		linkService := links.NewLinkService(dataDir, treeService, linkStore)
		writeMarkdownFile(filepath.Join(rootDir, "docs", "a.md"), `---
leafwiki_id: page-a-duplicate-syntax
leafwiki_title: Page A Duplicate Syntax
---
# Page A Duplicate Syntax

[Legacy](/docs/b)
[Canonical](/docs/b.md)
`)
		writeMarkdownFile(filepath.Join(rootDir, "docs", "b.md"), `---
leafwiki_id: page-b-duplicate-syntax
leafwiki_title: Page B Duplicate Syntax
---
# Page B Duplicate Syntax
`)

		service, err := NewService(ServiceOptions{
			Enabled: true,
			DataDir: dataDir,
			RootDir: rootDir,
			Tree:    treeService,
			AfterSync: func() error {
				return linkService.IndexAllPages()
			},
		})
		Expect(err).To(Succeed())

		status, err := service.SyncNow(context.Background(), SyncRequest{
			Reason: ReasonExplicit,
			Source: SourceFilesystem,
			Actor:  PublicEditorActor(),
		})
		Expect(err).To(Succeed())
		Expect(status.ValidationErrors).To(BeEmpty())
		pageA, err := treeService.GetPage("page-a-duplicate-syntax")
		Expect(err).To(Succeed())
		linkStatus, err := linkService.GetLinkStatusForPage(pageA.ID, pageA.CalculateRoutePath())
		Expect(err).To(Succeed())
		Expect(linkStatus).To(SatisfyAll(
			HaveField("Counts", SatisfyAll(
				HaveField("Outgoings", Equal(1)),
				HaveField("BrokenOutgoings", BeZero()),
			)),
			HaveField("Outgoings", ContainElement(HaveField("ToPageID", Equal(newFixturePageID("page-b-duplicate-syntax"))))),
		))
	})

	It("keeps raw incoming content and canonical writeback revisions", func() {
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

		_, err = service.SyncNow(context.Background(), SyncRequest{
			Reason: ReasonExplicit,
			Source: SourceFilesystem,
			Actor:  PublicEditorActor(),
		})
		Expect(err).To(Succeed())

		page, err := treeService.GetPage("page-a")
		Expect(err).To(Succeed())
		revisions, err := service.ListPageRevisions(context.Background(), page, "", 10)
		Expect(err).To(Succeed())
		Expect(revisions.Revisions).To(HaveLen(2))

		var snapshotContents []string
		for _, rev := range revisions.Revisions {
			snapshot, err := service.GetPageRevisionSnapshot(context.Background(), page, CommitHashFromRevisionID(rev.ID))
			Expect(err).To(Succeed())
			snapshotContents = append(snapshotContents, snapshot.Content)
		}
		Expect(snapshotContents).To(ContainElement(ContainSubstring("[B](/docs/b)\n")))
		Expect(snapshotContents).To(ContainElement(ContainSubstring("[B](/docs/b.md)")))
	})

	It("migrates complete legacy metadata once and keeps the raw revision", func() {
		dataDir := workspaceSyncTempDir()
		rootDir := filepath.Join(workspaceSyncTempDir(), "workspace")
		treeService := tree.NewTreeServiceWithOptions(tree.TreeOptions{DataDir: dataDir, RootDir: rootDir})
		Expect(treeService.LoadTree()).To(Succeed())
		writeMarkdownFile(filepath.Join(rootDir, "docs", "page.md"), `---
leafwiki_id: complete-legacy-page
leafwiki_title: Complete Legacy Page
leafwiki_created_at: 2026-03-21T10:15:30Z
leafwiki_updated_at: 2026-03-21T11:16:31Z
leafwiki_creator_id: alice
leafwiki_last_author_id: bob
---
# Complete Legacy Page

Fully populated legacy metadata.
`)

		service, err := NewService(ServiceOptions{
			Enabled: true,
			DataDir: dataDir,
			RootDir: rootDir,
			Tree:    treeService,
		})
		Expect(err).To(Succeed())

		_, err = service.SyncNow(context.Background(), SyncRequest{
			Reason: ReasonExplicit,
			Source: SourceFilesystem,
			Actor:  PublicEditorActor(),
		})
		Expect(err).To(Succeed())

		rawBytes, err := os.ReadFile(filepath.Join(rootDir, "docs", "page.md"))
		Expect(err).To(Succeed())
		raw := string(rawBytes)
		Expect(raw).To(HavePrefix("<!-- leafwiki\n"))
		Expect(raw).NotTo(HavePrefix("---\n"))
		Expect(raw).NotTo(ContainSubstring("leafwiki_id: complete-legacy-page"))
		parsed, _, err := markdown.ParsePageDocument(raw)
		Expect(err).To(Succeed())
		Expect(parsed.Metadata.Page).To(SatisfyAll(
			HaveField("ID", Equal("complete-legacy-page")),
			HaveField("Title", Equal("Complete Legacy Page")),
			HaveField("CreatedAt", Equal("2026-03-21T10:15:30Z")),
			HaveField("CreatorID", Equal("alice")),
			HaveField("LastAuthorID", Equal("bob")),
		))

		page, err := treeService.GetPage("complete-legacy-page")
		Expect(err).To(Succeed())
		revisions, err := service.ListPageRevisions(context.Background(), page, "", 10)
		Expect(err).To(Succeed())
		Expect(revisions.Revisions).To(HaveLen(2))
		latest, err := service.GetPageRevisionSnapshot(context.Background(), page, CommitHashFromRevisionID(revisions.Revisions[0].ID))
		Expect(err).To(Succeed())
		older, err := service.GetPageRevisionSnapshot(context.Background(), page, CommitHashFromRevisionID(revisions.Revisions[1].ID))
		Expect(err).To(Succeed())
		Expect(latest.Content).To(HavePrefix("<!-- leafwiki\n"))
		Expect(older.Content).To(HavePrefix("---\n"))

		_, err = service.SyncNow(context.Background(), SyncRequest{
			Reason: ReasonExplicit,
			Source: SourceFilesystem,
			Actor:  PublicEditorActor(),
		})
		Expect(err).To(Succeed())
		snapshots, err := service.ListSnapshots(context.Background(), 10)
		Expect(err).To(Succeed())
		Expect(snapshots).To(HaveLen(2))
	})
})

var _ = Describe("canonical markdown migration rollback", func() {
	It("restores earlier rewrites when a later file cannot be written", func() {
		rootDir := filepath.Join(workspaceSyncTempDir(), "workspace")
		service := &Service{rootDir: rootDir}

		firstPath := filepath.Join(rootDir, "a", "source.md")
		secondDir := filepath.Join(rootDir, "readonly")
		secondPath := filepath.Join(secondDir, "source.md")
		firstOriginal := `---
leafwiki_id: first-source
leafwiki_title: First Source
---
# First Source

[Target](/targets/first)
`
		secondOriginal := `---
leafwiki_id: second-source
leafwiki_title: Second Source
---
# Second Source

[Target](/targets/second)
`
		writeMarkdownFile(firstPath, firstOriginal)
		writeMarkdownFile(secondPath, secondOriginal)
		writeMarkdownFile(filepath.Join(rootDir, "targets", "first.md"), `---
leafwiki_id: first-target
leafwiki_title: First Target
---
# First Target
`)
		writeMarkdownFile(filepath.Join(rootDir, "targets", "second.md"), `---
leafwiki_id: second-target
leafwiki_title: Second Target
---
# Second Target
`)
		Expect(os.Chmod(secondPath, 0o444)).To(Succeed())
		Expect(os.Chmod(secondDir, 0o555)).To(Succeed())
		DeferCleanup(func() {
			_ = os.Chmod(secondDir, 0o755)
			_ = os.Chmod(secondPath, 0o644)
		})
		probePath := filepath.Join(secondDir, ".probe")
		if err := os.WriteFile(probePath, []byte("probe"), 0o644); err == nil {
			_ = os.Remove(probePath)
			Skip("filesystem permits writes to read-only test directory")
		}

		Expect(canonicalMigrationFailsWithoutCommit(service)).To(Succeed())
		Expect(readFileStringGinkgo(firstPath)).To(Equal(firstOriginal))
		Expect(readFileStringGinkgo(secondPath)).To(Equal(secondOriginal))
	})

	It("rolls back committed renames when atomic rewrite fails", func() {
		rootDir := workspaceSyncTempDir()
		firstPath := filepath.Join(rootDir, "first.md")
		blockingDir := filepath.Join(rootDir, "blocking")
		firstOriginal := "# First\n\n[Target](/target)\n"
		firstCanonical := "# First\n\n[Target](/target.md)\n"
		writeMarkdownFile(firstPath, firstOriginal)
		Expect(os.MkdirAll(blockingDir, 0o755)).To(Succeed())

		rewriteErr := writeCanonicalMarkdownRewritesAtomically([]canonicalMarkdownRewrite{
			{
				Path:     firstPath,
				Original: []byte(firstOriginal),
				Content:  []byte(firstCanonical),
				Mode:     0o644,
			},
			{
				Path:    blockingDir,
				Content: []byte("not a markdown file"),
				Mode:    0o644,
			},
		})

		linkErr, err := atomicRewriteLinkError(rewriteErr)
		Expect(err).To(Succeed())
		Expect(linkErr).To(HaveField("New", Equal(blockingDir)))
		Expect(readFileStringGinkgo(firstPath)).To(Equal(firstOriginal))
	})

	It("restores file and tree content when writeback capture fails", func() {
		dataDir := workspaceSyncTempDir()
		rootDir := filepath.Join(workspaceSyncTempDir(), "workspace")
		treeService := tree.NewTreeServiceWithOptions(tree.TreeOptions{DataDir: dataDir, RootDir: rootDir})
		Expect(treeService.LoadTree()).To(Succeed())
		sourcePath := filepath.Join(rootDir, "docs", "a.md")
		sourceOriginal := `---
leafwiki_id: page-a
leafwiki_title: Page A
---
# Page A

[B](/docs/b)
`
		writeMarkdownFile(sourcePath, sourceOriginal)
		writeMarkdownFile(filepath.Join(rootDir, "docs", "b.md"), `---
leafwiki_id: page-b
leafwiki_title: Page B
---
# Page B
`)
		captureErr := errors.New("writeback capture failed")
		service, err := NewService(ServiceOptions{
			Enabled: true,
			DataDir: dataDir,
			RootDir: rootDir,
			Tree:    treeService,
			Store: &fakeRevisionStore{
				capture:        &gitrevisions.Commit{Hash: "initial-commit"},
				captureErr:     captureErr,
				captureErrCall: 2,
			},
		})
		Expect(err).To(Succeed())

		_, err = service.SyncNow(context.Background(), SyncRequest{
			Reason: ReasonExplicit,
			Source: SourceFilesystem,
			Actor:  PublicEditorActor(),
		})

		Expect(err).To(MatchError(captureErr))
		Expect(readFileStringGinkgo(sourcePath)).To(SatisfyAll(
			ContainSubstring("[B](/docs/b)\n"),
			Not(ContainSubstring("[B](/docs/b.md)")),
		))
		page, err := treeService.GetPage("page-a")
		Expect(err).To(Succeed())
		Expect(page.RawContent).To(SatisfyAll(
			ContainSubstring("[B](/docs/b)\n"),
			Not(ContainSubstring("[B](/docs/b.md)")),
		))
	})
})

var _ = Describe("workspace sync failure during canonical writeback", func() {
	It("reports rewrite failure before derived indexes rebuild", func() {
		dataDir := workspaceSyncTempDir()
		rootDir := filepath.Join(workspaceSyncTempDir(), "workspace")
		treeService := tree.NewTreeServiceWithOptions(tree.TreeOptions{DataDir: dataDir, RootDir: rootDir})
		Expect(treeService.LoadTree()).To(Succeed())
		sourcePath := filepath.Join(rootDir, "docs", "a.md")
		sourceOriginal := `---
leafwiki_id: page-a
leafwiki_title: Page A
---
# Page A

[B](/docs/b)
`
		writeMarkdownFile(sourcePath, sourceOriginal)
		writeMarkdownFile(filepath.Join(rootDir, "docs", "b.md"), `---
leafwiki_id: page-b
leafwiki_title: Page B
---
# Page B
`)

		writeErr := errors.New("canonical migration write failed")
		previousWriter := canonicalMarkdownRewriteWriter
		canonicalMarkdownRewriteWriter = func([]canonicalMarkdownRewrite) error {
			return writeErr
		}
		DeferCleanup(func() {
			canonicalMarkdownRewriteWriter = previousWriter
		})

		derivedRebuilds := 0
		service, err := NewService(ServiceOptions{
			Enabled: true,
			DataDir: dataDir,
			RootDir: rootDir,
			Tree:    treeService,
			Store:   &fakeRevisionStore{capture: &gitrevisions.Commit{Hash: "initial-commit"}},
			AfterSync: func() error {
				derivedRebuilds++
				return nil
			},
		})
		Expect(err).To(Succeed())

		_, err = service.SyncNow(context.Background(), SyncRequest{
			Reason: ReasonExplicit,
			Source: SourceFilesystem,
			Actor:  PublicEditorActor(),
		})

		Expect(err).To(MatchError(writeErr))
		Expect(readFileStringGinkgo(sourcePath)).To(SatisfyAll(
			ContainSubstring("[B](/docs/b)\n"),
			Not(ContainSubstring("[B](/docs/b.md)")),
		))
		page, err := treeService.GetPage("page-a")
		Expect(err).To(Succeed())
		Expect(page.RawContent).To(SatisfyAll(
			ContainSubstring("[B](/docs/b)\n"),
			Not(ContainSubstring("[B](/docs/b.md)")),
		))
		Expect(derivedRebuilds).To(BeZero())
	})

	It("keeps failed metadata writeback out of tree state and revision capture", func() {
		dataDir := workspaceSyncTempDir()
		rootDir := filepath.Join(workspaceSyncTempDir(), "workspace")
		treeService := tree.NewTreeServiceWithOptions(tree.TreeOptions{DataDir: dataDir, RootDir: rootDir})
		Expect(treeService.LoadTree()).To(Succeed())
		sourcePath := filepath.Join(rootDir, "legacy-metadata.md")
		writeMarkdownFile(sourcePath, `---
leafwiki_id: legacy-metadata
leafwiki_title: Legacy Metadata
leafwiki_created_at: "2026-06-13T10:00:00Z"
leafwiki_updated_at: "2026-06-13T11:00:00Z"
---
# Legacy Metadata

body
`)
		Expect(os.Chmod(rootDir, 0o500)).To(Succeed())
		DeferCleanup(func() {
			_ = os.Chmod(rootDir, 0o700)
		})

		store := &fakeRevisionStore{capture: &gitrevisions.Commit{Hash: "initial-commit"}}
		derivedRebuilds := 0
		service, err := NewService(ServiceOptions{
			Enabled: true,
			DataDir: dataDir,
			RootDir: rootDir,
			Tree:    treeService,
			Store:   store,
			AfterSync: func() error {
				derivedRebuilds++
				return nil
			},
		})
		Expect(err).To(Succeed())

		status, err := service.SyncNow(context.Background(), SyncRequest{
			Reason: ReasonExplicit,
			Source: SourceFilesystem,
			Actor:  PublicEditorActor(),
		})

		Expect(err).To(Succeed())
		Expect(status.LastError).To(ContainSubstring(filepath.Base(sourcePath)))
		Expect(derivedRebuilds).To(BeZero())
		Expect(store.captureCalls).To(Equal(1))
		_, err = treeService.GetPage("legacy-metadata")
		Expect(err).To(MatchError(tree.ErrPageNotFound))
		Expect(readFileStringGinkgo(sourcePath)).NotTo(HavePrefix("<!-- leafwiki\n"))
	})
})

var _ = Describe("canonical link validation during workspace sync", func() {
	It("does not create another migration revision after links are canonical", func() {
		dataDir := workspaceSyncTempDir()
		rootDir := filepath.Join(workspaceSyncTempDir(), "workspace")
		treeService := tree.NewTreeServiceWithOptions(tree.TreeOptions{DataDir: dataDir, RootDir: rootDir})
		Expect(treeService.LoadTree()).To(Succeed())
		writeMarkdownFile(filepath.Join(rootDir, "a.md"), `---
leafwiki_id: page-a
leafwiki_title: Page A
---
# Page A

[B](/b)
`)
		writeMarkdownFile(filepath.Join(rootDir, "b.md"), `---
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
		for range 2 {
			_, err := service.SyncNow(context.Background(), SyncRequest{
				Reason: ReasonExplicit,
				Source: SourceFilesystem,
				Actor:  PublicEditorActor(),
			})
			Expect(err).To(Succeed())
		}

		snapshots, err := service.ListSnapshots(context.Background(), 10)
		Expect(err).To(Succeed())
		Expect(snapshots).To(HaveLen(2))
	})

	It("keeps unresolved legacy links in content and reports a broken link issue", func() {
		dataDir := workspaceSyncTempDir()
		rootDir := filepath.Join(workspaceSyncTempDir(), "workspace")
		treeService := tree.NewTreeServiceWithOptions(tree.TreeOptions{DataDir: dataDir, RootDir: rootDir})
		Expect(treeService.LoadTree()).To(Succeed())
		sourcePath := filepath.Join(rootDir, "docs", "a.md")
		writeMarkdownFile(sourcePath, `---
leafwiki_id: page-a
leafwiki_title: Page A
---
# Page A

[Missing](/docs/missing)
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

		Expect(readFileStringGinkgo(sourcePath)).To(ContainSubstring("[Missing](/docs/missing)"))
		Expect(status.ValidationErrors).To(SatisfyAll(
			HaveLen(1),
			testmatchers.HaveValidationIssue(wikivalidation.IssueCodeBrokenLink),
			ContainElement(HaveField("Path", Equal("docs/a"))),
		))
	})

	It("leaves invalid canonical links unchanged and reports invalid link issues", func() {
		dataDir := workspaceSyncTempDir()
		workspaceParent := workspaceSyncTempDir()
		rootDir := filepath.Join(workspaceParent, "workspace")
		treeService := tree.NewTreeServiceWithOptions(tree.TreeOptions{DataDir: dataDir, RootDir: rootDir})
		Expect(treeService.LoadTree()).To(Succeed())
		Expect(os.WriteFile(filepath.Join(workspaceParent, "outside.md"), []byte("---\nleafwiki_id: outside\nleafwiki_title: Outside\n---\n# Outside\n"), 0o644)).To(Succeed())
		sourcePath := filepath.Join(rootDir, "docs", "a.md")
		sourceOriginal := `---
leafwiki_id: page-a
leafwiki_title: Page A
---
# Page A

[Bad Encoding](/docs/%zz)
[Escape](../../outside.md)
`
		writeMarkdownFile(sourcePath, sourceOriginal)

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
		for _, originalLink := range []string{"[Bad Encoding](/docs/%zz)", "[Escape](../../outside.md)"} {
			Expect(readFileStringGinkgo(sourcePath)).To(ContainSubstring(originalLink))
		}
		page, err := treeService.GetPage("page-a")
		Expect(err).To(Succeed())
		for _, originalLink := range []string{"[Bad Encoding](/docs/%zz)", "[Escape](../../outside.md)"} {
			Expect(page.RawContent).To(ContainSubstring(originalLink))
		}
		Expect(status.ValidationErrors).To(SatisfyAll(
			HaveLen(2),
			testmatchers.HaveValidationIssues(wikivalidation.IssueCodeInvalidLink, wikivalidation.IssueCodeInvalidLink),
			HaveEach(HaveField("Path", Equal("docs/a"))),
		))
	})
})

var _ = Describe("ambiguous legacy markdown links during workspace sync", func() {
	It("keeps ambiguous extensionless links unchanged and reports an ambiguity issue", func() {
		dataDir := workspaceSyncTempDir()
		rootDir := filepath.Join(workspaceSyncTempDir(), "workspace")
		treeService := tree.NewTreeServiceWithOptions(tree.TreeOptions{DataDir: dataDir, RootDir: rootDir})
		Expect(treeService.LoadTree()).To(Succeed())
		sourcePath := filepath.Join(rootDir, "docs", "a.md")
		sourceOriginal := `---
leafwiki_id: page-a
leafwiki_title: Page A
---
# Page A

[Sync](/docs/sync)
`
		writeMarkdownFile(sourcePath, sourceOriginal)
		writeMarkdownFile(filepath.Join(rootDir, "docs", "sync.md"), `---
leafwiki_id: sync-page
leafwiki_title: Sync Page
---
# Sync Page
`)
		writeMarkdownFile(filepath.Join(rootDir, "docs", "sync", "index.md"), `---
leafwiki_id: sync-section
leafwiki_title: Sync Section
---
# Sync Section
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
		Expect(readFileStringGinkgo(sourcePath)).To(ContainSubstring("[Sync](/docs/sync)"))
		page, err := treeService.GetPage("page-a")
		Expect(err).To(Succeed())
		Expect(page.RawContent).To(ContainSubstring("[Sync](/docs/sync)"))
		Expect(status.ValidationErrors).To(SatisfyAll(
			testmatchers.HaveValidationIssue(wikivalidation.IssueCodeAmbiguousLegacyLink),
			ContainElement(HaveField("Path", Equal("docs/a"))),
		))
	})

	It("keeps ambiguity diagnostics when normal validation also finds broken links", func() {
		dataDir := workspaceSyncTempDir()
		rootDir := filepath.Join(workspaceSyncTempDir(), "workspace")
		treeService := tree.NewTreeServiceWithOptions(tree.TreeOptions{DataDir: dataDir, RootDir: rootDir})
		Expect(treeService.LoadTree()).To(Succeed())
		writeMarkdownFile(filepath.Join(rootDir, "docs", "a.md"), `---
leafwiki_id: page-a
leafwiki_title: Page A
---
# Page A

[Sync](/docs/sync)
[Missing](/docs/missing)
`)
		writeMarkdownFile(filepath.Join(rootDir, "docs", "sync.md"), `---
leafwiki_id: sync-page
leafwiki_title: Sync Page
---
# Sync Page
`)
		writeMarkdownFile(filepath.Join(rootDir, "docs", "sync", "index.md"), `---
leafwiki_id: sync-section
leafwiki_title: Sync Section
---
# Sync Section
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

		Expect(status.ValidationErrors).To(testmatchers.HaveValidationIssues(
			wikivalidation.IssueCodeAmbiguousLegacyLink,
			wikivalidation.IssueCodeBrokenLink,
		))
	})

	It("normalizes ambiguous link validation paths to route paths", func() {
		validationErrors := canonicalMigrationValidationErrors(workspaceSyncTempDir(), "plans/agent_hooks.PLAN.md", []markdownlinks.Issue{{
			Code:        markdownlinks.IssueCodeAmbiguousLegacyLink,
			Destination: "/plans/sync",
		}})

		Expect(validationErrors).To(SatisfyAll(
			HaveLen(1),
			testmatchers.HaveValidationIssue(wikivalidation.IssueCodeAmbiguousLegacyLink),
			ContainElement(HaveField("Path", Equal("plans/agent-hooks-plan"))),
		))
	})
})

var _ = Describe("workspace sync changed markdown tracking", func() {
	It("canonicalizes section trailing slashes without repeat revisions", func() {
		dataDir := workspaceSyncTempDir()
		rootDir := filepath.Join(workspaceSyncTempDir(), "workspace")
		treeService := tree.NewTreeServiceWithOptions(tree.TreeOptions{DataDir: dataDir, RootDir: rootDir})
		Expect(treeService.LoadTree()).To(Succeed())
		sourcePath := filepath.Join(rootDir, "docs", "a.md")
		writeMarkdownFile(sourcePath, `---
leafwiki_id: page-a
leafwiki_title: Page A
---
# Page A

[Sync](/docs/sync/)
`)
		writeMarkdownFile(filepath.Join(rootDir, "docs", "sync", "index.md"), `---
leafwiki_id: section-sync
leafwiki_title: Sync
---
# Sync
`)

		service, err := NewService(ServiceOptions{
			Enabled: true,
			DataDir: dataDir,
			RootDir: rootDir,
			Tree:    treeService,
		})
		Expect(err).To(Succeed())
		for range 2 {
			_, err := service.SyncNow(context.Background(), SyncRequest{
				Reason: ReasonExplicit,
				Source: SourceFilesystem,
				Actor:  PublicEditorActor(),
			})
			Expect(err).To(Succeed())
		}

		Expect(readFileStringGinkgo(sourcePath)).To(SatisfyAll(
			ContainSubstring("[Sync](/docs/sync)"),
			Not(ContainSubstring("/docs/sync/")),
		))
		snapshots, err := service.ListSnapshots(context.Background(), 10)
		Expect(err).To(Succeed())
		Expect(snapshots).To(HaveLen(2))
	})

	It("preserves the original changed markdown count when metadata writebacks are amended", func() {
		dataDir := workspaceSyncTempDir()
		rootDir := filepath.Join(workspaceSyncTempDir(), "workspace")
		treeService := tree.NewTreeServiceWithOptions(tree.TreeOptions{DataDir: dataDir, RootDir: rootDir})
		Expect(treeService.LoadTree()).To(Succeed())
		writeMarkdownFile(filepath.Join(rootDir, "already-has-metadata.md"), `<!-- leafwiki
version: 1
page:
  id: page-ready
  title: Already Has Metadata
  created_at: 2026-06-07T10:00:00Z
  updated_at: 2026-06-07T10:00:00Z
-->

# Already Has Metadata
`)
		writeMarkdownFile(filepath.Join(rootDir, "needs-metadata.md"), "# Needs Metadata\n\nbody")

		service, err := NewService(ServiceOptions{
			Enabled: true,
			DataDir: dataDir,
			RootDir: rootDir,
			Tree:    treeService,
		})
		Expect(err).To(Succeed())

		_, err = service.SyncNow(context.Background(), SyncRequest{
			Reason: ReasonExplicit,
			Source: SourceFilesystem,
			Actor:  PublicEditorActor(),
		})
		Expect(err).To(Succeed())

		snapshots, err := service.ListSnapshots(context.Background(), 10)
		Expect(err).To(Succeed())
		Expect(snapshots).To(ConsistOf(HaveField("ChangedMarkdownCount", Equal(2))))
	})

	It("records the latest commit hash changed paths and tree reconstruction", func() {
		fakeTree := &fakeTreeReconstructor{}
		service, err := NewService(ServiceOptions{
			Enabled: true,
			Tree:    fakeTree,
			Store: &fakeRevisionStore{
				capture: &gitrevisions.Commit{
					Hash:                 "abc123",
					ChangedMarkdownCount: 2,
					ChangedMarkdownPaths: []string{"docs/a.md", "docs/b.md"},
				},
			},
		})
		Expect(err).To(Succeed())

		status, err := service.SyncNow(context.Background(), SyncRequest{
			Reason: ReasonExplicit,
			Source: SourceFilesystem,
			Actor:  PublicEditorActor(),
		})
		Expect(err).To(Succeed())

		Expect(status).To(SatisfyAll(
			HaveField("LastCommitHash", Equal(CommitHash("abc123"))),
			HaveField("RecentChangedMarkdownPaths", Equal([]string{"docs/a.md", "docs/b.md"})),
		))
		Expect(fakeTree.reconstructCount()).To(Equal(1))
	})
})

var _ = Describe("workspace sync startup and snapshot listing", func() {
	It("logs each startup phase as it runs", func() {
		var logs bytes.Buffer
		service, err := NewService(ServiceOptions{
			Enabled: true,
			RootDir: workspaceSyncTempDir(),
			Tree:    &fakeTreeReconstructor{},
			Store: &fakeRevisionStore{
				capture: &gitrevisions.Commit{
					Hash:                 "startup-commit",
					ChangedMarkdownPaths: []string{"docs/a.md"},
				},
			},
			Log: slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelInfo})),
		})
		Expect(err).To(Succeed())

		_, err = service.SyncNow(context.Background(), SyncRequest{
			Reason: ReasonStartup,
			Source: SourceFilesystem,
			Actor:  PublicEditorActor(),
		})
		Expect(err).To(Succeed())

		logText := logs.String()
		Expect(logText).To(SatisfyAll(
			ContainSubstring(`phase=capture_snapshot`),
			ContainSubstring(`phase=reconstruct_tree`),
			ContainSubstring(`phase=canonical_link_migration`),
			ContainSubstring(`phase=capture_writebacks`),
			ContainSubstring(`phase=validate_and_after_sync`),
			ContainSubstring("workspace sync startup phase started"),
			ContainSubstring("workspace sync startup phase completed"),
		))
	})

	It("propagates changed markdown path read errors while listing snapshots", func() {
		errChangedPathTrailerReadFailed := errors.New("path trailer read failed")
		service, err := NewService(ServiceOptions{
			Enabled: true,
			Tree:    &fakeTreeReconstructor{},
			Store: &fakeRevisionStore{
				commits: []gitrevisions.Commit{
					{Hash: "abc123", ChangedMarkdownCount: 1},
				},
				changedPathsErr: errChangedPathTrailerReadFailed,
			},
		})
		Expect(err).To(Succeed())

		_, err = service.ListSnapshotPage(context.Background(), CommitHash(""), 10)
		Expect(err).To(MatchError(errChangedPathTrailerReadFailed))
	})

	It("does not read changed paths past the requested snapshot page", func() {
		service, err := NewService(ServiceOptions{
			Enabled: true,
			Tree:    &fakeTreeReconstructor{},
			Store: &fakeRevisionStore{
				commits: []gitrevisions.Commit{
					{Hash: "returned", ChangedMarkdownCount: 1},
					{Hash: "sentinel", ChangedMarkdownCount: 1},
				},
				changedPaths: map[CommitHash][]string{
					"returned": {"returned.md"},
				},
				changedPathsErrByHash: map[CommitHash]error{
					"sentinel": errors.New("sentinel diff should not be read"),
				},
			},
		})
		Expect(err).To(Succeed())

		page, err := service.ListSnapshotPage(context.Background(), CommitHash(""), 1)
		Expect(err).To(Succeed())
		Expect(page).To(SatisfyAll(
			HaveField("Snapshots", ConsistOf(HaveField("ID", Equal(CommitHash("returned"))))),
			HaveField("NextCursor", Equal(CommitHash("returned"))),
		))
	})
})

var _ = Describe("workspace sync status under snapshot listing", func() {
	It("serves status while changed-path snapshot listing is blocked", func() {
		store := &fakeRevisionStore{
			capture: &gitrevisions.Commit{Hash: "sync-commit"},
			commits: []gitrevisions.Commit{
				{Hash: "slow-snapshot", ChangedMarkdownCount: 1},
			},
			changedPaths: map[CommitHash][]string{
				"slow-snapshot": {"slow.md"},
			},
			changedPathsStarted: make(chan struct{}),
			unblockChangedPaths: make(chan struct{}),
		}
		service, err := NewService(ServiceOptions{
			Enabled: true,
			Tree:    &fakeTreeReconstructor{},
			Store:   store,
		})
		Expect(err).To(Succeed())

		changedPathsStarted := store.changedPathsStarted
		listDone := make(chan error, 1)
		go func() {
			_, err := service.ListSnapshotPage(context.Background(), CommitHash(""), 1)
			listDone <- err
		}()
		Eventually(changedPathsStarted).Within(time.Second).Should(BeClosed())
		unblockChangedPaths := closeOnce(store.unblockChangedPaths)
		DeferCleanup(unblockChangedPaths)

		syncDone := make(chan error, 1)
		go func() {
			_, err := service.SyncNow(context.Background(), SyncRequest{
				Reason: ReasonExplicit,
				Source: SourceFilesystem,
				Actor:  PublicEditorActor(),
			})
			syncDone <- err
		}()

		statusDone := make(chan SyncStatus, 1)
		go func() {
			statusDone <- service.Status()
		}()

		Eventually(statusDone).WithTimeout(200 * time.Millisecond).Should(Receive(matchEnabledWorkspaceSyncStatus()))

		unblockChangedPaths()
		Eventually(listDone).Should(Receive(Succeed()))
		Eventually(syncDone).Should(Receive(Succeed()))
	})

	It("runs after-sync hooks when reconstruction only reports validation warnings", func() {
		dataDir := workspaceSyncTempDir()
		rootDir := filepath.Join(workspaceSyncTempDir(), "workspace")
		treeService := tree.NewTreeServiceWithOptions(tree.TreeOptions{DataDir: dataDir, RootDir: rootDir})
		Expect(treeService.LoadTree()).To(Succeed())
		writeMarkdownFile(filepath.Join(rootDir, "valid-page.md"), `---
leafwiki_id: valid-page
leafwiki_title: Valid Page
---

# Valid Page
`)
		writeMarkdownFile(filepath.Join(rootDir, "!!!.md"), "---\nleafwiki_id: invalid-slug\nleafwiki_title: Invalid Slug\n---\n# Invalid Slug\n")
		var afterSyncCalls int32
		service, err := NewService(ServiceOptions{
			Enabled: true,
			DataDir: dataDir,
			RootDir: rootDir,
			Tree:    treeService,
			AfterSync: func() error {
				atomic.AddInt32(&afterSyncCalls, 1)
				return nil
			},
		})
		Expect(err).To(Succeed())

		status, err := service.SyncNow(context.Background(), SyncRequest{
			Reason: ReasonExplicit,
			Source: SourceFilesystem,
			Actor:  PublicEditorActor(),
		})
		Expect(err).To(Succeed())

		Expect(atomic.LoadInt32(&afterSyncCalls)).To(Equal(int32(1)))
		Expect(status.ValidationErrors).To(testmatchers.HaveValidationIssue(wikivalidation.IssueCodeInvalidSlug))
	})

	It("records validation error paths extracted from markdown reconstruction failures", func() {
		fakeTree := &fakeTreeReconstructor{err: errors.New(`duplicate leafwiki_id "dup" in /workspace/a.md and /workspace/docs/b.md`)}
		service, err := NewService(ServiceOptions{
			Enabled: true,
			RootDir: "/workspace",
			Tree:    fakeTree,
			Store:   &fakeRevisionStore{capture: &gitrevisions.Commit{Hash: "abc123"}},
		})
		Expect(err).To(Succeed())

		status, err := service.SyncNow(context.Background(), SyncRequest{
			Reason: ReasonExplicit,
			Source: SourceFilesystem,
			Actor:  PublicEditorActor(),
		})
		Expect(err).To(Succeed())

		Expect(status.ValidationErrors).To(ConsistOf(
			HaveField("Path", Equal("a.md")),
			HaveField("Path", Equal("docs/b.md")),
		))
	})
})

var _ = Describe("workspace sync validation and revision metadata", func() {
	It("reports duplicate canonical page IDs as structured validation issues", func() {
		dataDir := workspaceSyncTempDir()
		rootDir := filepath.Join(workspaceSyncTempDir(), "workspace")
		treeService := tree.NewTreeServiceWithOptions(tree.TreeOptions{DataDir: dataDir, RootDir: rootDir})
		writeMarkdownFile(filepath.Join(rootDir, "duplicate-a.md"), `<!-- leafwiki
version: 1
page:
  id: duplicate-page-id
  title: Duplicate A
-->

# Duplicate A
`)
		writeMarkdownFile(filepath.Join(rootDir, "duplicate-b.md"), `<!-- leafwiki
version: 1
page:
  id: duplicate-page-id
  title: Duplicate B
-->

# Duplicate B
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

		Expect(status.ValidationErrors).To(SatisfyAll(
			testmatchers.HaveValidationIssue(wikivalidation.IssueCodeDuplicateLeafwikiID),
			ContainElement(SatisfyAll(
				HaveField("Path", Equal("duplicate-b.md")),
				HaveField("Severity", Equal(wikivalidation.IssueSeverityError)),
			)),
		))
	})

	It("records primary and additional actors on the captured git commit", func() {
		dataDir := workspaceSyncTempDir()
		rootDir := filepath.Join(workspaceSyncTempDir(), "workspace")
		treeService := tree.NewTreeServiceWithOptions(tree.TreeOptions{DataDir: dataDir, RootDir: rootDir})
		writeMarkdownFile(filepath.Join(rootDir, "page.md"), "---\nleafwiki_id: page\nleafwiki_title: Page\n---\n# Page\n")
		store, err := gitrevisions.Open(gitrevisions.StoreOptions{DataDir: dataDir, RootDir: rootDir})
		Expect(err).To(Succeed())
		service, err := NewService(ServiceOptions{
			Enabled: true,
			DataDir: dataDir,
			RootDir: rootDir,
			Tree:    treeService,
			Store:   store,
		})
		Expect(err).To(Succeed())

		status, err := service.SyncNow(context.Background(), SyncRequest{
			Reason: ReasonExplicit,
			Source: SourceWeb,
			Actor:  Actor{ID: "alice", Name: "Alice", Email: "alice@example.test"},
			AdditionalActors: []Actor{
				{ID: "bob", Name: "Bob", Email: "bob@example.test"},
			},
		})
		Expect(err).To(Succeed())

		commit, err := store.GetCommit(context.Background(), status.LastCommitHash)
		Expect(err).To(Succeed())
		Expect(commit).To(SatisfyAll(
			HaveField("AuthorID", Equal(gitrevisions.ActorID("alice"))),
			HaveField("ActorIDs", WithTransform(gitRevisionActorIDStrings, Equal([]string{"alice", "bob"}))),
		))
	})

	It("stops before tree reconstruction when git capture fails", func() {
		captureErr := errors.New("git storage read-only")
		fakeTree := &fakeTreeReconstructor{}
		service, err := NewService(ServiceOptions{
			Enabled: true,
			Tree:    fakeTree,
			Store:   &fakeRevisionStore{captureErr: captureErr},
		})
		Expect(err).To(Succeed())

		_, err = service.SyncNow(context.Background(), SyncRequest{
			Reason: ReasonExplicit,
			Source: SourceFilesystem,
			Actor:  PublicEditorActor(),
		})
		Expect(err).To(MatchError(captureErr))
		Expect(fakeTree.reconstructCount()).To(BeZero())
	})

	It("uses commit author metadata when listing page revisions", func() {
		createdAt := time.Date(2026, 6, 7, 10, 0, 0, 0, time.UTC)
		page := &tree.Page{PageNode: &tree.PageNode{
			ID:    "page-1",
			Title: "Page One",
			Slug:  "page-one",
			Kind:  tree.NodeKindPage,
		}}
		store := &fakeRevisionStore{
			commits: []gitrevisions.Commit{{
				Hash:       "commit-1",
				Message:    "LeafWiki workspace sync",
				AuthorID:   "alice",
				AuthorName: "Alice",
				CreatedAt:  createdAt,
			}},
			filesAt: map[CommitHash]map[string]string{
				"commit-1": {"page-one.md": "# Page One\n"},
			},
			changedPaths: map[CommitHash][]string{
				"commit-1": {"page-one.md"},
			},
		}
		service, err := NewService(ServiceOptions{
			Enabled: true,
			Tree:    &fakeTreeReconstructor{},
			Store:   store,
		})
		Expect(err).To(Succeed())

		result, err := service.ListPageRevisions(context.Background(), page, "", 10)
		Expect(err).To(Succeed())
		Expect(result.Revisions).To(ConsistOf(SatisfyAll(
			HaveField("AuthorID", Equal("alice")),
			HaveField("CreatedAt", Equal(createdAt)),
		)))
	})
})

var _ = Describe("page revision listing", func() {
	It("paginates page commits and returns the next cursor for the following page", func() {
		page := &tree.Page{PageNode: &tree.PageNode{
			ID:    "page-a",
			Title: "Page A",
			Slug:  "page-a",
			Kind:  tree.NodeKindPage,
		}}
		store := &fakeRevisionStore{
			commits: []gitrevisions.Commit{
				{Hash: "page-a-change-3", AuthorID: "alice"},
				{Hash: "page-a-change-2", AuthorID: "alice"},
				{Hash: "page-a-change-1", AuthorID: "alice"},
			},
			filesAt: map[CommitHash]map[string]string{
				"page-a-change-3": {
					"page-a.md": "---\nleafwiki_id: page-a\nleafwiki_title: Page A\n---\n# Page A v3\n",
				},
				"page-a-change-2": {
					"page-a.md": "---\nleafwiki_id: page-a\nleafwiki_title: Page A\n---\n# Page A v2\n",
				},
				"page-a-change-1": {
					"page-a.md": "---\nleafwiki_id: page-a\nleafwiki_title: Page A\n---\n# Page A v1\n",
				},
			},
			changedPaths: map[CommitHash][]string{
				"page-a-change-3": {"page-a.md"},
				"page-a-change-2": {"page-a.md"},
				"page-a-change-1": {"page-a.md"},
			},
		}
		service, err := NewService(ServiceOptions{
			Enabled: true,
			Tree:    &fakeTreeReconstructor{},
			Store:   store,
		})
		Expect(err).To(Succeed())

		firstPage, err := service.ListPageRevisions(context.Background(), page, "", 2)
		Expect(err).To(Succeed())
		Expect(firstPage).To(SatisfyAll(
			HaveField("Revisions", WithTransform(revisionIDs, Equal([]string{"page-a-change-3", "page-a-change-2"}))),
			HaveField("NextCursor", Equal("page-a-change-2")),
		))

		secondPage, err := service.ListPageRevisions(context.Background(), page, firstPage.NextCursor, 2)
		Expect(err).To(Succeed())
		Expect(secondPage).To(SatisfyAll(
			HaveField("Revisions", WithTransform(revisionIDs, Equal([]string{"page-a-change-1"}))),
			HaveField("NextCursor", BeEmpty()),
		))
	})

	It("omits the next cursor when the final page exactly matches the limit", func() {
		page := &tree.Page{PageNode: &tree.PageNode{
			ID:    "page-a",
			Title: "Page A",
			Slug:  "page-a",
			Kind:  tree.NodeKindPage,
		}}
		store := &fakeRevisionStore{
			commits: []gitrevisions.Commit{
				{Hash: "page-a-change-2", AuthorID: "alice"},
				{Hash: "page-a-change-1", AuthorID: "alice"},
			},
			filesAt: map[CommitHash]map[string]string{
				"page-a-change-2": {
					"page-a.md": "---\nleafwiki_id: page-a\nleafwiki_title: Page A\n---\n# Page A v2\n",
				},
				"page-a-change-1": {
					"page-a.md": "---\nleafwiki_id: page-a\nleafwiki_title: Page A\n---\n# Page A v1\n",
				},
			},
			changedPaths: map[CommitHash][]string{
				"page-a-change-2": {"page-a.md"},
				"page-a-change-1": {"page-a.md"},
			},
		}
		service, err := NewService(ServiceOptions{
			Enabled: true,
			Tree:    &fakeTreeReconstructor{},
			Store:   store,
		})
		Expect(err).To(Succeed())

		result, err := service.ListPageRevisions(context.Background(), page, "", 2)
		Expect(err).To(Succeed())

		Expect(result).To(SatisfyAll(
			HaveField("Revisions", WithTransform(revisionIDs, Equal([]string{"page-a-change-2", "page-a-change-1"}))),
			HaveField("NextCursor", BeEmpty()),
		))
	})

	It("follows markdown renames by matching historical leafwiki IDs", func() {
		page := &tree.Page{PageNode: &tree.PageNode{
			ID:    "page-1",
			Title: "New Page",
			Slug:  "new-page",
			Kind:  tree.NodeKindPage,
		}}
		store := &fakeRevisionStore{
			commits: []gitrevisions.Commit{
				{Hash: "new-commit", AuthorID: "alice"},
				{Hash: "old-commit", AuthorID: "alice"},
			},
			filesAt: map[CommitHash]map[string]string{
				"new-commit": {
					"new-page.md": "---\nleafwiki_id: page-1\nleafwiki_title: New Page\n---\n# New Page\n",
				},
				"old-commit": {
					"old-page.md": "---\nleafwiki_id: page-1\nleafwiki_title: Old Page\n---\n# Old Page\n",
				},
			},
			changedPaths: map[CommitHash][]string{
				"new-commit": {"new-page.md"},
				"old-commit": {"old-page.md"},
			},
		}
		service, err := NewService(ServiceOptions{
			Enabled: true,
			Tree:    &fakeTreeReconstructor{},
			Store:   store,
		})
		Expect(err).To(Succeed())

		result, err := service.ListPageRevisions(context.Background(), page, "", 10)
		Expect(err).To(Succeed())

		Expect(result.Revisions).To(HaveExactElements(
			HaveField("Path", Equal("new-page")),
			HaveField("Path", Equal("old-page")),
		))
	})

	It("matches uppercase markdown extensions by historical leafwiki ID", func() {
		page := &tree.Page{PageNode: &tree.PageNode{
			ID:    "page-1",
			Title: "Page",
			Slug:  "page",
			Kind:  tree.NodeKindPage,
		}}
		store := &fakeRevisionStore{
			commits: []gitrevisions.Commit{{Hash: "uppercase-commit", AuthorID: "alice"}},
			filesAt: map[CommitHash]map[string]string{
				"uppercase-commit": {
					"Page.MD": "---\nleafwiki_id: page-1\nleafwiki_title: Page\n---\n# Page\n",
				},
			},
			changedPaths: map[CommitHash][]string{
				"uppercase-commit": {"Page.MD"},
			},
		}
		service, err := NewService(ServiceOptions{
			Enabled: true,
			Tree:    &fakeTreeReconstructor{},
			Store:   store,
		})
		Expect(err).To(Succeed())

		result, err := service.ListPageRevisions(context.Background(), page, "", 10)
		Expect(err).To(Succeed())

		Expect(result.Revisions).To(ConsistOf(HaveField("Path", Equal("Page"))))
	})

	It("matches raw historical paths without metadata through normalized routes", func() {
		page := &tree.Page{PageNode: &tree.PageNode{
			ID:    "page-1",
			Title: "Agent Hooks Plan",
			Slug:  "agent-hooks-plan",
			Kind:  tree.NodeKindPage,
			Parent: &tree.PageNode{
				ID:    "plans",
				Slug:  "plans",
				Title: "Plans",
				Kind:  tree.NodeKindSection,
			},
		}}
		store := &fakeRevisionStore{
			commits: []gitrevisions.Commit{{Hash: "raw-normalized-commit", AuthorID: "alice"}},
			filesAt: map[CommitHash]map[string]string{
				"raw-normalized-commit": {
					"plans/agent_hooks.PLAN.md": "# Agent Hooks Plan\n\nRaw content before writeback.\n",
				},
			},
			changedPaths: map[CommitHash][]string{
				"raw-normalized-commit": {"plans/agent_hooks.PLAN.md"},
			},
		}
		service, err := NewService(ServiceOptions{
			Enabled: true,
			Tree:    &fakeTreeReconstructor{},
			Store:   store,
		})
		Expect(err).To(Succeed())

		result, err := service.ListPageRevisions(context.Background(), page, "", 10)
		Expect(err).To(Succeed())

		Expect(result.Revisions).To(ConsistOf(HaveField("Path", Equal("plans/agent-hooks-plan"))))
		revisionID := result.Revisions[0].ID
		snapshot, err := service.GetPageRevisionSnapshot(context.Background(), page, CommitHashFromRevisionID(revisionID))
		Expect(err).To(Succeed())
		Expect(snapshot).To(SatisfyAll(
			HaveField("Revision", HaveField("Path", Equal("plans/agent-hooks-plan"))),
			HaveField("Content", ContainSubstring("Raw content before writeback.")),
		))
	})
})

var _ = Describe("page revision historical matching", func() {
	It("uses historical markdown metadata when current page metadata has changed", func() {
		page := &tree.Page{PageNode: &tree.PageNode{
			ID:    "page-1",
			Title: "Current Title",
			Slug:  "current-page",
			Kind:  tree.NodeKindPage,
		}}
		store := &fakeRevisionStore{
			commits: []gitrevisions.Commit{{Hash: "old-commit", AuthorID: "alice"}},
			filesAt: map[CommitHash]map[string]string{
				"old-commit": {
					"old-page.md": "---\nleafwiki_id: page-1\nleafwiki_title: Historical Title\n---\n# Historical Heading\n",
				},
			},
			changedPaths: map[CommitHash][]string{
				"old-commit": {"old-page.md"},
			},
		}
		service, err := NewService(ServiceOptions{
			Enabled: true,
			Tree:    &fakeTreeReconstructor{},
			Store:   store,
		})
		Expect(err).To(Succeed())

		result, err := service.ListPageRevisions(context.Background(), page, "", 1)
		Expect(err).To(Succeed())

		Expect(result.Revisions).To(ConsistOf(SatisfyAll(
			HaveField("Title", Equal("Historical Title")),
			HaveField("Slug", Equal(tree.Slug("old-page"))),
			HaveField("Kind", Equal(tree.NodeKindPage)),
			HaveField("Path", Equal("old-page")),
		)))
	})

	It("normalizes section index markdown paths to the section route", func() {
		page := &tree.Page{PageNode: &tree.PageNode{
			ID:    "section-1",
			Title: "Docs",
			Slug:  "docs",
			Kind:  tree.NodeKindSection,
		}}
		store := &fakeRevisionStore{
			commits: []gitrevisions.Commit{{Hash: "section-commit", AuthorID: "alice"}},
			filesAt: map[CommitHash]map[string]string{
				"section-commit": {
					"docs/index.md": "---\nleafwiki_id: section-1\nleafwiki_title: Historical Docs\n---\n# Historical Docs\n",
				},
			},
			changedPaths: map[CommitHash][]string{
				"section-commit": {"docs/index.md"},
			},
		}
		service, err := NewService(ServiceOptions{
			Enabled: true,
			Tree:    &fakeTreeReconstructor{},
			Store:   store,
		})
		Expect(err).To(Succeed())

		result, err := service.ListPageRevisions(context.Background(), page, "", 1)
		Expect(err).To(Succeed())

		Expect(result.Revisions).To(ConsistOf(SatisfyAll(
			HaveField("Title", Equal("Historical Docs")),
			HaveField("Slug", Equal(tree.Slug("docs"))),
			HaveField("Kind", Equal(tree.NodeKindSection)),
			HaveField("Path", Equal("docs")),
		)))
	})

	It("normalizes README markdown fallbacks to section routes", func() {
		page := &tree.Page{PageNode: &tree.PageNode{
			ID:    "section-guides",
			Title: "Guides",
			Slug:  "guides",
			Kind:  tree.NodeKindSection,
		}}
		store := &fakeRevisionStore{
			commits: []gitrevisions.Commit{{Hash: "readme-section-commit", AuthorID: "alice"}},
			filesAt: map[CommitHash]map[string]string{
				"readme-section-commit": {
					"guides/README.md": "---\nleafwiki_id: section-guides\nleafwiki_title: Historical Guides\n---\n# Historical Guides\n",
				},
			},
			changedPaths: map[CommitHash][]string{
				"readme-section-commit": {"guides/README.md"},
			},
		}
		service, err := NewService(ServiceOptions{
			Enabled: true,
			Tree:    &fakeTreeReconstructor{},
			Store:   store,
		})
		Expect(err).To(Succeed())

		result, err := service.ListPageRevisions(context.Background(), page, "", 1)
		Expect(err).To(Succeed())

		Expect(result.Revisions).To(ConsistOf(SatisfyAll(
			HaveField("Title", Equal("Historical Guides")),
			HaveField("Slug", Equal(tree.Slug("guides"))),
			HaveField("Kind", Equal(tree.NodeKindSection)),
			HaveField("Path", Equal("guides")),
		)))
	})

	It("maps README history as a page when the workspace directory has an index", func() {
		rootDir := workspaceSyncTempDir()
		writeMarkdownFile(filepath.Join(rootDir, "User Guides", "index.md"), "# User Guides\n")
		writeMarkdownFile(filepath.Join(rootDir, "User Guides", "README.md"), "---\nleafwiki_id: readme-page\nleafwiki_title: Historical README\n---\n# Historical README\n")

		page := &tree.Page{PageNode: &tree.PageNode{
			ID:    "readme-page",
			Title: "README",
			Slug:  "readme",
			Kind:  tree.NodeKindPage,
			Parent: &tree.PageNode{
				ID:    "user-guides",
				Slug:  "user-guides",
				Title: "User Guides",
				Kind:  tree.NodeKindSection,
			},
		}}
		store := &fakeRevisionStore{
			commits: []gitrevisions.Commit{{Hash: "readme-page-commit", AuthorID: "alice"}},
			filesAt: map[CommitHash]map[string]string{
				"readme-page-commit": {
					"User Guides/README.md": "---\nleafwiki_id: readme-page\nleafwiki_title: Historical README\n---\n# Historical README\n",
				},
			},
			changedPaths: map[CommitHash][]string{
				"readme-page-commit": {"User Guides/README.md"},
			},
		}
		service, err := NewService(ServiceOptions{
			Enabled: true,
			RootDir: rootDir,
			Tree:    &fakeTreeReconstructor{},
			Store:   store,
		})
		Expect(err).To(Succeed())

		result, err := service.ListPageRevisions(context.Background(), page, "", 1)
		Expect(err).To(Succeed())

		Expect(result.Revisions).To(ConsistOf(SatisfyAll(
			HaveField("Kind", Equal(tree.NodeKindPage)),
			HaveField("Path", Equal("user-guides/README")),
			HaveField("Slug", Equal(tree.Slug("README"))),
		)))
	})

	It("keeps the historical page kind after the current node becomes a section", func() {
		page := &tree.Page{PageNode: &tree.PageNode{
			ID:    "docs-1",
			Title: "Docs",
			Slug:  "docs",
			Kind:  tree.NodeKindSection,
		}}
		store := &fakeRevisionStore{
			commits: []gitrevisions.Commit{{Hash: "page-commit", AuthorID: "alice"}},
			filesAt: map[CommitHash]map[string]string{
				"page-commit": {
					"docs.md": "---\nleafwiki_id: docs-1\nleafwiki_title: Docs Page\n---\n# Docs Page\n",
				},
			},
			changedPaths: map[CommitHash][]string{
				"page-commit": {"docs.md"},
			},
		}
		service, err := NewService(ServiceOptions{
			Enabled: true,
			Tree:    &fakeTreeReconstructor{},
			Store:   store,
		})
		Expect(err).To(Succeed())

		result, err := service.ListPageRevisions(context.Background(), page, "", 1)
		Expect(err).To(Succeed())

		Expect(result.Revisions).To(ConsistOf(SatisfyAll(
			HaveField("Kind", Equal(tree.NodeKindPage)),
			HaveField("Path", Equal("docs")),
		)))
	})

	It("includes only commits that changed the requested page document", func() {
		page := &tree.Page{PageNode: &tree.PageNode{
			ID:    "page-a",
			Title: "Page A",
			Slug:  "page-a",
			Kind:  tree.NodeKindPage,
		}}
		store := &fakeRevisionStore{
			commits: []gitrevisions.Commit{
				{Hash: "page-b-change", AuthorID: "bob"},
				{Hash: "page-a-change", AuthorID: "alice"},
			},
			filesAt: map[CommitHash]map[string]string{
				"page-b-change": {
					"page-a.md": "---\nleafwiki_id: page-a\nleafwiki_title: Page A\n---\n# Page A\n",
					"page-b.md": "---\nleafwiki_id: page-b\nleafwiki_title: Page B\n---\n# Page B changed\n",
				},
				"page-a-change": {
					"page-a.md": "---\nleafwiki_id: page-a\nleafwiki_title: Page A\n---\n# Page A changed\n",
				},
			},
			changedPaths: map[CommitHash][]string{
				"page-b-change": {"page-b.md"},
				"page-a-change": {"page-a.md"},
			},
		}
		service, err := NewService(ServiceOptions{
			Enabled: true,
			Tree:    &fakeTreeReconstructor{},
			Store:   store,
		})
		Expect(err).To(Succeed())

		result, err := service.ListPageRevisions(context.Background(), page, "", 10)
		Expect(err).To(Succeed())

		Expect(result.Revisions).To(ConsistOf(HaveField("ID", Equal(revision.RevisionID("page-a-change")))))
	})

	It("scans past an unrelated head commit when the limit is one", func() {
		page := &tree.Page{PageNode: &tree.PageNode{
			ID:    "page-a",
			Title: "Page A",
			Slug:  "page-a",
			Kind:  tree.NodeKindPage,
		}}
		store := &fakeRevisionStore{
			commits: []gitrevisions.Commit{
				{Hash: "page-b-change", AuthorID: "bob"},
				{Hash: "page-a-change", AuthorID: "alice"},
			},
			filesAt: map[CommitHash]map[string]string{
				"page-b-change": {
					"page-a.md": "---\nleafwiki_id: page-a\nleafwiki_title: Page A\n---\n# Page A\n",
					"page-b.md": "---\nleafwiki_id: page-b\nleafwiki_title: Page B\n---\n# Page B changed\n",
				},
				"page-a-change": {
					"page-a.md": "---\nleafwiki_id: page-a\nleafwiki_title: Page A\n---\n# Page A changed\n",
				},
			},
			changedPaths: map[CommitHash][]string{
				"page-b-change": {"page-b.md"},
				"page-a-change": {"page-a.md"},
			},
		}
		service, err := NewService(ServiceOptions{
			Enabled: true,
			Tree:    &fakeTreeReconstructor{},
			Store:   store,
		})
		Expect(err).To(Succeed())

		result, err := service.ListPageRevisions(context.Background(), page, "", 1)
		Expect(err).To(Succeed())

		Expect(result.Revisions).To(ConsistOf(HaveField("ID", Equal(revision.RevisionID("page-a-change")))))
	})

	It("scans a large unrelated commit prefix to find the requested page", func() {
		page := &tree.Page{PageNode: &tree.PageNode{
			ID:    "page-a",
			Title: "Page A",
			Slug:  "page-a",
			Kind:  tree.NodeKindPage,
		}}
		store := &fakeRevisionStore{
			filesAt:      map[CommitHash]map[string]string{},
			changedPaths: map[CommitHash][]string{},
		}
		for i := 0; i < 1005; i++ {
			hash := "page-b-change-" + strconv.Itoa(i)
			store.commits = append(store.commits, gitrevisions.Commit{Hash: CommitHashFromString(hash), AuthorID: "bob"})
			store.filesAt[CommitHashFromString(hash)] = map[string]string{
				"page-a.md": "---\nleafwiki_id: page-a\nleafwiki_title: Page A\n---\n# Page A\n",
				"page-b.md": "---\nleafwiki_id: page-b\nleafwiki_title: Page B\n---\n# Page B changed\n",
			}
			store.changedPaths[CommitHashFromString(hash)] = []string{"page-b.md"}
		}
		store.commits = append(store.commits, gitrevisions.Commit{Hash: "page-a-change", AuthorID: "alice"})
		store.filesAt["page-a-change"] = map[string]string{
			"page-a.md": "---\nleafwiki_id: page-a\nleafwiki_title: Page A\n---\n# Page A changed\n",
		}
		store.changedPaths["page-a-change"] = []string{"page-a.md"}
		service, err := NewService(ServiceOptions{
			Enabled: true,
			Tree:    &fakeTreeReconstructor{},
			Store:   store,
		})
		Expect(err).To(Succeed())

		result, err := service.ListPageRevisions(context.Background(), page, "", 1)
		Expect(err).To(Succeed())

		Expect(result.Revisions).To(ConsistOf(HaveField("ID", Equal(revision.RevisionID("page-a-change")))))
	})

	It("stops scanning after it has a page revision and confirmed next cursor", func() {
		page := &tree.Page{PageNode: &tree.PageNode{
			ID:    "page-a",
			Title: "Page A",
			Slug:  "page-a",
			Kind:  tree.NodeKindPage,
		}}
		store := &fakeRevisionStore{
			commits: []gitrevisions.Commit{
				{Hash: "page-a-change-2", AuthorID: "alice"},
				{Hash: "page-a-change-1", AuthorID: "alice"},
			},
			filesAt: map[CommitHash]map[string]string{
				"page-a-change-2": {
					"page-a.md": "---\nleafwiki_id: page-a\nleafwiki_title: Page A\n---\n# Page A changed again\n",
				},
				"page-a-change-1": {
					"page-a.md": "---\nleafwiki_id: page-a\nleafwiki_title: Page A\n---\n# Page A changed\n",
				},
			},
			changedPaths: map[CommitHash][]string{
				"page-a-change-2": {"page-a.md"},
				"page-a-change-1": {"page-a.md"},
			},
		}
		for i := 0; i < 1005; i++ {
			hash := "page-b-change-" + strconv.Itoa(i)
			store.commits = append(store.commits, gitrevisions.Commit{Hash: CommitHashFromString(hash), AuthorID: "bob"})
			store.filesAt[CommitHashFromString(hash)] = map[string]string{
				"page-a.md": "---\nleafwiki_id: page-a\nleafwiki_title: Page A\n---\n# Page A\n",
				"page-b.md": "---\nleafwiki_id: page-b\nleafwiki_title: Page B\n---\n# Page B changed\n",
			}
			store.changedPaths[CommitHashFromString(hash)] = []string{"page-b.md"}
		}
		service, err := NewService(ServiceOptions{
			Enabled: true,
			Tree:    &fakeTreeReconstructor{},
			Store:   store,
		})
		Expect(err).To(Succeed())

		result, err := service.ListPageRevisions(context.Background(), page, "", 1)
		Expect(err).To(Succeed())

		Expect(result).To(SatisfyAll(
			HaveField("Revisions", ConsistOf(HaveField("ID", Equal(revision.RevisionID("page-a-change-2"))))),
			HaveField("NextCursor", Equal("page-a-change-2")),
		))
		Expect(store.scannedCommits).To(Equal(2))
	})
})

var _ = Describe("page revision store access", func() {
	It("scans document history without loading full trees", func() {
		page := &tree.Page{PageNode: &tree.PageNode{
			ID:    "page-a",
			Title: "Page A",
			Slug:  "page-a",
			Kind:  tree.NodeKindPage,
		}}
		store := &fakeRevisionStore{
			commits: []gitrevisions.Commit{
				{Hash: "page-b-change", AuthorID: "bob"},
				{Hash: "page-a-change", AuthorID: "alice"},
			},
			filesAt: map[CommitHash]map[string]string{
				"page-b-change": {
					"page-a.md": "---\nleafwiki_id: page-a\nleafwiki_title: Page A\n---\n# Page A\n",
					"page-b.md": "---\nleafwiki_id: page-b\nleafwiki_title: Page B\n---\n# Page B changed\n",
				},
				"page-a-change": {
					"page-a.md": "---\nleafwiki_id: page-a\nleafwiki_title: Page A\n---\n# Page A changed\n",
				},
			},
			changedPaths: map[CommitHash][]string{
				"page-b-change": {"page-b.md"},
				"page-a-change": {"page-a.md"},
			},
		}
		service, err := NewService(ServiceOptions{
			Enabled: true,
			Tree:    &fakeTreeReconstructor{},
			Store:   store,
		})
		Expect(err).To(Succeed())

		result, err := service.ListPageRevisions(context.Background(), page, "", 1)
		Expect(err).To(Succeed())

		Expect(result.Revisions).To(ConsistOf(HaveField("ID", Equal(revision.RevisionID("page-a-change")))))
		Expect(store.filesAtCalls).To(BeZero())
	})

	It("serves status while revision listing waits on the store", func() {
		page := &tree.Page{PageNode: &tree.PageNode{
			ID:    "page-a",
			Title: "Page A",
			Slug:  "page-a",
			Kind:  tree.NodeKindPage,
		}}
		store := &fakeRevisionStore{
			commits: []gitrevisions.Commit{{Hash: "page-a-change", AuthorID: "alice"}},
			filesAt: map[CommitHash]map[string]string{
				"page-a-change": {
					"page-a.md": "---\nleafwiki_id: page-a\nleafwiki_title: Page A\n---\n# Page A changed\n",
				},
			},
			changedPaths: map[CommitHash][]string{
				"page-a-change": {"page-a.md"},
			},
			changedContentsStarted: make(chan struct{}),
			unblockChangedContents: make(chan struct{}),
		}
		service, err := NewService(ServiceOptions{
			Enabled: true,
			Tree:    &fakeTreeReconstructor{},
			Store:   store,
		})
		Expect(err).To(Succeed())

		done := make(chan error, 1)
		go func() {
			_, err := service.ListPageRevisions(context.Background(), page, "", 1)
			done <- err
		}()
		Eventually(store.changedContentsStarted).Within(time.Second).Should(BeClosed())
		unblockChangedContents := closeOnce(store.unblockChangedContents)
		DeferCleanup(unblockChangedContents)

		statusDone := make(chan SyncStatus, 1)
		go func() {
			statusDone <- service.Status()
		}()

		Eventually(statusDone).WithTimeout(200 * time.Millisecond).Should(Receive(matchEnabledWorkspaceSyncStatus()))
		unblockChangedContents()
		Eventually(done).Should(Receive(Succeed()))
	})

	It("rejects snapshots for commits that did not change the page document", func() {
		page := &tree.Page{PageNode: &tree.PageNode{
			ID:    "page-a",
			Title: "Page A",
			Slug:  "page-a",
			Kind:  tree.NodeKindPage,
		}}
		store := &fakeRevisionStore{
			commits: []gitrevisions.Commit{{Hash: "page-b-change"}},
			filesAt: map[CommitHash]map[string]string{
				"page-b-change": {
					"page-a.md": "---\nleafwiki_id: page-a\nleafwiki_title: Page A\n---\n# Page A\n",
					"page-b.md": "---\nleafwiki_id: page-b\nleafwiki_title: Page B\n---\n# Page B changed\n",
				},
			},
			changedPaths: map[CommitHash][]string{
				"page-b-change": {"page-b.md"},
			},
		}
		service, err := NewService(ServiceOptions{
			Enabled: true,
			Tree:    &fakeTreeReconstructor{},
			Store:   store,
		})
		Expect(err).To(Succeed())

		_, err = service.GetPageRevisionSnapshot(context.Background(), page, CommitHash("page-b-change"))
		Expect(err).To(Satisfy(func(err error) bool {
			return errors.Is(err, ErrWorkspaceSyncDocumentUnchanged)
		}))
	})

	It("serves status while snapshot loading waits on the store", func() {
		page := &tree.Page{PageNode: &tree.PageNode{
			ID:    "page-a",
			Title: "Page A",
			Slug:  "page-a",
			Kind:  tree.NodeKindPage,
		}}
		store := &fakeRevisionStore{
			commits: []gitrevisions.Commit{{Hash: "page-a-change", AuthorID: "alice"}},
			filesAt: map[CommitHash]map[string]string{
				"page-a-change": {
					"page-a.md": "---\nleafwiki_id: page-a\nleafwiki_title: Page A\n---\n# Page A changed\n",
				},
			},
			changedPaths: map[CommitHash][]string{
				"page-a-change": {"page-a.md"},
			},
			changedContentsStarted: make(chan struct{}),
			unblockChangedContents: make(chan struct{}),
		}
		service, err := NewService(ServiceOptions{
			Enabled: true,
			Tree:    &fakeTreeReconstructor{},
			Store:   store,
		})
		Expect(err).To(Succeed())

		done := make(chan error, 1)
		go func() {
			_, err := service.GetPageRevisionSnapshot(context.Background(), page, CommitHash("page-a-change"))
			done <- err
		}()
		Eventually(store.changedContentsStarted).Within(time.Second).Should(BeClosed())
		unblockChangedContents := closeOnce(store.unblockChangedContents)
		DeferCleanup(unblockChangedContents)

		statusDone := make(chan SyncStatus, 1)
		go func() {
			statusDone <- service.Status()
		}()

		Eventually(statusDone).WithTimeout(200 * time.Millisecond).Should(Receive(matchEnabledWorkspaceSyncStatus()))
		unblockChangedContents()
		Eventually(done).Should(Receive(Succeed()))
	})
})

var _ = Describe("document restore from workspace revisions", func() {
	It("restores pre-rename content to the current markdown path", func() {
		dataDir := workspaceSyncTempDir()
		rootDir := filepath.Join(workspaceSyncTempDir(), "workspace")
		store, err := gitrevisions.Open(gitrevisions.StoreOptions{DataDir: dataDir, RootDir: rootDir})
		Expect(err).To(Succeed())
		writeMarkdownFile(filepath.Join(rootDir, "old-page.md"), "---\nleafwiki_id: page-1\nleafwiki_title: Old Page\n---\n# Old Page\n\nold content")
		oldCommit, err := store.Capture(context.Background(), gitrevisions.CommitRequest{
			Reason: ReasonExplicit,
			Source: SourceFilesystem,
			Actor:  PublicEditorActor(),
		})
		Expect(err).To(Succeed())
		Expect(os.Remove(filepath.Join(rootDir, "old-page.md"))).To(Succeed())
		writeMarkdownFile(filepath.Join(rootDir, "new-page.md"), "---\nleafwiki_id: page-1\nleafwiki_title: New Page\n---\n# New Page\n\nnew content")
		_, err = store.Capture(context.Background(), gitrevisions.CommitRequest{
			Reason: ReasonExplicit,
			Source: SourceFilesystem,
			Actor:  PublicEditorActor(),
		})
		Expect(err).To(Succeed())
		page := &tree.Page{PageNode: &tree.PageNode{ID: "page-1", Title: "New Page", Slug: "new-page", Kind: tree.NodeKindPage}}
		service, err := NewService(ServiceOptions{
			Enabled: true,
			Tree:    &fakeTreeReconstructor{},
			Store:   store,
		})
		Expect(err).To(Succeed())

		_, err = service.RestoreDocument(context.Background(), page, oldCommit.Hash, PublicEditorActor())
		Expect(err).To(Succeed())

		Expect(readFileStringGinkgo(filepath.Join(rootDir, "new-page.md"))).To(ContainSubstring("old content"))
	})

	It("preserves existing uppercase markdown paths", func() {
		dataDir := workspaceSyncTempDir()
		rootDir := filepath.Join(workspaceSyncTempDir(), "workspace")
		store, err := gitrevisions.Open(gitrevisions.StoreOptions{DataDir: dataDir, RootDir: rootDir})
		Expect(err).To(Succeed())
		writeMarkdownFile(filepath.Join(rootDir, "Page.MD"), "---\nleafwiki_id: page-1\nleafwiki_title: Page\n---\n# Page\n\nold content")
		oldCommit, err := store.Capture(context.Background(), gitrevisions.CommitRequest{
			Reason: ReasonExplicit,
			Source: SourceFilesystem,
			Actor:  PublicEditorActor(),
		})
		Expect(err).To(Succeed())
		writeMarkdownFile(filepath.Join(rootDir, "Page.MD"), "---\nleafwiki_id: page-1\nleafwiki_title: Page\n---\n# Page\n\ncurrent content")
		_, err = store.Capture(context.Background(), gitrevisions.CommitRequest{
			Reason: ReasonExplicit,
			Source: SourceFilesystem,
			Actor:  PublicEditorActor(),
		})
		Expect(err).To(Succeed())
		page := &tree.Page{PageNode: &tree.PageNode{ID: "page-1", Title: "Page", Slug: "Page", Kind: tree.NodeKindPage}}
		service, err := NewService(ServiceOptions{
			Enabled: true,
			DataDir: dataDir,
			RootDir: rootDir,
			Tree:    tree.NewTreeServiceWithOptions(tree.TreeOptions{DataDir: dataDir, RootDir: rootDir}),
			Store:   store,
		})
		Expect(err).To(Succeed())

		_, err = service.RestoreDocument(context.Background(), page, oldCommit.Hash, PublicEditorActor())
		Expect(err).To(Succeed())

		Expect(readFileStringGinkgo(filepath.Join(rootDir, "Page.MD"))).To(ContainSubstring("old content"))
		entries, err := os.ReadDir(rootDir)
		Expect(err).To(Succeed())
		Expect(entries).NotTo(ContainElement(HaveField("Name()", Equal("Page.md"))))
	})

	It("restores section index content to the current section path", func() {
		dataDir := workspaceSyncTempDir()
		rootDir := filepath.Join(workspaceSyncTempDir(), "workspace")
		store, err := gitrevisions.Open(gitrevisions.StoreOptions{DataDir: dataDir, RootDir: rootDir})
		Expect(err).To(Succeed())
		writeMarkdownFile(filepath.Join(rootDir, "docs", "index.md"), "---\nleafwiki_id: section-1\nleafwiki_title: Docs\n---\n# Docs\n\nold section content")
		oldCommit, err := store.Capture(context.Background(), gitrevisions.CommitRequest{
			Reason: ReasonExplicit,
			Source: SourceFilesystem,
			Actor:  PublicEditorActor(),
		})
		Expect(err).To(Succeed())
		writeMarkdownFile(filepath.Join(rootDir, "docs", "index.md"), "---\nleafwiki_id: section-1\nleafwiki_title: Docs\n---\n# Docs\n\nnew section content")
		_, err = store.Capture(context.Background(), gitrevisions.CommitRequest{
			Reason: ReasonExplicit,
			Source: SourceFilesystem,
			Actor:  PublicEditorActor(),
		})
		Expect(err).To(Succeed())
		section := &tree.Page{PageNode: &tree.PageNode{ID: "section-1", Title: "Docs", Slug: "docs", Kind: tree.NodeKindSection}}
		service, err := NewService(ServiceOptions{
			Enabled: true,
			Tree:    &fakeTreeReconstructor{},
			Store:   store,
		})
		Expect(err).To(Succeed())

		_, err = service.RestoreDocument(context.Background(), section, oldCommit.Hash, PublicEditorActor())
		Expect(err).To(Succeed())

		Expect(readFileStringGinkgo(filepath.Join(rootDir, "docs", "index.md"))).To(ContainSubstring("old section content"))
		Expect(filepath.Join(rootDir, "docs.md")).NotTo(BeAnExistingFile())
	})

	It("restores README fallback sections to the README path", func() {
		dataDir := workspaceSyncTempDir()
		rootDir := filepath.Join(workspaceSyncTempDir(), "workspace")
		store, err := gitrevisions.Open(gitrevisions.StoreOptions{DataDir: dataDir, RootDir: rootDir})
		Expect(err).To(Succeed())
		writeMarkdownFile(filepath.Join(rootDir, "docs", "README.md"), "---\nleafwiki_id: section-1\nleafwiki_title: Docs\n---\n# Docs\n\nold readme section content")
		oldCommit, err := store.Capture(context.Background(), gitrevisions.CommitRequest{
			Reason: ReasonExplicit,
			Source: SourceFilesystem,
			Actor:  PublicEditorActor(),
		})
		Expect(err).To(Succeed())
		writeMarkdownFile(filepath.Join(rootDir, "docs", "README.md"), "---\nleafwiki_id: section-1\nleafwiki_title: Docs\n---\n# Docs\n\nnew readme section content")
		_, err = store.Capture(context.Background(), gitrevisions.CommitRequest{
			Reason: ReasonExplicit,
			Source: SourceFilesystem,
			Actor:  PublicEditorActor(),
		})
		Expect(err).To(Succeed())
		section := &tree.Page{PageNode: &tree.PageNode{ID: "section-1", Title: "Docs", Slug: "docs", Kind: tree.NodeKindSection}}
		service, err := NewService(ServiceOptions{
			Enabled: true,
			RootDir: rootDir,
			Tree:    &fakeTreeReconstructor{},
			Store:   store,
		})
		Expect(err).To(Succeed())

		_, err = service.RestoreDocument(context.Background(), section, oldCommit.Hash, PublicEditorActor())
		Expect(err).To(Succeed())

		Expect(readFileStringGinkgo(filepath.Join(rootDir, "docs", "README.md"))).To(ContainSubstring("old readme section content"))
		Expect(filepath.Join(rootDir, "docs", "index.md")).NotTo(BeAnExistingFile())
	})

	It("rejects commits that did not change the requested document", func() {
		page := &tree.Page{PageNode: &tree.PageNode{
			ID:    "page-a",
			Title: "Page A",
			Slug:  "page-a",
			Kind:  tree.NodeKindPage,
		}}
		store := &fakeRevisionStore{
			capture: &gitrevisions.Commit{Hash: "restore-commit"},
			filesAt: map[CommitHash]map[string]string{
				"page-b-change": {
					"page-a.md": "---\nleafwiki_id: page-a\nleafwiki_title: Page A\n---\n# Page A\n",
					"page-b.md": "---\nleafwiki_id: page-b\nleafwiki_title: Page B\n---\n# Page B changed\n",
				},
			},
			changedPaths: map[CommitHash][]string{
				"page-b-change": {"page-b.md"},
			},
		}
		service, err := NewService(ServiceOptions{
			Enabled: true,
			Tree:    &fakeTreeReconstructor{},
			Store:   store,
		})
		Expect(err).To(Succeed())

		_, err = service.RestoreDocument(context.Background(), page, CommitHash("page-b-change"), PublicEditorActor())
		Expect(err).To(Satisfy(func(err error) bool {
			return errors.Is(err, ErrWorkspaceSyncDocumentUnchanged)
		}))
		Expect(store.restoreDocumentToPathCalls).To(BeZero())
	})

	It("uses changed content without loading full trees", func() {
		page := &tree.Page{PageNode: &tree.PageNode{
			ID:    "page-a",
			Title: "Page A",
			Slug:  "page-a",
			Kind:  tree.NodeKindPage,
		}}
		store := &fakeRevisionStore{
			capture: &gitrevisions.Commit{Hash: "restore-commit"},
			filesAt: map[CommitHash]map[string]string{
				"page-a-change": {
					"page-a.md": "---\nleafwiki_id: page-a\nleafwiki_title: Page A\n---\n# Page A restored\n",
				},
			},
			changedPaths: map[CommitHash][]string{
				"page-a-change": {"page-a.md"},
			},
		}
		service, err := NewService(ServiceOptions{
			Enabled: true,
			Tree:    &fakeTreeReconstructor{},
			Store:   store,
		})
		Expect(err).To(Succeed())

		_, err = service.RestoreDocument(context.Background(), page, CommitHash("page-a-change"), PublicEditorActor())
		Expect(err).To(Succeed())

		Expect(fakeRevisionStoreRestoreContentStateFor(store)).To(SatisfyAll(
			HaveField("FilesAtCalls", BeZero()),
			HaveField("RestoreDocumentContentToPathCalls", Equal(1)),
			HaveField("RestoredContent", ContainSubstring("Page A restored")),
		))
	})
})

var _ = Describe("workspace restore revision capture and validation", func() {
	It("returns reconstruction errors with path-specific validation state", func() {
		reconstructErr := errors.New(`duplicate leafwiki_id "page-a" in /workspace/page-a.md and /workspace/other.md`)
		page := &tree.Page{PageNode: &tree.PageNode{
			ID:    "page-a",
			Title: "Page A",
			Slug:  "page-a",
			Kind:  tree.NodeKindPage,
		}}
		store := &fakeRevisionStore{
			capture: &gitrevisions.Commit{Hash: "restore-commit"},
			filesAt: map[CommitHash]map[string]string{
				"page-a-change": {
					"page-a.md": "---\nleafwiki_id: page-a\nleafwiki_title: Page A\n---\n# Page A restored\n",
				},
			},
			changedPaths: map[CommitHash][]string{
				"page-a-change": {"page-a.md"},
			},
		}
		service, err := NewService(ServiceOptions{
			Enabled: true,
			RootDir: "/workspace",
			Tree:    &fakeTreeReconstructor{err: reconstructErr},
			Store:   store,
		})
		Expect(err).To(Succeed())

		status, err := service.RestoreDocument(context.Background(), page, CommitHash("page-a-change"), PublicEditorActor())
		Expect(err).To(MatchError(reconstructErr))
		Expect(status.ValidationErrors).To(ConsistOf(
			HaveField("Path", Equal("page-a.md")),
			HaveField("Path", Equal("other.md")),
		))
	})

	It("captures canonical metadata writeback when restoring a workspace", func() {
		dataDir := workspaceSyncTempDir()
		rootDir := filepath.Join(workspaceSyncTempDir(), "workspace")
		store, err := gitrevisions.Open(gitrevisions.StoreOptions{DataDir: dataDir, RootDir: rootDir})
		Expect(err).To(Succeed())
		writeMarkdownFile(filepath.Join(rootDir, "needs-metadata.md"), "# Needs Metadata\n\nold body")
		rawCommit, err := store.Capture(context.Background(), gitrevisions.CommitRequest{
			Reason: ReasonExplicit,
			Source: SourceFilesystem,
			Actor:  PublicEditorActor(),
		})
		Expect(err).To(Succeed())
		treeService := tree.NewTreeServiceWithOptions(tree.TreeOptions{DataDir: dataDir, RootDir: rootDir})
		service, err := NewService(ServiceOptions{
			Enabled: true,
			DataDir: dataDir,
			RootDir: rootDir,
			Tree:    treeService,
			Store:   store,
		})
		Expect(err).To(Succeed())

		status, err := service.RestoreWorkspace(context.Background(), rawCommit.Hash, PublicEditorActor())
		Expect(err).To(Succeed())

		files, err := store.FilesAt(context.Background(), status.LastCommitHash)
		Expect(err).To(Succeed())
		Expect(files).To(HaveKeyWithValue("needs-metadata.md", SatisfyAll(
			ContainSubstring("<!-- leafwiki\n"),
			ContainSubstring("  id:"),
		)))
	})

	It("captures canonical metadata writeback when restoring a document", func() {
		dataDir := workspaceSyncTempDir()
		rootDir := filepath.Join(workspaceSyncTempDir(), "workspace")
		store, err := gitrevisions.Open(gitrevisions.StoreOptions{DataDir: dataDir, RootDir: rootDir})
		Expect(err).To(Succeed())
		writeMarkdownFile(filepath.Join(rootDir, "needs-metadata.md"), "# Needs Metadata\n\nold body")
		rawCommit, err := store.Capture(context.Background(), gitrevisions.CommitRequest{
			Reason: ReasonExplicit,
			Source: SourceFilesystem,
			Actor:  PublicEditorActor(),
		})
		Expect(err).To(Succeed())
		writeMarkdownFile(filepath.Join(rootDir, "needs-metadata.md"), `---
leafwiki_id: page-1
leafwiki_title: Needs Metadata
leafwiki_created_at: 2026-06-07T10:00:00Z
leafwiki_updated_at: 2026-06-07T10:00:00Z
---

# Needs Metadata

current body`)
		_, err = store.Capture(context.Background(), gitrevisions.CommitRequest{
			Reason: ReasonExplicit,
			Source: SourceFilesystem,
			Actor:  PublicEditorActor(),
		})
		Expect(err).To(Succeed())
		treeService := tree.NewTreeServiceWithOptions(tree.TreeOptions{DataDir: dataDir, RootDir: rootDir})
		page := &tree.Page{PageNode: &tree.PageNode{ID: "page-1", Title: "Needs Metadata", Slug: "needs-metadata", Kind: tree.NodeKindPage}}
		service, err := NewService(ServiceOptions{
			Enabled: true,
			DataDir: dataDir,
			RootDir: rootDir,
			Tree:    treeService,
			Store:   store,
		})
		Expect(err).To(Succeed())

		status, err := service.RestoreDocument(context.Background(), page, rawCommit.Hash, PublicEditorActor())
		Expect(err).To(Succeed())

		files, err := store.FilesAt(context.Background(), status.LastCommitHash)
		Expect(err).To(Succeed())
		Expect(files).To(HaveKeyWithValue("needs-metadata.md", SatisfyAll(
			ContainSubstring("<!-- leafwiki\n"),
			ContainSubstring("  id:"),
		)))
	})

	It("rejects snapshots when a reused path belongs to another leafwiki ID", func() {
		page := &tree.Page{PageNode: &tree.PageNode{
			ID:    "page-a",
			Title: "Page A",
			Slug:  "page",
			Kind:  tree.NodeKindPage,
		}}
		store := &fakeRevisionStore{
			commits: []gitrevisions.Commit{{Hash: "path-reuse"}},
			filesAt: map[CommitHash]map[string]string{
				"path-reuse": {
					"page.md": "---\nleafwiki_id: page-b\nleafwiki_title: Page B\n---\n# Page B\n",
				},
			},
			changedPaths: map[CommitHash][]string{
				"path-reuse": {"page.md"},
			},
		}
		service, err := NewService(ServiceOptions{
			Enabled: true,
			Tree:    &fakeTreeReconstructor{},
			Store:   store,
		})
		Expect(err).To(Succeed())

		_, err = service.GetPageRevisionSnapshot(context.Background(), page, CommitHash("path-reuse"))
		Expect(err).To(Satisfy(func(err error) bool {
			return errors.Is(err, ErrWorkspaceSyncDocumentUnchanged)
		}))
	})
})

var _ = Describe("workspace sync writeback and validation", func() {
	It("captures metadata writeback as a new snapshot after the raw import commit", func() {
		dataDir := workspaceSyncTempDir()
		rootDir := filepath.Join(workspaceSyncTempDir(), "workspace")
		writeMarkdownFile(filepath.Join(rootDir, "needs-metadata.md"), "# Needs Metadata\n\nbody")
		store, err := gitrevisions.Open(gitrevisions.StoreOptions{DataDir: dataDir, RootDir: rootDir})
		Expect(err).To(Succeed())
		firstCommit, err := store.Capture(context.Background(), gitrevisions.CommitRequest{
			Reason: ReasonExplicit,
			Source: SourceFilesystem,
			Actor:  PublicEditorActor(),
		})
		Expect(err).To(Succeed())
		treeService := tree.NewTreeServiceWithOptions(tree.TreeOptions{DataDir: dataDir, RootDir: rootDir})
		service, err := NewService(ServiceOptions{
			Enabled: true,
			DataDir: dataDir,
			RootDir: rootDir,
			Tree:    treeService,
			Store:   store,
		})
		Expect(err).To(Succeed())

		status, err := service.SyncNow(context.Background(), SyncRequest{
			Reason: ReasonExplicit,
			Source: SourceFilesystem,
			Actor:  PublicEditorActor(),
		})
		Expect(err).To(Succeed())
		snapshots, err := service.ListSnapshots(context.Background(), 10)
		Expect(err).To(Succeed())

		Expect(status.LastCommitHash).NotTo(Equal(firstCommit.Hash))
		Expect(snapshots).To(HaveLen(2))
	})

	It("reports invalid slug markdown as a validation issue for the skipped file", func() {
		dataDir := workspaceSyncTempDir()
		rootDir := filepath.Join(workspaceSyncTempDir(), "workspace")
		treeService := tree.NewTreeServiceWithOptions(tree.TreeOptions{DataDir: dataDir, RootDir: rootDir})
		writeMarkdownFile(filepath.Join(rootDir, "!!!.md"), "---\nleafwiki_id: invalid-slug\nleafwiki_title: Invalid Slug\n---\n# Invalid Slug\n")
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

		Expect(status.ValidationErrors).To(SatisfyAll(
			testmatchers.HaveValidationIssue(wikivalidation.IssueCodeInvalidSlug),
			ContainElement(HaveField("Path", Equal("!!!.md"))),
		))
	})

	It("reports normalized route collisions as path conflict validation issues", func() {
		dataDir := workspaceSyncTempDir()
		rootDir := filepath.Join(workspaceSyncTempDir(), "workspace")
		treeService := tree.NewTreeServiceWithOptions(tree.TreeOptions{DataDir: dataDir, RootDir: rootDir})
		writeMarkdownFile(filepath.Join(rootDir, "plans", "a_b.md"), "---\nleafwiki_id: a-b-one\nleafwiki_title: A B One\n---\n# A B One\n")
		writeMarkdownFile(filepath.Join(rootDir, "plans", "a-b.md"), "---\nleafwiki_id: a-b-two\nleafwiki_title: A B Two\n---\n# A B Two\n")
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

		Expect(status.ValidationErrors).To(SatisfyAll(
			testmatchers.HaveValidationIssue(wikivalidation.IssueCodePathConflict),
			ContainElement(HaveField("Path", Equal("plans/a_b.md"))),
		))
	})
})

var _ = Describe("workspace sync file watcher", func() {
	It("syncs markdown events and reports the processed path", func() {
		fakeTree := &fakeTreeReconstructor{}
		fakeStore := &fakeRevisionStore{
			capture: &gitrevisions.Commit{
				Hash:                 "event-commit",
				ChangedMarkdownPaths: []string{"docs/a.md"},
			},
		}
		fakeWatcher := newFakeWatcher()
		service, err := NewService(ServiceOptions{
			Enabled: true,
			RootDir: "/workspace",
			Tree:    fakeTree,
			Store:   fakeStore,
			WatcherFactory: func(string) (fileWatcher, error) {
				return fakeWatcher, nil
			},
		})
		Expect(err).To(Succeed())
		ctx, cancel := context.WithCancel(context.Background())
		DeferCleanup(cancel)

		Expect(service.StartWatcher(ctx)).To(Succeed())
		fakeWatcher.events <- watcherEvent{Path: "/workspace/docs/a.md"}

		Eventually(fakeTree.reconstructCount).Should(Equal(1))
		Expect(service.Status()).To(matchRunningWatcherWithRecentMarkdownPaths("docs/a.md"))
		Expect(fakeStore.captureCalls).To(Equal(1))
	})

	It("syncs markdown events with uppercase extensions", func() {
		fakeTree := &fakeTreeReconstructor{}
		fakeStore := &fakeRevisionStore{
			capture: &gitrevisions.Commit{
				Hash:                 "event-commit",
				ChangedMarkdownPaths: []string{"Page.MD"},
			},
		}
		fakeWatcher := newFakeWatcher()
		service, err := NewService(ServiceOptions{
			Enabled: true,
			RootDir: "/workspace",
			Tree:    fakeTree,
			Store:   fakeStore,
			WatcherFactory: func(string) (fileWatcher, error) {
				return fakeWatcher, nil
			},
		})
		Expect(err).To(Succeed())
		ctx, cancel := context.WithCancel(context.Background())
		DeferCleanup(cancel)

		Expect(service.StartWatcher(ctx)).To(Succeed())
		fakeWatcher.events <- watcherEvent{Path: "/workspace/Page.MD"}

		Eventually(fakeTree.reconstructCount).Should(Equal(1))
		Expect(fakeStore.captureCalls).To(Equal(1))
	})

	It("coalesces duplicate markdown events into one sync", func() {
		fakeTree := &fakeTreeReconstructor{}
		fakeStore := &fakeRevisionStore{
			capture: &gitrevisions.Commit{
				Hash:                 "event-commit",
				ChangedMarkdownPaths: []string{"docs/a.md"},
			},
		}
		fakeWatcher := newFakeWatcher()
		service, err := NewService(ServiceOptions{
			Enabled: true,
			RootDir: "/workspace",
			Tree:    fakeTree,
			Store:   fakeStore,
			WatcherFactory: func(string) (fileWatcher, error) {
				return fakeWatcher, nil
			},
		})
		Expect(err).To(Succeed())
		ctx, cancel := context.WithCancel(context.Background())
		DeferCleanup(cancel)

		Expect(service.StartWatcher(ctx)).To(Succeed())
		fakeWatcher.events <- watcherEvent{Path: "/workspace/docs/a.md"}
		fakeWatcher.events <- watcherEvent{Path: "/workspace/docs/a.md"}

		Eventually(fakeTree.reconstructCount).Should(Equal(1))
		Consistently(func() int { return fakeStore.captureCalls }).WithTimeout(350 * time.Millisecond).Should(Equal(1))
	})

	It("records dropped event status and still syncs the workspace", func() {
		fakeTree := &fakeTreeReconstructor{}
		fakeStore := &fakeRevisionStore{capture: &gitrevisions.Commit{Hash: "drop-commit"}}
		fakeWatcher := newFakeWatcher()
		service, err := NewService(ServiceOptions{
			Enabled: true,
			RootDir: "/workspace",
			Tree:    fakeTree,
			Store:   fakeStore,
			WatcherFactory: func(string) (fileWatcher, error) {
				return fakeWatcher, nil
			},
		})
		Expect(err).To(Succeed())
		ctx, cancel := context.WithCancel(context.Background())
		DeferCleanup(cancel)

		Expect(service.StartWatcher(ctx)).To(Succeed())
		fakeWatcher.dropped <- watcherEvent{Path: "/workspace/docs/a.md", Dropped: true}

		Eventually(fakeTree.reconstructCount).Should(Equal(1))
		Expect(service.Status()).To(HaveField("LastError", Equal(watcherDroppedEventsStatus("docs/a.md"))))
		Expect(fakeStore.captureCalls).To(Equal(1))
	})

	It("records watcher errors and still syncs the workspace", func() {
		fakeTree := &fakeTreeReconstructor{}
		fakeStore := &fakeRevisionStore{capture: &gitrevisions.Commit{Hash: "error-commit"}}
		fakeWatcher := newFakeWatcher()
		fakeWatcher.watchErr = errors.New("watcher platform error")
		service, err := NewService(ServiceOptions{
			Enabled: true,
			RootDir: "/workspace",
			Tree:    fakeTree,
			Store:   fakeStore,
			WatcherFactory: func(string) (fileWatcher, error) {
				return fakeWatcher, nil
			},
		})
		Expect(err).To(Succeed())
		ctx, cancel := context.WithCancel(context.Background())
		DeferCleanup(cancel)

		Expect(service.StartWatcher(ctx)).To(Succeed())

		Eventually(fakeTree.reconstructCount).Should(Equal(1))
		Expect(service.Status()).To(matchStoppedWatcherStatus())
		Expect(fakeStore.captureCalls).To(Equal(1))
	})

	It("ignores temporary file events", func() {
		fakeStore := &fakeRevisionStore{capture: &gitrevisions.Commit{Hash: "temp-commit"}}
		fakeWatcher := newFakeWatcher()
		service, err := NewService(ServiceOptions{
			Enabled: true,
			RootDir: "/workspace",
			Tree:    &fakeTreeReconstructor{},
			Store:   fakeStore,
			WatcherFactory: func(string) (fileWatcher, error) {
				return fakeWatcher, nil
			},
		})
		Expect(err).To(Succeed())
		ctx, cancel := context.WithCancel(context.Background())
		DeferCleanup(cancel)

		Expect(service.StartWatcher(ctx)).To(Succeed())
		fakeWatcher.events <- watcherEvent{Path: "/workspace/page.md.swp"}
		fakeWatcher.events <- watcherEvent{Path: "/workspace/.DS_Store"}

		Consistently(func() int { return fakeStore.captureCalls }).WithTimeout(50 * time.Millisecond).Should(BeZero())
	})

	It("closes the underlying watcher when stopped", func() {
		fakeWatcher := newFakeWatcher()
		service, err := NewService(ServiceOptions{
			Enabled: true,
			RootDir: "/workspace",
			Tree:    &fakeTreeReconstructor{},
			Store:   &fakeRevisionStore{capture: &gitrevisions.Commit{Hash: "stop-commit"}},
			WatcherFactory: func(string) (fileWatcher, error) {
				return fakeWatcher, nil
			},
		})
		Expect(err).To(Succeed())
		Expect(service.StartWatcher(context.Background())).To(Succeed())

		service.StopWatcher()

		Expect(fakeWatcher.closeCount()).To(Equal(1))
	})
})

var _ = Describe("workspace sync path normalization", func() {
	It("cleans and joins markdown paths without losing section filenames", func() {
		Expect(cleanWorkspaceMarkdownPath(" /docs/section/index.md ")).To(Equal("docs/section/index.md"))
		Expect(cleanWorkspaceMarkdownPath("./docs/page.md")).To(Equal("./docs/page.md"))
		Expect(joinWorkspaceMarkdownPath("", "/docs/", " section ", "index.md")).To(Equal("docs/section/index.md"))
		Expect(joinWorkspaceMarkdownPath("docs", "", "/page.md/")).To(Equal("docs/page.md"))
	})

	It("selects section index content before README fallback", func() {
		rootDir := workspaceSyncTempDir()
		service := &Service{rootDir: rootDir}
		Expect(os.MkdirAll(filepath.Join(rootDir, "docs"), 0o755)).To(Succeed())
		writeMarkdownFile(filepath.Join(rootDir, "docs", "README.md"), "# Readme\n")

		Expect(service.currentSectionContentPath("docs", "docs.md")).To(Equal("docs/README.md"))

		writeMarkdownFile(filepath.Join(rootDir, "docs", "Index.MD"), "# Index\n")
		Expect(service.currentSectionContentPath("docs", "docs.md")).To(Equal("docs/Index.MD"))
		Expect(service.currentSectionContentPath("missing", "fallback.md")).To(Equal("fallback.md"))
	})

	It("keeps validation paths workspace-relative unless they escape the root", func() {
		rootDir := filepath.Join(workspaceSyncTempDir(), "workspace")
		Expect(normalizeValidationPath(rootDir, filepath.Join(rootDir, "docs", "page.md"))).To(Equal("docs/page.md"))
		Expect(normalizeValidationPath(rootDir, "./docs/page.md")).To(Equal("docs/page.md"))
		Expect(normalizeValidationPath(rootDir, "../outside.md")).To(BeEmpty())
		Expect(normalizeValidationPath(rootDir, filepath.Join(filepath.Dir(rootDir), "outside.md"))).To(Equal(filepath.ToSlash(filepath.Join(filepath.Dir(rootDir), "outside.md"))))
	})

	It("returns the first trimmed non-blank value", func() {
		Expect(firstNonEmpty("", " \t ", " value ", "later")).To(Equal("value"))
		Expect(firstNonEmpty("", " ")).To(BeEmpty())
	})
})

func workspaceSyncTempDir() string {
	GinkgoHelper()

	dir, err := os.MkdirTemp("", "leafwiki-workspacesync-*")
	Expect(err).To(Succeed())
	DeferCleanup(os.RemoveAll, dir)
	return dir
}

func writeMarkdownFile(path string, content string) {
	GinkgoHelper()
	Expect(os.MkdirAll(filepath.Dir(path), 0o755)).To(Succeed())
	Expect(os.WriteFile(path, []byte(content), 0o644)).To(Succeed())
}

func readFileStringGinkgo(path string) string {
	GinkgoHelper()

	raw, err := os.ReadFile(path)
	Expect(err).To(Succeed())
	return string(raw)
}

func mustGetOnlyPageGinkgo(treeService *tree.TreeService) *tree.Page {
	GinkgoHelper()

	var ids []tree.PageID
	err := treeService.WalkNodes(func(id tree.PageID) error {
		ids = append(ids, id)
		return nil
	})
	Expect(err).To(Succeed())
	Expect(ids).To(HaveLen(1))

	page, err := treeService.GetPage(ids[0])
	Expect(err).To(Succeed())
	return page
}

var (
	errCanonicalMigrationCompletedWithoutFailure = errors.New("canonical migration completed without write failure")
	errCanonicalMigrationCommittedBeforeRollback = errors.New("canonical migration committed before rollback")
	errAtomicRewriteFailureNotLinkError          = errors.New("atomic rewrite failure was not a link error")
)

func canonicalMigrationFailsWithoutCommit(service *Service) error {
	GinkgoHelper()

	changed, err := service.migrateCanonicalMarkdownLinksLocked()
	if err == nil {
		return errCanonicalMigrationCompletedWithoutFailure
	}
	if changed {
		return errCanonicalMigrationCommittedBeforeRollback
	}
	return nil
}

func atomicRewriteLinkError(err error) (*os.LinkError, error) {
	GinkgoHelper()

	var linkErr *os.LinkError
	if errors.As(err, &linkErr) {
		return linkErr, nil
	}
	return nil, errAtomicRewriteFailureNotLinkError
}

func revisionIDs(revisions []*revision.Revision) []string {
	ids := make([]string, 0, len(revisions))
	for _, rev := range revisions {
		if rev == nil {
			ids = append(ids, "")
			continue
		}
		ids = append(ids, rev.ID.CommitID())
	}
	return ids
}

type fakeTreeReconstructor struct {
	reconstructs int32
	err          error
	errs         []error
}

func (f *fakeTreeReconstructor) ReconstructTreeFromFS() error {
	call := int(atomic.AddInt32(&f.reconstructs, 1))
	if call <= len(f.errs) {
		return f.errs[call-1]
	}
	return f.err
}

func (f *fakeTreeReconstructor) reconstructCount() int {
	return int(atomic.LoadInt32(&f.reconstructs))
}

type fakeRevisionStore struct {
	capture                           *gitrevisions.Commit
	captureErr                        error
	captureErrCall                    int
	amendErr                          error
	listErr                           error
	getCommitErr                      error
	restoreWorkspaceErr               error
	restoreDocumentContentErr         error
	captureCalls                      int
	amendCalls                        int
	commits                           []gitrevisions.Commit
	filesAt                           map[CommitHash]map[string]string
	changedPaths                      map[CommitHash][]string
	changedPathsErr                   error
	changedPathsErrByHash             map[CommitHash]error
	scannedCommits                    int
	filesAtCalls                      int
	restoreDocumentToPathCalls        int
	restoreDocumentContentToPathCalls int
	restoredContent                   string
	changedPathsStarted               chan struct{}
	unblockChangedPaths               chan struct{}
	changedContents                   map[CommitHash]map[string]string
	changedContentsErr                error
	changedContentsStarted            chan struct{}
	unblockChangedContents            chan struct{}
	captureRequests                   []gitrevisions.CommitRequest
	amendRequests                     []gitrevisions.CommitRequest
	restoreWorkspaceRequests          []gitrevisions.CommitRequest
	restoreDocumentContentRequests    []gitrevisions.CommitRequest
}

type fakeRevisionStoreRestoreContentState struct {
	FilesAtCalls                      int
	RestoreDocumentContentToPathCalls int
	RestoredContent                   string
}

func fakeRevisionStoreRestoreContentStateFor(store *fakeRevisionStore) fakeRevisionStoreRestoreContentState {
	GinkgoHelper()

	return fakeRevisionStoreRestoreContentState{
		FilesAtCalls:                      store.filesAtCalls,
		RestoreDocumentContentToPathCalls: store.restoreDocumentContentToPathCalls,
		RestoredContent:                   store.restoredContent,
	}
}

func (f *fakeRevisionStore) Capture(_ context.Context, req gitrevisions.CommitRequest) (*gitrevisions.Commit, error) {
	f.captureCalls++
	f.captureRequests = append(f.captureRequests, req)
	if f.captureErr != nil && (f.captureErrCall == 0 || f.captureCalls == f.captureErrCall) {
		return nil, f.captureErr
	}
	if f.capture == nil {
		return nil, nil
	}
	commit := *f.capture
	if commit.Hash != "" {
		commit.Created = true
	}
	return &commit, nil
}

func (f *fakeRevisionStore) Amend(_ context.Context, req gitrevisions.CommitRequest) (*gitrevisions.Commit, error) {
	f.amendCalls++
	f.amendRequests = append(f.amendRequests, req)
	if f.amendErr != nil {
		return nil, f.amendErr
	}
	return f.capture, nil
}

func (f *fakeRevisionStore) ListCommits(_ context.Context, req gitrevisions.ListRequest) ([]gitrevisions.Commit, error) {
	if f.listErr != nil {
		return nil, f.listErr
	}
	if req.Limit <= 0 || req.Limit >= len(f.commits) {
		return f.commits, nil
	}
	return f.commits[:req.Limit], nil
}

func (f *fakeRevisionStore) ForEachCommit(_ context.Context, visit func(gitrevisions.Commit) (bool, error)) error {
	for _, commit := range f.commits {
		f.scannedCommits++
		keepGoing, err := visit(commit)
		if err != nil {
			return err
		}
		if !keepGoing {
			return nil
		}
	}
	return nil
}

func (f *fakeRevisionStore) ChangedMarkdownPaths(_ context.Context, hash CommitHash) ([]string, error) {
	if f.changedPathsStarted != nil {
		close(f.changedPathsStarted)
		f.changedPathsStarted = nil
	}
	if f.unblockChangedPaths != nil {
		<-f.unblockChangedPaths
	}
	if f.changedPathsErr != nil {
		return nil, f.changedPathsErr
	}
	if f.changedPathsErrByHash != nil {
		if err := f.changedPathsErrByHash[hash]; err != nil {
			return nil, err
		}
	}
	return f.changedPaths[hash], nil
}

func (f *fakeRevisionStore) ChangedMarkdownContents(_ context.Context, hash CommitHash) (map[string]string, error) {
	if f.changedContentsStarted != nil {
		close(f.changedContentsStarted)
		f.changedContentsStarted = nil
	}
	if f.unblockChangedContents != nil {
		<-f.unblockChangedContents
	}
	if f.changedContentsErr != nil {
		return nil, f.changedContentsErr
	}
	if f.changedContents != nil {
		return f.changedContents[hash], nil
	}
	contents := make(map[string]string)
	files := f.filesAt[hash]
	for _, path := range f.changedPaths[hash] {
		if content, ok := files[path]; ok {
			contents[path] = content
		}
	}
	return contents, nil
}

func (f *fakeRevisionStore) GetCommit(_ context.Context, hash CommitHash) (gitrevisions.Commit, error) {
	if f.getCommitErr != nil {
		return gitrevisions.Commit{}, f.getCommitErr
	}
	for _, commit := range f.commits {
		if CommitHashFromString(commit.Hash) == hash {
			return commit, nil
		}
	}
	if f.capture != nil && CommitHashFromString(f.capture.Hash) == hash {
		return *f.capture, nil
	}
	return gitrevisions.Commit{}, errors.New("commit not found")
}

func (f *fakeRevisionStore) RestoreWorkspace(_ context.Context, _ CommitHash, req gitrevisions.CommitRequest) (*gitrevisions.Commit, error) {
	f.restoreWorkspaceRequests = append(f.restoreWorkspaceRequests, req)
	if f.restoreWorkspaceErr != nil {
		return nil, f.restoreWorkspaceErr
	}
	return f.capture, nil
}

func (f *fakeRevisionStore) RestoreDocument(context.Context, string, CommitHash, gitrevisions.CommitRequest) (*gitrevisions.Commit, error) {
	return f.capture, nil
}

func (f *fakeRevisionStore) RestoreDocumentToPath(context.Context, string, string, CommitHash, gitrevisions.CommitRequest) (*gitrevisions.Commit, error) {
	f.restoreDocumentToPathCalls++
	return f.capture, nil
}

func (f *fakeRevisionStore) RestoreDocumentContentToPath(_ context.Context, _ string, content string, req gitrevisions.CommitRequest) (*gitrevisions.Commit, error) {
	f.restoreDocumentContentToPathCalls++
	f.restoreDocumentContentRequests = append(f.restoreDocumentContentRequests, req)
	f.restoredContent = content
	if f.restoreDocumentContentErr != nil {
		return nil, f.restoreDocumentContentErr
	}
	return f.capture, nil
}

func (f *fakeRevisionStore) FilesAt(_ context.Context, hash CommitHash) (map[string]string, error) {
	f.filesAtCalls++
	if f.filesAt == nil {
		return nil, nil
	}
	return f.filesAt[hash], nil
}

type fakeWatcher struct {
	events   chan watcherEvent
	dropped  chan watcherEvent
	watchErr error
	closes   int32
}

func newFakeWatcher() *fakeWatcher {
	return &fakeWatcher{
		events:  make(chan watcherEvent, 10),
		dropped: make(chan watcherEvent, 10),
	}
}

func (f *fakeWatcher) Watch(ctx context.Context) error {
	if f.watchErr != nil {
		return f.watchErr
	}
	<-ctx.Done()
	return ctx.Err()
}

func (f *fakeWatcher) Events() <-chan watcherEvent {
	return f.events
}

func (f *fakeWatcher) Dropped() <-chan watcherEvent {
	return f.dropped
}

func (f *fakeWatcher) Close() {
	atomic.AddInt32(&f.closes, 1)
}

func (f *fakeWatcher) closeCount() int {
	return int(atomic.LoadInt32(&f.closes))
}

func closeOnce(ch chan struct{}) func() {
	var once sync.Once
	return func() {
		once.Do(func() {
			close(ch)
		})
	}
}
