package revision

import (
	"errors"
	. "github.com/onsi/ginkgo/v2"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	. "github.com/onsi/gomega"

	"github.com/perber/wiki/internal/core/markdown"
	"github.com/perber/wiki/internal/core/tree"
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
			Slug:      newFixtureSlug("page"),
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
		pageID := createGomegaRevisionPage(treeService, "Page", newFixtureSlug("page"), "body")
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
		pageID := createGomegaRevisionPage(treeService, "Page", newFixtureSlug("page"), "body")

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
})
