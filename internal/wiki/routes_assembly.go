package wiki

import (
	"context"
	"fmt"
	"net/http"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
	corebranding "github.com/perber/wiki/internal/branding"
	"github.com/perber/wiki/internal/core/auth"
	httpinternal "github.com/perber/wiki/internal/http"
	"github.com/perber/wiki/internal/projectdaemon"
	wikihealth "github.com/perber/wiki/internal/wiki/health"
	wikimcp "github.com/perber/wiki/internal/wiki/mcp"
	wikioauth "github.com/perber/wiki/internal/wiki/oauth"
	wikipresence "github.com/perber/wiki/internal/wiki/presence"
	wikiworkspacesync "github.com/perber/wiki/internal/wiki/workspacesync"
)

var frontendBrandingConfig = func(s *corebranding.BrandingService) (*corebranding.BrandingConfigResponse, error) {
	return s.GetBranding()
}

func (w *Wiki) buildRoutes(options *WikiOptions) {
	w.pagesRoutes = w.buildPagesRoutes()
	w.authRoutes = w.buildAuthRoutes()
	w.assetsRoutes = w.buildAssetsRoutes()
	w.revisionsRoutes = w.buildRevisionsRoutes()
	w.searchRoutes = w.buildSearchRoutes()
	w.linksRoutes = w.buildLinksRoutes()
	w.tagsRoutes = w.buildTagsRoutes()
	w.propertiesRoutes = w.buildPropertiesRoutes()
	w.brandingRoutes = w.buildBrandingRoutes()
	w.importerRoutes = w.buildImporterRoutes(options)
	w.healthRoutes = wikihealth.NewRoutes(wikihealth.RoutesConfig{
		Index:      w.searchIndex,
		Status:     w.status,
		StorageDir: w.storageDir,
	})
	w.workspaceSyncRoutes = wikiworkspacesync.NewRoutes(wikiworkspacesync.RoutesConfig{
		Status:           w.WorkspaceSyncStatus,
		Refresh:          w.WorkspaceSyncRefresh,
		ListSnapshots:    w.WorkspaceSyncSnapshotPage,
		RestoreWorkspace: w.WorkspaceSyncRestoreWorkspace,
		AuthService:      w.auth,
	})
	w.presenceRoutes = wikipresence.NewRoutes(wikipresence.RoutesConfig{
		Registry:    w.webPresence,
		TreeService: w.tree,
		AuthService: w.auth,
	})
	w.oauthRoutes = wikioauth.NewRoutes(w.oauth)
	w.mcpRoutes = w.buildMCPRoutes()
}

func (w *Wiki) buildControlPlaneRoutes(_ *WikiOptions) {
	w.authRoutes = w.buildAuthRoutes()
	w.brandingRoutes = w.buildBrandingRoutes()
	w.healthRoutes = wikihealth.NewRoutes(wikihealth.RoutesConfig{
		StorageDir: w.storageDir,
	})
	w.oauthRoutes = wikioauth.NewRoutes(w.oauth)
}

// Registrars returns all domain route registrars in registration order.
func (w *Wiki) Registrars() []httpinternal.RouteRegistrar {
	return []httpinternal.RouteRegistrar{
		w.authRoutes,
		w.pagesRoutes,
		w.assetsRoutes,
		w.revisionsRoutes,
		w.searchRoutes,
		w.linksRoutes,
		w.tagsRoutes,
		w.propertiesRoutes,
		w.brandingRoutes,
		w.importerRoutes,
		w.healthRoutes,
		w.workspaceSyncRoutes,
		w.presenceRoutes,
		w.oauthRoutes,
		w.mcpRoutes,
	}
}

func (w *Wiki) FrontdRegistrars() []httpinternal.RouteRegistrar {
	return []httpinternal.RouteRegistrar{
		w.authRoutes,
		w.brandingRoutes,
		w.healthRoutes,
		w.oauthRoutes,
	}
}

func (w *Wiki) WorkspacedRegistrars() []httpinternal.RouteRegistrar {
	return []httpinternal.RouteRegistrar{
		w.pagesRoutes,
		w.assetsRoutes,
		w.revisionsRoutes,
		w.searchRoutes,
		w.linksRoutes,
		w.tagsRoutes,
		w.propertiesRoutes,
		w.importerRoutes,
		w.workspaceSyncRoutes,
		w.presenceRoutes,
	}
}

func (w *Wiki) SetAgentPresenceRegistry(registry *projectdaemon.AgentPresenceRegistry) {
	w.agentPresence = registry
}

func (w *Wiki) SetRuntimeRoleHealth(required []projectdaemon.RoleName, roleHealth func() []projectdaemon.RoleHealth) {
	if w.healthRoutes == nil {
		return
	}
	w.healthRoutes.SetRoleHealth(required, roleHealth)
}

func (w *Wiki) WebPresenceSessions(viewer *auth.User) ([]wikipresence.Session, error) {
	if w.webPresence == nil {
		return nil, fmt.Errorf("web presence is unavailable")
	}
	return w.webPresence.List(viewer), nil
}

func (w *Wiki) AgentPresenceSessions() ([]projectdaemon.AgentPresenceSession, error) {
	if w.agentPresence == nil {
		return nil, fmt.Errorf("agent presence is unavailable")
	}
	return w.agentPresence.List(), nil
}

func (w *Wiki) RunMCPStdio(ctx context.Context, opts httpinternal.RouterOptions, transport sdkmcp.Transport) error {
	return w.RunMCPStdioWithAuth(ctx, opts, transport, wikimcp.StdioAuth{DisabledAuth: opts.AuthDisabled})
}

func (w *Wiki) RunMCPStdioWithAuth(ctx context.Context, opts httpinternal.RouterOptions, transport sdkmcp.Transport, stdioAuth wikimcp.StdioAuth) error {
	if opts.MCPToolListPageSize <= 0 {
		opts.MCPToolListPageSize = 100
	}
	server := w.mcpRoutes.NewStdioServer(opts, stdioAuth)
	return server.Run(ctx, transport)
}

func (w *Wiki) MCPHTTPHandler(opts httpinternal.RouterOptions) http.Handler {
	if opts.MCPToolListPageSize <= 0 {
		opts.MCPToolListPageSize = 100
	}
	return w.mcpRoutes.NewHTTPHandler(opts)
}

func (w *Wiki) PrivateMCPHTTPHandler(opts httpinternal.RouterOptions) http.Handler {
	if opts.MCPToolListPageSize <= 0 {
		opts.MCPToolListPageSize = 100
	}
	return w.mcpRoutes.NewPrivateHTTPHandler(opts)
}

func (w *Wiki) ActorContextMCPHTTPHandler(opts httpinternal.RouterOptions) http.Handler {
	if opts.MCPToolListPageSize <= 0 {
		opts.MCPToolListPageSize = 100
	}
	return w.mcpRoutes.NewActorContextHTTPHandler(opts)
}

// FrontendConfig returns the minimal runtime data required by the router to serve the SPA.
func (w *Wiki) FrontendConfig() httpinternal.FrontendConfig {
	return httpinternal.FrontendConfig{
		StorageDir: w.storageDir,
		GetSiteName: func() string {
			if w.branding == nil {
				return ""
			}
			cfg, err := frontendBrandingConfig(w.branding)
			if err != nil || cfg == nil {
				return ""
			}
			return cfg.SiteName
		},
		GetFaviconFile: func() string {
			if w.branding == nil {
				return ""
			}
			cfg, err := frontendBrandingConfig(w.branding)
			if err != nil || cfg == nil {
				return ""
			}
			return cfg.FaviconFile
		},
	}
}
