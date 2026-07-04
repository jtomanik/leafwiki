package tree

import (
	. "github.com/onsi/gomega"
	"os"
	"path/filepath"
	"strings"

	ginkgo "github.com/onsi/ginkgo/v2"
)

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
