package workspacesyncapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"

	"github.com/gin-gonic/gin"
	coreauth "github.com/perber/wiki/internal/core/auth"
	sharederrors "github.com/perber/wiki/internal/core/shared/errors"
	httpinternal "github.com/perber/wiki/internal/http"
	"github.com/perber/wiki/internal/workspacesync"

	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/gstruct"
	"github.com/onsi/gomega/types"
)

var _ = ginkgo.Describe("workspace sync route handlers", ginkgo.Label("unit"), func() {
	ginkgo.DescribeTable("registers public and protected route topology",
		func(publicAccess bool, expectedStatusRoute string) {
			gin.SetMode(gin.TestMode)
			engine := gin.New()
			routes := NewRoutes(RoutesConfig{
				Status: func() workspacesync.SyncStatus {
					return workspacesync.SyncStatus{Enabled: true}
				},
			})

			routes.RegisterRoutes(httpinternal.RouterContext{
				Base: engine.Group(""),
				Opts: httpinternal.RouterOptions{PublicAccess: publicAccess, AuthDisabled: true},
			})

			Expect(workspaceSyncRegisteredRoutes(engine)).To(ContainElements(
				expectedStatusRoute,
				"POST /api/workspace-sync/refresh",
				"GET /api/workspace-sync/snapshots",
				"POST /api/workspace-sync/snapshots/:commit/restore",
			))
		},
		ginkgo.Entry("with protected status", false, "GET /api/workspace-sync/status"),
		ginkgo.Entry("with public status", true, "GET /api/workspace-sync/status"),
	)

	ginkgo.It("publishes status from the status callback", func() {
		ctx, rec := newWorkspaceSyncUnitContext(http.MethodGet, "/status")
		routes := NewRoutes(RoutesConfig{
			Status: func() workspacesync.SyncStatus {
				return workspacesync.SyncStatus{Enabled: true, PendingEventCount: 4}
			},
		})

		routes.handleStatus(ctx)

		Expect(rec).To(HaveHTTPStatus(http.StatusOK), rec.Body.String())
		var body workspaceSyncStatusResponse
		Expect(json.Unmarshal(rec.Body.Bytes(), &body)).To(Succeed())
		Expect(body).To(haveWorkspaceSyncEnabledStatus(4))
	})

	ginkgo.It("lists snapshots with validated cursor and limit", func() {
		var gotCursor workspacesync.CommitHash
		var gotLimit workspacesync.SnapshotLimit
		expected := workspacesync.Snapshot{
			ID:      workspacesync.CommitHashFromString("commit-1"),
			Message: "snapshot created",
		}
		ctx, rec := newWorkspaceSyncUnitContext(http.MethodGet, "/snapshots?cursor=after-1&limit=12")
		routes := NewRoutes(RoutesConfig{
			ListSnapshots: func(_ context.Context, cursor workspacesync.CommitHash, limit workspacesync.SnapshotLimit) (workspacesync.SnapshotList, error) {
				gotCursor = cursor
				gotLimit = limit
				return workspacesync.SnapshotList{
					Snapshots:  []workspacesync.Snapshot{expected},
					NextCursor: workspacesync.CommitHashFromString("after-2"),
				}, nil
			},
		})

		routes.handleSnapshots(ctx)

		Expect(rec).To(HaveHTTPStatus(http.StatusOK), rec.Body.String())
		Expect(gotCursor).To(Equal(workspacesync.CommitHashFromString("after-1")))
		Expect(gotLimit).To(Equal(workspacesync.SnapshotLimit(12)))
		var body struct {
			Snapshots []struct {
				ID      workspacesync.CommitHash `json:"id"`
				Message string                   `json:"message"`
			} `json:"snapshots"`
			NextCursor workspacesync.CommitHash `json:"nextCursor"`
		}
		Expect(json.Unmarshal(rec.Body.Bytes(), &body)).To(Succeed())
		Expect(body).To(haveWorkspaceSyncSnapshotPageResponse(expected, workspacesync.CommitHashFromString("after-2")))
	})

	ginkgo.DescribeTable("rejects invalid snapshot listing requests",
		func(target string, code workspacesyncRouteErrorCode) {
			ctx, rec := newWorkspaceSyncUnitContext(http.MethodGet, target)
			routes := NewRoutes(RoutesConfig{
				ListSnapshots: func(context.Context, workspacesync.CommitHash, workspacesync.SnapshotLimit) (workspacesync.SnapshotList, error) {
					return workspacesync.SnapshotList{}, nil
				},
			})

			routes.handleSnapshots(ctx)

			Expect(rec).To(matchWorkspaceSyncStructuredError(http.StatusBadRequest, code.ErrorCode()), rec.Body.String())
		},
		ginkgo.Entry("for cursors containing whitespace", "/snapshots?cursor=after%201", workspaceSyncRouteInvalidCursor),
		ginkgo.Entry("for non-numeric limits", "/snapshots?limit=many", workspaceSyncRouteInvalidLimit),
		ginkgo.Entry("for limits beyond the maximum page size", "/snapshots?limit=201", workspaceSyncRouteInvalidLimit),
	)

	ginkgo.It("returns structured errors for unavailable or failing snapshot listing", func() {
		ctx, rec := newWorkspaceSyncUnitContext(http.MethodGet, "/snapshots")
		NewRoutes(RoutesConfig{}).handleSnapshots(ctx)
		Expect(rec).To(matchWorkspaceSyncStructuredError(http.StatusNotFound, errCodeWorkspaceSyncDisabled), rec.Body.String())

		ctx, rec = newWorkspaceSyncUnitContext(http.MethodGet, "/snapshots")
		NewRoutes(RoutesConfig{
			ListSnapshots: func(context.Context, workspacesync.CommitHash, workspacesync.SnapshotLimit) (workspacesync.SnapshotList, error) {
				return workspacesync.SnapshotList{}, errors.New("snapshot listing failed")
			},
		}).handleSnapshots(ctx)
		Expect(rec).To(matchWorkspaceSyncStructuredError(http.StatusInternalServerError, errCodeWorkspaceSyncFailed), rec.Body.String())
	})

	ginkgo.It("refreshes workspace sync with an explicit filesystem request", func() {
		var gotRequest workspacesync.SyncRequest
		ctx, rec := newWorkspaceSyncUnitContext(http.MethodPost, "/refresh")
		routes := NewRoutes(RoutesConfig{
			Refresh: func(_ context.Context, req workspacesync.SyncRequest) (workspacesync.SyncStatus, error) {
				gotRequest = req
				return workspacesync.SyncStatus{Enabled: true, PendingEventCount: 2}, nil
			},
		})

		routes.handleRefresh(ctx)

		Expect(rec).To(HaveHTTPStatus(http.StatusOK), rec.Body.String())
		Expect(gotRequest).To(haveWorkspaceSyncRequest(workspacesync.ReasonExplicit, workspacesync.SourceFilesystem, workspacesync.PublicEditorActor()))
	})

	ginkgo.It("returns structured errors for unavailable or failing refresh", func() {
		ctx, rec := newWorkspaceSyncUnitContext(http.MethodPost, "/refresh")
		NewRoutes(RoutesConfig{}).handleRefresh(ctx)
		Expect(rec).To(matchWorkspaceSyncStructuredError(http.StatusNotFound, errCodeWorkspaceSyncDisabled), rec.Body.String())

		ctx, rec = newWorkspaceSyncUnitContext(http.MethodPost, "/refresh")
		NewRoutes(RoutesConfig{
			Refresh: func(context.Context, workspacesync.SyncRequest) (workspacesync.SyncStatus, error) {
				return workspacesync.SyncStatus{}, errors.New("refresh failed")
			},
		}).handleRefresh(ctx)
		Expect(rec).To(matchWorkspaceSyncStructuredError(http.StatusInternalServerError, errCodeWorkspaceSyncFailed), rec.Body.String())
	})

	ginkgo.It("restores a snapshot as the authenticated web actor", func() {
		var gotCommit workspacesync.CommitHash
		var gotActor workspacesync.Actor
		var gotSource workspacesync.Source
		ctx, rec := newWorkspaceSyncUnitContext(http.MethodPost, "/snapshots/commit-1/restore")
		ctx.Params = gin.Params{{Key: "commit", Value: "commit-1"}}
		ctx.Set("user", &coreauth.User{
			ID:       newFixtureWorkspaceSyncRouteUserID("editor-1"),
			Username: "Editor One",
			Email:    "editor@example.test",
		})
		routes := NewRoutes(RoutesConfig{
			RestoreWorkspace: func(_ context.Context, commit workspacesync.CommitHash, actor workspacesync.Actor, source workspacesync.Source) (workspacesync.SyncStatus, error) {
				gotCommit = commit
				gotActor = actor
				gotSource = source
				return workspacesync.SyncStatus{Enabled: true, LastCommitHash: commit}, nil
			},
		})

		routes.handleRestoreWorkspace(ctx)

		Expect(rec).To(HaveHTTPStatus(http.StatusOK), rec.Body.String())
		Expect(gotCommit).To(Equal(workspacesync.CommitHashFromString("commit-1")))
		Expect(gotActor).To(matchWorkspaceSyncRouteActor(
			workspacesync.ActorIDFromUserID(newFixtureWorkspaceSyncRouteUserID("editor-1")),
			"Editor One",
			"editor@example.test",
		))
		Expect(gotSource).To(Equal(workspacesync.SourceWeb))
	})

	ginkgo.It("does not restore without an authenticated user", func() {
		restore := &restoreWorkspaceRecorder{}
		ctx, rec := newWorkspaceSyncUnitContext(http.MethodPost, "/snapshots/commit-1/restore")
		ctx.Params = gin.Params{{Key: "commit", Value: "commit-1"}}
		NewRoutes(RoutesConfig{RestoreWorkspace: restore.RestoreWorkspace}).handleRestoreWorkspace(ctx)

		Expect(rec).To(HaveHTTPStatus(http.StatusForbidden), rec.Body.String())
		Expect(restore.requests).To(BeEmpty())
	})

	ginkgo.It("returns structured errors for unavailable or failing restore", func() {
		ctx, rec := newWorkspaceSyncUnitContext(http.MethodPost, "/snapshots/commit-1/restore")
		ctx.Params = gin.Params{{Key: "commit", Value: "commit-1"}}
		NewRoutes(RoutesConfig{}).handleRestoreWorkspace(ctx)
		Expect(rec).To(matchWorkspaceSyncStructuredError(http.StatusNotFound, errCodeWorkspaceSyncDisabled), rec.Body.String())

		ctx, rec = newWorkspaceSyncUnitContext(http.MethodPost, "/snapshots/commit-1/restore")
		ctx.Params = gin.Params{{Key: "commit", Value: "commit-1"}}
		ctx.Set("user", &coreauth.User{ID: newFixtureWorkspaceSyncRouteUserID("editor-1")})
		NewRoutes(RoutesConfig{
			RestoreWorkspace: func(context.Context, workspacesync.CommitHash, workspacesync.Actor, workspacesync.Source) (workspacesync.SyncStatus, error) {
				return workspacesync.SyncStatus{}, errors.New("restore failed")
			},
		}).handleRestoreWorkspace(ctx)
		Expect(rec).To(matchWorkspaceSyncStructuredError(http.StatusInternalServerError, errCodeWorkspaceSyncFailed), rec.Body.String())
	})
})

type workspacesyncRouteErrorCode uint8

const (
	workspaceSyncRouteInvalidCursor workspacesyncRouteErrorCode = iota
	workspaceSyncRouteInvalidLimit
)

func (code workspacesyncRouteErrorCode) ErrorCode() sharederrors.ErrorCode {
	switch code {
	case workspaceSyncRouteInvalidCursor:
		return errCodeWorkspaceSyncInvalidCursor
	case workspaceSyncRouteInvalidLimit:
		return errCodeWorkspaceSyncInvalidLimit
	default:
		return errCodeWorkspaceSyncFailed
	}
}

func newWorkspaceSyncUnitContext(method string, target string) (*gin.Context, *httptest.ResponseRecorder) {
	ginkgo.GinkgoHelper()

	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(rec)
	ctx.Request = httptest.NewRequest(method, target, nil)
	return ctx, rec
}

func workspaceSyncRegisteredRoutes(engine *gin.Engine) []string {
	routes := engine.Routes()
	out := make([]string, 0, len(routes))
	for _, route := range routes {
		out = append(out, route.Method+" "+route.Path)
	}
	return out
}

func newFixtureWorkspaceSyncRouteUserID[T ~string](raw T) coreauth.UserID {
	return coreauth.NewUserIDUnchecked(string(raw))
}

func matchWorkspaceSyncRouteActor(id workspacesync.ActorID, name string, email string) types.GomegaMatcher {
	return gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
		"ID":    Equal(id),
		"Name":  Equal(name),
		"Email": Equal(email),
	})
}
