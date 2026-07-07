package http

import (
	"embed"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/perber/wiki/internal/core/assets"
	coreauth "github.com/perber/wiki/internal/core/auth"
	"github.com/perber/wiki/internal/core/shared"
	auth_middleware "github.com/perber/wiki/internal/http/middleware/auth"
	"github.com/perber/wiki/internal/http/middleware/security"
)

//go:embed dist/**
var frontend embed.FS

// EmbedFrontend is a flag to enable or disable embedding the frontend.
// Set to "true" at build time to embed the SPA.
var EmbedFrontend = "false"

// Environment controls gin's run mode ("production" → ReleaseMode).
var Environment = "development"

const DefaultFaviconSVG = `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 100 100"><text y=".9em" font-size="90">🌿</text></svg>`

var (
	frontendSubFS             = fs.Sub
	frontendReadFile          = fs.ReadFile
	customStylesheetRelPathFn = filepath.Rel
)

// slogWriter forwards gin debug output (e.g. route registration) to slog at Debug level.
type slogWriter struct{ logger *slog.Logger }

func (sw *slogWriter) Write(p []byte) (n int, err error) {
	sw.logger.Debug(strings.TrimSpace(string(p)))
	return len(p), nil
}

// slogErrorWriter forwards gin Error logs to slog.
type slogErrorWriter struct{ logger *slog.Logger }

func (sew *slogErrorWriter) Write(p []byte) (n int, err error) {
	sew.logger.Error(strings.TrimSpace(string(p)))
	return len(p), nil
}

func slogRequestLogger() gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		path := c.Request.URL.Path
		if raw := c.Request.URL.RawQuery; raw != "" {
			path += "?" + raw
		}

		c.Next()

		slog.Default().Info("http request",
			"method", c.Request.Method,
			"path", path,
			"status", c.Writer.Status(),
			"latency", time.Since(start),
			"ip", c.ClientIP(),
		)
	}
}

func disableClientCache(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	c.Header("Pragma", "no-cache")
	c.Header("Expires", time.Unix(0, 0).UTC().Format(http.TimeFormat))
}

// HTTPRemoteUserConfig configures reverse-proxy-based authentication.
type HTTPRemoteUserConfig struct {
	Enabled        bool
	HeaderName     string
	TrustedProxies *auth_middleware.TrustedProxies
	UserService    *coreauth.UserService
	LogoutURL      string // optional URL the frontend redirects to after logout
}

// RouterOptions holds global HTTP server configuration shared across all domains.
type RouterOptions struct {
	PublicAccess            bool                 // Whether the wiki allows public read access
	InjectCodeInHeader      string               // Raw HTML/JS code to inject into the <head> tag
	CustomStylesheet        string               // Path to a custom CSS file (resolved by wiki before passing)
	AllowInsecure           bool                 // Whether to allow insecure HTTP connections
	AccessTokenTimeout      time.Duration        // Duration for access token validity
	RefreshTokenTimeout     time.Duration        // Duration for refresh token validity
	HideLinkMetadataSection bool                 // Whether to hide the link metadata section in the frontend UI
	AuthDisabled            bool                 // Whether authentication is disabled
	BasePath                string               // URL prefix when served behind a reverse proxy (e.g. "/wiki")
	MarkdownLinkRootPrefix  string               // Repository-root prefix for absolute Markdown links
	MaxAssetUploadSizeBytes shared.MaxBytes      // Maximum allowed size in bytes for asset uploads
	EnableWorkspaceSync     bool                 // Whether workspace sync capability is exposed to clients
	EnableLinkRefactor      bool                 // Whether the link refactoring feature is enabled in the frontend
	MCPEnabled              bool                 // Whether the local MCP endpoint is enabled
	MCPBindHost             string               // Configured server bind host; empty means MCP was not fully configured
	MCPToolListPageSize     int                  // MCP feature-list page size; 0 uses the production default
	HTTPRemoteUser          HTTPRemoteUserConfig // Reverse-proxy authentication via HTTP header
	DisableRequestLog       bool                 // Whether to suppress per-request access log lines
	DisableFrontendRoutes   bool                 // Whether to suppress embedded frontend/static route wiring
}

func IsLoopbackHost(host string) bool {
	normalized := strings.Trim(strings.TrimSpace(strings.ToLower(host)), "[]")
	switch normalized {
	case "localhost", "127.0.0.1", "::1":
		return true
	}
	if ip := net.ParseIP(normalized); ip != nil {
		return ip.IsLoopback()
	}
	return false
}

func IsLoopbackRemoteAddr(remoteAddr string) bool {
	host := strings.TrimSpace(remoteAddr)
	if parsedHost, _, err := net.SplitHostPort(host); err == nil {
		host = parsedHost
	}
	return IsLoopbackHost(host)
}

func LocalOnlyHandler(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if !IsLoopbackRemoteAddr(req.RemoteAddr) {
			http.NotFound(w, req)
			return
		}
		next.ServeHTTP(w, req)
	})
}

// FrontendConfig carries the minimal runtime data required to serve the embedded SPA.
type FrontendConfig struct {
	// GetSiteName returns the current site name injected into the HTML.
	GetSiteName func() string
	// GetFaviconFile returns the current branding favicon filename for initial HTML rendering.
	GetFaviconFile func() string
	// CustomStylesheetPath is the fully-resolved, validated path to a custom CSS file.
	// Empty string disables custom stylesheet serving.
	CustomStylesheetPath string
	// StorageDir is used to validate that CustomStylesheet in RouterOptions is within the storage dir.
	StorageDir string
}

// NewRouter creates the HTTP engine, builds the shared RouterContext, delegates all
// API and static routes to the provided registrars, and wires up the embedded SPA.
func NewRouter(registrars []RouteRegistrar, frontendCfg FrontendConfig, opts RouterOptions) *gin.Engine {
	opts = routerOptionsWithDefaults(opts)
	configureGinRuntime()

	authCookies := auth_middleware.NewAuthCookies(opts.AllowInsecure, opts.AccessTokenTimeout, opts.RefreshTokenTimeout)
	csrfCookie := security.NewCSRFCookie(opts.AllowInsecure, 3*24*time.Hour)

	engine := gin.New()
	installRouterMiddleware(engine, opts)
	base := engine.Group(opts.BasePath)
	installRemoteUserMiddleware(base, opts.HTTPRemoteUser)

	ctx := RouterContext{
		Engine:      engine,
		Base:        base,
		AuthCookies: authCookies,
		CSRFCookie:  csrfCookie,
		Opts:        opts,
	}

	for _, r := range registrars {
		r.RegisterRoutes(ctx)
	}

	registerFrontendRoutes(engine, base, frontendCfg, opts)

	return engine
}

func routerOptionsWithDefaults(opts RouterOptions) RouterOptions {
	if opts.MaxAssetUploadSizeBytes <= 0 {
		opts.MaxAssetUploadSizeBytes = assets.DefaultMaxUploadSizeBytes
	}
	return opts
}

func configureGinRuntime() {
	if Environment == "production" {
		gin.SetMode(gin.ReleaseMode)
	} else {
		gin.SetMode(gin.DebugMode)
	}

	gin.DefaultWriter = &slogWriter{logger: slog.Default().With("component", "gin")}
	gin.DefaultErrorWriter = &slogErrorWriter{logger: slog.Default().With("component", "gin")}
}

func installRouterMiddleware(engine *gin.Engine, opts RouterOptions) {
	if !opts.DisableRequestLog {
		engine.Use(slogRequestLogger())
	}
	engine.Use(gin.RecoveryWithWriter(gin.DefaultErrorWriter))
}

func installRemoteUserMiddleware(base *gin.RouterGroup, cfg HTTPRemoteUserConfig) {
	if cfg.Enabled {
		base.Use(auth_middleware.InjectRemoteUser(auth_middleware.RemoteUserConfig{
			Enabled:        cfg.Enabled,
			HeaderName:     cfg.HeaderName,
			TrustedProxies: cfg.TrustedProxies,
			UserService:    cfg.UserService,
		}))
	}
}

func registerFrontendRoutes(engine *gin.Engine, base *gin.RouterGroup, frontendCfg FrontendConfig, opts RouterOptions) {
	if opts.DisableFrontendRoutes {
		return
	}

	customStylesheetPath := resolveCustomStylesheetPath(frontendCfg, opts)
	registerCustomStylesheetRoute(base, customStylesheetPath)
	if EmbedFrontend == "true" {
		registerEmbeddedFrontendRoutes(engine, base, frontendCfg, opts, customStylesheetPath)
	}
}

func resolveCustomStylesheetPath(frontendCfg FrontendConfig, opts RouterOptions) string {
	customStylesheetPath := frontendCfg.CustomStylesheetPath
	if customStylesheetPath != "" || opts.CustomStylesheet == "" {
		return customStylesheetPath
	}

	resolved, err := NormalizeCustomStylesheetPath(frontendCfg.StorageDir, opts.CustomStylesheet)
	if err != nil {
		slog.Default().Error("custom stylesheet disabled", "error", err)
		return ""
	}
	return resolved
}

func registerCustomStylesheetRoute(base *gin.RouterGroup, customStylesheetPath string) {
	if customStylesheetPath == "" {
		return
	}

	cssPath := customStylesheetPath
	base.GET("/custom.css", func(c *gin.Context) {
		if _, err := os.Stat(cssPath); os.IsNotExist(err) {
			c.Status(http.StatusNotFound)
			return
		} else if err != nil {
			slog.Default().Error("error checking custom stylesheet existence", "error", err, "path", cssPath)
			c.Status(http.StatusInternalServerError)
			return
		}
		c.Header("Content-Type", "text/css; charset=utf-8")
		c.File(cssPath)
	})
}

func registerEmbeddedFrontendRoutes(engine *gin.Engine, base *gin.RouterGroup, frontendCfg FrontendConfig, opts RouterOptions, customStylesheetPath string) {
	fsys, err := frontendSubFS(frontend, "dist")
	if err != nil {
		panic("failed to create sub FS: " + err.Error())
	}
	staticFS, err := frontendSubFS(frontend, "dist/static")
	if err != nil {
		panic("failed to create sub FS: " + err.Error())
	}

	base.StaticFS("/static", http.FS(staticFS))
	base.GET("/favicon.svg", serveDefaultFavicon)
	engine.NoRoute(frontendNoRouteHandler(fsys, frontendCfg, opts, customStylesheetPath))
}

func serveDefaultFavicon(c *gin.Context) {
	disableClientCache(c)
	// favicon is served by the branding registrar if a custom one exists;
	// fall back to the default leaf SVG.
	c.Data(http.StatusOK, "image/svg+xml", []byte(DefaultFaviconSVG))
}

func frontendNoRouteHandler(fsys fs.FS, frontendCfg FrontendConfig, opts RouterOptions, customStylesheetPath string) gin.HandlerFunc {
	return func(c *gin.Context) {
		path, ok := frontendRequestPath(c.Request.URL.Path, opts.BasePath)
		if !ok || !isFrontendSPARoute(c.Request.Method, path) {
			c.String(http.StatusNotFound, "Page not found")
			return
		}

		serveFrontendIndex(c, fsys, frontendCfg, opts, customStylesheetPath, path)
	}
}

func frontendRequestPath(requestPath string, basePath string) (string, bool) {
	path := requestPath
	if basePath == "" {
		return path, true
	}
	if path != basePath && !strings.HasPrefix(path, basePath+"/") {
		return "", false
	}
	path = strings.TrimPrefix(path, basePath)
	if path == "" {
		path = "/"
	}
	return path, true
}

var frontendSPABlockedExactPaths = map[string]struct{}{
	"/agent-presence": {},
	"/mcp":            {},
}

var frontendSPABlockedPrefixes = []string{
	"/api",
	"/assets",
	"/static",
	"/branding",
	"/.well-known",
	"/agent-presence/",
	"/mcp/",
}

func isFrontendSPARoute(method string, path string) bool {
	if method != http.MethodGet {
		return false
	}
	if _, blocked := frontendSPABlockedExactPaths[path]; blocked {
		return false
	}
	return !hasFrontendSPABlockedPrefix(path)
}

func hasFrontendSPABlockedPrefix(path string) bool {
	for _, prefix := range frontendSPABlockedPrefixes {
		if strings.HasPrefix(path, prefix) {
			return true
		}
	}
	return false
}

func serveFrontendIndex(c *gin.Context, fsys fs.FS, frontendCfg FrontendConfig, opts RouterOptions, customStylesheetPath string, path string) {
	if path == "/oauth/approve" {
		disableClientCache(c)
		c.Header("Content-Security-Policy", "frame-ancestors 'none'")
		c.Header("X-Frame-Options", "DENY")
	}
	c.Writer.Header().Set("Content-Type", "text/html; charset=utf-8")
	data, err := frontendReadFile(fsys, "index.html")
	if err != nil {
		c.Status(http.StatusNotFound)
		return
	}

	html := frontendIndexHTML(string(data), frontendCfg, opts, customStylesheetPath)
	c.Data(http.StatusOK, "text/html; charset=utf-8", []byte(html))
}

func frontendIndexHTML(html string, frontendCfg FrontendConfig, opts RouterOptions, customStylesheetPath string) string {
	html = strings.ReplaceAll(html, "{{__SITE_NAME__}}", frontendSiteName(frontendCfg))
	html = strings.ReplaceAll(html, "{{__BASE_PATH__}}", opts.BasePath)
	html = strings.ReplaceAll(html, "{{__FAVICON_HREF__}}", BuildFrontendFaviconHref(opts.BasePath, frontendFaviconFile(frontendCfg)))
	html = rewriteFrontendStaticPaths(html, opts.BasePath)
	html = injectIntoHead(html, buildCustomStylesheetTag(opts.BasePath, customStylesheetPath))
	return injectIntoHead(html, opts.InjectCodeInHeader)
}

func frontendSiteName(frontendCfg FrontendConfig) string {
	if frontendCfg.GetSiteName == nil {
		return "LeafWiki"
	}
	if name := frontendCfg.GetSiteName(); name != "" {
		return name
	}
	return "LeafWiki"
}

func frontendFaviconFile(frontendCfg FrontendConfig) string {
	if frontendCfg.GetFaviconFile == nil {
		return ""
	}
	return frontendCfg.GetFaviconFile()
}

func rewriteFrontendStaticPaths(html string, basePath string) string {
	if basePath == "" {
		return html
	}
	return strings.ReplaceAll(html, `"/static/`, `"`+basePath+`/static/`)
}

func BuildFrontendFaviconHref(basePath, faviconFile string) string {
	if faviconFile != "" {
		return basePath + "/branding/" + faviconFile
	}

	return basePath + "/favicon.svg"
}

// NormalizeCustomStylesheetPath resolves and validates a CSS path relative to storageDir.
// Returns empty string (no error) if cssPath is empty.
func NormalizeCustomStylesheetPath(storageDir, customStylesheet string) (string, error) {
	cssPath := strings.TrimSpace(customStylesheet)
	if cssPath == "" {
		return "", nil
	}

	if strings.ToLower(filepath.Ext(cssPath)) != ".css" {
		return "", os.ErrPermission
	}

	if !filepath.IsAbs(cssPath) {
		cssPath = filepath.Join(storageDir, cssPath)
	}

	cleanStorageDir := filepath.Clean(storageDir)
	cleanCSSPath := filepath.Clean(cssPath)

	relPath, err := customStylesheetRelPathFn(cleanStorageDir, cleanCSSPath)
	if err != nil {
		return "", err
	}
	if relPath == ".." || strings.HasPrefix(relPath, ".."+string(os.PathSeparator)) {
		return "", os.ErrPermission
	}

	return cleanCSSPath, nil
}

func buildCustomStylesheetTag(basePath, customStylesheet string) string {
	if strings.TrimSpace(customStylesheet) == "" {
		return ""
	}
	return `<link rel="stylesheet" href="` + basePath + `/custom.css">`
}

func injectIntoHead(html, snippet string) string {
	if strings.TrimSpace(snippet) == "" {
		return html
	}
	newHTML := strings.Replace(html, "</head>", "  "+snippet+"\n  </head>", 1)
	if newHTML == html {
		slog.Default().Warn("could not inject code into header", "reason", "</head> tag not found")
	}
	return newHTML
}
