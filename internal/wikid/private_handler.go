package wikid

import (
	"net/http"
	"strings"

	"github.com/perber/wiki/internal/frontd"
	"github.com/perber/wiki/internal/projectdaemon"
)

type PrivateHandlerOptions struct {
	DaemonToken  string
	BasePath     string
	Control      http.Handler
	ControlPlane http.Handler
	ActorContext http.Handler
	TokenVerify  http.Handler
}

func NewPrivateHandler(opts PrivateHandlerOptions) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		switch {
		case req.URL.Path == "/__leafwiki/actor-context":
			if !hasDaemonToken(req, opts.DaemonToken) {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
			if opts.ActorContext == nil {
				http.NotFound(w, req)
				return
			}
			opts.ActorContext.ServeHTTP(w, req)
		case req.URL.Path == "/__leafwiki/token/verify":
			if !hasDaemonToken(req, opts.DaemonToken) {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
			if opts.TokenVerify == nil {
				http.NotFound(w, req)
				return
			}
			opts.TokenVerify.ServeHTTP(w, req)
		case strings.HasPrefix(req.URL.Path, frontd.ControlPlanePrefix):
			if !hasDaemonToken(req, opts.DaemonToken) {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
			if opts.ControlPlane == nil {
				http.NotFound(w, req)
				return
			}
			forwardPrivateControlPlane(opts.ControlPlane, opts.BasePath, w, req)
		default:
			if opts.Control == nil {
				http.NotFound(w, req)
				return
			}
			opts.Control.ServeHTTP(w, req)
		}
	})
}

func hasDaemonToken(req *http.Request, daemonToken string) bool {
	return strings.TrimSpace(daemonToken) != "" && req.Header.Get(projectdaemon.ControlTokenHeader) == daemonToken
}

func forwardPrivateControlPlane(controlPlane http.Handler, basePath string, w http.ResponseWriter, req *http.Request) {
	stripped := strings.TrimPrefix(req.URL.Path, frontd.ControlPlanePrefix)
	if stripped == "" {
		stripped = "/"
	}
	clone := req.Clone(req.Context())
	clone.Body = req.Body
	if isWellKnownPath(stripped) {
		clone.URL.Path = stripped
	} else {
		clone.URL.Path = joinBasePathForPrivateControlPlane(basePath, stripped)
	}
	clone.URL.RawPath = ""
	clone.RemoteAddr = originalRemoteAddr(req)
	clone.Header = req.Header.Clone()
	clone.Header.Del(projectdaemon.ControlTokenHeader)
	clone.Header.Del(projectdaemon.ActorContextHeader)
	controlPlane.ServeHTTP(w, clone)
}

func joinBasePathForPrivateControlPlane(basePath string, path string) string {
	basePath = strings.TrimRight(strings.TrimSpace(basePath), "/")
	if basePath == "" {
		if strings.HasPrefix(path, "/") {
			return path
		}
		return "/" + path
	}
	if strings.HasPrefix(path, "/") {
		return basePath + path
	}
	return basePath + "/" + path
}

func isWellKnownPath(path string) bool {
	return path == "/.well-known" || strings.HasPrefix(path, "/.well-known/")
}

func originalRemoteAddr(req *http.Request) string {
	if value := strings.TrimSpace(req.Header.Get("X-LeafWiki-Original-Remote-Addr")); value != "" {
		return value
	}
	return req.RemoteAddr
}
