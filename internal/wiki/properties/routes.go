package properties

import (
	"context"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	coreauth "github.com/perber/wiki/internal/core/auth"
	httpinternal "github.com/perber/wiki/internal/http"
	authmw "github.com/perber/wiki/internal/http/middleware/auth"
	"github.com/perber/wiki/internal/http/middleware/security"
	coreprop "github.com/perber/wiki/internal/properties"
)

// Routes is the RouteRegistrar for the properties domain.
type Routes struct {
	getPropertyKeys    propertyKeysExecutor
	getPagesByProperty pagesByPropertyExecutor
	authService        *coreauth.AuthService
}

type propertyKeysExecutor interface {
	Execute(ctx context.Context, in GetPropertyKeysInput) (*GetPropertyKeysOutput, error)
}

type pagesByPropertyExecutor interface {
	Execute(ctx context.Context, in GetPagesByPropertyInput) (*GetPagesByPropertyOutput, error)
}

// RoutesConfig holds the dependencies required to build a Routes instance.
type RoutesConfig struct {
	GetPropertyKeys    *GetPropertyKeysUseCase
	GetPagesByProperty *GetPagesByPropertyUseCase
	AuthService        *coreauth.AuthService
}

// NewRoutes constructs the properties RouteRegistrar.
func NewRoutes(cfg RoutesConfig) *Routes {
	var getPropertyKeys propertyKeysExecutor
	if cfg.GetPropertyKeys != nil {
		getPropertyKeys = cfg.GetPropertyKeys
	}
	var getPagesByProperty pagesByPropertyExecutor
	if cfg.GetPagesByProperty != nil {
		getPagesByProperty = cfg.GetPagesByProperty
	}
	return &Routes{
		getPropertyKeys:    getPropertyKeys,
		getPagesByProperty: getPagesByProperty,
		authService:        cfg.AuthService,
	}
}

// RegisterRoutes implements RouteRegistrar.
func (r *Routes) RegisterRoutes(ctx httpinternal.RouterContext) {
	opts := ctx.Opts

	if opts.PublicAccess {
		pub := ctx.Base.Group("/api")
		pub.GET("/properties", r.handleGetPropertyKeys)
		pub.GET("/properties/pages", r.handleGetPagesByProperty)
	}

	authGroup := ctx.Base.Group("/api")
	authGroup.Use(
		authmw.InjectPublicEditor(opts.AuthDisabled),
		authmw.RequireAuth(r.authService, ctx.AuthCookies, opts.AuthDisabled),
		security.CSRFMiddleware(ctx.CSRFCookie),
	)

	if !opts.PublicAccess {
		authGroup.GET("/properties", r.handleGetPropertyKeys)
		authGroup.GET("/properties/pages", r.handleGetPagesByProperty)
	}
}

// ─── Handlers ────────────────────────────────────────────────────────────────

// handleGetPropertyKeys handles GET /api/properties?q=&limit=
func (r *Routes) handleGetPropertyKeys(c *gin.Context) {
	filter := c.DefaultQuery("q", "")
	limitStr := c.DefaultQuery("limit", "50")

	limit, err := strconv.Atoi(limitStr)
	if err != nil {
		respondWithPropertiesBadRequest(c, ErrCodePropertiesInvalidLimit, "Invalid limit value", "invalid limit value")
		return
	}

	out, err := r.getPropertyKeys.Execute(c.Request.Context(), GetPropertyKeysInput{
		Filter:   filter,
		PageSize: coreprop.PropertyKeyLimit(limit),
	})
	if err != nil {
		respondWithPropertiesError(c, err)
		return
	}
	c.JSON(http.StatusOK, out.Keys)
}

// handleGetPagesByProperty handles GET /api/properties/pages?key=status&value=draft
func (r *Routes) handleGetPagesByProperty(c *gin.Context) {
	key := c.Query("key")
	value := c.Query("value")

	out, err := r.getPagesByProperty.Execute(c.Request.Context(), GetPagesByPropertyInput{Key: key, Value: value})
	if err != nil {
		respondWithPropertiesError(c, err)
		return
	}
	c.JSON(http.StatusOK, out.Pages)
}
