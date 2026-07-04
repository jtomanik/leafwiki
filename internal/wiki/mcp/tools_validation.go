package mcp

import (
	"context"
	"errors"
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

var errValidationContentKindMismatch = errors.New("kind does not match markdown path")

func (r *Routes) registerValidationTools(server *sdkmcp.Server) {
	addTypedTool[validatePageInput, validationOutput](server, toolValidatePage, func(ctx context.Context, in validatePageInput) (validationOutput, error) {
		return r.validatePageTool(ctx, in)
	})

	addTypedTool[validateContentInput, validationOutput](server, toolValidateContent, func(ctx context.Context, in validateContentInput) (validationOutput, error) {
		return r.validateContentTool(ctx, in)
	})

	addTypedTool[validateWikiInput, validationOutput](server, toolValidateWiki, func(ctx context.Context, in validateWikiInput) (validationOutput, error) {
		return r.validateWikiTool(ctx, in)
	})
}

func (r *Routes) validatePageTool(ctx context.Context, in validatePageInput) (validationOutput, error) {
	page, err := r.resolveValidationPage(ctx, in)
	if err != nil {
		return validationOutput{}, err
	}
	raw, err := r.treeService.ReadPageRaw(page.PageNode.ID)
	if err != nil {
		return validationOutput{}, err
	}
	routePath := tree.RoutePathFromString(strings.Trim(page.PageNode.CalculatePath(), "/"))
	return validationOutputFromResult(r.validateMarkdownContent(ctx, routePath, raw, page.PageNode.ID, page.PageNode.Kind)), nil
}

func (r *Routes) validateContentTool(ctx context.Context, in validateContentInput) (validationOutput, error) {
	existingPageID := tree.PageIDFromString(strings.TrimSpace(in.ExistingPageID))
	routePath, inputKind, err := r.normalizeValidationContentPathToolInput(in.Path, in.Kind)
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
			if declaredID == "" || tree.PageIDFromString(declaredID) == pageID {
				existingPageID = pageID
			}
		}
	}
	return validationOutputFromResult(r.validateMarkdownContent(ctx, routePath, in.Content, existingPageID, sourceKind)), nil
}

func (r *Routes) validateWikiTool(ctx context.Context, in validateWikiInput) (validationOutput, error) {
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
	walkErr := r.treeService.WalkNodes(func(id tree.PageID) error {
		page, err := r.treeService.GetPage(id)
		if err != nil {
			return err
		}
		routePath := tree.RoutePathFromString(strings.Trim(page.CalculatePath(), "/"))
		result = wikivalidation.Combine(result, r.validateMarkdownContent(ctx, routePath, page.RawContent, page.ID, page.Kind))
		return nil
	})
	if walkErr != nil {
		return validationResultFromIssues([]wikivalidation.Issue{{
			Severity:   wikivalidation.IssueSeverityError,
			Code:       wikivalidation.IssueCodeWorkspaceScanError,
			SourcePath: tree.MarkdownPathFromString("workspace"),
			MessageID:  wikivalidation.IssueCodeWorkspaceScanError.MessageID(),
			Message:    walkErr.Error(),
		}})
	}
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
			Code:      err.Code,
			Path:      err.Path,
			MessageID: err.MessageID,
			Message:   err.Message,
			Severity:  err.Severity,
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
			MessageID:    issue.MessageID,
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
	MessageID    sharederrors.MessageID
	Message      string
}

func (r *Routes) resolveValidationPage(ctx context.Context, in validatePageInput) (*tree.Page, error) {
	pageID := strings.TrimSpace(in.PageID)
	routePath := normalizeToolRoutePath(in.Path)
	if pageID != "" && routePath != "" {
		return nil, sharederrors.NewLocalizedErrorFromCode(errCodeMCPPageTargetAmbiguous, nil)
	}
	if pageID == "" && routePath == "" {
		return nil, sharederrors.NewLocalizedErrorFromCode(errCodeMCPPageTargetRequired, nil)
	}
	if pageID != "" {
		return r.treeService.GetPage(tree.PageIDFromString(pageID))
	}
	out, err := r.findToolPageByInputPath(ctx, in.Path, in.Kind)
	if err != nil {
		return nil, err
	}
	return out.Page, nil
}

func (r *Routes) validateMarkdownContent(ctx context.Context, routePath tree.RoutePath, content string, existingPageID tree.PageID, sourceKind tree.NodeKind) wikivalidation.Result {
	return wikivalidation.ValidateMarkdownContentWithOptions(routePath, content, wikivalidation.ContentValidationOptions{
		ExistingPageID: existingPageID,
		ResolvePageID: func(candidateRoutePath tree.RoutePath) (tree.PageID, bool) {
			if existingPageID != "" {
				return existingPageID, true
			}
			return r.resolveValidationPageIDForKind(candidateRoutePath, sourceKind)
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
		return "", "", false, wikivalidation.IssueCodeBrokenLink
	case markdownlinks.TargetKindPage, markdownlinks.TargetKindSection:
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
		routePath := tree.RoutePathFromString(strings.Trim(node.CalculatePath(), "/"))
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
