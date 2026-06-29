package frontd

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	"github.com/perber/wiki/internal/projectdaemon"
	"github.com/perber/wiki/internal/workspaceid"
)

var _ = DescribeTable("NewMCPProxy validates constructor inputs",
	func(upstream string, token string, wantErr string) {
		t := GinkgoT()

		proxy, err := NewMCPProxy(upstream, token)
		if err == nil {
			t.Fatalf("NewMCPProxy(%q, %q) returned proxy %#v, want error", upstream, token, proxy)
		}
		if !strings.Contains(err.Error(), wantErr) {
			t.Fatalf("NewMCPProxy error = %q, want substring %q", err.Error(), wantErr)
		}
	},
	Entry("invalid upstream URL", "://bad-url", "daemon-token", "invalid workspaced upstream"),
	Entry("missing daemon token", "http://127.0.0.1:1", "   ", "daemon token is required"),
)

var _ = It("NewMCPProxy injects the daemon token and strips public actor context", func() {
	t := GinkgoT()
	var seen struct {
		path         string
		token        string
		actorContext string
		body         string
	}
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		seen.path = req.URL.Path
		seen.token = req.Header.Get(projectdaemon.ControlTokenHeader)
		seen.actorContext = req.Header.Get(projectdaemon.ActorContextHeader)
		raw, _ := io.ReadAll(req.Body)
		seen.body = string(raw)
		w.WriteHeader(http.StatusAccepted)
		_, _ = w.Write([]byte("mcp"))
	}))
	DeferCleanup(upstream.Close)

	proxy, err := NewMCPProxy(upstream.URL, "daemon-token")
	if err != nil {
		t.Fatalf("NewMCPProxy failed: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(`{"jsonrpc":"2.0"}`))
	req.Header.Set(projectdaemon.ActorContextHeader, "spoofed")
	rec := httptest.NewRecorder()
	proxy.ServeHTTP(rec, req)

	if rec.Code != http.StatusAccepted || rec.Body.String() != "mcp" {
		t.Fatalf("proxy response = %d %q", rec.Code, rec.Body.String())
	}
	if seen.path != "/mcp" || seen.body != `{"jsonrpc":"2.0"}` {
		t.Fatalf("forwarded request path/body = %q/%q", seen.path, seen.body)
	}
	if seen.token != "daemon-token" {
		t.Fatalf("daemon token = %q, want daemon-token", seen.token)
	}
	if seen.actorContext != "" {
		t.Fatalf("public actor context reached MCP upstream: %q", seen.actorContext)
	}
})

var _ = It("mcpSessionResponseWriter records implicit OK status and unwraps the recorder", func() {
	t := GinkgoT()
	rec := httptest.NewRecorder()
	writer := &mcpSessionResponseWriter{ResponseWriter: rec}

	n, err := writer.Write([]byte("body"))
	if err != nil {
		t.Fatalf("Write failed: %v", err)
	}
	if n != len("body") {
		t.Fatalf("Write bytes = %d, want %d", n, len("body"))
	}
	if writer.statusCode != http.StatusOK {
		t.Fatalf("statusCode = %d, want 200", writer.statusCode)
	}
	if writer.Unwrap() != rec {
		t.Fatalf("Unwrap did not return underlying recorder")
	}
})

var _ = DescribeTable("parseWorkspaceMCPPath rejects invalid path shapes",
	func(path string) {
		t := GinkgoT()
		if workspaceID, ok := parseWorkspaceMCPPath(path); ok || workspaceID != "" {
			t.Fatalf("parseWorkspaceMCPPath(%q) = %q/%v, want empty/false", path, workspaceID, ok)
		}
	},
	Entry("root MCP path", "/mcp"),
	Entry("missing workspace ID", "/mcp/workspaces/"),
	Entry("empty workspace ID segment", "/mcp/workspaces//messages"),
	Entry("invalid workspace ID", "/mcp/workspaces/%20bad"),
)

var _ = It("WorkspaceMCPHandler does not unbind an empty DELETE session", func() {
	t := GinkgoT()
	bindings := NewMCPSessionBindings()
	if err := bindings.Bind("session-1", "alpha"); err != nil {
		t.Fatalf("Bind failed: %v", err)
	}
	handler := NewWorkspaceMCPHandler(WorkspaceMCPHandlerOptions{
		Sessions: bindings,
		Resolve: func(_ *http.Request, workspaceID workspaceid.WorkspaceID) (WorkspaceRoute, error) {
			return WorkspaceRoute{WorkspaceID: workspaceID, Upstream: "http://127.0.0.1:1", DaemonToken: "token"}, nil
		},
		Proxy: func(WorkspaceRoute) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusNoContent)
			})
		},
	})

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodDelete, "/mcp/workspaces/alpha", nil))

	if rec.Code != http.StatusNoContent {
		t.Fatalf("delete status = %d, want 204: %s", rec.Code, rec.Body.String())
	}
	if workspace, ok := bindings.Workspace("session-1"); !ok || workspace != "alpha" {
		t.Fatalf("existing session binding = %q/%v, want alpha/true", workspace, ok)
	}
})

var _ = DescribeTable("Ingress base-path routing edge cases",
	func(path string, basePath string, wantBody string) {
		t := GinkgoT()
		public := http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			_, _ = w.Write([]byte("public:" + req.URL.Path))
		})
		control := http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			_, _ = w.Write([]byte("control:" + req.URL.Path))
		})
		handler := NewIngressHandler(public, IngressOptions{
			BasePath:     basePath,
			ControlPlane: control,
		})

		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))

		if rec.Body.String() != wantBody {
			t.Fatalf("ingress body = %q, want %q", rec.Body.String(), wantBody)
		}
	},
	Entry("exact base path strips to root public route", "/wiki", "/wiki", "public:/wiki"),
	Entry("nonmatching base path falls through to public", "/wikid/api/config", "/wiki", "public:/wikid/api/config"),
	Entry("root well-known keeps control-plane precedence", "/.well-known/oauth-authorization-server", "/wiki", "control:/.well-known/oauth-authorization-server"),
)

var _ = It("WorkspaceRouterProxy reports malformed workspace paths before resolving", func() {
	t := GinkgoT()
	proxy := NewWorkspaceRouterProxy(WorkspaceRouterProxyOptions{
		Resolve: func(*http.Request, workspaceid.WorkspaceID) (WorkspaceRoute, error) {
			return WorkspaceRoute{}, errors.New("resolver should not be called")
		},
	})

	rec := httptest.NewRecorder()
	proxy.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/workspaces//tree", nil))

	if rec.Code != http.StatusNotFound {
		t.Fatalf("malformed workspace route status = %d, want 404: %s", rec.Code, rec.Body.String())
	}
})
