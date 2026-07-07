package pagesave

import (
	"io"
	"log/slog"
	"sort"

	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/types"

	"github.com/perber/wiki/internal/core/tree"
	"github.com/perber/wiki/internal/properties"
)

type searchIndexObservation struct {
	PageID   tree.PageID
	Path     string
	FilePath string
	Content  string
}

type recordingSearchPageIndex struct {
	clearErr  error
	indexErr  error
	removeErr error

	clearCount int
	indexed    []searchIndexObservation
	removed    []tree.PageID
}

func (idx *recordingSearchPageIndex) Clear() error {
	idx.clearCount++
	return idx.clearErr
}

func (idx *recordingSearchPageIndex) IndexPage(path string, filePath string, pageID tree.PageID, _ string, _ tree.NodeKind, raw string) error {
	idx.indexed = append(idx.indexed, searchIndexObservation{
		PageID:   pageID,
		Path:     path,
		FilePath: filePath,
		Content:  raw,
	})
	return idx.indexErr
}

func (idx *recordingSearchPageIndex) RemovePage(pageID tree.PageID) error {
	idx.removed = append(idx.removed, pageID)
	return idx.removeErr
}

type recordingSearchBootstrapTree struct {
	ids     []tree.PageID
	pages   []*tree.Page
	errs    []error
	walkErr error
}

func (t recordingSearchBootstrapTree) WalkNodes(fn func(tree.PageID) error) error {
	if t.walkErr != nil {
		return t.walkErr
	}
	for _, id := range t.ids {
		if err := fn(id); err != nil {
			return err
		}
	}
	return nil
}

func (t recordingSearchBootstrapTree) GetPages([]tree.PageID) ([]*tree.Page, []error) {
	return t.pages, t.errs
}

func newUnitSearchIndexSideEffect(index *recordingSearchPageIndex) *SearchIndexSideEffect {
	return &SearchIndexSideEffect{index: index, log: discardPageSaveLog()}
}

func recordSearchIndexedPages(want ...searchIndexObservation) types.GomegaMatcher {
	ginkgo.GinkgoHelper()
	return WithTransform(func(index *recordingSearchPageIndex) []searchIndexObservation {
		return index.indexed
	}, Equal(want))
}

func recordSearchRemovedPages(want ...tree.PageID) types.GomegaMatcher {
	ginkgo.GinkgoHelper()
	return WithTransform(func(index *recordingSearchPageIndex) []tree.PageID {
		return index.removed
	}, Equal(want))
}

func recordSearchIndexCleared() types.GomegaMatcher {
	ginkgo.GinkgoHelper()
	return WithTransform(func(index *recordingSearchPageIndex) int {
		return index.clearCount
	}, Equal(1))
}

type routePathKindObservation struct {
	Path string
	Kind tree.NodeKind
}

type linkIndexObservation struct {
	Updated             []tree.PageID
	Healed              []tree.PageID
	DeletedOutgoing     []tree.PageID
	BrokenIncoming      []tree.PageID
	BrokenPathKinds     []routePathKindObservation
	BrokenPrefixes      []string
	BrokenPrefixByKinds []routePathKindObservation
}

type recordingLinkIndexService struct {
	err error
	obs linkIndexObservation
}

func (svc *recordingLinkIndexService) UpdateLinksForPage(page *tree.Page, _ string) error {
	svc.obs.Updated = append(svc.obs.Updated, page.ID)
	return svc.err
}

func (svc *recordingLinkIndexService) DeleteOutgoingLinksForPage(pageID tree.PageID) error {
	svc.obs.DeletedOutgoing = append(svc.obs.DeletedOutgoing, pageID)
	return svc.err
}

func (svc *recordingLinkIndexService) MarkIncomingLinksBrokenForPage(pageID tree.PageID) error {
	svc.obs.BrokenIncoming = append(svc.obs.BrokenIncoming, pageID)
	return svc.err
}

func (svc *recordingLinkIndexService) MarkLinksBrokenForPathAndKind(toPath tree.RoutePath, toKind tree.NodeKind) error {
	svc.obs.BrokenPathKinds = append(svc.obs.BrokenPathKinds, routePathKindObservation{Path: toPath.WikiPath(), Kind: toKind})
	return svc.err
}

func (svc *recordingLinkIndexService) MarkLinksBrokenForPrefix(prefix string) error {
	svc.obs.BrokenPrefixes = append(svc.obs.BrokenPrefixes, prefix)
	return svc.err
}

func (svc *recordingLinkIndexService) MarkLinksBrokenForPrefixAndKind(prefix string, rootKind tree.NodeKind) error {
	svc.obs.BrokenPrefixByKinds = append(svc.obs.BrokenPrefixByKinds, routePathKindObservation{Path: prefix, Kind: rootKind})
	return svc.err
}

func (svc *recordingLinkIndexService) HealLinksForExactPath(page *tree.Page) error {
	svc.obs.Healed = append(svc.obs.Healed, page.ID)
	return svc.err
}

func newUnitLinkIndexSideEffect(svc *recordingLinkIndexService) *LinkIndexSideEffect {
	return &LinkIndexSideEffect{svc: svc, log: discardPageSaveLog()}
}

func recordLinkIndexCalls(want linkIndexObservation) types.GomegaMatcher {
	ginkgo.GinkgoHelper()
	return WithTransform(func(svc *recordingLinkIndexService) linkIndexObservation {
		return svc.obs
	}, Equal(want))
}

type propertySetObservation struct {
	PageID tree.PageID
	Keys   []string
}

type propertyIndexObservation struct {
	Set     []propertySetObservation
	Deleted []tree.PageID
}

type recordingPropertiesIndexService struct {
	err error
	obs propertyIndexObservation
}

func (svc *recordingPropertiesIndexService) SetPropertiesForPage(pageID tree.PageID, props map[string]properties.PropertyEntry) error {
	keys := make([]string, 0, len(props))
	for key := range props {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	svc.obs.Set = append(svc.obs.Set, propertySetObservation{PageID: pageID, Keys: keys})
	return svc.err
}

func (svc *recordingPropertiesIndexService) DeletePropertiesForPage(pageID tree.PageID) error {
	svc.obs.Deleted = append(svc.obs.Deleted, pageID)
	return svc.err
}

func newUnitPropertiesSideEffect(svc *recordingPropertiesIndexService) *PropertiesSideEffect {
	return &PropertiesSideEffect{svc: svc, log: discardPageSaveLog()}
}

func recordPropertyIndexCalls(want propertyIndexObservation) types.GomegaMatcher {
	ginkgo.GinkgoHelper()
	return WithTransform(func(svc *recordingPropertiesIndexService) propertyIndexObservation {
		return svc.obs
	}, Equal(want))
}

type tagIndexObservation struct {
	Indexed []tree.PageID
	Deleted []tree.PageID
}

type recordingTagsIndexService struct {
	err error
	obs tagIndexObservation
}

func (svc *recordingTagsIndexService) IndexPageContent(pageID tree.PageID, _ string) error {
	svc.obs.Indexed = append(svc.obs.Indexed, pageID)
	return svc.err
}

func (svc *recordingTagsIndexService) DeletePageIndex(pageID tree.PageID) error {
	svc.obs.Deleted = append(svc.obs.Deleted, pageID)
	return svc.err
}

func newUnitTagsSideEffect(svc *recordingTagsIndexService) *TagsSideEffect {
	return &TagsSideEffect{svc: svc, log: discardPageSaveLog()}
}

func recordTagIndexCalls(want tagIndexObservation) types.GomegaMatcher {
	ginkgo.GinkgoHelper()
	return WithTransform(func(svc *recordingTagsIndexService) tagIndexObservation {
		return svc.obs
	}, Equal(want))
}

func unitPage(pageID tree.PageID, title string, routePath tree.RoutePath, raw string) *tree.Page {
	ginkgo.GinkgoHelper()
	node := unitPageNode(pageID, title, routePath)
	return &tree.Page{
		PageNode:   node,
		Content:    raw,
		RawContent: raw,
	}
}

func unitPageNode(pageID tree.PageID, title string, routePath tree.RoutePath) *tree.PageNode {
	ginkgo.GinkgoHelper()
	segments := splitUnitRoutePath(routePath)
	if len(segments) == 0 {
		return &tree.PageNode{ID: pageID, Title: title, Slug: newFixtureSlug("root"), Kind: tree.NodeKindPage}
	}

	var parent *tree.PageNode
	for _, segment := range segments[:len(segments)-1] {
		parent = &tree.PageNode{Title: segment.String(), Slug: segment, Kind: tree.NodeKindSection, Parent: parent}
	}
	return &tree.PageNode{
		ID:     pageID,
		Title:  title,
		Slug:   segments[len(segments)-1],
		Kind:   tree.NodeKindPage,
		Parent: parent,
	}
}

func splitUnitRoutePath(routePath tree.RoutePath) []tree.Slug {
	return routePath.Segments()
}

func discardPageSaveLog() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}
