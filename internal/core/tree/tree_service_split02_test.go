package tree

import (
	. "github.com/onsi/gomega"
	"os"
	"path/filepath"
	"strings"

	ginkgo "github.com/onsi/ginkgo/v2"
)

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
			ID:       newFixturePageID("root"),
			Slug:     newFixtureSlug("root"),
			Title:    "root",
			Kind:     NodeKindSection,
			Children: []*PageNode{{ID: newFixturePageID("id-legacy"), Slug: newFixtureSlug("legacy"), Title: "Legacy", Kind: NodeKindPage}},
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
			ID:       newFixturePageID("root"),
			Slug:     newFixtureSlug("root"),
			Title:    "root",
			Kind:     NodeKindSection,
			Children: []*PageNode{{ID: newFixturePageID("id-legacy"), Slug: newFixtureSlug("legacy"), Title: "Legacy", Kind: NodeKindPage}},
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
			ID:       newFixturePageID("root"),
			Slug:     newFixtureSlug("root"),
			Title:    "root",
			Kind:     NodeKindSection,
			Children: []*PageNode{{ID: newFixturePageID("id-legacy"), Slug: newFixtureSlug("legacy"), Title: "Legacy", Kind: NodeKindPage}},
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
			ID:       newFixturePageID("root"),
			Slug:     newFixtureSlug("root"),
			Title:    "root",
			Kind:     NodeKindSection,
			Children: []*PageNode{{ID: newFixturePageID("id-legacy"), Slug: newFixtureSlug("legacy"), Title: "Legacy", Kind: NodeKindPage}},
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
			ID:       newFixturePageID("root"),
			Slug:     newFixtureSlug("root"),
			Title:    "root",
			Kind:     NodeKindSection,
			Children: []*PageNode{{ID: newFixturePageID("id-c"), Slug: newFixtureSlug("c"), Title: "C", Kind: NodeKindPage, Position: 0}, {ID: newFixturePageID("id-a"), Slug: newFixtureSlug("a"), Title: "A", Kind: NodeKindPage, Position: 1}, {ID: newFixturePageID("id-b"), Slug: newFixtureSlug("b"), Title: "B", Kind: NodeKindPage, Position: 2}},
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
			ID:    newFixturePageID("root"),
			Slug:  newFixtureSlug("root"),
			Title: "root",
			Kind:  NodeKindSection,
			Children: []*PageNode{
				{ID: newFixturePageID("id-b"), Slug: newFixtureSlug("b"), Title: "B", Kind: NodeKindPage, Position: 0},
				{ID: newFixturePageID("id-a"), Slug: newFixtureSlug("a"), Title: "A", Kind: NodeKindPage, Position: 1},
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
		idA, err := svc.CreateNode(newFixtureUserID("system"), nil, "A", newFixtureSlug("a"), ptrKind(NodeKindPage))
		Expect(err).To(Succeed(), "CreateNode A failed: %v",

			err)

		_, err = svc.CreateNode(newFixtureUserID("system"), idA, "B", newFixtureSlug("b"), ptrKind(NodeKindPage))
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
		pageID, err := svc.CreateNode(newFixtureUserID("system"), nil, "Welcome", newFixtureSlug("welcome"), ptrKind(NodeKindPage))
		Expect(err).To(Succeed(), "CreateNode failed: %v",

			err)

		afterCreate := svc.TreeHash()
		Expect(before).NotTo(Equal(afterCreate), "expected hash to change after create")
		{

			err := svc.UpdateNode(newFixtureUserID("system"), *pageID, "Welcome 2", newFixtureSlug("welcome"), nil, pageVersionUnchecked, false)
			Expect(err).To(Succeed(), "UpdateNode failed: %v",

				err)
		}

		afterUpdate := svc.TreeHash()
		Expect(afterCreate).
			NotTo(Equal(afterUpdate), "expected hash to change after update")

	})
})
