package workspaced

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	coreauth "github.com/perber/wiki/internal/core/auth"
	httpinternal "github.com/perber/wiki/internal/http"
	"github.com/perber/wiki/internal/http/middleware/security"
	"github.com/perber/wiki/internal/projectdaemon"
	"github.com/perber/wiki/internal/wiki"
)

type PrivateAuthOptions struct {
	DaemonToken string
	WorkspaceID string
	Now         func() time.Time
}

func NewAuthenticatedRouter(w *wiki.Wiki, opts httpinternal.RouterOptions, auth PrivateAuthOptions) *gin.Engine {
	opts.DisableFrontendRoutes = true
	registrars := w.WorkspacedRegistrars()
	guarded := make([]httpinternal.RouteRegistrar, 0, len(registrars))
	for _, registrar := range registrars {
		guarded = append(guarded, privateAuthRegistrar{
			auth:      auth,
			registrar: registrar,
		})
	}
	return httpinternal.NewRouter(guarded, httpinternal.FrontendConfig{}, opts)
}

type privateAuthRegistrar struct {
	auth      PrivateAuthOptions
	registrar httpinternal.RouteRegistrar
}

func (r privateAuthRegistrar) RegisterRoutes(ctx httpinternal.RouterContext) {
	next := ctx
	next.Base = ctx.Base.Group("", requirePrivateActorContext(r.auth))
	r.registrar.RegisterRoutes(next)
}

func requirePrivateActorContext(opts PrivateAuthOptions) gin.HandlerFunc {
	return func(c *gin.Context) {
		if c.GetHeader(projectdaemon.ControlTokenHeader) != opts.DaemonToken || opts.DaemonToken == "" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
			return
		}
		now := time.Now().UTC
		if opts.Now != nil {
			now = opts.Now
		}
		actor, err := projectdaemon.DecodeActorContext(c.GetHeader(projectdaemon.ActorContextHeader), projectdaemon.ActorContextValidation{
			Now:         now(),
			WorkspaceID: opts.WorkspaceID,
		})
		if err != nil {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "invalid actor context"})
			return
		}
		c.Set("user", &coreauth.User{
			ID:       actor.SubjectID(),
			Username: actor.Username,
			Email:    actor.Email,
			Role:     actor.Role,
		})
		security.TrustPrivateChannel(c)
		c.Next()
	}
}
