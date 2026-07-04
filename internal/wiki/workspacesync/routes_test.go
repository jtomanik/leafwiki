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
	"github.com/onsi/gomega/gstruct"
	"github.com/onsi/gomega/types"
	sharederrors "github.com/perber/wiki/internal/core/shared/errors"
	httpinternal "github.com/perber/wiki/internal/http"
	testmatchers "github.com/perber/wiki/internal/test_utils/matchers"
	"github.com/perber/wiki/internal/workspacesync"
)

var _ = ginkgo.Describe("workspace sync routes", func() {
	ginkgo.It("returns a localized structured error when snapshot listing is disabled", func() {
		router := newWorkspaceSyncTestRouter(RoutesConfig{
			Status: func() workspacesync.SyncStatus {
				return workspacesync.SyncStatus{Enabled: false}
			},
		})

		rec := performWorkspaceSyncRequest(router, http.MethodGet, "/api/workspace-sync/snapshots")

		Expect(rec).To(matchWorkspaceSyncStructuredError(http.StatusNotFound, errCodeWorkspaceSyncDisabled), rec.Body.String())
	})

	ginkgo.It("sanitizes raw snapshot listing failures in structured error responses", func() {
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

		Expect(rec).To(matchWorkspaceSyncStructuredError(http.StatusInternalServerError, errCodeWorkspaceSyncFailed), rec.Body.String())
		var body map[string]json.RawMessage
		Expect(json.Unmarshal(rec.Body.Bytes(), &body)).To(Succeed())
		Expect(body).To(SatisfyAll(
			HaveLen(1),
			HaveKey("error"),
		))
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
		Expect(response).To(HaveKeyWithValue("lastErrorDetail",
			testmatchers.HaveStructuredError(errCodeWorkspaceSyncFailed, sharederrors.MessageIDForCode(errCodeWorkspaceSyncFailed)),
		))
	})

	ginkgo.It("omits last-error detail for blank status errors", func() {
		Expect(workspaceSyncLastErrorDetail("")).To(BeNil())
		Expect(workspaceSyncLastErrorDetail(" \t\n")).To(BeNil())
	})

	ginkgo.It("returns localized failed detail for non-empty status errors", func() {
		detail := workspaceSyncLastErrorDetail("git failed")

		Expect(detail).NotTo(BeNil())
		Expect(detail).To(testmatchers.HaveStructuredError(errCodeWorkspaceSyncFailed, sharederrors.MessageIDForCode(errCodeWorkspaceSyncFailed)))
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

			Expect(rec).To(matchWorkspaceSyncStructuredError(http.StatusBadRequest, errCodeWorkspaceSyncInvalidCursor), rec.Body.String())
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

			Expect(rec).To(matchWorkspaceSyncStructuredError(http.StatusBadRequest, errCodeWorkspaceSyncInvalidLimit), rec.Body.String())
		},
		ginkgo.Entry("non-number", "many"),
		ginkgo.Entry("zero", "0"),
		ginkgo.Entry("over maximum", "201"),
	)

	ginkgo.It("passes snapshot cursor and limit to the list callback and returns next cursor", func() {
		var gotCursor workspacesync.CommitHash
		var gotLimit workspacesync.SnapshotLimit
		expectedSnapshot := workspacesync.Snapshot{
			ID:      workspacesync.CommitHashFromString("snapshot-1"),
			Message: "first snapshot",
		}
		router := newWorkspaceSyncTestRouter(RoutesConfig{
			Status: func() workspacesync.SyncStatus {
				return workspacesync.SyncStatus{Enabled: true}
			},
			ListSnapshots: func(_ context.Context, cursor workspacesync.CommitHash, limit workspacesync.SnapshotLimit) (workspacesync.SnapshotList, error) {
				gotCursor = cursor
				gotLimit = limit
				return workspacesync.SnapshotList{
					Snapshots:  []workspacesync.Snapshot{expectedSnapshot},
					NextCursor: workspacesync.CommitHashFromString("next-snapshot"),
				}, nil
			},
		})

		rec := performWorkspaceSyncRequest(router, http.MethodGet, "/api/workspace-sync/snapshots?cursor=after-commit&limit=12")

		Expect(rec).To(HaveHTTPStatus(http.StatusOK), rec.Body.String())
		Expect(gotCursor).To(Equal(workspacesync.CommitHashFromString("after-commit")))
		Expect(gotLimit).To(Equal(workspacesync.SnapshotLimit(12)))
		var body struct {
			Snapshots []struct {
				ID      workspacesync.CommitHash `json:"id"`
				Message string                   `json:"message"`
			} `json:"snapshots"`
			NextCursor workspacesync.CommitHash `json:"nextCursor"`
		}
		Expect(json.Unmarshal(rec.Body.Bytes(), &body)).To(Succeed())
		Expect(body).To(haveWorkspaceSyncSnapshotPageResponse(expectedSnapshot, workspacesync.CommitHashFromString("next-snapshot")))
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

		Expect(rec).To(HaveHTTPStatus(http.StatusOK), rec.Body.String())
		Expect(gotLimit).To(Equal(workspacesync.SnapshotLimit(50)))
	})

	ginkgo.It("serves status through the protected route when public access is disabled", func() {
		router := newWorkspaceSyncTestRouter(RoutesConfig{
			Status: func() workspacesync.SyncStatus {
				return workspacesync.SyncStatus{Enabled: true, PendingEventCount: 3}
			},
		})

		rec := performWorkspaceSyncRequest(router, http.MethodGet, "/api/workspace-sync/status")

		Expect(rec).To(HaveHTTPStatus(http.StatusOK), rec.Body.String())
		var body workspaceSyncStatusResponse
		Expect(json.Unmarshal(rec.Body.Bytes(), &body)).To(Succeed())
		Expect(body).To(haveWorkspaceSyncEnabledStatus(3))
	})

	ginkgo.It("serves status from the public route when public access is enabled", func() {
		router := newWorkspaceSyncTestRouterWithOptions(RoutesConfig{
			Status: func() workspacesync.SyncStatus {
				return workspacesync.SyncStatus{Enabled: true}
			},
		}, httpinternal.RouterOptions{PublicAccess: true})

		rec := performWorkspaceSyncRequest(router, http.MethodGet, "/api/workspace-sync/status")

		Expect(rec).To(HaveHTTPStatus(http.StatusOK), rec.Body.String())
	})

	ginkgo.It("returns disabled when refresh is not configured", func() {
		router := newWorkspaceSyncTestRouter(RoutesConfig{
			Status: func() workspacesync.SyncStatus {
				return workspacesync.SyncStatus{Enabled: false}
			},
		})

		rec := performWorkspaceSyncCSRFRequest(router, http.MethodPost, "/api/workspace-sync/refresh")

		Expect(rec).To(matchWorkspaceSyncStructuredError(http.StatusNotFound, errCodeWorkspaceSyncDisabled), rec.Body.String())
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

		Expect(rec).To(HaveHTTPStatus(http.StatusOK), rec.Body.String())
		Expect(gotRequest).To(haveWorkspaceSyncRequest(workspacesync.ReasonExplicit, workspacesync.SourceFilesystem, workspacesync.PublicEditorActor()))
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

		Expect(rec).To(matchWorkspaceSyncStructuredError(http.StatusInternalServerError, errCodeWorkspaceSyncFailed), rec.Body.String())
	})

	ginkgo.It("returns disabled when restore is not configured", func() {
		router := newWorkspaceSyncTestRouter(RoutesConfig{
			Status: func() workspacesync.SyncStatus {
				return workspacesync.SyncStatus{Enabled: false}
			},
		})

		rec := performWorkspaceSyncCSRFRequest(router, http.MethodPost, "/api/workspace-sync/snapshots/abc123/restore")

		Expect(rec).To(matchWorkspaceSyncStructuredError(http.StatusNotFound, errCodeWorkspaceSyncDisabled), rec.Body.String())
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

		Expect(rec).To(HaveHTTPStatus(http.StatusOK), rec.Body.String())
		Expect(gotCommit).To(Equal(workspacesync.CommitHashFromString("abc123")))
		Expect(gotActor).To(haveWorkspaceSyncRoutePublicEditorActor())
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

		Expect(rec).To(matchWorkspaceSyncStructuredError(http.StatusInternalServerError, errCodeWorkspaceSyncFailed), rec.Body.String())
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
		restore := &restoreWorkspaceRecorder{}
		routes := NewRoutes(RoutesConfig{
			RestoreWorkspace: restore.RestoreWorkspace,
		})
		router := gin.New()
		router.POST("/restore/:commit", routes.handleRestoreWorkspace)

		rec := performWorkspaceSyncRequest(router, http.MethodPost, "/restore/abc123")

		Expect(rec).To(HaveHTTPStatus(http.StatusForbidden), rec.Body.String())
		Expect(restore.requests).To(BeEmpty())
	})
})

type restoreWorkspaceRequest struct {
	Commit workspacesync.CommitHash
	Actor  workspacesync.Actor
	Source workspacesync.Source
}

type restoreWorkspaceRecorder struct {
	requests []restoreWorkspaceRequest
}

type workspaceSyncRouteState uint8

const (
	workspaceSyncRouteDisabled workspaceSyncRouteState = iota
	workspaceSyncRouteEnabled
)

type workspaceSyncStatusResponse struct {
	State             workspaceSyncRouteState `json:"enabled"`
	PendingEventCount int                     `json:"pendingEventCount"`
}

func (state *workspaceSyncRouteState) UnmarshalJSON(raw []byte) error {
	var enabled bool
	if err := json.Unmarshal(raw, &enabled); err != nil {
		return err
	}
	if enabled {
		*state = workspaceSyncRouteEnabled
		return nil
	}
	*state = workspaceSyncRouteDisabled
	return nil
}

func (r *restoreWorkspaceRecorder) RestoreWorkspace(_ context.Context, commit workspacesync.CommitHash, actor workspacesync.Actor, source workspacesync.Source) (workspacesync.SyncStatus, error) {
	r.requests = append(r.requests, restoreWorkspaceRequest{
		Commit: commit,
		Actor:  actor,
		Source: source,
	})
	return workspacesync.SyncStatus{}, nil
}

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

func matchWorkspaceSyncStructuredError(status int, code sharederrors.ErrorCode) types.GomegaMatcher {
	ginkgo.GinkgoHelper()
	return testmatchers.HaveHTTPStructuredError(status, code, sharederrors.MessageIDForCode(code))
}

func haveWorkspaceSyncSnapshotPageResponse(snapshot workspacesync.Snapshot, nextCursor workspacesync.CommitHash) types.GomegaMatcher {
	ginkgo.GinkgoHelper()
	return gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
		"Snapshots": HaveExactElements(gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
			"ID":      Equal(snapshot.ID),
			"Message": Equal(snapshot.Message),
		})),
		"NextCursor": Equal(nextCursor),
	})
}

func haveWorkspaceSyncEnabledStatus(pendingEventCount int) types.GomegaMatcher {
	ginkgo.GinkgoHelper()
	return gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
		"State":             Equal(workspaceSyncRouteEnabled),
		"PendingEventCount": Equal(pendingEventCount),
	})
}

func haveWorkspaceSyncRequest(reason workspacesync.Reason, source workspacesync.Source, actor workspacesync.Actor) types.GomegaMatcher {
	ginkgo.GinkgoHelper()
	return gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
		"Reason": Equal(reason),
		"Source": Equal(source),
		"Actor":  Equal(actor),
	})
}

func haveWorkspaceSyncRoutePublicEditorActor() types.GomegaMatcher {
	ginkgo.GinkgoHelper()
	return gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
		"ID":   Equal(workspacesync.PublicEditorActor().ID),
		"Name": Equal("public-editor"),
	})
}
