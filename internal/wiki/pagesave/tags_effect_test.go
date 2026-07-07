package pagesave

import (
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/perber/wiki/internal/core/tree"
	"github.com/perber/wiki/internal/tags"
)

func setupTagsEffectTest() (*tree.TreeService, *tags.TagsService, *TagsSideEffect) {
	ginkgo.GinkgoHelper()

	dir := tempPagesaveDir()
	treeSvc := tree.NewTreeService(dir)
	Expect(treeSvc.LoadTree()).To(Succeed())

	store, err := tags.NewTagsStore(dir)
	Expect(err).NotTo(HaveOccurred())
	ginkgo.DeferCleanup(func() {
		Expect(store.Close()).To(Succeed())
	})

	svc := tags.NewTagsService(store)
	return treeSvc, svc, NewTagsSideEffect(svc, nil)
}

// createPageWithFrontmatter creates a page whose frontmatter is set via the import path,
// so custom keys (tags, properties) survive the write.
func createPageWithFrontmatter(treeSvc *tree.TreeService, title string, slug tree.Slug, raw string) *tree.Page {
	ginkgo.GinkgoHelper()

	kind := tree.NodeKindPage
	id, err := treeSvc.CreateNode(newFixtureUserID("system"), nil, title, slug, &kind)
	Expect(err).NotTo(HaveOccurred())
	Expect(treeSvc.UpdateNodeUncheckedVersion(newFixtureUserID("system"), *id, title, slug, &raw, true)).To(Succeed())
	page, err := treeSvc.GetPage(*id)
	Expect(err).NotTo(HaveOccurred())
	return page
}

var _ = ginkgo.Describe("tag indexing side effect", ginkgo.Label("integration"), func() {
	ginkgo.When("a page is created with tag frontmatter", func() {
		ginkgo.It("indexes the page under each declared tag", func() {
			treeSvc, tagsSvc, effect := setupTagsEffectTest()
			raw := "---\ntags:\n  - golang\n  - testing\n---\n\nPage body."
			page := createPageWithFrontmatter(treeSvc, "Tagged Page", newFixtureSlug("tagged"), raw)

			effect.Apply(PageSaveEvent{
				Operation: PageOperationCreate,
				After:     page,
			})

			ids, err := tagsSvc.GetPageIDsByTags([]string{"golang"})
			Expect(err).NotTo(HaveOccurred())
			Expect(ids).To(ConsistOf(page.ID))

			ids, err = tagsSvc.GetPageIDsByTags([]string{"testing"})
			Expect(err).NotTo(HaveOccurred())
			Expect(ids).To(ConsistOf(page.ID))
		})
	})

	ginkgo.When("a page changes its tag frontmatter", func() {
		ginkgo.It("removes stale tag mappings and indexes the new tags", func() {
			treeSvc, tagsSvc, effect := setupTagsEffectTest()
			raw := "---\ntags:\n  - oldtag\n---\n\nOriginal."
			page := createPageWithFrontmatter(treeSvc, "Update Tags", newFixtureSlug("update-tags"), raw)
			effect.Apply(PageSaveEvent{Operation: PageOperationCreate, After: page})

			newRaw := "---\ntags:\n  - newtag\n---\n\nUpdated."
			Expect(treeSvc.UpdateNodeUncheckedVersion(newFixtureUserID("system"), tree.PageIDFromString(page.ID), "Update Tags", newFixtureSlug("update-tags"), &newRaw, true)).To(Succeed())
			updated, err := treeSvc.GetPage(tree.PageIDFromString(page.ID))
			Expect(err).NotTo(HaveOccurred())

			effect.Apply(PageSaveEvent{Operation: PageOperationUpdate, After: updated})

			old, err := tagsSvc.GetPageIDsByTags([]string{"oldtag"})
			Expect(err).NotTo(HaveOccurred())
			Expect(old).To(BeEmpty())

			fresh, err := tagsSvc.GetPageIDsByTags([]string{"newtag"})
			Expect(err).NotTo(HaveOccurred())
			Expect(fresh).To(ConsistOf(updated.ID))
		})
	})

	ginkgo.When("a tagged page is deleted", func() {
		ginkgo.It("removes the page from tag lookups", func() {
			treeSvc, tagsSvc, effect := setupTagsEffectTest()
			raw := "---\ntags:\n  - removeme\n---\n\nBody."
			page := createPageWithFrontmatter(treeSvc, "Delete Tags", newFixtureSlug("delete-tags"), raw)
			effect.Apply(PageSaveEvent{Operation: PageOperationCreate, After: page})

			effect.Apply(PageSaveEvent{
				Operation:     PageOperationDelete,
				AffectedPages: []*tree.Page{page},
			})

			ids, err := tagsSvc.GetPageIDsByTags([]string{"removeme"})
			Expect(err).NotTo(HaveOccurred())
			Expect(ids).To(BeEmpty())
		})
	})
})
