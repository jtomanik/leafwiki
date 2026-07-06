package revision

import (
	"os"
	"path/filepath"
	"time"

	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = ginkgo.Describe("revision store persistence behavior", func() {
	ginkgo.It("NewRevisionIDUnchecked preserves the raw commit identifier", ginkgo.Label("unit"), func() {
		id := RevisionIDFromString(" rev-raw ")

		Expect(id.CommitID()).To(Equal(" rev-raw "))
	})

	ginkgo.It("PruneRevisions keeps the newest revisions and removes pruned IDs from the index", ginkgo.Label("integration"), func() {
		store := NewFSStore(revisionTempDir())
		pageID := newFixturePageID("page-prune")
		createdAt := time.Date(2026, 6, 24, 12, 0, 0, 0, time.UTC)

		for i, revisionID := range []RevisionID{
			newFixtureRevisionID("rev-1"),
			newFixtureRevisionID("rev-2"),
			newFixtureRevisionID("rev-3"),
			newFixtureRevisionID("rev-4"),
		} {
			err := store.SaveRevision(&Revision{
				ID:        revisionID,
				PageID:    pageID,
				CreatedAt: createdAt.Add(time.Duration(i) * time.Minute),
				Type:      RevisionTypeContentUpdate,
				Title:     "Page",
				Slug:      newFixtureSlug("page"),
			})
			Expect(err).NotTo(HaveOccurred())
		}

		Expect(store.PruneRevisions(pageID, 2)).To(Succeed())

		revisions, err := store.ListRevisions(pageID)
		Expect(err).NotTo(HaveOccurred())
		Expect(revisions).To(HaveExactElements(
			HaveField("ID", newFixtureRevisionID("rev-4")),
			HaveField("ID", newFixtureRevisionID("rev-3")),
		))

		_, err = store.GetRevision(pageID, newFixtureRevisionID("rev-1"))
		Expect(err).To(MatchError(os.ErrNotExist))
		_, err = store.GetRevision(pageID, newFixtureRevisionID("rev-2"))
		Expect(err).To(MatchError(os.ErrNotExist))

		index, err := store.loadRevisionIndex(pageID)
		Expect(err).NotTo(HaveOccurred())
		Expect(index).NotTo(HaveKey(newFixtureRevisionID("rev-1").CommitID()))
		Expect(index).NotTo(HaveKey(newFixtureRevisionID("rev-2").CommitID()))
		Expect(index).To(HaveKey(newFixtureRevisionID("rev-3").CommitID()))
		Expect(index).To(HaveKey(newFixtureRevisionID("rev-4").CommitID()))
	})

	ginkgo.It("PruneRevisions keep count boundaries are no-ops for zero and already-small histories", ginkgo.Label("integration"), func() {
		store := NewFSStore(revisionTempDir())
		pageID := newFixturePageID("page-prune-boundary")
		revisionID := newFixtureRevisionID("rev-only")
		Expect(store.SaveRevision(&Revision{
			ID:        revisionID,
			PageID:    pageID,
			CreatedAt: time.Date(2026, 6, 24, 12, 0, 0, 0, time.UTC),
			Type:      RevisionTypeContentUpdate,
			Title:     "Page",
			Slug:      newFixtureSlug("page"),
		})).To(Succeed())

		Expect(store.PruneRevisions(pageID, 0)).To(Succeed())
		Expect(store.PruneRevisions(pageID, 5)).To(Succeed())

		rev, err := store.GetRevision(pageID, revisionID)
		Expect(err).NotTo(HaveOccurred())
		Expect(rev.ID).To(Equal(revisionID))
	})

	ginkgo.It("CopyAssetBlobToPath restores a stored asset blob with matching hash and size", ginkgo.Label("integration"), func() {
		tmp := revisionTempDir()
		store := NewFSStore(tmp)
		sourcePath := filepath.Join(tmp, "asset.txt")
		Expect(os.WriteFile(sourcePath, []byte("asset-data"), 0o644)).To(Succeed())

		hash, size, err := store.SaveAssetBlobFromPath(sourcePath)
		Expect(err).NotTo(HaveOccurred())

		dstPath := filepath.Join(tmp, "restored", "asset.txt")
		Expect(os.MkdirAll(filepath.Dir(dstPath), 0o755)).To(Succeed())
		Expect(store.CopyAssetBlobToPath(hash, size, dstPath)).To(Succeed())

		raw, err := os.ReadFile(dstPath)
		Expect(err).NotTo(HaveOccurred())
		Expect(string(raw)).To(Equal("asset-data"))
	})
})
