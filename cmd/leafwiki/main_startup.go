package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/perber/wiki/internal/agenthooks"
	"github.com/perber/wiki/internal/core/tools"
	authmw "github.com/perber/wiki/internal/http/middleware/auth"
	"github.com/perber/wiki/internal/localization"
	leaflogging "github.com/perber/wiki/internal/logging"
	"github.com/perber/wiki/internal/projectdaemon"
	"github.com/perber/wiki/internal/runtimeconfig"
	"github.com/perber/wiki/internal/wiki"
	"github.com/perber/wiki/internal/wikid"
)

const agentHookMaxPayloadBytes = 1024 * 1024

var (
	errRuntimeActorUserRequired            = errors.New("actor user is required")
	errRuntimeHomeGrantUserRequired        = errors.New("user is required")
	errFrontdMCPTokenInfoMissing           = errors.New("authenticated MCP token info missing")
	errFrontdMCPUserServiceUnavailable     = errors.New("authenticated MCP user service is unavailable")
	errFrontdOAuthActorServicesUnavailable = errors.New("oauth actor services are unavailable")
	errFrontdWorkspaceCredentialsMissing   = errors.New("authenticated workspace request is missing credentials")
	errFrontdRemoteUserServiceUnavailable  = errors.New("remote user service is unavailable")
	errRuntimeWorkspacedURLUnavailable     = errors.New("workspaced URL is unavailable")
	errRuntimeRoleExitedBeforeReadiness    = errors.New("process exited before readiness")
	errRuntimeRoleReadinessTimeout         = errors.New("runtime role did not become ready before timeout")
	errRuntimeRoleInvalidPID               = errors.New("runtime role reported invalid PID")
	errUnsupportedRuntimeRole              = errors.New("unsupported runtime role")
	errNativeStdioAPIKeyRequired           = errors.New("native STDIO requires an API key")
	errNativeStdioWorkspaceAccessDenied    = errors.New("native STDIO API key cannot access workspaces")
	errWorkspaceManagerUnavailable         = errors.New("workspace manager is unavailable")
	errWorkspaceIDRequired                 = errors.New("workspace ID is required")
	errUserHomeEmpty                       = errors.New("user home is empty")
	errAuthJWTSecretRequired               = errors.New("JWT secret is required. Set it using --jwt-secret or LEAFWIKI_JWT_SECRET environment variable.")
	errAuthAdminPasswordRequired           = errors.New("admin password is required. Set it using --admin-password or LEAFWIKI_ADMIN_PASSWORD environment variable.")
	errPrivateMCPURLUntrusted              = errors.New("private MCP URL is not trusted")
	errControlURLUntrusted                 = errors.New("control URL is not trusted")
	errControlHealthUnreachable            = errors.New("control health is unreachable")
	errControlHealthMismatch               = errors.New("control health does not match descriptor")
	errProjectLockedNoAttachableDaemon     = errors.New("project is locked but no attachable daemon was found")
	errRuntimeRoleMissingProcessHandle     = errors.New("runtime role process handle is missing")
	errProjectDaemonStartupFailed          = errors.New("project daemon failed to start")
	errGlobalWikidDescriptorUnavailable    = errors.New("global wikid descriptor is unavailable")
)

type cliFlags struct {
	config                  *string
	host                    *string
	port                    *string
	dataDir                 *string
	rootDir                 *string
	markdownLinkRootPrefix  *string
	adminPassword           *string
	jwtSecret               *string
	publicAccess            *bool
	allowInsecure           *bool
	injectCodeInHeader      *string
	customStylesheet        *string
	logTarget               *string
	logFile                 *string
	disableAuth             *bool
	hideLinkMetadataSection *bool
	accessTokenTimeout      *time.Duration
	refreshTokenTimeout     *time.Duration
	basePath                *string
	maxAssetUploadSize      *string
	enableLinkRefactor      *bool
	mcp                     *string
	apiKey                  *string
	daemonIdleTimeout       *time.Duration
	internalProjectDaemon   *string
	enableHTTPRemoteUser    *bool
	httpRemoteUserHeader    *string
	trustedProxyIPs         *string
	httpRemoteUserLogoutURL *string
	disableRequestLog       *bool
	internalRuntimeRole     *string
}

type leafwikiRuntimeConfig = runtimeconfig.LeafWikiRuntimeConfig

func registerFlags(fs *flag.FlagSet) *cliFlags {
	return &cliFlags{
		config:                  fs.String("config", "", "path to a flat YAML config file; mutually exclusive with other CLI flags"),
		host:                    fs.String("host", "", "host/IP address to bind the server to (e.g. 127.0.0.1 or 0.0.0.0)"),
		port:                    fs.String("port", "", "port to run the server on"),
		dataDir:                 fs.String("data-dir", "", "path to data directory"),
		rootDir:                 fs.String("root-dir", "", "path to managed markdown content directory"),
		markdownLinkRootPrefix:  fs.String("markdown-link-root-prefix", "", "repository-root prefix for absolute Markdown links (for example /docs)"),
		adminPassword:           fs.String("admin-password", "", "initial admin password"),
		jwtSecret:               fs.String("jwt-secret", "", "JWT secret for authentication"),
		publicAccess:            fs.Bool("public-access", false, "allow public access to the wiki with read access (default: false)"),
		allowInsecure:           fs.Bool("allow-insecure", false, "allow insecure HTTP connections (default: false)"),
		injectCodeInHeader:      fs.String("inject-code-in-header", "", "raw string injected into <head> (default: \"\")"),
		customStylesheet:        fs.String("custom-stylesheet", "", "path to a custom CSS file served as /custom.css"),
		logTarget:               fs.String("log-target", "", "log target: file, stderr, or stdout"),
		logFile:                 fs.String("log-file", "", "log file path when --log-target=file"),
		disableAuth:             fs.Bool("disable-auth", false, "disable authentication completely (default: false) (WARNING: only use in trusted networks!)"),
		hideLinkMetadataSection: fs.Bool("hide-link-metadata-section", false, "hide link metadata section (default: false)"),
		accessTokenTimeout:      fs.Duration("access-token-timeout", 15*time.Minute, "access token timeout duration (e.g. 24h, 15m) (default: 15m)"),
		refreshTokenTimeout:     fs.Duration("refresh-token-timeout", 7*24*time.Hour, "refresh token timeout duration (e.g. 168h, 7d) (default: 7d)"),
		basePath:                fs.String("base-path", "", "URL prefix when served behind a reverse proxy (e.g. /wiki)"),
		maxAssetUploadSize:      fs.String("max-asset-upload-size", "", "maximum size for asset uploads (for example 50MiB, 50MB, 52428800)"),
		enableLinkRefactor:      fs.Bool("enable-link-refactor", false, "enable the link refactoring dialog and rewrite flow (default: false)"),
		mcp:                     fs.String("mcp", "", "MCP transports: none, http, stdio, http,stdio, or stdio,http"),
		apiKey:                  fs.String("api-key", "", "native STDIO MCP API key; prefer LEAFWIKI_MCP_API_KEY"),
		daemonIdleTimeout:       fs.Duration("daemon-idle-timeout", projectdaemon.DefaultIdleTimeout, "federated runtime idle timeout after the last session or presence record exits; 0 stops immediately"),
		internalProjectDaemon:   fs.String("internal-project-daemon", "", "internal project daemon startup config path"),
		enableHTTPRemoteUser:    fs.Bool("enable-http-remote-user", false, "enable reverse-proxy authentication via HTTP header (default: false)"),
		httpRemoteUserHeader:    fs.String("http-remote-user-header-name", "Remote-User", "HTTP header name carrying the username from a trusted proxy (default: Remote-User)"),
		trustedProxyIPs:         fs.String("trusted-proxy-ips", "", "comma-separated list of trusted proxy IPs/CIDRs (e.g. 127.0.0.1,172.18.0.0/16)"),
		httpRemoteUserLogoutURL: fs.String("http-remote-user-logout-url", "", "URL the frontend redirects to after logout when reverse-proxy auth is active (e.g. https://auth.example.com/logout)"),
		disableRequestLog:       fs.Bool("disable-request-log", false, "suppress per-request HTTP access log lines (default: false)"),
		internalRuntimeRole:     fs.String("internal-runtime-role", "", "internal runtime role process name"),
	}
}

func main() {
	setupBootstrapLogger(os.Stderr)
	startup, ok := parseStartupCLI(os.Args[1:])
	if !ok {
		return
	}
	if err := rejectRemovedLeafWikiEnv(); err != nil {
		fail(localization.MessageIDCLIErrorInvalidEnvironment, "error", err)
	}
	if runInternalStartupCommand(startup.flags) {
		return
	}

	dataDir := resolveStartupDataDir(startup.flags, startup.visited, startup.serviceModeRequested)
	agentHookRequested := configureAgentHookRequest(startup.args)
	transports := resolveStartupMCPTransports(startup.flags, startup.visited, agentHookRequested)
	validateStartupCommandTransport(startup.serviceModeRequested, transports, startup.args)
	if handleStartupPositionalCommand(startup.args, agentHookRequested, dataDir) {
		return
	}

	cfg := buildRuntimeConfigForStartup(startup.flags, startup.visited, startup.serviceModeRequested, transports, dataDir)
	dispatchRuntimeCommand(startup.args, startup.serviceModeRequested, agentHookRequested, cfg)
}

type startupCLI struct {
	flags                *cliFlags
	visited              map[string]bool
	args                 []string
	serviceModeRequested bool
}

func parseStartupCLI(rawArgs []string) (startupCLI, bool) {
	failOpenAgentHookProvider = ""
	if provider, ok := agentHookProviderFromRawArgs(rawArgs); ok {
		failOpenAgentHookProvider = provider
	}
	originalRawArgs := rawArgs
	rawArgs = normalizeAgentHookRawArgs(rawArgs)
	configModeRequested := rawArgsContainFlag(rawArgs, "config")
	if err := validateRawConfigFlagUsage(originalRawArgs); err != nil {
		failInvalidConfigFile(err)
	}
	if !configModeRequested && shouldPrintUsage(rawArgs) {
		printUsage()
		return startupCLI{}, false
	}

	flag.CommandLine = flag.NewFlagSet(os.Args[0], flag.ContinueOnError)
	flag.CommandLine.SetOutput(os.Stderr)
	flag.CommandLine.Usage = func() {
		writeUsage(flag.CommandLine.Output())
	}

	flags := registerFlags(flag.CommandLine)
	if err := flag.CommandLine.Parse(rawArgs); err != nil {
		if configModeRequested {
			failWithoutAgentHook(localization.MessageIDCLIErrorInvalidConfigArguments, "error", err)
		}
		if failOpenAgentHookProvider != "" {
			fail(localization.MessageIDCLIErrorInvalidAgentHookArguments, "error", err)
		}
		os.Exit(2)
	}

	visited := map[string]bool{}
	flag.CommandLine.Visit(func(f *flag.Flag) { visited[f.Name] = true })
	args := flag.CommandLine.Args()
	if isDaemonHelpCommand(args) {
		printUsage()
		return startupCLI{}, false
	}
	serviceModeRequested := isDaemonCommand(args)
	applyStartupConfig(flag.CommandLine, flags, visited, args, serviceModeRequested)
	return startupCLI{
		flags:                flags,
		visited:              visited,
		args:                 args,
		serviceModeRequested: serviceModeRequested,
	}, true
}

func applyStartupConfig(fs *flag.FlagSet, flags *cliFlags, visited map[string]bool, args []string, serviceModeRequested bool) {
	if visited["config"] {
		if err := validateConfigModeArgs(args); err != nil {
			failInvalidConfigFile(err)
		}
		if err := applyYAMLConfigFile(fs, flags, visited); err != nil {
			failInvalidConfigFile(err)
		}
	}
	if !serviceModeRequested {
		return
	}
	if err := applyDaemonServiceConfig(fs, flags, visited, args); err != nil {
		var missingConfig daemonServiceConfigMissingError
		if errors.As(err, &missingConfig) {
			failWithoutAgentHook(localization.MessageIDCLIErrorServiceConfigRequired, "error", err)
		}
		failWithoutAgentHook(localization.MessageIDCLIErrorInvalidServiceConfigFile, "error", err)
	}
}

func runInternalStartupCommand(flags *cliFlags) bool {
	if strings.TrimSpace(*flags.internalProjectDaemon) != "" {
		if err := runInternalProjectDaemon(context.Background(), *flags.internalProjectDaemon); err != nil {
			fail(localization.MessageIDCLIErrorProjectDaemonFailed, "error", err)
		}
		return true
	}
	if strings.TrimSpace(*flags.internalRuntimeRole) != "" {
		if err := runInternalRuntimeRole(context.Background(), *flags.internalRuntimeRole); err != nil {
			fail(localization.MessageIDCLIErrorRuntimeRoleFailed, "error", err)
		}
		return true
	}
	return false
}

func resolveStartupDataDir(flags *cliFlags, visited map[string]bool, serviceModeRequested bool) string {
	defaultDataDir := "./data"
	if serviceModeRequested && !visited["data-dir"] {
		serviceDataDir, err := defaultDaemonServiceDataDir()
		if err != nil {
			failWithoutAgentHook(localization.MessageIDCLIErrorServiceConfigRequired, "error", err)
		}
		defaultDataDir = serviceDataDir
	}
	return resolveString("data-dir", *flags.dataDir, visited, "LEAFWIKI_DATA_DIR", defaultDataDir)
}

func configureAgentHookRequest(args []string) bool {
	agentHookRequested := isAgentHookCommand(args)
	if !agentHookRequested {
		failOpenAgentHookProvider = ""
		return false
	}
	if provider, ok := agentHookProviderFromArgs(args); ok {
		failOpenAgentHookProvider = provider
	}
	return true
}

func resolveStartupMCPTransports(flags *cliFlags, visited map[string]bool, agentHookRequested bool) mcpTransports {
	if agentHookRequested {
		return mcpTransports{}
	}
	transports, err := resolveMCPTransports(flags, visited)
	if err != nil {
		fail(localization.MessageIDCLIErrorInvalidMCPConfig, "error", err)
	}
	return transports
}

func validateStartupCommandTransport(serviceModeRequested bool, transports mcpTransports, args []string) {
	if serviceModeRequested && transports.Stdio {
		failWithoutAgentHook(localization.MessageIDCLIErrorInvalidServiceConfigFile, "error", fmt.Errorf("leafwiki daemon does not support mcp: stdio; use scripts/run.sh mcp for native STDIO clients"))
	}
	if transports.Stdio && len(args) > 0 {
		fail(localization.MessageIDCLIErrorInvalidNativeSTDIOConfig, "error", fmt.Errorf("native STDIO does not support positional commands"))
	}
}

func handleStartupPositionalCommand(args []string, agentHookRequested bool, dataDir string) bool {
	if len(args) == 0 || agentHookRequested {
		return false
	}
	switch args[0] {
	case "daemon":
		return false
	case "reset-admin-password":
		resetAdminPasswordCommand(dataDir)
		return true
	case "--help", "-h", "help":
		printUsage()
		return true
	default:
		fmt.Printf("%s\n\n", localization.English.Render(localization.MessageIDCLIStatusUnknownCommand, "", args[0]).Message)
		printUsage()
		return true
	}
}

func resetAdminPasswordCommand(dataDir string) {
	resetDataDir := authStorageDirForRuntime(dataDir)
	if err := wikid.CleanupLegacyAuthDBs(dataDir); err != nil {
		fail(localization.MessageIDCLIErrorPasswordResetFailed, "error", err)
	}
	user, err := tools.ResetAdminPassword(resetDataDir)
	if err != nil {
		fail(localization.MessageIDCLIErrorPasswordResetFailed, "error", err)
	}
	fmt.Println(localization.English.Render(localization.MessageIDCLIStatusAdminPasswordReset, "").Message)
	fmt.Println(localization.English.Render(localization.MessageIDCLIStatusAdminPasswordValue, "", user.Username, user.Password).Message)
}

func buildRuntimeConfigForStartup(flags *cliFlags, visited map[string]bool, serviceModeRequested bool, transports mcpTransports, dataDir string) leafwikiRuntimeConfig {
	host := resolveString("host", *flags.host, visited, "LEAFWIKI_HOST", "127.0.0.1")
	port := resolveString("port", *flags.port, visited, "LEAFWIKI_PORT", "8080")
	workspace := resolveWorkspaceForStartup(flags, visited)
	if workspace.ID != "" {
		dataDir = workspace.DataDir
	}
	markdownLinkRootPrefix, err := resolveMarkdownLinkRootPrefix(flags, visited)
	if err != nil {
		fail(localization.MessageIDCLIErrorInvalidMarkdownLinkRootPrefix, "error", err)
	}
	apiKey := ""
	if transports.Stdio {
		apiKey = resolveString("api-key", *flags.apiKey, visited, "LEAFWIKI_MCP_API_KEY", "")
	}
	trustedProxyIPsRaw := resolveString("trusted-proxy-ips", *flags.trustedProxyIPs, visited, "LEAFWIKI_TRUSTED_PROXY_IPS", "")
	validateProxyAuthSettings(trustedProxyIPsRaw, resolveBool("enable-http-remote-user", *flags.enableHTTPRemoteUser, visited, "LEAFWIKI_ENABLE_HTTP_REMOTE_USER"))
	loggingConfig := resolveLoggingConfigForStartup(flags, visited, dataDir)
	disableAuth := resolveBool("disable-auth", *flags.disableAuth, visited, "LEAFWIKI_DISABLE_AUTH")
	validateMCPSettings(transports, disableAuth, loggingConfig.Target, host, apiKey)
	return leafwikiRuntimeConfig{
		Workspace:               workspace,
		Host:                    host,
		Port:                    port,
		AdminPassword:           resolveString("admin-password", *flags.adminPassword, visited, "LEAFWIKI_ADMIN_PASSWORD", ""),
		JWTSecret:               resolveString("jwt-secret", *flags.jwtSecret, visited, "LEAFWIKI_JWT_SECRET", ""),
		PublicAccess:            resolveBool("public-access", *flags.publicAccess, visited, "LEAFWIKI_PUBLIC_ACCESS") || disableAuth,
		AllowInsecure:           resolveBool("allow-insecure", *flags.allowInsecure, visited, "LEAFWIKI_ALLOW_INSECURE"),
		InjectCodeInHeader:      resolveString("inject-code-in-header", *flags.injectCodeInHeader, visited, "LEAFWIKI_INJECT_CODE_IN_HEADER", ""),
		CustomStylesheet:        resolveString("custom-stylesheet", *flags.customStylesheet, visited, "LEAFWIKI_CUSTOM_STYLESHEET", ""),
		Logging:                 loggingConfig,
		DisableAuth:             disableAuth,
		HideLinkMetadataSection: resolveBool("hide-link-metadata-section", *flags.hideLinkMetadataSection, visited, "LEAFWIKI_HIDE_LINK_METADATA_SECTION"),
		AccessTokenTimeout:      resolveDuration("access-token-timeout", *flags.accessTokenTimeout, visited, "LEAFWIKI_ACCESS_TOKEN_TIMEOUT"),
		RefreshTokenTimeout:     resolveDuration("refresh-token-timeout", *flags.refreshTokenTimeout, visited, "LEAFWIKI_REFRESH_TOKEN_TIMEOUT"),
		BasePath:                normalizeBasePath(resolveString("base-path", *flags.basePath, visited, "LEAFWIKI_BASE_PATH", "")),
		MarkdownLinkRootPrefix:  markdownLinkRootPrefix,
		MaxAssetUploadSize: parseByteSize(
			resolveString("max-asset-upload-size", *flags.maxAssetUploadSize, visited, "LEAFWIKI_MAX_ASSET_UPLOAD_SIZE", "50MiB"),
			"max asset upload size",
		),
		EnableWorkspaceSync:     true,
		EnableLinkRefactor:      resolveBool("enable-link-refactor", *flags.enableLinkRefactor, visited, "LEAFWIKI_ENABLE_LINK_REFACTOR"),
		MCPTransports:           transports,
		APIKey:                  apiKey,
		EnableHTTPRemoteUser:    resolveBool("enable-http-remote-user", *flags.enableHTTPRemoteUser, visited, "LEAFWIKI_ENABLE_HTTP_REMOTE_USER"),
		HTTPRemoteUserHeader:    resolveString("http-remote-user-header-name", *flags.httpRemoteUserHeader, visited, "LEAFWIKI_HTTP_REMOTE_USER_HEADER_NAME", "Remote-User"),
		TrustedProxyIPsRaw:      trustedProxyIPsRaw,
		HTTPRemoteUserLogoutURL: resolveString("http-remote-user-logout-url", *flags.httpRemoteUserLogoutURL, visited, "LEAFWIKI_HTTP_REMOTE_USER_LOGOUT_URL", ""),
		DisableRequestLog:       resolveBool("disable-request-log", *flags.disableRequestLog, visited, "LEAFWIKI_DISABLE_REQUEST_LOG"),
		DaemonIdleTimeout:       resolveDuration("daemon-idle-timeout", *flags.daemonIdleTimeout, visited, "LEAFWIKI_DAEMON_IDLE_TIMEOUT"),
		DisableIdleShutdown:     serviceModeRequested,
		RuntimeStack:            projectdaemon.RuntimeStackWikidFrontd,
	}
}

func resolveWorkspaceForStartup(flags *cliFlags, visited map[string]bool) wiki.Workspace {
	workspace, _, err := resolveStartupWorkspace(flags, visited, flag.Args())
	if err != nil {
		fail(localization.MessageIDCLIErrorInvalidWorkspaceConfig, "error", err)
	}
	return workspace
}

func validateProxyAuthSettings(trustedProxyIPsRaw string, enableHTTPRemoteUser bool) {
	if _, err := authmw.ParseTrustedProxies(trustedProxyIPsRaw); err != nil {
		fail(localization.MessageIDCLIErrorInvalidTrustedProxyIPs, "error", err)
	}
	if err := validateHTTPRemoteUserConfig(enableHTTPRemoteUser, trustedProxyIPsRaw); err != nil {
		fail(localization.MessageIDCLIErrorInvalidHTTPRemoteUserConfig, "error", err)
	}
}

func resolveLoggingConfigForStartup(flags *cliFlags, visited map[string]bool, dataDir string) leaflogging.Config {
	loggingConfig, err := resolveLoggingConfig(flags, visited, dataDir)
	if err != nil {
		fail(localization.MessageIDCLIErrorInvalidLoggingConfig, "error", err)
	}
	return loggingConfig
}

func validateMCPSettings(transports mcpTransports, disableAuth bool, logTarget leaflogging.Target, host string, apiKey string) {
	if err := validateMCPTransportOptions(mcpTransportOptions{
		Transports:  transports,
		DisableAuth: disableAuth,
		LogTarget:   logTarget,
		Host:        host,
		APIKey:      apiKey,
	}); err != nil {
		fail(localization.MessageIDCLIErrorInvalidMCPConfig, "error", err)
	}
}

func dispatchRuntimeCommand(args []string, serviceModeRequested bool, agentHookRequested bool, cfg leafwikiRuntimeConfig) {
	if serviceModeRequested {
		if err := runDaemonServiceForDispatch(context.Background(), cfg); err != nil {
			failWithoutAgentHook(localization.MessageIDCLIErrorLeafWikiDaemonFailed, "error", err)
		}
		return
	}
	if agentHookRequested {
		provider := agenthooks.ProviderUnknown
		if len(args) >= 2 {
			provider = agenthooks.ProviderIDFromString(args[1])
		}
		if err := runAgentHookCommandForDispatch(context.Background(), cfg, provider, os.Stdin, os.Stdout); err != nil {
			slog.Default().Warn("Agent hook failed open", "provider", provider, "error", err)
		}
		return
	}
	if err := runProjectDaemonLauncherForDispatch(context.Background(), cfg); err != nil {
		fail(localization.MessageIDCLIErrorLeafWikiStartupFailed, "error", err)
	}
}
