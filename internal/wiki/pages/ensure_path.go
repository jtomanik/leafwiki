package pages

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	sharederrors "github.com/perber/wiki/internal/core/shared/errors"
	"github.com/perber/wiki/internal/core/tree"
	"github.com/perber/wiki/internal/wiki/pagesave"
)

// EnsurePathInput is the input for EnsurePathUseCase.
type EnsurePathInput struct {
	UserID      tree.UserID
	Source      string
	TargetPath  tree.RoutePath
	TargetTitle string
	Kind        *tree.NodeKind
}

// EnsurePathOutput is the output of EnsurePathUseCase.
type EnsurePathOutput struct {
	Page *tree.Page
}

// EnsurePathUseCase ensures a full path exists, creating intermediate nodes as needed.
type EnsurePathUseCase struct {
	tree         *tree.TreeService
	slug         *tree.SlugService
	orchestrator *pagesave.PageSaveOrchestrator
	log          *slog.Logger
}

// NewEnsurePathUseCase constructs an EnsurePathUseCase.
func NewEnsurePathUseCase(
	t *tree.TreeService,
	s *tree.SlugService,
	o *pagesave.PageSaveOrchestrator,
	log *slog.Logger,
) *EnsurePathUseCase {
	return &EnsurePathUseCase{tree: t, slug: s, orchestrator: o, log: log}
}

// Execute ensures the path exists and returns the final node.
func (uc *EnsurePathUseCase) Execute(_ context.Context, in EnsurePathInput) (*EnsurePathOutput, error) {
	ve := sharederrors.NewValidationErrors()

	routePath, routePathErr := ValidateRoutePathValue(in.TargetPath)
	if routePathErr != nil {
		if routeValidation, ok := routePathErr.(*sharederrors.ValidationErrors); ok {
			for _, fieldErr := range routeValidation.Errors {
				ve.Errors = append(ve.Errors, fieldErr)
			}
		} else {
			ve.AddWithCode("path", FieldCodePagePathInvalid, MessageIDPagePathInvalid, routePathErr.Error())
		}
	}

	cleanTitle := strings.TrimSpace(in.TargetTitle)
	if cleanTitle == "" {
		ve.AddWithCode("title", FieldCodePageTitleRequired, MessageIDPageTitleRequired, "Title must not be empty")
	}

	if ve.HasErrors() {
		return nil, ve
	}

	lookup, err := uc.tree.LookupPagePath(routePath)
	if err != nil {
		return nil, err
	}

	requestedKind := tree.NodeKindPage
	if in.Kind != nil {
		requestedKind = *in.Kind
	}

	if lookup.Exists && lookupFinalKindMatches(lookup, requestedKind) {
		page, err := uc.tree.GetPage(*lookup.Segments[len(lookup.Segments)-1].ID)
		if err != nil {
			return nil, err
		}
		return &EnsurePathOutput{Page: page}, nil
	}

	for _, segment := range lookup.Segments {
		if !segment.Exists {
			if err := uc.slug.IsValidSlug(segment.Slug.FilesystemPath()); err != nil {
				ve.AddWithCode("path", FieldCodePagePathInvalid, MessageIDPagePathInvalid, fmt.Sprintf("Invalid slug '%s': %s", segment.Slug, err.Error()))
			}
		}
	}
	if ve.HasErrors() {
		return nil, ve
	}

	result, err := uc.tree.EnsurePagePath(in.UserID, routePath, cleanTitle, in.Kind)
	if err != nil {
		return nil, err
	}

	ids := make([]tree.PageID, 0, len(result.Created)+1)
	seen := make(map[tree.PageID]struct{}, len(result.Created)+1)
	appendUnique := func(id tree.PageID) {
		if _, ok := seen[id]; ok {
			return
		}
		seen[id] = struct{}{}
		ids = append(ids, id)
	}

	appendUnique(result.Page.ID)
	for _, n := range result.Created {
		appendUnique(n.ID)
	}

	pages, errs := uc.tree.GetPages(ids)
	pageByID := make(map[tree.PageID]*tree.Page, len(ids))
	for i, p := range pages {
		if errs[i] != nil {
			if ids[i] == result.Page.ID {
				return nil, errs[i]
			}
			uc.log.Warn("failed to get page for post-create processing", "pageID", ids[i].String(), "error", errs[i])
			continue
		}
		pageByID[ids[i]] = p
	}

	page := pageByID[result.Page.ID]
	if page == nil {
		return nil, tree.ErrPageNotFound
	}

	for _, n := range result.Created {
		p := pageByID[n.ID]
		if p == nil {
			uc.log.Warn("failed to get page for post-create processing", "pageID", n.ID, "error", tree.ErrPageNotFound)
			continue
		}
		if err := uc.orchestrator.Run(pagesave.PageSaveEvent{
			Operation: pagesave.PageOperationCreate,
			UserID:    in.UserID,
			Source:    in.Source,
			After:     p,
			Summary:   "page created via ensure path",
		}); err != nil {
			return nil, err
		}
	}

	return &EnsurePathOutput{Page: page}, nil
}

func lookupFinalKindMatches(lookup *tree.PathLookup, kind tree.NodeKind) bool {
	if lookup == nil || len(lookup.Segments) == 0 {
		return false
	}
	finalKind := lookup.Segments[len(lookup.Segments)-1].Kind
	return finalKind != nil && *finalKind == kind
}
