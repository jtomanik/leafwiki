package pages

import (
	"context"
	"log/slog"

	"github.com/perber/wiki/internal/core/assets"
	"github.com/perber/wiki/internal/core/tree"
	"github.com/perber/wiki/internal/wiki/pagesave"
)

// DeletePageInput is the input for DeletePageUseCase.
type DeletePageInput struct {
	UserID    tree.UserID
	Source    string
	ID        tree.PageID
	Version   tree.PageVersion
	Recursive bool
}

// DeletePageUseCase removes a page (and optionally its subtree) including assets and links.
type DeletePageUseCase struct {
	tree         deletePageTree
	assets       pageAssetDeleter
	orchestrator *pagesave.PageSaveOrchestrator
	log          *slog.Logger
}

// NewDeletePageUseCase constructs a DeletePageUseCase.
func NewDeletePageUseCase(
	t *tree.TreeService,
	a *assets.AssetService,
	o *pagesave.PageSaveOrchestrator,
	log *slog.Logger,
) *DeletePageUseCase {
	return &DeletePageUseCase{tree: t, assets: a, orchestrator: o, log: log}
}

// Execute deletes the page, cleaning up links (via orchestrator) and assets.
func (uc *DeletePageUseCase) Execute(_ context.Context, in DeletePageInput) error {
	if in.ID.String() == "root" || in.ID.String() == "" {
		return newPageRootOperationError("delete")
	}

	in.Version = sanitizeSemanticClientVersion(in.Version)

	page, err := uc.tree.GetPage(in.ID)
	if err != nil {
		return err
	}

	if in.Recursive {
		subtreeIDs := collectSubtreeIDs(page.PageNode)

		// Build affected pages list before deletion (paths are no longer reachable after).
		affectedPages := make([]*tree.Page, 0, len(subtreeIDs))
		pages, errs := uc.tree.GetPages(subtreeIDs)
		for i, p := range pages {
			if errs[i] != nil {
				uc.log.Warn("failed to get page before recursive delete", "pageID", subtreeIDs[i].String(), "error", errs[i])
				continue
			}
			affectedPages = append(affectedPages, p)
		}

		oldPath := page.CalculateRoutePath()

		if err := uc.tree.DeleteNode(in.UserID, in.ID, true, in.Version); err != nil {
			return err
		}

		if err := uc.orchestrator.Run(pagesave.PageSaveEvent{
			Operation:     pagesave.PageOperationDelete,
			UserID:        in.UserID,
			Source:        in.Source,
			Before:        page,
			OldPath:       oldPath,
			AffectedPages: affectedPages,
		}); err != nil {
			return err
		}

		for _, p := range affectedPages {
			if err := uc.assets.DeleteAllAssetsForPage(p.PageNode); err != nil {
				uc.log.Warn("failed to delete assets for page", "pageID", p.ID, "error", err)
			}
		}

		return nil
	}

	// Non-recursive delete.
	oldPath := page.CalculateRoutePath()

	if err := uc.tree.DeleteNode(in.UserID, in.ID, false, in.Version); err != nil {
		return err
	}

	if err := uc.orchestrator.Run(pagesave.PageSaveEvent{
		Operation:     pagesave.PageOperationDelete,
		UserID:        in.UserID,
		Source:        in.Source,
		Before:        page,
		OldPath:       oldPath,
		AffectedPages: []*tree.Page{page},
	}); err != nil {
		return err
	}

	if err := uc.assets.DeleteAllAssetsForPage(page.PageNode); err != nil {
		uc.log.Warn("failed to delete assets for page", "pageID", page.ID, "error", err)
	}

	return nil
}
