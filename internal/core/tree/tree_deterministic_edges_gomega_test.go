package tree

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/gstruct"
)

var _ = Describe("deterministic tree edge behavior", Label("unit"), func() {
	It("records workspace route conflicts only for distinct sources of the same route and kind", func() {
		var nilTracker *workspaceRouteConflictTracker
		Expect(nilTracker.Record(WorkspaceMarkdownRoute{RoutePath: newFixtureRoutePath("docs"), Kind: NodeKindPage})).To(BeNil())

		tracker := newWorkspaceRouteConflictTracker()
		Expect(tracker.Record(WorkspaceMarkdownRoute{RoutePath: newFixtureRoutePath("docs/guide"), Kind: NodeKindPage, Skip: true})).To(BeNil())
		Expect(tracker.Record(WorkspaceMarkdownRoute{RoutePath: newFixtureRoutePath("docs/guide"), Kind: NodeKindPage})).To(BeNil())
		Expect(tracker.seen).To(HaveKey(RouteLowerKey{Kind: NodeKindPage, Path: newFixtureRoutePath("docs/guide")}))

		Expect(tracker.Record(WorkspaceMarkdownRoute{SourcePath: newFixtureWorkspaceSourcePath("docs/guide"), RoutePath: newFixtureRoutePath("docs/guide"), Kind: NodeKindPage})).To(BeNil())

		sectionTracker := newWorkspaceRouteConflictTracker()
		Expect(sectionTracker.Record(WorkspaceMarkdownRoute{
			SourcePath:  newFixtureWorkspaceSourcePath("docs"),
			RoutePath:   newFixtureRoutePath("docs"),
			Kind:        NodeKindSection,
			ContentPath: newFixtureMarkdownPath("docs/README.md"),
		})).To(BeNil())
		Expect(sectionTracker.Record(WorkspaceMarkdownRoute{
			SourcePath: newFixtureWorkspaceSourcePath("docs"),
			RoutePath:  newFixtureRoutePath("docs"),
			Kind:       NodeKindSection,
		})).To(BeNil())
	})

	It("maps workspace markdown edge paths without touching the tree store", func() {
		root := tempTreeDir()
		Expect(os.MkdirAll(filepath.Join(root, "docs"), 0o755)).To(Succeed())
		Expect(os.WriteFile(filepath.Join(root, "docs", "INDEX.MD"), []byte("# Docs\n"), 0o644)).To(Succeed())

		route, err := MapWorkspaceMarkdownRoute(root, " / ", true)
		Expect(err).NotTo(HaveOccurred())
		Expect(route).To(gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
			"Kind":      Equal(NodeKindSection),
			"RoutePath": BeEmpty(),
		}))

		route, err = MapWorkspaceMarkdownRoute(root, "docs/image.png", false)
		Expect(err).NotTo(HaveOccurred())
		Expect(route).To(matchSkippedWorkspaceMarkdownRoute("non_markdown"))

		route, err = MapWorkspaceMarkdownRoute(root, "docs/README.md", false)
		Expect(err).NotTo(HaveOccurred())
		Expect(route).To(gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
			"Kind":      Equal(NodeKindPage),
			"RoutePath": Equal(newFixtureRoutePath("docs/README")),
		}))

		normalized, err := normalizeWorkspaceRoutePath(NewSlugService(), "docs//User Guides")
		Expect(err).NotTo(HaveOccurred())
		Expect(normalized).To(Equal("docs/user-guides"))

		_, err = normalizeWorkspaceRoutePath(NewSlugService(), "docs/!!!")
		Expect(err).To(MatchError(ErrSlugEmpty))
		Expect(workspaceDirHasIndexFile(filepath.Join(root, "missing"))).To(BeFalse())
		Expect(workspaceDirHasIndexFile(filepath.Join(root, "docs"))).To(BeTrue())
	})

	It("derives non-default workspace source paths for imported content", func() {
		Expect(nonDefaultWorkspaceSourcePath(WorkspaceMarkdownRoute{Skip: true, SourcePath: newFixtureWorkspaceSourcePath("docs/page.md")})).To(BeEmpty())
		Expect(nonDefaultWorkspaceSourcePath(WorkspaceMarkdownRoute{RoutePath: newFixtureRoutePath("docs/page"), Kind: NodeKindPage, SourcePath: newFixtureWorkspaceSourcePath("Imported/Page.MD")})).To(Equal(newFixtureWorkspaceSourcePath("Imported/Page.MD")))
		Expect(nonDefaultWorkspaceSourcePath(WorkspaceMarkdownRoute{RoutePath: newFixtureRoutePath("docs"), Kind: NodeKindSection, SourcePath: newFixtureWorkspaceSourcePath("docs"), ContentPath: newFixtureMarkdownPath("docs/README.md")})).To(BeEmpty())
		Expect(sectionSourceDir(WorkspaceMarkdownRoute{})).To(BeEmpty())
	})

	It("node-store path and uniqueness guards reject invalid inputs", func() {
		store := NewNodeStoreWithOptions(NodeStoreOptions{DataDir: tempTreeDir(), RootDir: filepath.Join(tempTreeDir(), "root")})
		rootDir := tempTreeDir()

		defaultIndex, exists, err := store.sectionIndexPathInDir(filepath.Join(rootDir, "missing"))
		Expect(err).NotTo(HaveOccurred())
		defaultIndexLookup := sectionIndexPathLookup{Path: defaultIndex, Exists: exists}
		Expect(defaultIndexLookup).To(matchMissingSectionIndexPath(filepath.Join(rootDir, "missing", "index.md")))

		notDir := filepath.Join(rootDir, "not-dir")
		Expect(os.WriteFile(notDir, []byte("file"), 0o644)).To(Succeed())
		notDirIndex, exists, err := store.sectionIndexPathInDir(notDir)
		notDirLookup := sectionIndexPathLookup{Path: notDirIndex, Exists: exists}
		Expect(notDirLookup).To(matchMissingSectionIndexPath(filepath.Join(notDir, "index.md")))
		Expect(err).To(matchPathError())

		readmeDir := filepath.Join(rootDir, "readme")
		Expect(os.MkdirAll(readmeDir, 0o755)).To(Succeed())
		Expect(os.WriteFile(filepath.Join(readmeDir, "README.md"), []byte("# Readme\n"), 0o644)).To(Succeed())
		readmePath, exists, err := store.sectionIndexPathInDir(readmeDir)
		Expect(err).NotTo(HaveOccurred())
		readmeLookup := sectionIndexPathLookup{Path: readmePath, Exists: exists}
		Expect(readmeLookup).To(matchExistingSectionIndexPath(filepath.Join(readmeDir, "README.md")))

		Expect(ensureUniqueReconstructedID(map[PageID]string{}, newFixturePageID(""), "docs/page.md")).To(MatchError(ErrEmptyLeafwikiID))
		seenIDs := map[PageID]string{"page-1": "docs/first.md"}
		Expect(ensureUniqueReconstructedID(seenIDs, newFixturePageID("page-1"), "docs/second.md")).To(MatchError(ErrDuplicateLeafwikiID))

		Expect(ensureUniqueReconstructedSlug(map[reconstructedSlugKey]string{}, newFixtureSlug(""), NodeKindPage, "docs/page.md")).To(MatchError(ErrSlugEmpty))
		seenSlugs := map[reconstructedSlugKey]string{{kind: NodeKindPage, slug: SlugFromString("guide")}: "docs/guide.md"}
		Expect(ensureUniqueReconstructedSlug(seenSlugs, newFixtureSlug("GUIDE"), NodeKindPage, "docs/GUIDE.md")).To(MatchError(ErrDuplicateReconstructedSlug))
	})

	It("guards section index writes before touching disk", func() {
		store := NewNodeStoreWithOptions(NodeStoreOptions{DataDir: tempTreeDir(), RootDir: filepath.Join(tempTreeDir(), "root")})

		_, err := store.ensureSectionIndex(nil)
		Expect(err).To(matchInvalidOp("ensureSectionIndex"))
		_, err = store.ensureSectionIndex(&PageNode{Kind: NodeKindPage})
		Expect(err).To(matchInvalidOp("ensureSectionIndex"))

		_, err = store.ensureSectionIndexAtPath(nil, filepath.Join(store.rootDir, "docs", "index.md"))
		Expect(err).To(matchInvalidOp("ensureSectionIndexAtPath"))
		_, err = store.ensureSectionIndexAtPath(&PageNode{Kind: NodeKindPage}, filepath.Join(store.rootDir, "docs", "index.md"))
		Expect(err).To(matchInvalidOp("ensureSectionIndexAtPath"))
		_, err = store.ensureSectionIndexAtPath(&PageNode{Kind: NodeKindSection}, filepath.Join(filepath.Dir(store.rootDir), "outside.md"))
		Expect(err).To(matchInvalidOp("ensureSectionIndexAtPath"))
	})

	It("route, slug, and version validation reject invalid semantic values", func() {
		versionTime := time.Date(2026, time.June, 26, 9, 0, 0, 0, time.UTC)
		node := &PageNode{Metadata: PageMetadata{UpdatedAt: versionTime}}

		Expect(checkNodeVersion(&PageNode{}, newFixturePageVersion(""))).To(Succeed())
		Expect(checkNodeVersion(node, newFixturePageVersion(""))).To(MatchError(ErrVersionRequired))
		Expect(checkNodeVersion(node, NewPageVersionFromTime(versionTime.Add(time.Second)))).To(MatchError(ErrVersionConflict))

		_, err := ValidateRoutePath("")
		Expect(err).To(MatchError(ErrMissingRoutePath))
		_, err = ValidateRoutePath(`docs\guide`)
		Expect(err).To(MatchError(ErrInvalidRoutePath))
		_, err = ValidateRoutePath("docs//guide")
		Expect(err).To(MatchError(ErrInvalidRoutePath))

		slugger := NewSlugService()
		Expect(slugger.IsValidSlug("")).To(MatchError(ErrSlugEmpty))
		normalized, err := slugger.NormalizePath("Docs//User Guides", false)
		Expect(err).NotTo(HaveOccurred())
		Expect(normalized).To(Equal("docs/user-guides"))
		_, err = slugger.NormalizePath("!!!", true)
		Expect(err).To(MatchError(ErrSlugEmpty))
		normalized, err = slugger.NormalizePathToValidSlugs("docs//User Guides")
		Expect(err).NotTo(HaveOccurred())
		Expect(normalized).To(Equal("docs/user-guides"))

		_, err = MapWorkspaceMarkdownRoute(tempTreeDir(), "!!!", true)
		Expect(err).To(MatchError(ErrSlugEmpty))
		_, err = MapWorkspaceMarkdownRoute(tempTreeDir(), "!!!/page.md", false)
		Expect(err).To(MatchError(ErrSlugEmpty))
		_, err = MapWorkspaceMarkdownRoute(tempTreeDir(), "docs/!!!.md", false)
		Expect(err).To(MatchError(ErrSlugEmpty))
	})

	It("uses deterministic fallback metadata times and route kind lookup", func() {
		store := NewNodeStoreWithOptions(NodeStoreOptions{DataDir: tempTreeDir(), RootDir: filepath.Join(tempTreeDir(), "root")})
		fallback := time.Date(2026, time.June, 26, 10, 0, 0, 0, time.FixedZone("offset", 3600))
		Expect(store.metadataFallbackTime(filepath.Join(store.rootDir, "missing.md"), fallback)).To(BeTemporally("==", fallback.UTC()))

		existing := filepath.Join(tempTreeDir(), "existing.md")
		Expect(os.WriteFile(existing, []byte("# Existing\n"), 0o644)).To(Succeed())
		mtime := time.Date(2026, time.June, 25, 12, 0, 0, 0, time.UTC)
		Expect(os.Chtimes(existing, mtime, mtime)).To(Succeed())
		Expect(store.metadataFallbackTime(existing, fallback)).To(BeTemporally("==", mtime))

		svc, _ := newLoadedService()
		docsID, err := svc.CreateNode(newFixtureUserID("editor"), nil, "Docs", newFixtureSlug("docs"), ptrKind(NodeKindSection))
		Expect(err).NotTo(HaveOccurred())
		guideID, err := svc.CreateNode(newFixtureUserID("editor"), docsID, "Guide", newFixtureSlug("guide"), ptrKind(NodeKindPage))
		Expect(err).NotTo(HaveOccurred())

		Expect(svc.findNodeByRoutePathAndKindLocked(newFixtureRoutePath(""), NodeKindPage)).To(BeNil())
		Expect(svc.findNodeByRoutePathAndKindLocked(newFixtureRoutePath("docs/missing"), NodeKindPage)).To(BeNil())
		Expect(svc.findNodeByRoutePathAndKindLocked(newFixtureRoutePath("docs"), NodeKindPage)).To(BeNil())
		found := svc.findNodeByRoutePathAndKindLocked(newFixtureRoutePath("docs/guide"), NodeKindPage)
		Expect(found).NotTo(BeNil())
		Expect(found.ID).To(Equal(*guideID))
	})

	It("guards migration store adapter wrappers before touching the store", func() {
		adapter := &migrationStoreAdapter{}

		_, err := adapter.ResolveNode(nil)
		Expect(err).To(MatchError(ErrInvalidMigrationNode))
		_, err = adapter.ContentPathForRead(nil)
		Expect(err).To(MatchError(ErrInvalidMigrationNode))
		_, err = adapter.ContentPathForWrite(nil)
		Expect(err).To(MatchError(ErrInvalidMigrationNode))
		_, err = adapter.EnsureSectionIndex(nil)
		Expect(err).To(MatchError(ErrInvalidMigrationNode))
		Expect(adapter.SaveChildOrder(nil)).To(MatchError(ErrInvalidMigrationNode))
		_, err = adapter.ReadPageRaw(nil)
		Expect(err).To(MatchError(ErrInvalidMigrationNode))
	})

	It("persists legacy migration snapshots only for loaded trees", func() {
		svc := NewTreeServiceWithOptions(TreeOptions{DataDir: tempTreeDir(), RootDir: tempTreeDir()})

		Expect(svc.persistLegacyTreeSnapshotLocked()).To(MatchError(ErrLegacySnapshotTreeRequired))

		cyclic := &PageNode{ID: newFixturePageID("cycle"), Slug: newFixtureSlug("cycle"), Title: "Cycle"}
		cyclic.Children = []*PageNode{cyclic}
		svc.tree = cyclic
		Expect(svc.persistLegacyTreeSnapshotLocked()).To(MatchError(ErrMarshalLegacyTreeSnapshot))

		svc.tree = &PageNode{ID: RootPageID, Slug: newFixtureSlug("root"), Title: "Root"}
		Expect(svc.persistLegacyTreeSnapshotLocked()).To(Succeed())
		Expect(filepath.Join(svc.dataDir, legacyTreeFilename)).To(BeAnExistingFile())

		deps := svc.migrationDependencies()
		Expect(deps).To(gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
			"Root":                 Not(BeNil()),
			"CurrentSchemaVersion": Equal(CurrentSchemaVersion),
		}))
		Expect(os.ErrNotExist).To(matchMissingContentErrorState(deps.IsMissingContentErr, missingContentErrorRecognized))
		Expect(ErrFileNotFound).To(matchMissingContentErrorState(deps.IsMissingContentErr, missingContentErrorRecognized))
		Expect(errors.New("other")).To(matchMissingContentErrorState(deps.IsMissingContentErr, missingContentErrorUnrecognized))
		Expect(deps.SaveSchema(CurrentSchemaVersion)).To(Succeed())
	})

	It("loads and saves schema files across first-run, corrupt, and write-error cases", func() {
		tmp := tempTreeDir()

		schema, err := loadSchema(tmp)
		Expect(err).NotTo(HaveOccurred())
		Expect(schema.Version).To(BeZero())

		Expect(saveSchema(tmp, CurrentSchemaVersion)).To(Succeed())
		raw, err := os.ReadFile(filepath.Join(tmp, "schema.json"))
		Expect(err).NotTo(HaveOccurred())
		Expect(string(raw)).To(ContainSubstring(`"version": 5`))

		schema, err = loadSchema(tmp)
		Expect(err).NotTo(HaveOccurred())
		Expect(schema.Version).To(Equal(CurrentSchemaVersion))

		corruptDir := tempTreeDir()
		Expect(os.WriteFile(filepath.Join(corruptDir, "schema.json"), []byte("{invalid"), 0o644)).To(Succeed())
		_, err = loadSchema(corruptDir)
		Expect(err).To(matchJSONSyntaxError())

		if runtime.GOOS != "windows" {
			loopDir := tempTreeDir()
			Expect(os.Symlink("schema.json", filepath.Join(loopDir, "schema.json"))).To(Succeed())
			_, err = loadSchema(loopDir)
			Expect(err).To(matchSymlinkLoopError())
		}

		_, err = loadSchema(filepath.Join(tmp, "missing-parent"))
		Expect(err).NotTo(HaveOccurred())

		err = saveSchema(filepath.Join(tmp, "missing-parent"), CurrentSchemaVersion)
		Expect(err).To(matchPathError())
	})

	It("converts flat page files to section folders and folds empty folders back", func() {
		root := tempTreeDir()
		Expect(os.WriteFile(filepath.Join(root, "guide.md"), []byte("# Guide"), 0o644)).To(Succeed())

		Expect(EnsurePageIsFolder(root, newFixtureRoutePath("guide"))).To(Succeed())
		Expect(os.ReadFile(filepath.Join(root, "guide", "index.md"))).To(Equal([]byte("# Guide")))
		_, err := os.Stat(filepath.Join(root, "guide.md"))
		Expect(err).To(MatchError(os.ErrNotExist))

		Expect(EnsurePageIsFolder(root, newFixtureRoutePath("guide"))).To(Succeed())
		Expect(FoldPageFolderIfEmpty(root, "missing")).To(Succeed())

		Expect(os.WriteFile(filepath.Join(root, "guide", "extra.md"), []byte("# Extra"), 0o644)).To(Succeed())
		Expect(FoldPageFolderIfEmpty(root, "guide")).To(Succeed())
		_, err = os.Stat(filepath.Join(root, "guide"))
		Expect(err).NotTo(HaveOccurred())

		Expect(os.Remove(filepath.Join(root, "guide", "extra.md"))).To(Succeed())
		Expect(FoldPageFolderIfEmpty(root, "guide")).To(Succeed())
		Expect(os.ReadFile(filepath.Join(root, "guide.md"))).To(Equal([]byte("# Guide")))
		_, err = os.Stat(filepath.Join(root, "guide"))
		Expect(err).To(MatchError(os.ErrNotExist))
	})
})
