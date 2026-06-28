package frontd

import (
	. "github.com/onsi/ginkgo/v2"
	"net/http"
	"net/http/httptest"
	"strings"
	"time"

	"github.com/perber/wiki/internal/core/assets"
	httpinternal "github.com/perber/wiki/internal/http"
	"github.com/perber/wiki/internal/wiki"
)

type publicRuntimeRouteCase struct {
	method     string
	path       string
	wantStatus int
}

var _ = DescribeTable("TestRouterServesPublicRuntimeRoutes",
	func(tc publicRuntimeRouteCase) {
	t := GinkgoT()
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

	req := httptest.NewRequest(tc.method, tc.path, nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != tc.wantStatus {
		t.Fatalf("%s %s status = %d, want %d: %s", tc.method, tc.path, rec.Code, tc.wantStatus, rec.Body.String())
	}
},
	Entry("config", publicRuntimeRouteCase{method: http.MethodGet, path: "/api/config", wantStatus: http.StatusOK}),
	Entry("me", publicRuntimeRouteCase{method: http.MethodGet, path: "/api/auth/me", wantStatus: http.StatusOK}),
	Entry("branding", publicRuntimeRouteCase{method: http.MethodGet, path: "/api/branding", wantStatus: http.StatusOK}),
)

func newTestWiki(t frontdTestTB) *wiki.Wiki {
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
