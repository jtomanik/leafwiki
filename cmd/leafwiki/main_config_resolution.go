package main

import (
	"flag"
	"fmt"
	"math"
	"os"
	"strings"
	"time"

	"github.com/dustin/go-humanize"
	"github.com/perber/wiki/internal/core/markdownlinks"
	"github.com/perber/wiki/internal/core/shared"
	httpinternal "github.com/perber/wiki/internal/http"
	"github.com/perber/wiki/internal/localization"
	leaflogging "github.com/perber/wiki/internal/logging"
	"github.com/perber/wiki/internal/runtimeconfig"
	"github.com/perber/wiki/internal/wiki"
	"github.com/perber/wiki/internal/wikid"
)

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
		fail(localization.MessageIDCLIErrorInvalidEnvironmentVariableValue, "variable", envVar, "value", env, "expected", "true/false/1/0/yes/no")
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
		fail(localization.MessageIDCLIErrorInvalidEnvironmentVariableValue, "variable", envVar, "value", env, "expected", "integer")
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
		fail(localization.MessageIDCLIErrorInvalidEnvironmentVariableValue, "variable", envVar, "value", env, "expected", "duration like 24h, 15m")
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
	if !opts.Transports.Stdio {
		return nil
	}
	if opts.LogTarget == leaflogging.TargetStdout {
		return newCLIRenderedMessageError(cliMessageID(localization.MessageIDCLIErrorStdoutReservedForMCPStdio))
	}
	hasAPIKey := strings.TrimSpace(opts.APIKey) != ""
	if opts.DisableAuth && hasAPIKey {
		return newCLIRenderedMessageError(cliMessageID(localization.MessageIDCLIErrorStdioAuthAPIKeyConflict))
	}
	if !opts.DisableAuth && !hasAPIKey {
		return newCLIRenderedMessageError(cliMessageID(localization.MessageIDCLIErrorStdioAuthIdentityRequired))
	}
	return nil
}

func parseByteSize(raw string, label string) int64 {
	size, err := humanize.ParseBytes(strings.TrimSpace(raw))
	if err != nil {
		fail(localization.MessageIDCLIErrorInvalidByteSizeValue, "setting", label, "value", raw, "error", err)
	}
	if size == 0 {
		fail(localization.MessageIDCLIErrorByteSizeMustBePositive, "setting", label, "value", raw)
	}
	if size > math.MaxInt64 {
		fail(localization.MessageIDCLIErrorByteSizeTooLarge, "setting", label, "value", raw)
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
		MaxAssetUploadSizeBytes: shared.MaxBytes(in.maxAssetUploadSize),
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
