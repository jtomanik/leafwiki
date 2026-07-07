package main

import (
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"bytes"
	"context"
	"flag"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/onsi/gomega/gstruct"
	"github.com/onsi/gomega/types"
	"github.com/perber/wiki/internal/agenthooks"
	"github.com/perber/wiki/internal/localization"
	"github.com/perber/wiki/internal/locking"
	leaflogging "github.com/perber/wiki/internal/logging"
	"github.com/perber/wiki/internal/projectdaemon"
	"github.com/perber/wiki/internal/runtimeconfig"
	"github.com/perber/wiki/internal/wikid"
)

var _ = ginkgo.Describe("visible legacy subcases", func() {
	ginkgo.DescribeTable("rejects removed startup flags",
		ginkgo.Label("unit"),
		func(arg string) {
			flagName := removedStartupFlagName(arg)
			fs := flag.NewFlagSet("leafwiki-test", flag.ContinueOnError)
			var errOut bytes.Buffer
			fs.SetOutput(&errOut)
			registerFlags(fs)

			_ = fs.Parse([]string{arg})
			Expect(fs.Lookup(flagName)).To(BeNil())
		},
		ginkgo.Entry("--enable-revision", "--enable-revision"),
		ginkgo.Entry("--enable-workspace-sync", "--enable-workspace-sync"),
		ginkgo.Entry("--enable-mcp", "--enable-mcp"),
		ginkgo.Entry("--mcp-stdio", "--mcp-stdio"),
		ginkgo.Entry("--max-revision-history=0", "--max-revision-history=0"),
	)

	ginkgo.DescribeTable("rejects removed runtime env",
		ginkgo.Label("unit"),
		func(name string) {
			leafwikiSetenv(name, "")

			err := rejectRemovedLeafWikiEnv()
			Expect(err).To(MatchError(removedEnvironmentVariableError{Name: name}))
		},
		ginkgo.Entry("rejects removed runtime stack selection", "LEAFWIKI_RUNTIME_STACK"),
		ginkgo.Entry("rejects removed revision enablement", "LEAFWIKI_ENABLE_REVISION"),
		ginkgo.Entry("rejects removed workspace sync enablement", "LEAFWIKI_ENABLE_WORKSPACE_SYNC"),
		ginkgo.Entry("rejects removed revision history limit", "LEAFWIKI_MAX_REVISION_HISTORY"),
		ginkgo.Entry("rejects removed public tool transport enablement", "LEAFWIKI_ENABLE_MCP"),
		ginkgo.Entry("rejects removed native tool transport enablement", "LEAFWIKI_MCP_STDIO"),
	)

	ginkgo.DescribeTable("caps grant by current user role",
		ginkgo.Label("unit"),
		func(userRole wikid.GrantRole, grant wikid.GrantRole, want wikid.GrantRole) {
			Expect(effectiveWorkspaceGrantRole(userRole, grant)).To(Equal(want))
		},
		ginkgo.Entry("viewer grant remains viewer", wikid.GrantRoleEditor, wikid.GrantRoleViewer, wikid.GrantRoleViewer),
		ginkgo.Entry("editor grant capped by downgraded viewer", wikid.GrantRoleViewer, wikid.GrantRoleEditor, wikid.GrantRoleViewer),
		ginkgo.Entry("admin user keeps editor grant", wikid.GrantRoleAdmin, wikid.GrantRoleEditor, wikid.GrantRoleEditor),
		ginkgo.Entry("unknown user role denies effective grant", newFixtureGrantRole(""), wikid.GrantRoleEditor, newFixtureGrantRole("")),
	)

	type configFileErrorCase struct {
		yaml   string
		reason runtimeconfig.ConfigFileErrorReason
		key    string
	}

	ginkgo.DescribeTable("rejects invalid keys and values",
		ginkgo.Label("unit"),
		func(tc configFileErrorCase) {
			configPath := filepath.Join(leafwikiTempDir(), "leafwiki.yml")
			writeTestConfig(configPath, tc.yaml)

			_, _, _, err := parseConfigFlagsForArgsAllowError([]string{"--config", configPath})

			Expect(err).To(MatchRuntimeConfigFileError(tc.reason, tc.key))
		},
		ginkgo.Entry("unknown key", configFileErrorCase{yaml: "unknown-option: true\n", reason: runtimeconfig.ConfigFileErrorReasonUnknownKey, key: "unknown-option"}),
		ginkgo.Entry("duplicate key", configFileErrorCase{yaml: "port: 8080\nport: 8081\n", reason: runtimeconfig.ConfigFileErrorReasonDuplicateKey, key: "port"}),
		ginkgo.Entry("non scalar value", configFileErrorCase{yaml: "trusted-proxy-ips:\n  - 127.0.0.1\n", reason: runtimeconfig.ConfigFileErrorReasonScalarValue, key: "trusted-proxy-ips"}),
		ginkgo.Entry("null value", configFileErrorCase{yaml: "base-path: null\n", reason: runtimeconfig.ConfigFileErrorReasonScalarValue, key: "base-path"}),
		ginkgo.Entry("hidden compatibility key", configFileErrorCase{yaml: "enable-mcp: true\n", reason: runtimeconfig.ConfigFileErrorReasonUnknownKey, key: "enable-mcp"}),
		ginkgo.Entry("removed revision key", configFileErrorCase{yaml: "enable-revision: true\n", reason: runtimeconfig.ConfigFileErrorReasonUnknownKey, key: "enable-revision"}),
		ginkgo.Entry("removed workspace sync key", configFileErrorCase{yaml: "enable-workspace-sync: true\n", reason: runtimeconfig.ConfigFileErrorReasonUnknownKey, key: "enable-workspace-sync"}),
		ginkgo.Entry("removed revision limit key", configFileErrorCase{yaml: "max-revision-history: 0\n", reason: runtimeconfig.ConfigFileErrorReasonUnknownKey, key: "max-revision-history"}),
		ginkgo.Entry("internal key", configFileErrorCase{yaml: "internal-project-daemon: /tmp/startup.json\n", reason: runtimeconfig.ConfigFileErrorReasonUnknownKey, key: "internal-project-daemon"}),
		ginkgo.Entry("config key", configFileErrorCase{yaml: "config: other.yml\n", reason: runtimeconfig.ConfigFileErrorReasonUnknownKey, key: "config"}),
		ginkgo.Entry("mcp stdio compatibility key", configFileErrorCase{yaml: "mcp-stdio: true\n", reason: runtimeconfig.ConfigFileErrorReasonUnknownKey, key: "mcp-stdio"}),
		ginkgo.Entry("bad bool scalar", configFileErrorCase{yaml: "public-access: maybe\n", reason: runtimeconfig.ConfigFileErrorReasonInvalidFlagValue, key: "public-access"}),
		ginkgo.Entry("bad duration scalar", configFileErrorCase{yaml: "access-token-timeout: soon\n", reason: runtimeconfig.ConfigFileErrorReasonInvalidFlagValue, key: "access-token-timeout"}),
	)

	ginkgo.DescribeTable("rejects config mixed with subcommand trailing CLI flag",
		ginkgo.Label("unit"),
		func(args []string, wantErr error) {
			configPath := filepath.Join(leafwikiTempDir(), "leafwiki.yml")
			writeTestConfig(configPath, "data-dir: ./data\n")
			for i, arg := range args {
				if arg == "$CONFIG" {
					args[i] = configPath
				}
			}

			_, _, _, err := parseConfigFlagsForArgsAllowError(args)
			Expect(err).To(MatchError(wantErr))
		},
		ginkgo.Entry("reset password trailing flag", []string{"--config", "$CONFIG", "reset-admin-password", "--data-dir", "other"}, runtimeconfig.ConfigFlagMixError{Flag: "--data-dir"}),
		ginkgo.Entry("agent hook trailing flag", []string{"--config", "$CONFIG", "agent-hook", "codex", "--data-dir", "other"}, runtimeconfig.ConfigFlagMixError{Flag: "--data-dir"}),
	)

	ginkgo.DescribeTable("config agent hook rejects empty config path without fail open",
		ginkgo.Label("e2e"),
		func(args []string) {
			payload := `{"hook_event_name":"SessionStart","session_id":"empty-config-secret"}`

			stdout, stderr, err := runLeafwikiHelperWithInputAndTimeout(args, nil, payload, 5*time.Second)

			expectAgentHookConfigRejection(args)
			Expect(err).To(MatchProcessExitError())
			Expect(stdout).NotTo(MatchCodexAgentHookAllowResponse())
			Expect(readJSONLogEntriesFromText(stderr)).NotTo(ContainElement(HaveKey("session_id")))
		},
		ginkgo.Entry("inline empty", []string{"--config=", "agent-hook", "codex"}),
		ginkgo.Entry("separate empty", []string{"--config", "", "agent-hook", "codex"}),
		ginkgo.Entry("trailing bare after agent hook", []string{"agent-hook", "codex", "--config"}),
		ginkgo.Entry("inline empty before help after agent hook", []string{"agent-hook", "codex", "--config=", "--help"}),
		ginkgo.Entry("single dash", []string{"--config", "-", "agent-hook", "codex"}),
		ginkgo.Entry("double dash", []string{"--config", "--", "agent-hook", "codex"}),
	)

	ginkgo.DescribeTable("agent hook provider allow responses fail open",
		ginkgo.Label("e2e"),
		func(provider agenthooks.ProviderID, payload string, wantStdout types.GomegaMatcher) {
			baseDir := leafwikiTempDir()
			stdout, stderr, err := runLeafwikiHelperWithInputAndTimeout([]string{
				"agent-hook", agentHookProviderCLIArg(provider),
				"--disable-auth",
				"--data-dir", filepath.Join(baseDir, "data"),
				"--root-dir", filepath.Join(baseDir, "content"),
				"--host", "127.0.0.1",
				"--port", freeTCPPort(),
				"--log-target", "stderr",
			}, nil, payload, 5*time.Second)

			Expect(err).NotTo(HaveOccurred())
			Expect(stdout).To(wantStdout)
			Expect(readJSONLogEntriesFromText(stderr)).NotTo(ContainElement(HaveKey("session_id")))
		},
		ginkgo.Entry("claude malformed", agenthooks.ProviderClaude, "{", MatchAgentHookAllowResponse(agenthooks.ProviderClaude)),
		ginkgo.Entry("cursor malformed", agenthooks.ProviderCursor, "{", MatchAgentHookAllowResponse(agenthooks.ProviderCursor)),
		ginkgo.Entry("unknown provider", agenthooks.ProviderUnknown, `{"hook_event_name":"SessionStart","session_id":"unknown-secret"}`, BeEmpty()),
	)

	ginkgo.DescribeTable("control record failures fail open",
		ginkgo.Label("integration"),
		func(recordHandler func(http.ResponseWriter, *http.Request), parentTimeout time.Duration, wantErr types.GomegaMatcher) {
			cfg, cleanup := testRuntimeConfigWithHealthyControlDescriptor(recordHandler)
			defer cleanup()
			ctx := context.Background()
			if parentTimeout > 0 {
				var cancel context.CancelFunc
				ctx, cancel = context.WithTimeout(ctx, parentTimeout)
				defer cancel()
			}

			var stdout bytes.Buffer
			err := runAgentHookCommand(ctx, cfg, agenthooks.ProviderCodex, strings.NewReader(`{"hook_event_name":"SessionStart","session_id":"control-secret"}`), &stdout)

			Expect(stdout.String()).To(MatchCodexAgentHookAllowResponse())
			Expect(err).To(SatisfyAny(wantErr, MatchError(errProjectLockedNoAttachableDaemon)))
		},
		ginkgo.Entry("control 401", func(w http.ResponseWriter, _ *http.Request) { http.Error(w, "unauthorized", http.StatusUnauthorized) }, time.Duration(0), MatchProjectDaemonControlStatus(http.StatusUnauthorized)),
		ginkgo.Entry("control 400", func(w http.ResponseWriter, _ *http.Request) { http.Error(w, "bad event", http.StatusBadRequest) }, time.Duration(0), MatchProjectDaemonControlStatus(http.StatusBadRequest)),
		ginkgo.Entry("control 500", func(w http.ResponseWriter, _ *http.Request) { http.Error(w, "boom", http.StatusInternalServerError) }, time.Duration(0), MatchProjectDaemonControlStatus(http.StatusInternalServerError)),
		ginkgo.Entry("control timeout", func(w http.ResponseWriter, _ *http.Request) {
			time.Sleep(250 * time.Millisecond)
			w.WriteHeader(http.StatusNoContent)
		}, 50*time.Millisecond, MatchError(context.DeadlineExceeded)),
	)

	ginkgo.DescribeTable("removed revision and workspace sync flags fail unknown",
		ginkgo.Label("e2e"),
		func(removedFlag string) {
			baseDir := leafwikiTempDir()
			stdout, stderr, err := runLeafwikiHelperWithTimeout([]string{
				"--disable-auth",
				removedFlag,
				"--data-dir", filepath.Join(baseDir, "data"),
				"--root-dir", filepath.Join(baseDir, "content"),
				"--host", "127.0.0.1",
				"--port", freeTCPPort(),
				"--log-target", "stderr",
			}, nil, 5*time.Second)

			Expect(err).To(MatchProcessExitError(), "stdout:\n%s\nstderr:\n%s", stdout, stderr)
			Expect(err).NotTo(MatchError(context.DeadlineExceeded))
			Expect(stdout).To(BeEmpty())
		},
		ginkgo.Entry("--enable-revision", "--enable-revision"),
		ginkgo.Entry("--enable-workspace-sync", "--enable-workspace-sync"),
	)

	ginkgo.DescribeTable("legacy MCP flags fail unknown",
		ginkgo.Label("e2e"),
		func(removedFlag string) {
			stdout, stderr, err := runLeafwikiHelperWithTimeout([]string{
				removedFlag,
				"--disable-auth",
				"--data-dir", filepath.Join(leafwikiTempDir(), "data"),
				"--root-dir", filepath.Join(leafwikiTempDir(), "content"),
				"--host", "127.0.0.1",
				"--port", freeTCPPort(),
				"--log-target", "stderr",
			}, nil, 5*time.Second)

			Expect(err).To(MatchProcessExitError(), "stdout:\n%s\nstderr:\n%s", stdout, stderr)
			Expect(stdout).To(BeEmpty())
		},
		ginkgo.Entry("--enable-mcp", "--enable-mcp"),
		ginkgo.Entry("--mcp-stdio", "--mcp-stdio"),
	)

	ginkgo.DescribeTable("daemon relevant descriptor fields",
		ginkgo.Label("unit"),
		func(field string, mut func(*projectdaemon.Config)) {
			owner := completeDaemonCompareConfig()
			requested := owner
			mut(&requested)

			mismatches := compareProjectDaemonConfigForRequest(owner, requested, mcpTransports{HTTP: true})

			Expect(mismatches).To(ConsistOf(HaveField("Field", Equal(field))))
		},
		ginkgo.Entry("data dir", "data-dir", func(cfg *projectdaemon.Config) { cfg.DataDir = "/tmp/other-data" }),
		ginkgo.Entry("root dir", "root-dir", func(cfg *projectdaemon.Config) { cfg.RootDir = "/tmp/other-root" }),
		ginkgo.Entry("auth mode", "auth-disabled", func(cfg *projectdaemon.Config) { cfg.AuthDisabled = !cfg.AuthDisabled }),
		ginkgo.Entry("public MCP", "public-mcp-enabled", func(cfg *projectdaemon.Config) { cfg.PublicMCPEnabled = !cfg.PublicMCPEnabled }),
		ginkgo.Entry("host", "host", func(cfg *projectdaemon.Config) { cfg.Host = "127.0.0.2" }),
		ginkgo.Entry("port", "port", func(cfg *projectdaemon.Config) { cfg.Port = "9090" }),
		ginkgo.Entry("base path", "base-path", func(cfg *projectdaemon.Config) { cfg.BasePath = "/docs" }),
		ginkgo.Entry("markdown link root prefix", "markdown-link-root-prefix", func(cfg *projectdaemon.Config) { cfg.MarkdownLinkRootPrefix = "/docs" }),
		ginkgo.Entry("public access", "public-access", func(cfg *projectdaemon.Config) { cfg.PublicAccess = !cfg.PublicAccess }),
		ginkgo.Entry("allow insecure", "allow-insecure", func(cfg *projectdaemon.Config) { cfg.AllowInsecure = !cfg.AllowInsecure }),
		ginkgo.Entry("access token timeout", "access-token-timeout", func(cfg *projectdaemon.Config) { cfg.AccessTokenTimeout = "2h0m0s" }),
		ginkgo.Entry("refresh token timeout", "refresh-token-timeout", func(cfg *projectdaemon.Config) { cfg.RefreshTokenTimeout = "720h0m0s" }),
		ginkgo.Entry("injected header hash", "inject-code-in-header-hash", func(cfg *projectdaemon.Config) { cfg.InjectCodeInHeaderHash = "other-hash" }),
		ginkgo.Entry("custom stylesheet", "custom-stylesheet", func(cfg *projectdaemon.Config) { cfg.CustomStylesheet = "/tmp/custom.css" }),
		ginkgo.Entry("log target", "log-target", func(cfg *projectdaemon.Config) { cfg.LogTarget = "file" }),
		ginkgo.Entry("log file", "log-file", func(cfg *projectdaemon.Config) { cfg.LogFile = "/tmp/leafwiki.log" }),
		ginkgo.Entry("hide metadata", "hide-link-metadata-section", func(cfg *projectdaemon.Config) { cfg.HideLinkMetadataSection = !cfg.HideLinkMetadataSection }),
		ginkgo.Entry("upload size", "max-asset-upload-size-bytes", func(cfg *projectdaemon.Config) { cfg.MaxAssetUploadSizeBytes = 99 }),
		ginkgo.Entry("link refactor", "enable-link-refactor", func(cfg *projectdaemon.Config) { cfg.EnableLinkRefactor = !cfg.EnableLinkRefactor }),
		ginkgo.Entry("remote user enabled", "enable-http-remote-user", func(cfg *projectdaemon.Config) { cfg.EnableHTTPRemoteUser = !cfg.EnableHTTPRemoteUser }),
		ginkgo.Entry("remote user header", "http-remote-user-header", func(cfg *projectdaemon.Config) { cfg.HTTPRemoteUserHeader = "X-User" }),
		ginkgo.Entry("trusted proxies", "trusted-proxy-ips", func(cfg *projectdaemon.Config) { cfg.TrustedProxyIPs = "127.0.0.1/32" }),
		ginkgo.Entry("remote user logout", "http-remote-user-logout-url", func(cfg *projectdaemon.Config) { cfg.HTTPRemoteUserLogoutURL = "https://example.test/logout" }),
		ginkgo.Entry("request log", "disable-request-log", func(cfg *projectdaemon.Config) { cfg.DisableRequestLog = !cfg.DisableRequestLog }),
		ginkgo.Entry("idle timeout", "daemon-idle-timeout", func(cfg *projectdaemon.Config) { cfg.DaemonIdleTimeout = "1m0s" }),
	)

	ginkgo.DescribeTable("default env CLI and selector",
		ginkgo.Label("unit"),
		func(args []string, envValue *string, want mcpTransports) {
			if envValue != nil {
				leafwikiSetenv("LEAFWIKI_MCP", *envValue)
			}

			got, err := resolveMCPTransportsForArgs(args)

			Expect(err).NotTo(HaveOccurred())
			Expect(got).To(Equal(want))
		},
		ginkgo.Entry("default none", []string(nil), (*string)(nil), mcpTransports{}),
		ginkgo.Entry("env enables http", []string(nil), stringPtr("http"), mcpTransports{HTTP: true}),
		ginkgo.Entry("cli overrides env", []string{"--mcp=stdio"}, stringPtr("http"), mcpTransports{Stdio: true}),
		ginkgo.Entry("combined orderings stdio,http", []string{"--mcp=stdio,http"}, (*string)(nil), mcpTransports{HTTP: true, Stdio: true}),
		ginkgo.Entry("combined orderings http,stdio", []string{"--mcp=http,stdio"}, (*string)(nil), mcpTransports{HTTP: true, Stdio: true}),
		ginkgo.Entry("selector ignores removed legacy envs after env validation", []string{"--mcp=none"}, (*string)(nil), mcpTransports{}),
	)

	ginkgo.DescribeTable("rejects invalid values",
		ginkgo.Label("unit"),
		func(raw string, reason runtimeconfig.MCPTransportErrorReason) {
			_, err := parseMCPTransports(raw)
			Expect(err).To(MatchMCPTransportError(reason))
		},
		ginkgo.Entry("unknown", "websocket", runtimeconfig.MCPTransportErrorReasonInvalid),
		ginkgo.Entry("none combined", "none,stdio", runtimeconfig.MCPTransportErrorReasonNoneMixed),
		ginkgo.Entry("duplicate", "stdio,stdio", runtimeconfig.MCPTransportErrorReasonDuplicate),
		ginkgo.Entry("empty part", "stdio,", runtimeconfig.MCPTransportErrorReasonInvalid),
	)

	type mcpTransportOptionCase struct {
		opts      mcpTransportOptions
		messageID cliMessageID
	}

	ginkgo.DescribeTable("accepts compatible transport settings and rejects invalid STDIO authentication combinations",
		ginkgo.Label("unit"),
		func(tc mcpTransportOptionCase) {
			err := validateMCPTransportOptions(tc.opts)
			if tc.messageID == "" {
				Expect(err).NotTo(HaveOccurred())
				return
			}
			Expect(err).To(MatchCLIRenderedMessageError(tc.messageID))
		},
		ginkgo.Entry("HTTP allows non-loopback web host", mcpTransportOptionCase{opts: mcpTransportOptions{Transports: mcpTransports{HTTP: true}, Host: "0.0.0.0", LogTarget: leaflogging.TargetStderr}}),
		ginkgo.Entry("STDIO allows non-loopback web host", mcpTransportOptionCase{opts: mcpTransportOptions{Transports: mcpTransports{Stdio: true}, DisableAuth: true, Host: "0.0.0.0", LogTarget: leaflogging.TargetStderr}}),
		ginkgo.Entry("STDIO rejects stdout logging", mcpTransportOptionCase{opts: mcpTransportOptions{Transports: mcpTransports{Stdio: true}, DisableAuth: true, Host: "127.0.0.1", LogTarget: leaflogging.TargetStdout}, messageID: cliMessageID(localization.MessageIDCLIErrorStdoutReservedForMCPStdio)}),
		ginkgo.Entry("STDIO auth enabled requires key", mcpTransportOptionCase{opts: mcpTransportOptions{Transports: mcpTransports{Stdio: true}, Host: "127.0.0.1", LogTarget: leaflogging.TargetStderr}, messageID: cliMessageID(localization.MessageIDCLIErrorStdioAuthIdentityRequired)}),
		ginkgo.Entry("STDIO disabled auth rejects key", mcpTransportOptionCase{opts: mcpTransportOptions{Transports: mcpTransports{Stdio: true}, DisableAuth: true, APIKey: "lwk_fake", Host: "127.0.0.1", LogTarget: leaflogging.TargetStderr}, messageID: cliMessageID(localization.MessageIDCLIErrorStdioAuthAPIKeyConflict)}),
		ginkgo.Entry("HTTP ignores API key", mcpTransportOptionCase{opts: mcpTransportOptions{Transports: mcpTransports{HTTP: true}, APIKey: "lwk_invalid", Host: "127.0.0.1", LogTarget: leaflogging.TargetStderr}}),
	)

	ginkgo.DescribeTable("requires trusted proxy IPs only when remote-user auth is enabled",
		ginkgo.Label("unit"),
		func(enabled bool, trustedProxyIPs string, wantErr bool) {
			err := validateHTTPRemoteUserConfig(enabled, trustedProxyIPs)
			Expect(err != nil).To(Equal(wantErr))
		},
		ginkgo.Entry("disabled, no IPs", false, "", false),
		ginkgo.Entry("disabled, with IPs", false, "127.0.0.1", false),
		ginkgo.Entry("enabled, with IPs", true, "127.0.0.1", false),
		ginkgo.Entry("enabled, multiple IPs", true, "127.0.0.1,172.18.0.0/16", false),
		ginkgo.Entry("enabled, no IPs", true, "", true),
		ginkgo.Entry("enabled, whitespace only", true, "   ", true),
		ginkgo.Entry("enabled, commas only", true, ",,,", true),
		ginkgo.Entry("enabled, commas and whitespace", true, " , , ", true),
	)

	ginkgo.DescribeTable("preserves untrusted descriptor when any project lock is held",
		ginkgo.Label("integration"),
		func(lock func(dataDir string, rootDir string) func()) {
			baseDir := leafwikiTempDir()
			dataDir := filepath.Join(baseDir, "data")
			rootDir := filepath.Join(baseDir, "content")
			Expect(os.MkdirAll(filepath.Join(dataDir, ".leafwiki"), 0o755)).To(Succeed())
			Expect(os.MkdirAll(rootDir, 0o755)).To(Succeed())
			canonicalData, canonicalRoot, err := projectdaemon.CanonicalizeProject(dataDir, rootDir)
			Expect(err).NotTo(HaveOccurred())
			descriptorPath := projectdaemon.DescriptorPath(canonicalData)
			Expect(os.WriteFile(descriptorPath, []byte("{"), 0o600)).To(Succeed())
			release := lock(canonicalData, canonicalRoot)
			defer release()

			Expect(readHealthyProjectDaemonLockResult(context.Background(), descriptorPath, projectdaemon.Config{DataDir: canonicalData, RootDir: canonicalRoot})).To(MatchProjectDaemonDescriptorPreservedByLock())
			_, statErr := os.Stat(descriptorPath)
			Expect(statErr).NotTo(HaveOccurred())
		},
		ginkgo.Entry("data lock held", func(dataDir string, _ string) func() {
			ginkgo.GinkgoHelper()
			lock, err := locking.AcquireDataDirLock(dataDir)
			Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("acquire data lock: %v", err))

			return func() { _ = lock.Release() }
		}),
		ginkgo.Entry("root lock held", func(_ string, rootDir string) func() {
			ginkgo.GinkgoHelper()
			lock, err := locking.AcquireRootDirLock(rootDir)
			Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("acquire root lock: %v", err))

			return func() { _ = lock.Release() }
		}),
	)

	ginkgo.DescribeTable("untrusted stale descriptor is replaced when locks are free",
		ginkgo.Label("e2e"),
		func(setup func(path string)) {
			baseDir := leafwikiTempDir()
			dataDir := filepath.Join(baseDir, "data")
			rootDir := filepath.Join(baseDir, "content")
			descriptorPath := filepath.Join(dataDir, ".leafwiki", projectdaemon.DescriptorFileName)
			Expect(os.MkdirAll(filepath.Dir(descriptorPath), 0o755)).To(Succeed())
			Expect(os.MkdirAll(rootDir, 0o755)).To(Succeed())
			setup(descriptorPath)

			port := freeTCPPort()
			proc := startLeafwikiHelper([]string{
				"--disable-auth",
				"--data-dir", dataDir,
				"--root-dir", rootDir,
				"--host", "127.0.0.1",
				"--port", port,
				"--log-target", "stderr",
			}, nil)
			waitForLeafwikiReady(proc, port)

			desc := waitForProjectDaemonDescriptor(dataDir)
			Expect(desc).To(gstruct.PointTo(gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
				"PID":        Not(BeZero()),
				"ControlURL": Not(BeEmpty()),
			})))
			info, err := os.Stat(descriptorPath)
			Expect(err).NotTo(HaveOccurred())
			Expect(info.Mode().Perm()).To(Equal(os.FileMode(0o600)))
		},
		ginkgo.Entry("missing", func(string) {}),
		ginkgo.Entry("corrupt", func(path string) {
			ginkgo.GinkgoHelper()
			Expect(os.WriteFile(path, []byte("{"), 0o600)).To(Succeed())
		}),
		ginkgo.Entry("wrong mode", func(path string) {
			ginkgo.GinkgoHelper()
			Expect(os.WriteFile(path, []byte("{}"), 0o644)).To(Succeed())
		}),
		ginkgo.Entry("non regular path", func(path string) {
			ginkgo.GinkgoHelper()
			Expect(os.Mkdir(path, 0o700)).To(Succeed())
		}),
	)
})

var _ = ginkgo.Describe("cmd leafwiki helper contracts", func() {
	ginkgo.DescribeTable("renders config flag names for mixed configuration errors",
		ginkgo.Label("unit"),
		func(arg string, name string, want string) {
			Expect(configModeFlagDisplay(arg, name)).To(Equal(want))
		},
		ginkgo.Entry("long flag", "--config", "config", "--config"),
		ginkgo.Entry("single dash flag", "-config", "config", "-config"),
		ginkgo.Entry("inline value", "--config=leafwiki.yml", "config", "--config"),
	)

	ginkgo.DescribeTable("rejects bare config path placeholders",
		ginkgo.Label("unit"),
		func(value string, want bool) {
			Expect(isInvalidBareConfigPathValue(value)).To(Equal(want))
		},
		ginkgo.Entry("empty", "", true),
		ginkgo.Entry("blank", "   ", true),
		ginkgo.Entry("dash", "-", true),
		ginkgo.Entry("double dash", "--", true),
		ginkgo.Entry("valid path", "leafwiki.yml", false),
	)
})

func stringPtr(value string) *string {
	return &value
}
