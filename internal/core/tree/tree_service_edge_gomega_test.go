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

		_, err := svc.FindPageByID(newFixturePageID("missing"))
		Expect(err).To(MatchError(ErrTreeNotLoaded))
		Expect(svc.DeleteNode(newFixtureUserID("user"), newFixturePageID("missing"), false, pageVersionUnchecked)).To(MatchError(ErrTreeNotLoaded))
		Expect(svc.UpdateNode(newFixtureUserID("user"), newFixturePageID("missing"), "Missing", newFixtureSlug("missing"), nil, pageVersionUnchecked, false)).To(MatchError(ErrTreeNotLoaded))
		Expect(svc.ConvertNode(newFixtureUserID("user"), newFixturePageID("missing"), NodeKindSection, pageVersionUnchecked)).To(MatchError(ErrTreeNotLoaded))

		_, err = svc.GetPage(newFixturePageID("missing"))
		Expect(err).To(MatchError(ErrTreeNotLoaded))
		_, err = svc.ReadPageRaw(newFixturePageID("missing"))
		Expect(err).To(MatchError(ErrTreeNotLoaded))
		_, err = svc.ResolvePermalinkTarget(newFixturePageID("missing"))
		Expect(err).To(MatchError(ErrTreeNotLoaded))
		_, err = svc.LookupPagePath(newFixtureRoutePath("missing"))
		Expect(err).To(MatchError(ErrTreeNotLoaded))
		_, err = svc.LookupPagePathForKind(newFixtureRoutePath("missing"), NodeKindPage)
		Expect(err).To(MatchError(ErrTreeNotLoaded))
		_, err = svc.EnsurePagePath(newFixtureUserID("user"), newFixtureRoutePath("missing"), "Missing", nil)
		Expect(err).To(MatchError(ErrTreeNotLoaded))
		Expect(svc.MoveNode(newFixtureUserID("user"), newFixturePageID("missing"), RootPageID, pageVersionUnchecked)).To(MatchError(ErrTreeNotLoaded))
		Expect(svc.SortPages(RootPageID, nil)).To(MatchError(ErrTreeNotLoaded))

		bulkErrs := svc.BulkUpdateContent(newFixtureUserID("user"), []BulkContentUpdate{{ID: newFixturePageID("missing"), Content: "body"}})
		Expect(bulkErrs).To(ConsistOf(MatchError(ErrTreeNotLoaded)))
		pages, pageErrs := svc.GetPages([]PageID{newFixturePageID("missing")})
		Expect(pages).To(Equal([]*Page{nil}))
		Expect(pageErrs).To(ConsistOf(MatchError(ErrTreeNotLoaded)))

		_, err = svc.ContentPathForNode(nil)
		Expect(err).To(matchInvalidOp("contentPathForNodeRead"))
	})

	It("index and lookup helpers maintain stable tree results", func() {
		root := edgeSectionNode(RootPageID, newFixtureSlug("root"), "Root", nil)
		docs := edgeSectionNode(newFixturePageID("docs"), newFixtureSlug("docs"), "Docs", root)
		guide := edgePageNode(newFixturePageID("guide"), newFixtureSlug("Guide"), "Guide", docs)
		docs.Children = []*PageNode{nil, guide}
		root.Children = []*PageNode{nil, docs}

		svc := NewTreeService(tempTreeDir())
		svc.tree = root
		svc.rebuildIndexesLocked()

		Expect(svc.getNodeByIDLocked(newFixturePageID(""))).To(BeNil())
		Expect(svc.getNodeByIDLocked(RootPageID)).To(BeIdenticalTo(root))
		Expect(svc.getNodeByIDLocked(newFixturePageID("guide"))).To(BeIdenticalTo(guide))

		svc.rebuildChildSlugIndexForParentLocked(nil)
		svc.indexNodeLocked(nil)
		svc.removeNodeIndexLocked(nil)
		Expect(svc.findChildBySlugInParentLocked(nil, newFixtureSlug("guide"))).To(BeNil())
		Expect(svc.findChildBySlugExactInParentLocked(nil, newFixtureSlug("guide"))).To(BeNil())
		Expect(svc.findChildBySlugAndKindExactInParentLocked(nil, newFixtureSlug("guide"), NodeKindPage)).To(BeNil())

		delete(svc.childSlugs, docs.ID)
		Expect(svc.findChildBySlugInParentLocked(docs, newFixtureSlug("guide"))).To(BeIdenticalTo(guide))
		Expect(svc.findChildBySlugExactInParentLocked(docs, newFixtureSlug("missing"))).To(BeNil())
		Expect(svc.findChildBySlugAndKindExactInParentLocked(docs, newFixtureSlug("Guide"), NodeKindSection)).To(BeNil())

		svc.removeNodeIndexLocked(docs)
		Expect(svc).To(HaveRemovedTreeIndexEntries(newFixturePageID("guide"), newFixturePageID("docs")))

		svc.tree = nil
		svc.rebuildIndexesLocked()
		Expect(svc).To(HaveEmptyTreeIndexState())

		positions := snapshotChildPositions([]*PageNode{nil, guide})
		Expect(positions).To(HaveKeyWithValue(newFixturePageID("guide"), guide.Position))
		restoreChildSnapshot(nil, []*PageNode{guide}, positions)
		orphanParent := edgeSectionNode(newFixturePageID("orphan-parent"), newFixtureSlug("orphan-parent"), "Orphan Parent", root)
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

		emptyLookup, err := svc.lookupPagePathLocked(newFixtureRoutePath(""), newFixtureNodeKind(""))
		Expect(err).NotTo(HaveOccurred())
		Expect(emptyLookup).To(MatchPathLookupState(false, false))

		missingLookup, err := svc.lookupPagePathLocked(newFixtureRoutePath("missing/!!!"), newFixtureNodeKind(""))
		Expect(err).NotTo(HaveOccurred())
		Expect(missingLookup).To(MatchPathLookupState(false, false))

		_, err = svc.FindPageByRoutePath(newFixtureRoutePath(""))
		Expect(err).To(MatchError(ErrMissingRoutePath))
		_, err = svc.EnsurePagePath(newFixtureUserID("user"), newFixtureRoutePath(""), "Empty", nil)
		Expect(err).To(MatchError(ErrEnsurePagePath))

		pageID, err := svc.CreateNode(newFixtureUserID("user"), nil, "Page", newFixtureSlug("page"), ptrKind(NodeKindPage))
		Expect(err).NotTo(HaveOccurred())
		page, err := svc.FindPageByID(*pageID)
		Expect(err).NotTo(HaveOccurred())
		currentVersion := page.Version()

		_, err = svc.FindPageByID(newFixturePageID("missing"))
		Expect(err).To(MatchError(ErrPageNotFound))
		_, err = svc.GetPage(newFixturePageID("missing"))
		Expect(err).To(MatchError(ErrPageNotFound))
		_, err = svc.ReadPageRaw(newFixturePageID("missing"))
		Expect(err).To(MatchError(ErrPageNotFound))
		_, err = svc.ResolvePermalinkTarget(newFixturePageID("missing"))
		Expect(err).To(MatchError(ErrPageNotFound))

		relPath, err := svc.ContentPathForNode(page)
		Expect(err).NotTo(HaveOccurred())
		badCanonicalPath := filepath.Join(dataDir, "root", filepath.FromSlash(relPath))
		Expect(os.WriteFile(badCanonicalPath, []byte("<!-- leafwiki\nversion: 1\n"), 0o644)).To(Succeed())

		errs := svc.BulkUpdateContent(newFixtureUserID("bulk-user"), []BulkContentUpdate{{ID: newFixturePageID("missing"), Content: "body"}, {ID: *pageID, Content: "new body"}})
		Expect(errs).To(HaveExactElements(
			MatchError(ErrPageNotFound),
			MatchError(ErrLoadMarkdownFile),
		))
		afterFailedBulk, err := svc.FindPageByID(*pageID)
		Expect(err).NotTo(HaveOccurred())
		Expect(afterFailedBulk.Version()).To(Equal(currentVersion))

		gotPages, gotErrs := svc.GetPages([]PageID{newFixturePageID("missing"), *pageID})
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

	It(")legacy root comparison distinguishes equivalent roots", func() {
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

		legacy := &PageNode{ID: RootPageID, Slug: newFixtureSlug("root"), Kind: NodeKindSection, Children: []*PageNode{
			{ID: newFixturePageID("page"), Slug: newFixtureSlug("page"), Title: "Page", Kind: NodeKindPage},
			{ID: newFixturePageID("section"), Slug: newFixtureSlug("section"), Title: "Section", Kind: NodeKindSection},
			{ID: newFixturePageID("legacy"), Slug: newFixtureSlug("legacy"), Title: "Legacy", Kind: newFixtureNodeKind("")},
		}}
		paths, err := svc.expectedLegacyContentPaths(legacy)
		Expect(err).NotTo(HaveOccurred())
		Expect(paths).To(HaveLen(3))

		_, err = svc.expectedLegacyContentPaths(&PageNode{ID: newFixturePageID("bad"), Slug: newFixtureSlug(""), Kind: NodeKindPage})
		Expect(err).To(MatchError(ErrSlugEmpty))
		_, err = svc.expectedLegacyContentPaths(&PageNode{ID: newFixturePageID("bad"), Slug: newFixtureSlug("bad"), Kind: newFixtureNodeKind("unknown")})
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
		Expect(legacyTargetMatchesNodeResult(legacyContentPath{sourceFile: sourcePage, targetFile: targetPage, nodeID: newFixturePageID("page"), nodeTitle: "Page"})).To(Succeed())

		Expect(legacyTargetDiffersFromNodeResult(legacyContentPath{sourceFile: sourcePage, targetFile: targetPage, nodeID: newFixturePageID("other"), nodeTitle: "Page"})).To(Succeed())

		Expect(configuredRootHasLegacyContentResult(svc, legacy)).To(Succeed())
	})
})
