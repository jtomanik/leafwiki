package tree

import (
	. "github.com/onsi/gomega"
	"os"
	"path/filepath"
	"time"

	ginkgo "github.com/onsi/ginkgo/v2"
)

var _ = ginkgo.Describe("tree service behavior", ginkgo.Label("unit"), func() {
	ginkgo.It("delete page with children recursive deletes folder", func() {
		svc, tmpDir := newLoadedService()

		parentID, err := svc.CreateNode(newFixtureUserID("system"), nil, "Parent", newFixtureSlug("parent"), ptrKind(NodeKindPage))
		Expect(err).To(Succeed(), "CreateNode parent: %v",

			err)

		_, err = svc.CreateNode(newFixtureUserID("system"), parentID, "Child", newFixtureSlug("child"), ptrKind(NodeKindPage))
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
			err := svc.DeleteNode(newFixtureUserID("system"), *parentID, true, pageVersionUnchecked)
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

		err := svc.DeleteNode(newFixtureUserID("system"), newFixturePageID("does-not-exist"), false, pageVersionUnchecked)
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
		id, err := svc.CreateNode(newFixtureUserID("system"), nil, "Ghost", newFixtureSlug("ghost"), ptrKind(NodeKindPage))
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
		err = svc.DeleteNode(newFixtureUserID("system"), *id, false, pageVersionUnchecked)

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

		aID, _ := svc.CreateNode(newFixtureUserID("system"), nil, "A", newFixtureSlug("a"), ptrKind(NodeKindPage))
		bID, _ := svc.CreateNode(newFixtureUserID("system"), nil, "B", newFixtureSlug("b"), ptrKind(NodeKindPage))
		{

			// Move A under B (B is a page => should auto-convert to section)
			err := svc.MoveNode(newFixtureUserID("system"), *aID, *bID, pageVersionUnchecked)
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

		destID, err := svc.CreateNode(newFixtureUserID("system"), nil, "Dest", newFixtureSlug("dest"), ptrKind(NodeKindSection))
		Expect(err).To(Succeed(), "CreateNode dest: %v",

			err)

		moveID, err := svc.CreateNode(newFixtureUserID("system"), nil, "Move", newFixtureSlug("move"), ptrKind(NodeKindPage))
		Expect(err).To(Succeed(), "CreateNode move: %v",

			err)

		stayID, err := svc.CreateNode(newFixtureUserID("system"), nil, "Stay", newFixtureSlug("stay"), ptrKind(NodeKindPage))
		Expect(err).To(Succeed(), "CreateNode stay: %v",

			err)

		nestedID, err := svc.CreateNode(newFixtureUserID("system"), destID, "Nested", newFixtureSlug("nested"), ptrKind(NodeKindPage))
		Expect(err).To(Succeed(), "CreateNode nested: %v",

			err)
		{

			err := svc.MoveNode(newFixtureUserID("system"), *moveID, *destID, pageVersionUnchecked)
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

		destID, err := svc.CreateNode(newFixtureUserID("system"), nil, "Dest", newFixtureSlug("dest"), ptrKind(NodeKindSection))
		Expect(err).To(Succeed(), "CreateNode dest failed: %v",

			err)
		{

			_, err := svc.CreateNode(newFixtureUserID("system"), destID, "Sync Section", newFixtureSlug("sync"), ptrKind(NodeKindSection))
			Expect(err).To(Succeed(), "CreateNode destination section failed: %v",

				err,
			)
		}

		moveID, err := svc.CreateNode(newFixtureUserID("system"), nil, "Sync Page", newFixtureSlug("sync"), ptrKind(NodeKindPage))
		Expect(err).To(Succeed(), "CreateNode page failed: %v",

			err)
		{

			err := svc.MoveNode(newFixtureUserID("system"), *moveID, *destID, pageVersionUnchecked)
			Expect(err).To(Succeed(), "MoveNode page next to section basename failed: %v",

				err)
		}

		statTreePath(filepath.Join(tmpDir, "root", "dest", "sync.md"))
		statTreePath(filepath.Join(tmpDir, "root", "dest", "sync", "index.md"))

		page, err := svc.FindPageByRoutePathAndKind(newFixtureRoutePath("dest/sync"), NodeKindPage)
		Expect(err).To(Succeed(), "FindPageByRoutePathAndKind moved page failed: %v",

			err)

		Expect(page.ID).To(Equal(*moveID))

	})
})

var _ = ginkgo.Describe("tree service behavior", ginkgo.Label("unit"), func() {
	ginkgo.It("move node persists moved node metadata to frontmatter", func() {
		svc, tmpDir := newLoadedService()

		destID, err := svc.CreateNode(newFixtureUserID("system"), nil, "Dest", newFixtureSlug("dest"), ptrKind(NodeKindSection))
		Expect(err).To(Succeed(), "CreateNode dest: %v",

			err)

		moveID, err := svc.CreateNode(newFixtureUserID("system"), nil, "Move", newFixtureSlug("move"), ptrKind(NodeKindPage))
		Expect(err).To(Succeed(), "CreateNode move: %v",

			err)

		node, err := svc.FindPageByID(*moveID)
		Expect(err).To(Succeed(), "FindPageByID failed: %v",

			err)

		beforeUpdatedAt := node.Metadata.UpdatedAt
		{

			err := svc.MoveNode(newFixtureUserID("alice"), *moveID, *destID, pageVersionUnchecked)
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

		destID, err := svc.CreateNode(newFixtureUserID("system"), nil, "Dest", newFixtureSlug("dest"), ptrKind(NodeKindSection))
		Expect(err).To(Succeed(), "CreateNode dest: %v",

			err)

		moveID, err := svc.CreateNode(newFixtureUserID("system"), nil, "Move", newFixtureSlug("move"), ptrKind(NodeKindPage))
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

		err = svc.MoveNode(newFixtureUserID("system"), *moveID, *destID, pageVersionUnchecked)
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

		dest := findChildBySlug(root, newFixtureSlug("dest"))
		Expect(dest.Children).To(HaveLen(0),
			"expected destination children to be rolled back, got %#v",

			dest.Children)

	})
})

var _ = ginkgo.Describe("tree service behavior", ginkgo.Label("unit"), func() {
	ginkgo.It("move node prevents circular reference", func() {
		svc, _ := newLoadedService()

		aID, _ := svc.CreateNode(newFixtureUserID("system"), nil, "A", newFixtureSlug("a"), ptrKind(NodeKindPage))
		// create child under A so A becomes section and has child
		bID, _ := svc.CreateNode(newFixtureUserID("system"), aID, "B", newFixtureSlug("b"), ptrKind(NodeKindPage))

		// Try move A under B (A -> ... -> B). Should error with circular reference.
		err := svc.MoveNode(newFixtureUserID("system"), *aID, *bID, pageVersionUnchecked)
		Expect(err).To(MatchError(ErrMovePageCircularReference), "expected ErrMovePageCircularReference, got: %v",

			err)

	})
})

var _ = ginkgo.Describe("tree service behavior", ginkgo.Label("unit"), func() {
	ginkgo.It("move node prevents self parent", func() {
		svc, _ := newLoadedService()

		aID, _ := svc.CreateNode(newFixtureUserID("system"), nil, "A", newFixtureSlug("a"), ptrKind(NodeKindPage))

		err := svc.MoveNode(newFixtureUserID("system"), *aID, *aID, pageVersionUnchecked)
		Expect(err).To(MatchError(ErrPageCannotBeMovedToItself), "expected ErrPageCannotBeMovedToItself, got: %v",

			err)

	})
})

var _ = ginkgo.Describe("tree service behavior", ginkgo.Label("unit"), func() {
	ginkgo.It("move node rejects case insensitive slug conflict", func() {
		svc, _ := newLoadedService()

		parentID, err := svc.CreateNode(newFixtureUserID("system"), nil, "Parent", newFixtureSlug("parent"), ptrKind(NodeKindSection))
		Expect(err).To(Succeed(), "CreateNode parent failed: %v",

			err)

		moveID, err := svc.CreateNode(newFixtureUserID("system"), nil, "Move", newFixtureSlug("Alpha"), ptrKind(NodeKindPage))
		Expect(err).To(Succeed(), "CreateNode move failed: %v",

			err)
		{

			_, err := svc.CreateNode(newFixtureUserID("system"), parentID, "Existing", newFixtureSlug("alpha"), ptrKind(NodeKindPage))
			Expect(err).To(Succeed(), "CreateNode existing failed: %v",

				err,
			)
		}

		err = svc.MoveNode(newFixtureUserID("system"), *moveID, *parentID, pageVersionUnchecked)
		Expect(err).To(MatchError(ErrPageAlreadyExists), "expected ErrPageAlreadyExists, got %v",

			err,
		)

	})
})

// --- D) SortPages ---

var _ = ginkgo.Describe("tree service behavior", ginkgo.Label("unit"), func() {
	ginkgo.It("sort pages valid order", func() {
		svc, _ := newLoadedService()

		idA, _ := svc.CreateNode(newFixtureUserID("system"), nil, "A", newFixtureSlug("a"), ptrKind(NodeKindPage))
		idB, _ := svc.CreateNode(newFixtureUserID("system"), nil, "B", newFixtureSlug("b"), ptrKind(NodeKindPage))
		idC, _ := svc.CreateNode(newFixtureUserID("system"), nil, "C", newFixtureSlug("c"), ptrKind(NodeKindPage))

		err := svc.SortPages(newFixturePageID("root"), testPageIDs(*idC, *idA, *idB))
		Expect(err).To(Succeed(), "SortPages failed: %v",

			err)

		root := svc.GetTree()
		Expect(root).To(haveChildPageIDs(*idC, *idA, *idB), "unexpected order after sort")
		Expect(root).To(haveChildPositions(0, 1, 2), "expected positions to be reindexed")

	})
})
