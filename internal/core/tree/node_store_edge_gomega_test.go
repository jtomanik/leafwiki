package tree

import (
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/gstruct"
	"github.com/perber/wiki/internal/core/markdown"
)

var _ = Describe("node store filesystem and validation failure behavior", Label("unit"), func() {
	var (
		store  *NodeStore
		base   string
		root   string
		parent *PageNode
	)

	BeforeEach(func() {
		base = tempTreeDir()
		root = filepath.Join(base, "root")
		store = NewNodeStoreWithOptions(NodeStoreOptions{DataDir: filepath.Join(base, "data"), RootDir: root})
		parent = edgeSectionNode(RootPageID, "root", "Root", nil)
	})

	It("filesystem helper errors preserve containment boundaries", func() {
		Expect(os.MkdirAll(root, 0o755)).To(Succeed())
		Expect(os.WriteFile(filepath.Join(root, "guide"), []byte("blocks directory creation"), 0o644)).To(Succeed())
		Expect(os.WriteFile(filepath.Join(root, "guide.md"), []byte("# Guide"), 0o644)).To(Succeed())
		Expect(EnsurePageIsFolder(root, "guide")).To(MatchError(ErrEnsureFolder))

		if runtime.GOOS != "windows" {
			locked := filepath.Join(root, "locked")
			Expect(os.MkdirAll(locked, 0o755)).To(Succeed())
			Expect(os.WriteFile(filepath.Join(locked, "index.md"), []byte("# Locked"), 0o644)).To(Succeed())
			Expect(os.Chmod(locked, 0)).To(Succeed())
			DeferCleanup(os.Chmod, locked, os.FileMode(0o755))
			Expect(FoldPageFolderIfEmpty(root, "locked")).To(MatchError(ErrReadDirectory))
		}

		if runtime.GOOS != "windows" {
			loop := filepath.Join(root, "loop")
			Expect(os.Symlink("loop", loop)).To(Succeed())
			fallback := time.Date(2026, time.June, 27, 10, 0, 0, 0, time.UTC)
			Expect(store.metadataFallbackTime(loop, fallback)).To(BeTemporally("==", fallback))
			Expect(store.requirePathInRoot("loopOp", filepath.Join(loop, "page.md"))).To(MatchError(ErrResolvePath))
		}
	})

	It("store validation guards reject invalid requests before disk writes", func() {
		page := edgePageNode("page-1", "page", "Page", parent)
		section := edgeSectionNode("section-1", "section", "Section", parent)

		Expect(store.CreatePage(nil, page)).To(matchInvalidOp("CreatePage"))
		Expect(store.CreatePage(parent, nil)).To(matchInvalidOp("CreatePage"))
		Expect(store.CreatePage(parent, edgePageNode(RootPageID, "page", "Page", parent))).To(matchInvalidOp("CreatePage"))
		Expect(store.CreatePage(parent, edgePageNode("bad", "", "Bad", parent))).To(matchInvalidOp("CreatePage"))
		Expect(store.CreatePage(page, edgePageNode("child", "child", "Child", page))).To(matchInvalidOp("CreatePage"))
		Expect(store.CreatePage(parent, section)).To(matchInvalidOp("CreatePage"))
		Expect(store.CreatePage(edgeSectionNode("loose-parent", "loose", "Loose", nil), page)).To(matchInvalidOp("dirPathForNode"))

		Expect(store.CreateSection(nil, section)).To(matchInvalidOp("CreateSection"))
		Expect(store.CreateSection(parent, nil)).To(matchInvalidOp("CreateSection"))
		Expect(store.CreateSection(parent, edgeSectionNode(RootPageID, "section", "Section", parent))).To(matchInvalidOp("CreateSection"))
		Expect(store.CreateSection(parent, edgeSectionNode("bad", "", "Bad", parent))).To(matchInvalidOp("CreateSection"))
		Expect(store.CreateSection(page, section)).To(matchInvalidOp("CreateSection"))
		Expect(store.CreateSection(parent, page)).To(matchInvalidOp("CreateSection"))
		Expect(store.CreateSection(edgeSectionNode("loose-parent", "loose", "Loose", nil), section)).To(matchInvalidOp("dirPathForNode"))

		Expect(store.UpsertContent(nil, "body")).To(matchInvalidOp("UpsertContent"))
		Expect(store.UpsertContentPreservingFrontmatter(nil, "body")).To(matchInvalidOp("UpsertContentPreservingFrontmatter"))
		Expect(store.UpsertContentReplacingMetadata(nil, "body")).To(matchInvalidOp("UpsertContentReplacingMetadata"))
		Expect(store.SyncMetadataIfExists(nil)).To(matchInvalidOp("SyncMetadataIfExists"))
		Expect(store.SyncFrontmatterIfExists(nil)).To(matchInvalidOp("SyncFrontmatterIfExists"))
		Expect(store.SaveChildOrder(nil)).To(matchInvalidOp("SaveChildOrder"))

		unknown := &PageNode{ID: RootPageID, Slug: "root", Title: "Root", Kind: NodeKind("unknown")}
		_, err := store.contentPathForNodeRead(unknown)
		Expect(err).To(matchInvalidOp("contentPathForNodeRead"))
		_, err = store.contentPathForNodeWrite(unknown)
		Expect(err).To(matchInvalidOp("contentPathForNodeWrite"))
	})

	It("store read, order, and metadata writes propagate filesystem failures", func() {
		section := edgeSectionNode("docs", "docs", "Docs", parent)

		_, err := store.ensureSectionIndex(edgeSectionNode("loose", "loose", "Loose", nil))
		Expect(err).To(matchInvalidOp("dirPathForNode"))

		blocked := filepath.Join(root, "blocked")
		Expect(os.MkdirAll(root, 0o755)).To(Succeed())
		Expect(os.WriteFile(blocked, []byte("not a directory"), 0o644)).To(Succeed())
		_, err = store.ensureSectionIndexAtPath(section, filepath.Join(blocked, "index.md"))
		Expect(err).To(MatchError(ErrResolvePath))

		if runtime.GOOS != "windows" {
			noWriteRoot := filepath.Join(base, "no-write-root")
			noWriteStore := NewNodeStoreWithOptions(NodeStoreOptions{DataDir: filepath.Join(base, "no-write-data"), RootDir: noWriteRoot})
			noWriteSection := edgeSectionNode("docs", "docs", "Docs", edgeSectionNode(RootPageID, "root", "Root", nil))
			Expect(os.MkdirAll(noWriteRoot, 0o755)).To(Succeed())
			Expect(os.Chmod(noWriteRoot, 0o555)).To(Succeed())
			DeferCleanup(os.Chmod, noWriteRoot, os.FileMode(0o755))
			_, err = noWriteStore.ensureSectionIndexAtPath(noWriteSection, filepath.Join(noWriteRoot, "docs", "index.md"))
			Expect(err).To(MatchError(ErrEnsureFolder))
		}

		invalidIndexDir := filepath.Join(root, "invalid")
		Expect(os.MkdirAll(invalidIndexDir, 0o755)).To(Succeed())
		invalidIndex := filepath.Join(invalidIndexDir, "index.md")
		Expect(os.WriteFile(invalidIndex, []byte("<!-- leafwiki\nversion: 1\n"), 0o644)).To(Succeed())
		_, err = store.ensureSectionIndexAtPath(section, invalidIndex)
		Expect(err).To(MatchError(ErrLoadMarkdownFile))

		Expect(store.readChildOrder(root)).To(Equal(&childOrderFile{}))
		Expect(os.WriteFile(filepath.Join(root, orderFilename), []byte("{invalid"), 0o644)).To(Succeed())
		_, err = store.readChildOrder(root)
		Expect(err).To(matchJSONSyntaxError())

		Expect(os.Remove(filepath.Join(root, orderFilename))).To(Succeed())
		Expect(os.Mkdir(filepath.Join(root, orderFilename), 0o755)).To(Succeed())
		parent.Children = []*PageNode{edgePageNode("a", "a", "A", parent)}
		Expect(store.SaveChildOrder(parent)).To(MatchError(ErrPersistChildOrder))

		orderParent := edgeSectionNode("ordered", "ordered", "Ordered", parent)
		orderParent.Children = []*PageNode{
			edgePageNode("a", "a", "A", orderParent),
			edgePageNode("b", "b", "B", orderParent),
			edgePageNode("c", "c", "C", orderParent),
		}
		orderDir := filepath.Join(root, "ordered")
		Expect(os.MkdirAll(orderDir, 0o755)).To(Succeed())
		Expect(os.WriteFile(filepath.Join(orderDir, orderFilename), []byte(`{"ordered_ids":["b","b","a"]}`), 0o644)).To(Succeed())
		store.applyChildOrder(orderParent, orderDir)
		Expect(orderParent.Children).To(HaveExactElements(
			matchOrderedChild("b", 0),
			matchOrderedChild("a", 1),
			matchOrderedChild("c", 2),
		))

		child := edgePageNode("child", "child", "Child", nil)
		store.assignParentToChildren(&PageNode{Children: []*PageNode{child}})
		Expect(child.Parent).NotTo(BeNil())
	})

	It("legacy and filesystem reconstruction report unreadable state", func() {
		Expect(os.MkdirAll(filepath.Join(base, "legacy"), 0o755)).To(Succeed())
		Expect(os.WriteFile(filepath.Join(base, "legacy", "tree.json"), []byte("{invalid"), 0o644)).To(Succeed())
		_, err := loadLegacyTreeSnapshot(filepath.Join(base, "legacy"), "tree.json", slog.Default())
		Expect(err).To(MatchError(ErrUnmarshalTreeData))

		dirSnapshot := filepath.Join(base, "dir-snapshot")
		Expect(os.MkdirAll(filepath.Join(dirSnapshot, "tree.json"), 0o755)).To(Succeed())
		_, err = loadLegacyTreeSnapshot(dirSnapshot, "tree.json", slog.Default())
		Expect(err).To(MatchError(ErrReadTreeFile))

		emptyKindSnapshot := filepath.Join(base, "empty-kind")
		Expect(os.MkdirAll(emptyKindSnapshot, 0o755)).To(Succeed())
		Expect(os.WriteFile(filepath.Join(emptyKindSnapshot, "tree.json"), []byte(`{"id":"root","slug":"root","title":"root"}`), 0o644)).To(Succeed())
		loaded, err := loadLegacyTreeSnapshot(emptyKindSnapshot, "tree.json", slog.Default())
		Expect(err).NotTo(HaveOccurred())
		Expect(loaded.Kind).To(Equal(NodeKindSection))

		fileRoot := filepath.Join(base, "file-root")
		Expect(os.WriteFile(fileRoot, []byte("not a directory"), 0o644)).To(Succeed())
		fileRootStore := NewNodeStoreWithOptions(NodeStoreOptions{DataDir: filepath.Join(base, "file-data"), RootDir: fileRoot})
		_, err = fileRootStore.ReconstructTreeFromFS()
		Expect(err).To(MatchError(ErrRootPathNotDirectory))

		invalidRootStore := NewNodeStoreWithOptions(NodeStoreOptions{DataDir: filepath.Join(base, "invalid-root-data"), RootDir: filepath.Join(base, "invalid-root")})
		Expect(os.MkdirAll(invalidRootStore.rootDir, 0o755)).To(Succeed())
		Expect(os.WriteFile(filepath.Join(invalidRootStore.rootDir, "index.md"), []byte("<!-- leafwiki\nversion: 1\n"), 0o644)).To(Succeed())
		_, err = invalidRootStore.ReconstructTreeFromFS()
		Expect(err).To(MatchError(markdown.ErrMetadataParse))

		duplicateStore := NewNodeStoreWithOptions(NodeStoreOptions{DataDir: filepath.Join(base, "duplicate-data"), RootDir: filepath.Join(base, "duplicate-root")})
		Expect(os.MkdirAll(duplicateStore.rootDir, 0o755)).To(Succeed())
		Expect(os.WriteFile(filepath.Join(duplicateStore.rootDir, "a.md"), []byte("---\nleafwiki_id: same\nleafwiki_title: A\n---\n# A\n"), 0o644)).To(Succeed())
		Expect(os.WriteFile(filepath.Join(duplicateStore.rootDir, "b.md"), []byte("---\nleafwiki_id: same\nleafwiki_title: B\n---\n# B\n"), 0o644)).To(Succeed())
		_, err = duplicateStore.ReconstructTreeFromFS()
		Expect(err).To(MatchError(ErrDuplicateLeafwikiID))
	})

	It("CRUD drift and conversion paths return domain errors", func() {
		Expect(os.MkdirAll(root, 0o755)).To(Succeed())

		page := edgePageNode("page", "page", "Page", parent)
		section := edgeSectionNode("section", "section", "Section", parent)
		unknown := edgePageNode("unknown", "unknown", "Unknown", parent)
		unknown.Kind = NodeKind("unknown")

		Expect(store.DeletePage(nil)).To(matchInvalidOp("DeletePage"))
		Expect(store.DeletePage(edgePageNode(RootPageID, "root", "Root", nil))).To(matchInvalidOp("DeletePage"))
		Expect(store.DeletePage(section)).To(matchInvalidOp("DeletePage"))
		Expect(store.DeletePage(page)).To(matchDrift("expected file missing"))
		Expect(os.Mkdir(filepath.Join(root, "page.md"), 0o755)).To(Succeed())
		Expect(store.DeletePage(page)).To(matchDrift("expected file but found folder"))

		Expect(store.DeleteSection(nil)).To(matchInvalidOp("DeleteSection"))
		Expect(store.DeleteSection(edgeSectionNode(RootPageID, "root", "Root", nil))).To(matchInvalidOp("DeleteSection"))
		Expect(store.DeleteSection(page)).To(matchInvalidOp("DeleteSection"))
		Expect(store.DeleteSection(section)).To(matchDrift("expected folder missing"))
		Expect(os.WriteFile(filepath.Join(root, "section"), []byte("not a folder"), 0o644)).To(Succeed())
		Expect(store.DeleteSection(section)).To(matchDrift("expected folder but found file"))

		Expect(store.RenameNode(nil, "new")).To(matchInvalidOp("RenameNode"))
		Expect(store.RenameNode(page, "")).To(matchInvalidOp("RenameNode"))
		Expect(store.RenameNode(page, "../escape")).To(matchInvalidOp("RenameNode"))
		Expect(store.RenameNode(page, "page")).To(Succeed())
		Expect(store.RenameNode(edgePageNode(RootPageID, "root", "Root", nil), "new-root")).To(matchInvalidOp("RenameNode"))
		Expect(store.RenameNode(unknown, "new")).To(matchInvalidOp("RenameNode"))

		Expect(store.MoveNode(nil, parent)).To(matchInvalidOp("MoveNode"))
		Expect(store.MoveNode(page, nil)).To(matchInvalidOp("MoveNode"))
		Expect(store.MoveNode(edgePageNode(RootPageID, "root", "Root", nil), parent)).To(matchInvalidOp("MoveNode"))
		Expect(store.MoveNode(page, page)).To(matchInvalidOp("MoveNode"))
		Expect(store.MoveNode(unknown, parent)).To(matchInvalidOp("MoveNode"))

		Expect(store.ConvertNode(nil, NodeKindPage)).To(matchInvalidOp("ConvertNode"))
		Expect(store.ConvertNode(page, NodeKind("unknown"))).To(matchInvalidOp("ConvertNode"))

		missingSection := edgeSectionNode("missing", "missing", "Missing", parent)
		Expect(store.ConvertNode(missingSection, NodeKindPage)).To(Succeed())

		notFolder := edgeSectionNode("not-folder", "not-folder", "Not Folder", parent)
		Expect(os.WriteFile(filepath.Join(root, "not-folder"), []byte("file"), 0o644)).To(Succeed())
		Expect(store.ConvertNode(notFolder, NodeKindPage)).To(matchDrift("expected folder but found file"))

		nonEmpty := edgeSectionNode("non-empty", "non-empty", "Non Empty", parent)
		Expect(os.MkdirAll(filepath.Join(root, "non-empty"), 0o755)).To(Succeed())
		Expect(os.WriteFile(filepath.Join(root, "non-empty", "child.md"), []byte("# Child"), 0o644)).To(Succeed())
		Expect(store.ConvertNode(nonEmpty, NodeKindPage)).To(matchConvertNotAllowed(NodeKindSection, NodeKindPage))
	})

	It("markdown parse failures surface through content upsert and read paths", func() {
		Expect(os.MkdirAll(root, 0o755)).To(Succeed())
		page := edgePageNode("page", "page", "Page", parent)
		badCanonical := "<!-- leafwiki\nversion: 1\n"

		Expect(store.UpsertContentPreservingFrontmatter(page, badCanonical)).To(MatchError(markdown.ErrMetadataParse))
		Expect(store.UpsertContentReplacingMetadata(page, badCanonical)).To(MatchError(markdown.ErrMetadataParse))

		Expect(os.WriteFile(filepath.Join(root, "page.md"), []byte(badCanonical), 0o644)).To(Succeed())
		Expect(store.UpsertContent(page, "new body")).To(MatchError(ErrLoadMarkdownFile))
		_, _, err := store.ReadPageAndRaw(page)
		Expect(err).To(MatchError(markdown.ErrMetadataParse))
		_, err = store.ReadPageContent(page)
		Expect(err).To(MatchError(markdown.ErrMetadataParse))
		Expect(store.SyncMetadataIfExists(page)).To(MatchError(ErrLoadMarkdownFile))
	})

	It("path resolution and workspace source helpers return normalized semantic paths", func() {
		Expect(os.MkdirAll(root, 0o755)).To(Succeed())
		docs := edgeSectionNode("docs", "docs", "Docs", parent)
		guide := edgePageNode("guide", "guide", "Guide", docs)

		_, err := store.dirPathForNode(nil)
		Expect(err).To(matchInvalidOp("dirPathForNode"))
		Expect(store.dirPathForNode(parent)).To(Equal(root))
		_, err = store.dirPathForNode(edgePageNode("loose", "loose", "Loose", nil))
		Expect(err).To(matchInvalidOp("dirPathForNode"))

		_, err = store.sectionDirPathForNode(nil, "sectionOp")
		Expect(err).To(matchInvalidOp("sectionOp"))
		_, err = store.sectionDirPathForNode(guide, "sectionOp")
		Expect(err).To(matchInvalidOp("sectionOp"))
		docs.WorkspaceSourcePath = "Imported/Docs"
		Expect(store.sectionDirPathForNode(docs, "sectionOp")).To(Equal(filepath.Join(root, "Imported", "Docs")))
		docs.WorkspaceSourcePath = "../outside"
		_, err = store.sectionDirPathForNode(docs, "sectionOp")
		Expect(err).To(matchInvalidOp("sectionOp"))
		docs.WorkspaceSourcePath = ""

		_, err = store.pageFilePathForNode(nil, "pageOp")
		Expect(err).To(matchInvalidOp("pageOp"))
		_, err = store.pageFilePathForNode(docs, "pageOp")
		Expect(err).To(matchInvalidOp("pageOp"))
		guide.WorkspaceSourcePath = "Imported/Guide.md"
		Expect(store.pageFilePathForNode(guide, "pageOp")).To(Equal(filepath.Join(root, "Imported", "Guide.md")))
		guide.WorkspaceSourcePath = "../outside.md"
		_, err = store.pageFilePathForNode(guide, "pageOp")
		Expect(err).To(matchInvalidOp("pageOp"))
		guide.WorkspaceSourcePath = ""

		Expect(store.workspaceSourceDirForSection(nil)).To(BeEmpty())
		Expect(store.workspaceSourceDirForSection(parent)).To(BeEmpty())
		docs.WorkspaceSourcePath = "Imported/Docs"
		Expect(store.workspaceSourceDirForSection(docs)).To(Equal("Imported/Docs"))
		docs.WorkspaceSourcePath = ""
		Expect(store.workspaceSourceDirForSection(docs)).To(Equal("docs"))

		Expect(store.workspaceSourcePathForNode(nil)).To(BeEmpty())
		Expect(store.workspaceSourcePathForNode(parent)).To(BeEmpty())
		guide.WorkspaceSourcePath = "Imported/Guide.md"
		Expect(store.workspaceSourcePathForNode(guide)).To(Equal("Imported/Guide.md"))
		guide.WorkspaceSourcePath = ""
		Expect(store.workspaceSourcePathForNode(guide)).To(Equal("docs/guide.md"))

		Expect(routePathWithLeafSlug(nil, "new")).To(BeEmpty())
		Expect(childRoutePathUnder("docs", nil)).To(Equal(RoutePath("docs")))
		store.setWorkspaceSourcePathForPhysicalPath(nil, filepath.Join(root, "x.md"), "x", NodeKindPage)
		store.setWorkspaceSourcePath(nil, "docs/guide", NodeKindPage, "docs/guide.md")
		Expect(guide.WorkspaceSourcePath).To(BeEmpty())
		store.setWorkspaceSourcePath(guide, "docs/guide", NodeKindPage, "Imported/Guide.md")
		Expect(guide.WorkspaceSourcePath).To(Equal(WorkspaceSourcePath("Imported/Guide.md")))
		store.updateWorkspaceSourcePathsForSubtree(nil, "old", "new", "old", "new")
		Expect(replaceWorkspaceSourcePrefix("docs/guide.md", "", "Imported")).To(Equal("Imported/docs/guide.md"))
		Expect(replaceWorkspaceSourcePrefix("docs/guide.md", "docs", "Imported")).To(Equal("Imported/guide.md"))
		Expect(replaceWorkspaceSourcePrefix("docs/guide.md", "other", "Imported")).To(Equal("docs/guide.md"))

		badAncestor := edgeSectionNode("bad", "", "Bad", parent)
		guide.Parent = badAncestor
		Expect(store.validateNodeRoute(guide)).To(matchInvalidOp("dirPathForNode"))
		guide.Parent = docs

		if runtime.GOOS != "windows" {
			loopRoot := filepath.Join(base, "loop-root")
			Expect(os.Symlink("loop-root", loopRoot)).To(Succeed())
			loopStore := NewNodeStoreWithOptions(NodeStoreOptions{DataDir: filepath.Join(base, "loop-data"), RootDir: loopRoot})
			Expect(loopStore.requirePathInRoot("loopOp", filepath.Join(loopRoot, "page.md"))).To(MatchError(ErrResolveRootDir))

			outside := filepath.Join(base, "outside")
			Expect(os.MkdirAll(outside, 0o755)).To(Succeed())
			Expect(os.Symlink(outside, filepath.Join(root, "linked-outside"))).To(Succeed())
			Expect(store.requirePathInRoot("linkOp", filepath.Join(root, "linked-outside", "page.md"))).To(matchInvalidOp("linkOp"))
		}
	})
})

func edgePageNode(id PageID, slug Slug, title string, parent *PageNode) *PageNode {
	GinkgoHelper()
	return &PageNode{ID: id, Slug: slug, Title: title, Kind: NodeKindPage, Parent: parent}
}

func edgeSectionNode(id PageID, slug Slug, title string, parent *PageNode) *PageNode {
	GinkgoHelper()
	return &PageNode{ID: id, Slug: slug, Title: title, Kind: NodeKindSection, Parent: parent, Children: []*PageNode{}}
}

func matchOrderedChild(id PageID, position int) OmegaMatcher {
	GinkgoHelper()
	return SatisfyAll(
		HaveField("ID", Equal(id)),
		HaveField("Position", Equal(position)),
	)
}

func matchInvalidOp(op string) OmegaMatcher {
	GinkgoHelper()
	return WithTransform(func(err error) string {
		var invalid *InvalidOpError
		if !errors.As(err, &invalid) {
			return ""
		}
		return invalid.Op
	}, Equal(op))
}

func matchDrift(reason string) OmegaMatcher {
	GinkgoHelper()
	return WithTransform(func(err error) string {
		var drift *DriftError
		if !errors.As(err, &drift) {
			return ""
		}
		return drift.Reason
	}, Equal(reason))
}

func matchConvertNotAllowed(from NodeKind, to NodeKind) OmegaMatcher {
	GinkgoHelper()
	return WithTransform(func(err error) ConvertNotAllowedError {
		var convert *ConvertNotAllowedError
		if !errors.As(err, &convert) {
			return ConvertNotAllowedError{}
		}
		return *convert
	}, gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
		"From": Equal(from),
		"To":   Equal(to),
	}))
}

func mustReadString(path string) string {
	GinkgoHelper()
	raw, err := os.ReadFile(path)
	Expect(err).NotTo(HaveOccurred())
	return string(raw)
}
