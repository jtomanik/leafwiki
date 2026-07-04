package gitrevisions

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/go-git/go-billy/v6/osfs"
	git "github.com/go-git/go-git/v6"
	"github.com/go-git/go-git/v6/plumbing/cache"
	"github.com/go-git/go-git/v6/plumbing/storer"
	gitstorage "github.com/go-git/go-git/v6/storage"
	"github.com/go-git/go-git/v6/storage/filesystem"

	"github.com/perber/wiki/internal/core/identity"
)

type Reason string

const (
	ReasonStartup         Reason = "startup"
	ReasonWatcher         Reason = "watcher"
	ReasonExplicit        Reason = "explicit"
	ReasonExplicitRefresh Reason = "explicit_refresh"
	ReasonWebWrite        Reason = "web_write"
	ReasonRestore         Reason = "restore"
)

type Source string

const (
	SourceFilesystem Source = "filesystem"
	SourceWeb        Source = "web"
	SourceMCP        Source = "mcp"
	SourceSystem     Source = "system"
	SourceUnknown    Source = "unknown"
)

type Actor struct {
	ID    ActorID
	Name  string
	Email string
}

type StoreOptions struct {
	DataDir string
	RootDir string
}

type CommitRequest struct {
	Reason               Reason
	Source               Source
	Actor                Actor
	AdditionalActors     []Actor
	BatchID              string
	ChangedMarkdownPaths []string
}

type Commit struct {
	Hash                 identity.CommitHash
	Message              string
	AuthorID             ActorID
	AuthorName           string
	AuthorEmail          string
	ActorIDs             []ActorID
	CreatedAt            time.Time
	Created              bool
	Source               Source
	Reason               Reason
	BatchID              string
	ChangedMarkdownCount int
	ChangedMarkdownPaths []string
}

type ListRequest struct {
	Cursor identity.CommitHash
	Limit  int
}

type Store struct {
	dataDir string
	rootDir string
	repo    *git.Repository
}

var ErrDataDirRequired = errors.New("data dir is required")
var ErrRootDirRequired = errors.New("root dir is required")
var ErrDocumentRestorePathInvalid = errors.New("document restore path is invalid")
var ErrDocumentRestoreSourcePathInvalid = errors.New("document restore source path is invalid")

func PublicEditorActor() Actor {
	return Actor{
		ID:    "public-editor",
		Name:  "Public Editor",
		Email: "public-editor@leafwiki.local",
	}
}

func Open(options StoreOptions) (*Store, error) {
	dataDir := strings.TrimSpace(options.DataDir)
	rootDir := strings.TrimSpace(options.RootDir)
	if dataDir == "" {
		return nil, ErrDataDirRequired
	}
	if rootDir == "" {
		return nil, ErrRootDirRequired
	}
	dataDir = filepath.Clean(dataDir)
	rootDir = filepath.Clean(rootDir)
	if err := gitRevisionMkdirAll(filepath.Join(dataDir, ".leafwiki", "git"), 0o755); err != nil {
		return nil, fmt.Errorf("create internal git dir: %w", err)
	}
	if err := gitRevisionMkdirAll(rootDir, 0o755); err != nil {
		return nil, fmt.Errorf("create root dir: %w", err)
	}

	storage := filesystem.NewStorage(osfs.New(internalGitDir(dataDir), osfs.WithBoundOS()), cache.NewObjectLRUDefault())
	worktree := osfs.New(rootDir, osfs.WithBoundOS())
	repo, err := gitRevisionGitOpen(storage, worktree)
	if errors.Is(err, git.ErrRepositoryNotExists) {
		if _, initErr := gitRevisionGitInit(nonFilesystemInitStorageFor(storage), git.WithWorkTree(worktree)); initErr != nil {
			return nil, fmt.Errorf("open internal git repository: %w", initErr)
		}
		repo, err = gitRevisionGitOpen(storage, worktree)
	}
	if err != nil {
		return nil, fmt.Errorf("open internal git repository: %w", err)
	}
	if err := gitRevisionRemoveInternalRootGitFile(rootDir, internalGitDir(dataDir)); err != nil {
		return nil, err
	}
	return &Store{dataDir: dataDir, rootDir: rootDir, repo: repo}, nil
}

type nonFilesystemInitStorage struct {
	gitstorage.Storer
	initializer storer.Initializer
}

func nonFilesystemInitStorageFor(storage gitstorage.Storer) nonFilesystemInitStorage {
	initializer, _ := storage.(storer.Initializer)
	return nonFilesystemInitStorage{Storer: storage, initializer: initializer}
}

func (s nonFilesystemInitStorage) Init() error {
	if s.initializer == nil {
		return nil
	}
	return s.initializer.Init()
}
