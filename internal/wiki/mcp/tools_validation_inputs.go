package mcp

import (
	"context"
	"strings"

	"github.com/perber/wiki/internal/core/markdown"
	wikivalidation "github.com/perber/wiki/internal/core/markdownvalidation"
	"github.com/perber/wiki/internal/core/tree"
	wikiassets "github.com/perber/wiki/internal/wiki/assets"
	wikipages "github.com/perber/wiki/internal/wiki/pages"
)

func (r *Routes) normalizeValidationContentPathToolInput(rawPath string, rawKind string) (tree.RoutePath, tree.NodeKind, error) {
	kind, err := validationContentInputKindFromString(rawKind)
	if err != nil {
		return "", "", err
	}
	return r.normalizeValidationContentPathInput(rawPath, kind)
}

func validationContentInputKindFromString(rawKind string) (tree.NodeKind, error) {
	trimmed := strings.TrimSpace(rawKind)
	if trimmed == "" {
		return "", nil
	}
	return wikipages.ValidatePageKindString(trimmed)
}

func (r *Routes) normalizeValidationContentPathInput(rawPath string, inputKind tree.NodeKind) (tree.RoutePath, tree.NodeKind, error) {
	routePath := normalizeToolRoutePath(rawPath)
	parseRoutePath := func(raw string) (tree.RoutePath, error) {
		trimmed := strings.Trim(strings.TrimSpace(raw), "/")
		if trimmed == "" {
			return "", nil
		}
		return tree.ParseRoutePath(trimmed)
	}
	if pageRoute, sectionRoute, ok := wikipages.ReadmeMarkdownPathFallbackRoutes(rawPath); ok {
		if inputKind == "" {
			semanticPageRoute, err := parseRoutePath(pageRoute)
			if err != nil {
				return "", "", err
			}
			semanticSectionRoute := tree.RoutePathFromString(sectionRoute).Clean()
			if _, ok := r.resolveValidationPageIDForKind(semanticPageRoute, tree.NodeKindPage); ok {
				return semanticPageRoute, tree.NodeKindPage, nil
			}
			if r != nil && r.treeService != nil && wikipages.ReadmeFallbackSectionIsActive(r.treeService.RootDir(), sectionRoute) {
				return semanticSectionRoute, tree.NodeKindSection, nil
			}
			return semanticPageRoute, tree.NodeKindPage, nil
		}
		kind := inputKind
		if kind == tree.NodeKindSection {
			if r != nil && r.treeService != nil && wikipages.ReadmeFallbackSectionIsActive(r.treeService.RootDir(), sectionRoute) {
				semanticSectionRoute := tree.RoutePathFromString(sectionRoute).Clean()
				return semanticSectionRoute, tree.NodeKindSection, nil
			}
			return "", "", errValidationContentKindMismatch
		}
		semanticPageRoute, err := parseRoutePath(pageRoute)
		if err != nil {
			return "", "", err
		}
		return semanticPageRoute, tree.NodeKindPage, nil
	}
	if inputKind == "" {
		if derivedKind := wikipages.MarkdownPathInputKind(tree.MarkdownPathFromString(routePath)); derivedKind != "" {
			semanticRoutePath, err := parseRoutePath(tree.MarkdownPathToRoutePath(routePath))
			if err != nil {
				return "", "", err
			}
			return semanticRoutePath, derivedKind, nil
		}
		semanticRoutePath, err := parseRoutePath(routePath)
		if err != nil {
			return "", "", err
		}
		return semanticRoutePath, "", nil
	}
	kind := inputKind
	if derivedKind := wikipages.MarkdownPathInputKind(tree.MarkdownPathFromString(routePath)); derivedKind != "" {
		if kind != derivedKind {
			return "", "", errValidationContentKindMismatch
		}
		semanticRoutePath, err := parseRoutePath(tree.MarkdownPathToRoutePath(routePath))
		if err != nil {
			return "", "", err
		}
		return semanticRoutePath, kind, nil
	}
	semanticRoutePath, err := parseRoutePath(routePath)
	if err != nil {
		return "", "", err
	}
	return semanticRoutePath, kind, nil
}

func validationContentLeafWikiID(content string) string {
	doc, _, err := markdown.ParsePageDocument(content)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(doc.Metadata.Page.ID)
}

func (r *Routes) validationSourceMarkdownFile(sourceRoutePath tree.RoutePath) tree.MarkdownPath {
	if r != nil && r.treeService != nil {
		routePath := sourceRoutePath.Clean()
		if page, err := r.treeService.FindPageByRoutePath(routePath); err == nil && page != nil && page.PageNode != nil {
			return wikipages.MarkdownContentPathForRoute(routePath, page.PageNode.Kind)
		}
	}
	return wikipages.MarkdownContentPathForRoute(sourceRoutePath, tree.NodeKindPage)
}

func (r *Routes) resolveValidationPageID(routePath tree.RoutePath) (tree.PageID, bool) {
	if r == nil || r.treeService == nil {
		return "", false
	}
	normalized := routePath.Clean()
	if normalized.IsRoot() {
		return "", false
	}
	page, err := r.treeService.FindPageByRoutePath(normalized)
	if err != nil || page == nil {
		return "", false
	}
	return page.ID, true
}

func (r *Routes) resolveValidationPageIDForKind(routePath tree.RoutePath, kind tree.NodeKind) (tree.PageID, bool) {
	if r == nil || r.treeService == nil {
		return "", false
	}
	normalized := routePath.Clean()
	if normalized.IsRoot() {
		return "", false
	}
	page, err := r.treeService.FindPageByRoutePathAndKind(normalized, validationSourceKindOrDefault(kind))
	if err != nil || page == nil {
		return "", false
	}
	return page.ID, true
}

func (r *Routes) validationPageIDExists(pageID tree.PageID) bool {
	if r == nil || r.treeService == nil {
		return false
	}
	_, err := r.treeService.GetPage(pageID)
	return err == nil
}

func (r *Routes) validationAssetExists(ctx context.Context, pageID tree.PageID) func(destination string) bool {
	if r == nil || r.getAssets == nil || pageID == "" {
		return func(string) bool { return false }
	}
	out, err := r.getAssets.Execute(ctx, wikiassets.ListAssetsInput{PageID: pageID})
	if err != nil || out == nil {
		return func(string) bool { return false }
	}
	return validationAssetPredicate(pageID, out.Files)
}

func validationAssetPredicate(pageID tree.PageID, files []string) func(destination string) bool {
	known := map[string]struct{}{}
	for _, file := range files {
		clean := strings.TrimSpace(file)
		if clean == "" {
			continue
		}
		known[clean] = struct{}{}
		known[strings.TrimPrefix(clean, "/")] = struct{}{}
	}
	return func(destination string) bool {
		clean := cleanValidationAssetDestination(destination)
		if _, ok := known[clean]; ok {
			return true
		}
		if _, ok := known[strings.TrimPrefix(clean, "/")]; ok {
			return true
		}
		if strings.Contains(clean, "/") {
			return false
		}
		pageIDPath := pageID.MetadataValue()
		if _, ok := known["/assets/"+pageIDPath+"/"+clean]; ok {
			return true
		}
		if _, ok := known["assets/"+pageIDPath+"/"+clean]; ok {
			return true
		}
		return false
	}
}

func cleanValidationAssetDestination(destination string) string {
	dest := strings.TrimSpace(destination)
	dest = strings.TrimPrefix(dest, "<")
	dest = strings.TrimSuffix(dest, ">")
	if idx := strings.Index(dest, "#"); idx != -1 {
		dest = dest[:idx]
	}
	if idx := strings.Index(dest, "?"); idx != -1 {
		dest = dest[:idx]
	}
	return strings.TrimSpace(dest)
}

func validationOutputFromResult(result wikivalidation.Result) validationOutput {
	issues := make([]validationIssueOutput, 0, len(result.Issues))
	for _, issue := range result.Issues {
		issues = append(issues, validationIssueOutput{
			Severity:  issue.Severity,
			Code:      issue.Code,
			MessageID: issue.Code.MessageID(),
			Path:      markdownValidationIssuePath(issue),
			PageID:    issue.PageID,
			Message:   issue.Message,
		})
	}
	return validationOutput{
		OK: result.OK,
		Summary: validationSummaryOutput{
			Errors:       result.Summary.Errors,
			WarningCount: result.Summary.Warnings,
		},
		Issues: issues,
	}
}

func markdownValidationIssuePath(issue wikivalidation.Issue) string {
	if issue.SourcePath != "" {
		return issue.SourcePath.FilesystemPath()
	}
	return issue.RoutePath.FilesystemPath()
}
