package presence

import (
	"errors"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	coreauth "github.com/perber/wiki/internal/core/auth"
	sharederrors "github.com/perber/wiki/internal/core/shared/errors"
	"github.com/perber/wiki/internal/core/tree"
	httpinternal "github.com/perber/wiki/internal/http"
	authmw "github.com/perber/wiki/internal/http/middleware/auth"
	"github.com/perber/wiki/internal/http/middleware/security"
)

type Routes struct {
	registry    *WebPresenceRegistry
	treeService *tree.TreeService
	authService *coreauth.AuthService
}

type RoutesConfig struct {
	Registry    *WebPresenceRegistry
	TreeService *tree.TreeService
	AuthService *coreauth.AuthService
}

func NewRoutes(cfg RoutesConfig) *Routes {
	return &Routes{
		registry:    cfg.Registry,
		treeService: cfg.TreeService,
		authService: cfg.AuthService,
	}
}

func (r *Routes) RegisterRoutes(ctx httpinternal.RouterContext) {
	if r == nil || r.registry == nil {
		return
	}
	group := ctx.Base.Group("/api/presence")
	group.Use(
		authmw.InjectPublicEditor(ctx.Opts.AuthDisabled),
		authmw.RequireAuth(r.authService, ctx.AuthCookies, ctx.Opts.AuthDisabled),
		security.CSRFMiddleware(ctx.CSRFCookie),
	)
	group.POST("/heartbeat", r.handleHeartbeat)
	group.DELETE("/session/:id", r.handleDeleteSession)
}

func (r *Routes) handleHeartbeat(c *gin.Context) {
	user := authmw.MustGetUser(c)
	if user == nil {
		return
	}
	var heartbeat Heartbeat
	if err := c.ShouldBindJSON(&heartbeat); err != nil {
		c.JSON(http.StatusBadRequest, presenceErrorResponse{
			Error: sharederrors.NewLocalizedErrorDetailFromCode(ErrCodePresenceInvalidRequest, err.Error()),
		})
		return
	}
	page := r.resolvePage(heartbeat.PageID, heartbeat.Path)
	if err := r.registry.Record(heartbeat, user, page); err != nil {
		if loc, ok := sharederrors.AsLocalizedError(err); ok {
			c.JSON(http.StatusBadRequest, presenceErrorResponse{
				Error: sharederrors.LocalizedErrorDetailFromError(loc),
			})
			return
		}
		c.JSON(http.StatusBadRequest, presenceErrorResponse{
			Error: sharederrors.NewLocalizedErrorDetailFromCode(ErrCodePresenceInvalidRequest, err.Error()),
		})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

type presenceErrorResponse struct {
	Error sharederrors.LocalizedErrorDetail `json:"error"`
}

func (r *Routes) handleDeleteSession(c *gin.Context) {
	user := authmw.MustGetUser(c)
	if user == nil {
		return
	}
	sessionID := strings.TrimSpace(c.Param("id"))
	r.registry.Remove(WebSessionIDFromString(sessionID), user)
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

func (r *Routes) resolvePage(pageID tree.PageID, path string) *PageRef {
	if r == nil || r.treeService == nil {
		return nil
	}
	if pageID != "" {
		if page, err := r.treeService.GetPage(pageID); err == nil {
			return pageRefForPage(page)
		}
	}
	routePathValue := strings.Trim(strings.TrimSpace(path), "/")
	if routePathValue == "" {
		return nil
	}
	routePath, err := tree.ParseRoutePath(routePathValue)
	if err != nil {
		return nil
	}
	page, err := r.treeService.FindPageByRoutePath(routePath)
	if err != nil {
		if !errors.Is(err, tree.ErrPageNotFound) && !errors.Is(err, tree.ErrTreeNotLoaded) {
			return nil
		}
		return nil
	}
	return pageRefForPage(page)
}

func pageRefForPage(page *tree.Page) *PageRef {
	if page == nil || page.PageNode == nil {
		return nil
	}
	return &PageRef{
		ID:    page.ID,
		Path:  normalizePagePath(page.CalculatePath()),
		Title: page.Title,
	}
}
