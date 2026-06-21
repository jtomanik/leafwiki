package pages

import (
	"context"
	"log/slog"

	"github.com/perber/wiki/internal/core/tree"
	"github.com/perber/wiki/internal/wiki/pagesave"
)

// ConvertPageInput is the input for ConvertPageUseCase.
type ConvertPageInput struct {
	UserID     string
	Source     string
	ID         string
	Version    string
	TargetKind tree.NodeKind
}

// ConvertPageUseCase converts a page to a different node kind (page ↔ section).
type ConvertPageUseCase struct {
	tree         *tree.TreeService
	orchestrator *pagesave.PageSaveOrchestrator
	log          *slog.Logger
}

// NewConvertPageUseCase constructs a ConvertPageUseCase.
func NewConvertPageUseCase(
	t *tree.TreeService,
	o *pagesave.PageSaveOrchestrator,
	log *slog.Logger,
) *ConvertPageUseCase {
	return &ConvertPageUseCase{tree: t, orchestrator: o, log: log}
}

// Execute converts the node kind and records a structure revision.
func (uc *ConvertPageUseCase) Execute(_ context.Context, in ConvertPageInput) error {
	if in.ID == "root" || in.ID == "" {
		return newPageRootOperationError("convert")
	}
	in.Version = sanitizeClientVersion(in.Version)
	before, err := uc.tree.GetPage(in.ID)
	if err != nil {
		return err
	}
	oldPath := before.CalculatePath()
	if err := uc.tree.ConvertNode(in.UserID, in.ID, in.TargetKind, in.Version); err != nil {
		return err
	}
	after, err := uc.tree.GetPage(in.ID)
	if err != nil {
		return err
	}
	if uc.orchestrator != nil {
		if err := uc.orchestrator.Run(pagesave.PageSaveEvent{
			Operation:     pagesave.PageOperationUpdate,
			UserID:        in.UserID,
			Source:        in.Source,
			Before:        snapshotPage(before),
			After:         after,
			OldPath:       oldPath,
			AffectedPages: []*tree.Page{after},
			Summary:       "page converted",
		}); err != nil {
			return err
		}
	}
	return nil
}

func snapshotPage(page *tree.Page) *tree.Page {
	if page == nil || page.PageNode == nil {
		return page
	}
	node := *page.PageNode
	return &tree.Page{
		PageNode:   &node,
		Content:    page.Content,
		RawContent: page.RawContent,
	}
}
