package assets

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	httpinternal "github.com/perber/wiki/internal/http"
)

func TestRoutesServeStaticAssetsWhenPublicAccessEnabled(t *testing.T) {
	assetsDir := t.TempDir()
	pageDir := filepath.Join(assetsDir, "page-1")
	if err := os.MkdirAll(pageDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(filepath.Join(pageDir, "note.txt"), []byte("hello"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	router := httpinternal.NewRouter(
		[]httpinternal.RouteRegistrar{NewRoutes(RoutesConfig{AssetsDir: assetsDir})},
		httpinternal.FrontendConfig{},
		httpinternal.RouterOptions{PublicAccess: true, DisableFrontendRoutes: true},
	)

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/assets/page-1/note.txt", nil))

	if rec.Code != http.StatusOK || rec.Body.String() != "hello" {
		t.Fatalf("status/body = %d/%q, want 200/hello", rec.Code, rec.Body.String())
	}
}

func TestRoutesRequireAuthForPrivateStaticAssets(t *testing.T) {
	assetsDir := t.TempDir()
	router := httpinternal.NewRouter(
		[]httpinternal.RouteRegistrar{NewRoutes(RoutesConfig{AssetsDir: assetsDir})},
		httpinternal.FrontendConfig{},
		httpinternal.RouterOptions{DisableFrontendRoutes: true},
	)

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/assets/page-1/note.txt", nil))

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401: %s", rec.Code, rec.Body.String())
	}
}

func TestRoutesRequireCSRFForAssetMutations(t *testing.T) {
	router := httpinternal.NewRouter(
		[]httpinternal.RouteRegistrar{NewRoutes(RoutesConfig{})},
		httpinternal.FrontendConfig{},
		httpinternal.RouterOptions{
			AuthDisabled:          true,
			DisableFrontendRoutes: true,
		},
	)

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/pages/page-1/assets", nil))

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403: %s", rec.Code, rec.Body.String())
	}
}
