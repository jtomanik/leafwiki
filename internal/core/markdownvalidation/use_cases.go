package markdownvalidation

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/perber/wiki/internal/core/markdown"
	"github.com/perber/wiki/internal/core/markdownlinks"
	sharederrors "github.com/perber/wiki/internal/core/shared/errors"
	"github.com/perber/wiki/internal/core/tree"
)

type Issue struct {
	Severity     IssueSeverity
	Code         IssueCode
	SourcePath   tree.MarkdownPath
	RoutePath    tree.RoutePath
	PageID       tree.PageID
	TargetPageID tree.PageID
	MessageID    sharederrors.MessageID
	Message      string
}

type Summary struct {
	Errors   int
	Warnings int
}

type Result struct {
	OK      bool
	Summary Summary
	Issues  []Issue
}

type ContentValidationOptions struct {
	ExistingPageID         tree.PageID
	AllowRootRoute         bool
	ResolvePageID          func(routePath tree.RoutePath) (tree.PageID, bool)
	ResolveLinkPageID      func(routePath tree.RoutePath) (tree.PageID, bool)
	ResolveLinkTarget      func(routePath tree.RoutePath) (tree.PageID, tree.NodeKind, bool)
	ResolveMarkdownLink    func(destination string) (tree.PageID, tree.NodeKind, bool, IssueCode)
	ResolveReferencePath   func(destination string) string
	PageIDExists           func(pageID tree.PageID) bool
	AssetExists            func(destination string) bool
	MarkdownLinkRootPrefix string
}

type WorkspaceMarkdownValidationOptions struct {
	RootDir                string
	MarkdownLinkRootPrefix string
	IncludeWarnings        bool
	PageIDExists           func(pageID tree.PageID) bool
	AssetExists            func(pageID tree.PageID, destination string) bool
}

type WorkspaceStatusIssue struct {
	Code      IssueCode
	Path      string
	MessageID sharederrors.MessageID
	Message   string
	Severity  IssueSeverity
}

var (
	walkWorkspaceDir          = filepath.WalkDir
	mapWorkspaceMarkdownRoute = tree.MapWorkspaceMarkdownRoute
)

func ValidateMarkdownContent(routePath string, content string, existingPageID string) Result {
	return ValidateMarkdownContentWithOptions(tree.RoutePathFromString(routePath), content, ContentValidationOptions{
		ExistingPageID: tree.PageIDFromString(existingPageID),
	})
}

func ValidateMarkdownContentWithOptions(routePath tree.RoutePath, content string, opts ContentValidationOptions) Result {
	var issues []Issue
	normalizedRoutePath := routePath.Clean()
	if normalizedRoutePath.IsRoot() && !opts.AllowRootRoute {
		issues = append(issues, Issue{
			Severity:  IssueSeverityError,
			Code:      IssueCodeInvalidPath,
			MessageID: IssueCodeInvalidPath.MessageID(),
			RoutePath: normalizedRoutePath,
			PageID:    opts.ExistingPageID,
			Message:   "missing path",
		})
	} else if !normalizedRoutePath.IsRoot() {
		if validPath, err := normalizedRoutePath.Validate(); err != nil {
			issues = append(issues, Issue{
				Severity:  IssueSeverityError,
				Code:      IssueCodeInvalidPath,
				MessageID: IssueCodeInvalidPath.MessageID(),
				RoutePath: normalizedRoutePath,
				PageID:    opts.ExistingPageID,
				Message:   err.Error(),
			})
		} else {
			normalizedRoutePath = validPath
		}
	}
	if !normalizedRoutePath.IsRoot() && opts.ResolvePageID != nil {
		if pageID, exists := opts.ResolvePageID(normalizedRoutePath); exists && pageID != opts.ExistingPageID {
			issues = append(issues, Issue{
				Severity:  IssueSeverityError,
				Code:      IssueCodePathConflict,
				MessageID: IssueCodePathConflict.MessageID(),
				RoutePath: normalizedRoutePath,
				PageID:    opts.ExistingPageID,
				Message:   "path already belongs to another page",
			})
		}
	}
	doc, _, err := markdown.ParsePageDocument(content)
	if err != nil {
		issues = append(issues, Issue{
			Severity:  IssueSeverityError,
			Code:      IssueCodeMetadataParseError,
			MessageID: IssueCodeMetadataParseError.MessageID(),
			RoutePath: normalizedRoutePath,
			PageID:    opts.ExistingPageID,
			Message:   err.Error(),
		})
	}
	if err == nil {
		issues = append(issues, validateMetadata(normalizedRoutePath, opts, doc.Metadata)...)
		issues = append(issues, validateMarkdownReferences(normalizedRoutePath, doc.Body, opts)...)
	}
	return resultFromIssues(issues)
}

func ValidateWorkspaceStatus(statusIssues []WorkspaceStatusIssue, includeWarnings bool) Result {
	issues := make([]Issue, 0, len(statusIssues))
	for _, err := range statusIssues {
		severity := err.Severity.Normalize(IssueSeverityError)
		if !includeWarnings && severity == IssueSeverityWarning {
			continue
		}
		code := err.Code.Normalize(IssueCodeWorkspaceSyncValidation)
		messageID := err.MessageID
		if messageID == "" {
			messageID = code.MessageID()
		}
		issues = append(issues, Issue{
			Severity:   severity,
			Code:       code,
			MessageID:  messageID,
			SourcePath: tree.MarkdownPathFromString(err.Path),
			Message:    err.Message,
		})
	}
	return resultFromIssues(issues)
}

func ValidateWorkspaceMarkdownFiles(opts WorkspaceMarkdownValidationOptions) Result {
	rootDir := strings.TrimSpace(opts.RootDir)
	if rootDir == "" {
		return Result{OK: true}
	}
	type workspaceFile struct {
		RelPath        tree.MarkdownPath
		RoutePath      tree.RoutePath
		Content        string
		ExistingPageID tree.PageID
	}
	seenIDs := map[tree.PageID]string{}
	seenRoutePaths := map[workspaceValidationRouteKey]string{}
	var rootRoutePath tree.RoutePath
	filesByRoute := map[workspaceValidationRouteKey]tree.PageID{
		workspaceValidationRouteConflictKey(rootRoutePath, tree.NodeKindSection): "",
	}
	files := []workspaceFile{}
	issues := []Issue{}
	addIssue := func(severity IssueSeverity, code IssueCode, relPath string, pageID tree.PageID, message string) {
		issues = append(issues, Issue{
			Severity:   severity,
			Code:       code,
			MessageID:  code.MessageID(),
			SourcePath: tree.MarkdownPathFromString(filepath.ToSlash(relPath)),
			PageID:     pageID,
			Message:    message,
		})
	}
	recordRoutePath := func(relPath string, routePath tree.RoutePath, kind tree.NodeKind, allowSectionIndex bool) {
		routeKey := workspaceValidationRouteConflictKey(routePath, kind)
		if routeKey.RoutePath == "" {
			return
		}
		if firstPath, exists := seenRoutePaths[routeKey]; exists {
			if allowSectionIndex && firstPath == strings.Trim(strings.TrimSuffix(filepath.ToSlash(filepath.Dir(relPath)), "."), "/") {
				return
			}
			addIssue(IssueSeverityError, IssueCodePathConflict, relPath, "", "route path conflict between "+firstPath+" and "+relPath)
			return
		}
		seenRoutePaths[routeKey] = relPath
	}
	err := walkWorkspaceDir(rootDir, func(filePath string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			relPath := workspaceValidationRelPath(rootDir, filePath)
			addIssue(IssueSeverityError, IssueCodeWorkspaceScanError, relPath, "", walkErr.Error())
			if entry != nil && entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if filePath == rootDir {
			return nil
		}
		name := entry.Name()
		if entry.IsDir() {
			if strings.HasPrefix(name, ".") {
				return filepath.SkipDir
			}
			relPath := workspaceValidationRelPath(rootDir, filePath)
			route, err := mapWorkspaceMarkdownRoute(rootDir, relPath, true)
			if err != nil {
				addIssue(IssueSeverityError, IssueCodeInvalidSlug, relPath, "", err.Error())
				return filepath.SkipDir
			}
			if route.Skip {
				return filepath.SkipDir
			}
			recordRoutePath(relPath, route.RoutePath, tree.NodeKindSection, false)
			filesByRoute[workspaceValidationRouteConflictKey(route.RoutePath, tree.NodeKindSection)] = ""
			return nil
		}
		if !strings.EqualFold(filepath.Ext(name), ".md") {
			return nil
		}
		relPath := workspaceValidationRelPath(rootDir, filePath)
		if strings.HasPrefix(name, ".") {
			if opts.IncludeWarnings {
				addIssue(IssueSeverityWarning, IssueCodeHiddenMarkdownPath, relPath, "", "hidden markdown files are ignored by workspace sync")
			}
			return nil
		}
		route, err := mapWorkspaceMarkdownRoute(rootDir, relPath, false)
		if err != nil {
			addIssue(IssueSeverityError, IssueCodeInvalidSlug, relPath, "", err.Error())
			return nil
		}
		routePath := route.RoutePath
		routeKind := route.Kind
		isSectionIndex := route.Kind == tree.NodeKindSection && route.ContentPath != ""
		recordRoutePath(relPath, routePath, routeKind, isSectionIndex)
		rawBytes, err := os.ReadFile(filePath)
		if err != nil {
			addIssue(IssueSeverityError, IssueCodeWorkspaceScanError, relPath, "", err.Error())
			return nil
		}
		raw := string(rawBytes)
		doc, _, err := markdown.ParsePageDocument(raw)
		var existingPageID tree.PageID
		if err == nil {
			existingPageID = tree.PageIDFromString(strings.TrimSpace(doc.Metadata.Page.ID))
			if existingPageID != "" {
				if firstPath, exists := seenIDs[existingPageID]; exists {
					addIssue(IssueSeverityError, IssueCodeDuplicateLeafwikiID, relPath, existingPageID, "metadata page.id already appears in "+firstPath)
				} else {
					seenIDs[existingPageID] = relPath
				}
			}
		}
		filesByRoute[workspaceValidationRouteConflictKey(routePath, routeKind)] = existingPageID
		files = append(files, workspaceFile{
			RelPath:        tree.MarkdownPathFromString(relPath),
			RoutePath:      routePath,
			Content:        raw,
			ExistingPageID: existingPageID,
		})
		return nil
	})
	if err != nil {
		addIssue(IssueSeverityError, IssueCodeWorkspaceScanError, "workspace", "", err.Error())
	}
	linkIndex, indexErr := markdownlinks.NewIndexFromRootWithOptions(rootDir, markdownlinks.Options{
		MarkdownLinkRootPrefix: opts.MarkdownLinkRootPrefix,
	})
	if indexErr != nil {
		addIssue(IssueSeverityError, IssueCodeWorkspaceScanError, "workspace", "", indexErr.Error())
	}
	for _, file := range files {
		file := file
		markdownLinkResolver := newWorkspaceMarkdownLinkResolver(file.RelPath, linkIndex, filesByRoute)
		var assetExists func(destination string) bool
		if opts.AssetExists != nil {
			assetExists = func(destination string) bool {
				return opts.AssetExists(file.ExistingPageID, destination)
			}
		}
		result := ValidateMarkdownContentWithOptions(file.RoutePath, file.Content, ContentValidationOptions{
			ExistingPageID:         file.ExistingPageID,
			AllowRootRoute:         true,
			ResolveMarkdownLink:    markdownLinkResolver,
			MarkdownLinkRootPrefix: opts.MarkdownLinkRootPrefix,
			AssetExists:            assetExists,
		})
		issues = append(issues, result.Issues...)
	}
	return resultFromIssues(issues)
}

func Combine(results ...Result) Result {
	issues := []Issue{}
	for _, result := range results {
		issues = append(issues, result.Issues...)
	}
	return resultFromIssues(issues)
}

func workspaceLinkPageIDForRoute(filesByRoute map[workspaceValidationRouteKey]tree.PageID, routePath tree.RoutePath, kind tree.NodeKind) (tree.PageID, bool) {
	pageID, ok := filesByRoute[workspaceValidationRouteConflictKey(routePath, kind)]
	if !ok {
		return "", false
	}
	return pageID, true
}

func newWorkspaceMarkdownLinkResolver(sourceRelPath tree.MarkdownPath, linkIndex *markdownlinks.Index, filesByRoute map[workspaceValidationRouteKey]tree.PageID) func(string) (tree.PageID, tree.NodeKind, bool, IssueCode) {
	return func(destination string) (tree.PageID, tree.NodeKind, bool, IssueCode) {
		if linkIndex == nil {
			return "", "", false, IssueCodeBrokenLink
		}
		resolved := linkIndex.Resolve(sourceRelPath, destination)
		switch resolved.Kind {
		case markdownlinks.TargetKindPage:
			if pageID, ok := workspaceLinkPageIDForRoute(filesByRoute, resolved.RoutePath, tree.NodeKindPage); ok {
				return pageID, tree.NodeKindPage, true, ""
			}
			return "", tree.NodeKindPage, false, IssueCodeBrokenLink
		case markdownlinks.TargetKindSection:
			if pageID, ok := workspaceLinkPageIDForRoute(filesByRoute, resolved.RoutePath, tree.NodeKindSection); ok {
				return pageID, tree.NodeKindSection, true, ""
			}
			return "", tree.NodeKindSection, false, IssueCodeBrokenLink
		case markdownlinks.TargetKindInvalid:
			return "", "", false, IssueCodeInvalidLink
		case markdownlinks.TargetKindUnresolved:
			switch resolved.Code {
			case markdownlinks.IssueCodeNonCanonicalMarkdownPath:
				return "", "", false, IssueCodeNonCanonicalMarkdownPath
			}
			return "", "", false, IssueCodeBrokenLink
		default:
			return "", "", true, ""
		}
	}
}

func validateMetadata(routePath tree.RoutePath, opts ContentValidationOptions, meta markdown.PageMetadata) []Issue {
	issues := []Issue{}
	if id := tree.PageIDFromString(strings.TrimSpace(meta.Page.ID)); id != "" && id != opts.ExistingPageID {
		if opts.PageIDExists != nil && opts.PageIDExists(id) {
			issues = append(issues, Issue{
				Severity:  IssueSeverityError,
				Code:      IssueCodeDuplicateLeafwikiID,
				MessageID: IssueCodeDuplicateLeafwikiID.MessageID(),
				RoutePath: routePath,
				PageID:    opts.ExistingPageID,
				Message:   "metadata page.id already belongs to another page",
			})
		}
	}
	for key := range meta.Extra {
		if markdown.IsReservedMetadataKey(key) {
			issues = append(issues, Issue{
				Severity:  IssueSeverityError,
				Code:      IssueCodeReservedMetadata,
				MessageID: IssueCodeReservedMetadata.MessageID(),
				RoutePath: routePath,
				PageID:    opts.ExistingPageID,
				Message:   "metadata key uses reserved leafwiki_ prefix",
			})
		}
	}
	return issues
}
