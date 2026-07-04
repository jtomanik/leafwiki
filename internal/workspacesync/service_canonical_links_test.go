package workspacesync

import (
	"context"
	"os"
	"path/filepath"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/perber/wiki/internal/core/markdown"
	"github.com/perber/wiki/internal/core/tree"
	"github.com/perber/wiki/internal/links"
)

var _ = Describe("canonical markdown link migration", Label("integration"), func() {
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
