package frontd

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/perber/wiki/internal/projectdaemon"
	"github.com/perber/wiki/internal/workspaceid"
)

type observedMCPProxyRequest struct {
	Path         string
	Token        string
	ActorContext string
	Body         string
}

var _ = Describe("frontd MCP proxy behavior", func() {
	DescribeTable("constructor validation",
		func(upstream string, token string, wantErr error) {
			proxy, err := NewMCPProxy(upstream, token)

			Expect(proxy).To(BeNil())
			Expect(err).To(MatchError(wantErr))
		},
		Entry("rejects an invalid upstream URL", "://bad-url", "daemon-token", errInvalidWorkspacedUpstream),
		Entry("rejects a missing daemon token", "http://127.0.0.1:1", "   ", errDaemonTokenRequired),
	)

	It("injects the daemon token and strips public actor context", func() {
		var seen observedMCPProxyRequest
		upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			seen.Path = req.URL.Path
			seen.Token = req.Header.Get(projectdaemon.ControlTokenHeader)
			seen.ActorContext = req.Header.Get(projectdaemon.ActorContextHeader)
			raw, _ := io.ReadAll(req.Body)
			seen.Body = string(raw)
			w.WriteHeader(http.StatusAccepted)
			_, _ = w.Write([]byte("mcp"))
		}))
		DeferCleanup(upstream.Close)

		proxy, err := NewMCPProxy(upstream.URL, "daemon-token")
		Expect(err).To(Succeed())

		req := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(`{"jsonrpc":"2.0"}`))
		req.Header.Set(projectdaemon.ActorContextHeader, "spoofed")
		rec := httptest.NewRecorder()
		proxy.ServeHTTP(rec, req)

		Expect(rec).To(HaveHTTPStatus(http.StatusAccepted))
		Expect(rec).To(HaveHTTPBody("mcp"))
		Expect(seen).To(SatisfyAll(
			HaveField("Path", Equal("/mcp")),
			HaveField("Body", Equal(`{"jsonrpc":"2.0"}`)),
			HaveField("Token", Equal("daemon-token")),
			HaveField("ActorContext", BeEmpty()),
		))
	})

	It("records implicit OK status and unwraps the session recorder", func() {
		rec := httptest.NewRecorder()
		writer := &mcpSessionResponseWriter{ResponseWriter: rec}

		n, err := writer.Write([]byte("body"))
		Expect(err).To(Succeed())
		Expect(n).To(Equal(len("body")))
		Expect(writer.statusCode).To(Equal(http.StatusOK))
		Expect(writer.Unwrap()).To(Equal(rec))
	})

	DescribeTable("workspace MCP path parsing",
		func(path string) {
			workspaceID, ok := parseWorkspaceMCPPath(path)
			Expect(ok).To(BeFalse())
			Expect(workspaceID).To(BeEmpty())
		},
		Entry("rejects the root MCP path", "/mcp"),
		Entry("rejects a missing workspace ID", "/mcp/workspaces/"),
		Entry("rejects an empty workspace ID segment", "/mcp/workspaces//messages"),
		Entry("rejects an invalid workspace ID", "/mcp/workspaces/%20bad"),
	)

	It("preserves an existing binding when DELETE has no session header", func() {
		bindings := NewMCPSessionBindings()
		Expect(bindings.Bind(MCPSessionIDFromHeader("session-1"), "alpha")).To(Succeed())
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

		Expect(rec).To(HaveHTTPStatus(http.StatusNoContent))
		workspace, ok := bindings.Workspace(MCPSessionIDFromHeader("session-1"))
		Expect(ok).To(BeTrue())
		Expect(workspace).To(Equal(workspaceid.WorkspaceID("alpha")))
	})

	DescribeTable("ingress base-path routing",
		func(path string, basePath string, wantBody string) {
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

			Expect(rec).To(HaveHTTPBody(wantBody))
		},
		Entry("leaves exact base path on the public route", "/wiki", "/wiki", "public:/wiki"),
		Entry("lets nonmatching base paths fall through to public routing", "/wikid/api/config", "/wiki", "public:/wikid/api/config"),
		Entry("keeps root well-known paths on the control plane", "/.well-known/oauth-authorization-server", "/wiki", "control:/.well-known/oauth-authorization-server"),
	)

	It("rejects malformed workspace paths before resolving", func() {
		proxy := NewWorkspaceRouterProxy(WorkspaceRouterProxyOptions{
			Resolve: func(*http.Request, workspaceid.WorkspaceID) (WorkspaceRoute, error) {
				return WorkspaceRoute{}, errors.New("resolver should not be called")
			},
		})

		rec := httptest.NewRecorder()
		proxy.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/workspaces//tree", nil))

		Expect(rec).To(HaveHTTPStatus(http.StatusNotFound))
	})
})
