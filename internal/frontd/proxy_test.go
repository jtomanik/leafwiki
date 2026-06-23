package frontd

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/perber/wiki/internal/projectdaemon"
)

func assertFrontdProxyError(t *testing.T, rec *httptest.ResponseRecorder, wantStatus int, wantCode string, wantMessageID string) {
	t.Helper()
	if rec.Code != wantStatus {
		t.Fatalf("status = %d, want %d: %s", rec.Code, wantStatus, rec.Body.String())
	}
	var body struct {
		Error struct {
			Code      string `json:"code"`
			MessageID string `json:"messageId"`
			Message   string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("error body is not structured JSON: %v body=%q", err, rec.Body.String())
	}
	if body.Error.Code != wantCode || body.Error.MessageID != wantMessageID || body.Error.Message == "" {
		t.Fatalf("error = %#v, want code=%q messageId=%q", body.Error, wantCode, wantMessageID)
	}
}

func TestWorkspaceProxyStripsPublicCredentialsAndInjectsPrivateActorContext(t *testing.T) {
	now := time.Date(2026, 6, 16, 12, 0, 0, 0, time.UTC)
	var seen struct {
		method        string
		path          string
		query         string
		body          string
		token         string
		actorContext  string
		authorization string
		cookie        string
	}
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		seen.method = req.Method
		seen.path = req.URL.Path
		seen.query = req.URL.RawQuery
		seen.token = req.Header.Get(projectdaemon.ControlTokenHeader)
		seen.actorContext = req.Header.Get(projectdaemon.ActorContextHeader)
		seen.authorization = req.Header.Get("Authorization")
		seen.cookie = req.Header.Get("Cookie")
		raw, _ := io.ReadAll(req.Body)
		seen.body = string(raw)
		w.Header().Set("X-Upstream", "workspaced")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte("proxied"))
	}))
	defer upstream.Close()

	proxy, err := NewWorkspaceProxy(WorkspaceProxyOptions{
		Upstream:    upstream.URL,
		DaemonToken: "private-token",
		Actor: func(*http.Request) (projectdaemon.ActorContext, error) {
			return projectdaemon.ActorContext{
				Version:     1,
				Issuer:      projectdaemon.ActorContextIssuerWikid,
				Subject:     "user:admin",
				Username:    "admin",
				Role:        "admin",
				WorkspaceID: "current",
				AuthMethod:  "cookie",
				IssuedAt:    now,
				ExpiresAt:   now.Add(5 * time.Minute),
			}, nil
		},
	})
	if err != nil {
		t.Fatalf("NewWorkspaceProxy failed: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/pages?draft=1", strings.NewReader(`{"title":"A"}`))
	req.Header.Set("Authorization", "Bearer public-token")
	req.Header.Set("Cookie", "leafwiki=public-cookie")
	req.Header.Set(projectdaemon.ActorContextHeader, "spoofed")
	rec := httptest.NewRecorder()
	proxy.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated || rec.Body.String() != "proxied" || rec.Header().Get("X-Upstream") != "workspaced" {
		t.Fatalf("proxy response = %d %q headers=%v", rec.Code, rec.Body.String(), rec.Header())
	}
	if seen.method != http.MethodPost || seen.path != "/api/pages" || seen.query != "draft=1" || seen.body != `{"title":"A"}` {
		t.Fatalf("forwarded request = %#v", seen)
	}
	if seen.token != "private-token" {
		t.Fatalf("daemon token = %q, want private-token", seen.token)
	}
	if seen.authorization != "" || seen.cookie != "" {
		t.Fatalf("public credentials reached workspaced: auth=%q cookie=%q", seen.authorization, seen.cookie)
	}
	decoded, err := projectdaemon.DecodeActorContext(seen.actorContext, projectdaemon.ActorContextValidation{Now: now.Add(time.Minute), WorkspaceID: "current"})
	if err != nil {
		t.Fatalf("actor context was not valid: %v", err)
	}
	if decoded.Subject != "user:admin" || decoded.AuthMethod != "cookie" {
		t.Fatalf("actor context = %#v", decoded)
	}
}

func TestMCPProxyWithActorStripsPublicCredentialsAndInjectsPrivateActorContext(t *testing.T) {
	now := time.Date(2026, 6, 16, 12, 0, 0, 0, time.UTC)
	var seen struct {
		path          string
		token         string
		actorContext  string
		authorization string
		cookie        string
	}
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		seen.path = req.URL.Path
		seen.token = req.Header.Get(projectdaemon.ControlTokenHeader)
		seen.actorContext = req.Header.Get(projectdaemon.ActorContextHeader)
		seen.authorization = req.Header.Get("Authorization")
		seen.cookie = req.Header.Get("Cookie")
		w.WriteHeader(http.StatusAccepted)
		_, _ = w.Write([]byte("mcp-proxied"))
	}))
	defer upstream.Close()

	proxy, err := NewMCPProxyWithActor(WorkspaceProxyOptions{
		Upstream:    upstream.URL,
		DaemonToken: "private-token",
		Actor: func(*http.Request) (projectdaemon.ActorContext, error) {
			return projectdaemon.ActorContext{
				Version:     1,
				Issuer:      projectdaemon.ActorContextIssuerWikid,
				Subject:     "user:editor-1",
				Username:    "editor",
				Role:        "editor",
				WorkspaceID: "current",
				AuthMethod:  "oauth",
				IssuedAt:    now,
				ExpiresAt:   now.Add(5 * time.Minute),
			}, nil
		},
	})
	if err != nil {
		t.Fatalf("NewMCPProxyWithActor failed: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(`{"jsonrpc":"2.0"}`))
	req.Header.Set("Authorization", "Bearer public-oauth-token")
	req.Header.Set("Cookie", "leafwiki_at=public-cookie")
	req.Header.Set(projectdaemon.ActorContextHeader, "spoofed")
	rec := httptest.NewRecorder()
	proxy.ServeHTTP(rec, req)

	if rec.Code != http.StatusAccepted || rec.Body.String() != "mcp-proxied" {
		t.Fatalf("proxy response = %d %q", rec.Code, rec.Body.String())
	}
	if seen.path != "/mcp" || seen.token != "private-token" {
		t.Fatalf("forwarded path/token = %q/%q", seen.path, seen.token)
	}
	if seen.authorization != "" || seen.cookie != "" {
		t.Fatalf("public credentials reached private MCP: auth=%q cookie=%q", seen.authorization, seen.cookie)
	}
	decoded, err := projectdaemon.DecodeActorContext(seen.actorContext, projectdaemon.ActorContextValidation{Now: now.Add(time.Minute), WorkspaceID: "current"})
	if err != nil {
		t.Fatalf("actor context was not valid: %v", err)
	}
	if decoded.Subject != "user:editor-1" || decoded.AuthMethod != "oauth" {
		t.Fatalf("actor context = %#v", decoded)
	}
}

func TestWorkspaceProxyReturnsRetryableUnavailableWhenUpstreamIsDown(t *testing.T) {
	proxy, err := NewWorkspaceProxy(WorkspaceProxyOptions{
		Upstream:    "http://127.0.0.1:1",
		DaemonToken: "private-token",
		Actor: func(*http.Request) (projectdaemon.ActorContext, error) {
			now := time.Date(2026, 6, 16, 12, 0, 0, 0, time.UTC)
			return projectdaemon.ActorContext{
				Version:     1,
				Issuer:      projectdaemon.ActorContextIssuerWikid,
				Subject:     "user:editor",
				Username:    "editor",
				Role:        "editor",
				WorkspaceID: "current",
				AuthMethod:  "cookie",
				IssuedAt:    now,
				ExpiresAt:   now.Add(5 * time.Minute),
			}, nil
		},
	})
	if err != nil {
		t.Fatalf("NewWorkspaceProxy failed: %v", err)
	}

	rec := httptest.NewRecorder()
	proxy.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/tree", nil))

	assertFrontdProxyError(t, rec, http.StatusServiceUnavailable, "workspaced_unavailable", "errors.workspaced.unavailable")
	if rec.Header().Get("Retry-After") == "" {
		t.Fatalf("Retry-After header missing")
	}
}

func TestWorkspaceProxyReturnsStructuredActorResolutionError(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		t.Fatalf("upstream should not be called")
	}))
	defer upstream.Close()

	proxy, err := NewWorkspaceProxy(WorkspaceProxyOptions{
		Upstream:    upstream.URL,
		DaemonToken: "private-token",
		Actor: func(*http.Request) (projectdaemon.ActorContext, error) {
			return projectdaemon.ActorContext{}, errors.New("no actor")
		},
	})
	if err != nil {
		t.Fatalf("NewWorkspaceProxy failed: %v", err)
	}

	rec := httptest.NewRecorder()
	proxy.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/tree", nil))

	assertFrontdProxyError(t, rec, http.StatusUnauthorized, "workspace_actor_context_failed", "errors.workspace.actor_context_failed")
}

func TestControlPlaneProxyPreservesPublicCredentialsAndAddsPrivateToken(t *testing.T) {
	var seen struct {
		path          string
		query         string
		host          string
		token         string
		authorization string
		cookie        string
		actorContext  string
	}
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		seen.path = req.URL.Path
		seen.query = req.URL.RawQuery
		seen.host = req.Host
		seen.token = req.Header.Get(projectdaemon.ControlTokenHeader)
		seen.authorization = req.Header.Get("Authorization")
		seen.cookie = req.Header.Get("Cookie")
		seen.actorContext = req.Header.Get(projectdaemon.ActorContextHeader)
		w.Header().Set("Set-Cookie", "leafwiki_at=wikid-token")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("wikid"))
	}))
	defer upstream.Close()

	proxy, err := NewControlPlaneProxy(upstream.URL, "private-token")
	if err != nil {
		t.Fatalf("NewControlPlaneProxy failed: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/auth/me?fresh=1", nil)
	req.Host = "public.leafwiki.test"
	req.Header.Set("Authorization", "Bearer public-token")
	req.Header.Set("Cookie", "leafwiki_at=public-cookie")
	req.Header.Set(projectdaemon.ActorContextHeader, "spoofed")
	rec := httptest.NewRecorder()
	proxy.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK || rec.Body.String() != "wikid" || rec.Header().Get("Set-Cookie") != "leafwiki_at=wikid-token" {
		t.Fatalf("proxy response = %d %q headers=%v", rec.Code, rec.Body.String(), rec.Header())
	}
	if seen.path != "/__leafwiki/control-plane/api/auth/me" || seen.query != "fresh=1" {
		t.Fatalf("forwarded path/query = %q/%q", seen.path, seen.query)
	}
	if seen.host != "public.leafwiki.test" {
		t.Fatalf("forwarded host = %q, want public host", seen.host)
	}
	if seen.token != "private-token" {
		t.Fatalf("daemon token = %q, want private-token", seen.token)
	}
	if seen.authorization != "Bearer public-token" || seen.cookie != "leafwiki_at=public-cookie" {
		t.Fatalf("public credentials were not preserved for wikid: auth=%q cookie=%q", seen.authorization, seen.cookie)
	}
	if seen.actorContext != "" {
		t.Fatalf("spoofed actor context reached wikid: %q", seen.actorContext)
	}
}

func TestIngressHandlerRoutesWorkspaceAndMCPBeforePublicRouter(t *testing.T) {
	var workspacePath string
	var workspacesPath string
	var mcpPath string
	var controlPath string
	public := http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		_, _ = w.Write([]byte("public:" + req.URL.Path))
	})
	workspace := http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		workspacePath = req.URL.Path
		_, _ = w.Write([]byte("workspace"))
	})
	workspaces := http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		workspacesPath = req.URL.Path
		_, _ = w.Write([]byte("workspaces"))
	})
	mcp := http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		mcpPath = req.URL.Path
		_, _ = w.Write([]byte("mcp"))
	})
	control := http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		controlPath = req.URL.Path
		_, _ = w.Write([]byte("control"))
	})
	handler := NewIngressHandler(public, IngressOptions{
		BasePath:     "/wiki",
		Workspace:    workspace,
		Workspaces:   workspaces,
		MCP:          mcp,
		ControlPlane: control,
	})

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/wiki/api/tree?depth=1", nil))
	if rec.Body.String() != "workspace" || workspacePath != "/api/tree" {
		t.Fatalf("workspace dispatch body/path = %q/%q", rec.Body.String(), workspacePath)
	}

	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/wiki/api/workspaces", nil))
	if rec.Body.String() != "workspaces" || workspacesPath != "/api/workspaces" {
		t.Fatalf("workspaces dispatch body/path = %q/%q", rec.Body.String(), workspacesPath)
	}

	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/wiki/mcp", nil))
	if rec.Body.String() != "mcp" || mcpPath != "/mcp" {
		t.Fatalf("mcp dispatch body/path = %q/%q", rec.Body.String(), mcpPath)
	}

	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/wiki/mcp/workspaces/home", nil))
	if rec.Body.String() != "mcp" || mcpPath != "/mcp/workspaces/home" {
		t.Fatalf("workspace mcp dispatch body/path = %q/%q", rec.Body.String(), mcpPath)
	}

	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/wiki/api/config", nil))
	if rec.Body.String() != "control" || controlPath != "/api/config" {
		t.Fatalf("control dispatch body/path = %q/%q", rec.Body.String(), controlPath)
	}
}

func TestIngressHandlerRoutesRootWellKnownMetadataToControlPlaneBeforeBasePathStripping(t *testing.T) {
	var controlPath string
	public := http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		_, _ = w.Write([]byte("public:" + req.URL.Path))
	})
	control := http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		controlPath = req.URL.Path
		w.WriteHeader(http.StatusAccepted)
		_, _ = w.Write([]byte("control"))
	})
	handler := NewIngressHandler(public, IngressOptions{
		BasePath:     "/wiki",
		ControlPlane: control,
	})

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/.well-known/oauth-protected-resource/wiki/mcp", nil))

	if rec.Code != http.StatusAccepted || rec.Body.String() != "control" {
		t.Fatalf("root well-known response = %d %q, want control-plane 202", rec.Code, rec.Body.String())
	}
	if controlPath != "/.well-known/oauth-protected-resource/wiki/mcp" {
		t.Fatalf("control-plane path = %q, want root well-known path", controlPath)
	}
}
