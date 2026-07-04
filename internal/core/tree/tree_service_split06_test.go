package tree

import (
	"encoding/json"
	. "github.com/onsi/gomega"
	"os"
	"path/filepath"
	"time"

	ginkgo "github.com/onsi/ginkgo/v2"
)

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
