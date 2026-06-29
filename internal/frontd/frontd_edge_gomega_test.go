package frontd

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"time"

	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

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

var _ = ginkgo.Describe("frontd edge coverage", func() {
	ginkgo.It("validates proxy constructors and small path helpers", func() {
		_, err := NewControlPlaneProxy("://bad", "token")
		Expect(err).To(MatchError(ContainSubstring("invalid wikid upstream")))
		_, err = NewControlPlaneProxy("http://127.0.0.1:1", " ")
		Expect(err).To(MatchError("daemon token is required"))

		_, err = NewWorkspaceProxy(WorkspaceProxyOptions{Upstream: "://bad", DaemonToken: "token", Actor: func(*http.Request) (projectdaemon.ActorContext, error) {
			return validFrontdActor("home"), nil
		}})
		Expect(err).To(MatchError(ContainSubstring("invalid workspaced upstream")))
		_, err = NewWorkspaceProxy(WorkspaceProxyOptions{Upstream: "http://127.0.0.1:1", DaemonToken: " ", Actor: func(*http.Request) (projectdaemon.ActorContext, error) {
			return validFrontdActor("home"), nil
		}})
		Expect(err).To(MatchError("daemon token is required"))
		_, err = NewWorkspaceProxy(WorkspaceProxyOptions{Upstream: "http://127.0.0.1:1", DaemonToken: "token"})
		Expect(err).To(MatchError("actor context resolver is required"))

		_, err = NewWorkspacesAPI("://bad", "token")
		Expect(err).To(MatchError(ContainSubstring("invalid wikid upstream")))
		_, err = NewWorkspacesAPI("http://127.0.0.1:1", " ")
		Expect(err).To(MatchError("daemon token is required"))

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
		Expect(rec.Code).To(Equal(http.StatusUnauthorized))

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
		Expect(rec.Code).To(Equal(http.StatusInternalServerError))
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
		Expect(rec.Code).To(Equal(http.StatusServiceUnavailable))

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
		Expect(rec.Code).To(Equal(http.StatusUnauthorized))

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
		Expect(rec.Code).To(Equal(http.StatusInternalServerError))
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
			Expect(rec.Code).To(Equal(http.StatusServiceUnavailable))
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

	ginkgo.It("covers workspace MCP handler routing and binding edge cases", func() {
		handler := NewWorkspaceMCPHandler(WorkspaceMCPHandlerOptions{})
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/not-mcp", nil))
		Expect(rec.Code).To(Equal(http.StatusNotFound))

		handler = NewWorkspaceMCPHandler(WorkspaceMCPHandlerOptions{
			ResolveRoot: func(*http.Request) (workspaceid.WorkspaceID, error) {
				return "", ErrWorkspaceAmbiguous
			},
		})
		rec = httptest.NewRecorder()
		handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/mcp", nil))
		Expect(rec.Code).To(Equal(http.StatusConflict))

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
		Expect(rec.Code).To(Equal(http.StatusServiceUnavailable))

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
		Expect(rec.Code).To(Equal(http.StatusInternalServerError))
		_, ok := bindings.Workspace("server-session")
		Expect(ok).To(BeFalse())

		Expect(bindings.Bind("server-session", "other")).To(Succeed())
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
		Expect(rec.Code).To(Equal(http.StatusNoContent))
		workspace, ok := bindings.Workspace("server-session")
		Expect(ok).To(BeTrue())
		Expect(workspace).To(Equal(workspaceid.WorkspaceID("other")))
	})

	ginkgo.It("covers wikid single-workspace resolver response mapping", func() {
		_, err := NewWikidSingleWorkspaceResolver("://bad", "token")
		Expect(err).To(MatchError(ContainSubstring("invalid wikid upstream")))
		_, err = NewWikidSingleWorkspaceResolver("http://127.0.0.1:1", " ")
		Expect(err).To(MatchError("daemon token is required"))

		originalNewRequest := newFrontdRequestWithContext
		newFrontdRequestWithContext = func(context.Context, string, string, io.Reader) (*http.Request, error) {
			return nil, errors.New("request failed")
		}
		ginkgo.DeferCleanup(func() {
			newFrontdRequestWithContext = originalNewRequest
		})
		resolver, err := NewWikidSingleWorkspaceResolver("http://127.0.0.1:1", "token")
		Expect(err).ToNot(HaveOccurred())
		_, err = resolver(nil)
		Expect(err).To(MatchError("request failed"))
		newFrontdRequestWithContext = originalNewRequest

		cases := []struct {
			name    string
			status  int
			body    string
			wantErr string
		}{
			{name: "forbidden", status: http.StatusForbidden, wantErr: ErrWorkspaceForbidden.Error()},
			{name: "empty server error", status: http.StatusInternalServerError, wantErr: "500 Internal Server Error"},
			{name: "bad json", status: http.StatusOK, body: "{", wantErr: "decode workspace list response"},
			{name: "empty list", status: http.StatusOK, body: `{"workspaces":[]}`, wantErr: ErrWorkspaceForbidden.Error()},
			{name: "invalid workspace", status: http.StatusOK, body: `{"workspaces":[{"id":"bad id"}]}`, wantErr: ErrWorkspaceNotFound.Error()},
			{name: "ambiguous", status: http.StatusOK, body: `{"workspaces":[{"id":"one"},{"id":"two"}]}`, wantErr: ErrWorkspaceAmbiguous.Error()},
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
			Expect(err).To(MatchError(ContainSubstring(tt.wantErr)), tt.name)
		}

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {}))
		resolver, err = NewWikidSingleWorkspaceResolver(server.URL, "token")
		Expect(err).ToNot(HaveOccurred())
		server.Close()
		_, err = resolver(nil)
		Expect(err).To(HaveOccurred())
	})

	ginkgo.It("covers wikid workspace resolver response mapping", func() {
		_, err := NewWikidWorkspaceResolver("://bad", "token")
		Expect(err).To(MatchError(ContainSubstring("invalid wikid upstream")))
		_, err = NewWikidWorkspaceResolver("http://127.0.0.1:1", " ")
		Expect(err).To(MatchError("daemon token is required"))

		resolver, err := NewWikidWorkspaceResolver("http://127.0.0.1:1", "token")
		Expect(err).ToNot(HaveOccurred())
		_, err = resolver(nil, "bad id")
		Expect(err).To(MatchError(ErrWorkspaceNotFound))

		originalNewRequest := newFrontdRequestWithContext
		newFrontdRequestWithContext = func(context.Context, string, string, io.Reader) (*http.Request, error) {
			return nil, errors.New("request failed")
		}
		ginkgo.DeferCleanup(func() {
			newFrontdRequestWithContext = originalNewRequest
		})
		_, err = resolver(nil, "home")
		Expect(err).To(MatchError("request failed"))
		newFrontdRequestWithContext = originalNewRequest

		cases := []struct {
			name    string
			status  int
			body    string
			wantErr string
		}{
			{name: "not found", status: http.StatusNotFound, wantErr: ErrWorkspaceNotFound.Error()},
			{name: "forbidden", status: http.StatusForbidden, wantErr: ErrWorkspaceForbidden.Error()},
			{name: "empty server error", status: http.StatusInternalServerError, wantErr: "500 Internal Server Error"},
			{name: "bad json", status: http.StatusOK, body: "{", wantErr: "decode workspace ensure response"},
			{name: "bad status workspace id", status: http.StatusOK, body: `{"status":{"workspaceId":"bad id","state":"running","url":"http://workspaced"}}`, wantErr: "decode workspace ensure response ID"},
			{name: "bad workspace id fallback", status: http.StatusOK, body: `{"workspace":{"id":"bad id"},"status":{"state":"running","url":"http://workspaced"}}`, wantErr: "decode workspace ensure response ID"},
			{name: "not running", status: http.StatusOK, body: `{"status":{"state":"stopped","url":"http://workspaced"}}`, wantErr: "is not running"},
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
			Expect(err).To(MatchError(ContainSubstring(tt.wantErr)), tt.name)
		}

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			_, _ = w.Write([]byte(`{"status":{"state":"running","url":"http://workspaced/"}}`))
		}))
		ginkgo.DeferCleanup(server.Close)
		resolver, err = NewWikidWorkspaceResolver(server.URL, "token")
		Expect(err).ToNot(HaveOccurred())
		route, err := resolver(nil, "home")
		Expect(err).ToNot(HaveOccurred())
		Expect(route.WorkspaceID).To(Equal(workspaceid.WorkspaceID("home")))
		Expect(route.Upstream).To(Equal("http://workspaced"))

		server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			_, _ = w.Write([]byte(`{"workspace":{"id":"body-id"},"status":{"state":"running","url":"http://workspaced"}}`))
		}))
		ginkgo.DeferCleanup(server.Close)
		resolver, err = NewWikidWorkspaceResolver(server.URL, "token")
		Expect(err).ToNot(HaveOccurred())
		route, err = resolver(nil, "home")
		Expect(err).ToNot(HaveOccurred())
		Expect(route.WorkspaceID).To(Equal(workspaceid.WorkspaceID("body-id")))

		server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {}))
		resolver, err = NewWikidWorkspaceResolver(server.URL, "token")
		Expect(err).ToNot(HaveOccurred())
		server.Close()
		_, err = resolver(nil, "home")
		Expect(err).To(HaveOccurred())
	})

	ginkgo.It("covers original request header helpers with nil and blank inputs", func() {
		preserveOriginalRequestHeaders(nil, httptest.NewRequest(http.MethodGet, "/x", nil))
		target := httptest.NewRequest(http.MethodPost, "/target", nil)
		preserveOriginalRequestHeaders(target, nil)
		Expect(target.Header).To(BeEmpty())

		source := &http.Request{Method: http.MethodPatch, Header: http.Header{}, RemoteAddr: "127.0.0.1:1234"}
		preserveOriginalRequestHeaders(target, source)
		Expect(target.Header.Get("X-LeafWiki-Original-Method")).To(Equal(http.MethodPatch))
		Expect(target.Header.Get("X-LeafWiki-Original-Path")).To(BeEmpty())
		Expect(target.Header.Get("X-LeafWiki-Original-Remote-Addr")).To(Equal("127.0.0.1:1234"))

		setOriginalRequestHeaders(nil, http.MethodGet, "/x", "remote")
		setOriginalRequestHeaders(target, " ", " ", " ")
		Expect(target.Header.Get("X-LeafWiki-Original-Method")).To(BeEmpty())
		Expect(target.Header.Get("X-LeafWiki-Original-Path")).To(BeEmpty())
		Expect(target.Header.Get("X-LeafWiki-Original-Remote-Addr")).To(BeEmpty())
	})
})
