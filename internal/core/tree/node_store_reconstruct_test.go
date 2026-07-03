package tree

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

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

func findChildBySlug(parent *PageNode, slug string) *PageNode {
	ginkgo.GinkgoHelper()
	wantSlug := newFixtureSlug(slug)
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

var _ = ginkgo.Describe("node store filesystem reconstruction", func() {
	ginkgo.It("empty storage returns root", func() {
		tmp := tempTreeDir()
		store := NewNodeStore(tmp)

		tree, err := store.ReconstructTreeFromFS()
		Expect(err).To(Succeed(), "ReconstructTreeFromFS: %v",

			err)
		Expect(tree).To(matchRootSection(), "unexpected root: %#v", tree)

	})
})

var _ = ginkgo.Describe("node store filesystem reconstruction", func() {
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
		docs := findChildBySlug(tree, "docs")
		Expect(docs).To(matchTreeNode(NodeKindSection, newFixturePageID("sec-docs"), "Documentation"))

		// section title/id from index frontmatter

		// ensure index.md wasn't turned into a page child
		for _, ch := range docs.Children {
			Expect(ch.Slug).NotTo(Equal(newFixtureSlug("index")),
				"index.md must be skipped as page, but found slug index",
			)

		}

		intro := findChildBySlug(docs, "intro")
		Expect(intro).To(matchTreeNode(NodeKindPage, newFixturePageID("page-intro"), "Introduction"))

		// page title/id from frontmatter

		readme := findChildBySlug(tree, "readme")
		Expect(readme).To(matchTreeNode(NodeKindPage, newFixturePageID("page-readme"), "Readme"))
		Expect(docs.Parent).To(haveParentPageID(RootPageID), "expected docs parent root, got %#v", docs.Parent)
		Expect(intro.Parent).To(haveParentPageID(docs.ID), "expected intro parent docs, got %#v", intro.Parent)

		// parent pointers

	})
})

// - Uppercase INDEX.MD does not become a second child page when accepted by current index lookup rules
var _ = ginkgo.Describe("node store filesystem reconstruction", func() {
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

		resolvedIndexPath, hasIndex, err := store.sectionIndexPathInDir(sectionDir)
		Expect(err).To(Succeed(), "sectionIndexPathInDir: %v",

			err)
		Expect(hasIndex).To(
			BeTrue(),
			"sectionIndexPathInDir did not find INDEX.MD",
		)
		Expect(filepath.Base(resolvedIndexPath)).To(Equal("INDEX.MD"),
			"sectionIndexPathInDir path = %q, want INDEX.MD",

			resolvedIndexPath,
		)

		tree, err := store.ReconstructTreeFromFS()
		Expect(err).To(Succeed(), "ReconstructTreeFromFS: %v",

			err)

		docs := findChildBySlug(tree, "docs")
		Expect(docs).To(matchTreeNode(NodeKindSection, newFixturePageID("sec-docs"), "Documentation"))

		for _, ch := range docs.Children {
			Expect(strings.EqualFold(ch.Slug.
				String(), "index")).
				To(BeFalse(), "INDEX.MD must be skipped as page, but found slug %q",

					ch.Slug)

		}

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
var _ = ginkgo.Describe("node store filesystem reconstruction", func() {
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

		docs := findChildBySlug(tree, "docs")
		Expect(docs).To(matchTreeNode(NodeKindSection, newFixturePageID("sec-docs"), "Documentation"))

		for _, ch := range docs.Children {
			Expect(strings.EqualFold(ch.Slug.
				String(), "readme")).
				To(BeFalse(), "README.md fallback must not be reconstructed as a child page")

		}

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
var _ = ginkgo.Describe("node store filesystem reconstruction", func() {
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

		docs := findChildBySlug(tree, "docs")
		Expect(docs).To(matchTreeNode(NodeKindSection, newFixturePageID("sec-docs"), "Documentation"),
			"docs = %#v, want section from index.md", docs)

		raw, err := store.ReadPageRaw(docs)
		Expect(err).To(Succeed(), "ReadPageRaw docs: %v",

			err)
		Expect(raw).To(ContainSubstring("# Index section"),

			"docs raw = %q, want index.md content",

			raw)

		readme := findChildBySlug(docs, "README")
		Expect(readme).To(matchTreeNode(NodeKindPage, newFixturePageID("page-readme"), "Readme Page"),
			"README child = %#v, want separate page from README.md", readme)

	})
})

var _ = ginkgo.Describe("node store filesystem reconstruction", func() {
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

		docs := findChildBySlug(tree, "docs")
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
var _ = ginkgo.Describe("node store filesystem reconstruction", func() {
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

// - root index.md has precedence over root README.md
var _ = ginkgo.Describe("node store filesystem reconstruction", func() {
	ginkgo.It("root index beats root README", func() {
		tmp := tempTreeDir()
		store := NewNodeStore(tmp)

		createTreeDirectory(filepath.Join(tmp, "root"))
		writeTreeFile(filepath.Join(tmp, "root", "index.md"), `---
leafwiki_id: root
leafwiki_title: Root Index
---
# Root index
`, 0o644)
		writeTreeFile(filepath.Join(tmp, "root", "README.md"), `---
leafwiki_id: root-readme
leafwiki_title: Root Readme Page
---
# Root readme page
`, 0o644)

		tree, err := store.ReconstructTreeFromFS()
		Expect(err).To(Succeed(), "ReconstructTreeFromFS: %v",

			err)
		Expect(tree).To(matchTreeNode(NodeKindSection, RootPageID, "Root Index"),
			"root = %#v, want stable root ID with index title", tree)

		raw, err := store.ReadPageRaw(tree)
		Expect(err).To(Succeed(), "ReadPageRaw root: %v",

			err)
		Expect(raw).To(ContainSubstring("# Root index"),

			"root raw = %q, want index.md body",

			raw)

		readme := findChildBySlug(tree, "README")
		Expect(readme).To(matchTreeNode(NodeKindPage, newFixturePageID("root-readme"), "Root Readme Page"),
			"README child = %#v, want root README.md as separate page", readme)

	})
})

var _ = ginkgo.Describe("node store filesystem reconstruction", func() {
	ginkgo.It("section without index uses directory name as title and materializes index", func() {
		tmp := tempTreeDir()
		store := NewNodeStore(tmp)

		createTreeDirectory(filepath.Join(tmp, "root", "emptysec"))

		tree, err := store.ReconstructTreeFromFS()
		Expect(err).To(Succeed(), "ReconstructTreeFromFS: %v",

			err)

		sec := findChildBySlug(tree, "emptysec")
		Expect(sec).To(SatisfyAll(
			HaveField("Kind", Equal(NodeKindSection)),
			HaveField("Title", Equal("emptysec")),
			HaveField("ID", Not(BeEmpty())),
		), "unexpected materialized section: %#v", sec)

		indexPath := filepath.Join(tmp, "root", "emptysec", "index.md")
		raw, err := os.ReadFile(indexPath)
		Expect(err).To(Succeed(), "expected reconstruct to materialize missing index.md: %v",

			err)

		frontmatter, body, has, err := markdown.ParseFrontmatter(string(raw))
		Expect(err).To(Succeed(), "ParseFrontmatter: %v",

			err)
		Expect(has).To(BeTrue(), "expected frontmatter in materialized index")
		Expect(frontmatter).To(matchManagedFrontmatter(sec.ID, sec.Title),
			"unexpected frontmatter in materialized index: %#v", frontmatter)
		Expect(strings.TrimSpace(body)).To(BeEmpty(),

			"expected empty body in materialized index, got %q",

			body)

	})
})

var _ = ginkgo.Describe("node store filesystem reconstruction", func() {
	ginkgo.It("page without frontmatter falls back to headline title", func() {
		tmp := tempTreeDir()
		store := NewNodeStore(tmp)

		// FS: <tmp>/plain.md (no frontmatter)
		writeTreeFile(filepath.Join(tmp, "root", "plain.md"), "# hello\n", 0o644)

		tree, err := store.ReconstructTreeFromFS()
		Expect(err).To(Succeed(), "ReconstructTreeFromFS: %v",

			err)

		p := findChildBySlug(tree, "plain")
		Expect(p).To(SatisfyAll(
			HaveField("Kind", Equal(NodeKindPage)),
			HaveField("Title", Equal("hello")),
			HaveField("ID", Not(BeEmpty())),
		))

		// title fallback should be headline

		// should still have generated id (unless you later decide to keep empty)

	})
})

var _ = ginkgo.Describe("node store filesystem reconstruction", func() {
	ginkgo.It("positions are contiguous", func() {
		tmp := tempTreeDir()
		store := NewNodeStore(tmp)

		// Create several files/dirs
		writeTreeFile(filepath.Join(tmp, "root", "b.md"), "# b", 0o644)
		writeTreeFile(filepath.Join(tmp, "root", "a.md"), "# a", 0o644)
		createTreeDirectory(filepath.Join(tmp, "root", "zsec"))

		tree, err := store.ReconstructTreeFromFS()
		Expect(err).To(Succeed(), "ReconstructTreeFromFS: %v",

			err)

		// Positions should be 0..n-1 regardless of order
		seen := make([]int, 0, len(tree.Children))
		for _, ch := range tree.Children {
			seen = append(seen, ch.Position)
		}
		sort.Ints(seen)
		Expect(seen).To(HaveExactElements(0, 1, 2),
			"expected contiguous positions 0..%d, got %v (slugs=%v)",
			len(seen)-1, seen, slugs(tree.Children))

	})
})

var _ = ginkgo.Describe("node store filesystem reconstruction", func() {
	ginkgo.It("order file overrides default order", func() {
		tmp := tempTreeDir()
		store := NewNodeStore(tmp)

		writeTreeFile(filepath.Join(tmp, "root", "a.md"), "---\nleafwiki_id: id-a\n---\n# A", 0o644)
		writeTreeFile(filepath.Join(tmp, "root", "b.md"), "---\nleafwiki_id: id-b\n---\n# B", 0o644)
		writeTreeFile(filepath.Join(tmp, "root", "c.md"), "---\nleafwiki_id: id-c\n---\n# C", 0o644)

		orderRaw, err := json.Marshal(map[string][]string{
			"ordered_ids": {"id-c", "id-a"},
		})
		Expect(err).To(Succeed(), "marshal order file: %v",

			err,
		)

		writeTreeFile(filepath.Join(tmp, "root", ".order.json"), string(orderRaw), 0o644)

		tree, err := store.ReconstructTreeFromFS()
		Expect(err).To(Succeed(), "ReconstructTreeFromFS: %v",

			err)

		got := slugs(tree.Children)
		want := []string{"c", "a", "b"}
		Expect(strings.Join(
			got, ",")).
			To(Equal(strings.Join(
				want, ",",
			)), "unexpected child order: got %v want %v",

				got, want)

		for i, child := range tree.Children {
			Expect(child.Position).To(Equal(i),
				"expected child %q position %d, got %d",

				child.
					Slug, i, child.Position)

		}

	})
})

var _ = ginkgo.Describe("node store filesystem reconstruction", func() {
	ginkgo.It("order file ignores unknown IDs and keeps remaining stable", func() {
		tmp := tempTreeDir()
		store := NewNodeStore(tmp)

		writeTreeFile(filepath.Join(tmp, "root", "a.md"), "---\nleafwiki_id: id-a\n---\n# A", 0o644)
		writeTreeFile(filepath.Join(tmp, "root", "b.md"), "---\nleafwiki_id: id-b\n---\n# B", 0o644)
		writeTreeFile(filepath.Join(tmp, "root", "c.md"), "---\nleafwiki_id: id-c\n---\n# C", 0o644)

		orderRaw, err := json.Marshal(map[string][]string{
			"ordered_ids": {"missing-id", "id-b"},
		})
		Expect(err).To(Succeed(), "marshal order file: %v",

			err,
		)

		writeTreeFile(filepath.Join(tmp, "root", ".order.json"), string(orderRaw), 0o644)

		tree, err := store.ReconstructTreeFromFS()
		Expect(err).To(Succeed(), "ReconstructTreeFromFS: %v",

			err)

		got := slugs(tree.Children)
		want := []string{"b", "a", "c"}
		Expect(strings.Join(
			got, ",")).
			To(Equal(strings.Join(
				want, ",",
			)), "unexpected child order: got %v want %v",

				got, want)

	})
})

var _ = ginkgo.Describe("node store filesystem reconstruction", func() {
	ginkgo.It("returns an error on duplicate LeafWiki IDs", func() {
		tmp := tempTreeDir()
		store := NewNodeStore(tmp)

		writeTreeFile(filepath.Join(tmp, "root", "a.md"), `---
leafwiki_id: dup-id
leafwiki_title: A
---
# A`, 0o644)
		writeTreeFile(filepath.Join(tmp, "root", "b.md"), `---
leafwiki_id: dup-id
leafwiki_title: B
---
# B`, 0o644)

		_, err := store.ReconstructTreeFromFS()
		Expect(err).To(HaveOccurred(), "expected duplicate ID error")
		Expect(err).To(MatchError(ErrDuplicateLeafwikiID), "expected duplicate ID error, got: %v",

			err)

	})
})

var _ = ginkgo.Describe("node store filesystem reconstruction", func() {
	ginkgo.It("returns an error on duplicate canonical and legacy LeafWiki IDs", func() {
		tmp := tempTreeDir()
		store := NewNodeStore(tmp)

		writeTreeFile(filepath.Join(tmp, "root", "a.md"), `<!-- leafwiki
version: 1
page:
  id: mixed-dup-id
  title: A
-->

# A`, 0o644)
		writeTreeFile(filepath.Join(tmp, "root", "b.md"), `---
leafwiki_id: mixed-dup-id
leafwiki_title: B
---
# B`, 0o644)

		_, err := store.ReconstructTreeFromFS()
		Expect(err).To(HaveOccurred(), "expected duplicate ID error")
		Expect(err).To(MatchError(ErrDuplicateLeafwikiID), "expected duplicate ID error, got: %v",

			err)

	})
})

type malformedCanonicalMetadataCase struct {
	path string
	raw  string
}

var _ = ginkgo.DescribeTable("node store filesystem reconstruction returns an error on malformed canonical metadata",
	func(tt malformedCanonicalMetadataCase) {
		tmp := tempTreeDir()
		store := NewNodeStore(tmp)
		writeTreeFile(filepath.Join(tmp, tt.path), tt.raw, 0o644)

		_, err := store.ReconstructTreeFromFS()
		Expect(err).To(HaveOccurred(), "expected reconstruct error")
		Expect(err).To(MatchError(markdown.
			ErrMetadataParse,
		),
			"expected metadata parse error, got: %v",

			err)

	},
	ginkgo.Entry("page file", malformedCanonicalMetadataCase{
		path: filepath.Join("root", "bad.md"),
		raw: `<!-- leafwiki
version: 1
page:
  id: bad
fields:
  aliases:
    - one
-->
# Bad`,
	}),
	ginkgo.Entry("section index", malformedCanonicalMetadataCase{
		path: filepath.Join("root", "docs", "index.md"),
		raw: `<!-- leafwiki
version: 1
page:
  id: docs
fields:
  leafwiki_hidden: true
-->
# Docs`,
	}),
	ginkgo.Entry("root index", malformedCanonicalMetadataCase{
		path: filepath.Join("root", "index.md"),
		raw: `<!-- leafwiki
version: 2
page:
  id: root
-->
# Root`,
	}),
)

var _ = ginkgo.Describe("node store filesystem reconstruction", func() {
	ginkgo.It("returns an error on case insensitive duplicate slugs", func() {
		tmp := tempTreeDir()
		store := NewNodeStore(tmp)

		writeTreeFile(filepath.Join(tmp, "root", "abc.md"), "# lower", 0o644)
		writeTreeFile(filepath.Join(tmp, "root", "ABC.md"), "# upper", 0o644)

		entries, err := os.ReadDir(filepath.Join(tmp, "root"))
		Expect(err).To(Succeed(), "read root directory: %v",

			err,
		)

		if len(entries) < 2 {
			ginkgo.Skip("filesystem does not preserve case-only duplicate filenames")
		}

		_, err = store.ReconstructTreeFromFS()
		Expect(err).To(HaveOccurred(), "expected duplicate slug error")
		Expect(err).To(MatchError(ErrDuplicateReconstructedSlug), "expected duplicate slug error, got: %v",

			err)

	})
})

var _ = ginkgo.Describe("node store filesystem reconstruction", func() {
	ginkgo.It("allows directory file slug pair", func() {
		tmp := tempTreeDir()
		store := NewNodeStore(tmp)

		createTreeDirectory(filepath.Join(tmp, "root", "notes"))
		writeTreeFile(filepath.Join(tmp, "root", "notes", "index.md"), `---
leafwiki_id: notes-section
leafwiki_title: Notes Section
---
# Notes section
`, 0o644)
		writeTreeFile(filepath.Join(tmp, "root", "notes.md"), `---
leafwiki_id: notes-page
leafwiki_title: Notes Page
---
# Notes page
`, 0o644)

		tree, err := store.ReconstructTreeFromFS()
		Expect(err).To(Succeed(), "ReconstructTreeFromFS: %v",

			err)

		var page, section *PageNode
		for _, child := range tree.Children {
			if child.Slug != "notes" {
				continue
			}
			if child.Kind == NodeKindPage {
				page = child
			}
			if child.Kind == NodeKindSection {
				section = child
			}
		}
		Expect(page).To(matchTreeNodePointer(NodeKindPage, Equal(newFixturePageID("notes-page"))),
			"notes page = %#v, want notes-page", page)
		Expect(section).To(matchTreeNodePointer(NodeKindSection, Equal(newFixturePageID("notes-section"))),
			"notes section = %#v, want notes-section", section)

	})
})

var _ = ginkgo.Describe("node store filesystem reconstruction", func() {
	ginkgo.It("imports normalizable workspace routes", func() {
		tmp := tempTreeDir()
		store := NewNodeStore(tmp)

		createTreeDirectory(filepath.Join(tmp, "root", "plans"))
		writeTreeFile(filepath.Join(tmp, "root", "plans", "index.md"), "# Plans", 0o644)
		writeTreeFile(filepath.Join(tmp, "root", "plans", "agent_hooks.PLAN.md"), "# Agent Hooks Plan", 0o644)
		createTreeDirectory(filepath.Join(tmp, "root", "User Guides"))
		writeTreeFile(filepath.Join(tmp, "root", "User Guides", "index.md"), "# User Guides", 0o644)

		tree, err := store.ReconstructTreeFromFS()
		Expect(err).To(Succeed(), "ReconstructTreeFromFS: %v",

			err)

		plans := findChildBySlug(tree, "plans")
		Expect(plans).To(SatisfyAll(
			HaveField("Kind", Equal(NodeKindSection)),
			HaveField("Slug", Equal(newFixtureSlug("plans"))),
		), "unexpected plans section: %#v", plans)

		plan := findChildBySlug(plans, "agent-hooks-plan")
		Expect(plan).To(SatisfyAll(
			HaveField("Kind", Equal(NodeKindPage)),
			HaveField("Title", Equal("Agent Hooks Plan")),
		), "unexpected normalized plan page: %#v", plan)

		raw, err := store.ReadPageRaw(plan)
		Expect(err).To(Succeed(), "ReadPageRaw normalized plan: %v",

			err,
		)
		Expect(raw).To(ContainSubstring("Agent Hooks Plan"),

			"ReadPageRaw normalized plan = %q, want original file content",

			raw,
		)

		guides := findChildBySlug(tree, "user-guides")
		Expect(guides.Kind).
			To(Equal(NodeKindSection), "guides.Kind = %q, want %q",

				guides.
					Kind, NodeKindSection)

		rawGuides, err := store.ReadPageRaw(guides)
		Expect(err).To(Succeed(), "ReadPageRaw normalized section: %v",

			err)
		Expect(rawGuides).To(ContainSubstring("User Guides"),

			"ReadPageRaw normalized section = %q, want original section content",

			rawGuides)

	})
})

var _ = ginkgo.Describe("node store filesystem reconstruction", func() {
	ginkgo.It("returns an error on normalized duplicate page routes", func() {
		tmp := tempTreeDir()
		store := NewNodeStore(tmp)

		createTreeDirectory(filepath.Join(tmp, "root", "plans"))
		writeTreeFile(filepath.Join(tmp, "root", "plans", "foo_bar.md"), "# Foo Bar", 0o644)
		writeTreeFile(filepath.Join(tmp, "root", "plans", "foo-bar.md"), "# Foo Bar Duplicate", 0o644)

		_, err := store.ReconstructTreeFromFS()
		Expect(err).To(HaveOccurred(), "expected duplicate normalized slug error")
		Expect(err).To(MatchError(ErrDuplicateReconstructedSlug), "expected duplicate page slug error, got: %v",

			err)

	})
})

var _ = ginkgo.Describe("node store filesystem reconstruction", func() {
	ginkgo.It("skips top level static assets", func() {
		tmp := tempTreeDir()
		store := NewNodeStore(tmp)

		createTreeDirectory(filepath.Join(tmp, "root", "assets", "install"))
		writeTreeFile(filepath.Join(tmp, "root", "assets", "install", "image.png"), "png", 0o644)
		writeTreeFile(filepath.Join(tmp, "root", "guide.md"), "# Guide", 0o644)

		tree, err := store.ReconstructTreeFromFS()
		Expect(err).To(Succeed(), "ReconstructTreeFromFS: %v",

			err)

		findChildBySlug(tree, "guide")
		for _, child := range tree.Children {
			Expect(strings.ToLower(child.Slug.String())).NotTo(SatisfyAny(Equal("assets"), Equal("assets-1")),
				"top-level static assets directory became wiki child: %#v", child)

		}

	})
})

var _ = ginkgo.Describe("node store filesystem reconstruction", func() {
	ginkgo.It("writes IDs back to files", func() {
		tmp := tempTreeDir()
		store := NewNodeStore(tmp)

		// Create files without leafwiki_id in frontmatter
		writeTreeFile(filepath.Join(tmp, "root", "no-id.md"), "# No ID", 0o644)
		createTreeDirectory(filepath.Join(tmp, "root", "section"))
		writeTreeFile(filepath.Join(tmp, "root", "section", "index.md"), "# Section No ID", 0o644)

		// Run reconstruction
		tree, err := store.ReconstructTreeFromFS()
		Expect(err).To(Succeed(), "ReconstructTreeFromFS: %v",

			err)

		// Get the page and section nodes
		page := findChildBySlug(tree, "no-id")
		section := findChildBySlug(tree, "section")
		Expect(page.ID).NotTo(BeEmpty(),

			"expected page to have generated ID, got empty")
		Expect(section.ID).NotTo(BeEmpty(),

			"expected section to have generated ID, got empty",
		)

		// Verify that IDs were generated

		// Now reload the files and check that IDs were written back
		pageMd, err := markdown.LoadMarkdownFile(filepath.Join(tmp, "root", "no-id.md"))
		Expect(err).To(Succeed(), "failed to reload page: %v",

			err)
		Expect(newFixturePageID(pageMd.
			GetFrontmatter().LeafWikiID,
		)).
			To(Equal(page.
				ID),
				"expected page frontmatter ID=%q, got %q",
				page.ID,

				pageMd.GetFrontmatter().LeafWikiID)

		sectionMd, err := markdown.LoadMarkdownFile(filepath.Join(tmp, "root", "section", "index.md"))
		Expect(err).To(Succeed(), "failed to reload section index: %v",

			err)
		Expect(newFixturePageID(sectionMd.
			GetFrontmatter().LeafWikiID,
		)).To(Equal(section.
			ID), "expected section frontmatter ID=%q, got %q",

			section.ID, sectionMd.GetFrontmatter().LeafWikiID)

		// Run reconstruction again and verify IDs are stable (deterministic)
		tree2, err := store.ReconstructTreeFromFS()
		Expect(err).To(Succeed(), "second ReconstructTreeFromFS: %v",

			err,
		)

		page2 := findChildBySlug(tree2, "no-id")
		section2 := findChildBySlug(tree2, "section")
		Expect(page2.ID).To(
			Equal(page.
				ID),
			"expected deterministic page ID on second run: first=%q, second=%q",

			page.ID, page2.ID)
		Expect(section2.ID).
			To(Equal(section.
				ID), "expected deterministic section ID on second run: first=%q, second=%q",

				section.ID, section2.
					ID)

	})
})

var _ = ginkgo.Describe("node store filesystem reconstruction", func() {
	ginkgo.It("normalizes importable slugs and skips empty normalized slugs", func() {
		tmp := tempTreeDir()
		store := NewNodeStore(tmp)

		// Create files and directories with names that were invalid route slugs but
		// can be normalized safely.
		writeTreeFile(filepath.Join(tmp, "root", "Valid Page.md"), "# Valid", 0o644)
		writeTreeFile(filepath.Join(tmp, "root", "UPPERCASE.md"), "# Upper", 0o644)
		createTreeDirectory(filepath.Join(tmp, "root", "Valid Section"))
		writeTreeFile(filepath.Join(tmp, "root", "Valid Section", "index.md"), "# Section", 0o644)
		writeTreeFile(filepath.Join(tmp, "root", "!!!.md"), "# Invalid", 0o644)

		// Create a valid file to ensure the test still works
		writeTreeFile(filepath.Join(tmp, "root", "valid.md"), "# Valid", 0o644)

		tree, err := store.ReconstructTreeFromFS()
		Expect(err).To(Succeed(), "ReconstructTreeFromFS: %v",

			err)

		// The valid file should be present with normalized slug
		findChildBySlug(tree, "valid")
		findChildBySlug(tree, "UPPERCASE")
		findChildBySlug(tree, "valid-page")
		findChildBySlug(tree, "valid-section")
		Expect(tree.Children).To(HaveLen(4),
			"expected only empty-normalized names to be skipped, got %v",

			slugs(tree.Children))

	})
})

var _ = ginkgo.Describe("node store filesystem reconstruction", func() {
	ginkgo.It("preserves mixed case slug names", func() {
		tmp := tempTreeDir()
		store := NewNodeStore(tmp)

		writeTreeFile(filepath.Join(tmp, "root", "ABCD-efg.md"), "# Mixed Case", 0o644)

		tree, err := store.ReconstructTreeFromFS()
		Expect(err).To(Succeed(), "ReconstructTreeFromFS: %v",

			err)

		findChildBySlug(tree, "ABCD-efg")

	})
})
var _ = ginkgo.Describe("node store filesystem reconstruction", func() {
	ginkgo.It("reads metadata from frontmatter", func() {
		tmp := tempTreeDir()
		store := NewNodeStore(tmp)

		writeTreeFile(filepath.Join(tmp, "root", "page.md"), `---
leafwiki_id: page-1
leafwiki_title: Page One
leafwiki_created_at: 2026-03-21T10:15:30Z
leafwiki_updated_at: 2026-03-21T11:16:31Z
leafwiki_creator_id: alice
leafwiki_last_author_id: bob
---
# Page One`, 0o644)

		tree, err := store.ReconstructTreeFromFS()
		Expect(err).To(Succeed(), "ReconstructTreeFromFS: %v",

			err)

		page := findChildBySlug(tree, "page")
		Expect(page).To(SatisfyAll(
			HaveField("ID", Equal(newFixturePageID("page-1"))),
			HaveField("Metadata", SatisfyAll(
				matchPageMetadataTimestamps("2026-03-21T10:15:30Z", "2026-03-21T11:16:31Z"),
				matchPageMetadataAuthors(newFixtureUserID("alice"), newFixtureUserID("bob")),
			)),
		), "expected page metadata from frontmatter, got %#v", page)

	})
})

var _ = ginkgo.Describe("node store filesystem reconstruction", func() {
	ginkgo.It("canonicalizes complete legacy metadata", func() {
		tmp := tempTreeDir()
		store := NewNodeStore(tmp)

		rootIndex := filepath.Join(tmp, "root", "index.md")
		sectionIndex := filepath.Join(tmp, "root", "docs", "index.md")
		pagePath := filepath.Join(tmp, "root", "docs", "page.md")

		writeTreeFile(rootIndex, `---
leafwiki_id: root
leafwiki_title: Root
leafwiki_created_at: 2026-03-21T09:00:00Z
leafwiki_updated_at: 2026-03-21T09:30:00Z
leafwiki_creator_id: root-author
leafwiki_last_author_id: root-editor
---
# Root`, 0o644)
		writeTreeFile(sectionIndex, `---
leafwiki_id: docs-section
leafwiki_title: Docs
leafwiki_created_at: 2026-03-21T10:00:00Z
leafwiki_updated_at: 2026-03-21T10:30:00Z
leafwiki_creator_id: docs-author
leafwiki_last_author_id: docs-editor
---
# Docs`, 0o644)
		writeTreeFile(pagePath, `---
leafwiki_id: docs-page
leafwiki_title: Page
leafwiki_created_at: 2026-03-21T11:00:00Z
leafwiki_updated_at: 2026-03-21T11:30:00Z
leafwiki_creator_id: page-author
leafwiki_last_author_id: page-editor
---
# Page`, 0o644)

		tree, err := store.ReconstructTreeFromFS()
		Expect(err).To(Succeed(), "ReconstructTreeFromFS: %v",

			err)
		Expect(tree.ID).To(Equal(newFixturePageID("root")), "root ID = %q",
			tree.
				ID)

		section := findChildBySlug(tree, "docs")
		Expect(section.ID).To(Equal(newFixturePageID("docs-section")), "section ID = %q",

			section.
				ID)

		page := findChildBySlug(section, "page")
		Expect(page.ID).To(Equal(newFixturePageID("docs-page")), "page ID = %q",
			page.ID,
		)

		for _, path := range []string{rootIndex, sectionIndex, pagePath} {
			raw, err := os.ReadFile(path)
			Expect(err).To(Succeed(), "ReadFile(%s): %v",

				path, err,
			)

			content := string(raw)
			Expect(content).
				To(HavePrefix("<!-- leafwiki\n"),

					"%s was not canonicalized: %q",

					path, content)
			Expect(content).NotTo(HavePrefix("---\n"),

				"%s still starts with YAML frontmatter: %q",

				path, content)
			{

				_, _, err := markdown.ParsePageDocument(content)
				Expect(err).To(Succeed(), "ParsePageDocument(%s): %v",

					path, err,
				)
			}

		}

	})
})

var _ = ginkgo.Describe("node store filesystem reconstruction", func() {
	ginkgo.It("missing metadata falls back to mtime and system", func() {
		tmp := tempTreeDir()
		store := NewNodeStore(tmp)

		pagePath := filepath.Join(tmp, "root", "page.md")
		writeTreeFile(pagePath, `# Page One`, 0o644)

		wantTime := time.Date(2026, time.March, 21, 12, 34, 56, 0, time.UTC)
		{
			err := os.Chtimes(pagePath, wantTime, wantTime)
			Expect(err).To(Succeed(), "Chtimes: %v",

				err)
		}

		tree, err := store.ReconstructTreeFromFS()
		Expect(err).To(Succeed(), "ReconstructTreeFromFS: %v",

			err)

		page := findChildBySlug(tree, "page")
		Expect(strings.TrimSpace(page.
			ID.String())).NotTo(BeEmpty(),

			"expected generated ID",
		)
		{

			got := page.Metadata.CreatedAt.UTC().Format(time.RFC3339)
			Expect(got).To(Equal(wantTime.
				Format(time.RFC3339)),
				"expected created_at fallback from mtime, got %q",

				got)
		}
		{

			got := page.Metadata.UpdatedAt.UTC().Format(time.RFC3339)
			Expect(got).To(Equal(wantTime.
				Format(time.RFC3339)),
				"expected updated_at fallback from mtime, got %q",

				got)
		}
		Expect(page.Metadata).To(matchPageMetadataAuthors(reconstructSystemUserID, reconstructSystemUserID),
			"expected system user fallback, got %#v",

			page.Metadata)

		mdFile, err := markdown.LoadMarkdownFile(pagePath)
		Expect(err).To(Succeed(), "LoadMarkdownFile: %v",

			err)

		frontmatter := mdFile.GetFrontmatter()
		Expect(frontmatter).To(SatisfyAll(
			HaveField("LeafWikiID", WithTransform(func(raw string) PageID {
				return newFixturePageID(raw)
			}, Equal(page.ID))),
			HaveField("LeafWikiCreatedAt", Equal(wantTime.Format(time.RFC3339))),
			HaveField("LeafWikiUpdatedAt", Equal(wantTime.Format(time.RFC3339))),
			HaveField("LeafWikiCreatorID", Equal(reconstructSystemUserID)),
			HaveField("LeafWikiLastAuthorID", Equal(reconstructSystemUserID)),
		), "expected generated metadata fallback to be written back, got %#v", frontmatter)

	})
})

var _ = ginkgo.Describe("node store filesystem reconstruction", func() {
	ginkgo.It("invalid metadata timestamp falls back to mtime", func() {
		tmp := tempTreeDir()
		store := NewNodeStore(tmp)

		pagePath := filepath.Join(tmp, "root", "page.md")
		writeTreeFile(pagePath, `---
leafwiki_id: page-1
leafwiki_title: Page One
leafwiki_created_at: not-a-timestamp
leafwiki_updated_at: 2026-03-21T11:16:31Z
leafwiki_creator_id: alice
leafwiki_last_author_id: bob
---
# Page One`, 0o644)

		wantTime := time.Date(2026, time.March, 21, 12, 34, 56, 0, time.UTC)
		{
			err := os.Chtimes(pagePath, wantTime, wantTime)
			Expect(err).To(Succeed(), "Chtimes: %v",

				err)
		}

		tree, err := store.ReconstructTreeFromFS()
		Expect(err).To(Succeed(), "ReconstructTreeFromFS: %v",

			err)

		page := findChildBySlug(tree, "page")
		{
			got := page.Metadata.CreatedAt.UTC().Format(time.RFC3339)
			Expect(got).To(Equal(wantTime.
				Format(time.RFC3339)),
				"expected invalid created_at to fall back to mtime, got %q",

				got)
		}
		{

			got := page.Metadata.UpdatedAt.UTC().Format(time.RFC3339)
			Expect(got).To(Equal("2026-03-21T11:16:31Z"), "expected valid updated_at to be preserved, got %q",

				got)
		}
		Expect(page.Metadata).To(SatisfyAll(
			HaveField("CreatorID", Equal(newFixtureUserID("alice"))),
			HaveField("LastAuthorID", Equal(newFixtureUserID("bob"))),
		))

	})
})
