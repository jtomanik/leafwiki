package links

import (
	"os"
	"path/filepath"

	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/perber/wiki/internal/core/markdownlinks"
	"github.com/perber/wiki/internal/core/tree"
)

// Canonical Markdown links plan scenarios covered by tests in this package:
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

	aPage, err := ts.GetPage(pageAID)
	Expect(err).NotTo(HaveOccurred())
	contentA := "Link to B: [Go to B](/b.md)"
	Expect(ts.UpdateNodeUncheckedVersion(newFixtureUserID("system"), aPage.ID, aPage.Title, aPage.Slug, &contentA, false)).To(Succeed())

	bPage, err := ts.GetPage(pageBID)
	Expect(err).NotTo(HaveOccurred())
	contentB := "# Page B\nNo outgoing links."
	Expect(ts.UpdateNodeUncheckedVersion(newFixtureUserID("system"), bPage.ID, bPage.Title, bPage.Slug, &contentB, false)).To(Succeed())

	return pageAID, pageBID
}
