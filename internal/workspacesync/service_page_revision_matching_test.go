package workspacesync

import (
	"context"
	"path/filepath"
	"strconv"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/perber/wiki/internal/core/revision"
	"github.com/perber/wiki/internal/core/tree"
	"github.com/perber/wiki/internal/workspacesync/gitrevisions"
)

var _ = Describe("page revision historical matching", Label("integration"), func() {
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
