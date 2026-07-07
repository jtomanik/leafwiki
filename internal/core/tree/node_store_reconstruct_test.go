package tree

import (
	"os"
	"path/filepath"

	"github.com/perber/wiki/internal/core/markdown"

	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// Canonical Markdown links plan scenarios covered by tests in this file:
// - index.md has precedence over README.md
// - README.md is fallback section default
// - root README.md is fallback only without root index.md
// - root index.md has precedence over root README.md
// - Uppercase INDEX.MD does not become a second child page when accepted by current index lookup rules
// Plantrace evidence: TestNodeStore_ReconstructTreeFromFS_IndexBeatsReadmeAndReadmeIsSeparatePage.
// Plantrace evidence: TestNodeStore_ReconstructTreeFromFS_ReadmeFallbackSectionWhenNoIndexExists.
// Plantrace evidence: TestNodeStore_ReconstructTreeFromFS_RootReadmeFallbackSectionWhenNoIndexExists.
// Plantrace evidence: TestNodeStore_ReconstructTreeFromFS_RootIndexBeatsRootReadme.
// Plantrace evidence: TestNodeStore_ReconstructTreeFromFS_UsesUppercaseSectionIndex.

func findChildBySlug(parent *PageNode, slug Slug) *PageNode {
	ginkgo.GinkgoHelper()
	wantSlug := slug
	for _, ch := range parent.Children {
		if ch.Slug == wantSlug {
			return ch
		}
	}
	Expect(parent.Children).To(ContainElement(HaveField("Slug", Equal(wantSlug))))
	return nil
}

func slugs(children []*PageNode) []string {
	out := make([]string, 0, len(children))
	for _, c := range children {
		out = append(out, c.Slug.String())
	}
	return out
}

// --- tests ---

var _ = ginkgo.Describe("node store filesystem reconstruction", ginkgo.Label("unit"), func() {
	ginkgo.It("empty storage returns root", func() {
		tmp := tempTreeDir()
		store := NewNodeStore(tmp)

		tree, err := store.ReconstructTreeFromFS()
		Expect(err).To(Succeed(), "ReconstructTreeFromFS: %v",

			err)
		Expect(tree).To(matchRootSection(), "unexpected root: %#v", tree)

	})
})

var _ = ginkgo.Describe("node store filesystem reconstruction", ginkgo.Label("unit"), func() {
	ginkgo.It("builds sections and pages skips index markdown as page", func() {
		tmp := tempTreeDir()
		store := NewNodeStore(tmp)

		// FS layout:
		// <tmp>/docs/index.md (section content)
		// <tmp>/docs/intro.md (page)
		// <tmp>/readme.md (page at root)
		createTreeDirectory(filepath.Join(tmp, "root", "docs"))

		secIndex := `---
leafwiki_id: sec-docs
leafwiki_title: Documentation
---
# Section`
		writeTreeFile(filepath.Join(tmp, "root", "docs", "index.md"), secIndex, 0o644)

		pageIntro := `---
leafwiki_id: page-intro
leafwiki_title: Introduction
---
# Intro`
		writeTreeFile(filepath.Join(tmp, "root", "docs", "intro.md"), pageIntro, 0o644)

		rootPage := `---
leafwiki_id: page-readme
leafwiki_title: Readme
---
# Readme`
		writeTreeFile(filepath.Join(tmp, "root", "readme.md"), rootPage, 0o644)

		tree, err := store.ReconstructTreeFromFS()
		Expect(err).To(Succeed(), "ReconstructTreeFromFS: %v",

			err)

		// root has: docs(section), readme(page)
		docs := findChildBySlug(tree, newFixtureSlug("docs"))
		Expect(docs).To(matchTreeNode(NodeKindSection, newFixturePageID("sec-docs"), "Documentation"))

		// section title/id from index frontmatter

		// ensure index.md wasn't turned into a page child
		for _, ch := range docs.Children {
			Expect(ch.Slug).NotTo(Equal(newFixtureSlug("index")),
				"index.md must be skipped as page, but found slug index",
			)

		}

		intro := findChildBySlug(docs, newFixtureSlug("intro"))
		Expect(intro).To(matchTreeNode(NodeKindPage, newFixturePageID("page-intro"), "Introduction"))

		// page title/id from frontmatter

		readme := findChildBySlug(tree, newFixtureSlug("readme"))
		Expect(readme).To(matchTreeNode(NodeKindPage, newFixturePageID("page-readme"), "Readme"))
		Expect(docs.Parent).To(haveParentPageID(RootPageID), "expected docs parent root, got %#v", docs.Parent)
		Expect(intro.Parent).To(haveParentPageID(docs.ID), "expected intro parent docs, got %#v", intro.Parent)

		// parent pointers

	})
})

// - Uppercase INDEX.MD does not become a second child page when accepted by current index lookup rules
var _ = ginkgo.Describe("node store filesystem reconstruction", ginkgo.Label("unit"), func() {
	ginkgo.It("uses uppercase section index", func() {
		tmp := tempTreeDir()
		store := NewNodeStore(tmp)

		sectionDir := filepath.Join(tmp, "root", "docs")
		createTreeDirectory(sectionDir)
		indexPath := filepath.Join(sectionDir, "INDEX.MD")
		writeTreeFile(indexPath, `---
leafwiki_id: sec-docs
leafwiki_title: Documentation
---
# Section
`, 0o644)
		writeTreeFile(filepath.Join(sectionDir, "intro.md"), `---
leafwiki_id: page-intro
leafwiki_title: Introduction
---
# Intro
`, 0o644)

		resolvedIndexPath, err := sectionIndexPathInDirResult(store, sectionDir)
		Expect(err).To(Succeed(), "sectionIndexPathInDir: %v",

			err)
		Expect(filepath.Base(resolvedIndexPath)).To(Equal("INDEX.MD"),
			"sectionIndexPathInDir path = %q, want INDEX.MD",

			resolvedIndexPath,
		)

		tree, err := store.ReconstructTreeFromFS()
		Expect(err).To(Succeed(), "ReconstructTreeFromFS: %v",

			err)

		docs := findChildBySlug(tree, newFixtureSlug("docs"))
		Expect(docs).To(matchTreeNode(NodeKindSection, newFixturePageID("sec-docs"), "Documentation"))

		Expect(docs.Children).NotTo(ContainElement(
			HaveField("Slug", Equal(newFixtureSlug("index"))),
		), "INDEX.MD must be skipped as a child page")

		raw, err := store.ReadPageRaw(docs)
		Expect(err).To(Succeed(), "ReadPageRaw section: %v",

			err,
		)
		Expect(raw).To(ContainSubstring("# Section"),

			"section raw content = %q, want INDEX.MD body",

			raw)

		entries, err := os.ReadDir(sectionDir)
		Expect(err).To(Succeed(), "ReadDir section: %v",

			err)

		for _, entry := range entries {
			Expect(entry.Name()).
				NotTo(Equal("index.md"), "reconstruct materialized lowercase index.md alongside INDEX.MD")

		}

		mdFile, err := markdown.LoadMarkdownFile(indexPath)
		Expect(err).To(Succeed(), "LoadMarkdownFile INDEX.MD: %v",

			err)

		frontmatter := mdFile.GetFrontmatter()
		Expect(frontmatter).To(SatisfyAll(
			HaveField("LeafWikiID", Equal("sec-docs")),
			HaveField("LeafWikiTitle", Equal("Documentation")),
			HaveField("LeafWikiCreatedAt", Not(BeEmpty())),
			HaveField("LeafWikiUpdatedAt", Not(BeEmpty())),
		))

	})
})

// - README.md is fallback section default
var _ = ginkgo.Describe("node store filesystem reconstruction", ginkgo.Label("unit"), func() {
	ginkgo.It("README fallback section when no index exists", func() {
		tmp := tempTreeDir()
		store := NewNodeStore(tmp)

		sectionDir := filepath.Join(tmp, "root", "docs")
		createTreeDirectory(sectionDir)
		readmePath := filepath.Join(sectionDir, "README.md")
		writeTreeFile(readmePath, `---
leafwiki_id: sec-docs
leafwiki_title: Documentation
---
# Section readme
`, 0o644)

		tree, err := store.ReconstructTreeFromFS()
		Expect(err).To(Succeed(), "ReconstructTreeFromFS: %v",

			err)

		docs := findChildBySlug(tree, newFixtureSlug("docs"))
		Expect(docs).To(matchTreeNode(NodeKindSection, newFixturePageID("sec-docs"), "Documentation"))

		Expect(docs.Children).NotTo(ContainElement(
			HaveField("Slug", Equal(newFixtureSlug("readme"))),
		), "README.md fallback must not be reconstructed as a child page")

		raw, err := store.ReadPageRaw(docs)
		Expect(err).To(Succeed(), "ReadPageRaw section: %v",

			err,
		)
		Expect(raw).To(ContainSubstring("# Section readme"),

			"section raw content = %q, want README.md body",

			raw)
		{

			_, err := os.Stat(filepath.Join(sectionDir, "index.md"))
			Expect(err).To(MatchError(os.
				ErrNotExist,
			),

				"README.md fallback must not materialize index.md, stat err = %v",

				err)
		}

	})
})

// - index.md has precedence over README.md
var _ = ginkgo.Describe("node store filesystem reconstruction", ginkgo.Label("unit"), func() {
	ginkgo.It("index beats README and README is separate page", func() {
		tmp := tempTreeDir()
		store := NewNodeStore(tmp)

		sectionDir := filepath.Join(tmp, "root", "docs")
		createTreeDirectory(sectionDir)
		writeTreeFile(filepath.Join(sectionDir, "index.md"), `---
leafwiki_id: sec-docs
leafwiki_title: Documentation
---
# Index section
`, 0o644)
		writeTreeFile(filepath.Join(sectionDir, "README.md"), `---
leafwiki_id: page-readme
leafwiki_title: Readme Page
---
# Readme page
`, 0o644)

		tree, err := store.ReconstructTreeFromFS()
		Expect(err).To(Succeed(), "ReconstructTreeFromFS: %v",

			err)

		docs := findChildBySlug(tree, newFixtureSlug("docs"))
		Expect(docs).To(matchTreeNode(NodeKindSection, newFixturePageID("sec-docs"), "Documentation"),
			"docs = %#v, want section from index.md", docs)

		raw, err := store.ReadPageRaw(docs)
		Expect(err).To(Succeed(), "ReadPageRaw docs: %v",

			err)
		Expect(raw).To(ContainSubstring("# Index section"),

			"docs raw = %q, want index.md content",

			raw)

		readme := findChildBySlug(docs, newFixtureSlug("README"))
		Expect(readme).To(matchTreeNode(NodeKindPage, newFixturePageID("page-readme"), "Readme Page"),
			"README child = %#v, want separate page from README.md", readme)

	})
})

var _ = ginkgo.Describe("node store filesystem reconstruction", ginkgo.Label("unit"), func() {
	ginkgo.It("allows page and section with same basename", func() {
		tmp := tempTreeDir()
		store := NewNodeStore(tmp)

		docsDir := filepath.Join(tmp, "root", "docs")
		createTreeDirectory(filepath.Join(docsDir, "sync"))
		writeTreeFile(filepath.Join(docsDir, "index.md"), `---
leafwiki_id: sec-docs
leafwiki_title: Documentation
---
# Documentation
`, 0o644)
		writeTreeFile(filepath.Join(docsDir, "sync.md"), `---
leafwiki_id: page-sync
leafwiki_title: Sync Page
---
# Sync Page
`, 0o644)
		writeTreeFile(filepath.Join(docsDir, "sync", "index.md"), `---
leafwiki_id: sec-sync
leafwiki_title: Sync Section
---
# Sync Section
`, 0o644)

		tree, err := store.ReconstructTreeFromFS()
		Expect(err).To(Succeed(), "ReconstructTreeFromFS: %v",

			err)

		docs := findChildBySlug(tree, newFixtureSlug("docs"))
		var syncPage, syncSection *PageNode
		for _, ch := range docs.Children {
			if ch.Slug == "sync" && ch.Kind == NodeKindPage {
				syncPage = ch
			}
			if ch.Slug == "sync" && ch.Kind == NodeKindSection {
				syncSection = ch
			}
		}
		Expect(syncPage).NotTo(BeNil(),
			"docs children = %#v, want sync.md page child",

			docs.
				Children)
		Expect(syncSection).
			NotTo(BeNil(), "docs children = %#v, want sync/ section child",

				docs.Children)
		Expect(syncPage.ID).
			To(Equal(newFixturePageID("page-sync")), "sync page ID = %q, want page-sync",

				syncPage.
					ID)
		Expect(syncSection.ID).To(Equal(newFixturePageID("sec-sync")), "sync section ID = %q, want sec-sync",

			syncSection.ID)

	})
})

// - root README.md is fallback only without root index.md
var _ = ginkgo.Describe("node store filesystem reconstruction", ginkgo.Label("unit"), func() {
	ginkgo.It("root README fallback section when no index exists", func() {
		tmp := tempTreeDir()
		store := NewNodeStore(tmp)

		createTreeDirectory(filepath.Join(tmp, "root"))
		writeTreeFile(filepath.Join(tmp, "root", "README.md"), `---
leafwiki_id: root
leafwiki_title: Root Readme
---
# Root readme
`, 0o644)

		tree, err := store.ReconstructTreeFromFS()
		Expect(err).To(Succeed(), "ReconstructTreeFromFS: %v",

			err)
		Expect(tree).To(matchTreeNode(NodeKindSection, RootPageID, "Root Readme"),
			"root = %#v, want stable root ID with README title", tree)
		Expect(tree.Children).To(HaveLen(0),
			"root children = %v, want README.md used as root content only",

			slugs(tree.Children))

		raw, err := store.ReadPageRaw(tree)
		Expect(err).To(Succeed(), "ReadPageRaw root: %v",

			err)
		Expect(raw).To(ContainSubstring("# Root readme"),

			"root raw = %q, want README.md body",

			raw)

	})
})
