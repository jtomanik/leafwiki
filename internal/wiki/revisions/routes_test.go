package revisions

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"

	"github.com/gin-gonic/gin"
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/gstruct"

	coreauth "github.com/perber/wiki/internal/core/auth"
	"github.com/perber/wiki/internal/core/revision"
	"github.com/perber/wiki/internal/core/tree"
	httpinternal "github.com/perber/wiki/internal/http"
	"github.com/perber/wiki/internal/workspacesync"
)

var _ = ginkgo.Describe("revision routes", ginkgo.Label("integration"), func() {
	ginkgo.It("exposes revision API routes through the shared router", func() {
		router := httpinternal.NewRouter(
			[]httpinternal.RouteRegistrar{NewRoutes(RoutesConfig{})},
			httpinternal.FrontendConfig{},
			httpinternal.RouterOptions{AuthDisabled: true, DisableFrontendRoutes: true},
		)

		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/pages/page-1/revisions", nil))

		Expect(rec).To(HaveRevisionRouteError(http.StatusInternalServerError, ErrCodeRevisionInternalError), rec.Body.String())
	})

	ginkgo.It("passes pagination inputs to revision listing and returns the next cursor", func() {
		gin.SetMode(gin.TestMode)
		treeService := tree.NewTreeServiceWithOptions(tree.TreeOptions{
			DataDir: newRevisionTempDir(),
			RootDir: newRevisionTempDir(),
		})
		Expect(treeService.LoadTree()).To(Succeed())
		kind := tree.NodeKindPage
		pageID, err := treeService.CreateNode(newFixtureUserID("alice"), nil, "Page A", newFixtureSlug("page-a"), &kind)
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
						Slug:     newFixtureSlug("page-a"),
						Kind:     tree.NodeKindPage,
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

		Expect(rec).To(HaveHTTPStatus(http.StatusOK), rec.Body.String())
		Expect(seenCursor).To(Equal("rev-5"))
		Expect(seenLimit).To(Equal(workspacesync.PageRevisionLimit(1)))
		var body struct {
			Revisions  []*RevisionResponse `json:"revisions"`
			NextCursor string              `json:"nextCursor"`
		}
		Expect(json.Unmarshal(rec.Body.Bytes(), &body)).To(Succeed())
		Expect(body).To(gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
			"Revisions":  ConsistOf(HaveField("ID", Equal("rev-3"))),
			"NextCursor": Equal("rev-3"),
		}))
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
		Expect(rec).To(HaveRevisionRouteError(http.StatusBadRequest, ErrCodeRevisionInvalidPageID), rec.Body.String())

		rec = performRevisionHandlerRequest(
			NewRoutes(RoutesConfig{TreeService: fixture.treeService}).handleListRevisions,
			http.MethodGet,
			"/api/pages/"+fixture.pageID.MetadataValue()+"/revisions?limit=bad",
			gin.Params{{Key: "id", Value: fixture.pageID.MetadataValue()}},
			nil,
		)
		Expect(rec).To(HaveRevisionRouteError(http.StatusBadRequest, ErrCodeRevisionInvalidLimit), rec.Body.String())

		rec = performRevisionHandlerRequest(
			NewRoutes(RoutesConfig{TreeService: fixture.treeService}).handleListRevisions,
			http.MethodGet,
			"/api/pages/"+fixture.pageID.MetadataValue()+"/revisions?limit=0",
			gin.Params{{Key: "id", Value: fixture.pageID.MetadataValue()}},
			nil,
		)
		Expect(rec).To(HaveRevisionRouteError(http.StatusBadRequest, ErrCodeRevisionInvalidLimit), rec.Body.String())

		rec = performRevisionHandlerRequest(
			NewRoutes(RoutesConfig{TreeService: fixture.treeService}).handleListRevisions,
			http.MethodGet,
			"/api/pages/"+fixture.pageID.MetadataValue()+"/revisions",
			gin.Params{{Key: "id", Value: fixture.pageID.MetadataValue()}},
			nil,
		)
		Expect(rec).To(HaveRevisionRouteError(http.StatusInternalServerError, ErrCodeRevisionInternalError), rec.Body.String())

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
		Expect(rec).To(HaveRevisionRouteError(http.StatusInternalServerError, ErrCodeRevisionInternalError), rec.Body.String())

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
		Expect(rec).To(HaveRevisionRouteError(http.StatusNotFound, ErrCodeRevisionNotFound), rec.Body.String())

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
		Expect(rec).To(HaveRevisionRouteError(http.StatusNotFound, ErrCodeRevisionNotFound), rec.Body.String())
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
		Expect(rec).To(HaveRevisionRouteError(http.StatusBadRequest, ErrCodeRevisionInvalidRevisionID), rec.Body.String())

		rec = performRevisionHandlerRequest(
			NewRoutes(RoutesConfig{TreeService: fixture.treeService}).handleGetRevision,
			http.MethodGet,
			"/api/pages/"+fixture.pageID.MetadataValue()+"/revisions/"+revisionID.CommitID(),
			gin.Params{{Key: "id", Value: fixture.pageID.MetadataValue()}, {Key: "revisionId", Value: revisionID.CommitID()}},
			nil,
		)
		Expect(rec).To(HaveRevisionRouteError(http.StatusInternalServerError, ErrCodeRevisionInternalError), rec.Body.String())

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
		Expect(rec).To(HaveRevisionRouteError(http.StatusNotFound, ErrCodeRevisionNotFound), rec.Body.String())

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
		Expect(rec).To(HaveHTTPStatus(http.StatusOK), rec.Body.String())
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
		Expect(rec).To(HaveRevisionRouteError(http.StatusNotFound, ErrCodeRevisionNotFound), rec.Body.String())
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
		Expect(rec).To(HaveRevisionRouteError(http.StatusBadRequest, ErrCodeRevisionInvalidPageID), rec.Body.String())

		rec = performRevisionHandlerRequest(
			NewRoutes(RoutesConfig{TreeService: fixture.treeService}).handleGetLatestRevision,
			http.MethodGet,
			"/api/pages/"+fixture.pageID.MetadataValue()+"/revisions/latest",
			gin.Params{{Key: "id", Value: fixture.pageID.MetadataValue()}},
			nil,
		)
		Expect(rec).To(HaveRevisionRouteError(http.StatusInternalServerError, ErrCodeRevisionInternalError), rec.Body.String())

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
		Expect(rec).To(HaveRevisionRouteError(http.StatusNotFound, ErrCodeRevisionNotFound), rec.Body.String())

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
		Expect(rec).To(HaveHTTPStatus(http.StatusOK), rec.Body.String())
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
		Expect(rec).To(HaveRevisionRouteError(http.StatusBadRequest, ErrCodeRevisionCompareInvalidRequest), rec.Body.String())

		rec = performRevisionHandlerRequest(
			NewRoutes(RoutesConfig{TreeService: fixture.treeService}).handleCompareRevisions,
			http.MethodGet,
			"/api/pages/"+fixture.pageID.MetadataValue()+"/revisions/compare?base=base&target=target",
			gin.Params{{Key: "id", Value: fixture.pageID.MetadataValue()}},
			nil,
		)
		Expect(rec).To(HaveRevisionRouteError(http.StatusInternalServerError, ErrCodeRevisionInternalError), rec.Body.String())

		rec = performRevisionHandlerRequest(
			NewRoutes(RoutesConfig{
				GetWorkspaceRevision: func(context.Context, *tree.Page, revision.RevisionID) (*revision.RevisionSnapshot, error) {
					return nil, os.ErrNotExist
				},
			}).handleCompareRevisions,
			http.MethodGet,
			"/api/pages/"+fixture.pageID.MetadataValue()+"/revisions/compare?base=base&target=target",
			gin.Params{{Key: "id", Value: fixture.pageID.MetadataValue()}},
			nil,
		)
		Expect(rec).To(HaveRevisionRouteError(http.StatusNotFound, ErrCodeRevisionNotFound), rec.Body.String())

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
		Expect(rec).To(HaveRevisionRouteError(http.StatusNotFound, ErrCodeRevisionNotFound), rec.Body.String())

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
		Expect(rec).To(HaveHTTPStatus(http.StatusOK), rec.Body.String())
		var comparison RevisionComparisonResponse
		Expect(json.Unmarshal(rec.Body.Bytes(), &comparison)).To(Succeed())
		Expect(comparison).To(matchContentChangedRevisionComparison("old", "new", nil))
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
		Expect(rec).To(HaveRevisionRouteError(http.StatusBadRequest, ErrCodeRevisionPreviewAssetInvalidName), rec.Body.String())

		rec = performRevisionHandlerRequest(
			NewRoutes(RoutesConfig{}).handleGetRevisionAsset,
			http.MethodGet,
			"/api/pages/"+fixture.pageID.MetadataValue()+"/revisions/"+revisionID.CommitID()+"/assets/image.png",
			gin.Params{{Key: "id", Value: fixture.pageID.MetadataValue()}, {Key: "revisionId", Value: revisionID.CommitID()}, {Key: "name", Value: "/image.png"}},
			nil,
		)
		Expect(rec).To(HaveRevisionRouteError(http.StatusNotFound, ErrCodeRevisionPreviewAssetNotFound), rec.Body.String())
	})

	ginkgo.It("restores revisions and maps auth, lookup, and backend failures", func() {
		fixture := newRevisionRouteFixture()
		revisionID := newFixtureRevisionID("rev-restore")
		user := &coreauth.User{ID: newFixtureUserID("alice"), Username: "Alice", Email: "alice@example.test", Role: coreauth.RoleEditor}

		rec := performRevisionHandlerRequest(
			NewRoutes(RoutesConfig{}).handleRestoreRevision,
			http.MethodPost,
			"/api/pages/"+fixture.pageID.MetadataValue()+"/revisions/%20/restore",
			gin.Params{{Key: "id", Value: fixture.pageID.MetadataValue()}, {Key: "revisionId", Value: " "}},
			user,
		)
		Expect(rec).To(HaveRevisionRouteError(http.StatusBadRequest, ErrCodeRevisionInvalidRevisionID), rec.Body.String())

		rec = performRevisionHandlerRequest(
			NewRoutes(RoutesConfig{}).handleRestoreRevision,
			http.MethodPost,
			"/api/pages/"+fixture.pageID.MetadataValue()+"/revisions/"+revisionID.CommitID()+"/restore",
			gin.Params{{Key: "id", Value: fixture.pageID.MetadataValue()}, {Key: "revisionId", Value: revisionID.CommitID()}},
			nil,
		)
		Expect(rec).To(HaveHTTPStatus(http.StatusForbidden), rec.Body.String())

		rec = performRevisionHandlerRequest(
			NewRoutes(RoutesConfig{TreeService: fixture.treeService}).handleRestoreRevision,
			http.MethodPost,
			"/api/pages/"+fixture.pageID.MetadataValue()+"/revisions/"+revisionID.CommitID()+"/restore",
			gin.Params{{Key: "id", Value: fixture.pageID.MetadataValue()}, {Key: "revisionId", Value: revisionID.CommitID()}},
			user,
		)
		Expect(rec).To(HaveRevisionRouteError(http.StatusInternalServerError, ErrCodeRevisionInternalError), rec.Body.String())

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
		Expect(rec).To(HaveRevisionRouteError(http.StatusNotFound, ErrCodeRevisionNotFound), rec.Body.String())

		rec = performRevisionHandlerRequest(
			NewRoutes(RoutesConfig{
				TreeService: fixture.treeService,
				RestoreWorkspaceRevision: func(context.Context, *tree.Page, revision.RevisionID, workspacesync.Actor, workspacesync.Source) (*tree.Page, error) {
					return nil, os.ErrNotExist
				},
			}).handleRestoreRevision,
			http.MethodPost,
			"/api/pages/missing-page/revisions/"+revisionID.CommitID()+"/restore",
			gin.Params{{Key: "id", Value: "missing-page"}, {Key: "revisionId", Value: revisionID.CommitID()}},
			user,
		)
		Expect(rec).To(HaveRevisionRouteError(http.StatusNotFound, ErrCodeRevisionNotFound), rec.Body.String())

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
		Expect(rec).To(HaveHTTPStatus(http.StatusOK), rec.Body.String())
		Expect(seenActor).To(gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
			"ID":    Equal(workspacesync.ActorIDFromUserID(coreauth.UserIDFromString("alice"))),
			"Name":  Equal("Alice"),
			"Email": Equal("alice@example.test"),
		}))
		Expect(seenSource).To(Equal(workspacesync.SourceWeb))
	})
})
