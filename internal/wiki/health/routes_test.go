package health

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"time"

	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/gstruct"
	httpinternal "github.com/perber/wiki/internal/http"
	"github.com/perber/wiki/internal/projectdaemon"
	"github.com/perber/wiki/internal/search"
)

var _ = ginkgo.Describe("health routes", func() {
	ginkgo.It("reports degraded health when a required runtime role has crashed", func() {
		routes := NewRoutes(RoutesConfig{
			StorageDir: healthTempDir(),
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

		rec := performHealthRequest(router)

		Expect(rec).To(HaveHTTPStatus(http.StatusServiceUnavailable), rec.Body.String())
		body := decodeHealthResponse(rec)
		Expect(body).To(gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
			"Status": Equal(healthStatusDegraded),
			"Checks": HaveKeyWithValue(healthCheckRoleWorkspaced.String(), healthStatusCrashed),
		}))
	})

	ginkgo.It("returns ok when storage exists and no failing dependencies are configured", func() {
		routes := NewRoutes(RoutesConfig{StorageDir: healthTempDir()})
		router := httpinternal.NewRouter([]httpinternal.RouteRegistrar{routes}, httpinternal.FrontendConfig{}, httpinternal.RouterOptions{})

		rec := performHealthRequest(router)

		Expect(rec).To(HaveHTTPStatus(http.StatusOK), rec.Body.String())
		body := decodeHealthResponse(rec)
		Expect(body).To(gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
			"Status": Equal(healthStatusOK),
			"Checks": SatisfyAll(
				HaveKeyWithValue(healthCheckDataDir.String(), healthStatusOK),
				HaveKeyWithValue(healthCheckSearch.String(), healthStatusNotApplicable),
				HaveKeyWithValue(healthCheckSQLite.String(), healthStatusNotApplicable),
			),
		}))
	})
})

var _ = ginkgo.Describe("health evaluation", func() {
	ginkgo.It("marks missing or non-directory storage as failed", func() {
		missing := filepath.Join(healthTempDir(), "missing")
		healthy, checks := NewHealthUseCase(nil, nil, missing).Execute()
		Expect(healthy).To(BeFalse())
		Expect(checks).To(HaveKeyWithValue(healthCheckDataDir, healthStatusFailed))

		filePath := filepath.Join(healthTempDir(), "not-a-dir")
		Expect(os.WriteFile(filePath, []byte("x"), 0o600)).To(Succeed())
		healthy, checks = NewHealthUseCase(nil, nil, filePath).Execute()
		Expect(healthy).To(BeFalse())
		Expect(checks).To(HaveKeyWithValue(healthCheckDataDir, healthStatusFailed))
	})

	ginkgo.It("marks failed indexing status as unhealthy", func() {
		status := search.NewIndexingStatus()
		status.Start()
		status.Fail()
		status.Finish()

		healthy, checks := NewHealthUseCase(nil, status, healthTempDir()).Execute()

		Expect(healthy).To(BeFalse())
		Expect(checks).To(HaveKeyWithValue(healthCheckSearch, healthStatusFailed))
	})

	ginkgo.It("updates required role checks through SetRoleHealth", func() {
		uc := NewHealthUseCase(nil, nil, healthTempDir())
		healthy, checks := uc.Execute()
		Expect(healthy).To(BeTrue())
		Expect(checks).NotTo(HaveKey(healthCheckRoleWikid))

		uc.SetRoleHealth([]projectdaemon.RoleName{projectdaemon.RoleWikid}, func() []projectdaemon.RoleHealth {
			return []projectdaemon.RoleHealth{{Name: projectdaemon.RoleWikid, State: projectdaemon.RoleStateCrashed}}
		})
		healthy, checks = uc.Execute()

		Expect(healthy).To(BeFalse())
		Expect(checks).To(HaveKeyWithValue(healthCheckRoleWikid, healthStatusCrashed))
	})

	ginkgo.It("reports ready and active indexing states", func() {
		status := search.NewIndexingStatus()
		status.Start()
		status.Success()
		status.Finish()

		healthy, checks := NewHealthUseCase(nil, status, healthTempDir()).Execute()
		Expect(healthy).To(BeTrue())
		Expect(checks).To(HaveKeyWithValue(healthCheckSearch, healthStatusOK))

		status.Start()
		healthy, checks = NewHealthUseCase(nil, status, healthTempDir()).Execute()
		Expect(healthy).To(BeTrue())
		Expect(checks).To(HaveKeyWithValue(healthCheckSearch, healthStatusIndexing))
	})

	ginkgo.It("reports sqlite health for configured indexes and legacy constructor", func() {
		index, err := search.NewSQLiteIndex(healthTempDir())
		Expect(err).NotTo(HaveOccurred())
		ginkgo.DeferCleanup(func() {
			Expect(index.Close()).To(Succeed())
		})

		healthy, checks := NewLegacyHealthUseCase(index, nil, healthTempDir()).Execute()

		Expect(healthy).To(BeTrue())
		Expect(checks).To(HaveKeyWithValue(healthCheckSQLite, healthStatusOK))
		Expect(checks).To(HaveKeyWithValue(healthCheckSearch, healthStatusNotApplicable))
	})

	ginkgo.It("reports sqlite failure when a configured index cannot reopen its database", func() {
		indexStorage := healthTempDir()
		index, err := search.NewSQLiteIndex(indexStorage)
		Expect(err).NotTo(HaveOccurred())
		Expect(index.Close()).To(Succeed())
		Expect(os.RemoveAll(indexStorage)).To(Succeed())

		healthy, checks := NewHealthUseCase(index, nil, healthTempDir()).Execute()

		Expect(healthy).To(BeFalse())
		Expect(checks).To(HaveKeyWithValue(healthCheckSQLite, healthStatusFailed))
	})

	ginkgo.It("routes SetRoleHealth updates the health endpoint checks", func() {
		routes := NewRoutes(RoutesConfig{StorageDir: healthTempDir()})
		routes.SetRoleHealth([]projectdaemon.RoleName{projectdaemon.RoleWikid}, func() []projectdaemon.RoleHealth {
			return []projectdaemon.RoleHealth{{Name: projectdaemon.RoleWikid, State: projectdaemon.RoleStateCrashed}}
		})
		router := httpinternal.NewRouter([]httpinternal.RouteRegistrar{routes}, httpinternal.FrontendConfig{}, httpinternal.RouterOptions{})

		rec := performHealthRequest(router)

		Expect(rec).To(HaveHTTPStatus(http.StatusServiceUnavailable), rec.Body.String())
		body := decodeHealthResponse(rec)
		Expect(body.Checks).To(HaveKeyWithValue(healthCheckRoleWikid.String(), healthStatusCrashed))
	})
})

var _ = ginkgo.Describe("required role checks", func() {
	ginkgo.It("reports ready, missing, unknown, and non-ready role states", func() {
		checks, healthy := requiredRoleChecks(
			[]projectdaemon.RoleName{
				projectdaemon.RoleWikid,
				projectdaemon.RoleFrontd,
				projectdaemon.RoleWorkspaced,
				projectdaemon.RoleName("custom"),
			},
			[]projectdaemon.RoleHealth{
				{Name: projectdaemon.RoleWikid, State: projectdaemon.RoleStateReady},
				{Name: projectdaemon.RoleFrontd},
				{Name: projectdaemon.RoleWorkspaced, State: projectdaemon.RoleStateStarting},
			},
		)

		Expect(healthy).To(BeFalse())
		Expect(checks).To(Equal(healthChecks{
			healthCheckRoleWikid:      healthStatusOK,
			healthCheckRoleFrontd:     healthStatusUnknown,
			healthCheckRoleWorkspaced: healthStatusStarting,
			healthCheckRoleUnknown:    healthStatusMissing,
		}))
	})

	ginkgo.It("reports healthy when all required roles are ready", func() {
		checks, healthy := requiredRoleChecks(
			[]projectdaemon.RoleName{projectdaemon.RoleWikid},
			[]projectdaemon.RoleHealth{{Name: projectdaemon.RoleWikid, State: projectdaemon.RoleStateReady}},
		)

		Expect(healthy).To(BeTrue())
		Expect(checks).To(Equal(healthChecks{healthCheckRoleWikid: healthStatusOK}))
	})
})

func performHealthRequest(router http.Handler) *httptest.ResponseRecorder {
	ginkgo.GinkgoHelper()
	req := httptest.NewRequest(http.MethodGet, "/api/health", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec
}

func decodeHealthResponse(rec *httptest.ResponseRecorder) struct {
	Status healthStatus            `json:"status"`
	Checks map[string]healthStatus `json:"checks"`
} {
	ginkgo.GinkgoHelper()
	var body struct {
		Status healthStatus            `json:"status"`
		Checks map[string]healthStatus `json:"checks"`
	}
	Expect(json.Unmarshal(rec.Body.Bytes(), &body)).To(Succeed(), rec.Body.String())
	return body
}

func healthTempDir() string {
	ginkgo.GinkgoHelper()
	dir, err := os.MkdirTemp("", "leafwiki-health-*")
	Expect(err).NotTo(HaveOccurred())
	ginkgo.DeferCleanup(os.RemoveAll, dir)
	return dir
}
