package workspacesync

import (
	"context"
	"errors"
	"path/filepath"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	wikivalidation "github.com/perber/wiki/internal/core/markdownvalidation"
	"github.com/perber/wiki/internal/core/tree"
	testmatchers "github.com/perber/wiki/internal/test_utils/matchers"
	"github.com/perber/wiki/internal/workspacesync/gitrevisions"
)

var _ = Describe("workspace sync writeback and validation", Label("integration"), func() {
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

var _ = Describe("workspace sync file watcher", Label("integration"), func() {
	It("syncs markdown events and reports the processed path", func() {
		fakeTree := &fakeTreeReconstructor{}
		fakeStore := &fakeRevisionStore{
			capture: &gitrevisions.Commit{
				Hash:                 newFixtureCommitHash("event-commit"),
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
				Hash:                 newFixtureCommitHash("event-commit"),
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
				Hash:                 newFixtureCommitHash("event-commit"),
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
		fakeStore := &fakeRevisionStore{capture: &gitrevisions.Commit{Hash: newFixtureCommitHash("drop-commit")}}
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
		Expect(service.Status()).To(matchWorkspaceSyncLastError(watcherDroppedEventsStatus("docs/a.md")))
		Expect(fakeStore.captureCalls).To(Equal(1))
	})

	It("records watcher errors and still syncs the workspace", func() {
		fakeTree := &fakeTreeReconstructor{}
		fakeStore := &fakeRevisionStore{capture: &gitrevisions.Commit{Hash: newFixtureCommitHash("error-commit")}}
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
		fakeStore := &fakeRevisionStore{capture: &gitrevisions.Commit{Hash: newFixtureCommitHash("temp-commit")}}
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
			Store:   &fakeRevisionStore{capture: &gitrevisions.Commit{Hash: newFixtureCommitHash("stop-commit")}},
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
