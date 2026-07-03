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

var _ = It("ServiceCanonicalMigrationRollsBackWhenLaterWriteFails", func() {
	t := GinkgoT()
	rootDir := filepath.Join(t.TempDir(), "workspace")
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
	writeMarkdown(t, firstPath, firstOriginal)
	writeMarkdown(t, secondPath, secondOriginal)
	writeMarkdown(t, filepath.Join(rootDir, "targets", "first.md"), `---
leafwiki_id: first-target
leafwiki_title: First Target
---
# First Target
`)
	writeMarkdown(t, filepath.Join(rootDir, "targets", "second.md"), `---
leafwiki_id: second-target
leafwiki_title: Second Target
---
# Second Target
`)
	if err := os.Chmod(secondPath, 0o444); err != nil {
		t.Fatalf("chmod readonly source file: %v", err)
	}
	if err := os.Chmod(secondDir, 0o555); err != nil {
		t.Fatalf("chmod readonly source dir: %v", err)
	}
	DeferCleanup(func() {
		_ = os.Chmod(secondDir, 0o755)
		_ = os.Chmod(secondPath, 0o644)
	})
	if err := os.WriteFile(filepath.Join(secondDir, ".probe"), []byte("probe"), 0o644); err == nil {
		_ = os.Remove(filepath.Join(secondDir, ".probe"))
		t.Skip("filesystem permits writes to read-only test directory")
	}

	changed, err := service.migrateCanonicalMarkdownLinksLocked()

	if err == nil {
		t.Fatalf("migrateCanonicalMarkdownLinksLocked error = nil, want write failure")
	}
	if changed {
		t.Fatalf("changed = true, want no committed migration on write failure")
	}
	if got := readFileString(t, firstPath); got != firstOriginal {
		t.Fatalf("first source = %q, want original content after rollback", got)
	}
	if got := readFileString(t, secondPath); got != secondOriginal {
		t.Fatalf("second source = %q, want original content after failed write", got)
	}
})

var _ = It("WriteCanonicalMarkdownRewritesAtomicallyRollsBackCommittedRename", func() {
	t := GinkgoT()
	rootDir := t.TempDir()
	firstPath := filepath.Join(rootDir, "first.md")
	blockingDir := filepath.Join(rootDir, "blocking")
	firstOriginal := "# First\n\n[Target](/target)\n"
	firstCanonical := "# First\n\n[Target](/target.md)\n"
	writeMarkdown(t, firstPath, firstOriginal)
	if err := os.MkdirAll(blockingDir, 0o755); err != nil {
		t.Fatalf("mkdir blocking dir: %v", err)
	}

	err := writeCanonicalMarkdownRewritesAtomically([]canonicalMarkdownRewrite{
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

	if err == nil {
		t.Fatalf("writeCanonicalMarkdownRewritesAtomically error = nil, want rename failure")
	}
	if got := readFileString(t, firstPath); got != firstOriginal {
		t.Fatalf("first source = %q, want original content after committed rename rollback", got)
	}
})

var _ = It("ServiceSyncNowRollsBackCanonicalMigrationWhenWritebackCaptureFails", func() {
	t := GinkgoT()
	dataDir := t.TempDir()
	rootDir := filepath.Join(t.TempDir(), "workspace")
	treeService := tree.NewTreeServiceWithOptions(tree.TreeOptions{DataDir: dataDir, RootDir: rootDir})
	if err := treeService.LoadTree(); err != nil {
		t.Fatalf("LoadTree: %v", err)
	}
	sourcePath := filepath.Join(rootDir, "docs", "a.md")
	sourceOriginal := `---
leafwiki_id: page-a
leafwiki_title: Page A
---
# Page A

[B](/docs/b)
`
	writeMarkdown(t, sourcePath, sourceOriginal)
	writeMarkdown(t, filepath.Join(rootDir, "docs", "b.md"), `---
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
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}

	status, err := service.SyncNow(context.Background(), SyncRequest{
		Reason: ReasonExplicit,
		Source: SourceFilesystem,
		Actor:  PublicEditorActor(),
	})

	if !errors.Is(err, captureErr) {
		t.Fatalf("SyncNow error = %v, want %v", err, captureErr)
	}
	if !strings.Contains(status.LastError, "writeback capture failed") {
		t.Fatalf("LastError = %q, want writeback capture failure", status.LastError)
	}
	if got := readFileString(t, sourcePath); !strings.Contains(got, "[B](/docs/b)") || strings.Contains(got, "[B](/docs/b.md)") {
		t.Fatalf("source file = %q, want canonical migration rolled back after writeback capture failure", got)
	}
	page, err := treeService.GetPage("page-a")
	if err != nil {
		t.Fatalf("GetPage page-a: %v", err)
	}
	if !strings.Contains(page.RawContent, "[B](/docs/b)") || strings.Contains(page.RawContent, "[B](/docs/b.md)") {
		t.Fatalf("page raw content = %q, want reconstructed non-canonical content after rollback", page.RawContent)
	}
})

// - Migration write failure reports sync validation state without losing raw content
var _ = It("ServiceSyncNowStopsBeforeDerivedRebuildsWhenCanonicalMigrationWriteFails", func() {
	t := GinkgoT()
	dataDir := t.TempDir()
	rootDir := filepath.Join(t.TempDir(), "workspace")
	treeService := tree.NewTreeServiceWithOptions(tree.TreeOptions{DataDir: dataDir, RootDir: rootDir})
	if err := treeService.LoadTree(); err != nil {
		t.Fatalf("LoadTree: %v", err)
	}
	sourcePath := filepath.Join(rootDir, "docs", "a.md")
	sourceOriginal := `---
leafwiki_id: page-a
leafwiki_title: Page A
---
# Page A

[B](/docs/b)
`
	writeMarkdown(t, sourcePath, sourceOriginal)
	writeMarkdown(t, filepath.Join(rootDir, "docs", "b.md"), `---
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
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}

	status, err := service.SyncNow(context.Background(), SyncRequest{
		Reason: ReasonExplicit,
		Source: SourceFilesystem,
		Actor:  PublicEditorActor(),
	})

	if !errors.Is(err, writeErr) {
		t.Fatalf("SyncNow error = %v, want %v", err, writeErr)
	}
	if !strings.Contains(status.LastError, writeErr.Error()) {
		t.Fatalf("LastError = %q, want canonical write failure", status.LastError)
	}
	if got := readFileString(t, sourcePath); !strings.Contains(got, "[B](/docs/b)") || strings.Contains(got, "[B](/docs/b.md)") {
		t.Fatalf("source file = %q, want no canonical migration rewrite after write failure", got)
	}
	page, err := treeService.GetPage("page-a")
	if err != nil {
		t.Fatalf("GetPage page-a: %v", err)
	}
	if !strings.Contains(page.RawContent, "[B](/docs/b)") || strings.Contains(page.RawContent, "[B](/docs/b.md)") {
		t.Fatalf("tree raw content = %q, want no canonical migration rewrite after write failure", page.RawContent)
	}
	if derivedRebuilds != 0 {
		t.Fatalf("derived rebuilds = %d, want none after migration write failure", derivedRebuilds)
	}
})

var _ = It("ServiceSyncNowReportsMetadataWritebackFailureDuringReconstruction", func() {
	t := GinkgoT()
	dataDir := t.TempDir()
	rootDir := filepath.Join(t.TempDir(), "workspace")
	treeService := tree.NewTreeServiceWithOptions(tree.TreeOptions{DataDir: dataDir, RootDir: rootDir})
	if err := treeService.LoadTree(); err != nil {
		t.Fatalf("LoadTree: %v", err)
	}
	sourcePath := filepath.Join(rootDir, "legacy-metadata.md")
	writeMarkdown(t, sourcePath, `---
leafwiki_id: legacy-metadata
leafwiki_title: Legacy Metadata
leafwiki_created_at: "2026-06-13T10:00:00Z"
leafwiki_updated_at: "2026-06-13T11:00:00Z"
---
# Legacy Metadata

body
`)
	if err := os.Chmod(rootDir, 0o500); err != nil {
		t.Fatalf("chmod read-only root: %v", err)
	}
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
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}

	status, err := service.SyncNow(context.Background(), SyncRequest{
		Reason: ReasonExplicit,
		Source: SourceFilesystem,
		Actor:  PublicEditorActor(),
	})

	if err != nil {
		t.Fatalf("SyncNow error = %v, want status failure without returned error", err)
	}
	if !strings.Contains(status.LastError, "legacy-metadata.md") {
		t.Fatalf("LastError = %q, want metadata writeback failure path", status.LastError)
	}
	if derivedRebuilds != 0 {
		t.Fatalf("derived rebuilds = %d, want none after metadata writeback failure", derivedRebuilds)
	}
	if store.captureCalls != 1 {
		t.Fatalf("capture calls = %d, want only initial raw capture and no writeback capture", store.captureCalls)
	}
	if _, err := treeService.GetPage("legacy-metadata"); err == nil {
		t.Fatalf("legacy-metadata page was indexed despite failed metadata writeback")
	}
	if raw := readFileString(t, sourcePath); strings.HasPrefix(raw, "<!-- leafwiki\n") {
		t.Fatalf("source file was canonicalized despite writeback failure: %q", raw)
	}
})

// - Migration is idempotent
var _ = It("ServiceSyncNowCanonicalMigrationSecondRunCreatesNoNewRevision", func() {
	t := GinkgoT()
	dataDir := t.TempDir()
	rootDir := filepath.Join(t.TempDir(), "workspace")
	treeService := tree.NewTreeServiceWithOptions(tree.TreeOptions{DataDir: dataDir, RootDir: rootDir})
	if err := treeService.LoadTree(); err != nil {
		t.Fatalf("LoadTree: %v", err)
	}
	writeMarkdown(t, filepath.Join(rootDir, "a.md"), `---
leafwiki_id: page-a
leafwiki_title: Page A
---
# Page A

[B](/b)
`)
	writeMarkdown(t, filepath.Join(rootDir, "b.md"), `---
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
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	for i := 0; i < 2; i++ {
		if _, err := service.SyncNow(context.Background(), SyncRequest{
			Reason: ReasonExplicit,
			Source: SourceFilesystem,
			Actor:  PublicEditorActor(),
		}); err != nil {
			t.Fatalf("SyncNow %d: %v", i+1, err)
		}
	}

	snapshots, err := service.ListSnapshots(context.Background(), 10)
	if err != nil {
		t.Fatalf("ListSnapshots: %v", err)
	}
	if len(snapshots) != 2 {
		t.Fatalf("snapshot count = %d, want raw commit plus canonical migration writeback and no repeat revisions: %#v", len(snapshots), snapshots)
	}
})

// - Unresolved old extensionless page link becomes validation error
var _ = It("ServiceSyncNowLeavesUnresolvedLegacyPageLinkAndReportsValidationError", func() {
	t := GinkgoT()
	dataDir := t.TempDir()
	rootDir := filepath.Join(t.TempDir(), "workspace")
	treeService := tree.NewTreeServiceWithOptions(tree.TreeOptions{DataDir: dataDir, RootDir: rootDir})
	if err := treeService.LoadTree(); err != nil {
		t.Fatalf("LoadTree: %v", err)
	}
	writeMarkdown(t, filepath.Join(rootDir, "docs", "a.md"), `---
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
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}

	status, err := service.SyncNow(context.Background(), SyncRequest{
		Reason: ReasonExplicit,
		Source: SourceFilesystem,
		Actor:  PublicEditorActor(),
	})
	if err != nil {
		t.Fatalf("SyncNow: %v", err)
	}
	raw, err := os.ReadFile(filepath.Join(rootDir, "docs", "a.md"))
	if err != nil {
		t.Fatalf("ReadFile a.md: %v", err)
	}
	if !strings.Contains(string(raw), "[Missing](/docs/missing)") {
		t.Fatalf("a.md = %q, want unresolved legacy link left unchanged", string(raw))
	}
	if len(status.ValidationErrors) != 1 {
		t.Fatalf("ValidationErrors = %#v, want one unresolved link error", status.ValidationErrors)
	}
	if status.ValidationErrors[0].Path != "docs/a" || !strings.Contains(status.ValidationErrors[0].Message, "/docs/missing") {
		t.Fatalf("ValidationErrors = %#v, want source path and missing link detail", status.ValidationErrors)
	}
})

// - Relative link cannot escape the workspace root
var _ = It("ServiceSyncNowLeavesInvalidCanonicalLinksUnchangedAndReportsValidation", func() {
	t := GinkgoT()
	dataDir := t.TempDir()
	workspaceParent := t.TempDir()
	rootDir := filepath.Join(workspaceParent, "workspace")
	treeService := tree.NewTreeServiceWithOptions(tree.TreeOptions{DataDir: dataDir, RootDir: rootDir})
	if err := treeService.LoadTree(); err != nil {
		t.Fatalf("LoadTree: %v", err)
	}
	if err := os.WriteFile(filepath.Join(workspaceParent, "outside.md"), []byte("---\nleafwiki_id: outside\nleafwiki_title: Outside\n---\n# Outside\n"), 0o644); err != nil {
		t.Fatalf("write outside markdown: %v", err)
	}
	sourceOriginal := `---
leafwiki_id: page-a
leafwiki_title: Page A
---
# Page A

[Bad Encoding](/docs/%zz)
[Escape](../../outside.md)
`
	writeMarkdown(t, filepath.Join(rootDir, "docs", "a.md"), sourceOriginal)

	service, err := NewService(ServiceOptions{
		Enabled: true,
		DataDir: dataDir,
		RootDir: rootDir,
		Tree:    treeService,
	})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}

	status, err := service.SyncNow(context.Background(), SyncRequest{
		Reason: ReasonExplicit,
		Source: SourceFilesystem,
		Actor:  PublicEditorActor(),
	})
	if err != nil {
		t.Fatalf("SyncNow: %v", err)
	}
	got := readFileString(t, filepath.Join(rootDir, "docs", "a.md"))
	for _, originalLink := range []string{"[Bad Encoding](/docs/%zz)", "[Escape](../../outside.md)"} {
		if !strings.Contains(got, originalLink) {
			t.Fatalf("source file = %q, want invalid link %q left unchanged", got, originalLink)
		}
	}
	page, err := treeService.GetPage("page-a")
	if err != nil {
		t.Fatalf("GetPage page-a: %v", err)
	}
	for _, originalLink := range []string{"[Bad Encoding](/docs/%zz)", "[Escape](../../outside.md)"} {
		if !strings.Contains(page.RawContent, originalLink) {
			t.Fatalf("tree raw content = %q, want invalid link %q left unchanged", page.RawContent, originalLink)
		}
	}
	if len(status.ValidationErrors) != 2 {
		t.Fatalf("ValidationErrors = %#v, want two invalid link errors", status.ValidationErrors)
	}
	for _, validationError := range status.ValidationErrors {
		if validationError.Path != "docs/a" || validationError.Code != "invalid_link" {
			t.Fatalf("ValidationErrors = %#v, want invalid_link details for docs/a", status.ValidationErrors)
		}
	}
})

// - Ambiguous extensionless link is left as validation error
var _ = It("ServiceSyncNowReportsAmbiguousLegacyLinkWhenMigrationCannotRewrite", func() {
	t := GinkgoT()
	dataDir := t.TempDir()
	rootDir := filepath.Join(t.TempDir(), "workspace")
	treeService := tree.NewTreeServiceWithOptions(tree.TreeOptions{DataDir: dataDir, RootDir: rootDir})
	if err := treeService.LoadTree(); err != nil {
		t.Fatalf("LoadTree: %v", err)
	}
	sourceOriginal := `---
leafwiki_id: page-a
leafwiki_title: Page A
---
# Page A

[Sync](/docs/sync)
`
	writeMarkdown(t, filepath.Join(rootDir, "docs", "a.md"), sourceOriginal)
	writeMarkdown(t, filepath.Join(rootDir, "docs", "sync.md"), `---
leafwiki_id: sync-page
leafwiki_title: Sync Page
---
# Sync Page
`)
	writeMarkdown(t, filepath.Join(rootDir, "docs", "sync", "index.md"), `---
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
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}

	status, err := service.SyncNow(context.Background(), SyncRequest{
		Reason: ReasonExplicit,
		Source: SourceFilesystem,
		Actor:  PublicEditorActor(),
	})
	if err != nil {
		t.Fatalf("SyncNow: %v", err)
	}
	if got := readFileString(t, filepath.Join(rootDir, "docs", "a.md")); !strings.Contains(got, "[Sync](/docs/sync)") {
		t.Fatalf("source file = %q, want ambiguous legacy link left unchanged", got)
	}
	page, err := treeService.GetPage("page-a")
	if err != nil {
		t.Fatalf("GetPage page-a: %v", err)
	}
	if !strings.Contains(page.RawContent, "[Sync](/docs/sync)") {
		t.Fatalf("tree raw content = %q, want ambiguous legacy link left unchanged", page.RawContent)
	}
	if len(status.ValidationErrors) == 0 {
		t.Fatalf("ValidationErrors empty, want ambiguous legacy migration issue")
	}
	if !strings.Contains(status.ValidationErrors[0].Message, "ambiguous_legacy_link") ||
		!strings.Contains(status.ValidationErrors[0].Message, "/docs/sync") {
		t.Fatalf("ValidationErrors = %#v, want ambiguous legacy link detail", status.ValidationErrors)
	}
})

var _ = It("ServiceSyncNowPreservesMigrationAmbiguityWhenNormalValidationAlsoFails", func() {
	t := GinkgoT()
	dataDir := t.TempDir()
	rootDir := filepath.Join(t.TempDir(), "workspace")
	treeService := tree.NewTreeServiceWithOptions(tree.TreeOptions{DataDir: dataDir, RootDir: rootDir})
	if err := treeService.LoadTree(); err != nil {
		t.Fatalf("LoadTree: %v", err)
	}
	writeMarkdown(t, filepath.Join(rootDir, "docs", "a.md"), `---
leafwiki_id: page-a
leafwiki_title: Page A
---
# Page A

[Sync](/docs/sync)
[Missing](/docs/missing)
`)
	writeMarkdown(t, filepath.Join(rootDir, "docs", "sync.md"), `---
leafwiki_id: sync-page
leafwiki_title: Sync Page
---
# Sync Page
`)
	writeMarkdown(t, filepath.Join(rootDir, "docs", "sync", "index.md"), `---
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
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}

	status, err := service.SyncNow(context.Background(), SyncRequest{
		Reason: ReasonExplicit,
		Source: SourceFilesystem,
		Actor:  PublicEditorActor(),
	})
	if err != nil {
		t.Fatalf("SyncNow: %v", err)
	}

	assertValidationErrorContains := func(needle string) {
		t.Helper()
		for _, validationError := range status.ValidationErrors {
			if strings.Contains(validationError.Message, needle) {
				return
			}
		}
		t.Fatalf("ValidationErrors = %#v, want message containing %q", status.ValidationErrors, needle)
	}
	assertValidationErrorCode := func(code wikivalidation.IssueCode) {
		t.Helper()
		for _, validationError := range status.ValidationErrors {
			if validationError.Code == code {
				return
			}
		}
		t.Fatalf("ValidationErrors = %#v, want code %q", status.ValidationErrors, code)
	}
	assertValidationErrorCode(wikivalidation.IssueCodeAmbiguousLegacyLink)
	assertValidationErrorContains("/docs/missing")
})

var _ = It("CanonicalMigrationValidationErrorsUseNormalizedRoutePath", func() {
	t := GinkgoT()
	validationErrors := canonicalMigrationValidationErrors(t.TempDir(), "plans/agent_hooks.PLAN.md", []markdownlinks.Issue{{
		Code:        markdownlinks.IssueCodeAmbiguousLegacyLink,
		Destination: "/plans/sync",
	}})

	if len(validationErrors) != 1 {
		t.Fatalf("validationErrors = %#v, want one ambiguous migration error", validationErrors)
	}
	if validationErrors[0].Path != "plans/agent-hooks-plan" {
		t.Fatalf("validation error path = %q, want plans/agent-hooks-plan", validationErrors[0].Path)
	}
})

// - Old extensionless section link remains extensionless
var _ = It("ServiceSyncNowCanonicalizesSectionTrailingSlashWithoutRevisionLoop", func() {
	t := GinkgoT()
	dataDir := t.TempDir()
	rootDir := filepath.Join(t.TempDir(), "workspace")
	treeService := tree.NewTreeServiceWithOptions(tree.TreeOptions{DataDir: dataDir, RootDir: rootDir})
	if err := treeService.LoadTree(); err != nil {
		t.Fatalf("LoadTree: %v", err)
	}
	writeMarkdown(t, filepath.Join(rootDir, "docs", "a.md"), `---
leafwiki_id: page-a
leafwiki_title: Page A
---
# Page A

[Sync](/docs/sync/)
`)
	writeMarkdown(t, filepath.Join(rootDir, "docs", "sync", "index.md"), `---
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
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	for i := 0; i < 2; i++ {
		if _, err := service.SyncNow(context.Background(), SyncRequest{
			Reason: ReasonExplicit,
			Source: SourceFilesystem,
			Actor:  PublicEditorActor(),
		}); err != nil {
			t.Fatalf("SyncNow %d: %v", i+1, err)
		}
	}

	raw, err := os.ReadFile(filepath.Join(rootDir, "docs", "a.md"))
	if err != nil {
		t.Fatalf("ReadFile a.md: %v", err)
	}
	if !strings.Contains(string(raw), "[Sync](/docs/sync)") || strings.Contains(string(raw), "/docs/sync/") {
		t.Fatalf("a.md = %q, want section trailing slash canonicalized away", string(raw))
	}
	snapshots, err := service.ListSnapshots(context.Background(), 10)
	if err != nil {
		t.Fatalf("ListSnapshots: %v", err)
	}
	if len(snapshots) != 2 {
		t.Fatalf("snapshot count = %d, want raw sync plus canonical writeback with no repeat canonicalization revision: %#v", len(snapshots), snapshots)
	}
})

var _ = It("ServiceSyncNowPreservesOriginalChangedMarkdownCountWhenAmendingMetadataWritebacks", func() {
	t := GinkgoT()
	dataDir := t.TempDir()
	rootDir := filepath.Join(t.TempDir(), "workspace")
	treeService := tree.NewTreeServiceWithOptions(tree.TreeOptions{DataDir: dataDir, RootDir: rootDir})
	if err := treeService.LoadTree(); err != nil {
		t.Fatalf("LoadTree: %v", err)
	}
	writeMarkdown(t, filepath.Join(rootDir, "already-has-metadata.md"), `<!-- leafwiki
version: 1
page:
  id: page-ready
  title: Already Has Metadata
  created_at: 2026-06-07T10:00:00Z
  updated_at: 2026-06-07T10:00:00Z
-->

# Already Has Metadata
`)
	writeMarkdown(t, filepath.Join(rootDir, "needs-metadata.md"), "# Needs Metadata\n\nbody")

	service, err := NewService(ServiceOptions{
		Enabled: true,
		DataDir: dataDir,
		RootDir: rootDir,
		Tree:    treeService,
	})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}

	if _, err := service.SyncNow(context.Background(), SyncRequest{
		Reason: ReasonExplicit,
		Source: SourceFilesystem,
		Actor:  PublicEditorActor(),
	}); err != nil {
		t.Fatalf("SyncNow: %v", err)
	}

	snapshots, err := service.ListSnapshots(context.Background(), 10)
	if err != nil {
		t.Fatalf("ListSnapshots: %v", err)
	}
	if len(snapshots) != 1 {
		t.Fatalf("snapshot count = %d, want one amended commit: %#v", len(snapshots), snapshots)
	}
	if snapshots[0].ChangedMarkdownCount != 2 {
		t.Fatalf("ChangedMarkdownCount = %d, want original two-file snapshot count", snapshots[0].ChangedMarkdownCount)
	}
})

var _ = It("ServiceSyncNowRecordsChangedMarkdownPaths", func() {
	t := GinkgoT()
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
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}

	status, err := service.SyncNow(context.Background(), SyncRequest{
		Reason: ReasonExplicit,
		Source: SourceFilesystem,
		Actor:  PublicEditorActor(),
	})
	if err != nil {
		t.Fatalf("SyncNow: %v", err)
	}

	if status.LastCommitHash != "abc123" {
		t.Fatalf("LastCommitHash = %q, want abc123", status.LastCommitHash)
	}
	if strings.Join(status.RecentChangedMarkdownPaths, ",") != "docs/a.md,docs/b.md" {
		t.Fatalf("RecentChangedMarkdownPaths = %#v", status.RecentChangedMarkdownPaths)
	}
	if got := fakeTree.reconstructCount(); got != 1 {
		t.Fatalf("reconstructs = %d, want 1", got)
	}
})

var _ = It("ServiceSyncNowLogsStartupPhases", func() {
	t := GinkgoT()
	var logs bytes.Buffer
	service, err := NewService(ServiceOptions{
		Enabled: true,
		RootDir: t.TempDir(),
		Tree:    &fakeTreeReconstructor{},
		Store: &fakeRevisionStore{
			capture: &gitrevisions.Commit{
				Hash:                 "startup-commit",
				ChangedMarkdownPaths: []string{"docs/a.md"},
			},
		},
		Log: slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelInfo})),
	})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}

	if _, err := service.SyncNow(context.Background(), SyncRequest{
		Reason: ReasonStartup,
		Source: SourceFilesystem,
		Actor:  PublicEditorActor(),
	}); err != nil {
		t.Fatalf("SyncNow: %v", err)
	}

	logText := logs.String()
	for _, phase := range []string{
		`phase=capture_snapshot`,
		`phase=reconstruct_tree`,
		`phase=canonical_link_migration`,
		`phase=capture_writebacks`,
		`phase=validate_and_after_sync`,
	} {
		if !strings.Contains(logText, phase) {
			t.Fatalf("startup sync logs missing %s:\n%s", phase, logText)
		}
	}
	if !strings.Contains(logText, "workspace sync startup phase started") {
		t.Fatalf("startup sync logs missing phase start message:\n%s", logText)
	}
	if !strings.Contains(logText, "workspace sync startup phase completed") {
		t.Fatalf("startup sync logs missing phase completion message:\n%s", logText)
	}
})

var _ = It("ServiceListSnapshotPagePropagatesChangedMarkdownPathErrors", func() {
	t := GinkgoT()
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
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}

	_, err = service.ListSnapshotPage(context.Background(), CommitHash(""), 10)
	Expect(err).To(MatchError(errChangedPathTrailerReadFailed))
})

var _ = It("ServiceListSnapshotPageDoesNotReadChangedPathsForSentinelCommit", func() {
	t := GinkgoT()
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
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}

	page, err := service.ListSnapshotPage(context.Background(), CommitHash(""), 1)
	if err != nil {
		t.Fatalf("ListSnapshotPage returned sentinel error: %v", err)
	}
	if len(page.Snapshots) != 1 || page.Snapshots[0].ID != "returned" {
		t.Fatalf("snapshots = %#v, want only returned snapshot", page.Snapshots)
	}
	if page.NextCursor != "returned" {
		t.Fatalf("NextCursor = %q, want returned", page.NextCursor)
	}
})

var _ = It("ServiceListSnapshotPageDoesNotBlockStatusThroughSyncNowWhileReadingChangedPaths", func() {
	t := GinkgoT()
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
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}

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

	Eventually(statusDone).WithTimeout(200 * time.Millisecond).Should(Receive(WithTransform(func(status SyncStatus) bool {
		return status.Enabled
	}, BeTrue())))

	unblockChangedPaths()
	Eventually(listDone).Should(Receive(Succeed()))
	Eventually(syncDone).Should(Receive(Succeed()))
})

var _ = It("ServiceSyncNowRunsAfterSyncWhenValidationWarningsExist", func() {
	t := GinkgoT()
	dataDir := t.TempDir()
	rootDir := filepath.Join(t.TempDir(), "workspace")
	treeService := tree.NewTreeServiceWithOptions(tree.TreeOptions{DataDir: dataDir, RootDir: rootDir})
	if err := treeService.LoadTree(); err != nil {
		t.Fatalf("LoadTree: %v", err)
	}
	writeMarkdown(t, filepath.Join(rootDir, "valid-page.md"), `---
leafwiki_id: valid-page
leafwiki_title: Valid Page
---

# Valid Page
`)
	writeMarkdown(t, filepath.Join(rootDir, "!!!.md"), "---\nleafwiki_id: invalid-slug\nleafwiki_title: Invalid Slug\n---\n# Invalid Slug\n")
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
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}

	status, err := service.SyncNow(context.Background(), SyncRequest{
		Reason: ReasonExplicit,
		Source: SourceFilesystem,
		Actor:  PublicEditorActor(),
	})
	if err != nil {
		t.Fatalf("SyncNow: %v", err)
	}

	if got := atomic.LoadInt32(&afterSyncCalls); got != 1 {
		t.Fatalf("afterSync calls = %d, want 1 rebuild after successful reconstruction with validation warnings", got)
	}
	if len(status.ValidationErrors) == 0 {
		t.Fatalf("ValidationErrors empty, want invalid slug warning")
	}
})

var _ = It("ServiceSyncNowRecordsValidationErrorsWithMarkdownPaths", func() {
	t := GinkgoT()
	fakeTree := &fakeTreeReconstructor{err: errors.New(`duplicate leafwiki_id "dup" in /workspace/a.md and /workspace/docs/b.md`)}
	service, err := NewService(ServiceOptions{
		Enabled: true,
		RootDir: "/workspace",
		Tree:    fakeTree,
		Store:   &fakeRevisionStore{capture: &gitrevisions.Commit{Hash: "abc123"}},
	})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}

	status, err := service.SyncNow(context.Background(), SyncRequest{
		Reason: ReasonExplicit,
		Source: SourceFilesystem,
		Actor:  PublicEditorActor(),
	})
	if err != nil {
		t.Fatalf("SyncNow: %v", err)
	}

	if len(status.ValidationErrors) != 2 {
		t.Fatalf("ValidationErrors = %#v, want two path-specific errors", status.ValidationErrors)
	}
	got := []string{status.ValidationErrors[0].Path, status.ValidationErrors[1].Path}
	if strings.Join(got, ",") != "a.md,docs/b.md" {
		t.Fatalf("ValidationError paths = %#v, want a.md and docs/b.md", got)
	}
})

var _ = It("ServiceSyncNowReportsDuplicateCanonicalPageIDAsTypedValidationError", func() {
	t := GinkgoT()
	dataDir := t.TempDir()
	rootDir := filepath.Join(t.TempDir(), "workspace")
	treeService := tree.NewTreeServiceWithOptions(tree.TreeOptions{DataDir: dataDir, RootDir: rootDir})
	writeMarkdown(t, filepath.Join(rootDir, "duplicate-a.md"), `<!-- leafwiki
version: 1
page:
  id: duplicate-page-id
  title: Duplicate A
-->

# Duplicate A
`)
	writeMarkdown(t, filepath.Join(rootDir, "duplicate-b.md"), `<!-- leafwiki
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
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}

	status, err := service.SyncNow(context.Background(), SyncRequest{
		Reason: ReasonExplicit,
		Source: SourceFilesystem,
		Actor:  PublicEditorActor(),
	})
	if err != nil {
		t.Fatalf("SyncNow: %v", err)
	}

	for _, validationError := range status.ValidationErrors {
		if validationError.Code != "duplicate_leafwiki_id" {
			continue
		}
		if validationError.Path != "duplicate-b.md" {
			t.Fatalf("duplicate error path = %q, want duplicate-b.md", validationError.Path)
		}
		if validationError.Severity != "error" {
			t.Fatalf("duplicate error severity = %q, want error", validationError.Severity)
		}
		if !strings.Contains(validationError.Message, "page.id") {
			t.Fatalf("duplicate error message = %q, want canonical page.id wording", validationError.Message)
		}
		return
	}
	t.Fatalf("ValidationErrors = %#v, want duplicate_leafwiki_id", status.ValidationErrors)
})

var _ = It("ServiceSyncNowRecordsAdditionalBatchActors", func() {
	t := GinkgoT()
	dataDir := t.TempDir()
	rootDir := filepath.Join(t.TempDir(), "workspace")
	treeService := tree.NewTreeServiceWithOptions(tree.TreeOptions{DataDir: dataDir, RootDir: rootDir})
	writeMarkdown(t, filepath.Join(rootDir, "page.md"), "---\nleafwiki_id: page\nleafwiki_title: Page\n---\n# Page\n")
	store, err := gitrevisions.Open(gitrevisions.StoreOptions{DataDir: dataDir, RootDir: rootDir})
	if err != nil {
		t.Fatalf("Open store: %v", err)
	}
	service, err := NewService(ServiceOptions{
		Enabled: true,
		DataDir: dataDir,
		RootDir: rootDir,
		Tree:    treeService,
		Store:   store,
	})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}

	status, err := service.SyncNow(context.Background(), SyncRequest{
		Reason: ReasonExplicit,
		Source: SourceWeb,
		Actor:  Actor{ID: "alice", Name: "Alice", Email: "alice@example.test"},
		AdditionalActors: []Actor{
			{ID: "bob", Name: "Bob", Email: "bob@example.test"},
		},
	})
	if err != nil {
		t.Fatalf("SyncNow: %v", err)
	}

	commit, err := store.GetCommit(context.Background(), status.LastCommitHash)
	if err != nil {
		t.Fatalf("GetCommit: %v", err)
	}
	if commit.AuthorID != "alice" {
		t.Fatalf("AuthorID = %q, want alice", commit.AuthorID)
	}
	if strings.Join(gitRevisionActorIDStrings(commit.ActorIDs), ",") != "alice,bob" {
		t.Fatalf("ActorIDs = %#v, want alice,bob", commit.ActorIDs)
	}
})

var _ = It("ServiceSyncNowStopsBeforeRebuildWhenGitCaptureFails", func() {
	t := GinkgoT()
	captureErr := errors.New("git storage read-only")
	fakeTree := &fakeTreeReconstructor{}
	service, err := NewService(ServiceOptions{
		Enabled: true,
		Tree:    fakeTree,
		Store:   &fakeRevisionStore{captureErr: captureErr},
	})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}

	status, err := service.SyncNow(context.Background(), SyncRequest{
		Reason: ReasonExplicit,
		Source: SourceFilesystem,
		Actor:  PublicEditorActor(),
	})
	if !errors.Is(err, captureErr) {
		t.Fatalf("SyncNow error = %v, want %v", err, captureErr)
	}
	if got := fakeTree.reconstructCount(); got != 0 {
		t.Fatalf("reconstructs = %d, want 0", got)
	}
	if !strings.Contains(status.LastError, "git storage read-only") {
		t.Fatalf("LastError = %q, want git error", status.LastError)
	}
})

var _ = It("ServiceListPageRevisionsUsesCommitAuthorMetadata", func() {
	t := GinkgoT()
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
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}

	result, err := service.ListPageRevisions(context.Background(), page, "", 10)
	if err != nil {
		t.Fatalf("ListPageRevisions: %v", err)
	}
	revisions := result.Revisions

	if len(revisions) != 1 {
		t.Fatalf("revision count = %d, want 1", len(revisions))
	}
	if revisions[0].AuthorID != "alice" {
		t.Fatalf("AuthorID = %q, want alice", revisions[0].AuthorID)
	}
	if !revisions[0].CreatedAt.Equal(createdAt) {
		t.Fatalf("CreatedAt = %v, want %v", revisions[0].CreatedAt, createdAt)
	}
})

var _ = It("ServiceListPageRevisionsPaginatesMoreThanLimitPageCommits", func() {
	t := GinkgoT()
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
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}

	firstPage, err := service.ListPageRevisions(context.Background(), page, "", 2)
	if err != nil {
		t.Fatalf("ListPageRevisions first page: %v", err)
	}
	if got := revisionIDs(firstPage.Revisions); strings.Join(got, ",") != "page-a-change-3,page-a-change-2" {
		t.Fatalf("first page revision ids = %#v, want latest two page commits", got)
	}
	if firstPage.NextCursor != "page-a-change-2" {
		t.Fatalf("first page next cursor = %q, want page-a-change-2", firstPage.NextCursor)
	}

	secondPage, err := service.ListPageRevisions(context.Background(), page, firstPage.NextCursor, 2)
	if err != nil {
		t.Fatalf("ListPageRevisions second page: %v", err)
	}
	if got := revisionIDs(secondPage.Revisions); strings.Join(got, ",") != "page-a-change-1" {
		t.Fatalf("second page revision ids = %#v, want oldest page commit", got)
	}
	if secondPage.NextCursor != "" {
		t.Fatalf("second page next cursor = %q, want empty final cursor", secondPage.NextCursor)
	}
})

var _ = It("ServiceListPageRevisionsOmitsNextCursorWhenMatchesEqualLimit", func() {
	t := GinkgoT()
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
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}

	result, err := service.ListPageRevisions(context.Background(), page, "", 2)
	if err != nil {
		t.Fatalf("ListPageRevisions: %v", err)
	}

	if got := revisionIDs(result.Revisions); strings.Join(got, ",") != "page-a-change-2,page-a-change-1" {
		t.Fatalf("revision ids = %#v, want both page commits", got)
	}
	if result.NextCursor != "" {
		t.Fatalf("next cursor = %q, want empty cursor when matches equal limit", result.NextCursor)
	}
})

var _ = It("ServiceListPageRevisionsFollowsMarkdownRenameByLeafWikiID", func() {
	t := GinkgoT()
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
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}

	result, err := service.ListPageRevisions(context.Background(), page, "", 10)
	if err != nil {
		t.Fatalf("ListPageRevisions: %v", err)
	}
	revisions := result.Revisions

	if len(revisions) != 2 {
		t.Fatalf("revision count = %d, want 2", len(revisions))
	}
	if revisions[0].Path != "new-page" {
		t.Fatalf("latest revision path = %q, want new-page", revisions[0].Path)
	}
	if revisions[1].Path != "old-page" {
		t.Fatalf("renamed revision path = %q, want old-page", revisions[1].Path)
	}
})

var _ = It("ServiceListPageRevisionsMatchesUppercaseMarkdownExtensionByLeafWikiID", func() {
	t := GinkgoT()
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
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}

	result, err := service.ListPageRevisions(context.Background(), page, "", 10)
	if err != nil {
		t.Fatalf("ListPageRevisions: %v", err)
	}

	if len(result.Revisions) != 1 {
		t.Fatalf("revision count = %d, want uppercase Markdown commit", len(result.Revisions))
	}
	if result.Revisions[0].Path != "Page" {
		t.Fatalf("revision path = %q, want Page", result.Revisions[0].Path)
	}
})

var _ = It("ServiceListPageRevisionsMatchesNormalizedRawPathWithoutMetadata", func() {
	t := GinkgoT()
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
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}

	result, err := service.ListPageRevisions(context.Background(), page, "", 10)
	if err != nil {
		t.Fatalf("ListPageRevisions: %v", err)
	}

	if len(result.Revisions) != 1 {
		t.Fatalf("revision count = %d, want raw normalized path commit", len(result.Revisions))
	}
	if result.Revisions[0].Path != "plans/agent-hooks-plan" {
		t.Fatalf("revision path = %q, want plans/agent-hooks-plan", result.Revisions[0].Path)
	}
	snapshot, err := service.GetPageRevisionSnapshot(context.Background(), page, CommitHashFromRevisionID(result.Revisions[0].ID))
	if err != nil {
		t.Fatalf("GetPageRevisionSnapshot: %v", err)
	}
	if snapshot.Revision.Path != "plans/agent-hooks-plan" {
		t.Fatalf("snapshot revision path = %q, want plans/agent-hooks-plan", snapshot.Revision.Path)
	}
	if !strings.Contains(snapshot.Content, "Raw content before writeback.") {
		t.Fatalf("snapshot content = %q, want raw content", snapshot.Content)
	}
})

var _ = It("ServiceListPageRevisionsUsesHistoricalMarkdownMetadata", func() {
	t := GinkgoT()
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
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}

	result, err := service.ListPageRevisions(context.Background(), page, "", 1)
	if err != nil {
		t.Fatalf("ListPageRevisions: %v", err)
	}
	revisions := result.Revisions

	if len(revisions) != 1 {
		t.Fatalf("revision count = %d, want 1", len(revisions))
	}
	if revisions[0].Title != "Historical Title" {
		t.Fatalf("revision title = %q, want Historical Title", revisions[0].Title)
	}
	if revisions[0].Slug != "old-page" {
		t.Fatalf("revision slug = %q, want old-page", revisions[0].Slug)
	}
	if revisions[0].Kind != tree.NodeKindPage {
		t.Fatalf("revision kind = %q, want page", revisions[0].Kind)
	}
	if revisions[0].Path != "old-page" {
		t.Fatalf("revision path = %q, want old-page", revisions[0].Path)
	}
})

var _ = It("ServiceListPageRevisionsNormalizesSectionIndexPath", func() {
	t := GinkgoT()
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
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}

	result, err := service.ListPageRevisions(context.Background(), page, "", 1)
	if err != nil {
		t.Fatalf("ListPageRevisions: %v", err)
	}
	revisions := result.Revisions

	if len(revisions) != 1 {
		t.Fatalf("revision count = %d, want 1", len(revisions))
	}
	if revisions[0].Title != "Historical Docs" {
		t.Fatalf("revision title = %q, want Historical Docs", revisions[0].Title)
	}
	if revisions[0].Slug != "docs" {
		t.Fatalf("revision slug = %q, want docs", revisions[0].Slug)
	}
	if revisions[0].Kind != tree.NodeKindSection {
		t.Fatalf("revision kind = %q, want section", revisions[0].Kind)
	}
	if revisions[0].Path != "docs" {
		t.Fatalf("revision path = %q, want docs", revisions[0].Path)
	}
})

var _ = It("ServiceListPageRevisionsNormalizesReadmeFallbackSectionPath", func() {
	t := GinkgoT()
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
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}

	result, err := service.ListPageRevisions(context.Background(), page, "", 1)
	if err != nil {
		t.Fatalf("ListPageRevisions: %v", err)
	}
	revisions := result.Revisions

	if len(revisions) != 1 {
		t.Fatalf("revision count = %d, want 1", len(revisions))
	}
	if revisions[0].Title != "Historical Guides" {
		t.Fatalf("revision title = %q, want Historical Guides", revisions[0].Title)
	}
	if revisions[0].Slug != "guides" {
		t.Fatalf("revision slug = %q, want guides", revisions[0].Slug)
	}
	if revisions[0].Kind != tree.NodeKindSection {
		t.Fatalf("revision kind = %q, want section", revisions[0].Kind)
	}
	if revisions[0].Path != "guides" {
		t.Fatalf("revision path = %q, want guides", revisions[0].Path)
	}
})

var _ = It("ServiceListPageRevisionsMapsReadmeAsPageWhenWorkspaceDirHasIndex", func() {
	t := GinkgoT()
	rootDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(rootDir, "User Guides"), 0o755); err != nil {
		t.Fatalf("create workspace section: %v", err)
	}
	writeMarkdown(t, filepath.Join(rootDir, "User Guides", "index.md"), "# User Guides\n")
	writeMarkdown(t, filepath.Join(rootDir, "User Guides", "README.md"), "---\nleafwiki_id: readme-page\nleafwiki_title: Historical README\n---\n# Historical README\n")

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
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}

	result, err := service.ListPageRevisions(context.Background(), page, "", 1)
	if err != nil {
		t.Fatalf("ListPageRevisions: %v", err)
	}
	revisions := result.Revisions

	if len(revisions) != 1 {
		t.Fatalf("revision count = %d, want 1", len(revisions))
	}
	if revisions[0].Kind != tree.NodeKindPage {
		t.Fatalf("revision kind = %q, want page", revisions[0].Kind)
	}
	if revisions[0].Path != "user-guides/README" {
		t.Fatalf("revision path = %q, want user-guides/README", revisions[0].Path)
	}
	if revisions[0].Slug != "README" {
		t.Fatalf("revision slug = %q, want README", revisions[0].Slug)
	}
})

var _ = It("ServiceListPageRevisionsKeepsHistoricalPageKindAfterSectionConversion", func() {
	t := GinkgoT()
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
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}

	result, err := service.ListPageRevisions(context.Background(), page, "", 1)
	if err != nil {
		t.Fatalf("ListPageRevisions: %v", err)
	}
	revisions := result.Revisions

	if len(revisions) != 1 {
		t.Fatalf("revision count = %d, want 1", len(revisions))
	}
	if revisions[0].Kind != tree.NodeKindPage {
		t.Fatalf("revision kind = %q, want historical page kind", revisions[0].Kind)
	}
	if revisions[0].Path != "docs" {
		t.Fatalf("revision path = %q, want docs", revisions[0].Path)
	}
})

var _ = It("ServiceListPageRevisionsOnlyIncludesCommitsThatChangedDocument", func() {
	t := GinkgoT()
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
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}

	result, err := service.ListPageRevisions(context.Background(), page, "", 10)
	if err != nil {
		t.Fatalf("ListPageRevisions: %v", err)
	}
	revisions := result.Revisions

	if len(revisions) != 1 {
		t.Fatalf("revision count = %d, want only page A commit: %#v", len(revisions), revisions)
	}
	if revisions[0].ID != "page-a-change" {
		t.Fatalf("revision id = %q, want page-a-change", revisions[0].ID)
	}
})

var _ = It("ServiceListPageRevisionsScansPastUnrelatedHeadWhenLimitIsOne", func() {
	t := GinkgoT()
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
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}

	result, err := service.ListPageRevisions(context.Background(), page, "", 1)
	if err != nil {
		t.Fatalf("ListPageRevisions: %v", err)
	}
	revisions := result.Revisions

	if len(revisions) != 1 {
		t.Fatalf("revision count = %d, want latest page A revision after unrelated HEAD", len(revisions))
	}
	if revisions[0].ID != "page-a-change" {
		t.Fatalf("revision id = %q, want page-a-change", revisions[0].ID)
	}
})

var _ = It("ServiceListPageRevisionsScansAllCommitsPastLargeUnrelatedHead", func() {
	t := GinkgoT()
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
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}

	result, err := service.ListPageRevisions(context.Background(), page, "", 1)
	if err != nil {
		t.Fatalf("ListPageRevisions: %v", err)
	}
	revisions := result.Revisions

	if len(revisions) != 1 {
		t.Fatalf("revision count = %d, want page A revision after many unrelated commits", len(revisions))
	}
	if revisions[0].ID != "page-a-change" {
		t.Fatalf("revision id = %q, want page-a-change", revisions[0].ID)
	}
})

var _ = It("ServiceListPageRevisionsStopsScanningAfterConfirmedNextCursor", func() {
	t := GinkgoT()
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
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}

	result, err := service.ListPageRevisions(context.Background(), page, "", 1)
	if err != nil {
		t.Fatalf("ListPageRevisions: %v", err)
	}
	revisions := result.Revisions

	if len(revisions) != 1 || revisions[0].ID != "page-a-change-2" {
		t.Fatalf("revisions = %#v, want only latest page A change", revisions)
	}
	if result.NextCursor != "page-a-change-2" {
		t.Fatalf("next cursor = %q, want page-a-change-2", result.NextCursor)
	}
	if store.scannedCommits != 2 {
		t.Fatalf("scannedCommits = %d, want 2", store.scannedCommits)
	}
})

var _ = It("ServiceListPageRevisionsDoesNotLoadFullTreesWhileScanning", func() {
	t := GinkgoT()
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
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}

	result, err := service.ListPageRevisions(context.Background(), page, "", 1)
	if err != nil {
		t.Fatalf("ListPageRevisions: %v", err)
	}
	revisions := result.Revisions

	if len(revisions) != 1 || revisions[0].ID != "page-a-change" {
		t.Fatalf("revisions = %#v, want page A change", revisions)
	}
	if store.filesAtCalls != 0 {
		t.Fatalf("FilesAt calls = %d, want document history scan to avoid full-tree loads", store.filesAtCalls)
	}
})

var _ = It("ServiceListPageRevisionsDoesNotBlockStatusWhileScanningStore", func() {
	t := GinkgoT()
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
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}

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

	Eventually(statusDone).WithTimeout(200 * time.Millisecond).Should(Receive(WithTransform(func(status SyncStatus) bool {
		return status.Enabled
	}, BeTrue())))
	unblockChangedContents()
	Eventually(done).Should(Receive(Succeed()))
})

var _ = It("ServiceGetPageRevisionSnapshotRejectsUnrelatedCommit", func() {
	t := GinkgoT()
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
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}

	if _, err := service.GetPageRevisionSnapshot(context.Background(), page, CommitHash("page-b-change")); err == nil {
		t.Fatalf("GetPageRevisionSnapshot returned unrelated commit, want error")
	}
})

var _ = It("ServiceGetPageRevisionSnapshotDoesNotBlockStatusWhileReadingStore", func() {
	t := GinkgoT()
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
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}

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

	Eventually(statusDone).WithTimeout(200 * time.Millisecond).Should(Receive(WithTransform(func(status SyncStatus) bool {
		return status.Enabled
	}, BeTrue())))
	unblockChangedContents()
	Eventually(done).Should(Receive(Succeed()))
})

var _ = It("ServiceRestoreDocumentRestoresPreRenameContentToCurrentPath", func() {
	t := GinkgoT()
	dataDir := t.TempDir()
	rootDir := filepath.Join(t.TempDir(), "workspace")
	store, err := gitrevisions.Open(gitrevisions.StoreOptions{DataDir: dataDir, RootDir: rootDir})
	if err != nil {
		t.Fatalf("Open store: %v", err)
	}
	writeMarkdown(t, filepath.Join(rootDir, "old-page.md"), "---\nleafwiki_id: page-1\nleafwiki_title: Old Page\n---\n# Old Page\n\nold content")
	oldCommit, err := store.Capture(context.Background(), gitrevisions.CommitRequest{
		Reason: ReasonExplicit,
		Source: SourceFilesystem,
		Actor:  PublicEditorActor(),
	})
	if err != nil {
		t.Fatalf("capture old commit: %v", err)
	}
	if err := os.Remove(filepath.Join(rootDir, "old-page.md")); err != nil {
		t.Fatalf("remove old path: %v", err)
	}
	writeMarkdown(t, filepath.Join(rootDir, "new-page.md"), "---\nleafwiki_id: page-1\nleafwiki_title: New Page\n---\n# New Page\n\nnew content")
	if _, err := store.Capture(context.Background(), gitrevisions.CommitRequest{
		Reason: ReasonExplicit,
		Source: SourceFilesystem,
		Actor:  PublicEditorActor(),
	}); err != nil {
		t.Fatalf("capture new commit: %v", err)
	}
	page := &tree.Page{PageNode: &tree.PageNode{ID: "page-1", Title: "New Page", Slug: "new-page", Kind: tree.NodeKindPage}}
	service, err := NewService(ServiceOptions{
		Enabled: true,
		Tree:    &fakeTreeReconstructor{},
		Store:   store,
	})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}

	if _, err := service.RestoreDocument(context.Background(), page, newFixtureCommitHash(oldCommit.Hash), PublicEditorActor()); err != nil {
		t.Fatalf("RestoreDocument from pre-rename commit: %v", err)
	}

	rawBytes, err := os.ReadFile(filepath.Join(rootDir, "new-page.md"))
	if err != nil {
		t.Fatalf("read restored current path: %v", err)
	}
	raw := string(rawBytes)
	if !strings.Contains(raw, "old content") {
		t.Fatalf("current path content after restore = %q, want old content", raw)
	}
})

var _ = It("ServiceRestoreDocumentPreservesExistingUppercaseMarkdownPath", func() {
	t := GinkgoT()
	dataDir := t.TempDir()
	rootDir := filepath.Join(t.TempDir(), "workspace")
	store, err := gitrevisions.Open(gitrevisions.StoreOptions{DataDir: dataDir, RootDir: rootDir})
	if err != nil {
		t.Fatalf("Open store: %v", err)
	}
	writeMarkdown(t, filepath.Join(rootDir, "Page.MD"), "---\nleafwiki_id: page-1\nleafwiki_title: Page\n---\n# Page\n\nold content")
	oldCommit, err := store.Capture(context.Background(), gitrevisions.CommitRequest{
		Reason: ReasonExplicit,
		Source: SourceFilesystem,
		Actor:  PublicEditorActor(),
	})
	if err != nil {
		t.Fatalf("capture old commit: %v", err)
	}
	writeMarkdown(t, filepath.Join(rootDir, "Page.MD"), "---\nleafwiki_id: page-1\nleafwiki_title: Page\n---\n# Page\n\ncurrent content")
	if _, err := store.Capture(context.Background(), gitrevisions.CommitRequest{
		Reason: ReasonExplicit,
		Source: SourceFilesystem,
		Actor:  PublicEditorActor(),
	}); err != nil {
		t.Fatalf("capture current commit: %v", err)
	}
	page := &tree.Page{PageNode: &tree.PageNode{ID: "page-1", Title: "Page", Slug: "Page", Kind: tree.NodeKindPage}}
	service, err := NewService(ServiceOptions{
		Enabled: true,
		DataDir: dataDir,
		RootDir: rootDir,
		Tree:    tree.NewTreeServiceWithOptions(tree.TreeOptions{DataDir: dataDir, RootDir: rootDir}),
		Store:   store,
	})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}

	if _, err := service.RestoreDocument(context.Background(), page, newFixtureCommitHash(oldCommit.Hash), PublicEditorActor()); err != nil {
		t.Fatalf("RestoreDocument from uppercase path: %v", err)
	}

	rawBytes, err := os.ReadFile(filepath.Join(rootDir, "Page.MD"))
	if err != nil {
		t.Fatalf("read uppercase restored path: %v", err)
	}
	if !strings.Contains(string(rawBytes), "old content") {
		t.Fatalf("Page.MD content = %q, want old content", string(rawBytes))
	}
	entries, err := os.ReadDir(rootDir)
	if err != nil {
		t.Fatalf("ReadDir root: %v", err)
	}
	for _, entry := range entries {
		if entry.Name() == "Page.md" {
			t.Fatalf("found lowercase duplicate Page.md in directory entries")
		}
	}
})

var _ = It("ServiceRestoreDocumentRestoresSectionIndexToCurrentSectionPath", func() {
	t := GinkgoT()
	dataDir := t.TempDir()
	rootDir := filepath.Join(t.TempDir(), "workspace")
	store, err := gitrevisions.Open(gitrevisions.StoreOptions{DataDir: dataDir, RootDir: rootDir})
	if err != nil {
		t.Fatalf("Open store: %v", err)
	}
	writeMarkdown(t, filepath.Join(rootDir, "docs", "index.md"), "---\nleafwiki_id: section-1\nleafwiki_title: Docs\n---\n# Docs\n\nold section content")
	oldCommit, err := store.Capture(context.Background(), gitrevisions.CommitRequest{
		Reason: ReasonExplicit,
		Source: SourceFilesystem,
		Actor:  PublicEditorActor(),
	})
	if err != nil {
		t.Fatalf("capture old commit: %v", err)
	}
	writeMarkdown(t, filepath.Join(rootDir, "docs", "index.md"), "---\nleafwiki_id: section-1\nleafwiki_title: Docs\n---\n# Docs\n\nnew section content")
	if _, err := store.Capture(context.Background(), gitrevisions.CommitRequest{
		Reason: ReasonExplicit,
		Source: SourceFilesystem,
		Actor:  PublicEditorActor(),
	}); err != nil {
		t.Fatalf("capture new commit: %v", err)
	}
	section := &tree.Page{PageNode: &tree.PageNode{ID: "section-1", Title: "Docs", Slug: "docs", Kind: tree.NodeKindSection}}
	service, err := NewService(ServiceOptions{
		Enabled: true,
		Tree:    &fakeTreeReconstructor{},
		Store:   store,
	})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}

	if _, err := service.RestoreDocument(context.Background(), section, newFixtureCommitHash(oldCommit.Hash), PublicEditorActor()); err != nil {
		t.Fatalf("RestoreDocument from section commit: %v", err)
	}

	indexBytes, err := os.ReadFile(filepath.Join(rootDir, "docs", "index.md"))
	if err != nil {
		t.Fatalf("read restored section index: %v", err)
	}
	if !strings.Contains(string(indexBytes), "old section content") {
		t.Fatalf("section index content = %q, want old section content", string(indexBytes))
	}
	if _, err := os.Stat(filepath.Join(rootDir, "docs.md")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("docs.md exists after section restore, want no page file; err=%v", err)
	}
})

var _ = It("ServiceRestoreDocumentRestoresReadmeFallbackSectionToReadmePath", func() {
	t := GinkgoT()
	dataDir := t.TempDir()
	rootDir := filepath.Join(t.TempDir(), "workspace")
	store, err := gitrevisions.Open(gitrevisions.StoreOptions{DataDir: dataDir, RootDir: rootDir})
	if err != nil {
		t.Fatalf("Open store: %v", err)
	}
	writeMarkdown(t, filepath.Join(rootDir, "docs", "README.md"), "---\nleafwiki_id: section-1\nleafwiki_title: Docs\n---\n# Docs\n\nold readme section content")
	oldCommit, err := store.Capture(context.Background(), gitrevisions.CommitRequest{
		Reason: ReasonExplicit,
		Source: SourceFilesystem,
		Actor:  PublicEditorActor(),
	})
	if err != nil {
		t.Fatalf("capture old commit: %v", err)
	}
	writeMarkdown(t, filepath.Join(rootDir, "docs", "README.md"), "---\nleafwiki_id: section-1\nleafwiki_title: Docs\n---\n# Docs\n\nnew readme section content")
	if _, err := store.Capture(context.Background(), gitrevisions.CommitRequest{
		Reason: ReasonExplicit,
		Source: SourceFilesystem,
		Actor:  PublicEditorActor(),
	}); err != nil {
		t.Fatalf("capture new commit: %v", err)
	}
	section := &tree.Page{PageNode: &tree.PageNode{ID: "section-1", Title: "Docs", Slug: "docs", Kind: tree.NodeKindSection}}
	service, err := NewService(ServiceOptions{
		Enabled: true,
		RootDir: rootDir,
		Tree:    &fakeTreeReconstructor{},
		Store:   store,
	})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}

	if _, err := service.RestoreDocument(context.Background(), section, newFixtureCommitHash(oldCommit.Hash), PublicEditorActor()); err != nil {
		t.Fatalf("RestoreDocument from README section commit: %v", err)
	}

	readmeBytes, err := os.ReadFile(filepath.Join(rootDir, "docs", "README.md"))
	if err != nil {
		t.Fatalf("read restored README section: %v", err)
	}
	if !strings.Contains(string(readmeBytes), "old readme section content") {
		t.Fatalf("README.md content = %q, want old readme section content", string(readmeBytes))
	}
	if _, err := os.Stat(filepath.Join(rootDir, "docs", "index.md")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("index.md exists after README section restore, want no new index; err=%v", err)
	}
})

var _ = It("ServiceRestoreDocumentRejectsCommitThatDidNotChangeDocument", func() {
	t := GinkgoT()
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
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}

	if _, err := service.RestoreDocument(context.Background(), page, CommitHash("page-b-change"), PublicEditorActor()); err == nil {
		t.Fatalf("RestoreDocument restored unrelated commit, want error")
	}
	if store.restoreDocumentToPathCalls != 0 {
		t.Fatalf("restoreDocumentToPathCalls = %d, want 0", store.restoreDocumentToPathCalls)
	}
})

var _ = It("ServiceRestoreDocumentUsesChangedContentWithoutLoadingFullTree", func() {
	t := GinkgoT()
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
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}

	if _, err := service.RestoreDocument(context.Background(), page, CommitHash("page-a-change"), PublicEditorActor()); err != nil {
		t.Fatalf("RestoreDocument: %v", err)
	}

	if store.filesAtCalls != 0 {
		t.Fatalf("FilesAt calls = %d, want restore to avoid full-tree load", store.filesAtCalls)
	}
	if store.restoreDocumentContentToPathCalls != 1 {
		t.Fatalf("restoreDocumentContentToPathCalls = %d, want 1", store.restoreDocumentContentToPathCalls)
	}
	if !strings.Contains(store.restoredContent, "Page A restored") {
		t.Fatalf("restoredContent = %q, want selected historical content", store.restoredContent)
	}
})

var _ = It("ServiceRestoreDocumentReturnsReconstructionError", func() {
	t := GinkgoT()
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
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}

	status, err := service.RestoreDocument(context.Background(), page, CommitHash("page-a-change"), PublicEditorActor())
	if !errors.Is(err, reconstructErr) {
		t.Fatalf("RestoreDocument error = %v, want %v", err, reconstructErr)
	}
	if !strings.Contains(status.LastError, "duplicate leafwiki_id") {
		t.Fatalf("LastError = %q, want reconstruct error", status.LastError)
	}
	if len(status.ValidationErrors) != 2 {
		t.Fatalf("ValidationErrors = %#v, want path-specific reconstruct errors", status.ValidationErrors)
	}
})

var _ = It("ServiceRestoreWorkspaceCapturesMetadataWriteback", func() {
	t := GinkgoT()
	dataDir := t.TempDir()
	rootDir := filepath.Join(t.TempDir(), "workspace")
	store, err := gitrevisions.Open(gitrevisions.StoreOptions{DataDir: dataDir, RootDir: rootDir})
	if err != nil {
		t.Fatalf("Open store: %v", err)
	}
	writeMarkdown(t, filepath.Join(rootDir, "needs-metadata.md"), "# Needs Metadata\n\nold body")
	rawCommit, err := store.Capture(context.Background(), gitrevisions.CommitRequest{
		Reason: ReasonExplicit,
		Source: SourceFilesystem,
		Actor:  PublicEditorActor(),
	})
	if err != nil {
		t.Fatalf("capture raw commit: %v", err)
	}
	treeService := tree.NewTreeServiceWithOptions(tree.TreeOptions{DataDir: dataDir, RootDir: rootDir})
	service, err := NewService(ServiceOptions{
		Enabled: true,
		DataDir: dataDir,
		RootDir: rootDir,
		Tree:    treeService,
		Store:   store,
	})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}

	status, err := service.RestoreWorkspace(context.Background(), newFixtureCommitHash(rawCommit.Hash), PublicEditorActor())
	if err != nil {
		t.Fatalf("RestoreWorkspace: %v", err)
	}

	files, err := store.FilesAt(context.Background(), status.LastCommitHash)
	if err != nil {
		t.Fatalf("FilesAt restore head: %v", err)
	}
	if !strings.Contains(files["needs-metadata.md"], "<!-- leafwiki\n") || !strings.Contains(files["needs-metadata.md"], "  id:") {
		t.Fatalf("restore commit did not include reconstructed canonical metadata writeback: %q", files["needs-metadata.md"])
	}
})

var _ = It("ServiceRestoreDocumentCapturesMetadataWriteback", func() {
	t := GinkgoT()
	dataDir := t.TempDir()
	rootDir := filepath.Join(t.TempDir(), "workspace")
	store, err := gitrevisions.Open(gitrevisions.StoreOptions{DataDir: dataDir, RootDir: rootDir})
	if err != nil {
		t.Fatalf("Open store: %v", err)
	}
	writeMarkdown(t, filepath.Join(rootDir, "needs-metadata.md"), "# Needs Metadata\n\nold body")
	rawCommit, err := store.Capture(context.Background(), gitrevisions.CommitRequest{
		Reason: ReasonExplicit,
		Source: SourceFilesystem,
		Actor:  PublicEditorActor(),
	})
	if err != nil {
		t.Fatalf("capture raw commit: %v", err)
	}
	writeMarkdown(t, filepath.Join(rootDir, "needs-metadata.md"), `---
leafwiki_id: page-1
leafwiki_title: Needs Metadata
leafwiki_created_at: 2026-06-07T10:00:00Z
leafwiki_updated_at: 2026-06-07T10:00:00Z
---

# Needs Metadata

current body`)
	if _, err := store.Capture(context.Background(), gitrevisions.CommitRequest{
		Reason: ReasonExplicit,
		Source: SourceFilesystem,
		Actor:  PublicEditorActor(),
	}); err != nil {
		t.Fatalf("capture current commit: %v", err)
	}
	treeService := tree.NewTreeServiceWithOptions(tree.TreeOptions{DataDir: dataDir, RootDir: rootDir})
	page := &tree.Page{PageNode: &tree.PageNode{ID: "page-1", Title: "Needs Metadata", Slug: "needs-metadata", Kind: tree.NodeKindPage}}
	service, err := NewService(ServiceOptions{
		Enabled: true,
		DataDir: dataDir,
		RootDir: rootDir,
		Tree:    treeService,
		Store:   store,
	})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}

	status, err := service.RestoreDocument(context.Background(), page, newFixtureCommitHash(rawCommit.Hash), PublicEditorActor())
	if err != nil {
		t.Fatalf("RestoreDocument: %v", err)
	}

	files, err := store.FilesAt(context.Background(), status.LastCommitHash)
	if err != nil {
		t.Fatalf("FilesAt restore head: %v", err)
	}
	if !strings.Contains(files["needs-metadata.md"], "<!-- leafwiki\n") || !strings.Contains(files["needs-metadata.md"], "  id:") {
		t.Fatalf("document restore commit did not include reconstructed canonical metadata writeback: %q", files["needs-metadata.md"])
	}
})

var _ = It("ServiceGetPageRevisionSnapshotRejectsPathReuseWithDifferentLeafWikiID", func() {
	t := GinkgoT()
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
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}

	if _, err := service.GetPageRevisionSnapshot(context.Background(), page, CommitHash("path-reuse")); err == nil {
		t.Fatalf("GetPageRevisionSnapshot accepted reused path with different leafwiki_id, want error")
	}
})

var _ = It("ServiceSyncNowCreatesNewCommitForWritebackOnlySync", func() {
	t := GinkgoT()
	dataDir := t.TempDir()
	rootDir := filepath.Join(t.TempDir(), "workspace")
	writeMarkdown(t, filepath.Join(rootDir, "needs-metadata.md"), "# Needs Metadata\n\nbody")
	store, err := gitrevisions.Open(gitrevisions.StoreOptions{DataDir: dataDir, RootDir: rootDir})
	if err != nil {
		t.Fatalf("Open store: %v", err)
	}
	firstCommit, err := store.Capture(context.Background(), gitrevisions.CommitRequest{
		Reason: ReasonExplicit,
		Source: SourceFilesystem,
		Actor:  PublicEditorActor(),
	})
	if err != nil {
		t.Fatalf("capture raw missing-metadata commit: %v", err)
	}
	treeService := tree.NewTreeServiceWithOptions(tree.TreeOptions{DataDir: dataDir, RootDir: rootDir})
	service, err := NewService(ServiceOptions{
		Enabled: true,
		DataDir: dataDir,
		RootDir: rootDir,
		Tree:    treeService,
		Store:   store,
	})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}

	status, err := service.SyncNow(context.Background(), SyncRequest{
		Reason: ReasonExplicit,
		Source: SourceFilesystem,
		Actor:  PublicEditorActor(),
	})
	if err != nil {
		t.Fatalf("SyncNow: %v", err)
	}
	if status.LastCommitHash == newFixtureCommitHash(firstCommit.Hash) {
		t.Fatalf("writeback-only sync amended previous commit %s, want new commit", firstCommit.Hash)
	}
	snapshots, err := service.ListSnapshots(context.Background(), 10)
	if err != nil {
		t.Fatalf("ListSnapshots: %v", err)
	}
	if len(snapshots) != 2 {
		t.Fatalf("snapshot count = %d, want raw commit plus writeback commit: %#v", len(snapshots), snapshots)
	}
})

var _ = It("ServiceSyncNowReportsValidationForSkippedInvalidSlugMarkdown", func() {
	t := GinkgoT()
	dataDir := t.TempDir()
	rootDir := filepath.Join(t.TempDir(), "workspace")
	treeService := tree.NewTreeServiceWithOptions(tree.TreeOptions{DataDir: dataDir, RootDir: rootDir})
	writeMarkdown(t, filepath.Join(rootDir, "!!!.md"), "---\nleafwiki_id: invalid-slug\nleafwiki_title: Invalid Slug\n---\n# Invalid Slug\n")
	service, err := NewService(ServiceOptions{
		Enabled: true,
		DataDir: dataDir,
		RootDir: rootDir,
		Tree:    treeService,
	})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}

	status, err := service.SyncNow(context.Background(), SyncRequest{
		Reason: ReasonExplicit,
		Source: SourceFilesystem,
		Actor:  PublicEditorActor(),
	})
	if err != nil {
		t.Fatalf("SyncNow: %v", err)
	}

	if len(status.ValidationErrors) == 0 {
		t.Fatalf("ValidationErrors empty, want invalid slug error")
	}
	if status.ValidationErrors[0].Path != "!!!.md" {
		t.Fatalf("validation error path = %q, want !!!.md", status.ValidationErrors[0].Path)
	}
})

var _ = It("ServiceSyncNowReportsNormalizedRouteConflictsAsPathConflict", func() {
	t := GinkgoT()
	dataDir := t.TempDir()
	rootDir := filepath.Join(t.TempDir(), "workspace")
	treeService := tree.NewTreeServiceWithOptions(tree.TreeOptions{DataDir: dataDir, RootDir: rootDir})
	writeMarkdown(t, filepath.Join(rootDir, "plans", "a_b.md"), "---\nleafwiki_id: a-b-one\nleafwiki_title: A B One\n---\n# A B One\n")
	writeMarkdown(t, filepath.Join(rootDir, "plans", "a-b.md"), "---\nleafwiki_id: a-b-two\nleafwiki_title: A B Two\n---\n# A B Two\n")
	service, err := NewService(ServiceOptions{
		Enabled: true,
		DataDir: dataDir,
		RootDir: rootDir,
		Tree:    treeService,
	})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}

	status, err := service.SyncNow(context.Background(), SyncRequest{
		Reason: ReasonExplicit,
		Source: SourceFilesystem,
		Actor:  PublicEditorActor(),
	})
	if err != nil {
		t.Fatalf("SyncNow: %v", err)
	}

	for _, validationError := range status.ValidationErrors {
		if validationError.Code == "path_conflict" {
			if !strings.Contains(validationError.Message, "plans/a_b.md") || !strings.Contains(validationError.Message, "plans/a-b.md") {
				t.Fatalf("path_conflict message = %q, want both source paths", validationError.Message)
			}
			return
		}
	}
	t.Fatalf("ValidationErrors = %#v, want path_conflict for normalized collision", status.ValidationErrors)
})

var _ = It("ServiceStartWatcherSyncsMarkdownEvents", func() {
	t := GinkgoT()
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
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	DeferCleanup(cancel)

	if err := service.StartWatcher(ctx); err != nil {
		t.Fatalf("StartWatcher: %v", err)
	}
	fakeWatcher.events <- watcherEvent{Path: "/workspace/docs/a.md"}

	waitUntil(t, func() bool { return fakeTree.reconstructCount() == 1 })
	status := service.Status()
	if !status.WatcherEnabled || !status.WatcherRunning {
		t.Fatalf("watcher status = enabled:%v running:%v, want enabled and running", status.WatcherEnabled, status.WatcherRunning)
	}
	if status.PendingEventCount != 0 {
		t.Fatalf("PendingEventCount = %d, want 0", status.PendingEventCount)
	}
	if strings.Join(status.RecentChangedMarkdownPaths, ",") != "docs/a.md" {
		t.Fatalf("RecentChangedMarkdownPaths = %#v", status.RecentChangedMarkdownPaths)
	}
	if fakeStore.captureCalls != 1 {
		t.Fatalf("captureCalls = %d, want 1", fakeStore.captureCalls)
	}
})

var _ = It("ServiceStartWatcherSyncsUppercaseMarkdownEvents", func() {
	t := GinkgoT()
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
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	DeferCleanup(cancel)

	if err := service.StartWatcher(ctx); err != nil {
		t.Fatalf("StartWatcher: %v", err)
	}
	fakeWatcher.events <- watcherEvent{Path: "/workspace/Page.MD"}

	waitUntil(t, func() bool { return fakeTree.reconstructCount() == 1 })
	if fakeStore.captureCalls != 1 {
		t.Fatalf("captureCalls = %d, want uppercase Markdown event to trigger sync", fakeStore.captureCalls)
	}
})

var _ = It("ServiceStartWatcherCoalescesDuplicateMarkdownEvents", func() {
	t := GinkgoT()
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
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	DeferCleanup(cancel)

	if err := service.StartWatcher(ctx); err != nil {
		t.Fatalf("StartWatcher: %v", err)
	}
	fakeWatcher.events <- watcherEvent{Path: "/workspace/docs/a.md"}
	fakeWatcher.events <- watcherEvent{Path: "/workspace/docs/a.md"}

	waitUntil(t, func() bool { return fakeTree.reconstructCount() == 1 })
	Consistently(func() int { return fakeStore.captureCalls }).WithTimeout(350 * time.Millisecond).Should(Equal(1))
})

var _ = It("ServiceStartWatcherDroppedEventRecordsStatusAndSyncs", func() {
	t := GinkgoT()
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
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	DeferCleanup(cancel)

	if err := service.StartWatcher(ctx); err != nil {
		t.Fatalf("StartWatcher: %v", err)
	}
	fakeWatcher.dropped <- watcherEvent{Path: "/workspace/docs/a.md", Dropped: true}

	waitUntil(t, func() bool { return fakeTree.reconstructCount() == 1 })
	status := service.Status()
	if !strings.Contains(status.LastError, "watcher dropped events") {
		t.Fatalf("LastError = %q, want dropped event status", status.LastError)
	}
	if fakeStore.captureCalls != 1 {
		t.Fatalf("captureCalls = %d, want 1", fakeStore.captureCalls)
	}
})

var _ = It("ServiceStartWatcherErrorRecordsStatusAndSyncs", func() {
	t := GinkgoT()
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
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	DeferCleanup(cancel)

	if err := service.StartWatcher(ctx); err != nil {
		t.Fatalf("StartWatcher: %v", err)
	}

	waitUntil(t, func() bool { return fakeTree.reconstructCount() == 1 })
	status := service.Status()
	if !strings.Contains(status.LastError, "watcher platform error") {
		t.Fatalf("LastError = %q, want watcher platform error", status.LastError)
	}
	if fakeStore.captureCalls != 1 {
		t.Fatalf("captureCalls = %d, want 1", fakeStore.captureCalls)
	}
})

var _ = It("ServiceStartWatcherIgnoresTemporaryFiles", func() {
	t := GinkgoT()
	fakeTree := &fakeTreeReconstructor{}
	fakeStore := &fakeRevisionStore{capture: &gitrevisions.Commit{Hash: "temp-commit"}}
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
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	DeferCleanup(cancel)

	if err := service.StartWatcher(ctx); err != nil {
		t.Fatalf("StartWatcher: %v", err)
	}
	fakeWatcher.events <- watcherEvent{Path: "/workspace/page.md.swp"}
	fakeWatcher.events <- watcherEvent{Path: "/workspace/.DS_Store"}

	Consistently(func() int { return fakeStore.captureCalls }).WithTimeout(50 * time.Millisecond).Should(Equal(0))
})

var _ = It("ServiceStopWatcherClosesUnderlyingWatcher", func() {
	t := GinkgoT()
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
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	if err := service.StartWatcher(context.Background()); err != nil {
		t.Fatalf("StartWatcher: %v", err)
	}

	service.StopWatcher()

	if got := fakeWatcher.closeCount(); got != 1 {
		t.Fatalf("watcher Close calls = %d, want 1", got)
	}
})

var _ = It("normalizes workspace markdown path helpers", func() {
	Expect(cleanWorkspaceMarkdownPath(" /docs/section/index.md ")).To(Equal("docs/section/index.md"))
	Expect(cleanWorkspaceMarkdownPath("./docs/page.md")).To(Equal("./docs/page.md"))
	Expect(joinWorkspaceMarkdownPath("", "/docs/", " section ", "index.md")).To(Equal("docs/section/index.md"))
	Expect(joinWorkspaceMarkdownPath("docs", "", "/page.md/")).To(Equal("docs/page.md"))
})

var _ = It("currentSectionContentPath prefers index markdown before README fallback", func() {
	t := GinkgoT()
	rootDir := t.TempDir()
	service := &Service{rootDir: rootDir}
	Expect(os.MkdirAll(filepath.Join(rootDir, "docs"), 0o755)).To(Succeed())
	writeMarkdown(t, filepath.Join(rootDir, "docs", "README.md"), "# Readme\n")

	Expect(service.currentSectionContentPath("docs", "docs.md")).To(Equal("docs/README.md"))

	writeMarkdown(t, filepath.Join(rootDir, "docs", "Index.MD"), "# Index\n")
	Expect(service.currentSectionContentPath("docs", "docs.md")).To(Equal("docs/Index.MD"))
	Expect(service.currentSectionContentPath("missing", "fallback.md")).To(Equal("fallback.md"))
})

var _ = It("normalizes validation paths and rejects workspace escapes", func() {
	t := GinkgoT()
	rootDir := filepath.Join(t.TempDir(), "workspace")
	Expect(normalizeValidationPath(rootDir, filepath.Join(rootDir, "docs", "page.md"))).To(Equal("docs/page.md"))
	Expect(normalizeValidationPath(rootDir, "./docs/page.md")).To(Equal("docs/page.md"))
	Expect(normalizeValidationPath(rootDir, "../outside.md")).To(BeEmpty())
	Expect(normalizeValidationPath(rootDir, filepath.Join(filepath.Dir(rootDir), "outside.md"))).To(Equal(filepath.ToSlash(filepath.Join(filepath.Dir(rootDir), "outside.md"))))
})

var _ = It("firstNonEmpty trims values and returns the first non-blank value", func() {
	Expect(firstNonEmpty("", " \t ", " value ", "later")).To(Equal("value"))
	Expect(firstNonEmpty("", " ")).To(BeEmpty())
})

type testHelper interface {
	Helper()
	Fatalf(format string, args ...any)
}

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

func writeMarkdown(t testHelper, path string, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("create parent for %s: %v", path, err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func readFileString(t testHelper, path string) string {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(raw)
}

func mustGetPage(t testHelper, treeService *tree.TreeService, id tree.PageID) *tree.Page {
	t.Helper()
	page, err := treeService.GetPage(id)
	if err != nil {
		t.Fatalf("GetPage %s: %v", id, err)
	}
	return page
}

func mustGetOnlyPage(t testHelper, treeService *tree.TreeService) *tree.Page {
	t.Helper()
	var ids []tree.PageID
	if err := treeService.WalkNodes(func(id tree.PageID) error {
		ids = append(ids, id)
		return nil
	}); err != nil {
		t.Fatalf("WalkNodes: %v", err)
	}
	if len(ids) != 1 {
		t.Fatalf("page ids = %#v, want one page", ids)
	}
	return mustGetPage(t, treeService, ids[0])
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

func waitUntil(t testHelper, condition func() bool) {
	t.Helper()
	GinkgoHelper()
	Eventually(condition).WithTimeout(2 * time.Second).Should(BeTrue())
}

func closeOnce(ch chan struct{}) func() {
	var once sync.Once
	return func() {
		once.Do(func() {
			close(ch)
		})
	}
}
