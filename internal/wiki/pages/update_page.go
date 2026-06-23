package pages

import (
	"context"
	"log/slog"

	sharederrors "github.com/perber/wiki/internal/core/shared/errors"
	"github.com/perber/wiki/internal/core/tree"
	"github.com/perber/wiki/internal/wiki/pagesave"
)

// UpdatePageInput is the input for UpdatePageUseCase.
type UpdatePageInput struct {
	UserID     tree.UserID
	Source     string
	ID         tree.PageID
	Version    tree.PageVersion
	Title      string
	Slug       tree.Slug
	Content    *string
	Kind       *tree.NodeKind
	FromImport bool
}

// UpdatePageOutput is the output of UpdatePageUseCase.
type UpdatePageOutput struct {
	Page *tree.Page
}

// UpdatePageUseCase updates an existing page's content and/or structure.
type UpdatePageUseCase struct {
	tree         *tree.TreeService
	slug         *tree.SlugService
	orchestrator *pagesave.PageSaveOrchestrator
	log          *slog.Logger
}

// NewUpdatePageUseCase constructs an UpdatePageUseCase.
func NewUpdatePageUseCase(
	t *tree.TreeService,
	s *tree.SlugService,
	o *pagesave.PageSaveOrchestrator,
	log *slog.Logger,
) *UpdatePageUseCase {
	return &UpdatePageUseCase{tree: t, slug: s, orchestrator: o, log: log}
}

// Execute validates, updates the node, and fires post-save side effects.
func (uc *UpdatePageUseCase) Execute(_ context.Context, in UpdatePageInput) (*UpdatePageOutput, error) {
	ve := sharederrors.NewValidationErrors()
	if in.Title == "" {
		ve.AddWithCode("title", FieldCodePageTitleRequired, MessageIDPageTitleRequired, "Title must not be empty")
	}
	if err := in.Slug.Validate(); err != nil {
		ve.AddWithCode("slug", FieldCodePageSlugInvalid, MessageIDPageSlugInvalid, err.Error())
	}
	if ve.HasErrors() {
		return nil, ve
	}

	in.Version = sanitizeSemanticClientVersion(in.Version)

	before, err := uc.tree.GetPage(in.ID)
	if err != nil {
		return nil, err
	}

	slugChanged := in.Slug != before.Slug
	oldPath := before.CalculatePath()
	// Snapshot mutable fields before UpdateNode mutates the live tree node.
	oldTitle := before.Title
	oldContent := before.Content
	oldRawContent := before.RawContent

	var subtreeIDs []tree.PageID
	if slugChanged {
		subtreeIDs = collectSubtreeIDs(before.PageNode)
		if len(subtreeIDs) == 0 {
			subtreeIDs = []tree.PageID{in.ID}
		}
	}

	if err = uc.tree.UpdateNode(in.UserID, in.ID, in.Title, in.Slug, in.Content, in.Version, in.FromImport); err != nil {
		return nil, err
	}

	after, err := uc.tree.GetPage(in.ID)
	if err != nil {
		return nil, err
	}

	contentChanged := oldContent != after.Content
	titleChanged := oldTitle != after.Title
	metadataChanged := oldRawContent != after.RawContent && !contentChanged && !titleChanged && !slugChanged

	event := pagesave.PageSaveEvent{
		Operation:       pagesave.PageOperationUpdate,
		UserID:          in.UserID,
		Source:          in.Source,
		After:           after,
		OldPath:         oldPath,
		ContentChanged:  contentChanged,
		MetadataChanged: metadataChanged,
		SlugChanged:     slugChanged,
		TitleChanged:    titleChanged,
	}

	if slugChanged {
		pages, errs := uc.tree.GetPages(subtreeIDs)
		for i, p := range pages {
			if errs[i] != nil {
				uc.log.Warn("failed to get page for affected list", "pageID", subtreeIDs[i].String(), "error", errs[i])
				continue
			}
			event.AffectedPages = append(event.AffectedPages, p)
		}
	}

	if err := uc.orchestrator.Run(event); err != nil {
		return nil, err
	}

	return &UpdatePageOutput{Page: after}, nil
}
