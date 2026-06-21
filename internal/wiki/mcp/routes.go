package mcp

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	sdkauth "github.com/modelcontextprotocol/go-sdk/auth"
	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
	coreauth "github.com/perber/wiki/internal/core/auth"
	corerevision "github.com/perber/wiki/internal/core/revision"
	"github.com/perber/wiki/internal/core/tree"
	httpinternal "github.com/perber/wiki/internal/http"
	"github.com/perber/wiki/internal/projectdaemon"
	wikiassets "github.com/perber/wiki/internal/wiki/assets"
	wikilinks "github.com/perber/wiki/internal/wiki/links"
	wikioauth "github.com/perber/wiki/internal/wiki/oauth"
	wikipages "github.com/perber/wiki/internal/wiki/pages"
	wikipresence "github.com/perber/wiki/internal/wiki/presence"
	wikiproperties "github.com/perber/wiki/internal/wiki/properties"
	wikisearch "github.com/perber/wiki/internal/wiki/search"
	wikitags "github.com/perber/wiki/internal/wiki/tags"
	"github.com/perber/wiki/internal/workspacesync"
)

const defaultToolListPageSize = 100

// Routes registers LeafWiki's local-only MCP Streamable HTTP endpoint.
type Routes struct {
	treeService  *tree.TreeService
	userResolver *coreauth.UserResolver
	userService  *coreauth.UserService
	apiKeys      *coreauth.APIKeyService
	oauthService *wikioauth.Service
	authDisabled bool
	stdioAPIKey  string
	createPage   *wikipages.CreatePageUseCase
	updatePage   *wikipages.UpdatePageUseCase
	getPage      *wikipages.GetPageUseCase
	findByPath   *wikipages.FindByPathUseCase
	lookupPath   *wikipages.LookupPagePathUseCase
	resolveLink  *wikipages.ResolvePermalinkUseCase
	suggestSlug  *wikipages.SuggestSlugUseCase
	deletePage   *wikipages.DeletePageUseCase
	movePage     *wikipages.MovePageUseCase
	sortPages    *wikipages.SortPagesUseCase
	ensurePath   *wikipages.EnsurePathUseCase
	convertPage  *wikipages.ConvertPageUseCase
	copyPage     *wikipages.CopyPageUseCase
	previewRef   *wikipages.PreviewPageRefactorUseCase
	applyRef     *wikipages.ApplyPageRefactorUseCase
	search       *wikisearch.SearchUseCase
	searchStatus *wikisearch.GetIndexingStatusUseCase
	getTags      *wikitags.GetTagsUseCase
	pagesByTags  *wikitags.GetPagesByTagsUseCase
	propertyKeys *wikiproperties.GetPropertyKeysUseCase
	pagesByProp  *wikiproperties.GetPagesByPropertyUseCase
	linkStatus   *wikilinks.GetLinkStatusUseCase
	uploadAsset  *wikiassets.UploadAssetUseCase
	getAsset     *wikiassets.GetAssetUseCase
	getAssets    *wikiassets.ListAssetsUseCase
	renameAsset  *wikiassets.RenameAssetUseCase
	deleteAsset  *wikiassets.DeleteAssetUseCase

	listWorkspaceRevisions   func(context.Context, *tree.Page, string, int) (workspacesync.PageRevisionList, error)
	getWorkspaceRevision     func(context.Context, *tree.Page, string) (*corerevision.RevisionSnapshot, error)
	restoreWorkspaceRevision func(context.Context, *tree.Page, string, workspacesync.Actor, workspacesync.Source) (*tree.Page, error)
	workspaceSyncStatus      func() workspacesync.SyncStatus
	workspaceSyncRefresh     func(context.Context, workspacesync.SyncRequest) (workspacesync.SyncStatus, error)
	listWorkspaceSnapshots   func(context.Context, string, int) (workspacesync.SnapshotList, error)
	workspaceRootDir         string
	workspaceDataDir         string
	markdownLinkRootPrefix   string
	webPresenceProvider      func(*coreauth.User) ([]wikipresence.Session, error)
	agentPresenceProvider    func() ([]projectdaemon.AgentPresenceSession, error)
	contextStore             *contextCheckpointStore
	workspaceID              string
	now                      func() time.Time
	actorContextAllowed      bool
	actorContextRequired     bool
}

type RoutesConfig struct {
	TreeService  *tree.TreeService
	UserResolver *coreauth.UserResolver
	UserService  *coreauth.UserService
	APIKeys      *coreauth.APIKeyService
	OAuthService *wikioauth.Service
	CreatePage   *wikipages.CreatePageUseCase
	UpdatePage   *wikipages.UpdatePageUseCase
	GetPage      *wikipages.GetPageUseCase
	FindByPath   *wikipages.FindByPathUseCase
	LookupPath   *wikipages.LookupPagePathUseCase
	ResolveLink  *wikipages.ResolvePermalinkUseCase
	SuggestSlug  *wikipages.SuggestSlugUseCase
	DeletePage   *wikipages.DeletePageUseCase
	MovePage     *wikipages.MovePageUseCase
	SortPages    *wikipages.SortPagesUseCase
	EnsurePath   *wikipages.EnsurePathUseCase
	ConvertPage  *wikipages.ConvertPageUseCase
	CopyPage     *wikipages.CopyPageUseCase
	PreviewRef   *wikipages.PreviewPageRefactorUseCase
	ApplyRef     *wikipages.ApplyPageRefactorUseCase
	Search       *wikisearch.SearchUseCase
	SearchStatus *wikisearch.GetIndexingStatusUseCase
	GetTags      *wikitags.GetTagsUseCase
	PagesByTags  *wikitags.GetPagesByTagsUseCase
	PropertyKeys *wikiproperties.GetPropertyKeysUseCase
	PagesByProp  *wikiproperties.GetPagesByPropertyUseCase
	LinkStatus   *wikilinks.GetLinkStatusUseCase
	UploadAsset  *wikiassets.UploadAssetUseCase
	GetAsset     *wikiassets.GetAssetUseCase
	GetAssets    *wikiassets.ListAssetsUseCase
	RenameAsset  *wikiassets.RenameAssetUseCase
	DeleteAsset  *wikiassets.DeleteAssetUseCase

	ListWorkspaceRevisions   func(context.Context, *tree.Page, string, int) (workspacesync.PageRevisionList, error)
	GetWorkspaceRevision     func(context.Context, *tree.Page, string) (*corerevision.RevisionSnapshot, error)
	RestoreWorkspaceRevision func(context.Context, *tree.Page, string, workspacesync.Actor, workspacesync.Source) (*tree.Page, error)
	WorkspaceSyncStatus      func() workspacesync.SyncStatus
	WorkspaceSyncRefresh     func(context.Context, workspacesync.SyncRequest) (workspacesync.SyncStatus, error)
	ListWorkspaceSnapshots   func(context.Context, string, int) (workspacesync.SnapshotList, error)
	WorkspaceRootDir         string
	WorkspaceDataDir         string
	MarkdownLinkRootPrefix   string
	WebPresenceProvider      func(*coreauth.User) ([]wikipresence.Session, error)
	AgentPresenceProvider    func() ([]projectdaemon.AgentPresenceSession, error)
	WorkspaceID              string
}

func NewRoutes(cfg RoutesConfig) *Routes {
	return &Routes{
		treeService:  cfg.TreeService,
		userResolver: cfg.UserResolver,
		userService:  cfg.UserService,
		apiKeys:      cfg.APIKeys,
		oauthService: cfg.OAuthService,
		createPage:   cfg.CreatePage,
		updatePage:   cfg.UpdatePage,
		getPage:      cfg.GetPage,
		findByPath:   cfg.FindByPath,
		lookupPath:   cfg.LookupPath,
		resolveLink:  cfg.ResolveLink,
		suggestSlug:  cfg.SuggestSlug,
		deletePage:   cfg.DeletePage,
		movePage:     cfg.MovePage,
		sortPages:    cfg.SortPages,
		ensurePath:   cfg.EnsurePath,
		convertPage:  cfg.ConvertPage,
		copyPage:     cfg.CopyPage,
		previewRef:   cfg.PreviewRef,
		applyRef:     cfg.ApplyRef,
		search:       cfg.Search,
		searchStatus: cfg.SearchStatus,
		getTags:      cfg.GetTags,
		pagesByTags:  cfg.PagesByTags,
		propertyKeys: cfg.PropertyKeys,
		pagesByProp:  cfg.PagesByProp,
		linkStatus:   cfg.LinkStatus,
		uploadAsset:  cfg.UploadAsset,
		getAsset:     cfg.GetAsset,
		getAssets:    cfg.GetAssets,
		renameAsset:  cfg.RenameAsset,
		deleteAsset:  cfg.DeleteAsset,

		listWorkspaceRevisions:   cfg.ListWorkspaceRevisions,
		getWorkspaceRevision:     cfg.GetWorkspaceRevision,
		restoreWorkspaceRevision: cfg.RestoreWorkspaceRevision,
		workspaceSyncStatus:      cfg.WorkspaceSyncStatus,
		workspaceSyncRefresh:     cfg.WorkspaceSyncRefresh,
		listWorkspaceSnapshots:   cfg.ListWorkspaceSnapshots,
		workspaceRootDir:         cfg.WorkspaceRootDir,
		workspaceDataDir:         cfg.WorkspaceDataDir,
		markdownLinkRootPrefix:   cfg.MarkdownLinkRootPrefix,
		webPresenceProvider:      cfg.WebPresenceProvider,
		agentPresenceProvider:    cfg.AgentPresenceProvider,
		contextStore:             newContextCheckpointStore(10),
		workspaceID:              cfg.WorkspaceID,
	}
}

func (r *Routes) RegisterRoutes(ctx httpinternal.RouterContext) {
	if !ctx.Opts.MCPEnabled || strings.TrimSpace(ctx.Opts.MCPBindHost) == "" {
		return
	}
	if !ctx.Opts.AuthDisabled && r.oauthService == nil {
		return
	}

	httpHandler := r.NewHTTPHandler(ctx.Opts)
	wrapped := gin.WrapH(httpinternal.LocalOnlyHandler(httpHandler))
	ctx.Base.GET("/mcp", wrapped)
	ctx.Base.POST("/mcp", wrapped)
	ctx.Base.DELETE("/mcp", wrapped)
}

func (r *Routes) NewHTTPHandler(opts httpinternal.RouterOptions) http.Handler {
	server := r.NewServer(opts)
	handler := sdkmcp.NewStreamableHTTPHandler(func(*http.Request) *sdkmcp.Server {
		return server
	}, streamableHTTPOptions())

	var httpHandler http.Handler = handler
	if !opts.AuthDisabled {
		httpHandler = http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			authenticated := sdkauth.RequireBearerToken(r.verifyBearerToken, &sdkauth.RequireBearerTokenOptions{
				ResourceMetadataURL: wikioauth.ProtectedResourceMetadataURL(req, opts.BasePath),
				Scopes:              []string{wikioauth.ScopeMCP},
			})(handler)
			authenticated.ServeHTTP(w, req)
		})
	}
	return httpHandler
}

func (r *Routes) NewPrivateHTTPHandler(opts httpinternal.RouterOptions) http.Handler {
	handler := sdkmcp.NewStreamableHTTPHandler(func(req *http.Request) *sdkmcp.Server {
		auth := StdioAuth{DisabledAuth: opts.AuthDisabled}
		if !opts.AuthDisabled {
			auth.APIKey = bearerTokenFromRequest(req)
		}
		return r.NewStdioServer(opts, auth)
	}, streamableHTTPOptions())
	if opts.AuthDisabled {
		return handler
	}
	return r.requirePrivateStdioAPIKey(handler)
}

func (r *Routes) NewActorContextHTTPHandler(opts httpinternal.RouterOptions) http.Handler {
	server := r.NewActorContextServer(opts)
	return sdkmcp.NewStreamableHTTPHandler(func(*http.Request) *sdkmcp.Server {
		return server
	}, streamableHTTPOptions())
}

func streamableHTTPOptions() *sdkmcp.StreamableHTTPOptions {
	return &sdkmcp.StreamableHTTPOptions{
		Stateless:                  false,
		JSONResponse:               true,
		SessionTimeout:             30 * time.Minute,
		DisableLocalhostProtection: false,
	}
}

func bearerTokenFromRequest(req *http.Request) string {
	if req == nil {
		return ""
	}
	header := strings.TrimSpace(req.Header.Get("Authorization"))
	prefix := "Bearer "
	if len(header) < len(prefix) || !strings.EqualFold(header[:len(prefix)], prefix) {
		return ""
	}
	return strings.TrimSpace(header[len(prefix):])
}

func (r *Routes) requirePrivateStdioAPIKey(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		token := bearerTokenFromRequest(req)
		if token == "" || !coreauth.IsAPIKeyBearer(token) {
			http.Error(w, "native STDIO requires an API key", http.StatusUnauthorized)
			return
		}
		if r.apiKeys == nil {
			http.Error(w, "api key verifier unavailable", http.StatusInternalServerError)
			return
		}
		if _, err := r.apiKeys.VerifyAPIKey(token); err != nil {
			if errors.Is(err, coreauth.ErrInvalidToken) {
				http.Error(w, "invalid api key", http.StatusUnauthorized)
				return
			}
			http.Error(w, fmt.Sprintf("api key verifier failed: %v", err), http.StatusServiceUnavailable)
			return
		}
		next.ServeHTTP(w, req)
	})
}

func (r *Routes) verifyBearerToken(ctx context.Context, token string, req *http.Request) (*sdkauth.TokenInfo, error) {
	if coreauth.IsAPIKeyBearer(token) {
		if r.apiKeys == nil {
			return nil, fmt.Errorf("%w: api key verifier unavailable", sdkauth.ErrInvalidToken)
		}
		verified, err := r.apiKeys.VerifyAPIKey(token)
		if err != nil {
			if !errors.Is(err, coreauth.ErrInvalidToken) {
				return nil, fmt.Errorf("api key verifier failed: %w", err)
			}
			return nil, fmt.Errorf("%w: invalid api key", sdkauth.ErrInvalidToken)
		}
		return &sdkauth.TokenInfo{
			UserID:     verified.User.ID,
			Scopes:     []string{wikioauth.ScopeMCP},
			Expiration: time.Date(9999, 12, 31, 23, 59, 59, 0, time.UTC),
		}, nil
	}
	return r.oauthService.VerifyBearerToken(ctx, token, req)
}

func (r *Routes) NewServer(opts httpinternal.RouterOptions) *sdkmcp.Server {
	serverRoutes := *r
	serverRoutes.authDisabled = opts.AuthDisabled
	serverRoutes.stdioAPIKey = ""
	serverRoutes.actorContextAllowed = false
	serverRoutes.actorContextRequired = false
	return serverRoutes.newServer(opts)
}

type StdioAuth struct {
	DisabledAuth bool
	APIKey       string
}

func (r *Routes) NewStdioServer(opts httpinternal.RouterOptions, auth StdioAuth) *sdkmcp.Server {
	serverRoutes := *r
	serverRoutes.authDisabled = auth.DisabledAuth
	serverRoutes.stdioAPIKey = auth.APIKey
	serverRoutes.actorContextAllowed = false
	serverRoutes.actorContextRequired = false
	return serverRoutes.newServer(opts)
}

func (r *Routes) NewActorContextServer(opts httpinternal.RouterOptions) *sdkmcp.Server {
	serverRoutes := *r
	serverRoutes.authDisabled = false
	serverRoutes.stdioAPIKey = ""
	serverRoutes.actorContextAllowed = true
	serverRoutes.actorContextRequired = true
	return serverRoutes.newServer(opts)
}

func (r *Routes) newServer(opts httpinternal.RouterOptions) *sdkmcp.Server {
	pageSize := opts.MCPToolListPageSize
	if pageSize <= 0 {
		pageSize = defaultToolListPageSize
	}

	server := sdkmcp.NewServer(&sdkmcp.Implementation{
		Name:    "leafwiki",
		Version: "local",
	}, &sdkmcp.ServerOptions{PageSize: pageSize})

	r.registerConfigTools(server, opts)
	r.registerContextTools(server, opts)
	r.registerNavigationTools(server)
	r.registerValidationTools(server)
	r.registerPartialEditTools(server)
	r.registerPageTools(server)
	r.registerSearchTools(server)
	r.registerTagTools(server)
	r.registerPropertyTools(server)
	r.registerLinkTools(server)
	r.registerAssetTools(server, opts)
	r.registerOptionalTools(server, opts)

	return server
}

type optionalToolGate string

const (
	optionalToolGateWorkspaceSync optionalToolGate = "workspace_sync"
	optionalToolGateRevision      optionalToolGate = "revision"
	optionalToolGateLinkRefactor  optionalToolGate = "link_refactor"
)

func optionalToolGatesForOptions(opts httpinternal.RouterOptions) []optionalToolGate {
	gates := []optionalToolGate{optionalToolGateWorkspaceSync, optionalToolGateRevision}
	if opts.EnableLinkRefactor {
		gates = append(gates, optionalToolGateLinkRefactor)
	}
	return gates
}

func (r *Routes) registerOptionalTools(server *sdkmcp.Server, opts httpinternal.RouterOptions) {
	for _, gate := range optionalToolGatesForOptions(opts) {
		switch gate {
		case optionalToolGateWorkspaceSync:
			r.registerWorkspaceSyncTools(server)
		case optionalToolGateRevision:
			r.registerRevisionTools(server, opts)
		case optionalToolGateLinkRefactor:
			r.registerRefactorTools(server)
		}
	}
}

func toolNamesForGate(gate optionalToolGate) []string {
	switch gate {
	case optionalToolGateWorkspaceSync:
		return WorkspaceSyncToolNames()
	case optionalToolGateRevision:
		return RevisionToolNames()
	case optionalToolGateLinkRefactor:
		return LinkRefactorToolNames()
	default:
		return nil
	}
}
