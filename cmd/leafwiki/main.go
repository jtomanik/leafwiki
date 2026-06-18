package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"math"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/dustin/go-humanize"
	sdkauth "github.com/modelcontextprotocol/go-sdk/auth"
	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/perber/wiki/internal/agenthooks"
	corebranding "github.com/perber/wiki/internal/branding"
	coreauth "github.com/perber/wiki/internal/core/auth"
	"github.com/perber/wiki/internal/core/markdownlinks"
	"github.com/perber/wiki/internal/core/tools"
	"github.com/perber/wiki/internal/frontd"
	httpinternal "github.com/perber/wiki/internal/http"
	authmw "github.com/perber/wiki/internal/http/middleware/auth"
	"github.com/perber/wiki/internal/locking"
	leaflogging "github.com/perber/wiki/internal/logging"
	"github.com/perber/wiki/internal/projectdaemon"
	"github.com/perber/wiki/internal/runtimeconfig"
	"github.com/perber/wiki/internal/wiki"
	wikioauth "github.com/perber/wiki/internal/wiki/oauth"
	"github.com/perber/wiki/internal/wikid"
	"github.com/perber/wiki/internal/workspaced"
	"golang.org/x/sync/singleflight"
)

func writeUsage(w io.Writer) {
	if _, err := fmt.Fprintln(w, `LeafWiki – lightweight selfhosted wiki 🌿

	Usage:
	leafwiki --jwt-secret <SECRET> --admin-password <PASSWORD> [--host <HOST>] [--port <PORT>] [--data-dir <DIR>] [--root-dir <DIR>]
	leafwiki --disable-auth [--host <HOST>] [--port <PORT>] [--data-dir <DIR>] [--root-dir <DIR>]
	leafwiki --mcp=stdio --disable-auth [--host <HOST>] [--port <PORT>] [--data-dir <DIR>] [--root-dir <DIR>]
	leafwiki agent-hook <codex|claude|cursor|unknown> [--host <HOST>] [--port <PORT>] [--data-dir <DIR>] [--root-dir <DIR>]
	leafwiki daemon
	leafwiki reset-admin-password
	leafwiki --help

	Service mode:
	leafwiki daemon reads ~/.leafwiki/leafwiki.yml and runs the install-wide wikid/frontd/workspaced runtime in the foreground.

	Options:
	--host             Host/IP address to bind the server to (default: 127.0.0.1)
	--port             Port to run the server on (default: 8080)
	--data-dir         Path to data directory (default: ./data)
	--root-dir         Path to managed markdown content directory (default: <data-dir>/root)
	--markdown-link-root-prefix Repository-root prefix for absolute Markdown links (for example /docs) (default: "")
	--admin-password   Initial admin password (used only if no admin exists)
	--jwt-secret       Secret for signing auth tokens (JWT) (required)
	--public-access    Allow public access to the wiki only with read access (default: false)
	--allow-insecure   Allow insecure HTTP connections (default: false)                      
	--access-token-timeout  Access token timeout duration (e.g. 24h, 15m) (default: 15m)
	--refresh-token-timeout Refresh token timeout duration (e.g. 168h, 7d) (default: 7d)
	--inject-code-in-header  Raw HTML/JS code injected into <head> tag (e.g., analytics, custom CSS) (default: "")
	                         WARNING: Use only with trusted code to avoid XSS vulnerabilities. No sanitization is performed.
	--custom-stylesheet      Path to a .css file inside the data dir, served publicly as /custom.css
	                         (or <base-path>/custom.css when --base-path is set) (default: "")
	--log-target             Log target: file, stderr, or stdout (default: file)
	--log-file               Log file path when --log-target=file; relative paths resolve under --data-dir
	                         (default: <data-dir>/.leafwiki/logs/leafwiki.log)
	--disable-auth                Disable authentication completely (default: false) (WARNING: only use in trusted networks!)
	--hide-link-metadata-section  Hide link metadata section in the frontend UI (default: false)
	--base-path                   URL prefix when served behind a reverse proxy (e.g. /wiki) (default: "")
	--max-asset-upload-size       Maximum size for asset uploads (for example 50MiB, 50MB, 52428800) (default: 50MiB)
	--enable-revision             Enable the revision / page history feature (default: false)
	--enable-workspace-sync       Enable workspace sync and Git-backed Markdown history (default: false)
	--enable-link-refactor        Enable the link refactoring dialog and rewrite flow (default: false)
	--mcp                         MCP transports: none, http, stdio, http,stdio, or stdio,http (default: none)
	--api-key                     Native STDIO MCP API key convenience flag; prefer LEAFWIKI_MCP_API_KEY
	--daemon-idle-timeout         Federated runtime idle timeout after the last session or presence record exits; 0 stops immediately (default: 10m)
	--max-revision-history        Maximum revisions kept per page; 0 = unlimited (default: 100)
	--enable-http-remote-user       Enable reverse-proxy authentication via HTTP header (default: false)
	--http-remote-user-header-name  HTTP header carrying the username from a trusted proxy (default: Remote-User)
	--trusted-proxy-ips             Comma-separated trusted proxy IPs/CIDRs (e.g. 127.0.0.1,172.18.0.0/16)
	--http-remote-user-logout-url   URL the frontend redirects to after logout in proxy-auth mode (default: "")
	--disable-request-log           Suppress per-request HTTP access log lines (default: false)
	--config                        Path to flat YAML config file; mutually exclusive with other CLI flags

	Environment variables:
	LEAFWIKI_HOST
	LEAFWIKI_PORT
	LEAFWIKI_DATA_DIR
	LEAFWIKI_ROOT_DIR
	LEAFWIKI_MARKDOWN_LINK_ROOT_PREFIX
	LEAFWIKI_JWT_SECRET
	LEAFWIKI_LOG_LEVEL
	LEAFWIKI_ADMIN_PASSWORD
	LEAFWIKI_PUBLIC_ACCESS
	LEAFWIKI_ALLOW_INSECURE
	LEAFWIKI_INJECT_CODE_IN_HEADER
	LEAFWIKI_CUSTOM_STYLESHEET
	LEAFWIKI_LOG_TARGET
	LEAFWIKI_LOG_FILE
	LEAFWIKI_ACCESS_TOKEN_TIMEOUT
	LEAFWIKI_REFRESH_TOKEN_TIMEOUT
	LEAFWIKI_DISABLE_AUTH
	LEAFWIKI_HIDE_LINK_METADATA_SECTION
	LEAFWIKI_BASE_PATH
	LEAFWIKI_MAX_ASSET_UPLOAD_SIZE
	LEAFWIKI_ENABLE_REVISION
	LEAFWIKI_ENABLE_WORKSPACE_SYNC
	LEAFWIKI_ENABLE_LINK_REFACTOR
	LEAFWIKI_MCP
	LEAFWIKI_MCP_API_KEY
	LEAFWIKI_DAEMON_IDLE_TIMEOUT
	LEAFWIKI_MAX_REVISION_HISTORY
	LEAFWIKI_ENABLE_HTTP_REMOTE_USER
	LEAFWIKI_HTTP_REMOTE_USER_HEADER_NAME
	LEAFWIKI_TRUSTED_PROXY_IPS
	LEAFWIKI_HTTP_REMOTE_USER_LOGOUT_URL
	LEAFWIKI_DISABLE_REQUEST_LOG
	`); err != nil {
		panic(err)
	}
}

func printUsage() {
	writeUsage(os.Stdout)
}

func setupBootstrapLogger(stderr io.Writer) {
	handler := slog.NewJSONHandler(stderr, &slog.HandlerOptions{
		Level:     slog.LevelInfo,
		AddSource: true,
	})

	slog.SetDefault(slog.New(handler))
}

func setupLogger(cfg leaflogging.Config, stdout io.Writer, stderr io.Writer) (io.Closer, error) {
	logger, closer, err := leaflogging.Open(cfg, leaflogging.Streams{
		Stdout: stdout,
		Stderr: stderr,
	})
	if err != nil {
		return nil, err
	}
	slog.SetDefault(logger)
	return closer, nil
}

var failOpenAgentHookProvider string

type configFlagMixError = runtimeconfig.ConfigFlagMixError
type configUsageError = runtimeconfig.ConfigUsageError
type daemonServiceConfigMissingError = runtimeconfig.DaemonServiceConfigMissingError

func fail(msg string, args ...any) {
	if failOpenAgentHookProvider != "" {
		provider := failOpenAgentHookProvider
		slog.Default().Warn("Agent hook failed open", "provider", provider, "reason", msg)
		if allowResponse := agenthooks.AllowResponse(provider); len(allowResponse) > 0 {
			_, _ = os.Stdout.Write(allowResponse)
		}
		os.Exit(0)
	}
	slog.Default().Error(msg, args...)
	fmt.Fprintln(os.Stderr, failureMessage(msg, args...))
	os.Exit(1)
}

func failInvalidConfigFile(err error) {
	var mixErr configFlagMixError
	var usageErr configUsageError
	if errors.As(err, &mixErr) || errors.As(err, &usageErr) {
		failWithoutAgentHook("Invalid config file", "error", err)
	}
	fail("Invalid config file", "error", err)
}

func failWithoutAgentHook(msg string, args ...any) {
	slog.Default().Error(msg, args...)
	fmt.Fprintln(os.Stderr, failureMessage(msg, args...))
	os.Exit(1)
}

func failureMessage(msg string, args ...any) string {
	var b strings.Builder
	b.WriteString(msg)
	for i := 0; i+1 < len(args); i += 2 {
		b.WriteByte(' ')
		b.WriteString(fmt.Sprint(args[i]))
		b.WriteByte('=')
		b.WriteString(fmt.Sprint(args[i+1]))
	}
	return b.String()
}

var projectDaemonExecutable = os.Executable

var projectDaemonStartupConfigPostStartCleanupDelay = 30 * time.Second

const agentHookMaxPayloadBytes = 1024 * 1024

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
	enableRevision          *bool
	enableWorkspaceSync     *bool
	enableLinkRefactor      *bool
	mcp                     *string
	apiKey                  *string
	daemonIdleTimeout       *time.Duration
	internalProjectDaemon   *string
	enableMCP               *bool
	mcpStdio                *bool
	maxRevisionHistory      *int
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
		enableRevision:          fs.Bool("enable-revision", false, "enable the revision / page history feature (default: false)"),
		enableWorkspaceSync:     fs.Bool("enable-workspace-sync", false, "enable workspace sync and Git-backed Markdown history (default: false)"),
		enableLinkRefactor:      fs.Bool("enable-link-refactor", false, "enable the link refactoring dialog and rewrite flow (default: false)"),
		mcp:                     fs.String("mcp", "", "MCP transports: none, http, stdio, http,stdio, or stdio,http"),
		apiKey:                  fs.String("api-key", "", "native STDIO MCP API key; prefer LEAFWIKI_MCP_API_KEY"),
		daemonIdleTimeout:       fs.Duration("daemon-idle-timeout", projectdaemon.DefaultIdleTimeout, "federated runtime idle timeout after the last session or presence record exits; 0 stops immediately"),
		internalProjectDaemon:   fs.String("internal-project-daemon", "", "internal project daemon startup config path"),
		enableMCP:               fs.Bool("enable-mcp", false, "compatibility flag for local MCP Streamable HTTP endpoint"),
		mcpStdio:                fs.Bool("mcp-stdio", false, "compatibility flag for native MCP STDIO"),
		maxRevisionHistory:      fs.Int("max-revision-history", 100, "maximum revisions kept per page; 0 = unlimited (default: 100)"),
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
			failWithoutAgentHook("Invalid config arguments", "error", err)
		}
		if failOpenAgentHookProvider != "" {
			fail("Invalid agent hook arguments", "error", err)
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
			failWithoutAgentHook("Service config file is required for service mode", "error", err)
		}
		failWithoutAgentHook("Invalid service config file", "error", err)
	}
}

func runInternalStartupCommand(flags *cliFlags) bool {
	if strings.TrimSpace(*flags.internalProjectDaemon) != "" {
		if err := runInternalProjectDaemon(context.Background(), *flags.internalProjectDaemon); err != nil {
			fail("Project daemon failed", "error", err)
		}
		return true
	}
	if strings.TrimSpace(*flags.internalRuntimeRole) != "" {
		if err := runInternalRuntimeRole(context.Background(), *flags.internalRuntimeRole); err != nil {
			fail("Runtime role failed", "error", err)
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
			failWithoutAgentHook("Service config file is required for service mode", "error", err)
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
		fail("Invalid MCP configuration", "error", err)
	}
	return transports
}

func validateStartupCommandTransport(serviceModeRequested bool, transports mcpTransports, args []string) {
	if serviceModeRequested && transports.Stdio {
		failWithoutAgentHook("Invalid service config file", "error", fmt.Errorf("leafwiki daemon does not support mcp: stdio; use scripts/run.sh mcp for native STDIO clients"))
	}
	if transports.Stdio && len(args) > 0 {
		fail("Invalid native STDIO configuration", "error", fmt.Errorf("native STDIO does not support positional commands"))
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
		fmt.Printf("Unknown command: %s\n\n", args[0])
		printUsage()
		return true
	}
}

func resetAdminPasswordCommand(dataDir string) {
	runtimeStack, stackErr := resolveRuntimeStack()
	if stackErr != nil {
		fail("Invalid runtime stack", "error", stackErr)
	}
	resetDataDir := authStorageDirForRuntime(dataDir, runtimeStack)
	if runtimeStack == projectdaemon.RuntimeStackWikidFrontd {
		if err := wikid.CleanupLegacyAuthDBs(dataDir); err != nil {
			fail("Password reset failed", "error", err)
		}
	}
	user, err := tools.ResetAdminPassword(resetDataDir)
	if err != nil {
		fail("Password reset failed", "error", err)
	}
	fmt.Println("Admin password reset successfully.")
	fmt.Printf("New password for user %s: %s\n", user.Username, user.Password)
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
		fail("Invalid markdown link root prefix", "error", err)
	}
	enableRevision := resolveBool("enable-revision", *flags.enableRevision, visited, "LEAFWIKI_ENABLE_REVISION")
	enableWorkspaceSync := resolveBool("enable-workspace-sync", *flags.enableWorkspaceSync, visited, "LEAFWIKI_ENABLE_WORKSPACE_SYNC")
	if enableRevision && enableWorkspaceSync {
		fail("Invalid revision configuration", "error", fmt.Errorf("enable-revision and enable-workspace-sync cannot be combined"))
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
	runtimeStack := resolveRuntimeStackForStartup(serviceModeRequested)

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
		EnableRevision:          enableRevision,
		EnableWorkspaceSync:     enableWorkspaceSync,
		EnableLinkRefactor:      resolveBool("enable-link-refactor", *flags.enableLinkRefactor, visited, "LEAFWIKI_ENABLE_LINK_REFACTOR"),
		MCPTransports:           transports,
		APIKey:                  apiKey,
		MaxRevisionHistory:      resolveInt("max-revision-history", *flags.maxRevisionHistory, visited, "LEAFWIKI_MAX_REVISION_HISTORY", 100),
		EnableHTTPRemoteUser:    resolveBool("enable-http-remote-user", *flags.enableHTTPRemoteUser, visited, "LEAFWIKI_ENABLE_HTTP_REMOTE_USER"),
		HTTPRemoteUserHeader:    resolveString("http-remote-user-header-name", *flags.httpRemoteUserHeader, visited, "LEAFWIKI_HTTP_REMOTE_USER_HEADER_NAME", "Remote-User"),
		TrustedProxyIPsRaw:      trustedProxyIPsRaw,
		HTTPRemoteUserLogoutURL: resolveString("http-remote-user-logout-url", *flags.httpRemoteUserLogoutURL, visited, "LEAFWIKI_HTTP_REMOTE_USER_LOGOUT_URL", ""),
		DisableRequestLog:       resolveBool("disable-request-log", *flags.disableRequestLog, visited, "LEAFWIKI_DISABLE_REQUEST_LOG"),
		DaemonIdleTimeout:       resolveDuration("daemon-idle-timeout", *flags.daemonIdleTimeout, visited, "LEAFWIKI_DAEMON_IDLE_TIMEOUT"),
		DisableIdleShutdown:     serviceModeRequested,
		RuntimeStack:            runtimeStack,
	}
}

func resolveWorkspaceForStartup(flags *cliFlags, visited map[string]bool) wiki.Workspace {
	workspace, _, err := resolveStartupWorkspace(flags, visited, flag.Args())
	if err != nil {
		fail("Invalid workspace configuration", "error", err)
	}
	return workspace
}

func validateProxyAuthSettings(trustedProxyIPsRaw string, enableHTTPRemoteUser bool) {
	if _, err := authmw.ParseTrustedProxies(trustedProxyIPsRaw); err != nil {
		fail("invalid --trusted-proxy-ips value", "error", err)
	}
	if err := validateHTTPRemoteUserConfig(enableHTTPRemoteUser, trustedProxyIPsRaw); err != nil {
		fail("Invalid HTTP remote user configuration", "error", err)
	}
}

func resolveLoggingConfigForStartup(flags *cliFlags, visited map[string]bool, dataDir string) leaflogging.Config {
	loggingConfig, err := resolveLoggingConfig(flags, visited, dataDir)
	if err != nil {
		fail("Invalid logging configuration", "error", err)
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
		fail("Invalid MCP configuration", "error", err)
	}
}

func resolveRuntimeStackForStartup(serviceModeRequested bool) string {
	if serviceModeRequested {
		return projectdaemon.RuntimeStackWikidFrontd
	}
	runtimeStack, err := resolveRuntimeStack()
	if err != nil {
		fail("Invalid runtime stack", "error", err)
	}
	return runtimeStack
}

func dispatchRuntimeCommand(args []string, serviceModeRequested bool, agentHookRequested bool, cfg leafwikiRuntimeConfig) {
	if serviceModeRequested {
		if err := runDaemonService(context.Background(), cfg); err != nil {
			failWithoutAgentHook("LeafWiki daemon failed", "error", err)
		}
		return
	}
	if agentHookRequested {
		provider := agenthooks.ProviderUnknown
		if len(args) >= 2 {
			provider = args[1]
		}
		if err := runAgentHookCommand(context.Background(), cfg, provider, os.Stdin, os.Stdout); err != nil {
			slog.Default().Warn("Agent hook failed open", "provider", provider, "error", err)
		}
		return
	}
	if err := runProjectDaemonLauncher(context.Background(), cfg); err != nil {
		fail("LeafWiki startup failed", "error", err)
	}
}

func normalizeAgentHookRawArgs(args []string) []string {
	if len(args) >= 2 && args[0] == "agent-hook" {
		normalized := make([]string, 0, len(args))
		normalized = append(normalized, args[2:]...)
		normalized = append(normalized, args[0], args[1])
		return normalized
	}
	return args
}

func resolveRuntimeStack() (string, error) {
	stack := strings.TrimSpace(os.Getenv("LEAFWIKI_RUNTIME_STACK"))
	if stack == "" {
		return projectdaemon.RuntimeStackWikidFrontd, nil
	}
	switch stack {
	case projectdaemon.RuntimeStackLegacy, projectdaemon.RuntimeStackWikidFrontd:
		return stack, nil
	default:
		return "", fmt.Errorf("unsupported LEAFWIKI_RUNTIME_STACK %q", stack)
	}
}

func agentHookProviderFromArgs(args []string) (string, bool) {
	for i, arg := range args {
		if arg != "agent-hook" {
			continue
		}
		if len(args) > i+1 {
			return args[i+1], true
		}
		return agenthooks.ProviderUnknown, true
	}
	return "", false
}

func agentHookProviderFromRawArgs(args []string) (string, bool) {
	skipNext := false
	for i, arg := range args {
		if skipNext {
			skipNext = false
			continue
		}
		if name, hasInlineValue, ok := rawFlagName(arg); ok {
			if _, takesValue := valueTakingFlagNames()[name]; takesValue && !hasInlineValue {
				skipNext = true
			}
			continue
		}
		if arg != "agent-hook" {
			continue
		}
		if len(args) > i+1 {
			return args[i+1], true
		}
		return agenthooks.ProviderUnknown, true
	}
	return "", false
}

func rawFlagName(arg string) (string, bool, bool) {
	return runtimeconfig.RawFlagName(arg)
}

func rawArgsContainFlag(args []string, flagName string) bool {
	skipNext := false
	for _, arg := range args {
		if skipNext {
			skipNext = false
			continue
		}
		name, hasInlineValue, ok := rawFlagName(arg)
		if ok {
			if _, takesValue := valueTakingFlagNames()[name]; takesValue && !hasInlineValue {
				skipNext = true
			}
		}
		if ok && name == flagName {
			return true
		}
	}
	return false
}

func validateRawConfigFlagUsage(args []string) error {
	return runtimeconfig.ValidateRawConfigFlagUsage(args)
}

func isInvalidBareConfigPathValue(path string) bool {
	return runtimeconfig.IsInvalidBareConfigPathValue(path)
}

func valueTakingFlagNames() map[string]struct{} {
	return runtimeconfig.ValueTakingFlagNames()
}

func isAgentHookCommand(args []string) bool {
	return len(args) > 0 && args[0] == "agent-hook"
}

func isDaemonCommand(args []string) bool {
	return runtimeconfig.IsDaemonCommand(args)
}

func isDaemonHelpCommand(args []string) bool {
	return runtimeconfig.IsDaemonHelpCommand(args)
}

func shouldPrintUsage(args []string) bool {
	if len(args) == 1 && args[0] == "help" {
		return true
	}

	fs := flag.NewFlagSet("leafwiki-help-check", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	registerFlags(fs)
	return fs.Parse(args) == flag.ErrHelp
}

func buildListenAddress(host, port string) string {
	return net.JoinHostPort(host, port)
}

func defaultDaemonServiceDataDir() (string, error) {
	return runtimeconfig.DefaultDaemonServiceDataDir()
}

func defaultDaemonServiceConfigPath() (string, error) {
	return runtimeconfig.DefaultDaemonServiceConfigPath()
}

func newNativeStdioJSONFilter(stdin io.ReadCloser, stdout io.Writer) (io.ReadCloser, <-chan error) {
	reader, writer := io.Pipe()
	done := make(chan error, 1)
	go func() {
		done <- filterNativeStdioJSON(stdin, writer, stdout)
	}()
	return reader, done
}

func filterNativeStdioJSON(stdin io.Reader, forward *io.PipeWriter, stdout io.Writer) error {
	defer forward.Close()

	reader := bufio.NewReader(stdin)
	for {
		line, readErr := reader.ReadBytes('\n')
		if len(line) > 0 {
			frame := bytes.TrimSpace(line)
			if len(frame) > 0 {
				if !json.Valid(frame) {
					if _, err := io.WriteString(stdout, `{"jsonrpc":"2.0","id":null,"error":{"code":-32700,"message":"Parse error"}}`+"\n"); err != nil {
						_ = forward.CloseWithError(err)
						return err
					}
				} else {
					if _, err := forward.Write(line); err != nil {
						return err
					}
					if line[len(line)-1] != '\n' {
						if _, err := forward.Write([]byte("\n")); err != nil {
							return err
						}
					}
				}
			}
		}
		if readErr != nil {
			if errors.Is(readErr, io.EOF) {
				return nil
			}
			_ = forward.CloseWithError(readErr)
			return readErr
		}
	}
}

func isCleanNativeStdioClose(err error) bool {
	if err == nil {
		return true
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, io.EOF) {
		return true
	}
	return strings.Contains(err.Error(), "server is closing: EOF")
}

func nativeStdioHTTPURL(host, port, basePath string) string {
	return "http://" + net.JoinHostPort(host, port) + basePath
}

func runProjectDaemonLauncher(parent context.Context, cfg leafwikiRuntimeConfig) error {
	ctx, cancel := context.WithCancel(parent)
	defer cancel()
	var closeStdin sync.Once
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(signals)
	go func() {
		select {
		case <-signals:
			cancel()
			if cfg.MCPTransports.Stdio {
				closeStdin.Do(func() {
					_ = os.Stdin.Close()
				})
			}
		case <-ctx.Done():
		}
	}()
	desc, err := attachOrStartRuntimeDaemon(ctx, cfg)
	if err != nil {
		if errors.Is(ctx.Err(), context.Canceled) {
			return nil
		}
		return err
	}
	client := projectdaemon.NewClient(desc.ControlURL, desc.ControlToken)
	if cfg.MCPTransports.Stdio && !cfg.DisableAuth {
		if err := client.VerifyStdioAuth(ctx, cfg.APIKey); err != nil {
			if errors.Is(ctx.Err(), context.Canceled) {
				return nil
			}
			if !projectdaemon.IsControlStatus(err, http.StatusUnauthorized) {
				return fmt.Errorf("verify native STDIO API key: %w", err)
			}
			return fmt.Errorf("invalid native STDIO API key")
		}
	}
	handle, err := client.RegisterSession(ctx)
	if err != nil {
		if errors.Is(ctx.Err(), context.Canceled) {
			return nil
		}
		return fmt.Errorf("register project daemon session: %w", err)
	}
	heartbeatCtx, stopHeartbeat := context.WithCancel(ctx)
	defer stopHeartbeat()
	heartbeatErr := make(chan error, 1)
	go func() {
		heartbeatErr <- runDaemonHeartbeat(heartbeatCtx, client, handle.ID, 2*time.Second)
	}()
	defer func() {
		releaseCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = client.ReleaseSession(releaseCtx, handle.ID)
	}()

	if cfg.MCPTransports.Stdio {
		bridgeCtx, stopBridge := context.WithCancel(ctx)
		defer stopBridge()
		bridgeCfg := daemonStdioBridgeConfig(desc, cfg)
		if strings.TrimSpace(desc.PrivateMCPURL) != "" {
			actorContext, err := daemonStdioActorContext(ctx, desc, cfg)
			if err != nil {
				return err
			}
			bridgeCfg.ActorContext = actorContext
		}
		bridgeErr := make(chan error, 1)
		go func() {
			bridgeErr <- runDaemonStdioBridge(bridgeCtx, bridgeCfg)
		}()
		select {
		case err := <-bridgeErr:
			return err
		case err := <-heartbeatErr:
			stopBridge()
			if err == nil || errors.Is(err, context.Canceled) {
				return nil
			}
			return fmt.Errorf("project daemon heartbeat failed: %w", err)
		case <-ctx.Done():
			stopBridge()
			return nil
		}
	}
	if err := waitForForegroundSession(ctx, heartbeatErr); err != nil && !errors.Is(err, context.Canceled) {
		return err
	}
	return nil
}

func runDaemonService(parent context.Context, cfg leafwikiRuntimeConfig) error {
	cfg.RuntimeStack = projectdaemon.RuntimeStackWikidFrontd
	cfg.DisableIdleShutdown = true
	ctx, cancel := context.WithCancel(parent)
	defer cancel()
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(signals)
	go func() {
		select {
		case <-signals:
			cancel()
		case <-ctx.Done():
		}
	}()
	return runProjectDaemonOwner(ctx, cfg)
}

func runAgentHookCommand(parent context.Context, cfg leafwikiRuntimeConfig, provider string, stdin io.Reader, stdout io.Writer) (err error) {
	allowResponse := agenthooks.AllowResponse(provider)
	defer func() {
		if recovered := recover(); recovered != nil {
			err = fmt.Errorf("agent hook panic: %v", recovered)
		}
		if len(allowResponse) > 0 {
			if _, writeErr := stdout.Write(allowResponse); writeErr != nil && err == nil {
				err = fmt.Errorf("write hook allow response: %w", writeErr)
			}
		}
	}()

	raw, err := io.ReadAll(io.LimitReader(stdin, agentHookMaxPayloadBytes+1))
	if err != nil {
		return fmt.Errorf("read hook payload: %w", err)
	}
	if len(raw) > agentHookMaxPayloadBytes {
		return fmt.Errorf("hook payload exceeds %d bytes", agentHookMaxPayloadBytes)
	}
	event, ok := agenthooks.Normalize(provider, raw, time.Now().UTC())
	if !ok {
		return nil
	}

	cfg.DetachDaemonOwnerIO = true
	ctx, cancel := context.WithTimeout(parent, 10*time.Second)
	defer cancel()
	desc, err := attachOrStartRuntimeDaemon(ctx, cfg)
	if err != nil {
		return err
	}
	client := projectdaemon.NewClient(desc.ControlURL, desc.ControlToken)
	if err := client.RecordAgentPresence(ctx, event); err != nil {
		return fmt.Errorf("record agent presence: %w", err)
	}
	return nil
}

func daemonStdioBridgeConfig(desc *projectdaemon.Descriptor, cfg leafwikiRuntimeConfig) daemonStdioBridge {
	endpointURL := strings.TrimRight(desc.ControlURL, "/") + "/mcp"
	controlToken := desc.ControlToken
	if strings.TrimSpace(desc.PrivateMCPURL) != "" && strings.TrimSpace(desc.PrivateMCPToken) != "" {
		endpointURL = strings.TrimSpace(desc.PrivateMCPURL)
		controlToken = strings.TrimSpace(desc.PrivateMCPToken)
	}
	return daemonStdioBridge{
		EndpointURL:      endpointURL,
		ControlToken:     controlToken,
		AuthControlURL:   strings.TrimSpace(desc.ControlURL),
		AuthControlToken: strings.TrimSpace(desc.ControlToken),
		WorkspaceID:      strings.TrimSpace(desc.WorkspaceID),
		APIKey:           cfg.APIKey,
		Stdin:            os.Stdin,
		Stdout:           os.Stdout,
		Stderr:           os.Stderr,
	}
}

func daemonStdioActorContext(ctx context.Context, desc *projectdaemon.Descriptor, cfg leafwikiRuntimeConfig) (string, error) {
	source := &http.Request{
		Method: http.MethodPost,
		URL:    &url.URL{Path: "/mcp"},
		Header: http.Header{},
	}
	if strings.TrimSpace(cfg.APIKey) != "" {
		source.Header.Set("Authorization", "Bearer "+strings.TrimSpace(cfg.APIKey))
	}
	if strings.TrimSpace(desc.WorkspaceID) != "" {
		source.Header.Set(projectdaemon.WorkspaceIDHeader, strings.TrimSpace(desc.WorkspaceID))
	}
	out := struct {
		Actor projectdaemon.ActorContext `json:"actor"`
	}{}
	if err := callWikidPrivateEndpoint(ctx, desc.ControlURL, desc.ControlToken, "/__leafwiki/actor-context", source, &out); err != nil {
		return "", fmt.Errorf("resolve native STDIO actor context: %w", err)
	}
	encoded, err := projectdaemon.EncodeActorContext(out.Actor)
	if err != nil {
		return "", fmt.Errorf("encode native STDIO actor context: %w", err)
	}
	return encoded, nil
}

func validateAuthStartupConfig(cfg leafwikiRuntimeConfig) error {
	if cfg.DisableAuth {
		return nil
	}
	if cfg.JWTSecret == "" {
		return fmt.Errorf("JWT secret is required. Set it using --jwt-secret or LEAFWIKI_JWT_SECRET environment variable.")
	}
	if cfg.AdminPassword == "" {
		return fmt.Errorf("admin password is required. Set it using --admin-password or LEAFWIKI_ADMIN_PASSWORD environment variable.")
	}
	return nil
}

func logStartupValidationFailure(cfg leaflogging.Config, msg string) {
	if cfg.Target != leaflogging.TargetFile {
		return
	}
	logger, closer, err := leaflogging.Open(cfg, leaflogging.Streams{Stdout: os.Stdout, Stderr: os.Stderr})
	if err != nil {
		return
	}
	defer closer.Close()
	logger.Error(msg)
}

func attachOrStartRuntimeDaemon(ctx context.Context, cfg leafwikiRuntimeConfig) (*projectdaemon.Descriptor, error) {
	requestCfg, err := daemonWorkspaceRequestConfigForRuntime(cfg)
	if err != nil {
		return nil, err
	}
	descriptorPath := projectdaemon.DescriptorPath(requestCfg.DataDir)
	if cfg.RuntimeStack == projectdaemon.RuntimeStackWikidFrontd {
		return attachOrStartFederatedProjectDaemon(ctx, cfg, requestCfg, descriptorPath)
	}
	return attachOrStartProjectDaemon(ctx, cfg, requestCfg, descriptorPath)
}

func attachOrStartProjectDaemon(ctx context.Context, cfg leafwikiRuntimeConfig, ownerCfg projectdaemon.Config, descriptorPath string) (*projectdaemon.Descriptor, error) {
	desc, healthy, err := readHealthyProjectDaemon(ctx, descriptorPath, ownerCfg)
	if err != nil {
		return nil, err
	}
	if healthy {
		if mismatches := compareProjectDaemonDescriptorForRequest(desc, ownerCfg, cfg.MCPTransports); len(mismatches) > 0 {
			return nil, errors.New(projectdaemon.FormatConfigMismatch(mismatches))
		}
		return desc, nil
	}
	if desc != nil {
		_ = projectdaemon.RemoveDescriptor(descriptorPath)
	}
	if err := validateAuthStartupConfig(cfg); err != nil {
		logStartupValidationFailure(cfg.Logging, err.Error())
		return nil, err
	}
	if cfg.MCPTransports.Stdio && !cfg.DisableAuth {
		if err := verifyStdioAPIKeyFromStorage(authStorageDirForRuntime(ownerCfg.DataDir, ownerCfg.RuntimeStack), cfg.APIKey); err != nil {
			if errors.Is(err, coreauth.ErrInvalidToken) {
				return nil, fmt.Errorf("invalid native STDIO API key")
			}
			return nil, fmt.Errorf("verify native STDIO API key: %w", err)
		}
	}
	spawnCfg, err := daemonOwnerRuntimeConfig(cfg)
	if err != nil {
		return nil, err
	}
	spawnOwnerCfg, err := daemonConfigForRuntime(spawnCfg)
	if err != nil {
		return nil, err
	}
	errorPath, err := spawnProjectDaemonOwner(cfg)
	if err != nil {
		return nil, err
	}
	return waitForProjectDaemon(ctx, descriptorPath, errorPath, spawnOwnerCfg, cfg.MCPTransports)
}

func attachOrStartFederatedProjectDaemon(ctx context.Context, cfg leafwikiRuntimeConfig, requestCfg projectdaemon.Config, workspaceDescriptorPath string) (*projectdaemon.Descriptor, error) {
	globalCfg, err := daemonRequestConfigForRuntime(cfg)
	if err != nil {
		return nil, err
	}
	layout := wikid.GlobalLayout(globalCfg.DataDir)
	if cfg.MCPTransports.Stdio {
		if workspace, ok, err := registeredFederatedWorkspaceForRequest(layout, requestCfg); err != nil {
			return nil, err
		} else if ok {
			directRequestCfg := requestCfg
			directRequestCfg.WorkspaceID = workspace.ID
			desc, healthy, err := readHealthyProjectDaemon(ctx, workspaceDescriptorPath, directRequestCfg)
			if err != nil {
				return nil, err
			}
			if healthy {
				if mismatches := compareProjectDaemonDescriptorForRequest(desc, directRequestCfg, cfg.MCPTransports); len(mismatches) > 0 {
					return nil, errors.New(projectdaemon.FormatConfigMismatch(mismatches))
				}
				return desc, nil
			}
			if desc != nil {
				_ = projectdaemon.RemoveDescriptor(workspaceDescriptorPath)
			}
		}
	}

	globalDescriptorPath := projectdaemon.GlobalDescriptorPath(layout.RuntimeDir, projectdaemon.RoleWikid)
	globalDesc, healthy, err := readHealthyProjectDaemon(ctx, globalDescriptorPath, globalCfg)
	if err != nil {
		return nil, err
	}
	if !healthy {
		if globalDesc != nil {
			_ = projectdaemon.RemoveDescriptor(globalDescriptorPath)
		}
		if err := validateAuthStartupConfig(cfg); err != nil {
			logStartupValidationFailure(cfg.Logging, err.Error())
			return nil, err
		}
		if cfg.MCPTransports.Stdio && !cfg.DisableAuth {
			if err := verifyStdioAPIKeyFromStorage(authStorageDirForRuntime(globalCfg.DataDir, globalCfg.RuntimeStack), cfg.APIKey); err != nil {
				if errors.Is(err, coreauth.ErrInvalidToken) {
					return nil, fmt.Errorf("invalid native STDIO API key")
				}
				return nil, fmt.Errorf("verify native STDIO API key: %w", err)
			}
		}
		errorPath, err := spawnProjectDaemonOwner(cfg)
		if err != nil {
			return nil, err
		}
		globalDesc, err = waitForProjectDaemon(ctx, globalDescriptorPath, errorPath, globalCfg, cfg.MCPTransports)
		if err != nil {
			return nil, err
		}
	} else if mismatches := compareProjectDaemonDescriptorForRequest(globalDesc, globalCfg, cfg.MCPTransports); len(mismatches) > 0 {
		return nil, errors.New(projectdaemon.FormatConfigMismatch(mismatches))
	}

	workspace, isHome, err := registerFederatedFirstContact(layout, requestCfg, cfg)
	if err != nil {
		return nil, err
	}
	if isHome {
		return globalDesc, nil
	}
	requestCfg.WorkspaceID = workspace.ID
	requestCfg.MarkdownLinkRootPrefix = workspace.MarkdownLinkRootPrefix
	shouldEnsure := cfg.DisableAuth || cfg.MCPTransports.Stdio
	if shouldEnsure {
		if err := ensureFederatedWorkspace(ctx, globalDesc, workspace.ID, cfg); err != nil {
			return nil, err
		}
	}
	if cfg.MCPTransports.Stdio {
		return waitForProjectDaemon(ctx, workspaceDescriptorPath, "", requestCfg, cfg.MCPTransports)
	}
	return globalDesc, nil
}

func registeredFederatedWorkspaceForRequest(layout wikid.Layout, requestCfg projectdaemon.Config) (wikid.WorkspaceRecord, bool, error) {
	if filepath.Clean(requestCfg.DataDir) == filepath.Clean(layout.HomeDir) &&
		filepath.Clean(requestCfg.RootDir) == filepath.Clean(layout.HomeRootDir) {
		return wikid.WorkspaceRecord{ID: wikid.HomeWorkspaceID, DataDir: layout.HomeDir, RootDir: layout.HomeRootDir}, true, nil
	}
	registry := wikid.NewRegistryService(wikid.NewRegistryStore(layout.DBPath), layout)
	workspaces, err := registry.ListWorkspaces()
	if err != nil {
		return wikid.WorkspaceRecord{}, false, err
	}
	for _, workspace := range workspaces {
		if filepath.Clean(workspace.DataDir) == filepath.Clean(requestCfg.DataDir) &&
			filepath.Clean(workspace.RootDir) == filepath.Clean(requestCfg.RootDir) {
			return workspace, true, nil
		}
	}
	return wikid.WorkspaceRecord{}, false, nil
}

func registerFederatedFirstContact(layout wikid.Layout, requestCfg projectdaemon.Config, cfg leafwikiRuntimeConfig) (wikid.WorkspaceRecord, bool, error) {
	if requestCfg.DataDir == layout.HomeDir && requestCfg.RootDir == layout.HomeRootDir {
		return wikid.WorkspaceRecord{ID: wikid.HomeWorkspaceID, DataDir: layout.HomeDir, RootDir: layout.HomeRootDir}, true, nil
	}
	registry := wikid.NewRegistryService(wikid.NewRegistryStore(layout.DBPath), layout)
	registration, err := registry.RegisterWorkspaceWithResultAndGrants(wikid.RegisterWorkspaceRequest{
		DisplayName:            federatedWorkspaceDisplayName(requestCfg),
		DataDir:                requestCfg.DataDir,
		RootDir:                requestCfg.RootDir,
		MarkdownLinkRootPrefix: cfg.MarkdownLinkRootPrefix,
	}, func(registration wikid.RegisterWorkspaceResult) ([]wikid.Grant, error) {
		workspace := registration.Workspace
		var grants []wikid.Grant
		if cfg.DisableAuth {
			grants = append(grants, wikid.Grant{Subject: "user:public-editor", WorkspaceID: workspace.ID, Role: wikid.GrantRoleEditor})
		}
		if cfg.PublicAccess {
			grants = append(grants, wikid.Grant{Subject: "user:public-viewer", WorkspaceID: workspace.ID, Role: wikid.GrantRoleViewer})
		}
		if cfg.MCPTransports.Stdio && !cfg.DisableAuth && registration.Created {
			grant, ok, err := federatedStdioAPIKeyWorkspaceGrant(layout, cfg, workspace.ID)
			if err != nil {
				return nil, err
			}
			if ok {
				grants = append(grants, grant)
			}
		}
		return grants, nil
	})
	if err != nil {
		return wikid.WorkspaceRecord{}, false, err
	}
	return registration.Workspace, false, nil
}

func federatedStdioAPIKeyWorkspaceGrant(layout wikid.Layout, cfg leafwikiRuntimeConfig, workspaceID string) (wikid.Grant, bool, error) {
	apiKey := strings.TrimSpace(cfg.APIKey)
	if apiKey == "" {
		return wikid.Grant{}, false, nil
	}
	user, err := stdioAPIKeyUserFromStorage(authStorageDirForRuntime(layout.HomeDir, cfg.RuntimeStack), apiKey)
	if err != nil {
		return wikid.Grant{}, false, fmt.Errorf("resolve native STDIO API-key grant user: %w", err)
	}
	userRole := wikidGrantRoleForCoreRole(user.Role)
	role := userRole
	if role == "" {
		return wikid.Grant{}, false, fmt.Errorf("native STDIO API-key user role %q cannot access workspaces", user.Role)
	}
	return wikid.Grant{Subject: "user:" + user.ID, WorkspaceID: workspaceID, Role: role}, true, nil
}

func federatedWorkspaceDisplayName(cfg projectdaemon.Config) string {
	if base := strings.TrimSpace(filepath.Base(cfg.RootDir)); base != "" && base != "." && base != string(filepath.Separator) {
		return base
	}
	if base := strings.TrimSpace(filepath.Base(cfg.DataDir)); base != "" && base != "." && base != string(filepath.Separator) {
		return base
	}
	return "Workspace"
}

func ensureFederatedWorkspace(ctx context.Context, desc *projectdaemon.Descriptor, workspaceID string, cfg leafwikiRuntimeConfig) error {
	if desc == nil {
		return fmt.Errorf("global wikid descriptor is unavailable")
	}
	endpoint := strings.TrimRight(desc.ControlURL, "/") + "/__leafwiki/workspaces/" + url.PathEscape(workspaceID) + "/ensure"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, nil)
	if err != nil {
		return err
	}
	req.Header.Set(projectdaemon.ControlTokenHeader, desc.ControlToken)
	req.Header.Set("X-LeafWiki-Original-Method", http.MethodPost)
	req.Header.Set("X-LeafWiki-Original-Path", "/mcp")
	if strings.TrimSpace(cfg.APIKey) != "" {
		req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(cfg.APIKey))
	}
	resp, err := (&http.Client{Timeout: 10 * time.Second}).Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		msg := strings.TrimSpace(string(raw))
		if msg == "" {
			msg = resp.Status
		}
		return fmt.Errorf("ensure workspace %q: %s", workspaceID, msg)
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	return nil
}

func readHealthyProjectDaemon(ctx context.Context, descriptorPath string, ownerCfg projectdaemon.Config) (*projectdaemon.Descriptor, bool, error) {
	desc, err := projectdaemon.ReadTrustedDescriptor(descriptorPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, false, nil
		}
		locksFree, lockErr := projectDaemonLocksFree(ownerCfg.DataDir, ownerCfg.RootDir)
		if lockErr != nil {
			return nil, false, lockErr
		}
		if !locksFree {
			return nil, false, fmt.Errorf("read project daemon descriptor: %w", err)
		}
		_ = projectdaemon.RemoveDescriptor(descriptorPath)
		return nil, false, nil
	}
	if desc.SchemaVersion != projectdaemon.DescriptorSchemaVersion {
		locksFree, lockErr := projectDaemonLocksFree(ownerCfg.DataDir, ownerCfg.RootDir)
		if lockErr != nil {
			return nil, false, lockErr
		}
		if !locksFree {
			return nil, false, fmt.Errorf("project daemon descriptor schema version = %d, want %d while project locks are held", desc.SchemaVersion, projectdaemon.DescriptorSchemaVersion)
		}
		return desc, false, nil
	}
	if desc.DataDir != ownerCfg.DataDir || desc.RootDir != ownerCfg.RootDir {
		healthy, err := projectDaemonDescriptorHealthy(ctx, desc)
		if err != nil {
			return nil, false, err
		}
		if healthy {
			return nil, false, projectDaemonIdentityMismatch(desc, ownerCfg)
		}
		return desc, false, nil
	}
	healthy, err := projectDaemonDescriptorHealthy(ctx, desc)
	if err != nil {
		return nil, false, err
	}
	return desc, healthy, nil
}

func projectDaemonDescriptorHealthy(ctx context.Context, desc *projectdaemon.Descriptor) (bool, error) {
	if desc == nil {
		return false, nil
	}
	if desc.Role == projectdaemon.RoleWorkspaced && strings.TrimSpace(desc.PrivateMCPURL) != "" && strings.TrimSpace(desc.PrivateMCPToken) != "" {
		if !isTrustedDaemonControlURL(desc.PrivateMCPURL) {
			return false, fmt.Errorf("workspaced descriptor private MCP URL is not trusted")
		}
		if !processPIDAlive(desc.PID) {
			return false, nil
		}
		return workspacedPrivateMCPEndpointReachable(ctx, desc), nil
	}
	locksHeld, err := projectDaemonLocksHeld(desc.DataDir, desc.RootDir)
	if err != nil {
		return false, err
	}
	if !locksHeld {
		return false, nil
	}
	if !isTrustedDaemonControlURL(desc.ControlURL) {
		return false, fmt.Errorf("project daemon locks are held but descriptor control URL is not trusted")
	}
	pingCtx, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	health, err := projectdaemon.NewClient(desc.ControlURL, desc.ControlToken).Health(pingCtx)
	if err != nil {
		return false, fmt.Errorf("project daemon locks are held but control health is unreachable: %w", err)
	}
	if !daemonHealthMatchesDescriptor(desc, health) {
		return false, fmt.Errorf("project daemon locks are held but control health does not match descriptor")
	}
	return true, nil
}

func workspacedPrivateMCPEndpointReachable(ctx context.Context, desc *projectdaemon.Descriptor) bool {
	probeCtx, cancel := context.WithTimeout(ctx, 500*time.Millisecond)
	defer cancel()
	req, err := http.NewRequestWithContext(probeCtx, http.MethodGet, strings.TrimSpace(desc.PrivateMCPURL), nil)
	if err != nil {
		return false
	}
	req.Header.Set(projectdaemon.ControlTokenHeader, strings.TrimSpace(desc.PrivateMCPToken))
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1024))
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return false
	}
	return resp.StatusCode < http.StatusInternalServerError
}

func processPIDAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	process, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	err = process.Signal(syscall.Signal(0))
	return err == nil || errors.Is(err, syscall.EPERM)
}

func projectDaemonIdentityMismatch(desc *projectdaemon.Descriptor, ownerCfg projectdaemon.Config) error {
	var mismatches []projectdaemon.Mismatch
	if desc.DataDir != ownerCfg.DataDir {
		mismatches = append(mismatches, projectdaemon.Mismatch{
			Field: "data-dir",
			Want:  desc.DataDir,
			Got:   ownerCfg.DataDir,
		})
	}
	if desc.RootDir != ownerCfg.RootDir {
		mismatches = append(mismatches, projectdaemon.Mismatch{
			Field: "root-dir",
			Want:  desc.RootDir,
			Got:   ownerCfg.RootDir,
		})
	}
	return errors.New(projectdaemon.FormatConfigMismatch(mismatches))
}

const projectDaemonWaitTimeout = 30 * time.Second

func waitForProjectDaemon(ctx context.Context, descriptorPath string, errorPath string, ownerCfg projectdaemon.Config, requestTransports mcpTransports) (*projectdaemon.Descriptor, error) {
	deadlineCtx, cancel := context.WithTimeout(ctx, projectDaemonWaitTimeout)
	defer cancel()
	defer os.Remove(errorPath)
	ticker := time.NewTicker(25 * time.Millisecond)
	defer ticker.Stop()
	var lastErr error
	var startupErr projectDaemonStartupError
	for {
		desc, healthy, err := readHealthyProjectDaemon(deadlineCtx, descriptorPath, ownerCfg)
		if err != nil {
			lastErr = err
		} else if healthy {
			if mismatches := compareProjectDaemonDescriptorForRequest(desc, ownerCfg, requestTransports); len(mismatches) > 0 {
				return nil, errors.New(projectdaemon.FormatConfigMismatch(mismatches))
			}
			return desc, nil
		}
		if raw, err := os.ReadFile(errorPath); err == nil && len(raw) > 0 {
			if startupErr.Message == "" {
				startupErr = parseProjectDaemonStartupError(raw)
			}
			if startupErr.Message != "" && !startupErr.IsLock() {
				return nil, formatProjectDaemonStartupError(startupErr)
			}
			if startupErr.Message != "" && startupErr.IsLock() {
				disjointRootOwner, lockErr := projectDaemonDataLockFreeRootLockHeld(ownerCfg.DataDir, ownerCfg.RootDir)
				if lockErr != nil {
					lastErr = lockErr
				} else if disjointRootOwner {
					return nil, formatProjectDaemonStartupError(startupErr)
				}
			}
		}
		select {
		case <-deadlineCtx.Done():
			if errors.Is(ctx.Err(), context.Canceled) {
				return nil, ctx.Err()
			}
			if startupErr.Message != "" {
				return nil, formatProjectDaemonStartupError(startupErr)
			}
			if lastErr != nil {
				return nil, fmt.Errorf("project is locked but no attachable daemon was found: %w", lastErr)
			}
			return nil, fmt.Errorf("project is locked but no attachable daemon was found")
		case <-ticker.C:
		}
	}
}

const (
	projectDaemonStartupErrorKindStartup = "startup"
	projectDaemonStartupErrorKindLock    = "lock"
)

type projectDaemonStartupError struct {
	Kind    string `json:"kind"`
	Message string `json:"message"`
}

func (e projectDaemonStartupError) IsLock() bool {
	return e.Kind == projectDaemonStartupErrorKindLock
}

func parseProjectDaemonStartupError(raw []byte) projectDaemonStartupError {
	trimmed := strings.TrimSpace(string(raw))
	var structured projectDaemonStartupError
	if err := json.Unmarshal(raw, &structured); err == nil && strings.TrimSpace(structured.Message) != "" {
		structured.Message = strings.TrimSpace(structured.Message)
		if structured.Kind == "" {
			structured.Kind = projectDaemonStartupErrorKindStartup
		}
		return structured
	}
	kind := projectDaemonStartupErrorKindStartup
	if isLegacyProjectDaemonLockStartupMessage(trimmed) {
		kind = projectDaemonStartupErrorKindLock
	}
	return projectDaemonStartupError{Kind: kind, Message: trimmed}
}

func formatProjectDaemonStartupError(startupErr projectDaemonStartupError) error {
	if startupErr.IsLock() {
		return fmt.Errorf("project is locked but no attachable daemon was found: %s", startupErr.Message)
	}
	return fmt.Errorf("project daemon failed to start: %s", startupErr.Message)
}

func writeProjectDaemonStartupError(path string, err error) {
	if strings.TrimSpace(path) == "" || err == nil {
		return
	}
	kind := projectDaemonStartupErrorKindStartup
	if locking.IsLockHeld(err) {
		kind = projectDaemonStartupErrorKindLock
	}
	raw, marshalErr := json.Marshal(projectDaemonStartupError{Kind: kind, Message: err.Error()})
	if marshalErr != nil {
		raw = []byte(err.Error())
	}
	_ = os.WriteFile(path, raw, 0o600)
}

func isLegacyProjectDaemonLockStartupMessage(msg string) bool {
	normalized := strings.ToLower(strings.TrimSpace(msg))
	return strings.Contains(normalized, "acquire data directory lock:") ||
		strings.Contains(normalized, "acquire root directory lock:") ||
		strings.Contains(normalized, "data directory is already in use") ||
		strings.Contains(normalized, "root directory is already in use")
}

func isTrustedDaemonControlURL(raw string) bool {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.Scheme != "http" {
		return false
	}
	host := parsed.Hostname()
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func projectDaemonLocksHeld(dataDir string, rootDir string) (bool, error) {
	dataLock, err := locking.AcquireDataDirLock(dataDir)
	if err == nil {
		_ = dataLock.Release()
		return false, nil
	}
	if !locking.IsDataDirLockHeld(err) {
		return false, err
	}
	rootLock, err := locking.AcquireRootDirLock(rootDir)
	if err == nil {
		_ = rootLock.Release()
		return false, nil
	}
	if !locking.IsRootDirLockHeld(err) {
		return false, err
	}
	return true, nil
}

func projectDaemonLocksFree(dataDir string, rootDir string) (bool, error) {
	dataLock, err := locking.AcquireDataDirLock(dataDir)
	if err != nil {
		if locking.IsDataDirLockHeld(err) {
			return false, nil
		}
		return false, err
	}
	defer dataLock.Release()

	rootLock, err := locking.AcquireRootDirLock(rootDir)
	if err != nil {
		if locking.IsRootDirLockHeld(err) {
			return false, nil
		}
		return false, err
	}
	if err := rootLock.Release(); err != nil {
		return false, err
	}
	return true, nil
}

func projectDaemonDataLockFreeRootLockHeld(dataDir string, rootDir string) (bool, error) {
	dataLock, err := locking.AcquireDataDirLock(dataDir)
	if err != nil {
		if locking.IsDataDirLockHeld(err) {
			return false, nil
		}
		return false, err
	}
	if err := dataLock.Release(); err != nil {
		return false, err
	}

	rootLock, err := locking.AcquireRootDirLock(rootDir)
	if err == nil {
		_ = rootLock.Release()
		return false, nil
	}
	if locking.IsRootDirLockHeld(err) {
		return true, nil
	}
	return false, err
}

func daemonHealthMatchesDescriptor(desc *projectdaemon.Descriptor, health *projectdaemon.DaemonHealth) bool {
	if desc == nil || health == nil {
		return false
	}
	return health.SchemaVersion == desc.SchemaVersion &&
		health.PID == desc.PID &&
		health.DataDir == desc.DataDir &&
		health.RootDir == desc.RootDir &&
		health.ConfigHash == desc.ConfigHash
}

func projectDaemonDescriptorRole(runtimeStack string) projectdaemon.RoleName {
	if runtimeStack == projectdaemon.RuntimeStackWikidFrontd {
		return projectdaemon.RoleWikid
	}
	return ""
}

func projectDaemonDescriptorRoles(runtimeStack string, pid int, publicAddr string, roleShells *wikidFrontdRuntime) []projectdaemon.RoleHealth {
	if runtimeStack != projectdaemon.RuntimeStackWikidFrontd {
		return nil
	}
	if roleShells != nil && len(roleShells.roles) > 0 {
		return append([]projectdaemon.RoleHealth(nil), roleShells.roles...)
	}
	now := time.Now().UTC()
	return []projectdaemon.RoleHealth{
		{Name: projectdaemon.RoleWikid, State: projectdaemon.RoleStateReady, PID: pid, UpdatedAt: now},
		{Name: projectdaemon.RoleFrontd, State: projectdaemon.RoleStateReady, PID: pid, URL: "http://" + publicAddr, UpdatedAt: now},
		{Name: projectdaemon.RoleWorkspaced, State: projectdaemon.RoleStateReady, PID: pid, Private: true, UpdatedAt: now},
	}
}

func compareProjectDaemonConfigForRequest(owner projectdaemon.Config, requested projectdaemon.Config, requestTransports mcpTransports) []projectdaemon.Mismatch {
	normalized := requested
	if requestTransports.Stdio && !requestTransports.HTTP {
		normalized.PublicMCPEnabled = owner.PublicMCPEnabled
		normalized.Host = owner.Host
		normalized.Port = owner.Port
		normalized.LogTarget = owner.LogTarget
		normalized.LogFile = owner.LogFile
		normalized.DisableRequestLog = owner.DisableRequestLog
	}
	return projectdaemon.CompareConfig(owner, normalized)
}

func compareProjectDaemonDescriptorForRequest(desc *projectdaemon.Descriptor, requested projectdaemon.Config, requestTransports mcpTransports) []projectdaemon.Mismatch {
	if desc == nil {
		return nil
	}
	normalized := requested
	if desc.Role == projectdaemon.RoleWorkspaced && requestTransports.Stdio && strings.TrimSpace(desc.PrivateMCPURL) != "" {
		normalized.Host = desc.Config.Host
		normalized.Port = desc.Config.Port
		normalized.PublicMCPEnabled = desc.Config.PublicMCPEnabled
		normalized.LogTarget = desc.Config.LogTarget
		normalized.LogFile = desc.Config.LogFile
		normalized.DisableRequestLog = desc.Config.DisableRequestLog
	}
	mismatches := compareProjectDaemonConfigForRequest(desc.Config, normalized, requestTransports)
	requestedWorkspaceID := strings.TrimSpace(normalized.WorkspaceID)
	descriptorWorkspaceID := strings.TrimSpace(desc.WorkspaceID)
	if requestedWorkspaceID != "" && descriptorWorkspaceID != "" && requestedWorkspaceID != descriptorWorkspaceID && !hasProjectDaemonMismatch(mismatches, "workspace-id") {
		mismatches = append(mismatches, projectdaemon.Mismatch{
			Field: "workspace-id",
			Want:  descriptorWorkspaceID,
			Got:   requestedWorkspaceID,
		})
	}
	return mismatches
}

func hasProjectDaemonMismatch(mismatches []projectdaemon.Mismatch, field string) bool {
	for _, mismatch := range mismatches {
		if mismatch.Field == field {
			return true
		}
	}
	return false
}

func authStorageDirForRuntime(dataDir string, runtimeStack string) string {
	if runtimeStack == projectdaemon.RuntimeStackWikidFrontd {
		return wikid.AuthStoragePaths(dataDir).AuthDir
	}
	return dataDir
}

func verifyStdioAPIKeyFromStorage(dataDir string, apiKey string) error {
	_, err := stdioAPIKeyUserFromStorage(dataDir, apiKey)
	return err
}

func stdioAPIKeyUserFromStorage(dataDir string, apiKey string) (*coreauth.User, error) {
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		return nil, err
	}
	userStore, err := coreauth.NewUserStore(dataDir)
	if err != nil {
		return nil, err
	}
	defer userStore.Close()
	userService := coreauth.NewUserService(userStore)
	apiKeyStore, err := coreauth.NewAPIKeyStore(dataDir)
	if err != nil {
		return nil, err
	}
	apiKeyService := coreauth.NewAPIKeyService(apiKeyStore, userService)
	defer apiKeyService.Close()
	verified, err := apiKeyService.VerifyAPIKey(apiKey)
	if err != nil {
		return nil, err
	}
	return verified.User, nil
}

func wikidControlMCPActorResolver(authDir string, cfg leafwikiRuntimeConfig) func(*http.Request) (projectdaemon.ActorContext, error) {
	return func(req *http.Request) (projectdaemon.ActorContext, error) {
		if cfg.DisableAuth {
			return actorContextForUser(&coreauth.User{ID: "public-editor", Username: "public-editor", Role: coreauth.RoleEditor}, "disabled", cfg)
		}
		token := httpBearerToken(req)
		if token == "" || !coreauth.IsAPIKeyBearer(token) {
			return projectdaemon.ActorContext{}, fmt.Errorf("native STDIO requires an API key")
		}
		user, err := stdioAPIKeyUserFromStorage(authDir, token)
		if err != nil {
			return projectdaemon.ActorContext{}, err
		}
		return actorContextForUser(user, "api_key", cfg)
	}
}

func daemonConfigForRuntime(cfg leafwikiRuntimeConfig) (projectdaemon.Config, error) {
	dataDir, rootDir, err := projectdaemon.CanonicalizeProject(cfg.Workspace.DataDir, cfg.Workspace.RootDir)
	if err != nil {
		return projectdaemon.Config{}, err
	}
	logFile := daemonLogFileForConfig(cfg, dataDir)
	return projectdaemon.Config{
		RuntimeStack:            cfg.RuntimeStack,
		WorkspaceID:             runtimeWorkspaceID(cfg.Workspace),
		DataDir:                 dataDir,
		RootDir:                 rootDir,
		AuthDisabled:            cfg.DisableAuth,
		PublicMCPEnabled:        cfg.MCPTransports.HTTP,
		Host:                    cfg.Host,
		Port:                    cfg.Port,
		BasePath:                cfg.BasePath,
		MarkdownLinkRootPrefix:  cfg.MarkdownLinkRootPrefix,
		PublicAccess:            cfg.PublicAccess,
		AllowInsecure:           cfg.AllowInsecure,
		AccessTokenTimeout:      cfg.AccessTokenTimeout.String(),
		RefreshTokenTimeout:     cfg.RefreshTokenTimeout.String(),
		InjectCodeInHeaderHash:  projectdaemon.HashSecret(cfg.InjectCodeInHeader),
		CustomStylesheet:        cfg.CustomStylesheet,
		LogTarget:               string(cfg.Logging.Target),
		LogFile:                 logFile,
		HideLinkMetadataSection: cfg.HideLinkMetadataSection,
		MaxAssetUploadSizeBytes: cfg.MaxAssetUploadSize,
		EnableRevision:          cfg.EnableRevision,
		EnableWorkspaceSync:     cfg.EnableWorkspaceSync,
		EnableLinkRefactor:      cfg.EnableLinkRefactor,
		MaxRevisionHistory:      cfg.MaxRevisionHistory,
		EnableHTTPRemoteUser:    cfg.EnableHTTPRemoteUser,
		HTTPRemoteUserHeader:    cfg.HTTPRemoteUserHeader,
		TrustedProxyIPs:         cfg.TrustedProxyIPsRaw,
		HTTPRemoteUserLogoutURL: cfg.HTTPRemoteUserLogoutURL,
		DisableRequestLog:       cfg.DisableRequestLog,
		DaemonIdleTimeout:       cfg.DaemonIdleTimeout.String(),
	}, nil
}

func daemonRequestConfigForRuntime(cfg leafwikiRuntimeConfig) (projectdaemon.Config, error) {
	ownerCfg, err := daemonOwnerRuntimeConfig(cfg)
	if err != nil {
		return projectdaemon.Config{}, err
	}
	return daemonConfigForRuntime(ownerCfg)
}

func daemonWorkspaceRequestConfigForRuntime(cfg leafwikiRuntimeConfig) (projectdaemon.Config, error) {
	workspaceCfg, err := daemonWorkspaceRuntimeConfig(cfg)
	if err != nil {
		return projectdaemon.Config{}, err
	}
	return daemonConfigForRuntime(workspaceCfg)
}

func daemonWorkspaceRuntimeConfig(cfg leafwikiRuntimeConfig) (leafwikiRuntimeConfig, error) {
	ownerCfg := cfg
	ownerCfg.APIKey = ""
	if ownerCfg.RuntimeStack == projectdaemon.RuntimeStackWikidFrontd {
		ownerCfg.EnableWorkspaceSync = true
		ownerCfg.EnableRevision = false
	}
	if ownerCfg.MCPTransports.Stdio && ownerCfg.Logging.Target == leaflogging.TargetStderr {
		fileLogging, err := leaflogging.Resolve(leaflogging.ConfigInput{
			Target:    string(leaflogging.TargetFile),
			TargetSet: true,
			DataDir:   ownerCfg.Workspace.DataDir,
		})
		if err != nil {
			return leafwikiRuntimeConfig{}, err
		}
		fileLogging.Level = ownerCfg.Logging.Level
		ownerCfg.Logging = fileLogging
	}
	return ownerCfg, nil
}

func daemonLogFileForConfig(cfg leafwikiRuntimeConfig, canonicalDataDir string) string {
	if cfg.Logging.Target != leaflogging.TargetFile || strings.TrimSpace(cfg.Logging.FilePath) == "" {
		return cfg.Logging.FilePath
	}
	logPath := filepath.Clean(cfg.Logging.FilePath)
	absLogPath, err := filepath.Abs(logPath)
	if err != nil {
		return logPath
	}
	originalDataDir, err := filepath.Abs(filepath.Clean(cfg.Workspace.DataDir))
	if err == nil {
		if rel, ok := localRelativePath(originalDataDir, absLogPath); ok {
			return filepath.Clean(filepath.Join(canonicalDataDir, rel))
		}
	}
	if rel, ok := localRelativePath(canonicalDataDir, absLogPath); ok {
		return filepath.Clean(filepath.Join(canonicalDataDir, rel))
	}
	return filepath.Clean(absLogPath)
}

func localRelativePath(base string, target string) (string, bool) {
	rel, err := filepath.Rel(filepath.Clean(base), filepath.Clean(target))
	if err != nil {
		return "", false
	}
	if rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", false
	}
	return rel, true
}

func spawnProjectDaemonOwner(cfg leafwikiRuntimeConfig) (string, error) {
	ownerCfg, err := daemonOwnerRuntimeConfig(cfg)
	if err != nil {
		return "", err
	}
	errorFile, err := os.CreateTemp("", "leafwiki-project-daemon-*.err")
	if err != nil {
		return "", fmt.Errorf("create daemon startup error file: %w", err)
	}
	errorPath := errorFile.Name()
	_ = errorFile.Close()
	_ = os.Remove(errorPath)
	ownerCfg.DaemonStartupErrorPath = errorPath
	raw, err := json.Marshal(ownerCfg)
	if err != nil {
		return "", err
	}
	tmp, err := os.CreateTemp("", "leafwiki-project-daemon-*.json")
	if err != nil {
		return "", fmt.Errorf("create daemon startup config: %w", err)
	}
	startupPath := tmp.Name()
	removeStartupConfig := true
	defer func() {
		if removeStartupConfig {
			_ = os.Remove(startupPath)
		}
	}()
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return "", err
	}
	if _, err := tmp.Write(raw); err != nil {
		_ = tmp.Close()
		return "", err
	}
	if err := tmp.Close(); err != nil {
		return "", err
	}

	exe, err := projectDaemonExecutable()
	if err != nil {
		return "", err
	}
	args := []string{"--internal-project-daemon", startupPath}
	if os.Getenv("GO_WANT_LEAFWIKI_HELPER_PROCESS") == "1" {
		args = []string{"-test.run=TestLeafWikiHelperProcess", "--", "--internal-project-daemon", startupPath}
	}
	cmd := exec.Command(exe, args...)
	cmd.Env = daemonOwnerEnv()
	cleanupIO, err := configureDaemonOwnerIO(cmd, cfg)
	if err != nil {
		return "", err
	}
	defer cleanupIO()
	if err := cmd.Start(); err != nil {
		return "", fmt.Errorf("start project daemon: %w", err)
	}
	removeStartupConfig = false
	scheduleProjectDaemonStartupConfigCleanup(startupPath)
	if cmd.Process != nil {
		if err := cmd.Process.Release(); err != nil {
			return "", fmt.Errorf("release project daemon process: %w", err)
		}
	}
	return errorPath, nil
}

func scheduleProjectDaemonStartupConfigCleanup(path string) {
	if strings.TrimSpace(path) == "" {
		return
	}
	delay := projectDaemonStartupConfigPostStartCleanupDelay
	if delay <= 0 {
		delay = 30 * time.Second
	}
	go func() {
		timer := time.NewTimer(delay)
		defer timer.Stop()
		<-timer.C
		_ = os.Remove(path)
	}()
}

func daemonOwnerRuntimeConfig(cfg leafwikiRuntimeConfig) (leafwikiRuntimeConfig, error) {
	ownerCfg, err := daemonWorkspaceRuntimeConfig(cfg)
	if err != nil {
		return leafwikiRuntimeConfig{}, err
	}
	if ownerCfg.RuntimeStack == projectdaemon.RuntimeStackWikidFrontd {
		workspace, err := globalRuntimeWorkspace()
		if err != nil {
			return leafwikiRuntimeConfig{}, err
		}
		ownerCfg.Workspace = workspace
		ownerCfg.MarkdownLinkRootPrefix = ""
	}
	return ownerCfg, nil
}

func globalRuntimeWorkspace() (wiki.Workspace, error) {
	homeDir, err := globalRuntimeHomeDir()
	if err != nil {
		return wiki.Workspace{}, err
	}
	return wiki.NormalizeWorkspace(wiki.Workspace{
		ID:      wikid.HomeWorkspaceID,
		DataDir: homeDir,
		RootDir: filepath.Join(homeDir, "root"),
	}), nil
}

func globalRuntimeHomeDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve user home: %w", err)
	}
	home = strings.TrimSpace(home)
	if home == "" {
		return "", fmt.Errorf("user home is empty")
	}
	return filepath.Clean(filepath.Join(home, ".leafwiki")), nil
}

func configureDaemonOwnerIO(cmd *exec.Cmd, cfg leafwikiRuntimeConfig) (func(), error) {
	configureDaemonOwnerProcessGroup(cmd)
	if !cfg.MCPTransports.Stdio && !cfg.DetachDaemonOwnerIO {
		cmd.Stdin = nil
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		return func() {}, nil
	}

	nullDevice, closeNullDevice, err := openDaemonNullDevice()
	if err != nil {
		return nil, err
	}
	cmd.Stdin = nullDevice
	cmd.Stdout = nullDevice
	cmd.Stderr = nullDevice
	return closeNullDevice, nil
}

func configureDaemonOwnerProcessGroup(cmd *exec.Cmd) {
	attr := &syscall.SysProcAttr{}
	if setSysProcAttrBool(attr, "Setpgid", true) {
		cmd.SysProcAttr = attr
	}
}

func setSysProcAttrBool(attr *syscall.SysProcAttr, field string, value bool) bool {
	if attr == nil {
		return false
	}
	v := reflect.ValueOf(attr).Elem().FieldByName(field)
	if !v.IsValid() || !v.CanSet() || v.Kind() != reflect.Bool {
		return false
	}
	v.SetBool(value)
	return true
}

func openDaemonNullDevice() (*os.File, func(), error) {
	nullDevice, err := os.OpenFile(os.DevNull, os.O_RDWR, 0)
	if err != nil {
		return nil, func() {}, fmt.Errorf("open daemon null device: %w", err)
	}
	return nullDevice, func() {
		_ = nullDevice.Close()
	}, nil
}

func daemonOwnerEnv() []string {
	env := []string{}
	secretKeys := map[string]bool{
		"LEAFWIKI_MCP_API_KEY":            true,
		"LEAFWIKI_RUN_MCP_API_KEY":        true,
		"LEAFWIKI_JWT_SECRET":             true,
		"LEAFWIKI_RUN_MCP_JWT_SECRET":     true,
		"LEAFWIKI_ADMIN_PASSWORD":         true,
		"LEAFWIKI_RUN_MCP_ADMIN_PASSWORD": true,
	}
	for _, entry := range os.Environ() {
		key, _, ok := strings.Cut(entry, "=")
		if ok && secretKeys[key] {
			continue
		}
		env = append(env, entry)
	}
	return env
}

func runDaemonHeartbeat(ctx context.Context, client *projectdaemon.Client, sessionID string, interval time.Duration) error {
	if interval <= 0 {
		interval = 2 * time.Second
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			if err := client.HeartbeatSession(ctx, sessionID); err != nil {
				return err
			}
		}
	}
}

type daemonStdioBridge struct {
	EndpointURL      string
	ControlToken     string
	AuthControlURL   string
	AuthControlToken string
	WorkspaceID      string
	APIKey           string
	ActorContext     string
	Stdin            io.ReadCloser
	Stdout           io.Writer
	Stderr           io.Writer
}

func runDaemonStdioBridge(parent context.Context, cfg daemonStdioBridge) error {
	if cfg.Stdin == nil {
		cfg.Stdin = os.Stdin
	}
	if cfg.Stdout == nil {
		cfg.Stdout = os.Stdout
	}
	ctx, cancel := context.WithCancel(parent)
	defer cancel()
	var closeStdin sync.Once
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(signals)
	go func() {
		select {
		case <-signals:
			cancel()
			closeStdin.Do(func() {
				_ = cfg.Stdin.Close()
			})
		case <-ctx.Done():
			closeStdin.Do(func() {
				_ = cfg.Stdin.Close()
			})
		}
	}()
	protocolStdout := &lockedWriter{Writer: cfg.Stdout}
	filteredStdin, filterDone := newNativeStdioJSONFilter(cfg.Stdin, protocolStdout)
	stdioTransport := &sdkmcp.IOTransport{
		Reader: filteredStdin,
		Writer: nopWriteCloser{Writer: protocolStdout},
	}
	httpTransport := &sdkmcp.StreamableClientTransport{
		Endpoint:             cfg.EndpointURL,
		HTTPClient:           daemonStdioBridgeHTTPClient(cfg),
		DisableStandaloneSSE: true,
		MaxRetries:           -1,
	}
	err := bridgeTransports(ctx, stdioTransport, httpTransport)
	cancel()
	select {
	case filterErr := <-filterDone:
		if filterErr != nil && !isCleanNativeStdioClose(filterErr) {
			return fmt.Errorf("MCP STDIO input failed: %w", filterErr)
		}
	default:
	}
	if !isCleanNativeStdioClose(err) {
		return fmt.Errorf("MCP STDIO failed: %w", err)
	}
	return nil
}

func daemonStdioBridgeHTTPClient(cfg daemonStdioBridge) *http.Client {
	return &http.Client{
		Transport: daemonStdioBridgeHTTPTransport(cfg, &http.Transport{
			Proxy:                 http.ProxyFromEnvironment,
			DialContext:           (&net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
			ForceAttemptHTTP2:     true,
			MaxIdleConns:          100,
			IdleConnTimeout:       90 * time.Second,
			TLSHandshakeTimeout:   10 * time.Second,
			ExpectContinueTimeout: 1 * time.Second,
		}),
	}
}

func daemonStdioBridgeHTTPTransport(cfg daemonStdioBridge, base http.RoundTripper) http.RoundTripper {
	actorContext := cfg.ActorContext
	if strings.TrimSpace(cfg.APIKey) != "" &&
		strings.TrimSpace(cfg.AuthControlURL) != "" &&
		strings.TrimSpace(cfg.AuthControlToken) != "" {
		actorContext = ""
	}
	authTransport := projectdaemon.AuthRoundTripper{
		Base:         base,
		ControlToken: cfg.ControlToken,
		BearerToken:  cfg.APIKey,
		ActorContext: actorContext,
	}
	if strings.TrimSpace(cfg.APIKey) == "" ||
		strings.TrimSpace(cfg.AuthControlURL) == "" ||
		strings.TrimSpace(cfg.AuthControlToken) == "" {
		return authTransport
	}
	return stdioActorContextRoundTripper{
		Base:             authTransport,
		AuthControlURL:   cfg.AuthControlURL,
		AuthControlToken: cfg.AuthControlToken,
		WorkspaceID:      cfg.WorkspaceID,
		APIKey:           cfg.APIKey,
	}
}

type stdioActorContextRoundTripper struct {
	Base             http.RoundTripper
	AuthControlURL   string
	AuthControlToken string
	WorkspaceID      string
	APIKey           string
}

func (rt stdioActorContextRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	actorContext, err := rt.actorContext(req)
	if err != nil {
		return nil, err
	}
	next := rt.Base
	if next == nil {
		next = http.DefaultTransport
	}
	clone := req.Clone(req.Context())
	clone.Header = req.Header.Clone()
	clone.Header.Set(projectdaemon.ActorContextHeader, actorContext)
	return next.RoundTrip(clone)
}

func (rt stdioActorContextRoundTripper) actorContext(req *http.Request) (string, error) {
	source := &http.Request{
		Method: http.MethodPost,
		URL:    &url.URL{Path: "/mcp"},
		Header: http.Header{},
	}
	if req != nil {
		source = req.Clone(req.Context())
		source.Header = req.Header.Clone()
	}
	source.Header.Set("Authorization", "Bearer "+strings.TrimSpace(rt.APIKey))
	if strings.TrimSpace(rt.WorkspaceID) != "" {
		source.Header.Set(projectdaemon.WorkspaceIDHeader, strings.TrimSpace(rt.WorkspaceID))
	}
	out := struct {
		Actor projectdaemon.ActorContext `json:"actor"`
	}{}
	if err := callWikidPrivateEndpoint(source.Context(), rt.AuthControlURL, rt.AuthControlToken, "/__leafwiki/actor-context", source, &out); err != nil {
		if strings.Contains(err.Error(), "unauthorized") || strings.Contains(err.Error(), "invalid") {
			return "", fmt.Errorf("unauthorized native STDIO API key: %w", err)
		}
		return "", fmt.Errorf("resolve native STDIO actor context: %w", err)
	}
	encoded, err := projectdaemon.EncodeActorContext(out.Actor)
	if err != nil {
		return "", fmt.Errorf("encode native STDIO actor context: %w", err)
	}
	return encoded, nil
}

func bridgeTransports(ctx context.Context, left sdkmcp.Transport, right sdkmcp.Transport) error {
	leftConn, err := left.Connect(ctx)
	if err != nil {
		return err
	}
	defer leftConn.Close()
	rightConn, err := right.Connect(ctx)
	if err != nil {
		return err
	}
	defer rightConn.Close()

	type pumpResult struct {
		fromLeft  bool
		forwarded int
		err       error
	}
	errs := make(chan pumpResult, 2)
	pump := func(from sdkmcp.Connection, to sdkmcp.Connection, fromLeft bool) {
		forwarded := 0
		for {
			msg, err := from.Read(ctx)
			if err != nil {
				errs <- pumpResult{fromLeft: fromLeft, forwarded: forwarded, err: err}
				return
			}
			if err := to.Write(ctx, msg); err != nil {
				errs <- pumpResult{fromLeft: fromLeft, forwarded: forwarded, err: err}
				return
			}
			forwarded++
		}
	}
	go pump(leftConn, rightConn, true)
	go pump(rightConn, leftConn, false)
	result := <-errs
	err = result.err
	if result.fromLeft && isCleanNativeStdioClose(result.err) {
		if result.forwarded == 0 {
			_ = leftConn.Close()
			_ = rightConn.Close()
			return nil
		}
		timer := time.NewTimer(2 * time.Second)
		defer timer.Stop()
		select {
		case responseResult := <-errs:
			err = responseResult.err
		case <-timer.C:
			err = nil
		case <-ctx.Done():
			err = ctx.Err()
		}
	}
	_ = leftConn.Close()
	_ = rightConn.Close()
	return err
}

func waitForForegroundSession(ctx context.Context, heartbeatErr <-chan error) error {
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(signals)
	select {
	case <-ctx.Done():
		return ctx.Err()
	case err := <-heartbeatErr:
		if err == nil || errors.Is(err, context.Canceled) {
			return nil
		}
		return fmt.Errorf("project daemon heartbeat failed: %w", err)
	case <-signals:
		return nil
	}
}

func runInternalProjectDaemon(ctx context.Context, startupPath string) error {
	raw, err := os.ReadFile(startupPath)
	if err != nil {
		return fmt.Errorf("read daemon startup config: %w", err)
	}
	_ = os.Remove(startupPath)
	var cfg leafwikiRuntimeConfig
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return fmt.Errorf("decode daemon startup config: %w", err)
	}
	err = runProjectDaemonOwner(ctx, cfg)
	if err != nil {
		writeProjectDaemonStartupError(cfg.DaemonStartupErrorPath, err)
	}
	return err
}

type wikidFrontdRuntime struct {
	mu             sync.Mutex
	cfg            leafwikiRuntimeConfig
	daemonToken    string
	ctx            context.Context
	cancel         context.CancelFunc
	supervisor     *wikid.Supervisor
	processes      map[projectdaemon.RoleName]*internalRuntimeRoleProcess
	roles          []projectdaemon.RoleHealth
	workspacedURL  string
	wikidURL       string
	stopping       bool
	onRolesChanged func([]projectdaemon.RoleHealth)
}

type internalRuntimeRoleStartupConfig struct {
	Role          projectdaemon.RoleName `json:"role"`
	Runtime       leafwikiRuntimeConfig  `json:"runtime"`
	WorkspacedURL string                 `json:"workspacedUrl,omitempty"`
	WikidURL      string                 `json:"wikidUrl,omitempty"`
	DaemonToken   string                 `json:"daemonToken"`
	ReadyPath     string                 `json:"readyPath"`
	ParentPID     int                    `json:"parentPid"`
}

type internalRuntimeRoleReady struct {
	Role    projectdaemon.RoleName `json:"role"`
	PID     int                    `json:"pid"`
	URL     string                 `json:"url,omitempty"`
	Private bool                   `json:"private,omitempty"`
}

type internalRuntimeRoleProcess struct {
	role     projectdaemon.RoleName
	pid      int
	process  *os.Process
	done     <-chan error
	waitOnce sync.Once
	waitDone chan struct{}
	waitErr  error
}

type federatedWorkspaceManager struct {
	mu               sync.Mutex
	base             leafwikiRuntimeConfig
	daemonToken      string
	wikidURL         string
	layout           wikid.Layout
	supervisor       *wikid.WorkspaceSupervisor
	processes        map[string]*internalRuntimeRoleProcess
	descriptors      map[string][]string
	workspaces       map[string]wikid.WorkspaceRecord
	ensureGroup      singleflight.Group
	stopped          bool
	startRole        func(internalRuntimeRoleStartupConfig) (*internalRuntimeRoleProcess, internalRuntimeRoleReady, error)
	removeDescriptor func(string) error
	writeDescriptor  func(wikid.WorkspaceRecord, leafwikiRuntimeConfig, internalRuntimeRoleReady) error
}

func newFederatedWorkspaceManager(base leafwikiRuntimeConfig, daemonToken string, wikidURL string, layout wikid.Layout, supervisor *wikid.WorkspaceSupervisor) *federatedWorkspaceManager {
	return &federatedWorkspaceManager{
		base:             base,
		daemonToken:      daemonToken,
		wikidURL:         wikidURL,
		layout:           layout,
		supervisor:       supervisor,
		processes:        map[string]*internalRuntimeRoleProcess{},
		descriptors:      map[string][]string{},
		workspaces:       map[string]wikid.WorkspaceRecord{},
		startRole:        startInternalRuntimeRoleProcess,
		removeDescriptor: projectdaemon.RemoveDescriptor,
	}
}

func (m *federatedWorkspaceManager) MarkReady(workspaceID string, pid int, url string) {
	if m == nil || m.supervisor == nil {
		return
	}
	m.supervisor.MarkReady(workspaceID, pid, url)
}

func (m *federatedWorkspaceManager) Ensure(ctx context.Context, workspace wikid.WorkspaceRecord) (wikid.WorkspaceStatus, error) {
	if m == nil || m.supervisor == nil {
		return wikid.WorkspaceStatus{}, fmt.Errorf("workspace manager is unavailable")
	}
	workspaceID := strings.TrimSpace(workspace.ID)
	if workspaceID == "" {
		return wikid.WorkspaceStatus{}, fmt.Errorf("workspace ID is required")
	}
	m.mu.Lock()
	if current := m.supervisor.Status(workspaceID); current.State == wikid.WorkspaceStateRunning {
		if proc := m.processes[workspaceID]; proc == nil || !proc.isDone() {
			m.mu.Unlock()
			return current, nil
		}
	}
	m.mu.Unlock()

	resultCh := m.ensureGroup.DoChan(workspaceID, func() (any, error) {
		return m.ensureWorkspace(workspaceID, workspace)
	})
	select {
	case <-ctx.Done():
		return m.supervisor.Status(workspaceID), ctx.Err()
	case result := <-resultCh:
		status, ok := result.Val.(wikid.WorkspaceStatus)
		if !ok && result.Err == nil {
			return wikid.WorkspaceStatus{}, fmt.Errorf("ensure workspace %q returned unexpected result %T", workspaceID, result.Val)
		}
		return status, result.Err
	}
}

func (m *federatedWorkspaceManager) ensureWorkspace(workspaceID string, workspace wikid.WorkspaceRecord) (wikid.WorkspaceStatus, error) {
	m.mu.Lock()
	if current := m.supervisor.Status(workspaceID); current.State == wikid.WorkspaceStateRunning {
		if proc := m.processes[workspaceID]; proc == nil || !proc.isDone() {
			m.mu.Unlock()
			return current, nil
		}
	}
	m.workspaces[workspaceID] = workspace
	m.supervisor.MarkStarting(workspaceID)
	cfg := m.workspaceRuntimeConfig(workspace, "0")
	m.mu.Unlock()

	proc, ready, err := m.startRole(internalRuntimeRoleStartupConfig{
		Role:        projectdaemon.RoleWorkspaced,
		Runtime:     cfg,
		DaemonToken: m.daemonToken,
		WikidURL:    m.wikidURL,
	})
	if err != nil {
		m.supervisor.RecordCrash(workspaceID, err.Error())
		return m.supervisor.Status(workspaceID), err
	}
	writeDescriptor := m.writeWorkspaceDescriptor
	if m.writeDescriptor != nil {
		writeDescriptor = m.writeDescriptor
	}
	if err := writeDescriptor(workspace, cfg, ready); err != nil {
		stopCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		_ = proc.stop(stopCtx)
		cancel()
		m.supervisor.RecordCrash(workspaceID, err.Error())
		return m.supervisor.Status(workspaceID), err
	}

	m.mu.Lock()
	m.processes[workspaceID] = proc
	m.supervisor.MarkReady(workspaceID, ready.PID, ready.URL)
	status := m.supervisor.Status(workspaceID)
	m.mu.Unlock()
	go m.monitorWorkspaceProcess(workspaceID, proc)
	return status, nil
}

func (m *federatedWorkspaceManager) workspaceRuntimeConfig(workspace wikid.WorkspaceRecord, port string) leafwikiRuntimeConfig {
	cfg := m.base
	cfg.Workspace = wiki.Workspace{
		ID:      strings.TrimSpace(workspace.ID),
		DataDir: strings.TrimSpace(workspace.DataDir),
		RootDir: strings.TrimSpace(workspace.RootDir),
	}
	cfg.MarkdownLinkRootPrefix = strings.TrimSpace(workspace.MarkdownLinkRootPrefix)
	cfg.Host = "127.0.0.1"
	cfg.Port = port
	cfg.EnableWorkspaceSync = true
	cfg.EnableRevision = false
	return cfg
}

func (m *federatedWorkspaceManager) writeWorkspaceDescriptor(workspace wikid.WorkspaceRecord, cfg leafwikiRuntimeConfig, ready internalRuntimeRoleReady) error {
	ownerCfg, err := daemonConfigForRuntime(cfg)
	if err != nil {
		return err
	}
	hash, err := projectdaemon.ConfigHash(ownerCfg)
	if err != nil {
		return err
	}
	privateMCPURL := strings.TrimRight(ready.URL, "/") + "/mcp"
	desc := &projectdaemon.Descriptor{
		SchemaVersion:   projectdaemon.DescriptorSchemaVersion,
		RuntimeStack:    cfg.RuntimeStack,
		Role:            projectdaemon.RoleWorkspaced,
		WorkspaceID:     workspace.ID,
		PID:             ready.PID,
		StartedAt:       time.Now().UTC(),
		DataDir:         ownerCfg.DataDir,
		RootDir:         ownerCfg.RootDir,
		BasePath:        cfg.BasePath,
		ControlURL:      m.wikidURL,
		PrivateMCPURL:   privateMCPURL,
		PrivateMCPToken: m.daemonToken,
		ConfigHash:      hash,
		IdleTimeout:     cfg.DaemonIdleTimeout.String(),
		ControlToken:    m.daemonToken,
		Config:          ownerCfg,
		Roles: []projectdaemon.RoleHealth{
			{Name: projectdaemon.RoleWorkspaced, State: projectdaemon.RoleStateReady, PID: ready.PID, URL: ready.URL, Private: true, UpdatedAt: time.Now().UTC()},
		},
	}
	paths := []string{
		projectdaemon.DescriptorPath(ownerCfg.DataDir),
		workspaceRuntimeDescriptorPath(m.layout.RuntimeDir, workspace.ID),
	}
	for _, path := range paths {
		if err := removeNonRegularDescriptor(path); err != nil {
			return err
		}
		if err := projectdaemon.WriteDescriptorAtomic(path, desc); err != nil {
			return err
		}
	}
	m.mu.Lock()
	m.descriptors[workspace.ID] = paths
	m.mu.Unlock()
	return nil
}

func removeNonRegularDescriptor(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("inspect descriptor target %s: %w", path, err)
	}
	if info.Mode().IsRegular() {
		return nil
	}
	if err := projectdaemon.RemoveDescriptor(path); err != nil {
		return fmt.Errorf("remove non-regular descriptor %s: %w", path, err)
	}
	return nil
}

func (m *federatedWorkspaceManager) monitorWorkspaceProcess(workspaceID string, proc *internalRuntimeRoleProcess) {
	err := proc.wait()
	m.mu.Lock()
	current := m.processes[workspaceID]
	if current != proc {
		m.mu.Unlock()
		return
	}
	delete(m.processes, workspaceID)
	descriptorPaths := append([]string(nil), m.descriptors[workspaceID]...)
	delete(m.descriptors, workspaceID)
	workspace := m.workspaces[workspaceID]
	stopped := m.stopped
	message := "process exited"
	if err != nil {
		message = err.Error()
	}
	restartAt, restart := m.supervisor.RecordCrash(workspaceID, message)
	m.mu.Unlock()
	m.removeDescriptors(descriptorPaths)
	if restart && !stopped && strings.TrimSpace(workspace.ID) != "" {
		go m.restartWorkspaceAfter(workspace, restartAt)
	}
}

func (m *federatedWorkspaceManager) removeDescriptors(paths []string) {
	for _, path := range paths {
		if err := m.removeDescriptor(path); err != nil {
			fmt.Fprintf(os.Stderr, "leafwiki: remove workspace descriptor %s: %v\n", path, err)
		}
	}
}

func (m *federatedWorkspaceManager) restartWorkspaceAfter(workspace wikid.WorkspaceRecord, restartAt time.Time) {
	delay := time.Until(restartAt)
	if delay > 0 {
		timer := time.NewTimer(delay)
		<-timer.C
	}
	m.mu.Lock()
	stopped := m.stopped
	m.mu.Unlock()
	if stopped {
		return
	}
	_, _ = m.Ensure(context.Background(), workspace)
}

func (m *federatedWorkspaceManager) stop(ctx context.Context) error {
	if m == nil {
		return nil
	}
	m.mu.Lock()
	m.stopped = true
	processes := make([]*internalRuntimeRoleProcess, 0, len(m.processes))
	for _, proc := range m.processes {
		processes = append(processes, proc)
	}
	descriptors := make([]string, 0)
	for _, paths := range m.descriptors {
		descriptors = append(descriptors, paths...)
	}
	m.processes = map[string]*internalRuntimeRoleProcess{}
	m.mu.Unlock()
	var errs []error
	for _, proc := range processes {
		if err := proc.stop(ctx); err != nil {
			errs = append(errs, err)
		}
	}
	for _, path := range descriptors {
		if err := m.removeDescriptor(path); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

func workspaceRuntimeDescriptorPath(runtimeDir string, workspaceID string) string {
	return filepath.Join(runtimeDir, "workspaces", strings.TrimSpace(workspaceID)+".json")
}

func startWikidFrontdRuntime(parent context.Context, cfg leafwikiRuntimeConfig, daemonToken string, wikidURL string) (*wikidFrontdRuntime, error) {
	ctx, cancel := context.WithCancel(parent)
	runtime := &wikidFrontdRuntime{
		cfg:         cfg,
		daemonToken: daemonToken,
		ctx:         ctx,
		cancel:      cancel,
		supervisor:  wikid.NewSupervisor(wikid.SupervisorOptions{}),
		processes:   map[projectdaemon.RoleName]*internalRuntimeRoleProcess{},
		wikidURL:    wikidURL,
	}
	runtime.supervisor.MarkReady(projectdaemon.RoleWikid, os.Getpid(), "", false)
	if err := runtime.withProcessLock(func() error {
		if err := runtime.startWorkspacedLocked(); err != nil {
			return err
		}
		if err := runtime.startFrontdLocked(); err != nil {
			return err
		}
		runtime.publishRolesLocked()
		return nil
	}); err != nil {
		cancel()
		stopCtx, stopCancel := context.WithTimeout(context.Background(), 2*time.Second)
		_ = runtime.stop(stopCtx)
		stopCancel()
		return nil, err
	}
	return runtime, nil
}

func (s *wikidFrontdRuntime) startWorkspacedLocked() error {
	workspacedCfg := s.cfg
	workspacedCfg.Host = "127.0.0.1"
	workspacedCfg.Port = "0"
	proc, ready, err := startInternalRuntimeRoleProcess(internalRuntimeRoleStartupConfig{
		Role:        projectdaemon.RoleWorkspaced,
		Runtime:     workspacedCfg,
		DaemonToken: s.daemonToken,
	})
	if err != nil {
		return err
	}
	s.processes[projectdaemon.RoleWorkspaced] = proc
	s.workspacedURL = ready.URL
	s.supervisor.MarkReady(projectdaemon.RoleWorkspaced, ready.PID, ready.URL, true)
	go s.monitorRoleProcess(proc)
	return nil
}

func (s *wikidFrontdRuntime) startFrontdLocked() error {
	if strings.TrimSpace(s.workspacedURL) == "" {
		return fmt.Errorf("workspaced URL is unavailable")
	}
	proc, ready, err := startInternalRuntimeRoleProcess(internalRuntimeRoleStartupConfig{
		Role:          projectdaemon.RoleFrontd,
		Runtime:       s.cfg,
		WorkspacedURL: s.workspacedURL,
		WikidURL:      s.wikidURL,
		DaemonToken:   s.daemonToken,
	})
	if err != nil {
		return err
	}
	s.processes[projectdaemon.RoleFrontd] = proc
	s.supervisor.MarkReady(projectdaemon.RoleFrontd, ready.PID, ready.URL, false)
	go s.monitorRoleProcess(proc)
	return nil
}

func (s *wikidFrontdRuntime) monitorRoleProcess(proc *internalRuntimeRoleProcess) {
	err := proc.wait()
	s.mu.Lock()
	current := s.processes[proc.role]
	if s.stopping || current != proc {
		s.mu.Unlock()
		return
	}
	message := "process exited"
	if err != nil {
		message = err.Error()
	}
	restartAt, scheduled := s.supervisor.RecordCrash(proc.role, message)
	s.publishRolesLocked()
	if !scheduled {
		s.mu.Unlock()
		return
	}
	delay := time.Until(restartAt)
	s.mu.Unlock()
	if delay > 0 {
		timer := time.NewTimer(delay)
		select {
		case <-timer.C:
		case <-s.ctx.Done():
			timer.Stop()
			return
		}
	}
	s.restartRole(proc.role)
}

func (s *wikidFrontdRuntime) restartRole(role projectdaemon.RoleName) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.stopping || s.ctx.Err() != nil {
		return
	}
	var err error
	switch role {
	case projectdaemon.RoleWorkspaced:
		err = s.startWorkspacedLocked()
	case projectdaemon.RoleFrontd:
		err = s.startFrontdLocked()
	default:
		err = fmt.Errorf("unsupported restart role %s", role)
	}
	if err != nil {
		s.supervisor.RecordCrash(role, err.Error())
	}
	s.publishRolesLocked()
}

func (s *wikidFrontdRuntime) setRoleChangeCallback(callback func([]projectdaemon.RoleHealth)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.onRolesChanged = callback
	s.publishRolesLocked()
}

func (s *wikidFrontdRuntime) withProcessLock(fn func() error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return fn()
}

func (s *wikidFrontdRuntime) roleHealthSnapshot() []projectdaemon.RoleHealth {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]projectdaemon.RoleHealth(nil), s.roles...)
}

func (s *wikidFrontdRuntime) publishRolesLocked() {
	roles := s.supervisor.Roles()
	s.roles = roles
	if s.onRolesChanged != nil {
		s.onRolesChanged(append([]projectdaemon.RoleHealth(nil), roles...))
	}
}

func requiredRuntimeRoleHealth() []projectdaemon.RoleName {
	return []projectdaemon.RoleName{
		projectdaemon.RoleWikid,
		projectdaemon.RoleFrontd,
		projectdaemon.RoleWorkspaced,
	}
}

func startInternalRuntimeRoleProcess(startup internalRuntimeRoleStartupConfig) (*internalRuntimeRoleProcess, internalRuntimeRoleReady, error) {
	readyFile, err := os.CreateTemp("", "leafwiki-runtime-ready-*.json")
	if err != nil {
		return nil, internalRuntimeRoleReady{}, fmt.Errorf("create %s ready file: %w", startup.Role, err)
	}
	readyPath := readyFile.Name()
	_ = readyFile.Close()
	_ = os.Remove(readyPath)
	startup.ReadyPath = readyPath
	startup.ParentPID = os.Getpid()

	startupPath, err := writeInternalRuntimeRoleStartupConfig(startup)
	if err != nil {
		return nil, internalRuntimeRoleReady{}, err
	}
	removeStartupConfig := true
	defer func() {
		if removeStartupConfig {
			_ = os.Remove(startupPath)
		}
	}()

	exe, err := projectDaemonExecutable()
	if err != nil {
		return nil, internalRuntimeRoleReady{}, err
	}
	cmd := exec.Command(exe, internalRuntimeRoleArgs(startupPath)...)
	cmd.Env = daemonOwnerEnv()
	configureDaemonOwnerProcessGroup(cmd)
	cleanupIO, err := configureInternalRuntimeRoleIO(cmd, startup.Runtime)
	if err != nil {
		return nil, internalRuntimeRoleReady{}, err
	}
	if err := cmd.Start(); err != nil {
		cleanupIO()
		return nil, internalRuntimeRoleReady{}, fmt.Errorf("start %s role process: %w", startup.Role, err)
	}
	cleanupIO()
	if cmd.Process == nil {
		return nil, internalRuntimeRoleReady{}, fmt.Errorf("start %s role process: missing process handle", startup.Role)
	}
	removeStartupConfig = false
	scheduleProjectDaemonStartupConfigCleanup(startupPath)

	done := make(chan error, 1)
	go func() {
		done <- cmd.Wait()
	}()
	proc := &internalRuntimeRoleProcess{
		role:     startup.Role,
		pid:      cmd.Process.Pid,
		process:  cmd.Process,
		done:     done,
		waitDone: make(chan struct{}),
	}
	ready, err := waitForInternalRuntimeRoleReady(readyPath, proc, 10*time.Second)
	_ = os.Remove(readyPath)
	if err != nil {
		stopCtx, stopCancel := context.WithTimeout(context.Background(), 2*time.Second)
		_ = proc.stop(stopCtx)
		stopCancel()
		return nil, internalRuntimeRoleReady{}, err
	}
	if ready.Role != startup.Role {
		stopCtx, stopCancel := context.WithTimeout(context.Background(), 2*time.Second)
		_ = proc.stop(stopCtx)
		stopCancel()
		return nil, internalRuntimeRoleReady{}, fmt.Errorf("%s role reported readiness for %s", startup.Role, ready.Role)
	}
	return proc, ready, nil
}

func writeInternalRuntimeRoleStartupConfig(startup internalRuntimeRoleStartupConfig) (string, error) {
	raw, err := json.Marshal(startup)
	if err != nil {
		return "", err
	}
	tmp, err := os.CreateTemp("", "leafwiki-runtime-role-*.json")
	if err != nil {
		return "", fmt.Errorf("create %s startup config: %w", startup.Role, err)
	}
	path := tmp.Name()
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		_ = os.Remove(path)
		return "", err
	}
	if _, err := tmp.Write(raw); err != nil {
		_ = tmp.Close()
		_ = os.Remove(path)
		return "", err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(path)
		return "", err
	}
	return path, nil
}

func waitForInternalRuntimeRoleReady(path string, proc *internalRuntimeRoleProcess, timeout time.Duration) (internalRuntimeRoleReady, error) {
	deadline := time.NewTimer(timeout)
	defer deadline.Stop()
	ticker := time.NewTicker(25 * time.Millisecond)
	defer ticker.Stop()
	done := make(chan error, 1)
	go func() {
		done <- proc.wait()
	}()
	for {
		select {
		case err := <-done:
			if err == nil {
				err = fmt.Errorf("process exited before readiness")
			}
			return internalRuntimeRoleReady{}, err
		case <-deadline.C:
			return internalRuntimeRoleReady{}, fmt.Errorf("runtime role did not become ready before timeout")
		case <-ticker.C:
			raw, err := os.ReadFile(path)
			if err != nil {
				if errors.Is(err, os.ErrNotExist) {
					continue
				}
				return internalRuntimeRoleReady{}, err
			}
			if len(strings.TrimSpace(string(raw))) == 0 {
				continue
			}
			var ready internalRuntimeRoleReady
			if err := json.Unmarshal(raw, &ready); err != nil {
				return internalRuntimeRoleReady{}, err
			}
			if ready.PID <= 0 {
				return internalRuntimeRoleReady{}, fmt.Errorf("runtime role reported invalid PID")
			}
			return ready, nil
		}
	}
}

func writeInternalRuntimeRoleReady(path string, ready internalRuntimeRoleReady) error {
	if strings.TrimSpace(path) == "" {
		return nil
	}
	raw, err := json.Marshal(ready)
	if err != nil {
		return err
	}
	return os.WriteFile(path, raw, 0o600)
}

func internalRuntimeRoleArgs(startupPath string) []string {
	args := []string{"--internal-runtime-role", startupPath}
	if os.Getenv("GO_WANT_LEAFWIKI_HELPER_PROCESS") == "1" {
		args = []string{"-test.run=TestLeafWikiHelperProcess", "--", "--internal-runtime-role", startupPath}
	}
	return args
}

func configureInternalRuntimeRoleIO(cmd *exec.Cmd, cfg leafwikiRuntimeConfig) (func(), error) {
	if !cfg.MCPTransports.Stdio && !cfg.DetachDaemonOwnerIO {
		cmd.Stdin = nil
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		return func() {}, nil
	}
	nullDevice, closeNullDevice, err := openDaemonNullDevice()
	if err != nil {
		return nil, err
	}
	cmd.Stdin = nullDevice
	cmd.Stdout = nullDevice
	cmd.Stderr = nullDevice
	return closeNullDevice, nil
}

func (s *wikidFrontdRuntime) stop(ctx context.Context) error {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	s.stopping = true
	if s.cancel != nil {
		s.cancel()
	}
	processes := make([]*internalRuntimeRoleProcess, 0, len(s.processes))
	for _, proc := range s.processes {
		processes = append(processes, proc)
	}
	s.mu.Unlock()
	var joined error
	for _, proc := range processes {
		if err := proc.stop(ctx); err != nil {
			joined = errors.Join(joined, err)
		}
	}
	return joined
}

func (p *internalRuntimeRoleProcess) wait() error {
	if p == nil {
		return nil
	}
	p.waitOnce.Do(func() {
		p.waitErr = <-p.done
		close(p.waitDone)
	})
	<-p.waitDone
	return p.waitErr
}

func (p *internalRuntimeRoleProcess) isDone() bool {
	if p == nil {
		return true
	}
	select {
	case <-p.waitDone:
		return true
	default:
		return false
	}
}

func (p *internalRuntimeRoleProcess) stop(ctx context.Context) error {
	if p == nil || p.process == nil {
		return nil
	}
	if p.isDone() {
		return p.wait()
	}
	_ = p.process.Signal(os.Interrupt)
	done := make(chan error, 1)
	go func() {
		done <- p.wait()
	}()
	select {
	case err := <-done:
		return err
	case <-ctx.Done():
		_ = p.process.Kill()
		select {
		case err := <-done:
			return errors.Join(ctx.Err(), err)
		case <-time.After(2 * time.Second):
			return ctx.Err()
		}
	}
}

func runInternalRuntimeRole(parent context.Context, startupPath string) error {
	raw, err := os.ReadFile(startupPath)
	if err != nil {
		role := projectdaemon.RoleName(strings.TrimSpace(startupPath))
		switch role {
		case projectdaemon.RoleWikid, projectdaemon.RoleFrontd, projectdaemon.RoleWorkspaced:
			return waitForInternalRuntimeRoleSignal(parent)
		default:
			return fmt.Errorf("read runtime role startup config: %w", err)
		}
	}
	_ = os.Remove(startupPath)
	var startup internalRuntimeRoleStartupConfig
	if err := json.Unmarshal(raw, &startup); err != nil {
		return fmt.Errorf("decode runtime role startup config: %w", err)
	}
	switch startup.Role {
	case projectdaemon.RoleWikid, projectdaemon.RoleFrontd, projectdaemon.RoleWorkspaced:
	default:
		return fmt.Errorf("unsupported runtime role %q", startup.Role)
	}
	switch startup.Role {
	case projectdaemon.RoleFrontd:
		return runFrontdRole(parent, startup)
	case projectdaemon.RoleWorkspaced:
		return runWorkspacedRole(parent, startup)
	default:
		return waitForInternalRuntimeRoleSignal(parent)
	}
}

func waitForInternalRuntimeRoleSignal(parent context.Context) error {
	ctx, cancel := context.WithCancel(parent)
	defer cancel()
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(signals)

	select {
	case <-ctx.Done():
		return nil
	case <-signals:
		return nil
	}
}

func runFrontdRole(parent context.Context, startup internalRuntimeRoleStartupConfig) error {
	cfg := startup.Runtime
	ownerCfg, err := daemonConfigForRuntime(cfg)
	if err != nil {
		return err
	}
	logCloser, err := setupLogger(cfg.Logging, os.Stdout, os.Stderr)
	if err != nil {
		return fmt.Errorf("invalid logging configuration: %w", err)
	}
	defer logCloser.Close()

	opts, err := routerOptionsForRuntimeWithUserService(cfg, nil, cfg.BasePath, false, cfg.Host)
	if err != nil {
		return err
	}
	opts.HTTPRemoteUser = httpinternal.HTTPRemoteUserConfig{}
	publicRouter := httpinternal.NewRouter(nil, frontendConfigForRuntimeStorage(ownerCfg.DataDir), opts)
	controlPlaneProxy, err := frontd.NewControlPlaneProxy(startup.WikidURL, startup.DaemonToken)
	if err != nil {
		return err
	}
	workspaceProxy, err := frontd.NewWorkspaceProxy(frontd.WorkspaceProxyOptions{
		Upstream:    startup.WorkspacedURL,
		DaemonToken: startup.DaemonToken,
		Actor:       wikidActorResolver(startup.WikidURL, startup.DaemonToken),
	})
	if err != nil {
		return err
	}
	workspacesAPI, err := frontd.NewWorkspacesAPI(startup.WikidURL, startup.DaemonToken)
	if err != nil {
		return err
	}
	workspaceResolver, err := frontd.NewWikidWorkspaceResolver(startup.WikidURL, startup.DaemonToken)
	if err != nil {
		return err
	}
	actorResolver := wikidActorResolver(startup.WikidURL, startup.DaemonToken)
	workspaceRouterProxy := frontd.NewWorkspaceRouterProxy(frontd.WorkspaceRouterProxyOptions{
		Resolve: workspaceResolver,
		Actor: func(req *http.Request, workspaceID string) (projectdaemon.ActorContext, error) {
			clone := req.Clone(req.Context())
			clone.Header = req.Header.Clone()
			clone.Header.Set(projectdaemon.WorkspaceIDHeader, workspaceID)
			return actorResolver(clone)
		},
	})
	workspaceMux := http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if strings.HasPrefix(req.URL.Path, frontd.PublicWorkspacesPrefix+"/") {
			workspaceRouterProxy.ServeHTTP(w, req)
			return
		}
		workspaceProxy.ServeHTTP(w, req)
	})
	var mcpProxy http.Handler
	if cfg.MCPTransports.HTTP {
		baseMCPProxy, err := frontdPublicMCPHandler(cfg, startup.WorkspacedURL, startup.DaemonToken, startup.WikidURL)
		if err != nil {
			return err
		}
		rootMCPWorkspaceResolver, err := frontd.NewWikidSingleWorkspaceResolver(startup.WikidURL, startup.DaemonToken)
		if err != nil {
			return err
		}
		mcpSessions := frontd.NewMCPSessionBindings()
		workspaceMCP := frontd.NewWorkspaceMCPHandler(frontd.WorkspaceMCPHandlerOptions{
			Sessions:    mcpSessions,
			Resolve:     workspaceResolver,
			ResolveRoot: rootMCPWorkspaceResolver,
			Proxy: func(route frontd.WorkspaceRoute) http.Handler {
				proxy, err := frontd.NewMCPProxyWithActor(frontd.WorkspaceProxyOptions{
					Upstream:    route.Upstream,
					DaemonToken: route.DaemonToken,
					Actor: func(req *http.Request) (projectdaemon.ActorContext, error) {
						clone := req.Clone(req.Context())
						clone.Header = req.Header.Clone()
						clone.Header.Set(projectdaemon.WorkspaceIDHeader, route.WorkspaceID)
						return actorResolver(clone)
					},
				})
				if err != nil {
					return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
						http.Error(w, "workspace mcp unavailable", http.StatusServiceUnavailable)
					})
				}
				return proxy
			},
		})
		workspaceMCP = frontdMCPBearerAuthHandler(cfg, startup.WikidURL, startup.DaemonToken, workspaceMCP)
		mcpProxy = http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			if req.URL.Path == "/mcp" || strings.HasPrefix(req.URL.Path, "/mcp/workspaces/") {
				workspaceMCP.ServeHTTP(w, req)
				return
			}
			baseMCPProxy.ServeHTTP(w, req)
		})
	}
	handler := frontd.NewIngressHandler(publicRouter, frontd.IngressOptions{
		BasePath:     cfg.BasePath,
		Workspace:    workspaceMux,
		Workspaces:   workspacesAPI,
		MCP:          mcpProxy,
		ControlPlane: controlPlaneProxy,
	})
	listener, err := net.Listen("tcp", buildListenAddress(cfg.Host, cfg.Port))
	if err != nil {
		return fmt.Errorf("start frontd listener: %w", err)
	}
	defer listener.Close()
	ready := internalRuntimeRoleReady{
		Role: projectdaemon.RoleFrontd,
		PID:  os.Getpid(),
		URL:  publicURLForListener(cfg.Host, listener, cfg.BasePath),
	}
	if err := writeInternalRuntimeRoleReady(startup.ReadyPath, ready); err != nil {
		return err
	}
	return serveInternalRuntimeHTTP(parent, projectdaemon.RoleFrontd, listener, handler, startup.ParentPID)
}

func frontendConfigForRuntimeStorage(storageDir string) httpinternal.FrontendConfig {
	store := corebranding.NewBrandingStore(storageDir)
	return httpinternal.FrontendConfig{
		StorageDir: storageDir,
		GetSiteName: func() string {
			cfg, err := store.Load()
			if err != nil || cfg == nil {
				return ""
			}
			return cfg.SiteName
		},
		GetFaviconFile: func() string {
			cfg, err := store.Load()
			if err != nil || cfg == nil {
				return ""
			}
			return cfg.FaviconFile
		},
	}
}

func runWorkspacedRole(parent context.Context, startup internalRuntimeRoleStartupConfig) error {
	cfg := startup.Runtime
	ownerCfg, err := daemonConfigForRuntime(cfg)
	if err != nil {
		return err
	}
	logCloser, err := setupLogger(cfg.Logging, os.Stdout, os.Stderr)
	if err != nil {
		return fmt.Errorf("invalid logging configuration: %w", err)
	}
	defer logCloser.Close()

	w, err := newRuntimeWiki(cfg, ownerCfg, runtimeWikiWorkspaceOnly)
	if err != nil {
		return err
	}
	defer w.Close()

	opts, err := routerOptionsForRuntime(cfg, w, "", false, "127.0.0.1")
	if err != nil {
		return err
	}
	opts.HTTPRemoteUser = httpinternal.HTTPRemoteUserConfig{}
	router := workspaced.NewAuthenticatedRouter(w, opts, workspaced.PrivateAuthOptions{
		DaemonToken: startup.DaemonToken,
		WorkspaceID: runtimeWorkspaceID(cfg.Workspace),
	})
	mcpOpts := opts
	mcpOpts.MCPEnabled = true
	mcpOpts.MCPBindHost = "127.0.0.1"
	privateMCP := w.ActorContextMCPHTTPHandler(mcpOpts)
	handler := http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.URL.Path == "/mcp" {
			if req.Header.Get(projectdaemon.ControlTokenHeader) != startup.DaemonToken {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
			privateMCP.ServeHTTP(w, req)
			return
		}
		router.ServeHTTP(w, req)
	})
	listener, err := net.Listen("tcp", buildListenAddress("127.0.0.1", cfg.Port))
	if err != nil {
		return fmt.Errorf("start workspaced listener: %w", err)
	}
	defer listener.Close()
	ready := internalRuntimeRoleReady{
		Role:    projectdaemon.RoleWorkspaced,
		PID:     os.Getpid(),
		URL:     "http://" + listener.Addr().String(),
		Private: true,
	}
	if err := writeInternalRuntimeRoleReady(startup.ReadyPath, ready); err != nil {
		return err
	}
	return serveInternalRuntimeHTTP(parent, projectdaemon.RoleWorkspaced, listener, handler, startup.ParentPID)
}

func serveInternalRuntimeHTTP(parent context.Context, role projectdaemon.RoleName, listener net.Listener, handler http.Handler, parentPID int) error {
	ctx, cancel := context.WithCancel(parent)
	defer cancel()
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(signals)
	go func() {
		select {
		case <-signals:
			cancel()
		case <-ctx.Done():
		}
	}()
	go cancelWhenParentExits(ctx, cancel, parentPID, 2*time.Second)
	server := &http.Server{Addr: listener.Addr().String(), Handler: handler}
	done := make(chan error, 1)
	go func() {
		err := server.Serve(listener)
		if errors.Is(err, http.ErrServerClosed) || errors.Is(err, net.ErrClosed) {
			err = nil
		}
		done <- err
	}()
	select {
	case <-ctx.Done():
	case err := <-done:
		if err != nil {
			return fmt.Errorf("%s server failed: %w", role, err)
		}
	}
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutdownCancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		return err
	}
	return nil
}

func cancelWhenParentExits(ctx context.Context, cancel context.CancelFunc, parentPID int, interval time.Duration) {
	if parentPID <= 0 {
		return
	}
	if interval <= 0 {
		interval = 2 * time.Second
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if !processAlive(parentPID) {
				cancel()
				return
			}
		}
	}
}

func processAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	err := syscall.Kill(pid, 0)
	return err == nil || errors.Is(err, syscall.EPERM)
}

func publicURLForListener(host string, listener net.Listener, basePath string) string {
	_, port, err := net.SplitHostPort(listener.Addr().String())
	if err != nil || port == "" {
		return "http://" + listener.Addr().String() + basePath
	}
	return nativeStdioHTTPURL(host, port, basePath)
}

type runtimeWikiMode string

const (
	runtimeWikiFull             runtimeWikiMode = "full"
	runtimeWikiWorkspaceOnly    runtimeWikiMode = "workspace-only"
	runtimeWikiControlPlaneOnly runtimeWikiMode = "control-plane-only"
)

func newRuntimeWiki(cfg leafwikiRuntimeConfig, ownerCfg projectdaemon.Config, mode runtimeWikiMode) (*wiki.Wiki, error) {
	authStorageDir := ""
	if cfg.RuntimeStack == projectdaemon.RuntimeStackWikidFrontd {
		authStorageDir = authStorageDirForRuntime(ownerCfg.DataDir, cfg.RuntimeStack)
	}
	w, err := wiki.NewWiki(&wiki.WikiOptions{
		Workspace:              wiki.Workspace{ID: cfg.Workspace.ID, DataDir: ownerCfg.DataDir, RootDir: ownerCfg.RootDir},
		StorageDir:             ownerCfg.DataDir,
		AuthStorageDir:         authStorageDir,
		WorkspaceOnly:          mode == runtimeWikiWorkspaceOnly,
		ControlPlaneOnly:       mode == runtimeWikiControlPlaneOnly,
		AdminPassword:          cfg.AdminPassword,
		JWTSecret:              cfg.JWTSecret,
		AccessTokenTimeout:     cfg.AccessTokenTimeout,
		RefreshTokenTimeout:    cfg.RefreshTokenTimeout,
		AuthDisabled:           cfg.DisableAuth,
		EnableRevision:         cfg.EnableRevision,
		EnableWorkspaceSync:    cfg.EnableWorkspaceSync,
		MaxRevisionHistory:     cfg.MaxRevisionHistory,
		MarkdownLinkRootPrefix: cfg.MarkdownLinkRootPrefix,
	})
	if err != nil {
		return nil, fmt.Errorf("initialize Wiki: %w", err)
	}
	return w, nil
}

func routerOptionsForRuntime(cfg leafwikiRuntimeConfig, w *wiki.Wiki, basePath string, enableMCP bool, mcpBindHost string) (httpinternal.RouterOptions, error) {
	return routerOptionsForRuntimeWithUserService(cfg, w.UserService(), basePath, enableMCP, mcpBindHost)
}

func controlPlaneRouterOptionsForRuntime(cfg leafwikiRuntimeConfig, w *wiki.Wiki) (httpinternal.RouterOptions, error) {
	return routerOptionsForRuntime(cfg, w, cfg.BasePath, cfg.MCPTransports.HTTP, cfg.Host)
}

func routerOptionsForRuntimeWithUserService(cfg leafwikiRuntimeConfig, userService *coreauth.UserService, basePath string, enableMCP bool, mcpBindHost string) (httpinternal.RouterOptions, error) {
	trustedProxies, err := authmw.ParseTrustedProxies(cfg.TrustedProxyIPsRaw)
	if err != nil {
		return httpinternal.RouterOptions{}, fmt.Errorf("invalid trusted proxies: %w", err)
	}
	return buildHTTPRouterOptions(httpRouterOptionsInput{
		publicAccess:            cfg.PublicAccess,
		injectCodeInHeader:      cfg.InjectCodeInHeader,
		customStylesheet:        cfg.CustomStylesheet,
		allowInsecure:           cfg.AllowInsecure,
		hideLinkMetadataSection: cfg.HideLinkMetadataSection,
		accessTokenTimeout:      cfg.AccessTokenTimeout,
		refreshTokenTimeout:     cfg.RefreshTokenTimeout,
		authDisabled:            cfg.DisableAuth,
		basePath:                basePath,
		markdownLinkRootPrefix:  cfg.MarkdownLinkRootPrefix,
		maxAssetUploadSize:      cfg.MaxAssetUploadSize,
		enableRevision:          cfg.EnableRevision,
		enableWorkspaceSync:     cfg.EnableWorkspaceSync,
		enableLinkRefactor:      cfg.EnableLinkRefactor,
		enableMCP:               enableMCP,
		host:                    mcpBindHost,
		httpRemoteUser: httpinternal.HTTPRemoteUserConfig{
			Enabled:        cfg.EnableHTTPRemoteUser,
			HeaderName:     cfg.HTTPRemoteUserHeader,
			TrustedProxies: trustedProxies,
			UserService:    userService,
			LogoutURL:      cfg.HTTPRemoteUserLogoutURL,
		},
		disableRequestLog: cfg.DisableRequestLog,
	}), nil
}

func frontdActorResolver(w *wiki.Wiki, cfg leafwikiRuntimeConfig) func(*http.Request) (projectdaemon.ActorContext, error) {
	return func(req *http.Request) (projectdaemon.ActorContext, error) {
		user, method, err := frontdActorUser(req, w, cfg)
		if err != nil {
			return projectdaemon.ActorContext{}, err
		}
		return actorContextForUser(user, method, cfg)
	}
}

func frontdPublicMCPHandler(cfg leafwikiRuntimeConfig, workspacedURL string, daemonToken string, wikidURL string) (http.Handler, error) {
	proxy, err := frontd.NewMCPProxyWithActor(frontd.WorkspaceProxyOptions{
		Upstream:    workspacedURL,
		DaemonToken: daemonToken,
		Actor:       wikidActorResolver(wikidURL, daemonToken),
	})
	if err != nil {
		return nil, err
	}
	if cfg.DisableAuth {
		return proxy, nil
	}
	return frontdMCPBearerAuthHandler(cfg, wikidURL, daemonToken, proxy), nil
}

func frontdMCPBearerAuthHandler(cfg leafwikiRuntimeConfig, wikidURL string, daemonToken string, next http.Handler) http.Handler {
	if cfg.DisableAuth {
		return next
	}
	return http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		authenticated := sdkauth.RequireBearerToken(wikidMCPTokenVerifier(wikidURL, daemonToken), &sdkauth.RequireBearerTokenOptions{
			ResourceMetadataURL: wikioauth.ProtectedResourceMetadataURL(req, cfg.BasePath),
			Scopes:              []string{wikioauth.ScopeMCP},
		})(next)
		authenticated.ServeHTTP(rw, req)
	})
}

func wikidActorResolver(wikidURL string, daemonToken string) func(*http.Request) (projectdaemon.ActorContext, error) {
	return func(req *http.Request) (projectdaemon.ActorContext, error) {
		out := struct {
			Actor projectdaemon.ActorContext `json:"actor"`
		}{}
		if err := callWikidPrivateEndpoint(req.Context(), wikidURL, daemonToken, "/__leafwiki/actor-context", req, &out); err != nil {
			return projectdaemon.ActorContext{}, err
		}
		return out.Actor, nil
	}
}

func wikidMCPTokenVerifier(wikidURL string, daemonToken string) sdkauth.TokenVerifier {
	return func(ctx context.Context, token string, req *http.Request) (*sdkauth.TokenInfo, error) {
		verifyReq := req
		if verifyReq == nil {
			verifyReq = &http.Request{Header: http.Header{}}
		}
		clone := verifyReq.Clone(ctx)
		clone.Header = verifyReq.Header.Clone()
		clone.Header.Set("Authorization", "Bearer "+strings.TrimSpace(token))
		out := struct {
			UserID     string    `json:"userId"`
			Scopes     []string  `json:"scopes"`
			Expiration time.Time `json:"expiration"`
		}{}
		if err := callWikidPrivateEndpoint(ctx, wikidURL, daemonToken, "/__leafwiki/token/verify", clone, &out); err != nil {
			return nil, fmt.Errorf("%w: %v", sdkauth.ErrInvalidToken, err)
		}
		return &sdkauth.TokenInfo{UserID: out.UserID, Scopes: out.Scopes, Expiration: out.Expiration}, nil
	}
}

func callWikidPrivateEndpoint(ctx context.Context, wikidURL string, daemonToken string, path string, source *http.Request, out any) error {
	endpoint := strings.TrimRight(wikidURL, "/") + path
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, nil)
	if err != nil {
		return err
	}
	if source != nil {
		req.Header = source.Header.Clone()
		req.Header.Set("X-LeafWiki-Original-Method", source.Method)
		req.Header.Set("X-LeafWiki-Original-Path", source.URL.Path)
		req.Header.Set("X-LeafWiki-Original-Remote-Addr", source.RemoteAddr)
	}
	req.Header.Set(projectdaemon.ControlTokenHeader, daemonToken)
	req.Header.Del(projectdaemon.ActorContextHeader)
	resp, err := (&http.Client{Timeout: 5 * time.Second}).Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		msg := strings.TrimSpace(string(raw))
		if msg == "" {
			msg = resp.Status
		}
		return fmt.Errorf("wikid private endpoint %s failed: %s", path, msg)
	}
	if out == nil {
		_, _ = io.Copy(io.Discard, resp.Body)
		return nil
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

func frontdMCPTokenVerifier(w *wiki.Wiki) sdkauth.TokenVerifier {
	return func(ctx context.Context, token string, req *http.Request) (*sdkauth.TokenInfo, error) {
		if coreauth.IsAPIKeyBearer(token) {
			if w.APIKeyService() == nil {
				return nil, fmt.Errorf("%w: api key verifier unavailable", sdkauth.ErrInvalidToken)
			}
			verified, err := w.APIKeyService().VerifyAPIKey(token)
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
		if w.OAuthService() == nil {
			return nil, fmt.Errorf("%w: oauth verifier unavailable", sdkauth.ErrInvalidToken)
		}
		return w.OAuthService().VerifyBearerToken(ctx, token, req)
	}
}

func frontdMCPActorResolver(w *wiki.Wiki, cfg leafwikiRuntimeConfig) func(*http.Request) (projectdaemon.ActorContext, error) {
	if cfg.DisableAuth {
		return frontdActorResolver(w, cfg)
	}
	return func(req *http.Request) (projectdaemon.ActorContext, error) {
		tokenInfo := sdkauth.TokenInfoFromContext(req.Context())
		if tokenInfo == nil || strings.TrimSpace(tokenInfo.UserID) == "" {
			return projectdaemon.ActorContext{}, fmt.Errorf("authenticated MCP token info missing")
		}
		if w.UserService() == nil {
			return projectdaemon.ActorContext{}, fmt.Errorf("authenticated MCP user service is unavailable")
		}
		user, err := w.UserService().GetUserByID(tokenInfo.UserID)
		if err != nil {
			return projectdaemon.ActorContext{}, err
		}
		method := "oauth"
		if coreauth.IsAPIKeyBearer(httpBearerToken(req)) {
			method = "api_key"
		}
		return actorContextForUser(user, method, cfg)
	}
}

func frontdActorUser(req *http.Request, w *wiki.Wiki, cfg leafwikiRuntimeConfig) (*coreauth.User, string, error) {
	if cfg.DisableAuth {
		return &coreauth.User{ID: "public-editor", Username: "public-editor", Role: coreauth.RoleEditor}, "disabled", nil
	}
	if user, method, ok, err := frontdRemoteUser(req, w, cfg); ok || err != nil {
		return user, method, err
	}
	if token := httpBearerToken(req); token != "" && coreauth.IsAPIKeyBearer(token) && isMCPActorPath(req.URL.Path) {
		verified, err := w.APIKeyService().VerifyAPIKey(token)
		if err != nil {
			return nil, "", err
		}
		return verified.User, "api_key", nil
	}
	if token := httpBearerToken(req); token != "" && isMCPActorPath(req.URL.Path) {
		if w.OAuthService() == nil || w.UserService() == nil {
			return nil, "", fmt.Errorf("oauth actor services are unavailable")
		}
		info, err := w.OAuthService().VerifyBearerToken(req.Context(), token, req)
		if err != nil {
			return nil, "", err
		}
		user, err := w.UserService().GetUserByID(info.UserID)
		if err != nil {
			return nil, "", err
		}
		return user, "oauth", nil
	}
	if token := accessTokenFromHTTPRequest(req); token != "" {
		user, err := w.AuthService().ValidateToken(token)
		if err != nil {
			return nil, "", err
		}
		return user, "cookie", nil
	}
	if cfg.PublicAccess && req != nil && req.Method == http.MethodGet {
		return &coreauth.User{ID: "public-viewer", Username: "public-viewer", Role: coreauth.RoleViewer}, "public_access", nil
	}
	return nil, "", fmt.Errorf("authenticated workspace request is missing credentials")
}

func actorContextForUser(user *coreauth.User, method string, cfg leafwikiRuntimeConfig) (projectdaemon.ActorContext, error) {
	if user == nil {
		return projectdaemon.ActorContext{}, fmt.Errorf("actor user is required")
	}
	now := time.Now().UTC()
	return projectdaemon.ActorContext{
		Version:     1,
		Issuer:      projectdaemon.ActorContextIssuerWikid,
		Subject:     "user:" + user.ID,
		Username:    user.Username,
		Email:       user.Email,
		Role:        user.Role,
		Scopes:      []string{"leafwiki:workspace:read", "leafwiki:workspace:write", "leafwiki:mcp"},
		WorkspaceID: runtimeWorkspaceID(cfg.Workspace),
		AuthMethod:  method,
		IssuedAt:    now,
		ExpiresAt:   now.Add(5 * time.Minute),
	}, nil
}

func actorContextForWorkspaceGrant(user *coreauth.User, method string, cfg leafwikiRuntimeConfig, workspaceID string, role wikid.GrantRole) (projectdaemon.ActorContext, error) {
	actor, err := actorContextForUser(user, method, cfg)
	if err != nil {
		return projectdaemon.ActorContext{}, err
	}
	actor.WorkspaceID = strings.TrimSpace(workspaceID)
	actor.Role = string(role)
	actor.Scopes = scopesForGrantRole(role)
	return actor, nil
}

func scopesForGrantRole(role wikid.GrantRole) []string {
	caps, err := wikid.CapabilitiesForRole(role)
	if err != nil {
		return nil
	}
	var scopes []string
	if caps.ReadContent {
		scopes = append(scopes, "leafwiki:workspace:read", "leafwiki:mcp")
	}
	if caps.WriteContent {
		scopes = append(scopes, "leafwiki:workspace:write")
	}
	if caps.AdministerGrants {
		scopes = append(scopes, "leafwiki:workspace:admin")
	}
	return scopes
}

func frontdRemoteUser(req *http.Request, w *wiki.Wiki, cfg leafwikiRuntimeConfig) (*coreauth.User, string, bool, error) {
	if req == nil || !cfg.EnableHTTPRemoteUser {
		return nil, "", false, nil
	}
	trustedProxies, err := authmw.ParseTrustedProxies(cfg.TrustedProxyIPsRaw)
	if err != nil {
		return nil, "", false, err
	}
	if trustedProxies == nil || !trustedProxies.IsTrusted(req.RemoteAddr) {
		return nil, "", false, nil
	}
	headerName := strings.TrimSpace(cfg.HTTPRemoteUserHeader)
	if headerName == "" {
		headerName = "Remote-User"
	}
	username := strings.TrimSpace(req.Header.Get(headerName))
	if username == "" {
		return nil, "", false, nil
	}
	if w.UserService() == nil {
		return nil, "", true, fmt.Errorf("remote user service is unavailable")
	}
	user, err := w.UserService().GetUserByUsername(username)
	if err != nil {
		return nil, "", true, err
	}
	return user, "remote_user", true, nil
}

func httpBearerToken(req *http.Request) string {
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

func accessTokenFromHTTPRequest(req *http.Request) string {
	if req == nil {
		return ""
	}
	for _, name := range []string{"leafwiki_at", "__Host-leafwiki_at"} {
		cookie, err := req.Cookie(name)
		if err == nil && strings.TrimSpace(cookie.Value) != "" {
			return strings.TrimSpace(cookie.Value)
		}
	}
	return ""
}

func runtimeWorkspaceID(workspace wiki.Workspace) string {
	if strings.TrimSpace(workspace.ID) != "" {
		return strings.TrimSpace(workspace.ID)
	}
	return "current"
}

func originalRemoteAddr(req *http.Request) string {
	if value := strings.TrimSpace(req.Header.Get("X-LeafWiki-Original-Remote-Addr")); value != "" {
		return value
	}
	return req.RemoteAddr
}

func originalMethod(req *http.Request) string {
	if value := strings.TrimSpace(req.Header.Get("X-LeafWiki-Original-Method")); value != "" {
		return value
	}
	return req.Method
}

func originalPath(req *http.Request) string {
	if value := strings.TrimSpace(req.Header.Get("X-LeafWiki-Original-Path")); value != "" {
		return value
	}
	if req.URL != nil && req.URL.Path != "" {
		return req.URL.Path
	}
	return "/"
}

func cloneWithOriginalRequest(req *http.Request) *http.Request {
	if req == nil {
		return nil
	}
	clone := req.Clone(req.Context())
	clone.Body = req.Body
	clone.Method = originalMethod(req)
	clone.URL.Path = originalPath(req)
	clone.URL.RawPath = ""
	clone.RemoteAddr = originalRemoteAddr(req)
	clone.Header = req.Header.Clone()
	clone.Header.Del(projectdaemon.ControlTokenHeader)
	clone.Header.Del(projectdaemon.ActorContextHeader)
	return clone
}

func isMCPActorPath(path string) bool {
	return path == "/mcp" || strings.HasPrefix(path, "/mcp/")
}

func handleWikidActorContext(w http.ResponseWriter, req *http.Request, identity *wiki.Wiki, cfg leafwikiRuntimeConfig, registry *wikid.RegistryService, grants *wikid.GrantStore) {
	clone := cloneWithOriginalRequest(req)
	clone.Body = nil
	user, method, err := frontdActorUser(clone, identity, cfg)
	if err != nil {
		http.Error(w, "resolve actor context", http.StatusUnauthorized)
		return
	}
	workspaceID := strings.TrimSpace(req.Header.Get(projectdaemon.WorkspaceIDHeader))
	if workspaceID == "" {
		workspaceID = runtimeWorkspaceID(cfg.Workspace)
	}
	if registry != nil {
		if _, ok, err := registry.Workspace(workspaceID); err != nil {
			http.Error(w, "load workspace", http.StatusInternalServerError)
			return
		} else if !ok {
			http.Error(w, "workspace not found", http.StatusNotFound)
			return
		}
	}
	userRole := wikidGrantRoleForCoreRole(user.Role)
	role := userRole
	if grants != nil {
		if err := ensureRuntimeHomeGrant(grants, user); err != nil {
			http.Error(w, "seed home workspace grant", http.StatusInternalServerError)
			return
		}
		if userRole == wikid.GrantRoleAdmin {
			role = wikid.GrantRoleAdmin
		} else {
			subject := "user:" + user.ID
			userGrants, err := grants.GrantsForSubject(subject)
			if err != nil {
				http.Error(w, "load workspace grants", http.StatusInternalServerError)
				return
			}
			role = ""
			for _, grant := range userGrants {
				if grant.WorkspaceID == workspaceID {
					role = effectiveWorkspaceGrantRole(userRole, grant.Role)
					break
				}
			}
			if role == "" {
				http.Error(w, "workspace access denied", http.StatusForbidden)
				return
			}
		}
	}
	actor, err := actorContextForWorkspaceGrant(user, method, cfg, workspaceID, role)
	if err != nil {
		http.Error(w, "encode actor context", http.StatusInternalServerError)
		return
	}
	writeRuntimeJSON(w, map[string]any{"actor": actor})
}

func handleWikidTokenVerify(w http.ResponseWriter, req *http.Request, identity *wiki.Wiki) {
	token := httpBearerToken(req)
	if token == "" {
		http.Error(w, "missing bearer token", http.StatusUnauthorized)
		return
	}
	info, err := frontdMCPTokenVerifier(identity)(req.Context(), token, req)
	if err != nil {
		http.Error(w, "invalid bearer token", http.StatusUnauthorized)
		return
	}
	writeRuntimeJSON(w, map[string]any{
		"userId":     info.UserID,
		"scopes":     info.Scopes,
		"expiration": info.Expiration,
	})
}

func writeRuntimeJSON(w http.ResponseWriter, value any) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(value); err != nil {
		http.Error(w, "encode response", http.StatusInternalServerError)
	}
}

func runWikidFrontdOwner(parent context.Context, cfg leafwikiRuntimeConfig, ownerCfg projectdaemon.Config) error {
	controlListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return fmt.Errorf("start control listener: %w", err)
	}
	defer controlListener.Close()

	controlToken, err := projectdaemon.RandomToken()
	if err != nil {
		return err
	}
	hash, err := projectdaemon.ConfigHash(ownerCfg)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithCancel(parent)
	defer cancel()
	controlPlaneWiki, err := newRuntimeWiki(cfg, ownerCfg, runtimeWikiControlPlaneOnly)
	if err != nil {
		return err
	}
	defer controlPlaneWiki.Close()
	controlPlaneOpts, err := controlPlaneRouterOptionsForRuntime(cfg, controlPlaneWiki)
	if err != nil {
		return err
	}
	controlPlaneRouter := frontd.NewRouter(controlPlaneWiki, controlPlaneOpts)
	wikidURL := "http://" + controlListener.Addr().String()
	runtime, err := startWikidFrontdRuntime(ctx, cfg, controlToken, wikidURL)
	if err != nil {
		return err
	}
	defer func() {
		stopCtx, stopCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer stopCancel()
		if err := runtime.stop(stopCtx); err != nil {
			slog.Default().Warn("Runtime role shutdown failed", "error", err)
		}
	}()
	controlPlaneWiki.SetRuntimeRoleHealth(requiredRuntimeRoleHealth(), runtime.roleHealthSnapshot)
	runtime.mu.Lock()
	workspacedURL := runtime.workspacedURL
	workspacedPID := 0
	if role, ok := findRuntimeRoleHealth(runtime.roles, projectdaemon.RoleWorkspaced); ok {
		workspacedPID = role.PID
	}
	runtime.mu.Unlock()
	privateMCP, err := frontd.NewMCPProxyWithActor(frontd.WorkspaceProxyOptions{
		Upstream:    workspacedURL,
		DaemonToken: controlToken,
		Actor:       wikidControlMCPActorResolver(authStorageDirForRuntime(ownerCfg.DataDir, cfg.RuntimeStack), cfg),
	})
	if err != nil {
		return err
	}

	var sessions *projectdaemon.SessionRegistry
	var agentPresence *projectdaemon.AgentPresenceRegistry
	activityChanged := func(int) {}
	if !cfg.DisableIdleShutdown {
		activityChanged = idleShutdownCallback(ctx, cancel, cfg.DaemonIdleTimeout, func() int {
			return projectDaemonActivityCount(sessions, agentPresence)
		})
	}
	notifyActivityChanged := func(int) {
		activityChanged(projectDaemonActivityCount(sessions, agentPresence))
	}
	sessions = projectdaemon.NewSessionRegistry(projectdaemon.DefaultHeartbeatTTL, notifyActivityChanged)
	agentPresence = projectdaemon.NewAgentPresenceRegistry(cfg.DaemonIdleTimeout, notifyActivityChanged)
	go sessions.RunExpiryLoop(ctx, 0)
	go agentPresence.RunExpiryLoop(ctx, 0)

	layout := wikid.GlobalLayout(ownerCfg.DataDir)
	registry := wikid.NewRegistryService(wikid.NewRegistryStore(layout.DBPath), layout)
	if _, err := registry.BootstrapHomeWorkspace(ownerCfg.DataDir, ownerCfg.RootDir); err != nil {
		return fmt.Errorf("bootstrap home workspace: %w", err)
	}
	grants := wikid.NewGrantStore(layout.DBPath)
	if err := seedRuntimeHomeGrants(grants, cfg); err != nil {
		return err
	}
	workspaceSupervisor := wikid.NewWorkspaceSupervisor(wikid.WorkspaceSupervisorOptions{})
	workspaceSupervisor.MarkReady(wikid.HomeWorkspaceID, workspacedPID, workspacedURL)
	workspaceManager := newFederatedWorkspaceManager(cfg, controlToken, wikidURL, layout, workspaceSupervisor)
	workspaceManager.MarkReady(wikid.HomeWorkspaceID, workspacedPID, workspacedURL)
	defer func() {
		stopCtx, stopCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer stopCancel()
		if err := workspaceManager.stop(stopCtx); err != nil {
			slog.Default().Warn("Workspace runtime shutdown failed", "error", err)
		}
	}()
	workspaceAPI := wikid.NewPrivateWorkspaceAPI(wikid.PrivateWorkspaceAPIOptions{
		Registry:   registry,
		Grants:     grants,
		Supervisor: workspaceSupervisor,
		Subject: func(req *http.Request) (wikid.WorkspaceSubject, error) {
			user, _, err := frontdActorUser(cloneWithOriginalRequest(req), controlPlaneWiki, cfg)
			if err != nil {
				return wikid.WorkspaceSubject{}, err
			}
			if err := ensureRuntimeHomeGrant(grants, user); err != nil {
				return wikid.WorkspaceSubject{}, err
			}
			return wikid.WorkspaceSubject{
				Subject: "user:" + user.ID,
				Role:    wikidGrantRoleForCoreRole(user.Role),
			}, nil
		},
		Ensure: func(ctx context.Context, workspace wikid.WorkspaceRecord) (wikid.WorkspaceStatus, error) {
			return workspaceManager.Ensure(ctx, workspace)
		},
	})

	controlHandler := projectdaemon.NewControlServer(projectdaemon.ControlServerOptions{
		Token:         controlToken,
		Sessions:      sessions,
		AgentPresence: agentPresence,
		PrivateMCP:    privateMCP,
		AuthDisabled:  cfg.DisableAuth,
		Health: projectdaemon.DaemonHealth{
			SchemaVersion: projectdaemon.DescriptorSchemaVersion,
			PID:           os.Getpid(),
			DataDir:       ownerCfg.DataDir,
			RootDir:       ownerCfg.RootDir,
			ConfigHash:    hash,
		},
		VerifyAPIKey: func(key string) error {
			err := verifyStdioAPIKeyFromStorage(authStorageDirForRuntime(ownerCfg.DataDir, cfg.RuntimeStack), key)
			if errors.Is(err, coreauth.ErrInvalidToken) {
				return projectdaemon.ErrInvalidAPIKey
			}
			return err
		},
	})
	controlServer := &http.Server{Addr: controlListener.Addr().String(), Handler: wikid.NewPrivateHandler(wikid.PrivateHandlerOptions{
		DaemonToken:  controlToken,
		BasePath:     cfg.BasePath,
		Control:      controlHandler,
		ControlPlane: controlPlaneRouter,
		ActorContext: http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			handleWikidActorContext(w, req, controlPlaneWiki, cfg, registry, grants)
		}),
		TokenVerify: http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			handleWikidTokenVerify(w, req, controlPlaneWiki)
		}),
		WorkspaceAPI: workspaceAPI,
	})}
	serverDone := make(chan error, 1)
	go func() {
		err := controlServer.Serve(controlListener)
		if errors.Is(err, http.ErrServerClosed) {
			err = nil
		}
		serverDone <- err
	}()

	desc := &projectdaemon.Descriptor{
		SchemaVersion:    projectdaemon.DescriptorSchemaVersion,
		RuntimeStack:     cfg.RuntimeStack,
		Role:             projectDaemonDescriptorRole(cfg.RuntimeStack),
		WorkspaceID:      runtimeWorkspaceID(cfg.Workspace),
		PID:              os.Getpid(),
		StartedAt:        time.Now().UTC(),
		DataDir:          ownerCfg.DataDir,
		RootDir:          ownerCfg.RootDir,
		PublicURL:        roleURL(runtime.roles, projectdaemon.RoleFrontd),
		PublicMCPEnabled: cfg.MCPTransports.HTTP,
		BasePath:         cfg.BasePath,
		ControlURL:       "http://" + controlListener.Addr().String(),
		PrivateMCPURL:    strings.TrimRight(workspacedURL, "/") + "/mcp",
		PrivateMCPToken:  controlToken,
		ConfigHash:       hash,
		IdleTimeout:      cfg.DaemonIdleTimeout.String(),
		ControlToken:     controlToken,
		Config:           ownerCfg,
		Roles:            append([]projectdaemon.RoleHealth(nil), runtime.roles...),
	}
	if desc.PublicURL == "" {
		desc.PublicURL = nativeStdioHTTPURL(cfg.Host, cfg.Port, cfg.BasePath)
	}
	descriptorPath := projectdaemon.DescriptorPath(ownerCfg.DataDir)
	if err := projectdaemon.WriteDescriptorAtomic(descriptorPath, desc); err != nil {
		return err
	}
	globalDescriptorPath := projectdaemon.GlobalDescriptorPath(layout.RuntimeDir, projectdaemon.RoleWikid)
	if err := projectdaemon.WriteDescriptorAtomic(globalDescriptorPath, desc); err != nil {
		return err
	}
	var descriptorMu sync.Mutex
	runtime.setRoleChangeCallback(func(roles []projectdaemon.RoleHealth) {
		descriptorMu.Lock()
		defer descriptorMu.Unlock()
		desc.Roles = append([]projectdaemon.RoleHealth(nil), roles...)
		syncHomeWorkspaceStatus(workspaceSupervisor, roles)
		if publicURL := roleURL(roles, projectdaemon.RoleFrontd); publicURL != "" {
			desc.PublicURL = publicURL
		}
		if err := projectdaemon.WriteDescriptorAtomic(descriptorPath, desc); err != nil {
			slog.Default().Warn("Runtime role descriptor update failed", "error", err)
		}
		if err := projectdaemon.WriteDescriptorAtomic(globalDescriptorPath, desc); err != nil {
			slog.Default().Warn("Runtime role global descriptor update failed", "error", err)
		}
	})
	defer projectdaemon.RemoveDescriptor(descriptorPath)
	defer projectdaemon.RemoveDescriptor(globalDescriptorPath)
	if !cfg.DisableIdleShutdown {
		go cancelIfNoActivityAfterStartupGrace(ctx, cancel, sessions, agentPresence, projectdaemon.DefaultHeartbeatTTL)
	}

	slog.Default().Info("Starting LeafWiki", "address", buildListenAddress(cfg.Host, cfg.Port), "data_dir", ownerCfg.DataDir)
	select {
	case <-ctx.Done():
	case err := <-serverDone:
		if err != nil {
			return err
		}
	}
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutdownCancel()
	return controlServer.Shutdown(shutdownCtx)
}

func roleURL(roles []projectdaemon.RoleHealth, name projectdaemon.RoleName) string {
	for _, role := range roles {
		if role.Name == name {
			return role.URL
		}
	}
	return ""
}

func findRuntimeRoleHealth(roles []projectdaemon.RoleHealth, name projectdaemon.RoleName) (projectdaemon.RoleHealth, bool) {
	for _, role := range roles {
		if role.Name == name {
			return role, true
		}
	}
	return projectdaemon.RoleHealth{}, false
}

func syncHomeWorkspaceStatus(supervisor *wikid.WorkspaceSupervisor, roles []projectdaemon.RoleHealth) {
	if supervisor == nil {
		return
	}
	role, ok := findRuntimeRoleHealth(roles, projectdaemon.RoleWorkspaced)
	if !ok {
		return
	}
	status := wikid.WorkspaceStatus{
		WorkspaceID: wikid.HomeWorkspaceID,
		PID:         role.PID,
		URL:         strings.TrimSpace(role.URL),
		Error:       role.Error,
		UpdatedAt:   role.UpdatedAt,
	}
	switch role.State {
	case projectdaemon.RoleStateReady:
		status.State = wikid.WorkspaceStateRunning
	case projectdaemon.RoleStateStarting:
		status.State = wikid.WorkspaceStateStarting
	case projectdaemon.RoleStateRestarting:
		status.State = wikid.WorkspaceStateRestarting
	case projectdaemon.RoleStateCrashed, projectdaemon.RoleStateStopped:
		status.State = wikid.WorkspaceStateCrashed
	default:
		status.State = wikid.WorkspaceStateRegistered
	}
	supervisor.MarkStatus(status)
}

func seedRuntimeHomeGrants(store *wikid.GrantStore, cfg leafwikiRuntimeConfig) error {
	if cfg.DisableAuth {
		if err := store.Upsert(wikid.Grant{Subject: "user:public-editor", WorkspaceID: wikid.HomeWorkspaceID, Role: wikid.GrantRoleEditor}); err != nil {
			return fmt.Errorf("seed disabled-auth home grant: %w", err)
		}
	}
	if cfg.PublicAccess {
		if err := store.Upsert(wikid.Grant{Subject: "user:public-viewer", WorkspaceID: wikid.HomeWorkspaceID, Role: wikid.GrantRoleViewer}); err != nil {
			return fmt.Errorf("seed public home grant: %w", err)
		}
	}
	return nil
}

func ensureRuntimeHomeGrant(store *wikid.GrantStore, user *coreauth.User) error {
	if user == nil {
		return fmt.Errorf("user is required")
	}
	role := wikidGrantRoleForCoreRole(user.Role)
	if role == "" {
		return nil
	}
	return store.Upsert(wikid.Grant{Subject: "user:" + user.ID, WorkspaceID: wikid.HomeWorkspaceID, Role: role})
}

func wikidGrantRoleForCoreRole(role string) wikid.GrantRole {
	switch role {
	case coreauth.RoleViewer:
		return wikid.GrantRoleViewer
	case coreauth.RoleEditor:
		return wikid.GrantRoleEditor
	case coreauth.RoleAdmin:
		return wikid.GrantRoleAdmin
	default:
		return ""
	}
}

func effectiveWorkspaceGrantRole(userRole wikid.GrantRole, grantRole wikid.GrantRole) wikid.GrantRole {
	if wikidGrantRoleRank(userRole) < wikidGrantRoleRank(grantRole) {
		return userRole
	}
	return grantRole
}

func wikidGrantRoleRank(role wikid.GrantRole) int {
	switch role {
	case wikid.GrantRoleViewer:
		return 1
	case wikid.GrantRoleEditor:
		return 2
	case wikid.GrantRoleAdmin:
		return 3
	default:
		return 0
	}
}

func runProjectDaemonOwner(parent context.Context, cfg leafwikiRuntimeConfig) error {
	ownerCfg, err := daemonConfigForRuntime(cfg)
	if err != nil {
		return err
	}
	dataDirMissingBeforeLock := false
	if _, err := os.Stat(ownerCfg.DataDir); os.IsNotExist(err) {
		dataDirMissingBeforeLock = true
	}
	dataLock, err := locking.AcquireDataDirLock(ownerCfg.DataDir)
	if err != nil {
		return fmt.Errorf("acquire data directory lock: %w", err)
	}
	defer dataLock.Release()

	logCloser, err := setupLogger(cfg.Logging, os.Stdout, os.Stderr)
	if err != nil {
		return fmt.Errorf("invalid logging configuration: %w", err)
	}
	defer logCloser.Close()

	if cfg.DisableAuth {
		slog.Default().Warn("Authentication disabled. Wiki is publicly accessible without authentication.")
	}
	if cfg.AllowInsecure {
		slog.Default().Warn("allow-insecure enabled. Auth cookies may be transmitted over plain HTTP (INSECURE).")
	}
	if cfg.EnableHTTPRemoteUser {
		slog.Default().Info("Reverse-proxy authentication enabled",
			"header", cfg.HTTPRemoteUserHeader,
			"trusted_proxies", cfg.TrustedProxyIPsRaw,
		)
	}
	if dataDirMissingBeforeLock {
		slog.Default().Info("Data directory created", "path", cfg.Workspace.DataDir)
	}
	if _, err := os.Stat(ownerCfg.DataDir); os.IsNotExist(err) {
		if err := os.MkdirAll(ownerCfg.DataDir, 0o755); err != nil {
			return fmt.Errorf("create data directory: %w", err)
		}
	}
	if _, err := os.Stat(ownerCfg.RootDir); os.IsNotExist(err) {
		if err := os.MkdirAll(ownerCfg.RootDir, 0o755); err != nil {
			return fmt.Errorf("create root directory: %w", err)
		}
	}
	rootLock, err := locking.AcquireRootDirLock(ownerCfg.RootDir)
	if err != nil {
		return fmt.Errorf("acquire root directory lock: %w", err)
	}
	defer rootLock.Release()

	authStorageDir := ""
	if cfg.RuntimeStack == projectdaemon.RuntimeStackWikidFrontd {
		if err := wikid.CleanupLegacyAuthDBs(ownerCfg.DataDir); err != nil {
			return fmt.Errorf("cleanup legacy auth DBs: %w", err)
		}
		authPaths := wikid.AuthStoragePaths(ownerCfg.DataDir)
		if err := os.MkdirAll(authPaths.AuthDir, 0o755); err != nil {
			return fmt.Errorf("create wikid auth dir: %w", err)
		}
		if err := os.MkdirAll(authPaths.OAuthDir, 0o755); err != nil {
			return fmt.Errorf("create wikid oauth dir: %w", err)
		}
		authStorageDir = authPaths.AuthDir
	}
	if cfg.RuntimeStack == projectdaemon.RuntimeStackWikidFrontd {
		return runWikidFrontdOwner(parent, cfg, ownerCfg)
	}
	w, err := wiki.NewWiki(&wiki.WikiOptions{
		Workspace:              wiki.Workspace{ID: cfg.Workspace.ID, DataDir: ownerCfg.DataDir, RootDir: ownerCfg.RootDir},
		StorageDir:             ownerCfg.DataDir,
		AuthStorageDir:         authStorageDir,
		AdminPassword:          cfg.AdminPassword,
		JWTSecret:              cfg.JWTSecret,
		AccessTokenTimeout:     cfg.AccessTokenTimeout,
		RefreshTokenTimeout:    cfg.RefreshTokenTimeout,
		AuthDisabled:           cfg.DisableAuth,
		EnableRevision:         cfg.EnableRevision,
		EnableWorkspaceSync:    cfg.EnableWorkspaceSync,
		MaxRevisionHistory:     cfg.MaxRevisionHistory,
		MarkdownLinkRootPrefix: cfg.MarkdownLinkRootPrefix,
	})
	if err != nil {
		return fmt.Errorf("initialize Wiki: %w", err)
	}
	defer w.Close()

	trustedProxies, err := authmw.ParseTrustedProxies(cfg.TrustedProxyIPsRaw)
	if err != nil {
		return fmt.Errorf("invalid trusted proxies: %w", err)
	}
	publicRouterOpts := buildHTTPRouterOptions(httpRouterOptionsInput{
		publicAccess:            cfg.PublicAccess,
		injectCodeInHeader:      cfg.InjectCodeInHeader,
		customStylesheet:        cfg.CustomStylesheet,
		allowInsecure:           cfg.AllowInsecure,
		hideLinkMetadataSection: cfg.HideLinkMetadataSection,
		accessTokenTimeout:      cfg.AccessTokenTimeout,
		refreshTokenTimeout:     cfg.RefreshTokenTimeout,
		authDisabled:            cfg.DisableAuth,
		basePath:                cfg.BasePath,
		markdownLinkRootPrefix:  cfg.MarkdownLinkRootPrefix,
		maxAssetUploadSize:      cfg.MaxAssetUploadSize,
		enableRevision:          cfg.EnableRevision,
		enableWorkspaceSync:     cfg.EnableWorkspaceSync,
		enableLinkRefactor:      cfg.EnableLinkRefactor,
		enableMCP:               cfg.MCPTransports.HTTP,
		host:                    cfg.Host,
		httpRemoteUser: httpinternal.HTTPRemoteUserConfig{
			Enabled:        cfg.EnableHTTPRemoteUser,
			HeaderName:     cfg.HTTPRemoteUserHeader,
			TrustedProxies: trustedProxies,
			UserService:    w.UserService(),
			LogoutURL:      cfg.HTTPRemoteUserLogoutURL,
		},
		disableRequestLog: cfg.DisableRequestLog,
	})
	publicRouter := httpinternal.NewRouter(w.Registrars(), w.FrontendConfig(), publicRouterOpts)
	privateRouterOpts := publicRouterOpts
	privateRouterOpts.MCPEnabled = true
	privateMCP := w.PrivateMCPHTTPHandler(privateRouterOpts)

	publicListener, err := net.Listen("tcp", buildListenAddress(cfg.Host, cfg.Port))
	if err != nil {
		return fmt.Errorf("start HTTP listener: %w", err)
	}
	controlListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		_ = publicListener.Close()
		return fmt.Errorf("start control listener: %w", err)
	}
	defer publicListener.Close()
	defer controlListener.Close()

	controlToken, err := projectdaemon.RandomToken()
	if err != nil {
		_ = publicListener.Close()
		_ = controlListener.Close()
		return err
	}
	hash, err := projectdaemon.ConfigHash(ownerCfg)
	if err != nil {
		_ = publicListener.Close()
		_ = controlListener.Close()
		return err
	}
	ctx, cancel := context.WithCancel(parent)
	defer cancel()
	var roleShells *wikidFrontdRuntime
	defer func() {
		stopCtx, stopCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer stopCancel()
		if err := roleShells.stop(stopCtx); err != nil {
			slog.Default().Warn("Runtime role shell shutdown failed", "error", err)
		}
	}()
	var sessions *projectdaemon.SessionRegistry
	var agentPresence *projectdaemon.AgentPresenceRegistry
	activityChanged := idleShutdownCallback(ctx, cancel, cfg.DaemonIdleTimeout, func() int {
		return projectDaemonActivityCount(sessions, agentPresence)
	})
	notifyActivityChanged := func(int) {
		activityChanged(projectDaemonActivityCount(sessions, agentPresence))
	}
	sessions = projectdaemon.NewSessionRegistry(projectdaemon.DefaultHeartbeatTTL, notifyActivityChanged)
	agentPresence = projectdaemon.NewAgentPresenceRegistry(cfg.DaemonIdleTimeout, notifyActivityChanged)
	w.SetAgentPresenceRegistry(agentPresence)
	go sessions.RunExpiryLoop(ctx, 0)
	go agentPresence.RunExpiryLoop(ctx, 0)

	publicServer := &http.Server{Addr: buildListenAddress(cfg.Host, cfg.Port), Handler: publicRouter}
	controlServer := &http.Server{Addr: controlListener.Addr().String(), Handler: projectdaemon.NewControlServer(projectdaemon.ControlServerOptions{
		Token:         controlToken,
		Sessions:      sessions,
		AgentPresence: agentPresence,
		PrivateMCP:    privateMCP,
		AuthDisabled:  cfg.DisableAuth,
		Health: projectdaemon.DaemonHealth{
			SchemaVersion: projectdaemon.DescriptorSchemaVersion,
			PID:           os.Getpid(),
			DataDir:       ownerCfg.DataDir,
			RootDir:       ownerCfg.RootDir,
			ConfigHash:    hash,
		},
		VerifyAPIKey: func(key string) error {
			_, err := w.APIKeyService().VerifyAPIKey(key)
			if errors.Is(err, coreauth.ErrInvalidToken) {
				return projectdaemon.ErrInvalidAPIKey
			}
			return err
		},
	})}
	serverDone := make(chan error, 2)
	go func() {
		err := controlServer.Serve(controlListener)
		if errors.Is(err, http.ErrServerClosed) {
			err = nil
		}
		serverDone <- err
	}()

	desc := &projectdaemon.Descriptor{
		SchemaVersion:    projectdaemon.DescriptorSchemaVersion,
		RuntimeStack:     cfg.RuntimeStack,
		Role:             projectDaemonDescriptorRole(cfg.RuntimeStack),
		PID:              os.Getpid(),
		StartedAt:        time.Now().UTC(),
		DataDir:          ownerCfg.DataDir,
		RootDir:          ownerCfg.RootDir,
		PublicURL:        nativeStdioHTTPURL(cfg.Host, cfg.Port, cfg.BasePath),
		PublicMCPEnabled: cfg.MCPTransports.HTTP,
		BasePath:         cfg.BasePath,
		ControlURL:       "http://" + controlListener.Addr().String(),
		ConfigHash:       hash,
		IdleTimeout:      cfg.DaemonIdleTimeout.String(),
		ControlToken:     controlToken,
		Config:           ownerCfg,
		Roles:            projectDaemonDescriptorRoles(cfg.RuntimeStack, os.Getpid(), publicListener.Addr().String(), roleShells),
	}
	descriptorPath := projectdaemon.DescriptorPath(ownerCfg.DataDir)
	if err := projectdaemon.WriteDescriptorAtomic(descriptorPath, desc); err != nil {
		return err
	}
	defer projectdaemon.RemoveDescriptor(descriptorPath)
	go func() {
		if err := waitForFirstProjectDaemonActivity(ctx, sessions, agentPresence, 25*time.Millisecond); err != nil {
			return
		}
		err := publicServer.Serve(publicListener)
		if errors.Is(err, http.ErrServerClosed) || errors.Is(err, net.ErrClosed) {
			err = nil
		}
		serverDone <- err
	}()
	go cancelIfNoActivityAfterStartupGrace(ctx, cancel, sessions, agentPresence, projectdaemon.DefaultHeartbeatTTL)

	slog.Default().Info("Starting LeafWiki", "address", buildListenAddress(cfg.Host, cfg.Port), "data_dir", ownerCfg.DataDir)
	select {
	case <-ctx.Done():
	case err := <-serverDone:
		if err != nil {
			return err
		}
	}
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutdownCancel()
	err = errors.Join(controlServer.Shutdown(shutdownCtx), publicServer.Shutdown(shutdownCtx))
	if err != nil {
		return err
	}
	return nil
}

func waitForFirstProjectDaemonSession(ctx context.Context, sessions *projectdaemon.SessionRegistry, interval time.Duration) error {
	if interval <= 0 {
		interval = 25 * time.Millisecond
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		if sessions.SeenSession() {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

func waitForFirstProjectDaemonActivity(ctx context.Context, sessions *projectdaemon.SessionRegistry, presence *projectdaemon.AgentPresenceRegistry, interval time.Duration) error {
	if interval <= 0 {
		interval = 25 * time.Millisecond
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		seen, _ := projectDaemonSeenActivityCount(sessions, presence)
		if seen {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

func idleShutdownCallback(ctx context.Context, cancel context.CancelFunc, idleTimeout time.Duration, currentCount func() int) func(count int) {
	var mu sync.Mutex
	var timer *time.Timer
	lastCount := -1
	return func(count int) {
		mu.Lock()
		defer mu.Unlock()
		if currentCount != nil && currentCount() != count {
			return
		}
		if count == lastCount {
			return
		}
		lastCount = count
		if timer != nil {
			timer.Stop()
			timer = nil
		}
		if count > 0 {
			return
		}
		if idleTimeout == 0 {
			if currentCount == nil || currentCount() == 0 {
				cancel()
			}
			return
		}
		timer = time.AfterFunc(idleTimeout, func() {
			if ctx.Err() == nil && (currentCount == nil || currentCount() == 0) {
				cancel()
			}
		})
	}
}

func projectDaemonActivityCount(sessions *projectdaemon.SessionRegistry, presence *projectdaemon.AgentPresenceRegistry) int {
	count := 0
	if sessions != nil {
		count += sessions.Count()
	}
	if presence != nil {
		count += presence.Count()
	}
	return count
}

func projectDaemonSeenActivityCount(sessions *projectdaemon.SessionRegistry, presence *projectdaemon.AgentPresenceRegistry) (bool, int) {
	seen := false
	count := 0
	if sessions != nil {
		sessionSeen, sessionCount := sessions.SeenSessionCount()
		seen = seen || sessionSeen
		count += sessionCount
	}
	if presence != nil {
		presenceSeen, presenceCount := presence.SeenPresenceCount()
		seen = seen || presenceSeen
		count += presenceCount
	}
	return seen, count
}

func cancelIfNoSessionAfterStartupGrace(ctx context.Context, cancel context.CancelFunc, sessions *projectdaemon.SessionRegistry, grace time.Duration) {
	if grace <= 0 {
		grace = projectdaemon.DefaultHeartbeatTTL
	}
	timer := time.NewTimer(grace)
	defer timer.Stop()
	ticker := time.NewTicker(25 * time.Millisecond)
	defer ticker.Stop()
	for {
		seen, _ := sessions.SeenSessionCount()
		if seen {
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		case <-timer.C:
			seen, count := sessions.SeenSessionCount()
			if !seen && count == 0 {
				cancel()
			}
			return
		}
	}
}

func cancelIfNoActivityAfterStartupGrace(ctx context.Context, cancel context.CancelFunc, sessions *projectdaemon.SessionRegistry, presence *projectdaemon.AgentPresenceRegistry, grace time.Duration) {
	if grace <= 0 {
		grace = projectdaemon.DefaultHeartbeatTTL
	}
	timer := time.NewTimer(grace)
	defer timer.Stop()
	ticker := time.NewTicker(25 * time.Millisecond)
	defer ticker.Stop()
	for {
		seen, _ := projectDaemonSeenActivityCount(sessions, presence)
		if seen {
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		case <-timer.C:
			seen, count := projectDaemonSeenActivityCount(sessions, presence)
			if !seen && count == 0 {
				cancel()
			}
			return
		}
	}
}

type nopWriteCloser struct {
	io.Writer
}

func (nopWriteCloser) Close() error { return nil }

type lockedWriter struct {
	io.Writer
	mu sync.Mutex
}

func (w *lockedWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.Writer.Write(p)
}

// CLI > ENV > default(flag)
func resolveString(flagName, flagVal string, visited map[string]bool, envVar string, def string) string {
	// If flag was explicitly set, it takes precedence
	if visited[flagName] {
		return flagVal
	}
	// Next, check environment variable
	if env := strings.TrimSpace(os.Getenv(envVar)); env != "" {
		return env
	}
	// Fall back to provided default when flag wasn't set and no env var is present
	return def
}

func resolveMarkdownLinkRootPrefix(flags *cliFlags, visited map[string]bool) (string, error) {
	value := resolveString(
		"markdown-link-root-prefix",
		*flags.markdownLinkRootPrefix,
		visited,
		"LEAFWIKI_MARKDOWN_LINK_ROOT_PREFIX",
		"",
	)
	return markdownlinks.NormalizeMarkdownLinkRootPrefix(value)
}

func resolveWorkspace(flags *cliFlags, visited map[string]bool) (wiki.Workspace, error) {
	dataDir := resolveString("data-dir", *flags.dataDir, visited, "LEAFWIKI_DATA_DIR", "./data")
	rootDir := resolveString("root-dir", *flags.rootDir, visited, "LEAFWIKI_ROOT_DIR", "")
	workspace := wiki.NormalizeWorkspace(wiki.Workspace{
		ID:      wikid.HomeWorkspaceID,
		DataDir: dataDir,
		RootDir: rootDir,
	})
	if err := validateWorkspaceDirs(workspace.DataDir, workspace.RootDir); err != nil {
		return wiki.Workspace{}, err
	}
	return workspace, nil
}

func applyYAMLConfigFile(fs *flag.FlagSet, flags *cliFlags, visited map[string]bool) error {
	return runtimeconfig.ApplyYAMLConfigFile(fs, *flags.config, visited)
}

func applyYAMLConfigPath(fs *flag.FlagSet, visited map[string]bool, path string, source string) error {
	return runtimeconfig.ApplyYAMLConfigPath(fs, visited, path, source)
}

func applyDaemonServiceConfig(fs *flag.FlagSet, flags *cliFlags, visited map[string]bool, args []string) error {
	return runtimeconfig.ApplyDaemonServiceConfig(fs, visited, args)
}

func applyDaemonServiceDefaults(fs *flag.FlagSet, flags *cliFlags, visited map[string]bool) error {
	return runtimeconfig.ApplyDaemonServiceDefaults(fs, visited)
}

func validateConfigModeArgs(args []string) error {
	return runtimeconfig.ValidateConfigModeArgs(args)
}

func configModeFlagDisplay(arg string, name string) string {
	if strings.HasPrefix(arg, "--") {
		return "--" + name
	}
	return strings.SplitN(arg, "=", 2)[0]
}

func configFileFlagNames() map[string]struct{} {
	return runtimeconfig.ConfigFileFlagNames()
}

func resolveStartupWorkspace(flags *cliFlags, visited map[string]bool, args []string) (wiki.Workspace, bool, error) {
	if len(args) > 0 && !isAgentHookCommand(args) && !isDaemonCommand(args) {
		return wiki.Workspace{}, false, nil
	}
	workspace, err := resolveWorkspace(flags, visited)
	if err != nil {
		return wiki.Workspace{}, true, err
	}
	return workspace, true, nil
}

func resolveLoggingConfig(flags *cliFlags, visited map[string]bool, dataDir string) (leaflogging.Config, error) {
	envTargetSet := strings.TrimSpace(os.Getenv("LEAFWIKI_LOG_TARGET")) != ""
	logTarget := resolveString("log-target", *flags.logTarget, visited, "LEAFWIKI_LOG_TARGET", "file")

	envLogFileSet := false
	if envLogFile, ok := os.LookupEnv("LEAFWIKI_LOG_FILE"); ok && strings.TrimSpace(envLogFile) != "" {
		envLogFileSet = true
	}
	logFile := resolveString("log-file", *flags.logFile, visited, "LEAFWIKI_LOG_FILE", "")
	if visited["log-target"] && !visited["log-file"] && strings.TrimSpace(strings.ToLower(logTarget)) != string(leaflogging.TargetFile) {
		envLogFileSet = false
		logFile = ""
	}

	return leaflogging.Resolve(leaflogging.ConfigInput{
		Target:          logTarget,
		TargetSet:       visited["log-target"] || envTargetSet,
		FilePath:        logFile,
		FilePathSet:     visited["log-file"] || envLogFileSet,
		LevelFromConfig: os.Getenv("LEAFWIKI_LOG_LEVEL"),
		DataDir:         dataDir,
	})
}

func validateWorkspaceDirs(dataDir string, rootDir string) error {
	return wiki.ValidateWorkspace(wiki.Workspace{
		ID:      "default",
		DataDir: dataDir,
		RootDir: rootDir,
	})
}

// CLI > ENV > default(flag)
func resolveBool(flagName string, flagVal bool, visited map[string]bool, envVar string) bool {
	if visited[flagName] {
		return flagVal
	}
	if env := strings.TrimSpace(os.Getenv(envVar)); env != "" {
		if b, ok := parseBool(env); ok {
			return b
		}
		// If env var is set but invalid, fail fast (helps operators)
		fail("Invalid environment variable value", "variable", envVar, "value", env, "expected", "true/false/1/0/yes/no")
	}
	return flagVal // default from flag
}

func resolveInt(flagName string, flagVal int, visited map[string]bool, envVar string, def int) int {
	if visited[flagName] {
		return flagVal
	}
	if env := strings.TrimSpace(os.Getenv(envVar)); env != "" {
		var n int
		if _, err := fmt.Sscanf(env, "%d", &n); err == nil {
			return n
		}
		fail("Invalid environment variable value", "variable", envVar, "value", env, "expected", "integer")
	}
	return def
}

func resolveDuration(flagName string, flagVal time.Duration, visited map[string]bool, envVar string) time.Duration {
	if visited[flagName] {
		return flagVal
	}
	if env := strings.TrimSpace(os.Getenv(envVar)); env != "" {
		if d, ok := parseDuration(env); ok {
			return d
		}
		// If env var is set but invalid, fail fast (helps operators)
		fail("Invalid environment variable value", "variable", envVar, "value", env, "expected", "duration like 24h, 15m")
	}
	return flagVal // default from flag
}

type mcpTransports = runtimeconfig.MCPTransports

func resolveMCPTransports(flags *cliFlags, visited map[string]bool) (mcpTransports, error) {
	raw := resolveString("mcp", *flags.mcp, visited, "LEAFWIKI_MCP", "none")
	return parseMCPTransports(raw)
}

func parseMCPTransports(raw string) (mcpTransports, error) {
	return runtimeconfig.ParseMCPTransports(raw)
}

type mcpTransportOptions struct {
	Transports  mcpTransports
	DisableAuth bool
	LogTarget   leaflogging.Target
	Host        string
	APIKey      string
}

func validateMCPTransportOptions(opts mcpTransportOptions) error {
	if opts.Transports.HTTP && !httpinternal.IsLoopbackHost(opts.Host) {
		return fmt.Errorf("MCP requires a loopback host (localhost, 127.0.0.1, or ::1)")
	}
	if !opts.Transports.Stdio {
		return nil
	}
	if opts.LogTarget == leaflogging.TargetStdout {
		return fmt.Errorf("stdout is reserved for MCP STDIO")
	}
	hasAPIKey := strings.TrimSpace(opts.APIKey) != ""
	if opts.DisableAuth && hasAPIKey {
		return fmt.Errorf("disabled auth and API-key STDIO identity cannot be combined")
	}
	if !opts.DisableAuth && !hasAPIKey {
		return fmt.Errorf("native STDIO requires either disabled auth or an API key")
	}
	return nil
}

func parseByteSize(raw string, label string) int64 {
	size, err := humanize.ParseBytes(strings.TrimSpace(raw))
	if err != nil {
		fail("Invalid byte size value", "setting", label, "value", raw, "error", err)
	}
	if size == 0 {
		fail("Byte size value must be greater than zero", "setting", label, "value", raw)
	}
	if size > math.MaxInt64 {
		fail("Byte size value is too large", "setting", label, "value", raw)
	}
	return int64(size)
}

func parseBool(s string) (bool, bool) {
	s = strings.TrimSpace(strings.ToLower(s))
	switch s {
	case "true", "1", "yes", "y", "on":
		return true, true
	case "false", "0", "no", "n", "off":
		return false, true
	}

	return false, false
}

func parseDuration(s string) (time.Duration, bool) {
	d, err := time.ParseDuration(s)
	if err != nil {
		return 0, false
	}
	return d, true
}

func validateHTTPRemoteUserConfig(enabled bool, trustedProxyIPsRaw string) error {
	if !enabled {
		return nil
	}
	hasTrustedProxy := false
	for _, entry := range strings.Split(trustedProxyIPsRaw, ",") {
		if strings.TrimSpace(entry) != "" {
			hasTrustedProxy = true
			break
		}
	}
	if !hasTrustedProxy {
		return fmt.Errorf("--trusted-proxy-ips is required when --enable-http-remote-user is set. Set it using --trusted-proxy-ips or LEAFWIKI_TRUSTED_PROXY_IPS")
	}
	return nil
}

type httpRouterOptionsInput struct {
	publicAccess            bool
	injectCodeInHeader      string
	customStylesheet        string
	allowInsecure           bool
	hideLinkMetadataSection bool
	accessTokenTimeout      time.Duration
	refreshTokenTimeout     time.Duration
	authDisabled            bool
	basePath                string
	markdownLinkRootPrefix  string
	maxAssetUploadSize      int64
	enableRevision          bool
	enableWorkspaceSync     bool
	enableLinkRefactor      bool
	enableMCP               bool
	host                    string
	mcpToolListPageSize     int
	httpRemoteUser          httpinternal.HTTPRemoteUserConfig
	disableRequestLog       bool
}

func buildHTTPRouterOptions(in httpRouterOptionsInput) httpinternal.RouterOptions {
	return httpinternal.RouterOptions{
		PublicAccess:            in.publicAccess,
		InjectCodeInHeader:      in.injectCodeInHeader,
		CustomStylesheet:        in.customStylesheet,
		AllowInsecure:           in.allowInsecure,
		HideLinkMetadataSection: in.hideLinkMetadataSection,
		AccessTokenTimeout:      in.accessTokenTimeout,
		RefreshTokenTimeout:     in.refreshTokenTimeout,
		AuthDisabled:            in.authDisabled,
		BasePath:                in.basePath,
		MarkdownLinkRootPrefix:  in.markdownLinkRootPrefix,
		MaxAssetUploadSizeBytes: in.maxAssetUploadSize,
		EnableRevision:          in.enableRevision,
		EnableWorkspaceSync:     in.enableWorkspaceSync,
		EnableLinkRefactor:      in.enableLinkRefactor,
		MCPEnabled:              in.enableMCP,
		MCPBindHost:             in.host,
		MCPToolListPageSize:     in.mcpToolListPageSize,
		HTTPRemoteUser:          in.httpRemoteUser,
		DisableRequestLog:       in.disableRequestLog,
	}
}

// normalizeBasePath normalizes the base path to the form "/mypath" (no trailing slash).
// Accepts "mypath", "/mypath", "/mypath/", etc. Returns "" for root.
func normalizeBasePath(s string) string {
	s = strings.TrimSpace(s)
	s = strings.Trim(s, "/")
	if s == "" {
		return ""
	}
	return "/" + s
}
