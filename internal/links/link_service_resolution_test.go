package links

import (
	"path/filepath"

	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/perber/wiki/internal/core/tree"
)

var _ = ginkgo.Describe("target link resolution", ginkgo.Label("integration"), func() {
	ginkgo.It("resolves relative page links from the current page directory", func() {
		ts, page1ID, page2ID := setupTreeForLinksTest()

		// current page: docs/page1
		page1, err := ts.GetPage(page1ID)
		Expect(err).NotTo(HaveOccurred())
		currentPath := page1.CalculatePath() // should be "docs/page1"

		// we want to link from page1 to page2 using a relative link
		links := []string{"./page2.md"}

		targets := resolveTargetLinks(ts, tree.RoutePathFromString(currentPath), links)

		Expect(targets).To(ConsistOf(matchResolvedTargetLink(page2ID, newFixtureRoutePath("/docs/page2"))))
	})

	// - Canonical .md page link indexes as outgoing link
	ginkgo.It("resolves canonical relative page markdown links from the source file directory", func() {
		ts, page1ID, page2ID := setupTreeForLinksTest()

		page1, err := ts.GetPage(page1ID)
		Expect(err).NotTo(HaveOccurred())

		targets := resolveTargetLinks(ts, page1.CalculateRoutePath(), []string{"./page2.md"})

		Expect(targets).To(ConsistOf(matchResolvedTargetLink(page2ID, newFixtureRoutePath("/docs/page2"))))
	})

	// - Relative section links are resolved from the source file directory
	ginkgo.It("resolves relative section links from the source file directory", func() {
		storageDir := linksTempDir()
		ts := tree.NewTreeService(storageDir)
		Expect(ts.LoadTree()).To(Succeed())

		docsID, err := ts.CreateNode(newFixtureUserID("system"), nil, "Docs", newFixtureSlug("docs"), sectionNodeKind())
		Expect(err).NotTo(HaveOccurred())
		aID, err := ts.CreateNode(newFixtureUserID("system"), docsID, "A", newFixtureSlug("a"), sectionNodeKind())
		Expect(err).NotTo(HaveOccurred())
		currentID, err := ts.CreateNode(newFixtureUserID("system"), aID, "Current", newFixtureSlug("current"), pageNodeKind())
		Expect(err).NotTo(HaveOccurred())
		bID, err := ts.CreateNode(newFixtureUserID("system"), docsID, "B", newFixtureSlug("b"), sectionNodeKind())
		Expect(err).NotTo(HaveOccurred())
		current, err := ts.GetPage(*currentID)
		Expect(err).NotTo(HaveOccurred())

		targets := resolveTargetLinks(ts, current.CalculateRoutePath(), []string{"../b"})

		Expect(targets).To(ConsistOf(matchResolvedTargetLink(*bID, newFixtureRoutePath("/docs/b"))))
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
		source, err := ts.GetPage(newFixturePageID("page-source"))
		Expect(err).NotTo(HaveOccurred())
		guides, err := ts.GetPage(newFixturePageID("section-guides"))
		Expect(err).NotTo(HaveOccurred())

		targets := resolveTargetLinks(ts, source.CalculateRoutePath(), extractLinksFromMarkdown(source.Content))

		Expect(targets).To(ConsistOf(matchResolvedTargetLink(guides.ID, newFixtureRoutePath("/guides"))))
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
		source, err := ts.GetPage(newFixturePageID("page-a"))
		Expect(err).NotTo(HaveOccurred())

		targets := resolveTargetLinks(ts, source.CalculateRoutePath(), extractLinksFromMarkdown(source.Content))

		Expect(targets).To(ConsistOf(matchResolvedTargetLink(newFixturePageID("sync-section"), newFixtureRoutePath("/docs/sync"))))
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
		source, err := ts.GetPage(newFixturePageID("sync-section"))
		Expect(err).NotTo(HaveOccurred())

		targets := resolveTargetLinksForSourceKind(ts, source.CalculateRoutePath(), source.Kind, extractLinksFromMarkdown(source.Content))

		Expect(targets).To(ConsistOf(matchResolvedTargetLink(newFixturePageID("sync-child"), newFixtureRoutePath("/docs/sync/child"))))
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
		source, err := ts.GetPage(newFixturePageID("sync-page"))
		Expect(err).NotTo(HaveOccurred())

		targets := resolveTargetLinksForSourceKind(ts, source.CalculateRoutePath(), source.Kind, extractLinksFromMarkdown(source.Content))

		Expect(targets).To(ConsistOf(matchResolvedTargetLink(newFixturePageID("sync-sibling"), newFixtureRoutePath("/docs/sibling"))))
	})

	// - Broken canonical .md page link is reported as broken
	ginkgo.It("returns broken targets with normalized paths for missing destinations", func() {
		ts, page1ID, _ := setupTreeForLinksTest()

		page1, err := ts.GetPage(page1ID)
		Expect(err).NotTo(HaveOccurred())
		currentPath := page1.CalculatePath()

		links := []string{
			"./does-not-exist",
			"/docs/unknown",
		}

		targets := resolveTargetLinks(ts, tree.RoutePathFromString(currentPath), links)

		Expect(targets).To(ConsistOf(
			matchBrokenTargetLink(newFixtureRoutePath("/docs/does-not-exist")),
			matchBrokenTargetLink(newFixtureRoutePath("/docs/unknown")),
		))
	})

	ginkgo.It("ignores asset destinations during target resolution", func() {
		ts, page1ID, _ := setupTreeForLinksTest()

		page1, err := ts.GetPage(page1ID)
		Expect(err).NotTo(HaveOccurred())

		targets := resolveTargetLinks(ts, page1.CalculateRoutePath(), []string{
			"/assets/abc/manual.pdf",
			"assets/abc/picture.png",
		})

		Expect(targets).To(BeEmpty())
	})
})
