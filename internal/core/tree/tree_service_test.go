package tree

import (
	"encoding/json"
	"os"
	"path/filepath"

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
