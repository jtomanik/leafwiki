package links

import (
	"path/filepath"

	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/perber/wiki/internal/core/markdownlinks"
	"github.com/perber/wiki/internal/core/tree"
)

var _ = ginkgo.Describe("link service indexing", ginkgo.Label("integration"), func() {
	ginkgo.It("records backlinks when pages link to existing targets", func() {
		svc, ts, _ := setupLinkService()
		pageAID, pageBID := createSimpleLinkedPages(ts)

		Expect(svc.IndexAllPages()).To(Succeed())

		data, err := svc.GetBacklinksForPage(pageBID)
		Expect(err).NotTo(HaveOccurred())

		Expect(data).To(matchBacklinkResult(1,
			matchBacklinkResultItem(pageAID, pageBID),
		))
	})
})

// - Duplicate syntaxes do not create duplicate target identities after migration
var _ = ginkgo.Describe("same-path page and section targets", ginkgo.Label("integration"), func() {
	ginkgo.It("keeps page and section target identities distinct", func() {
		dataDir := linksTempDir()
		rootDir := filepath.Join(linksTempDir(), "workspace")
		writeLinkServiceMarkdown(filepath.Join(rootDir, "docs", "source.md"), `---
leafwiki_id: source
leafwiki_title: Source
---
# Source

[Sync page](/docs/sync.md)
[Sync section](/docs/sync)
`)
		writeLinkServiceMarkdown(filepath.Join(rootDir, "docs", "section-source", "index.md"), `---
leafwiki_id: section-source
leafwiki_title: Section Source
---
# Section Source

[Sync page](/docs/sync.md)
`)
		writeLinkServiceMarkdown(filepath.Join(rootDir, "docs", "sync.md"), `---
leafwiki_id: sync-page
leafwiki_title: Sync Page
---
# Sync Page
`)
		writeLinkServiceMarkdown(filepath.Join(rootDir, "docs", "sync", "index.md"), `---
leafwiki_id: sync-section
leafwiki_title: Sync Section
---
# Sync Section
`)

		ts := tree.NewTreeServiceWithOptions(tree.TreeOptions{DataDir: dataDir, RootDir: rootDir})
		Expect(ts.LoadTree()).To(Succeed())
		store, err := NewLinksStore(dataDir)
		Expect(err).NotTo(HaveOccurred())
		svc := NewLinkService(dataDir, ts, store)
		source, err := ts.GetPage(newFixturePageID("source"))
		Expect(err).NotTo(HaveOccurred())

		Expect(svc.IndexAllPages()).To(Succeed())

		outgoing, err := svc.GetOutgoingLinksForPage(source.ID)
		Expect(err).NotTo(HaveOccurred())
		Expect(outgoing).To(matchOutgoingResult(2,
			matchResolvedOutgoingResultItem(source.ID, newFixturePageID("sync-page"), newFixtureRoutePath("/docs/sync")),
			matchResolvedOutgoingResultItem(source.ID, newFixturePageID("sync-section"), newFixtureRoutePath("/docs/sync")),
		))

		pageBacklinks, err := svc.GetBacklinksForPage(newFixturePageID("sync-page"))
		Expect(err).NotTo(HaveOccurred())
		Expect(pageBacklinks).To(matchBacklinkResult(2,
			matchBacklinkResultItemFromKind(tree.NodeKindPage),
			matchBacklinkResultItemFromKind(tree.NodeKindSection),
		))
		sectionBacklinks, err := svc.GetBacklinksForPage(newFixturePageID("sync-section"))
		Expect(err).NotTo(HaveOccurred())
		Expect(sectionBacklinks).To(matchBacklinkResult(1,
			matchBacklinkResultItemFromKind(tree.NodeKindPage),
		))

	})
})

var _ = ginkgo.Describe("markdown root prefix indexing", ginkgo.Label("integration"), func() {
	ginkgo.It("resolves prefixed absolute markdown links during full indexing", func() {
		dataDir := linksTempDir()
		repoRoot := linksTempDir()
		rootDir := filepath.Join(repoRoot, "docs")
		writeLinkServiceMarkdown(filepath.Join(rootDir, "source.md"), `---
leafwiki_id: source
leafwiki_title: Source
---
# Source

[Glossary](/docs/sync/glossary.md)
`)
		writeLinkServiceMarkdown(filepath.Join(rootDir, "sync", "glossary.md"), `---
leafwiki_id: glossary
leafwiki_title: Glossary
---
# Glossary
`)

		ts := tree.NewTreeServiceWithOptions(tree.TreeOptions{DataDir: dataDir, RootDir: rootDir})
		Expect(ts.LoadTree()).To(Succeed())
		store, err := NewLinksStore(dataDir)
		Expect(err).NotTo(HaveOccurred())
		svc := NewLinkServiceWithOptions(dataDir, ts, store, LinkServiceOptions{
			MarkdownLinkRootPrefix: "/docs",
		})
		source, err := ts.GetPage(newFixturePageID("source"))
		Expect(err).NotTo(HaveOccurred())

		Expect(svc.IndexAllPages()).To(Succeed())

		outgoing, err := svc.GetOutgoingLinksForPage(source.ID)
		Expect(err).NotTo(HaveOccurred())
		glossary, err := ts.GetPage(newFixturePageID("glossary"))
		Expect(err).NotTo(HaveOccurred())
		Expect(outgoing).To(matchOutgoingResult(1,
			matchResolvedOutgoingResultItem(source.ID, glossary.ID, newFixtureRoutePath("/sync/glossary")),
		))

	})
})

var _ = ginkgo.Describe("markdown root prefix page updates", ginkgo.Label("integration"), func() {
	ginkgo.It("resolves prefixed absolute markdown links during single-page updates", func() {
		dataDir := linksTempDir()
		repoRoot := linksTempDir()
		rootDir := filepath.Join(repoRoot, "docs")
		writeLinkServiceMarkdown(filepath.Join(rootDir, "source.md"), `---
leafwiki_id: source
leafwiki_title: Source
---
# Source

[Glossary](/docs/sync/glossary.md)
`)
		writeLinkServiceMarkdown(filepath.Join(rootDir, "sync", "glossary.md"), `---
leafwiki_id: glossary
leafwiki_title: Glossary
---
# Glossary
`)

		ts := tree.NewTreeServiceWithOptions(tree.TreeOptions{DataDir: dataDir, RootDir: rootDir})
		Expect(ts.LoadTree()).To(Succeed())
		store, err := NewLinksStore(dataDir)
		Expect(err).NotTo(HaveOccurred())
		svc := NewLinkServiceWithOptions(dataDir, ts, store, LinkServiceOptions{
			MarkdownLinkRootPrefix: "/docs",
		})
		source, err := ts.GetPage(newFixturePageID("source"))
		Expect(err).NotTo(HaveOccurred())

		Expect(svc.UpdateLinksForPage(source, source.Content)).To(Succeed())

		outgoing, err := svc.GetOutgoingLinksForPage(source.ID)
		Expect(err).NotTo(HaveOccurred())
		glossary, err := ts.GetPage(newFixturePageID("glossary"))
		Expect(err).NotTo(HaveOccurred())
		Expect(outgoing).To(matchOutgoingResult(1,
			matchResolvedOutgoingResultItem(source.ID, glossary.ID, newFixtureRoutePath("/sync/glossary")),
		))

	})
})

var _ = ginkgo.Describe("root-backed markdown indexing", ginkgo.Label("integration"), func() {
	ginkgo.It("builds the markdown index once for a full batch", func() {
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
		calls := countMarkdownRootIndexBuilds(markdownlinks.NewIndex([]markdownlinks.Entry{
			{Kind: markdownlinks.EntryKindPage, Path: newFixtureMarkdownPath("source-a.md")},
			{Kind: markdownlinks.EntryKindPage, Path: newFixtureMarkdownPath("source-b.md")},
			{Kind: markdownlinks.EntryKindPage, Path: newFixtureMarkdownPath("target.md")},
		}))

		Expect(svc.IndexAllPages()).To(Succeed())

		Expect(*calls).To(Equal(1))

	})
})

var _ = ginkgo.Describe("link service reindexing", ginkgo.Label("integration"), func() {
	ginkgo.It("replaces old links when source content changes", func() {
		svc, ts, _ := setupLinkService()
		pageAID, pageBID := createSimpleLinkedPages(ts)

		Expect(svc.IndexAllPages()).To(Succeed())

		aPage, err := ts.GetPage(pageAID)
		Expect(err).NotTo(HaveOccurred())
		var noLinks = "No more links."
		Expect(ts.UpdateNodeUncheckedVersion(newFixtureUserID("system"), aPage.ID, aPage.Title, aPage.Slug, &noLinks, false)).To(Succeed())

		Expect(svc.IndexAllPages()).To(Succeed())

		data, err := svc.GetBacklinksForPage(pageBID)
		Expect(err).NotTo(HaveOccurred())

		Expect(data.Backlinks).To(HaveLen(0))

	})
})
var _ = ginkgo.Describe("single-page link updates", ginkgo.Label("integration"), func() {
	ginkgo.It("updates only the selected page outgoing links", func() {
		svc, ts, _ := setupLinkService()
		pageAID, pageBID := createSimpleLinkedPages(ts)

		pageA, err := ts.GetPage(pageAID)
		Expect(err).NotTo(HaveOccurred())
		Expect(svc.UpdateLinksForPage(pageA, pageA.Content)).To(Succeed())

		dataB, err := svc.GetBacklinksForPage(pageBID)
		Expect(err).NotTo(HaveOccurred())
		Expect(dataB.Backlinks).To(HaveLen(1))

		dataA, err := svc.GetBacklinksForPage(pageAID)
		Expect(err).NotTo(HaveOccurred())
		Expect(dataA.Backlinks).To(HaveLen(0))

	})
})

var _ = ginkgo.Describe("link clearing", ginkgo.Label("integration"), func() {
	ginkgo.It("removes every indexed link", func() {
		svc, ts, _ := setupLinkService()
		_, pageBID := createSimpleLinkedPages(ts)

		Expect(svc.IndexAllPages()).To(Succeed())

		Expect(svc.ClearLinks()).To(Succeed())

		data, err := svc.GetBacklinksForPage(pageBID)
		Expect(err).NotTo(HaveOccurred())
		Expect(data.Backlinks).To(HaveLen(0))

	})
})

var _ = ginkgo.Describe("outgoing link queries", ginkgo.Label("integration"), func() {
	ginkgo.It("returns resolved outgoing links with target page details", func() {
		svc, ts, _ := setupLinkService()
		pageAID, pageBID := createSimpleLinkedPages(ts)

		Expect(svc.IndexAllPages()).To(Succeed())

		result, err := svc.GetOutgoingLinksForPage(pageAID)
		Expect(err).NotTo(HaveOccurred())

		pageB, err := ts.GetPage(pageBID)
		Expect(err).NotTo(HaveOccurred())
		Expect(result).To(matchOutgoingResult(1,
			SatisfyAll(
				matchResolvedOutgoingResultItem(pageAID, pageBID, newFixtureRoutePath("/b")),
				matchOutgoingResultItemTitle(pageB.Title),
			),
		))

	})
})

var _ = ginkgo.Describe("outgoing link queries for pages without links", ginkgo.Label("integration"), func() {
	ginkgo.It("returns an empty outgoing result", func() {
		svc, ts, _ := setupLinkService()

		aIDPtr, err := ts.CreateNode(newFixtureUserID("system"), nil, "Lonely Page", newFixtureSlug("lonely"), pageNodeKind())
		Expect(err).NotTo(HaveOccurred())
		lonelyID := *aIDPtr

		page, err := ts.GetPage(lonelyID)
		Expect(err).NotTo(HaveOccurred())

		var noLinks = "Just some text, no links."
		Expect(ts.UpdateNodeUncheckedVersion(newFixtureUserID("system"), page.ID, page.Title, page.Slug, &noLinks, false)).To(Succeed())

		Expect(svc.IndexAllPages()).To(Succeed())

		result, err := svc.GetOutgoingLinksForPage(lonelyID)
		Expect(err).NotTo(HaveOccurred())

		Expect(result).To(matchOutgoingResult(0))

	})
})

var _ = ginkgo.Describe("asset link filtering during indexing", ginkgo.Label("integration"), func() {
	ginkgo.It("excludes asset links from outgoing and broken-link sets", func() {
		svc, ts, _ := setupLinkService()

		aIDPtr, err := ts.CreateNode(newFixtureUserID("system"), nil, "Page A", newFixtureSlug("a"), pageNodeKind())
		Expect(err).NotTo(HaveOccurred())
		pageAID := *aIDPtr

		bIDPtr, err := ts.CreateNode(newFixtureUserID("system"), nil, "Page B", newFixtureSlug("b"), pageNodeKind())
		Expect(err).NotTo(HaveOccurred())
		pageBID := *bIDPtr

		pageA, err := ts.GetPage(pageAID)
		Expect(err).NotTo(HaveOccurred())
		contentA := "Asset: [Manual](/assets/abc/manual.pdf)\nPage: [Go](/b)"
		Expect(ts.UpdateNodeUncheckedVersion(newFixtureUserID("system"), pageA.ID, pageA.Title, pageA.Slug, &contentA, false)).To(Succeed())

		Expect(svc.IndexAllPages()).To(Succeed())

		outgoing, err := svc.GetOutgoingLinksForPage(pageAID)
		Expect(err).NotTo(HaveOccurred())
		Expect(outgoing).To(matchOutgoingResult(1,
			matchBrokenOutgoingResultItem(pageAID, newFixtureRoutePath("/b")),
		))

		status, err := svc.GetLinkStatusForPage(pageAID, newFixtureRoutePath("/a"))
		Expect(err).NotTo(HaveOccurred())
		Expect(status.Counts).To(matchLinkStatusCounts(0, 0, 0, 1))

		backlinks, err := svc.GetBacklinksForPage(pageBID)
		Expect(err).NotTo(HaveOccurred())
		Expect(backlinks).To(matchBacklinkResult(0))

	})
})
