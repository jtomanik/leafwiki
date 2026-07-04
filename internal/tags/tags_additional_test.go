package tags

import (
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = ginkgo.Describe("TagsService page index deletion", ginkgo.Label("unit"), func() {
	ginkgo.It("deletes both tags and excerpts for a page", func() {
		store := newTestStore()
		service := NewTagsService(store)

		content := "---\ntags:\n  - Go\n  - Testing\n---\n\n# Page\n\nA useful excerpt."
		Expect(service.IndexPageContent("page-1", content)).To(Succeed())

		Expect(service.DeletePageIndex("page-1")).To(Succeed())

		tagsByPage, err := service.GetTagsForPages(testPageIDs("page-1"))
		Expect(err).NotTo(HaveOccurred())
		Expect(tagsByPage).NotTo(HaveKey(newFixturePageID("page-1")))

		excerpts, err := service.GetExcerptsForPages(testPageIDs("page-1"))
		Expect(err).NotTo(HaveOccurred())
		Expect(excerpts).NotTo(HaveKey(newFixturePageID("page-1")))
	})
})

var _ = ginkgo.Describe("selection-aware tag suggestions", ginkgo.Label("unit"), func() {
	ginkgo.It("suggests additive tags for pages matching the selected tags", func() {
		store := newTestStore()
		Expect(store.SetTagsForPage("page-1", []string{"go", "react", "testing"})).To(Succeed())
		Expect(store.SetTagsForPage("page-2", []string{"go", "react"})).To(Succeed())
		Expect(store.SetTagsForPage("page-3", []string{"go", "rust"})).To(Succeed())
		Expect(store.SetTagsForPage("page-4", []string{"testing"})).To(Succeed())

		got, err := store.GetAllTagsForSelection("", []string{"go"}, 0)
		Expect(err).NotTo(HaveOccurred())
		Expect(got).To(Equal([]TagCount{
			{Tag: "react", Count: 2},
			{Tag: "rust", Count: 1},
			{Tag: "testing", Count: 1},
		}))

		filtered, err := store.GetAllTagsForSelection("re", []string{"go"}, 1)
		Expect(err).NotTo(HaveOccurred())
		Expect(filtered).To(Equal([]TagCount{{Tag: "react", Count: 2}}))
	})

	ginkgo.It("falls back to normal tag listing when no tags are selected", func() {
		store := newTestStore()
		service := NewTagsService(store)
		Expect(store.SetTagsForPage("page-1", []string{"go", "react"})).To(Succeed())
		Expect(store.SetTagsForPage("page-2", []string{"react"})).To(Succeed())

		got, err := service.GetAllTagsForSelection("r", nil, 10)
		Expect(err).NotTo(HaveOccurred())
		Expect(got).To(Equal([]TagCount{{Tag: "react", Count: 2}}))
	})
})

var _ = ginkgo.Describe("tag normalization", ginkgo.Label("unit"), func() {
	ginkgo.It("normalizes mixed interface slices and ignores non-string values", func() {
		got := normalizeTags([]interface{}{" Go ", 42, "go", "", "RUST", nil})
		Expect(got).To(Equal([]string{"go", "rust"}))
	})

	ginkgo.It("returns nil for unsupported metadata shapes", func() {
		Expect(normalizeTags("go")).To(BeNil())
		Expect(normalizeTags(map[string]string{"tag": "go"})).To(BeNil())
	})
})

var _ = ginkgo.Describe("TagsStore duplicate tag writes", ginkgo.Label("unit"), func() {
	ginkgo.It("stores duplicate input tags only once for a page", func() {
		store := newTestStore()
		Expect(store.SetTagsForPage("page-1", []string{"go", "go", "rust"})).To(Succeed())

		got, err := store.GetTagsForPages(testPageIDs("page-1"))
		Expect(err).NotTo(HaveOccurred())
		Expect(got).To(HaveKeyWithValue(newFixturePageID("page-1"), []string{"go", "rust"}))
	})
})
