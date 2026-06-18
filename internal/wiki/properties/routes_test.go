package properties

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	httpinternal "github.com/perber/wiki/internal/http"
	coreprop "github.com/perber/wiki/internal/properties"
)

func TestRoutesPublicAccessExposesPropertyKeysWithoutAuth(t *testing.T) {
	store, err := coreprop.NewPropertiesStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewPropertiesStore: %v", err)
	}
	svc := coreprop.NewPropertiesService(store)
	if err := svc.SetPropertiesForPage("page-1", map[string]coreprop.PropertyEntry{
		"status": {Value: "draft", Type: "text"},
	}); err != nil {
		t.Fatalf("SetPropertiesForPage: %v", err)
	}

	router := httpinternal.NewRouter(
		[]httpinternal.RouteRegistrar{NewRoutes(RoutesConfig{
			GetPropertyKeys: NewGetPropertyKeysUseCase(svc),
		})},
		httpinternal.FrontendConfig{},
		httpinternal.RouterOptions{PublicAccess: true, DisableFrontendRoutes: true},
	)

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/properties?limit=10", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var body []coreprop.PropertyKeyCount
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(body) != 1 || body[0].Key != "status" || body[0].Count != 1 {
		t.Fatalf("body = %#v, want status count", body)
	}
}

func TestRoutesPrivatePropertiesRequireAuth(t *testing.T) {
	router := httpinternal.NewRouter(
		[]httpinternal.RouteRegistrar{NewRoutes(RoutesConfig{})},
		httpinternal.FrontendConfig{},
		httpinternal.RouterOptions{DisableFrontendRoutes: true},
	)

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/properties", nil))

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401: %s", rec.Code, rec.Body.String())
	}
}
