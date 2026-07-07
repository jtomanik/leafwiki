package frontd

import (
	"errors"
	"net/http"
	"net/http/httptest"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	sharederrors "github.com/perber/wiki/internal/core/shared/errors"
	"github.com/perber/wiki/internal/workspaceid"
)

type rootMCPWorkspaceResolutionCase struct {
	rootID     workspaceid.WorkspaceID
	rootErr    error
	wantStatus int
	wantSeenID workspaceid.WorkspaceID
}

type workspaceMCPErrorResponseCase struct {
	err    error
	status int
	code   sharederrors.ErrorCode
}

var _ = Describe("workspace MCP session routing", func() {
	It("rejects rebinding a session to a different workspace", Label("unit"), func() {
		bindings := NewMCPSessionBindings()

		Expect(bindings.Bind(MCPSessionIDFromHeader("session-1"), mustDecodeWorkspaceID("alpha"))).To(Succeed())
		Expect(bindings.Bind(MCPSessionIDFromHeader("session-1"), mustDecodeWorkspaceID("alpha"))).To(Succeed())
		Expect(bindings.Bind(MCPSessionIDFromHeader("session-1"), mustDecodeWorkspaceID("beta"))).To(MatchError(ErrMCPSessionWorkspaceMismatch))
	})

	It("rejects invalid workspace identifiers before binding", Label("unit"), func() {
		bindings := NewMCPSessionBindings()

		Expect(bindings.Bind(MCPSessionIDFromHeader("session-1"), mustDecodeWorkspaceID(" alpha "))).To(matchFrontdWorkspaceIDError(workspaceid.ErrCodeWorkspaceIDWhitespace))
		Expect(bindings).NotTo(HaveMCPSession(MCPSessionIDFromHeader("session-1")))
	})

	It("records explicit proxy response status before body writes", Label("unit"), func() {
		rec := httptest.NewRecorder()
		writer := &mcpSessionResponseWriter{ResponseWriter: rec}

		writer.WriteHeader(http.StatusCreated)
		_, err := writer.Write([]byte("body"))

		Expect(err).To(Succeed())
		Expect(writer.statusCode).To(Equal(http.StatusCreated))
		Expect(rec).To(HaveHTTPStatus(http.StatusCreated))
	})

	DescribeTable("workspace MCP dependency error responses", Label("unit"),
		func(tc workspaceMCPErrorResponseCase) {
			rec := httptest.NewRecorder()

			writeWorkspaceMCPError(rec, tc.err)

			Expect(rec).To(matchStructuredFrontdError(tc.status, tc.code, sharederrors.MessageIDForCode(tc.code)))
		},
		Entry("reports missing workspaces", workspaceMCPErrorResponseCase{
			err:    ErrWorkspaceNotFound,
			status: http.StatusNotFound,
			code:   errCodeWorkspaceNotFound,
		}),
		Entry("reports forbidden workspaces", workspaceMCPErrorResponseCase{
			err:    ErrWorkspaceForbidden,
			status: http.StatusForbidden,
			code:   errCodeWorkspaceForbidden,
		}),
		Entry("reports ambiguous root workspace selection", workspaceMCPErrorResponseCase{
			err:    ErrWorkspaceAmbiguous,
			status: http.StatusConflict,
			code:   errCodeWorkspaceAmbiguous,
		}),
		Entry("reports resolver dependency failures", workspaceMCPErrorResponseCase{
			err:    ErrWorkspaceListFailed,
			status: http.StatusServiceUnavailable,
			code:   errCodeWorkspaceUnavailable,
		}),
	)

	It("routes explicit workspace MCP requests and binds the client session", Label("integration"), func() {
		bindings := NewMCPSessionBindings()
		var seenPath string
		handler := NewWorkspaceMCPHandler(WorkspaceMCPHandlerOptions{
			Sessions: bindings,
			Resolve: func(_ *http.Request, workspaceID workspaceid.WorkspaceID) (WorkspaceRoute, error) {
				return WorkspaceRoute{WorkspaceID: workspaceID, Upstream: "http://127.0.0.1:1", DaemonToken: "token"}, nil
			},
			Proxy: func(route WorkspaceRoute) http.Handler {
				return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
					seenPath = req.URL.Path
					w.WriteHeader(http.StatusAccepted)
				})
			},
		})

		req := httptest.NewRequest(http.MethodPost, "/mcp/workspaces/home", nil)
		req.Header.Set("Mcp-Session-Id", "session-1")
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		Expect(rec).To(HaveHTTPStatus(http.StatusAccepted))
		Expect(seenPath).To(Equal("/mcp"))
		Expect(bindings).To(HaveMCPSessionBinding(MCPSessionIDFromHeader("session-1"), mustDecodeWorkspaceID("home")))

		req = httptest.NewRequest(http.MethodPost, "/mcp/workspaces/alpha", nil)
		req.Header.Set("Mcp-Session-Id", "session-1")
		rec = httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		Expect(rec).To(matchStructuredFrontdError(
			http.StatusConflict,
			errCodeMCPSessionWorkspaceMismatch,
			sharederrors.MessageIDForCode(errCodeMCPSessionWorkspaceMismatch),
		))
	})

	It("binds a server-issued session only after a successful proxy response", Label("integration"), func() {
		bindings := NewMCPSessionBindings()
		handler := NewWorkspaceMCPHandler(WorkspaceMCPHandlerOptions{
			Sessions: bindings,
			Resolve: func(*http.Request, workspaceid.WorkspaceID) (WorkspaceRoute, error) {
				return WorkspaceRoute{WorkspaceID: mustDecodeWorkspaceID("alpha"), Upstream: "http://127.0.0.1:1", DaemonToken: "token"}, nil
			},
			Proxy: func(route WorkspaceRoute) http.Handler {
				return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
					Expect(req.Header.Get("Mcp-Session-Id")).To(BeEmpty())
					w.Header().Set("Mcp-Session-Id", "server-session-1")
					w.WriteHeader(http.StatusAccepted)
				})
			},
		})

		req := httptest.NewRequest(http.MethodPost, "/mcp/workspaces/alpha", nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		Expect(rec).To(HaveHTTPStatus(http.StatusAccepted))
		Expect(bindings).To(HaveMCPSessionBinding(MCPSessionIDFromHeader("server-session-1"), mustDecodeWorkspaceID("alpha")))
	})

	It("binds server sessions after streaming MCP response bodies", Label("integration"), func() {
		bindings := NewMCPSessionBindings()
		handler := NewWorkspaceMCPHandler(WorkspaceMCPHandlerOptions{
			Sessions: bindings,
			Resolve: func(*http.Request, workspaceid.WorkspaceID) (WorkspaceRoute, error) {
				return WorkspaceRoute{WorkspaceID: mustDecodeWorkspaceID("alpha"), Upstream: "http://127.0.0.1:1", DaemonToken: "token"}, nil
			},
			Proxy: func(route WorkspaceRoute) http.Handler {
				return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
					Expect(http.NewResponseController(w).Flush()).To(Succeed())
					w.Header().Set("Mcp-Session-Id", "server-session-stream")
					_, err := w.Write([]byte(`{"jsonrpc":"2.0","result":{}}`))
					Expect(err).To(Succeed())
				})
			},
		})

		req := httptest.NewRequest(http.MethodPost, "/mcp/workspaces/alpha", nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		Expect(rec).To(HaveHTTPStatus(http.StatusOK))
		Expect(rec).To(HaveHTTPBody(MatchJSON(`{"jsonrpc":"2.0","result":{}}`)))
		Expect(bindings).To(HaveMCPSessionBinding(MCPSessionIDFromHeader("server-session-stream"), mustDecodeWorkspaceID("alpha")))
	})

	It("routes root MCP through an existing session binding", Label("integration"), func() {
		bindings := NewMCPSessionBindings()
		Expect(bindings.Bind(MCPSessionIDFromHeader("session-1"), mustDecodeWorkspaceID("alpha"))).To(Succeed())
		var seenID workspaceid.WorkspaceID
		handler := NewWorkspaceMCPHandler(WorkspaceMCPHandlerOptions{
			Sessions: bindings,
			ResolveRoot: func(*http.Request) (workspaceid.WorkspaceID, error) {
				return "unexpected-root", nil
			},
			Resolve: func(_ *http.Request, workspaceID workspaceid.WorkspaceID) (WorkspaceRoute, error) {
				seenID = workspaceID
				return WorkspaceRoute{WorkspaceID: workspaceID, Upstream: "http://127.0.0.1:1", DaemonToken: "token"}, nil
			},
			Proxy: func(route WorkspaceRoute) http.Handler {
				return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
					w.WriteHeader(http.StatusAccepted)
				})
			},
		})

		req := httptest.NewRequest(http.MethodPost, "/mcp", nil)
		req.Header.Set("Mcp-Session-Id", "session-1")
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		Expect(rec).To(HaveHTTPStatus(http.StatusAccepted))
		Expect(seenID).To(Equal(mustDecodeWorkspaceID("alpha")))
	})

	It("clears a session binding after a successful DELETE", Label("integration"), func() {
		bindings := NewMCPSessionBindings()
		Expect(bindings.Bind(MCPSessionIDFromHeader("session-1"), mustDecodeWorkspaceID("alpha"))).To(Succeed())
		handler := NewWorkspaceMCPHandler(WorkspaceMCPHandlerOptions{
			Sessions: bindings,
			ResolveRoot: func(*http.Request) (workspaceid.WorkspaceID, error) {
				return "", ErrWorkspaceAmbiguous
			},
			Resolve: func(_ *http.Request, workspaceID workspaceid.WorkspaceID) (WorkspaceRoute, error) {
				return WorkspaceRoute{WorkspaceID: workspaceID, Upstream: "http://127.0.0.1:1", DaemonToken: "token"}, nil
			},
			Proxy: func(route WorkspaceRoute) http.Handler {
				return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
					w.WriteHeader(http.StatusOK)
				})
			},
		})

		req := httptest.NewRequest(http.MethodDelete, "/mcp", nil)
		req.Header.Set("Mcp-Session-Id", "session-1")
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		Expect(rec).To(HaveHTTPStatus(http.StatusOK))
		Expect(bindings).NotTo(HaveMCPSession(MCPSessionIDFromHeader("session-1")))

		req = httptest.NewRequest(http.MethodPost, "/mcp", nil)
		req.Header.Set("Mcp-Session-Id", "session-1")
		rec = httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		Expect(rec).To(HaveHTTPStatus(http.StatusConflict))
	})

	It("authorizes explicit workspace requests before binding the session", Label("integration"), func() {
		bindings := NewMCPSessionBindings()
		handler := NewWorkspaceMCPHandler(WorkspaceMCPHandlerOptions{
			Sessions: bindings,
			Resolve: func(*http.Request, workspaceid.WorkspaceID) (WorkspaceRoute, error) {
				return WorkspaceRoute{}, ErrWorkspaceForbidden
			},
			Proxy: func(WorkspaceRoute) http.Handler {
				return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
					w.WriteHeader(http.StatusAccepted)
				})
			},
		})

		req := httptest.NewRequest(http.MethodPost, "/mcp/workspaces/alpha", nil)
		req.Header.Set("Mcp-Session-Id", "forbidden-session")
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		Expect(rec).To(HaveHTTPStatus(http.StatusForbidden))
		Expect(bindings).NotTo(HaveMCPSession(MCPSessionIDFromHeader("forbidden-session")))
	})

	DescribeTable("root MCP workspace resolution", Label("integration"),
		func(tc rootMCPWorkspaceResolutionCase) {
			var seenID workspaceid.WorkspaceID
			handler := NewWorkspaceMCPHandler(WorkspaceMCPHandlerOptions{
				ResolveRoot: func(*http.Request) (workspaceid.WorkspaceID, error) {
					return tc.rootID, tc.rootErr
				},
				Resolve: func(_ *http.Request, workspaceID workspaceid.WorkspaceID) (WorkspaceRoute, error) {
					seenID = workspaceID
					return WorkspaceRoute{WorkspaceID: workspaceID, Upstream: "http://127.0.0.1:1", DaemonToken: "token"}, nil
				},
				Proxy: func(route WorkspaceRoute) http.Handler {
					return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
						w.WriteHeader(http.StatusAccepted)
					})
				},
			})

			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/mcp", nil))

			if tc.wantStatus >= http.StatusBadRequest {
				want := map[int]struct {
					code      sharederrors.ErrorCode
					messageID sharederrors.MessageID
				}{
					http.StatusForbidden:          {code: errCodeWorkspaceForbidden, messageID: sharederrors.MessageIDForCode(errCodeWorkspaceForbidden)},
					http.StatusConflict:           {code: errCodeWorkspaceAmbiguous, messageID: sharederrors.MessageIDForCode(errCodeWorkspaceAmbiguous)},
					http.StatusNotFound:           {code: errCodeWorkspaceNotFound, messageID: sharederrors.MessageIDForCode(errCodeWorkspaceNotFound)},
					http.StatusServiceUnavailable: {code: errCodeWorkspaceUnavailable, messageID: sharederrors.MessageIDForCode(errCodeWorkspaceUnavailable)},
				}[tc.wantStatus]
				Expect(rec).To(matchStructuredFrontdError(tc.wantStatus, want.code, want.messageID))
			} else {
				Expect(rec).To(HaveHTTPStatus(tc.wantStatus))
			}
			Expect(seenID).To(Equal(tc.wantSeenID))
		},
		Entry("routes the only available workspace", rootMCPWorkspaceResolutionCase{
			rootID:     mustDecodeWorkspaceID("only"),
			wantStatus: http.StatusAccepted,
			wantSeenID: mustDecodeWorkspaceID("only"),
		}),
		Entry("rejects an empty workspace list", rootMCPWorkspaceResolutionCase{
			rootErr:    ErrWorkspaceForbidden,
			wantStatus: http.StatusForbidden,
		}),
		Entry("rejects an ambiguous workspace list", rootMCPWorkspaceResolutionCase{
			rootErr:    ErrWorkspaceAmbiguous,
			wantStatus: http.StatusConflict,
		}),
		Entry("rejects a missing workspace", rootMCPWorkspaceResolutionCase{
			rootErr:    ErrWorkspaceNotFound,
			wantStatus: http.StatusNotFound,
		}),
		Entry("reports workspace resolution dependency failures", rootMCPWorkspaceResolutionCase{
			rootErr:    errors.New("boom"),
			wantStatus: http.StatusServiceUnavailable,
		}),
	)

	It("reports structured dependency errors when root MCP cannot proxy", Label("integration"), func() {
		handler := NewWorkspaceMCPHandler(WorkspaceMCPHandlerOptions{
			ResolveRoot: func(*http.Request) (workspaceid.WorkspaceID, error) { return "home", nil },
		})
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/mcp", nil))

		Expect(rec).To(matchStructuredFrontdError(
			http.StatusServiceUnavailable,
			errCodeMCPWorkspaceRouterUnavailable,
			sharederrors.MessageIDForCode(errCodeMCPWorkspaceRouterUnavailable),
		))
	})
})
