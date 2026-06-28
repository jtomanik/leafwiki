package frontd

import (
	"errors"
	. "github.com/onsi/ginkgo/v2"
	"net/http"
	"net/http/httptest"

	"github.com/perber/wiki/internal/projectdaemon"
	"github.com/perber/wiki/internal/workspaceid"
)

var _ = It("TestWikidWorkspaceResolverEnsuresWorkspaceAndReturnsRoute", func() {
	t := GinkgoT()
	var seen struct {
		path         string
		token        string
		auth         string
		originalPath string
	}
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		seen.path = req.URL.Path
		seen.token = req.Header.Get(projectdaemon.ControlTokenHeader)
		seen.auth = req.Header.Get("Authorization")
		seen.originalPath = req.Header.Get("X-LeafWiki-Original-Path")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"workspace":{"id":"docs"},
			"status":{"workspaceId":"docs","state":"running","url":"http://127.0.0.1:49152"}
		}`))
	}))
	defer upstream.Close()

	resolve, err := NewWikidWorkspaceResolver(upstream.URL, "daemon-token")
	if err != nil {
		t.Fatalf("NewWikidWorkspaceResolver failed: %v", err)
	}
	req := httptest.NewRequest(http.MethodGet, "/api/workspaces/docs/tree", nil)
	req.Header.Set("Authorization", "Bearer public-token")

	route, err := resolve(req, workspaceid.WorkspaceID("docs"))
	if err != nil {
		t.Fatalf("resolve failed: %v", err)
	}

	if seen.path != "/__leafwiki/workspaces/docs/ensure" {
		t.Fatalf("ensure path = %q", seen.path)
	}
	if seen.token != "daemon-token" || seen.auth != "Bearer public-token" || seen.originalPath != "/api/workspaces/docs/tree" {
		t.Fatalf("forwarded headers = token %q auth %q originalPath %q", seen.token, seen.auth, seen.originalPath)
	}
	if route.WorkspaceID != workspaceid.WorkspaceID("docs") || route.Upstream != "http://127.0.0.1:49152" || route.PrivateMCPURL != "http://127.0.0.1:49152/mcp" {
		t.Fatalf("route = %#v", route)
	}

})

var _ = It("TestWikidWorkspaceResolverRejectsInvalidWorkspaceIDBeforeEnsure", func() {
	t := GinkgoT()
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		t.Fatalf("invalid workspace ID unexpectedly reached wikid: %s", req.URL.Path)
	}))
	defer upstream.Close()

	resolve, err := NewWikidWorkspaceResolver(upstream.URL, "daemon-token")
	if err != nil {
		t.Fatalf("NewWikidWorkspaceResolver failed: %v", err)
	}

	_, err = resolve(httptest.NewRequest(http.MethodGet, "/api/workspaces/%20docs/tree", nil), workspaceid.WorkspaceID(" docs"))
	if !errors.Is(err, ErrWorkspaceNotFound) {
		t.Fatalf("resolve error = %v, want ErrWorkspaceNotFound", err)
	}

})

var _ = It("TestWikidSingleWorkspaceResolverPreservesOriginalMCPPath", func() {
	t := GinkgoT()
	var seen struct {
		path         string
		originalPath string
	}
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		seen.path = req.URL.Path
		seen.originalPath = req.Header.Get("X-LeafWiki-Original-Path")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"workspaces":[{"id":"home"}]}`))
	}))
	defer upstream.Close()

	resolve, err := NewWikidSingleWorkspaceResolver(upstream.URL, "daemon-token")
	if err != nil {
		t.Fatalf("NewWikidSingleWorkspaceResolver failed: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/mcp", nil)

	workspaceID, err := resolve(req)
	if err != nil {
		t.Fatalf("resolve failed: %v", err)
	}

	if workspaceID != workspaceid.WorkspaceID("home") {
		t.Fatalf("workspaceID = %q, want home", workspaceID)
	}
	if seen.path != "/__leafwiki/workspaces" || seen.originalPath != "/mcp" {
		t.Fatalf("resolver request path/original = %q/%q", seen.path, seen.originalPath)
	}

})

var _ = It("TestWikidSingleWorkspaceResolverRejectsAmbiguousWorkspaceList", func() {
	t := GinkgoT()
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"workspaces":[{"id":"home"},{"id":"docs"}]}`))
	}))
	defer upstream.Close()

	resolve, err := NewWikidSingleWorkspaceResolver(upstream.URL, "daemon-token")
	if err != nil {
		t.Fatalf("NewWikidSingleWorkspaceResolver failed: %v", err)
	}

	_, err = resolve(httptest.NewRequest(http.MethodPost, "/mcp", nil))
	if err != ErrWorkspaceAmbiguous {
		t.Fatalf("err = %v, want %v", err, ErrWorkspaceAmbiguous)
	}

})

type workspaceResolverAccessErrorCase struct {
	code int
	want error
}

var _ = DescribeTable("TestWikidWorkspaceResolverMapsAccessErrors",
	func(tt workspaceResolverAccessErrorCase) {
	t := GinkgoT()
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		w.WriteHeader(tt.code)
	}))
	defer upstream.Close()
	resolve, err := NewWikidWorkspaceResolver(upstream.URL, "daemon-token")
	if err != nil {
		t.Fatalf("NewWikidWorkspaceResolver failed: %v", err)
	}
	_, err = resolve(httptest.NewRequest(http.MethodGet, "/api/workspaces/docs/tree", nil), workspaceid.WorkspaceID("docs"))
	if err != tt.want {
		t.Fatalf("err = %v, want %v", err, tt.want)
	}
},
	Entry("not found", workspaceResolverAccessErrorCase{code: http.StatusNotFound, want: ErrWorkspaceNotFound}),
	Entry("forbidden", workspaceResolverAccessErrorCase{code: http.StatusForbidden, want: ErrWorkspaceForbidden}),
)
