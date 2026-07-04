package tree

import (
	"os"
	"path/filepath"
	"runtime"

	"github.com/perber/wiki/internal/core/markdown"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/gstruct"
)

func HaveRemovedTreeIndexEntries(nodeID PageID, parentID PageID) OmegaMatcher {
	return Satisfy(func(svc *TreeService) bool {
		if svc == nil {
			return false
		}
		_, hasNode := svc.nodesByID[nodeID]
		_, hasChildSlugs := svc.childSlugs[parentID]
		return !hasNode && !hasChildSlugs
	})
}

func HaveEmptyTreeIndexState() OmegaMatcher {
	return Satisfy(func(svc *TreeService) bool {
		if svc == nil {
			return false
		}
		return len(svc.nodesByID) == 0 && len(svc.childSlugs) == 0
	})
}

var _ = Describe("tree service unloaded, lookup, and legacy edge behavior", Label("unit"), func() {
	It("unloaded services return errors without disk state", func() {
		svc := NewTreeServiceWithOptions(TreeOptions{DataDir: tempTreeDir(), RootDir: filepath.Join(tempTreeDir(), "root")})

		_, err := svc.FindPageByID("missing")
		Expect(err).To(MatchError(ErrTreeNotLoaded))
		Expect(svc.DeleteNode("user", "missing", false, pageVersionUnchecked)).To(MatchError(ErrTreeNotLoaded))
		Expect(svc.UpdateNode("user", "missing", "Missing", "missing", nil, pageVersionUnchecked, false)).To(MatchError(ErrTreeNotLoaded))
		Expect(svc.ConvertNode("user", "missing", NodeKindSection, pageVersionUnchecked)).To(MatchError(ErrTreeNotLoaded))

		_, err = svc.GetPage("missing")
		Expect(err).To(MatchError(ErrTreeNotLoaded))
		_, err = svc.ReadPageRaw("missing")
		Expect(err).To(MatchError(ErrTreeNotLoaded))
		_, err = svc.ResolvePermalinkTarget("missing")
		Expect(err).To(MatchError(ErrTreeNotLoaded))
		_, err = svc.LookupPagePath("missing")
		Expect(err).To(MatchError(ErrTreeNotLoaded))
		_, err = svc.LookupPagePathForKind("missing", NodeKindPage)
		Expect(err).To(MatchError(ErrTreeNotLoaded))
		_, err = svc.EnsurePagePath("user", "missing", "Missing", nil)
		Expect(err).To(MatchError(ErrTreeNotLoaded))
		Expect(svc.MoveNode("user", "missing", RootPageID, pageVersionUnchecked)).To(MatchError(ErrTreeNotLoaded))
		Expect(svc.SortPages(RootPageID, nil)).To(MatchError(ErrTreeNotLoaded))

		bulkErrs := svc.BulkUpdateContent("user", []BulkContentUpdate{{ID: "missing", Content: "body"}})
		Expect(bulkErrs).To(ConsistOf(MatchError(ErrTreeNotLoaded)))
		pages, pageErrs := svc.GetPages([]PageID{"missing"})
		Expect(pages).To(Equal([]*Page{nil}))
		Expect(pageErrs).To(ConsistOf(MatchError(ErrTreeNotLoaded)))

		_, err = svc.ContentPathForNode(nil)
		Expect(err).To(matchInvalidOp("contentPathForNodeRead"))
	})

	It("index and lookup helpers maintain stable tree results", func() {
		root := edgeSectionNode(RootPageID, "root", "Root", nil)
		docs := edgeSectionNode("docs", "docs", "Docs", root)
		guide := edgePageNode("guide", "Guide", "Guide", docs)
		docs.Children = []*PageNode{nil, guide}
		root.Children = []*PageNode{nil, docs}

		svc := NewTreeService(tempTreeDir())
		svc.tree = root
		svc.rebuildIndexesLocked()

		Expect(svc.getNodeByIDLocked("")).To(BeNil())
		Expect(svc.getNodeByIDLocked(RootPageID)).To(BeIdenticalTo(root))
		Expect(svc.getNodeByIDLocked("guide")).To(BeIdenticalTo(guide))

		svc.rebuildChildSlugIndexForParentLocked(nil)
		svc.indexNodeLocked(nil)
		svc.removeNodeIndexLocked(nil)
		Expect(svc.findChildBySlugInParentLocked(nil, "guide")).To(BeNil())
		Expect(svc.findChildBySlugExactInParentLocked(nil, "guide")).To(BeNil())
		Expect(svc.findChildBySlugAndKindExactInParentLocked(nil, "guide", NodeKindPage)).To(BeNil())

		delete(svc.childSlugs, docs.ID)
		Expect(svc.findChildBySlugInParentLocked(docs, "guide")).To(BeIdenticalTo(guide))
		Expect(svc.findChildBySlugExactInParentLocked(docs, "missing")).To(BeNil())
		Expect(svc.findChildBySlugAndKindExactInParentLocked(docs, "Guide", NodeKindSection)).To(BeNil())

		svc.removeNodeIndexLocked(docs)
		Expect(svc).To(HaveRemovedTreeIndexEntries(newFixturePageID("guide"), newFixturePageID("docs")))

		svc.tree = nil
		svc.rebuildIndexesLocked()
		Expect(svc).To(HaveEmptyTreeIndexState())

		positions := snapshotChildPositions([]*PageNode{nil, guide})
		Expect(positions).To(HaveKeyWithValue(PageID("guide"), guide.Position))
		restoreChildSnapshot(nil, []*PageNode{guide}, positions)
		orphanParent := edgeSectionNode("orphan-parent", "orphan-parent", "Orphan Parent", root)
		restoreChildSnapshot(orphanParent, []*PageNode{nil, guide}, map[PageID]int{"guide": 7})
		Expect(guide).To(WithTransform(func(node *PageNode) PageNode {
			if node == nil {
				return PageNode{}
			}
			return *node
		}, gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
			"Parent":   BeIdenticalTo(orphanParent),
			"Position": Equal(7),
		})))
	})

	It("service lookup, read, and batch APIs propagate failure states", func() {
		svc, dataDir := newLoadedService()

		emptyLookup, err := svc.lookupPagePathLocked("", "")
		Expect(err).NotTo(HaveOccurred())
		Expect(emptyLookup).To(MatchPathLookupState(false, false))

		missingLookup, err := svc.lookupPagePathLocked("missing/!!!", "")
		Expect(err).NotTo(HaveOccurred())
		Expect(missingLookup).To(MatchPathLookupState(false, false))

		_, err = svc.FindPageByRoutePath("")
		Expect(err).To(MatchError(ErrMissingRoutePath))
		_, err = svc.EnsurePagePath("user", "", "Empty", nil)
		Expect(err).To(MatchError(ErrEnsurePagePath))

		pageID, err := svc.CreateNode("user", nil, "Page", "page", ptrKind(NodeKindPage))
		Expect(err).NotTo(HaveOccurred())
		page, err := svc.FindPageByID(*pageID)
		Expect(err).NotTo(HaveOccurred())
		currentVersion := page.Version()

		_, err = svc.FindPageByID("missing")
		Expect(err).To(MatchError(ErrPageNotFound))
		_, err = svc.GetPage("missing")
		Expect(err).To(MatchError(ErrPageNotFound))
		_, err = svc.ReadPageRaw("missing")
		Expect(err).To(MatchError(ErrPageNotFound))
		_, err = svc.ResolvePermalinkTarget("missing")
		Expect(err).To(MatchError(ErrPageNotFound))

		relPath, err := svc.ContentPathForNode(page)
		Expect(err).NotTo(HaveOccurred())
		badCanonicalPath := filepath.Join(dataDir, "root", filepath.FromSlash(relPath))
		Expect(os.WriteFile(badCanonicalPath, []byte("<!-- leafwiki\nversion: 1\n"), 0o644)).To(Succeed())

		errs := svc.BulkUpdateContent("bulk-user", []BulkContentUpdate{{ID: "missing", Content: "body"}, {ID: *pageID, Content: "new body"}})
		Expect(errs).To(HaveExactElements(
			MatchError(ErrPageNotFound),
			MatchError(ErrLoadMarkdownFile),
		))
		afterFailedBulk, err := svc.FindPageByID(*pageID)
		Expect(err).NotTo(HaveOccurred())
		Expect(afterFailedBulk.Version()).To(Equal(currentVersion))

		gotPages, gotErrs := svc.GetPages([]PageID{"missing", *pageID})
		Expect(gotPages).To(HaveExactElements(BeNil(), BeNil()))
		Expect(gotErrs).To(HaveExactElements(
			MatchError(ErrPageNotFound),
			MatchError(ErrGetPageContent),
		))

		_, err = svc.GetPage(*pageID)
		Expect(err).To(MatchError(ErrGetPageContent))
		_, err = svc.ReadPageRaw(*pageID)
		Expect(err).NotTo(HaveOccurred())
	})

	It("legacy root comparison distinguishes equivalent roots", func() {
		base := tempTreeDir()
		sourceDir := filepath.Join(base, "source")
		targetDir := filepath.Join(base, "target")
		Expect(os.MkdirAll(sourceDir, 0o755)).To(Succeed())
		Expect(os.MkdirAll(targetDir, 0o755)).To(Succeed())

		Expect(directoryFileContentsDiffer(sourceDir, targetDir)).To(Succeed())

		Expect(os.WriteFile(filepath.Join(sourceDir, "same.md"), []byte("# Same"), 0o644)).To(Succeed())
		Expect(directoryFileContentsDiffer(sourceDir, targetDir)).To(Succeed())
		Expect(os.WriteFile(filepath.Join(targetDir, "same.md"), []byte("# Same"), 0o644)).To(Succeed())
		Expect(directoryFileContentsMatch(sourceDir, targetDir)).To(Succeed())
		Expect(os.WriteFile(filepath.Join(targetDir, "same.md"), []byte("# Different"), 0o644)).To(Succeed())
		Expect(directoryFileContentsDiffer(sourceDir, targetDir)).To(Succeed())

		_, err := collectRelativeFiles(filepath.Join(base, "missing"))
		Expect(err).To(MatchError(ErrCollectLegacyContentFiles))
		Expect(filesHaveDifferentContent(filepath.Join(base, "missing.md"), filepath.Join(targetDir, "same.md"))).To(MatchError(ErrReadLegacyContentPath))

		if runtime.GOOS != "windows" {
			loop := filepath.Join(base, "loop")
			Expect(os.Symlink("loop", loop)).To(Succeed())
			Expect(directoryHasNoEntriesResult(loop)).To(MatchError(ErrReadDirectory))
			Expect(filesHaveDifferentContent(filepath.Join(sourceDir, "same.md"), loop)).To(MatchError(ErrReadConfiguredLegacyContentPath))
		}

		Expect(cleanPathsMatch(filepath.Join(base, "a", "..", "source"), sourceDir)).To(Succeed())
		Expect(cleanPathsDiffer(sourceDir, targetDir)).To(Succeed())
	})

	It("legacy content path comparison reports stable expectations", func() {
		base := tempTreeDir()
		dataDir := filepath.Join(base, "data")
		rootDir := filepath.Join(base, "configured")
		defaultRoot := filepath.Join(dataDir, "root")
		Expect(os.MkdirAll(defaultRoot, 0o755)).To(Succeed())
		Expect(os.MkdirAll(rootDir, 0o755)).To(Succeed())
		svc := NewTreeServiceWithOptions(TreeOptions{DataDir: dataDir, RootDir: rootDir})

		Expect(configuredRootMissingLegacyContentResult(svc, nil)).To(Succeed())

		legacy := &PageNode{ID: RootPageID, Slug: "root", Kind: NodeKindSection, Children: []*PageNode{
			{ID: "page", Slug: "page", Title: "Page", Kind: NodeKindPage},
			{ID: "section", Slug: "section", Title: "Section", Kind: NodeKindSection},
			{ID: "legacy", Slug: "legacy", Title: "Legacy", Kind: ""},
		}}
		paths, err := svc.expectedLegacyContentPaths(legacy)
		Expect(err).NotTo(HaveOccurred())
		Expect(paths).To(HaveLen(3))

		_, err = svc.expectedLegacyContentPaths(&PageNode{ID: "bad", Slug: "", Kind: NodeKindPage})
		Expect(err).To(MatchError(ErrSlugEmpty))
		_, err = svc.expectedLegacyContentPaths(&PageNode{ID: "bad", Slug: "bad", Kind: NodeKind("unknown")})
		Expect(err).To(MatchError(ErrLegacyUnknownKind))

		Expect(configuredRootMissingLegacyContentResult(svc, legacy)).To(Succeed())
		Expect(os.WriteFile(filepath.Join(rootDir, "marker.md"), []byte("# Marker"), 0o644)).To(Succeed())
		Expect(configuredRootHasLegacyContentResult(svc, legacy)).To(Succeed())
		Expect(os.Remove(filepath.Join(rootDir, "marker.md"))).To(Succeed())

		sourcePage := filepath.Join(defaultRoot, "page.md")
		Expect(os.WriteFile(sourcePage, []byte("# Page"), 0o644)).To(Succeed())
		Expect(configuredRootMissingLegacyContentResult(svc, legacy)).To(Succeed())

		targetPage := filepath.Join(rootDir, "page.md")
		Expect(os.MkdirAll(filepath.Dir(targetPage), 0o755)).To(Succeed())
		Expect(os.Mkdir(targetPage, 0o755)).To(Succeed())
		Expect(configuredRootMissingLegacyContentResult(svc, legacy)).To(Succeed())

		Expect(os.Remove(targetPage)).To(Succeed())
		mdFile := markdown.NewMarkdownFile(targetPage, "# Page", markdown.Frontmatter{LeafWikiID: "page", LeafWikiTitle: "Page"})
		Expect(mdFile.WriteToFile()).To(Succeed())
		Expect(os.WriteFile(sourcePage, []byte(mustReadString(targetPage)), 0o644)).To(Succeed())
		Expect(legacyTargetMatchesNodeResult(legacyContentPath{sourceFile: sourcePage, targetFile: targetPage, nodeID: "page", nodeTitle: "Page"})).To(Succeed())

		Expect(legacyTargetDiffersFromNodeResult(legacyContentPath{sourceFile: sourcePage, targetFile: targetPage, nodeID: "other", nodeTitle: "Page"})).To(Succeed())

		Expect(configuredRootHasLegacyContentResult(svc, legacy)).To(Succeed())
	})
})
