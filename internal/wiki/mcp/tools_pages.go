package mcp

import (
	"context"
	"strings"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/perber/wiki/internal/core/markdown"
	"github.com/perber/wiki/internal/core/tree"
	"github.com/perber/wiki/internal/http/dto"
	wikilinks "github.com/perber/wiki/internal/wiki/links"
	wikipages "github.com/perber/wiki/internal/wiki/pages"
	"github.com/perber/wiki/internal/wiki/pagesave"
)

var buildMarkdownWithPublicMetadataPatch = wikipages.BuildMarkdownWithPublicMetadataPatch

func (r *Routes) registerPageTools(server *sdkmcp.Server) {
	addTypedTool[getTreeInput, treeOutput](server, toolGetTree, func(_ context.Context, in getTreeInput) (treeOutput, error) {
		return r.getTreeTool(in), nil
	})

	addTypedTool[pageIDInput, pageOutput](server, toolGetPage, func(ctx context.Context, in pageIDInput) (pageOutput, error) {
		pageID, err := exactlyOneIDOrPageID(in.ID, in.PageID)
		if err != nil {
			return pageOutput{}, err
		}
		out, err := r.getPage.Execute(ctx, wikipages.GetPageInput{ID: pageID})
		if err != nil {
			return pageOutput{}, err
		}
		return r.pageOutputWithLinkStatus(ctx, out.Page, 0)
	})

	addTypedTool[pagePathInput, pageOutput](server, toolGetPageByPath, func(ctx context.Context, in pagePathInput) (pageOutput, error) {
		out, err := r.findToolPageByInputPath(ctx, in.Path, in.Kind)
		if err != nil {
			return pageOutput{}, err
		}
		depth := 0
		if out.Page.Kind == tree.NodeKindSection {
			depth = 1
		}
		return r.pageOutputWithLinkStatus(ctx, out.Page, depth)
	})

	addTypedTool[pathInput, lookupPathOutput](server, toolLookupPath, func(ctx context.Context, in pathInput) (lookupPathOutput, error) {
		return r.lookupPathTool(ctx, in)
	})

	addTypedTool[pageIDInput, resolvePermalinkOutput](server, toolResolvePermalink, func(ctx context.Context, in pageIDInput) (resolvePermalinkOutput, error) {
		pageID, err := exactlyOneIDOrPageID(in.ID, in.PageID)
		if err != nil {
			return resolvePermalinkOutput{}, err
		}
		out, err := r.resolveLink.Execute(ctx, wikipages.ResolvePermalinkInput{ID: pageID})
		if err != nil {
			return resolvePermalinkOutput{}, err
		}
		return resolvePermalinkOutput{Target: out.Target}, nil
	})

	addEditorTool[suggestSlugInput, suggestSlugOutput](r, server, toolSuggestSlug, func(ctx context.Context, _ toolActor, in suggestSlugInput) (suggestSlugOutput, error) {
		title, err := wikipages.ValidateSuggestSlugTitle(in.Title)
		if err != nil {
			return suggestSlugOutput{}, err
		}
		out, err := r.suggestSlug.Execute(ctx, wikipages.SuggestSlugInput{
			ParentID:  tree.PageIDFromString(strings.TrimSpace(in.ParentID)),
			CurrentID: tree.PageIDFromString(strings.TrimSpace(in.CurrentID)),
			Title:     title,
		})
		if err != nil {
			return suggestSlugOutput{}, err
		}
		return suggestSlugOutput{Slug: out.Slug.FilesystemPath()}, nil
	})

	addEditorTool[createPageInput, pageOutput](r, server, toolCreatePage, func(ctx context.Context, actor toolActor, in createPageInput) (pageOutput, error) {
		kind, err := wikipages.ValidatePageKind(in.Kind)
		if err != nil {
			return pageOutput{}, err
		}
		out, err := r.createPage.Execute(ctx, wikipages.CreatePageInput{
			UserID:   actor.ID,
			Source:   pagesave.PageMutationSourceMCP,
			ParentID: mcpPageIDPtr(in.ParentID),
			Title:    in.Title,
			Slug:     tree.SlugFromString(in.Slug),
			Kind:     &kind,
		})
		if err != nil {
			return pageOutput{}, err
		}
		return pageOutput{Page: r.apiPage(out.Page, 0)}, nil
	})

	addEditorTool[updatePageInput, pageOutput](r, server, toolUpdatePage, func(ctx context.Context, actor toolActor, in updatePageInput) (pageOutput, error) {
		return r.updatePageTool(ctx, actor, in)
	})

	addEditorTool[deletePageInput, messageOutput](r, server, toolDeletePage, func(ctx context.Context, actor toolActor, in deletePageInput) (messageOutput, error) {
		if err := r.deletePage.Execute(ctx, wikipages.DeletePageInput{
			UserID:    actor.ID,
			Source:    pagesave.PageMutationSourceMCP,
			ID:        tree.PageIDFromString(strings.TrimSpace(in.ID)),
			Version:   tree.PageVersionFromString(strings.TrimSpace(in.Version)),
			Recursive: in.Recursive,
		}); err != nil {
			return messageOutput{}, err
		}
		return newMessageOutput(ToolMessageDeletePageSuccess), nil
	})

	addEditorTool[movePageInput, messageOutput](r, server, toolMovePage, func(ctx context.Context, actor toolActor, in movePageInput) (messageOutput, error) {
		parentID := ""
		if in.ParentID != nil {
			parentID = *in.ParentID
		}
		if err := r.movePage.Execute(ctx, wikipages.MovePageInput{
			UserID:   actor.ID,
			Source:   pagesave.PageMutationSourceMCP,
			ID:       tree.PageIDFromString(strings.TrimSpace(in.ID)),
			Version:  tree.PageVersionFromString(strings.TrimSpace(in.Version)),
			ParentID: tree.PageIDFromString(parentID),
		}); err != nil {
			return messageOutput{}, err
		}
		return newMessageOutput(ToolMessageMovePageSuccess), nil
	})

	addEditorTool[sortPagesInput, messageOutput](r, server, toolSortPages, func(ctx context.Context, _ toolActor, in sortPagesInput) (messageOutput, error) {
		if err := r.sortPages.Execute(ctx, wikipages.SortPagesInput{
			ParentID:   tree.PageIDFromString(strings.TrimSpace(in.ParentID)),
			OrderedIDs: mcpPageIDs(in.OrderedIDs),
		}); err != nil {
			return messageOutput{}, err
		}
		return newMessageOutput(ToolMessageSortPagesSuccess), nil
	})

	addEditorTool[ensurePageInput, pageOutput](r, server, toolEnsurePage, func(ctx context.Context, actor toolActor, in ensurePageInput) (pageOutput, error) {
		return r.ensurePageTool(ctx, actor, in)
	})

	addEditorTool[convertPageInput, messageOutput](r, server, toolConvertPage, func(ctx context.Context, actor toolActor, in convertPageInput) (messageOutput, error) {
		targetKind, err := wikipages.ValidateConvertTargetKind(in.TargetKind)
		if err != nil {
			return messageOutput{}, err
		}
		if err := r.convertPage.Execute(ctx, wikipages.ConvertPageInput{
			UserID:     actor.ID,
			Source:     pagesave.PageMutationSourceMCP,
			ID:         tree.PageIDFromString(strings.TrimSpace(in.ID)),
			Version:    tree.PageVersionFromString(strings.TrimSpace(in.Version)),
			TargetKind: targetKind,
		}); err != nil {
			return messageOutput{}, err
		}
		return newMessageOutput(ToolMessageConvertPageSuccess), nil
	})

	addEditorTool[copyPageInput, pageOutput](r, server, toolCopyPage, func(ctx context.Context, actor toolActor, in copyPageInput) (pageOutput, error) {
		out, err := r.copyPage.Execute(ctx, wikipages.CopyPageInput{
			UserID:         actor.ID,
			Source:         pagesave.PageMutationSourceMCP,
			SourcePageID:   tree.PageIDFromString(strings.TrimSpace(in.ID)),
			TargetParentID: mcpPageIDPtr(in.TargetParentID),
			Title:          in.Title,
			Slug:           tree.SlugFromString(in.Slug),
		})
		if err != nil {
			return pageOutput{}, err
		}
		return pageOutput{Page: r.apiPage(out.Page, 0)}, nil
	})
}

func (r *Routes) getTreeTool(in getTreeInput) treeOutput {
	root := r.treeService.GetTree()
	if root == nil {
		return treeOutput{}
	}
	if in.Depth != nil {
		return treeOutput{Tree: dto.ToAPINodeWithContentPathsAndDepth(root, "", r.userResolver, r.treeService.ContentPathForNode, *in.Depth)}
	}
	return treeOutput{Tree: dto.ToAPINodeWithContentPaths(root, "", r.userResolver, r.treeService.ContentPathForNode)}
}

func (r *Routes) lookupPathTool(ctx context.Context, in pathInput) (lookupPathOutput, error) {
	var kind tree.NodeKind
	if strings.TrimSpace(in.Kind) != "" {
		validKind, err := wikipages.ValidatePageKindString(strings.TrimSpace(in.Kind))
		if err != nil {
			return lookupPathOutput{}, err
		}
		kind = validKind
	}
	routePath, err := wikipages.ValidateSemanticRoutePath(normalizeToolRoutePath(in.Path))
	if err != nil {
		return lookupPathOutput{}, err
	}
	out, err := r.lookupPath.Execute(ctx, wikipages.LookupPagePathInput{Path: routePath, Kind: kind})
	if err != nil {
		return lookupPathOutput{}, err
	}
	return lookupPathOutput{Lookup: out.Lookup}, nil
}

func (r *Routes) updatePageTool(ctx context.Context, actor toolActor, in updatePageInput) (pageOutput, error) {
	var tagsForValidation []string
	if in.TagsPresent {
		tagsForValidation = in.Tags
	}
	var propertiesForValidation map[string]string
	if in.PropertiesPresent {
		propertiesForValidation = in.Properties
	}
	if err := wikipages.ValidatePageMetadataInput(tagsForValidation, propertiesForValidation); err != nil {
		return pageOutput{}, err
	}
	contentToSave := in.Content
	fromImport := false
	if in.Content != nil || in.TagsPresent || in.PropertiesPresent {
		pageID := tree.PageIDFromString(strings.TrimSpace(in.ID))
		currentRaw, err := r.treeService.ReadPageRaw(pageID)
		if err != nil {
			return pageOutput{}, err
		}
		body := ""
		if in.Content != nil {
			body = *in.Content
		} else {
			doc, _, err := markdown.ParsePageDocument(currentRaw)
			if err != nil {
				return pageOutput{}, err
			}
			body = doc.Body
		}
		combined, err := buildMarkdownWithPublicMetadataPatch(currentRaw, pageID, in.Title, wikipages.PublicMetadataPatch{
			Tags:              tagsForValidation,
			TagsPresent:       in.TagsPresent,
			Properties:        propertiesForValidation,
			PropertiesPresent: in.PropertiesPresent,
		}, body)
		if err != nil {
			return pageOutput{}, err
		}
		contentToSave = &combined
		fromImport = true
	}
	kind := tree.NodeKindPage
	out, err := r.updatePage.Execute(ctx, wikipages.UpdatePageInput{
		UserID:     actor.ID,
		Source:     pagesave.PageMutationSourceMCP,
		ID:         tree.PageIDFromString(strings.TrimSpace(in.ID)),
		Version:    tree.PageVersionFromString(strings.TrimSpace(in.Version)),
		Title:      in.Title,
		Slug:       tree.SlugFromString(in.Slug),
		Content:    contentToSave,
		Kind:       &kind,
		FromImport: fromImport,
	})
	if err != nil {
		return pageOutput{}, err
	}
	return pageOutput{Page: r.apiPage(out.Page, 0)}, nil
}

func (r *Routes) ensurePageTool(ctx context.Context, actor toolActor, in ensurePageInput) (pageOutput, error) {
	kind, err := wikipages.ValidatePageKind(in.Kind)
	if err != nil {
		return pageOutput{}, err
	}
	targetPath, err := wikipages.ValidateSemanticRoutePath(in.Path)
	if err != nil {
		return pageOutput{}, err
	}
	out, err := r.ensurePath.Execute(ctx, wikipages.EnsurePathInput{
		UserID:      actor.ID,
		Source:      pagesave.PageMutationSourceMCP,
		TargetPath:  targetPath,
		TargetTitle: in.Title,
		Kind:        &kind,
	})
	if err != nil {
		return pageOutput{}, err
	}
	return pageOutput{Page: r.apiPage(out.Page, 0)}, nil
}

func (r *Routes) findToolPageByInputPath(ctx context.Context, rawPath string, rawKind string) (*wikipages.FindByPathOutput, error) {
	if out, handled, err := wikipages.FindReadmeMarkdownPathFallbackRawInput(rawPath, rawKind, wikipages.ReadmeMarkdownPathFallbackLookup{
		RootDir: r.treeService.RootDir(),
		FindByPath: func(input wikipages.FindByPathInput) (*wikipages.FindByPathOutput, error) {
			return r.findByPath.Execute(ctx, input)
		},
		RootPage: func() (*tree.Page, error) {
			return r.treeService.GetPage(tree.RootPageID)
		},
	}); err != nil || handled {
		return out, err
	}
	routePath, kind, err := normalizeToolPagePathInput(rawPath, rawKind)
	if err != nil {
		return nil, err
	}
	return r.findToolPageByPath(ctx, routePath, kind)
}

func (r *Routes) findToolPageByPath(ctx context.Context, routePath tree.RoutePath, kind tree.NodeKind) (*wikipages.FindByPathOutput, error) {
	if kind != "" {
		return r.findByPath.Execute(ctx, wikipages.FindByPathInput{RoutePath: routePath, Kind: kind})
	}
	if out, err := r.findByPath.Execute(ctx, wikipages.FindByPathInput{RoutePath: routePath, Kind: tree.NodeKindSection}); err == nil {
		return out, nil
	}
	return r.findByPath.Execute(ctx, wikipages.FindByPathInput{RoutePath: routePath})
}

func mcpPageIDPtr(id *string) *tree.PageID {
	if id == nil {
		return nil
	}
	typed := tree.PageIDFromString(*id)
	return &typed
}

func mcpPageIDs(ids []string) []tree.PageID {
	out := make([]tree.PageID, len(ids))
	for i, id := range ids {
		out[i] = tree.PageIDFromString(id)
	}
	return out
}

func (r *Routes) pageOutputWithLinkStatus(ctx context.Context, page *tree.Page, depth int) (pageOutput, error) {
	out, err := r.linkStatus.Execute(ctx, wikilinks.GetLinkStatusInput{PageID: page.ID})
	if err != nil {
		return pageOutput{}, err
	}
	return pageOutput{Page: r.apiPage(page, depth), LinkStatus: out.Status}, nil
}
