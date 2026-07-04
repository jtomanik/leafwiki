package tree

import (
	"errors"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

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
