package workspacesync

import (
	"context"
	"errors"
	"os"
	"path/filepath"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/perber/wiki/internal/core/tree"
	"github.com/perber/wiki/internal/workspacesync/gitrevisions"
)

var _ = Describe("document restore from workspace revisions", Label("integration"), func() {
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
		page := &tree.Page{PageNode: &tree.PageNode{ID: newFixturePageID("page-1"), Title: "New Page", Slug: newFixtureSlug("new-page"), Kind: tree.NodeKindPage}}
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
		page := &tree.Page{PageNode: &tree.PageNode{ID: newFixturePageID("page-1"), Title: "Page", Slug: newFixtureSlug("Page"), Kind: tree.NodeKindPage}}
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
		section := &tree.Page{PageNode: &tree.PageNode{ID: newFixturePageID("section-1"), Title: "Docs", Slug: newFixtureSlug("docs"), Kind: tree.NodeKindSection}}
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
		section := &tree.Page{PageNode: &tree.PageNode{ID: newFixturePageID("section-1"), Title: "Docs", Slug: newFixtureSlug("docs"), Kind: tree.NodeKindSection}}
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
			ID:    newFixturePageID("page-a"),
			Title: "Page A",
			Slug:  newFixtureSlug("page-a"),
			Kind:  tree.NodeKindPage,
		}}
		store := &fakeRevisionStore{
			capture: &gitrevisions.Commit{Hash: newFixtureCommitHash("restore-commit")},
			filesAt: map[CommitHash]map[string]string{newFixtureCommitHash("page-b-change"): {
				"page-a.md": "---\nleafwiki_id: page-a\nleafwiki_title: Page A\n---\n# Page A\n",
				"page-b.md": "---\nleafwiki_id: page-b\nleafwiki_title: Page B\n---\n# Page B changed\n",
			},
			},
			changedPaths: map[CommitHash][]string{newFixtureCommitHash("page-b-change"): {"page-b.md"}},
		}
		service, err := NewService(ServiceOptions{
			Enabled: true,
			Tree:    &fakeTreeReconstructor{},
			Store:   store,
		})
		Expect(err).To(Succeed())

		_, err = service.RestoreDocument(context.Background(), page, newFixtureCommitHash("page-b-change"), PublicEditorActor())
		Expect(err).To(Satisfy(func(err error) bool {
			return errors.Is(err, ErrWorkspaceSyncDocumentUnchanged)
		}))
		Expect(store.restoreDocumentToPathCalls).To(BeZero())
	})

	It("uses changed content without loading full trees", func() {
		page := &tree.Page{PageNode: &tree.PageNode{
			ID:    newFixturePageID("page-a"),
			Title: "Page A",
			Slug:  newFixtureSlug("page-a"),
			Kind:  tree.NodeKindPage,
		}}
		store := &fakeRevisionStore{
			capture: &gitrevisions.Commit{Hash: newFixtureCommitHash("restore-commit")},
			filesAt: map[CommitHash]map[string]string{newFixtureCommitHash("page-a-change"): {
				"page-a.md": "---\nleafwiki_id: page-a\nleafwiki_title: Page A\n---\n# Page A restored\n",
			},
			},
			changedPaths: map[CommitHash][]string{newFixtureCommitHash("page-a-change"): {"page-a.md"}},
		}
		service, err := NewService(ServiceOptions{
			Enabled: true,
			Tree:    &fakeTreeReconstructor{},
			Store:   store,
		})
		Expect(err).To(Succeed())

		_, err = service.RestoreDocument(context.Background(), page, newFixtureCommitHash("page-a-change"), PublicEditorActor())
		Expect(err).To(Succeed())

		Expect(fakeRevisionStoreRestoreContentStateFor(store)).To(SatisfyAll(
			HaveField("FilesAtCalls", BeZero()),
			HaveField("RestoreDocumentContentToPathCalls", Equal(1)),
			HaveField("RestoredContent", ContainSubstring("Page A restored")),
		))
	})
})

var _ = Describe("workspace restore revision capture and validation", Label("integration"), func() {
	It("returns reconstruction errors with path-specific validation state", func() {
		reconstructErr := errors.New(`duplicate leafwiki_id "page-a" in /workspace/page-a.md and /workspace/other.md`)
		page := &tree.Page{PageNode: &tree.PageNode{
			ID:    newFixturePageID("page-a"),
			Title: "Page A",
			Slug:  newFixtureSlug("page-a"),
			Kind:  tree.NodeKindPage,
		}}
		store := &fakeRevisionStore{
			capture: &gitrevisions.Commit{Hash: newFixtureCommitHash("restore-commit")},
			filesAt: map[CommitHash]map[string]string{newFixtureCommitHash("page-a-change"): {
				"page-a.md": "---\nleafwiki_id: page-a\nleafwiki_title: Page A\n---\n# Page A restored\n",
			},
			},
			changedPaths: map[CommitHash][]string{newFixtureCommitHash("page-a-change"): {"page-a.md"}},
		}
		service, err := NewService(ServiceOptions{
			Enabled: true,
			RootDir: "/workspace",
			Tree:    &fakeTreeReconstructor{err: reconstructErr},
			Store:   store,
		})
		Expect(err).To(Succeed())

		status, err := service.RestoreDocument(context.Background(), page, newFixtureCommitHash("page-a-change"), PublicEditorActor())
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
		page := &tree.Page{PageNode: &tree.PageNode{ID: newFixturePageID("page-1"), Title: "Needs Metadata", Slug: newFixtureSlug("needs-metadata"), Kind: tree.NodeKindPage}}
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
			ID:    newFixturePageID("page-a"),
			Title: "Page A",
			Slug:  newFixtureSlug("page"),
			Kind:  tree.NodeKindPage,
		}}
		store := &fakeRevisionStore{
			commits: []gitrevisions.Commit{{Hash: newFixtureCommitHash("path-reuse")}},
			filesAt: map[CommitHash]map[string]string{newFixtureCommitHash("path-reuse"): {
				"page.md": "---\nleafwiki_id: page-b\nleafwiki_title: Page B\n---\n# Page B\n",
			},
			},
			changedPaths: map[CommitHash][]string{newFixtureCommitHash("path-reuse"): {"page.md"}},
		}
		service, err := NewService(ServiceOptions{
			Enabled: true,
			Tree:    &fakeTreeReconstructor{},
			Store:   store,
		})
		Expect(err).To(Succeed())

		_, err = service.GetPageRevisionSnapshot(context.Background(), page, newFixtureCommitHash("path-reuse"))
		Expect(err).To(Satisfy(func(err error) bool {
			return errors.Is(err, ErrWorkspaceSyncDocumentUnchanged)
		}))
	})
})
