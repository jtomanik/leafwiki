package frontd

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"

	"github.com/perber/wiki/internal/workspaceid"
)

type MCPSessionBindings struct {
	mu       sync.Mutex
	sessions map[MCPSessionID]workspaceid.WorkspaceID
}

type MCPSessionID string

func (id MCPSessionID) String() string {
	return string(id)
}

func MCPSessionIDFromHeader(raw string) MCPSessionID {
	return MCPSessionID(strings.TrimSpace(raw))
}

func NewMCPSessionBindings() *MCPSessionBindings {
	return &MCPSessionBindings{sessions: map[MCPSessionID]workspaceid.WorkspaceID{}}
}

func (b *MCPSessionBindings) Bind(sessionID MCPSessionID, workspaceID workspaceid.WorkspaceID) error {
	if sessionID == "" || workspaceID == "" {
		return nil
	}
	if err := workspaceID.Validate(); err != nil {
		return err
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if existing := b.sessions[sessionID]; existing != "" && existing != workspaceID {
		return fmt.Errorf("mcp session %q is bound to workspace %q", sessionID.String(), existing.String())
	}
	b.sessions[sessionID] = workspaceID
	return nil
}

func (b *MCPSessionBindings) Workspace(sessionID MCPSessionID) (workspaceid.WorkspaceID, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	workspaceID, ok := b.sessions[sessionID]
	return workspaceID, ok
}

func (b *MCPSessionBindings) Unbind(sessionID MCPSessionID) {
	if sessionID == "" {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	delete(b.sessions, sessionID)
}

type WorkspaceMCPHandlerOptions struct {
	Sessions    *MCPSessionBindings
	Resolve     func(*http.Request, workspaceid.WorkspaceID) (WorkspaceRoute, error)
	ResolveRoot func(*http.Request) (workspaceid.WorkspaceID, error)
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
			if boundWorkspaceID, bound := h.opts.Sessions.Workspace(MCPSessionIDFromHeader(sessionID)); bound {
				workspaceID = boundWorkspaceID
			}
		}
		if workspaceID == "" {
			var err error
			workspaceID, err = h.opts.ResolveRoot(req)
			if err != nil {
				writeWorkspaceMCPError(w, err)
				return
			}
		}
	}
	if h.opts.Resolve == nil || h.opts.Proxy == nil {
		writeFrontdError(w, http.StatusServiceUnavailable, errCodeMCPWorkspaceRouterUnavailable)
		return
	}
	route, err := h.opts.Resolve(req, workspaceID)
	if err != nil {
		writeWorkspaceMCPError(w, err)
		return
	}
	if route.WorkspaceID == "" {
		route.WorkspaceID = workspaceID
	}
	if h.opts.Sessions != nil {
		if err := h.opts.Sessions.Bind(MCPSessionIDFromHeader(req.Header.Get("Mcp-Session-Id")), route.WorkspaceID); err != nil {
			writeFrontdError(w, http.StatusConflict, errCodeMCPSessionWorkspaceMismatch)
			return
		}
	}
	proxy := h.opts.Proxy(route)
	if proxy == nil {
		writeFrontdError(w, http.StatusServiceUnavailable, errCodeMCPWorkspaceUnavailable)
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
		h.opts.Sessions.Unbind(MCPSessionIDFromHeader(req.Header.Get("Mcp-Session-Id")))
		return
	}
	if err := h.opts.Sessions.Bind(MCPSessionIDFromHeader(recorder.Header().Get("Mcp-Session-Id")), route.WorkspaceID); err != nil {
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
		writeFrontdError(w, http.StatusNotFound, errCodeWorkspaceNotFound)
	case errors.Is(err, ErrWorkspaceForbidden):
		writeFrontdError(w, http.StatusForbidden, errCodeWorkspaceForbidden)
	case errors.Is(err, ErrWorkspaceAmbiguous):
		writeFrontdError(w, http.StatusConflict, errCodeWorkspaceAmbiguous)
	default:
		writeFrontdError(w, http.StatusServiceUnavailable, errCodeWorkspaceUnavailable)
	}
}

func parseWorkspaceMCPPath(path string) (workspaceid.WorkspaceID, bool) {
	rest := strings.TrimPrefix(path, "/mcp/workspaces/")
	if rest == path {
		return "", false
	}
	workspaceID := strings.Trim(strings.SplitN(rest, "/", 2)[0], "/")
	if workspaceID == "" {
		return "", false
	}
	typedWorkspaceID, err := workspaceid.ParseWorkspaceID(workspaceID)
	if err != nil {
		return "", false
	}
	return typedWorkspaceID, true
}
