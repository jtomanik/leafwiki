package workspacesyncapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	sharederrors "github.com/perber/wiki/internal/core/shared/errors"
	httpinternal "github.com/perber/wiki/internal/http"
	"github.com/perber/wiki/internal/workspacesync"
)

var _ = ginkgo.Describe("workspace sync routes", func() {
	ginkgo.It("TestSnapshotRouteDisabledUsesLocalizedStructuredError", func() {
		router := newWorkspaceSyncTestRouter(RoutesConfig{
			Status: func() workspacesync.SyncStatus {
				return workspacesync.SyncStatus{Enabled: false}
			},
		})

		rec := performWorkspaceSyncRequest(router, http.MethodGet, "/api/workspace-sync/snapshots")

		Expect(rec.Code).To(Equal(http.StatusNotFound), rec.Body.String())
		assertWorkspaceSyncStructuredError(rec, "workspace_sync_disabled", "errors.workspace.sync_disabled", "workspace sync is not enabled")
	})

	ginkgo.It("TestSnapshotRouteFailureDoesNotRenderRawErrorAsMessage", func() {
		rawErr := errors.New("git exploded with private path /tmp/secret")
		router := newWorkspaceSyncTestRouter(RoutesConfig{
			Status: func() workspacesync.SyncStatus {
				return workspacesync.SyncStatus{Enabled: true}
			},
			ListSnapshots: func(context.Context, workspacesync.CommitHash, workspacesync.SnapshotLimit) (workspacesync.SnapshotList, error) {
				return workspacesync.SnapshotList{}, rawErr
			},
		})

		rec := performWorkspaceSyncRequest(router, http.MethodGet, "/api/workspace-sync/snapshots")

		Expect(rec.Code).To(Equal(http.StatusInternalServerError), rec.Body.String())
		assertWorkspaceSyncStructuredError(rec, "workspace_sync_failed", "errors.workspace.sync_failed", "Workspace sync failed")
		Expect(strings.Contains(rec.Body.String(), rawErr.Error())).To(BeFalse())
	})

	ginkgo.It("maps SyncStatus fields into the public status response", func() {
		lastSync := time.Date(2026, 6, 25, 10, 30, 0, 0, time.UTC)
		status := workspacesync.SyncStatus{
			Enabled:                    true,
			WatcherEnabled:             true,
			WatcherRunning:             true,
			PendingEventCount:          7,
			LastSyncTime:               lastSync,
			LastError:                  "sync failed",
			LastCommitHash:             workspacesync.CommitHashFromString("abc123"),
			RecentChangedMarkdownPaths: []string{"docs/page.md"},
			ValidationErrors: []workspacesync.ValidationError{{
				Path:     "docs/page.md",
				Message:  "broken link",
				Severity: "error",
			}},
		}

		response := statusResponse(status)

		Expect(response).To(HaveKeyWithValue("enabled", true))
		Expect(response).To(HaveKeyWithValue("watcherEnabled", true))
		Expect(response).To(HaveKeyWithValue("watcherRunning", true))
		Expect(response).To(HaveKeyWithValue("pendingEventCount", 7))
		Expect(response).To(HaveKeyWithValue("lastSyncTime", lastSync))
		Expect(response).To(HaveKeyWithValue("lastCommitHash", workspacesync.CommitHashFromString("abc123")))
		Expect(response).To(HaveKeyWithValue("recentChangedMarkdownPaths", []string{"docs/page.md"}))
		Expect(response).To(HaveKeyWithValue("validationErrorDetails", status.ValidationErrors))
		detail, ok := response["lastErrorDetail"].(*sharederrors.LocalizedErrorDetail)
		Expect(ok).To(BeTrue())
		Expect(detail.Code.String()).To(Equal("workspace_sync_failed"))
		Expect(detail.MessageID.String()).To(Equal("errors.workspace.sync_failed"))
	})

	ginkgo.It("omits last-error detail for blank status errors", func() {
		Expect(workspaceSyncLastErrorDetail("")).To(BeNil())
		Expect(workspaceSyncLastErrorDetail(" \t\n")).To(BeNil())
	})

	ginkgo.It("returns localized failed detail for non-empty status errors", func() {
		detail := workspaceSyncLastErrorDetail("git failed")

		Expect(detail).NotTo(BeNil())
		Expect(detail.Code.String()).To(Equal("workspace_sync_failed"))
		Expect(detail.MessageID.String()).To(Equal("errors.workspace.sync_failed"))
		Expect(detail.Message).To(Equal("Workspace sync failed"))
	})

	ginkgo.DescribeTable("rejects invalid snapshot cursors",
		func(query string) {
			router := newWorkspaceSyncTestRouter(RoutesConfig{
				Status: func() workspacesync.SyncStatus {
					return workspacesync.SyncStatus{Enabled: true}
				},
				ListSnapshots: func(context.Context, workspacesync.CommitHash, workspacesync.SnapshotLimit) (workspacesync.SnapshotList, error) {
					return workspacesync.SnapshotList{}, nil
				},
			})

			rec := performWorkspaceSyncRequest(router, http.MethodGet, "/api/workspace-sync/snapshots?cursor="+query)

			Expect(rec.Code).To(Equal(http.StatusBadRequest), rec.Body.String())
			assertWorkspaceSyncStructuredError(rec, "workspace_sync_invalid_cursor", "errors.workspace.sync_invalid_cursor", "invalid snapshot cursor")
		},
		ginkgo.Entry("cursor with whitespace", "abc%20123"),
		ginkgo.Entry("cursor longer than 256 characters", strings.Repeat("a", 257)),
	)

	ginkgo.DescribeTable("rejects invalid snapshot limits",
		func(limit string) {
			router := newWorkspaceSyncTestRouter(RoutesConfig{
				Status: func() workspacesync.SyncStatus {
					return workspacesync.SyncStatus{Enabled: true}
				},
				ListSnapshots: func(context.Context, workspacesync.CommitHash, workspacesync.SnapshotLimit) (workspacesync.SnapshotList, error) {
					return workspacesync.SnapshotList{}, nil
				},
			})

			rec := performWorkspaceSyncRequest(router, http.MethodGet, "/api/workspace-sync/snapshots?limit="+limit)

			Expect(rec.Code).To(Equal(http.StatusBadRequest), rec.Body.String())
			assertWorkspaceSyncStructuredError(rec, "workspace_sync_invalid_limit", "errors.workspace.sync_invalid_limit", "invalid snapshot limit")
		},
		ginkgo.Entry("non-number", "many"),
		ginkgo.Entry("zero", "0"),
		ginkgo.Entry("over maximum", "201"),
	)

	ginkgo.It("passes snapshot cursor and limit to the list callback and returns next cursor", func() {
		var gotCursor workspacesync.CommitHash
		var gotLimit workspacesync.SnapshotLimit
		router := newWorkspaceSyncTestRouter(RoutesConfig{
			Status: func() workspacesync.SyncStatus {
				return workspacesync.SyncStatus{Enabled: true}
			},
			ListSnapshots: func(_ context.Context, cursor workspacesync.CommitHash, limit workspacesync.SnapshotLimit) (workspacesync.SnapshotList, error) {
				gotCursor = cursor
				gotLimit = limit
				return workspacesync.SnapshotList{
					Snapshots: []workspacesync.Snapshot{{
						ID:      workspacesync.CommitHashFromString("snapshot-1"),
						Message: "first snapshot",
					}},
					NextCursor: workspacesync.CommitHashFromString("next-snapshot"),
				}, nil
			},
		})

		rec := performWorkspaceSyncRequest(router, http.MethodGet, "/api/workspace-sync/snapshots?cursor=after-commit&limit=12")

		Expect(rec.Code).To(Equal(http.StatusOK), rec.Body.String())
		Expect(gotCursor).To(Equal(workspacesync.CommitHashFromString("after-commit")))
		Expect(gotLimit).To(Equal(workspacesync.SnapshotLimit(12)))
		var body struct {
			Snapshots []struct {
				ID      string `json:"id"`
				Message string `json:"message"`
			} `json:"snapshots"`
			NextCursor string `json:"nextCursor"`
		}
		Expect(json.Unmarshal(rec.Body.Bytes(), &body)).To(Succeed())
		Expect(body.Snapshots).To(HaveLen(1))
		Expect(body.Snapshots[0].ID).To(Equal("snapshot-1"))
		Expect(body.Snapshots[0].Message).To(Equal("first snapshot"))
		Expect(body.NextCursor).To(Equal("next-snapshot"))
	})

	ginkgo.It("uses the default snapshot page limit when the query omits limit", func() {
		var gotLimit workspacesync.SnapshotLimit
		router := newWorkspaceSyncTestRouter(RoutesConfig{
			Status: func() workspacesync.SyncStatus {
				return workspacesync.SyncStatus{Enabled: true}
			},
			ListSnapshots: func(_ context.Context, _ workspacesync.CommitHash, limit workspacesync.SnapshotLimit) (workspacesync.SnapshotList, error) {
				gotLimit = limit
				return workspacesync.SnapshotList{}, nil
			},
		})

		rec := performWorkspaceSyncRequest(router, http.MethodGet, "/api/workspace-sync/snapshots")

		Expect(rec.Code).To(Equal(http.StatusOK), rec.Body.String())
		Expect(gotLimit).To(Equal(workspacesync.SnapshotLimit(50)))
	})

	ginkgo.It("serves status through the protected route when public access is disabled", func() {
		router := newWorkspaceSyncTestRouter(RoutesConfig{
			Status: func() workspacesync.SyncStatus {
				return workspacesync.SyncStatus{Enabled: true, PendingEventCount: 3}
			},
		})

		rec := performWorkspaceSyncRequest(router, http.MethodGet, "/api/workspace-sync/status")

		Expect(rec.Code).To(Equal(http.StatusOK), rec.Body.String())
		var body struct {
			Enabled           bool `json:"enabled"`
			PendingEventCount int  `json:"pendingEventCount"`
		}
		Expect(json.Unmarshal(rec.Body.Bytes(), &body)).To(Succeed())
		Expect(body.Enabled).To(BeTrue())
		Expect(body.PendingEventCount).To(Equal(3))
	})

	ginkgo.It("serves status from the public route when public access is enabled", func() {
		router := newWorkspaceSyncTestRouterWithOptions(RoutesConfig{
			Status: func() workspacesync.SyncStatus {
				return workspacesync.SyncStatus{Enabled: true}
			},
		}, httpinternal.RouterOptions{PublicAccess: true})

		rec := performWorkspaceSyncRequest(router, http.MethodGet, "/api/workspace-sync/status")

		Expect(rec.Code).To(Equal(http.StatusOK), rec.Body.String())
	})

	ginkgo.It("returns disabled when refresh is not configured", func() {
		router := newWorkspaceSyncTestRouter(RoutesConfig{
			Status: func() workspacesync.SyncStatus {
				return workspacesync.SyncStatus{Enabled: false}
			},
		})

		rec := performWorkspaceSyncCSRFRequest(router, http.MethodPost, "/api/workspace-sync/refresh")

		Expect(rec.Code).To(Equal(http.StatusNotFound), rec.Body.String())
		assertWorkspaceSyncStructuredError(rec, "workspace_sync_disabled", "errors.workspace.sync_disabled", "workspace sync is not enabled")
	})

	ginkgo.It("passes explicit filesystem refresh requests to the refresh callback", func() {
		var gotRequest workspacesync.SyncRequest
		router := newWorkspaceSyncTestRouter(RoutesConfig{
			Status: func() workspacesync.SyncStatus {
				return workspacesync.SyncStatus{Enabled: true}
			},
			Refresh: func(_ context.Context, req workspacesync.SyncRequest) (workspacesync.SyncStatus, error) {
				gotRequest = req
				return workspacesync.SyncStatus{Enabled: true, PendingEventCount: 1}, nil
			},
		})

		rec := performWorkspaceSyncCSRFRequest(router, http.MethodPost, "/api/workspace-sync/refresh")

		Expect(rec.Code).To(Equal(http.StatusOK), rec.Body.String())
		Expect(gotRequest.Reason).To(Equal(workspacesync.ReasonExplicit))
		Expect(gotRequest.Source).To(Equal(workspacesync.SourceFilesystem))
		Expect(gotRequest.Actor).To(Equal(workspacesync.PublicEditorActor()))
	})

	ginkgo.It("returns a structured failure when refresh fails", func() {
		router := newWorkspaceSyncTestRouter(RoutesConfig{
			Status: func() workspacesync.SyncStatus {
				return workspacesync.SyncStatus{Enabled: true}
			},
			Refresh: func(context.Context, workspacesync.SyncRequest) (workspacesync.SyncStatus, error) {
				return workspacesync.SyncStatus{}, errors.New("refresh failed")
			},
		})

		rec := performWorkspaceSyncCSRFRequest(router, http.MethodPost, "/api/workspace-sync/refresh")

		Expect(rec.Code).To(Equal(http.StatusInternalServerError), rec.Body.String())
		assertWorkspaceSyncStructuredError(rec, "workspace_sync_failed", "errors.workspace.sync_failed", "Workspace sync failed")
	})

	ginkgo.It("returns disabled when restore is not configured", func() {
		router := newWorkspaceSyncTestRouter(RoutesConfig{
			Status: func() workspacesync.SyncStatus {
				return workspacesync.SyncStatus{Enabled: false}
			},
		})

		rec := performWorkspaceSyncCSRFRequest(router, http.MethodPost, "/api/workspace-sync/snapshots/abc123/restore")

		Expect(rec.Code).To(Equal(http.StatusNotFound), rec.Body.String())
		assertWorkspaceSyncStructuredError(rec, "workspace_sync_disabled", "errors.workspace.sync_disabled", "workspace sync is not enabled")
	})

	ginkgo.It("passes commit and public-editor actor details to the restore callback", func() {
		var gotCommit workspacesync.CommitHash
		var gotActor workspacesync.Actor
		var gotSource workspacesync.Source
		router := newWorkspaceSyncTestRouter(RoutesConfig{
			Status: func() workspacesync.SyncStatus {
				return workspacesync.SyncStatus{Enabled: true}
			},
			RestoreWorkspace: func(_ context.Context, commit workspacesync.CommitHash, actor workspacesync.Actor, source workspacesync.Source) (workspacesync.SyncStatus, error) {
				gotCommit = commit
				gotActor = actor
				gotSource = source
				return workspacesync.SyncStatus{Enabled: true, LastCommitHash: commit}, nil
			},
		})

		rec := performWorkspaceSyncCSRFRequest(router, http.MethodPost, "/api/workspace-sync/snapshots/abc123/restore")

		Expect(rec.Code).To(Equal(http.StatusOK), rec.Body.String())
		Expect(gotCommit).To(Equal(workspacesync.CommitHashFromString("abc123")))
		Expect(gotActor.ID.String()).To(Equal("public-editor"))
		Expect(gotActor.Name).To(Equal("public-editor"))
		Expect(gotSource).To(Equal(workspacesync.SourceWeb))
	})

	ginkgo.It("returns a structured failure when restore fails", func() {
		router := newWorkspaceSyncTestRouter(RoutesConfig{
			Status: func() workspacesync.SyncStatus {
				return workspacesync.SyncStatus{Enabled: true}
			},
			RestoreWorkspace: func(context.Context, workspacesync.CommitHash, workspacesync.Actor, workspacesync.Source) (workspacesync.SyncStatus, error) {
				return workspacesync.SyncStatus{}, errors.New("restore failed")
			},
		})

		rec := performWorkspaceSyncCSRFRequest(router, http.MethodPost, "/api/workspace-sync/snapshots/abc123/restore")

		Expect(rec.Code).To(Equal(http.StatusInternalServerError), rec.Body.String())
		assertWorkspaceSyncStructuredError(rec, "workspace_sync_failed", "errors.workspace.sync_failed", "Workspace sync failed")
	})

	ginkgo.It("ignores nil route registrations and configs without a status callback", func() {
		Expect(func() {
			var routes *Routes
			routes.RegisterRoutes(httpinternal.RouterContext{})
		}).NotTo(Panic())
		Expect(func() {
			NewRoutes(RoutesConfig{}).RegisterRoutes(httpinternal.RouterContext{})
		}).NotTo(Panic())
	})

	ginkgo.It("does not call restore when middleware did not attach a user", func() {
		gin.SetMode(gin.TestMode)
		called := false
		routes := NewRoutes(RoutesConfig{
			RestoreWorkspace: func(context.Context, workspacesync.CommitHash, workspacesync.Actor, workspacesync.Source) (workspacesync.SyncStatus, error) {
				called = true
				return workspacesync.SyncStatus{}, nil
			},
		})
		router := gin.New()
		router.POST("/restore/:commit", routes.handleRestoreWorkspace)

		rec := performWorkspaceSyncRequest(router, http.MethodPost, "/restore/abc123")

		Expect(rec.Code).To(Equal(http.StatusForbidden), rec.Body.String())
		Expect(called).To(BeFalse())
	})
})

func newWorkspaceSyncTestRouter(cfg RoutesConfig) http.Handler {
	ginkgo.GinkgoHelper()
	return newWorkspaceSyncTestRouterWithOptions(cfg, httpinternal.RouterOptions{})
}

func newWorkspaceSyncTestRouterWithOptions(cfg RoutesConfig, opts httpinternal.RouterOptions) http.Handler {
	ginkgo.GinkgoHelper()
	opts.AllowInsecure = true
	opts.AuthDisabled = true
	opts.DisableFrontendRoutes = true
	return httpinternal.NewRouter(
		[]httpinternal.RouteRegistrar{NewRoutes(cfg)},
		httpinternal.FrontendConfig{},
		opts,
	)
}

func performWorkspaceSyncRequest(router http.Handler, method string, path string) *httptest.ResponseRecorder {
	ginkgo.GinkgoHelper()
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(method, path, nil))
	return rec
}

func performWorkspaceSyncCSRFRequest(router http.Handler, method string, path string) *httptest.ResponseRecorder {
	ginkgo.GinkgoHelper()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(method, path, nil)
	req.Header.Set("X-CSRF-Token", "test-csrf-token")
	req.AddCookie(&http.Cookie{Name: "leafwiki_csrf", Value: "test-csrf-token"})
	router.ServeHTTP(rec, req)
	return rec
}

func assertWorkspaceSyncStructuredError(rec *httptest.ResponseRecorder, code string, messageID string, message string) {
	ginkgo.GinkgoHelper()
	var body struct {
		Error struct {
			Code      string `json:"code"`
			MessageID string `json:"messageId"`
			Message   string `json:"message"`
		} `json:"error"`
	}
	Expect(json.Unmarshal(rec.Body.Bytes(), &body)).To(Succeed(), rec.Body.String())
	Expect(body.Error.Code).To(Equal(code))
	Expect(body.Error.MessageID).To(Equal(messageID))
	Expect(body.Error.Message).To(Equal(message))
}
