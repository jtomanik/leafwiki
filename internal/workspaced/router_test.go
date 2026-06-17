package workspaced

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/perber/wiki/internal/core/assets"
	httpinternal "github.com/perber/wiki/internal/http"
	"github.com/perber/wiki/internal/wiki"
)

func TestRouterExcludesPublicIdentityAndGlobalRoutes(t *testing.T) {
	w := newTestWiki(t)
	defer w.Close()
	router := NewRouter(w, httpinternal.RouterOptions{
		PublicAccess:            true,
		AllowInsecure:           true,
		AuthDisabled:            true,
		AccessTokenTimeout:      15 * time.Minute,
		RefreshTokenTimeout:     7 * 24 * time.Hour,
		MaxAssetUploadSizeBytes: assets.DefaultMaxUploadSizeBytes,
	})

	for _, tc := range []struct {
		name   string
		method string
		path   string
	}{
		{name: "config", method: http.MethodGet, path: "/api/config"},
		{name: "auth me", method: http.MethodGet, path: "/api/auth/me"},
		{name: "login", method: http.MethodPost, path: "/api/auth/login"},
		{name: "users", method: http.MethodGet, path: "/api/users"},
		{name: "branding", method: http.MethodGet, path: "/api/branding"},
		{name: "oauth token", method: http.MethodPost, path: "/oauth/token"},
		{name: "oauth metadata", method: http.MethodGet, path: "/.well-known/oauth-protected-resource/mcp"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec := request(router, tc.method, tc.path)
			if rec.Code != http.StatusNotFound {
				t.Fatalf("%s %s status = %d, want 404: %s", tc.method, tc.path, rec.Code, rec.Body.String())
			}
		})
	}

	rec := request(router, http.MethodGet, "/api/tree")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /api/tree status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
}

func TestRoutersDoNotExposeEmbeddedFrontendRoutes(t *testing.T) {
	embedFrontendOrig := httpinternal.EmbedFrontend
	httpinternal.EmbedFrontend = "true"
	t.Cleanup(func() {
		httpinternal.EmbedFrontend = embedFrontendOrig
	})
	stylesheetDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(stylesheetDir, "custom.css"), []byte("body { color: red; }\n"), 0o644); err != nil {
		t.Fatalf("write custom stylesheet: %v", err)
	}
	t.Chdir(stylesheetDir)

	routerOptions := func() httpinternal.RouterOptions {
		opts := workspacedRouterOptions()
		opts.CustomStylesheet = "custom.css"
		return opts
	}

	for _, tc := range []struct {
		name   string
		router func(*wiki.Wiki) http.Handler
	}{
		{
			name: "workspaced",
			router: func(w *wiki.Wiki) http.Handler {
				return NewRouter(w, routerOptions())
			},
		},
		{
			name: "authenticated workspaced",
			router: func(w *wiki.Wiki) http.Handler {
				return NewAuthenticatedRouter(w, routerOptions(), PrivateAuthOptions{
					DaemonToken: "private-token",
					WorkspaceID: "current",
					Now:         func() time.Time { return time.Date(2026, 6, 16, 12, 0, 0, 0, time.UTC) },
				})
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := newTestWiki(t)
			defer w.Close()
			router := tc.router(w)

			for _, path := range []string{
				"/custom.css",
				"/favicon.svg",
				"/static/index-DYW7NERi.js",
				"/workspace-spa-route",
			} {
				rec := request(router, http.MethodGet, path)
				if rec.Code != http.StatusNotFound {
					t.Fatalf("GET %s status = %d, want 404 route absence: %s", path, rec.Code, rec.Body.String())
				}
			}
		})
	}
}

func newTestWiki(t *testing.T) *wiki.Wiki {
	t.Helper()
	w, err := wiki.NewWiki(&wiki.WikiOptions{
		StorageDir:          t.TempDir(),
		AdminPassword:       "admin",
		JWTSecret:           "secret",
		AccessTokenTimeout:  15 * time.Minute,
		RefreshTokenTimeout: 7 * 24 * time.Hour,
		AuthDisabled:        true,
	})
	if err != nil {
		t.Fatalf("NewWiki failed: %v", err)
	}
	return w
}

func workspacedRouterOptions() httpinternal.RouterOptions {
	return httpinternal.RouterOptions{
		PublicAccess:            true,
		AllowInsecure:           true,
		AuthDisabled:            true,
		AccessTokenTimeout:      15 * time.Minute,
		RefreshTokenTimeout:     7 * 24 * time.Hour,
		MaxAssetUploadSizeBytes: assets.DefaultMaxUploadSizeBytes,
	}
}

func request(router http.Handler, method, path string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(""))
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec
}
