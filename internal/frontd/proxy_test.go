package frontd

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	sharederrors "github.com/perber/wiki/internal/core/shared/errors"
	"github.com/perber/wiki/internal/projectdaemon"
)

type observedWorkspaceProxyRequest struct {
	Method        string
	Path          string
	Query         string
	Body          string
	Token         string
	ActorContext  string
	Authorization string
	Cookie        string
}

type observedMCPActorProxyRequest struct {
	Path          string
	Token         string
	ActorContext  string
	Authorization string
	Cookie        string
}

type observedControlPlaneProxyRequest struct {
	Path          string
	Query         string
	Host          string
	Token         string
	Authorization string
	Cookie        string
	ActorContext  string
}

var _ = Describe("frontd proxy routing", Label("integration"), func() {
	It("strips public credentials and injects private actor context for workspace requests", func() {
		now := time.Date(2026, 6, 16, 12, 0, 0, 0, time.UTC)
		var seen observedWorkspaceProxyRequest
		upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			seen.Method = req.Method
			seen.Path = req.URL.Path
			seen.Query = req.URL.RawQuery
			seen.Token = req.Header.Get(projectdaemon.ControlTokenHeader)
			seen.ActorContext = req.Header.Get(projectdaemon.ActorContextHeader)
			seen.Authorization = req.Header.Get("Authorization")
			seen.Cookie = req.Header.Get("Cookie")
			raw, _ := io.ReadAll(req.Body)
			seen.Body = string(raw)
			w.Header().Set("X-Upstream", "workspaced")
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte("proxied"))
		}))
		DeferCleanup(upstream.Close)

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
		Expect(err).To(Succeed())

		req := httptest.NewRequest(http.MethodPost, "/api/pages?draft=1", strings.NewReader(`{"title":"A"}`))
		req.Header.Set("Authorization", "Bearer public-token")
		req.Header.Set("Cookie", "leafwiki=public-cookie")
		req.Header.Set(projectdaemon.ActorContextHeader, "spoofed")
		rec := httptest.NewRecorder()
		proxy.ServeHTTP(rec, req)

		Expect(rec).To(HaveHTTPStatus(http.StatusCreated))
		Expect(rec).To(HaveHTTPBody("proxied"))
		Expect(rec.Header().Get("X-Upstream")).To(Equal("workspaced"))
		Expect(seen).To(SatisfyAll(
			HaveField("Method", Equal(http.MethodPost)),
			HaveField("Path", Equal("/api/pages")),
			HaveField("Query", Equal("draft=1")),
			HaveField("Body", Equal(`{"title":"A"}`)),
			HaveField("Token", Equal("private-token")),
			HaveField("Authorization", BeEmpty()),
			HaveField("Cookie", BeEmpty()),
		))
		decoded, err := projectdaemon.DecodeActorContext(seen.ActorContext, projectdaemon.ActorContextValidation{Now: now.Add(time.Minute), WorkspaceID: "current"})
		Expect(err).To(Succeed())
		Expect(decoded).To(SatisfyAll(
			HaveField("Subject", Equal("user:admin")),
			HaveField("AuthMethod", Equal("cookie")),
		))
	})

	It("strips public credentials and injects private actor context for MCP requests", func() {
		now := time.Date(2026, 6, 16, 12, 0, 0, 0, time.UTC)
		var seen observedMCPActorProxyRequest
		upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			seen.Path = req.URL.Path
			seen.Token = req.Header.Get(projectdaemon.ControlTokenHeader)
			seen.ActorContext = req.Header.Get(projectdaemon.ActorContextHeader)
			seen.Authorization = req.Header.Get("Authorization")
			seen.Cookie = req.Header.Get("Cookie")
			w.WriteHeader(http.StatusAccepted)
			_, _ = w.Write([]byte("mcp-proxied"))
		}))
		DeferCleanup(upstream.Close)

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
		Expect(err).To(Succeed())

		req := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(`{"jsonrpc":"2.0"}`))
		req.Header.Set("Authorization", "Bearer public-oauth-token")
		req.Header.Set("Cookie", "leafwiki_at=public-cookie")
		req.Header.Set(projectdaemon.ActorContextHeader, "spoofed")
		rec := httptest.NewRecorder()
		proxy.ServeHTTP(rec, req)

		Expect(rec).To(HaveHTTPStatus(http.StatusAccepted))
		Expect(rec).To(HaveHTTPBody("mcp-proxied"))
		Expect(seen).To(SatisfyAll(
			HaveField("Path", Equal("/mcp")),
			HaveField("Token", Equal("private-token")),
			HaveField("Authorization", BeEmpty()),
			HaveField("Cookie", BeEmpty()),
		))
		decoded, err := projectdaemon.DecodeActorContext(seen.ActorContext, projectdaemon.ActorContextValidation{Now: now.Add(time.Minute), WorkspaceID: "current"})
		Expect(err).To(Succeed())
		Expect(decoded).To(SatisfyAll(
			HaveField("Subject", Equal("user:editor-1")),
			HaveField("AuthMethod", Equal("oauth")),
		))
	})

	It("returns a retryable structured unavailable error when workspaced is down", func() {
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
		Expect(err).To(Succeed())

		rec := httptest.NewRecorder()
		proxy.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/tree", nil))

		Expect(rec).To(matchStructuredFrontdError(
			http.StatusServiceUnavailable,
			errCodeWorkspacedUnavailable,
			sharederrors.MessageIDForCode(errCodeWorkspacedUnavailable),
		))
		Expect(rec.Header().Get("Retry-After")).NotTo(BeEmpty())
	})

	It("returns a structured actor-resolution error before contacting workspaced", func() {
		upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			w.WriteHeader(http.StatusAccepted)
		}))
		DeferCleanup(upstream.Close)

		proxy, err := NewWorkspaceProxy(WorkspaceProxyOptions{
			Upstream:    upstream.URL,
			DaemonToken: "private-token",
			Actor: func(*http.Request) (projectdaemon.ActorContext, error) {
				return projectdaemon.ActorContext{}, errors.New("no actor")
			},
		})
		Expect(err).To(Succeed())

		rec := httptest.NewRecorder()
		proxy.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/tree", nil))

		Expect(rec).To(matchStructuredFrontdError(
			http.StatusUnauthorized,
			errCodeWorkspaceActorContextFailed,
			sharederrors.MessageIDForCode(errCodeWorkspaceActorContextFailed),
		))
	})

	It("preserves public credentials while adding the private token for the control plane", func() {
		var seen observedControlPlaneProxyRequest
		upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			seen.Path = req.URL.Path
			seen.Query = req.URL.RawQuery
			seen.Host = req.Host
			seen.Token = req.Header.Get(projectdaemon.ControlTokenHeader)
			seen.Authorization = req.Header.Get("Authorization")
			seen.Cookie = req.Header.Get("Cookie")
			seen.ActorContext = req.Header.Get(projectdaemon.ActorContextHeader)
			w.Header().Set("Set-Cookie", "leafwiki_at=wikid-token")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("wikid"))
		}))
		DeferCleanup(upstream.Close)

		proxy, err := NewControlPlaneProxy(upstream.URL, "private-token")
		Expect(err).To(Succeed())

		req := httptest.NewRequest(http.MethodGet, "/api/auth/me?fresh=1", nil)
		req.Host = "public.leafwiki.test"
		req.Header.Set("Authorization", "Bearer public-token")
		req.Header.Set("Cookie", "leafwiki_at=public-cookie")
		req.Header.Set(projectdaemon.ActorContextHeader, "spoofed")
		rec := httptest.NewRecorder()
		proxy.ServeHTTP(rec, req)

		Expect(rec).To(HaveHTTPStatus(http.StatusOK))
		Expect(rec).To(HaveHTTPBody("wikid"))
		Expect(rec.Header().Get("Set-Cookie")).To(Equal("leafwiki_at=wikid-token"))
		Expect(seen).To(SatisfyAll(
			HaveField("Path", Equal("/__leafwiki/control-plane/api/auth/me")),
			HaveField("Query", Equal("fresh=1")),
			HaveField("Host", Equal("public.leafwiki.test")),
			HaveField("Token", Equal("private-token")),
			HaveField("Authorization", Equal("Bearer public-token")),
			HaveField("Cookie", Equal("leafwiki_at=public-cookie")),
			HaveField("ActorContext", BeEmpty()),
		))
	})

	It("routes workspace, MCP, and control-plane prefixes before the public router", func() {
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
		Expect(rec).To(HaveHTTPBody("workspace"))
		Expect(workspacePath).To(Equal("/api/tree"))

		rec = httptest.NewRecorder()
		handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/wiki/api/workspaces", nil))
		Expect(rec).To(HaveHTTPBody("workspaces"))
		Expect(workspacesPath).To(Equal("/api/workspaces"))

		rec = httptest.NewRecorder()
		handler.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/wiki/mcp", nil))
		Expect(rec).To(HaveHTTPBody("mcp"))
		Expect(mcpPath).To(Equal("/mcp"))

		rec = httptest.NewRecorder()
		handler.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/wiki/mcp/workspaces/home", nil))
		Expect(rec).To(HaveHTTPBody("mcp"))
		Expect(mcpPath).To(Equal("/mcp/workspaces/home"))

		rec = httptest.NewRecorder()
		handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/wiki/api/config", nil))
		Expect(rec).To(HaveHTTPBody("control"))
		Expect(controlPath).To(Equal("/api/config"))
	})

	It("routes root well-known metadata to the control plane before base-path stripping", func() {
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

		Expect(rec).To(HaveHTTPStatus(http.StatusAccepted))
		Expect(rec).To(HaveHTTPBody("control"))
		Expect(controlPath).To(Equal("/.well-known/oauth-protected-resource/wiki/mcp"))
	})
})
