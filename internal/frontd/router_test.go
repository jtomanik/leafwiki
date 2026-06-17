package frontd

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/perber/wiki/internal/core/assets"
	httpinternal "github.com/perber/wiki/internal/http"
	"github.com/perber/wiki/internal/wiki"
)

func TestRouterServesPublicRuntimeRoutes(t *testing.T) {
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
		name       string
		method     string
		path       string
		wantStatus int
	}{
		{name: "config", method: http.MethodGet, path: "/api/config", wantStatus: http.StatusOK},
		{name: "me", method: http.MethodGet, path: "/api/auth/me", wantStatus: http.StatusOK},
		{name: "branding", method: http.MethodGet, path: "/api/branding", wantStatus: http.StatusOK},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, tc.path, nil)
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)
			if rec.Code != tc.wantStatus {
				t.Fatalf("%s %s status = %d, want %d: %s", tc.method, tc.path, rec.Code, tc.wantStatus, rec.Body.String())
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

func request(router http.Handler, method, path string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(""))
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec
}
