package frontd

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
)

type MCPSessionBindings struct {
	mu       sync.Mutex
	sessions map[string]string
}

func NewMCPSessionBindings() *MCPSessionBindings {
	return &MCPSessionBindings{sessions: map[string]string{}}
}

func (b *MCPSessionBindings) Bind(sessionID string, workspaceID string) error {
	sessionID = strings.TrimSpace(sessionID)
	workspaceID = strings.TrimSpace(workspaceID)
	if sessionID == "" || workspaceID == "" {
		return nil
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if existing := b.sessions[sessionID]; existing != "" && existing != workspaceID {
		return fmt.Errorf("mcp session %q is bound to workspace %q", sessionID, existing)
	}
	b.sessions[sessionID] = workspaceID
	return nil
}

func (b *MCPSessionBindings) Workspace(sessionID string) (string, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	workspaceID, ok := b.sessions[strings.TrimSpace(sessionID)]
	return workspaceID, ok
}

func (b *MCPSessionBindings) Unbind(sessionID string) {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	delete(b.sessions, sessionID)
}

type WorkspaceMCPHandlerOptions struct {
	Sessions    *MCPSessionBindings
	Resolve     func(*http.Request, string) (WorkspaceRoute, error)
	ResolveRoot func(*http.Request) (string, error)
	Proxy       func(WorkspaceRoute) http.Handler
}

func NewWorkspaceMCPHandler(opts WorkspaceMCPHandlerOptions) http.Handler {
	return &workspaceMCPHandler{opts: opts}
}

type workspaceMCPHandler struct {
	opts WorkspaceMCPHandlerOptions
}

func (h *workspaceMCPHandler) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	workspaceID, ok := parseWorkspaceMCPPath(req.URL.Path)
	if !ok {
		if req.URL.Path != "/mcp" || h.opts.ResolveRoot == nil {
			http.NotFound(w, req)
			return
		}
		sessionID := req.Header.Get("Mcp-Session-Id")
		if h.opts.Sessions != nil {
			if boundWorkspaceID, bound := h.opts.Sessions.Workspace(sessionID); bound {
				workspaceID = boundWorkspaceID
			}
		}
		if strings.TrimSpace(workspaceID) == "" {
			var err error
			workspaceID, err = h.opts.ResolveRoot(req)
			if err != nil {
				writeWorkspaceMCPError(w, err)
				return
			}
		}
	}
	if h.opts.Resolve == nil || h.opts.Proxy == nil {
		http.Error(w, "mcp workspace router unavailable", http.StatusServiceUnavailable)
		return
	}
	route, err := h.opts.Resolve(req, workspaceID)
	if err != nil {
		writeWorkspaceMCPError(w, err)
		return
	}
	if strings.TrimSpace(route.WorkspaceID) == "" {
		route.WorkspaceID = workspaceID
	}
	if h.opts.Sessions != nil {
		if err := h.opts.Sessions.Bind(req.Header.Get("Mcp-Session-Id"), route.WorkspaceID); err != nil {
			http.Error(w, "mcp session workspace mismatch", http.StatusConflict)
			return
		}
	}
	proxy := h.opts.Proxy(route)
	if proxy == nil {
		http.Error(w, "workspace mcp unavailable", http.StatusServiceUnavailable)
		return
	}
	clone := req.Clone(req.Context())
	clone.Body = req.Body
	clone.URL.Path = "/mcp"
	clone.URL.RawPath = ""
	recorder := &mcpSessionResponseWriter{ResponseWriter: w}
	proxy.ServeHTTP(recorder, clone)
	if h.opts.Sessions == nil || recorder.statusCode >= http.StatusBadRequest {
		return
	}
	if req.Method == http.MethodDelete {
		h.opts.Sessions.Unbind(req.Header.Get("Mcp-Session-Id"))
		return
	}
	if err := h.opts.Sessions.Bind(recorder.Header().Get("Mcp-Session-Id"), route.WorkspaceID); err != nil {
		// A mismatch here means the upstream tried to reuse an already-bound
		// server session across workspaces. The response has already been sent,
		// so record no new binding and let the next request fail before proxying.
		return
	}
}

type mcpSessionResponseWriter struct {
	http.ResponseWriter
	statusCode int
}

func (w *mcpSessionResponseWriter) WriteHeader(statusCode int) {
	if w.statusCode == 0 {
		w.statusCode = statusCode
	}
	w.ResponseWriter.WriteHeader(statusCode)
}

func (w *mcpSessionResponseWriter) Write(bytes []byte) (int, error) {
	if w.statusCode == 0 {
		w.statusCode = http.StatusOK
	}
	return w.ResponseWriter.Write(bytes)
}

func (w *mcpSessionResponseWriter) Unwrap() http.ResponseWriter {
	return w.ResponseWriter
}

func writeWorkspaceMCPError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrWorkspaceNotFound):
		http.Error(w, "workspace not found", http.StatusNotFound)
	case errors.Is(err, ErrWorkspaceForbidden):
		http.Error(w, "workspace forbidden", http.StatusForbidden)
	case errors.Is(err, ErrWorkspaceAmbiguous):
		http.Error(w, "workspace selection is ambiguous", http.StatusConflict)
	default:
		http.Error(w, "workspace unavailable", http.StatusServiceUnavailable)
	}
}

func parseWorkspaceMCPPath(path string) (string, bool) {
	rest := strings.TrimPrefix(path, "/mcp/workspaces/")
	if rest == path {
		return "", false
	}
	workspaceID := strings.Trim(strings.SplitN(rest, "/", 2)[0], "/")
	if workspaceID == "" {
		return "", false
	}
	return workspaceID, true
}
