package wiki

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/perber/wiki/internal/branding"
	"github.com/perber/wiki/internal/core/assets"
	"github.com/perber/wiki/internal/core/auth"
	"github.com/perber/wiki/internal/core/shared"
	"github.com/perber/wiki/internal/core/tree"
	"github.com/perber/wiki/internal/links"
	"github.com/perber/wiki/internal/projectdaemon"
	"github.com/perber/wiki/internal/properties"
	"github.com/perber/wiki/internal/search"
	"github.com/perber/wiki/internal/tags"
	wikiassets "github.com/perber/wiki/internal/wiki/assets"
	wikiauth "github.com/perber/wiki/internal/wiki/auth"
	wikibranding "github.com/perber/wiki/internal/wiki/branding"
	wikihealth "github.com/perber/wiki/internal/wiki/health"
	wikiimporter "github.com/perber/wiki/internal/wiki/importer"
	wikilinks "github.com/perber/wiki/internal/wiki/links"
	wikimcp "github.com/perber/wiki/internal/wiki/mcp"
	wikioauth "github.com/perber/wiki/internal/wiki/oauth"
	wikipages "github.com/perber/wiki/internal/wiki/pages"
	"github.com/perber/wiki/internal/wiki/pagesave"
	wikipresence "github.com/perber/wiki/internal/wiki/presence"
	wikiproperties "github.com/perber/wiki/internal/wiki/properties"
	wikirevisions "github.com/perber/wiki/internal/wiki/revisions"
	wikisearch "github.com/perber/wiki/internal/wiki/search"
	wikitags "github.com/perber/wiki/internal/wiki/tags"
	wikiworkspacesync "github.com/perber/wiki/internal/wiki/workspacesync"
	"github.com/perber/wiki/internal/workspacesync"
)

type Wiki struct {
	tree                   *tree.TreeService
	slug                   *tree.SlugService
	auth                   *auth.AuthService
	apiKeys                *auth.APIKeyService
	userResolver           *auth.UserResolver
	user                   *auth.UserService
	asset                  *assets.AssetService
	branding               *branding.BrandingService
	searchIndex            *search.SQLiteIndex
	status                 *search.IndexingStatus
	storageDir             string
	workspace              Workspace
	markdownLinkRootPrefix string
	workspaceSync          *workspacesync.Service
	workspaceSyncCancel    context.CancelFunc
	webPresence            *wikipresence.WebPresenceRegistry
	agentPresence          *projectdaemon.AgentPresenceRegistry

	// Domain route registrars (populated by NewWiki).
	pagesRoutes         *wikipages.Routes
	authRoutes          *wikiauth.Routes
	assetsRoutes        *wikiassets.Routes
	revisionsRoutes     *wikirevisions.Routes
	searchRoutes        *wikisearch.Routes
	linksRoutes         *wikilinks.Routes
	tagsRoutes          *wikitags.Routes
	propertiesRoutes    *wikiproperties.Routes
	brandingRoutes      *wikibranding.Routes
	importerRoutes      *wikiimporter.Routes
	healthRoutes        *wikihealth.Routes
	workspaceSyncRoutes *wikiworkspacesync.Routes
	presenceRoutes      *wikipresence.Routes
	mcpRoutes           *wikimcp.Routes
	oauthRoutes         *wikioauth.Routes
	links               *links.LinkService
	tags                *tags.TagsService
	props               *properties.PropertiesService
	oauth               *wikioauth.Service
	log                 *slog.Logger
}

const SYSTEM_USER_ID = "system"

const workspaceSyncStartupPhaseOpenService = "open_service"

type WikiOptions struct {
	Workspace               Workspace
	StorageDir              string          // Path to storage directory
	AuthStorageDir          string          // Optional path for user/session/API-key stores
	WorkspaceOnly           bool            // Skip identity, OAuth, API-key, session, and branding stores
	ControlPlaneOnly        bool            // Skip workspace services while keeping identity, OAuth, and branding routes
	AdminPassword           string          // Initial admin password
	JWTSecret               string          // JWT secret for authentication
	AccessTokenTimeout      time.Duration   // Access token timeout duration
	RefreshTokenTimeout     time.Duration   // Refresh token timeout duration
	AuthDisabled            bool            // Whether authentication is disabled
	MaxAssetUploadSizeBytes shared.MaxBytes // Maximum allowed size in bytes for asset/import uploads; 0 = default
	MarkdownLinkRootPrefix  string          // Repository-root prefix for absolute Markdown links
}

func NewWiki(options *WikiOptions) (*Wiki, error) {
	if options.WorkspaceOnly && options.ControlPlaneOnly {
		return nil, fmt.Errorf("workspace-only and control-plane-only modes cannot be combined")
	}
	workspace := resolveWorkspaceOptions(options)
	if err := ValidateWorkspace(workspace); err != nil {
		return nil, err
	}
	if err := ensureWorkspaceDirs(workspace); err != nil {
		return nil, err
	}
	w := &Wiki{
		storageDir:             workspace.DataDir,
		workspace:              workspace,
		markdownLinkRootPrefix: options.MarkdownLinkRootPrefix,
		log:                    slog.Default().With("component", "Wiki"),
	}
	if !options.WorkspaceOnly {
		if err := w.initAuth(options); err != nil {
			return nil, err
		}
		if err := w.initOAuth(options); err != nil {
			return nil, err
		}
	}
	if options.ControlPlaneOnly {
		if err := w.initBranding(); err != nil {
			return nil, err
		}
		w.buildControlPlaneRoutes(options)
		return w, nil
	}
	if err := w.initCoreServices(options); err != nil {
		return nil, err
	}
	if err := w.initLinkService(); err != nil {
		return nil, err
	}
	if err := w.initTagsService(); err != nil {
		return nil, err
	}
	if err := w.initPropertiesService(); err != nil {
		return nil, err
	}
	w.bootstrapTagsAndProperties()
	if err := w.initSearch(); err != nil {
		return nil, err
	}
	w.configureWorkspaceSyncRebuilder()
	if !options.WorkspaceOnly {
		if err := w.initBranding(); err != nil {
			return nil, err
		}
	}
	w.webPresence = wikipresence.NewWebPresenceRegistry(wikipresence.DefaultWebPresenceTTL, nil)
	if w.tree.IsLoaded() {
		if err := w.EnsureWelcomePage(); err != nil {
			return nil, err
		}
	} else {
		w.log.Warn("skipping welcome page creation because workspace sync validation left the tree unloaded")
	}
	w.buildRoutes(options)
	w.startWorkspaceSyncWatcher()
	return w, nil
}

func resolveWorkspaceOptions(options *WikiOptions) Workspace {
	if options.Workspace.DataDir != "" {
		return NormalizeWorkspace(options.Workspace)
	}
	return DefaultWorkspace(options.StorageDir)
}

func ensureWorkspaceDirs(workspace Workspace) error {
	if err := os.MkdirAll(workspace.DataDir, 0o755); err != nil {
		return fmt.Errorf("create data dir: %w", err)
	}
	if err := os.MkdirAll(workspace.RootDir, 0o755); err != nil {
		return fmt.Errorf("create root dir: %w", err)
	}
	return nil
}

// ─── Subsystem initializers ───────────────────────────────────────────────────

func (w *Wiki) initAuth(options *WikiOptions) error {
	authStorageDir := w.storageDir
	if strings.TrimSpace(options.AuthStorageDir) != "" {
		authStorageDir = options.AuthStorageDir
	}
	store, err := auth.NewUserStore(authStorageDir)
	if err != nil {
		return err
	}
	w.user = auth.NewUserService(store)
	apiKeyStore, err := auth.NewAPIKeyStore(authStorageDir)
	if err != nil {
		return err
	}
	w.apiKeys = auth.NewAPIKeyService(apiKeyStore, w.user)
	if !options.AuthDisabled {
		if err := w.user.InitDefaultAdmin(options.AdminPassword); err != nil {
			return err
		}
	}
	w.userResolver, err = auth.NewUserResolver(w.user)
	if err != nil {
		return err
	}
	if !options.AuthDisabled {
		sessionStore, err := auth.NewSessionStore(authStorageDir)
		if err != nil {
			return err
		}
		w.auth = auth.NewAuthService(w.user, sessionStore, options.JWTSecret, options.AccessTokenTimeout, options.RefreshTokenTimeout)
	}
	return nil
}

func (w *Wiki) initOAuth(options *WikiOptions) error {
	service, err := wikioauth.NewService(wikioauth.ServiceConfig{
		AuthService:         w.auth,
		UserService:         w.user,
		AccessTokenTimeout:  options.AccessTokenTimeout,
		RefreshTokenTimeout: options.RefreshTokenTimeout,
	})
	if err != nil {
		return err
	}
	w.oauth = service
	return nil
}

func (w *Wiki) initCoreServices(_ *WikiOptions) error {
	w.tree = tree.NewTreeServiceWithOptions(tree.TreeOptions{
		DataDir: w.workspace.DataDir,
		RootDir: w.workspace.RootDir,
	})
	workspaceSyncLog := w.log.With("subsystem", "workspaceSync")
	phaseStarted := time.Now()
	workspaceSyncLog.Info("workspace sync startup phase started",
		"phase", workspaceSyncStartupPhaseOpenService,
		"data_dir", w.workspace.DataDir,
		"root_dir", w.workspace.RootDir,
	)
	service, err := workspacesync.NewService(workspacesync.ServiceOptions{
		Enabled:                true,
		DataDir:                w.workspace.DataDir,
		RootDir:                w.workspace.RootDir,
		MarkdownLinkRootPrefix: w.markdownLinkRootPrefix,
		Tree:                   w.tree,
		Log:                    workspaceSyncLog,
	})
	if err != nil {
		workspaceSyncLog.Error("workspace sync startup phase failed",
			"phase", workspaceSyncStartupPhaseOpenService,
			"duration", time.Since(phaseStarted),
			"error", err,
		)
		return err
	}
	workspaceSyncLog.Info("workspace sync startup phase completed",
		"phase", workspaceSyncStartupPhaseOpenService,
		"duration", time.Since(phaseStarted),
	)
	w.workspaceSync = service
	if _, err := w.workspaceSync.SyncNow(context.Background(), workspacesync.SyncRequest{
		Reason: workspacesync.ReasonStartup,
		Source: workspacesync.SourceFilesystem,
		Actor:  workspacesync.PublicEditorActor(),
	}); err != nil {
		return err
	}
	w.slug = tree.NewSlugService()
	w.asset = assets.NewAssetService(w.storageDir, w.slug)
	return nil
}

func (w *Wiki) initLinkService() error {
	linksStore, err := links.NewLinksStore(w.storageDir)
	if err != nil {
		return fmt.Errorf("failed to init links store: %w", err)
	}
	w.links = links.NewLinkServiceWithOptions(w.storageDir, w.tree, linksStore, links.LinkServiceOptions{
		MarkdownLinkRootPrefix: w.markdownLinkRootPrefix,
	})
	if err := w.links.IndexAllPages(); err != nil {
		w.log.Warn("failed to index links on startup", "error", err)
	}
	return nil
}

func (w *Wiki) initTagsService() error {
	tagsStore, err := tags.NewTagsStore(w.storageDir)
	if err != nil {
		return fmt.Errorf("failed to init tags store: %w", err)
	}
	w.tags = tags.NewTagsService(tagsStore)
	return nil
}

func (w *Wiki) initPropertiesService() error {
	propsStore, err := properties.NewPropertiesStore(w.storageDir)
	if err != nil {
		return fmt.Errorf("failed to init properties store: %w", err)
	}
	w.props = properties.NewPropertiesService(propsStore)
	return nil
}

// bootstrapTagsAndProperties clears and rebuilds tag and property indexes in a single
// parallel GetPages pass — avoids two sequential ReadPageRaw loops at startup.
func (w *Wiki) bootstrapTagsAndProperties() {
	if err := w.rebuildTagsAndProperties(); err != nil {
		w.log.Warn("failed to rebuild tags/properties during bootstrap", "error", err)
	}
}

func (w *Wiki) rebuildTagsAndProperties() error {
	if err := w.tags.ClearIndex(); err != nil {
		return err
	}
	if err := w.props.ClearIndex(); err != nil {
		return err
	}
	var ids []tree.PageID
	if err := w.tree.WalkNodes(func(id tree.PageID) error {
		ids = append(ids, id)
		return nil
	}); err != nil {
		return err
	}
	pages, errs := w.tree.GetPages(ids)
	for i, page := range pages {
		if errs[i] != nil {
			w.log.Warn("skipping page during bootstrap", "pageID", ids[i].String(), "error", errs[i])
			continue
		}
		if err := w.tags.IndexPageContent(page.ID, page.RawContent); err != nil {
			w.log.Warn("failed to index tags", "pageID", page.ID, "error", err)
		}
		if err := w.props.IndexPageContent(page.ID, page.RawContent); err != nil {
			w.log.Warn("failed to index properties", "pageID", page.ID, "error", err)
		}
	}
	return nil
}

func (w *Wiki) initSearch() error {
	var err error
	w.searchIndex, err = search.NewSQLiteIndex(w.storageDir)
	if err != nil {
		return fmt.Errorf("failed to init search index: %w", err)
	}
	w.status = search.NewIndexingStatus()
	searchEffect := pagesave.NewSearchIndexSideEffect(w.searchIndex, w.tree, w.log)
	w.log.Info("search indexing started")
	go func() {
		w.status.Start()
		defer w.status.Finish()
		if err := searchEffect.IndexAllPages(); err != nil {
			w.log.Warn("search bootstrap failed", "error", err)
			w.status.Fail()
		} else {
			w.log.Info("search indexing completed")
			w.status.Success()
		}
	}()
	return nil
}

func (w *Wiki) initBranding() error {
	var err error
	w.branding, err = branding.NewBrandingService(w.storageDir)
	if err != nil {
		return fmt.Errorf("failed to init branding service: %w", err)
	}
	return nil
}

func (w *Wiki) configureWorkspaceSyncRebuilder() {
	if w.workspaceSync == nil {
		return
	}
	w.workspaceSync.SetAfterSync(w.rebuildDerivedIndexes)
}

func (w *Wiki) rebuildDerivedIndexes() error {
	if err := w.links.IndexAllPages(); err != nil {
		return fmt.Errorf("rebuild links: %w", err)
	}
	if err := w.rebuildTagsAndProperties(); err != nil {
		return fmt.Errorf("rebuild tags/properties: %w", err)
	}
	searchEffect := pagesave.NewSearchIndexSideEffect(w.searchIndex, w.tree, w.log)
	if err := searchEffect.IndexAllPages(); err != nil {
		return fmt.Errorf("rebuild search: %w", err)
	}
	return nil
}

func (w *Wiki) startWorkspaceSyncWatcher() {
	if w.workspaceSync == nil {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	w.workspaceSyncCancel = cancel
	if err := w.workspaceSync.StartWatcher(ctx); err != nil {
		w.log.Warn("workspace sync watcher failed to start", "error", err)
	}
}

func (w *Wiki) EnsureWelcomePage() error {
	if w.tree.HasPages() {
		w.log.Info("Welcome page already exists, skipping creation")
		return nil
	}
	o := w.newPageOrchestrator()
	k := tree.NodeKindPage
	systemUserID := tree.UserIDFromString(SYSTEM_USER_ID)
	createOut, err := wikipages.NewCreatePageUseCase(w.tree, w.slug, o, w.log).Execute(
		context.Background(),
		wikipages.CreatePageInput{UserID: systemUserID, Title: "Welcome to LeafWiki", Slug: "welcome-to-leafwiki", Kind: &k},
	)
	if err != nil {
		return err
	}
	p := createOut.Page

	// Set the content of the welcome page
	content := `# Welcome to LeafWiki!

LeafWiki – A fast wiki for people who think in folders, not feeds.
Single Go binary. Markdown on disk. No external database service.

LeafWiki is a lightweight, self-hosted wiki for runbooks, internal docs, and technical notes — built for fast writing and explicit structure. It keeps your content as plain Markdown on disk and gives you fast navigation, search, and editing — without running additional services.


---

## Features

- **Markdown-based** pages stored on disk (no database required)
- **Hierarchical navigation** with sections and pages
- **Full-text search** powered by SQLite FTS5
- **Asset management** (upload, rename, delete attachments)
- **Revision history** with snapshots and restore
- **Import** from Markdown zip archives
- **Branding** customization (site name, logo, favicon)
- **Multi-user** with role-based access control (admin / editor / viewer)
- **Public access mode** for read-only anonymous browsing

## Getting Started

1. Create your first page using the **+** button in the sidebar
2. Write in **Markdown** — headings, lists, code blocks, and links are all supported
3. Use **sections** to group related pages into a folder-like hierarchy
4. Upload files by dragging them into the editor

For more information, visit the [LeafWiki GitHub repository](https://github.com/perber/leafwiki).
`
	current, err := w.tree.GetPage(p.ID)
	if err != nil {
		return err
	}
	if _, err := wikipages.NewUpdatePageUseCase(w.tree, w.slug, o, w.log).Execute(
		context.Background(),
		wikipages.UpdatePageInput{UserID: systemUserID, ID: p.ID, Version: current.Version(), Title: p.Title, Slug: p.Slug, Content: &content, Kind: &k},
	); err != nil {
		return err
	}

	return nil
}

// ─── Service getters (test infrastructure) ───────────────────────────────────

func (w *Wiki) GetStorageDir() string {
	return w.storageDir
}

func (w *Wiki) GetRootDir() string {
	return w.workspace.RootDir
}

func (w *Wiki) Workspace() Workspace {
	return w.workspace
}

func (w *Wiki) UserService() *auth.UserService {
	return w.user
}

func (w *Wiki) AuthService() *auth.AuthService {
	return w.auth
}

func (w *Wiki) APIKeyService() *auth.APIKeyService {
	return w.apiKeys
}

func (w *Wiki) OAuthService() *wikioauth.Service {
	return w.oauth
}

func (w *Wiki) Close() error {
	if w.workspaceSyncCancel != nil {
		w.workspaceSyncCancel()
	}
	if w.workspaceSync != nil {
		w.workspaceSync.StopWatcher()
	}
	if w.status != nil {
		w.status.Finish()
	}
	if w.user != nil {
		if err := w.user.Close(); err != nil {
			return err
		}
	}
	if w.apiKeys != nil {
		if err := w.apiKeys.Close(); err != nil {
			return err
		}
	}

	if w.links != nil {
		if err := w.links.Close(); err != nil {
			w.log.Error("error closing links", "error", err)
		}
	}

	if w.searchIndex != nil {
		return w.searchIndex.Close()
	}
	return nil
}
