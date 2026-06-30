package frontd

import (
	"encoding/json"
	"errors"
	. "github.com/onsi/ginkgo/v2"
	"net/http"
	"net/http/httptest"

	sharederrors "github.com/perber/wiki/internal/core/shared/errors"
	"github.com/perber/wiki/internal/workspaceid"
)

var _ = It("TestMCPSessionBindingsRejectWorkspaceMismatch", func() {
	t := GinkgoT()
	bindings := NewMCPSessionBindings()

	if err := bindings.Bind(MCPSessionIDFromHeader("session-1"), workspaceid.WorkspaceID("alpha")); err != nil {
		t.Fatalf("Bind alpha failed: %v", err)
	}
	if err := bindings.Bind(MCPSessionIDFromHeader("session-1"), workspaceid.WorkspaceID("alpha")); err != nil {
		t.Fatalf("Bind same workspace failed: %v", err)
	}
	if err := bindings.Bind(MCPSessionIDFromHeader("session-1"), workspaceid.WorkspaceID("beta")); err == nil {
		t.Fatalf("expected workspace mismatch")
	}

})

var _ = It("TestMCPSessionBindingsRejectInvalidWorkspaceID", func() {
	t := GinkgoT()
	bindings := NewMCPSessionBindings()

	if err := bindings.Bind(MCPSessionIDFromHeader("session-1"), workspaceid.WorkspaceID(" alpha ")); err == nil {
		t.Fatal("Bind accepted a workspace ID that should have been parsed at the edge")
	}
	if _, ok := bindings.Workspace(MCPSessionIDFromHeader("session-1")); ok {
		t.Fatal("invalid workspace ID was bound")
	}

})

var _ = It("TestWorkspaceMCPHandlerRoutesExplicitWorkspaceAndBindsSession", func() {
	t := GinkgoT()
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

	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
	}
	if seenPath != "/mcp" {
		t.Fatalf("proxied path = %q, want /mcp", seenPath)
	}
	if workspace, ok := bindings.Workspace(MCPSessionIDFromHeader("session-1")); !ok || workspace != workspaceid.WorkspaceID("home") {
		t.Fatalf("session binding = %q/%v, want home/true", workspace, ok)
	}

	req = httptest.NewRequest(http.MethodPost, "/mcp/workspaces/alpha", nil)
	req.Header.Set("Mcp-Session-Id", "session-1")
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusConflict {
		t.Fatalf("mismatch status = %d, want 409: %s", rec.Code, rec.Body.String())
	}
	assertStructuredMCPWorkspaceError(t, rec, errCodeMCPSessionWorkspaceMismatch, sharederrors.MessageIDForCode(errCodeMCPSessionWorkspaceMismatch))

})

var _ = It("TestWorkspaceMCPHandlerBindsServerIssuedSessionAfterProxy", func() {
	t := GinkgoT()
	bindings := NewMCPSessionBindings()
	handler := NewWorkspaceMCPHandler(WorkspaceMCPHandlerOptions{
		Sessions: bindings,
		Resolve: func(*http.Request, workspaceid.WorkspaceID) (WorkspaceRoute, error) {
			return WorkspaceRoute{WorkspaceID: workspaceid.WorkspaceID("alpha"), Upstream: "http://127.0.0.1:1", DaemonToken: "token"}, nil
		},
		Proxy: func(route WorkspaceRoute) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				if req.Header.Get("Mcp-Session-Id") != "" {
					t.Fatalf("request unexpectedly had client session header")
				}
				w.Header().Set("Mcp-Session-Id", "server-session-1")
				w.WriteHeader(http.StatusAccepted)
			})
		},
	})

	req := httptest.NewRequest(http.MethodPost, "/mcp/workspaces/alpha", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
	}
	if workspace, ok := bindings.Workspace(MCPSessionIDFromHeader("server-session-1")); !ok || workspace != workspaceid.WorkspaceID("alpha") {
		t.Fatalf("server session binding = %q/%v, want alpha/true", workspace, ok)
	}

})

var _ = It("TestWorkspaceMCPHandlerRootMCPUsesExistingSessionBinding", func() {
	t := GinkgoT()
	bindings := NewMCPSessionBindings()
	if err := bindings.Bind(MCPSessionIDFromHeader("session-1"), workspaceid.WorkspaceID("alpha")); err != nil {
		t.Fatalf("Bind failed: %v", err)
	}
	var seenID workspaceid.WorkspaceID
	handler := NewWorkspaceMCPHandler(WorkspaceMCPHandlerOptions{
		Sessions: bindings,
		ResolveRoot: func(*http.Request) (workspaceid.WorkspaceID, error) {
			t.Fatalf("ResolveRoot should not run for a bound root MCP session")
			return "", nil
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

	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
	}
	if seenID != workspaceid.WorkspaceID("alpha") {
		t.Fatalf("resolved workspace = %q, want alpha", seenID)
	}

})

var _ = It("TestWorkspaceMCPHandlerSuccessfulDeleteClearsSessionBinding", func() {
	t := GinkgoT()
	bindings := NewMCPSessionBindings()
	if err := bindings.Bind(MCPSessionIDFromHeader("session-1"), workspaceid.WorkspaceID("alpha")); err != nil {
		t.Fatalf("Bind failed: %v", err)
	}
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

	if rec.Code != http.StatusOK {
		t.Fatalf("delete status = %d: %s", rec.Code, rec.Body.String())
	}
	if workspace, ok := bindings.Workspace(MCPSessionIDFromHeader("session-1")); ok {
		t.Fatalf("session binding = %q/%v, want cleared", workspace, ok)
	}

	req = httptest.NewRequest(http.MethodPost, "/mcp", nil)
	req.Header.Set("Mcp-Session-Id", "session-1")
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusConflict {
		t.Fatalf("root status after delete = %d, want ambiguous conflict", rec.Code)
	}

})

var _ = It("TestWorkspaceMCPHandlerResolvesBeforeBindingSession", func() {
	t := GinkgoT()
	bindings := NewMCPSessionBindings()
	handler := NewWorkspaceMCPHandler(WorkspaceMCPHandlerOptions{
		Sessions: bindings,
		Resolve: func(*http.Request, workspaceid.WorkspaceID) (WorkspaceRoute, error) {
			return WorkspaceRoute{}, ErrWorkspaceForbidden
		},
		Proxy: func(WorkspaceRoute) http.Handler {
			t.Fatalf("proxy should not be called")
			return nil
		},
	})

	req := httptest.NewRequest(http.MethodPost, "/mcp/workspaces/alpha", nil)
	req.Header.Set("Mcp-Session-Id", "forbidden-session")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403: %s", rec.Code, rec.Body.String())
	}
	if _, ok := bindings.Workspace(MCPSessionIDFromHeader("forbidden-session")); ok {
		t.Fatalf("forbidden session was bound before authorization")
	}

})

type rootMCPWorkspaceResolutionCase struct {
	rootID     workspaceid.WorkspaceID
	rootErr    error
	wantStatus int
	wantSeenID workspaceid.WorkspaceID
}

var _ = DescribeTable("TestWorkspaceMCPHandlerRootMCPRequiresExactlyOneWorkspace",
	func(tc rootMCPWorkspaceResolutionCase) {
		t := GinkgoT()
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

		if rec.Code != tc.wantStatus {
			t.Fatalf("status = %d, want %d: %s", rec.Code, tc.wantStatus, rec.Body.String())
		}
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
			assertStructuredMCPWorkspaceError(t, rec, want.code, want.messageID)
		}
		if seenID != tc.wantSeenID {
			t.Fatalf("resolved workspace = %q, want %q", seenID, tc.wantSeenID)
		}
	},
	Entry("one", rootMCPWorkspaceResolutionCase{
		rootID:     workspaceid.WorkspaceID("only"),
		wantStatus: http.StatusAccepted,
		wantSeenID: workspaceid.WorkspaceID("only"),
	}),
	Entry("none", rootMCPWorkspaceResolutionCase{
		rootErr:    ErrWorkspaceForbidden,
		wantStatus: http.StatusForbidden,
	}),
	Entry("ambiguous", rootMCPWorkspaceResolutionCase{
		rootErr:    ErrWorkspaceAmbiguous,
		wantStatus: http.StatusConflict,
	}),
	Entry("not found", rootMCPWorkspaceResolutionCase{
		rootErr:    ErrWorkspaceNotFound,
		wantStatus: http.StatusNotFound,
	}),
	Entry("unavailable", rootMCPWorkspaceResolutionCase{
		rootErr:    errors.New("boom"),
		wantStatus: http.StatusServiceUnavailable,
	}),
)

var _ = It("TestWorkspaceMCPHandlerReportsStructuredDependencyErrors", func() {
	t := GinkgoT()
	handler := NewWorkspaceMCPHandler(WorkspaceMCPHandlerOptions{
		ResolveRoot: func(*http.Request) (workspaceid.WorkspaceID, error) { return "home", nil },
	})
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/mcp", nil))
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503: %s", rec.Code, rec.Body.String())
	}
	assertStructuredMCPWorkspaceError(t, rec, errCodeMCPWorkspaceRouterUnavailable, sharederrors.MessageIDForCode(errCodeMCPWorkspaceRouterUnavailable))

})

func assertStructuredMCPWorkspaceError(t frontdTestTB, rec *httptest.ResponseRecorder, code sharederrors.ErrorCode, messageID sharederrors.MessageID) {
	t.Helper()
	var body struct {
		Error struct {
			Code      sharederrors.ErrorCode `json:"code"`
			MessageID sharederrors.MessageID `json:"messageId"`
			Message   string                 `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode structured error: %v; body=%s", err, rec.Body.String())
	}
	if body.Error.Code != code || body.Error.MessageID != messageID || body.Error.Message == "" {
		t.Fatalf("structured error = %#v, want code=%q messageId=%q with message", body.Error, code, messageID)
	}
}
