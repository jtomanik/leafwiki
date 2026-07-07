package links

import (
	"context"
	"net/http"

	"github.com/gin-gonic/gin"
	coreauth "github.com/perber/wiki/internal/core/auth"
	"github.com/perber/wiki/internal/core/tree"
	httpinternal "github.com/perber/wiki/internal/http"
	authmw "github.com/perber/wiki/internal/http/middleware/auth"
	"github.com/perber/wiki/internal/http/middleware/security"
)

// Routes is the RouteRegistrar for the links domain.
type Routes struct {
	getLinkStatus linkStatusExecutor
	authService   *coreauth.AuthService
}

type linkStatusExecutor interface {
	Execute(ctx context.Context, in GetLinkStatusInput) (*GetLinkStatusOutput, error)
}

// RoutesConfig holds the dependencies required to build a Routes instance.
type RoutesConfig struct {
	GetLinkStatus *GetLinkStatusUseCase
	AuthService   *coreauth.AuthService
}

// NewRoutes constructs the links RouteRegistrar.
func NewRoutes(cfg RoutesConfig) *Routes {
	return &Routes{
		getLinkStatus: cfg.GetLinkStatus,
		authService:   cfg.AuthService,
	}
}

// RegisterRoutes implements RouteRegistrar.
func (r *Routes) RegisterRoutes(ctx httpinternal.RouterContext) {
	opts := ctx.Opts

	if opts.PublicAccess {
		pub := ctx.Base.Group("/api")
		pub.GET("/pages/:id/links", r.handleGetLinkStatus)
	}

	authGroup := ctx.Base.Group("/api")
	authGroup.Use(
		authmw.InjectPublicEditor(opts.AuthDisabled),
		authmw.RequireAuth(r.authService, ctx.AuthCookies, opts.AuthDisabled),
		security.CSRFMiddleware(ctx.CSRFCookie),
	)

	if !opts.PublicAccess {
		authGroup.GET("/pages/:id/links", r.handleGetLinkStatus)
	}
}

// ─── Handlers ───────────────────────────────────────────────────────────────

func (r *Routes) handleGetLinkStatus(c *gin.Context) {
	pageID := c.Param("id")
	out, err := r.getLinkStatus.Execute(c.Request.Context(), GetLinkStatusInput{PageID: tree.PageIDFromString(pageID)})
	if err != nil {
		respondWithLinkError(c, err)
		return
	}
	c.JSON(http.StatusOK, out.Status)
}
