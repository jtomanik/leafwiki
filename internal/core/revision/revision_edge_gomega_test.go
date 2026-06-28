package revision

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/perber/wiki/internal/core/markdown"
	sharederrors "github.com/perber/wiki/internal/core/shared/errors"
	"github.com/perber/wiki/internal/core/tree"
)

func newGomegaRevisionService() (*Service, *tree.TreeService, string) {
	GinkgoHelper()

	storageDir := GinkgoT().TempDir()
	treeService := tree.NewTreeService(storageDir)
	Expect(treeService.LoadTree()).To(Succeed())

	return NewService(storageDir, treeService, nil), treeService, storageDir
}

func createGomegaRevisionPage(treeService *tree.TreeService, title, slug, content string) tree.PageID {
	GinkgoHelper()

	kind := tree.NodeKindPage
	id, err := treeService.CreateNode(newFixtureUserID("tester"), nil, title, newFixtureSlug(slug), &kind)
	Expect(err).NotTo(HaveOccurred())
	Expect(id).NotTo(BeNil())

	Expect(treeService.UpdateNodeUncheckedVersion(newFixtureUserID("tester"), *id, title, newFixtureSlug(slug), &content, false)).To(Succeed())
	return *id
}

func writeGomegaLiveAsset(storageDir string, pageID tree.PageID, name, content string) {
	GinkgoHelper()

	dir := revisionAssetPath(storageDir, pageID)
	Expect(os.MkdirAll(dir, 0o755)).To(Succeed())
	Expect(os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644)).To(Succeed())
}

func writeStoredAssetBlob(store *FSStore, content []byte) (string, int64) {
	GinkgoHelper()

	hash := sha256HexBytes(content)
	path := store.AssetBlobPath(hash)
	Expect(os.MkdirAll(filepath.Dir(path), 0o755)).To(Succeed())
	Expect(os.WriteFile(path, content, 0o644)).To(Succeed())
	return hash, int64(len(content))
}

func saveRevisionFixture(store *FSStore, pageID tree.PageID, revisionID RevisionID, createdAt time.Time, contentHash, assetManifestHash string) *Revision {
	GinkgoHelper()

	rev := &Revision{
		ID:                revisionID,
		PageID:            pageID,
		CreatedAt:         createdAt.UTC(),
		Type:              RevisionTypeContentUpdate,
		Title:             "Page",
		Slug:              "page",
		ContentHash:       contentHash,
		AssetManifestHash: assetManifestHash,
	}
	Expect(store.SaveRevision(rev)).To(Succeed())
	return rev
}

func expectLocalizedRevisionErrorCode(err error, code sharederrors.ErrorCode) {
	GinkgoHelper()

	Expect(err).To(HaveOccurred())
	localized, ok := sharederrors.AsLocalizedError(err)
	Expect(ok).To(BeTrue(), "expected localized error, got %T: %v", err, err)
	Expect(localized.Code).To(Equal(code))
}

var _ = Describe("revision Gomega edge coverage", func() {
	It("handles service option and no-op branches explicitly", func() {
		service := NewService(GinkgoT().TempDir(), nil, nil, ServiceOptions{MaxRevisions: 1})

		Expect(service.maxRevisions).To(Equal(1))
		Expect(service.RecordContentUpdates(nil, newFixtureUserID("tester"), "empty")).To(BeEmpty())
		Expect(service.DeletePageData(newFixturePageID(""))).To(Succeed())
		Expect(func() { service.pruneAfterSave(newFixturePageID("")) }).NotTo(Panic())
	})

	It("returns public API errors for missing content inputs", func() {
		service, _, _ := newGomegaRevisionService()

		_, created, err := service.RecordContentUpdate(newFixturePageID("missing"), newFixtureUserID("tester"), "missing")
		Expect(created).To(BeFalse())
		Expect(errors.Is(err, tree.ErrPageNotFound)).To(BeTrue())
	})

	It("covers revision metadata helper fallbacks and marshal failures", func() {
		Expect(revisionStoredMetadataHash(nil)).To(BeEmpty())
		Expect(revisionStoredMetadataHash(&Revision{ExtraFrontmatterHash: "legacy-hash"})).To(Equal("legacy-hash"))
		Expect(revisionStoredMetadataHash(&Revision{PageMetadataHash: "canonical-hash", ExtraFrontmatterHash: "legacy-hash"})).To(Equal("canonical-hash"))

		Expect(revisionPageMetadata(markdown.PageMetadata{})).To(BeNil())
		source := markdown.PageMetadata{
			Version: 1,
			Tags:    []string{"alpha"},
			Fields:  map[string]interface{}{},
			Extra:   map[string]interface{}{"aliases": []interface{}{"a"}},
		}
		snapshot := revisionPageMetadata(source)
		Expect(snapshot).NotTo(BeNil())
		Expect(snapshot.Fields).To(BeNil())
		Expect(snapshot.Extra).To(Equal(map[string]interface{}{"aliases": []interface{}{"a"}}))
		source.Tags[0] = "mutated"
		Expect(snapshot.Tags).To(Equal([]string{"alpha"}))

		Expect(hashPageMetadata(nil)).To(BeEmpty())
		_, err := hashPageMetadata(&markdown.PageMetadata{
			Version: 1,
			Page:    markdown.PageMetadataPage{ID: "page"},
			Fields:  map[string]interface{}{"bad": func() {}},
		})
		Expect(err).To(MatchError(ContainSubstring("marshal page metadata")))

		Expect(hashExtraFrontmatter(nil)).To(BeEmpty())
		_, err = hashExtraFrontmatter(map[string]interface{}{"bad": func() {}})
		Expect(err).To(MatchError(ContainSubstring("marshal compatibility metadata extras")))
	})

	It("covers restored content helper fallbacks and validation errors", func() {
		raw, replaceMetadata, err := buildRestoredRawContent(newFixturePageID("page"), " Page ", nil, nil, "body")
		Expect(err).NotTo(HaveOccurred())
		Expect(raw).To(Equal("body"))
		Expect(replaceMetadata).To(BeFalse())

		_, _, err = buildRestoredRawContent(newFixturePageID("page"), "Page", &markdown.PageMetadata{
			Version: 1,
			Fields:  map[string]interface{}{"bad": []string{"unsupported"}},
		}, nil, "body")
		Expect(err).To(HaveOccurred())

		_, _, err = buildRestoredRawContent(newFixturePageID("../bad"), "Page", nil, map[string]interface{}{
			"legacy": "value",
		}, "body")
		Expect(err).To(HaveOccurred())
	})

	It("reports each revision asset delta type in stable order", func() {
		deltas := compareRevisionAssets(
			[]AssetRef{
				{Name: "modified.txt", SHA256: "old", SizeBytes: 1},
				{Name: "removed.txt", SHA256: "gone", SizeBytes: 2},
				{Name: "same.txt", SHA256: "same", SizeBytes: 3},
			},
			[]AssetRef{
				{Name: "added.txt", SHA256: "new", SizeBytes: 4},
				{Name: "modified.txt", SHA256: "new", SizeBytes: 1},
				{Name: "same.txt", SHA256: "same", SizeBytes: 3},
			},
		)

		Expect(deltas).To(Equal([]RevisionAssetDelta{
			{Name: "added.txt", Status: "added"},
			{Name: "modified.txt", Status: "modified"},
			{Name: "removed.txt", Status: "removed"},
		}))
	})

	It("covers FSStore empty, invalid, and malformed helper paths", func() {
		store := NewFSStore(GinkgoT().TempDir())

		Expect(store.ReadContentBlob(" ")).To(BeEmpty())
		reader, err := store.OpenContentBlob(" ")
		Expect(err).NotTo(HaveOccurred())
		Expect(io.ReadAll(reader)).To(BeEmpty())
		Expect(reader.Close()).To(Succeed())

		_, err = store.ReadContentBlob("missing-content")
		Expect(err).To(MatchError(ContainSubstring("read content blob")))
		_, err = store.ReadAssetBlob("missing-asset")
		Expect(err).To(MatchError(ContainSubstring("read asset blob")))
		_, err = store.OpenAssetBlob(" ")
		Expect(err).To(MatchError(ContainSubstring("asset hash is required")))
		Expect(store.AssetManifestExists("")).To(BeFalse())
		Expect(store.DeletePageRevisions(newFixturePageID("../bad"))).To(Succeed())

		Expect(cloneAndSortAssetRefs([]AssetRef{
			{Name: "b.txt", SHA256: "2"},
			{Name: "a.txt", SHA256: "2"},
			{Name: "a.txt", SHA256: "1"},
		})).To(Equal([]AssetRef{
			{Name: "a.txt", SHA256: "1"},
			{Name: "a.txt", SHA256: "2"},
			{Name: "b.txt", SHA256: "2"},
		}))

		Expect(writeJSONAtomic(filepath.Join(GinkgoT().TempDir(), "bad.json"), map[string]interface{}{"bad": func() {}})).To(HaveOccurred())

		Expect(store.saveRevisionIndex(newFixturePageID("indexed"), nil)).To(Succeed())
		index, err := store.loadRevisionIndex(newFixturePageID("indexed"))
		Expect(err).NotTo(HaveOccurred())
		Expect(index).To(BeEmpty())
	})

	It("surfaces malformed revision files and indexes", func() {
		store := NewFSStore(GinkgoT().TempDir())

		badListPageID := newFixturePageID("bad-list")
		Expect(os.MkdirAll(store.revisionsPageDir(badListPageID), 0o755)).To(Succeed())
		Expect(os.WriteFile(filepath.Join(store.revisionsPageDir(badListPageID), "20260626T120000.000000000Z_bad.json"), []byte("{"), 0o644)).To(Succeed())
		_, _, err := store.ListRevisionsPage(badListPageID, "", 0)
		Expect(err).To(MatchError(ContainSubstring("read revision")))
		_, err = store.GetLatestRevision(badListPageID)
		Expect(err).To(MatchError(ContainSubstring("read latest revision")))

		badIndexPageID := newFixturePageID("bad-index")
		Expect(os.MkdirAll(store.revisionsPageDir(badIndexPageID), 0o755)).To(Succeed())
		Expect(os.WriteFile(store.revisionIndexPath(badIndexPageID), []byte("{"), 0o644)).To(Succeed())
		_, err = store.loadRevisionIndex(badIndexPageID)
		Expect(err).To(MatchError(ContainSubstring("read revision index")))
		_, err = store.GetRevision(badIndexPageID, newFixtureRevisionID("rev"))
		Expect(err).To(MatchError(ContainSubstring("read revision index")))

		indexedBadPageID := newFixturePageID("indexed-bad")
		Expect(os.MkdirAll(store.revisionsPageDir(indexedBadPageID), 0o755)).To(Succeed())
		Expect(os.WriteFile(store.revisionIndexPath(indexedBadPageID), []byte(`{"rev-bad":"bad.json"}`), 0o644)).To(Succeed())
		Expect(os.WriteFile(filepath.Join(store.revisionsPageDir(indexedBadPageID), "bad.json"), []byte("{"), 0o644)).To(Succeed())
		_, err = store.GetRevision(indexedBadPageID, newFixtureRevisionID("rev-bad"))
		Expect(err).To(MatchError(ContainSubstring("read revision bad.json")))

		fallbackBadPageID := newFixturePageID("fallback-bad")
		Expect(os.MkdirAll(store.revisionsPageDir(fallbackBadPageID), 0o755)).To(Succeed())
		Expect(os.WriteFile(filepath.Join(store.revisionsPageDir(fallbackBadPageID), "20260626T120000.000000000Z_rev-fallback.json"), []byte("{"), 0o644)).To(Succeed())
		_, err = store.GetRevision(fallbackBadPageID, newFixtureRevisionID("rev-fallback"))
		Expect(err).To(MatchError(ContainSubstring("read revision")))

		pruneBadIndexPageID := newFixturePageID("prune-bad-index")
		createdAt := time.Date(2026, 6, 26, 12, 0, 0, 0, time.UTC)
		saveRevisionFixture(store, pruneBadIndexPageID, newFixtureRevisionID("rev-1"), createdAt, "", "")
		saveRevisionFixture(store, pruneBadIndexPageID, newFixtureRevisionID("rev-2"), createdAt.Add(time.Minute), "", "")
		Expect(os.WriteFile(store.revisionIndexPath(pruneBadIndexPageID), []byte("{"), 0o644)).To(Succeed())
		Expect(store.PruneRevisions(pruneBadIndexPageID, 1)).To(MatchError(ContainSubstring("read revision index")))
	})

	It("validates SaveRevision required fields", func() {
		store := NewFSStore(GinkgoT().TempDir())

		err := store.SaveRevision(&Revision{
			ID:     newFixtureRevisionID("rev-no-created-at"),
			PageID: newFixturePageID("page"),
			Type:   RevisionTypeContentUpdate,
			Title:  "Page",
			Slug:   "page",
		})
		Expect(err).To(MatchError(ContainSubstring("created_at is required")))
	})

	It("covers asset copy validation and destination errors", func() {
		tmp := GinkgoT().TempDir()
		store := NewFSStore(tmp)
		hash, size := writeStoredAssetBlob(store, []byte("asset"))

		missingDirPath := filepath.Join(tmp, "missing-dir", "asset.txt")
		Expect(store.CopyAssetBlobToPath(hash, size, missingDirPath)).To(MatchError(ContainSubstring("create temp restore file")))

		restoreDir := filepath.Join(tmp, "restore")
		Expect(os.MkdirAll(restoreDir, 0o755)).To(Succeed())
		Expect(store.CopyAssetBlobToPath(hash, size+1, filepath.Join(restoreDir, "wrong-size.txt"))).To(MatchError(ContainSubstring("asset blob size mismatch")))

		tamperedHash := strings.Repeat("b", 64)
		tamperedPath := store.AssetBlobPath(tamperedHash)
		Expect(os.MkdirAll(filepath.Dir(tamperedPath), 0o755)).To(Succeed())
		Expect(os.WriteFile(tamperedPath, []byte("tampered"), 0o644)).To(Succeed())
		Expect(store.CopyAssetBlobToPath(tamperedHash, int64(len("tampered")), filepath.Join(restoreDir, "wrong-hash.txt"))).To(MatchError(ContainSubstring("asset blob hash mismatch")))

		existingDirTarget := filepath.Join(tmp, "existing-dir-target")
		Expect(os.MkdirAll(existingDirTarget, 0o755)).To(Succeed())
		Expect(store.CopyAssetBlobToPath(hash, size, existingDirTarget)).To(MatchError(ContainSubstring("move restored asset into place")))
	})

	It("reports missing asset blobs and size mismatches during integrity checks", func() {
		service, treeService, _ := newGomegaRevisionService()
		pageID := createGomegaRevisionPage(treeService, "Page", "page", "body")
		contentHash, err := service.store.SaveContentBlob([]byte("body"))
		Expect(err).NotTo(HaveOccurred())
		createdAt := time.Date(2026, 6, 26, 12, 0, 0, 0, time.UTC)

		missingBlobManifest, err := service.store.SaveAssetManifest([]AssetRef{{
			Name:      "missing.txt",
			SHA256:    strings.Repeat("c", 64),
			SizeBytes: 5,
		}})
		Expect(err).NotTo(HaveOccurred())
		saveRevisionFixture(service.store, pageID, newFixtureRevisionID("rev-missing-asset-blob"), createdAt, contentHash, missingBlobManifest)

		sizeHash, size := writeStoredAssetBlob(service.store, []byte("asset"))
		sizeMismatchManifest, err := service.store.SaveAssetManifest([]AssetRef{{
			Name:      "wrong-size.txt",
			SHA256:    sizeHash,
			SizeBytes: size + 1,
		}})
		Expect(err).NotTo(HaveOccurred())
		saveRevisionFixture(service.store, pageID, newFixtureRevisionID("rev-wrong-size"), createdAt.Add(time.Minute), contentHash, sizeMismatchManifest)

		issues, err := service.CheckRevisionIntegrity(pageID)
		Expect(err).NotTo(HaveOccurred())

		codes := make([]sharederrors.ErrorCode, 0, len(issues))
		for _, issue := range issues {
			codes = append(codes, issue.Code)
		}
		Expect(codes).To(ContainElements(
			sharederrors.ErrorCode("missing_asset_blob"),
			sharederrors.ErrorCode("asset_blob_size_mismatch"),
		))
	})

	It("covers service error branches and manifest resolution fallbacks", func() {
		service, treeService, storageDir := newGomegaRevisionService()

		errs := service.RecordContentUpdates([]*tree.Page{{
			PageNode: &tree.PageNode{
				ID:    newFixturePageID("missing-batch-page"),
				Title: "Missing",
				Slug:  newFixtureSlug("missing"),
			},
		}}, newFixtureUserID("tester"), "batch")
		Expect(errs).To(HaveLen(1))
		Expect(errs[0]).To(HaveOccurred())

		_, created, err := service.RecordAssetChange(newFixturePageID("../bad"), newFixtureUserID("tester"), "bad")
		Expect(created).To(BeFalse())
		Expect(err).To(HaveOccurred())
		_, created, err = service.RecordAssetChange(newFixturePageID("missing-asset-page"), newFixtureUserID("tester"), "missing")
		Expect(created).To(BeFalse())
		Expect(err).To(HaveOccurred())
		_, created, err = service.RecordStructureChange(newFixturePageID("../bad"), newFixtureUserID("tester"), "bad")
		Expect(created).To(BeFalse())
		Expect(err).To(HaveOccurred())
		_, created, err = service.RecordStructureChange(newFixturePageID("missing-structure-page"), newFixtureUserID("tester"), "missing")
		Expect(created).To(BeFalse())
		Expect(err).To(HaveOccurred())
		Expect(service.recordRestoreRevision(newFixturePageID("missing-restore-page"), newFixtureUserID("tester"))).To(HaveOccurred())
		Expect(service.enrichStateWithExtraFrontmatter(newFixturePageID("page"), nil)).To(MatchError(ContainSubstring("revision state is required")))
		Expect(service.enrichStateWithExtraFrontmatter(newFixturePageID("missing-enrich-page"), &RevisionState{})).To(HaveOccurred())
		_, err = service.resolveAssetManifestHash(newFixturePageID("missing-manifest-page"), nil)
		Expect(err).To(HaveOccurred())

		pageID := createGomegaRevisionPage(treeService, "Page", "page", "body")
		writeGomegaLiveAsset(storageDir, pageID, "asset.txt", "asset")
		rev, created, err := service.RecordAssetChange(pageID, newFixtureUserID("tester"), "assets")
		Expect(err).NotTo(HaveOccurred())
		Expect(created).To(BeTrue())
		service.assetManifestCache.Delete(pageID)
		hash, err := service.resolveAssetManifestHash(pageID, rev)
		Expect(err).NotTo(HaveOccurred())
		Expect(hash).To(Equal(rev.AssetManifestHash))
	})

	It("covers asset helper defaults and restore asset validation", func() {
		service, _, _ := newGomegaRevisionService()
		assetPath := filepath.Join(GinkgoT().TempDir(), "asset")
		Expect(os.WriteFile(assetPath, []byte("asset"), 0o644)).To(Succeed())

		ref, err := buildAssetRef(assetPath, "asset")
		Expect(err).NotTo(HaveOccurred())
		Expect(ref.MIMEType).To(Equal("application/octet-stream"))

		_, err = buildAssetRef(filepath.Dir(assetPath), "dir")
		Expect(err).To(HaveOccurred())

		pageID := newFixturePageID("restore-validation")
		hash, size := writeStoredAssetBlob(service.store, []byte("asset"))
		Expect(service.restoreAssets(pageID, []AssetRef{{Name: "../bad.txt", SHA256: hash, SizeBytes: size}})).To(MatchError(ContainSubstring("invalid asset name")))
		Expect(service.restoreAssets(pageID, []AssetRef{
			{Name: "dup.txt", SHA256: hash, SizeBytes: size},
			{Name: "dup.txt", SHA256: hash, SizeBytes: size},
		})).To(MatchError(ContainSubstring("duplicate asset name")))
	})

	It("rolls back content when restore asset rehydration fails", func() {
		service, treeService, _ := newGomegaRevisionService()
		pageID := createGomegaRevisionPage(treeService, "Page", "page", "original")
		contentHash, err := service.store.SaveContentBlob([]byte("restored"))
		Expect(err).NotTo(HaveOccurred())
		brokenManifest, err := service.store.SaveAssetManifest([]AssetRef{{
			Name:      "missing.txt",
			SHA256:    strings.Repeat("d", 64),
			SizeBytes: 7,
		}})
		Expect(err).NotTo(HaveOccurred())
		rev := saveRevisionFixture(service.store, pageID, newFixtureRevisionID("rev-broken-restore-assets"), time.Date(2026, 6, 26, 12, 0, 0, 0, time.UTC), contentHash, brokenManifest)

		err = service.RestoreRevision(pageID, rev.ID, newFixtureUserID("tester"))
		expectLocalizedRevisionErrorCode(err, "revision_restore_failed")

		page, err := treeService.GetPage(pageID)
		Expect(err).NotTo(HaveOccurred())
		Expect(page.Content).To(Equal("original"))
	})

	It("covers additional deterministic service and store edge paths", func() {
		service, treeService, storageDir := newGomegaRevisionService()
		pageID := createGomegaRevisionPage(treeService, "Page", "page", "body")
		createdAt := time.Date(2026, 6, 26, 12, 0, 0, 0, time.UTC)

		contentHash, err := service.store.SaveContentBlob([]byte("body"))
		Expect(err).NotTo(HaveOccurred())
		manifestHash, err := service.store.SaveAssetManifest(nil)
		Expect(err).NotTo(HaveOccurred())
		validRev := saveRevisionFixture(service.store, pageID, newFixtureRevisionID("rev-valid"), createdAt, contentHash, manifestHash)

		_, err = service.CompareRevisionSnapshots(pageID, validRev.ID, newFixtureRevisionID("missing-target"))
		Expect(err).To(HaveOccurred())
		_, err = service.GetRevisionAsset(pageID, newFixtureRevisionID("missing-asset-revision"), tree.AssetName("image.png"))
		Expect(err).To(HaveOccurred())

		Expect(os.WriteFile(service.store.revisionIndexPath(pageID), []byte("{"), 0o644)).To(Succeed())
		expectLocalizedRevisionErrorCode(service.RestoreRevision(pageID, validRev.ID, newFixtureUserID("tester")), "revision_restore_failed")
		Expect(os.Remove(service.store.revisionIndexPath(pageID))).To(Succeed())

		invalidMetadataRev := saveRevisionFixture(service.store, pageID, newFixtureRevisionID("rev-invalid-metadata"), createdAt.Add(time.Minute), contentHash, manifestHash)
		invalidMetadataRev.PageMetadata = &markdown.PageMetadata{
			Version: 1,
			Fields:  map[string]interface{}{"bad": []string{"unsupported"}},
		}
		Expect(service.store.SaveRevision(invalidMetadataRev)).To(Succeed())
		expectLocalizedRevisionErrorCode(service.RestoreRevision(pageID, invalidMetadataRev.ID, newFixtureUserID("tester")), "revision_restore_failed")

		brokenAssetPageID := createGomegaRevisionPage(treeService, "Broken Assets", "broken-assets", "body")
		assetPath := revisionAssetPath(storageDir, brokenAssetPageID)
		Expect(os.MkdirAll(filepath.Dir(assetPath), 0o755)).To(Succeed())
		Expect(os.WriteFile(assetPath, []byte("not a directory"), 0o644)).To(Succeed())
		_, _, err = service.RecordContentUpdate(brokenAssetPageID, newFixtureUserID("tester"), "broken assets")
		Expect(err).To(HaveOccurred())
		_, _, err = service.RecordStructureChange(brokenAssetPageID, newFixtureUserID("tester"), "broken assets")
		Expect(err).To(HaveOccurred())
		Expect(os.Remove(assetPath)).To(Succeed())

		Expect(service.persistLiveAssets(pageID, []AssetRef{{Name: "missing.txt", SHA256: strings.Repeat("e", 64), SizeBytes: 7}})).To(HaveOccurred())

		brokenSymlinkDir := revisionAssetPath(storageDir, newFixturePageID("broken-symlink-page"))
		Expect(os.MkdirAll(brokenSymlinkDir, 0o755)).To(Succeed())
		Expect(os.Symlink(filepath.Join(brokenSymlinkDir, "missing-target"), filepath.Join(brokenSymlinkDir, "broken.txt"))).To(Succeed())
		_, err = service.scanLiveAssets(newFixturePageID("broken-symlink-page"))
		Expect(err).To(HaveOccurred())

		badPage := &tree.Page{PageNode: &tree.PageNode{ID: newFixturePageID("../bad")}}
		_, _, err = service.recordContentUpdateForPage(badPage, newFixtureUserID("tester"), "bad")
		Expect(err).To(HaveOccurred())
	})

	It("covers additional deterministic FSStore edge paths", func() {
		store := NewFSStore(GinkgoT().TempDir())

		fileBackedPageID := newFixturePageID("file-backed")
		Expect(os.MkdirAll(store.revisionsDir(), 0o755)).To(Succeed())
		Expect(os.WriteFile(store.revisionsPageDir(fileBackedPageID), []byte("not a dir"), 0o644)).To(Succeed())
		_, err := store.GetLatestRevision(fileBackedPageID)
		Expect(err).To(MatchError(ContainSubstring("read revisions dir")))
		_, err = store.GetRevision(fileBackedPageID, newFixtureRevisionID("rev"))
		Expect(err).To(MatchError(ContainSubstring("read revision index")))

		nullIndexPageID := newFixturePageID("null-index")
		Expect(os.MkdirAll(store.revisionsPageDir(nullIndexPageID), 0o755)).To(Succeed())
		Expect(os.WriteFile(store.revisionIndexPath(nullIndexPageID), []byte("null"), 0o644)).To(Succeed())
		index, err := store.loadRevisionIndex(nullIndexPageID)
		Expect(err).NotTo(HaveOccurred())
		Expect(index).To(BeEmpty())

		noIndexChangePageID := newFixturePageID("no-index-change")
		createdAt := time.Date(2026, 6, 26, 12, 0, 0, 0, time.UTC)
		saveRevisionFixture(store, noIndexChangePageID, newFixtureRevisionID("rev-1"), createdAt, "", "")
		saveRevisionFixture(store, noIndexChangePageID, newFixtureRevisionID("rev-2"), createdAt.Add(time.Minute), "", "")
		Expect(store.saveRevisionIndex(noIndexChangePageID, revisionIndex{})).To(Succeed())
		Expect(store.PruneRevisions(noIndexChangePageID, 1)).To(Succeed())

		writeErrorPageID := newFixturePageID("write-error")
		revisionID := newFixtureRevisionID("rev-dir-conflict")
		revisionTime := time.Date(2026, 6, 26, 13, 0, 0, 0, time.UTC)
		Expect(os.MkdirAll(store.revisionFilePath(writeErrorPageID, revisionID, revisionTime), 0o755)).To(Succeed())
		err = store.SaveRevision(&Revision{
			ID:        revisionID,
			PageID:    writeErrorPageID,
			CreatedAt: revisionTime,
			Type:      RevisionTypeContentUpdate,
			Title:     "Page",
			Slug:      "page",
		})
		Expect(err).To(MatchError(ContainSubstring("write revision")))

		badIndexSavePageID := newFixturePageID("save-with-bad-index")
		Expect(os.MkdirAll(store.revisionsPageDir(badIndexSavePageID), 0o755)).To(Succeed())
		Expect(os.WriteFile(store.revisionIndexPath(badIndexSavePageID), []byte("{"), 0o644)).To(Succeed())
		err = store.SaveRevision(&Revision{
			ID:        newFixtureRevisionID("rev-bad-index"),
			PageID:    badIndexSavePageID,
			CreatedAt: revisionTime.Add(time.Minute),
			Type:      RevisionTypeContentUpdate,
			Title:     "Page",
			Slug:      "page",
		})
		Expect(err).To(MatchError(ContainSubstring("read revision index")))

		invalidBase := filepath.Join(GinkgoT().TempDir(), "not-a-dir")
		Expect(os.WriteFile(invalidBase, []byte("x"), 0o644)).To(Succeed())
		invalidStore := NewFSStore(invalidBase)
		Expect(invalidStore.saveRevisionIndex(newFixturePageID("page"), nil)).To(MatchError(ContainSubstring("ensure revision dir")))
	})

	It("localizes restore, snapshot, comparison, and asset preview failures", func() {
		service, treeService, _ := newGomegaRevisionService()
		pageID := createGomegaRevisionPage(treeService, "Page", "page", "body")
		createdAt := time.Date(2026, 6, 26, 12, 0, 0, 0, time.UTC)

		expectLocalizedRevisionErrorCode(service.RestoreRevision(newFixturePageID(""), newFixtureRevisionID("rev"), newFixtureUserID("tester")), "revision_restore_invalid_page_id")
		expectLocalizedRevisionErrorCode(service.RestoreRevision(pageID, newFixtureRevisionID(""), newFixtureUserID("tester")), "revision_restore_invalid_revision")
		expectLocalizedRevisionErrorCode(service.RestoreRevision(newFixturePageID("missing"), newFixtureRevisionID("rev"), newFixtureUserID("tester")), "revision_restore_page_not_found")
		expectLocalizedRevisionErrorCode(service.RestoreRevision(pageID, newFixtureRevisionID("missing-rev"), newFixtureUserID("tester")), "revision_restore_revision_not_found")

		manifestHash, err := service.store.SaveAssetManifest(nil)
		Expect(err).NotTo(HaveOccurred())
		missingContentRev := saveRevisionFixture(service.store, pageID, newFixtureRevisionID("rev-missing-content"), createdAt, "missing-content", manifestHash)
		expectLocalizedRevisionErrorCode(service.RestoreRevision(pageID, missingContentRev.ID, newFixtureUserID("tester")), "revision_restore_content_missing")

		contentHash, err := service.store.SaveContentBlob([]byte("body"))
		Expect(err).NotTo(HaveOccurred())
		missingManifestRev := saveRevisionFixture(service.store, pageID, newFixtureRevisionID("rev-missing-manifest"), createdAt.Add(time.Minute), contentHash, "missing-manifest")
		expectLocalizedRevisionErrorCode(service.RestoreRevision(pageID, missingManifestRev.ID, newFixtureUserID("tester")), "revision_restore_assets_missing")

		_, err = service.GetRevisionSnapshot(pageID, newFixtureRevisionID("missing-snapshot"))
		Expect(err).To(HaveOccurred())
		_, err = service.GetRevisionSnapshot(pageID, missingContentRev.ID)
		expectLocalizedRevisionErrorCode(err, "revision_preview_content_unavailable")
		_, err = service.GetRevisionSnapshot(pageID, missingManifestRev.ID)
		expectLocalizedRevisionErrorCode(err, "revision_preview_assets_unavailable")

		_, err = service.CompareRevisionSnapshots(pageID, newFixtureRevisionID("missing-base"), missingManifestRev.ID)
		Expect(err).To(HaveOccurred())
		_, err = service.CompareRevisionSnapshots(pageID, missingContentRev.ID, newFixtureRevisionID("missing-target"))
		Expect(err).To(HaveOccurred())

		_, err = service.GetRevisionAsset(pageID, missingManifestRev.ID, tree.AssetName(" "))
		expectLocalizedRevisionErrorCode(err, "revision_preview_asset_invalid_name")
		_, err = service.GetRevisionAsset(pageID, missingManifestRev.ID, tree.AssetName("image.png"))
		expectLocalizedRevisionErrorCode(err, "revision_preview_assets_unavailable")

		missingBlobManifestHash, err := service.store.SaveAssetManifest([]AssetRef{{Name: "image.png", SHA256: strings.Repeat("a", 64), SizeBytes: 5}})
		Expect(err).NotTo(HaveOccurred())
		missingBlobRev := saveRevisionFixture(service.store, pageID, newFixtureRevisionID("rev-missing-blob"), createdAt.Add(2*time.Minute), contentHash, missingBlobManifestHash)
		_, err = service.GetRevisionAsset(pageID, missingBlobRev.ID, tree.AssetName("image.png"))
		expectLocalizedRevisionErrorCode(err, "revision_preview_asset_blob_unavailable")
	})
})
