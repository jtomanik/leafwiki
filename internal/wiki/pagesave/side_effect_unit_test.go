package pagesave

import (
	"errors"

	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/perber/wiki/internal/core/tree"
	"github.com/perber/wiki/internal/links"
	"github.com/perber/wiki/internal/properties"
	"github.com/perber/wiki/internal/search"
	"github.com/perber/wiki/internal/tags"
)

var _ = ginkgo.Describe("page save side-effect constructors", ginkgo.Label("unit"), func() {
	ginkgo.It("normalizes omitted and configured concrete services", func() {
		Expect(NewSearchIndexSideEffect(nil, nil, nil).index).To(BeNil())
		Expect(NewSearchIndexSideEffect(&search.SQLiteIndex{}, nil, nil).index).NotTo(BeNil())
		Expect(NewLinkIndexSideEffect(nil, nil).svc).To(BeNil())
		Expect(NewLinkIndexSideEffect(&links.LinkService{}, nil).svc).NotTo(BeNil())
		Expect(NewPropertiesSideEffect(nil, nil).svc).To(BeNil())
		Expect(NewPropertiesSideEffect(&properties.PropertiesService{}, nil).svc).NotTo(BeNil())
		Expect(NewTagsSideEffect(nil, nil).svc).To(BeNil())
		Expect(NewTagsSideEffect(&tags.TagsService{}, nil).svc).NotTo(BeNil())
	})
})

var _ = ginkgo.Describe("page save search side effects", ginkgo.Label("unit"), func() {
	ginkgo.It("indexes created, restored, updated, and moved pages", func() {
		index := &recordingSearchPageIndex{}
		effect := newUnitSearchIndexSideEffect(index)
		created := unitPage(newFixturePageID("created"), "Created", newFixtureRoutePath("created"), "created body")
		affected := unitPage(newFixturePageID("affected"), "Affected", newFixtureRoutePath("section/affected"), "affected body")

		effect.Apply(PageSaveEvent{Operation: PageOperationCreate, After: created})
		effect.Apply(PageSaveEvent{Operation: PageOperationRestore, AffectedPages: []*tree.Page{affected}})
		effect.Apply(PageSaveEvent{Operation: PageOperationUpdate, After: created})
		effect.Apply(PageSaveEvent{Operation: PageOperationMove, AffectedPages: []*tree.Page{affected}})

		Expect(index).To(recordSearchIndexedPages(
			searchIndexObservation{PageID: newFixturePageID("created"), Path: "created", FilePath: "created.md", Content: "created body"},
			searchIndexObservation{PageID: newFixturePageID("affected"), Path: "section/affected", FilePath: "section/affected.md", Content: "affected body"},
			searchIndexObservation{PageID: newFixturePageID("created"), Path: "created", FilePath: "created.md", Content: "created body"},
			searchIndexObservation{PageID: newFixturePageID("affected"), Path: "section/affected", FilePath: "section/affected.md", Content: "affected body"},
		))
	})

	ginkgo.It("removes deleted pages and tolerates index failures", func() {
		removeErr := errors.New("remove failed")
		index := &recordingSearchPageIndex{indexErr: errors.New("index failed"), removeErr: removeErr}
		effect := newUnitSearchIndexSideEffect(index)
		page := unitPage(newFixturePageID("page-1"), "Page", newFixtureRoutePath("page-1"), "body")

		effect.Apply(PageSaveEvent{Operation: PageOperationCreate, After: page})
		effect.indexPage(nil)
		effect.Apply(PageSaveEvent{Operation: PageOperationDelete, AffectedPages: []*tree.Page{page}})

		Expect(index).To(recordSearchIndexedPages(searchIndexObservation{
			PageID:   newFixturePageID("page-1"),
			Path:     "page-1",
			FilePath: "page-1.md",
			Content:  "body",
		}))
		Expect(index).To(recordSearchRemovedPages(newFixturePageID("page-1")))
	})

	ginkgo.It("clears and rebuilds the search index from bootstrap pages", func() {
		index := &recordingSearchPageIndex{}
		first := unitPage(newFixturePageID("first"), "First", newFixtureRoutePath("first"), "first body")
		second := unitPage(newFixturePageID("second"), "Second", newFixtureRoutePath(""), "second body")
		effect := &SearchIndexSideEffect{
			index: index,
			tree: recordingSearchBootstrapTree{
				ids:   []tree.PageID{first.ID, second.ID},
				pages: []*tree.Page{first, second},
				errs:  []error{nil, errors.New("skip second")},
			},
			log: discardPageSaveLog(),
		}

		Expect(effect.IndexAllPages()).To(Succeed())

		Expect(index).To(recordSearchIndexCleared())
		Expect(index).To(recordSearchIndexedPages(searchIndexObservation{
			PageID:   first.ID,
			Path:     "first",
			FilePath: "first.md",
			Content:  "first body",
		}))
	})

	ginkgo.It("returns bootstrap clear and walk failures", func() {
		clearErr := errors.New("clear failed")
		clearEffect := &SearchIndexSideEffect{
			index: &recordingSearchPageIndex{clearErr: clearErr},
			tree:  recordingSearchBootstrapTree{},
			log:   discardPageSaveLog(),
		}
		Expect(clearEffect.IndexAllPages()).To(MatchError(clearErr))

		walkErr := errors.New("walk failed")
		walkEffect := &SearchIndexSideEffect{
			index: &recordingSearchPageIndex{},
			tree:  recordingSearchBootstrapTree{walkErr: walkErr},
			log:   discardPageSaveLog(),
		}
		Expect(walkEffect.IndexAllPages()).To(MatchError(walkErr))
	})
})

var _ = ginkgo.Describe("page save link side effects", ginkgo.Label("unit"), func() {
	ginkgo.It("treats omitted link services as no-ops", func() {
		NewLinkIndexSideEffect(nil, nil).Apply(PageSaveEvent{Operation: PageOperationDelete})
	})

	ginkgo.It("updates links and heals exact paths for saved pages", func() {
		svc := &recordingLinkIndexService{}
		effect := newUnitLinkIndexSideEffect(svc)
		page := unitPage(newFixturePageID("page-1"), "Page", newFixtureRoutePath("page-1"), "[Target](/target)")
		affected := unitPage(newFixturePageID("affected"), "Affected", newFixtureRoutePath("affected"), "[Other](/other)")

		effect.Apply(PageSaveEvent{Operation: PageOperationCreate, After: page})
		effect.Apply(PageSaveEvent{Operation: PageOperationRestore, After: page})
		effect.Apply(PageSaveEvent{Operation: PageOperationUpdate, After: page})
		effect.Apply(PageSaveEvent{Operation: PageOperationUpdate, AffectedPages: []*tree.Page{affected}})
		effect.updateAndHeal(nil)
		effect.healExact(nil)

		Expect(svc).To(recordLinkIndexCalls(linkIndexObservation{
			Updated: []tree.PageID{page.ID, page.ID, page.ID, affected.ID},
			Healed:  []tree.PageID{page.ID, page.ID, page.ID, affected.ID},
		}))
	})

	ginkgo.It("marks old paths during slug changes and moves", func() {
		svc := &recordingLinkIndexService{}
		effect := newUnitLinkIndexSideEffect(svc)
		page := unitPage(newFixturePageID("page-1"), "Page", newFixtureRoutePath("new-page"), "body")
		affected := unitPage(newFixturePageID("affected"), "Affected", newFixtureRoutePath("affected"), "body")

		effect.Apply(PageSaveEvent{
			Operation:     PageOperationUpdate,
			SlugChanged:   true,
			OldPath:       newFixtureRoutePath("/old-page"),
			After:         page,
			AffectedPages: []*tree.Page{affected},
		})
		effect.Apply(PageSaveEvent{
			Operation:     PageOperationMove,
			OldPath:       newFixtureRoutePath("/old-section"),
			AffectedPages: []*tree.Page{affected},
		})
		effect.Apply(PageSaveEvent{
			Operation:     PageOperationMove,
			OldPath:       newFixtureRoutePath("/old-page-with-kind"),
			After:         page,
			AffectedPages: []*tree.Page{page},
		})
		effect.markBrokenForOldPath(newFixtureRoutePath(""), page)

		Expect(svc).To(recordLinkIndexCalls(linkIndexObservation{
			Updated:             []tree.PageID{affected.ID, affected.ID, page.ID},
			Healed:              []tree.PageID{affected.ID, affected.ID, page.ID},
			BrokenPrefixes:      []string{"/old-section"},
			BrokenPrefixByKinds: []routePathKindObservation{{Path: "/old-page", Kind: tree.NodeKindPage}, {Path: "/old-page-with-kind", Kind: tree.NodeKindPage}},
		}))
	})

	ginkgo.It("removes outgoing links and marks incoming links for deletes", func() {
		svc := &recordingLinkIndexService{}
		effect := newUnitLinkIndexSideEffect(svc)
		page := unitPage(newFixturePageID("page-1"), "Page", newFixtureRoutePath("page-1"), "body")
		child := unitPage(newFixturePageID("child"), "Child", newFixtureRoutePath("page-1/child"), "body")

		effect.Apply(PageSaveEvent{Operation: PageOperationDelete, AffectedPages: []*tree.Page{page}})
		effect.Apply(PageSaveEvent{
			Operation:     PageOperationDelete,
			Before:        page,
			OldPath:       newFixtureRoutePath("/page-1"),
			AffectedPages: []*tree.Page{page},
		})
		effect.Apply(PageSaveEvent{
			Operation:     PageOperationDelete,
			Before:        page,
			OldPath:       newFixtureRoutePath("/page-1"),
			AffectedPages: []*tree.Page{page, child},
		})

		Expect(svc).To(recordLinkIndexCalls(linkIndexObservation{
			DeletedOutgoing: []tree.PageID{page.ID, page.ID, page.ID, child.ID},
			BrokenIncoming:  []tree.PageID{page.ID},
			BrokenPathKinds: []routePathKindObservation{{Path: "/page-1", Kind: tree.NodeKindPage}},
			BrokenPrefixByKinds: []routePathKindObservation{
				{Path: "/page-1", Kind: tree.NodeKindPage},
			},
		}))
	})

	ginkgo.It("logs link index failures without changing event flow", func() {
		svc := &recordingLinkIndexService{err: errors.New("link write failed")}
		effect := newUnitLinkIndexSideEffect(svc)
		page := unitPage(newFixturePageID("page-1"), "Page", newFixtureRoutePath("page-1"), "body")

		effect.Apply(PageSaveEvent{Operation: PageOperationCreate, After: page})
		effect.Apply(PageSaveEvent{Operation: PageOperationUpdate, After: page})
		effect.Apply(PageSaveEvent{Operation: PageOperationMove, OldPath: newFixtureRoutePath("/old")})
		effect.Apply(PageSaveEvent{Operation: PageOperationMove, OldPath: newFixtureRoutePath("/old"), After: page})
		effect.Apply(PageSaveEvent{
			Operation:     PageOperationDelete,
			Before:        page,
			OldPath:       newFixtureRoutePath("/old"),
			AffectedPages: []*tree.Page{page},
		})

		Expect(svc).To(recordLinkIndexCalls(linkIndexObservation{
			Updated:             []tree.PageID{page.ID, page.ID},
			Healed:              []tree.PageID{page.ID, page.ID},
			DeletedOutgoing:     []tree.PageID{page.ID},
			BrokenIncoming:      []tree.PageID{page.ID},
			BrokenPrefixes:      []string{"/old"},
			BrokenPathKinds:     []routePathKindObservation{{Path: "/old", Kind: tree.NodeKindPage}},
			BrokenPrefixByKinds: []routePathKindObservation{{Path: "/old", Kind: tree.NodeKindPage}},
		}))
	})
})

var _ = ginkgo.Describe("page save metadata side effects", ginkgo.Label("unit"), func() {
	ginkgo.It("indexes and deletes properties for saved pages", func() {
		svc := &recordingPropertiesIndexService{}
		effect := newUnitPropertiesSideEffect(svc)
		page := unitPage(newFixturePageID("page-1"), "Page", newFixtureRoutePath("page-1"), "---\nstatus: ready\n---\n\nBody")
		affected := unitPage(newFixturePageID("affected"), "Affected", newFixtureRoutePath("affected"), "---\nowner: docs\n---\n\nBody")

		effect.Apply(PageSaveEvent{Operation: PageOperationCreate, After: page})
		effect.Apply(PageSaveEvent{Operation: PageOperationRestore, AffectedPages: []*tree.Page{affected}})
		effect.Apply(PageSaveEvent{Operation: PageOperationMove, After: page})
		effect.Apply(PageSaveEvent{Operation: PageOperationDelete, AffectedPages: []*tree.Page{page}})

		Expect(svc).To(recordPropertyIndexCalls(propertyIndexObservation{
			Set: []propertySetObservation{
				{PageID: page.ID, Keys: []string{"status"}},
				{PageID: affected.ID, Keys: []string{"owner"}},
			},
			Deleted: []tree.PageID{page.ID},
		}))
	})

	ginkgo.It("indexes and deletes tags for saved pages", func() {
		svc := &recordingTagsIndexService{}
		effect := newUnitTagsSideEffect(svc)
		page := unitPage(newFixturePageID("page-1"), "Page", newFixtureRoutePath("page-1"), "---\ntags:\n  - docs\n---\n\nBody")
		affected := unitPage(newFixturePageID("affected"), "Affected", newFixtureRoutePath("affected"), "---\ntags:\n  - review\n---\n\nBody")

		effect.Apply(PageSaveEvent{Operation: PageOperationUpdate, After: page})
		effect.Apply(PageSaveEvent{Operation: PageOperationRestore, AffectedPages: []*tree.Page{affected}})
		effect.Apply(PageSaveEvent{Operation: PageOperationMove, After: page})
		effect.Apply(PageSaveEvent{Operation: PageOperationDelete, AffectedPages: []*tree.Page{page}})

		Expect(svc).To(recordTagIndexCalls(tagIndexObservation{
			Indexed: []tree.PageID{page.ID, affected.ID},
			Deleted: []tree.PageID{page.ID},
		}))
	})

	ginkgo.It("treats omitted metadata services as no-ops and logs write failures", func() {
		page := unitPage(newFixturePageID("page-1"), "Page", newFixtureRoutePath("page-1"), "---\ntags:\n  - docs\n---\n\nBody")
		NewPropertiesSideEffect(nil, nil).Apply(PageSaveEvent{Operation: PageOperationCreate, After: page})
		NewTagsSideEffect(nil, nil).Apply(PageSaveEvent{Operation: PageOperationCreate, After: page})

		propSvc := &recordingPropertiesIndexService{err: errors.New("properties failed")}
		newUnitPropertiesSideEffect(propSvc).Apply(PageSaveEvent{Operation: PageOperationCreate, After: page})
		newUnitPropertiesSideEffect(propSvc).Apply(PageSaveEvent{Operation: PageOperationDelete, AffectedPages: []*tree.Page{page}})
		Expect(propSvc).To(recordPropertyIndexCalls(propertyIndexObservation{
			Set:     []propertySetObservation{{PageID: page.ID, Keys: []string{}}},
			Deleted: []tree.PageID{page.ID},
		}))

		tagSvc := &recordingTagsIndexService{err: errors.New("tags failed")}
		newUnitTagsSideEffect(tagSvc).Apply(PageSaveEvent{Operation: PageOperationCreate, After: page})
		newUnitTagsSideEffect(tagSvc).Apply(PageSaveEvent{Operation: PageOperationDelete, AffectedPages: []*tree.Page{page}})
		Expect(tagSvc).To(recordTagIndexCalls(tagIndexObservation{
			Indexed: []tree.PageID{page.ID},
			Deleted: []tree.PageID{page.ID},
		}))
	})
})
