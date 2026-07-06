package links

import (
	"path/filepath"

	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/perber/wiki/internal/core/markdownlinks"
	"github.com/perber/wiki/internal/core/tree"
)

var _ = ginkgo.Describe("batch link updates and healing", ginkgo.Label("integration"), func() {
	ginkgo.It("updates and heals multiple source pages in one pass", func() {
		svc, ts, _ := setupLinkService()

		aIDPtr, err := ts.CreateNode(newFixtureUserID("system"), nil, "Page A", newFixtureSlug("a"), pageNodeKind())
		Expect(err).NotTo(HaveOccurred())
		pageAID := *aIDPtr

		cIDPtr, err := ts.CreateNode(newFixtureUserID("system"), nil, "Page C", newFixtureSlug("c"), pageNodeKind())
		Expect(err).NotTo(HaveOccurred())
		pageCID := *cIDPtr

		pageA, err := ts.GetPage(pageAID)
		Expect(err).NotTo(HaveOccurred())
		contentA := "Link: [B](/b.md)"
		Expect(ts.UpdateNodeUncheckedVersion(newFixtureUserID("system"), pageA.ID, pageA.Title, pageA.Slug, &contentA, false)).To(Succeed())

		pageC, err := ts.GetPage(pageCID)
		Expect(err).NotTo(HaveOccurred())
		contentC := "Link: [D](/d.md)"
		Expect(ts.UpdateNodeUncheckedVersion(newFixtureUserID("system"), pageC.ID, pageC.Title, pageC.Slug, &contentC, false)).To(Succeed())

		Expect(svc.IndexAllPages()).To(Succeed())

		bIDPtr, err := ts.CreateNode(newFixtureUserID("system"), nil, "Page B", newFixtureSlug("b"), pageNodeKind())
		Expect(err).NotTo(HaveOccurred())
		pageB, err := ts.GetPage(*bIDPtr)
		Expect(err).NotTo(HaveOccurred())

		dIDPtr, err := ts.CreateNode(newFixtureUserID("system"), nil, "Page D", newFixtureSlug("d"), pageNodeKind())
		Expect(err).NotTo(HaveOccurred())
		pageD, err := ts.GetPage(*dIDPtr)
		Expect(err).NotTo(HaveOccurred())

		Expect(svc.UpdateLinksAndHealForPages([]*tree.Page{pageB, pageD})).To(Succeed())

		outA, err := svc.GetOutgoingLinksForPage(pageAID)
		Expect(err).NotTo(HaveOccurred())
		Expect(outA).To(matchOutgoingResult(1,
			matchResolvedOutgoingResultItem(pageAID, pageB.ID, newFixtureRoutePath("/b")),
		))

		outC, err := svc.GetOutgoingLinksForPage(pageCID)
		Expect(err).NotTo(HaveOccurred())
		Expect(outC).To(matchOutgoingResult(1,
			matchResolvedOutgoingResultItem(pageCID, pageD.ID, newFixtureRoutePath("/d")),
		))

	})
})

var _ = ginkgo.Describe("batch healing for extensionless links", ginkgo.Label("integration"), func() {
	ginkgo.It("leaves extensionless links broken until a matching section exists", func() {
		svc, ts, _ := setupLinkService()

		sourceIDPtr, err := ts.CreateNode(newFixtureUserID("system"), nil, "Source", newFixtureSlug("source"), pageNodeKind())
		Expect(err).NotTo(HaveOccurred())
		source, err := ts.GetPage(*sourceIDPtr)
		Expect(err).NotTo(HaveOccurred())
		content := "[Legacy](/target)"
		Expect(ts.UpdateNodeUncheckedVersion(newFixtureUserID("system"), source.ID, source.Title, source.Slug, &content, false)).To(Succeed())

		Expect(svc.IndexAllPages()).To(Succeed())

		pageIDPtr, err := ts.CreateNode(newFixtureUserID("system"), nil, "Target", newFixtureSlug("target"), pageNodeKind())
		Expect(err).NotTo(HaveOccurred())
		pageTarget, err := ts.GetPage(*pageIDPtr)
		Expect(err).NotTo(HaveOccurred())
		Expect(svc.UpdateLinksAndHealForPages([]*tree.Page{pageTarget})).To(Succeed())

		pageBacklinks, err := svc.GetBacklinksForPage(pageTarget.ID)
		Expect(err).NotTo(HaveOccurred())
		Expect(pageBacklinks).To(matchBacklinkResult(0))
		outAfterPage, err := svc.GetOutgoingLinksForPage(source.ID)
		Expect(err).NotTo(HaveOccurred())
		Expect(outAfterPage).To(matchOutgoingResult(1,
			matchBrokenOutgoingResultItem(source.ID, newFixtureRoutePath("/target")),
		))

		sectionIDPtr, err := ts.CreateNode(newFixtureUserID("system"), nil, "Target Section", newFixtureSlug("target"), sectionNodeKind())
		Expect(err).NotTo(HaveOccurred())
		sectionTarget, err := ts.GetPage(*sectionIDPtr)
		Expect(err).NotTo(HaveOccurred())
		Expect(svc.UpdateLinksAndHealForPages([]*tree.Page{sectionTarget})).To(Succeed())

		sectionBacklinks, err := svc.GetBacklinksForPage(sectionTarget.ID)
		Expect(err).NotTo(HaveOccurred())
		Expect(sectionBacklinks).To(matchBacklinkResult(1,
			matchBacklinkResultItem(source.ID, sectionTarget.ID),
		))

	})
})

var _ = ginkgo.Describe("root-backed batch link updates", ginkgo.Label("integration"), func() {
	ginkgo.It("builds the markdown index once while updating multiple pages", func() {
		dataDir := linksTempDir()
		rootDir := filepath.Join(linksTempDir(), "workspace")
		writeLinkServiceMarkdown(filepath.Join(rootDir, "source-a.md"), `---
leafwiki_id: source-a
leafwiki_title: Source A
---
[Target](/target.md)
`)
		writeLinkServiceMarkdown(filepath.Join(rootDir, "source-b.md"), `---
leafwiki_id: source-b
leafwiki_title: Source B
---
[Target](/target.md)
`)
		writeLinkServiceMarkdown(filepath.Join(rootDir, "target.md"), `---
leafwiki_id: target
leafwiki_title: Target
---
# Target
`)

		ts := tree.NewTreeServiceWithOptions(tree.TreeOptions{DataDir: dataDir, RootDir: rootDir})
		Expect(ts.LoadTree()).To(Succeed())
		store, err := NewLinksStore(dataDir)
		Expect(err).NotTo(HaveOccurred())
		svc := NewLinkService(dataDir, ts, store)
		sourceA, err := ts.GetPage(newFixturePageID("source-a"))
		Expect(err).NotTo(HaveOccurred())
		sourceB, err := ts.GetPage(newFixturePageID("source-b"))
		Expect(err).NotTo(HaveOccurred())
		calls := countMarkdownRootIndexBuilds(markdownlinks.NewIndex([]markdownlinks.Entry{
			{Kind: markdownlinks.EntryKindPage, Path: newFixtureMarkdownPath("source-a.md")},
			{Kind: markdownlinks.EntryKindPage, Path: newFixtureMarkdownPath("source-b.md")},
			{Kind: markdownlinks.EntryKindPage, Path: newFixtureMarkdownPath("target.md")},
		}))

		Expect(svc.UpdateLinksAndHealForPages([]*tree.Page{sourceA, sourceB})).To(Succeed())

		Expect(*calls).To(Equal(1))

	})
})

var _ = ginkgo.Describe("empty batch link updates", ginkgo.Label("integration"), func() {
	ginkgo.It("skips markdown index construction for nil-only batches", func() {
		dataDir := linksTempDir()
		rootDir := filepath.Join(linksTempDir(), "workspace")
		writeLinkServiceMarkdown(filepath.Join(rootDir, "target.md"), "# Target\n")

		ts := tree.NewTreeServiceWithOptions(tree.TreeOptions{DataDir: dataDir, RootDir: rootDir})
		Expect(ts.LoadTree()).To(Succeed())
		store, err := NewLinksStore(dataDir)
		Expect(err).NotTo(HaveOccurred())
		svc := NewLinkService(dataDir, ts, store)
		calls := countMarkdownRootIndexBuilds(markdownlinks.NewIndex(nil))

		Expect(svc.UpdateLinksAndHealForPages([]*tree.Page{nil})).To(Succeed())

		Expect(*calls).To(BeZero())

	})
})

var _ = ginkgo.Describe("root-backed rewritten link updates", ginkgo.Label("integration"), func() {
	ginkgo.It("builds the markdown index once while rewriting multiple pages", func() {
		dataDir := linksTempDir()
		rootDir := filepath.Join(linksTempDir(), "workspace")
		writeLinkServiceMarkdown(filepath.Join(rootDir, "source-a.md"), `---
leafwiki_id: source-a
leafwiki_title: Source A
---
[Old](/old-target.md)
`)
		writeLinkServiceMarkdown(filepath.Join(rootDir, "source-b.md"), `---
leafwiki_id: source-b
leafwiki_title: Source B
---
[Old](/old-target.md)
`)
		writeLinkServiceMarkdown(filepath.Join(rootDir, "old-target.md"), `---
leafwiki_id: old-target
leafwiki_title: Old Target
---
# Old Target
`)
		writeLinkServiceMarkdown(filepath.Join(rootDir, "new-target.md"), `---
leafwiki_id: new-target
leafwiki_title: New Target
---
# New Target
`)

		ts := tree.NewTreeServiceWithOptions(tree.TreeOptions{DataDir: dataDir, RootDir: rootDir})
		Expect(ts.LoadTree()).To(Succeed())
		store, err := NewLinksStore(dataDir)
		Expect(err).NotTo(HaveOccurred())
		svc := NewLinkService(dataDir, ts, store)
		Expect(svc.IndexAllPages()).To(Succeed())
		sourceA, err := ts.GetPage(newFixturePageID("source-a"))
		Expect(err).NotTo(HaveOccurred())
		sourceB, err := ts.GetPage(newFixturePageID("source-b"))
		Expect(err).NotTo(HaveOccurred())
		calls := countMarkdownRootIndexBuilds(markdownlinks.NewIndex([]markdownlinks.Entry{
			{Kind: markdownlinks.EntryKindPage, Path: newFixtureMarkdownPath("source-a.md")},
			{Kind: markdownlinks.EntryKindPage, Path: newFixtureMarkdownPath("source-b.md")},
			{Kind: markdownlinks.EntryKindPage, Path: newFixtureMarkdownPath("old-target.md")},
			{Kind: markdownlinks.EntryKindPage, Path: newFixtureMarkdownPath("new-target.md")},
		}))

		Expect(svc.UpdateRewrittenLinksAndHealForPages([]*tree.Page{sourceA, sourceB}, []RewriteRule{{
			OldPath: newFixtureRoutePath("/old-target"),
			NewPath: newFixtureRoutePath("/new-target"),
			Kind:    TargetKindPage,
		}})).To(Succeed())

		Expect(*calls).To(Equal(1))

	})
})

var _ = ginkgo.Describe("empty rewritten link updates", ginkgo.Label("integration"), func() {
	ginkgo.It("skips markdown index construction for nil-only rewrite batches", func() {
		dataDir := linksTempDir()
		rootDir := filepath.Join(linksTempDir(), "workspace")
		writeLinkServiceMarkdown(filepath.Join(rootDir, "target.md"), "# Target\n")

		ts := tree.NewTreeServiceWithOptions(tree.TreeOptions{DataDir: dataDir, RootDir: rootDir})
		Expect(ts.LoadTree()).To(Succeed())
		store, err := NewLinksStore(dataDir)
		Expect(err).NotTo(HaveOccurred())
		svc := NewLinkService(dataDir, ts, store)
		calls := countMarkdownRootIndexBuilds(markdownlinks.NewIndex(nil))

		Expect(svc.UpdateRewrittenLinksAndHealForPages([]*tree.Page{nil}, nil)).To(Succeed())

		Expect(*calls).To(BeZero())

	})
})

var _ = ginkgo.Describe("source page reindexing during batch healing", ginkgo.Label("integration"), func() {
	ginkgo.It("moves backlinks from old targets to new targets after source content changes", func() {
		svc, ts, _ := setupLinkService()

		sourceIDPtr, err := ts.CreateNode(newFixtureUserID("system"), nil, "Source", newFixtureSlug("source"), pageNodeKind())
		Expect(err).NotTo(HaveOccurred())
		sourceID := *sourceIDPtr

		oldTargetIDPtr, err := ts.CreateNode(newFixtureUserID("system"), nil, "Old Target", newFixtureSlug("old-target"), pageNodeKind())
		Expect(err).NotTo(HaveOccurred())
		oldTargetID := *oldTargetIDPtr

		source, err := ts.GetPage(sourceID)
		Expect(err).NotTo(HaveOccurred())
		oldContent := "Link: [Old](/old-target.md)"
		Expect(ts.UpdateNodeUncheckedVersion(newFixtureUserID("system"), source.ID, source.Title, source.Slug, &oldContent, false)).To(Succeed())

		Expect(svc.IndexAllPages()).To(Succeed())

		newTargetIDPtr, err := ts.CreateNode(newFixtureUserID("system"), nil, "New Target", newFixtureSlug("new-target"), pageNodeKind())
		Expect(err).NotTo(HaveOccurred())
		newTargetID := *newTargetIDPtr

		updatedContent := "Link: [New](/new-target.md)"
		Expect(ts.UpdateNodeUncheckedVersion(newFixtureUserID("system"), source.ID, source.Title, source.Slug, &updatedContent, false)).To(Succeed())

		updatedSource, err := ts.GetPage(sourceID)
		Expect(err).NotTo(HaveOccurred())

		Expect(svc.UpdateLinksAndHealForPages([]*tree.Page{updatedSource})).To(Succeed())

		oldBacklinks, err := svc.GetBacklinksForPage(oldTargetID)
		Expect(err).NotTo(HaveOccurred())
		Expect(oldBacklinks).To(matchBacklinkResult(0))

		newBacklinks, err := svc.GetBacklinksForPage(newTargetID)
		Expect(err).NotTo(HaveOccurred())
		Expect(newBacklinks).To(matchBacklinkResult(1,
			matchBacklinkResultItem(sourceID, newTargetID),
		))

	})
})
