package workspaced

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/perber/wiki/internal/core/assets"
	httpinternal "github.com/perber/wiki/internal/http"
	"github.com/perber/wiki/internal/projectdaemon"
	"github.com/perber/wiki/internal/workspaceid"
)

func TestAuthenticatedRouterRequiresPrivateTokenAndActorContext(t *testing.T) {
	now := time.Date(2026, 6, 16, 12, 0, 0, 0, time.UTC)
	w := newTestWiki(t)
	defer w.Close()
	router := NewAuthenticatedRouter(w, httpinternal.RouterOptions{
		PublicAccess:            true,
		AllowInsecure:           true,
		AuthDisabled:            true,
		AccessTokenTimeout:      15 * time.Minute,
		RefreshTokenTimeout:     7 * 24 * time.Hour,
		MaxAssetUploadSizeBytes: assets.DefaultMaxUploadSizeBytes,
	}, PrivateAuthOptions{
		DaemonToken: "private-token",
		WorkspaceID: "current",
		Now:         func() time.Time { return now },
	})

	for _, tc := range []struct {
		name          string
		token         string
		actor         string
		status        int
		wantCode      string
		wantMessageID string
	}{
		{name: "missing token", status: http.StatusUnauthorized, wantCode: "private_control_token_invalid", wantMessageID: "errors.private.control_token_invalid"},
		{name: "wrong token", token: "wrong", status: http.StatusUnauthorized, wantCode: "private_control_token_invalid", wantMessageID: "errors.private.control_token_invalid"},
		{name: "missing actor", token: "private-token", status: http.StatusUnauthorized, wantCode: "private_actor_context_invalid", wantMessageID: "errors.private.actor_context_invalid"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := newPrivateRequest(http.MethodGet, "/api/tree", tc.token, tc.actor)
			rec := requestWithRequest(router, req)
			if rec.Code != tc.status {
				t.Fatalf("status = %d, want %d: %s", rec.Code, tc.status, rec.Body.String())
			}
			assertStructuredPrivateAuthError(t, rec, tc.wantCode, tc.wantMessageID)
		})
	}

	wrongWorkspaceActor, err := projectdaemon.EncodeActorContext(projectdaemon.ActorContext{
		Version:     1,
		Issuer:      projectdaemon.ActorContextIssuerWikid,
		Subject:     "user:admin",
		Username:    "admin",
		Role:        "admin",
		WorkspaceID: "other",
		AuthMethod:  "disabled",
		IssuedAt:    now,
		ExpiresAt:   now.Add(5 * time.Minute),
	})
	if err != nil {
		t.Fatalf("EncodeActorContext failed: %v", err)
	}
	wrongWorkspaceReq := newPrivateRequest(http.MethodGet, "/api/tree", "private-token", wrongWorkspaceActor)
	wrongWorkspaceRec := requestWithRequest(router, wrongWorkspaceReq)
	if wrongWorkspaceRec.Code != http.StatusUnauthorized {
		t.Fatalf("wrong-workspace actor status = %d, want 401: %s", wrongWorkspaceRec.Code, wrongWorkspaceRec.Body.String())
	}
	assertStructuredPrivateAuthError(t, wrongWorkspaceRec, "private_actor_context_invalid", "errors.private.actor_context_invalid")

	actor, err := projectdaemon.EncodeActorContext(projectdaemon.ActorContext{
		Version:     1,
		Issuer:      projectdaemon.ActorContextIssuerWikid,
		Subject:     "user:admin",
		Username:    "admin",
		Role:        "admin",
		WorkspaceID: "current",
		AuthMethod:  "disabled",
		IssuedAt:    now,
		ExpiresAt:   now.Add(5 * time.Minute),
	})
	if err != nil {
		t.Fatalf("EncodeActorContext failed: %v", err)
	}
	req := newPrivateRequest(http.MethodGet, "/api/tree", "private-token", actor)
	rec := requestWithRequest(router, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("valid private request status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
}

func TestPrivateAuthOptionsCarriesSemanticWorkspaceID(t *testing.T) {
	auth := PrivateAuthOptions{WorkspaceID: workspaceid.WorkspaceID("current")}

	var _ workspaceid.WorkspaceID = auth.WorkspaceID
}

func TestAuthenticatedRouterInstallsActorAsRequestUser(t *testing.T) {
	now := time.Date(2026, 6, 16, 12, 0, 0, 0, time.UTC)
	w := newTestWiki(t)
	defer w.Close()
	router := NewAuthenticatedRouter(w, httpinternal.RouterOptions{
		AllowInsecure:           true,
		AccessTokenTimeout:      15 * time.Minute,
		RefreshTokenTimeout:     7 * 24 * time.Hour,
		MaxAssetUploadSizeBytes: assets.DefaultMaxUploadSizeBytes,
	}, PrivateAuthOptions{
		DaemonToken: "private-token",
		WorkspaceID: "current",
		Now:         func() time.Time { return now },
	})
	actor, err := projectdaemon.EncodeActorContext(projectdaemon.ActorContext{
		Version:     1,
		Issuer:      projectdaemon.ActorContextIssuerWikid,
		Subject:     "user:editor-1",
		Username:    "editor",
		Email:       "editor@example.com",
		Role:        "editor",
		WorkspaceID: "current",
		AuthMethod:  "cookie",
		IssuedAt:    now,
		ExpiresAt:   now.Add(5 * time.Minute),
	})
	if err != nil {
		t.Fatalf("EncodeActorContext failed: %v", err)
	}

	req := newPrivateRequest(http.MethodGet, "/api/tree", "private-token", actor)
	rec := requestWithRequest(router, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("valid private authed request status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
}

func TestAuthenticatedRouterAllowsPrivateMutationWithoutPublicCSRFCookie(t *testing.T) {
	now := time.Date(2026, 6, 16, 12, 0, 0, 0, time.UTC)
	w := newTestWiki(t)
	defer w.Close()
	router := NewAuthenticatedRouter(w, httpinternal.RouterOptions{
		AllowInsecure:           true,
		AccessTokenTimeout:      15 * time.Minute,
		RefreshTokenTimeout:     7 * 24 * time.Hour,
		MaxAssetUploadSizeBytes: assets.DefaultMaxUploadSizeBytes,
	}, PrivateAuthOptions{
		DaemonToken: "private-token",
		WorkspaceID: "current",
		Now:         func() time.Time { return now },
	})
	actor, err := projectdaemon.EncodeActorContext(projectdaemon.ActorContext{
		Version:     1,
		Issuer:      projectdaemon.ActorContextIssuerWikid,
		Subject:     "user:editor-1",
		Username:    "editor",
		Role:        "editor",
		WorkspaceID: "current",
		AuthMethod:  "cookie",
		IssuedAt:    now,
		ExpiresAt:   now.Add(5 * time.Minute),
	})
	if err != nil {
		t.Fatalf("EncodeActorContext failed: %v", err)
	}

	req := newPrivateRequest(http.MethodPost, "/api/pages", "private-token", actor)
	req.Body = io.NopCloser(strings.NewReader(`{"kind":"page","slug":"private-mutation","title":"Private Mutation"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := requestWithRequest(router, req)
	if rec.Code == http.StatusForbidden && strings.Contains(rec.Body.String(), "CSRF") {
		t.Fatalf("private actor-context mutation was blocked by public CSRF middleware: %s", rec.Body.String())
	}
	if rec.Code != http.StatusCreated {
		t.Fatalf("private mutation status = %d, want 201: %s", rec.Code, rec.Body.String())
	}
}

func newPrivateRequest(method, path, token, actor string) *http.Request {
	req := httptest.NewRequest(method, path, nil)
	if token != "" {
		req.Header.Set(projectdaemon.ControlTokenHeader, token)
	}
	if actor != "" {
		req.Header.Set(projectdaemon.ActorContextHeader, actor)
	}
	return req
}

func requestWithRequest(router http.Handler, req *http.Request) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec
}

func assertStructuredPrivateAuthError(t *testing.T, rec *httptest.ResponseRecorder, code string, messageID string) {
	t.Helper()
	var body struct {
		Error struct {
			Code      string `json:"code"`
			MessageID string `json:"messageId"`
			Message   string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode private auth error: %v; body=%s", err, rec.Body.String())
	}
	if body.Error.Code != code || body.Error.MessageID != messageID || body.Error.Message == "" {
		t.Fatalf("private auth error = %#v, want %s/%s with message", body.Error, code, messageID)
	}
}
