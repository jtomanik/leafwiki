package tree

import (
	"os"
	"path/filepath"
	"runtime"

	"github.com/perber/wiki/internal/core/markdown"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("tree service edge coverage", func() {
	It("covers unloaded service errors without constructing disk state", func() {
		svc := NewTreeServiceWithOptions(TreeOptions{DataDir: GinkgoT().TempDir(), RootDir: filepath.Join(GinkgoT().TempDir(), "root")})

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

	It("covers index and lookup helpers directly", func() {
		root := edgeSectionNode(RootPageID, "root", "Root", nil)
		docs := edgeSectionNode("docs", "docs", "Docs", root)
		guide := edgePageNode("guide", "Guide", "Guide", docs)
		docs.Children = []*PageNode{nil, guide}
		root.Children = []*PageNode{nil, docs}

		svc := NewTreeService(GinkgoT().TempDir())
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
		Expect(svc.nodesByID).NotTo(HaveKey(PageID("guide")))
		Expect(svc.childSlugs).NotTo(HaveKey(PageID("docs")))

		svc.tree = nil
		svc.rebuildIndexesLocked()
		Expect(svc.nodesByID).To(BeEmpty())
		Expect(svc.childSlugs).To(BeEmpty())

		positions := snapshotChildPositions([]*PageNode{nil, guide})
		Expect(positions).To(HaveKeyWithValue(PageID("guide"), guide.Position))
		restoreChildSnapshot(nil, []*PageNode{guide}, positions)
		orphanParent := edgeSectionNode("orphan-parent", "orphan-parent", "Orphan Parent", root)
		restoreChildSnapshot(orphanParent, []*PageNode{nil, guide}, map[PageID]int{"guide": 7})
		Expect(guide.Parent).To(BeIdenticalTo(orphanParent))
		Expect(guide.Position).To(Equal(7))
	})

	It("covers service lookup, read, and batch failure branches", func() {
		svc, dataDir := newLoadedService(GinkgoT())

		emptyLookup, err := svc.lookupPagePathLocked("", "")
		Expect(err).NotTo(HaveOccurred())
		Expect(emptyLookup.Exists).To(BeFalse())
		Expect(emptyLookup.CanCreate).To(BeFalse())

		missingLookup, err := svc.lookupPagePathLocked("missing/!!!", "")
		Expect(err).NotTo(HaveOccurred())
		Expect(missingLookup.Exists).To(BeFalse())
		Expect(missingLookup.CanCreate).To(BeFalse())

		_, err = svc.FindPageByRoutePath("")
		Expect(err).To(MatchError("missing path"))
		_, err = svc.EnsurePagePath("user", "", "Empty", nil)
		Expect(err).To(MatchError(ContainSubstring("could not ensure page path")))

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
		Expect(errs[0]).To(MatchError(ErrPageNotFound))
		Expect(errs[1]).To(HaveOccurred())
		afterFailedBulk, err := svc.FindPageByID(*pageID)
		Expect(err).NotTo(HaveOccurred())
		Expect(afterFailedBulk.Version()).To(Equal(currentVersion))

		gotPages, gotErrs := svc.GetPages([]PageID{"missing", *pageID})
		Expect(gotPages[0]).To(BeNil())
		Expect(gotErrs[0]).To(MatchError(ErrPageNotFound))
		Expect(gotPages[1]).To(BeNil())
		Expect(gotErrs[1]).To(MatchError(ContainSubstring("could not get page content")))

		_, err = svc.GetPage(*pageID)
		Expect(err).To(MatchError(ContainSubstring("could not get page content")))
		_, err = svc.ReadPageRaw(*pageID)
		Expect(err).NotTo(HaveOccurred())
	})

	It("covers legacy root comparison helpers", func() {
		base := GinkgoT().TempDir()
		sourceDir := filepath.Join(base, "source")
		targetDir := filepath.Join(base, "target")
		Expect(os.MkdirAll(sourceDir, 0o755)).To(Succeed())
		Expect(os.MkdirAll(targetDir, 0o755)).To(Succeed())

		matches, err := directoryFileContentMatches(sourceDir, targetDir)
		Expect(err).NotTo(HaveOccurred())
		Expect(matches).To(BeFalse())

		Expect(os.WriteFile(filepath.Join(sourceDir, "same.md"), []byte("# Same"), 0o644)).To(Succeed())
		matches, err = directoryFileContentMatches(sourceDir, targetDir)
		Expect(err).NotTo(HaveOccurred())
		Expect(matches).To(BeFalse())
		Expect(os.WriteFile(filepath.Join(targetDir, "same.md"), []byte("# Same"), 0o644)).To(Succeed())
		matches, err = directoryFileContentMatches(sourceDir, targetDir)
		Expect(err).NotTo(HaveOccurred())
		Expect(matches).To(BeTrue())
		Expect(os.WriteFile(filepath.Join(targetDir, "same.md"), []byte("# Different"), 0o644)).To(Succeed())
		matches, err = directoryFileContentMatches(sourceDir, targetDir)
		Expect(err).NotTo(HaveOccurred())
		Expect(matches).To(BeFalse())

		_, err = collectRelativeFiles(filepath.Join(base, "missing"))
		Expect(err).To(MatchError(ContainSubstring("collect legacy content files")))
		_, err = filesHaveSameContent(filepath.Join(base, "missing.md"), filepath.Join(targetDir, "same.md"))
		Expect(err).To(MatchError(ContainSubstring("read legacy content path")))

		if runtime.GOOS != "windows" {
			loop := filepath.Join(base, "loop")
			Expect(os.Symlink("loop", loop)).To(Succeed())
			_, err = directoryHasEntries(loop)
			Expect(err).To(MatchError(ContainSubstring("read directory")))
			_, err = filesHaveSameContent(filepath.Join(sourceDir, "same.md"), loop)
			Expect(err).To(MatchError(ContainSubstring("read configured legacy content path")))
		}

		Expect(sameCleanPath(filepath.Join(base, "a", "..", "source"), sourceDir)).To(BeTrue())
		Expect(sameCleanPath(sourceDir, targetDir)).To(BeFalse())
	})

	It("covers legacy content path expectation branches", func() {
		base := GinkgoT().TempDir()
		dataDir := filepath.Join(base, "data")
		rootDir := filepath.Join(base, "configured")
		defaultRoot := filepath.Join(dataDir, "root")
		Expect(os.MkdirAll(defaultRoot, 0o755)).To(Succeed())
		Expect(os.MkdirAll(rootDir, 0o755)).To(Succeed())
		svc := NewTreeServiceWithOptions(TreeOptions{DataDir: dataDir, RootDir: rootDir})

		missing, err := svc.configuredRootMissingLegacyContent(nil)
		Expect(err).NotTo(HaveOccurred())
		Expect(missing).To(BeTrue())

		legacy := &PageNode{ID: RootPageID, Slug: "root", Kind: NodeKindSection, Children: []*PageNode{
			{ID: "page", Slug: "page", Title: "Page", Kind: NodeKindPage},
			{ID: "section", Slug: "section", Title: "Section", Kind: NodeKindSection},
			{ID: "legacy", Slug: "legacy", Title: "Legacy", Kind: ""},
		}}
		paths, err := svc.expectedLegacyContentPaths(legacy)
		Expect(err).NotTo(HaveOccurred())
		Expect(paths).To(HaveLen(3))

		_, err = svc.expectedLegacyContentPaths(&PageNode{ID: "bad", Slug: "", Kind: NodeKindPage})
		Expect(err).To(MatchError(ContainSubstring("empty slug")))
		_, err = svc.expectedLegacyContentPaths(&PageNode{ID: "bad", Slug: "bad", Kind: NodeKind("unknown")})
		Expect(err).To(MatchError(ContainSubstring("unknown kind")))

		missing, err = svc.configuredRootMissingLegacyContent(legacy)
		Expect(err).NotTo(HaveOccurred())
		Expect(missing).To(BeTrue())
		Expect(os.WriteFile(filepath.Join(rootDir, "marker.md"), []byte("# Marker"), 0o644)).To(Succeed())
		missing, err = svc.configuredRootMissingLegacyContent(legacy)
		Expect(err).NotTo(HaveOccurred())
		Expect(missing).To(BeFalse())
		Expect(os.Remove(filepath.Join(rootDir, "marker.md"))).To(Succeed())

		sourcePage := filepath.Join(defaultRoot, "page.md")
		Expect(os.WriteFile(sourcePage, []byte("# Page"), 0o644)).To(Succeed())
		missing, err = svc.configuredRootMissingLegacyContent(legacy)
		Expect(err).NotTo(HaveOccurred())
		Expect(missing).To(BeTrue())

		targetPage := filepath.Join(rootDir, "page.md")
		Expect(os.MkdirAll(filepath.Dir(targetPage), 0o755)).To(Succeed())
		Expect(os.Mkdir(targetPage, 0o755)).To(Succeed())
		missing, err = svc.configuredRootMissingLegacyContent(legacy)
		Expect(err).NotTo(HaveOccurred())
		Expect(missing).To(BeTrue())

		Expect(os.Remove(targetPage)).To(Succeed())
		mdFile := markdown.NewMarkdownFile(targetPage, "# Page", markdown.Frontmatter{LeafWikiID: "page", LeafWikiTitle: "Page"})
		Expect(mdFile.WriteToFile()).To(Succeed())
		Expect(os.WriteFile(sourcePage, []byte(mustReadString(targetPage)), 0o644)).To(Succeed())
		matches, err := legacyTargetMatchesNode(legacyContentPath{sourceFile: sourcePage, targetFile: targetPage, nodeID: "page", nodeTitle: "Page"})
		Expect(err).NotTo(HaveOccurred())
		Expect(matches).To(BeTrue())

		matches, err = legacyTargetMatchesNode(legacyContentPath{sourceFile: sourcePage, targetFile: targetPage, nodeID: "other", nodeTitle: "Page"})
		Expect(err).NotTo(HaveOccurred())
		Expect(matches).To(BeFalse())

		missing, err = svc.configuredRootMissingLegacyContent(legacy)
		Expect(err).NotTo(HaveOccurred())
		Expect(missing).To(BeFalse())
	})
})
