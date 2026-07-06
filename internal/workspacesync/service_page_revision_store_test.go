package workspacesync

import (
	"context"
	"errors"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/perber/wiki/internal/core/tree"
	"github.com/perber/wiki/internal/workspacesync/gitrevisions"
)

var _ = Describe("page revision store access", Label("integration"), func() {
	It("scans document history without loading full trees", func() {
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
		Expect(store.filesAtCalls).To(BeZero())
	})

	It("serves status while revision listing waits on the store", func() {
		page := &tree.Page{PageNode: &tree.PageNode{
			ID:    newFixturePageID("page-a"),
			Title: "Page A",
			Slug:  newFixtureSlug("page-a"),
			Kind:  tree.NodeKindPage,
		}}
		store := &fakeRevisionStore{
			commits: []gitrevisions.Commit{{Hash: newFixtureCommitHash("page-a-change"), AuthorID: newFixtureActorID("alice")}},
			filesAt: map[CommitHash]map[string]string{newFixtureCommitHash("page-a-change"): {
				"page-a.md": "---\nleafwiki_id: page-a\nleafwiki_title: Page A\n---\n# Page A changed\n",
			},
			},
			changedPaths:           map[CommitHash][]string{newFixtureCommitHash("page-a-change"): {"page-a.md"}},
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
			ID:    newFixturePageID("page-a"),
			Title: "Page A",
			Slug:  newFixtureSlug("page-a"),
			Kind:  tree.NodeKindPage,
		}}
		store := &fakeRevisionStore{
			commits: []gitrevisions.Commit{{Hash: newFixtureCommitHash("page-b-change")}},
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

		_, err = service.GetPageRevisionSnapshot(context.Background(), page, newFixtureCommitHash("page-b-change"))
		Expect(err).To(Satisfy(func(err error) bool {
			return errors.Is(err, ErrWorkspaceSyncDocumentUnchanged)
		}))
	})

	It("serves status while snapshot loading waits on the store", func() {
		page := &tree.Page{PageNode: &tree.PageNode{
			ID:    newFixturePageID("page-a"),
			Title: "Page A",
			Slug:  newFixtureSlug("page-a"),
			Kind:  tree.NodeKindPage,
		}}
		store := &fakeRevisionStore{
			commits: []gitrevisions.Commit{{Hash: newFixtureCommitHash("page-a-change"), AuthorID: newFixtureActorID("alice")}},
			filesAt: map[CommitHash]map[string]string{newFixtureCommitHash("page-a-change"): {
				"page-a.md": "---\nleafwiki_id: page-a\nleafwiki_title: Page A\n---\n# Page A changed\n",
			},
			},
			changedPaths:           map[CommitHash][]string{newFixtureCommitHash("page-a-change"): {"page-a.md"}},
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
			_, err := service.GetPageRevisionSnapshot(context.Background(), page, newFixtureCommitHash("page-a-change"))
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
