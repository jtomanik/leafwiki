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
		parent = edgeSectionNode(RootPageID, newFixtureSlug("root"), "Root", nil)
	})

	It("filesystem helper errors preserve containment boundaries", func() {
		Expect(os.MkdirAll(root, 0o755)).To(Succeed())
		Expect(os.WriteFile(filepath.Join(root, "guide"), []byte("blocks directory creation"), 0o644)).To(Succeed())
		Expect(os.WriteFile(filepath.Join(root, "guide.md"), []byte("# Guide"), 0o644)).To(Succeed())
		Expect(EnsurePageIsFolder(root, newFixtureRoutePath("guide"))).To(MatchError(ErrEnsureFolder))

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
		page := edgePageNode(newFixturePageID("page-1"), newFixtureSlug("page"), "Page", parent)
		section := edgeSectionNode(newFixturePageID("section-1"), newFixtureSlug("section"), "Section", parent)

		Expect(store.CreatePage(nil, page)).To(matchInvalidOp("CreatePage"))
		Expect(store.CreatePage(parent, nil)).To(matchInvalidOp("CreatePage"))
		Expect(store.CreatePage(parent, edgePageNode(RootPageID, newFixtureSlug("page"), "Page", parent))).To(matchInvalidOp("CreatePage"))
		Expect(store.CreatePage(parent, edgePageNode(newFixturePageID("bad"), newFixtureSlug(""), "Bad", parent))).To(matchInvalidOp("CreatePage"))
		Expect(store.CreatePage(page, edgePageNode(newFixturePageID("child"), newFixtureSlug("child"), "Child", page))).To(matchInvalidOp("CreatePage"))
		Expect(store.CreatePage(parent, section)).To(matchInvalidOp("CreatePage"))
		Expect(store.CreatePage(edgeSectionNode(newFixturePageID("loose-parent"), newFixtureSlug("loose"), "Loose", nil), page)).To(matchInvalidOp("dirPathForNode"))

		Expect(store.CreateSection(nil, section)).To(matchInvalidOp("CreateSection"))
		Expect(store.CreateSection(parent, nil)).To(matchInvalidOp("CreateSection"))
		Expect(store.CreateSection(parent, edgeSectionNode(RootPageID, newFixtureSlug("section"), "Section", parent))).To(matchInvalidOp("CreateSection"))
		Expect(store.CreateSection(parent, edgeSectionNode(newFixturePageID("bad"), newFixtureSlug(""), "Bad", parent))).To(matchInvalidOp("CreateSection"))
		Expect(store.CreateSection(page, section)).To(matchInvalidOp("CreateSection"))
		Expect(store.CreateSection(parent, page)).To(matchInvalidOp("CreateSection"))
		Expect(store.CreateSection(edgeSectionNode(newFixturePageID("loose-parent"), newFixtureSlug("loose"), "Loose", nil), section)).To(matchInvalidOp("dirPathForNode"))

		Expect(store.UpsertContent(nil, "body")).To(matchInvalidOp("UpsertContent"))
		Expect(store.UpsertContentPreservingFrontmatter(nil, "body")).To(matchInvalidOp("UpsertContentPreservingFrontmatter"))
		Expect(store.UpsertContentReplacingMetadata(nil, "body")).To(matchInvalidOp("UpsertContentReplacingMetadata"))
		Expect(store.SyncMetadataIfExists(nil)).To(matchInvalidOp("SyncMetadataIfExists"))
		Expect(store.SyncFrontmatterIfExists(nil)).To(matchInvalidOp("SyncFrontmatterIfExists"))
		Expect(store.SaveChildOrder(nil)).To(matchInvalidOp("SaveChildOrder"))

		unknown := &PageNode{ID: RootPageID, Slug: newFixtureSlug("root"), Title: "Root", Kind: newFixtureNodeKind("unknown")}
		_, err := store.contentPathForNodeRead(unknown)
		Expect(err).To(matchInvalidOp("contentPathForNodeRead"))
		_, err = store.contentPathForNodeWrite(unknown)
		Expect(err).To(matchInvalidOp("contentPathForNodeWrite"))
	})

	It("store read, order, and metadata writes propagate filesystem failures", func() {
		section := edgeSectionNode(newFixturePageID("docs"), newFixtureSlug("docs"), "Docs", parent)

		_, err := store.ensureSectionIndex(edgeSectionNode(newFixturePageID("loose"), newFixtureSlug("loose"), "Loose", nil))
		Expect(err).To(matchInvalidOp("dirPathForNode"))

		blocked := filepath.Join(root, "blocked")
		Expect(os.MkdirAll(root, 0o755)).To(Succeed())
		Expect(os.WriteFile(blocked, []byte("not a directory"), 0o644)).To(Succeed())
		_, err = store.ensureSectionIndexAtPath(section, filepath.Join(blocked, "index.md"))
		Expect(err).To(MatchError(ErrResolvePath))

		if runtime.GOOS != "windows" {
			noWriteRoot := filepath.Join(base, "no-write-root")
			noWriteStore := NewNodeStoreWithOptions(NodeStoreOptions{DataDir: filepath.Join(base, "no-write-data"), RootDir: noWriteRoot})
			noWriteSection := edgeSectionNode(newFixturePageID("docs"), newFixtureSlug("docs"), "Docs", edgeSectionNode(RootPageID, newFixtureSlug("root"), "Root", nil))
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
		parent.Children = []*PageNode{edgePageNode(newFixturePageID("a"), newFixtureSlug("a"), "A", parent)}
		Expect(store.SaveChildOrder(parent)).To(MatchError(ErrPersistChildOrder))

		orderParent := edgeSectionNode(newFixturePageID("ordered"), newFixtureSlug("ordered"), "Ordered", parent)
		orderParent.Children = []*PageNode{
			edgePageNode(newFixturePageID("a"), newFixtureSlug("a"), "A", orderParent),
			edgePageNode(newFixturePageID("b"), newFixtureSlug("b"), "B", orderParent),
			edgePageNode(newFixturePageID("c"), newFixtureSlug("c"), "C", orderParent),
		}
		orderDir := filepath.Join(root, "ordered")
		Expect(os.MkdirAll(orderDir, 0o755)).To(Succeed())
		Expect(os.WriteFile(filepath.Join(orderDir, orderFilename), []byte(`{"ordered_ids":["b","b","a"]}`), 0o644)).To(Succeed())
		store.applyChildOrder(orderParent, orderDir)
		Expect(orderParent.Children).To(HaveExactElements(
			matchOrderedChild(newFixturePageID("b"), 0),
			matchOrderedChild(newFixturePageID("a"), 1),
			matchOrderedChild(newFixturePageID("c"), 2),
		))

		child := edgePageNode(newFixturePageID("child"), newFixtureSlug("child"), "Child", nil)
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

		page := edgePageNode(newFixturePageID("page"), newFixtureSlug("page"), "Page", parent)
		section := edgeSectionNode(newFixturePageID("section"), newFixtureSlug("section"), "Section", parent)
		unknown := edgePageNode(newFixturePageID("unknown"), newFixtureSlug("unknown"), "Unknown", parent)
		unknown.Kind = newFixtureNodeKind("unknown")

		Expect(store.DeletePage(nil)).To(matchInvalidOp("DeletePage"))
		Expect(store.DeletePage(edgePageNode(RootPageID, newFixtureSlug("root"), "Root", nil))).To(matchInvalidOp("DeletePage"))
		Expect(store.DeletePage(section)).To(matchInvalidOp("DeletePage"))
		Expect(store.DeletePage(page)).To(matchDrift("expected file missing"))
		Expect(os.Mkdir(filepath.Join(root, "page.md"), 0o755)).To(Succeed())
		Expect(store.DeletePage(page)).To(matchDrift("expected file but found folder"))

		Expect(store.DeleteSection(nil)).To(matchInvalidOp("DeleteSection"))
		Expect(store.DeleteSection(edgeSectionNode(RootPageID, newFixtureSlug("root"), "Root", nil))).To(matchInvalidOp("DeleteSection"))
		Expect(store.DeleteSection(page)).To(matchInvalidOp("DeleteSection"))
		Expect(store.DeleteSection(section)).To(matchDrift("expected folder missing"))
		Expect(os.WriteFile(filepath.Join(root, "section"), []byte("not a folder"), 0o644)).To(Succeed())
		Expect(store.DeleteSection(section)).To(matchDrift("expected folder but found file"))

		Expect(store.RenameNode(nil, newFixtureSlug("new"))).To(matchInvalidOp("RenameNode"))
		Expect(store.RenameNode(page, newFixtureSlug(""))).To(matchInvalidOp("RenameNode"))
		Expect(store.RenameNode(page, newFixtureSlug("../escape"))).To(matchInvalidOp("RenameNode"))
		Expect(store.RenameNode(page, newFixtureSlug("page"))).To(Succeed())
		Expect(store.RenameNode(edgePageNode(RootPageID, newFixtureSlug("root"), "Root", nil), newFixtureSlug("new-root"))).To(matchInvalidOp("RenameNode"))
		Expect(store.RenameNode(unknown, newFixtureSlug("new"))).To(matchInvalidOp("RenameNode"))

		Expect(store.MoveNode(nil, parent)).To(matchInvalidOp("MoveNode"))
		Expect(store.MoveNode(page, nil)).To(matchInvalidOp("MoveNode"))
		Expect(store.MoveNode(edgePageNode(RootPageID, newFixtureSlug("root"), "Root", nil), parent)).To(matchInvalidOp("MoveNode"))
		Expect(store.MoveNode(page, page)).To(matchInvalidOp("MoveNode"))
		Expect(store.MoveNode(unknown, parent)).To(matchInvalidOp("MoveNode"))

		Expect(store.ConvertNode(nil, NodeKindPage)).To(matchInvalidOp("ConvertNode"))
		Expect(store.ConvertNode(page, newFixtureNodeKind("unknown"))).To(matchInvalidOp("ConvertNode"))

		missingSection := edgeSectionNode(newFixturePageID("missing"), newFixtureSlug("missing"), "Missing", parent)
		Expect(store.ConvertNode(missingSection, NodeKindPage)).To(Succeed())

		notFolder := edgeSectionNode(newFixturePageID("not-folder"), newFixtureSlug("not-folder"), "Not Folder", parent)
		Expect(os.WriteFile(filepath.Join(root, "not-folder"), []byte("file"), 0o644)).To(Succeed())
		Expect(store.ConvertNode(notFolder, NodeKindPage)).To(matchDrift("expected folder but found file"))

		nonEmpty := edgeSectionNode(newFixturePageID("non-empty"), newFixtureSlug("non-empty"), "Non Empty", parent)
		Expect(os.MkdirAll(filepath.Join(root, "non-empty"), 0o755)).To(Succeed())
		Expect(os.WriteFile(filepath.Join(root, "non-empty", "child.md"), []byte("# Child"), 0o644)).To(Succeed())
		Expect(store.ConvertNode(nonEmpty, NodeKindPage)).To(matchConvertNotAllowed(NodeKindSection, NodeKindPage))
	})

	It("markdown parse failures surface through content upsert and read paths", func() {
		Expect(os.MkdirAll(root, 0o755)).To(Succeed())
		page := edgePageNode(newFixturePageID("page"), newFixtureSlug("page"), "Page", parent)
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
		docs := edgeSectionNode(newFixturePageID("docs"), newFixtureSlug("docs"), "Docs", parent)
		guide := edgePageNode(newFixturePageID("guide"), newFixtureSlug("guide"), "Guide", docs)

		_, err := store.dirPathForNode(nil)
		Expect(err).To(matchInvalidOp("dirPathForNode"))
		Expect(store.dirPathForNode(parent)).To(Equal(root))
		_, err = store.dirPathForNode(edgePageNode(newFixturePageID("loose"), newFixtureSlug("loose"), "Loose", nil))
		Expect(err).To(matchInvalidOp("dirPathForNode"))

		_, err = store.sectionDirPathForNode(nil, "sectionOp")
		Expect(err).To(matchInvalidOp("sectionOp"))
		_, err = store.sectionDirPathForNode(guide, "sectionOp")
		Expect(err).To(matchInvalidOp("sectionOp"))
		docs.WorkspaceSourcePath = newFixtureWorkspaceSourcePath("Imported/Docs")
		Expect(store.sectionDirPathForNode(docs, "sectionOp")).To(Equal(filepath.Join(root, "Imported", "Docs")))
		docs.WorkspaceSourcePath = newFixtureWorkspaceSourcePath("../outside")
		_, err = store.sectionDirPathForNode(docs, "sectionOp")
		Expect(err).To(matchInvalidOp("sectionOp"))
		docs.WorkspaceSourcePath = newFixtureWorkspaceSourcePath("")

		_, err = store.pageFilePathForNode(nil, "pageOp")
		Expect(err).To(matchInvalidOp("pageOp"))
		_, err = store.pageFilePathForNode(docs, "pageOp")
		Expect(err).To(matchInvalidOp("pageOp"))
		guide.WorkspaceSourcePath = newFixtureWorkspaceSourcePath("Imported/Guide.md")
		Expect(store.pageFilePathForNode(guide, "pageOp")).To(Equal(filepath.Join(root, "Imported", "Guide.md")))
		guide.WorkspaceSourcePath = newFixtureWorkspaceSourcePath("../outside.md")
		_, err = store.pageFilePathForNode(guide, "pageOp")
		Expect(err).To(matchInvalidOp("pageOp"))
		guide.WorkspaceSourcePath = newFixtureWorkspaceSourcePath("")

		Expect(store.workspaceSourceDirForSection(nil)).To(BeEmpty())
		Expect(store.workspaceSourceDirForSection(parent)).To(BeEmpty())
		docs.WorkspaceSourcePath = newFixtureWorkspaceSourcePath("Imported/Docs")
		Expect(store.workspaceSourceDirForSection(docs)).To(Equal("Imported/Docs"))
		docs.WorkspaceSourcePath = newFixtureWorkspaceSourcePath("")
		Expect(store.workspaceSourceDirForSection(docs)).To(Equal("docs"))

		Expect(store.workspaceSourcePathForNode(nil)).To(BeEmpty())
		Expect(store.workspaceSourcePathForNode(parent)).To(BeEmpty())
		guide.WorkspaceSourcePath = newFixtureWorkspaceSourcePath("Imported/Guide.md")
		Expect(store.workspaceSourcePathForNode(guide)).To(Equal("Imported/Guide.md"))
		guide.WorkspaceSourcePath = newFixtureWorkspaceSourcePath("")
		Expect(store.workspaceSourcePathForNode(guide)).To(Equal("docs/guide.md"))

		Expect(routePathWithLeafSlug(nil, newFixtureSlug("new"))).To(BeEmpty())
		Expect(childRoutePathUnder(newFixtureRoutePath("docs"), nil)).To(Equal(newFixtureRoutePath("docs")))
		store.setWorkspaceSourcePathForPhysicalPath(nil, filepath.Join(root, "x.md"), newFixtureRoutePath("x"), NodeKindPage)
		store.setWorkspaceSourcePath(nil, newFixtureRoutePath("docs/guide"), NodeKindPage, "docs/guide.md")
		Expect(guide.WorkspaceSourcePath).To(BeEmpty())
		store.setWorkspaceSourcePath(guide, newFixtureRoutePath("docs/guide"), NodeKindPage, "Imported/Guide.md")
		Expect(guide.WorkspaceSourcePath).To(Equal(newFixtureWorkspaceSourcePath("Imported/Guide.md")))
		store.updateWorkspaceSourcePathsForSubtree(nil, newFixtureRoutePath("old"), newFixtureRoutePath("new"), "old", "new")
		Expect(replaceWorkspaceSourcePrefix("docs/guide.md", "", "Imported")).To(Equal("Imported/docs/guide.md"))
		Expect(replaceWorkspaceSourcePrefix("docs/guide.md", "docs", "Imported")).To(Equal("Imported/guide.md"))
		Expect(replaceWorkspaceSourcePrefix("docs/guide.md", "other", "Imported")).To(Equal("docs/guide.md"))

		badAncestor := edgeSectionNode(newFixturePageID("bad"), newFixtureSlug(""), "Bad", parent)
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
