package importer

import (
	"os"

	"github.com/perber/wiki/internal/core/tree"

	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// Canonical Markdown links plan scenarios covered by tests in this file:
// - README.md as normal page keeps its filesystem casing in generated links

var _ = ginkgo.Describe("import plan root index fallback titles", ginkgo.Label("unit"), func() {
	ginkgo.It("uses the root index filename when title extraction fails", func() {
		// Test case for root-level index.md with empty TargetBasePath and markdown loading failure
		// When wikiPath is empty, path.Base("") returns ".", which is not meaningful.
		// The fix should use filename without extension as fallback.
		tmp := importerTempDir()
		abs := importerWriteFile(tmp, "index.md", "# Title")

		// Make file unreadable to trigger markdown loading failure
		Expect(os.Chmod(abs, 0o000)).To(Succeed())
		ginkgo.DeferCleanup(os.Chmod, abs, os.FileMode(0o644))

		wiki := &fakeWiki{treeHash: "h", lookups: map[string]*tree.PathLookup{}}
		p := newPlannerWithFake(wiki)

		res, err := p.CreatePlan([]ImportMDFile{{SourcePath: newFixtureWorkspaceSourcePath("index.md")}}, PlanOptions{
			SourceBasePath: tmp,
			TargetBasePath: "", // empty target base path
		})
		Expect(err).To(Succeed())
		Expect(res).To(HaveImportPlanResult(ConsistOf(SatisfyAll(
			HaveField("TargetPath", Equal(newFixtureRoutePath(""))),
			HaveField("Kind", Equal(tree.NodeKindSection)),
			HaveField("Title", Equal("index")),
			HaveField("Notes", ContainElement(ContainSubstring("Failed to load markdown file for title extraction"))),
		))))

	})
})

var _ = ginkgo.Describe("import plan folder index and sibling pages", ginkgo.Label("unit"), func() {
	ginkgo.It("creates section and sibling page items for folder imports", func() {
		tmp := importerTempDir()
		importerWriteFile(tmp, "Ordner/index.md", `---
title: Ordner
---

# Ordner`)
		importerWriteFile(tmp, "Ordner/Ordner.md", "# Unterseite")

		wiki := &fakeWiki{treeHash: "h", lookups: map[string]*tree.PathLookup{}}
		p := newPlannerWithFake(wiki)

		res, err := p.CreatePlan([]ImportMDFile{
			{SourcePath: newFixtureWorkspaceSourcePath("Ordner/index.md")},
			{SourcePath: newFixtureWorkspaceSourcePath("Ordner/Ordner.md")},
		}, PlanOptions{
			SourceBasePath: tmp,
			TargetBasePath: "",
		})
		Expect(err).To(Succeed())
		Expect(res).To(HaveImportPlanResult(ConsistOf(
			SatisfyAll(
				HaveField("SourcePath", Equal(newFixtureWorkspaceSourcePath("Ordner/index.md"))),
				HaveField("Kind", Equal(tree.NodeKindSection)),
				HaveField("TargetPath", Equal(newFixtureRoutePath("ordner"))),
			),
			SatisfyAll(
				HaveField("SourcePath", Equal(newFixtureWorkspaceSourcePath("Ordner/Ordner.md"))),
				HaveField("Kind", Equal(tree.NodeKindPage)),
				HaveField("TargetPath", Equal(newFixtureRoutePath("ordner/ordner"))),
			),
		)))

	})
})

var _ = ginkgo.Describe("import plan folder markdown without index", ginkgo.Label("unit"), func() {
	ginkgo.It("creates folder markdown as pages when no index exists", func() {
		tmp := importerTempDir()
		importerWriteFile(tmp, "Ordner/Ordner.md", "# Unterseite")

		wiki := &fakeWiki{treeHash: "h", lookups: map[string]*tree.PathLookup{}}
		p := newPlannerWithFake(wiki)

		res, err := p.CreatePlan([]ImportMDFile{{SourcePath: newFixtureWorkspaceSourcePath("Ordner/Ordner.md")}}, PlanOptions{
			SourceBasePath: tmp,
			TargetBasePath: "wiki",
		})
		Expect(err).To(Succeed())
		Expect(res).To(HaveImportPlanResult(ConsistOf(SatisfyAll(
			HaveField("Kind", Equal(tree.NodeKindPage)),
			HaveField("TargetPath", Equal(newFixtureRoutePath("wiki/ordner/ordner"))),
		))))

	})
})

var _ = ginkgo.Describe("import plan uppercase index sections", ginkgo.Label("unit"), func() {
	ginkgo.It("treats uppercase index files as section indexes", func() {
		tmp := importerTempDir()
		importerWriteFile(tmp, "Guides/index.MD", `---
title: Guides
---

# Ignored`)

		wiki := &fakeWiki{treeHash: "h", lookups: map[string]*tree.PathLookup{}}
		p := newPlannerWithFake(wiki)

		res, err := p.CreatePlan([]ImportMDFile{{SourcePath: newFixtureWorkspaceSourcePath("Guides/index.MD")}}, PlanOptions{
			SourceBasePath: tmp,
			TargetBasePath: "docs",
		})
		Expect(err).To(Succeed())
		Expect(res.Items).To(ConsistOf(SatisfyAll(
			HaveField("Kind", Equal(tree.NodeKindSection)),
			HaveField("TargetPath", Equal(newFixtureRoutePath("docs/guides"))),
		)))

	})
})
