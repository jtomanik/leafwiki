package pagesave

import (
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/perber/wiki/internal/core/tree"
	"github.com/perber/wiki/internal/properties"
)

func setupPropertiesEffectTest() (*tree.TreeService, *properties.PropertiesService, *PropertiesSideEffect) {
	ginkgo.GinkgoHelper()

	dir := tempPagesaveDir()
	treeSvc := tree.NewTreeService(dir)
	Expect(treeSvc.LoadTree()).To(Succeed())

	store, err := properties.NewPropertiesStore(dir)
	Expect(err).NotTo(HaveOccurred())
	ginkgo.DeferCleanup(func() {
		Expect(store.Close()).To(Succeed())
	})

	svc := properties.NewPropertiesService(store)
	return treeSvc, svc, NewPropertiesSideEffect(svc, nil)
}

var _ = ginkgo.Describe("property indexing side effect", func() {
	ginkgo.When("a page is created with property frontmatter", func() {
		ginkgo.It("indexes the page under each declared property", func() {
			treeSvc, propsSvc, effect := setupPropertiesEffectTest()
			raw := "---\nstatus: draft\nauthor: alice\n---\n\nPage body."
			page := createPageWithFrontmatter(treeSvc, "Props Page", "props-page", raw)

			effect.Apply(PageSaveEvent{
				Operation: PageOperationCreate,
				After:     page,
			})

			ids, err := propsSvc.GetPageIDsByProperty("status", "draft")
			Expect(err).NotTo(HaveOccurred())
			Expect(ids).To(ConsistOf(page.ID))

			ids, err = propsSvc.GetPageIDsByProperty("author", "alice")
			Expect(err).NotTo(HaveOccurred())
			Expect(ids).To(ConsistOf(page.ID))
		})
	})

	ginkgo.When("a page changes its property frontmatter", func() {
		ginkgo.It("removes stale property mappings and indexes the new value", func() {
			treeSvc, propsSvc, effect := setupPropertiesEffectTest()
			raw := "---\nstatus: draft\n---\n\nOriginal."
			page := createPageWithFrontmatter(treeSvc, "Update Props", "update-props", raw)
			effect.Apply(PageSaveEvent{Operation: PageOperationCreate, After: page})

			newRaw := "---\nstatus: published\n---\n\nUpdated."
			Expect(treeSvc.UpdateNodeUncheckedVersion(newFixtureUserID("system"), tree.PageIDFromString(page.ID), "Update Props", newFixtureSlug("update-props"), &newRaw, true)).To(Succeed())
			updated, err := treeSvc.GetPage(tree.PageIDFromString(page.ID))
			Expect(err).NotTo(HaveOccurred())
			effect.Apply(PageSaveEvent{Operation: PageOperationUpdate, After: updated})

			old, err := propsSvc.GetPageIDsByProperty("status", "draft")
			Expect(err).NotTo(HaveOccurred())
			Expect(old).To(BeEmpty())

			fresh, err := propsSvc.GetPageIDsByProperty("status", "published")
			Expect(err).NotTo(HaveOccurred())
			Expect(fresh).To(ConsistOf(updated.ID))
		})
	})

	ginkgo.When("a page with properties is deleted", func() {
		ginkgo.It("removes the page from property lookups", func() {
			treeSvc, propsSvc, effect := setupPropertiesEffectTest()
			raw := "---\nstatus: draft\n---\n\nBody."
			page := createPageWithFrontmatter(treeSvc, "Delete Props", "delete-props", raw)
			effect.Apply(PageSaveEvent{Operation: PageOperationCreate, After: page})

			effect.Apply(PageSaveEvent{
				Operation:     PageOperationDelete,
				AffectedPages: []*tree.Page{page},
			})

			ids, err := propsSvc.GetPageIDsByProperty("status", "draft")
			Expect(err).NotTo(HaveOccurred())
			Expect(ids).To(BeEmpty())
		})
	})
})
