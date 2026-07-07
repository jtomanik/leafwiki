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
	"github.com/onsi/gomega/types"

	coreauth "github.com/perber/wiki/internal/core/auth"
	"github.com/perber/wiki/internal/core/revision"
	sharederrors "github.com/perber/wiki/internal/core/shared/errors"
	"github.com/perber/wiki/internal/core/tree"
	httpinternal "github.com/perber/wiki/internal/http"
	"github.com/perber/wiki/internal/http/dto"
	"github.com/perber/wiki/internal/workspacesync"
)

var _ = ginkgo.Describe("revision route handlers", ginkgo.Label("unit"), func() {
	ginkgo.It("constructs and registers revision routes with configured dependencies", func() {
		pageID := newFixturePageID("route-page")
		revisionID := newFixtureRevisionID("route-revision")
		list := func(context.Context, *tree.Page, string, workspacesync.PageRevisionLimit) (workspacesync.PageRevisionList, error) {
			return workspacesync.PageRevisionList{}, nil
		}
		get := func(context.Context, *tree.Page, revision.RevisionID) (*revision.RevisionSnapshot, error) {
			return revisionSnapshotFor(pageID, revisionID, "route body"), nil
		}
		restore := func(context.Context, *tree.Page, revision.RevisionID, workspacesync.Actor, workspacesync.Source) (*tree.Page, error) {
			return revisionUnitPage(pageID, "Route Page", newFixtureRoutePath("route-page")), nil
		}
		routes := NewRoutes(RoutesConfig{
			ListWorkspaceRevisions:   list,
			GetWorkspaceRevision:     get,
			RestoreWorkspaceRevision: restore,
		})
		Expect(routes).To(matchRevisionRouteBackends())
		Expect(NewRoutes(RoutesConfig{TreeService: &tree.TreeService{}})).To(matchRevisionPageStore())

		gin.SetMode(gin.TestMode)
		engine := gin.New()
		routes.RegisterRoutes(httpinternal.RouterContext{
			Base: engine.Group(""),
			Opts: httpinternal.RouterOptions{AuthDisabled: true},
		})
		Expect(revisionRegisteredRoutes(engine)).To(exposeRevisionRouteContract())
	})

	ginkgo.It("lists revisions with normalized pagination", func() {
		pageID := newFixturePageID("page-1")
		revisionID := newFixtureRevisionID("rev-1")
		var seen revisionListObservation
		routes := &Routes{
			treeService: newUnitRevisionPageStore(revisionUnitPage(pageID, "Page", newFixtureRoutePath("page"))),
			listWorkspaceRevisions: func(_ context.Context, page *tree.Page, cursor string, limit workspacesync.PageRevisionLimit) (workspacesync.PageRevisionList, error) {
				seen = revisionListObservation{PageID: page.ID, Cursor: cursor, PageSize: limit}
				return workspacesync.PageRevisionList{
					Revisions:  []*revision.Revision{revisionFor(pageID, revisionID)},
					NextCursor: "next-rev",
				}, nil
			},
		}

		rec := performRevisionHandlerRequest(
			routes.handleListRevisions,
			http.MethodGet,
			"/api/pages/page-1/revisions?cursor=after&limit=2",
			gin.Params{{Key: "id", Value: pageID.MetadataValue()}},
			nil,
		)

		Expect(rec).To(HaveHTTPStatus(http.StatusOK), rec.Body.String())
		Expect(seen).To(Equal(revisionListObservation{PageID: pageID, Cursor: "after", PageSize: workspacesync.PageRevisionLimit(2)}))
		body := decodeRevisionListResponse(rec.Body.Bytes())
		Expect(body).To(matchRevisionListResponse(revisionID, "next-rev"))
	})

	ginkgo.It("writes structured list revision errors", func() {
		pageID := newFixturePageID("page-1")
		missingPageStore := newUnitRevisionPageStore(revisionUnitPage(pageID, "Page", newFixtureRoutePath("page")))
		missingPageStore.getErr = tree.ErrPageNotFound

		cases := []struct {
			name   string
			routes *Routes
			target string
			params gin.Params
			status int
			code   sharederrors.ErrorCode
		}{
			{name: "missing page ID", routes: &Routes{}, target: "/api/pages/%20/revisions", params: gin.Params{{Key: "id", Value: " "}}, status: http.StatusBadRequest, code: ErrCodeRevisionInvalidPageID},
			{name: "invalid limit syntax", routes: &Routes{treeService: newUnitRevisionPageStore(revisionUnitPage(pageID, "Page", newFixtureRoutePath("page")))}, target: "/api/pages/page-1/revisions?limit=bad", params: gin.Params{{Key: "id", Value: "page-1"}}, status: http.StatusBadRequest, code: ErrCodeRevisionInvalidLimit},
			{name: "invalid normalized limit", routes: &Routes{treeService: newUnitRevisionPageStore(revisionUnitPage(pageID, "Page", newFixtureRoutePath("page")))}, target: "/api/pages/page-1/revisions?limit=0", params: gin.Params{{Key: "id", Value: "page-1"}}, status: http.StatusBadRequest, code: ErrCodeRevisionInvalidLimit},
			{name: "missing tree service", routes: &Routes{listWorkspaceRevisions: successfulRevisionList(pageID)}, target: "/api/pages/page-1/revisions", params: gin.Params{{Key: "id", Value: "page-1"}}, status: http.StatusInternalServerError, code: ErrCodeRevisionInternalError},
			{name: "missing backend", routes: &Routes{treeService: newUnitRevisionPageStore(revisionUnitPage(pageID, "Page", newFixtureRoutePath("page")))}, target: "/api/pages/page-1/revisions", params: gin.Params{{Key: "id", Value: "page-1"}}, status: http.StatusInternalServerError, code: ErrCodeRevisionInternalError},
			{name: "missing page", routes: &Routes{treeService: missingPageStore, listWorkspaceRevisions: successfulRevisionList(pageID)}, target: "/api/pages/missing/revisions", params: gin.Params{{Key: "id", Value: "missing"}}, status: http.StatusNotFound, code: ErrCodeRevisionNotFound},
			{name: "backend failure", routes: &Routes{
				treeService: newUnitRevisionPageStore(revisionUnitPage(pageID, "Page", newFixtureRoutePath("page"))),
				listWorkspaceRevisions: func(context.Context, *tree.Page, string, workspacesync.PageRevisionLimit) (workspacesync.PageRevisionList, error) {
					return workspacesync.PageRevisionList{}, errors.New("list failed")
				},
			}, target: "/api/pages/page-1/revisions", params: gin.Params{{Key: "id", Value: "page-1"}}, status: http.StatusInternalServerError, code: ErrCodeRevisionInternalError},
		}

		for _, row := range cases {
			rec := performRevisionHandlerRequest(row.routes.handleListRevisions, http.MethodGet, row.target, row.params, nil)
			Expect(rec).To(HaveRevisionRouteError(row.status, row.code), row.name)
		}
	})

	ginkgo.It("gets a revision snapshot for a semantic page and revision ID", func() {
		pageID := newFixturePageID("page-1")
		revisionID := newFixtureRevisionID("rev-1")
		var seen revisionLookupObservation
		routes := &Routes{
			treeService: newUnitRevisionPageStore(revisionUnitPage(pageID, "Page", newFixtureRoutePath("page"))),
			getWorkspaceRevision: func(_ context.Context, page *tree.Page, revisionID revision.RevisionID) (*revision.RevisionSnapshot, error) {
				seen = revisionLookupObservation{PageID: page.ID, RevisionID: revisionID}
				return revisionSnapshotFor(pageID, revisionID, "snapshot body"), nil
			},
		}

		rec := performRevisionHandlerRequest(
			routes.handleGetRevision,
			http.MethodGet,
			"/api/pages/page-1/revisions/rev-1",
			gin.Params{{Key: "id", Value: "page-1"}, {Key: "revisionId", Value: "rev-1"}},
			nil,
		)

		Expect(rec).To(HaveHTTPStatus(http.StatusOK), rec.Body.String())
		Expect(seen).To(Equal(revisionLookupObservation{PageID: pageID, RevisionID: revisionID}))
		Expect(decodeRevisionSnapshotResponse(rec.Body.Bytes())).To(matchRevisionSnapshot(revisionID, "snapshot body"))
	})

	ginkgo.It("writes structured revision lookup errors", func() {
		pageID := newFixturePageID("page-1")
		store := newUnitRevisionPageStore(revisionUnitPage(pageID, "Page", newFixtureRoutePath("page")))

		invalid := performRevisionHandlerRequest((&Routes{}).handleGetRevision, http.MethodGet, "/api/pages/page-1/revisions/%20", gin.Params{{Key: "id", Value: "page-1"}, {Key: "revisionId", Value: " "}}, nil)
		Expect(invalid).To(HaveRevisionRouteError(http.StatusBadRequest, ErrCodeRevisionInvalidRevisionID), invalid.Body.String())

		missingBackend := performRevisionHandlerRequest((&Routes{treeService: store}).handleGetRevision, http.MethodGet, "/api/pages/page-1/revisions/rev-1", gin.Params{{Key: "id", Value: "page-1"}, {Key: "revisionId", Value: "rev-1"}}, nil)
		Expect(missingBackend).To(HaveRevisionRouteError(http.StatusInternalServerError, ErrCodeRevisionInternalError), missingBackend.Body.String())

		missingPageStore := newUnitRevisionPageStore(revisionUnitPage(pageID, "Page", newFixtureRoutePath("page")))
		missingPageStore.getErr = tree.ErrPageNotFound
		missingPage := performRevisionHandlerRequest((&Routes{
			treeService:          missingPageStore,
			getWorkspaceRevision: successfulRevisionSnapshot(pageID, newFixtureRevisionID("rev-1")),
		}).handleGetRevision, http.MethodGet, "/api/pages/page-1/revisions/rev-1", gin.Params{{Key: "id", Value: "page-1"}, {Key: "revisionId", Value: "rev-1"}}, nil)
		Expect(missingPage).To(HaveRevisionRouteError(http.StatusNotFound, ErrCodeRevisionNotFound), missingPage.Body.String())

		missingSnapshot := performRevisionHandlerRequest((&Routes{
			treeService: store,
			getWorkspaceRevision: func(context.Context, *tree.Page, revision.RevisionID) (*revision.RevisionSnapshot, error) {
				return nil, errors.New("snapshot missing")
			},
		}).handleGetRevision, http.MethodGet, "/api/pages/page-1/revisions/rev-1", gin.Params{{Key: "id", Value: "page-1"}, {Key: "revisionId", Value: "rev-1"}}, nil)
		Expect(missingSnapshot).To(HaveRevisionRouteError(http.StatusNotFound, ErrCodeRevisionNotFound), missingSnapshot.Body.String())
	})

	ginkgo.It("gets the latest revision or reports unavailable history", func() {
		pageID := newFixturePageID("page-1")
		revisionID := newFixtureRevisionID("latest")
		store := newUnitRevisionPageStore(revisionUnitPage(pageID, "Page", newFixtureRoutePath("page")))

		invalid := performRevisionHandlerRequest((&Routes{}).handleGetLatestRevision, http.MethodGet, "/api/pages/%20/revisions/latest", gin.Params{{Key: "id", Value: " "}}, nil)
		Expect(invalid).To(HaveRevisionRouteError(http.StatusBadRequest, ErrCodeRevisionInvalidPageID), invalid.Body.String())

		missingBackend := performRevisionHandlerRequest((&Routes{treeService: store}).handleGetLatestRevision, http.MethodGet, "/api/pages/page-1/revisions/latest", gin.Params{{Key: "id", Value: "page-1"}}, nil)
		Expect(missingBackend).To(HaveRevisionRouteError(http.StatusInternalServerError, ErrCodeRevisionInternalError), missingBackend.Body.String())

		missingPageStore := newUnitRevisionPageStore(revisionUnitPage(pageID, "Page", newFixtureRoutePath("page")))
		missingPageStore.getErr = tree.ErrPageNotFound
		missingPage := performRevisionHandlerRequest((&Routes{
			treeService:            missingPageStore,
			listWorkspaceRevisions: successfulRevisionListWith(pageID, revisionID),
		}).handleGetLatestRevision, http.MethodGet, "/api/pages/page-1/revisions/latest", gin.Params{{Key: "id", Value: "page-1"}}, nil)
		Expect(missingPage).To(HaveRevisionRouteError(http.StatusNotFound, ErrCodeRevisionNotFound), missingPage.Body.String())

		emptyHistory := performRevisionHandlerRequest((&Routes{
			treeService:            store,
			listWorkspaceRevisions: successfulRevisionListWith(pageID),
		}).handleGetLatestRevision, http.MethodGet, "/api/pages/page-1/revisions/latest", gin.Params{{Key: "id", Value: "page-1"}}, nil)
		Expect(emptyHistory).To(HaveRevisionRouteError(http.StatusNotFound, ErrCodeRevisionNotFound), emptyHistory.Body.String())

		failedHistory := performRevisionHandlerRequest((&Routes{
			treeService: store,
			listWorkspaceRevisions: func(context.Context, *tree.Page, string, workspacesync.PageRevisionLimit) (workspacesync.PageRevisionList, error) {
				return workspacesync.PageRevisionList{}, errors.New("history failed")
			},
		}).handleGetLatestRevision, http.MethodGet, "/api/pages/page-1/revisions/latest", gin.Params{{Key: "id", Value: "page-1"}}, nil)
		Expect(failedHistory).To(HaveRevisionRouteError(http.StatusNotFound, ErrCodeRevisionNotFound), failedHistory.Body.String())

		success := performRevisionHandlerRequest((&Routes{
			treeService:            store,
			listWorkspaceRevisions: successfulRevisionListWith(pageID, revisionID),
		}).handleGetLatestRevision, http.MethodGet, "/api/pages/page-1/revisions/latest", gin.Params{{Key: "id", Value: "page-1"}}, nil)
		Expect(success).To(HaveHTTPStatus(http.StatusOK), success.Body.String())
		Expect(decodeRevisionResponse(success.Body.Bytes())).To(matchRevisionID(revisionID))
	})

	ginkgo.It("compares revisions and reports compare failures", func() {
		pageID := newFixturePageID("page-1")
		baseID := newFixtureRevisionID("base")
		targetID := newFixtureRevisionID("target")
		store := newUnitRevisionPageStore(revisionUnitPage(pageID, "Page", newFixtureRoutePath("page")))

		invalid := performRevisionHandlerRequest((&Routes{}).handleCompareRevisions, http.MethodGet, "/api/pages/page-1/revisions/compare?base=base", gin.Params{{Key: "id", Value: "page-1"}}, nil)
		Expect(invalid).To(HaveRevisionRouteError(http.StatusBadRequest, ErrCodeRevisionCompareInvalidRequest), invalid.Body.String())

		missingBackend := performRevisionHandlerRequest((&Routes{treeService: store}).handleCompareRevisions, http.MethodGet, "/api/pages/page-1/revisions/compare?base=base&target=target", gin.Params{{Key: "id", Value: "page-1"}}, nil)
		Expect(missingBackend).To(HaveRevisionRouteError(http.StatusInternalServerError, ErrCodeRevisionInternalError), missingBackend.Body.String())

		missingPageStore := newUnitRevisionPageStore(revisionUnitPage(pageID, "Page", newFixtureRoutePath("page")))
		missingPageStore.getErr = tree.ErrPageNotFound
		missingPage := performRevisionHandlerRequest((&Routes{treeService: missingPageStore, getWorkspaceRevision: successfulRevisionSnapshot(pageID, baseID)}).handleCompareRevisions, http.MethodGet, "/api/pages/page-1/revisions/compare?base=base&target=target", gin.Params{{Key: "id", Value: "page-1"}}, nil)
		Expect(missingPage).To(HaveRevisionRouteError(http.StatusNotFound, ErrCodeRevisionNotFound), missingPage.Body.String())

		missingSnapshot := performRevisionHandlerRequest((&Routes{
			treeService: store,
			getWorkspaceRevision: func(context.Context, *tree.Page, revision.RevisionID) (*revision.RevisionSnapshot, error) {
				return nil, errors.New("missing")
			},
		}).handleCompareRevisions, http.MethodGet, "/api/pages/page-1/revisions/compare?base=base&target=target", gin.Params{{Key: "id", Value: "page-1"}}, nil)
		Expect(missingSnapshot).To(HaveRevisionRouteError(http.StatusNotFound, ErrCodeRevisionNotFound), missingSnapshot.Body.String())

		success := performRevisionHandlerRequest((&Routes{
			treeService: store,
			getWorkspaceRevision: func(_ context.Context, _ *tree.Page, revisionID revision.RevisionID) (*revision.RevisionSnapshot, error) {
				if revisionID == baseID {
					return revisionSnapshotFor(pageID, baseID, "old"), nil
				}
				return revisionSnapshotFor(pageID, targetID, "new"), nil
			},
		}).handleCompareRevisions, http.MethodGet, "/api/pages/page-1/revisions/compare?base=base&target=target", gin.Params{{Key: "id", Value: "page-1"}}, nil)
		Expect(success).To(HaveHTTPStatus(http.StatusOK), success.Body.String())
		Expect(decodeRevisionComparisonResponse(success.Body.Bytes())).To(matchContentChangedRevisionComparison("old", "new", nil))
	})

	ginkgo.It("returns unavailable revision assets after validating asset parameters", func() {
		invalid := performRevisionHandlerRequest((&Routes{}).handleGetRevisionAsset, http.MethodGet, "/api/pages/page-1/revisions/rev-1/assets/", gin.Params{{Key: "id", Value: "page-1"}, {Key: "revisionId", Value: "rev-1"}, {Key: "name", Value: "/"}}, nil)
		Expect(invalid).To(HaveRevisionRouteError(http.StatusBadRequest, ErrCodeRevisionPreviewAssetInvalidName), invalid.Body.String())

		missing := performRevisionHandlerRequest((&Routes{}).handleGetRevisionAsset, http.MethodGet, "/api/pages/page-1/revisions/rev-1/assets/image.png", gin.Params{{Key: "id", Value: "page-1"}, {Key: "revisionId", Value: "rev-1"}, {Key: "name", Value: "/image.png"}}, nil)
		Expect(missing).To(HaveRevisionRouteError(http.StatusNotFound, ErrCodeRevisionPreviewAssetNotFound), missing.Body.String())
	})

	ginkgo.It("restores revisions with authenticated actor context", func() {
		pageID := newFixturePageID("page-1")
		revisionID := newFixtureRevisionID("rev-restore")
		page := revisionUnitPage(pageID, "Page", newFixtureRoutePath("page"))
		store := newUnitRevisionPageStore(page)
		store.raw[pageID] = "---\ntags:\n  - restored\nstatus: done\n---\n\nRestored"
		user := &coreauth.User{ID: newFixtureUserID("alice"), Username: "Alice", Email: "alice@example.test", Role: coreauth.RoleEditor}
		var seen revisionRestoreObservation

		rec := performRevisionHandlerRequest((&Routes{
			treeService: store,
			restoreWorkspaceRevision: func(_ context.Context, page *tree.Page, revisionID revision.RevisionID, actor workspacesync.Actor, source workspacesync.Source) (*tree.Page, error) {
				seen = revisionRestoreObservation{PageID: page.ID, RevisionID: revisionID, Actor: actor, Source: source}
				return page, nil
			},
		}).handleRestoreRevision, http.MethodPost, "/api/pages/page-1/revisions/rev-restore/restore", gin.Params{{Key: "id", Value: "page-1"}, {Key: "revisionId", Value: "rev-restore"}}, user)

		Expect(rec).To(HaveHTTPStatus(http.StatusOK), rec.Body.String())
		Expect(seen).To(matchRevisionRestore(pageID, revisionID, workspacesync.ActorIDFromUserID(coreauth.UserIDFromString("alice")), workspacesync.SourceWeb))
		Expect(decodeRevisionRestoredPage(rec.Body.Bytes())).To(matchRestoredPageMetadata([]string{"restored"}, map[string]string{"status": "done"}))
	})

	ginkgo.It("writes structured restore errors", func() {
		pageID := newFixturePageID("page-1")
		revisionID := newFixtureRevisionID("rev-restore")
		store := newUnitRevisionPageStore(revisionUnitPage(pageID, "Page", newFixtureRoutePath("page")))
		user := &coreauth.User{ID: newFixtureUserID("alice"), Username: "Alice", Email: "alice@example.test", Role: coreauth.RoleEditor}

		invalid := performRevisionHandlerRequest((&Routes{}).handleRestoreRevision, http.MethodPost, "/api/pages/page-1/revisions/%20/restore", gin.Params{{Key: "id", Value: "page-1"}, {Key: "revisionId", Value: " "}}, user)
		Expect(invalid).To(HaveRevisionRouteError(http.StatusBadRequest, ErrCodeRevisionInvalidRevisionID), invalid.Body.String())

		forbidden := performRevisionHandlerRequest((&Routes{}).handleRestoreRevision, http.MethodPost, "/api/pages/page-1/revisions/rev-restore/restore", gin.Params{{Key: "id", Value: "page-1"}, {Key: "revisionId", Value: "rev-restore"}}, nil)
		Expect(forbidden).To(HaveHTTPStatus(http.StatusForbidden), forbidden.Body.String())

		missingBackend := performRevisionHandlerRequest((&Routes{treeService: store}).handleRestoreRevision, http.MethodPost, "/api/pages/page-1/revisions/rev-restore/restore", gin.Params{{Key: "id", Value: "page-1"}, {Key: "revisionId", Value: "rev-restore"}}, user)
		Expect(missingBackend).To(HaveRevisionRouteError(http.StatusInternalServerError, ErrCodeRevisionInternalError), missingBackend.Body.String())

		missingPageStore := newUnitRevisionPageStore(revisionUnitPage(pageID, "Page", newFixtureRoutePath("page")))
		missingPageStore.getErr = tree.ErrPageNotFound
		missingPage := performRevisionHandlerRequest((&Routes{treeService: missingPageStore, restoreWorkspaceRevision: successfulRestore(pageID, revisionID)}).handleRestoreRevision, http.MethodPost, "/api/pages/page-1/revisions/rev-restore/restore", gin.Params{{Key: "id", Value: "page-1"}, {Key: "revisionId", Value: "rev-restore"}}, user)
		Expect(missingPage).To(HaveRevisionRouteError(http.StatusNotFound, ErrCodeRevisionNotFound), missingPage.Body.String())

		failedRestore := performRevisionHandlerRequest((&Routes{
			treeService: store,
			restoreWorkspaceRevision: func(context.Context, *tree.Page, revision.RevisionID, workspacesync.Actor, workspacesync.Source) (*tree.Page, error) {
				return nil, errors.New("restore failed")
			},
		}).handleRestoreRevision, http.MethodPost, "/api/pages/page-1/revisions/rev-restore/restore", gin.Params{{Key: "id", Value: "page-1"}, {Key: "revisionId", Value: revisionID.CommitID()}}, user)
		Expect(failedRestore).To(HaveRevisionRouteError(http.StatusNotFound, ErrCodeRevisionNotFound), failedRestore.Body.String())
	})

	ginkgo.It("reports unavailable workspace page storage", func() {
		page, err := (&Routes{}).workspacePage(newFixturePageID("page-1"))

		Expect(page).To(BeNil())
		Expect(err).To(MatchError(errRevisionTreeServiceUnavailable))
	})
})

var _ = ginkgo.Describe("revision validation and error helpers", ginkgo.Label("unit"), func() {
	ginkgo.It("reports invalid revision lookup and asset names", func() {
		_, _, err := ValidateRevisionLookup(newFixturePageID("page-1"), "")
		Expect(err).To(MatchRevisionErrorCode(ErrCodeRevisionInvalidRevisionID))

		_, _, _, err = ValidateRevisionAsset(newFixturePageID("page-1"), "rev-1", "/")
		Expect(err).To(MatchRevisionErrorCode(ErrCodeRevisionPreviewAssetInvalidName))
	})

	ginkgo.It("maps localized, missing, and internal revision errors", func() {
		localized := newRevisionErrorRecorder(func(ctx *gin.Context) {
			respondWithRevisionError(ctx, sharederrors.NewLocalizedErrorFromCode(ErrCodeRevisionInvalidRevisionID, nil))
		})
		Expect(localized).To(HaveRevisionRouteError(http.StatusBadRequest, ErrCodeRevisionInvalidRevisionID), localized.Body.String())

		missing := newRevisionErrorRecorder(func(ctx *gin.Context) {
			respondWithRevisionError(ctx, os.ErrNotExist)
		})
		Expect(missing).To(HaveRevisionRouteError(http.StatusNotFound, ErrCodeRevisionNotFound), missing.Body.String())

		internal := newRevisionErrorRecorder(func(ctx *gin.Context) {
			respondWithRevisionError(ctx, tree.ErrPageNotFound)
		})
		Expect(internal).To(HaveRevisionRouteError(http.StatusInternalServerError, ErrCodeRevisionInternalError), internal.Body.String())

		status := newRevisionErrorRecorder(func(ctx *gin.Context) {
			respondWithRevisionStatusError(ctx, http.StatusNotFound, ErrCodeRevisionNotFound, "ignored", "ignored")
		})
		Expect(status).To(HaveRevisionRouteError(http.StatusNotFound, ErrCodeRevisionNotFound), status.Body.String())
	})
})

var _ = ginkgo.Describe("revision author response mapping", ginkgo.Label("unit"), func() {
	ginkgo.It("includes resolved author labels when a resolver is configured", func() {
		store, err := coreauth.NewUserStore(newRevisionTempDir())
		Expect(err).To(Succeed())
		ginkgo.DeferCleanup(func() {
			Expect(store.Close()).To(Succeed())
		})
		userService := coreauth.NewUserService(store)
		user, err := userService.CreateUser("alice", "alice@example.test", "password", coreauth.RoleEditor)
		Expect(err).To(Succeed())
		resolver, err := coreauth.NewUserResolver(userService)
		Expect(err).To(Succeed())
		rev := revisionFor(newFixturePageID("page-1"), newFixtureRevisionID("rev-1"))
		rev.AuthorID = user.ID.MetadataValue()

		out := ToRevisionResponse(rev, resolver)

		Expect(out.Author).To(Equal(&coreauth.UserLabel{ID: user.ID, Username: "alice"}))
	})
})

type unitRevisionPageStore struct {
	pages   map[tree.PageID]*tree.Page
	raw     map[tree.PageID]string
	getErr  error
	readErr error
}

func newUnitRevisionPageStore(pages ...*tree.Page) *unitRevisionPageStore {
	store := &unitRevisionPageStore{
		pages: map[tree.PageID]*tree.Page{},
		raw:   map[tree.PageID]string{},
	}
	for _, page := range pages {
		store.pages[page.ID] = page
		store.raw[page.ID] = page.RawContent
	}
	return store
}

func (store *unitRevisionPageStore) GetPage(pageID tree.PageID) (*tree.Page, error) {
	if store.getErr != nil {
		return nil, store.getErr
	}
	page := store.pages[pageID]
	if page == nil {
		return nil, tree.ErrPageNotFound
	}
	return page, nil
}

func (store *unitRevisionPageStore) ReadPageRaw(pageID tree.PageID) (string, error) {
	if store.readErr != nil {
		return "", store.readErr
	}
	return store.raw[pageID], nil
}

type revisionListObservation struct {
	PageID   tree.PageID
	Cursor   string
	PageSize workspacesync.PageRevisionLimit
}

type revisionLookupObservation struct {
	PageID     tree.PageID
	RevisionID revision.RevisionID
}

type revisionRestoreObservation struct {
	PageID     tree.PageID
	RevisionID revision.RevisionID
	Actor      workspacesync.Actor
	Source     workspacesync.Source
}

func successfulRevisionList(pageID tree.PageID) func(context.Context, *tree.Page, string, workspacesync.PageRevisionLimit) (workspacesync.PageRevisionList, error) {
	return successfulRevisionListWith(pageID, newFixtureRevisionID("rev-1"))
}

func successfulRevisionListWith(pageID tree.PageID, revisionIDs ...revision.RevisionID) func(context.Context, *tree.Page, string, workspacesync.PageRevisionLimit) (workspacesync.PageRevisionList, error) {
	return func(context.Context, *tree.Page, string, workspacesync.PageRevisionLimit) (workspacesync.PageRevisionList, error) {
		revisions := make([]*revision.Revision, 0, len(revisionIDs))
		for _, revisionID := range revisionIDs {
			revisions = append(revisions, revisionFor(pageID, revisionID))
		}
		return workspacesync.PageRevisionList{Revisions: revisions}, nil
	}
}

func successfulRevisionSnapshot(pageID tree.PageID, revisionID revision.RevisionID) func(context.Context, *tree.Page, revision.RevisionID) (*revision.RevisionSnapshot, error) {
	return func(context.Context, *tree.Page, revision.RevisionID) (*revision.RevisionSnapshot, error) {
		return revisionSnapshotFor(pageID, revisionID, "snapshot body"), nil
	}
}

func successfulRestore(pageID tree.PageID, revisionID revision.RevisionID) func(context.Context, *tree.Page, revision.RevisionID, workspacesync.Actor, workspacesync.Source) (*tree.Page, error) {
	return func(context.Context, *tree.Page, revision.RevisionID, workspacesync.Actor, workspacesync.Source) (*tree.Page, error) {
		return revisionUnitPage(pageID, revisionID.CommitID(), newFixtureRoutePath("restored")), nil
	}
}

func revisionUnitPage(pageID tree.PageID, title string, routePath tree.RoutePath) *tree.Page {
	ginkgo.GinkgoHelper()
	node := revisionUnitNode(pageID, title, routePath)
	return &tree.Page{PageNode: node, Content: "body", RawContent: "# " + title}
}

func revisionUnitNode(pageID tree.PageID, title string, routePath tree.RoutePath) *tree.PageNode {
	ginkgo.GinkgoHelper()
	return &tree.PageNode{ID: pageID, Title: title, Slug: routePath.LeafSlug(), Kind: tree.NodeKindPage}
}

type revisionListResponseBody struct {
	Revisions  []*RevisionResponse `json:"revisions"`
	NextCursor string              `json:"nextCursor"`
}

func decodeRevisionListResponse(body []byte) revisionListResponseBody {
	ginkgo.GinkgoHelper()
	var out revisionListResponseBody
	Expect(json.Unmarshal(body, &out)).To(Succeed(), string(body))
	return out
}

func decodeRevisionResponse(body []byte) RevisionResponse {
	ginkgo.GinkgoHelper()
	var out RevisionResponse
	Expect(json.Unmarshal(body, &out)).To(Succeed(), string(body))
	return out
}

func decodeRevisionSnapshotResponse(body []byte) RevisionSnapshotResponse {
	ginkgo.GinkgoHelper()
	var out RevisionSnapshotResponse
	Expect(json.Unmarshal(body, &out)).To(Succeed(), string(body))
	return out
}

func decodeRevisionComparisonResponse(body []byte) RevisionComparisonResponse {
	ginkgo.GinkgoHelper()
	var out RevisionComparisonResponse
	Expect(json.Unmarshal(body, &out)).To(Succeed(), string(body))
	return out
}

func decodeRevisionRestoredPage(body []byte) dto.Page {
	ginkgo.GinkgoHelper()
	var out dto.Page
	Expect(json.Unmarshal(body, &out)).To(Succeed(), string(body))
	return out
}

func matchRevisionListResponse(revisionID revision.RevisionID, nextCursor string) types.GomegaMatcher {
	ginkgo.GinkgoHelper()
	return gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
		"Revisions":  HaveExactElements(gstruct.PointTo(gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{"ID": Equal(revisionID.CommitID())}))),
		"NextCursor": Equal(nextCursor),
	})
}

func matchRevisionSnapshot(revisionID revision.RevisionID, content string) types.GomegaMatcher {
	ginkgo.GinkgoHelper()
	return gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
		"Revision": gstruct.PointTo(gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{"ID": Equal(revisionID.CommitID())})),
		"Content":  Equal(content),
	})
}

func matchRevisionRouteBackends() types.GomegaMatcher {
	ginkgo.GinkgoHelper()
	return SatisfyAll(
		WithTransform(func(routes *Routes) any { return routes.listWorkspaceRevisions }, Not(BeNil())),
		WithTransform(func(routes *Routes) any { return routes.getWorkspaceRevision }, Not(BeNil())),
		WithTransform(func(routes *Routes) any { return routes.restoreWorkspaceRevision }, Not(BeNil())),
	)
}

func matchRevisionPageStore() types.GomegaMatcher {
	ginkgo.GinkgoHelper()
	return WithTransform(func(routes *Routes) any { return routes.treeService }, Not(BeNil()))
}

func matchRevisionID(revisionID revision.RevisionID) types.GomegaMatcher {
	ginkgo.GinkgoHelper()
	return gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{"ID": Equal(revisionID.CommitID())})
}

func matchRevisionRestore(pageID tree.PageID, revisionID revision.RevisionID, actorID workspacesync.ActorID, source workspacesync.Source) types.GomegaMatcher {
	ginkgo.GinkgoHelper()
	return gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
		"PageID":     Equal(pageID),
		"RevisionID": Equal(revisionID),
		"Actor": gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
			"ID": Equal(actorID),
		}),
		"Source": Equal(source),
	})
}

func matchRestoredPageMetadata(tags []string, properties map[string]string) types.GomegaMatcher {
	ginkgo.GinkgoHelper()
	return gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
		"Tags":       Equal(tags),
		"Properties": Equal(properties),
	})
}

func revisionRegisteredRoutes(engine *gin.Engine) []string {
	routes := engine.Routes()
	out := make([]string, 0, len(routes))
	for _, route := range routes {
		out = append(out, route.Method+" "+route.Path)
	}
	return out
}

func exposeRevisionRouteContract() types.GomegaMatcher {
	ginkgo.GinkgoHelper()
	return ConsistOf(
		"GET /api/pages/:id/revisions",
		"GET /api/pages/:id/revisions/latest",
		"GET /api/pages/:id/revisions/compare",
		"GET /api/pages/:id/revisions/:revisionId/assets/*name",
		"GET /api/pages/:id/revisions/:revisionId",
		"POST /api/pages/:id/revisions/:revisionId/restore",
	)
}

func newRevisionErrorRecorder(write func(*gin.Context)) *httptest.ResponseRecorder {
	ginkgo.GinkgoHelper()
	rec := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(rec)
	write(ctx)
	return rec
}
