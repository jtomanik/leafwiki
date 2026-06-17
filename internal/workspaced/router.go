package workspaced

import (
	"github.com/gin-gonic/gin"
	httpinternal "github.com/perber/wiki/internal/http"
	"github.com/perber/wiki/internal/wiki"
)

func NewRouter(w *wiki.Wiki, opts httpinternal.RouterOptions) *gin.Engine {
	opts.DisableFrontendRoutes = true
	return httpinternal.NewRouter(w.WorkspacedRegistrars(), httpinternal.FrontendConfig{}, opts)
}
