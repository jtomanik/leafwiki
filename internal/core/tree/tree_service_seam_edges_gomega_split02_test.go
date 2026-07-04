package tree

import (
	"errors"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/perber/wiki/internal/core/markdown"
	"github.com/perber/wiki/internal/core/treemigration"
)

var _ = Describe("tree service migration and store seam failure behavior", Label("unit"), func() {
	It("LoadTree reports migration and reconstruction seam failures", func() {
		svc := NewTreeService(tempTreeDir())
		var nilRoot *PageNode
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
			return nilRoot, nil
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
			return nilRoot, nil
		})
		Expect(svc.LoadTree()).To(MatchError(ErrTreeReconstructionNil))
	})

	It("LoadTree falls back to legacy data and cleans stale state", func() {
		dataDir := tempTreeDir()
		legacyPath := filepath.Join(dataDir, legacyTreeFilename)
		root := edgeSectionNode(RootPageID, "root", "Root", nil)
		var nilRoot *PageNode

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
			return nilRoot, nil
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
		var nilRoot *PageNode

		reconstructErr := errors.New("reconstruct failed")
		swapTreeSeam(&treeStoreReconstructTreeFromFS, func(*NodeStore) (*PageNode, error) {
			return nil, reconstructErr
		})
		Expect(svc.ReconstructTreeFromFS()).To(MatchError(reconstructErr))

		swapTreeSeam(&treeStoreReconstructTreeFromFS, func(*NodeStore) (*PageNode, error) {
			return nilRoot, nil
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
})
