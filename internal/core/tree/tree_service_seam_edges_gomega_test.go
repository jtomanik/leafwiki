package tree

import (
	"errors"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/perber/wiki/internal/core/markdown"
	"github.com/perber/wiki/internal/core/treemigration"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("tree service migration and store seam failure behavior", Label("unit"), func() {
	It("LoadTree reports migration and reconstruction seam failures", func() {
		svc := NewTreeService(tempTreeDir())
		schemaErr := errors.New("schema failed")
		swapTreeSeam(&treeLoadSchema, func(string) (SchemaInfo, error) {
			return SchemaInfo{}, schemaErr
		})
		Expect(svc.LoadTree()).To(MatchError(schemaErr))

		swapTreeSeam(&treeLoadSchema, func(string) (SchemaInfo, error) {
			return SchemaInfo{Version: CurrentSchemaVersion}, nil
		})
		reconstructErr := errors.New("reconstruct failed")
		swapTreeSeam(&treeStoreReconstructTreeFromFS, func(*NodeStore) (*PageNode, error) {
			return nil, reconstructErr
		})
		Expect(svc.LoadTree()).To(MatchError(reconstructErr))

		swapTreeSeam(&treeStoreReconstructTreeFromFS, func(*NodeStore) (*PageNode, error) {
			return nil, nil
		})
		Expect(svc.LoadTree()).To(MatchError(ErrTreeReconstructionNil))

		root := edgeSectionNode(RootPageID, "root", "Root", nil)
		swapTreeSeam(&treeLoadSchema, func(string) (SchemaInfo, error) {
			return SchemaInfo{Version: 0}, nil
		})
		swapTreeSeam(&treeStoreReconstructTreeFromFS, func(*NodeStore) (*PageNode, error) {
			return root, nil
		})
		migrationErr := errors.New("migration failed")
		swapTreeSeam(&treeRunMigration, func(int, treemigration.Dependencies) error {
			return migrationErr
		})
		Expect(svc.LoadTree()).To(MatchError(migrationErr))

		swapTreeSeam(&treeRunMigration, func(int, treemigration.Dependencies) error { return nil })
		calls := 0
		finalReconstructErr := errors.New("final reconstruct failed")
		swapTreeSeam(&treeStoreReconstructTreeFromFS, func(*NodeStore) (*PageNode, error) {
			calls++
			if calls == 1 {
				return root, nil
			}
			return nil, finalReconstructErr
		})
		Expect(svc.LoadTree()).To(MatchError(finalReconstructErr))

		calls = 0
		swapTreeSeam(&treeStoreReconstructTreeFromFS, func(*NodeStore) (*PageNode, error) {
			calls++
			if calls == 1 {
				return root, nil
			}
			return nil, nil
		})
		Expect(svc.LoadTree()).To(MatchError(ErrTreeReconstructionNil))
	})

	It("LoadTree falls back to legacy data and cleans stale state", func() {
		dataDir := tempTreeDir()
		legacyPath := filepath.Join(dataDir, legacyTreeFilename)
		root := edgeSectionNode(RootPageID, "root", "Root", nil)

		swapTreeSeam(&treeLoadSchema, func(string) (SchemaInfo, error) {
			return SchemaInfo{Version: 0}, nil
		})
		swapTreeSeam(&treeOSStat, func(path string) (os.FileInfo, error) {
			if path == legacyPath {
				return fakeTreeFileInfo{name: legacyTreeFilename}, nil
			}
			return nil, os.ErrNotExist
		})
		swapTreeSeam(&treeLoadLegacyTreeSnapshot, func(string, string, *slog.Logger) (*PageNode, error) {
			return nil, errors.New("legacy load failed")
		})
		fallbackReconstructErr := errors.New("fallback reconstruct failed")
		swapTreeSeam(&treeStoreReconstructTreeFromFS, func(*NodeStore) (*PageNode, error) {
			return nil, fallbackReconstructErr
		})
		Expect(NewTreeService(dataDir).LoadTree()).To(MatchError(fallbackReconstructErr))

		swapTreeSeam(&treeStoreReconstructTreeFromFS, func(*NodeStore) (*PageNode, error) {
			return nil, nil
		})
		Expect(NewTreeService(dataDir).LoadTree()).To(MatchError(ErrTreeReconstructionNil))

		swapTreeSeam(&treeOSStat, func(string) (os.FileInfo, error) {
			return nil, os.ErrNotExist
		})
		currentReconstructErr := errors.New("current reconstruct failed")
		swapTreeSeam(&treeStoreReconstructTreeFromFS, func(*NodeStore) (*PageNode, error) {
			return nil, currentReconstructErr
		})
		Expect(NewTreeService(dataDir).LoadTree()).To(MatchError(currentReconstructErr))

		reconstructCalls := 0
		swapTreeSeam(&treeStoreReconstructTreeFromFS, func(*NodeStore) (*PageNode, error) {
			reconstructCalls++
			return root, nil
		})
		swapTreeSeam(&treeRunMigration, func(int, treemigration.Dependencies) error { return nil })
		swapTreeSeam(&treeOSRemove, func(string) error {
			return errors.New("remove legacy snapshot failed")
		})
		Expect(NewTreeService(dataDir).LoadTree()).To(Succeed())
		Expect(reconstructCalls).To(Equal(2))
	})

	It("reconstruction seams roll back state and compare legacy roots", func() {
		svc := newInMemoryService()
		oldTree := svc.tree

		reconstructErr := errors.New("reconstruct failed")
		swapTreeSeam(&treeStoreReconstructTreeFromFS, func(*NodeStore) (*PageNode, error) {
			return nil, reconstructErr
		})
		Expect(svc.ReconstructTreeFromFS()).To(MatchError(reconstructErr))

		swapTreeSeam(&treeStoreReconstructTreeFromFS, func(*NodeStore) (*PageNode, error) {
			return nil, nil
		})
		Expect(svc.ReconstructTreeFromFS()).To(MatchError(ErrTreeReconstructionNil))

		newTree := edgeSectionNode(RootPageID, "root", "New Root", nil)
		swapTreeSeam(&treeStoreReconstructTreeFromFS, func(*NodeStore) (*PageNode, error) {
			return newTree, nil
		})
		saveSchemaErr := errors.New("save schema failed")
		swapTreeSeam(&treeSaveSchema, func(string, int) error {
			return saveSchemaErr
		})
		Expect(svc.ReconstructTreeFromFS()).To(MatchError(saveSchemaErr))
		Expect(svc.tree).To(BeIdenticalTo(oldTree))

		errFixtureInfoFailed := errors.New("info failed")
		swapTreeSeam(&treeFilepathWalkDir, func(dir string, fn fs.WalkDirFunc) error {
			return fn(filepath.Join(dir, "bad.md"), fakeTreeDirEntry{name: "bad.md", infoErr: errFixtureInfoFailed}, nil)
		})
		_, err := collectRelativeFiles("/legacy")
		Expect(err).To(MatchError(errFixtureInfoFailed))

		swapTreeSeam(&treeFilepathWalkDir, func(dir string, fn fs.WalkDirFunc) error {
			return fn(filepath.Join(dir, "bad.md"), fakeTreeDirEntry{name: "bad.md"}, nil)
		})
		errFixtureRelFailed := errors.New("rel failed")
		swapTreeSeam(&treeFilepathRel, func(string, string) (string, error) {
			return "", errFixtureRelFailed
		})
		_, err = collectRelativeFiles("/legacy")
		Expect(err).To(MatchError(errFixtureRelFailed))

		swapTreeSeam(&treeFilepathRel, filepath.Rel)
		swapTreeSeam(&treeOSReadFile, func(path string) ([]byte, error) {
			if filepath.Base(path) == "target.md" {
				return nil, errors.New("target read failed")
			}
			return []byte("# Source"), nil
		})
		Expect(filesHaveDifferentContent("/legacy/source.md", "/configured/target.md")).To(MatchError(ErrReadConfiguredLegacyContentPath))

		swapTreeSeam(&treeFilepathAbs, func(string) (string, error) {
			return "", errors.New("abs failed")
		})
		Expect(cleanPathsDiffer("/a/../b", "/other")).To(Succeed())
	})

	It("configured legacy content comparison reports mismatches", func() {
		base := tempTreeDir()
		dataDir := filepath.Join(base, "data")
		rootDir := filepath.Join(base, "configured")
		defaultRoot := filepath.Join(dataDir, "root")
		svc := NewTreeServiceWithOptions(TreeOptions{DataDir: dataDir, RootDir: rootDir})
		legacy := edgeSectionNode(RootPageID, "root", "Root", nil)
		page := edgePageNode("page", "page", "Page", legacy)
		legacy.Children = []*PageNode{page}

		swapTreeSeam(&treeOSStat, func(path string) (os.FileInfo, error) {
			if path == filepath.Join(defaultRoot, "page.md") {
				return fakeTreeFileInfo{mode: fs.ModeDir}, nil
			}
			return nil, os.ErrNotExist
		})
		swapTreeSeam(&treeOSReadDir, func(string) ([]os.DirEntry, error) {
			return nil, errors.New("configured read failed")
		})
		Expect(configuredRootHasLegacyContentResult(svc, legacy)).To(MatchError(ErrReadDirectory))

		swapTreeSeam(&treeOSStat, func(path string) (os.FileInfo, error) {
			switch path {
			case filepath.Join(defaultRoot, "page.md"):
				return fakeTreeFileInfo{}, nil
			case filepath.Join(rootDir, "page.md"):
				return nil, errors.New("target stat failed")
			default:
				return nil, os.ErrNotExist
			}
		})
		Expect(configuredRootHasLegacyContentResult(svc, legacy)).To(MatchError(ErrStatConfiguredLegacyContentPath))

		swapTreeSeam(&treeOSStat, func(path string) (os.FileInfo, error) {
			switch path {
			case filepath.Join(defaultRoot, "page.md"):
				return fakeTreeFileInfo{}, nil
			case filepath.Join(rootDir, "page.md"):
				return fakeTreeFileInfo{mode: fs.ModeDir}, nil
			default:
				return nil, os.ErrNotExist
			}
		})
		Expect(configuredRootMissingLegacyContentResult(svc, legacy)).To(Succeed())

		swapTreeSeam(&treeOSStat, func(path string) (os.FileInfo, error) {
			switch path {
			case filepath.Join(defaultRoot, "page.md"), filepath.Join(rootDir, "page.md"):
				return fakeTreeFileInfo{}, nil
			default:
				return nil, os.ErrNotExist
			}
		})
		swapTreeSeam(&treeOSReadFile, func(path string) ([]byte, error) {
			if path == filepath.Join(rootDir, "page.md") {
				return []byte("# Other"), nil
			}
			return []byte("# Page"), nil
		})
		Expect(configuredRootMissingLegacyContentResult(svc, legacy)).To(Succeed())

		swapTreeSeam(&treeOSReadFile, func(string) ([]byte, error) {
			return []byte("# Page"), nil
		})
		swapTreeSeam(&treeLoadMarkdownFile, func(string) (*markdown.MarkdownFile, error) {
			return nil, errors.New("load target failed")
		})
		Expect(configuredRootHasLegacyContentResult(svc, legacy)).To(MatchError(ErrLoadConfiguredLegacyContentPath))

		badParent := edgeSectionNode(RootPageID, "root", "Root", nil)
		badParent.Children = []*PageNode{edgePageNode("bad", "", "Bad", badParent)}
		_, err := svc.expectedLegacyContentPaths(badParent)
		Expect(err).To(MatchError(ErrSlugEmpty))
	})

	It("legacy-root readiness helpers identify migration prerequisites", func() {
		base := tempTreeDir()
		dataDir := filepath.Join(base, "data")
		rootDir := filepath.Join(base, "configured")
		defaultRoot := filepath.Join(dataDir, "root")
		Expect(os.MkdirAll(defaultRoot, 0o755)).To(Succeed())
		Expect(os.MkdirAll(rootDir, 0o755)).To(Succeed())
		svc := NewTreeServiceWithOptions(TreeOptions{DataDir: dataDir, RootDir: rootDir})
		legacy := edgeSectionNode(RootPageID, "root", "Root", nil)

		swapTreeSeam(&treeOSReadDir, func(path string) ([]os.DirEntry, error) {
			if path == defaultRoot {
				return nil, errors.New("read default failed")
			}
			return []os.DirEntry{}, nil
		})
		Expect(svc.ensureLegacyRootDirReady(legacy)).To(MatchError(ErrReadDirectory))
		Expect(svc.ensureCurrentRootDirReady()).To(MatchError(ErrReadDirectory))

		swapTreeSeam(&treeOSReadDir, func(path string) ([]os.DirEntry, error) {
			if path == defaultRoot {
				return []os.DirEntry{fakeTreeDirEntry{name: "page.md"}}, nil
			}
			return []os.DirEntry{}, nil
		})
		swapTreeSeam(&treeFilepathWalkDir, func(string, fs.WalkDirFunc) error {
			return errors.New("walk failed")
		})
		Expect(svc.ensureLegacyRootDirReady(legacy)).To(MatchError(ErrCollectLegacyContentFiles))
		Expect(svc.ensureCurrentRootDirReady()).To(MatchError(ErrCollectLegacyContentFiles))

		swapTreeSeam(&treeFilepathWalkDir, filepath.WalkDir)
		Expect(os.WriteFile(filepath.Join(defaultRoot, "page.md"), []byte("# Page"), 0o644)).To(Succeed())
		Expect(svc.ensureCurrentRootDirReady()).To(MatchError(ErrLegacyContentRemains))
		Expect(svc.ensureLegacyRootDirReady(legacy)).To(MatchError(ErrLegacyContentRemains))

		swapTreeSeam(&treeOSStat, func(path string) (os.FileInfo, error) {
			if filepath.Base(path) == "page.md" {
				return nil, errors.New("stat failed")
			}
			return fakeTreeFileInfo{}, nil
		})
		pageLegacy := edgeSectionNode(RootPageID, "root", "Root", nil)
		pageLegacy.Children = []*PageNode{edgePageNode("page", "page", "Page", pageLegacy)}
		Expect(configuredRootHasLegacyContentResult(svc, pageLegacy)).To(MatchError(ErrStatLegacyContentPath))
	})

	It("create and restore operations roll back on store seam errors", func() {
		svc := newInMemoryService()
		pageKind := NodeKindPage

		unloaded := NewTreeService(tempTreeDir())
		_, err := unloaded.CreateNode("user", nil, "Unloaded", "unloaded", &pageKind)
		Expect(err).To(MatchError(ErrTreeNotLoaded))

		_, err = svc.CreateNode("user", nil, "Bad", "", &pageKind)
		Expect(err).To(matchInvalidOp("CreateNode"))

		missingParent := PageID("missing")
		_, err = svc.CreateNode("user", &missingParent, "Child", "child", &pageKind)
		Expect(err).To(MatchError(ErrParentNotFound))

		existing := edgePageNode("existing", "existing", "Existing", svc.tree)
		svc.tree.Children = append(svc.tree.Children, existing)
		svc.rebuildIndexesLocked()
		_, err = svc.CreateNode("user", nil, "Existing", "existing", &pageKind)
		Expect(err).To(MatchError(ErrPageAlreadyExists))

		swapTreeSeam(&treeGenerateUniqueID, func() (string, error) {
			return "", errors.New("id failed")
		})
		_, err = svc.CreateNode("user", nil, "Generated", "generated", &pageKind)
		Expect(err).To(MatchError(ErrGenerateUniqueID))

		swapTreeSeam(&treeGenerateUniqueID, func() (string, error) { return "new-id", nil })
		swapTreeSeam(&treeStoreCreatePage, func(*NodeStore, *PageNode, *PageNode) error {
			return errors.New("create failed")
		})
		_, err = svc.CreateNode("user", nil, "Created", "created", &pageKind)
		Expect(err).To(MatchError(ErrCreatePageEntry))

		swapTreeSeam(&treeStoreCreatePage, func(*NodeStore, *PageNode, *PageNode) error { return nil })
		swapTreeSeam(&treeStoreSaveChildOrder, func(*NodeStore, *PageNode) error {
			return errors.New("order failed")
		})
		swapTreeSeam(&treeStoreDeletePage, func(*NodeStore, *PageNode) error { return nil })
		_, err = svc.CreateNode("user", nil, "Rollback", "rollback", &pageKind)
		Expect(err).To(MatchError(ErrPersistChildOrder))

		swapTreeSeam(&treeStoreDeletePage, func(*NodeStore, *PageNode) error {
			return errors.New("delete failed")
		})
		_, err = svc.CreateNode("user", nil, "Rollback Fail", "rollback-fail", &pageKind)
		Expect(err).To(MatchError(ErrPersistChildOrder))
		Expect(err).To(MatchError(ErrRollbackCreatedNode))

		swapTreeSeam(&treeStoreSaveChildOrder, func(*NodeStore, *PageNode) error { return nil })
		swapTreeSeam(&treeStoreCreatePage, func(*NodeStore, *PageNode, *PageNode) error { return nil })
		swapTreeSeam(&treeStoreUpsertContent, func(*NodeStore, *PageNode, string) error {
			return errors.New("upsert failed")
		})
		_, err = svc.RestoreNode("user", "restored", nil, "Restored", "restored", NodeKindPage, "body", PageMetadata{})
		Expect(err).To(MatchError(ErrRestoreContent))

		swapTreeSeam(&treeStoreUpsertContent, func(*NodeStore, *PageNode, string) error { return nil })
		swapTreeSeam(&treeStoreSyncMetadataIfExists, func(*NodeStore, *PageNode) error {
			return errors.New("sync failed")
		})
		_, err = svc.RestoreNode("user", "restored-sync", nil, "Restored Sync", "restored-sync", NodeKindPage, "body", PageMetadata{})
		Expect(err).To(MatchError(ErrSyncRestoredMetadata))
	})

	It("create rollback, delete, and convert seams preserve service state", func() {
		pageKind := NodeKindPage
		sectionKind := NodeKindSection

		invalidRootSvc := newInMemoryService()
		invalidRootSvc.tree.Kind = NodeKindPage
		_, err := invalidRootSvc.CreateNode("user", nil, "Child", "child", &pageKind)
		Expect(err).To(MatchError(ErrParentMustBeSection))

		convertParentSvc := newInMemoryService()
		parentPage := edgePageNode("parent-page", "parent-page", "Parent Page", convertParentSvc.tree)
		convertParentSvc.tree.Children = []*PageNode{parentPage}
		convertParentSvc.rebuildIndexesLocked()
		swapTreeSeam(&treeStoreConvertNode, func(*NodeStore, *PageNode, NodeKind) error {
			return errors.New("convert parent failed")
		})
		_, err = convertParentSvc.CreateNode("user", &parentPage.ID, "Child", "child", &pageKind)
		Expect(err).To(MatchError(ErrConvertParentNode))

		duplicateIDSvc := newInMemoryService()
		existing := edgePageNode("existing", "existing", "Existing", duplicateIDSvc.tree)
		duplicateIDSvc.tree.Children = []*PageNode{existing}
		duplicateIDSvc.rebuildIndexesLocked()
		_, err = duplicateIDSvc.RestoreNode("user", "existing", nil, "Existing", "restored", NodeKindPage, "body", PageMetadata{})
		Expect(err).To(MatchError(ErrPageAlreadyExists))

		createSectionSvc := newInMemoryService()
		swapTreeSeam(&treeGenerateUniqueID, func() (string, error) { return "section-id", nil })
		swapTreeSeam(&treeStoreCreateSection, func(*NodeStore, *PageNode, *PageNode) error {
			return errors.New("create section failed")
		})
		_, err = createSectionSvc.CreateNode("user", nil, "Section", "section", &sectionKind)
		Expect(err).To(MatchError(ErrCreateSectionEntry))

		rollbackSvc := newInMemoryService()
		Expect(rollbackSvc.rollbackCreatedNodeLocked(nil, nil, true)).To(Succeed())

		rollbackParent := edgeSectionNode("rollback-parent", "rollback-parent", "Rollback Parent", rollbackSvc.tree)
		rollbackSection := edgeSectionNode("rollback-section", "rollback-section", "Rollback Section", rollbackParent)
		rollbackParent.Children = []*PageNode{rollbackSection}
		deleteSectionErr := errors.New("delete section failed")
		swapTreeSeam(&treeStoreDeleteSection, func(*NodeStore, *PageNode) error {
			return deleteSectionErr
		})
		Expect(rollbackSvc.rollbackCreatedNodeLocked(rollbackParent, rollbackSection, false)).To(MatchError(deleteSectionErr))

		rollbackParent = edgeSectionNode("rollback-parent", "rollback-parent", "Rollback Parent", rollbackSvc.tree)
		rollbackPage := edgePageNode("rollback-page", "rollback-page", "Rollback Page", rollbackParent)
		rollbackParent.Children = []*PageNode{rollbackPage}
		swapTreeSeam(&treeStoreDeletePage, func(*NodeStore, *PageNode) error { return nil })
		removeOrderErr := errors.New("remove order failed")
		swapTreeSeam(&treeOSRemove, func(string) error {
			return removeOrderErr
		})
		Expect(rollbackSvc.rollbackCreatedNodeLocked(rollbackParent, rollbackPage, true)).To(MatchError(removeOrderErr))

		rollbackParent = edgeSectionNode("rollback-parent", "rollback-parent", "Rollback Parent", rollbackSvc.tree)
		rollbackPage = edgePageNode("rollback-page", "rollback-page", "Rollback Page", rollbackParent)
		rollbackParent.Children = []*PageNode{rollbackPage}
		swapTreeSeam(&treeOSRemove, func(string) error { return os.ErrNotExist })
		foldBackErr := errors.New("fold back failed")
		swapTreeSeam(&treeStoreConvertNode, func(*NodeStore, *PageNode, NodeKind) error {
			return foldBackErr
		})
		Expect(rollbackSvc.rollbackCreatedNodeLocked(rollbackParent, rollbackPage, true)).To(MatchError(foldBackErr))

		rollbackParent = edgeSectionNode("rollback-parent", "rollback-parent", "Rollback Parent", rollbackSvc.tree)
		rollbackPage = edgePageNode("rollback-page", "rollback-page", "Rollback Page", rollbackParent)
		rollbackParent.Children = []*PageNode{rollbackPage}
		swapTreeSeam(&treeStoreConvertNode, func(*NodeStore, *PageNode, NodeKind) error { return nil })
		Expect(rollbackSvc.rollbackCreatedNodeLocked(rollbackParent, rollbackPage, true)).To(Succeed())
		Expect(rollbackParent.Kind).To(Equal(NodeKindPage))

		orphanSvc := newInMemoryService()
		orphan := edgePageNode("orphan", "orphan", "Orphan", nil)
		orphanSvc.nodesByID[orphan.ID] = orphan
		Expect(orphanSvc.DeleteNode("user", "orphan", false, pageVersionUnchecked)).To(MatchError(ErrParentNotFound))

		deletePageWithChildrenSvc := newInMemoryService()
		pageWithChildren := edgePageNode("page-with-children", "page-with-children", "Page With Children", deletePageWithChildrenSvc.tree)
		child := edgePageNode("child", "child", "Child", pageWithChildren)
		pageWithChildren.Children = []*PageNode{child}
		deletePageWithChildrenSvc.tree.Children = []*PageNode{pageWithChildren}
		deletePageWithChildrenSvc.rebuildIndexesLocked()
		convertPageErr := errors.New("convert page failed")
		swapTreeSeam(&treeStoreConvertNode, func(*NodeStore, *PageNode, NodeKind) error {
			return convertPageErr
		})
		Expect(deletePageWithChildrenSvc.DeleteNode("user", pageWithChildren.ID, true, pageVersionUnchecked)).To(MatchError(convertPageErr))

		deletePageWithChildrenSvc = newInMemoryService()
		pageWithChildren = edgePageNode("page-with-children", "page-with-children", "Page With Children", deletePageWithChildrenSvc.tree)
		child = edgePageNode("child", "child", "Child", pageWithChildren)
		pageWithChildren.Children = []*PageNode{child}
		deletePageWithChildrenSvc.tree.Children = []*PageNode{pageWithChildren}
		deletePageWithChildrenSvc.rebuildIndexesLocked()
		swapTreeSeam(&treeStoreConvertNode, func(*NodeStore, *PageNode, NodeKind) error { return nil })
		deleteConvertedSectionErr := errors.New("delete converted section failed")
		swapTreeSeam(&treeStoreDeleteSection, func(*NodeStore, *PageNode) error {
			return deleteConvertedSectionErr
		})
		Expect(deletePageWithChildrenSvc.DeleteNode("user", pageWithChildren.ID, true, pageVersionUnchecked)).To(MatchError(deleteConvertedSectionErr))

		deleteOrderSvc := newInMemoryService()
		deletedPage := edgePageNode("deleted-page", "deleted-page", "Deleted Page", deleteOrderSvc.tree)
		deleteOrderSvc.tree.Children = []*PageNode{deletedPage}
		deleteOrderSvc.rebuildIndexesLocked()
		swapTreeSeam(&treeStoreDeletePage, func(*NodeStore, *PageNode) error { return nil })
		deleteOrderErr := errors.New("delete order failed")
		swapTreeSeam(&treeStoreSaveChildOrder, func(*NodeStore, *PageNode) error {
			return deleteOrderErr
		})
		Expect(deleteOrderSvc.DeleteNode("user", deletedPage.ID, false, pageVersionUnchecked)).To(MatchError(deleteOrderErr))

		convertSvc := newInMemoryService()
		page := edgePageNode("page", "page", "Page", convertSvc.tree)
		section := edgeSectionNode("section", "section", "Section", convertSvc.tree)
		section.Children = []*PageNode{edgePageNode("section-child", "section-child", "Section Child", section)}
		convertSvc.tree.Children = []*PageNode{page, section}
		convertSvc.rebuildIndexesLocked()
		Expect(convertSvc.UpdateNode("user", "missing", "Missing", "missing", nil, pageVersionUnchecked, false)).To(MatchError(ErrPageNotFound))
		Expect(convertSvc.ConvertNode("user", "missing", NodeKindSection, pageVersionUnchecked)).To(MatchError(ErrPageNotFound))
		Expect(convertSvc.ConvertNode("user", "page", NodeKindPage, pageVersionUnchecked)).To(Succeed())
		Expect(convertSvc.ConvertNode("user", "section", NodeKindPage, pageVersionUnchecked)).To(MatchError(ErrPageHasChildren))
	})

	It("update, delete, convert, move, and sort operations propagate orchestration failures", func() {
		svc := newInMemoryService()
		page := edgePageNode("page", "page", "Page", svc.tree)
		page.Metadata.UpdatedAt = time.Now().UTC()
		section := edgeSectionNode("section", "section", "Section", svc.tree)
		child := edgePageNode("child", "child", "Child", section)
		section.Children = []*PageNode{child}
		child.Parent = section
		svc.tree.Children = []*PageNode{page, section}
		svc.rebuildIndexesLocked()

		Expect(svc.DeleteNode("user", "missing", false, pageVersionUnchecked)).To(MatchError(ErrPageNotFound))
		Expect(svc.DeleteNode("user", "section", false, pageVersionUnchecked)).To(MatchError(ErrPageHasChildren))

		deletePageErr := errors.New("delete page failed")
		swapTreeSeam(&treeStoreDeletePage, func(*NodeStore, *PageNode) error {
			return deletePageErr
		})
		Expect(svc.DeleteNode("user", "page", false, pageVersionUnchecked)).To(MatchError(deletePageErr))

		swapTreeSeam(&treeStoreDeletePage, func(*NodeStore, *PageNode) error { return nil })
		deleteSectionErr := errors.New("delete section failed")
		swapTreeSeam(&treeStoreDeleteSection, func(*NodeStore, *PageNode) error {
			return deleteSectionErr
		})
		Expect(svc.DeleteNode("user", "section", true, pageVersionUnchecked)).To(MatchError(deleteSectionErr))

		swapTreeSeam(&treeStoreDeleteSection, func(*NodeStore, *PageNode) error { return nil })
		unknown := edgePageNode("unknown", "unknown", "Unknown", svc.tree)
		unknown.Kind = NodeKind("unknown")
		svc.tree.Children = append(svc.tree.Children, unknown)
		svc.rebuildIndexesLocked()
		Expect(svc.DeleteNode("user", "unknown", false, pageVersionUnchecked)).To(matchInvalidOp("DeleteNode"))

		content := "body"
		plainContentErr := errors.New("plain failed")
		swapTreeSeam(&treeStoreUpsertContent, func(*NodeStore, *PageNode, string) error {
			return plainContentErr
		})
		Expect(svc.UpdateNode("user", "page", "Page", "page", &content, pageVersionUnchecked, false)).To(MatchError(plainContentErr))

		swapTreeSeam(&treeStoreUpsertContent, func(*NodeStore, *PageNode, string) error { return nil })
		preserveContentErr := errors.New("preserve failed")
		swapTreeSeam(&treeStoreUpsertContentPreservingFrontmatter, func(*NodeStore, *PageNode, string) error {
			return preserveContentErr
		})
		Expect(svc.UpdateNode("user", "page", "Page", "page", &content, pageVersionUnchecked, true)).To(MatchError(preserveContentErr))

		swapTreeSeam(&treeStoreUpsertContentPreservingFrontmatter, func(*NodeStore, *PageNode, string) error { return nil })
		replaceContentErr := errors.New("replace failed")
		swapTreeSeam(&treeStoreUpsertContentReplacingMetadata, func(*NodeStore, *PageNode, string) error {
			return replaceContentErr
		})
		Expect(svc.UpdateNodeReplacingMetadata("user", "page", "Page", "page", &content, pageVersionUnchecked)).To(MatchError(replaceContentErr))

		swapTreeSeam(&treeStoreUpsertContentReplacingMetadata, func(*NodeStore, *PageNode, string) error { return nil })
		renameErr := errors.New("rename failed")
		swapTreeSeam(&treeStoreRenameNode, func(*NodeStore, *PageNode, Slug) error {
			return renameErr
		})
		Expect(svc.UpdateNode("user", "page", "Page", "renamed", nil, pageVersionUnchecked, false)).To(MatchError(renameErr))

		swapTreeSeam(&treeStoreRenameNode, func(*NodeStore, *PageNode, Slug) error { return nil })
		syncErr := errors.New("sync failed")
		swapTreeSeam(&treeStoreSyncMetadataIfExists, func(*NodeStore, *PageNode) error {
			return syncErr
		})
		Expect(svc.UpdateNode("user", "page", "Page", "page", nil, pageVersionUnchecked, false)).To(MatchError(syncErr))
		Expect(svc.ConvertNode("user", "page", NodeKindSection, pageVersionUnchecked)).To(MatchError(syncErr))
		page.Kind = NodeKindPage

		swapTreeSeam(&treeStoreSyncMetadataIfExists, func(*NodeStore, *PageNode) error { return nil })
		convertErr := errors.New("convert failed")
		swapTreeSeam(&treeStoreConvertNode, func(*NodeStore, *PageNode, NodeKind) error {
			return convertErr
		})
		Expect(svc.ConvertNode("user", "page", NodeKindSection, pageVersionUnchecked)).To(MatchError(convertErr))

		swapTreeSeam(&treeStoreConvertNode, func(*NodeStore, *PageNode, NodeKind) error { return nil })
		Expect(svc.MoveNode("user", "missing", RootPageID, pageVersionUnchecked)).To(MatchError(ErrPageNotFound))
		Expect(svc.MoveNode("user", "page", "missing-parent", pageVersionUnchecked)).To(MatchError(ErrParentNotFound))
		Expect(svc.MoveNode("user", "page", "page", pageVersionUnchecked)).To(MatchError(ErrPageCannotBeMovedToItself))
		Expect(svc.MoveNode("user", "section", "child", pageVersionUnchecked)).To(MatchError(ErrMovePageCircularReference))

		moveErr := errors.New("move failed")
		swapTreeSeam(&treeStoreMoveNode, func(*NodeStore, *PageNode, *PageNode) error {
			return moveErr
		})
		Expect(svc.MoveNode("user", "page", "section", pageVersionUnchecked)).To(MatchError(moveErr))

		swapTreeSeam(&treeStoreMoveNode, func(*NodeStore, *PageNode, *PageNode) error { return nil })
		sourceOrderErr := errors.New("order failed")
		swapTreeSeam(&treeStoreSaveChildOrder, func(*NodeStore, *PageNode) error {
			return sourceOrderErr
		})
		Expect(svc.MoveNode("user", "page", "section", pageVersionUnchecked)).To(MatchError(sourceOrderErr))

		sortSvc := newInMemoryService()
		sortPage := edgePageNode("sort-page", "sort-page", "Sort Page", sortSvc.tree)
		sortSection := edgeSectionNode("sort-section", "sort-section", "Sort Section", sortSvc.tree)
		sortSvc.tree.Children = []*PageNode{sortPage, sortSection}
		sortSvc.rebuildIndexesLocked()
		Expect(sortSvc.SortPages("missing", nil)).To(MatchError(ErrParentNotFound))
		Expect(sortSvc.SortPages(RootPageID, []PageID{"only-one"})).To(MatchError(ErrInvalidSortOrder))
		Expect(sortSvc.SortPages(RootPageID, []PageID{"sort-page", "not-present"})).To(MatchError(ErrInvalidSortOrder))
		Expect(sortSvc.SortPages(RootPageID, []PageID{"sort-page", "sort-page"})).To(MatchError(ErrInvalidSortOrder))
	})

	It("batch, lookup, ensure, and move operations roll back through service seams", func() {
		svc := newInMemoryService()
		pageKind := NodeKindPage
		page := edgePageNode("page", "page", "Page", svc.tree)
		section := edgeSectionNode("section", "section", "Section", svc.tree)
		destPage := edgePageNode("dest-page", "dest-page", "Dest Page", svc.tree)
		svc.tree.Children = []*PageNode{page, section, destPage}
		svc.rebuildIndexesLocked()

		Expect(svc.BulkUpdateContent("user", nil)).To(BeEmpty())
		bulkErrs := svc.BulkUpdateContent("user", []BulkContentUpdate{{ID: "missing", Content: "body"}})
		Expect(bulkErrs).To(ConsistOf(MatchError(ErrPageNotFound)))
		pages, pageErrs := svc.GetPages(nil)
		Expect(pages).To(BeEmpty())
		Expect(pageErrs).To(BeEmpty())
		pages, pageErrs = svc.GetPages([]PageID{"missing"})
		Expect(pages).To(Equal([]*Page{nil}))
		Expect(pageErrs).To(ConsistOf(MatchError(ErrPageNotFound)))

		errFixtureRawFailed := errors.New("raw failed")
		swapTreeSeam(&treeStoreReadPageRaw, func(*NodeStore, *PageNode) (string, error) {
			return "", errFixtureRawFailed
		})
		_, err := svc.ReadPageRaw("page")
		Expect(err).To(MatchError(ErrGetPageRawContent))

		unloaded := NewTreeService(tempTreeDir())
		_, err = unloaded.FindPageByRoutePath("page")
		Expect(err).To(MatchError(ErrTreeNotLoaded))
		_, err = svc.LookupPagePath("")
		Expect(err).To(MatchError(ErrMissingRoutePath))
		_, err = svc.LookupPagePathForKind("", NodeKindPage)
		Expect(err).To(MatchError(ErrMissingRoutePath))

		swapTreeSeam(&treeStoreReadPageContent, func(*NodeStore, *PageNode) (string, error) {
			return "", errors.New("content failed")
		})
		_, err = svc.FindPageByRoutePath("page")
		Expect(err).To(MatchError(ErrGetPageContent))

		errFixtureRelFailed := errors.New("rel failed")
		relCalls := 0
		swapTreeSeam(&treeFilepathRel, func(string, string) (string, error) {
			relCalls++
			if relCalls == 1 {
				return "page.md", nil
			}
			return "", errFixtureRelFailed
		})
		_, err = svc.ContentPathForNode(page)
		Expect(err).To(MatchError(errFixtureRelFailed))

		missingIDPageSvc := newInMemoryService()
		emptyIDPage := edgePageNode("", "empty-id", "Empty ID", missingIDPageSvc.tree)
		missingIDPageSvc.tree.Children = []*PageNode{emptyIDPage}
		missingIDPageSvc.rebuildIndexesLocked()
		_, err = missingIDPageSvc.EnsurePagePath("user", "empty-id", "Empty ID", &pageKind)
		Expect(err).To(MatchError(ErrFindExistingPageByID))

		ensureSvc := newInMemoryService()
		swapTreeSeam(&treeGenerateUniqueID, func() (string, error) { return "", nil })
		swapTreeSeam(&treeStoreCreatePage, func(*NodeStore, *PageNode, *PageNode) error { return nil })
		swapTreeSeam(&treeStoreSaveChildOrder, func(*NodeStore, *PageNode) error { return nil })
		_, err = ensureSvc.EnsurePagePath("user", "created", "Created", &pageKind)
		Expect(err).To(MatchError(ErrFindCreatedPageByID))

		orphanSvc := newInMemoryService()
		orphan := edgePageNode("orphan", "orphan", "Orphan", nil)
		orphanSvc.nodesByID[orphan.ID] = orphan
		Expect(orphanSvc.MoveNode("user", "orphan", RootPageID, pageVersionUnchecked)).To(MatchError(ErrParentNotFound))

		convertDestErr := errors.New("convert dest failed")
		swapTreeSeam(&treeStoreConvertNode, func(*NodeStore, *PageNode, NodeKind) error {
			return convertDestErr
		})
		Expect(svc.MoveNode("user", "page", "dest-page", pageVersionUnchecked)).To(MatchError(convertDestErr))

		invalidDestSvc := newInMemoryService()
		movePage := edgePageNode("move-page", "move-page", "Move Page", invalidDestSvc.tree)
		invalidDest := edgeSectionNode("invalid-dest", "invalid-dest", "Invalid Dest", invalidDestSvc.tree)
		invalidDest.Kind = NodeKind("unknown")
		invalidDestSvc.tree.Children = []*PageNode{movePage, invalidDest}
		invalidDestSvc.rebuildIndexesLocked()
		Expect(invalidDestSvc.MoveNode("user", "move-page", "invalid-dest", pageVersionUnchecked)).To(MatchError(ErrParentMustBeSection))

		moveOrderSvc := newInMemoryService()
		movePage = edgePageNode("move-page", "move-page", "Move Page", moveOrderSvc.tree)
		moveDest := edgeSectionNode("move-dest", "move-dest", "Move Dest", moveOrderSvc.tree)
		moveOrderSvc.tree.Children = []*PageNode{movePage, moveDest}
		moveOrderSvc.rebuildIndexesLocked()
		swapTreeSeam(&treeStoreMoveNode, func(*NodeStore, *PageNode, *PageNode) error { return nil })
		saveCalls := 0
		destinationOrderErr := errors.New("destination order failed")
		swapTreeSeam(&treeStoreSaveChildOrder, func(*NodeStore, *PageNode) error {
			saveCalls++
			if saveCalls == 2 {
				return destinationOrderErr
			}
			return nil
		})
		Expect(moveOrderSvc.MoveNode("user", "move-page", "move-dest", pageVersionUnchecked)).To(MatchError(destinationOrderErr))

		moveSyncSvc := newInMemoryService()
		movePage = edgePageNode("move-page", "move-page", "Move Page", moveSyncSvc.tree)
		moveDest = edgeSectionNode("move-dest", "move-dest", "Move Dest", moveSyncSvc.tree)
		moveSyncSvc.tree.Children = []*PageNode{movePage, moveDest}
		moveSyncSvc.rebuildIndexesLocked()
		swapTreeSeam(&treeStoreSaveChildOrder, func(*NodeStore, *PageNode) error { return nil })
		swapTreeSeam(&treeStoreSyncMetadataIfExists, func(*NodeStore, *PageNode) error {
			return errors.New("sync moved failed")
		})
		Expect(moveSyncSvc.MoveNode("user", "move-page", "move-dest", pageVersionUnchecked)).To(MatchError(ErrSyncMovedNodeMetadata))

		rollbackSvc := newInMemoryService()
		oldParent := rollbackSvc.tree
		newParent := edgeSectionNode("new-parent", "new-parent", "New Parent", oldParent)
		node := edgePageNode("rolled", "rolled", "Rolled", newParent)
		previousOldChildren := []*PageNode{}
		previousNewChildren := []*PageNode{node}
		swapTreeSeam(&treeStoreMoveNode, func(*NodeStore, *PageNode, *PageNode) error {
			return errors.New("move back failed")
		})
		swapTreeSeam(&treeStoreSaveChildOrder, func(*NodeStore, *PageNode) error { return nil })
		err = rollbackSvc.rollbackMovedNodeLocked(node, oldParent, newParent, previousOldChildren, map[PageID]int{}, previousNewChildren, map[PageID]int{"rolled": 3}, 3, PageMetadata{}, false)
		Expect(err).To(MatchError(ErrMoveNodeBackOnDisk))

		swapTreeSeam(&treeStoreMoveNode, func(*NodeStore, *PageNode, *PageNode) error { return nil })
		convertedParent := edgeSectionNode("converted-parent", "converted-parent", "Converted Parent", oldParent)
		convertedParent.WorkspaceSourcePath = "../outside"
		err = rollbackSvc.rollbackMovedNodeLocked(node, oldParent, convertedParent, nil, map[PageID]int{}, nil, map[PageID]int{}, 0, PageMetadata{}, true)
		Expect(err).To(MatchError(ErrResolveConvertedParentDir))

		swapTreeSeam(&treeFilepathRel, filepath.Rel)
		swapTreeSeam(&treeStoreConvertNode, func(*NodeStore, *PageNode, NodeKind) error { return nil })
		convertedParent = edgeSectionNode("converted-parent", "converted-parent", "Converted Parent", oldParent)
		swapTreeSeam(&treeOSRemoveAll, func(string) error {
			return errors.New("remove order failed")
		})
		err = rollbackSvc.rollbackMovedNodeLocked(node, oldParent, convertedParent, nil, map[PageID]int{}, nil, map[PageID]int{}, 0, PageMetadata{}, true)
		Expect(err).To(MatchError(ErrRemoveChildOrderBeforeParentRollback))

		convertedParent = edgeSectionNode("converted-parent", "converted-parent", "Converted Parent", oldParent)
		swapTreeSeam(&treeOSRemoveAll, func(string) error { return nil })
		swapTreeSeam(&treeStoreConvertNode, func(*NodeStore, *PageNode, NodeKind) error {
			return errors.New("convert back failed")
		})
		err = rollbackSvc.rollbackMovedNodeLocked(node, oldParent, convertedParent, nil, map[PageID]int{}, nil, map[PageID]int{}, 0, PageMetadata{}, true)
		Expect(err).To(MatchError(ErrConvertDestinationParentBackToPage))

		convertedParent = edgeSectionNode("converted-parent", "converted-parent", "Converted Parent", oldParent)
		swapTreeSeam(&treeStoreConvertNode, func(*NodeStore, *PageNode, NodeKind) error { return nil })
		Expect(rollbackSvc.rollbackMovedNodeLocked(node, oldParent, convertedParent, nil, map[PageID]int{}, nil, map[PageID]int{}, 0, PageMetadata{}, true)).To(Succeed())
		Expect(convertedParent.Kind).To(Equal(NodeKindPage))
	})

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
		legacy := edgeSectionNode(RootPageID, "root", "Root", nil)
		legacy.Children = []*PageNode{edgePageNode("page", "page", "Page", legacy)}
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

		Expect(configuredRootHasLegacyContentResult(svc, &PageNode{ID: "bad", Slug: "", Kind: NodeKindPage})).To(MatchError(ErrSlugEmpty))

		swapTreeSeam(&treeOSReadDir, func(string) ([]os.DirEntry, error) {
			return nil, errors.New("configured read failed")
		})
		Expect(configuredRootHasLegacyContentResult(svc, nil)).To(MatchError(ErrReadDirectory))

		swapTreeSeam(&treeOSReadFile, func(string) ([]byte, error) {
			return nil, errors.New("source read failed")
		})
		Expect(legacyTargetDiffersFromNodeResult(legacyContentPath{sourceFile: "source.md", targetFile: "target.md"})).To(MatchError(ErrReadLegacyContentPath))

		rollbackSvc := newInMemoryService()
		rollbackParent := edgeSectionNode("rollback-parent", "rollback-parent", "Rollback Parent", rollbackSvc.tree)
		rollbackParent.WorkspaceSourcePath = "../outside"
		rollbackPage := edgePageNode("rollback-page", "rollback-page", "Rollback Page", rollbackParent)
		rollbackParent.Children = []*PageNode{rollbackPage}
		swapTreeSeam(&treeStoreDeletePage, func(*NodeStore, *PageNode) error { return nil })
		Expect(rollbackSvc.rollbackCreatedNodeLocked(rollbackParent, rollbackPage, true)).To(matchInvalidOp("rollbackCreatedNode"))

		lookupSvc := newInMemoryService()
		delete(lookupSvc.childSlugs, lookupSvc.tree.ID)
		Expect(lookupSvc.findChildBySlugInParentLocked(lookupSvc.tree, "missing")).To(BeNil())
		_, err = lookupSvc.lookupPagePathLocked("bad//path", "")
		Expect(err).To(MatchError(ErrInvalidRoutePath))
		_, err = lookupSvc.EnsurePagePath("user", "bad//path", "Bad", nil)
		Expect(err).To(MatchError(ErrLookupPagePath))

		contentSvc := newInMemoryService()
		contentPage := edgePageNode("content-page", "content-page", "Content Page", contentSvc.tree)
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
		_, err = ensureSvc.EnsurePagePath("user", "created", "Created", nil)
		Expect(err).To(MatchError(ErrCreateSegment))

		ensureReuseSvc := newInMemoryService()
		existingSection := edgeSectionNode("existing", "existing", "Existing", ensureReuseSvc.tree)
		ensureReuseSvc.tree.Children = []*PageNode{existingSection}
		ensureReuseSvc.rebuildIndexesLocked()
		swapTreeSeam(&treeGenerateUniqueID, func() (string, error) { return "child-id", nil })
		swapTreeSeam(&treeStoreCreatePage, func(*NodeStore, *PageNode, *PageNode) error { return nil })
		swapTreeSeam(&treeStoreSaveChildOrder, func(*NodeStore, *PageNode) error { return nil })
		ensured, err := ensureReuseSvc.EnsurePagePath("user", "existing/child", "Child", nil)
		Expect(err).NotTo(HaveOccurred())
		Expect(ensured.Page.Parent).To(BeIdenticalTo(existingSection))

		sourceOrderSvc := newInMemoryService()
		movePage := edgePageNode("move-page", "move-page", "Move Page", sourceOrderSvc.tree)
		moveDest := edgeSectionNode("move-dest", "move-dest", "Move Dest", sourceOrderSvc.tree)
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
		err = sourceOrderSvc.MoveNode("user", "move-page", "move-dest", pageVersionUnchecked)
		Expect(err).To(MatchError(ErrPersistSourceChildOrder))
		Expect(err).NotTo(MatchError(ErrRollbackMovedNode))

		destRollbackSvc := newInMemoryService()
		movePage = edgePageNode("move-page", "move-page", "Move Page", destRollbackSvc.tree)
		moveDest = edgeSectionNode("move-dest", "move-dest", "Move Dest", destRollbackSvc.tree)
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
		err = destRollbackSvc.MoveNode("user", "move-page", "move-dest", pageVersionUnchecked)
		Expect(err).To(MatchError(ErrPersistDestinationChildOrder))
		Expect(err).To(MatchError(ErrRollbackMovedNode))

		syncRollbackSvc := newInMemoryService()
		movePage = edgePageNode("move-page", "move-page", "Move Page", syncRollbackSvc.tree)
		moveDest = edgeSectionNode("move-dest", "move-dest", "Move Dest", syncRollbackSvc.tree)
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
		err = syncRollbackSvc.MoveNode("user", "move-page", "move-dest", pageVersionUnchecked)
		Expect(err).To(MatchError(ErrSyncMovedNodeMetadata))
		Expect(err).To(MatchError(ErrRollbackMovedNode))
	})
})

func newInMemoryService() *TreeService {
	GinkgoHelper()
	svc := NewTreeService(tempTreeDir())
	svc.tree = edgeSectionNode(RootPageID, "root", "Root", nil)
	svc.rebuildIndexesLocked()
	return svc
}
