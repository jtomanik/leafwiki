package health

import (
	"net/http"

	"github.com/gin-gonic/gin"
	httpinternal "github.com/perber/wiki/internal/http"
	"github.com/perber/wiki/internal/projectdaemon"
	"github.com/perber/wiki/internal/search"
)

type Routes struct {
	health *HealthUseCase
}

type RoutesConfig struct {
	Index         *search.SQLiteIndex
	Status        *search.IndexingStatus
	StorageDir    string
	RequiredRoles []projectdaemon.RoleName
	RoleHealth    func() []projectdaemon.RoleHealth
}

func NewRoutes(cfg RoutesConfig) *Routes {
	return &Routes{
		health: NewHealthUseCase(cfg.Index, cfg.Status, cfg.StorageDir, HealthUseCaseOptions{
			RequiredRoles: cfg.RequiredRoles,
			RoleHealth:    cfg.RoleHealth,
		}),
	}
}

func (r *Routes) SetRoleHealth(required []projectdaemon.RoleName, roleHealth func() []projectdaemon.RoleHealth) {
	r.health.SetRoleHealth(required, roleHealth)
}

func (r *Routes) RegisterRoutes(ctx httpinternal.RouterContext) {
	ctx.Base.GET("/api/health", r.handleHealth)
}

func (r *Routes) handleHealth(c *gin.Context) {
	healthy, checks := r.health.Execute()

	status := "ok"
	code := http.StatusOK
	if !healthy {
		status = "degraded"
		code = http.StatusServiceUnavailable
	}

	c.JSON(code, gin.H{
		"status": status,
		"checks": checks,
	})
}
