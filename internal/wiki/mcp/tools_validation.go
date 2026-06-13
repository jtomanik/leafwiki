package mcp

import (
	"context"
	"fmt"
	"strings"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/perber/wiki/internal/core/markdown"
	"github.com/perber/wiki/internal/core/markdownlinks"
	wikivalidation "github.com/perber/wiki/internal/core/markdownvalidation"
	"github.com/perber/wiki/internal/core/tree"
	wikiassets "github.com/perber/wiki/internal/wiki/assets"
	wikipages "github.com/perber/wiki/internal/wiki/pages"
	"github.com/perber/wiki/internal/workspacesync"
)

func (r *Routes) registerValidationTools(server *sdkmcp.Server) {
	addTypedTool[validatePageInput, validationOutput](server, toolValidatePage, func(ctx context.Context, in validatePageInput) (validationOutput, error) {
		page, err := r.resolveValidationPage(ctx, in)
		if err != nil {
			return validationOutput{}, err
		}
		raw, err := r.treeService.ReadPageRaw(page.PageNode.ID)
		if err != nil {
			return validationOutput{}, err
		}
		return validationOutputFromResult(r.validateMarkdownContent(ctx, strings.Trim(page.PageNode.CalculatePath(), "/"), raw, page.PageNode.ID, page.PageNode.Kind)), nil
	})

	addTypedTool[validateContentInput, validationOutput](server, toolValidateContent, func(ctx context.Context, in validateContentInput) (validationOutput, error) {
		existingPageID := strings.TrimSpace(in.ExistingPageID)
		routePath, inputKind, err := r.normalizeValidationContentPathInput(in.Path, in.Kind)
		if err != nil {
			return validationOutput{}, err
		}
		sourceKind := inputKind
		if existingPageID != "" {
			sourceKind = r.validationSourceKind(existingPageID)
		} else if sourceKind == "" {
			sourceKind = r.validationSourceKindForRoute(routePath)
		}
		if existingPageID == "" {
			if pageID, ok := r.resolveValidationPageIDForKind(routePath, sourceKind); ok {
				declaredID := validationContentLeafWikiID(in.Content)
				if declaredID == "" || declaredID == pageID {
					existingPageID = pageID
				}
			}
		}
		return validationOutputFromResult(r.validateMarkdownContent(ctx, routePath, in.Content, existingPageID, sourceKind)), nil
	})

	addTypedTool[validateWikiInput, validationOutput](server, toolValidateWiki, func(ctx context.Context, in validateWikiInput) (validationOutput, error) {
		includeWarnings := true
		if in.IncludeWarnings != nil {
			includeWarnings = *in.IncludeWarnings
		}
		result := wikivalidation.Result{OK: true}
		if strings.TrimSpace(r.workspaceRootDir) != "" {
			result = r.validateWorkspaceMarkdownFiles(ctx, includeWarnings)
		} else {
			status := r.currentWorkspaceSyncStatus()
			result = wikivalidation.ValidateWorkspaceStatus(workspaceStatusIssues(status.ValidationErrors), includeWarnings)
			result = wikivalidation.Combine(result, r.validateLoadedTree(ctx))
		}
		result = validationResultFromIssues(result.Issues)
		return validationOutputFromResult(result), nil
	})
}

func (r *Routes) validateWorkspaceMarkdownFiles(ctx context.Context, includeWarnings bool) wikivalidation.Result {
	assetExists := cachedValidationAssetExists(func(pageID string) func(string) bool {
		return r.validationAssetExists(ctx, pageID)
	})
	return wikivalidation.ValidateWorkspaceMarkdownFiles(wikivalidation.WorkspaceMarkdownValidationOptions{
		RootDir:         r.workspaceRootDir,
		IncludeWarnings: includeWarnings,
		PageIDExists:    r.validationPageIDExists,
		AssetExists:     assetExists,
	})
}

func cachedValidationAssetExists(factory func(pageID string) func(destination string) bool) func(pageID string, destination string) bool {
	cache := map[string]func(string) bool{}
	return func(pageID string, destination string) bool {
		if factory == nil {
			return false
		}
		normalizedPageID := strings.TrimSpace(pageID)
		predicate, ok := cache[normalizedPageID]
		if !ok {
			predicate = factory(normalizedPageID)
			if predicate == nil {
				predicate = func(string) bool { return false }
			}
			cache[normalizedPageID] = predicate
		}
		return predicate(destination)
	}
}

func (r *Routes) validateLoadedTree(ctx context.Context) wikivalidation.Result {
	result := wikivalidation.Result{OK: true}
	if r.treeService == nil {
		return result
	}
	_ = r.treeService.WalkNodes(func(id string) error {
		page, err := r.treeService.GetPage(id)
		if err != nil {
			return nil
		}
		result = wikivalidation.Combine(result, r.validateMarkdownContent(ctx, strings.Trim(page.CalculatePath(), "/"), page.RawContent, page.ID, page.Kind))
		return nil
	})
	return result
}

func validationResultFromIssues(issues []wikivalidation.Issue) wikivalidation.Result {
	issues = dedupeValidationIssues(issues)
	summary := wikivalidation.Summary{}
	for _, issue := range issues {
		if issue.Severity == "warning" {
			summary.Warnings++
			continue
		}
		summary.Errors++
	}
	return wikivalidation.Result{
		OK:      summary.Errors == 0,
		Summary: summary,
		Issues:  issues,
	}
}

func workspaceStatusIssues(errors []workspacesync.ValidationError) []wikivalidation.WorkspaceStatusIssue {
	issues := make([]wikivalidation.WorkspaceStatusIssue, 0, len(errors))
	for _, err := range errors {
		issues = append(issues, wikivalidation.WorkspaceStatusIssue{
			Code:     err.Code,
			Path:     err.Path,
			Message:  err.Message,
			Severity: err.Severity,
		})
	}
	return issues
}

func dedupeValidationIssues(issues []wikivalidation.Issue) []wikivalidation.Issue {
	if len(issues) < 2 {
		return issues
	}
	out := make([]wikivalidation.Issue, 0, len(issues))
	seen := map[string]struct{}{}
	for _, issue := range issues {
		key := strings.Join([]string{issue.Severity, issue.Code, issue.Path, issue.PageID, issue.Message}, "\x00")
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, issue)
	}
	return out
}

func (r *Routes) resolveValidationPage(ctx context.Context, in validatePageInput) (*tree.Page, error) {
	pageID := strings.TrimSpace(in.PageID)
	routePath := normalizeToolRoutePath(in.Path)
	if pageID != "" && routePath != "" {
		return nil, fmt.Errorf("pageId and path cannot both be supplied")
	}
	if pageID == "" && routePath == "" {
		return nil, fmt.Errorf("pageId or path is required")
	}
	if pageID != "" {
		return r.treeService.GetPage(pageID)
	}
	out, err := r.findToolPageByInputPath(ctx, in.Path, in.Kind)
	if err != nil {
		return nil, err
	}
	return out.Page, nil
}

func (r *Routes) validateMarkdownContent(ctx context.Context, routePath string, content string, existingPageID string, sourceKind tree.NodeKind) wikivalidation.Result {
	normalizedRoutePath := strings.Trim(routePath, "/")
	return wikivalidation.ValidateMarkdownContentWithOptions(routePath, content, wikivalidation.ContentValidationOptions{
		ExistingPageID: existingPageID,
		ResolvePageID: func(candidateRoutePath string) (string, bool) {
			if strings.Trim(candidateRoutePath, "/") == normalizedRoutePath {
				if existingPageID != "" {
					return existingPageID, true
				}
				return r.resolveValidationPageIDForKind(candidateRoutePath, sourceKind)
			}
			return r.resolveValidationPageID(candidateRoutePath)
		},
		ResolveMarkdownLink: func(destination string) (string, tree.NodeKind, bool, string) {
			return r.resolveValidationMarkdownLink(routePath, sourceKind, destination)
		},
		PageIDExists: r.validationPageIDExists,
		AssetExists:  r.validationAssetExists(ctx, existingPageID),
	})
}

func (r *Routes) resolveValidationMarkdownLink(sourceRoutePath string, sourceKind tree.NodeKind, destination string) (string, tree.NodeKind, bool, string) {
	if r == nil || r.treeService == nil {
		return "", "", false, "broken_link"
	}
	index := r.validationMarkdownLinkIndex()
	sourceFile := wikipages.MarkdownContentPathForRoute(sourceRoutePath, validationSourceKindOrDefault(sourceKind))
	resolved := index.Resolve(sourceFile, destination)
	switch resolved.Kind {
	case markdownlinks.TargetKindExternal, markdownlinks.TargetKindAsset:
		return "", "", true, ""
	case markdownlinks.TargetKindInvalid:
		return resolved.RoutePath, "", false, "invalid_link"
	case markdownlinks.TargetKindUnresolved:
		if resolved.Code == "workspace_escape" || resolved.Code == "invalid_percent_encoding" {
			return resolved.RoutePath, "", false, "invalid_link"
		}
		if resolved.Code == "ambiguous_legacy_link" {
			return resolved.RoutePath, "", false, resolved.Code
		}
		return resolved.RoutePath, "", false, "broken_link"
	case markdownlinks.TargetKindPage, markdownlinks.TargetKindSection:
	default:
		return "", "", false, "broken_link"
	}
	targetRoutePath := strings.Trim(resolved.RoutePath, "/")
	if targetRoutePath == "" && resolved.Kind == markdownlinks.TargetKindSection {
		return "", tree.NodeKindSection, true, ""
	}
	page, err := r.treeService.FindPageByRoutePathAndKind(targetRoutePath, markdownTargetNodeKind(resolved.Kind))
	if err != nil || page == nil || page.PageNode == nil {
		return targetRoutePath, "", false, "broken_link"
	}
	return targetRoutePath, page.PageNode.Kind, true, ""
}

func markdownTargetNodeKind(kind markdownlinks.TargetKind) tree.NodeKind {
	if kind == markdownlinks.TargetKindSection {
		return tree.NodeKindSection
	}
	return tree.NodeKindPage
}

func (r *Routes) validationMarkdownLinkIndex() *markdownlinks.Index {
	if rootDir := strings.TrimSpace(r.workspaceRootDir); rootDir != "" {
		if index, err := markdownlinks.NewIndexFromRoot(rootDir); err == nil {
			return index
		}
	}
	entries := []markdownlinks.Entry{{Kind: markdownlinks.EntryKindSection, Path: "", ContentPath: "index.md"}}
	if r.treeService == nil {
		return markdownlinks.NewIndex(entries)
	}
	root := r.treeService.GetTree()
	var walk func(node *tree.PageNode)
	walk = func(node *tree.PageNode) {
		if node == nil {
			return
		}
		routePath := strings.Trim(node.CalculatePath(), "/")
		switch node.Kind {
		case tree.NodeKindSection:
			entries = append(entries, markdownlinks.Entry{
				Kind:        markdownlinks.EntryKindSection,
				Path:        routePath,
				ContentPath: wikipages.MarkdownContentPathForRoute(routePath, tree.NodeKindSection),
			})
		case tree.NodeKindPage:
			if routePath != "" {
				entries = append(entries, markdownlinks.Entry{
					Kind: markdownlinks.EntryKindPage,
					Path: routePath + ".md",
				})
			}
		}
		for _, child := range node.Children {
			walk(child)
		}
	}
	walk(root)
	return markdownlinks.NewIndex(entries)
}

func validationSourceKindOrDefault(kind tree.NodeKind) tree.NodeKind {
	if kind == tree.NodeKindSection {
		return tree.NodeKindSection
	}
	return tree.NodeKindPage
}

func (r *Routes) validationSourceKind(existingPageID string) tree.NodeKind {
	if r == nil || r.treeService == nil {
		return tree.NodeKindPage
	}
	pageID := strings.TrimSpace(existingPageID)
	if pageID == "" {
		return tree.NodeKindPage
	}
	page, err := r.treeService.FindPageByID(pageID)
	if err != nil || page == nil {
		return tree.NodeKindPage
	}
	return page.Kind
}

func (r *Routes) validationSourceKindForRoute(routePath string) tree.NodeKind {
	if r == nil || r.treeService == nil {
		return tree.NodeKindPage
	}
	normalized := strings.Trim(strings.TrimSpace(routePath), "/")
	if normalized == "" {
		return tree.NodeKindPage
	}
	page, err := r.treeService.FindPageByRoutePath(normalized)
	if err != nil || page == nil {
		return tree.NodeKindPage
	}
	return page.Kind
}

func (r *Routes) normalizeValidationContentPathInput(rawPath string, rawKind string) (string, tree.NodeKind, error) {
	routePath := normalizeToolRoutePath(rawPath)
	rawKind = strings.TrimSpace(rawKind)
	if pageRoute, sectionRoute, ok := wikipages.ReadmeMarkdownPathFallbackRoutes(rawPath); ok {
		if rawKind == "" {
			if _, ok := r.resolveValidationPageIDForKind(pageRoute, tree.NodeKindPage); ok {
				return pageRoute, tree.NodeKindPage, nil
			}
			if r != nil && r.treeService != nil && wikipages.ReadmeFallbackSectionIsActive(r.treeService.RootDir(), sectionRoute) {
				return sectionRoute, tree.NodeKindSection, nil
			}
			return pageRoute, tree.NodeKindPage, nil
		}
		kind, err := wikipages.ValidatePageKindString(rawKind)
		if err != nil {
			return "", "", err
		}
		if kind == tree.NodeKindSection {
			if r != nil && r.treeService != nil && wikipages.ReadmeFallbackSectionIsActive(r.treeService.RootDir(), sectionRoute) {
				return sectionRoute, tree.NodeKindSection, nil
			}
			return "", "", fmt.Errorf("kind does not match markdown path")
		}
		return pageRoute, tree.NodeKindPage, nil
	}
	if rawKind == "" {
		if derivedKind := wikipages.MarkdownPathInputKind(routePath); derivedKind != "" {
			return tree.MarkdownPathToRoutePath(routePath), derivedKind, nil
		}
		return routePath, "", nil
	}
	kind, err := wikipages.ValidatePageKindString(rawKind)
	if err != nil {
		return "", "", err
	}
	if derivedKind := wikipages.MarkdownPathInputKind(routePath); derivedKind != "" {
		if kind != derivedKind {
			return "", "", fmt.Errorf("kind does not match markdown path")
		}
		return tree.MarkdownPathToRoutePath(routePath), kind, nil
	}
	return routePath, kind, nil
}

func validationContentLeafWikiID(content string) string {
	fm, _, _, err := markdown.ParseFrontmatter(content)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(fm.LeafWikiID)
}

func (r *Routes) validationSourceMarkdownFile(sourceRoutePath string) string {
	routePath := strings.Trim(strings.TrimSpace(sourceRoutePath), "/")
	if r != nil && r.treeService != nil {
		if page, err := r.treeService.FindPageByRoutePath(routePath); err == nil && page != nil && page.PageNode != nil {
			return wikipages.MarkdownContentPathForRoute(routePath, page.PageNode.Kind)
		}
	}
	return wikipages.MarkdownContentPathForRoute(routePath, tree.NodeKindPage)
}

func (r *Routes) resolveValidationPageID(routePath string) (string, bool) {
	if r == nil || r.treeService == nil {
		return "", false
	}
	normalized := strings.Trim(strings.TrimSpace(routePath), "/")
	if normalized == "" {
		return "", false
	}
	page, err := r.treeService.FindPageByRoutePath(normalized)
	if err != nil || page == nil {
		return "", false
	}
	return page.ID, true
}

func (r *Routes) resolveValidationPageIDForKind(routePath string, kind tree.NodeKind) (string, bool) {
	if r == nil || r.treeService == nil {
		return "", false
	}
	normalized := strings.Trim(strings.TrimSpace(routePath), "/")
	if normalized == "" {
		return "", false
	}
	page, err := r.treeService.FindPageByRoutePathAndKind(normalized, validationSourceKindOrDefault(kind))
	if err != nil || page == nil {
		return "", false
	}
	return page.ID, true
}

func (r *Routes) validationPageIDExists(pageID string) bool {
	if r == nil || r.treeService == nil {
		return false
	}
	_, err := r.treeService.FindPageByID(strings.TrimSpace(pageID))
	return err == nil
}

func (r *Routes) validationAssetExists(ctx context.Context, pageID string) func(destination string) bool {
	pageID = strings.TrimSpace(pageID)
	if r == nil || r.getAssets == nil || pageID == "" {
		return func(string) bool { return false }
	}
	out, err := r.getAssets.Execute(ctx, wikiassets.ListAssetsInput{PageID: pageID})
	if err != nil || out == nil {
		return func(string) bool { return false }
	}
	known := map[string]struct{}{}
	for _, file := range out.Files {
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
		if _, ok := known["/assets/"+pageID+"/"+clean]; ok {
			return true
		}
		if _, ok := known["assets/"+pageID+"/"+clean]; ok {
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
			Severity: issue.Severity,
			Code:     issue.Code,
			Path:     issue.Path,
			PageID:   issue.PageID,
			Message:  issue.Message,
		})
	}
	return validationOutput{
		OK: result.OK,
		Summary: validationSummaryOutput{
			Errors:   result.Summary.Errors,
			Warnings: result.Summary.Warnings,
		},
		Issues: issues,
	}
}
