package tree

import (
	"os"
	"path/filepath"
	"strings"

	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = ginkgo.Describe("route path generation from page nodes", ginkgo.Label("unit"), func() {
	ginkgo.It("uses an empty route for the root and appends ancestor slugs for descendants", func() {
		root := &PageNode{ID: newFixturePageID("root"), Slug: newFixtureSlug("root"), Title: "root"}
		docs := &PageNode{ID: newFixturePageID("docs"), Slug: newFixtureSlug("docs"), Title: "Docs", Parent: root}
		guide := &PageNode{ID: newFixturePageID("guide"), Slug: newFixtureSlug("guide"), Title: "Guide", Parent: docs}

		Expect(GenerateRoutePathFromPageNode(root)).To(BeEmpty())
		Expect(GenerateRoutePathFromPageNode(docs)).To(Equal(newFixtureRoutePath("docs")))
		Expect(GenerateRoutePathFromPageNode(guide)).To(Equal(newFixtureRoutePath("docs/guide")))
	})
})

var _ = ginkgo.Describe("page folder materialization", ginkgo.Label("unit"), func() {
	ginkgo.It("converts a flat markdown file into a folder with index content", func() {
		tmp := tempTreeDir()
		pagePath := "docs/guide"
		flatFile := filepath.Join(tmp, "docs", "guide.md")

		Expect(os.MkdirAll(filepath.Dir(flatFile), 0o755)).To(Succeed())
		Expect(os.WriteFile(flatFile, []byte("# Guide"), 0o644)).To(Succeed())

		Expect(EnsurePageIsFolder(tmp, RoutePathFromString(pagePath))).To(Succeed())

		Expect(filepath.Join(tmp, "docs", "guide", "index.md")).To(BeAnExistingFile())
		Expect(flatFile).NotTo(BeAnExistingFile())
	})
})

var _ = ginkgo.Describe("empty page folder folding", ginkgo.Label("unit"), func() {
	ginkgo.It("folds an index-only folder back into a flat markdown file", func() {
		tmp := tempTreeDir()
		dir := filepath.Join(tmp, "docs", "guide")

		Expect(os.MkdirAll(dir, 0o755)).To(Succeed())
		Expect(os.WriteFile(filepath.Join(dir, "index.md"), []byte("# Guide"), 0o644)).To(Succeed())

		Expect(FoldPageFolderIfEmpty(tmp, "docs/guide")).To(Succeed())

		Expect(filepath.Join(tmp, "docs", "guide.md")).To(BeAnExistingFile())
		Expect(dir).NotTo(BeAnExistingFile())
	})
})

var _ = ginkgo.Describe("page disk path construction", ginkgo.Label("unit"), func() {
	ginkgo.It("joins Windows-style roots with route paths", func() {
		storageDir := `C:\wiki\data\root`
		pagePath := "docs/guide"

		Expect(strings.ReplaceAll(pageDirectoryDiskPath(storageDir, pagePath), `\`, `/`)).To(Equal(`C:/wiki/data/root/docs/guide`))
		Expect(strings.ReplaceAll(pageMarkdownDiskPath(storageDir, pagePath), `\`, `/`)).To(Equal(`C:/wiki/data/root/docs/guide.md`))
		Expect(strings.ReplaceAll(pageIndexDiskPath(storageDir, pagePath), `\`, `/`)).To(Equal(`C:/wiki/data/root/docs/guide/index.md`))
	})
})
