package links

import (
	ginkgo "github.com/onsi/ginkgo/v2"

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

func writeLinkServiceMarkdown(t linksTestT, filePath string, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(filePath), 0o755); err != nil {
		t.Fatalf("MkdirAll %s: %v", filepath.Dir(filePath), err)
	}
	if err := os.WriteFile(filePath, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile %s: %v", filePath, err)
	}
}

func countMarkdownRootIndexBuilds(t linksTestT, index *markdownlinks.Index) *int {
	t.Helper()
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

var _ = ginkgo.Describe("TestExtractLinksFromMarkdown_FiltersExternalAndNormalizes", func() {
	ginkgo.It("preserves behavior", func() {
		t := ginkgo.GinkgoT()
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

		if len(links) != len(want) {
			t.Fatalf("expected %d links, got %d: %#v", len(want), len(links), links)
		}

		for i, w := range want {
			if links[i] != w {
				t.Errorf("link[%d] = %q, want %q", i, links[i], w)
			}
		}

	})
})

var _ = ginkgo.Describe("TestExtractLinksFromMarkdown_IgnoresExternalLinksCaseInsensitive", func() {
	ginkgo.It("preserves behavior", func() {
		t := ginkgo.GinkgoT()
		md := `
[HTTPS](HTTPS://example.com)
[Mail](Mailto:test@example.com)
[Anchor](#intro)
[Wiki](/docs/page1)
`

		links := extractLinksFromMarkdown(md)

		if len(links) != 1 {
			t.Fatalf("expected 1 wiki link, got %d: %#v", len(links), links)
		}
		if links[0] != "/docs/page1" {
			t.Fatalf("link[0] = %q, want %q", links[0], "/docs/page1")
		}

	})
})

var _ = ginkgo.Describe("TestLinkService_GetRefactorMatchesForPrefixAndKindAcceptsRoutePath", func() {
	ginkgo.It("preserves behavior", func() {
		t := ginkgo.GinkgoT()
		store, err := NewLinksStore(t.TempDir())
		if err != nil {
			t.Fatalf("NewLinksStore failed: %v", err)
		}

		if err := store.AddLinks(newFixturePageID("source"), "Source", []TargetLink{
			{TargetPageID: newFixturePageID("target"), TargetPagePath: "/docs/guide", TargetKind: string(tree.NodeKindSection)},
			{TargetPageID: newFixturePageID("child"), TargetPagePath: "/docs/guide/child", TargetKind: string(tree.NodeKindSection)},
		}); err != nil {
			t.Fatalf("AddLinks failed: %v", err)
		}

		service := NewLinkService(t.TempDir(), nil, store)
		matches, err := service.GetRefactorMatchesForPrefixAndKind(tree.RoutePath("docs/guide"), tree.NodeKindSection)
		if err != nil {
			t.Fatalf("GetRefactorMatchesForPrefixAndKind failed: %v", err)
		}
		if len(matches) != 2 {
			t.Fatalf("expected exact and child route matches, got %d: %#v", len(matches), matches)
		}

	})
})

// - Assets are not coerced
var _ = ginkgo.Describe("TestExtractLinksFromMarkdown_IgnoresAssetDestinations", func() {
	ginkgo.It("preserves behavior", func() {
		t := ginkgo.GinkgoT()
		md := `
Asset absolute: [File](/assets/abc/manual.pdf)
Asset relative: [Image](assets/abc/picture.png)
Internal: [Page](/docs/page1)
`

		links := extractLinksFromMarkdown(md)

		want := []string{"/docs/page1"}
		if len(links) != len(want) {
			t.Fatalf("expected %d links, got %d: %#v", len(want), len(links), links)
		}
		for i, w := range want {
			if links[i] != w {
				t.Fatalf("link[%d] = %q, want %q", i, links[i], w)
			}
		}

	})
})

// - Image links remain governed by existing image and asset validation
var _ = ginkgo.Describe("TestExtractLinksFromMarkdown_IgnoresImageLinksToPageDestinations", func() {
	ginkgo.It("preserves behavior", func() {
		t := ginkgo.GinkgoT()
		md := `
Image page path: ![Alt](/docs/b.md)
Normal page link: [Page](/docs/b.md)
`

		links := extractLinksFromMarkdown(md)

		want := []string{"/docs/b.md"}
		if len(links) != len(want) {
			t.Fatalf("expected %d wiki links, got %d: %#v", len(want), len(links), links)
		}
		for i, w := range want {
			if links[i] != w {
				t.Fatalf("link[%d] = %q, want %q", i, links[i], w)
			}
		}

	})
})

var _ = ginkgo.Describe("TestExtractLinksFromMarkdown_IgnoresMixedCaseExternalSchemes", func() {
	ginkgo.It("preserves behavior", func() {
		t := ginkgo.GinkgoT()
		md := `
Uppercase HTTPS: [A](HTTPS://example.com)
Uppercase HTTP: [B](HTTP://example.com)
Uppercase MAILTO: [C](MAILTO:foo@bar.com)
Mixed case Https: [D](Https://example.com)
Mixed case Mailto: [E](Mailto:foo@bar.com)
Internal: [Page](/docs/page1)
`

		links := extractLinksFromMarkdown(md)

		want := []string{"/docs/page1"}
		if len(links) != len(want) {
			t.Fatalf("expected %d links, got %d: %#v", len(want), len(links), links)
		}
		for i, w := range want {
			if links[i] != w {
				t.Fatalf("link[%d] = %q, want %q", i, links[i], w)
			}
		}

	})
})

// helper to create a small tree structure:
// root
//
//	└─ docs
//	     ├─ page1
//	     └─ page2
func setupTreeForLinksTest(t linksTestT) (*tree.TreeService, tree.PageID, tree.PageID) {
	t.Helper()

	storageDir := t.TempDir()
	ts := tree.NewTreeService(storageDir)

	if err := ts.LoadTree(); err != nil {
		t.Fatalf("LoadTree failed: %v", err)
	}

	// create "docs" under root
	docsIDPtr, err := ts.CreateNode("system", nil, "Docs", "docs", pageNodeKind())
	if err != nil {
		t.Fatalf("CreatePage docs failed: %v", err)
	}
	docsID := *docsIDPtr

	// create "page1" and "page2" under docs
	page1IDPtr, err := ts.CreateNode("system", &docsID, "Page 1", "page1", pageNodeKind())
	if err != nil {
		t.Fatalf("CreatePage page1 failed: %v", err)
	}
	page2IDPtr, err := ts.CreateNode("system", &docsID, "Page 2", "page2", pageNodeKind())
	if err != nil {
		t.Fatalf("CreatePage page2 failed: %v", err)
	}

	return ts, *page1IDPtr, *page2IDPtr
}

var _ = ginkgo.Describe("TestResolveTargetLinks_FindsExistingTargets", func() {
	ginkgo.It("preserves behavior", func() {
		t := ginkgo.GinkgoT()
		ts, page1ID, page2ID := setupTreeForLinksTest(t)

		// current page: docs/page1
		page1, err := ts.GetPage(newFixturePageID(page1ID))
		if err != nil {
			t.Fatalf("GetPage(page1) failed: %v", err)
		}
		currentPath := page1.CalculatePath() // should be "docs/page1"

		// we want to link from page1 to page2 using a relative link
		links := []string{"./page2.md"}

		targets := resolveTargetLinks(ts, tree.RoutePathFromString(currentPath), links)

		if len(targets) != 1 {
			t.Fatalf("expected 1 target link, got %d: %#v", len(targets), targets)
		}

		got := targets[0]
		if got.TargetPageID != newFixturePageID(page2ID) {
			t.Errorf("TargetPageID = %q, want %q", got.TargetPageID, page2ID)
		}
		if got.TargetPagePath == "" {
			t.Errorf("TargetPagePath should not be empty")
		}

	})
})

// - Canonical .md page link indexes as outgoing link
var _ = ginkgo.Describe("TestResolveTargetLinks_ResolvesCanonicalRelativePageMdFromSourceFileDirectory", func() {
	ginkgo.It("preserves behavior", func() {
		t := ginkgo.GinkgoT()
		ts, page1ID, page2ID := setupTreeForLinksTest(t)

		page1, err := ts.GetPage(newFixturePageID(page1ID))
		if err != nil {
			t.Fatalf("GetPage(page1) failed: %v", err)
		}

		targets := resolveTargetLinks(ts, page1.CalculateRoutePath(), []string{"./page2.md"})

		if len(targets) != 1 {
			t.Fatalf("expected 1 target link, got %d: %#v", len(targets), targets)
		}
		got := targets[0]
		if got.Broken {
			t.Fatalf("target = %#v, want resolved canonical .md page link", got)
		}
		if got.TargetPageID != newFixturePageID(page2ID) {
			t.Fatalf("TargetPageID = %q, want %q", got.TargetPageID, page2ID)
		}
		if got.TargetPagePath != "/docs/page2" {
			t.Fatalf("TargetPagePath = %q, want /docs/page2", got.TargetPagePath)
		}

	})
})

// - Relative section links are resolved from the source file directory
var _ = ginkgo.Describe("TestResolveTargetLinks_ResolvesRelativeSectionLinkFromSourceFileDirectory", func() {
	ginkgo.It("preserves behavior", func() {
		t := ginkgo.GinkgoT()
		storageDir := t.TempDir()
		ts := tree.NewTreeService(storageDir)
		if err := ts.LoadTree(); err != nil {
			t.Fatalf("LoadTree failed: %v", err)
		}

		docsID, err := ts.CreateNode("system", nil, "Docs", "docs", sectionNodeKind())
		if err != nil {
			t.Fatalf("CreateNode docs failed: %v", err)
		}
		aID, err := ts.CreateNode("system", docsID, "A", "a", sectionNodeKind())
		if err != nil {
			t.Fatalf("CreateNode a failed: %v", err)
		}
		currentID, err := ts.CreateNode("system", aID, "Current", "current", pageNodeKind())
		if err != nil {
			t.Fatalf("CreateNode current failed: %v", err)
		}
		bID, err := ts.CreateNode("system", docsID, "B", "b", sectionNodeKind())
		if err != nil {
			t.Fatalf("CreateNode b failed: %v", err)
		}
		current, err := ts.GetPage(*currentID)
		if err != nil {
			t.Fatalf("GetPage current failed: %v", err)
		}

		targets := resolveTargetLinks(ts, current.CalculateRoutePath(), []string{"../b"})

		if len(targets) != 1 {
			t.Fatalf("expected 1 target link, got %d: %#v", len(targets), targets)
		}
		if targets[0].Broken {
			t.Fatalf("target = %#v, want resolved section link", targets[0])
		}
		if targets[0].TargetPageID != *bID {
			t.Fatalf("TargetPageID = %q, want %q", targets[0].TargetPageID, bID.String())
		}
		if targets[0].TargetPagePath != "/docs/b" {
			t.Fatalf("TargetPagePath = %q, want /docs/b", targets[0].TargetPagePath)
		}

	})
})

var _ = ginkgo.Describe("TestResolveTargetLinks_ResolvesReadmeFallbackSectionDefaultFile", func() {
	ginkgo.It("preserves behavior", func() {
		t := ginkgo.GinkgoT()
		dataDir := t.TempDir()
		rootDir := filepath.Join(t.TempDir(), "workspace")
		writeLinkServiceMarkdown(t, filepath.Join(rootDir, "source.md"), `---
leafwiki_id: page-source
leafwiki_title: Source
---
# Source

[Guides](/guides/README.md)
`)
		writeLinkServiceMarkdown(t, filepath.Join(rootDir, "guides", "README.md"), `---
leafwiki_id: section-guides
leafwiki_title: Guides
---
# Guides
`)

		ts := tree.NewTreeServiceWithOptions(tree.TreeOptions{DataDir: dataDir, RootDir: rootDir})
		if err := ts.LoadTree(); err != nil {
			t.Fatalf("LoadTree failed: %v", err)
		}
		source, err := ts.GetPage("page-source")
		if err != nil {
			t.Fatalf("GetPage source failed: %v", err)
		}
		guides, err := ts.GetPage("section-guides")
		if err != nil {
			t.Fatalf("GetPage guides failed: %v", err)
		}

		targets := resolveTargetLinks(ts, source.CalculateRoutePath(), extractLinksFromMarkdown(source.Content))

		if len(targets) != 1 {
			t.Fatalf("expected 1 target link, got %d: %#v", len(targets), targets)
		}
		if targets[0].Broken {
			t.Fatalf("target = %#v, want README fallback section link resolved", targets[0])
		}
		if targets[0].TargetPageID != guides.ID {
			t.Fatalf("TargetPageID = %q, want %q", targets[0].TargetPageID, guides.ID)
		}
		if targets[0].TargetPagePath != "/guides" {
			t.Fatalf("TargetPagePath = %q, want /guides", targets[0].TargetPagePath)
		}

	})
})

// - Canonical section link indexes as outgoing link
var _ = ginkgo.Describe("TestResolveTargetLinks_ResolvesCanonicalSectionLinkForSameBasenameTwin", func() {
	ginkgo.It("preserves behavior", func() {
		t := ginkgo.GinkgoT()
		dataDir := t.TempDir()
		rootDir := filepath.Join(t.TempDir(), "workspace")
		writeLinkServiceMarkdown(t, filepath.Join(rootDir, "docs", "a.md"), `---
leafwiki_id: page-a
leafwiki_title: Page A
---
# Page A

[Sync](/docs/sync)
`)
		writeLinkServiceMarkdown(t, filepath.Join(rootDir, "docs", "sync.md"), `---
leafwiki_id: sync-page
leafwiki_title: Sync Page
---
# Sync Page
`)
		writeLinkServiceMarkdown(t, filepath.Join(rootDir, "docs", "sync", "index.md"), `---
leafwiki_id: sync-section
leafwiki_title: Sync Section
---
# Sync Section
`)

		ts := tree.NewTreeServiceWithOptions(tree.TreeOptions{DataDir: dataDir, RootDir: rootDir})
		if err := ts.LoadTree(); err != nil {
			t.Fatalf("LoadTree failed: %v", err)
		}
		source, err := ts.GetPage("page-a")
		if err != nil {
			t.Fatalf("GetPage source failed: %v", err)
		}

		targets := resolveTargetLinks(ts, source.CalculateRoutePath(), extractLinksFromMarkdown(source.Content))

		if len(targets) != 1 {
			t.Fatalf("expected 1 target link, got %d: %#v", len(targets), targets)
		}
		if targets[0].Broken {
			t.Fatalf("target = %#v, want canonical section link resolved", targets[0])
		}
		if targets[0].TargetPageID != "sync-section" {
			t.Fatalf("TargetPageID = %q, want sync-section", targets[0].TargetPageID)
		}
		if targets[0].TargetPagePath != "/docs/sync" {
			t.Fatalf("TargetPagePath = %q, want /docs/sync", targets[0].TargetPagePath)
		}

	})
})

var _ = ginkgo.Describe("TestResolveTargetLinks_UsesSectionSourceFileForSameBasenameSection", func() {
	ginkgo.It("preserves behavior", func() {
		t := ginkgo.GinkgoT()
		dataDir := t.TempDir()
		rootDir := filepath.Join(t.TempDir(), "workspace")
		writeLinkServiceMarkdown(t, filepath.Join(rootDir, "docs", "sync.md"), `---
leafwiki_id: sync-page
leafwiki_title: Sync Page
---
# Sync Page

[Wrong](./child.md)
`)
		writeLinkServiceMarkdown(t, filepath.Join(rootDir, "docs", "sync", "index.md"), `---
leafwiki_id: sync-section
leafwiki_title: Sync Section
---
# Sync Section

[Child](./child.md)
`)
		writeLinkServiceMarkdown(t, filepath.Join(rootDir, "docs", "sync", "child.md"), `---
leafwiki_id: sync-child
leafwiki_title: Sync Child
---
# Sync Child
`)

		ts := tree.NewTreeServiceWithOptions(tree.TreeOptions{DataDir: dataDir, RootDir: rootDir})
		if err := ts.LoadTree(); err != nil {
			t.Fatalf("LoadTree failed: %v", err)
		}
		source, err := ts.GetPage("sync-section")
		if err != nil {
			t.Fatalf("GetPage section source failed: %v", err)
		}

		targets := resolveTargetLinksForSourceKind(ts, source.CalculateRoutePath(), source.Kind, extractLinksFromMarkdown(source.Content))

		if len(targets) != 1 {
			t.Fatalf("expected 1 target link, got %d: %#v", len(targets), targets)
		}
		if targets[0].Broken {
			t.Fatalf("target = %#v, want section-relative link resolved", targets[0])
		}
		if targets[0].TargetPageID != "sync-child" {
			t.Fatalf("TargetPageID = %q, want sync-child", targets[0].TargetPageID)
		}
		if targets[0].TargetPagePath != "/docs/sync/child" {
			t.Fatalf("TargetPagePath = %q, want /docs/sync/child", targets[0].TargetPagePath)
		}

	})
})

var _ = ginkgo.Describe("TestResolveTargetLinks_UsesPageSourceFileForSameBasenamePage", func() {
	ginkgo.It("preserves behavior", func() {
		t := ginkgo.GinkgoT()
		dataDir := t.TempDir()
		rootDir := filepath.Join(t.TempDir(), "workspace")
		writeLinkServiceMarkdown(t, filepath.Join(rootDir, "docs", "sync.md"), `---
leafwiki_id: sync-page
leafwiki_title: Sync Page
---
# Sync Page

[Sibling](./sibling.md)
`)
		writeLinkServiceMarkdown(t, filepath.Join(rootDir, "docs", "sibling.md"), `---
leafwiki_id: sync-sibling
leafwiki_title: Sync Sibling
---
# Sync Sibling
`)
		writeLinkServiceMarkdown(t, filepath.Join(rootDir, "docs", "sync", "index.md"), `---
leafwiki_id: sync-section
leafwiki_title: Sync Section
---
# Sync Section
`)
		writeLinkServiceMarkdown(t, filepath.Join(rootDir, "docs", "sync", "sibling.md"), `---
leafwiki_id: nested-sibling
leafwiki_title: Nested Sibling
---
# Nested Sibling
`)

		ts := tree.NewTreeServiceWithOptions(tree.TreeOptions{DataDir: dataDir, RootDir: rootDir})
		if err := ts.LoadTree(); err != nil {
			t.Fatalf("LoadTree failed: %v", err)
		}
		source, err := ts.GetPage("sync-page")
		if err != nil {
			t.Fatalf("GetPage page source failed: %v", err)
		}

		targets := resolveTargetLinksForSourceKind(ts, source.CalculateRoutePath(), source.Kind, extractLinksFromMarkdown(source.Content))

		if len(targets) != 1 {
			t.Fatalf("expected 1 target link, got %d: %#v", len(targets), targets)
		}
		if targets[0].Broken {
			t.Fatalf("target = %#v, want page-relative link resolved", targets[0])
		}
		if targets[0].TargetPageID != "sync-sibling" {
			t.Fatalf("TargetPageID = %q, want sync-sibling", targets[0].TargetPageID)
		}
		if targets[0].TargetPagePath != "/docs/sibling" {
			t.Fatalf("TargetPagePath = %q, want /docs/sibling", targets[0].TargetPagePath)
		}

	})
})

// - Broken canonical .md page link is reported as broken
var _ = ginkgo.Describe("TestResolveTargetLinks_ReturnsBrokenTargetsForNonExisting", func() {
	ginkgo.It("preserves behavior", func() {
		t := ginkgo.GinkgoT()
		ts, page1ID, _ := setupTreeForLinksTest(t)

		page1, err := ts.GetPage(newFixturePageID(page1ID))
		if err != nil {
			t.Fatalf("GetPage(page1) failed: %v", err)
		}
		currentPath := page1.CalculatePath()

		links := []string{
			"./does-not-exist",
			"/docs/unknown",
		}

		targets := resolveTargetLinks(ts, tree.RoutePathFromString(currentPath), links)

		if len(targets) != 2 {
			t.Fatalf("expected 2 target links, got %d: %#v", len(targets), targets)
		}

		if targets[0].Broken != true {
			t.Errorf("targets[0].Broken = %v, want true", targets[0].Broken)
		}
		if targets[0].TargetPageID != "" {
			t.Errorf("targets[0].TargetPageID = %q, want empty", targets[0].TargetPageID)
		}
		if targets[0].TargetPagePath != "/docs/does-not-exist" {
			t.Errorf("targets[0].TargetPagePath = %q, want %q", targets[0].TargetPagePath, "/docs/does-not-exist")
		}

		if targets[1].Broken != true {
			t.Errorf("targets[1].Broken = %v, want true", targets[1].Broken)
		}
		if targets[1].TargetPageID != "" {
			t.Errorf("targets[1].TargetPageID = %q, want empty", targets[1].TargetPageID)
		}
		if targets[1].TargetPagePath != "/docs/unknown" {
			t.Errorf("targets[1].TargetPagePath = %q, want %q", targets[1].TargetPagePath, "/docs/unknown")
		}

	})
})

var _ = ginkgo.Describe("TestResolveTargetLinks_IgnoresAssetDestinations", func() {
	ginkgo.It("preserves behavior", func() {
		t := ginkgo.GinkgoT()
		ts, page1ID, _ := setupTreeForLinksTest(t)

		page1, err := ts.GetPage(newFixturePageID(page1ID))
		if err != nil {
			t.Fatalf("GetPage(page1) failed: %v", err)
		}

		targets := resolveTargetLinks(ts, page1.CalculateRoutePath(), []string{
			"/assets/abc/manual.pdf",
			"assets/abc/picture.png",
		})

		if len(targets) != 0 {
			t.Fatalf("expected asset links to be ignored, got %#v", targets)
		}

	})
})

func setupLinkService(t linksTestT) (*LinkService, *tree.TreeService, *LinksStore) {
	t.Helper()

	dataDir := t.TempDir()

	ts := tree.NewTreeService(dataDir)
	if err := ts.LoadTree(); err != nil {
		t.Fatalf("LoadTree failed: %v", err)
	}

	store, err := NewLinksStore(dataDir)
	if err != nil {
		t.Fatalf("NewLinksStore failed: %v", err)
	}

	svc := NewLinkService(dataDir, ts, store)
	return svc, ts, store
}

func createSimpleLinkedPages(t linksTestT, ts *tree.TreeService) (pageAID, pageBID tree.PageID) {
	t.Helper()

	aIDPtr, err := ts.CreateNode("system", nil, "Page A", "a", pageNodeKind())
	if err != nil {
		t.Fatalf("CreatePage a failed: %v", err)
	}
	pageAID = *aIDPtr

	bIDPtr, err := ts.CreateNode("system", nil, "Page B", "b", pageNodeKind())
	if err != nil {
		t.Fatalf("CreatePage b failed: %v", err)
	}
	pageBID = *bIDPtr

	aPage, err := ts.GetPage(newFixturePageID(pageAID))
	if err != nil {
		t.Fatalf("GetPage a failed: %v", err)
	}
	contentA := "Link to B: [Go to B](/b.md)"
	if err := ts.UpdateNodeUncheckedVersion(newFixtureUserID("system"), newFixturePageID(aPage.ID), aPage.Title, newFixtureSlug(aPage.Slug), &contentA, false); err != nil {
		t.Fatalf("UpdatePage a failed: %v", err)
	}

	bPage, err := ts.GetPage(newFixturePageID(pageBID))
	if err != nil {
		t.Fatalf("GetPage b failed: %v", err)
	}
	contentB := "# Page B\nNo outgoing links."
	if err := ts.UpdateNodeUncheckedVersion(newFixtureUserID("system"), newFixturePageID(bPage.ID), bPage.Title, newFixtureSlug(bPage.Slug), &contentB, false); err != nil {
		t.Fatalf("UpdatePage b failed: %v", err)
	}

	return pageAID, pageBID
}

var _ = ginkgo.Describe("TestLinkService_IndexAllPages_BuildsLinks", func() {
	ginkgo.It("preserves behavior", func() {
		t := ginkgo.GinkgoT()
		svc, ts, _ := setupLinkService(t)
		pageAID, pageBID := createSimpleLinkedPages(t, ts)

		if err := svc.IndexAllPages(); err != nil {
			t.Fatalf("IndexAllPages failed: %v", err)
		}

		data, err := svc.GetBacklinksForPage(newFixturePageID(pageBID))
		if err != nil {
			t.Fatalf("GetBacklinksForPage failed: %v", err)
		}

		if len(data.Backlinks) != 1 {
			t.Fatalf("expected 1 backlink for pageB, got %d: %#v", len(data.Backlinks), data.Backlinks)
		}

		bl := data.Backlinks[0]
		if bl.FromPageID != newFixturePageID(pageAID) {
			t.Errorf("FromPageID = %q, want %q", bl.FromPageID, pageAID)
		}
		if bl.ToPageID != newFixturePageID(pageBID) {
			t.Errorf("ToPageID = %q, want %q", bl.ToPageID, pageBID)
		}
		if bl.FromTitle == "" {
			t.Errorf("FromTitle should not be empty")
		}

	})
})

// - Duplicate syntaxes do not create duplicate target identities after migration
var _ = ginkgo.Describe("TestLinkService_IndexAllPages_PreservesSamePathPageAndSectionTargets", func() {
	ginkgo.It("preserves behavior", func() {
		t := ginkgo.GinkgoT()
		dataDir := t.TempDir()
		rootDir := filepath.Join(t.TempDir(), "workspace")
		writeLinkServiceMarkdown(t, filepath.Join(rootDir, "docs", "source.md"), `---
leafwiki_id: source
leafwiki_title: Source
---
# Source

[Sync page](/docs/sync.md)
[Sync section](/docs/sync)
`)
		writeLinkServiceMarkdown(t, filepath.Join(rootDir, "docs", "section-source", "index.md"), `---
leafwiki_id: section-source
leafwiki_title: Section Source
---
# Section Source

[Sync page](/docs/sync.md)
`)
		writeLinkServiceMarkdown(t, filepath.Join(rootDir, "docs", "sync.md"), `---
leafwiki_id: sync-page
leafwiki_title: Sync Page
---
# Sync Page
`)
		writeLinkServiceMarkdown(t, filepath.Join(rootDir, "docs", "sync", "index.md"), `---
leafwiki_id: sync-section
leafwiki_title: Sync Section
---
# Sync Section
`)

		ts := tree.NewTreeServiceWithOptions(tree.TreeOptions{DataDir: dataDir, RootDir: rootDir})
		if err := ts.LoadTree(); err != nil {
			t.Fatalf("LoadTree failed: %v", err)
		}
		store, err := NewLinksStore(dataDir)
		if err != nil {
			t.Fatalf("NewLinksStore failed: %v", err)
		}
		svc := NewLinkService(dataDir, ts, store)
		source, err := ts.GetPage("source")
		if err != nil {
			t.Fatalf("GetPage source failed: %v", err)
		}

		if err := svc.IndexAllPages(); err != nil {
			t.Fatalf("IndexAllPages failed: %v", err)
		}

		outgoing, err := svc.GetOutgoingLinksForPage(source.ID)
		if err != nil {
			t.Fatalf("GetOutgoingLinksForPage failed: %v", err)
		}
		if outgoing.Count != 2 {
			t.Fatalf("expected 2 outgoing links, got %d: %#v", outgoing.Count, outgoing.Outgoings)
		}
		targetIDs := map[tree.PageID]bool{}
		for _, item := range outgoing.Outgoings {
			if item.ToPath != "/docs/sync" {
				t.Fatalf("ToPath = %q, want /docs/sync in %#v", item.ToPath, outgoing.Outgoings)
			}
			targetIDs[item.ToPageID] = true
		}
		if !targetIDs[newFixturePageID("sync-page")] || !targetIDs[newFixturePageID("sync-section")] {
			t.Fatalf("outgoing target IDs = %#v, want page sync-page and section sync-section", targetIDs)
		}

		pageBacklinks, err := svc.GetBacklinksForPage(newFixturePageID("sync-page"))
		if err != nil {
			t.Fatalf("GetBacklinksForPage(page) failed: %v", err)
		}
		if pageBacklinks.Count != 2 {
			t.Fatalf("expected 2 page backlinks, got %d: %#v", pageBacklinks.Count, pageBacklinks.Backlinks)
		}
		pageBacklinkKinds := map[string]bool{}
		for _, backlink := range pageBacklinks.Backlinks {
			pageBacklinkKinds[backlink.FromKind] = true
		}
		if !pageBacklinkKinds[string(tree.NodeKindPage)] || !pageBacklinkKinds[string(tree.NodeKindSection)] {
			t.Fatalf("page backlink FromKind values = %#v, want page and section", pageBacklinkKinds)
		}
		sectionBacklinks, err := svc.GetBacklinksForPage(newFixturePageID("sync-section"))
		if err != nil {
			t.Fatalf("GetBacklinksForPage(section) failed: %v", err)
		}
		if sectionBacklinks.Count != 1 {
			t.Fatalf("expected 1 section backlink, got %d: %#v", sectionBacklinks.Count, sectionBacklinks.Backlinks)
		}
		if sectionBacklinks.Backlinks[0].FromKind != string(tree.NodeKindPage) {
			t.Fatalf("section backlink FromKind = %q, want page", sectionBacklinks.Backlinks[0].FromKind)
		}

	})
})

var _ = ginkgo.Describe("TestLinkService_IndexAllPages_ResolvesMarkdownLinkRootPrefix", func() {
	ginkgo.It("preserves behavior", func() {
		t := ginkgo.GinkgoT()
		dataDir := t.TempDir()
		repoRoot := t.TempDir()
		rootDir := filepath.Join(repoRoot, "docs")
		writeLinkServiceMarkdown(t, filepath.Join(rootDir, "source.md"), `---
leafwiki_id: source
leafwiki_title: Source
---
# Source

[Glossary](/docs/sync/glossary.md)
`)
		writeLinkServiceMarkdown(t, filepath.Join(rootDir, "sync", "glossary.md"), `---
leafwiki_id: glossary
leafwiki_title: Glossary
---
# Glossary
`)

		ts := tree.NewTreeServiceWithOptions(tree.TreeOptions{DataDir: dataDir, RootDir: rootDir})
		if err := ts.LoadTree(); err != nil {
			t.Fatalf("LoadTree failed: %v", err)
		}
		store, err := NewLinksStore(dataDir)
		if err != nil {
			t.Fatalf("NewLinksStore failed: %v", err)
		}
		svc := NewLinkServiceWithOptions(dataDir, ts, store, LinkServiceOptions{
			MarkdownLinkRootPrefix: "/docs",
		})
		source, err := ts.GetPage("source")
		if err != nil {
			t.Fatalf("GetPage source failed: %v", err)
		}

		if err := svc.IndexAllPages(); err != nil {
			t.Fatalf("IndexAllPages failed: %v", err)
		}

		outgoing, err := svc.GetOutgoingLinksForPage(source.ID)
		if err != nil {
			t.Fatalf("GetOutgoingLinksForPage failed: %v", err)
		}
		if outgoing.Count != 1 {
			t.Fatalf("outgoing.Count = %d, want 1: %#v", outgoing.Count, outgoing)
		}
		if outgoing.Outgoings[0].ToPath != "/sync/glossary" || outgoing.Outgoings[0].Broken {
			t.Fatalf("outgoing link = %#v, want resolved /sync/glossary", outgoing.Outgoings[0])
		}

	})
})

var _ = ginkgo.Describe("TestLinkService_UpdateLinksForPage_ResolvesMarkdownLinkRootPrefix", func() {
	ginkgo.It("preserves behavior", func() {
		t := ginkgo.GinkgoT()
		dataDir := t.TempDir()
		repoRoot := t.TempDir()
		rootDir := filepath.Join(repoRoot, "docs")
		writeLinkServiceMarkdown(t, filepath.Join(rootDir, "source.md"), `---
leafwiki_id: source
leafwiki_title: Source
---
# Source

[Glossary](/docs/sync/glossary.md)
`)
		writeLinkServiceMarkdown(t, filepath.Join(rootDir, "sync", "glossary.md"), `---
leafwiki_id: glossary
leafwiki_title: Glossary
---
# Glossary
`)

		ts := tree.NewTreeServiceWithOptions(tree.TreeOptions{DataDir: dataDir, RootDir: rootDir})
		if err := ts.LoadTree(); err != nil {
			t.Fatalf("LoadTree failed: %v", err)
		}
		store, err := NewLinksStore(dataDir)
		if err != nil {
			t.Fatalf("NewLinksStore failed: %v", err)
		}
		svc := NewLinkServiceWithOptions(dataDir, ts, store, LinkServiceOptions{
			MarkdownLinkRootPrefix: "/docs",
		})
		source, err := ts.GetPage("source")
		if err != nil {
			t.Fatalf("GetPage source failed: %v", err)
		}

		if err := svc.UpdateLinksForPage(source, source.Content); err != nil {
			t.Fatalf("UpdateLinksForPage failed: %v", err)
		}

		outgoing, err := svc.GetOutgoingLinksForPage(source.ID)
		if err != nil {
			t.Fatalf("GetOutgoingLinksForPage failed: %v", err)
		}
		if outgoing.Count != 1 {
			t.Fatalf("outgoing.Count = %d, want 1: %#v", outgoing.Count, outgoing)
		}
		if outgoing.Outgoings[0].ToPath != "/sync/glossary" || outgoing.Outgoings[0].Broken {
			t.Fatalf("outgoing link = %#v, want resolved /sync/glossary", outgoing.Outgoings[0])
		}

	})
})

var _ = ginkgo.Describe("TestLinkService_IndexAllPages_ReusesMarkdownIndexForRootBackedBatch", func() {
	ginkgo.It("preserves behavior", func() {
		t := ginkgo.GinkgoT()
		dataDir := t.TempDir()
		rootDir := filepath.Join(t.TempDir(), "workspace")
		writeLinkServiceMarkdown(t, filepath.Join(rootDir, "source-a.md"), `---
leafwiki_id: source-a
leafwiki_title: Source A
---
[Target](/target.md)
`)
		writeLinkServiceMarkdown(t, filepath.Join(rootDir, "source-b.md"), `---
leafwiki_id: source-b
leafwiki_title: Source B
---
[Target](/target.md)
`)
		writeLinkServiceMarkdown(t, filepath.Join(rootDir, "target.md"), `---
leafwiki_id: target
leafwiki_title: Target
---
# Target
`)

		ts := tree.NewTreeServiceWithOptions(tree.TreeOptions{DataDir: dataDir, RootDir: rootDir})
		if err := ts.LoadTree(); err != nil {
			t.Fatalf("LoadTree failed: %v", err)
		}
		store, err := NewLinksStore(dataDir)
		if err != nil {
			t.Fatalf("NewLinksStore failed: %v", err)
		}
		svc := NewLinkService(dataDir, ts, store)
		calls := countMarkdownRootIndexBuilds(t, markdownlinks.NewIndex([]markdownlinks.Entry{
			{Kind: markdownlinks.EntryKindPage, Path: "source-a.md"},
			{Kind: markdownlinks.EntryKindPage, Path: "source-b.md"},
			{Kind: markdownlinks.EntryKindPage, Path: "target.md"},
		}))

		if err := svc.IndexAllPages(); err != nil {
			t.Fatalf("IndexAllPages failed: %v", err)
		}

		if *calls != 1 {
			t.Fatalf("root-backed markdown index built %d times, want 1", *calls)
		}

	})
})

var _ = ginkgo.Describe("TestLinkService_IndexAllPages_ReplacesExistingLinks", func() {
	ginkgo.It("preserves behavior", func() {
		t := ginkgo.GinkgoT()
		svc, ts, _ := setupLinkService(t)
		pageAID, pageBID := createSimpleLinkedPages(t, ts)

		if err := svc.IndexAllPages(); err != nil {
			t.Fatalf("IndexAllPages (first) failed: %v", err)
		}

		aPage, err := ts.GetPage(newFixturePageID(pageAID))
		if err != nil {
			t.Fatalf("GetPage a failed: %v", err)
		}
		var noLinks = "No more links."
		if err := ts.UpdateNodeUncheckedVersion(newFixtureUserID("system"), newFixturePageID(aPage.ID), aPage.Title, newFixtureSlug(aPage.Slug), &noLinks, false); err != nil {
			t.Fatalf("UpdatePage a failed: %v", err)
		}

		if err := svc.IndexAllPages(); err != nil {
			t.Fatalf("IndexAllPages (second) failed: %v", err)
		}

		data, err := svc.GetBacklinksForPage(newFixturePageID(pageBID))
		if err != nil {
			t.Fatalf("GetBacklinksForPage failed: %v", err)
		}

		if len(data.Backlinks) != 0 {
			t.Fatalf("expected 0 backlinks after reindex, got %d: %#v", len(data.Backlinks), data.Backlinks)
		}

	})
})
var _ = ginkgo.Describe("TestLinkService_UpdateLinksForPage_OnlyAffectsOnePage", func() {
	ginkgo.It("preserves behavior", func() {
		t := ginkgo.GinkgoT()
		svc, ts, _ := setupLinkService(t)
		pageAID, pageBID := createSimpleLinkedPages(t, ts)

		pageA, err := ts.GetPage(newFixturePageID(pageAID))
		if err != nil {
			t.Fatalf("GetPage a failed: %v", err)
		}
		if err := svc.UpdateLinksForPage(pageA, pageA.Content); err != nil {
			t.Fatalf("UpdateLinksForPage failed: %v", err)
		}

		dataB, err := svc.GetBacklinksForPage(newFixturePageID(pageBID))
		if err != nil {
			t.Fatalf("GetBacklinksForPage for B failed: %v", err)
		}
		if len(dataB.Backlinks) != 1 {
			t.Fatalf("expected 1 backlink for B, got %d: %#v", len(dataB.Backlinks), dataB.Backlinks)
		}

		dataA, err := svc.GetBacklinksForPage(newFixturePageID(pageAID))
		if err != nil {
			t.Fatalf("GetBacklinksForPage for A failed: %v", err)
		}
		if len(dataA.Backlinks) != 0 {
			t.Fatalf("expected 0 backlinks for A, got %d: %#v", len(dataA.Backlinks), dataA.Backlinks)
		}

	})
})

var _ = ginkgo.Describe("TestLinkService_ClearLinks_RemovesAllLinks", func() {
	ginkgo.It("preserves behavior", func() {
		t := ginkgo.GinkgoT()
		svc, ts, _ := setupLinkService(t)
		_, pageBID := createSimpleLinkedPages(t, ts)

		if err := svc.IndexAllPages(); err != nil {
			t.Fatalf("IndexAllPages failed: %v", err)
		}

		if err := svc.ClearLinks(); err != nil {
			t.Fatalf("ClearLinks failed: %v", err)
		}

		data, err := svc.GetBacklinksForPage(newFixturePageID(pageBID))
		if err != nil {
			t.Fatalf("GetBacklinksForPage failed: %v", err)
		}
		if len(data.Backlinks) != 0 {
			t.Fatalf("expected 0 backlinks after ClearBacklinks, got %d: %#v", len(data.Backlinks), data.Backlinks)
		}

	})
})

var _ = ginkgo.Describe("TestLinkService_GetOutgoingLinksForPage_ReturnsOutgoingLinks", func() {
	ginkgo.It("preserves behavior", func() {
		t := ginkgo.GinkgoT()
		svc, ts, _ := setupLinkService(t)
		pageAID, pageBID := createSimpleLinkedPages(t, ts)

		if err := svc.IndexAllPages(); err != nil {
			t.Fatalf("IndexAllPages failed: %v", err)
		}

		result, err := svc.GetOutgoingLinksForPage(newFixturePageID(pageAID))
		if err != nil {
			t.Fatalf("GetOutgoingLinksForPage failed: %v", err)
		}

		if result == nil {
			t.Fatalf("expected non-nil result")
		}

		if result.Count != 1 {
			t.Fatalf("expected 1 outgoing link for pageA, got %d: %#v", result.Count, result.Outgoings)
		}

		item := result.Outgoings[0]

		if item.FromPageID != newFixturePageID(pageAID) {
			t.Errorf("FromPageID = %q, want %q", item.FromPageID, pageAID)
		}

		if item.ToPageID != newFixturePageID(pageBID) {
			t.Errorf("ToPageID = %q, want %q", item.ToPageID, pageBID)
		}

		pageB, err := ts.GetPage(newFixturePageID(pageBID))
		if err != nil {
			t.Fatalf("GetPage(pageB) failed: %v", err)
		}
		if item.ToPath != "/b" {
			t.Errorf("ToPath = %q, want %q", item.ToPath, "/b")
		}
		if item.ToPageTitle != pageB.Title {
			t.Errorf("ToPageTitle = %q, want %q", item.ToPageTitle, pageB.Title)
		}

	})
})

var _ = ginkgo.Describe("TestLinkService_GetOutgoingLinksForPage_NoOutgoings", func() {
	ginkgo.It("preserves behavior", func() {
		t := ginkgo.GinkgoT()
		svc, ts, _ := setupLinkService(t)

		aIDPtr, err := ts.CreateNode("system", nil, "Lonely Page", "lonely", pageNodeKind())
		if err != nil {
			t.Fatalf("CreateNode lonely failed: %v", err)
		}
		lonelyID := *aIDPtr

		page, err := ts.GetPage(newFixturePageID(lonelyID))
		if err != nil {
			t.Fatalf("GetPage lonely failed: %v", err)
		}

		var noLinks = "Just some text, no links."
		if err := ts.UpdateNodeUncheckedVersion(newFixtureUserID("system"), newFixturePageID(page.ID), page.Title, newFixtureSlug(page.Slug), &noLinks, false); err != nil {
			t.Fatalf("UpdateNode lonely failed: %v", err)
		}

		if err := svc.IndexAllPages(); err != nil {
			t.Fatalf("IndexAllPages failed: %v", err)
		}

		result, err := svc.GetOutgoingLinksForPage(newFixturePageID(lonelyID))
		if err != nil {
			t.Fatalf("GetOutgoingLinksForPage failed: %v", err)
		}

		if result == nil {
			t.Fatalf("expected non-nil result")
		}

		if result.Count != 0 {
			t.Fatalf("expected 0 outgoing links, got %d: %#v", result.Count, result.Outgoings)
		}

	})
})

var _ = ginkgo.Describe("TestLinkService_IndexAllPages_IgnoresAssetLinksInOutgoingAndBrokenSets", func() {
	ginkgo.It("preserves behavior", func() {
		t := ginkgo.GinkgoT()
		svc, ts, _ := setupLinkService(t)

		aIDPtr, err := ts.CreateNode("system", nil, "Page A", "a", pageNodeKind())
		if err != nil {
			t.Fatalf("CreateNode a failed: %v", err)
		}
		pageAID := *aIDPtr

		bIDPtr, err := ts.CreateNode("system", nil, "Page B", "b", pageNodeKind())
		if err != nil {
			t.Fatalf("CreateNode b failed: %v", err)
		}
		pageBID := *bIDPtr

		pageA, err := ts.GetPage(newFixturePageID(pageAID))
		if err != nil {
			t.Fatalf("GetPage a failed: %v", err)
		}
		contentA := "Asset: [Manual](/assets/abc/manual.pdf)\nPage: [Go](/b)"
		if err := ts.UpdateNodeUncheckedVersion(newFixtureUserID("system"), newFixturePageID(pageA.ID), pageA.Title, newFixtureSlug(pageA.Slug), &contentA, false); err != nil {
			t.Fatalf("UpdateNode a failed: %v", err)
		}

		if err := svc.IndexAllPages(); err != nil {
			t.Fatalf("IndexAllPages failed: %v", err)
		}

		outgoing, err := svc.GetOutgoingLinksForPage(newFixturePageID(pageAID))
		if err != nil {
			t.Fatalf("GetOutgoingLinksForPage failed: %v", err)
		}
		if outgoing.Count != 1 {
			t.Fatalf("expected only wiki links in outgoings, got %d: %#v", outgoing.Count, outgoing.Outgoings)
		}
		if outgoing.Outgoings[0].ToPath != "/b" {
			t.Fatalf("ToPath = %q, want %q", outgoing.Outgoings[0].ToPath, "/b")
		}
		if !outgoing.Outgoings[0].Broken {
			t.Fatalf("extensionless page link should be indexed as broken/non-canonical, got %#v", outgoing.Outgoings[0])
		}

		status, err := svc.GetLinkStatusForPage(newFixturePageID(pageAID), "/a")
		if err != nil {
			t.Fatalf("GetLinkStatusForPage failed: %v", err)
		}
		if status.Counts.Outgoings != 0 || status.Counts.BrokenOutgoings != 1 {
			t.Fatalf("unexpected link status counts: %#v", status.Counts)
		}

		backlinks, err := svc.GetBacklinksForPage(newFixturePageID(pageBID))
		if err != nil {
			t.Fatalf("GetBacklinksForPage failed: %v", err)
		}
		if backlinks.Count != 0 {
			t.Fatalf("expected non-canonical page alias to stay out of healthy backlinks, got %d: %#v", backlinks.Count, backlinks.Backlinks)
		}

	})
})

var _ = ginkgo.Describe("TestToOutgoingResult_MapsOutgoingToResultItems", func() {
	ginkgo.It("preserves behavior", func() {
		t := ginkgo.GinkgoT()
		ts, page1ID, page2ID := setupTreeForLinksTest(t)

		root := ts.GetTree()
		if root == nil {
			t.Fatalf("tree root is nil")
		}

		outgoings := []Outgoing{{FromPageID: newFixturePageID(page1ID), ToPageID: newFixturePageID(page2ID), ToPath: "/docs/page2", Broken: false, FromTitle: "Page 1"}}

		result := toOutgoingLinkResult(ts, outgoings)
		if result == nil {
			t.Fatalf("expected non-nil result")
		}
		if result.Count != 1 {
			t.Fatalf("expected 1 outgoing, got %d", result.Count)
		}

		item := result.Outgoings[0]

		if item.FromPageID != newFixturePageID(page1ID) {
			t.Errorf("FromPageID = %q, want %q", item.FromPageID, page1ID)
		}
		if item.ToPageID != newFixturePageID(page2ID) {
			t.Errorf("ToPageID = %q, want %q", item.ToPageID, page2ID)
		}

		page2, err := ts.GetPage(newFixturePageID(page2ID))
		if err != nil {
			t.Fatalf("GetPage page2 failed: %v", err)
		}
		if item.ToPageTitle != page2.Title {
			t.Errorf("ToPageTitle = %q, want %q", item.ToPageTitle, page2.Title)
		}
		if item.ToPath != "/docs/page2" {
			t.Errorf("ToPath = %q, want %q", item.ToPath, "/docs/page2")
		}
		if item.Broken {
			t.Errorf("Broken = %v, want %v", item.Broken, false)
		}

	})
})

var _ = ginkgo.Describe("TestLinkService_LateCreatedTarget_BecomesResolvedAfterReindex", func() {
	ginkgo.It("preserves behavior", func() {
		t := ginkgo.GinkgoT()
		svc, ts, _ := setupLinkService(t)

		aIDPtr, err := ts.CreateNode("system", nil, "Page A", "a", pageNodeKind())
		if err != nil {
			t.Fatalf("CreateNode a failed: %v", err)
		}
		pageAID := *aIDPtr

		aPage, err := ts.GetPage(newFixturePageID(pageAID))
		if err != nil {
			t.Fatalf("GetPage a failed: %v", err)
		}
		var linkToB = "Link to B: [Go](/b.md)"
		if err := ts.UpdateNodeUncheckedVersion(newFixtureUserID("system"), newFixturePageID(aPage.ID), aPage.Title, newFixtureSlug(aPage.Slug), &linkToB, false); err != nil {
			t.Fatalf("UpdateNode a failed: %v", err)
		}

		if err := svc.IndexAllPages(); err != nil {
			t.Fatalf("IndexAllPages failed: %v", err)
		}

		out1, err := svc.GetOutgoingLinksForPage(newFixturePageID(pageAID))
		if err != nil {
			t.Fatalf("GetOutgoingLinksForPage failed: %v", err)
		}
		if out1.Count != 1 {
			t.Fatalf("expected 1 outgoing, got %d: %#v", out1.Count, out1.Outgoings)
		}
		if out1.Outgoings[0].Broken != true {
			t.Fatalf("expected outgoing to be broken, got %#v", out1.Outgoings[0])
		}
		if out1.Outgoings[0].ToPath != "/b" {
			t.Fatalf("expected ToPath '/b', got %q", out1.Outgoings[0].ToPath)
		}
		if out1.Outgoings[0].ToPageID != "" {
			t.Fatalf("expected empty ToPageID for broken link, got %q", out1.Outgoings[0].ToPageID)
		}

		bIDPtr, err := ts.CreateNode("system", nil, "Page B", "b", pageNodeKind())
		if err != nil {
			t.Fatalf("CreateNode b failed: %v", err)
		}
		pageBID := *bIDPtr

		bPage, err := ts.GetPage(newFixturePageID(pageBID))
		if err != nil {
			t.Fatalf("GetPage b failed: %v", err)
		}
		var pageBContent = "# Page B"
		if err := ts.UpdateNodeUncheckedVersion(newFixtureUserID("system"), newFixturePageID(bPage.ID), bPage.Title, newFixtureSlug(bPage.Slug), &pageBContent, false); err != nil {
			t.Fatalf("UpdateNode b failed: %v", err)
		}

		if err := svc.IndexAllPages(); err != nil {
			t.Fatalf("IndexAllPages (second) failed: %v", err)
		}

		out2, err := svc.GetOutgoingLinksForPage(newFixturePageID(pageAID))
		if err != nil {
			t.Fatalf("GetOutgoingLinksForPage (second) failed: %v", err)
		}
		if out2.Count != 1 {
			t.Fatalf("expected 1 outgoing, got %d: %#v", out2.Count, out2.Outgoings)
		}
		if out2.Outgoings[0].Broken != false {
			t.Fatalf("expected outgoing to be resolved, got %#v", out2.Outgoings[0])
		}
		if out2.Outgoings[0].ToPageID != newFixturePageID(pageBID) {
			t.Fatalf("expected ToPageID %q, got %q", pageBID, out2.Outgoings[0].ToPageID)
		}

		bl, err := svc.GetBacklinksForPage(newFixturePageID(pageBID))
		if err != nil {
			t.Fatalf("GetBacklinksForPage failed: %v", err)
		}
		if bl.Count != 1 {
			t.Fatalf("expected 1 backlink, got %d: %#v", bl.Count, bl.Backlinks)
		}
		if bl.Backlinks[0].FromPageID != newFixturePageID(pageAID) {
			t.Fatalf("expected FromPageID %q, got %q", pageAID, bl.Backlinks[0].FromPageID)
		}
		if bl.Backlinks[0].ToPageID != newFixturePageID(pageBID) {
			t.Fatalf("expected ToPageID %q, got %q", pageBID, bl.Backlinks[0].ToPageID)
		}

	})
})

var _ = ginkgo.Describe("TestLinkService_HealOnPageCreate_ResolvesBrokenLinksWithoutReindex", func() {
	ginkgo.It("preserves behavior", func() {
		t := ginkgo.GinkgoT()
		svc, ts, _ := setupLinkService(t)

		aIDPtr, err := ts.CreateNode("system", nil, "Page A", "a", pageNodeKind())
		if err != nil {
			t.Fatalf("CreateNode A failed: %v", err)
		}
		pageAID := *aIDPtr

		pageA, err := ts.GetPage(newFixturePageID(pageAID))
		if err != nil {
			t.Fatalf("GetPage A failed: %v", err)
		}
		var linkToB = "Link to B: [Go](/b.md)"
		if err := ts.UpdateNodeUncheckedVersion(newFixtureUserID("system"), newFixturePageID(pageA.ID), pageA.Title, newFixtureSlug(pageA.Slug), &linkToB, false); err != nil {
			t.Fatalf("UpdateNode A failed: %v", err)
		}

		if err := svc.IndexAllPages(); err != nil {
			t.Fatalf("IndexAllPages failed: %v", err)
		}

		out1, err := svc.GetOutgoingLinksForPage(newFixturePageID(pageAID))
		if err != nil {
			t.Fatalf("GetOutgoingLinksForPage failed: %v", err)
		}
		if out1.Count != 1 {
			t.Fatalf("expected 1 outgoing for A, got %d: %#v", out1.Count, out1.Outgoings)
		}

		if out1.Outgoings[0].Broken != true {
			t.Fatalf("expected outgoing to be broken before heal, got %#v", out1.Outgoings[0])
		}
		if out1.Outgoings[0].ToPath != "/b" {
			t.Fatalf("expected ToPath '/b' before heal, got %q", out1.Outgoings[0].ToPath)
		}
		if out1.Outgoings[0].ToPageID != "" {
			t.Fatalf("expected empty ToPageID before heal, got %q", out1.Outgoings[0].ToPageID)
		}

		bIDPtr, err := ts.CreateNode("system", nil, "Page B", "b", pageNodeKind())
		if err != nil {
			t.Fatalf("CreateNode B failed: %v", err)
		}
		pageBID := *bIDPtr

		pageB, err := ts.GetPage(newFixturePageID(pageBID))
		if err != nil {
			t.Fatalf("GetPage B failed: %v", err)
		}

		if err := svc.HealLinksForExactPath(pageB); err != nil {
			t.Fatalf("HealLinksForExactPath failed: %v", err)
		}

		out2, err := svc.GetOutgoingLinksForPage(newFixturePageID(pageAID))
		if err != nil {
			t.Fatalf("GetOutgoingLinksForPage (after heal) failed: %v", err)
		}
		if out2.Count != 1 {
			t.Fatalf("expected 1 outgoing for A after heal, got %d: %#v", out2.Count, out2.Outgoings)
		}

		if out2.Outgoings[0].Broken != false {
			t.Fatalf("expected outgoing to be resolved after heal, got %#v", out2.Outgoings[0])
		}
		if out2.Outgoings[0].ToPageID != newFixturePageID(pageBID) {
			t.Fatalf("expected ToPageID %q after heal, got %q", pageBID, out2.Outgoings[0].ToPageID)
		}

		bl, err := svc.GetBacklinksForPage(newFixturePageID(pageBID))
		if err != nil {
			t.Fatalf("GetBacklinksForPage failed: %v", err)
		}
		if bl.Count != 1 {
			t.Fatalf("expected 1 backlink for B after heal, got %d: %#v", bl.Count, bl.Backlinks)
		}
		if bl.Backlinks[0].FromPageID != newFixturePageID(pageAID) {
			t.Fatalf("expected backlink FromPageID %q, got %q", pageAID, bl.Backlinks[0].FromPageID)
		}
		if bl.Backlinks[0].ToPageID != newFixturePageID(pageBID) {
			t.Fatalf("expected backlink ToPageID %q, got %q", pageBID, bl.Backlinks[0].ToPageID)
		}

	})
})

var _ = ginkgo.Describe("TestLinksStore_GetBrokenIncomingForPath_ReturnsBrokenLinks", func() {
	ginkgo.It("preserves behavior", func() {
		t := ginkgo.GinkgoT()
		svc, ts, store := setupLinkService(t)

		// Create three pages: A, B, C
		aIDPtr, err := ts.CreateNode("system", nil, "Page A", "a", pageNodeKind())
		if err != nil {
			t.Fatalf("CreateNode A failed: %v", err)
		}
		pageAID := *aIDPtr

		bIDPtr, err := ts.CreateNode("system", nil, "Page B", "b", pageNodeKind())
		if err != nil {
			t.Fatalf("CreateNode B failed: %v", err)
		}
		pageBID := *bIDPtr

		cIDPtr, err := ts.CreateNode("system", nil, "Page C", "c", pageNodeKind())
		if err != nil {
			t.Fatalf("CreateNode C failed: %v", err)
		}
		pageCID := *cIDPtr

		// Update A and B to link to a non-existent page "/nonexistent"
		pageA, err := ts.GetPage(newFixturePageID(pageAID))
		if err != nil {
			t.Fatalf("GetPage A failed: %v", err)
		}
		var linkToMissing = "Link: [Missing](/nonexistent)"
		if err := ts.UpdateNodeUncheckedVersion(newFixtureUserID("system"), newFixturePageID(pageA.ID), pageA.Title, newFixtureSlug(pageA.Slug), &linkToMissing, false); err != nil {
			t.Fatalf("UpdateNode A failed: %v", err)
		}

		pageB, err := ts.GetPage(newFixturePageID(pageBID))
		if err != nil {
			t.Fatalf("GetPage B failed: %v", err)
		}
		if err := ts.UpdateNodeUncheckedVersion(newFixtureUserID("system"), newFixturePageID(pageB.ID), pageB.Title, newFixtureSlug(pageB.Slug), &linkToMissing, false); err != nil {
			t.Fatalf("UpdateNode B failed: %v", err)
		}

		// Page C links to a different broken page
		pageC, err := ts.GetPage(newFixturePageID(pageCID))
		if err != nil {
			t.Fatalf("GetPage C failed: %v", err)
		}
		var linkToOther = "Link: [Other](/other-missing)"
		if err := ts.UpdateNodeUncheckedVersion(newFixtureUserID("system"), newFixturePageID(pageC.ID), pageC.Title, newFixtureSlug(pageC.Slug), &linkToOther, false); err != nil {
			t.Fatalf("UpdateNode C failed: %v", err)
		}

		// Index all pages to create broken links
		if err := svc.IndexAllPages(); err != nil {
			t.Fatalf("IndexAllPages failed: %v", err)
		}

		// Test: GetBrokenIncomingForPath should return broken links for "/nonexistent"
		brokenLinks, err := store.GetBrokenIncomingForPath("/nonexistent")
		if err != nil {
			t.Fatalf("GetBrokenIncomingForPath failed: %v", err)
		}

		if len(brokenLinks) != 2 {
			t.Fatalf("expected 2 broken links for /nonexistent, got %d: %#v", len(brokenLinks), brokenLinks)
		}

		// Verify all returned links are marked as broken
		for i, link := range brokenLinks {
			if !link.Broken {
				t.Errorf("brokenLinks[%d].Broken = %v, want true", i, link.Broken)
			}
			if link.ToPageID != "" {
				t.Errorf("brokenLinks[%d].ToPageID = %q, want empty string for broken link", i, link.ToPageID)
			}
			if link.FromTitle == "" {
				t.Errorf("brokenLinks[%d].FromTitle should not be empty", i)
			}
		}

		// Verify the links come from pages A and B
		fromPageIDs := map[tree.PageID]struct{}{}
		for _, link := range brokenLinks {
			fromPageIDs[link.FromPageID] = struct{}{}
		}
		if _, found := fromPageIDs[newFixturePageID(pageAID)]; !found {
			t.Errorf("expected broken link from page A (%s)", pageAID)
		}
		if _, found := fromPageIDs[newFixturePageID(pageBID)]; !found {
			t.Errorf("expected broken link from page B (%s)", pageBID)
		}

	})
})

var _ = ginkgo.Describe("TestLinksStore_GetBrokenIncomingForPath_FiltersByPath", func() {
	ginkgo.It("preserves behavior", func() {
		t := ginkgo.GinkgoT()
		svc, ts, store := setupLinkService(t)

		aIDPtr, err := ts.CreateNode("system", nil, "Page A", "a", pageNodeKind())
		if err != nil {
			t.Fatalf("CreateNode A failed: %v", err)
		}
		pageAID := *aIDPtr

		bIDPtr, err := ts.CreateNode("system", nil, "Page B", "b", pageNodeKind())
		if err != nil {
			t.Fatalf("CreateNode B failed: %v", err)
		}
		pageBID := *bIDPtr

		// Page A links to "/missing1"
		pageA, err := ts.GetPage(newFixturePageID(pageAID))
		if err != nil {
			t.Fatalf("GetPage A failed: %v", err)
		}
		var linkToMissing1 = "Link: [Missing1](/missing1)"
		if err := ts.UpdateNodeUncheckedVersion(newFixtureUserID("system"), newFixturePageID(pageA.ID), pageA.Title, newFixtureSlug(pageA.Slug), &linkToMissing1, false); err != nil {
			t.Fatalf("UpdateNode A failed: %v", err)
		}

		// Page B links to "/missing2"
		pageB, err := ts.GetPage(newFixturePageID(pageBID))
		if err != nil {
			t.Fatalf("GetPage B failed: %v", err)
		}
		var linkToMissing2 = "Link: [Missing2](/missing2)"
		if err := ts.UpdateNodeUncheckedVersion(newFixtureUserID("system"), newFixturePageID(pageB.ID), pageB.Title, newFixtureSlug(pageB.Slug), &linkToMissing2, false); err != nil {
			t.Fatalf("UpdateNode B failed: %v", err)
		}

		if err := svc.IndexAllPages(); err != nil {
			t.Fatalf("IndexAllPages failed: %v", err)
		}

		// Test: Should only return broken links for "/missing1"
		broken1, err := store.GetBrokenIncomingForPath("/missing1")
		if err != nil {
			t.Fatalf("GetBrokenIncomingForPath(/missing1) failed: %v", err)
		}

		if len(broken1) != 1 {
			t.Fatalf("expected 1 broken link for /missing1, got %d: %#v", len(broken1), broken1)
		}
		if broken1[0].FromPageID != newFixturePageID(pageAID) {
			t.Errorf("broken link FromPageID = %q, want %q", broken1[0].FromPageID, pageAID)
		}

		// Test: Should only return broken links for "/missing2"
		broken2, err := store.GetBrokenIncomingForPath("/missing2")
		if err != nil {
			t.Fatalf("GetBrokenIncomingForPath(/missing2) failed: %v", err)
		}

		if len(broken2) != 1 {
			t.Fatalf("expected 1 broken link for /missing2, got %d: %#v", len(broken2), broken2)
		}
		if broken2[0].FromPageID != newFixturePageID(pageBID) {
			t.Errorf("broken link FromPageID = %q, want %q", broken2[0].FromPageID, pageBID)
		}

	})
})

var _ = ginkgo.Describe("TestLinksStore_GetBrokenIncomingForPath_EmptyWhenNoBrokenLinks", func() {
	ginkgo.It("preserves behavior", func() {
		t := ginkgo.GinkgoT()
		svc, ts, store := setupLinkService(t)

		aIDPtr, err := ts.CreateNode("system", nil, "Page A", "a", pageNodeKind())
		if err != nil {
			t.Fatalf("CreateNode A failed: %v", err)
		}
		pageAID := *aIDPtr

		_, err = ts.CreateNode("system", nil, "Page B", "b", pageNodeKind())
		if err != nil {
			t.Fatalf("CreateNode B failed: %v", err)
		}

		// Page A links to existing Page B (not broken)
		pageA, err := ts.GetPage(newFixturePageID(pageAID))
		if err != nil {
			t.Fatalf("GetPage A failed: %v", err)
		}
		var linkToB = "Link: [To B](/b.md)"
		if err := ts.UpdateNodeUncheckedVersion(newFixtureUserID("system"), newFixturePageID(pageA.ID), pageA.Title, newFixtureSlug(pageA.Slug), &linkToB, false); err != nil {
			t.Fatalf("UpdateNode A failed: %v", err)
		}

		if err := svc.IndexAllPages(); err != nil {
			t.Fatalf("IndexAllPages failed: %v", err)
		}

		// Test: Should return empty for "/b" since the link is not broken
		brokenLinks, err := store.GetBrokenIncomingForPath("/b")
		if err != nil {
			t.Fatalf("GetBrokenIncomingForPath failed: %v", err)
		}

		if len(brokenLinks) != 0 {
			t.Fatalf("expected 0 broken links for /b (link exists), got %d: %#v", len(brokenLinks), brokenLinks)
		}

		// Test: Should return empty for a path that has no links at all
		noLinks, err := store.GetBrokenIncomingForPath("/never-linked")
		if err != nil {
			t.Fatalf("GetBrokenIncomingForPath(/never-linked) failed: %v", err)
		}

		if len(noLinks) != 0 {
			t.Fatalf("expected 0 broken links for /never-linked, got %d: %#v", len(noLinks), noLinks)
		}

	})
})

var _ = ginkgo.Describe("TestLinksStore_GetBrokenIncomingForPath_OrdersByFromTitle", func() {
	ginkgo.It("preserves behavior", func() {
		t := ginkgo.GinkgoT()
		svc, ts, store := setupLinkService(t)

		// Create three pages with titles that should be ordered alphabetically
		zIDPtr, err := ts.CreateNode("system", nil, "Zebra Page", "z", pageNodeKind())
		if err != nil {
			t.Fatalf("CreateNode Z failed: %v", err)
		}

		aIDPtr, err := ts.CreateNode("system", nil, "Alpha Page", "a", pageNodeKind())
		if err != nil {
			t.Fatalf("CreateNode A failed: %v", err)
		}

		mIDPtr, err := ts.CreateNode("system", nil, "Middle Page", "m", pageNodeKind())
		if err != nil {
			t.Fatalf("CreateNode M failed: %v", err)
		}

		// All three pages link to the same non-existent page
		pageIDs := []tree.PageID{*zIDPtr, *aIDPtr, *mIDPtr}
		for _, id := range pageIDs {
			page, err := ts.GetPage(id)
			if err != nil {
				t.Fatalf("GetPage(%s) failed: %v", id, err)
			}
			var linkToMissing = "Link: [Missing](/missing)"
			if err := ts.UpdateNodeUncheckedVersion(newFixtureUserID("system"), newFixturePageID(page.ID), page.Title, newFixtureSlug(page.Slug), &linkToMissing, false); err != nil {
				t.Fatalf("UpdateNode(%s) failed: %v", id, err)
			}
		}

		if err := svc.IndexAllPages(); err != nil {
			t.Fatalf("IndexAllPages failed: %v", err)
		}

		// Test: Results should be ordered by from_title ASC
		brokenLinks, err := store.GetBrokenIncomingForPath("/missing")
		if err != nil {
			t.Fatalf("GetBrokenIncomingForPath failed: %v", err)
		}

		if len(brokenLinks) != 3 {
			t.Fatalf("expected 3 broken links, got %d: %#v", len(brokenLinks), brokenLinks)
		}

		// Verify ordering: Alpha Page, Middle Page, Zebra Page
		expectedTitles := []string{"Alpha Page", "Middle Page", "Zebra Page"}
		for i, expected := range expectedTitles {
			if brokenLinks[i].FromTitle != expected {
				t.Errorf("brokenLinks[%d].FromTitle = %q, want %q", i, brokenLinks[i].FromTitle, expected)
			}
		}

	})
})

var _ = ginkgo.Describe("TestLinksStore_GetBrokenIncomingForPath_OnlyReturnsBrokenNotResolved", func() {
	ginkgo.It("preserves behavior", func() {
		t := ginkgo.GinkgoT()
		svc, ts, store := setupLinkService(t)

		// Create Page A that links to a non-existent page
		aIDPtr, err := ts.CreateNode("system", nil, "Page A", "a", pageNodeKind())
		if err != nil {
			t.Fatalf("CreateNode A failed: %v", err)
		}
		pageAID := *aIDPtr

		pageA, err := ts.GetPage(newFixturePageID(pageAID))
		if err != nil {
			t.Fatalf("GetPage A failed: %v", err)
		}
		var linkToB = "Link: [To B](/b.md)"
		if err := ts.UpdateNodeUncheckedVersion(newFixtureUserID("system"), newFixturePageID(pageA.ID), pageA.Title, newFixtureSlug(pageA.Slug), &linkToB, false); err != nil {
			t.Fatalf("UpdateNode A failed: %v", err)
		}

		// Index - this creates a broken link since B doesn't exist
		if err := svc.IndexAllPages(); err != nil {
			t.Fatalf("IndexAllPages (first) failed: %v", err)
		}

		// Verify the broken link exists
		brokenBefore, err := store.GetBrokenIncomingForPath("/b")
		if err != nil {
			t.Fatalf("GetBrokenIncomingForPath (before) failed: %v", err)
		}
		if len(brokenBefore) != 1 {
			t.Fatalf("expected 1 broken link before creating B, got %d", len(brokenBefore))
		}

		// Now create Page B - this should heal the link
		bIDPtr, err := ts.CreateNode("system", nil, "Page B", "b", pageNodeKind())
		if err != nil {
			t.Fatalf("CreateNode B failed: %v", err)
		}
		pageBID := *bIDPtr

		pageB, err := ts.GetPage(newFixturePageID(pageBID))
		if err != nil {
			t.Fatalf("GetPage B failed: %v", err)
		}
		var contentB = "# Page B"
		if err := ts.UpdateNodeUncheckedVersion(newFixtureUserID("system"), newFixturePageID(pageB.ID), pageB.Title, newFixtureSlug(pageB.Slug), &contentB, false); err != nil {
			t.Fatalf("UpdateNode B failed: %v", err)
		}

		// Use HealLinksForExactPath to heal the broken link
		if err := svc.HealLinksForExactPath(pageB); err != nil {
			t.Fatalf("HealLinksForExactPath failed: %v", err)
		}

		// Verify the link is no longer broken
		brokenAfter, err := store.GetBrokenIncomingForPath("/b")
		if err != nil {
			t.Fatalf("GetBrokenIncomingForPath (after) failed: %v", err)
		}
		if len(brokenAfter) != 0 {
			t.Fatalf("expected 0 broken links after healing, got %d: %#v", len(brokenAfter), brokenAfter)
		}

		// Verify the link still exists but is not broken
		backlinks, err := store.GetBacklinksForPage(newFixturePageID(pageBID))
		if err != nil {
			t.Fatalf("GetBacklinksForPage failed: %v", err)
		}
		if len(backlinks) != 1 {
			t.Fatalf("expected 1 resolved backlink, got %d: %#v", len(backlinks), backlinks)
		}
		if backlinks[0].FromPageID != newFixturePageID(pageAID) {
			t.Errorf("backlink FromPageID = %q, want %q", backlinks[0].FromPageID, pageAID)
		}

	})
})

var _ = ginkgo.Describe("TestLinkService_HealLinksForExactPath_RehomesExtensionlessLinkToSectionTwin", func() {
	ginkgo.It("preserves behavior", func() {
		t := ginkgo.GinkgoT()
		svc, ts, _ := setupLinkService(t)

		sourceIDPtr, err := ts.CreateNode("system", nil, "Source", "source", pageNodeKind())
		if err != nil {
			t.Fatalf("CreateNode source failed: %v", err)
		}
		sourceID := *sourceIDPtr
		source, err := ts.GetPage(newFixturePageID(sourceID))
		if err != nil {
			t.Fatalf("GetPage source failed: %v", err)
		}
		sourceContent := "Link: [Target](/x)"
		if err := ts.UpdateNodeUncheckedVersion(newFixtureUserID("system"), newFixturePageID(source.ID), source.Title, newFixtureSlug(source.Slug), &sourceContent, false); err != nil {
			t.Fatalf("UpdateNode source failed: %v", err)
		}
		source, err = ts.GetPage(newFixturePageID(sourceID))
		if err != nil {
			t.Fatalf("GetPage updated source failed: %v", err)
		}
		if err := svc.UpdateLinksForPage(source, source.Content); err != nil {
			t.Fatalf("UpdateLinksForPage source failed: %v", err)
		}

		initialStatus, err := svc.GetLinkStatusForPage(source.ID, source.CalculateRoutePath())
		if err != nil {
			t.Fatalf("GetLinkStatusForPage initial failed: %v", err)
		}
		if initialStatus.Counts.BrokenOutgoings != 1 {
			t.Fatalf("initial broken outgoing count = %d, want 1", initialStatus.Counts.BrokenOutgoings)
		}

		pageIDPtr, err := ts.CreateNode("system", nil, "X Page", "x", pageNodeKind())
		if err != nil {
			t.Fatalf("CreateNode page target failed: %v", err)
		}
		pageTarget, err := ts.GetPage(*pageIDPtr)
		if err != nil {
			t.Fatalf("GetPage page target failed: %v", err)
		}
		if err := svc.HealLinksForExactPath(pageTarget); err != nil {
			t.Fatalf("HealLinksForExactPath page target failed: %v", err)
		}
		pageBacklinks, err := svc.GetBacklinksForPage(pageTarget.ID)
		if err != nil {
			t.Fatalf("GetBacklinksForPage page target failed: %v", err)
		}
		if pageBacklinks.Count != 0 {
			t.Fatalf("page target backlinks after extensionless heal = %#v, want none", pageBacklinks.Backlinks)
		}
		statusAfterPageHeal, err := svc.GetLinkStatusForPage(source.ID, source.CalculateRoutePath())
		if err != nil {
			t.Fatalf("GetLinkStatusForPage after page heal failed: %v", err)
		}
		if statusAfterPageHeal.Counts.BrokenOutgoings != 1 {
			t.Fatalf("broken outgoing count after page heal = %d, want 1", statusAfterPageHeal.Counts.BrokenOutgoings)
		}

		sectionIDPtr, err := ts.CreateNode("system", nil, "X Section", "x", sectionNodeKind())
		if err != nil {
			t.Fatalf("CreateNode section target failed: %v", err)
		}
		sectionTarget, err := ts.GetPage(*sectionIDPtr)
		if err != nil {
			t.Fatalf("GetPage section target failed: %v", err)
		}
		if err := svc.HealLinksForExactPath(sectionTarget); err != nil {
			t.Fatalf("HealLinksForExactPath section target failed: %v", err)
		}

		sectionBacklinks, err := svc.GetBacklinksForPage(sectionTarget.ID)
		if err != nil {
			t.Fatalf("GetBacklinksForPage section target failed: %v", err)
		}
		if sectionBacklinks.Count != 1 || sectionBacklinks.Backlinks[0].FromPageID != source.ID {
			t.Fatalf("section target backlinks = %#v, want source backlink", sectionBacklinks.Backlinks)
		}
		pageBacklinksAfter, err := svc.GetBacklinksForPage(pageTarget.ID)
		if err != nil {
			t.Fatalf("GetBacklinksForPage page target after section failed: %v", err)
		}
		if pageBacklinksAfter.Count != 0 {
			t.Fatalf("page target backlinks after section heal = %#v, want none", pageBacklinksAfter.Backlinks)
		}

	})
})

var _ = ginkgo.Describe("TestLinkService_UpdateLinksAndHealForPages_UpdatesAndHealsMultiplePages", func() {
	ginkgo.It("preserves behavior", func() {
		t := ginkgo.GinkgoT()
		svc, ts, _ := setupLinkService(t)

		aIDPtr, err := ts.CreateNode("system", nil, "Page A", "a", pageNodeKind())
		if err != nil {
			t.Fatalf("CreateNode A failed: %v", err)
		}
		pageAID := *aIDPtr

		cIDPtr, err := ts.CreateNode("system", nil, "Page C", "c", pageNodeKind())
		if err != nil {
			t.Fatalf("CreateNode C failed: %v", err)
		}
		pageCID := *cIDPtr

		pageA, err := ts.GetPage(newFixturePageID(pageAID))
		if err != nil {
			t.Fatalf("GetPage A failed: %v", err)
		}
		contentA := "Link: [B](/b.md)"
		if err := ts.UpdateNodeUncheckedVersion(newFixtureUserID("system"), newFixturePageID(pageA.ID), pageA.Title, newFixtureSlug(pageA.Slug), &contentA, false); err != nil {
			t.Fatalf("UpdateNode A failed: %v", err)
		}

		pageC, err := ts.GetPage(newFixturePageID(pageCID))
		if err != nil {
			t.Fatalf("GetPage C failed: %v", err)
		}
		contentC := "Link: [D](/d.md)"
		if err := ts.UpdateNodeUncheckedVersion(newFixtureUserID("system"), newFixturePageID(pageC.ID), pageC.Title, newFixtureSlug(pageC.Slug), &contentC, false); err != nil {
			t.Fatalf("UpdateNode C failed: %v", err)
		}

		if err := svc.IndexAllPages(); err != nil {
			t.Fatalf("IndexAllPages failed: %v", err)
		}

		bIDPtr, err := ts.CreateNode("system", nil, "Page B", "b", pageNodeKind())
		if err != nil {
			t.Fatalf("CreateNode B failed: %v", err)
		}
		pageB, err := ts.GetPage(*bIDPtr)
		if err != nil {
			t.Fatalf("GetPage B failed: %v", err)
		}

		dIDPtr, err := ts.CreateNode("system", nil, "Page D", "d", pageNodeKind())
		if err != nil {
			t.Fatalf("CreateNode D failed: %v", err)
		}
		pageD, err := ts.GetPage(*dIDPtr)
		if err != nil {
			t.Fatalf("GetPage D failed: %v", err)
		}

		if err := svc.UpdateLinksAndHealForPages([]*tree.Page{pageB, pageD}); err != nil {
			t.Fatalf("UpdateLinksAndHealForPages failed: %v", err)
		}

		outA, err := svc.GetOutgoingLinksForPage(newFixturePageID(pageAID))
		if err != nil {
			t.Fatalf("GetOutgoingLinksForPage(A) failed: %v", err)
		}
		if outA.Count != 1 {
			t.Fatalf("expected 1 outgoing for A, got %d: %#v", outA.Count, outA.Outgoings)
		}
		if outA.Outgoings[0].Broken {
			t.Fatalf("expected A link to be healed, got %#v", outA.Outgoings[0])
		}
		if outA.Outgoings[0].ToPageID != pageB.ID {
			t.Fatalf("A ToPageID = %q, want %q", outA.Outgoings[0].ToPageID, pageB.ID)
		}

		outC, err := svc.GetOutgoingLinksForPage(newFixturePageID(pageCID))
		if err != nil {
			t.Fatalf("GetOutgoingLinksForPage(C) failed: %v", err)
		}
		if outC.Count != 1 {
			t.Fatalf("expected 1 outgoing for C, got %d: %#v", outC.Count, outC.Outgoings)
		}
		if outC.Outgoings[0].Broken {
			t.Fatalf("expected C link to be healed, got %#v", outC.Outgoings[0])
		}
		if outC.Outgoings[0].ToPageID != pageD.ID {
			t.Fatalf("C ToPageID = %q, want %q", outC.Outgoings[0].ToPageID, pageD.ID)
		}

	})
})

var _ = ginkgo.Describe("TestLinkService_UpdateLinksAndHealForPages_DoesNotHealUnknownExtensionlessLinksIntoPages", func() {
	ginkgo.It("preserves behavior", func() {
		t := ginkgo.GinkgoT()
		svc, ts, _ := setupLinkService(t)

		sourceIDPtr, err := ts.CreateNode("system", nil, "Source", "source", pageNodeKind())
		if err != nil {
			t.Fatalf("CreateNode source failed: %v", err)
		}
		source, err := ts.GetPage(*sourceIDPtr)
		if err != nil {
			t.Fatalf("GetPage source failed: %v", err)
		}
		content := "[Legacy](/target)"
		if err := ts.UpdateNodeUncheckedVersion(newFixtureUserID("system"), newFixturePageID(source.ID), source.Title, newFixtureSlug(source.Slug), &content, false); err != nil {
			t.Fatalf("UpdateNode source failed: %v", err)
		}

		if err := svc.IndexAllPages(); err != nil {
			t.Fatalf("IndexAllPages failed: %v", err)
		}

		pageIDPtr, err := ts.CreateNode("system", nil, "Target", "target", pageNodeKind())
		if err != nil {
			t.Fatalf("CreateNode target page failed: %v", err)
		}
		pageTarget, err := ts.GetPage(*pageIDPtr)
		if err != nil {
			t.Fatalf("GetPage target page failed: %v", err)
		}
		if err := svc.UpdateLinksAndHealForPages([]*tree.Page{pageTarget}); err != nil {
			t.Fatalf("UpdateLinksAndHealForPages page target failed: %v", err)
		}

		pageBacklinks, err := svc.GetBacklinksForPage(pageTarget.ID)
		if err != nil {
			t.Fatalf("GetBacklinksForPage page target failed: %v", err)
		}
		if pageBacklinks.Count != 0 {
			t.Fatalf("page target backlinks = %#v, want none for non-canonical extensionless link", pageBacklinks.Backlinks)
		}
		outAfterPage, err := svc.GetOutgoingLinksForPage(source.ID)
		if err != nil {
			t.Fatalf("GetOutgoingLinksForPage after page target failed: %v", err)
		}
		if outAfterPage.Count != 1 || !outAfterPage.Outgoings[0].Broken || outAfterPage.Outgoings[0].ToPageID != "" {
			t.Fatalf("outgoing after page target = %#v, want unresolved extensionless link to stay broken", outAfterPage.Outgoings)
		}

		sectionIDPtr, err := ts.CreateNode("system", nil, "Target Section", "target", sectionNodeKind())
		if err != nil {
			t.Fatalf("CreateNode target section failed: %v", err)
		}
		sectionTarget, err := ts.GetPage(*sectionIDPtr)
		if err != nil {
			t.Fatalf("GetPage target section failed: %v", err)
		}
		if err := svc.UpdateLinksAndHealForPages([]*tree.Page{sectionTarget}); err != nil {
			t.Fatalf("UpdateLinksAndHealForPages section target failed: %v", err)
		}

		sectionBacklinks, err := svc.GetBacklinksForPage(sectionTarget.ID)
		if err != nil {
			t.Fatalf("GetBacklinksForPage section target failed: %v", err)
		}
		if sectionBacklinks.Count != 1 || sectionBacklinks.Backlinks[0].FromPageID != source.ID {
			t.Fatalf("section target backlinks = %#v, want source backlink", sectionBacklinks.Backlinks)
		}

	})
})

var _ = ginkgo.Describe("TestLinkService_UpdateLinksAndHealForPages_ReusesMarkdownIndexForRootBackedBatch", func() {
	ginkgo.It("preserves behavior", func() {
		t := ginkgo.GinkgoT()
		dataDir := t.TempDir()
		rootDir := filepath.Join(t.TempDir(), "workspace")
		writeLinkServiceMarkdown(t, filepath.Join(rootDir, "source-a.md"), `---
leafwiki_id: source-a
leafwiki_title: Source A
---
[Target](/target.md)
`)
		writeLinkServiceMarkdown(t, filepath.Join(rootDir, "source-b.md"), `---
leafwiki_id: source-b
leafwiki_title: Source B
---
[Target](/target.md)
`)
		writeLinkServiceMarkdown(t, filepath.Join(rootDir, "target.md"), `---
leafwiki_id: target
leafwiki_title: Target
---
# Target
`)

		ts := tree.NewTreeServiceWithOptions(tree.TreeOptions{DataDir: dataDir, RootDir: rootDir})
		if err := ts.LoadTree(); err != nil {
			t.Fatalf("LoadTree failed: %v", err)
		}
		store, err := NewLinksStore(dataDir)
		if err != nil {
			t.Fatalf("NewLinksStore failed: %v", err)
		}
		svc := NewLinkService(dataDir, ts, store)
		sourceA, err := ts.GetPage("source-a")
		if err != nil {
			t.Fatalf("GetPage source-a failed: %v", err)
		}
		sourceB, err := ts.GetPage("source-b")
		if err != nil {
			t.Fatalf("GetPage source-b failed: %v", err)
		}
		calls := countMarkdownRootIndexBuilds(t, markdownlinks.NewIndex([]markdownlinks.Entry{
			{Kind: markdownlinks.EntryKindPage, Path: "source-a.md"},
			{Kind: markdownlinks.EntryKindPage, Path: "source-b.md"},
			{Kind: markdownlinks.EntryKindPage, Path: "target.md"},
		}))

		if err := svc.UpdateLinksAndHealForPages([]*tree.Page{sourceA, sourceB}); err != nil {
			t.Fatalf("UpdateLinksAndHealForPages failed: %v", err)
		}

		if *calls != 1 {
			t.Fatalf("root-backed markdown index built %d times, want 1", *calls)
		}

	})
})

var _ = ginkgo.Describe("TestLinkService_UpdateLinksAndHealForPages_AllNilBatchDoesNotBuildMarkdownIndex", func() {
	ginkgo.It("preserves behavior", func() {
		t := ginkgo.GinkgoT()
		dataDir := t.TempDir()
		rootDir := filepath.Join(t.TempDir(), "workspace")
		writeLinkServiceMarkdown(t, filepath.Join(rootDir, "target.md"), "# Target\n")

		ts := tree.NewTreeServiceWithOptions(tree.TreeOptions{DataDir: dataDir, RootDir: rootDir})
		if err := ts.LoadTree(); err != nil {
			t.Fatalf("LoadTree failed: %v", err)
		}
		store, err := NewLinksStore(dataDir)
		if err != nil {
			t.Fatalf("NewLinksStore failed: %v", err)
		}
		svc := NewLinkService(dataDir, ts, store)
		calls := countMarkdownRootIndexBuilds(t, markdownlinks.NewIndex(nil))

		if err := svc.UpdateLinksAndHealForPages([]*tree.Page{nil}); err != nil {
			t.Fatalf("UpdateLinksAndHealForPages failed: %v", err)
		}

		if *calls != 0 {
			t.Fatalf("root-backed markdown index built %d times, want 0", *calls)
		}

	})
})

var _ = ginkgo.Describe("TestLinkService_UpdateRewrittenLinksAndHealForPages_ReusesMarkdownIndexForRootBackedBatch", func() {
	ginkgo.It("preserves behavior", func() {
		t := ginkgo.GinkgoT()
		dataDir := t.TempDir()
		rootDir := filepath.Join(t.TempDir(), "workspace")
		writeLinkServiceMarkdown(t, filepath.Join(rootDir, "source-a.md"), `---
leafwiki_id: source-a
leafwiki_title: Source A
---
[Old](/old-target.md)
`)
		writeLinkServiceMarkdown(t, filepath.Join(rootDir, "source-b.md"), `---
leafwiki_id: source-b
leafwiki_title: Source B
---
[Old](/old-target.md)
`)
		writeLinkServiceMarkdown(t, filepath.Join(rootDir, "old-target.md"), `---
leafwiki_id: old-target
leafwiki_title: Old Target
---
# Old Target
`)
		writeLinkServiceMarkdown(t, filepath.Join(rootDir, "new-target.md"), `---
leafwiki_id: new-target
leafwiki_title: New Target
---
# New Target
`)

		ts := tree.NewTreeServiceWithOptions(tree.TreeOptions{DataDir: dataDir, RootDir: rootDir})
		if err := ts.LoadTree(); err != nil {
			t.Fatalf("LoadTree failed: %v", err)
		}
		store, err := NewLinksStore(dataDir)
		if err != nil {
			t.Fatalf("NewLinksStore failed: %v", err)
		}
		svc := NewLinkService(dataDir, ts, store)
		if err := svc.IndexAllPages(); err != nil {
			t.Fatalf("IndexAllPages failed: %v", err)
		}
		sourceA, err := ts.GetPage("source-a")
		if err != nil {
			t.Fatalf("GetPage source-a failed: %v", err)
		}
		sourceB, err := ts.GetPage("source-b")
		if err != nil {
			t.Fatalf("GetPage source-b failed: %v", err)
		}
		calls := countMarkdownRootIndexBuilds(t, markdownlinks.NewIndex([]markdownlinks.Entry{
			{Kind: markdownlinks.EntryKindPage, Path: "source-a.md"},
			{Kind: markdownlinks.EntryKindPage, Path: "source-b.md"},
			{Kind: markdownlinks.EntryKindPage, Path: "old-target.md"},
			{Kind: markdownlinks.EntryKindPage, Path: "new-target.md"},
		}))

		if err := svc.UpdateRewrittenLinksAndHealForPages([]*tree.Page{sourceA, sourceB}, []RewriteRule{{
			OldPath: "/old-target",
			NewPath: "/new-target",
			Kind:    "page",
		}}); err != nil {
			t.Fatalf("UpdateRewrittenLinksAndHealForPages failed: %v", err)
		}

		if *calls != 1 {
			t.Fatalf("root-backed markdown index built %d times, want 1", *calls)
		}

	})
})

var _ = ginkgo.Describe("TestLinkService_UpdateRewrittenLinksAndHealForPages_AllNilBatchDoesNotBuildMarkdownIndex", func() {
	ginkgo.It("preserves behavior", func() {
		t := ginkgo.GinkgoT()
		dataDir := t.TempDir()
		rootDir := filepath.Join(t.TempDir(), "workspace")
		writeLinkServiceMarkdown(t, filepath.Join(rootDir, "target.md"), "# Target\n")

		ts := tree.NewTreeServiceWithOptions(tree.TreeOptions{DataDir: dataDir, RootDir: rootDir})
		if err := ts.LoadTree(); err != nil {
			t.Fatalf("LoadTree failed: %v", err)
		}
		store, err := NewLinksStore(dataDir)
		if err != nil {
			t.Fatalf("NewLinksStore failed: %v", err)
		}
		svc := NewLinkService(dataDir, ts, store)
		calls := countMarkdownRootIndexBuilds(t, markdownlinks.NewIndex(nil))

		if err := svc.UpdateRewrittenLinksAndHealForPages([]*tree.Page{nil}, nil); err != nil {
			t.Fatalf("UpdateRewrittenLinksAndHealForPages failed: %v", err)
		}

		if *calls != 0 {
			t.Fatalf("root-backed markdown index built %d times, want 0", *calls)
		}

	})
})

var _ = ginkgo.Describe("TestLinkService_UpdateLinksAndHealForPages_ReindexesOutgoingForSourcePages", func() {
	ginkgo.It("preserves behavior", func() {
		t := ginkgo.GinkgoT()
		svc, ts, _ := setupLinkService(t)

		sourceIDPtr, err := ts.CreateNode("system", nil, "Source", "source", pageNodeKind())
		if err != nil {
			t.Fatalf("CreateNode source failed: %v", err)
		}
		sourceID := *sourceIDPtr

		oldTargetIDPtr, err := ts.CreateNode("system", nil, "Old Target", "old-target", pageNodeKind())
		if err != nil {
			t.Fatalf("CreateNode old target failed: %v", err)
		}
		oldTargetID := *oldTargetIDPtr

		source, err := ts.GetPage(newFixturePageID(sourceID))
		if err != nil {
			t.Fatalf("GetPage source failed: %v", err)
		}
		oldContent := "Link: [Old](/old-target.md)"
		if err := ts.UpdateNodeUncheckedVersion(newFixtureUserID("system"), newFixturePageID(source.ID), source.Title, newFixtureSlug(source.Slug), &oldContent, false); err != nil {
			t.Fatalf("UpdateNode source failed: %v", err)
		}

		if err := svc.IndexAllPages(); err != nil {
			t.Fatalf("IndexAllPages failed: %v", err)
		}

		newTargetIDPtr, err := ts.CreateNode("system", nil, "New Target", "new-target", pageNodeKind())
		if err != nil {
			t.Fatalf("CreateNode new target failed: %v", err)
		}
		newTargetID := *newTargetIDPtr

		updatedContent := "Link: [New](/new-target.md)"
		if err := ts.UpdateNodeUncheckedVersion(newFixtureUserID("system"), newFixturePageID(source.ID), source.Title, newFixtureSlug(source.Slug), &updatedContent, false); err != nil {
			t.Fatalf("UpdateNode source (rewrite) failed: %v", err)
		}

		updatedSource, err := ts.GetPage(newFixturePageID(sourceID))
		if err != nil {
			t.Fatalf("GetPage source (updated) failed: %v", err)
		}

		if err := svc.UpdateLinksAndHealForPages([]*tree.Page{updatedSource}); err != nil {
			t.Fatalf("UpdateLinksAndHealForPages failed: %v", err)
		}

		oldBacklinks, err := svc.GetBacklinksForPage(newFixturePageID(oldTargetID))
		if err != nil {
			t.Fatalf("GetBacklinksForPage(old target) failed: %v", err)
		}
		if oldBacklinks.Count != 0 {
			t.Fatalf("expected 0 backlinks for old target, got %d: %#v", oldBacklinks.Count, oldBacklinks.Backlinks)
		}

		newBacklinks, err := svc.GetBacklinksForPage(newFixturePageID(newTargetID))
		if err != nil {
			t.Fatalf("GetBacklinksForPage(new target) failed: %v", err)
		}
		if newBacklinks.Count != 1 {
			t.Fatalf("expected 1 backlink for new target, got %d: %#v", newBacklinks.Count, newBacklinks.Backlinks)
		}
		if newBacklinks.Backlinks[0].FromPageID != newFixturePageID(sourceID) {
			t.Fatalf("new backlink FromPageID = %q, want %q", newBacklinks.Backlinks[0].FromPageID, sourceID)
		}

	})
})
