package workspacesync

import (
	"context"
	"fmt"
	"time"

	"github.com/perber/wiki/internal/core/revision"
	"github.com/perber/wiki/internal/core/tree"
	"github.com/perber/wiki/internal/workspacesync/gitrevisions"
)

func (s *Service) RestoreWorkspace(ctx context.Context, commitID CommitHash, actor Actor) (SyncStatus, error) {
	return s.RestoreWorkspaceWithSource(ctx, commitID, actor, SourceSystem)
}

func (s *Service) RestoreWorkspaceWithSource(ctx context.Context, commitID CommitHash, actor Actor, source Source) (SyncStatus, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.enabled {
		return s.status, ErrWorkspaceSyncDisabled
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
	s.status.LastCommitHash = commit.Hash
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
		return nil, ErrWorkspaceSyncDisabled
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
		return nil, fmt.Errorf("document %s did not change in commit %s: %w", relPath, commitID, ErrWorkspaceSyncDocumentUnchanged)
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
		return s.status, ErrWorkspaceSyncDisabled
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
		err := fmt.Errorf("document %s did not change in commit %s: %w", targetRelPath, commitID, ErrWorkspaceSyncDocumentUnchanged)
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
	s.status.LastCommitHash = commit.Hash
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
