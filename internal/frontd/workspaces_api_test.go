package frontd

import (
	. "github.com/onsi/ginkgo/v2"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"

	"github.com/perber/wiki/internal/projectdaemon"
)

var _ = It("TestWorkspacesAPIForwardsListStatusAndEnsureToWikid", func() {
	t := GinkgoT()
	var seen []string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		seen = append(seen, req.Method+" "+req.URL.Path+" "+req.Header.Get(projectdaemon.ControlTokenHeader))
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"ok":true}`)
	}))
	DeferCleanup(upstream.Close)

	handler, err := NewWorkspacesAPI(upstream.URL, "daemon-token")
	if err != nil {
		t.Fatalf("NewWorkspacesAPI failed: %v", err)
	}
	for _, tc := range []struct {
		method string
		path   string
		want   string
	}{
		{method: http.MethodGet, path: "/api/workspaces", want: "GET /__leafwiki/workspaces daemon-token"},
		{method: http.MethodGet, path: "/api/workspaces/home/status", want: "GET /__leafwiki/workspaces/home/status daemon-token"},
		{method: http.MethodPost, path: "/api/workspaces/home/ensure", want: "POST /__leafwiki/workspaces/home/ensure daemon-token"},
	} {
		req := httptest.NewRequest(tc.method, tc.path, nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s %s status = %d: %s", tc.method, tc.path, rec.Code, rec.Body.String())
		}
		if got := seen[len(seen)-1]; got != tc.want {
			t.Fatalf("%s %s forwarded = %q, want %q", tc.method, tc.path, got, tc.want)
		}
	}

})

var _ = It("TestWorkspacesAPIOverwritesSpoofedOriginalRequestHeaders", func() {
	t := GinkgoT()
	var seen struct {
		method string
		path   string
		remote string
	}
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		seen.method = req.Header.Get("X-LeafWiki-Original-Method")
		seen.path = req.Header.Get("X-LeafWiki-Original-Path")
		seen.remote = req.Header.Get("X-LeafWiki-Original-Remote-Addr")
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"ok":true}`)
	}))
	DeferCleanup(upstream.Close)

	handler, err := NewWorkspacesAPI(upstream.URL, "daemon-token")
	if err != nil {
		t.Fatalf("NewWorkspacesAPI failed: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/workspaces/home/ensure", nil)
	req.RemoteAddr = "198.51.100.7:1234"
	req.Header.Set("X-LeafWiki-Original-Method", http.MethodGet)
	req.Header.Set("X-LeafWiki-Original-Path", "/spoofed")
	req.Header.Set("X-LeafWiki-Original-Remote-Addr", "127.0.0.1:1")

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
	}
	if seen.method != http.MethodPost || seen.path != "/api/workspaces/home/ensure" || seen.remote != "198.51.100.7:1234" {
		t.Fatalf("original headers = method %q path %q remote %q", seen.method, seen.path, seen.remote)
	}

})

var _ = It("TestWorkspacesAPIRejectsUnsupportedWorkspaceAPIShape", func() {
	t := GinkgoT()
	handler, err := NewWorkspacesAPI("http://127.0.0.1:1", "daemon-token")
	if err != nil {
		t.Fatalf("NewWorkspacesAPI failed: %v", err)
	}

	req := httptest.NewRequest(http.MethodDelete, "/api/workspaces/home", strings.NewReader(""))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404: %s", rec.Code, rec.Body.String())
	}

})
