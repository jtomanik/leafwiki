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
			ID:    newFixturePageID("page-a"),
			Title: "Page A",
			Slug:  newFixtureSlug("page-a"),
			Kind:  tree.NodeKindPage,
		}}
		store := &fakeRevisionStore{
			commits: []gitrevisions.Commit{
				{Hash: newFixtureCommitHash("page-a-change-3"), AuthorID: newFixtureActorID("alice")},
				{Hash: newFixtureCommitHash("page-a-change-2"), AuthorID: newFixtureActorID("alice")},
				{Hash: newFixtureCommitHash("page-a-change-1"), AuthorID: newFixtureActorID("alice")},
			},
			filesAt: map[CommitHash]map[string]string{newFixtureCommitHash("page-a-change-3"): {
				"page-a.md": "---\nleafwiki_id: page-a\nleafwiki_title: Page A\n---\n# Page A v3\n",
			}, newFixtureCommitHash("page-a-change-2"): {
				"page-a.md": "---\nleafwiki_id: page-a\nleafwiki_title: Page A\n---\n# Page A v2\n",
			}, newFixtureCommitHash("page-a-change-1"): {
				"page-a.md": "---\nleafwiki_id: page-a\nleafwiki_title: Page A\n---\n# Page A v1\n",
			},
			},
			changedPaths: map[CommitHash][]string{newFixtureCommitHash("page-a-change-3"): {"page-a.md"}, newFixtureCommitHash("page-a-change-2"): {"page-a.md"}, newFixtureCommitHash("page-a-change-1"): {"page-a.md"}},
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
				"page-a.md": "---\nleafwiki_id: page-a\nleafwiki_title: Page A\n---\n# Page A v2\n",
			}, newFixtureCommitHash("page-a-change-1"): {
				"page-a.md": "---\nleafwiki_id: page-a\nleafwiki_title: Page A\n---\n# Page A v1\n",
			},
			},
			changedPaths: map[CommitHash][]string{newFixtureCommitHash("page-a-change-2"): {"page-a.md"}, newFixtureCommitHash("page-a-change-1"): {"page-a.md"}},
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
			ID:    newFixturePageID("page-1"),
			Title: "New Page",
			Slug:  newFixtureSlug("new-page"),
			Kind:  tree.NodeKindPage,
		}}
		store := &fakeRevisionStore{
			commits: []gitrevisions.Commit{
				{Hash: newFixtureCommitHash("new-commit"), AuthorID: newFixtureActorID("alice")},
				{Hash: newFixtureCommitHash("old-commit"), AuthorID: newFixtureActorID("alice")},
			},
			filesAt: map[CommitHash]map[string]string{newFixtureCommitHash("new-commit"): {
				"new-page.md": "---\nleafwiki_id: page-1\nleafwiki_title: New Page\n---\n# New Page\n",
			}, newFixtureCommitHash("old-commit"): {
				"old-page.md": "---\nleafwiki_id: page-1\nleafwiki_title: Old Page\n---\n# Old Page\n",
			},
			},
			changedPaths: map[CommitHash][]string{newFixtureCommitHash("new-commit"): {"new-page.md"}, newFixtureCommitHash("old-commit"): {"old-page.md"}},
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
			ID:    newFixturePageID("page-1"),
			Title: "Page",
			Slug:  newFixtureSlug("page"),
			Kind:  tree.NodeKindPage,
		}}
		store := &fakeRevisionStore{
			commits: []gitrevisions.Commit{{Hash: newFixtureCommitHash("uppercase-commit"), AuthorID: newFixtureActorID("alice")}},
			filesAt: map[CommitHash]map[string]string{newFixtureCommitHash("uppercase-commit"): {
				"Page.MD": "---\nleafwiki_id: page-1\nleafwiki_title: Page\n---\n# Page\n",
			},
			},
			changedPaths: map[CommitHash][]string{newFixtureCommitHash("uppercase-commit"): {"Page.MD"}},
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
			ID:    newFixturePageID("page-1"),
			Title: "Agent Hooks Plan",
			Slug:  newFixtureSlug("agent-hooks-plan"),
			Kind:  tree.NodeKindPage,
			Parent: &tree.PageNode{
				ID:    newFixturePageID("plans"),
				Slug:  newFixtureSlug("plans"),
				Title: "Plans",
				Kind:  tree.NodeKindSection,
			},
		}}
		store := &fakeRevisionStore{
			commits: []gitrevisions.Commit{{Hash: newFixtureCommitHash("raw-normalized-commit"), AuthorID: newFixtureActorID("alice")}},
			filesAt: map[CommitHash]map[string]string{newFixtureCommitHash("raw-normalized-commit"): {
				"plans/agent_hooks.PLAN.md": "# Agent Hooks Plan\n\nRaw content before writeback.\n",
			},
			},
			changedPaths: map[CommitHash][]string{newFixtureCommitHash("raw-normalized-commit"): {"plans/agent_hooks.PLAN.md"}},
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
