package main

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/gstruct"
	"github.com/onsi/gomega/types"

	"github.com/perber/wiki/internal/agenthooks"
	shared "github.com/perber/wiki/internal/core/shared"
	httpinternal "github.com/perber/wiki/internal/http"
	leaflogging "github.com/perber/wiki/internal/logging"
	"github.com/perber/wiki/internal/projectdaemon"
	"github.com/perber/wiki/internal/wiki"
	"github.com/perber/wiki/internal/wikid"
	"github.com/perber/wiki/internal/workspaceid"
)

type cliFlagPresence uint8

const (
	cliFlagAbsent cliFlagPresence = iota
	cliFlagPresent
)

func observeRawFlag(args []string, flagName string) cliFlagPresence {
	if rawArgsContainFlag(args, flagName) {
		return cliFlagPresent
	}
	return cliFlagAbsent
}

type daemonHelpCommandClassification uint8

const (
	daemonRuntimeCommand daemonHelpCommandClassification = iota
	daemonHelpCommand
)

func classifyDaemonHelpCommand(args []string) daemonHelpCommandClassification {
	if isDaemonHelpCommand(args) {
		return daemonHelpCommand
	}
	return daemonRuntimeCommand
}

type runtimeProcessLivenessState uint8

const (
	runtimeProcessNotRunning runtimeProcessLivenessState = iota
	runtimeProcessRunning
)

func observeProcessLiveness(pid int) runtimeProcessLivenessState {
	if processPIDAlive(pid) {
		return runtimeProcessRunning
	}
	return runtimeProcessNotRunning
}

type daemonAuthPolicy uint8

const (
	daemonAuthRequired daemonAuthPolicy = iota
	daemonAuthDisabled
)

type daemonPublicMCPPolicy uint8

const (
	daemonPrivateMCPOnly daemonPublicMCPPolicy = iota
	daemonPublicMCPEnabled
)

type daemonWorkspaceSyncPolicy uint8

const (
	daemonWorkspaceSyncDisabled daemonWorkspaceSyncPolicy = iota
	daemonWorkspaceSyncEnabled
)

type runtimeRoleNameClassification uint8

const (
	runtimeRoleUnsupported runtimeRoleNameClassification = iota
	runtimeRoleWikid
	runtimeRoleFrontd
	runtimeRoleWorkspaced
)

func classifyRuntimeRoleName(role projectdaemon.RoleName, ok bool) runtimeRoleNameClassification {
	if !ok {
		return runtimeRoleUnsupported
	}
	switch role {
	case projectdaemon.RoleWikid:
		return runtimeRoleWikid
	case projectdaemon.RoleFrontd:
		return runtimeRoleFrontd
	case projectdaemon.RoleWorkspaced:
		return runtimeRoleWorkspaced
	default:
		return runtimeRoleUnsupported
	}
}

type daemonRuntimeCapabilities struct {
	auth          daemonAuthPolicy
	publicMCP     daemonPublicMCPPolicy
	workspaceSync daemonWorkspaceSyncPolicy
}

func observeDaemonRuntimeCapabilities(cfg projectdaemon.Config) daemonRuntimeCapabilities {
	capabilities := daemonRuntimeCapabilities{
		auth:          daemonAuthRequired,
		publicMCP:     daemonPrivateMCPOnly,
		workspaceSync: daemonWorkspaceSyncDisabled,
	}
	if cfg.AuthDisabled {
		capabilities.auth = daemonAuthDisabled
	}
	if cfg.PublicMCPEnabled {
		capabilities.publicMCP = daemonPublicMCPEnabled
	}
	if cfg.EnableWorkspaceSync {
		capabilities.workspaceSync = daemonWorkspaceSyncEnabled
	}
	return capabilities
}

func ExposeDaemonRuntimeCapabilities(want daemonRuntimeCapabilities) types.GomegaMatcher {
	ginkgo.GinkgoHelper()
	return WithTransform(observeDaemonRuntimeCapabilities, Equal(want))
}

type workspaceRuntimeConfigObservation struct {
	workspaceID            workspaceid.WorkspaceID
	dataDir                string
	rootDir                string
	markdownLinkRootPrefix string
	host                   string
	port                   string
	workspaceSync          daemonWorkspaceSyncPolicy
}

func observeWorkspaceRuntimeConfig(cfg leafwikiRuntimeConfig) workspaceRuntimeConfigObservation {
	observation := workspaceRuntimeConfigObservation{
		workspaceID:            cfg.Workspace.ID,
		dataDir:                cfg.Workspace.DataDir,
		rootDir:                cfg.Workspace.RootDir,
		markdownLinkRootPrefix: cfg.MarkdownLinkRootPrefix,
		host:                   cfg.Host,
		port:                   cfg.Port,
		workspaceSync:          daemonWorkspaceSyncDisabled,
	}
	if cfg.EnableWorkspaceSync {
		observation.workspaceSync = daemonWorkspaceSyncEnabled
	}
	return observation
}

func MatchWorkspaceRuntimeConfig(want workspaceRuntimeConfigObservation) types.GomegaMatcher {
	ginkgo.GinkgoHelper()
	return WithTransform(observeWorkspaceRuntimeConfig, Equal(want))
}

type runtimeRoleStartupConfigObservation struct {
	role        projectdaemon.RoleName
	daemonToken string
}

func observeRuntimeRoleStartupConfig(startup internalRuntimeRoleStartupConfig) runtimeRoleStartupConfigObservation {
	return runtimeRoleStartupConfigObservation{
		role:        startup.Role,
		daemonToken: startup.DaemonToken,
	}
}

func MatchRuntimeRoleStartupConfig(want runtimeRoleStartupConfigObservation) types.GomegaMatcher {
	ginkgo.GinkgoHelper()
	return WithTransform(observeRuntimeRoleStartupConfig, Equal(want))
}

type startupParseState uint8

const (
	startupUsageDisplayed startupParseState = iota
	startupRuntimeRequested
)

type startupServiceMode uint8

const (
	startupDirectRuntime startupServiceMode = iota
	startupDaemonServiceRuntime
)

type startupParseObservation struct {
	state       startupParseState
	serviceMode startupServiceMode
}

func parseStartupForObservation(args []string) (startupCLI, startupParseObservation) {
	startup, ok := parseStartupCLI(args)
	observation := startupParseObservation{state: startupUsageDisplayed, serviceMode: startupDirectRuntime}
	if ok {
		observation.state = startupRuntimeRequested
	}
	if startup.serviceModeRequested {
		observation.serviceMode = startupDaemonServiceRuntime
	}
	return startup, observation
}

type runtimePublicAccessPolicy uint8

const (
	runtimeAuthenticatedAccess runtimePublicAccessPolicy = iota
	runtimePublicAccess
)

type runtimeMCPExposure uint8

const (
	runtimeMCPDisabled runtimeMCPExposure = iota
	runtimeHTTPMCPEnabled
	runtimeStdioMCPEnabled
	runtimeHTTPAndStdioMCPEnabled
)

type runtimeIdleShutdownPolicy uint8

const (
	runtimeIdleShutdownAllowed runtimeIdleShutdownPolicy = iota
	runtimeIdleShutdownDisabled
)

type startupRuntimeCapabilities struct {
	auth         daemonAuthPolicy
	publicAccess runtimePublicAccessPolicy
	mcp          runtimeMCPExposure
	idleShutdown runtimeIdleShutdownPolicy
}

func observeStartupRuntimeCapabilities(cfg leafwikiRuntimeConfig) startupRuntimeCapabilities {
	capabilities := startupRuntimeCapabilities{
		auth:         daemonAuthRequired,
		publicAccess: runtimeAuthenticatedAccess,
		mcp:          runtimeMCPDisabled,
		idleShutdown: runtimeIdleShutdownAllowed,
	}
	if cfg.DisableAuth {
		capabilities.auth = daemonAuthDisabled
	}
	if cfg.PublicAccess {
		capabilities.publicAccess = runtimePublicAccess
	}
	switch {
	case cfg.MCPTransports.HTTP && cfg.MCPTransports.Stdio:
		capabilities.mcp = runtimeHTTPAndStdioMCPEnabled
	case cfg.MCPTransports.HTTP:
		capabilities.mcp = runtimeHTTPMCPEnabled
	case cfg.MCPTransports.Stdio:
		capabilities.mcp = runtimeStdioMCPEnabled
	}
	if cfg.DisableIdleShutdown {
		capabilities.idleShutdown = runtimeIdleShutdownDisabled
	}
	return capabilities
}

func ExposeStartupRuntimeCapabilities(want startupRuntimeCapabilities) types.GomegaMatcher {
	ginkgo.GinkgoHelper()
	return WithTransform(observeStartupRuntimeCapabilities, Equal(want))
}

func MatchHTTPRouterOptions(in httpRouterOptionsInput) types.GomegaMatcher {
	ginkgo.GinkgoHelper()
	return gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
		"PublicAccess":            Equal(in.publicAccess),
		"InjectCodeInHeader":      Equal(in.injectCodeInHeader),
		"CustomStylesheet":        Equal(in.customStylesheet),
		"AllowInsecure":           Equal(in.allowInsecure),
		"HideLinkMetadataSection": Equal(in.hideLinkMetadataSection),
		"AccessTokenTimeout":      Equal(in.accessTokenTimeout),
		"RefreshTokenTimeout":     Equal(in.refreshTokenTimeout),
		"AuthDisabled":            Equal(in.authDisabled),
		"BasePath":                Equal(in.basePath),
		"MarkdownLinkRootPrefix":  Equal(in.markdownLinkRootPrefix),
		"MaxAssetUploadSizeBytes": Equal(shared.MaxBytes(in.maxAssetUploadSize)),
		"EnableWorkspaceSync":     Equal(in.enableWorkspaceSync),
		"EnableLinkRefactor":      Equal(in.enableLinkRefactor),
		"MCPEnabled":              Equal(in.enableMCP),
		"MCPBindHost":             Equal(in.host),
		"MCPToolListPageSize":     Equal(in.mcpToolListPageSize),
		"HTTPRemoteUser":          Equal(in.httpRemoteUser),
		"DisableRequestLog":       Equal(in.disableRequestLog),
	})
}

var _ = ginkgo.Describe("startup argument normalization", ginkgo.Label("unit"), func() {
	ginkgo.It("keeps agent-hook provider identity after flag parsing reorders arguments", func() {
		Expect(normalizeAgentHookRawArgs([]string{"agent-hook", "codex", "--config", "leafwiki.yml"})).To(Equal([]string{"--config", "leafwiki.yml", "agent-hook", "codex"}))
		Expect(normalizeAgentHookRawArgs([]string{"daemon", "--help"})).To(Equal([]string{"daemon", "--help"}))
		provider, err := agentHookProviderFromRawArgsResult([]string{"--config", "leafwiki.yml", "agent-hook", "cursor"})
		Expect(err).To(Succeed())
		Expect(provider).To(Equal(agenthooks.ProviderIDFromString("cursor")))
	})

	ginkgo.It("detects flags without treating their values as commands", func() {
		Expect(observeRawFlag([]string{"--config", "leafwiki.yml", "agent-hook", "codex"}, "config")).To(Equal(cliFlagPresent))
		Expect(observeRawFlag([]string{"--config", "agent-hook", "codex"}, "daemon")).To(Equal(cliFlagAbsent))
		Expect(classifyDaemonHelpCommand([]string{"daemon", "--help"})).To(Equal(daemonHelpCommand))
		Expect(classifyDaemonHelpCommand([]string{"daemon"})).To(Equal(daemonRuntimeCommand))
	})

	ginkgo.It("classifies help and direct startup requests before runtime launch", func() {
		_, helpObservation := parseStartupForObservation([]string{"help"})
		Expect(helpObservation).To(Equal(startupParseObservation{state: startupUsageDisplayed, serviceMode: startupDirectRuntime}))

		directStartup, directObservation := parseStartupForObservation([]string{"--disable-auth", "--mcp", "http"})
		Expect(directObservation).To(Equal(startupParseObservation{state: startupRuntimeRequested, serviceMode: startupDirectRuntime}))
		Expect(directStartup.args).To(BeEmpty())
	})
})

var _ = ginkgo.Describe("startup configuration helpers", ginkgo.Label("unit"), func() {
	ginkgo.It("normalizes base paths and native STDIO HTTP URLs", func() {
		Expect(normalizeBasePath("")).To(BeEmpty())
		Expect(normalizeBasePath(" /wiki/ ")).To(Equal("/wiki"))
		Expect(nativeStdioHTTPURL("127.0.0.1", "8080", "/wiki")).To(Equal("http://127.0.0.1:8080/wiki"))
	})

	ginkgo.It("maps startup router input into HTTP router options", func() {
		input := httpRouterOptionsInput{
			publicAccess:            true,
			injectCodeInHeader:      "<meta>",
			customStylesheet:        "custom.css",
			allowInsecure:           true,
			hideLinkMetadataSection: true,
			accessTokenTimeout:      time.Minute,
			refreshTokenTimeout:     time.Hour,
			authDisabled:            true,
			basePath:                "/wiki",
			markdownLinkRootPrefix:  "/docs",
			maxAssetUploadSize:      1024,
			enableWorkspaceSync:     true,
			enableLinkRefactor:      true,
			enableMCP:               true,
			host:                    "127.0.0.1",
			mcpToolListPageSize:     25,
			httpRemoteUser:          httpinternal.HTTPRemoteUserConfig{Enabled: true, HeaderName: "Remote-User"},
			disableRequestLog:       true,
		}

		Expect(buildHTTPRouterOptions(input)).To(MatchHTTPRouterOptions(input))
	})

	ginkgo.It("validates auth startup requirements without opening runtime services", func() {
		Expect(validateAuthStartupConfig(leafwikiRuntimeConfig{DisableAuth: true})).To(Succeed())
		Expect(validateAuthStartupConfig(leafwikiRuntimeConfig{})).To(MatchError(errAuthJWTSecretRequired))
		Expect(validateAuthStartupConfig(leafwikiRuntimeConfig{JWTSecret: "jwt"})).To(MatchError(errAuthAdminPasswordRequired))
		Expect(validateAuthStartupConfig(leafwikiRuntimeConfig{JWTSecret: "jwt", AdminPassword: "admin"})).To(Succeed())
	})

	ginkgo.It("assembles direct runtime config from parsed startup flags", func() {
		startup, observation := parseStartupForObservation([]string{
			"--host", "0.0.0.0",
			"--port", "9090",
			"--disable-auth",
			"--mcp", "http",
			"--base-path", "/wiki/",
			"--max-asset-upload-size", "1MiB",
			"--daemon-idle-timeout", "2s",
		})
		Expect(observation).To(Equal(startupParseObservation{state: startupRuntimeRequested, serviceMode: startupDirectRuntime}))

		cfg := buildRuntimeConfigForStartup(startup.flags, startup.visited, startup.serviceModeRequested, resolveStartupMCPTransports(startup.flags, startup.visited, false), resolveStartupDataDir(startup.flags, startup.visited, startup.serviceModeRequested))

		Expect(cfg).To(And(
			ExposeStartupRuntimeCapabilities(startupRuntimeCapabilities{
				auth:         daemonAuthDisabled,
				publicAccess: runtimePublicAccess,
				mcp:          runtimeHTTPMCPEnabled,
				idleShutdown: runtimeIdleShutdownAllowed,
			}),
			gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
				"Host":                   Equal("0.0.0.0"),
				"Port":                   Equal("9090"),
				"BasePath":               Equal("/wiki"),
				"MaxAssetUploadSize":     Equal(int64(1024 * 1024)),
				"DaemonIdleTimeout":      Equal(2 * time.Second),
				"RuntimeStack":           Equal(projectdaemon.RuntimeStackWikidFrontd),
				"MarkdownLinkRootPrefix": BeEmpty(),
			}),
		))
	})
})

var _ = ginkgo.Describe("daemon runtime configuration helpers", ginkgo.Label("unit"), func() {
	ginkgo.It("derives owner and workspace request configs from semantic workspace paths", func() {
		dataDir := leafwikiTempDir()
		rootDir := leafwikiTempDir()
		cfg := leafwikiRuntimeConfig{
			Workspace:              wiki.Workspace{ID: newFixtureWorkspaceID("home"), DataDir: dataDir, RootDir: rootDir},
			RuntimeStack:           projectdaemon.RuntimeStackWikidFrontd,
			Host:                   "127.0.0.1",
			Port:                   "8080",
			BasePath:               "/wiki",
			MarkdownLinkRootPrefix: "/docs",
			DisableAuth:            true,
			MCPTransports:          mcpTransports{HTTP: true, Stdio: true},
			AccessTokenTimeout:     time.Minute,
			RefreshTokenTimeout:    time.Hour,
			Logging:                leaflogging.Config{Target: leaflogging.TargetStderr, Level: slog.LevelDebug},
			DaemonIdleTimeout:      time.Second,
		}

		daemonCfg, err := daemonRequestConfigForRuntime(cfg)
		Expect(err).To(Succeed())
		Expect(daemonCfg).To(And(
			ExposeDaemonRuntimeCapabilities(daemonRuntimeCapabilities{
				auth:          daemonAuthDisabled,
				publicMCP:     daemonPublicMCPEnabled,
				workspaceSync: daemonWorkspaceSyncEnabled,
			}),
			gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
				"RuntimeStack":           Equal(projectdaemon.RuntimeStackWikidFrontd),
				"WorkspaceID":            Equal(newFixtureWorkspaceID("home")),
				"MarkdownLinkRootPrefix": BeEmpty(),
				"AccessTokenTimeout":     Equal(time.Minute.String()),
				"RefreshTokenTimeout":    Equal(time.Hour.String()),
				"DaemonIdleTimeout":      Equal(time.Second.String()),
			}),
		))

		workspaceCfg, err := daemonWorkspaceRequestConfigForRuntime(cfg)
		Expect(err).To(Succeed())
		Expect(workspaceCfg).To(And(
			ExposeDaemonRuntimeCapabilities(daemonRuntimeCapabilities{
				auth:          daemonAuthDisabled,
				publicMCP:     daemonPublicMCPEnabled,
				workspaceSync: daemonWorkspaceSyncEnabled,
			}),
			gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
				"RuntimeStack": Equal(projectdaemon.RuntimeStackWikidFrontd),
				"WorkspaceID":  Equal(newFixtureWorkspaceID("home")),
				"LogTarget":    Equal(string(leaflogging.TargetFile)),
			}),
		))
	})

	ginkgo.It("records startup validation failures only for file logging targets", func() {
		logDir := leafwikiTempDir()
		logFile := filepath.Join(logDir, "startup.log")

		logStartupValidationFailure(leaflogging.Config{Target: leaflogging.TargetStderr}, "ignored")
		logStartupValidationFailure(leaflogging.Config{Target: leaflogging.TargetFile, FilePath: logFile}, "cli.error.leafwiki_startup_failed")

		Expect(readJSONLogEntries(logFile)).To(ContainElement(haveJSONLogEntry("cli.error.leafwiki_startup_failed")))
	})
})

var _ = ginkgo.Describe("internal runtime role helpers", ginkgo.Label("unit"), func() {
	ginkgo.It("writes role readiness and startup configs as private JSON files", func() {
		readyPath := filepath.Join(leafwikiTempDir(), "ready.json")
		ready := internalRuntimeRoleReady{Role: projectdaemon.RoleWorkspaced, PID: os.Getpid(), URL: "http://127.0.0.1:45001", Private: true}
		Expect(writeInternalRuntimeRoleReady(readyPath, ready)).To(Succeed())

		var decodedReady internalRuntimeRoleReady
		rawReady, err := os.ReadFile(readyPath)
		Expect(err).To(Succeed())
		Expect(json.Unmarshal(rawReady, &decodedReady)).To(Succeed())
		Expect(decodedReady).To(Equal(ready))

		startupPath, err := writeInternalRuntimeRoleStartupConfig(internalRuntimeRoleStartupConfig{
			Role:        projectdaemon.RoleFrontd,
			Runtime:     leafwikiRuntimeConfig{Host: "127.0.0.1", Port: "0"},
			DaemonToken: "daemon-token",
		})
		Expect(err).To(Succeed())
		ginkgo.DeferCleanup(os.Remove, startupPath)

		var decodedStartup internalRuntimeRoleStartupConfig
		rawStartup, err := os.ReadFile(startupPath)
		Expect(err).To(Succeed())
		Expect(json.Unmarshal(rawStartup, &decodedStartup)).To(Succeed())
		Expect(decodedStartup).To(MatchRuntimeRoleStartupConfig(runtimeRoleStartupConfigObservation{
			role:        projectdaemon.RoleFrontd,
			daemonToken: "daemon-token",
		}))
	})

	ginkgo.It("resolves role names and process completion without launching runtime roles", func() {
		role, ok := parseInternalRuntimeRoleName(" workspaced ")
		Expect(classifyRuntimeRoleName(role, ok)).To(Equal(runtimeRoleWorkspaced))
		role, ok = parseInternalRuntimeRoleName("unknown")
		Expect(classifyRuntimeRoleName(role, ok)).To(Equal(runtimeRoleUnsupported))

		done := make(chan error, 1)
		proc := &internalRuntimeRoleProcess{done: done, waitDone: make(chan struct{})}
		Expect(classifyRuntimeRoleProcessDone(proc)).To(Equal(runtimeRoleProcessRunning))
		done <- nil
		Expect(proc.wait()).To(Succeed())
		Expect(classifyRuntimeRoleProcessDone(proc)).To(Equal(runtimeRoleProcessDone))
	})
})

var _ = ginkgo.Describe("federated workspace runtime helpers", ginkgo.Label("unit"), func() {
	ginkgo.It("derives workspace-local runtime config from registry records", func() {
		base := leafwikiRuntimeConfig{Host: "0.0.0.0", Port: "8080", MarkdownLinkRootPrefix: "/base"}
		manager := newFederatedWorkspaceManager(base, "daemon-token", "http://127.0.0.1:4100", wikid.GlobalLayout(leafwikiTempDir()), wikid.NewWorkspaceSupervisor(wikid.WorkspaceSupervisorOptions{}))
		workspace := wikid.WorkspaceRecord{
			ID:                     newFixtureWorkspaceID("docs"),
			DataDir:                "  /tmp/docs-data  ",
			RootDir:                "  /tmp/docs-root  ",
			MarkdownLinkRootPrefix: "  /docs  ",
		}

		Expect(manager.workspaceRuntimeConfig(workspace, "0")).To(MatchWorkspaceRuntimeConfig(workspaceRuntimeConfigObservation{
			workspaceID:            newFixtureWorkspaceID("docs"),
			dataDir:                "/tmp/docs-data",
			rootDir:                "/tmp/docs-root",
			markdownLinkRootPrefix: "/docs",
			host:                   "127.0.0.1",
			port:                   "0",
			workspaceSync:          daemonWorkspaceSyncEnabled,
		}))
		Expect(workspaceRuntimeDescriptorPath("/runtime", newFixtureWorkspaceID("docs"))).To(Equal(filepath.Join("/runtime", "workspaces", "docs.json")))
	})
})

var _ = ginkgo.Describe("native STDIO helpers", ginkgo.Label("unit"), func() {
	ginkgo.It("streams valid JSON frames and reports parse errors to stdout", func() {
		stdin := io.NopCloser(bytes.NewBufferString("{bad-json}\n" + `{"jsonrpc":"2.0","id":1}` + "\n"))
		var stdout bytes.Buffer

		filtered, done := newNativeStdioJSONFilter(stdin, &stdout)
		forwarded, err := io.ReadAll(filtered)
		Expect(err).To(Succeed())
		Expect(done).To(Receive(Succeed()))

		Expect(string(forwarded)).To(Equal(`{"jsonrpc":"2.0","id":1}` + "\n"))
		Expect(stdout.String()).To(MatchNativeStdioParseErrorFrame())
	})

	ginkgo.It("observes process liveness without starting child processes", func() {
		Expect(observeProcessLiveness(-1)).To(Equal(runtimeProcessNotRunning))
		Expect(observeProcessLiveness(os.Getpid())).To(Equal(runtimeProcessRunning))
	})
})
