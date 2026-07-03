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
	"github.com/onsi/gomega/gstruct"

	"github.com/perber/wiki/internal/core/markdownlinks"
	wikivalidation "github.com/perber/wiki/internal/core/markdownvalidation"
	"github.com/perber/wiki/internal/core/tree"
	testmatchers "github.com/perber/wiki/internal/test_utils/matchers"
	"github.com/perber/wiki/internal/workspacesync/gitrevisions"
	"github.com/sgtdi/fswatcher"
)

var errCanonicalMarkdownMigrationUnchanged = errors.New("canonical markdown migration did not change files")

var _ = Describe("workspace sync deterministic service behavior", func() {
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
		Expect(service.Status()).To(gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
			"PendingEventCount":          BeZero(),
			"RecentChangedMarkdownPaths": Equal([]string{"docs/a.md"}),
		}))
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

		captureErr := errors.New("capture failed")
		store := &fakeRevisionStore{
			capture:    workspaceSyncCoverageCommit("capture-failed"),
			captureErr: captureErr,
		}
		service := workspaceSyncCoverageService(store, &fakeTreeReconstructor{})
		service.log = logger
		_, err := service.SyncNow(ctx, SyncRequest{Reason: ReasonStartup, Source: SourceFilesystem, Actor: PublicEditorActor()})
		Expect(err).To(MatchError(captureErr))

		store = &fakeRevisionStore{capture: workspaceSyncCoverageCommit("reconstruct-failed", "docs/a.md")}
		reconstructErr := errors.New("reconstruct failed")
		treeService := &fakeTreeReconstructor{err: reconstructErr}
		service = workspaceSyncCoverageService(store, treeService)
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
		store = &fakeRevisionStore{capture: workspaceSyncCoverageCommit("migration-failed")}
		service = workspaceSyncCoverageService(store, &fakeTreeReconstructor{})
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
			capture:  workspaceSyncCoverageCommit("writeback-failed"),
			amendErr: amendErr,
		}
		service = workspaceSyncCoverageService(store, &fakeTreeReconstructor{})
		service.log = logger
		_, err = service.SyncNow(ctx, SyncRequest{Reason: ReasonStartup, Source: SourceFilesystem, Actor: PublicEditorActor()})
		Expect(err).To(MatchError(amendErr))

		store = &fakeRevisionStore{capture: workspaceSyncCoverageCommit("after-sync-failed")}
		service = workspaceSyncCoverageService(store, &fakeTreeReconstructor{})
		service.log = logger
		afterSyncErr := errors.New("after sync failed")
		service.SetAfterSync(func() error { return afterSyncErr })
		_, err = service.SyncNow(ctx, SyncRequest{Reason: ReasonStartup, Source: SourceFilesystem, Actor: PublicEditorActor()})
		Expect(err).To(MatchError(afterSyncErr))

		Expect(logs.String()).To(ContainSubstring("workspace sync startup failed"))
	})

	It("rolls back canonical migration status for nil, failed, tree-less, reconstruct-failed, and successful rollbacks", func() {
		service := &Service{status: SyncStatus{LastError: "primary"}}
		service.rollbackCanonicalMarkdownMigrationLocked(nil)
		Expect(service.status.LastError).To(Equal("primary"))

		rollbackErr := errors.New("rollback failed")
		service.rollbackCanonicalMarkdownMigrationLocked(func() error { return rollbackErr })
		Expect(service.status).To(gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
			"LastError":        Not(Equal("primary")),
			"ValidationErrors": BeNil(),
		}))

		service = &Service{status: SyncStatus{LastError: "primary"}}
		service.rollbackCanonicalMarkdownMigrationLocked(func() error { return nil })
		Expect(service.status.LastError).To(Equal("primary"))

		reconstructRollbackErr := errors.New("reconstruct rollback failed")
		reconstructTree := &fakeTreeReconstructor{err: reconstructRollbackErr}
		service = &Service{tree: reconstructTree, status: SyncStatus{LastError: "primary"}}
		service.rollbackCanonicalMarkdownMigrationLocked(func() error { return nil })
		Expect(service.status).To(gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
			"LastError":        Not(Equal("primary")),
			"ValidationErrors": Not(BeEmpty()),
		}))
		Expect(reconstructTree.reconstructCount()).To(Equal(1))

		service = &Service{tree: &fakeTreeReconstructor{}, status: SyncStatus{LastError: "primary", ValidationErrors: []ValidationError{{Path: "old"}}}}
		service.rollbackCanonicalMarkdownMigrationLocked(func() error { return nil })
		Expect(service.status).To(gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
			"LastError":        Equal("primary"),
			"ValidationErrors": BeNil(),
		}))
	})

	It("returns snapshot pages for disabled services and propagates list failures", func() {
		disabled := &Service{}
		page, err := disabled.ListSnapshotPage(ctx, "", 0)
		Expect(err).To(Succeed())
		Expect(page.Snapshots).To(BeEmpty())

		listErr := errors.New("list failed")
		store := &fakeRevisionStore{listErr: listErr}
		service := workspaceSyncCoverageService(store, &fakeTreeReconstructor{})
		_, err = service.ListSnapshots(ctx, 1)
		Expect(err).To(MatchError(listErr))

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
		Expect(err).To(Succeed())
		Expect(list).To(gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
			"Snapshots":  HaveLen(1),
			"NextCursor": Equal(CommitHashFromString("c1")),
		}))

		changedPathsErr := errors.New("changed paths failed")
		store.changedPathsErrByHash = map[CommitHash]error{"c1": changedPathsErr}
		_, err = service.ListSnapshotPage(ctx, "", 1)
		Expect(err).To(MatchError(changedPathsErr))
	})

	It("lists page revisions with cursor scans and propagates content lookup failures", func() {
		page := workspaceSyncEdgePage("page-1", "Page", "page", tree.NodeKindPage)
		disabled := &Service{}
		revisions, err := disabled.ListPageRevisions(ctx, page, "", 0)
		Expect(err).To(Succeed())
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
		Expect(err).To(Succeed())
		Expect(list).To(gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
			"Revisions":  HaveLen(1),
			"NextCursor": Equal("c1"),
		}))
		Expect(store.scannedCommits).To(Equal(3))

		changedContentsErr := errors.New("changed contents failed")
		store.changedContentsErr = changedContentsErr
		_, err = service.ListPageRevisions(ctx, page, "", 1)
		Expect(err).To(MatchError(changedContentsErr))
	})

	It("restores workspaces with default source metadata and propagates restore failures", func() {
		disabled := &Service{}
		_, err := disabled.RestoreWorkspace(ctx, "commit", PublicEditorActor())
		Expect(err).To(MatchError(ErrWorkspaceSyncDisabled))

		restoreErr := errors.New("restore failed")
		store := &fakeRevisionStore{capture: workspaceSyncCoverageCommit("restore-error"), restoreWorkspaceErr: restoreErr}
		service := workspaceSyncCoverageService(store, &fakeTreeReconstructor{})
		_, err = service.RestoreWorkspaceWithSource(ctx, "commit", PublicEditorActor(), "")
		Expect(err).To(MatchError(restoreErr))
		Expect(store.restoreWorkspaceRequests).To(ContainElement(gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
			"Source": Equal(SourceSystem),
		})))

		store = &fakeRevisionStore{capture: workspaceSyncCoverageCommit("restore-reconstruct", "docs/a.md")}
		restoreReconstructErr := errors.New("restore reconstruct failed")
		service = workspaceSyncCoverageService(store, &fakeTreeReconstructor{err: restoreReconstructErr})
		status, err := service.RestoreWorkspaceWithSource(ctx, "commit", PublicEditorActor(), SourceMCP)
		Expect(err).To(Succeed())
		Expect(status.ValidationErrors).NotTo(BeEmpty())

		restoreAmendErr := errors.New("restore amend failed")
		store = &fakeRevisionStore{capture: workspaceSyncCoverageCommit("restore-amend", "docs/a.md"), amendErr: restoreAmendErr}
		service = workspaceSyncCoverageService(store, &fakeTreeReconstructor{})
		_, err = service.RestoreWorkspaceWithSource(ctx, "commit", PublicEditorActor(), SourceMCP)
		Expect(err).To(MatchError(restoreAmendErr))

		store = &fakeRevisionStore{capture: workspaceSyncCoverageCommit("restore-after", "docs/a.md")}
		service = workspaceSyncCoverageService(store, &fakeTreeReconstructor{})
		restoreAfterErr := errors.New("restore after failed")
		service.SetAfterSync(func() error { return restoreAfterErr })
		_, err = service.RestoreWorkspaceWithSource(ctx, "commit", PublicEditorActor(), SourceMCP)
		Expect(err).To(MatchError(restoreAfterErr))
	})

	It("returns page snapshots and document restore errors with source metadata", func() {
		page := workspaceSyncEdgePage("page-1", "Page", "page", tree.NodeKindPage)
		disabled := &Service{}
		_, err := disabled.GetPageRevisionSnapshot(ctx, page, "commit")
		Expect(err).To(MatchError(ErrWorkspaceSyncDisabled))
		_, err = disabled.RestoreDocument(ctx, page, "commit", PublicEditorActor())
		Expect(err).To(MatchError(ErrWorkspaceSyncDisabled))

		changedContentsErr := errors.New("changed contents failed")
		store := &fakeRevisionStore{changedContentsErr: changedContentsErr}
		service := workspaceSyncCoverageService(store, &fakeTreeReconstructor{})
		_, err = service.GetPageRevisionSnapshot(ctx, page, "commit")
		Expect(err).To(MatchError(changedContentsErr))
		_, err = service.RestoreDocumentWithSource(ctx, page, "commit", PublicEditorActor(), "")
		Expect(err).To(MatchError(changedContentsErr))

		store = &fakeRevisionStore{changedContents: map[CommitHash]map[string]string{"commit": {"other.md": workspaceSyncEdgeMarkdown("other", "Other")}}}
		service = workspaceSyncCoverageService(store, &fakeTreeReconstructor{})
		_, err = service.GetPageRevisionSnapshot(ctx, page, "commit")
		Expect(err).To(MatchError(ErrWorkspaceSyncDocumentUnchanged))
		_, err = service.RestoreDocumentWithSource(ctx, page, "commit", PublicEditorActor(), SourceMCP)
		Expect(err).To(MatchError(ErrWorkspaceSyncDocumentUnchanged))

		store = &fakeRevisionStore{
			changedContents: map[CommitHash]map[string]string{"commit": {"page.md": workspaceSyncEdgeMarkdown("page-1", "Page")}},
			getCommitErr:    errors.New("get commit failed"),
		}
		getCommitErr := store.getCommitErr
		service = workspaceSyncCoverageService(store, &fakeTreeReconstructor{})
		_, err = service.GetPageRevisionSnapshot(ctx, page, "commit")
		Expect(err).To(MatchError(getCommitErr))

		restoreDocumentErr := errors.New("restore document failed")
		store = &fakeRevisionStore{
			capture:                   workspaceSyncCoverageCommit("restore-doc", "page.md"),
			changedContents:           map[CommitHash]map[string]string{"commit": {"page.md": workspaceSyncEdgeMarkdown("page-1", "Page")}},
			restoreDocumentContentErr: restoreDocumentErr,
		}
		service = workspaceSyncCoverageService(store, &fakeTreeReconstructor{})
		_, err = service.RestoreDocumentWithSource(ctx, page, "commit", PublicEditorActor(), "")
		Expect(err).To(MatchError(restoreDocumentErr))
		Expect(store.restoreDocumentContentRequests).To(ContainElement(gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
			"Source": Equal(SourceSystem),
		})))

		store.restoreDocumentContentErr = nil
		documentReconstructErr := errors.New("document reconstruct failed")
		service = workspaceSyncCoverageService(store, &fakeTreeReconstructor{err: documentReconstructErr})
		_, err = service.RestoreDocumentWithSource(ctx, page, "commit", PublicEditorActor(), SourceMCP)
		Expect(err).To(MatchError(documentReconstructErr))

		documentAmendErr := errors.New("document amend failed")
		store = &fakeRevisionStore{
			capture:         workspaceSyncCoverageCommit("restore-doc-amend", "page.md"),
			changedContents: map[CommitHash]map[string]string{"commit": {"page.md": workspaceSyncEdgeMarkdown("page-1", "Page")}},
			amendErr:        documentAmendErr,
		}
		service = workspaceSyncCoverageService(store, &fakeTreeReconstructor{})
		_, err = service.RestoreDocumentWithSource(ctx, page, "commit", PublicEditorActor(), SourceMCP)
		Expect(err).To(MatchError(documentAmendErr))

		store = &fakeRevisionStore{
			capture:         workspaceSyncCoverageCommit("restore-doc-after", "page.md"),
			changedContents: map[CommitHash]map[string]string{"commit": {"page.md": workspaceSyncEdgeMarkdown("page-1", "Page")}},
		}
		service = workspaceSyncCoverageService(store, &fakeTreeReconstructor{})
		documentAfterErr := errors.New("document after failed")
		service.SetAfterSync(func() error { return documentAfterErr })
		_, err = service.RestoreDocumentWithSource(ctx, page, "commit", PublicEditorActor(), SourceMCP)
		Expect(err).To(MatchError(documentAfterErr))
	})

	It("selects markdown paths from source metadata route scans and revision routes", func() {
		rootDir := workspaceSyncTempDir()
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
		found, err := currentWorkspaceMarkdownPathByRouteResult(service, routePage)
		Expect(err).To(Succeed())
		Expect(found).To(Equal("docs/route.md"))
		_, err = currentWorkspaceMarkdownPathByRouteResult(&Service{rootDir: filepath.Join(rootDir, "missing")}, page)
		Expect(err).To(MatchError(errChangedContentMissing))
		Expect(service.currentSectionContentPath("docs", "fallback.md")).To(Equal("docs/README.md"))
		Expect(service.currentSectionContentPath("", "fallback.md")).To(Equal("fallback.md"))

		result, err := contentForPageAtCommitPathResult(rootDir, page, "preferred.md", map[string]string{
			"preferred.md": workspaceSyncEdgeMarkdown("other", "Other"),
			"docs/Page.MD": workspaceSyncEdgeMarkdown("page-1", "Page"),
		})
		Expect(err).To(Succeed())
		Expect(result).To(gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
			"RelPath": Equal("docs/Page.MD"),
			"Content": ContainSubstring("Page"),
		}))
		_, err = changedContentForPageAtCommitResult(rootDir, page, "preferred.md", map[string]string{
			"preferred.md": workspaceSyncEdgeMarkdown("other", "Other"),
		})
		Expect(err).To(MatchError(errChangedContentMissing))
		_, err = leafWikiIDFromContentResult("---\nleafwiki_id: [broken\n---\n# Broken\n")
		Expect(err).To(MatchError(errLeafWikiIDMissing))

		routePath, kind := revisionRoutePathAndKind("", "docs/index.md", page)
		Expect(routePath.FilesystemPath()).To(Equal("docs"))
		Expect(kind).To(Equal(tree.NodeKindSection))
		routePath, kind = revisionRoutePathAndKind("", "weird///page.md", page)
		Expect(routePath.FilesystemPath()).To(Equal("weird/page"))
		Expect(kind).To(Equal(tree.NodeKindPage))
	})

	It("falls back to markdown validation issues and extracts markdown paths from errors", func() {
		rootDir := workspaceSyncTempDir()
		Expect(os.WriteFile(filepath.Join(rootDir, "bad.md"), []byte("[missing](/missing)\n"), 0o644)).To(Succeed())
		service := &Service{rootDir: rootDir, markdownLinkRootPrefix: "/"}

		errs := service.validationErrorsFromError(errors.New("generic failure"))
		Expect(errs).To(HaveLen(1))
		Expect(errs).To(testmatchers.HaveValidationIssue(wikivalidation.IssueCodeBrokenLink))
		Expect(errs).To(ContainElement(gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
			"Path": Equal("bad"),
		})))

		Expect(markdownPathsInError(rootDir, "file=../outside.md file=. path=notes.txt")).To(BeEmpty())
	})

	It("starts the default watcher and drains closed watcher channels", func() {
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
		Eventually(func() SyncStatus { return service.Status() }).Should(gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
			"WatcherRunning": BeTrue(),
		}))
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

	It("rolls back canonical migration when reconstruction and filesystem seams fail", func() {
		preserveWorkspacesyncCoverageSeams()

		rootDir := workspaceSyncTempDir()
		Expect(os.MkdirAll(filepath.Join(rootDir, "docs"), 0o755)).To(Succeed())
		Expect(os.WriteFile(filepath.Join(rootDir, "docs", "a.md"), []byte("[B](/docs/b)\n"), 0o644)).To(Succeed())
		Expect(os.WriteFile(filepath.Join(rootDir, "docs", "b.md"), []byte("# B\n"), 0o644)).To(Succeed())
		secondReconstructErr := errors.New("second reconstruct failed")
		service := workspaceSyncCoverageService(
			&fakeRevisionStore{capture: workspaceSyncCoverageCommit("migration-second-reconstruct")},
			&fakeTreeReconstructor{errs: []error{nil, secondReconstructErr, nil}},
		)
		service.rootDir = rootDir
		_, err := service.SyncNow(ctx, SyncRequest{Reason: ReasonExplicit, Source: SourceFilesystem, Actor: PublicEditorActor()})
		Expect(err).To(Succeed())
		Expect(readFileStringGinkgo(filepath.Join(rootDir, "docs", "a.md"))).To(SatisfyAll(
			ContainSubstring("[B](/docs/b)\n"),
			Not(ContainSubstring("[B](/docs/b.md)")),
		))
		Expect(service.tree.(*fakeTreeReconstructor).reconstructCount()).To(Equal(3))

		service = &Service{rootDir: ""}
		rollback, err := canonicalMarkdownMigrationRollbackResult(service)
		Expect(err).To(MatchError(errCanonicalMarkdownMigrationUnchanged))
		Expect(rollback).To(BeNil())

		rootFile := filepath.Join(workspaceSyncTempDir(), "root.md")
		Expect(os.WriteFile(rootFile, []byte("# Root\n"), 0o644)).To(Succeed())
		service = &Service{rootDir: rootFile}
		rollback, err = canonicalMarkdownMigrationRollbackResult(service)
		Expect(err).To(MatchError(errCanonicalMarkdownMigrationUnchanged))
		Expect(rollback).To(BeNil())

		service = &Service{rootDir: "/workspace"}
		statErr := errors.New("stat failed")
		workspacesyncOSStat = func(string) (os.FileInfo, error) {
			return nil, statErr
		}
		_, err = canonicalMarkdownMigrationRollbackResult(service)
		Expect(err).To(MatchError(statErr))

		workspacesyncOSStat = os.Stat
		indexErr := errors.New("index failed")
		workspacesyncNewMarkdownLinkIndex = func(string, markdownlinks.Options) (*markdownlinks.Index, error) {
			return nil, indexErr
		}
		service.rootDir = workspaceSyncTempDir()
		_, err = canonicalMarkdownMigrationRollbackResult(service)
		Expect(err).To(MatchError(indexErr))

		workspacesyncNewMarkdownLinkIndex = markdownlinks.NewIndexFromRootWithOptions
		walkErr := errors.New("walk failed")
		workspacesyncWalkDir = func(root string, fn fs.WalkDirFunc) error {
			return fn(filepath.Join(root, "bad.md"), workspacesyncCoverageDirEntry{name: "bad.md"}, walkErr)
		}
		_, err = canonicalMarkdownMigrationRollbackResult(service)
		Expect(err).To(MatchError(walkErr))

		workspacesyncWalkDir = func(root string, fn fs.WalkDirFunc) error {
			return fn(filepath.Join(root, "bad.md"), workspacesyncCoverageDirEntry{name: "bad.md"}, nil)
		}
		relErr := errors.New("rel failed")
		workspacesyncRel = func(string, string) (string, error) {
			return "", relErr
		}
		_, err = canonicalMarkdownMigrationRollbackResult(service)
		Expect(err).To(MatchError(relErr))

		workspacesyncRel = filepath.Rel
		workspacesyncWalkDir = func(root string, fn fs.WalkDirFunc) error {
			Expect(fn(filepath.Join(root, ".hidden"), workspacesyncCoverageDirEntry{name: ".hidden", dir: true}, nil)).To(Equal(filepath.SkipDir))
			Expect(fn(filepath.Join(root, "notes.txt"), workspacesyncCoverageDirEntry{name: "notes.txt"}, nil)).To(Succeed())
			return nil
		}
		rollback, err = canonicalMarkdownMigrationRollbackResult(service)
		Expect(err).To(MatchError(errCanonicalMarkdownMigrationUnchanged))
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
		readErr := errors.New("read failed")
		workspacesyncReadFile = func(string) ([]byte, error) {
			return nil, readErr
		}
		_, err = canonicalMarkdownMigrationRollbackResult(service)
		Expect(err).To(MatchError(readErr))

		workspacesyncReadFile = func(string) ([]byte, error) {
			return []byte("[B](/b)\n"), nil
		}
		infoErr := errors.New("info failed")
		workspacesyncWalkDir = func(root string, fn fs.WalkDirFunc) error {
			return fn(filepath.Join(root, "a.md"), workspacesyncCoverageDirEntry{name: "a.md", infoErr: infoErr}, nil)
		}
		_, err = canonicalMarkdownMigrationRollbackResult(service)
		Expect(err).To(MatchError(infoErr))
	})

	It("cleans temporary canonical rewrite files and reports rollback failures", func() {
		preserveWorkspacesyncCoverageSeams()
		rewrite := canonicalMarkdownRewrite{Path: "/workspace/a.md", Original: []byte("old"), Content: []byte("new"), Mode: 0o644}

		createTempErr := errors.New("create temp failed")
		workspacesyncCreateTemp = func(string, string) (workspacesyncTempFile, error) {
			return nil, createTempErr
		}
		Expect(writeCanonicalMarkdownRewritesAtomically([]canonicalMarkdownRewrite{rewrite})).To(MatchError(createTempErr))

		removed := []string{}
		workspacesyncRemove = func(path string) error {
			removed = append(removed, path)
			return nil
		}
		writeErr := errors.New("write failed")
		workspacesyncCreateTemp = func(string, string) (workspacesyncTempFile, error) {
			return &workspacesyncCoverageTempFile{name: "/workspace/.a.tmp", writeErr: writeErr}, nil
		}
		Expect(writeCanonicalMarkdownRewritesAtomically([]canonicalMarkdownRewrite{rewrite})).To(MatchError(writeErr))
		Expect(removed).To(ContainElement("/workspace/.a.tmp"))

		closeErr := errors.New("close failed")
		workspacesyncCreateTemp = func(string, string) (workspacesyncTempFile, error) {
			return &workspacesyncCoverageTempFile{name: "/workspace/.a.tmp", closeErr: closeErr}, nil
		}
		Expect(writeCanonicalMarkdownRewritesAtomically([]canonicalMarkdownRewrite{rewrite})).To(MatchError(closeErr))

		workspacesyncCreateTemp = func(string, string) (workspacesyncTempFile, error) {
			return &workspacesyncCoverageTempFile{name: "/workspace/.a.tmp"}, nil
		}
		chmodErr := errors.New("chmod failed")
		workspacesyncChmod = func(string, os.FileMode) error {
			return chmodErr
		}
		Expect(writeCanonicalMarkdownRewritesAtomically([]canonicalMarkdownRewrite{rewrite})).To(MatchError(chmodErr))

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
		renameErr := errors.New("rename failed")
		workspacesyncRename = func(string, string) error {
			renameCalls++
			if renameCalls == 2 {
				return renameErr
			}
			return nil
		}
		rollbackWriteErr := errors.New("rollback write failed")
		workspacesyncWriteFile = func(string, []byte, os.FileMode) error {
			return rollbackWriteErr
		}
		err := writeCanonicalMarkdownRewritesAtomically([]canonicalMarkdownRewrite{
			rewrite,
			{Path: "/workspace/b.md", Original: []byte("old b"), Content: []byte("new b"), Mode: 0o644},
		})
		Expect(err).To(MatchError(rollbackWriteErr))

		Expect(rollbackCanonicalMarkdownRewrites([]canonicalMarkdownRewrite{rewrite})).To(MatchError(rollbackWriteErr))
	})

	It("applies default list limits and resolves fallback route helpers", func() {
		service := workspaceSyncCoverageService(&fakeRevisionStore{}, &fakeTreeReconstructor{})
		snapshots, err := service.ListSnapshotPage(ctx, "", 0)
		Expect(err).NotTo(HaveOccurred())
		Expect(snapshots.Snapshots).To(BeEmpty())

		page := workspaceSyncEdgePage("page-1", "Page", "page", tree.NodeKindPage)
		revisions, err := service.ListPageRevisions(ctx, page, "", 0)
		Expect(err).NotTo(HaveOccurred())
		Expect(revisions.Revisions).To(BeEmpty())

		Expect(service.captureWritebacksLocked(ctx, gitrevisions.CommitRequest{}, nil, true)).To(Succeed())
		changedContentsErr := errors.New("changed contents failed")
		service.store = &fakeRevisionStore{changedContentsErr: changedContentsErr}
		Expect(service.captureWritebacksLocked(ctx, gitrevisions.CommitRequest{}, workspaceSyncCoverageCommit("writeback-error"), true)).To(MatchError(changedContentsErr))

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

		result, err := contentForPageAtCommitPathResult("", page, "preferred.md", map[string]string{
			"bad.md": "<!-- leafwiki\nnot yaml\n-->\n# Broken\n",
			"old.md": workspaceSyncEdgeMarkdown("page-1", "Old Page"),
		})
		Expect(err).To(Succeed())
		Expect(result).To(gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
			"RelPath": Equal("old.md"),
			"Content": ContainSubstring("Old Page"),
		}))

		result, err = changedContentForPageAtCommitResult("", page, "preferred.md", map[string]string{
			"bad.md": "<!-- leafwiki\nnot yaml\n-->\n# Broken\n",
			"old.md": workspaceSyncEdgeMarkdown("page-1", "Old Page"),
		})
		Expect(err).To(Succeed())
		Expect(result).To(gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
			"RelPath": Equal("old.md"),
			"Content": ContainSubstring("Old Page"),
		}))

		_, err = managedMarkdownEventPathResult("/workspace", " ")
		Expect(err).To(MatchError(errManagedMarkdownEventIgnored))
		_, err = managedMarkdownEventPathResult("/workspace", ".")
		Expect(err).To(MatchError(errManagedMarkdownEventIgnored))
		_, err = managedMarkdownEventPathResult("/workspace", "../outside.md")
		Expect(err).To(MatchError(errManagedMarkdownEventIgnored))
		_, err = managedMarkdownEventPathResult("/workspace", "notes.txt")
		Expect(err).To(MatchError(errManagedMarkdownEventIgnored))

		timer := time.NewTimer(time.Hour)
		stopWorkspacesyncTimer(timer)
		timer = time.NewTimer(time.Nanosecond)
		Eventually(timer.C).Should(Receive())
		stopWorkspacesyncTimer(timer)
		timer = time.NewTimer(time.Nanosecond)
		Eventually(timer.C).Should(Receive())
		drainWorkspacesyncTimer(timer)

		preserveWorkspacesyncCoverageSeams()
		rootDir := workspaceSyncTempDir()
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
		_, err = currentWorkspaceMarkdownPathByRouteResult(&Service{rootDir: rootDir}, page)
		Expect(err).To(MatchError(errChangedContentMissing))

		workspacesyncWalkDir = func(root string, fn fs.WalkDirFunc) error {
			return fn(filepath.Join(root, "page.md"), workspacesyncCoverageDirEntry{name: "page.md"}, nil)
		}
		workspacesyncRel = func(string, string) (string, error) {
			return "", errors.New("route rel failed")
		}
		_, err = currentWorkspaceMarkdownPathByRouteResult(&Service{rootDir: rootDir}, page)
		Expect(err).To(MatchError(errChangedContentMissing))

		routePage := workspaceSyncEdgePage("page-1", "Page", "page", tree.NodeKindPage)
		routePage.Parent = &tree.PageNode{ID: "docs", Title: "Docs", Slug: "docs", Kind: tree.NodeKindSection}
		result, err = contentForPageAtCommitPathResult(rootDir, routePage, "preferred.md", map[string]string{
			"docs/page.md": "# Page without metadata\n",
		})
		Expect(err).To(Succeed())
		Expect(result).To(gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
			"RelPath": Equal("docs/page.md"),
			"Content": ContainSubstring("without metadata"),
		}))

		_, err = changedContentForPageAtCommitResult(rootDir, routePage, "preferred.md", map[string]string{})
		Expect(err).To(MatchError(errChangedContentMissing))
		result, err = changedContentForPageAtCommitResult(rootDir, routePage, "preferred.md", map[string]string{
			"docs/page.md": "# Changed without metadata\n",
		})
		Expect(err).To(Succeed())
		Expect(result).To(gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
			"RelPath": Equal("docs/page.md"),
			"Content": ContainSubstring("Changed"),
		}))

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

func canonicalMarkdownMigrationRollbackResult(service *Service) (func() error, error) {
	GinkgoHelper()

	changed, rollback, err := service.migrateCanonicalMarkdownLinksLockedWithRollback()
	if err != nil {
		return nil, err
	}
	if !changed {
		return rollback, errCanonicalMarkdownMigrationUnchanged
	}
	return rollback, nil
}

func contentForPageAtCommitPathResult(rootDir string, page *tree.Page, preferredPath string, files map[string]string) (changedContentResult, error) {
	GinkgoHelper()

	content, relPath, ok := contentForPageAtCommitPath(rootDir, page, preferredPath, files)
	if !ok {
		return changedContentResult{}, errChangedContentMissing
	}
	return changedContentResult{
		Content: content,
		RelPath: relPath,
	}, nil
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
		Hash:                 CommitHashFromString(hash),
		BatchID:              "batch-" + hash,
		Message:              "commit " + hash,
		AuthorID:             gitrevisions.ParseActorID("author-" + hash),
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
