package workspacesync

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/gstruct"
	wikivalidation "github.com/perber/wiki/internal/core/markdownvalidation"
	"github.com/perber/wiki/internal/core/tree"
	"github.com/perber/wiki/internal/workspacesync/gitrevisions"
	"github.com/sgtdi/fswatcher"
)

var (
	errAdditionalFSWatcherFailed       = errors.New("fswatcher failed")
	errAdditionalCaptureFailed         = errors.New("capture failed")
	errAdditionalChangedContentsFailed = errors.New("changed contents failed")
	errAdditionalWatchStopped          = errors.New("watch stopped")
)

var _ = Describe("workspace sync additional edge coverage", func() {
	Describe("watcher adapter", func() {
		It("constructs and closes a real filesystem watcher", func() {
			watcher, err := newFileWatcher(GinkgoT().TempDir())
			Expect(err).NotTo(HaveOccurred())
			Expect(watcher).NotTo(BeNil())

			closer, ok := watcher.(closableFileWatcher)
			Expect(ok).To(BeTrue())
			closer.Close()
		})

		It("surfaces fswatcher construction failures", func() {
			previous := workspacesyncNewFSWatcher
			DeferCleanup(func() { workspacesyncNewFSWatcher = previous })
			workspacesyncNewFSWatcher = func(...fswatcher.WatcherOpt) (fswatcher.Watcher, error) {
				return nil, errAdditionalFSWatcherFailed
			}

			watcher, err := newFileWatcher("/workspace")

			Expect(watcher).To(BeNil())
			Expect(err).To(MatchError(errAdditionalFSWatcherFailed))
		})

		It("pumps events and dropped events until the wrapped watcher returns", func() {
			wrapped := newFakeFSWatcher()
			adapter := &fsWatcherAdapter{
				watcher: wrapped,
				events:  make(chan watcherEvent, 4),
				dropped: make(chan watcherEvent, 4),
			}
			release := make(chan struct{})
			wrapped.watch = func(ctx context.Context) error {
				wrapped.events <- fswatcher.WatchEvent{Path: "/workspace/a.md"}
				wrapped.dropped <- fswatcher.WatchEvent{Path: "/workspace/dropped.md"}
				close(wrapped.events)
				close(wrapped.dropped)
				select {
				case <-release:
					return errAdditionalWatchStopped
				case <-ctx.Done():
					return ctx.Err()
				}
			}

			done := make(chan error, 1)
			go func() {
				done <- adapter.Watch(context.Background())
			}()

			Eventually(adapter.Events()).Should(Receive(Equal(watcherEvent{Path: "/workspace/a.md"})))
			Eventually(adapter.Dropped()).Should(Receive(Equal(watcherEvent{Path: "/workspace/dropped.md", Dropped: true})))
			close(release)
			Eventually(done).Should(Receive(MatchError(errAdditionalWatchStopped)))
			adapter.Close()
			Expect(wrapped.closeCount).To(Equal(1))
		})

		It("stops pumping when the context is canceled", func() {
			wrapped := newFakeFSWatcher()
			adapter := &fsWatcherAdapter{
				watcher: wrapped,
				events:  make(chan watcherEvent),
				dropped: make(chan watcherEvent),
			}
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			var wg sync.WaitGroup
			wg.Add(1)

			adapter.pump(ctx, &wg, wrapped.events, adapter.events, false)

			done := make(chan struct{})
			go func() {
				defer close(done)
				wg.Wait()
			}()
			Eventually(done).Should(BeClosed())
		})
	})

	Describe("service helpers", func() {
		It("covers disabled service and startup logging no-op branches", func() {
			service, err := NewService(ServiceOptions{Enabled: false})
			Expect(err).NotTo(HaveOccurred())

			status, err := service.SyncNow(context.Background(), SyncRequest{})
			Expect(err).NotTo(HaveOccurred())
			Expect(status.Enabled).To(BeFalse())
			Expect(service.StartWatcher(context.Background())).To(Succeed())
			service.StopWatcher()
			Expect(service.logStartupSyncStarted(false, SyncRequest{})).To(BeZero())
			service.logStartupSyncCompleted(false, time.Now(), SyncStatus{})
			service.logStartupSyncFailed(false, time.Now(), errors.New("ignored"))
			Expect(service.logStartupPhaseStarted(false, "phase")).To(BeZero())
			service.logStartupPhaseCompleted(false, "phase", time.Now())
			service.logStartupPhaseFailed(false, "phase", time.Now(), errors.New("ignored"))
		})

		It("reports store construction failures when no store is injected", func() {
			blocker := filepath.Join(GinkgoT().TempDir(), "not-a-dir")
			Expect(os.WriteFile(blocker, []byte("x"), 0o644)).To(Succeed())

			service, err := NewService(ServiceOptions{
				Enabled: true,
				DataDir: filepath.Join(blocker, "data"),
				RootDir: filepath.Join(blocker, "root"),
				Tree:    &fakeTreeReconstructor{},
			})

			Expect(service).To(BeNil())
			Expect(err).To(HaveOccurred())
		})

		It("updates watcher batch status for dropped events and sync errors", func() {
			service := &Service{
				enabled: true,
				tree:    &fakeTreeReconstructor{},
				store:   &fakeRevisionStore{captureErr: errAdditionalCaptureFailed},
				status:  SyncStatus{Enabled: true, PendingEventCount: 1},
			}

			service.handleWatcherBatch(context.Background(), 3, true, "")
			Expect(service.status).To(gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
				"PendingEventCount": BeZero(),
				"LastError":         Equal(watcherDroppedEventsStatus("")),
			}))

			service.status.LastError = ""
			service.status.PendingEventCount = 2
			service.handleWatcherBatch(context.Background(), 1, true, "docs/a.md")
			Expect(service.status).To(gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
				"PendingEventCount": Equal(1),
				"LastError":         Equal(watcherDroppedEventsStatus("docs/a.md")),
			}))

			service.status.LastError = ""
			service.handleWatcherBatch(context.Background(), 1, false, "")
			Expect(service.status.LastError).To(Equal(errAdditionalCaptureFailed.Error()))
		})

		It("records changed markdown paths with trimming, dedupe, and history bounds", func() {
			service := &Service{}
			paths := []string{" ", "a.md", "a.md"}
			for i := 0; i < 25; i++ {
				paths = append(paths, tree.MarkdownPathFromString("docs/page-"+string(rune('a'+i))+".md").FilesystemPath())
			}

			service.recordChangedMarkdownPaths(paths)

			Expect(service.status.RecentChangedMarkdownPaths).To(HaveLen(20))
			Expect(service.status.RecentChangedMarkdownPaths).NotTo(ContainElement(""))
			Expect(service.status.RecentChangedMarkdownPaths).NotTo(ContainElement("a.md"))
		})

		It("merges validation errors without aliasing or duplicates", func() {
			existing := []ValidationError{{Code: wikivalidation.IssueCodeBrokenLink, Path: "a", Message: "same"}}
			next := []ValidationError{
				{Code: wikivalidation.IssueCodeBrokenLink, Path: "a", Message: "same"},
				{Code: wikivalidation.IssueCodeInvalidSlug, Path: "b", Message: "other"},
			}

			Expect(mergeValidationErrors(nil, next)).To(Equal(next))
			Expect(mergeValidationErrors(existing, nil)).To(Equal(existing))
			Expect(mergeValidationErrors(existing, next)).To(Equal([]ValidationError{
				{Code: wikivalidation.IssueCodeBrokenLink, Path: "a", Message: "same"},
				{Code: wikivalidation.IssueCodeInvalidSlug, Path: "b", Message: "other"},
			}))
		})

		It("detects metadata writeback requirements in captured markdown", func() {
			store := &fakeRevisionStore{changedContents: map[CommitHash]map[string]string{
				"metadata": {
					"notes.txt": "not markdown",
					"page.md":   "---\nleafwiki_id: page-1\nleafwiki_title: Page One\n---\n# Page One\n",
				},
				"parse-error": {
					"page.md": "<!-- leafwiki\nnot yaml\n-->\n# Broken\n",
				},
			}}

			requires, err := capturedMarkdownRequiresMetadataWriteback(context.Background(), nil, "metadata")
			Expect(err).NotTo(HaveOccurred())
			Expect(requires).To(BeFalse())

			requires, err = capturedMarkdownRequiresMetadataWriteback(context.Background(), store, "")
			Expect(err).NotTo(HaveOccurred())
			Expect(requires).To(BeFalse())

			requires, err = capturedMarkdownRequiresMetadataWriteback(context.Background(), store, "metadata")
			Expect(err).NotTo(HaveOccurred())
			Expect(requires).To(BeTrue())

			_, err = capturedMarkdownRequiresMetadataWriteback(context.Background(), &fakeRevisionStore{changedContentsErr: errAdditionalChangedContentsFailed}, "metadata")
			Expect(err).To(MatchError(errAdditionalChangedContentsFailed))

			_, err = capturedMarkdownRequiresMetadataWriteback(context.Background(), store, "parse-error")
			Expect(err).To(HaveOccurred())
		})

		It("covers markdown path and revision content helpers", func() {
			rootDir := filepath.Join(GinkgoT().TempDir(), "workspace")
			Expect(os.MkdirAll(filepath.Join(rootDir, "docs"), 0o755)).To(Succeed())
			Expect(os.WriteFile(filepath.Join(rootDir, "docs", "README.md"), []byte("# Readme\n"), 0o644)).To(Succeed())
			Expect(os.WriteFile(filepath.Join(rootDir, "docs", "Page.MD"), []byte("# Page\n"), 0o644)).To(Succeed())
			service := &Service{rootDir: rootDir}
			root := workspaceSyncEdgePage("root", "Root", "", tree.NodeKindSection)
			page := workspaceSyncEdgePage("page-1", "Page", "page", tree.NodeKindPage)
			page.Parent = &tree.PageNode{ID: "docs", Title: "Docs", Slug: "docs", Kind: tree.NodeKindSection}
			section := workspaceSyncEdgePage("docs", "Docs", "docs", tree.NodeKindSection)

			Expect(pageMarkdownPath(root)).To(Equal("index.md"))
			Expect(pageWorkspaceSourcePath(nil)).To(BeEmpty())
			Expect(service.currentPageMarkdownPath(workspaceSyncEdgePage("missing", "Missing", "missing", tree.NodeKindPage))).To(Equal("missing.md"))
			Expect(service.currentPageMarkdownPath(section)).To(Equal("docs/README.md"))
			_, ok := service.currentWorkspaceMarkdownPathByRoute(nil)
			Expect(ok).To(BeFalse())
			Expect(service.currentSectionContentPath("missing", "fallback.md")).To(Equal("fallback.md"))

			files := map[string]string{
				"docs/Page.MD": workspaceSyncEdgeMarkdown("page-1", "Historical Title"),
				"notes.txt":    "ignored",
				"other.md":     "not markdown: [",
			}
			content, relPath, ok := changedContentForPageAtCommit(rootDir, page, "missing.md", files)
			Expect(ok).To(BeTrue())
			Expect(relPath).To(Equal("docs/Page.MD"))
			Expect(content).To(ContainSubstring("Historical Title"))
			Expect(sortedMarkdownPaths(files)).To(Equal([]string{"docs/Page.MD", "other.md"}))
			Expect(markdownPathMatchesPageRoute(rootDir, nil, "docs/Page.MD")).To(BeFalse())
			Expect(contentMatchesLeafWikiID(page, workspaceSyncEdgeMarkdown("different-page", "Different"))).To(BeFalse())
			_, ok = leafWikiIDFromContent("---\nleafwiki_id: [broken\n---\n# Broken\n")
			Expect(ok).To(BeFalse())

			commit := gitrevisions.Commit{Hash: CommitHashFromString("hash-1"), Message: "", AuthorID: gitrevisions.ParseActorID(""), CreatedAt: time.Date(2026, 6, 27, 12, 0, 0, 0, time.UTC)}
			rev := revisionForPageContent(rootDir, page, commit, "docs/Page.MD", files["docs/Page.MD"])
			Expect(rev).To(gstruct.PointTo(gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
				"AuthorID": Equal(PublicEditorActor().ID.MetadataValue()),
				"Summary":  Equal("workspace sync"),
				"Title":    Equal("Historical Title"),
			})))
		})

		It("extracts markdown paths from quoted error tokens without duplicates", func() {
			rootDir := filepath.Join(GinkgoT().TempDir(), "workspace")
			message := "open path=" + filepath.Join(rootDir, "docs", "a.md") + ": failed file='docs/a.md' file=../outside.md bad=nope.txt"

			Expect(markdownPathsInError(rootDir, message)).To(Equal([]string{"docs/a.md"}))
			Expect(markdownPathsInError(rootDir, "no markdown here")).To(BeEmpty())
		})

		It("uses route path when validation issue source path is absent", func() {
			issue := wikivalidationIssueForRoute("docs/page")

			Expect(markdownValidationIssuePath(issue)).To(Equal("docs/page"))
		})
	})
})

type fakeFSWatcher struct {
	events     chan fswatcher.WatchEvent
	dropped    chan fswatcher.WatchEvent
	watch      func(context.Context) error
	closeCount int
}

func newFakeFSWatcher() *fakeFSWatcher {
	return &fakeFSWatcher{
		events:  make(chan fswatcher.WatchEvent, 4),
		dropped: make(chan fswatcher.WatchEvent, 4),
	}
}

func (f *fakeFSWatcher) Watch(ctx context.Context) error {
	if f.watch != nil {
		return f.watch(ctx)
	}
	<-ctx.Done()
	return ctx.Err()
}

func (f *fakeFSWatcher) AddPath(string, ...fswatcher.PathOption) error { return nil }
func (f *fakeFSWatcher) DropPath(string) error                         { return nil }
func (f *fakeFSWatcher) Events() <-chan fswatcher.WatchEvent           { return f.events }
func (f *fakeFSWatcher) Dropped() <-chan fswatcher.WatchEvent          { return f.dropped }
func (f *fakeFSWatcher) IsRunning() bool                               { return false }
func (f *fakeFSWatcher) Stats() fswatcher.WatcherStats                 { return fswatcher.WatcherStats{} }
func (f *fakeFSWatcher) Paths() []string                               { return nil }
func (f *fakeFSWatcher) Log(fswatcher.Severity, string, ...any)        {}
func (f *fakeFSWatcher) Close()                                        { f.closeCount++ }

func readWatcherEvents(ch <-chan watcherEvent) []watcherEvent {
	GinkgoHelper()
	var out []watcherEvent
	for event := range ch {
		out = append(out, event)
	}
	return out
}

func wikivalidationIssueForRoute(route string) wikivalidation.Issue {
	GinkgoHelper()
	return wikivalidation.Issue{
		RoutePath: tree.RoutePathFromString(route),
	}
}
