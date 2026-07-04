package workspacesync

import (
	"context"
	"path/filepath"
	"strings"

	"github.com/perber/wiki/internal/workspacesync/gitrevisions"
)

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
		nextCursor = commits[requestedLimit-1].Hash
		commits = commits[:requestedLimit]
	}
	snapshots := make([]Snapshot, 0, len(commits))
	for _, commit := range commits {
		s.storeMu.Lock()
		changedPaths, err := store.ChangedMarkdownPaths(ctx, commit.Hash)
		s.storeMu.Unlock()
		if err != nil {
			return SnapshotList{}, err
		}
		snapshots = append(snapshots, Snapshot{
			ID:                   commit.Hash,
			Message:              commit.Message,
			AuthorID:             commit.AuthorID,
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
