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
		parent = edgeSectionNode(RootPageID, newFixtureSlug("root"), "Root", nil)
	})

	It("migration adapters, route mapping, and content paths preserve legacy compatibility", func() {
		page := edgePageNode(newFixturePageID("page"), newFixtureSlug("page"), "Page", parent)
		section := edgeSectionNode(newFixturePageID("section"), newFixtureSlug("section"), "Section", parent)

		adapter := &migrationStoreAdapter{store: store}
		_, err := adapter.ResolveNode(&migrationNodeAdapter{node: page})
		var notFound *NotFoundError
		Expect(err).To(matchErrorAs(&notFound), "expected NotFoundError, got %T: %v", err, err)

		svc := NewTreeService(tempTreeDir())
		svc.tree = edgeSectionNode(RootPageID, newFixtureSlug("root"), "Root", nil)
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
		looseParent := edgeSectionNode(newFixturePageID("loose"), newFixtureSlug("loose"), "Loose", nil)
		Expect(store.SaveChildOrder(looseParent)).To(matchInvalidOp("dirPathForNode"))

		parent.Children = []*PageNode{nil, page}
		swapTreeSeam(&treeWriteFileAtomic, func(string, []byte, os.FileMode) error { return nil })
		Expect(store.SaveChildOrder(parent)).To(Succeed())

		section.WorkspaceSourcePath = newFixtureWorkspaceSourcePath("Imported/Section")
		swapTreeSeam(&treeOSReadDir, func(string) ([]os.DirEntry, error) {
			return []os.DirEntry{fakeTreeDirEntry{name: "index.md"}}, nil
		})
		sourcePath, exists, err := store.workspaceContentPathForNode(section, "workspaceContent")
		sectionSourceLookup := workspaceContentPathLookup{Path: sourcePath, Exists: exists, Err: err}
		Expect(sectionSourceLookup).To(matchExistingWorkspaceContentPath(
			ContainSubstring("index.md"),
			Succeed(),
		))

		page.WorkspaceSourcePath = newFixtureWorkspaceSourcePath("../outside.md")
		sourcePath, exists, err = store.workspaceContentPathForNode(page, "workspaceContent")
		outsideSourceLookup := workspaceContentPathLookup{Path: sourcePath, Exists: exists, Err: err}
		Expect(outsideSourceLookup).To(matchMissingWorkspaceContentPath(
			BeEmpty(),
			matchInvalidOp("workspaceContent"),
		))

		_, err = store.contentPathForNodeRead(edgePageNode(newFixturePageID("loose-page"), newFixtureSlug("loose-page"), "Loose Page", nil))
		Expect(err).To(matchInvalidOp("dirPathForNode"))
		_, err = store.contentPathForNodeWrite(edgePageNode(newFixturePageID("loose-page"), newFixtureSlug("loose-page"), "Loose Page", nil))
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
			Expect(observeDirectoryFlag(isDir)).To(Equal(fileInfoDirectory))
			return WorkspaceMarkdownRoute{Kind: NodeKindSection, RoutePath: newFixtureRoutePath("same"), SourcePath: WorkspaceSourcePathFromString(relPath)}, nil
		})
		Expect(store.reconstructTreeRecursive(root, parent, now, map[PageID]string{RootPageID: root})).To(MatchError(ErrDuplicateReconstructedSlug))

		ids := []string{"same-id", "same-id"}
		swapTreeSeam(&treeGenerateUniqueID, func() (string, error) {
			id := ids[0]
			ids = ids[1:]
			return id, nil
		})
		swapTreeSeam(&treeMapWorkspaceMarkdownRoute, func(_ string, relPath string, isDir bool) (WorkspaceMarkdownRoute, error) {
			Expect(observeDirectoryFlag(isDir)).To(Equal(fileInfoDirectory))
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
			"notes.txt": {Kind: NodeKindPage, RoutePath: newFixtureRoutePath("notes")},
			"README.md": {Kind: NodeKindPage, RoutePath: newFixtureRoutePath("readme")},
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
			return WorkspaceMarkdownRoute{Kind: NodeKindPage, RoutePath: newFixtureRoutePath("page"), SourcePath: WorkspaceSourcePathFromString(relPath)}, nil
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
})
