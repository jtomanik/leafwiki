package tree

import (
	. "github.com/onsi/gomega"
	"os"
	"path/filepath"
	"strings"
	"time"

	ginkgo "github.com/onsi/ginkgo/v2"
)

var _ = ginkgo.Describe("node store persistence", ginkgo.Label("unit"), func() {
	ginkgo.It("convert node page to section moves to index", func() {
		tmp := tempTreeDir()
		store := NewNodeStore(tmp)

		root := &PageNode{ID: newFixturePageID("root"), Slug: newFixtureSlug("root"), Title: "root", Kind: NodeKindSection}
		entry := &PageNode{ID: newFixturePageID("p1"), Slug: newFixtureSlug("p"), Title: "P", Kind: NodeKindPage, Parent: root}

		file := filepath.Join(tmp, "root", "p.md")
		writeTreeFile(file, "# hi", 0o644)
		{

			err := store.ConvertNode(entry, NodeKindSection)
			Expect(err).To(Succeed(), "ConvertNode(page->section): %v",

				err)
		}

		index := filepath.Join(tmp, "root", "p", "index.md")
		{
			_, err := os.Stat(index)
			Expect(err).To(Succeed(), "expected index at %s",

				index,
			)
		}
		{

			_, err := os.Stat(file)
			Expect(err).To(MatchError(os.
				ErrNotExist,
			),

				"expected old file removed",
			)
		}

	})
})

var _ = ginkgo.Describe("node store persistence", ginkgo.Label("unit"), func() {
	ginkgo.It("convert node page to section preserves existing metadata and body", func() {
		tmp := tempTreeDir()
		store := NewNodeStore(tmp)

		root := &PageNode{ID: newFixturePageID("root"), Slug: newFixtureSlug("root"), Title: "root", Kind: NodeKindSection}
		entry := &PageNode{
			ID:     newFixturePageID("p1"),
			Slug:   newFixtureSlug("p"),
			Title:  "Section Title",
			Kind:   NodeKindPage,
			Parent: root,
			Metadata: PageMetadata{
				CreatedAt:    time.Date(2026, time.March, 22, 10, 15, 30, 0, time.UTC),
				UpdatedAt:    time.Date(2026, time.March, 22, 11, 16, 31, 0, time.UTC),
				CreatorID:    newFixtureUserID("alice"),
				LastAuthorID: newFixtureUserID("bob"),
			},
		}

		file := filepath.Join(tmp, "root", "p.md")
		writeTreeFile(file, `---
custom_key: keep-me
leafwiki_id: legacy-id
leafwiki_title: Legacy Title
---
# hi
`, 0o644)
		{

			err := store.ConvertNode(entry, NodeKindSection)
			Expect(err).To(Succeed(), "ConvertNode(page->section): %v",

				err)
		}

		index := filepath.Join(tmp, "root", "p", "index.md")
		raw := string(readTreeFile(index))
		Expect(raw).
			To(ContainSubstring("custom_key: keep-me"),

				"expected custom frontmatter to be preserved, got %q",

				raw)

		frontmatter, body, err := parseRequiredFrontmatter(raw)
		Expect(err).To(Succeed(), "ParseFrontmatter: %v", err)
		Expect(frontmatter).To(matchManagedFrontmatter(newFixturePageID("p1"), "Section Title"),
			"expected managed frontmatter from tree metadata, got %#v", frontmatter)
		Expect(frontmatter).To(matchFrontmatterTimestamps("2026-03-22T10:15:30Z", "2026-03-22T11:16:31Z"),
			"expected timestamps from tree metadata, got %#v", frontmatter)
		Expect(frontmatter).To(matchFrontmatterAuthors(newFixtureUserID("alice"), newFixtureUserID("bob")),
			"expected author metadata from tree metadata, got %#v", frontmatter)
		Expect(strings.TrimSpace(body)).To(Equal("# hi"), "expected body to be preserved, got %q",

			body)

	})
})

var _ = ginkgo.Describe("node store persistence", ginkgo.Label("unit"), func() {
	ginkgo.It("convert node section to page rejects non empty folder", func() {
		tmp := tempTreeDir()
		store := NewNodeStore(tmp)

		root := &PageNode{ID: newFixturePageID("root"), Slug: newFixtureSlug("root"), Title: "root", Kind: NodeKindSection}
		entry := &PageNode{ID: newFixturePageID("s1"), Slug: newFixtureSlug("docs"), Title: "Docs", Kind: NodeKindSection, Parent: root}

		directory := filepath.Join(tmp, "root", "docs")
		createTreeDirectory(directory)
		writeTreeFile(filepath.Join(directory, "index.md"), "# idx", 0o644)
		writeTreeFile(filepath.Join(directory, "other.txt"), "nope", 0o644)

		err := store.ConvertNode(entry, NodeKindPage)

		var cna *ConvertNotAllowedError
		Expect(err).To(matchErrorAs(&cna), "expected ConvertNotAllowedError, got %T: %v",

			err,

			err)

	})
})

var _ = ginkgo.Describe("node store persistence", ginkgo.Label("unit"), func() {
	ginkgo.It("convert node section to page with index moves and removes folder", func() {
		tmp := tempTreeDir()
		store := NewNodeStore(tmp)

		root := &PageNode{ID: newFixturePageID("root"), Slug: newFixtureSlug("root"), Title: "root", Kind: NodeKindSection}
		entry := &PageNode{ID: newFixturePageID("s1"), Slug: newFixtureSlug("docs"), Title: "Docs", Kind: NodeKindSection, Parent: root}

		directory := filepath.Join(tmp, "root", "docs")
		createTreeDirectory(directory)
		writeTreeFile(filepath.Join(directory, "index.md"), "# idx", 0o644)
		{

			err := store.ConvertNode(entry, NodeKindPage)
			Expect(err).To(Succeed(), "ConvertNode(section->page): %v",

				err)
		}

		pageFile := filepath.Join(tmp, "root", "docs.md")
		{
			_, err := os.Stat(pageFile)
			Expect(err).To(Succeed(), "expected page file: %v",

				err,
			)
		}
		{

			_, err := os.Stat(directory)
			Expect(err).To(MatchError(os.
				ErrNotExist,
			),

				"expected folder removed",
			)
		}

	})
})

var _ = ginkgo.Describe("node store persistence", ginkgo.Label("unit"), func() {
	ginkgo.It("convert node section to page no index creates empty page with frontmatter", func() {
		tmp := tempTreeDir()
		store := NewNodeStore(tmp)

		root := &PageNode{ID: newFixturePageID("root"), Slug: newFixtureSlug("root"), Title: "root", Kind: NodeKindSection}
		entry := &PageNode{ID: newFixturePageID("s1"), Slug: newFixtureSlug("docs"), Title: "Docs", Kind: NodeKindSection, Parent: root}

		directory := filepath.Join(tmp, "root", "docs")
		createTreeDirectory(directory)
		{
			// empty folder, no index.md

			err := store.ConvertNode(entry, NodeKindPage)
			Expect(err).To(Succeed(), "ConvertNode(section->page no index): %v",

				err)
		}

		pageFile := filepath.Join(tmp, "root", "docs.md")
		raw := string(readTreeFile(pageFile))
		Expect(raw).To(haveCanonicalNodeStoreRawStorage())
		frontmatter, _, err := parseRequiredFrontmatter(raw)
		Expect(err).To(Succeed(), "ParseFrontmatter: %v", err)
		Expect(frontmatter).To(matchManagedFrontmatter(newFixturePageID("s1"), "Docs"),
			"unexpected frontmatter: %#v", frontmatter)
		{

			_, err := os.Stat(directory)
			Expect(err).To(MatchError(os.
				ErrNotExist,
			),

				"expected folder removed",
			)
		}

	})
})

var _ = ginkgo.Describe("node store persistence", ginkgo.Label("unit"), func() {
	ginkgo.It("convert node section to page with order metadata preserves index content", func() {
		tmp := tempTreeDir()
		store := NewNodeStore(tmp)

		root := &PageNode{ID: newFixturePageID("root"), Slug: newFixtureSlug("root"), Title: "root", Kind: NodeKindSection}
		entry := &PageNode{ID: newFixturePageID("s1"), Slug: newFixtureSlug("docs"), Title: "Docs", Kind: NodeKindSection, Parent: root}

		directory := filepath.Join(tmp, "root", "docs")
		createTreeDirectory(directory)
		indexPath := filepath.Join(directory, "index.md")
		writeTreeFile(indexPath, `---
leafwiki_id: existing
leafwiki_title: Existing
custom: keep
---
# idx
`, 0o644)
		writeTreeFile(filepath.Join(directory, orderFilename), `{"ordered_ids":[]}`, 0o644)
		{

			err := store.ConvertNode(entry, NodeKindPage)
			Expect(err).To(Succeed(), "ConvertNode(section->page with order metadata): %v",

				err)
		}

		pageFile := filepath.Join(tmp, "root", "docs.md")
		raw := string(readTreeFile(pageFile))
		Expect(raw).To(SatisfyAll(ContainSubstring("custom: keep"), ContainSubstring("# idx")),
			"expected converted page to keep index content, got: %s", raw)
		{

			_, err := os.Stat(directory)
			Expect(err).To(MatchError(os.
				ErrNotExist,
			),

				"expected folder removed",
			)
		}

	})
})

var _ = ginkgo.Describe("node store persistence", ginkgo.Label("unit"), func() {
	ginkgo.It("move node section moves folder strict", func() {
		tmp := tempTreeDir()
		store := NewNodeStore(tmp)

		root := &PageNode{ID: newFixturePageID("root"), Slug: newFixtureSlug("root"), Title: "root", Kind: NodeKindSection}
		secA := &PageNode{ID: newFixturePageID("a"), Slug: newFixtureSlug("a"), Title: "A", Kind: NodeKindSection, Parent: root}
		secB := &PageNode{ID: newFixturePageID("b"), Slug: newFixtureSlug("b"), Title: "B", Kind: NodeKindSection, Parent: root}
		entry := &PageNode{ID: newFixturePageID("s1"), Slug: newFixtureSlug("docs"), Title: "Docs", Kind: NodeKindSection, Parent: secA}

		srcDir := filepath.Join(tmp, "root", "a", "docs")
		createTreeDirectory(srcDir)
		writeTreeFile(filepath.Join(srcDir, "index.md"), "# hi", 0o644)
		{

			err := store.MoveNode(entry, secB)
			Expect(err).To(Succeed(), "MoveNode(section): %v",

				err)
		}

		dstDir := filepath.Join(tmp, "root", "b", "docs")
		Expect(dstDir).To(BeADirectory(), "expected moved section directory")
		{

			_, err := os.Stat(srcDir)
			Expect(err).To(MatchError(os.
				ErrNotExist,
			),

				"expected old section directory removed",
			)
		}

	})
})

var _ = ginkgo.Describe("node store persistence", ginkgo.Label("unit"), func() {
	ginkgo.It("move node page drift when source is folder", func() {
		tmp := tempTreeDir()
		store := NewNodeStore(tmp)

		root := &PageNode{ID: newFixturePageID("root"), Slug: newFixtureSlug("root"), Title: "root", Kind: NodeKindSection}
		sec := &PageNode{ID: newFixturePageID("s"), Slug: newFixtureSlug("s"), Title: "S", Kind: NodeKindSection, Parent: root}
		page := &PageNode{ID: newFixturePageID("p1"), Slug: newFixtureSlug("p"), Title: "P", Kind: NodeKindPage, Parent: sec}

		createTreeDirectory(filepath.Join(tmp, "root", "s", "p.md"))

		err := store.MoveNode(page, root)

		var de *DriftError
		Expect(err).To(matchErrorAs(&de), "expected DriftError, got %T: %v",

			err, err,
		)

	})
})

var _ = ginkgo.Describe("node store persistence", ginkgo.Label("unit"), func() {
	ginkgo.It("move node section drift when source is file", func() {
		tmp := tempTreeDir()
		store := NewNodeStore(tmp)

		root := &PageNode{ID: newFixturePageID("root"), Slug: newFixtureSlug("root"), Title: "root", Kind: NodeKindSection}
		sec := &PageNode{ID: newFixturePageID("s"), Slug: newFixtureSlug("s"), Title: "S", Kind: NodeKindSection, Parent: root}
		entry := &PageNode{ID: newFixturePageID("p1"), Slug: newFixtureSlug("docs"), Title: "Docs", Kind: NodeKindSection, Parent: sec}

		writeTreeFile(filepath.Join(tmp, "root", "s", "docs"), "not a directory", 0o644)

		err := store.MoveNode(entry, root)

		var de *DriftError
		Expect(err).To(matchErrorAs(&de), "expected DriftError, got %T: %v",

			err, err,
		)

	})
})

var _ = ginkgo.Describe("node store persistence", ginkgo.Label("unit"), func() {
	ginkgo.It("move node rejects destination collision", func() {
		tmp := tempTreeDir()
		store := NewNodeStore(tmp)

		root := &PageNode{ID: newFixturePageID("root"), Slug: newFixtureSlug("root"), Title: "root", Kind: NodeKindSection}
		secA := &PageNode{ID: newFixturePageID("a"), Slug: newFixtureSlug("a"), Title: "A", Kind: NodeKindSection, Parent: root}
		secB := &PageNode{ID: newFixturePageID("b"), Slug: newFixtureSlug("b"), Title: "B", Kind: NodeKindSection, Parent: root}
		page := &PageNode{ID: newFixturePageID("p1"), Slug: newFixtureSlug("p"), Title: "P", Kind: NodeKindPage, Parent: secA}

		src := filepath.Join(tmp, "root", "a", "p.md")
		dst := filepath.Join(tmp, "root", "b", "p.md")
		writeTreeFile(src, "# hi", 0o644)
		writeTreeFile(dst, "# existing", 0o644)

		err := store.MoveNode(page, secB)

		var existsErr *PageAlreadyExistsError
		Expect(err).To(matchErrorAs(&existsErr), "expected PageAlreadyExistsError, got %T: %v",

			err, err)

	})
})

var _ = ginkgo.Describe("node store persistence", ginkgo.Label("unit"), func() {
	ginkgo.It("convert node page to section creates index when page missing", func() {
		tmp := tempTreeDir()
		store := NewNodeStore(tmp)

		root := &PageNode{ID: newFixturePageID("root"), Slug: newFixtureSlug("root"), Title: "root", Kind: NodeKindSection}
		entry := &PageNode{ID: newFixturePageID("p1"), Slug: newFixtureSlug("p"), Title: "P", Kind: NodeKindPage, Parent: root}
		{

			err := store.ConvertNode(entry, NodeKindSection)
			Expect(err).To(Succeed(), "ConvertNode(page->section missing page): %v",

				err)
		}

		index := filepath.Join(tmp, "root", "p", "index.md")
		{
			_, err := os.Stat(index)
			Expect(err).To(Succeed(), "expected materialized section index: %v",

				err)
		}

	})
})

var _ = ginkgo.Describe("node store persistence", ginkgo.Label("unit"), func() {
	ginkgo.It("convert node rejects unknown target", func() {
		tmp := tempTreeDir()
		store := NewNodeStore(tmp)

		root := &PageNode{ID: newFixturePageID("root"), Slug: newFixtureSlug("root"), Title: "root", Kind: NodeKindSection}
		entry := &PageNode{ID: newFixturePageID("p1"), Slug: newFixtureSlug("p"), Title: "P", Kind: NodeKindPage, Parent: root}

		err := store.ConvertNode(entry, newFixtureNodeKind("weird"))

		var opErr *InvalidOpError
		Expect(err).To(matchErrorAs(&opErr),
			"expected InvalidOpError, got %T: %v",

			err, err,
		)

	})
})

var _ = ginkgo.Describe("node store persistence", ginkgo.Label("unit"), func() {
	ginkgo.It("convert node section to page drift when path is file", func() {
		tmp := tempTreeDir()
		store := NewNodeStore(tmp)

		root := &PageNode{ID: newFixturePageID("root"), Slug: newFixtureSlug("root"), Title: "root", Kind: NodeKindSection}
		entry := &PageNode{ID: newFixturePageID("s1"), Slug: newFixtureSlug("docs"), Title: "Docs", Kind: NodeKindSection, Parent: root}

		writeTreeFile(filepath.Join(tmp, "root", "docs"), "not a directory", 0o644)

		err := store.ConvertNode(entry, NodeKindPage)

		var de *DriftError
		Expect(err).To(matchErrorAs(&de), "expected DriftError, got %T: %v",

			err, err,
		)

	})
})

func readTreeFile(path string) []byte {
	ginkgo.GinkgoHelper()
	b, err := os.ReadFile(path)
	Expect(err).NotTo(HaveOccurred())
	return b
}
