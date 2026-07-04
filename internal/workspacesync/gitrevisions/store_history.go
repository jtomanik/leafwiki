package gitrevisions

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"

	git "github.com/go-git/go-git/v6"
	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/object"

	"github.com/perber/wiki/internal/core/identity"
)

var stopCommitIteration = errors.New("stop commit iteration")

func (s *Store) ListCommits(ctx context.Context, req ListRequest) ([]Commit, error) {
	limit := req.Limit
	if limit <= 0 {
		limit = 50
	}
	commits := make([]Commit, 0, limit)
	cursor := req.Cursor
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
	iter, err := gitRevisionRepoLog(s.repo, &git.LogOptions{})
	if errors.Is(err, plumbing.ErrReferenceNotFound) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("list commits: %w", err)
	}
	defer iter.Close()

	err = gitRevisionCommitIterForEach(iter, func(commit *object.Commit) error {
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

func (s *Store) GetCommit(ctx context.Context, commitHash identity.CommitHash) (Commit, error) {
	if err := ctx.Err(); err != nil {
		return Commit{}, err
	}
	commit, err := gitRevisionRepoCommitObject(s.repo, PlumbingHashFromCommitHash(commitHash))
	if err != nil {
		return Commit{}, fmt.Errorf("load commit %s: %w", commitHash, err)
	}
	return commitFromObject(commit), nil
}

func (s *Store) ChangedMarkdownPaths(ctx context.Context, commitHash identity.CommitHash) ([]string, error) {
	paths, _, err := s.changedMarkdownEntries(ctx, commitHash)
	return paths, err
}

func (s *Store) ChangedMarkdownContents(ctx context.Context, commitHash identity.CommitHash) (map[string]string, error) {
	_, contents, err := s.changedMarkdownEntries(ctx, commitHash)
	return contents, err
}

func (s *Store) changedMarkdownEntries(ctx context.Context, commitHash identity.CommitHash) ([]string, map[string]string, error) {
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	commit, err := gitRevisionRepoCommitObject(s.repo, PlumbingHashFromCommitHash(commitHash))
	if err != nil {
		return nil, nil, fmt.Errorf("load commit %s: %w", commitHash, err)
	}
	currentTree, err := gitRevisionCommitTree(commit)
	if err != nil {
		return nil, nil, fmt.Errorf("load commit tree: %w", err)
	}
	paths := make(map[string]struct{})
	contents := make(map[string]string)

	parentIter := gitRevisionCommitParents(commit)
	parent, err := gitRevisionCommitIterNext(parentIter)
	if errors.Is(err, object.ErrParentNotFound) || errors.Is(err, io.EOF) {
		iter := gitRevisionTreeFiles(currentTree)
		defer iter.Close()
		if err := gitRevisionFileIterForEach(iter, func(file *object.File) error {
			if err := ctx.Err(); err != nil {
				return err
			}
			if !isManagedMarkdownRelPath(file.Name) {
				return nil
			}
			content, err := gitRevisionFileContents(file)
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
	parentTree, err := gitRevisionCommitTree(parent)
	if err != nil {
		return nil, nil, fmt.Errorf("load parent tree: %w", err)
	}

	changes, err := gitRevisionTreeDiffContext(parentTree, ctx, currentTree)
	if err != nil {
		return nil, nil, fmt.Errorf("diff commit %s: %w", commitHash, err)
	}
	for _, change := range changes {
		if err := ctx.Err(); err != nil {
			return nil, nil, err
		}
		from, to, err := gitRevisionChangeFiles(change)
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
		content, err := gitRevisionFileContents(to)
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
	var authorID ActorID
	if len(actorIDs) > 0 {
		authorID = actorIDs[0]
	}
	return Commit{
		Hash:                 CommitHashFromPlumbingHash(commit.Hash),
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

func parseCommitMessage(message string) (string, map[string]string, []ActorID) {
	lines := strings.Split(message, "\n")
	title := ""
	if len(lines) > 0 {
		title = strings.TrimSpace(lines[0])
	}
	trailers := make(map[string]string)
	var actorIDs []ActorID
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
			actorIDs = append(actorIDs, ParseActorID(value))
			if _, exists := trailers[key]; !exists {
				trailers[key] = value
			}
			continue
		}
		trailers[key] = value
	}
	return title, trailers, actorIDs
}
