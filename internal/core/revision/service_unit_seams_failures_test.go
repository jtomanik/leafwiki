package revision

import (
	"errors"
	"io"
	"os"
	"time"

	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/perber/wiki/internal/core/markdown"
	"github.com/perber/wiki/internal/core/tree"
)

var _ = ginkgo.Describe("revision service failure unit seams", ginkgo.Label("unit"), func() {
	ginkgo.It("classifies manifest fallback and snapshot lookup failures through service seams", func() {
		pageID := newFixturePageID("manifest-error-page")
		authorID := newFixtureUserID("author-user")
		page := unitRevisionPage(pageID, "Manifest body")
		service := NewService(revisionTempDir(), nil, nil)
		setRevisionSeam(&revisionPagesGetPage, func(*tree.TreeService, tree.PageID) (*tree.Page, error) {
			return page, nil
		})
		setRevisionSeam(&revisionPagesReadPageRaw, func(*tree.TreeService, tree.PageID) (string, error) {
			return page.Content, nil
		})

		emptyManifestHash, err := computeAssetManifestHash(nil)
		Expect(err).To(Succeed())
		prev := &Revision{
			ID:                newFixtureRevisionID("manifest-prev"),
			PageID:            pageID,
			ContentHash:       sha256HexBytes([]byte(page.Content)),
			AssetManifestHash: emptyManifestHash,
		}
		restoreLatest := setRevisionSeam(&revisionStoreGetLatestRevision, func(*FSStore, tree.PageID) (*Revision, error) {
			return prev, nil
		})
		Expect(reusedRevisionRecord(service.RecordAssetChange(pageID, authorID, "asset unchanged"))).
			To(haveRecordedRevision(matchRevisionServiceRecord(revisionServiceRecordContract{
				PageID: pageID,
			})))
		restoreLatest()

		writeUnitRevisionLiveAsset(service.storageDir, pageID, "asset.bin", "asset")
		persistErr := errors.New("manifest fallback asset persist failed")
		restoreAssetSave := setRevisionSeam(&revisionStoreSaveAssetBlobFromPath, func(*FSStore, string) (string, int64, error) {
			return "", 0, persistErr
		})
		Expect(failedRevisionRecord(service.RecordAssetChange(pageID, authorID, "asset persistence"))).
			To(haveRevisionRecordError(matchRevisionErrorCause(persistErr)))
		_, err = service.resolveAssetManifestHash(pageID, nil)
		Expect(err).To(matchRevisionErrorCause(persistErr))
		restoreAssetSave()

		manifestErr := errors.New("manifest fallback save failed")
		restoreManifest := setRevisionSeam(&revisionStoreSaveAssetManifest, func(*FSStore, []AssetRef) (string, error) {
			return "", manifestErr
		})
		_, err = service.resolveAssetManifestHash(pageID, nil)
		Expect(err).To(matchRevisionErrorCause(manifestErr))
		Expect(failedRevisionRecord(service.RecordStructureChange(pageID, authorID, "structure"))).
			To(haveRevisionRecordError(matchRevisionErrorCause(manifestErr)))
		restoreManifest()

		restoreManifest = setRevisionSeam(&revisionStoreSaveAssetManifest, func(*FSStore, []AssetRef) (string, error) {
			return "wrong-manifest-hash", nil
		})
		_, err = service.resolveAssetManifestHash(pageID, nil)
		Expect(err).To(matchRevisionErrorCause(ErrAssetManifestHashMismatch))
		restoreManifest()

		pageLookupErr := errors.New("manifest page unavailable")
		restoreGetPage := setRevisionSeam(&revisionPagesGetPage, func(*tree.TreeService, tree.PageID) (*tree.Page, error) {
			return nil, pageLookupErr
		})
		_, err = service.resolveAssetManifestHash(pageID, nil)
		Expect(err).To(matchRevisionErrorCause(pageLookupErr))
		restoreGetPage()

		revisionErr := errors.New("preview revision unavailable")
		restoreGetRevision := setRevisionSeam(&revisionStoreGetRevision, func(*FSStore, tree.PageID, RevisionID) (*Revision, error) {
			return nil, revisionErr
		})
		_, err = service.GetRevisionSnapshot(pageID, newFixtureRevisionID("missing-preview"))
		Expect(err).To(matchRevisionErrorCause(revisionErr))
		_, err = service.GetRevisionAsset(pageID, newFixtureRevisionID("missing-preview"), newFixtureAssetName("asset.bin"))
		Expect(err).To(matchRevisionErrorCause(revisionErr))
		restoreGetRevision()

		restoreGetRevision = setRevisionSeam(&revisionStoreGetRevision, func(*FSStore, tree.PageID, RevisionID) (*Revision, error) {
			return &Revision{ID: newFixtureRevisionID("manifest-preview"), PageID: pageID, AssetManifestHash: "missing-manifest"}, nil
		})
		assetManifestErr := errors.New("preview manifest unavailable")
		restoreLoadManifest := setRevisionSeam(&revisionStoreLoadAssetManifest, func(*FSStore, string) ([]AssetRef, error) {
			return nil, assetManifestErr
		})
		_, err = service.GetRevisionAsset(pageID, newFixtureRevisionID("manifest-preview"), newFixtureAssetName("asset.bin"))
		Expect(err).To(matchLocalizedRevisionErrorDetails(errCodeRevisionPreviewAssetsUnavailable, pageID.MetadataValue(), newFixtureRevisionID("manifest-preview").CommitID()))
		restoreLoadManifest()
		restoreGetRevision()
	})

	ginkgo.It("classifies integrity and capture failures without rendered error probes", func() {
		pageID := newFixturePageID("integrity-error-page")
		service := NewService(revisionTempDir(), nil, nil)
		listErr := errors.New("integrity list unavailable")
		restoreList := setRevisionSeam(&revisionStoreListRevisions, func(*FSStore, tree.PageID) ([]*Revision, error) {
			return nil, listErr
		})
		_, err := service.CheckRevisionIntegrity(pageID)
		Expect(err).To(matchRevisionErrorCause(listErr))
		restoreList()

		restoreList = setRevisionSeam(&revisionStoreListRevisions, func(*FSStore, tree.PageID) ([]*Revision, error) {
			return []*Revision{nil}, nil
		})
		issues, err := service.CheckRevisionIntegrity(pageID)
		Expect(err).To(Succeed())
		Expect(issues).To(BeEmpty())
		restoreList()

		contentHash, err := service.store.SaveContentBlob([]byte("integrity body"))
		Expect(err).To(Succeed())
		assetHash, assetSize := writeStoredAssetBlob(service.store, []byte("asset body"))
		sizeManifestHash, err := service.store.SaveAssetManifest([]AssetRef{{
			Name:      "wrong-size.txt",
			SHA256:    assetHash,
			SizeBytes: assetSize + 1,
			MIMEType:  "text/plain; charset=utf-8",
		}})
		Expect(err).To(Succeed())
		sizeRev := saveRevisionFixture(service.store, pageID, newFixtureRevisionID("integrity-size"), time.Date(2026, 7, 7, 9, 0, 0, 0, time.UTC), contentHash, sizeManifestHash)
		issues, err = service.CheckRevisionIntegrity(pageID)
		Expect(err).To(Succeed())
		Expect(issues).To(ContainElement(SatisfyAll(matchRevisionIntegrityIssue(errCodeRevisionIntegritySizeMismatch), HaveField("RevisionID", sizeRev.ID))))

		copyErr := errors.New("integrity asset copy failed")
		restoreCopy := setRevisionSeam(&revisionCopy, func(io.Writer, io.Reader) (int64, error) {
			return 0, copyErr
		})
		issues, err = service.CheckRevisionIntegrity(pageID)
		Expect(err).To(Succeed())
		Expect(issues).To(ContainElement(SatisfyAll(matchRevisionIntegrityIssue(errCodeRevisionIntegrityMissingAssetBlob), HaveField("RevisionID", sizeRev.ID))))
		restoreCopy()

		page := unitRevisionPage(pageID, "Capture body")
		setRevisionSeam(&revisionPagesGetPage, func(*tree.TreeService, tree.PageID) (*tree.Page, error) {
			return page, nil
		})
		setRevisionSeam(&revisionPagesReadPageRaw, func(*tree.TreeService, tree.PageID) (string, error) {
			return page.Content, nil
		})
		readDirErr := errors.New("capture asset scan failed")
		restoreReadDir := setRevisionSeam(&revisionReadDir, func(string) ([]os.DirEntry, error) {
			return nil, readDirErr
		})
		_, err = service.CapturePageState(pageID)
		Expect(err).To(matchRevisionErrorCause(readDirErr))
		restoreReadDir()

		marshalErr := errors.New("capture manifest marshal failed")
		restoreMarshal := setRevisionSeam(&revisionJSONMarshal, func(any) ([]byte, error) {
			return nil, marshalErr
		})
		_, err = service.CapturePageState(pageID)
		Expect(err).To(matchRevisionErrorCause(marshalErr))
		_, err = computeAssetManifestHash(nil)
		Expect(err).To(matchRevisionErrorCause(marshalErr))
		restoreReadRawMetadata := setRevisionSeam(&revisionPagesReadPageRaw, func(*tree.TreeService, tree.PageID) (string, error) {
			return renderRevisionTestMarkdown(pageID.MetadataValue(), "Guide", map[string]interface{}{"status": "draft"}, nil, "Body"), nil
		})
		state := sRevisionState(pageID)
		Expect(service.enrichStateWithExtraFrontmatter(pageID, state)).To(matchRevisionErrorCause(marshalErr))
		restoreReadRawMetadata()
		restoreMarshal()

		Expect(service.enrichStateWithExtraFrontmatter(pageID, nil)).To(matchRevisionErrorCause(ErrRevisionStateRequired))
	})

	ginkgo.It("reports restore rollback and restore-record failures through localized contracts", func() {
		pageID := newFixturePageID("restore-rollback-page")
		authorID := newFixtureUserID("author-user")
		page := unitRevisionPage(pageID, "Before body")
		service := NewService(revisionTempDir(), nil, nil)
		contentHash, err := service.store.SaveContentBlob([]byte("Restored body"))
		Expect(err).To(Succeed())
		manifestHash, err := service.store.SaveAssetManifest(nil)
		Expect(err).To(Succeed())
		rev := saveRevisionFixture(service.store, pageID, newFixtureRevisionID("restore-rollback"), time.Date(2026, 7, 7, 9, 10, 0, 0, time.UTC), contentHash, manifestHash)

		setRevisionSeam(&revisionPagesGetPage, func(*tree.TreeService, tree.PageID) (*tree.Page, error) {
			return page, nil
		})
		setRevisionSeam(&revisionPagesReadPageRaw, func(*tree.TreeService, tree.PageID) (string, error) {
			return page.Content, nil
		})

		pageLookupErr := errors.New("restore page lookup failed")
		restoreGetPage := setRevisionSeam(&revisionPagesGetPage, func(*tree.TreeService, tree.PageID) (*tree.Page, error) {
			return nil, pageLookupErr
		})
		Expect(service.RestoreRevision(pageID, rev.ID, authorID)).
			To(matchLocalizedRevisionErrorDetails(errCodeRevisionRestoreFailed, pageID.MetadataValue()))
		restoreGetPage()

		revisionLookupErr := errors.New("restore revision lookup failed")
		restoreGetRevision := setRevisionSeam(&revisionStoreGetRevision, func(*FSStore, tree.PageID, RevisionID) (*Revision, error) {
			return nil, revisionLookupErr
		})
		Expect(service.RestoreRevision(pageID, rev.ID, authorID)).
			To(matchLocalizedRevisionErrorDetails(errCodeRevisionRestoreFailed, pageID.MetadataValue()))
		restoreGetRevision()

		assetRestoreErr := errors.New("restore asset write failed")
		buildCalls := 0
		updateCalls := 0
		assetRestoreCalls := 0
		restoreBuild := setRevisionSeam(&revisionBuildRestoredRawContent, func(_ tree.PageID, _ string, _ *markdown.PageMetadata, _ map[string]interface{}, body string) (string, bool, error) {
			buildCalls++
			if buildCalls > 1 {
				return body, false, errors.New("rollback raw rebuild failed")
			}
			return body, false, nil
		})
		restoreUpdate := setRevisionSeam(&revisionUpdateRestoredContent, func(*Service, tree.UserID, tree.PageID, string, tree.Slug, *string, bool) error {
			updateCalls++
			if updateCalls > 1 {
				return errors.New("rollback content update failed")
			}
			return nil
		})
		restoreAssets := setRevisionSeam(&revisionRestoreAssets, func(*Service, tree.PageID, []AssetRef) error {
			assetRestoreCalls++
			return assetRestoreErr
		})
		Expect(service.RestoreRevision(pageID, rev.ID, authorID)).
			To(matchLocalizedRevisionErrorDetails(errCodeRevisionRestoreFailed, pageID.MetadataValue()))
		Expect(buildCalls).To(Equal(2))
		Expect(updateCalls).To(Equal(2))
		Expect(assetRestoreCalls).To(Equal(2))
		restoreAssets()
		restoreUpdate()
		restoreBuild()

		recordErr := errors.New("restore record failed")
		buildCalls = 0
		updateCalls = 0
		assetRestoreCalls = 0
		restoreBuild = setRevisionSeam(&revisionBuildRestoredRawContent, func(_ tree.PageID, _ string, _ *markdown.PageMetadata, _ map[string]interface{}, body string) (string, bool, error) {
			buildCalls++
			if buildCalls > 1 {
				return body, false, errors.New("record rollback raw rebuild failed")
			}
			return body, false, nil
		})
		restoreUpdate = setRevisionSeam(&revisionUpdateRestoredContent, func(*Service, tree.UserID, tree.PageID, string, tree.Slug, *string, bool) error {
			updateCalls++
			if updateCalls > 1 {
				return errors.New("record rollback content update failed")
			}
			return nil
		})
		restoreAssets = setRevisionSeam(&revisionRestoreAssets, func(*Service, tree.PageID, []AssetRef) error {
			assetRestoreCalls++
			if assetRestoreCalls > 1 {
				return errors.New("record rollback asset restore failed")
			}
			return nil
		})
		restoreRecord := setRevisionSeam(&revisionRecordRestoreRevision, func(*Service, tree.PageID, tree.UserID) error {
			return recordErr
		})
		Expect(service.RestoreRevision(pageID, rev.ID, authorID)).
			To(matchLocalizedRevisionErrorDetails(errCodeRevisionRestoreFailed, pageID.MetadataValue()))
		Expect(buildCalls).To(Equal(2))
		Expect(updateCalls).To(Equal(2))
		Expect(assetRestoreCalls).To(Equal(2))
		restoreRecord()
		restoreAssets()
		restoreUpdate()
		restoreBuild()
	})

	ginkgo.It("reports restore revision record persistence failures through service seams", func() {
		pageID := newFixturePageID("restore-record-page")
		authorID := newFixtureUserID("author-user")
		page := unitRevisionPage(pageID, "Restore record body")
		service := NewService(revisionTempDir(), nil, nil)
		setRevisionSeam(&revisionPagesGetPage, func(*tree.TreeService, tree.PageID) (*tree.Page, error) {
			return page, nil
		})
		setRevisionSeam(&revisionPagesReadPageRaw, func(*tree.TreeService, tree.PageID) (string, error) {
			return page.Content, nil
		})

		pageLookupErr := errors.New("restore record page unavailable")
		restoreGetPage := setRevisionSeam(&revisionPagesGetPage, func(*tree.TreeService, tree.PageID) (*tree.Page, error) {
			return nil, pageLookupErr
		})
		Expect(service.recordRestoreRevision(pageID, authorID)).To(matchRevisionErrorCause(pageLookupErr))
		restoreGetPage()

		contentErr := errors.New("restore record content save failed")
		restoreContent := setRevisionSeam(&revisionStoreSaveContentBlob, func(*FSStore, []byte) (string, error) {
			return "", contentErr
		})
		Expect(service.recordRestoreRevision(pageID, authorID)).To(matchRevisionErrorCause(contentErr))
		restoreContent()

		restoreContent = setRevisionSeam(&revisionStoreSaveContentBlob, func(*FSStore, []byte) (string, error) {
			return "wrong-content-hash", nil
		})
		Expect(service.recordRestoreRevision(pageID, authorID)).To(matchRevisionErrorCause(ErrContentHashMismatch))
		restoreContent()

		writeUnitRevisionLiveAsset(service.storageDir, pageID, "record.bin", "record")
		persistErr := errors.New("restore record asset persist failed")
		restoreAssetSave := setRevisionSeam(&revisionStoreSaveAssetBlobFromPath, func(*FSStore, string) (string, int64, error) {
			return "", 0, persistErr
		})
		Expect(service.recordRestoreRevision(pageID, authorID)).To(matchRevisionErrorCause(persistErr))
		restoreAssetSave()

		manifestErr := errors.New("restore record manifest save failed")
		restoreManifest := setRevisionSeam(&revisionStoreSaveAssetManifest, func(*FSStore, []AssetRef) (string, error) {
			return "", manifestErr
		})
		Expect(service.recordRestoreRevision(pageID, authorID)).To(matchRevisionErrorCause(manifestErr))
		restoreManifest()

		restoreManifest = setRevisionSeam(&revisionStoreSaveAssetManifest, func(*FSStore, []AssetRef) (string, error) {
			return "wrong-manifest-hash", nil
		})
		Expect(service.recordRestoreRevision(pageID, authorID)).To(matchRevisionErrorCause(ErrAssetManifestHashMismatch))
		restoreManifest()

		idErr := errors.New("restore record id failed")
		restoreID := setRevisionSeam(&revisionGenerateUniqueID, func() (string, error) {
			return "", idErr
		})
		Expect(service.recordRestoreRevision(pageID, authorID)).To(matchRevisionErrorCause(idErr))
		restoreID()

		saveErr := errors.New("restore record save failed")
		restoreSave := setRevisionSeam(&revisionStoreSaveRevision, func(*FSStore, *Revision) error {
			return saveErr
		})
		Expect(service.recordRestoreRevision(pageID, authorID)).To(matchRevisionErrorCause(saveErr))
		restoreSave()
	})
})
