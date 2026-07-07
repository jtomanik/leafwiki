package pages

import (
	"errors"
	"os"
	"path/filepath"

	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/gstruct"
	"github.com/onsi/gomega/types"

	"github.com/gin-gonic/gin"
	"github.com/perber/wiki/internal/core/tree"
	"github.com/perber/wiki/internal/links"
	"github.com/perber/wiki/internal/wiki/pagesave"
)

func ptrPageID(id tree.PageID) *tree.PageID {
	return &id
}

func matchPageSaveEvent(fields gstruct.Fields) types.GomegaMatcher {
	return gstruct.MatchFields(gstruct.IgnoreExtras, fields)
}

func matchRefactorAffectedPage(fields gstruct.Fields) types.GomegaMatcher {
	return gstruct.MatchFields(gstruct.IgnoreExtras, fields)
}

func matchPathChangeSnapshot(fields gstruct.Fields) types.GomegaMatcher {
	return gstruct.MatchFields(gstruct.IgnoreExtras, fields)
}

type recordingPageSaveEffect struct {
	events []pagesave.PageSaveEvent
}

func (e *recordingPageSaveEffect) Apply(event pagesave.PageSaveEvent) {
	e.events = append(e.events, event)
}

type failingPageSaveEffect struct {
	err error
}

func (e *failingPageSaveEffect) Apply(pagesave.PageSaveEvent) {}

func (e *failingPageSaveEffect) ApplyRequired(pagesave.PageSaveEvent) error {
	return e.err
}

type failingSummaryPageSaveEffect struct {
	summary string
	err     error
}

func (e *failingSummaryPageSaveEffect) Apply(pagesave.PageSaveEvent) {}

func (e *failingSummaryPageSaveEffect) ApplyRequired(event pagesave.PageSaveEvent) error {
	if event.Summary == e.summary {
		return e.err
	}
	return nil
}

func writePageMarkdown(deps *routesSpecDeps, page *tree.Page, raw string) {
	ginkgo.GinkgoHelper()

	path := pageMarkdownPath(deps, page)
	Expect(os.WriteFile(path, []byte(raw), 0o644)).To(Succeed())
}

func removePageMarkdown(deps *routesSpecDeps, page *tree.Page) {
	ginkgo.GinkgoHelper()

	Expect(os.Remove(pageMarkdownPath(deps, page))).To(Succeed())
}

func pageMarkdownPath(deps *routesSpecDeps, page *tree.Page) string {
	ginkgo.GinkgoHelper()

	rel, err := deps.tree.ContentPathForNode(page.PageNode)
	Expect(err).NotTo(HaveOccurred())
	return filepath.Join(deps.tree.RootDir(), filepath.FromSlash(rel))
}

func testFixturePage(id tree.PageID, title string, slug tree.Slug, kind tree.NodeKind) *tree.Page {
	return testPage(id, title, slug, kind)
}

func testPage(id tree.PageID, title string, slug tree.Slug, kind tree.NodeKind) *tree.Page {
	node := &tree.PageNode{
		ID:    id,
		Title: title,
		Slug:  slug,
		Kind:  kind,
	}
	return &tree.Page{PageNode: node, Content: title + " body"}
}

func testFixtureChildPage(parent *tree.Page, id tree.PageID, title string, slug tree.Slug, kind tree.NodeKind) *tree.Page {
	page := testFixturePage(id, title, slug, kind)
	page.Parent = parent.PageNode
	return page
}

func fakeEnsureTree(resultPage *tree.PageNode, created []*tree.PageNode, getPages func([]tree.PageID) ([]*tree.Page, []error)) *pageUseCaseFakeTree {
	return &pageUseCaseFakeTree{
		lookupPagePathFunc: func(routePath tree.RoutePath) (*tree.PathLookup, error) {
			return &tree.PathLookup{Path: routePath, CanCreate: true}, nil
		},
		ensurePagePathFunc: func(tree.UserID, tree.RoutePath, string, *tree.NodeKind) (*tree.EnsurePathResult, error) {
			return &tree.EnsurePathResult{Page: resultPage, Created: created}, nil
		},
		getPagesFunc: getPages,
	}
}

func fakeTreeWithPages(pages ...*tree.Page) *pageUseCaseFakeTree {
	byID := make(map[tree.PageID]*tree.Page, len(pages))
	for _, page := range pages {
		byID[page.ID] = page
	}
	return &pageUseCaseFakeTree{
		getPageFunc: func(id tree.PageID) (*tree.Page, error) {
			page := byID[id]
			if page == nil {
				return nil, tree.ErrPageNotFound
			}
			return page, nil
		},
		getPagesFunc: func(ids []tree.PageID) ([]*tree.Page, []error) {
			out := make([]*tree.Page, len(ids))
			errs := make([]error, len(ids))
			for i, id := range ids {
				out[i] = byID[id]
				if out[i] == nil {
					errs[i] = tree.ErrPageNotFound
				}
			}
			return out, errs
		},
	}
}

type pageUseCaseFakeTree struct {
	findPageByIDFunc               func(tree.PageID) (*tree.PageNode, error)
	createNodeFunc                 func(tree.UserID, *tree.PageID, string, tree.Slug, *tree.NodeKind) (*tree.PageID, error)
	getPageFunc                    func(tree.PageID) (*tree.Page, error)
	updateNodeFunc                 func(tree.UserID, tree.PageID, string, tree.Slug, *string, tree.PageVersion, bool) error
	getPagesFunc                   func([]tree.PageID) ([]*tree.Page, []error)
	deleteNodeFunc                 func(tree.UserID, tree.PageID, bool, tree.PageVersion) error
	moveNodeFunc                   func(tree.UserID, tree.PageID, tree.PageID, tree.PageVersion) error
	convertNodeFunc                func(tree.UserID, tree.PageID, tree.NodeKind, tree.PageVersion) error
	deleteNodeUncheckedVersionFunc func(tree.UserID, tree.PageID, bool) error
	updateNodeUncheckedVersionFunc func(tree.UserID, tree.PageID, string, tree.Slug, *string, bool) error
	lookupPagePathFunc             func(tree.RoutePath) (*tree.PathLookup, error)
	ensurePagePathFunc             func(tree.UserID, tree.RoutePath, string, *tree.NodeKind) (*tree.EnsurePathResult, error)
	bulkUpdateContentFunc          func(tree.UserID, []tree.BulkContentUpdate) []error
}

func (f *pageUseCaseFakeTree) FindPageByID(id tree.PageID) (*tree.PageNode, error) {
	if f.findPageByIDFunc != nil {
		return f.findPageByIDFunc(id)
	}
	return nil, errors.New("unexpected FindPageByID")
}

func (f *pageUseCaseFakeTree) CreateNode(userID tree.UserID, parentID *tree.PageID, title string, slug tree.Slug, kind *tree.NodeKind) (*tree.PageID, error) {
	if f.createNodeFunc != nil {
		return f.createNodeFunc(userID, parentID, title, slug, kind)
	}
	return nil, errors.New("unexpected CreateNode")
}

func (f *pageUseCaseFakeTree) GetPage(id tree.PageID) (*tree.Page, error) {
	if f.getPageFunc != nil {
		return f.getPageFunc(id)
	}
	return nil, errors.New("unexpected GetPage")
}

func (f *pageUseCaseFakeTree) UpdateNode(userID tree.UserID, id tree.PageID, title string, slug tree.Slug, content *string, version tree.PageVersion, fromImport bool) error {
	if f.updateNodeFunc != nil {
		return f.updateNodeFunc(userID, id, title, slug, content, version, fromImport)
	}
	return errors.New("unexpected UpdateNode")
}

func (f *pageUseCaseFakeTree) GetPages(ids []tree.PageID) ([]*tree.Page, []error) {
	if f.getPagesFunc != nil {
		return f.getPagesFunc(ids)
	}
	pages := make([]*tree.Page, len(ids))
	errs := make([]error, len(ids))
	for i, id := range ids {
		pages[i] = testPage(id, "Generated Page", newFixtureSlug("generated-page"), tree.NodeKindPage)
	}
	return pages, errs
}

func (f *pageUseCaseFakeTree) DeleteNode(userID tree.UserID, id tree.PageID, recursive bool, version tree.PageVersion) error {
	if f.deleteNodeFunc != nil {
		return f.deleteNodeFunc(userID, id, recursive, version)
	}
	return errors.New("unexpected DeleteNode")
}

func (f *pageUseCaseFakeTree) MoveNode(userID tree.UserID, id tree.PageID, parentID tree.PageID, version tree.PageVersion) error {
	if f.moveNodeFunc != nil {
		return f.moveNodeFunc(userID, id, parentID, version)
	}
	return errors.New("unexpected MoveNode")
}

func (f *pageUseCaseFakeTree) ConvertNode(userID tree.UserID, id tree.PageID, kind tree.NodeKind, version tree.PageVersion) error {
	if f.convertNodeFunc != nil {
		return f.convertNodeFunc(userID, id, kind, version)
	}
	return errors.New("unexpected ConvertNode")
}

func (f *pageUseCaseFakeTree) DeleteNodeUncheckedVersion(userID tree.UserID, id tree.PageID, recursive bool) error {
	if f.deleteNodeUncheckedVersionFunc != nil {
		return f.deleteNodeUncheckedVersionFunc(userID, id, recursive)
	}
	return errors.New("unexpected DeleteNodeUncheckedVersion")
}

func (f *pageUseCaseFakeTree) UpdateNodeUncheckedVersion(userID tree.UserID, id tree.PageID, title string, slug tree.Slug, content *string, fromImport bool) error {
	if f.updateNodeUncheckedVersionFunc != nil {
		return f.updateNodeUncheckedVersionFunc(userID, id, title, slug, content, fromImport)
	}
	return errors.New("unexpected UpdateNodeUncheckedVersion")
}

func (f *pageUseCaseFakeTree) LookupPagePath(routePath tree.RoutePath) (*tree.PathLookup, error) {
	if f.lookupPagePathFunc != nil {
		return f.lookupPagePathFunc(routePath)
	}
	return nil, errors.New("unexpected LookupPagePath")
}

func (f *pageUseCaseFakeTree) EnsurePagePath(userID tree.UserID, routePath tree.RoutePath, title string, kind *tree.NodeKind) (*tree.EnsurePathResult, error) {
	if f.ensurePagePathFunc != nil {
		return f.ensurePagePathFunc(userID, routePath, title, kind)
	}
	return nil, errors.New("unexpected EnsurePagePath")
}

func (f *pageUseCaseFakeTree) BulkUpdateContent(userID tree.UserID, updates []tree.BulkContentUpdate) []error {
	if f.bulkUpdateContentFunc != nil {
		return f.bulkUpdateContentFunc(userID, updates)
	}
	return make([]error, len(updates))
}

type fakeRefactorLinks struct {
	matches []links.RefactorLinkMatch
	err     error
}

func (f *fakeRefactorLinks) GetRefactorMatchesForPrefixAndKind(tree.RoutePath, tree.NodeKind) ([]links.RefactorLinkMatch, error) {
	return f.matches, f.err
}

type fakePageAssets struct {
	copyErr     error
	deleteErr   error
	copyCalls   int
	deleteCalls int
}

type pageAssetCalls struct {
	Copy   int
	Delete int
}

func pageAssetCallCounts(assets *fakePageAssets) pageAssetCalls {
	return pageAssetCalls{Copy: assets.copyCalls, Delete: assets.deleteCalls}
}

func (a *fakePageAssets) CopyAllAssets(*tree.PageNode, *tree.PageNode) error {
	a.copyCalls++
	return a.copyErr
}

func (a *fakePageAssets) DeleteAllAssetsForPage(*tree.PageNode) error {
	a.deleteCalls++
	return a.deleteErr
}

func ginParams(key string, value string) gin.Params {
	ginkgo.GinkgoHelper()
	return gin.Params{{Key: key, Value: value}}
}

func readmeLookupPathForRoute(routePath tree.RoutePath) string {
	return filepath.ToSlash(filepath.Join(routePath.FilesystemPath(), "README.md"))
}

func ginPageIDParams(id tree.PageID) gin.Params {
	ginkgo.GinkgoHelper()
	return gin.Params{{Key: "id", Value: id.String()}}
}
