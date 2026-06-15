package markdownvalidation

import (
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/perber/wiki/internal/core/markdown"
	"github.com/perber/wiki/internal/core/markdownlinks"
	"github.com/perber/wiki/internal/core/tree"
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/text"
)

type Issue struct {
	Severity string
	Code     string
	Path     string
	PageID   string
	Message  string
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
	ExistingPageID         string
	AllowRootRoute         bool
	ResolvePageID          func(routePath string) (string, bool)
	ResolveLinkPageID      func(routePath string) (string, bool)
	ResolveLinkTarget      func(routePath string) (string, tree.NodeKind, bool)
	ResolveMarkdownLink    func(destination string) (string, tree.NodeKind, bool, string)
	ResolveReferencePath   func(destination string) string
	PageIDExists           func(pageID string) bool
	AssetExists            func(destination string) bool
	MarkdownLinkRootPrefix string
}

type WorkspaceMarkdownValidationOptions struct {
	RootDir                string
	MarkdownLinkRootPrefix string
	IncludeWarnings        bool
	PageIDExists           func(pageID string) bool
	AssetExists            func(pageID string, destination string) bool
}

type WorkspaceStatusIssue struct {
	Code     string
	Path     string
	Message  string
	Severity string
}

func ValidateMarkdownContent(routePath string, content string, existingPageID string) Result {
	return ValidateMarkdownContentWithOptions(routePath, content, ContentValidationOptions{
		ExistingPageID: existingPageID,
	})
}

func ValidateMarkdownContentWithOptions(routePath string, content string, opts ContentValidationOptions) Result {
	var issues []Issue
	path := strings.Trim(strings.TrimSpace(routePath), "/")
	if path == "" && !opts.AllowRootRoute {
		issues = append(issues, Issue{
			Severity: "error",
			Code:     "invalid_path",
			Path:     path,
			PageID:   opts.ExistingPageID,
			Message:  "missing path",
		})
	} else if path != "" {
		if _, err := tree.ValidateRoutePath(path); err != nil {
			issues = append(issues, Issue{
				Severity: "error",
				Code:     "invalid_path",
				Path:     path,
				PageID:   opts.ExistingPageID,
				Message:  err.Error(),
			})
		}
	}
	if path != "" && opts.ResolvePageID != nil {
		if pageID, exists := opts.ResolvePageID(path); exists && pageID != opts.ExistingPageID {
			issues = append(issues, Issue{
				Severity: "error",
				Code:     "path_conflict",
				Path:     path,
				PageID:   opts.ExistingPageID,
				Message:  "path already belongs to another page",
			})
		}
	}
	doc, _, err := markdown.ParsePageDocument(content)
	if err != nil {
		issues = append(issues, Issue{
			Severity: "error",
			Code:     "metadata_parse_error",
			Path:     path,
			PageID:   opts.ExistingPageID,
			Message:  err.Error(),
		})
	}
	if err == nil {
		issues = append(issues, validateMetadata(path, opts, doc.Metadata)...)
		issues = append(issues, validateMarkdownReferences(path, doc.Body, opts)...)
	}
	return resultFromIssues(issues)
}

func ValidateWorkspaceStatus(statusIssues []WorkspaceStatusIssue, includeWarnings bool) Result {
	issues := make([]Issue, 0, len(statusIssues))
	for _, err := range statusIssues {
		severity := strings.TrimSpace(err.Severity)
		if severity == "" {
			severity = "error"
		}
		if !includeWarnings && severity == "warning" {
			continue
		}
		code := strings.TrimSpace(err.Code)
		if code == "" {
			code = "workspace_sync_validation"
		}
		issues = append(issues, Issue{
			Severity: severity,
			Code:     code,
			Path:     err.Path,
			Message:  err.Message,
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
		RelPath        string
		RoutePath      string
		Content        string
		ExistingPageID string
	}
	seenIDs := map[string]string{}
	seenRoutePaths := map[string]string{}
	filesByRoute := map[string]string{}
	files := []workspaceFile{}
	issues := []Issue{}
	addIssue := func(severity string, code string, relPath string, pageID string, message string) {
		issues = append(issues, Issue{
			Severity: severity,
			Code:     code,
			Path:     filepath.ToSlash(relPath),
			PageID:   pageID,
			Message:  message,
		})
	}
	recordRoutePath := func(relPath string, routePath string, kind tree.NodeKind, allowSectionIndex bool) {
		routePath = strings.Trim(routePath, "/")
		if routePath == "" {
			return
		}
		routeKey := workspaceValidationRouteConflictKey(routePath, kind)
		if firstPath, exists := seenRoutePaths[routeKey]; exists {
			if allowSectionIndex && firstPath == strings.Trim(strings.TrimSuffix(filepath.ToSlash(filepath.Dir(relPath)), "."), "/") {
				return
			}
			addIssue("error", "path_conflict", relPath, "", "route path conflict between "+firstPath+" and "+relPath)
			return
		}
		seenRoutePaths[routeKey] = relPath
	}
	err := filepath.WalkDir(rootDir, func(filePath string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			relPath := workspaceValidationRelPath(rootDir, filePath)
			addIssue("error", "workspace_scan_error", relPath, "", walkErr.Error())
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
				addIssue("error", "invalid_slug", relPath, "", err.Error())
				return filepath.SkipDir
			}
			if route.Skip {
				return filepath.SkipDir
			}
			recordRoutePath(relPath, route.RoutePath, tree.NodeKindSection, false)
			filesByRoute[workspaceValidationRouteConflictKey(route.RoutePath, tree.NodeKindSection)] = "filesystem:" + strings.Trim(route.RoutePath, "/")
			return nil
		}
		if !strings.EqualFold(filepath.Ext(name), ".md") {
			return nil
		}
		relPath := workspaceValidationRelPath(rootDir, filePath)
		if strings.HasPrefix(name, ".") {
			if opts.IncludeWarnings {
				addIssue("warning", "hidden_markdown_path", relPath, "", "hidden markdown files are ignored by workspace sync")
			}
			return nil
		}
		route, err := tree.MapWorkspaceMarkdownRoute(rootDir, relPath, false)
		if err != nil {
			addIssue("error", "invalid_slug", relPath, "", err.Error())
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
			addIssue("error", "workspace_scan_error", relPath, "", err.Error())
			return nil
		}
		raw := string(rawBytes)
		doc, _, err := markdown.ParsePageDocument(raw)
		existingPageID := ""
		if err == nil {
			existingPageID = strings.TrimSpace(doc.Metadata.Page.ID)
			if existingPageID != "" {
				if firstPath, exists := seenIDs[existingPageID]; exists {
					addIssue("error", "duplicate_leafwiki_id", relPath, existingPageID, "metadata page.id already appears in "+firstPath)
				} else {
					seenIDs[existingPageID] = relPath
				}
			}
			if mdFile, err := markdown.NewMarkdownFileFromRaw(relPath, raw); err == nil {
				if _, err := mdFile.GetTitle(); err != nil {
					addIssue("error", "missing_title", relPath, existingPageID, err.Error())
				}
			}
		}
		linkID := existingPageID
		if linkID == "" {
			linkID = "filesystem:" + strings.Trim(routePath, "/")
		}
		filesByRoute[workspaceValidationRouteConflictKey(routePath, routeKind)] = linkID
		files = append(files, workspaceFile{
			RelPath:        relPath,
			RoutePath:      routePath,
			Content:        raw,
			ExistingPageID: existingPageID,
		})
		return nil
	})
	if err != nil {
		addIssue("error", "workspace_scan_error", "workspace", "", err.Error())
	}
	linkIndex, indexErr := markdownlinks.NewIndexFromRootWithOptions(rootDir, markdownlinks.Options{
		MarkdownLinkRootPrefix: opts.MarkdownLinkRootPrefix,
	})
	if indexErr != nil {
		addIssue("error", "workspace_scan_error", "workspace", "", indexErr.Error())
	}
	for _, file := range files {
		file := file
		linkResolver := func(routePath string) (string, bool) {
			if id, ok := filesByRoute[workspaceValidationRouteConflictKey(routePath, tree.NodeKindPage)]; ok {
				return id, true
			}
			if id, ok := filesByRoute[workspaceValidationRouteConflictKey(routePath, tree.NodeKindSection)]; ok {
				return id, true
			}
			return "", false
		}
		markdownLinkResolver := func(destination string) (string, tree.NodeKind, bool, string) {
			if linkIndex == nil {
				return "", "", false, "broken_link"
			}
			resolved := linkIndex.Resolve(file.RelPath, destination)
			switch resolved.Kind {
			case markdownlinks.TargetKindPage:
				return resolved.RoutePath, tree.NodeKindPage, true, ""
			case markdownlinks.TargetKindSection:
				return resolved.RoutePath, tree.NodeKindSection, true, ""
			case markdownlinks.TargetKindInvalid:
				return resolved.RoutePath, "", false, "invalid_link"
			case markdownlinks.TargetKindUnresolved:
				if resolved.Code == "ambiguous_legacy_link" || resolved.Code == "non_canonical_markdown_path" {
					return resolved.RoutePath, "", false, resolved.Code
				}
				return resolved.RoutePath, "", false, "broken_link"
			default:
				return "", "", true, ""
			}
		}
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

func validateMetadata(routePath string, opts ContentValidationOptions, meta markdown.PageMetadata) []Issue {
	issues := []Issue{}
	if id := strings.TrimSpace(meta.Page.ID); id != "" && id != opts.ExistingPageID {
		if opts.PageIDExists != nil && opts.PageIDExists(id) {
			issues = append(issues, Issue{
				Severity: "error",
				Code:     "duplicate_leafwiki_id",
				Path:     routePath,
				PageID:   opts.ExistingPageID,
				Message:  "metadata page.id already belongs to another page",
			})
		}
	}
	for key := range meta.Extra {
		if markdown.IsReservedMetadataKey(key) {
			issues = append(issues, Issue{
				Severity: "error",
				Code:     "reserved_metadata",
				Path:     routePath,
				PageID:   opts.ExistingPageID,
				Message:  "metadata key uses reserved leafwiki_ prefix",
			})
		}
	}
	return issues
}

func validateMarkdownReferences(routePath string, body string, opts ContentValidationOptions) []Issue {
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
					Severity: "error",
					Code:     "missing_asset",
					Path:     routePath,
					PageID:   opts.ExistingPageID,
					Message:  "asset reference does not resolve: " + ref.Destination,
				})
			}
			continue
		}
		if opts.ResolveMarkdownLink != nil {
			_, kind, ok, code := opts.ResolveMarkdownLink(ref.Destination)
			if !ok {
				if code == "" {
					code = "broken_link"
				}
				issues = append(issues, Issue{
					Severity: "error",
					Code:     code,
					Path:     routePath,
					PageID:   opts.ExistingPageID,
					Message:  "wiki link does not resolve: " + ref.Destination,
				})
				continue
			}
			if kind == tree.NodeKindPage && isExtensionlessWikiDestination(ref.Destination) {
				issues = append(issues, Issue{
					Severity: "error",
					Code:     "non_canonical_link",
					Path:     routePath,
					PageID:   opts.ExistingPageID,
					Message:  "page link must use .md: " + ref.Destination,
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
		if opts.ResolveLinkTarget != nil {
			_, kind, ok := opts.ResolveLinkTarget(targetRoutePath)
			if !ok {
				issues = append(issues, Issue{
					Severity: "error",
					Code:     "broken_link",
					Path:     routePath,
					PageID:   opts.ExistingPageID,
					Message:  "wiki link does not resolve: " + resolved,
				})
				continue
			}
			if kind == tree.NodeKindPage && isExtensionlessWikiDestination(ref.Destination) {
				issues = append(issues, Issue{
					Severity: "error",
					Code:     "non_canonical_link",
					Path:     routePath,
					PageID:   opts.ExistingPageID,
					Message:  "page link must use .md: " + ref.Destination,
				})
			}
			continue
		}
		if _, ok := resolvePageID(targetRoutePath); !ok {
			issues = append(issues, Issue{
				Severity: "error",
				Code:     "broken_link",
				Path:     routePath,
				PageID:   opts.ExistingPageID,
				Message:  "wiki link does not resolve: " + resolved,
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

func resolveWorkspaceReferencePath(relPath string, routePath string, destination string) string {
	dest := cleanDestination(destination)
	if dest == "" {
		return ""
	}
	if isMarkdownFileDestination(dest) {
		if strings.HasPrefix(dest, "/") {
			return "/" + tree.MarkdownPathToRoutePath(strings.TrimPrefix(dest, "/"))
		}
		baseDir := path.Dir(filepath.ToSlash(relPath))
		if baseDir == "." {
			baseDir = ""
		}
		return "/" + tree.MarkdownPathToRoutePath(path.Join(baseDir, dest))
	}
	return resolveReferencePath(routePath, dest)
}

func isMarkdownFileDestination(destination string) bool {
	return strings.EqualFold(path.Ext(cleanDestination(destination)), ".md")
}

func workspaceValidationRouteConflictKey(routePath string, kind tree.NodeKind) string {
	return string(kind) + ":" + strings.ToLower(strings.Trim(routePath, "/"))
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

func resolveReferencePath(routePath string, destination string) string {
	dest := cleanDestination(destination)
	if dest == "" {
		return ""
	}
	basePath := "/" + strings.Trim(strings.TrimSpace(routePath), "/")
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
		case "warning":
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
