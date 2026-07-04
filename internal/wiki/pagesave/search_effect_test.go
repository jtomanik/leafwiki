package pagesave

import (
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/gstruct"
	"github.com/onsi/gomega/types"

	"github.com/perber/wiki/internal/core/tree"
	"github.com/perber/wiki/internal/search"
)

func setupSearchTest() (*tree.TreeService, *search.SQLiteIndex, *SearchIndexSideEffect) {
	ginkgo.GinkgoHelper()

	tmp := tempPagesaveDir()
	treeSvc := tree.NewTreeService(tmp)
	Expect(treeSvc.LoadTree()).To(Succeed())

	index, err := search.NewSQLiteIndex(tmp)
	Expect(err).NotTo(HaveOccurred())
	ginkgo.DeferCleanup(func() {
		Expect(index.Close()).To(Succeed())
	})

	return treeSvc, index, NewSearchIndexSideEffect(index, treeSvc, nil)
}

func createPageWithContent(treeSvc *tree.TreeService, title, slug, content string) *tree.Page {
	ginkgo.GinkgoHelper()
	return createChildPageWithContent(treeSvc, nil, title, slug, content)
}

func createChildPageWithContent(treeSvc *tree.TreeService, parentID *tree.PageID, title, slug, content string) *tree.Page {
	ginkgo.GinkgoHelper()

	kind := tree.NodeKindPage
	id, err := treeSvc.CreateNode(newFixtureUserID("system"), parentID, title, tree.SlugFromString(slug), &kind)
	Expect(err).NotTo(HaveOccurred())
	page, err := treeSvc.GetPage(*id)
	Expect(err).NotTo(HaveOccurred())
	Expect(treeSvc.UpdateNode(newFixtureUserID("system"), *id, title, tree.SlugFromString(slug), &content, tree.PageVersionFromString(page.Version()), false)).To(Succeed())
	page, err = treeSvc.GetPage(*id)
	Expect(err).NotTo(HaveOccurred())
	return page
}

func searchForTerm(index *search.SQLiteIndex, term string) *search.SearchResult {
	ginkgo.GinkgoHelper()

	result, err := index.Search(term, nil, 0, 10)
	Expect(err).NotTo(HaveOccurred())
	return result
}

func haveSearchHitForPage(pageID tree.PageID) types.GomegaMatcher {
	return gstruct.PointTo(gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
		"Count": BeNumerically(">=", 1),
		"Items": ContainElement(HaveField("PageID", Equal(pageID))),
	}))
}

func haveSearchHitForPageAtPath(pageID tree.PageID, path string) types.GomegaMatcher {
	return gstruct.PointTo(gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
		"Count": BeNumerically(">=", 1),
		"Items": ContainElement(SatisfyAll(
			HaveField("PageID", Equal(pageID)),
			HaveField("Path", Equal(path)),
		)),
	}))
}

func haveNoSearchHits() types.GomegaMatcher {
	return gstruct.PointTo(gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
		"Count": BeZero(),
		"Items": BeEmpty(),
	}))
}

var _ = ginkgo.Describe("search indexing side effect", func() {
	ginkgo.When("search bootstrap runs", func() {
		ginkgo.It("indexes existing pages in the tree", func() {
			treeSvc, index, effect := setupSearchTest()
			page := createPageWithContent(treeSvc, "Search Test Page", "search-test", "# Search Test Page\nThis is some uniquecontent for indexing.")

			Expect(effect.IndexAllPages()).To(Succeed())

			Expect(searchForTerm(index, "uniquecontent")).To(haveSearchHitForPage(page.ID))
		})

		ginkgo.It("clears stale index entries that are absent from the tree", func() {
			_, index, effect := setupSearchTest()
			Expect(index.IndexPage("stale/path", "stale.md", newFixturePageID("stale-id"), "Stale Page", tree.NodeKindPage, "stale content ghostpage")).To(Succeed())

			Expect(effect.IndexAllPages()).To(Succeed())

			Expect(searchForTerm(index, "ghostpage")).To(haveNoSearchHits())
		})

		ginkgo.It("succeeds without adding hits for an empty tree", func() {
			_, index, effect := setupSearchTest()

			Expect(effect.IndexAllPages()).To(Succeed())

			Expect(searchForTerm(index, "anything")).To(haveNoSearchHits())
		})
	})

	ginkgo.When("a page is created", func() {
		ginkgo.It("indexes the saved page content", func() {
			treeSvc, index, effect := setupSearchTest()
			page := createPageWithContent(treeSvc, "Created Page", "created", "some uniqueterm_create content")

			effect.Apply(PageSaveEvent{
				Operation: PageOperationCreate,
				After:     page,
			})

			Expect(searchForTerm(index, "uniqueterm_create")).To(haveSearchHitForPage(page.ID))
		})
	})

	ginkgo.When("a page is updated after bootstrap", func() {
		ginkgo.It("replaces stale indexed terms with the updated content", func() {
			treeSvc, index, effect := setupSearchTest()
			page := createPageWithContent(treeSvc, "My Page", "my-page", "initial uniqueword_before content")
			Expect(effect.IndexAllPages()).To(Succeed())
			Expect(searchForTerm(index, "uniqueword_before")).To(haveSearchHitForPage(page.ID))

			newContent := "updated uniqueword_after content"
			Expect(treeSvc.UpdateNode(newFixtureUserID("system"), tree.PageIDFromString(page.ID), page.Title, tree.SlugFromString(page.Slug), &newContent, tree.PageVersionFromString(page.Version()), false)).To(Succeed())
			updated, err := treeSvc.GetPage(tree.PageIDFromString(page.ID))
			Expect(err).NotTo(HaveOccurred())

			effect.Apply(PageSaveEvent{
				Operation: PageOperationUpdate,
				After:     updated,
			})

			Expect(searchForTerm(index, "uniqueword_before")).To(haveNoSearchHits())
			Expect(searchForTerm(index, "uniqueword_after")).To(haveSearchHitForPage(updated.ID))
		})
	})

	ginkgo.When("a page is deleted", func() {
		ginkgo.It("removes the page from the search index", func() {
			treeSvc, index, effect := setupSearchTest()
			page := createPageWithContent(treeSvc, "Delete Me", "delete-me", "deletable uniqueterm_delete content")
			Expect(effect.IndexAllPages()).To(Succeed())
			Expect(searchForTerm(index, "uniqueterm_delete")).To(haveSearchHitForPage(page.ID))

			effect.Apply(PageSaveEvent{
				Operation:     PageOperationDelete,
				AffectedPages: []*tree.Page{page},
			})

			Expect(searchForTerm(index, "uniqueterm_delete")).To(haveNoSearchHits())
		})

		ginkgo.It("removes every affected page in a recursive delete", func() {
			treeSvc, index, effect := setupSearchTest()
			parent := createPageWithContent(treeSvc, "Parent Section", "parent", "parent uniqueterm_parent content")

			parentID := tree.PageIDFromString(parent.ID)
			child1 := createChildPageWithContent(treeSvc, &parentID, "Child One", "child-one", "child one uniqueterm_child1 content")
			child2 := createChildPageWithContent(treeSvc, &parentID, "Child Two", "child-two", "child two uniqueterm_child2 content")
			Expect(effect.IndexAllPages()).To(Succeed())

			Expect(searchForTerm(index, "uniqueterm_parent")).To(haveSearchHitForPage(parent.ID))
			Expect(searchForTerm(index, "uniqueterm_child1")).To(haveSearchHitForPage(child1.ID))
			Expect(searchForTerm(index, "uniqueterm_child2")).To(haveSearchHitForPage(child2.ID))

			effect.Apply(PageSaveEvent{
				Operation:     PageOperationDelete,
				AffectedPages: []*tree.Page{child1, child2, parent},
			})

			Expect(searchForTerm(index, "uniqueterm_parent")).To(haveNoSearchHits())
			Expect(searchForTerm(index, "uniqueterm_child1")).To(haveNoSearchHits())
			Expect(searchForTerm(index, "uniqueterm_child2")).To(haveNoSearchHits())
		})
	})

	ginkgo.When("a page moves under another section", func() {
		ginkgo.It("keeps the page searchable at its new path", func() {
			treeSvc, index, effect := setupSearchTest()
			kind := tree.NodeKindPage
			parentID, err := treeSvc.CreateNode(newFixtureUserID("system"), nil, "Target Section", newFixtureSlug("target"), &kind)
			Expect(err).NotTo(HaveOccurred())
			page := createPageWithContent(treeSvc, "Movable Page", "movable", "movable uniqueterm_move content")
			Expect(effect.IndexAllPages()).To(Succeed())

			Expect(treeSvc.MoveNode(newFixtureUserID("system"), tree.PageIDFromString(page.ID), *parentID, tree.PageVersionFromString(page.Version()))).To(Succeed())
			moved, err := treeSvc.GetPage(tree.PageIDFromString(page.ID))
			Expect(err).NotTo(HaveOccurred())

			effect.Apply(PageSaveEvent{
				Operation:     PageOperationMove,
				AffectedPages: []*tree.Page{moved},
			})

			Expect(searchForTerm(index, "uniqueterm_move")).To(haveSearchHitForPageAtPath(page.ID, "target/movable"))
		})
	})
})
