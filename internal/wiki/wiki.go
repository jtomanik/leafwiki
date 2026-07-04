package wiki

import (
	"context"
	"errors"
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
	workspaceSync          workspaceSyncFacade
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

var ErrWikiModeConflict = errors.New("workspace-only and control-plane-only modes cannot be combined")

var (
	newWikiEnsureWorkspaceDirs = ensureWorkspaceDirs
	newWikiInitAuth            = func(w *Wiki, options *WikiOptions) error { return w.initAuth(options) }
	newWikiInitOAuth           = func(w *Wiki, options *WikiOptions) error { return w.initOAuth(options) }
	newWikiInitCoreServices    = func(w *Wiki, options *WikiOptions) error { return w.initCoreServices(options) }
	newWikiInitLinkService     = func(w *Wiki) error { return w.initLinkService() }
	newWikiInitTagsService     = func(w *Wiki) error { return w.initTagsService() }
	newWikiInitProperties      = func(w *Wiki) error { return w.initPropertiesService() }
	newWikiBootstrapIndexes    = func(w *Wiki) { w.bootstrapTagsAndProperties() }
	newWikiInitSearch          = func(w *Wiki) error { return w.initSearch() }
	newWikiInitBranding        = func(w *Wiki) error { return w.initBranding() }
	newWikiEnsureWelcomePage   = func(w *Wiki) error { return w.EnsureWelcomePage() }

	wikiMkdirAll         = os.MkdirAll
	newWikiUserStore     = auth.NewUserStore
	newWikiAPIKeyStore   = auth.NewAPIKeyStore
	wikiInitDefaultAdmin = func(s *auth.UserService, password string) error { return s.InitDefaultAdmin(password) }
	newWikiUserResolver  = auth.NewUserResolver
	newWikiSessionStore  = auth.NewSessionStore
	newWikiOAuthService  = wikioauth.NewService
	newWikiTreeService   = tree.NewTreeServiceWithOptions
	newWikiWorkspaceSync = func(options workspacesync.ServiceOptions) (workspaceSyncFacade, error) {
		return workspacesync.NewService(options)
	}
	newWikiLinksStore         = links.NewLinksStore
	wikiLinksIndexAllPages    = func(s *links.LinkService) error { return s.IndexAllPages() }
	newWikiTagsStore          = tags.NewTagsStore
	newWikiPropertiesStore    = properties.NewPropertiesStore
	wikiRebuildTagsProperties = func(w *Wiki) error { return w.rebuildTagsAndProperties() }
	wikiTagsClearIndex        = func(s *tags.TagsService) error { return s.ClearIndex() }
	wikiPropertiesClearIndex  = func(s *properties.PropertiesService) error { return s.ClearIndex() }
	wikiTreeWalkNodes         = func(s *tree.TreeService, fn func(tree.PageID) error) error { return s.WalkNodes(fn) }
	wikiTreeGetPages          = func(s *tree.TreeService, ids []tree.PageID) ([]*tree.Page, []error) { return s.GetPages(ids) }
	wikiTagsIndexPageContent  = func(s *tags.TagsService, pageID tree.PageID, rawContent string) error {
		return s.IndexPageContent(pageID, rawContent)
	}
	wikiPropsIndexPageContent = func(s *properties.PropertiesService, pageID tree.PageID, rawContent string) error {
		return s.IndexPageContent(pageID, rawContent)
	}
	newWikiSQLiteIndex      = search.NewSQLiteIndex
	wikiSearchIndexAllPages = func(s *pagesave.SearchIndexSideEffect) error { return s.IndexAllPages() }
	newWikiBrandingService  = branding.NewBrandingService
	wikiCloseUserService    = func(s *auth.UserService) error { return s.Close() }
	wikiCloseAPIKeyService  = func(s *auth.APIKeyService) error { return s.Close() }
	wikiCloseLinksService   = func(s *links.LinkService) error { return s.Close() }
	wikiCloseSearchIndex    = func(s *search.SQLiteIndex) error { return s.Close() }
	wikiCreateWelcomePage   = func(w *Wiki, userID tree.UserID, kind *tree.NodeKind) (*wikipages.CreatePageOutput, error) {
		return wikipages.NewCreatePageUseCase(w.tree, w.slug, w.newPageOrchestrator(), w.log).Execute(
			context.Background(),
			wikipages.CreatePageInput{UserID: userID, Title: "Welcome to LeafWiki", Slug: "welcome-to-leafwiki", Kind: kind},
		)
	}
	wikiGetWelcomePage = func(treeService *tree.TreeService, pageID tree.PageID) (*tree.Page, error) {
		return treeService.GetPage(pageID)
	}
	wikiUpdateWelcomePage = func(w *Wiki, userID tree.UserID, page *tree.Page, content *string, kind *tree.NodeKind) (*wikipages.UpdatePageOutput, error) {
		return wikipages.NewUpdatePageUseCase(w.tree, w.slug, w.newPageOrchestrator(), w.log).Execute(
			context.Background(),
			wikipages.UpdatePageInput{UserID: userID, ID: page.ID, Version: page.Version(), Title: page.Title, Slug: page.Slug, Content: content, Kind: kind},
		)
	}
)

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
		return nil, ErrWikiModeConflict
	}
	workspace := resolveWorkspaceOptions(options)
	if err := ValidateWorkspace(workspace); err != nil {
		return nil, err
	}
	if err := newWikiEnsureWorkspaceDirs(workspace); err != nil {
		return nil, err
	}
	w := &Wiki{
		storageDir:             workspace.DataDir,
		workspace:              workspace,
		markdownLinkRootPrefix: options.MarkdownLinkRootPrefix,
		log:                    slog.Default().With("component", "Wiki"),
	}
	if !options.WorkspaceOnly {
		if err := newWikiInitAuth(w, options); err != nil {
			return nil, err
		}
		if err := newWikiInitOAuth(w, options); err != nil {
			return nil, err
		}
	}
	if options.ControlPlaneOnly {
		if err := newWikiInitBranding(w); err != nil {
			return nil, err
		}
		w.buildControlPlaneRoutes(options)
		return w, nil
	}
	if err := newWikiInitCoreServices(w, options); err != nil {
		return nil, err
	}
	if err := newWikiInitLinkService(w); err != nil {
		return nil, err
	}
	if err := newWikiInitTagsService(w); err != nil {
		return nil, err
	}
	if err := newWikiInitProperties(w); err != nil {
		return nil, err
	}
	newWikiBootstrapIndexes(w)
	if err := newWikiInitSearch(w); err != nil {
		return nil, err
	}
	w.configureWorkspaceSyncRebuilder()
	if !options.WorkspaceOnly {
		if err := newWikiInitBranding(w); err != nil {
			return nil, err
		}
	}
	w.webPresence = wikipresence.NewWebPresenceRegistry(wikipresence.DefaultWebPresenceTTL, nil)
	if w.tree.IsLoaded() {
		if err := newWikiEnsureWelcomePage(w); err != nil {
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
	if err := wikiMkdirAll(workspace.DataDir, 0o755); err != nil {
		return fmt.Errorf("create data dir: %w", err)
	}
	if err := wikiMkdirAll(workspace.RootDir, 0o755); err != nil {
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
	store, err := newWikiUserStore(authStorageDir)
	if err != nil {
		return err
	}
	w.user = auth.NewUserService(store)
	apiKeyStore, err := newWikiAPIKeyStore(authStorageDir)
	if err != nil {
		return err
	}
	w.apiKeys = auth.NewAPIKeyService(apiKeyStore, w.user)
	if !options.AuthDisabled {
		if err := wikiInitDefaultAdmin(w.user, options.AdminPassword); err != nil {
			return err
		}
	}
	w.userResolver, err = newWikiUserResolver(w.user)
	if err != nil {
		return err
	}
	if !options.AuthDisabled {
		sessionStore, err := newWikiSessionStore(authStorageDir)
		if err != nil {
			return err
		}
		w.auth = auth.NewAuthService(w.user, sessionStore, options.JWTSecret, options.AccessTokenTimeout, options.RefreshTokenTimeout)
	}
	return nil
}

func (w *Wiki) initOAuth(options *WikiOptions) error {
	service, err := newWikiOAuthService(wikioauth.ServiceConfig{
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
	w.tree = newWikiTreeService(tree.TreeOptions{
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
	service, err := newWikiWorkspaceSync(workspacesync.ServiceOptions{
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
	linksStore, err := newWikiLinksStore(w.storageDir)
	if err != nil {
		return fmt.Errorf("failed to init links store: %w", err)
	}
	w.links = links.NewLinkServiceWithOptions(w.storageDir, w.tree, linksStore, links.LinkServiceOptions{
		MarkdownLinkRootPrefix: w.markdownLinkRootPrefix,
	})
	if err := wikiLinksIndexAllPages(w.links); err != nil {
		w.log.Warn("failed to index links on startup", "error", err)
	}
	return nil
}

func (w *Wiki) initTagsService() error {
	tagsStore, err := newWikiTagsStore(w.storageDir)
	if err != nil {
		return fmt.Errorf("failed to init tags store: %w", err)
	}
	w.tags = tags.NewTagsService(tagsStore)
	return nil
}

func (w *Wiki) initPropertiesService() error {
	propsStore, err := newWikiPropertiesStore(w.storageDir)
	if err != nil {
		return fmt.Errorf("failed to init properties store: %w", err)
	}
	w.props = properties.NewPropertiesService(propsStore)
	return nil
}

// bootstrapTagsAndProperties clears and rebuilds tag and property indexes in a single
// parallel GetPages pass — avoids two sequential ReadPageRaw loops at startup.
func (w *Wiki) bootstrapTagsAndProperties() {
	if err := wikiRebuildTagsProperties(w); err != nil {
		w.log.Warn("failed to rebuild tags/properties during bootstrap", "error", err)
	}
}

func (w *Wiki) rebuildTagsAndProperties() error {
	if err := wikiTagsClearIndex(w.tags); err != nil {
		return err
	}
	if err := wikiPropertiesClearIndex(w.props); err != nil {
		return err
	}
	var ids []tree.PageID
	if err := wikiTreeWalkNodes(w.tree, func(id tree.PageID) error {
		ids = append(ids, id)
		return nil
	}); err != nil {
		return err
	}
	pages, errs := wikiTreeGetPages(w.tree, ids)
	for i, page := range pages {
		if errs[i] != nil {
			w.log.Warn("skipping page during bootstrap", "pageID", ids[i].String(), "error", errs[i])
			continue
		}
		if err := wikiTagsIndexPageContent(w.tags, page.ID, page.RawContent); err != nil {
			w.log.Warn("failed to index tags", "pageID", page.ID, "error", err)
		}
		if err := wikiPropsIndexPageContent(w.props, page.ID, page.RawContent); err != nil {
			w.log.Warn("failed to index properties", "pageID", page.ID, "error", err)
		}
	}
	return nil
}

func (w *Wiki) initSearch() error {
	var err error
	w.searchIndex, err = newWikiSQLiteIndex(w.storageDir)
	if err != nil {
		return fmt.Errorf("failed to init search index: %w", err)
	}
	w.status = search.NewIndexingStatus()
	searchEffect := pagesave.NewSearchIndexSideEffect(w.searchIndex, w.tree, w.log)
	w.log.Info("search indexing started")
	go func() {
		w.status.Start()
		defer w.status.Finish()
		if err := wikiSearchIndexAllPages(searchEffect); err != nil {
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
	w.branding, err = newWikiBrandingService(w.storageDir)
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
	if err := wikiLinksIndexAllPages(w.links); err != nil {
		return fmt.Errorf("rebuild links: %w", err)
	}
	if err := wikiRebuildTagsProperties(w); err != nil {
		return fmt.Errorf("rebuild tags/properties: %w", err)
	}
	searchEffect := pagesave.NewSearchIndexSideEffect(w.searchIndex, w.tree, w.log)
	if err := wikiSearchIndexAllPages(searchEffect); err != nil {
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
	k := tree.NodeKindPage
	systemUserID := tree.UserIDFromString(SYSTEM_USER_ID)
	createOut, err := wikiCreateWelcomePage(w, systemUserID, &k)
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
	current, err := wikiGetWelcomePage(w.tree, p.ID)
	if err != nil {
		return err
	}
	if _, err := wikiUpdateWelcomePage(w, systemUserID, current, &content, &k); err != nil {
		return err
	}

	return nil
}

// ─── Service getters (test infrastructure) ───────────────────────────────────
