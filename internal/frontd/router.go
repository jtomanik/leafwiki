package frontd

import (
	"github.com/gin-gonic/gin"
	httpinternal "github.com/perber/wiki/internal/http"
	"github.com/perber/wiki/internal/wiki"
)

func NewRouter(w *wiki.Wiki, opts httpinternal.RouterOptions) *gin.Engine {
	return httpinternal.NewRouter(w.FrontdRegistrars(), w.FrontendConfig(), opts)
}
