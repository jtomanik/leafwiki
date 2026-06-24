package markdownvalidation

import (
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/perber/wiki/internal/core/markdown"
	"github.com/perber/wiki/internal/core/markdownlinks"
	sharederrors "github.com/perber/wiki/internal/core/shared/errors"
	"github.com/perber/wiki/internal/core/tree"
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/text"
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
	err := filepath.WalkDir(rootDir, func(filePath string, entry os.DirEntry, walkErr error) error {
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
			route, err := tree.MapWorkspaceMarkdownRoute(rootDir, relPath, true)
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
		route, err := tree.MapWorkspaceMarkdownRoute(rootDir, relPath, false)
		if err != nil {
			addIssue(IssueSeverityError, IssueCodeInvalidSlug, relPath, "", err.Error())
			return nil
		}
		if route.Skip {
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
			if mdFile, err := markdown.NewMarkdownFileFromRaw(relPath, raw); err == nil {
				if _, err := mdFile.GetTitle(); err != nil {
					addIssue(IssueSeverityError, IssueCodeMissingTitle, relPath, existingPageID, err.Error())
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
		linkResolver := func(routePath tree.RoutePath) (tree.PageID, bool) {
			if id, ok := workspaceLinkPageIDForRoute(filesByRoute, routePath, tree.NodeKindPage); ok {
				return id, true
			}
			if id, ok := workspaceLinkPageIDForRoute(filesByRoute, routePath, tree.NodeKindSection); ok {
				return id, true
			}
			return "", false
		}
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
			ResolveLinkPageID:      linkResolver,
			ResolveMarkdownLink:    markdownLinkResolver,
			MarkdownLinkRootPrefix: opts.MarkdownLinkRootPrefix,
			ResolveReferencePath: func(destination string) string {
				return resolveWorkspaceReferencePath(file.RelPath, file.RoutePath, destination)
			},
			AssetExists: assetExists,
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
			case markdownlinks.IssueCodeAmbiguousLegacyLink:
				return "", "", false, IssueCodeAmbiguousLegacyLink
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

func validateMarkdownReferences(routePath tree.RoutePath, body string, opts ContentValidationOptions) []Issue {
	if opts.ResolvePageID == nil && opts.ResolveLinkPageID == nil && opts.ResolveMarkdownLink == nil && opts.AssetExists == nil {
		return nil
	}
	resolvePageID := opts.ResolveLinkPageID
	if resolvePageID == nil {
		resolvePageID = opts.ResolvePageID
	}
	issues := []Issue{}
	for _, ref := range markdownReferences(body) {
		if shouldIgnoreDestination(ref.Destination) {
			continue
		}
		assetDestination := stripMarkdownLinkRootPrefix(ref.Destination, opts.MarkdownLinkRootPrefix)
		if ref.Image || isAssetDestination(assetDestination) {
			if opts.AssetExists != nil && !opts.AssetExists(assetDestination) {
				issues = append(issues, Issue{
					Severity:  IssueSeverityError,
					Code:      IssueCodeMissingAsset,
					MessageID: IssueCodeMissingAsset.MessageID(),
					RoutePath: routePath,
					PageID:    opts.ExistingPageID,
					Message:   "asset reference does not resolve: " + ref.Destination,
				})
			}
			continue
		}
		if opts.ResolveMarkdownLink != nil {
			targetPageID, kind, ok, code := opts.ResolveMarkdownLink(ref.Destination)
			if !ok {
				if code == "" {
					code = IssueCodeBrokenLink
				}
				issues = append(issues, Issue{
					Severity:     "error",
					Code:         code,
					MessageID:    code.MessageID(),
					RoutePath:    routePath,
					PageID:       opts.ExistingPageID,
					TargetPageID: targetPageID,
					Message:      "wiki link does not resolve: " + ref.Destination,
				})
				continue
			}
			if kind == tree.NodeKindPage && isExtensionlessWikiDestination(ref.Destination) {
				issues = append(issues, Issue{
					Severity:     IssueSeverityError,
					Code:         IssueCodeNonCanonicalLink,
					MessageID:    IssueCodeNonCanonicalLink.MessageID(),
					RoutePath:    routePath,
					PageID:       opts.ExistingPageID,
					TargetPageID: targetPageID,
					Message:      "page link must use .md: " + ref.Destination,
				})
			}
			continue
		}
		if resolvePageID == nil {
			continue
		}
		resolved := ""
		if opts.ResolveReferencePath != nil {
			resolved = opts.ResolveReferencePath(ref.Destination)
		} else {
			resolved = resolveReferencePath(routePath, ref.Destination)
		}
		if resolved == "" {
			continue
		}
		targetRoutePath := strings.TrimPrefix(resolved, "/")
		var parsedTargetRoutePath tree.RoutePath
		if strings.Trim(targetRoutePath, "/") != "" {
			var err error
			parsedTargetRoutePath, err = tree.ParseRoutePath(targetRoutePath)
			if err != nil {
				issues = append(issues, Issue{
					Severity:  IssueSeverityError,
					Code:      IssueCodeBrokenLink,
					MessageID: IssueCodeBrokenLink.MessageID(),
					RoutePath: routePath,
					PageID:    opts.ExistingPageID,
					Message:   "wiki link does not resolve: " + resolved,
				})
				continue
			}
		}
		if opts.ResolveLinkTarget != nil {
			_, kind, ok := opts.ResolveLinkTarget(parsedTargetRoutePath)
			if !ok {
				issues = append(issues, Issue{
					Severity:  IssueSeverityError,
					Code:      IssueCodeBrokenLink,
					MessageID: IssueCodeBrokenLink.MessageID(),
					RoutePath: routePath,
					PageID:    opts.ExistingPageID,
					Message:   "wiki link does not resolve: " + resolved,
				})
				continue
			}
			if kind == tree.NodeKindPage && isExtensionlessWikiDestination(ref.Destination) {
				issues = append(issues, Issue{
					Severity:  IssueSeverityError,
					Code:      IssueCodeNonCanonicalLink,
					MessageID: IssueCodeNonCanonicalLink.MessageID(),
					RoutePath: routePath,
					PageID:    opts.ExistingPageID,
					Message:   "page link must use .md: " + ref.Destination,
				})
			}
			continue
		}
		if _, ok := resolvePageID(parsedTargetRoutePath); !ok {
			issues = append(issues, Issue{
				Severity:  IssueSeverityError,
				Code:      IssueCodeBrokenLink,
				MessageID: IssueCodeBrokenLink.MessageID(),
				RoutePath: routePath,
				PageID:    opts.ExistingPageID,
				Message:   "wiki link does not resolve: " + resolved,
			})
		}
	}
	return issues
}

func stripMarkdownLinkRootPrefix(destination string, prefix string) string {
	trimmedPrefix := strings.TrimRight(strings.TrimSpace(prefix), "/")
	if trimmedPrefix == "" || !strings.HasPrefix(destination, "/") {
		return destination
	}
	if !strings.HasPrefix(trimmedPrefix, "/") {
		trimmedPrefix = "/" + trimmedPrefix
	}
	if destination == trimmedPrefix {
		return "/"
	}
	if strings.HasPrefix(destination, trimmedPrefix+"/") {
		return destination[len(trimmedPrefix):]
	}
	return destination
}

func isExtensionlessWikiDestination(destination string) bool {
	dest := cleanDestination(destination)
	if dest == "" || strings.HasSuffix(dest, "/") {
		return false
	}
	return path.Ext(dest) == ""
}

type markdownReference struct {
	Destination string
	Image       bool
}

var parser = goldmark.New()

func markdownReferences(content string) []markdownReference {
	refs := []markdownReference{}
	reader := text.NewReader([]byte(content))
	doc := parser.Parser().Parse(reader)
	_ = ast.Walk(doc, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		switch typed := n.(type) {
		case *ast.Link:
			refs = append(refs, markdownReference{Destination: string(typed.Destination)})
		case *ast.Image:
			refs = append(refs, markdownReference{Destination: string(typed.Destination), Image: true})
		}
		return ast.WalkContinue, nil
	})
	return refs
}

func resolveWorkspaceReferencePath(relPath tree.MarkdownPath, routePath tree.RoutePath, destination string) string {
	dest := cleanDestination(destination)
	if dest == "" {
		return ""
	}
	if isMarkdownFileDestination(dest) {
		if strings.HasPrefix(dest, "/") {
			return "/" + tree.MarkdownPathToRoutePath(strings.TrimPrefix(dest, "/"))
		}
		baseDir := relPath.SourceDir().FilesystemPath()
		return "/" + tree.MarkdownPathToRoutePath(path.Join(baseDir, dest))
	}
	return resolveReferencePath(routePath, dest)
}

func isMarkdownFileDestination(destination string) bool {
	return strings.EqualFold(path.Ext(cleanDestination(destination)), ".md")
}

type workspaceValidationRouteKey struct {
	RoutePath tree.RoutePath
	Kind      tree.NodeKind
}

func workspaceValidationRouteConflictKey(routePath tree.RoutePath, kind tree.NodeKind) workspaceValidationRouteKey {
	return workspaceValidationRouteKey{
		RoutePath: routePath.Lower(),
		Kind:      kind,
	}
}

func workspaceValidationRelPath(rootDir string, filePath string) string {
	relPath, err := filepath.Rel(rootDir, filePath)
	if err != nil {
		return filepath.ToSlash(filePath)
	}
	return filepath.ToSlash(relPath)
}

func shouldIgnoreDestination(destination string) bool {
	dest := cleanDestination(destination)
	lower := strings.ToLower(dest)
	return dest == "" ||
		strings.HasPrefix(lower, "http://") ||
		strings.HasPrefix(lower, "https://") ||
		strings.HasPrefix(lower, "mailto:") ||
		strings.HasPrefix(lower, "#")
}

func isAssetDestination(destination string) bool {
	dest := strings.TrimPrefix(cleanDestination(destination), "/")
	if strings.HasPrefix(dest, "assets/") {
		return true
	}
	ext := strings.ToLower(path.Ext(dest))
	return ext != "" && ext != ".md" && ext != ".markdown"
}

func cleanDestination(destination string) string {
	dest := strings.TrimSpace(destination)
	dest = strings.TrimPrefix(dest, "<")
	dest = strings.TrimSuffix(dest, ">")
	if idx := strings.Index(dest, "#"); idx != -1 {
		dest = dest[:idx]
	}
	if idx := strings.Index(dest, "?"); idx != -1 {
		dest = dest[:idx]
	}
	return dest
}

func resolveReferencePath(routePath tree.RoutePath, destination string) string {
	dest := cleanDestination(destination)
	if dest == "" {
		return ""
	}
	basePath := routePath.WikiPath()
	if basePath != "/" && !strings.HasSuffix(basePath, "/") {
		basePath += "/"
	}
	base, err := url.Parse("https://leafwiki.local" + basePath)
	if err != nil {
		return ""
	}
	ref, err := url.Parse(dest)
	if err != nil {
		return ""
	}
	resolved := base.ResolveReference(ref)
	out := path.Clean(resolved.Path)
	if out == "." || out == "/" {
		return ""
	}
	return "/" + strings.Trim(out, "/")
}

func resultFromIssues(issues []Issue) Result {
	summary := Summary{}
	for _, issue := range issues {
		switch issue.Severity {
		case IssueSeverityWarning:
			summary.Warnings++
		default:
			summary.Errors++
		}
	}
	return Result{
		OK:      summary.Errors == 0,
		Summary: summary,
		Issues:  issues,
	}
}
