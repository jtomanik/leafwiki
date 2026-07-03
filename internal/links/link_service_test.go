package links

import (
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"os"
	"path/filepath"

	"github.com/perber/wiki/internal/core/markdownlinks"
	"github.com/perber/wiki/internal/core/tree"
)

// Canonical Markdown links plan scenarios covered by tests in this file:
// - Relative section links are resolved from the source file directory
// - Canonical .md page link indexes as outgoing link
// - Canonical section link indexes as outgoing link
// - Assets are not coerced
// - Broken canonical .md page link is reported as broken
// - Duplicate syntaxes do not create duplicate target identities after migration
// - Image links remain governed by existing image and asset validation

func pageNodeKind() *tree.NodeKind {
	kind := tree.NodeKindPage
	return &kind
}

func sectionNodeKind() *tree.NodeKind {
	kind := tree.NodeKindSection
	return &kind
}

func writeLinkServiceMarkdown(filePath string, content string) {
	ginkgo.GinkgoHelper()
	Expect(os.MkdirAll(filepath.Dir(filePath), 0o755)).To(Succeed())
	Expect(os.WriteFile(filePath, []byte(content), 0o644)).To(Succeed())
}

func countMarkdownRootIndexBuilds(index *markdownlinks.Index) *int {
	ginkgo.GinkgoHelper()
	original := newMarkdownLinkIndexFromRoot
	calls := 0
	newMarkdownLinkIndexFromRoot = func(rootDir string) (*markdownlinks.Index, error) {
		calls++
		return index, nil
	}
	ginkgo.DeferCleanup(func() {
		newMarkdownLinkIndexFromRoot = original
	})
	return &calls
}

var _ = ginkgo.Describe("markdown link extraction", func() {
	ginkgo.It("returns normalized wiki destinations while filtering external anchors and query details", func() {
		md := `
# Example

Internal: [Page 1](/docs/page1)
Relative: [Rel](../docs/page2)
Anchor only: [Section](#heading)
External: [Google](https://google.com)
Mail: [Mail](mailto:test@example.com)
With fragment: [WithFragment](/docs/page3#intro)
With query: [WithQuery](/docs/page4?foo=bar)
With both: [Both](/docs/page5?foo=bar#section)
`

		links := extractLinksFromMarkdown(md)

		want := []string{
			"/docs/page1",
			"../docs/page2",
			"/docs/page3",
			"/docs/page4",
			"/docs/page5",
		}

		Expect(links).To(Equal(want))
	})

	ginkgo.It("filters external schemes case-insensitively", func() {
		md := `
[HTTPS](HTTPS://example.com)
[Mail](Mailto:test@example.com)
[Anchor](#intro)
[Wiki](/docs/page1)
`

		links := extractLinksFromMarkdown(md)

		Expect(links).To(Equal([]string{"/docs/page1"}))
	})

	ginkgo.It("leaves asset destinations out of wiki link extraction", func() {
		md := `
Asset absolute: [File](/assets/abc/manual.pdf)
Asset relative: [Image](assets/abc/picture.png)
Internal: [Page](/docs/page1)
`

		links := extractLinksFromMarkdown(md)

		Expect(links).To(Equal([]string{"/docs/page1"}))
	})

	ginkgo.It("ignores image links even when they point at page destinations", func() {
		md := `
Image page path: ![Alt](/docs/b.md)
Normal page link: [Page](/docs/b.md)
`

		links := extractLinksFromMarkdown(md)

		Expect(links).To(Equal([]string{"/docs/b.md"}))
	})

	ginkgo.It("filters mixed-case external schemes while keeping wiki links", func() {
		md := `
Uppercase HTTPS: [A](HTTPS://example.com)
Uppercase HTTP: [B](HTTP://example.com)
Uppercase MAILTO: [C](MAILTO:foo@bar.com)
Mixed case Https: [D](Https://example.com)
Mixed case Mailto: [E](Mailto:foo@bar.com)
Internal: [Page](/docs/page1)
`

		links := extractLinksFromMarkdown(md)

		Expect(links).To(Equal([]string{"/docs/page1"}))
	})
})

var _ = ginkgo.Describe("link service refactor matches", func() {
	ginkgo.It("accepts route paths when matching descendants by prefix and kind", func() {
		store, err := NewLinksStore(linksTempDir())
		Expect(err).NotTo(HaveOccurred())

		Expect(store.AddLinks(newFixturePageID("source"), "Source", []TargetLink{
			{TargetPageID: newFixturePageID("target"), TargetPagePath: "/docs/guide", TargetKind: TargetKindSection},
			{TargetPageID: newFixturePageID("child"), TargetPagePath: "/docs/guide/child", TargetKind: TargetKindSection},
		})).To(Succeed())

		service := NewLinkService(linksTempDir(), nil, store)
		matches, err := service.GetRefactorMatchesForPrefixAndKind(tree.RoutePath("docs/guide"), tree.NodeKindSection)
		Expect(err).NotTo(HaveOccurred())
		Expect(matches).To(HaveLen(2))
	})
})

// helper to create a small tree structure:
// root
//
//	└─ docs
//	     ├─ page1
//	     └─ page2
func setupTreeForLinksTest() (*tree.TreeService, tree.PageID, tree.PageID) {
	ginkgo.GinkgoHelper()

	storageDir := linksTempDir()
	ts := tree.NewTreeService(storageDir)

	Expect(ts.LoadTree()).To(Succeed())

	// create "docs" under root
	docsIDPtr, err := ts.CreateNode("system", nil, "Docs", "docs", pageNodeKind())
	Expect(err).NotTo(HaveOccurred())
	docsID := *docsIDPtr

	// create "page1" and "page2" under docs
	page1IDPtr, err := ts.CreateNode("system", &docsID, "Page 1", "page1", pageNodeKind())
	Expect(err).NotTo(HaveOccurred())
	page2IDPtr, err := ts.CreateNode("system", &docsID, "Page 2", "page2", pageNodeKind())
	Expect(err).NotTo(HaveOccurred())

	return ts, *page1IDPtr, *page2IDPtr
}

var _ = ginkgo.Describe("target link resolution", func() {
	ginkgo.It("resolves relative page links from the current page directory", func() {
		ts, page1ID, page2ID := setupTreeForLinksTest()

		// current page: docs/page1
		page1, err := ts.GetPage(newFixturePageID(page1ID))
		Expect(err).NotTo(HaveOccurred())
		currentPath := page1.CalculatePath() // should be "docs/page1"

		// we want to link from page1 to page2 using a relative link
		links := []string{"./page2.md"}

		targets := resolveTargetLinks(ts, tree.RoutePathFromString(currentPath), links)

		Expect(targets).To(ConsistOf(matchResolvedTargetLink(newFixturePageID(page2ID), "/docs/page2")))
	})

	// - Canonical .md page link indexes as outgoing link
	ginkgo.It("resolves canonical relative page markdown links from the source file directory", func() {
		ts, page1ID, page2ID := setupTreeForLinksTest()

		page1, err := ts.GetPage(newFixturePageID(page1ID))
		Expect(err).NotTo(HaveOccurred())

		targets := resolveTargetLinks(ts, page1.CalculateRoutePath(), []string{"./page2.md"})

		Expect(targets).To(ConsistOf(matchResolvedTargetLink(newFixturePageID(page2ID), "/docs/page2")))
	})

	// - Relative section links are resolved from the source file directory
	ginkgo.It("resolves relative section links from the source file directory", func() {
		storageDir := linksTempDir()
		ts := tree.NewTreeService(storageDir)
		Expect(ts.LoadTree()).To(Succeed())

		docsID, err := ts.CreateNode("system", nil, "Docs", "docs", sectionNodeKind())
		Expect(err).NotTo(HaveOccurred())
		aID, err := ts.CreateNode("system", docsID, "A", "a", sectionNodeKind())
		Expect(err).NotTo(HaveOccurred())
		currentID, err := ts.CreateNode("system", aID, "Current", "current", pageNodeKind())
		Expect(err).NotTo(HaveOccurred())
		bID, err := ts.CreateNode("system", docsID, "B", "b", sectionNodeKind())
		Expect(err).NotTo(HaveOccurred())
		current, err := ts.GetPage(*currentID)
		Expect(err).NotTo(HaveOccurred())

		targets := resolveTargetLinks(ts, current.CalculateRoutePath(), []string{"../b"})

		Expect(targets).To(ConsistOf(matchResolvedTargetLink(*bID, "/docs/b")))
	})

	ginkgo.It("resolves section default files through README fallback links", func() {
		dataDir := linksTempDir()
		rootDir := filepath.Join(linksTempDir(), "workspace")
		writeLinkServiceMarkdown(filepath.Join(rootDir, "source.md"), `---
leafwiki_id: page-source
leafwiki_title: Source
---
# Source

[Guides](/guides/README.md)
`)
		writeLinkServiceMarkdown(filepath.Join(rootDir, "guides", "README.md"), `---
leafwiki_id: section-guides
leafwiki_title: Guides
---
# Guides
`)

		ts := tree.NewTreeServiceWithOptions(tree.TreeOptions{DataDir: dataDir, RootDir: rootDir})
		Expect(ts.LoadTree()).To(Succeed())
		source, err := ts.GetPage("page-source")
		Expect(err).NotTo(HaveOccurred())
		guides, err := ts.GetPage("section-guides")
		Expect(err).NotTo(HaveOccurred())

		targets := resolveTargetLinks(ts, source.CalculateRoutePath(), extractLinksFromMarkdown(source.Content))

		Expect(targets).To(ConsistOf(matchResolvedTargetLink(guides.ID, "/guides")))
	})

	// - Canonical section link indexes as outgoing link
	ginkgo.It("prefers canonical section targets over same-basename page twins for extensionless links", func() {
		dataDir := linksTempDir()
		rootDir := filepath.Join(linksTempDir(), "workspace")
		writeLinkServiceMarkdown(filepath.Join(rootDir, "docs", "a.md"), `---
leafwiki_id: page-a
leafwiki_title: Page A
---
# Page A

[Sync](/docs/sync)
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
		source, err := ts.GetPage("page-a")
		Expect(err).NotTo(HaveOccurred())

		targets := resolveTargetLinks(ts, source.CalculateRoutePath(), extractLinksFromMarkdown(source.Content))

		Expect(targets).To(ConsistOf(matchResolvedTargetLink(newFixturePageID("sync-section"), "/docs/sync")))
	})

	ginkgo.It("uses the section source file when same-basename page and section nodes both exist", func() {
		dataDir := linksTempDir()
		rootDir := filepath.Join(linksTempDir(), "workspace")
		writeLinkServiceMarkdown(filepath.Join(rootDir, "docs", "sync.md"), `---
leafwiki_id: sync-page
leafwiki_title: Sync Page
---
# Sync Page

[Wrong](./child.md)
`)
		writeLinkServiceMarkdown(filepath.Join(rootDir, "docs", "sync", "index.md"), `---
leafwiki_id: sync-section
leafwiki_title: Sync Section
---
# Sync Section

[Child](./child.md)
`)
		writeLinkServiceMarkdown(filepath.Join(rootDir, "docs", "sync", "child.md"), `---
leafwiki_id: sync-child
leafwiki_title: Sync Child
---
# Sync Child
`)

		ts := tree.NewTreeServiceWithOptions(tree.TreeOptions{DataDir: dataDir, RootDir: rootDir})
		Expect(ts.LoadTree()).To(Succeed())
		source, err := ts.GetPage("sync-section")
		Expect(err).NotTo(HaveOccurred())

		targets := resolveTargetLinksForSourceKind(ts, source.CalculateRoutePath(), source.Kind, extractLinksFromMarkdown(source.Content))

		Expect(targets).To(ConsistOf(matchResolvedTargetLink(newFixturePageID("sync-child"), "/docs/sync/child")))
	})

	ginkgo.It("uses the page source file when same-basename page and section nodes both exist", func() {
		dataDir := linksTempDir()
		rootDir := filepath.Join(linksTempDir(), "workspace")
		writeLinkServiceMarkdown(filepath.Join(rootDir, "docs", "sync.md"), `---
leafwiki_id: sync-page
leafwiki_title: Sync Page
---
# Sync Page

[Sibling](./sibling.md)
`)
		writeLinkServiceMarkdown(filepath.Join(rootDir, "docs", "sibling.md"), `---
leafwiki_id: sync-sibling
leafwiki_title: Sync Sibling
---
# Sync Sibling
`)
		writeLinkServiceMarkdown(filepath.Join(rootDir, "docs", "sync", "index.md"), `---
leafwiki_id: sync-section
leafwiki_title: Sync Section
---
# Sync Section
`)
		writeLinkServiceMarkdown(filepath.Join(rootDir, "docs", "sync", "sibling.md"), `---
leafwiki_id: nested-sibling
leafwiki_title: Nested Sibling
---
# Nested Sibling
`)

		ts := tree.NewTreeServiceWithOptions(tree.TreeOptions{DataDir: dataDir, RootDir: rootDir})
		Expect(ts.LoadTree()).To(Succeed())
		source, err := ts.GetPage("sync-page")
		Expect(err).NotTo(HaveOccurred())

		targets := resolveTargetLinksForSourceKind(ts, source.CalculateRoutePath(), source.Kind, extractLinksFromMarkdown(source.Content))

		Expect(targets).To(ConsistOf(matchResolvedTargetLink(newFixturePageID("sync-sibling"), "/docs/sibling")))
	})

	// - Broken canonical .md page link is reported as broken
	ginkgo.It("returns broken targets with normalized paths for missing destinations", func() {
		ts, page1ID, _ := setupTreeForLinksTest()

		page1, err := ts.GetPage(newFixturePageID(page1ID))
		Expect(err).NotTo(HaveOccurred())
		currentPath := page1.CalculatePath()

		links := []string{
			"./does-not-exist",
			"/docs/unknown",
		}

		targets := resolveTargetLinks(ts, tree.RoutePathFromString(currentPath), links)

		Expect(targets).To(ConsistOf(
			matchBrokenTargetLink("/docs/does-not-exist"),
			matchBrokenTargetLink("/docs/unknown"),
		))
	})

	ginkgo.It("ignores asset destinations during target resolution", func() {
		ts, page1ID, _ := setupTreeForLinksTest()

		page1, err := ts.GetPage(newFixturePageID(page1ID))
		Expect(err).NotTo(HaveOccurred())

		targets := resolveTargetLinks(ts, page1.CalculateRoutePath(), []string{
			"/assets/abc/manual.pdf",
			"assets/abc/picture.png",
		})

		Expect(targets).To(BeEmpty())
	})
})

func setupLinkService() (*LinkService, *tree.TreeService, *LinksStore) {
	ginkgo.GinkgoHelper()

	dataDir := linksTempDir()

	ts := tree.NewTreeService(dataDir)
	Expect(ts.LoadTree()).To(Succeed())

	store, err := NewLinksStore(dataDir)
	Expect(err).NotTo(HaveOccurred())

	svc := NewLinkService(dataDir, ts, store)
	return svc, ts, store
}

func createSimpleLinkedPages(ts *tree.TreeService) (pageAID, pageBID tree.PageID) {
	ginkgo.GinkgoHelper()

	aIDPtr, err := ts.CreateNode("system", nil, "Page A", "a", pageNodeKind())
	Expect(err).NotTo(HaveOccurred())
	pageAID = *aIDPtr

	bIDPtr, err := ts.CreateNode("system", nil, "Page B", "b", pageNodeKind())
	Expect(err).NotTo(HaveOccurred())
	pageBID = *bIDPtr

	aPage, err := ts.GetPage(newFixturePageID(pageAID))
	Expect(err).NotTo(HaveOccurred())
	contentA := "Link to B: [Go to B](/b.md)"
	Expect(ts.UpdateNodeUncheckedVersion(newFixtureUserID("system"), newFixturePageID(aPage.ID), aPage.Title, newFixtureSlug(aPage.Slug), &contentA, false)).To(Succeed())

	bPage, err := ts.GetPage(newFixturePageID(pageBID))
	Expect(err).NotTo(HaveOccurred())
	contentB := "# Page B\nNo outgoing links."
	Expect(ts.UpdateNodeUncheckedVersion(newFixtureUserID("system"), newFixturePageID(bPage.ID), bPage.Title, newFixtureSlug(bPage.Slug), &contentB, false)).To(Succeed())

	return pageAID, pageBID
}

var _ = ginkgo.Describe("link service indexing", func() {
	ginkgo.It("records backlinks when pages link to existing targets", func() {
		svc, ts, _ := setupLinkService()
		pageAID, pageBID := createSimpleLinkedPages(ts)

		Expect(svc.IndexAllPages()).To(Succeed())

		data, err := svc.GetBacklinksForPage(newFixturePageID(pageBID))
		Expect(err).NotTo(HaveOccurred())

		Expect(data).To(matchBacklinkResult(1,
			matchBacklinkResultItem(newFixturePageID(pageAID), newFixturePageID(pageBID)),
		))
	})
})

// - Duplicate syntaxes do not create duplicate target identities after migration
var _ = ginkgo.Describe("same-path page and section targets", func() {
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
		source, err := ts.GetPage("source")
		Expect(err).NotTo(HaveOccurred())

		Expect(svc.IndexAllPages()).To(Succeed())

		outgoing, err := svc.GetOutgoingLinksForPage(source.ID)
		Expect(err).NotTo(HaveOccurred())
		Expect(outgoing).To(matchOutgoingResult(2,
			matchResolvedOutgoingResultItem(source.ID, newFixturePageID("sync-page"), tree.RoutePath("/docs/sync")),
			matchResolvedOutgoingResultItem(source.ID, newFixturePageID("sync-section"), tree.RoutePath("/docs/sync")),
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

var _ = ginkgo.Describe("markdown root prefix indexing", func() {
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
		source, err := ts.GetPage("source")
		Expect(err).NotTo(HaveOccurred())

		Expect(svc.IndexAllPages()).To(Succeed())

		outgoing, err := svc.GetOutgoingLinksForPage(source.ID)
		Expect(err).NotTo(HaveOccurred())
		glossary, err := ts.GetPage("glossary")
		Expect(err).NotTo(HaveOccurred())
		Expect(outgoing).To(matchOutgoingResult(1,
			matchResolvedOutgoingResultItem(source.ID, glossary.ID, tree.RoutePath("/sync/glossary")),
		))

	})
})

var _ = ginkgo.Describe("markdown root prefix page updates", func() {
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
		source, err := ts.GetPage("source")
		Expect(err).NotTo(HaveOccurred())

		Expect(svc.UpdateLinksForPage(source, source.Content)).To(Succeed())

		outgoing, err := svc.GetOutgoingLinksForPage(source.ID)
		Expect(err).NotTo(HaveOccurred())
		glossary, err := ts.GetPage("glossary")
		Expect(err).NotTo(HaveOccurred())
		Expect(outgoing).To(matchOutgoingResult(1,
			matchResolvedOutgoingResultItem(source.ID, glossary.ID, tree.RoutePath("/sync/glossary")),
		))

	})
})

var _ = ginkgo.Describe("root-backed markdown indexing", func() {
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
			{Kind: markdownlinks.EntryKindPage, Path: "source-a.md"},
			{Kind: markdownlinks.EntryKindPage, Path: "source-b.md"},
			{Kind: markdownlinks.EntryKindPage, Path: "target.md"},
		}))

		Expect(svc.IndexAllPages()).To(Succeed())

		Expect(*calls).To(Equal(1))

	})
})

var _ = ginkgo.Describe("link service reindexing", func() {
	ginkgo.It("replaces old links when source content changes", func() {
		svc, ts, _ := setupLinkService()
		pageAID, pageBID := createSimpleLinkedPages(ts)

		Expect(svc.IndexAllPages()).To(Succeed())

		aPage, err := ts.GetPage(newFixturePageID(pageAID))
		Expect(err).NotTo(HaveOccurred())
		var noLinks = "No more links."
		Expect(ts.UpdateNodeUncheckedVersion(newFixtureUserID("system"), newFixturePageID(aPage.ID), aPage.Title, newFixtureSlug(aPage.Slug), &noLinks, false)).To(Succeed())

		Expect(svc.IndexAllPages()).To(Succeed())

		data, err := svc.GetBacklinksForPage(newFixturePageID(pageBID))
		Expect(err).NotTo(HaveOccurred())

		Expect(data.Backlinks).To(HaveLen(0))

	})
})
var _ = ginkgo.Describe("single-page link updates", func() {
	ginkgo.It("updates only the selected page outgoing links", func() {
		svc, ts, _ := setupLinkService()
		pageAID, pageBID := createSimpleLinkedPages(ts)

		pageA, err := ts.GetPage(newFixturePageID(pageAID))
		Expect(err).NotTo(HaveOccurred())
		Expect(svc.UpdateLinksForPage(pageA, pageA.Content)).To(Succeed())

		dataB, err := svc.GetBacklinksForPage(newFixturePageID(pageBID))
		Expect(err).NotTo(HaveOccurred())
		Expect(dataB.Backlinks).To(HaveLen(1))

		dataA, err := svc.GetBacklinksForPage(newFixturePageID(pageAID))
		Expect(err).NotTo(HaveOccurred())
		Expect(dataA.Backlinks).To(HaveLen(0))

	})
})

var _ = ginkgo.Describe("link clearing", func() {
	ginkgo.It("removes every indexed link", func() {
		svc, ts, _ := setupLinkService()
		_, pageBID := createSimpleLinkedPages(ts)

		Expect(svc.IndexAllPages()).To(Succeed())

		Expect(svc.ClearLinks()).To(Succeed())

		data, err := svc.GetBacklinksForPage(newFixturePageID(pageBID))
		Expect(err).NotTo(HaveOccurred())
		Expect(data.Backlinks).To(HaveLen(0))

	})
})

var _ = ginkgo.Describe("outgoing link queries", func() {
	ginkgo.It("returns resolved outgoing links with target page details", func() {
		svc, ts, _ := setupLinkService()
		pageAID, pageBID := createSimpleLinkedPages(ts)

		Expect(svc.IndexAllPages()).To(Succeed())

		result, err := svc.GetOutgoingLinksForPage(newFixturePageID(pageAID))
		Expect(err).NotTo(HaveOccurred())

		pageB, err := ts.GetPage(newFixturePageID(pageBID))
		Expect(err).NotTo(HaveOccurred())
		Expect(result).To(matchOutgoingResult(1,
			SatisfyAll(
				matchResolvedOutgoingResultItem(newFixturePageID(pageAID), newFixturePageID(pageBID), tree.RoutePath("/b")),
				matchOutgoingResultItemTitle(pageB.Title),
			),
		))

	})
})

var _ = ginkgo.Describe("outgoing link queries for pages without links", func() {
	ginkgo.It("returns an empty outgoing result", func() {
		svc, ts, _ := setupLinkService()

		aIDPtr, err := ts.CreateNode("system", nil, "Lonely Page", "lonely", pageNodeKind())
		Expect(err).NotTo(HaveOccurred())
		lonelyID := *aIDPtr

		page, err := ts.GetPage(newFixturePageID(lonelyID))
		Expect(err).NotTo(HaveOccurred())

		var noLinks = "Just some text, no links."
		Expect(ts.UpdateNodeUncheckedVersion(newFixtureUserID("system"), newFixturePageID(page.ID), page.Title, newFixtureSlug(page.Slug), &noLinks, false)).To(Succeed())

		Expect(svc.IndexAllPages()).To(Succeed())

		result, err := svc.GetOutgoingLinksForPage(newFixturePageID(lonelyID))
		Expect(err).NotTo(HaveOccurred())

		Expect(result).To(matchOutgoingResult(0))

	})
})

var _ = ginkgo.Describe("asset link filtering during indexing", func() {
	ginkgo.It("excludes asset links from outgoing and broken-link sets", func() {
		svc, ts, _ := setupLinkService()

		aIDPtr, err := ts.CreateNode("system", nil, "Page A", "a", pageNodeKind())
		Expect(err).NotTo(HaveOccurred())
		pageAID := *aIDPtr

		bIDPtr, err := ts.CreateNode("system", nil, "Page B", "b", pageNodeKind())
		Expect(err).NotTo(HaveOccurred())
		pageBID := *bIDPtr

		pageA, err := ts.GetPage(newFixturePageID(pageAID))
		Expect(err).NotTo(HaveOccurred())
		contentA := "Asset: [Manual](/assets/abc/manual.pdf)\nPage: [Go](/b)"
		Expect(ts.UpdateNodeUncheckedVersion(newFixtureUserID("system"), newFixturePageID(pageA.ID), pageA.Title, newFixtureSlug(pageA.Slug), &contentA, false)).To(Succeed())

		Expect(svc.IndexAllPages()).To(Succeed())

		outgoing, err := svc.GetOutgoingLinksForPage(newFixturePageID(pageAID))
		Expect(err).NotTo(HaveOccurred())
		Expect(outgoing).To(matchOutgoingResult(1,
			matchBrokenOutgoingResultItem(newFixturePageID(pageAID), tree.RoutePath("/b")),
		))

		status, err := svc.GetLinkStatusForPage(newFixturePageID(pageAID), "/a")
		Expect(err).NotTo(HaveOccurred())
		Expect(status.Counts).To(matchLinkStatusCounts(0, 0, 0, 1))

		backlinks, err := svc.GetBacklinksForPage(newFixturePageID(pageBID))
		Expect(err).NotTo(HaveOccurred())
		Expect(backlinks).To(matchBacklinkResult(0))

	})
})

var _ = ginkgo.Describe("outgoing result mapping", func() {
	ginkgo.It("adds resolved target metadata to outgoing result items", func() {
		ts, page1ID, page2ID := setupTreeForLinksTest()

		root := ts.GetTree()
		Expect(root).NotTo(BeNil())

		outgoings := []Outgoing{{FromPageID: newFixturePageID(page1ID), ToPageID: newFixturePageID(page2ID), ToPath: "/docs/page2", Broken: false, FromTitle: "Page 1"}}

		result := toOutgoingLinkResult(ts, outgoings)
		page2, err := ts.GetPage(newFixturePageID(page2ID))
		Expect(err).NotTo(HaveOccurred())
		Expect(result).To(matchOutgoingResult(1,
			SatisfyAll(
				matchResolvedOutgoingResultItem(newFixturePageID(page1ID), newFixturePageID(page2ID), tree.RoutePath("/docs/page2")),
				matchOutgoingResultItemTitle(page2.Title),
			),
		))

	})
})

var _ = ginkgo.Describe("late-created target indexing", func() {
	ginkgo.It("resolves formerly broken links after reindexing", func() {
		svc, ts, _ := setupLinkService()

		aIDPtr, err := ts.CreateNode("system", nil, "Page A", "a", pageNodeKind())
		Expect(err).NotTo(HaveOccurred())
		pageAID := *aIDPtr

		aPage, err := ts.GetPage(newFixturePageID(pageAID))
		Expect(err).NotTo(HaveOccurred())
		var linkToB = "Link to B: [Go](/b.md)"
		Expect(ts.UpdateNodeUncheckedVersion(newFixtureUserID("system"), newFixturePageID(aPage.ID), aPage.Title, newFixtureSlug(aPage.Slug), &linkToB, false)).To(Succeed())

		Expect(svc.IndexAllPages()).To(Succeed())

		out1, err := svc.GetOutgoingLinksForPage(newFixturePageID(pageAID))
		Expect(err).NotTo(HaveOccurred())
		Expect(out1).To(matchOutgoingResult(1,
			matchBrokenOutgoingResultItem(newFixturePageID(pageAID), tree.RoutePath("/b")),
		))

		bIDPtr, err := ts.CreateNode("system", nil, "Page B", "b", pageNodeKind())
		Expect(err).NotTo(HaveOccurred())
		pageBID := *bIDPtr

		bPage, err := ts.GetPage(newFixturePageID(pageBID))
		Expect(err).NotTo(HaveOccurred())
		var pageBContent = "# Page B"
		Expect(ts.UpdateNodeUncheckedVersion(newFixtureUserID("system"), newFixturePageID(bPage.ID), bPage.Title, newFixtureSlug(bPage.Slug), &pageBContent, false)).To(Succeed())

		Expect(svc.IndexAllPages()).To(Succeed())

		out2, err := svc.GetOutgoingLinksForPage(newFixturePageID(pageAID))
		Expect(err).NotTo(HaveOccurred())
		Expect(out2).To(matchOutgoingResult(1,
			matchResolvedOutgoingResultItem(newFixturePageID(pageAID), newFixturePageID(pageBID), tree.RoutePath("/b")),
		))

		bl, err := svc.GetBacklinksForPage(newFixturePageID(pageBID))
		Expect(err).NotTo(HaveOccurred())
		Expect(bl).To(matchBacklinkResult(1,
			matchBacklinkResultItem(newFixturePageID(pageAID), newFixturePageID(pageBID)),
		))

	})
})

var _ = ginkgo.Describe("exact-path link healing", func() {
	ginkgo.It("resolves existing broken links without a full reindex", func() {
		svc, ts, _ := setupLinkService()

		aIDPtr, err := ts.CreateNode("system", nil, "Page A", "a", pageNodeKind())
		Expect(err).NotTo(HaveOccurred())
		pageAID := *aIDPtr

		pageA, err := ts.GetPage(newFixturePageID(pageAID))
		Expect(err).NotTo(HaveOccurred())
		var linkToB = "Link to B: [Go](/b.md)"
		Expect(ts.UpdateNodeUncheckedVersion(newFixtureUserID("system"), newFixturePageID(pageA.ID), pageA.Title, newFixtureSlug(pageA.Slug), &linkToB, false)).To(Succeed())

		Expect(svc.IndexAllPages()).To(Succeed())

		out1, err := svc.GetOutgoingLinksForPage(newFixturePageID(pageAID))
		Expect(err).NotTo(HaveOccurred())
		Expect(out1).To(matchOutgoingResult(1,
			matchBrokenOutgoingResultItem(newFixturePageID(pageAID), tree.RoutePath("/b")),
		))

		bIDPtr, err := ts.CreateNode("system", nil, "Page B", "b", pageNodeKind())
		Expect(err).NotTo(HaveOccurred())
		pageBID := *bIDPtr

		pageB, err := ts.GetPage(newFixturePageID(pageBID))
		Expect(err).NotTo(HaveOccurred())

		Expect(svc.HealLinksForExactPath(pageB)).To(Succeed())

		out2, err := svc.GetOutgoingLinksForPage(newFixturePageID(pageAID))
		Expect(err).NotTo(HaveOccurred())
		Expect(out2).To(matchOutgoingResult(1,
			matchResolvedOutgoingResultItem(newFixturePageID(pageAID), newFixturePageID(pageBID), tree.RoutePath("/b")),
		))

		bl, err := svc.GetBacklinksForPage(newFixturePageID(pageBID))
		Expect(err).NotTo(HaveOccurred())
		Expect(bl).To(matchBacklinkResult(1,
			matchBacklinkResultItem(newFixturePageID(pageAID), newFixturePageID(pageBID)),
		))

	})
})

var _ = ginkgo.Describe("broken incoming link queries", func() {
	ginkgo.It("returns broken links that target the requested path", func() {
		svc, ts, store := setupLinkService()

		// Create three pages: A, B, C
		aIDPtr, err := ts.CreateNode("system", nil, "Page A", "a", pageNodeKind())
		Expect(err).NotTo(HaveOccurred())
		pageAID := *aIDPtr

		bIDPtr, err := ts.CreateNode("system", nil, "Page B", "b", pageNodeKind())
		Expect(err).NotTo(HaveOccurred())
		pageBID := *bIDPtr

		cIDPtr, err := ts.CreateNode("system", nil, "Page C", "c", pageNodeKind())
		Expect(err).NotTo(HaveOccurred())
		pageCID := *cIDPtr

		// Update A and B to link to a non-existent page "/nonexistent"
		pageA, err := ts.GetPage(newFixturePageID(pageAID))
		Expect(err).NotTo(HaveOccurred())
		var linkToMissing = "Link: [Missing](/nonexistent)"
		Expect(ts.UpdateNodeUncheckedVersion(newFixtureUserID("system"), newFixturePageID(pageA.ID), pageA.Title, newFixtureSlug(pageA.Slug), &linkToMissing, false)).To(Succeed())

		pageB, err := ts.GetPage(newFixturePageID(pageBID))
		Expect(err).NotTo(HaveOccurred())
		Expect(ts.UpdateNodeUncheckedVersion(newFixtureUserID("system"), newFixturePageID(pageB.ID), pageB.Title, newFixtureSlug(pageB.Slug), &linkToMissing, false)).To(Succeed())

		// Page C links to a different broken page
		pageC, err := ts.GetPage(newFixturePageID(pageCID))
		Expect(err).NotTo(HaveOccurred())
		var linkToOther = "Link: [Other](/other-missing)"
		Expect(ts.UpdateNodeUncheckedVersion(newFixtureUserID("system"), newFixturePageID(pageC.ID), pageC.Title, newFixtureSlug(pageC.Slug), &linkToOther, false)).To(Succeed())

		// Index all pages to create broken links
		Expect(svc.IndexAllPages()).To(Succeed())

		// Test: GetBrokenIncomingForPath should return broken links for "/nonexistent"
		brokenLinks, err := store.GetBrokenIncomingForPath("/nonexistent")
		Expect(err).NotTo(HaveOccurred())

		Expect(brokenLinks).To(ConsistOf(
			matchBrokenBacklink(newFixturePageID(pageAID)),
			matchBrokenBacklink(newFixturePageID(pageBID)),
		))

	})
})

var _ = ginkgo.Describe("broken incoming link filtering", func() {
	ginkgo.It("returns only broken links for the requested path", func() {
		svc, ts, store := setupLinkService()

		aIDPtr, err := ts.CreateNode("system", nil, "Page A", "a", pageNodeKind())
		Expect(err).NotTo(HaveOccurred())
		pageAID := *aIDPtr

		bIDPtr, err := ts.CreateNode("system", nil, "Page B", "b", pageNodeKind())
		Expect(err).NotTo(HaveOccurred())
		pageBID := *bIDPtr

		// Page A links to "/missing1"
		pageA, err := ts.GetPage(newFixturePageID(pageAID))
		Expect(err).NotTo(HaveOccurred())
		var linkToMissing1 = "Link: [Missing1](/missing1)"
		Expect(ts.UpdateNodeUncheckedVersion(newFixtureUserID("system"), newFixturePageID(pageA.ID), pageA.Title, newFixtureSlug(pageA.Slug), &linkToMissing1, false)).To(Succeed())

		// Page B links to "/missing2"
		pageB, err := ts.GetPage(newFixturePageID(pageBID))
		Expect(err).NotTo(HaveOccurred())
		var linkToMissing2 = "Link: [Missing2](/missing2)"
		Expect(ts.UpdateNodeUncheckedVersion(newFixtureUserID("system"), newFixturePageID(pageB.ID), pageB.Title, newFixtureSlug(pageB.Slug), &linkToMissing2, false)).To(Succeed())

		Expect(svc.IndexAllPages()).To(Succeed())

		// Test: Should only return broken links for "/missing1"
		broken1, err := store.GetBrokenIncomingForPath("/missing1")
		Expect(err).NotTo(HaveOccurred())

		Expect(broken1).To(ConsistOf(matchBrokenBacklink(newFixturePageID(pageAID))))

		// Test: Should only return broken links for "/missing2"
		broken2, err := store.GetBrokenIncomingForPath("/missing2")
		Expect(err).NotTo(HaveOccurred())

		Expect(broken2).To(ConsistOf(matchBrokenBacklink(newFixturePageID(pageBID))))

	})
})

var _ = ginkgo.Describe("broken incoming link queries without matching links", func() {
	ginkgo.It("returns empty results for healthy and unused paths", func() {
		svc, ts, store := setupLinkService()

		aIDPtr, err := ts.CreateNode("system", nil, "Page A", "a", pageNodeKind())
		Expect(err).NotTo(HaveOccurred())
		pageAID := *aIDPtr

		_, err = ts.CreateNode("system", nil, "Page B", "b", pageNodeKind())
		Expect(err).NotTo(HaveOccurred())

		// Page A links to existing Page B (not broken)
		pageA, err := ts.GetPage(newFixturePageID(pageAID))
		Expect(err).NotTo(HaveOccurred())
		var linkToB = "Link: [To B](/b.md)"
		Expect(ts.UpdateNodeUncheckedVersion(newFixtureUserID("system"), newFixturePageID(pageA.ID), pageA.Title, newFixtureSlug(pageA.Slug), &linkToB, false)).To(Succeed())

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

var _ = ginkgo.Describe("broken incoming link ordering", func() {
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
			Expect(ts.UpdateNodeUncheckedVersion(newFixtureUserID("system"), newFixturePageID(page.ID), page.Title, newFixtureSlug(page.Slug), &linkToMissing, false)).To(Succeed())
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

var _ = ginkgo.Describe("broken incoming link healing", func() {
	ginkgo.It("removes healed links from broken incoming queries", func() {
		svc, ts, store := setupLinkService()

		// Create Page A that links to a non-existent page
		aIDPtr, err := ts.CreateNode("system", nil, "Page A", "a", pageNodeKind())
		Expect(err).NotTo(HaveOccurred())
		pageAID := *aIDPtr

		pageA, err := ts.GetPage(newFixturePageID(pageAID))
		Expect(err).NotTo(HaveOccurred())
		var linkToB = "Link: [To B](/b.md)"
		Expect(ts.UpdateNodeUncheckedVersion(newFixtureUserID("system"), newFixturePageID(pageA.ID), pageA.Title, newFixtureSlug(pageA.Slug), &linkToB, false)).To(Succeed())

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

		pageB, err := ts.GetPage(newFixturePageID(pageBID))
		Expect(err).NotTo(HaveOccurred())
		var contentB = "# Page B"
		Expect(ts.UpdateNodeUncheckedVersion(newFixtureUserID("system"), newFixturePageID(pageB.ID), pageB.Title, newFixtureSlug(pageB.Slug), &contentB, false)).To(Succeed())

		// Use HealLinksForExactPath to heal the broken link
		Expect(svc.HealLinksForExactPath(pageB)).To(Succeed())

		// Verify the link is no longer broken
		brokenAfter, err := store.GetBrokenIncomingForPath("/b")
		Expect(err).NotTo(HaveOccurred())
		Expect(brokenAfter).To(HaveLen(0))

		// Verify the link still exists but is not broken
		backlinks, err := store.GetBacklinksForPage(newFixturePageID(pageBID))
		Expect(err).NotTo(HaveOccurred())
		Expect(backlinks).To(ConsistOf(matchBacklinkResultItem(newFixturePageID(pageAID), newFixturePageID(pageBID))))

	})
})

var _ = ginkgo.Describe("extensionless link healing", func() {
	ginkgo.It("homes extensionless links to matching sections instead of page twins", func() {
		svc, ts, _ := setupLinkService()

		sourceIDPtr, err := ts.CreateNode("system", nil, "Source", "source", pageNodeKind())
		Expect(err).NotTo(HaveOccurred())
		sourceID := *sourceIDPtr
		source, err := ts.GetPage(newFixturePageID(sourceID))
		Expect(err).NotTo(HaveOccurred())
		sourceContent := "Link: [Target](/x)"
		Expect(ts.UpdateNodeUncheckedVersion(newFixtureUserID("system"), newFixturePageID(source.ID), source.Title, newFixtureSlug(source.Slug), &sourceContent, false)).To(Succeed())
		source, err = ts.GetPage(newFixturePageID(sourceID))
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

var _ = ginkgo.Describe("batch link updates and healing", func() {
	ginkgo.It("updates and heals multiple source pages in one pass", func() {
		svc, ts, _ := setupLinkService()

		aIDPtr, err := ts.CreateNode("system", nil, "Page A", "a", pageNodeKind())
		Expect(err).NotTo(HaveOccurred())
		pageAID := *aIDPtr

		cIDPtr, err := ts.CreateNode("system", nil, "Page C", "c", pageNodeKind())
		Expect(err).NotTo(HaveOccurred())
		pageCID := *cIDPtr

		pageA, err := ts.GetPage(newFixturePageID(pageAID))
		Expect(err).NotTo(HaveOccurred())
		contentA := "Link: [B](/b.md)"
		Expect(ts.UpdateNodeUncheckedVersion(newFixtureUserID("system"), newFixturePageID(pageA.ID), pageA.Title, newFixtureSlug(pageA.Slug), &contentA, false)).To(Succeed())

		pageC, err := ts.GetPage(newFixturePageID(pageCID))
		Expect(err).NotTo(HaveOccurred())
		contentC := "Link: [D](/d.md)"
		Expect(ts.UpdateNodeUncheckedVersion(newFixtureUserID("system"), newFixturePageID(pageC.ID), pageC.Title, newFixtureSlug(pageC.Slug), &contentC, false)).To(Succeed())

		Expect(svc.IndexAllPages()).To(Succeed())

		bIDPtr, err := ts.CreateNode("system", nil, "Page B", "b", pageNodeKind())
		Expect(err).NotTo(HaveOccurred())
		pageB, err := ts.GetPage(*bIDPtr)
		Expect(err).NotTo(HaveOccurred())

		dIDPtr, err := ts.CreateNode("system", nil, "Page D", "d", pageNodeKind())
		Expect(err).NotTo(HaveOccurred())
		pageD, err := ts.GetPage(*dIDPtr)
		Expect(err).NotTo(HaveOccurred())

		Expect(svc.UpdateLinksAndHealForPages([]*tree.Page{pageB, pageD})).To(Succeed())

		outA, err := svc.GetOutgoingLinksForPage(newFixturePageID(pageAID))
		Expect(err).NotTo(HaveOccurred())
		Expect(outA).To(matchOutgoingResult(1,
			matchResolvedOutgoingResultItem(newFixturePageID(pageAID), pageB.ID, tree.RoutePath("/b")),
		))

		outC, err := svc.GetOutgoingLinksForPage(newFixturePageID(pageCID))
		Expect(err).NotTo(HaveOccurred())
		Expect(outC).To(matchOutgoingResult(1,
			matchResolvedOutgoingResultItem(newFixturePageID(pageCID), pageD.ID, tree.RoutePath("/d")),
		))

	})
})

var _ = ginkgo.Describe("batch healing for extensionless links", func() {
	ginkgo.It("leaves extensionless links broken until a matching section exists", func() {
		svc, ts, _ := setupLinkService()

		sourceIDPtr, err := ts.CreateNode("system", nil, "Source", "source", pageNodeKind())
		Expect(err).NotTo(HaveOccurred())
		source, err := ts.GetPage(*sourceIDPtr)
		Expect(err).NotTo(HaveOccurred())
		content := "[Legacy](/target)"
		Expect(ts.UpdateNodeUncheckedVersion(newFixtureUserID("system"), newFixturePageID(source.ID), source.Title, newFixtureSlug(source.Slug), &content, false)).To(Succeed())

		Expect(svc.IndexAllPages()).To(Succeed())

		pageIDPtr, err := ts.CreateNode("system", nil, "Target", "target", pageNodeKind())
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
			matchBrokenOutgoingResultItem(source.ID, tree.RoutePath("/target")),
		))

		sectionIDPtr, err := ts.CreateNode("system", nil, "Target Section", "target", sectionNodeKind())
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

var _ = ginkgo.Describe("root-backed batch link updates", func() {
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
		sourceA, err := ts.GetPage("source-a")
		Expect(err).NotTo(HaveOccurred())
		sourceB, err := ts.GetPage("source-b")
		Expect(err).NotTo(HaveOccurred())
		calls := countMarkdownRootIndexBuilds(markdownlinks.NewIndex([]markdownlinks.Entry{
			{Kind: markdownlinks.EntryKindPage, Path: "source-a.md"},
			{Kind: markdownlinks.EntryKindPage, Path: "source-b.md"},
			{Kind: markdownlinks.EntryKindPage, Path: "target.md"},
		}))

		Expect(svc.UpdateLinksAndHealForPages([]*tree.Page{sourceA, sourceB})).To(Succeed())

		Expect(*calls).To(Equal(1))

	})
})

var _ = ginkgo.Describe("empty batch link updates", func() {
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

var _ = ginkgo.Describe("root-backed rewritten link updates", func() {
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
		sourceA, err := ts.GetPage("source-a")
		Expect(err).NotTo(HaveOccurred())
		sourceB, err := ts.GetPage("source-b")
		Expect(err).NotTo(HaveOccurred())
		calls := countMarkdownRootIndexBuilds(markdownlinks.NewIndex([]markdownlinks.Entry{
			{Kind: markdownlinks.EntryKindPage, Path: "source-a.md"},
			{Kind: markdownlinks.EntryKindPage, Path: "source-b.md"},
			{Kind: markdownlinks.EntryKindPage, Path: "old-target.md"},
			{Kind: markdownlinks.EntryKindPage, Path: "new-target.md"},
		}))

		Expect(svc.UpdateRewrittenLinksAndHealForPages([]*tree.Page{sourceA, sourceB}, []RewriteRule{{
			OldPath: "/old-target",
			NewPath: "/new-target",
			Kind:    "page",
		}})).To(Succeed())

		Expect(*calls).To(Equal(1))

	})
})

var _ = ginkgo.Describe("empty rewritten link updates", func() {
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

var _ = ginkgo.Describe("source page reindexing during batch healing", func() {
	ginkgo.It("moves backlinks from old targets to new targets after source content changes", func() {
		svc, ts, _ := setupLinkService()

		sourceIDPtr, err := ts.CreateNode("system", nil, "Source", "source", pageNodeKind())
		Expect(err).NotTo(HaveOccurred())
		sourceID := *sourceIDPtr

		oldTargetIDPtr, err := ts.CreateNode("system", nil, "Old Target", "old-target", pageNodeKind())
		Expect(err).NotTo(HaveOccurred())
		oldTargetID := *oldTargetIDPtr

		source, err := ts.GetPage(newFixturePageID(sourceID))
		Expect(err).NotTo(HaveOccurred())
		oldContent := "Link: [Old](/old-target.md)"
		Expect(ts.UpdateNodeUncheckedVersion(newFixtureUserID("system"), newFixturePageID(source.ID), source.Title, newFixtureSlug(source.Slug), &oldContent, false)).To(Succeed())

		Expect(svc.IndexAllPages()).To(Succeed())

		newTargetIDPtr, err := ts.CreateNode("system", nil, "New Target", "new-target", pageNodeKind())
		Expect(err).NotTo(HaveOccurred())
		newTargetID := *newTargetIDPtr

		updatedContent := "Link: [New](/new-target.md)"
		Expect(ts.UpdateNodeUncheckedVersion(newFixtureUserID("system"), newFixturePageID(source.ID), source.Title, newFixtureSlug(source.Slug), &updatedContent, false)).To(Succeed())

		updatedSource, err := ts.GetPage(newFixturePageID(sourceID))
		Expect(err).NotTo(HaveOccurred())

		Expect(svc.UpdateLinksAndHealForPages([]*tree.Page{updatedSource})).To(Succeed())

		oldBacklinks, err := svc.GetBacklinksForPage(newFixturePageID(oldTargetID))
		Expect(err).NotTo(HaveOccurred())
		Expect(oldBacklinks).To(matchBacklinkResult(0))

		newBacklinks, err := svc.GetBacklinksForPage(newFixturePageID(newTargetID))
		Expect(err).NotTo(HaveOccurred())
		Expect(newBacklinks).To(matchBacklinkResult(1,
			matchBacklinkResultItem(newFixturePageID(sourceID), newFixturePageID(newTargetID)),
		))

	})
})
