package gitrevisions

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/go-git/go-git/v6/plumbing/object"

	"github.com/perber/wiki/internal/core/identity"
)

func (s *Store) RestoreWorkspace(ctx context.Context, commitHash identity.CommitHash, req CommitRequest) (*Commit, error) {
	files, err := gitRevisionStoreFilesAt(s, ctx, commitHash)
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
		if err := gitRevisionMkdirAll(filepath.Dir(fullPath), 0o755); err != nil {
			return nil, fmt.Errorf("create restore parent %s: %w", relPath, err)
		}
		if err := gitRevisionWriteFile(fullPath, []byte(content), 0o644); err != nil {
			return nil, fmt.Errorf("write restored markdown %s: %w", relPath, err)
		}
	}
	current, err := gitRevisionCollectMarkdownPaths(s.rootDir)
	if err != nil {
		return nil, err
	}
	for _, relPath := range current {
		if _, keep := target[relPath]; keep {
			continue
		}
		if err := gitRevisionRemove(filepath.Join(s.rootDir, filepath.FromSlash(relPath))); err != nil && !errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("remove markdown absent from restore %s: %w", relPath, err)
		}
	}
	if req.Reason == "" {
		req.Reason = ReasonRestore
	}
	if req.Source == "" {
		req.Source = SourceSystem
	}
	return gitRevisionStoreCapture(s, ctx, req)
}

func (s *Store) RestoreDocument(ctx context.Context, relPath string, commitHash identity.CommitHash, req CommitRequest) (*Commit, error) {
	return s.RestoreDocumentToPath(ctx, relPath, relPath, commitHash, req)
}

func (s *Store) RestoreDocumentToPath(ctx context.Context, targetRelPath string, sourceRelPath string, commitHash identity.CommitHash, req CommitRequest) (*Commit, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	targetRelPath = filepath.ToSlash(filepath.Clean(strings.TrimSpace(targetRelPath)))
	if targetRelPath == "." || strings.HasPrefix(targetRelPath, "../") || !isManagedMarkdownRelPath(targetRelPath) {
		return nil, fmt.Errorf("%w: %s", ErrDocumentRestorePathInvalid, targetRelPath)
	}
	sourceRelPath = filepath.ToSlash(filepath.Clean(strings.TrimSpace(sourceRelPath)))
	if sourceRelPath == "." || strings.HasPrefix(sourceRelPath, "../") || !isManagedMarkdownRelPath(sourceRelPath) {
		return nil, fmt.Errorf("%w: %s", ErrDocumentRestoreSourcePathInvalid, sourceRelPath)
	}
	content, err := gitRevisionStoreFileContentAt(s, ctx, commitHash, sourceRelPath)
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
		return nil, fmt.Errorf("%w: %s", ErrDocumentRestorePathInvalid, targetRelPath)
	}
	fullPath := filepath.Join(s.rootDir, filepath.FromSlash(targetRelPath))
	if err := gitRevisionMkdirAll(filepath.Dir(fullPath), 0o755); err != nil {
		return nil, fmt.Errorf("create restore parent %s: %w", targetRelPath, err)
	}
	if err := gitRevisionWriteFile(fullPath, []byte(content), 0o644); err != nil {
		return nil, fmt.Errorf("write restored markdown %s: %w", targetRelPath, err)
	}
	if req.Reason == "" {
		req.Reason = ReasonRestore
	}
	if req.Source == "" {
		req.Source = SourceSystem
	}
	return gitRevisionStoreCapture(s, ctx, req)
}

func (s *Store) fileContentAt(ctx context.Context, commitHash identity.CommitHash, relPath string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	hash := PlumbingHashFromCommitHash(commitHash)
	commit, err := gitRevisionRepoCommitObject(s.repo, hash)
	if err != nil {
		return "", fmt.Errorf("load commit %s: %w", commitHash, err)
	}
	tree, err := gitRevisionCommitTree(commit)
	if err != nil {
		return "", fmt.Errorf("load commit tree: %w", err)
	}
	file, err := gitRevisionTreeFile(tree, relPath)
	if err != nil {
		return "", fmt.Errorf("document %s is not present in commit %s: %w", relPath, commitHash, err)
	}
	content, err := gitRevisionFileContents(file)
	if err != nil {
		return "", fmt.Errorf("read document %s from commit %s: %w", relPath, commitHash, err)
	}
	return content, nil
}

func (s *Store) FilesAt(ctx context.Context, commitHash identity.CommitHash) (map[string]string, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	hash := PlumbingHashFromCommitHash(commitHash)
	commit, err := gitRevisionRepoCommitObject(s.repo, hash)
	if err != nil {
		return nil, fmt.Errorf("load commit %s: %w", commitHash, err)
	}
	return s.filesAtCommit(ctx, commit)
}

func (s *Store) filesAtCommit(ctx context.Context, commit *object.Commit) (map[string]string, error) {
	tree, err := gitRevisionCommitTree(commit)
	if err != nil {
		return nil, fmt.Errorf("load commit tree: %w", err)
	}
	files := make(map[string]string)
	iter := gitRevisionTreeFiles(tree)
	defer iter.Close()
	err = gitRevisionFileIterForEach(iter, func(file *object.File) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if !isManagedMarkdownRelPath(file.Name) {
			return nil
		}
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
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("read commit files: %w", err)
	}
	return files, nil
}
