package markdownvalidation

import (
	"net/url"
	"path"
	"path/filepath"
	"strings"

	"github.com/perber/wiki/internal/core/tree"
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/text"
)

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
