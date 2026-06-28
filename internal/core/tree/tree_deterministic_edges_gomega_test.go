package tree

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("tree deterministic edge coverage", func() {
	It("records workspace route conflicts only for distinct sources of the same route and kind", func() {
		var nilTracker *workspaceRouteConflictTracker
		Expect(nilTracker.Record(WorkspaceMarkdownRoute{RoutePath: "docs", Kind: NodeKindPage})).To(BeNil())

		tracker := newWorkspaceRouteConflictTracker()
		Expect(tracker.Record(WorkspaceMarkdownRoute{RoutePath: "docs/guide", Kind: NodeKindPage, Skip: true})).To(BeNil())
		Expect(tracker.Record(WorkspaceMarkdownRoute{RoutePath: "docs/guide", Kind: NodeKindPage})).To(BeNil())
		Expect(tracker.seen).To(HaveKey("page:docs/guide"))

		Expect(tracker.Record(WorkspaceMarkdownRoute{SourcePath: "docs/guide", RoutePath: "docs/guide", Kind: NodeKindPage})).To(BeNil())

		sectionTracker := newWorkspaceRouteConflictTracker()
		Expect(sectionTracker.Record(WorkspaceMarkdownRoute{
			SourcePath:  "docs",
			RoutePath:   "docs",
			Kind:        NodeKindSection,
			ContentPath: "docs/README.md",
		})).To(BeNil())
		Expect(sectionTracker.Record(WorkspaceMarkdownRoute{
			SourcePath: "docs",
			RoutePath:  "docs",
			Kind:       NodeKindSection,
		})).To(BeNil())
	})

	It("maps workspace markdown edge paths without touching the tree store", func() {
		root := GinkgoT().TempDir()
		Expect(os.MkdirAll(filepath.Join(root, "docs"), 0o755)).To(Succeed())
		Expect(os.WriteFile(filepath.Join(root, "docs", "INDEX.MD"), []byte("# Docs\n"), 0o644)).To(Succeed())

		route, err := MapWorkspaceMarkdownRoute(root, " / ", true)
		Expect(err).NotTo(HaveOccurred())
		Expect(route.Kind).To(Equal(NodeKindSection))
		Expect(route.RoutePath).To(BeEmpty())

		route, err = MapWorkspaceMarkdownRoute(root, "docs/image.png", false)
		Expect(err).NotTo(HaveOccurred())
		Expect(route.Skip).To(BeTrue())
		Expect(route.SkipReason).To(Equal("non_markdown"))

		route, err = MapWorkspaceMarkdownRoute(root, "docs/README.md", false)
		Expect(err).NotTo(HaveOccurred())
		Expect(route.Kind).To(Equal(NodeKindPage))
		Expect(route.RoutePath).To(Equal(RoutePath("docs/README")))

		normalized, err := normalizeWorkspaceRoutePath(NewSlugService(), "docs//User Guides")
		Expect(err).NotTo(HaveOccurred())
		Expect(normalized).To(Equal("docs/user-guides"))

		_, err = normalizeWorkspaceRoutePath(NewSlugService(), "docs/!!!")
		Expect(err).To(MatchError(ContainSubstring("slug must not be empty")))
		Expect(workspaceDirHasIndexFile(filepath.Join(root, "missing"))).To(BeFalse())
		Expect(workspaceDirHasIndexFile(filepath.Join(root, "docs"))).To(BeTrue())
	})

	It("derives non-default workspace source paths for imported content", func() {
		Expect(nonDefaultWorkspaceSourcePath(WorkspaceMarkdownRoute{Skip: true, SourcePath: "docs/page.md"})).To(BeEmpty())
		Expect(nonDefaultWorkspaceSourcePath(WorkspaceMarkdownRoute{RoutePath: "docs/page", Kind: NodeKindPage, SourcePath: "Imported/Page.MD"})).To(Equal(WorkspaceSourcePath("Imported/Page.MD")))
		Expect(nonDefaultWorkspaceSourcePath(WorkspaceMarkdownRoute{RoutePath: "docs", Kind: NodeKindSection, SourcePath: "docs", ContentPath: "docs/README.md"})).To(BeEmpty())
		Expect(sectionSourceDir(WorkspaceMarkdownRoute{})).To(BeEmpty())
	})

	It("covers node-store path and uniqueness guards directly", func() {
		store := NewNodeStoreWithOptions(NodeStoreOptions{DataDir: GinkgoT().TempDir(), RootDir: filepath.Join(GinkgoT().TempDir(), "root")})
		rootDir := GinkgoT().TempDir()

		defaultIndex, exists, err := store.sectionIndexPathInDir(filepath.Join(rootDir, "missing"))
		Expect(err).NotTo(HaveOccurred())
		Expect(exists).To(BeFalse())
		Expect(defaultIndex).To(Equal(filepath.Join(rootDir, "missing", "index.md")))

		notDir := filepath.Join(rootDir, "not-dir")
		Expect(os.WriteFile(notDir, []byte("file"), 0o644)).To(Succeed())
		_, _, err = store.sectionIndexPathInDir(notDir)
		Expect(err).To(HaveOccurred())

		readmeDir := filepath.Join(rootDir, "readme")
		Expect(os.MkdirAll(readmeDir, 0o755)).To(Succeed())
		Expect(os.WriteFile(filepath.Join(readmeDir, "README.md"), []byte("# Readme\n"), 0o644)).To(Succeed())
		readmePath, exists, err := store.sectionIndexPathInDir(readmeDir)
		Expect(err).NotTo(HaveOccurred())
		Expect(exists).To(BeTrue())
		Expect(readmePath).To(Equal(filepath.Join(readmeDir, "README.md")))

		Expect(ensureUniqueReconstructedID(map[PageID]string{}, "", "docs/page.md")).To(MatchError(ContainSubstring("empty leafwiki_id")))
		seenIDs := map[PageID]string{"page-1": "docs/first.md"}
		Expect(ensureUniqueReconstructedID(seenIDs, "page-1", "docs/second.md")).To(MatchError(ContainSubstring("duplicate leafwiki_id")))

		Expect(ensureUniqueReconstructedSlug(map[string]string{}, "", NodeKindPage, "docs/page.md")).To(MatchError(ContainSubstring("empty slug")))
		seenSlugs := map[string]string{"page:guide": "docs/guide.md"}
		Expect(ensureUniqueReconstructedSlug(seenSlugs, "GUIDE", NodeKindPage, "docs/GUIDE.md")).To(MatchError(ContainSubstring("duplicate page slug")))
	})

	It("guards section index writes before touching disk", func() {
		store := NewNodeStoreWithOptions(NodeStoreOptions{DataDir: GinkgoT().TempDir(), RootDir: filepath.Join(GinkgoT().TempDir(), "root")})

		_, err := store.ensureSectionIndex(nil)
		Expect(err).To(MatchError(ContainSubstring("an entry is required")))
		_, err = store.ensureSectionIndex(&PageNode{Kind: NodeKindPage})
		Expect(err).To(MatchError(ContainSubstring("entry must be a section")))

		_, err = store.ensureSectionIndexAtPath(nil, filepath.Join(store.rootDir, "docs", "index.md"))
		Expect(err).To(MatchError(ContainSubstring("an entry is required")))
		_, err = store.ensureSectionIndexAtPath(&PageNode{Kind: NodeKindPage}, filepath.Join(store.rootDir, "docs", "index.md"))
		Expect(err).To(MatchError(ContainSubstring("entry must be a section")))
		_, err = store.ensureSectionIndexAtPath(&PageNode{Kind: NodeKindSection}, filepath.Join(filepath.Dir(store.rootDir), "outside.md"))
		Expect(err).To(HaveOccurred())
	})

	It("covers reachable route, slug, and version validation edge branches", func() {
		versionTime := time.Date(2026, time.June, 26, 9, 0, 0, 0, time.UTC)
		node := &PageNode{Metadata: PageMetadata{UpdatedAt: versionTime}}

		Expect(checkNodeVersion(&PageNode{}, "")).To(Succeed())
		Expect(checkNodeVersion(node, "")).To(MatchError(ErrVersionRequired))
		Expect(checkNodeVersion(node, NewPageVersionFromTime(versionTime.Add(time.Second)))).To(MatchError(ErrVersionConflict))

		_, err := ValidateRoutePath("")
		Expect(err).To(MatchError("missing path"))
		_, err = ValidateRoutePath(`docs\guide`)
		Expect(err).To(MatchError(ContainSubstring("invalid path")))
		_, err = ValidateRoutePath("docs//guide")
		Expect(err).To(MatchError(ContainSubstring("invalid path")))

		slugger := NewSlugService()
		Expect(slugger.IsValidSlug("")).To(MatchError("slug must not be empty"))
		normalized, err := slugger.NormalizePath("Docs//User Guides", false)
		Expect(err).NotTo(HaveOccurred())
		Expect(normalized).To(Equal("docs/user-guides"))
		_, err = slugger.NormalizePath("!!!", true)
		Expect(err).To(MatchError(ContainSubstring("not a valid slug")))
		normalized, err = slugger.NormalizePathToValidSlugs("docs//User Guides")
		Expect(err).NotTo(HaveOccurred())
		Expect(normalized).To(Equal("docs/user-guides"))

		_, err = MapWorkspaceMarkdownRoute(GinkgoT().TempDir(), "!!!", true)
		Expect(err).To(MatchError(ContainSubstring("slug must not be empty")))
		_, err = MapWorkspaceMarkdownRoute(GinkgoT().TempDir(), "!!!/page.md", false)
		Expect(err).To(MatchError(ContainSubstring("slug must not be empty")))
		_, err = MapWorkspaceMarkdownRoute(GinkgoT().TempDir(), "docs/!!!.md", false)
		Expect(err).To(MatchError(ContainSubstring("slug must not be empty")))
	})

	It("uses deterministic fallback metadata times and route kind lookup", func() {
		store := NewNodeStoreWithOptions(NodeStoreOptions{DataDir: GinkgoT().TempDir(), RootDir: filepath.Join(GinkgoT().TempDir(), "root")})
		fallback := time.Date(2026, time.June, 26, 10, 0, 0, 0, time.FixedZone("offset", 3600))
		Expect(store.metadataFallbackTime(filepath.Join(store.rootDir, "missing.md"), fallback)).To(Equal(fallback.UTC()))

		existing := filepath.Join(GinkgoT().TempDir(), "existing.md")
		Expect(os.WriteFile(existing, []byte("# Existing\n"), 0o644)).To(Succeed())
		mtime := time.Date(2026, time.June, 25, 12, 0, 0, 0, time.UTC)
		Expect(os.Chtimes(existing, mtime, mtime)).To(Succeed())
		Expect(store.metadataFallbackTime(existing, fallback)).To(Equal(mtime))

		svc, _ := newLoadedService(GinkgoT())
		docsID, err := svc.CreateNode(newFixtureUserID("editor"), nil, "Docs", "docs", ptrKind(NodeKindSection))
		Expect(err).NotTo(HaveOccurred())
		guideID, err := svc.CreateNode(newFixtureUserID("editor"), docsID, "Guide", "guide", ptrKind(NodeKindPage))
		Expect(err).NotTo(HaveOccurred())

		Expect(svc.findNodeByRoutePathAndKindLocked("", NodeKindPage)).To(BeNil())
		Expect(svc.findNodeByRoutePathAndKindLocked("docs/missing", NodeKindPage)).To(BeNil())
		Expect(svc.findNodeByRoutePathAndKindLocked("docs", NodeKindPage)).To(BeNil())
		found := svc.findNodeByRoutePathAndKindLocked("docs/guide", NodeKindPage)
		Expect(found).NotTo(BeNil())
		Expect(found.ID).To(Equal(*guideID))
	})

	It("guards migration store adapter wrappers before touching the store", func() {
		adapter := &migrationStoreAdapter{}

		_, err := adapter.ResolveNode(nil)
		Expect(err).To(MatchError("invalid migration node"))
		_, err = adapter.ContentPathForRead(nil)
		Expect(err).To(MatchError("invalid migration node"))
		_, err = adapter.ContentPathForWrite(nil)
		Expect(err).To(MatchError("invalid migration node"))
		_, err = adapter.EnsureSectionIndex(nil)
		Expect(err).To(MatchError("invalid migration node"))
		Expect(adapter.SaveChildOrder(nil)).To(MatchError("invalid migration node"))
		_, err = adapter.ReadPageRaw(nil)
		Expect(err).To(MatchError("invalid migration node"))
	})

	It("persists legacy migration snapshots only for loaded trees", func() {
		svc := NewTreeServiceWithOptions(TreeOptions{DataDir: GinkgoT().TempDir(), RootDir: GinkgoT().TempDir()})

		Expect(svc.persistLegacyTreeSnapshotLocked()).To(MatchError(ContainSubstring("legacy migration snapshot requires loaded tree")))

		cyclic := &PageNode{ID: "cycle", Slug: "cycle", Title: "Cycle"}
		cyclic.Children = []*PageNode{cyclic}
		svc.tree = cyclic
		Expect(svc.persistLegacyTreeSnapshotLocked()).To(MatchError(ContainSubstring("marshal legacy migration snapshot")))

		svc.tree = &PageNode{ID: RootPageID, Slug: "root", Title: "Root"}
		Expect(svc.persistLegacyTreeSnapshotLocked()).To(Succeed())
		Expect(filepath.Join(svc.dataDir, legacyTreeFilename)).To(BeAnExistingFile())

		deps := svc.migrationDependencies()
		Expect(deps.Root).NotTo(BeNil())
		Expect(deps.CurrentSchemaVersion).To(Equal(CurrentSchemaVersion))
		Expect(deps.IsMissingContentErr(os.ErrNotExist)).To(BeTrue())
		Expect(deps.IsMissingContentErr(ErrFileNotFound)).To(BeTrue())
		Expect(deps.IsMissingContentErr(errors.New("other"))).To(BeFalse())
		Expect(deps.SaveSchema(CurrentSchemaVersion)).To(Succeed())
	})

	It("loads and saves schema files across first-run, corrupt, and write-error cases", func() {
		tmp := GinkgoT().TempDir()

		schema, err := loadSchema(tmp)
		Expect(err).NotTo(HaveOccurred())
		Expect(schema.Version).To(Equal(0))

		Expect(saveSchema(tmp, CurrentSchemaVersion)).To(Succeed())
		raw, err := os.ReadFile(filepath.Join(tmp, "schema.json"))
		Expect(err).NotTo(HaveOccurred())
		Expect(string(raw)).To(ContainSubstring(`"version": 5`))

		schema, err = loadSchema(tmp)
		Expect(err).NotTo(HaveOccurred())
		Expect(schema.Version).To(Equal(CurrentSchemaVersion))

		corruptDir := GinkgoT().TempDir()
		Expect(os.WriteFile(filepath.Join(corruptDir, "schema.json"), []byte("{invalid"), 0o644)).To(Succeed())
		_, err = loadSchema(corruptDir)
		Expect(err).To(HaveOccurred())

		if runtime.GOOS != "windows" {
			loopDir := GinkgoT().TempDir()
			Expect(os.Symlink("schema.json", filepath.Join(loopDir, "schema.json"))).To(Succeed())
			_, err = loadSchema(loopDir)
			Expect(err).To(HaveOccurred())
		}

		_, err = loadSchema(filepath.Join(tmp, "missing-parent"))
		Expect(err).NotTo(HaveOccurred())

		err = saveSchema(filepath.Join(tmp, "missing-parent"), CurrentSchemaVersion)
		Expect(err).To(HaveOccurred())
	})

	It("converts flat page files to section folders and folds empty folders back", func() {
		root := GinkgoT().TempDir()
		Expect(os.WriteFile(filepath.Join(root, "guide.md"), []byte("# Guide"), 0o644)).To(Succeed())

		Expect(EnsurePageIsFolder(root, "guide")).To(Succeed())
		Expect(os.ReadFile(filepath.Join(root, "guide", "index.md"))).To(Equal([]byte("# Guide")))
		_, err := os.Stat(filepath.Join(root, "guide.md"))
		Expect(os.IsNotExist(err)).To(BeTrue())

		Expect(EnsurePageIsFolder(root, "guide")).To(Succeed())
		Expect(FoldPageFolderIfEmpty(root, "missing")).To(Succeed())

		Expect(os.WriteFile(filepath.Join(root, "guide", "extra.md"), []byte("# Extra"), 0o644)).To(Succeed())
		Expect(FoldPageFolderIfEmpty(root, "guide")).To(Succeed())
		_, err = os.Stat(filepath.Join(root, "guide"))
		Expect(err).NotTo(HaveOccurred())

		Expect(os.Remove(filepath.Join(root, "guide", "extra.md"))).To(Succeed())
		Expect(FoldPageFolderIfEmpty(root, "guide")).To(Succeed())
		Expect(os.ReadFile(filepath.Join(root, "guide.md"))).To(Equal([]byte("# Guide")))
		_, err = os.Stat(filepath.Join(root, "guide"))
		Expect(os.IsNotExist(err)).To(BeTrue())
	})
})
