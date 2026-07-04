package wikid

import (
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/gstruct"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"

	"github.com/perber/wiki/internal/frontd"
	"github.com/perber/wiki/internal/projectdaemon"
)

type privateControlPlaneRequest struct {
	Path         string
	Token        string
	ActorContext string
	RemoteAddr   string
	Body         string
}

func privateHandlerAcceptedDownstream() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		w.WriteHeader(http.StatusAccepted)
	})
}

var _ = ginkgo.Describe("private wikid handler", func() {
	ginkgo.It("rejects actor-context requests that omit the daemon token", func() {
		handler := NewPrivateHandler(PrivateHandlerOptions{
			DaemonToken:  "private-token",
			ActorContext: privateHandlerAcceptedDownstream(),
		})

		req := httptest.NewRequest(http.MethodPost, "/__leafwiki/actor-context", nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		Expect(rec).To(HaveHTTPStatus(http.StatusUnauthorized))
	})

	ginkgo.It("rejects workspace API requests that omit the daemon token", func() {
		handler := NewPrivateHandler(PrivateHandlerOptions{
			DaemonToken:  "private-token",
			WorkspaceAPI: privateHandlerAcceptedDownstream(),
		})

		req := httptest.NewRequest(http.MethodGet, "/__leafwiki/workspaces", nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		Expect(rec).To(HaveHTTPStatus(http.StatusUnauthorized))
	})

	ginkgo.It("forwards workspace API requests that include the daemon token", func() {
		var seenPath string
		handler := NewPrivateHandler(PrivateHandlerOptions{
			DaemonToken: "private-token",
			WorkspaceAPI: http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				seenPath = req.URL.Path
				w.WriteHeader(http.StatusAccepted)
			}),
		})

		req := httptest.NewRequest(http.MethodGet, "/__leafwiki/workspaces", nil)
		req.Header.Set(projectdaemon.ControlTokenHeader, "private-token")
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		Expect(rec).To(HaveHTTPStatus(http.StatusAccepted))
		Expect(seenPath).To(Equal("/__leafwiki/workspaces"))
	})

	ginkgo.It("rebases control-plane requests and strips private headers before forwarding", func() {
		var seen privateControlPlaneRequest
		controlPlane := http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			seen.Path = req.URL.Path
			seen.Token = req.Header.Get(projectdaemon.ControlTokenHeader)
			seen.ActorContext = req.Header.Get(projectdaemon.ActorContextHeader)
			seen.RemoteAddr = req.RemoteAddr
			raw, err := io.ReadAll(req.Body)
			Expect(err).To(Succeed())
			seen.Body = string(raw)
			w.WriteHeader(http.StatusAccepted)
		})
		handler := NewPrivateHandler(PrivateHandlerOptions{
			DaemonToken:  "private-token",
			BasePath:     "/wiki",
			ControlPlane: controlPlane,
		})

		req := httptest.NewRequest(http.MethodPost, frontd.ControlPlanePrefix+"/api/branding", strings.NewReader(`{"siteName":"Runtime Wiki"}`))
		req.RemoteAddr = "127.0.0.1:1111"
		req.Header.Set(projectdaemon.ControlTokenHeader, "private-token")
		req.Header.Set(projectdaemon.ActorContextHeader, "spoofed")
		req.Header.Set("X-LeafWiki-Original-Remote-Addr", "203.0.113.10:4321")
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		Expect(rec).To(HaveHTTPStatus(http.StatusAccepted))
		Expect(seen).To(gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
			"Path":         Equal("/wiki/api/branding"),
			"Token":        BeEmpty(),
			"ActorContext": BeEmpty(),
			"RemoteAddr":   Equal("203.0.113.10:4321"),
			"Body":         Equal(`{"siteName":"Runtime Wiki"}`),
		}))
	})

	ginkgo.It("forwards root well-known control-plane paths without rebasing", func() {
		var seenPath string
		controlPlane := http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			seenPath = req.URL.Path
			w.WriteHeader(http.StatusAccepted)
		})
		handler := NewPrivateHandler(PrivateHandlerOptions{
			DaemonToken:  "private-token",
			BasePath:     "/wiki",
			ControlPlane: controlPlane,
		})

		req := httptest.NewRequest(http.MethodGet, frontd.ControlPlanePrefix+"/.well-known/oauth-protected-resource/wiki/mcp", nil)
		req.Header.Set(projectdaemon.ControlTokenHeader, "private-token")
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		Expect(rec).To(HaveHTTPStatus(http.StatusAccepted))
		Expect(seenPath).To(Equal("/.well-known/oauth-protected-resource/wiki/mcp"))
	})
})
