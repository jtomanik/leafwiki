package revisions

import (
	"time"

	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/gstruct"

	coreauth "github.com/perber/wiki/internal/core/auth"
	"github.com/perber/wiki/internal/core/revision"
	"github.com/perber/wiki/internal/core/tree"
)

var _ = ginkgo.Describe("revision response mappers", func() {
	ginkgo.It("ToRevisionResponse returns nil for nil revisions", ginkgo.Label("unit"), func() {
		Expect(ToRevisionResponse(nil, nil)).To(BeNil())
	})

	ginkgo.It("ToRevisionResponse includes resolved author labels when a resolver is available", ginkgo.Label("integration"), func() {
		store, err := coreauth.NewUserStore(newRevisionTempDir())
		Expect(err).NotTo(HaveOccurred())
		ginkgo.DeferCleanup(func() {
			Expect(store.Close()).To(Succeed())
		})
		userService := coreauth.NewUserService(store)
		user, err := userService.CreateUser("alice", "alice@example.test", "password", coreauth.RoleEditor)
		Expect(err).NotTo(HaveOccurred())
		resolver, err := coreauth.NewUserResolver(userService)
		Expect(err).NotTo(HaveOccurred())

		rev := revisionFor(newFixturePageID("page-1"), newFixtureRevisionID("rev-1"))
		rev.AuthorID = user.ID.MetadataValue()

		out := ToRevisionResponse(rev, resolver)
		Expect(out.Author).To(Equal(&coreauth.UserLabel{ID: user.ID, Username: "alice"}))
	})

	ginkgo.It("ToSnapshotResponse returns nil for nil snapshots and maps revision content and assets", ginkgo.Label("unit"), func() {
		Expect(ToSnapshotResponse(nil, nil)).To(BeNil())

		createdAt := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
		snapshot := &revision.RevisionSnapshot{
			Revision: &revision.Revision{
				ID:                   newFixtureRevisionID("rev-1"),
				PageID:               newFixturePageID("page-1"),
				ParentID:             newFixturePageID("parent-1"),
				Type:                 revision.RevisionTypeContentUpdate,
				AuthorID:             "alice",
				CreatedAt:            createdAt,
				Title:                "Title",
				Slug:                 newFixtureSlug("title"),
				Kind:                 tree.NodeKindPage,
				Path:                 "title",
				ContentHash:          "content-hash",
				AssetManifestHash:    "asset-hash",
				PageCreatedAt:        createdAt,
				PageUpdatedAt:        createdAt.Add(time.Hour),
				CreatorID:            "creator",
				LastAuthorID:         "last-author",
				Summary:              "summary",
				ExtraFrontmatterHash: "extra-hash",
			},
			Content: "body",
			Assets: []revision.AssetRef{{
				Name:      "asset.png",
				SHA256:    "sha",
				SizeBytes: 42,
				MIMEType:  "image/png",
			}},
		}

		Expect(ToSnapshotResponse(snapshot, nil)).To(gstruct.PointTo(gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
			"Content": Equal("body"),
			"Revision": gstruct.PointTo(gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
				"ID":            matchRevisionWireID(newFixtureRevisionID("rev-1")),
				"PageID":        matchRevisionPageWireID(newFixturePageID("page-1")),
				"ParentID":      matchRevisionPageWireID(newFixturePageID("parent-1")),
				"CreatedAt":     Equal(createdAt.Format(time.RFC3339)),
				"PageCreatedAt": Equal(createdAt.Format(time.RFC3339)),
				"PageUpdatedAt": Equal(createdAt.Add(time.Hour).Format(time.RFC3339)),
			})),
			"Assets": Equal([]RevisionAssetResponse{{
				Name:      "asset.png",
				SHA256:    "sha",
				SizeBytes: 42,
				MIMEType:  "image/png",
			}}),
		})))
	})

	ginkgo.It("ToComparisonResponse returns nil for nil comparisons and maps asset deltas", ginkgo.Label("unit"), func() {
		Expect(ToComparisonResponse(nil, nil)).To(BeNil())

		cmp := &revision.RevisionComparison{
			Base:           &revision.RevisionSnapshot{Revision: &revision.Revision{ID: newFixtureRevisionID("base"), PageID: newFixturePageID("page-1")}, Content: "old"},
			Target:         &revision.RevisionSnapshot{Revision: &revision.Revision{ID: newFixtureRevisionID("target"), PageID: newFixturePageID("page-1")}, Content: "new"},
			ContentChanged: true,
			AssetChanges: []revision.RevisionAssetDelta{
				{Name: "added.png", Status: "added"},
				{Name: "removed.png", Status: "removed"},
			},
		}

		Expect(ToComparisonResponse(cmp, nil)).To(gstruct.PointTo(matchContentChangedRevisionComparison(
			"old",
			"new",
			Equal([]RevisionAssetDeltaResponse{
				{Name: "added.png", Status: "added"},
				{Name: "removed.png", Status: "removed"},
			}),
		)))
	})

	ginkgo.It("NormalizeRevisionListLimit handles default, valid, and invalid limits", ginkgo.Label("unit"), func() {
		pageID := newFixturePageID("page-1")
		limit, err := NormalizeRevisionListLimit(nil, pageID)
		Expect(err).NotTo(HaveOccurred())
		Expect(limit).To(Equal(DefaultRevisionListLimit))

		raw := 25
		limit, err = NormalizeRevisionListLimit(&raw, pageID)
		Expect(err).NotTo(HaveOccurred())
		Expect(limit).To(Equal(25))

		invalid := 0
		_, err = NormalizeRevisionListLimit(&invalid, pageID)
		Expect(err).To(MatchRevisionErrorCode(ErrCodeRevisionInvalidLimit))

		tooLarge := MaxRevisionListLimit + 1
		_, err = NormalizeRevisionListLimit(&tooLarge, pageID)
		Expect(err).To(MatchRevisionErrorCode(ErrCodeRevisionInvalidLimit))
	})
})
