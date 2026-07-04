package workspacesync

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/perber/wiki/internal/core/markdownlinks"
	wikivalidation "github.com/perber/wiki/internal/core/markdownvalidation"
	"github.com/perber/wiki/internal/core/tree"
	"github.com/perber/wiki/internal/workspacesync/gitrevisions"
)

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
		if validationErrors := s.validationErrorsFromError(err); validationErrorsIncludeActionableMarkdownCode(validationErrors) {
			s.status.ValidationErrors = mergeValidationErrors(s.status.ValidationErrors, validationErrors)
			if strings.TrimSpace(s.status.LastError) == "" {
				s.status.LastError = validationErrors[0].Message
			}
			return false, nil, nil
		}
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
				return fmt.Errorf("write canonical markdown migration: %w; rollback failed: %w", err, rollbackErr)
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
