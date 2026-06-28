package frontd

import (
	"fmt"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"

	"github.com/perber/wiki/internal/projectdaemon"
)

type WorkspaceProxyOptions struct {
	Upstream    string
	DaemonToken string
	Actor       func(*http.Request) (projectdaemon.ActorContext, error)
}

type IngressOptions struct {
	BasePath     string
	Workspace    http.Handler
	Workspaces   http.Handler
	MCP          http.Handler
	ControlPlane http.Handler
}

const ControlPlanePrefix = "/__leafwiki/control-plane"

var encodeActorContext = projectdaemon.EncodeActorContext

func NewIngressHandler(public http.Handler, opts IngressOptions) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if opts.ControlPlane != nil && isWellKnownPath(req.URL.Path) {
			opts.ControlPlane.ServeHTTP(w, cloneRequestPath(req, req.URL.Path))
			return
		}
		strippedPath, ok := stripBasePath(req.URL.Path, opts.BasePath)
		if !ok {
			public.ServeHTTP(w, req)
			return
		}
		if opts.MCP != nil && isMCPPath(strippedPath) {
			opts.MCP.ServeHTTP(w, cloneRequestPath(req, strippedPath))
			return
		}
		if opts.Workspaces != nil && isWorkspacesPath(strippedPath) {
			opts.Workspaces.ServeHTTP(w, cloneRequestPath(req, strippedPath))
			return
		}
		if opts.Workspace != nil && isWorkspacePath(strippedPath) {
			opts.Workspace.ServeHTTP(w, cloneRequestPath(req, strippedPath))
			return
		}
		if opts.ControlPlane != nil && isControlPlanePath(strippedPath) {
			opts.ControlPlane.ServeHTTP(w, cloneRequestPath(req, strippedPath))
			return
		}
		public.ServeHTTP(w, req)
	})
}

func NewControlPlaneProxy(upstreamURL string, daemonToken string) (http.Handler, error) {
	upstream, err := url.Parse(strings.TrimSpace(upstreamURL))
	if err != nil || upstream.Scheme == "" || upstream.Host == "" {
		return nil, fmt.Errorf("invalid wikid upstream %q", upstreamURL)
	}
	if strings.TrimSpace(daemonToken) == "" {
		return nil, fmt.Errorf("daemon token is required")
	}
	proxy := httputil.NewSingleHostReverseProxy(upstream)
	proxy.ErrorHandler = retryableUnavailable
	originalDirector := proxy.Director
	proxy.Director = func(req *http.Request) {
		originalHost := req.Host
		originalMethod := req.Method
		originalRemoteAddr := req.RemoteAddr
		originalDirector(req)
		req.Host = originalHost
		req.URL.Path = ControlPlanePrefix + ensureLeadingSlash(req.URL.Path)
		req.URL.RawPath = ""
		req.Header.Set(projectdaemon.ControlTokenHeader, daemonToken)
		req.Header.Set("X-LeafWiki-Original-Method", originalMethod)
		req.Header.Set("X-LeafWiki-Original-Remote-Addr", originalRemoteAddr)
		req.Header.Del(projectdaemon.ActorContextHeader)
	}
	return proxy, nil
}

func NewWorkspaceProxy(opts WorkspaceProxyOptions) (http.Handler, error) {
	upstream, err := url.Parse(strings.TrimSpace(opts.Upstream))
	if err != nil || upstream.Scheme == "" || upstream.Host == "" {
		return nil, fmt.Errorf("invalid workspaced upstream %q", opts.Upstream)
	}
	if strings.TrimSpace(opts.DaemonToken) == "" {
		return nil, fmt.Errorf("daemon token is required")
	}
	if opts.Actor == nil {
		return nil, fmt.Errorf("actor context resolver is required")
	}

	proxy := newPrivateActorProxy(upstream, func(*http.Request) string {
		return opts.DaemonToken
	})
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		actor, err := opts.Actor(req)
		if err != nil {
			writeFrontdError(w, http.StatusUnauthorized, errCodeWorkspaceActorContextFailed)
			return
		}
		encoded, err := encodeActorContext(actor)
		if err != nil {
			writeFrontdError(w, http.StatusInternalServerError, errCodeWorkspaceActorContextEncodeFailed)
			return
		}
		clone := req.Clone(req.Context())
		clone.Body = req.Body
		clone.Header = req.Header.Clone()
		clone.Header.Set(projectdaemon.ActorContextHeader, encoded)
		proxy.ServeHTTP(w, clone)
	}), nil
}

func NewMCPProxy(upstreamURL string, daemonToken string) (http.Handler, error) {
	upstream, err := url.Parse(strings.TrimSpace(upstreamURL))
	if err != nil || upstream.Scheme == "" || upstream.Host == "" {
		return nil, fmt.Errorf("invalid workspaced upstream %q", upstreamURL)
	}
	if strings.TrimSpace(daemonToken) == "" {
		return nil, fmt.Errorf("daemon token is required")
	}
	proxy := httputil.NewSingleHostReverseProxy(upstream)
	proxy.ErrorHandler = retryableUnavailable
	originalDirector := proxy.Director
	proxy.Director = func(req *http.Request) {
		originalDirector(req)
		req.Host = upstream.Host
		req.Header.Set(projectdaemon.ControlTokenHeader, daemonToken)
		req.Header.Del(projectdaemon.ActorContextHeader)
	}
	return proxy, nil
}

func NewMCPProxyWithActor(opts WorkspaceProxyOptions) (http.Handler, error) {
	return NewWorkspaceProxy(opts)
}

func newPrivateActorProxy(upstream *url.URL, daemonToken func(*http.Request) string) *httputil.ReverseProxy {
	proxy := httputil.NewSingleHostReverseProxy(upstream)
	proxy.ErrorHandler = retryableUnavailable
	originalDirector := proxy.Director
	proxy.Director = func(req *http.Request) {
		actorContext := req.Header.Get(projectdaemon.ActorContextHeader)
		token := daemonToken(req)
		originalDirector(req)
		req.Host = upstream.Host
		req.Header.Del("Authorization")
		req.Header.Del("Cookie")
		req.Header.Set(projectdaemon.ActorContextHeader, actorContext)
		req.Header.Set(projectdaemon.ControlTokenHeader, token)
	}
	return proxy
}

func retryableUnavailable(w http.ResponseWriter, _ *http.Request, _ error) {
	w.Header().Set("Retry-After", "1")
	writeFrontdError(w, http.StatusServiceUnavailable, errCodeWorkspacedUnavailable)
}

func stripBasePath(path string, basePath string) (string, bool) {
	basePath = strings.TrimRight(strings.TrimSpace(basePath), "/")
	if basePath == "" {
		if path == "" {
			return "/", true
		}
		return path, true
	}
	if path != basePath && !strings.HasPrefix(path, basePath+"/") {
		return path, false
	}
	stripped := strings.TrimPrefix(path, basePath)
	if stripped == "" {
		return "/", true
	}
	return stripped, true
}

func cloneRequestPath(req *http.Request, path string) *http.Request {
	clone := req.Clone(req.Context())
	clone.Body = req.Body
	clone.URL.Path = path
	clone.URL.RawPath = ""
	return clone
}

func ensureLeadingSlash(path string) string {
	if strings.HasPrefix(path, "/") {
		return path
	}
	return "/" + path
}

func isWorkspacesPath(path string) bool {
	return isWorkspacesAPIPath(http.MethodGet, path) || isWorkspacesAPIPath(http.MethodPost, path)
}

func isMCPPath(path string) bool {
	return path == "/mcp" || strings.HasPrefix(path, "/mcp/workspaces/")
}

func isWorkspacePath(path string) bool {
	if strings.HasPrefix(path, PublicWorkspacesPrefix+"/") {
		return true
	}
	for _, prefix := range []string{
		"/assets",
		"/api/tree",
		"/api/pages",
		"/api/search",
		"/api/tags",
		"/api/properties",
		"/api/import",
		"/api/presence",
		"/api/workspace-sync",
	} {
		if path == prefix || strings.HasPrefix(path, prefix+"/") {
			return true
		}
	}
	return false
}

func isControlPlanePath(path string) bool {
	for _, prefix := range []string{
		"/api/config",
		"/api/auth",
		"/api/users",
		"/api/branding",
		"/api/health",
		"/branding",
		"/favicon.ico",
		"/oauth",
		"/.well-known",
	} {
		if path == prefix || strings.HasPrefix(path, prefix+"/") {
			return true
		}
	}
	return false
}

func isWellKnownPath(path string) bool {
	return path == "/.well-known" || strings.HasPrefix(path, "/.well-known/")
}
