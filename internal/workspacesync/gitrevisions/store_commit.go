package gitrevisions

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	git "github.com/go-git/go-git/v6"
	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/object"

	"github.com/perber/wiki/internal/core/identity"
)

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
	wt, err := gitRevisionRepoWorktree(s.repo)
	if err != nil {
		return nil, err
	}
	changedMarkdownPaths, err := gitRevisionStoreStageMarkdownChanges(s, ctx, wt)
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
	hash, err := gitRevisionWorktreeCommit(wt, commitMessage(req, batchID, messageChangedMarkdownPaths), &git.CommitOptions{
		Author:    signature(req.Actor),
		Committer: leafWikiCommitter(),
		Amend:     amend,
	})
	if errors.Is(err, git.ErrEmptyCommit) {
		head, headErr := gitRevisionRepoHead(s.repo)
		if req.Reason == ReasonRestore && !amend {
			hash, err = gitRevisionWorktreeCommit(wt, commitMessage(req, batchID, messageChangedMarkdownPaths), &git.CommitOptions{
				Author:            signature(req.Actor),
				Committer:         leafWikiCommitter(),
				AllowEmptyCommits: true,
			})
			if err != nil {
				return nil, fmt.Errorf("commit empty restore snapshot: %w", err)
			}
			return newCommitResult(CommitHashFromPlumbingHash(hash), batchID, messageChangedMarkdownPaths, true), nil
		}
		if errors.Is(headErr, plumbing.ErrReferenceNotFound) && !amend {
			hash, err = gitRevisionWorktreeCommit(wt, commitMessage(req, batchID, messageChangedMarkdownPaths), &git.CommitOptions{
				Author:            signature(req.Actor),
				Committer:         leafWikiCommitter(),
				AllowEmptyCommits: true,
			})
			if err != nil {
				return nil, fmt.Errorf("commit empty initial markdown snapshot: %w", err)
			}
			return newCommitResult(CommitHashFromPlumbingHash(hash), batchID, messageChangedMarkdownPaths, true), nil
		}
		if headErr != nil {
			return nil, err
		}
		return newCommitResult(CommitHashFromPlumbingHash(head.Hash()), batchID, nil, false), nil
	}
	if err != nil {
		return nil, fmt.Errorf("commit markdown snapshot: %w", err)
	}
	return newCommitResult(CommitHashFromPlumbingHash(hash), batchID, messageChangedMarkdownPaths, true), nil
}

func newCommitResult(hash identity.CommitHash, batchID string, changedMarkdownPaths []string, created bool) *Commit {
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
	paths, err := gitRevisionCollectMarkdownPaths(s.rootDir)
	if err != nil {
		return nil, err
	}
	trackedFiles, err := gitRevisionStoreTrackedMarkdownFiles(s)
	if err != nil {
		return nil, err
	}
	changed := make(map[string]struct{})
	for _, path := range paths {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		raw, err := gitRevisionReadFile(filepath.Join(s.rootDir, filepath.FromSlash(path)))
		if err != nil {
			return nil, fmt.Errorf("read markdown %s: %w", path, err)
		}
		if tracked, ok := trackedFiles[path]; !ok || tracked != string(raw) {
			changed[path] = struct{}{}
		}
		if _, err := gitRevisionWorktreeAdd(wt, path); err != nil {
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
			if _, err := gitRevisionWorktreeRemove(wt, path); err != nil {
				return nil, fmt.Errorf("stage markdown delete %s: %w", path, err)
			}
		} else if err := gitRevisionStoreRemoveFromIndexOnly(s, path); err != nil {
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
	idx, err := gitRevisionRepositoryIndex(s.repo)
	if err != nil {
		return err
	}
	if _, err := gitRevisionIndexRemove(idx, path); err != nil {
		return err
	}
	return gitRevisionRepositorySetIndex(s.repo, idx)
}

func collectMarkdownPaths(rootDir string) ([]string, error) {
	var paths []string
	err := gitRevisionWalkDir(rootDir, func(path string, entry os.DirEntry, walkErr error) error {
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
		rel, err := gitRevisionRel(rootDir, path)
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
	head, err := gitRevisionRepoHead(s.repo)
	if errors.Is(err, plumbing.ErrReferenceNotFound) {
		return files, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read internal git HEAD: %w", err)
	}
	commit, err := gitRevisionRepoCommitObject(s.repo, head.Hash())
	if err != nil {
		return nil, fmt.Errorf("load HEAD commit: %w", err)
	}
	tree, err := gitRevisionCommitTree(commit)
	if err != nil {
		return nil, fmt.Errorf("load HEAD tree: %w", err)
	}
	iter := gitRevisionTreeFiles(tree)
	defer iter.Close()
	if err := gitRevisionFileIterForEach(iter, func(file *object.File) error {
		if isTrackedMarkdownRelPath(file.Name) {
			reader, err := gitRevisionFileReader(file)
			if err != nil {
				return err
			}
			raw, readErr := gitRevisionReadAll(reader)
			closeErr := reader.Close()
			if readErr != nil {
				if closeErr != nil {
					return errors.Join(readErr, closeErr)
				}
				return readErr
			}
			if closeErr != nil {
				return closeErr
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

func commitActorIDs(req CommitRequest) []ActorID {
	primaryID := TrimActorID(req.Actor.ID)
	if primaryID == "" {
		primaryID = PublicEditorActor().ID
	}
	actorIDs := []ActorID{primaryID}
	seen := map[ActorID]struct{}{primaryID: {}}
	for _, actor := range req.AdditionalActors {
		actorID := TrimActorID(actor.ID)
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
	if _, err := gitRevisionRandRead(raw[:]); err != nil {
		return fmt.Sprintf("%d", time.Now().UTC().UnixNano())
	}
	return hex.EncodeToString(raw[:])
}

func signature(actor Actor) *object.Signature {
	if TrimActorID(actor.ID) == "" {
		actor = PublicEditorActor()
	}
	name := strings.TrimSpace(actor.Name)
	if name == "" {
		name = ActorIDGitSignatureName(actor.ID)
	}
	email := strings.TrimSpace(actor.Email)
	if email == "" {
		email = ActorIDGitSignatureEmail(actor.ID)
	}
	return &object.Signature{Name: name, Email: email, When: time.Now().UTC()}
}

func leafWikiCommitter() *object.Signature {
	return &object.Signature{Name: "LeafWiki", Email: "leafwiki@leafwiki.local", When: time.Now().UTC()}
}
