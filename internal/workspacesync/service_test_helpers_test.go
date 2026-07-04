package workspacesync

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/types"

	"github.com/perber/wiki/internal/core/revision"
	"github.com/perber/wiki/internal/core/tree"
	"github.com/perber/wiki/internal/workspacesync/gitrevisions"
)

func workspaceSyncTempDir() string {
	GinkgoHelper()

	dir, err := os.MkdirTemp("", "leafwiki-workspacesync-*")
	Expect(err).To(Succeed())
	DeferCleanup(os.RemoveAll, dir)
	return dir
}

func writeMarkdownFile(path string, content string) {
	GinkgoHelper()
	Expect(os.MkdirAll(filepath.Dir(path), 0o755)).To(Succeed())
	Expect(os.WriteFile(path, []byte(content), 0o644)).To(Succeed())
}

func readFileStringGinkgo(path string) string {
	GinkgoHelper()

	raw, err := os.ReadFile(path)
	Expect(err).To(Succeed())
	return string(raw)
}

func mustGetOnlyPageGinkgo(treeService *tree.TreeService) *tree.Page {
	GinkgoHelper()

	var ids []tree.PageID
	err := treeService.WalkNodes(func(id tree.PageID) error {
		ids = append(ids, id)
		return nil
	})
	Expect(err).To(Succeed())
	Expect(ids).To(HaveLen(1))

	page, err := treeService.GetPage(ids[0])
	Expect(err).To(Succeed())
	return page
}

var (
	errCanonicalMigrationCompletedWithoutFailure = errors.New("canonical migration completed without write failure")
	errCanonicalMigrationCommittedBeforeRollback = errors.New("canonical migration committed before rollback")
	errAtomicRewriteFailureNotLinkError          = errors.New("atomic rewrite failure was not a link error")
)

func canonicalMigrationFailsWithoutCommit(service *Service) error {
	GinkgoHelper()

	changed, err := service.migrateCanonicalMarkdownLinksLocked()
	if err == nil {
		return errCanonicalMigrationCompletedWithoutFailure
	}
	if changed {
		return errCanonicalMigrationCommittedBeforeRollback
	}
	return nil
}

func atomicRewriteLinkError(err error) (*os.LinkError, error) {
	GinkgoHelper()

	var linkErr *os.LinkError
	if errors.As(err, &linkErr) {
		return linkErr, nil
	}
	return nil, errAtomicRewriteFailureNotLinkError
}

func revisionIDs(revisions []*revision.Revision) []string {
	ids := make([]string, 0, len(revisions))
	for _, rev := range revisions {
		if rev == nil {
			ids = append(ids, "")
			continue
		}
		ids = append(ids, rev.ID.CommitID())
	}
	return ids
}

type fakeTreeReconstructor struct {
	reconstructs int32
	err          error
	errs         []error
}

func (f *fakeTreeReconstructor) ReconstructTreeFromFS() error {
	call := int(atomic.AddInt32(&f.reconstructs, 1))
	if call <= len(f.errs) {
		return f.errs[call-1]
	}
	return f.err
}

func (f *fakeTreeReconstructor) reconstructCount() int {
	return int(atomic.LoadInt32(&f.reconstructs))
}

type fakeRevisionStore struct {
	capture                           *gitrevisions.Commit
	captureErr                        error
	captureErrCall                    int
	amendErr                          error
	listErr                           error
	getCommitErr                      error
	restoreWorkspaceErr               error
	restoreDocumentContentErr         error
	captureCalls                      int
	amendCalls                        int
	commits                           []gitrevisions.Commit
	filesAt                           map[CommitHash]map[string]string
	changedPaths                      map[CommitHash][]string
	changedPathsErr                   error
	changedPathsErrByHash             map[CommitHash]error
	scannedCommits                    int
	filesAtCalls                      int
	restoreDocumentToPathCalls        int
	restoreDocumentContentToPathCalls int
	restoredContent                   string
	changedPathsStarted               chan struct{}
	unblockChangedPaths               chan struct{}
	changedContents                   map[CommitHash]map[string]string
	changedContentsErr                error
	changedContentsStarted            chan struct{}
	unblockChangedContents            chan struct{}
	captureRequests                   []gitrevisions.CommitRequest
	amendRequests                     []gitrevisions.CommitRequest
	restoreWorkspaceRequests          []gitrevisions.CommitRequest
	restoreDocumentContentRequests    []gitrevisions.CommitRequest
}

type fakeRevisionStoreRestoreContentState struct {
	FilesAtCalls                      int
	RestoreDocumentContentToPathCalls int
	RestoredContent                   string
}

func fakeRevisionStoreRestoreContentStateFor(store *fakeRevisionStore) fakeRevisionStoreRestoreContentState {
	GinkgoHelper()

	return fakeRevisionStoreRestoreContentState{
		FilesAtCalls:                      store.filesAtCalls,
		RestoreDocumentContentToPathCalls: store.restoreDocumentContentToPathCalls,
		RestoredContent:                   store.restoredContent,
	}
}

func (f *fakeRevisionStore) Capture(_ context.Context, req gitrevisions.CommitRequest) (*gitrevisions.Commit, error) {
	f.captureCalls++
	f.captureRequests = append(f.captureRequests, req)
	if f.captureErr != nil && (f.captureErrCall == 0 || f.captureCalls == f.captureErrCall) {
		return nil, f.captureErr
	}
	if f.capture == nil {
		return &gitrevisions.Commit{}, nil
	}
	commit := *f.capture
	if commit.Hash != "" {
		commit.Created = true
	}
	return &commit, nil
}

func (f *fakeRevisionStore) Amend(_ context.Context, req gitrevisions.CommitRequest) (*gitrevisions.Commit, error) {
	f.amendCalls++
	f.amendRequests = append(f.amendRequests, req)
	if f.amendErr != nil {
		return nil, f.amendErr
	}
	return f.capture, nil
}

func (f *fakeRevisionStore) ListCommits(_ context.Context, req gitrevisions.ListRequest) ([]gitrevisions.Commit, error) {
	if f.listErr != nil {
		return nil, f.listErr
	}
	if req.Limit <= 0 || req.Limit >= len(f.commits) {
		return f.commits, nil
	}
	return f.commits[:req.Limit], nil
}

func (f *fakeRevisionStore) ForEachCommit(_ context.Context, visit func(gitrevisions.Commit) (bool, error)) error {
	for _, commit := range f.commits {
		f.scannedCommits++
		keepGoing, err := visit(commit)
		if err != nil {
			return err
		}
		if !keepGoing {
			return nil
		}
	}
	return nil
}

func (f *fakeRevisionStore) ChangedMarkdownPaths(_ context.Context, hash CommitHash) ([]string, error) {
	if f.changedPathsStarted != nil {
		close(f.changedPathsStarted)
		f.changedPathsStarted = nil
	}
	if f.unblockChangedPaths != nil {
		<-f.unblockChangedPaths
	}
	if f.changedPathsErr != nil {
		return nil, f.changedPathsErr
	}
	if f.changedPathsErrByHash != nil {
		if err := f.changedPathsErrByHash[hash]; err != nil {
			return nil, err
		}
	}
	return f.changedPaths[hash], nil
}

func (f *fakeRevisionStore) ChangedMarkdownContents(_ context.Context, hash CommitHash) (map[string]string, error) {
	if f.changedContentsStarted != nil {
		close(f.changedContentsStarted)
		f.changedContentsStarted = nil
	}
	if f.unblockChangedContents != nil {
		<-f.unblockChangedContents
	}
	if f.changedContentsErr != nil {
		return nil, f.changedContentsErr
	}
	if f.changedContents != nil {
		return f.changedContents[hash], nil
	}
	contents := make(map[string]string)
	files := f.filesAt[hash]
	for _, path := range f.changedPaths[hash] {
		if content, ok := files[path]; ok {
			contents[path] = content
		}
	}
	return contents, nil
}

func (f *fakeRevisionStore) GetCommit(_ context.Context, hash CommitHash) (gitrevisions.Commit, error) {
	if f.getCommitErr != nil {
		return gitrevisions.Commit{}, f.getCommitErr
	}
	for _, commit := range f.commits {
		if CommitHashFromString(commit.Hash) == hash {
			return commit, nil
		}
	}
	if f.capture != nil && CommitHashFromString(f.capture.Hash) == hash {
		return *f.capture, nil
	}
	return gitrevisions.Commit{}, errors.New("commit not found")
}

func (f *fakeRevisionStore) RestoreWorkspace(_ context.Context, _ CommitHash, req gitrevisions.CommitRequest) (*gitrevisions.Commit, error) {
	f.restoreWorkspaceRequests = append(f.restoreWorkspaceRequests, req)
	if f.restoreWorkspaceErr != nil {
		return nil, f.restoreWorkspaceErr
	}
	return f.capture, nil
}

func (f *fakeRevisionStore) RestoreDocument(context.Context, string, CommitHash, gitrevisions.CommitRequest) (*gitrevisions.Commit, error) {
	return f.capture, nil
}

func (f *fakeRevisionStore) RestoreDocumentToPath(context.Context, string, string, CommitHash, gitrevisions.CommitRequest) (*gitrevisions.Commit, error) {
	f.restoreDocumentToPathCalls++
	return f.capture, nil
}

func (f *fakeRevisionStore) RestoreDocumentContentToPath(_ context.Context, _ string, content string, req gitrevisions.CommitRequest) (*gitrevisions.Commit, error) {
	f.restoreDocumentContentToPathCalls++
	f.restoreDocumentContentRequests = append(f.restoreDocumentContentRequests, req)
	f.restoredContent = content
	if f.restoreDocumentContentErr != nil {
		return nil, f.restoreDocumentContentErr
	}
	return f.capture, nil
}

func (f *fakeRevisionStore) FilesAt(_ context.Context, hash CommitHash) (map[string]string, error) {
	f.filesAtCalls++
	if f.filesAt == nil {
		return map[string]string{}, nil
	}
	return f.filesAt[hash], nil
}

type fakeWatcher struct {
	events   chan watcherEvent
	dropped  chan watcherEvent
	watchErr error
	closes   int32
}

func newFakeWatcher() *fakeWatcher {
	return &fakeWatcher{
		events:  make(chan watcherEvent, 10),
		dropped: make(chan watcherEvent, 10),
	}
}

func (f *fakeWatcher) Watch(ctx context.Context) error {
	if f.watchErr != nil {
		return f.watchErr
	}
	<-ctx.Done()
	return ctx.Err()
}

func (f *fakeWatcher) Events() <-chan watcherEvent {
	return f.events
}

func (f *fakeWatcher) Dropped() <-chan watcherEvent {
	return f.dropped
}

func (f *fakeWatcher) Close() {
	atomic.AddInt32(&f.closes, 1)
}

func (f *fakeWatcher) closeCount() int {
	return int(atomic.LoadInt32(&f.closes))
}

func closeOnce(ch chan struct{}) func() {
	var once sync.Once
	return func() {
		once.Do(func() {
			close(ch)
		})
	}
}

type workspaceSyncStartupLogRecords struct {
	Records []workspaceSyncStartupLogRecord
}

type workspaceSyncStartupLogRecord struct {
	Level   slog.Level
	Message string
	Attrs   []slog.Attr
}

func (r *workspaceSyncStartupLogRecords) Enabled(context.Context, slog.Level) bool {
	return true
}

func (r *workspaceSyncStartupLogRecords) Handle(_ context.Context, record slog.Record) error {
	attrs := make([]slog.Attr, 0, record.NumAttrs())
	record.Attrs(func(attr slog.Attr) bool {
		attrs = append(attrs, attr)
		return true
	})
	r.Records = append(r.Records, workspaceSyncStartupLogRecord{
		Level:   record.Level,
		Message: record.Message,
		Attrs:   attrs,
	})
	return nil
}

func (r *workspaceSyncStartupLogRecords) WithAttrs([]slog.Attr) slog.Handler {
	return r
}

func (r *workspaceSyncStartupLogRecords) WithGroup(string) slog.Handler {
	return r
}

type startupPhaseLifecycle uint8

const (
	startupPhaseStarted startupPhaseLifecycle = iota
	startupPhaseCompleted
)

type startupPhaseLogObservation struct {
	Phase     string
	Lifecycle startupPhaseLifecycle
}

func reportStartupPhaseLifecycle(phases ...string) types.GomegaMatcher {
	expected := make([]startupPhaseLogObservation, 0, len(phases)*2)
	for _, phase := range phases {
		expected = append(expected,
			startupPhaseLogObservation{Phase: phase, Lifecycle: startupPhaseStarted},
			startupPhaseLogObservation{Phase: phase, Lifecycle: startupPhaseCompleted},
		)
	}
	return WithTransform(func(records *workspaceSyncStartupLogRecords) []startupPhaseLogObservation {
		GinkgoHelper()
		observed := make([]startupPhaseLogObservation, 0, len(records.Records))
		for _, record := range records.Records {
			phase, ok := record.startupPhase()
			if !ok || record.Level != slog.LevelInfo {
				continue
			}
			observed = append(observed, startupPhaseLogObservation{
				Phase:     phase,
				Lifecycle: record.startupPhaseLifecycle(),
			})
		}
		return observed
	}, ConsistOf(expected))
}

func (r workspaceSyncStartupLogRecord) startupPhase() (string, bool) {
	for _, attr := range r.Attrs {
		if attr.Key == "phase" {
			return attr.Value.String(), true
		}
	}
	return "", false
}

func (r workspaceSyncStartupLogRecord) startupPhaseLifecycle() startupPhaseLifecycle {
	for _, attr := range r.Attrs {
		if attr.Key == "duration" {
			return startupPhaseCompleted
		}
	}
	return startupPhaseStarted
}
