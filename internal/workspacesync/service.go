package workspacesync

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/perber/wiki/internal/core/markdownlinks"
	wikivalidation "github.com/perber/wiki/internal/core/markdownvalidation"
	"github.com/perber/wiki/internal/core/revision"
	sharederrors "github.com/perber/wiki/internal/core/shared/errors"
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

const (
	watcherBatchDebounce            = 250 * time.Millisecond
	watcherDroppedEventsStatusLabel = "watcher dropped events"
)

var (
	ErrTreeServiceRequired            = errors.New("tree service is required")
	ErrWorkspaceSyncDisabled          = errors.New("workspace sync is not enabled")
	ErrWorkspaceSyncDocumentUnchanged = errors.New("workspace sync document did not change")
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
	AuthorID             ActorID    `json:"authorId,omitempty"`
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
		return nil, ErrTreeServiceRequired
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
