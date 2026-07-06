package tree

import (
	. "github.com/onsi/gomega"
	"os"
	"path/filepath"
	"strings"
	"time"

	ginkgo "github.com/onsi/ginkgo/v2"
	"github.com/perber/wiki/internal/core/markdown"
)

var _ = ginkgo.Describe("node store persistence", ginkgo.Label("unit"), func() {
	ginkgo.It("rename node returns nil when slug unchanged", func() {
		tmp := tempTreeDir()
		store := NewNodeStore(tmp)

		root := &PageNode{ID: newFixturePageID("root"), Slug: newFixtureSlug("root"), Title: "root", Kind: NodeKindSection}
		page := &PageNode{ID: newFixturePageID("p1"), Slug: newFixtureSlug("same"), Title: "P", Kind: NodeKindPage, Parent: root}
		file := filepath.Join(tmp, "root", "same.md")
		writeTreeFile(file, "# x", 0o644)
		{

			err := store.RenameNode(page, newFixtureSlug("same"))
			Expect(err).To(Succeed(), "expected unchanged slug rename to be a no-op, got %v",

				err,
			)
		}
		{

			_, err := os.Stat(file)
			Expect(err).To(Succeed(), "expected original file to remain: %v",

				err)
		}

	})
})

var _ = ginkgo.Describe("node store persistence", ginkgo.Label("unit"), func() {
	ginkgo.It("rename node rejects destination collision", func() {
		tmp := tempTreeDir()
		store := NewNodeStore(tmp)

		root := &PageNode{ID: newFixturePageID("root"), Slug: newFixtureSlug("root"), Title: "root", Kind: NodeKindSection}
		page := &PageNode{ID: newFixturePageID("p1"), Slug: newFixtureSlug("old"), Title: "P", Kind: NodeKindPage, Parent: root}
		writeTreeFile(filepath.Join(tmp, "root", "old.md"), "# x", 0o644)
		writeTreeFile(filepath.Join(tmp, "root", "new.md"), "# y", 0o644)

		err := store.RenameNode(page, newFixtureSlug("new"))

		var existsErr *PageAlreadyExistsError
		Expect(err).To(matchErrorAs(&existsErr), "expected PageAlreadyExistsError, got %T: %v",

			err, err)

	})
})

var _ = ginkgo.Describe("node store persistence", ginkgo.Label("unit"), func() {
	ginkgo.It("rename node page drift when source is folder", func() {
		tmp := tempTreeDir()
		store := NewNodeStore(tmp)

		root := &PageNode{ID: newFixturePageID("root"), Slug: newFixtureSlug("root"), Title: "root", Kind: NodeKindSection}
		page := &PageNode{ID: newFixturePageID("p1"), Slug: newFixtureSlug("old"), Title: "P", Kind: NodeKindPage, Parent: root}
		createTreeDirectory(filepath.Join(tmp, "root", "old.md"))

		err := store.RenameNode(page, newFixtureSlug("new"))

		var de *DriftError
		Expect(err).To(matchErrorAs(&de), "expected DriftError, got %T: %v",

			err, err,
		)

	})
})

var _ = ginkgo.Describe("node store persistence", ginkgo.Label("unit"), func() {
	ginkgo.It("rename node section drift when source is file", func() {
		tmp := tempTreeDir()
		store := NewNodeStore(tmp)

		root := &PageNode{ID: newFixturePageID("root"), Slug: newFixtureSlug("root"), Title: "root", Kind: NodeKindSection}
		sec := &PageNode{ID: newFixturePageID("s1"), Slug: newFixtureSlug("docs"), Title: "Docs", Kind: NodeKindSection, Parent: root}
		writeTreeFile(filepath.Join(tmp, "root", "docs"), "not a directory", 0o644)

		err := store.RenameNode(sec, newFixtureSlug("docs2"))

		var de *DriftError
		Expect(err).To(matchErrorAs(&de), "expected DriftError, got %T: %v",

			err, err,
		)

	})
})

var _ = ginkgo.Describe("node store persistence", ginkgo.Label("unit"), func() {
	ginkgo.It("rename node rejects unknown kind", func() {
		tmp := tempTreeDir()
		store := NewNodeStore(tmp)

		root := &PageNode{ID: newFixturePageID("root"), Slug: newFixtureSlug("root"), Title: "root", Kind: NodeKindSection}
		entry := &PageNode{ID: newFixturePageID("x1"), Slug: newFixtureSlug("weird"), Title: "Weird", Kind: newFixtureNodeKind("mystery"), Parent: root}

		err := store.RenameNode(entry, newFixtureSlug("other"))

		var opErr *InvalidOpError
		Expect(err).To(matchErrorAs(&opErr),
			"expected InvalidOpError, got %T: %v",

			err, err,
		)

	})
})

var _ = ginkgo.Describe("node store persistence", ginkgo.Label("unit"), func() {
	ginkgo.It("read page raw section no index returns empty nil", func() {
		tmp := tempTreeDir()
		store := NewNodeStore(tmp)

		root := &PageNode{ID: newFixturePageID("root"), Slug: newFixtureSlug("root"), Title: "root", Kind: NodeKindSection}
		sec := &PageNode{ID: newFixturePageID("s1"), Slug: newFixtureSlug("docs"), Title: "Docs", Kind: NodeKindSection, Parent: root}

		createTreeDirectory(filepath.Join(tmp, "root", "docs"))

		raw, err := store.ReadPageRaw(sec)
		Expect(err).To(Succeed(), "ReadPageRaw: %v",

			err)
		Expect(raw).To(BeEmpty(),

			"expected empty raw for section without index, got %q",

			raw,
		)
		{

			_, err := os.Stat(filepath.Join(tmp, "root", "docs", "index.md"))
			Expect(err).To(MatchError(os.ErrNotExist), "expected no index.md side effect on read")
		}

	})
})

var _ = ginkgo.Describe("node store persistence", ginkgo.Label("unit"), func() {
	ginkgo.It("read page raw page missing is drift", func() {
		tmp := tempTreeDir()
		store := NewNodeStore(tmp)

		root := &PageNode{ID: newFixturePageID("root"), Slug: newFixtureSlug("root"), Title: "root", Kind: NodeKindSection}
		page := &PageNode{ID: newFixturePageID("p1"), Slug: newFixtureSlug("p"), Title: "P", Kind: NodeKindPage, Parent: root}

		_, err := store.ReadPageRaw(page)
		var de *DriftError
		Expect(err).To(matchErrorAs(&de), "expected DriftError, got %T: %v", err, err)

	})
})

var _ = ginkgo.Describe("node store persistence", ginkgo.Label("unit"), func() {
	ginkgo.It("read page content strips frontmatter and preserves body", func() {
		tmp := tempTreeDir()
		store := NewNodeStore(tmp)

		root := &PageNode{ID: newFixturePageID("root"), Slug: newFixtureSlug("root"), Title: "root", Kind: NodeKindSection}
		page := &PageNode{ID: newFixturePageID("p1"), Slug: newFixtureSlug("p"), Title: "P", Kind: NodeKindPage, Parent: root}

		path := filepath.Join(tmp, "root", "p.md")
		writeTreeFile(path, `---
custom_key: keep-me
leafwiki_id: p1
leafwiki_title: Existing Title
---
# Body
Hello
`, 0o644)

		content, err := store.ReadPageContent(page)
		Expect(err).To(Succeed(), "ReadPageContent: %v",

			err)
		Expect(content).To(Equal(`# Body
Hello
`), "expected body without frontmatter, got %q",

			content)

	})
})

var _ = ginkgo.Describe("node store persistence", ginkgo.Label("unit"), func() {
	ginkgo.It("read page content invalid frontmatter returns raw", func() {
		tmp := tempTreeDir()
		store := NewNodeStore(tmp)

		root := &PageNode{ID: newFixturePageID("root"), Slug: newFixtureSlug("root"), Title: "root", Kind: NodeKindSection}
		page := &PageNode{ID: newFixturePageID("p1"), Slug: newFixtureSlug("p"), Title: "P", Kind: NodeKindPage, Parent: root}

		path := filepath.Join(tmp, "root", "p.md")
		raw := `---
leafwiki_id: [broken
---
# Body
Hello
`
		writeTreeFile(path, raw, 0o644)

		content, err := store.ReadPageContent(page)
		Expect(err).To(MatchError(markdown.ErrFrontmatterParse), "expected frontmatter parse error, got %v", err)
		Expect(content).To(Equal(raw),
			"expected raw content fallback on parse error, got %q",

			content)

	})
})

var _ = ginkgo.Describe("node store persistence", ginkgo.Label("unit"), func() {
	ginkgo.It("sync frontmatter if exists page updates or adds frontmatter", func() {
		tmp := tempTreeDir()
		store := NewNodeStore(tmp)

		root := &PageNode{ID: newFixturePageID("root"), Slug: newFixtureSlug("root"), Title: "root", Kind: NodeKindSection}
		page := &PageNode{
			ID:     newFixturePageID("p1"),
			Slug:   newFixtureSlug("p"),
			Title:  "Title A",
			Kind:   NodeKindPage,
			Parent: root,
			Metadata: PageMetadata{
				CreatedAt:    time.Date(2026, time.March, 21, 10, 15, 30, 0, time.UTC),
				UpdatedAt:    time.Date(2026, time.March, 21, 11, 16, 31, 0, time.UTC),
				CreatorID:    newFixtureUserID("alice"),
				LastAuthorID: newFixtureUserID("bob"),
			},
		}

		path := filepath.Join(tmp, "root", "p.md")

		// file without FM
		writeTreeFile(path, "# Body\nHello", 0o644)
		{

			err := store.SyncFrontmatterIfExists(page)
			Expect(err).To(Succeed(), "SyncFrontmatterIfExists: %v",

				err)
		}

		raw := string(readTreeFile(path))
		Expect(raw).To(haveCanonicalNodeStoreRawStorage())
		frontmatter, body, err := parseRequiredFrontmatter(raw)
		Expect(err).To(Succeed(), "ParseFrontmatter: %v", err)
		Expect(frontmatter).To(matchManagedFrontmatter(newFixturePageID("p1"), "Title A"),
			"unexpected frontmatter: %#v", frontmatter)
		Expect(frontmatter).To(matchFrontmatterTimestamps("2026-03-21T10:15:30Z", "2026-03-21T11:16:31Z"),
			"unexpected timestamp metadata: %#v", frontmatter)
		Expect(frontmatter).To(matchFrontmatterAuthors(newFixtureUserID("alice"), newFixtureUserID("bob")),
			"unexpected author metadata: %#v", frontmatter)
		Expect(strings.TrimSpace(body)).To(Equal("# Body\nHello"), "body changed unexpectedly: %q",

			body)

		// update title and id
		page.Title = "Title B"
		page.ID = newFixturePageID("p1b")
		page.Metadata.UpdatedAt = time.Date(2026, time.March, 21, 12, 17, 32, 0, time.UTC)
		page.Metadata.LastAuthorID = newFixtureUserID("carol")
		{
			err := store.SyncFrontmatterIfExists(page)
			Expect(err).To(Succeed(), "SyncFrontmatterIfExists(update): %v",

				err,
			)
		}

		raw2 := string(readTreeFile(path))
		fm2, body2, err := parseRequiredFrontmatter(raw2)
		Expect(err).To(Succeed(), "ParseFrontmatter: %v", err)
		Expect(fm2).To(matchManagedFrontmatter(newFixturePageID("p1b"), "Title B"),
			"expected updated frontmatter, got %#v", fm2)
		Expect(fm2).To(matchFrontmatterTimestamps("2026-03-21T10:15:30Z", "2026-03-21T12:17:32Z"),
			"expected updated timestamps in frontmatter, got %#v", fm2)
		Expect(fm2).To(matchFrontmatterAuthors(newFixtureUserID("alice"), newFixtureUserID("carol")),
			"expected updated author metadata in frontmatter, got %#v", fm2)
		Expect(strings.TrimSpace(body2)).To(
			Equal("# Body\nHello"), "body changed unexpectedly on update: %q",

			body2)

	})
})

var _ = ginkgo.Describe("node store persistence", ginkgo.Label("unit"), func() {
	ginkgo.It("sync frontmatter if exists preserves existing custom frontmatter", func() {
		tmp := tempTreeDir()
		store := NewNodeStore(tmp)

		root := &PageNode{ID: newFixturePageID("root"), Slug: newFixtureSlug("root"), Title: "root", Kind: NodeKindSection}
		page := &PageNode{ID: newFixturePageID("p1"), Slug: newFixtureSlug("p"), Title: "Title A", Kind: NodeKindPage, Parent: root}

		path := filepath.Join(tmp, "root", "p.md")
		writeTreeFile(path, `---
custom_key: keep-me
aliases:
  - one
leafwiki_id: old-id
leafwiki_title: Old Title
---
# Body
Hello
`, 0o644)
		{

			err := store.SyncFrontmatterIfExists(page)
			Expect(err).To(Succeed(), "SyncFrontmatterIfExists: %v",

				err)
		}

		raw := string(readTreeFile(path))
		Expect(raw).
			To(ContainSubstring("custom_key: keep-me"),

				"expected custom frontmatter to be preserved, got: %q",

				raw)
		Expect(raw).To(ContainSubstring("- one"),

			"expected custom list frontmatter to be preserved, got: %q",

			raw)

		frontmatter, body, err := parseRequiredFrontmatter(raw)
		Expect(err).To(Succeed(), "ParseFrontmatter: %v", err)
		Expect(frontmatter).To(matchManagedFrontmatter(newFixturePageID("p1"), "Title A"),
			"unexpected frontmatter: %#v", frontmatter)
		Expect(strings.TrimSpace(body)).To(Equal(`# Body
Hello`), "body changed unexpectedly: %q",

			body)

	})
})

var _ = ginkgo.Describe("node store persistence", ginkgo.Label("unit"), func() {
	ginkgo.It("sync frontmatter if exists section no index no side effects", func() {
		tmp := tempTreeDir()
		store := NewNodeStore(tmp)

		root := &PageNode{ID: newFixturePageID("root"), Slug: newFixtureSlug("root"), Title: "root", Kind: NodeKindSection}
		sec := &PageNode{ID: newFixturePageID("s1"), Slug: newFixtureSlug("docs"), Title: "Docs", Kind: NodeKindSection, Parent: root}
		{

			// Do NOT create folder: sync must not mkdir via write-path; should return nil.
			err := store.SyncFrontmatterIfExists(sec)
			Expect(err).To(Succeed(), "SyncFrontmatterIfExists(section): %v",

				err)
		}
		{

			// Ensure no folder created implicitly
			_, err := os.Stat(filepath.Join(tmp, "root", "docs"))
			Expect(err).To(MatchError(os.ErrNotExist), "expected no side effects (folder created), but folder exists")
		}

	})
})

var _ = ginkgo.Describe("node store persistence", ginkgo.Label("unit"), func() {
	ginkgo.It("resolve node file vs folder", func() {
		tmp := tempTreeDir()
		store := NewNodeStore(tmp)

		root := &PageNode{ID: newFixturePageID("root"), Slug: newFixtureSlug("root"), Title: "root", Kind: NodeKindSection}

		page := &PageNode{ID: newFixturePageID("p1"), Slug: newFixtureSlug("p"), Title: "P", Kind: NodeKindPage, Parent: root}
		writeTreeFile(filepath.Join(tmp, "root", "p.md"), "# x", 0o644)

		r1, err := store.resolveNode(page)
		Expect(err).To(Succeed(), "resolveNode(page): %v",

			err)
		Expect(r1).To(matchResolvedNode(NodeKindPage, true, HaveSuffix("p.md")),
			"unexpected resolved: %#v", r1)

		sec := &PageNode{ID: newFixturePageID("s1"), Slug: newFixtureSlug("docs"), Title: "Docs", Kind: NodeKindSection, Parent: root}
		secDir := filepath.Join(tmp, "root", "docs")
		createTreeDirectory(secDir)

		r2, err := store.resolveNode(sec)
		Expect(err).To(Succeed(), "resolveNode(sec without index): %v",

			err,
		)
		Expect(r2).To(matchResolvedNode(NodeKindSection, false, nil),
			"expected section without content: %#v", r2)

		writeTreeFile(filepath.Join(secDir, "index.md"), "# idx", 0o644)
		r3, err := store.resolveNode(sec)
		Expect(err).To(Succeed(), "resolveNode(sec with index): %v",

			err)
		Expect(r3).To(matchResolvedNode(NodeKindSection, true, HaveSuffix("index.md")),
			"unexpected resolved: %#v", r3)

	})
})
