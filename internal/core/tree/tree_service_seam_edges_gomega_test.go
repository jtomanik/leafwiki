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

var _ = Describe("tree service seam edge coverage", func() {
	It("covers LoadTree migration and reconstruction error branches", func() {
		svc := NewTreeService(GinkgoT().TempDir())
		swapTreeSeam(&treeLoadSchema, func(string) (SchemaInfo, error) {
			return SchemaInfo{}, errors.New("schema failed")
		})
		Expect(svc.LoadTree()).To(MatchError("schema failed"))

		swapTreeSeam(&treeLoadSchema, func(string) (SchemaInfo, error) {
			return SchemaInfo{Version: CurrentSchemaVersion}, nil
		})
		swapTreeSeam(&treeStoreReconstructTreeFromFS, func(*NodeStore) (*PageNode, error) {
			return nil, errors.New("reconstruct failed")
		})
		Expect(svc.LoadTree()).To(MatchError("reconstruct failed"))

		swapTreeSeam(&treeStoreReconstructTreeFromFS, func(*NodeStore) (*PageNode, error) {
			return nil, nil
		})
		Expect(svc.LoadTree()).To(MatchError(ContainSubstring("nil tree")))

		root := edgeSectionNode(RootPageID, "root", "Root", nil)
		swapTreeSeam(&treeLoadSchema, func(string) (SchemaInfo, error) {
			return SchemaInfo{Version: 0}, nil
		})
		swapTreeSeam(&treeStoreReconstructTreeFromFS, func(*NodeStore) (*PageNode, error) {
			return root, nil
		})
		swapTreeSeam(&treeRunMigration, func(int, treemigration.Dependencies) error {
			return errors.New("migration failed")
		})
		Expect(svc.LoadTree()).To(MatchError("migration failed"))

		swapTreeSeam(&treeRunMigration, func(int, treemigration.Dependencies) error { return nil })
		calls := 0
		swapTreeSeam(&treeStoreReconstructTreeFromFS, func(*NodeStore) (*PageNode, error) {
			calls++
			if calls == 1 {
				return root, nil
			}
			return nil, errors.New("final reconstruct failed")
		})
		Expect(svc.LoadTree()).To(MatchError("final reconstruct failed"))

		calls = 0
		swapTreeSeam(&treeStoreReconstructTreeFromFS, func(*NodeStore) (*PageNode, error) {
			calls++
			if calls == 1 {
				return root, nil
			}
			return nil, nil
		})
		Expect(svc.LoadTree()).To(MatchError(ContainSubstring("nil tree")))
	})

	It("covers LoadTree legacy fallback and cleanup branches", func() {
		dataDir := GinkgoT().TempDir()
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
		swapTreeSeam(&treeStoreReconstructTreeFromFS, func(*NodeStore) (*PageNode, error) {
			return nil, errors.New("fallback reconstruct failed")
		})
		Expect(NewTreeService(dataDir).LoadTree()).To(MatchError("fallback reconstruct failed"))

		swapTreeSeam(&treeStoreReconstructTreeFromFS, func(*NodeStore) (*PageNode, error) {
			return nil, nil
		})
		Expect(NewTreeService(dataDir).LoadTree()).To(MatchError(ContainSubstring("nil tree")))

		swapTreeSeam(&treeOSStat, func(string) (os.FileInfo, error) {
			return nil, os.ErrNotExist
		})
		swapTreeSeam(&treeStoreReconstructTreeFromFS, func(*NodeStore) (*PageNode, error) {
			return nil, errors.New("current reconstruct failed")
		})
		Expect(NewTreeService(dataDir).LoadTree()).To(MatchError("current reconstruct failed"))

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

	It("covers reconstruction rollback and legacy comparison seam branches", func() {
		svc := newInMemoryService()
		oldTree := svc.tree

		swapTreeSeam(&treeStoreReconstructTreeFromFS, func(*NodeStore) (*PageNode, error) {
			return nil, errors.New("reconstruct failed")
		})
		Expect(svc.ReconstructTreeFromFS()).To(MatchError("reconstruct failed"))

		swapTreeSeam(&treeStoreReconstructTreeFromFS, func(*NodeStore) (*PageNode, error) {
			return nil, nil
		})
		Expect(svc.ReconstructTreeFromFS()).To(MatchError(ContainSubstring("nil tree")))

		newTree := edgeSectionNode(RootPageID, "root", "New Root", nil)
		swapTreeSeam(&treeStoreReconstructTreeFromFS, func(*NodeStore) (*PageNode, error) {
			return newTree, nil
		})
		swapTreeSeam(&treeSaveSchema, func(string, int) error {
			return errors.New("save schema failed")
		})
		Expect(svc.ReconstructTreeFromFS()).To(MatchError("save schema failed"))
		Expect(svc.tree).To(BeIdenticalTo(oldTree))

		swapTreeSeam(&treeFilepathWalkDir, func(dir string, fn fs.WalkDirFunc) error {
			return fn(filepath.Join(dir, "bad.md"), fakeTreeDirEntry{name: "bad.md", infoErr: errors.New("info failed")}, nil)
		})
		_, err := collectRelativeFiles("/legacy")
		Expect(err).To(MatchError(ContainSubstring("info failed")))

		swapTreeSeam(&treeFilepathWalkDir, func(dir string, fn fs.WalkDirFunc) error {
			return fn(filepath.Join(dir, "bad.md"), fakeTreeDirEntry{name: "bad.md"}, nil)
		})
		swapTreeSeam(&treeFilepathRel, func(string, string) (string, error) {
			return "", errors.New("rel failed")
		})
		_, err = collectRelativeFiles("/legacy")
		Expect(err).To(MatchError(ContainSubstring("rel failed")))

		swapTreeSeam(&treeFilepathRel, filepath.Rel)
		swapTreeSeam(&treeOSReadFile, func(path string) ([]byte, error) {
			if filepath.Base(path) == "target.md" {
				return nil, errors.New("target read failed")
			}
			return []byte("# Source"), nil
		})
		_, err = filesHaveSameContent("/legacy/source.md", "/configured/target.md")
		Expect(err).To(MatchError(ContainSubstring("read configured legacy content path")))

		swapTreeSeam(&treeFilepathAbs, func(string) (string, error) {
			return "", errors.New("abs failed")
		})
		Expect(sameCleanPath("/a/../b", "/other")).To(BeFalse())
	})

	It("covers configured legacy content comparison branches", func() {
		base := GinkgoT().TempDir()
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
		_, err := svc.configuredRootMissingLegacyContent(legacy)
		Expect(err).To(MatchError(ContainSubstring("read directory")))

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
		_, err = svc.configuredRootMissingLegacyContent(legacy)
		Expect(err).To(MatchError(ContainSubstring("stat configured legacy content path")))

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
		missing, err := svc.configuredRootMissingLegacyContent(legacy)
		Expect(err).NotTo(HaveOccurred())
		Expect(missing).To(BeTrue())

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
		missing, err = svc.configuredRootMissingLegacyContent(legacy)
		Expect(err).NotTo(HaveOccurred())
		Expect(missing).To(BeTrue())

		swapTreeSeam(&treeOSReadFile, func(string) ([]byte, error) {
			return []byte("# Page"), nil
		})
		swapTreeSeam(&treeLoadMarkdownFile, func(string) (*markdown.MarkdownFile, error) {
			return nil, errors.New("load target failed")
		})
		_, err = svc.configuredRootMissingLegacyContent(legacy)
		Expect(err).To(MatchError(ContainSubstring("load configured legacy content path")))

		badParent := edgeSectionNode(RootPageID, "root", "Root", nil)
		badParent.Children = []*PageNode{edgePageNode("bad", "", "Bad", badParent)}
		_, err = svc.expectedLegacyContentPaths(badParent)
		Expect(err).To(MatchError(ContainSubstring("empty slug")))
	})

	It("covers legacy-root readiness helper branches", func() {
		base := GinkgoT().TempDir()
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
		Expect(svc.ensureLegacyRootDirReady(legacy)).To(MatchError(ContainSubstring("read directory")))
		Expect(svc.ensureCurrentRootDirReady()).To(MatchError(ContainSubstring("read directory")))

		swapTreeSeam(&treeOSReadDir, func(path string) ([]os.DirEntry, error) {
			if path == defaultRoot {
				return []os.DirEntry{fakeTreeDirEntry{name: "page.md"}}, nil
			}
			return []os.DirEntry{}, nil
		})
		swapTreeSeam(&treeFilepathWalkDir, func(string, fs.WalkDirFunc) error {
			return errors.New("walk failed")
		})
		Expect(svc.ensureLegacyRootDirReady(legacy)).To(MatchError(ContainSubstring("collect legacy content files")))
		Expect(svc.ensureCurrentRootDirReady()).To(MatchError(ContainSubstring("collect legacy content files")))

		swapTreeSeam(&treeFilepathWalkDir, filepath.WalkDir)
		Expect(os.WriteFile(filepath.Join(defaultRoot, "page.md"), []byte("# Page"), 0o644)).To(Succeed())
		Expect(svc.ensureCurrentRootDirReady()).To(MatchError(ContainSubstring("legacy content remains")))
		Expect(svc.ensureLegacyRootDirReady(legacy)).To(MatchError(ContainSubstring("legacy content remains")))

		swapTreeSeam(&treeOSStat, func(path string) (os.FileInfo, error) {
			if filepath.Base(path) == "page.md" {
				return nil, errors.New("stat failed")
			}
			return fakeTreeFileInfo{}, nil
		})
		pageLegacy := edgeSectionNode(RootPageID, "root", "Root", nil)
		pageLegacy.Children = []*PageNode{edgePageNode("page", "page", "Page", pageLegacy)}
		_, err := svc.configuredRootMissingLegacyContent(pageLegacy)
		Expect(err).To(MatchError(ContainSubstring("stat legacy content path")))
	})

	It("covers create and restore rollback/error branches through store seams", func() {
		svc := newInMemoryService()
		pageKind := NodeKindPage

		unloaded := NewTreeService(GinkgoT().TempDir())
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
		Expect(err).To(MatchError(ContainSubstring("could not generate unique ID")))

		swapTreeSeam(&treeGenerateUniqueID, func() (string, error) { return "new-id", nil })
		swapTreeSeam(&treeStoreCreatePage, func(*NodeStore, *PageNode, *PageNode) error {
			return errors.New("create failed")
		})
		_, err = svc.CreateNode("user", nil, "Created", "created", &pageKind)
		Expect(err).To(MatchError(ContainSubstring("could not create page entry")))

		swapTreeSeam(&treeStoreCreatePage, func(*NodeStore, *PageNode, *PageNode) error { return nil })
		swapTreeSeam(&treeStoreSaveChildOrder, func(*NodeStore, *PageNode) error {
			return errors.New("order failed")
		})
		swapTreeSeam(&treeStoreDeletePage, func(*NodeStore, *PageNode) error { return nil })
		_, err = svc.CreateNode("user", nil, "Rollback", "rollback", &pageKind)
		Expect(err).To(MatchError(ContainSubstring("could not persist child order")))

		swapTreeSeam(&treeStoreDeletePage, func(*NodeStore, *PageNode) error {
			return errors.New("delete failed")
		})
		_, err = svc.CreateNode("user", nil, "Rollback Fail", "rollback-fail", &pageKind)
		Expect(err).To(MatchError(And(ContainSubstring("could not persist child order"), ContainSubstring("rollback created node"))))

		swapTreeSeam(&treeStoreSaveChildOrder, func(*NodeStore, *PageNode) error { return nil })
		swapTreeSeam(&treeStoreCreatePage, func(*NodeStore, *PageNode, *PageNode) error { return nil })
		swapTreeSeam(&treeStoreUpsertContent, func(*NodeStore, *PageNode, string) error {
			return errors.New("upsert failed")
		})
		_, err = svc.RestoreNode("user", "restored", nil, "Restored", "restored", NodeKindPage, "body", PageMetadata{})
		Expect(err).To(MatchError(ContainSubstring("could not restore content")))

		swapTreeSeam(&treeStoreUpsertContent, func(*NodeStore, *PageNode, string) error { return nil })
		swapTreeSeam(&treeStoreSyncMetadataIfExists, func(*NodeStore, *PageNode) error {
			return errors.New("sync failed")
		})
		_, err = svc.RestoreNode("user", "restored-sync", nil, "Restored Sync", "restored-sync", NodeKindPage, "body", PageMetadata{})
		Expect(err).To(MatchError(ContainSubstring("could not sync restored metadata")))
	})

	It("covers create rollback and delete/convert branch seams", func() {
		pageKind := NodeKindPage
		sectionKind := NodeKindSection

		invalidRootSvc := newInMemoryService()
		invalidRootSvc.tree.Kind = NodeKindPage
		_, err := invalidRootSvc.CreateNode("user", nil, "Child", "child", &pageKind)
		Expect(err).To(MatchError(ContainSubstring("cannot add child to non-section parent")))

		convertParentSvc := newInMemoryService()
		parentPage := edgePageNode("parent-page", "parent-page", "Parent Page", convertParentSvc.tree)
		convertParentSvc.tree.Children = []*PageNode{parentPage}
		convertParentSvc.rebuildIndexesLocked()
		swapTreeSeam(&treeStoreConvertNode, func(*NodeStore, *PageNode, NodeKind) error {
			return errors.New("convert parent failed")
		})
		_, err = convertParentSvc.CreateNode("user", &parentPage.ID, "Child", "child", &pageKind)
		Expect(err).To(MatchError(ContainSubstring("could not convert parent node")))

		duplicateIDSvc := newInMemoryService()
		existing := edgePageNode("existing", "existing", "Existing", duplicateIDSvc.tree)
		duplicateIDSvc.tree.Children = []*PageNode{existing}
		duplicateIDSvc.rebuildIndexesLocked()
		_, err = duplicateIDSvc.RestoreNode("user", "existing", nil, "Existing", "restored", NodeKindPage, "body", PageMetadata{})
		Expect(err).To(MatchError(ContainSubstring("page id already exists")))

		createSectionSvc := newInMemoryService()
		swapTreeSeam(&treeGenerateUniqueID, func() (string, error) { return "section-id", nil })
		swapTreeSeam(&treeStoreCreateSection, func(*NodeStore, *PageNode, *PageNode) error {
			return errors.New("create section failed")
		})
		_, err = createSectionSvc.CreateNode("user", nil, "Section", "section", &sectionKind)
		Expect(err).To(MatchError(ContainSubstring("could not create section entry")))

		rollbackSvc := newInMemoryService()
		Expect(rollbackSvc.rollbackCreatedNodeLocked(nil, nil, true)).To(Succeed())

		rollbackParent := edgeSectionNode("rollback-parent", "rollback-parent", "Rollback Parent", rollbackSvc.tree)
		rollbackSection := edgeSectionNode("rollback-section", "rollback-section", "Rollback Section", rollbackParent)
		rollbackParent.Children = []*PageNode{rollbackSection}
		swapTreeSeam(&treeStoreDeleteSection, func(*NodeStore, *PageNode) error {
			return errors.New("delete section failed")
		})
		Expect(rollbackSvc.rollbackCreatedNodeLocked(rollbackParent, rollbackSection, false)).To(MatchError("delete section failed"))

		rollbackParent = edgeSectionNode("rollback-parent", "rollback-parent", "Rollback Parent", rollbackSvc.tree)
		rollbackPage := edgePageNode("rollback-page", "rollback-page", "Rollback Page", rollbackParent)
		rollbackParent.Children = []*PageNode{rollbackPage}
		swapTreeSeam(&treeStoreDeletePage, func(*NodeStore, *PageNode) error { return nil })
		swapTreeSeam(&treeOSRemove, func(string) error {
			return errors.New("remove order failed")
		})
		Expect(rollbackSvc.rollbackCreatedNodeLocked(rollbackParent, rollbackPage, true)).To(MatchError(ContainSubstring("remove parent order file")))

		rollbackParent = edgeSectionNode("rollback-parent", "rollback-parent", "Rollback Parent", rollbackSvc.tree)
		rollbackPage = edgePageNode("rollback-page", "rollback-page", "Rollback Page", rollbackParent)
		rollbackParent.Children = []*PageNode{rollbackPage}
		swapTreeSeam(&treeOSRemove, func(string) error { return os.ErrNotExist })
		swapTreeSeam(&treeStoreConvertNode, func(*NodeStore, *PageNode, NodeKind) error {
			return errors.New("fold back failed")
		})
		Expect(rollbackSvc.rollbackCreatedNodeLocked(rollbackParent, rollbackPage, true)).To(MatchError("fold back failed"))

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
		swapTreeSeam(&treeStoreConvertNode, func(*NodeStore, *PageNode, NodeKind) error {
			return errors.New("convert page failed")
		})
		Expect(deletePageWithChildrenSvc.DeleteNode("user", pageWithChildren.ID, true, pageVersionUnchecked)).To(MatchError(ContainSubstring("could not convert page to section")))

		deletePageWithChildrenSvc = newInMemoryService()
		pageWithChildren = edgePageNode("page-with-children", "page-with-children", "Page With Children", deletePageWithChildrenSvc.tree)
		child = edgePageNode("child", "child", "Child", pageWithChildren)
		pageWithChildren.Children = []*PageNode{child}
		deletePageWithChildrenSvc.tree.Children = []*PageNode{pageWithChildren}
		deletePageWithChildrenSvc.rebuildIndexesLocked()
		swapTreeSeam(&treeStoreConvertNode, func(*NodeStore, *PageNode, NodeKind) error { return nil })
		swapTreeSeam(&treeStoreDeleteSection, func(*NodeStore, *PageNode) error {
			return errors.New("delete converted section failed")
		})
		Expect(deletePageWithChildrenSvc.DeleteNode("user", pageWithChildren.ID, true, pageVersionUnchecked)).To(MatchError(ContainSubstring("could not delete section entry")))

		deleteOrderSvc := newInMemoryService()
		deletedPage := edgePageNode("deleted-page", "deleted-page", "Deleted Page", deleteOrderSvc.tree)
		deleteOrderSvc.tree.Children = []*PageNode{deletedPage}
		deleteOrderSvc.rebuildIndexesLocked()
		swapTreeSeam(&treeStoreDeletePage, func(*NodeStore, *PageNode) error { return nil })
		swapTreeSeam(&treeStoreSaveChildOrder, func(*NodeStore, *PageNode) error {
			return errors.New("delete order failed")
		})
		Expect(deleteOrderSvc.DeleteNode("user", deletedPage.ID, false, pageVersionUnchecked)).To(MatchError(ContainSubstring("could not persist child order")))

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

	It("covers update, delete, convert, move, and sort orchestration branches", func() {
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

		swapTreeSeam(&treeStoreDeletePage, func(*NodeStore, *PageNode) error {
			return errors.New("delete page failed")
		})
		Expect(svc.DeleteNode("user", "page", false, pageVersionUnchecked)).To(MatchError(ContainSubstring("could not delete page entry")))

		swapTreeSeam(&treeStoreDeletePage, func(*NodeStore, *PageNode) error { return nil })
		swapTreeSeam(&treeStoreDeleteSection, func(*NodeStore, *PageNode) error {
			return errors.New("delete section failed")
		})
		Expect(svc.DeleteNode("user", "section", true, pageVersionUnchecked)).To(MatchError(ContainSubstring("could not delete section entry")))

		swapTreeSeam(&treeStoreDeleteSection, func(*NodeStore, *PageNode) error { return nil })
		unknown := edgePageNode("unknown", "unknown", "Unknown", svc.tree)
		unknown.Kind = NodeKind("unknown")
		svc.tree.Children = append(svc.tree.Children, unknown)
		svc.rebuildIndexesLocked()
		Expect(svc.DeleteNode("user", "unknown", false, pageVersionUnchecked)).To(MatchError(ContainSubstring("unknown node kind")))

		content := "body"
		swapTreeSeam(&treeStoreUpsertContent, func(*NodeStore, *PageNode, string) error {
			return errors.New("plain failed")
		})
		Expect(svc.UpdateNode("user", "page", "Page", "page", &content, pageVersionUnchecked, false)).To(MatchError(ContainSubstring("could not upsert content")))

		swapTreeSeam(&treeStoreUpsertContent, func(*NodeStore, *PageNode, string) error { return nil })
		swapTreeSeam(&treeStoreUpsertContentPreservingFrontmatter, func(*NodeStore, *PageNode, string) error {
			return errors.New("preserve failed")
		})
		Expect(svc.UpdateNode("user", "page", "Page", "page", &content, pageVersionUnchecked, true)).To(MatchError(ContainSubstring("could not upsert content")))

		swapTreeSeam(&treeStoreUpsertContentPreservingFrontmatter, func(*NodeStore, *PageNode, string) error { return nil })
		swapTreeSeam(&treeStoreUpsertContentReplacingMetadata, func(*NodeStore, *PageNode, string) error {
			return errors.New("replace failed")
		})
		Expect(svc.UpdateNodeReplacingMetadata("user", "page", "Page", "page", &content, pageVersionUnchecked)).To(MatchError(ContainSubstring("could not upsert content")))

		swapTreeSeam(&treeStoreUpsertContentReplacingMetadata, func(*NodeStore, *PageNode, string) error { return nil })
		swapTreeSeam(&treeStoreRenameNode, func(*NodeStore, *PageNode, Slug) error {
			return errors.New("rename failed")
		})
		Expect(svc.UpdateNode("user", "page", "Page", "renamed", nil, pageVersionUnchecked, false)).To(MatchError(ContainSubstring("could not rename node")))

		swapTreeSeam(&treeStoreRenameNode, func(*NodeStore, *PageNode, Slug) error { return nil })
		swapTreeSeam(&treeStoreSyncMetadataIfExists, func(*NodeStore, *PageNode) error {
			return errors.New("sync failed")
		})
		Expect(svc.UpdateNode("user", "page", "Page", "page", nil, pageVersionUnchecked, false)).To(MatchError(ContainSubstring("could not sync metadata")))
		Expect(svc.ConvertNode("user", "page", NodeKindSection, pageVersionUnchecked)).To(MatchError(ContainSubstring("could not sync metadata")))
		page.Kind = NodeKindPage

		swapTreeSeam(&treeStoreSyncMetadataIfExists, func(*NodeStore, *PageNode) error { return nil })
		swapTreeSeam(&treeStoreConvertNode, func(*NodeStore, *PageNode, NodeKind) error {
			return errors.New("convert failed")
		})
		Expect(svc.ConvertNode("user", "page", NodeKindSection, pageVersionUnchecked)).To(MatchError(ContainSubstring("could not convert node")))

		swapTreeSeam(&treeStoreConvertNode, func(*NodeStore, *PageNode, NodeKind) error { return nil })
		Expect(svc.MoveNode("user", "missing", RootPageID, pageVersionUnchecked)).To(MatchError(ErrPageNotFound))
		Expect(svc.MoveNode("user", "page", "missing-parent", pageVersionUnchecked)).To(MatchError(ContainSubstring("new parent not found")))
		Expect(svc.MoveNode("user", "page", "page", pageVersionUnchecked)).To(MatchError(ContainSubstring("page cannot be moved to itself")))
		Expect(svc.MoveNode("user", "section", "child", pageVersionUnchecked)).To(MatchError(ContainSubstring("circular reference")))

		swapTreeSeam(&treeStoreMoveNode, func(*NodeStore, *PageNode, *PageNode) error {
			return errors.New("move failed")
		})
		Expect(svc.MoveNode("user", "page", "section", pageVersionUnchecked)).To(MatchError(ContainSubstring("could not move node on disk")))

		swapTreeSeam(&treeStoreMoveNode, func(*NodeStore, *PageNode, *PageNode) error { return nil })
		swapTreeSeam(&treeStoreSaveChildOrder, func(*NodeStore, *PageNode) error {
			return errors.New("order failed")
		})
		Expect(svc.MoveNode("user", "page", "section", pageVersionUnchecked)).To(MatchError(ContainSubstring("could not persist source child order")))

		sortSvc := newInMemoryService()
		sortPage := edgePageNode("sort-page", "sort-page", "Sort Page", sortSvc.tree)
		sortSection := edgeSectionNode("sort-section", "sort-section", "Sort Section", sortSvc.tree)
		sortSvc.tree.Children = []*PageNode{sortPage, sortSection}
		sortSvc.rebuildIndexesLocked()
		Expect(sortSvc.SortPages("missing", nil)).To(MatchError(ErrParentNotFound))
		Expect(sortSvc.SortPages(RootPageID, []PageID{"only-one"})).To(MatchError(ContainSubstring("number of ordered IDs")))
		Expect(sortSvc.SortPages(RootPageID, []PageID{"sort-page", "not-present"})).To(MatchError(ContainSubstring("invalid ID in sort order")))
		Expect(sortSvc.SortPages(RootPageID, []PageID{"sort-page", "sort-page"})).To(MatchError(ContainSubstring("duplicate ID")))
	})

	It("covers service batch, lookup, ensure, and move rollback branches", func() {
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

		swapTreeSeam(&treeStoreReadPageRaw, func(*NodeStore, *PageNode) (string, error) {
			return "", errors.New("raw failed")
		})
		_, err := svc.ReadPageRaw("page")
		Expect(err).To(MatchError(ContainSubstring("could not get page raw content")))

		unloaded := NewTreeService(GinkgoT().TempDir())
		_, err = unloaded.FindPageByRoutePath("page")
		Expect(err).To(MatchError(ErrTreeNotLoaded))
		_, err = svc.LookupPagePath("")
		Expect(err).To(MatchError("missing path"))
		_, err = svc.LookupPagePathForKind("", NodeKindPage)
		Expect(err).To(MatchError("missing path"))

		swapTreeSeam(&treeStoreReadPageContent, func(*NodeStore, *PageNode) (string, error) {
			return "", errors.New("content failed")
		})
		_, err = svc.FindPageByRoutePath("page")
		Expect(err).To(MatchError(ContainSubstring("could not get page content")))

		relCalls := 0
		swapTreeSeam(&treeFilepathRel, func(string, string) (string, error) {
			relCalls++
			if relCalls == 1 {
				return "page.md", nil
			}
			return "", errors.New("rel failed")
		})
		_, err = svc.ContentPathForNode(page)
		Expect(err).To(MatchError(ContainSubstring("rel failed")))

		missingIDPageSvc := newInMemoryService()
		emptyIDPage := edgePageNode("", "empty-id", "Empty ID", missingIDPageSvc.tree)
		missingIDPageSvc.tree.Children = []*PageNode{emptyIDPage}
		missingIDPageSvc.rebuildIndexesLocked()
		_, err = missingIDPageSvc.EnsurePagePath("user", "empty-id", "Empty ID", &pageKind)
		Expect(err).To(MatchError(ContainSubstring("could not find existing page by ID")))

		ensureSvc := newInMemoryService()
		swapTreeSeam(&treeGenerateUniqueID, func() (string, error) { return "", nil })
		swapTreeSeam(&treeStoreCreatePage, func(*NodeStore, *PageNode, *PageNode) error { return nil })
		swapTreeSeam(&treeStoreSaveChildOrder, func(*NodeStore, *PageNode) error { return nil })
		_, err = ensureSvc.EnsurePagePath("user", "created", "Created", &pageKind)
		Expect(err).To(MatchError(ContainSubstring("could not find created page by ID")))

		orphanSvc := newInMemoryService()
		orphan := edgePageNode("orphan", "orphan", "Orphan", nil)
		orphanSvc.nodesByID[orphan.ID] = orphan
		Expect(orphanSvc.MoveNode("user", "orphan", RootPageID, pageVersionUnchecked)).To(MatchError(ContainSubstring("old parent not found")))

		swapTreeSeam(&treeStoreConvertNode, func(*NodeStore, *PageNode, NodeKind) error {
			return errors.New("convert dest failed")
		})
		Expect(svc.MoveNode("user", "page", "dest-page", pageVersionUnchecked)).To(MatchError(ContainSubstring("could not auto-convert new parent page")))

		invalidDestSvc := newInMemoryService()
		movePage := edgePageNode("move-page", "move-page", "Move Page", invalidDestSvc.tree)
		invalidDest := edgeSectionNode("invalid-dest", "invalid-dest", "Invalid Dest", invalidDestSvc.tree)
		invalidDest.Kind = NodeKind("unknown")
		invalidDestSvc.tree.Children = []*PageNode{movePage, invalidDest}
		invalidDestSvc.rebuildIndexesLocked()
		Expect(invalidDestSvc.MoveNode("user", "move-page", "invalid-dest", pageVersionUnchecked)).To(MatchError(ContainSubstring("destination parent must be a section")))

		moveOrderSvc := newInMemoryService()
		movePage = edgePageNode("move-page", "move-page", "Move Page", moveOrderSvc.tree)
		moveDest := edgeSectionNode("move-dest", "move-dest", "Move Dest", moveOrderSvc.tree)
		moveOrderSvc.tree.Children = []*PageNode{movePage, moveDest}
		moveOrderSvc.rebuildIndexesLocked()
		swapTreeSeam(&treeStoreMoveNode, func(*NodeStore, *PageNode, *PageNode) error { return nil })
		saveCalls := 0
		swapTreeSeam(&treeStoreSaveChildOrder, func(*NodeStore, *PageNode) error {
			saveCalls++
			if saveCalls == 2 {
				return errors.New("destination order failed")
			}
			return nil
		})
		Expect(moveOrderSvc.MoveNode("user", "move-page", "move-dest", pageVersionUnchecked)).To(MatchError(ContainSubstring("could not persist destination child order")))

		moveSyncSvc := newInMemoryService()
		movePage = edgePageNode("move-page", "move-page", "Move Page", moveSyncSvc.tree)
		moveDest = edgeSectionNode("move-dest", "move-dest", "Move Dest", moveSyncSvc.tree)
		moveSyncSvc.tree.Children = []*PageNode{movePage, moveDest}
		moveSyncSvc.rebuildIndexesLocked()
		swapTreeSeam(&treeStoreSaveChildOrder, func(*NodeStore, *PageNode) error { return nil })
		swapTreeSeam(&treeStoreSyncMetadataIfExists, func(*NodeStore, *PageNode) error {
			return errors.New("sync moved failed")
		})
		Expect(moveSyncSvc.MoveNode("user", "move-page", "move-dest", pageVersionUnchecked)).To(MatchError(ContainSubstring("could not sync moved node metadata")))

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
		Expect(err).To(MatchError(ContainSubstring("move node back on disk")))

		swapTreeSeam(&treeStoreMoveNode, func(*NodeStore, *PageNode, *PageNode) error { return nil })
		convertedParent := edgeSectionNode("converted-parent", "converted-parent", "Converted Parent", oldParent)
		convertedParent.WorkspaceSourcePath = "../outside"
		err = rollbackSvc.rollbackMovedNodeLocked(node, oldParent, convertedParent, nil, map[PageID]int{}, nil, map[PageID]int{}, 0, PageMetadata{}, true)
		Expect(err).To(MatchError(ContainSubstring("resolve converted parent dir")))

		swapTreeSeam(&treeFilepathRel, filepath.Rel)
		swapTreeSeam(&treeStoreConvertNode, func(*NodeStore, *PageNode, NodeKind) error { return nil })
		convertedParent = edgeSectionNode("converted-parent", "converted-parent", "Converted Parent", oldParent)
		swapTreeSeam(&treeOSRemoveAll, func(string) error {
			return errors.New("remove order failed")
		})
		err = rollbackSvc.rollbackMovedNodeLocked(node, oldParent, convertedParent, nil, map[PageID]int{}, nil, map[PageID]int{}, 0, PageMetadata{}, true)
		Expect(err).To(MatchError(ContainSubstring("remove child order before parent rollback")))

		convertedParent = edgeSectionNode("converted-parent", "converted-parent", "Converted Parent", oldParent)
		swapTreeSeam(&treeOSRemoveAll, func(string) error { return nil })
		swapTreeSeam(&treeStoreConvertNode, func(*NodeStore, *PageNode, NodeKind) error {
			return errors.New("convert back failed")
		})
		err = rollbackSvc.rollbackMovedNodeLocked(node, oldParent, convertedParent, nil, map[PageID]int{}, nil, map[PageID]int{}, 0, PageMetadata{}, true)
		Expect(err).To(MatchError(ContainSubstring("convert destination parent back to page")))

		convertedParent = edgeSectionNode("converted-parent", "converted-parent", "Converted Parent", oldParent)
		swapTreeSeam(&treeStoreConvertNode, func(*NodeStore, *PageNode, NodeKind) error { return nil })
		Expect(rollbackSvc.rollbackMovedNodeLocked(node, oldParent, convertedParent, nil, map[PageID]int{}, nil, map[PageID]int{}, 0, PageMetadata{}, true)).To(Succeed())
		Expect(convertedParent.Kind).To(Equal(NodeKindPage))
	})

	It("covers final service root, lookup, ensure, and move branches", func() {
		base := GinkgoT().TempDir()
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
		Expect(NewTreeServiceWithOptions(TreeOptions{DataDir: dataDir, RootDir: rootDir}).LoadTree()).To(MatchError(ContainSubstring("read directory")))

		swapTreeSeam(&treeOSStat, os.Stat)
		swapTreeSeam(&treeOSReadDir, os.ReadDir)
		Expect(os.WriteFile(filepath.Join(defaultRoot, "page.md"), []byte("# Page"), 0o644)).To(Succeed())
		Expect(os.WriteFile(filepath.Join(rootDir, "page.md"), []byte("# Page"), 0o644)).To(Succeed())
		legacy := edgeSectionNode(RootPageID, "root", "Root", nil)
		legacy.Children = []*PageNode{edgePageNode("page", "page", "Page", legacy)}
		svc := NewTreeServiceWithOptions(TreeOptions{DataDir: dataDir, RootDir: rootDir})

		swapTreeSeam(&treeLoadMarkdownFile, func(string) (*markdown.MarkdownFile, error) {
			return nil, errors.New("legacy target load failed")
		})
		Expect(svc.ensureLegacyRootDirReady(legacy)).To(MatchError(ContainSubstring("legacy target load failed")))

		swapTreeSeam(&treeLoadMarkdownFile, markdown.LoadMarkdownFile)
		Expect(svc.ensureLegacyRootDirReady(legacy)).To(MatchError(ContainSubstring("legacy content remains")))
		Expect(svc.ensureCurrentRootDirReady()).To(Succeed())

		swapTreeSeam(&treeOSReadFile, func(path string) ([]byte, error) {
			if filepath.Base(path) == "page.md" && filepath.Dir(path) == rootDir {
				return nil, errors.New("target read failed")
			}
			return []byte("# Page"), nil
		})
		matches, err := directoryFileContentMatches(defaultRoot, rootDir)
		Expect(err).To(MatchError(ContainSubstring("read configured legacy content path")))
		Expect(matches).To(BeFalse())

		swapTreeSeam(&treeFilepathWalkDir, func(dir string, fn fs.WalkDirFunc) error {
			return fn(filepath.Join(dir, "link.md"), fakeTreeDirEntry{name: "link.md", mode: fs.ModeSymlink}, nil)
		})
		files, err := collectRelativeFiles(defaultRoot)
		Expect(err).NotTo(HaveOccurred())
		Expect(files).To(BeEmpty())

		_, err = svc.configuredRootMissingLegacyContent(&PageNode{ID: "bad", Slug: "", Kind: NodeKindPage})
		Expect(err).To(MatchError(ContainSubstring("empty slug")))

		swapTreeSeam(&treeOSReadDir, func(string) ([]os.DirEntry, error) {
			return nil, errors.New("configured read failed")
		})
		_, err = svc.configuredRootMissingLegacyContent(nil)
		Expect(err).To(MatchError(ContainSubstring("read directory")))

		swapTreeSeam(&treeOSReadFile, func(string) ([]byte, error) {
			return nil, errors.New("source read failed")
		})
		_, err = legacyTargetMatchesNode(legacyContentPath{sourceFile: "source.md", targetFile: "target.md"})
		Expect(err).To(MatchError(ContainSubstring("read legacy content path")))

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
		Expect(err).To(MatchError(ContainSubstring("invalid path")))
		_, err = lookupSvc.EnsurePagePath("user", "bad//path", "Bad", nil)
		Expect(err).To(MatchError(ContainSubstring("could not lookup page path")))

		contentSvc := newInMemoryService()
		contentPage := edgePageNode("content-page", "content-page", "Content Page", contentSvc.tree)
		contentSvc.tree.Children = []*PageNode{contentPage}
		contentSvc.rebuildIndexesLocked()
		relCalls := 0
		swapTreeSeam(&treeFilepathRel, func(string, string) (string, error) {
			relCalls++
			if relCalls <= 2 {
				return "content-page.md", nil
			}
			return "", errors.New("service rel failed")
		})
		_, err = contentSvc.ContentPathForNode(contentPage)
		Expect(err).To(MatchError("service rel failed"))

		ensureSvc := newInMemoryService()
		swapTreeSeam(&treeFilepathRel, filepath.Rel)
		swapTreeSeam(&treeGenerateUniqueID, func() (string, error) { return "created-id", nil })
		swapTreeSeam(&treeStoreCreatePage, func(*NodeStore, *PageNode, *PageNode) error {
			return errors.New("create segment failed")
		})
		_, err = ensureSvc.EnsurePagePath("user", "created", "Created", nil)
		Expect(err).To(MatchError(ContainSubstring("could not create segment")))

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
		Expect(err).To(MatchError(And(ContainSubstring("could not persist source child order"), Not(ContainSubstring("rollback moved node")))))

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
		Expect(err).To(MatchError(And(ContainSubstring("could not persist destination child order"), ContainSubstring("rollback moved node"))))

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
		Expect(err).To(MatchError(And(ContainSubstring("could not sync moved node metadata"), ContainSubstring("rollback moved node"))))
	})
})

func newInMemoryService() *TreeService {
	GinkgoHelper()
	svc := NewTreeService(GinkgoT().TempDir())
	svc.tree = edgeSectionNode(RootPageID, "root", "Root", nil)
	svc.rebuildIndexesLocked()
	return svc
}
