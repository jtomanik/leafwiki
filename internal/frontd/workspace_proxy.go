package frontd

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/perber/wiki/internal/projectdaemon"
	"github.com/perber/wiki/internal/workspaceid"
)

var (
	ErrWorkspaceNotFound  = errors.New("workspace not found")
	ErrWorkspaceForbidden = errors.New("workspace forbidden")
	ErrWorkspaceAmbiguous = errors.New("workspace ambiguous")
)

type WorkspaceRoute struct {
	WorkspaceID   string
	Upstream      string
	DaemonToken   string
	PrivateMCPURL string
}

type WorkspaceRouterProxyOptions struct {
	Resolve func(*http.Request, string) (WorkspaceRoute, error)
	Actor   func(*http.Request, string) (projectdaemon.ActorContext, error)
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
		http.Error(w, "workspace resolver is unavailable", http.StatusServiceUnavailable)
		return
	}
	route, err := p.opts.Resolve(req, workspaceID)
	if err != nil {
		switch {
		case errors.Is(err, ErrWorkspaceNotFound):
			http.Error(w, "workspace not found", http.StatusNotFound)
		case errors.Is(err, ErrWorkspaceForbidden):
			http.Error(w, "workspace forbidden", http.StatusForbidden)
		default:
			http.Error(w, "workspace unavailable", http.StatusServiceUnavailable)
		}
		return
	}
	if strings.TrimSpace(route.WorkspaceID) == "" {
		route.WorkspaceID = workspaceID
	}
	if p.opts.Actor == nil {
		http.Error(w, "actor context resolver is unavailable", http.StatusServiceUnavailable)
		return
	}
	actor, err := p.opts.Actor(req, route.WorkspaceID)
	if err != nil {
		http.Error(w, "resolve actor context", http.StatusUnauthorized)
		return
	}
	encoded, err := projectdaemon.EncodeActorContext(actor)
	if err != nil {
		http.Error(w, "encode actor context", http.StatusInternalServerError)
		return
	}
	proxy, err := p.proxy(route)
	if err != nil {
		http.Error(w, "workspace unavailable", http.StatusServiceUnavailable)
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

func (p *workspaceRouterProxy) proxy(route WorkspaceRoute) (http.Handler, error) {
	upstreamURL := strings.TrimSpace(route.Upstream)
	daemonToken := strings.TrimSpace(route.DaemonToken)
	if upstreamURL == "" || daemonToken == "" {
		return nil, fmt.Errorf("workspace route is incomplete")
	}
	upstream, err := url.Parse(upstreamURL)
	if err != nil || upstream.Scheme == "" || upstream.Host == "" {
		return nil, fmt.Errorf("invalid workspaced upstream %q", upstreamURL)
	}
	return newPrivateActorProxy(upstream, func(req *http.Request) string {
		return req.Header.Get(projectdaemon.ControlTokenHeader)
	}), nil
}

func parseWorkspaceAPIPath(path string) (string, string, bool) {
	rest := strings.TrimPrefix(path, PublicWorkspacesPrefix+"/")
	if rest == path {
		return "", "", false
	}
	parts := strings.SplitN(strings.Trim(rest, "/"), "/", 2)
	if len(parts) != 2 || strings.TrimSpace(parts[0]) == "" || strings.TrimSpace(parts[1]) == "" {
		return "", "", false
	}
	if err := workspaceid.ValidateWorkspaceID(parts[0]); err != nil {
		return "", "", false
	}
	if strings.HasPrefix(parts[1], "assets/") {
		return parts[0], "/" + parts[1], true
	}
	return parts[0], "/api/" + parts[1], true
}
