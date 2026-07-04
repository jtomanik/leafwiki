package tree

import (
	"errors"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/perber/wiki/internal/core/markdown"
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
})
