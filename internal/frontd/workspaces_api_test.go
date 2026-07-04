package frontd

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/perber/wiki/internal/projectdaemon"
)

type observedWorkspacesAPIOriginalHeaders struct {
	Method string
	Path   string
	Remote string
}

var _ = Describe("workspaces API proxy", Label("integration"), func() {
	It("forwards list, status, and ensure requests to wikid", func() {
		var seen []string
		upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			seen = append(seen, req.Method+" "+req.URL.Path+" "+req.Header.Get(projectdaemon.ControlTokenHeader))
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"ok":true}`)
		}))
		DeferCleanup(upstream.Close)

		handler, err := NewWorkspacesAPI(upstream.URL, "daemon-token")
		Expect(err).To(Succeed())
		for _, tc := range []struct {
			method string
			path   string
		}{
			{method: http.MethodGet, path: "/api/workspaces"},
			{method: http.MethodGet, path: "/api/workspaces/home/status"},
			{method: http.MethodPost, path: "/api/workspaces/home/ensure"},
		} {
			req := httptest.NewRequest(tc.method, tc.path, nil)
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)
			Expect(rec).To(HaveHTTPStatus(http.StatusOK))
		}

		Expect(seen).To(HaveExactElements(
			"GET /__leafwiki/workspaces daemon-token",
			"GET /__leafwiki/workspaces/home/status daemon-token",
			"POST /__leafwiki/workspaces/home/ensure daemon-token",
		))
	})

	It("overwrites spoofed original-request headers before forwarding", func() {
		var seen observedWorkspacesAPIOriginalHeaders
		upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			seen.Method = req.Header.Get("X-LeafWiki-Original-Method")
			seen.Path = req.Header.Get("X-LeafWiki-Original-Path")
			seen.Remote = req.Header.Get("X-LeafWiki-Original-Remote-Addr")
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"ok":true}`)
		}))
		DeferCleanup(upstream.Close)

		handler, err := NewWorkspacesAPI(upstream.URL, "daemon-token")
		Expect(err).To(Succeed())
		req := httptest.NewRequest(http.MethodPost, "/api/workspaces/home/ensure", nil)
		req.RemoteAddr = "198.51.100.7:1234"
		req.Header.Set("X-LeafWiki-Original-Method", http.MethodGet)
		req.Header.Set("X-LeafWiki-Original-Path", "/spoofed")
		req.Header.Set("X-LeafWiki-Original-Remote-Addr", "127.0.0.1:1")

		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		Expect(rec).To(HaveHTTPStatus(http.StatusOK))
		Expect(seen).To(SatisfyAll(
			HaveField("Method", Equal(http.MethodPost)),
			HaveField("Path", Equal("/api/workspaces/home/ensure")),
			HaveField("Remote", Equal("198.51.100.7:1234")),
		))
	})

	It("rejects unsupported workspace API shapes", func() {
		handler, err := NewWorkspacesAPI("http://127.0.0.1:1", "daemon-token")
		Expect(err).To(Succeed())

		req := httptest.NewRequest(http.MethodDelete, "/api/workspaces/home", strings.NewReader(""))
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		Expect(rec).To(HaveHTTPStatus(http.StatusNotFound))
	})
})
