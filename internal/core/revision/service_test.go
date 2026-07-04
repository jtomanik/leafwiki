package revision

import (
	"errors"
	ginkgo "github.com/onsi/ginkgo/v2"
	"os"
	"path/filepath"
	"syscall"

	. "github.com/onsi/gomega"
	"github.com/perber/wiki/internal/core/markdown"
	sharederrors "github.com/perber/wiki/internal/core/shared/errors"
	"github.com/perber/wiki/internal/core/tree"
)

func newRevisionTestService() (*Service, *tree.TreeService, string) {
	ginkgo.GinkgoHelper()

	storageDir := revisionTempDir()
	treeService := tree.NewTreeService(storageDir)
	Expect(treeService.LoadTree()).To(Succeed())

	return NewService(storageDir, treeService, nil), treeService, storageDir
}

func createRevisionTestPage(treeService *tree.TreeService, title, slug, content string) tree.PageID {
	ginkgo.GinkgoHelper()

	kind := tree.NodeKindPage
	id, err := treeService.CreateNode("tester", nil, title, tree.SlugFromString(slug), &kind)
	Expect(err).NotTo(HaveOccurred())
	Expect(id).NotTo(BeNil())
	Expect(treeService.UpdateNodeUncheckedVersion(newFixtureUserID("tester"), *id, title, tree.SlugFromString(slug), &content, false)).To(Succeed())
	return *id
}

func revisionTestPageID[T ~string](raw T) tree.PageID {
	return tree.PageIDFromString(raw)
}

func revisionTestUserID(raw string) tree.UserID {
	return tree.UserIDFromString(raw)
}

func renderRevisionTestMarkdown[T ~string](pageID T, title string, fields map[string]interface{}, extra map[string]interface{}, body string) string {
	ginkgo.GinkgoHelper()

	raw, err := markdown.RenderPageDocument(markdown.PageDocument{
		Body: body,
		Metadata: markdown.PageMetadata{
			Version: 1,
			Page: markdown.PageMetadataPage{
				ID:    string(pageID),
				Title: title,
			},
			Fields: fields,
			Extra:  extra,
		},
	})
	Expect(err).NotTo(HaveOccurred())
	return raw
}

func revisionAssetPath[T ~string](storageDir string, pageID T, parts ...string) string {
	elems := append([]string{storageDir, "assets", string(pageID)}, parts...)
	return filepath.Join(elems...)
}

func writeLiveAsset[T ~string](storageDir string, pageID T, name, content string) {
	ginkgo.GinkgoHelper()

	dir := revisionAssetPath(storageDir, pageID)
	Expect(os.MkdirAll(dir, 0o755)).To(Succeed())
	Expect(os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644)).To(Succeed())
}

var _ = ginkgo.Describe("service", func() {
	ginkgo.It("records the first content revision and reuses it when content is unchanged", func() {
		service, treeService, storageDir := newRevisionTestService()
		pageID := createRevisionTestPage(treeService, "Page", "page", "hello")
		authorID := revisionTestUserID("tester")
		writeLiveAsset(storageDir, pageID, "a.txt", "asset")

		firstRecord := createdRevisionRecord(service.RecordContentUpdate(revisionTestPageID(pageID), authorID, "first"))
		Expect(firstRecord).To(haveRecordedRevision(SatisfyAll(
			HaveField("Type", RevisionTypeContentUpdate),
			HaveField("ParentID", BeEmpty()),
			HaveField("AssetManifestHash", Not(BeEmpty())),
			HaveField("ContentHash", Not(BeEmpty())),
			HaveField("PageCreatedAt", Not(BeZero())),
			HaveField("PageUpdatedAt", Not(BeZero())),
		)))
		rev := firstRecord.Revision

		secondRecord := reusedRevisionRecord(service.RecordContentUpdate(revisionTestPageID(pageID), authorID, "second"))
		Expect(secondRecord).To(haveRecordedRevision(HaveField("ID", rev.ID)))
	})

	ginkgo.It("records batch content revisions once and preserves no-op histories", func() {
		service, treeService, storageDir := newRevisionTestService()
		pageID1 := createRevisionTestPage(treeService, "Page 1", "page-1", "hello")
		pageID2 := createRevisionTestPage(treeService, "Page 2", "page-2", "world")
		writeLiveAsset(storageDir, pageID1, "a.txt", "asset-a")
		writeLiveAsset(storageDir, pageID2, "b.txt", "asset-b")

		page1, err := treeService.GetPage(tree.PageIDFromString(pageID1))
		Expect(err).NotTo(HaveOccurred())
		page2, err := treeService.GetPage(tree.PageIDFromString(pageID2))
		Expect(err).NotTo(HaveOccurred())

		errs := service.RecordContentUpdates([]*tree.Page{page1, page2}, "tester", "batch")
		Expect(errs).To(HaveLen(2))
		Expect(errs).To(HaveEach(Succeed()))

		revisions1, err := service.ListRevisions(tree.PageIDFromString(pageID1))
		Expect(err).NotTo(HaveOccurred())
		Expect(revisions1).To(HaveExactElements(HaveField("Type", RevisionTypeContentUpdate)))

		revisions2, err := service.ListRevisions(tree.PageIDFromString(pageID2))
		Expect(err).NotTo(HaveOccurred())
		Expect(revisions2).To(HaveExactElements(HaveField("Type", RevisionTypeContentUpdate)))

		errs = service.RecordContentUpdates([]*tree.Page{page1, page2}, "tester", "batch")
		Expect(errs).To(HaveLen(2))
		Expect(errs).To(HaveEach(Succeed()))

		revisions1After, err := service.ListRevisions(tree.PageIDFromString(pageID1))
		Expect(err).NotTo(HaveOccurred())
		Expect(revisions1After).To(HaveLen(1))

		revisions2After, err := service.ListRevisions(tree.PageIDFromString(pageID2))
		Expect(err).NotTo(HaveOccurred())
		Expect(revisions2After).To(HaveLen(1))
	})

	ginkgo.It("preserves per-input errors while recording valid batch pages", func() {
		service, treeService, storageDir := newRevisionTestService()
		pageID1 := createRevisionTestPage(treeService, "Page 1", "page-1", "hello")
		pageID2 := createRevisionTestPage(treeService, "Page 2", "page-2", "world")
		writeLiveAsset(storageDir, pageID1, "a.txt", "asset-a")
		writeLiveAsset(storageDir, pageID2, "b.txt", "asset-b")

		page1, err := treeService.GetPage(tree.PageIDFromString(pageID1))
		Expect(err).NotTo(HaveOccurred())
		page2, err := treeService.GetPage(tree.PageIDFromString(pageID2))
		Expect(err).NotTo(HaveOccurred())

		errs := service.RecordContentUpdates([]*tree.Page{page1, nil, page2}, "tester", "batch")
		Expect(errs).To(HaveExactElements(
			Succeed(),
			rejectRevisionValidation(),
			Succeed(),
		))

		revisions1, err := service.ListRevisions(tree.PageIDFromString(pageID1))
		Expect(err).NotTo(HaveOccurred())
		Expect(revisions1).To(HaveLen(1))

		revisions2, err := service.ListRevisions(tree.PageIDFromString(pageID2))
		Expect(err).NotTo(HaveOccurred())
		Expect(revisions2).To(HaveLen(1))
	})

	ginkgo.It("records duplicate batch page IDs once", func() {
		service, treeService, storageDir := newRevisionTestService()
		pageID := createRevisionTestPage(treeService, "Page", "page", "hello")
		writeLiveAsset(storageDir, pageID, "a.txt", "asset-a")

		page, err := treeService.GetPage(tree.PageIDFromString(pageID))
		Expect(err).NotTo(HaveOccurred())

		errs := service.RecordContentUpdates([]*tree.Page{page, page}, "tester", "batch")
		Expect(errs).To(HaveLen(2))
		Expect(errs).To(HaveEach(Succeed()))

		revisions, err := service.ListRevisions(tree.PageIDFromString(pageID))
		Expect(err).NotTo(HaveOccurred())
		Expect(revisions).To(HaveLen(1))
	})

	ginkgo.It("wraps localized errors with causes and structured details", func() {
		cause := errors.New("boom")
		err := sharederrors.NewLocalizedError("code", "message", "template %s", cause, "arg")
		Expect(err.Error()).NotTo(BeEmpty())
		Expect(err).To(matchRevisionErrorCause(cause))
		Expect(err).To(matchLocalizedRevisionErrorDetails(sharederrors.ErrorCode("code"), "arg"))
		Expect(errors.New("plain")).NotTo(matchLocalizedRevisionErrorDetails(sharederrors.ErrorCode("code"), "arg"))
	})

	ginkgo.It("exposes revision wrappers and deletes page revision data", func() {
		service, treeService, storageDir := newRevisionTestService()
		pageID := createRevisionTestPage(treeService, "Page", "page", "hello")
		writeLiveAsset(storageDir, pageID, "a.txt", "asset")

		typedPageID := revisionTestPageID(pageID)
		state, err := service.CapturePageState(typedPageID)
		Expect(err).NotTo(HaveOccurred())
		Expect(state).To(SatisfyAll(
			HaveField("PageID", typedPageID),
			HaveField("Assets", HaveLen(1)),
		))

		Expect(createdRevisionRecord(service.RecordContentUpdate(revisionTestPageID(pageID), "tester", "content"))).
			To(haveRecordedRevision(Not(BeNil())))

		revisions, err := service.ListRevisions(tree.PageIDFromString(pageID))
		Expect(err).NotTo(HaveOccurred())
		Expect(revisions).To(HaveExactElements(HaveField("Type", RevisionTypeContentUpdate)))
		paged, _, err := service.ListRevisionsPage(tree.PageIDFromString(pageID), "", 1)
		Expect(err).NotTo(HaveOccurred())
		Expect(paged).To(HaveLen(1))

		Expect(service.DeletePageData(typedPageID)).To(Succeed())
		revisions, err = service.ListRevisions(tree.PageIDFromString(pageID))
		Expect(err).NotTo(HaveOccurred())
		Expect(revisions).To(BeEmpty())

		Expect(service.persistLiveAssets(typedPageID, nil)).To(Succeed())
		_, err = service.scanLiveAssets(revisionTestPageID("missing"))
		Expect(err).NotTo(HaveOccurred())
	})

	ginkgo.It("records asset and structure revisions only when state changes", func() {
		service, treeService, storageDir := newRevisionTestService()
		pageID := createRevisionTestPage(treeService, "Page", "page", "hello")
		writeLiveAsset(storageDir, pageID, "a.txt", "asset")

		firstAssetRecord := createdRevisionRecord(service.RecordAssetChange(revisionTestPageID(pageID), "tester", "asset"))
		Expect(firstAssetRecord).To(haveRecordedRevision(Not(BeNil())))
		rev1 := firstAssetRecord.Revision
		Expect(reusedRevisionRecord(service.RecordAssetChange(revisionTestPageID(pageID), "tester", "asset"))).
			To(haveRecordedRevision(HaveField("ID", rev1.ID)))

		parentKind := tree.NodeKindSection
		parentID, err := treeService.CreateNode("tester", nil, "Docs", "docs", &parentKind)
		Expect(err).NotTo(HaveOccurred())
		Expect(treeService.MoveNodeUncheckedVersion("tester", tree.PageIDFromString(pageID), *parentID)).To(Succeed())
		Expect(createdRevisionRecord(service.RecordStructureChange(revisionTestPageID(pageID), "tester", "structure"))).
			To(haveRecordedRevision(SatisfyAll(
				HaveField("Type", RevisionTypeStructureUpdate),
				HaveField("ParentID", *parentID),
			)))
	})

	ginkgo.It("rejects duplicate, missing, and invalid restore assets", func() {
		service, treeService, storageDir := newRevisionTestService()
		pageID := createRevisionTestPage(treeService, "Page", "page", "hello")

		typedPageID := revisionTestPageID(pageID)
		blobHash, blobSize := writeStoredAssetBlob(service.store, []byte("asset"))
		Expect(service.restoreAssets(typedPageID, []AssetRef{{Name: "dup.txt", SHA256: blobHash, SizeBytes: blobSize}, {Name: "dup.txt", SHA256: blobHash, SizeBytes: blobSize}})).To(MatchError(ErrDuplicateAssetName))
		Expect(service.restoreAssets(typedPageID, []AssetRef{{Name: "missing.txt", SHA256: "abc", SizeBytes: 3}})).To(matchRevisionError(os.ErrNotExist))

		assetPath := filepath.Join(storageDir, "standalone.txt")
		Expect(os.WriteFile(assetPath, []byte("css"), 0o644)).To(Succeed())
		ref, err := buildAssetRef(assetPath, "style.css")
		Expect(err).NotTo(HaveOccurred())
		Expect(ref.MIMEType).NotTo(Equal("application/octet-stream"))
		_, err = buildAssetRef(filepath.Join(storageDir, "missing.txt"), "missing.txt")
		Expect(err).To(matchRevisionError(os.ErrNotExist))
	})

	ginkgo.It("records restore revisions with and without live assets", func() {
		loggerService := NewService(revisionTempDir(), nil, nil)
		Expect(loggerService).NotTo(BeNil())
		Expect(loggerService.log).NotTo(BeNil())

		service, treeService, storageDir := newRevisionTestService()
		pageID := createRevisionTestPage(treeService, "Page", "page", "hello")
		writeLiveAsset(storageDir, pageID, "a.txt", "asset")

		typedPageID := revisionTestPageID(pageID)
		Expect(service.recordRestoreRevision(typedPageID, revisionTestUserID("tester"))).To(Succeed())
		latest, err := service.GetLatestRevision(tree.PageIDFromString(pageID))
		Expect(err).NotTo(HaveOccurred())
		Expect(latest).To(HaveField("Type", RevisionTypeRestore))

		Expect(os.RemoveAll(revisionAssetPath(storageDir, pageID))).To(Succeed())
		Expect(service.recordRestoreRevision(typedPageID, revisionTestUserID("tester"))).To(Succeed())
	})

	ginkgo.It("detects live assets and reports persistence mismatches", func() {
		service, treeService, storageDir := newRevisionTestService()
		pageID := createRevisionTestPage(treeService, "Page", "page", "hello")
		writeLiveAsset(storageDir, pageID, "a.txt", "asset")
		Expect(os.MkdirAll(revisionAssetPath(storageDir, pageID, "subdir"), 0o755)).To(Succeed())

		typedPageID := revisionTestPageID(pageID)
		refs, err := service.scanLiveAssets(typedPageID)
		Expect(err).NotTo(HaveOccurred())
		Expect(refs).To(HaveExactElements(HaveField("Name", "a.txt")))

		Expect(service.persistLiveAssets(typedPageID, []AssetRef{{Name: "a.txt", SHA256: "wrong", SizeBytes: int64(len("asset"))}})).To(matchRevisionError(ErrAssetBlobHashMismatch))
		goodRef, err := buildAssetRef(revisionAssetPath(storageDir, pageID, "a.txt"), "a.txt")
		Expect(err).NotTo(HaveOccurred())
		goodRef.SizeBytes++
		Expect(service.persistLiveAssets(typedPageID, []AssetRef{goodRef})).To(matchRevisionError(ErrAssetBlobSizeMismatch))

		badPageID := "bad-assets"
		badDir := filepath.Join(storageDir, "assets", badPageID)
		Expect(os.MkdirAll(filepath.Dir(badDir), 0o755)).To(Succeed())
		Expect(os.WriteFile(badDir, []byte("not a dir"), 0o644)).To(Succeed())
		_, err = service.scanLiveAssets(revisionTestPageID(badPageID))
		Expect(err).To(matchRevisionError(syscall.ENOTDIR))
	})

	ginkgo.It("reports restored asset hash and size mismatches", func() {
		service, _, _ := newRevisionTestService()

		hash := sha256HexBytes([]byte("asset"))
		assetBlob := service.store.AssetBlobPath(hash)
		Expect(os.MkdirAll(filepath.Dir(assetBlob), 0o755)).To(Succeed())
		Expect(os.WriteFile(assetBlob, []byte("tampered"), 0o644)).To(Succeed())
		Expect(service.restoreAssets("page-1", []AssetRef{{Name: "a.txt", SHA256: hash, SizeBytes: int64(len("asset"))}})).To(matchRevisionError(ErrAssetBlobHashMismatch))

		hash2, err := service.store.SaveContentBlob([]byte("size-ok"))
		Expect(err).NotTo(HaveOccurred())
		assetBlob2 := service.store.AssetBlobPath(hash2)
		Expect(os.MkdirAll(filepath.Dir(assetBlob2), 0o755)).To(Succeed())
		Expect(os.WriteFile(assetBlob2, []byte("size-ok"), 0o644)).To(Succeed())
		Expect(service.restoreAssets("page-2", []AssetRef{{Name: "a.txt", SHA256: hash2, SizeBytes: 999}})).To(matchRevisionError(ErrAssetBlobSizeMismatch))
	})

	ginkgo.It("records structure revisions even when no live assets exist", func() {
		service, treeService, _ := newRevisionTestService()
		pageID := createRevisionTestPage(treeService, "Page", "page", "hello")

		Expect(createdRevisionRecord(service.RecordStructureChange(revisionTestPageID(pageID), "tester", "structure"))).
			To(haveRecordedRevision(SatisfyAll(
				HaveField("Type", RevisionTypeStructureUpdate),
				HaveField("AssetManifestHash", Not(BeEmpty())),
			)))
	})

	ginkgo.It("captures live page state and trims revision authors", func() {
		service, treeService, storageDir := newRevisionTestService()
		pageID := createRevisionTestPage(treeService, "Page", "page", "hello")
		writeLiveAsset(storageDir, pageID, "a.txt", "asset")

		typedPageID := revisionTestPageID(pageID)
		state, err := service.capturePageState(typedPageID, true)
		Expect(err).NotTo(HaveOccurred())
		Expect(state).To(SatisfyAll(
			HaveField("PageID", typedPageID),
			HaveField("ParentID", BeEmpty()),
			HaveField("AssetManifestHash", Not(BeEmpty())),
			HaveField("Assets", HaveExactElements(HaveField("Name", "a.txt"))),
		))

		rev, err := service.newRevision(RevisionTypeContentUpdate, state, " tester ", "summary", state.AssetManifestHash)
		Expect(err).NotTo(HaveOccurred())
		Expect(rev).To(SatisfyAll(
			HaveField("PageID", typedPageID),
			HaveField("AuthorID", "tester"),
			HaveField("AssetManifestHash", state.AssetManifestHash),
			HaveField("PageCreatedAt", Not(BeZero())),
			HaveField("PageUpdatedAt", Not(BeZero())),
		))
	})

	ginkgo.It("records content and asset changes when the asset set is empty", func() {
		service, treeService, _ := newRevisionTestService()
		pageID := createRevisionTestPage(treeService, "Page", "page", "hello")

		firstAssetRecord := createdRevisionRecord(service.RecordAssetChange(revisionTestPageID(pageID), "tester", "asset"))
		Expect(firstAssetRecord).To(haveRecordedRevision(HaveField("AssetManifestHash", Not(BeEmpty()))))
		assetRev1 := firstAssetRecord.Revision
		Expect(reusedRevisionRecord(service.RecordAssetChange(revisionTestPageID(pageID), "tester", "asset"))).
			To(haveRecordedRevision(HaveField("ID", assetRev1.ID)))

		content := "hello-2"
		Expect(treeService.UpdateNodeUncheckedVersion(newFixtureUserID("tester"), tree.PageIDFromString(pageID), "Page", newFixtureSlug("page"), &content, false)).To(Succeed())
		Expect(createdRevisionRecord(service.RecordAssetChange(revisionTestPageID(pageID), "tester", "asset after content"))).
			To(haveRecordedRevision(Not(HaveField("ID", assetRev1.ID))))

		content = "hello-3"
		Expect(treeService.UpdateNodeUncheckedVersion(newFixtureUserID("tester"), tree.PageIDFromString(pageID), "Page", newFixtureSlug("page"), &content, false)).To(Succeed())
		Expect(createdRevisionRecord(service.RecordContentUpdate(revisionTestPageID(pageID), "tester", "content"))).
			To(haveRecordedRevision(HaveField("Type", RevisionTypeContentUpdate)))
	})

	ginkgo.It("rehydrates historical content and assets while preserving current route", func() {
		service, treeService, storageDir := newRevisionTestService()

		sectionKind := tree.NodeKindSection
		docsID, err := treeService.CreateNode("tester", nil, "Docs", "docs", &sectionKind)
		Expect(err).NotTo(HaveOccurred())
		archiveID, err := treeService.CreateNode("tester", nil, "Archive", "archive", &sectionKind)
		Expect(err).NotTo(HaveOccurred())

		pageKind := tree.NodeKindPage
		pageIDPtr, err := treeService.CreateNode("tester", docsID, "Original", "original", &pageKind)
		Expect(err).NotTo(HaveOccurred())
		pageID := *pageIDPtr

		originalContent := "first version"
		Expect(treeService.UpdateNodeUncheckedVersion(newFixtureUserID("tester"), tree.PageIDFromString(pageID), "Original", newFixtureSlug("original"), &originalContent, false)).To(Succeed())
		writeLiveAsset(storageDir, pageID, "old.txt", "old-asset")
		originalRecord := createdRevisionRecord(service.RecordAssetChange(revisionTestPageID(pageID), "tester", "original state"))
		Expect(originalRecord).To(haveRecordedRevision(Not(BeNil())))
		originalRev := originalRecord.Revision

		changedContent := "second version"
		Expect(treeService.UpdateNodeUncheckedVersion(newFixtureUserID("tester"), tree.PageIDFromString(pageID), "Changed", newFixtureSlug("changed"), &changedContent, false)).To(Succeed())
		Expect(treeService.MoveNodeUncheckedVersion("tester", tree.PageIDFromString(pageID), *archiveID)).To(Succeed())
		Expect(os.Remove(revisionAssetPath(storageDir, pageID, "old.txt"))).To(Succeed())
		writeLiveAsset(storageDir, pageID, "new.txt", "new-asset")

		Expect(service.RestoreRevision(tree.PageIDFromString(pageID), RevisionIDFromString(originalRev.ID), newFixtureUserID("tester"))).To(Succeed())

		page, err := treeService.GetPage(tree.PageIDFromString(pageID))
		Expect(err).NotTo(HaveOccurred())
		// Restore rehydrates revision content and title while preserving the current slug/path.
		Expect(page).To(SatisfyAll(
			HaveField("Title", "Original"),
			HaveField("Slug", newFixtureSlug("changed")),
			HaveField("Content", originalContent),
		))
		Expect(page.CalculatePath()).To(Equal("/archive/changed"))

		oldAsset, err := os.ReadFile(revisionAssetPath(storageDir, pageID, "old.txt"))
		Expect(err).NotTo(HaveOccurred())
		Expect(string(oldAsset)).To(Equal("old-asset"))
		_, err = os.Stat(revisionAssetPath(storageDir, pageID, "new.txt"))
		Expect(err).To(MatchError(os.ErrNotExist))

		latest, err := service.GetLatestRevision(tree.PageIDFromString(pageID))
		Expect(err).NotTo(HaveOccurred())
		Expect(latest).To(HaveField("Type", RevisionTypeRestore))
	})

	ginkgo.It("captures canonical page metadata without legacy extra frontmatter", func() {
		service, treeService, _ := newRevisionTestService()

		pageKind := tree.NodeKindPage
		pageIDPtr, err := treeService.CreateNode("tester", nil, "Page", "page", &pageKind)
		Expect(err).NotTo(HaveOccurred())
		pageID := *pageIDPtr

		firstRaw := renderRevisionTestMarkdown(pageID, "Page",
			map[string]interface{}{"customKey": "first"},
			map[string]interface{}{"aliases": []interface{}{"one"}},
			"Body",
		)
		Expect(treeService.UpdateNodeUncheckedVersion(newFixtureUserID("tester"), tree.PageIDFromString(pageID), "Page", newFixtureSlug("page"), &firstRaw, true)).To(Succeed())

		firstRecord := createdRevisionRecord(service.RecordContentUpdate(revisionTestPageID(pageID), "tester", "first"))
		Expect(firstRecord).To(haveRecordedRevision(SatisfyAll(
			HaveField("ExtraFrontmatter", BeNil()),
			HaveField("ExtraFrontmatterHash", BeEmpty()),
			HaveField("PageMetadata", Not(BeNil())),
		)))
		firstRev := firstRecord.Revision
		Expect(firstRev.PageMetadata.Fields).To(HaveKeyWithValue("customKey", "first"))

		secondRaw := renderRevisionTestMarkdown(pageID, "Page",
			map[string]interface{}{"customKey": "second"},
			map[string]interface{}{"aliases": []interface{}{"two"}},
			"Body",
		)
		Expect(treeService.UpdateNodeUncheckedVersion(newFixtureUserID("tester"), tree.PageIDFromString(pageID), "Page", newFixtureSlug("page"), &secondRaw, true)).To(Succeed())

		secondRecord := createdRevisionRecord(service.RecordContentUpdate(revisionTestPageID(pageID), "tester", "second"))
		Expect(secondRecord).To(haveRecordedRevision(SatisfyAll(
			Not(HaveField("ID", firstRev.ID)),
			HaveField("ExtraFrontmatter", BeNil()),
			HaveField("ExtraFrontmatterHash", BeEmpty()),
			HaveField("PageMetadata", Not(BeNil())),
		)))
		secondRev := secondRecord.Revision
		Expect(secondRev.PageMetadata).To(SatisfyAll(
			HaveField("Fields", HaveKeyWithValue("customKey", "second")),
			HaveField("Extra", HaveKeyWithValue("aliases", HaveExactElements("two"))),
		))
	})

	ginkgo.It("restores historical custom frontmatter while keeping managed fields current", func() {
		service, treeService, _ := newRevisionTestService()

		pageKind := tree.NodeKindPage
		pageIDPtr, err := treeService.CreateNode("creator", nil, "Page", "page", &pageKind)
		Expect(err).NotTo(HaveOccurred())
		pageID := *pageIDPtr

		firstRaw := renderRevisionTestMarkdown(pageID, "Page",
			map[string]interface{}{"customKey": "first"},
			map[string]interface{}{"aliases": []interface{}{"one"}},
			"Body",
		)
		Expect(treeService.UpdateNodeUncheckedVersion(newFixtureUserID("creator"), tree.PageIDFromString(pageID), "Page", newFixtureSlug("page"), &firstRaw, true)).To(Succeed())
		firstRecord := createdRevisionRecord(service.RecordContentUpdate(revisionTestPageID(pageID), "creator", "first"))
		Expect(firstRecord).To(haveRecordedRevision(Not(BeNil())))
		firstRev := firstRecord.Revision

		secondRaw := renderRevisionTestMarkdown(pageID, "Changed",
			map[string]interface{}{"customKey": "second"},
			map[string]interface{}{"aliases": []interface{}{"two"}},
			"Body changed",
		)
		Expect(treeService.UpdateNodeUncheckedVersion(newFixtureUserID("editor"), tree.PageIDFromString(pageID), "Changed", newFixtureSlug("page"), &secondRaw, true)).To(Succeed())
		Expect(createdRevisionRecord(service.RecordContentUpdate(revisionTestPageID(pageID), "editor", "second"))).
			To(haveRecordedRevision(Not(BeNil())))

		beforeRestore, err := treeService.GetPage(tree.PageIDFromString(pageID))
		Expect(err).NotTo(HaveOccurred())
		managedID := beforeRestore.ID
		managedCreatedAt := beforeRestore.Metadata.CreatedAt
		managedCreatorID := beforeRestore.Metadata.CreatorID
		beforeUpdatedAt := beforeRestore.Metadata.UpdatedAt

		Expect(service.RestoreRevision(tree.PageIDFromString(pageID), RevisionIDFromString(firstRev.ID), newFixtureUserID("restorer"))).To(Succeed())

		page, err := treeService.GetPage(tree.PageIDFromString(pageID))
		Expect(err).NotTo(HaveOccurred())
		Expect(page).To(SatisfyAll(
			HaveField("ID", managedID),
			HaveField("Title", "Page"),
			HaveField("Metadata.CreatedAt", managedCreatedAt),
			HaveField("Metadata.CreatorID", managedCreatorID),
			HaveField("Metadata.LastAuthorID", revisionTestUserID("restorer")),
		))
		Expect(page.Metadata.UpdatedAt).To(BeTemporally(">", beforeUpdatedAt))

		raw, err := treeService.ReadPageRaw(tree.PageIDFromString(pageID))
		Expect(err).NotTo(HaveOccurred())
		Expect(raw).To(haveCanonicalRevisionRawStorage())
		Expect(parsedRevisionFrontmatter(markdown.ParseFrontmatter(raw))).To(haveParsedRevisionFrontmatter(
			SatisfyAll(
				HaveField("LeafWikiID", managedID.MetadataValue()),
				HaveField("LeafWikiTitle", page.Title),
				HaveField("LeafWikiCreatorID", managedCreatorID.MetadataValue()),
				HaveField("LeafWikiLastAuthorID", "restorer"),
				HaveField("LeafWikiUpdatedAt", Not(BeEmpty())),
				HaveField("ExtraFields", SatisfyAll(
					HaveKeyWithValue("customKey", "first"),
					HaveKeyWithValue("aliases", HaveExactElements("one")),
				)),
			),
			Equal("Body"),
		))
	})

	ginkgo.It("restores canonical fields without moving extras into managed fields", func() {
		service, treeService, _ := newRevisionTestService()

		pageKind := tree.NodeKindPage
		pageIDPtr, err := treeService.CreateNode("creator", nil, "Page", "page", &pageKind)
		Expect(err).NotTo(HaveOccurred())
		pageID := *pageIDPtr

		firstRaw, err := markdown.RenderPageDocument(markdown.PageDocument{
			Body: "First body",
			Metadata: markdown.PageMetadata{
				Version: 1,
				Page:    markdown.PageMetadataPage{ID: pageID.MetadataValue(), Title: "Page"},
				Fields:  map[string]interface{}{"status": "draft", "priority": 2},
				Extra:   map[string]interface{}{"source": "imported"},
			},
		})
		Expect(err).NotTo(HaveOccurred())
		Expect(treeService.UpdateNodeUncheckedVersion(newFixtureUserID("creator"), pageID, "Page", newFixtureSlug("page"), &firstRaw, true)).To(Succeed())
		firstRecord := createdRevisionRecord(service.RecordContentUpdate(revisionTestPageID(pageID), "creator", "first"))
		Expect(firstRecord).To(haveRecordedRevision(Not(BeNil())))
		firstRev := firstRecord.Revision

		secondRaw, err := markdown.RenderPageDocument(markdown.PageDocument{
			Body: "Second body",
			Metadata: markdown.PageMetadata{
				Version: 1,
				Page:    markdown.PageMetadataPage{ID: pageID.MetadataValue(), Title: "Page"},
				Fields:  map[string]interface{}{"status": "ready"},
			},
		})
		Expect(err).NotTo(HaveOccurred())
		Expect(treeService.UpdateNodeUncheckedVersion(newFixtureUserID("editor"), pageID, "Page", newFixtureSlug("page"), &secondRaw, true)).To(Succeed())
		Expect(createdRevisionRecord(service.RecordContentUpdate(revisionTestPageID(pageID), "editor", "second"))).
			To(haveRecordedRevision(Not(BeNil())))

		Expect(service.RestoreRevision(tree.PageIDFromString(pageID), RevisionIDFromString(firstRev.ID), newFixtureUserID("restorer"))).To(Succeed())
		raw, err := treeService.ReadPageRaw(tree.PageIDFromString(pageID))
		Expect(err).NotTo(HaveOccurred())
		Expect(raw).To(haveCanonicalRevisionRawStorage())
		doc, _, err := markdown.ParsePageDocument(raw)
		Expect(err).NotTo(HaveOccurred())
		Expect(doc).To(SatisfyAll(
			HaveField("Metadata.Fields", SatisfyAll(
				HaveKeyWithValue("priority", 2),
				Not(HaveKey("source")),
			)),
			HaveField("Metadata.Extra", HaveKeyWithValue("source", "imported")),
			HaveField("Body", "First body"),
		))
	})

	ginkgo.It("restores an explicitly empty metadata snapshot", func() {
		service, treeService, _ := newRevisionTestService()

		pageKind := tree.NodeKindPage
		pageIDPtr, err := treeService.CreateNode("creator", nil, "Page", "page", &pageKind)
		Expect(err).NotTo(HaveOccurred())
		pageID := *pageIDPtr

		firstRaw := renderRevisionTestMarkdown(pageID, "Page", nil, nil, "Empty metadata body")
		Expect(treeService.UpdateNodeUncheckedVersion(newFixtureUserID("creator"), tree.PageIDFromString(pageID), "Page", newFixtureSlug("page"), &firstRaw, true)).To(Succeed())
		firstRecord := createdRevisionRecord(service.RecordContentUpdate(revisionTestPageID(pageID), "creator", "empty metadata"))
		Expect(firstRecord).To(haveRecordedRevision(Not(BeNil())))
		firstRev := firstRecord.Revision
		Expect(firstRev.PageMetadata).NotTo(BeNil())

		secondRaw := renderRevisionTestMarkdown(pageID, "Page",
			map[string]interface{}{"status": "ready"},
			map[string]interface{}{"source": "imported"},
			"Non-empty metadata body",
		)
		Expect(treeService.UpdateNodeUncheckedVersion(newFixtureUserID("editor"), tree.PageIDFromString(pageID), "Page", newFixtureSlug("page"), &secondRaw, true)).To(Succeed())
		Expect(createdRevisionRecord(service.RecordContentUpdate(revisionTestPageID(pageID), "editor", "non-empty metadata"))).
			To(haveRecordedRevision(Not(BeNil())))

		Expect(service.RestoreRevision(tree.PageIDFromString(pageID), RevisionIDFromString(firstRev.ID), newFixtureUserID("restorer"))).To(Succeed())
		raw, err := treeService.ReadPageRaw(tree.PageIDFromString(pageID))
		Expect(err).NotTo(HaveOccurred())
		Expect(raw).To(haveCanonicalRevisionRawStorage())
		doc, _, err := markdown.ParsePageDocument(raw)
		Expect(err).NotTo(HaveOccurred())
		Expect(doc).To(SatisfyAll(
			HaveField("Metadata.Tags", BeEmpty()),
			HaveField("Metadata.Fields", BeEmpty()),
			HaveField("Metadata.Extra", BeEmpty()),
			HaveField("Body", "Empty metadata body"),
		))
	})

	ginkgo.It("preserves current custom frontmatter for legacy revisions without metadata snapshots", func() {
		service, treeService, _ := newRevisionTestService()

		pageKind := tree.NodeKindPage
		pageIDPtr, err := treeService.CreateNode("creator", nil, "Page", "page", &pageKind)
		Expect(err).NotTo(HaveOccurred())
		pageID := *pageIDPtr

		initialRaw := renderRevisionTestMarkdown(pageID, "Page",
			map[string]interface{}{"customKey": "current"},
			nil,
			"Current body",
		)
		Expect(treeService.UpdateNodeUncheckedVersion(newFixtureUserID("creator"), tree.PageIDFromString(pageID), "Page", newFixtureSlug("page"), &initialRaw, true)).To(Succeed())

		page, err := treeService.GetPage(tree.PageIDFromString(pageID))
		Expect(err).NotTo(HaveOccurred())

		state := service.revisionStateFromPage(page)
		contentHash, err := service.store.SaveContentBlob([]byte("Legacy body"))
		Expect(err).NotTo(HaveOccurred())
		legacyRevision, err := service.newRevision(RevisionTypeContentUpdate, state, "legacy-author", "legacy", "")
		Expect(err).NotTo(HaveOccurred())
		legacyRevision.ContentHash = contentHash
		legacyRevision.ExtraFrontmatter = nil
		legacyRevision.ExtraFrontmatterHash = ""
		Expect(service.store.SaveRevision(legacyRevision)).To(Succeed())

		Expect(service.RestoreRevision(tree.PageIDFromString(pageID), RevisionIDFromString(legacyRevision.ID), newFixtureUserID("restorer"))).To(Succeed())

		raw, err := treeService.ReadPageRaw(tree.PageIDFromString(pageID))
		Expect(err).NotTo(HaveOccurred())
		Expect(raw).To(haveCanonicalRevisionRawStorage())
		Expect(parsedRevisionFrontmatter(markdown.ParseFrontmatter(raw))).To(haveParsedRevisionFrontmatter(
			HaveField("ExtraFields", HaveKeyWithValue("customKey", "current")),
			Equal("Legacy body"),
		))
	})

	ginkgo.It("writes canonical metadata from legacy revision extra frontmatter", func() {
		service, treeService, _ := newRevisionTestService()

		pageKind := tree.NodeKindPage
		pageIDPtr, err := treeService.CreateNode("creator", nil, "Page", "page", &pageKind)
		Expect(err).NotTo(HaveOccurred())
		pageID := *pageIDPtr

		initialContent := "Current body"
		Expect(treeService.UpdateNodeUncheckedVersion(newFixtureUserID("creator"), tree.PageIDFromString(pageID), "Page", newFixtureSlug("page"), &initialContent, false)).To(Succeed())

		page, err := treeService.GetPage(tree.PageIDFromString(pageID))
		Expect(err).NotTo(HaveOccurred())

		state := service.revisionStateFromPage(page)
		contentHash, err := service.store.SaveContentBlob([]byte("Legacy body with extra"))
		Expect(err).NotTo(HaveOccurred())
		legacyRevision, err := service.newRevision(RevisionTypeContentUpdate, state, "legacy-author", "legacy extra", "")
		Expect(err).NotTo(HaveOccurred())
		legacyRevision.ContentHash = contentHash
		legacyRevision.PageMetadata = nil
		legacyRevision.PageMetadataHash = ""
		legacyRevision.ExtraFrontmatter = map[string]interface{}{
			"status":  "legacy",
			"aliases": []interface{}{"old"},
		}
		legacyRevision.ExtraFrontmatterHash, err = hashExtraFrontmatter(legacyRevision.ExtraFrontmatter)
		Expect(err).NotTo(HaveOccurred())
		Expect(service.store.SaveRevision(legacyRevision)).To(Succeed())

		Expect(service.RestoreRevision(tree.PageIDFromString(pageID), RevisionIDFromString(legacyRevision.ID), newFixtureUserID("restorer"))).To(Succeed())

		raw, err := treeService.ReadPageRaw(tree.PageIDFromString(pageID))
		Expect(err).NotTo(HaveOccurred())
		Expect(raw).To(haveCanonicalRevisionRawStorage())
		doc, _, err := markdown.ParsePageDocument(raw)
		Expect(err).NotTo(HaveOccurred())
		Expect(doc).To(SatisfyAll(
			HaveField("Metadata", SatisfyAll(
				HaveField("Fields", HaveKeyWithValue("status", "legacy")),
				HaveField("Extra", HaveKeyWithValue("aliases", HaveExactElements("old"))),
			)),
			HaveField("Body", Equal("Legacy body with extra")),
		))
	})

	ginkgo.It("keeps legacy YAML-looking content in the page body", func() {
		service, treeService, _ := newRevisionTestService()

		pageKind := tree.NodeKindPage
		pageIDPtr, err := treeService.CreateNode("creator", nil, "Page", "page", &pageKind)
		Expect(err).NotTo(HaveOccurred())
		pageID := *pageIDPtr

		initialContent := "Current body"
		Expect(treeService.UpdateNodeUncheckedVersion(newFixtureUserID("creator"), tree.PageIDFromString(pageID), "Page", newFixtureSlug("page"), &initialContent, false)).To(Succeed())

		page, err := treeService.GetPage(tree.PageIDFromString(pageID))
		Expect(err).NotTo(HaveOccurred())

		legacyBody := "---\ntitle: not frontmatter\n---\nBody content"
		state := service.revisionStateFromPage(page)
		contentHash, err := service.store.SaveContentBlob([]byte(legacyBody))
		Expect(err).NotTo(HaveOccurred())
		legacyRevision, err := service.newRevision(RevisionTypeContentUpdate, state, "legacy-author", "legacy body-only", "")
		Expect(err).NotTo(HaveOccurred())
		legacyRevision.ContentHash = contentHash
		legacyRevision.ExtraFrontmatter = nil
		legacyRevision.ExtraFrontmatterHash = ""
		Expect(service.store.SaveRevision(legacyRevision)).To(Succeed())

		Expect(service.RestoreRevision(tree.PageIDFromString(pageID), RevisionIDFromString(legacyRevision.ID), newFixtureUserID("restorer"))).To(Succeed())

		restoredPage, err := treeService.GetPage(tree.PageIDFromString(pageID))
		Expect(err).NotTo(HaveOccurred())
		Expect(restoredPage.Content).To(Equal(legacyBody))

		raw, err := treeService.ReadPageRaw(tree.PageIDFromString(pageID))
		Expect(err).NotTo(HaveOccurred())
		Expect(raw).To(haveCanonicalRevisionRawStorage())
		fm, body, hasFrontmatter, err := markdown.ParseFrontmatter(raw)
		Expect(parsedRevisionFrontmatter(fm, body, hasFrontmatter, err)).
			To(haveParsedRevisionFrontmatter(HaveField("ExtraFields", BeEmpty()), Equal(legacyBody)))
	})

	ginkgo.It("rebuilds missing previous manifests for content and structure revisions", func() {
		service, treeService, storageDir := newRevisionTestService()
		pageID := createRevisionTestPage(treeService, "Page", "page", "hello")
		writeLiveAsset(storageDir, pageID, "a.txt", "asset-a")

		firstRecord := createdRevisionRecord(service.RecordAssetChange(revisionTestPageID(pageID), "tester", "asset"))
		Expect(firstRecord).To(haveRecordedRevision(Not(BeNil())))
		firstRev := firstRecord.Revision
		missingManifestPath := service.store.assetManifestPath(firstRev.AssetManifestHash)
		Expect(os.Remove(missingManifestPath)).To(Succeed())

		content := "hello-updated"
		Expect(treeService.UpdateNodeUncheckedVersion(newFixtureUserID("tester"), tree.PageIDFromString(pageID), "Page", newFixtureSlug("page"), &content, false)).To(Succeed())
		contentRecord := createdRevisionRecord(service.RecordContentUpdate(revisionTestPageID(pageID), "tester", "content"))
		Expect(contentRecord).To(haveRecordedRevision(HaveField("AssetManifestHash", firstRev.AssetManifestHash)))
		contentRev := contentRecord.Revision
		_, err := service.store.LoadAssetManifest(contentRev.AssetManifestHash)
		Expect(err).NotTo(HaveOccurred())

		Expect(os.Remove(service.store.assetManifestPath(contentRev.AssetManifestHash))).To(Succeed())
		Expect(createdRevisionRecord(service.RecordStructureChange(revisionTestPageID(pageID), "tester", "structure"))).
			To(haveRecordedRevision(HaveField("AssetManifestHash", firstRev.AssetManifestHash)))
	})

	ginkgo.It("reports missing and tampered revision artifacts", func() {
		service, treeService, storageDir := newRevisionTestService()

		pageID1 := createRevisionTestPage(treeService, "Page1", "page1", "hello")
		contentRecord := createdRevisionRecord(service.RecordContentUpdate(revisionTestPageID(pageID1), "tester", "content"))
		Expect(contentRecord).To(haveRecordedRevision(Not(BeNil())))
		Expect(os.Remove(service.store.contentBlobPath(contentRecord.Revision.ContentHash))).To(Succeed())
		issues1, err := service.CheckRevisionIntegrity(revisionTestPageID(pageID1))
		Expect(err).NotTo(HaveOccurred())
		Expect(issues1).To(HaveExactElements(matchRevisionIntegrityIssue(errCodeRevisionIntegrityMissingContent)))

		pageID2 := createRevisionTestPage(treeService, "Page2", "page2", "hello")
		writeLiveAsset(storageDir, pageID2, "a.txt", "asset-a")
		assetRecord := createdRevisionRecord(service.RecordAssetChange(revisionTestPageID(pageID2), "tester", "asset"))
		Expect(assetRecord).To(haveRecordedRevision(Not(BeNil())))
		assetRev := assetRecord.Revision
		Expect(os.Remove(service.store.assetManifestPath(assetRev.AssetManifestHash))).To(Succeed())
		issues2, err := service.CheckRevisionIntegrity(revisionTestPageID(pageID2))
		Expect(err).NotTo(HaveOccurred())
		Expect(issues2).To(HaveExactElements(matchRevisionIntegrityIssue(errCodeRevisionIntegrityMissingManifest)))

		pageID3 := createRevisionTestPage(treeService, "Page3", "page3", "hello")
		writeLiveAsset(storageDir, pageID3, "a.txt", "asset-a")
		assetRecord3 := createdRevisionRecord(service.RecordAssetChange(revisionTestPageID(pageID3), "tester", "asset"))
		Expect(assetRecord3).To(haveRecordedRevision(Not(BeNil())))
		assetRev3 := assetRecord3.Revision
		refs, err := service.store.LoadAssetManifest(assetRev3.AssetManifestHash)
		Expect(err).NotTo(HaveOccurred())
		Expect(refs).To(HaveLen(1))
		Expect(os.WriteFile(service.store.AssetBlobPath(refs[0].SHA256), []byte("tampered"), 0o644)).To(Succeed())
		issues3, err := service.CheckRevisionIntegrity(revisionTestPageID(pageID3))
		Expect(err).NotTo(HaveOccurred())
		Expect(issues3).To(HaveExactElements(matchRevisionIntegrityIssue(errCodeRevisionIntegrityHashMismatch)))
	})

	ginkgo.It("compares revision snapshots with content and asset deltas", func() {
		service, treeService, storageDir := newRevisionTestService()
		pageID := createRevisionTestPage(treeService, "Page", "page", "one")
		writeLiveAsset(storageDir, pageID, "a.txt", "asset-a")

		baseRecord := createdRevisionRecord(service.RecordAssetChange(revisionTestPageID(pageID), "tester", "base"))
		Expect(baseRecord).To(haveRecordedRevision(Not(BeNil())))
		baseRev := baseRecord.Revision

		content := "two"
		Expect(treeService.UpdateNodeUncheckedVersion(newFixtureUserID("tester"), tree.PageIDFromString(pageID), "Page", newFixtureSlug("page"), &content, false)).To(Succeed())
		writeLiveAsset(storageDir, pageID, "b.txt", "asset-b")
		targetRecord := createdRevisionRecord(service.RecordAssetChange(revisionTestPageID(pageID), "tester", "target"))
		Expect(targetRecord).To(haveRecordedRevision(Not(BeNil())))
		targetRev := targetRecord.Revision

		comparison, err := service.CompareRevisionSnapshots(tree.PageIDFromString(pageID), RevisionIDFromString(baseRev.ID), RevisionIDFromString(targetRev.ID))
		Expect(err).NotTo(HaveOccurred())
		Expect(comparison).To(SatisfyAll(
			HaveField("Base", Not(BeNil())),
			HaveField("Target", Not(BeNil())),
			HaveField("ContentChanged", BeTrue()),
			HaveField("AssetChanges", HaveExactElements(SatisfyAll(
				HaveField("Name", "b.txt"),
				HaveField("Status", "added"),
			))),
		))
	})

	ginkgo.It("returns stored revision asset blobs after live assets are deleted", func() {
		service, treeService, storageDir := newRevisionTestService()
		pageID := createRevisionTestPage(treeService, "Page", "page", "one")
		writeLiveAsset(storageDir, pageID, "image.png", "asset-image")

		record := createdRevisionRecord(service.RecordAssetChange(revisionTestPageID(pageID), "tester", "with asset"))
		Expect(record).To(haveRecordedRevision(Not(BeNil())))
		rev := record.Revision

		Expect(os.Remove(revisionAssetPath(storageDir, pageID, "image.png"))).To(Succeed())

		asset, err := service.GetRevisionAsset(tree.PageIDFromString(pageID), RevisionIDFromString(rev.ID), tree.AssetName("image.png"))
		Expect(err).NotTo(HaveOccurred())
		Expect(asset).NotTo(BeNil())
		Expect(asset.Asset.Name).To(Equal("image.png"))
		content, err := os.ReadFile(asset.Path)
		Expect(err).NotTo(HaveOccurred())
		Expect(string(content)).To(Equal("asset-image"))
	})

	ginkgo.It("returns a localized asset-not-found error for missing manifest entries", func() {
		service, treeService, storageDir := newRevisionTestService()
		pageID := createRevisionTestPage(treeService, "Page", "page", "one")
		writeLiveAsset(storageDir, pageID, "image.png", "asset-image")

		record := createdRevisionRecord(service.RecordAssetChange(revisionTestPageID(pageID), "tester", "with asset"))
		Expect(record).To(haveRecordedRevision(Not(BeNil())))
		rev := record.Revision

		_, err := service.GetRevisionAsset(tree.PageIDFromString(pageID), RevisionIDFromString(rev.ID), tree.AssetName("missing.png"))
		Expect(err).To(MatchLocalizedRevisionErrorCode(errCodeRevisionPreviewAssetNotFound))
	})
})
