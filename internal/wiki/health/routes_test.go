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
	"github.com/onsi/gomega/gcustom"
	"github.com/onsi/gomega/gstruct"
	"github.com/onsi/gomega/types"
	httpinternal "github.com/perber/wiki/internal/http"
	"github.com/perber/wiki/internal/projectdaemon"
	"github.com/perber/wiki/internal/search"
)

type healthEvaluation struct {
	Healthy bool
	Checks  healthChecks
}

var _ = ginkgo.Describe("health routes", ginkgo.Label("integration"), func() {
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

	ginkgo.DescribeTable("publishes non-ready runtime role states",
		func(state projectdaemon.RoleState, expected healthStatus) {
			routes := NewRoutes(RoutesConfig{
				StorageDir:    healthTempDir(),
				RequiredRoles: []projectdaemon.RoleName{projectdaemon.RoleWikid},
				RoleHealth: func() []projectdaemon.RoleHealth {
					return []projectdaemon.RoleHealth{{Name: projectdaemon.RoleWikid, State: state}}
				},
			})
			router := httpinternal.NewRouter([]httpinternal.RouteRegistrar{routes}, httpinternal.FrontendConfig{}, httpinternal.RouterOptions{})

			rec := performHealthRequest(router)

			Expect(rec).To(HaveHTTPStatus(http.StatusServiceUnavailable), rec.Body.String())
			body := decodeHealthResponse(rec)
			Expect(body).To(gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
				"Status": Equal(healthStatusDegraded),
				"Checks": HaveKeyWithValue(
					healthCheckRoleWikid.String(),
					expected,
				),
			}))
		},
		ginkgo.Entry("starting roles are reported as starting", projectdaemon.RoleStateStarting, healthStatusStarting),
		ginkgo.Entry("degraded roles are reported as degraded", projectdaemon.RoleStateDegraded, healthStatusDegraded),
		ginkgo.Entry("restarting roles are reported as restarting", projectdaemon.RoleStateRestarting, healthStatusRestarting),
		ginkgo.Entry("stopped roles are reported as stopped", projectdaemon.RoleStateStopped, healthStatusStopped),
		ginkgo.Entry("unrecognized roles are reported as unknown", newFixtureRoleState("custom"), healthStatusUnknown),
	)
})

var _ = ginkgo.Describe("health evaluation", func() {
	ginkgo.It("marks missing or non-directory storage as failed", ginkgo.Label("unit"), func() {
		missing := filepath.Join(healthTempDir(), "missing")
		Expect(evaluateHealth(NewHealthUseCase(nil, nil, missing))).To(reportUnhealthyHealthChecks(
			HaveKeyWithValue(healthCheckDataDir, healthStatusFailed),
		))

		filePath := filepath.Join(healthTempDir(), "not-a-dir")
		Expect(os.WriteFile(filePath, []byte("x"), 0o600)).To(Succeed())
		Expect(evaluateHealth(NewHealthUseCase(nil, nil, filePath))).To(reportUnhealthyHealthChecks(
			HaveKeyWithValue(healthCheckDataDir, healthStatusFailed),
		))
	})

	ginkgo.It("marks failed indexing status as unhealthy", ginkgo.Label("unit"), func() {
		status := search.NewIndexingStatus()
		status.Start()
		status.Fail()
		status.Finish()

		Expect(evaluateHealth(NewHealthUseCase(nil, status, healthTempDir()))).To(reportUnhealthyHealthChecks(
			HaveKeyWithValue(healthCheckSearch, healthStatusFailed),
		))
	})

	ginkgo.It("updates required role checks from runtime role health", ginkgo.Label("unit"), func() {
		uc := NewHealthUseCase(nil, nil, healthTempDir())
		Expect(evaluateHealth(uc)).To(reportHealthyHealthChecks(
			Not(HaveKey(healthCheckRoleWikid)),
		))

		uc.SetRoleHealth([]projectdaemon.RoleName{projectdaemon.RoleWikid}, func() []projectdaemon.RoleHealth {
			return []projectdaemon.RoleHealth{{Name: projectdaemon.RoleWikid, State: projectdaemon.RoleStateCrashed}}
		})

		Expect(evaluateHealth(uc)).To(reportUnhealthyHealthChecks(
			HaveKeyWithValue(healthCheckRoleWikid, healthStatusCrashed),
		))
	})

	ginkgo.It("reports ready and active indexing states", ginkgo.Label("unit"), func() {
		status := search.NewIndexingStatus()
		status.Start()
		status.Success()
		status.Finish()

		Expect(evaluateHealth(NewHealthUseCase(nil, status, healthTempDir()))).To(reportHealthyHealthChecks(
			HaveKeyWithValue(healthCheckSearch, healthStatusOK),
		))

		status.Start()
		Expect(evaluateHealth(NewHealthUseCase(nil, status, healthTempDir()))).To(reportHealthyHealthChecks(
			HaveKeyWithValue(healthCheckSearch, healthStatusIndexing),
		))
	})

	ginkgo.It("reports sqlite health for configured indexes and legacy constructor", ginkgo.Label("integration"), func() {
		index, err := search.NewSQLiteIndex(healthTempDir())
		Expect(err).NotTo(HaveOccurred())
		ginkgo.DeferCleanup(func() {
			Expect(index.Close()).To(Succeed())
		})

		Expect(evaluateHealth(NewLegacyHealthUseCase(index, nil, healthTempDir()))).To(reportHealthyHealthChecks(
			SatisfyAll(
				HaveKeyWithValue(healthCheckSQLite, healthStatusOK),
				HaveKeyWithValue(healthCheckSearch, healthStatusNotApplicable),
			),
		))
	})

	ginkgo.It("reports sqlite failure when a configured index cannot reopen its database", ginkgo.Label("integration"), func() {
		indexStorage := healthTempDir()
		index, err := search.NewSQLiteIndex(indexStorage)
		Expect(err).NotTo(HaveOccurred())
		Expect(index.Close()).To(Succeed())
		Expect(os.RemoveAll(indexStorage)).To(Succeed())

		Expect(evaluateHealth(NewHealthUseCase(index, nil, healthTempDir()))).To(reportUnhealthyHealthChecks(
			HaveKeyWithValue(healthCheckSQLite, healthStatusFailed),
		))
	})

	ginkgo.It("publishes updated role health checks through the health endpoint", ginkgo.Label("integration"), func() {
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

var _ = ginkgo.Describe("required role checks", ginkgo.Label("unit"), func() {
	ginkgo.It("reports ready, missing, unknown, and non-ready role states", func() {
		Expect(evaluateRequiredRoles(
			[]projectdaemon.RoleName{
				projectdaemon.RoleWikid,
				projectdaemon.RoleFrontd,
				projectdaemon.RoleWorkspaced,
				newFixtureRoleName("custom"),
			},
			[]projectdaemon.RoleHealth{
				{Name: projectdaemon.RoleWikid, State: projectdaemon.RoleStateReady},
				{Name: projectdaemon.RoleFrontd},
				{Name: projectdaemon.RoleWorkspaced, State: projectdaemon.RoleStateStarting},
			},
		)).To(reportUnhealthyHealthChecks(Equal(healthChecks{
			healthCheckRoleWikid:      healthStatusOK,
			healthCheckRoleFrontd:     healthStatusUnknown,
			healthCheckRoleWorkspaced: healthStatusStarting,
			healthCheckRoleUnknown:    healthStatusMissing,
		})))
	})

	ginkgo.It("reports healthy when all required roles are ready", func() {
		Expect(evaluateRequiredRoles(
			[]projectdaemon.RoleName{projectdaemon.RoleWikid},
			[]projectdaemon.RoleHealth{{Name: projectdaemon.RoleWikid, State: projectdaemon.RoleStateReady}},
		)).To(reportHealthyHealthChecks(Equal(healthChecks{healthCheckRoleWikid: healthStatusOK})))
	})
})

func evaluateHealth(uc *HealthUseCase) healthEvaluation {
	ginkgo.GinkgoHelper()
	healthy, checks := uc.Execute()
	return healthEvaluation{Healthy: healthy, Checks: checks}
}

func evaluateRequiredRoles(required []projectdaemon.RoleName, roles []projectdaemon.RoleHealth) healthEvaluation {
	ginkgo.GinkgoHelper()
	checks, healthy := requiredRoleChecks(required, roles)
	return healthEvaluation{Healthy: healthy, Checks: checks}
}

func newFixtureRoleName[T ~string](raw T) projectdaemon.RoleName {
	return projectdaemon.RoleName(raw)
}

func reportHealthyHealthChecks(checks types.GomegaMatcher) types.GomegaMatcher {
	ginkgo.GinkgoHelper()
	return gcustom.MakeMatcher(func(actual healthEvaluation) (bool, error) {
		if !actual.Healthy {
			return false, nil
		}
		return checks.Match(actual.Checks)
	}).WithMessage("report healthy health checks")
}

func reportUnhealthyHealthChecks(checks types.GomegaMatcher) types.GomegaMatcher {
	ginkgo.GinkgoHelper()
	return gcustom.MakeMatcher(func(actual healthEvaluation) (bool, error) {
		if actual.Healthy {
			return false, nil
		}
		return checks.Match(actual.Checks)
	}).WithMessage("report unhealthy health checks")
}

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
