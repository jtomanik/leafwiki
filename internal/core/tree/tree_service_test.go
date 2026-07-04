package tree

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/perber/wiki/internal/core/treemigration"

	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/types"
)

// Canonical Markdown links plan scenarios covered by tests in this file:
// - New section creates index.md
// Plantrace evidence: TestTreeService_CreateNode_Section_CreatesIndexWithFrontmatter.

// --- helpers ---

func newLoadedService() (*TreeService, string) {
	ginkgo.GinkgoHelper()
	tmpDir := tempTreeDir()

	// Ensure schema is current so LoadTree doesn't try to migrate unless a test wants it.
	Expect(saveSchema(tmpDir, CurrentSchemaVersion)).To(Succeed())

	svc := NewTreeService(tmpDir)
	Expect(svc.LoadTree()).To(Succeed())
	return svc, tmpDir
}

func newLoadedServiceWithDirs() (*TreeService, string, string) {
	ginkgo.GinkgoHelper()
	dataDir := filepath.Join(tempTreeDir(), "data")
	rootDir := filepath.Join(tempTreeDir(), "content")

	Expect(os.MkdirAll(dataDir, 0o755)).To(Succeed())
	Expect(saveSchema(dataDir, CurrentSchemaVersion)).To(Succeed())

	svc := NewTreeServiceWithOptions(TreeOptions{
		DataDir: dataDir,
		RootDir: rootDir,
	})
	Expect(svc.LoadTree()).To(Succeed())
	return svc, dataDir, rootDir
}

func statTreePath(path string) os.FileInfo {
	ginkgo.GinkgoHelper()
	info, err := os.Stat(path)
	Expect(err).NotTo(HaveOccurred())
	return info
}

func beMissingTreePath() types.GomegaMatcher {
	return WithTransform(func(path string) error {
		_, err := os.Stat(path)
		return err
	}, MatchError(os.ErrNotExist))
}

func writeTreeTestFile(filePath string, content string) {
	ginkgo.GinkgoHelper()
	Expect(os.MkdirAll(filepath.Dir(filePath), 0o755)).To(Succeed())
	Expect(os.WriteFile(filePath, []byte(content), 0o644)).To(Succeed())
}

func testPageIDs(ids ...PageID) []PageID {
	return ids
}

func persistLegacyTreeSnapshot(storageDir string, tree *PageNode) {
	ginkgo.GinkgoHelper()
	raw, err := json.Marshal(tree)
	Expect(err).NotTo(HaveOccurred())
	Expect(os.WriteFile(filepath.Join(storageDir, legacyTreeFilename), raw, 0o644)).To(Succeed())
}

func readOrderIDs(directory string) []string {
	ginkgo.GinkgoHelper()
	raw, err := os.ReadFile(filepath.Join(directory, ".order.json"))
	Expect(err).NotTo(HaveOccurred())
	var persisted struct {
		OrderedIDs []string `json:"ordered_ids"`
	}
	Expect(json.Unmarshal(raw, &persisted)).To(Succeed())
	return persisted.OrderedIDs
}

func matchPersistedPageIDOrder(want ...PageID) types.GomegaMatcher {
	return WithTransform(func(got []string) []PageID {
		typed := make([]PageID, 0, len(got))
		for _, rawID := range got {
			typed = append(typed, PageIDFromString(rawID))
		}
		return typed
	}, Equal(want))
}

func matchPageIDOrder(want ...PageID) types.GomegaMatcher {
	return Equal(want)
}

// --- A) Load/Save basics ---

var _ = ginkgo.Describe("tree service behavior", ginkgo.Label("unit"), func() {
	ginkgo.It("load tree default root when missing", func() {
		tmpDir := tempTreeDir()
		{

			// schema current to prevent migration from failing due to missing schema file
			err := saveSchema(tmpDir, CurrentSchemaVersion)
			Expect(err).To(Succeed(), "saveSchema failed: %v",

				err)
		}

		svc := NewTreeService(tmpDir)
		{
			err := svc.LoadTree()
			Expect(err).To(Succeed(), "LoadTree failed: %v",

				err)
		}

		tree := svc.GetTree()
		Expect(tree).To(matchRootSection(), "expected default root, got: %+v", tree)
		Expect(tree.Kind).To(Equal(NodeKindSection),
			"expected root to be section, got %q",

			tree.Kind,
		)

	})
})

var _ = ginkgo.Describe("tree service construction with explicit roots", ginkgo.Label("unit"), func() {
	ginkgo.It("uses separate root directory for content and data directory for schema", func() {
		svc, dataDir, rootDir := newLoadedServiceWithDirs()

		pageID, err := svc.CreateNode("system", nil, "Page", "page", ptrKind(NodeKindPage))
		Expect(err).To(Succeed(), "CreateNode page failed: %v",

			err)

		sectionID, err := svc.CreateNode("system", nil, "Section", "section", ptrKind(NodeKindSection))
		Expect(err).To(Succeed(), "CreateNode section failed: %v",

			err,
		)
		{

			_, err := svc.CreateNode("system", sectionID, "Nested", "nested", ptrKind(NodeKindPage))
			Expect(err).To(Succeed(), "CreateNode nested failed: %v",

				err)
		}

		statTreePath(filepath.Join(rootDir, "page.md"))
		statTreePath(filepath.Join(rootDir, "section", "index.md"))
		statTreePath(filepath.Join(rootDir, "section", "nested.md"))
		statTreePath(filepath.Join(rootDir, ".order.json"))
		statTreePath(filepath.Join(dataDir, "schema.json"))
		Expect(filepath.Join(rootDir, "root")).To(beMissingTreePath())
		Expect(filepath.Join(dataDir, "root", "page.md")).To(beMissingTreePath())

		loaded := NewTreeServiceWithOptions(TreeOptions{DataDir: dataDir, RootDir: rootDir})
		{
			err := loaded.LoadTree()
			Expect(err).To(Succeed(), "LoadTree with explicit root failed: %v",

				err)
		}
		{

			_, err := loaded.GetPage(*pageID)
			Expect(err).To(Succeed(), "loaded page from explicit root: %v",

				err)
		}

	})
})

var _ = ginkgo.Describe("tree service configured root loading", ginkgo.Label("unit"), func() {
	ginkgo.It("migrates legacy tree from data directory into root directory", func() {
		dataDir := filepath.Join(tempTreeDir(), "data")
		rootDir := filepath.Join(tempTreeDir(), "content")
		{
			err := os.MkdirAll(dataDir, 0o755)
			Expect(err).To(Succeed(), "mkdir data directory: %v",

				err)
		}

		writeTreeFile(filepath.Join(rootDir, "legacy.md"), `---
leafwiki_id: id-legacy
leafwiki_title: Legacy
---
# Legacy`, 0o644)
		persistLegacyTreeSnapshot(dataDir, &PageNode{
			ID:       "root",
			Slug:     "root",
			Title:    "root",
			Kind:     NodeKindSection,
			Children: []*PageNode{{ID: "id-legacy", Slug: "legacy", Title: "Legacy", Kind: NodeKindPage}},
		})
		{
			err := saveSchema(dataDir, 4)
			Expect(err).To(Succeed(), "saveSchema failed: %v",

				err)
		}

		svc := NewTreeServiceWithOptions(TreeOptions{DataDir: dataDir, RootDir: rootDir})
		{
			err := svc.LoadTree()
			Expect(err).To(Succeed(), "LoadTree failed: %v",

				err)
		}

		statTreePath(filepath.Join(rootDir, ".order.json"))
		statTreePath(filepath.Join(dataDir, "schema.json"))
		Expect(filepath.Join(dataDir, legacyTreeFilename)).To(beMissingTreePath())
		{
			_, err := svc.GetPage("id-legacy")
			Expect(err).To(Succeed(), "expected migrated page from root directory: %v",

				err)
		}

	})
})

var _ = ginkgo.Describe("tree service configured root loading", ginkgo.Label("unit"), func() {
	ginkgo.It("allows legacy section without index when page content moved", func() {
		dataDir := filepath.Join(tempTreeDir(), "data")
		rootDir := filepath.Join(tempTreeDir(), "content")
		{
			err := os.MkdirAll(dataDir, 0o755)
			Expect(err).To(Succeed(), "mkdir data directory: %v",

				err)
		}

		writeTreeFile(filepath.Join(dataDir, "root", "docs", "legacy.md"), `---
leafwiki_id: id-legacy
leafwiki_title: Legacy
---
# Legacy`, 0o644)
		writeTreeFile(filepath.Join(rootDir, "docs", "legacy.md"), `---
leafwiki_id: id-legacy
leafwiki_title: Legacy
---
# Legacy`, 0o644)
		persistLegacyTreeSnapshot(dataDir, &PageNode{
			ID:    "root",
			Slug:  "root",
			Title: "root",
			Kind:  NodeKindSection,
			Children: []*PageNode{{
				ID:    "id-docs",
				Slug:  "docs",
				Title: "Docs",
				Kind:  NodeKindSection,
				Children: []*PageNode{{
					ID:    "id-legacy",
					Slug:  "legacy",
					Title: "Legacy",
					Kind:  NodeKindPage,
				}},
			}},
		})
		{
			err := saveSchema(dataDir, 4)
			Expect(err).To(Succeed(), "saveSchema failed: %v",

				err)
		}

		svc := NewTreeServiceWithOptions(TreeOptions{DataDir: dataDir, RootDir: rootDir})
		{
			err := svc.LoadTree()
			Expect(err).To(Succeed(), "LoadTree failed: %v",

				err)
		}
		{

			_, err := svc.GetPage("id-legacy")
			Expect(err).To(Succeed(), "expected migrated page from moved section content: %v",

				err,
			)
		}

	})
})

var _ = ginkgo.Describe("tree service configured root loading", ginkgo.Label("unit"), func() {
	ginkgo.It("fails safely when current schema content still in default root", func() {
		dataDir := filepath.Join(tempTreeDir(), "data")
		rootDir := filepath.Join(tempTreeDir(), "content")
		{
			err := os.MkdirAll(dataDir, 0o755)
			Expect(err).To(Succeed(), "mkdir data directory: %v",

				err)
		}

		writeTreeFile(filepath.Join(dataDir, "root", "current.md"), `---
leafwiki_id: id-current
leafwiki_title: Current
---
# Current`, 0o644)
		{
			err := saveSchema(dataDir, CurrentSchemaVersion)
			Expect(err).To(Succeed(), "saveSchema failed: %v",

				err)
		}

		svc := NewTreeServiceWithOptions(TreeOptions{DataDir: dataDir, RootDir: rootDir})
		err := svc.LoadTree()
		Expect(err).To(MatchError(ErrLegacyContentRemains), "expected legacy content remains error, got: %v",

			err)

		statTreePath(filepath.Join(dataDir, "schema.json"))
		statTreePath(filepath.Join(dataDir, "root", "current.md"))
		Expect(filepath.Join(rootDir, "current.md")).To(beMissingTreePath())

	})
})

var _ = ginkgo.Describe("tree service configured root loading", ginkgo.Label("unit"), func() {
	ginkgo.It("fails safely when legacy content still in default root", func() {
		dataDir := filepath.Join(tempTreeDir(), "data")
		rootDir := filepath.Join(tempTreeDir(), "content")
		{
			err := os.MkdirAll(dataDir, 0o755)
			Expect(err).To(Succeed(), "mkdir data directory: %v",

				err)
		}

		writeTreeFile(filepath.Join(dataDir, "root", "legacy.md"), `---
leafwiki_id: id-legacy
leafwiki_title: Legacy
---
# Legacy`, 0o644)
		persistLegacyTreeSnapshot(dataDir, &PageNode{
			ID:       "root",
			Slug:     "root",
			Title:    "root",
			Kind:     NodeKindSection,
			Children: []*PageNode{{ID: "id-legacy", Slug: "legacy", Title: "Legacy", Kind: NodeKindPage}},
		})
		{
			err := saveSchema(dataDir, 4)
			Expect(err).To(Succeed(), "saveSchema failed: %v",

				err)
		}

		svc := NewTreeServiceWithOptions(TreeOptions{DataDir: dataDir, RootDir: rootDir})
		err := svc.LoadTree()
		Expect(err).To(MatchError(ErrLegacyContentRemains), "expected legacy content remains error, got: %v",

			err)

		statTreePath(filepath.Join(dataDir, legacyTreeFilename))
		statTreePath(filepath.Join(dataDir, "root", "legacy.md"))
		Expect(filepath.Join(rootDir, "legacy.md")).To(beMissingTreePath())

	})
})

var _ = ginkgo.Describe("tree service configured root loading", ginkgo.Label("unit"), func() {
	ginkgo.It("fails safely when legacy tree snapshot is corrupt and default root has content", func() {
		dataDir := filepath.Join(tempTreeDir(), "data")
		rootDir := filepath.Join(tempTreeDir(), "content")
		{
			err := os.MkdirAll(dataDir, 0o755)
			Expect(err).To(Succeed(), "mkdir data directory: %v",

				err)
		}

		writeTreeFile(filepath.Join(dataDir, "root", "legacy.md"), `---
leafwiki_id: id-legacy
leafwiki_title: Legacy
---
# Legacy`, 0o644)
		writeTreeFile(filepath.Join(dataDir, legacyTreeFilename), "{", 0o644)
		{
			err := saveSchema(dataDir, 4)
			Expect(err).To(Succeed(), "saveSchema failed: %v",

				err)
		}

		svc := NewTreeServiceWithOptions(TreeOptions{DataDir: dataDir, RootDir: rootDir})
		err := svc.LoadTree()
		Expect(err).To(MatchError(ErrLegacyContentRemains), "expected legacy content remains error, got: %v",

			err)

		statTreePath(filepath.Join(dataDir, legacyTreeFilename))
		statTreePath(filepath.Join(dataDir, "root", "legacy.md"))
		Expect(filepath.Join(rootDir, "legacy.md")).To(beMissingTreePath())

	})
})

var _ = ginkgo.Describe("tree service configured root loading", ginkgo.Label("unit"), func() {
	ginkgo.It("fails safely when configured root has unrelated content", func() {
		dataDir := filepath.Join(tempTreeDir(), "data")
		rootDir := filepath.Join(tempTreeDir(), "content")
		{
			err := os.MkdirAll(dataDir, 0o755)
			Expect(err).To(Succeed(), "mkdir data directory: %v",

				err)
		}

		writeTreeFile(filepath.Join(dataDir, "root", "legacy.md"), `---
leafwiki_id: id-legacy
leafwiki_title: Legacy
---
# Legacy`, 0o644)
		writeTreeFile(filepath.Join(rootDir, "unrelated.md"), "# Unrelated", 0o644)
		persistLegacyTreeSnapshot(dataDir, &PageNode{
			ID:       "root",
			Slug:     "root",
			Title:    "root",
			Kind:     NodeKindSection,
			Children: []*PageNode{{ID: "id-legacy", Slug: "legacy", Title: "Legacy", Kind: NodeKindPage}},
		})
		{
			err := saveSchema(dataDir, 4)
			Expect(err).To(Succeed(), "saveSchema failed: %v",

				err)
		}

		svc := NewTreeServiceWithOptions(TreeOptions{DataDir: dataDir, RootDir: rootDir})
		err := svc.LoadTree()
		Expect(err).To(MatchError(ErrLegacyContentRemains), "expected legacy content remains error, got: %v",

			err)

		statTreePath(filepath.Join(dataDir, legacyTreeFilename))
		statTreePath(filepath.Join(dataDir, "root", "legacy.md"))
		statTreePath(filepath.Join(rootDir, "unrelated.md"))
		Expect(filepath.Join(rootDir, "legacy.md")).To(beMissingTreePath())

	})
})

var _ = ginkgo.Describe("tree service configured root loading", ginkgo.Label("unit"), func() {
	ginkgo.It("fails safely when legacy default root has extra file", func() {
		dataDir := filepath.Join(tempTreeDir(), "data")
		rootDir := filepath.Join(tempTreeDir(), "content")
		{
			err := os.MkdirAll(dataDir, 0o755)
			Expect(err).To(Succeed(), "mkdir data directory: %v",

				err)
		}

		writeTreeFile(filepath.Join(dataDir, "root", "legacy.md"), `---
leafwiki_id: id-legacy
leafwiki_title: Legacy
---
# Legacy`, 0o644)
		writeTreeFile(filepath.Join(rootDir, "legacy.md"), `---
leafwiki_id: id-legacy
leafwiki_title: Legacy
---
# Legacy`, 0o644)
		writeTreeFile(filepath.Join(dataDir, "root", "orphan.md"), `---
leafwiki_id: id-orphan
leafwiki_title: Orphan
---
# Orphan`, 0o644)
		persistLegacyTreeSnapshot(dataDir, &PageNode{
			ID:       "root",
			Slug:     "root",
			Title:    "root",
			Kind:     NodeKindSection,
			Children: []*PageNode{{ID: "id-legacy", Slug: "legacy", Title: "Legacy", Kind: NodeKindPage}},
		})
		{
			err := saveSchema(dataDir, 4)
			Expect(err).To(Succeed(), "saveSchema failed: %v",

				err)
		}

		svc := NewTreeServiceWithOptions(TreeOptions{DataDir: dataDir, RootDir: rootDir})
		err := svc.LoadTree()
		Expect(err).To(MatchError(ErrLegacyContentRemains), "expected legacy content remains error, got: %v",

			err)

		statTreePath(filepath.Join(dataDir, legacyTreeFilename))
		statTreePath(filepath.Join(dataDir, "root", "legacy.md"))
		statTreePath(filepath.Join(dataDir, "root", "orphan.md"))
		statTreePath(filepath.Join(rootDir, "legacy.md"))
		Expect(filepath.Join(rootDir, "orphan.md")).To(beMissingTreePath())

	})
})

var _ = ginkgo.Describe("tree service configured root loading", ginkgo.Label("unit"), func() {
	ginkgo.It("fails safely when configured root has same path different legacy identity", func() {
		dataDir := filepath.Join(tempTreeDir(), "data")
		rootDir := filepath.Join(tempTreeDir(), "content")
		{
			err := os.MkdirAll(dataDir, 0o755)
			Expect(err).To(Succeed(), "mkdir data directory: %v",

				err)
		}

		writeTreeFile(filepath.Join(dataDir, "root", "legacy.md"), `---
leafwiki_id: id-legacy
leafwiki_title: Legacy
---
# Legacy`, 0o644)
		writeTreeFile(filepath.Join(rootDir, "legacy.md"), `---
leafwiki_id: id-other
leafwiki_title: Other
---
# Other`, 0o644)
		persistLegacyTreeSnapshot(dataDir, &PageNode{
			ID:       "root",
			Slug:     "root",
			Title:    "root",
			Kind:     NodeKindSection,
			Children: []*PageNode{{ID: "id-legacy", Slug: "legacy", Title: "Legacy", Kind: NodeKindPage}},
		})
		{
			err := saveSchema(dataDir, 4)
			Expect(err).To(Succeed(), "saveSchema failed: %v",

				err)
		}

		svc := NewTreeServiceWithOptions(TreeOptions{DataDir: dataDir, RootDir: rootDir})
		err := svc.LoadTree()
		Expect(err).To(MatchError(ErrLegacyContentRemains), "expected legacy content remains error, got: %v",

			err)

		statTreePath(filepath.Join(dataDir, legacyTreeFilename))
		statTreePath(filepath.Join(dataDir, "root", "legacy.md"))
		statTreePath(filepath.Join(rootDir, "legacy.md"))

	})
})

var _ = ginkgo.Describe("tree service configured root loading", ginkgo.Label("unit"), func() {
	ginkgo.It("fails safely when configured root has same identity but different content", func() {
		dataDir := filepath.Join(tempTreeDir(), "data")
		rootDir := filepath.Join(tempTreeDir(), "content")
		{
			err := os.MkdirAll(dataDir, 0o755)
			Expect(err).To(Succeed(), "mkdir data directory: %v",

				err)
		}

		writeTreeFile(filepath.Join(dataDir, "root", "legacy.md"), `---
leafwiki_id: id-legacy
leafwiki_title: Legacy
---
# Legacy

new content`, 0o644)
		writeTreeFile(filepath.Join(rootDir, "legacy.md"), `---
leafwiki_id: id-legacy
leafwiki_title: Legacy
---
# Legacy

old content`, 0o644)
		persistLegacyTreeSnapshot(dataDir, &PageNode{
			ID:       "root",
			Slug:     "root",
			Title:    "root",
			Kind:     NodeKindSection,
			Children: []*PageNode{{ID: "id-legacy", Slug: "legacy", Title: "Legacy", Kind: NodeKindPage}},
		})
		{
			err := saveSchema(dataDir, 4)
			Expect(err).To(Succeed(), "saveSchema failed: %v",

				err)
		}

		svc := NewTreeServiceWithOptions(TreeOptions{DataDir: dataDir, RootDir: rootDir})
		err := svc.LoadTree()
		Expect(err).To(MatchError(ErrLegacyContentRemains), "expected legacy content remains error, got: %v",

			err)

		statTreePath(filepath.Join(dataDir, legacyTreeFilename))
		statTreePath(filepath.Join(dataDir, "root", "legacy.md"))
		statTreePath(filepath.Join(rootDir, "legacy.md"))

	})
})

var _ = ginkgo.Describe("tree service behavior", ginkgo.Label("unit"), func() {
	ginkgo.It("load tree migrates legacy tree order into order files", func() {
		tmpDir := tempTreeDir()

		writeTreeFile(filepath.Join(tmpDir, "root", "a.md"), `---
leafwiki_id: id-a
leafwiki_title: A
---
# A`, 0o644)
		writeTreeFile(filepath.Join(tmpDir, "root", "b.md"), `---
leafwiki_id: id-b
leafwiki_title: B
---
# B`, 0o644)
		writeTreeFile(filepath.Join(tmpDir, "root", "c.md"), `---
leafwiki_id: id-c
leafwiki_title: C
---
# C`, 0o644)

		legacyTree := &PageNode{
			ID:       "root",
			Slug:     "root",
			Title:    "root",
			Kind:     NodeKindSection,
			Children: []*PageNode{{ID: "id-c", Slug: "c", Title: "C", Kind: NodeKindPage, Position: 0}, {ID: "id-a", Slug: "a", Title: "A", Kind: NodeKindPage, Position: 1}, {ID: "id-b", Slug: "b", Title: "B", Kind: NodeKindPage, Position: 2}},
		}
		persistLegacyTreeSnapshot(tmpDir, legacyTree)
		{
			err := saveSchema(tmpDir, 4)
			Expect(err).To(Succeed(), "saveSchema failed: %v",

				err)
		}

		svc := NewTreeService(tmpDir)
		{
			err := svc.LoadTree()
			Expect(err).To(Succeed(), "LoadTree failed: %v",

				err)
		}

		root := svc.GetTree()
		{
			got, want := slugs(root.Children), []string{"c", "a", "b"}
			Expect(strings.Join(
				got, ",")).
				To(Equal(strings.
					Join(want, ",")), "unexpected child order after legacy migration: got %v want %v",

					got, want,
				)
		}
		{

			got, want := readOrderIDs(filepath.Join(tmpDir, "root")), []string{"id-c", "id-a", "id-b"}
			Expect(strings.Join(
				got, ",")).
				To(Equal(strings.
					Join(want, ",")), "unexpected persisted order after legacy migration: got %v want %v",

					got,
					want)
		}

		Expect(filepath.Join(tmpDir, legacyTreeFilename)).To(beMissingTreePath())

	})
})

var _ = ginkgo.Describe("tree service behavior", ginkgo.Label("unit"), func() {
	ginkgo.It("load tree removes legacy tree snapshot after successful migration", func() {
		tmpDir := tempTreeDir()

		writeTreeFile(filepath.Join(tmpDir, "root", "a.md"), `---
leafwiki_id: id-a
leafwiki_title: A
---
# A`, 0o644)
		writeTreeFile(filepath.Join(tmpDir, "root", "b.md"), `---
leafwiki_id: id-b
leafwiki_title: B
---
# B`, 0o644)

		legacyTree := &PageNode{
			ID:    "root",
			Slug:  "root",
			Title: "root",
			Kind:  NodeKindSection,
			Children: []*PageNode{
				{ID: "id-b", Slug: "b", Title: "B", Kind: NodeKindPage, Position: 0},
				{ID: "id-a", Slug: "a", Title: "A", Kind: NodeKindPage, Position: 1},
			},
		}
		persistLegacyTreeSnapshot(tmpDir, legacyTree)
		{
			err := saveSchema(tmpDir, 4)
			Expect(err).To(Succeed(), "saveSchema failed: %v",

				err)
		}

		svc := NewTreeService(tmpDir)
		{
			err := svc.LoadTree()
			Expect(err).To(Succeed(), "LoadTree failed: %v",

				err)
		}

		Expect(filepath.Join(tmpDir, legacyTreeFilename)).To(beMissingTreePath())

	})
})

var _ = ginkgo.Describe("tree service behavior", ginkgo.Label("unit"), func() {
	ginkgo.It("save and load roundtrip parents", func() {
		svc, tmpDir := newLoadedService()

		// Create a small tree through public API (exercises disk + tree)
		idA, err := svc.CreateNode("system", nil, "A", "a", ptrKind(NodeKindPage))
		Expect(err).To(Succeed(), "CreateNode A failed: %v",

			err)

		_, err = svc.CreateNode("system", idA, "B", "b", ptrKind(NodeKindPage))
		Expect(err).To(Succeed(), "CreateNode B failed: %v",

			err)
		{

			// Reload in a new service instance
			err := saveSchema(tmpDir, CurrentSchemaVersion)
			Expect(err).To(Succeed(), "saveSchema failed: %v",

				err)
		}

		loaded := NewTreeService(tmpDir)
		{
			err := loaded.LoadTree()
			Expect(err).To(Succeed(), "LoadTree failed: %v",

				err)
		}

		root := loaded.GetTree()
		Expect(root.Children).To(HaveLen(1),
			"expected 1 child at root, got %d",

			len(root.
				Children,
			),
		)

		a := root.Children[0]
		Expect(a).To(SatisfyAll(
			HaveField("Parent", haveParentPageID(RootPageID)),
			HaveField("Children", HaveLen(1)),
		), "expected A to be attached under root with one child")

		b := a.Children[0]
		Expect(b.Parent).To(haveParentPageID(a.ID), "expected parent pointer on B")

	})
})

var _ = ginkgo.Describe("tree service behavior", ginkgo.Label("unit"), func() {
	ginkgo.It("tree hash is stable across repeated calls", func() {
		svc, _ := newLoadedService()

		h1 := svc.TreeHash()
		h2 := svc.TreeHash()
		Expect(h1).NotTo(BeEmpty(),

			"expected non-empty hash")
		Expect(h1).To(Equal(
			h2), "expected stable hash across repeated calls, got %q and %q",

			h1,
			h2,
		)
		{

			want := svc.GetTree().Hash()
			Expect(h1).To(Equal(
				want), "expected TreeHash to match underlying tree hash, got %q want %q",

				h1, want)
		}

	})
})

var _ = ginkgo.Describe("tree service behavior", ginkgo.Label("unit"), func() {
	ginkgo.It("tree hash changes when tree changes", func() {
		svc, _ := newLoadedService()

		before := svc.TreeHash()
		pageID, err := svc.CreateNode("system", nil, "Welcome", "welcome", ptrKind(NodeKindPage))
		Expect(err).To(Succeed(), "CreateNode failed: %v",

			err)

		afterCreate := svc.TreeHash()
		Expect(before).NotTo(Equal(afterCreate), "expected hash to change after create")
		{

			err := svc.UpdateNode(newFixtureUserID("system"), *pageID, "Welcome 2", Slug("welcome"), nil, pageVersionUnchecked, false)
			Expect(err).To(Succeed(), "UpdateNode failed: %v",

				err)
		}

		afterUpdate := svc.TreeHash()
		Expect(afterCreate).
			NotTo(Equal(afterUpdate), "expected hash to change after update")

	})
})

var _ = ginkgo.Describe("tree service behavior", ginkgo.Label("unit"), func() {
	ginkgo.It("tree hash changes when order changes", func() {
		svc, _ := newLoadedService()

		firstID, err := svc.CreateNode("system", nil, "One", "one", ptrKind(NodeKindPage))
		Expect(err).To(Succeed(), "CreateNode first failed: %v",

			err)

		secondID, err := svc.CreateNode("system", nil, "Two", "two", ptrKind(NodeKindPage))
		Expect(err).To(Succeed(), "CreateNode second failed: %v",

			err)

		before := svc.TreeHash()
		{
			err := svc.SortPages("", testPageIDs(*secondID, *firstID))
			Expect(err).To(Succeed(), "SortPages failed: %v",

				err)
		}

		after := svc.TreeHash()
		Expect(before).NotTo(Equal(after), "expected hash to change after sort")

	})
})

// --- B) Create/Update/Delete disk sync ---

var _ = ginkgo.Describe("tree service behavior", ginkgo.Label("unit"), func() {
	ginkgo.It("create node reloads from filesystem", func() {
		svc, tmpDir := newLoadedService()

		id, err := svc.CreateNode("system", nil, "Welcome", "welcome", ptrKind(NodeKindPage))
		Expect(err).To(Succeed(), "CreateNode failed: %v",

			err)

		reloaded := NewTreeService(tmpDir)
		{
			err := reloaded.LoadTree()
			Expect(err).To(Succeed(), "LoadTree failed: %v",

				err)
		}

		root := reloaded.GetTree()
		Expect(root.Children).To(ConsistOf(HaveField("ID", Equal(*id))))

	})
})

var _ = ginkgo.Describe("tree service behavior", ginkgo.Label("unit"), func() {
	ginkgo.It("create child rolls back parent auto convert when tree save fails", func() {
		svc, tmpDir := newLoadedService()

		parentID, err := svc.CreateNode("system", nil, "Docs", "docs", ptrKind(NodeKindPage))
		Expect(err).To(Succeed(), "CreateNode parent failed: %v",

			err)

		statTreePath(filepath.Join(tmpDir, "root", "docs.md"))

		createTreeDirectory(filepath.Join(tmpDir, "root", "docs", ".order.json"))

		childID, err := svc.CreateNode("system", parentID, "Child", "child", ptrKind(NodeKindPage))
		Expect(err).To(MatchError(ErrPersistChildOrder), "expected child order persistence error, got: %v",

			err)

		Expect(childID).To(BeNil())

		root := svc.GetTree()
		Expect(root.Children).To(HaveLen(1),
			"expected only original parent after rollback, got %d root children",

			len(root.Children))

		parent := root.Children[0]
		Expect(parent).To(SatisfyAll(
			HaveField("Kind", Equal(NodeKindPage)),
			HaveField("Children", BeEmpty()),
		), "expected parent to roll back to a childless page, got %#v", parent)

		statTreePath(filepath.Join(tmpDir, "root", "docs.md"))
		Expect(filepath.Join(tmpDir, "root", "docs")).To(beMissingTreePath())
		Expect(filepath.Join(tmpDir, "root", "docs", "index.md")).To(beMissingTreePath())
		Expect(filepath.Join(tmpDir, "root", "docs", "child.md")).To(beMissingTreePath())

	})
})

var _ = ginkgo.Describe("tree service behavior", ginkgo.Label("unit"), func() {
	ginkgo.It("create node rolls back when tree save fails", func() {
		svc, tmpDir := newLoadedService()

		createTreeDirectory(filepath.Join(tmpDir, "root", ".order.json"))

		id, err := svc.CreateNode("system", nil, "Welcome", "welcome", ptrKind(NodeKindPage))
		Expect(err).To(MatchError(ErrPersistChildOrder), "expected child order persistence error, got: %v",

			err)

		Expect(id).To(BeNil())
		Expect(svc.GetTree().
			Children).
			To(HaveLen(0), "expected in-memory tree rollback, got %d root children",

				len(svc.GetTree().Children))

		Expect(filepath.Join(tmpDir, "root", "welcome.md")).To(beMissingTreePath())
		info, err := os.Stat(filepath.Join(tmpDir, "root", ".order.json"))
		Expect(err).To(Succeed())
		Expect(info).To(matchFileInfoKind(fileInfoDirectory))
		reloaded := NewTreeService(tmpDir)
		{
			err := reloaded.LoadTree()
			Expect(err).To(Succeed(), "LoadTree failed: %v",

				err)
		}
		Expect(reloaded.GetTree().Children).To(HaveLen(0), "expected no persisted children after rollback, got %d",

			len(reloaded.GetTree().Children),
		)

	})
})

var _ = ginkgo.Describe("tree service behavior", ginkgo.Label("unit"), func() {
	ginkgo.It("create node rolls back when order write fails", func() {
		svc, tmpDir := newLoadedService()

		createTreeDirectory(filepath.Join(tmpDir, "root", ".order.json"))

		id, err := svc.CreateNode("system", nil, "Welcome", "welcome", ptrKind(NodeKindPage))
		Expect(err).To(MatchError(ErrPersistChildOrder), "expected child order persistence error, got: %v",

			err)

		Expect(id).To(BeNil())
		Expect(svc.GetTree().
			Children).
			To(HaveLen(0), "expected in-memory tree rollback, got %d root children",

				len(svc.GetTree().Children))

		Expect(filepath.Join(tmpDir, "root", "welcome.md")).To(beMissingTreePath())

		reloaded := NewTreeService(tmpDir)
		{
			err := reloaded.LoadTree()
			Expect(err).To(Succeed(), "LoadTree failed: %v",

				err)
		}
		Expect(reloaded.GetTree().Children).To(HaveLen(0), "expected no persisted children after rollback, got %d",

			len(reloaded.GetTree().Children),
		)

	})
})

var _ = ginkgo.Describe("tree service behavior", ginkgo.Label("unit"), func() {
	ginkgo.It("create node page root creates file and frontmatter", func() {
		svc, tmpDir := newLoadedService()

		id, err := svc.CreateNode("system", nil, "Welcome", "welcome", ptrKind(NodeKindPage))
		Expect(err).To(Succeed(), "CreateNode failed: %v",

			err)

		// file path: <tmp>/root/welcome.md (based on your existing tests + GeneratePath convention)
		p := filepath.Join(tmpDir, "root", "welcome.md")
		statTreePath(p)

		raw, err := os.ReadFile(p)
		Expect(err).To(Succeed(), "read file: %v",

			err,
		)

		frontmatter, _, err := parseRequiredFrontmatter(string(raw))
		Expect(err).To(Succeed(), "ParseFrontmatter: %v", err)

		Expect(PageIDFromString(strings.TrimSpace(frontmatter.LeafWikiID))).To(Equal(*id))
		Expect(frontmatter).To(SatisfyAll(
			HaveField("LeafWikiCreatedAt", Not(BeEmpty())),
			HaveField("LeafWikiUpdatedAt", Not(BeEmpty())),
		), "expected leafwiki timestamps to be set, got %#v", frontmatter)
		Expect(frontmatter).To(matchFrontmatterAuthors(newFixtureUserID("system"), newFixtureUserID("system")),
			"expected creator metadata to be set, got %#v", frontmatter)

	})
})

var _ = ginkgo.Describe("tree service behavior", ginkgo.Label("unit"), func() {
	ginkgo.It("create node rejects case insensitive slug conflict", func() {
		svc, _ := newLoadedService()
		{

			_, err := svc.CreateNode("system", nil, "Alpha", "Alpha", ptrKind(NodeKindPage))
			Expect(err).To(Succeed(), "CreateNode alpha failed: %v",

				err)
		}
		{

			_, err := svc.CreateNode("system", nil, "Alpha Lower", "alpha", ptrKind(NodeKindPage))
			Expect(err).To(MatchError(ErrPageAlreadyExists), "expected ErrPageAlreadyExists for case-insensitive conflict, got %v",

				err)
		}

	})
})

var _ = ginkgo.Describe("tree service behavior", ginkgo.Label("unit"), func() {
	ginkgo.It("create node allows same basename page and section twins", func() {
		svc, dataDir := newLoadedService()

		pageID, err := svc.CreateNode("system", nil, "Sync Page", "sync", ptrKind(NodeKindPage))
		Expect(err).To(Succeed(), "CreateNode page failed: %v",

			err)

		sectionID, err := svc.CreateNode("system", nil, "Sync Section", "sync", ptrKind(NodeKindSection))
		Expect(err).To(Succeed(), "CreateNode section twin failed: %v",

			err)

		statTreePath(filepath.Join(dataDir, "root", "sync.md"))
		statTreePath(filepath.Join(dataDir, "root", "sync", "index.md"))

		page, err := svc.FindPageByRoutePathAndKind("sync", NodeKindPage)
		Expect(err).To(Succeed(), "FindPageByRoutePathAndKind page failed: %v",

			err,
		)

		Expect(page.ID).To(Equal(*pageID))

		section, err := svc.FindPageByRoutePathAndKind("sync", NodeKindSection)
		Expect(err).To(Succeed(), "FindPageByRoutePathAndKind section failed: %v",

			err)

		Expect(section.ID).To(Equal(*sectionID))
		{

			_, err := svc.CreateNode("system", nil, "Duplicate Page", "SYNC", ptrKind(NodeKindPage))
			Expect(err).To(MatchError(ErrPageAlreadyExists), "expected ErrPageAlreadyExists for same-kind duplicate, got %v",

				err)
		}

	})
})

var _ = ginkgo.Describe("tree service behavior", ginkgo.Label("unit"), func() {
	ginkgo.It("content path for node uses core read rules", func() {
		svc, dataDir := newLoadedService()

		sectionID, err := svc.CreateNode("system", nil, "Docs", "docs", ptrKind(NodeKindSection))
		Expect(err).To(Succeed(), "CreateNode section failed: %v",

			err,
		)

		pageID, err := svc.CreateNode("system", nil, "Guide", "guide", ptrKind(NodeKindPage))
		Expect(err).To(Succeed(), "CreateNode page failed: %v",

			err)
		{

			err := os.Rename(filepath.Join(dataDir, "root", "docs", "index.md"), filepath.Join(dataDir, "root", "docs", "INDEX.MD"))
			Expect(err).To(Succeed(), "rename section index: %v",

				err)
		}
		{

			err := os.WriteFile(filepath.Join(dataDir, "root", "docs", "README.md"), []byte("# README\n"), 0o644)
			Expect(err).To(Succeed(), "write README: %v",

				err)
		}

		section, err := svc.GetPage(*sectionID)
		Expect(err).To(Succeed(), "GetPage section failed: %v",

			err)

		sectionPath, err := svc.ContentPathForNode(section.PageNode)
		Expect(err).To(Succeed(), "ContentPathForNode section failed: %v",

			err)
		Expect(sectionPath).
			To(Equal("docs/INDEX.MD"), "section content path = %q, want docs/INDEX.MD",

				sectionPath)

		page, err := svc.GetPage(*pageID)
		Expect(err).To(Succeed(), "GetPage page failed: %v",

			err)

		pagePath, err := svc.ContentPathForNode(page.PageNode)
		Expect(err).To(Succeed(), "ContentPathForNode page failed: %v",

			err)
		Expect(pagePath).To(
			Equal("guide.md"),
			"page content path = %q, want guide.md",

			pagePath,
		)

	})
})

var _ = ginkgo.Describe("tree service behavior", ginkgo.Label("unit"), func() {
	ginkgo.It("create node rejects traversal slug", func() {
		svc, dataDir := newLoadedService()

		_, err := svc.CreateNode("system", nil, "Outside", "../outside", ptrKind(NodeKindPage))
		Expect(err).To(MatchError(ErrInvalidOperation), "expected ErrInvalidOperation, got %v",

			err,
		)

		Expect(filepath.Join(dataDir, "outside.md")).To(beMissingTreePath())

	})
})

var _ = ginkgo.Describe("tree service behavior", ginkgo.Label("unit"), func() {
	ginkgo.It("create node persists root order file", func() {
		svc, tmpDir := newLoadedService()

		idA, err := svc.CreateNode("system", nil, "A", "a", ptrKind(NodeKindPage))
		Expect(err).To(Succeed(), "CreateNode A failed: %v",

			err)

		idB, err := svc.CreateNode("system", nil, "B", "b", ptrKind(NodeKindPage))
		Expect(err).To(Succeed(), "CreateNode B failed: %v",

			err)

		Expect(readOrderIDs(filepath.Join(tmpDir, "root"))).To(matchPersistedPageIDOrder(*idA, *idB))

	})
})

// - New section creates index.md
var _ = ginkgo.Describe("tree service behavior", ginkgo.Label("unit"), func() {
	ginkgo.It("create node section creates index with frontmatter", func() {
		svc, tmpDir := newLoadedService()

		id, err := svc.CreateNode("system", nil, "Docs", "docs", ptrKind(NodeKindSection))
		Expect(err).To(Succeed(), "CreateNode failed: %v",

			err)

		index := filepath.Join(tmpDir, "root", "docs", "index.md")
		raw, err := os.ReadFile(index)
		Expect(err).To(Succeed(), "read file: %v",

			err,
		)

		frontmatter, body, err := parseRequiredFrontmatter(string(raw))
		Expect(err).To(Succeed(), "ParseFrontmatter: %v", err)

		Expect(PageIDFromString(strings.TrimSpace(frontmatter.LeafWikiID))).To(Equal(*id))
		Expect(frontmatter.LeafWikiTitle).To(
			Equal(
				"Docs"), "expected leafwiki_title Docs, got %q",

			frontmatter.LeafWikiTitle)
		Expect(frontmatter).To(SatisfyAll(
			HaveField("LeafWikiCreatedAt", Not(BeEmpty())),
			HaveField("LeafWikiUpdatedAt", Not(BeEmpty())),
		), "expected leafwiki timestamps to be set, got %#v", frontmatter)
		Expect(frontmatter).To(matchFrontmatterAuthors(newFixtureUserID("system"), newFixtureUserID("system")),
			"expected creator metadata to be set, got %#v", frontmatter)
		Expect(strings.TrimSpace(body)).To(BeEmpty(),

			"expected empty section body, got %q",

			body)

	})
})

var _ = ginkgo.Describe("tree service behavior", ginkgo.Label("unit"), func() {
	ginkgo.It("create child under page auto converts parent to section", func() {
		svc, tmpDir := newLoadedService()

		// Create parent as page
		parentID, err := svc.CreateNode("system", nil, "Docs", "docs", ptrKind(NodeKindPage))
		Expect(err).To(Succeed(), "Create parent failed: %v",

			err)

		// Should exist as file initially
		parentFile := filepath.Join(tmpDir, "root", "docs.md")
		statTreePath(parentFile)

		// Create child under parent: must convert parent to section
		_, err = svc.CreateNode("system", parentID, "Getting Started", "getting-started", ptrKind(NodeKindPage))
		Expect(err).To(Succeed(), "Create child failed: %v",

			err)

		// Parent should now be a folder with index.md (converted from docs.md)
		parentDir := filepath.Join(tmpDir, "root", "docs")
		statTreePath(parentDir)
		index := filepath.Join(parentDir, "index.md")
		statTreePath(index)

		// Old file should be gone
		Expect(parentFile).To(beMissingTreePath())

		// Child file should be inside folder
		childFile := filepath.Join(parentDir, "getting-started.md")
		statTreePath(childFile)

		// Tree kind updated
		parentNode, err := svc.FindPageByID(*parentID)
		Expect(err).To(Succeed(), "FindPageByID: %v",

			err)
		Expect(parentNode.Kind).To(Equal(NodeKindSection), "expected parent kind section, got %q",

			parentNode.Kind)

	})
})

var _ = ginkgo.Describe("tree service behavior", ginkgo.Label("unit"), func() {
	ginkgo.It("update node title only syncs frontmatter if file exists", func() {
		svc, tmpDir := newLoadedService()

		id, err := svc.CreateNode("system", nil, "Docs", "docs", ptrKind(NodeKindPage))
		Expect(err).To(Succeed(), "CreateNode failed: %v",

			err)

		p := filepath.Join(tmpDir, "root", "docs.md")
		statTreePath(p)
		{

			// Update title only: content=nil, slug unchanged
			err := svc.UpdateNode(newFixtureUserID("system"), *id, "Documentation", Slug("docs"), nil, pageVersionUnchecked, false)
			Expect(err).To(Succeed(), "UpdateNode failed: %v",

				err)
		}

		raw, err := os.ReadFile(p)
		Expect(err).To(Succeed(), "read: %v",
			err,
		)

		frontmatter, _, err := parseRequiredFrontmatter(string(raw))
		Expect(err).To(Succeed(), "ParseFrontmatter: %v", err)
		Expect(frontmatter.LeafWikiTitle).To(
			Equal(
				"Documentation"),
			"expected leafwiki_title to be updated, got %q",

			frontmatter.LeafWikiTitle)

	})
})

var _ = ginkgo.Describe("tree service behavior", ginkgo.Label("unit"), func() {
	ginkgo.It("update node slug rename renames on disk", func() {
		svc, tmpDir := newLoadedService()

		id, err := svc.CreateNode("system", nil, "Docs", "docs", ptrKind(NodeKindPage))
		Expect(err).To(Succeed(), "CreateNode failed: %v",

			err)

		oldPath := filepath.Join(tmpDir, "root", "docs.md")
		statTreePath(oldPath)

		newSlug := "documentation"
		{
			err := svc.UpdateNode(newFixtureUserID("system"), *id, "Docs", SlugFromString(newSlug), nil, pageVersionUnchecked, false)
			Expect(err).To(Succeed(), "UpdateNode failed: %v",

				err)
		}

		newPath := filepath.Join(tmpDir, "root", newSlug+".md")
		statTreePath(newPath)
		Expect(oldPath).To(beMissingTreePath())

	})
})

var _ = ginkgo.Describe("tree service behavior", ginkgo.Label("unit"), func() {
	ginkgo.It("update node allows rename to same basename page section twin", func() {
		svc, tmpDir := newLoadedService()
		{

			_, err := svc.CreateNode("system", nil, "Sync Section", "sync", ptrKind(NodeKindSection))
			Expect(err).To(Succeed(), "CreateNode section failed: %v",

				err,
			)
		}

		pageID, err := svc.CreateNode("system", nil, "Draft Page", "draft", ptrKind(NodeKindPage))
		Expect(err).To(Succeed(), "CreateNode page failed: %v",

			err)
		{

			err := svc.UpdateNode(newFixtureUserID("system"), *pageID, "Sync Page", Slug("sync"), nil, pageVersionUnchecked, false)
			Expect(err).To(Succeed(), "UpdateNode page rename to section basename failed: %v",

				err,
			)
		}

		statTreePath(filepath.Join(tmpDir, "root", "sync.md"))
		statTreePath(filepath.Join(tmpDir, "root", "sync", "index.md"))

		page, err := svc.FindPageByRoutePathAndKind("sync", NodeKindPage)
		Expect(err).To(Succeed(), "FindPageByRoutePathAndKind page failed: %v",

			err,
		)

		Expect(page.ID).To(Equal(*pageID))

	})
})

var _ = ginkgo.Describe("tree service behavior", ginkgo.Label("unit"), func() {
	ginkgo.It("update node rejects case insensitive slug conflict", func() {
		svc, _ := newLoadedService()

		firstID, err := svc.CreateNode("system", nil, "Alpha", "Alpha", ptrKind(NodeKindPage))
		Expect(err).To(Succeed(), "CreateNode first failed: %v",

			err)

		secondID, err := svc.CreateNode("system", nil, "Beta", "Beta", ptrKind(NodeKindPage))
		Expect(err).To(Succeed(), "CreateNode second failed: %v",

			err)

		err = svc.UpdateNode(newFixtureUserID("system"), *secondID, "Beta", Slug("alpha"), nil, pageVersionUnchecked, false)
		Expect(err).To(MatchError(ErrPageAlreadyExists), "expected ErrPageAlreadyExists, got %v",

			err,
		)

		page, err := svc.GetPage(*firstID)
		Expect(err).To(Succeed(), "GetPage first failed: %v",

			err)
		Expect(page.Slug).To(Equal(newFixtureSlug("Alpha")),
			"expected original slug to remain unchanged, got %q",

			page.Slug)

	})
})

var _ = ginkgo.Describe("tree service behavior", ginkgo.Label("unit"), func() {
	ginkgo.It("update node rejects traversal slug", func() {
		svc, dataDir := newLoadedService()
		id, err := svc.CreateNode("system", nil, "Docs", "docs", ptrKind(NodeKindPage))
		Expect(err).To(Succeed(), "CreateNode failed: %v",

			err)

		err = svc.UpdateNode(newFixtureUserID("system"), *id, "Docs", Slug("../outside"), nil, pageVersionUnchecked, false)
		Expect(err).To(MatchError(ErrInvalidOperation), "expected ErrInvalidOperation, got %v",

			err,
		)

		statTreePath(filepath.Join(dataDir, "root", "docs.md"))
		Expect(filepath.Join(dataDir, "outside.md")).To(beMissingTreePath())

	})
})

/*
Disable this test for now as we are not enforcing to pass the kinds yet.
var _ = ginkgo.Describe("tree service behavior", ginkgo.Label("unit"), func() {
	ginkgo.It("update node section to page disallowed with children", func() {
	svc, _ := newLoadedService()

	// Create parent page, then child to force parent to section
	parentID, err := svc.CreateNode("system", nil, "Docs", "docs", ptrKind(NodeKindPage))
		Expect(err).To(Succeed())
		_, err = svc.CreateNode("system", parentID, "Child", "child", ptrKind(NodeKindPage))
		Expect(err).To(Succeed())

	// Now parent is section with children, attempt to convert back to page
	err = svc.UpdateNode(newFixtureUserID("system"), *parentID, "Docs", Slug("docs"), nil, pageVersionUnchecked, false)
		Expect(err).To(MatchError(ErrPageHasChildren))

	})
})
*/

var _ = ginkgo.Describe("tree service behavior", ginkgo.Label("unit"), func() {
	ginkgo.It("delete node non recursive errors when has children", func() {
		svc, _ := newLoadedService()

		parentID, _ := svc.CreateNode("system", nil, "Parent", "parent", ptrKind(NodeKindPage))
		_, _ = svc.CreateNode("system", parentID, "Child", "child", ptrKind(NodeKindPage))

		err := svc.DeleteNode("system", *parentID, false, pageVersionUnchecked)
		Expect(err).To(MatchError(ErrPageHasChildren), "expected ErrPageHasChildren, got: %v",

			err,
		)

	})
})

var _ = ginkgo.Describe("tree service behavior", ginkgo.Label("unit"), func() {
	ginkgo.It("delete node recursive deletes disk and tree", func() {
		svc, tmpDir := newLoadedService()

		parentID, _ := svc.CreateNode("system", nil, "Parent", "parent", ptrKind(NodeKindPage))
		_, _ = svc.CreateNode("system", parentID, "Child", "child", ptrKind(NodeKindPage))

		// Parent should now be a folder
		parentDir := filepath.Join(tmpDir, "root", "parent")
		statTreePath(parentDir)

		err := svc.DeleteNode("system", *parentID, true, pageVersionUnchecked)
		Expect(err).To(Succeed(), "DeleteNode recursive failed: %v",

			err,
		)

		// Folder should be gone
		Expect(parentDir).To(beMissingTreePath())
		Expect(svc.GetTree().
			Children).
			To(HaveLen(0), "expected root to have no children")

		// Tree should have no children at root

	})
})

var _ = ginkgo.Describe("tree service behavior", ginkgo.Label("unit"), func() {
	ginkgo.It("delete page LeafWiki success removes file and tree and reindexes", func() {
		svc, tmpDir := newLoadedService()

		// Create 3 leaf pages
		idA, err := svc.CreateNode("system", nil, "A", "a", ptrKind(NodeKindPage))
		Expect(err).To(Succeed(), "CreateNode A: %v",

			err)

		idB, err := svc.CreateNode("system", nil, "B", "b", ptrKind(NodeKindPage))
		Expect(err).To(Succeed(), "CreateNode B: %v",

			err)

		idC, err := svc.CreateNode("system", nil, "C", "c", ptrKind(NodeKindPage))
		Expect(err).To(Succeed(), "CreateNode C: %v",

			err)

		// Verify files exist
		pathA := filepath.Join(tmpDir, "root", "a.md")
		pathB := filepath.Join(tmpDir, "root", "b.md")
		pathC := filepath.Join(tmpDir, "root", "c.md")
		{
			_, err := os.Stat(pathB)
			Expect(err).To(Succeed(), "expected %s exists: %v",

				pathB, err,
			)
		}
		{

			// Delete middle page (B)
			err := svc.DeleteNode("system", *idB, false, pageVersionUnchecked)
			Expect(err).To(Succeed(), "DeleteNode failed: %v",

				err)
		}
		{

			// Disk: B gone; A/C still there
			_, err := os.Stat(pathB)
			Expect(err).To(MatchError(os.ErrNotExist),
				"expected %s to be deleted, got err=%v",

				pathB,

				err)
		}
		{

			_, err := os.Stat(pathA)
			Expect(err).To(Succeed(), "expected %s exists: %v",

				pathA, err,
			)
		}
		{

			_, err := os.Stat(pathC)
			Expect(err).To(Succeed(), "expected %s exists: %v",

				pathC, err,
			)
		}

		// Tree: only 2 children remain
		root := svc.GetTree()
		Expect(root.Children).To(HaveLen(2),
			"expected 2 children after delete, got %d",

			len(root.
				Children,
			))

		// Ensure deleted ID not present
		for _, ch := range root.Children {
			Expect(ch.ID).NotTo(
				Equal(*idB), "deleted node still present in tree",
			)

		}
		Expect(root).To(haveChildPositions(0, 1), "expected positions reindexed to 0..1")

		// Reindex: positions must be 0..1 (order depends on previous positions; we just assert contiguous)

		// Optional: ensure remaining IDs are the ones we expect
		_ = idA
		_ = idC

	})
})

var _ = ginkgo.Describe("tree service behavior", ginkgo.Label("unit"), func() {
	ginkgo.It("delete node updates root order file", func() {
		svc, tmpDir := newLoadedService()

		idA, err := svc.CreateNode("system", nil, "A", "a", ptrKind(NodeKindPage))
		Expect(err).To(Succeed(), "CreateNode A: %v",

			err)

		idB, err := svc.CreateNode("system", nil, "B", "b", ptrKind(NodeKindPage))
		Expect(err).To(Succeed(), "CreateNode B: %v",

			err)

		idC, err := svc.CreateNode("system", nil, "C", "c", ptrKind(NodeKindPage))
		Expect(err).To(Succeed(), "CreateNode C: %v",

			err)
		{

			err := svc.DeleteNode("system", *idB, false, pageVersionUnchecked)
			Expect(err).To(Succeed(), "DeleteNode failed: %v",

				err)
		}

		Expect(readOrderIDs(filepath.Join(tmpDir, "root"))).To(matchPersistedPageIDOrder(*idA, *idC))

	})
})

var _ = ginkgo.Describe("tree service behavior", ginkgo.Label("unit"), func() {
	ginkgo.It("delete page with children non recursive returns err page has children", func() {
		svc, _ := newLoadedService()

		parentID, err := svc.CreateNode("system", nil, "Parent", "parent", ptrKind(NodeKindPage))
		Expect(err).To(Succeed(), "CreateNode parent: %v",

			err)

		_, err = svc.CreateNode("system", parentID, "Child", "child", ptrKind(NodeKindPage))
		Expect(err).To(Succeed(), "CreateNode child: %v",

			err)

		err = svc.DeleteNode("system", *parentID, false, pageVersionUnchecked)
		Expect(err).To(MatchError(ErrPageHasChildren), "expected ErrPageHasChildren, got: %v",

			err,
		)

	})
})

var _ = ginkgo.Describe("tree service behavior", ginkgo.Label("unit"), func() {
	ginkgo.It("delete page with children recursive deletes folder", func() {
		svc, tmpDir := newLoadedService()

		parentID, err := svc.CreateNode("system", nil, "Parent", "parent", ptrKind(NodeKindPage))
		Expect(err).To(Succeed(), "CreateNode parent: %v",

			err)

		_, err = svc.CreateNode("system", parentID, "Child", "child", ptrKind(NodeKindPage))
		Expect(err).To(Succeed(), "CreateNode child: %v",

			err)

		// Parent was auto-converted to section -> folder should exist
		parentDir := filepath.Join(tmpDir, "root", "parent")
		{
			_, err := os.Stat(parentDir)
			Expect(err).To(Succeed(), "expected parent directory exists (after auto-convert): %v",

				err)
		}
		{

			// Recursive delete should remove the folder
			err := svc.DeleteNode("system", *parentID, true, pageVersionUnchecked)
			Expect(err).To(Succeed(), "DeleteNode recursive failed: %v",

				err,
			)
		}
		{

			_, err := os.Stat(parentDir)
			Expect(err).To(MatchError(os.ErrNotExist),
				"expected parent folder deleted, got err=%v",

				err,
			)
		}
		Expect(svc.GetTree().
			Children).
			To(HaveLen(0), "expected root to have no children after delete, got %d",

				len(svc.GetTree().Children))

		// Tree should no longer contain parent

	})
})

var _ = ginkgo.Describe("tree service behavior", ginkgo.Label("unit"), func() {
	ginkgo.It("delete page invalid ID returns err page not found", func() {
		svc, _ := newLoadedService()

		err := svc.DeleteNode("system", "does-not-exist", false, pageVersionUnchecked)
		Expect(err).To(MatchError(ErrPageNotFound),
			"expected ErrPageNotFound, got: %v",

			err,
		)

	})
})

var _ = ginkgo.Describe("tree service behavior", ginkgo.Label("unit"), func() {
	ginkgo.It("delete page drift file missing returns an error", func() {
		svc, tmpDir := newLoadedService()

		// Create a leaf page normally (creates file)
		id, err := svc.CreateNode("system", nil, "Ghost", "ghost", ptrKind(NodeKindPage))
		Expect(err).To(Succeed(), "CreateNode: %v",

			err,
		)

		// Delete the file manually to simulate drift
		p := filepath.Join(tmpDir, "root", "ghost.md")
		{
			err := os.Remove(p)
			Expect(err).To(Succeed(), "failed to remove file to simulate drift: %v",

				err)
		}

		// Now delete node - should error (drift)
		err = svc.DeleteNode("system", *id, false, pageVersionUnchecked)

		// If you have a concrete DriftError type, you can assert with errors.As.
		var dErr *DriftError
		Expect(err).To(matchErrorAs(&dErr), "expected DriftError, got: %T (%v)",

			err, err)

	})
})

// --- C) Move semantics ---

var _ = ginkgo.Describe("tree service behavior", ginkgo.Label("unit"), func() {
	ginkgo.It("move node target page auto converts to section", func() {
		svc, tmpDir := newLoadedService()

		aID, _ := svc.CreateNode("system", nil, "A", "a", ptrKind(NodeKindPage))
		bID, _ := svc.CreateNode("system", nil, "B", "b", ptrKind(NodeKindPage))
		{

			// Move A under B (B is a page => should auto-convert to section)
			err := svc.MoveNode("system", *aID, *bID, pageVersionUnchecked)
			Expect(err).To(Succeed(), "MoveNode failed: %v",

				err)
		}

		// B should now be folder with index.md
		bDir := filepath.Join(tmpDir, "root", "b")
		statTreePath(bDir)
		statTreePath(filepath.Join(bDir, "index.md"))

		// A should now be inside B folder
		aPath := filepath.Join(bDir, "a.md")
		statTreePath(aPath)

	})
})

var _ = ginkgo.Describe("tree service behavior", ginkgo.Label("unit"), func() {
	ginkgo.It("move node updates source and destination order files", func() {
		svc, tmpDir := newLoadedService()

		destID, err := svc.CreateNode("system", nil, "Dest", "dest", ptrKind(NodeKindSection))
		Expect(err).To(Succeed(), "CreateNode dest: %v",

			err)

		moveID, err := svc.CreateNode("system", nil, "Move", "move", ptrKind(NodeKindPage))
		Expect(err).To(Succeed(), "CreateNode move: %v",

			err)

		stayID, err := svc.CreateNode("system", nil, "Stay", "stay", ptrKind(NodeKindPage))
		Expect(err).To(Succeed(), "CreateNode stay: %v",

			err)

		nestedID, err := svc.CreateNode("system", destID, "Nested", "nested", ptrKind(NodeKindPage))
		Expect(err).To(Succeed(), "CreateNode nested: %v",

			err)
		{

			err := svc.MoveNode("system", *moveID, *destID, pageVersionUnchecked)
			Expect(err).To(Succeed(), "MoveNode failed: %v",

				err)
		}

		Expect(readOrderIDs(filepath.Join(tmpDir, "root"))).To(matchPersistedPageIDOrder(*destID, *stayID))

		Expect(readOrderIDs(filepath.Join(tmpDir, "root", "dest"))).To(matchPersistedPageIDOrder(*nestedID, *moveID))

	})
})

var _ = ginkgo.Describe("tree service behavior", ginkgo.Label("unit"), func() {
	ginkgo.It("move node allows move to same basename page section twin", func() {
		svc, tmpDir := newLoadedService()

		destID, err := svc.CreateNode("system", nil, "Dest", "dest", ptrKind(NodeKindSection))
		Expect(err).To(Succeed(), "CreateNode dest failed: %v",

			err)
		{

			_, err := svc.CreateNode("system", destID, "Sync Section", "sync", ptrKind(NodeKindSection))
			Expect(err).To(Succeed(), "CreateNode destination section failed: %v",

				err,
			)
		}

		moveID, err := svc.CreateNode("system", nil, "Sync Page", "sync", ptrKind(NodeKindPage))
		Expect(err).To(Succeed(), "CreateNode page failed: %v",

			err)
		{

			err := svc.MoveNode("system", *moveID, *destID, pageVersionUnchecked)
			Expect(err).To(Succeed(), "MoveNode page next to section basename failed: %v",

				err)
		}

		statTreePath(filepath.Join(tmpDir, "root", "dest", "sync.md"))
		statTreePath(filepath.Join(tmpDir, "root", "dest", "sync", "index.md"))

		page, err := svc.FindPageByRoutePathAndKind("dest/sync", NodeKindPage)
		Expect(err).To(Succeed(), "FindPageByRoutePathAndKind moved page failed: %v",

			err)

		Expect(page.ID).To(Equal(*moveID))

	})
})

var _ = ginkgo.Describe("tree service behavior", ginkgo.Label("unit"), func() {
	ginkgo.It("move node persists moved node metadata to frontmatter", func() {
		svc, tmpDir := newLoadedService()

		destID, err := svc.CreateNode("system", nil, "Dest", "dest", ptrKind(NodeKindSection))
		Expect(err).To(Succeed(), "CreateNode dest: %v",

			err)

		moveID, err := svc.CreateNode("system", nil, "Move", "move", ptrKind(NodeKindPage))
		Expect(err).To(Succeed(), "CreateNode move: %v",

			err)

		node, err := svc.FindPageByID(*moveID)
		Expect(err).To(Succeed(), "FindPageByID failed: %v",

			err)

		beforeUpdatedAt := node.Metadata.UpdatedAt
		{

			err := svc.MoveNode("alice", *moveID, *destID, pageVersionUnchecked)
			Expect(err).To(Succeed(), "MoveNode failed: %v",

				err)
		}

		raw, err := os.ReadFile(filepath.Join(tmpDir, "root", "dest", "move.md"))
		Expect(err).To(Succeed(), "read moved page: %v",

			err)

		frontmatter, _, err := parseRequiredFrontmatter(string(raw))
		Expect(err).To(Succeed(), "ParseFrontmatter: %v", err)
		Expect(frontmatter).To(SatisfyAll(
			HaveField("LeafWikiLastAuthorID", Equal("alice")),
			HaveField("LeafWikiUpdatedAt", Not(BeEmpty())),
		), "expected moved page metadata to persist, got %#v", frontmatter)

		reloaded := NewTreeService(tmpDir)
		{
			err := reloaded.LoadTree()
			Expect(err).To(Succeed(), "LoadTree after move failed: %v",

				err,
			)
		}

		reloadedNode, err := reloaded.FindPageByID(*moveID)
		Expect(err).To(Succeed(), "FindPageByID after reload failed: %v",

			err)
		Expect(reloadedNode.
			Metadata.LastAuthorID,
		).
			To(Equal(newFixtureUserID("alice")),
				"expected persisted last author after reload, got %#v",

				reloadedNode.Metadata)

		persistedUpdatedAt, err := time.Parse(time.RFC3339, frontmatter.LeafWikiUpdatedAt)
		Expect(err).To(Succeed(), "parse persisted updated_at failed: %v",

			err)
		Expect(reloadedNode.
			Metadata.UpdatedAt.
			Equal(persistedUpdatedAt)).To(BeTrue(), "expected reloaded metadata to match persisted frontmatter, frontmatter=%s reloaded=%s (before=%s)",

			persistedUpdatedAt,

			reloadedNode.Metadata.UpdatedAt,
			beforeUpdatedAt)

	})
})

var _ = ginkgo.Describe("tree service behavior", ginkgo.Label("unit"), func() {
	ginkgo.It("move node returns an error and rolls back when order persistence fails", func() {
		svc, tmpDir := newLoadedService()

		destID, err := svc.CreateNode("system", nil, "Dest", "dest", ptrKind(NodeKindSection))
		Expect(err).To(Succeed(), "CreateNode dest: %v",

			err)

		moveID, err := svc.CreateNode("system", nil, "Move", "move", ptrKind(NodeKindPage))
		Expect(err).To(Succeed(), "CreateNode move: %v",

			err)
		{

			err := os.Remove(filepath.Join(tmpDir, "root", ".order.json"))
			Expect(err).To(Succeed(), "remove root order file: %v",

				err)
		}

		createTreeDirectory(filepath.Join(tmpDir, "root", ".order.json"))
		{
			err := os.Remove(filepath.Join(tmpDir, "root", "dest", ".order.json"))
			Expect(err).To(SatisfyAny(Succeed(), matchErrorIs(os.ErrNotExist)), "remove dest order file: %v", err)
		}

		createTreeDirectory(filepath.Join(tmpDir, "root", "dest", ".order.json"))

		err = svc.MoveNode("system", *moveID, *destID, pageVersionUnchecked)
		Expect(err).To(MatchError(ErrPersistSourceChildOrder), "expected source child order persistence error, got: %v",

			err)

		statTreePath(filepath.Join(tmpDir, "root", "move.md"))
		{
			_, statErr := os.Stat(filepath.Join(tmpDir, "root", "dest", "move.md"))
			Expect(statErr).To(MatchError(
				os.ErrNotExist,
			), "expected moved file to be rolled back from destination, stat err = %v",

				statErr)
		}

		root := svc.GetTree()
		Expect(root.Children).To(HaveLen(2),
			"expected rollback to restore root children, got %#v",

			root.Children)
		Expect(root).To(haveChildPageIDs(*destID, *moveID), "unexpected root children after rollback")

		dest := findChildBySlug(root, "dest")
		Expect(dest.Children).To(HaveLen(0),
			"expected destination children to be rolled back, got %#v",

			dest.Children)

	})
})

var _ = ginkgo.Describe("tree service behavior", ginkgo.Label("unit"), func() {
	ginkgo.It("move node prevents circular reference", func() {
		svc, _ := newLoadedService()

		aID, _ := svc.CreateNode("system", nil, "A", "a", ptrKind(NodeKindPage))
		// create child under A so A becomes section and has child
		bID, _ := svc.CreateNode("system", aID, "B", "b", ptrKind(NodeKindPage))

		// Try move A under B (A -> ... -> B). Should error with circular reference.
		err := svc.MoveNode("system", *aID, *bID, pageVersionUnchecked)
		Expect(err).To(MatchError(ErrMovePageCircularReference), "expected ErrMovePageCircularReference, got: %v",

			err)

	})
})

var _ = ginkgo.Describe("tree service behavior", ginkgo.Label("unit"), func() {
	ginkgo.It("move node prevents self parent", func() {
		svc, _ := newLoadedService()

		aID, _ := svc.CreateNode("system", nil, "A", "a", ptrKind(NodeKindPage))

		err := svc.MoveNode("system", *aID, *aID, pageVersionUnchecked)
		Expect(err).To(MatchError(ErrPageCannotBeMovedToItself), "expected ErrPageCannotBeMovedToItself, got: %v",

			err)

	})
})

var _ = ginkgo.Describe("tree service behavior", ginkgo.Label("unit"), func() {
	ginkgo.It("move node rejects case insensitive slug conflict", func() {
		svc, _ := newLoadedService()

		parentID, err := svc.CreateNode("system", nil, "Parent", "parent", ptrKind(NodeKindSection))
		Expect(err).To(Succeed(), "CreateNode parent failed: %v",

			err)

		moveID, err := svc.CreateNode("system", nil, "Move", "Alpha", ptrKind(NodeKindPage))
		Expect(err).To(Succeed(), "CreateNode move failed: %v",

			err)
		{

			_, err := svc.CreateNode("system", parentID, "Existing", "alpha", ptrKind(NodeKindPage))
			Expect(err).To(Succeed(), "CreateNode existing failed: %v",

				err,
			)
		}

		err = svc.MoveNode("system", *moveID, *parentID, pageVersionUnchecked)
		Expect(err).To(MatchError(ErrPageAlreadyExists), "expected ErrPageAlreadyExists, got %v",

			err,
		)

	})
})

// --- D) SortPages ---

var _ = ginkgo.Describe("tree service behavior", ginkgo.Label("unit"), func() {
	ginkgo.It("sort pages valid order", func() {
		svc, _ := newLoadedService()

		idA, _ := svc.CreateNode("system", nil, "A", "a", ptrKind(NodeKindPage))
		idB, _ := svc.CreateNode("system", nil, "B", "b", ptrKind(NodeKindPage))
		idC, _ := svc.CreateNode("system", nil, "C", "c", ptrKind(NodeKindPage))

		err := svc.SortPages("root", testPageIDs(*idC, *idA, *idB))
		Expect(err).To(Succeed(), "SortPages failed: %v",

			err)

		root := svc.GetTree()
		Expect(root).To(haveChildPageIDs(*idC, *idA, *idB), "unexpected order after sort")
		Expect(root).To(haveChildPositions(0, 1, 2), "expected positions to be reindexed")

	})
})

var _ = ginkgo.Describe("tree service behavior", ginkgo.Label("unit"), func() {
	ginkgo.It("sort pages persists order file without changing metadata", func() {
		svc, tmpDir := newLoadedService()

		idA, err := svc.CreateNode("system", nil, "A", "a", ptrKind(NodeKindPage))
		Expect(err).To(Succeed(), "CreateNode A: %v",

			err)

		idB, err := svc.CreateNode("system", nil, "B", "b", ptrKind(NodeKindPage))
		Expect(err).To(Succeed(), "CreateNode B: %v",

			err)

		idC, err := svc.CreateNode("system", nil, "C", "c", ptrKind(NodeKindPage))
		Expect(err).To(Succeed(), "CreateNode C: %v",

			err)

		root := svc.GetTree()
		before := map[PageID]PageMetadata{}
		for _, child := range root.Children {
			before[child.ID] = child.Metadata
		}
		{

			err := svc.SortPages("root", testPageIDs(*idC, *idA, *idB))
			Expect(err).To(Succeed(), "SortPages failed: %v",

				err)
		}

		orderPath := filepath.Join(tmpDir, "root", ".order.json")
		raw, err := os.ReadFile(orderPath)
		Expect(err).To(Succeed(), "read order file: %v",

			err)

		var persisted struct {
			OrderedIDs []string `json:"ordered_ids"`
		}
		{
			err := json.Unmarshal(raw, &persisted)
			Expect(err).To(Succeed(), "unmarshal order file: %v",

				err)
		}

		Expect(persisted.OrderedIDs).To(matchPersistedPageIDOrder(*idC, *idA, *idB))

		for _, child := range svc.GetTree().Children {
			{
				got := child.Metadata
				Expect(got).To(Equal(before[child.
					ID],
				), "metadata changed during reorder for %q: before=%+v after=%+v",

					child.ID, before[child.ID], got)
			}

		}

	})
})

var _ = ginkgo.Describe("tree service behavior", ginkgo.Label("unit"), func() {
	ginkgo.It("sort pages rolls back when order persistence fails", func() {
		svc, tmpDir := newLoadedService()

		idA, err := svc.CreateNode("system", nil, "A", "a", ptrKind(NodeKindPage))
		Expect(err).To(Succeed(), "CreateNode A: %v",

			err)

		idB, err := svc.CreateNode("system", nil, "B", "b", ptrKind(NodeKindPage))
		Expect(err).To(Succeed(), "CreateNode B: %v",

			err)

		idC, err := svc.CreateNode("system", nil, "C", "c", ptrKind(NodeKindPage))
		Expect(err).To(Succeed(), "CreateNode C: %v",

			err)
		{

			err := os.Remove(filepath.Join(tmpDir, "root", ".order.json"))
			Expect(err).To(Succeed(), "remove root order file: %v",

				err)
		}

		createTreeDirectory(filepath.Join(tmpDir, "root", ".order.json"))

		err = svc.SortPages("root", testPageIDs(*idC, *idA, *idB))
		Expect(err).To(MatchError(ErrPersistChildOrder), "expected child order persistence error, got: %v",

			err)

		root := svc.GetTree()
		got := []PageID{root.Children[0].ID, root.Children[1].ID, root.Children[2].ID}
		Expect(got).To(matchPageIDOrder(*idA, *idB, *idC))
		for i, child := range root.Children {
			Expect(child.Position).To(Equal(i), "expected child %q position rollback to %d, got %d",

				child.
					ID, i, child.Position)

		}

	})
})

var _ = ginkgo.Describe("tree service behavior", ginkgo.Label("unit"), func() {
	ginkgo.It("sort pages invalid length", func() {
		svc, _ := newLoadedService()

		_, _ = svc.CreateNode("system", nil, "A", "a", ptrKind(NodeKindPage))
		_, _ = svc.CreateNode("system", nil, "B", "b", ptrKind(NodeKindPage))

		err := svc.SortPages("root", testPageIDs(PageID("only-one")))
		Expect(err).To(MatchError(ErrInvalidSortOrder), "expected ErrInvalidSortOrder, got: %v",

			err,
		)

	})
})

var _ = ginkgo.Describe("tree service behavior", ginkgo.Label("unit"), func() {
	ginkgo.It("sort pages duplicate ID", func() {
		svc, _ := newLoadedService()

		idA, _ := svc.CreateNode("system", nil, "A", "a", ptrKind(NodeKindPage))
		idB, _ := svc.CreateNode("system", nil, "B", "b", ptrKind(NodeKindPage))

		err := svc.SortPages("root", testPageIDs(*idA, *idA, *idB))
		Expect(err).To(MatchError(ErrInvalidSortOrder), "expected duplicate sort IDs to return invalid sort order, got %v", err)

	})
})

// --- E) Routing, Lookup, Ensure ---

var _ = ginkgo.Describe("tree service behavior", ginkgo.Label("unit"), func() {
	ginkgo.It("get page section without index does not materialize index", func() {
		svc, tmpDir := newLoadedService()

		id, err := svc.CreateNode("system", nil, "Docs", "docs", ptrKind(NodeKindSection))
		Expect(err).To(Succeed(), "CreateNode failed: %v",

			err)

		indexPath := filepath.Join(tmpDir, "root", "docs", "index.md")
		{
			err := os.Remove(indexPath)
			Expect(err).To(Succeed(), "remove index.md: %v",

				err)
		}

		page, err := svc.GetPage(*id)
		Expect(err).To(Succeed(), "GetPage failed: %v",

			err)

		Expect(page).To(SatisfyAll(
			HaveField("ID", Equal(*id)),
			HaveField("Content", BeEmpty()),
		), "expected empty content for section without index, got %#v", page)
		{

			_, err := os.Stat(indexPath)
			Expect(err).To(MatchError(os.ErrNotExist), "expected GetPage to avoid materializing index.md")
		}

	})
})

var _ = ginkgo.Describe("tree service behavior", ginkgo.Label("unit"), func() {
	ginkgo.It("convert node page to section materializes index with node metadata", func() {
		svc, tmpDir := newLoadedService()

		id, err := svc.CreateNode("system", nil, "Docs", "docs", ptrKind(NodeKindPage))
		Expect(err).To(Succeed(), "CreateNode failed: %v",

			err)

		node, err := svc.FindPageByID(*id)
		Expect(err).To(Succeed(), "FindPageByID failed: %v",

			err)

		node.Metadata.CreatedAt = time.Date(2026, time.March, 22, 10, 15, 30, 0, time.UTC)
		node.Metadata.UpdatedAt = time.Date(2026, time.March, 22, 11, 16, 31, 0, time.UTC)
		node.Metadata.CreatorID = "alice"
		node.Metadata.LastAuthorID = "bob"
		{

			err := svc.ConvertNode("carol", *id, NodeKindSection, pageVersionUnchecked)
			Expect(err).To(Succeed(), "ConvertNode failed: %v",

				err)
		}

		indexPath := filepath.Join(tmpDir, "root", "docs", "index.md")
		raw, err := os.ReadFile(indexPath)
		Expect(err).To(Succeed(), "read converted index: %v",

			err)

		frontmatter, body, err := parseRequiredFrontmatter(string(raw))
		Expect(err).To(Succeed(), "ParseFrontmatter: %v", err)
		Expect(frontmatter).To(SatisfyAll(
			HaveField("LeafWikiID", WithTransform(func(raw string) PageID {
				return PageIDFromString(raw)
			}, Equal(*id))),
			HaveField("LeafWikiTitle", Equal("Docs")),
			HaveField("LeafWikiCreatedAt", Equal("2026-03-22T10:15:30Z")),
			HaveField("LeafWikiUpdatedAt", Not(BeEmpty())),
			HaveField("LeafWikiCreatorID", Equal("alice")),
			HaveField("LeafWikiLastAuthorID", Equal("carol")),
		), "expected metadata to be carried over and updated for actor, got %#v", frontmatter)
		Expect(body).To(ContainSubstring("# Docs"),

			"expected converted body to be preserved, got %q",

			body)

	})
})

var _ = ginkgo.Describe("tree service behavior", ginkgo.Label("unit"), func() {
	ginkgo.It("find page by route path returns content", func() {
		svc, _ := newLoadedService()

		archID, _ := svc.CreateNode("system", nil, "Architecture", "architecture", ptrKind(NodeKindPage))
		// create child -> converts arch to section
		projectID, _ := svc.CreateNode("system", archID, "Project A", "project-a", ptrKind(NodeKindPage))
		_, _ = svc.CreateNode("system", projectID, "Specs", "specs", ptrKind(NodeKindPage))

		// Update specs content
		specsNode := svc.GetTree().Children[0].Children[0].Children[0]
		body := "# Specs\nHello"
		{
			err := svc.UpdateNode(newFixtureUserID("system"), specsNode.ID, "Specs", Slug("specs"), &body, pageVersionUnchecked, false)
			Expect(err).To(Succeed(), "UpdateNode content failed: %v",

				err,
			)
		}

		page, err := svc.FindPageByRoutePath("architecture/project-a/specs")
		Expect(err).To(Succeed(), "FindPageByRoutePath failed: %v",

			err,
		)
		Expect(page).To(SatisfyAll(
			HaveField("Slug", Equal(newFixtureSlug("specs"))),
			HaveField("Content", ContainSubstring("Hello")),
		), "expected routed page to include specs content, got %#v", page)

	})
})

var _ = ginkgo.Describe("tree service behavior", ginkgo.Label("unit"), func() {
	ginkgo.It("find page by route path returns not found for missing path", func() {
		svc, _ := newLoadedService()

		homeID, err := svc.CreateNode("system", nil, "Home", "home", ptrKind(NodeKindPage))
		Expect(err).To(Succeed(), "CreateNode home failed: %v",

			err)
		{

			_, err := svc.CreateNode("system", homeID, "About", "about", ptrKind(NodeKindPage))
			Expect(err).To(Succeed(), "CreateNode about failed: %v",

				err)
		}

		_, err = svc.FindPageByRoutePath("home/team")
		Expect(err).To(MatchError(ErrPageNotFound),
			"expected ErrPageNotFound, got %v",

			err,
		)

	})
})

var _ = ginkgo.Describe("tree service behavior", ginkgo.Label("unit"), func() {
	ginkgo.It("find page by route path is case sensitive", func() {
		svc, _ := newLoadedService()

		homeID, err := svc.CreateNode("system", nil, "Home", "Home", ptrKind(NodeKindPage))
		Expect(err).To(Succeed(), "CreateNode home failed: %v",

			err)
		{

			_, err := svc.CreateNode("system", homeID, "About", "About", ptrKind(NodeKindPage))
			Expect(err).To(Succeed(), "CreateNode about failed: %v",

				err)
		}

		_, err = svc.FindPageByRoutePath("home/About")
		Expect(err).To(MatchError(ErrPageNotFound),
			"expected ErrPageNotFound for case-mismatched route, got %v",

			err)

		page, err := svc.FindPageByRoutePath("Home/About")
		Expect(err).To(Succeed(), "FindPageByRoutePath exact case failed: %v",

			err,
		)
		Expect(page.Slug).To(Equal(newFixtureSlug("About")),
			"expected exact-case route to resolve About, got %q",

			page.Slug)

	})
})

var _ = ginkgo.Describe("tree service behavior", ginkgo.Label("unit"), func() {
	ginkgo.It("find page by route path and kind distinguishes same basename page and section", func() {
		svc, _, rootDir := newLoadedServiceWithDirs()
		writeTreeTestFile(filepath.Join(rootDir, "docs", "index.md"), `---
leafwiki_id: docs-section
leafwiki_title: Docs
---
# Docs
`)
		writeTreeTestFile(filepath.Join(rootDir, "docs", "sync.md"), `---
leafwiki_id: sync-page
leafwiki_title: Sync Page
---
# Sync Page
`)
		writeTreeTestFile(filepath.Join(rootDir, "docs", "sync", "index.md"), `---
leafwiki_id: sync-section
leafwiki_title: Sync Section
---
# Sync Section
`)
		writeTreeTestFile(filepath.Join(rootDir, "docs", "sync", "child.md"), `---
leafwiki_id: sync-child
leafwiki_title: Sync Child
---
# Sync Child
`)
		{
			err := svc.ReconstructTreeFromFS()
			Expect(err).To(Succeed(), "ReconstructTreeFromFS: %v",

				err)
		}

		page, err := svc.FindPageByRoutePathAndKind("docs/sync", NodeKindPage)
		Expect(err).To(Succeed(), "FindPageByRoutePathAndKind page: %v",

			err)
		Expect(page.ID).To(Equal(newFixturePageID("sync-page")),
			"page ID = %q, want sync-page",

			page.ID)

		section, err := svc.FindPageByRoutePathAndKind("docs/sync", NodeKindSection)
		Expect(err).To(Succeed(), "FindPageByRoutePathAndKind section: %v",

			err)
		Expect(section.ID).To(Equal(newFixturePageID("sync-section")),
			"section ID = %q, want sync-section",

			section.
				ID)

		child, err := svc.FindPageByRoutePath("docs/sync/child")
		Expect(err).To(Succeed(), "FindPageByRoutePath child: %v",

			err,
		)
		Expect(child.ID).To(
			Equal(newFixturePageID("sync-child")), "child ID = %q, want sync-child via section path",

			child.ID)

	})
})

var _ = ginkgo.Describe("tree service behavior", ginkgo.Label("unit"), func() {
	ginkgo.It("find page by route path prefers section for same basename twin", func() {
		svc, _ := newLoadedService()

		_, err := svc.CreateNode("system", nil, "Sync Page", "sync", ptrKind(NodeKindPage))
		Expect(err).To(Succeed(), "CreateNode page failed: %v",

			err)

		sectionID, err := svc.CreateNode("system", nil, "Sync Section", "sync", ptrKind(NodeKindSection))
		Expect(err).To(Succeed(), "CreateNode section twin failed: %v",

			err)

		page, err := svc.FindPageByRoutePath("sync")
		Expect(err).To(Succeed(), "FindPageByRoutePath sync: %v",

			err)

		Expect(page).To(SatisfyAll(
			HaveField("ID", Equal(*sectionID)),
			HaveField("Kind", Equal(NodeKindSection)),
		), "expected ambiguous route lookup to prefer the section twin, got %#v", page)

	})
})

var _ = ginkgo.Describe("tree service behavior", ginkgo.Label("unit"), func() {
	ginkgo.It("lookup page path segments", func() {
		svc, _ := newLoadedService()

		homeID, _ := svc.CreateNode("system", nil, "Home", "home", ptrKind(NodeKindPage))
		aboutID, _ := svc.CreateNode("system", homeID, "About", "about", ptrKind(NodeKindPage))

		lookup, err := svc.LookupPagePath("home/about/team")
		Expect(err).To(Succeed(), "LookupPagePath failed: %v",

			err)
		Expect(lookup).To(matchMissingPathLookup(HaveExactElements(
			matchExistingPathSegment(*homeID),
			matchExistingPathSegment(*aboutID),
			matchMissingPathSegment(),
		)),
			"expected lookup to resolve existing ancestors and report the missing leaf, got %#v", lookup)

	})
})

var _ = ginkgo.Describe("tree service behavior", ginkgo.Label("unit"), func() {
	ginkgo.It("lookup page path is case insensitive", func() {
		svc, _ := newLoadedService()

		homeID, err := svc.CreateNode("system", nil, "Home", "Home", ptrKind(NodeKindPage))
		Expect(err).To(Succeed(), "CreateNode home failed: %v",

			err)
		var aboutID *PageID
		{

			aboutID, err = svc.CreateNode("system", homeID, "About", "About", ptrKind(NodeKindPage))
			Expect(err).To(Succeed(), "CreateNode about failed: %v",

				err)
		}

		lookup, err := svc.LookupPagePath("home/about")
		Expect(err).To(Succeed(), "LookupPagePath failed: %v",

			err)
		Expect(lookup).To(matchExistingPathLookup(HaveExactElements(
			matchExistingPathSegment(*homeID),
			matchExistingPathSegment(*aboutID),
		)), "expected case-insensitive path lookup to resolve existing path")

	})
})

var _ = ginkgo.Describe("tree service behavior", ginkgo.Label("unit"), func() {
	ginkgo.It("lookup page path prefers section for same basename twin", func() {
		svc, _ := newLoadedService()

		sectionID, err := svc.CreateNode("system", nil, "Sync Section", "sync", ptrKind(NodeKindSection))
		Expect(err).To(Succeed(), "CreateNode section failed: %v",

			err,
		)
		{

			_, err := svc.CreateNode("system", nil, "Sync Page", "sync", ptrKind(NodeKindPage))
			Expect(err).To(Succeed(), "CreateNode page twin failed: %v",

				err,
			)
		}

		lookup, err := svc.LookupPagePath("sync")
		Expect(err).To(Succeed(), "LookupPagePath failed: %v",

			err)
		Expect(lookup).To(matchExistingPathLookup(HaveExactElements(SatisfyAll(
			matchExistingPathSegment(*sectionID),
			HaveField("Kind", pointToValue[NodeKind](Equal(NodeKindSection))),
		))),
			"lookup = %#v, want existing section segment", lookup)

	})
})

var _ = ginkgo.Describe("tree service behavior", ginkgo.Label("unit"), func() {
	ginkgo.It("lookup page path reflects slug rename", func() {
		svc, _ := newLoadedService()

		id, err := svc.CreateNode("system", nil, "Docs", "docs", ptrKind(NodeKindPage))
		Expect(err).To(Succeed(), "CreateNode docs failed: %v",

			err)
		var guideID *PageID
		{

			guideID, err = svc.CreateNode("system", id, "Guide", "guide", ptrKind(NodeKindPage))
			Expect(err).To(Succeed(), "CreateNode guide failed: %v",

				err)
		}
		{

			err := svc.UpdateNode(newFixtureUserID("system"), *id, "Documentation", Slug("documentation"), nil, pageVersionUnchecked, false)
			Expect(err).To(Succeed(), "UpdateNode failed: %v",

				err)
		}

		oldLookup, err := svc.LookupPagePath("docs/guide")
		Expect(err).To(Succeed(), "LookupPagePath old path failed: %v",

			err)
		Expect(oldLookup).To(matchMissingPathLookup(HaveExactElements(
			matchMissingPathSegment(),
			matchMissingPathSegment(),
		)), "expected old path to stop resolving after slug rename")

		newLookup, err := svc.LookupPagePath("documentation/guide")
		Expect(err).To(Succeed(), "LookupPagePath new path failed: %v",

			err)
		Expect(newLookup).To(matchExistingPathLookup(HaveExactElements(
			matchExistingPathSegment(*id),
			matchExistingPathSegment(*guideID),
		)), "expected renamed path to resolve")

		page, err := svc.FindPageByRoutePath("documentation/guide")
		Expect(err).To(Succeed(), "FindPageByRoutePath renamed path failed: %v",

			err)
		Expect(page.Slug).To(Equal(newFixtureSlug("guide")),
			"expected guide page, got %q",

			page.
				Slug)

	})
})

var _ = ginkgo.Describe("tree service behavior", ginkgo.Label("unit"), func() {
	ginkgo.It("lookup page path can create for missing valid path", func() {
		svc, _ := newLoadedService()

		lookup, err := svc.LookupPagePath("docs/guide")
		Expect(err).To(Succeed(), "LookupPagePath failed: %v",

			err)
		Expect(lookup.CanCreate).To(BeTrue(),
			"expected missing valid path to be creatable",
		)

	})
})

var _ = ginkgo.Describe("tree service behavior", ginkgo.Label("unit"), func() {
	ginkgo.It("lookup page path cannot create reserved missing path", func() {
		svc, _ := newLoadedService()

		lookup, err := svc.LookupPagePath("history/guide")
		Expect(err).To(Succeed(), "LookupPagePath failed: %v",

			err)
		Expect(lookup.CanCreate).To(BeFalse(),
			"expected reserved slug path to be non-creatable",
		)

	})
})

var _ = ginkgo.Describe("tree service behavior", ginkgo.Label("unit"), func() {
	ginkgo.It("resolve permalink target reflects rename and move", func() {
		svc, _ := newLoadedService()

		docsID, err := svc.CreateNode("system", nil, "Docs", "docs", ptrKind(NodeKindPage))
		Expect(err).To(Succeed(), "CreateNode docs failed: %v",

			err)

		guideID, err := svc.CreateNode("system", docsID, "Guide", "guide", ptrKind(NodeKindPage))
		Expect(err).To(Succeed(), "CreateNode guide failed: %v",

			err)

		archiveID, err := svc.CreateNode("system", nil, "Archive", "archive", ptrKind(NodeKindPage))
		Expect(err).To(Succeed(), "CreateNode archive failed: %v",

			err,
		)
		{

			err := svc.UpdateNode(newFixtureUserID("system"), *guideID, "User Guide", Slug("user-guide"), nil, pageVersionUnchecked, false)
			Expect(err).To(Succeed(), "UpdateNode guide failed: %v",

				err)
		}
		{

			err := svc.MoveNode("system", *guideID, *archiveID, pageVersionUnchecked)
			Expect(err).To(Succeed(), "MoveNode guide failed: %v",

				err)
		}

		target, err := svc.ResolvePermalinkTarget(*guideID)
		Expect(err).To(Succeed(), "ResolvePermalinkTarget failed: %v",

			err)

		Expect(target).To(SatisfyAll(
			HaveField("ID", Equal(*guideID)),
			HaveField("Slug", Equal(newFixtureSlug("user-guide"))),
			HaveField("Path", Equal("archive/user-guide")),
		), "expected permalink to resolve the archived guide, got %#v", target)

	})
})

var _ = ginkgo.Describe("tree service behavior", ginkgo.Label("unit"), func() {
	ginkgo.It("resolve permalink target returns not found for missing page", func() {
		svc, _ := newLoadedService()

		_, err := svc.ResolvePermalinkTarget("missing-page")
		Expect(err).To(MatchError(ErrPageNotFound),
			"expected ErrPageNotFound, got %v",

			err,
		)

	})
})

var _ = ginkgo.Describe("tree service behavior", ginkgo.Label("unit"), func() {
	ginkgo.It("ensure page path persists order files for created path", func() {
		svc, tmpDir := newLoadedService()

		res, err := svc.EnsurePagePath("system", "home/about/team/members", "Members", ptrKind(NodeKindPage))
		Expect(err).To(Succeed(), "EnsurePagePath failed: %v",

			err)
		Expect(res.Page).To(SatisfyAll(
			Not(BeNil()),
			HaveField("Slug", Equal(newFixtureSlug("members"))),
		), "expected final page 'members'")

		rootOrder := readOrderIDs(filepath.Join(tmpDir, "root"))
		Expect(rootOrder).To(matchPersistedPageIDOrder(res.Created[0].ID),
			"unexpected root order after EnsurePagePath: %v", rootOrder)

		homeOrder := readOrderIDs(filepath.Join(tmpDir, "root", "home"))
		Expect(homeOrder).To(matchPersistedPageIDOrder(res.Created[1].ID),
			"unexpected home order after EnsurePagePath: %v", homeOrder)

		aboutOrder := readOrderIDs(filepath.Join(tmpDir, "root", "home", "about"))
		Expect(aboutOrder).To(matchPersistedPageIDOrder(res.Created[2].ID),
			"unexpected about order after EnsurePagePath: %v", aboutOrder)

		teamOrder := readOrderIDs(filepath.Join(tmpDir, "root", "home", "about", "team"))
		Expect(teamOrder).To(matchPersistedPageIDOrder(res.Created[3].ID),
			"unexpected team order after EnsurePagePath: %v", teamOrder)

	})
})

var _ = ginkgo.Describe("tree service behavior", ginkgo.Label("unit"), func() {
	ginkgo.It("ensure page path creates intermediate sections and final page", func() {
		svc, _ := newLoadedService()

		// Ensure a deep path; intermediate nodes should become sections
		res, err := svc.EnsurePagePath("system", "home/about/team/members", "Members", ptrKind(NodeKindPage))
		Expect(err).To(Succeed(), "EnsurePagePath failed: %v",

			err)
		Expect(res.Page).To(SatisfyAll(
			Not(BeNil()),
			HaveField("Slug", Equal(newFixtureSlug("members"))),
		), "expected final page 'members'")

		// home/about/team should exist as path now
		lookup, err := svc.LookupPagePath("home/about/team/members")
		Expect(err).To(Succeed(), "LookupPagePath failed: %v",

			err)
		Expect(lookup).To(matchExistingPathLookup(HaveExactElements(
			matchExistingPathSegment(res.Created[0].ID),
			matchExistingPathSegment(res.Created[1].ID),
			matchExistingPathSegment(res.Created[2].ID),
			matchExistingPathSegment(res.Created[3].ID),
		)), "expected path to exist after EnsurePagePath")

	})
})

var _ = ginkgo.Describe("tree service behavior", ginkgo.Label("unit"), func() {
	ginkgo.It("ensure page path returns existing page without creating nodes", func() {
		svc, _ := newLoadedService()

		res, err := svc.EnsurePagePath("system", "home/about/team/members", "Members", ptrKind(NodeKindPage))
		Expect(err).To(Succeed(), "EnsurePagePath initial create failed: %v",

			err,
		)

		existing, err := svc.EnsurePagePath("system", "home/about/team/members", "Ignored", ptrKind(NodeKindPage))
		Expect(err).To(Succeed(), "EnsurePagePath existing failed: %v",

			err)
		Expect(existing).To(matchExistingEnsurePathResult(matchTreeNodePointer(NodeKindPage, Equal(res.Page.ID))),
			"expected EnsurePagePath to return the existing page without creating nodes, got %#v", existing)

	})
})

var _ = ginkgo.Describe("tree service behavior", ginkgo.Label("unit"), func() {
	ginkgo.It("ensure page path creates page twin when section route exists", func() {
		svc, _ := newLoadedService()

		sectionID, err := svc.CreateNode("system", nil, "Sync Section", "sync", ptrKind(NodeKindSection))
		Expect(err).To(Succeed(), "CreateNode section failed: %v",

			err,
		)

		res, err := svc.EnsurePagePath("system", "sync", "Sync Page", ptrKind(NodeKindPage))
		Expect(err).To(Succeed(), "EnsurePagePath page twin failed: %v",

			err)
		Expect(res).To(SatisfyAll(
			HaveField("Page", matchTreeNodePointer(NodeKindPage, Not(Equal(*sectionID)))),
			HaveField("Created", HaveExactElements(
				matchTreeNodePointer(NodeKindPage, Not(Equal(*sectionID))),
			)),
		), "expected EnsurePagePath to create one page twin, got %#v", res)

		section, err := svc.FindPageByRoutePathAndKind("sync", NodeKindSection)
		Expect(err).To(Succeed(), "FindPageByRoutePathAndKind section failed: %v",

			err)

		Expect(section.ID).To(Equal(*sectionID))
		page, err := svc.FindPageByRoutePathAndKind("sync", NodeKindPage)
		Expect(err).To(Succeed(), "FindPageByRoutePathAndKind page failed: %v",

			err,
		)
		Expect(page.ID).To(Equal(res.Page.
			ID),
			"page route ID = %q, want %q",

			page.
				ID, res.
				Page.ID,
		)

		second, err := svc.EnsurePagePath("system", "sync", "Ignored", ptrKind(NodeKindPage))
		Expect(err).To(Succeed(), "EnsurePagePath existing page twin failed: %v",

			err)
		Expect(second).To(matchExistingEnsurePathResult(matchTreeNodePointer(NodeKindPage, Equal(res.Page.ID))),
			"expected second ensure to return the existing page twin, got %#v", second)

	})
})

var _ = ginkgo.Describe("tree service behavior", ginkgo.Label("unit"), func() {
	ginkgo.It("ensure page path creates section twin when page route exists", func() {
		svc, _ := newLoadedService()

		pageID, err := svc.CreateNode("system", nil, "Sync Page", "sync", ptrKind(NodeKindPage))
		Expect(err).To(Succeed(), "CreateNode page failed: %v",

			err)

		res, err := svc.EnsurePagePath("system", "sync", "Sync Section", ptrKind(NodeKindSection))
		Expect(err).To(Succeed(), "EnsurePagePath section twin failed: %v",

			err)
		Expect(res).To(SatisfyAll(
			HaveField("Page", matchTreeNodePointer(NodeKindSection, Not(Equal(*pageID)))),
			HaveField("Created", HaveExactElements(
				matchTreeNodePointer(NodeKindSection, Not(Equal(*pageID))),
			)),
		), "expected EnsurePagePath to create one section twin, got %#v", res)

		page, err := svc.FindPageByRoutePathAndKind("sync", NodeKindPage)
		Expect(err).To(Succeed(), "FindPageByRoutePathAndKind page failed: %v",

			err,
		)

		Expect(page.ID).To(Equal(*pageID))
		section, err := svc.FindPageByRoutePathAndKind("sync", NodeKindSection)
		Expect(err).To(Succeed(), "FindPageByRoutePathAndKind section failed: %v",

			err)
		Expect(section.ID).To(Equal(res.
			Page.
			ID), "section route ID = %q, want %q",

			section.
				ID, res.
				Page.ID)

		second, err := svc.EnsurePagePath("system", "sync", "Ignored", ptrKind(NodeKindSection))
		Expect(err).To(Succeed(), "EnsurePagePath existing section twin failed: %v",

			err)
		Expect(second).To(matchExistingEnsurePathResult(matchTreeNodePointer(NodeKindSection, Equal(res.Page.ID))),
			"expected second ensure to return the existing section twin, got %#v", second)

	})
})

var _ = ginkgo.Describe("tree service behavior", ginkgo.Label("unit"), func() {
	ginkgo.It("move node updates path lookup", func() {
		svc, _ := newLoadedService()

		docsID, err := svc.CreateNode("system", nil, "Docs", "docs", ptrKind(NodeKindSection))
		Expect(err).To(Succeed(), "CreateNode docs failed: %v",

			err)

		archiveID, err := svc.CreateNode("system", nil, "Archive", "archive", ptrKind(NodeKindSection))
		Expect(err).To(Succeed(), "CreateNode archive failed: %v",

			err,
		)

		guideID, err := svc.CreateNode("system", docsID, "Guide", "guide", ptrKind(NodeKindPage))
		Expect(err).To(Succeed(), "CreateNode guide failed: %v",

			err)
		{

			err := svc.MoveNode("system", *guideID, *archiveID, pageVersionUnchecked)
			Expect(err).To(Succeed(), "MoveNode failed: %v",

				err)
		}

		oldLookup, err := svc.LookupPagePath("docs/guide")
		Expect(err).To(Succeed(), "LookupPagePath old path failed: %v",

			err)
		Expect(oldLookup).To(matchMissingPathLookup(HaveExactElements(
			matchExistingPathSegment(*docsID),
			matchMissingPathSegment(),
		)), "expected old path to stop resolving after move")

		newLookup, err := svc.LookupPagePath("archive/guide")
		Expect(err).To(Succeed(), "LookupPagePath new path failed: %v",

			err)
		Expect(newLookup).To(matchExistingPathLookup(HaveExactElements(
			matchExistingPathSegment(*archiveID),
			matchExistingPathSegment(*guideID),
		)), "expected moved path to resolve at destination")

	})
})

// --- F) Migration V3 (metadata frontmatter backfill) ---
var _ = ginkgo.Describe("tree service behavior", ginkgo.Label("unit"), func() {
	ginkgo.It("load tree migrates to V5 backfills child order files", func() {
		if CurrentSchemaVersion < 5 {
			ginkgo.Skip("requires schema v5+")
		}

		tmpDir := tempTreeDir()
		{

			err := saveSchema(tmpDir, CurrentSchemaVersion)
			Expect(err).To(Succeed(), "saveSchema failed: %v",

				err)
		}

		svc := NewTreeService(tmpDir)
		{
			err := svc.LoadTree()
			Expect(err).To(Succeed(), "LoadTree failed: %v",

				err)
		}

		docsID, err := svc.CreateNode("system", nil, "Docs", "docs", ptrKind(NodeKindSection))
		Expect(err).To(Succeed(), "CreateNode docs failed: %v",

			err)

		alphaID, err := svc.CreateNode("system", nil, "Alpha", "alpha", ptrKind(NodeKindPage))
		Expect(err).To(Succeed(), "CreateNode alpha failed: %v",

			err)

		betaID, err := svc.CreateNode("system", docsID, "Beta", "beta", ptrKind(NodeKindPage))
		Expect(err).To(Succeed(), "CreateNode beta failed: %v",

			err)

		root := svc.GetTree()
		root.Children = []*PageNode{root.Children[1], root.Children[0]}
		for i, child := range root.Children {
			child.Position = i
		}

		docsNode, err := svc.FindPageByID(*docsID)
		Expect(err).To(Succeed(), "FindPageByID docs failed: %v",

			err)
		Expect(docsNode).To(haveChildPageIDs(*betaID), "expected docs child beta before migration")
		{

			err := os.Remove(filepath.Join(tmpDir, "root", ".order.json"))
			Expect(err).To(SatisfyAny(Succeed(), matchErrorIs(os.ErrNotExist)), "remove root order file: %v", err)
		}
		{

			err := os.Remove(filepath.Join(tmpDir, "root", "docs", ".order.json"))
			Expect(err).To(SatisfyAny(Succeed(), matchErrorIs(os.ErrNotExist)), "remove docs order file: %v", err)
		}

		persistLegacyTreeSnapshot(tmpDir, svc.GetTree())
		{

			err := saveSchema(tmpDir, 4)
			Expect(err).To(Succeed(), "saveSchema failed: %v",

				err)
		}

		loaded := NewTreeService(tmpDir)
		{
			err := loaded.LoadTree()
			Expect(err).To(Succeed(), "LoadTree (migrating) failed: %v",

				err,
			)
		}

		Expect(readOrderIDs(filepath.Join(tmpDir, "root"))).To(matchPersistedPageIDOrder(*alphaID, *docsID))

		Expect(readOrderIDs(filepath.Join(tmpDir, "root", "docs"))).To(matchPersistedPageIDOrder(*betaID))

	})
})

var _ = ginkgo.Describe("tree service behavior", ginkgo.Label("unit"), func() {
	ginkgo.It("load tree migrates to V4 materializes missing section index", func() {
		if CurrentSchemaVersion < 4 {
			ginkgo.Skip("requires schema v4+")
		}

		tmpDir := tempTreeDir()
		{

			err := saveSchema(tmpDir, 3)
			Expect(err).To(Succeed(), "saveSchema failed: %v",

				err)
		}

		svc := NewTreeService(tmpDir)
		{
			err := svc.LoadTree()
			Expect(err).To(Succeed(), "LoadTree failed: %v",

				err)
		}

		id, err := svc.CreateNode("system", nil, "Docs", "docs", ptrKind(NodeKindSection))
		Expect(err).To(Succeed(), "CreateNode failed: %v",

			err)

		node, err := svc.FindPageByID(*id)
		Expect(err).To(Succeed(), "FindPageByID failed: %v",

			err)

		node.Metadata = PageMetadata{
			CreatedAt:    time.Date(2026, time.March, 22, 10, 15, 30, 0, time.UTC),
			UpdatedAt:    time.Date(2026, time.March, 22, 11, 16, 31, 0, time.UTC),
			CreatorID:    "alice",
			LastAuthorID: "bob",
		}

		persistLegacyTreeSnapshot(tmpDir, svc.GetTree())

		indexPath := filepath.Join(tmpDir, "root", "docs", "index.md")
		{
			err := os.Remove(indexPath)
			Expect(err).To(Succeed(), "remove section index failed: %v",

				err,
			)
		}
		{

			err := saveSchema(tmpDir, 3)
			Expect(err).To(Succeed(), "saveSchema failed: %v",

				err)
		}

		loaded := NewTreeService(tmpDir)
		{
			err := loaded.LoadTree()
			Expect(err).To(Succeed(), "LoadTree (migrating) failed: %v",

				err,
			)
		}

		raw, err := os.ReadFile(indexPath)
		Expect(err).To(Succeed(), "read migrated section index: %v",

			err,
		)

		frontmatter, body, err := parseRequiredFrontmatter(string(raw))
		Expect(err).To(Succeed(), "ParseFrontmatter: %v", err)
		Expect(frontmatter).To(SatisfyAll(
			matchManagedFrontmatter(*id, "Docs"),
			matchFrontmatterTimestamps("2026-03-22T10:15:30Z", "2026-03-22T11:16:31Z"),
			matchFrontmatterAuthors(newFixtureUserID("alice"), newFixtureUserID("bob")),
		), "expected section frontmatter to be materialized, got %#v", frontmatter)
		Expect(strings.TrimSpace(body)).To(BeEmpty(),

			"expected empty section body after migration, got %q",

			body)

	})
})

var _ = ginkgo.Describe("tree service behavior", ginkgo.Label("unit"), func() {
	ginkgo.It("load tree resumes interrupted migration with persisted legacy snapshot", func() {
		if CurrentSchemaVersion < 3 {
			ginkgo.Skip("requires schema v3+")
		}

		tmpDir := tempTreeDir()
		{
			err := saveSchema(tmpDir, CurrentSchemaVersion)
			Expect(err).To(Succeed(), "saveSchema failed: %v",

				err)
		}

		svc := NewTreeService(tmpDir)
		{
			err := svc.LoadTree()
			Expect(err).To(Succeed(), "LoadTree failed: %v",

				err)
		}

		id, err := svc.CreateNode("system", nil, "Page1", "page1", ptrKind(NodeKindPage))
		Expect(err).To(Succeed(), "CreateNode failed: %v",

			err)

		node, err := svc.FindPageByID(*id)
		Expect(err).To(Succeed(), "FindPageByID failed: %v",

			err)

		root := svc.GetTree()
		// Build a legacy snapshot with metadata stripped, without mutating the live tree.
		legacySnapshot := &PageNode{
			ID:    root.ID,
			Slug:  root.Slug,
			Title: root.Title,
			Kind:  root.Kind,
			Children: []*PageNode{{
				ID:       node.ID,
				Slug:     node.Slug,
				Title:    node.Title,
				Kind:     node.Kind,
				Position: node.Position,
			}},
		}
		persistLegacyTreeSnapshot(tmpDir, legacySnapshot)

		pagePath := filepath.Join(tmpDir, "root", "page1.md")
		legacyBody := "# Page 1 Content\nHello World\n"
		{
			err := os.WriteFile(pagePath, []byte(legacyBody), 0o644)
			Expect(err).To(Succeed(), "write legacy content failed: %v",

				err,
			)
		}

		originalModTime := time.Date(2024, time.January, 2, 3, 4, 5, 0, time.UTC)
		{
			err := os.Chtimes(pagePath, originalModTime, originalModTime)
			Expect(err).To(Succeed(), "Chtimes failed: %v",

				err)
		}
		{

			err := saveSchema(tmpDir, 0)
			Expect(err).To(Succeed(), "saveSchema failed: %v",

				err)
		}

		interrupted := NewTreeService(tmpDir)
		legacyTree, err := interrupted.store.LoadTree(legacyTreeFilename)
		Expect(err).To(Succeed(), "LoadTree legacy snapshot failed: %v",

			err)

		interrupted.tree = legacyTree

		deps := interrupted.migrationDependencies()
		stopErr := errors.New("stop after v2")
		deps.SaveSchema = func(version int) error {
			if err := saveSchema(tmpDir, version); err != nil {
				return err
			}
			if version == 2 {
				return stopErr
			}
			return nil
		}

		err = treemigration.Run(0, deps)
		Expect(err).To(MatchError(stopErr), "expected interrupted migration error, got %v",

			err)

		reloaded := NewTreeService(tmpDir)
		{
			err := reloaded.LoadTree()
			Expect(err).To(Succeed(), "LoadTree after interrupted migration failed: %v",

				err)
		}

		raw, err := os.ReadFile(pagePath)
		Expect(err).To(Succeed(), "read resumed migration file: %v",

			err,
		)

		frontmatter, _, err := parseRequiredFrontmatter(string(raw))
		Expect(err).To(Succeed(), "ParseFrontmatter: %v", err)
		Expect(frontmatter).To(matchFrontmatterTimestamps(
			originalModTime.Format(time.RFC3339),
			originalModTime.Format(time.RFC3339),
		), "expected resumed migration to preserve v1 metadata via persisted legacy snapshot, got %#v", frontmatter)

	})
})

var _ = ginkgo.Describe("tree service behavior", ginkgo.Label("unit"), func() {
	ginkgo.It("load tree migrates to V3 backfills metadata frontmatter", func() {
		if CurrentSchemaVersion < 3 {
			ginkgo.Skip("requires schema v3+")
		}

		tmpDir := tempTreeDir()
		{

			err := saveSchema(tmpDir, 2)
			Expect(err).To(Succeed(), "saveSchema failed: %v",

				err)
		}

		svc := NewTreeService(tmpDir)
		{
			err := svc.LoadTree()
			Expect(err).To(Succeed(), "LoadTree failed: %v",

				err)
		}

		id, err := svc.CreateNode("system", nil, "Page1", "page1", ptrKind(NodeKindPage))
		Expect(err).To(Succeed(), "CreateNode failed: %v",

			err)

		node, err := svc.FindPageByID(*id)
		Expect(err).To(Succeed(), "FindPageByID failed: %v",

			err)

		node.Metadata = PageMetadata{
			CreatedAt:    time.Date(2026, time.March, 21, 10, 15, 30, 0, time.UTC),
			UpdatedAt:    time.Date(2026, time.March, 21, 11, 16, 31, 0, time.UTC),
			CreatorID:    "alice",
			LastAuthorID: "bob",
		}

		persistLegacyTreeSnapshot(tmpDir, svc.GetTree())

		pagePath := filepath.Join(tmpDir, "root", "page1.md")
		legacyContent := fmt.Sprintf("---\nleafwiki_id: %s\nleafwiki_title: Page1\n---\n# Page 1 Content\nHello World\n", *id)
		{
			err := os.WriteFile(pagePath, []byte(legacyContent), 0o644)
			Expect(err).To(Succeed(), "write legacy content failed: %v",

				err,
			)
		}
		{

			err := saveSchema(tmpDir, 2)
			Expect(err).To(Succeed(), "saveSchema failed: %v",

				err)
		}

		loaded := NewTreeService(tmpDir)
		{
			err := loaded.LoadTree()
			Expect(err).To(Succeed(), "LoadTree (migrating) failed: %v",

				err,
			)
		}

		raw, err := os.ReadFile(pagePath)
		Expect(err).To(Succeed(), "read migrated file: %v",

			err)

		frontmatter, migratedBody, err := parseRequiredFrontmatter(string(raw))
		Expect(err).To(Succeed(), "ParseFrontmatter: %v", err)
		Expect(frontmatter).To(SatisfyAll(
			matchFrontmatterTimestamps("2026-03-21T10:15:30Z", "2026-03-21T11:16:31Z"),
			matchFrontmatterAuthors(newFixtureUserID("alice"), newFixtureUserID("bob")),
		), "expected metadata to be backfilled, got %#v", frontmatter)

		wantBody := "# Page 1 Content\nHello World\n"
		Expect(migratedBody).
			To(Equal(
				wantBody,
			), "expected body preserved exactly.\nGot:\n%q\nWant:\n%q",

				migratedBody, wantBody)

	})
})

// --- F) Migration V2 (frontmatter backfill) ---
var _ = ginkgo.Describe("tree service behavior", ginkgo.Label("unit"), func() {
	ginkgo.It("load tree migrates to V2 adds frontmatter and preserves body", func() {
		if CurrentSchemaVersion < 2 {
			ginkgo.Skip("requires schema v2+")
		}

		tmpDir := tempTreeDir()
		{

			// start on v1 (or generally: current-1)
			err := saveSchema(tmpDir, 1)
			Expect(err).To(Succeed(), "saveSchema failed: %v",

				err)
		}

		svc := NewTreeService(tmpDir)
		{
			err := svc.LoadTree()
			Expect(err).To(Succeed(), "LoadTree failed: %v",

				err)
		}

		id, err := svc.CreateNode("system", nil, "Page1", "page1", ptrKind(NodeKindPage))
		Expect(err).To(Succeed(), "CreateNode failed: %v",

			err)

		// IMPORTANT: persist tree so the next service instance sees the node
		persistLegacyTreeSnapshot(tmpDir, svc.GetTree())

		// overwrite file without FM
		pagePath := filepath.Join(tmpDir, "root", "page1.md")
		body := "# Page 1 Content\nHello World\n"
		{
			err := os.WriteFile(pagePath, []byte(body), 0o644)
			Expect(err).To(Succeed(), "write old content failed: %v",

				err)
		}
		{

			// force schema old again
			err := saveSchema(tmpDir, 1)
			Expect(err).To(Succeed(), "saveSchema failed: %v",

				err)
		}

		loaded := NewTreeService(tmpDir)
		{
			err := loaded.LoadTree()
			Expect(err).To(Succeed(), "LoadTree (migrating) failed: %v",

				err,
			)
		}

		raw, err := os.ReadFile(pagePath)
		Expect(err).To(Succeed(), "read migrated file: %v",

			err)

		frontmatter, migratedBody, err := parseRequiredFrontmatter(string(raw))
		Expect(err).To(Succeed(), "ParseFrontmatter: %v", err)

		Expect(PageIDFromString(frontmatter.LeafWikiID)).To(Equal(*id))
		Expect(strings.TrimSpace(frontmatter.
			LeafWikiTitle,
		)).NotTo(BeEmpty(),

			"expected leafwiki_title to be set")
		Expect(migratedBody).
			To(Equal(
				body),
				"expected body preserved exactly.\nGot:\n%q\nWant:\n%q",

				migratedBody, body)

	})
})

var _ = ginkgo.Describe("tree service behavior", ginkgo.Label("unit"), func() {
	ginkgo.It("load tree migrates to V2 preserves existing custom frontmatter", func() {
		if CurrentSchemaVersion < 2 {
			ginkgo.Skip("requires schema v2+")
		}

		tmpDir := tempTreeDir()
		{

			err := saveSchema(tmpDir, 1)
			Expect(err).To(Succeed(), "saveSchema failed: %v",

				err)
		}

		svc := NewTreeService(tmpDir)
		{
			err := svc.LoadTree()
			Expect(err).To(Succeed(), "LoadTree failed: %v",

				err)
		}

		id, err := svc.CreateNode("system", nil, "Page1", "page1", ptrKind(NodeKindPage))
		Expect(err).To(Succeed(), "CreateNode failed: %v",

			err)

		persistLegacyTreeSnapshot(tmpDir, svc.GetTree())

		pagePath := filepath.Join(tmpDir, "root", "page1.md")
		legacyContent := `---
custom_key: keep-me
tags:
  - alpha
---
# Page 1 Content
Hello World
`
		{
			err := os.WriteFile(pagePath, []byte(legacyContent), 0o644)
			Expect(err).To(Succeed(), "write legacy content failed: %v",

				err,
			)
		}
		{

			err := saveSchema(tmpDir, 1)
			Expect(err).To(Succeed(), "saveSchema failed: %v",

				err)
		}

		loaded := NewTreeService(tmpDir)
		{
			err := loaded.LoadTree()
			Expect(err).To(Succeed(), "LoadTree (migrating) failed: %v",

				err,
			)
		}

		raw, err := os.ReadFile(pagePath)
		Expect(err).To(Succeed(), "read migrated file: %v",

			err)

		migrated := string(raw)
		Expect(migrated).To(ContainSubstring("custom_key: keep-me"),

			`expected custom frontmatter to be preserved, got:
%s`,

			migrated)
		Expect(migrated).To(ContainSubstring("- alpha"),

			`expected list frontmatter to be preserved, got:
%s`,

			migrated)

		frontmatter, migratedBody, err := parseRequiredFrontmatter(migrated)
		Expect(err).To(Succeed(), "ParseFrontmatter: %v", err)

		Expect(PageIDFromString(frontmatter.LeafWikiID)).To(Equal(*id))
		Expect(strings.TrimSpace(frontmatter.
			LeafWikiTitle,
		)).NotTo(BeEmpty(),

			"expected leafwiki_title to be set")

		wantBody := `# Page 1 Content
Hello World
`
		Expect(migratedBody).
			To(Equal(
				wantBody,
			), "expected body preserved exactly.\nGot:\n%q\nWant:\n%q",

				migratedBody, wantBody)

	})
})

var _ = ginkgo.Describe("tree service behavior", ginkgo.Label("unit"), func() {
	ginkgo.It("load tree migrates to V2 preserves existing LeafWiki title", func() {
		if CurrentSchemaVersion < 2 {
			ginkgo.Skip("requires schema v2+")
		}

		tmpDir := tempTreeDir()
		{

			err := saveSchema(tmpDir, 1)
			Expect(err).To(Succeed(), "saveSchema failed: %v",

				err)
		}

		svc := NewTreeService(tmpDir)
		{
			err := svc.LoadTree()
			Expect(err).To(Succeed(), "LoadTree failed: %v",

				err)
		}

		id, err := svc.CreateNode("system", nil, "Tree Title", "page1", ptrKind(NodeKindPage))
		Expect(err).To(Succeed(), "CreateNode failed: %v",

			err)

		persistLegacyTreeSnapshot(tmpDir, svc.GetTree())

		pagePath := filepath.Join(tmpDir, "root", "page1.md")
		legacyContent := `---
leafwiki_title: Existing Title
---
# Page 1 Content
Hello World
`
		{
			err := os.WriteFile(pagePath, []byte(legacyContent), 0o644)
			Expect(err).To(Succeed(), "write legacy content failed: %v",

				err,
			)
		}
		{

			err := saveSchema(tmpDir, 1)
			Expect(err).To(Succeed(), "saveSchema failed: %v",

				err)
		}

		loaded := NewTreeService(tmpDir)
		{
			err := loaded.LoadTree()
			Expect(err).To(Succeed(), "LoadTree (migrating) failed: %v",

				err,
			)
		}

		raw, err := os.ReadFile(pagePath)
		Expect(err).To(Succeed(), "read migrated file: %v",

			err)

		frontmatter, migratedBody, err := parseRequiredFrontmatter(string(raw))
		Expect(err).To(Succeed(), "ParseFrontmatter: %v", err)

		Expect(PageIDFromString(frontmatter.LeafWikiID)).To(Equal(*id))
		Expect(frontmatter.LeafWikiTitle).To(
			Equal(
				"Existing Title"),
			"expected existing leafwiki_title to be preserved, got %q",

			frontmatter.LeafWikiTitle,
		)

		wantBody := `# Page 1 Content
Hello World
`
		Expect(migratedBody).
			To(Equal(
				wantBody,
			), "expected body preserved exactly.\nGot:\n%q\nWant:\n%q",

				migratedBody, wantBody)

	})
})

var _ = ginkgo.Describe("tree service behavior", ginkgo.Label("unit"), func() {
	ginkgo.It("load tree migrates to V2 preserves title alias", func() {
		if CurrentSchemaVersion < 2 {
			ginkgo.Skip("requires schema v2+")
		}

		tmpDir := tempTreeDir()
		{

			err := saveSchema(tmpDir, 1)
			Expect(err).To(Succeed(), "saveSchema failed: %v",

				err)
		}

		svc := NewTreeService(tmpDir)
		{
			err := svc.LoadTree()
			Expect(err).To(Succeed(), "LoadTree failed: %v",

				err)
		}

		id, err := svc.CreateNode("system", nil, "Tree Title", "page1", ptrKind(NodeKindPage))
		Expect(err).To(Succeed(), "CreateNode failed: %v",

			err)

		persistLegacyTreeSnapshot(tmpDir, svc.GetTree())

		pagePath := filepath.Join(tmpDir, "root", "page1.md")
		legacyContent := `---
title: Alias Title
custom_key: keep-me
---
# Page 1 Content
Hello World
`
		{
			err := os.WriteFile(pagePath, []byte(legacyContent), 0o644)
			Expect(err).To(Succeed(), "write legacy content failed: %v",

				err,
			)
		}
		{

			err := saveSchema(tmpDir, 1)
			Expect(err).To(Succeed(), "saveSchema failed: %v",

				err)
		}

		loaded := NewTreeService(tmpDir)
		{
			err := loaded.LoadTree()
			Expect(err).To(Succeed(), "LoadTree (migrating) failed: %v",

				err,
			)
		}

		raw, err := os.ReadFile(pagePath)
		Expect(err).To(Succeed(), "read migrated file: %v",

			err)

		migrated := string(raw)
		Expect(migrated).To(ContainSubstring("title: Alias Title"),

			`expected title alias to be preserved, got:
%s`,

			migrated)
		Expect(migrated).To(ContainSubstring("custom_key: keep-me"),

			`expected custom frontmatter to be preserved, got:
%s`,

			migrated)

		frontmatter, migratedBody, err := parseRequiredFrontmatter(migrated)
		Expect(err).To(Succeed(), "ParseFrontmatter: %v", err)

		Expect(PageIDFromString(frontmatter.LeafWikiID)).To(Equal(*id))
		Expect(frontmatter.LeafWikiTitle).To(
			Equal(
				"Alias Title"), "expected title alias to remain effective, got %q",

			frontmatter.LeafWikiTitle)

		wantBody := `# Page 1 Content
Hello World
`
		Expect(migratedBody).
			To(Equal(
				wantBody,
			), "expected body preserved exactly.\nGot:\n%q\nWant:\n%q",

				migratedBody, wantBody)

	})
})

// TestTreeService_ReconstructTreeFromFS_UpdatesSchemaVersion verifies that
// ReconstructTreeFromFS writes the current schema version to prevent unnecessary migrations
var _ = ginkgo.Describe("tree service behavior", ginkgo.Label("unit"), func() {
	ginkgo.It("reconstruct tree from filesystem updates schema version", func() {
		tmpDir := tempTreeDir()

		// Create a minimal file structure for reconstruction
		createTreeDirectory(filepath.Join(tmpDir, "root"))
		writeTreeFile(filepath.Join(tmpDir, "root", "test.md"), "# Test Page", 0o644)

		// Create service WITHOUT schema.json (simulating an old/missing schema)
		svc := NewTreeService(tmpDir)
		{

			// Reconstruct the tree (no prior tree loaded)
			err := svc.ReconstructTreeFromFS()
			Expect(err).To(Succeed(), "ReconstructTreeFromFS failed: %v",

				err)
		}

		// Verify schema.json was created with current version
		schema, err := loadSchema(tmpDir)
		Expect(err).To(Succeed(), "loadSchema failed: %v",

			err)
		Expect(schema.Version).To(Equal(CurrentSchemaVersion), "expected schema version %d after reconstruction, got %d",

			CurrentSchemaVersion, schema.
				Version)

		// Startup reconstruction should no longer create a tree.json snapshot.
		Expect(filepath.Join(tmpDir, "tree.json")).To(beMissingTreePath())

	})
})

// --- G) ReconstructTreeFromFS ---

var _ = ginkgo.Describe("tree service behavior", ginkgo.Label("unit"), func() {
	ginkgo.It("reconstruct tree from filesystem backfills metadata", func() {
		svc, tmpDir := newLoadedService()

		// Create some files on disk manually (simulating external changes)
		writeTreeFile(filepath.Join(tmpDir, "root", "page1.md"), `---
leafwiki_id: page-1
leafwiki_title: Page One
---
# Page One`, 0o644)

		createTreeDirectory(filepath.Join(tmpDir, "root", "section1"))
		writeTreeFile(filepath.Join(tmpDir, "root", "section1", "index.md"), `---
leafwiki_id: sec-1
leafwiki_title: Section One
---
# Section One`, 0o644)

		writeTreeFile(filepath.Join(tmpDir, "root", "section1", "page2.md"), `---
leafwiki_id: page-2
leafwiki_title: Page Two
---
# Page Two`, 0o644)

		// Reconstruct the tree from filesystem
		err := svc.ReconstructTreeFromFS()
		Expect(err).To(Succeed(), "ReconstructTreeFromFS failed: %v",

			err)

		// Verify metadata was backfilled for all nodes
		tree := svc.GetTree()
		Expect(tree.Metadata).To(haveRecordedMetadataTimestamps(), "expected root metadata timestamps to be backfilled")

		// Check root metadata

		// Find and verify page1
		page1 := findChildBySlug(tree, "page1")
		Expect(page1.Metadata).To(haveRecordedMetadataTimestamps(), "expected page1 metadata timestamps to be backfilled")

		// Find and verify section1
		section1 := findChildBySlug(tree, "section1")
		Expect(section1.Metadata).To(haveRecordedMetadataTimestamps(), "expected section1 metadata timestamps to be backfilled")

		// Find and verify page2 (child of section1)
		page2 := findChildBySlug(section1, "page2")
		Expect(page2.Metadata).To(haveRecordedMetadataTimestamps(), "expected page2 metadata timestamps to be backfilled")

	})
})

var _ = ginkgo.Describe("tree service behavior", ginkgo.Label("unit"), func() {
	ginkgo.It("reconstruct tree from filesystem reloads from filesystem", func() {
		svc, tmpDir := newLoadedService()

		// Create some files on disk manually
		writeTreeFile(filepath.Join(tmpDir, "root", "readme.md"), `---
leafwiki_id: readme-page
leafwiki_title: README
---
# README`, 0o644)

		// Reconstruct the tree from filesystem
		err := svc.ReconstructTreeFromFS()
		Expect(err).To(Succeed(), "ReconstructTreeFromFS failed: %v",

			err)

		// Verify we can reload the tree directly from the filesystem.
		newSvc := NewTreeService(tmpDir)
		{
			err := newSvc.LoadTree()
			Expect(err).To(Succeed(), "LoadTree after reconstruction failed: %v",

				err,
			)
		}

		// Verify the tree structure matches
		tree := newSvc.GetTree()
		Expect(tree).To(SatisfyAll(Not(BeNil()), HaveField("ID", Equal(RootPageID))),
			"expected root node after reload, got: %+v", tree)

		// Verify the readme page exists
		readme := findChildBySlug(tree, "readme")
		Expect(readme).To(SatisfyAll(
			HaveField("ID", Equal(newFixturePageID("readme-page"))),
			HaveField("Title", Equal("README")),
		), "expected readme page metadata after reload, got %#v", readme)
		Expect(readme.Metadata).To(haveRecordedMetadataTimestamps(), "expected persisted metadata timestamps to not be zero")

		// Verify metadata was persisted

	})
})

var _ = ginkgo.Describe("tree service behavior", ginkgo.Label("unit"), func() {
	ginkgo.It("reconstruct tree from filesystem reloads metadata from frontmatter", func() {
		svc, tmpDir := newLoadedService()

		writeTreeFile(filepath.Join(tmpDir, "root", "readme.md"), `---
leafwiki_id: readme-page
leafwiki_title: README
leafwiki_created_at: 2026-03-21T10:15:30Z
leafwiki_updated_at: 2026-03-21T11:16:31Z
leafwiki_creator_id: alice
leafwiki_last_author_id: bob
---
# README`, 0o644)
		{

			err := svc.ReconstructTreeFromFS()
			Expect(err).To(Succeed(), "ReconstructTreeFromFS failed: %v",

				err)
		}

		newSvc := NewTreeService(tmpDir)
		{
			err := newSvc.LoadTree()
			Expect(err).To(Succeed(), "LoadTree after reconstruction failed: %v",

				err,
			)
		}

		readme := findChildBySlug(newSvc.GetTree(), "readme")
		{
			got := readme.Metadata.CreatedAt.UTC().Format(time.RFC3339)
			Expect(got).To(Equal("2026-03-21T10:15:30Z"), "expected persisted created_at from frontmatter, got %q",

				got)
		}
		{

			got := readme.Metadata.UpdatedAt.UTC().Format(time.RFC3339)
			Expect(got).To(Equal("2026-03-21T11:16:31Z"), "expected persisted updated_at from frontmatter, got %q",

				got)
		}
		Expect(readme.Metadata).To(matchPageMetadataAuthors(newFixtureUserID("alice"), newFixtureUserID("bob")),
			"expected persisted author metadata from frontmatter, got %#v", readme.Metadata)

	})
})

var _ = ginkgo.Describe("tree service behavior", ginkgo.Label("unit"), func() {
	ginkgo.It("reconstruct tree from filesystem reloads metadata fallbacks when missing", func() {
		svc, tmpDir := newLoadedService()

		readmePath := filepath.Join(tmpDir, "root", "readme.md")
		writeTreeFile(readmePath, `# README`, 0o644)

		wantTime := time.Date(2026, time.March, 21, 12, 34, 56, 0, time.UTC)
		{
			err := os.Chtimes(readmePath, wantTime, wantTime)
			Expect(err).To(Succeed(), "Chtimes: %v",

				err)
		}
		{

			err := svc.ReconstructTreeFromFS()
			Expect(err).To(Succeed(), "ReconstructTreeFromFS failed: %v",

				err)
		}

		newSvc := NewTreeService(tmpDir)
		{
			err := newSvc.LoadTree()
			Expect(err).To(Succeed(), "LoadTree after reconstruction failed: %v",

				err,
			)
		}

		readme := findChildBySlug(newSvc.GetTree(), "readme")
		Expect(strings.TrimSpace(readme.
			ID.String(),
		)).NotTo(BeEmpty(),

			"expected generated ID to persist")
		{

			got := readme.Metadata.CreatedAt.UTC().Format(time.RFC3339)
			Expect(got).To(Equal(wantTime.
				Format(
					time.RFC3339,
				)), "expected persisted created_at fallback from mtime, got %q",

				got)
		}
		{

			got := readme.Metadata.UpdatedAt.UTC().Format(time.RFC3339)
			Expect(got).To(Equal(wantTime.
				Format(
					time.RFC3339,
				)), "expected persisted updated_at fallback from mtime, got %q",

				got)
		}
		Expect(readme.Metadata).To(matchPageMetadataAuthors(reconstructSystemUserID, reconstructSystemUserID),
			"expected persisted system-user metadata fallback, got %#v", readme.Metadata)

	})
})

var _ = ginkgo.Describe("tree service behavior", ginkgo.Label("unit"), func() {
	ginkgo.It("reconstruct tree from filesystem complex tree preserves structure", func() {
		svc, tmpDir := newLoadedService()

		// Create a complex tree structure on disk
		writeTreeFile(filepath.Join(tmpDir, "root", "intro.md"), `---
leafwiki_id: intro
leafwiki_title: Introduction
---
# Introduction`, 0o644)

		createTreeDirectory(filepath.Join(tmpDir, "root", "docs"))
		writeTreeFile(filepath.Join(tmpDir, "root", "docs", "index.md"), `---
leafwiki_id: docs-section
leafwiki_title: Documentation
---
# Documentation`, 0o644)

		writeTreeFile(filepath.Join(tmpDir, "root", "docs", "getting-started.md"), `---
leafwiki_id: getting-started
leafwiki_title: Getting Started
---
# Getting Started`, 0o644)

		createTreeDirectory(filepath.Join(tmpDir, "root", "docs", "guides"))
		writeTreeFile(filepath.Join(tmpDir, "root", "docs", "guides", "index.md"), `---
leafwiki_id: guides-section
leafwiki_title: Guides
---
# Guides`, 0o644)

		writeTreeFile(filepath.Join(tmpDir, "root", "docs", "guides", "basic.md"), `---
leafwiki_id: basic-guide
leafwiki_title: Basic Guide
---
# Basic Guide`, 0o644)

		// Reconstruct
		err := svc.ReconstructTreeFromFS()
		Expect(err).To(Succeed(), "ReconstructTreeFromFS failed: %v",

			err)

		tree := svc.GetTree()

		// Verify structure
		intro := findChildBySlug(tree, "intro")
		Expect(intro).To(matchReconstructedNode(NodeKindPage, nil), "expected intro to be reconstructed as a page with metadata")

		docs := findChildBySlug(tree, "docs")
		Expect(docs).To(matchReconstructedNode(NodeKindSection, Equal(newFixturePageID("docs-section"))),
			"expected docs to reload as the frontmatter-backed section with metadata")

		gettingStarted := findChildBySlug(docs, "getting-started")
		Expect(gettingStarted).To(matchReconstructedNode(NodeKindPage, nil),
			"expected getting-started to be reconstructed as a page with metadata")

		guides := findChildBySlug(docs, "guides")
		Expect(guides).To(matchReconstructedNode(NodeKindSection, nil),
			"expected guides to be reconstructed as a section with metadata")

		basic := findChildBySlug(guides, "basic")
		Expect(basic).To(matchReconstructedNode(NodeKindPage, nil),
			"expected basic to be reconstructed as a page with metadata")

		// Verify all nodes have metadata

		Expect(filepath.Join(tmpDir, "tree.json")).To(beMissingTreePath())

		reloadedSvc := NewTreeService(tmpDir)
		{
			err := reloadedSvc.LoadTree()
			Expect(err).To(Succeed(), "LoadTree after reconstruction failed: %v",

				err,
			)
		}

		reloadedTree := reloadedSvc.GetTree()
		Expect(reloadedTree.
			Children).
			To(HaveLen(len(tree.Children)),
				"expected reloaded tree to have same number of children",
			)

	})
})

var _ = ginkgo.Describe("tree service behavior", ginkgo.Label("unit"), func() {
	ginkgo.It("reconstruct tree from filesystem empty directory creates root and persists", func() {
		svc, tmpDir := newLoadedService()

		// Reconstruct from empty directory (should create just root)
		err := svc.ReconstructTreeFromFS()
		Expect(err).To(Succeed(), "ReconstructTreeFromFS failed: %v",

			err)

		tree := svc.GetTree()
		Expect(tree).To(SatisfyAll(Not(BeNil()), HaveField("ID", Equal(RootPageID))),
			"expected root node, got: %+v", tree)

		// Note: Root metadata may not be backfilled from filesystem when directory is empty
		// because there's no corresponding file/directory to stat. This is expected behavior.
		// The important thing is that the tree is reconstructed and persisted.

		Expect(filepath.Join(tmpDir, "tree.json")).To(beMissingTreePath())

		// Verify we can reload
		reloadedSvc := NewTreeService(tmpDir)
		{
			err := reloadedSvc.LoadTree()
			Expect(err).To(Succeed(), "LoadTree after reconstruction failed: %v",

				err,
			)
		}

		reloadedTree := reloadedSvc.GetTree()
		Expect(reloadedTree).To(SatisfyAll(Not(BeNil()), HaveField("ID", Equal(RootPageID))),
			"expected root node after reload")

	})
})

var _ = ginkgo.Describe("tree service behavior", ginkgo.Label("unit"), func() {
	ginkgo.It("reconstruct tree from filesystem reverts on metadata backfill error", func() {
		// This test is harder to trigger without mocking, but we can at least verify
		// that if the tree state is preserved if we can cause a failure scenario.
		// For now, we'll test that a successful reconstruction doesn't lose the old tree.
		svc, tmpDir := newLoadedService()

		// Create initial tree state
		initialID, err := svc.CreateNode("system", nil, "Initial", "initial", ptrKind(NodeKindPage))
		Expect(err).To(Succeed(), "CreateNode failed: %v",

			err)

		// Get initial tree
		initialTree := svc.GetTree()
		Expect(initialTree.Children).To(HaveLen(1),
			"expected 1 child in initial tree",
		)

		// Create a new file on disk
		writeTreeFile(filepath.Join(tmpDir, "root", "new-page.md"), `---
leafwiki_id: new-page
leafwiki_title: New Page
---
# New Page`, 0o644)

		// Reconstruct should succeed
		err = svc.ReconstructTreeFromFS()
		Expect(err).To(Succeed(), "ReconstructTreeFromFS failed: %v",

			err)

		// Verify new tree has both nodes
		newTree := svc.GetTree()
		Expect(newTree.Children).To(HaveLen(2), "expected 2 children after reconstruction, got %d",

			len(newTree.Children))

		// Verify initial node still exists
		var foundInitial bool
		for _, child := range newTree.Children {
			if child.ID == *initialID {
				foundInitial = true
				break
			}
		}
		Expect(foundInitial).
			To(BeTrue(), "expected initial node to still exist after reconstruction")

	})
})

// --- small util ---

func ptrKind(k NodeKind) *NodeKind { return &k }

var _ = ginkgo.Describe("tree service behavior", ginkgo.Label("unit"), func() {
	ginkgo.It("load tree migrates to V5 returns an error when order file cannot be written", func() {
		if CurrentSchemaVersion < 5 {
			ginkgo.Skip("requires schema v5+")
		}
		if runtime.GOOS == "windows" {
			ginkgo.Skip("permission-based migration failure test is not reliable on Windows")
		}

		tmpDir := tempTreeDir()
		{

			err := saveSchema(tmpDir, CurrentSchemaVersion)
			Expect(err).To(Succeed(), "saveSchema failed: %v",

				err)
		}

		svc := NewTreeService(tmpDir)
		{
			err := svc.LoadTree()
			Expect(err).To(Succeed(), "LoadTree failed: %v",

				err)
		}

		_, err := svc.CreateNode("system", nil, "Docs", "docs", ptrKind(NodeKindSection))
		Expect(err).To(Succeed(), "CreateNode failed: %v",

			err)

		_, err = svc.CreateNode("system", nil, "Alpha", "alpha", ptrKind(NodeKindPage))
		Expect(err).To(Succeed(), "CreateNode alpha failed: %v",

			err)
		{

			err := os.Remove(filepath.Join(tmpDir, "root", ".order.json"))
			Expect(err).To(SatisfyAny(Succeed(), matchErrorIs(os.ErrNotExist)), "remove root order file failed: %v", err)
		}

		createTreeDirectory(filepath.Join(tmpDir, "root", ".order.json"))
		{

			err := saveSchema(tmpDir, 4)
			Expect(err).To(Succeed(), "saveSchema failed: %v",

				err)
		}

		loaded := NewTreeService(tmpDir)
		err = loaded.LoadTree()
		Expect(err).To(MatchError(treemigration.
			ErrPersistChildOrder,
		), "expected migration child order persistence error, got: %v",

			err)

	})
})

var _ = ginkgo.Describe("tree service behavior", ginkgo.Label("unit"), func() {
	ginkgo.It("load tree migrates to V4 returns an error when section index cannot be written", func() {
		if CurrentSchemaVersion < 4 {
			ginkgo.Skip("requires schema v4+")
		}
		if runtime.GOOS == "windows" {
			ginkgo.Skip("permission-based migration failure test is not reliable on Windows")
		}

		tmpDir := tempTreeDir()
		{

			err := saveSchema(tmpDir, 3)
			Expect(err).To(Succeed(), "saveSchema failed: %v",

				err)
		}

		svc := NewTreeService(tmpDir)
		{
			err := svc.LoadTree()
			Expect(err).To(Succeed(), "LoadTree failed: %v",

				err)
		}

		id, err := svc.CreateNode("system", nil, "Docs", "docs", ptrKind(NodeKindSection))
		Expect(err).To(Succeed(), "CreateNode failed: %v",

			err)

		node, err := svc.FindPageByID(*id)
		Expect(err).To(Succeed(), "FindPageByID failed: %v",

			err)

		node.Metadata = PageMetadata{
			CreatedAt:    time.Date(2026, time.March, 22, 10, 15, 30, 0, time.UTC),
			UpdatedAt:    time.Date(2026, time.March, 22, 11, 16, 31, 0, time.UTC),
			CreatorID:    "alice",
			LastAuthorID: "bob",
		}

		persistLegacyTreeSnapshot(tmpDir, svc.GetTree())

		sectionDir := filepath.Join(tmpDir, "root", "docs")
		indexPath := filepath.Join(sectionDir, "index.md")
		{
			err := os.Remove(indexPath)
			Expect(err).To(Succeed(), "remove section index failed: %v",

				err,
			)
		}
		{

			err := os.Chmod(sectionDir, 0o555)
			Expect(err).To(Succeed(), "chmod section directory failed: %v",

				err)
		}

		ginkgo.DeferCleanup(os.Chmod, sectionDir, os.FileMode(0o755))
		{

			err := saveSchema(tmpDir, 3)
			Expect(err).To(Succeed(), "saveSchema failed: %v",

				err)
		}

		loaded := NewTreeService(tmpDir)
		err = loaded.LoadTree()
		Expect(err).To(MatchError(treemigration.
			ErrMaterializeSectionIndex,
		), "expected migration section index materialization error, got: %v",

			err)

	})
})

// ─── IsLoaded ─────────────────────────────────────────────────────────────────

var _ = ginkgo.Describe("tree service behavior", ginkgo.Label("unit"), func() {
	ginkgo.It("reports an unloaded tree before LoadTree runs", func() {
		svc := NewTreeService(tempTreeDir())
		Expect(svc).To(haveTreeServiceLoadState(treeServiceNotLoaded))

	})
})

var _ = ginkgo.Describe("tree service behavior", ginkgo.Label("unit"), func() {
	ginkgo.It("reports a loaded tree after LoadTree succeeds", func() {
		tmpDir := tempTreeDir()
		{
			err := saveSchema(tmpDir, CurrentSchemaVersion)
			Expect(err).To(Succeed(), "saveSchema failed: %v",

				err)
		}

		svc := NewTreeService(tmpDir)
		{
			err := svc.LoadTree()
			Expect(err).To(Succeed(), "LoadTree failed: %v",

				err)
		}
		Expect(svc).To(haveTreeServiceLoadState(treeServiceLoaded))

	})
})

// ─── HasPages ─────────────────────────────────────────────────────────────────

var _ = ginkgo.Describe("tree service behavior", ginkgo.Label("unit"), func() {
	ginkgo.It("reports page content as unavailable before LoadTree runs", func() {
		svc := NewTreeService(tempTreeDir())
		Expect(svc).To(haveTreeServicePageState(treeServicePagesUnavailable))

	})
})

var _ = ginkgo.Describe("tree service behavior", ginkgo.Label("unit"), func() {
	ginkgo.It("reports no page content for an empty loaded tree", func() {
		tmpDir := tempTreeDir()
		{
			err := saveSchema(tmpDir, CurrentSchemaVersion)
			Expect(err).To(Succeed(), "saveSchema failed: %v",

				err)
		}

		svc := NewTreeService(tmpDir)
		{
			err := svc.LoadTree()
			Expect(err).To(Succeed(), "LoadTree failed: %v",

				err)
		}
		Expect(svc).To(haveTreeServicePageState(treeServicePagesEmpty))

	})
})

var _ = ginkgo.Describe("tree service behavior", ginkgo.Label("unit"), func() {
	ginkgo.It("reports page content after creating a page", func() {
		tmpDir := tempTreeDir()
		{
			err := saveSchema(tmpDir, CurrentSchemaVersion)
			Expect(err).To(Succeed(), "saveSchema failed: %v",

				err)
		}

		svc := NewTreeService(tmpDir)
		{
			err := svc.LoadTree()
			Expect(err).To(Succeed(), "LoadTree failed: %v",

				err)
		}
		{

			_, err := svc.CreateNode("user1", nil, "Test", "test", ptrKind(NodeKindPage))
			Expect(err).To(Succeed(), "CreateNode failed: %v",

				err)
		}
		Expect(svc).To(haveTreeServicePageState(treeServicePagesPresent))

	})
})

// ─── WalkNodes ────────────────────────────────────────────────────────────────

var _ = ginkgo.Describe("tree service behavior", ginkgo.Label("unit"), func() {
	ginkgo.It("walk nodes does nothing when not loaded", func() {
		svc := NewTreeService(tempTreeDir())
		var visitedIDs []PageID
		err := svc.WalkNodes(func(_ PageID) error {
			visitedIDs = append(visitedIDs, RootPageID)
			return nil
		})
		Expect(err).To(Succeed(), "expected no error, got: %v",

			err)
		Expect(visitedIDs).To(BeEmpty(), "expected fn not to be called when tree is not loaded")

	})
})

var _ = ginkgo.Describe("tree service behavior", ginkgo.Label("unit"), func() {
	ginkgo.It("walk nodes visits all non root nodes", func() {
		tmpDir := tempTreeDir()
		{
			err := saveSchema(tmpDir, CurrentSchemaVersion)
			Expect(err).To(Succeed(), "saveSchema failed: %v",

				err)
		}

		svc := NewTreeService(tmpDir)
		{
			err := svc.LoadTree()
			Expect(err).To(Succeed(), "LoadTree failed: %v",

				err)
		}
		{

			_, err := svc.CreateNode("u", nil, "A", "a", ptrKind(NodeKindPage))
			Expect(err).To(Succeed(), "CreateNode A: %v",

				err)
		}
		{

			_, err := svc.CreateNode("u", nil, "B", "b", ptrKind(NodeKindPage))
			Expect(err).To(Succeed(), "CreateNode B: %v",

				err)
		}

		var visited []string
		{
			err := svc.WalkNodes(func(id PageID) error {
				page, err := svc.GetPage(id)
				if err != nil {
					return err
				}
				visited = append(visited, page.Slug.String())
				return nil
			})
			Expect(err).To(Succeed(), "WalkNodes failed: %v",

				err)
		}
		Expect(visited).To(HaveLen(2),
			"expected 2 visited nodes, got %d: %v",

			len(visited), visited,
		)

		for _, s := range []string{"a", "b"} {
			Expect(visited).To(ContainElement(s), "expected slug %q to be visited, got: %v", s, visited)

		}

	})
})

var _ = ginkgo.Describe("tree service behavior", ginkgo.Label("unit"), func() {
	ginkgo.It("walk nodes skips root node", func() {
		tmpDir := tempTreeDir()
		{
			err := saveSchema(tmpDir, CurrentSchemaVersion)
			Expect(err).To(Succeed(), "saveSchema failed: %v",

				err)
		}

		svc := NewTreeService(tmpDir)
		{
			err := svc.LoadTree()
			Expect(err).To(Succeed(), "LoadTree failed: %v",

				err)
		}
		{

			err := svc.WalkNodes(func(id PageID) error {
				if id == "root" {
					return errors.New("root node must not be visited")
				}
				return nil
			})
			Expect(err).To(Succeed(), err)
		}

	})
})

var _ = ginkgo.Describe("tree service behavior", ginkgo.Label("unit"), func() {
	ginkgo.It("walk nodes stops on error", func() {
		tmpDir := tempTreeDir()
		{
			err := saveSchema(tmpDir, CurrentSchemaVersion)
			Expect(err).To(Succeed(), "saveSchema failed: %v",

				err)
		}

		svc := NewTreeService(tmpDir)
		{
			err := svc.LoadTree()
			Expect(err).To(Succeed(), "LoadTree failed: %v",

				err)
		}

		for _, title := range []string{"A", "B", "C"} {
			{
				_, err := svc.CreateNode("u", nil, title, SlugFromString(strings.ToLower(title)), ptrKind(NodeKindPage))
				Expect(err).To(Succeed(), "CreateNode %s: %v",

					title, err)
			}

		}

		sentinel := errors.New("stop")
		calls := 0
		err := svc.WalkNodes(func(_ PageID) error {
			calls++
			return sentinel
		})
		Expect(err).To(MatchError(sentinel),
			"expected sentinel error, got: %v",

			err)
		Expect(calls).To(Equal(1), "expected fn called once before stop, got %d",

			calls)

	})
})

var _ = ginkgo.Describe("tree service behavior", ginkgo.Label("unit"), func() {
	ginkgo.It("walk nodes visits nested nodes", func() {
		tmpDir := tempTreeDir()
		{
			err := saveSchema(tmpDir, CurrentSchemaVersion)
			Expect(err).To(Succeed(), "saveSchema failed: %v",

				err)
		}

		svc := NewTreeService(tmpDir)
		{
			err := svc.LoadTree()
			Expect(err).To(Succeed(), "LoadTree failed: %v",

				err)
		}

		parentID, err := svc.CreateNode("u", nil, "Parent", "parent", ptrKind(NodeKindSection))
		Expect(err).To(Succeed(), "CreateNode parent: %v",

			err)
		{

			_, err := svc.CreateNode("u", parentID, "Child", "child", ptrKind(NodeKindPage))
			Expect(err).To(Succeed(), "CreateNode child: %v",

				err)
		}

		var visited []string
		{
			err := svc.WalkNodes(func(id PageID) error {
				page, err := svc.GetPage(id)
				if err != nil {
					return err
				}
				visited = append(visited, page.Slug.String())
				return nil
			})
			Expect(err).To(Succeed(), "WalkNodes failed: %v",

				err)
		}
		Expect(visited).To(HaveLen(2),
			"expected 2 visited nodes (parent + child), got %d: %v",

			len(visited), visited)

	})
})

var _ = ginkgo.Describe("tree service behavior", ginkgo.Label("unit"), func() {
	ginkgo.It("get pages preserves order and aligns errors", func() {
		svc, _ := newLoadedService()

		firstID, err := svc.CreateNode("system", nil, "First", "first", ptrKind(NodeKindPage))
		Expect(err).To(Succeed(), "CreateNode(first) failed: %v",

			err)

		secondID, err := svc.CreateNode("system", nil, "Second", "second", ptrKind(NodeKindPage))
		Expect(err).To(Succeed(), "CreateNode(second) failed: %v",

			err,
		)

		pages, errs := svc.GetPages([]PageID{*secondID, PageID("missing-id"), *firstID})
		Expect(pages).To(HaveLen(3), "unexpected page result length")
		Expect(errs).To(HaveLen(3), "unexpected error result length")
		pageResults := make([]pageLookupResult, 0, len(pages))
		for i := range pages {
			pageResults = append(pageResults, pageLookupResult{Page: pages[i], Err: errs[i]})
		}
		Expect(pageResults).To(HaveExactElements(
			SatisfyAll(
				HaveField("Page", pointToValue[Page](HaveField("ID", Equal(*secondID)))),
				HaveField("Err", Succeed()),
			),
			SatisfyAll(
				HaveField("Page", BeNil()),
				HaveField("Err", MatchError(ErrPageNotFound)),
			),
			SatisfyAll(
				HaveField("Page", pointToValue[Page](HaveField("ID", Equal(*firstID)))),
				HaveField("Err", Succeed()),
			),
		))

	})
})

var _ = ginkgo.Describe("tree service behavior", ginkgo.Label("unit"), func() {
	ginkgo.It("bulk update content treats frontmatter like input as body", func() {
		svc, _ := newLoadedService()

		firstID, err := svc.CreateNode("system", nil, "First", "first", ptrKind(NodeKindPage))
		Expect(err).To(Succeed(), "CreateNode(first) failed: %v",

			err)

		secondID, err := svc.CreateNode("system", nil, "Second", "second", ptrKind(NodeKindPage))
		Expect(err).To(Succeed(), "CreateNode(second) failed: %v",

			err,
		)

		beforeFirst, err := svc.GetPage(*firstID)
		Expect(err).To(Succeed(), "GetPage(first before) failed: %v",

			err)

		// Content that looks like invalid YAML frontmatter is now stored as plain
		// body text — UpsertContent no longer parses frontmatter from UI content.
		errs := svc.BulkUpdateContent("bulk-user", []BulkContentUpdate{
			{ID: *firstID, Content: "updated first"},
			{ID: *secondID, Content: "---\ninvalid: [\n---\nbody"},
			{ID: "missing-id", Content: "ignored"},
		})
		Expect(errs).To(HaveExactElements(
			Succeed(),
			Succeed(),
			MatchError(ErrPageNotFound),
		))

		// Index 1 now succeeds: frontmatter-like content is treated as plain body.

		afterFirst, err := svc.GetPage(*firstID)
		Expect(err).To(Succeed(), "GetPage(first after) failed: %v",

			err,
		)
		Expect(afterFirst).To(SatisfyAll(
			HaveField("Content", Equal("updated first")),
			HaveField("Metadata", SatisfyAll(
				HaveField("LastAuthorID", Equal(newFixtureUserID("bulk-user"))),
				HaveField("UpdatedAt", Or(
					BeTemporally(">", beforeFirst.Metadata.UpdatedAt),
					BeTemporally("==", beforeFirst.Metadata.UpdatedAt),
				)),
			)),
		), "expected first page content and metadata to reflect the bulk update")

		afterSecond, err := svc.GetPage(*secondID)
		Expect(err).To(Succeed(), "GetPage(second after) failed: %v",

			err)
		Expect(afterSecond.
			Content,
		).
			To(ContainSubstring("invalid: ["),

				"expected second content to contain plain body text, got %q",

				afterSecond.
					Content)

		// The "invalid YAML" block is now stored verbatim as body content.

	})
})

// ─────────────────────────────────────────────────────────────────────────────
// Optimistic locking: version check is enforced inside the write lock
// ─────────────────────────────────────────────────────────────────────────────

var _ = ginkgo.Describe("tree service behavior", ginkgo.Label("unit"), func() {
	ginkgo.It("update node stale version returns err version conflict", func() {
		svc, _ := newLoadedService()
		id, _ := svc.CreateNode("system", nil, "Page", "page", ptrKind(NodeKindPage))

		node, _ := svc.FindPageByID(*id)
		currentVersion := node.Version()
		{

			// First update succeeds — advances the version.
			err := svc.UpdateNode(newFixtureUserID("system"), *id, "Page v2", Slug("page"), nil, PageVersionFromString(currentVersion), false)
			Expect(err).To(Succeed(), "first UpdateNode failed: %v",

				err)
		}

		// Second update with the same (now stale) version must fail.
		err := svc.UpdateNode(newFixtureUserID("system"), *id, "Page v3", Slug("page"), nil, PageVersionFromString(currentVersion), false)
		Expect(err).To(MatchError(ErrVersionConflict), "expected ErrVersionConflict, got %v",

			err)

	})
})

var _ = ginkgo.Describe("tree service behavior", ginkgo.Label("unit"), func() {
	ginkgo.It("update node missing version returns err version required", func() {
		svc, _ := newLoadedService()
		id, _ := svc.CreateNode("system", nil, "Page", "page", ptrKind(NodeKindPage))

		err := svc.UpdateNode(newFixtureUserID("system"), *id, "Page v2", Slug("page"), nil, PageVersion(""), false)
		Expect(err).To(MatchError(ErrVersionRequired), "expected ErrVersionRequired, got %v",

			err)

	})
})

var _ = ginkgo.Describe("tree service behavior", ginkgo.Label("unit"), func() {
	ginkgo.It("delete node stale version returns err version conflict", func() {
		svc, _ := newLoadedService()
		id, _ := svc.CreateNode("system", nil, "Page", "page", ptrKind(NodeKindPage))

		node, _ := svc.FindPageByID(*id)
		staleVersion := node.Version()
		{

			// Advance the version via an update.
			err := svc.UpdateNode(newFixtureUserID("system"), *id, "Page v2", Slug("page"), nil, PageVersionFromString(staleVersion), false)
			Expect(err).To(Succeed(), "UpdateNode failed: %v",

				err)
		}

		err := svc.DeleteNode("system", *id, false, PageVersionFromString(staleVersion))
		Expect(err).To(MatchError(ErrVersionConflict), "expected ErrVersionConflict, got %v",

			err)

	})
})

var _ = ginkgo.Describe("tree service behavior", ginkgo.Label("unit"), func() {
	ginkgo.It("delete node missing version returns err version required", func() {
		svc, _ := newLoadedService()
		id, _ := svc.CreateNode("system", nil, "Page", "page", ptrKind(NodeKindPage))

		err := svc.DeleteNode("system", *id, false, "")
		Expect(err).To(MatchError(ErrVersionRequired), "expected ErrVersionRequired, got %v",

			err)

	})
})

var _ = ginkgo.Describe("tree service behavior", ginkgo.Label("unit"), func() {
	ginkgo.It("move node stale version returns err version conflict", func() {
		svc, _ := newLoadedService()
		destID, _ := svc.CreateNode("system", nil, "Dest", "dest", ptrKind(NodeKindPage))
		moveID, _ := svc.CreateNode("system", nil, "Move", "move", ptrKind(NodeKindPage))

		node, _ := svc.FindPageByID(*moveID)
		staleVersion := node.Version()
		{

			// Advance the version.
			err := svc.UpdateNode(newFixtureUserID("system"), *moveID, "Move v2", Slug("move"), nil, PageVersionFromString(staleVersion), false)
			Expect(err).To(Succeed(), "UpdateNode failed: %v",

				err)
		}

		err := svc.MoveNode("system", *moveID, *destID, PageVersionFromString(staleVersion))
		Expect(err).To(MatchError(ErrVersionConflict), "expected ErrVersionConflict, got %v",

			err)

	})
})

var _ = ginkgo.Describe("tree service behavior", ginkgo.Label("unit"), func() {
	ginkgo.It("move node missing version returns err version required", func() {
		svc, _ := newLoadedService()
		destID, _ := svc.CreateNode("system", nil, "Dest", "dest", ptrKind(NodeKindPage))
		moveID, _ := svc.CreateNode("system", nil, "Move", "move", ptrKind(NodeKindPage))

		err := svc.MoveNode("system", *moveID, *destID, "")
		Expect(err).To(MatchError(ErrVersionRequired), "expected ErrVersionRequired, got %v",

			err)

	})
})

var _ = ginkgo.Describe("tree service behavior", ginkgo.Label("unit"), func() {
	ginkgo.It("convert node stale version returns err version conflict", func() {
		svc, _ := newLoadedService()
		id, _ := svc.CreateNode("system", nil, "Page", "page", ptrKind(NodeKindPage))

		node, _ := svc.FindPageByID(*id)
		staleVersion := node.Version()
		{

			// Advance the version.
			err := svc.UpdateNode(newFixtureUserID("system"), *id, "Page v2", Slug("page"), nil, PageVersionFromString(staleVersion), false)
			Expect(err).To(Succeed(), "UpdateNode failed: %v",

				err)
		}

		err := svc.ConvertNode("system", *id, NodeKindSection, PageVersionFromString(staleVersion))
		Expect(err).To(MatchError(ErrVersionConflict), "expected ErrVersionConflict, got %v",

			err)

	})
})

var _ = ginkgo.Describe("tree service behavior", ginkgo.Label("unit"), func() {
	ginkgo.It("convert node missing version returns err version required", func() {
		svc, _ := newLoadedService()
		id, _ := svc.CreateNode("system", nil, "Page", "page", ptrKind(NodeKindPage))

		err := svc.ConvertNode("system", *id, NodeKindSection, "")
		Expect(err).To(MatchError(ErrVersionRequired), "expected ErrVersionRequired, got %v",

			err)

	})
})

var _ = ginkgo.Describe("tree service behavior", ginkgo.Label("unit"), func() {
	ginkgo.It("version unchecked bypasses version check", func() {
		svc, _ := newLoadedService()
		id, _ := svc.CreateNode("system", nil, "Page", "page", ptrKind(NodeKindPage))
		{

			// The tree-owned unchecked operation must always succeed regardless of actual node version.
			err := svc.UpdateNode(newFixtureUserID("system"), *id, "Page v2", Slug("page"), nil, pageVersionUnchecked, false)
			Expect(err).To(Succeed(), "expected unchecked operation to bypass check, got: %v",

				err,
			)
		}
		{

			err := svc.UpdateNode(newFixtureUserID("system"), *id, "Page v3", Slug("page"), nil, pageVersionUnchecked, false)
			Expect(err).To(Succeed(), "expected unchecked operation to bypass check on second call, got: %v",

				err)
		}

	})
})

// ─── RawContent ───────────────────────────────────────────────────────────────

var _ = ginkgo.Describe("tree service behavior", ginkgo.Label("unit"), func() {
	ginkgo.It("get page raw content contains canonical metadata and body", func() {
		svc, _ := newLoadedService()

		id, err := svc.CreateNode("system", nil, "Raw Test", "raw-test", ptrKind(NodeKindPage))
		Expect(err).To(Succeed(), "CreateNode: %v",

			err,
		)

		body := "Hello raw world"
		page, err := svc.GetPage(*id)
		Expect(err).To(Succeed(), "GetPage before update: %v",

			err)
		{

			err := svc.UpdateNode(newFixtureUserID("system"), *id, "Raw Test", Slug("raw-test"), &body, PageVersionFromString(page.Version()), false)
			Expect(err).To(Succeed(), "UpdateNode: %v",

				err,
			)
		}

		page, err = svc.GetPage(*id)
		Expect(err).To(Succeed(), "GetPage: %v",

			err)
		Expect(page).To(SatisfyAll(
			HaveField("RawContent", SatisfyAll(
				Not(BeEmpty()),
				ContainSubstring("<!-- leafwiki\n"),
				ContainSubstring("Hello raw world"),
			)),
			HaveField("Content", SatisfyAll(
				WithTransform(strings.TrimSpace, Not(HavePrefix("<!-- leafwiki"))),
				Not(Equal(page.RawContent)),
			)),
		), "expected page to expose body content separately from raw storage, got %#v", page)

	})
})

var _ = ginkgo.Describe("tree service behavior", ginkgo.Label("unit"), func() {
	ginkgo.It("get pages raw content populated for all", func() {
		svc, _ := newLoadedService()

		id1, err := svc.CreateNode("system", nil, "Page One", "page-one", ptrKind(NodeKindPage))
		Expect(err).To(Succeed(), "CreateNode 1: %v",

			err)

		id2, err := svc.CreateNode("system", nil, "Page Two", "page-two", ptrKind(NodeKindPage))
		Expect(err).To(Succeed(), "CreateNode 2: %v",

			err)

		pages, errs := svc.GetPages([]PageID{*id1, *id2})
		Expect(errs).To(HaveEach(Succeed()))
		for i, p := range pages {
			Expect(p.RawContent).
				NotTo(BeEmpty(),

					"GetPages[%d]: expected RawContent to be populated",

					i)
			Expect(p.RawContent).To(ContainSubstring("<!-- leafwiki\n"),

				"GetPages[%d]: expected RawContent to contain canonical metadata, got: %q",

				i, p.RawContent)

		}

	})
})

var _ = ginkgo.Describe("tree service behavior", ginkgo.Label("unit"), func() {
	ginkgo.It("get page raw content not serialized to JSON", func() {
		svc, _ := newLoadedService()

		id, err := svc.CreateNode("system", nil, "JSON Test", "json-test", ptrKind(NodeKindPage))
		Expect(err).To(Succeed(), "CreateNode: %v",

			err,
		)

		page, err := svc.GetPage(*id)
		Expect(err).To(Succeed(), "GetPage: %v",

			err)

		data, err := json.Marshal(page)
		Expect(err).To(Succeed(), "json.Marshal: %v",

			err)

		s := string(data)
		Expect(s).NotTo(SatisfyAny(ContainSubstring("rawContent"), ContainSubstring("raw_content")),
			"RawContent must not appear in JSON output, got: %s", s)

	})
})
