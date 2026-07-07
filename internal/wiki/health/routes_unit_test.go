package health

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"

	"github.com/gin-gonic/gin"
	httpinternal "github.com/perber/wiki/internal/http"
	"github.com/perber/wiki/internal/projectdaemon"
	"github.com/perber/wiki/internal/search"

	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/gstruct"
	"github.com/onsi/gomega/types"
)

var _ = ginkgo.Describe("health route handlers", ginkgo.Label("unit"), func() {
	ginkgo.It("registers the health endpoint", func() {
		gin.SetMode(gin.TestMode)
		engine := gin.New()

		NewRoutes(RoutesConfig{StorageDir: healthTempDir()}).RegisterRoutes(httpinternal.RouterContext{
			Base: engine.Group(""),
		})

		Expect(healthRegisteredRoutes(engine)).To(ContainElement("GET /api/health"))
	})

	ginkgo.It("publishes degraded health from the direct handler", func() {
		ctx, rec := newHealthUnitContext()
		routes := NewRoutes(RoutesConfig{StorageDir: filepath.Join(healthTempDir(), "missing")})

		routes.handleHealth(ctx)

		Expect(rec).To(HaveHTTPStatus(http.StatusServiceUnavailable), rec.Body.String())
		body := decodeHealthResponse(rec)
		Expect(body).To(matchHealthResponse(healthStatusDegraded, HaveKeyWithValue(healthCheckDataDir.String(), healthStatusFailed)))
	})

	ginkgo.It("publishes updated role health from the direct handler", func() {
		ctx, rec := newHealthUnitContext()
		routes := NewRoutes(RoutesConfig{StorageDir: healthTempDir()})
		routes.SetRoleHealth([]projectdaemon.RoleName{projectdaemon.RoleWikid}, func() []projectdaemon.RoleHealth {
			return []projectdaemon.RoleHealth{{Name: projectdaemon.RoleWikid, State: projectdaemon.RoleStateReady}}
		})

		routes.handleHealth(ctx)

		Expect(rec).To(HaveHTTPStatus(http.StatusOK), rec.Body.String())
		body := decodeHealthResponse(rec)
		Expect(body.Checks).To(HaveKeyWithValue(healthCheckRoleWikid.String(), healthStatusOK))
	})

	ginkgo.It("keeps the legacy constructor behavior aligned with default health checks", func() {
		Expect(evaluateHealth(NewLegacyHealthUseCase(nil, nil, healthTempDir()))).To(reportHealthyHealthChecks(
			HaveKeyWithValue(healthCheckSQLite, healthStatusNotApplicable),
		))
	})

	ginkgo.It("treats typed-nil constructor indexes as not applicable", func() {
		var index *search.SQLiteIndex

		Expect(evaluateHealth(NewHealthUseCase(index, nil, healthTempDir()))).To(reportHealthyHealthChecks(
			HaveKeyWithValue(healthCheckSQLite, healthStatusNotApplicable),
		))
		Expect(evaluateHealth(NewLegacyHealthUseCase(index, nil, healthTempDir()))).To(reportHealthyHealthChecks(
			HaveKeyWithValue(healthCheckSQLite, healthStatusNotApplicable),
		))
	})

	ginkgo.It("keeps concrete constructor indexes configured without pinging them", func() {
		index := &search.SQLiteIndex{}

		Expect(NewHealthUseCase(index, nil, healthTempDir()).index).NotTo(BeNil())
		Expect(NewLegacyHealthUseCase(index, nil, healthTempDir()).index).NotTo(BeNil())
	})

	ginkgo.It("treats omitted route indexes as not applicable", func() {
		ctx, rec := newHealthUnitContext()

		NewRoutes(RoutesConfig{StorageDir: healthTempDir()}).handleHealth(ctx)

		Expect(rec).To(HaveHTTPStatus(http.StatusOK), rec.Body.String())
		body := decodeHealthResponse(rec)
		Expect(body.Checks).To(HaveKeyWithValue(healthCheckSQLite.String(), healthStatusNotApplicable))
	})

	ginkgo.It("marks configured index ping failures as unhealthy", func() {
		Expect(evaluateHealth(newHealthUseCase(failingHealthIndex{}, nil, healthTempDir()))).To(reportUnhealthyHealthChecks(
			HaveKeyWithValue(healthCheckSQLite, healthStatusFailed),
		))
	})

	ginkgo.It("marks configured index ping success as healthy", func() {
		Expect(evaluateHealth(newHealthUseCase(passingHealthIndex{}, nil, healthTempDir()))).To(reportHealthyHealthChecks(
			HaveKeyWithValue(healthCheckSQLite, healthStatusOK),
		))
	})
})

var _ = ginkgo.Describe("health response serialization", ginkgo.Label("unit"), func() {
	ginkgo.It("serializes health check keys and statuses for HTTP responses", func() {
		checks := healthChecks{
			healthCheckDataDir: healthStatusOK,
			healthCheckSearch:  healthStatusIndexing,
		}

		Expect(healthCheckDataDir.String()).To(Equal("data_dir"))
		Expect(healthStatusOK.String()).To(Equal("ok"))
		Expect(checks.HTTPMap()).To(Equal(map[string]string{
			"data_dir": "ok",
			"search":   "indexing",
		}))
	})

	ginkgo.DescribeTable("maps runtime role states into health statuses",
		func(state projectdaemon.RoleState, expected healthStatus) {
			Expect(healthStatusForRoleState(state)).To(Equal(expected))
		},
		ginkgo.Entry("starting roles", projectdaemon.RoleStateStarting, healthStatusStarting),
		ginkgo.Entry("ready roles", projectdaemon.RoleStateReady, healthStatusOK),
		ginkgo.Entry("degraded roles", projectdaemon.RoleStateDegraded, healthStatusDegraded),
		ginkgo.Entry("restarting roles", projectdaemon.RoleStateRestarting, healthStatusRestarting),
		ginkgo.Entry("crashed roles", projectdaemon.RoleStateCrashed, healthStatusCrashed),
		ginkgo.Entry("stopped roles", projectdaemon.RoleStateStopped, healthStatusStopped),
		ginkgo.Entry("unknown roles", newFixtureRoleState("custom"), healthStatusUnknown),
	)
})

func newHealthUnitContext() (*gin.Context, *httptest.ResponseRecorder) {
	ginkgo.GinkgoHelper()

	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(rec)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/api/health", nil)
	return ctx, rec
}

func healthRegisteredRoutes(engine *gin.Engine) []string {
	routes := engine.Routes()
	out := make([]string, 0, len(routes))
	for _, route := range routes {
		out = append(out, route.Method+" "+route.Path)
	}
	return out
}

func matchHealthResponse(status healthStatus, checks types.GomegaMatcher) types.GomegaMatcher {
	ginkgo.GinkgoHelper()
	return gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
		"Status": Equal(status),
		"Checks": checks,
	})
}

func newFixtureRoleState[T ~string](raw T) projectdaemon.RoleState {
	return projectdaemon.RoleState(raw)
}

type failingHealthIndex struct{}

func (failingHealthIndex) Ping() error {
	return errors.New("index ping failed")
}

type passingHealthIndex struct{}

func (passingHealthIndex) Ping() error {
	return nil
}
