package revision

import (
	ginkgo "github.com/onsi/ginkgo/v2"
	"os"
	"path/filepath"
	"syscall"
	"time"

	. "github.com/onsi/gomega"
	"github.com/perber/wiki/internal/core/markdown"
	sharederrors "github.com/perber/wiki/internal/core/shared/errors"
	"github.com/perber/wiki/internal/core/tree"
)

var invalidRevisionPageIDCases = []struct {
	name string
	id   string
}{
	{name: "parent directory", id: "../other"},
	{name: "nested parent directory", id: "../../etc/passwd"},
	{name: "dot dot", id: ".."},
	{name: "dot", id: "."},
	{name: "empty string", id: ""},
	{name: "whitespace-only string", id: "  "},
	{name: "path separator", id: "foo/bar"},
	{name: "windows separator", id: `foo\bar`},
}

var _ = ginkgo.Describe("fs store", func() {
	ginkgo.It("returns revisions in newest-first pages and retrieves explicit revisions", ginkgo.Label("integration"), func() {
		store := NewFSStore(revisionTempDir())
		created1 := time.Date(2026, 3, 26, 10, 0, 0, 0, time.UTC)
		created2 := created1.Add(time.Minute)
		created3 := created2.Add(time.Minute)

		rev1 := &Revision{ID: "rev1", PageID: "page-1", CreatedAt: created1, Type: RevisionTypeContentUpdate, Title: "A", Slug: "a"}
		rev2 := &Revision{ID: "rev2", PageID: "page-1", CreatedAt: created2, Type: RevisionTypeAssetUpdate, Title: "A", Slug: "a"}
		rev3 := &Revision{ID: "rev3", PageID: "page-1", CreatedAt: created3, Type: RevisionTypeStructureUpdate, Title: "A", Slug: "a"}
		for _, rev := range []*Revision{rev1, rev2, rev3} {
			Expect(store.SaveRevision(rev)).To(Succeed())
		}

		latest, err := store.GetLatestRevision("page-1")
		Expect(err).NotTo(HaveOccurred())
		Expect(latest).To(HaveField("ID", newFixtureRevisionID("rev3")))

		got, err := store.GetRevision("page-1", "rev2")
		Expect(err).NotTo(HaveOccurred())
		Expect(got).To(HaveField("ID", newFixtureRevisionID("rev2")))

		firstPage, nextCursor, err := store.ListRevisionsPage("page-1", "", 2)
		Expect(err).NotTo(HaveOccurred())
		Expect(firstPage).To(HaveExactElements(
			HaveField("ID", newFixtureRevisionID("rev3")),
			HaveField("ID", newFixtureRevisionID("rev2")),
		))
		Expect(nextCursor).NotTo(BeEmpty())

		secondPage, nextCursor2, err := store.ListRevisionsPage("page-1", nextCursor, 2)
		Expect(err).NotTo(HaveOccurred())
		Expect(secondPage).To(HaveExactElements(HaveField("ID", newFixtureRevisionID("rev1"))))
		Expect(nextCursor2).To(BeEmpty())
	})

	ginkgo.It("stores and restores content blobs, asset blobs, and manifests", ginkgo.Label("integration"), func() {
		store := NewFSStore(revisionTempDir())

		contentHash, err := store.SaveContentBlob([]byte("hello"))
		Expect(err).NotTo(HaveOccurred())
		raw, err := store.ReadContentBlob(contentHash)
		Expect(err).NotTo(HaveOccurred())
		Expect(string(raw)).To(Equal("hello"))

		assetSrcDir := filepath.Join(revisionTempDir(), "src")
		Expect(os.MkdirAll(assetSrcDir, 0o755)).To(Succeed())
		assetSrc := filepath.Join(assetSrcDir, "asset.txt")
		Expect(os.WriteFile(assetSrc, []byte("asset-data"), 0o644)).To(Succeed())
		hash, size, err := store.SaveAssetBlobFromPath(assetSrc)
		Expect(err).NotTo(HaveOccurred())
		Expect(size).To(Equal(int64(len("asset-data"))))
		assetRaw, err := store.ReadAssetBlob(hash)
		Expect(err).NotTo(HaveOccurred())
		Expect(string(assetRaw)).To(Equal("asset-data"))

		manifestHash, err := store.SaveAssetManifest([]AssetRef{{Name: "asset.txt", SHA256: hash, SizeBytes: size}})
		Expect(err).NotTo(HaveOccurred())
		manifest, err := store.LoadAssetManifest(manifestHash)
		Expect(err).NotTo(HaveOccurred())
		Expect(manifest).To(HaveExactElements(HaveField("Name", "asset.txt")))
	})

	ginkgo.It("serializes page metadata with snake-case JSON fields", ginkgo.Label("integration"), func() {
		store := NewFSStore(revisionTempDir())
		createdAt := time.Date(2026, 6, 14, 10, 0, 0, 0, time.UTC)
		revision := &Revision{
			ID:        "rev-snake",
			PageID:    "page-snake",
			CreatedAt: createdAt,
			Type:      RevisionTypeContentUpdate,
			Title:     "Page",
			Slug:      "page",
			PageMetadata: &markdown.PageMetadata{
				Version: 1,
				Page: markdown.PageMetadataPage{
					ID:           "page-snake",
					Title:        "Page",
					CreatedAt:    "2026-06-13T10:00:00Z",
					UpdatedAt:    "2026-06-13T11:00:00Z",
					CreatorID:    "creator",
					LastAuthorID: "editor",
				},
				Tags:   []string{"ready"},
				Fields: map[string]interface{}{"status": "published"},
			},
		}

		Expect(store.SaveRevision(revision)).To(Succeed())

		raw, err := os.ReadFile(store.revisionFilePath("page-snake", "rev-snake", createdAt))
		Expect(err).NotTo(HaveOccurred())
		revisionJSON := string(raw)
		for _, want := range []string{
			`"page_metadata"`,
			`"version"`,
			`"page"`,
			`"created_at"`,
			`"updated_at"`,
			`"creator_id"`,
			`"last_author_id"`,
		} {
			Expect(revisionJSON).To(ContainSubstring(want))
		}
		for _, legacyGoName := range []string{
			`"Version":`,
			`"Page":`,
			`"CreatedAt":`,
			`"UpdatedAt":`,
			`"CreatorID":`,
			`"LastAuthorID":`,
		} {
			Expect(revisionJSON).NotTo(ContainSubstring(legacyGoName))
		}
	})

	ginkgo.It("removes page revision history idempotently", ginkgo.Label("integration"), func() {
		store := NewFSStore(revisionTempDir())
		createdAt := time.Date(2026, 4, 12, 18, 0, 0, 0, time.UTC)

		revision := &Revision{
			ID:        "rev1",
			PageID:    "page-1",
			CreatedAt: createdAt,
			Type:      RevisionTypeContentUpdate,
			Title:     "Page",
			Slug:      "page",
		}
		Expect(store.SaveRevision(revision)).To(Succeed())

		_, err := os.Stat(store.revisionsPageDir("page-1"))
		Expect(err).NotTo(HaveOccurred())

		Expect(store.DeletePageRevisions("page-1")).To(Succeed())

		_, err = os.Stat(store.revisionsPageDir("page-1"))
		Expect(err).To(MatchError(os.ErrNotExist))

		revisions, err := store.ListRevisions("page-1")
		Expect(err).NotTo(HaveOccurred())
		Expect(revisions).To(BeEmpty())

		Expect(store.DeletePageRevisions("page-1")).To(Succeed())
	})

	ginkgo.It("accepts empty content references and rejects missing required revision data", ginkgo.Label("unit"), func() {
		store := NewFSStore(revisionTempDir())

		_, err := store.ReadContentBlob("")
		Expect(err).NotTo(HaveOccurred())
		_, err = store.LoadAssetManifest("")
		Expect(err).NotTo(HaveOccurred())
		_, err = store.ReadAssetBlob("")
		Expect(err).To(MatchError(ErrAssetHashRequired))
		Expect(store.SaveRevision(nil)).To(rejectRevisionValidation())
	})

	ginkgo.It("reads legacy revision files without compatibility metadata fields", ginkgo.Label("integration"), func() {
		store := NewFSStore(revisionTempDir())
		createdAt := time.Date(2026, 4, 20, 15, 4, 5, 0, time.UTC)
		pageID := newFixturePageID("page-1")
		revisionID := newFixtureRevisionID("rev-legacy")

		payload := map[string]interface{}{
			"id":              revisionID,
			"page_id":         pageID,
			"type":            string(RevisionTypeContentUpdate),
			"author_id":       "tester",
			"created_at":      createdAt.Format(time.RFC3339),
			"title":           "Legacy",
			"slug":            "legacy",
			"kind":            "page",
			"path":            "/legacy",
			"content_hash":    "abc123",
			"page_created_at": createdAt.Format(time.RFC3339),
			"page_updated_at": createdAt.Format(time.RFC3339),
			"creator_id":      "creator",
			"last_author_id":  "editor",
		}

		revisionPath := store.revisionFilePath(pageID, revisionID, createdAt)
		Expect(os.MkdirAll(filepath.Dir(revisionPath), 0o755)).To(Succeed())
		Expect(writeJSONAtomic(revisionPath, payload)).To(Succeed())
		Expect(store.saveRevisionIndex(pageID, revisionIndex{revisionID.CommitID(): filepath.Base(revisionPath)})).To(Succeed())

		rev, err := store.GetRevision(pageID, revisionID)
		Expect(err).NotTo(HaveOccurred())
		Expect(rev).To(SatisfyAll(
			HaveField("ID", revisionID),
			HaveField("ExtraFrontmatter", BeNil()),
			HaveField("ExtraFrontmatterHash", BeEmpty()),
		))
	})

	ginkgo.It("returns stable empty results and validates missing revisions", ginkgo.Label("integration"), func() {
		store := NewFSStore(revisionTempDir())
		got, err := store.ListRevisions("missing")
		Expect(err).NotTo(HaveOccurred())
		Expect(got).To(BeEmpty())

		latest, err := store.GetLatestRevision("missing")
		Expect(err).NotTo(HaveOccurred())
		Expect(latest).To(BeNil())

		_, err = store.GetRevision("missing", "")
		Expect(err).To(MatchError(os.ErrNotExist))
		_, err = store.GetRevision("missing", "rev1")
		Expect(err).To(MatchError(os.ErrNotExist))

		Expect(shardHash("a")).To(Equal("00"))
		Expect(shardHash("abcd")).To(Equal("ab"))

		items := cloneAndSortAssetRefs([]AssetRef{{Name: "b.txt", SHA256: "2"}, {Name: "a.txt", SHA256: "1"}})
		Expect(items).To(HaveExactElements(
			HaveField("Name", "a.txt"),
			HaveField("Name", "b.txt"),
		))
	})

	ginkgo.It("reuses content, asset, and manifest hashes for repeated saves", ginkgo.Label("integration"), func() {
		store := NewFSStore(revisionTempDir())

		h1, err := store.SaveContentBlob([]byte("same"))
		Expect(err).NotTo(HaveOccurred())
		h2, err := store.SaveContentBlob([]byte("same"))
		Expect(err).NotTo(HaveOccurred())
		Expect(h2).To(Equal(h1))

		srcDir := filepath.Join(revisionTempDir(), "src")
		Expect(os.MkdirAll(srcDir, 0o755)).To(Succeed())
		src := filepath.Join(srcDir, "asset.txt")
		Expect(os.WriteFile(src, []byte("asset"), 0o644)).To(Succeed())
		hash1, size1, err := store.SaveAssetBlobFromPath(src)
		Expect(err).NotTo(HaveOccurred())
		hash2, size2, err := store.SaveAssetBlobFromPath(src)
		Expect(err).NotTo(HaveOccurred())
		Expect(hash2).To(Equal(hash1))
		Expect(size2).To(Equal(size1))

		manifest := []AssetRef{{Name: "asset.txt", SHA256: hash1, SizeBytes: size1}}
		m1, err := store.SaveAssetManifest(manifest)
		Expect(err).NotTo(HaveOccurred())
		m2, err := store.SaveAssetManifest(manifest)
		Expect(err).NotTo(HaveOccurred())
		Expect(m2).To(Equal(m1))

		Expect(store.SaveRevision(&Revision{})).To(rejectRevisionValidation())
	})

	ginkgo.It("filters non-revision files and returns empty pages for stale cursors", ginkgo.Label("integration"), func() {
		store := NewFSStore(revisionTempDir())
		pageID := newFixturePageID("page-1")
		created := time.Date(2026, 3, 26, 12, 0, 0, 0, time.UTC)
		for i := 0; i < 2; i++ {
			rev := &Revision{ID: RevisionIDFromString(string(rune('a' + i))), PageID: pageID, CreatedAt: created.Add(time.Duration(i) * time.Minute), Type: RevisionTypeContentUpdate, Title: "Page", Slug: "page"}
			Expect(store.SaveRevision(rev)).To(Succeed())
		}

		dir := store.revisionsPageDir(pageID)
		Expect(os.MkdirAll(filepath.Join(dir, "ignored-dir"), 0o755)).To(Succeed())
		Expect(os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("ignore"), 0o644)).To(Succeed())

		names, err := store.revisionFileNames(pageID)
		Expect(err).NotTo(HaveOccurred())
		Expect(names).To(HaveLen(2))

		got, next, err := store.ListRevisionsPage(pageID, "missing-cursor", 1)
		Expect(err).NotTo(HaveOccurred())
		Expect(got).To(BeEmpty())
		Expect(next).To(BeEmpty())

		brokenDir := store.revisionsPageDir("broken-page")
		Expect(os.MkdirAll(brokenDir, 0o755)).To(Succeed())
		Expect(os.WriteFile(filepath.Join(brokenDir, "20260326T120000.000000000Z_a.json"), []byte("{"), 0o644)).To(Succeed())
		_, err = store.GetLatestRevision("broken-page")
		Expect(err).To(MatchJSONSyntaxError())
	})

	ginkgo.It("rejects unsafe revision identifiers", ginkgo.Label("unit"), func() {
		store := NewFSStore(revisionTempDir())
		rev := &Revision{
			ID:        "x/../../outside",
			PageID:    "page-1",
			CreatedAt: time.Now().UTC(),
			Type:      RevisionTypeContentUpdate,
			Title:     "Page",
			Slug:      "page",
		}

		err := store.SaveRevision(rev)
		Expect(err).To(rejectRevisionValidation())
	})

	ginkgo.It("rejects invalid page identifiers before revision identifiers", ginkgo.Label("unit"), func() {
		store := NewFSStore(revisionTempDir())
		rev := &Revision{
			ID:        "x/../../outside",
			PageID:    "../page-1",
			CreatedAt: time.Now().UTC(),
			Type:      RevisionTypeContentUpdate,
			Title:     "Page",
			Slug:      "page",
		}

		err := store.SaveRevision(rev)
		Expect(err).To(rejectRevisionValidation())
	})

	ginkgo.It("round-trips JSON helpers and keeps nil localized errors inert", ginkgo.Label("unit"), func() {
		path := filepath.Join(revisionTempDir(), "value.json")
		payload := map[string]string{"a": "b"}
		Expect(writeJSONAtomic(path, payload)).To(Succeed())
		var got map[string]string
		Expect(readJSON(path, &got)).To(Succeed())
		Expect(got).To(HaveKeyWithValue("a", "b"))

		badPath := filepath.Join(revisionTempDir(), "bad.json")
		Expect(os.WriteFile(badPath, []byte("{"), 0o644)).To(Succeed())
		Expect(readJSON(badPath, &got)).To(MatchJSONSyntaxError())

		var localized *sharederrors.LocalizedError
		Expect(localized.Error()).To(BeEmpty())
		Expect(localized.Unwrap()).To(Succeed())
	})

	ginkgo.It("accepts storage-safe identifiers and rejects path traversal", ginkgo.Label("unit"), func() {
		good := []string{"page-1", "abc123", "some-uuid-here", "a"}
		for _, id := range good {
			Expect(validateStorageID(id)).To(Succeed())
		}

		bad := []string{
			"",
			"  ",
			".",
			"..",
			"../other",
			"../../etc/passwd",
			"foo/bar",
			`foo\bar`,
		}
		for _, id := range bad {
			Expect(validateStorageID(id)).To(rejectRevisionValidation())
		}
	})

	ginkgo.Describe("path traversal page identifiers", ginkgo.Label("unit"), func() {
		for _, tc := range invalidRevisionPageIDCases {
			tc := tc
			ginkgo.It(tc.name, func() {
				store := NewFSStore(revisionTempDir())
				pageID := tree.PageIDFromString(tc.id)
				_, _, err := store.ListRevisionsPage(pageID, "", 50)
				Expect(err).To(rejectRevisionValidation())
				_, err = store.GetLatestRevision(pageID)
				Expect(err).To(rejectRevisionValidation())
				_, err = store.GetRevision(pageID, "rev1")
				Expect(err).To(rejectRevisionValidation())
				Expect(store.PruneRevisions(pageID, 5)).To(rejectRevisionValidation())
			})
		}
	})

	ginkgo.It("returns an error when a live asset blob source is missing", ginkgo.Label("integration"), func() {
		store := NewFSStore(revisionTempDir())
		_, _, err := store.SaveAssetBlobFromPath(filepath.Join(revisionTempDir(), "missing.txt"))
		Expect(err).To(matchRevisionError(os.ErrNotExist))
	})

	ginkgo.It("surfaces filesystem errors when the store root is not a directory", ginkgo.Label("integration"), func() {
		root := revisionTempDir()
		invalidBase := filepath.Join(root, "not-a-dir")
		Expect(os.WriteFile(invalidBase, []byte("x"), 0o644)).To(Succeed())
		store := NewFSStore(invalidBase)

		_, err := store.SaveContentBlob([]byte("hello"))
		Expect(err).To(matchRevisionError(syscall.ENOTDIR))

		src := filepath.Join(root, "asset.txt")
		Expect(os.WriteFile(src, []byte("asset"), 0o644)).To(Succeed())
		_, _, err = store.SaveAssetBlobFromPath(src)
		Expect(err).To(matchRevisionError(syscall.ENOTDIR))
		_, err = store.SaveAssetManifest([]AssetRef{{Name: "asset.txt", SHA256: "abc", SizeBytes: 5}})
		Expect(err).To(matchRevisionError(syscall.ENOTDIR))

		rev := &Revision{ID: "rev1", PageID: "page-1", CreatedAt: time.Now().UTC(), Type: RevisionTypeContentUpdate, Title: "Page", Slug: "page"}
		Expect(store.SaveRevision(rev)).To(matchRevisionError(syscall.ENOTDIR))
		_, err = store.ListRevisions("page-1")
		Expect(err).To(matchRevisionError(syscall.ENOTDIR))
	})

	ginkgo.It("uses and backfills revision indexes for direct lookups", ginkgo.Label("integration"), func() {
		store := NewFSStore(revisionTempDir())
		created := time.Date(2026, 3, 26, 12, 30, 0, 0, time.UTC)
		rev := &Revision{ID: "rev-index", PageID: "page-1", CreatedAt: created, Type: RevisionTypeContentUpdate, Title: "Page", Slug: "page"}
		Expect(store.SaveRevision(rev)).To(Succeed())

		index, err := store.loadRevisionIndex("page-1")
		Expect(err).NotTo(HaveOccurred())
		Expect(index).To(HaveKey(rev.ID.CommitID()))

		Expect(os.Remove(store.revisionIndexPath("page-1"))).To(Succeed())
		got, err := store.GetRevision("page-1", rev.ID)
		Expect(err).NotTo(HaveOccurred())
		Expect(got).To(HaveField("ID", rev.ID))
		index, err = store.loadRevisionIndex("page-1")
		Expect(err).NotTo(HaveOccurred())
		Expect(index).To(HaveKey(rev.ID.CommitID()))
	})
})
