package mcp

import (
	"context"
	"strings"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/perber/wiki/internal/core/tree"
	wikipages "github.com/perber/wiki/internal/wiki/pages"
	"github.com/perber/wiki/internal/wiki/pagesave"
)

func (r *Routes) registerRefactorTools(server *sdkmcp.Server) {
	addEditorTool[previewRefactorInput, *wikipages.RefactorPreview](r, server, toolPreviewRefactor, func(ctx context.Context, _ toolActor, in previewRefactorInput) (*wikipages.RefactorPreview, error) {
		return r.previewRefactorTool(ctx, in)
	})

	addEditorTool[applyRefactorInput, pageOutput](r, server, toolApplyRefactor, func(ctx context.Context, actor toolActor, in applyRefactorInput) (pageOutput, error) {
		return r.applyRefactorTool(ctx, actor, in)
	})
}

func (r *Routes) previewRefactorTool(ctx context.Context, in previewRefactorInput) (*wikipages.RefactorPreview, error) {
	pageID, err := exactlyOneIDOrPageID(in.ID, in.PageID)
	if err != nil {
		return nil, err
	}
	out, err := r.previewRef.Execute(ctx, wikipages.RefactorPreviewInput{
		PageID:      pageID,
		Kind:        in.Kind,
		Title:       in.Title,
		Slug:        tree.SlugFromString(in.Slug),
		Content:     in.Content,
		NewParentID: mcpPageIDPtr(in.ParentID),
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (r *Routes) applyRefactorTool(ctx context.Context, actor toolActor, in applyRefactorInput) (pageOutput, error) {
	pageID, err := exactlyOneIDOrPageID(in.ID, in.PageID)
	if err != nil {
		return pageOutput{}, err
	}
	page, err := r.applyRef.Execute(ctx, wikipages.RefactorApplyInput{
		UserID:       tree.UserIDFromString(actor.ID),
		Source:       pagesave.PageMutationSourceMCP,
		Version:      tree.PageVersionFromString(strings.TrimSpace(in.Version)),
		RewriteLinks: in.RewriteLinks,
		RefactorPreviewInput: wikipages.RefactorPreviewInput{
			PageID:      pageID,
			Kind:        in.Kind,
			Title:       in.Title,
			Slug:        tree.SlugFromString(in.Slug),
			Content:     in.Content,
			NewParentID: mcpPageIDPtr(in.ParentID),
		},
	})
	if err != nil {
		return pageOutput{}, err
	}
	return pageOutput{Page: r.apiPage(page, 0)}, nil
}
