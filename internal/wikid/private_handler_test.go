package wikid

import (
	ginkgo "github.com/onsi/ginkgo/v2"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"

	"github.com/perber/wiki/internal/frontd"
	"github.com/perber/wiki/internal/projectdaemon"
)

var _ = ginkgo.It("TestPrivateHandlerRequiresDaemonTokenForWikidEndpoints", func() {
	t := ginkgo.GinkgoT()
	handler := NewPrivateHandler(PrivateHandlerOptions{
		DaemonToken: "private-token",
		ActorContext: http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			t.Fatalf("actor context handler was called without daemon token")
		}),
	})

	req := httptest.NewRequest(http.MethodPost, "/__leafwiki/actor-context", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401: %s", rec.Code, rec.Body.String())
	}
})

var _ = ginkgo.It("TestPrivateHandlerRequiresDaemonTokenForWorkspaceAPI", func() {
	t := ginkgo.GinkgoT()
	handler := NewPrivateHandler(PrivateHandlerOptions{
		DaemonToken: "private-token",
		WorkspaceAPI: http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			t.Fatalf("workspace API handler was called without daemon token")
		}),
	})

	req := httptest.NewRequest(http.MethodGet, "/__leafwiki/workspaces", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401: %s", rec.Code, rec.Body.String())
	}
})

var _ = ginkgo.It("TestPrivateHandlerForwardsWorkspaceAPIWithDaemonToken", func() {
	t := ginkgo.GinkgoT()
	var seenPath string
	handler := NewPrivateHandler(PrivateHandlerOptions{
		DaemonToken: "private-token",
		WorkspaceAPI: http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			seenPath = req.URL.Path
			w.WriteHeader(http.StatusAccepted)
		}),
	})

	req := httptest.NewRequest(http.MethodGet, "/__leafwiki/workspaces", nil)
	req.Header.Set(projectdaemon.ControlTokenHeader, "private-token")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want 202: %s", rec.Code, rec.Body.String())
	}
	if seenPath != "/__leafwiki/workspaces" {
		t.Fatalf("seen path = %q", seenPath)
	}
})

var _ = ginkgo.It("TestPrivateHandlerForwardsControlPlaneWithBasePathAndPrivateHeadersStripped", func() {
	t := ginkgo.GinkgoT()
	var seen struct {
		path         string
		token        string
		actorContext string
		remoteAddr   string
		body         string
	}
	controlPlane := http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		seen.path = req.URL.Path
		seen.token = req.Header.Get(projectdaemon.ControlTokenHeader)
		seen.actorContext = req.Header.Get(projectdaemon.ActorContextHeader)
		seen.remoteAddr = req.RemoteAddr
		raw, _ := io.ReadAll(req.Body)
		seen.body = string(raw)
		w.WriteHeader(http.StatusAccepted)
	})
	handler := NewPrivateHandler(PrivateHandlerOptions{
		DaemonToken:  "private-token",
		BasePath:     "/wiki",
		ControlPlane: controlPlane,
	})

	req := httptest.NewRequest(http.MethodPost, frontd.ControlPlanePrefix+"/api/branding", strings.NewReader(`{"siteName":"Runtime Wiki"}`))
	req.RemoteAddr = "127.0.0.1:1111"
	req.Header.Set(projectdaemon.ControlTokenHeader, "private-token")
	req.Header.Set(projectdaemon.ActorContextHeader, "spoofed")
	req.Header.Set("X-LeafWiki-Original-Remote-Addr", "203.0.113.10:4321")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want 202: %s", rec.Code, rec.Body.String())
	}
	if seen.path != "/wiki/api/branding" {
		t.Fatalf("forwarded path = %q, want /wiki/api/branding", seen.path)
	}
	if seen.token != "" || seen.actorContext != "" {
		t.Fatalf("private headers reached control plane: token=%q actor=%q", seen.token, seen.actorContext)
	}
	if seen.remoteAddr != "203.0.113.10:4321" {
		t.Fatalf("remote addr = %q, want original remote addr", seen.remoteAddr)
	}
	if seen.body != `{"siteName":"Runtime Wiki"}` {
		t.Fatalf("body = %q", seen.body)
	}
})

var _ = ginkgo.It("TestPrivateHandlerForwardsRootWellKnownControlPlanePathWithoutRebasing", func() {
	t := ginkgo.GinkgoT()
	var seenPath string
	controlPlane := http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		seenPath = req.URL.Path
		w.WriteHeader(http.StatusAccepted)
	})
	handler := NewPrivateHandler(PrivateHandlerOptions{
		DaemonToken:  "private-token",
		BasePath:     "/wiki",
		ControlPlane: controlPlane,
	})

	req := httptest.NewRequest(http.MethodGet, frontd.ControlPlanePrefix+"/.well-known/oauth-protected-resource/wiki/mcp", nil)
	req.Header.Set(projectdaemon.ControlTokenHeader, "private-token")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want 202: %s", rec.Code, rec.Body.String())
	}
	if seenPath != "/.well-known/oauth-protected-resource/wiki/mcp" {
		t.Fatalf("forwarded path = %q, want root well-known path", seenPath)
	}
})
