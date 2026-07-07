package revision

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"time"

	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/gstruct"

	"github.com/perber/wiki/internal/core/markdown"
	"github.com/perber/wiki/internal/core/tree"
)

var _ = ginkgo.Describe("revision service metadata asset unit seams", ginkgo.Label("unit"), func() {
	ginkgo.It("classifies revision integrity issues for stored artifacts", func() {
		pageID := newFixturePageID("integrity-page")
		service := NewService(revisionTempDir(), nil, nil)
		contentHash, err := service.store.SaveContentBlob([]byte("integrity body"))
		Expect(err).To(Succeed())
		assetHash, assetSize := writeStoredAssetBlob(service.store, []byte("asset body"))
		manifestHash, err := service.store.SaveAssetManifest([]AssetRef{{
			Name:      "asset.txt",
			SHA256:    assetHash,
			SizeBytes: assetSize,
			MIMEType:  "text/plain; charset=utf-8",
		}})
		Expect(err).To(Succeed())
		saveRevisionFixture(service.store, pageID, newFixtureRevisionID("integrity-ok"), time.Date(2026, 7, 7, 8, 10, 0, 0, time.UTC), contentHash, manifestHash)

		issues, err := service.CheckRevisionIntegrity(pageID)
		Expect(err).To(Succeed())
		Expect(issues).To(BeEmpty())

		missingContent := saveRevisionFixture(service.store, pageID, newFixtureRevisionID("integrity-missing-content"), time.Date(2026, 7, 7, 8, 11, 0, 0, time.UTC), "missing-content", manifestHash)
		missingManifest := saveRevisionFixture(service.store, pageID, newFixtureRevisionID("integrity-missing-manifest"), time.Date(2026, 7, 7, 8, 12, 0, 0, time.UTC), contentHash, "missing-manifest")
		missingAssetHash, missingAssetSize := writeStoredAssetBlob(service.store, []byte("missing asset"))
		Expect(os.Remove(service.store.AssetBlobPath(missingAssetHash))).To(Succeed())
		missingAssetManifestHash, err := service.store.SaveAssetManifest([]AssetRef{{
			Name:      "missing.txt",
			SHA256:    missingAssetHash,
			SizeBytes: missingAssetSize,
			MIMEType:  "text/plain; charset=utf-8",
		}})
		Expect(err).To(Succeed())
		missingAsset := saveRevisionFixture(service.store, pageID, newFixtureRevisionID("integrity-missing-asset"), time.Date(2026, 7, 7, 8, 13, 0, 0, time.UTC), contentHash, missingAssetManifestHash)
		tamperedHash, tamperedSize := writeStoredAssetBlob(service.store, []byte("original asset"))
		Expect(os.WriteFile(service.store.AssetBlobPath(tamperedHash), []byte("tampered asset"), 0o644)).To(Succeed())
		tamperedManifestHash, err := service.store.SaveAssetManifest([]AssetRef{{
			Name:      "tampered.txt",
			SHA256:    tamperedHash,
			SizeBytes: tamperedSize,
			MIMEType:  "text/plain; charset=utf-8",
		}})
		Expect(err).To(Succeed())
		tamperedAsset := saveRevisionFixture(service.store, pageID, newFixtureRevisionID("integrity-tampered-asset"), time.Date(2026, 7, 7, 8, 14, 0, 0, time.UTC), contentHash, tamperedManifestHash)

		issues, err = service.CheckRevisionIntegrity(pageID)
		Expect(err).To(Succeed())
		Expect(issues).To(ContainElements(
			SatisfyAll(matchRevisionIntegrityIssue(errCodeRevisionIntegrityMissingContent), HaveField("RevisionID", missingContent.ID)),
			SatisfyAll(matchRevisionIntegrityIssue(errCodeRevisionIntegrityMissingManifest), HaveField("RevisionID", missingManifest.ID)),
			SatisfyAll(matchRevisionIntegrityIssue(errCodeRevisionIntegrityMissingAssetBlob), HaveField("RevisionID", missingAsset.ID)),
			SatisfyAll(matchRevisionIntegrityIssue(errCodeRevisionIntegrityHashMismatch), HaveField("RevisionID", tamperedAsset.ID)),
		))
	})

	ginkgo.It("captures structured page metadata and batch content updates through unit seams", func() {
		pageID := newFixturePageID("metadata-page")
		authorID := newFixtureUserID("author-user")
		page := unitRevisionPage(pageID, "Metadata body")
		service := NewService(revisionTempDir(), nil, nil)
		setRevisionSeam(&revisionPagesGetPage, func(*tree.TreeService, tree.PageID) (*tree.Page, error) {
			return page, nil
		})
		setRevisionSeam(&revisionPagesReadPageRaw, func(*tree.TreeService, tree.PageID) (string, error) {
			return renderRevisionTestMarkdown(pageID.MetadataValue(), "Guide", map[string]interface{}{"status": "draft"}, map[string]interface{}{"priority": "high"}, page.Content), nil
		})
		restoreProcs := setRevisionSeam(&revisionGOMAXPROCS, func(int) int {
			return 0
		})

		errs := service.RecordContentUpdates([]*tree.Page{page}, authorID, "batch update")
		Expect(errs).To(ConsistOf(BeNil()))
		restoreProcs()

		rev, err := service.GetLatestRevision(pageID)
		Expect(err).To(Succeed())
		Expect(rev).To(gstruct.PointTo(gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
			"PageMetadata": gstruct.PointTo(gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
				"Fields": HaveKeyWithValue("status", "draft"),
				"Extra":  HaveKeyWithValue("priority", "high"),
			})),
			"PageMetadataHash":     Not(BeEmpty()),
			"ExtraFrontmatterHash": BeEmpty(),
		})))

		Expect(hashExtraFrontmatter(map[string]interface{}{"priority": "high"})).To(Not(BeEmpty()))
		raw, replaceMetadata, err := buildRestoredRawContent(pageID, "Guide", rev.PageMetadata, nil, "Metadata body")
		Expect(restoredRawContentObservation(raw, replaceMetadata, err)).To(matchRestoredRawContent(
			revisionRestoredRawReplacingMetadata,
			haveCanonicalRevisionRawStorage(),
		))
		raw, replaceMetadata, err = buildRestoredRawContent(pageID, "Guide", &markdown.PageMetadata{
			Version: 1,
			Extra:   map[string]interface{}{"priority": "high"},
		}, nil, "Metadata body")
		Expect(restoredRawContentObservation(raw, replaceMetadata, err)).To(matchRestoredRawContent(
			revisionRestoredRawReplacingMetadata,
			haveCanonicalRevisionRawStorage(),
		))
		raw, replaceMetadata, err = buildRestoredRawContent(pageID, "Guide", nil, map[string]interface{}{"priority": "high"}, "Metadata body")
		Expect(restoredRawContentObservation(raw, replaceMetadata, err)).To(matchRestoredRawContent(
			revisionRestoredRawReplacingMetadata,
			haveCanonicalRevisionRawStorage(),
		))
		frontmatter, body, hasFrontmatter, err := markdown.ParseFrontmatter(raw)
		Expect(parsedRevisionFrontmatter(frontmatter, body, hasFrontmatter, err)).To(haveParsedRevisionFrontmatter(
			gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
				"ExtraFields": HaveKeyWithValue("priority", "high"),
			}),
			Equal(page.Content),
		))

		state := service.revisionStateFromPage(&tree.Page{
			PageNode: &tree.PageNode{
				ID:     pageID,
				Parent: &tree.PageNode{ID: newFixturePageID("parent-page")},
				Title:  "Guide",
				Slug:   newFixtureSlug("guide"),
				Kind:   tree.NodeKindPage,
			},
		})
		Expect(state.ParentID).To(Equal(newFixturePageID("parent-page")))
		Expect(hashPageMetadata(nil)).To(BeEmpty())
		Expect(hashExtraFrontmatter(nil)).To(BeEmpty())
		_, err = hashExtraFrontmatter(map[string]interface{}{"bad": func() {}})
		Expect(err).To(MatchJSONUnsupportedTypeError())
	})

	ginkgo.It("resolves manifest cache fallbacks and snapshot preview failures", func() {
		pageID := newFixturePageID("preview-page")
		authorID := newFixtureUserID("author-user")
		page := unitRevisionPage(pageID, "Preview body")
		service := NewService(revisionTempDir(), nil, nil)
		setRevisionSeam(&revisionPagesGetPage, func(*tree.TreeService, tree.PageID) (*tree.Page, error) {
			return page, nil
		})
		setRevisionSeam(&revisionPagesReadPageRaw, func(*tree.TreeService, tree.PageID) (string, error) {
			return page.Content, nil
		})

		manifestHash, err := service.store.SaveAssetManifest(nil)
		Expect(err).To(Succeed())
		service.assetManifestCache.Store(pageID, assetManifestEntry{hash: "stale-manifest"})
		prev := &Revision{PageID: pageID, AssetManifestHash: manifestHash}
		resolvedHash, err := service.resolveAssetManifestHash(pageID, prev)
		Expect(err).To(Succeed())
		Expect(assetManifestObservationFor(service.store, resolvedHash)).To(matchAssetManifestPresence(assetManifestPresent, Equal(manifestHash)))

		restoreManifest := setRevisionSeam(&revisionStoreLoadAssetManifest, func(*FSStore, string) ([]AssetRef, error) {
			return nil, os.ErrNotExist
		})
		writeUnitRevisionLiveAsset(service.storageDir, pageID, "fallback.bin", "fallback")
		service.assetManifestCache.Store(pageID, assetManifestEntry{hash: "stale-manifest"})
		resolvedHash, err = service.resolveAssetManifestHash(pageID, prev)
		Expect(err).To(Succeed())
		Expect(assetManifestObservationFor(service.store, resolvedHash)).To(matchAssetManifestPresence(assetManifestPresent, Not(Equal(manifestHash))))
		restoreManifest()

		contentHash, err := service.store.SaveContentBlob([]byte("Preview body"))
		Expect(err).To(Succeed())
		okRev := saveRevisionFixture(service.store, pageID, newFixtureRevisionID("preview-ok"), time.Date(2026, 7, 7, 8, 20, 0, 0, time.UTC), contentHash, resolvedHash)
		missingContentRev := saveRevisionFixture(service.store, pageID, newFixtureRevisionID("preview-missing-content"), time.Date(2026, 7, 7, 8, 21, 0, 0, time.UTC), "missing-content", resolvedHash)
		missingManifestRev := saveRevisionFixture(service.store, pageID, newFixtureRevisionID("preview-missing-manifest"), time.Date(2026, 7, 7, 8, 22, 0, 0, time.UTC), contentHash, "missing-manifest")

		_, err = service.GetRevisionSnapshot(pageID, missingContentRev.ID)
		Expect(err).To(matchLocalizedRevisionErrorDetails(errCodeRevisionPreviewContentUnavailable, pageID.MetadataValue(), missingContentRev.ID.CommitID()))
		_, err = service.GetRevisionSnapshot(pageID, missingManifestRev.ID)
		Expect(err).To(matchLocalizedRevisionErrorDetails(errCodeRevisionPreviewAssetsUnavailable, pageID.MetadataValue(), missingManifestRev.ID.CommitID()))
		_, err = service.CompareRevisionSnapshots(pageID, missingContentRev.ID, okRev.ID)
		Expect(err).To(matchLocalizedRevisionErrorDetails(errCodeRevisionPreviewContentUnavailable, pageID.MetadataValue(), missingContentRev.ID.CommitID()))
		_, err = service.CompareRevisionSnapshots(pageID, okRev.ID, missingManifestRev.ID)
		Expect(err).To(matchLocalizedRevisionErrorDetails(errCodeRevisionPreviewAssetsUnavailable, pageID.MetadataValue(), missingManifestRev.ID.CommitID()))

		missingBlobManifestHash, err := service.store.SaveAssetManifest([]AssetRef{{
			Name:      "missing.png",
			SHA256:    "missing-asset-hash",
			SizeBytes: 7,
			MIMEType:  "image/png",
		}})
		Expect(err).To(Succeed())
		missingBlobRev := saveRevisionFixture(service.store, pageID, newFixtureRevisionID("preview-missing-blob"), time.Date(2026, 7, 7, 8, 23, 0, 0, time.UTC), contentHash, missingBlobManifestHash)
		_, err = service.GetRevisionAsset(pageID, missingBlobRev.ID, newFixtureAssetName("other.png"))
		Expect(err).To(matchLocalizedRevisionErrorDetails(errCodeRevisionPreviewAssetNotFound, "other.png", pageID.MetadataValue(), missingBlobRev.ID.CommitID()))
		_, err = service.GetRevisionAsset(pageID, missingBlobRev.ID, newFixtureAssetName("missing.png"))
		Expect(err).To(matchLocalizedRevisionErrorDetails(errCodeRevisionPreviewAssetBlobMissing, "missing.png", pageID.MetadataValue(), missingBlobRev.ID.CommitID()))

		service.assetManifestCache.Store(pageID, assetManifestEntry{hash: manifestHash})
		Expect(service.DeletePageData(pageID)).To(Succeed())
		Expect(assetManifestObservationFor(service.store, manifestHash)).To(matchAssetManifestPresence(assetManifestPresent, Equal(manifestHash)))
		deleteErr := errors.New("delete revisions unavailable")
		restoreDelete := setRevisionSeam(&revisionStoreDeletePageRevisions, func(*FSStore, tree.PageID) error {
			return deleteErr
		})
		Expect(service.DeletePageData(pageID)).To(MatchError(deleteErr))
		restoreDelete()

		metadataRaw := "metadata raw"
		setRevisionSeam(&revisionPagesUpdateNodeReplacingMetadataUncheckedVersion, func(*tree.TreeService, tree.UserID, tree.PageID, string, tree.Slug, *string) error {
			return nil
		})
		Expect(service.updateRestoredContent(authorID, pageID, "Preview", newFixtureSlug("preview"), &metadataRaw, true)).To(Succeed())
	})

	ginkgo.It("reports asset helper edge contracts through service seams", func() {
		pageID := newFixturePageID("asset-helper-page")
		service := NewService(revisionTempDir(), nil, nil)
		writeUnitRevisionLiveAsset(service.storageDir, pageID, "no-extension", "asset")
		writeUnitRevisionLiveAsset(service.storageDir, pageID, "z.txt", "z")
		writeUnitRevisionLiveAsset(service.storageDir, pageID, "a.txt", "a")

		refs, err := service.scanLiveAssets(pageID)
		Expect(err).To(Succeed())
		Expect(refs).To(HaveExactElements(
			HaveField("Name", "a.txt"),
			SatisfyAll(
				HaveField("Name", "no-extension"),
				HaveField("MIMEType", "application/octet-stream"),
			),
			HaveField("Name", "z.txt"),
		))

		readDirErr := errors.New("asset directory unreadable")
		restoreReadDir := setRevisionSeam(&revisionReadDir, func(string) ([]os.DirEntry, error) {
			return nil, readDirErr
		})
		_, err = service.scanLiveAssets(pageID)
		Expect(err).To(MatchError(readDirErr))
		restoreReadDir()

		openErr := errors.New("asset open failed")
		restoreOpen := setRevisionSeam(&revisionOpen, func(string) (*os.File, error) {
			return nil, openErr
		})
		_, err = buildAssetRef(filepath.Join(service.storageDir, "asset.bin"), "asset.bin")
		Expect(err).To(MatchError(openErr))
		restoreOpen()

		restoreReadDir = setRevisionSeam(&revisionReadDir, func(string) ([]os.DirEntry, error) {
			return []os.DirEntry{fakeRevisionDirEntry{name: "missing.bin"}}, nil
		})
		restoreOpen = setRevisionSeam(&revisionOpen, func(string) (*os.File, error) {
			return nil, openErr
		})
		_, err = service.scanLiveAssets(pageID)
		Expect(err).To(MatchError(openErr))
		restoreOpen()
		restoreReadDir()

		restoreReadDir = setRevisionSeam(&revisionReadDir, func(string) ([]os.DirEntry, error) {
			return []os.DirEntry{
				fakeRevisionDirEntry{name: "a.txt"},
				fakeRevisionDirEntry{name: "a.txt"},
			}, nil
		})
		refs, err = service.scanLiveAssets(pageID)
		Expect(err).To(Succeed())
		Expect(refs).To(HaveLen(2))
		restoreReadDir()

		copyErr := errors.New("asset hash failed")
		restoreCopy := setRevisionSeam(&revisionCopy, func(io.Writer, io.Reader) (int64, error) {
			return 0, copyErr
		})
		_, err = buildAssetRef(filepath.Join(service.storageDir, "assets", pageID.MetadataValue(), "a.txt"), "a.txt")
		Expect(err).To(MatchError(copyErr))
		restoreCopy()

		saveHashErr := errors.New("asset persist failed")
		restoreAssetSave := setRevisionSeam(&revisionStoreSaveAssetBlobFromPath, func(*FSStore, string) (string, int64, error) {
			return "", 0, saveHashErr
		})
		Expect(service.persistLiveAssets(pageID, refs[:1])).To(MatchError(saveHashErr))
		restoreAssetSave()

		restoreAssetSave = setRevisionSeam(&revisionStoreSaveAssetBlobFromPath, func(*FSStore, string) (string, int64, error) {
			return "wrong-hash", refs[0].SizeBytes, nil
		})
		Expect(service.persistLiveAssets(pageID, refs[:1])).To(MatchError(ErrAssetBlobHashMismatch))
		restoreAssetSave()

		restoreAssetSave = setRevisionSeam(&revisionStoreSaveAssetBlobFromPath, func(*FSStore, string) (string, int64, error) {
			return refs[0].SHA256, refs[0].SizeBytes + 1, nil
		})
		Expect(service.persistLiveAssets(pageID, refs[:1])).To(MatchError(ErrAssetBlobSizeMismatch))
		restoreAssetSave()

		removeErr := errors.New("asset reset failed")
		restoreRemoveAll := setRevisionSeam(&revisionRemoveAll, func(string) error {
			return removeErr
		})
		Expect(service.restoreAssets(pageID, refs[:1])).To(MatchError(removeErr))
		restoreRemoveAll()

		mkdirErr := errors.New("asset restore directory failed")
		restoreMkdir := setRevisionSeam(&revisionMkdirAll, func(string, os.FileMode) error {
			return mkdirErr
		})
		Expect(service.restoreAssets(pageID, refs[:1])).To(MatchError(mkdirErr))
		restoreMkdir()

		Expect(service.restoreAssets(pageID, []AssetRef{{Name: "../bad.txt", SHA256: refs[0].SHA256, SizeBytes: refs[0].SizeBytes}})).To(MatchError(ErrInvalidAssetName))
		writeUnitRevisionLiveAsset(service.storageDir, pageID, "a.txt", "a")
		_, _, err = service.store.SaveAssetBlobFromPath(filepath.Join(service.storageDir, "assets", pageID.MetadataValue(), "a.txt"))
		Expect(err).To(Succeed())
		Expect(service.restoreAssets(pageID, []AssetRef{
			{Name: "dup.txt", SHA256: refs[0].SHA256, SizeBytes: refs[0].SizeBytes},
			{Name: "dup.txt", SHA256: refs[0].SHA256, SizeBytes: refs[0].SizeBytes},
		})).To(MatchError(ErrDuplicateAssetName))

		copyRestoreErr := errors.New("asset restore copy failed")
		restoreCopyAsset := setRevisionSeam(&revisionStoreCopyAssetBlobToPath, func(*FSStore, string, int64, string) error {
			return copyRestoreErr
		})
		Expect(service.restoreAssets(pageID, refs[:1])).To(MatchError(copyRestoreErr))
		restoreCopyAsset()

		Expect(service.store.CopyAssetBlobToPath("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", refs[0].SizeBytes, filepath.Join(service.storageDir, "missing.bin"))).
			To(matchRevisionErrorCause(os.ErrNotExist))
	})

	ginkgo.It("reports content update batch and persistence failures through unit seams", func() {
		pageID := newFixturePageID("content-error-page")
		authorID := newFixtureUserID("author-user")
		page := unitRevisionPage(pageID, "Content body")
		service := NewService(revisionTempDir(), nil, nil)
		setRevisionSeam(&revisionPagesGetPage, func(*tree.TreeService, tree.PageID) (*tree.Page, error) {
			return page, nil
		})
		setRevisionSeam(&revisionPagesReadPageRaw, func(*tree.TreeService, tree.PageID) (string, error) {
			return page.Content, nil
		})

		latestErr := errors.New("content latest unavailable")
		restoreLatest := setRevisionSeam(&revisionStoreGetLatestRevision, func(*FSStore, tree.PageID) (*Revision, error) {
			return nil, latestErr
		})
		Expect(failedRevisionRecord(service.recordContentUpdateForPage(page, authorID, "content"))).
			To(haveRevisionRecordError(matchRevisionErrorCause(latestErr)))
		errs := service.RecordContentUpdates([]*tree.Page{page}, authorID, "batch")
		Expect(errs).To(ConsistOf(matchRevisionErrorCause(latestErr)))
		restoreLatest()

		parseErr := errors.New("content metadata parse failed")
		restoreParse := setRevisionSeam(&revisionParsePageDocument, func(string) (markdown.PageDocument, markdown.PageDocumentParseResult, error) {
			return markdown.PageDocument{}, markdown.PageDocumentParseResult{}, parseErr
		})
		Expect(failedRevisionRecord(service.recordContentUpdateForPage(page, authorID, "content"))).
			To(haveRevisionRecordError(matchRevisionErrorCause(parseErr)))
		restoreParse()

		manifestErr := errors.New("content manifest unavailable")
		restoreManifest := setRevisionSeam(&revisionStoreSaveAssetManifest, func(*FSStore, []AssetRef) (string, error) {
			return "", manifestErr
		})
		Expect(failedRevisionRecord(service.recordContentUpdateForPage(page, authorID, "content"))).
			To(haveRevisionRecordError(matchRevisionErrorCause(manifestErr)))
		restoreManifest()

		saveContentErr := errors.New("content blob unavailable")
		restoreContent := setRevisionSeam(&revisionStoreSaveContentBlob, func(*FSStore, []byte) (string, error) {
			return "", saveContentErr
		})
		Expect(failedRevisionRecord(service.recordContentUpdateForPage(page, authorID, "content"))).
			To(haveRevisionRecordError(matchRevisionErrorCause(saveContentErr)))
		restoreContent()

		restoreContent = setRevisionSeam(&revisionStoreSaveContentBlob, func(*FSStore, []byte) (string, error) {
			return "wrong-content-hash", nil
		})
		Expect(failedRevisionRecord(service.recordContentUpdateForPage(page, authorID, "content"))).
			To(haveRevisionRecordError(matchRevisionErrorCause(ErrContentHashMismatch)))
		restoreContent()

		idErr := errors.New("content revision id unavailable")
		restoreID := setRevisionSeam(&revisionGenerateUniqueID, func() (string, error) {
			return "", idErr
		})
		Expect(failedRevisionRecord(service.recordContentUpdateForPage(page, authorID, "content"))).
			To(haveRevisionRecordError(matchRevisionErrorCause(idErr)))
		restoreID()

		saveErr := errors.New("content revision save unavailable")
		restoreSave := setRevisionSeam(&revisionStoreSaveRevision, func(*FSStore, *Revision) error {
			return saveErr
		})
		Expect(failedRevisionRecord(service.recordContentUpdateForPage(page, authorID, "content"))).
			To(haveRevisionRecordError(matchRevisionErrorCause(saveErr)))
		restoreSave()
	})

})
