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

var _ = ginkgo.Describe("node store filesystem reconstruction", ginkgo.Label("unit"), func() {
	ginkgo.It("imports normalizable workspace routes", func() {
		tmp := tempTreeDir()
		store := NewNodeStore(tmp)

		createTreeDirectory(filepath.Join(tmp, "root", "plans"))
		writeTreeFile(filepath.Join(tmp, "root", "plans", "index.md"), "# Plans", 0o644)
		writeTreeFile(filepath.Join(tmp, "root", "plans", "agent_hooks.PLAN.md"), "# Agent Hooks Plan", 0o644)
		createTreeDirectory(filepath.Join(tmp, "root", "User Guides"))
		writeTreeFile(filepath.Join(tmp, "root", "User Guides", "index.md"), "# User Guides", 0o644)

		tree, err := store.ReconstructTreeFromFS()
		Expect(err).To(Succeed(), "ReconstructTreeFromFS: %v",

			err)

		plans := findChildBySlug(tree, "plans")
		Expect(plans).To(SatisfyAll(
			HaveField("Kind", Equal(NodeKindSection)),
			HaveField("Slug", Equal(newFixtureSlug("plans"))),
		), "unexpected plans section: %#v", plans)

		plan := findChildBySlug(plans, "agent-hooks-plan")
		Expect(plan).To(SatisfyAll(
			HaveField("Kind", Equal(NodeKindPage)),
			HaveField("Title", Equal("Agent Hooks Plan")),
		), "unexpected normalized plan page: %#v", plan)

		raw, err := store.ReadPageRaw(plan)
		Expect(err).To(Succeed(), "ReadPageRaw normalized plan: %v",

			err,
		)
		Expect(raw).To(ContainSubstring("Agent Hooks Plan"),

			"ReadPageRaw normalized plan = %q, want original file content",

			raw,
		)

		guides := findChildBySlug(tree, "user-guides")
		Expect(guides.Kind).
			To(Equal(NodeKindSection), "guides.Kind = %q, want %q",

				guides.
					Kind, NodeKindSection)

		rawGuides, err := store.ReadPageRaw(guides)
		Expect(err).To(Succeed(), "ReadPageRaw normalized section: %v",

			err)
		Expect(rawGuides).To(ContainSubstring("User Guides"),

			"ReadPageRaw normalized section = %q, want original section content",

			rawGuides)

	})
})

var _ = ginkgo.Describe("node store filesystem reconstruction", ginkgo.Label("unit"), func() {
	ginkgo.It("returns an error on normalized duplicate page routes", func() {
		tmp := tempTreeDir()
		store := NewNodeStore(tmp)

		createTreeDirectory(filepath.Join(tmp, "root", "plans"))
		writeTreeFile(filepath.Join(tmp, "root", "plans", "foo_bar.md"), "# Foo Bar", 0o644)
		writeTreeFile(filepath.Join(tmp, "root", "plans", "foo-bar.md"), "# Foo Bar Duplicate", 0o644)

		_, err := store.ReconstructTreeFromFS()
		Expect(err).To(MatchError(ErrDuplicateReconstructedSlug), "expected duplicate page slug error, got: %v",

			err)

	})
})

var _ = ginkgo.Describe("node store filesystem reconstruction", ginkgo.Label("unit"), func() {
	ginkgo.It("skips top level static assets", func() {
		tmp := tempTreeDir()
		store := NewNodeStore(tmp)

		createTreeDirectory(filepath.Join(tmp, "root", "assets", "install"))
		writeTreeFile(filepath.Join(tmp, "root", "assets", "install", "image.png"), "png", 0o644)
		writeTreeFile(filepath.Join(tmp, "root", "guide.md"), "# Guide", 0o644)

		tree, err := store.ReconstructTreeFromFS()
		Expect(err).To(Succeed(), "ReconstructTreeFromFS: %v",

			err)

		findChildBySlug(tree, "guide")
		for _, child := range tree.Children {
			Expect(strings.ToLower(child.Slug.String())).NotTo(SatisfyAny(Equal("assets"), Equal("assets-1")),
				"top-level static assets directory became wiki child: %#v", child)

		}

	})
})

var _ = ginkgo.Describe("node store filesystem reconstruction", ginkgo.Label("unit"), func() {
	ginkgo.It("writes IDs back to files", func() {
		tmp := tempTreeDir()
		store := NewNodeStore(tmp)

		// Create files without leafwiki_id in frontmatter
		writeTreeFile(filepath.Join(tmp, "root", "no-id.md"), "# No ID", 0o644)
		createTreeDirectory(filepath.Join(tmp, "root", "section"))
		writeTreeFile(filepath.Join(tmp, "root", "section", "index.md"), "# Section No ID", 0o644)

		// Run reconstruction
		tree, err := store.ReconstructTreeFromFS()
		Expect(err).To(Succeed(), "ReconstructTreeFromFS: %v",

			err)

		// Get the page and section nodes
		page := findChildBySlug(tree, "no-id")
		section := findChildBySlug(tree, "section")

		// Now reload the files and check that IDs were written back
		pageMd, err := markdown.LoadMarkdownFile(filepath.Join(tmp, "root", "no-id.md"))
		Expect(err).To(Succeed(), "failed to reload page: %v",

			err)
		Expect(PageIDFromString(pageMd.
			GetFrontmatter().LeafWikiID,
		)).
			To(Equal(page.
				ID),
				"expected page frontmatter ID=%q, got %q",
				page.ID,

				pageMd.GetFrontmatter().LeafWikiID)

		sectionMd, err := markdown.LoadMarkdownFile(filepath.Join(tmp, "root", "section", "index.md"))
		Expect(err).To(Succeed(), "failed to reload section index: %v",

			err)
		Expect(PageIDFromString(sectionMd.
			GetFrontmatter().LeafWikiID,
		)).To(Equal(section.
			ID), "expected section frontmatter ID=%q, got %q",

			section.ID, sectionMd.GetFrontmatter().LeafWikiID)

		// Run reconstruction again and verify IDs are stable (deterministic)
		tree2, err := store.ReconstructTreeFromFS()
		Expect(err).To(Succeed(), "second ReconstructTreeFromFS: %v",

			err,
		)

		page2 := findChildBySlug(tree2, "no-id")
		section2 := findChildBySlug(tree2, "section")
		Expect(page2.ID).To(
			Equal(page.
				ID),
			"expected deterministic page ID on second run: first=%q, second=%q",

			page.ID, page2.ID)
		Expect(section2.ID).
			To(Equal(section.
				ID), "expected deterministic section ID on second run: first=%q, second=%q",

				section.ID, section2.
					ID)

	})
})

var _ = ginkgo.Describe("node store filesystem reconstruction", ginkgo.Label("unit"), func() {
	ginkgo.It("normalizes importable slugs and skips empty normalized slugs", func() {
		tmp := tempTreeDir()
		store := NewNodeStore(tmp)

		// Create files and directories with names that were invalid route slugs but
		// can be normalized safely.
		writeTreeFile(filepath.Join(tmp, "root", "Valid Page.md"), "# Valid", 0o644)
		writeTreeFile(filepath.Join(tmp, "root", "UPPERCASE.md"), "# Upper", 0o644)
		createTreeDirectory(filepath.Join(tmp, "root", "Valid Section"))
		writeTreeFile(filepath.Join(tmp, "root", "Valid Section", "index.md"), "# Section", 0o644)
		writeTreeFile(filepath.Join(tmp, "root", "!!!.md"), "# Invalid", 0o644)

		// Create a valid file to ensure the test still works
		writeTreeFile(filepath.Join(tmp, "root", "valid.md"), "# Valid", 0o644)

		tree, err := store.ReconstructTreeFromFS()
		Expect(err).To(Succeed(), "ReconstructTreeFromFS: %v",

			err)

		// The valid file should be present with normalized slug
		findChildBySlug(tree, "valid")
		findChildBySlug(tree, "UPPERCASE")
		findChildBySlug(tree, "valid-page")
		findChildBySlug(tree, "valid-section")
		Expect(tree.Children).To(HaveLen(4),
			"expected only empty-normalized names to be skipped, got %v",

			slugs(tree.Children))

	})
})

var _ = ginkgo.Describe("node store filesystem reconstruction", ginkgo.Label("unit"), func() {
	ginkgo.It("preserves mixed case slug names", func() {
		tmp := tempTreeDir()
		store := NewNodeStore(tmp)

		writeTreeFile(filepath.Join(tmp, "root", "ABCD-efg.md"), "# Mixed Case", 0o644)

		tree, err := store.ReconstructTreeFromFS()
		Expect(err).To(Succeed(), "ReconstructTreeFromFS: %v",

			err)

		findChildBySlug(tree, "ABCD-efg")

	})
})
var _ = ginkgo.Describe("node store filesystem reconstruction", ginkgo.Label("unit"), func() {
	ginkgo.It("reads metadata from frontmatter", func() {
		tmp := tempTreeDir()
		store := NewNodeStore(tmp)

		writeTreeFile(filepath.Join(tmp, "root", "page.md"), `---
leafwiki_id: page-1
leafwiki_title: Page One
leafwiki_created_at: 2026-03-21T10:15:30Z
leafwiki_updated_at: 2026-03-21T11:16:31Z
leafwiki_creator_id: alice
leafwiki_last_author_id: bob
---
# Page One`, 0o644)

		tree, err := store.ReconstructTreeFromFS()
		Expect(err).To(Succeed(), "ReconstructTreeFromFS: %v",

			err)

		page := findChildBySlug(tree, "page")
		Expect(page).To(SatisfyAll(
			HaveField("ID", Equal(newFixturePageID("page-1"))),
			HaveField("Metadata", SatisfyAll(
				matchPageMetadataTimestamps("2026-03-21T10:15:30Z", "2026-03-21T11:16:31Z"),
				matchPageMetadataAuthors(newFixtureUserID("alice"), newFixtureUserID("bob")),
			)),
		), "expected page metadata from frontmatter, got %#v", page)

	})
})

var _ = ginkgo.Describe("node store filesystem reconstruction", ginkgo.Label("unit"), func() {
	ginkgo.It("canonicalizes complete legacy metadata", func() {
		tmp := tempTreeDir()
		store := NewNodeStore(tmp)

		rootIndex := filepath.Join(tmp, "root", "index.md")
		sectionIndex := filepath.Join(tmp, "root", "docs", "index.md")
		pagePath := filepath.Join(tmp, "root", "docs", "page.md")

		writeTreeFile(rootIndex, `---
leafwiki_id: root
leafwiki_title: Root
leafwiki_created_at: 2026-03-21T09:00:00Z
leafwiki_updated_at: 2026-03-21T09:30:00Z
leafwiki_creator_id: root-author
leafwiki_last_author_id: root-editor
---
# Root`, 0o644)
		writeTreeFile(sectionIndex, `---
leafwiki_id: docs-section
leafwiki_title: Docs
leafwiki_created_at: 2026-03-21T10:00:00Z
leafwiki_updated_at: 2026-03-21T10:30:00Z
leafwiki_creator_id: docs-author
leafwiki_last_author_id: docs-editor
---
# Docs`, 0o644)
		writeTreeFile(pagePath, `---
leafwiki_id: docs-page
leafwiki_title: Page
leafwiki_created_at: 2026-03-21T11:00:00Z
leafwiki_updated_at: 2026-03-21T11:30:00Z
leafwiki_creator_id: page-author
leafwiki_last_author_id: page-editor
---
# Page`, 0o644)

		tree, err := store.ReconstructTreeFromFS()
		Expect(err).To(Succeed(), "ReconstructTreeFromFS: %v",

			err)
		Expect(tree.ID).To(Equal(newFixturePageID("root")), "root ID = %q",
			tree.
				ID)

		section := findChildBySlug(tree, "docs")
		Expect(section.ID).To(Equal(newFixturePageID("docs-section")), "section ID = %q",

			section.
				ID)

		page := findChildBySlug(section, "page")
		Expect(page.ID).To(Equal(newFixturePageID("docs-page")), "page ID = %q",
			page.ID,
		)

		for _, path := range []string{rootIndex, sectionIndex, pagePath} {
			raw, err := os.ReadFile(path)
			Expect(err).To(Succeed(), "ReadFile(%s): %v",

				path, err,
			)

			content := string(raw)
			Expect(content).
				To(HavePrefix("<!-- leafwiki\n"),

					"%s was not canonicalized: %q",

					path, content)
			Expect(content).NotTo(HavePrefix("---\n"),

				"%s still starts with YAML frontmatter: %q",

				path, content)
			{

				_, _, err := markdown.ParsePageDocument(content)
				Expect(err).To(Succeed(), "ParsePageDocument(%s): %v",

					path, err,
				)
			}

		}

	})
})

var _ = ginkgo.Describe("node store filesystem reconstruction", ginkgo.Label("unit"), func() {
	ginkgo.It("missing metadata falls back to mtime and system", func() {
		tmp := tempTreeDir()
		store := NewNodeStore(tmp)

		pagePath := filepath.Join(tmp, "root", "page.md")
		writeTreeFile(pagePath, `# Page One`, 0o644)

		wantTime := time.Date(2026, time.March, 21, 12, 34, 56, 0, time.UTC)
		{
			err := os.Chtimes(pagePath, wantTime, wantTime)
			Expect(err).To(Succeed(), "Chtimes: %v",

				err)
		}

		tree, err := store.ReconstructTreeFromFS()
		Expect(err).To(Succeed(), "ReconstructTreeFromFS: %v",

			err)

		page := findChildBySlug(tree, "page")
		Expect(strings.TrimSpace(page.
			ID.String())).NotTo(BeEmpty(),

			"expected generated ID",
		)
		{

			got := page.Metadata.CreatedAt.UTC().Format(time.RFC3339)
			Expect(got).To(Equal(wantTime.
				Format(time.RFC3339)),
				"expected created_at fallback from mtime, got %q",

				got)
		}
		{

			got := page.Metadata.UpdatedAt.UTC().Format(time.RFC3339)
			Expect(got).To(Equal(wantTime.
				Format(time.RFC3339)),
				"expected updated_at fallback from mtime, got %q",

				got)
		}
		Expect(page.Metadata).To(matchPageMetadataAuthors(reconstructSystemUserID, reconstructSystemUserID),
			"expected system user fallback, got %#v",

			page.Metadata)

		mdFile, err := markdown.LoadMarkdownFile(pagePath)
		Expect(err).To(Succeed(), "LoadMarkdownFile: %v",

			err)

		frontmatter := mdFile.GetFrontmatter()
		Expect(frontmatter).To(SatisfyAll(
			HaveField("LeafWikiID", WithTransform(func(raw string) PageID {
				return PageIDFromString(raw)
			}, Equal(page.ID))),
			HaveField("LeafWikiCreatedAt", Equal(wantTime.Format(time.RFC3339))),
			HaveField("LeafWikiUpdatedAt", Equal(wantTime.Format(time.RFC3339))),
			HaveField("LeafWikiCreatorID", Equal(reconstructSystemUserID)),
			HaveField("LeafWikiLastAuthorID", Equal(reconstructSystemUserID)),
		), "expected generated metadata fallback to be written back, got %#v", frontmatter)

	})
})
