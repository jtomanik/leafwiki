package tree

import (
	"encoding/json"
	. "github.com/onsi/gomega"
	"os"
	"path/filepath"
	"sort"
	"strings"

	ginkgo "github.com/onsi/ginkgo/v2"
	"github.com/perber/wiki/internal/core/markdown"
)

// - root index.md has precedence over root README.md
var _ = ginkgo.Describe("node store filesystem reconstruction", ginkgo.Label("unit"), func() {
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

var _ = ginkgo.Describe("node store filesystem reconstruction", ginkgo.Label("unit"), func() {
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

		frontmatter, body, err := parseRequiredFrontmatter(string(raw))
		Expect(err).To(Succeed(), "ParseFrontmatter: %v", err)
		Expect(frontmatter).To(matchManagedFrontmatter(sec.ID, sec.Title),
			"unexpected frontmatter in materialized index: %#v", frontmatter)
		Expect(strings.TrimSpace(body)).To(BeEmpty(),

			"expected empty body in materialized index, got %q",

			body)

	})
})

var _ = ginkgo.Describe("node store filesystem reconstruction", ginkgo.Label("unit"), func() {
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

var _ = ginkgo.Describe("node store filesystem reconstruction", ginkgo.Label("unit"), func() {
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

var _ = ginkgo.Describe("node store filesystem reconstruction", ginkgo.Label("unit"), func() {
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

var _ = ginkgo.Describe("node store filesystem reconstruction", ginkgo.Label("unit"), func() {
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

var _ = ginkgo.Describe("node store filesystem reconstruction", ginkgo.Label("unit"), func() {
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
		Expect(err).To(MatchError(ErrDuplicateLeafwikiID), "expected duplicate ID error, got: %v",

			err)

	})
})

var _ = ginkgo.Describe("node store filesystem reconstruction", ginkgo.Label("unit"), func() {
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
		Expect(err).To(MatchError(ErrDuplicateLeafwikiID), "expected duplicate ID error, got: %v",

			err)

	})
})

type malformedCanonicalMetadataCase struct {
	path string
	raw  string
}

var _ = ginkgo.DescribeTable("node store filesystem reconstruction returns an error on malformed canonical metadata", ginkgo.Label("unit"),
	func(tt malformedCanonicalMetadataCase) {
		tmp := tempTreeDir()
		store := NewNodeStore(tmp)
		writeTreeFile(filepath.Join(tmp, tt.path), tt.raw, 0o644)

		_, err := store.ReconstructTreeFromFS()
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

var _ = ginkgo.Describe("node store filesystem reconstruction", ginkgo.Label("unit"), func() {
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
		Expect(err).To(MatchError(ErrDuplicateReconstructedSlug), "expected duplicate slug error, got: %v",

			err)

	})
})

var _ = ginkgo.Describe("node store filesystem reconstruction", ginkgo.Label("unit"), func() {
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
