package pages

import (
	"context"
	"log/slog"

	sharederrors "github.com/perber/wiki/internal/core/shared/errors"
	"github.com/perber/wiki/internal/core/tree"
	"github.com/perber/wiki/internal/wiki/pagesave"
)

// CreatePageInput is the input for CreatePageUseCase.
type CreatePageInput struct {
	UserID   tree.UserID
	Source   string
	ParentID *tree.PageID
	Title    string
	Slug     tree.Slug
	Kind     *tree.NodeKind
}

// CreatePageOutput is the output of CreatePageUseCase.
type CreatePageOutput struct {
	Page *tree.Page
}

// CreatePageUseCase creates a new page in the tree and fires post-save side effects.
type CreatePageUseCase struct {
	tree         createPageTree
	slug         *tree.SlugService
	orchestrator *pagesave.PageSaveOrchestrator
	log          *slog.Logger
}

// NewCreatePageUseCase constructs a CreatePageUseCase.
func NewCreatePageUseCase(
	t *tree.TreeService,
	s *tree.SlugService,
	o *pagesave.PageSaveOrchestrator,
	log *slog.Logger,
) *CreatePageUseCase {
	return &CreatePageUseCase{tree: t, slug: s, orchestrator: o, log: log}
}

// Execute validates input, creates the page node, and fires post-save side effects.
func (uc *CreatePageUseCase) Execute(_ context.Context, in CreatePageInput) (*CreatePageOutput, error) {
	ve := sharederrors.NewValidationErrors()

	if in.Title == "" {
		ve.AddWithCode("title", FieldCodePageTitleRequired, MessageIDPageTitleRequired)
	}
	if in.Kind == nil {
		ve.AddWithCode("kind", FieldCodePageKindRequired, MessageIDPageKindRequired)
	}
	if in.Kind != nil && *in.Kind != tree.NodeKindPage && *in.Kind != tree.NodeKindSection {
		ve.AddWithCode("kind", FieldCodePageKindInvalid, MessageIDPageKindInvalid)
	}
	if err := in.Slug.Validate(); err != nil {
		ve.AddWithCode("slug", FieldCodePageSlugInvalid, MessageIDPageSlugInvalid)
	}
	if ve.HasErrors() {
		return nil, ve
	}

	parentID, err := ValidateOptionalSemanticParentID(in.ParentID)
	if err != nil {
		return nil, err
	}
	in.ParentID = parentID

	if in.ParentID != nil && in.ParentID.String() != "" {
		if _, err := uc.tree.FindPageByID(*in.ParentID); err != nil {
			return nil, err
		}
	}

	id, err := uc.tree.CreateNode(in.UserID, in.ParentID, in.Title, in.Slug, in.Kind)
	if err != nil {
		return nil, err
	}

	page, err := uc.tree.GetPage(*id)
	if err != nil {
		return nil, err
	}

	if err := uc.orchestrator.Run(pagesave.PageSaveEvent{
		Operation: pagesave.PageOperationCreate,
		UserID:    in.UserID,
		Source:    in.Source,
		After:     page,
		Summary:   "page created",
	}); err != nil {
		return nil, err
	}

	return &CreatePageOutput{Page: page}, nil
}
