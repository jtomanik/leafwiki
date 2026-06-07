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
	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/perber/wiki/internal/agenthooks"
	coreauth "github.com/perber/wiki/internal/core/auth"
	"github.com/perber/wiki/internal/core/tools"
	httpinternal "github.com/perber/wiki/internal/http"
	authmw "github.com/perber/wiki/internal/http/middleware/auth"
	"github.com/perber/wiki/internal/locking"
	leaflogging "github.com/perber/wiki/internal/logging"
	"github.com/perber/wiki/internal/projectdaemon"
	"github.com/perber/wiki/internal/wiki"
)

func writeUsage(w io.Writer) {
	if _, err := fmt.Fprintln(w, `LeafWiki – lightweight selfhosted wiki 🌿

	Usage:
	leafwiki --jwt-secret <SECRET> --admin-password <PASSWORD> [--host <HOST>] [--port <PORT>] [--data-dir <DIR>] [--root-dir <DIR>]
	leafwiki --disable-auth [--host <HOST>] [--port <PORT>] [--data-dir <DIR>] [--root-dir <DIR>]
	leafwiki --mcp=stdio --disable-auth [--host <HOST>] [--port <PORT>] [--data-dir <DIR>] [--root-dir <DIR>]
	leafwiki agent-hook <codex|claude|cursor|unknown> [--host <HOST>] [--port <PORT>] [--data-dir <DIR>] [--root-dir <DIR>]
	leafwiki reset-admin-password
	leafwiki --help

	Options:
	--host             Host/IP address to bind the server to (default: 127.0.0.1)
	--port             Port to run the server on (default: 8080)
	--data-dir         Path to data directory (default: ./data)
	--root-dir         Path to managed markdown content directory (default: <data-dir>/root)
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
	--daemon-idle-timeout         Project daemon idle timeout after last session exits; 0 stops immediately (default: 10m)
	--max-revision-history        Maximum revisions kept per page; 0 = unlimited (default: 100)
	--enable-http-remote-user       Enable reverse-proxy authentication via HTTP header (default: false)
	--http-remote-user-header-name  HTTP header carrying the username from a trusted proxy (default: Remote-User)
	--trusted-proxy-ips             Comma-separated trusted proxy IPs/CIDRs (e.g. 127.0.0.1,172.18.0.0/16)
	--http-remote-user-logout-url   URL the frontend redirects to after logout in proxy-auth mode (default: "")
	--disable-request-log           Suppress per-request HTTP access log lines (default: false)

	Environment variables:
	LEAFWIKI_HOST
	LEAFWIKI_PORT
	LEAFWIKI_DATA_DIR
	LEAFWIKI_ROOT_DIR
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
	host                    *string
	port                    *string
	dataDir                 *string
	rootDir                 *string
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
}

type leafwikiRuntimeConfig struct {
	Workspace               wiki.Workspace
	Host                    string
	Port                    string
	AdminPassword           string
	JWTSecret               string
	PublicAccess            bool
	AllowInsecure           bool
	InjectCodeInHeader      string
	CustomStylesheet        string
	Logging                 leaflogging.Config
	DisableAuth             bool
	HideLinkMetadataSection bool
	AccessTokenTimeout      time.Duration
	RefreshTokenTimeout     time.Duration
	BasePath                string
	MaxAssetUploadSize      int64
	EnableRevision          bool
	EnableWorkspaceSync     bool
	EnableLinkRefactor      bool
	MCPTransports           mcpTransports
	APIKey                  string
	MaxRevisionHistory      int
	EnableHTTPRemoteUser    bool
	HTTPRemoteUserHeader    string
	TrustedProxyIPsRaw      string
	HTTPRemoteUserLogoutURL string
	DisableRequestLog       bool
	DaemonIdleTimeout       time.Duration
	DaemonStartupErrorPath  string
	DetachDaemonOwnerIO     bool
}

func registerFlags(fs *flag.FlagSet) *cliFlags {
	return &cliFlags{
		host:                    fs.String("host", "", "host/IP address to bind the server to (e.g. 127.0.0.1 or 0.0.0.0)"),
		port:                    fs.String("port", "", "port to run the server on"),
		dataDir:                 fs.String("data-dir", "", "path to data directory"),
		rootDir:                 fs.String("root-dir", "", "path to managed markdown content directory"),
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
		daemonIdleTimeout:       fs.Duration("daemon-idle-timeout", projectdaemon.DefaultIdleTimeout, "project daemon idle timeout after last session exits; 0 stops immediately"),
		internalProjectDaemon:   fs.String("internal-project-daemon", "", "internal project daemon startup config path"),
		enableMCP:               fs.Bool("enable-mcp", false, "compatibility flag for local MCP Streamable HTTP endpoint"),
		mcpStdio:                fs.Bool("mcp-stdio", false, "compatibility flag for native MCP STDIO"),
		maxRevisionHistory:      fs.Int("max-revision-history", 100, "maximum revisions kept per page; 0 = unlimited (default: 100)"),
		enableHTTPRemoteUser:    fs.Bool("enable-http-remote-user", false, "enable reverse-proxy authentication via HTTP header (default: false)"),
		httpRemoteUserHeader:    fs.String("http-remote-user-header-name", "Remote-User", "HTTP header name carrying the username from a trusted proxy (default: Remote-User)"),
		trustedProxyIPs:         fs.String("trusted-proxy-ips", "", "comma-separated list of trusted proxy IPs/CIDRs (e.g. 127.0.0.1,172.18.0.0/16)"),
		httpRemoteUserLogoutURL: fs.String("http-remote-user-logout-url", "", "URL the frontend redirects to after logout when reverse-proxy auth is active (e.g. https://auth.example.com/logout)"),
		disableRequestLog:       fs.Bool("disable-request-log", false, "suppress per-request HTTP access log lines (default: false)"),
	}
}

func main() {
	setupBootstrapLogger(os.Stderr)
	failOpenAgentHookProvider = ""
	rawArgs := os.Args[1:]
	if provider, ok := agentHookProviderFromArgs(rawArgs); ok {
		failOpenAgentHookProvider = provider
	}
	rawArgs = normalizeAgentHookRawArgs(rawArgs)
	if shouldPrintUsage(rawArgs) {
		printUsage()
		return
	}

	flag.CommandLine = flag.NewFlagSet(os.Args[0], flag.ContinueOnError)
	flag.CommandLine.SetOutput(os.Stderr)
	flag.CommandLine.Usage = func() {
		writeUsage(flag.CommandLine.Output())
	}

	flags := registerFlags(flag.CommandLine)
	if err := flag.CommandLine.Parse(rawArgs); err != nil {
		if failOpenAgentHookProvider != "" {
			fail("Invalid agent hook arguments", "error", err)
		}
		os.Exit(2)
	}
	if strings.TrimSpace(*flags.internalProjectDaemon) != "" {
		if err := runInternalProjectDaemon(context.Background(), *flags.internalProjectDaemon); err != nil {
			fail("Project daemon failed", "error", err)
		}
		return
	}

	// Track which flags were explicitly set on CLI
	visited := map[string]bool{}
	flag.Visit(func(f *flag.Flag) { visited[f.Name] = true })

	dataDir := resolveString("data-dir", *flags.dataDir, visited, "LEAFWIKI_DATA_DIR", "./data")
	args := flag.Args()
	agentHookRequested := isAgentHookCommand(args)
	resolvedMCPTransports := mcpTransports{}
	if !agentHookRequested {
		var err error
		resolvedMCPTransports, err = resolveMCPTransports(flags, visited)
		if err != nil {
			fail("Invalid MCP configuration", "error", err)
		}
	}
	if resolvedMCPTransports.Stdio && len(args) > 0 {
		fail("Invalid native STDIO configuration", "error", fmt.Errorf("native STDIO does not support positional commands"))
	}
	if len(args) > 0 && !agentHookRequested {
		switch args[0] {
		case "reset-admin-password":
			user, err := tools.ResetAdminPassword(dataDir)
			if err != nil {
				fail("Password reset failed", "error", err)
			}

			fmt.Println("Admin password reset successfully.")
			fmt.Printf("New password for user %s: %s\n", user.Username, user.Password)
			return
		case "--help", "-h", "help":
			printUsage()
			return
		default:
			fmt.Printf("Unknown command: %s\n\n", args[0])
			printUsage()
			return
		}
	}

	host := resolveString("host", *flags.host, visited, "LEAFWIKI_HOST", "127.0.0.1")
	port := resolveString("port", *flags.port, visited, "LEAFWIKI_PORT", "8080")
	workspace, shouldStartWiki, err := resolveStartupWorkspace(flags, visited, flag.Args())
	if err != nil {
		fail("Invalid workspace configuration", "error", err)
	}
	if shouldStartWiki {
		dataDir = workspace.DataDir
	}
	adminPassword := resolveString("admin-password", *flags.adminPassword, visited, "LEAFWIKI_ADMIN_PASSWORD", "")
	jwtSecret := resolveString("jwt-secret", *flags.jwtSecret, visited, "LEAFWIKI_JWT_SECRET", "")
	injectCodeInHeader := resolveString("inject-code-in-header", *flags.injectCodeInHeader, visited, "LEAFWIKI_INJECT_CODE_IN_HEADER", "")
	customStylesheet := resolveString("custom-stylesheet", *flags.customStylesheet, visited, "LEAFWIKI_CUSTOM_STYLESHEET", "")
	allowInsecure := resolveBool("allow-insecure", *flags.allowInsecure, visited, "LEAFWIKI_ALLOW_INSECURE")
	publicAccess := resolveBool("public-access", *flags.publicAccess, visited, "LEAFWIKI_PUBLIC_ACCESS")
	hideLinkMetadataSection := resolveBool("hide-link-metadata-section", *flags.hideLinkMetadataSection, visited, "LEAFWIKI_HIDE_LINK_METADATA_SECTION")
	accessTokenTimeout := resolveDuration("access-token-timeout", *flags.accessTokenTimeout, visited, "LEAFWIKI_ACCESS_TOKEN_TIMEOUT")
	refreshTokenTimeout := resolveDuration("refresh-token-timeout", *flags.refreshTokenTimeout, visited, "LEAFWIKI_REFRESH_TOKEN_TIMEOUT")
	daemonIdleTimeout := resolveDuration("daemon-idle-timeout", *flags.daemonIdleTimeout, visited, "LEAFWIKI_DAEMON_IDLE_TIMEOUT")
	// If disable-auth is set, later logic will override publicAccess accordingly
	disableAuth := resolveBool("disable-auth", *flags.disableAuth, visited, "LEAFWIKI_DISABLE_AUTH")
	basePath := normalizeBasePath(resolveString("base-path", *flags.basePath, visited, "LEAFWIKI_BASE_PATH", ""))
	maxAssetUploadSize := parseByteSize(
		resolveString("max-asset-upload-size", *flags.maxAssetUploadSize, visited, "LEAFWIKI_MAX_ASSET_UPLOAD_SIZE", "50MiB"),
		"max asset upload size",
	)
	enableRevision := resolveBool("enable-revision", *flags.enableRevision, visited, "LEAFWIKI_ENABLE_REVISION")
	enableWorkspaceSync := resolveBool("enable-workspace-sync", *flags.enableWorkspaceSync, visited, "LEAFWIKI_ENABLE_WORKSPACE_SYNC")
	if enableRevision && enableWorkspaceSync {
		fail("Invalid revision configuration", "error", fmt.Errorf("enable-revision and enable-workspace-sync cannot be combined"))
	}
	enableLinkRefactor := resolveBool("enable-link-refactor", *flags.enableLinkRefactor, visited, "LEAFWIKI_ENABLE_LINK_REFACTOR")
	apiKey := ""
	if resolvedMCPTransports.Stdio {
		apiKey = resolveString("api-key", *flags.apiKey, visited, "LEAFWIKI_MCP_API_KEY", "")
	}
	maxRevisionHistory := resolveInt("max-revision-history", *flags.maxRevisionHistory, visited, "LEAFWIKI_MAX_REVISION_HISTORY", 100)
	enableHTTPRemoteUser := resolveBool("enable-http-remote-user", *flags.enableHTTPRemoteUser, visited, "LEAFWIKI_ENABLE_HTTP_REMOTE_USER")
	httpRemoteUserHeader := resolveString("http-remote-user-header-name", *flags.httpRemoteUserHeader, visited, "LEAFWIKI_HTTP_REMOTE_USER_HEADER_NAME", "Remote-User")
	trustedProxyIPsRaw := resolveString("trusted-proxy-ips", *flags.trustedProxyIPs, visited, "LEAFWIKI_TRUSTED_PROXY_IPS", "")
	httpRemoteUserLogoutURL := resolveString("http-remote-user-logout-url", *flags.httpRemoteUserLogoutURL, visited, "LEAFWIKI_HTTP_REMOTE_USER_LOGOUT_URL", "")
	disableRequestLog := resolveBool("disable-request-log", *flags.disableRequestLog, visited, "LEAFWIKI_DISABLE_REQUEST_LOG")
	if _, err := authmw.ParseTrustedProxies(trustedProxyIPsRaw); err != nil {
		fail("invalid --trusted-proxy-ips value", "error", err)
	}

	if err := validateHTTPRemoteUserConfig(enableHTTPRemoteUser, trustedProxyIPsRaw); err != nil {
		fail("Invalid HTTP remote user configuration", "error", err)
	}

	loggingConfig, err := resolveLoggingConfig(flags, visited, dataDir)
	if err != nil {
		fail("Invalid logging configuration", "error", err)
	}
	if err := validateMCPTransportOptions(mcpTransportOptions{
		Transports:  resolvedMCPTransports,
		DisableAuth: disableAuth,
		LogTarget:   loggingConfig.Target,
		Host:        host,
		APIKey:      apiKey,
	}); err != nil {
		fail("Invalid MCP configuration", "error", err)
	}
	if disableAuth {
		publicAccess = true
	}

	cfg := leafwikiRuntimeConfig{
		Workspace:               workspace,
		Host:                    host,
		Port:                    port,
		AdminPassword:           adminPassword,
		JWTSecret:               jwtSecret,
		PublicAccess:            publicAccess,
		AllowInsecure:           allowInsecure,
		InjectCodeInHeader:      injectCodeInHeader,
		CustomStylesheet:        customStylesheet,
		Logging:                 loggingConfig,
		DisableAuth:             disableAuth,
		HideLinkMetadataSection: hideLinkMetadataSection,
		AccessTokenTimeout:      accessTokenTimeout,
		RefreshTokenTimeout:     refreshTokenTimeout,
		BasePath:                basePath,
		MaxAssetUploadSize:      maxAssetUploadSize,
		EnableRevision:          enableRevision,
		EnableWorkspaceSync:     enableWorkspaceSync,
		EnableLinkRefactor:      enableLinkRefactor,
		MCPTransports:           resolvedMCPTransports,
		APIKey:                  apiKey,
		MaxRevisionHistory:      maxRevisionHistory,
		EnableHTTPRemoteUser:    enableHTTPRemoteUser,
		HTTPRemoteUserHeader:    httpRemoteUserHeader,
		TrustedProxyIPsRaw:      trustedProxyIPsRaw,
		HTTPRemoteUserLogoutURL: httpRemoteUserLogoutURL,
		DisableRequestLog:       disableRequestLog,
		DaemonIdleTimeout:       daemonIdleTimeout,
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

func agentHookProviderFromArgs(args []string) (string, bool) {
	if len(args) == 0 || args[0] != "agent-hook" {
		return "", false
	}
	if len(args) >= 2 {
		return args[1], true
	}
	return agenthooks.ProviderUnknown, true
}

func isAgentHookCommand(args []string) bool {
	return len(args) > 0 && args[0] == "agent-hook"
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
	ownerCfg, err := daemonRequestConfigForRuntime(cfg)
	if err != nil {
		return err
	}
	descriptorPath := projectdaemon.DescriptorPath(ownerCfg.DataDir)
	desc, err := attachOrStartProjectDaemon(ctx, cfg, ownerCfg, descriptorPath)
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
		bridgeErr := make(chan error, 1)
		go func() {
			bridgeErr <- runDaemonStdioBridge(bridgeCtx, daemonStdioBridgeConfig(desc, cfg))
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
	ownerCfg, err := daemonRequestConfigForRuntime(cfg)
	if err != nil {
		return err
	}
	desc, err := attachOrStartProjectDaemon(ctx, cfg, ownerCfg, projectdaemon.DescriptorPath(ownerCfg.DataDir))
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
	return daemonStdioBridge{
		ControlURL:   desc.ControlURL,
		ControlToken: desc.ControlToken,
		APIKey:       cfg.APIKey,
		Stdin:        os.Stdin,
		Stdout:       os.Stdout,
		Stderr:       os.Stderr,
	}
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

func attachOrStartProjectDaemon(ctx context.Context, cfg leafwikiRuntimeConfig, ownerCfg projectdaemon.Config, descriptorPath string) (*projectdaemon.Descriptor, error) {
	desc, healthy, err := readHealthyProjectDaemon(ctx, descriptorPath, ownerCfg)
	if err != nil {
		return nil, err
	}
	if healthy {
		if mismatches := compareProjectDaemonConfigForRequest(desc.Config, ownerCfg, cfg.MCPTransports); len(mismatches) > 0 {
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
		if err := verifyStdioAPIKeyFromStorage(ownerCfg.DataDir, cfg.APIKey); err != nil {
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

func waitForProjectDaemon(ctx context.Context, descriptorPath string, errorPath string, ownerCfg projectdaemon.Config, requestTransports mcpTransports) (*projectdaemon.Descriptor, error) {
	deadlineCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
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
			if mismatches := compareProjectDaemonConfigForRequest(desc.Config, ownerCfg, requestTransports); len(mismatches) > 0 {
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

func compareProjectDaemonConfigForRequest(owner projectdaemon.Config, requested projectdaemon.Config, requestTransports mcpTransports) []projectdaemon.Mismatch {
	normalized := requested
	if requestTransports.Stdio && !requestTransports.HTTP {
		normalized.PublicMCPEnabled = owner.PublicMCPEnabled
		normalized.Host = owner.Host
		normalized.LogTarget = owner.LogTarget
		normalized.LogFile = owner.LogFile
		normalized.DisableRequestLog = owner.DisableRequestLog
	}
	return projectdaemon.CompareConfig(owner, normalized)
}

func verifyStdioAPIKeyFromStorage(dataDir string, apiKey string) error {
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		return err
	}
	userStore, err := coreauth.NewUserStore(dataDir)
	if err != nil {
		return err
	}
	defer userStore.Close()
	userService := coreauth.NewUserService(userStore)
	apiKeyStore, err := coreauth.NewAPIKeyStore(dataDir)
	if err != nil {
		return err
	}
	apiKeyService := coreauth.NewAPIKeyService(apiKeyStore, userService)
	defer apiKeyService.Close()
	_, err = apiKeyService.VerifyAPIKey(apiKey)
	return err
}

func daemonConfigForRuntime(cfg leafwikiRuntimeConfig) (projectdaemon.Config, error) {
	dataDir, rootDir, err := projectdaemon.CanonicalizeProject(cfg.Workspace.DataDir, cfg.Workspace.RootDir)
	if err != nil {
		return projectdaemon.Config{}, err
	}
	logFile := daemonLogFileForConfig(cfg, dataDir)
	return projectdaemon.Config{
		DataDir:                 dataDir,
		RootDir:                 rootDir,
		AuthDisabled:            cfg.DisableAuth,
		PublicMCPEnabled:        cfg.MCPTransports.HTTP,
		Host:                    cfg.Host,
		Port:                    cfg.Port,
		BasePath:                cfg.BasePath,
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
	ownerCfg := cfg
	ownerCfg.APIKey = ""
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
	ControlURL   string
	ControlToken string
	APIKey       string
	Stdin        io.ReadCloser
	Stdout       io.Writer
	Stderr       io.Writer
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
		Endpoint:             strings.TrimRight(cfg.ControlURL, "/") + "/mcp",
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
		Transport: projectdaemon.AuthRoundTripper{
			Base: &http.Transport{
				Proxy:                 http.ProxyFromEnvironment,
				DialContext:           (&net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
				ForceAttemptHTTP2:     true,
				MaxIdleConns:          100,
				IdleConnTimeout:       90 * time.Second,
				TLSHandshakeTimeout:   10 * time.Second,
				ExpectContinueTimeout: 1 * time.Second,
			},
			ControlToken: cfg.ControlToken,
			BearerToken:  cfg.APIKey,
		},
	}
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

	errs := make(chan error, 2)
	pump := func(from sdkmcp.Connection, to sdkmcp.Connection) {
		for {
			msg, err := from.Read(ctx)
			if err != nil {
				errs <- err
				return
			}
			if err := to.Write(ctx, msg); err != nil {
				errs <- err
				return
			}
		}
	}
	go pump(leftConn, rightConn)
	go pump(rightConn, leftConn)
	err = <-errs
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

	w, err := wiki.NewWiki(&wiki.WikiOptions{
		Workspace:           wiki.Workspace{ID: cfg.Workspace.ID, DataDir: ownerCfg.DataDir, RootDir: ownerCfg.RootDir},
		StorageDir:          ownerCfg.DataDir,
		AdminPassword:       cfg.AdminPassword,
		JWTSecret:           cfg.JWTSecret,
		AccessTokenTimeout:  cfg.AccessTokenTimeout,
		RefreshTokenTimeout: cfg.RefreshTokenTimeout,
		AuthDisabled:        cfg.DisableAuth,
		EnableRevision:      cfg.EnableRevision,
		EnableWorkspaceSync: cfg.EnableWorkspaceSync,
		MaxRevisionHistory:  cfg.MaxRevisionHistory,
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

func resolveWorkspace(flags *cliFlags, visited map[string]bool) (wiki.Workspace, error) {
	dataDir := resolveString("data-dir", *flags.dataDir, visited, "LEAFWIKI_DATA_DIR", "./data")
	rootDir := resolveString("root-dir", *flags.rootDir, visited, "LEAFWIKI_ROOT_DIR", "")
	workspace := wiki.NormalizeWorkspace(wiki.Workspace{
		ID:      "default",
		DataDir: dataDir,
		RootDir: rootDir,
	})
	if err := validateWorkspaceDirs(workspace.DataDir, workspace.RootDir); err != nil {
		return wiki.Workspace{}, err
	}
	return workspace, nil
}

func resolveStartupWorkspace(flags *cliFlags, visited map[string]bool, args []string) (wiki.Workspace, bool, error) {
	if len(args) > 0 && !isAgentHookCommand(args) {
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

type mcpTransports struct {
	HTTP  bool
	Stdio bool
}

func (t mcpTransports) any() bool {
	return t.HTTP || t.Stdio
}

func resolveMCPTransports(flags *cliFlags, visited map[string]bool) (mcpTransports, error) {
	raw := resolveString("mcp", *flags.mcp, visited, "LEAFWIKI_MCP", "none")
	return parseMCPTransports(raw)
}

func parseMCPTransports(raw string) (mcpTransports, error) {
	value := strings.TrimSpace(strings.ToLower(raw))
	if value == "" {
		value = "none"
	}

	parts := strings.Split(value, ",")
	if len(parts) > 2 {
		return mcpTransports{}, fmt.Errorf("invalid MCP transport %q", raw)
	}

	var transports mcpTransports
	seen := map[string]bool{}
	for _, part := range parts {
		name := strings.TrimSpace(part)
		if name == "" {
			return mcpTransports{}, fmt.Errorf("invalid MCP transport %q", raw)
		}
		if seen[name] {
			return mcpTransports{}, fmt.Errorf("duplicate MCP transport %q", name)
		}
		seen[name] = true
		switch name {
		case "none":
		case "http":
			transports.HTTP = true
		case "stdio":
			transports.Stdio = true
		default:
			return mcpTransports{}, fmt.Errorf("invalid MCP transport %q", name)
		}
	}

	if seen["none"] && len(seen) > 1 {
		return mcpTransports{}, fmt.Errorf("none cannot be combined with other MCP transports")
	}
	return transports, nil
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
