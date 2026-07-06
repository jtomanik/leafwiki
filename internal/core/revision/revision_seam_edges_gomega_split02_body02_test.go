package revision

import (
	"errors"
	. "github.com/onsi/ginkgo/v2"
	"os"
	"time"

	. "github.com/onsi/gomega"

	"github.com/perber/wiki/internal/core/markdown"
	"github.com/perber/wiki/internal/core/tree"
)

var _ = Describe("revision seam-driven failure behavior", Label("integration"), func() {

	It("rolls back restore attempts when orchestration seams fail", func() {
		service, treeService, _ := newGomegaRevisionService()
		pageID := createGomegaRevisionPage(treeService, "Page", newFixtureSlug("page"), "before")
		contentHash, err := service.store.SaveContentBlob([]byte("after"))
		Expect(err).NotTo(HaveOccurred())
		manifestHash, err := service.store.SaveAssetManifest(nil)
		Expect(err).NotTo(HaveOccurred())
		rev := saveRevisionFixture(service.store, pageID, newFixtureRevisionID("rev-restore-seams"), time.Date(2026, 6, 27, 12, 0, 0, 0, time.UTC), contentHash, manifestHash)

		restoreGet := setRevisionSeam(&revisionStoreGetRevision, func(*FSStore, tree.PageID, RevisionID) (*Revision, error) {
			return nil, errors.New("revision lookup failed")
		})
		Expect(service.RestoreRevision(pageID, rev.ID, newFixtureUserID("tester"))).To(MatchLocalizedRevisionErrorCode(errCodeRevisionRestoreFailed))
		restoreGet()

		restoreRead := setRevisionSeam(&revisionStoreReadContentBlob, func(*FSStore, string) ([]byte, error) {
			return nil, errors.New("content missing")
		})
		Expect(service.RestoreRevision(pageID, rev.ID, newFixtureUserID("tester"))).To(MatchLocalizedRevisionErrorCode(errCodeRevisionRestoreContentMissing))
		restoreRead()

		restoreLoad := setRevisionSeam(&revisionStoreLoadAssetManifest, func(*FSStore, string) ([]AssetRef, error) {
			return nil, errors.New("manifest missing")
		})
		Expect(service.RestoreRevision(pageID, rev.ID, newFixtureUserID("tester"))).To(MatchLocalizedRevisionErrorCode(errCodeRevisionRestoreAssetsMissing))
		restoreLoad()

		restoreBuild := setRevisionSeam(&revisionBuildRestoredRawContent, func(tree.PageID, string, *markdown.PageMetadata, map[string]interface{}, string) (string, bool, error) {
			return "", false, errors.New("build failed")
		})
		Expect(service.RestoreRevision(pageID, rev.ID, newFixtureUserID("tester"))).To(MatchLocalizedRevisionErrorCode(errCodeRevisionRestoreFailed))
		restoreBuild()

		restoreUpdate := setRevisionSeam(&revisionUpdateRestoredContent, func(*Service, tree.UserID, tree.PageID, string, tree.Slug, *string, bool) error {
			return errors.New("update failed")
		})
		Expect(service.RestoreRevision(pageID, rev.ID, newFixtureUserID("tester"))).To(MatchLocalizedRevisionErrorCode(errCodeRevisionRestoreFailed))
		restoreUpdate()

		restoreAssets := setRevisionSeam(&revisionRestoreAssets, func(*Service, tree.PageID, []AssetRef) error {
			return errors.New("restore assets failed")
		})
		buildCall := 0
		restoreBuild = setRevisionSeam(&revisionBuildRestoredRawContent, func(_ tree.PageID, _ string, _ *markdown.PageMetadata, _ map[string]interface{}, body string) (string, bool, error) {
			buildCall++
			if buildCall == 1 {
				return body, false, nil
			}
			return "", false, errors.New("rollback build failed")
		})
		updateCall := 0
		restoreUpdate = setRevisionSeam(&revisionUpdateRestoredContent, func(*Service, tree.UserID, tree.PageID, string, tree.Slug, *string, bool) error {
			updateCall++
			if updateCall == 1 {
				return nil
			}
			return errors.New("rollback update failed")
		})
		Expect(service.RestoreRevision(pageID, rev.ID, newFixtureUserID("tester"))).To(MatchLocalizedRevisionErrorCode(errCodeRevisionRestoreFailed))
		restoreUpdate()
		restoreBuild()
		restoreAssets()

		call := 0
		restoreAssets = setRevisionSeam(&revisionRestoreAssets, func(*Service, tree.PageID, []AssetRef) error {
			call++
			if call == 1 {
				return nil
			}
			return errors.New("rollback assets failed")
		})
		restoreRecord := setRevisionSeam(&revisionRecordRestoreRevision, func(*Service, tree.PageID, tree.UserID) error {
			return errors.New("record failed")
		})
		buildCall = 0
		restoreBuild = setRevisionSeam(&revisionBuildRestoredRawContent, func(_ tree.PageID, _ string, _ *markdown.PageMetadata, _ map[string]interface{}, body string) (string, bool, error) {
			buildCall++
			if buildCall == 1 {
				return body, false, nil
			}
			return "", false, errors.New("rollback build failed")
		})
		updateCall = 0
		restoreUpdate = setRevisionSeam(&revisionUpdateRestoredContent, func(*Service, tree.UserID, tree.PageID, string, tree.Slug, *string, bool) error {
			updateCall++
			if updateCall == 1 {
				return nil
			}
			return errors.New("rollback update failed")
		})
		Expect(service.RestoreRevision(pageID, rev.ID, newFixtureUserID("tester"))).To(MatchLocalizedRevisionErrorCode(errCodeRevisionRestoreFailed))
		restoreUpdate()
		restoreBuild()
		restoreRecord()
		restoreAssets()
	})

	It("propagates restore-revision recording failures", func() {
		service, treeService, storageDir := newGomegaRevisionService()
		pageID := createGomegaRevisionPage(treeService, "Page", newFixtureSlug("page"), "body")
		writeGomegaLiveAsset(storageDir, pageID, "asset.txt", "asset")

		contentSaveErr := errors.New("content save failed")
		restoreContent := setRevisionSeam(&revisionStoreSaveContentBlob, func(*FSStore, []byte) (string, error) {
			return "", contentSaveErr
		})
		Expect(service.recordRestoreRevision(pageID, newFixtureUserID("tester"))).To(MatchError(contentSaveErr))
		restoreContent()

		restoreContent = setRevisionSeam(&revisionStoreSaveContentBlob, func(*FSStore, []byte) (string, error) {
			return "wrong-hash", nil
		})
		Expect(service.recordRestoreRevision(pageID, newFixtureUserID("tester"))).To(MatchError(ErrContentHashMismatch))
		restoreContent()

		assetSaveErr := errors.New("asset save failed")
		restoreAsset := setRevisionSeam(&revisionStoreSaveAssetBlobFromPath, func(*FSStore, string) (string, int64, error) {
			return "", 0, assetSaveErr
		})
		Expect(service.recordRestoreRevision(pageID, newFixtureUserID("tester"))).To(MatchError(assetSaveErr))
		restoreAsset()

		manifestSaveErr := errors.New("manifest save failed")
		restoreManifest := setRevisionSeam(&revisionStoreSaveAssetManifest, func(*FSStore, []AssetRef) (string, error) {
			return "", manifestSaveErr
		})
		Expect(service.recordRestoreRevision(pageID, newFixtureUserID("tester"))).To(MatchError(manifestSaveErr))
		restoreManifest()

		restoreManifest = setRevisionSeam(&revisionStoreSaveAssetManifest, func(*FSStore, []AssetRef) (string, error) {
			return "wrong-manifest", nil
		})
		Expect(service.recordRestoreRevision(pageID, newFixtureUserID("tester"))).To(MatchError(ErrAssetManifestHashMismatch))
		restoreManifest()

		idFailedErr := errors.New("id failed")
		restoreID := setRevisionSeam(&revisionGenerateUniqueID, func() (string, error) {
			return "", idFailedErr
		})
		Expect(service.recordRestoreRevision(pageID, newFixtureUserID("tester"))).To(MatchError(idFailedErr))
		restoreID()
	})

	It("reports batch, manifest fallback, integrity, and restore failures", func() {
		service, treeService, storageDir := newGomegaRevisionService()
		pageID := createGomegaRevisionPage(treeService, "Page", newFixtureSlug("page"), "body")
		page, err := treeService.GetPage(pageID)
		Expect(err).NotTo(HaveOccurred())

		restoreProcs := setRevisionSeam(&revisionGOMAXPROCS, func(int) int {
			return 0
		})
		errs := service.RecordContentUpdates([]*tree.Page{page}, newFixtureUserID("tester"), "batch")
		Expect(errs).To(ConsistOf(BeNil()))
		restoreProcs()

		writeGomegaLiveAsset(storageDir, pageID, "asset.txt", "asset")
		service.assetManifestCache.Store(pageID, assetManifestEntry{hash: "stale-manifest"})
		restoreManifestExists := setRevisionSeam(&revisionStoreAssetManifestExists, func(*FSStore, string) bool {
			return false
		})
		persistErr := errors.New("persist failed")
		restoreAssetBlob := setRevisionSeam(&revisionStoreSaveAssetBlobFromPath, func(*FSStore, string) (string, int64, error) {
			return "", 0, persistErr
		})
		_, err = service.resolveAssetManifestHash(pageID, nil)
		Expect(err).To(MatchError(persistErr))
		restoreAssetBlob()
		restoreManifestExists()

		restoreList := setRevisionSeam(&revisionStoreListRevisions, func(*FSStore, tree.PageID) ([]*Revision, error) {
			return []*Revision{nil}, nil
		})
		issues, err := service.CheckRevisionIntegrity(pageID)
		Expect(err).NotTo(HaveOccurred())
		Expect(issues).To(BeEmpty())
		restoreList()

		unloadedTree := tree.NewTreeService(revisionTempDir())
		unloadedService := NewService(revisionTempDir(), unloadedTree, nil)
		Expect(unloadedService.RestoreRevision(newFixturePageID("unloaded"), newFixtureRevisionID("rev"), newFixtureUserID("tester"))).To(MatchLocalizedRevisionErrorCode(errCodeRevisionRestoreFailed))

		contentHash, err := service.store.SaveContentBlob([]byte("body"))
		Expect(err).NotTo(HaveOccurred())
		manifestHash, err := service.store.SaveAssetManifest(nil)
		Expect(err).NotTo(HaveOccurred())
		rev := saveRevisionFixture(service.store, pageID, newFixtureRevisionID("rev-before-state-failure"), time.Date(2026, 6, 27, 12, 30, 0, 0, time.UTC), contentHash, manifestHash)

		scanFailedErr := errors.New("scan failed")
		restoreReadDir := setRevisionSeam(&revisionReadDir, func(string) ([]os.DirEntry, error) {
			return nil, scanFailedErr
		})
		Expect(service.RestoreRevision(pageID, rev.ID, newFixtureUserID("tester"))).To(MatchLocalizedRevisionErrorCode(errCodeRevisionRestoreFailed))
		_, err = service.capturePageState(pageID, true)
		Expect(err).To(MatchError(scanFailedErr))
		restoreReadDir()

		restoreParse := setRevisionSeam(&revisionParsePageDocument, func(string) (markdown.PageDocument, markdown.PageDocumentParseResult, error) {
			return markdown.PageDocument{Body: "body"}, markdown.PageDocumentParseResult{}, nil
		})
		assetManifestHashFailedErr := errors.New("asset manifest hash failed")
		restoreJSON := setRevisionSeam(&revisionJSONMarshal, func(any) ([]byte, error) {
			return nil, assetManifestHashFailedErr
		})
		_, err = service.CapturePageState(pageID)
		Expect(err).To(MatchError(assetManifestHashFailedErr))
		restoreJSON()
		restoreParse()
	})
})
