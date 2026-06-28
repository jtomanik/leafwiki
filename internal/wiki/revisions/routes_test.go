package revisions

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"time"

	"github.com/gin-gonic/gin"
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	coreauth "github.com/perber/wiki/internal/core/auth"
	"github.com/perber/wiki/internal/core/revision"
	sharederrors "github.com/perber/wiki/internal/core/shared/errors"
	"github.com/perber/wiki/internal/core/tree"
	httpinternal "github.com/perber/wiki/internal/http"
	"github.com/perber/wiki/internal/workspacesync"
)

var _ = ginkgo.Describe("revision routes", func() {
	ginkgo.It("RegisterRoutes wires revision API routes through the shared router", func() {
		router := httpinternal.NewRouter(
			[]httpinternal.RouteRegistrar{NewRoutes(RoutesConfig{})},
			httpinternal.FrontendConfig{},
			httpinternal.RouterOptions{AuthDisabled: true, DisableFrontendRoutes: true},
		)

		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/pages/page-1/revisions", nil))

		expectRevisionRouteError(rec, http.StatusInternalServerError, ErrCodeRevisionInternalError)
	})

	ginkgo.It("TestRoutesListWorkspaceRevisionsPassesCursorAndReturnsNextCursor", func() {
		gin.SetMode(gin.TestMode)
		treeService := tree.NewTreeServiceWithOptions(tree.TreeOptions{
			DataDir: ginkgo.GinkgoT().TempDir(),
			RootDir: ginkgo.GinkgoT().TempDir(),
		})
		Expect(treeService.LoadTree()).To(Succeed())
		kind := tree.NodeKindPage
		pageID, err := treeService.CreateNode("alice", nil, "Page A", "page-a", &kind)
		Expect(err).NotTo(HaveOccurred())

		var seenCursor string
		var seenLimit workspacesync.PageRevisionLimit
		routes := NewRoutes(RoutesConfig{
			TreeService: treeService,
			ListWorkspaceRevisions: func(_ context.Context, page *tree.Page, cursor string, limit workspacesync.PageRevisionLimit) (workspacesync.PageRevisionList, error) {
				Expect(page.ID).To(Equal(*pageID))
				seenCursor = cursor
				seenLimit = limit
				return workspacesync.PageRevisionList{
					Revisions: []*revision.Revision{{
						ID:       newFixtureRevisionID("rev-3"),
						PageID:   *pageID,
						Type:     revision.RevisionTypeContentUpdate,
						AuthorID: "alice",
						Title:    "Page A",
						Slug:     "page-a",
						Kind:     string(tree.NodeKindPage),
						Path:     "page-a",
					}},
					NextCursor: "rev-3",
				}, nil
			},
		})

		pageIDValue := pageID.MetadataValue()
		req := httptest.NewRequest(http.MethodGet, "/api/pages/"+pageIDValue+"/revisions?cursor=rev-5&limit=1", nil)
		rec := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(rec)
		c.Request = req
		c.Params = gin.Params{{Key: "id", Value: pageIDValue}}

		routes.handleListRevisions(c)

		Expect(rec.Code).To(Equal(http.StatusOK), rec.Body.String())
		Expect(seenCursor).To(Equal("rev-5"))
		Expect(seenLimit).To(Equal(workspacesync.PageRevisionLimit(1)))
		var body struct {
			Revisions  []*RevisionResponse `json:"revisions"`
			NextCursor string              `json:"nextCursor"`
		}
		Expect(json.Unmarshal(rec.Body.Bytes(), &body)).To(Succeed())
		Expect(body.Revisions).To(HaveLen(1))
		Expect(body.Revisions[0].ID).To(Equal("rev-3"))
		Expect(body.NextCursor).To(Equal("rev-3"))
	})

	ginkgo.It("returns structured list revision errors for malformed requests and unavailable dependencies", func() {
		fixture := newRevisionRouteFixture()

		rec := performRevisionHandlerRequest(
			NewRoutes(RoutesConfig{}).handleListRevisions,
			http.MethodGet,
			"/api/pages/%20/revisions",
			gin.Params{{Key: "id", Value: " "}},
			nil,
		)
		expectRevisionRouteError(rec, http.StatusBadRequest, ErrCodeRevisionInvalidPageID)

		rec = performRevisionHandlerRequest(
			NewRoutes(RoutesConfig{TreeService: fixture.treeService}).handleListRevisions,
			http.MethodGet,
			"/api/pages/"+fixture.pageID.MetadataValue()+"/revisions?limit=bad",
			gin.Params{{Key: "id", Value: fixture.pageID.MetadataValue()}},
			nil,
		)
		expectRevisionRouteError(rec, http.StatusBadRequest, ErrCodeRevisionInvalidLimit)

		rec = performRevisionHandlerRequest(
			NewRoutes(RoutesConfig{TreeService: fixture.treeService}).handleListRevisions,
			http.MethodGet,
			"/api/pages/"+fixture.pageID.MetadataValue()+"/revisions?limit=0",
			gin.Params{{Key: "id", Value: fixture.pageID.MetadataValue()}},
			nil,
		)
		expectRevisionRouteError(rec, http.StatusBadRequest, ErrCodeRevisionInvalidLimit)

		rec = performRevisionHandlerRequest(
			NewRoutes(RoutesConfig{TreeService: fixture.treeService}).handleListRevisions,
			http.MethodGet,
			"/api/pages/"+fixture.pageID.MetadataValue()+"/revisions",
			gin.Params{{Key: "id", Value: fixture.pageID.MetadataValue()}},
			nil,
		)
		expectRevisionRouteError(rec, http.StatusInternalServerError, ErrCodeRevisionInternalError)

		rec = performRevisionHandlerRequest(
			NewRoutes(RoutesConfig{
				TreeService: fixture.treeService,
				ListWorkspaceRevisions: func(context.Context, *tree.Page, string, workspacesync.PageRevisionLimit) (workspacesync.PageRevisionList, error) {
					return workspacesync.PageRevisionList{}, errors.New("backend unavailable")
				},
			}).handleListRevisions,
			http.MethodGet,
			"/api/pages/"+fixture.pageID.MetadataValue()+"/revisions",
			gin.Params{{Key: "id", Value: fixture.pageID.MetadataValue()}},
			nil,
		)
		expectRevisionRouteError(rec, http.StatusInternalServerError, ErrCodeRevisionInternalError)

		rec = performRevisionHandlerRequest(
			NewRoutes(RoutesConfig{
				ListWorkspaceRevisions: func(context.Context, *tree.Page, string, workspacesync.PageRevisionLimit) (workspacesync.PageRevisionList, error) {
					return workspacesync.PageRevisionList{}, nil
				},
			}).handleGetLatestRevision,
			http.MethodGet,
			"/api/pages/"+fixture.pageID.MetadataValue()+"/revisions/latest",
			gin.Params{{Key: "id", Value: fixture.pageID.MetadataValue()}},
			nil,
		)
		expectRevisionRouteError(rec, http.StatusNotFound, ErrCodeRevisionNotFound)

		rec = performRevisionHandlerRequest(
			NewRoutes(RoutesConfig{
				TreeService: fixture.treeService,
				ListWorkspaceRevisions: func(context.Context, *tree.Page, string, workspacesync.PageRevisionLimit) (workspacesync.PageRevisionList, error) {
					return workspacesync.PageRevisionList{}, nil
				},
			}).handleListRevisions,
			http.MethodGet,
			"/api/pages/missing-page/revisions",
			gin.Params{{Key: "id", Value: "missing-page"}},
			nil,
		)
		expectRevisionRouteError(rec, http.StatusNotFound, ErrCodeRevisionNotFound)
	})

	ginkgo.It("gets revisions and maps lookup failures", func() {
		fixture := newRevisionRouteFixture()
		revisionID := newFixtureRevisionID("rev-1")

		rec := performRevisionHandlerRequest(
			NewRoutes(RoutesConfig{}).handleGetRevision,
			http.MethodGet,
			"/api/pages/"+fixture.pageID.MetadataValue()+"/revisions/%20",
			gin.Params{{Key: "id", Value: fixture.pageID.MetadataValue()}, {Key: "revisionId", Value: " "}},
			nil,
		)
		expectRevisionRouteError(rec, http.StatusBadRequest, ErrCodeRevisionInvalidRevisionID)

		rec = performRevisionHandlerRequest(
			NewRoutes(RoutesConfig{TreeService: fixture.treeService}).handleGetRevision,
			http.MethodGet,
			"/api/pages/"+fixture.pageID.MetadataValue()+"/revisions/"+revisionID.CommitID(),
			gin.Params{{Key: "id", Value: fixture.pageID.MetadataValue()}, {Key: "revisionId", Value: revisionID.CommitID()}},
			nil,
		)
		expectRevisionRouteError(rec, http.StatusInternalServerError, ErrCodeRevisionInternalError)

		rec = performRevisionHandlerRequest(
			NewRoutes(RoutesConfig{
				TreeService: fixture.treeService,
				GetWorkspaceRevision: func(context.Context, *tree.Page, revision.RevisionID) (*revision.RevisionSnapshot, error) {
					return nil, errors.New("missing revision")
				},
			}).handleGetRevision,
			http.MethodGet,
			"/api/pages/"+fixture.pageID.MetadataValue()+"/revisions/"+revisionID.CommitID(),
			gin.Params{{Key: "id", Value: fixture.pageID.MetadataValue()}, {Key: "revisionId", Value: revisionID.CommitID()}},
			nil,
		)
		expectRevisionRouteError(rec, http.StatusNotFound, ErrCodeRevisionNotFound)

		rec = performRevisionHandlerRequest(
			NewRoutes(RoutesConfig{
				TreeService: fixture.treeService,
				GetWorkspaceRevision: func(context.Context, *tree.Page, revision.RevisionID) (*revision.RevisionSnapshot, error) {
					return revisionSnapshotFor(fixture.pageID, revisionID, "snapshot body"), nil
				},
			}).handleGetRevision,
			http.MethodGet,
			"/api/pages/"+fixture.pageID.MetadataValue()+"/revisions/"+revisionID.CommitID(),
			gin.Params{{Key: "id", Value: fixture.pageID.MetadataValue()}, {Key: "revisionId", Value: revisionID.CommitID()}},
			nil,
		)
		Expect(rec.Code).To(Equal(http.StatusOK), rec.Body.String())
		var snapshot RevisionSnapshotResponse
		Expect(json.Unmarshal(rec.Body.Bytes(), &snapshot)).To(Succeed())
		Expect(snapshot.Content).To(Equal("snapshot body"))
		Expect(snapshot.Revision.ID).To(Equal(revisionID.CommitID()))

		rec = performRevisionHandlerRequest(
			NewRoutes(RoutesConfig{
				TreeService: fixture.treeService,
				GetWorkspaceRevision: func(context.Context, *tree.Page, revision.RevisionID) (*revision.RevisionSnapshot, error) {
					return revisionSnapshotFor(fixture.pageID, revisionID, "snapshot body"), nil
				},
			}).handleGetRevision,
			http.MethodGet,
			"/api/pages/missing-page/revisions/"+revisionID.CommitID(),
			gin.Params{{Key: "id", Value: "missing-page"}, {Key: "revisionId", Value: revisionID.CommitID()}},
			nil,
		)
		expectRevisionRouteError(rec, http.StatusNotFound, ErrCodeRevisionNotFound)
	})

	ginkgo.It("gets the latest revision and reports empty or unavailable history", func() {
		fixture := newRevisionRouteFixture()

		rec := performRevisionHandlerRequest(
			NewRoutes(RoutesConfig{}).handleGetLatestRevision,
			http.MethodGet,
			"/api/pages/%20/revisions/latest",
			gin.Params{{Key: "id", Value: " "}},
			nil,
		)
		expectRevisionRouteError(rec, http.StatusBadRequest, ErrCodeRevisionInvalidPageID)

		rec = performRevisionHandlerRequest(
			NewRoutes(RoutesConfig{TreeService: fixture.treeService}).handleGetLatestRevision,
			http.MethodGet,
			"/api/pages/"+fixture.pageID.MetadataValue()+"/revisions/latest",
			gin.Params{{Key: "id", Value: fixture.pageID.MetadataValue()}},
			nil,
		)
		expectRevisionRouteError(rec, http.StatusInternalServerError, ErrCodeRevisionInternalError)

		rec = performRevisionHandlerRequest(
			NewRoutes(RoutesConfig{
				TreeService: fixture.treeService,
				ListWorkspaceRevisions: func(context.Context, *tree.Page, string, workspacesync.PageRevisionLimit) (workspacesync.PageRevisionList, error) {
					return workspacesync.PageRevisionList{}, nil
				},
			}).handleGetLatestRevision,
			http.MethodGet,
			"/api/pages/"+fixture.pageID.MetadataValue()+"/revisions/latest",
			gin.Params{{Key: "id", Value: fixture.pageID.MetadataValue()}},
			nil,
		)
		expectRevisionRouteError(rec, http.StatusNotFound, ErrCodeRevisionNotFound)

		revisionID := newFixtureRevisionID("rev-latest")
		rec = performRevisionHandlerRequest(
			NewRoutes(RoutesConfig{
				TreeService: fixture.treeService,
				ListWorkspaceRevisions: func(context.Context, *tree.Page, string, workspacesync.PageRevisionLimit) (workspacesync.PageRevisionList, error) {
					return workspacesync.PageRevisionList{Revisions: []*revision.Revision{revisionFor(fixture.pageID, revisionID)}}, nil
				},
			}).handleGetLatestRevision,
			http.MethodGet,
			"/api/pages/"+fixture.pageID.MetadataValue()+"/revisions/latest",
			gin.Params{{Key: "id", Value: fixture.pageID.MetadataValue()}},
			nil,
		)
		Expect(rec.Code).To(Equal(http.StatusOK), rec.Body.String())
		var latest RevisionResponse
		Expect(json.Unmarshal(rec.Body.Bytes(), &latest)).To(Succeed())
		Expect(latest.ID).To(Equal(revisionID.CommitID()))
	})

	ginkgo.It("compares revisions and reports validation, lookup, and backend failures", func() {
		fixture := newRevisionRouteFixture()
		baseID := newFixtureRevisionID("base")
		targetID := newFixtureRevisionID("target")

		rec := performRevisionHandlerRequest(
			NewRoutes(RoutesConfig{}).handleCompareRevisions,
			http.MethodGet,
			"/api/pages/"+fixture.pageID.MetadataValue()+"/revisions/compare?base=base",
			gin.Params{{Key: "id", Value: fixture.pageID.MetadataValue()}},
			nil,
		)
		expectRevisionRouteError(rec, http.StatusBadRequest, ErrCodeRevisionCompareInvalidRequest)

		rec = performRevisionHandlerRequest(
			NewRoutes(RoutesConfig{TreeService: fixture.treeService}).handleCompareRevisions,
			http.MethodGet,
			"/api/pages/"+fixture.pageID.MetadataValue()+"/revisions/compare?base=base&target=target",
			gin.Params{{Key: "id", Value: fixture.pageID.MetadataValue()}},
			nil,
		)
		expectRevisionRouteError(rec, http.StatusInternalServerError, ErrCodeRevisionInternalError)

		rec = performRevisionHandlerRequest(
			NewRoutes(RoutesConfig{
				GetWorkspaceRevision: func(context.Context, *tree.Page, revision.RevisionID) (*revision.RevisionSnapshot, error) {
					return nil, nil
				},
			}).handleCompareRevisions,
			http.MethodGet,
			"/api/pages/"+fixture.pageID.MetadataValue()+"/revisions/compare?base=base&target=target",
			gin.Params{{Key: "id", Value: fixture.pageID.MetadataValue()}},
			nil,
		)
		expectRevisionRouteError(rec, http.StatusNotFound, ErrCodeRevisionNotFound)

		rec = performRevisionHandlerRequest(
			NewRoutes(RoutesConfig{
				TreeService: fixture.treeService,
				GetWorkspaceRevision: func(context.Context, *tree.Page, revision.RevisionID) (*revision.RevisionSnapshot, error) {
					return nil, errors.New("missing")
				},
			}).handleCompareRevisions,
			http.MethodGet,
			"/api/pages/"+fixture.pageID.MetadataValue()+"/revisions/compare?base="+baseID.CommitID()+"&target="+targetID.CommitID(),
			gin.Params{{Key: "id", Value: fixture.pageID.MetadataValue()}},
			nil,
		)
		expectRevisionRouteError(rec, http.StatusNotFound, ErrCodeRevisionNotFound)

		rec = performRevisionHandlerRequest(
			NewRoutes(RoutesConfig{
				TreeService: fixture.treeService,
				GetWorkspaceRevision: func(_ context.Context, _ *tree.Page, revisionID revision.RevisionID) (*revision.RevisionSnapshot, error) {
					switch revisionID {
					case baseID:
						return revisionSnapshotFor(fixture.pageID, baseID, "old"), nil
					case targetID:
						return revisionSnapshotFor(fixture.pageID, targetID, "new"), nil
					default:
						return nil, errors.New("unexpected revision")
					}
				},
			}).handleCompareRevisions,
			http.MethodGet,
			"/api/pages/"+fixture.pageID.MetadataValue()+"/revisions/compare?base="+baseID.CommitID()+"&target="+targetID.CommitID(),
			gin.Params{{Key: "id", Value: fixture.pageID.MetadataValue()}},
			nil,
		)
		Expect(rec.Code).To(Equal(http.StatusOK), rec.Body.String())
		var comparison RevisionComparisonResponse
		Expect(json.Unmarshal(rec.Body.Bytes(), &comparison)).To(Succeed())
		Expect(comparison.ContentChanged).To(BeTrue())
		Expect(comparison.Base.Content).To(Equal("old"))
		Expect(comparison.Target.Content).To(Equal("new"))
	})

	ginkgo.It("reports revision asset validation and unavailable asset previews", func() {
		fixture := newRevisionRouteFixture()
		revisionID := newFixtureRevisionID("rev-asset")

		rec := performRevisionHandlerRequest(
			NewRoutes(RoutesConfig{}).handleGetRevisionAsset,
			http.MethodGet,
			"/api/pages/"+fixture.pageID.MetadataValue()+"/revisions/"+revisionID.CommitID()+"/assets/",
			gin.Params{{Key: "id", Value: fixture.pageID.MetadataValue()}, {Key: "revisionId", Value: revisionID.CommitID()}, {Key: "name", Value: "/"}},
			nil,
		)
		expectRevisionRouteError(rec, http.StatusBadRequest, ErrCodeRevisionPreviewAssetInvalidName)

		rec = performRevisionHandlerRequest(
			NewRoutes(RoutesConfig{}).handleGetRevisionAsset,
			http.MethodGet,
			"/api/pages/"+fixture.pageID.MetadataValue()+"/revisions/"+revisionID.CommitID()+"/assets/image.png",
			gin.Params{{Key: "id", Value: fixture.pageID.MetadataValue()}, {Key: "revisionId", Value: revisionID.CommitID()}, {Key: "name", Value: "/image.png"}},
			nil,
		)
		expectRevisionRouteError(rec, http.StatusNotFound, ErrCodeRevisionPreviewAssetNotFound)
	})

	ginkgo.It("restores revisions and maps auth, lookup, and backend failures", func() {
		fixture := newRevisionRouteFixture()
		revisionID := newFixtureRevisionID("rev-restore")
		user := &coreauth.User{ID: "alice", Username: "Alice", Email: "alice@example.test", Role: coreauth.RoleEditor}

		rec := performRevisionHandlerRequest(
			NewRoutes(RoutesConfig{}).handleRestoreRevision,
			http.MethodPost,
			"/api/pages/"+fixture.pageID.MetadataValue()+"/revisions/%20/restore",
			gin.Params{{Key: "id", Value: fixture.pageID.MetadataValue()}, {Key: "revisionId", Value: " "}},
			user,
		)
		expectRevisionRouteError(rec, http.StatusBadRequest, ErrCodeRevisionInvalidRevisionID)

		rec = performRevisionHandlerRequest(
			NewRoutes(RoutesConfig{}).handleRestoreRevision,
			http.MethodPost,
			"/api/pages/"+fixture.pageID.MetadataValue()+"/revisions/"+revisionID.CommitID()+"/restore",
			gin.Params{{Key: "id", Value: fixture.pageID.MetadataValue()}, {Key: "revisionId", Value: revisionID.CommitID()}},
			nil,
		)
		Expect(rec.Code).To(Equal(http.StatusForbidden), rec.Body.String())

		rec = performRevisionHandlerRequest(
			NewRoutes(RoutesConfig{TreeService: fixture.treeService}).handleRestoreRevision,
			http.MethodPost,
			"/api/pages/"+fixture.pageID.MetadataValue()+"/revisions/"+revisionID.CommitID()+"/restore",
			gin.Params{{Key: "id", Value: fixture.pageID.MetadataValue()}, {Key: "revisionId", Value: revisionID.CommitID()}},
			user,
		)
		expectRevisionRouteError(rec, http.StatusInternalServerError, ErrCodeRevisionInternalError)

		rec = performRevisionHandlerRequest(
			NewRoutes(RoutesConfig{
				TreeService: fixture.treeService,
				RestoreWorkspaceRevision: func(context.Context, *tree.Page, revision.RevisionID, workspacesync.Actor, workspacesync.Source) (*tree.Page, error) {
					return nil, errors.New("missing")
				},
			}).handleRestoreRevision,
			http.MethodPost,
			"/api/pages/"+fixture.pageID.MetadataValue()+"/revisions/"+revisionID.CommitID()+"/restore",
			gin.Params{{Key: "id", Value: fixture.pageID.MetadataValue()}, {Key: "revisionId", Value: revisionID.CommitID()}},
			user,
		)
		expectRevisionRouteError(rec, http.StatusNotFound, ErrCodeRevisionNotFound)

		rec = performRevisionHandlerRequest(
			NewRoutes(RoutesConfig{
				TreeService: fixture.treeService,
				RestoreWorkspaceRevision: func(context.Context, *tree.Page, revision.RevisionID, workspacesync.Actor, workspacesync.Source) (*tree.Page, error) {
					return nil, nil
				},
			}).handleRestoreRevision,
			http.MethodPost,
			"/api/pages/missing-page/revisions/"+revisionID.CommitID()+"/restore",
			gin.Params{{Key: "id", Value: "missing-page"}, {Key: "revisionId", Value: revisionID.CommitID()}},
			user,
		)
		expectRevisionRouteError(rec, http.StatusNotFound, ErrCodeRevisionNotFound)

		var seenActor workspacesync.Actor
		var seenSource workspacesync.Source
		rec = performRevisionHandlerRequest(
			NewRoutes(RoutesConfig{
				TreeService: fixture.treeService,
				RestoreWorkspaceRevision: func(_ context.Context, page *tree.Page, revisionID revision.RevisionID, actor workspacesync.Actor, source workspacesync.Source) (*tree.Page, error) {
					seenActor = actor
					seenSource = source
					Expect(revisionID).To(Equal(newFixtureRevisionID("rev-restore")))
					return page, nil
				},
			}).handleRestoreRevision,
			http.MethodPost,
			"/api/pages/"+fixture.pageID.MetadataValue()+"/revisions/"+revisionID.CommitID()+"/restore",
			gin.Params{{Key: "id", Value: fixture.pageID.MetadataValue()}, {Key: "revisionId", Value: revisionID.CommitID()}},
			user,
		)
		Expect(rec.Code).To(Equal(http.StatusOK), rec.Body.String())
		Expect(seenActor.ID.String()).To(Equal("alice"))
		Expect(seenActor.Name).To(Equal("Alice"))
		Expect(seenActor.Email).To(Equal("alice@example.test"))
		Expect(seenSource).To(Equal(workspacesync.SourceWeb))
	})
})

var _ = ginkgo.Describe("revision response mappers", func() {
	ginkgo.It("ToRevisionResponse returns nil for nil revisions", func() {
		Expect(ToRevisionResponse(nil, nil)).To(BeNil())
	})

	ginkgo.It("ToRevisionResponse includes resolved author labels when a resolver is available", func() {
		store, err := coreauth.NewUserStore(ginkgo.GinkgoT().TempDir())
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
		rev.AuthorID = user.ID

		out := ToRevisionResponse(rev, resolver)
		Expect(out.Author).To(Equal(&coreauth.UserLabel{ID: user.ID, Username: "alice"}))
	})

	ginkgo.It("ToSnapshotResponse returns nil for nil snapshots and maps revision content and assets", func() {
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
				Slug:                 "title",
				Kind:                 string(tree.NodeKindPage),
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

		out := ToSnapshotResponse(snapshot, nil)
		Expect(out.Content).To(Equal("body"))
		Expect(out.Revision.ID).To(Equal("rev-1"))
		Expect(out.Revision.PageID).To(Equal("page-1"))
		Expect(out.Revision.ParentID).To(Equal("parent-1"))
		Expect(out.Revision.CreatedAt).To(Equal(createdAt.Format(time.RFC3339)))
		Expect(out.Revision.PageCreatedAt).To(Equal(createdAt.Format(time.RFC3339)))
		Expect(out.Revision.PageUpdatedAt).To(Equal(createdAt.Add(time.Hour).Format(time.RFC3339)))
		Expect(out.Assets).To(Equal([]RevisionAssetResponse{{
			Name:      "asset.png",
			SHA256:    "sha",
			SizeBytes: 42,
			MIMEType:  "image/png",
		}}))
	})

	ginkgo.It("ToComparisonResponse returns nil for nil comparisons and maps asset deltas", func() {
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

		out := ToComparisonResponse(cmp, nil)
		Expect(out.Base.Content).To(Equal("old"))
		Expect(out.Target.Content).To(Equal("new"))
		Expect(out.ContentChanged).To(BeTrue())
		Expect(out.AssetChanges).To(Equal([]RevisionAssetDeltaResponse{
			{Name: "added.png", Status: "added"},
			{Name: "removed.png", Status: "removed"},
		}))
	})

	ginkgo.It("NormalizeRevisionListLimit handles default, valid, and invalid limits", func() {
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
		expectRevisionErrorCode(err, ErrCodeRevisionInvalidLimit)

		tooLarge := MaxRevisionListLimit + 1
		_, err = NormalizeRevisionListLimit(&tooLarge, pageID)
		expectRevisionErrorCode(err, ErrCodeRevisionInvalidLimit)
	})
})

type revisionRouteFixture struct {
	treeService *tree.TreeService
	pageID      tree.PageID
}

func newRevisionRouteFixture() revisionRouteFixture {
	ginkgo.GinkgoHelper()
	gin.SetMode(gin.TestMode)

	treeService := tree.NewTreeServiceWithOptions(tree.TreeOptions{
		DataDir: ginkgo.GinkgoT().TempDir(),
		RootDir: ginkgo.GinkgoT().TempDir(),
	})
	Expect(treeService.LoadTree()).To(Succeed())
	kind := tree.NodeKindPage
	pageID, err := treeService.CreateNode("alice", nil, "Page A", "page-a", &kind)
	Expect(err).NotTo(HaveOccurred())

	return revisionRouteFixture{
		treeService: treeService,
		pageID:      *pageID,
	}
}

func performRevisionHandlerRequest(handler gin.HandlerFunc, method, target string, params gin.Params, user *coreauth.User) *httptest.ResponseRecorder {
	ginkgo.GinkgoHelper()
	gin.SetMode(gin.TestMode)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(method, target, nil)
	c, _ := gin.CreateTestContext(rec)
	c.Request = req
	c.Params = params
	if user != nil {
		c.Set("user", user)
	}

	handler(c)
	return rec
}

func expectRevisionRouteError(rec *httptest.ResponseRecorder, status int, code sharederrors.ErrorCode) {
	ginkgo.GinkgoHelper()

	Expect(rec.Code).To(Equal(status), rec.Body.String())
	var body RevisionErrorResponse
	Expect(json.Unmarshal(rec.Body.Bytes(), &body)).To(Succeed())
	Expect(body.Error.Code).To(Equal(code))
}

func revisionFor(pageID tree.PageID, revisionID revision.RevisionID) *revision.Revision {
	ginkgo.GinkgoHelper()

	return &revision.Revision{
		ID:        revisionID,
		PageID:    pageID,
		Type:      revision.RevisionTypeContentUpdate,
		AuthorID:  "alice",
		CreatedAt: time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC),
		Title:     "Page A",
		Slug:      "page-a",
		Kind:      string(tree.NodeKindPage),
		Path:      "page-a",
	}
}

func revisionSnapshotFor(pageID tree.PageID, revisionID revision.RevisionID, content string) *revision.RevisionSnapshot {
	ginkgo.GinkgoHelper()

	return &revision.RevisionSnapshot{
		Revision: revisionFor(pageID, revisionID),
		Content:  content,
	}
}
