package pagesave

import (
	"database/sql"
	"errors"
	"os"
	"path/filepath"

	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/gstruct"
	"github.com/onsi/gomega/types"

	"github.com/perber/wiki/internal/core/tree"
	"github.com/perber/wiki/internal/links"
	"github.com/perber/wiki/internal/properties"
	"github.com/perber/wiki/internal/search"
	"github.com/perber/wiki/internal/tags"
	"modernc.org/sqlite"
	sqlite3 "modernc.org/sqlite/lib"
)

var _ = ginkgo.Describe("page save side-effect fallback and failure behavior", func() {
	ginkgo.It("treats search bootstrap as a no-op when no index is configured", func() {
		Expect(NewSearchIndexSideEffect(nil, nil, nil).IndexAllPages()).To(Succeed())
	})

	ginkgo.It("returns search bootstrap tree walk errors", func() {
		_, _, index, _ := setupSearchSideEffect()
		walkFailedErr := errors.New("walk failed")
		effect := &SearchIndexSideEffect{
			index: index,
			tree:  failingSearchBootstrapTree{err: walkFailedErr},
		}

		Expect(effect.IndexAllPages()).To(MatchError(walkFailedErr))
	})

	ginkgo.It("applies link index updates across update, move, restore, and delete event shapes", func() {
		_, treeService, linkService, effect := setupLinkSideEffect()
		source := createMarkdownPage(treeService, "Source", "source", "[Target](/target)")
		affected := createMarkdownPage(treeService, "Affected", "affected", "[Other](/other)")

		NewLinkIndexSideEffect(nil, nil).Apply(PageSaveEvent{Operation: PageOperationDelete})
		effect.Apply(PageSaveEvent{Operation: PageOperationCreate})
		effect.healExact(nil)
		effect.Apply(PageSaveEvent{
			Operation:     PageOperationUpdate,
			SlugChanged:   true,
			OldPath:       tree.RoutePathFromString("/old-source"),
			After:         source,
			AffectedPages: []*tree.Page{affected},
		})
		effect.Apply(PageSaveEvent{
			Operation:     PageOperationUpdate,
			AffectedPages: []*tree.Page{affected},
		})
		effect.Apply(PageSaveEvent{
			Operation:     PageOperationMove,
			OldPath:       "",
			After:         source,
			AffectedPages: []*tree.Page{affected},
		})
		effect.Apply(PageSaveEvent{Operation: PageOperationRestore, After: source})
		effect.Apply(PageSaveEvent{Operation: PageOperationUpdate, After: source})

		outgoing, err := linkService.GetOutgoingLinksForPage(affected.ID)
		Expect(err).NotTo(HaveOccurred())
		Expect(outgoing.Count).To(BeNumerically(">=", 1))

		effect.Apply(PageSaveEvent{
			Operation:     PageOperationDelete,
			AffectedPages: []*tree.Page{source},
		})
		effect.Apply(PageSaveEvent{
			Operation:     PageOperationDelete,
			Before:        source,
			OldPath:       source.CalculateRoutePath(),
			AffectedPages: []*tree.Page{source},
		})
		effect.Apply(PageSaveEvent{
			Operation:     PageOperationDelete,
			Before:        source,
			OldPath:       tree.RoutePathFromString("/old-section"),
			AffectedPages: []*tree.Page{source, affected},
		})
	})

	ginkgo.It("logs link index write failures without panicking", func() {
		dir, treeService, _, effect := setupLinkSideEffect()
		page := createMarkdownPage(treeService, "Broken Links", "broken-links", "[Missing](/missing)")
		dropSQLiteTables(filepath.Join(dir, "links.db"), "links")

		Expect(func() {
			effect.Apply(PageSaveEvent{Operation: PageOperationCreate, After: page})
		}).NotTo(Panic())
		Expect(func() {
			effect.Apply(PageSaveEvent{Operation: PageOperationUpdate, After: page})
		}).NotTo(Panic())
		Expect(func() {
			effect.Apply(PageSaveEvent{
				Operation: PageOperationMove,
				OldPath:   tree.RoutePathFromString("/old-prefix"),
			})
		}).NotTo(Panic())
		Expect(func() {
			effect.Apply(PageSaveEvent{
				Operation: PageOperationMove,
				OldPath:   tree.RoutePathFromString("/old-prefix"),
				After:     page,
			})
		}).NotTo(Panic())
		Expect(func() {
			effect.Apply(PageSaveEvent{
				Operation:     PageOperationDelete,
				Before:        page,
				OldPath:       page.CalculateRoutePath(),
				AffectedPages: []*tree.Page{page},
			})
		}).NotTo(Panic())
	})

	ginkgo.It("indexes search fallback pages and exposes deterministic search index failures", func() {
		dir, treeService, index, effect := setupSearchSideEffect()
		page := createMarkdownPage(treeService, "Search Fallback", "search-fallback", "needle fallback content")

		effect.Apply(PageSaveEvent{
			Operation:     PageOperationUpdate,
			AffectedPages: []*tree.Page{page},
		})

		result, err := index.Search("needle", nil, 0, 10)
		Expect(err).NotTo(HaveOccurred())
		Expect(result).To(gstruct.PointTo(gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
			"Count": Equal(1),
			"Items": ContainElement(HaveField("PageID", Equal(page.ID))),
		})))

		effect.indexPage(nil)
		unreadable := createMarkdownPage(treeService, "Unreadable", "unreadable", "unreadable content")
		relativeContentPath, err := treeService.ContentPathForNode(unreadable.PageNode)
		Expect(err).NotTo(HaveOccurred())
		Expect(os.Remove(filepath.Join(treeService.RootDir(), relativeContentPath))).To(Succeed())
		Expect(effect.IndexAllPages()).To(Succeed())

		dropSQLiteTables(filepath.Join(dir, "search.db"), "pages")
		Expect(effect.IndexAllPages()).To(HaveSQLiteErrorCode(sqlite3.SQLITE_ERROR))
		Expect(func() {
			effect.Apply(PageSaveEvent{Operation: PageOperationCreate, After: page})
		}).NotTo(Panic())
		Expect(func() {
			effect.Apply(PageSaveEvent{
				Operation:     PageOperationDelete,
				AffectedPages: []*tree.Page{page},
			})
		}).NotTo(Panic())
	})

	ginkgo.It("updates properties from fallback pages and logs write failures", func() {
		dir, treeService, service, effect := setupPropertiesSideEffect()
		page := createRawPage(treeService, "Properties Fallback", "properties-fallback", "---\nstatus: staged\n---\n\nBody")

		effect.Apply(PageSaveEvent{
			Operation:     PageOperationRestore,
			AffectedPages: []*tree.Page{page},
		})
		effect.Apply(PageSaveEvent{Operation: PageOperationMove, After: page})

		ids, err := service.GetPageIDsByProperty("status", "staged")
		Expect(err).NotTo(HaveOccurred())
		Expect(ids).To(ConsistOf(page.ID))

		dropSQLiteTables(filepath.Join(dir, "properties.db"), "page_properties")
		Expect(func() {
			effect.Apply(PageSaveEvent{Operation: PageOperationCreate, After: page})
		}).NotTo(Panic())
		Expect(func() {
			effect.Apply(PageSaveEvent{
				Operation:     PageOperationDelete,
				AffectedPages: []*tree.Page{page},
			})
		}).NotTo(Panic())
	})

	ginkgo.It("updates tags from fallback pages and logs write failures", func() {
		dir, treeService, service, effect := setupTagsSideEffect()
		page := createRawPage(treeService, "Tags Fallback", "tags-fallback", "---\ntags:\n  - branch\n---\n\nBody")

		effect.Apply(PageSaveEvent{
			Operation:     PageOperationUpdate,
			AffectedPages: []*tree.Page{page},
		})
		effect.Apply(PageSaveEvent{Operation: PageOperationMove, After: page})

		ids, err := service.GetPageIDsByTags([]string{"branch"})
		Expect(err).NotTo(HaveOccurred())
		Expect(ids).To(ConsistOf(page.ID))

		dropSQLiteTables(filepath.Join(dir, "tags.db"), "page_tags", "page_meta")
		Expect(func() {
			effect.Apply(PageSaveEvent{Operation: PageOperationCreate, After: page})
		}).NotTo(Panic())
		Expect(func() {
			effect.Apply(PageSaveEvent{
				Operation:     PageOperationDelete,
				AffectedPages: []*tree.Page{page},
			})
		}).NotTo(Panic())
	})
})

func setupLinkSideEffect() (string, *tree.TreeService, *links.LinkService, *LinkIndexSideEffect) {
	ginkgo.GinkgoHelper()
	dir, treeService := setupTreeService()
	store, err := links.NewLinksStore(dir)
	Expect(err).NotTo(HaveOccurred())
	ginkgo.DeferCleanup(func() {
		Expect(store.Close()).To(Succeed())
	})
	service := links.NewLinkService(dir, treeService, store)
	return dir, treeService, service, NewLinkIndexSideEffect(service, nil)
}

func setupSearchSideEffect() (string, *tree.TreeService, *search.SQLiteIndex, *SearchIndexSideEffect) {
	ginkgo.GinkgoHelper()
	dir, treeService := setupTreeService()
	index, err := search.NewSQLiteIndex(dir)
	Expect(err).NotTo(HaveOccurred())
	ginkgo.DeferCleanup(func() {
		Expect(index.Close()).To(Succeed())
	})
	return dir, treeService, index, NewSearchIndexSideEffect(index, treeService, nil)
}

type failingSearchBootstrapTree struct {
	err error
}

func (t failingSearchBootstrapTree) WalkNodes(func(tree.PageID) error) error {
	return t.err
}

func (t failingSearchBootstrapTree) GetPages([]tree.PageID) ([]*tree.Page, []error) {
	return nil, nil
}

func setupPropertiesSideEffect() (string, *tree.TreeService, *properties.PropertiesService, *PropertiesSideEffect) {
	ginkgo.GinkgoHelper()
	dir, treeService := setupTreeService()
	store, err := properties.NewPropertiesStore(dir)
	Expect(err).NotTo(HaveOccurred())
	ginkgo.DeferCleanup(func() {
		Expect(store.Close()).To(Succeed())
	})
	service := properties.NewPropertiesService(store)
	return dir, treeService, service, NewPropertiesSideEffect(service, nil)
}

func setupTagsSideEffect() (string, *tree.TreeService, *tags.TagsService, *TagsSideEffect) {
	ginkgo.GinkgoHelper()
	dir, treeService := setupTreeService()
	store, err := tags.NewTagsStore(dir)
	Expect(err).NotTo(HaveOccurred())
	ginkgo.DeferCleanup(func() {
		Expect(store.Close()).To(Succeed())
	})
	service := tags.NewTagsService(store)
	return dir, treeService, service, NewTagsSideEffect(service, nil)
}

func setupTreeService() (string, *tree.TreeService) {
	ginkgo.GinkgoHelper()
	dir := tempPagesaveDir()
	treeService := tree.NewTreeService(dir)
	Expect(treeService.LoadTree()).To(Succeed())
	return dir, treeService
}

func createMarkdownPage(treeService *tree.TreeService, title, slug, content string) *tree.Page {
	ginkgo.GinkgoHelper()
	kind := tree.NodeKindPage
	id, err := treeService.CreateNode(newFixtureUserID("system"), nil, title, newFixtureSlug(slug), &kind)
	Expect(err).NotTo(HaveOccurred())
	page, err := treeService.GetPage(*id)
	Expect(err).NotTo(HaveOccurred())
	Expect(treeService.UpdateNode(newFixtureUserID("system"), *id, title, newFixtureSlug(slug), &content, newFixturePageVersion(page.Version()), false)).To(Succeed())
	page, err = treeService.GetPage(*id)
	Expect(err).NotTo(HaveOccurred())
	return page
}

func createRawPage(treeService *tree.TreeService, title, slug, raw string) *tree.Page {
	ginkgo.GinkgoHelper()
	kind := tree.NodeKindPage
	id, err := treeService.CreateNode(newFixtureUserID("system"), nil, title, newFixtureSlug(slug), &kind)
	Expect(err).NotTo(HaveOccurred())
	Expect(treeService.UpdateNodeUncheckedVersion(newFixtureUserID("system"), *id, title, newFixtureSlug(slug), &raw, true)).To(Succeed())
	page, err := treeService.GetPage(*id)
	Expect(err).NotTo(HaveOccurred())
	return page
}

func dropSQLiteTables(dbPath string, tableNames ...string) {
	ginkgo.GinkgoHelper()
	db, err := sql.Open("sqlite", dbPath)
	Expect(err).NotTo(HaveOccurred())
	for _, tableName := range tableNames {
		_, err = db.Exec("DROP TABLE IF EXISTS " + tableName)
		Expect(err).NotTo(HaveOccurred())
	}
	Expect(db.Close()).To(Succeed())
}

func HaveSQLiteErrorCode(code int) types.GomegaMatcher {
	return WithTransform(func(err error) int {
		var sqliteErr *sqlite.Error
		if !errors.As(err, &sqliteErr) {
			return 0
		}
		return sqliteErr.Code()
	}, Equal(code))
}
