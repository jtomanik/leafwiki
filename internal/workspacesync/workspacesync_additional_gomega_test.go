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

	"github.com/perber/wiki/internal/core/markdown"
	wikivalidation "github.com/perber/wiki/internal/core/markdownvalidation"
	"github.com/perber/wiki/internal/core/tree"
	"github.com/perber/wiki/internal/workspacesync/gitrevisions"
	"github.com/sgtdi/fswatcher"
)

var (
	errAdditionalFSWatcherFailed        = errors.New("fswatcher failed")
	errAdditionalCaptureFailed          = errors.New("capture failed")
	errAdditionalChangedContentsFailed  = errors.New("changed contents failed")
	errAdditionalWatchStopped           = errors.New("watch stopped")
	errCapturedMarkdownAlreadyCanonical = errors.New("captured markdown already has canonical metadata")
	errChangedContentMissing            = errors.New("changed content for page is missing")
	errLeafWikiIDMissing                = errors.New("leafwiki ID is missing from content")
	errMarkdownPathMatchesRoute         = errors.New("markdown path matches page route")
	errMarkdownPathMissesRoute          = errors.New("markdown path does not match page route")
	errPathErrorMissing                 = errors.New("path error missing")
	errWatcherNotClosable               = errors.New("watcher does not expose close")
)

var _ = Describe("workspace sync filesystem watcher and helper contracts", func() {
	Describe("watcher adapter", func() {
		It("constructs and closes a real filesystem watcher", func() {
			watcher, err := newFileWatcher(workspaceSyncTempDir())
			Expect(err).To(Succeed())
			Expect(watcher).NotTo(BeNil())

			closer, err := closableFileWatcherResult(watcher)
			Expect(err).To(Succeed())
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
		It("returns disabled status and keeps startup logging disabled as a no-op", func() {
			service, err := NewService(ServiceOptions{Enabled: false})
			Expect(err).To(Succeed())

			status, err := service.SyncNow(context.Background(), SyncRequest{})
			Expect(err).To(Succeed())
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
			blocker := filepath.Join(workspaceSyncTempDir(), "not-a-dir")
			Expect(os.WriteFile(blocker, []byte("x"), 0o644)).To(Succeed())

			service, err := NewService(ServiceOptions{
				Enabled: true,
				DataDir: filepath.Join(blocker, "data"),
				RootDir: filepath.Join(blocker, "root"),
				Tree:    &fakeTreeReconstructor{},
			})

			Expect(service).To(BeNil())
			pathErr, err := pathErrorResult(err)
			Expect(err).To(Succeed())
			Expect(pathErr.Path).To(Equal(blocker))
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
			Expect(service.status.PendingEventCount).To(BeZero())
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

			err := capturedMarkdownRequiresMetadataWritebackResult(context.Background(), nil, "metadata")
			Expect(err).To(MatchError(errCapturedMarkdownAlreadyCanonical))

			err = capturedMarkdownRequiresMetadataWritebackResult(context.Background(), store, "")
			Expect(err).To(MatchError(errCapturedMarkdownAlreadyCanonical))

			err = capturedMarkdownRequiresMetadataWritebackResult(context.Background(), store, "metadata")
			Expect(err).To(Succeed())

			err = capturedMarkdownRequiresMetadataWritebackResult(context.Background(), &fakeRevisionStore{changedContentsErr: errAdditionalChangedContentsFailed}, "metadata")
			Expect(err).To(MatchError(errAdditionalChangedContentsFailed))

			err = capturedMarkdownRequiresMetadataWritebackResult(context.Background(), store, "parse-error")
			Expect(err).To(MatchError(markdown.ErrMetadataParse))
		})

		It("derives current and historical markdown paths from source metadata and route fallbacks", func() {
			rootDir := filepath.Join(workspaceSyncTempDir(), "workspace")
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
			_, err := currentWorkspaceMarkdownPathByRouteResult(service, nil)
			Expect(err).To(MatchError(errChangedContentMissing))
			Expect(service.currentSectionContentPath("missing", "fallback.md")).To(Equal("fallback.md"))

			files := map[string]string{
				"docs/Page.MD": workspaceSyncEdgeMarkdown("page-1", "Historical Title"),
				"notes.txt":    "ignored",
				"other.md":     "not markdown: [",
			}
			result, err := changedContentForPageAtCommitResult(rootDir, page, "missing.md", files)
			Expect(err).To(Succeed())
			Expect(result).To(gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
				"RelPath": Equal("docs/Page.MD"),
				"Content": ContainSubstring("Historical Title"),
			}))
			Expect(sortedMarkdownPaths(files)).To(Equal([]string{"docs/Page.MD", "other.md"}))
			Expect(markdownPathMatchesPageRouteResult(rootDir, nil, "docs/Page.MD")).To(MatchError(errMarkdownPathMissesRoute))
			id, err := leafWikiIDFromContentResult(workspaceSyncEdgeMarkdown("different-page", "Different"))
			Expect(err).To(Succeed())
			Expect(id).NotTo(Equal(page.ID))
			_, err = leafWikiIDFromContentResult("---\nleafwiki_id: [broken\n---\n# Broken\n")
			Expect(err).To(MatchError(errLeafWikiIDMissing))

			commit := gitrevisions.Commit{Hash: CommitHashFromString("hash-1"), Message: "", AuthorID: gitrevisions.ParseActorID(""), CreatedAt: time.Date(2026, 6, 27, 12, 0, 0, 0, time.UTC)}
			rev := revisionForPageContent(rootDir, page, commit, "docs/Page.MD", files["docs/Page.MD"])
			Expect(rev).To(gstruct.PointTo(gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
				"AuthorID": Equal(PublicEditorActor().ID.MetadataValue()),
				"Summary":  Equal("workspace sync"),
				"Title":    Equal("Historical Title"),
			})))
		})

		It("extracts markdown paths from quoted error tokens without duplicates", func() {
			rootDir := filepath.Join(workspaceSyncTempDir(), "workspace")
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

func closableFileWatcherResult(watcher fileWatcher) (closableFileWatcher, error) {
	GinkgoHelper()

	closer, ok := watcher.(closableFileWatcher)
	if !ok {
		return nil, errWatcherNotClosable
	}
	return closer, nil
}

func pathErrorResult(err error) (*os.PathError, error) {
	GinkgoHelper()

	var pathErr *os.PathError
	if !errors.As(err, &pathErr) {
		return nil, errPathErrorMissing
	}
	return pathErr, nil
}

func capturedMarkdownRequiresMetadataWritebackResult(ctx context.Context, store revisionStore, commitHash CommitHash) error {
	GinkgoHelper()

	requires, err := capturedMarkdownRequiresMetadataWriteback(ctx, store, commitHash)
	if err != nil {
		return err
	}
	if !requires {
		return errCapturedMarkdownAlreadyCanonical
	}
	return nil
}

func currentWorkspaceMarkdownPathByRouteResult(service *Service, page *tree.Page) (string, error) {
	GinkgoHelper()

	path, ok := service.currentWorkspaceMarkdownPathByRoute(page)
	if !ok {
		return "", errChangedContentMissing
	}
	return path, nil
}

type changedContentResult struct {
	Content string
	RelPath string
}

func changedContentForPageAtCommitResult(rootDir string, page *tree.Page, preferredPath string, files map[string]string) (changedContentResult, error) {
	GinkgoHelper()

	content, relPath, ok := changedContentForPageAtCommit(rootDir, page, preferredPath, files)
	if !ok {
		return changedContentResult{}, errChangedContentMissing
	}
	return changedContentResult{
		Content: content,
		RelPath: relPath,
	}, nil
}

func markdownPathMatchesPageRouteResult(rootDir string, page *tree.Page, relPath string) error {
	GinkgoHelper()

	if markdownPathMatchesPageRoute(rootDir, page, relPath) {
		return errMarkdownPathMatchesRoute
	}
	return errMarkdownPathMissesRoute
}

func leafWikiIDFromContentResult(content string) (tree.PageID, error) {
	GinkgoHelper()

	id, ok := leafWikiIDFromContent(content)
	if !ok {
		return "", errLeafWikiIDMissing
	}
	return id, nil
}

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
