package pages

import (
	"context"
	"log/slog"

	"github.com/perber/wiki/internal/core/tree"
	"github.com/perber/wiki/internal/wiki/pagesave"
)

// MovePageInput is the input for MovePageUseCase.
type MovePageInput struct {
	UserID   tree.UserID
	Source   string
	ID       tree.PageID
	Version  tree.PageVersion
	ParentID tree.PageID
}

// MovePageUseCase moves a page to a new parent, updating links and recording revisions.
type MovePageUseCase struct {
	tree         movePageTree
	orchestrator *pagesave.PageSaveOrchestrator
	log          *slog.Logger
}

// NewMovePageUseCase constructs a MovePageUseCase.
func NewMovePageUseCase(
	t *tree.TreeService,
	o *pagesave.PageSaveOrchestrator,
	log *slog.Logger,
) *MovePageUseCase {
	return &MovePageUseCase{tree: t, orchestrator: o, log: log}
}

// Execute moves the page and fires post-save side effects for the whole subtree.
func (uc *MovePageUseCase) Execute(_ context.Context, in MovePageInput) error {
	if in.ID.String() == "root" || in.ID.String() == "" {
		return newPageRootOperationError("move")
	}

	parentID, err := ValidateSemanticMoveParentID(in.ParentID)
	if err != nil {
		return err
	}
	in.ParentID = parentID
	in.Version = sanitizeSemanticClientVersion(in.Version)

	beforePage, err := uc.tree.GetPage(in.ID)
	if err != nil {
		return err
	}
	subtreeIDs := collectSubtreeIDs(beforePage.PageNode)

	oldPath := beforePage.CalculateRoutePath()

	if err := uc.tree.MoveNode(in.UserID, in.ID, in.ParentID, in.Version); err != nil {
		return err
	}

	event := pagesave.PageSaveEvent{
		Operation: pagesave.PageOperationMove,
		UserID:    in.UserID,
		Source:    in.Source,
		OldPath:   oldPath,
	}

	pages, errs := uc.tree.GetPages(subtreeIDs)
	for i, p := range pages {
		if errs[i] != nil {
			uc.log.Warn("failed to get page after move", "pageID", subtreeIDs[i], "error", errs[i])
			continue
		}
		event.AffectedPages = append(event.AffectedPages, p)
	}

	if err := uc.orchestrator.Run(event); err != nil {
		return err
	}

	return nil
}
