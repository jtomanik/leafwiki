package gitrevisions

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/go-git/go-billy/v6/osfs"
	git "github.com/go-git/go-git/v6"
	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/cache"
	"github.com/go-git/go-git/v6/plumbing/object"
	"github.com/go-git/go-git/v6/plumbing/storer"
	gitstorage "github.com/go-git/go-git/v6/storage"
	"github.com/go-git/go-git/v6/storage/filesystem"
)

type Reason string

const (
	ReasonStartup  Reason = "startup"
	ReasonWatcher  Reason = "watcher"
	ReasonExplicit Reason = "explicit"
	ReasonWebWrite Reason = "web_write"
	ReasonRestore  Reason = "restore"
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
	ID    string
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
	Hash                 string
	Message              string
	AuthorID             string
	AuthorName           string
	AuthorEmail          string
	ActorIDs             []string
	CreatedAt            time.Time
	Created              bool
	Source               Source
	Reason               Reason
	BatchID              string
	ChangedMarkdownCount int
	ChangedMarkdownPaths []string
}

type ListRequest struct {
	Cursor string
	Limit  int
}

type Store struct {
	dataDir string
	rootDir string
	repo    *git.Repository
}

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
		return nil, fmt.Errorf("data dir is required")
	}
	if rootDir == "" {
		return nil, fmt.Errorf("root dir is required")
	}
	dataDir = filepath.Clean(dataDir)
	rootDir = filepath.Clean(rootDir)
	if err := os.MkdirAll(filepath.Join(dataDir, ".leafwiki", "git"), 0o755); err != nil {
		return nil, fmt.Errorf("create internal git dir: %w", err)
	}
	if err := os.MkdirAll(rootDir, 0o755); err != nil {
		return nil, fmt.Errorf("create root dir: %w", err)
	}

	storage := filesystem.NewStorage(osfs.New(internalGitDir(dataDir), osfs.WithBoundOS()), cache.NewObjectLRUDefault())
	worktree := osfs.New(rootDir, osfs.WithBoundOS())
	repo, err := git.Open(storage, worktree)
	if errors.Is(err, git.ErrRepositoryNotExists) {
		if _, initErr := git.Init(nonFilesystemInitStorageFor(storage), git.WithWorkTree(worktree)); initErr != nil {
			return nil, fmt.Errorf("open internal git repository: %w", initErr)
		}
		repo, err = git.Open(storage, worktree)
	}
	if err != nil {
		return nil, fmt.Errorf("open internal git repository: %w", err)
	}
	if err := removeInternalRootGitFile(rootDir, internalGitDir(dataDir)); err != nil {
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

func internalGitDir(dataDir string) string {
	return filepath.Join(dataDir, ".leafwiki", "git")
}

func removeInternalRootGitFile(rootDir string, internalGitDir string) error {
	gitPath := filepath.Join(rootDir, ".git")
	info, err := os.Lstat(gitPath)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("stat root .git: %w", err)
	}
	if info.IsDir() {
		return nil
	}
	raw, err := os.ReadFile(gitPath)
	if err != nil {
		return fmt.Errorf("read root .git file: %w", err)
	}
	target, ok := parseGitDirFile(string(raw), rootDir)
	if !ok || !sameFilesystemPath(target, internalGitDir) {
		return nil
	}
	if err := os.Remove(gitPath); err != nil {
		return fmt.Errorf("remove root .git file: %w", err)
	}
	return nil
}

func parseGitDirFile(raw string, rootDir string) (string, bool) {
	line := strings.TrimSpace(strings.Split(raw, "\n")[0])
	target, ok := strings.CutPrefix(line, "gitdir:")
	if !ok {
		return "", false
	}
	target = strings.TrimSpace(target)
	if target == "" {
		return "", false
	}
	if filepath.IsAbs(target) {
		return filepath.Clean(target), true
	}
	return filepath.Clean(filepath.Join(rootDir, target)), true
}

func sameFilesystemPath(a string, b string) bool {
	absA, errA := filepath.Abs(a)
	absB, errB := filepath.Abs(b)
	if errA == nil {
		a = absA
	}
	if errB == nil {
		b = absB
	}
	return filepath.Clean(a) == filepath.Clean(b)
}

func (s *Store) Capture(ctx context.Context, req CommitRequest) (*Commit, error) {
	return s.commit(ctx, req, false)
}

func (s *Store) Amend(ctx context.Context, req CommitRequest) (*Commit, error) {
	return s.commit(ctx, req, true)
}

func (s *Store) commit(ctx context.Context, req CommitRequest, amend bool) (*Commit, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	wt, err := s.repo.Worktree()
	if err != nil {
		return nil, err
	}
	changedMarkdownPaths, err := s.stageMarkdownChanges(ctx, wt)
	if err != nil {
		return nil, err
	}
	messageChangedMarkdownPaths := changedMarkdownPaths
	if len(req.ChangedMarkdownPaths) > 0 {
		messageChangedMarkdownPaths = mergeMarkdownPaths(req.ChangedMarkdownPaths, changedMarkdownPaths)
	}
	batchID := strings.TrimSpace(req.BatchID)
	if batchID == "" {
		batchID = newBatchID()
	}
	hash, err := wt.Commit(commitMessage(req, batchID, messageChangedMarkdownPaths), &git.CommitOptions{
		Author:    signature(req.Actor),
		Committer: leafWikiCommitter(),
		Amend:     amend,
	})
	if errors.Is(err, git.ErrEmptyCommit) {
		head, headErr := s.repo.Head()
		if req.Reason == ReasonRestore && !amend {
			hash, err = wt.Commit(commitMessage(req, batchID, messageChangedMarkdownPaths), &git.CommitOptions{
				Author:            signature(req.Actor),
				Committer:         leafWikiCommitter(),
				AllowEmptyCommits: true,
			})
			if err != nil {
				return nil, fmt.Errorf("commit empty restore snapshot: %w", err)
			}
			return newCommitResult(hash.String(), batchID, messageChangedMarkdownPaths, true), nil
		}
		if errors.Is(headErr, plumbing.ErrReferenceNotFound) && !amend {
			hash, err = wt.Commit(commitMessage(req, batchID, messageChangedMarkdownPaths), &git.CommitOptions{
				Author:            signature(req.Actor),
				Committer:         leafWikiCommitter(),
				AllowEmptyCommits: true,
			})
			if err != nil {
				return nil, fmt.Errorf("commit empty initial markdown snapshot: %w", err)
			}
			return newCommitResult(hash.String(), batchID, messageChangedMarkdownPaths, true), nil
		}
		if headErr != nil {
			return nil, err
		}
		return newCommitResult(head.Hash().String(), batchID, nil, false), nil
	}
	if err != nil {
		return nil, fmt.Errorf("commit markdown snapshot: %w", err)
	}
	return newCommitResult(hash.String(), batchID, messageChangedMarkdownPaths, true), nil
}

func newCommitResult(hash string, batchID string, changedMarkdownPaths []string, created bool) *Commit {
	paths := append([]string(nil), changedMarkdownPaths...)
	sort.Strings(paths)
	return &Commit{
		Hash:                 hash,
		Created:              created,
		BatchID:              batchID,
		ChangedMarkdownCount: len(paths),
		ChangedMarkdownPaths: paths,
	}
}

func mergeMarkdownPaths(groups ...[]string) []string {
	seen := make(map[string]struct{})
	for _, paths := range groups {
		for _, path := range paths {
			path = strings.TrimSpace(filepath.ToSlash(path))
			if path == "" {
				continue
			}
			seen[path] = struct{}{}
		}
	}
	return sortedKeys(seen)
}

func (s *Store) stageMarkdownChanges(ctx context.Context, wt *git.Worktree) ([]string, error) {
	paths, err := collectMarkdownPaths(s.rootDir)
	if err != nil {
		return nil, err
	}
	trackedFiles, err := s.trackedMarkdownFiles()
	if err != nil {
		return nil, err
	}
	changed := make(map[string]struct{})
	for _, path := range paths {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		raw, err := os.ReadFile(filepath.Join(s.rootDir, filepath.FromSlash(path)))
		if err != nil {
			return nil, fmt.Errorf("read markdown %s: %w", path, err)
		}
		if tracked, ok := trackedFiles[path]; !ok || tracked != string(raw) {
			changed[path] = struct{}{}
		}
		if _, err := wt.Add(path); err != nil {
			return nil, fmt.Errorf("stage markdown %s: %w", path, err)
		}
	}
	current := make(map[string]struct{}, len(paths))
	for _, path := range paths {
		current[path] = struct{}{}
	}
	for path := range trackedFiles {
		if _, exists := current[path]; exists {
			continue
		}
		if isManagedMarkdownRelPath(path) {
			if _, err := wt.Remove(path); err != nil {
				return nil, fmt.Errorf("stage markdown delete %s: %w", path, err)
			}
		} else if err := s.removeFromIndexOnly(path); err != nil {
			return nil, fmt.Errorf("stage unmanaged markdown delete %s: %w", path, err)
		}
		changed[path] = struct{}{}
	}
	changedPaths := make([]string, 0, len(changed))
	for path := range changed {
		changedPaths = append(changedPaths, path)
	}
	sort.Strings(changedPaths)
	return changedPaths, nil
}

func (s *Store) removeFromIndexOnly(path string) error {
	idx, err := s.repo.Storer.Index()
	if err != nil {
		return err
	}
	if _, err := idx.Remove(path); err != nil {
		return err
	}
	return s.repo.Storer.SetIndex(idx)
}

func collectMarkdownPaths(rootDir string) ([]string, error) {
	var paths []string
	err := filepath.WalkDir(rootDir, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if path == rootDir {
			return nil
		}
		name := entry.Name()
		if entry.IsDir() {
			if shouldSkipDir(name) {
				return filepath.SkipDir
			}
			return nil
		}
		if !entry.Type().IsRegular() || !isManagedMarkdownPath(name) {
			return nil
		}
		rel, err := filepath.Rel(rootDir, path)
		if err != nil {
			return err
		}
		paths = append(paths, filepath.ToSlash(rel))
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("collect markdown paths: %w", err)
	}
	sort.Strings(paths)
	return paths, nil
}

func (s *Store) trackedMarkdownFiles() (map[string]string, error) {
	files := make(map[string]string)
	head, err := s.repo.Head()
	if errors.Is(err, plumbing.ErrReferenceNotFound) {
		return files, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read internal git HEAD: %w", err)
	}
	commit, err := s.repo.CommitObject(head.Hash())
	if err != nil {
		return nil, fmt.Errorf("load HEAD commit: %w", err)
	}
	tree, err := commit.Tree()
	if err != nil {
		return nil, fmt.Errorf("load HEAD tree: %w", err)
	}
	iter := tree.Files()
	defer iter.Close()
	if err := iter.ForEach(func(file *object.File) error {
		if isTrackedMarkdownRelPath(file.Name) {
			reader, err := file.Reader()
			if err != nil {
				return err
			}
			defer reader.Close()
			raw, err := io.ReadAll(reader)
			if err != nil {
				return err
			}
			files[file.Name] = string(raw)
		}
		return nil
	}); err != nil {
		return nil, fmt.Errorf("list tracked markdown: %w", err)
	}
	return files, nil
}

func shouldSkipDir(name string) bool {
	return strings.HasPrefix(name, ".")
}

func isManagedMarkdownPath(name string) bool {
	if strings.HasPrefix(name, ".") {
		return false
	}
	return strings.EqualFold(filepath.Ext(name), ".md")
}

func isManagedMarkdownRelPath(relPath string) bool {
	relPath = filepath.ToSlash(relPath)
	for _, segment := range strings.Split(relPath, "/") {
		if strings.HasPrefix(segment, ".") {
			return false
		}
	}
	base := filepath.Base(relPath)
	return isManagedMarkdownPath(base)
}

func IsManagedMarkdownRelPath(relPath string) bool {
	return isManagedMarkdownRelPath(relPath)
}

func isTrackedMarkdownRelPath(relPath string) bool {
	base := filepath.Base(filepath.ToSlash(relPath))
	return strings.EqualFold(filepath.Ext(base), ".md")
}

func commitMessage(req CommitRequest, batchID string, changedMarkdownPaths []string) string {
	reason := req.Reason
	if reason == "" {
		reason = ReasonExplicit
	}
	source := req.Source
	if source == "" {
		source = SourceUnknown
	}
	actorIDs := commitActorIDs(req)
	title := "LeafWiki workspace sync"
	if reason == ReasonStartup {
		title = "LeafWiki initial workspace snapshot"
	}
	if reason == ReasonRestore {
		title = "LeafWiki workspace restore"
	}
	var b strings.Builder
	fmt.Fprintf(
		&b,
		"%s\n\nLeafWiki-Source: %s\nLeafWiki-Reason: %s\nLeafWiki-Batch: %s\n",
		title,
		source,
		reason,
		batchID,
	)
	for _, actorID := range actorIDs {
		fmt.Fprintf(&b, "LeafWiki-Actor: %s\n", actorID)
	}
	fmt.Fprintf(&b, "LeafWiki-Changed-Markdown: %d\n", len(changedMarkdownPaths))
	return b.String()
}

func commitActorIDs(req CommitRequest) []string {
	primaryID := strings.TrimSpace(req.Actor.ID)
	if primaryID == "" {
		primaryID = PublicEditorActor().ID
	}
	actorIDs := []string{primaryID}
	seen := map[string]struct{}{primaryID: {}}
	for _, actor := range req.AdditionalActors {
		actorID := strings.TrimSpace(actor.ID)
		if actorID == "" {
			continue
		}
		if _, ok := seen[actorID]; ok {
			continue
		}
		seen[actorID] = struct{}{}
		actorIDs = append(actorIDs, actorID)
	}
	return actorIDs
}

func newBatchID() string {
	var raw [8]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return fmt.Sprintf("%d", time.Now().UTC().UnixNano())
	}
	return hex.EncodeToString(raw[:])
}

func signature(actor Actor) *object.Signature {
	if strings.TrimSpace(actor.ID) == "" {
		actor = PublicEditorActor()
	}
	name := strings.TrimSpace(actor.Name)
	if name == "" {
		name = actor.ID
	}
	email := strings.TrimSpace(actor.Email)
	if email == "" {
		email = actor.ID + "@leafwiki.local"
	}
	return &object.Signature{Name: name, Email: email, When: time.Now().UTC()}
}

func leafWikiCommitter() *object.Signature {
	return &object.Signature{Name: "LeafWiki", Email: "leafwiki@leafwiki.local", When: time.Now().UTC()}
}

var stopCommitIteration = errors.New("stop commit iteration")

func (s *Store) ListCommits(ctx context.Context, req ListRequest) ([]Commit, error) {
	limit := req.Limit
	if limit <= 0 {
		limit = 50
	}
	commits := make([]Commit, 0, limit)
	cursor := strings.TrimSpace(req.Cursor)
	foundCursor := cursor == ""
	err := s.ForEachCommit(ctx, func(commit Commit) (bool, error) {
		if !foundCursor {
			if commit.Hash == cursor {
				foundCursor = true
			}
			return true, nil
		}
		commits = append(commits, commit)
		return len(commits) < limit, nil
	})
	if err != nil {
		return nil, err
	}
	return commits, nil
}

func (s *Store) ForEachCommit(ctx context.Context, visit func(Commit) (bool, error)) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if visit == nil {
		return nil
	}
	iter, err := s.repo.Log(&git.LogOptions{})
	if errors.Is(err, plumbing.ErrReferenceNotFound) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("list commits: %w", err)
	}
	defer iter.Close()

	err = iter.ForEach(func(commit *object.Commit) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		keepGoing, err := visit(commitFromObject(commit))
		if err != nil {
			return err
		}
		if !keepGoing {
			return stopCommitIteration
		}
		return nil
	})
	if errors.Is(err, stopCommitIteration) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("iterate commits: %w", err)
	}
	return nil
}

func (s *Store) GetCommit(ctx context.Context, commitHash string) (Commit, error) {
	if err := ctx.Err(); err != nil {
		return Commit{}, err
	}
	commit, err := s.repo.CommitObject(plumbing.NewHash(commitHash))
	if err != nil {
		return Commit{}, fmt.Errorf("load commit %s: %w", commitHash, err)
	}
	return commitFromObject(commit), nil
}

func (s *Store) ChangedMarkdownPaths(ctx context.Context, commitHash string) ([]string, error) {
	paths, _, err := s.changedMarkdownEntries(ctx, commitHash)
	return paths, err
}

func (s *Store) ChangedMarkdownContents(ctx context.Context, commitHash string) (map[string]string, error) {
	_, contents, err := s.changedMarkdownEntries(ctx, commitHash)
	return contents, err
}

func (s *Store) changedMarkdownEntries(ctx context.Context, commitHash string) ([]string, map[string]string, error) {
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	commit, err := s.repo.CommitObject(plumbing.NewHash(commitHash))
	if err != nil {
		return nil, nil, fmt.Errorf("load commit %s: %w", commitHash, err)
	}
	currentTree, err := commit.Tree()
	if err != nil {
		return nil, nil, fmt.Errorf("load commit tree: %w", err)
	}
	paths := make(map[string]struct{})
	contents := make(map[string]string)

	parentIter := commit.Parents()
	parent, err := parentIter.Next()
	if errors.Is(err, object.ErrParentNotFound) || errors.Is(err, io.EOF) {
		iter := currentTree.Files()
		defer iter.Close()
		if err := iter.ForEach(func(file *object.File) error {
			if err := ctx.Err(); err != nil {
				return err
			}
			if !isManagedMarkdownRelPath(file.Name) {
				return nil
			}
			content, err := file.Contents()
			if err != nil {
				return err
			}
			paths[file.Name] = struct{}{}
			contents[file.Name] = content
			return nil
		}); err != nil {
			return nil, nil, fmt.Errorf("list changed markdown for root commit: %w", err)
		}
		return sortedKeys(paths), contents, nil
	}
	if err != nil {
		return nil, nil, fmt.Errorf("load parent for commit %s: %w", commitHash, err)
	}
	parentTree, err := parent.Tree()
	if err != nil {
		return nil, nil, fmt.Errorf("load parent tree: %w", err)
	}

	changes, err := parentTree.DiffContext(ctx, currentTree)
	if err != nil {
		return nil, nil, fmt.Errorf("diff commit %s: %w", commitHash, err)
	}
	for _, change := range changes {
		if err := ctx.Err(); err != nil {
			return nil, nil, err
		}
		from, to, err := change.Files()
		if err != nil {
			return nil, nil, err
		}
		if from != nil && isManagedMarkdownRelPath(from.Name) {
			paths[from.Name] = struct{}{}
		}
		if to == nil || !isManagedMarkdownRelPath(to.Name) {
			continue
		}
		paths[to.Name] = struct{}{}
		content, err := to.Contents()
		if err != nil {
			return nil, nil, err
		}
		contents[to.Name] = content
	}
	return sortedKeys(paths), contents, nil
}

func sortedKeys(values map[string]struct{}) []string {
	paths := make([]string, 0, len(values))
	for path := range values {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	return paths
}

func commitFromObject(commit *object.Commit) Commit {
	title, trailers, actorIDs := parseCommitMessage(commit.Message)
	changed, _ := strconv.Atoi(trailers["LeafWiki-Changed-Markdown"])
	authorID := ""
	if len(actorIDs) > 0 {
		authorID = actorIDs[0]
	}
	return Commit{
		Hash:                 commit.Hash.String(),
		Message:              title,
		AuthorID:             authorID,
		AuthorName:           commit.Author.Name,
		AuthorEmail:          commit.Author.Email,
		ActorIDs:             actorIDs,
		CreatedAt:            commit.Author.When,
		Source:               Source(trailers["LeafWiki-Source"]),
		Reason:               Reason(trailers["LeafWiki-Reason"]),
		BatchID:              trailers["LeafWiki-Batch"],
		ChangedMarkdownCount: changed,
	}
}

func parseCommitMessage(message string) (string, map[string]string, []string) {
	lines := strings.Split(message, "\n")
	title := ""
	if len(lines) > 0 {
		title = strings.TrimSpace(lines[0])
	}
	trailers := make(map[string]string)
	var actorIDs []string
	for _, line := range lines[1:] {
		key, value, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		if !strings.HasPrefix(key, "LeafWiki-") {
			continue
		}
		value = strings.TrimSpace(value)
		if key == "LeafWiki-Actor" {
			actorIDs = append(actorIDs, value)
			if _, exists := trailers[key]; !exists {
				trailers[key] = value
			}
			continue
		}
		trailers[key] = value
	}
	return title, trailers, actorIDs
}

func (s *Store) RestoreWorkspace(ctx context.Context, commitHash string, req CommitRequest) (*Commit, error) {
	files, err := s.FilesAt(ctx, commitHash)
	if err != nil {
		return nil, err
	}
	target := make(map[string]struct{}, len(files))
	for relPath, content := range files {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if !isManagedMarkdownRelPath(relPath) {
			continue
		}
		target[relPath] = struct{}{}
		fullPath := filepath.Join(s.rootDir, filepath.FromSlash(relPath))
		if err := os.MkdirAll(filepath.Dir(fullPath), 0o755); err != nil {
			return nil, fmt.Errorf("create restore parent %s: %w", relPath, err)
		}
		if err := os.WriteFile(fullPath, []byte(content), 0o644); err != nil {
			return nil, fmt.Errorf("write restored markdown %s: %w", relPath, err)
		}
	}
	current, err := collectMarkdownPaths(s.rootDir)
	if err != nil {
		return nil, err
	}
	for _, relPath := range current {
		if _, keep := target[relPath]; keep {
			continue
		}
		if err := os.Remove(filepath.Join(s.rootDir, filepath.FromSlash(relPath))); err != nil && !errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("remove markdown absent from restore %s: %w", relPath, err)
		}
	}
	if req.Reason == "" {
		req.Reason = ReasonRestore
	}
	if req.Source == "" {
		req.Source = SourceSystem
	}
	return s.Capture(ctx, req)
}

func (s *Store) RestoreDocument(ctx context.Context, relPath string, commitHash string, req CommitRequest) (*Commit, error) {
	return s.RestoreDocumentToPath(ctx, relPath, relPath, commitHash, req)
}

func (s *Store) RestoreDocumentToPath(ctx context.Context, targetRelPath string, sourceRelPath string, commitHash string, req CommitRequest) (*Commit, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	targetRelPath = filepath.ToSlash(filepath.Clean(strings.TrimSpace(targetRelPath)))
	if targetRelPath == "." || strings.HasPrefix(targetRelPath, "../") || !isManagedMarkdownRelPath(targetRelPath) {
		return nil, fmt.Errorf("document restore path is invalid: %s", targetRelPath)
	}
	sourceRelPath = filepath.ToSlash(filepath.Clean(strings.TrimSpace(sourceRelPath)))
	if sourceRelPath == "." || strings.HasPrefix(sourceRelPath, "../") || !isManagedMarkdownRelPath(sourceRelPath) {
		return nil, fmt.Errorf("document restore source path is invalid: %s", sourceRelPath)
	}
	content, err := s.fileContentAt(ctx, commitHash, sourceRelPath)
	if err != nil {
		return nil, err
	}
	return s.RestoreDocumentContentToPath(ctx, targetRelPath, content, req)
}

func (s *Store) RestoreDocumentContentToPath(ctx context.Context, targetRelPath string, content string, req CommitRequest) (*Commit, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	targetRelPath = filepath.ToSlash(filepath.Clean(strings.TrimSpace(targetRelPath)))
	if targetRelPath == "." || strings.HasPrefix(targetRelPath, "../") || !isManagedMarkdownRelPath(targetRelPath) {
		return nil, fmt.Errorf("document restore path is invalid: %s", targetRelPath)
	}
	fullPath := filepath.Join(s.rootDir, filepath.FromSlash(targetRelPath))
	if err := os.MkdirAll(filepath.Dir(fullPath), 0o755); err != nil {
		return nil, fmt.Errorf("create restore parent %s: %w", targetRelPath, err)
	}
	if err := os.WriteFile(fullPath, []byte(content), 0o644); err != nil {
		return nil, fmt.Errorf("write restored markdown %s: %w", targetRelPath, err)
	}
	if req.Reason == "" {
		req.Reason = ReasonRestore
	}
	if req.Source == "" {
		req.Source = SourceSystem
	}
	return s.Capture(ctx, req)
}

func (s *Store) fileContentAt(ctx context.Context, commitHash string, relPath string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	hash := plumbing.NewHash(commitHash)
	commit, err := s.repo.CommitObject(hash)
	if err != nil {
		return "", fmt.Errorf("load commit %s: %w", commitHash, err)
	}
	tree, err := commit.Tree()
	if err != nil {
		return "", fmt.Errorf("load commit tree: %w", err)
	}
	file, err := tree.File(relPath)
	if err != nil {
		return "", fmt.Errorf("document %s is not present in commit %s: %w", relPath, commitHash, err)
	}
	content, err := file.Contents()
	if err != nil {
		return "", fmt.Errorf("read document %s from commit %s: %w", relPath, commitHash, err)
	}
	return content, nil
}

func (s *Store) FilesAt(ctx context.Context, commitHash string) (map[string]string, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	hash := plumbing.NewHash(commitHash)
	commit, err := s.repo.CommitObject(hash)
	if err != nil {
		return nil, fmt.Errorf("load commit %s: %w", commitHash, err)
	}
	return s.filesAtCommit(ctx, commit)
}

func (s *Store) filesAtCommit(ctx context.Context, commit *object.Commit) (map[string]string, error) {
	tree, err := commit.Tree()
	if err != nil {
		return nil, fmt.Errorf("load commit tree: %w", err)
	}
	files := make(map[string]string)
	iter := tree.Files()
	defer iter.Close()
	err = iter.ForEach(func(file *object.File) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if !isManagedMarkdownRelPath(file.Name) {
			return nil
		}
		reader, err := file.Reader()
		if err != nil {
			return err
		}
		defer reader.Close()
		raw, err := io.ReadAll(reader)
		if err != nil {
			return err
		}
		files[file.Name] = string(raw)
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("read commit files: %w", err)
	}
	return files, nil
}
