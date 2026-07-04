package workspacesync

import (
	"path/filepath"
	"strings"

	wikivalidation "github.com/perber/wiki/internal/core/markdownvalidation"
)

func (s *Service) validateWorkspaceMarkdownFiles() []ValidationError {
	result := wikivalidation.ValidateWorkspaceMarkdownFiles(wikivalidation.WorkspaceMarkdownValidationOptions{
		RootDir:                s.rootDir,
		MarkdownLinkRootPrefix: s.markdownLinkRootPrefix,
		IncludeWarnings:        false,
	})
	if len(result.Issues) == 0 {
		return nil
	}
	validationErrors := make([]ValidationError, 0, len(result.Issues))
	for _, issue := range result.Issues {
		validationErrors = append(validationErrors, ValidationError{
			Code:      issue.Code,
			MessageID: issue.Code.MessageID(),
			Path:      markdownValidationIssuePath(issue),
			Message:   issue.Message,
			Severity:  issue.Severity,
		})
	}
	return validationErrors
}

func markdownValidationIssuePath(issue wikivalidation.Issue) string {
	if issue.SourcePath != "" {
		return issue.SourcePath.FilesystemPath()
	}
	return issue.RoutePath.FilesystemPath()
}

func (s *Service) validationErrorsFromError(err error) []ValidationError {
	if err == nil {
		return nil
	}
	if validationErrors := s.validateWorkspaceMarkdownFiles(); validationErrorsIncludeActionableMarkdownCode(validationErrors) {
		return validationErrors
	}
	message := err.Error()
	paths := markdownPathsInError(s.rootDir, message)
	if len(paths) == 0 {
		return []ValidationError{{
			Code:      wikivalidation.IssueCodeWorkspaceSyncError,
			MessageID: wikivalidation.IssueCodeWorkspaceSyncError.MessageID(),
			Path:      "workspace",
			Message:   message,
			Severity:  wikivalidation.IssueSeverityError,
		}}
	}
	errors := make([]ValidationError, 0, len(paths))
	for _, path := range paths {
		errors = append(errors, ValidationError{
			Code:      wikivalidation.IssueCodeWorkspaceSyncError,
			MessageID: wikivalidation.IssueCodeWorkspaceSyncError.MessageID(),
			Path:      path,
			Message:   message,
			Severity:  wikivalidation.IssueSeverityError,
		})
	}
	return errors
}

func validationErrorsIncludeActionableMarkdownCode(errors []ValidationError) bool {
	for _, validationError := range errors {
		switch validationError.Code {
		case "", wikivalidation.IssueCodeWorkspaceScanError, wikivalidation.IssueCodeWorkspaceSyncError, wikivalidation.IssueCodeWorkspaceSyncValidation:
			continue
		default:
			return true
		}
	}
	return false
}

func markdownPathsInError(rootDir string, message string) []string {
	seen := make(map[string]struct{})
	var paths []string
	for _, token := range strings.Fields(message) {
		candidate := cleanErrorTokenMarkdownPath(token)
		if candidate == "" {
			continue
		}
		path := normalizeValidationPath(rootDir, candidate)
		if path == "" {
			continue
		}
		if _, ok := seen[path]; ok {
			continue
		}
		seen[path] = struct{}{}
		paths = append(paths, path)
	}
	return paths
}

func cleanErrorTokenMarkdownPath(token string) string {
	cleaned := strings.Trim(token, "\"'`()[]{},;:")
	cleaned = strings.TrimSuffix(cleaned, ".")
	index := strings.Index(strings.ToLower(cleaned), ".md")
	if index < 0 {
		return ""
	}
	cleaned = cleaned[:index+len(".md")]
	for _, prefix := range []string{"path=", "file="} {
		cleaned = strings.TrimPrefix(cleaned, prefix)
	}
	cleaned = strings.Trim(cleaned, "\"'`()[]{},;:")
	return strings.TrimSuffix(cleaned, ".")
}

func normalizeValidationPath(rootDir string, candidate string) string {
	candidate = filepath.Clean(candidate)
	if filepath.IsAbs(candidate) {
		if rootDir != "" {
			if rel, err := filepath.Rel(rootDir, candidate); err == nil && rel != "." && !strings.HasPrefix(rel, "..") {
				return filepath.ToSlash(rel)
			}
		}
		return filepath.ToSlash(candidate)
	}
	candidate = filepath.ToSlash(candidate)
	candidate = strings.TrimPrefix(candidate, "./")
	if candidate == "." || strings.HasPrefix(candidate, "../") {
		return ""
	}
	return candidate
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}
