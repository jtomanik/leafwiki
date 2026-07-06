package revision

import (
	"errors"
	sharederrors "github.com/perber/wiki/internal/core/shared/errors"
	"os"
	"path/filepath"
	"syscall"

	. "github.com/onsi/gomega"

	ginkgo "github.com/onsi/ginkgo/v2"
	"github.com/perber/wiki/internal/core/tree"
)

var _ = ginkgo.Describe("service", func() {
	ginkgo.It("records the first content revision and reuses it when content is unchanged", ginkgo.Label("integration"), func() {
		service, treeService, storageDir := newRevisionTestService()
		pageID := createRevisionTestPage(treeService, "Page", newFixtureSlug("page"), "hello")
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

	ginkgo.It("records batch content revisions once and preserves no-op histories", ginkgo.Label("integration"), func() {
		service, treeService, storageDir := newRevisionTestService()
		pageID1 := createRevisionTestPage(treeService, "Page 1", newFixtureSlug("page-1"), "hello")
		pageID2 := createRevisionTestPage(treeService, "Page 2", newFixtureSlug("page-2"), "world")
		writeLiveAsset(storageDir, pageID1, "a.txt", "asset-a")
		writeLiveAsset(storageDir, pageID2, "b.txt", "asset-b")

		page1, err := treeService.GetPage(tree.PageIDFromString(pageID1))
		Expect(err).NotTo(HaveOccurred())
		page2, err := treeService.GetPage(tree.PageIDFromString(pageID2))
		Expect(err).NotTo(HaveOccurred())

		errs := service.RecordContentUpdates([]*tree.Page{page1, page2}, newFixtureUserID("tester"), "batch")
		Expect(errs).To(HaveLen(2))
		Expect(errs).To(HaveEach(Succeed()))

		revisions1, err := service.ListRevisions(tree.PageIDFromString(pageID1))
		Expect(err).NotTo(HaveOccurred())
		Expect(revisions1).To(HaveExactElements(HaveField("Type", RevisionTypeContentUpdate)))

		revisions2, err := service.ListRevisions(tree.PageIDFromString(pageID2))
		Expect(err).NotTo(HaveOccurred())
		Expect(revisions2).To(HaveExactElements(HaveField("Type", RevisionTypeContentUpdate)))

		errs = service.RecordContentUpdates([]*tree.Page{page1, page2}, newFixtureUserID("tester"), "batch")
		Expect(errs).To(HaveLen(2))
		Expect(errs).To(HaveEach(Succeed()))

		revisions1After, err := service.ListRevisions(tree.PageIDFromString(pageID1))
		Expect(err).NotTo(HaveOccurred())
		Expect(revisions1After).To(HaveLen(1))

		revisions2After, err := service.ListRevisions(tree.PageIDFromString(pageID2))
		Expect(err).NotTo(HaveOccurred())
		Expect(revisions2After).To(HaveLen(1))
	})

	ginkgo.It("preserves per-input errors while recording valid batch pages", ginkgo.Label("integration"), func() {
		service, treeService, storageDir := newRevisionTestService()
		pageID1 := createRevisionTestPage(treeService, "Page 1", newFixtureSlug("page-1"), "hello")
		pageID2 := createRevisionTestPage(treeService, "Page 2", newFixtureSlug("page-2"), "world")
		writeLiveAsset(storageDir, pageID1, "a.txt", "asset-a")
		writeLiveAsset(storageDir, pageID2, "b.txt", "asset-b")

		page1, err := treeService.GetPage(tree.PageIDFromString(pageID1))
		Expect(err).NotTo(HaveOccurred())
		page2, err := treeService.GetPage(tree.PageIDFromString(pageID2))
		Expect(err).NotTo(HaveOccurred())

		errs := service.RecordContentUpdates([]*tree.Page{page1, nil, page2}, newFixtureUserID("tester"), "batch")
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

	ginkgo.It("records duplicate batch page IDs once", ginkgo.Label("integration"), func() {
		service, treeService, storageDir := newRevisionTestService()
		pageID := createRevisionTestPage(treeService, "Page", newFixtureSlug("page"), "hello")
		writeLiveAsset(storageDir, pageID, "a.txt", "asset-a")

		page, err := treeService.GetPage(tree.PageIDFromString(pageID))
		Expect(err).NotTo(HaveOccurred())

		errs := service.RecordContentUpdates([]*tree.Page{page, page}, newFixtureUserID("tester"), "batch")
		Expect(errs).To(HaveLen(2))
		Expect(errs).To(HaveEach(Succeed()))

		revisions, err := service.ListRevisions(tree.PageIDFromString(pageID))
		Expect(err).NotTo(HaveOccurred())
		Expect(revisions).To(HaveLen(1))
	})

	ginkgo.It("wraps localized errors with causes and structured details", ginkgo.Label("unit"), func() {
		cause := errors.New("boom")
		err := sharederrors.NewLocalizedError(newFixtureErrorCode("code"), "message", "template %s", cause, "arg")
		Expect(err.Error()).NotTo(BeEmpty())
		Expect(err).To(matchRevisionErrorCause(cause))
		Expect(err).To(matchLocalizedRevisionErrorDetails(newFixtureErrorCode("code"), "arg"))
		Expect(errors.New("plain")).NotTo(matchLocalizedRevisionErrorDetails(newFixtureErrorCode("code"), "arg"))
	})

	ginkgo.It("exposes revision wrappers and deletes page revision data", ginkgo.Label("integration"), func() {
		service, treeService, storageDir := newRevisionTestService()
		pageID := createRevisionTestPage(treeService, "Page", newFixtureSlug("page"), "hello")
		writeLiveAsset(storageDir, pageID, "a.txt", "asset")

		typedPageID := revisionTestPageID(pageID)
		state, err := service.CapturePageState(typedPageID)
		Expect(err).NotTo(HaveOccurred())
		Expect(state).To(SatisfyAll(
			HaveField("PageID", typedPageID),
			HaveField("Assets", HaveLen(1)),
		))

		Expect(createdRevisionRecord(service.RecordContentUpdate(revisionTestPageID(pageID), newFixtureUserID("tester"), "content"))).
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

	ginkgo.It("records asset and structure revisions only when state changes", ginkgo.Label("integration"), func() {
		service, treeService, storageDir := newRevisionTestService()
		pageID := createRevisionTestPage(treeService, "Page", newFixtureSlug("page"), "hello")
		writeLiveAsset(storageDir, pageID, "a.txt", "asset")

		firstAssetRecord := createdRevisionRecord(service.RecordAssetChange(revisionTestPageID(pageID), newFixtureUserID("tester"), "asset"))
		Expect(firstAssetRecord).To(haveRecordedRevision(Not(BeNil())))
		rev1 := firstAssetRecord.Revision
		Expect(reusedRevisionRecord(service.RecordAssetChange(revisionTestPageID(pageID), newFixtureUserID("tester"), "asset"))).
			To(haveRecordedRevision(HaveField("ID", rev1.ID)))

		parentKind := tree.NodeKindSection
		parentID, err := treeService.CreateNode(newFixtureUserID("tester"), nil, "Docs", newFixtureSlug("docs"), &parentKind)
		Expect(err).NotTo(HaveOccurred())
		Expect(treeService.MoveNodeUncheckedVersion(newFixtureUserID("tester"), tree.PageIDFromString(pageID), *parentID)).To(Succeed())
		Expect(createdRevisionRecord(service.RecordStructureChange(revisionTestPageID(pageID), newFixtureUserID("tester"), "structure"))).
			To(haveRecordedRevision(SatisfyAll(
				HaveField("Type", RevisionTypeStructureUpdate),
				HaveField("ParentID", *parentID),
			)))
	})

	ginkgo.It("rejects duplicate, missing, and invalid restore assets", ginkgo.Label("integration"), func() {
		service, treeService, storageDir := newRevisionTestService()
		pageID := createRevisionTestPage(treeService, "Page", newFixtureSlug("page"), "hello")

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

	ginkgo.It("records restore revisions with and without live assets", ginkgo.Label("integration"), func() {
		loggerService := NewService(revisionTempDir(), nil, nil)
		Expect(loggerService).NotTo(BeNil())
		Expect(loggerService.log).NotTo(BeNil())

		service, treeService, storageDir := newRevisionTestService()
		pageID := createRevisionTestPage(treeService, "Page", newFixtureSlug("page"), "hello")
		writeLiveAsset(storageDir, pageID, "a.txt", "asset")

		typedPageID := revisionTestPageID(pageID)
		Expect(service.recordRestoreRevision(typedPageID, revisionTestUserID("tester"))).To(Succeed())
		latest, err := service.GetLatestRevision(tree.PageIDFromString(pageID))
		Expect(err).NotTo(HaveOccurred())
		Expect(latest).To(HaveField("Type", RevisionTypeRestore))

		Expect(os.RemoveAll(revisionAssetPath(storageDir, pageID))).To(Succeed())
		Expect(service.recordRestoreRevision(typedPageID, revisionTestUserID("tester"))).To(Succeed())
	})

	ginkgo.It("detects live assets and reports persistence mismatches", ginkgo.Label("integration"), func() {
		service, treeService, storageDir := newRevisionTestService()
		pageID := createRevisionTestPage(treeService, "Page", newFixtureSlug("page"), "hello")
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

	ginkgo.It("reports restored asset hash and size mismatches", ginkgo.Label("integration"), func() {
		service, _, _ := newRevisionTestService()

		hash := sha256HexBytes([]byte("asset"))
		assetBlob := service.store.AssetBlobPath(hash)
		Expect(os.MkdirAll(filepath.Dir(assetBlob), 0o755)).To(Succeed())
		Expect(os.WriteFile(assetBlob, []byte("tampered"), 0o644)).To(Succeed())
		Expect(service.restoreAssets(newFixturePageID("page-1"), []AssetRef{{Name: "a.txt", SHA256: hash, SizeBytes: int64(len("asset"))}})).To(matchRevisionError(ErrAssetBlobHashMismatch))

		hash2, err := service.store.SaveContentBlob([]byte("size-ok"))
		Expect(err).NotTo(HaveOccurred())
		assetBlob2 := service.store.AssetBlobPath(hash2)
		Expect(os.MkdirAll(filepath.Dir(assetBlob2), 0o755)).To(Succeed())
		Expect(os.WriteFile(assetBlob2, []byte("size-ok"), 0o644)).To(Succeed())
		Expect(service.restoreAssets(newFixturePageID("page-2"), []AssetRef{{Name: "a.txt", SHA256: hash2, SizeBytes: 999}})).To(matchRevisionError(ErrAssetBlobSizeMismatch))
	})

	ginkgo.It("records structure revisions even when no live assets exist", ginkgo.Label("integration"), func() {
		service, treeService, _ := newRevisionTestService()
		pageID := createRevisionTestPage(treeService, "Page", newFixtureSlug("page"), "hello")

		Expect(createdRevisionRecord(service.RecordStructureChange(revisionTestPageID(pageID), newFixtureUserID("tester"), "structure"))).
			To(haveRecordedRevision(SatisfyAll(
				HaveField("Type", RevisionTypeStructureUpdate),
				HaveField("AssetManifestHash", Not(BeEmpty())),
			)))
	})

	ginkgo.It("captures live page state and trims revision authors", ginkgo.Label("integration"), func() {
		service, treeService, storageDir := newRevisionTestService()
		pageID := createRevisionTestPage(treeService, "Page", newFixtureSlug("page"), "hello")
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

		rev, err := service.newRevision(RevisionTypeContentUpdate, state, newFixtureUserID(" tester "), "summary", state.AssetManifestHash)
		Expect(err).NotTo(HaveOccurred())
		Expect(rev).To(SatisfyAll(
			HaveField("PageID", typedPageID),
			HaveField("AuthorID", "tester"),
			HaveField("AssetManifestHash", state.AssetManifestHash),
			HaveField("PageCreatedAt", Not(BeZero())),
			HaveField("PageUpdatedAt", Not(BeZero())),
		))
	})

	ginkgo.It("records content and asset changes when the asset set is empty", ginkgo.Label("integration"), func() {
		service, treeService, _ := newRevisionTestService()
		pageID := createRevisionTestPage(treeService, "Page", newFixtureSlug("page"), "hello")

		firstAssetRecord := createdRevisionRecord(service.RecordAssetChange(revisionTestPageID(pageID), newFixtureUserID("tester"), "asset"))
		Expect(firstAssetRecord).To(haveRecordedRevision(HaveField("AssetManifestHash", Not(BeEmpty()))))
		assetRev1 := firstAssetRecord.Revision
		Expect(reusedRevisionRecord(service.RecordAssetChange(revisionTestPageID(pageID), newFixtureUserID("tester"), "asset"))).
			To(haveRecordedRevision(HaveField("ID", assetRev1.ID)))

		content := "hello-2"
		Expect(treeService.UpdateNodeUncheckedVersion(newFixtureUserID("tester"), tree.PageIDFromString(pageID), "Page", newFixtureSlug("page"), &content, false)).To(Succeed())
		Expect(createdRevisionRecord(service.RecordAssetChange(revisionTestPageID(pageID), newFixtureUserID("tester"), "asset after content"))).
			To(haveRecordedRevision(Not(HaveField("ID", assetRev1.ID))))

		content = "hello-3"
		Expect(treeService.UpdateNodeUncheckedVersion(newFixtureUserID("tester"), tree.PageIDFromString(pageID), "Page", newFixtureSlug("page"), &content, false)).To(Succeed())
		Expect(createdRevisionRecord(service.RecordContentUpdate(revisionTestPageID(pageID), newFixtureUserID("tester"), "content"))).
			To(haveRecordedRevision(HaveField("Type", RevisionTypeContentUpdate)))
	})

	ginkgo.It("rehydrates historical content and assets while preserving current route", ginkgo.Label("integration"), func() {
		service, treeService, storageDir := newRevisionTestService()

		sectionKind := tree.NodeKindSection
		docsID, err := treeService.CreateNode(newFixtureUserID("tester"), nil, "Docs", newFixtureSlug("docs"), &sectionKind)
		Expect(err).NotTo(HaveOccurred())
		archiveID, err := treeService.CreateNode(newFixtureUserID("tester"), nil, "Archive", newFixtureSlug("archive"), &sectionKind)
		Expect(err).NotTo(HaveOccurred())

		pageKind := tree.NodeKindPage
		pageIDPtr, err := treeService.CreateNode(newFixtureUserID("tester"), docsID, "Original", newFixtureSlug("original"), &pageKind)
		Expect(err).NotTo(HaveOccurred())
		pageID := *pageIDPtr

		originalContent := "first version"
		Expect(treeService.UpdateNodeUncheckedVersion(newFixtureUserID("tester"), tree.PageIDFromString(pageID), "Original", newFixtureSlug("original"), &originalContent, false)).To(Succeed())
		writeLiveAsset(storageDir, pageID, "old.txt", "old-asset")
		originalRecord := createdRevisionRecord(service.RecordAssetChange(revisionTestPageID(pageID), newFixtureUserID("tester"), "original state"))
		Expect(originalRecord).To(haveRecordedRevision(Not(BeNil())))
		originalRev := originalRecord.Revision

		changedContent := "second version"
		Expect(treeService.UpdateNodeUncheckedVersion(newFixtureUserID("tester"), tree.PageIDFromString(pageID), "Changed", newFixtureSlug("changed"), &changedContent, false)).To(Succeed())
		Expect(treeService.MoveNodeUncheckedVersion(newFixtureUserID("tester"), tree.PageIDFromString(pageID), *archiveID)).To(Succeed())
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

	ginkgo.It("captures canonical page metadata without legacy extra frontmatter", ginkgo.Label("integration"), func() {
		service, treeService, _ := newRevisionTestService()

		pageKind := tree.NodeKindPage
		pageIDPtr, err := treeService.CreateNode(newFixtureUserID("tester"), nil, "Page", newFixtureSlug("page"), &pageKind)
		Expect(err).NotTo(HaveOccurred())
		pageID := *pageIDPtr

		firstRaw := renderRevisionTestMarkdown(pageID, "Page",
			map[string]interface{}{"customKey": "first"},
			map[string]interface{}{"aliases": []interface{}{"one"}},
			"Body",
		)
		Expect(treeService.UpdateNodeUncheckedVersion(newFixtureUserID("tester"), tree.PageIDFromString(pageID), "Page", newFixtureSlug("page"), &firstRaw, true)).To(Succeed())

		firstRecord := createdRevisionRecord(service.RecordContentUpdate(revisionTestPageID(pageID), newFixtureUserID("tester"), "first"))
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

		secondRecord := createdRevisionRecord(service.RecordContentUpdate(revisionTestPageID(pageID), newFixtureUserID("tester"), "second"))
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
})
