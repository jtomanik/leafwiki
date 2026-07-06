package links

import (
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = ginkgo.Describe("outgoing result mapping", ginkgo.Label("integration"), func() {
	ginkgo.It("adds resolved target metadata to outgoing result items", func() {
		ts, page1ID, page2ID := setupTreeForLinksTest()

		root := ts.GetTree()
		Expect(root).NotTo(BeNil())

		outgoings := []Outgoing{{FromPageID: page1ID, ToPageID: page2ID, ToPath: newFixtureRoutePath("/docs/page2"), Broken: false, FromTitle: "Page 1"}}

		result := toOutgoingLinkResult(ts, outgoings)
		page2, err := ts.GetPage(page2ID)
		Expect(err).NotTo(HaveOccurred())
		Expect(result).To(matchOutgoingResult(1,
			SatisfyAll(
				matchResolvedOutgoingResultItem(page1ID, page2ID, newFixtureRoutePath("/docs/page2")),
				matchOutgoingResultItemTitle(page2.Title),
			),
		))

	})
})

var _ = ginkgo.Describe("late-created target indexing", ginkgo.Label("integration"), func() {
	ginkgo.It("resolves formerly broken links after reindexing", func() {
		svc, ts, _ := setupLinkService()

		aIDPtr, err := ts.CreateNode(newFixtureUserID("system"), nil, "Page A", newFixtureSlug("a"), pageNodeKind())
		Expect(err).NotTo(HaveOccurred())
		pageAID := *aIDPtr

		aPage, err := ts.GetPage(pageAID)
		Expect(err).NotTo(HaveOccurred())
		var linkToB = "Link to B: [Go](/b.md)"
		Expect(ts.UpdateNodeUncheckedVersion(newFixtureUserID("system"), aPage.ID, aPage.Title, aPage.Slug, &linkToB, false)).To(Succeed())

		Expect(svc.IndexAllPages()).To(Succeed())

		out1, err := svc.GetOutgoingLinksForPage(pageAID)
		Expect(err).NotTo(HaveOccurred())
		Expect(out1).To(matchOutgoingResult(1,
			matchBrokenOutgoingResultItem(pageAID, newFixtureRoutePath("/b")),
		))

		bIDPtr, err := ts.CreateNode(newFixtureUserID("system"), nil, "Page B", newFixtureSlug("b"), pageNodeKind())
		Expect(err).NotTo(HaveOccurred())
		pageBID := *bIDPtr

		bPage, err := ts.GetPage(pageBID)
		Expect(err).NotTo(HaveOccurred())
		var pageBContent = "# Page B"
		Expect(ts.UpdateNodeUncheckedVersion(newFixtureUserID("system"), bPage.ID, bPage.Title, bPage.Slug, &pageBContent, false)).To(Succeed())

		Expect(svc.IndexAllPages()).To(Succeed())

		out2, err := svc.GetOutgoingLinksForPage(pageAID)
		Expect(err).NotTo(HaveOccurred())
		Expect(out2).To(matchOutgoingResult(1,
			matchResolvedOutgoingResultItem(pageAID, pageBID, newFixtureRoutePath("/b")),
		))

		bl, err := svc.GetBacklinksForPage(pageBID)
		Expect(err).NotTo(HaveOccurred())
		Expect(bl).To(matchBacklinkResult(1,
			matchBacklinkResultItem(pageAID, pageBID),
		))

	})
})

var _ = ginkgo.Describe("exact-path link healing", ginkgo.Label("integration"), func() {
	ginkgo.It("resolves existing broken links without a full reindex", func() {
		svc, ts, _ := setupLinkService()

		aIDPtr, err := ts.CreateNode(newFixtureUserID("system"), nil, "Page A", newFixtureSlug("a"), pageNodeKind())
		Expect(err).NotTo(HaveOccurred())
		pageAID := *aIDPtr

		pageA, err := ts.GetPage(pageAID)
		Expect(err).NotTo(HaveOccurred())
		var linkToB = "Link to B: [Go](/b.md)"
		Expect(ts.UpdateNodeUncheckedVersion(newFixtureUserID("system"), pageA.ID, pageA.Title, pageA.Slug, &linkToB, false)).To(Succeed())

		Expect(svc.IndexAllPages()).To(Succeed())

		out1, err := svc.GetOutgoingLinksForPage(pageAID)
		Expect(err).NotTo(HaveOccurred())
		Expect(out1).To(matchOutgoingResult(1,
			matchBrokenOutgoingResultItem(pageAID, newFixtureRoutePath("/b")),
		))

		bIDPtr, err := ts.CreateNode(newFixtureUserID("system"), nil, "Page B", newFixtureSlug("b"), pageNodeKind())
		Expect(err).NotTo(HaveOccurred())
		pageBID := *bIDPtr

		pageB, err := ts.GetPage(pageBID)
		Expect(err).NotTo(HaveOccurred())

		Expect(svc.HealLinksForExactPath(pageB)).To(Succeed())

		out2, err := svc.GetOutgoingLinksForPage(pageAID)
		Expect(err).NotTo(HaveOccurred())
		Expect(out2).To(matchOutgoingResult(1,
			matchResolvedOutgoingResultItem(pageAID, pageBID, newFixtureRoutePath("/b")),
		))

		bl, err := svc.GetBacklinksForPage(pageBID)
		Expect(err).NotTo(HaveOccurred())
		Expect(bl).To(matchBacklinkResult(1,
			matchBacklinkResultItem(pageAID, pageBID),
		))

	})
})

var _ = ginkgo.Describe("broken incoming link queries", ginkgo.Label("integration"), func() {
	ginkgo.It("returns broken links that target the requested path", func() {
		svc, ts, store := setupLinkService()

		// Create three pages: A, B, C
		aIDPtr, err := ts.CreateNode(newFixtureUserID("system"), nil, "Page A", newFixtureSlug("a"), pageNodeKind())
		Expect(err).NotTo(HaveOccurred())
		pageAID := *aIDPtr

		bIDPtr, err := ts.CreateNode(newFixtureUserID("system"), nil, "Page B", newFixtureSlug("b"), pageNodeKind())
		Expect(err).NotTo(HaveOccurred())
		pageBID := *bIDPtr

		cIDPtr, err := ts.CreateNode(newFixtureUserID("system"), nil, "Page C", newFixtureSlug("c"), pageNodeKind())
		Expect(err).NotTo(HaveOccurred())
		pageCID := *cIDPtr

		// Update A and B to link to a non-existent page "/nonexistent"
		pageA, err := ts.GetPage(pageAID)
		Expect(err).NotTo(HaveOccurred())
		var linkToMissing = "Link: [Missing](/nonexistent)"
		Expect(ts.UpdateNodeUncheckedVersion(newFixtureUserID("system"), pageA.ID, pageA.Title, pageA.Slug, &linkToMissing, false)).To(Succeed())

		pageB, err := ts.GetPage(pageBID)
		Expect(err).NotTo(HaveOccurred())
		Expect(ts.UpdateNodeUncheckedVersion(newFixtureUserID("system"), pageB.ID, pageB.Title, pageB.Slug, &linkToMissing, false)).To(Succeed())

		// Page C links to a different broken page
		pageC, err := ts.GetPage(pageCID)
		Expect(err).NotTo(HaveOccurred())
		var linkToOther = "Link: [Other](/other-missing)"
		Expect(ts.UpdateNodeUncheckedVersion(newFixtureUserID("system"), pageC.ID, pageC.Title, pageC.Slug, &linkToOther, false)).To(Succeed())

		// Index all pages to create broken links
		Expect(svc.IndexAllPages()).To(Succeed())

		// Test: GetBrokenIncomingForPath should return broken links for "/nonexistent"
		brokenLinks, err := store.GetBrokenIncomingForPath(newFixtureRoutePath("/nonexistent"))
		Expect(err).NotTo(HaveOccurred())

		Expect(brokenLinks).To(ConsistOf(
			matchBrokenBacklink(pageAID),
			matchBrokenBacklink(pageBID),
		))

	})
})
