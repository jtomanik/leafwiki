package tree

import (
	. "github.com/onsi/gomega"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	ginkgo "github.com/onsi/ginkgo/v2"
)

var _ = ginkgo.Describe("node store persistence", ginkgo.Label("unit"), func() {
	ginkgo.It("create page sets LeafWiki title initially", func() {
		tmp := tempTreeDir()
		store := NewNodeStore(tmp)

		root := &PageNode{ID: "root", Slug: "root", Title: "root", Kind: NodeKindSection}
		page := &PageNode{ID: "p1", Slug: "hello", Title: "Hello World", Kind: NodeKindPage, Parent: root}
		{

			err := store.CreatePage(root, page)
			Expect(err).To(Succeed(), "CreatePage: %v",

				err)
		}

		raw := string(readTreeFile(filepath.Join(tmp, "root", "hello.md")))
		frontmatter, _, err := parseRequiredFrontmatter(raw)
		Expect(err).To(Succeed(), "ParseFrontmatter: %v", err)
		Expect(frontmatter).To(SatisfyAll(
			HaveField("LeafWikiID", Equal("p1")),
			HaveField("LeafWikiTitle", Equal("Hello World")),
		), "expected CreatePage to write managed frontmatter, got %#v", frontmatter)

	})
})

var _ = ginkgo.Describe("node store persistence", ginkgo.Label("unit"), func() {
	ginkgo.It("create page rejects existing page file but allows sibling section directory", func() {
		tmp := tempTreeDir()
		store := NewNodeStore(tmp)

		root := &PageNode{ID: "root", Slug: "root", Title: "root", Kind: NodeKindSection}

		writeTreeFile(filepath.Join(tmp, "root", "dup.md"), "x", 0o644)
		page := &PageNode{ID: "p1", Slug: "dup", Title: "Dup", Kind: NodeKindPage, Parent: root}
		{
			err := store.CreatePage(root, page)
			var existsErr *PageAlreadyExistsError
			Expect(err).To(matchErrorAs(&existsErr), "expected PageAlreadyExistsError, got %T: %v", err, err)
		}

		createTreeDirectory(filepath.Join(tmp, "root", "sync"))
		writeTreeFile(filepath.Join(tmp, "root", "sync", "index.md"), "# Section", 0o644)
		page2 := &PageNode{ID: "p2", Slug: "sync", Title: "Sync Page", Kind: NodeKindPage, Parent: root}
		{
			err := store.CreatePage(root, page2)
			Expect(err).To(Succeed(), "CreatePage should allow sibling section directory with same basename: %v",

				err)
		}
		{

			_, err := os.Stat(filepath.Join(tmp, "root", "sync.md"))
			Expect(err).To(Succeed(), "expected page file next to section directory: %v",

				err)
		}
		{

			_, err := os.Stat(filepath.Join(tmp, "root", "sync", "index.md"))
			Expect(err).To(Succeed(), "expected sibling section index to remain: %v",

				err,
			)
		}

	})
})

var _ = ginkgo.Describe("node store persistence", ginkgo.Label("unit"), func() {
	ginkgo.It("create section rejects existing section directory but allows sibling page file", func() {
		tmp := tempTreeDir()
		store := NewNodeStore(tmp)

		root := &PageNode{ID: "root", Slug: "root", Title: "root", Kind: NodeKindSection}

		createTreeDirectory(filepath.Join(tmp, "root", "dup"))
		section := &PageNode{ID: "s1", Slug: "dup", Title: "Dup", Kind: NodeKindSection, Parent: root}
		{
			err := store.CreateSection(root, section)
			var existsErr *PageAlreadyExistsError
			Expect(err).To(matchErrorAs(&existsErr), "expected PageAlreadyExistsError, got %T: %v", err, err)
		}

		writeTreeFile(filepath.Join(tmp, "root", "sync.md"), "# Page", 0o644)
		section2 := &PageNode{ID: "s2", Slug: "sync", Title: "Sync Section", Kind: NodeKindSection, Parent: root}
		{
			err := store.CreateSection(root, section2)
			Expect(err).To(Succeed(), "CreateSection should allow sibling page file with same basename: %v",

				err)
		}
		{

			_, err := os.Stat(filepath.Join(tmp, "root", "sync.md"))
			Expect(err).To(Succeed(), "expected sibling page file to remain: %v",

				err)
		}
		{

			_, err := os.Stat(filepath.Join(tmp, "root", "sync", "index.md"))
			Expect(err).To(Succeed(), "expected section index next to page file: %v",

				err,
			)
		}

	})
})

var _ = ginkgo.Describe("node store persistence", ginkgo.Label("unit"), func() {
	ginkgo.It("upsert content page creates or updates preserves mode", func() {
		tmp := tempTreeDir()
		store := NewNodeStore(tmp)

		root := &PageNode{ID: "root", Slug: "root", Title: "root", Kind: NodeKindSection}
		page := &PageNode{ID: "p1", Slug: "p", Title: "My Page", Kind: NodeKindPage, Parent: root}

		// create with custom mode
		path := filepath.Join(tmp, "root", "p.md")
		writeTreeFile(path, "# old", 0o600)
		{

			err := store.UpsertContent(page, "# new")
			Expect(err).To(Succeed(), "UpsertContent: %v",

				err)
		}

		st, err := os.Stat(path)
		Expect(err).To(Succeed(), "stat: %v",

			err)

		// permissions should stay (best-effort; Windows behaves differently sometimes)
		if runtime.GOOS != "windows" {
			Expect(st.Mode().Perm()).To(Equal(os.FileMode(0o600)), "expected perm 0600, got %o",

				st.
					Mode().Perm())

		}

		raw, _ := os.ReadFile(path)
		Expect(string(raw)).To(haveCanonicalNodeStoreRawStorage())
		frontmatter, body, err := parseRequiredFrontmatter(string(raw))
		Expect(err).To(Succeed(), "ParseFrontmatter: %v", err)
		Expect(frontmatter).To(SatisfyAll(
			HaveField("LeafWikiID", Equal("p1")),
			HaveField("LeafWikiTitle", Equal("My Page")),
		), "expected overwrite to preserve managed frontmatter, got %#v", frontmatter)
		Expect(strings.TrimSpace(body)).To(Equal("# new"), "expected body '# new', got %q",

			body)

	})
})

var _ = ginkgo.Describe("node store persistence", ginkgo.Label("unit"), func() {
	ginkgo.It("upsert content preserves existing custom frontmatter", func() {
		tmp := tempTreeDir()
		store := NewNodeStore(tmp)

		root := &PageNode{ID: "root", Slug: "root", Title: "root", Kind: NodeKindSection}
		page := &PageNode{ID: "p1", Slug: "p", Title: "My Page", Kind: NodeKindPage, Parent: root}

		path := filepath.Join(tmp, "root", "p.md")
		writeTreeFile(path, `---
custom_key: keep-me
tags:
  - alpha
leafwiki_id: old-id
leafwiki_title: Old Title
---
# old
`, 0o644)
		{

			err := store.UpsertContent(page, "# new")
			Expect(err).To(Succeed(), "UpsertContent: %v",

				err)
		}

		raw := string(readTreeFile(path))
		Expect(raw).
			To(ContainSubstring("custom_key: keep-me"),

				"expected custom frontmatter to be preserved, got: %q",

				raw)
		Expect(raw).To(ContainSubstring("- alpha"),

			"expected custom list frontmatter to be preserved, got: %q",

			raw)

		frontmatter, body, err := parseRequiredFrontmatter(raw)
		Expect(err).To(Succeed(), "ParseFrontmatter: %v", err)
		Expect(frontmatter).To(SatisfyAll(
			HaveField("LeafWikiID", Equal("p1")),
			HaveField("LeafWikiTitle", Equal("My Page")),
		), "expected replaced body to preserve managed frontmatter, got %#v", frontmatter)
		Expect(strings.TrimSpace(body)).To(Equal("# new"), "expected body '# new', got %q",

			body)

	})
})

// UpsertContent must treat incoming content that looks like frontmatter as plain
// body text — matching the UI behaviour where the editor sends raw markdown.
var _ = ginkgo.Describe("node store persistence", ginkgo.Label("unit"), func() {
	ginkgo.It("upsert content raw frontmatter treated as plain body", func() {
		tmp := tempTreeDir()
		store := NewNodeStore(tmp)

		root := &PageNode{ID: "root", Slug: "root", Title: "root", Kind: NodeKindSection}
		page := &PageNode{ID: "p1", Slug: "p", Title: "My Page", Kind: NodeKindPage, Parent: root}
		{

			err := store.CreatePage(root, page)
			Expect(err).To(Succeed(), "CreatePage: %v",

				err)
		}

		rawContent := "---\naliases:\n  - alpha\ncustom_key: keep-me\ntitle: Imported Title\n---\n\n# Imported Title\nBody"
		{
			err := store.UpsertContent(page, rawContent)
			Expect(err).To(Succeed(), "UpsertContent: %v",

				err)
		}

		path := filepath.Join(tmp, "root", "p.md")
		raw := string(readTreeFile(path))
		frontmatter, body, err := parseRequiredFrontmatter(raw)
		Expect(err).To(Succeed(), "ParseFrontmatter: %v", err)
		Expect(body).
			To(ContainSubstring("custom_key: keep-me"),

				"expected raw content in body, got: %q",

				body)
		Expect(body).To(ContainSubstring("# Imported Title"),

			"expected heading in body, got: %q",

			body)
		Expect(frontmatter.ExtraFields).NotTo(HaveKey("custom_key"), "expected custom_key to stay as body, got ExtraFields %#v",

			frontmatter.ExtraFields)
		Expect(frontmatter).To(SatisfyAll(
			HaveField("LeafWikiID", Equal("p1")),
			HaveField("LeafWikiTitle", Equal("My Page")),
		), "expected imported content to preserve managed frontmatter, got %#v", frontmatter)

		// The full raw input must appear verbatim in the body.

		// No user keys must leak into system ExtraFields.

		// Managed fields must come from the page node, not from user content.

	})
})

// Regression test for #942: content typed in the UI that looks like frontmatter
// must be stored as plain body text, not extracted and merged into system frontmatter.
var _ = ginkgo.Describe("node store persistence", ginkgo.Label("unit"), func() {
	ginkgo.It("upsert content treats leading frontmatter as plain body", func() {
		tmp := tempTreeDir()
		store := NewNodeStore(tmp)

		root := &PageNode{ID: "root", Slug: "root", Title: "root", Kind: NodeKindSection}
		page := &PageNode{ID: "p1", Slug: "p", Title: "My Page", Kind: NodeKindPage, Parent: root}
		{

			err := store.CreatePage(root, page)
			Expect(err).To(Succeed(), "CreatePage: %v",

				err)
		}

		userContent := "---\ncustom: bar\ntitle: user-title\n---\n\n# Heading"
		{
			err := store.UpsertContent(page, userContent)
			Expect(err).To(Succeed(), "UpsertContent: %v",

				err)
		}

		path := filepath.Join(tmp, "root", "p.md")
		raw := string(readTreeFile(path))
		frontmatter, body, err := parseRequiredFrontmatter(raw)
		Expect(err).To(Succeed(), "ParseFrontmatter: %v", err)
		Expect(frontmatter).To(SatisfyAll(
			HaveField("LeafWikiID", Equal("p1")),
			HaveField("ExtraFields", Not(SatisfyAny(HaveKey("custom"), HaveKey("title")))),
		), "expected managed frontmatter without user keys, got %#v", frontmatter)
		Expect(body).To(ContainSubstring("custom: bar"),

			"expected user frontmatter block preserved in body, got: %q",

			body)
		Expect(body).To(ContainSubstring("# Heading"),

			"expected heading in body, got: %q",

			body)

		// System frontmatter must use the page's managed identity, not the user-supplied value.

		// User-supplied keys must NOT be extracted into ExtraFields.

		// The full user content (including the frontmatter-like block) must be in the body.

	})
})

// UpsertContentPreservingFrontmatter is the legacy-named importer path: it parses
// incoming metadata/frontmatter and writes the canonical metadata comment.
var _ = ginkgo.Describe("node store persistence", ginkgo.Label("unit"), func() {
	ginkgo.It("upsert content preserving frontmatter merges extras into written metadata", func() {
		tmp := tempTreeDir()
		store := NewNodeStore(tmp)

		root := &PageNode{ID: "root", Slug: "root", Title: "root", Kind: NodeKindSection}
		page := &PageNode{ID: "p1", Slug: "p", Title: "My Page", Kind: NodeKindPage, Parent: root}
		{

			err := store.CreatePage(root, page)
			Expect(err).To(Succeed(), "CreatePage: %v",

				err)
		}

		rawContent := "---\naliases:\n  - alpha\ncustom_key: keep-me\nleafwiki_id: source-id\nleafwiki_title: Source Title\ntitle: Imported Title\n---\n\n# Imported Title\nBody"
		{
			err := store.UpsertContentPreservingFrontmatter(page, rawContent)
			Expect(err).To(Succeed(), "UpsertContentPreservingFrontmatter: %v",

				err)
		}

		path := filepath.Join(tmp, "root", "p.md")
		raw := string(readTreeFile(path))
		Expect(raw).To(HavePrefix("<!-- leafwiki\n"),

			"expected canonical metadata comment, got: %q",

			raw)
		Expect(raw).NotTo(HavePrefix("---\n"),

			"expected YAML frontmatter to be removed, got: %q",

			raw)

		frontmatter, body, err := parseRequiredFrontmatter(raw)
		Expect(err).To(Succeed(), "ParseFrontmatter: %v", err)
		Expect(body).To(Equal("\n# Imported Title\nBody"), "unexpected body: %q",

			body,
		)
		Expect(frontmatter).To(SatisfyAll(
			HaveField("LeafWikiID", BeEquivalentTo(newFixturePageID("p1"))),
			HaveField("ExtraFields", SatisfyAll(
				HaveKeyWithValue("custom_key", Equal("keep-me")),
				HaveKeyWithValue("title", Equal("Imported Title")),
				HaveKeyWithValue("aliases", ConsistOf("alpha")),
			)),
		), "expected imported frontmatter fields to be preserved, got %#v", frontmatter)
		Expect(raw).NotTo(ContainSubstring("leafwiki_id: source-id"),

			"expected source leafwiki_id to be dropped, got: %q",

			raw)

	})
})

var _ = ginkgo.Describe("node store persistence", ginkgo.Label("unit"), func() {
	ginkgo.It("upsert content section writes index and creates directory", func() {
		tmp := tempTreeDir()
		store := NewNodeStore(tmp)

		root := &PageNode{ID: "root", Slug: "root", Title: "root", Kind: NodeKindSection}
		sec := &PageNode{ID: "s1", Slug: "docs", Title: "Docs", Kind: NodeKindSection, Parent: root}
		{

			err := store.UpsertContent(sec, "# docs")
			Expect(err).To(Succeed(), "UpsertContent: %v",

				err)
		}

		index := filepath.Join(tmp, "root", "docs", "index.md")
		{
			_, err := os.Stat(index)
			Expect(err).To(Succeed(), "expected index.md to exist: %v",

				err)
		}

	})
})
