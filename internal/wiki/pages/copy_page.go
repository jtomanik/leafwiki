package pages

import (
	"context"
	"log/slog"
	"strings"

	"github.com/perber/wiki/internal/core/assets"
	sharederrors "github.com/perber/wiki/internal/core/shared/errors"
	"github.com/perber/wiki/internal/core/tree"
	"github.com/perber/wiki/internal/wiki/pagesave"
)

// CopyPageInput is the input for CopyPageUseCase.
type CopyPageInput struct {
	UserID         tree.UserID
	Source         string
	SourcePageID   tree.PageID
	TargetParentID *tree.PageID
	Title          string
	Slug           tree.Slug
}

// CopyPageOutput is the output of CopyPageUseCase.
type CopyPageOutput struct {
	Page *tree.Page
}

// CopyPageUseCase duplicates a page and its assets under a new slug/title.
type CopyPageUseCase struct {
	tree         *tree.TreeService
	slug         *tree.SlugService
	assets       *assets.AssetService
	orchestrator *pagesave.PageSaveOrchestrator
	log          *slog.Logger
}

// NewCopyPageUseCase constructs a CopyPageUseCase.
func NewCopyPageUseCase(
	t *tree.TreeService,
	s *tree.SlugService,
	o *pagesave.PageSaveOrchestrator,
	a *assets.AssetService,
	log *slog.Logger,
) *CopyPageUseCase {
	return &CopyPageUseCase{tree: t, slug: s, assets: a, orchestrator: o, log: log}
}

// Execute copies the source page to a new node with duplicated assets.
func (uc *CopyPageUseCase) Execute(_ context.Context, in CopyPageInput) (*CopyPageOutput, error) {
	ve := sharederrors.NewValidationErrors()
	if in.Title == "" {
		ve.AddWithCode("title", FieldCodePageTitleRequired, MessageIDPageTitleRequired)
	}
	if err := uc.slug.IsValidSlug(in.Slug.FilesystemPath()); err != nil {
		ve.AddWithCode("slug", FieldCodePageSlugInvalid, MessageIDPageSlugInvalid)
	}
	if ve.HasErrors() {
		return nil, ve
	}

	targetParentID, err := ValidateOptionalSemanticParentID(in.TargetParentID)
	if err != nil {
		return nil, err
	}
	in.TargetParentID = targetParentID

	page, err := uc.tree.GetPage(in.SourcePageID)
	if err != nil {
		return nil, err
	}

	kind := tree.NodeKindPage
	copyID, err := uc.tree.CreateNode(in.UserID, in.TargetParentID, in.Title, in.Slug, &kind)
	if err != nil {
		return nil, err
	}
	cleanup := func() { _ = uc.tree.DeleteNodeUncheckedVersion(in.UserID, *copyID, false) }

	copyPage, err := uc.tree.GetPage(*copyID)
	if err != nil {
		cleanup()
		return nil, err
	}

	if err := uc.assets.CopyAllAssets(page.PageNode, copyPage.PageNode); err != nil {
		cleanup()
		return nil, err
	}

	updatedContent := strings.ReplaceAll(page.Content, pageAssetURLPrefix(page.ID), pageAssetURLPrefix(copyPage.ID))
	if err := uc.tree.UpdateNodeUncheckedVersion(in.UserID, copyPage.ID, copyPage.Title, copyPage.Slug, &updatedContent, false); err != nil {
		cleanup()
		_ = uc.assets.DeleteAllAssetsForPage(copyPage.PageNode)
		return nil, err
	}

	// Re-fetch after content update so After reflects the final state.
	copyPage, err = uc.tree.GetPage(copyPage.ID)
	if err != nil {
		return nil, err
	}

	if err := uc.orchestrator.Run(pagesave.PageSaveEvent{
		Operation: pagesave.PageOperationCreate,
		UserID:    in.UserID,
		Source:    in.Source,
		After:     copyPage,
		Summary:   "page copied",
	}); err != nil {
		return nil, err
	}

	return &CopyPageOutput{Page: copyPage}, nil
}
