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
	httpinternal "github.com/perber/wiki/internal/http"
	"github.com/perber/wiki/internal/projectdaemon"
	"github.com/perber/wiki/internal/search"
)

var _ = ginkgo.Describe("health routes", func() {
	ginkgo.It("TestRoutesDegradesWhenRequiredRuntimeRoleCrashed", func() {
		routes := NewRoutes(RoutesConfig{
			StorageDir: ginkgo.GinkgoT().TempDir(),
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

		Expect(rec.Code).To(Equal(http.StatusServiceUnavailable), rec.Body.String())
		body := decodeHealthResponse(rec)
		Expect(body.Status).To(Equal("degraded"))
		Expect(body.Checks["role_workspaced"]).To(Equal("crashed"))
	})

	ginkgo.It("returns ok when storage exists and no failing dependencies are configured", func() {
		routes := NewRoutes(RoutesConfig{StorageDir: ginkgo.GinkgoT().TempDir()})
		router := httpinternal.NewRouter([]httpinternal.RouteRegistrar{routes}, httpinternal.FrontendConfig{}, httpinternal.RouterOptions{})

		rec := performHealthRequest(router)

		Expect(rec.Code).To(Equal(http.StatusOK), rec.Body.String())
		body := decodeHealthResponse(rec)
		Expect(body.Status).To(Equal("ok"))
		Expect(body.Checks).To(Equal(map[string]string{
			"data_dir": "ok",
			"search":   "not_applicable",
			"sqlite":   "not_applicable",
		}))
	})
})

var _ = ginkgo.Describe("HealthUseCase", func() {
	ginkgo.It("marks missing or non-directory storage as failed", func() {
		missing := filepath.Join(ginkgo.GinkgoT().TempDir(), "missing")
		healthy, checks := NewHealthUseCase(nil, nil, missing).Execute()
		Expect(healthy).To(BeFalse())
		Expect(checks["data_dir"]).To(Equal("failed"))

		filePath := filepath.Join(ginkgo.GinkgoT().TempDir(), "not-a-dir")
		Expect(os.WriteFile(filePath, []byte("x"), 0o600)).To(Succeed())
		healthy, checks = NewHealthUseCase(nil, nil, filePath).Execute()
		Expect(healthy).To(BeFalse())
		Expect(checks["data_dir"]).To(Equal("failed"))
	})

	ginkgo.It("marks failed indexing status as unhealthy", func() {
		status := search.NewIndexingStatus()
		status.Start()
		status.Fail()
		status.Finish()

		healthy, checks := NewHealthUseCase(nil, status, ginkgo.GinkgoT().TempDir()).Execute()

		Expect(healthy).To(BeFalse())
		Expect(checks["search"]).To(Equal("failed"))
	})

	ginkgo.It("updates required role checks through SetRoleHealth", func() {
		uc := NewHealthUseCase(nil, nil, ginkgo.GinkgoT().TempDir())
		healthy, checks := uc.Execute()
		Expect(healthy).To(BeTrue())
		Expect(checks).NotTo(HaveKey("role_wikid"))

		uc.SetRoleHealth([]projectdaemon.RoleName{projectdaemon.RoleWikid}, func() []projectdaemon.RoleHealth {
			return []projectdaemon.RoleHealth{{Name: projectdaemon.RoleWikid, State: projectdaemon.RoleStateCrashed}}
		})
		healthy, checks = uc.Execute()

		Expect(healthy).To(BeFalse())
		Expect(checks["role_wikid"]).To(Equal("crashed"))
	})

	ginkgo.It("reports ready and active indexing states", func() {
		status := search.NewIndexingStatus()
		status.Start()
		status.Success()
		status.Finish()

		healthy, checks := NewHealthUseCase(nil, status, ginkgo.GinkgoT().TempDir()).Execute()
		Expect(healthy).To(BeTrue())
		Expect(checks["search"]).To(Equal("ok"))

		status.Start()
		healthy, checks = NewHealthUseCase(nil, status, ginkgo.GinkgoT().TempDir()).Execute()
		Expect(healthy).To(BeTrue())
		Expect(checks["search"]).To(Equal("indexing"))
	})

	ginkgo.It("reports sqlite health for configured indexes and legacy constructor", func() {
		index, err := search.NewSQLiteIndex(ginkgo.GinkgoT().TempDir())
		Expect(err).NotTo(HaveOccurred())
		defer index.Close()

		healthy, checks := NewLegacyHealthUseCase(index, nil, ginkgo.GinkgoT().TempDir()).Execute()

		Expect(healthy).To(BeTrue())
		Expect(checks["sqlite"]).To(Equal("ok"))
		Expect(checks["search"]).To(Equal("not_applicable"))
	})

	ginkgo.It("reports sqlite failure when a configured index cannot reopen its database", func() {
		indexStorage := ginkgo.GinkgoT().TempDir()
		index, err := search.NewSQLiteIndex(indexStorage)
		Expect(err).NotTo(HaveOccurred())
		Expect(index.Close()).To(Succeed())
		Expect(os.RemoveAll(indexStorage)).To(Succeed())

		healthy, checks := NewHealthUseCase(index, nil, ginkgo.GinkgoT().TempDir()).Execute()

		Expect(healthy).To(BeFalse())
		Expect(checks["sqlite"]).To(Equal("failed"))
	})

	ginkgo.It("routes SetRoleHealth updates the health endpoint checks", func() {
		routes := NewRoutes(RoutesConfig{StorageDir: ginkgo.GinkgoT().TempDir()})
		routes.SetRoleHealth([]projectdaemon.RoleName{projectdaemon.RoleWikid}, func() []projectdaemon.RoleHealth {
			return []projectdaemon.RoleHealth{{Name: projectdaemon.RoleWikid, State: projectdaemon.RoleStateCrashed}}
		})
		router := httpinternal.NewRouter([]httpinternal.RouteRegistrar{routes}, httpinternal.FrontendConfig{}, httpinternal.RouterOptions{})

		rec := performHealthRequest(router)

		Expect(rec.Code).To(Equal(http.StatusServiceUnavailable), rec.Body.String())
		body := decodeHealthResponse(rec)
		Expect(body.Checks["role_wikid"]).To(Equal("crashed"))
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
		Expect(checks).To(Equal(map[string]string{
			"role_wikid":      "ok",
			"role_frontd":     "unknown",
			"role_workspaced": "starting",
			"role_custom":     "missing",
		}))
	})

	ginkgo.It("reports healthy when all required roles are ready", func() {
		checks, healthy := requiredRoleChecks(
			[]projectdaemon.RoleName{projectdaemon.RoleWikid},
			[]projectdaemon.RoleHealth{{Name: projectdaemon.RoleWikid, State: projectdaemon.RoleStateReady}},
		)

		Expect(healthy).To(BeTrue())
		Expect(checks).To(Equal(map[string]string{"role_wikid": "ok"}))
	})
})

func performHealthRequest(router http.Handler) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, "/api/health", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec
}

func decodeHealthResponse(rec *httptest.ResponseRecorder) struct {
	Status string            `json:"status"`
	Checks map[string]string `json:"checks"`
} {
	var body struct {
		Status string            `json:"status"`
		Checks map[string]string `json:"checks"`
	}
	Expect(json.Unmarshal(rec.Body.Bytes(), &body)).To(Succeed(), rec.Body.String())
	return body
}
