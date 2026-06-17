package health

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	httpinternal "github.com/perber/wiki/internal/http"
	"github.com/perber/wiki/internal/projectdaemon"
)

func TestRoutesDegradesWhenRequiredRuntimeRoleCrashed(t *testing.T) {
	routes := NewRoutes(RoutesConfig{
		StorageDir: t.TempDir(),
		RequiredRoles: []projectdaemon.RoleName{
			projectdaemon.RoleWikid,
			projectdaemon.RoleFrontd,
			projectdaemon.RoleWorkspaced,
		},
		RoleHealth: func() []projectdaemon.RoleHealth {
			now := time.Date(2026, 6, 16, 12, 0, 0, 0, time.UTC)
			return []projectdaemon.RoleHealth{
				{Name: projectdaemon.RoleWikid, State: projectdaemon.RoleStateReady, PID: 1, UpdatedAt: now},
				{Name: projectdaemon.RoleFrontd, State: projectdaemon.RoleStateReady, PID: 2, UpdatedAt: now},
				{Name: projectdaemon.RoleWorkspaced, State: projectdaemon.RoleStateCrashed, PID: 3, Error: "restart exhausted", UpdatedAt: now},
			}
		},
	})
	router := httpinternal.NewRouter([]httpinternal.RouteRegistrar{routes}, httpinternal.FrontendConfig{}, httpinternal.RouterOptions{})

	req := httptest.NewRequest(http.MethodGet, "/api/health", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("GET /api/health status = %d, want 503: %s", rec.Code, rec.Body.String())
	}
	var body struct {
		Status string            `json:"status"`
		Checks map[string]string `json:"checks"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode health response: %v", err)
	}
	if body.Status != "degraded" {
		t.Fatalf("status = %q, want degraded", body.Status)
	}
	if body.Checks["role_workspaced"] != "crashed" {
		t.Fatalf("role_workspaced check = %q, want crashed; checks=%#v", body.Checks["role_workspaced"], body.Checks)
	}
}
