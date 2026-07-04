package workspacesync

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/perber/wiki/internal/core/tree"
	"github.com/perber/wiki/internal/workspacesync/gitrevisions"
)

var _ = Describe("page revision listing", Label("integration"), func() {
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
