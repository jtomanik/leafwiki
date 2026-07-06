package links

import (
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/gstruct"

	"github.com/perber/wiki/internal/core/tree"
)

var _ = ginkgo.Describe("RewriteMarkdownLinks", ginkgo.Label("unit"), func() {
	ginkgo.It("rewrites markdown through the wrapper and returns the replacement count", ginkgo.Label("unit"), func() {
		content := "[Absolute](/docs/old)\n[Relative](./old)\n[External](https://example.com/docs/old)"

		rewritten, count := RewriteMarkdownLinks(content, newFixtureRoutePath("/docs/source"), []RewriteRule{{
			OldPath: newFixtureRoutePath("/docs/old"),
			NewPath: newFixtureRoutePath("/docs/new"),
		}})

		Expect(count).To(Equal(2))
		Expect(rewritten).To(ContainSubstring("[Absolute](/docs/new)"))
		Expect(rewritten).To(ContainSubstring("[Relative](./new)"))
		Expect(rewritten).To(ContainSubstring("[External](https://example.com/docs/old)"))
	})
})

var _ = ginkgo.Describe("stored link index mutations", ginkgo.Label("integration"), func() {
	ginkgo.It("deletes outgoing links for a source page", ginkgo.Label("integration"), func() {
		store := newAdditionalLinksStore()
		service := NewLinkService("", nil, store)
		Expect(seedAdditionalLinks(store)).To(Succeed())

		Expect(service.DeleteOutgoingLinksForPage(newFixturePageID("source-page"))).To(Succeed())

		outgoing, err := store.GetOutgoingLinksForPages(testAdditionalPageIDs(
			newFixturePageID("source-page"),
			newFixturePageID("section-source"),
		))
		Expect(err).NotTo(HaveOccurred())
		Expect(outgoing).To(SatisfyAll(
			Not(HaveKey(newFixturePageID("source-page"))),
			HaveKeyWithValue(newFixturePageID("section-source"), HaveLen(1)),
		))
	})

	ginkgo.It("marks exact page-kind links broken without marking same-path section links", ginkgo.Label("integration"), func() {
		store := newAdditionalLinksStore()
		service := NewLinkService("", nil, store)
		Expect(seedAdditionalLinks(store)).To(Succeed())

		Expect(service.MarkLinksBrokenForPathAndKind(newFixtureRoutePath("docs/topic"), tree.NodeKindPage)).To(Succeed())

		brokenPage, err := store.GetBrokenIncomingForPathAndKind(newFixtureRoutePath("/docs/topic"), tree.NodeKindPage)
		Expect(err).NotTo(HaveOccurred())
		Expect(brokenPage).To(ConsistOf(matchBacklink(gstruct.Fields{
			"FromPageID": Equal(newFixturePageID("source-page")),
		})))

		brokenSection, err := store.GetBrokenIncomingForPathAndKind(newFixtureRoutePath("/docs/topic"), tree.NodeKindSection)
		Expect(err).NotTo(HaveOccurred())
		Expect(brokenSection).To(BeEmpty())
	})

	ginkgo.It("marks section prefixes broken while preserving same-path page twins", ginkgo.Label("integration"), func() {
		store := newAdditionalLinksStore()
		service := NewLinkService("", nil, store)
		Expect(seedAdditionalLinks(store)).To(Succeed())

		Expect(service.MarkLinksBrokenForPrefixAndKind("docs/topic", tree.NodeKindSection)).To(Succeed())

		brokenPageRoot, err := store.GetBrokenIncomingForPathAndKind(newFixtureRoutePath("/docs/topic"), tree.NodeKindPage)
		Expect(err).NotTo(HaveOccurred())
		Expect(brokenPageRoot).To(BeEmpty())

		brokenSectionRoot, err := store.GetBrokenIncomingForPathAndKind(newFixtureRoutePath("/docs/topic"), tree.NodeKindSection)
		Expect(err).NotTo(HaveOccurred())
		Expect(brokenSectionRoot).To(ConsistOf(matchBacklink(gstruct.Fields{
			"FromPageID": Equal(newFixturePageID("section-source")),
		})))

		descendant, err := store.GetBrokenIncomingForPath(newFixtureRoutePath("/docs/topic/child"))
		Expect(err).NotTo(HaveOccurred())
		Expect(descendant).To(ConsistOf(matchBacklink(gstruct.Fields{
			"FromPageID": Equal(newFixturePageID("child-source")),
		})))
	})
})

var _ = ginkgo.Describe("refactor prefix queries", ginkgo.Label("integration"), func() {
	ginkgo.It("returns prefix matches and source page IDs filtered by route kind", ginkgo.Label("integration"), func() {
		store := newAdditionalLinksStore()
		service := NewLinkService("", nil, store)
		Expect(seedAdditionalLinks(store)).To(Succeed())

		pageMatches, err := service.GetRefactorMatchesForPrefixAndKind(newFixtureRoutePath("/docs/topic"), tree.NodeKindPage)
		Expect(err).NotTo(HaveOccurred())
		Expect(pageMatches).To(ConsistOf(RefactorLinkMatch{
			FromPageID: newFixturePageID("source-page"),
			FromTitle:  "Source Page",
			ToPath:     newFixtureRoutePath("/docs/topic").Clean(),
			ToKind:     TargetKindPage,
			Broken:     false,
		}))

		sectionMatches, err := service.GetRefactorMatchesForPrefixAndKind(newFixtureRoutePath("/docs/topic"), tree.NodeKindSection)
		Expect(err).NotTo(HaveOccurred())
		Expect(sectionMatches).To(ConsistOf(
			RefactorLinkMatch{
				FromPageID: newFixturePageID("section-source"),
				FromTitle:  "Section Source",
				ToPath:     newFixtureRoutePath("/docs/topic").Clean(),
				ToKind:     TargetKindSection,
				Broken:     false,
			},
			RefactorLinkMatch{
				FromPageID: newFixturePageID("child-source"),
				FromTitle:  "Child Source",
				ToPath:     newFixtureRoutePath("/docs/topic/child").Clean(),
				ToKind:     TargetKindPage,
				Broken:     false,
			},
		))

		sourceIDs, err := service.GetRefactorSourcePageIDsForPrefixAndKind(newFixtureRoutePath("/docs/topic"), tree.NodeKindSection)
		Expect(err).NotTo(HaveOccurred())
		Expect(sourceIDs).To(ConsistOf(
			newFixturePageID("section-source"),
			newFixturePageID("child-source"),
		))
	})
})

func newAdditionalLinksStore() *LinksStore {
	ginkgo.GinkgoHelper()
	store, err := NewLinksStore(linksTempDir())
	Expect(err).NotTo(HaveOccurred())
	ginkgo.DeferCleanup(func() {
		Expect(store.Close()).To(Succeed())
	})
	return store
}

func seedAdditionalLinks(store *LinksStore) error {
	if err := store.AddLinks(newFixturePageID("source-page"), "Source Page", []TargetLink{{
		TargetPageID:   newFixturePageID("target-page"),
		TargetPagePath: "/docs/topic",
		TargetKind:     TargetKindPage,
	}}); err != nil {
		return err
	}
	if err := store.AddLinks(newFixturePageID("section-source"), "Section Source", []TargetLink{{
		TargetPageID:   newFixturePageID("target-section"),
		TargetPagePath: "/docs/topic",
		TargetKind:     TargetKindSection,
	}}); err != nil {
		return err
	}
	return store.AddLinks(newFixturePageID("child-source"), "Child Source", []TargetLink{{
		TargetPageID:   newFixturePageID("target-child"),
		TargetPagePath: "/docs/topic/child",
		TargetKind:     TargetKindPage,
	}})
}

func testAdditionalPageIDs(ids ...tree.PageID) []tree.PageID {
	return append([]tree.PageID(nil), ids...)
}
