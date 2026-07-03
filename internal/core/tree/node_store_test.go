package tree

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/perber/wiki/internal/core/markdown"

	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/types"
)

func writeTreeFile(path string, data string, perm os.FileMode) {
	ginkgo.GinkgoHelper()
	Expect(os.MkdirAll(filepath.Dir(path), 0o755)).To(Succeed())
	Expect(os.WriteFile(path, []byte(data), perm)).To(Succeed())
}

func writeLegacyTreeJSON(storageDir string, tree *PageNode) {
	ginkgo.GinkgoHelper()
	raw, err := json.Marshal(tree)
	Expect(err).NotTo(HaveOccurred())
	Expect(os.WriteFile(filepath.Join(storageDir, "tree.json"), raw, 0o644)).To(Succeed())
}

func createTreeDirectory(path string) {
	ginkgo.GinkgoHelper()
	Expect(os.MkdirAll(path, 0o755)).To(Succeed())
}

func haveCanonicalNodeStoreRawStorage() types.GomegaMatcher {
	return SatisfyAll(
		HavePrefix("<!-- leafwiki\n"),
		Not(HavePrefix("---\n")),
	)
}

var _ = ginkgo.Describe("node store persistence", func() {
	ginkgo.It("load tree missing file returns default root", func() {
		tmp := tempTreeDir()
		store := NewNodeStore(tmp)

		tree, err := store.LoadTree("missing.json")
		Expect(err).To(Succeed(), "LoadTree: %v",

			err)
		Expect(tree).To(matchRootSection(), "unexpected default root: %#v", tree)

	})
})

var _ = ginkgo.Describe("node store persistence", func() {
	ginkgo.It("save tree then load tree assigns parents", func() {
		tmp := tempTreeDir()
		store := NewNodeStore(tmp)

		tree := &PageNode{
			ID:    "root",
			Slug:  "root",
			Title: "root",
			Kind:  NodeKindSection,
			Children: []*PageNode{
				{
					ID:    "s1",
					Slug:  "sec",
					Title: "Section",
					Kind:  NodeKindSection,
					Children: []*PageNode{
						{
							ID:    "p1",
							Slug:  "page",
							Title: "Page",
							Kind:  NodeKindPage,
						},
					},
				},
			},
		}

		writeLegacyTreeJSON(tmp, tree)

		loaded, err := store.LoadTree("tree.json")
		Expect(err).To(Succeed(), "LoadTree: %v",

			err)

		sec := loaded.Children[0]
		p := sec.Children[0]
		Expect(sec.Parent).To(haveParentPageID(RootPageID), "expected section parent root, got %#v", sec.Parent)
		Expect(p.Parent).To(haveParentPageID(newFixturePageID("s1")), "expected page parent s1, got %#v", p.Parent)

	})
})

var _ = ginkgo.Describe("node store persistence", func() {
	ginkgo.It("create page rejects traversal slug", func() {
		baseDir := tempTreeDir()
		rootDir := filepath.Join(baseDir, "content")
		store := NewNodeStoreWithOptions(NodeStoreOptions{DataDir: filepath.Join(baseDir, "data"), RootDir: rootDir})
		parent := &PageNode{ID: "root", Slug: "root", Title: "root", Kind: NodeKindSection}
		entry := &PageNode{ID: "outside", Slug: "../outside", Title: "Outside", Kind: NodeKindPage, Parent: parent}

		err := store.CreatePage(parent, entry)
		Expect(err).To(HaveOccurred(), "expected CreatePage to reject traversal slug")
		Expect(err).To(MatchError(ErrInvalidOperation), "expected ErrInvalidOperation, got %v",

			err)

		Expect(filepath.Join(baseDir, "outside.md")).To(beMissingTreePath())

	})
})

var _ = ginkgo.Describe("node store persistence", func() {
	ginkgo.It("rename node rejects traversal slug", func() {
		baseDir := tempTreeDir()
		rootDir := filepath.Join(baseDir, "content")
		store := NewNodeStoreWithOptions(NodeStoreOptions{DataDir: filepath.Join(baseDir, "data"), RootDir: rootDir})
		parent := &PageNode{ID: "root", Slug: "root", Title: "root", Kind: NodeKindSection}
		entry := &PageNode{ID: "docs", Slug: "docs", Title: "Docs", Kind: NodeKindPage, Parent: parent}
		{
			err := store.CreatePage(parent, entry)
			Expect(err).To(Succeed(), "CreatePage failed: %v",

				err)
		}

		err := store.RenameNode(entry, "../outside")
		Expect(err).To(HaveOccurred(), "expected RenameNode to reject traversal slug")
		Expect(err).To(MatchError(ErrInvalidOperation), "expected ErrInvalidOperation, got %v",

			err)

		statTreePath(filepath.Join(rootDir, "docs.md"))
		Expect(filepath.Join(baseDir, "outside.md")).To(beMissingTreePath())

	})
})

var _ = ginkgo.Describe("node store persistence", func() {
	ginkgo.It("upsert content rejects parentless non root page", func() {
		baseDir := tempTreeDir()
		rootDir := filepath.Join(baseDir, "content")
		store := NewNodeStoreWithOptions(NodeStoreOptions{DataDir: filepath.Join(baseDir, "data"), RootDir: rootDir})
		entry := &PageNode{ID: "loose", Slug: "loose", Title: "Loose", Kind: NodeKindPage}

		err := store.UpsertContent(entry, "# Loose")
		Expect(err).To(HaveOccurred(), "expected UpsertContent to reject parentless non-root page")
		Expect(err).To(MatchError(ErrInvalidOperation), "expected ErrInvalidOperation, got %v",

			err)

		Expect(rootDir + ".md").To(beMissingTreePath())

	})
})

var _ = ginkgo.Describe("node store persistence", func() {
	ginkgo.It("create page rejects symlinked parent escaping root", func() {
		if runtime.GOOS == "windows" {
			ginkgo.Skip("symlink creation requires privileges on Windows")
		}
		baseDir := tempTreeDir()
		rootDir := filepath.Join(baseDir, "content")
		outsideDir := filepath.Join(baseDir, "outside")
		{
			err := os.MkdirAll(rootDir, 0o755)
			Expect(err).To(Succeed(), "mkdir root directory: %v",

				err,
			)
		}
		{

			err := os.MkdirAll(outsideDir, 0o755)
			Expect(err).To(Succeed(), "mkdir outside directory: %v",

				err)
		}
		{

			err := os.Symlink(outsideDir, filepath.Join(rootDir, "docs"))
			Expect(err).To(Succeed(), "create symlinked section directory: %v",

				err)
		}

		store := NewNodeStoreWithOptions(NodeStoreOptions{DataDir: filepath.Join(baseDir, "data"), RootDir: rootDir})
		root := &PageNode{ID: "root", Slug: "root", Title: "root", Kind: NodeKindSection}
		docs := &PageNode{ID: "docs", Slug: "docs", Title: "Docs", Kind: NodeKindSection, Parent: root}
		child := &PageNode{ID: "child", Slug: "child", Title: "Child", Kind: NodeKindPage, Parent: docs}

		err := store.CreatePage(docs, child)
		Expect(err).To(HaveOccurred(), "expected CreatePage to reject symlinked parent outside root")
		Expect(err).To(MatchError(ErrInvalidOperation), "expected ErrInvalidOperation, got %v",

			err)

		Expect(filepath.Join(outsideDir, "child.md")).To(beMissingTreePath())

	})
})

var _ = ginkgo.Describe("node store persistence", func() {
	ginkgo.It("upsert content rejects symlinked parent escaping root", func() {
		if runtime.GOOS == "windows" {
			ginkgo.Skip("symlink creation requires privileges on Windows")
		}
		baseDir := tempTreeDir()
		rootDir := filepath.Join(baseDir, "content")
		outsideDir := filepath.Join(baseDir, "outside")
		{
			err := os.MkdirAll(rootDir, 0o755)
			Expect(err).To(Succeed(), "mkdir root directory: %v",

				err,
			)
		}
		{

			err := os.MkdirAll(outsideDir, 0o755)
			Expect(err).To(Succeed(), "mkdir outside directory: %v",

				err)
		}
		{

			err := os.Symlink(outsideDir, filepath.Join(rootDir, "docs"))
			Expect(err).To(Succeed(), "create symlinked section directory: %v",

				err)
		}

		store := NewNodeStoreWithOptions(NodeStoreOptions{DataDir: filepath.Join(baseDir, "data"), RootDir: rootDir})
		root := &PageNode{ID: "root", Slug: "root", Title: "root", Kind: NodeKindSection}
		docs := &PageNode{ID: "docs", Slug: "docs", Title: "Docs", Kind: NodeKindSection, Parent: root}
		child := &PageNode{ID: "child", Slug: "child", Title: "Child", Kind: NodeKindPage, Parent: docs}

		err := store.UpsertContent(child, "# Child")
		Expect(err).To(HaveOccurred(), "expected UpsertContent to reject symlinked parent outside root")
		Expect(err).To(MatchError(ErrInvalidOperation), "expected ErrInvalidOperation, got %v",

			err)

		Expect(filepath.Join(outsideDir, "child.md")).To(beMissingTreePath())

	})
})

var _ = ginkgo.Describe("node store persistence", func() {
	ginkgo.It("save child order root writes order file", func() {
		tmp := tempTreeDir()
		store := NewNodeStore(tmp)

		root := &PageNode{
			ID:    "root",
			Slug:  "root",
			Title: "root",
			Kind:  NodeKindSection,
			Children: []*PageNode{
				{ID: "a"},
				{ID: "b"},
			},
		}
		{

			err := store.SaveChildOrder(root)
			Expect(err).To(Succeed(), "SaveChildOrder root: %v",

				err,
			)
		}
		{

			_, err := os.Stat(filepath.Join(tmp, "root", ".order.json"))
			Expect(err).To(Succeed(), "expected root order file: %v",

				err)
		}

	})
})

var _ = ginkgo.Describe("node store persistence", func() {
	ginkgo.It("save child order page returns an error without creating directory", func() {
		tmp := tempTreeDir()
		store := NewNodeStore(tmp)

		root := &PageNode{ID: "root", Slug: "root", Title: "root", Kind: NodeKindSection}
		page := &PageNode{ID: "page1", Slug: "docs", Title: "Docs", Kind: NodeKindPage, Parent: root}

		err := store.SaveChildOrder(page)
		Expect(err).To(HaveOccurred(), "expected SaveChildOrder to reject page nodes")

		var opErr *InvalidOpError
		Expect(err).To(matchErrorAs(&opErr),
			"expected InvalidOpError, got %T (%v)",

			err, err,
		)
		{

			_, err := os.Stat(filepath.Join(tmp, "root", "docs"))
			Expect(err).To(MatchError(os.ErrNotExist), "expected no stray page directory, got err=%v",

				err)
		}

	})
})

var _ = ginkgo.Describe("node store persistence", func() {
	ginkgo.It("create section creates folder and index with frontmatter", func() {
		tmp := tempTreeDir()
		store := NewNodeStore(tmp)

		root := &PageNode{ID: "root", Slug: "root", Title: "root", Kind: NodeKindSection}
		sec := &PageNode{
			ID:     "sec1",
			Slug:   "docs",
			Title:  "Docs",
			Kind:   NodeKindSection,
			Parent: root,
			Metadata: PageMetadata{
				CreatedAt:    time.Date(2026, time.March, 22, 10, 15, 30, 0, time.UTC),
				UpdatedAt:    time.Date(2026, time.March, 22, 11, 16, 31, 0, time.UTC),
				CreatorID:    "alice",
				LastAuthorID: "bob",
			},
		}
		{

			err := store.CreateSection(root, sec)
			Expect(err).To(Succeed(), "CreateSection: %v",

				err)
		}

		// expected folder: <tmp>/root/docs
		directory := filepath.Join(tmp, "root", "docs")
		Expect(directory).To(BeADirectory(), "expected section folder")

		index := filepath.Join(directory, "index.md")
		raw := string(readTreeFile(index))
		Expect(raw).To(haveCanonicalNodeStoreRawStorage())
		frontmatter, body, has, err := markdown.ParseFrontmatter(raw)
		Expect(err).To(Succeed(), "ParseFrontmatter: %v",

			err)
		Expect(has).To(BeTrue(), "expected frontmatter in section index")
		Expect(frontmatter).To(matchManagedFrontmatter(newFixturePageID("sec1"), "Docs"),
			"unexpected section frontmatter: %#v", frontmatter)
		Expect(frontmatter).To(matchFrontmatterTimestamps("2026-03-22T10:15:30Z", "2026-03-22T11:16:31Z"),
			"unexpected section timestamp metadata: %#v", frontmatter)
		Expect(frontmatter).To(matchFrontmatterAuthors(newFixtureUserID("alice"), newFixtureUserID("bob")),
			"unexpected section author metadata: %#v", frontmatter)
		Expect(strings.TrimSpace(body)).To(BeEmpty(),

			"expected empty section body, got %q",

			body)

	})
})

var _ = ginkgo.Describe("node store persistence", func() {
	ginkgo.It("create section kind guards", func() {
		tmp := tempTreeDir()
		store := NewNodeStore(tmp)

		rootPageWrong := &PageNode{ID: "root", Slug: "root", Title: "root", Kind: NodeKindPage}
		sec := &PageNode{ID: "sec1", Slug: "docs", Title: "Docs", Kind: NodeKindSection}
		{

			err := store.CreateSection(rootPageWrong, sec)
			Expect(err).To(HaveOccurred(), "expected error when parent is not a section")
		}

		root := &PageNode{ID: "root", Slug: "root", Title: "root", Kind: NodeKindSection}
		pageWrong := &PageNode{ID: "x", Slug: "x", Title: "X", Kind: NodeKindPage}
		{
			err := store.CreateSection(root, pageWrong)
			Expect(err).To(HaveOccurred(), "expected error when new entry is not a section")
		}

	})
})

var _ = ginkgo.Describe("node store persistence", func() {
	ginkgo.It("create page creates markdown with frontmatter", func() {
		tmp := tempTreeDir()
		store := NewNodeStore(tmp)

		root := &PageNode{ID: "root", Slug: "root", Title: "root", Kind: NodeKindSection}
		page := &PageNode{ID: "p1", Slug: "hello", Title: "Hello World", Kind: NodeKindPage, Parent: root}
		{

			err := store.CreatePage(root, page)
			Expect(err).To(Succeed(), "CreatePage: %v",

				err)
		}

		p := filepath.Join(tmp, "root", "hello.md")
		raw, err := os.ReadFile(p)
		Expect(err).To(Succeed(), "read created page: %v",

			err)

		Expect(string(raw)).To(haveCanonicalNodeStoreRawStorage())

		frontmatter, body, has, err := markdown.ParseFrontmatter(string(raw))
		Expect(err).To(Succeed(), "ParseFrontmatter: %v",

			err)
		Expect(has).To(BeTrue(), "expected frontmatter")
		Expect(strings.TrimSpace(frontmatter.
			LeafWikiID)).To(
			Equal("p1"),
			"expected leafwiki_id p1, got %q",

			frontmatter.LeafWikiID)
		Expect(frontmatter.LeafWikiTitle).To(Equal("Hello World"), "expected leafwiki_title 'Hello World', got %q",

			frontmatter.LeafWikiTitle,
		)
		Expect(body).To(ContainSubstring("# Hello World"),

			"expected H1 title in body, got: %q",

			body)

	})
})

var _ = ginkgo.Describe("node store persistence", func() {
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
		frontmatter, _, has, err := markdown.ParseFrontmatter(raw)
		Expect(err).To(Succeed(), "ParseFrontmatter: %v",

			err)
		Expect(has).To(BeTrue(), "expected frontmatter")
		Expect(frontmatter).To(SatisfyAll(
			HaveField("LeafWikiID", Equal("p1")),
			HaveField("LeafWikiTitle", Equal("Hello World")),
		), "expected CreatePage to write managed frontmatter, got %#v", frontmatter)

	})
})

var _ = ginkgo.Describe("node store persistence", func() {
	ginkgo.It("create page rejects existing page file but allows sibling section directory", func() {
		tmp := tempTreeDir()
		store := NewNodeStore(tmp)

		root := &PageNode{ID: "root", Slug: "root", Title: "root", Kind: NodeKindSection}

		writeTreeFile(filepath.Join(tmp, "root", "dup.md"), "x", 0o644)
		page := &PageNode{ID: "p1", Slug: "dup", Title: "Dup", Kind: NodeKindPage, Parent: root}
		{
			err := store.CreatePage(root, page)
			Expect(err).To(HaveOccurred(), "expected PageAlreadyExistsError for existing file")
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

var _ = ginkgo.Describe("node store persistence", func() {
	ginkgo.It("create section rejects existing section directory but allows sibling page file", func() {
		tmp := tempTreeDir()
		store := NewNodeStore(tmp)

		root := &PageNode{ID: "root", Slug: "root", Title: "root", Kind: NodeKindSection}

		createTreeDirectory(filepath.Join(tmp, "root", "dup"))
		section := &PageNode{ID: "s1", Slug: "dup", Title: "Dup", Kind: NodeKindSection, Parent: root}
		{
			err := store.CreateSection(root, section)
			Expect(err).To(HaveOccurred(), "expected PageAlreadyExistsError for existing section directory")
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

var _ = ginkgo.Describe("node store persistence", func() {
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
		frontmatter, body, has, err := markdown.ParseFrontmatter(string(raw))
		Expect(err).To(Succeed(), "ParseFrontmatter: %v",

			err)
		Expect(has).To(BeTrue(), "expected FM to exist")
		Expect(frontmatter).To(SatisfyAll(
			HaveField("LeafWikiID", Equal("p1")),
			HaveField("LeafWikiTitle", Equal("My Page")),
		), "expected overwrite to preserve managed frontmatter, got %#v", frontmatter)
		Expect(strings.TrimSpace(body)).To(Equal("# new"), "expected body '# new', got %q",

			body)

	})
})

var _ = ginkgo.Describe("node store persistence", func() {
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

		frontmatter, body, has, err := markdown.ParseFrontmatter(raw)
		Expect(err).To(Succeed(), "ParseFrontmatter: %v",

			err)
		Expect(has).To(BeTrue(), "expected FM to exist")
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
var _ = ginkgo.Describe("node store persistence", func() {
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
		frontmatter, body, has, err := markdown.ParseFrontmatter(raw)
		Expect(err).To(Succeed(), "ParseFrontmatter: %v",

			err)
		Expect(has).To(BeTrue(), "expected system frontmatter in written file")
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
var _ = ginkgo.Describe("node store persistence", func() {
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
		frontmatter, body, has, err := markdown.ParseFrontmatter(raw)
		Expect(err).To(Succeed(), "ParseFrontmatter: %v",

			err)
		Expect(has).To(BeTrue(), "expected system frontmatter in written file")
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
var _ = ginkgo.Describe("node store persistence", func() {
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

		frontmatter, body, has, err := markdown.ParseFrontmatter(raw)
		Expect(err).To(Succeed(), "ParseFrontmatter: %v",

			err)
		Expect(has).To(BeTrue(), "expected frontmatter in written file")
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

var _ = ginkgo.Describe("node store persistence", func() {
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

var _ = ginkgo.Describe("node store persistence", func() {
	ginkgo.It("move node page moves file strict", func() {
		tmp := tempTreeDir()
		store := NewNodeStore(tmp)

		root := &PageNode{ID: "root", Slug: "root", Title: "root", Kind: NodeKindSection}
		secA := &PageNode{ID: "a", Slug: "a", Title: "A", Kind: NodeKindSection, Parent: root}
		secB := &PageNode{ID: "b", Slug: "b", Title: "B", Kind: NodeKindSection, Parent: root}
		page := &PageNode{ID: "p1", Slug: "p", Title: "P", Kind: NodeKindPage, Parent: secA}

		// create source file at old location (tree-based path)
		src := filepath.Join(tmp, "root", "a", "p.md")
		writeTreeFile(src, "# hi", 0o644)
		{

			err := store.MoveNode(page, secB)
			Expect(err).To(Succeed(), "MoveNode: %v",

				err)
		}

		dst := filepath.Join(tmp, "root", "b", "p.md")
		{
			_, err := os.Stat(dst)
			Expect(err).To(Succeed(), "expected dest file: %v",

				err,
			)
		}
		{

			_, err := os.Stat(src)
			Expect(err).To(MatchError(os.
				ErrNotExist,
			),

				"expected src removed",
			)
		}

	})
})

var _ = ginkgo.Describe("node store persistence", func() {
	ginkgo.It("move node page uses workspace source path", func() {
		tmp := tempTreeDir()
		store := NewNodeStore(tmp)

		root := &PageNode{ID: "root", Slug: "root", Title: "root", Kind: NodeKindSection}
		plans := &PageNode{ID: "plans", Slug: "plans", Title: "Plans", Kind: NodeKindSection, Parent: root}
		archive := &PageNode{ID: "archive", Slug: "archive", Title: "Archive", Kind: NodeKindSection, Parent: root}
		page := &PageNode{
			ID:                  "p1",
			Slug:                "agent-hooks-plan",
			Title:               "Agent Hooks Plan",
			Kind:                NodeKindPage,
			Parent:              plans,
			WorkspaceSourcePath: "plans/agent_hooks.PLAN.md",
		}

		src := filepath.Join(tmp, "root", "plans", "agent_hooks.PLAN.md")
		writeTreeFile(src, "# plan", 0o644)
		{

			err := store.MoveNode(page, archive)
			Expect(err).To(Succeed(), "MoveNode: %v",

				err)
		}

		dst := filepath.Join(tmp, "root", "archive", "agent_hooks.PLAN.md")
		{
			_, err := os.Stat(dst)
			Expect(err).To(Succeed(), "expected retained source filename at destination: %v",

				err,
			)
		}
		{

			_, err := os.Stat(src)
			Expect(err).To(MatchError(os.
				ErrNotExist,
			),

				"expected original source file removed",
			)
		}
		Expect(page.WorkspaceSourcePath).To(
			Equal(newFixtureWorkspaceSourcePath("archive/agent_hooks.PLAN.md")), "WorkspaceSourcePath = %q, want archive/agent_hooks.PLAN.md",

			page.WorkspaceSourcePath)

	})
})

var _ = ginkgo.Describe("node store persistence", func() {
	ginkgo.It("move node drift when missing source", func() {
		tmp := tempTreeDir()
		store := NewNodeStore(tmp)

		root := &PageNode{ID: "root", Slug: "root", Title: "root", Kind: NodeKindSection}
		sec := &PageNode{ID: "s", Slug: "s", Title: "S", Kind: NodeKindSection, Parent: root}
		page := &PageNode{ID: "p1", Slug: "p", Title: "P", Kind: NodeKindPage, Parent: sec}

		err := store.MoveNode(page, root)
		Expect(err).To(HaveOccurred(), "expected DriftError, got nil")

		var de *DriftError
		Expect(err).To(matchErrorAs(&de), "expected DriftError, got %T: %v",

			err, err,
		)

	})
})

var _ = ginkgo.Describe("node store persistence", func() {
	ginkgo.It("create page uses workspace source parent directory", func() {
		tmp := tempTreeDir()
		store := NewNodeStore(tmp)

		root := &PageNode{ID: "root", Slug: "root", Title: "root", Kind: NodeKindSection}
		parent := &PageNode{
			ID:                  "s1",
			Slug:                "my-docs",
			Title:               "My Docs",
			Kind:                NodeKindSection,
			Parent:              root,
			WorkspaceSourcePath: "My Docs",
		}
		page := &PageNode{ID: "p1", Slug: "child", Title: "Child", Kind: NodeKindPage, Parent: parent}
		{

			err := store.CreatePage(parent, page)
			Expect(err).To(Succeed(), "CreatePage: %v",

				err)
		}

		want := filepath.Join(tmp, "root", "My Docs", "child.md")
		{
			_, err := os.Stat(want)
			Expect(err).To(Succeed(), "expected page under retained parent source directory: %v",

				err)
		}
		Expect(page.WorkspaceSourcePath).To(
			Equal(newFixtureWorkspaceSourcePath("My Docs/child.md")), "WorkspaceSourcePath = %q, want My Docs/child.md",

			page.WorkspaceSourcePath,
		)

	})
})

var _ = ginkgo.Describe("node store persistence", func() {
	ginkgo.It("delete page removes file or drift if missing", func() {
		tmp := tempTreeDir()
		store := NewNodeStore(tmp)

		root := &PageNode{ID: "root", Slug: "root", Title: "root", Kind: NodeKindSection}
		page := &PageNode{ID: "p1", Slug: "p", Title: "P", Kind: NodeKindPage, Parent: root}

		path := filepath.Join(tmp, "root", "p.md")
		writeTreeFile(path, "# x", 0o644)
		{

			err := store.DeletePage(page)
			Expect(err).To(Succeed(), "DeletePage: %v",

				err)
		}
		{

			_, err := os.Stat(path)
			Expect(err).To(MatchError(os.
				ErrNotExist,
			),

				"expected file deleted",
			)
		}

		// delete again -> drift
		err := store.DeletePage(page)
		Expect(err).To(HaveOccurred(), "expected DriftError")

	})
})

var _ = ginkgo.Describe("node store persistence", func() {
	ginkgo.It("delete page uses workspace source path", func() {
		tmp := tempTreeDir()
		store := NewNodeStore(tmp)

		root := &PageNode{ID: "root", Slug: "root", Title: "root", Kind: NodeKindSection}
		plans := &PageNode{ID: "plans", Slug: "plans", Title: "Plans", Kind: NodeKindSection, Parent: root}
		page := &PageNode{
			ID:                  "p1",
			Slug:                "agent-hooks-plan",
			Title:               "Agent Hooks Plan",
			Kind:                NodeKindPage,
			Parent:              plans,
			WorkspaceSourcePath: "plans/agent_hooks.PLAN.md",
		}

		rawSource := filepath.Join(tmp, "root", "plans", "agent_hooks.PLAN.md")
		writeTreeFile(rawSource, "# plan", 0o644)
		{

			err := store.DeletePage(page)
			Expect(err).To(Succeed(), "DeletePage: %v",

				err)
		}
		{

			_, err := os.Stat(rawSource)
			Expect(err).To(MatchError(os.
				ErrNotExist,
			),

				"expected raw source file deleted",
			)
		}

	})
})

var _ = ginkgo.Describe("node store persistence", func() {
	ginkgo.It("delete section removes folder recursive or drift if missing", func() {
		tmp := tempTreeDir()
		store := NewNodeStore(tmp)

		root := &PageNode{ID: "root", Slug: "root", Title: "root", Kind: NodeKindSection}
		sec := &PageNode{ID: "s1", Slug: "docs", Title: "Docs", Kind: NodeKindSection, Parent: root}

		directory := filepath.Join(tmp, "root", "docs")
		createTreeDirectory(directory)
		writeTreeFile(filepath.Join(directory, "index.md"), "# hi", 0o644)
		writeTreeFile(filepath.Join(directory, "nested.txt"), "x", 0o644)
		{

			err := store.DeleteSection(sec)
			Expect(err).To(Succeed(), "DeleteSection: %v",

				err)
		}
		{

			_, err := os.Stat(directory)
			Expect(err).To(MatchError(os.
				ErrNotExist,
			),

				"expected folder deleted",
			)
		}

		err := store.DeleteSection(sec)
		Expect(err).To(HaveOccurred(), "expected DriftError")

	})
})

var _ = ginkgo.Describe("node store persistence", func() {
	ginkgo.It("save child order uses workspace source section directory", func() {
		tmp := tempTreeDir()
		store := NewNodeStore(tmp)

		root := &PageNode{ID: "root", Slug: "root", Title: "root", Kind: NodeKindSection}
		parent := &PageNode{
			ID:                  "s1",
			Slug:                "my-docs",
			Title:               "My Docs",
			Kind:                NodeKindSection,
			Parent:              root,
			WorkspaceSourcePath: "My Docs",
			Children: []*PageNode{
				{ID: "p1", Slug: "child", Title: "Child", Kind: NodeKindPage},
			},
		}
		assignParentToChildren(parent)
		{

			err := store.SaveChildOrder(parent)
			Expect(err).To(Succeed(), "SaveChildOrder: %v",

				err)
		}
		{

			_, err := os.Stat(filepath.Join(tmp, "root", "My Docs", orderFilename))
			Expect(err).To(Succeed(), "expected child order under retained source directory: %v",

				err)
		}
		{

			_, err := os.Stat(filepath.Join(tmp, "root", "my-docs", orderFilename))
			Expect(err).To(MatchError(os.
				ErrNotExist,
			),

				"expected no child order file under normalized route directory",
			)
		}

	})
})

var _ = ginkgo.Describe("node store persistence", func() {
	ginkgo.It("rename node page and section", func() {
		tmp := tempTreeDir()
		store := NewNodeStore(tmp)

		root := &PageNode{ID: "root", Slug: "root", Title: "root", Kind: NodeKindSection}

		// page rename
		page := &PageNode{ID: "p1", Slug: "old", Title: "P", Kind: NodeKindPage, Parent: root}
		oldFile := filepath.Join(tmp, "root", "old.md")
		writeTreeFile(oldFile, "# x", 0o644)
		{

			err := store.RenameNode(page, "new")
			Expect(err).To(Succeed(), "RenameNode(page): %v",

				err)
		}
		{

			_, err := os.Stat(filepath.Join(tmp, "root", "new.md"))
			Expect(err).To(Succeed(), "expected new page file")
		}

		// section rename
		sec := &PageNode{ID: "s1", Slug: "docs", Title: "Docs", Kind: NodeKindSection, Parent: root}
		secDir := filepath.Join(tmp, "root", "docs")
		createTreeDirectory(secDir)
		writeTreeFile(filepath.Join(secDir, "index.md"), "# y", 0o644)
		{

			err := store.RenameNode(sec, "docs2")
			Expect(err).To(Succeed(), "RenameNode(section): %v",

				err,
			)
		}
		Expect(filepath.Join(tmp, "root", "docs2")).To(BeADirectory(), "expected renamed section directory")

	})
})

var _ = ginkgo.Describe("node store persistence", func() {
	ginkgo.It("rename node page uses workspace source path and clears default source", func() {
		tmp := tempTreeDir()
		store := NewNodeStore(tmp)

		root := &PageNode{ID: "root", Slug: "root", Title: "root", Kind: NodeKindSection}
		plans := &PageNode{ID: "plans", Slug: "plans", Title: "Plans", Kind: NodeKindSection, Parent: root}
		page := &PageNode{
			ID:                  "p1",
			Slug:                "agent-hooks-plan",
			Title:               "Agent Hooks Plan",
			Kind:                NodeKindPage,
			Parent:              plans,
			WorkspaceSourcePath: "plans/agent_hooks.PLAN.md",
		}
		rawSource := filepath.Join(tmp, "root", "plans", "agent_hooks.PLAN.md")
		writeTreeFile(rawSource, "# plan", 0o644)
		{

			err := store.RenameNode(page, "agent-hooks-v2")
			Expect(err).To(Succeed(), "RenameNode: %v",

				err)
		}
		{

			_, err := os.Stat(filepath.Join(tmp, "root", "plans", "agent-hooks-v2.md"))
			Expect(err).To(Succeed(), "expected renamed canonical page file: %v",

				err)
		}
		{

			_, err := os.Stat(rawSource)
			Expect(err).To(MatchError(os.
				ErrNotExist,
			),

				"expected raw source filename removed",
			)
		}
		Expect(page.WorkspaceSourcePath).To(
			BeEmpty(), "WorkspaceSourcePath = %q, want cleared default source",

			page.WorkspaceSourcePath)

	})
})

var _ = ginkgo.Describe("node store persistence", func() {
	ginkgo.It("rename node rejects empty slug and root", func() {
		tmp := tempTreeDir()
		store := NewNodeStore(tmp)

		root := &PageNode{ID: "root", Slug: "root", Title: "root", Kind: NodeKindSection}
		page := &PageNode{ID: "p1", Slug: "old", Title: "P", Kind: NodeKindPage, Parent: root}
		{

			err := store.RenameNode(page, "   ")
			Expect(err).To(HaveOccurred(), "expected empty slug to be rejected")
		}
		{

			err := store.RenameNode(root, "new-root")
			Expect(err).To(HaveOccurred(), "expected root rename to be rejected")
		}

	})
})

var _ = ginkgo.Describe("node store persistence", func() {
	ginkgo.It("rename node returns nil when slug unchanged", func() {
		tmp := tempTreeDir()
		store := NewNodeStore(tmp)

		root := &PageNode{ID: "root", Slug: "root", Title: "root", Kind: NodeKindSection}
		page := &PageNode{ID: "p1", Slug: "same", Title: "P", Kind: NodeKindPage, Parent: root}
		file := filepath.Join(tmp, "root", "same.md")
		writeTreeFile(file, "# x", 0o644)
		{

			err := store.RenameNode(page, "same")
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

var _ = ginkgo.Describe("node store persistence", func() {
	ginkgo.It("rename node rejects destination collision", func() {
		tmp := tempTreeDir()
		store := NewNodeStore(tmp)

		root := &PageNode{ID: "root", Slug: "root", Title: "root", Kind: NodeKindSection}
		page := &PageNode{ID: "p1", Slug: "old", Title: "P", Kind: NodeKindPage, Parent: root}
		writeTreeFile(filepath.Join(tmp, "root", "old.md"), "# x", 0o644)
		writeTreeFile(filepath.Join(tmp, "root", "new.md"), "# y", 0o644)

		err := store.RenameNode(page, "new")
		Expect(err).To(HaveOccurred(), "expected PageAlreadyExistsError")

		var existsErr *PageAlreadyExistsError
		Expect(err).To(matchErrorAs(&existsErr), "expected PageAlreadyExistsError, got %T: %v",

			err, err)

	})
})

var _ = ginkgo.Describe("node store persistence", func() {
	ginkgo.It("rename node page drift when source is folder", func() {
		tmp := tempTreeDir()
		store := NewNodeStore(tmp)

		root := &PageNode{ID: "root", Slug: "root", Title: "root", Kind: NodeKindSection}
		page := &PageNode{ID: "p1", Slug: "old", Title: "P", Kind: NodeKindPage, Parent: root}
		createTreeDirectory(filepath.Join(tmp, "root", "old.md"))

		err := store.RenameNode(page, "new")
		Expect(err).To(HaveOccurred(), "expected DriftError")

		var de *DriftError
		Expect(err).To(matchErrorAs(&de), "expected DriftError, got %T: %v",

			err, err,
		)

	})
})

var _ = ginkgo.Describe("node store persistence", func() {
	ginkgo.It("rename node section drift when source is file", func() {
		tmp := tempTreeDir()
		store := NewNodeStore(tmp)

		root := &PageNode{ID: "root", Slug: "root", Title: "root", Kind: NodeKindSection}
		sec := &PageNode{ID: "s1", Slug: "docs", Title: "Docs", Kind: NodeKindSection, Parent: root}
		writeTreeFile(filepath.Join(tmp, "root", "docs"), "not a directory", 0o644)

		err := store.RenameNode(sec, "docs2")
		Expect(err).To(HaveOccurred(), "expected DriftError")

		var de *DriftError
		Expect(err).To(matchErrorAs(&de), "expected DriftError, got %T: %v",

			err, err,
		)

	})
})

var _ = ginkgo.Describe("node store persistence", func() {
	ginkgo.It("rename node rejects unknown kind", func() {
		tmp := tempTreeDir()
		store := NewNodeStore(tmp)

		root := &PageNode{ID: "root", Slug: "root", Title: "root", Kind: NodeKindSection}
		entry := &PageNode{ID: "x1", Slug: "weird", Title: "Weird", Kind: NodeKind("mystery"), Parent: root}

		err := store.RenameNode(entry, "other")
		Expect(err).To(HaveOccurred(), "expected InvalidOpError")

		var opErr *InvalidOpError
		Expect(err).To(matchErrorAs(&opErr),
			"expected InvalidOpError, got %T: %v",

			err, err,
		)

	})
})

var _ = ginkgo.Describe("node store persistence", func() {
	ginkgo.It("read page raw section no index returns empty nil", func() {
		tmp := tempTreeDir()
		store := NewNodeStore(tmp)

		root := &PageNode{ID: "root", Slug: "root", Title: "root", Kind: NodeKindSection}
		sec := &PageNode{ID: "s1", Slug: "docs", Title: "Docs", Kind: NodeKindSection, Parent: root}

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
			Expect(err).To(HaveOccurred(), "expected no index.md side effect on read")
		}

	})
})

var _ = ginkgo.Describe("node store persistence", func() {
	ginkgo.It("read page raw page missing is drift", func() {
		tmp := tempTreeDir()
		store := NewNodeStore(tmp)

		root := &PageNode{ID: "root", Slug: "root", Title: "root", Kind: NodeKindSection}
		page := &PageNode{ID: "p1", Slug: "p", Title: "P", Kind: NodeKindPage, Parent: root}

		_, err := store.ReadPageRaw(page)
		Expect(err).To(HaveOccurred(), "expected DriftError")

	})
})

var _ = ginkgo.Describe("node store persistence", func() {
	ginkgo.It("read page content strips frontmatter and preserves body", func() {
		tmp := tempTreeDir()
		store := NewNodeStore(tmp)

		root := &PageNode{ID: "root", Slug: "root", Title: "root", Kind: NodeKindSection}
		page := &PageNode{ID: "p1", Slug: "p", Title: "P", Kind: NodeKindPage, Parent: root}

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

var _ = ginkgo.Describe("node store persistence", func() {
	ginkgo.It("read page content invalid frontmatter returns raw", func() {
		tmp := tempTreeDir()
		store := NewNodeStore(tmp)

		root := &PageNode{ID: "root", Slug: "root", Title: "root", Kind: NodeKindSection}
		page := &PageNode{ID: "p1", Slug: "p", Title: "P", Kind: NodeKindPage, Parent: root}

		path := filepath.Join(tmp, "root", "p.md")
		raw := `---
leafwiki_id: [broken
---
# Body
Hello
`
		writeTreeFile(path, raw, 0o644)

		content, err := store.ReadPageContent(page)
		Expect(err).To(HaveOccurred(), "expected parse error for invalid frontmatter")
		Expect(content).To(Equal(raw),
			"expected raw content fallback on parse error, got %q",

			content)

	})
})

var _ = ginkgo.Describe("node store persistence", func() {
	ginkgo.It("sync frontmatter if exists page updates or adds frontmatter", func() {
		tmp := tempTreeDir()
		store := NewNodeStore(tmp)

		root := &PageNode{ID: "root", Slug: "root", Title: "root", Kind: NodeKindSection}
		page := &PageNode{
			ID:     "p1",
			Slug:   "p",
			Title:  "Title A",
			Kind:   NodeKindPage,
			Parent: root,
			Metadata: PageMetadata{
				CreatedAt:    time.Date(2026, time.March, 21, 10, 15, 30, 0, time.UTC),
				UpdatedAt:    time.Date(2026, time.March, 21, 11, 16, 31, 0, time.UTC),
				CreatorID:    "alice",
				LastAuthorID: "bob",
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
		frontmatter, body, has, err := markdown.ParseFrontmatter(raw)
		Expect(err).To(Succeed(), "ParseFrontmatter: %v",

			err)
		Expect(has).To(BeTrue(), "expected frontmatter after sync")
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
		page.ID = "p1b"
		page.Metadata.UpdatedAt = time.Date(2026, time.March, 21, 12, 17, 32, 0, time.UTC)
		page.Metadata.LastAuthorID = "carol"
		{
			err := store.SyncFrontmatterIfExists(page)
			Expect(err).To(Succeed(), "SyncFrontmatterIfExists(update): %v",

				err,
			)
		}

		raw2 := string(readTreeFile(path))
		fm2, body2, has2, err := markdown.ParseFrontmatter(raw2)
		Expect(err).To(Succeed(), "ParseFrontmatter: %v",

			err)
		Expect(has2).To(BeTrue(), "expected frontmatter after update")
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

var _ = ginkgo.Describe("node store persistence", func() {
	ginkgo.It("sync frontmatter if exists preserves existing custom frontmatter", func() {
		tmp := tempTreeDir()
		store := NewNodeStore(tmp)

		root := &PageNode{ID: "root", Slug: "root", Title: "root", Kind: NodeKindSection}
		page := &PageNode{ID: "p1", Slug: "p", Title: "Title A", Kind: NodeKindPage, Parent: root}

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

		frontmatter, body, has, err := markdown.ParseFrontmatter(raw)
		Expect(err).To(Succeed(), "ParseFrontmatter: %v",

			err)
		Expect(has).To(BeTrue(), "expected FM to exist")
		Expect(frontmatter).To(matchManagedFrontmatter(newFixturePageID("p1"), "Title A"),
			"unexpected frontmatter: %#v", frontmatter)
		Expect(strings.TrimSpace(body)).To(Equal(`# Body
Hello`), "body changed unexpectedly: %q",

			body)

	})
})

var _ = ginkgo.Describe("node store persistence", func() {
	ginkgo.It("sync frontmatter if exists section no index no side effects", func() {
		tmp := tempTreeDir()
		store := NewNodeStore(tmp)

		root := &PageNode{ID: "root", Slug: "root", Title: "root", Kind: NodeKindSection}
		sec := &PageNode{ID: "s1", Slug: "docs", Title: "Docs", Kind: NodeKindSection, Parent: root}
		{

			// Do NOT create folder: sync must not mkdir via write-path; should return nil.
			err := store.SyncFrontmatterIfExists(sec)
			Expect(err).To(Succeed(), "SyncFrontmatterIfExists(section): %v",

				err)
		}
		{

			// Ensure no folder created implicitly
			_, err := os.Stat(filepath.Join(tmp, "root", "docs"))
			Expect(err).To(HaveOccurred(), "expected no side effects (folder created), but folder exists")
		}

	})
})

var _ = ginkgo.Describe("node store persistence", func() {
	ginkgo.It("resolve node file vs folder", func() {
		tmp := tempTreeDir()
		store := NewNodeStore(tmp)

		root := &PageNode{ID: "root", Slug: "root", Title: "root", Kind: NodeKindSection}

		page := &PageNode{ID: "p1", Slug: "p", Title: "P", Kind: NodeKindPage, Parent: root}
		writeTreeFile(filepath.Join(tmp, "root", "p.md"), "# x", 0o644)

		r1, err := store.resolveNode(page)
		Expect(err).To(Succeed(), "resolveNode(page): %v",

			err)
		Expect(r1).To(matchResolvedNode(NodeKindPage, BeTrue(), HaveSuffix("p.md")),
			"unexpected resolved: %#v", r1)

		sec := &PageNode{ID: "s1", Slug: "docs", Title: "Docs", Kind: NodeKindSection, Parent: root}
		secDir := filepath.Join(tmp, "root", "docs")
		createTreeDirectory(secDir)

		r2, err := store.resolveNode(sec)
		Expect(err).To(Succeed(), "resolveNode(sec without index): %v",

			err,
		)
		Expect(r2).To(SatisfyAll(
			HaveField("Kind", Equal(NodeKindSection)),
			HaveField("HasContent", BeFalse()),
		), "expected section without content: %#v", r2)

		writeTreeFile(filepath.Join(secDir, "index.md"), "# idx", 0o644)
		r3, err := store.resolveNode(sec)
		Expect(err).To(Succeed(), "resolveNode(sec with index): %v",

			err)
		Expect(r3).To(matchResolvedNode(NodeKindSection, BeTrue(), HaveSuffix("index.md")),
			"unexpected resolved: %#v", r3)

	})
})

var _ = ginkgo.Describe("node store persistence", func() {
	ginkgo.It("convert node page to section moves to index", func() {
		tmp := tempTreeDir()
		store := NewNodeStore(tmp)

		root := &PageNode{ID: "root", Slug: "root", Title: "root", Kind: NodeKindSection}
		entry := &PageNode{ID: "p1", Slug: "p", Title: "P", Kind: NodeKindPage, Parent: root}

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

var _ = ginkgo.Describe("node store persistence", func() {
	ginkgo.It("convert node page to section preserves existing metadata and body", func() {
		tmp := tempTreeDir()
		store := NewNodeStore(tmp)

		root := &PageNode{ID: "root", Slug: "root", Title: "root", Kind: NodeKindSection}
		entry := &PageNode{
			ID:     "p1",
			Slug:   "p",
			Title:  "Section Title",
			Kind:   NodeKindPage,
			Parent: root,
			Metadata: PageMetadata{
				CreatedAt:    time.Date(2026, time.March, 22, 10, 15, 30, 0, time.UTC),
				UpdatedAt:    time.Date(2026, time.March, 22, 11, 16, 31, 0, time.UTC),
				CreatorID:    "alice",
				LastAuthorID: "bob",
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

		frontmatter, body, has, err := markdown.ParseFrontmatter(raw)
		Expect(err).To(Succeed(), "ParseFrontmatter: %v",

			err)
		Expect(has).To(BeTrue(), "expected frontmatter after conversion")
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

var _ = ginkgo.Describe("node store persistence", func() {
	ginkgo.It("convert node section to page rejects non empty folder", func() {
		tmp := tempTreeDir()
		store := NewNodeStore(tmp)

		root := &PageNode{ID: "root", Slug: "root", Title: "root", Kind: NodeKindSection}
		entry := &PageNode{ID: "s1", Slug: "docs", Title: "Docs", Kind: NodeKindSection, Parent: root}

		directory := filepath.Join(tmp, "root", "docs")
		createTreeDirectory(directory)
		writeTreeFile(filepath.Join(directory, "index.md"), "# idx", 0o644)
		writeTreeFile(filepath.Join(directory, "other.txt"), "nope", 0o644)

		err := store.ConvertNode(entry, NodeKindPage)
		Expect(err).To(HaveOccurred(), "expected ConvertNotAllowedError")

		var cna *ConvertNotAllowedError
		Expect(err).To(matchErrorAs(&cna), "expected ConvertNotAllowedError, got %T: %v",

			err,

			err)

	})
})

var _ = ginkgo.Describe("node store persistence", func() {
	ginkgo.It("convert node section to page with index moves and removes folder", func() {
		tmp := tempTreeDir()
		store := NewNodeStore(tmp)

		root := &PageNode{ID: "root", Slug: "root", Title: "root", Kind: NodeKindSection}
		entry := &PageNode{ID: "s1", Slug: "docs", Title: "Docs", Kind: NodeKindSection, Parent: root}

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

var _ = ginkgo.Describe("node store persistence", func() {
	ginkgo.It("convert node section to page no index creates empty page with frontmatter", func() {
		tmp := tempTreeDir()
		store := NewNodeStore(tmp)

		root := &PageNode{ID: "root", Slug: "root", Title: "root", Kind: NodeKindSection}
		entry := &PageNode{ID: "s1", Slug: "docs", Title: "Docs", Kind: NodeKindSection, Parent: root}

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
		frontmatter, _, has, err := markdown.ParseFrontmatter(raw)
		Expect(err).To(Succeed(), "ParseFrontmatter: %v",

			err)
		Expect(has).To(BeTrue(), "expected frontmatter after conversion")
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

var _ = ginkgo.Describe("node store persistence", func() {
	ginkgo.It("convert node section to page with order metadata preserves index content", func() {
		tmp := tempTreeDir()
		store := NewNodeStore(tmp)

		root := &PageNode{ID: "root", Slug: "root", Title: "root", Kind: NodeKindSection}
		entry := &PageNode{ID: "s1", Slug: "docs", Title: "Docs", Kind: NodeKindSection, Parent: root}

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

var _ = ginkgo.Describe("node store persistence", func() {
	ginkgo.It("move node section moves folder strict", func() {
		tmp := tempTreeDir()
		store := NewNodeStore(tmp)

		root := &PageNode{ID: "root", Slug: "root", Title: "root", Kind: NodeKindSection}
		secA := &PageNode{ID: "a", Slug: "a", Title: "A", Kind: NodeKindSection, Parent: root}
		secB := &PageNode{ID: "b", Slug: "b", Title: "B", Kind: NodeKindSection, Parent: root}
		entry := &PageNode{ID: "s1", Slug: "docs", Title: "Docs", Kind: NodeKindSection, Parent: secA}

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

var _ = ginkgo.Describe("node store persistence", func() {
	ginkgo.It("move node page drift when source is folder", func() {
		tmp := tempTreeDir()
		store := NewNodeStore(tmp)

		root := &PageNode{ID: "root", Slug: "root", Title: "root", Kind: NodeKindSection}
		sec := &PageNode{ID: "s", Slug: "s", Title: "S", Kind: NodeKindSection, Parent: root}
		page := &PageNode{ID: "p1", Slug: "p", Title: "P", Kind: NodeKindPage, Parent: sec}

		createTreeDirectory(filepath.Join(tmp, "root", "s", "p.md"))

		err := store.MoveNode(page, root)
		Expect(err).To(HaveOccurred(), "expected DriftError")

		var de *DriftError
		Expect(err).To(matchErrorAs(&de), "expected DriftError, got %T: %v",

			err, err,
		)

	})
})

var _ = ginkgo.Describe("node store persistence", func() {
	ginkgo.It("move node section drift when source is file", func() {
		tmp := tempTreeDir()
		store := NewNodeStore(tmp)

		root := &PageNode{ID: "root", Slug: "root", Title: "root", Kind: NodeKindSection}
		sec := &PageNode{ID: "s", Slug: "s", Title: "S", Kind: NodeKindSection, Parent: root}
		entry := &PageNode{ID: "p1", Slug: "docs", Title: "Docs", Kind: NodeKindSection, Parent: sec}

		writeTreeFile(filepath.Join(tmp, "root", "s", "docs"), "not a directory", 0o644)

		err := store.MoveNode(entry, root)
		Expect(err).To(HaveOccurred(), "expected DriftError")

		var de *DriftError
		Expect(err).To(matchErrorAs(&de), "expected DriftError, got %T: %v",

			err, err,
		)

	})
})

var _ = ginkgo.Describe("node store persistence", func() {
	ginkgo.It("move node rejects destination collision", func() {
		tmp := tempTreeDir()
		store := NewNodeStore(tmp)

		root := &PageNode{ID: "root", Slug: "root", Title: "root", Kind: NodeKindSection}
		secA := &PageNode{ID: "a", Slug: "a", Title: "A", Kind: NodeKindSection, Parent: root}
		secB := &PageNode{ID: "b", Slug: "b", Title: "B", Kind: NodeKindSection, Parent: root}
		page := &PageNode{ID: "p1", Slug: "p", Title: "P", Kind: NodeKindPage, Parent: secA}

		src := filepath.Join(tmp, "root", "a", "p.md")
		dst := filepath.Join(tmp, "root", "b", "p.md")
		writeTreeFile(src, "# hi", 0o644)
		writeTreeFile(dst, "# existing", 0o644)

		err := store.MoveNode(page, secB)
		Expect(err).To(HaveOccurred(), "expected PageAlreadyExistsError")

		var existsErr *PageAlreadyExistsError
		Expect(err).To(matchErrorAs(&existsErr), "expected PageAlreadyExistsError, got %T: %v",

			err, err)

	})
})

var _ = ginkgo.Describe("node store persistence", func() {
	ginkgo.It("convert node page to section creates index when page missing", func() {
		tmp := tempTreeDir()
		store := NewNodeStore(tmp)

		root := &PageNode{ID: "root", Slug: "root", Title: "root", Kind: NodeKindSection}
		entry := &PageNode{ID: "p1", Slug: "p", Title: "P", Kind: NodeKindPage, Parent: root}
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

var _ = ginkgo.Describe("node store persistence", func() {
	ginkgo.It("convert node rejects unknown target", func() {
		tmp := tempTreeDir()
		store := NewNodeStore(tmp)

		root := &PageNode{ID: "root", Slug: "root", Title: "root", Kind: NodeKindSection}
		entry := &PageNode{ID: "p1", Slug: "p", Title: "P", Kind: NodeKindPage, Parent: root}

		err := store.ConvertNode(entry, NodeKind("weird"))
		Expect(err).To(HaveOccurred(), "expected InvalidOpError")

		var opErr *InvalidOpError
		Expect(err).To(matchErrorAs(&opErr),
			"expected InvalidOpError, got %T: %v",

			err, err,
		)

	})
})

var _ = ginkgo.Describe("node store persistence", func() {
	ginkgo.It("convert node section to page drift when path is file", func() {
		tmp := tempTreeDir()
		store := NewNodeStore(tmp)

		root := &PageNode{ID: "root", Slug: "root", Title: "root", Kind: NodeKindSection}
		entry := &PageNode{ID: "s1", Slug: "docs", Title: "Docs", Kind: NodeKindSection, Parent: root}

		writeTreeFile(filepath.Join(tmp, "root", "docs"), "not a directory", 0o644)

		err := store.ConvertNode(entry, NodeKindPage)
		Expect(err).To(HaveOccurred(), "expected DriftError")

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
