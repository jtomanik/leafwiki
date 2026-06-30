package frontd

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	sharederrors "github.com/perber/wiki/internal/core/shared/errors"
	"github.com/perber/wiki/internal/projectdaemon"
	"github.com/perber/wiki/internal/workspaceid"
)

var (
	ErrWorkspaceNotFound     = errors.New("workspace not found")
	ErrWorkspaceForbidden    = errors.New("workspace forbidden")
	ErrWorkspaceAmbiguous    = errors.New("workspace ambiguous")
	ErrWorkspaceListFailed   = errors.New("list workspaces failed")
	ErrWorkspaceEnsureFailed = errors.New("ensure workspace failed")
	ErrWorkspaceNotRunning   = errors.New("workspace is not running")
)

const (
	errCodeWorkspaceResolverUnavailable      sharederrors.ErrorCode = "workspace_resolver_unavailable"
	errCodeWorkspaceNotFound                 sharederrors.ErrorCode = "workspace_not_found"
	errCodeWorkspaceForbidden                sharederrors.ErrorCode = "workspace_forbidden"
	errCodeWorkspaceUnavailable              sharederrors.ErrorCode = "workspace_unavailable"
	errCodeWorkspaceActorContextUnavailable  sharederrors.ErrorCode = "workspace_actor_context_unavailable"
	errCodeWorkspaceActorContextFailed       sharederrors.ErrorCode = "workspace_actor_context_failed"
	errCodeWorkspaceActorContextEncodeFailed sharederrors.ErrorCode = "workspace_actor_context_encode_failed"
	errCodeWorkspacedUnavailable             sharederrors.ErrorCode = "workspaced_unavailable"
	errCodeMCPWorkspaceRouterUnavailable     sharederrors.ErrorCode = "mcp_workspace_router_unavailable"
	errCodeMCPSessionWorkspaceMismatch       sharederrors.ErrorCode = "mcp_session_workspace_mismatch"
	errCodeMCPWorkspaceUnavailable           sharederrors.ErrorCode = "mcp_workspace_unavailable"
	errCodeWorkspaceAmbiguous                sharederrors.ErrorCode = "workspace_ambiguous"
)

type WorkspaceRoute struct {
	WorkspaceID   workspaceid.WorkspaceID
	Upstream      string
	DaemonToken   string
	PrivateMCPURL string
}

type WorkspaceRouterProxyOptions struct {
	Resolve func(*http.Request, workspaceid.WorkspaceID) (WorkspaceRoute, error)
	Actor   func(*http.Request, workspaceid.WorkspaceID) (projectdaemon.ActorContext, error)
}

func NewWorkspaceRouterProxy(opts WorkspaceRouterProxyOptions) http.Handler {
	return &workspaceRouterProxy{opts: opts}
}

type workspaceRouterProxy struct {
	opts WorkspaceRouterProxyOptions
}

func (p *workspaceRouterProxy) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	workspaceID, upstreamPath, ok := parseWorkspaceAPIPath(req.URL.Path)
	if !ok {
		http.NotFound(w, req)
		return
	}
	if p.opts.Resolve == nil {
		writeFrontdError(w, http.StatusServiceUnavailable, errCodeWorkspaceResolverUnavailable)
		return
	}
	route, err := p.opts.Resolve(req, workspaceID)
	if err != nil {
		switch {
		case errors.Is(err, ErrWorkspaceNotFound):
			writeFrontdError(w, http.StatusNotFound, errCodeWorkspaceNotFound)
		case errors.Is(err, ErrWorkspaceForbidden):
			writeFrontdError(w, http.StatusForbidden, errCodeWorkspaceForbidden)
		default:
			writeFrontdError(w, http.StatusServiceUnavailable, errCodeWorkspaceUnavailable)
		}
		return
	}
	if route.WorkspaceID == "" {
		route.WorkspaceID = workspaceID
	}
	if p.opts.Actor == nil {
		writeFrontdError(w, http.StatusServiceUnavailable, errCodeWorkspaceActorContextUnavailable)
		return
	}
	actor, err := p.opts.Actor(req, route.WorkspaceID)
	if err != nil {
		writeFrontdError(w, http.StatusUnauthorized, errCodeWorkspaceActorContextFailed)
		return
	}
	encoded, err := encodeActorContext(actor)
	if err != nil {
		writeFrontdError(w, http.StatusInternalServerError, errCodeWorkspaceActorContextEncodeFailed)
		return
	}
	proxy, err := p.proxy(route)
	if err != nil {
		writeFrontdError(w, http.StatusServiceUnavailable, errCodeWorkspaceUnavailable)
		return
	}
	clone := req.Clone(req.Context())
	clone.Body = req.Body
	clone.Header = req.Header.Clone()
	clone.URL.Path = upstreamPath
	clone.URL.RawPath = ""
	clone.Header.Set(projectdaemon.ActorContextHeader, encoded)
	clone.Header.Set(projectdaemon.ControlTokenHeader, route.DaemonToken)
	proxy.ServeHTTP(w, clone)
}

func writeFrontdError(w http.ResponseWriter, status int, code sharederrors.ErrorCode) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"error": sharederrors.NewLocalizedErrorDetailFromCode(code),
	})
}

func (p *workspaceRouterProxy) proxy(route WorkspaceRoute) (http.Handler, error) {
	upstreamURL := strings.TrimSpace(route.Upstream)
	daemonToken := strings.TrimSpace(route.DaemonToken)
	if upstreamURL == "" || daemonToken == "" {
		return nil, errWorkspaceRouteIncomplete
	}
	upstream, err := url.Parse(upstreamURL)
	if err != nil || upstream.Scheme == "" || upstream.Host == "" {
		return nil, fmt.Errorf("%w: %q", errInvalidWorkspacedUpstream, upstreamURL)
	}
	return newPrivateActorProxy(upstream, func(req *http.Request) string {
		return req.Header.Get(projectdaemon.ControlTokenHeader)
	}), nil
}

func parseWorkspaceAPIPath(path string) (workspaceid.WorkspaceID, string, bool) {
	rest := strings.TrimPrefix(path, PublicWorkspacesPrefix+"/")
	if rest == path {
		return "", "", false
	}
	parts := strings.SplitN(strings.Trim(rest, "/"), "/", 2)
	if len(parts) != 2 || strings.TrimSpace(parts[0]) == "" || strings.TrimSpace(parts[1]) == "" {
		return "", "", false
	}
	workspaceID, err := workspaceid.ValidateWorkspaceID(parts[0])
	if err != nil {
		return "", "", false
	}
	if strings.HasPrefix(parts[1], "assets/") {
		return workspaceID, "/" + parts[1], true
	}
	return workspaceID, "/api/" + parts[1], true
}
