package workspacesync

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log/slog"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/perber/wiki/internal/core/markdown"
	"github.com/perber/wiki/internal/core/markdownlinks"
	wikivalidation "github.com/perber/wiki/internal/core/markdownvalidation"
	"github.com/perber/wiki/internal/core/revision"
	sharederrors "github.com/perber/wiki/internal/core/shared/errors"
	"github.com/perber/wiki/internal/core/tree"
	"github.com/perber/wiki/internal/workspacesync/gitrevisions"
)

type Reason = gitrevisions.Reason

const (
	ReasonStartup         = gitrevisions.ReasonStartup
	ReasonWatcher         = gitrevisions.ReasonWatcher
	ReasonExplicit        = gitrevisions.ReasonExplicit
	ReasonExplicitRefresh = gitrevisions.ReasonExplicitRefresh
	ReasonWebWrite        = gitrevisions.ReasonWebWrite
	ReasonRestore         = gitrevisions.ReasonRestore
)

type Source = gitrevisions.Source

const (
	SourceFilesystem = gitrevisions.SourceFilesystem
	SourceWeb        = gitrevisions.SourceWeb
	SourceMCP        = gitrevisions.SourceMCP
	SourceSystem     = gitrevisions.SourceSystem
	SourceUnknown    = gitrevisions.SourceUnknown
)

const watcherBatchDebounce = 250 * time.Millisecond

var (
	canonicalMarkdownRewriteWriter    = writeCanonicalMarkdownRewritesAtomically
	workspacesyncOSStat               = os.Stat
	workspacesyncNewMarkdownLinkIndex = markdownlinks.NewIndexFromRootWithOptions
	workspacesyncWalkDir              = filepath.WalkDir
	workspacesyncRel                  = filepath.Rel
	workspacesyncReadFile             = os.ReadFile
	workspacesyncCreateTemp           = func(dir string, pattern string) (workspacesyncTempFile, error) {
		return os.CreateTemp(dir, pattern)
	}
	workspacesyncRemove    = os.Remove
	workspacesyncRename    = os.Rename
	workspacesyncChmod     = os.Chmod
	workspacesyncWriteFile = os.WriteFile
)

type workspacesyncTempFile interface {
	Name() string
	Write([]byte) (int, error)
	Close() error
}

type Actor = gitrevisions.Actor
type ActorID = gitrevisions.ActorID

func NewActorIDUnchecked(raw string) ActorID {
	return gitrevisions.NewActorIDUnchecked(raw)
}

type treeReconstructor interface {
	ReconstructTreeFromFS() error
}

type revisionStore interface {
	Capture(context.Context, gitrevisions.CommitRequest) (*gitrevisions.Commit, error)
	Amend(context.Context, gitrevisions.CommitRequest) (*gitrevisions.Commit, error)
	ListCommits(context.Context, gitrevisions.ListRequest) ([]gitrevisions.Commit, error)
	ForEachCommit(context.Context, func(gitrevisions.Commit) (bool, error)) error
	ChangedMarkdownPaths(context.Context, CommitHash) ([]string, error)
	ChangedMarkdownContents(context.Context, CommitHash) (map[string]string, error)
	GetCommit(context.Context, CommitHash) (gitrevisions.Commit, error)
	RestoreWorkspace(context.Context, CommitHash, gitrevisions.CommitRequest) (*gitrevisions.Commit, error)
	RestoreDocument(ctx context.Context, relFile string, commitID CommitHash, req gitrevisions.CommitRequest) (*gitrevisions.Commit, error)
	RestoreDocumentToPath(ctx context.Context, targetFile string, sourceFile string, commitID CommitHash, req gitrevisions.CommitRequest) (*gitrevisions.Commit, error)
	RestoreDocumentContentToPath(ctx context.Context, targetFile string, content string, req gitrevisions.CommitRequest) (*gitrevisions.Commit, error)
	FilesAt(context.Context, CommitHash) (map[string]string, error)
}

type watcherEvent struct {
	Path    string
	Dropped bool
}

type fileWatcher interface {
	Watch(context.Context) error
	Events() <-chan watcherEvent
	Dropped() <-chan watcherEvent
}

type closableFileWatcher interface {
	Close()
}

type watcherFactory func(rootDir string) (fileWatcher, error)

type ServiceOptions struct {
	Enabled                bool
	DataDir                string
	RootDir                string
	MarkdownLinkRootPrefix string
	Tree                   treeReconstructor
	Store                  revisionStore
	WatcherFactory         watcherFactory
	AfterSync              func() error
	Log                    *slog.Logger
}

type SyncRequest struct {
	Reason           Reason
	Source           Source
	Actor            Actor
	AdditionalActors []Actor
}

type ValidationError struct {
	Code      wikivalidation.IssueCode     `json:"code,omitempty"`
	MessageID sharederrors.MessageID       `json:"messageId,omitempty"`
	Path      string                       `json:"path"`
	Message   string                       `json:"message"`
	Severity  wikivalidation.IssueSeverity `json:"severity,omitempty"`
}

type SyncStatus struct {
	Enabled                    bool
	WatcherEnabled             bool
	WatcherRunning             bool
	PendingEventCount          int
	LastSyncTime               time.Time
	LastError                  string
	LastCommitHash             CommitHash
	RecentChangedMarkdownPaths []string
	ValidationErrors           []ValidationError
}

type Snapshot struct {
	ID                   CommitHash `json:"id"`
	Message              string     `json:"message,omitempty"`
	AuthorID             string     `json:"authorId,omitempty"`
	AuthorName           string     `json:"author,omitempty"`
	AuthorEmail          string     `json:"authorEmail,omitempty"`
	CreatedAt            time.Time  `json:"createdAt,omitempty"`
	Source               string     `json:"source,omitempty"`
	Reason               string     `json:"reason,omitempty"`
	ChangedMarkdownCount int        `json:"changedMarkdownCount,omitempty"`
	ChangedMarkdownPaths []string   `json:"changedMarkdownPaths,omitempty"`
}

type SnapshotList struct {
	Snapshots  []Snapshot
	NextCursor CommitHash
}

type PageRevisionList struct {
	Revisions  []*revision.Revision
	NextCursor string
}

type Service struct {
	enabled                bool
	rootDir                string
	markdownLinkRootPrefix string
	tree                   treeReconstructor
	store                  revisionStore
	watcherFactory         watcherFactory
	afterSync              func() error
	log                    *slog.Logger

	mu            sync.Mutex
	storeMu       sync.Mutex
	status        SyncStatus
	watcher       fileWatcher
	watcherCancel context.CancelFunc
	watcherDone   chan struct{}
}

func PublicEditorActor() Actor {
	return gitrevisions.PublicEditorActor()
}

func NewService(options ServiceOptions) (*Service, error) {
	status := SyncStatus{Enabled: options.Enabled}
	logger := options.Log
	if logger == nil {
		logger = slog.Default().With("component", "WorkspaceSync")
	}
	service := &Service{
		enabled:                options.Enabled,
		rootDir:                strings.TrimSpace(options.RootDir),
		markdownLinkRootPrefix: options.MarkdownLinkRootPrefix,
		tree:                   options.Tree,
		watcherFactory:         options.WatcherFactory,
		afterSync:              options.AfterSync,
		log:                    logger,
		status:                 status,
	}
	if !options.Enabled {
		return service, nil
	}
	if options.Tree == nil {
		return nil, fmt.Errorf("tree service is required")
	}
	if options.Store != nil {
		service.store = options.Store
	} else {
		store, err := gitrevisions.Open(gitrevisions.StoreOptions{
			DataDir: options.DataDir,
			RootDir: options.RootDir,
		})
		if err != nil {
			return nil, err
		}
		service.store = store
	}
	return service, nil
}

func (s *Service) SetAfterSync(fn func() error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.afterSync = fn
}

func (s *Service) StartWatcher(ctx context.Context) error {
	if !s.enabled {
		return nil
	}
	factory := s.watcherFactory
	if factory == nil {
		factory = newFileWatcher
	}
	watcher, err := factory(s.rootDir)
	if err != nil {
		s.mu.Lock()
		s.status.WatcherEnabled = true
		s.status.WatcherRunning = false
		s.status.LastError = err.Error()
		s.mu.Unlock()
		return err
	}
	watchCtx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	s.mu.Lock()
	s.status.WatcherEnabled = true
	s.status.WatcherRunning = true
	s.watcher = watcher
	s.watcherCancel = cancel
	s.watcherDone = done
	s.mu.Unlock()

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		if err := watcher.Watch(watchCtx); err != nil && watchCtx.Err() == nil {
			s.mu.Lock()
			s.status.LastError = err.Error()
			s.mu.Unlock()
			if _, syncErr := s.SyncNow(context.Background(), SyncRequest{
				Reason: ReasonWatcher,
				Source: SourceFilesystem,
				Actor:  PublicEditorActor(),
			}); syncErr != nil {
				s.mu.Lock()
				s.status.LastError = err.Error() + ": " + syncErr.Error()
				s.mu.Unlock()
			} else {
				s.mu.Lock()
				s.status.LastError = err.Error()
				s.mu.Unlock()
			}
		}
		s.mu.Lock()
		if s.watcher == watcher {
			s.watcher = nil
		}
		s.status.WatcherRunning = false
		s.mu.Unlock()
	}()
	go func() {
		defer wg.Done()
		s.consumeWatcherEvents(watchCtx, watcher)
	}()
	go func() {
		wg.Wait()
		close(done)
	}()
	return nil
}

func (s *Service) StopWatcher() {
	s.mu.Lock()
	cancel := s.watcherCancel
	watcher := s.watcher
	done := s.watcherDone
	s.watcher = nil
	s.watcherCancel = nil
	s.watcherDone = nil
	s.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	if closer, ok := watcher.(closableFileWatcher); ok {
		closer.Close()
	}
	if done == nil {
		return
	}
	select {
	case <-done:
	case <-time.After(2 * time.Second):
	}
}

func (s *Service) consumeWatcherEvents(ctx context.Context, watcher fileWatcher) {
	var timer *time.Timer
	var timerC <-chan time.Time
	pendingCount := 0
	dropped := false
	droppedPath := ""

	stopTimer := func() {
		if timer == nil {
			return
		}
		stopWorkspacesyncTimer(timer)
		timer = nil
		timerC = nil
	}
	queue := func(event watcherEvent) {
		relPath, managed := managedMarkdownEventPath(s.rootDir, event.Path)
		if !managed && !event.Dropped {
			return
		}
		pendingCount++
		if event.Dropped {
			dropped = true
			if relPath != "" {
				droppedPath = relPath
			}
		}
		s.mu.Lock()
		s.status.PendingEventCount++
		s.mu.Unlock()
		if timer == nil {
			timer = time.NewTimer(watcherBatchDebounce)
			timerC = timer.C
			return
		}
		stopWorkspacesyncTimer(timer)
		timer.Reset(watcherBatchDebounce)
	}
	flush := func() {
		if pendingCount == 0 {
			return
		}
		stopTimer()
		s.handleWatcherBatch(ctx, pendingCount, dropped, droppedPath)
		pendingCount = 0
		dropped = false
		droppedPath = ""
	}

	defer stopTimer()
	for {
		select {
		case <-ctx.Done():
			return
		case <-timerC:
			flush()
		case event, ok := <-watcher.Events():
			if !ok {
				flush()
				return
			}
			queue(event)
		case event, ok := <-watcher.Dropped():
			if !ok {
				flush()
				return
			}
			event.Dropped = true
			queue(event)
		}
	}
}

func stopWorkspacesyncTimer(timer *time.Timer) {
	if !timer.Stop() {
		drainWorkspacesyncTimer(timer)
	}
}

func drainWorkspacesyncTimer(timer *time.Timer) {
	select {
	case <-timer.C:
	default:
	}
}

func (s *Service) handleWatcherBatch(ctx context.Context, eventCount int, dropped bool, droppedPath string) {
	_, err := s.SyncNow(ctx, SyncRequest{
		Reason: ReasonWatcher,
		Source: SourceFilesystem,
		Actor:  PublicEditorActor(),
	})

	s.mu.Lock()
	s.status.PendingEventCount -= eventCount
	if s.status.PendingEventCount < 0 {
		s.status.PendingEventCount = 0
	}
	if dropped {
		if droppedPath == "" {
			s.status.LastError = "watcher dropped events"
		} else {
			s.status.LastError = "watcher dropped events for " + droppedPath
		}
	} else if err != nil {
		s.status.LastError = err.Error()
	}
	s.mu.Unlock()
}

func (s *Service) shouldLogStartupSync(req SyncRequest) bool {
	return req.Reason == ReasonStartup && s.log != nil
}

func (s *Service) logStartupSyncStarted(enabled bool, req SyncRequest) time.Time {
	if !enabled {
		return time.Time{}
	}
	started := time.Now()
	s.log.Info("workspace sync startup started",
		"reason", string(req.Reason),
		"source", string(req.Source),
		"root_dir", s.rootDir,
	)
	return started
}

func (s *Service) logStartupSyncCompleted(enabled bool, started time.Time, status SyncStatus) {
	if !enabled {
		return
	}
	s.log.Info("workspace sync startup completed",
		"duration", time.Since(started),
		"last_commit_hash", status.LastCommitHash,
		"last_error", status.LastError,
		"validation_errors", len(status.ValidationErrors),
	)
}

func (s *Service) logStartupSyncFailed(enabled bool, started time.Time, err error) {
	if !enabled {
		return
	}
	s.log.Error("workspace sync startup failed",
		"duration", time.Since(started),
		"error", err,
	)
}

func (s *Service) logStartupPhaseStarted(enabled bool, phase string, attrs ...any) time.Time {
	if !enabled {
		return time.Time{}
	}
	started := time.Now()
	args := append([]any{"phase", phase}, attrs...)
	s.log.Info("workspace sync startup phase started", args...)
	return started
}

func (s *Service) logStartupPhaseCompleted(enabled bool, phase string, started time.Time, attrs ...any) {
	if !enabled {
		return
	}
	args := append([]any{
		"phase", phase,
		"duration", time.Since(started),
	}, attrs...)
	s.log.Info("workspace sync startup phase completed", args...)
}

func (s *Service) logStartupPhaseFailed(enabled bool, phase string, started time.Time, err error) {
	if !enabled {
		return
	}
	s.log.Error("workspace sync startup phase failed",
		"phase", phase,
		"duration", time.Since(started),
		"error", err,
	)
}

func (s *Service) SyncNow(ctx context.Context, req SyncRequest) (SyncStatus, error) {
	s.mu.Lock()
	if !s.enabled {
		defer s.mu.Unlock()
		return s.status, nil
	}
	store := s.store
	s.mu.Unlock()

	logStartup := s.shouldLogStartupSync(req)
	startupStarted := s.logStartupSyncStarted(logStartup, req)
	commitReq := gitrevisions.CommitRequest{
		Reason:           req.Reason,
		Source:           req.Source,
		Actor:            req.Actor,
		AdditionalActors: req.AdditionalActors,
	}
	phaseStarted := s.logStartupPhaseStarted(logStartup, "capture_snapshot")
	s.storeMu.Lock()
	commit, err := store.Capture(ctx, commitReq)
	s.storeMu.Unlock()
	if err != nil {
		s.logStartupPhaseFailed(logStartup, "capture_snapshot", phaseStarted, err)
	} else {
		s.logStartupPhaseCompleted(logStartup, "capture_snapshot", phaseStarted,
			"commit_hash", commit.Hash,
			"changed_markdown_count", commit.ChangedMarkdownCount,
			"changed_markdown_paths", len(commit.ChangedMarkdownPaths),
		)
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if err != nil {
		s.status.LastError = err.Error()
		s.status.LastSyncTime = time.Now().UTC()
		s.logStartupSyncFailed(logStartup, startupStarted, err)
		return s.status, err
	}
	s.status.LastCommitHash = CommitHashFromString(commit.Hash)
	s.status.LastSyncTime = time.Now().UTC()
	s.status.LastError = ""
	s.status.ValidationErrors = nil
	s.recordChangedMarkdownPaths(commit.ChangedMarkdownPaths)

	phaseStarted = s.logStartupPhaseStarted(logStartup, "reconstruct_tree")
	if err := s.tree.ReconstructTreeFromFS(); err != nil {
		s.status.LastError = err.Error()
		s.status.ValidationErrors = s.validationErrorsFromError(err)
		s.logStartupPhaseFailed(logStartup, "reconstruct_tree", phaseStarted, err)
		s.logStartupSyncCompleted(logStartup, startupStarted, s.status)
		return s.status, nil
	}
	s.logStartupPhaseCompleted(logStartup, "reconstruct_tree", phaseStarted)
	phaseStarted = s.logStartupPhaseStarted(logStartup, "canonical_link_migration")
	migratedCanonicalLinks, rollbackCanonicalLinks, err := s.migrateCanonicalMarkdownLinksLockedWithRollback()
	if err != nil {
		s.status.LastError = err.Error()
		s.logStartupPhaseFailed(logStartup, "canonical_link_migration", phaseStarted, err)
		s.logStartupSyncFailed(logStartup, startupStarted, err)
		return s.status, err
	}
	s.logStartupPhaseCompleted(logStartup, "canonical_link_migration", phaseStarted,
		"migrated", migratedCanonicalLinks,
		"validation_errors", len(s.status.ValidationErrors),
	)
	if migratedCanonicalLinks {
		phaseStarted = s.logStartupPhaseStarted(logStartup, "reconstruct_tree_after_canonical_link_migration")
		if err := s.tree.ReconstructTreeFromFS(); err != nil {
			s.status.LastError = err.Error()
			s.status.ValidationErrors = s.validationErrorsFromError(err)
			s.rollbackCanonicalMarkdownMigrationLocked(rollbackCanonicalLinks)
			s.logStartupPhaseFailed(logStartup, "reconstruct_tree_after_canonical_link_migration", phaseStarted, err)
			s.logStartupSyncCompleted(logStartup, startupStarted, s.status)
			return s.status, nil
		}
		s.logStartupPhaseCompleted(logStartup, "reconstruct_tree_after_canonical_link_migration", phaseStarted)
	}
	phaseStarted = s.logStartupPhaseStarted(logStartup, "capture_writebacks")
	if err := s.captureWritebacksLocked(ctx, commitReq, commit, !migratedCanonicalLinks); err != nil {
		s.status.LastError = err.Error()
		s.rollbackCanonicalMarkdownMigrationLocked(rollbackCanonicalLinks)
		s.logStartupPhaseFailed(logStartup, "capture_writebacks", phaseStarted, err)
		s.logStartupSyncFailed(logStartup, startupStarted, err)
		return s.status, err
	}
	s.logStartupPhaseCompleted(logStartup, "capture_writebacks", phaseStarted,
		"last_commit_hash", s.status.LastCommitHash,
		"changed_markdown_paths", len(s.status.RecentChangedMarkdownPaths),
	)
	phaseStarted = s.logStartupPhaseStarted(logStartup, "validate_and_after_sync")
	if err := s.validateAndRunAfterSyncLocked(); err != nil {
		s.status.LastError = err.Error()
		s.logStartupPhaseFailed(logStartup, "validate_and_after_sync", phaseStarted, err)
		s.logStartupSyncFailed(logStartup, startupStarted, err)
		return s.status, err
	}
	s.logStartupPhaseCompleted(logStartup, "validate_and_after_sync", phaseStarted,
		"validation_errors", len(s.status.ValidationErrors),
		"last_error", s.status.LastError,
	)
	s.logStartupSyncCompleted(logStartup, startupStarted, s.status)
	return s.status, nil
}

func (s *Service) migrateCanonicalMarkdownLinksLocked() (bool, error) {
	changed, _, err := s.migrateCanonicalMarkdownLinksLockedWithRollback()
	return changed, err
}

func (s *Service) migrateCanonicalMarkdownLinksLockedWithRollback() (bool, func() error, error) {
	if strings.TrimSpace(s.rootDir) == "" {
		return false, nil, nil
	}
	if info, err := workspacesyncOSStat(s.rootDir); err != nil {
		if os.IsNotExist(err) {
			return false, nil, nil
		}
		return false, nil, err
	} else if !info.IsDir() {
		return false, nil, nil
	}
	index, err := workspacesyncNewMarkdownLinkIndex(s.rootDir, markdownlinks.Options{
		MarkdownLinkRootPrefix: s.markdownLinkRootPrefix,
	})
	if err != nil {
		return false, nil, err
	}
	rewrites := make([]canonicalMarkdownRewrite, 0)
	migrationIssues := make([]ValidationError, 0)
	err = workspacesyncWalkDir(s.rootDir, func(filePath string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if filePath == s.rootDir {
			return nil
		}
		if entry.IsDir() {
			if strings.HasPrefix(entry.Name(), ".") {
				return filepath.SkipDir
			}
			return nil
		}
		relPath, err := workspacesyncRel(s.rootDir, filePath)
		if err != nil {
			return err
		}
		relPath = filepath.ToSlash(relPath)
		if !gitrevisions.IsManagedMarkdownRelPath(relPath) {
			return nil
		}
		raw, err := workspacesyncReadFile(filePath)
		if err != nil {
			return err
		}
		result := index.RewriteMarkdown(tree.MarkdownPathFromString(relPath), string(raw))
		migrationIssues = append(migrationIssues, canonicalMigrationValidationErrors(s.rootDir, relPath, result.Issues)...)
		if !result.Changed {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		rewrites = append(rewrites, canonicalMarkdownRewrite{
			Path:     filePath,
			Original: raw,
			Content:  []byte(result.Content),
			Mode:     info.Mode().Perm(),
		})
		return nil
	})
	if err != nil {
		return false, nil, err
	}
	if len(migrationIssues) > 0 {
		s.status.ValidationErrors = append(s.status.ValidationErrors, migrationIssues...)
		if strings.TrimSpace(s.status.LastError) == "" {
			s.status.LastError = migrationIssues[0].Message
		}
	}
	if len(rewrites) == 0 {
		return false, nil, nil
	}
	if err := canonicalMarkdownRewriteWriter(rewrites); err != nil {
		return false, nil, err
	}
	return true, func() error {
		return rollbackCanonicalMarkdownRewrites(rewrites)
	}, nil
}

func canonicalMigrationValidationErrors(rootDir string, relPath string, issues []markdownlinks.Issue) []ValidationError {
	if len(issues) == 0 {
		return nil
	}
	out := make([]ValidationError, 0, len(issues))
	for _, issue := range issues {
		if issue.Code != markdownlinks.IssueCodeAmbiguousLegacyLink {
			continue
		}
		routePath := tree.CleanMarkdownPath(relPath).RoutePath()
		if route, err := tree.MapWorkspaceMarkdownRoute(rootDir, relPath, false); err == nil && !route.Skip {
			routePath = route.RoutePath
		}
		out = append(out, ValidationError{
			Code:      wikivalidation.IssueCodeAmbiguousLegacyLink,
			MessageID: wikivalidation.IssueCodeAmbiguousLegacyLink.MessageID(),
			Path:      routePath.FilesystemPath(),
			Message:   fmt.Sprintf("ambiguous_legacy_link: %s is ambiguous during canonical Markdown link migration", issue.Destination),
			Severity:  wikivalidation.IssueSeverityError,
		})
	}
	return out
}

func (s *Service) rollbackCanonicalMarkdownMigrationLocked(rollback func() error) {
	if rollback == nil {
		return
	}
	previousErr := s.status.LastError
	if err := rollback(); err != nil {
		s.status.LastError = appendSyncError(previousErr, fmt.Sprintf("canonical migration rollback failed: %v", err))
		return
	}
	if s.tree == nil {
		return
	}
	if err := s.tree.ReconstructTreeFromFS(); err != nil {
		s.status.LastError = appendSyncError(previousErr, fmt.Sprintf("reconstruct after canonical migration rollback failed: %v", err))
		s.status.ValidationErrors = s.validationErrorsFromError(err)
		return
	}
	s.status.LastError = previousErr
	s.status.ValidationErrors = nil
}

func appendSyncError(primary string, secondary string) string {
	if strings.TrimSpace(primary) == "" {
		return secondary
	}
	if strings.TrimSpace(secondary) == "" {
		return primary
	}
	return primary + "; " + secondary
}

type canonicalMarkdownRewrite struct {
	Path     string
	Original []byte
	Content  []byte
	Mode     os.FileMode
}

type preparedCanonicalMarkdownRewrite struct {
	canonicalMarkdownRewrite
	TempPath string
}

func writeCanonicalMarkdownRewritesAtomically(rewrites []canonicalMarkdownRewrite) error {
	prepared := make([]preparedCanonicalMarkdownRewrite, 0, len(rewrites))
	for _, rewrite := range rewrites {
		prep, err := prepareCanonicalMarkdownRewrite(rewrite)
		if err != nil {
			cleanupPreparedCanonicalMarkdownRewrites(prepared)
			return err
		}
		prepared = append(prepared, prep)
	}

	committed := make([]canonicalMarkdownRewrite, 0, len(prepared))
	for _, prep := range prepared {
		if err := workspacesyncRename(prep.TempPath, prep.Path); err != nil {
			cleanupPreparedCanonicalMarkdownRewrites(prepared[len(committed):])
			rollbackErr := rollbackCanonicalMarkdownRewrites(committed)
			if rollbackErr != nil {
				return fmt.Errorf("write canonical markdown migration: %w; rollback failed: %v", err, rollbackErr)
			}
			return err
		}
		committed = append(committed, prep.canonicalMarkdownRewrite)
	}
	return nil
}

func prepareCanonicalMarkdownRewrite(rewrite canonicalMarkdownRewrite) (preparedCanonicalMarkdownRewrite, error) {
	temp, err := workspacesyncCreateTemp(filepath.Dir(rewrite.Path), "."+filepath.Base(rewrite.Path)+".*.tmp")
	if err != nil {
		return preparedCanonicalMarkdownRewrite{}, err
	}
	tempPath := temp.Name()
	if _, err := temp.Write(rewrite.Content); err != nil {
		_ = temp.Close()
		_ = workspacesyncRemove(tempPath)
		return preparedCanonicalMarkdownRewrite{}, err
	}
	if err := temp.Close(); err != nil {
		_ = workspacesyncRemove(tempPath)
		return preparedCanonicalMarkdownRewrite{}, err
	}
	if err := workspacesyncChmod(tempPath, rewrite.Mode); err != nil {
		_ = workspacesyncRemove(tempPath)
		return preparedCanonicalMarkdownRewrite{}, err
	}
	return preparedCanonicalMarkdownRewrite{
		canonicalMarkdownRewrite: rewrite,
		TempPath:                 tempPath,
	}, nil
}

func cleanupPreparedCanonicalMarkdownRewrites(prepared []preparedCanonicalMarkdownRewrite) {
	for _, prep := range prepared {
		_ = workspacesyncRemove(prep.TempPath)
	}
}

func rollbackCanonicalMarkdownRewrites(rewrites []canonicalMarkdownRewrite) error {
	for i := len(rewrites) - 1; i >= 0; i-- {
		rewrite := rewrites[i]
		if err := workspacesyncWriteFile(rewrite.Path, rewrite.Original, rewrite.Mode); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) recordChangedMarkdownPaths(paths []string) {
	if len(paths) == 0 {
		return
	}
	seen := make(map[string]struct{}, len(s.status.RecentChangedMarkdownPaths)+len(paths))
	recent := make([]string, 0, len(s.status.RecentChangedMarkdownPaths)+len(paths))
	for _, path := range append(s.status.RecentChangedMarkdownPaths, paths...) {
		trimmed := strings.TrimSpace(path)
		if trimmed == "" {
			continue
		}
		if _, ok := seen[trimmed]; ok {
			continue
		}
		seen[trimmed] = struct{}{}
		recent = append(recent, trimmed)
	}
	if len(recent) > 20 {
		recent = recent[len(recent)-20:]
	}
	s.status.RecentChangedMarkdownPaths = recent
}

func managedMarkdownEventPath(rootDir string, path string) (string, bool) {
	trimmed := strings.TrimSpace(path)
	if trimmed == "" {
		return "", false
	}
	normalizedRoot := strings.TrimSpace(rootDir)
	if normalizedRoot != "" && filepath.IsAbs(trimmed) {
		rel, err := workspacesyncRel(normalizedRoot, trimmed)
		if err == nil && !strings.HasPrefix(rel, "..") && rel != "." {
			trimmed = rel
		}
	}
	rel := filepath.ToSlash(filepath.Clean(trimmed))
	if rel == "." || strings.HasPrefix(rel, "../") || rel == ".." {
		return "", false
	}
	parts := strings.Split(rel, "/")
	for _, part := range parts {
		if part == ".git" || part == ".leafwiki" {
			return "", false
		}
	}
	base := parts[len(parts)-1]
	if strings.HasPrefix(base, ".") {
		return "", false
	}
	if isTemporaryPath(base) {
		return "", false
	}
	if !gitrevisions.IsManagedMarkdownRelPath(rel) {
		return "", false
	}
	return rel, true
}

func isTemporaryPath(base string) bool {
	switch {
	case strings.HasSuffix(base, "~"):
		return true
	case strings.EqualFold(filepath.Ext(base), ".swp"):
		return true
	case strings.EqualFold(filepath.Ext(base), ".tmp"):
		return true
	case strings.HasSuffix(strings.ToLower(base), ".download"):
		return true
	case strings.HasSuffix(strings.ToLower(base), ".partial"):
		return true
	case strings.HasSuffix(strings.ToLower(base), ".crdownload"):
		return true
	default:
		return false
	}
}

func (s *Service) Status() SyncStatus {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.status
}

func (s *Service) ListSnapshots(ctx context.Context, pageSize SnapshotLimit) ([]Snapshot, error) {
	out, err := s.ListSnapshotPage(ctx, CommitHashFromString(""), pageSize)
	if err != nil {
		return nil, err
	}
	return out.Snapshots, nil
}

func (s *Service) ListSnapshotPage(ctx context.Context, cursor CommitHash, pageSize SnapshotLimit) (SnapshotList, error) {
	s.mu.Lock()
	if !s.enabled {
		s.mu.Unlock()
		return SnapshotList{}, nil
	}
	store := s.store
	s.mu.Unlock()

	requestedLimit := int(pageSize)
	if requestedLimit <= 0 {
		requestedLimit = 50
	}
	s.storeMu.Lock()
	commits, err := store.ListCommits(ctx, gitrevisions.ListRequest{
		Cursor: cursor,
		Limit:  requestedLimit + 1,
	})
	s.storeMu.Unlock()
	if err != nil {
		return SnapshotList{}, err
	}
	nextCursor := CommitHashFromString("")
	if len(commits) > requestedLimit {
		nextCursor = CommitHashFromString(commits[requestedLimit-1].Hash)
		commits = commits[:requestedLimit]
	}
	snapshots := make([]Snapshot, 0, len(commits))
	for _, commit := range commits {
		s.storeMu.Lock()
		changedPaths, err := store.ChangedMarkdownPaths(ctx, CommitHashFromString(commit.Hash))
		s.storeMu.Unlock()
		if err != nil {
			return SnapshotList{}, err
		}
		snapshots = append(snapshots, Snapshot{
			ID:                   CommitHashFromString(commit.Hash),
			Message:              commit.Message,
			AuthorID:             commit.AuthorID.String(),
			AuthorName:           commit.AuthorName,
			AuthorEmail:          commit.AuthorEmail,
			CreatedAt:            commit.CreatedAt,
			Source:               string(commit.Source),
			Reason:               string(commit.Reason),
			ChangedMarkdownCount: commit.ChangedMarkdownCount,
			ChangedMarkdownPaths: changedPaths,
		})
	}
	return SnapshotList{Snapshots: snapshots, NextCursor: nextCursor}, nil
}

func (s *Service) RestoreWorkspace(ctx context.Context, commitID CommitHash, actor Actor) (SyncStatus, error) {
	return s.RestoreWorkspaceWithSource(ctx, commitID, actor, SourceSystem)
}

func (s *Service) RestoreWorkspaceWithSource(ctx context.Context, commitID CommitHash, actor Actor, source Source) (SyncStatus, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.enabled {
		return s.status, fmt.Errorf("workspace sync is not enabled")
	}
	if source == "" {
		source = SourceSystem
	}
	s.storeMu.Lock()
	commit, err := s.store.RestoreWorkspace(ctx, commitID, gitrevisions.CommitRequest{
		Reason: gitrevisions.ReasonRestore,
		Source: source,
		Actor:  actor,
	})
	s.storeMu.Unlock()
	if err != nil {
		s.status.LastError = err.Error()
		s.status.LastSyncTime = time.Now().UTC()
		return s.status, err
	}
	s.status.LastCommitHash = CommitHashFromString(commit.Hash)
	s.status.LastSyncTime = time.Now().UTC()
	s.status.LastError = ""
	s.status.ValidationErrors = nil
	if err := s.tree.ReconstructTreeFromFS(); err != nil {
		s.status.LastError = err.Error()
		s.status.ValidationErrors = s.validationErrorsFromError(err)
		return s.status, nil
	}
	s.recordChangedMarkdownPaths(commit.ChangedMarkdownPaths)
	if err := s.captureWritebacksLocked(ctx, gitrevisions.CommitRequest{
		Reason: gitrevisions.ReasonRestore,
		Source: source,
		Actor:  actor,
	}, commit, true); err != nil {
		s.status.LastError = err.Error()
		return s.status, err
	}
	if err := s.validateAndRunAfterSyncLocked(); err != nil {
		s.status.LastError = err.Error()
		return s.status, err
	}
	return s.status, nil
}

func (s *Service) GetPageRevisionSnapshot(ctx context.Context, page *tree.Page, commitID CommitHash) (*revision.RevisionSnapshot, error) {
	s.mu.Lock()
	if !s.enabled || page == nil || page.PageNode == nil {
		s.mu.Unlock()
		return nil, fmt.Errorf("workspace sync is not enabled")
	}
	store := s.store
	relPath := pageMarkdownPath(page)
	rootDir := s.rootDir
	s.mu.Unlock()

	s.storeMu.Lock()
	changedFiles, err := store.ChangedMarkdownContents(ctx, commitID)
	if err != nil {
		s.storeMu.Unlock()
		return nil, err
	}
	content, revisionPath, ok := changedContentForPageAtCommit(rootDir, page, relPath, changedFiles)
	if !ok {
		s.storeMu.Unlock()
		return nil, fmt.Errorf("document %s did not change in commit %s", relPath, commitID)
	}
	commit, err := store.GetCommit(ctx, commitID)
	s.storeMu.Unlock()
	if err != nil {
		return nil, err
	}
	return &revision.RevisionSnapshot{
		Revision: revisionForPageContent(rootDir, page, commit, revisionPath, content),
		Content:  content,
		Assets:   nil,
	}, nil
}

func (s *Service) RestoreDocument(ctx context.Context, page *tree.Page, commitID CommitHash, actor Actor) (SyncStatus, error) {
	return s.RestoreDocumentWithSource(ctx, page, commitID, actor, SourceSystem)
}

func (s *Service) RestoreDocumentWithSource(ctx context.Context, page *tree.Page, commitID CommitHash, actor Actor, source Source) (SyncStatus, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.enabled || page == nil || page.PageNode == nil {
		return s.status, fmt.Errorf("workspace sync is not enabled")
	}
	if source == "" {
		source = SourceSystem
	}
	s.storeMu.Lock()
	changedFiles, err := s.store.ChangedMarkdownContents(ctx, commitID)
	s.storeMu.Unlock()
	if err != nil {
		s.status.LastError = err.Error()
		s.status.LastSyncTime = time.Now().UTC()
		return s.status, err
	}
	targetRelPath := s.currentPageMarkdownPath(page)
	content, _, ok := changedContentForPageAtCommit(s.rootDir, page, targetRelPath, changedFiles)
	if !ok {
		err := fmt.Errorf("document %s did not change in commit %s", targetRelPath, commitID)
		s.status.LastError = err.Error()
		s.status.LastSyncTime = time.Now().UTC()
		return s.status, err
	}
	s.storeMu.Lock()
	commit, err := s.store.RestoreDocumentContentToPath(ctx, targetRelPath, content, gitrevisions.CommitRequest{
		Reason: gitrevisions.ReasonRestore,
		Source: source,
		Actor:  actor,
	})
	s.storeMu.Unlock()
	if err != nil {
		s.status.LastError = err.Error()
		s.status.LastSyncTime = time.Now().UTC()
		return s.status, err
	}
	s.status.LastCommitHash = CommitHashFromString(commit.Hash)
	s.status.LastSyncTime = time.Now().UTC()
	s.status.LastError = ""
	s.status.ValidationErrors = nil
	s.recordChangedMarkdownPaths(commit.ChangedMarkdownPaths)
	if err := s.tree.ReconstructTreeFromFS(); err != nil {
		s.status.LastError = err.Error()
		s.status.ValidationErrors = s.validationErrorsFromError(err)
		return s.status, err
	}
	if err := s.captureWritebacksLocked(ctx, gitrevisions.CommitRequest{
		Reason: gitrevisions.ReasonRestore,
		Source: source,
		Actor:  actor,
	}, commit, true); err != nil {
		s.status.LastError = err.Error()
		return s.status, err
	}
	if err := s.validateAndRunAfterSyncLocked(); err != nil {
		s.status.LastError = err.Error()
		return s.status, err
	}
	return s.status, nil
}

func (s *Service) captureWritebacksLocked(ctx context.Context, req gitrevisions.CommitRequest, commit *gitrevisions.Commit, amendCreatedCommit bool) error {
	if commit == nil {
		return nil
	}
	req.BatchID = commit.BatchID
	req.ChangedMarkdownPaths = commit.ChangedMarkdownPaths
	var (
		writebackCommit *gitrevisions.Commit
		err             error
	)
	s.storeMu.Lock()
	defer s.storeMu.Unlock()
	if commit.Created && amendCreatedCommit {
		requiresMigrationWriteback, err := capturedMarkdownRequiresMetadataWriteback(ctx, s.store, CommitHashFromString(commit.Hash))
		if err != nil {
			return err
		}
		if requiresMigrationWriteback {
			amendCreatedCommit = false
		}
	}
	if commit.Created && amendCreatedCommit {
		writebackCommit, err = s.store.Amend(ctx, req)
	} else {
		writebackCommit, err = s.store.Capture(ctx, req)
	}
	if err != nil {
		return err
	}
	if writebackCommit != nil && writebackCommit.Hash != "" {
		s.status.LastCommitHash = CommitHashFromString(writebackCommit.Hash)
		s.recordChangedMarkdownPaths(writebackCommit.ChangedMarkdownPaths)
	}
	return nil
}

func capturedMarkdownRequiresMetadataWriteback(ctx context.Context, store revisionStore, commitHash CommitHash) (bool, error) {
	if store == nil || commitHash == "" {
		return false, nil
	}
	changedFiles, err := store.ChangedMarkdownContents(ctx, commitHash)
	if err != nil {
		return false, err
	}
	for relPath, content := range changedFiles {
		if !gitrevisions.IsManagedMarkdownRelPath(relPath) {
			continue
		}
		_, result, err := markdown.ParsePageDocument(content)
		if err != nil {
			return false, err
		}
		if result.RequiresWriteback {
			return true, nil
		}
	}
	return false, nil
}

func (s *Service) validateAndRunAfterSyncLocked() error {
	if validationErrors := s.validateWorkspaceMarkdownFiles(); len(validationErrors) > 0 {
		s.status.ValidationErrors = mergeValidationErrors(s.status.ValidationErrors, validationErrors)
		if strings.TrimSpace(s.status.LastError) == "" {
			s.status.LastError = s.status.ValidationErrors[0].Message
		}
	}
	return s.runAfterSyncLocked()
}

func mergeValidationErrors(existing []ValidationError, next []ValidationError) []ValidationError {
	if len(existing) == 0 {
		return append([]ValidationError(nil), next...)
	}
	if len(next) == 0 {
		return append([]ValidationError(nil), existing...)
	}
	merged := make([]ValidationError, 0, len(existing)+len(next))
	seen := map[validationErrorDedupeKey]struct{}{}
	appendUnique := func(validationError ValidationError) {
		key := validationErrorDedupeKey{
			Code:      validationError.Code,
			MessageID: validationError.MessageID,
			Path:      validationError.Path,
			Message:   validationError.Message,
			Severity:  validationError.Severity,
		}
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = struct{}{}
		merged = append(merged, validationError)
	}
	for _, validationError := range existing {
		appendUnique(validationError)
	}
	for _, validationError := range next {
		appendUnique(validationError)
	}
	return merged
}

type validationErrorDedupeKey struct {
	Code      wikivalidation.IssueCode
	MessageID sharederrors.MessageID
	Path      string
	Message   string
	Severity  wikivalidation.IssueSeverity
}

func (s *Service) runAfterSyncLocked() error {
	if s.afterSync == nil {
		return nil
	}
	return s.afterSync()
}

func (s *Service) ListPageRevisions(ctx context.Context, page *tree.Page, cursor string, pageSize PageRevisionLimit) (PageRevisionList, error) {
	s.mu.Lock()
	if !s.enabled || page == nil || page.PageNode == nil {
		s.mu.Unlock()
		return PageRevisionList{}, nil
	}
	store := s.store
	rootDir := s.rootDir
	s.mu.Unlock()

	requestedLimit := int(pageSize)
	if requestedLimit <= 0 {
		requestedLimit = 50
	}
	relPath := pageMarkdownPath(page)
	scanLimit := requestedLimit + 1
	revisions := make([]*revision.Revision, 0, scanLimit)
	cursor = strings.TrimSpace(cursor)
	foundCursor := cursor == ""
	s.storeMu.Lock()
	err := store.ForEachCommit(ctx, func(commit gitrevisions.Commit) (bool, error) {
		if !foundCursor {
			if commit.Hash == cursor {
				foundCursor = true
			}
			return true, nil
		}
		changedFiles, err := store.ChangedMarkdownContents(ctx, CommitHashFromString(commit.Hash))
		if err != nil {
			return false, err
		}
		content, revisionPath, ok := changedContentForPageAtCommit(rootDir, page, relPath, changedFiles)
		if !ok {
			return true, nil
		}
		revisions = append(revisions, revisionForPageContent(rootDir, page, commit, revisionPath, content))
		if len(revisions) >= scanLimit {
			return false, nil
		}
		return true, nil
	})
	s.storeMu.Unlock()
	if err != nil {
		return PageRevisionList{}, err
	}
	nextCursor := ""
	if len(revisions) > requestedLimit {
		nextCursor = revisions[requestedLimit-1].ID.CommitID()
		revisions = revisions[:requestedLimit]
	}
	return PageRevisionList{Revisions: revisions, NextCursor: nextCursor}, nil
}

func pageMarkdownPath(page *tree.Page) string {
	if sourcePath := pageWorkspaceSourcePath(page); sourcePath != "" {
		sourcePathFS := sourcePath.FilesystemPath()
		if page.Kind == tree.NodeKindSection {
			return joinWorkspaceMarkdownPath(sourcePathFS, "index.md")
		}
		return sourcePathFS
	}
	path := strings.TrimPrefix(page.CalculatePath(), "/")
	if page != nil && page.Kind == tree.NodeKindSection {
		if path == "" {
			return "index.md"
		}
		return path + "/index.md"
	}
	return path + ".md"
}

func (s *Service) currentPageMarkdownPath(page *tree.Page) string {
	preferred := pageMarkdownPath(page)
	rootDir := strings.TrimSpace(s.rootDir)
	if rootDir == "" {
		return preferred
	}
	if sourcePath := pageWorkspaceSourcePath(page); sourcePath != "" {
		sourcePathFS := sourcePath.FilesystemPath()
		if page.Kind == tree.NodeKindSection {
			return s.currentSectionContentPath(sourcePathFS, preferred)
		}
		return sourcePathFS
	}
	if mappedPath, ok := s.currentWorkspaceMarkdownPathByRoute(page); ok {
		return mappedPath
	}
	dir, base := filepath.Split(filepath.FromSlash(preferred))
	entries, err := os.ReadDir(filepath.Join(rootDir, dir))
	if err != nil {
		return preferred
	}
	readmeFallback := ""
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		if !strings.EqualFold(entry.Name(), base) {
			if page.Kind == tree.NodeKindSection && entry.Name() == "README.md" {
				readmeFallback = filepath.ToSlash(filepath.Join(filepath.ToSlash(dir), entry.Name()))
			}
			continue
		}
		candidate := filepath.ToSlash(filepath.Join(filepath.ToSlash(dir), entry.Name()))
		if gitrevisions.IsManagedMarkdownRelPath(candidate) {
			return candidate
		}
	}
	if readmeFallback != "" && gitrevisions.IsManagedMarkdownRelPath(readmeFallback) {
		return readmeFallback
	}
	return preferred
}

func pageWorkspaceSourcePath(page *tree.Page) tree.WorkspaceSourcePath {
	if page == nil || page.PageNode == nil {
		return ""
	}
	return page.PageNode.WorkspaceSourcePath.Clean()
}

func cleanWorkspaceMarkdownPath(value string) string {
	return strings.Trim(strings.TrimSpace(filepath.ToSlash(value)), "/")
}

func joinWorkspaceMarkdownPath(parts ...string) string {
	nonEmpty := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.Trim(cleanWorkspaceMarkdownPath(part), "/")
		if part != "" {
			nonEmpty = append(nonEmpty, part)
		}
	}
	return strings.Join(nonEmpty, "/")
}

func (s *Service) currentSectionContentPath(sourcePath string, fallback string) string {
	dir := filepath.Join(s.rootDir, filepath.FromSlash(sourcePath))
	entries, err := os.ReadDir(dir)
	if err != nil {
		return fallback
	}
	readmeFallback := ""
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		ext := path.Ext(name)
		base := strings.TrimSuffix(name, ext)
		if strings.EqualFold(base, "index") && strings.EqualFold(ext, ".md") {
			return joinWorkspaceMarkdownPath(sourcePath, name)
		}
		if name == "README.md" {
			readmeFallback = joinWorkspaceMarkdownPath(sourcePath, name)
		}
	}
	if readmeFallback != "" {
		return readmeFallback
	}
	return fallback
}

func (s *Service) currentWorkspaceMarkdownPathByRoute(page *tree.Page) (string, bool) {
	if page == nil || page.PageNode == nil {
		return "", false
	}
	targetRoutePath := tree.RoutePathFromString(strings.Trim(page.CalculatePath(), "/")).Clean()
	var found string
	err := workspacesyncWalkDir(s.rootDir, func(filePath string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if filePath == s.rootDir {
			return nil
		}
		if entry.IsDir() {
			if strings.HasPrefix(entry.Name(), ".") {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.EqualFold(filepath.Ext(entry.Name()), ".md") {
			return nil
		}
		relPath, err := workspacesyncRel(s.rootDir, filePath)
		if err != nil {
			return err
		}
		relPath = filepath.ToSlash(relPath)
		routePath, kind := revisionRoutePathAndKind(s.rootDir, relPath, page)
		if kind != page.Kind || routePath.Clean() != targetRoutePath {
			return nil
		}
		found = relPath
		return filepath.SkipAll
	})
	if err != nil || found == "" {
		return "", false
	}
	return found, true
}

func contentForPageAtCommit(rootDir string, page *tree.Page, files map[string]string) (string, string, bool) {
	return contentForPageAtCommitPath(rootDir, page, pageMarkdownPath(page), files)
}

func contentForPageAtCommitPath(rootDir string, page *tree.Page, preferredPath string, files map[string]string) (string, string, bool) {
	if content, ok := files[preferredPath]; ok {
		if contentMatchesLeafWikiID(page, content) {
			return content, preferredPath, true
		}
	}
	paths := sortedMarkdownPaths(files)
	for _, markdownPath := range paths {
		content := files[markdownPath]
		if markdownPathMatchesPageRoute(rootDir, page, markdownPath) && contentMatchesLeafWikiID(page, content) {
			return content, markdownPath, true
		}
	}
	for _, markdownPath := range paths {
		content := files[markdownPath]
		leafWikiID, ok := leafWikiIDFromContent(content)
		if !ok {
			continue
		}
		if leafWikiID == page.ID {
			return content, markdownPath, true
		}
	}
	return "", "", false
}

func changedContentForPageAtCommit(rootDir string, page *tree.Page, preferredPath string, changedFiles map[string]string) (string, string, bool) {
	if len(changedFiles) == 0 {
		return "", "", false
	}
	if content, ok := changedFiles[preferredPath]; ok {
		if contentMatchesLeafWikiID(page, content) {
			return content, preferredPath, true
		}
	}
	paths := sortedMarkdownPaths(changedFiles)
	for _, markdownPath := range paths {
		content := changedFiles[markdownPath]
		if markdownPathMatchesPageRoute(rootDir, page, markdownPath) && contentMatchesLeafWikiID(page, content) {
			return content, markdownPath, true
		}
	}
	for _, markdownPath := range paths {
		content := changedFiles[markdownPath]
		leafWikiID, ok := leafWikiIDFromContent(content)
		if !ok {
			continue
		}
		if leafWikiID == page.ID {
			return content, markdownPath, true
		}
	}
	return "", "", false
}

func sortedMarkdownPaths(files map[string]string) []string {
	paths := make([]string, 0, len(files))
	for markdownPath := range files {
		if !gitrevisions.IsManagedMarkdownRelPath(markdownPath) {
			continue
		}
		paths = append(paths, markdownPath)
	}
	sort.Strings(paths)
	return paths
}

func markdownPathMatchesPageRoute(rootDir string, page *tree.Page, relPath string) bool {
	if page == nil || page.PageNode == nil {
		return false
	}
	routePath, kind := revisionRoutePathAndKind(rootDir, relPath, page)
	return kind == page.Kind && routePath.Clean() == tree.RoutePathFromString(strings.Trim(page.CalculatePath(), "/")).Clean()
}

func contentMatchesLeafWikiID(page *tree.Page, content string) bool {
	leafWikiID, ok := leafWikiIDFromContent(content)
	if !ok {
		return false
	}
	return leafWikiID == "" || leafWikiID == page.ID
}

func leafWikiIDFromContent(content string) (tree.PageID, bool) {
	doc, _, err := markdown.ParsePageDocument(content)
	if err != nil {
		return "", false
	}
	return tree.PageIDFromString(strings.TrimSpace(doc.Metadata.Page.ID)), true
}

func revisionForPageContent(rootDir string, page *tree.Page, commit gitrevisions.Commit, relPath string, content string) *revision.Revision {
	sum := sha256.Sum256([]byte(content))
	authorID := tree.UserIDFromString(strings.TrimSpace(commit.AuthorID.String()))
	if authorID == "" {
		authorID = tree.UserIDFromString(PublicEditorActor().ID.String())
	}
	summary := strings.TrimSpace(commit.Message)
	if summary == "" {
		summary = "workspace sync"
	}
	title := page.Title
	if mdFile, err := markdown.NewMarkdownFileFromRaw(relPath, content); err == nil {
		if historicalTitle, err := mdFile.GetTitle(); err == nil && strings.TrimSpace(historicalTitle) != "" {
			title = strings.TrimSpace(historicalTitle)
		}
	}
	routePath, slug, kind := revisionRoutePathSlugAndKind(rootDir, relPath, page)
	return &revision.Revision{
		ID:            tree.RevisionIDFromString(CommitHashFromString(commit.Hash)),
		PageID:        page.ID,
		Type:          revision.RevisionTypeContentUpdate,
		AuthorID:      authorID.MetadataValue(),
		CreatedAt:     commit.CreatedAt,
		Title:         title,
		Slug:          slug,
		Kind:          string(kind),
		Path:          routePath.FilesystemPath(),
		ContentHash:   hex.EncodeToString(sum[:]),
		PageCreatedAt: page.Metadata.CreatedAt,
		PageUpdatedAt: page.Metadata.UpdatedAt,
		CreatorID:     firstNonEmptyUserID(page.Metadata.CreatorID, authorID),
		LastAuthorID:  firstNonEmptyUserID(page.Metadata.LastAuthorID, authorID),
		Summary:       summary,
	}
}

func firstNonEmptyUserID(primary tree.UserID, fallback tree.UserID) string {
	if primary != "" {
		return primary.MetadataValue()
	}
	return fallback.MetadataValue()
}

func revisionRoutePathSlugAndKind(rootDir string, relPath string, page *tree.Page) (tree.RoutePath, tree.Slug, tree.NodeKind) {
	relPath = filepath.ToSlash(relPath)
	routePath, kind := revisionRoutePathAndKind(rootDir, relPath, page)
	slug := routePath.LeafSlug()
	if slug == "" && page != nil {
		slug = page.Slug
	}
	return routePath, slug, kind
}

func revisionRoutePathAndKind(rootDir string, relPath string, page *tree.Page) (tree.RoutePath, tree.NodeKind) {
	relPath = filepath.ToSlash(relPath)
	base := path.Base(relPath)
	dir := path.Dir(relPath)
	if dir == "." {
		dir = ""
	}
	dir = strings.Trim(dir, "/")
	route, err := tree.MapWorkspaceMarkdownRoute(rootDir, relPath, false)
	if err == nil && !route.Skip {
		return route.RoutePath, route.Kind
	}
	if isRevisionReadmeFallbackSection(relPath, page, dir) {
		return tree.RoutePathFromString(dir), tree.NodeKindSection
	}
	if strings.EqualFold(base, "index.md") {
		return tree.RoutePathFromString(dir), tree.NodeKindSection
	}
	return tree.CleanMarkdownPath(relPath).RoutePath(), tree.NodeKindPage
}

func isRevisionReadmeFallbackSection(relPath string, page *tree.Page, dir string) bool {
	if path.Base(filepath.ToSlash(relPath)) != "README.md" {
		return false
	}
	if page == nil || page.PageNode == nil || page.Kind != tree.NodeKindSection {
		return false
	}
	return tree.RoutePathFromString(strings.Trim(page.CalculatePath(), "/")).Clean() == tree.RoutePathFromString(dir).Clean()
}

func (s *Service) validateWorkspaceMarkdownFiles() []ValidationError {
	result := wikivalidation.ValidateWorkspaceMarkdownFiles(wikivalidation.WorkspaceMarkdownValidationOptions{
		RootDir:                s.rootDir,
		MarkdownLinkRootPrefix: s.markdownLinkRootPrefix,
		IncludeWarnings:        false,
	})
	if len(result.Issues) == 0 {
		return nil
	}
	validationErrors := make([]ValidationError, 0, len(result.Issues))
	for _, issue := range result.Issues {
		validationErrors = append(validationErrors, ValidationError{
			Code:      issue.Code,
			MessageID: issue.Code.MessageID(),
			Path:      markdownValidationIssuePath(issue),
			Message:   issue.Message,
			Severity:  issue.Severity,
		})
	}
	return validationErrors
}

func markdownValidationIssuePath(issue wikivalidation.Issue) string {
	if issue.SourcePath != "" {
		return issue.SourcePath.FilesystemPath()
	}
	return issue.RoutePath.FilesystemPath()
}

func (s *Service) validationErrorsFromError(err error) []ValidationError {
	if err == nil {
		return nil
	}
	if validationErrors := s.validateWorkspaceMarkdownFiles(); validationErrorsIncludeActionableMarkdownCode(validationErrors) {
		return validationErrors
	}
	message := err.Error()
	paths := markdownPathsInError(s.rootDir, message)
	if len(paths) == 0 {
		return []ValidationError{{
			Code:      wikivalidation.IssueCodeWorkspaceSyncError,
			MessageID: wikivalidation.IssueCodeWorkspaceSyncError.MessageID(),
			Path:      "workspace",
			Message:   message,
			Severity:  wikivalidation.IssueSeverityError,
		}}
	}
	errors := make([]ValidationError, 0, len(paths))
	for _, path := range paths {
		errors = append(errors, ValidationError{
			Code:      wikivalidation.IssueCodeWorkspaceSyncError,
			MessageID: wikivalidation.IssueCodeWorkspaceSyncError.MessageID(),
			Path:      path,
			Message:   message,
			Severity:  wikivalidation.IssueSeverityError,
		})
	}
	return errors
}

func validationErrorsIncludeActionableMarkdownCode(errors []ValidationError) bool {
	for _, validationError := range errors {
		switch validationError.Code {
		case "", wikivalidation.IssueCodeWorkspaceScanError, wikivalidation.IssueCodeWorkspaceSyncError, wikivalidation.IssueCodeWorkspaceSyncValidation:
			continue
		default:
			return true
		}
	}
	return false
}

func markdownPathsInError(rootDir string, message string) []string {
	seen := make(map[string]struct{})
	var paths []string
	for _, token := range strings.Fields(message) {
		candidate := cleanErrorTokenMarkdownPath(token)
		if candidate == "" {
			continue
		}
		path := normalizeValidationPath(rootDir, candidate)
		if path == "" {
			continue
		}
		if _, ok := seen[path]; ok {
			continue
		}
		seen[path] = struct{}{}
		paths = append(paths, path)
	}
	return paths
}

func cleanErrorTokenMarkdownPath(token string) string {
	cleaned := strings.Trim(token, "\"'`()[]{},;:")
	cleaned = strings.TrimSuffix(cleaned, ".")
	index := strings.Index(strings.ToLower(cleaned), ".md")
	if index < 0 {
		return ""
	}
	cleaned = cleaned[:index+len(".md")]
	for _, prefix := range []string{"path=", "file="} {
		cleaned = strings.TrimPrefix(cleaned, prefix)
	}
	cleaned = strings.Trim(cleaned, "\"'`()[]{},;:")
	return strings.TrimSuffix(cleaned, ".")
}

func normalizeValidationPath(rootDir string, candidate string) string {
	candidate = filepath.Clean(candidate)
	if filepath.IsAbs(candidate) {
		if rootDir != "" {
			if rel, err := filepath.Rel(rootDir, candidate); err == nil && rel != "." && !strings.HasPrefix(rel, "..") {
				return filepath.ToSlash(rel)
			}
		}
		return filepath.ToSlash(candidate)
	}
	candidate = filepath.ToSlash(candidate)
	candidate = strings.TrimPrefix(candidate, "./")
	if candidate == "." || strings.HasPrefix(candidate, "../") {
		return ""
	}
	return candidate
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}
