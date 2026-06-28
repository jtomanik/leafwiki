package links

import (
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/perber/wiki/internal/core/tree"
)

var _ = ginkgo.Describe("RewriteMarkdownLinks", func() {
	ginkgo.It("rewrites markdown through the wrapper and returns the replacement count", func() {
		content := "[Absolute](/docs/old)\n[Relative](./old)\n[External](https://example.com/docs/old)"

		rewritten, count := RewriteMarkdownLinks(content, "/docs/source", []RewriteRule{{
			OldPath: "/docs/old",
			NewPath: "/docs/new",
		}})

		Expect(count).To(Equal(2))
		Expect(rewritten).To(ContainSubstring("[Absolute](/docs/new)"))
		Expect(rewritten).To(ContainSubstring("[Relative](./new)"))
		Expect(rewritten).To(ContainSubstring("[External](https://example.com/docs/old)"))
	})
})

var _ = ginkgo.Describe("LinkService store mutations", func() {
	ginkgo.It("deletes outgoing links for a source page", func() {
		store := newAdditionalLinksStore()
		service := NewLinkService("", nil, store)
		Expect(seedAdditionalLinks(store)).To(Succeed())

		Expect(service.DeleteOutgoingLinksForPage("source-page")).To(Succeed())

		outgoing, err := store.GetOutgoingLinksForPages(testAdditionalPageIDs("source-page", "section-source"))
		Expect(err).NotTo(HaveOccurred())
		Expect(outgoing[newFixturePageID("source-page")]).To(BeEmpty())
		Expect(outgoing[newFixturePageID("section-source")]).To(HaveLen(1))
	})

	ginkgo.It("marks exact page-kind links broken without marking same-path section links", func() {
		store := newAdditionalLinksStore()
		service := NewLinkService("", nil, store)
		Expect(seedAdditionalLinks(store)).To(Succeed())

		Expect(service.MarkLinksBrokenForPathAndKind("docs/topic", tree.NodeKindPage)).To(Succeed())

		brokenPage, err := store.GetBrokenIncomingForPathAndKind("/docs/topic", string(tree.NodeKindPage))
		Expect(err).NotTo(HaveOccurred())
		Expect(brokenPage).To(HaveLen(1))
		Expect(brokenPage[0].FromPageID).To(Equal(newFixturePageID("source-page")))

		brokenSection, err := store.GetBrokenIncomingForPathAndKind("/docs/topic", string(tree.NodeKindSection))
		Expect(err).NotTo(HaveOccurred())
		Expect(brokenSection).To(BeEmpty())
	})

	ginkgo.It("marks section prefixes broken while preserving same-path page twins", func() {
		store := newAdditionalLinksStore()
		service := NewLinkService("", nil, store)
		Expect(seedAdditionalLinks(store)).To(Succeed())

		Expect(service.MarkLinksBrokenForPrefixAndKind("docs/topic", tree.NodeKindSection)).To(Succeed())

		brokenPageRoot, err := store.GetBrokenIncomingForPathAndKind("/docs/topic", string(tree.NodeKindPage))
		Expect(err).NotTo(HaveOccurred())
		Expect(brokenPageRoot).To(BeEmpty())

		brokenSectionRoot, err := store.GetBrokenIncomingForPathAndKind("/docs/topic", string(tree.NodeKindSection))
		Expect(err).NotTo(HaveOccurred())
		Expect(brokenSectionRoot).To(HaveLen(1))
		Expect(brokenSectionRoot[0].FromPageID).To(Equal(newFixturePageID("section-source")))

		descendant, err := store.GetBrokenIncomingForPath("/docs/topic/child")
		Expect(err).NotTo(HaveOccurred())
		Expect(descendant).To(HaveLen(1))
		Expect(descendant[0].FromPageID).To(Equal(newFixturePageID("child-source")))
	})
})

var _ = ginkgo.Describe("refactor prefix queries", func() {
	ginkgo.It("returns prefix matches and source page IDs filtered by route kind", func() {
		store := newAdditionalLinksStore()
		service := NewLinkService("", nil, store)
		Expect(seedAdditionalLinks(store)).To(Succeed())

		pageMatches, err := service.GetRefactorMatchesForPrefixAndKind("/docs/topic", tree.NodeKindPage)
		Expect(err).NotTo(HaveOccurred())
		Expect(pageMatches).To(ConsistOf(RefactorLinkMatch{
			FromPageID: newFixturePageID("source-page"),
			FromTitle:  "Source Page",
			ToPath:     tree.RoutePathFromString("/docs/topic").Clean(),
			ToKind:     string(tree.NodeKindPage),
			Broken:     false,
		}))

		sectionMatches, err := service.GetRefactorMatchesForPrefixAndKind("/docs/topic", tree.NodeKindSection)
		Expect(err).NotTo(HaveOccurred())
		Expect(sectionMatches).To(ConsistOf(
			RefactorLinkMatch{
				FromPageID: newFixturePageID("section-source"),
				FromTitle:  "Section Source",
				ToPath:     tree.RoutePathFromString("/docs/topic").Clean(),
				ToKind:     string(tree.NodeKindSection),
				Broken:     false,
			},
			RefactorLinkMatch{
				FromPageID: newFixturePageID("child-source"),
				FromTitle:  "Child Source",
				ToPath:     tree.RoutePathFromString("/docs/topic/child").Clean(),
				ToKind:     string(tree.NodeKindPage),
				Broken:     false,
			},
		))

		sourceIDs, err := service.GetRefactorSourcePageIDsForPrefixAndKind("/docs/topic", tree.NodeKindSection)
		Expect(err).NotTo(HaveOccurred())
		Expect(sourceIDs).To(ConsistOf(
			newFixturePageID("section-source"),
			newFixturePageID("child-source"),
		))
	})
})

func newAdditionalLinksStore() *LinksStore {
	store, err := NewLinksStore(ginkgo.GinkgoT().TempDir())
	Expect(err).NotTo(HaveOccurred())
	ginkgo.DeferCleanup(func() {
		Expect(store.Close()).To(Succeed())
	})
	return store
}

func seedAdditionalLinks(store *LinksStore) error {
	if err := store.AddLinks("source-page", "Source Page", []TargetLink{{
		TargetPageID:   "target-page",
		TargetPagePath: "/docs/topic",
		TargetKind:     string(tree.NodeKindPage),
	}}); err != nil {
		return err
	}
	if err := store.AddLinks("section-source", "Section Source", []TargetLink{{
		TargetPageID:   "target-section",
		TargetPagePath: "/docs/topic",
		TargetKind:     string(tree.NodeKindSection),
	}}); err != nil {
		return err
	}
	return store.AddLinks("child-source", "Child Source", []TargetLink{{
		TargetPageID:   "target-child",
		TargetPagePath: "/docs/topic/child",
		TargetKind:     string(tree.NodeKindPage),
	}})
}

func testAdditionalPageIDs(ids ...string) []tree.PageID {
	out := make([]tree.PageID, 0, len(ids))
	for _, id := range ids {
		out = append(out, newFixturePageID(id))
	}
	return out
}
