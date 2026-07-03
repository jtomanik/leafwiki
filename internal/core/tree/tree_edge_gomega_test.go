package tree

import (
	"os"
	"path/filepath"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/perber/wiki/internal/core/treemigration"
)

var _ = Describe("tree semantic value and service wrapper edge behavior", func() {
	It("asserts semantic value edge methods and slug filename helpers", func() {
		Expect(RevisionIDFromString("rev-1")).To(Equal(newFixtureRevisionID("rev-1")))
		Expect(CleanMarkdownPath(".")).To(BeEmpty())
		Expect(MarkdownPathFromString("readme.md").SourceDir()).To(BeEmpty())
		Expect(equalFoldASCII("index", "index.md")).To(BeFalse())
		Expect(equalFoldASCII("INDEX.MD", "index.md")).To(BeTrue())
		Expect(equalFoldASCII("index.md", "INDEX.MD")).To(BeTrue())
		Expect(equalFoldASCII("indey.md", "index.md")).To(BeFalse())

		Expect(RoutePathFromString("").Segments()).To(BeNil())
		Expect(RoutePathFromString("docs//guide").Segments()).To(Equal([]Slug{
			newFixtureSlug("docs"),
			newFixtureSlug("guide"),
		}))
		Expect(RoutePathFromString("").WithLeafSlug("home")).To(Equal(RoutePathFromString("home")))
		Expect(RoutePathFromString("docs").WithLeafSlug("guide")).To(Equal(RoutePathFromString("guide")))
		Expect(RoutePathFromString("docs/old").WithLeafSlug("guide")).To(Equal(RoutePathFromString("docs/guide")))
		Expect(RoutePathFromString("").MarkdownContentPath(NodeKindPage)).To(Equal(MarkdownPathFromString("index.md")))
		Expect(RoutePathFromString("").LeafSlug()).To(BeEmpty())
		Expect(WorkspaceSourcePathFromString("readme.md").Dir()).To(BeEmpty())

		assetName := AssetNameFromString(" icon.svg ")
		Expect(assetName).To(Equal(AssetNameFromString(" icon.svg ")))
		Expect(assetName.Clean().Filename()).To(Equal("icon.svg"))

		parsedSlug, err := ParseSlug("valid-slug")
		Expect(err).NotTo(HaveOccurred())
		Expect(parsedSlug).To(Equal(newFixtureSlug("valid-slug")))
		_, err = ParseSlug("api")
		Expect(err).To(HaveOccurred())

		slugService := NewSlugService()
		Expect(slugService.NormalizeFilename("My File.MD")).To(Equal("my-file.MD"))
		Expect(slugService.GenerateUniqueFilename([]string{"my-file.md", "my-file-1.md"}, "My File.md")).To(Equal("my-file-2.md"))
		_, err = slugService.NormalizePath("api", true)
		Expect(err).To(HaveOccurred())
		_, err = slugService.NormalizePathToValidSlugs("!!!")
		Expect(err).To(HaveOccurred())
		_, err = slugService.NormalizeFilenameToValidSlug("!!!.md")
		Expect(err).To(HaveOccurred())
	})

	It("asserts page-node fallbacks and nil-safe wrappers", func() {
		var nilPage *Page
		var nilNode *PageNode
		Expect(nilPage.Version()).To(BeEmpty())
		Expect(nilNode.Version()).To(BeEmpty())
		Expect((&Page{}).Version()).To(BeEmpty())
		Expect((&Page{PageNode: &PageNode{}}).Version()).To(BeEmpty())

		root := &PageNode{ID: RootPageID, Slug: "root", Title: "Root"}
		docs := &PageNode{ID: newFixturePageID("docs"), Slug: "Docs", Title: "Docs", Parent: root, Kind: NodeKindSection}
		guide := &PageNode{ID: newFixturePageID("guide"), Slug: "guide", Title: "Guide", Parent: docs, Kind: NodeKindPage}
		docs.Children = []*PageNode{guide}
		root.Children = []*PageNode{docs}

		Expect(docs.ChildAlreadyExists("docs")).To(BeFalse())
		Expect(docs.ChildAlreadyExists("GUIDE")).To(BeTrue())
		Expect(root.IsChildOf(newFixturePageID("guide"), false)).To(BeFalse())
		Expect(root.IsChildOf(newFixturePageID("guide"), true)).To(BeTrue())
		Expect(root.CalculatePath()).To(BeEmpty())
		Expect((&PageNode{ID: newFixturePageID("orphan"), Slug: "orphan"}).CalculateRoutePath()).To(Equal(RoutePathFromString("orphan")))

		hashable := &PageNode{
			ID:       newFixturePageID("hash-root"),
			Slug:     "hash-root",
			Children: []*PageNode{nil, &PageNode{ID: newFixturePageID("child"), Slug: "child"}},
		}
		Expect(hashable.Hash()).To(HaveLen(64))

		tieSorted := &PageNode{
			ID:   newFixturePageID("tie-root"),
			Slug: "tie-root",
			Children: []*PageNode{
				{ID: newFixturePageID("b"), Slug: "b", Position: 0},
				{ID: newFixturePageID("a"), Slug: "a", Position: 0},
			},
		}
		Expect(tieSorted.hashSum(false)).NotTo(Equal(tieSorted.hashSum(true)))
	})

	It("migration node adapters guard nil inputs and preserve metadata mutation", func() {
		var nilAdapter *migrationNodeAdapter
		Expect(nilAdapter.ID()).To(BeEmpty())
		Expect(nilAdapter.Title()).To(BeEmpty())
		Expect(nilAdapter.Slug()).To(BeEmpty())
		Expect(nilAdapter.Kind()).To(Equal(treemigration.NodeKindUnknown))
		Expect(nilAdapter.Metadata().CreatorID).To(BeEmpty())
		Expect(nilAdapter.Children()).To(BeNil())
		nilAdapter.SetKind(treemigration.NodeKindSection)
		nilAdapter.SetMetadata(emptyTreemigrationMetadata())

		createdAt := time.Date(2026, time.June, 1, 10, 0, 0, 0, time.UTC)
		updatedAt := time.Date(2026, time.June, 2, 11, 0, 0, 0, time.UTC)
		node := &PageNode{
			ID:    newFixturePageID("docs"),
			Title: "Docs",
			Slug:  "docs",
			Kind:  NodeKindPage,
			Children: []*PageNode{{
				ID:    newFixturePageID("guide"),
				Title: "Guide",
				Slug:  "guide",
				Kind:  NodeKindPage,
			}},
		}
		adapter := &migrationNodeAdapter{node: node}

		Expect(adapter.ID()).To(Equal("docs"))
		Expect(adapter.Title()).To(Equal("Docs"))
		Expect(adapter.Slug()).To(Equal("docs"))
		Expect(adapter.Kind()).To(Equal(treemigration.NodeKindPage))
		adapter.SetKind(treemigration.NodeKindSection)
		Expect(node.Kind).To(Equal(NodeKindSection))

		metadata := emptyTreemigrationMetadata()
		metadata.CreatedAt = createdAt
		metadata.UpdatedAt = updatedAt
		metadata.CreatorID = "alice"
		metadata.LastAuthorID = "bob"
		adapter.SetMetadata(metadata)
		Expect(adapter.Metadata().CreatorID).To(Equal("alice"))
		Expect(adapter.Metadata().LastAuthorID).To(Equal("bob"))
		Expect(adapter.Metadata().CreatedAt).To(BeTemporally("==", createdAt))
		Expect(adapter.Metadata().UpdatedAt).To(BeTemporally("==", updatedAt))

		children := adapter.Children()
		Expect(children).To(HaveLen(1))
		Expect(children[0].Slug()).To(Equal("guide"))

		_, err := unwrapMigrationNode(nil)
		Expect(err).To(HaveOccurred())
		_, err = unwrapMigrationNode(&migrationNodeAdapter{})
		Expect(err).To(HaveOccurred())
		unwrapped, err := unwrapMigrationNode(adapter)
		Expect(err).NotTo(HaveOccurred())
		Expect(unwrapped).To(BeIdenticalTo(node))
	})

	It("service wrappers preserve restore, raw-read, kind lookup, root-dir, and metadata contracts", func() {
		svc, dataDir := newLoadedService()
		Expect(svc.RootDir()).To(Equal(filepath.Join(dataDir, "root")))

		metadata := PageMetadata{
			CreatedAt:    time.Date(2026, time.June, 1, 10, 0, 0, 0, time.FixedZone("offset", 3600)),
			UpdatedAt:    time.Date(2026, time.June, 2, 11, 0, 0, 0, time.FixedZone("offset", 3600)),
			CreatorID:    newFixtureUserID("alice"),
			LastAuthorID: newFixtureUserID("bob"),
		}
		restored, err := svc.RestoreNode(
			newFixtureUserID("restorer"),
			newFixturePageID("restored-page"),
			nil,
			"Restored Page",
			newFixtureSlug("restored"),
			NodeKindPage,
			"restored body",
			metadata,
		)
		Expect(err).NotTo(HaveOccurred())
		Expect(restored.ID).To(Equal(newFixturePageID("restored-page")))
		Expect(restored.Metadata.CreatedAt.Location()).To(Equal(time.UTC))
		Expect(restored.Metadata.UpdatedAt.Location()).To(Equal(time.UTC))

		raw, err := svc.ReadPageRaw(restored.ID)
		Expect(err).NotTo(HaveOccurred())
		Expect(raw).To(ContainSubstring("restored body"))

		lookup, err := svc.LookupPagePathForKind("restored", NodeKindPage)
		Expect(err).NotTo(HaveOccurred())
		Expect(lookup).To(MatchExistingPathLookupWithKind(NodeKindPage))

		sectionLookup, err := svc.LookupPagePathForKind("restored", NodeKindSection)
		Expect(err).NotTo(HaveOccurred())
		Expect(sectionLookup).To(MatchPathLookupState(false, true))

		plainUpdate := "plain unchecked body"
		Expect(svc.UpdateNodeUncheckedVersion(
			newFixtureUserID("carol"),
			restored.ID,
			"Restored Page",
			newFixtureSlug("restored"),
			&plainUpdate,
			false,
		)).To(Succeed())
		raw, err = svc.ReadPageRaw(restored.ID)
		Expect(err).NotTo(HaveOccurred())
		Expect(raw).To(ContainSubstring("plain unchecked body"))

		convertID, err := svc.CreateNode(newFixtureUserID("carol"), nil, "Convertible", newFixtureSlug("convertible"), ptrKind(NodeKindPage))
		Expect(err).NotTo(HaveOccurred())
		Expect(svc.ConvertNodeUncheckedVersion(newFixtureUserID("carol"), *convertID, NodeKindSection)).To(Succeed())
		converted, err := svc.FindPageByID(*convertID)
		Expect(err).NotTo(HaveOccurred())
		Expect(converted.Kind).To(Equal(NodeKindSection))

		destID, err := svc.CreateNode(newFixtureUserID("carol"), nil, "Archive", newFixtureSlug("archive"), ptrKind(NodeKindSection))
		Expect(err).NotTo(HaveOccurred())
		moveID, err := svc.CreateNode(newFixtureUserID("carol"), nil, "Movable", newFixtureSlug("movable"), ptrKind(NodeKindPage))
		Expect(err).NotTo(HaveOccurred())
		Expect(svc.MoveNodeUncheckedVersion(newFixtureUserID("carol"), *moveID, *destID)).To(Succeed())
		moved, err := svc.FindPageByID(*moveID)
		Expect(err).NotTo(HaveOccurred())
		Expect(moved.Parent.ID).To(Equal(*destID))
		Expect(moved.CalculateRoutePath()).To(Equal(RoutePathFromString("archive/movable")))

		replacement := "---\nleafwiki_title: From Raw\nleafwiki_id: ignored\n---\nreplacement body"
		Expect(svc.UpdateNodeReplacingMetadataUncheckedVersion(
			newFixtureUserID("carol"),
			restored.ID,
			"Restored Page",
			newFixtureSlug("restored"),
			&replacement,
		)).To(Succeed())
		raw, err = svc.ReadPageRaw(restored.ID)
		Expect(err).NotTo(HaveOccurred())
		Expect(raw).To(ContainSubstring("Restored Page"))
		Expect(raw).NotTo(ContainSubstring("ignored"))
		Expect(raw).To(ContainSubstring("replacement body"))

		Expect(svc.DeleteNodeUncheckedVersion(newFixtureUserID("carol"), restored.ID, false)).To(Succeed())
		_, err = svc.ReadPageRaw(restored.ID)
		Expect(err).To(MatchError(ErrPageNotFound))
	})

	It("filesystem helper no-ops leave absent paths stable", func() {
		tmpDir := tempTreeDir()
		Expect(EnsurePageIsFolder(tmpDir, "missing/page")).To(Succeed())
		Expect(FoldPageFolderIfEmpty(tmpDir, "missing/page")).To(Succeed())

		dir := filepath.Join(tmpDir, "docs", "guide")
		Expect(os.MkdirAll(dir, 0o755)).To(Succeed())
		Expect(os.WriteFile(filepath.Join(dir, "index.md"), []byte("# Guide"), 0o644)).To(Succeed())
		Expect(os.WriteFile(filepath.Join(dir, "child.md"), []byte("# Child"), 0o644)).To(Succeed())
		Expect(FoldPageFolderIfEmpty(tmpDir, "docs/guide")).To(Succeed())
		Expect(filepath.Join(tmpDir, "docs", "guide", "index.md")).To(BeAnExistingFile())
		Expect(filepath.Join(tmpDir, "docs", "guide.md")).NotTo(BeAnExistingFile())
	})
})

func emptyTreemigrationMetadata() treemigration.Metadata {
	return treemigration.Metadata{}
}
