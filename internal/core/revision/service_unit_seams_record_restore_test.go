package revision

import (
	"errors"
	"os"
	"path/filepath"
	"time"

	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/gstruct"

	"github.com/perber/wiki/internal/core/markdown"
	"github.com/perber/wiki/internal/core/tree"
)

var _ = ginkgo.Describe("revision service unit seams", ginkgo.Label("unit"), func() {
	ginkgo.It("records content structure asset and restore revisions without wiring tree storage", func() {
		pageID := newFixturePageID("guide-page")
		authorID := newFixtureUserID("author-user")
		page := unitRevisionPage(pageID, "Initial body")
		service := NewService(revisionTempDir(), nil, nil, ServiceOptions{MaxRevisions: 10})
		setRevisionSeam(&revisionPagesGetPage, func(*tree.TreeService, tree.PageID) (*tree.Page, error) {
			return page, nil
		})
		setRevisionSeam(&revisionPagesReadPageRaw, func(*tree.TreeService, tree.PageID) (string, error) {
			return page.Content, nil
		})
		setRevisionSeam(&revisionPagesUpdateNodeUncheckedVersion, func(*tree.TreeService, tree.UserID, tree.PageID, string, tree.Slug, *string, bool) error {
			return nil
		})
		setRevisionSeam(&revisionPagesUpdateNodeReplacingMetadataUncheckedVersion, func(*tree.TreeService, tree.UserID, tree.PageID, string, tree.Slug, *string) error {
			return nil
		})

		contentRev, created, err := service.RecordContentUpdate(pageID, authorID, "content changed")
		Expect(createdRevisionRecord(contentRev, created, err)).To(haveRecordedRevision(matchRevisionServiceRecord(revisionServiceRecordContract{
			Type:    RevisionTypeContentUpdate,
			PageID:  pageID,
			Summary: "content changed",
		})))

		reused, created, err := service.RecordContentUpdate(pageID, authorID, "unchanged")
		Expect(reusedRevisionRecord(reused, created, err)).To(haveRecordedRevision(matchRevisionServiceRecord(revisionServiceRecordContract{
			Type:    RevisionTypeContentUpdate,
			PageID:  pageID,
			Summary: "content changed",
		})))

		page.Content = "Structure body"
		structureRev, created, err := service.RecordStructureChange(pageID, authorID, "structure changed")
		Expect(createdRevisionRecord(structureRev, created, err)).To(haveRecordedRevision(matchRevisionServiceRecord(revisionServiceRecordContract{
			Type:    RevisionTypeStructureUpdate,
			PageID:  pageID,
			Summary: "structure changed",
		})))

		page.Content = "Asset body"
		assetRev, created, err := service.RecordAssetChange(pageID, authorID, "asset changed")
		Expect(createdRevisionRecord(assetRev, created, err)).To(haveRecordedRevision(matchRevisionServiceRecord(revisionServiceRecordContract{
			Type:    RevisionTypeAssetUpdate,
			PageID:  pageID,
			Summary: "asset changed",
		})))

		revisions, err := service.ListRevisions(pageID)
		Expect(err).To(Succeed())
		Expect(revisions).To(HaveLen(3))

		pageOfRevisions, cursor, err := service.ListRevisionsPage(pageID, "", RevisionListLimit(2))
		Expect(err).To(Succeed())
		Expect(pageOfRevisions).To(HaveLen(2))
		Expect(cursor).NotTo(BeEmpty())

		latest, err := service.GetLatestRevision(pageID)
		Expect(err).To(Succeed())
		Expect(latest).To(matchRevisionServiceRecord(revisionServiceRecordContract{
			Type:    RevisionTypeAssetUpdate,
			PageID:  pageID,
			Summary: "asset changed",
		}))

		baseSnapshot, err := service.GetRevisionSnapshot(pageID, contentRev.ID)
		Expect(err).To(Succeed())
		Expect(baseSnapshot.Content).To(Equal("Initial body"))

		targetSnapshot, err := service.GetRevisionSnapshot(pageID, assetRev.ID)
		Expect(err).To(Succeed())
		Expect(targetSnapshot.Content).To(Equal("Asset body"))

		comparison, err := service.CompareRevisionSnapshots(pageID, contentRev.ID, assetRev.ID)
		Expect(err).To(Succeed())
		Expect(comparison).To(matchRevisionContentDelta(Equal("Initial body"), Equal("Asset body")))

		_, err = service.GetRevisionAsset(pageID, assetRev.ID, newFixtureAssetName(""))
		Expect(err).To(matchLocalizedRevisionErrorDetails(errCodeRevisionPreviewAssetInvalidName, pageID.MetadataValue(), assetRev.ID.CommitID()))
		_, err = service.GetRevisionAsset(pageID, assetRev.ID, newFixtureAssetName("missing.png"))
		Expect(err).To(matchLocalizedRevisionErrorDetails(errCodeRevisionPreviewAssetNotFound, "missing.png", pageID.MetadataValue(), assetRev.ID.CommitID()))

		setRevisionSeam(&revisionRecordRestoreRevision, func(*Service, tree.PageID, tree.UserID) error {
			return nil
		})
		Expect(service.RestoreRevision(pageID, contentRev.ID, authorID)).To(Succeed())
	})

	ginkgo.It("reports validation and page seam failures through service methods", func() {
		pageID := newFixturePageID("guide-page")
		authorID := newFixtureUserID("author-user")
		service := NewService(revisionTempDir(), nil, nil)
		setRevisionSeam(&revisionPagesGetPage, func(*tree.TreeService, tree.PageID) (*tree.Page, error) {
			return nil, tree.ErrPageNotFound
		})

		_, err := service.CapturePageState(pageID)
		Expect(err).To(Equal(tree.ErrPageNotFound))
		_, created, err := service.RecordContentUpdate(pageID, authorID, "content changed")
		Expect(failedRevisionRecord(nil, created, err)).To(haveRevisionRecordError(Equal(tree.ErrPageNotFound)))

		errs := service.RecordContentUpdates([]*tree.Page{nil}, authorID, "batch")
		Expect(errs).To(ConsistOf(matchRevisionErrorCause(ErrRevisionValidation)))

		Expect(service.DeletePageData(newFixturePageID(""))).To(Succeed())
	})

	ginkgo.It("captures live assets and restores stored revision state through service seams", func() {
		pageID := newFixturePageID("asset-page")
		authorID := newFixtureUserID("author-user")
		storageDir := revisionTempDir()
		page := unitRevisionPage(pageID, "Original body")
		service := NewService(storageDir, nil, nil, ServiceOptions{MaxRevisions: 10})
		setRevisionSeam(&revisionPagesGetPage, func(*tree.TreeService, tree.PageID) (*tree.Page, error) {
			return page, nil
		})
		setRevisionSeam(&revisionPagesReadPageRaw, func(*tree.TreeService, tree.PageID) (string, error) {
			return page.Content, nil
		})
		setRevisionSeam(&revisionPagesUpdateNodeUncheckedVersion, func(_ *tree.TreeService, _ tree.UserID, _ tree.PageID, _ string, _ tree.Slug, content *string, _ bool) error {
			if content != nil {
				page.Content = *content
			}
			return nil
		})

		writeUnitRevisionLiveAsset(storageDir, pageID, "diagram.png", "stored asset")
		Expect(os.MkdirAll(filepath.Join(storageDir, "assets", pageID.MetadataValue(), "drafts"), 0o755)).To(Succeed())
		record := createdRevisionRecord(service.RecordAssetChange(pageID, authorID, "asset captured"))
		Expect(record).To(haveRecordedRevision(matchRevisionServiceRecord(revisionServiceRecordContract{
			Type:    RevisionTypeAssetUpdate,
			PageID:  pageID,
			Summary: "asset captured",
		})))

		issues, err := service.CheckRevisionIntegrity(pageID)
		Expect(err).To(Succeed())
		Expect(issues).To(BeEmpty())

		asset, err := service.GetRevisionAsset(pageID, record.Revision.ID, newFixtureAssetName("diagram.png"))
		Expect(err).To(Succeed())
		Expect(asset).To(gstruct.PointTo(gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
			"Asset": gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
				"Name":      Equal("diagram.png"),
				"SizeBytes": Equal(int64(len("stored asset"))),
				"MIMEType":  Equal("image/png"),
			}),
		})))

		page.Content = "Changed body"
		Expect(os.RemoveAll(filepath.Join(storageDir, "assets", pageID.MetadataValue()))).To(Succeed())
		Expect(service.RestoreRevision(pageID, record.Revision.ID, authorID)).To(Succeed())
		Expect(page.Content).To(Equal("Original body"))
		restored, err := os.ReadFile(filepath.Join(storageDir, "assets", pageID.MetadataValue(), "diagram.png"))
		Expect(err).To(Succeed())
		Expect(string(restored)).To(Equal("stored asset"))
	})

	ginkgo.It("reports asset and structure recording failures through service seams", func() {
		pageID := newFixturePageID("failure-page")
		authorID := newFixtureUserID("author-user")
		page := unitRevisionPage(pageID, "Failure body")
		service := NewService(revisionTempDir(), nil, nil)
		setRevisionSeam(&revisionPagesGetPage, func(*tree.TreeService, tree.PageID) (*tree.Page, error) {
			return page, nil
		})
		setRevisionSeam(&revisionPagesReadPageRaw, func(*tree.TreeService, tree.PageID) (string, error) {
			return page.Content, nil
		})
		service.pruneAfterSave(pageID)
		Expect(newFixtureRevisionID("unit-revision")).To(matchRevisionCommitIdentity(newFixtureRevisionID("unit-revision")))

		latestErr := errors.New("latest revision unavailable")
		restoreLatest := setRevisionSeam(&revisionStoreGetLatestRevision, func(*FSStore, tree.PageID) (*Revision, error) {
			return nil, latestErr
		})
		Expect(failedRevisionRecord(service.RecordAssetChange(pageID, authorID, "asset"))).
			To(haveRevisionRecordError(MatchError(latestErr)))
		Expect(failedRevisionRecord(service.RecordStructureChange(pageID, authorID, "structure"))).
			To(haveRevisionRecordError(MatchError(latestErr)))
		restoreLatest()

		pageLookupErr := errors.New("page lookup unavailable")
		restoreGetPage := setRevisionSeam(&revisionPagesGetPage, func(*tree.TreeService, tree.PageID) (*tree.Page, error) {
			return nil, pageLookupErr
		})
		Expect(failedRevisionRecord(service.RecordAssetChange(pageID, authorID, "asset"))).
			To(haveRevisionRecordError(MatchError(pageLookupErr)))
		Expect(failedRevisionRecord(service.RecordStructureChange(pageID, authorID, "structure"))).
			To(haveRevisionRecordError(MatchError(pageLookupErr)))
		restoreGetPage()

		contentSaveErr := errors.New("content save unavailable")
		restoreContent := setRevisionSeam(&revisionStoreSaveContentBlob, func(*FSStore, []byte) (string, error) {
			return "", contentSaveErr
		})
		Expect(failedRevisionRecord(service.RecordAssetChange(pageID, authorID, "asset"))).
			To(haveRevisionRecordError(MatchError(contentSaveErr)))
		Expect(failedRevisionRecord(service.RecordStructureChange(pageID, authorID, "structure"))).
			To(haveRevisionRecordError(MatchError(contentSaveErr)))
		restoreContent()

		restoreContent = setRevisionSeam(&revisionStoreSaveContentBlob, func(*FSStore, []byte) (string, error) {
			return "wrong-content-hash", nil
		})
		Expect(failedRevisionRecord(service.RecordAssetChange(pageID, authorID, "asset"))).
			To(haveRevisionRecordError(MatchError(ErrContentHashMismatch)))
		Expect(failedRevisionRecord(service.RecordStructureChange(pageID, authorID, "structure"))).
			To(haveRevisionRecordError(MatchError(ErrContentHashMismatch)))
		restoreContent()

		manifestSaveErr := errors.New("asset manifest save unavailable")
		restoreManifest := setRevisionSeam(&revisionStoreSaveAssetManifest, func(*FSStore, []AssetRef) (string, error) {
			return "", manifestSaveErr
		})
		Expect(failedRevisionRecord(service.RecordAssetChange(pageID, authorID, "asset"))).
			To(haveRevisionRecordError(MatchError(manifestSaveErr)))
		restoreManifest()

		restoreManifest = setRevisionSeam(&revisionStoreSaveAssetManifest, func(*FSStore, []AssetRef) (string, error) {
			return "wrong-manifest-hash", nil
		})
		Expect(failedRevisionRecord(service.RecordAssetChange(pageID, authorID, "asset"))).
			To(haveRevisionRecordError(MatchError(ErrAssetManifestHashMismatch)))
		restoreManifest()

		idErr := errors.New("revision id unavailable")
		restoreID := setRevisionSeam(&revisionGenerateUniqueID, func() (string, error) {
			return "", idErr
		})
		Expect(failedRevisionRecord(service.RecordAssetChange(pageID, authorID, "asset"))).
			To(haveRevisionRecordError(MatchError(idErr)))
		Expect(failedRevisionRecord(service.RecordStructureChange(pageID, authorID, "structure"))).
			To(haveRevisionRecordError(MatchError(idErr)))
		restoreID()

		saveErr := errors.New("revision save unavailable")
		restoreSave := setRevisionSeam(&revisionStoreSaveRevision, func(*FSStore, *Revision) error {
			return saveErr
		})
		Expect(failedRevisionRecord(service.RecordAssetChange(pageID, authorID, "asset"))).
			To(haveRevisionRecordError(MatchError(saveErr)))
		Expect(failedRevisionRecord(service.RecordStructureChange(pageID, authorID, "structure"))).
			To(haveRevisionRecordError(MatchError(saveErr)))
		restoreSave()
	})

	ginkgo.It("reports restore orchestration failures as localized service contracts", func() {
		pageID := newFixturePageID("restore-page")
		authorID := newFixtureUserID("author-user")
		page := unitRevisionPage(pageID, "Before body")
		service := NewService(revisionTempDir(), nil, nil)
		contentHash, err := service.store.SaveContentBlob([]byte("Restored body"))
		Expect(err).To(Succeed())
		manifestHash, err := service.store.SaveAssetManifest(nil)
		Expect(err).To(Succeed())
		rev := saveRevisionFixture(service.store, pageID, newFixtureRevisionID("restore-revision"), time.Date(2026, 7, 7, 8, 0, 0, 0, time.UTC), contentHash, manifestHash)

		setRevisionSeam(&revisionPagesGetPage, func(*tree.TreeService, tree.PageID) (*tree.Page, error) {
			return page, nil
		})
		setRevisionSeam(&revisionPagesReadPageRaw, func(*tree.TreeService, tree.PageID) (string, error) {
			return page.Content, nil
		})
		setRevisionSeam(&revisionPagesUpdateNodeUncheckedVersion, func(_ *tree.TreeService, _ tree.UserID, _ tree.PageID, _ string, _ tree.Slug, content *string, _ bool) error {
			if content != nil {
				page.Content = *content
			}
			return nil
		})

		Expect(service.RestoreRevision(newFixturePageID(""), rev.ID, authorID)).
			To(matchLocalizedRevisionErrorDetails(errCodeRevisionRestoreInvalidPageID, ""))
		Expect(service.RestoreRevision(pageID, newFixtureRevisionID(""), authorID)).
			To(matchLocalizedRevisionErrorDetails(errCodeRevisionRestoreInvalidRevision, "", pageID.MetadataValue()))

		restoreGetPage := setRevisionSeam(&revisionPagesGetPage, func(*tree.TreeService, tree.PageID) (*tree.Page, error) {
			return nil, tree.ErrPageNotFound
		})
		Expect(service.RestoreRevision(pageID, rev.ID, authorID)).
			To(matchLocalizedRevisionErrorDetails(errCodeRevisionRestorePageNotFound, pageID.MetadataValue()))
		restoreGetPage()

		restoreGet := setRevisionSeam(&revisionStoreGetRevision, func(*FSStore, tree.PageID, RevisionID) (*Revision, error) {
			return nil, os.ErrNotExist
		})
		Expect(service.RestoreRevision(pageID, rev.ID, authorID)).
			To(matchLocalizedRevisionErrorDetails(errCodeRevisionRestoreRevisionNotFound, rev.ID.CommitID(), pageID.MetadataValue()))
		restoreGet()

		restoreRead := setRevisionSeam(&revisionStoreReadContentBlob, func(*FSStore, string) ([]byte, error) {
			return nil, os.ErrNotExist
		})
		Expect(service.RestoreRevision(pageID, rev.ID, authorID)).
			To(matchLocalizedRevisionErrorDetails(errCodeRevisionRestoreContentMissing, pageID.MetadataValue()))
		restoreRead()

		restoreManifest := setRevisionSeam(&revisionStoreLoadAssetManifest, func(*FSStore, string) ([]AssetRef, error) {
			return nil, os.ErrNotExist
		})
		Expect(service.RestoreRevision(pageID, rev.ID, authorID)).
			To(matchLocalizedRevisionErrorDetails(errCodeRevisionRestoreAssetsMissing, pageID.MetadataValue()))
		restoreManifest()

		rawReadErr := errors.New("raw page unavailable")
		restoreReadRaw := setRevisionSeam(&revisionPagesReadPageRaw, func(*tree.TreeService, tree.PageID) (string, error) {
			return "", rawReadErr
		})
		Expect(service.RestoreRevision(pageID, rev.ID, authorID)).
			To(matchLocalizedRevisionErrorDetails(errCodeRevisionRestoreFailed, pageID.MetadataValue()))
		restoreReadRaw()

		buildErr := errors.New("restore raw build failed")
		restoreBuild := setRevisionSeam(&revisionBuildRestoredRawContent, func(tree.PageID, string, *markdown.PageMetadata, map[string]interface{}, string) (string, bool, error) {
			return "", false, buildErr
		})
		Expect(service.RestoreRevision(pageID, rev.ID, authorID)).
			To(matchLocalizedRevisionErrorDetails(errCodeRevisionRestoreFailed, pageID.MetadataValue()))
		restoreBuild()

		updateErr := errors.New("restore update failed")
		restoreUpdate := setRevisionSeam(&revisionUpdateRestoredContent, func(*Service, tree.UserID, tree.PageID, string, tree.Slug, *string, bool) error {
			return updateErr
		})
		Expect(service.RestoreRevision(pageID, rev.ID, authorID)).
			To(matchLocalizedRevisionErrorDetails(errCodeRevisionRestoreFailed, pageID.MetadataValue()))
		restoreUpdate()

		restoreErr := errors.New("restore assets failed")
		restoreAssets := setRevisionSeam(&revisionRestoreAssets, func(*Service, tree.PageID, []AssetRef) error {
			return restoreErr
		})
		Expect(service.RestoreRevision(pageID, rev.ID, authorID)).
			To(matchLocalizedRevisionErrorDetails(errCodeRevisionRestoreFailed, pageID.MetadataValue()))
		Expect(page.Content).To(Equal("Before body"))
		restoreAssets()

		recordErr := errors.New("restore revision record failed")
		restoreRecord := setRevisionSeam(&revisionRecordRestoreRevision, func(*Service, tree.PageID, tree.UserID) error {
			return recordErr
		})
		Expect(service.RestoreRevision(pageID, rev.ID, authorID)).
			To(matchLocalizedRevisionErrorDetails(errCodeRevisionRestoreFailed, pageID.MetadataValue()))
		Expect(page.Content).To(Equal("Before body"))
		restoreRecord()
	})

})
