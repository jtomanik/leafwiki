package tree

import (
	"errors"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/perber/wiki/internal/core/markdown"
)

var _ = Describe("tree service migration and store seam failure behavior", Label("unit"), func() {

	It("root, lookup, ensure, and move seams preserve service error contracts", func() {
		base := tempTreeDir()
		dataDir := filepath.Join(base, "data")
		rootDir := filepath.Join(base, "configured")
		defaultRoot := filepath.Join(dataDir, "root")
		Expect(os.MkdirAll(defaultRoot, 0o755)).To(Succeed())
		Expect(os.MkdirAll(rootDir, 0o755)).To(Succeed())

		swapTreeSeam(&treeLoadSchema, func(string) (SchemaInfo, error) {
			return SchemaInfo{Version: 0}, nil
		})
		swapTreeSeam(&treeOSStat, func(string) (os.FileInfo, error) {
			return nil, os.ErrNotExist
		})
		swapTreeSeam(&treeOSReadDir, func(path string) ([]os.DirEntry, error) {
			if path == defaultRoot {
				return nil, errors.New("default root read failed")
			}
			return nil, os.ErrNotExist
		})
		Expect(NewTreeServiceWithOptions(TreeOptions{DataDir: dataDir, RootDir: rootDir}).LoadTree()).To(MatchError(ErrReadDirectory))

		swapTreeSeam(&treeOSStat, os.Stat)
		swapTreeSeam(&treeOSReadDir, os.ReadDir)
		Expect(os.WriteFile(filepath.Join(defaultRoot, "page.md"), []byte("# Page"), 0o644)).To(Succeed())
		Expect(os.WriteFile(filepath.Join(rootDir, "page.md"), []byte("# Page"), 0o644)).To(Succeed())
		legacy := edgeSectionNode(RootPageID, newFixtureSlug("root"), "Root", nil)
		legacy.Children = []*PageNode{edgePageNode(newFixturePageID("page"), newFixtureSlug("page"), "Page", legacy)}
		svc := NewTreeServiceWithOptions(TreeOptions{DataDir: dataDir, RootDir: rootDir})

		legacyTargetLoadErr := errors.New("legacy target load failed")
		swapTreeSeam(&treeLoadMarkdownFile, func(string) (*markdown.MarkdownFile, error) {
			return nil, legacyTargetLoadErr
		})
		Expect(svc.ensureLegacyRootDirReady(legacy)).To(MatchError(legacyTargetLoadErr))

		swapTreeSeam(&treeLoadMarkdownFile, markdown.LoadMarkdownFile)
		Expect(svc.ensureLegacyRootDirReady(legacy)).To(MatchError(ErrLegacyContentRemains))
		Expect(svc.ensureCurrentRootDirReady()).To(Succeed())

		swapTreeSeam(&treeOSReadFile, func(path string) ([]byte, error) {
			if filepath.Base(path) == "page.md" && filepath.Dir(path) == rootDir {
				return nil, errors.New("target read failed")
			}
			return []byte("# Page"), nil
		})
		err := directoryFileContentsDiffer(defaultRoot, rootDir)
		Expect(err).To(MatchError(ErrReadConfiguredLegacyContentPath))

		swapTreeSeam(&treeFilepathWalkDir, func(dir string, fn fs.WalkDirFunc) error {
			return fn(filepath.Join(dir, "link.md"), fakeTreeDirEntry{name: "link.md", mode: fs.ModeSymlink}, nil)
		})
		files, err := collectRelativeFiles(defaultRoot)
		Expect(err).NotTo(HaveOccurred())
		Expect(files).To(BeEmpty())

		Expect(configuredRootHasLegacyContentResult(svc, &PageNode{ID: newFixturePageID("bad"), Slug: newFixtureSlug(""), Kind: NodeKindPage})).To(MatchError(ErrSlugEmpty))

		swapTreeSeam(&treeOSReadDir, func(string) ([]os.DirEntry, error) {
			return nil, errors.New("configured read failed")
		})
		Expect(configuredRootHasLegacyContentResult(svc, nil)).To(MatchError(ErrReadDirectory))

		swapTreeSeam(&treeOSReadFile, func(string) ([]byte, error) {
			return nil, errors.New("source read failed")
		})
		Expect(legacyTargetDiffersFromNodeResult(legacyContentPath{sourceFile: "source.md", targetFile: "target.md"})).To(MatchError(ErrReadLegacyContentPath))

		rollbackSvc := newInMemoryService()
		rollbackParent := edgeSectionNode(newFixturePageID("rollback-parent"), newFixtureSlug("rollback-parent"), "Rollback Parent", rollbackSvc.tree)
		rollbackParent.WorkspaceSourcePath = newFixtureWorkspaceSourcePath("../outside")
		rollbackPage := edgePageNode(newFixturePageID("rollback-page"), newFixtureSlug("rollback-page"), "Rollback Page", rollbackParent)
		rollbackParent.Children = []*PageNode{rollbackPage}
		swapTreeSeam(&treeStoreDeletePage, func(*NodeStore, *PageNode) error { return nil })
		Expect(rollbackSvc.rollbackCreatedNodeLocked(rollbackParent, rollbackPage, true)).To(matchInvalidOp("rollbackCreatedNode"))

		lookupSvc := newInMemoryService()
		delete(lookupSvc.childSlugs, lookupSvc.tree.ID)
		Expect(lookupSvc.findChildBySlugInParentLocked(lookupSvc.tree, newFixtureSlug("missing"))).To(BeNil())
		_, err = lookupSvc.lookupPagePathLocked(newFixtureRoutePath("bad//path"), newFixtureNodeKind(""))
		Expect(err).To(MatchError(ErrInvalidRoutePath))
		_, err = lookupSvc.EnsurePagePath(newFixtureUserID("user"), newFixtureRoutePath("bad//path"), "Bad", nil)
		Expect(err).To(MatchError(ErrLookupPagePath))

		contentSvc := newInMemoryService()
		contentPage := edgePageNode(newFixturePageID("content-page"), newFixtureSlug("content-page"), "Content Page", contentSvc.tree)
		contentSvc.tree.Children = []*PageNode{contentPage}
		contentSvc.rebuildIndexesLocked()
		errFixtureServiceRelFailed := errors.New("service rel failed")
		relCalls := 0
		swapTreeSeam(&treeFilepathRel, func(string, string) (string, error) {
			relCalls++
			if relCalls <= 2 {
				return "content-page.md", nil
			}
			return "", errFixtureServiceRelFailed
		})
		_, err = contentSvc.ContentPathForNode(contentPage)
		Expect(err).To(MatchError(errFixtureServiceRelFailed))

		ensureSvc := newInMemoryService()
		swapTreeSeam(&treeFilepathRel, filepath.Rel)
		swapTreeSeam(&treeGenerateUniqueID, func() (string, error) { return "created-id", nil })
		swapTreeSeam(&treeStoreCreatePage, func(*NodeStore, *PageNode, *PageNode) error {
			return errors.New("create segment failed")
		})
		_, err = ensureSvc.EnsurePagePath(newFixtureUserID("user"), newFixtureRoutePath("created"), "Created", nil)
		Expect(err).To(MatchError(ErrCreateSegment))

		ensureReuseSvc := newInMemoryService()
		existingSection := edgeSectionNode(newFixturePageID("existing"), newFixtureSlug("existing"), "Existing", ensureReuseSvc.tree)
		ensureReuseSvc.tree.Children = []*PageNode{existingSection}
		ensureReuseSvc.rebuildIndexesLocked()
		swapTreeSeam(&treeGenerateUniqueID, func() (string, error) { return "child-id", nil })
		swapTreeSeam(&treeStoreCreatePage, func(*NodeStore, *PageNode, *PageNode) error { return nil })
		swapTreeSeam(&treeStoreSaveChildOrder, func(*NodeStore, *PageNode) error { return nil })
		ensured, err := ensureReuseSvc.EnsurePagePath(newFixtureUserID("user"), newFixtureRoutePath("existing/child"), "Child", nil)
		Expect(err).NotTo(HaveOccurred())
		Expect(ensured.Page.Parent).To(BeIdenticalTo(existingSection))

		sourceOrderSvc := newInMemoryService()
		movePage := edgePageNode(newFixturePageID("move-page"), newFixtureSlug("move-page"), "Move Page", sourceOrderSvc.tree)
		moveDest := edgeSectionNode(newFixturePageID("move-dest"), newFixtureSlug("move-dest"), "Move Dest", sourceOrderSvc.tree)
		sourceOrderSvc.tree.Children = []*PageNode{movePage, moveDest}
		sourceOrderSvc.rebuildIndexesLocked()
		swapTreeSeam(&treeStoreMoveNode, func(*NodeStore, *PageNode, *PageNode) error { return nil })
		saveCalls := 0
		swapTreeSeam(&treeStoreSaveChildOrder, func(*NodeStore, *PageNode) error {
			saveCalls++
			if saveCalls == 1 {
				return errors.New("source order failed")
			}
			return nil
		})
		err = sourceOrderSvc.MoveNode(newFixtureUserID("user"), newFixturePageID("move-page"), newFixturePageID("move-dest"), pageVersionUnchecked)
		Expect(err).To(MatchError(ErrPersistSourceChildOrder))
		Expect(err).NotTo(MatchError(ErrRollbackMovedNode))

		destRollbackSvc := newInMemoryService()
		movePage = edgePageNode(newFixturePageID("move-page"), newFixtureSlug("move-page"), "Move Page", destRollbackSvc.tree)
		moveDest = edgeSectionNode(newFixturePageID("move-dest"), newFixtureSlug("move-dest"), "Move Dest", destRollbackSvc.tree)
		destRollbackSvc.tree.Children = []*PageNode{movePage, moveDest}
		destRollbackSvc.rebuildIndexesLocked()
		moveCalls := 0
		swapTreeSeam(&treeStoreMoveNode, func(*NodeStore, *PageNode, *PageNode) error {
			moveCalls++
			if moveCalls == 2 {
				return errors.New("move back failed")
			}
			return nil
		})
		saveCalls = 0
		swapTreeSeam(&treeStoreSaveChildOrder, func(*NodeStore, *PageNode) error {
			saveCalls++
			if saveCalls == 2 {
				return errors.New("destination order failed")
			}
			return nil
		})
		err = destRollbackSvc.MoveNode(newFixtureUserID("user"), newFixturePageID("move-page"), newFixturePageID("move-dest"), pageVersionUnchecked)
		Expect(err).To(MatchError(ErrPersistDestinationChildOrder))
		Expect(err).To(MatchError(ErrRollbackMovedNode))

		syncRollbackSvc := newInMemoryService()
		movePage = edgePageNode(newFixturePageID("move-page"), newFixtureSlug("move-page"), "Move Page", syncRollbackSvc.tree)
		moveDest = edgeSectionNode(newFixturePageID("move-dest"), newFixtureSlug("move-dest"), "Move Dest", syncRollbackSvc.tree)
		syncRollbackSvc.tree.Children = []*PageNode{movePage, moveDest}
		syncRollbackSvc.rebuildIndexesLocked()
		moveCalls = 0
		swapTreeSeam(&treeStoreMoveNode, func(*NodeStore, *PageNode, *PageNode) error {
			moveCalls++
			if moveCalls == 2 {
				return errors.New("move back failed")
			}
			return nil
		})
		swapTreeSeam(&treeStoreSaveChildOrder, func(*NodeStore, *PageNode) error { return nil })
		swapTreeSeam(&treeStoreSyncMetadataIfExists, func(*NodeStore, *PageNode) error {
			return errors.New("sync failed")
		})
		err = syncRollbackSvc.MoveNode(newFixtureUserID("user"), newFixturePageID("move-page"), newFixturePageID("move-dest"), pageVersionUnchecked)
		Expect(err).To(MatchError(ErrSyncMovedNodeMetadata))
		Expect(err).To(MatchError(ErrRollbackMovedNode))
	})
})
