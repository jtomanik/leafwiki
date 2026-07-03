package treemigration_test

import (
	"encoding/json"
	"errors"
	"fmt"
	ginkgo "github.com/onsi/ginkgo/v2"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/gcustom"
	"github.com/onsi/gomega/gstruct"
	"github.com/onsi/gomega/types"
	"github.com/perber/wiki/internal/core/markdown"
	"github.com/perber/wiki/internal/core/tree"
	"github.com/perber/wiki/internal/core/treemigration"
)

func writeSchema(dir string, version int) {
	ginkgo.GinkgoHelper()

	raw, err := json.MarshalIndent(struct {
		Version int `json:"version"`
	}{Version: version}, "", "  ")
	Expect(err).NotTo(HaveOccurred())

	Expect(os.WriteFile(filepath.Join(dir, "schema.json"), raw, 0o644)).To(Succeed())
}

func ptrKind(kind tree.NodeKind) *tree.NodeKind { return &kind }

func persistLegacyTreeSnapshot(storageDir string, root *tree.PageNode) {
	ginkgo.GinkgoHelper()

	raw, err := json.Marshal(root)
	Expect(err).NotTo(HaveOccurred())
	Expect(os.WriteFile(filepath.Join(storageDir, "tree.json"), raw, 0o644)).To(Succeed())
}

func tempMigrationDir() string {
	ginkgo.GinkgoHelper()

	path, err := os.MkdirTemp("", "leafwiki-treemigration-*")
	Expect(err).NotTo(HaveOccurred())
	ginkgo.DeferCleanup(os.RemoveAll, path)
	return path
}

func removeIfPresent(path string) error {
	ginkgo.GinkgoHelper()

	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func skipUnlessSchemaVersionAtLeast(version int) {
	ginkgo.GinkgoHelper()

	if tree.CurrentSchemaVersion < version {
		ginkgo.Skip(fmt.Sprintf("requires schema v%d+", version))
	}
}

func matchMigrationError(want error) types.GomegaMatcher {
	return gcustom.MakeMatcher(func(actual error) (bool, error) {
		return errors.Is(actual, want), nil
	}).WithTemplate("Expected:\n{{.FormattedActual}}\n{{.To}} wrap migration error\n{{format .Data 1}}", want)
}

func matchOrderIDs(want ...tree.PageID) types.GomegaMatcher {
	return gcustom.MakeMatcher(func(actual []string) (bool, error) {
		gotIDs := make([]tree.PageID, 0, len(actual))
		for _, rawID := range actual {
			gotIDs = append(gotIDs, newFixturePageID(rawID))
		}
		return Equal(want).Match(gotIDs)
	}).WithTemplate("Expected:\n{{.FormattedActual}}\n{{.To}} preserve tree page IDs in order\n{{format .Data 1}}", want)
}

func matchManagedMetadata(createdAt string, updatedAt string, creatorID string, lastAuthorID string) types.GomegaMatcher {
	return gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
		"LeafWikiCreatedAt":    Equal(createdAt),
		"LeafWikiUpdatedAt":    Equal(updatedAt),
		"LeafWikiCreatorID":    Equal(creatorID),
		"LeafWikiLastAuthorID": Equal(lastAuthorID),
	})
}

func matchSectionFrontmatter(id tree.PageID, title string, createdAt string, updatedAt string, creatorID string, lastAuthorID string) types.GomegaMatcher {
	return gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
		"LeafWikiID":           WithTransform(func(raw string) tree.PageID { return newFixturePageID(raw) }, Equal(id)),
		"LeafWikiTitle":        Equal(title),
		"LeafWikiCreatedAt":    Equal(createdAt),
		"LeafWikiUpdatedAt":    Equal(updatedAt),
		"LeafWikiCreatorID":    Equal(creatorID),
		"LeafWikiLastAuthorID": Equal(lastAuthorID),
	})
}

var _ = ginkgo.Describe("runner", func() {
	ginkgo.It("adds managed frontmatter during V2 migration while preserving page body", func() {
		skipUnlessSchemaVersionAtLeast(2)

		tmpDir := tempMigrationDir()
		writeSchema(tmpDir, 1)

		svc := tree.NewTreeService(tmpDir)
		Expect(svc.LoadTree()).To(Succeed())

		id, err := svc.CreateNode("system", nil, "Page1", "page1", ptrKind(tree.NodeKindPage))
		Expect(err).NotTo(HaveOccurred())
		persistLegacyTreeSnapshot(tmpDir, svc.GetTree())

		pagePath := filepath.Join(tmpDir, "root", "page1.md")
		body := "# Page 1 Content\nHello World\n"
		Expect(os.WriteFile(pagePath, []byte(body), 0o644)).To(Succeed())
		writeSchema(tmpDir, 1)

		loaded := tree.NewTreeService(tmpDir)
		Expect(loaded.LoadTree()).To(Succeed())

		raw, err := os.ReadFile(pagePath)
		Expect(err).NotTo(HaveOccurred())

		fm, migratedBody, has, err := markdown.ParseFrontmatter(string(raw))
		Expect(err).NotTo(HaveOccurred())
		Expect(has).To(BeTrue(), "expected frontmatter after migration, got:\n%s", string(raw))
		Expect(newFixturePageID(fm.LeafWikiID)).To(Equal(*id))
		Expect(strings.TrimSpace(fm.LeafWikiTitle)).NotTo(BeEmpty())
		Expect(migratedBody).To(Equal(body))
	})

	ginkgo.It("preserves custom frontmatter while adding V2 managed metadata", func() {
		skipUnlessSchemaVersionAtLeast(2)

		tmpDir := tempMigrationDir()
		writeSchema(tmpDir, 1)

		svc := tree.NewTreeService(tmpDir)
		Expect(svc.LoadTree()).To(Succeed())

		id, err := svc.CreateNode("system", nil, "Page1", "page1", ptrKind(tree.NodeKindPage))
		Expect(err).NotTo(HaveOccurred())
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
		Expect(os.WriteFile(pagePath, []byte(legacyContent), 0o644)).To(Succeed())
		writeSchema(tmpDir, 1)

		loaded := tree.NewTreeService(tmpDir)
		Expect(loaded.LoadTree()).To(Succeed())

		raw, err := os.ReadFile(pagePath)
		Expect(err).NotTo(HaveOccurred())

		migrated := string(raw)
		Expect(migrated).To(ContainSubstring("custom_key: keep-me"))
		Expect(migrated).To(ContainSubstring("- alpha"))

		fm, migratedBody, has, err := markdown.ParseFrontmatter(migrated)
		Expect(err).NotTo(HaveOccurred())
		Expect(has).To(BeTrue(), "expected frontmatter after migration, got:\n%s", migrated)
		Expect(newFixturePageID(fm.LeafWikiID)).To(Equal(*id))
		Expect(strings.TrimSpace(fm.LeafWikiTitle)).NotTo(BeEmpty())
		wantBody := "# Page 1 Content\nHello World\n"
		Expect(migratedBody).To(Equal(wantBody))
	})

	ginkgo.It("backfills V3 metadata frontmatter from stored page metadata", func() {
		skipUnlessSchemaVersionAtLeast(3)

		tmpDir := tempMigrationDir()
		writeSchema(tmpDir, 2)

		svc := tree.NewTreeService(tmpDir)
		Expect(svc.LoadTree()).To(Succeed())

		id, err := svc.CreateNode("system", nil, "Page1", "page1", ptrKind(tree.NodeKindPage))
		Expect(err).NotTo(HaveOccurred())

		node, err := svc.FindPageByID(*id)
		Expect(err).NotTo(HaveOccurred())
		node.Metadata = tree.PageMetadata{
			CreatedAt:    time.Date(2026, time.March, 21, 10, 15, 30, 0, time.UTC),
			UpdatedAt:    time.Date(2026, time.March, 21, 11, 16, 31, 0, time.UTC),
			CreatorID:    "alice",
			LastAuthorID: "bob",
		}

		persistLegacyTreeSnapshot(tmpDir, svc.GetTree())

		pagePath := filepath.Join(tmpDir, "root", "page1.md")
		legacyContent := fmt.Sprintf("---\nleafwiki_id: %s\nleafwiki_title: Page1\n---\n# Page 1 Content\nHello World\n", *id)
		Expect(os.WriteFile(pagePath, []byte(legacyContent), 0o644)).To(Succeed())
		writeSchema(tmpDir, 2)

		loaded := tree.NewTreeService(tmpDir)
		Expect(loaded.LoadTree()).To(Succeed())

		raw, err := os.ReadFile(pagePath)
		Expect(err).NotTo(HaveOccurred())

		fm, migratedBody, has, err := markdown.ParseFrontmatter(string(raw))
		Expect(err).NotTo(HaveOccurred())
		Expect(has).To(BeTrue())
		Expect(fm).To(matchManagedMetadata("2026-03-21T10:15:30Z", "2026-03-21T11:16:31Z", "alice", "bob"))
		wantBody := "# Page 1 Content\nHello World\n"
		Expect(migratedBody).To(Equal(wantBody))
	})

	ginkgo.It("backfills V5 child order files from the legacy tree order", func() {
		skipUnlessSchemaVersionAtLeast(5)

		tmpDir := tempMigrationDir()
		writeSchema(tmpDir, tree.CurrentSchemaVersion)

		svc := tree.NewTreeService(tmpDir)
		Expect(svc.LoadTree()).To(Succeed())

		docsID, err := svc.CreateNode("system", nil, "Docs", "docs", ptrKind(tree.NodeKindSection))
		Expect(err).NotTo(HaveOccurred())
		alphaID, err := svc.CreateNode("system", nil, "Alpha", "alpha", ptrKind(tree.NodeKindPage))
		Expect(err).NotTo(HaveOccurred())
		betaID, err := svc.CreateNode("system", docsID, "Beta", "beta", ptrKind(tree.NodeKindPage))
		Expect(err).NotTo(HaveOccurred())

		root := svc.GetTree()
		root.Children = []*tree.PageNode{root.Children[1], root.Children[0]}
		for i, child := range root.Children {
			child.Position = i
		}

		Expect(removeIfPresent(filepath.Join(tmpDir, "root", ".order.json"))).To(Succeed())
		Expect(removeIfPresent(filepath.Join(tmpDir, "root", "docs", ".order.json"))).To(Succeed())

		persistLegacyTreeSnapshot(tmpDir, svc.GetTree())
		writeSchema(tmpDir, 4)

		loaded := tree.NewTreeService(tmpDir)
		Expect(loaded.LoadTree()).To(Succeed())

		var rootOrder struct {
			OrderedIDs []string `json:"ordered_ids"`
		}
		rawRootOrder, err := os.ReadFile(filepath.Join(tmpDir, "root", ".order.json"))
		Expect(err).NotTo(HaveOccurred())
		Expect(json.Unmarshal(rawRootOrder, &rootOrder)).To(Succeed())
		Expect(rootOrder.OrderedIDs).To(matchOrderIDs(*alphaID, *docsID))

		var docsOrder struct {
			OrderedIDs []string `json:"ordered_ids"`
		}
		rawDocsOrder, err := os.ReadFile(filepath.Join(tmpDir, "root", "docs", ".order.json"))
		Expect(err).NotTo(HaveOccurred())
		Expect(json.Unmarshal(rawDocsOrder, &docsOrder)).To(Succeed())
		Expect(docsOrder.OrderedIDs).To(matchOrderIDs(*betaID))
	})

	ginkgo.It("materializes missing V4 section index frontmatter from section metadata", func() {
		skipUnlessSchemaVersionAtLeast(4)

		tmpDir := tempMigrationDir()
		writeSchema(tmpDir, 3)

		svc := tree.NewTreeService(tmpDir)
		Expect(svc.LoadTree()).To(Succeed())

		id, err := svc.CreateNode("system", nil, "Docs", "docs", ptrKind(tree.NodeKindSection))
		Expect(err).NotTo(HaveOccurred())

		node, err := svc.FindPageByID(*id)
		Expect(err).NotTo(HaveOccurred())
		node.Metadata = tree.PageMetadata{
			CreatedAt:    time.Date(2026, time.March, 22, 10, 15, 30, 0, time.UTC),
			UpdatedAt:    time.Date(2026, time.March, 22, 11, 16, 31, 0, time.UTC),
			CreatorID:    "alice",
			LastAuthorID: "bob",
		}

		persistLegacyTreeSnapshot(tmpDir, svc.GetTree())

		indexPath := filepath.Join(tmpDir, "root", "docs", "index.md")
		Expect(os.Remove(indexPath)).To(Succeed())
		writeSchema(tmpDir, 3)

		loaded := tree.NewTreeService(tmpDir)
		Expect(loaded.LoadTree()).To(Succeed())

		raw, err := os.ReadFile(indexPath)
		Expect(err).NotTo(HaveOccurred())
		fm, body, has, err := markdown.ParseFrontmatter(string(raw))
		Expect(err).NotTo(HaveOccurred())
		Expect(has).To(BeTrue())
		Expect(fm).To(matchSectionFrontmatter(*id, "Docs", "2026-03-22T10:15:30Z", "2026-03-22T11:16:31Z", "alice", "bob"))
		Expect(strings.TrimSpace(body)).To(BeEmpty())
	})

	ginkgo.It("coerces legacy page nodes with children so V5 preserves child order", func() {
		// Regression test for: https://github.com/perber/wiki/issues/932
		// Legacy trees could contain page nodes that have children when a folder and an .md file
		// shared the same name. The V5 migration must coerce such nodes to section so that
		// SaveChildOrder can write the .order.json and the legacy child ordering is preserved.
		skipUnlessSchemaVersionAtLeast(5)

		tmpDir := tempMigrationDir()
		writeSchema(tmpDir, tree.CurrentSchemaVersion)

		svc := tree.NewTreeService(tmpDir)
		Expect(svc.LoadTree()).To(Succeed())

		// Build: root → notes (section) → zebra, alpha (pages in non-alphabetical order)
		// so we can verify the legacy ordering is preserved, not reset to alphabetical.
		notesID, err := svc.CreateNode("system", nil, "Notes", "notes", ptrKind(tree.NodeKindSection))
		Expect(err).NotTo(HaveOccurred())
		zebraID, err := svc.CreateNode("system", notesID, "Zebra", "zebra", ptrKind(tree.NodeKindPage))
		Expect(err).NotTo(HaveOccurred())
		alphaID, err := svc.CreateNode("system", notesID, "Alpha", "alpha", ptrKind(tree.NodeKindPage))
		Expect(err).NotTo(HaveOccurred())

		// Corrupt the tree snapshot: flip "notes" kind from section → page to simulate
		// the legacy data shape reported in issue #932 (folder and .md file with same name).
		root := svc.GetTree()
		for _, child := range root.Children {
			if child.ID == *notesID {
				child.Kind = tree.NodeKindPage
			}
		}

		Expect(removeIfPresent(filepath.Join(tmpDir, "root", ".order.json"))).To(Succeed())
		Expect(removeIfPresent(filepath.Join(tmpDir, "root", "notes", ".order.json"))).To(Succeed())

		persistLegacyTreeSnapshot(tmpDir, root)
		writeSchema(tmpDir, 4)

		loaded := tree.NewTreeService(tmpDir)
		Expect(loaded.LoadTree()).To(Succeed())

		// .order.json for notes must exist and reflect the legacy order (zebra before alpha),
		// not the alphabetical fallback order that ReconstructTreeFromFS would produce without it.
		var notesOrder struct {
			OrderedIDs []string `json:"ordered_ids"`
		}
		rawOrder, err := os.ReadFile(filepath.Join(tmpDir, "root", "notes", ".order.json"))
		Expect(err).NotTo(HaveOccurred())
		Expect(json.Unmarshal(rawOrder, &notesOrder)).To(Succeed())
		Expect(notesOrder.OrderedIDs).To(matchOrderIDs(*zebraID, *alphaID))
	})

	ginkgo.It("returns a V5 child-order persistence error when order files cannot be written", func() {
		skipUnlessSchemaVersionAtLeast(5)
		if runtime.GOOS == "windows" {
			ginkgo.Skip("permission-based migration failure test is not reliable on Windows")
		}

		tmpDir := tempMigrationDir()
		writeSchema(tmpDir, tree.CurrentSchemaVersion)

		svc := tree.NewTreeService(tmpDir)
		Expect(svc.LoadTree()).To(Succeed())

		_, err := svc.CreateNode("system", nil, "Docs", "docs", ptrKind(tree.NodeKindSection))
		Expect(err).NotTo(HaveOccurred())
		_, err = svc.CreateNode("system", nil, "Alpha", "alpha", ptrKind(tree.NodeKindPage))
		Expect(err).NotTo(HaveOccurred())

		Expect(removeIfPresent(filepath.Join(tmpDir, "root", ".order.json"))).To(Succeed())
		Expect(os.Mkdir(filepath.Join(tmpDir, "root", ".order.json"), 0o755)).To(Succeed())
		writeSchema(tmpDir, 4)

		loaded := tree.NewTreeService(tmpDir)
		err = loaded.LoadTree()
		Expect(err).To(HaveOccurred())
		Expect(err).To(matchMigrationError(treemigration.ErrPersistChildOrder))
	})

	ginkgo.It("returns a V4 section-index materialization error when index files cannot be written", func() {
		skipUnlessSchemaVersionAtLeast(4)
		if runtime.GOOS == "windows" {
			ginkgo.Skip("permission-based migration failure test is not reliable on Windows")
		}

		tmpDir := tempMigrationDir()
		writeSchema(tmpDir, 3)

		svc := tree.NewTreeService(tmpDir)
		Expect(svc.LoadTree()).To(Succeed())

		id, err := svc.CreateNode("system", nil, "Docs", "docs", ptrKind(tree.NodeKindSection))
		Expect(err).NotTo(HaveOccurred())

		node, err := svc.FindPageByID(*id)
		Expect(err).NotTo(HaveOccurred())
		node.Metadata = tree.PageMetadata{
			CreatedAt:    time.Date(2026, time.March, 22, 10, 15, 30, 0, time.UTC),
			UpdatedAt:    time.Date(2026, time.March, 22, 11, 16, 31, 0, time.UTC),
			CreatorID:    "alice",
			LastAuthorID: "bob",
		}

		persistLegacyTreeSnapshot(tmpDir, svc.GetTree())

		sectionDir := filepath.Join(tmpDir, "root", "docs")
		indexPath := filepath.Join(sectionDir, "index.md")
		Expect(os.Remove(indexPath)).To(Succeed())
		Expect(os.Chmod(sectionDir, 0o555)).To(Succeed())
		ginkgo.DeferCleanup(os.Chmod, sectionDir, os.FileMode(0o755))
		writeSchema(tmpDir, 3)

		loaded := tree.NewTreeService(tmpDir)
		err = loaded.LoadTree()
		Expect(err).To(HaveOccurred())
		Expect(err).To(matchMigrationError(treemigration.ErrMaterializeSectionIndex))
	})
})
