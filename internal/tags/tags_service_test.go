package tags

import (
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/perber/wiki/internal/core/tree"
)

var _ = ginkgo.Describe("tag extraction from page frontmatter", ginkgo.Label("unit"), func() {
	ginkgo.It("reads block-list tag syntax", func() {
		content := "---\ntags:\n  - react\n  - typescript\n---\n\n# Page"

		got := ExtractTagsFromContent(content)

		Expect(got).To(matchTagSet("react", "typescript"))
	})

	ginkgo.It("reads inline-list tag syntax", func() {
		content := "---\ntags: [react, typescript]\n---\n\n# Page"

		got := ExtractTagsFromContent(content)

		Expect(got).To(matchTagSet("react", "typescript"))
	})

	ginkgo.It("normalizes extracted tags to lowercase", func() {
		content := "---\ntags:\n  - React\n  - TypeScript\n  - GO\n---\n"

		got := ExtractTagsFromContent(content)

		Expect(got).To(matchTagSet("react", "typescript", "go"))
	})

	ginkgo.It("deduplicates tags case-insensitively", func() {
		content := "---\ntags:\n  - react\n  - React\n  - REACT\n---\n"

		got := ExtractTagsFromContent(content)

		Expect(got).To(Equal([]string{"react"}))
	})

	ginkgo.It("trims surrounding whitespace from tag values", func() {
		content := "---\ntags:\n  - \" react \"\n  - \" go \"\n---\n"

		got := ExtractTagsFromContent(content)

		Expect(got).To(matchTagSet("react", "go"))
	})

	ginkgo.It("returns nil when frontmatter is absent", func() {
		content := "# Page\n\nJust content, no frontmatter."

		got := ExtractTagsFromContent(content)

		Expect(got).To(BeNil())
	})

	ginkgo.It("returns nil for empty content", func() {
		Expect(ExtractTagsFromContent("")).To(BeNil())
	})

	ginkgo.It("returns nil when frontmatter has no tags field", func() {
		content := "---\ntitle: My Page\nauthor: Alice\n---\n\n# Content"

		got := ExtractTagsFromContent(content)

		Expect(got).To(BeNil())
	})

	ginkgo.It("returns empty tags for an explicit empty tag list", func() {
		content := "---\ntags: []\n---\n\n# Content"

		got := ExtractTagsFromContent(content)

		Expect(got).To(BeEmpty())
	})

	ginkgo.It("skips empty tag entries", func() {
		content := "---\ntags:\n  - react\n  - \"\"\n  - typescript\n---\n"

		got := ExtractTagsFromContent(content)

		Expect(got).To(matchTagSet("react", "typescript"))
	})

	ginkgo.It("matches the tags key case-insensitively", func() {
		content := "---\nTags:\n  - react\n---\n"

		got := ExtractTagsFromContent(content)

		Expect(got).To(Equal([]string{"react"}))
	})
})

var _ = ginkgo.Describe("TagsService tree indexing", ginkgo.Label("unit"), func() {
	ginkgo.It("builds a queryable tag index for pages with tags", func() {
		svc, ts := setupTagsService()

		id1 := createPageWithTags(ts, "Page React", "react-page", []string{"react", "typescript"})
		id2 := createPageWithTags(ts, "Page Go", "go-page", []string{"go"})

		indexAllPages(svc, ts)

		pageIDs, err := svc.GetPageIDsByTags([]string{"react"})
		Expect(err).NotTo(HaveOccurred())
		Expect(pageIDs).To(matchPageIDSet(id1))

		pageIDs, err = svc.GetPageIDsByTags([]string{"go"})
		Expect(err).NotTo(HaveOccurred())
		Expect(pageIDs).To(matchPageIDSet(id2))
	})

	ginkgo.It("rebuilds the tag index idempotently", func() {
		svc, ts := setupTagsService()
		createPageWithTags(ts, "Page A", "page-a", []string{"go"})

		for i := 0; i < 3; i++ {
			Expect(svc.ClearIndex()).To(Succeed())
			indexAllPages(svc, ts)
		}

		allTags, err := svc.GetAllTags("", 50)
		Expect(err).NotTo(HaveOccurred())
		Expect(allTags).To(Equal([]TagCount{{Tag: "go", Count: 1}}))
	})

	ginkgo.It("skips pages that do not contain frontmatter tags", func() {
		svc, ts := setupTagsService()

		idPtr, err := ts.CreateNode("system", nil, "No Tags Page", newFixtureSlug("no-tags"), pageKind())
		Expect(err).NotTo(HaveOccurred())
		content := "# No Tags Page\n\nNo frontmatter."
		Expect(ts.UpdateNodeUncheckedVersion(newFixtureUserID("system"), *idPtr, "No Tags Page", newFixtureSlug("no-tags"), &content, false)).To(Succeed())

		indexAllPages(svc, ts)

		allTags, err := svc.GetAllTags("", 50)
		Expect(err).NotTo(HaveOccurred())
		Expect(allTags).To(BeEmpty())
	})

	ginkgo.It("normalizes indexed page tags to lowercase", func() {
		svc, ts := setupTagsService()
		createPageWithTags(ts, "Mixed Case", "mixed", []string{"React", "TypeScript"})

		indexAllPages(svc, ts)

		allTags, err := svc.GetAllTags("", 50)
		Expect(err).NotTo(HaveOccurred())
		Expect(allTags).To(matchTagCounts(map[string]int{
			"react":      1,
			"typescript": 1,
		}))
	})

	ginkgo.It("indexes tags from raw frontmatter even when parsed content omits metadata", func() {
		svc, ts := setupTagsService()
		pageID := createPageWithTags(ts, "Tagged Page", "tagged-page", []string{"react"})

		page, err := ts.GetPage(pageID)
		Expect(err).NotTo(HaveOccurred())
		Expect(ExtractTagsFromContent(page.Content)).To(BeNil())

		indexAllPages(svc, ts)

		pageIDs, err := svc.GetPageIDsByTags([]string{"react"})
		Expect(err).NotTo(HaveOccurred())
		Expect(pageIDs).To(matchPageIDSet(pageID))
	})

	ginkgo.It("deletes page tags through the service", func() {
		svc, _ := setupTagsService()

		Expect(svc.SetTagsForPage(newFixturePageID("page-x"), []string{"go", "test"})).To(Succeed())
		Expect(svc.DeleteTagsForPage(newFixturePageID("page-x"))).To(Succeed())

		allTags, err := svc.GetAllTags("", 50)
		Expect(err).NotTo(HaveOccurred())
		Expect(allTags).To(BeEmpty())
	})

	ginkgo.It("returns tags for requested pages", func() {
		svc, _ := setupTagsService()

		Expect(svc.SetTagsForPage(newFixturePageID("p1"), []string{"go", "testing"})).To(Succeed())
		Expect(svc.SetTagsForPage(newFixturePageID("p2"), []string{"typescript"})).To(Succeed())

		got, err := svc.GetTagsForPages(testPageIDs("p1", "p2"))
		Expect(err).NotTo(HaveOccurred())
		Expect(got).To(SatisfyAll(
			HaveKeyWithValue(newFixturePageID("p1"), matchTagSet("go", "testing")),
			HaveKeyWithValue(newFixturePageID("p2"), matchTagSet("typescript")),
		))
	})

	ginkgo.It("stores excerpts while indexing pages from the tree", func() {
		svc, ts := setupTagsService()
		pageID := createPageWithTags(ts, "Excerpt Page", "excerpt-page", []string{"go"})

		indexAllPages(svc, ts)

		excerpts, err := svc.GetExcerptsForPages(testPageIDs(pageID))
		Expect(err).NotTo(HaveOccurred())
		Expect(excerpts).To(HaveKeyWithValue(pageID, Not(BeEmpty())))
	})
})

var _ = ginkgo.Describe("TagsService page content indexing", ginkgo.Label("unit"), func() {
	ginkgo.It("stores extracted tags and excerpt together", func() {
		svc, _ := setupTagsService()

		raw := "---\ntags:\n  - go\n  - testing\n---\n\nThis is the page body."
		Expect(svc.IndexPageContent(newFixturePageID("page-1"), raw)).To(Succeed())

		tags, err := svc.GetAllTags("", 50)
		Expect(err).NotTo(HaveOccurred())
		Expect(tags).To(matchTagCounts(map[string]int{
			"go":      1,
			"testing": 1,
		}))

		excerpts, err := svc.GetExcerptsForPages(testPageIDs("page-1"))
		Expect(err).NotTo(HaveOccurred())
		Expect(excerpts).To(HaveKeyWithValue(newFixturePageID("page-1"), "This is the page body."))
	})

	ginkgo.It("stores an excerpt without tags when frontmatter is absent", func() {
		svc, _ := setupTagsService()

		raw := "# Just a page\n\nSome content without frontmatter."
		Expect(svc.IndexPageContent(newFixturePageID("page-1"), raw)).To(Succeed())

		tags, err := svc.GetAllTags("", 50)
		Expect(err).NotTo(HaveOccurred())
		Expect(tags).To(BeEmpty())

		excerpts, err := svc.GetExcerptsForPages(testPageIDs("page-1"))
		Expect(err).NotTo(HaveOccurred())
		Expect(excerpts).To(HaveKeyWithValue(newFixturePageID("page-1"), Not(BeEmpty())))
	})

	ginkgo.It("replaces existing tags and excerpt for a page", func() {
		svc, _ := setupTagsService()

		Expect(svc.IndexPageContent(newFixturePageID("page-1"), "---\ntags:\n  - go\n---\n\nFirst version.")).To(Succeed())
		Expect(svc.IndexPageContent(newFixturePageID("page-1"), "---\ntags:\n  - rust\n---\n\nSecond version.")).To(Succeed())

		tags, err := svc.GetPageIDsByTags([]string{"rust"})
		Expect(err).NotTo(HaveOccurred())
		Expect(tags).To(matchFixturePageIDSet("page-1"))

		oldTags, err := svc.GetPageIDsByTags([]string{"go"})
		Expect(err).NotTo(HaveOccurred())
		Expect(oldTags).To(BeEmpty())
	})
})

func setupTagsService() (*TagsService, *tree.TreeService) {
	ginkgo.GinkgoHelper()

	dir := tempTagsDir()
	ts := tree.NewTreeService(dir)
	Expect(ts.LoadTree()).To(Succeed())

	store, err := NewTagsStore(dir)
	Expect(err).NotTo(HaveOccurred())
	closeTagsStoreForTest(store)

	return NewTagsService(store), ts
}

func pageKind() *tree.NodeKind {
	k := tree.NodeKindPage
	return &k
}

func createPageWithTags(ts *tree.TreeService, title, slug string, tags []string) tree.PageID {
	ginkgo.GinkgoHelper()

	idPtr, err := ts.CreateNode("system", nil, title, tree.SlugFromString(slug), pageKind())
	Expect(err).NotTo(HaveOccurred())

	frontmatter := "---\ntags:\n"
	for _, tag := range tags {
		frontmatter += "  - " + tag + "\n"
	}
	frontmatter += "---\n\n# " + title

	Expect(ts.UpdateNodeUncheckedVersion(newFixtureUserID("system"), *idPtr, title, tree.SlugFromString(slug), &frontmatter, true)).To(Succeed())

	return *idPtr
}

func indexAllPages(svc *TagsService, ts *tree.TreeService) {
	ginkgo.GinkgoHelper()

	var ids []tree.PageID
	Expect(ts.WalkNodes(func(id tree.PageID) error {
		ids = append(ids, id)
		return nil
	})).To(Succeed())

	pages, errs := ts.GetPages(ids)
	Expect(errs).To(HaveEach(BeNil()))
	for _, page := range pages {
		Expect(svc.IndexPageContent(page.ID, page.RawContent)).To(Succeed())
	}
}
