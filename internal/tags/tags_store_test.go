package tags

import (
	"os"
	"path/filepath"

	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/perber/wiki/internal/core/tree"
)

var _ = ginkgo.Describe("tag database lifecycle", ginkgo.Label("unit"), func() {
	ginkgo.It("creates the tag database inside the storage directory", func() {
		tmp := tempTagsDir()

		store, err := NewTagsStore(tmp)
		Expect(err).NotTo(HaveOccurred())
		closeTagsStoreForTest(store)

		_, err = os.Stat(filepath.Join(tmp, "tags.db"))
		Expect(err).NotTo(HaveOccurred())
	})

	ginkgo.It("can initialize the same schema repeatedly", func() {
		tmp := tempTagsDir()

		for i := 0; i < 3; i++ {
			store, err := NewTagsStore(tmp)
			Expect(err).NotTo(HaveOccurred())
			Expect(store.Close()).To(Succeed())
		}
	})
})

var _ = ginkgo.Describe("page tag persistence", ginkgo.Label("unit"), func() {
	ginkgo.It("stores tags for a page", func() {
		store := newTestStore()

		Expect(store.SetTagsForPage(newFixturePageID("page-1"), []string{"go", "testing"})).To(Succeed())

		got, err := store.GetTagsForPages(testPageIDs("page-1"))
		Expect(err).NotTo(HaveOccurred())
		Expect(got).To(HaveKeyWithValue(newFixturePageID("page-1"), matchTagSet("go", "testing")))
	})

	ginkgo.It("replaces existing tags on the next write", func() {
		store := newTestStore()

		Expect(store.SetTagsForPage(newFixturePageID("page-1"), []string{"go", "testing"})).To(Succeed())
		Expect(store.SetTagsForPage(newFixturePageID("page-1"), []string{"typescript"})).To(Succeed())

		got, err := store.GetTagsForPages(testPageIDs("page-1"))
		Expect(err).NotTo(HaveOccurred())
		Expect(got).To(HaveKeyWithValue(newFixturePageID("page-1"), matchTagSet("typescript")))
	})

	ginkgo.It("clears existing tags when an empty tag list is written", func() {
		store := newTestStore()

		Expect(store.SetTagsForPage(newFixturePageID("page-1"), []string{"go", "testing"})).To(Succeed())
		Expect(store.SetTagsForPage(newFixturePageID("page-1"), []string{})).To(Succeed())

		got, err := store.GetTagsForPages(testPageIDs("page-1"))
		Expect(err).NotTo(HaveOccurred())
		Expect(got).NotTo(HaveKey(newFixturePageID("page-1")))
	})

	ginkgo.It("clears existing tags when nil tags are written", func() {
		store := newTestStore()

		Expect(store.SetTagsForPage(newFixturePageID("page-1"), []string{"go"})).To(Succeed())
		Expect(store.SetTagsForPage(newFixturePageID("page-1"), nil)).To(Succeed())

		got, err := store.GetTagsForPages(testPageIDs("page-1"))
		Expect(err).NotTo(HaveOccurred())
		Expect(got).NotTo(HaveKey(newFixturePageID("page-1")))
	})
})

var _ = ginkgo.Describe("page tag removal", ginkgo.Label("unit"), func() {
	ginkgo.It("removes tags for the requested page", func() {
		store := newTestStore()

		Expect(store.SetTagsForPage(newFixturePageID("page-1"), []string{"go", "testing"})).To(Succeed())
		Expect(store.DeleteTagsForPage(newFixturePageID("page-1"))).To(Succeed())

		got, err := store.GetTagsForPages(testPageIDs("page-1"))
		Expect(err).NotTo(HaveOccurred())
		Expect(got).NotTo(HaveKey(newFixturePageID("page-1")))
	})

	ginkgo.It("treats deleting an unknown page as a no-op", func() {
		store := newTestStore()

		Expect(store.DeleteTagsForPage(newFixturePageID("does-not-exist"))).To(Succeed())
	})

	ginkgo.It("does not affect other pages when deleting one page", func() {
		store := newTestStore()

		Expect(store.SetTagsForPage(newFixturePageID("page-1"), []string{"go"})).To(Succeed())
		Expect(store.SetTagsForPage(newFixturePageID("page-2"), []string{"typescript"})).To(Succeed())
		Expect(store.DeleteTagsForPage(newFixturePageID("page-1"))).To(Succeed())

		got, err := store.GetTagsForPages(testPageIDs("page-2"))
		Expect(err).NotTo(HaveOccurred())
		Expect(got).To(HaveKeyWithValue(newFixturePageID("page-2"), matchTagSet("typescript")))
	})
})

var _ = ginkgo.Describe("tag catalog listing", ginkgo.Label("unit"), func() {
	ginkgo.It("returns an empty list when no tags are stored", func() {
		store := newTestStore()

		tags, err := store.GetAllTags("", 50)

		Expect(err).NotTo(HaveOccurred())
		Expect(tags).To(BeEmpty())
	})

	ginkgo.It("returns tag counts across pages", func() {
		store := newTestStore()

		Expect(store.SetTagsForPage(newFixturePageID("page-1"), []string{"go", "testing"})).To(Succeed())
		Expect(store.SetTagsForPage(newFixturePageID("page-2"), []string{"go", "typescript"})).To(Succeed())
		Expect(store.SetTagsForPage(newFixturePageID("page-3"), []string{"typescript"})).To(Succeed())

		tags, err := store.GetAllTags("", 50)

		Expect(err).NotTo(HaveOccurred())
		Expect(tags).To(matchTagCounts(map[string]int{
			"go":         2,
			"testing":    1,
			"typescript": 2,
		}))
	})

	ginkgo.It("orders tags by descending count and then alphabetically", func() {
		store := newTestStore()

		Expect(store.SetTagsForPage(newFixturePageID("page-1"), []string{"alpha", "beta", "gamma"})).To(Succeed())
		Expect(store.SetTagsForPage(newFixturePageID("page-2"), []string{"alpha", "beta"})).To(Succeed())
		Expect(store.SetTagsForPage(newFixturePageID("page-3"), []string{"alpha"})).To(Succeed())

		tags, err := store.GetAllTags("", 50)

		Expect(err).NotTo(HaveOccurred())
		Expect(tags).To(Equal([]TagCount{
			{Tag: "alpha", Count: 3},
			{Tag: "beta", Count: 2},
			{Tag: "gamma", Count: 1},
		}))
	})

	ginkgo.It("orders equally common tags alphabetically", func() {
		store := newTestStore()

		Expect(store.SetTagsForPage(newFixturePageID("page-1"), []string{"zebra", "apple"})).To(Succeed())

		tags, err := store.GetAllTags("", 50)

		Expect(err).NotTo(HaveOccurred())
		Expect(tags).To(Equal([]TagCount{
			{Tag: "apple", Count: 1},
			{Tag: "zebra", Count: 1},
		}))
	})

	ginkgo.It("filters tags by prefix", func() {
		store := newTestStore()

		Expect(store.SetTagsForPage(newFixturePageID("page-1"), []string{"react", "redux", "rails", "node"})).To(Succeed())

		tags, err := store.GetAllTags("re", 50)

		Expect(err).NotTo(HaveOccurred())
		Expect(tags).To(Equal([]TagCount{
			{Tag: "react", Count: 1},
			{Tag: "redux", Count: 1},
		}))
	})

	ginkgo.It("returns all tags when the filter is empty", func() {
		store := newTestStore()

		Expect(store.SetTagsForPage(newFixturePageID("page-1"), []string{"alpha", "beta", "gamma"})).To(Succeed())

		tags, err := store.GetAllTags("", 50)

		Expect(err).NotTo(HaveOccurred())
		Expect(tags).To(HaveLen(3))
	})

	ginkgo.It("limits the number of returned tags", func() {
		store := newTestStore()

		Expect(store.SetTagsForPage(newFixturePageID("page-1"), []string{"a", "b", "c", "d", "e"})).To(Succeed())

		tags, err := store.GetAllTags("", 3)

		Expect(err).NotTo(HaveOccurred())
		Expect(tags).To(HaveLen(3))
	})

	ginkgo.It("returns all tags when the limit is zero", func() {
		store := newTestStore()

		Expect(store.SetTagsForPage(newFixturePageID("page-1"), []string{"a", "b", "c", "d", "e"})).To(Succeed())

		tags, err := store.GetAllTags("", 0)

		Expect(err).NotTo(HaveOccurred())
		Expect(tags).To(HaveLen(5))
	})

	ginkgo.It("treats LIKE wildcard characters in filters as literals", func() {
		store := newTestStore()

		Expect(store.SetTagsForPage(newFixturePageID("page-1"), []string{"react", "redux"})).To(Succeed())

		tags, err := store.GetAllTags("%", 50)
		Expect(err).NotTo(HaveOccurred())
		Expect(tags).To(BeEmpty())

		tags, err = store.GetAllTags("_eact", 50)
		Expect(err).NotTo(HaveOccurred())
		Expect(tags).To(BeEmpty())
	})
})

var _ = ginkgo.Describe("tag-filtered page lookup", ginkgo.Label("unit"), func() {
	ginkgo.It("returns pages that have all requested tags", func() {
		store := newTestStore()

		Expect(store.SetTagsForPage(newFixturePageID("page-1"), []string{"react", "typescript"})).To(Succeed())
		Expect(store.SetTagsForPage(newFixturePageID("page-2"), []string{"react"})).To(Succeed())
		Expect(store.SetTagsForPage(newFixturePageID("page-3"), []string{"typescript"})).To(Succeed())
		Expect(store.SetTagsForPage(newFixturePageID("page-4"), []string{"vue", "typescript"})).To(Succeed())

		ids, err := store.GetPageIDsByTags([]string{"react", "typescript"})

		Expect(err).NotTo(HaveOccurred())
		Expect(ids).To(matchFixturePageIDSet("page-1"))
	})

	ginkgo.It("returns all pages that have a single requested tag", func() {
		store := newTestStore()

		Expect(store.SetTagsForPage(newFixturePageID("page-1"), []string{"react", "typescript"})).To(Succeed())
		Expect(store.SetTagsForPage(newFixturePageID("page-2"), []string{"react"})).To(Succeed())
		Expect(store.SetTagsForPage(newFixturePageID("page-3"), []string{"vue"})).To(Succeed())

		ids, err := store.GetPageIDsByTags([]string{"react"})

		Expect(err).NotTo(HaveOccurred())
		Expect(sortedPageIDs(ids)).To(matchPageIDsInOrder("page-1", "page-2"))
	})

	ginkgo.It("returns no page IDs when no pages match the requested tag", func() {
		store := newTestStore()

		Expect(store.SetTagsForPage(newFixturePageID("page-1"), []string{"react"})).To(Succeed())

		ids, err := store.GetPageIDsByTags([]string{"vue"})

		Expect(err).NotTo(HaveOccurred())
		Expect(ids).To(BeEmpty())
	})

	ginkgo.It("returns nil page IDs when no tags are requested", func() {
		store := newTestStore()

		Expect(store.SetTagsForPage(newFixturePageID("page-1"), []string{"react"})).To(Succeed())

		ids, err := store.GetPageIDsByTags([]string{})

		Expect(err).NotTo(HaveOccurred())
		Expect(ids).To(BeNil())
	})

	ginkgo.It("requires every requested tag when filtering by three tags", func() {
		store := newTestStore()

		Expect(store.SetTagsForPage(newFixturePageID("page-1"), []string{"a", "b", "c"})).To(Succeed())
		Expect(store.SetTagsForPage(newFixturePageID("page-2"), []string{"a", "b"})).To(Succeed())
		Expect(store.SetTagsForPage(newFixturePageID("page-3"), []string{"a"})).To(Succeed())

		ids, err := store.GetPageIDsByTags([]string{"a", "b", "c"})

		Expect(err).NotTo(HaveOccurred())
		Expect(ids).To(matchFixturePageIDSet("page-1"))
	})
})

var _ = ginkgo.Describe("page-to-tag lookup", ginkgo.Label("unit"), func() {
	ginkgo.It("returns tags for the requested pages", func() {
		store := newTestStore()

		Expect(store.SetTagsForPage(newFixturePageID("page-1"), []string{"go", "testing"})).To(Succeed())
		Expect(store.SetTagsForPage(newFixturePageID("page-2"), []string{"typescript"})).To(Succeed())
		Expect(store.SetTagsForPage(newFixturePageID("page-3"), []string{"react", "vue"})).To(Succeed())

		got, err := store.GetTagsForPages(testPageIDs("page-1", "page-3"))

		Expect(err).NotTo(HaveOccurred())
		Expect(got).To(SatisfyAll(
			HaveKeyWithValue(newFixturePageID("page-1"), matchTagSet("go", "testing")),
			HaveKeyWithValue(newFixturePageID("page-3"), matchTagSet("react", "vue")),
			Not(HaveKey(newFixturePageID("page-2"))),
		))
	})

	ginkgo.It("returns an empty map when no page IDs are requested", func() {
		store := newTestStore()

		got, err := store.GetTagsForPages(testPageIDs[string]())

		Expect(err).NotTo(HaveOccurred())
		Expect(got).To(BeEmpty())
	})

	ginkgo.It("returns no entry for unknown page IDs", func() {
		store := newTestStore()

		got, err := store.GetTagsForPages(testPageIDs("does-not-exist"))

		Expect(err).NotTo(HaveOccurred())
		Expect(got).To(BeEmpty())
	})
})

var _ = ginkgo.Describe("tag index clearing", ginkgo.Label("unit"), func() {
	ginkgo.It("removes all tag entries", func() {
		store := newTestStore()

		Expect(store.SetTagsForPage(newFixturePageID("page-1"), []string{"go"})).To(Succeed())
		Expect(store.SetTagsForPage(newFixturePageID("page-2"), []string{"typescript"})).To(Succeed())
		Expect(store.Clear()).To(Succeed())

		tags, err := store.GetAllTags("", 50)
		Expect(err).NotTo(HaveOccurred())
		Expect(tags).To(BeEmpty())
	})

	ginkgo.It("removes page excerpt metadata as well as tags", func() {
		store := newTestStore()

		Expect(store.SetPageIndex(newFixturePageID("page-1"), []string{"go"}, "some excerpt")).To(Succeed())
		Expect(store.Clear()).To(Succeed())

		excerpts, err := store.GetExcerptsForPages(testPageIDs("page-1"))
		Expect(err).NotTo(HaveOccurred())
		Expect(excerpts).NotTo(HaveKey(newFixturePageID("page-1")))
	})
})

var _ = ginkgo.Describe("page index persistence", ginkgo.Label("unit"), func() {
	ginkgo.It("stores tags and excerpts together", func() {
		store := newTestStore()

		Expect(store.SetPageIndex(newFixturePageID("page-1"), []string{"go", "testing"}, "some excerpt")).To(Succeed())

		gotTags, err := store.GetTagsForPages(testPageIDs("page-1"))
		Expect(err).NotTo(HaveOccurred())
		Expect(gotTags).To(HaveKeyWithValue(newFixturePageID("page-1"), matchTagSet("go", "testing")))

		gotExcerpts, err := store.GetExcerptsForPages(testPageIDs("page-1"))
		Expect(err).NotTo(HaveOccurred())
		Expect(gotExcerpts).To(HaveKeyWithValue(newFixturePageID("page-1"), "some excerpt"))
	})

	ginkgo.It("updates the stored excerpt on the next page index write", func() {
		store := newTestStore()

		Expect(store.SetPageIndex(newFixturePageID("page-1"), []string{"go"}, "first excerpt")).To(Succeed())
		Expect(store.SetPageIndex(newFixturePageID("page-1"), []string{"go"}, "updated excerpt")).To(Succeed())

		got, err := store.GetExcerptsForPages(testPageIDs("page-1"))
		Expect(err).NotTo(HaveOccurred())
		Expect(got).To(HaveKeyWithValue(newFixturePageID("page-1"), "updated excerpt"))
	})

	ginkgo.It("keeps the excerpt when an empty tag set clears tags", func() {
		store := newTestStore()

		Expect(store.SetPageIndex(newFixturePageID("page-1"), []string{"go"}, "excerpt here")).To(Succeed())
		Expect(store.SetPageIndex(newFixturePageID("page-1"), []string{}, "excerpt here")).To(Succeed())

		tags, err := store.GetTagsForPages(testPageIDs("page-1"))
		Expect(err).NotTo(HaveOccurred())
		Expect(tags).NotTo(HaveKey(newFixturePageID("page-1")))

		excerpts, err := store.GetExcerptsForPages(testPageIDs("page-1"))
		Expect(err).NotTo(HaveOccurred())
		Expect(excerpts).To(HaveKeyWithValue(newFixturePageID("page-1"), "excerpt here"))
	})
})

var _ = ginkgo.Describe("page index removal", ginkgo.Label("unit"), func() {
	ginkgo.It("removes both tags and excerpt metadata", func() {
		store := newTestStore()

		Expect(store.SetPageIndex(newFixturePageID("page-1"), []string{"go"}, "some excerpt")).To(Succeed())
		Expect(store.DeletePageIndex(newFixturePageID("page-1"))).To(Succeed())

		tags, err := store.GetTagsForPages(testPageIDs("page-1"))
		Expect(err).NotTo(HaveOccurred())
		Expect(tags).NotTo(HaveKey(newFixturePageID("page-1")))

		excerpts, err := store.GetExcerptsForPages(testPageIDs("page-1"))
		Expect(err).NotTo(HaveOccurred())
		Expect(excerpts).NotTo(HaveKey(newFixturePageID("page-1")))
	})

	ginkgo.It("treats deleting an unknown page index as a no-op", func() {
		store := newTestStore()

		Expect(store.DeletePageIndex(newFixturePageID("does-not-exist"))).To(Succeed())
	})
})

var _ = ginkgo.Describe("page excerpt lookup", ginkgo.Label("unit"), func() {
	ginkgo.It("returns excerpts for requested pages", func() {
		store := newTestStore()

		Expect(store.SetPageIndex(newFixturePageID("p1"), []string{"go"}, "excerpt one")).To(Succeed())
		Expect(store.SetPageIndex(newFixturePageID("p2"), []string{"ts"}, "excerpt two")).To(Succeed())

		got, err := store.GetExcerptsForPages(testPageIDs("p1", "p2"))

		Expect(err).NotTo(HaveOccurred())
		Expect(got).To(SatisfyAll(
			HaveKeyWithValue(newFixturePageID("p1"), "excerpt one"),
			HaveKeyWithValue(newFixturePageID("p2"), "excerpt two"),
		))
	})

	ginkgo.It("returns no entry for unknown page IDs", func() {
		store := newTestStore()

		got, err := store.GetExcerptsForPages(testPageIDs("ghost"))

		Expect(err).NotTo(HaveOccurred())
		Expect(got).NotTo(HaveKey(newFixturePageID("ghost")))
	})

	ginkgo.It("returns an empty map when no page IDs are requested", func() {
		store := newTestStore()

		got, err := store.GetExcerptsForPages(testPageIDs[string]())

		Expect(err).NotTo(HaveOccurred())
		Expect(got).To(BeEmpty())
	})
})

var _ = ginkgo.Describe("tag page ID fixtures", ginkgo.Label("unit"), func() {
	ginkgo.It("builds typed page IDs for shared tag queries", func() {
		Expect(testPageIDs("page-1", "page-2")).To(Equal([]tree.PageID{
			newFixturePageID("page-1"),
			newFixturePageID("page-2"),
		}))
	})
})
