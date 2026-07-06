package tree

import (
	. "github.com/onsi/gomega"
	"os"
	"path/filepath"
	"strings"
	"time"

	ginkgo "github.com/onsi/ginkgo/v2"
)

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
		page1 := findChildBySlug(tree, newFixtureSlug("page1"))
		Expect(page1.Metadata).To(haveRecordedMetadataTimestamps(), "expected page1 metadata timestamps to be backfilled")

		// Find and verify section1
		section1 := findChildBySlug(tree, newFixtureSlug("section1"))
		Expect(section1.Metadata).To(haveRecordedMetadataTimestamps(), "expected section1 metadata timestamps to be backfilled")

		// Find and verify page2 (child of section1)
		page2 := findChildBySlug(section1, newFixtureSlug("page2"))
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
		readme := findChildBySlug(tree, newFixtureSlug("readme"))
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

		readme := findChildBySlug(newSvc.GetTree(), newFixtureSlug("readme"))
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

		readme := findChildBySlug(newSvc.GetTree(), newFixtureSlug("readme"))
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
		intro := findChildBySlug(tree, newFixtureSlug("intro"))
		Expect(intro).To(matchReconstructedNode(NodeKindPage, nil), "expected intro to be reconstructed as a page with metadata")

		docs := findChildBySlug(tree, newFixtureSlug("docs"))
		Expect(docs).To(matchReconstructedNode(NodeKindSection, Equal(newFixturePageID("docs-section"))),
			"expected docs to reload as the frontmatter-backed section with metadata")

		gettingStarted := findChildBySlug(docs, newFixtureSlug("getting-started"))
		Expect(gettingStarted).To(matchReconstructedNode(NodeKindPage, nil),
			"expected getting-started to be reconstructed as a page with metadata")

		guides := findChildBySlug(docs, newFixtureSlug("guides"))
		Expect(guides).To(matchReconstructedNode(NodeKindSection, nil),
			"expected guides to be reconstructed as a section with metadata")

		basic := findChildBySlug(guides, newFixtureSlug("basic"))
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
		initialID, err := svc.CreateNode(newFixtureUserID("system"), nil, "Initial", newFixtureSlug("initial"), ptrKind(NodeKindPage))
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
