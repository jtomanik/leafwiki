package importer

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	httpinternal "github.com/perber/wiki/internal/http"
)

func TestRoutesRequireAuthForImportPlanReads(t *testing.T) {
	router := httpinternal.NewRouter(
		[]httpinternal.RouteRegistrar{NewRoutes(RoutesConfig{})},
		httpinternal.FrontendConfig{},
		httpinternal.RouterOptions{DisableFrontendRoutes: true},
	)

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/import/plan", nil))

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401: %s", rec.Code, rec.Body.String())
	}
}

func TestRoutesRequireCSRFForImportPlanMutations(t *testing.T) {
	router := httpinternal.NewRouter(
		[]httpinternal.RouteRegistrar{NewRoutes(RoutesConfig{})},
		httpinternal.FrontendConfig{},
		httpinternal.RouterOptions{
			AuthDisabled:          true,
			DisableFrontendRoutes: true,
		},
	)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/import/plan", strings.NewReader(""))
	req.Header.Set("Content-Type", "multipart/form-data; boundary=missing")
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403: %s", rec.Code, rec.Body.String())
	}
}
