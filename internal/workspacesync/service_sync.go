package workspacesync

import (
	"context"
	"strings"
	"time"

	"github.com/perber/wiki/internal/core/markdown"
	wikivalidation "github.com/perber/wiki/internal/core/markdownvalidation"
	sharederrors "github.com/perber/wiki/internal/core/shared/errors"
	"github.com/perber/wiki/internal/workspacesync/gitrevisions"
)

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
	s.status.LastCommitHash = commit.Hash
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
		requiresMigrationWriteback, err := capturedMarkdownRequiresMetadataWriteback(ctx, s.store, commit.Hash)
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
