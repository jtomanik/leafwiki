package frontd

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"time"

	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/gstruct"
	"github.com/onsi/gomega/types"

	sharederrors "github.com/perber/wiki/internal/core/shared/errors"
	"github.com/perber/wiki/internal/projectdaemon"
	"github.com/perber/wiki/internal/workspaceid"
)

func validFrontdActor(workspaceID workspaceid.WorkspaceID) projectdaemon.ActorContext {
	return projectdaemon.ActorContext{
		Version:     1,
		Issuer:      projectdaemon.ActorContextIssuerWikid,
		Subject:     "user:admin",
		WorkspaceID: workspaceID,
		ExpiresAt:   time.Now().Add(time.Minute),
	}
}

var _ = ginkgo.Describe("frontd routing and proxy edge behavior", func() {
	ginkgo.It("validates proxy constructors and small path helpers", func() {
		_, err := NewControlPlaneProxy("://bad", "token")
		Expect(err).To(MatchError(errInvalidWikidUpstream))
		_, err = NewControlPlaneProxy("http://127.0.0.1:1", " ")
		Expect(err).To(MatchError(errDaemonTokenRequired))

		_, err = NewWorkspaceProxy(WorkspaceProxyOptions{Upstream: "://bad", DaemonToken: "token", Actor: func(*http.Request) (projectdaemon.ActorContext, error) {
			return validFrontdActor("home"), nil
		}})
		Expect(err).To(MatchError(errInvalidWorkspacedUpstream))
		_, err = NewWorkspaceProxy(WorkspaceProxyOptions{Upstream: "http://127.0.0.1:1", DaemonToken: " ", Actor: func(*http.Request) (projectdaemon.ActorContext, error) {
			return validFrontdActor("home"), nil
		}})
		Expect(err).To(MatchError(errDaemonTokenRequired))
		_, err = NewWorkspaceProxy(WorkspaceProxyOptions{Upstream: "http://127.0.0.1:1", DaemonToken: "token"})
		Expect(err).To(MatchError(errActorContextResolverRequired))

		_, err = NewWorkspacesAPI("://bad", "token")
		Expect(err).To(MatchError(errInvalidWikidUpstream))
		_, err = NewWorkspacesAPI("http://127.0.0.1:1", " ")
		Expect(err).To(MatchError(errDaemonTokenRequired))

		path, ok := stripBasePath("", "")
		Expect(path).To(Equal("/"))
		Expect(ok).To(BeTrue())
		path, ok = stripBasePath("/path", "")
		Expect(path).To(Equal("/path"))
		Expect(ok).To(BeTrue())
		Expect(ensureLeadingSlash("api")).To(Equal("/api"))
		Expect(isWorkspacePath(PublicWorkspacesPrefix + "/home/tree")).To(BeTrue())
	})

	ginkgo.It("maps workspace proxy actor and encoding failures", func() {
		upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			w.WriteHeader(http.StatusNoContent)
		}))
		ginkgo.DeferCleanup(upstream.Close)

		handler, err := NewWorkspaceProxy(WorkspaceProxyOptions{
			Upstream:    upstream.URL,
			DaemonToken: "token",
			Actor: func(*http.Request) (projectdaemon.ActorContext, error) {
				return projectdaemon.ActorContext{}, errors.New("actor failed")
			},
		})
		Expect(err).ToNot(HaveOccurred())
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/tree", nil))
		Expect(rec).To(HaveHTTPStatus(http.StatusUnauthorized))

		originalEncodeActorContext := encodeActorContext
		encodeActorContext = func(projectdaemon.ActorContext) (string, error) {
			return "", errors.New("encode failed")
		}
		ginkgo.DeferCleanup(func() {
			encodeActorContext = originalEncodeActorContext
		})

		handler, err = NewWorkspaceProxy(WorkspaceProxyOptions{
			Upstream:    upstream.URL,
			DaemonToken: "token",
			Actor:       func(*http.Request) (projectdaemon.ActorContext, error) { return validFrontdActor("home"), nil },
		})
		Expect(err).ToNot(HaveOccurred())
		rec = httptest.NewRecorder()
		handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/tree", nil))
		Expect(rec).To(HaveHTTPStatus(http.StatusInternalServerError))
		encodeActorContext = originalEncodeActorContext
	})

	ginkgo.It("maps workspace router defaults and proxy construction failures", func() {
		request := httptest.NewRequest(http.MethodGet, PublicWorkspacesPrefix+"/home/tree", nil)

		handler := NewWorkspaceRouterProxy(WorkspaceRouterProxyOptions{
			Resolve: func(*http.Request, workspaceid.WorkspaceID) (WorkspaceRoute, error) {
				return WorkspaceRoute{Upstream: "http://127.0.0.1:1", DaemonToken: "token"}, nil
			},
		})
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, request)
		Expect(rec).To(HaveHTTPStatus(http.StatusServiceUnavailable))

		handler = NewWorkspaceRouterProxy(WorkspaceRouterProxyOptions{
			Resolve: func(*http.Request, workspaceid.WorkspaceID) (WorkspaceRoute, error) {
				return WorkspaceRoute{Upstream: "http://127.0.0.1:1", DaemonToken: "token"}, nil
			},
			Actor: func(*http.Request, workspaceid.WorkspaceID) (projectdaemon.ActorContext, error) {
				return projectdaemon.ActorContext{}, errors.New("actor failed")
			},
		})
		rec = httptest.NewRecorder()
		handler.ServeHTTP(rec, request)
		Expect(rec).To(HaveHTTPStatus(http.StatusUnauthorized))

		originalEncodeActorContext := encodeActorContext
		encodeActorContext = func(projectdaemon.ActorContext) (string, error) {
			return "", errors.New("encode failed")
		}
		ginkgo.DeferCleanup(func() {
			encodeActorContext = originalEncodeActorContext
		})
		handler = NewWorkspaceRouterProxy(WorkspaceRouterProxyOptions{
			Resolve: func(*http.Request, workspaceid.WorkspaceID) (WorkspaceRoute, error) {
				return WorkspaceRoute{Upstream: "http://127.0.0.1:1", DaemonToken: "token"}, nil
			},
			Actor: func(*http.Request, workspaceid.WorkspaceID) (projectdaemon.ActorContext, error) {
				return validFrontdActor("home"), nil
			},
		})
		rec = httptest.NewRecorder()
		handler.ServeHTTP(rec, request)
		Expect(rec).To(HaveHTTPStatus(http.StatusInternalServerError))
		encodeActorContext = originalEncodeActorContext

		for _, route := range []WorkspaceRoute{
			{WorkspaceID: "home", Upstream: "", DaemonToken: "token"},
			{WorkspaceID: "home", Upstream: "://bad", DaemonToken: "token"},
		} {
			route := route
			handler = NewWorkspaceRouterProxy(WorkspaceRouterProxyOptions{
				Resolve: func(*http.Request, workspaceid.WorkspaceID) (WorkspaceRoute, error) {
					return route, nil
				},
				Actor: func(*http.Request, workspaceid.WorkspaceID) (projectdaemon.ActorContext, error) {
					return validFrontdActor("home"), nil
				},
			})
			rec = httptest.NewRecorder()
			handler.ServeHTTP(rec, request)
			Expect(rec).To(HaveHTTPStatus(http.StatusServiceUnavailable))
		}

		workspaceID, upstreamPath, ok := parseWorkspaceAPIPath(PublicWorkspacesPrefix + "/bad id/tree")
		Expect(workspaceID).To(BeEmpty())
		Expect(upstreamPath).To(BeEmpty())
		Expect(ok).To(BeFalse())
		workspaceID, upstreamPath, ok = parseWorkspaceAPIPath("/api/other/home/tree")
		Expect(workspaceID).To(BeEmpty())
		Expect(upstreamPath).To(BeEmpty())
		Expect(ok).To(BeFalse())
		workspaceID, upstreamPath, ok = parseWorkspaceAPIPath(PublicWorkspacesPrefix + "/home/assets/logo.png")
		Expect(workspaceID).To(Equal(workspaceid.WorkspaceID("home")))
		Expect(upstreamPath).To(Equal("/assets/logo.png"))
		Expect(ok).To(BeTrue())
	})

	ginkgo.It("routes workspace MCP requests and preserves session binding contracts", func() {
		handler := NewWorkspaceMCPHandler(WorkspaceMCPHandlerOptions{})
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/not-mcp", nil))
		Expect(rec).To(HaveHTTPStatus(http.StatusNotFound))

		handler = NewWorkspaceMCPHandler(WorkspaceMCPHandlerOptions{
			ResolveRoot: func(*http.Request) (workspaceid.WorkspaceID, error) {
				return "", ErrWorkspaceAmbiguous
			},
		})
		rec = httptest.NewRecorder()
		handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/mcp", nil))
		Expect(rec).To(HaveHTTPStatus(http.StatusConflict))

		bindings := NewMCPSessionBindings()
		handler = NewWorkspaceMCPHandler(WorkspaceMCPHandlerOptions{
			Sessions: bindings,
			Resolve: func(*http.Request, workspaceid.WorkspaceID) (WorkspaceRoute, error) {
				return WorkspaceRoute{Upstream: "http://127.0.0.1:1", DaemonToken: "token"}, nil
			},
			Proxy: func(WorkspaceRoute) http.Handler { return nil },
		})
		rec = httptest.NewRecorder()
		handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/mcp/workspaces/home", nil))
		Expect(rec).To(HaveHTTPStatus(http.StatusServiceUnavailable))

		handler = NewWorkspaceMCPHandler(WorkspaceMCPHandlerOptions{
			Sessions: bindings,
			Resolve: func(*http.Request, workspaceid.WorkspaceID) (WorkspaceRoute, error) {
				return WorkspaceRoute{Upstream: "http://127.0.0.1:1", DaemonToken: "token"}, nil
			},
			Proxy: func(WorkspaceRoute) http.Handler {
				return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
					w.Header().Set("Mcp-Session-Id", "server-session")
					w.WriteHeader(http.StatusInternalServerError)
				})
			},
		})
		req := httptest.NewRequest(http.MethodGet, "/mcp/workspaces/home", nil)
		rec = httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		Expect(rec).To(HaveHTTPStatus(http.StatusInternalServerError))
		_, ok := bindings.Workspace(MCPSessionIDFromHeader("server-session"))
		Expect(ok).To(BeFalse())

		Expect(bindings.Bind(MCPSessionIDFromHeader("server-session"), "other")).To(Succeed())
		handler = NewWorkspaceMCPHandler(WorkspaceMCPHandlerOptions{
			Sessions: bindings,
			Resolve: func(*http.Request, workspaceid.WorkspaceID) (WorkspaceRoute, error) {
				return WorkspaceRoute{Upstream: "http://127.0.0.1:1", DaemonToken: "token"}, nil
			},
			Proxy: func(WorkspaceRoute) http.Handler {
				return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
					w.Header().Set("Mcp-Session-Id", "server-session")
					w.WriteHeader(http.StatusNoContent)
				})
			},
		})
		rec = httptest.NewRecorder()
		handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/mcp/workspaces/home", nil))
		Expect(rec).To(HaveHTTPStatus(http.StatusNoContent))
		workspace, ok := bindings.Workspace(MCPSessionIDFromHeader("server-session"))
		Expect(ok).To(BeTrue())
		Expect(workspace).To(Equal(workspaceid.WorkspaceID("other")))
	})

	ginkgo.It("maps wikid single-workspace resolver responses into routing errors", func() {
		_, err := NewWikidSingleWorkspaceResolver("://bad", "token")
		Expect(err).To(MatchError(errInvalidWikidUpstream))
		_, err = NewWikidSingleWorkspaceResolver("http://127.0.0.1:1", " ")
		Expect(err).To(MatchError(errDaemonTokenRequired))

		originalNewRequest := newFrontdRequestWithContext
		requestErr := errors.New("frontd request failed")
		newFrontdRequestWithContext = func(context.Context, string, string, io.Reader) (*http.Request, error) {
			return nil, requestErr
		}
		ginkgo.DeferCleanup(func() {
			newFrontdRequestWithContext = originalNewRequest
		})
		resolver, err := NewWikidSingleWorkspaceResolver("http://127.0.0.1:1", "token")
		Expect(err).ToNot(HaveOccurred())
		_, err = resolver(nil)
		Expect(err).To(MatchError(requestErr))
		newFrontdRequestWithContext = originalNewRequest

		cases := []struct {
			name   string
			status int
			body   string
			want   types.GomegaMatcher
		}{
			{name: "forbidden", status: http.StatusForbidden, want: MatchError(ErrWorkspaceForbidden)},
			{name: "empty server error", status: http.StatusInternalServerError, want: MatchError(ErrWorkspaceListFailed)},
			{name: "bad json", status: http.StatusOK, body: "{", want: matchFrontdJSONDecodeError()},
			{name: "empty list", status: http.StatusOK, body: `{"workspaces":[]}`, want: MatchError(ErrWorkspaceForbidden)},
			{name: "invalid workspace", status: http.StatusOK, body: `{"workspaces":[{"id":"bad id"}]}`, want: MatchError(ErrWorkspaceNotFound)},
			{name: "ambiguous", status: http.StatusOK, body: `{"workspaces":[{"id":"one"},{"id":"two"}]}`, want: MatchError(ErrWorkspaceAmbiguous)},
		}

		for _, tt := range cases {
			tt := tt
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				w.WriteHeader(tt.status)
				_, _ = w.Write([]byte(tt.body))
			}))
			ginkgo.DeferCleanup(server.Close)
			resolver, err := NewWikidSingleWorkspaceResolver(server.URL, "token")
			Expect(err).ToNot(HaveOccurred())
			_, err = resolver(httptest.NewRequest(http.MethodGet, "/mcp", nil))
			Expect(err).To(tt.want, tt.name)
		}

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {}))
		resolver, err = NewWikidSingleWorkspaceResolver(server.URL, "token")
		Expect(err).ToNot(HaveOccurred())
		server.Close()
		_, err = resolver(nil)
		Expect(err).To(HaveOccurred())
	})

	ginkgo.It("maps wikid workspace resolver responses into workspace routes and errors", func() {
		_, err := NewWikidWorkspaceResolver("://bad", "token")
		Expect(err).To(MatchError(errInvalidWikidUpstream))
		_, err = NewWikidWorkspaceResolver("http://127.0.0.1:1", " ")
		Expect(err).To(MatchError(errDaemonTokenRequired))

		resolver, err := NewWikidWorkspaceResolver("http://127.0.0.1:1", "token")
		Expect(err).ToNot(HaveOccurred())
		_, err = resolver(nil, "bad id")
		Expect(err).To(MatchError(ErrWorkspaceNotFound))

		originalNewRequest := newFrontdRequestWithContext
		requestErr := errors.New("frontd request failed")
		newFrontdRequestWithContext = func(context.Context, string, string, io.Reader) (*http.Request, error) {
			return nil, requestErr
		}
		ginkgo.DeferCleanup(func() {
			newFrontdRequestWithContext = originalNewRequest
		})
		_, err = resolver(nil, "home")
		Expect(err).To(MatchError(requestErr))
		newFrontdRequestWithContext = originalNewRequest

		cases := []struct {
			name   string
			status int
			body   string
			want   types.GomegaMatcher
		}{
			{name: "not found", status: http.StatusNotFound, want: MatchError(ErrWorkspaceNotFound)},
			{name: "forbidden", status: http.StatusForbidden, want: MatchError(ErrWorkspaceForbidden)},
			{name: "empty server error", status: http.StatusInternalServerError, want: MatchError(ErrWorkspaceEnsureFailed)},
			{name: "bad json", status: http.StatusOK, body: "{", want: matchFrontdJSONDecodeError()},
			{name: "bad status workspace id", status: http.StatusOK, body: `{"status":{"workspaceId":"bad id","state":"running","url":"http://workspaced"}}`, want: matchFrontdWorkspaceIDError(workspaceid.ErrCodeWorkspaceIDInvalid)},
			{name: "bad workspace id fallback", status: http.StatusOK, body: `{"workspace":{"id":"bad id"},"status":{"state":"running","url":"http://workspaced"}}`, want: matchFrontdWorkspaceIDError(workspaceid.ErrCodeWorkspaceIDInvalid)},
			{name: "not running", status: http.StatusOK, body: `{"status":{"state":"stopped","url":"http://workspaced"}}`, want: MatchError(ErrWorkspaceNotRunning)},
		}

		for _, tt := range cases {
			tt := tt
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				w.WriteHeader(tt.status)
				_, _ = w.Write([]byte(tt.body))
			}))
			ginkgo.DeferCleanup(server.Close)
			resolver, err := NewWikidWorkspaceResolver(server.URL, "token")
			Expect(err).ToNot(HaveOccurred())
			_, err = resolver(httptest.NewRequest(http.MethodGet, "/workspace", nil), "home")
			Expect(err).To(tt.want, tt.name)
		}

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			_, _ = w.Write([]byte(`{"status":{"state":"running","url":"http://workspaced/"}}`))
		}))
		ginkgo.DeferCleanup(server.Close)
		resolver, err = NewWikidWorkspaceResolver(server.URL, "token")
		Expect(err).ToNot(HaveOccurred())
		route, err := resolver(nil, "home")
		Expect(err).ToNot(HaveOccurred())
		Expect(route).To(gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
			"WorkspaceID": Equal(workspaceid.WorkspaceID("home")),
			"Upstream":    Equal("http://workspaced"),
		}))

		server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			_, _ = w.Write([]byte(`{"workspace":{"id":"body-id"},"status":{"state":"running","url":"http://workspaced"}}`))
		}))
		ginkgo.DeferCleanup(server.Close)
		resolver, err = NewWikidWorkspaceResolver(server.URL, "token")
		Expect(err).ToNot(HaveOccurred())
		route, err = resolver(nil, "home")
		Expect(err).ToNot(HaveOccurred())
		Expect(route).To(gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
			"WorkspaceID": Equal(workspaceid.WorkspaceID("body-id")),
		}))

		server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {}))
		resolver, err = NewWikidWorkspaceResolver(server.URL, "token")
		Expect(err).ToNot(HaveOccurred())
		server.Close()
		_, err = resolver(nil, "home")
		Expect(err).To(HaveOccurred())
	})

	ginkgo.It("preserves original request headers while ignoring nil and blank inputs", func() {
		preserveOriginalRequestHeaders(nil, httptest.NewRequest(http.MethodGet, "/x", nil))
		target := httptest.NewRequest(http.MethodPost, "/target", nil)
		preserveOriginalRequestHeaders(target, nil)
		Expect(target.Header).To(BeEmpty())

		source := &http.Request{Method: http.MethodPatch, Header: http.Header{}, RemoteAddr: "127.0.0.1:1234"}
		preserveOriginalRequestHeaders(target, source)
		Expect(target.Header).To(HaveKeyWithValue(http.CanonicalHeaderKey("X-LeafWiki-Original-Method"), ConsistOf(http.MethodPatch)))
		Expect(target.Header).NotTo(HaveKey(http.CanonicalHeaderKey("X-LeafWiki-Original-Path")))
		Expect(target.Header).To(HaveKeyWithValue(http.CanonicalHeaderKey("X-LeafWiki-Original-Remote-Addr"), ConsistOf("127.0.0.1:1234")))

		setOriginalRequestHeaders(nil, http.MethodGet, "/x", "remote")
		setOriginalRequestHeaders(target, " ", " ", " ")
		Expect(target.Header).NotTo(HaveKey(http.CanonicalHeaderKey("X-LeafWiki-Original-Method")))
		Expect(target.Header).NotTo(HaveKey(http.CanonicalHeaderKey("X-LeafWiki-Original-Path")))
		Expect(target.Header).NotTo(HaveKey(http.CanonicalHeaderKey("X-LeafWiki-Original-Remote-Addr")))
	})
})

func matchFrontdJSONDecodeError() types.GomegaMatcher {
	ginkgo.GinkgoHelper()
	return Satisfy(func(err error) bool {
		var syntaxErr *json.SyntaxError
		return errors.As(err, &syntaxErr) || errors.Is(err, io.ErrUnexpectedEOF)
	})
}

func matchFrontdWorkspaceIDError(code sharederrors.ErrorCode) types.GomegaMatcher {
	ginkgo.GinkgoHelper()
	return WithTransform(workspaceid.WorkspaceIDErrorCode, Equal(code))
}
