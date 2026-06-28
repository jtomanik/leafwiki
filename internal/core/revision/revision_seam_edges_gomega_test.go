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

var _ = Describe("revision seam-driven edge coverage", func() {
	It("covers content and manifest idempotency races and write failures", func() {
		store := NewFSStore(GinkgoT().TempDir())

		restore := setRevisionSeam(&revisionWriteFileAtomic, func(path string, data []byte, perm os.FileMode) error {
			Expect(os.MkdirAll(filepath.Dir(path), 0o755)).To(Succeed())
			Expect(os.WriteFile(path, data, perm)).To(Succeed())
			return errors.New("write raced")
		})
		hash, err := store.SaveContentBlob([]byte("content-race"))
		Expect(err).NotTo(HaveOccurred())
		Expect(hash).To(Equal(sha256HexBytes([]byte("content-race"))))
		restore()

		restore = setRevisionSeam(&revisionWriteFileAtomic, func(string, []byte, os.FileMode) error {
			return errors.New("write failed")
		})
		_, err = store.SaveContentBlob([]byte("content-failure"))
		Expect(err).To(MatchError(ContainSubstring("write content blob")))
		restore()

		restoreJSON := setRevisionSeam(&revisionJSONMarshal, func(any) ([]byte, error) {
			return nil, errors.New("marshal failed")
		})
		_, err = store.SaveAssetManifest(nil)
		Expect(err).To(MatchError(ContainSubstring("marshal asset manifest")))
		restoreJSON()

		restore = setRevisionSeam(&revisionWriteFileAtomic, func(path string, data []byte, perm os.FileMode) error {
			Expect(os.MkdirAll(filepath.Dir(path), 0o755)).To(Succeed())
			Expect(os.WriteFile(path, data, perm)).To(Succeed())
			return errors.New("manifest raced")
		})
		manifestHash, err := store.SaveAssetManifest([]AssetRef{{Name: "asset.txt", SHA256: strings.Repeat("a", 64), SizeBytes: 5}})
		Expect(err).NotTo(HaveOccurred())
		Expect(store.AssetManifestExists(manifestHash)).To(BeTrue())
		restore()

		restore = setRevisionSeam(&revisionWriteFileAtomic, func(string, []byte, os.FileMode) error {
			return errors.New("manifest write failed")
		})
		_, err = store.SaveAssetManifest([]AssetRef{{Name: "other.txt", SHA256: strings.Repeat("b", 64), SizeBytes: 5}})
		Expect(err).To(MatchError(ContainSubstring("write asset manifest")))
	})

	It("covers asset blob save failure and race paths", func() {
		tmp := GinkgoT().TempDir()
		store := NewFSStore(tmp)
		srcPath := filepath.Join(tmp, "live.txt")
		Expect(os.WriteFile(srcPath, []byte("asset"), 0o644)).To(Succeed())

		restoreMkdir := setRevisionSeam(&revisionMkdirAll, func(path string, perm os.FileMode) error {
			if strings.HasSuffix(path, string(filepath.Separator)+"tmp") {
				return errors.New("tmp mkdir failed")
			}
			return os.MkdirAll(path, perm)
		})
		_, _, err := store.SaveAssetBlobFromPath(srcPath)
		Expect(err).To(MatchError(ContainSubstring("ensure tmp dir")))
		restoreMkdir()

		restoreCreate := setRevisionSeam(&revisionCreateTemp, func(string, string) (*os.File, error) {
			return nil, errors.New("temp failed")
		})
		_, _, err = store.SaveAssetBlobFromPath(srcPath)
		Expect(err).To(MatchError(ContainSubstring("create temp asset blob")))
		restoreCreate()

		restoreCopy := setRevisionSeam(&revisionCopy, func(io.Writer, io.Reader) (int64, error) {
			return 0, errors.New("copy failed")
		})
		_, _, err = store.SaveAssetBlobFromPath(srcPath)
		Expect(err).To(MatchError(ContainSubstring("copy asset to temp blob")))
		restoreCopy()

		restoreChmod := setRevisionSeam(&revisionFileChmod, func(*os.File, os.FileMode) error {
			return errors.New("chmod failed")
		})
		_, _, err = store.SaveAssetBlobFromPath(srcPath)
		Expect(err).To(MatchError(ContainSubstring("chmod temp asset blob")))
		restoreChmod()

		restoreClose := setRevisionSeam(&revisionFileClose, func(*os.File) error {
			return errors.New("close failed")
		})
		_, _, err = store.SaveAssetBlobFromPath(srcPath)
		Expect(err).To(MatchError(ContainSubstring("close temp asset blob")))
		restoreClose()

		hash, size, err := store.SaveAssetBlobFromPath(srcPath)
		Expect(err).NotTo(HaveOccurred())
		hashAgain, sizeAgain, err := store.SaveAssetBlobFromPath(srcPath)
		Expect(err).NotTo(HaveOccurred())
		Expect(hashAgain).To(Equal(hash))
		Expect(sizeAgain).To(Equal(size))

		restoreMkdir = setRevisionSeam(&revisionMkdirAll, func(path string, perm os.FileMode) error {
			if strings.Contains(path, filepath.Join("blobs", "assets")) {
				return errors.New("asset blob mkdir failed")
			}
			return os.MkdirAll(path, perm)
		})
		_, _, err = NewFSStore(filepath.Join(tmp, "mkdir-fail")).SaveAssetBlobFromPath(srcPath)
		Expect(err).To(MatchError(ContainSubstring("ensure asset blob dir")))
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

		restoreRename = setRevisionSeam(&revisionRename, func(string, string) error {
			return errors.New("rename failed")
		})
		_, _, err = NewFSStore(filepath.Join(tmp, "rename-fail")).SaveAssetBlobFromPath(srcPath)
		Expect(err).To(MatchError(ContainSubstring("move asset blob into place")))
	})

	It("covers asset restore copy failures", func() {
		tmp := GinkgoT().TempDir()
		store := NewFSStore(tmp)
		hash, size := writeStoredAssetBlob(store, []byte("asset"))
		restoreDir := filepath.Join(tmp, "restore")
		Expect(os.MkdirAll(restoreDir, 0o755)).To(Succeed())

		restoreCopy := setRevisionSeam(&revisionCopy, func(io.Writer, io.Reader) (int64, error) {
			return 0, errors.New("stream failed")
		})
		Expect(store.CopyAssetBlobToPath(hash, size, filepath.Join(restoreDir, "copy.txt"))).To(MatchError(ContainSubstring("stream asset blob")))
		restoreCopy()

		restoreChmod := setRevisionSeam(&revisionFileChmod, func(*os.File, os.FileMode) error {
			return errors.New("chmod failed")
		})
		Expect(store.CopyAssetBlobToPath(hash, size, filepath.Join(restoreDir, "chmod.txt"))).To(MatchError(ContainSubstring("chmod restored asset")))
		restoreChmod()

		restoreClose := setRevisionSeam(&revisionFileClose, func(*os.File) error {
			return errors.New("close failed")
		})
		Expect(store.CopyAssetBlobToPath(hash, size, filepath.Join(restoreDir, "close.txt"))).To(MatchError(ContainSubstring("close temp restore file")))
		restoreClose()
	})

	It("covers revision index, lookup, prune, and delete failure exits", func() {
		store := NewFSStore(GinkgoT().TempDir())
		pageID := newFixturePageID("store-failure-page")
		createdAt := time.Date(2026, 6, 27, 12, 0, 0, 0, time.UTC)

		restoreWrite := setRevisionSeam(&revisionWriteFileAtomic, func(path string, data []byte, perm os.FileMode) error {
			if filepath.Base(path) == revisionIndexFileName {
				return errors.New("index write failed")
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
		Expect(err).To(MatchError(ContainSubstring("write revision index")))
		restoreWrite()

		lookupPageID := newFixturePageID("lookup-read-dir-error")
		Expect(store.saveRevisionIndex(lookupPageID, revisionIndex{})).To(Succeed())
		restoreReadDir := setRevisionSeam(&revisionReadDir, func(string) ([]os.DirEntry, error) {
			return nil, errors.New("read dir failed")
		})
		_, err = store.GetRevision(lookupPageID, newFixtureRevisionID("missing"))
		Expect(err).To(MatchError(ContainSubstring("read revisions dir")))
		restoreReadDir()

		prunePageID := newFixturePageID("prune-remove-error")
		saveRevisionFixture(store, prunePageID, newFixtureRevisionID("rev-old"), createdAt, "", "")
		saveRevisionFixture(store, prunePageID, newFixtureRevisionID("rev-new"), createdAt.Add(time.Minute), "", "")
		restoreRemove := setRevisionSeam(&revisionRemove, func(string) error {
			return errors.New("remove failed")
		})
		Expect(store.PruneRevisions(prunePageID, 1)).To(MatchError(ContainSubstring("delete revision file")))
		restoreRemove()

		restoreRemoveAll := setRevisionSeam(&revisionRemoveAll, func(string) error {
			return errors.New("remove all failed")
		})
		Expect(store.DeletePageRevisions(newFixturePageID("delete-failure"))).To(MatchError(ContainSubstring("delete page revisions")))
		restoreRemoveAll()
	})

	It("covers service record failure branches through store seams", func() {
		service, treeService, storageDir := newGomegaRevisionService()
		pageID := createGomegaRevisionPage(treeService, "Page", "page", "body")
		page, err := treeService.GetPage(pageID)
		Expect(err).NotTo(HaveOccurred())

		restoreLatest := setRevisionSeam(&revisionStoreGetLatestRevision, func(*FSStore, tree.PageID) (*Revision, error) {
			return nil, errors.New("latest failed")
		})
		_, created, err := service.RecordAssetChange(pageID, newFixtureUserID("tester"), "asset")
		Expect(created).To(BeFalse())
		Expect(err).To(MatchError("latest failed"))
		_, created, err = service.RecordStructureChange(pageID, newFixtureUserID("tester"), "structure")
		Expect(created).To(BeFalse())
		Expect(err).To(MatchError("latest failed"))
		_, created, err = service.recordContentUpdateForPage(page, newFixtureUserID("tester"), "content")
		Expect(created).To(BeFalse())
		Expect(err).To(MatchError("latest failed"))
		restoreLatest()

		restoreContent := setRevisionSeam(&revisionStoreSaveContentBlob, func(*FSStore, []byte) (string, error) {
			return "", errors.New("content save failed")
		})
		_, _, err = service.RecordAssetChange(pageID, newFixtureUserID("tester"), "asset")
		Expect(err).To(MatchError("content save failed"))
		_, _, err = service.RecordStructureChange(pageID, newFixtureUserID("tester"), "structure")
		Expect(err).To(MatchError("content save failed"))
		_, _, err = service.recordContentUpdateForPage(page, newFixtureUserID("tester"), "content")
		Expect(err).To(MatchError("content save failed"))
		restoreContent()

		restoreContent = setRevisionSeam(&revisionStoreSaveContentBlob, func(*FSStore, []byte) (string, error) {
			return "wrong-hash", nil
		})
		_, _, err = service.RecordAssetChange(pageID, newFixtureUserID("tester"), "asset")
		Expect(err).To(MatchError(ContainSubstring("content hash mismatch")))
		_, _, err = service.RecordStructureChange(pageID, newFixtureUserID("tester"), "structure")
		Expect(err).To(MatchError(ContainSubstring("content hash mismatch")))
		_, _, err = service.recordContentUpdateForPage(page, newFixtureUserID("tester"), "content")
		Expect(err).To(MatchError(ContainSubstring("content hash mismatch")))
		restoreContent()

		writeGomegaLiveAsset(storageDir, pageID, "asset.txt", "asset")
		restoreAssetBlob := setRevisionSeam(&revisionStoreSaveAssetBlobFromPath, func(*FSStore, string) (string, int64, error) {
			return "", 0, errors.New("asset blob failed")
		})
		_, _, err = service.RecordAssetChange(pageID, newFixtureUserID("tester"), "asset")
		Expect(err).To(MatchError("asset blob failed"))
		restoreAssetBlob()

		restoreManifest := setRevisionSeam(&revisionStoreSaveAssetManifest, func(*FSStore, []AssetRef) (string, error) {
			return "", errors.New("manifest save failed")
		})
		_, _, err = service.RecordAssetChange(pageID, newFixtureUserID("tester"), "asset")
		Expect(err).To(MatchError("manifest save failed"))
		service.assetManifestCache.Delete(pageID)
		_, err = service.resolveAssetManifestHash(pageID, nil)
		Expect(err).To(MatchError("manifest save failed"))
		restoreManifest()

		restoreManifest = setRevisionSeam(&revisionStoreSaveAssetManifest, func(*FSStore, []AssetRef) (string, error) {
			return "wrong-manifest", nil
		})
		_, _, err = service.RecordAssetChange(pageID, newFixtureUserID("tester"), "asset")
		Expect(err).To(MatchError(ContainSubstring("asset manifest hash mismatch")))
		service.assetManifestCache.Delete(pageID)
		_, err = service.resolveAssetManifestHash(pageID, nil)
		Expect(err).To(MatchError(ContainSubstring("asset manifest hash mismatch")))
		restoreManifest()

		restoreID := setRevisionSeam(&revisionGenerateUniqueID, func() (string, error) {
			return "", errors.New("id failed")
		})
		_, _, err = service.RecordAssetChange(pageID, newFixtureUserID("tester"), "asset")
		Expect(err).To(MatchError(ContainSubstring("generate revision id")))
		_, _, err = service.RecordStructureChange(pageID, newFixtureUserID("tester"), "structure")
		Expect(err).To(MatchError(ContainSubstring("generate revision id")))
		_, _, err = service.recordContentUpdateForPage(page, newFixtureUserID("tester"), "content")
		Expect(err).To(MatchError(ContainSubstring("generate revision id")))
		_, err = service.newRevision(RevisionTypeContentUpdate, service.revisionStateFromPage(page), newFixtureUserID("tester"), "summary", "")
		Expect(err).To(MatchError(ContainSubstring("generate revision id")))
		restoreID()

		restoreSaveRevision := setRevisionSeam(&revisionStoreSaveRevision, func(*FSStore, *Revision) error {
			return errors.New("save revision failed")
		})
		_, _, err = service.RecordAssetChange(pageID, newFixtureUserID("tester"), "asset")
		Expect(err).To(MatchError("save revision failed"))
		_, _, err = service.RecordStructureChange(pageID, newFixtureUserID("tester"), "structure")
		Expect(err).To(MatchError("save revision failed"))
		_, _, err = service.recordContentUpdateForPage(page, newFixtureUserID("tester"), "content")
		Expect(err).To(MatchError("save revision failed"))
		restoreSaveRevision()
	})

	It("covers service integrity, delete, parser, sort, and restore failure branches", func() {
		service, treeService, _ := newGomegaRevisionService()
		pageID := createGomegaRevisionPage(treeService, "Page", "page", "body")

		restoreDelete := setRevisionSeam(&revisionStoreDeletePageRevisions, func(*FSStore, tree.PageID) error {
			return errors.New("delete revisions failed")
		})
		Expect(service.DeletePageData(pageID)).To(MatchError("delete revisions failed"))
		restoreDelete()

		restoreList := setRevisionSeam(&revisionStoreListRevisions, func(*FSStore, tree.PageID) ([]*Revision, error) {
			return nil, errors.New("list failed")
		})
		_, err := service.CheckRevisionIntegrity(pageID)
		Expect(err).To(MatchError("list failed"))
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

		restoreParse := setRevisionSeam(&revisionParsePageDocument, func(string) (markdown.PageDocument, markdown.PageDocumentParseResult, error) {
			return markdown.PageDocument{}, markdown.PageDocumentParseResult{}, errors.New("parse failed")
		})
		Expect(service.enrichStateWithExtraFrontmatter(pageID, &RevisionState{})).To(MatchError("parse failed"))
		restoreParse()

		restoreParse = setRevisionSeam(&revisionParsePageDocument, func(string) (markdown.PageDocument, markdown.PageDocumentParseResult, error) {
			return markdown.PageDocument{Body: "body"}, markdown.PageDocumentParseResult{}, nil
		})
		state := &RevisionState{}
		Expect(service.enrichStateWithExtraFrontmatter(pageID, state)).To(Succeed())
		Expect(state.PageMetadata).To(BeNil())
		restoreParse()

		restoreJSON := setRevisionSeam(&revisionJSONMarshal, func(any) ([]byte, error) {
			return nil, errors.New("manifest hash failed")
		})
		_, err = service.CapturePageState(pageID)
		Expect(err).To(MatchError(ContainSubstring("marshal page metadata")))
		_, err = computeAssetManifestHash(nil)
		Expect(err).To(MatchError(ContainSubstring("marshal asset manifest for hash")))
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

		restoreRemoveAll := setRevisionSeam(&revisionRemoveAll, func(string) error {
			return errors.New("reset failed")
		})
		Expect(service.restoreAssets(pageID, nil)).To(MatchError(ContainSubstring("reset live asset dir")))
		restoreRemoveAll()

		restoreMkdir := setRevisionSeam(&revisionMkdirAll, func(string, os.FileMode) error {
			return errors.New("mkdir failed")
		})
		Expect(service.restoreAssets(pageID, []AssetRef{{Name: "asset.txt", SHA256: strings.Repeat("f", 64)}})).To(MatchError(ContainSubstring("ensure live asset dir")))
		restoreMkdir()
	})

	It("covers restore rollback paths through orchestration seams", func() {
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
		expectLocalizedRevisionErrorCode(service.RestoreRevision(pageID, rev.ID, newFixtureUserID("tester")), "revision_restore_failed")
		restoreGet()

		restoreRead := setRevisionSeam(&revisionStoreReadContentBlob, func(*FSStore, string) ([]byte, error) {
			return nil, errors.New("content missing")
		})
		expectLocalizedRevisionErrorCode(service.RestoreRevision(pageID, rev.ID, newFixtureUserID("tester")), "revision_restore_content_missing")
		restoreRead()

		restoreLoad := setRevisionSeam(&revisionStoreLoadAssetManifest, func(*FSStore, string) ([]AssetRef, error) {
			return nil, errors.New("manifest missing")
		})
		expectLocalizedRevisionErrorCode(service.RestoreRevision(pageID, rev.ID, newFixtureUserID("tester")), "revision_restore_assets_missing")
		restoreLoad()

		restoreBuild := setRevisionSeam(&revisionBuildRestoredRawContent, func(tree.PageID, string, *markdown.PageMetadata, map[string]interface{}, string) (string, bool, error) {
			return "", false, errors.New("build failed")
		})
		expectLocalizedRevisionErrorCode(service.RestoreRevision(pageID, rev.ID, newFixtureUserID("tester")), "revision_restore_failed")
		restoreBuild()

		restoreUpdate := setRevisionSeam(&revisionUpdateRestoredContent, func(*Service, tree.UserID, tree.PageID, string, tree.Slug, *string, bool) error {
			return errors.New("update failed")
		})
		expectLocalizedRevisionErrorCode(service.RestoreRevision(pageID, rev.ID, newFixtureUserID("tester")), "revision_restore_failed")
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
		expectLocalizedRevisionErrorCode(service.RestoreRevision(pageID, rev.ID, newFixtureUserID("tester")), "revision_restore_failed")
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
		expectLocalizedRevisionErrorCode(service.RestoreRevision(pageID, rev.ID, newFixtureUserID("tester")), "revision_restore_failed")
		restoreUpdate()
		restoreBuild()
		restoreRecord()
		restoreAssets()
	})

	It("covers restore-revision recording failures", func() {
		service, treeService, storageDir := newGomegaRevisionService()
		pageID := createGomegaRevisionPage(treeService, "Page", "page", "body")
		writeGomegaLiveAsset(storageDir, pageID, "asset.txt", "asset")

		restoreContent := setRevisionSeam(&revisionStoreSaveContentBlob, func(*FSStore, []byte) (string, error) {
			return "", errors.New("content save failed")
		})
		Expect(service.recordRestoreRevision(pageID, newFixtureUserID("tester"))).To(MatchError("content save failed"))
		restoreContent()

		restoreContent = setRevisionSeam(&revisionStoreSaveContentBlob, func(*FSStore, []byte) (string, error) {
			return "wrong-hash", nil
		})
		Expect(service.recordRestoreRevision(pageID, newFixtureUserID("tester"))).To(MatchError(ContainSubstring("content hash mismatch")))
		restoreContent()

		restoreAsset := setRevisionSeam(&revisionStoreSaveAssetBlobFromPath, func(*FSStore, string) (string, int64, error) {
			return "", 0, errors.New("asset save failed")
		})
		Expect(service.recordRestoreRevision(pageID, newFixtureUserID("tester"))).To(MatchError("asset save failed"))
		restoreAsset()

		restoreManifest := setRevisionSeam(&revisionStoreSaveAssetManifest, func(*FSStore, []AssetRef) (string, error) {
			return "", errors.New("manifest save failed")
		})
		Expect(service.recordRestoreRevision(pageID, newFixtureUserID("tester"))).To(MatchError("manifest save failed"))
		restoreManifest()

		restoreManifest = setRevisionSeam(&revisionStoreSaveAssetManifest, func(*FSStore, []AssetRef) (string, error) {
			return "wrong-manifest", nil
		})
		Expect(service.recordRestoreRevision(pageID, newFixtureUserID("tester"))).To(MatchError(ContainSubstring("asset manifest hash mismatch")))
		restoreManifest()

		restoreID := setRevisionSeam(&revisionGenerateUniqueID, func() (string, error) {
			return "", errors.New("id failed")
		})
		Expect(service.recordRestoreRevision(pageID, newFixtureUserID("tester"))).To(MatchError(ContainSubstring("generate revision id")))
		restoreID()
	})

	It("covers remaining batch, manifest fallback, integrity, and restore failure branches", func() {
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
		restoreAssetBlob := setRevisionSeam(&revisionStoreSaveAssetBlobFromPath, func(*FSStore, string) (string, int64, error) {
			return "", 0, errors.New("persist failed")
		})
		_, err = service.resolveAssetManifestHash(pageID, nil)
		Expect(err).To(MatchError("persist failed"))
		restoreAssetBlob()
		restoreManifestExists()

		restoreList := setRevisionSeam(&revisionStoreListRevisions, func(*FSStore, tree.PageID) ([]*Revision, error) {
			return []*Revision{nil}, nil
		})
		issues, err := service.CheckRevisionIntegrity(pageID)
		Expect(err).NotTo(HaveOccurred())
		Expect(issues).To(BeEmpty())
		restoreList()

		unloadedTree := tree.NewTreeService(GinkgoT().TempDir())
		unloadedService := NewService(GinkgoT().TempDir(), unloadedTree, nil)
		expectLocalizedRevisionErrorCode(unloadedService.RestoreRevision(newFixturePageID("unloaded"), newFixtureRevisionID("rev"), newFixtureUserID("tester")), "revision_restore_failed")

		contentHash, err := service.store.SaveContentBlob([]byte("body"))
		Expect(err).NotTo(HaveOccurred())
		manifestHash, err := service.store.SaveAssetManifest(nil)
		Expect(err).NotTo(HaveOccurred())
		rev := saveRevisionFixture(service.store, pageID, newFixtureRevisionID("rev-before-state-failure"), time.Date(2026, 6, 27, 12, 30, 0, 0, time.UTC), contentHash, manifestHash)

		restoreReadDir := setRevisionSeam(&revisionReadDir, func(string) ([]os.DirEntry, error) {
			return nil, errors.New("scan failed")
		})
		expectLocalizedRevisionErrorCode(service.RestoreRevision(pageID, rev.ID, newFixtureUserID("tester")), "revision_restore_failed")
		_, err = service.capturePageState(pageID, true)
		Expect(err).To(MatchError(ContainSubstring("scan failed")))
		restoreReadDir()

		restoreParse := setRevisionSeam(&revisionParsePageDocument, func(string) (markdown.PageDocument, markdown.PageDocumentParseResult, error) {
			return markdown.PageDocument{Body: "body"}, markdown.PageDocumentParseResult{}, nil
		})
		restoreJSON := setRevisionSeam(&revisionJSONMarshal, func(any) ([]byte, error) {
			return nil, errors.New("asset manifest hash failed")
		})
		_, err = service.CapturePageState(pageID)
		Expect(err).To(MatchError(ContainSubstring("marshal asset manifest for hash")))
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
