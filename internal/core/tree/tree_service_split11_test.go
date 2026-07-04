package tree

import (
	"errors"
	. "github.com/onsi/gomega"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	ginkgo "github.com/onsi/ginkgo/v2"
	"github.com/perber/wiki/internal/core/treemigration"
)

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
