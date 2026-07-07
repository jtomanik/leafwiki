package main

import (
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"bytes"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/onsi/gomega/gstruct"
	"github.com/onsi/gomega/types"
	"github.com/perber/wiki/internal/agenthooks"
	coreauth "github.com/perber/wiki/internal/core/auth"
	httpinternal "github.com/perber/wiki/internal/http"
	"github.com/perber/wiki/internal/projectdaemon"
	"github.com/perber/wiki/internal/wiki"
	"github.com/perber/wiki/internal/wikid"
	"github.com/perber/wiki/internal/workspaceid"
)

const (
	leafwikiStartupLogMessage          = "Starting LeafWiki"
	leafwikiHTTPRequestLogMessage      = "http request"
	leafwikiDataDirectoryCreatedLogMsg = "Data directory created"
	leafwikiMCPStdioFailedLogMessage   = "MCP STDIO failed"
	leafwikiInvalidNativeStdioAPIKey   = "invalid native STDIO API key"
	leafwikiNativeStdioPositionalCmd   = "native STDIO does not support positional commands"
	leafwikiRootDirLockHeldMessage     = "root directory is already in use"
)

func TestLeafWikiSuite(t *testing.T) {
	if runLeafWikiHelperProcessForTest() {
		return
	}
	RegisterFailHandler(ginkgo.Fail)
	ginkgo.RunSpecs(t, "LeafWiki Suite")
}

func leafwikiTempDir() string {
	ginkgo.GinkgoHelper()
	dir, err := os.MkdirTemp("", "leafwiki-test-*")
	Expect(err).NotTo(HaveOccurred())
	ginkgo.DeferCleanup(func() {
		terminateProjectDaemonDescriptorsUnder(dir)
		Expect(os.RemoveAll(dir)).To(Succeed())
	})
	return dir
}

func terminateProjectDaemonDescriptorsUnder(root string) {
	ginkgo.GinkgoHelper()

	Expect(filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				return nil
			}
			return err
		}
		if entry == nil || entry.IsDir() || filepath.Ext(path) != ".json" {
			return nil
		}
		desc, readDescriptorErr := projectdaemon.ReadTrustedDescriptor(path)
		if readDescriptorErr != nil || desc.PID == os.Getpid() || !leafwikiInternalProcessPID(desc.PID) {
			return nil
		}
		terminateProjectDaemonProcess(desc.PID)
		return nil
	})).To(Succeed())
}

func leafwikiSetenv(key string, value string) {
	ginkgo.GinkgoHelper()
	previous, existed := os.LookupEnv(key)
	Expect(os.Setenv(key, value)).To(Succeed())
	ginkgo.DeferCleanup(func() {
		if existed {
			Expect(os.Setenv(key, previous)).To(Succeed())
			return
		}
		Expect(os.Unsetenv(key)).To(Succeed())
	})
}

func MatchCodexAgentHookAllowResponse() types.GomegaMatcher {
	return MatchAgentHookAllowResponse(agenthooks.ProviderCodex)
}

func MatchAgentHookAllowResponse(provider agenthooks.ProviderID) types.GomegaMatcher {
	return WithTransform(func(output string) []byte {
		return []byte(output)
	}, Equal(agenthooks.AllowProviderResponse(provider)))
}

func MatchPublicWorkspaceSyncDaemonConfig(fields gstruct.Fields) types.GomegaMatcher {
	return SatisfyAll(
		gstruct.MatchFields(gstruct.IgnoreExtras, fields),
		WithTransform(classifyPublicWorkspaceSyncConfig, Equal(publicWorkspaceSyncConfigEnabled)),
	)
}

func MatchNoMCPTransports() types.GomegaMatcher {
	return WithTransform(classifyMCPTransports, Equal(mcpTransportsDisabled))
}

func MatchHTTPMCPTransport() types.GomegaMatcher {
	return WithTransform(classifyMCPTransports, Equal(mcpTransportHTTPOnly))
}

func MatchStdioMCPTransport() types.GomegaMatcher {
	return WithTransform(classifyMCPTransports, Equal(mcpTransportStdioOnly))
}

func MatchCombinedMCPTransports() types.GomegaMatcher {
	return WithTransform(classifyMCPTransports, Equal(mcpTransportHTTPAndStdio))
}

func MatchPublicMCPRouterOptions(fields gstruct.Fields) types.GomegaMatcher {
	return SatisfyAll(
		gstruct.MatchFields(gstruct.IgnoreExtras, fields),
		WithTransform(classifyPublicMCPRouterOptions, Equal(publicMCPRouterEnabled)),
	)
}

func MatchProjectDaemonDescriptorPathIdentity(dataDir string, rootDir string) types.GomegaMatcher {
	return WithTransform(func(desc *projectdaemon.Descriptor) workspacePathIdentity {
		if desc == nil {
			return workspacePathIdentity{}
		}
		return workspacePathIdentity{
			DataDir: classifySameFilePath(desc.DataDir, dataDir),
			RootDir: classifySameFilePath(desc.RootDir, rootDir),
		}
	}, Equal(workspacePathIdentity{DataDir: workspacePathSameFile, RootDir: workspacePathSameFile}))
}

type publicWorkspaceSyncConfigState uint8

const (
	publicWorkspaceSyncConfigDisabled publicWorkspaceSyncConfigState = iota
	publicWorkspaceSyncConfigEnabled
)

func classifyPublicWorkspaceSyncConfig(cfg projectdaemon.Config) publicWorkspaceSyncConfigState {
	if cfg.PublicMCPEnabled && cfg.EnableWorkspaceSync {
		return publicWorkspaceSyncConfigEnabled
	}
	return publicWorkspaceSyncConfigDisabled
}

type mcpTransportSelection uint8

const (
	mcpTransportsDisabled mcpTransportSelection = iota
	mcpTransportHTTPOnly
	mcpTransportStdioOnly
	mcpTransportHTTPAndStdio
)

func classifyMCPTransports(transports mcpTransports) mcpTransportSelection {
	switch {
	case transports.HTTP && transports.Stdio:
		return mcpTransportHTTPAndStdio
	case transports.HTTP:
		return mcpTransportHTTPOnly
	case transports.Stdio:
		return mcpTransportStdioOnly
	default:
		return mcpTransportsDisabled
	}
}

type publicMCPRouterState uint8

const (
	publicMCPRouterDisabled publicMCPRouterState = iota
	publicMCPRouterEnabled
)

func classifyPublicMCPRouterOptions(opts httpinternal.RouterOptions) publicMCPRouterState {
	if opts.MCPEnabled {
		return publicMCPRouterEnabled
	}
	return publicMCPRouterDisabled
}

var (
	errAgentHookEventRejected           = errors.New("agent hook event rejected")
	errAgentHookProviderAbsent          = errors.New("agent hook provider absent")
	errBoolValueRejected                = errors.New("bool value rejected")
	errDurationValueRejected            = errors.New("duration value rejected")
	errFederatedWorkspaceAbsent         = errors.New("federated workspace absent")
	errFrontdRemoteUserAbsent           = errors.New("frontd remote user absent")
	errProjectDaemonDescriptorUnhealthy = errors.New("project daemon descriptor unhealthy")
	errRelativePathOutsideBase          = errors.New("relative path outside base")
	errRegistryWorkspaceAbsent          = errors.New("registry workspace absent")
	errRuntimeRoleAbsent                = errors.New("runtime role absent")
	errWorkspaceGrantAbsent             = errors.New("workspace grant absent")
)

func agentHookProviderFromArgsResult(args []string) (agenthooks.ProviderID, error) {
	provider, ok := agentHookProviderFromArgs(args)
	if !ok {
		return "", errAgentHookProviderAbsent
	}
	return provider, nil
}

func agentHookProviderFromRawArgsResult(args []string) (agenthooks.ProviderID, error) {
	provider, ok := agentHookProviderFromRawArgs(args)
	if !ok {
		return "", errAgentHookProviderAbsent
	}
	return provider, nil
}

func normalizedAgentHookEventResult(provider agenthooks.ProviderID, raw []byte, seenAt time.Time) (agenthooks.Event, error) {
	event, accepted := agenthooks.Normalize(provider, raw, seenAt)
	if !accepted {
		return agenthooks.Event{}, errAgentHookEventRejected
	}
	return event, nil
}

func parseBoolResult(raw string) (bool, error) {
	value, ok := parseBool(raw)
	if !ok {
		return false, errBoolValueRejected
	}
	return value, nil
}

func parseDurationResult(raw string) (time.Duration, error) {
	value, ok := parseDuration(raw)
	if !ok {
		return 0, errDurationValueRejected
	}
	return value, nil
}

func localRelativePathResult(base string, target string) (string, error) {
	path, ok := localRelativePath(base, target)
	if !ok {
		return "", errRelativePathOutsideBase
	}
	return path, nil
}

func runtimeRoleHealthResult(roles []projectdaemon.RoleHealth, name projectdaemon.RoleName) (projectdaemon.RoleHealth, error) {
	role, ok := findRuntimeRoleHealth(roles, name)
	if !ok {
		return projectdaemon.RoleHealth{}, errRuntimeRoleAbsent
	}
	return role, nil
}

func registeredFederatedWorkspaceForRequestResult(layout wikid.Layout, requestCfg projectdaemon.Config) (wikid.WorkspaceRecord, error) {
	workspace, ok, err := registeredFederatedWorkspaceForRequest(layout, requestCfg)
	if err != nil {
		return wikid.WorkspaceRecord{}, err
	}
	if !ok {
		return wikid.WorkspaceRecord{}, errFederatedWorkspaceAbsent
	}
	return workspace, nil
}

func federatedStdioAPIKeyWorkspaceGrantResult(layout wikid.Layout, cfg leafwikiRuntimeConfig, workspaceID workspaceid.WorkspaceID) (wikid.Grant, error) {
	grant, ok, err := federatedStdioAPIKeyWorkspaceGrant(layout, cfg, workspaceID)
	if err != nil {
		return wikid.Grant{}, err
	}
	if !ok {
		return wikid.Grant{}, errWorkspaceGrantAbsent
	}
	return grant, nil
}

func frontdRemoteUserResult(req *http.Request, w *wiki.Wiki, cfg leafwikiRuntimeConfig) (*coreauth.User, string, error) {
	user, method, present, err := frontdRemoteUser(req, w, cfg)
	if err != nil {
		return user, method, err
	}
	if !present {
		return user, method, errFrontdRemoteUserAbsent
	}
	return user, method, nil
}

func registryWorkspaceResult(registry *wikid.RegistryService, id workspaceid.WorkspaceID) (wikid.WorkspaceRecord, error) {
	workspace, ok, err := registry.Workspace(id)
	if err != nil {
		return wikid.WorkspaceRecord{}, err
	}
	if !ok {
		return wikid.WorkspaceRecord{}, errRegistryWorkspaceAbsent
	}
	return workspace, nil
}

func readHealthyProjectDaemonResult(ctx context.Context, descriptorPath string, ownerCfg projectdaemon.Config) (*projectdaemon.Descriptor, error) {
	desc, healthy, err := readHealthyProjectDaemon(ctx, descriptorPath, ownerCfg)
	if err != nil {
		return desc, err
	}
	if !healthy {
		return desc, errProjectDaemonDescriptorUnhealthy
	}
	return desc, nil
}

func projectDaemonDescriptorHealthyResult(ctx context.Context, desc *projectdaemon.Descriptor) error {
	healthy, err := projectDaemonDescriptorHealthy(ctx, desc)
	if err != nil {
		return err
	}
	if !healthy {
		return errProjectDaemonDescriptorUnhealthy
	}
	return nil
}

func haveDaemonStdioBridgeHTTPClient(controlToken string, bearerToken string) types.GomegaMatcher {
	ginkgo.GinkgoHelper()

	return SatisfyAll(
		Not(BeNil()),
		HaveField("Timeout", BeZero()),
		HaveField("Transport", WithTransform(func(transport http.RoundTripper) (projectdaemon.AuthRoundTripper, error) {
			authTransport, ok := transport.(projectdaemon.AuthRoundTripper)
			if !ok {
				return projectdaemon.AuthRoundTripper{}, fmt.Errorf("expected projectdaemon.AuthRoundTripper, got %T", transport)
			}
			return authTransport, nil
		}, SatisfyAll(
			HaveField("ControlToken", Equal(controlToken)),
			HaveField("BearerToken", Equal(bearerToken)),
		))),
	)
}

var _ = ginkgo.Describe("leafwiki usage output", func() {
	ginkgo.It("documents MCP transport selector", ginkgo.Label("unit"), func() {
		Expect(leafwikiUsage()).To(MatchLeafwikiUsageContract())

	})
})

var _ = ginkgo.Describe("leafwiki usage output", func() {
	ginkgo.It("renders help body from catalog", ginkgo.Label("unit"), func() {
		var buf bytes.Buffer

		writeUsage(&buf)

		Expect(leafwikiUsage().MessageIDs).To(ContainElement(leafwikiUsageMessageCLIHelpBody))
		Expect(buf.String()).To(ContainRenderedLeafwikiUsageMessage(leafwikiUsageMessageCLIHelpBody), fmt.Sprintf("usage output did not include catalog help body"))

	})
})

// Plantrace evidence: TestFailureMessageRendersCatalogBackedErrorBody.
var _ = ginkgo.Describe("CLI failure messages", func() {
	ginkgo.It("renders catalog backed error body", ginkgo.Label("unit"), func() {
		got := failureMessage("cli.error.invalid_environment", "error", "bad env")
		want := "Invalid environment error=bad env"
		Expect(got).To(Equal(want), fmt.Sprintf("failureMessage = %q, want %q", got, want))

	})
})

var _ = ginkgo.Describe("CLI flag registration", func() {
	ginkgo.It("rejects removed startup flags", ginkgo.Label("unit"), func() {
		for _, arg := range []string{
			"--enable-revision",
			"--enable-workspace-sync",
			"--enable-mcp",
			"--mcp-stdio",
			"--max-revision-history=0",
		} {
			func() {
				_ = arg
				flagName := removedStartupFlagName(arg)
				fs := flag.NewFlagSet("leafwiki-test", flag.ContinueOnError)
				var errOut bytes.Buffer
				fs.SetOutput(&errOut)
				registerFlags(fs)

				_ = fs.Parse([]string{arg})
				Expect(fs.Lookup(flagName)).To(BeNil(), fmt.Sprintf("removed flag %s is still registered", flagName))

			}()
		}

	})
})

var _ = ginkgo.Describe("removed LeafWiki environment validation", func() {
	ginkgo.DescribeTable("rejects removed runtime environment variables",
		ginkgo.Label("unit"),
		func(name string) {
			leafwikiSetenv(name, "")

			err := rejectRemovedLeafWikiEnv()
			Expect(err).To(MatchError(removedEnvironmentVariableError{Name: name}))
		},
		ginkgo.Entry("runtime stack", "LEAFWIKI_RUNTIME_STACK"),
		ginkgo.Entry("enable revision", "LEAFWIKI_ENABLE_REVISION"),
		ginkgo.Entry("enable workspace sync", "LEAFWIKI_ENABLE_WORKSPACE_SYNC"),
		ginkgo.Entry("max revision history", "LEAFWIKI_MAX_REVISION_HISTORY"),
		ginkgo.Entry("enable MCP", "LEAFWIKI_ENABLE_MCP"),
		ginkgo.Entry("MCP stdio", "LEAFWIKI_MCP_STDIO"),
	)
})

var _ = ginkgo.Describe("daemon runtime configuration", func() {
	ginkgo.It("includes workspace ID", ginkgo.Label("unit"), func() {
		cfg := leafwikiRuntimeConfig{
			Workspace: wiki.Workspace{
				ID:      newFixtureWorkspaceID("home"),
				DataDir: filepath.Join(leafwikiTempDir(), "data"),
				RootDir: filepath.Join(leafwikiTempDir(), "root"),
			},
			RuntimeStack: projectdaemon.RuntimeStackWikidFrontd,
		}

		ownerCfg, err := daemonConfigForRuntime(cfg)
		Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("daemonConfigForRuntime failed: %v", err))
		Expect(ownerCfg.WorkspaceID).To(Equal(wikid.HomeWorkspaceID), fmt.Sprintf("WorkspaceID = %q, want home", ownerCfg.WorkspaceID))

	})
})

var _ = ginkgo.Describe("workspace resolution", func() {
	ginkgo.It("defaults to home workspace ID", ginkgo.Label("unit"), func() {
		fs := flag.NewFlagSet("leafwiki-test", flag.ContinueOnError)
		fs.SetOutput(io.Discard)
		flags := registerFlags(fs)
		Expect(fs.Parse([]string{
			"--data-dir", filepath.Join(leafwikiTempDir(), "data"),
			"--root-dir", filepath.Join(leafwikiTempDir(), "root"),
		})).To(Succeed())
		visited := map[string]bool{}
		fs.Visit(func(f *flag.Flag) { visited[f.Name] = true })

		workspace, err := resolveWorkspace(flags, visited)
		Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("resolveWorkspace failed: %v", err))
		Expect(workspace.ID).To(Equal(wikid.HomeWorkspaceID), fmt.Sprintf("workspace ID = %q, want home", workspace.ID))

	})
})
