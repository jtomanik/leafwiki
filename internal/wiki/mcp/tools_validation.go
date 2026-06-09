package mcp

import (
	"context"
	"fmt"
	"strings"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
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
		return validationOutputFromResult(r.validateMarkdownContent(ctx, strings.Trim(page.PageNode.CalculatePath(), "/"), raw, page.PageNode.ID)), nil
	})

	addTypedTool[validateContentInput, validationOutput](server, toolValidateContent, func(ctx context.Context, in validateContentInput) (validationOutput, error) {
		return validationOutputFromResult(r.validateMarkdownContent(ctx, normalizeToolRoutePath(in.Path), in.Content, strings.TrimSpace(in.ExistingPageID))), nil
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
		result = wikivalidation.Combine(result, r.validateMarkdownContent(ctx, strings.Trim(page.CalculatePath(), "/"), page.RawContent, page.ID))
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
	validPath, err := wikipages.ValidatePageRoutePath(routePath)
	if err != nil {
		return nil, err
	}
	out, err := r.findByPath.Execute(ctx, wikipages.FindByPathInput{RoutePath: validPath})
	if err != nil {
		return nil, err
	}
	return out.Page, nil
}

func (r *Routes) validateMarkdownContent(ctx context.Context, routePath string, content string, existingPageID string) wikivalidation.Result {
	return wikivalidation.ValidateMarkdownContentWithOptions(routePath, content, wikivalidation.ContentValidationOptions{
		ExistingPageID: existingPageID,
		ResolvePageID:  r.resolveValidationPageID,
		PageIDExists:   r.validationPageIDExists,
		AssetExists:    r.validationAssetExists(ctx, existingPageID),
	})
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
