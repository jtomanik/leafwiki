package frontd

import (
	"fmt"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"

	"github.com/perber/wiki/internal/projectdaemon"
)

const PublicWorkspacesPrefix = "/api/workspaces"

func NewWorkspacesAPI(wikidURL string, daemonToken string) (http.Handler, error) {
	upstream, err := url.Parse(strings.TrimSpace(wikidURL))
	if err != nil || upstream.Scheme == "" || upstream.Host == "" {
		return nil, fmt.Errorf("invalid wikid upstream %q", wikidURL)
	}
	if strings.TrimSpace(daemonToken) == "" {
		return nil, fmt.Errorf("daemon token is required")
	}
	proxy := httputil.NewSingleHostReverseProxy(upstream)
	proxy.ErrorHandler = retryableUnavailable
	originalDirector := proxy.Director
	proxy.Director = func(req *http.Request) {
		originalMethod := req.Method
		originalPath := req.URL.Path
		originalRemoteAddr := req.RemoteAddr
		originalDirector(req)
		req.Host = upstream.Host
		req.URL.Path = privateWorkspaceAPIPath(originalPath)
		req.URL.RawPath = ""
		setOriginalRequestHeaders(req, originalMethod, originalPath, originalRemoteAddr)
		req.Header.Set(projectdaemon.ControlTokenHeader, daemonToken)
		req.Header.Del(projectdaemon.ActorContextHeader)
	}
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if !isWorkspacesAPIPath(req.Method, req.URL.Path) {
			http.NotFound(w, req)
			return
		}
		proxy.ServeHTTP(w, req)
	}), nil
}

func isWorkspacesAPIPath(method string, path string) bool {
	if method == http.MethodGet && path == PublicWorkspacesPrefix {
		return true
	}
	rest := strings.TrimPrefix(path, PublicWorkspacesPrefix+"/")
	if rest == path {
		return false
	}
	parts := strings.Split(strings.Trim(rest, "/"), "/")
	if len(parts) != 2 || parts[0] == "" {
		return false
	}
	return (method == http.MethodGet && parts[1] == "status") ||
		(method == http.MethodPost && parts[1] == "ensure")
}

func privateWorkspaceAPIPath(path string) string {
	if path == PublicWorkspacesPrefix {
		return "/__leafwiki/workspaces"
	}
	return "/__leafwiki/workspaces/" + strings.TrimPrefix(path, PublicWorkspacesPrefix+"/")
}
