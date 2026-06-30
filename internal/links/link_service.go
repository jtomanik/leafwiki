package links

import (
	"path"
	"strings"

	"github.com/perber/wiki/internal/core/markdownlinks"
	"github.com/perber/wiki/internal/core/tree"
)

type LinkService struct {
	storageDir             string
	treeService            *tree.TreeService
	store                  *LinksStore
	markdownLinkRootPrefix string
}

type LinkServiceOptions struct {
	MarkdownLinkRootPrefix string
}

func NewLinkService(storageDir string, treeService *tree.TreeService, store *LinksStore) *LinkService {
	return NewLinkServiceWithOptions(storageDir, treeService, store, LinkServiceOptions{})
}

func NewLinkServiceWithOptions(storageDir string, treeService *tree.TreeService, store *LinksStore, opts LinkServiceOptions) *LinkService {
	return &LinkService{
		storageDir:             storageDir,
		treeService:            treeService,
		store:                  store,
		markdownLinkRootPrefix: opts.MarkdownLinkRootPrefix,
	}
}

func (b *LinkService) markdownLinkOptions() markdownlinks.Options {
	return markdownlinks.Options{
		MarkdownLinkRootPrefix: b.markdownLinkRootPrefix,
	}
}

func (b *LinkService) markdownLinkIndexForTree() *markdownlinks.Index {
	return markdownLinkIndexForTreeWithOptions(b.treeService, b.markdownLinkOptions())
}

func (b *LinkService) IndexAllPages() error {
	if !b.treeService.IsLoaded() {
		return nil
	}

	if err := b.store.Clear(); err != nil {
		return err
	}

	var ids []tree.PageID
	_ = b.treeService.WalkNodes(func(id tree.PageID) error {
		ids = append(ids, id)
		return nil
	})

	pages, errs := b.treeService.GetPages(ids)
	markdownIndex := b.markdownLinkIndexForTree()
	for i, page := range pages {
		if errs[i] != nil {
			return errs[i]
		}
		links := extractLinksFromMarkdown(page.Content)
		targets := resolveTargetLinksWithIndex(b.treeService, markdownIndex, page.CalculateRoutePath(), page.Kind, links)
		if err := b.store.AddLinks(page.ID, page.Title, targets); err != nil {
			return err
		}
	}

	return nil
}

func (b *LinkService) ClearLinks() error {
	return b.store.Clear()
}

func (b *LinkService) GetBacklinksForPage(pageID tree.PageID) (*BacklinkResult, error) {
	backlinks, err := b.store.GetBacklinksForPage(pageID)
	return toBacklinkResult(b.treeService, backlinks), err
}

func (b *LinkService) GetOutgoingLinksForPage(pageID tree.PageID) (*OutgoingResult, error) {
	outgoingLinks, err := b.store.GetOutgoingLinksForPage(pageID)
	return toOutgoingLinkResult(b.treeService, outgoingLinks), err
}

func (b *LinkService) GetRefactorMatchesForPrefix(oldPrefix tree.RoutePath) ([]RefactorLinkMatch, error) {
	return b.store.GetRefactorMatchesForPrefix(oldPrefix)
}

func (b *LinkService) GetRefactorMatchesForPrefixAndKind(oldPrefix tree.RoutePath, rootKind tree.NodeKind) ([]RefactorLinkMatch, error) {
	return b.store.GetRefactorMatchesForPrefixAndKind(oldPrefix, rootKind)
}

func (b *LinkService) GetRefactorSourcePageIDsForPrefix(oldPrefix tree.RoutePath) ([]tree.PageID, error) {
	return b.store.GetRefactorSourcePageIDsForPrefix(oldPrefix)
}

func (b *LinkService) GetRefactorSourcePageIDsForPrefixAndKind(oldPrefix tree.RoutePath, rootKind tree.NodeKind) ([]tree.PageID, error) {
	return b.store.GetRefactorSourcePageIDsForPrefixAndKind(oldPrefix, rootKind)
}

func (b *LinkService) UpdateRewrittenLinksAndHealForPages(pages []*tree.Page, rules []RewriteRule) error {
	outgoingByPageID, err := b.store.GetOutgoingLinksForPages(pageIDsForPages(pages))
	if err != nil {
		return err
	}

	updates := make([]PageLinkUpdate, 0, len(pages))
	var markdownIndex *markdownlinks.Index
	for _, page := range pages {
		if page == nil {
			continue
		}
		if markdownIndex == nil {
			markdownIndex = b.markdownLinkIndexForTree()
		}
		pageRoutePath := page.CalculateRoutePath()
		targets := rewriteResolvedTargets(pageRoutePath, page.Kind, outgoingByPageID[page.ID], rules, b.treeService, markdownIndex)
		updates = append(updates, PageLinkUpdate{
			FromPageID: page.ID,
			FromTitle:  page.Title,
			ToPath:     pageRoutePath,
			ToKind:     page.Kind,
			Targets:    targets,
		})
	}

	if len(updates) == 0 {
		return nil
	}

	return b.store.ReplaceLinksAndHeal(updates)
}

func (b *LinkService) GetLinkStatusForPage(pageID tree.PageID, pagePath tree.RoutePath) (*LinkStatusResult, error) {
	pagePath = tree.RoutePathFromString(normalizeWikiPath(pagePath.WikiPath())).Clean()
	pageKind := tree.NodeKindPage
	if b.treeService != nil {
		if page, err := b.treeService.GetPage(pageID); err == nil && page != nil {
			pageKind = page.Kind
		}
	}

	// 1) Valid inbound backlinks
	validBacklinks, err := b.store.GetBacklinksForPage(pageID)
	if err != nil {
		return nil, err
	}
	validBacklinksResult := toBacklinkResult(b.treeService, validBacklinks)

	// 2) Broken inbound
	brokenIncoming, err := b.store.GetBrokenIncomingForPathAndKind(pagePath, pageKind)
	if err != nil {
		return nil, err
	}
	brokenIncomingResult := toBacklinkResult(b.treeService, brokenIncoming)

	// 3) Outgoings
	outgoings, err := b.store.GetOutgoingLinksForPage(pageID)
	if err != nil {
		return nil, err
	}
	outgoingResult := toOutgoingLinkResult(b.treeService, outgoings)

	// Split outgoing in broken/non-broken
	okOut := make([]OutgoingResultItem, 0, len(outgoingResult.Outgoings))
	brokenOut := make([]OutgoingResultItem, 0)
	for _, it := range outgoingResult.Outgoings {
		if it.Broken {
			brokenOut = append(brokenOut, it)
		} else {
			okOut = append(okOut, it)
		}
	}

	return &LinkStatusResult{
		Backlinks:       validBacklinksResult.Backlinks,
		BrokenIncoming:  brokenIncomingResult.Backlinks,
		Outgoings:       okOut,
		BrokenOutgoings: brokenOut,
		Counts: LinkStatusCounts{
			Backlinks:       len(validBacklinksResult.Backlinks),
			BrokenIncoming:  len(brokenIncomingResult.Backlinks),
			Outgoings:       len(okOut),
			BrokenOutgoings: len(brokenOut),
		},
	}, nil
}

func (b *LinkService) UpdateLinksForPage(page *tree.Page, content string) error {
	links := extractLinksFromMarkdown(content)

	targets := resolveTargetLinksWithIndex(b.treeService, b.markdownLinkIndexForTree(), page.CalculateRoutePath(), page.Kind, links)

	err := b.store.AddLinks(page.ID, page.Title, targets)
	if err != nil {
		return err
	}

	return nil
}

func (b *LinkService) UpdateLinksAndHealForPages(pages []*tree.Page) error {
	updates := make([]PageLinkUpdate, 0, len(pages))
	var markdownIndex *markdownlinks.Index
	for _, page := range pages {
		if page == nil {
			continue
		}
		if markdownIndex == nil {
			markdownIndex = b.markdownLinkIndexForTree()
		}
		pagePath := page.CalculateRoutePath()
		links := extractLinksFromMarkdown(page.Content)
		targets := resolveTargetLinksWithIndex(b.treeService, markdownIndex, pagePath, page.Kind, links)
		updates = append(updates, PageLinkUpdate{
			FromPageID: page.ID,
			FromTitle:  page.Title,
			ToPath:     pagePath,
			ToKind:     page.Kind,
			Targets:    targets,
		})
	}

	if len(updates) == 0 {
		return nil
	}

	return b.store.ReplaceLinksAndHeal(updates)
}

// DeleteOutgoingLinksForPage removes all outgoing link records for a page.
func (b *LinkService) DeleteOutgoingLinksForPage(pageID tree.PageID) error {
	return b.store.DeleteOutgoingLinks(pageID)
}

// MarkIncomingLinksBrokenForPage marks all incoming links pointing to pageID as broken.
func (b *LinkService) MarkIncomingLinksBrokenForPage(pageID tree.PageID) error {
	return b.store.MarkIncomingLinksBroken(pageID)
}

// MarkLinksBrokenForPath marks links pointing to an exact path as broken.
func (b *LinkService) MarkLinksBrokenForPath(toPath tree.RoutePath) error {
	toPath = tree.RoutePathFromString(normalizeWikiPath(toPath.WikiPath())).Clean()
	return b.store.MarkLinksBrokenForPath(toPath)
}

// MarkLinksBrokenForPathAndKind marks links pointing to an exact path and kind as broken.
func (b *LinkService) MarkLinksBrokenForPathAndKind(toPath tree.RoutePath, toKind tree.NodeKind) error {
	toPath = tree.RoutePathFromString(normalizeWikiPath(toPath.WikiPath())).Clean()
	return b.store.MarkLinksBrokenForPathAndKind(toPath, toKind)
}

// MarkLinksBrokenForPrefix marks all links under a prefix as broken (subtree move/delete).
func (b *LinkService) MarkLinksBrokenForPrefix(prefix string) error {
	prefix = normalizeWikiPath(prefix)
	return b.store.MarkLinksBrokenForPrefix(prefix)
}

// MarkLinksBrokenForPrefixAndKind marks a subtree root by kind while preserving same-path twins.
func (b *LinkService) MarkLinksBrokenForPrefixAndKind(prefix string, rootKind tree.NodeKind) error {
	prefix = normalizeWikiPath(prefix)
	return b.store.MarkLinksBrokenForPrefixAndKind(prefix, rootKind)
}

func (b *LinkService) HealLinksForExactPath(page *tree.Page) error {
	toPath := normalizeWikiPath(page.CalculatePath())
	return b.store.HealLinksForPathAndKind(toPath, page.Kind, page.ID)
}

func (b *LinkService) Close() error {
	if b.store == nil {
		return nil
	}
	return b.store.Close()
}

func pageIDsForPages(pages []*tree.Page) []tree.PageID {
	ids := make([]tree.PageID, 0, len(pages))
	for _, page := range pages {
		if page == nil {
			continue
		}
		ids = append(ids, page.ID)
	}
	return ids
}

func rewriteResolvedTargets(currentPath tree.RoutePath, sourceKind tree.NodeKind, outgoings []Outgoing, rules []RewriteRule, treeService *tree.TreeService, markdownIndex *markdownlinks.Index) []TargetLink {
	if len(outgoings) == 0 {
		return nil
	}

	paths := make([]string, 0, len(outgoings))
	for _, outgoing := range outgoings {
		targetPath := outgoing.ToPath.Clean()
		if rewritten, ok := applyRewriteRulesForKind(targetPath, outgoing.ToKind, rules); ok {
			targetPath = rewritten
		}
		paths = append(paths, storedTargetMarkdownHref(targetPath, outgoing.ToKind))
	}

	return resolveTargetLinksWithIndex(treeService, markdownIndex, currentPath, sourceKind, paths)
}

func storedTargetMarkdownHref(targetPath tree.RoutePath, targetKind TargetKind) string {
	wikiPath := targetPath.WikiPath()
	if storedTargetKind(targetKind) != TargetKindPage || wikiPath == "/" {
		return wikiPath
	}
	if strings.EqualFold(path.Ext(wikiPath), ".md") {
		return wikiPath
	}
	return wikiPath + ".md"
}
