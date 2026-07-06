package workspacesync

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/gstruct"

	"github.com/sgtdi/fswatcher"
)

var _ = Describe("workspace sync watcher and startup failure handling", Label("unit"), func() {
	var ctx context.Context

	BeforeEach(func() {
		ctx = context.Background()
	})

	It("records watcher sync failures after the watcher itself fails", func() {
		store := &fakeRevisionStore{
			capture:    workspaceSyncServiceCommit(newFixtureCommitHash("unused")),
			captureErr: errors.New("sync after watcher failed"),
		}
		watcher := newFakeWatcher()
		watcher.watchErr = errors.New("watcher failed")
		service := workspaceSyncServiceHarness(store, &fakeTreeReconstructor{})
		service.watcherFactory = func(string) (fileWatcher, error) { return watcher, nil }

		Expect(service.StartWatcher(ctx)).To(Succeed())

		Eventually(func() SyncStatus { return service.Status() }).Should(matchStoppedWatcherStatus())
		Eventually(func() int { return store.captureCalls }).Should(Equal(1))
	})

	It("flushes queued watcher events when event channels close", func() {
		store := &fakeRevisionStore{
			capture: workspaceSyncServiceCommit(newFixtureCommitHash("closed-event"), "docs/a.md"),
		}
		service := workspaceSyncServiceHarness(store, &fakeTreeReconstructor{})
		watcher := newFakeWatcher()
		watcher.events <- watcherEvent{Path: "/workspace/docs/a.md"}
		close(watcher.events)

		service.consumeWatcherEvents(ctx, watcher)

		Expect(store.captureCalls).To(Equal(1))
		Expect(service.Status()).To(gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
			"PendingEventCount":          BeZero(),
			"RecentChangedMarkdownPaths": Equal([]string{"docs/a.md"}),
		}))
	})

	It("logs startup phase failures when startup logging is enabled", func() {
		records := &workspaceSyncStartupLogRecords{}
		service := &Service{log: slog.New(records), rootDir: "/workspace"}
		started := time.Now().Add(-time.Second)

		service.logStartupSyncFailed(true, started, errors.New("startup failed"))
		service.logStartupPhaseFailed(true, "phase", started, errors.New("phase failed"))

		Expect(records.Records).To(reportWorkspaceSyncStartupLogEvents(
			workspaceSyncStartupFailed,
			workspaceSyncStartupPhaseFailed,
		))
	})

	It("reports startup capture, reconstruction, migration, writeback, and after-sync failures", func() {
		records := &workspaceSyncStartupLogRecords{}
		logger := slog.New(records)

		captureErr := errors.New("capture failed")
		store := &fakeRevisionStore{
			capture:    workspaceSyncServiceCommit(newFixtureCommitHash("capture-failed")),
			captureErr: captureErr,
		}
		service := workspaceSyncServiceHarness(store, &fakeTreeReconstructor{})
		service.log = logger
		_, err := service.SyncNow(ctx, SyncRequest{Reason: ReasonStartup, Source: SourceFilesystem, Actor: PublicEditorActor()})
		Expect(err).To(MatchError(captureErr))

		store = &fakeRevisionStore{capture: workspaceSyncServiceCommit(newFixtureCommitHash("reconstruct-failed"), "docs/a.md")}
		reconstructErr := errors.New("reconstruct failed")
		treeService := &fakeTreeReconstructor{err: reconstructErr}
		service = workspaceSyncServiceHarness(store, treeService)
		service.log = logger
		status, err := service.SyncNow(ctx, SyncRequest{Reason: ReasonStartup, Source: SourceFilesystem, Actor: PublicEditorActor()})
		Expect(err).To(Succeed())
		Expect(status).To(gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
			"ValidationErrors": Not(BeEmpty()),
		}))

		rootDir := workspaceSyncTempDir()
		Expect(os.MkdirAll(filepath.Join(rootDir, "docs"), 0o755)).To(Succeed())
		Expect(os.WriteFile(filepath.Join(rootDir, "docs", "a.md"), []byte("[B](/docs/b)\n"), 0o644)).To(Succeed())
		Expect(os.WriteFile(filepath.Join(rootDir, "docs", "b.md"), []byte("# B\n"), 0o644)).To(Succeed())
		store = &fakeRevisionStore{capture: workspaceSyncServiceCommit(newFixtureCommitHash("migration-failed"))}
		service = workspaceSyncServiceHarness(store, &fakeTreeReconstructor{})
		service.rootDir = rootDir
		service.markdownLinkRootPrefix = "/"
		service.log = logger
		previousWriter := canonicalMarkdownRewriteWriter
		DeferCleanup(func() { canonicalMarkdownRewriteWriter = previousWriter })
		rewriteErr := errors.New("rewrite failed")
		canonicalMarkdownRewriteWriter = func([]canonicalMarkdownRewrite) error {
			return rewriteErr
		}
		_, err = service.SyncNow(ctx, SyncRequest{Reason: ReasonStartup, Source: SourceFilesystem, Actor: PublicEditorActor()})
		Expect(err).To(MatchError(rewriteErr))

		canonicalMarkdownRewriteWriter = previousWriter
		amendErr := errors.New("amend failed")
		store = &fakeRevisionStore{
			capture:  workspaceSyncServiceCommit(newFixtureCommitHash("writeback-failed")),
			amendErr: amendErr,
		}
		service = workspaceSyncServiceHarness(store, &fakeTreeReconstructor{})
		service.log = logger
		_, err = service.SyncNow(ctx, SyncRequest{Reason: ReasonStartup, Source: SourceFilesystem, Actor: PublicEditorActor()})
		Expect(err).To(MatchError(amendErr))

		store = &fakeRevisionStore{capture: workspaceSyncServiceCommit(newFixtureCommitHash("after-sync-failed"))}
		service = workspaceSyncServiceHarness(store, &fakeTreeReconstructor{})
		service.log = logger
		afterSyncErr := errors.New("after sync failed")
		service.SetAfterSync(func() error { return afterSyncErr })
		_, err = service.SyncNow(ctx, SyncRequest{Reason: ReasonStartup, Source: SourceFilesystem, Actor: PublicEditorActor()})
		Expect(err).To(MatchError(afterSyncErr))

		Expect(records.Records).To(reportWorkspaceSyncStartupLogEvents(workspaceSyncStartupFailed))
	})

	It("starts the default watcher and drains closed watcher channels", func() {
		preserveWorkspacesyncServiceSeams()
		wrapped := newFakeFSWatcher()
		workspacesyncNewFSWatcher = func(...fswatcher.WatcherOpt) (fswatcher.Watcher, error) {
			return wrapped, nil
		}
		service := workspaceSyncServiceHarness(
			&fakeRevisionStore{capture: workspaceSyncServiceCommit(newFixtureCommitHash("default-watcher"))},
			&fakeTreeReconstructor{},
		)

		Expect(service.StartWatcher(ctx)).To(Succeed())
		Eventually(func() SyncStatus { return service.Status() }).Should(matchRunningWatcherStatus())
		service.StopWatcher()
		Expect(wrapped.closeCount).To(Equal(1))

		watcher := newFakeWatcher()
		close(watcher.events)
		service.consumeWatcherEvents(ctx, watcher)
		Expect(service.Status().PendingEventCount).To(BeZero())

		watcher = newFakeWatcher()
		close(watcher.dropped)
		service.consumeWatcherEvents(ctx, watcher)
		Expect(service.Status().PendingEventCount).To(BeZero())

		adapter := &fsWatcherAdapter{}
		from := make(chan fswatcher.WatchEvent)
		to := make(chan watcherEvent)
		pumpCtx, cancelPump := context.WithCancel(ctx)
		var wg sync.WaitGroup
		wg.Add(1)
		go adapter.pump(pumpCtx, &wg, from, to, false)
		sent := make(chan struct{})
		go func() {
			from <- fswatcher.WatchEvent{Path: "/workspace/blocked.md"}
			close(sent)
		}()
		Eventually(sent).Should(BeClosed())
		cancelPump()
		pumpDone := make(chan struct{})
		go func() {
			defer close(pumpDone)
			wg.Wait()
		}()
		Eventually(pumpDone).Should(BeClosed())
	})

})
