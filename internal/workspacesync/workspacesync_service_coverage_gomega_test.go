package workspacesync

import (
	"bytes"
	"context"
	"errors"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/perber/wiki/internal/core/markdownlinks"
	"github.com/perber/wiki/internal/core/tree"
	"github.com/perber/wiki/internal/workspacesync/gitrevisions"
	"github.com/sgtdi/fswatcher"
)

var _ = Describe("workspace sync service deterministic branch coverage", func() {
	var ctx context.Context

	BeforeEach(func() {
		ctx = context.Background()
	})

	It("records watcher sync failures after the watcher itself fails", func() {
		store := &fakeRevisionStore{
			capture:    workspaceSyncCoverageCommit("unused"),
			captureErr: errors.New("sync after watcher failed"),
		}
		watcher := newFakeWatcher()
		watcher.watchErr = errors.New("watcher failed")
		service := workspaceSyncCoverageService(store, &fakeTreeReconstructor{})
		service.watcherFactory = func(string) (fileWatcher, error) { return watcher, nil }

		Expect(service.StartWatcher(ctx)).To(Succeed())

		Eventually(func() SyncStatus { return service.Status() }).Should(SatisfyAll(
			WithTransform(func(status SyncStatus) bool { return status.WatcherRunning }, BeFalse()),
			WithTransform(func(status SyncStatus) string { return status.LastError }, Equal("watcher failed: sync after watcher failed")),
		))
	})

	It("flushes queued watcher events when event channels close", func() {
		store := &fakeRevisionStore{
			capture: workspaceSyncCoverageCommit("closed-event", "docs/a.md"),
		}
		service := workspaceSyncCoverageService(store, &fakeTreeReconstructor{})
		watcher := newFakeWatcher()
		watcher.events <- watcherEvent{Path: "/workspace/docs/a.md"}
		close(watcher.events)

		service.consumeWatcherEvents(ctx, watcher)

		Expect(store.captureCalls).To(Equal(1))
		Expect(service.Status().PendingEventCount).To(Equal(0))
		Expect(service.Status().RecentChangedMarkdownPaths).To(Equal([]string{"docs/a.md"}))
	})

	It("logs enabled startup failure branches", func() {
		var out bytes.Buffer
		service := &Service{log: slog.New(slog.NewTextHandler(&out, nil)), rootDir: "/workspace"}
		started := time.Now().Add(-time.Second)

		service.logStartupSyncFailed(true, started, errors.New("startup failed"))
		service.logStartupPhaseFailed(true, "phase", started, errors.New("phase failed"))

		Expect(out.String()).To(ContainSubstring("workspace sync startup failed"))
		Expect(out.String()).To(ContainSubstring("workspace sync startup phase failed"))
	})

	It("handles SyncNow startup capture, reconstruct, migration, writeback, and after-sync failures", func() {
		var logs bytes.Buffer
		logger := slog.New(slog.NewTextHandler(&logs, nil))

		store := &fakeRevisionStore{
			capture:    workspaceSyncCoverageCommit("capture-failed"),
			captureErr: errors.New("capture failed"),
		}
		service := workspaceSyncCoverageService(store, &fakeTreeReconstructor{})
		service.log = logger
		_, err := service.SyncNow(ctx, SyncRequest{Reason: ReasonStartup, Source: SourceFilesystem, Actor: PublicEditorActor()})
		Expect(err).To(MatchError("capture failed"))
		Expect(service.Status().LastError).To(Equal("capture failed"))

		store = &fakeRevisionStore{capture: workspaceSyncCoverageCommit("reconstruct-failed", "docs/a.md")}
		treeService := &fakeTreeReconstructor{err: errors.New("reconstruct failed")}
		service = workspaceSyncCoverageService(store, treeService)
		service.log = logger
		status, err := service.SyncNow(ctx, SyncRequest{Reason: ReasonStartup, Source: SourceFilesystem, Actor: PublicEditorActor()})
		Expect(err).NotTo(HaveOccurred())
		Expect(status.LastError).To(Equal("reconstruct failed"))
		Expect(status.ValidationErrors).NotTo(BeEmpty())

		rootDir := GinkgoT().TempDir()
		Expect(os.MkdirAll(filepath.Join(rootDir, "docs"), 0o755)).To(Succeed())
		Expect(os.WriteFile(filepath.Join(rootDir, "docs", "a.md"), []byte("[B](/docs/b)\n"), 0o644)).To(Succeed())
		Expect(os.WriteFile(filepath.Join(rootDir, "docs", "b.md"), []byte("# B\n"), 0o644)).To(Succeed())
		store = &fakeRevisionStore{capture: workspaceSyncCoverageCommit("migration-failed")}
		service = workspaceSyncCoverageService(store, &fakeTreeReconstructor{})
		service.rootDir = rootDir
		service.markdownLinkRootPrefix = "/"
		service.log = logger
		previousWriter := canonicalMarkdownRewriteWriter
		DeferCleanup(func() { canonicalMarkdownRewriteWriter = previousWriter })
		canonicalMarkdownRewriteWriter = func([]canonicalMarkdownRewrite) error {
			return errors.New("rewrite failed")
		}
		_, err = service.SyncNow(ctx, SyncRequest{Reason: ReasonStartup, Source: SourceFilesystem, Actor: PublicEditorActor()})
		Expect(err).To(MatchError("rewrite failed"))
		Expect(service.Status().LastError).To(Equal("rewrite failed"))

		canonicalMarkdownRewriteWriter = previousWriter
		store = &fakeRevisionStore{
			capture:  workspaceSyncCoverageCommit("writeback-failed"),
			amendErr: errors.New("amend failed"),
		}
		service = workspaceSyncCoverageService(store, &fakeTreeReconstructor{})
		service.log = logger
		_, err = service.SyncNow(ctx, SyncRequest{Reason: ReasonStartup, Source: SourceFilesystem, Actor: PublicEditorActor()})
		Expect(err).To(MatchError("amend failed"))
		Expect(service.Status().LastError).To(Equal("amend failed"))

		store = &fakeRevisionStore{capture: workspaceSyncCoverageCommit("after-sync-failed")}
		service = workspaceSyncCoverageService(store, &fakeTreeReconstructor{})
		service.log = logger
		service.SetAfterSync(func() error { return errors.New("after sync failed") })
		_, err = service.SyncNow(ctx, SyncRequest{Reason: ReasonStartup, Source: SourceFilesystem, Actor: PublicEditorActor()})
		Expect(err).To(MatchError("after sync failed"))
		Expect(service.Status().LastError).To(Equal("after sync failed"))

		Expect(logs.String()).To(ContainSubstring("workspace sync startup failed"))
	})

	It("rolls back canonical migration status for nil, failed, tree-less, reconstruct-failed, and successful rollbacks", func() {
		service := &Service{status: SyncStatus{LastError: "primary"}}
		service.rollbackCanonicalMarkdownMigrationLocked(nil)
		Expect(service.status.LastError).To(Equal("primary"))

		service.rollbackCanonicalMarkdownMigrationLocked(func() error { return errors.New("rollback failed") })
		Expect(service.status.LastError).To(ContainSubstring("canonical migration rollback failed"))

		service = &Service{status: SyncStatus{LastError: "primary"}}
		service.rollbackCanonicalMarkdownMigrationLocked(func() error { return nil })
		Expect(service.status.LastError).To(Equal("primary"))

		service = &Service{tree: &fakeTreeReconstructor{err: errors.New("reconstruct rollback failed")}, status: SyncStatus{LastError: "primary"}}
		service.rollbackCanonicalMarkdownMigrationLocked(func() error { return nil })
		Expect(service.status.LastError).To(ContainSubstring("reconstruct after canonical migration rollback failed"))
		Expect(service.status.ValidationErrors).NotTo(BeEmpty())

		service = &Service{tree: &fakeTreeReconstructor{}, status: SyncStatus{LastError: "primary", ValidationErrors: []ValidationError{{Path: "old"}}}}
		service.rollbackCanonicalMarkdownMigrationLocked(func() error { return nil })
		Expect(service.status.LastError).To(Equal("primary"))
		Expect(service.status.ValidationErrors).To(BeNil())
	})

	It("covers snapshot list disabled, error, paging, and changed-path failures", func() {
		disabled := &Service{}
		page, err := disabled.ListSnapshotPage(ctx, "", 0)
		Expect(err).NotTo(HaveOccurred())
		Expect(page.Snapshots).To(BeEmpty())

		store := &fakeRevisionStore{listErr: errors.New("list failed")}
		service := workspaceSyncCoverageService(store, &fakeTreeReconstructor{})
		_, err = service.ListSnapshots(ctx, 1)
		Expect(err).To(MatchError("list failed"))

		store = &fakeRevisionStore{
			commits: []gitrevisions.Commit{
				workspaceSyncCoverageCommitValue("c1", "a.md"),
				workspaceSyncCoverageCommitValue("c2", "b.md"),
			},
			changedPaths: map[CommitHash][]string{
				"c1": {"a.md"},
				"c2": {"b.md"},
			},
		}
		service = workspaceSyncCoverageService(store, &fakeTreeReconstructor{})
		list, err := service.ListSnapshotPage(ctx, "", 1)
		Expect(err).NotTo(HaveOccurred())
		Expect(list.Snapshots).To(HaveLen(1))
		Expect(list.NextCursor).To(Equal(CommitHashFromString("c1")))

		store.changedPathsErrByHash = map[CommitHash]error{"c1": errors.New("changed paths failed")}
		_, err = service.ListSnapshotPage(ctx, "", 1)
		Expect(err).To(MatchError("changed paths failed"))
	})

	It("covers page revision cursor, paging, error, and disabled branches", func() {
		page := workspaceSyncEdgePage("page-1", "Page", "page", tree.NodeKindPage)
		disabled := &Service{}
		revisions, err := disabled.ListPageRevisions(ctx, page, "", 0)
		Expect(err).NotTo(HaveOccurred())
		Expect(revisions.Revisions).To(BeEmpty())

		store := &fakeRevisionStore{
			commits: []gitrevisions.Commit{
				workspaceSyncCoverageCommitValue("cursor"),
				workspaceSyncCoverageCommitValue("c1"),
				workspaceSyncCoverageCommitValue("c2"),
			},
			changedContents: map[CommitHash]map[string]string{
				"c1": {"page.md": workspaceSyncEdgeMarkdown("page-1", "Page v1")},
				"c2": {"page.md": workspaceSyncEdgeMarkdown("page-1", "Page v2")},
			},
		}
		service := workspaceSyncCoverageService(store, &fakeTreeReconstructor{})
		list, err := service.ListPageRevisions(ctx, page, "cursor", 1)
		Expect(err).NotTo(HaveOccurred())
		Expect(list.Revisions).To(HaveLen(1))
		Expect(list.NextCursor).To(Equal("c1"))
		Expect(store.scannedCommits).To(Equal(3))

		store.changedContentsErr = errors.New("changed contents failed")
		_, err = service.ListPageRevisions(ctx, page, "", 1)
		Expect(err).To(MatchError("changed contents failed"))
	})

	It("covers workspace restore disabled, default source, store error, reconstruct error, writeback error, and after-sync error", func() {
		disabled := &Service{}
		_, err := disabled.RestoreWorkspace(ctx, "commit", PublicEditorActor())
		Expect(err).To(MatchError("workspace sync is not enabled"))

		store := &fakeRevisionStore{capture: workspaceSyncCoverageCommit("restore-error"), restoreWorkspaceErr: errors.New("restore failed")}
		service := workspaceSyncCoverageService(store, &fakeTreeReconstructor{})
		_, err = service.RestoreWorkspaceWithSource(ctx, "commit", PublicEditorActor(), "")
		Expect(err).To(MatchError("restore failed"))
		Expect(store.restoreWorkspaceRequests[0].Source).To(Equal(SourceSystem))

		store = &fakeRevisionStore{capture: workspaceSyncCoverageCommit("restore-reconstruct", "docs/a.md")}
		service = workspaceSyncCoverageService(store, &fakeTreeReconstructor{err: errors.New("restore reconstruct failed")})
		status, err := service.RestoreWorkspaceWithSource(ctx, "commit", PublicEditorActor(), SourceMCP)
		Expect(err).NotTo(HaveOccurred())
		Expect(status.LastError).To(Equal("restore reconstruct failed"))

		store = &fakeRevisionStore{capture: workspaceSyncCoverageCommit("restore-amend", "docs/a.md"), amendErr: errors.New("restore amend failed")}
		service = workspaceSyncCoverageService(store, &fakeTreeReconstructor{})
		_, err = service.RestoreWorkspaceWithSource(ctx, "commit", PublicEditorActor(), SourceMCP)
		Expect(err).To(MatchError("restore amend failed"))

		store = &fakeRevisionStore{capture: workspaceSyncCoverageCommit("restore-after", "docs/a.md")}
		service = workspaceSyncCoverageService(store, &fakeTreeReconstructor{})
		service.SetAfterSync(func() error { return errors.New("restore after failed") })
		_, err = service.RestoreWorkspaceWithSource(ctx, "commit", PublicEditorActor(), SourceMCP)
		Expect(err).To(MatchError("restore after failed"))
	})

	It("covers page snapshot and document restore error branches", func() {
		page := workspaceSyncEdgePage("page-1", "Page", "page", tree.NodeKindPage)
		disabled := &Service{}
		_, err := disabled.GetPageRevisionSnapshot(ctx, page, "commit")
		Expect(err).To(MatchError("workspace sync is not enabled"))
		_, err = disabled.RestoreDocument(ctx, page, "commit", PublicEditorActor())
		Expect(err).To(MatchError("workspace sync is not enabled"))

		store := &fakeRevisionStore{changedContentsErr: errors.New("changed contents failed")}
		service := workspaceSyncCoverageService(store, &fakeTreeReconstructor{})
		_, err = service.GetPageRevisionSnapshot(ctx, page, "commit")
		Expect(err).To(MatchError("changed contents failed"))
		_, err = service.RestoreDocumentWithSource(ctx, page, "commit", PublicEditorActor(), "")
		Expect(err).To(MatchError("changed contents failed"))
		Expect(service.Status().LastError).To(Equal("changed contents failed"))

		store = &fakeRevisionStore{changedContents: map[CommitHash]map[string]string{"commit": {"other.md": workspaceSyncEdgeMarkdown("other", "Other")}}}
		service = workspaceSyncCoverageService(store, &fakeTreeReconstructor{})
		_, err = service.GetPageRevisionSnapshot(ctx, page, "commit")
		Expect(err).To(MatchError(ContainSubstring("did not change")))
		_, err = service.RestoreDocumentWithSource(ctx, page, "commit", PublicEditorActor(), SourceMCP)
		Expect(err).To(MatchError(ContainSubstring("did not change")))

		store = &fakeRevisionStore{
			changedContents: map[CommitHash]map[string]string{"commit": {"page.md": workspaceSyncEdgeMarkdown("page-1", "Page")}},
			getCommitErr:    errors.New("get commit failed"),
		}
		service = workspaceSyncCoverageService(store, &fakeTreeReconstructor{})
		_, err = service.GetPageRevisionSnapshot(ctx, page, "commit")
		Expect(err).To(MatchError("get commit failed"))

		store = &fakeRevisionStore{
			capture:                   workspaceSyncCoverageCommit("restore-doc", "page.md"),
			changedContents:           map[CommitHash]map[string]string{"commit": {"page.md": workspaceSyncEdgeMarkdown("page-1", "Page")}},
			restoreDocumentContentErr: errors.New("restore document failed"),
		}
		service = workspaceSyncCoverageService(store, &fakeTreeReconstructor{})
		_, err = service.RestoreDocumentWithSource(ctx, page, "commit", PublicEditorActor(), "")
		Expect(err).To(MatchError("restore document failed"))
		Expect(store.restoreDocumentContentRequests[0].Source).To(Equal(SourceSystem))

		store.restoreDocumentContentErr = nil
		service = workspaceSyncCoverageService(store, &fakeTreeReconstructor{err: errors.New("document reconstruct failed")})
		_, err = service.RestoreDocumentWithSource(ctx, page, "commit", PublicEditorActor(), SourceMCP)
		Expect(err).To(MatchError("document reconstruct failed"))

		store = &fakeRevisionStore{
			capture:         workspaceSyncCoverageCommit("restore-doc-amend", "page.md"),
			changedContents: map[CommitHash]map[string]string{"commit": {"page.md": workspaceSyncEdgeMarkdown("page-1", "Page")}},
			amendErr:        errors.New("document amend failed"),
		}
		service = workspaceSyncCoverageService(store, &fakeTreeReconstructor{})
		_, err = service.RestoreDocumentWithSource(ctx, page, "commit", PublicEditorActor(), SourceMCP)
		Expect(err).To(MatchError("document amend failed"))

		store = &fakeRevisionStore{
			capture:         workspaceSyncCoverageCommit("restore-doc-after", "page.md"),
			changedContents: map[CommitHash]map[string]string{"commit": {"page.md": workspaceSyncEdgeMarkdown("page-1", "Page")}},
		}
		service = workspaceSyncCoverageService(store, &fakeTreeReconstructor{})
		service.SetAfterSync(func() error { return errors.New("document after failed") })
		_, err = service.RestoreDocumentWithSource(ctx, page, "commit", PublicEditorActor(), SourceMCP)
		Expect(err).To(MatchError("document after failed"))
	})

	It("covers path selection helpers for fallback, route scans, and revision routes", func() {
		rootDir := GinkgoT().TempDir()
		Expect(os.MkdirAll(filepath.Join(rootDir, "docs"), 0o755)).To(Succeed())
		Expect(os.MkdirAll(filepath.Join(rootDir, ".hidden"), 0o755)).To(Succeed())
		Expect(os.WriteFile(filepath.Join(rootDir, ".hidden", "ignored.md"), []byte("# Hidden\n"), 0o644)).To(Succeed())
		Expect(os.WriteFile(filepath.Join(rootDir, "docs", "README.md"), []byte("# Readme\n"), 0o644)).To(Succeed())
		Expect(os.WriteFile(filepath.Join(rootDir, "docs", "Page.MD"), []byte("# Page\n"), 0o644)).To(Succeed())
		Expect(os.WriteFile(filepath.Join(rootDir, "docs", "route.md"), []byte("# Route\n"), 0o644)).To(Succeed())
		service := &Service{rootDir: rootDir}
		section := workspaceSyncEdgePage("docs", "Docs", "docs", tree.NodeKindSection)
		page := workspaceSyncEdgePage("page-1", "Page", "page", tree.NodeKindPage)
		page.Parent = &tree.PageNode{ID: "docs", Title: "Docs", Slug: "docs", Kind: tree.NodeKindSection}
		routePage := workspaceSyncEdgePage("route-1", "Route", "route", tree.NodeKindPage)
		routePage.Parent = page.Parent

		Expect(service.currentPageMarkdownPath(section)).To(Equal("docs/README.md"))
		Expect(service.currentPageMarkdownPath(page)).To(Equal("docs/Page.MD"))
		found, ok := service.currentWorkspaceMarkdownPathByRoute(routePage)
		Expect(ok).To(BeTrue())
		Expect(found).To(Equal("docs/route.md"))
		_, ok = (&Service{rootDir: filepath.Join(rootDir, "missing")}).currentWorkspaceMarkdownPathByRoute(page)
		Expect(ok).To(BeFalse())
		Expect(service.currentSectionContentPath("docs", "fallback.md")).To(Equal("docs/README.md"))
		Expect(service.currentSectionContentPath("", "fallback.md")).To(Equal("fallback.md"))

		content, relPath, ok := contentForPageAtCommitPath(rootDir, page, "preferred.md", map[string]string{
			"preferred.md": workspaceSyncEdgeMarkdown("other", "Other"),
			"docs/Page.MD": workspaceSyncEdgeMarkdown("page-1", "Page"),
		})
		Expect(ok).To(BeTrue())
		Expect(relPath).To(Equal("docs/Page.MD"))
		Expect(content).To(ContainSubstring("Page"))
		_, _, ok = changedContentForPageAtCommit(rootDir, page, "preferred.md", map[string]string{
			"preferred.md": workspaceSyncEdgeMarkdown("other", "Other"),
		})
		Expect(ok).To(BeFalse())
		Expect(contentMatchesLeafWikiID(page, "---\nleafwiki_id: [broken\n---\n# Broken\n")).To(BeFalse())

		routePath, kind := revisionRoutePathAndKind("", "docs/index.md", page)
		Expect(routePath.FilesystemPath()).To(Equal("docs"))
		Expect(kind).To(Equal(tree.NodeKindSection))
		routePath, kind = revisionRoutePathAndKind("", "weird///page.md", page)
		Expect(routePath.FilesystemPath()).To(Equal("weird/page"))
		Expect(kind).To(Equal(tree.NodeKindPage))
	})

	It("covers validation fallback and markdown path extraction edges", func() {
		rootDir := GinkgoT().TempDir()
		Expect(os.WriteFile(filepath.Join(rootDir, "bad.md"), []byte("[missing](/missing)\n"), 0o644)).To(Succeed())
		service := &Service{rootDir: rootDir, markdownLinkRootPrefix: "/"}

		errs := service.validationErrorsFromError(errors.New("generic failure"))
		Expect(errs).To(HaveLen(1))
		Expect(errs[0].Code).NotTo(Equal(""))
		Expect(errs[0].Path).To(Equal("bad"))

		Expect(markdownPathsInError(rootDir, "file=../outside.md file=. path=notes.txt")).To(BeEmpty())
	})

	It("covers the default watcher factory and closed watcher channel branches", func() {
		preserveWorkspacesyncCoverageSeams()
		wrapped := newFakeFSWatcher()
		workspacesyncNewFSWatcher = func(...fswatcher.WatcherOpt) (fswatcher.Watcher, error) {
			return wrapped, nil
		}
		service := workspaceSyncCoverageService(
			&fakeRevisionStore{capture: workspaceSyncCoverageCommit("default-watcher")},
			&fakeTreeReconstructor{},
		)

		Expect(service.StartWatcher(ctx)).To(Succeed())
		Eventually(func() bool { return service.Status().WatcherRunning }).Should(BeTrue())
		service.StopWatcher()
		Expect(wrapped.closeCount).To(Equal(1))

		watcher := newFakeWatcher()
		close(watcher.events)
		service.consumeWatcherEvents(ctx, watcher)
		Expect(service.Status().PendingEventCount).To(Equal(0))

		watcher = newFakeWatcher()
		close(watcher.dropped)
		service.consumeWatcherEvents(ctx, watcher)
		Expect(service.Status().PendingEventCount).To(Equal(0))

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

	It("covers canonical migration second-reconstruct rollback and filesystem seam failures", func() {
		preserveWorkspacesyncCoverageSeams()

		rootDir := GinkgoT().TempDir()
		Expect(os.MkdirAll(filepath.Join(rootDir, "docs"), 0o755)).To(Succeed())
		Expect(os.WriteFile(filepath.Join(rootDir, "docs", "a.md"), []byte("[B](/docs/b)\n"), 0o644)).To(Succeed())
		Expect(os.WriteFile(filepath.Join(rootDir, "docs", "b.md"), []byte("# B\n"), 0o644)).To(Succeed())
		service := workspaceSyncCoverageService(
			&fakeRevisionStore{capture: workspaceSyncCoverageCommit("migration-second-reconstruct")},
			&fakeTreeReconstructor{errs: []error{nil, errors.New("second reconstruct failed"), nil}},
		)
		service.rootDir = rootDir
		status, err := service.SyncNow(ctx, SyncRequest{Reason: ReasonExplicit, Source: SourceFilesystem, Actor: PublicEditorActor()})
		Expect(err).NotTo(HaveOccurred())
		Expect(status.LastError).To(Equal("second reconstruct failed"))

		service = &Service{rootDir: ""}
		changed, rollback, err := service.migrateCanonicalMarkdownLinksLockedWithRollback()
		Expect(err).NotTo(HaveOccurred())
		Expect(changed).To(BeFalse())
		Expect(rollback).To(BeNil())

		rootFile := filepath.Join(GinkgoT().TempDir(), "root.md")
		Expect(os.WriteFile(rootFile, []byte("# Root\n"), 0o644)).To(Succeed())
		service = &Service{rootDir: rootFile}
		changed, rollback, err = service.migrateCanonicalMarkdownLinksLockedWithRollback()
		Expect(err).NotTo(HaveOccurred())
		Expect(changed).To(BeFalse())
		Expect(rollback).To(BeNil())

		service = &Service{rootDir: "/workspace"}
		workspacesyncOSStat = func(string) (os.FileInfo, error) {
			return nil, errors.New("stat failed")
		}
		_, _, err = service.migrateCanonicalMarkdownLinksLockedWithRollback()
		Expect(err).To(MatchError("stat failed"))

		workspacesyncOSStat = os.Stat
		workspacesyncNewMarkdownLinkIndex = func(string, markdownlinks.Options) (*markdownlinks.Index, error) {
			return nil, errors.New("index failed")
		}
		service.rootDir = GinkgoT().TempDir()
		_, _, err = service.migrateCanonicalMarkdownLinksLockedWithRollback()
		Expect(err).To(MatchError("index failed"))

		workspacesyncNewMarkdownLinkIndex = markdownlinks.NewIndexFromRootWithOptions
		workspacesyncWalkDir = func(root string, fn fs.WalkDirFunc) error {
			return fn(filepath.Join(root, "bad.md"), workspacesyncCoverageDirEntry{name: "bad.md"}, errors.New("walk failed"))
		}
		_, _, err = service.migrateCanonicalMarkdownLinksLockedWithRollback()
		Expect(err).To(MatchError("walk failed"))

		workspacesyncWalkDir = func(root string, fn fs.WalkDirFunc) error {
			return fn(filepath.Join(root, "bad.md"), workspacesyncCoverageDirEntry{name: "bad.md"}, nil)
		}
		workspacesyncRel = func(string, string) (string, error) {
			return "", errors.New("rel failed")
		}
		_, _, err = service.migrateCanonicalMarkdownLinksLockedWithRollback()
		Expect(err).To(MatchError("rel failed"))

		workspacesyncRel = filepath.Rel
		workspacesyncWalkDir = func(root string, fn fs.WalkDirFunc) error {
			Expect(fn(filepath.Join(root, ".hidden"), workspacesyncCoverageDirEntry{name: ".hidden", dir: true}, nil)).To(Equal(filepath.SkipDir))
			Expect(fn(filepath.Join(root, "notes.txt"), workspacesyncCoverageDirEntry{name: "notes.txt"}, nil)).To(Succeed())
			return nil
		}
		changed, rollback, err = service.migrateCanonicalMarkdownLinksLockedWithRollback()
		Expect(err).NotTo(HaveOccurred())
		Expect(changed).To(BeFalse())
		Expect(rollback).To(BeNil())

		index := markdownlinks.NewIndexWithOptions([]markdownlinks.Entry{
			{Kind: markdownlinks.EntryKindSection},
			{Kind: markdownlinks.EntryKindPage, RoutePath: tree.RoutePathFromString("b"), ContentPath: tree.MarkdownPathFromString("b.md")},
		}, markdownlinks.Options{})
		workspacesyncNewMarkdownLinkIndex = func(string, markdownlinks.Options) (*markdownlinks.Index, error) {
			return index, nil
		}
		workspacesyncWalkDir = func(root string, fn fs.WalkDirFunc) error {
			return fn(filepath.Join(root, "a.md"), workspacesyncCoverageDirEntry{name: "a.md"}, nil)
		}
		workspacesyncReadFile = func(string) ([]byte, error) {
			return nil, errors.New("read failed")
		}
		_, _, err = service.migrateCanonicalMarkdownLinksLockedWithRollback()
		Expect(err).To(MatchError("read failed"))

		workspacesyncReadFile = func(string) ([]byte, error) {
			return []byte("[B](/b)\n"), nil
		}
		workspacesyncWalkDir = func(root string, fn fs.WalkDirFunc) error {
			return fn(filepath.Join(root, "a.md"), workspacesyncCoverageDirEntry{name: "a.md", infoErr: errors.New("info failed")}, nil)
		}
		_, _, err = service.migrateCanonicalMarkdownLinksLockedWithRollback()
		Expect(err).To(MatchError("info failed"))
	})

	It("covers canonical rewrite cleanup and rollback failures through filesystem seams", func() {
		preserveWorkspacesyncCoverageSeams()
		rewrite := canonicalMarkdownRewrite{Path: "/workspace/a.md", Original: []byte("old"), Content: []byte("new"), Mode: 0o644}

		workspacesyncCreateTemp = func(string, string) (workspacesyncTempFile, error) {
			return nil, errors.New("create temp failed")
		}
		Expect(writeCanonicalMarkdownRewritesAtomically([]canonicalMarkdownRewrite{rewrite})).To(MatchError("create temp failed"))

		removed := []string{}
		workspacesyncRemove = func(path string) error {
			removed = append(removed, path)
			return nil
		}
		workspacesyncCreateTemp = func(string, string) (workspacesyncTempFile, error) {
			return &workspacesyncCoverageTempFile{name: "/workspace/.a.tmp", writeErr: errors.New("write failed")}, nil
		}
		Expect(writeCanonicalMarkdownRewritesAtomically([]canonicalMarkdownRewrite{rewrite})).To(MatchError("write failed"))
		Expect(removed).To(ContainElement("/workspace/.a.tmp"))

		workspacesyncCreateTemp = func(string, string) (workspacesyncTempFile, error) {
			return &workspacesyncCoverageTempFile{name: "/workspace/.a.tmp", closeErr: errors.New("close failed")}, nil
		}
		Expect(writeCanonicalMarkdownRewritesAtomically([]canonicalMarkdownRewrite{rewrite})).To(MatchError("close failed"))

		workspacesyncCreateTemp = func(string, string) (workspacesyncTempFile, error) {
			return &workspacesyncCoverageTempFile{name: "/workspace/.a.tmp"}, nil
		}
		workspacesyncChmod = func(string, os.FileMode) error {
			return errors.New("chmod failed")
		}
		Expect(writeCanonicalMarkdownRewritesAtomically([]canonicalMarkdownRewrite{rewrite})).To(MatchError("chmod failed"))

		workspacesyncChmod = func(string, os.FileMode) error {
			return nil
		}
		tempNames := []string{"/workspace/.a.tmp", "/workspace/.b.tmp"}
		workspacesyncCreateTemp = func(string, string) (workspacesyncTempFile, error) {
			name := tempNames[0]
			tempNames = tempNames[1:]
			return &workspacesyncCoverageTempFile{name: name}, nil
		}
		renameCalls := 0
		workspacesyncRename = func(string, string) error {
			renameCalls++
			if renameCalls == 2 {
				return errors.New("rename failed")
			}
			return nil
		}
		workspacesyncWriteFile = func(string, []byte, os.FileMode) error {
			return errors.New("rollback write failed")
		}
		err := writeCanonicalMarkdownRewritesAtomically([]canonicalMarkdownRewrite{
			rewrite,
			{Path: "/workspace/b.md", Original: []byte("old b"), Content: []byte("new b"), Mode: 0o644},
		})
		Expect(err).To(MatchError(ContainSubstring("rollback failed")))

		Expect(rollbackCanonicalMarkdownRewrites([]canonicalMarkdownRewrite{rewrite})).To(MatchError("rollback write failed"))
	})

	It("covers service default limits, writeback nil/error, path miss, and fallback route helpers", func() {
		service := workspaceSyncCoverageService(&fakeRevisionStore{}, &fakeTreeReconstructor{})
		snapshots, err := service.ListSnapshotPage(ctx, "", 0)
		Expect(err).NotTo(HaveOccurred())
		Expect(snapshots.Snapshots).To(BeEmpty())

		page := workspaceSyncEdgePage("page-1", "Page", "page", tree.NodeKindPage)
		revisions, err := service.ListPageRevisions(ctx, page, "", 0)
		Expect(err).NotTo(HaveOccurred())
		Expect(revisions.Revisions).To(BeEmpty())

		Expect(service.captureWritebacksLocked(ctx, gitrevisions.CommitRequest{}, nil, true)).To(Succeed())
		service.store = &fakeRevisionStore{changedContentsErr: errors.New("changed contents failed")}
		Expect(service.captureWritebacksLocked(ctx, gitrevisions.CommitRequest{}, workspaceSyncCoverageCommit("writeback-error"), true)).To(MatchError("changed contents failed"))

		section := workspaceSyncEdgePage("!!!", "Invalid", "!!!", tree.NodeKindSection)
		routePath, kind := revisionRoutePathAndKind("", "!!!/README.md", section)
		Expect(routePath.FilesystemPath()).To(Equal("!!!"))
		Expect(kind).To(Equal(tree.NodeKindSection))
		routePath, kind = revisionRoutePathAndKind("", "!!!/index.md", page)
		Expect(routePath.FilesystemPath()).To(Equal("!!!"))
		Expect(kind).To(Equal(tree.NodeKindSection))
		routePath, kind = revisionRoutePathAndKind("", "!!!/page.md", page)
		Expect(routePath.FilesystemPath()).To(Equal("!!!/page"))
		Expect(kind).To(Equal(tree.NodeKindPage))

		content, relPath, ok := contentForPageAtCommitPath("", page, "preferred.md", map[string]string{
			"bad.md": "<!-- leafwiki\nnot yaml\n-->\n# Broken\n",
			"old.md": workspaceSyncEdgeMarkdown("page-1", "Old Page"),
		})
		Expect(ok).To(BeTrue())
		Expect(relPath).To(Equal("old.md"))
		Expect(content).To(ContainSubstring("Old Page"))

		content, relPath, ok = changedContentForPageAtCommit("", page, "preferred.md", map[string]string{
			"bad.md": "<!-- leafwiki\nnot yaml\n-->\n# Broken\n",
			"old.md": workspaceSyncEdgeMarkdown("page-1", "Old Page"),
		})
		Expect(ok).To(BeTrue())
		Expect(relPath).To(Equal("old.md"))
		Expect(content).To(ContainSubstring("Old Page"))

		eventPath, managed := managedMarkdownEventPath("/workspace", " ")
		Expect(eventPath).To(BeEmpty())
		Expect(managed).To(BeFalse())
		eventPath, managed = managedMarkdownEventPath("/workspace", ".")
		Expect(eventPath).To(BeEmpty())
		Expect(managed).To(BeFalse())
		eventPath, managed = managedMarkdownEventPath("/workspace", "../outside.md")
		Expect(eventPath).To(BeEmpty())
		Expect(managed).To(BeFalse())
		eventPath, managed = managedMarkdownEventPath("/workspace", "notes.txt")
		Expect(eventPath).To(BeEmpty())
		Expect(managed).To(BeFalse())

		timer := time.NewTimer(time.Hour)
		stopWorkspacesyncTimer(timer)
		timer = time.NewTimer(time.Nanosecond)
		Eventually(timer.C).Should(Receive())
		stopWorkspacesyncTimer(timer)
		timer = time.NewTimer(time.Nanosecond)
		Eventually(timer.C).Should(Receive())
		drainWorkspacesyncTimer(timer)

		preserveWorkspacesyncCoverageSeams()
		rootDir := GinkgoT().TempDir()
		Expect(os.MkdirAll(filepath.Join(rootDir, "docs"), 0o755)).To(Succeed())
		Expect(os.WriteFile(filepath.Join(rootDir, "docs", "README.md"), []byte("# Readme\n"), 0o644)).To(Succeed())
		workspacesyncWalkDir = func(string, fs.WalkDirFunc) error {
			return errors.New("route scan failed")
		}
		sectionWithReadme := workspaceSyncEdgePage("docs", "Docs", "docs", tree.NodeKindSection)
		Expect((&Service{rootDir: rootDir}).currentPageMarkdownPath(sectionWithReadme)).To(Equal("docs/README.md"))

		workspacesyncWalkDir = func(root string, fn fs.WalkDirFunc) error {
			Expect(fn(root, workspacesyncCoverageDirEntry{name: filepath.Base(root), dir: true}, nil)).To(Succeed())
			Expect(fn(filepath.Join(root, "notes.txt"), workspacesyncCoverageDirEntry{name: "notes.txt"}, nil)).To(Succeed())
			Expect(fn(filepath.Join(root, ".hidden"), workspacesyncCoverageDirEntry{name: ".hidden", dir: true}, nil)).To(Equal(filepath.SkipDir))
			return nil
		}
		_, found := (&Service{rootDir: rootDir}).currentWorkspaceMarkdownPathByRoute(page)
		Expect(found).To(BeFalse())

		workspacesyncWalkDir = func(root string, fn fs.WalkDirFunc) error {
			return fn(filepath.Join(root, "page.md"), workspacesyncCoverageDirEntry{name: "page.md"}, nil)
		}
		workspacesyncRel = func(string, string) (string, error) {
			return "", errors.New("route rel failed")
		}
		_, found = (&Service{rootDir: rootDir}).currentWorkspaceMarkdownPathByRoute(page)
		Expect(found).To(BeFalse())

		routePage := workspaceSyncEdgePage("page-1", "Page", "page", tree.NodeKindPage)
		routePage.Parent = &tree.PageNode{ID: "docs", Title: "Docs", Slug: "docs", Kind: tree.NodeKindSection}
		content, relPath, ok = contentForPageAtCommitPath(rootDir, routePage, "preferred.md", map[string]string{
			"docs/page.md": "# Page without metadata\n",
		})
		Expect(ok).To(BeTrue())
		Expect(relPath).To(Equal("docs/page.md"))
		Expect(content).To(ContainSubstring("without metadata"))

		_, _, ok = changedContentForPageAtCommit(rootDir, routePage, "preferred.md", map[string]string{})
		Expect(ok).To(BeFalse())
		content, relPath, ok = changedContentForPageAtCommit(rootDir, routePage, "preferred.md", map[string]string{
			"docs/page.md": "# Changed without metadata\n",
		})
		Expect(ok).To(BeTrue())
		Expect(relPath).To(Equal("docs/page.md"))
		Expect(content).To(ContainSubstring("Changed"))

		Expect((&Service{}).validationErrorsFromError(nil)).To(BeNil())
		Expect(markdownPathsInError(rootDir, "notes.txt has no markdown token")).To(BeEmpty())
	})
})

func workspaceSyncCoverageService(store *fakeRevisionStore, treeService treeReconstructor) *Service {
	GinkgoHelper()
	return &Service{
		enabled: true,
		rootDir: "/workspace",
		tree:    treeService,
		store:   store,
		status:  SyncStatus{Enabled: true},
	}
}

func preserveWorkspacesyncCoverageSeams() {
	GinkgoHelper()
	previousNewFSWatcher := workspacesyncNewFSWatcher
	previousRewriteWriter := canonicalMarkdownRewriteWriter
	previousStat := workspacesyncOSStat
	previousNewIndex := workspacesyncNewMarkdownLinkIndex
	previousWalkDir := workspacesyncWalkDir
	previousRel := workspacesyncRel
	previousReadFile := workspacesyncReadFile
	previousCreateTemp := workspacesyncCreateTemp
	previousRemove := workspacesyncRemove
	previousRename := workspacesyncRename
	previousChmod := workspacesyncChmod
	previousWriteFile := workspacesyncWriteFile
	DeferCleanup(func() {
		workspacesyncNewFSWatcher = previousNewFSWatcher
		canonicalMarkdownRewriteWriter = previousRewriteWriter
		workspacesyncOSStat = previousStat
		workspacesyncNewMarkdownLinkIndex = previousNewIndex
		workspacesyncWalkDir = previousWalkDir
		workspacesyncRel = previousRel
		workspacesyncReadFile = previousReadFile
		workspacesyncCreateTemp = previousCreateTemp
		workspacesyncRemove = previousRemove
		workspacesyncRename = previousRename
		workspacesyncChmod = previousChmod
		workspacesyncWriteFile = previousWriteFile
	})
}

type workspacesyncCoverageTempFile struct {
	name     string
	writeErr error
	closeErr error
}

func (f *workspacesyncCoverageTempFile) Name() string {
	return f.name
}

func (f *workspacesyncCoverageTempFile) Write([]byte) (int, error) {
	if f.writeErr != nil {
		return 0, f.writeErr
	}
	return 0, nil
}

func (f *workspacesyncCoverageTempFile) Close() error {
	return f.closeErr
}

type workspacesyncCoverageDirEntry struct {
	name    string
	dir     bool
	infoErr error
}

func (e workspacesyncCoverageDirEntry) Name() string {
	return e.name
}

func (e workspacesyncCoverageDirEntry) IsDir() bool {
	return e.dir
}

func (e workspacesyncCoverageDirEntry) Type() fs.FileMode {
	if e.dir {
		return fs.ModeDir
	}
	return 0
}

func (e workspacesyncCoverageDirEntry) Info() (fs.FileInfo, error) {
	if e.infoErr != nil {
		return nil, e.infoErr
	}
	return workspacesyncCoverageFileInfo{name: e.name, mode: 0o644}, nil
}

type workspacesyncCoverageFileInfo struct {
	name string
	mode fs.FileMode
}

func (i workspacesyncCoverageFileInfo) Name() string       { return i.name }
func (i workspacesyncCoverageFileInfo) Size() int64        { return 0 }
func (i workspacesyncCoverageFileInfo) Mode() fs.FileMode  { return i.mode }
func (i workspacesyncCoverageFileInfo) ModTime() time.Time { return time.Time{} }
func (i workspacesyncCoverageFileInfo) IsDir() bool        { return i.mode.IsDir() }
func (i workspacesyncCoverageFileInfo) Sys() any           { return nil }

func workspaceSyncCoverageCommit(hash string, paths ...string) *gitrevisions.Commit {
	GinkgoHelper()
	commit := workspaceSyncCoverageCommitValue(hash, paths...)
	return &commit
}

func workspaceSyncCoverageCommitValue(hash string, paths ...string) gitrevisions.Commit {
	GinkgoHelper()
	return gitrevisions.Commit{
		Hash:                 hash,
		BatchID:              "batch-" + hash,
		Message:              "commit " + hash,
		AuthorID:             gitrevisions.NewActorIDUnchecked("author-" + hash),
		AuthorName:           "Author " + hash,
		AuthorEmail:          hash + "@example.test",
		CreatedAt:            time.Date(2026, 6, 27, 12, 0, 0, 0, time.UTC),
		Reason:               ReasonExplicit,
		Source:               SourceFilesystem,
		ChangedMarkdownCount: len(paths),
		ChangedMarkdownPaths: append([]string(nil), paths...),
		Created:              true,
	}
}
