package frontd

import (
	"encoding/json"
	"errors"
	. "github.com/onsi/ginkgo/v2"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"time"

	"github.com/perber/wiki/internal/projectdaemon"
	"github.com/perber/wiki/internal/workspaceid"
)

var _ = It("TestWorkspaceRouterProxyResolvesWorkspaceAndRewritesAPIPath", func() {
	t := GinkgoT()
	now := time.Date(2026, 6, 17, 12, 0, 0, 0, time.UTC)
	var seen struct {
		path         string
		query        string
		token        string
		actorContext string
		body         string
	}
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		seen.path = req.URL.Path
		seen.query = req.URL.RawQuery
		seen.token = req.Header.Get(projectdaemon.ControlTokenHeader)
		seen.actorContext = req.Header.Get(projectdaemon.ActorContextHeader)
		raw, _ := io.ReadAll(req.Body)
		seen.body = string(raw)
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte("proxied"))
	}))
	DeferCleanup(upstream.Close)

	proxy := NewWorkspaceRouterProxy(WorkspaceRouterProxyOptions{
		Resolve: func(*http.Request, workspaceid.WorkspaceID) (WorkspaceRoute, error) {
			return WorkspaceRoute{WorkspaceID: workspaceid.WorkspaceID("home"), Upstream: upstream.URL, DaemonToken: "private-token"}, nil
		},
		Actor: func(_ *http.Request, workspaceID workspaceid.WorkspaceID) (projectdaemon.ActorContext, error) {
			return projectdaemon.ActorContext{
				Version:     1,
				Issuer:      projectdaemon.ActorContextIssuerWikid,
				Subject:     "user:admin",
				Role:        "admin",
				WorkspaceID: workspaceID,
				AuthMethod:  "cookie",
				IssuedAt:    now,
				ExpiresAt:   now.Add(5 * time.Minute),
			}, nil
		},
	})

	req := httptest.NewRequest(http.MethodPost, "/api/workspaces/home/tree?depth=1", strings.NewReader("body"))
	req.Header.Set("Authorization", "Bearer public")
	req.Header.Set("Cookie", "leafwiki_at=public")
	rec := httptest.NewRecorder()
	proxy.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated || rec.Body.String() != "proxied" {
		t.Fatalf("response = %d %q", rec.Code, rec.Body.String())
	}
	if seen.path != "/api/tree" || seen.query != "depth=1" || seen.body != "body" {
		t.Fatalf("forwarded request = %#v", seen)
	}
	if seen.token != "private-token" {
		t.Fatalf("daemon token = %q", seen.token)
	}
	decoded, err := projectdaemon.DecodeActorContext(seen.actorContext, projectdaemon.ActorContextValidation{Now: now.Add(time.Minute), WorkspaceID: "home"})
	if err != nil {
		t.Fatalf("actor context invalid: %v", err)
	}
	if decoded.Subject != "user:admin" {
		t.Fatalf("actor = %#v", decoded)
	}

})

var _ = It("TestWorkspaceRouterProxyRewritesWorkspaceAssetPathsToStaticAssetRoute", func() {
	t := GinkgoT()
	var seenPath string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		seenPath = req.URL.Path
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("asset"))
	}))
	DeferCleanup(upstream.Close)

	proxy := NewWorkspaceRouterProxy(WorkspaceRouterProxyOptions{
		Resolve: func(*http.Request, workspaceid.WorkspaceID) (WorkspaceRoute, error) {
			return WorkspaceRoute{WorkspaceID: workspaceid.WorkspaceID("docs"), Upstream: upstream.URL, DaemonToken: "private-token"}, nil
		},
		Actor: func(_ *http.Request, workspaceID workspaceid.WorkspaceID) (projectdaemon.ActorContext, error) {
			return projectdaemon.ActorContext{
				Version:     1,
				Issuer:      projectdaemon.ActorContextIssuerWikid,
				Subject:     "user:admin",
				Role:        "admin",
				WorkspaceID: workspaceID,
				AuthMethod:  "cookie",
				IssuedAt:    time.Now().UTC(),
				ExpiresAt:   time.Now().UTC().Add(5 * time.Minute),
			}, nil
		},
	})

	rec := httptest.NewRecorder()
	proxy.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/workspaces/docs/assets/page-id/upload-test.png", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("response = %d %q", rec.Code, rec.Body.String())
	}
	if seenPath != "/assets/page-id/upload-test.png" {
		t.Fatalf("forwarded path = %q, want static asset route", seenPath)
	}

})

type workspaceRouterProxyResolverErrorCase struct {
	err           error
	wantStatus    int
	wantCode      string
	wantMessageID string
}

var _ = DescribeTable("TestWorkspaceRouterProxyMapsResolverErrors",
	func(tt workspaceRouterProxyResolverErrorCase) {
		t := GinkgoT()
		proxy := NewWorkspaceRouterProxy(WorkspaceRouterProxyOptions{
			Resolve: func(*http.Request, workspaceid.WorkspaceID) (WorkspaceRoute, error) {
				return WorkspaceRoute{}, tt.err
			},
			Actor: func(*http.Request, workspaceid.WorkspaceID) (projectdaemon.ActorContext, error) {
				return projectdaemon.ActorContext{}, nil
			},
		})
		rec := httptest.NewRecorder()
		proxy.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/workspaces/home/tree", nil))
		if rec.Code != tt.wantStatus {
			t.Fatalf("status = %d, want %d: %s", rec.Code, tt.wantStatus, rec.Body.String())
		}
		assertStructuredFrontdError(t, rec, tt.wantCode, tt.wantMessageID)
	},
	Entry("not found", workspaceRouterProxyResolverErrorCase{err: ErrWorkspaceNotFound, wantStatus: http.StatusNotFound, wantCode: "workspace_not_found", wantMessageID: "errors.workspace.not_found"}),
	Entry("forbidden", workspaceRouterProxyResolverErrorCase{err: ErrWorkspaceForbidden, wantStatus: http.StatusForbidden, wantCode: "workspace_forbidden", wantMessageID: "errors.workspace.forbidden"}),
)

var _ = It("TestWorkspaceRouterProxyMapsResolverErrors unavailable", func() {
	t := GinkgoT()
	proxy := NewWorkspaceRouterProxy(WorkspaceRouterProxyOptions{
		Resolve: func(*http.Request, workspaceid.WorkspaceID) (WorkspaceRoute, error) {
			return WorkspaceRoute{}, errors.New("boom")
		},
		Actor: func(*http.Request, workspaceid.WorkspaceID) (projectdaemon.ActorContext, error) {
			return projectdaemon.ActorContext{}, nil
		},
	})
	rec := httptest.NewRecorder()
	proxy.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/workspaces/home/tree", nil))
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503: %s", rec.Code, rec.Body.String())
	}
	assertStructuredFrontdError(t, rec, "workspace_unavailable", "errors.workspace.unavailable")

})

type workspaceRouterProxyDependencyErrorCase struct {
	opts          WorkspaceRouterProxyOptions
	wantCode      string
	wantMessageID string
}

var _ = DescribeTable("TestWorkspaceRouterProxyReportsStructuredDependencyErrors",
	func(tt workspaceRouterProxyDependencyErrorCase) {
		t := GinkgoT()
		proxy := NewWorkspaceRouterProxy(tt.opts)
		rec := httptest.NewRecorder()
		proxy.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/workspaces/home/tree", nil))
		if rec.Code != http.StatusServiceUnavailable {
			t.Fatalf("status = %d, want 503: %s", rec.Code, rec.Body.String())
		}
		assertStructuredFrontdError(t, rec, tt.wantCode, tt.wantMessageID)
	},
	Entry("resolver unavailable", workspaceRouterProxyDependencyErrorCase{
		opts:          WorkspaceRouterProxyOptions{},
		wantCode:      "workspace_resolver_unavailable",
		wantMessageID: "errors.workspace.resolver_unavailable",
	}),
	Entry("actor resolver unavailable", workspaceRouterProxyDependencyErrorCase{
		opts: WorkspaceRouterProxyOptions{
			Resolve: func(*http.Request, workspaceid.WorkspaceID) (WorkspaceRoute, error) {
				return WorkspaceRoute{WorkspaceID: "home", Upstream: "http://127.0.0.1:1", DaemonToken: "token"}, nil
			},
		},
		wantCode:      "workspace_actor_context_unavailable",
		wantMessageID: "errors.workspace.actor_context_unavailable",
	}),
)

func assertStructuredFrontdError(t frontdTestTB, rec *httptest.ResponseRecorder, code string, messageID string) {
	t.Helper()
	var body struct {
		Error struct {
			Code      string `json:"code"`
			MessageID string `json:"messageId"`
			Message   string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode structured error: %v; body=%s", err, rec.Body.String())
	}
	if body.Error.Code != code || body.Error.MessageID != messageID || body.Error.Message == "" {
		t.Fatalf("structured error = %#v, want code=%q messageId=%q with message", body.Error, code, messageID)
	}
}
