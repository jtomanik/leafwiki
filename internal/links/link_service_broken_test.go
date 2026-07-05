package links

import (
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/perber/wiki/internal/core/tree"
)

var _ = ginkgo.Describe("broken incoming link filtering", ginkgo.Label("integration"), func() {
	ginkgo.It("returns only broken links for the requested path", func() {
		svc, ts, store := setupLinkService()

		aIDPtr, err := ts.CreateNode("system", nil, "Page A", "a", pageNodeKind())
		Expect(err).NotTo(HaveOccurred())
		pageAID := *aIDPtr

		bIDPtr, err := ts.CreateNode("system", nil, "Page B", "b", pageNodeKind())
		Expect(err).NotTo(HaveOccurred())
		pageBID := *bIDPtr

		// Page A links to "/missing1"
		pageA, err := ts.GetPage(pageAID)
		Expect(err).NotTo(HaveOccurred())
		var linkToMissing1 = "Link: [Missing1](/missing1)"
		Expect(ts.UpdateNodeUncheckedVersion(newFixtureUserID("system"), pageA.ID, pageA.Title, pageA.Slug, &linkToMissing1, false)).To(Succeed())

		// Page B links to "/missing2"
		pageB, err := ts.GetPage(pageBID)
		Expect(err).NotTo(HaveOccurred())
		var linkToMissing2 = "Link: [Missing2](/missing2)"
		Expect(ts.UpdateNodeUncheckedVersion(newFixtureUserID("system"), pageB.ID, pageB.Title, pageB.Slug, &linkToMissing2, false)).To(Succeed())

		Expect(svc.IndexAllPages()).To(Succeed())

		// Test: Should only return broken links for "/missing1"
		broken1, err := store.GetBrokenIncomingForPath("/missing1")
		Expect(err).NotTo(HaveOccurred())

		Expect(broken1).To(ConsistOf(matchBrokenBacklink(pageAID)))

		// Test: Should only return broken links for "/missing2"
		broken2, err := store.GetBrokenIncomingForPath("/missing2")
		Expect(err).NotTo(HaveOccurred())

		Expect(broken2).To(ConsistOf(matchBrokenBacklink(pageBID)))

	})
})

var _ = ginkgo.Describe("broken incoming link queries without matching links", ginkgo.Label("integration"), func() {
	ginkgo.It("returns empty results for healthy and unused paths", func() {
		svc, ts, store := setupLinkService()

		aIDPtr, err := ts.CreateNode("system", nil, "Page A", "a", pageNodeKind())
		Expect(err).NotTo(HaveOccurred())
		pageAID := *aIDPtr

		_, err = ts.CreateNode("system", nil, "Page B", "b", pageNodeKind())
		Expect(err).NotTo(HaveOccurred())

		// Page A links to existing Page B (not broken)
		pageA, err := ts.GetPage(pageAID)
		Expect(err).NotTo(HaveOccurred())
		var linkToB = "Link: [To B](/b.md)"
		Expect(ts.UpdateNodeUncheckedVersion(newFixtureUserID("system"), pageA.ID, pageA.Title, pageA.Slug, &linkToB, false)).To(Succeed())

		Expect(svc.IndexAllPages()).To(Succeed())

		// Test: Should return empty for "/b" since the link is not broken
		brokenLinks, err := store.GetBrokenIncomingForPath("/b")
		Expect(err).NotTo(HaveOccurred())

		Expect(brokenLinks).To(HaveLen(0))

		// Test: Should return empty for a path that has no links at all
		noLinks, err := store.GetBrokenIncomingForPath("/never-linked")
		Expect(err).NotTo(HaveOccurred())

		Expect(noLinks).To(HaveLen(0))

	})
})

var _ = ginkgo.Describe("broken incoming link ordering", ginkgo.Label("integration"), func() {
	ginkgo.It("orders broken incoming links by source title", func() {
		svc, ts, store := setupLinkService()

		// Create three pages with titles that should be ordered alphabetically
		zIDPtr, err := ts.CreateNode("system", nil, "Zebra Page", "z", pageNodeKind())
		Expect(err).NotTo(HaveOccurred())

		aIDPtr, err := ts.CreateNode("system", nil, "Alpha Page", "a", pageNodeKind())
		Expect(err).NotTo(HaveOccurred())

		mIDPtr, err := ts.CreateNode("system", nil, "Middle Page", "m", pageNodeKind())
		Expect(err).NotTo(HaveOccurred())

		// All three pages link to the same non-existent page
		pageIDs := []tree.PageID{*zIDPtr, *aIDPtr, *mIDPtr}
		for _, id := range pageIDs {
			page, err := ts.GetPage(id)
			Expect(err).NotTo(HaveOccurred())
			var linkToMissing = "Link: [Missing](/missing)"
			Expect(ts.UpdateNodeUncheckedVersion(newFixtureUserID("system"), page.ID, page.Title, page.Slug, &linkToMissing, false)).To(Succeed())
		}

		Expect(svc.IndexAllPages()).To(Succeed())

		// Test: Results should be ordered by from_title ASC
		brokenLinks, err := store.GetBrokenIncomingForPath("/missing")
		Expect(err).NotTo(HaveOccurred())

		Expect(brokenLinks).To(HaveExactElements(
			matchBacklinkTitle("Alpha Page"),
			matchBacklinkTitle("Middle Page"),
			matchBacklinkTitle("Zebra Page"),
		))

	})
})

var _ = ginkgo.Describe("broken incoming link healing", ginkgo.Label("integration"), func() {
	ginkgo.It("removes healed links from broken incoming queries", func() {
		svc, ts, store := setupLinkService()

		// Create Page A that links to a non-existent page
		aIDPtr, err := ts.CreateNode("system", nil, "Page A", "a", pageNodeKind())
		Expect(err).NotTo(HaveOccurred())
		pageAID := *aIDPtr

		pageA, err := ts.GetPage(pageAID)
		Expect(err).NotTo(HaveOccurred())
		var linkToB = "Link: [To B](/b.md)"
		Expect(ts.UpdateNodeUncheckedVersion(newFixtureUserID("system"), pageA.ID, pageA.Title, pageA.Slug, &linkToB, false)).To(Succeed())

		// Index - this creates a broken link since B doesn't exist
		Expect(svc.IndexAllPages()).To(Succeed())

		// Verify the broken link exists
		brokenBefore, err := store.GetBrokenIncomingForPath("/b")
		Expect(err).NotTo(HaveOccurred())
		Expect(brokenBefore).To(HaveLen(1))

		// Now create Page B - this should heal the link
		bIDPtr, err := ts.CreateNode("system", nil, "Page B", "b", pageNodeKind())
		Expect(err).NotTo(HaveOccurred())
		pageBID := *bIDPtr

		pageB, err := ts.GetPage(pageBID)
		Expect(err).NotTo(HaveOccurred())
		var contentB = "# Page B"
		Expect(ts.UpdateNodeUncheckedVersion(newFixtureUserID("system"), pageB.ID, pageB.Title, pageB.Slug, &contentB, false)).To(Succeed())

		// Use HealLinksForExactPath to heal the broken link
		Expect(svc.HealLinksForExactPath(pageB)).To(Succeed())

		// Verify the link is no longer broken
		brokenAfter, err := store.GetBrokenIncomingForPath("/b")
		Expect(err).NotTo(HaveOccurred())
		Expect(brokenAfter).To(HaveLen(0))

		// Verify the link still exists but is not broken
		backlinks, err := store.GetBacklinksForPage(pageBID)
		Expect(err).NotTo(HaveOccurred())
		Expect(backlinks).To(ConsistOf(matchBacklinkResultItem(pageAID, pageBID)))

	})
})

var _ = ginkgo.Describe("extensionless link healing", ginkgo.Label("integration"), func() {
	ginkgo.It("homes extensionless links to matching sections instead of page twins", func() {
		svc, ts, _ := setupLinkService()

		sourceIDPtr, err := ts.CreateNode("system", nil, "Source", "source", pageNodeKind())
		Expect(err).NotTo(HaveOccurred())
		sourceID := *sourceIDPtr
		source, err := ts.GetPage(sourceID)
		Expect(err).NotTo(HaveOccurred())
		sourceContent := "Link: [Target](/x)"
		Expect(ts.UpdateNodeUncheckedVersion(newFixtureUserID("system"), source.ID, source.Title, source.Slug, &sourceContent, false)).To(Succeed())
		source, err = ts.GetPage(sourceID)
		Expect(err).NotTo(HaveOccurred())
		Expect(svc.UpdateLinksForPage(source, source.Content)).To(Succeed())

		initialStatus, err := svc.GetLinkStatusForPage(source.ID, source.CalculateRoutePath())
		Expect(err).NotTo(HaveOccurred())
		Expect(initialStatus.Counts).To(matchLinkStatusCounts(0, 0, 0, 1))

		pageIDPtr, err := ts.CreateNode("system", nil, "X Page", "x", pageNodeKind())
		Expect(err).NotTo(HaveOccurred())
		pageTarget, err := ts.GetPage(*pageIDPtr)
		Expect(err).NotTo(HaveOccurred())
		Expect(svc.HealLinksForExactPath(pageTarget)).To(Succeed())
		pageBacklinks, err := svc.GetBacklinksForPage(pageTarget.ID)
		Expect(err).NotTo(HaveOccurred())
		Expect(pageBacklinks).To(matchBacklinkResult(0))
		statusAfterPageHeal, err := svc.GetLinkStatusForPage(source.ID, source.CalculateRoutePath())
		Expect(err).NotTo(HaveOccurred())
		Expect(statusAfterPageHeal.Counts).To(matchLinkStatusCounts(0, 0, 0, 1))

		sectionIDPtr, err := ts.CreateNode("system", nil, "X Section", "x", sectionNodeKind())
		Expect(err).NotTo(HaveOccurred())
		sectionTarget, err := ts.GetPage(*sectionIDPtr)
		Expect(err).NotTo(HaveOccurred())
		Expect(svc.HealLinksForExactPath(sectionTarget)).To(Succeed())

		sectionBacklinks, err := svc.GetBacklinksForPage(sectionTarget.ID)
		Expect(err).NotTo(HaveOccurred())
		Expect(sectionBacklinks).To(matchBacklinkResult(1,
			matchBacklinkResultItem(source.ID, sectionTarget.ID),
		))
		pageBacklinksAfter, err := svc.GetBacklinksForPage(pageTarget.ID)
		Expect(err).NotTo(HaveOccurred())
		Expect(pageBacklinksAfter).To(matchBacklinkResult(0))

	})
})
