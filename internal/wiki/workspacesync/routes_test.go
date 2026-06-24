package workspacesyncapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	httpinternal "github.com/perber/wiki/internal/http"
	"github.com/perber/wiki/internal/workspacesync"
)

func TestSnapshotRouteDisabledUsesLocalizedStructuredError(t *testing.T) {
	router := httpinternal.NewRouter(
		[]httpinternal.RouteRegistrar{NewRoutes(RoutesConfig{
			Status: func() workspacesync.SyncStatus {
				return workspacesync.SyncStatus{Enabled: false}
			},
		})},
		httpinternal.FrontendConfig{},
		httpinternal.RouterOptions{
			AuthDisabled:          true,
			DisableFrontendRoutes: true,
		},
	)

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/workspace-sync/snapshots", nil))

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404: %s", rec.Code, rec.Body.String())
	}
	assertWorkspaceSyncStructuredError(t, rec, "workspace_sync_disabled", "errors.workspace.sync_disabled", "workspace sync is not enabled")
}

func TestSnapshotRouteFailureDoesNotRenderRawErrorAsMessage(t *testing.T) {
	rawErr := errors.New("git exploded with private path /tmp/secret")
	router := httpinternal.NewRouter(
		[]httpinternal.RouteRegistrar{NewRoutes(RoutesConfig{
			Status: func() workspacesync.SyncStatus {
				return workspacesync.SyncStatus{Enabled: true}
			},
			ListSnapshots: func(context.Context, workspacesync.CommitHash, workspacesync.SnapshotLimit) (workspacesync.SnapshotList, error) {
				return workspacesync.SnapshotList{}, rawErr
			},
		})},
		httpinternal.FrontendConfig{},
		httpinternal.RouterOptions{
			AuthDisabled:          true,
			DisableFrontendRoutes: true,
		},
	)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/workspace-sync/snapshots", nil))

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500: %s", rec.Code, rec.Body.String())
	}
	assertWorkspaceSyncStructuredError(t, rec, "workspace_sync_failed", "errors.workspace.sync_failed", "Workspace sync failed")
	if strings.Contains(rec.Body.String(), rawErr.Error()) {
		t.Fatalf("body = %s, want raw detail omitted from structured error response", rec.Body.String())
	}
}

func assertWorkspaceSyncStructuredError(t *testing.T, rec *httptest.ResponseRecorder, code string, messageID string, message string) {
	t.Helper()
	var body struct {
		Error struct {
			Code      string `json:"code"`
			MessageID string `json:"messageId"`
			Message   string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v: %s", err, rec.Body.String())
	}
	if body.Error.Code != code || body.Error.MessageID != messageID || body.Error.Message != message {
		t.Fatalf("error = %#v, want code=%q messageId=%q message=%q", body.Error, code, messageID, message)
	}
}
