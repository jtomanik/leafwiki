package tree

import (
	. "github.com/onsi/gomega"
	"os"
	"path/filepath"

	ginkgo "github.com/onsi/ginkgo/v2"
)

var _ = ginkgo.Describe("tree service behavior", ginkgo.Label("unit"), func() {
	ginkgo.It("create child under page auto converts parent to section", func() {
		svc, tmpDir := newLoadedService()

		// Create parent as page
		parentID, err := svc.CreateNode(newFixtureUserID("system"), nil, "Docs", newFixtureSlug("docs"), ptrKind(NodeKindPage))
		Expect(err).To(Succeed(), "Create parent failed: %v",

			err)

		// Should exist as file initially
		parentFile := filepath.Join(tmpDir, "root", "docs.md")
		statTreePath(parentFile)

		// Create child under parent: must convert parent to section
		_, err = svc.CreateNode(newFixtureUserID("system"), parentID, "Getting Started", newFixtureSlug("getting-started"), ptrKind(NodeKindPage))
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

		id, err := svc.CreateNode(newFixtureUserID("system"), nil, "Docs", newFixtureSlug("docs"), ptrKind(NodeKindPage))
		Expect(err).To(Succeed(), "CreateNode failed: %v",

			err)

		p := filepath.Join(tmpDir, "root", "docs.md")
		statTreePath(p)
		{

			// Update title only: content=nil, slug unchanged
			err := svc.UpdateNode(newFixtureUserID("system"), *id, "Documentation", newFixtureSlug("docs"), nil, pageVersionUnchecked, false)
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

		id, err := svc.CreateNode(newFixtureUserID("system"), nil, "Docs", newFixtureSlug("docs"), ptrKind(NodeKindPage))
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

			_, err := svc.CreateNode(newFixtureUserID("system"), nil, "Sync Section", newFixtureSlug("sync"), ptrKind(NodeKindSection))
			Expect(err).To(Succeed(), "CreateNode section failed: %v",

				err,
			)
		}

		pageID, err := svc.CreateNode(newFixtureUserID("system"), nil, "Draft Page", newFixtureSlug("draft"), ptrKind(NodeKindPage))
		Expect(err).To(Succeed(), "CreateNode page failed: %v",

			err)
		{

			err := svc.UpdateNode(newFixtureUserID("system"), *pageID, "Sync Page", newFixtureSlug("sync"), nil, pageVersionUnchecked, false)
			Expect(err).To(Succeed(), "UpdateNode page rename to section basename failed: %v",

				err,
			)
		}

		statTreePath(filepath.Join(tmpDir, "root", "sync.md"))
		statTreePath(filepath.Join(tmpDir, "root", "sync", "index.md"))

		page, err := svc.FindPageByRoutePathAndKind(newFixtureRoutePath("sync"), NodeKindPage)
		Expect(err).To(Succeed(), "FindPageByRoutePathAndKind page failed: %v",

			err,
		)

		Expect(page.ID).To(Equal(*pageID))

	})
})

var _ = ginkgo.Describe("tree service behavior", ginkgo.Label("unit"), func() {
	ginkgo.It("update node rejects case insensitive slug conflict", func() {
		svc, _ := newLoadedService()

		firstID, err := svc.CreateNode(newFixtureUserID("system"), nil, "Alpha", newFixtureSlug("Alpha"), ptrKind(NodeKindPage))
		Expect(err).To(Succeed(), "CreateNode first failed: %v",

			err)

		secondID, err := svc.CreateNode(newFixtureUserID("system"), nil, "Beta", newFixtureSlug("Beta"), ptrKind(NodeKindPage))
		Expect(err).To(Succeed(), "CreateNode second failed: %v",

			err)

		err = svc.UpdateNode(newFixtureUserID("system"), *secondID, "Beta", newFixtureSlug("alpha"), nil, pageVersionUnchecked, false)
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
		id, err := svc.CreateNode(newFixtureUserID("system"), nil, "Docs", newFixtureSlug("docs"), ptrKind(NodeKindPage))
		Expect(err).To(Succeed(), "CreateNode failed: %v",

			err)

		err = svc.UpdateNode(newFixtureUserID("system"), *id, "Docs", newFixtureSlug("../outside"), nil, pageVersionUnchecked, false)
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
	parentID, err := svc.CreateNode(newFixtureUserID("system"), nil, "Docs", newFixtureSlug("docs"), ptrKind(NodeKindPage))
		Expect(err).To(Succeed())
		_, err = svc.CreateNode("system", parentID, "Child", newFixtureSlug("child"), ptrKind(NodeKindPage))
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

		parentID, _ := svc.CreateNode(newFixtureUserID("system"), nil, "Parent", newFixtureSlug("parent"), ptrKind(NodeKindPage))
		_, _ = svc.CreateNode(newFixtureUserID("system"), parentID, "Child", newFixtureSlug("child"), ptrKind(NodeKindPage))

		err := svc.DeleteNode(newFixtureUserID("system"), *parentID, false, pageVersionUnchecked)
		Expect(err).To(MatchError(ErrPageHasChildren), "expected ErrPageHasChildren, got: %v",

			err,
		)

	})
})

var _ = ginkgo.Describe("tree service behavior", ginkgo.Label("unit"), func() {
	ginkgo.It("delete node recursive deletes disk and tree", func() {
		svc, tmpDir := newLoadedService()

		parentID, _ := svc.CreateNode(newFixtureUserID("system"), nil, "Parent", newFixtureSlug("parent"), ptrKind(NodeKindPage))
		_, _ = svc.CreateNode(newFixtureUserID("system"), parentID, "Child", newFixtureSlug("child"), ptrKind(NodeKindPage))

		// Parent should now be a folder
		parentDir := filepath.Join(tmpDir, "root", "parent")
		statTreePath(parentDir)

		err := svc.DeleteNode(newFixtureUserID("system"), *parentID, true, pageVersionUnchecked)
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
		idA, err := svc.CreateNode(newFixtureUserID("system"), nil, "A", newFixtureSlug("a"), ptrKind(NodeKindPage))
		Expect(err).To(Succeed(), "CreateNode A: %v",

			err)

		idB, err := svc.CreateNode(newFixtureUserID("system"), nil, "B", newFixtureSlug("b"), ptrKind(NodeKindPage))
		Expect(err).To(Succeed(), "CreateNode B: %v",

			err)

		idC, err := svc.CreateNode(newFixtureUserID("system"), nil, "C", newFixtureSlug("c"), ptrKind(NodeKindPage))
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
			err := svc.DeleteNode(newFixtureUserID("system"), *idB, false, pageVersionUnchecked)
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

		idA, err := svc.CreateNode(newFixtureUserID("system"), nil, "A", newFixtureSlug("a"), ptrKind(NodeKindPage))
		Expect(err).To(Succeed(), "CreateNode A: %v",

			err)

		idB, err := svc.CreateNode(newFixtureUserID("system"), nil, "B", newFixtureSlug("b"), ptrKind(NodeKindPage))
		Expect(err).To(Succeed(), "CreateNode B: %v",

			err)

		idC, err := svc.CreateNode(newFixtureUserID("system"), nil, "C", newFixtureSlug("c"), ptrKind(NodeKindPage))
		Expect(err).To(Succeed(), "CreateNode C: %v",

			err)
		{

			err := svc.DeleteNode(newFixtureUserID("system"), *idB, false, pageVersionUnchecked)
			Expect(err).To(Succeed(), "DeleteNode failed: %v",

				err)
		}

		Expect(readOrderIDs(filepath.Join(tmpDir, "root"))).To(matchPersistedPageIDOrder(*idA, *idC))

	})
})

var _ = ginkgo.Describe("tree service behavior", ginkgo.Label("unit"), func() {
	ginkgo.It("delete page with children non recursive returns err page has children", func() {
		svc, _ := newLoadedService()

		parentID, err := svc.CreateNode(newFixtureUserID("system"), nil, "Parent", newFixtureSlug("parent"), ptrKind(NodeKindPage))
		Expect(err).To(Succeed(), "CreateNode parent: %v",

			err)

		_, err = svc.CreateNode(newFixtureUserID("system"), parentID, "Child", newFixtureSlug("child"), ptrKind(NodeKindPage))
		Expect(err).To(Succeed(), "CreateNode child: %v",

			err)

		err = svc.DeleteNode(newFixtureUserID("system"), *parentID, false, pageVersionUnchecked)
		Expect(err).To(MatchError(ErrPageHasChildren), "expected ErrPageHasChildren, got: %v",

			err,
		)

	})
})
