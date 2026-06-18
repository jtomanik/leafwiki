package frontd

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestMCPSessionBindingsRejectWorkspaceMismatch(t *testing.T) {
	bindings := NewMCPSessionBindings()

	if err := bindings.Bind("session-1", "alpha"); err != nil {
		t.Fatalf("Bind alpha failed: %v", err)
	}
	if err := bindings.Bind("session-1", "alpha"); err != nil {
		t.Fatalf("Bind same workspace failed: %v", err)
	}
	if err := bindings.Bind("session-1", "beta"); err == nil {
		t.Fatalf("expected workspace mismatch")
	}
}

func TestWorkspaceMCPHandlerRoutesExplicitWorkspaceAndBindsSession(t *testing.T) {
	bindings := NewMCPSessionBindings()
	var seenPath string
	handler := NewWorkspaceMCPHandler(WorkspaceMCPHandlerOptions{
		Sessions: bindings,
		Resolve: func(_ *http.Request, workspaceID string) (WorkspaceRoute, error) {
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
	if workspace, ok := bindings.Workspace("session-1"); !ok || workspace != "home" {
		t.Fatalf("session binding = %q/%v, want home/true", workspace, ok)
	}

	req = httptest.NewRequest(http.MethodPost, "/mcp/workspaces/alpha", nil)
	req.Header.Set("Mcp-Session-Id", "session-1")
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusConflict {
		t.Fatalf("mismatch status = %d, want 409: %s", rec.Code, rec.Body.String())
	}
}

func TestWorkspaceMCPHandlerBindsServerIssuedSessionAfterProxy(t *testing.T) {
	bindings := NewMCPSessionBindings()
	handler := NewWorkspaceMCPHandler(WorkspaceMCPHandlerOptions{
		Sessions: bindings,
		Resolve: func(*http.Request, string) (WorkspaceRoute, error) {
			return WorkspaceRoute{WorkspaceID: "alpha", Upstream: "http://127.0.0.1:1", DaemonToken: "token"}, nil
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
	if workspace, ok := bindings.Workspace("server-session-1"); !ok || workspace != "alpha" {
		t.Fatalf("server session binding = %q/%v, want alpha/true", workspace, ok)
	}
}

func TestWorkspaceMCPHandlerRootMCPUsesExistingSessionBinding(t *testing.T) {
	bindings := NewMCPSessionBindings()
	if err := bindings.Bind("session-1", "alpha"); err != nil {
		t.Fatalf("Bind failed: %v", err)
	}
	var seenID string
	handler := NewWorkspaceMCPHandler(WorkspaceMCPHandlerOptions{
		Sessions: bindings,
		ResolveRoot: func(*http.Request) (string, error) {
			t.Fatalf("ResolveRoot should not run for a bound root MCP session")
			return "", nil
		},
		Resolve: func(_ *http.Request, workspaceID string) (WorkspaceRoute, error) {
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
	if seenID != "alpha" {
		t.Fatalf("resolved workspace = %q, want alpha", seenID)
	}
}

func TestWorkspaceMCPHandlerSuccessfulDeleteClearsSessionBinding(t *testing.T) {
	bindings := NewMCPSessionBindings()
	if err := bindings.Bind("session-1", "alpha"); err != nil {
		t.Fatalf("Bind failed: %v", err)
	}
	handler := NewWorkspaceMCPHandler(WorkspaceMCPHandlerOptions{
		Sessions: bindings,
		ResolveRoot: func(*http.Request) (string, error) {
			return "", ErrWorkspaceAmbiguous
		},
		Resolve: func(_ *http.Request, workspaceID string) (WorkspaceRoute, error) {
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
	if workspace, ok := bindings.Workspace("session-1"); ok {
		t.Fatalf("session binding = %q/%v, want cleared", workspace, ok)
	}

	req = httptest.NewRequest(http.MethodPost, "/mcp", nil)
	req.Header.Set("Mcp-Session-Id", "session-1")
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusConflict {
		t.Fatalf("root status after delete = %d, want ambiguous conflict", rec.Code)
	}
}

func TestWorkspaceMCPHandlerResolvesBeforeBindingSession(t *testing.T) {
	bindings := NewMCPSessionBindings()
	handler := NewWorkspaceMCPHandler(WorkspaceMCPHandlerOptions{
		Sessions: bindings,
		Resolve: func(*http.Request, string) (WorkspaceRoute, error) {
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
	if _, ok := bindings.Workspace("forbidden-session"); ok {
		t.Fatalf("forbidden session was bound before authorization")
	}
}

func TestWorkspaceMCPHandlerRootMCPRequiresExactlyOneWorkspace(t *testing.T) {
	for _, tc := range []struct {
		name       string
		rootID     string
		rootErr    error
		wantStatus int
		wantSeenID string
	}{
		{name: "one", rootID: "only", wantStatus: http.StatusAccepted, wantSeenID: "only"},
		{name: "none", rootErr: ErrWorkspaceForbidden, wantStatus: http.StatusForbidden},
		{name: "ambiguous", rootErr: ErrWorkspaceAmbiguous, wantStatus: http.StatusConflict},
		{name: "not found", rootErr: ErrWorkspaceNotFound, wantStatus: http.StatusNotFound},
		{name: "unavailable", rootErr: errors.New("boom"), wantStatus: http.StatusServiceUnavailable},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var seenID string
			handler := NewWorkspaceMCPHandler(WorkspaceMCPHandlerOptions{
				ResolveRoot: func(*http.Request) (string, error) {
					return tc.rootID, tc.rootErr
				},
				Resolve: func(_ *http.Request, workspaceID string) (WorkspaceRoute, error) {
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
			if seenID != tc.wantSeenID {
				t.Fatalf("resolved workspace = %q, want %q", seenID, tc.wantSeenID)
			}
		})
	}
}
