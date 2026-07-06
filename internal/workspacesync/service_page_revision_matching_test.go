package workspacesync

import (
	"context"
	"path/filepath"
	"strconv"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/perber/wiki/internal/core/tree"
	"github.com/perber/wiki/internal/workspacesync/gitrevisions"
)

var _ = Describe("page revision historical matching", Label("integration"), func() {
	It("uses historical markdown metadata when current page metadata has changed", func() {
		page := &tree.Page{PageNode: &tree.PageNode{
			ID:    newFixturePageID("page-1"),
			Title: "Current Title",
			Slug:  newFixtureSlug("current-page"),
			Kind:  tree.NodeKindPage,
		}}
		store := &fakeRevisionStore{
			commits: []gitrevisions.Commit{{Hash: newFixtureCommitHash("old-commit"), AuthorID: newFixtureActorID("alice")}},
			filesAt: map[CommitHash]map[string]string{newFixtureCommitHash("old-commit"): {
				"old-page.md": "---\nleafwiki_id: page-1\nleafwiki_title: Historical Title\n---\n# Historical Heading\n",
			},
			},
			changedPaths: map[CommitHash][]string{newFixtureCommitHash("old-commit"): {"old-page.md"}},
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
			HaveField("Slug", Equal(newFixtureSlug("old-page"))),
			HaveField("Kind", Equal(tree.NodeKindPage)),
			HaveField("Path", Equal("old-page")),
		)))
	})

	It("normalizes section index markdown paths to the section route", func() {
		page := &tree.Page{PageNode: &tree.PageNode{
			ID:    newFixturePageID("section-1"),
			Title: "Docs",
			Slug:  newFixtureSlug("docs"),
			Kind:  tree.NodeKindSection,
		}}
		store := &fakeRevisionStore{
			commits: []gitrevisions.Commit{{Hash: newFixtureCommitHash("section-commit"), AuthorID: newFixtureActorID("alice")}},
			filesAt: map[CommitHash]map[string]string{newFixtureCommitHash("section-commit"): {
				"docs/index.md": "---\nleafwiki_id: section-1\nleafwiki_title: Historical Docs\n---\n# Historical Docs\n",
			},
			},
			changedPaths: map[CommitHash][]string{newFixtureCommitHash("section-commit"): {"docs/index.md"}},
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
			HaveField("Slug", Equal(newFixtureSlug("docs"))),
			HaveField("Kind", Equal(tree.NodeKindSection)),
			HaveField("Path", Equal("docs")),
		)))
	})

	It("normalizes README markdown fallbacks to section routes", func() {
		page := &tree.Page{PageNode: &tree.PageNode{
			ID:    newFixturePageID("section-guides"),
			Title: "Guides",
			Slug:  newFixtureSlug("guides"),
			Kind:  tree.NodeKindSection,
		}}
		store := &fakeRevisionStore{
			commits: []gitrevisions.Commit{{Hash: newFixtureCommitHash("readme-section-commit"), AuthorID: newFixtureActorID("alice")}},
			filesAt: map[CommitHash]map[string]string{newFixtureCommitHash("readme-section-commit"): {
				"guides/README.md": "---\nleafwiki_id: section-guides\nleafwiki_title: Historical Guides\n---\n# Historical Guides\n",
			},
			},
			changedPaths: map[CommitHash][]string{newFixtureCommitHash("readme-section-commit"): {"guides/README.md"}},
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
			HaveField("Slug", Equal(newFixtureSlug("guides"))),
			HaveField("Kind", Equal(tree.NodeKindSection)),
			HaveField("Path", Equal("guides")),
		)))
	})

	It("maps README history as a page when the workspace directory has an index", func() {
		rootDir := workspaceSyncTempDir()
		writeMarkdownFile(filepath.Join(rootDir, "User Guides", "index.md"), "# User Guides\n")
		writeMarkdownFile(filepath.Join(rootDir, "User Guides", "README.md"), "---\nleafwiki_id: readme-page\nleafwiki_title: Historical README\n---\n# Historical README\n")

		page := &tree.Page{PageNode: &tree.PageNode{
			ID:    newFixturePageID("readme-page"),
			Title: "README",
			Slug:  newFixtureSlug("readme"),
			Kind:  tree.NodeKindPage,
			Parent: &tree.PageNode{
				ID:    newFixturePageID("user-guides"),
				Slug:  newFixtureSlug("user-guides"),
				Title: "User Guides",
				Kind:  tree.NodeKindSection,
			},
		}}
		store := &fakeRevisionStore{
			commits: []gitrevisions.Commit{{Hash: newFixtureCommitHash("readme-page-commit"), AuthorID: newFixtureActorID("alice")}},
			filesAt: map[CommitHash]map[string]string{newFixtureCommitHash("readme-page-commit"): {
				"User Guides/README.md": "---\nleafwiki_id: readme-page\nleafwiki_title: Historical README\n---\n# Historical README\n",
			},
			},
			changedPaths: map[CommitHash][]string{newFixtureCommitHash("readme-page-commit"): {"User Guides/README.md"}},
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
			HaveField("Slug", Equal(newFixtureSlug("README"))),
		)))
	})

	It("keeps the historical page kind after the current node becomes a section", func() {
		page := &tree.Page{PageNode: &tree.PageNode{
			ID:    newFixturePageID("docs-1"),
			Title: "Docs",
			Slug:  newFixtureSlug("docs"),
			Kind:  tree.NodeKindSection,
		}}
		store := &fakeRevisionStore{
			commits: []gitrevisions.Commit{{Hash: newFixtureCommitHash("page-commit"), AuthorID: newFixtureActorID("alice")}},
			filesAt: map[CommitHash]map[string]string{newFixtureCommitHash("page-commit"): {
				"docs.md": "---\nleafwiki_id: docs-1\nleafwiki_title: Docs Page\n---\n# Docs Page\n",
			},
			},
			changedPaths: map[CommitHash][]string{newFixtureCommitHash("page-commit"): {"docs.md"}},
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
			ID:    newFixturePageID("page-a"),
			Title: "Page A",
			Slug:  newFixtureSlug("page-a"),
			Kind:  tree.NodeKindPage,
		}}
		store := &fakeRevisionStore{
			commits: []gitrevisions.Commit{
				{Hash: newFixtureCommitHash("page-b-change"), AuthorID: newFixtureActorID("bob")},
				{Hash: newFixtureCommitHash("page-a-change"), AuthorID: newFixtureActorID("alice")},
			},
			filesAt: map[CommitHash]map[string]string{newFixtureCommitHash("page-b-change"): {
				"page-a.md": "---\nleafwiki_id: page-a\nleafwiki_title: Page A\n---\n# Page A\n",
				"page-b.md": "---\nleafwiki_id: page-b\nleafwiki_title: Page B\n---\n# Page B changed\n",
			}, newFixtureCommitHash("page-a-change"): {
				"page-a.md": "---\nleafwiki_id: page-a\nleafwiki_title: Page A\n---\n# Page A changed\n",
			},
			},
			changedPaths: map[CommitHash][]string{newFixtureCommitHash("page-b-change"): {"page-b.md"}, newFixtureCommitHash("page-a-change"): {"page-a.md"}},
		}
		service, err := NewService(ServiceOptions{
			Enabled: true,
			Tree:    &fakeTreeReconstructor{},
			Store:   store,
		})
		Expect(err).To(Succeed())

		result, err := service.ListPageRevisions(context.Background(), page, "", 10)
		Expect(err).To(Succeed())

		Expect(result.Revisions).To(ConsistOf(HaveField("ID", Equal(newFixtureRevisionID("page-a-change")))))
	})

	It("scans past an unrelated head commit when the limit is one", func() {
		page := &tree.Page{PageNode: &tree.PageNode{
			ID:    newFixturePageID("page-a"),
			Title: "Page A",
			Slug:  newFixtureSlug("page-a"),
			Kind:  tree.NodeKindPage,
		}}
		store := &fakeRevisionStore{
			commits: []gitrevisions.Commit{
				{Hash: newFixtureCommitHash("page-b-change"), AuthorID: newFixtureActorID("bob")},
				{Hash: newFixtureCommitHash("page-a-change"), AuthorID: newFixtureActorID("alice")},
			},
			filesAt: map[CommitHash]map[string]string{newFixtureCommitHash("page-b-change"): {
				"page-a.md": "---\nleafwiki_id: page-a\nleafwiki_title: Page A\n---\n# Page A\n",
				"page-b.md": "---\nleafwiki_id: page-b\nleafwiki_title: Page B\n---\n# Page B changed\n",
			}, newFixtureCommitHash("page-a-change"): {
				"page-a.md": "---\nleafwiki_id: page-a\nleafwiki_title: Page A\n---\n# Page A changed\n",
			},
			},
			changedPaths: map[CommitHash][]string{newFixtureCommitHash("page-b-change"): {"page-b.md"}, newFixtureCommitHash("page-a-change"): {"page-a.md"}},
		}
		service, err := NewService(ServiceOptions{
			Enabled: true,
			Tree:    &fakeTreeReconstructor{},
			Store:   store,
		})
		Expect(err).To(Succeed())

		result, err := service.ListPageRevisions(context.Background(), page, "", 1)
		Expect(err).To(Succeed())

		Expect(result.Revisions).To(ConsistOf(HaveField("ID", Equal(newFixtureRevisionID("page-a-change")))))
	})

	It("scans a large unrelated commit prefix to find the requested page", func() {
		page := &tree.Page{PageNode: &tree.PageNode{
			ID:    newFixturePageID("page-a"),
			Title: "Page A",
			Slug:  newFixtureSlug("page-a"),
			Kind:  tree.NodeKindPage,
		}}
		store := &fakeRevisionStore{
			filesAt:      map[CommitHash]map[string]string{},
			changedPaths: map[CommitHash][]string{},
		}
		for i := 0; i < 1005; i++ {
			hash := "page-b-change-" + strconv.Itoa(i)
			store.commits = append(store.commits, gitrevisions.Commit{Hash: CommitHashFromString(hash), AuthorID: newFixtureActorID("bob")})
			store.filesAt[CommitHashFromString(hash)] = map[string]string{
				"page-a.md": "---\nleafwiki_id: page-a\nleafwiki_title: Page A\n---\n# Page A\n",
				"page-b.md": "---\nleafwiki_id: page-b\nleafwiki_title: Page B\n---\n# Page B changed\n",
			}
			store.changedPaths[CommitHashFromString(hash)] = []string{"page-b.md"}
		}
		store.commits = append(store.commits, gitrevisions.Commit{Hash: newFixtureCommitHash("page-a-change"), AuthorID: newFixtureActorID("alice")})
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

		Expect(result.Revisions).To(ConsistOf(HaveField("ID", Equal(newFixtureRevisionID("page-a-change")))))
	})

	It("stops scanning after it has a page revision and confirmed next cursor", func() {
		page := &tree.Page{PageNode: &tree.PageNode{
			ID:    newFixturePageID("page-a"),
			Title: "Page A",
			Slug:  newFixtureSlug("page-a"),
			Kind:  tree.NodeKindPage,
		}}
		store := &fakeRevisionStore{
			commits: []gitrevisions.Commit{
				{Hash: newFixtureCommitHash("page-a-change-2"), AuthorID: newFixtureActorID("alice")},
				{Hash: newFixtureCommitHash("page-a-change-1"), AuthorID: newFixtureActorID("alice")},
			},
			filesAt: map[CommitHash]map[string]string{newFixtureCommitHash("page-a-change-2"): {
				"page-a.md": "---\nleafwiki_id: page-a\nleafwiki_title: Page A\n---\n# Page A changed again\n",
			}, newFixtureCommitHash("page-a-change-1"): {
				"page-a.md": "---\nleafwiki_id: page-a\nleafwiki_title: Page A\n---\n# Page A changed\n",
			},
			},
			changedPaths: map[CommitHash][]string{newFixtureCommitHash("page-a-change-2"): {"page-a.md"}, newFixtureCommitHash("page-a-change-1"): {"page-a.md"}},
		}
		for i := 0; i < 1005; i++ {
			hash := "page-b-change-" + strconv.Itoa(i)
			store.commits = append(store.commits, gitrevisions.Commit{Hash: CommitHashFromString(hash), AuthorID: newFixtureActorID("bob")})
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
			HaveField("Revisions", ConsistOf(HaveField("ID", Equal(newFixtureRevisionID("page-a-change-2"))))),
			HaveField("NextCursor", Equal("page-a-change-2")),
		))
		Expect(store.scannedCommits).To(Equal(2))
	})
})
