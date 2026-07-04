package revision

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/perber/wiki/internal/core/markdown"
	sharederrors "github.com/perber/wiki/internal/core/shared/errors"
	"github.com/perber/wiki/internal/core/tree"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("revision seam-driven failure behavior", Label("integration"), func() {
	It("preserves idempotent content and manifest writes while propagating write failures", func() {
		store := NewFSStore(revisionTempDir())

		restore := setRevisionSeam(&revisionWriteFileAtomic, func(path string, data []byte, perm os.FileMode) error {
			Expect(os.MkdirAll(filepath.Dir(path), 0o755)).To(Succeed())
			Expect(os.WriteFile(path, data, perm)).To(Succeed())
			return errors.New("write raced")
		})
		hash, err := store.SaveContentBlob([]byte("content-race"))
		Expect(err).NotTo(HaveOccurred())
		Expect(hash).To(Equal(sha256HexBytes([]byte("content-race"))))
		restore()

		writeFailedErr := errors.New("write failed")
		restore = setRevisionSeam(&revisionWriteFileAtomic, func(string, []byte, os.FileMode) error {
			return writeFailedErr
		})
		_, err = store.SaveContentBlob([]byte("content-failure"))
		Expect(err).To(MatchError(writeFailedErr))
		restore()

		marshalFailedErr := errors.New("marshal failed")
		restoreJSON := setRevisionSeam(&revisionJSONMarshal, func(any) ([]byte, error) {
			return nil, marshalFailedErr
		})
		_, err = store.SaveAssetManifest(nil)
		Expect(err).To(MatchError(marshalFailedErr))
		restoreJSON()

		restore = setRevisionSeam(&revisionWriteFileAtomic, func(path string, data []byte, perm os.FileMode) error {
			Expect(os.MkdirAll(filepath.Dir(path), 0o755)).To(Succeed())
			Expect(os.WriteFile(path, data, perm)).To(Succeed())
			return errors.New("manifest raced")
		})
		manifestHash, err := store.SaveAssetManifest([]AssetRef{{Name: "asset.txt", SHA256: strings.Repeat("a", 64), SizeBytes: 5}})
		Expect(err).NotTo(HaveOccurred())
		Expect(assetManifestObservationFor(store, manifestHash)).To(matchAssetManifestPresence(assetManifestPresent, Equal(manifestHash)))
		restore()

		manifestWriteFailedErr := errors.New("manifest write failed")
		restore = setRevisionSeam(&revisionWriteFileAtomic, func(string, []byte, os.FileMode) error {
			return manifestWriteFailedErr
		})
		defer restore()
		_, err = store.SaveAssetManifest([]AssetRef{{Name: "other.txt", SHA256: strings.Repeat("b", 64), SizeBytes: 5}})
		Expect(err).To(MatchError(manifestWriteFailedErr))
	})

	It("propagates asset blob save failures and treats raced writes as idempotent", func() {
		tmp := revisionTempDir()
		store := NewFSStore(tmp)
		srcPath := filepath.Join(tmp, "live.txt")
		Expect(os.WriteFile(srcPath, []byte("asset"), 0o644)).To(Succeed())

		tmpMkdirFailedErr := errors.New("tmp mkdir failed")
		restoreMkdir := setRevisionSeam(&revisionMkdirAll, func(path string, perm os.FileMode) error {
			if strings.HasSuffix(path, string(filepath.Separator)+"tmp") {
				return tmpMkdirFailedErr
			}
			return os.MkdirAll(path, perm)
		})
		_, _, err := store.SaveAssetBlobFromPath(srcPath)
		Expect(err).To(MatchError(tmpMkdirFailedErr))
		restoreMkdir()

		tempFailedErr := errors.New("temp failed")
		restoreCreate := setRevisionSeam(&revisionCreateTemp, func(string, string) (*os.File, error) {
			return nil, tempFailedErr
		})
		_, _, err = store.SaveAssetBlobFromPath(srcPath)
		Expect(err).To(MatchError(tempFailedErr))
		restoreCreate()

		copyFailedErr := errors.New("copy failed")
		restoreCopy := setRevisionSeam(&revisionCopy, func(io.Writer, io.Reader) (int64, error) {
			return 0, copyFailedErr
		})
		_, _, err = store.SaveAssetBlobFromPath(srcPath)
		Expect(err).To(MatchError(copyFailedErr))
		restoreCopy()

		chmodFailedErr := errors.New("chmod failed")
		restoreChmod := setRevisionSeam(&revisionFileChmod, func(*os.File, os.FileMode) error {
			return chmodFailedErr
		})
		_, _, err = store.SaveAssetBlobFromPath(srcPath)
		Expect(err).To(MatchError(chmodFailedErr))
		restoreChmod()

		closeFailedErr := errors.New("close failed")
		restoreClose := setRevisionSeam(&revisionFileClose, func(*os.File) error {
			return closeFailedErr
		})
		_, _, err = store.SaveAssetBlobFromPath(srcPath)
		Expect(err).To(MatchError(closeFailedErr))
		restoreClose()

		hash, size, err := store.SaveAssetBlobFromPath(srcPath)
		Expect(err).NotTo(HaveOccurred())
		hashAgain, sizeAgain, err := store.SaveAssetBlobFromPath(srcPath)
		Expect(err).NotTo(HaveOccurred())
		Expect(hashAgain).To(Equal(hash))
		Expect(sizeAgain).To(Equal(size))

		assetBlobMkdirFailedErr := errors.New("asset blob mkdir failed")
		restoreMkdir = setRevisionSeam(&revisionMkdirAll, func(path string, perm os.FileMode) error {
			if strings.Contains(path, filepath.Join("blobs", "assets")) {
				return assetBlobMkdirFailedErr
			}
			return os.MkdirAll(path, perm)
		})
		_, _, err = NewFSStore(filepath.Join(tmp, "mkdir-fail")).SaveAssetBlobFromPath(srcPath)
		Expect(err).To(MatchError(assetBlobMkdirFailedErr))
		restoreMkdir()

		restoreRename := setRevisionSeam(&revisionRename, func(src, dst string) error {
			Expect(os.MkdirAll(filepath.Dir(dst), 0o755)).To(Succeed())
			Expect(os.WriteFile(dst, []byte("asset"), 0o644)).To(Succeed())
			return errors.New("rename raced")
		})
		hash, _, err = NewFSStore(filepath.Join(tmp, "rename-race")).SaveAssetBlobFromPath(srcPath)
		Expect(err).NotTo(HaveOccurred())
		Expect(hash).To(Equal(sha256HexBytes([]byte("asset"))))
		restoreRename()

		renameFailedErr := errors.New("rename failed")
		restoreRename = setRevisionSeam(&revisionRename, func(string, string) error {
			return renameFailedErr
		})
		defer restoreRename()
		_, _, err = NewFSStore(filepath.Join(tmp, "rename-fail")).SaveAssetBlobFromPath(srcPath)
		Expect(err).To(MatchError(renameFailedErr))
	})

	It("propagates asset restore copy failures", func() {
		tmp := revisionTempDir()
		store := NewFSStore(tmp)
		hash, size := writeStoredAssetBlob(store, []byte("asset"))
		restoreDir := filepath.Join(tmp, "restore")
		Expect(os.MkdirAll(restoreDir, 0o755)).To(Succeed())

		streamFailedErr := errors.New("stream failed")
		restoreCopy := setRevisionSeam(&revisionCopy, func(io.Writer, io.Reader) (int64, error) {
			return 0, streamFailedErr
		})
		Expect(store.CopyAssetBlobToPath(hash, size, filepath.Join(restoreDir, "copy.txt"))).To(MatchError(streamFailedErr))
		restoreCopy()

		chmodFailedErr := errors.New("chmod failed")
		restoreChmod := setRevisionSeam(&revisionFileChmod, func(*os.File, os.FileMode) error {
			return chmodFailedErr
		})
		Expect(store.CopyAssetBlobToPath(hash, size, filepath.Join(restoreDir, "chmod.txt"))).To(MatchError(chmodFailedErr))
		restoreChmod()

		closeFailedErr := errors.New("close failed")
		restoreClose := setRevisionSeam(&revisionFileClose, func(*os.File) error {
			return closeFailedErr
		})
		Expect(store.CopyAssetBlobToPath(hash, size, filepath.Join(restoreDir, "close.txt"))).To(MatchError(closeFailedErr))
		restoreClose()
	})

	It("propagates revision index, lookup, prune, and delete failures", func() {
		store := NewFSStore(revisionTempDir())
		pageID := newFixturePageID("store-failure-page")
		createdAt := time.Date(2026, 6, 27, 12, 0, 0, 0, time.UTC)

		indexWriteFailedErr := errors.New("index write failed")
		restoreWrite := setRevisionSeam(&revisionWriteFileAtomic, func(path string, data []byte, perm os.FileMode) error {
			if filepath.Base(path) == revisionIndexFileName {
				return indexWriteFailedErr
			}
			return os.WriteFile(path, data, perm)
		})
		err := store.SaveRevision(&Revision{
			ID:        newFixtureRevisionID("rev-index-write-fails"),
			PageID:    pageID,
			CreatedAt: createdAt,
			Type:      RevisionTypeContentUpdate,
			Title:     "Page",
			Slug:      "page",
		})
		Expect(err).To(MatchError(indexWriteFailedErr))
		restoreWrite()

		lookupPageID := newFixturePageID("lookup-read-dir-error")
		Expect(store.saveRevisionIndex(lookupPageID, revisionIndex{})).To(Succeed())
		readDirFailedErr := errors.New("read dir failed")
		restoreReadDir := setRevisionSeam(&revisionReadDir, func(string) ([]os.DirEntry, error) {
			return nil, readDirFailedErr
		})
		_, err = store.GetRevision(lookupPageID, newFixtureRevisionID("missing"))
		Expect(err).To(MatchError(readDirFailedErr))
		restoreReadDir()

		prunePageID := newFixturePageID("prune-remove-error")
		saveRevisionFixture(store, prunePageID, newFixtureRevisionID("rev-old"), createdAt, "", "")
		saveRevisionFixture(store, prunePageID, newFixtureRevisionID("rev-new"), createdAt.Add(time.Minute), "", "")
		removeFailedErr := errors.New("remove failed")
		restoreRemove := setRevisionSeam(&revisionRemove, func(string) error {
			return removeFailedErr
		})
		Expect(store.PruneRevisions(prunePageID, 1)).To(MatchError(removeFailedErr))
		restoreRemove()

		removeAllFailedErr := errors.New("remove all failed")
		restoreRemoveAll := setRevisionSeam(&revisionRemoveAll, func(string) error {
			return removeAllFailedErr
		})
		Expect(store.DeletePageRevisions(newFixturePageID("delete-failure"))).To(MatchError(removeAllFailedErr))
		restoreRemoveAll()
	})

	It("propagates service record failures from store seams", func() {
		service, treeService, storageDir := newGomegaRevisionService()
		pageID := createGomegaRevisionPage(treeService, "Page", "page", "body")
		page, err := treeService.GetPage(pageID)
		Expect(err).NotTo(HaveOccurred())

		latestErr := errors.New("latest failed")
		restoreLatest := setRevisionSeam(&revisionStoreGetLatestRevision, func(*FSStore, tree.PageID) (*Revision, error) {
			return nil, latestErr
		})
		Expect(failedRevisionRecord(service.RecordAssetChange(pageID, newFixtureUserID("tester"), "asset"))).
			To(haveRevisionRecordError(MatchError(latestErr)))
		Expect(failedRevisionRecord(service.RecordStructureChange(pageID, newFixtureUserID("tester"), "structure"))).
			To(haveRevisionRecordError(MatchError(latestErr)))
		Expect(failedRevisionRecord(service.recordContentUpdateForPage(page, newFixtureUserID("tester"), "content"))).
			To(haveRevisionRecordError(MatchError(latestErr)))
		restoreLatest()

		contentSaveErr := errors.New("content save failed")
		restoreContent := setRevisionSeam(&revisionStoreSaveContentBlob, func(*FSStore, []byte) (string, error) {
			return "", contentSaveErr
		})
		Expect(failedRevisionRecord(service.RecordAssetChange(pageID, newFixtureUserID("tester"), "asset"))).
			To(haveRevisionRecordError(MatchError(contentSaveErr)))
		Expect(failedRevisionRecord(service.RecordStructureChange(pageID, newFixtureUserID("tester"), "structure"))).
			To(haveRevisionRecordError(MatchError(contentSaveErr)))
		Expect(failedRevisionRecord(service.recordContentUpdateForPage(page, newFixtureUserID("tester"), "content"))).
			To(haveRevisionRecordError(MatchError(contentSaveErr)))
		restoreContent()

		restoreContent = setRevisionSeam(&revisionStoreSaveContentBlob, func(*FSStore, []byte) (string, error) {
			return "wrong-hash", nil
		})
		Expect(failedRevisionRecord(service.RecordAssetChange(pageID, newFixtureUserID("tester"), "asset"))).
			To(haveRevisionRecordError(MatchError(ErrContentHashMismatch)))
		Expect(failedRevisionRecord(service.RecordStructureChange(pageID, newFixtureUserID("tester"), "structure"))).
			To(haveRevisionRecordError(MatchError(ErrContentHashMismatch)))
		Expect(failedRevisionRecord(service.recordContentUpdateForPage(page, newFixtureUserID("tester"), "content"))).
			To(haveRevisionRecordError(MatchError(ErrContentHashMismatch)))
		restoreContent()

		writeGomegaLiveAsset(storageDir, pageID, "asset.txt", "asset")
		assetBlobErr := errors.New("asset blob failed")
		restoreAssetBlob := setRevisionSeam(&revisionStoreSaveAssetBlobFromPath, func(*FSStore, string) (string, int64, error) {
			return "", 0, assetBlobErr
		})
		Expect(failedRevisionRecord(service.RecordAssetChange(pageID, newFixtureUserID("tester"), "asset"))).
			To(haveRevisionRecordError(MatchError(assetBlobErr)))
		restoreAssetBlob()

		manifestSaveErr := errors.New("manifest save failed")
		restoreManifest := setRevisionSeam(&revisionStoreSaveAssetManifest, func(*FSStore, []AssetRef) (string, error) {
			return "", manifestSaveErr
		})
		Expect(failedRevisionRecord(service.RecordAssetChange(pageID, newFixtureUserID("tester"), "asset"))).
			To(haveRevisionRecordError(MatchError(manifestSaveErr)))
		service.assetManifestCache.Delete(pageID)
		_, err = service.resolveAssetManifestHash(pageID, nil)
		Expect(err).To(MatchError(manifestSaveErr))
		restoreManifest()

		restoreManifest = setRevisionSeam(&revisionStoreSaveAssetManifest, func(*FSStore, []AssetRef) (string, error) {
			return "wrong-manifest", nil
		})
		Expect(failedRevisionRecord(service.RecordAssetChange(pageID, newFixtureUserID("tester"), "asset"))).
			To(haveRevisionRecordError(MatchError(ErrAssetManifestHashMismatch)))
		service.assetManifestCache.Delete(pageID)
		_, err = service.resolveAssetManifestHash(pageID, nil)
		Expect(err).To(MatchError(ErrAssetManifestHashMismatch))
		restoreManifest()

		idFailedErr := errors.New("id failed")
		restoreID := setRevisionSeam(&revisionGenerateUniqueID, func() (string, error) {
			return "", idFailedErr
		})
		Expect(failedRevisionRecord(service.RecordAssetChange(pageID, newFixtureUserID("tester"), "asset"))).
			To(haveRevisionRecordError(MatchError(idFailedErr)))
		Expect(failedRevisionRecord(service.RecordStructureChange(pageID, newFixtureUserID("tester"), "structure"))).
			To(haveRevisionRecordError(MatchError(idFailedErr)))
		Expect(failedRevisionRecord(service.recordContentUpdateForPage(page, newFixtureUserID("tester"), "content"))).
			To(haveRevisionRecordError(MatchError(idFailedErr)))
		_, err = service.newRevision(RevisionTypeContentUpdate, service.revisionStateFromPage(page), newFixtureUserID("tester"), "summary", "")
		Expect(err).To(MatchError(idFailedErr))
		restoreID()

		saveRevisionErr := errors.New("save revision failed")
		restoreSaveRevision := setRevisionSeam(&revisionStoreSaveRevision, func(*FSStore, *Revision) error {
			return saveRevisionErr
		})
		Expect(failedRevisionRecord(service.RecordAssetChange(pageID, newFixtureUserID("tester"), "asset"))).
			To(haveRevisionRecordError(MatchError(saveRevisionErr)))
		Expect(failedRevisionRecord(service.RecordStructureChange(pageID, newFixtureUserID("tester"), "structure"))).
			To(haveRevisionRecordError(MatchError(saveRevisionErr)))
		Expect(failedRevisionRecord(service.recordContentUpdateForPage(page, newFixtureUserID("tester"), "content"))).
			To(haveRevisionRecordError(MatchError(saveRevisionErr)))
		restoreSaveRevision()
	})

	It("reports integrity, delete, parser, sort, and restore failures", func() {
		service, treeService, _ := newGomegaRevisionService()
		pageID := createGomegaRevisionPage(treeService, "Page", "page", "body")

		deleteRevisionsErr := errors.New("delete revisions failed")
		restoreDelete := setRevisionSeam(&revisionStoreDeletePageRevisions, func(*FSStore, tree.PageID) error {
			return deleteRevisionsErr
		})
		Expect(service.DeletePageData(pageID)).To(MatchError(deleteRevisionsErr))
		restoreDelete()

		listErr := errors.New("list failed")
		restoreList := setRevisionSeam(&revisionStoreListRevisions, func(*FSStore, tree.PageID) ([]*Revision, error) {
			return nil, listErr
		})
		_, err := service.CheckRevisionIntegrity(pageID)
		Expect(err).To(MatchError(listErr))
		restoreList()

		restoreOpenContent := setRevisionSeam(&revisionStoreOpenContentBlob, func(*FSStore, string) (io.ReadCloser, error) {
			return nil, errors.New("missing content")
		})
		restoreManifest := setRevisionSeam(&revisionStoreLoadAssetManifest, func(*FSStore, string) ([]AssetRef, error) {
			return nil, errors.New("missing manifest")
		})
		restoreList = setRevisionSeam(&revisionStoreListRevisions, func(*FSStore, tree.PageID) ([]*Revision, error) {
			return []*Revision{{ID: newFixtureRevisionID("rev-integrity"), PageID: pageID, ContentHash: "content", AssetManifestHash: "manifest"}}, nil
		})
		issues, err := service.CheckRevisionIntegrity(pageID)
		Expect(err).NotTo(HaveOccurred())
		Expect(revisionIssueCodes(issues)).To(ContainElements(errCodeRevisionIntegrityMissingContent, errCodeRevisionIntegrityMissingManifest))
		restoreList()
		restoreManifest()
		restoreOpenContent()

		restoreCopy := setRevisionSeam(&revisionCopy, func(io.Writer, io.Reader) (int64, error) {
			return 0, errors.New("copy failed")
		})
		hash, _ := writeStoredAssetBlob(service.store, []byte("asset"))
		manifestHash, err := service.store.SaveAssetManifest([]AssetRef{{Name: "asset.txt", SHA256: hash, SizeBytes: int64(len("asset"))}})
		Expect(err).NotTo(HaveOccurred())
		restoreList = setRevisionSeam(&revisionStoreListRevisions, func(*FSStore, tree.PageID) ([]*Revision, error) {
			return []*Revision{{ID: newFixtureRevisionID("rev-copy"), PageID: pageID, AssetManifestHash: manifestHash}}, nil
		})
		issues, err = service.CheckRevisionIntegrity(pageID)
		Expect(err).NotTo(HaveOccurred())
		Expect(revisionIssueCodes(issues)).To(ContainElement(errCodeRevisionIntegrityMissingAssetBlob))
		restoreList()
		restoreCopy()

		parseErr := errors.New("parse failed")
		restoreParse := setRevisionSeam(&revisionParsePageDocument, func(string) (markdown.PageDocument, markdown.PageDocumentParseResult, error) {
			return markdown.PageDocument{}, markdown.PageDocumentParseResult{}, parseErr
		})
		Expect(service.enrichStateWithExtraFrontmatter(pageID, &RevisionState{})).To(MatchError(parseErr))
		restoreParse()

		restoreParse = setRevisionSeam(&revisionParsePageDocument, func(string) (markdown.PageDocument, markdown.PageDocumentParseResult, error) {
			return markdown.PageDocument{Body: "body"}, markdown.PageDocumentParseResult{}, nil
		})
		state := &RevisionState{}
		Expect(service.enrichStateWithExtraFrontmatter(pageID, state)).To(Succeed())
		Expect(state.PageMetadata).To(BeNil())
		restoreParse()

		manifestHashFailedErr := errors.New("manifest hash failed")
		restoreJSON := setRevisionSeam(&revisionJSONMarshal, func(any) ([]byte, error) {
			return nil, manifestHashFailedErr
		})
		_, err = service.CapturePageState(pageID)
		Expect(err).To(MatchError(manifestHashFailedErr))
		_, err = computeAssetManifestHash(nil)
		Expect(err).To(MatchError(manifestHashFailedErr))
		restoreJSON()

		assetDir := service.liveAssetDir(newFixturePageID("duplicate-dir-entries"))
		Expect(os.MkdirAll(assetDir, 0o755)).To(Succeed())
		Expect(os.WriteFile(filepath.Join(assetDir, "asset.txt"), []byte("asset"), 0o644)).To(Succeed())
		restoreReadDir := setRevisionSeam(&revisionReadDir, func(string) ([]os.DirEntry, error) {
			return []os.DirEntry{fakeRevisionDirEntry{name: "asset.txt"}, fakeRevisionDirEntry{name: "asset.txt"}}, nil
		})
		assets, err := service.scanLiveAssets(newFixturePageID("duplicate-dir-entries"))
		Expect(err).NotTo(HaveOccurred())
		Expect(assets).To(HaveLen(2))
		restoreReadDir()

		resetFailedErr := errors.New("reset failed")
		restoreRemoveAll := setRevisionSeam(&revisionRemoveAll, func(string) error {
			return resetFailedErr
		})
		Expect(service.restoreAssets(pageID, nil)).To(MatchError(resetFailedErr))
		restoreRemoveAll()

		mkdirFailedErr := errors.New("mkdir failed")
		restoreMkdir := setRevisionSeam(&revisionMkdirAll, func(string, os.FileMode) error {
			return mkdirFailedErr
		})
		Expect(service.restoreAssets(pageID, []AssetRef{{Name: "asset.txt", SHA256: strings.Repeat("f", 64)}})).To(MatchError(mkdirFailedErr))
		restoreMkdir()
	})

	It("rolls back restore attempts when orchestration seams fail", func() {
		service, treeService, _ := newGomegaRevisionService()
		pageID := createGomegaRevisionPage(treeService, "Page", "page", "before")
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
		pageID := createGomegaRevisionPage(treeService, "Page", "page", "body")
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
		pageID := createGomegaRevisionPage(treeService, "Page", "page", "body")
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

func setRevisionSeam[T any](target *T, replacement T) func() {
	GinkgoHelper()

	original := *target
	*target = replacement
	restored := false
	restore := func() {
		if restored {
			return
		}
		*target = original
		restored = true
	}
	DeferCleanup(restore)
	return restore
}

func revisionIssueCodes(issues []RevisionIntegrityIssue) []sharederrors.ErrorCode {
	GinkgoHelper()

	codes := make([]sharederrors.ErrorCode, 0, len(issues))
	for _, issue := range issues {
		codes = append(codes, issue.Code)
	}
	return codes
}

type fakeRevisionDirEntry struct {
	name string
}

func (e fakeRevisionDirEntry) Name() string {
	return e.name
}

func (fakeRevisionDirEntry) IsDir() bool {
	return false
}

func (fakeRevisionDirEntry) Type() os.FileMode {
	return 0
}

func (fakeRevisionDirEntry) Info() (os.FileInfo, error) {
	return nil, errors.New("not implemented")
}
