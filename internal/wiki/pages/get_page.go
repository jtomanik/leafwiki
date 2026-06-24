package pages

import (
	"context"

	"github.com/perber/wiki/internal/core/tree"
)

// GetPageInput is the input for GetPageUseCase.
type GetPageInput struct {
	ID tree.PageID
}

// GetPageOutput is the output of GetPageUseCase.
type GetPageOutput struct {
	Page *tree.Page
}

// GetPageUseCase retrieves a single page by ID.
type GetPageUseCase struct {
	tree *tree.TreeService
}

// NewGetPageUseCase constructs a GetPageUseCase.
func NewGetPageUseCase(t *tree.TreeService) *GetPageUseCase {
	return &GetPageUseCase{tree: t}
}

// Execute fetches the page by ID.
func (uc *GetPageUseCase) Execute(_ context.Context, in GetPageInput) (*GetPageOutput, error) {
	page, err := uc.tree.GetPage(in.ID)
	if err != nil {
		return nil, err
	}
	return &GetPageOutput{Page: page}, nil
}

// ─── FindByPath ─────────────────────────────────────────────────────────────

// FindByPathInput is the input for FindByPathUseCase.
type FindByPathInput struct {
	RoutePath tree.RoutePath
	Kind      tree.NodeKind
}

// FindByPathOutput is the output of FindByPathUseCase.
type FindByPathOutput struct {
	Page *tree.Page
}

// FindByPathUseCase looks up a page by its URL route path (e.g. "docs/api/intro").
type FindByPathUseCase struct {
	tree *tree.TreeService
}

// NewFindByPathUseCase constructs a FindByPathUseCase.
func NewFindByPathUseCase(t *tree.TreeService) *FindByPathUseCase {
	return &FindByPathUseCase{tree: t}
}

// Execute finds the page matching the given route path.
func (uc *FindByPathUseCase) Execute(_ context.Context, in FindByPathInput) (*FindByPathOutput, error) {
	routePath, err := ValidateRoutePathValue(in.RoutePath)
	if err != nil {
		return nil, err
	}
	var (
		page *tree.Page
	)
	if in.Kind == "" {
		page, err = uc.tree.FindPageByRoutePath(routePath)
	} else {
		page, err = uc.tree.FindPageByRoutePathAndKind(routePath, in.Kind)
	}
	if err != nil {
		return nil, err
	}
	return &FindByPathOutput{Page: page}, nil
}

// ─── LookupPagePath ─────────────────────────────────────────────────────────

// LookupPagePathInput is the input for LookupPagePathUseCase.
type LookupPagePathInput struct {
	Path tree.RoutePath
	Kind tree.NodeKind
}

// LookupPagePathOutput is the output of LookupPagePathUseCase.
type LookupPagePathOutput struct {
	Lookup *tree.PathLookup
}

// LookupPagePathUseCase resolves a path to its tree segments, including non-existing ones.
type LookupPagePathUseCase struct {
	tree *tree.TreeService
}

// NewLookupPagePathUseCase constructs a LookupPagePathUseCase.
func NewLookupPagePathUseCase(t *tree.TreeService) *LookupPagePathUseCase {
	return &LookupPagePathUseCase{tree: t}
}

// Execute looks up the path and returns segment metadata.
func (uc *LookupPagePathUseCase) Execute(_ context.Context, in LookupPagePathInput) (*LookupPagePathOutput, error) {
	routePath, err := ValidateRoutePathValue(in.Path)
	if err != nil {
		return nil, err
	}
	var (
		lookup *tree.PathLookup
	)
	if in.Kind == "" {
		lookup, err = uc.tree.LookupPagePath(routePath)
	} else {
		lookup, err = uc.tree.LookupPagePathForKind(routePath, in.Kind)
	}
	if err != nil {
		return nil, err
	}
	return &LookupPagePathOutput{Lookup: lookup}, nil
}

// ─── ResolvePermalink ───────────────────────────────────────────────────────

// ResolvePermalinkInput is the input for ResolvePermalinkUseCase.
type ResolvePermalinkInput struct {
	ID tree.PageID
}

// ResolvePermalinkOutput is the output of ResolvePermalinkUseCase.
type ResolvePermalinkOutput struct {
	Target *tree.PermalinkTarget
}

// ResolvePermalinkUseCase resolves a stable page ID to its current route path.
type ResolvePermalinkUseCase struct {
	tree *tree.TreeService
}

// NewResolvePermalinkUseCase constructs a ResolvePermalinkUseCase.
func NewResolvePermalinkUseCase(t *tree.TreeService) *ResolvePermalinkUseCase {
	return &ResolvePermalinkUseCase{tree: t}
}

// Execute resolves the permalink target for the given page ID.
func (uc *ResolvePermalinkUseCase) Execute(_ context.Context, in ResolvePermalinkInput) (*ResolvePermalinkOutput, error) {
	target, err := uc.tree.ResolvePermalinkTarget(in.ID)
	if err != nil {
		return nil, err
	}
	return &ResolvePermalinkOutput{Target: target}, nil
}

// ─── SortPages ──────────────────────────────────────────────────────────────

// SortPagesInput is the input for SortPagesUseCase.
type SortPagesInput struct {
	ParentID   tree.PageID
	OrderedIDs []tree.PageID
}

// SortPagesUseCase reorders the children of a parent node.
type SortPagesUseCase struct {
	tree *tree.TreeService
}

// NewSortPagesUseCase constructs a SortPagesUseCase.
func NewSortPagesUseCase(t *tree.TreeService) *SortPagesUseCase {
	return &SortPagesUseCase{tree: t}
}

// Execute applies the new child order to the parent node.
func (uc *SortPagesUseCase) Execute(_ context.Context, in SortPagesInput) error {
	return uc.tree.SortPages(in.ParentID, in.OrderedIDs)
}

// ─── SuggestSlug ────────────────────────────────────────────────────────────

// SuggestSlugInput is the input for SuggestSlugUseCase.
type SuggestSlugInput struct {
	ParentID  tree.PageID
	CurrentID tree.PageID
	Title     string
}

// SuggestSlugOutput is the output of SuggestSlugUseCase.
type SuggestSlugOutput struct {
	Slug tree.Slug
}

// SuggestSlugUseCase generates a unique slug suggestion for the given title in a parent.
type SuggestSlugUseCase struct {
	tree *tree.TreeService
	slug *tree.SlugService
}

// NewSuggestSlugUseCase constructs a SuggestSlugUseCase.
func NewSuggestSlugUseCase(t *tree.TreeService, s *tree.SlugService) *SuggestSlugUseCase {
	return &SuggestSlugUseCase{tree: t, slug: s}
}

// Execute generates and returns a unique slug suggestion.
func (uc *SuggestSlugUseCase) Execute(_ context.Context, in SuggestSlugInput) (*SuggestSlugOutput, error) {
	if in.ParentID == "" || in.ParentID == "root" {
		return &SuggestSlugOutput{Slug: tree.SlugFromString(uc.slug.GenerateUniqueChildSlug(uc.tree.GetTree(), in.CurrentID, in.Title))}, nil
	}
	parent, err := uc.tree.FindPageByID(in.ParentID)
	if err != nil {
		return nil, err
	}
	return &SuggestSlugOutput{Slug: tree.SlugFromString(uc.slug.GenerateUniqueChildSlug(parent, in.CurrentID, in.Title))}, nil
}
