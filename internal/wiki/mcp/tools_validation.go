package mcp

import (
	"context"
	"fmt"
	"strings"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/perber/wiki/internal/core/markdown"
	"github.com/perber/wiki/internal/core/markdownlinks"
	wikivalidation "github.com/perber/wiki/internal/core/markdownvalidation"
	sharederrors "github.com/perber/wiki/internal/core/shared/errors"
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
		routePath := tree.NewRoutePathUnchecked(strings.Trim(page.PageNode.CalculatePath(), "/"))
		return validationOutputFromResult(r.validateMarkdownContent(ctx, routePath, raw, page.PageNode.ID, page.PageNode.Kind)), nil
	})

	addTypedTool[validateContentInput, validationOutput](server, toolValidateContent, func(ctx context.Context, in validateContentInput) (validationOutput, error) {
		existingPageID := tree.NewPageIDUnchecked(strings.TrimSpace(in.ExistingPageID))
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
				if declaredID == "" || tree.NewPageIDUnchecked(declaredID) == pageID {
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
	assetExists := cachedValidationAssetExists(func(pageID tree.PageID) func(string) bool {
		return r.validationAssetExists(ctx, pageID)
	})
	return wikivalidation.ValidateWorkspaceMarkdownFiles(wikivalidation.WorkspaceMarkdownValidationOptions{
		RootDir:                r.workspaceRootDir,
		IncludeWarnings:        includeWarnings,
		MarkdownLinkRootPrefix: r.markdownLinkRootPrefix,
		PageIDExists:           r.validationPageIDExists,
		AssetExists:            assetExists,
	})
}

func cachedValidationAssetExists(factory func(pageID tree.PageID) func(destination string) bool) func(pageID tree.PageID, destination string) bool {
	cache := map[tree.PageID]func(string) bool{}
	return func(pageID tree.PageID, destination string) bool {
		if factory == nil {
			return false
		}
		if pageID == "" {
			return false
		}
		predicate, ok := cache[pageID]
		if !ok {
			predicate = factory(pageID)
			if predicate == nil {
				predicate = func(string) bool { return false }
			}
			cache[pageID] = predicate
		}
		return predicate(destination)
	}
}

func (r *Routes) validateLoadedTree(ctx context.Context) wikivalidation.Result {
	result := wikivalidation.Result{OK: true}
	if r.treeService == nil {
		return result
	}
	_ = r.treeService.WalkNodes(func(id tree.PageID) error {
		page, err := r.treeService.GetPage(id)
		if err != nil {
			return nil
		}
		routePath := tree.NewRoutePathUnchecked(strings.Trim(page.CalculatePath(), "/"))
		result = wikivalidation.Combine(result, r.validateMarkdownContent(ctx, routePath, page.RawContent, page.ID, page.Kind))
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
	seen := map[validationIssueDedupeKey]struct{}{}
	for _, issue := range issues {
		key := validationIssueDedupeKey{
			Severity:     issue.Severity,
			Code:         issue.Code,
			RoutePath:    issue.RoutePath,
			SourcePath:   issue.SourcePath,
			PageID:       issue.PageID,
			TargetPageID: issue.TargetPageID,
			Message:      issue.Message,
		}
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, issue)
	}
	return out
}

type validationIssueDedupeKey struct {
	Severity     wikivalidation.IssueSeverity
	Code         wikivalidation.IssueCode
	RoutePath    tree.RoutePath
	SourcePath   tree.MarkdownPath
	PageID       tree.PageID
	TargetPageID tree.PageID
	Message      string
}

func (r *Routes) resolveValidationPage(ctx context.Context, in validatePageInput) (*tree.Page, error) {
	pageID := strings.TrimSpace(in.PageID)
	routePath := normalizeToolRoutePath(in.Path)
	if pageID != "" && routePath != "" {
		return nil, sharederrors.NewLocalizedError(
			errCodeMCPPageTargetAmbiguous,
			"pageId and path cannot both be supplied",
			"pageId and path cannot both be supplied",
			nil,
		)
	}
	if pageID == "" && routePath == "" {
		return nil, sharederrors.NewLocalizedError(
			errCodeMCPPageTargetRequired,
			"pageId or path is required",
			"pageId or path is required",
			nil,
		)
	}
	if pageID != "" {
		return r.treeService.GetPage(tree.NewPageIDUnchecked(pageID))
	}
	out, err := r.findToolPageByInputPath(ctx, in.Path, in.Kind)
	if err != nil {
		return nil, err
	}
	return out.Page, nil
}

func (r *Routes) validateMarkdownContent(ctx context.Context, routePath tree.RoutePath, content string, existingPageID tree.PageID, sourceKind tree.NodeKind) wikivalidation.Result {
	normalizedRoutePath := routePath.Clean()
	return wikivalidation.ValidateMarkdownContentWithOptions(routePath, content, wikivalidation.ContentValidationOptions{
		ExistingPageID: existingPageID,
		ResolvePageID: func(candidateRoutePath tree.RoutePath) (tree.PageID, bool) {
			if candidateRoutePath.Clean() == normalizedRoutePath {
				if existingPageID != "" {
					return existingPageID, true
				}
				return r.resolveValidationPageIDForKind(candidateRoutePath, sourceKind)
			}
			return r.resolveValidationPageID(candidateRoutePath)
		},
		ResolveMarkdownLink: func(destination string) (tree.PageID, tree.NodeKind, bool, wikivalidation.IssueCode) {
			return r.resolveValidationMarkdownLink(routePath, sourceKind, destination)
		},
		PageIDExists:           r.validationPageIDExists,
		AssetExists:            r.validationAssetExists(ctx, existingPageID),
		MarkdownLinkRootPrefix: r.markdownLinkRootPrefix,
	})
}

func (r *Routes) resolveValidationMarkdownLink(sourceRoutePath tree.RoutePath, sourceKind tree.NodeKind, destination string) (tree.PageID, tree.NodeKind, bool, wikivalidation.IssueCode) {
	if r == nil || r.treeService == nil {
		return "", "", false, wikivalidation.IssueCodeBrokenLink
	}
	index := r.validationMarkdownLinkIndex()
	sourceFile := wikipages.MarkdownContentPathForRoute(sourceRoutePath, validationSourceKindOrDefault(sourceKind))
	resolved := index.Resolve(sourceFile, destination)
	switch resolved.Kind {
	case markdownlinks.TargetKindExternal, markdownlinks.TargetKindAsset:
		return "", "", true, ""
	case markdownlinks.TargetKindInvalid:
		return "", "", false, wikivalidation.IssueCodeInvalidLink
	case markdownlinks.TargetKindUnresolved:
		if resolved.Code == markdownlinks.IssueCodeWorkspaceEscape || resolved.Code == markdownlinks.IssueCodeInvalidPercentEncoding {
			return "", "", false, wikivalidation.IssueCodeInvalidLink
		}
		if resolved.Code == markdownlinks.IssueCodeAmbiguousLegacyLink {
			return "", "", false, wikivalidation.IssueCodeAmbiguousLegacyLink
		}
		return "", "", false, wikivalidation.IssueCodeBrokenLink
	case markdownlinks.TargetKindPage, markdownlinks.TargetKindSection:
	default:
		return "", "", false, wikivalidation.IssueCodeBrokenLink
	}
	targetRoutePath := resolved.RoutePath
	if targetRoutePath == "" && resolved.Kind == markdownlinks.TargetKindSection {
		return "", tree.NodeKindSection, true, ""
	}
	page, err := r.treeService.FindPageByRoutePathAndKind(targetRoutePath, markdownTargetNodeKind(resolved.Kind))
	if err != nil || page == nil || page.PageNode == nil {
		return "", "", false, wikivalidation.IssueCodeBrokenLink
	}
	return page.PageNode.ID, page.PageNode.Kind, true, ""
}

func markdownTargetNodeKind(kind markdownlinks.TargetKind) tree.NodeKind {
	if kind == markdownlinks.TargetKindSection {
		return tree.NodeKindSection
	}
	return tree.NodeKindPage
}

func (r *Routes) validationMarkdownLinkIndex() *markdownlinks.Index {
	if rootDir := strings.TrimSpace(r.workspaceRootDir); rootDir != "" {
		if index, err := markdownlinks.NewIndexFromRootWithOptions(rootDir, markdownlinks.Options{MarkdownLinkRootPrefix: r.markdownLinkRootPrefix}); err == nil {
			return index
		}
	}
	entries := []markdownlinks.Entry{{Kind: markdownlinks.EntryKindSection, ContentPath: "index.md"}}
	if r.treeService == nil {
		return markdownlinks.NewIndex(entries)
	}
	root := r.treeService.GetTree()
	var walk func(node *tree.PageNode)
	walk = func(node *tree.PageNode) {
		if node == nil {
			return
		}
		routePath := tree.NewRoutePathUnchecked(strings.Trim(node.CalculatePath(), "/"))
		switch node.Kind {
		case tree.NodeKindSection:
			entries = append(entries, markdownlinks.Entry{
				Kind:        markdownlinks.EntryKindSection,
				RoutePath:   routePath,
				ContentPath: wikipages.MarkdownContentPathForRoute(routePath, tree.NodeKindSection),
			})
		case tree.NodeKindPage:
			if routePath != "" {
				entries = append(entries, markdownlinks.Entry{
					Kind:      markdownlinks.EntryKindPage,
					RoutePath: routePath,
				})
			}
		}
		for _, child := range node.Children {
			walk(child)
		}
	}
	walk(root)
	return markdownlinks.NewIndexWithOptions(entries, markdownlinks.Options{MarkdownLinkRootPrefix: r.markdownLinkRootPrefix})
}

func validationSourceKindOrDefault(kind tree.NodeKind) tree.NodeKind {
	if kind == tree.NodeKindSection {
		return tree.NodeKindSection
	}
	return tree.NodeKindPage
}

func (r *Routes) validationSourceKind(existingPageID tree.PageID) tree.NodeKind {
	if r == nil || r.treeService == nil {
		return tree.NodeKindPage
	}
	if existingPageID == "" {
		return tree.NodeKindPage
	}
	page, err := r.treeService.FindPageByID(existingPageID)
	if err != nil || page == nil {
		return tree.NodeKindPage
	}
	return page.Kind
}

func (r *Routes) validationSourceKindForRoute(routePath tree.RoutePath) tree.NodeKind {
	if r == nil || r.treeService == nil {
		return tree.NodeKindPage
	}
	normalized := routePath.Clean()
	if normalized.IsRoot() {
		return tree.NodeKindPage
	}
	page, err := r.treeService.FindPageByRoutePath(normalized)
	if err != nil || page == nil {
		return tree.NodeKindPage
	}
	return page.Kind
}

func (r *Routes) normalizeValidationContentPathInput(rawPath string, rawKind string) (tree.RoutePath, tree.NodeKind, error) {
	routePath := normalizeToolRoutePath(rawPath)
	rawKind = strings.TrimSpace(rawKind)
	parseRoutePath := func(raw string) (tree.RoutePath, error) {
		trimmed := strings.Trim(strings.TrimSpace(raw), "/")
		if trimmed == "" {
			return "", nil
		}
		return tree.ParseRoutePath(trimmed)
	}
	if pageRoute, sectionRoute, ok := wikipages.ReadmeMarkdownPathFallbackRoutes(rawPath); ok {
		if rawKind == "" {
			semanticPageRoute, err := parseRoutePath(pageRoute)
			if err != nil {
				return "", "", err
			}
			semanticSectionRoute, err := parseRoutePath(sectionRoute)
			if err != nil {
				return "", "", err
			}
			if _, ok := r.resolveValidationPageIDForKind(semanticPageRoute, tree.NodeKindPage); ok {
				return semanticPageRoute, tree.NodeKindPage, nil
			}
			if r != nil && r.treeService != nil && wikipages.ReadmeFallbackSectionIsActive(r.treeService.RootDir(), sectionRoute) {
				return semanticSectionRoute, tree.NodeKindSection, nil
			}
			return semanticPageRoute, tree.NodeKindPage, nil
		}
		kind, err := wikipages.ValidatePageKindString(rawKind)
		if err != nil {
			return "", "", err
		}
		if kind == tree.NodeKindSection {
			if r != nil && r.treeService != nil && wikipages.ReadmeFallbackSectionIsActive(r.treeService.RootDir(), sectionRoute) {
				semanticSectionRoute, err := parseRoutePath(sectionRoute)
				if err != nil {
					return "", "", err
				}
				return semanticSectionRoute, tree.NodeKindSection, nil
			}
			return "", "", fmt.Errorf("kind does not match markdown path")
		}
		semanticPageRoute, err := parseRoutePath(pageRoute)
		if err != nil {
			return "", "", err
		}
		return semanticPageRoute, tree.NodeKindPage, nil
	}
	if rawKind == "" {
		if derivedKind := wikipages.MarkdownPathInputKind(routePath); derivedKind != "" {
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
	kind, err := wikipages.ValidatePageKindString(rawKind)
	if err != nil {
		return "", "", err
	}
	if derivedKind := wikipages.MarkdownPathInputKind(routePath); derivedKind != "" {
		if kind != derivedKind {
			return "", "", fmt.Errorf("kind does not match markdown path")
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
			Severity: issue.Severity,
			Code:     issue.Code,
			Path:     markdownValidationIssuePath(issue),
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

func markdownValidationIssuePath(issue wikivalidation.Issue) string {
	if issue.SourcePath != "" {
		return issue.SourcePath.FilesystemPath()
	}
	return issue.RoutePath.FilesystemPath()
}
