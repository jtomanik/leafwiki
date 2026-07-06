package tree

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

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

var _ = ginkgo.Describe("node store persistence", ginkgo.Label("unit"), func() {
	ginkgo.It("load tree missing file returns default root", func() {
		tmp := tempTreeDir()
		store := NewNodeStore(tmp)

		tree, err := store.LoadTree("missing.json")
		Expect(err).To(Succeed(), "LoadTree: %v",

			err)
		Expect(tree).To(matchRootSection(), "unexpected default root: %#v", tree)

	})
})

var _ = ginkgo.Describe("node store persistence", ginkgo.Label("unit"), func() {
	ginkgo.It("save tree then load tree assigns parents", func() {
		tmp := tempTreeDir()
		store := NewNodeStore(tmp)

		tree := &PageNode{
			ID:    newFixturePageID("root"),
			Slug:  newFixtureSlug("root"),
			Title: "root",
			Kind:  NodeKindSection,
			Children: []*PageNode{
				{
					ID:    newFixturePageID("s1"),
					Slug:  newFixtureSlug("sec"),
					Title: "Section",
					Kind:  NodeKindSection,
					Children: []*PageNode{
						{
							ID:    newFixturePageID("p1"),
							Slug:  newFixtureSlug("page"),
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

var _ = ginkgo.Describe("node store persistence", ginkgo.Label("unit"), func() {
	ginkgo.It("create page rejects traversal slug", func() {
		baseDir := tempTreeDir()
		rootDir := filepath.Join(baseDir, "content")
		store := NewNodeStoreWithOptions(NodeStoreOptions{DataDir: filepath.Join(baseDir, "data"), RootDir: rootDir})
		parent := &PageNode{ID: newFixturePageID("root"), Slug: newFixtureSlug("root"), Title: "root", Kind: NodeKindSection}
		entry := &PageNode{ID: newFixturePageID("outside"), Slug: newFixtureSlug("../outside"), Title: "Outside", Kind: NodeKindPage, Parent: parent}

		err := store.CreatePage(parent, entry)
		Expect(err).To(MatchError(ErrInvalidOperation), "expected ErrInvalidOperation, got %v",

			err)

		Expect(filepath.Join(baseDir, "outside.md")).To(beMissingTreePath())

	})
})

var _ = ginkgo.Describe("node store persistence", ginkgo.Label("unit"), func() {
	ginkgo.It("rename node rejects traversal slug", func() {
		baseDir := tempTreeDir()
		rootDir := filepath.Join(baseDir, "content")
		store := NewNodeStoreWithOptions(NodeStoreOptions{DataDir: filepath.Join(baseDir, "data"), RootDir: rootDir})
		parent := &PageNode{ID: newFixturePageID("root"), Slug: newFixtureSlug("root"), Title: "root", Kind: NodeKindSection}
		entry := &PageNode{ID: newFixturePageID("docs"), Slug: newFixtureSlug("docs"), Title: "Docs", Kind: NodeKindPage, Parent: parent}
		{
			err := store.CreatePage(parent, entry)
			Expect(err).To(Succeed(), "CreatePage failed: %v",

				err)
		}

		err := store.RenameNode(entry, newFixtureSlug("../outside"))
		Expect(err).To(MatchError(ErrInvalidOperation), "expected ErrInvalidOperation, got %v",

			err)

		statTreePath(filepath.Join(rootDir, "docs.md"))
		Expect(filepath.Join(baseDir, "outside.md")).To(beMissingTreePath())

	})
})

var _ = ginkgo.Describe("node store persistence", ginkgo.Label("unit"), func() {
	ginkgo.It("upsert content rejects parentless non root page", func() {
		baseDir := tempTreeDir()
		rootDir := filepath.Join(baseDir, "content")
		store := NewNodeStoreWithOptions(NodeStoreOptions{DataDir: filepath.Join(baseDir, "data"), RootDir: rootDir})
		entry := &PageNode{ID: newFixturePageID("loose"), Slug: newFixtureSlug("loose"), Title: "Loose", Kind: NodeKindPage}

		err := store.UpsertContent(entry, "# Loose")
		Expect(err).To(MatchError(ErrInvalidOperation), "expected ErrInvalidOperation, got %v",

			err)

		Expect(rootDir + ".md").To(beMissingTreePath())

	})
})

var _ = ginkgo.Describe("node store persistence", ginkgo.Label("unit"), func() {
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
		root := &PageNode{ID: newFixturePageID("root"), Slug: newFixtureSlug("root"), Title: "root", Kind: NodeKindSection}
		docs := &PageNode{ID: newFixturePageID("docs"), Slug: newFixtureSlug("docs"), Title: "Docs", Kind: NodeKindSection, Parent: root}
		child := &PageNode{ID: newFixturePageID("child"), Slug: newFixtureSlug("child"), Title: "Child", Kind: NodeKindPage, Parent: docs}

		err := store.CreatePage(docs, child)
		Expect(err).To(MatchError(ErrInvalidOperation), "expected ErrInvalidOperation, got %v",

			err)

		Expect(filepath.Join(outsideDir, "child.md")).To(beMissingTreePath())

	})
})

var _ = ginkgo.Describe("node store persistence", ginkgo.Label("unit"), func() {
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
		root := &PageNode{ID: newFixturePageID("root"), Slug: newFixtureSlug("root"), Title: "root", Kind: NodeKindSection}
		docs := &PageNode{ID: newFixturePageID("docs"), Slug: newFixtureSlug("docs"), Title: "Docs", Kind: NodeKindSection, Parent: root}
		child := &PageNode{ID: newFixturePageID("child"), Slug: newFixtureSlug("child"), Title: "Child", Kind: NodeKindPage, Parent: docs}

		err := store.UpsertContent(child, "# Child")
		Expect(err).To(MatchError(ErrInvalidOperation), "expected ErrInvalidOperation, got %v",

			err)

		Expect(filepath.Join(outsideDir, "child.md")).To(beMissingTreePath())

	})
})

var _ = ginkgo.Describe("node store persistence", ginkgo.Label("unit"), func() {
	ginkgo.It("save child order root writes order file", func() {
		tmp := tempTreeDir()
		store := NewNodeStore(tmp)

		root := &PageNode{
			ID:    newFixturePageID("root"),
			Slug:  newFixtureSlug("root"),
			Title: "root",
			Kind:  NodeKindSection,
			Children: []*PageNode{
				{ID: newFixturePageID("a")},
				{ID: newFixturePageID("b")},
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

var _ = ginkgo.Describe("node store persistence", ginkgo.Label("unit"), func() {
	ginkgo.It("save child order page returns an error without creating directory", func() {
		tmp := tempTreeDir()
		store := NewNodeStore(tmp)

		root := &PageNode{ID: newFixturePageID("root"), Slug: newFixtureSlug("root"), Title: "root", Kind: NodeKindSection}
		page := &PageNode{ID: newFixturePageID("page1"), Slug: newFixtureSlug("docs"), Title: "Docs", Kind: NodeKindPage, Parent: root}

		err := store.SaveChildOrder(page)

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

var _ = ginkgo.Describe("node store persistence", ginkgo.Label("unit"), func() {
	ginkgo.It("create section creates folder and index with frontmatter", func() {
		tmp := tempTreeDir()
		store := NewNodeStore(tmp)

		root := &PageNode{ID: newFixturePageID("root"), Slug: newFixtureSlug("root"), Title: "root", Kind: NodeKindSection}
		sec := &PageNode{
			ID:     newFixturePageID("sec1"),
			Slug:   newFixtureSlug("docs"),
			Title:  "Docs",
			Kind:   NodeKindSection,
			Parent: root,
			Metadata: PageMetadata{
				CreatedAt:    time.Date(2026, time.March, 22, 10, 15, 30, 0, time.UTC),
				UpdatedAt:    time.Date(2026, time.March, 22, 11, 16, 31, 0, time.UTC),
				CreatorID:    newFixtureUserID("alice"),
				LastAuthorID: newFixtureUserID("bob"),
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
		frontmatter, body, err := parseRequiredFrontmatter(raw)
		Expect(err).To(Succeed(), "ParseFrontmatter: %v", err)
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

var _ = ginkgo.Describe("node store persistence", ginkgo.Label("unit"), func() {
	ginkgo.It("create section kind guards", func() {
		tmp := tempTreeDir()
		store := NewNodeStore(tmp)

		rootPageWrong := &PageNode{ID: newFixturePageID("root"), Slug: newFixtureSlug("root"), Title: "root", Kind: NodeKindPage}
		sec := &PageNode{ID: newFixturePageID("sec1"), Slug: newFixtureSlug("docs"), Title: "Docs", Kind: NodeKindSection}
		{

			err := store.CreateSection(rootPageWrong, sec)
			Expect(err).To(MatchError(ErrInvalidOperation), "expected parent section validation error, got %v", err)
		}

		root := &PageNode{ID: newFixturePageID("root"), Slug: newFixtureSlug("root"), Title: "root", Kind: NodeKindSection}
		pageWrong := &PageNode{ID: newFixturePageID("x"), Slug: newFixtureSlug("x"), Title: "X", Kind: NodeKindPage}
		{
			err := store.CreateSection(root, pageWrong)
			Expect(err).To(MatchError(ErrInvalidOperation), "expected section entry validation error, got %v", err)
		}

	})
})

var _ = ginkgo.Describe("node store persistence", ginkgo.Label("unit"), func() {
	ginkgo.It("create page creates markdown with frontmatter", func() {
		tmp := tempTreeDir()
		store := NewNodeStore(tmp)

		root := &PageNode{ID: newFixturePageID("root"), Slug: newFixtureSlug("root"), Title: "root", Kind: NodeKindSection}
		page := &PageNode{ID: newFixturePageID("p1"), Slug: newFixtureSlug("hello"), Title: "Hello World", Kind: NodeKindPage, Parent: root}
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

		frontmatter, body, err := parseRequiredFrontmatter(string(raw))
		Expect(err).To(Succeed(), "ParseFrontmatter: %v", err)
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
