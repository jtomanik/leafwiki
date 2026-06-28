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

var _ = Describe("tree seam edge coverage", func() {
	var (
		base   string
		root   string
		store  *NodeStore
		parent *PageNode
	)

	BeforeEach(func() {
		base = GinkgoT().TempDir()
		root = filepath.Join(base, "root")
		store = NewNodeStoreWithOptions(NodeStoreOptions{DataDir: filepath.Join(base, "data"), RootDir: root})
		parent = edgeSectionNode(RootPageID, "root", "Root", nil)
	})

	It("covers helper rename and remove failures through filesystem seams", func() {
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
		swapTreeSeam(&treeOSRename, func(string, string) error { return errors.New("rename failed") })

		Expect(EnsurePageIsFolder(root, "guide")).To(MatchError(ContainSubstring("could not move file to index.md")))

		swapTreeSeam(&treeOSStat, func(path string) (os.FileInfo, error) {
			return fakeTreeFileInfo{name: filepath.Base(path), mode: fs.ModeDir}, nil
		})
		swapTreeSeam(&treeOSReadDir, func(string) ([]os.DirEntry, error) {
			return []os.DirEntry{fakeTreeDirEntry{name: "index.md"}}, nil
		})
		Expect(FoldPageFolderIfEmpty(root, "guide")).To(MatchError(ContainSubstring("could not move index.md to flat file")))

		swapTreeSeam(&treeOSRename, func(string, string) error { return nil })
		swapTreeSeam(&treeOSRemove, func(string) error { return errors.New("remove failed") })
		Expect(FoldPageFolderIfEmpty(root, "guide")).To(MatchError(ContainSubstring("could not remove folder")))
	})

	It("covers metadata and section-index failure seams", func() {
		mdFile := markdown.NewMarkdownFile(filepath.Join(root, "page.md"), "# Page\n", markdown.Frontmatter{})
		entry := edgePageNode("page", "page", "Page", parent)

		swapTreeSeam(&treeMarkdownWriteToFile, func(*markdown.MarkdownFile) error { return errors.New("write failed") })
		Expect(store.writeReconstructedMetadata(mdFile, entry)).To(MatchError(ContainSubstring("write reconstructed metadata")))

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
		Expect(err).To(MatchError(ContainSubstring("could not load markdown file")))

		swapTreeSeam(&treeLoadMarkdownFile, func(path string) (*markdown.MarkdownFile, error) {
			return markdown.NewMarkdownFile(path, "# Loaded\n", markdown.Frontmatter{}), nil
		})
		swapTreeSeam(&treeMarkdownWriteToFile, func(*markdown.MarkdownFile) error { return errors.New("write failed") })
		_, err = store.ensureSectionIndexAtPath(section, filepath.Join(root, "section", "index.md"))
		Expect(err).To(MatchError(ContainSubstring("could not write markdown file")))
	})

	It("covers node store operation failures through filesystem seams", func() {
		page := edgePageNode("page", "page", "Page", parent)
		section := edgeSectionNode("section", "section", "Section", parent)

		swapTreeSeam(&treeOSMkdirAll, func(string, os.FileMode) error { return errors.New("mkdir failed") })
		Expect(store.SaveChildOrder(parent)).To(MatchError(ContainSubstring("could not ensure parent directory exists")))
		Expect(store.CreatePage(parent, page)).To(MatchError(ContainSubstring("could not ensure parent directory exists")))
		Expect(store.CreateSection(parent, section)).To(MatchError(ContainSubstring("could not ensure parent directory exists")))
		Expect(store.MoveNode(page, parent)).To(MatchError(ContainSubstring("could not ensure parent directory exists")))

		swapTreeSeam(&treeOSMkdirAll, func(string, os.FileMode) error { return nil })
		swapTreeSeam(&treeWriteFileAtomic, func(string, []byte, os.FileMode) error { return errors.New("atomic failed") })
		Expect(store.SaveChildOrder(parent)).To(MatchError(ContainSubstring("could not atomically write child order file")))

		swapTreeSeam(&treeMarkdownWriteToFile, func(*markdown.MarkdownFile) error { return errors.New("write failed") })
		Expect(store.CreatePage(parent, page)).To(MatchError(ContainSubstring("could not create file")))
		Expect(store.UpsertContent(page, "body")).To(MatchError(ContainSubstring("could not write markdown file")))
		Expect(store.UpsertContentPreservingFrontmatter(page, "body")).To(MatchError(ContainSubstring("could not write markdown file")))
		Expect(store.UpsertContentReplacingMetadata(page, "body")).To(MatchError(ContainSubstring("could not write markdown file")))

		swapTreeSeam(&treeMarkdownWriteToFile, func(*markdown.MarkdownFile) error { return nil })
		swapTreeSeam(&treeOSMkdirAll, func(path string, _ os.FileMode) error {
			if strings.HasSuffix(path, "section") {
				return errors.New("section mkdir failed")
			}
			return nil
		})
		Expect(store.CreateSection(parent, section)).To(MatchError(ContainSubstring("could not create section folder")))
	})

	It("covers reconstruction branches through deterministic seams", func() {
		swapTreeSeam(&treeOSStat, func(string) (os.FileInfo, error) {
			return nil, errors.New("stat failed")
		})
		_, err := store.ReconstructTreeFromFS()
		Expect(err).To(MatchError(ContainSubstring("stat root dir")))

		swapTreeSeam(&treeOSStat, func(string) (os.FileInfo, error) {
			return fakeTreeFileInfo{mode: fs.ModeDir}, nil
		})
		swapTreeSeam(&treeOSReadDir, func(string) ([]os.DirEntry, error) {
			return nil, errors.New("read failed")
		})
		_, err = store.ReconstructTreeFromFS()
		Expect(err).To(MatchError(ContainSubstring("reconstruct root content from fs")))
		Expect(store.reconstructTreeRecursive(root, parent, time.Now().UTC(), map[PageID]string{RootPageID: root})).To(MatchError(ContainSubstring("read dir")))

		swapTreeSeam(&treeOSReadDir, func(path string) ([]os.DirEntry, error) {
			if path == root {
				return []os.DirEntry{fakeTreeDirEntry{name: "index.md"}}, nil
			}
			return nil, os.ErrNotExist
		})
		swapTreeSeam(&treeLoadMarkdownFile, func(path string) (*markdown.MarkdownFile, error) {
			return markdown.NewMarkdownFile(path, "# Root\n", markdown.Frontmatter{}), nil
		})
		swapTreeSeam(&treeMarkdownWriteToFile, func(*markdown.MarkdownFile) error {
			return errors.New("write failed")
		})
		rootNode := edgeSectionNode(RootPageID, "root", "Root", nil)
		Expect(store.applyRootSectionContent(rootNode, time.Now().UTC())).To(MatchError(ContainSubstring("write reconstructed metadata")))

		swapTreeSeam(&treeMarkdownWriteToFile, func(*markdown.MarkdownFile) error { return nil })
		swapTreeSeam(&treeGenerateUniqueID, func() (string, error) {
			return "", errors.New("id failed")
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
		Expect(store.reconstructTreeRecursive(root, parent, time.Now().UTC(), map[PageID]string{RootPageID: root})).To(MatchError(ContainSubstring("generate unique ID")))

		swapTreeSeam(&treeGenerateUniqueID, func() (string, error) { return "generated", nil })
		swapTreeSeam(&treeOSReadDir, func(path string) ([]os.DirEntry, error) {
			switch path {
			case root:
				return []os.DirEntry{fakeTreeDirEntry{name: "docs", isDir: true}}, nil
			case filepath.Join(root, "docs"):
				return nil, errors.New("section index failed")
			default:
				return []os.DirEntry{}, nil
			}
		})
		Expect(store.reconstructTreeRecursive(root, parent, time.Now().UTC(), map[PageID]string{RootPageID: root})).To(MatchError(ContainSubstring("resolve section index")))

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
		swapTreeSeam(&treeLoadMarkdownFile, func(string) (*markdown.MarkdownFile, error) {
			return nil, errors.New("load failed")
		})
		Expect(store.reconstructTreeRecursive(root, parent, time.Now().UTC(), map[PageID]string{RootPageID: root})).To(MatchError(ContainSubstring("load section index")))
	})

	It("covers CRUD stat, rename, and remove branches through seams", func() {
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
		Expect(store.MoveNode(page, dest)).To(MatchError(ContainSubstring("already exists")))

		swapTreeSeam(&treeOSStat, func(path string) (os.FileInfo, error) {
			switch {
			case strings.HasSuffix(path, "dest/page.md"):
				return nil, os.ErrNotExist
			case strings.HasSuffix(path, "page.md"):
				return nil, errors.New("stat failed")
			default:
				return nil, os.ErrNotExist
			}
		})
		Expect(store.MoveNode(page, dest)).To(MatchError(ContainSubstring("stat source file")))

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
		swapTreeSeam(&treeOSRename, func(string, string) error { return errors.New("rename failed") })
		Expect(store.MoveNode(page, dest)).To(MatchError(ContainSubstring("could not move file")))

		swapTreeSeam(&treeOSStat, func(path string) (os.FileInfo, error) {
			if filepath.Base(path) == "section" && filepath.Base(filepath.Dir(path)) != "dest" {
				return fakeTreeFileInfo{mode: fs.ModeDir}, nil
			}
			return nil, os.ErrNotExist
		})
		Expect(store.MoveNode(section, dest)).To(MatchError(ContainSubstring("could not move folder")))

		swapTreeSeam(&treeOSRemove, func(string) error { return errors.New("remove failed") })
		swapTreeSeam(&treeOSStat, func(path string) (os.FileInfo, error) {
			if strings.HasSuffix(path, "page.md") {
				return fakeTreeFileInfo{name: "page.md"}, nil
			}
			return nil, os.ErrNotExist
		})
		Expect(store.DeletePage(page)).To(MatchError(ContainSubstring("could not delete file")))

		swapTreeSeam(&treeOSRemoveAll, func(string) error { return errors.New("remove all failed") })
		swapTreeSeam(&treeOSStat, func(path string) (os.FileInfo, error) {
			if filepath.Base(path) == "section" {
				return fakeTreeFileInfo{mode: fs.ModeDir}, nil
			}
			return nil, os.ErrNotExist
		})
		Expect(store.DeleteSection(section)).To(MatchError(ContainSubstring("could not delete folder")))
	})

	It("covers rename, read, sync, path, resolve, and conversion seam branches", func() {
		page := edgePageNode("page", "page", "Page", parent)
		section := edgeSectionNode("section", "section", "Section", parent)

		swapTreeSeam(&treeOSStat, func(path string) (os.FileInfo, error) {
			if strings.HasSuffix(path, "renamed.md") {
				return fakeTreeFileInfo{name: "renamed.md"}, nil
			}
			return nil, os.ErrNotExist
		})
		Expect(store.RenameNode(page, "renamed")).To(MatchError(ContainSubstring("already exists")))

		swapTreeSeam(&treeOSStat, func(path string) (os.FileInfo, error) {
			switch {
			case strings.HasSuffix(path, "renamed.md"):
				return nil, os.ErrNotExist
			case strings.HasSuffix(path, "page.md"):
				return nil, errors.New("stat failed")
			default:
				return nil, os.ErrNotExist
			}
		})
		Expect(store.RenameNode(page, "renamed")).To(MatchError(ContainSubstring("stat source file")))

		swapTreeSeam(&treeOSStat, func(path string) (os.FileInfo, error) {
			if strings.HasSuffix(path, "page.md") {
				return fakeTreeFileInfo{name: "page.md"}, nil
			}
			return nil, os.ErrNotExist
		})
		swapTreeSeam(&treeOSRename, func(string, string) error { return errors.New("rename failed") })
		Expect(store.RenameNode(page, "renamed")).To(MatchError(ContainSubstring("could not rename file")))

		swapTreeSeam(&treeOSStat, func(path string) (os.FileInfo, error) {
			if filepath.Base(path) == "section" {
				return fakeTreeFileInfo{mode: fs.ModeDir}, nil
			}
			return nil, os.ErrNotExist
		})
		Expect(store.RenameNode(section, "renamed-section")).To(MatchError(ContainSubstring("could not rename folder")))

		swapTreeSeam(&treeOSRename, func(string, string) error { return nil })
		swapTreeSeam(&treeOSReadFile, func(string) ([]byte, error) { return nil, errors.New("read failed") })
		swapTreeSeam(&treeOSStat, func(path string) (os.FileInfo, error) {
			if strings.HasSuffix(path, "page.md") {
				return fakeTreeFileInfo{name: "page.md"}, nil
			}
			return nil, os.ErrNotExist
		})
		_, err := store.ReadPageRaw(page)
		Expect(err).To(MatchError("read failed"))

		swapTreeSeam(&treeOSReadFile, func(string) ([]byte, error) { return []byte("# Page\n"), nil })
		swapTreeSeam(&treeNewMarkdownFileFromRaw, func(string, string) (*markdown.MarkdownFile, error) {
			return nil, errors.New("parse failed")
		})
		_, _, err = store.ReadPageAndRaw(page)
		Expect(err).To(MatchError("parse failed"))
		_, err = store.ReadPageContent(page)
		Expect(err).To(MatchError("parse failed"))

		swapTreeSeam(&treeNewMarkdownFileFromRaw, markdown.NewMarkdownFileFromRaw)
		swapTreeSeam(&treeLoadMarkdownFile, func(path string) (*markdown.MarkdownFile, error) {
			return markdown.NewMarkdownFile(path, "# Page\n", markdown.Frontmatter{}), nil
		})
		swapTreeSeam(&treeMarkdownWriteToFile, func(*markdown.MarkdownFile) error { return errors.New("write failed") })
		Expect(store.SyncMetadataIfExists(page)).To(MatchError(ContainSubstring("write markdown file")))

		swapTreeSeam(&treeFilepathRel, func(string, string) (string, error) {
			return "", errors.New("rel failed")
		})
		store.setWorkspaceSourcePathForPhysicalPath(page, filepath.Join(root, "page.md"), "page", NodeKindPage)
		Expect(page.WorkspaceSourcePath).To(BeEmpty())
		Expect(store.requirePathInRoot("relOp", filepath.Join(root, "page.md"))).To(MatchError(ContainSubstring("compare path to root dir")))

		swapTreeSeam(&treeFilepathRel, filepath.Rel)
		swapTreeSeam(&treeFilepathAbs, func(string) (string, error) { return "", errors.New("abs failed") })
		_, err = resolvePathForContainment(root)
		Expect(err).To(MatchError("abs failed"))

		swapTreeSeam(&treeFilepathAbs, filepath.Abs)
		swapTreeSeam(&treeFilepathEvalSymlinks, func(string) (string, error) { return "", os.ErrNotExist })
		resolved, err := resolvePathForContainment(filepath.Join(root, "missing", "page.md"))
		Expect(err).NotTo(HaveOccurred())
		Expect(resolved).To(ContainSubstring("missing"))

		workspaceSection := edgeSectionNode("workspace-section", "workspace-section", "Workspace Section", parent)
		workspaceSection.WorkspaceSourcePath = "Imported/Section"
		swapTreeSeam(&treeOSReadDir, func(string) ([]os.DirEntry, error) { return nil, errors.New("read failed") })
		_, _, err = store.workspaceContentPathForNode(workspaceSection, "workspaceContent")
		Expect(err).To(MatchError("read failed"))

		swapTreeSeam(&treeOSReadDir, func(string) ([]os.DirEntry, error) { return []os.DirEntry{}, nil })
		swapTreeSeam(&treeOSMkdirAll, func(string, os.FileMode) error { return errors.New("mkdir failed") })
		_, err = store.contentPathForNodeWrite(workspaceSection)
		Expect(err).To(MatchError(ContainSubstring("could not ensure folder")))

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

		swapTreeSeam(&treeOSStat, func(string) (os.FileInfo, error) { return nil, errors.New("stat failed") })
		Expect(store.ConvertNode(section, NodeKindPage)).To(MatchError("stat failed"))

		swapTreeSeam(&treeOSStat, func(path string) (os.FileInfo, error) {
			if strings.HasSuffix(path, "section") {
				return fakeTreeFileInfo{mode: fs.ModeDir}, nil
			}
			return nil, os.ErrNotExist
		})
		swapTreeSeam(&treeOSReadDir, func(string) ([]os.DirEntry, error) { return nil, errors.New("read failed") })
		Expect(store.ConvertNode(section, NodeKindPage)).To(MatchError("read failed"))

		swapTreeSeam(&treeOSReadDir, func(string) ([]os.DirEntry, error) {
			return []os.DirEntry{fakeTreeDirEntry{name: "index.md"}}, nil
		})
		swapTreeSeam(&treeOSRename, func(string, string) error { return errors.New("rename failed") })
		swapTreeSeam(&treeOSStat, func(path string) (os.FileInfo, error) {
			if strings.HasSuffix(path, "section") || strings.HasSuffix(path, "index.md") {
				return fakeTreeFileInfo{mode: map[bool]os.FileMode{true: fs.ModeDir, false: 0}[strings.HasSuffix(path, "section")]}, nil
			}
			return nil, os.ErrNotExist
		})
		Expect(store.ConvertNode(section, NodeKindPage)).To(MatchError(ContainSubstring("could not move index to page")))
	})

	It("covers migration adapters, route mapping, and remaining content path branches", func() {
		page := edgePageNode("page", "page", "Page", parent)
		section := edgeSectionNode("section", "section", "Section", parent)

		adapter := &migrationStoreAdapter{store: store}
		_, err := adapter.ResolveNode(&migrationNodeAdapter{node: page})
		Expect(err).To(HaveOccurred())

		svc := NewTreeService(GinkgoT().TempDir())
		svc.tree = edgeSectionNode(RootPageID, "root", "Root", nil)
		swapTreeSeam(&treeWriteFileAtomic, func(string, []byte, os.FileMode) error {
			return errors.New("snapshot failed")
		})
		Expect(svc.persistLegacyTreeSnapshotLocked()).To(MatchError(ContainSubstring("write legacy migration snapshot")))

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
		swapTreeSeam(&treeFilepathRel, func(string, string) (string, error) {
			return "", errors.New("rel failed")
		})
		Expect(store.reconstructTreeRecursive(root, parent, time.Now().UTC(), map[PageID]string{RootPageID: root})).To(MatchError(ContainSubstring("resolve workspace relative path")))

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
		sourcePath, ok, err := store.workspaceContentPathForNode(section, "workspaceContent")
		Expect(err).NotTo(HaveOccurred())
		Expect(ok).To(BeTrue())
		Expect(sourcePath).To(ContainSubstring("index.md"))

		page.WorkspaceSourcePath = "../outside.md"
		_, _, err = store.workspaceContentPathForNode(page, "workspaceContent")
		Expect(err).To(matchInvalidOp("workspaceContent"))

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
		swapTreeSeam(&treeOSReadDir, func(string) ([]os.DirEntry, error) {
			return nil, errors.New("read failed")
		})
		_, err = store.resolveNode(section)
		Expect(err).To(MatchError("read failed"))
	})

	It("covers remaining reconstruction duplicate, skip, and writeback branches", func() {
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
		Expect(store.reconstructTreeRecursive(root, parent, now, map[PageID]string{RootPageID: root})).To(MatchError(ContainSubstring("duplicate section slug")))

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
		Expect(store.reconstructTreeRecursive(root, parent, now, map[PageID]string{RootPageID: root})).To(MatchError(ContainSubstring("duplicate leafwiki_id")))

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
		swapTreeSeam(&treeMarkdownWriteToFile, func(*markdown.MarkdownFile) error {
			return errors.New("writeback failed")
		})
		Expect(store.reconstructTreeRecursive(root, parent, now, map[PageID]string{RootPageID: root})).To(MatchError(ContainSubstring("write reconstructed metadata")))

		swapTreeSeam(&treeMarkdownWriteToFile, func(*markdown.MarkdownFile) error {
			return errors.New("materialize failed")
		})
		swapTreeSeam(&treeOSReadDir, func(path string) ([]os.DirEntry, error) {
			if path == root {
				return []os.DirEntry{fakeTreeDirEntry{name: "docs", isDir: true}}, nil
			}
			return []os.DirEntry{}, nil
		})
		Expect(store.reconstructTreeRecursive(root, parent, now, map[PageID]string{RootPageID: root})).To(MatchError(ContainSubstring("materialize missing section index")))

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
		swapTreeSeam(&treeMarkdownWriteToFile, func(*markdown.MarkdownFile) error {
			return errors.New("file writeback failed")
		})
		Expect(store.reconstructTreeRecursive(root, parent, now, map[PageID]string{RootPageID: root})).To(MatchError(ContainSubstring("write reconstructed metadata")))
	})

	It("covers remaining store operation path and conversion branches", func() {
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
		swapTreeSeam(&treeMarkdownWriteToFile, func(*markdown.MarkdownFile) error {
			return errors.New("section index failed")
		})
		Expect(store.CreateSection(parent, section)).To(MatchError(ContainSubstring("could not write markdown file")))

		swapTreeSeam(&treeMarkdownWriteToFile, func(*markdown.MarkdownFile) error { return nil })
		Expect(store.UpsertContentPreservingFrontmatter(loosePage, "body")).To(matchInvalidOp("dirPathForNode"))
		Expect(store.UpsertContentReplacingMetadata(loosePage, "body")).To(matchInvalidOp("dirPathForNode"))

		swapTreeSeam(&treeOSStat, func(path string) (os.FileInfo, error) {
			if strings.HasSuffix(path, "page.md") {
				return fakeTreeFileInfo{name: "page.md"}, nil
			}
			return nil, os.ErrNotExist
		})
		swapTreeSeam(&treeLoadMarkdownFile, func(string) (*markdown.MarkdownFile, error) {
			return nil, errors.New("load failed")
		})
		Expect(store.UpsertContentPreservingFrontmatter(page, "body")).To(MatchError(ContainSubstring("could not load markdown file")))
		Expect(store.UpsertContentReplacingMetadata(page, "body")).To(MatchError(ContainSubstring("could not load markdown file")))

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
		swapTreeSeam(&treeOSStat, func(path string) (os.FileInfo, error) {
			if strings.HasSuffix(path, "section") {
				return nil, errors.New("stat failed")
			}
			return nil, os.ErrNotExist
		})
		Expect(store.MoveNode(section, parent)).To(MatchError(ContainSubstring("stat source dir")))

		swapTreeSeam(&treeOSStat, func(path string) (os.FileInfo, error) {
			if strings.HasSuffix(path, "section") {
				return nil, os.ErrNotExist
			}
			return nil, os.ErrNotExist
		})
		Expect(store.MoveNode(section, parent)).To(matchDrift("expected folder missing"))

		swapTreeSeam(&treeOSStat, func(string) (os.FileInfo, error) {
			return nil, errors.New("stat failed")
		})
		Expect(store.DeletePage(page)).To(MatchError(ContainSubstring("stat file")))
		Expect(store.DeleteSection(section)).To(MatchError(ContainSubstring("stat dir")))

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
		Expect(store.RenameNode(section, "renamed-section")).To(MatchError(ContainSubstring("already exists")))

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
		swapTreeSeam(&treeOSReadDir, func(string) ([]os.DirEntry, error) {
			return nil, errors.New("read failed")
		})
		_, err = store.contentPathForNodeRead(section)
		Expect(err).To(MatchError("read failed"))
		_, err = store.contentPathForNodeWrite(section)
		Expect(err).To(MatchError("read failed"))

		swapTreeSeam(&treeOSReadDir, func(string) ([]os.DirEntry, error) {
			return []os.DirEntry{}, nil
		})
		swapTreeSeam(&treeOSMkdirAll, func(string, os.FileMode) error {
			return errors.New("mkdir failed")
		})
		_, err = store.contentPathForNodeWrite(section)
		Expect(err).To(MatchError(ContainSubstring("could not ensure folder")))

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
		swapTreeSeam(&treeOSMkdirAll, func(string, os.FileMode) error {
			return errors.New("mkdir failed")
		})
		Expect(store.ConvertNode(page, NodeKindSection)).To(MatchError(ContainSubstring("could not create folder")))

		swapTreeSeam(&treeOSMkdirAll, func(string, os.FileMode) error { return nil })
		swapTreeSeam(&treeOSRename, func(string, string) error {
			return errors.New("rename failed")
		})
		Expect(store.ConvertNode(page, NodeKindSection)).To(MatchError(ContainSubstring("could not move page into folder")))

		swapTreeSeam(&treeOSRename, func(string, string) error { return nil })
		swapTreeSeam(&treeMarkdownWriteToFile, func(*markdown.MarkdownFile) error {
			return errors.New("ensure failed")
		})
		Expect(store.ConvertNode(page, NodeKindSection)).To(MatchError(ContainSubstring("could not write markdown file")))

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
		swapTreeSeam(&treeMarkdownWriteToFile, func(*markdown.MarkdownFile) error {
			return errors.New("write page failed")
		})
		Expect(store.ConvertNode(section, NodeKindPage)).To(MatchError(ContainSubstring("could not write page file")))

		swapTreeSeam(&treeMarkdownWriteToFile, func(*markdown.MarkdownFile) error { return nil })
		swapTreeSeam(&treeOSRemove, func(path string) error {
			if strings.HasSuffix(path, orderFilename) {
				return errors.New("remove order failed")
			}
			return nil
		})
		Expect(store.ConvertNode(section, NodeKindPage)).To(MatchError(ContainSubstring("could not remove child order file")))

		swapTreeSeam(&treeOSRemove, func(path string) error {
			if strings.HasSuffix(path, orderFilename) {
				return os.ErrNotExist
			}
			return errors.New("remove folder failed")
		})
		Expect(store.ConvertNode(section, NodeKindPage)).To(MatchError("remove folder failed"))
	})

	It("covers final node store seam branches", func() {
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
		Expect(store.MoveNode(section, dest)).To(MatchError(ContainSubstring("already exists")))

		swapTreeSeam(&treeOSStat, func(path string) (os.FileInfo, error) {
			switch {
			case strings.HasSuffix(path, "renamed-section"):
				return nil, os.ErrNotExist
			case strings.HasSuffix(path, "section"):
				return nil, errors.New("stat section failed")
			default:
				return nil, os.ErrNotExist
			}
		})
		Expect(store.RenameNode(section, "renamed-section")).To(MatchError(ContainSubstring("stat source dir")))
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

		evalCalls := 0
		swapTreeSeam(&treeFilepathEvalSymlinks, func(string) (string, error) {
			evalCalls++
			if evalCalls == 1 {
				return "", os.ErrNotExist
			}
			return "", errors.New("eval failed")
		})
		_, err = resolvePathForContainment(filepath.Join(root, "missing", "page.md"))
		Expect(err).To(MatchError("eval failed"))

		workspaceSection := edgeSectionNode("workspace-section", "workspace-section", "Workspace Section", parent)
		workspaceSection.WorkspaceSourcePath = "../outside"
		swapTreeSeam(&treeFilepathEvalSymlinks, filepath.EvalSymlinks)
		swapTreeSeam(&treeOSReadDir, func(string) ([]os.DirEntry, error) {
			return nil, os.ErrNotExist
		})
		_, _, err = store.workspaceContentPathForNode(workspaceSection, "workspaceContent")
		Expect(err).To(matchInvalidOp("workspaceContent"))

		unknownWorkspace := edgePageNode("unknown", "unknown", "Unknown", parent)
		unknownWorkspace.Kind = NodeKind("unknown")
		unknownWorkspace.WorkspaceSourcePath = "custom.md"
		path, ok, err := store.workspaceContentPathForNode(unknownWorkspace, "workspaceContent")
		Expect(err).NotTo(HaveOccurred())
		Expect(ok).To(BeFalse())
		Expect(path).To(BeEmpty())

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
		swapTreeSeam(&treeOSMkdirAll, func(string, os.FileMode) error {
			return errors.New("mkdir missing folder failed")
		})
		Expect(store.ConvertNode(page, NodeKindSection)).To(MatchError(ContainSubstring("could not ensure folder exists")))

		swapTreeSeam(&treeOSMkdirAll, func(string, os.FileMode) error { return nil })
		swapTreeSeam(&treeOSReadDir, func(string) ([]os.DirEntry, error) {
			return nil, os.ErrNotExist
		})
		swapTreeSeam(&treeMarkdownWriteToFile, func(*markdown.MarkdownFile) error { return nil })
		Expect(store.ConvertNode(page, NodeKindSection)).To(Succeed())

		page.Kind = NodeKindPage
		swapTreeSeam(&treeMarkdownWriteToFile, func(*markdown.MarkdownFile) error {
			return errors.New("ensure index failed")
		})
		Expect(store.ConvertNode(page, NodeKindSection)).To(MatchError(ContainSubstring("could not write markdown file")))
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
