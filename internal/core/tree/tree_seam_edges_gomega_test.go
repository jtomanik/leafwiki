package tree

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/perber/wiki/internal/core/markdown"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("tree filesystem seam failure behavior", Label("unit"), func() {
	var (
		base   string
		root   string
		store  *NodeStore
		parent *PageNode
	)

	BeforeEach(func() {
		base = tempTreeDir()
		root = filepath.Join(base, "root")
		store = NewNodeStoreWithOptions(NodeStoreOptions{DataDir: filepath.Join(base, "data"), RootDir: root})
		parent = edgeSectionNode(RootPageID, "root", "Root", nil)
	})

	It("filesystem helper seams propagate rename and remove failures", func() {
		swapTreeSeam(&treeOSStat, func(path string) (os.FileInfo, error) {
			switch {
			case strings.HasSuffix(path, "guide"):
				return nil, os.ErrNotExist
			case strings.HasSuffix(path, "guide.md"):
				return fakeTreeFileInfo{name: filepath.Base(path)}, nil
			default:
				return nil, os.ErrNotExist
			}
		})
		swapTreeSeam(&treeOSMkdirAll, func(string, os.FileMode) error { return nil })
		renameErr := errors.New("rename failed")
		swapTreeSeam(&treeOSRename, func(string, string) error { return renameErr })

		Expect(EnsurePageIsFolder(root, "guide")).To(MatchError(renameErr))

		swapTreeSeam(&treeOSStat, func(path string) (os.FileInfo, error) {
			return fakeTreeFileInfo{name: filepath.Base(path), mode: fs.ModeDir}, nil
		})
		swapTreeSeam(&treeOSReadDir, func(string) ([]os.DirEntry, error) {
			return []os.DirEntry{fakeTreeDirEntry{name: "index.md"}}, nil
		})
		Expect(FoldPageFolderIfEmpty(root, "guide")).To(MatchError(renameErr))

		swapTreeSeam(&treeOSRename, func(string, string) error { return nil })
		removeErr := errors.New("remove failed")
		swapTreeSeam(&treeOSRemove, func(string) error { return removeErr })
		Expect(FoldPageFolderIfEmpty(root, "guide")).To(MatchError(removeErr))
	})

	It("metadata and section-index seams surface write failures", func() {
		mdFile := markdown.NewMarkdownFile(filepath.Join(root, "page.md"), "# Page\n", markdown.Frontmatter{})
		entry := edgePageNode("page", "page", "Page", parent)

		writeMetadataErr := errors.New("write failed")
		swapTreeSeam(&treeMarkdownWriteToFile, func(*markdown.MarkdownFile) error { return writeMetadataErr })
		Expect(store.writeReconstructedMetadata(mdFile, entry)).To(MatchError(writeMetadataErr))

		swapTreeSeam(&treeMarkdownWriteToFile, func(*markdown.MarkdownFile) error { return nil })
		swapTreeSeam(&treeOSStat, func(string) (os.FileInfo, error) {
			return fakeTreeFileInfo{modTime: time.Date(2026, time.June, 27, 12, 0, 0, 0, time.UTC)}, nil
		})
		swapTreeSeam(&treeOSChtimes, func(string, time.Time, time.Time) error { return errors.New("chtime failed") })
		Expect(store.writeReconstructedMetadata(mdFile, entry)).To(Succeed())

		section := edgeSectionNode("section", "section", "Section", parent)
		swapTreeSeam(&treeLoadMarkdownFile, func(string) (*markdown.MarkdownFile, error) {
			return nil, errors.New("load failed")
		})
		swapTreeSeam(&treeOSStat, func(string) (os.FileInfo, error) {
			return fakeTreeFileInfo{name: "index.md"}, nil
		})
		_, err := store.ensureSectionIndex(section)
		Expect(err).To(MatchError(ErrLoadMarkdownFile))

		swapTreeSeam(&treeLoadMarkdownFile, func(path string) (*markdown.MarkdownFile, error) {
			return markdown.NewMarkdownFile(path, "# Loaded\n", markdown.Frontmatter{}), nil
		})
		swapTreeSeam(&treeMarkdownWriteToFile, func(*markdown.MarkdownFile) error { return errors.New("write failed") })
		_, err = store.ensureSectionIndexAtPath(section, filepath.Join(root, "section", "index.md"))
		Expect(err).To(MatchError(ErrWriteMarkdownFile))
	})

	It("node store filesystem seams propagate operation failures", func() {
		page := edgePageNode("page", "page", "Page", parent)
		section := edgeSectionNode("section", "section", "Section", parent)

		mkdirErr := errors.New("mkdir failed")
		swapTreeSeam(&treeOSMkdirAll, func(string, os.FileMode) error { return mkdirErr })
		Expect(store.SaveChildOrder(parent)).To(MatchError(mkdirErr))
		Expect(store.CreatePage(parent, page)).To(MatchError(mkdirErr))
		Expect(store.CreateSection(parent, section)).To(MatchError(mkdirErr))
		Expect(store.MoveNode(page, parent)).To(MatchError(mkdirErr))

		swapTreeSeam(&treeOSMkdirAll, func(string, os.FileMode) error { return nil })
		atomicErr := errors.New("atomic failed")
		swapTreeSeam(&treeWriteFileAtomic, func(string, []byte, os.FileMode) error { return atomicErr })
		Expect(store.SaveChildOrder(parent)).To(MatchError(atomicErr))

		writeErr := errors.New("write failed")
		swapTreeSeam(&treeMarkdownWriteToFile, func(*markdown.MarkdownFile) error { return writeErr })
		Expect(store.CreatePage(parent, page)).To(MatchError(writeErr))
		Expect(store.UpsertContent(page, "body")).To(MatchError(writeErr))
		Expect(store.UpsertContentPreservingFrontmatter(page, "body")).To(MatchError(writeErr))
		Expect(store.UpsertContentReplacingMetadata(page, "body")).To(MatchError(writeErr))

		swapTreeSeam(&treeMarkdownWriteToFile, func(*markdown.MarkdownFile) error { return nil })
		swapTreeSeam(&treeOSMkdirAll, func(path string, _ os.FileMode) error {
			if strings.HasSuffix(path, "section") {
				return mkdirErr
			}
			return nil
		})
		Expect(store.CreateSection(parent, section)).To(MatchError(mkdirErr))
	})

	It("reconstruction seams produce deterministic filesystem outcomes", func() {
		errFixtureStatFailed := errors.New("stat failed")
		swapTreeSeam(&treeOSStat, func(string) (os.FileInfo, error) {
			return nil, errFixtureStatFailed
		})
		_, err := store.ReconstructTreeFromFS()
		Expect(err).To(MatchError(errFixtureStatFailed))

		swapTreeSeam(&treeOSStat, func(string) (os.FileInfo, error) {
			return fakeTreeFileInfo{mode: fs.ModeDir}, nil
		})
		errFixtureReadFailed := errors.New("read failed")
		swapTreeSeam(&treeOSReadDir, func(string) ([]os.DirEntry, error) {
			return nil, errFixtureReadFailed
		})
		_, err = store.ReconstructTreeFromFS()
		Expect(err).To(MatchError(errFixtureReadFailed))
		Expect(store.reconstructTreeRecursive(root, parent, time.Now().UTC(), map[PageID]string{RootPageID: root})).To(MatchError(ErrReadDirectory))

		swapTreeSeam(&treeOSReadDir, func(path string) ([]os.DirEntry, error) {
			if path == root {
				return []os.DirEntry{fakeTreeDirEntry{name: "index.md"}}, nil
			}
			return nil, os.ErrNotExist
		})
		swapTreeSeam(&treeLoadMarkdownFile, func(path string) (*markdown.MarkdownFile, error) {
			return markdown.NewMarkdownFile(path, "# Root\n", markdown.Frontmatter{}), nil
		})
		rootMetadataWriteErr := errors.New("write failed")
		swapTreeSeam(&treeMarkdownWriteToFile, func(*markdown.MarkdownFile) error {
			return rootMetadataWriteErr
		})
		rootNode := edgeSectionNode(RootPageID, "root", "Root", nil)
		Expect(store.applyRootSectionContent(rootNode, time.Now().UTC())).To(MatchError(rootMetadataWriteErr))

		swapTreeSeam(&treeMarkdownWriteToFile, func(*markdown.MarkdownFile) error { return nil })
		idErr := errors.New("id failed")
		swapTreeSeam(&treeGenerateUniqueID, func() (string, error) {
			return "", idErr
		})
		swapTreeSeam(&treeOSReadDir, func(path string) ([]os.DirEntry, error) {
			if path == root {
				return []os.DirEntry{
					fakeTreeDirEntry{name: ".hidden.md"},
					fakeTreeDirEntry{name: "A.md"},
					fakeTreeDirEntry{name: "a.md"},
					fakeTreeDirEntry{name: "notes.txt"},
				}, nil
			}
			return []os.DirEntry{}, nil
		})
		Expect(store.reconstructTreeRecursive(root, parent, time.Now().UTC(), map[PageID]string{RootPageID: root})).To(MatchError(idErr))

		swapTreeSeam(&treeGenerateUniqueID, func() (string, error) { return "generated", nil })
		sectionIndexErr := errors.New("section index failed")
		swapTreeSeam(&treeOSReadDir, func(path string) ([]os.DirEntry, error) {
			switch path {
			case root:
				return []os.DirEntry{fakeTreeDirEntry{name: "docs", isDir: true}}, nil
			case filepath.Join(root, "docs"):
				return nil, sectionIndexErr
			default:
				return []os.DirEntry{}, nil
			}
		})
		Expect(store.reconstructTreeRecursive(root, parent, time.Now().UTC(), map[PageID]string{RootPageID: root})).To(MatchError(sectionIndexErr))

		swapTreeSeam(&treeOSReadDir, func(path string) ([]os.DirEntry, error) {
			switch path {
			case root:
				return []os.DirEntry{fakeTreeDirEntry{name: "docs", isDir: true}}, nil
			case filepath.Join(root, "docs"):
				return []os.DirEntry{fakeTreeDirEntry{name: "index.md"}}, nil
			default:
				return []os.DirEntry{}, nil
			}
		})
		loadErr := errors.New("load failed")
		swapTreeSeam(&treeLoadMarkdownFile, func(string) (*markdown.MarkdownFile, error) {
			return nil, loadErr
		})
		Expect(store.reconstructTreeRecursive(root, parent, time.Now().UTC(), map[PageID]string{RootPageID: root})).To(MatchError(loadErr))
	})

	It("CRUD filesystem seams preserve stat, rename, and remove failure contracts", func() {
		page := edgePageNode("page", "page", "Page", parent)
		dest := edgeSectionNode("dest", "dest", "Dest", parent)
		section := edgeSectionNode("section", "section", "Section", parent)

		swapTreeSeam(&treeOSMkdirAll, func(string, os.FileMode) error { return nil })
		swapTreeSeam(&treeOSStat, func(path string) (os.FileInfo, error) {
			if strings.HasSuffix(path, "dest/page.md") {
				return fakeTreeFileInfo{name: "page.md"}, nil
			}
			return nil, os.ErrNotExist
		})
		Expect(store.MoveNode(page, dest)).To(MatchError(ErrPageAlreadyExists))

		statSourceErr := errors.New("stat failed")
		swapTreeSeam(&treeOSStat, func(path string) (os.FileInfo, error) {
			switch {
			case strings.HasSuffix(path, "dest/page.md"):
				return nil, os.ErrNotExist
			case strings.HasSuffix(path, "page.md"):
				return nil, statSourceErr
			default:
				return nil, os.ErrNotExist
			}
		})
		Expect(store.MoveNode(page, dest)).To(MatchError(statSourceErr))

		swapTreeSeam(&treeOSStat, func(path string) (os.FileInfo, error) {
			switch {
			case strings.HasSuffix(path, "dest/page.md"):
				return nil, os.ErrNotExist
			case strings.HasSuffix(path, "page.md"):
				return fakeTreeFileInfo{mode: fs.ModeDir}, nil
			default:
				return nil, os.ErrNotExist
			}
		})
		Expect(store.MoveNode(page, dest)).To(matchDrift("expected file but found folder"))

		swapTreeSeam(&treeOSStat, func(path string) (os.FileInfo, error) {
			if filepath.Base(path) == "page.md" && filepath.Base(filepath.Dir(path)) != "dest" {
				return fakeTreeFileInfo{name: "page.md"}, nil
			}
			return nil, os.ErrNotExist
		})
		moveRenameErr := errors.New("rename failed")
		swapTreeSeam(&treeOSRename, func(string, string) error { return moveRenameErr })
		Expect(store.MoveNode(page, dest)).To(MatchError(moveRenameErr))

		swapTreeSeam(&treeOSStat, func(path string) (os.FileInfo, error) {
			if filepath.Base(path) == "section" && filepath.Base(filepath.Dir(path)) != "dest" {
				return fakeTreeFileInfo{mode: fs.ModeDir}, nil
			}
			return nil, os.ErrNotExist
		})
		Expect(store.MoveNode(section, dest)).To(MatchError(moveRenameErr))

		removePageErr := errors.New("remove failed")
		swapTreeSeam(&treeOSRemove, func(string) error { return removePageErr })
		swapTreeSeam(&treeOSStat, func(path string) (os.FileInfo, error) {
			if strings.HasSuffix(path, "page.md") {
				return fakeTreeFileInfo{name: "page.md"}, nil
			}
			return nil, os.ErrNotExist
		})
		Expect(store.DeletePage(page)).To(MatchError(removePageErr))

		removeSectionErr := errors.New("remove all failed")
		swapTreeSeam(&treeOSRemoveAll, func(string) error { return removeSectionErr })
		swapTreeSeam(&treeOSStat, func(path string) (os.FileInfo, error) {
			if filepath.Base(path) == "section" {
				return fakeTreeFileInfo{mode: fs.ModeDir}, nil
			}
			return nil, os.ErrNotExist
		})
		Expect(store.DeleteSection(section)).To(MatchError(removeSectionErr))
	})

	It("filesystem seams propagate rename, read, sync, path, resolve, and conversion failures", func() {
		page := edgePageNode("page", "page", "Page", parent)
		section := edgeSectionNode("section", "section", "Section", parent)

		swapTreeSeam(&treeOSStat, func(path string) (os.FileInfo, error) {
			if strings.HasSuffix(path, "renamed.md") {
				return fakeTreeFileInfo{name: "renamed.md"}, nil
			}
			return nil, os.ErrNotExist
		})
		Expect(store.RenameNode(page, "renamed")).To(MatchError(ErrPageAlreadyExists))

		statRenameErr := errors.New("stat failed")
		swapTreeSeam(&treeOSStat, func(path string) (os.FileInfo, error) {
			switch {
			case strings.HasSuffix(path, "renamed.md"):
				return nil, os.ErrNotExist
			case strings.HasSuffix(path, "page.md"):
				return nil, statRenameErr
			default:
				return nil, os.ErrNotExist
			}
		})
		Expect(store.RenameNode(page, "renamed")).To(MatchError(statRenameErr))

		swapTreeSeam(&treeOSStat, func(path string) (os.FileInfo, error) {
			if strings.HasSuffix(path, "page.md") {
				return fakeTreeFileInfo{name: "page.md"}, nil
			}
			return nil, os.ErrNotExist
		})
		renameNodeErr := errors.New("rename failed")
		swapTreeSeam(&treeOSRename, func(string, string) error { return renameNodeErr })
		Expect(store.RenameNode(page, "renamed")).To(MatchError(renameNodeErr))

		swapTreeSeam(&treeOSStat, func(path string) (os.FileInfo, error) {
			if filepath.Base(path) == "section" {
				return fakeTreeFileInfo{mode: fs.ModeDir}, nil
			}
			return nil, os.ErrNotExist
		})
		Expect(store.RenameNode(section, "renamed-section")).To(MatchError(renameNodeErr))

		swapTreeSeam(&treeOSRename, func(string, string) error { return nil })
		errFixtureReadFailed := errors.New("read failed")
		swapTreeSeam(&treeOSReadFile, func(string) ([]byte, error) { return nil, errFixtureReadFailed })
		swapTreeSeam(&treeOSStat, func(path string) (os.FileInfo, error) {
			if strings.HasSuffix(path, "page.md") {
				return fakeTreeFileInfo{name: "page.md"}, nil
			}
			return nil, os.ErrNotExist
		})
		_, err := store.ReadPageRaw(page)
		Expect(err).To(MatchError(errFixtureReadFailed))

		swapTreeSeam(&treeOSReadFile, func(string) ([]byte, error) { return []byte("# Page\n"), nil })
		errFixtureParseFailed := errors.New("parse failed")
		swapTreeSeam(&treeNewMarkdownFileFromRaw, func(string, string) (*markdown.MarkdownFile, error) {
			return nil, errFixtureParseFailed
		})
		_, _, err = store.ReadPageAndRaw(page)
		Expect(err).To(MatchError(errFixtureParseFailed))
		_, err = store.ReadPageContent(page)
		Expect(err).To(MatchError(errFixtureParseFailed))

		swapTreeSeam(&treeNewMarkdownFileFromRaw, markdown.NewMarkdownFileFromRaw)
		swapTreeSeam(&treeLoadMarkdownFile, func(path string) (*markdown.MarkdownFile, error) {
			return markdown.NewMarkdownFile(path, "# Page\n", markdown.Frontmatter{}), nil
		})
		errFixtureWriteFailed := errors.New("write failed")
		swapTreeSeam(&treeMarkdownWriteToFile, func(*markdown.MarkdownFile) error { return errFixtureWriteFailed })
		Expect(store.SyncMetadataIfExists(page)).To(MatchError(errFixtureWriteFailed))

		relErr := errors.New("rel failed")
		swapTreeSeam(&treeFilepathRel, func(string, string) (string, error) {
			return "", relErr
		})
		store.setWorkspaceSourcePathForPhysicalPath(page, filepath.Join(root, "page.md"), "page", NodeKindPage)
		Expect(page.WorkspaceSourcePath).To(BeEmpty())
		Expect(store.requirePathInRoot("relOp", filepath.Join(root, "page.md"))).To(MatchError(relErr))

		swapTreeSeam(&treeFilepathRel, filepath.Rel)
		errFixtureAbsFailed := errors.New("abs failed")
		swapTreeSeam(&treeFilepathAbs, func(string) (string, error) { return "", errFixtureAbsFailed })
		_, err = resolvePathForContainment(root)
		Expect(err).To(MatchError(errFixtureAbsFailed))

		swapTreeSeam(&treeFilepathAbs, filepath.Abs)
		swapTreeSeam(&treeFilepathEvalSymlinks, func(string) (string, error) { return "", os.ErrNotExist })
		resolved, err := resolvePathForContainment(filepath.Join(root, "missing", "page.md"))
		Expect(err).NotTo(HaveOccurred())
		Expect(resolved).To(ContainSubstring("missing"))

		workspaceSection := edgeSectionNode("workspace-section", "workspace-section", "Workspace Section", parent)
		workspaceSection.WorkspaceSourcePath = "Imported/Section"
		swapTreeSeam(&treeOSReadDir, func(string) ([]os.DirEntry, error) { return nil, errFixtureReadFailed })
		path, exists, err := store.workspaceContentPathForNode(workspaceSection, "workspaceContent")
		failedWorkspaceLookup := workspaceContentPathLookup{Path: path, Exists: exists, Err: err}
		Expect(failedWorkspaceLookup).To(matchMissingWorkspaceContentPath(
			BeEmpty(),
			MatchError(errFixtureReadFailed),
		))

		swapTreeSeam(&treeOSReadDir, func(string) ([]os.DirEntry, error) { return []os.DirEntry{}, nil })
		swapTreeSeam(&treeOSMkdirAll, func(string, os.FileMode) error { return errors.New("mkdir failed") })
		_, err = store.contentPathForNodeWrite(workspaceSection)
		Expect(err).To(MatchError(ErrEnsureFolder))

		swapTreeSeam(&treeOSMkdirAll, func(string, os.FileMode) error { return nil })
		swapTreeSeam(&treeOSStat, func(path string) (os.FileInfo, error) {
			switch {
			case strings.HasSuffix(path, "page.md"):
				return fakeTreeFileInfo{name: "page.md"}, nil
			case strings.HasSuffix(path, "section"):
				return fakeTreeFileInfo{mode: fs.ModeDir}, nil
			default:
				return nil, os.ErrNotExist
			}
		})
		resolvedNode, err := store.resolveNode(page)
		Expect(err).NotTo(HaveOccurred())
		Expect(resolvedNode.Kind).To(Equal(NodeKindPage))
		resolvedNode, err = store.resolveNode(section)
		Expect(err).NotTo(HaveOccurred())
		Expect(resolvedNode.Kind).To(Equal(NodeKindSection))

		statErr := errors.New("stat failed")
		swapTreeSeam(&treeOSStat, func(string) (os.FileInfo, error) { return nil, statErr })
		Expect(store.ConvertNode(section, NodeKindPage)).To(MatchError(statErr))

		swapTreeSeam(&treeOSStat, func(path string) (os.FileInfo, error) {
			if strings.HasSuffix(path, "section") {
				return fakeTreeFileInfo{mode: fs.ModeDir}, nil
			}
			return nil, os.ErrNotExist
		})
		readErr := errors.New("read failed")
		swapTreeSeam(&treeOSReadDir, func(string) ([]os.DirEntry, error) { return nil, readErr })
		Expect(store.ConvertNode(section, NodeKindPage)).To(MatchError(readErr))

		swapTreeSeam(&treeOSReadDir, func(string) ([]os.DirEntry, error) {
			return []os.DirEntry{fakeTreeDirEntry{name: "index.md"}}, nil
		})
		convertRenameErr := errors.New("rename failed")
		swapTreeSeam(&treeOSRename, func(string, string) error { return convertRenameErr })
		swapTreeSeam(&treeOSStat, func(path string) (os.FileInfo, error) {
			if strings.HasSuffix(path, "section") || strings.HasSuffix(path, "index.md") {
				return fakeTreeFileInfo{mode: map[bool]os.FileMode{true: fs.ModeDir, false: 0}[strings.HasSuffix(path, "section")]}, nil
			}
			return nil, os.ErrNotExist
		})
		Expect(store.ConvertNode(section, NodeKindPage)).To(MatchError(convertRenameErr))
	})

	It("migration adapters, route mapping, and content paths preserve legacy compatibility", func() {
		page := edgePageNode("page", "page", "Page", parent)
		section := edgeSectionNode("section", "section", "Section", parent)

		adapter := &migrationStoreAdapter{store: store}
		_, err := adapter.ResolveNode(&migrationNodeAdapter{node: page})
		var notFound *NotFoundError
		Expect(err).To(matchErrorAs(&notFound), "expected NotFoundError, got %T: %v", err, err)

		svc := NewTreeService(tempTreeDir())
		svc.tree = edgeSectionNode(RootPageID, "root", "Root", nil)
		swapTreeSeam(&treeWriteFileAtomic, func(string, []byte, os.FileMode) error {
			return errors.New("snapshot failed")
		})
		Expect(svc.persistLegacyTreeSnapshotLocked()).To(MatchError(ErrWriteLegacyTreeSnapshot))

		swapTreeSeam(&treeWriteFileAtomic, func(string, []byte, os.FileMode) error { return nil })
		swapTreeSeam(&treeOSReadDir, func(string) ([]os.DirEntry, error) {
			return []os.DirEntry{fakeTreeDirEntry{name: "entry.md"}}, nil
		})
		swapTreeSeam(&treeMapWorkspaceMarkdownRoute, func(string, string, bool) (WorkspaceMarkdownRoute, error) {
			return WorkspaceMarkdownRoute{}, errors.New("route failed")
		})
		Expect(store.reconstructTreeRecursive(root, parent, time.Now().UTC(), map[PageID]string{RootPageID: root})).To(Succeed())

		swapTreeSeam(&treeMapWorkspaceMarkdownRoute, func(string, string, bool) (WorkspaceMarkdownRoute, error) {
			return WorkspaceMarkdownRoute{Skip: true}, nil
		})
		Expect(store.reconstructTreeRecursive(root, parent, time.Now().UTC(), map[PageID]string{RootPageID: root})).To(Succeed())

		swapTreeSeam(&treeMapWorkspaceMarkdownRoute, func(_ string, _ string, isDir bool) (WorkspaceMarkdownRoute, error) {
			if isDir {
				return WorkspaceMarkdownRoute{Kind: NodeKindSection}, nil
			}
			return WorkspaceMarkdownRoute{Kind: NodeKindPage}, nil
		})
		swapTreeSeam(&treeOSReadDir, func(path string) ([]os.DirEntry, error) {
			if path == root {
				return []os.DirEntry{fakeTreeDirEntry{name: "empty", isDir: true}}, nil
			}
			return []os.DirEntry{}, nil
		})
		Expect(store.reconstructTreeRecursive(root, parent, time.Now().UTC(), map[PageID]string{RootPageID: root})).To(Succeed())

		swapTreeSeam(&treeOSReadDir, func(path string) ([]os.DirEntry, error) {
			if path == root {
				return []os.DirEntry{fakeTreeDirEntry{name: "empty.md"}}, nil
			}
			return []os.DirEntry{}, nil
		})
		Expect(store.reconstructTreeRecursive(root, parent, time.Now().UTC(), map[PageID]string{RootPageID: root})).To(Succeed())

		swapTreeSeam(&treeMapWorkspaceMarkdownRoute, MapWorkspaceMarkdownRoute)
		workspaceRelErr := errors.New("rel failed")
		swapTreeSeam(&treeFilepathRel, func(string, string) (string, error) {
			return "", workspaceRelErr
		})
		Expect(store.reconstructTreeRecursive(root, parent, time.Now().UTC(), map[PageID]string{RootPageID: root})).To(MatchError(workspaceRelErr))

		swapTreeSeam(&treeFilepathRel, filepath.Rel)
		looseParent := edgeSectionNode("loose", "loose", "Loose", nil)
		Expect(store.SaveChildOrder(looseParent)).To(matchInvalidOp("dirPathForNode"))

		parent.Children = []*PageNode{nil, page}
		swapTreeSeam(&treeWriteFileAtomic, func(string, []byte, os.FileMode) error { return nil })
		Expect(store.SaveChildOrder(parent)).To(Succeed())

		section.WorkspaceSourcePath = "Imported/Section"
		swapTreeSeam(&treeOSReadDir, func(string) ([]os.DirEntry, error) {
			return []os.DirEntry{fakeTreeDirEntry{name: "index.md"}}, nil
		})
		sourcePath, exists, err := store.workspaceContentPathForNode(section, "workspaceContent")
		sectionSourceLookup := workspaceContentPathLookup{Path: sourcePath, Exists: exists, Err: err}
		Expect(sectionSourceLookup).To(matchExistingWorkspaceContentPath(
			ContainSubstring("index.md"),
			Succeed(),
		))

		page.WorkspaceSourcePath = "../outside.md"
		sourcePath, exists, err = store.workspaceContentPathForNode(page, "workspaceContent")
		outsideSourceLookup := workspaceContentPathLookup{Path: sourcePath, Exists: exists, Err: err}
		Expect(outsideSourceLookup).To(matchMissingWorkspaceContentPath(
			BeEmpty(),
			matchInvalidOp("workspaceContent"),
		))

		_, err = store.contentPathForNodeRead(edgePageNode("loose-page", "loose-page", "Loose Page", nil))
		Expect(err).To(matchInvalidOp("dirPathForNode"))
		_, err = store.contentPathForNodeWrite(edgePageNode("loose-page", "loose-page", "Loose Page", nil))
		Expect(err).To(matchInvalidOp("dirPathForNode"))

		swapTreeSeam(&treeOSStat, func(path string) (os.FileInfo, error) {
			if strings.HasSuffix(path, "section.md") {
				return nil, os.ErrNotExist
			}
			if strings.HasSuffix(path, "section") {
				return fakeTreeFileInfo{mode: fs.ModeDir}, nil
			}
			return nil, os.ErrNotExist
		})
		errFixtureResolveReadFailed := errors.New("read failed")
		swapTreeSeam(&treeOSReadDir, func(string) ([]os.DirEntry, error) {
			return nil, errFixtureResolveReadFailed
		})
		_, err = store.resolveNode(section)
		Expect(err).To(MatchError(errFixtureResolveReadFailed))
	})

	It("reconstruction seams handle duplicates, skipped paths, and metadata writeback", func() {
		now := time.Now().UTC()

		swapTreeSeam(&treeGenerateUniqueID, func() (string, error) { return "generated", nil })
		swapTreeSeam(&treeMarkdownWriteToFile, func(*markdown.MarkdownFile) error { return nil })
		swapTreeSeam(&treeOSReadDir, func(path string) ([]os.DirEntry, error) {
			if path == root {
				return []os.DirEntry{
					fakeTreeDirEntry{name: "a", isDir: true},
					fakeTreeDirEntry{name: "b", isDir: true},
				}, nil
			}
			return []os.DirEntry{}, nil
		})
		swapTreeSeam(&treeMapWorkspaceMarkdownRoute, func(_ string, relPath string, isDir bool) (WorkspaceMarkdownRoute, error) {
			Expect(isDir).To(BeTrue())
			return WorkspaceMarkdownRoute{Kind: NodeKindSection, RoutePath: "same", SourcePath: WorkspaceSourcePathFromString(relPath)}, nil
		})
		Expect(store.reconstructTreeRecursive(root, parent, now, map[PageID]string{RootPageID: root})).To(MatchError(ErrDuplicateReconstructedSlug))

		ids := []string{"same-id", "same-id"}
		swapTreeSeam(&treeGenerateUniqueID, func() (string, error) {
			id := ids[0]
			ids = ids[1:]
			return id, nil
		})
		swapTreeSeam(&treeMapWorkspaceMarkdownRoute, func(_ string, relPath string, isDir bool) (WorkspaceMarkdownRoute, error) {
			Expect(isDir).To(BeTrue())
			return WorkspaceMarkdownRoute{Kind: NodeKindSection, RoutePath: RoutePathFromString(relPath), SourcePath: WorkspaceSourcePathFromString(relPath)}, nil
		})
		Expect(store.reconstructTreeRecursive(root, parent, now, map[PageID]string{RootPageID: root})).To(MatchError(ErrDuplicateLeafwikiID))

		swapTreeSeam(&treeGenerateUniqueID, func() (string, error) { return "section-id", nil })
		swapTreeSeam(&treeOSReadDir, func(path string) ([]os.DirEntry, error) {
			switch path {
			case root:
				return []os.DirEntry{fakeTreeDirEntry{name: "docs", isDir: true}}, nil
			case filepath.Join(root, "docs"):
				return []os.DirEntry{fakeTreeDirEntry{name: "index.md"}}, nil
			default:
				return []os.DirEntry{}, nil
			}
		})
		swapTreeSeam(&treeLoadMarkdownFile, func(path string) (*markdown.MarkdownFile, error) {
			return markdown.NewMarkdownFile(path, "# Docs\n", markdown.Frontmatter{}), nil
		})
		writebackErr := errors.New("writeback failed")
		swapTreeSeam(&treeMarkdownWriteToFile, func(*markdown.MarkdownFile) error {
			return writebackErr
		})
		Expect(store.reconstructTreeRecursive(root, parent, now, map[PageID]string{RootPageID: root})).To(MatchError(writebackErr))

		materializeErr := errors.New("materialize failed")
		swapTreeSeam(&treeMarkdownWriteToFile, func(*markdown.MarkdownFile) error {
			return materializeErr
		})
		swapTreeSeam(&treeOSReadDir, func(path string) ([]os.DirEntry, error) {
			if path == root {
				return []os.DirEntry{fakeTreeDirEntry{name: "docs", isDir: true}}, nil
			}
			return []os.DirEntry{}, nil
		})
		Expect(store.reconstructTreeRecursive(root, parent, now, map[PageID]string{RootPageID: root})).To(MatchError(materializeErr))

		swapTreeSeam(&treeMarkdownWriteToFile, func(*markdown.MarkdownFile) error { return nil })
		swapTreeSeam(&treeGenerateUniqueID, func() (string, error) { return "file-id", nil })
		swapTreeSeam(&treeOSReadDir, func(path string) ([]os.DirEntry, error) {
			if path == root {
				return []os.DirEntry{
					fakeTreeDirEntry{name: "notes.txt"},
					fakeTreeDirEntry{name: "README.md"},
					fakeTreeDirEntry{name: "empty.md"},
				}, nil
			}
			return []os.DirEntry{}, nil
		})
		routeCalls := map[string]WorkspaceMarkdownRoute{
			"notes.txt": {Kind: NodeKindPage, RoutePath: "notes"},
			"README.md": {Kind: NodeKindPage, RoutePath: "readme"},
			"empty.md":  {Kind: NodeKindPage},
		}
		swapTreeSeam(&treeMapWorkspaceMarkdownRoute, func(_ string, relPath string, _ bool) (WorkspaceMarkdownRoute, error) {
			return routeCalls[filepath.ToSlash(relPath)], nil
		})
		Expect(store.reconstructTreeRecursive(root, parent, now, map[PageID]string{RootPageID: root})).To(Succeed())

		swapTreeSeam(&treeOSReadDir, func(path string) ([]os.DirEntry, error) {
			if path == root {
				return []os.DirEntry{fakeTreeDirEntry{name: "page.md"}}, nil
			}
			return []os.DirEntry{}, nil
		})
		swapTreeSeam(&treeMapWorkspaceMarkdownRoute, func(_ string, relPath string, _ bool) (WorkspaceMarkdownRoute, error) {
			return WorkspaceMarkdownRoute{Kind: NodeKindPage, RoutePath: "page", SourcePath: WorkspaceSourcePathFromString(relPath)}, nil
		})
		swapTreeSeam(&treeLoadMarkdownFile, func(path string) (*markdown.MarkdownFile, error) {
			return markdown.NewMarkdownFile(path, "# Page\n", markdown.Frontmatter{}), nil
		})
		fileWritebackErr := errors.New("file writeback failed")
		swapTreeSeam(&treeMarkdownWriteToFile, func(*markdown.MarkdownFile) error {
			return fileWritebackErr
		})
		Expect(store.reconstructTreeRecursive(root, parent, now, map[PageID]string{RootPageID: root})).To(MatchError(fileWritebackErr))
	})

	It("store operation seams preserve path and conversion error contracts", func() {
		page := edgePageNode("page", "page", "Page", parent)
		section := edgeSectionNode("section", "section", "Section", parent)
		dest := edgeSectionNode("dest", "dest", "Dest", parent)
		loosePage := edgePageNode("loose", "loose", "Loose", nil)
		looseSection := edgeSectionNode("loose-section", "loose-section", "Loose Section", nil)

		swapTreeSeam(&treeOSMkdirAll, func(string, os.FileMode) error { return nil })
		swapTreeSeam(&treeFilepathRel, func(_, target string) (string, error) {
			if filepath.Base(target) == "page" || filepath.Base(target) == "section" {
				return "../outside", nil
			}
			return ".", nil
		})
		Expect(store.CreatePage(parent, page)).To(matchInvalidOp("CreatePage"))
		Expect(store.CreateSection(parent, section)).To(matchInvalidOp("CreateSection"))

		swapTreeSeam(&treeFilepathRel, filepath.Rel)
		sectionIndexWriteErr := errors.New("section index failed")
		swapTreeSeam(&treeMarkdownWriteToFile, func(*markdown.MarkdownFile) error {
			return sectionIndexWriteErr
		})
		Expect(store.CreateSection(parent, section)).To(MatchError(sectionIndexWriteErr))

		swapTreeSeam(&treeMarkdownWriteToFile, func(*markdown.MarkdownFile) error { return nil })
		Expect(store.UpsertContentPreservingFrontmatter(loosePage, "body")).To(matchInvalidOp("dirPathForNode"))
		Expect(store.UpsertContentReplacingMetadata(loosePage, "body")).To(matchInvalidOp("dirPathForNode"))

		swapTreeSeam(&treeOSStat, func(path string) (os.FileInfo, error) {
			if strings.HasSuffix(path, "page.md") {
				return fakeTreeFileInfo{name: "page.md"}, nil
			}
			return nil, os.ErrNotExist
		})
		loadExistingErr := errors.New("load failed")
		swapTreeSeam(&treeLoadMarkdownFile, func(string) (*markdown.MarkdownFile, error) {
			return nil, loadExistingErr
		})
		Expect(store.UpsertContentPreservingFrontmatter(page, "body")).To(MatchError(loadExistingErr))
		Expect(store.UpsertContentReplacingMetadata(page, "body")).To(MatchError(loadExistingErr))

		swapTreeSeam(&treeLoadMarkdownFile, func(path string) (*markdown.MarkdownFile, error) {
			return markdown.NewMarkdownFile(path, "# Page\n", markdown.Frontmatter{}), nil
		})
		Expect(store.MoveNode(page, looseSection)).To(matchInvalidOp("dirPathForNode"))
		Expect(store.MoveNode(loosePage, parent)).To(matchInvalidOp("dirPathForNode"))
		Expect(store.MoveNode(looseSection, parent)).To(matchInvalidOp("dirPathForNode"))

		swapTreeSeam(&treeFilepathRel, func(_, target string) (string, error) {
			if strings.Contains(filepath.ToSlash(target), "/dest/page.md") {
				return "../outside", nil
			}
			return ".", nil
		})
		Expect(store.MoveNode(page, dest)).To(matchInvalidOp("MoveNode"))

		swapTreeSeam(&treeFilepathRel, filepath.Rel)
		statSectionErr := errors.New("stat failed")
		swapTreeSeam(&treeOSStat, func(path string) (os.FileInfo, error) {
			if strings.HasSuffix(path, "section") {
				return nil, statSectionErr
			}
			return nil, os.ErrNotExist
		})
		Expect(store.MoveNode(section, parent)).To(MatchError(statSectionErr))

		swapTreeSeam(&treeOSStat, func(path string) (os.FileInfo, error) {
			if strings.HasSuffix(path, "section") {
				return nil, os.ErrNotExist
			}
			return nil, os.ErrNotExist
		})
		Expect(store.MoveNode(section, parent)).To(matchDrift("expected folder missing"))

		statDeleteErr := errors.New("stat failed")
		swapTreeSeam(&treeOSStat, func(string) (os.FileInfo, error) {
			return nil, statDeleteErr
		})
		Expect(store.DeletePage(page)).To(MatchError(statDeleteErr))
		Expect(store.DeleteSection(section)).To(MatchError(statDeleteErr))

		Expect(store.DeletePage(loosePage)).To(matchInvalidOp("dirPathForNode"))
		Expect(store.DeleteSection(looseSection)).To(matchInvalidOp("dirPathForNode"))

		swapTreeSeam(&treeFilepathRel, func(string, string) (string, error) {
			return "../outside", nil
		})
		Expect(store.RenameNode(page, "renamed")).To(matchInvalidOp("RenameNode"))

		swapTreeSeam(&treeFilepathRel, filepath.Rel)
		swapTreeSeam(&treeOSStat, func(path string) (os.FileInfo, error) {
			if strings.HasSuffix(path, "renamed-section") {
				return fakeTreeFileInfo{mode: fs.ModeDir}, nil
			}
			return nil, os.ErrNotExist
		})
		Expect(store.RenameNode(section, "renamed-section")).To(MatchError(ErrPageAlreadyExists))

		swapTreeSeam(&treeOSStat, func(string) (os.FileInfo, error) {
			return nil, os.ErrNotExist
		})
		Expect(store.RenameNode(looseSection, "renamed-section")).To(matchInvalidOp("dirPathForNode"))

		swapTreeSeam(&treeOSStat, func(path string) (os.FileInfo, error) {
			if strings.HasSuffix(path, "section") {
				return nil, os.ErrNotExist
			}
			return nil, os.ErrNotExist
		})
		Expect(store.RenameNode(section, "renamed-section")).To(matchDrift("expected folder missing"))

		swapTreeSeam(&treeOSStat, func(path string) (os.FileInfo, error) {
			if strings.HasSuffix(path, "page.md") {
				return nil, os.ErrNotExist
			}
			return nil, os.ErrNotExist
		})
		Expect(store.RenameNode(page, "renamed")).To(matchDrift("expected file missing"))

		_, err := store.ReadPageRaw(loosePage)
		Expect(err).To(matchInvalidOp("dirPathForNode"))
		_, _, err = store.ReadPageAndRaw(loosePage)
		Expect(err).To(matchInvalidOp("dirPathForNode"))
		_, err = store.ReadPageContent(loosePage)
		Expect(err).To(matchInvalidOp("dirPathForNode"))

		Expect(store.SyncFrontmatterIfExists(page)).To(matchDrift("expected page file missing"))

		swapTreeSeam(&treeFilepathRel, func(string, string) (string, error) {
			return "../outside", nil
		})
		_, err = store.dirPathForNode(parent)
		Expect(err).To(matchInvalidOp("dirPathForNode"))

		swapTreeSeam(&treeFilepathRel, filepath.Rel)
		errFixtureContentReadFailed := errors.New("read failed")
		swapTreeSeam(&treeOSReadDir, func(string) ([]os.DirEntry, error) {
			return nil, errFixtureContentReadFailed
		})
		_, err = store.contentPathForNodeRead(section)
		Expect(err).To(MatchError(errFixtureContentReadFailed))
		_, err = store.contentPathForNodeWrite(section)
		Expect(err).To(MatchError(errFixtureContentReadFailed))

		swapTreeSeam(&treeOSReadDir, func(string) ([]os.DirEntry, error) {
			return []os.DirEntry{}, nil
		})
		swapTreeSeam(&treeOSMkdirAll, func(string, os.FileMode) error {
			return errors.New("mkdir failed")
		})
		_, err = store.contentPathForNodeWrite(section)
		Expect(err).To(MatchError(ErrEnsureFolder))

		swapTreeSeam(&treeOSMkdirAll, func(string, os.FileMode) error { return nil })
		Expect(store.ConvertNode(loosePage, NodeKindSection)).To(matchInvalidOp("dirPathForNode"))

		page.WorkspaceSourcePath = "Imported/Page.md"
		swapTreeSeam(&treeFilepathRel, func(_, target string) (string, error) {
			if filepath.Base(target) == "page" {
				return "../outside", nil
			}
			return ".", nil
		})
		Expect(store.ConvertNode(page, NodeKindSection)).To(matchInvalidOp("ConvertNode"))
		page.WorkspaceSourcePath = ""

		swapTreeSeam(&treeFilepathRel, filepath.Rel)
		swapTreeSeam(&treeOSStat, func(path string) (os.FileInfo, error) {
			if strings.HasSuffix(path, "page.md") {
				return fakeTreeFileInfo{name: "page.md"}, nil
			}
			return nil, os.ErrNotExist
		})
		convertMkdirErr := errors.New("mkdir failed")
		swapTreeSeam(&treeOSMkdirAll, func(string, os.FileMode) error {
			return convertMkdirErr
		})
		Expect(store.ConvertNode(page, NodeKindSection)).To(MatchError(convertMkdirErr))

		swapTreeSeam(&treeOSMkdirAll, func(string, os.FileMode) error { return nil })
		convertMoveErr := errors.New("rename failed")
		swapTreeSeam(&treeOSRename, func(string, string) error {
			return convertMoveErr
		})
		Expect(store.ConvertNode(page, NodeKindSection)).To(MatchError(convertMoveErr))

		swapTreeSeam(&treeOSRename, func(string, string) error { return nil })
		ensureIndexErr := errors.New("ensure failed")
		swapTreeSeam(&treeMarkdownWriteToFile, func(*markdown.MarkdownFile) error {
			return ensureIndexErr
		})
		Expect(store.ConvertNode(page, NodeKindSection)).To(MatchError(ensureIndexErr))

		swapTreeSeam(&treeMarkdownWriteToFile, func(*markdown.MarkdownFile) error { return nil })
		Expect(store.ConvertNode(looseSection, NodeKindPage)).To(matchInvalidOp("dirPathForNode"))

		section.WorkspaceSourcePath = "Imported/Section"
		swapTreeSeam(&treeFilepathRel, func(_, target string) (string, error) {
			if filepath.Base(target) == "section.md" {
				return "../outside", nil
			}
			return ".", nil
		})
		Expect(store.ConvertNode(section, NodeKindPage)).To(matchInvalidOp("ConvertNode"))
		section.WorkspaceSourcePath = ""

		swapTreeSeam(&treeFilepathRel, filepath.Rel)
		swapTreeSeam(&treeOSStat, func(path string) (os.FileInfo, error) {
			if strings.HasSuffix(path, "section") {
				return fakeTreeFileInfo{mode: fs.ModeDir}, nil
			}
			if strings.HasSuffix(path, "index.md") {
				return nil, os.ErrNotExist
			}
			return nil, os.ErrNotExist
		})
		swapTreeSeam(&treeOSReadDir, func(string) ([]os.DirEntry, error) {
			return []os.DirEntry{}, nil
		})
		writePageErr := errors.New("write page failed")
		swapTreeSeam(&treeMarkdownWriteToFile, func(*markdown.MarkdownFile) error {
			return writePageErr
		})
		Expect(store.ConvertNode(section, NodeKindPage)).To(MatchError(writePageErr))

		swapTreeSeam(&treeMarkdownWriteToFile, func(*markdown.MarkdownFile) error { return nil })
		removeOrderErr := errors.New("remove order failed")
		swapTreeSeam(&treeOSRemove, func(path string) error {
			if strings.HasSuffix(path, orderFilename) {
				return removeOrderErr
			}
			return nil
		})
		Expect(store.ConvertNode(section, NodeKindPage)).To(MatchError(removeOrderErr))

		removeFolderErr := errors.New("remove folder failed")
		swapTreeSeam(&treeOSRemove, func(path string) error {
			if strings.HasSuffix(path, orderFilename) {
				return os.ErrNotExist
			}
			return removeFolderErr
		})
		Expect(store.ConvertNode(section, NodeKindPage)).To(MatchError(removeFolderErr))
	})

	It("node store seams propagate filesystem failure contracts", func() {
		page := edgePageNode("page", "page", "Page", parent)
		section := edgeSectionNode("section", "section", "Section", parent)
		dest := edgeSectionNode("dest", "dest", "Dest", parent)
		loosePage := edgePageNode("loose", "loose", "Loose", nil)

		swapTreeSeam(&treeOSMkdirAll, func(string, os.FileMode) error { return nil })
		swapTreeSeam(&treeOSStat, func(path string) (os.FileInfo, error) {
			if strings.HasSuffix(filepath.ToSlash(path), "dest/section") {
				return fakeTreeFileInfo{mode: fs.ModeDir}, nil
			}
			return nil, os.ErrNotExist
		})
		Expect(store.MoveNode(section, dest)).To(MatchError(ErrPageAlreadyExists))

		statRenameSectionErr := errors.New("stat section failed")
		swapTreeSeam(&treeOSStat, func(path string) (os.FileInfo, error) {
			switch {
			case strings.HasSuffix(path, "renamed-section"):
				return nil, os.ErrNotExist
			case strings.HasSuffix(path, "section"):
				return nil, statRenameSectionErr
			default:
				return nil, os.ErrNotExist
			}
		})
		Expect(store.RenameNode(section, "renamed-section")).To(MatchError(statRenameSectionErr))
		Expect(store.RenameNode(loosePage, "renamed")).To(matchInvalidOp("dirPathForNode"))

		swapTreeSeam(&treeOSStat, func(path string) (os.FileInfo, error) {
			if strings.HasSuffix(path, "page.md") {
				return fakeTreeFileInfo{name: "page.md"}, nil
			}
			return nil, os.ErrNotExist
		})
		swapTreeSeam(&treeOSReadFile, func(string) ([]byte, error) {
			page.WorkspaceSourcePath = "../outside.md"
			return []byte("# Page\n"), nil
		})
		_, raw, err := store.ReadPageAndRaw(page)
		Expect(err).To(matchInvalidOp("contentPathForNodeRead"))
		Expect(raw).To(Equal("# Page\n"))

		page.WorkspaceSourcePath = ""
		_, err = store.ReadPageContent(page)
		Expect(err).To(matchInvalidOp("contentPathForNodeRead"))

		err = store.SyncMetadataIfExists(loosePage)
		Expect(err).To(matchInvalidOp("dirPathForNode"))

		_, err = store.contentPathForNodeRead(loosePage)
		Expect(err).To(matchInvalidOp("dirPathForNode"))

		swapTreeSeam(&treeFilepathRel, func(string, string) (string, error) {
			return "../outside", nil
		})
		page.WorkspaceSourcePath = ""
		_, err = store.contentPathForNodeRead(page)
		Expect(err).To(matchInvalidOp("dirPathForNode"))
		swapTreeSeam(&treeFilepathRel, filepath.Rel)

		workspaceWriteSection := edgeSectionNode("workspace-write-section", "workspace-write-section", "Workspace Write Section", parent)
		workspaceWriteSection.WorkspaceSourcePath = "../outside"
		_, err = store.contentPathForNodeWrite(workspaceWriteSection)
		Expect(err).To(matchInvalidOp("contentPathForNodeWrite"))

		errFixtureEvalFailed := errors.New("eval failed")
		evalCalls := 0
		swapTreeSeam(&treeFilepathEvalSymlinks, func(string) (string, error) {
			evalCalls++
			if evalCalls == 1 {
				return "", os.ErrNotExist
			}
			return "", errFixtureEvalFailed
		})
		_, err = resolvePathForContainment(filepath.Join(root, "missing", "page.md"))
		Expect(err).To(MatchError(errFixtureEvalFailed))

		workspaceSection := edgeSectionNode("workspace-section", "workspace-section", "Workspace Section", parent)
		workspaceSection.WorkspaceSourcePath = "../outside"
		swapTreeSeam(&treeFilepathEvalSymlinks, filepath.EvalSymlinks)
		swapTreeSeam(&treeOSReadDir, func(string) ([]os.DirEntry, error) {
			return nil, os.ErrNotExist
		})
		path, exists, err := store.workspaceContentPathForNode(workspaceSection, "workspaceContent")
		invalidWorkspaceLookup := workspaceContentPathLookup{Path: path, Exists: exists, Err: err}
		Expect(invalidWorkspaceLookup).To(matchMissingWorkspaceContentPath(
			BeEmpty(),
			matchInvalidOp("workspaceContent"),
		))

		unknownWorkspace := edgePageNode("unknown", "unknown", "Unknown", parent)
		unknownWorkspace.Kind = NodeKind("unknown")
		unknownWorkspace.WorkspaceSourcePath = "custom.md"
		path, exists, err = store.workspaceContentPathForNode(unknownWorkspace, "workspaceContent")
		unknownWorkspaceLookup := workspaceContentPathLookup{Path: path, Exists: exists, Err: err}
		Expect(unknownWorkspaceLookup).To(matchMissingWorkspaceContentPath(
			BeEmpty(),
			Succeed(),
		))

		relCalls := 0
		swapTreeSeam(&treeFilepathRel, func(string, string) (string, error) {
			relCalls++
			if relCalls == 1 {
				return ".", nil
			}
			return "../outside", nil
		})
		swapTreeSeam(&treeOSReadDir, func(string) ([]os.DirEntry, error) {
			return nil, os.ErrNotExist
		})
		_, err = store.contentPathForNodeRead(section)
		Expect(err).To(matchInvalidOp("contentPathForNodeRead"))

		page.WorkspaceSourcePath = ""
		relCalls = 0
		_, err = store.contentPathForNodeRead(page)
		Expect(err).To(matchInvalidOp("contentPathForNodeRead"))

		_, err = store.contentPathForNodeWrite(nil)
		Expect(err).To(matchInvalidOp("contentPathForNodeWrite"))

		page.WorkspaceSourcePath = "Imported/Page.md"
		swapTreeSeam(&treeFilepathRel, filepath.Rel)
		path, err = store.contentPathForNodeWrite(page)
		Expect(err).NotTo(HaveOccurred())
		Expect(path).To(ContainSubstring(filepath.Join("Imported", "Page.md")))
		page.WorkspaceSourcePath = ""

		relCalls = 0
		swapTreeSeam(&treeFilepathRel, func(string, string) (string, error) {
			relCalls++
			if relCalls == 1 {
				return ".", nil
			}
			return "../outside", nil
		})
		_, err = store.contentPathForNodeWrite(section)
		Expect(err).To(matchInvalidOp("contentPathForNodeWrite"))

		relCalls = 0
		_, err = store.contentPathForNodeWrite(page)
		Expect(err).To(matchInvalidOp("contentPathForNodeWrite"))

		_, err = store.resolveNode(loosePage)
		Expect(err).To(matchInvalidOp("dirPathForNode"))

		swapTreeSeam(&treeFilepathRel, filepath.Rel)
		swapTreeSeam(&treeOSStat, func(path string) (os.FileInfo, error) {
			if strings.HasSuffix(path, "page.md") {
				return nil, os.ErrNotExist
			}
			return nil, os.ErrNotExist
		})
		missingFolderErr := errors.New("mkdir missing folder failed")
		swapTreeSeam(&treeOSMkdirAll, func(string, os.FileMode) error {
			return missingFolderErr
		})
		Expect(store.ConvertNode(page, NodeKindSection)).To(MatchError(missingFolderErr))

		swapTreeSeam(&treeOSMkdirAll, func(string, os.FileMode) error { return nil })
		swapTreeSeam(&treeOSReadDir, func(string) ([]os.DirEntry, error) {
			return nil, os.ErrNotExist
		})
		swapTreeSeam(&treeMarkdownWriteToFile, func(*markdown.MarkdownFile) error { return nil })
		Expect(store.ConvertNode(page, NodeKindSection)).To(Succeed())

		page.Kind = NodeKindPage
		ensurePageIndexErr := errors.New("ensure index failed")
		swapTreeSeam(&treeMarkdownWriteToFile, func(*markdown.MarkdownFile) error {
			return ensurePageIndexErr
		})
		Expect(store.ConvertNode(page, NodeKindSection)).To(MatchError(ensurePageIndexErr))
	})
})

func swapTreeSeam[T any](target *T, replacement T) {
	GinkgoHelper()
	previous := *target
	*target = replacement
	DeferCleanup(func() {
		*target = previous
	})
}

type fakeTreeFileInfo struct {
	name    string
	mode    os.FileMode
	modTime time.Time
}

func (f fakeTreeFileInfo) Name() string {
	if f.name == "" {
		return "fake"
	}
	return f.name
}

func (f fakeTreeFileInfo) Size() int64 {
	return 0
}

func (f fakeTreeFileInfo) Mode() os.FileMode {
	return f.mode
}

func (f fakeTreeFileInfo) ModTime() time.Time {
	if f.modTime.IsZero() {
		return time.Date(2026, time.June, 27, 12, 0, 0, 0, time.UTC)
	}
	return f.modTime
}

func (f fakeTreeFileInfo) IsDir() bool {
	return f.mode.IsDir()
}

func (f fakeTreeFileInfo) Sys() any {
	return nil
}

type fakeTreeDirEntry struct {
	name    string
	isDir   bool
	mode    os.FileMode
	infoErr error
}

func (e fakeTreeDirEntry) Name() string {
	return e.name
}

func (e fakeTreeDirEntry) IsDir() bool {
	return e.isDir
}

func (e fakeTreeDirEntry) Type() os.FileMode {
	if e.isDir {
		return fs.ModeDir
	}
	if e.mode != 0 {
		return e.mode
	}
	return 0
}

func (e fakeTreeDirEntry) Info() (os.FileInfo, error) {
	if e.infoErr != nil {
		return nil, e.infoErr
	}
	return fakeTreeFileInfo{name: e.name, mode: e.Type()}, nil
}
