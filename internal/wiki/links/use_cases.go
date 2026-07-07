package links

import (
	"context"
	"errors"

	sharederrors "github.com/perber/wiki/internal/core/shared/errors"
	"github.com/perber/wiki/internal/core/tree"
	corelinks "github.com/perber/wiki/internal/links"
)

const (
	linkServiceUnavailableMessage  = "Link service is unavailable"
	linkServiceUnavailableTemplate = "link service is unavailable"
)

var ErrLinkServiceUnavailable = sharederrors.NewLocalizedErrorFromCodeWithFallback(
	ErrCodeLinkUnavailable,
	linkServiceUnavailableMessage,
	linkServiceUnavailableTemplate,
	nil,
)

// ─── GetLinkStatusUseCase ────────────────────────────────────────────────────

type GetLinkStatusInput struct {
	PageID tree.PageID
}

type GetLinkStatusOutput struct {
	Status *corelinks.LinkStatusResult
}

type GetLinkStatusUseCase struct {
	links linkStatusService
	tree  linkPageGetter
}

type linkStatusService interface {
	GetLinkStatusForPage(pageID tree.PageID, pagePath tree.RoutePath) (*corelinks.LinkStatusResult, error)
}

type linkPageGetter interface {
	GetPage(id tree.PageID) (*tree.Page, error)
}

func NewGetLinkStatusUseCase(l *corelinks.LinkService, t *tree.TreeService) *GetLinkStatusUseCase {
	var links linkStatusService
	if l != nil {
		links = l
	}
	var treeService linkPageGetter
	if t != nil {
		treeService = t
	}
	return newGetLinkStatusUseCase(links, treeService)
}

func newGetLinkStatusUseCase(l linkStatusService, t linkPageGetter) *GetLinkStatusUseCase {
	return &GetLinkStatusUseCase{links: l, tree: t}
}

func (uc *GetLinkStatusUseCase) Execute(_ context.Context, in GetLinkStatusInput) (*GetLinkStatusOutput, error) {
	if uc.links == nil {
		return nil, ErrLinkServiceUnavailable
	}
	page, err := uc.tree.GetPage(in.PageID)
	if err != nil {
		if errors.Is(err, tree.ErrPageNotFound) {
			return nil, sharederrors.NewLocalizedErrorFromCode(ErrCodeLinkPageNotFound, err)
		}
		return nil, err
	}
	status, err := uc.links.GetLinkStatusForPage(in.PageID, page.CalculateRoutePath())
	if err != nil {
		return nil, err
	}
	return &GetLinkStatusOutput{Status: status}, nil
}

// ─── GetBacklinksUseCase ─────────────────────────────────────────────────────

type GetBacklinksInput struct {
	PageID tree.PageID
}

type GetBacklinksOutput struct {
	Result *corelinks.BacklinkResult
}

type GetBacklinksUseCase struct {
	links backlinksService
}

type backlinksService interface {
	GetBacklinksForPage(pageID tree.PageID) (*corelinks.BacklinkResult, error)
}

func NewGetBacklinksUseCase(l *corelinks.LinkService) *GetBacklinksUseCase {
	var links backlinksService
	if l != nil {
		links = l
	}
	return newGetBacklinksUseCase(links)
}

func newGetBacklinksUseCase(l backlinksService) *GetBacklinksUseCase {
	return &GetBacklinksUseCase{links: l}
}

func (uc *GetBacklinksUseCase) Execute(_ context.Context, in GetBacklinksInput) (*GetBacklinksOutput, error) {
	if uc.links == nil {
		return nil, ErrLinkServiceUnavailable
	}
	result, err := uc.links.GetBacklinksForPage(in.PageID)
	if err != nil {
		return nil, err
	}
	return &GetBacklinksOutput{Result: result}, nil
}

// ─── GetOutgoingLinksUseCase ─────────────────────────────────────────────────

type GetOutgoingLinksInput struct {
	PageID tree.PageID
}

type GetOutgoingLinksOutput struct {
	Result *corelinks.OutgoingResult
}

type GetOutgoingLinksUseCase struct {
	links outgoingLinksService
}

type outgoingLinksService interface {
	GetOutgoingLinksForPage(pageID tree.PageID) (*corelinks.OutgoingResult, error)
}

func NewGetOutgoingLinksUseCase(l *corelinks.LinkService) *GetOutgoingLinksUseCase {
	var links outgoingLinksService
	if l != nil {
		links = l
	}
	return newGetOutgoingLinksUseCase(links)
}

func newGetOutgoingLinksUseCase(l outgoingLinksService) *GetOutgoingLinksUseCase {
	return &GetOutgoingLinksUseCase{links: l}
}

func (uc *GetOutgoingLinksUseCase) Execute(_ context.Context, in GetOutgoingLinksInput) (*GetOutgoingLinksOutput, error) {
	if uc.links == nil {
		return nil, ErrLinkServiceUnavailable
	}
	result, err := uc.links.GetOutgoingLinksForPage(in.PageID)
	if err != nil {
		return nil, err
	}
	return &GetOutgoingLinksOutput{Result: result}, nil
}

// ─── ReindexLinksUseCase ─────────────────────────────────────────────────────

type ReindexLinksUseCase struct {
	links linkReindexService
}

type linkReindexService interface {
	IndexAllPages() error
}

func NewReindexLinksUseCase(l *corelinks.LinkService) *ReindexLinksUseCase {
	var links linkReindexService
	if l != nil {
		links = l
	}
	return newReindexLinksUseCase(links)
}

func newReindexLinksUseCase(l linkReindexService) *ReindexLinksUseCase {
	return &ReindexLinksUseCase{links: l}
}

func (uc *ReindexLinksUseCase) Execute(_ context.Context) error {
	if uc.links == nil {
		return nil
	}
	return uc.links.IndexAllPages()
}
