package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	sdkauth "github.com/modelcontextprotocol/go-sdk/auth"
	sdkjsonrpc "github.com/modelcontextprotocol/go-sdk/jsonrpc"
	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/gcustom"
	"github.com/onsi/gomega/gstruct"
	"github.com/onsi/gomega/types"

	"github.com/perber/wiki/internal/agenthooks"
	coreauth "github.com/perber/wiki/internal/core/auth"
	sharederrors "github.com/perber/wiki/internal/core/shared/errors"
	"github.com/perber/wiki/internal/frontd"
	httpinternal "github.com/perber/wiki/internal/http"
	authmw "github.com/perber/wiki/internal/http/middleware/auth"
	"github.com/perber/wiki/internal/localization"
	"github.com/perber/wiki/internal/locking"
	leaflogging "github.com/perber/wiki/internal/logging"
	"github.com/perber/wiki/internal/projectdaemon"
	testmatchers "github.com/perber/wiki/internal/test_utils/matchers"
	"github.com/perber/wiki/internal/wiki"
	"github.com/perber/wiki/internal/wikid"
	"github.com/perber/wiki/internal/workspaceid"
	sqlite "modernc.org/sqlite"
	sqlite3 "modernc.org/sqlite/lib"
)

const (
	leafwikiNativeStdioParseErrorFrame = `{"jsonrpc":"2.0","id":null,"error":{"code":-32700,"message":"Parse error"}}`
	leafwikiFixtureDaemonStopped       = "daemon stopped"
	leafwikiFixtureFrontdExited        = "frontd exited"
)

func MatchProjectDaemonConfigMismatch(mismatches ...projectdaemon.Mismatch) types.GomegaMatcher {
	expected := append([]projectdaemon.Mismatch(nil), mismatches...)
	return gcustom.MakeMatcher(func(err error) (bool, error) {
		var mismatchErr *projectdaemon.ConfigMismatchError
		if !errors.As(err, &mismatchErr) {
			return false, nil
		}
		for _, want := range expected {
			if !containsProjectDaemonMismatch(mismatchErr.Mismatches, want) {
				return false, nil
			}
		}
		return true, nil
	}).WithTemplate("Expected:\n{{.FormattedActual}}\n{{.To}} match project daemon config mismatch\n{{format .Data 1}}", expected)
}

func containsProjectDaemonMismatch(mismatches []projectdaemon.Mismatch, want projectdaemon.Mismatch) bool {
	for _, got := range mismatches {
		if got == want {
			return true
		}
	}
	return false
}

func MatchProjectDaemonStartupFailure() types.GomegaMatcher {
	ginkgo.GinkgoHelper()

	return SatisfyAll(
		HaveField("Kind", Equal(projectDaemonStartupErrorKindStartup)),
		HaveField("MessageID", Equal(projectDaemonStartupErrorMessageID())),
	)
}

func MatchProjectDaemonLockStartupFailure() types.GomegaMatcher {
	ginkgo.GinkgoHelper()

	return SatisfyAll(
		HaveField("Kind", Equal(projectDaemonStartupErrorKindLock)),
		HaveField("MessageID", Equal(projectDaemonStartupErrorMessageID())),
	)
}

func projectDaemonStartupErrorMessageID() sharederrors.MessageID {
	return projectDaemonStartupError{MessageID: localization.MessageIDCLIErrorProjectDaemonFailed}.MessageID
}

func MatchNativeStdioParseErrorFrame() types.GomegaMatcher {
	ginkgo.GinkgoHelper()

	return WithTransform(func(raw string) ([]int, error) {
		codes := []int{}
		for _, response := range nativeStdioResponses(raw) {
			if len(response.Error) == 0 {
				continue
			}
			var frameError struct {
				Code int `json:"code"`
			}
			if err := json.Unmarshal(response.Error, &frameError); err != nil {
				return nil, err
			}
			codes = append(codes, frameError.Code)
		}
		return codes, nil
	}, ContainElement(-32700))
}

func MatchPathError() types.GomegaMatcher {
	return gcustom.MakeMatcher(func(err error) (bool, error) {
		var pathErr *os.PathError
		return errors.As(err, &pathErr), nil
	}).WithMessage("match path error")
}

func MatchPathErrorIs(target error) types.GomegaMatcher {
	ginkgo.GinkgoHelper()

	return gcustom.MakeMatcher(func(err error) (bool, error) {
		var pathErr *os.PathError
		return errors.As(err, &pathErr) && errors.Is(err, target), nil
	}).WithTemplate("Expected:\n{{.FormattedActual}}\n{{.To}} match path error\n{{format .Data 1}}", target)
}

func MatchSQLitePrimaryError(code int) types.GomegaMatcher {
	ginkgo.GinkgoHelper()

	return WithTransform(func(err error) int {
		var sqliteErr *sqlite.Error
		if !errors.As(err, &sqliteErr) {
			return -1
		}
		return sqliteErr.Code() & 0xFF
	}, Equal(code))
}

type projectDaemonLockProbe struct {
	DataDir string
	RootDir string
}

func projectDaemonLocks(dataDir string, rootDir string) projectDaemonLockProbe {
	return projectDaemonLockProbe{DataDir: dataDir, RootDir: rootDir}
}

type startupWorkspaceResolution struct {
	Workspace     wiki.Workspace
	StartsRuntime bool
	Err           error
}

func resolveStartupWorkspaceResult(flags *cliFlags, visited map[string]bool, args []string) startupWorkspaceResolution {
	workspace, startsRuntime, err := resolveStartupWorkspace(flags, visited, args)
	return startupWorkspaceResolution{Workspace: workspace, StartsRuntime: startsRuntime, Err: err}
}

func MatchStartupSubcommandWorkspaceSkip() types.GomegaMatcher {
	ginkgo.GinkgoHelper()

	return gcustom.MakeMatcher(func(result startupWorkspaceResolution) (bool, error) {
		return result.Err == nil && result.Workspace == (wiki.Workspace{}) && !result.StartsRuntime, nil
	}).WithMessage("match startup subcommand workspace skip")
}

type projectDaemonDescriptorReadResult struct {
	Descriptor *projectdaemon.Descriptor
	Healthy    bool
	Err        error
}

func readHealthyProjectDaemonLockResult(ctx context.Context, descriptorPath string, cfg projectdaemon.Config) projectDaemonDescriptorReadResult {
	descriptor, healthy, err := readHealthyProjectDaemon(ctx, descriptorPath, cfg)
	return projectDaemonDescriptorReadResult{Descriptor: descriptor, Healthy: healthy, Err: err}
}

func MatchProjectDaemonDescriptorPreservedByLock() types.GomegaMatcher {
	ginkgo.GinkgoHelper()

	return gcustom.MakeMatcher(func(result projectDaemonDescriptorReadResult) (bool, error) {
		var syntaxErr *json.SyntaxError
		return result.Descriptor == nil && !result.Healthy && errors.As(result.Err, &syntaxErr), nil
	}).WithMessage("match unreadable project daemon descriptor preserved by a held project lock")
}

func MatchWikidPrivateEndpointStatus(statusCode int) types.GomegaMatcher {
	return gcustom.MakeMatcher(func(err error) (bool, error) {
		var endpointErr *wikidPrivateEndpointError
		return errors.As(err, &endpointErr) && endpointErr.StatusCode == statusCode, nil
	}).WithMessage("match wikid private endpoint status")
}

func MatchJSONSyntaxError() types.GomegaMatcher {
	return gcustom.MakeMatcher(func(err error) (bool, error) {
		var syntaxErr *json.SyntaxError
		return errors.As(err, &syntaxErr), nil
	}).WithMessage("match JSON syntax error")
}

func BeCleanNativeStdioClose() types.GomegaMatcher {
	ginkgo.GinkgoHelper()

	return gcustom.MakeMatcher(func(err error) (bool, error) {
		return isCleanNativeStdioClose(err), nil
	}).WithMessage("match clean native STDIO close")
}

func BeTrustedDaemonControlURL() types.GomegaMatcher {
	ginkgo.GinkgoHelper()

	return gcustom.MakeMatcher(func(raw string) (bool, error) {
		return isTrustedDaemonControlURL(raw), nil
	}).WithMessage("match trusted daemon control URL")
}

func MatchDaemonHealthDescriptor(desc *projectdaemon.Descriptor) types.GomegaMatcher {
	ginkgo.GinkgoHelper()

	return gcustom.MakeMatcher(func(health *projectdaemon.DaemonHealth) (bool, error) {
		return daemonHealthMatchesDescriptor(desc, health), nil
	}).WithTemplate("Expected:\n{{.FormattedActual}}\n{{.To}} match daemon health descriptor\n{{format .Data 1}}", desc)
}

func BeHealthyProjectDaemonDescriptor(ctx context.Context) types.GomegaMatcher {
	ginkgo.GinkgoHelper()

	return gcustom.MakeMatcher(func(desc *projectdaemon.Descriptor) (bool, error) {
		healthy, err := projectDaemonDescriptorHealthy(ctx, desc)
		return err == nil && healthy, nil
	}).WithMessage("match healthy project daemon descriptor")
}

func BeUnhealthyProjectDaemonDescriptor(ctx context.Context) types.GomegaMatcher {
	ginkgo.GinkgoHelper()

	return gcustom.MakeMatcher(func(desc *projectdaemon.Descriptor) (bool, error) {
		healthy, err := projectDaemonDescriptorHealthy(ctx, desc)
		return err == nil && !healthy, nil
	}).WithMessage("match unhealthy project daemon descriptor")
}

func MatchInvalidWorkspacedUpstream() types.GomegaMatcher {
	ginkgo.GinkgoHelper()

	return gcustom.MakeMatcher(func(err error) (bool, error) {
		return frontd.IsInvalidWorkspacedUpstream(err), nil
	}).WithMessage("match invalid workspaced upstream")
}

func MatchInvalidWikidUpstream() types.GomegaMatcher {
	ginkgo.GinkgoHelper()

	return gcustom.MakeMatcher(func(err error) (bool, error) {
		return frontd.IsInvalidWikidUpstream(err), nil
	}).WithMessage("match invalid wikid upstream")
}

func MatchHeldRuntimeLockError() types.GomegaMatcher {
	ginkgo.GinkgoHelper()

	return gcustom.MakeMatcher(func(err error) (bool, error) {
		return locking.IsLockHeld(err), nil
	}).WithMessage("match held runtime lock error")
}

func HaveAvailableProjectDaemonLocks() types.GomegaMatcher {
	ginkgo.GinkgoHelper()

	return gcustom.MakeMatcher(func(probe projectDaemonLockProbe) (bool, error) {
		return projectDaemonLocksFree(probe.DataDir, probe.RootDir)
	}).WithMessage("match available project daemon locks")
}

func HaveHeldProjectDaemonLocks() types.GomegaMatcher {
	ginkgo.GinkgoHelper()

	return gcustom.MakeMatcher(func(probe projectDaemonLockProbe) (bool, error) {
		return projectDaemonLocksHeld(probe.DataDir, probe.RootDir)
	}).WithMessage("match held project daemon locks")
}

func HaveFreeDataLockWithHeldRootLock() types.GomegaMatcher {
	ginkgo.GinkgoHelper()

	return gcustom.MakeMatcher(func(probe projectDaemonLockProbe) (bool, error) {
		return projectDaemonDataLockFreeRootLockHeld(probe.DataDir, probe.RootDir)
	}).WithMessage("match free data lock with held root lock")
}

func MatchProjectDaemonLockAvailabilityError(target error) types.GomegaMatcher {
	ginkgo.GinkgoHelper()

	return gcustom.MakeMatcher(func(probe projectDaemonLockProbe) (bool, error) {
		available, err := projectDaemonLocksFree(probe.DataDir, probe.RootDir)
		return !available && errors.Is(err, target), nil
	}).WithTemplate("Expected:\n{{.FormattedActual}}\n{{.To}} match project daemon lock availability error\n{{format .Data 1}}", target)
}

func MatchProjectDaemonHeldLockProbeError(target error) types.GomegaMatcher {
	ginkgo.GinkgoHelper()

	return gcustom.MakeMatcher(func(probe projectDaemonLockProbe) (bool, error) {
		held, err := projectDaemonLocksHeld(probe.DataDir, probe.RootDir)
		return !held && errors.Is(err, target), nil
	}).WithTemplate("Expected:\n{{.FormattedActual}}\n{{.To}} match project daemon held-lock error\n{{format .Data 1}}", target)
}

func MatchProjectDaemonDataRootLockProbeError(target error) types.GomegaMatcher {
	ginkgo.GinkgoHelper()

	return gcustom.MakeMatcher(func(probe projectDaemonLockProbe) (bool, error) {
		disjoint, err := projectDaemonDataLockFreeRootLockHeld(probe.DataDir, probe.RootDir)
		return !disjoint && errors.Is(err, target), nil
	}).WithTemplate("Expected:\n{{.FormattedActual}}\n{{.To}} match project daemon data/root lock error\n{{format .Data 1}}", target)
}

func MatchHomeFederatedFirstContact() types.GomegaMatcher {
	ginkgo.GinkgoHelper()

	return gcustom.MakeMatcher(func(result federatedFirstContactResult) (bool, error) {
		return result.Err == nil &&
			result.Home &&
			result.Workspace.ID == wikid.HomeWorkspaceID &&
			result.Workspace.DataDir != "" &&
			result.Workspace.RootDir != "", nil
	}).WithMessage("match home workspace federated first contact")
}

func MatchAbsentProjectDaemonHealth() types.GomegaMatcher {
	ginkgo.GinkgoHelper()

	return gcustom.MakeMatcher(func(result projectDaemonDescriptorReadResult) (bool, error) {
		return result.Descriptor == nil && !result.Healthy, nil
	}).WithMessage("match absent project daemon health")
}

func MatchStaleProjectDaemonHealth() types.GomegaMatcher {
	ginkgo.GinkgoHelper()

	return gcustom.MakeMatcher(func(result projectDaemonDescriptorReadResult) (bool, error) {
		return result.Descriptor != nil && result.Descriptor.SchemaVersion == 0 && !result.Healthy, nil
	}).WithMessage("match stale project daemon health")
}

func MatchProjectDaemonDescriptorReadError(target error) types.GomegaMatcher {
	ginkgo.GinkgoHelper()

	return gcustom.MakeMatcher(func(result projectDaemonDescriptorReadResult) (bool, error) {
		return result.Descriptor == nil && !result.Healthy && errors.Is(result.Err, target), nil
	}).WithTemplate("Expected:\n{{.FormattedActual}}\n{{.To}} match project daemon health error\n{{format .Data 1}}", target)
}

func projectDaemonDescriptorHealthResult(ctx context.Context, desc *projectdaemon.Descriptor) projectDaemonDescriptorReadResult {
	healthy, err := projectDaemonDescriptorHealthy(ctx, desc)
	return projectDaemonDescriptorReadResult{Descriptor: desc, Healthy: healthy, Err: err}
}

func MatchProjectDaemonDescriptorHealthError(target error) types.GomegaMatcher {
	ginkgo.GinkgoHelper()

	return gcustom.MakeMatcher(func(result projectDaemonDescriptorReadResult) (bool, error) {
		return result.Descriptor != nil && !result.Healthy && errors.Is(result.Err, target), nil
	}).WithTemplate("Expected:\n{{.FormattedActual}}\n{{.To}} match project daemon descriptor health error\n{{format .Data 1}}", target)
}

func MatchUnreachableWorkspacedPrivateMCP() types.GomegaMatcher {
	ginkgo.GinkgoHelper()

	return gcustom.MakeMatcher(func(desc *projectdaemon.Descriptor) (bool, error) {
		healthy, err := projectDaemonDescriptorHealthy(context.Background(), desc)
		return !healthy && err == nil, nil
	}).WithMessage("match unreachable workspaced private MCP descriptor")
}

func MatchUnhealthyProjectDaemonDescriptor() types.GomegaMatcher {
	ginkgo.GinkgoHelper()

	return gcustom.MakeMatcher(func(result projectDaemonDescriptorReadResult) (bool, error) {
		return result.Descriptor != nil && !result.Healthy, nil
	}).WithMessage("match unhealthy project daemon descriptor")
}

type federatedFirstContactResult struct {
	Workspace wikid.WorkspaceRecord
	Home      bool
	Err       error
}

func registerFederatedFirstContactResult(layout wikid.Layout, cfg projectdaemon.Config, runtime leafwikiRuntimeConfig) federatedFirstContactResult {
	workspace, home, err := registerFederatedFirstContact(layout, cfg, runtime)
	return federatedFirstContactResult{Workspace: workspace, Home: home, Err: err}
}

func MatchFederatedFirstContactError(target error) types.GomegaMatcher {
	ginkgo.GinkgoHelper()

	return gcustom.MakeMatcher(func(result federatedFirstContactResult) (bool, error) {
		return result.Workspace == (wikid.WorkspaceRecord{}) && !result.Home && errors.Is(result.Err, target), nil
	}).WithTemplate("Expected:\n{{.FormattedActual}}\n{{.To}} match federated first-contact error\n{{format .Data 1}}", target)
}

func MatchFederatedRegisteredWorkspace(fields gstruct.Fields) types.GomegaMatcher {
	ginkgo.GinkgoHelper()

	return gcustom.MakeMatcher(func(result federatedFirstContactResult) (bool, error) {
		if result.Err != nil || result.Home {
			return false, nil
		}
		return gstruct.MatchFields(gstruct.IgnoreExtras, fields).Match(result.Workspace)
	}).WithTemplate("Expected:\n{{.FormattedActual}}\n{{.To}} match non-home federated workspace registration\n{{format .Data 1}}", fields)
}

func MatchFederatedWorkspacePathIdentity(dataDir string, rootDir string) types.GomegaMatcher {
	ginkgo.GinkgoHelper()

	expected := struct {
		DataDir string
		RootDir string
	}{DataDir: dataDir, RootDir: rootDir}
	return gcustom.MakeMatcher(func(workspace wikid.WorkspaceRecord) (bool, error) {
		dataInfo, err := os.Stat(workspace.DataDir)
		if err != nil {
			return false, nil
		}
		wantDataInfo, err := os.Stat(dataDir)
		if err != nil {
			return false, nil
		}
		rootInfo, err := os.Stat(workspace.RootDir)
		if err != nil {
			return false, nil
		}
		wantRootInfo, err := os.Stat(rootDir)
		if err != nil {
			return false, nil
		}
		return os.SameFile(dataInfo, wantDataInfo) && os.SameFile(rootInfo, wantRootInfo), nil
	}).WithTemplate("Expected:\n{{.FormattedActual}}\n{{.To}} match federated workspace path identity\n{{format .Data 1}}", expected)
}

func MatchProjectDaemonRoleHealth(name projectdaemon.RoleName, state projectdaemon.RoleState, fields gstruct.Fields) types.GomegaMatcher {
	expected := gstruct.Fields{
		"Name":  Equal(name),
		"State": Equal(state),
	}
	for field, matcher := range fields {
		expected[field] = matcher
	}
	return gstruct.MatchFields(gstruct.IgnoreExtras, expected)
}

func MatchPrivateProjectDaemonRoleHealth(name projectdaemon.RoleName, state projectdaemon.RoleState, fields gstruct.Fields) types.GomegaMatcher {
	return SatisfyAll(
		MatchProjectDaemonRoleHealth(name, state, fields),
		gcustom.MakeMatcher(func(role projectdaemon.RoleHealth) (bool, error) {
			return role.Private, nil
		}).WithMessage("mark project daemon role as private"),
	)
}

func MatchWorkspaceSyncEnabledDaemonConfig(fields gstruct.Fields) types.GomegaMatcher {
	return SatisfyAll(
		gstruct.MatchFields(gstruct.IgnoreExtras, fields),
		gcustom.MakeMatcher(func(cfg projectdaemon.Config) (bool, error) {
			return cfg.EnableWorkspaceSync, nil
		}).WithMessage("enable workspace sync in daemon config"),
	)
}

func MatchFederatedEnsureUnexpectedResult(workspaceID workspaceid.WorkspaceID, resultType string) types.GomegaMatcher {
	expected := struct {
		WorkspaceID workspaceid.WorkspaceID
		ResultType  string
	}{WorkspaceID: workspaceID, ResultType: resultType}
	return gcustom.MakeMatcher(func(err error) (bool, error) {
		var unexpectedErr *federatedEnsureUnexpectedResultError
		if !errors.As(err, &unexpectedErr) {
			return false, nil
		}
		return unexpectedErr.WorkspaceID == workspaceID && unexpectedErr.ResultType == resultType, nil
	}).WithTemplate("Expected:\n{{.FormattedActual}}\n{{.To}} match federated ensure unexpected result\n{{format .Data 1}}", expected)
}

func MatchProjectDaemonControlStatus(status int) types.GomegaMatcher {
	ginkgo.GinkgoHelper()

	return gcustom.MakeMatcher(func(err error) (bool, error) {
		return projectdaemon.IsControlStatus(err, status), nil
	}).WithTemplate("Expected:\n{{.FormattedActual}}\n{{.To}} match project daemon control status\n{{format .Data 1}}", status)
}

func MatchNetOpError() types.GomegaMatcher {
	ginkgo.GinkgoHelper()

	return gcustom.MakeMatcher(func(err error) (bool, error) {
		var opErr *net.OpError
		return errors.As(err, &opErr), nil
	}).WithMessage("match net operation error")
}

func MatchURLError() types.GomegaMatcher {
	ginkgo.GinkgoHelper()

	return gcustom.MakeMatcher(func(err error) (bool, error) {
		var urlErr *url.Error
		return errors.As(err, &urlErr), nil
	}).WithMessage("match URL error")
}

func MatchProcessExitError() types.GomegaMatcher {
	ginkgo.GinkgoHelper()

	return gcustom.MakeMatcher(func(err error) (bool, error) {
		var exitErr *exec.ExitError
		return errors.As(err, &exitErr), nil
	}).WithMessage("match helper process exit error")
}

func MatchWikidPrivateEndpoint(status int, code sharederrors.ErrorCode) types.GomegaMatcher {
	ginkgo.GinkgoHelper()

	expected := struct {
		Status int
		Code   sharederrors.ErrorCode
	}{Status: status, Code: code}
	return gcustom.MakeMatcher(func(err error) (bool, error) {
		var endpointErr *wikidPrivateEndpointError
		if !errors.As(err, &endpointErr) {
			return false, nil
		}
		return endpointErr.StatusCode == status && endpointErr.Code == code, nil
	}).WithTemplate("Expected:\n{{.FormattedActual}}\n{{.To}} match wikid private endpoint\n{{format .Data 1}}", expected)
}

func waitForLeafwikiContextCancellation(ctx context.Context) error {
	ginkgo.GinkgoHelper()

	deadline := time.Now().Add(3 * time.Second)
	for ctx.Err() == nil {
		if time.Now().After(deadline) {
			return context.DeadlineExceeded
		}
		time.Sleep(10 * time.Millisecond)
	}
	return ctx.Err()
}

var _ = ginkgo.Describe("leafwiki command helper edges", func() {
	ginkgo.It("panics on usage write failures instead of silently truncating help", func() {
		writeErr := errors.New("usage writer failed")
		Expect(func() {
			writeUsage(&leafwikiFailAfterWriter{failAt: 1, err: writeErr})
		}).To(PanicWith(writeErr))

		Expect(func() {
			writeUsage(&leafwikiFailAfterWriter{failAt: 2, err: writeErr})
		}).To(PanicWith(writeErr))
	})

	ginkgo.It("exits when internal startup validation rejects missing or invalid role inputs", func() {

		_, flags := leafwikiEdgeFlagSet()
		*flags.internalProjectDaemon = filepath.Join(leafwikiTempDir(), "missing-daemon-startup.json")
		Expect(func() {
			_ = runInternalStartupCommand(flags)
		}).To(PanicWithLeafwikiExit(1))

		_, flags = leafwikiEdgeFlagSet()
		*flags.internalRuntimeRole = filepath.Join(leafwikiTempDir(), "missing-runtime-role.json")
		Expect(func() {
			_ = runInternalStartupCommand(flags)
		}).To(PanicWithLeafwikiExit(1))

		_, flags = leafwikiEdgeFlagSet()
		*flags.mcp = "invalid-transport"
		Expect(func() {
			_ = resolveStartupMCPTransports(flags, map[string]bool{"mcp": true}, false)
		}).To(PanicWithLeafwikiExit(1))

		Expect(func() {
			validateStartupCommandTransport(true, mcpTransports{Stdio: true}, nil)
		}).To(PanicWithLeafwikiExit(1))
		Expect(func() {
			validateStartupCommandTransport(false, mcpTransports{Stdio: true}, []string{"serve"})
		}).To(PanicWithLeafwikiExit(1))

		Expect(func() {
			validateProxyAuthSettings("not-a-cidr", false)
		}).To(PanicWithLeafwikiExit(1))
		Expect(func() {
			validateProxyAuthSettings("", true)
		}).To(PanicWithLeafwikiExit(1))

		_, flags = leafwikiEdgeFlagSet()
		*flags.markdownLinkRootPrefix = "https://example.test/wiki"
		Expect(func() {
			_ = buildRuntimeConfigForStartup(flags, map[string]bool{"markdown-link-root-prefix": true}, false, mcpTransports{}, leafwikiTempDir())
		}).To(PanicWithLeafwikiExit(1))
	})

	ginkgo.It("resolves startup data directories for service mode without ignoring explicit inputs", func() {
		homeDir := leafwikiTempDir()
		leafwikiSetenv("HOME", homeDir)
		_, flags := leafwikiEdgeFlagSet()

		Expect(resolveStartupDataDir(flags, map[string]bool{}, true)).To(Equal(filepath.Join(homeDir, ".leafwiki")))

		leafwikiSetenv("LEAFWIKI_DATA_DIR", filepath.Join(leafwikiTempDir(), "env-data"))
		Expect(resolveStartupDataDir(flags, map[string]bool{}, true)).To(Equal(os.Getenv("LEAFWIKI_DATA_DIR")))

		*flags.dataDir = filepath.Join(leafwikiTempDir(), "flag-data")
		Expect(resolveStartupDataDir(flags, map[string]bool{"data-dir": true}, true)).To(Equal(*flags.dataDir))

		leafwikiSetenv("HOME", "")
		Expect(func() {
			_ = resolveStartupDataDir(flags, map[string]bool{}, true)
		}).To(PanicWithLeafwikiExit(1))
	})

	ginkgo.It("parses agent-hook commands without treating flag values as providers", func() {
		provider, err := agentHookProviderFromArgsResult([]string{"--config", "leafwiki.yml"})
		Expect(err).To(MatchError(errAgentHookProviderAbsent))
		Expect(provider).To(BeEmpty())

		provider, err = agentHookProviderFromArgsResult([]string{"--config", "leafwiki.yml", "agent-hook", "cursor"})
		Expect(err).To(Succeed())
		Expect(provider).To(Equal(agenthooks.ProviderCursor))

		provider, err = agentHookProviderFromArgsResult([]string{"agent-hook"})
		Expect(err).To(Succeed())
		Expect(provider).To(Equal(agenthooks.ProviderUnknown))

		provider, err = agentHookProviderFromRawArgsResult([]string{"--config", "agent-hook"})
		Expect(err).To(MatchError(errAgentHookProviderAbsent))
		Expect(provider).To(BeEmpty())

		provider, err = agentHookProviderFromRawArgsResult([]string{"--config=leafwiki.yml", "agent-hook"})
		Expect(err).To(Succeed())
		Expect(provider).To(Equal(agenthooks.ProviderUnknown))
	})

	ginkgo.It("recognizes help commands before full startup dispatch", func() {
		Expect(shouldPrintUsage([]string{"help"})).To(BeTrue())
		Expect(shouldPrintUsage([]string{"--help"})).To(BeTrue())
		Expect(shouldPrintUsage([]string{"daemon"})).To(BeFalse())
	})

	ginkgo.It("handles startup help positional commands without launching runtime work", func() {
		output := captureLeafwikiStdout(func() {
			Expect(handleStartupPositionalCommand([]string{"help"}, false, "")).To(BeTrue())
		})

		Expect(output).To(ContainSubstring("Usage: leafwiki [command]"))
	})

	ginkgo.It("resolves daemon service defaults from the current user home", func() {
		homeDir := leafwikiTempDir()
		leafwikiSetenv("HOME", homeDir)

		dataDir, err := defaultDaemonServiceDataDir()
		Expect(err).NotTo(HaveOccurred())
		Expect(dataDir).To(Equal(filepath.Join(homeDir, ".leafwiki")))

		configPath, err := defaultDaemonServiceConfigPath()
		Expect(err).NotTo(HaveOccurred())
		Expect(configPath).To(Equal(filepath.Join(homeDir, ".leafwiki", "leafwiki.yml")))

		fs, flags := leafwikiEdgeFlagSet()
		visited := map[string]bool{}
		Expect(applyDaemonServiceDefaults(fs, flags, visited)).To(Succeed())
		Expect(*flags.dataDir).To(Equal(dataDir))
		Expect(*flags.rootDir).To(Equal(filepath.Join(dataDir, "root")))
		Expect(*flags.host).To(Equal("127.0.0.1"))
		Expect(*flags.port).To(Equal("8080"))
		Expect(*flags.logTarget).To(Equal("file"))
		Expect(visited).To(HaveKey("log-file"))
	})

	ginkgo.It("parses scalar config helpers without invoking failure exits", func() {
		Expect(resolveInt("workers", 7, map[string]bool{"workers": true}, "LEAFWIKI_TEST_WORKERS", 3)).To(Equal(7))
		leafwikiSetenv("LEAFWIKI_TEST_WORKERS", "42")
		Expect(resolveInt("workers", 7, map[string]bool{}, "LEAFWIKI_TEST_WORKERS", 3)).To(Equal(42))
		leafwikiSetenv("LEAFWIKI_TEST_WORKERS", "")
		Expect(resolveInt("workers", 7, map[string]bool{}, "LEAFWIKI_TEST_WORKERS", 3)).To(Equal(3))

		Expect(parseByteSize("1MiB", "upload")).To(Equal(int64(1024 * 1024)))
		parsed, err := parseBoolResult(" ON ")
		Expect(err).To(Succeed())
		Expect(parsed).To(BeTrue())
		parsed, err = parseBoolResult(" off ")
		Expect(err).To(Succeed())
		Expect(parsed).To(BeFalse())
		parsed, err = parseBoolResult("maybe")
		Expect(err).To(MatchError(errBoolValueRejected))
		Expect(parsed).To(BeFalse())

		duration, err := parseDurationResult("1500ms")
		Expect(err).To(Succeed())
		Expect(duration).To(Equal(1500 * time.Millisecond))
		duration, err = parseDurationResult("not-a-duration")
		Expect(err).To(MatchError(errDurationValueRejected))
		Expect(duration).To(BeZero())
	})

	ginkgo.It("fails fast for invalid scalar environment and byte-size values", func() {

		leafwikiSetenv("LEAFWIKI_EDGE_BOOL", "bogus")
		Expect(func() {
			_ = resolveBool("edge-bool", false, map[string]bool{}, "LEAFWIKI_EDGE_BOOL")
		}).To(PanicWithLeafwikiExit(1))

		leafwikiSetenv("LEAFWIKI_EDGE_INT", "bogus")
		Expect(func() {
			_ = resolveInt("edge-int", 0, map[string]bool{}, "LEAFWIKI_EDGE_INT", 1)
		}).To(PanicWithLeafwikiExit(1))

		leafwikiSetenv("LEAFWIKI_EDGE_DURATION", "bogus")
		Expect(func() {
			_ = resolveDuration("edge-duration", 0, map[string]bool{}, "LEAFWIKI_EDGE_DURATION")
		}).To(PanicWithLeafwikiExit(1))

		Expect(func() {
			_ = parseByteSize("bogus", "edge size")
		}).To(PanicWithLeafwikiExit(1))
		Expect(func() {
			_ = parseByteSize("0B", "edge size")
		}).To(PanicWithLeafwikiExit(1))
		Expect(func() {
			_ = parseByteSize("16EiB", "edge size")
		}).To(PanicWithLeafwikiExit(1))
		Expect(func() {
			_ = parseByteSize("8EiB", "edge size")
		}).To(PanicWithLeafwikiExit(1))
	})

	ginkgo.It("returns logger setup errors without replacing the default logger", func() {
		closer, err := setupLogger(leaflogging.Config{Target: leaflogging.Target("bogus")}, io.Discard, io.Discard)
		Expect(err).To(MatchError(leaflogging.ErrInvalidLogTarget))
		Expect(closer).To(BeNil())
	})

	ginkgo.It("formats project daemon identity and role snapshots", func() {
		err := projectDaemonIdentityMismatch(&projectdaemon.Descriptor{
			DataDir: "/owner/data",
			RootDir: "/owner/root",
		}, projectdaemon.Config{
			DataDir: "/requested/data",
			RootDir: "/requested/root",
		})
		Expect(err).To(MatchProjectDaemonConfigMismatch(
			projectdaemon.Mismatch{Field: "data-dir", Want: "/owner/data", Got: "/requested/data"},
			projectdaemon.Mismatch{Field: "root-dir", Want: "/owner/root", Got: "/requested/root"},
		))

		Expect(projectDaemonDescriptorRole(projectdaemon.RuntimeStackWikidFrontd)).To(Equal(projectdaemon.RoleWikid))
		Expect(projectDaemonDescriptorRole("single-process")).To(BeEmpty())
		Expect(projectDaemonDescriptorRoles("single-process", 123, "127.0.0.1:8080", nil)).To(BeNil())

		Expect(projectDaemonDescriptorRoles(projectdaemon.RuntimeStackWikidFrontd, 123, "127.0.0.1:8080", nil)).To(HaveExactElements(
			MatchProjectDaemonRoleHealth(projectdaemon.RoleWikid, projectdaemon.RoleStateReady, gstruct.Fields{
				"PID": Equal(123),
			}),
			MatchProjectDaemonRoleHealth(projectdaemon.RoleFrontd, projectdaemon.RoleStateReady, gstruct.Fields{
				"PID": Equal(123),
				"URL": Equal("http://127.0.0.1:8080"),
			}),
			MatchPrivateProjectDaemonRoleHealth(projectdaemon.RoleWorkspaced, projectdaemon.RoleStateReady, gstruct.Fields{
				"PID": Equal(123),
			}),
		))

		roles := []projectdaemon.RoleHealth{{Name: projectdaemon.RoleWorkspaced, State: projectdaemon.RoleStateCrashed, Error: "boom"}}
		copied := projectDaemonDescriptorRoles(projectdaemon.RuntimeStackWikidFrontd, 123, "127.0.0.1:8080", &wikidFrontdRuntime{roles: roles})
		Expect(copied).To(Equal(roles))
		copied[0].Error = "mutated"
		Expect(roles).To(HaveExactElements(MatchProjectDaemonRoleHealth(projectdaemon.RoleWorkspaced, projectdaemon.RoleStateCrashed, gstruct.Fields{
			"Error": Equal("boom"),
		})))
	})

	ginkgo.It("derives workspace display names and original request paths", func() {
		Expect(federatedWorkspaceDisplayName(projectdaemon.Config{RootDir: "/repo/docs", DataDir: "/data/wiki"})).To(Equal("docs"))
		Expect(federatedWorkspaceDisplayName(projectdaemon.Config{RootDir: string(filepath.Separator), DataDir: "/data/wiki"})).To(Equal("wiki"))
		Expect(federatedWorkspaceDisplayName(projectdaemon.Config{})).To(Equal("Workspace"))

		req := &http.Request{URL: &url.URL{Path: "/mcp"}}
		Expect(originalPath(req)).To(Equal("/mcp"))
		req.Header = make(http.Header)
		req.Header.Set("X-LeafWiki-Original-Path", " /original ")
		Expect(originalPath(req)).To(Equal("/original"))
		Expect(originalPath(&http.Request{})).To(Equal("/"))
	})

	ginkgo.It("writes structured project daemon startup errors", func() {
		path := filepath.Join(leafwikiTempDir(), "startup-error.json")
		writeProjectDaemonStartupError(path, os.ErrPermission)

		var startupErr projectDaemonStartupError
		Expect(os.ReadFile(path)).To(WithTransform(func(raw []byte) error {
			return json.Unmarshal(raw, &startupErr)
		}, Succeed()))
		Expect(startupErr).To(MatchProjectDaemonStartupFailure())
		Expect(formatProjectDaemonStartupError(startupErr)).To(MatchError(errProjectDaemonStartupFailed))

		lockDir := filepath.Join(leafwikiTempDir(), "locked-data")
		dataLock, err := locking.AcquireDataDirLock(lockDir)
		Expect(err).NotTo(HaveOccurred())
		_, lockErr := locking.AcquireDataDirLock(lockDir)
		Expect(lockErr).To(MatchHeldRuntimeLockError())
		lockPath := filepath.Join(leafwikiTempDir(), "lock-startup-error.json")
		writeProjectDaemonStartupError(lockPath, lockErr)
		Expect(dataLock.Release()).To(Succeed())
		var lockStartupErr projectDaemonStartupError
		Expect(os.ReadFile(lockPath)).To(WithTransform(func(raw []byte) error {
			return json.Unmarshal(raw, &lockStartupErr)
		}, Succeed()))
		Expect(lockStartupErr).To(MatchProjectDaemonLockStartupFailure())

		blankPath := filepath.Join(leafwikiTempDir(), "blank.json")
		writeProjectDaemonStartupError(" ", os.ErrPermission)
		writeProjectDaemonStartupError(blankPath, nil)
		Expect(os.Stat(blankPath)).Error().To(MatchError(os.ErrNotExist))

		Expect(runInternalProjectDaemon(context.Background(), filepath.Join(leafwikiTempDir(), "missing.json"))).To(MatchError(os.ErrNotExist))
		badDaemonStartup := filepath.Join(leafwikiTempDir(), "bad-daemon.json")
		Expect(os.WriteFile(badDaemonStartup, []byte("{bad"), 0o600)).To(Succeed())
		Expect(runInternalProjectDaemon(context.Background(), badDaemonStartup)).To(MatchJSONSyntaxError())

		ownerErrPath := filepath.Join(leafwikiTempDir(), "owner-startup.err")
		ownerFailureStartup := filepath.Join(leafwikiTempDir(), "owner-failure.json")
		raw, err := json.Marshal(leafwikiRuntimeConfig{
			DaemonStartupErrorPath: ownerErrPath,
			Workspace:              wiki.Workspace{DataDir: "bad\x00data", RootDir: leafwikiTempDir()},
		})
		Expect(err).NotTo(HaveOccurred())
		Expect(os.WriteFile(ownerFailureStartup, raw, 0o600)).To(Succeed())
		Expect(runInternalProjectDaemon(context.Background(), ownerFailureStartup)).To(MatchPathError())
		var ownerStartupErr projectDaemonStartupError
		Expect(os.ReadFile(ownerErrPath)).To(WithTransform(func(raw []byte) error {
			return json.Unmarshal(raw, &ownerStartupErr)
		}, Succeed()))
		Expect(ownerStartupErr).To(MatchProjectDaemonStartupFailure())
	})

	ginkgo.It("filters native STDIO frames without leaking invalid JSON to the MCP stream", func() {
		var stdout strings.Builder
		pr, pw := io.Pipe()

		Expect(filterNativeStdioJSON(strings.NewReader("{bad-json}\n"), pw, &stdout)).To(Succeed())
		forwarded, err := io.ReadAll(pr)
		Expect(err).NotTo(HaveOccurred())

		Expect(forwarded).To(BeEmpty())
		Expect(stdout.String()).To(MatchNativeStdioParseErrorFrame())
	})

	ginkgo.It("normalizes valid native STDIO frames and preserves IO failures", func() {
		pr, pw := io.Pipe()
		done := make(chan error, 1)
		go func() {
			done <- filterNativeStdioJSON(strings.NewReader(`{"jsonrpc":"2.0"}`), pw, io.Discard)
		}()

		forwarded, err := io.ReadAll(pr)
		Expect(err).NotTo(HaveOccurred())
		Eventually(done).Should(Receive(Succeed()))
		Expect(string(forwarded)).To(Equal(`{"jsonrpc":"2.0"}` + "\n"))

		closedReader, closedForward := io.Pipe()
		readerClosedErr := errors.New("native STDIO reader closed")
		Expect(closedReader.CloseWithError(readerClosedErr)).To(Succeed())
		Expect(filterNativeStdioJSON(strings.NewReader(`{"jsonrpc":"2.0"}`+"\n"), closedForward, io.Discard)).To(MatchError(readerClosedErr))

		partialReader, partialForward := io.Pipe()
		done = make(chan error, 1)
		go func() {
			done <- filterNativeStdioJSON(strings.NewReader(`{"jsonrpc":"2.0"}`), partialForward, io.Discard)
		}()
		frame := make([]byte, len(`{"jsonrpc":"2.0"}`))
		_, err = io.ReadFull(partialReader, frame)
		Expect(err).NotTo(HaveOccurred())
		Expect(string(frame)).To(Equal(`{"jsonrpc":"2.0"}`))
		newlineErr := errors.New("newline rejected")
		Expect(partialReader.CloseWithError(newlineErr)).To(Succeed())
		Eventually(done).Should(Receive(MatchError(newlineErr)))

		stdoutErr := errors.New("stdout closed")
		_, failingStdoutForward := io.Pipe()
		Expect(filterNativeStdioJSON(strings.NewReader("{bad-json}\n"), failingStdoutForward, leafwikiFailWriter{err: stdoutErr})).To(MatchError(stdoutErr))

		readErr := errors.New("stdin failed")
		_, failingReadForward := io.Pipe()
		Expect(filterNativeStdioJSON(leafwikiErrReader{err: readErr}, failingReadForward, io.Discard)).To(MatchError(readErr))
	})

	ginkgo.It("classifies native STDIO close errors narrowly", func() {
		Expect(error(nil)).To(BeCleanNativeStdioClose())
		Expect(io.EOF).To(BeCleanNativeStdioClose())
		Expect(errors.New("server is closing: EOF")).To(BeCleanNativeStdioClose())
		Expect(errors.New("broken pipe")).NotTo(BeCleanNativeStdioClose())
	})

	ginkgo.It("parses startup diagnostics and trusts only local daemon control URLs", func() {
		structured := parseProjectDaemonStartupError([]byte(`{"message":" ` + leafwikiFixtureDaemonStopped + ` "}`))
		Expect(structured).To(MatchProjectDaemonStartupFailure())
		Expect(formatProjectDaemonStartupError(structured)).To(MatchError(errProjectDaemonStartupFailed))

		lockStartup := parseProjectDaemonStartupError([]byte("acquire data directory lock: /tmp/wiki"))
		Expect(formatProjectDaemonStartupError(lockStartup)).To(MatchError(errProjectLockedNoAttachableDaemon))

		Expect(" http://localhost:8080/control ").To(BeTrustedDaemonControlURL())
		Expect("http://127.0.0.1:8080/control").To(BeTrustedDaemonControlURL())
		Expect("http://[::1]:8080/control").To(BeTrustedDaemonControlURL())
		Expect("https://localhost:8080/control").NotTo(BeTrustedDaemonControlURL())
		Expect("http://example.com/control").NotTo(BeTrustedDaemonControlURL())
		Expect("http://%zz").NotTo(BeTrustedDaemonControlURL())
	})

	ginkgo.It("classifies project daemon lock states from real data and root locks", func() {
		baseDir := leafwikiTempDir()
		dataDir := filepath.Join(baseDir, "data")
		rootDir := filepath.Join(baseDir, "root")
		Expect(os.MkdirAll(rootDir, 0o755)).To(Succeed())

		Expect(projectDaemonLocks(dataDir, rootDir)).To(HaveAvailableProjectDaemonLocks())
		Expect(projectDaemonLocks(dataDir, rootDir)).NotTo(HaveHeldProjectDaemonLocks())

		dataLock, err := locking.AcquireDataDirLock(dataDir)
		Expect(err).NotTo(HaveOccurred())
		ginkgo.DeferCleanup(dataLock.Release)
		rootLock, err := locking.AcquireRootDirLock(rootDir)
		Expect(err).NotTo(HaveOccurred())
		ginkgo.DeferCleanup(rootLock.Release)

		Expect(projectDaemonLocks(dataDir, rootDir)).To(HaveHeldProjectDaemonLocks())

		Expect(projectDaemonLocks(dataDir, rootDir)).NotTo(HaveAvailableProjectDaemonLocks())

		Expect(projectDaemonLocks(dataDir, rootDir)).NotTo(HaveFreeDataLockWithHeldRootLock())

		Expect(dataLock.Release()).To(Succeed())
		Expect(projectDaemonLocks(dataDir, rootDir)).To(HaveFreeDataLockWithHeldRootLock())

		Expect(rootLock.Release()).To(Succeed())
		Expect(projectDaemonLocks(dataDir, rootDir)).NotTo(HaveFreeDataLockWithHeldRootLock())

		fileDataDir := filepath.Join(baseDir, "file-data")
		Expect(os.WriteFile(fileDataDir, []byte("not a directory"), 0o600)).To(Succeed())
		Expect(projectDaemonLocks(fileDataDir, rootDir)).To(MatchProjectDaemonLockAvailabilityError(syscall.ENOTDIR))
	})

	ginkgo.It("compares daemon descriptor health and request identity variants", func() {
		desc := &projectdaemon.Descriptor{
			SchemaVersion: 1,
			PID:           1234,
			DataDir:       "/data",
			RootDir:       "/root",
			ConfigHash:    "hash",
		}
		health := &projectdaemon.DaemonHealth{
			SchemaVersion: 1,
			PID:           1234,
			DataDir:       "/data",
			RootDir:       "/root",
			ConfigHash:    "hash",
		}
		Expect(health).To(MatchDaemonHealthDescriptor(desc))
		Expect(health).NotTo(MatchDaemonHealthDescriptor(nil))
		health.PID = 4321
		Expect(health).NotTo(MatchDaemonHealthDescriptor(desc))

		owner := completeDaemonCompareConfig()
		requested := owner
		requested.Host = "0.0.0.0"
		requested.Port = "9999"
		requested.PublicMCPEnabled = true
		requested.LogTarget = "file"
		requested.LogFile = "/tmp/leafwiki.log"
		requested.DisableRequestLog = true
		Expect(compareProjectDaemonConfigForRequest(owner, requested, mcpTransports{Stdio: true})).To(BeEmpty())
		Expect(compareProjectDaemonConfigForRequest(owner, requested, mcpTransports{Stdio: true, HTTP: true})).To(ContainElement(HaveField("Field", Equal("public-mcp-enabled"))))

		Expect(compareProjectDaemonDescriptorForRequest(nil, requested, mcpTransports{})).To(BeNil())
		desc = &projectdaemon.Descriptor{
			Role:          projectdaemon.RoleWorkspaced,
			PrivateMCPURL: "http://127.0.0.1/private",
			WorkspaceID:   "owner-workspace",
			Config:        owner,
		}
		requested = owner
		requested.WorkspaceID = "requested-workspace"
		desc.Config.WorkspaceID = requested.WorkspaceID
		mismatches := compareProjectDaemonDescriptorForRequest(desc, requested, mcpTransports{Stdio: true})
		Expect(mismatches).To(ContainElement(Satisfy(func(mismatch projectdaemon.Mismatch) bool {
			return mismatch.Field == "workspace-id" &&
				mismatch.Want == "owner-workspace" &&
				mismatch.Got == "requested-workspace"
		})))
	})

	ginkgo.It("normalizes daemon runtime config and log paths for owner and workspace requests", func() {
		dataDir := filepath.Join(leafwikiTempDir(), "data")
		rootDir := filepath.Join(leafwikiTempDir(), "root")
		logPath := filepath.Join(dataDir, ".leafwiki", "logs", "leafwiki.log")

		cfg := leafwikiRuntimeConfig{
			Workspace: wiki.Workspace{
				ID:      "home",
				DataDir: dataDir,
				RootDir: rootDir,
			},
			RuntimeStack:            projectdaemon.RuntimeStackWikidFrontd,
			Host:                    "127.0.0.1",
			Port:                    "8080",
			PublicAccess:            true,
			AllowInsecure:           true,
			AccessTokenTimeout:      time.Hour,
			RefreshTokenTimeout:     2 * time.Hour,
			InjectCodeInHeader:      "secret-header",
			Logging:                 leaflogging.Config{Target: leaflogging.TargetFile, FilePath: logPath},
			DisableAuth:             true,
			MaxAssetUploadSize:      1234,
			MCPTransports:           mcpTransports{HTTP: true},
			EnableLinkRefactor:      true,
			EnableHTTPRemoteUser:    true,
			HTTPRemoteUserHeader:    "Remote-User",
			TrustedProxyIPsRaw:      "127.0.0.1",
			HTTPRemoteUserLogoutURL: "/logout",
			DisableRequestLog:       true,
			DaemonIdleTimeout:       5 * time.Minute,
			MarkdownLinkRootPrefix:  "/docs",
		}

		daemonCfg, err := daemonConfigForRuntime(cfg)
		Expect(err).NotTo(HaveOccurred())
		expectedDataDir, expectedRootDir, err := projectdaemon.CanonicalizeProject(dataDir, rootDir)
		Expect(err).NotTo(HaveOccurred())
		Expect(daemonCfg).To(MatchWorkspaceSyncEnabledDaemonConfig(gstruct.Fields{
			"WorkspaceID":            Equal(workspaceid.WorkspaceID("home")),
			"DataDir":                Equal(expectedDataDir),
			"RootDir":                Equal(expectedRootDir),
			"LogFile":                Equal(filepath.Join(expectedDataDir, ".leafwiki", "logs", "leafwiki.log")),
			"InjectCodeInHeaderHash": Not(BeEmpty()),
			"MarkdownLinkRootPrefix": Equal("/docs"),
		}))

		cfg.APIKey = "sk-test"
		cfg.Logging = leaflogging.Config{Target: leaflogging.TargetStderr}
		cfg.MCPTransports = mcpTransports{Stdio: true}
		workspaceCfg, err := daemonWorkspaceRuntimeConfig(cfg)
		Expect(err).NotTo(HaveOccurred())
		Expect(workspaceCfg).To(gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
			"APIKey":       BeEmpty(),
			"RuntimeStack": Equal(projectdaemon.RuntimeStackWikidFrontd),
			"Logging": gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
				"Target":   Equal(leaflogging.TargetFile),
				"FilePath": Equal(filepath.Join(dataDir, ".leafwiki", "logs", "leafwiki.log")),
			}),
		}))

		rel, err := localRelativePathResult(dataDir, filepath.Join(dataDir, "nested", "leafwiki.log"))
		Expect(err).To(Succeed())
		Expect(rel).To(Equal(filepath.Join("nested", "leafwiki.log")))
		_, err = localRelativePathResult(dataDir, dataDir)
		Expect(err).To(MatchError(errRelativePathOutsideBase))
		_, err = localRelativePathResult(dataDir, filepath.Dir(dataDir))
		Expect(err).To(MatchError(errRelativePathOutsideBase))

		canonicalDataDir := filepath.Join(leafwikiTempDir(), "canonical")
		Expect(daemonLogFileForConfig(leafwikiRuntimeConfig{}, canonicalDataDir)).To(BeEmpty())
		cfg.Workspace.DataDir = filepath.Join(leafwikiTempDir(), "original")
		cfg.Logging = leaflogging.Config{Target: leaflogging.TargetFile, FilePath: filepath.Join(canonicalDataDir, ".leafwiki", "logs", "leafwiki.log")}
		Expect(daemonLogFileForConfig(cfg, canonicalDataDir)).To(Equal(filepath.Clean(cfg.Logging.FilePath)))
		cfg.Logging.FilePath = filepath.Join(leafwikiTempDir(), "external.log")
		Expect(daemonLogFileForConfig(cfg, canonicalDataDir)).To(Equal(filepath.Clean(cfg.Logging.FilePath)))
	})

	ginkgo.It("handles runtime role lookup, HTTP tokens, and response encoding errors", func() {
		roles := []projectdaemon.RoleHealth{
			{Name: projectdaemon.RoleFrontd, URL: "http://127.0.0.1:8080"},
		}
		Expect(roleURL(roles, projectdaemon.RoleFrontd)).To(Equal("http://127.0.0.1:8080"))
		Expect(roleURL(roles, projectdaemon.RoleWorkspaced)).To(BeEmpty())
		role, err := runtimeRoleHealthResult(roles, projectdaemon.RoleFrontd)
		Expect(err).To(Succeed())
		Expect(role.Name).To(Equal(projectdaemon.RoleFrontd))
		_, err = runtimeRoleHealthResult(roles, projectdaemon.RoleWorkspaced)
		Expect(err).To(MatchError(errRuntimeRoleAbsent))

		Expect(httpBearerToken(nil)).To(BeEmpty())
		req := httptest.NewRequest(http.MethodGet, "http://leafwiki.local/mcp", nil)
		req.Header.Set("Authorization", "bearer token-123 ")
		Expect(httpBearerToken(req)).To(Equal("token-123"))
		req.Header.Set("Authorization", "Basic token-123")
		Expect(httpBearerToken(req)).To(BeEmpty())

		Expect(accessTokenFromHTTPRequest(nil)).To(BeEmpty())
		req = httptest.NewRequest(http.MethodGet, "http://leafwiki.local/", nil)
		req.AddCookie(&http.Cookie{Name: "leafwiki_at", Value: " "})
		req.AddCookie(&http.Cookie{Name: "__Host-leafwiki_at", Value: " cookie-token "})
		Expect(accessTokenFromHTTPRequest(req)).To(Equal("cookie-token"))

		rec := httptest.NewRecorder()
		writeRuntimeJSON(rec, map[string]string{"ok": "true"})
		Expect(rec).To(HaveHTTPHeaderWithValue("Content-Type", "application/json"))
		Expect(rec).To(HaveHTTPBody(ContainSubstring(`"ok":"true"`)))

		writeRuntimeJSON(httptest.NewRecorder(), func() {})
		failingWriter := &leafwikiFailingResponseWriter{err: errors.New("write failed")}
		writeRuntimeError(failingWriter, http.StatusForbidden, runtimeErrorCodeWorkspaceGrantDenied)
		Expect(failingWriter.statuses).To(ContainElement(http.StatusForbidden))
	})

	ginkgo.It("maps actor contexts, runtime grants, and home workspace status edges", func() {
		_, err := actorContextForUser(nil, "api_key", leafwikiRuntimeConfig{})
		Expect(err).To(MatchError(errRuntimeActorUserRequired))

		actor, err := actorContextForUser(&coreauth.User{
			ID:       "u1",
			Username: "ada",
			Email:    "ada@example.test",
			Role:     coreauth.RoleEditor,
		}, "api_key", leafwikiRuntimeConfig{Workspace: wiki.Workspace{ID: "workspace-a"}})
		Expect(err).NotTo(HaveOccurred())
		Expect(actor).To(gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
			"Subject":     Equal("user:u1"),
			"WorkspaceID": Equal(workspaceid.WorkspaceID("workspace-a")),
			"Scopes":      ContainElement("leafwiki:mcp"),
		}))

		Expect(wikidGrantRoleForCoreRole(coreauth.RoleViewer)).To(Equal(wikid.GrantRoleViewer))
		Expect(wikidGrantRoleForCoreRole(coreauth.RoleEditor)).To(Equal(wikid.GrantRoleEditor))
		Expect(wikidGrantRoleForCoreRole(coreauth.RoleAdmin)).To(Equal(wikid.GrantRoleAdmin))
		Expect(wikidGrantRoleForCoreRole("owner")).To(BeEmpty())
		Expect(scopesForGrantRole("owner")).To(BeNil())
		Expect(ensureRuntimeHomeGrant(nil, nil)).To(MatchError(errRuntimeHomeGrantUserRequired))
		Expect(ensureRuntimeHomeGrant(nil, &coreauth.User{Role: "owner"})).To(Succeed())

		now := time.Now().UTC()
		syncHomeWorkspaceStatus(nil, nil)
		supervisor := wikid.NewWorkspaceSupervisor(wikid.WorkspaceSupervisorOptions{Now: func() time.Time { return now }})
		syncHomeWorkspaceStatus(supervisor, nil)
		Expect(supervisor.Status(wikid.HomeWorkspaceID).State).To(Equal(wikid.WorkspaceStateRegistered))

		syncHomeWorkspaceStatus(supervisor, []projectdaemon.RoleHealth{{
			Name:      projectdaemon.RoleWorkspaced,
			State:     projectdaemon.RoleStateStarting,
			PID:       22,
			URL:       " http://127.0.0.1:8001 ",
			UpdatedAt: now,
		}})
		status := supervisor.Status(wikid.HomeWorkspaceID)
		Expect(status).To(gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
			"State": Equal(wikid.WorkspaceStateStarting),
			"URL":   Equal("http://127.0.0.1:8001"),
		}))

		syncHomeWorkspaceStatus(supervisor, []projectdaemon.RoleHealth{{
			Name:      projectdaemon.RoleWorkspaced,
			State:     projectdaemon.RoleStateStopped,
			Error:     "stopped",
			UpdatedAt: now.Add(time.Second),
		}})
		status = supervisor.Status(wikid.HomeWorkspaceID)
		Expect(status).To(gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
			"State": Equal(wikid.WorkspaceStateCrashed),
			"Error": Equal("stopped"),
		}))

		syncHomeWorkspaceStatus(supervisor, []projectdaemon.RoleHealth{{
			Name:      projectdaemon.RoleWorkspaced,
			State:     projectdaemon.RoleStateDegraded,
			UpdatedAt: now.Add(2 * time.Second),
		}})
		Expect(supervisor.Status(wikid.HomeWorkspaceID).State).To(Equal(wikid.WorkspaceStateRegistered))
	})

	ginkgo.It("preserves runtime tokens, actor resolution, restart handling, and readiness waits", func() {
		w := newFrontdActorTestWiki()
		ginkgo.DeferCleanup(w.Close)
		cfg := leafwikiRuntimeConfig{
			Workspace:   wiki.Workspace{ID: "home"},
			DisableAuth: true,
		}

		req := httptest.NewRequest(http.MethodGet, "/mcp", nil)
		actor, err := frontdActorResolver(w, cfg)(req)
		Expect(err).NotTo(HaveOccurred())
		Expect(actor).To(gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
			"Subject":    Equal("user:public-editor"),
			"AuthMethod": Equal("disabled"),
		}))

		_, err = frontdMCPTokenVerifier(&wiki.Wiki{})(context.Background(), "not-an-api-key", req)
		Expect(err).To(MatchError(sdkauth.ErrInvalidToken))
		_, err = frontdMCPTokenVerifier(&wiki.Wiki{})(context.Background(), "lwk_key_secret", req)
		Expect(err).To(MatchError(sdkauth.ErrInvalidToken))

		editor, err := w.UserService().CreateUser("token-editor", "token-editor@example.com", "password", coreauth.RoleEditor)
		Expect(err).NotTo(HaveOccurred())
		editorID := coreauth.UserIDFromString(editor.ID)
		created, err := w.APIKeyService().CreateAPIKey(editorID, "MCP client", editorID)
		Expect(err).NotTo(HaveOccurred())
		info, err := frontdMCPTokenVerifier(w)(context.Background(), created.Secret, req)
		Expect(err).NotTo(HaveOccurred())
		Expect(info.UserID).To(Equal(string(editor.ID)))

		rec := httptest.NewRecorder()
		handleWikidTokenVerify(rec, httptest.NewRequest(http.MethodPost, "/__leafwiki/token/verify", nil), w)
		Expect(rec).To(HaveHTTPStatus(http.StatusUnauthorized))

		rec = httptest.NewRecorder()
		invalidReq := httptest.NewRequest(http.MethodPost, "/__leafwiki/token/verify", nil)
		invalidReq.Header.Set("Authorization", "Bearer invalid")
		handleWikidTokenVerify(rec, invalidReq, w)
		Expect(rec).To(HaveHTTPStatus(http.StatusUnauthorized))

		rec = httptest.NewRecorder()
		validReq := httptest.NewRequest(http.MethodPost, "/__leafwiki/token/verify", nil)
		validReq.Header.Set("Authorization", "Bearer "+created.Secret)
		handleWikidTokenVerify(rec, validReq, w)
		Expect(rec).To(HaveHTTPStatus(http.StatusOK))
		Expect(rec).To(HaveHTTPBody(ContainSubstring(string(editor.ID))))

		authenticatedResolver := frontdMCPActorResolver(&wiki.Wiki{}, leafwikiRuntimeConfig{})
		_, err = authenticatedResolver(req)
		Expect(err).To(MatchError(errFrontdMCPTokenInfoMissing))

		disabledResolver := frontdMCPActorResolver(w, cfg)
		actor, err = disabledResolver(req)
		Expect(err).NotTo(HaveOccurred())
		Expect(actor).To(gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
			"AuthMethod": Equal("disabled"),
		}))

		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		Expect(waitForInternalRuntimeRoleSignal(ctx)).To(Succeed())

		sessions := projectdaemon.NewSessionRegistry(time.Millisecond, nil)
		_, err = sessions.Register()
		Expect(err).NotTo(HaveOccurred())
		Expect(sessions.Count()).To(Equal(1))
		Expect(waitForFirstProjectDaemonSession(context.Background(), sessions, time.Millisecond)).To(Succeed())
		Expect(waitForFirstProjectDaemonSession(context.Background(), sessions, 0)).To(Succeed())

		cancelCtx, cancelWait := context.WithCancel(context.Background())
		cancelWait()
		Expect(waitForFirstProjectDaemonSession(cancelCtx, projectdaemon.NewSessionRegistry(time.Millisecond, nil), time.Millisecond)).To(MatchError(context.Canceled))
		Expect(waitForFirstProjectDaemonActivity(context.Background(), sessions, projectdaemon.NewAgentPresenceRegistry(time.Millisecond, nil), time.Millisecond)).To(Succeed())
		Expect(waitForFirstProjectDaemonActivity(context.Background(), sessions, projectdaemon.NewAgentPresenceRegistry(time.Millisecond, nil), 0)).To(Succeed())
		Expect(waitForFirstProjectDaemonActivity(cancelCtx, projectdaemon.NewSessionRegistry(time.Millisecond, nil), projectdaemon.NewAgentPresenceRegistry(time.Millisecond, nil), time.Millisecond)).To(MatchError(context.Canceled))

		idleCtx, idleCancelContext := context.WithCancel(context.Background())
		idleCancel := func() {
			idleCancelContext()
		}
		idleCallback := idleShutdownCallback(idleCtx, idleCancel, time.Hour, nil)
		idleCallback(1)
		idleCallback(1)
		Consistently(idleCtx.Done()).WithTimeout(25 * time.Millisecond).ShouldNot(BeClosed())

		seenSessions := projectdaemon.NewSessionRegistry(time.Millisecond, nil)
		_, err = seenSessions.Register()
		Expect(err).NotTo(HaveOccurred())
		noSessionCtx, noSessionCancel := context.WithCancel(context.Background())
		cancelIfNoSessionAfterStartupGrace(context.Background(), noSessionCancel, seenSessions, 0)
		Consistently(noSessionCtx.Done()).WithTimeout(25 * time.Millisecond).ShouldNot(BeClosed())

		noActivityCtx, noActivityCancel := context.WithCancel(context.Background())
		cancelIfNoActivityAfterStartupGrace(context.Background(), noActivityCancel, seenSessions, projectdaemon.NewAgentPresenceRegistry(time.Millisecond, nil), 0)
		Consistently(noActivityCtx.Done()).WithTimeout(25 * time.Millisecond).ShouldNot(BeClosed())

		parentSessionCtx, parentSessionCancel := context.WithCancel(context.Background())
		cancelIfNoSessionAfterStartupGrace(cancelCtx, parentSessionCancel, projectdaemon.NewSessionRegistry(time.Millisecond, nil), time.Hour)
		Consistently(parentSessionCtx.Done()).WithTimeout(25 * time.Millisecond).ShouldNot(BeClosed())

		parentActivityCtx, parentActivityCancel := context.WithCancel(context.Background())
		cancelIfNoActivityAfterStartupGrace(cancelCtx, parentActivityCancel, projectdaemon.NewSessionRegistry(time.Millisecond, nil), projectdaemon.NewAgentPresenceRegistry(time.Millisecond, nil), time.Hour)
		Consistently(parentActivityCtx.Done()).WithTimeout(25 * time.Millisecond).ShouldNot(BeClosed())

		runtimeCtx, runtimeCancel := context.WithCancel(context.Background())
		runtimeCancel()
		runtime := &wikidFrontdRuntime{
			ctx:        runtimeCtx,
			supervisor: wikid.NewSupervisor(wikid.SupervisorOptions{}),
			processes:  map[projectdaemon.RoleName]*internalRuntimeRoleProcess{},
		}
		runtime.restartRole(projectdaemon.RoleFrontd)

		activeRuntime := &wikidFrontdRuntime{
			ctx:        context.Background(),
			supervisor: wikid.NewSupervisor(wikid.SupervisorOptions{}),
			processes:  map[projectdaemon.RoleName]*internalRuntimeRoleProcess{},
		}
		activeRuntime.restartRole(projectdaemon.RoleName("unsupported"))
	})

	ginkgo.It("resolves private endpoints, wikid tokens, and frontd actors", func() {
		w := newFrontdActorTestWiki()
		ginkgo.DeferCleanup(w.Close)
		editor, err := w.UserService().CreateUser("edge-editor", "edge-editor@example.com", "password", coreauth.RoleEditor)
		Expect(err).NotTo(HaveOccurred())
		editorID := coreauth.UserIDFromString(editor.ID)
		apiKey, err := w.APIKeyService().CreateAPIKey(editorID, "edge mcp", editorID)
		Expect(err).NotTo(HaveOccurred())

		var seenPaths []string
		var seenAuthorization []string
		var seenControlTokens []string
		privateServer := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
			seenPaths = append(seenPaths, req.URL.Path)
			seenAuthorization = append(seenAuthorization, req.Header.Get("Authorization"))
			seenControlTokens = append(seenControlTokens, req.Header.Get(projectdaemon.ControlTokenHeader))
			Expect(req.Header.Get(projectdaemon.ActorContextHeader)).To(BeEmpty())
			switch req.URL.Path {
			case "/__leafwiki/token/verify":
				writeRuntimeJSON(rw, map[string]any{
					"userId":     editor.ID,
					"scopes":     []string{"leafwiki:mcp"},
					"expiration": time.Now().UTC().Add(time.Hour),
				})
			case "/__leafwiki/actor-context":
				writeRuntimeJSON(rw, map[string]any{
					"actor": projectdaemon.ActorContext{Subject: "user:" + editor.ID, Username: editor.Username},
				})
			case "/discard":
				rw.WriteHeader(http.StatusNoContent)
			case "/structured-error":
				writeRuntimeError(rw, http.StatusForbidden, errCodeStdioAuthAPIKeyInvalid)
			default:
				http.Error(rw, "missing", http.StatusNotFound)
			}
		}))
		ginkgo.DeferCleanup(privateServer.Close)

		info, err := wikidMCPTokenVerifier(privateServer.URL+"/", "daemon-token")(context.Background(), " edge-token ", nil)
		Expect(err).NotTo(HaveOccurred())
		Expect(info).To(gstruct.PointTo(gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
			"UserID": Equal(editor.ID),
			"Scopes": ContainElement("leafwiki:mcp"),
		})))
		Expect(struct {
			Paths          []string
			Authorizations []string
			ControlTokens  []string
		}{seenPaths, seenAuthorization, seenControlTokens}).To(gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
			"Paths":          ContainElement("/__leafwiki/token/verify"),
			"Authorizations": ContainElement("Bearer edge-token"),
			"ControlTokens":  ContainElement("daemon-token"),
		}))
		_, err = wikidMCPTokenVerifier("http://[::1", "daemon-token")(context.Background(), "edge-token", nil)
		Expect(err).To(MatchError(sdkauth.ErrInvalidToken))

		req := httptest.NewRequest(http.MethodPatch, "/source", nil)
		req.RemoteAddr = "127.0.0.1:9191"
		req.Header.Set(projectdaemon.ActorContextHeader, "caller-supplied")
		var out struct {
			Actor projectdaemon.ActorContext `json:"actor"`
		}
		Expect(callWikidPrivateEndpoint(context.Background(), privateServer.URL, "daemon-token", "/__leafwiki/actor-context", req, &out)).To(Succeed())
		Expect(out.Actor.Subject).To(Equal("user:" + editor.ID))
		Expect(callWikidPrivateEndpoint(context.Background(), privateServer.URL, "daemon-token", "/discard", nil, nil)).To(Succeed())
		nilHeaderSource := &http.Request{Method: http.MethodPut, URL: &url.URL{Path: "/nil-header"}}
		Expect(callWikidPrivateEndpoint(context.Background(), privateServer.URL, "daemon-token", "/discard", nilHeaderSource, nil)).To(Succeed())
		Expect(callWikidPrivateEndpoint(context.Background(), "http://127.0.0.1:1", "daemon-token", "/discard", nil, nil)).To(MatchURLError())

		actor, err := wikidActorResolver(privateServer.URL, "daemon-token")(httptest.NewRequest(http.MethodGet, "/mcp", nil))
		Expect(err).NotTo(HaveOccurred())
		Expect(actor.Subject).To(Equal("user:" + editor.ID))
		_, err = wikidActorResolver("http://[::1", "daemon-token")(httptest.NewRequest(http.MethodGet, "/mcp", nil))
		Expect(err).To(MatchURLError())

		_, err = frontdPublicMCPHandler(leafwikiRuntimeConfig{}, "://bad-upstream", "daemon-token", privateServer.URL)
		Expect(err).To(MatchInvalidWorkspacedUpstream())

		err = callWikidPrivateEndpoint(context.Background(), privateServer.URL, "daemon-token", "/structured-error", nil, nil)
		Expect(err).To(MatchWikidPrivateEndpointStatus(http.StatusForbidden))
		Expect(isWikidPrivateAuthFailure(err)).To(BeTrue())
		var endpointErr *wikidPrivateEndpointError
		Expect(err).To(Satisfy(func(err error) bool {
			return errors.As(err, &endpointErr)
		}))
		Expect(endpointErr).To(testmatchers.HaveStructuredError(errCodeStdioAuthAPIKeyInvalid, sharederrors.MessageIDForCode(errCodeStdioAuthAPIKeyInvalid)))
		Expect((*wikidPrivateEndpointError)(nil).Error()).To(BeEmpty())
		Expect((&wikidPrivateEndpointError{Path: "/empty", StatusCode: 499}).StatusCode).To(Equal(499))
		Expect(isWikidPrivateAuthFailure(errors.New("plain"))).To(BeFalse())
		Expect(isWikidPrivateAuthFailure(&wikidPrivateEndpointError{StatusCode: http.StatusInternalServerError})).To(BeFalse())

		_, err = wikidMCPTokenVerifier(privateServer.URL, "daemon-token")(context.Background(), "edge-token", httptest.NewRequest(http.MethodGet, "/mcp", nil))
		Expect(err).NotTo(HaveOccurred())
		_, err = wikidMCPTokenVerifier(privateServer.URL, "daemon-token")(context.Background(), "edge-token", httptest.NewRequest(http.MethodGet, "/missing", nil))
		Expect(err).NotTo(HaveOccurred())

		cfg := leafwikiRuntimeConfig{Workspace: wiki.Workspace{ID: "workspace-a"}}
		resolver := frontdMCPActorResolver(w, cfg)
		resolved, resolveErr := resolveWithSDKToken(resolver, apiKey.Secret, editor.ID)
		Expect(resolveErr).NotTo(HaveOccurred())
		Expect(resolved).To(gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
			"Subject":    Equal("user:" + editor.ID),
			"AuthMethod": Equal("api_key"),
		}))

		resolved, resolveErr = resolveWithSDKToken(resolver, "oauth-token", editor.ID)
		Expect(resolveErr).NotTo(HaveOccurred())
		Expect(resolved).To(gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
			"AuthMethod": Equal("oauth"),
		}))

		_, resolveErr = resolveWithSDKToken(frontdMCPActorResolver(&wiki.Wiki{}, cfg), "oauth-token", editor.ID)
		Expect(resolveErr).To(MatchError(errFrontdMCPUserServiceUnavailable))

		_, resolveErr = resolveWithSDKToken(resolver, "oauth-token", "missing-user")
		Expect(resolveErr).To(MatchError(coreauth.ErrUserNotFound))

		_, _, err = frontdActorUser(httptest.NewRequest(http.MethodGet, "/mcp", nil), &wiki.Wiki{}, leafwikiRuntimeConfig{})
		Expect(err).To(MatchError(errFrontdWorkspaceCredentialsMissing))
		_, err = frontdActorResolver(w, leafwikiRuntimeConfig{})(httptest.NewRequest(http.MethodGet, "/mcp", nil))
		Expect(err).To(MatchError(errFrontdWorkspaceCredentialsMissing))

		mcpReq := httptest.NewRequest(http.MethodPost, "/mcp", nil)
		mcpReq.Header.Set("Authorization", "Bearer lwk_key_invalid")
		_, _, err = frontdActorUser(mcpReq, w, leafwikiRuntimeConfig{})
		Expect(err).To(MatchError(coreauth.ErrInvalidToken))

		oauthReq := httptest.NewRequest(http.MethodPost, "/mcp", nil)
		oauthReq.Header.Set("Authorization", "Bearer oauth-token")
		_, _, err = frontdActorUser(oauthReq, &wiki.Wiki{}, leafwikiRuntimeConfig{})
		Expect(err).To(MatchError(errFrontdOAuthActorServicesUnavailable))
		_, _, err = frontdActorUser(oauthReq, w, leafwikiRuntimeConfig{})
		Expect(err).To(MatchError(sdkauth.ErrInvalidToken))

		cookieReq := httptest.NewRequest(http.MethodGet, "/", nil)
		cookieReq.AddCookie(&http.Cookie{Name: "leafwiki_at", Value: "invalid-token"})
		_, _, err = frontdActorUser(cookieReq, w, leafwikiRuntimeConfig{})
		Expect(err).To(MatchError(coreauth.ErrInvalidToken))

		publicReq := httptest.NewRequest(http.MethodGet, "/", nil)
		publicUser, method, err := frontdActorUser(publicReq, &wiki.Wiki{}, leafwikiRuntimeConfig{PublicAccess: true})
		Expect(err).NotTo(HaveOccurred())
		Expect(publicUser.ID).To(Equal("public-viewer"))
		Expect(method).To(Equal("public_access"))
	})

	ginkgo.It("orchestrates wikid-frontd runtime roles through the starter boundary", func() {
		var calls []internalRuntimeRoleStartupConfig
		var doneChans []chan error
		swapInternalRuntimeRoleStarter(func(startup internalRuntimeRoleStartupConfig) (*internalRuntimeRoleProcess, internalRuntimeRoleReady, error) {
			calls = append(calls, startup)
			proc, done := newLeafwikiRuntimeRoleProcess(startup.Role, 100+len(calls))
			doneChans = append(doneChans, done)
			switch startup.Role {
			case projectdaemon.RoleWorkspaced:
				return proc, internalRuntimeRoleReady{Role: startup.Role, PID: proc.pid, URL: "http://workspaced.local", Private: true}, nil
			case projectdaemon.RoleFrontd:
				return proc, internalRuntimeRoleReady{Role: startup.Role, PID: proc.pid, URL: "http://frontd.local"}, nil
			default:
				return nil, internalRuntimeRoleReady{}, errors.New("unexpected role")
			}
		})

		runtime, err := startWikidFrontdRuntime(context.Background(), leafwikiRuntimeConfig{}, "daemon-token", "http://wikid.local")
		Expect(err).NotTo(HaveOccurred())
		ginkgo.DeferCleanup(func() {
			Expect(runtime.stop(context.Background())).To(Succeed())
			releaseLeafwikiRuntimeRoleProcesses(doneChans, context.Canceled)
		})

		Expect(calls).To(HaveExactElements(
			gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
				"Role": Equal(projectdaemon.RoleWorkspaced),
				"Runtime": gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
					"Host": Equal("127.0.0.1"),
					"Port": Equal("0"),
				}),
			}),
			gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
				"Role":          Equal(projectdaemon.RoleFrontd),
				"WorkspacedURL": Equal("http://workspaced.local"),
				"WikidURL":      Equal("http://wikid.local"),
			}),
		))
		Expect(runtime.workspacedURL).To(Equal("http://workspaced.local"))
		Expect(runtime.roleHealthSnapshot()).To(ContainElement(Satisfy(func(role projectdaemon.RoleHealth) bool {
			return role.Name == projectdaemon.RoleFrontd && role.URL == "http://frontd.local"
		})))
	})

	ginkgo.It("reports wikid-frontd runtime startup and restart failures", func() {
		workspacedErr := errors.New("workspaced failed")
		swapInternalRuntimeRoleStarter(func(startup internalRuntimeRoleStartupConfig) (*internalRuntimeRoleProcess, internalRuntimeRoleReady, error) {
			return nil, internalRuntimeRoleReady{}, workspacedErr
		})
		runtime, err := startWikidFrontdRuntime(context.Background(), leafwikiRuntimeConfig{}, "daemon-token", "http://wikid.local")
		Expect(runtime).To(BeNil())
		Expect(err).To(MatchError(workspacedErr))

		var doneChans []chan error
		frontdErr := errors.New("frontd failed")
		swapInternalRuntimeRoleStarter(func(startup internalRuntimeRoleStartupConfig) (*internalRuntimeRoleProcess, internalRuntimeRoleReady, error) {
			if startup.Role == projectdaemon.RoleFrontd {
				return nil, internalRuntimeRoleReady{}, frontdErr
			}
			proc, done := newLeafwikiRuntimeRoleProcess(startup.Role, 201)
			doneChans = append(doneChans, done)
			return proc, internalRuntimeRoleReady{Role: startup.Role, PID: proc.pid, URL: "http://workspaced.local"}, nil
		})
		runtime, err = startWikidFrontdRuntime(context.Background(), leafwikiRuntimeConfig{}, "daemon-token", "http://wikid.local")
		Expect(runtime).To(BeNil())
		Expect(err).To(MatchError(frontdErr))
		releaseLeafwikiRuntimeRoleProcesses(doneChans, context.Canceled)

		runtime = &wikidFrontdRuntime{
			ctx:        context.Background(),
			supervisor: wikid.NewSupervisor(wikid.SupervisorOptions{}),
			processes:  map[projectdaemon.RoleName]*internalRuntimeRoleProcess{},
		}
		Expect(runtime.startFrontdLocked()).To(MatchError(errRuntimeWorkspacedURLUnavailable))
	})

	ginkgo.It("restarts runtime roles and publishes crash details", func() {
		var doneChans []chan error
		swapInternalRuntimeRoleStarter(func(startup internalRuntimeRoleStartupConfig) (*internalRuntimeRoleProcess, internalRuntimeRoleReady, error) {
			proc, done := newLeafwikiRuntimeRoleProcess(startup.Role, 300+len(doneChans))
			doneChans = append(doneChans, done)
			ready := internalRuntimeRoleReady{Role: startup.Role, PID: proc.pid, URL: "http://" + string(startup.Role)}
			if startup.Role == projectdaemon.RoleWorkspaced {
				ready.Private = true
			}
			return proc, ready, nil
		})

		runtime := &wikidFrontdRuntime{
			cfg:           leafwikiRuntimeConfig{},
			daemonToken:   "daemon-token",
			ctx:           context.Background(),
			supervisor:    wikid.NewSupervisor(wikid.SupervisorOptions{}),
			processes:     map[projectdaemon.RoleName]*internalRuntimeRoleProcess{},
			workspacedURL: "http://workspaced.local",
			wikidURL:      "http://wikid.local",
		}
		ginkgo.DeferCleanup(func() {
			Expect(runtime.stop(context.Background())).To(Succeed())
			releaseLeafwikiRuntimeRoleProcesses(doneChans, context.Canceled)
		})
		runtime.restartRole(projectdaemon.RoleWorkspaced)
		runtime.restartRole(projectdaemon.RoleFrontd)
		Expect(runtime.roleHealthSnapshot()).To(ContainElements(
			Satisfy(func(role projectdaemon.RoleHealth) bool {
				return role.Name == projectdaemon.RoleWorkspaced && role.State == projectdaemon.RoleStateReady
			}),
			Satisfy(func(role projectdaemon.RoleHealth) bool {
				return role.Name == projectdaemon.RoleFrontd && role.State == projectdaemon.RoleStateReady
			}),
		))

		crashDoneChans := []chan error{}
		crashingProc, crashDone := newLeafwikiRuntimeRoleProcess(projectdaemon.RoleFrontd, 401)
		crashDoneChans = append(crashDoneChans, crashDone)
		crashRuntime := &wikidFrontdRuntime{
			ctx:        context.Background(),
			supervisor: wikid.NewSupervisor(wikid.SupervisorOptions{MaxRestarts: 1}),
			processes:  map[projectdaemon.RoleName]*internalRuntimeRoleProcess{projectdaemon.RoleFrontd: crashingProc},
		}
		crashRuntime.supervisor.MarkReady(projectdaemon.RoleFrontd, crashingProc.pid, "http://frontd.local", false)
		crashRuntime.supervisor.RecordCrash(projectdaemon.RoleFrontd, "previous crash")
		var published [][]projectdaemon.RoleHealth
		crashRuntime.onRolesChanged = func(roles []projectdaemon.RoleHealth) {
			published = append(published, roles)
		}
		go crashRuntime.monitorRoleProcess(crashingProc)
		frontdExitedErr := errors.New(leafwikiFixtureFrontdExited)
		crashDone <- frontdExitedErr
		waitForLeafwikiRoleState(crashRuntime, projectdaemon.RoleFrontd, projectdaemon.RoleStateCrashed)
		Expect(published).To(ContainElement(ContainElement(HaveField("State", Equal(projectdaemon.RoleStateCrashed)))))
		Expect(crashRuntime.supervisor.State(projectdaemon.RoleFrontd)).To(MatchProjectDaemonRoleHealth(projectdaemon.RoleFrontd, projectdaemon.RoleStateCrashed, nil))
		releaseLeafwikiRuntimeRoleProcesses(crashDoneChans, context.Canceled)

		scheduledProc, scheduledDone := newLeafwikiRuntimeRoleProcess(projectdaemon.RoleFrontd, 402)
		scheduledRuntime := &wikidFrontdRuntime{
			cfg:           leafwikiRuntimeConfig{},
			daemonToken:   "daemon-token",
			ctx:           context.Background(),
			supervisor:    wikid.NewSupervisor(wikid.SupervisorOptions{MaxRestarts: 2, Backoff: time.Nanosecond}),
			processes:     map[projectdaemon.RoleName]*internalRuntimeRoleProcess{projectdaemon.RoleFrontd: scheduledProc},
			workspacedURL: "http://workspaced.local",
			wikidURL:      "http://wikid.local",
		}
		scheduledRuntime.supervisor.MarkReady(projectdaemon.RoleFrontd, scheduledProc.pid, "http://frontd.local", false)
		go scheduledRuntime.monitorRoleProcess(scheduledProc)
		scheduledDone <- errors.New("frontd exited once")
		waitForLeafwikiRoleState(scheduledRuntime, projectdaemon.RoleFrontd, projectdaemon.RoleStateReady)
	})

	ginkgo.It("authenticates daemon STDIO requests and keeps workspace descriptors scoped to local runtime state", func() {
		dataDir := filepath.Join(leafwikiTempDir(), "data")
		rootDir := filepath.Join(leafwikiTempDir(), "root")
		Expect(os.MkdirAll(rootDir, 0o755)).To(Succeed())

		Expect(validateAuthStartupConfig(leafwikiRuntimeConfig{DisableAuth: true})).To(Succeed())
		Expect(validateAuthStartupConfig(leafwikiRuntimeConfig{})).To(MatchError(errAuthJWTSecretRequired))
		Expect(validateAuthStartupConfig(leafwikiRuntimeConfig{JWTSecret: "jwt"})).To(MatchError(errAuthAdminPasswordRequired))
		Expect(validateAuthStartupConfig(leafwikiRuntimeConfig{JWTSecret: "jwt", AdminPassword: "admin"})).To(Succeed())

		logPath := filepath.Join(dataDir, "startup.log")
		logStartupValidationFailure(leaflogging.Config{Target: leaflogging.TargetStderr}, "ignored")
		logStartupValidationFailure(leaflogging.Config{Target: leaflogging.TargetFile, FilePath: logPath}, "startup failed")
		Expect(os.ReadFile(logPath)).To(ContainSubstring("startup failed"))
		parentFile := filepath.Join(leafwikiTempDir(), "not-a-dir")
		Expect(os.WriteFile(parentFile, []byte("file"), 0o600)).To(Succeed())
		logStartupValidationFailure(leaflogging.Config{Target: leaflogging.TargetFile, FilePath: filepath.Join(parentFile, "startup.log")}, "ignored")

		Expect(os.MkdirAll(authStorageDirForRuntime(dataDir), 0o755)).To(Succeed())
		output := captureLeafwikiStdout(func() {
			resetAdminPasswordCommand(dataDir)
		})
		Expect(output).To(ContainSubstring("admin"))
		cleanupFailureDir := filepath.Join(leafwikiTempDir(), "cleanup-failure")
		Expect(os.MkdirAll(filepath.Join(cleanupFailureDir, "users.db", "child"), 0o755)).To(Succeed())
		Expect(func() {
			resetAdminPasswordCommand(cleanupFailureDir)
		}).To(PanicWithLeafwikiExit(1))
		resetFailureDir := filepath.Join(leafwikiTempDir(), "reset-failure")
		Expect(os.MkdirAll(resetFailureDir, 0o755)).To(Succeed())
		Expect(os.WriteFile(filepath.Join(resetFailureDir, ".leafwiki"), []byte("not a directory"), 0o600)).To(Succeed())
		Expect(func() {
			resetAdminPasswordCommand(resetFailureDir)
		}).To(PanicWithLeafwikiExit(1))

		now := time.Now().UTC()
		var privateControlTokens []string
		var privateAuthorizations []string
		var privateWorkspaceIDs []string
		privateServer := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
			privateControlTokens = append(privateControlTokens, req.Header.Get(projectdaemon.ControlTokenHeader))
			switch req.URL.Path {
			case "/__leafwiki/actor-context":
				if req.Header.Get("Authorization") == "Bearer rejected" {
					writeRuntimeError(rw, http.StatusUnauthorized, errCodeStdioAuthAPIKeyInvalid)
					return
				}
				if req.Header.Get("Authorization") == "Bearer server-error" {
					http.Error(rw, "actor context failed", http.StatusInternalServerError)
					return
				}
				privateAuthorizations = append(privateAuthorizations, req.Header.Get("Authorization"))
				privateWorkspaceIDs = append(privateWorkspaceIDs, req.Header.Get(projectdaemon.WorkspaceIDHeader))
				writeRuntimeJSON(rw, map[string]any{"actor": projectdaemon.ActorContext{
					Version:     1,
					Issuer:      projectdaemon.ActorContextIssuerWikid,
					Subject:     "user:stdio",
					Username:    "stdio",
					Role:        coreauth.RoleEditor,
					Scopes:      []string{"leafwiki:workspace:read", "leafwiki:workspace:write"},
					WorkspaceID: "workspace-a",
					AuthMethod:  "api_key",
					IssuedAt:    now,
					ExpiresAt:   now.Add(time.Hour),
				}})
			case "/mcp":
				http.Error(rw, "not ready", http.StatusInternalServerError)
			default:
				http.NotFound(rw, req)
			}
		}))
		ginkgo.DeferCleanup(privateServer.Close)

		desc := &projectdaemon.Descriptor{ControlURL: privateServer.URL, ControlToken: "daemon-token", WorkspaceID: "workspace-a"}
		encoded, err := daemonStdioActorContext(context.Background(), desc, leafwikiRuntimeConfig{APIKey: " stdio-key "})
		Expect(err).NotTo(HaveOccurred())
		decoded, err := projectdaemon.DecodeActorContext(encoded, projectdaemon.ActorContextValidation{Now: now, WorkspaceID: "workspace-a"})
		Expect(err).NotTo(HaveOccurred())
		Expect(decoded.Subject).To(Equal("user:stdio"))
		Expect(privateControlTokens).To(ContainElement("daemon-token"))
		Expect(privateAuthorizations).To(ContainElement("Bearer stdio-key"))
		Expect(privateWorkspaceIDs).To(ContainElement(workspaceid.WorkspaceID("workspace-a").HTTPHeaderValue()))

		_, err = daemonStdioActorContext(context.Background(), desc, leafwikiRuntimeConfig{APIKey: "rejected"})
		Expect(err).To(MatchWikidPrivateEndpointStatus(http.StatusUnauthorized))
		var endpointErr *wikidPrivateEndpointError
		Expect(err).To(Satisfy(func(err error) bool {
			return errors.As(err, &endpointErr)
		}))
		Expect(endpointErr).To(testmatchers.HaveStructuredError(errCodeStdioAuthAPIKeyInvalid, sharederrors.MessageIDForCode(errCodeStdioAuthAPIKeyInvalid)))
		_, err = daemonStdioActorContext(context.Background(), desc, leafwikiRuntimeConfig{APIKey: "server-error"})
		Expect(err).To(MatchWikidPrivateEndpointStatus(http.StatusInternalServerError))
		Expect(err).To(Satisfy(func(err error) bool {
			return errors.As(err, &endpointErr)
		}))
		Expect(endpointErr.StatusCode).To(Equal(http.StatusInternalServerError))
		previousEncodeActorContext := encodeActorContextForRuntime
		ginkgo.DeferCleanup(func() {
			encodeActorContextForRuntime = previousEncodeActorContext
		})
		encodeActorContextErr := errors.New("encode failed")
		encodeActorContextForRuntime = func(projectdaemon.ActorContext) (string, error) {
			return "", encodeActorContextErr
		}
		_, err = daemonStdioActorContext(context.Background(), desc, leafwikiRuntimeConfig{APIKey: "stdio-key"})
		Expect(err).To(MatchError(encodeActorContextErr))
		encodeActorContextForRuntime = previousEncodeActorContext

		layout := wikid.GlobalLayout(dataDir)
		homeCfg := projectdaemon.Config{DataDir: layout.HomeDir, RootDir: layout.HomeRootDir}
		workspace, err := registeredFederatedWorkspaceForRequestResult(layout, homeCfg)
		Expect(err).NotTo(HaveOccurred())
		Expect(workspace.ID).To(Equal(wikid.HomeWorkspaceID))

		registry := wikid.NewRegistryService(wikid.NewRegistryStore(layout.DBPath), layout)
		Expect(registerFederatedFirstContactResult(layout, homeCfg, leafwikiRuntimeConfig{})).To(MatchHomeFederatedFirstContact())
		grant, err := federatedStdioAPIKeyWorkspaceGrantResult(layout, leafwikiRuntimeConfig{}, "workspace-a")
		Expect(err).To(MatchError(errWorkspaceGrantAbsent))
		Expect(grant).To(Equal(wikid.Grant{}))
		_, err = federatedStdioAPIKeyWorkspaceGrantResult(layout, leafwikiRuntimeConfig{APIKey: "lwk_key_missing"}, "workspace-a")
		Expect(err).To(MatchError(coreauth.ErrInvalidToken))
		unsupportedRoleAuthDir := authStorageDirForRuntime(layout.HomeDir)
		Expect(os.MkdirAll(unsupportedRoleAuthDir, 0o755)).To(Succeed())
		userStore, err := coreauth.NewUserStore(unsupportedRoleAuthDir)
		Expect(err).NotTo(HaveOccurred())
		ginkgo.DeferCleanup(userStore.Close)
		unsupportedRoleUser := &coreauth.User{ID: "unsupported-role-user", Username: "unsupported-role", Email: "unsupported@example.test", Password: "hash", Role: "owner"}
		Expect(userStore.CreateUser(unsupportedRoleUser)).To(Succeed())
		apiKeyStore, err := coreauth.NewAPIKeyStore(unsupportedRoleAuthDir)
		Expect(err).NotTo(HaveOccurred())
		apiKeyService := coreauth.NewAPIKeyService(apiKeyStore, coreauth.NewUserService(userStore))
		ginkgo.DeferCleanup(apiKeyService.Close)
		unsupportedRoleKey, err := apiKeyService.CreateAPIKey(coreauth.UserIDFromString(unsupportedRoleUser.ID), "unsupported role", coreauth.UserIDFromString(unsupportedRoleUser.ID))
		Expect(err).NotTo(HaveOccurred())
		_, err = federatedStdioAPIKeyWorkspaceGrantResult(layout, leafwikiRuntimeConfig{APIKey: unsupportedRoleKey.Secret}, "workspace-a")
		Expect(err).To(MatchError(errNativeStdioWorkspaceAccessDenied))

		workspaceData := filepath.Join(leafwikiTempDir(), "workspace-data")
		workspaceRoot := filepath.Join(leafwikiTempDir(), "workspace-root")
		registered, err := registry.RegisterWorkspace(wikid.RegisterWorkspaceRequest{
			DisplayName: "Workspace A",
			DataDir:     workspaceData,
			RootDir:     workspaceRoot,
		})
		Expect(err).NotTo(HaveOccurred())
		workspace, err = registeredFederatedWorkspaceForRequestResult(layout, projectdaemon.Config{DataDir: registered.DataDir, RootDir: registered.RootDir})
		Expect(err).NotTo(HaveOccurred())
		Expect(workspace.ID).To(Equal(registered.ID))

		descriptorPath := filepath.Join(leafwikiTempDir(), "descriptor.json")
		ownerCfg := projectdaemon.Config{DataDir: dataDir, RootDir: rootDir}
		descriptorRead := readHealthyProjectDaemonLockResult(context.Background(), descriptorPath, ownerCfg)
		Expect(descriptorRead.Err).NotTo(HaveOccurred())
		Expect(descriptorRead).To(MatchAbsentProjectDaemonHealth())

		Expect(os.WriteFile(descriptorPath, []byte("{bad"), 0o600)).To(Succeed())
		descriptorRead = readHealthyProjectDaemonLockResult(context.Background(), descriptorPath, ownerCfg)
		Expect(descriptorRead.Err).NotTo(HaveOccurred())
		Expect(descriptorRead).To(MatchAbsentProjectDaemonHealth())
		_, err = os.Stat(descriptorPath)
		Expect(err).To(MatchError(os.ErrNotExist))

		staleDesc := &projectdaemon.Descriptor{
			SchemaVersion: 0,
			DataDir:       ownerCfg.DataDir,
			RootDir:       ownerCfg.RootDir,
			ControlURL:    "http://127.0.0.1:1",
		}
		Expect(projectdaemon.WriteDescriptorAtomic(descriptorPath, staleDesc)).To(Succeed())
		descriptorRead = readHealthyProjectDaemonLockResult(context.Background(), descriptorPath, ownerCfg)
		Expect(descriptorRead.Err).NotTo(HaveOccurred())
		Expect(descriptorRead).To(MatchStaleProjectDaemonHealth())

		healthyDesc := &projectdaemon.Descriptor{
			SchemaVersion:   projectdaemon.DescriptorSchemaVersion,
			Role:            projectdaemon.RoleWorkspaced,
			PID:             os.Getpid(),
			DataDir:         ownerCfg.DataDir,
			RootDir:         ownerCfg.RootDir,
			PrivateMCPURL:   privateServer.URL + "/mcp",
			PrivateMCPToken: "token",
		}
		Expect(healthyDesc).To(MatchUnreachableWorkspacedPrivateMCP())
		healthyDesc.PrivateMCPURL = "https://example.com/mcp"
		Expect(projectDaemonDescriptorHealthResult(context.Background(), healthyDesc)).To(MatchProjectDaemonDescriptorHealthError(errPrivateMCPURLUntrusted))
	})

	ginkgo.It("derives wikid actor context from remote-user requests", func() {
		w := newFrontdActorTestWiki()
		ginkgo.DeferCleanup(w.Close)
		editor, err := w.UserService().CreateUser("remote-editor", "remote-editor@example.com", "password", coreauth.RoleEditor)
		Expect(err).NotTo(HaveOccurred())

		cfg := leafwikiRuntimeConfig{Workspace: wiki.Workspace{ID: "home"}}
		rec := httptest.NewRecorder()
		handleWikidActorContext(rec, httptest.NewRequest(http.MethodPost, "/__leafwiki/actor-context", nil), &wiki.Wiki{}, cfg, nil, nil)
		Expect(rec).To(HaveHTTPStatus(http.StatusUnauthorized))

		rec = httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/__leafwiki/actor-context", nil)
		req.Header.Set(projectdaemon.WorkspaceIDHeader, "not valid")
		handleWikidActorContext(rec, req, w, leafwikiRuntimeConfig{DisableAuth: true}, nil, nil)
		Expect(rec).To(HaveHTTPStatus(http.StatusNotFound))

		layout := wikid.GlobalLayout(leafwikiTempDir())
		registry := wikid.NewRegistryService(wikid.NewRegistryStore(layout.DBPath), layout)
		rec = httptest.NewRecorder()
		req = httptest.NewRequest(http.MethodPost, "/__leafwiki/actor-context", nil)
		req.Header.Set(projectdaemon.WorkspaceIDHeader, workspaceid.WorkspaceID("missing-workspace").HTTPHeaderValue())
		handleWikidActorContext(rec, req, w, leafwikiRuntimeConfig{DisableAuth: true}, registry, nil)
		Expect(rec).To(HaveHTTPStatus(http.StatusNotFound))

		grantLayout := wikid.GlobalLayout(leafwikiTempDir())
		grantRegistry := wikid.NewRegistryService(wikid.NewRegistryStore(grantLayout.DBPath), grantLayout)
		_, err = grantRegistry.BootstrapHomeWorkspace(grantLayout.HomeDir, grantLayout.HomeRootDir)
		Expect(err).NotTo(HaveOccurred())
		grantedWorkspace, err := grantRegistry.RegisterWorkspace(wikid.RegisterWorkspaceRequest{
			DisplayName: "Workspace B",
			DataDir:     filepath.Join(leafwikiTempDir(), "workspace-b-data"),
			RootDir:     filepath.Join(leafwikiTempDir(), "workspace-b-root"),
		})
		Expect(err).NotTo(HaveOccurred())
		grants := wikid.NewGrantStore(grantLayout.DBPath)
		rec = httptest.NewRecorder()
		req = httptest.NewRequest(http.MethodPost, "/__leafwiki/actor-context", nil)
		req.Header.Set(projectdaemon.WorkspaceIDHeader, grantedWorkspace.ID.HTTPHeaderValue())
		handleWikidActorContext(rec, req, w, leafwikiRuntimeConfig{DisableAuth: true}, nil, grants)
		Expect(rec).To(testmatchers.HaveHTTPStructuredError(http.StatusForbidden, runtimeErrorCodeWorkspaceGrantDenied, sharederrors.MessageIDForCode(runtimeErrorCodeWorkspaceGrantDenied)))

		Expect(grants.Upsert(wikid.Grant{Subject: "user:public-editor", WorkspaceID: grantedWorkspace.ID, Role: wikid.GrantRoleViewer})).To(Succeed())
		rec = httptest.NewRecorder()
		handleWikidActorContext(rec, req, w, leafwikiRuntimeConfig{DisableAuth: true}, nil, grants)
		Expect(rec).To(HaveHTTPStatus(http.StatusOK))
		var actorContextBody struct {
			Actor projectdaemon.ActorContext `json:"actor"`
		}
		Expect(json.Unmarshal(rec.Body.Bytes(), &actorContextBody)).To(Succeed())
		Expect(actorContextBody.Actor).To(gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
			"WorkspaceID": Equal(grantedWorkspace.ID),
		}))

		previousGrantsForSubject := grantsForSubjectForRuntime
		errGrantLookupFailed := errors.New("grant lookup failed")
		grantsForSubjectForRuntime = func(*wikid.GrantStore, string) ([]wikid.Grant, error) {
			return nil, errGrantLookupFailed
		}
		rec = httptest.NewRecorder()
		handleWikidActorContext(rec, req, w, leafwikiRuntimeConfig{DisableAuth: true}, nil, grants)
		Expect(rec).To(HaveHTTPStatus(http.StatusInternalServerError))
		grantsForSubjectForRuntime = previousGrantsForSubject

		admin, err := w.UserService().CreateUser("remote-admin", "remote-admin@example.com", "password", coreauth.RoleAdmin)
		Expect(err).NotTo(HaveOccurred())
		token, err := w.AuthService().Login(admin.Username, "password")
		Expect(err).NotTo(HaveOccurred())
		adminReq := httptest.NewRequest(http.MethodPost, "/__leafwiki/actor-context", nil)
		adminReq.AddCookie(&http.Cookie{Name: "leafwiki_at", Value: token.Token})
		adminReq.Header.Set(projectdaemon.WorkspaceIDHeader, workspaceid.WorkspaceID("admin-workspace").HTTPHeaderValue())
		rec = httptest.NewRecorder()
		handleWikidActorContext(rec, adminReq, w, cfg, nil, grants)
		Expect(rec).To(HaveHTTPStatus(http.StatusOK))
		Expect(json.Unmarshal(rec.Body.Bytes(), &actorContextBody)).To(Succeed())
		Expect(actorContextBody.Actor.Scopes).To(ContainElement("leafwiki:workspace:admin"))

		_, err = actorContextForWorkspaceGrant(nil, "api_key", cfg, "workspace-a", wikid.GrantRoleViewer)
		Expect(err).To(MatchError(errRuntimeActorUserRequired))
		Expect(seedRuntimeHomeGrants(grants, leafwikiRuntimeConfig{DisableAuth: true, PublicAccess: true})).To(Succeed())

		remoteReq := httptest.NewRequest(http.MethodGet, "/", nil)
		_, _, err = frontdRemoteUserResult(remoteReq, w, leafwikiRuntimeConfig{EnableHTTPRemoteUser: true, TrustedProxyIPsRaw: "bad-cidr"})
		Expect(err).To(MatchError(authmw.ErrInvalidTrustedProxy))
		remoteReq.RemoteAddr = "192.0.2.10:1111"
		user, method, err := frontdRemoteUserResult(remoteReq, w, leafwikiRuntimeConfig{EnableHTTPRemoteUser: true, TrustedProxyIPsRaw: "127.0.0.1"})
		Expect(err).To(MatchError(errFrontdRemoteUserAbsent))
		Expect(user).To(BeNil())
		Expect(method).To(BeEmpty())

		remoteReq.RemoteAddr = "127.0.0.1:1111"
		user, method, err = frontdRemoteUserResult(remoteReq, w, leafwikiRuntimeConfig{EnableHTTPRemoteUser: true, TrustedProxyIPsRaw: "127.0.0.1"})
		Expect(err).To(MatchError(errFrontdRemoteUserAbsent))
		Expect(user).To(BeNil())
		Expect(method).To(BeEmpty())

		remoteReq.Header.Set("Remote-User", editor.Username)
		_, _, err = frontdRemoteUserResult(remoteReq, &wiki.Wiki{}, leafwikiRuntimeConfig{EnableHTTPRemoteUser: true, TrustedProxyIPsRaw: "127.0.0.1"})
		Expect(err).To(MatchError(errFrontdRemoteUserServiceUnavailable))

		user, method, err = frontdRemoteUserResult(remoteReq, w, leafwikiRuntimeConfig{EnableHTTPRemoteUser: true, TrustedProxyIPsRaw: "127.0.0.1"})
		Expect(err).NotTo(HaveOccurred())
		Expect(user.ID).To(Equal(editor.ID))
		Expect(method).To(Equal("remote_user"))

		remoteReq.Header.Set("Remote-User", "missing-user")
		_, _, err = frontdRemoteUserResult(remoteReq, w, leafwikiRuntimeConfig{EnableHTTPRemoteUser: true, TrustedProxyIPsRaw: "127.0.0.1"})
		Expect(err).To(MatchError(coreauth.ErrUserNotFound))
	})

	ginkgo.It("cancels wikid-frontd owner boot through runtime role boundaries", func() {
		dataDir := filepath.Join(leafwikiTempDir(), "data")
		rootDir := filepath.Join(leafwikiTempDir(), "root")

		var doneChans []chan error
		swapInternalRuntimeRoleStarter(func(startup internalRuntimeRoleStartupConfig) (*internalRuntimeRoleProcess, internalRuntimeRoleReady, error) {
			proc, done := newLeafwikiRuntimeRoleProcess(startup.Role, 900+len(doneChans))
			doneChans = append(doneChans, done)
			switch startup.Role {
			case projectdaemon.RoleWorkspaced:
				return proc, internalRuntimeRoleReady{Role: startup.Role, PID: proc.pid, URL: "http://workspaced.local", Private: true}, nil
			case projectdaemon.RoleFrontd:
				return proc, internalRuntimeRoleReady{Role: startup.Role, PID: proc.pid, URL: "http://frontd.local"}, nil
			default:
				return nil, internalRuntimeRoleReady{}, errors.New("unexpected runtime role")
			}
		})

		ctx, cancel := context.WithCancel(context.Background())
		ginkgo.DeferCleanup(cancel)
		done := make(chan error, 1)
		go func() {
			done <- runProjectDaemonOwner(ctx, leafwikiRuntimeConfig{
				Workspace: wiki.Workspace{
					DataDir: dataDir,
					RootDir: rootDir,
				},
				Host:                 "127.0.0.1",
				Port:                 "0",
				DisableAuth:          true,
				PublicAccess:         true,
				AllowInsecure:        true,
				EnableHTTPRemoteUser: true,
				HTTPRemoteUserHeader: "Remote-User",
				TrustedProxyIPsRaw:   "127.0.0.1",
				Logging:              leaflogging.Config{Target: leaflogging.TargetStderr},
				DaemonIdleTimeout:    time.Hour,
				RuntimeStack:         projectdaemon.RuntimeStackWikidFrontd,
			})
		}()

		Expect(waitForLeafwikiDescriptor(projectdaemon.DescriptorPath(dataDir))).To(gstruct.PointTo(gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
			"ControlURL":    HavePrefix("http://127.0.0.1:"),
			"PrivateMCPURL": Equal("http://workspaced.local/mcp"),
			"PublicURL":     Equal("http://frontd.local"),
			"Roles": ContainElements(
				MatchPrivateProjectDaemonRoleHealth(projectdaemon.RoleWorkspaced, projectdaemon.RoleStateReady, gstruct.Fields{
					"URL": Equal("http://workspaced.local"),
				}),
				MatchProjectDaemonRoleHealth(projectdaemon.RoleFrontd, projectdaemon.RoleStateReady, gstruct.Fields{
					"URL": Equal("http://frontd.local"),
				}),
			),
		})))

		cancel()
		releaseLeafwikiRuntimeRoleProcesses(doneChans, context.Canceled)
		Eventually(done).WithTimeout(3 * time.Second).Should(Receive(Succeed()))
	})

	ginkgo.It("coordinates project daemon locks, descriptors, and foreground waits", func() {
		dataDir := filepath.Join(leafwikiTempDir(), "data")
		rootDir := filepath.Join(leafwikiTempDir(), "root")
		Expect(os.MkdirAll(dataDir, 0o755)).To(Succeed())
		Expect(os.MkdirAll(rootDir, 0o755)).To(Succeed())

		Expect(projectDaemonLocks(dataDir, rootDir)).NotTo(HaveHeldProjectDaemonLocks())

		dataLock, err := locking.AcquireDataDirLock(dataDir)
		Expect(err).NotTo(HaveOccurred())
		Expect(projectDaemonLocks(dataDir, rootDir)).NotTo(HaveHeldProjectDaemonLocks())
		Expect(projectDaemonLocks(dataDir, rootDir)).NotTo(HaveAvailableProjectDaemonLocks())
		Expect(dataLock.Release()).To(Succeed())

		rootLock, err := locking.AcquireRootDirLock(rootDir)
		Expect(err).NotTo(HaveOccurred())
		Expect(projectDaemonLocks(dataDir, rootDir)).To(HaveFreeDataLockWithHeldRootLock())

		dataLock, err = locking.AcquireDataDirLock(dataDir)
		Expect(err).NotTo(HaveOccurred())
		Expect(projectDaemonLocks(dataDir, rootDir)).To(HaveHeldProjectDaemonLocks())

		descriptorPath := filepath.Join(leafwikiTempDir(), "descriptor.json")
		Expect(os.WriteFile(descriptorPath, []byte("{bad"), 0o600)).To(Succeed())
		desc, err := readHealthyProjectDaemonResult(context.Background(), descriptorPath, projectdaemon.Config{DataDir: dataDir, RootDir: rootDir})
		var syntaxErr *json.SyntaxError
		Expect(err).To(Satisfy(func(err error) bool {
			return errors.As(err, &syntaxErr)
		}))
		Expect(desc).To(BeNil())

		stalePath := filepath.Join(leafwikiTempDir(), "stale-descriptor.json")
		Expect(projectdaemon.WriteDescriptorAtomic(stalePath, &projectdaemon.Descriptor{
			SchemaVersion: 0,
			DataDir:       dataDir,
			RootDir:       rootDir,
		})).To(Succeed())
		desc, err = readHealthyProjectDaemonResult(context.Background(), stalePath, projectdaemon.Config{DataDir: dataDir, RootDir: rootDir})
		Expect(err).To(MatchError(projectdaemon.ErrDescriptorSchemaMismatch))
		Expect(desc).To(BeNil())

		Expect(dataLock.Release()).To(Succeed())
		Expect(rootLock.Release()).To(Succeed())

		Expect(projectDaemonDescriptorHealthy(context.Background(), nil)).To(BeFalse())
		Expect(processPIDAlive(-1)).To(BeFalse())
		Expect(processPIDAlive(os.Getpid())).To(BeTrue())
		Expect(workspacedPrivateMCPEndpointReachable(context.Background(), &projectdaemon.Descriptor{PrivateMCPURL: "http://[::1"})).To(BeFalse())
		Expect(projectDaemonDescriptorHealthy(context.Background(), &projectdaemon.Descriptor{
			SchemaVersion:   projectdaemon.DescriptorSchemaVersion,
			Role:            projectdaemon.RoleWorkspaced,
			PID:             -1,
			DataDir:         dataDir,
			RootDir:         rootDir,
			PrivateMCPURL:   "http://127.0.0.1:1/mcp",
			PrivateMCPToken: "token",
		})).To(BeFalse())

		heartbeatErr := make(chan error, 1)
		heartbeatErr <- nil
		Expect(waitForForegroundSession(context.Background(), heartbeatErr)).To(Succeed())
		heartbeatErr <- context.Canceled
		Expect(waitForForegroundSession(context.Background(), heartbeatErr)).To(Succeed())
		heartbeatFailure := errors.New("heartbeat failed")
		heartbeatErr <- heartbeatFailure
		Expect(waitForForegroundSession(context.Background(), heartbeatErr)).To(MatchError(heartbeatFailure))

		canceled, cancel := context.WithCancel(context.Background())
		cancel()
		_, err = waitForProjectDaemon(canceled, filepath.Join(leafwikiTempDir(), "missing.json"), "", projectdaemon.Config{DataDir: dataDir, RootDir: rootDir}, mcpTransports{})
		Expect(err).To(MatchError(context.Canceled))

		previousAcquireDataLock := acquireDataDirLockForRuntime
		previousAcquireRootLock := acquireRootDirLockForRuntime
		ginkgo.DeferCleanup(func() {
			acquireDataDirLockForRuntime = previousAcquireDataLock
			acquireRootDirLockForRuntime = previousAcquireRootLock
		})
		dataLock, err = locking.AcquireDataDirLock(dataDir)
		Expect(err).NotTo(HaveOccurred())
		rootAcquireErr := errors.New("root acquire failed")
		acquireRootDirLockForRuntime = func(string) (leafwikiRuntimeLock, error) {
			return nil, rootAcquireErr
		}
		Expect(projectDaemonLocks(dataDir, rootDir)).To(MatchProjectDaemonHeldLockProbeError(rootAcquireErr))
		Expect(dataLock.Release()).To(Succeed())

		acquireDataDirLockForRuntime = func(string) (leafwikiRuntimeLock, error) {
			return leafwikiFakeRuntimeLock{}, nil
		}
		acquireRootDirLockForRuntime = func(string) (leafwikiRuntimeLock, error) {
			return nil, rootAcquireErr
		}
		Expect(projectDaemonLocks(dataDir, rootDir)).To(MatchProjectDaemonLockAvailabilityError(rootAcquireErr))
		rootReleaseErr := errors.New("root release failed")
		acquireRootDirLockForRuntime = func(string) (leafwikiRuntimeLock, error) {
			return leafwikiFakeRuntimeLock{releaseErr: rootReleaseErr}, nil
		}
		Expect(projectDaemonLocks(dataDir, rootDir)).To(MatchProjectDaemonLockAvailabilityError(rootReleaseErr))

		dataAcquireErr := errors.New("data acquire failed")
		acquireDataDirLockForRuntime = func(string) (leafwikiRuntimeLock, error) {
			return nil, dataAcquireErr
		}
		Expect(projectDaemonLocks(dataDir, rootDir)).To(MatchProjectDaemonDataRootLockProbeError(dataAcquireErr))
		dataReleaseErr := errors.New("data release failed")
		acquireDataDirLockForRuntime = func(string) (leafwikiRuntimeLock, error) {
			return leafwikiFakeRuntimeLock{releaseErr: dataReleaseErr}, nil
		}
		Expect(projectDaemonLocks(dataDir, rootDir)).To(MatchProjectDaemonDataRootLockProbeError(dataReleaseErr))
		acquireDataDirLockForRuntime = func(string) (leafwikiRuntimeLock, error) {
			return leafwikiFakeRuntimeLock{}, nil
		}
		acquireRootDirLockForRuntime = func(string) (leafwikiRuntimeLock, error) {
			return nil, rootAcquireErr
		}
		Expect(projectDaemonLocks(dataDir, rootDir)).To(MatchProjectDaemonDataRootLockProbeError(rootAcquireErr))
		acquireDataDirLockForRuntime = previousAcquireDataLock
		acquireRootDirLockForRuntime = previousAcquireRootLock

		lockedRoot, err := locking.AcquireRootDirLock(rootDir)
		Expect(err).NotTo(HaveOccurred())
		lockStartupPath := filepath.Join(leafwikiTempDir(), "lock-startup.txt")
		Expect(os.WriteFile(lockStartupPath, []byte("acquire data directory lock: held"), 0o600)).To(Succeed())
		_, err = waitForProjectDaemon(context.Background(), filepath.Join(leafwikiTempDir(), "missing-descriptor.json"), lockStartupPath, projectdaemon.Config{DataDir: dataDir, RootDir: rootDir}, mcpTransports{})
		Expect(err).To(MatchError(errProjectLockedNoAttachableDaemon))
		Expect(lockedRoot.Release()).To(Succeed())

		privateMCPServer := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
			rw.WriteHeader(http.StatusNoContent)
		}))
		ginkgo.DeferCleanup(privateMCPServer.Close)
		mismatchDescriptorPath := filepath.Join(leafwikiTempDir(), "workspaced-descriptor.json")
		descriptorCfg := projectdaemon.Config{DataDir: dataDir, RootDir: rootDir, Host: "owner"}
		Expect(projectdaemon.WriteDescriptorAtomic(mismatchDescriptorPath, &projectdaemon.Descriptor{
			SchemaVersion:   projectdaemon.DescriptorSchemaVersion,
			Role:            projectdaemon.RoleWorkspaced,
			PID:             os.Getpid(),
			DataDir:         dataDir,
			RootDir:         rootDir,
			PrivateMCPURL:   privateMCPServer.URL,
			PrivateMCPToken: "token",
			Config:          descriptorCfg,
		})).To(Succeed())
		_, err = waitForProjectDaemon(context.Background(), mismatchDescriptorPath, "", projectdaemon.Config{DataDir: dataDir, RootDir: rootDir, Host: "requested"}, mcpTransports{})
		Expect(err).To(MatchProjectDaemonConfigMismatch())
	})

	ginkgo.It("starts direct runtime role processes through startup helpers", func() {

		Expect((*wikidFrontdRuntime)(nil).stop(context.Background())).To(Succeed())
		Expect((*internalRuntimeRoleProcess)(nil).wait()).To(Succeed())
		Expect((*internalRuntimeRoleProcess)(nil).isDone()).To(BeTrue())
		Expect((*internalRuntimeRoleProcess)(nil).stop(context.Background())).To(Succeed())

		done := make(chan error, 1)
		proc := &internalRuntimeRoleProcess{done: done, waitDone: make(chan struct{})}
		doneErr := errors.New("role exited")
		done <- doneErr
		Expect(proc.wait()).To(MatchError(doneErr))
		Expect(proc.isDone()).To(BeTrue())
		process, err := os.FindProcess(os.Getpid())
		Expect(err).NotTo(HaveOccurred())
		proc.process = process
		Expect(proc.stop(context.Background())).To(MatchError(doneErr))

		for _, tc := range []struct {
			name     string
			lateDone bool
		}{
			{name: "returns joined context and process error", lateDone: true},
			{name: "returns the context error when wait never completes"},
		} {
			cmd := exec.Command("sleep", "10")
			Expect(cmd.Start()).To(Succeed(), tc.name)
			processDone := make(chan error)
			blockingProc := &internalRuntimeRoleProcess{
				role:     projectdaemon.RoleFrontd,
				pid:      cmd.Process.Pid,
				process:  cmd.Process,
				done:     processDone,
				waitDone: make(chan struct{}),
			}
			canceledStop, cancelStop := context.WithCancel(context.Background())
			cancelStop()
			if tc.lateDone {
				go func() {
					time.Sleep(10 * time.Millisecond)
					processDone <- errors.New("role exited after kill")
				}()
			}
			err = blockingProc.stop(canceledStop)
			Expect(err).To(MatchError(context.Canceled), tc.name)
			if !tc.lateDone {
				processDone <- errors.New("late role exit")
			}
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
		}

		startupPath, err := writeInternalRuntimeRoleStartupConfig(internalRuntimeRoleStartupConfig{
			Role:        projectdaemon.RoleFrontd,
			DaemonToken: "token",
			Runtime:     leafwikiRuntimeConfig{Workspace: wiki.Workspace{DataDir: leafwikiTempDir(), RootDir: leafwikiTempDir()}},
		})
		Expect(err).NotTo(HaveOccurred())
		ginkgo.DeferCleanup(os.Remove, startupPath)
		Expect(startupPath).NotTo(BeEmpty())
		Expect(os.ReadFile(startupPath)).To(ContainSubstring(`"role":"frontd"`))

		Expect(writeInternalRuntimeRoleReady("", internalRuntimeRoleReady{Role: projectdaemon.RoleFrontd})).To(Succeed())
		readyPath := filepath.Join(leafwikiTempDir(), "ready.json")
		Expect(writeInternalRuntimeRoleReady(readyPath, internalRuntimeRoleReady{Role: projectdaemon.RoleFrontd, PID: os.Getpid(), URL: "http://frontd.local"})).To(Succeed())
		readyProc, readyDone := newLeafwikiRuntimeRoleProcess(projectdaemon.RoleFrontd, os.Getpid())
		ready, err := waitForInternalRuntimeRoleReady(readyPath, readyProc, 100*time.Millisecond)
		Expect(err).NotTo(HaveOccurred())
		Expect(ready.Role).To(Equal(projectdaemon.RoleFrontd))
		releaseLeafwikiRuntimeRoleProcesses([]chan error{readyDone}, context.Canceled)

		badReadyPath := filepath.Join(leafwikiTempDir(), "bad-ready.json")
		Expect(os.WriteFile(badReadyPath, []byte("{bad"), 0o600)).To(Succeed())
		badProc, badDone := newLeafwikiRuntimeRoleProcess(projectdaemon.RoleFrontd, os.Getpid())
		_, err = waitForInternalRuntimeRoleReady(badReadyPath, badProc, 100*time.Millisecond)
		Expect(err).To(MatchJSONSyntaxError())
		releaseLeafwikiRuntimeRoleProcesses([]chan error{badDone}, context.Canceled)

		unsupportedPath := filepath.Join(leafwikiTempDir(), "unsupported.json")
		Expect(os.WriteFile(unsupportedPath, []byte(`{"role":"unknown"}`), 0o600)).To(Succeed())
		Expect(runInternalRuntimeRole(context.Background(), unsupportedPath)).To(MatchError(errUnsupportedRuntimeRole))
		badRuntimeStartup := filepath.Join(leafwikiTempDir(), "bad-runtime.json")
		Expect(os.WriteFile(badRuntimeStartup, []byte("{bad"), 0o600)).To(Succeed())
		Expect(runInternalRuntimeRole(context.Background(), badRuntimeStartup)).To(MatchJSONSyntaxError())
		wikidRuntimeStartup := filepath.Join(leafwikiTempDir(), "wikid-runtime.json")
		Expect(os.WriteFile(wikidRuntimeStartup, []byte(`{"role":"wikid"}`), 0o600)).To(Succeed())
		wikidCanceled, cancelWikid := context.WithCancel(context.Background())
		cancelWikid()
		Expect(runInternalRuntimeRole(wikidCanceled, wikidRuntimeStartup)).To(Succeed())

		canceled, cancel := context.WithCancel(context.Background())
		cancel()
		Expect(runInternalRuntimeRoleName(canceled, projectdaemon.RoleWikid)).To(Succeed())
	})

	ginkgo.It("reports runtime role startup process failures through existing boundaries", func() {
		blockingFile := filepath.Join(leafwikiTempDir(), "not-a-dir")
		validTempDir := leafwikiTempDir()
		missingExecutable := filepath.Join(leafwikiTempDir(), "missing-leafwiki")
		Expect(os.WriteFile(blockingFile, []byte("x"), 0o600)).To(Succeed())

		leafwikiSetenv("TMPDIR", blockingFile)
		_, _, err := startInternalRuntimeRoleProcess(internalRuntimeRoleStartupConfig{Role: projectdaemon.RoleFrontd})
		Expect(err).To(MatchError(syscall.ENOTDIR))
		_, err = writeInternalRuntimeRoleStartupConfig(internalRuntimeRoleStartupConfig{Role: projectdaemon.RoleFrontd})
		Expect(err).To(MatchError(syscall.ENOTDIR))

		leafwikiSetenv("TMPDIR", validTempDir)
		previousExecutable := projectDaemonExecutable
		executableUnavailableErr := errors.New("executable unavailable")
		projectDaemonExecutable = func() (string, error) {
			return "", executableUnavailableErr
		}
		ginkgo.DeferCleanup(func() {
			projectDaemonExecutable = previousExecutable
		})
		_, _, err = startInternalRuntimeRoleProcess(internalRuntimeRoleStartupConfig{Role: projectdaemon.RoleFrontd})
		Expect(err).To(MatchError(executableUnavailableErr))

		projectDaemonExecutable = previousExecutable
		previousReadinessTimeout := internalRuntimeRoleReadinessTimeoutForProcess
		internalRuntimeRoleReadinessTimeoutForProcess = 20 * time.Millisecond
		ginkgo.DeferCleanup(func() {
			internalRuntimeRoleReadinessTimeoutForProcess = previousReadinessTimeout
		})
		leafwikiSetenv("GO_WANT_LEAFWIKI_HELPER_PROCESS", "1")
		_, _, err = startInternalRuntimeRoleProcess(internalRuntimeRoleStartupConfig{Role: projectdaemon.RoleWikid})
		Expect(err).To(MatchError(errRuntimeRoleReadinessTimeout))

		projectDaemonExecutable = func() (string, error) {
			return missingExecutable, nil
		}
		_, _, err = startInternalRuntimeRoleProcess(internalRuntimeRoleStartupConfig{Role: projectdaemon.RoleFrontd})
		Expect(err).To(MatchError(os.ErrNotExist))
	})

	ginkgo.It("reports temp-file chmod, write, and close failures", func() {
		previousCreateTemp := createTempFileForRuntime
		previousExecutable := projectDaemonExecutable
		ginkgo.DeferCleanup(func() {
			createTempFileForRuntime = previousCreateTemp
			projectDaemonExecutable = previousExecutable
		})

		for _, tc := range []struct {
			name string
			err  error
			file *leafwikiFakeTempFile
		}{
			{name: "chmod", err: errors.New("chmod failed")},
			{name: "write", err: errors.New("write failed")},
			{name: "close", err: errors.New("close failed")},
		} {
			tc.file = &leafwikiFakeTempFile{name: filepath.Join(leafwikiTempDir(), tc.name+".json")}
			switch tc.name {
			case "chmod":
				tc.file.chmodErr = tc.err
			case "write":
				tc.file.writeErr = tc.err
			case "close":
				tc.file.closeErr = tc.err
			}
			tempFile := tc.file
			createTempFileForRuntime = func(string, string) (leafwikiTempFile, error) {
				return tempFile, nil
			}
			_, err := writeInternalRuntimeRoleStartupConfig(internalRuntimeRoleStartupConfig{Role: projectdaemon.RoleFrontd})
			Expect(err).To(MatchError(tc.err))
		}

		validCfg := leafwikiRuntimeConfig{Logging: leaflogging.Config{Target: leaflogging.TargetStderr}, DisableAuth: true}
		errTemp := &leafwikiFakeTempFile{name: filepath.Join(leafwikiTempDir(), "daemon.err")}
		daemonStartupWriteErr := errors.New("daemon startup write failed")
		startupTemp := &leafwikiFakeTempFile{name: filepath.Join(leafwikiTempDir(), "daemon.json"), writeErr: daemonStartupWriteErr}
		createTempFileForRuntime = func(_ string, pattern string) (leafwikiTempFile, error) {
			if strings.Contains(pattern, "*.err") {
				return errTemp, nil
			}
			return startupTemp, nil
		}
		_, err := spawnProjectDaemonOwner(validCfg)
		Expect(err).To(MatchError(daemonStartupWriteErr))
		for _, tc := range []struct {
			name string
			err  error
			file *leafwikiFakeTempFile
		}{
			{name: "daemon startup chmod", err: errors.New("daemon startup chmod failed")},
			{name: "daemon startup close", err: errors.New("daemon startup close failed")},
		} {
			tc.file = &leafwikiFakeTempFile{name: filepath.Join(leafwikiTempDir(), strings.ReplaceAll(tc.name, " ", "-")+".json")}
			switch tc.name {
			case "daemon startup chmod":
				tc.file.chmodErr = tc.err
			case "daemon startup close":
				tc.file.closeErr = tc.err
			}
			startupTemp := tc.file
			createTempFileForRuntime = func(_ string, pattern string) (leafwikiTempFile, error) {
				if strings.Contains(pattern, "*.err") {
					return errTemp, nil
				}
				return startupTemp, nil
			}
			_, err = spawnProjectDaemonOwner(validCfg)
			Expect(err).To(MatchError(tc.err))
		}

		projectDaemonExecutable = func() (string, error) {
			return "", errors.New("executable should not be reached")
		}
		readyTemp := &leafwikiFakeTempFile{name: filepath.Join(leafwikiTempDir(), "ready.json")}
		roleStartupCloseErr := errors.New("role startup close failed")
		roleTemp := &leafwikiFakeTempFile{name: filepath.Join(leafwikiTempDir(), "role.json"), closeErr: roleStartupCloseErr}
		createTempFileForRuntime = func(_ string, pattern string) (leafwikiTempFile, error) {
			if strings.Contains(pattern, "runtime-ready") {
				return readyTemp, nil
			}
			return roleTemp, nil
		}
		_, _, err = startInternalRuntimeRoleProcess(internalRuntimeRoleStartupConfig{Role: projectdaemon.RoleFrontd})
		Expect(err).To(MatchError(roleStartupCloseErr))
	})

	ginkgo.It("reports runtime role readiness failures", func() {

		exitedProc, exitedDone := newLeafwikiRuntimeRoleProcess(projectdaemon.RoleFrontd, os.Getpid())
		exitedDone <- nil
		_, err := waitForInternalRuntimeRoleReady(filepath.Join(leafwikiTempDir(), "missing-ready.json"), exitedProc, time.Second)
		Expect(err).To(MatchError(errRuntimeRoleExitedBeforeReadiness))

		timeoutProc, timeoutDone := newLeafwikiRuntimeRoleProcess(projectdaemon.RoleFrontd, os.Getpid())
		_, err = waitForInternalRuntimeRoleReady(filepath.Join(leafwikiTempDir(), "missing-ready.json"), timeoutProc, time.Millisecond)
		Expect(err).To(MatchError(errRuntimeRoleReadinessTimeout))
		releaseLeafwikiRuntimeRoleProcesses([]chan error{timeoutDone}, context.Canceled)

		blockingFile := filepath.Join(leafwikiTempDir(), "not-a-dir")
		Expect(os.WriteFile(blockingFile, []byte("x"), 0o600)).To(Succeed())
		readErrProc, readErrDone := newLeafwikiRuntimeRoleProcess(projectdaemon.RoleFrontd, os.Getpid())
		_, err = waitForInternalRuntimeRoleReady(filepath.Join(blockingFile, "ready.json"), readErrProc, time.Second)
		Expect(err).To(MatchPathError())
		releaseLeafwikiRuntimeRoleProcesses([]chan error{readErrDone}, context.Canceled)

		blankPath := filepath.Join(leafwikiTempDir(), "blank-ready.json")
		Expect(os.WriteFile(blankPath, []byte("  \n"), 0o600)).To(Succeed())
		blankProc, blankDone := newLeafwikiRuntimeRoleProcess(projectdaemon.RoleFrontd, os.Getpid())
		go func() {
			defer ginkgo.GinkgoRecover()
			time.Sleep(30 * time.Millisecond)
			Expect(os.WriteFile(blankPath, []byte(`{"role":"frontd","pid":0}`), 0o600)).To(Succeed())
		}()
		_, err = waitForInternalRuntimeRoleReady(blankPath, blankProc, time.Second)
		Expect(err).To(MatchError(errRuntimeRoleInvalidPID))
		releaseLeafwikiRuntimeRoleProcesses([]chan error{blankDone}, context.Canceled)
	})

	ginkgo.It("reports frontd and workspaced role fast failures", func() {
		validRuntime := leafwikiRuntimeConfig{
			Workspace: wiki.Workspace{
				DataDir: filepath.Join(leafwikiTempDir(), "data"),
				RootDir: filepath.Join(leafwikiTempDir(), "root"),
			},
			Host:        "127.0.0.1",
			Port:        "0",
			DisableAuth: true,
			Logging:     leaflogging.Config{Target: leaflogging.TargetStderr},
		}
		Expect(os.MkdirAll(validRuntime.Workspace.DataDir, 0o755)).To(Succeed())
		Expect(os.MkdirAll(validRuntime.Workspace.RootDir, 0o755)).To(Succeed())

		parentExitCtx, parentExitCancel := context.WithCancel(context.Background())
		cancelWhenParentExits(context.Background(), parentExitCancel, 0, 0)
		Consistently(parentExitCtx.Done()).WithTimeout(25 * time.Millisecond).ShouldNot(BeClosed())
		parentCanceled, parentCancel := context.WithCancel(context.Background())
		parentCancel()
		parentCanceledExitCtx, parentCanceledExitCancel := context.WithCancel(context.Background())
		cancelWhenParentExits(parentCanceled, parentCanceledExitCancel, os.Getpid(), 0)
		Consistently(parentCanceledExitCtx.Done()).WithTimeout(25 * time.Millisecond).ShouldNot(BeClosed())
		acceptErr := errors.New("accept failed")
		Expect(serveInternalRuntimeHTTP(context.Background(), projectdaemon.RoleFrontd, leafwikiErrorListener{
			addr: leafwikiStringAddr("127.0.0.1:0"),
			err:  acceptErr,
		}, http.NotFoundHandler(), 0)).To(MatchError(acceptErr))

		_, err := newRuntimeWiki(leafwikiRuntimeConfig{Workspace: wiki.Workspace{ID: "home"}}, projectdaemon.Config{DataDir: "bad\x00data", RootDir: validRuntime.Workspace.RootDir}, runtimeWikiFull)
		Expect(err).To(MatchPathError())

		badPathRuntime := validRuntime
		badPathRuntime.Workspace.DataDir = "bad\x00data"
		err = runFrontdRole(context.Background(), internalRuntimeRoleStartupConfig{Role: projectdaemon.RoleFrontd, Runtime: badPathRuntime})
		Expect(err).To(MatchPathError())
		err = runWorkspacedRole(context.Background(), internalRuntimeRoleStartupConfig{Role: projectdaemon.RoleWorkspaced, Runtime: badPathRuntime})
		Expect(err).To(MatchPathError())

		badLogRuntime := validRuntime
		badLogRuntime.Logging = leaflogging.Config{Target: leaflogging.Target("not-a-target")}
		Expect(runFrontdRole(context.Background(), internalRuntimeRoleStartupConfig{Role: projectdaemon.RoleFrontd, Runtime: badLogRuntime})).To(MatchError(leaflogging.ErrInvalidLogTarget))
		Expect(runWorkspacedRole(context.Background(), internalRuntimeRoleStartupConfig{Role: projectdaemon.RoleWorkspaced, Runtime: badLogRuntime})).To(MatchError(leaflogging.ErrInvalidLogTarget))

		badProxyRuntime := validRuntime
		badProxyRuntime.TrustedProxyIPsRaw = "bad-cidr"
		_, err = routerOptionsForRuntimeWithUserService(badProxyRuntime, nil, "", false, "127.0.0.1")
		Expect(err).To(MatchError(authmw.ErrInvalidTrustedProxy))
		Expect(runFrontdRole(context.Background(), internalRuntimeRoleStartupConfig{Role: projectdaemon.RoleFrontd, Runtime: badProxyRuntime})).To(MatchError(authmw.ErrInvalidTrustedProxy))
		Expect(runWorkspacedRole(context.Background(), internalRuntimeRoleStartupConfig{Role: projectdaemon.RoleWorkspaced, Runtime: badProxyRuntime})).To(MatchError(authmw.ErrInvalidTrustedProxy))

		startup := internalRuntimeRoleStartupConfig{
			Role:          projectdaemon.RoleFrontd,
			Runtime:       validRuntime,
			DaemonToken:   "daemon-token",
			WikidURL:      "http://127.0.0.1:1",
			WorkspacedURL: "http://127.0.0.1:2",
			ReadyPath:     filepath.Join(leafwikiTempDir(), "ready.json"),
			ParentPID:     os.Getpid(),
		}
		badWikid := startup
		badWikid.WikidURL = "http://[::1"
		Expect(runFrontdRole(context.Background(), badWikid)).To(MatchInvalidWikidUpstream())

		badWorkspaced := startup
		badWorkspaced.WorkspacedURL = "http://[::1"
		Expect(runFrontdRole(context.Background(), badWorkspaced)).To(MatchInvalidWorkspacedUpstream())

		badHost := startup
		badHost.Runtime.Host = "bad host"
		Expect(runFrontdRole(context.Background(), badHost)).To(MatchNetOpError())

		badReady := startup
		badReady.ReadyPath = filepath.Join(blockingPathForLeafwikiTest(), "ready.json")
		Expect(runFrontdRole(context.Background(), badReady)).To(MatchPathError())

		workspacedStartup := internalRuntimeRoleStartupConfig{
			Role:        projectdaemon.RoleWorkspaced,
			Runtime:     validRuntime,
			DaemonToken: "daemon-token",
			ReadyPath:   filepath.Join(leafwikiTempDir(), "workspaced-ready.json"),
			ParentPID:   os.Getpid(),
		}
		badWorkspacedHost := workspacedStartup
		badWorkspacedHost.Runtime.Port = "not-a-port"
		Expect(runWorkspacedRole(context.Background(), badWorkspacedHost)).To(MatchNetOpError())

		badWorkspacedReady := workspacedStartup
		badWorkspacedReady.ReadyPath = filepath.Join(blockingPathForLeafwikiTest(), "ready.json")
		Expect(runWorkspacedRole(context.Background(), badWorkspacedReady)).To(MatchPathError())

		successRuntime := validRuntime
		successRuntime.Workspace.DataDir = filepath.Join(leafwikiTempDir(), "success-data")
		successRuntime.Workspace.RootDir = filepath.Join(leafwikiTempDir(), "success-root")
		Expect(os.MkdirAll(successRuntime.Workspace.DataDir, 0o755)).To(Succeed())
		Expect(os.MkdirAll(successRuntime.Workspace.RootDir, 0o755)).To(Succeed())
		successStartup := workspacedStartup
		successStartup.Runtime = successRuntime
		successStartup.ReadyPath = filepath.Join(leafwikiTempDir(), "workspaced-success-ready.json")
		successCtx, successCancel := context.WithCancel(context.Background())
		done := make(chan error, 1)
		go func() {
			done <- runWorkspacedRole(successCtx, successStartup)
		}()
		ready := waitForLeafwikiRuntimeReady(successStartup.ReadyPath)
		resp, err := http.Get(strings.TrimRight(ready.URL, "/") + "/mcp")
		Expect(err).NotTo(HaveOccurred())
		Expect(resp).To(HaveHTTPStatus(http.StatusUnauthorized))
		Expect(resp.Body.Close()).To(Succeed())
		successCancel()
		Eventually(done).WithTimeout(3 * time.Second).Should(Receive(Succeed()))
	})

	ginkgo.It("dispatches runtime roles through project daemon launcher boundaries", func() {
		previousRunDaemonService := runDaemonServiceForDispatch
		previousRunAgentHookCommand := runAgentHookCommandForDispatch
		previousRunProjectDaemonLauncher := runProjectDaemonLauncherForDispatch
		previousAttach := attachOrStartRuntimeDaemonForLaunch
		previousAgentHookAttach := attachOrStartRuntimeDaemonForAgentHook
		previousHeartbeat := runDaemonHeartbeatForLaunch
		previousBridge := runDaemonStdioBridgeForLaunch
		previousActorContext := daemonStdioActorContextForLaunch
		ginkgo.DeferCleanup(func() {
			runDaemonServiceForDispatch = previousRunDaemonService
			runAgentHookCommandForDispatch = previousRunAgentHookCommand
			runProjectDaemonLauncherForDispatch = previousRunProjectDaemonLauncher
			attachOrStartRuntimeDaemonForLaunch = previousAttach
			attachOrStartRuntimeDaemonForAgentHook = previousAgentHookAttach
			runDaemonHeartbeatForLaunch = previousHeartbeat
			runDaemonStdioBridgeForLaunch = previousBridge
			daemonStdioActorContextForLaunch = previousActorContext
		})

		runDaemonServiceForDispatch = func(context.Context, leafwikiRuntimeConfig) error {
			return errors.New("service failed")
		}
		Expect(func() {
			dispatchRuntimeCommand(nil, true, false, leafwikiRuntimeConfig{})
		}).To(PanicWithLeafwikiExit(1))

		var hookProvider agenthooks.ProviderID
		runAgentHookCommandForDispatch = func(_ context.Context, _ leafwikiRuntimeConfig, provider agenthooks.ProviderID, _ io.Reader, _ io.Writer) error {
			hookProvider = provider
			return errors.New("hook failed open")
		}
		dispatchRuntimeCommand([]string{"agent-hook", "cursor"}, false, true, leafwikiRuntimeConfig{})
		Expect(hookProvider).To(Equal(agenthooks.ProviderCursor))
		hookStdoutErr := errors.New("stdout failed")
		Expect(runAgentHookCommand(context.Background(), leafwikiRuntimeConfig{}, agenthooks.ProviderCodex, strings.NewReader(`{}`), leafwikiFailWriter{err: hookStdoutErr})).To(MatchError(hookStdoutErr))
		attachOrStartRuntimeDaemonForAgentHook = func(context.Context, leafwikiRuntimeConfig) (*projectdaemon.Descriptor, error) {
			return &projectdaemon.Descriptor{ControlURL: "http://127.0.0.1:1", ControlToken: "daemon-token"}, nil
		}
		Expect(runAgentHookCommand(context.Background(), leafwikiRuntimeConfig{}, agenthooks.ProviderCodex, strings.NewReader(`{"hook_event_name":"SessionStart","session_id":"session-a"}`), io.Discard)).To(MatchURLError())
		attachOrStartRuntimeDaemonForAgentHook = previousAgentHookAttach

		runProjectDaemonLauncherForDispatch = func(context.Context, leafwikiRuntimeConfig) error {
			return errors.New("launcher failed")
		}
		Expect(func() {
			dispatchRuntimeCommand(nil, false, false, leafwikiRuntimeConfig{})
		}).To(PanicWithLeafwikiExit(1))

		verifierDownErr := errors.New("verifier down")
		sessions := projectdaemon.NewSessionRegistry(time.Minute, nil)
		controlServer := httptest.NewServer(projectdaemon.NewControlServer(projectdaemon.ControlServerOptions{
			Token:    "daemon-token",
			Sessions: sessions,
			VerifyAPIKey: func(key string) error {
				switch key {
				case "invalid":
					return projectdaemon.ErrInvalidAPIKey
				case "broken":
					return verifierDownErr
				default:
					return nil
				}
			},
		}))
		ginkgo.DeferCleanup(controlServer.Close)
		descriptor := &projectdaemon.Descriptor{ControlURL: controlServer.URL, ControlToken: "daemon-token"}

		attachOrStartRuntimeDaemonForLaunch = func(ctx context.Context, _ leafwikiRuntimeConfig) (*projectdaemon.Descriptor, error) {
			return descriptor, nil
		}
		runDaemonHeartbeatForLaunch = func(context.Context, *projectdaemon.Client, projectdaemon.SessionID, time.Duration) error {
			return nil
		}
		Expect(runProjectDaemonLauncher(context.Background(), leafwikiRuntimeConfig{})).To(Succeed())

		canceledVerify, cancelVerify := context.WithCancel(context.Background())
		cancelVerify()
		Expect(runProjectDaemonLauncher(canceledVerify, leafwikiRuntimeConfig{MCPTransports: mcpTransports{Stdio: true}, APIKey: "valid"})).To(Succeed())
		canceledRegister, cancelRegister := context.WithCancel(context.Background())
		cancelRegister()
		Expect(runProjectDaemonLauncher(canceledRegister, leafwikiRuntimeConfig{})).To(Succeed())

		badTokenDescriptor := *descriptor
		badTokenDescriptor.ControlToken = "wrong-token"
		attachOrStartRuntimeDaemonForLaunch = func(ctx context.Context, _ leafwikiRuntimeConfig) (*projectdaemon.Descriptor, error) {
			return &badTokenDescriptor, nil
		}
		Expect(runProjectDaemonLauncher(context.Background(), leafwikiRuntimeConfig{})).To(MatchProjectDaemonControlStatus(http.StatusUnauthorized))
		attachOrStartRuntimeDaemonForLaunch = func(ctx context.Context, _ leafwikiRuntimeConfig) (*projectdaemon.Descriptor, error) {
			return descriptor, nil
		}

		launcherHeartbeatErr := errors.New("heartbeat failed")
		runDaemonHeartbeatForLaunch = func(context.Context, *projectdaemon.Client, projectdaemon.SessionID, time.Duration) error {
			return launcherHeartbeatErr
		}
		Expect(runProjectDaemonLauncher(context.Background(), leafwikiRuntimeConfig{})).To(MatchError(launcherHeartbeatErr))

		attachErr := errors.New("attach failed")
		attachOrStartRuntimeDaemonForLaunch = func(ctx context.Context, _ leafwikiRuntimeConfig) (*projectdaemon.Descriptor, error) {
			return nil, attachErr
		}
		Expect(runProjectDaemonLauncher(context.Background(), leafwikiRuntimeConfig{})).To(MatchError(attachErr))
		canceled, cancel := context.WithCancel(context.Background())
		cancel()
		Expect(runProjectDaemonLauncher(canceled, leafwikiRuntimeConfig{})).To(Succeed())

		attachOrStartRuntimeDaemonForLaunch = func(ctx context.Context, _ leafwikiRuntimeConfig) (*projectdaemon.Descriptor, error) {
			return descriptor, nil
		}
		runDaemonHeartbeatForLaunch = func(ctx context.Context, _ *projectdaemon.Client, _ projectdaemon.SessionID, _ time.Duration) error {
			return waitForLeafwikiContextCancellation(ctx)
		}
		bridgeErr := errors.New("bridge failed")
		runDaemonStdioBridgeForLaunch = func(context.Context, daemonStdioBridge) error {
			return bridgeErr
		}
		Expect(runProjectDaemonLauncher(context.Background(), leafwikiRuntimeConfig{DisableAuth: true, MCPTransports: mcpTransports{Stdio: true}})).To(MatchError(bridgeErr))

		stdioHeartbeatErr := errors.New("stdio heartbeat failed")
		runDaemonHeartbeatForLaunch = func(context.Context, *projectdaemon.Client, projectdaemon.SessionID, time.Duration) error {
			return stdioHeartbeatErr
		}
		runDaemonStdioBridgeForLaunch = func(ctx context.Context, _ daemonStdioBridge) error {
			return waitForLeafwikiContextCancellation(ctx)
		}
		Expect(runProjectDaemonLauncher(context.Background(), leafwikiRuntimeConfig{DisableAuth: true, MCPTransports: mcpTransports{Stdio: true}})).To(MatchError(stdioHeartbeatErr))

		descriptor.PrivateMCPURL = "http://private.local/mcp"
		descriptor.PrivateMCPToken = "private-token"
		actorContextErr := errors.New("actor context failed")
		daemonStdioActorContextForLaunch = func(context.Context, *projectdaemon.Descriptor, leafwikiRuntimeConfig) (string, error) {
			return "", actorContextErr
		}
		Expect(runProjectDaemonLauncher(context.Background(), leafwikiRuntimeConfig{DisableAuth: true, MCPTransports: mcpTransports{Stdio: true}})).To(MatchError(actorContextErr))
		daemonStdioActorContextForLaunch = func(context.Context, *projectdaemon.Descriptor, leafwikiRuntimeConfig) (string, error) {
			return "", context.Canceled
		}
		Expect(runProjectDaemonLauncher(context.Background(), leafwikiRuntimeConfig{DisableAuth: true, MCPTransports: mcpTransports{Stdio: true}})).To(Succeed())
		descriptor.PrivateMCPURL = ""
		descriptor.PrivateMCPToken = ""
		daemonStdioActorContextForLaunch = previousActorContext

		Expect(runProjectDaemonLauncher(context.Background(), leafwikiRuntimeConfig{MCPTransports: mcpTransports{Stdio: true}, APIKey: "invalid"})).To(MatchError(projectdaemon.ErrInvalidAPIKey))
		err := runProjectDaemonLauncher(context.Background(), leafwikiRuntimeConfig{MCPTransports: mcpTransports{Stdio: true}, APIKey: "broken"})
		var controlErr *projectdaemon.ControlHTTPError
		Expect(err).To(Satisfy(func(err error) bool {
			return errors.As(err, &controlErr)
		}))
		Expect(controlErr.StatusCode).To(Equal(http.StatusServiceUnavailable))

		runDaemonHeartbeatForLaunch = func(context.Context, *projectdaemon.Client, projectdaemon.SessionID, time.Duration) error {
			return nil
		}
		runDaemonStdioBridgeForLaunch = func(ctx context.Context, _ daemonStdioBridge) error {
			return waitForLeafwikiContextCancellation(ctx)
		}
		Expect(runProjectDaemonLauncher(context.Background(), leafwikiRuntimeConfig{DisableAuth: true, MCPTransports: mcpTransports{Stdio: true}})).To(Succeed())
	})

	ginkgo.It("reports SDK transport bridge connect and pump errors", func() {
		connectErr := errors.New("left connect failed")
		Expect(bridgeTransports(context.Background(), leafwikiFakeMCPTransport{err: connectErr}, leafwikiFakeMCPTransport{})).To(MatchError(connectErr))

		leftConn := newLeafwikiFakeMCPConnection()
		rightConnectErr := errors.New("right connect failed")
		Expect(bridgeTransports(context.Background(), leafwikiFakeMCPTransport{conn: leftConn}, leafwikiFakeMCPTransport{err: rightConnectErr})).To(MatchError(rightConnectErr))

		leftConn = newLeafwikiFakeMCPConnection()
		rightConn := newLeafwikiFakeMCPConnection()
		writeErr := errors.New("right write failed")
		rightConn.writeErr = writeErr
		leftConn.reads <- &sdkjsonrpc.Request{Method: "test/method"}
		Expect(bridgeTransports(context.Background(), leafwikiFakeMCPTransport{conn: leftConn}, leafwikiFakeMCPTransport{conn: rightConn})).To(MatchError(writeErr))

		leftConn = newLeafwikiFakeMCPConnection()
		rightConn = newLeafwikiFakeMCPConnection()
		leftConn.readErr = io.EOF
		leftConn.reads <- nil
		Expect(bridgeTransports(context.Background(), leafwikiFakeMCPTransport{conn: leftConn}, leafwikiFakeMCPTransport{conn: rightConn})).To(Succeed())

		leftConn = newLeafwikiFakeMCPConnection()
		rightConn = newLeafwikiFakeMCPConnection()
		leftConn.reads <- &sdkjsonrpc.Request{Method: "test/method"}
		rightConn.readErr = io.EOF
		go func() {
			<-rightConn.writes
			leftConn.readErr = io.EOF
			leftConn.reads <- nil
			time.Sleep(10 * time.Millisecond)
			rightConn.reads <- nil
		}()
		Expect(bridgeTransports(context.Background(), leafwikiFakeMCPTransport{conn: leftConn}, leafwikiFakeMCPTransport{conn: rightConn})).To(MatchError(io.EOF))

		now := time.Now().UTC()
		authServer := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
			Expect(req.Header).To(HaveKeyWithValue(http.CanonicalHeaderKey(projectdaemon.ControlTokenHeader), ContainElement("daemon-token")))
			writeRuntimeJSON(rw, map[string]any{"actor": projectdaemon.ActorContext{
				Version:     1,
				Issuer:      projectdaemon.ActorContextIssuerWikid,
				Subject:     "user:stdio",
				Username:    "stdio",
				Role:        coreauth.RoleEditor,
				Scopes:      []string{"leafwiki:mcp"},
				WorkspaceID: "workspace-a",
				AuthMethod:  "api_key",
				IssuedAt:    now,
				ExpiresAt:   now.Add(time.Hour),
			}})
		}))
		ginkgo.DeferCleanup(authServer.Close)
		upstream := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
			Expect(req.Header.Get(projectdaemon.ActorContextHeader)).NotTo(BeEmpty())
			rw.WriteHeader(http.StatusNoContent)
		}))
		ginkgo.DeferCleanup(upstream.Close)
		rt := stdioActorContextRoundTripper{
			AuthControlURL:   authServer.URL,
			AuthControlToken: "daemon-token",
			WorkspaceID:      "workspace-a",
			APIKey:           "stdio-key",
		}
		resp, err := rt.RoundTrip(httptest.NewRequest(http.MethodGet, upstream.URL, nil))
		Expect(err).NotTo(HaveOccurred())
		Expect(resp).To(HaveHTTPStatus(http.StatusNoContent))
		Expect(resp.Body.Close()).To(Succeed())

		previousEncodeActorContext := encodeActorContextForRuntime
		ginkgo.DeferCleanup(func() {
			encodeActorContextForRuntime = previousEncodeActorContext
		})
		encodeActorContextErr := errors.New("round trip encode failed")
		encodeActorContextForRuntime = func(projectdaemon.ActorContext) (string, error) {
			return "", encodeActorContextErr
		}
		_, err = rt.actorContext(httptest.NewRequest(http.MethodGet, upstream.URL, nil))
		Expect(err).To(MatchError(encodeActorContextErr))
		encodeActorContextForRuntime = previousEncodeActorContext
	})

	ginkgo.It("reports project daemon owner and spawn failures", func() {
		previousOwner := runWikidFrontdOwnerForProjectDaemon
		previousExecutable := projectDaemonExecutable
		ginkgo.DeferCleanup(func() {
			runWikidFrontdOwnerForProjectDaemon = previousOwner
			projectDaemonExecutable = previousExecutable
		})

		validCfg := leafwikiRuntimeConfig{
			Workspace: wiki.Workspace{
				DataDir: filepath.Join(leafwikiTempDir(), "data"),
				RootDir: filepath.Join(leafwikiTempDir(), "root"),
			},
			Logging:     leaflogging.Config{Target: leaflogging.TargetStderr},
			DisableAuth: true,
		}

		badPathCfg := validCfg
		badPathCfg.Workspace.DataDir = "bad\x00data"
		Expect(runProjectDaemonOwner(context.Background(), badPathCfg)).To(MatchPathError())

		Expect(os.MkdirAll(validCfg.Workspace.DataDir, 0o755)).To(Succeed())
		Expect(os.MkdirAll(validCfg.Workspace.RootDir, 0o755)).To(Succeed())
		dataLock, err := locking.AcquireDataDirLock(validCfg.Workspace.DataDir)
		Expect(err).NotTo(HaveOccurred())
		Expect(runProjectDaemonOwner(context.Background(), validCfg)).To(Satisfy(locking.IsDataDirLockHeld))
		Expect(dataLock.Release()).To(Succeed())

		rootLock, err := locking.AcquireRootDirLock(validCfg.Workspace.RootDir)
		Expect(err).NotTo(HaveOccurred())
		Expect(runProjectDaemonOwner(context.Background(), validCfg)).To(Satisfy(locking.IsRootDirLockHeld))
		Expect(rootLock.Release()).To(Succeed())

		badLogCfg := validCfg
		badLogCfg.Logging = leaflogging.Config{Target: leaflogging.Target("bad-target")}
		Expect(runProjectDaemonOwner(context.Background(), badLogCfg)).To(MatchError(leaflogging.ErrInvalidLogTarget))

		legacyDBDir := filepath.Join(validCfg.Workspace.DataDir, "users.db")
		Expect(os.MkdirAll(legacyDBDir, 0o755)).To(Succeed())
		Expect(os.WriteFile(filepath.Join(legacyDBDir, "child"), []byte("x"), 0o600)).To(Succeed())
		Expect(runProjectDaemonOwner(context.Background(), validCfg)).To(MatchPathError())
		Expect(os.Remove(filepath.Join(legacyDBDir, "child"))).To(Succeed())
		Expect(os.Remove(legacyDBDir)).To(Succeed())

		authDirFailureCfg := validCfg
		authDirFailureCfg.Workspace.DataDir = filepath.Join(leafwikiTempDir(), "auth-dir-data")
		authDirFailureCfg.Workspace.RootDir = filepath.Join(leafwikiTempDir(), "auth-dir-root")
		Expect(os.MkdirAll(authDirFailureCfg.Workspace.DataDir, 0o755)).To(Succeed())
		Expect(os.MkdirAll(authDirFailureCfg.Workspace.RootDir, 0o755)).To(Succeed())
		Expect(os.MkdirAll(filepath.Join(authDirFailureCfg.Workspace.DataDir, ".leafwiki"), 0o755)).To(Succeed())
		Expect(os.WriteFile(filepath.Join(authDirFailureCfg.Workspace.DataDir, ".leafwiki", "wikid"), []byte("not a directory"), 0o600)).To(Succeed())
		Expect(runProjectDaemonOwner(context.Background(), authDirFailureCfg)).To(MatchPathError())

		oauthDirFailureCfg := validCfg
		oauthDirFailureCfg.Workspace.DataDir = filepath.Join(leafwikiTempDir(), "oauth-dir-data")
		oauthDirFailureCfg.Workspace.RootDir = filepath.Join(leafwikiTempDir(), "oauth-dir-root")
		Expect(os.MkdirAll(oauthDirFailureCfg.Workspace.DataDir, 0o755)).To(Succeed())
		Expect(os.MkdirAll(oauthDirFailureCfg.Workspace.RootDir, 0o755)).To(Succeed())
		oauthPaths := wikid.AuthStoragePaths(oauthDirFailureCfg.Workspace.DataDir)
		Expect(os.MkdirAll(oauthPaths.AuthDir, 0o755)).To(Succeed())
		Expect(os.WriteFile(oauthPaths.OAuthDir, []byte("not a directory"), 0o600)).To(Succeed())
		Expect(runProjectDaemonOwner(context.Background(), oauthDirFailureCfg)).To(MatchPathError())

		wikidFrontdErr := errors.New("wikid-frontd failed")
		runWikidFrontdOwnerForProjectDaemon = func(context.Context, leafwikiRuntimeConfig, projectdaemon.Config) error {
			return wikidFrontdErr
		}
		Expect(runProjectDaemonOwner(context.Background(), validCfg)).To(MatchError(wikidFrontdErr))

		runWikidFrontdOwnerForProjectDaemon = func(context.Context, leafwikiRuntimeConfig, projectdaemon.Config) error {
			return nil
		}
		missingRootCfg := validCfg
		missingRootCfg.Workspace.DataDir = filepath.Join(leafwikiTempDir(), "missing-root-data")
		missingRootCfg.Workspace.RootDir = filepath.Join(leafwikiTempDir(), "missing-root")
		Expect(runProjectDaemonOwner(context.Background(), missingRootCfg)).To(Succeed())
		Expect(missingRootCfg.Workspace.RootDir).To(BeADirectory())

		validTempDir := leafwikiTempDir()
		missingExecutable := filepath.Join(leafwikiTempDir(), "missing-leafwiki")
		blockingFile := blockingPathForLeafwikiTest()
		leafwikiSetenv("TMPDIR", blockingFile)
		_, err = spawnProjectDaemonOwner(validCfg)
		Expect(err).To(MatchError(syscall.ENOTDIR))

		leafwikiSetenv("TMPDIR", validTempDir)
		executableUnavailableErr := errors.New("executable unavailable")
		projectDaemonExecutable = func() (string, error) {
			return "", executableUnavailableErr
		}
		_, err = spawnProjectDaemonOwner(validCfg)
		Expect(err).To(MatchError(executableUnavailableErr))

		projectDaemonExecutable = func() (string, error) {
			return missingExecutable, nil
		}
		_, err = spawnProjectDaemonOwner(validCfg)
		Expect(err).To(MatchError(os.ErrNotExist))

		scheduleProjectDaemonStartupConfigCleanup("")
	})

	ginkgo.It("reports wikid-frontd owner dependency failures", func() {
		cfg := leafwikiRuntimeConfig{
			Workspace: wiki.Workspace{
				ID:      wikid.HomeWorkspaceID,
				DataDir: filepath.Join(leafwikiTempDir(), "data"),
				RootDir: filepath.Join(leafwikiTempDir(), "root"),
			},
			Host:         "127.0.0.1",
			Port:         "0",
			DisableAuth:  true,
			Logging:      leaflogging.Config{Target: leaflogging.TargetStderr},
			RuntimeStack: projectdaemon.RuntimeStackWikidFrontd,
		}
		ownerCfg := projectdaemon.Config{
			RuntimeStack: projectdaemon.RuntimeStackWikidFrontd,
			DataDir:      cfg.Workspace.DataDir,
			RootDir:      cfg.Workspace.RootDir,
			Host:         cfg.Host,
			Port:         cfg.Port,
		}
		Expect(os.MkdirAll(ownerCfg.DataDir, 0o755)).To(Succeed())
		Expect(os.MkdirAll(ownerCfg.RootDir, 0o755)).To(Succeed())

		previousNetListen := netListenForRuntime
		previousRandomToken := randomTokenForRuntime
		previousConfigHash := configHashForRuntime
		previousNewRuntimeWiki := newRuntimeWikiForRuntime
		previousControlPlaneOptions := controlPlaneRouterOptionsForOwner
		previousStartRuntime := startWikidFrontdRuntimeForOwner
		previousMCPProxy := newMCPProxyWithActorForRuntime
		previousWriteDescriptor := writeDescriptorAtomicForRuntime
		ginkgo.DeferCleanup(func() {
			netListenForRuntime = previousNetListen
			randomTokenForRuntime = previousRandomToken
			configHashForRuntime = previousConfigHash
			newRuntimeWikiForRuntime = previousNewRuntimeWiki
			controlPlaneRouterOptionsForOwner = previousControlPlaneOptions
			startWikidFrontdRuntimeForOwner = previousStartRuntime
			newMCPProxyWithActorForRuntime = previousMCPProxy
			writeDescriptorAtomicForRuntime = previousWriteDescriptor
		})

		listenErr := errors.New("listen failed")
		netListenForRuntime = func(string, string) (net.Listener, error) {
			return nil, listenErr
		}
		Expect(runWikidFrontdOwner(context.Background(), cfg, ownerCfg)).To(MatchError(listenErr))
		netListenForRuntime = previousNetListen

		tokenErr := errors.New("token failed")
		randomTokenForRuntime = func() (string, error) {
			return "", tokenErr
		}
		Expect(runWikidFrontdOwner(context.Background(), cfg, ownerCfg)).To(MatchError(tokenErr))
		randomTokenForRuntime = previousRandomToken

		hashErr := errors.New("hash failed")
		configHashForRuntime = func(projectdaemon.Config) (string, error) {
			return "", hashErr
		}
		Expect(runWikidFrontdOwner(context.Background(), cfg, ownerCfg)).To(MatchError(hashErr))
		configHashForRuntime = previousConfigHash

		wikiErr := errors.New("wiki failed")
		newRuntimeWikiForRuntime = func(leafwikiRuntimeConfig, projectdaemon.Config, runtimeWikiMode) (*wiki.Wiki, error) {
			return nil, wikiErr
		}
		Expect(runWikidFrontdOwner(context.Background(), cfg, ownerCfg)).To(MatchError(wikiErr))
		newRuntimeWikiForRuntime = func(leafwikiRuntimeConfig, projectdaemon.Config, runtimeWikiMode) (*wiki.Wiki, error) {
			w := newFrontdActorTestWiki()
			ginkgo.DeferCleanup(w.Close)
			return w, nil
		}

		routerOptionsErr := errors.New("router options failed")
		controlPlaneRouterOptionsForOwner = func(leafwikiRuntimeConfig, *wiki.Wiki) (httpinternal.RouterOptions, error) {
			return httpinternal.RouterOptions{}, routerOptionsErr
		}
		Expect(runWikidFrontdOwner(context.Background(), cfg, ownerCfg)).To(MatchError(routerOptionsErr))
		controlPlaneRouterOptionsForOwner = previousControlPlaneOptions

		runtimeErr := errors.New("runtime failed")
		startWikidFrontdRuntimeForOwner = func(context.Context, leafwikiRuntimeConfig, string, string) (*wikidFrontdRuntime, error) {
			return nil, runtimeErr
		}
		Expect(runWikidFrontdOwner(context.Background(), cfg, ownerCfg)).To(MatchError(runtimeErr))

		startWikidFrontdRuntimeForOwner = func(ctx context.Context, _ leafwikiRuntimeConfig, _ string, _ string) (*wikidFrontdRuntime, error) {
			return newLeafwikiReadyOwnerRuntime(ctx), nil
		}
		privateMCPErr := errors.New("private mcp failed")
		newMCPProxyWithActorForRuntime = func(frontd.WorkspaceProxyOptions) (http.Handler, error) {
			return nil, privateMCPErr
		}
		Expect(runWikidFrontdOwner(context.Background(), cfg, ownerCfg)).To(MatchError(privateMCPErr))
		newMCPProxyWithActorForRuntime = previousMCPProxy

		writeDescriptorErr := errors.New("write descriptor failed")
		writeDescriptorAtomicForRuntime = func(string, *projectdaemon.Descriptor) error {
			return writeDescriptorErr
		}
		Expect(runWikidFrontdOwner(context.Background(), cfg, ownerCfg)).To(MatchError(writeDescriptorErr))

		writeCalls := 0
		writeGlobalDescriptorErr := errors.New("write global descriptor failed")
		writeDescriptorAtomicForRuntime = func(path string, desc *projectdaemon.Descriptor) error {
			writeCalls++
			if writeCalls == 2 {
				return writeGlobalDescriptorErr
			}
			return previousWriteDescriptor(path, desc)
		}
		Expect(runWikidFrontdOwner(context.Background(), cfg, ownerCfg)).To(MatchError(writeGlobalDescriptorErr))
	})

	ginkgo.It("reports direct actor, token, and private endpoint errors", func() {
		w := newFrontdActorTestWiki()
		ginkgo.DeferCleanup(w.Close)

		actor, err := wikidControlMCPActorResolver("", leafwikiRuntimeConfig{DisableAuth: true})(httptest.NewRequest(http.MethodPost, "/mcp", nil))
		Expect(err).NotTo(HaveOccurred())
		Expect(actor.Subject).To(Equal("user:public-editor"))

		_, err = wikidControlMCPActorResolver(leafwikiTempDir(), leafwikiRuntimeConfig{})(httptest.NewRequest(http.MethodPost, "/mcp", nil))
		Expect(err).To(MatchError(errNativeStdioAPIKeyRequired))
		missingKeyReq := httptest.NewRequest(http.MethodPost, "/mcp", nil)
		missingKeyReq.Header.Set("Authorization", "Bearer lwk_key_missing")
		_, err = wikidControlMCPActorResolver(leafwikiTempDir(), leafwikiRuntimeConfig{})(missingKeyReq)
		Expect(err).To(MatchError(coreauth.ErrInvalidToken))

		userStoreFailureDir := leafwikiTempDir()
		Expect(os.Mkdir(filepath.Join(userStoreFailureDir, "users.db"), 0o755)).To(Succeed())
		_, err = stdioAPIKeyUserFromStorage(userStoreFailureDir, "lwk_key_missing")
		Expect(err).To(MatchSQLitePrimaryError(sqlite3.SQLITE_CANTOPEN))
		apiKeyStoreFailureDir := leafwikiTempDir()
		Expect(os.Mkdir(filepath.Join(apiKeyStoreFailureDir, "api_keys.db"), 0o755)).To(Succeed())
		_, err = stdioAPIKeyUserFromStorage(apiKeyStoreFailureDir, "lwk_key_missing")
		Expect(err).To(MatchSQLitePrimaryError(sqlite3.SQLITE_CANTOPEN))
		_, err = stdioAPIKeyUserFromStorage(leafwikiTempDir(), "lwk_key_missing")
		Expect(err).To(MatchError(coreauth.ErrInvalidToken))

		_, err = frontdMCPTokenVerifier(&wiki.Wiki{})(context.Background(), "lwk_key_missing", httptest.NewRequest(http.MethodPost, "/mcp", nil))
		Expect(err).To(MatchError(sdkauth.ErrInvalidToken))

		noCodeErr := newWikidPrivateEndpointError("/plain", http.StatusBadGateway, nil)
		Expect(noCodeErr.Code).To(BeEmpty())
		Expect(noCodeErr.StatusCode).To(Equal(http.StatusBadGateway))

		err = callWikidPrivateEndpoint(context.Background(), "http://[::1", "token", "/private", nil, nil)
		Expect(err).To(MatchURLError())
		Expect(cloneWithOriginalRequest(nil)).To(BeNil())

		blockingFile := filepath.Join(leafwikiTempDir(), "not-a-dir")
		Expect(os.WriteFile(blockingFile, []byte("x"), 0o600)).To(Succeed())
		badRegistryLayout := wikid.GlobalLayout(leafwikiTempDir())
		badRegistry := wikid.NewRegistryService(wikid.NewRegistryStore(filepath.Join(blockingFile, "registry.db")), badRegistryLayout)
		rec := httptest.NewRecorder()
		handleWikidActorContext(rec, httptest.NewRequest(http.MethodPost, "/__leafwiki/actor-context", nil), w, leafwikiRuntimeConfig{DisableAuth: true, Workspace: wiki.Workspace{ID: "home"}}, badRegistry, nil)
		Expect(rec).To(HaveHTTPStatus(http.StatusInternalServerError))

		badGrantStore := wikid.NewGrantStore(filepath.Join(blockingFile, "grants.db"))
		rec = httptest.NewRecorder()
		handleWikidActorContext(rec, httptest.NewRequest(http.MethodPost, "/__leafwiki/actor-context", nil), w, leafwikiRuntimeConfig{DisableAuth: true, Workspace: wiki.Workspace{ID: "home"}}, nil, badGrantStore)
		Expect(rec).To(HaveHTTPStatus(http.StatusInternalServerError))
		previousGrantsForSubject := grantsForSubjectForRuntime
		previousActorContextForGrant := actorContextForWorkspaceGrantForRuntime
		ginkgo.DeferCleanup(func() {
			grantsForSubjectForRuntime = previousGrantsForSubject
			actorContextForWorkspaceGrantForRuntime = previousActorContextForGrant
		})
		grantsForSubjectForRuntime = func(*wikid.GrantStore, string) ([]wikid.Grant, error) {
			return nil, errors.New("grant lookup failed")
		}
		validGrantStore := wikid.NewGrantStore(filepath.Join(leafwikiTempDir(), "grants.db"))
		rec = httptest.NewRecorder()
		handleWikidActorContext(rec, httptest.NewRequest(http.MethodPost, "/__leafwiki/actor-context", nil), w, leafwikiRuntimeConfig{DisableAuth: true, Workspace: wiki.Workspace{ID: "home"}}, nil, validGrantStore)
		Expect(rec).To(HaveHTTPStatus(http.StatusInternalServerError))
		grantsForSubjectForRuntime = previousGrantsForSubject

		actorContextForWorkspaceGrantForRuntime = func(*coreauth.User, string, leafwikiRuntimeConfig, workspaceid.WorkspaceID, wikid.GrantRole) (projectdaemon.ActorContext, error) {
			return projectdaemon.ActorContext{}, errors.New("actor context failed")
		}
		rec = httptest.NewRecorder()
		handleWikidActorContext(rec, httptest.NewRequest(http.MethodPost, "/__leafwiki/actor-context", nil), w, leafwikiRuntimeConfig{DisableAuth: true, Workspace: wiki.Workspace{ID: "home"}}, nil, nil)
		Expect(rec).To(HaveHTTPStatus(http.StatusInternalServerError))
		actorContextForWorkspaceGrantForRuntime = previousActorContextForGrant

		Expect(seedRuntimeHomeGrants(badGrantStore, leafwikiRuntimeConfig{DisableAuth: true})).To(MatchPathError())
		Expect(seedRuntimeHomeGrants(badGrantStore, leafwikiRuntimeConfig{PublicAccess: true})).To(MatchPathError())
	})

	ginkgo.It("keeps direct manager, storage, and environment helpers deterministic", func() {

		var manager *federatedWorkspaceManager
		manager.MarkReady("workspace-a", 1, "http://workspace.local")
		_, err := manager.Ensure(context.Background(), wikid.WorkspaceRecord{ID: "workspace-a"})
		Expect(err).To(MatchError(errWorkspaceManagerUnavailable))

		manager = newFederatedWorkspaceManager(leafwikiRuntimeConfig{}, "daemon-token", "http://wikid.local", wikid.GlobalLayout(leafwikiTempDir()), wikid.NewWorkspaceSupervisor(wikid.WorkspaceSupervisorOptions{}))
		_, err = manager.Ensure(context.Background(), wikid.WorkspaceRecord{})
		Expect(err).To(MatchError(errWorkspaceIDRequired))

		workspace := wikid.WorkspaceRecord{ID: "workspace-a", DataDir: leafwikiTempDir(), RootDir: leafwikiTempDir()}
		manager.supervisor.MarkReady(workspace.ID, os.Getpid(), "http://workspace.local")
		status, err := manager.ensureWorkspace(workspace.ID, workspace)
		Expect(err).NotTo(HaveOccurred())
		Expect(status.State).To(Equal(wikid.WorkspaceStateRunning))

		manager.removeDescriptor = func(string) error {
			return errors.New("remove failed")
		}
		manager.removeDescriptors([]string{"descriptor.json"})
		Expect((*federatedWorkspaceManager)(nil).stop(context.Background())).To(Succeed())
		manager.stopped = true
		manager.restartWorkspaceAfter(wikid.WorkspaceRecord{ID: "workspace-a"}, time.Now().Add(-time.Second))

		processStopErr := errors.New("process stop failed")
		stoppedProcDone := make(chan error, 1)
		stoppedProcDone <- processStopErr
		stoppedProc := &internalRuntimeRoleProcess{
			role:     projectdaemon.RoleWorkspaced,
			done:     stoppedProcDone,
			waitDone: make(chan struct{}),
		}
		Expect(stoppedProc.wait()).To(MatchError(processStopErr))
		process, err := os.FindProcess(os.Getpid())
		Expect(err).NotTo(HaveOccurred())
		stoppedProc.process = process
		manager = newFederatedWorkspaceManager(leafwikiRuntimeConfig{}, "daemon-token", "http://wikid.local", wikid.GlobalLayout(leafwikiTempDir()), wikid.NewWorkspaceSupervisor(wikid.WorkspaceSupervisorOptions{}))
		manager.processes["workspace-stop"] = stoppedProc
		Expect(manager.stop(context.Background())).To(MatchError(processStopErr))

		blockingFile := blockingPathForLeafwikiTest()
		Expect(removeNonRegularDescriptor(filepath.Join(blockingFile, "descriptor.json"))).To(MatchPathError())
		_, err = stdioAPIKeyUserFromStorage(filepath.Join(blockingFile, "auth"), "lwk_key_missing")
		Expect(err).To(MatchPathError())
		_, err = daemonConfigForRuntime(leafwikiRuntimeConfig{Workspace: wiki.Workspace{DataDir: "bad\x00data", RootDir: leafwikiTempDir()}})
		Expect(err).To(MatchPathError())
		_, err = daemonWorkspaceRequestConfigForRuntime(leafwikiRuntimeConfig{Workspace: wiki.Workspace{DataDir: "bad\x00data", RootDir: leafwikiTempDir()}})
		Expect(err).To(MatchPathError())
		previousHome := userHomeDirForRuntime
		userHomeDirForRuntime = func() (string, error) {
			return "", nil
		}
		ginkgo.DeferCleanup(func() {
			userHomeDirForRuntime = previousHome
		})
		_, err = globalRuntimeHomeDir()
		Expect(err).To(MatchError(errUserHomeEmpty))
		_, err = globalRuntimeWorkspace()
		Expect(err).To(MatchError(errUserHomeEmpty))
		_, err = daemonOwnerRuntimeConfig(leafwikiRuntimeConfig{})
		Expect(err).To(MatchError(errUserHomeEmpty))
		_, err = daemonRequestConfigForRuntime(leafwikiRuntimeConfig{})
		Expect(err).To(MatchError(errUserHomeEmpty))

		opts := frontendConfigForRuntimeStorage(filepath.Join(leafwikiTempDir(), "missing"))
		Expect(opts.GetSiteName()).To(Equal("LeafWiki"))
		Expect(opts.GetFaviconFile()).To(BeEmpty())
		blockedFrontendOpts := frontendConfigForRuntimeStorage(filepath.Join(blockingFile, "frontend"))
		Expect(blockedFrontendOpts.GetSiteName()).To(BeEmpty())
		Expect(blockedFrontendOpts.GetFaviconFile()).To(BeEmpty())

		Expect(processAlive(-1)).To(BeFalse())
		Expect(processAlive(os.Getpid())).To(BeTrue())
		Expect(publicURLForListener("127.0.0.1", leafwikiFakeListener{addr: leafwikiStringAddr("listener-without-port")}, "/base")).To(Equal("http://listener-without-port/base"))
		Expect(setSysProcAttrBool(nil, "Setpgid", true)).To(BeFalse())
		Expect(setSysProcAttrBool(&syscall.SysProcAttr{}, "MissingField", true)).To(BeFalse())
		Expect(setSysProcAttrBool(&syscall.SysProcAttr{}, "Pdeathsig", true)).To(BeFalse())

		w := newFrontdActorTestWiki()
		ginkgo.DeferCleanup(w.Close)
		_, err = frontdMCPTokenVerifier(w)(context.Background(), "lwk_key_invalid", httptest.NewRequest(http.MethodPost, "/mcp", nil))
		Expect(err).To(MatchError(sdkauth.ErrInvalidToken))

		_, _, err = frontdActorUser(httptest.NewRequest(http.MethodPost, "/mcp", nil), w, leafwikiRuntimeConfig{})
		Expect(err).To(MatchError(errFrontdWorkspaceCredentialsMissing))
	})

	ginkgo.It("reports portable system errors from runtime helpers", func() {
		previousAbs := filepathAbsForRuntime
		previousRel := filepathRelForRuntime
		previousHome := userHomeDirForRuntime
		previousOpenNull := openDaemonNullDeviceForRuntime
		previousMarshal := jsonMarshalForRuntime
		previousResolveLogging := resolveLoggingForRuntime
		previousCleanupDelay := projectDaemonStartupConfigPostStartCleanupDelay
		ginkgo.DeferCleanup(func() {
			filepathAbsForRuntime = previousAbs
			filepathRelForRuntime = previousRel
			userHomeDirForRuntime = previousHome
			openDaemonNullDeviceForRuntime = previousOpenNull
			jsonMarshalForRuntime = previousMarshal
			resolveLoggingForRuntime = previousResolveLogging
			projectDaemonStartupConfigPostStartCleanupDelay = previousCleanupDelay
		})

		filepathAbsForRuntime = func(string) (string, error) {
			return "", errors.New("abs failed")
		}
		logCfg := leafwikiRuntimeConfig{Logging: leaflogging.Config{Target: leaflogging.TargetFile, FilePath: "leafwiki.log"}}
		Expect(daemonLogFileForConfig(logCfg, leafwikiTempDir())).To(Equal("leafwiki.log"))
		filepathAbsForRuntime = previousAbs

		filepathRelForRuntime = func(string, string) (string, error) {
			return "", errors.New("rel failed")
		}
		_, err := localRelativePathResult(leafwikiTempDir(), filepath.Join(leafwikiTempDir(), "leafwiki.log"))
		Expect(err).To(MatchError(errRelativePathOutsideBase))
		filepathRelForRuntime = previousRel

		userHomeDirForRuntime = func() (string, error) {
			return "", nil
		}
		_, err = globalRuntimeHomeDir()
		Expect(err).To(MatchError(errUserHomeEmpty))
		homeErr := errors.New("home failed")
		userHomeDirForRuntime = func() (string, error) {
			return "", homeErr
		}
		_, err = globalRuntimeHomeDir()
		Expect(err).To(MatchError(homeErr))
		userHomeDirForRuntime = previousHome

		openNullErr := errors.New("open null failed")
		openDaemonNullDeviceForRuntime = func() (*os.File, error) {
			return nil, openNullErr
		}
		_, _, err = openDaemonNullDevice()
		Expect(err).To(MatchError(openNullErr))
		_, err = configureDaemonOwnerIO(&exec.Cmd{}, leafwikiRuntimeConfig{MCPTransports: mcpTransports{Stdio: true}})
		Expect(err).To(MatchError(openNullErr))
		_, err = configureInternalRuntimeRoleIO(&exec.Cmd{}, leafwikiRuntimeConfig{MCPTransports: mcpTransports{Stdio: true}})
		Expect(err).To(MatchError(openNullErr))
		_, _, err = startInternalRuntimeRoleProcess(internalRuntimeRoleStartupConfig{Role: projectdaemon.RoleFrontd, Runtime: leafwikiRuntimeConfig{MCPTransports: mcpTransports{Stdio: true}}})
		Expect(err).To(MatchError(openNullErr))
		_, err = spawnProjectDaemonOwner(leafwikiRuntimeConfig{
			Workspace:     wiki.Workspace{DataDir: leafwikiTempDir(), RootDir: leafwikiTempDir()},
			DisableAuth:   true,
			MCPTransports: mcpTransports{Stdio: true},
			Logging:       leaflogging.Config{Target: leaflogging.TargetStderr},
		})
		Expect(err).To(MatchError(openNullErr))
		openDaemonNullDeviceForRuntime = previousOpenNull

		marshalErr := errors.New("marshal failed")
		jsonMarshalForRuntime = func(any) ([]byte, error) {
			return nil, marshalErr
		}
		startupErrPath := filepath.Join(leafwikiTempDir(), "startup.err")
		plainStartupErr := errors.New("project daemon startup marshal fallback")
		writeProjectDaemonStartupError(startupErrPath, plainStartupErr)
		rawFallbackStartupErr, err := os.ReadFile(startupErrPath)
		Expect(err).NotTo(HaveOccurred())
		var fallbackStartupErr projectDaemonStartupError
		Expect(json.Unmarshal(rawFallbackStartupErr, &fallbackStartupErr)).To(MatchJSONSyntaxError())
		_, err = spawnProjectDaemonOwner(leafwikiRuntimeConfig{DisableAuth: true, Logging: leaflogging.Config{Target: leaflogging.TargetStderr}})
		Expect(err).To(MatchError(marshalErr))
		_, err = writeInternalRuntimeRoleStartupConfig(internalRuntimeRoleStartupConfig{Role: projectdaemon.RoleFrontd})
		Expect(err).To(MatchError(marshalErr))
		err = writeInternalRuntimeRoleReady(filepath.Join(leafwikiTempDir(), "ready.json"), internalRuntimeRoleReady{Role: projectdaemon.RoleFrontd})
		Expect(err).To(MatchError(marshalErr))
		jsonMarshalForRuntime = previousMarshal

		projectDaemonStartupConfigPostStartCleanupDelay = 0
		scheduleProjectDaemonStartupConfigCleanup(filepath.Join(leafwikiTempDir(), "startup.json"))

		loggingResolveErr := errors.New("logging resolve failed")
		resolveLoggingForRuntime = func(leaflogging.ConfigInput) (leaflogging.Config, error) {
			return leaflogging.Config{}, loggingResolveErr
		}
		badWorkspaceCfg := leafwikiRuntimeConfig{
			Workspace:     wiki.Workspace{DataDir: leafwikiTempDir(), RootDir: leafwikiTempDir()},
			MCPTransports: mcpTransports{Stdio: true},
			Logging:       leaflogging.Config{Target: leaflogging.TargetStderr},
		}
		_, err = daemonWorkspaceRuntimeConfig(badWorkspaceCfg)
		Expect(err).To(MatchError(loggingResolveErr))
		_, err = daemonWorkspaceRequestConfigForRuntime(badWorkspaceCfg)
		Expect(err).To(MatchError(loggingResolveErr))
		_, err = daemonOwnerRuntimeConfig(badWorkspaceCfg)
		Expect(err).To(MatchError(loggingResolveErr))
		_, err = spawnProjectDaemonOwner(badWorkspaceCfg)
		Expect(err).To(MatchError(loggingResolveErr))
		resolveLoggingForRuntime = previousResolveLogging
	})

	ginkgo.It("reports process, wait, bridge, and role dependency failures", func() {
		previousWaitTimeout := projectDaemonWaitTimeout
		previousFindProcess := processFindProcessForRuntime
		previousStartCommand := startCommandForRuntime
		previousReleaseProcess := releaseProcessForRuntime
		previousCreateTempFile := createTempFileForRuntime
		previousBridgeTransports := bridgeTransportsForRuntime
		previousDefaultStdin := defaultDaemonStdinForRuntime
		previousDefaultStdout := defaultDaemonStdoutForRuntime
		previousNotifySignals := notifyRuntimeSignalsForRuntime
		previousStopSignals := stopRuntimeSignalsForRuntime
		previousShutdownInternalHTTP := shutdownInternalRuntimeHTTPServerForRuntime
		previousNewRuntimeWiki := newRuntimeWikiForRuntime
		previousWorkspacesAPI := newWorkspacesAPIForRuntime
		previousWorkspaceResolver := newWikidWorkspaceResolverForRuntime
		previousPublicMCP := frontdPublicMCPHandlerForRuntime
		previousSingleResolver := newWikidSingleWorkspaceResolverForRuntime
		previousAcquireDataLock := acquireDataDirLockForRuntime
		previousAcquireRootLock := acquireRootDirLockForRuntime
		previousStatPath := statPathForRuntime
		previousMkdirAll := mkdirAllForRuntime
		previousOwner := runWikidFrontdOwnerForProjectDaemon
		ginkgo.DeferCleanup(func() {
			projectDaemonWaitTimeout = previousWaitTimeout
			processFindProcessForRuntime = previousFindProcess
			startCommandForRuntime = previousStartCommand
			releaseProcessForRuntime = previousReleaseProcess
			createTempFileForRuntime = previousCreateTempFile
			bridgeTransportsForRuntime = previousBridgeTransports
			defaultDaemonStdinForRuntime = previousDefaultStdin
			defaultDaemonStdoutForRuntime = previousDefaultStdout
			notifyRuntimeSignalsForRuntime = previousNotifySignals
			stopRuntimeSignalsForRuntime = previousStopSignals
			shutdownInternalRuntimeHTTPServerForRuntime = previousShutdownInternalHTTP
			newRuntimeWikiForRuntime = previousNewRuntimeWiki
			newWorkspacesAPIForRuntime = previousWorkspacesAPI
			newWikidWorkspaceResolverForRuntime = previousWorkspaceResolver
			frontdPublicMCPHandlerForRuntime = previousPublicMCP
			newWikidSingleWorkspaceResolverForRuntime = previousSingleResolver
			acquireDataDirLockForRuntime = previousAcquireDataLock
			acquireRootDirLockForRuntime = previousAcquireRootLock
			statPathForRuntime = previousStatPath
			mkdirAllForRuntime = previousMkdirAll
			runWikidFrontdOwnerForProjectDaemon = previousOwner
		})

		Expect(defaultDaemonStdin()).To(Equal(os.Stdin))
		Expect(defaultDaemonStdout()).To(Equal(os.Stdout))

		processFindProcessForRuntime = func(int) (*os.Process, error) {
			return nil, errors.New("find process failed")
		}
		Expect(processPIDAlive(12345)).To(BeFalse())
		processFindProcessForRuntime = previousFindProcess

		canceledHeartbeat, cancelHeartbeat := context.WithCancel(context.Background())
		cancelHeartbeat()
		Expect(runDaemonHeartbeat(canceledHeartbeat, nil, "", 0)).To(MatchError(context.Canceled))

		projectDaemonWaitTimeout = time.Millisecond
		waitErrPath := filepath.Join(leafwikiTempDir(), "startup.err")
		Expect(os.WriteFile(waitErrPath, []byte("acquire data directory lock: held"), 0o600)).To(Succeed())
		acquireDataDirLockForRuntime = func(string) (leafwikiRuntimeLock, error) {
			return nil, errors.New("data lock probe failed")
		}
		_, err := waitForProjectDaemon(context.Background(), filepath.Join(leafwikiTempDir(), "missing.json"), waitErrPath, projectdaemon.Config{DataDir: leafwikiTempDir(), RootDir: leafwikiTempDir()}, mcpTransports{})
		Expect(err).To(MatchError(errProjectLockedNoAttachableDaemon))
		acquireDataDirLockForRuntime = previousAcquireDataLock

		waitLockPath := filepath.Join(leafwikiTempDir(), "startup-lock.err")
		Expect(os.WriteFile(waitLockPath, []byte("acquire data directory lock: held"), 0o600)).To(Succeed())
		_, err = waitForProjectDaemon(context.Background(), filepath.Join(leafwikiTempDir(), "missing.json"), waitLockPath, projectdaemon.Config{DataDir: leafwikiTempDir(), RootDir: leafwikiTempDir()}, mcpTransports{})
		Expect(err).To(MatchError(errProjectLockedNoAttachableDaemon))

		descriptorLockProbeErr := errors.New("descriptor lock probe failed")
		acquireDataDirLockForRuntime = func(string) (leafwikiRuntimeLock, error) {
			return nil, descriptorLockProbeErr
		}
		badDescriptorPath := filepath.Join(leafwikiTempDir(), "bad-descriptor.json")
		Expect(os.WriteFile(badDescriptorPath, []byte("{bad"), 0o600)).To(Succeed())
		_, err = waitForProjectDaemon(context.Background(), badDescriptorPath, "", projectdaemon.Config{DataDir: leafwikiTempDir(), RootDir: leafwikiTempDir()}, mcpTransports{})
		Expect(err).To(MatchError(errProjectLockedNoAttachableDaemon))
		Expect(err).To(MatchError(descriptorLockProbeErr))
		acquireDataDirLockForRuntime = previousAcquireDataLock

		startCommandForRuntime = func(*exec.Cmd) error {
			return nil
		}
		_, _, err = startInternalRuntimeRoleProcess(internalRuntimeRoleStartupConfig{Role: projectdaemon.RoleFrontd})
		Expect(err).To(MatchError(errRuntimeRoleMissingProcessHandle))

		createCalls := 0
		startupConfigTempErr := errors.New("startup config temp failed")
		createTempFileForRuntime = func(dir string, pattern string) (leafwikiTempFile, error) {
			createCalls++
			if createCalls == 1 {
				file, err := os.CreateTemp(dir, pattern)
				if err != nil {
					return nil, err
				}
				return file, nil
			}
			return nil, startupConfigTempErr
		}
		_, err = spawnProjectDaemonOwner(leafwikiRuntimeConfig{DisableAuth: true, Logging: leaflogging.Config{Target: leaflogging.TargetStderr}})
		Expect(err).To(MatchError(startupConfigTempErr))
		createTempFileForRuntime = previousCreateTempFile

		releaseErr := errors.New("release failed")
		releaseProcessForRuntime = func(*os.Process) error {
			return releaseErr
		}
		startCommandForRuntime = func(cmd *exec.Cmd) error {
			cmd.Process = &os.Process{Pid: os.Getpid()}
			return nil
		}
		_, err = spawnProjectDaemonOwner(leafwikiRuntimeConfig{DisableAuth: true, Logging: leaflogging.Config{Target: leaflogging.TargetStderr}})
		Expect(err).To(MatchError(releaseErr))
		startCommandForRuntime = previousStartCommand
		releaseProcessForRuntime = previousReleaseProcess

		roleStopErr := errors.New("role stop failed")
		stopDone := make(chan error, 1)
		stopDone <- roleStopErr
		stopProc := &internalRuntimeRoleProcess{done: stopDone, waitDone: make(chan struct{})}
		Expect(stopProc.wait()).To(MatchError(roleStopErr))
		process, err := os.FindProcess(os.Getpid())
		Expect(err).NotTo(HaveOccurred())
		stopProc.process = process
		runtime := &wikidFrontdRuntime{processes: map[projectdaemon.RoleName]*internalRuntimeRoleProcess{projectdaemon.RoleFrontd: stopProc}}
		Expect(runtime.stop(context.Background())).To(MatchError(roleStopErr))

		defaultInput := &leafwikiErrReadCloser{err: io.EOF}
		defaultDaemonStdinForRuntime = func() io.ReadCloser { return defaultInput }
		defaultDaemonStdoutForRuntime = func() io.Writer { return io.Discard }
		bridgeErr := errors.New("bridge failed")
		bridgeTransportsForRuntime = func(context.Context, sdkmcp.Transport, sdkmcp.Transport) error {
			return bridgeErr
		}
		Expect(runDaemonStdioBridge(context.Background(), daemonStdioBridge{EndpointURL: "http://127.0.0.1:1/mcp"})).To(MatchError(bridgeErr))
		Eventually(defaultInput.Closed).WithTimeout(200 * time.Millisecond).Should(BeTrue())

		stdinErr := errors.New("stdin failed")
		filterInput := &leafwikiErrReadCloser{err: stdinErr}
		bridgeTransportsForRuntime = func(context.Context, sdkmcp.Transport, sdkmcp.Transport) error {
			time.Sleep(20 * time.Millisecond)
			return nil
		}
		Expect(runDaemonStdioBridge(context.Background(), daemonStdioBridge{EndpointURL: "http://127.0.0.1:1/mcp", Stdin: filterInput, Stdout: io.Discard})).To(MatchError(stdinErr))
		bridgeTransportsForRuntime = previousBridgeTransports
		defaultDaemonStdinForRuntime = previousDefaultStdin
		defaultDaemonStdoutForRuntime = previousDefaultStdout

		notifyRuntimeSignalsForRuntime = func(c chan<- os.Signal, sig ...os.Signal) {
			c <- os.Interrupt
		}
		stopRuntimeSignalsForRuntime = func(chan<- os.Signal) {}
		Expect(waitForInternalRuntimeRoleSignal(context.Background())).To(Succeed())
		notifyRuntimeSignalsForRuntime = previousNotifySignals
		stopRuntimeSignalsForRuntime = previousStopSignals

		shutdownErr := errors.New("shutdown failed")
		shutdownInternalRuntimeHTTPServerForRuntime = func(*http.Server, context.Context) error {
			return shutdownErr
		}
		Expect(serveInternalRuntimeHTTP(context.Background(), projectdaemon.RoleFrontd, leafwikiFakeListener{addr: leafwikiStringAddr("127.0.0.1:0")}, http.NotFoundHandler(), 0)).To(MatchError(shutdownErr))
		shutdownInternalRuntimeHTTPServerForRuntime = previousShutdownInternalHTTP

		validRuntime := leafwikiRuntimeConfig{
			Workspace: wiki.Workspace{DataDir: leafwikiTempDir(), RootDir: leafwikiTempDir()},
			Host:      "127.0.0.1",
			Port:      "0",
			Logging:   leaflogging.Config{Target: leaflogging.TargetStderr},
		}
		startup := internalRuntimeRoleStartupConfig{
			Role:          projectdaemon.RoleFrontd,
			Runtime:       validRuntime,
			DaemonToken:   "daemon-token",
			WikidURL:      "http://127.0.0.1:1",
			WorkspacedURL: "http://127.0.0.1:2",
			ReadyPath:     filepath.Join(leafwikiTempDir(), "ready.json"),
		}
		workspacesAPIErr := errors.New("workspaces api failed")
		newWorkspacesAPIForRuntime = func(string, string) (http.Handler, error) {
			return nil, workspacesAPIErr
		}
		Expect(runFrontdRole(context.Background(), startup)).To(MatchError(workspacesAPIErr))
		newWorkspacesAPIForRuntime = previousWorkspacesAPI

		workspaceResolverErr := errors.New("workspace resolver failed")
		newWikidWorkspaceResolverForRuntime = func(string, string) (func(*http.Request, workspaceid.WorkspaceID) (frontd.WorkspaceRoute, error), error) {
			return nil, workspaceResolverErr
		}
		Expect(runFrontdRole(context.Background(), startup)).To(MatchError(workspaceResolverErr))
		newWikidWorkspaceResolverForRuntime = previousWorkspaceResolver

		httpStartup := startup
		httpStartup.Runtime.MCPTransports = mcpTransports{HTTP: true}
		publicMCPErr := errors.New("public mcp failed")
		frontdPublicMCPHandlerForRuntime = func(leafwikiRuntimeConfig, string, string, string) (http.Handler, error) {
			return nil, publicMCPErr
		}
		Expect(runFrontdRole(context.Background(), httpStartup)).To(MatchError(publicMCPErr))
		frontdPublicMCPHandlerForRuntime = previousPublicMCP

		singleResolverErr := errors.New("single resolver failed")
		newWikidSingleWorkspaceResolverForRuntime = func(string, string) (func(*http.Request) (workspaceid.WorkspaceID, error), error) {
			return nil, singleResolverErr
		}
		Expect(runFrontdRole(context.Background(), httpStartup)).To(MatchError(singleResolverErr))
		newWikidSingleWorkspaceResolverForRuntime = previousSingleResolver

		runtimeWikiErr := errors.New("runtime wiki failed")
		newRuntimeWikiForRuntime = func(leafwikiRuntimeConfig, projectdaemon.Config, runtimeWikiMode) (*wiki.Wiki, error) {
			return nil, runtimeWikiErr
		}
		Expect(runWorkspacedRole(context.Background(), internalRuntimeRoleStartupConfig{Role: projectdaemon.RoleWorkspaced, Runtime: validRuntime})).To(MatchError(runtimeWikiErr))
		newRuntimeWikiForRuntime = previousNewRuntimeWiki

		ownerCfg := validRuntime
		ownerCfg.DisableAuth = true
		ownerDaemonCfg, err := daemonConfigForRuntime(ownerCfg)
		Expect(err).NotTo(HaveOccurred())
		runWikidFrontdOwnerForProjectDaemon = func(context.Context, leafwikiRuntimeConfig, projectdaemon.Config) error { return nil }
		acquireDataDirLockForRuntime = func(string) (leafwikiRuntimeLock, error) { return leafwikiFakeRuntimeLock{}, nil }
		acquireRootDirLockForRuntime = func(string) (leafwikiRuntimeLock, error) { return leafwikiFakeRuntimeLock{}, nil }
		statPathForRuntime = func(path string) (os.FileInfo, error) {
			if path == ownerDaemonCfg.DataDir {
				return nil, os.ErrNotExist
			}
			return nil, nil
		}
		mkdirDataErr := errors.New("mkdir data failed")
		mkdirAllForRuntime = func(string, os.FileMode) error { return mkdirDataErr }
		Expect(runProjectDaemonOwner(context.Background(), ownerCfg)).To(MatchError(mkdirDataErr))

		statPathForRuntime = func(path string) (os.FileInfo, error) {
			if path == ownerDaemonCfg.RootDir {
				return nil, os.ErrNotExist
			}
			return nil, nil
		}
		mkdirRootErr := errors.New("mkdir root failed")
		mkdirAllForRuntime = func(string, os.FileMode) error { return mkdirRootErr }
		Expect(runProjectDaemonOwner(context.Background(), ownerCfg)).To(MatchError(mkdirRootErr))
	})

	ginkgo.It("normalizes workspace ensure results and daemon auth callbacks", func() {

		status, err := federatedEnsureResultStatus("workspace-a", wikid.WorkspaceStatus{State: wikid.WorkspaceStateRunning}, nil)
		Expect(err).NotTo(HaveOccurred())
		Expect(status.State).To(Equal(wikid.WorkspaceStateRunning))
		_, err = federatedEnsureResultStatus("workspace-a", "unexpected", nil)
		Expect(err).To(MatchFederatedEnsureUnexpectedResult("workspace-a", "string"))
		resultErr := errors.New("ensure failed")
		_, err = federatedEnsureResultStatus("workspace-a", "unexpected", resultErr)
		Expect(err).To(MatchError(resultErr))

		var routed []string
		mux := frontdWorkspaceMux(
			http.HandlerFunc(func(http.ResponseWriter, *http.Request) { routed = append(routed, "workspace-router") }),
			http.HandlerFunc(func(http.ResponseWriter, *http.Request) { routed = append(routed, "workspace-proxy") }),
		)
		mux.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, frontd.PublicWorkspacesPrefix+"/workspace-a/status", nil))
		mux.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/pages", nil))
		Expect(routed).To(Equal([]string{"workspace-router", "workspace-proxy"}))

		mcpRouted := []string{}
		mcpMux := frontdMCPMux(
			http.HandlerFunc(func(http.ResponseWriter, *http.Request) { mcpRouted = append(mcpRouted, "base") }),
			http.HandlerFunc(func(http.ResponseWriter, *http.Request) { mcpRouted = append(mcpRouted, "workspace") }),
		)
		workspaceMCPReq := httptest.NewRequest(http.MethodPost, "/mcp/workspaces/workspace-a", nil)
		workspaceMCPReq.RemoteAddr = "127.0.0.1:1234"
		mcpMux.ServeHTTP(httptest.NewRecorder(), workspaceMCPReq)
		baseMCPReq := httptest.NewRequest(http.MethodPost, "/other-mcp", nil)
		baseMCPReq.RemoteAddr = "127.0.0.1:1234"
		mcpMux.ServeHTTP(httptest.NewRecorder(), baseMCPReq)
		Expect(mcpRouted).To(Equal([]string{"workspace", "base"}))

		previousMCPProxy := newMCPProxyWithActorForRuntime
		previousVerifyAPIKey := verifyFrontdAPIKeyForRuntime
		previousVerifyOAuth := verifyFrontdOAuthBearerTokenForRuntime
		previousGetUser := getFrontdUserByIDForRuntime
		previousEnsureHomeGrant := ensureRuntimeHomeGrantForOwner
		previousWriteDescriptor := writeDescriptorAtomicForRuntime
		previousRegisteredWorkspace := registeredFederatedWorkspaceForAttach
		ginkgo.DeferCleanup(func() {
			newMCPProxyWithActorForRuntime = previousMCPProxy
			verifyFrontdAPIKeyForRuntime = previousVerifyAPIKey
			verifyFrontdOAuthBearerTokenForRuntime = previousVerifyOAuth
			getFrontdUserByIDForRuntime = previousGetUser
			ensureRuntimeHomeGrantForOwner = previousEnsureHomeGrant
			writeDescriptorAtomicForRuntime = previousWriteDescriptor
			registeredFederatedWorkspaceForAttach = previousRegisteredWorkspace
		})

		newMCPProxyWithActorForRuntime = func(frontd.WorkspaceProxyOptions) (http.Handler, error) {
			return nil, errors.New("mcp proxy failed")
		}
		rec := httptest.NewRecorder()
		frontdWorkspaceMCPProxy(frontd.WorkspaceRoute{
			WorkspaceID: "workspace-a",
			Upstream:    "http://127.0.0.1:1",
			DaemonToken: "daemon-token",
		}, func(*http.Request) (projectdaemon.ActorContext, error) {
			return projectdaemon.ActorContext{}, nil
		}).ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/mcp", nil))
		Expect(rec).To(HaveHTTPStatus(http.StatusServiceUnavailable))

		newMCPProxyWithActorForRuntime = func(opts frontd.WorkspaceProxyOptions) (http.Handler, error) {
			return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				actor, actorErr := opts.Actor(req)
				Expect(actorErr).NotTo(HaveOccurred())
				Expect(actor.WorkspaceID).To(Equal(workspaceid.WorkspaceID("workspace-a")))
				w.WriteHeader(http.StatusNoContent)
			}), nil
		}
		rec = httptest.NewRecorder()
		frontdWorkspaceMCPProxy(frontd.WorkspaceRoute{
			WorkspaceID: "workspace-a",
			Upstream:    "http://127.0.0.1:1",
			DaemonToken: "daemon-token",
		}, func(req *http.Request) (projectdaemon.ActorContext, error) {
			workspaceID, parseErr := workspaceid.ParseWorkspaceID(req.Header.Get(projectdaemon.WorkspaceIDHeader))
			Expect(parseErr).NotTo(HaveOccurred())
			return projectdaemon.ActorContext{WorkspaceID: workspaceID}, nil
		}).ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/mcp", nil))
		Expect(rec).To(HaveHTTPStatus(http.StatusNoContent))
		newMCPProxyWithActorForRuntime = previousMCPProxy

		w := newFrontdActorTestWiki()
		ginkgo.DeferCleanup(w.Close)
		apiKeyBackendErr := errors.New("api key backend failed")
		verifyFrontdAPIKeyForRuntime = func(*wiki.Wiki, string) (*coreauth.APIKeyVerification, error) {
			return nil, apiKeyBackendErr
		}
		_, err = frontdMCPTokenVerifier(w)(context.Background(), "lwk_key_backend", httptest.NewRequest(http.MethodPost, "/mcp", nil))
		Expect(err).To(MatchError(apiKeyBackendErr))
		verifyFrontdAPIKeyForRuntime = previousVerifyAPIKey

		verifyFrontdOAuthBearerTokenForRuntime = func(*wiki.Wiki, context.Context, string, *http.Request) (*sdkauth.TokenInfo, error) {
			return &sdkauth.TokenInfo{UserID: "missing-user"}, nil
		}
		oauthUserLookupErr := errors.New("oauth user lookup failed")
		getFrontdUserByIDForRuntime = func(*wiki.Wiki, coreauth.UserID) (*coreauth.User, error) {
			return nil, oauthUserLookupErr
		}
		oauthReq := httptest.NewRequest(http.MethodPost, "/mcp", nil)
		oauthReq.Header.Set("Authorization", "Bearer oauth-token")
		_, _, err = frontdActorUser(oauthReq, w, leafwikiRuntimeConfig{})
		Expect(err).To(MatchError(oauthUserLookupErr))
		getFrontdUserByIDForRuntime = previousGetUser
		verifyFrontdOAuthBearerTokenForRuntime = previousVerifyOAuth

		_, err = runtimeWorkspaceSubject(httptest.NewRequest(http.MethodPost, "/__leafwiki/workspaces/workspace-a/ensure", nil), w, leafwikiRuntimeConfig{}, nil)
		Expect(err).To(MatchError(errFrontdWorkspaceCredentialsMissing))
		homeGrantErr := errors.New("home grant failed")
		ensureRuntimeHomeGrantForOwner = func(*wikid.GrantStore, *coreauth.User) error {
			return homeGrantErr
		}
		_, err = runtimeWorkspaceSubject(httptest.NewRequest(http.MethodPost, "/__leafwiki/workspaces/home/ensure", nil), w, leafwikiRuntimeConfig{DisableAuth: true, Workspace: wiki.Workspace{ID: "home"}}, nil)
		Expect(err).To(MatchError(homeGrantErr))
		ensureRuntimeHomeGrantForOwner = previousEnsureHomeGrant
		ensureRuntimeHomeGrantForOwner = func(*wikid.GrantStore, *coreauth.User) error {
			return nil
		}
		subject, err := runtimeWorkspaceSubject(httptest.NewRequest(http.MethodPost, "/__leafwiki/workspaces/home/ensure", nil), w, leafwikiRuntimeConfig{DisableAuth: true, Workspace: wiki.Workspace{ID: "home"}}, nil)
		Expect(err).NotTo(HaveOccurred())
		Expect(subject.Subject).To(Equal("user:public-editor"))
		ensureRuntimeHomeGrantForOwner = previousEnsureHomeGrant

		rec = httptest.NewRecorder()
		runtimeTokenVerifyHandler(w).ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/__leafwiki/token/verify", nil))
		Expect(rec).To(HaveHTTPStatus(http.StatusUnauthorized))
		err = verifyOwnerControlAPIKey(projectdaemon.Config{DataDir: leafwikiTempDir()}, "lwk_key_missing")
		Expect(err).To(MatchError(projectdaemon.ErrInvalidAPIKey))

		writeDescriptorAtomicForRuntime = func(string, *projectdaemon.Descriptor) error {
			return errors.New("descriptor write failed")
		}
		desc := &projectdaemon.Descriptor{}
		supervisor := wikid.NewWorkspaceSupervisor(wikid.WorkspaceSupervisorOptions{})
		updateRuntimeRoleDescriptors(&sync.Mutex{}, desc, supervisor, "descriptor.json", "global.json", []projectdaemon.RoleHealth{{
			Name:  projectdaemon.RoleFrontd,
			URL:   "http://127.0.0.1:4321",
			State: projectdaemon.RoleStateReady,
		}})
		Expect(desc.PublicURL).To(Equal("http://127.0.0.1:4321"))
		writeDescriptorAtomicForRuntime = previousWriteDescriptor

		registeredWorkspaceErr := errors.New("registered workspace failed")
		registeredFederatedWorkspaceForAttach = func(wikid.Layout, projectdaemon.Config) (wikid.WorkspaceRecord, bool, error) {
			return wikid.WorkspaceRecord{}, false, registeredWorkspaceErr
		}
		cfg := leafwikiRuntimeConfig{
			Workspace:     wiki.Workspace{DataDir: leafwikiTempDir(), RootDir: leafwikiTempDir()},
			MCPTransports: mcpTransports{Stdio: true},
		}
		requestCfg, err := daemonWorkspaceRequestConfigForRuntime(cfg)
		Expect(err).NotTo(HaveOccurred())
		_, err = attachOrStartFederatedProjectDaemon(context.Background(), cfg, requestCfg, filepath.Join(leafwikiTempDir(), "descriptor.json"))
		Expect(err).To(MatchError(registeredWorkspaceErr))
	})

	ginkgo.It("runs wikid owner server startup and shutdown behavior", func() {
		previousNewRuntimeWiki := newRuntimeWikiForRuntime
		previousStartRuntime := startWikidFrontdRuntimeForOwner
		previousMCPProxy := newMCPProxyWithActorForRuntime
		previousServeControl := serveWikidControlServerForOwner
		previousShutdownControl := shutdownWikidControlServerForRuntime
		previousSeedHomeGrants := seedRuntimeHomeGrantsForOwner
		previousWorkspaceManager := newFederatedWorkspaceManagerForOwner
		previousWriteDescriptor := writeDescriptorAtomicForRuntime
		ginkgo.DeferCleanup(func() {
			newRuntimeWikiForRuntime = previousNewRuntimeWiki
			startWikidFrontdRuntimeForOwner = previousStartRuntime
			newMCPProxyWithActorForRuntime = previousMCPProxy
			serveWikidControlServerForOwner = previousServeControl
			shutdownWikidControlServerForRuntime = previousShutdownControl
			seedRuntimeHomeGrantsForOwner = previousSeedHomeGrants
			newFederatedWorkspaceManagerForOwner = previousWorkspaceManager
			writeDescriptorAtomicForRuntime = previousWriteDescriptor
		})

		baseCfg := leafwikiRuntimeConfig{
			Workspace:           wiki.Workspace{ID: "home", DataDir: leafwikiTempDir(), RootDir: leafwikiTempDir()},
			Host:                "127.0.0.1",
			Port:                "0",
			DisableAuth:         true,
			DisableIdleShutdown: true,
			DaemonIdleTimeout:   time.Minute,
			Logging:             leaflogging.Config{Target: leaflogging.TargetStderr},
		}
		ownerCfg, err := daemonConfigForRuntime(baseCfg)
		Expect(err).NotTo(HaveOccurred())
		newRuntimeWikiForRuntime = func(leafwikiRuntimeConfig, projectdaemon.Config, runtimeWikiMode) (*wiki.Wiki, error) {
			return newFrontdActorTestWiki(), nil
		}
		newMCPProxyWithActorForRuntime = func(frontd.WorkspaceProxyOptions) (http.Handler, error) {
			return http.NotFoundHandler(), nil
		}
		fakeRuntime := func(stopErr error) *wikidFrontdRuntime {
			runtimeCtx, runtimeCancel := context.WithCancel(context.Background())
			supervisor := wikid.NewSupervisor(wikid.SupervisorOptions{})
			supervisor.MarkReady(projectdaemon.RoleWikid, os.Getpid(), "", false)
			supervisor.MarkReady(projectdaemon.RoleWorkspaced, 123, "http://127.0.0.1:65535", true)
			processes := map[projectdaemon.RoleName]*internalRuntimeRoleProcess{}
			if stopErr != nil {
				process, findErr := os.FindProcess(os.Getpid())
				Expect(findErr).NotTo(HaveOccurred())
				done := make(chan error, 1)
				done <- stopErr
				proc := &internalRuntimeRoleProcess{
					role:     projectdaemon.RoleFrontd,
					process:  process,
					done:     done,
					waitDone: make(chan struct{}),
				}
				Expect(proc.wait()).To(MatchError(stopErr))
				processes[projectdaemon.RoleFrontd] = proc
			}
			return &wikidFrontdRuntime{
				ctx:           runtimeCtx,
				cancel:        runtimeCancel,
				supervisor:    supervisor,
				processes:     processes,
				roles:         []projectdaemon.RoleHealth{{Name: projectdaemon.RoleWorkspaced, State: projectdaemon.RoleStateReady, PID: 123, URL: "http://127.0.0.1:65535", Private: true}},
				workspacedURL: "http://127.0.0.1:65535",
			}
		}

		runtimeStopErr := errors.New("runtime stop failed")
		startWikidFrontdRuntimeForOwner = func(context.Context, leafwikiRuntimeConfig, string, string) (*wikidFrontdRuntime, error) {
			return fakeRuntime(runtimeStopErr), nil
		}
		newFederatedWorkspaceManagerForOwner = func(base leafwikiRuntimeConfig, daemonToken string, wikidURL string, layout wikid.Layout, supervisor *wikid.WorkspaceSupervisor) *federatedWorkspaceManager {
			manager := newFederatedWorkspaceManager(base, daemonToken, wikidURL, layout, supervisor)
			process, findErr := os.FindProcess(os.Getpid())
			Expect(findErr).NotTo(HaveOccurred())
			workspaceStopErr := errors.New("workspace stop failed")
			done := make(chan error, 1)
			done <- workspaceStopErr
			proc := &internalRuntimeRoleProcess{
				role:     projectdaemon.RoleWorkspaced,
				process:  process,
				done:     done,
				waitDone: make(chan struct{}),
			}
			Expect(proc.wait()).To(MatchError(workspaceStopErr))
			manager.processes["workspace-stop"] = proc
			return manager
		}
		writeCount := 0
		writeDescriptorAtomicForRuntime = func(string, *projectdaemon.Descriptor) error {
			writeCount++
			if writeCount > 2 {
				return errors.New("descriptor update failed")
			}
			return nil
		}
		controlServerErr := errors.New("control server failed")
		serveWikidControlServerForOwner = func(*http.Server, net.Listener) <-chan error {
			done := make(chan error, 1)
			done <- controlServerErr
			return done
		}
		err = runWikidFrontdOwner(context.Background(), baseCfg, ownerCfg)
		Expect(err).To(MatchError(controlServerErr))
		Expect(writeCount).To(BeNumerically(">=", 4))
		newFederatedWorkspaceManagerForOwner = previousWorkspaceManager

		bootstrapOwnerCfg := ownerCfg
		bootstrapOwnerCfg.DataDir = filepath.Join(blockingPathForLeafwikiTest(), "data")
		err = runWikidFrontdOwner(context.Background(), baseCfg, bootstrapOwnerCfg)
		Expect(err).To(MatchError(syscall.ENOTDIR))

		seedErr := errors.New("seed failed")
		seedRuntimeHomeGrantsForOwner = func(*wikid.GrantStore, leafwikiRuntimeConfig) error {
			return seedErr
		}
		writeDescriptorAtomicForRuntime = previousWriteDescriptor
		serveWikidControlServerForOwner = previousServeControl
		startWikidFrontdRuntimeForOwner = func(context.Context, leafwikiRuntimeConfig, string, string) (*wikidFrontdRuntime, error) {
			return fakeRuntime(nil), nil
		}
		err = runWikidFrontdOwner(context.Background(), baseCfg, ownerCfg)
		Expect(err).To(MatchError(seedErr))
		seedRuntimeHomeGrantsForOwner = previousSeedHomeGrants

		canceledCtx, cancel := context.WithCancel(context.Background())
		cancel()
		serveWikidControlServerForOwner = func(*http.Server, net.Listener) <-chan error {
			return make(chan error)
		}
		controlShutdownErr := errors.New("control shutdown failed")
		shutdownWikidControlServerForRuntime = func(*http.Server, context.Context) error {
			return controlShutdownErr
		}
		err = runWikidFrontdOwner(canceledCtx, baseCfg, ownerCfg)
		Expect(err).To(MatchError(controlShutdownErr))
		shutdownWikidControlServerForRuntime = previousShutdownControl

		Eventually(serveWikidControlServer(&http.Server{Handler: http.NotFoundHandler()}, leafwikiFakeListener{addr: leafwikiStringAddr("127.0.0.1:0")})).Should(Receive(Succeed()))
		Expect(shutdownHTTPServer(&http.Server{}, context.Background())).To(Succeed())
	})

	ginkgo.It("validates descriptor health and manager cleanup behavior", func() {
		dataDir := filepath.Join(leafwikiTempDir(), "data")
		rootDir := filepath.Join(leafwikiTempDir(), "root")
		Expect(os.MkdirAll(dataDir, 0o755)).To(Succeed())
		Expect(os.MkdirAll(rootDir, 0o755)).To(Succeed())
		blockingFile := blockingPathForLeafwikiTest()

		descriptorPath := filepath.Join(leafwikiTempDir(), "descriptor.json")
		Expect(os.WriteFile(descriptorPath, []byte("{bad"), 0o600)).To(Succeed())
		desc, err := readHealthyProjectDaemonResult(context.Background(), descriptorPath, projectdaemon.Config{DataDir: filepath.Join(blockingFile, "data"), RootDir: rootDir})
		Expect(err).To(MatchPathErrorIs(syscall.ENOTDIR))
		Expect(desc).To(BeNil())

		untrustedWorkspacedDescriptorPath := filepath.Join(leafwikiTempDir(), "untrusted-workspaced.json")
		Expect(projectdaemon.WriteDescriptorAtomic(untrustedWorkspacedDescriptorPath, &projectdaemon.Descriptor{
			SchemaVersion:   projectdaemon.DescriptorSchemaVersion,
			Role:            projectdaemon.RoleWorkspaced,
			PID:             os.Getpid(),
			DataDir:         filepath.Join(leafwikiTempDir(), "other-data"),
			RootDir:         filepath.Join(leafwikiTempDir(), "other-root"),
			PrivateMCPURL:   "https://example.com/mcp",
			PrivateMCPToken: "private-token",
		})).To(Succeed())
		desc, err = readHealthyProjectDaemonResult(context.Background(), untrustedWorkspacedDescriptorPath, projectdaemon.Config{DataDir: dataDir, RootDir: rootDir})
		Expect(err).To(MatchError(errPrivateMCPURLUntrusted))
		Expect(desc).To(BeNil())

		mismatchedDescriptorPath := filepath.Join(leafwikiTempDir(), "mismatched.json")
		Expect(projectdaemon.WriteDescriptorAtomic(mismatchedDescriptorPath, &projectdaemon.Descriptor{
			SchemaVersion: projectdaemon.DescriptorSchemaVersion,
			Role:          projectdaemon.RoleWikid,
			PID:           os.Getpid(),
			DataDir:       leafwikiTempDir(),
			RootDir:       leafwikiTempDir(),
		})).To(Succeed())
		descriptorRead := readHealthyProjectDaemonLockResult(context.Background(), mismatchedDescriptorPath, projectdaemon.Config{DataDir: dataDir, RootDir: rootDir})
		Expect(descriptorRead.Err).NotTo(HaveOccurred())
		Expect(descriptorRead).To(MatchUnhealthyProjectDaemonDescriptor())

		staleDescriptorPath := filepath.Join(leafwikiTempDir(), "stale.json")
		Expect(projectdaemon.WriteDescriptorAtomic(staleDescriptorPath, &projectdaemon.Descriptor{
			SchemaVersion: 0,
			DataDir:       dataDir,
			RootDir:       rootDir,
		})).To(Succeed())
		desc, err = readHealthyProjectDaemonResult(context.Background(), staleDescriptorPath, projectdaemon.Config{DataDir: filepath.Join(blockingFile, "data"), RootDir: rootDir})
		Expect(err).To(MatchPathErrorIs(syscall.ENOTDIR))
		Expect(desc).To(BeNil())

		err = projectDaemonDescriptorHealthyResult(context.Background(), &projectdaemon.Descriptor{DataDir: filepath.Join(blockingFile, "data"), RootDir: rootDir})
		Expect(err).To(MatchPathError())

		for _, tc := range []struct {
			status int
			want   bool
		}{
			{status: http.StatusUnauthorized, want: false},
			{status: http.StatusNoContent, want: true},
		} {
			privateMCPServer := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
				Expect(req.Header).To(HaveKeyWithValue(http.CanonicalHeaderKey(projectdaemon.ControlTokenHeader), ContainElement("private-token")))
				rw.WriteHeader(tc.status)
			}))
			desc := &projectdaemon.Descriptor{
				SchemaVersion:   projectdaemon.DescriptorSchemaVersion,
				Role:            projectdaemon.RoleWorkspaced,
				PID:             os.Getpid(),
				DataDir:         dataDir,
				RootDir:         rootDir,
				PrivateMCPURL:   privateMCPServer.URL,
				PrivateMCPToken: "private-token",
			}
			healthy, err := projectDaemonDescriptorHealthy(context.Background(), desc)
			Expect(err).NotTo(HaveOccurred())
			Expect(healthy).To(Equal(tc.want))
			privateMCPServer.Close()
		}

		dataLock, err := locking.AcquireDataDirLock(dataDir)
		Expect(err).NotTo(HaveOccurred())
		rootLock, err := locking.AcquireRootDirLock(rootDir)
		Expect(err).NotTo(HaveOccurred())
		ginkgo.DeferCleanup(func() {
			Expect(dataLock.Release()).To(Succeed())
			Expect(rootLock.Release()).To(Succeed())
		})

		err = projectDaemonDescriptorHealthyResult(context.Background(), &projectdaemon.Descriptor{
			SchemaVersion: projectdaemon.DescriptorSchemaVersion,
			PID:           os.Getpid(),
			DataDir:       dataDir,
			RootDir:       rootDir,
			ControlURL:    "https://example.com",
		})
		Expect(err).To(MatchError(errControlURLUntrusted))

		unreachableDesc := &projectdaemon.Descriptor{
			SchemaVersion: projectdaemon.DescriptorSchemaVersion,
			PID:           os.Getpid(),
			DataDir:       dataDir,
			RootDir:       rootDir,
			ControlURL:    "http://127.0.0.1:1",
			ControlToken:  "control-token",
		}
		err = projectDaemonDescriptorHealthyResult(context.Background(), unreachableDesc)
		Expect(err).To(MatchError(errControlHealthUnreachable))

		mismatchServer := httptest.NewServer(projectdaemon.NewControlServer(projectdaemon.ControlServerOptions{
			Token:        "control-token",
			Sessions:     projectdaemon.NewSessionRegistry(time.Minute, nil),
			AuthDisabled: true,
			Health: projectdaemon.DaemonHealth{
				SchemaVersion: projectdaemon.DescriptorSchemaVersion,
				PID:           os.Getpid() + 1,
				DataDir:       dataDir,
				RootDir:       rootDir,
			},
		}))
		ginkgo.DeferCleanup(mismatchServer.Close)
		mismatchDesc := *unreachableDesc
		mismatchDesc.ControlURL = mismatchServer.URL
		err = projectDaemonDescriptorHealthyResult(context.Background(), &mismatchDesc)
		Expect(err).To(MatchError(errControlHealthMismatch))

		healthyServer := httptest.NewServer(projectdaemon.NewControlServer(projectdaemon.ControlServerOptions{
			Token:        "control-token",
			Sessions:     projectdaemon.NewSessionRegistry(time.Minute, nil),
			AuthDisabled: true,
			Health: projectdaemon.DaemonHealth{
				SchemaVersion: projectdaemon.DescriptorSchemaVersion,
				PID:           os.Getpid(),
				DataDir:       dataDir,
				RootDir:       rootDir,
			},
		}))
		ginkgo.DeferCleanup(healthyServer.Close)
		healthyDesc := *unreachableDesc
		healthyDesc.ControlURL = healthyServer.URL
		Expect(projectDaemonDescriptorHealthy(context.Background(), &healthyDesc)).To(BeTrue())

		mismatchPath := filepath.Join(leafwikiTempDir(), "healthy-mismatch.json")
		Expect(projectdaemon.WriteDescriptorAtomic(mismatchPath, &healthyDesc)).To(Succeed())
		desc, err = readHealthyProjectDaemonResult(context.Background(), mismatchPath, projectdaemon.Config{DataDir: filepath.Join(leafwikiTempDir(), "other-data"), RootDir: rootDir})
		Expect(err).To(MatchProjectDaemonConfigMismatch())
		Expect(desc).To(BeNil())

		manager := newFederatedWorkspaceManager(leafwikiRuntimeConfig{}, "daemon-token", "http://wikid.local", wikid.GlobalLayout(leafwikiTempDir()), wikid.NewWorkspaceSupervisor(wikid.WorkspaceSupervisorOptions{}))
		manager.descriptors["workspace-a"] = []string{"descriptor-a.json"}
		removeDescriptorErr := errors.New("remove failed")
		manager.removeDescriptor = func(string) error {
			return removeDescriptorErr
		}
		Expect(manager.stop(context.Background())).To(MatchError(removeDescriptorErr))

		descriptorDir := filepath.Join(leafwikiTempDir(), "descriptor-dir")
		Expect(os.MkdirAll(filepath.Join(descriptorDir, "child"), 0o755)).To(Succeed())
		Expect(removeNonRegularDescriptor(descriptorDir)).To(MatchPathError())

		badDescriptorManager := newFederatedWorkspaceManager(leafwikiRuntimeConfig{}, "daemon-token", "http://wikid.local", wikid.GlobalLayout(leafwikiTempDir()), wikid.NewWorkspaceSupervisor(wikid.WorkspaceSupervisorOptions{}))
		err = badDescriptorManager.writeWorkspaceDescriptor(
			wikid.WorkspaceRecord{ID: "workspace-a", DataDir: "bad\x00data", RootDir: leafwikiTempDir()},
			leafwikiRuntimeConfig{Workspace: wiki.Workspace{DataDir: "bad\x00data", RootDir: leafwikiTempDir()}},
			internalRuntimeRoleReady{Role: projectdaemon.RoleWorkspaced, PID: os.Getpid(), URL: "http://workspace.local"},
		)
		Expect(err).To(MatchPathError())

		writeFailManager := newFederatedWorkspaceManager(leafwikiRuntimeConfig{}, "daemon-token", "http://wikid.local", wikid.GlobalLayout(leafwikiTempDir()), wikid.NewWorkspaceSupervisor(wikid.WorkspaceSupervisorOptions{}))
		writeFailManager.startRole = func(startup internalRuntimeRoleStartupConfig) (*internalRuntimeRoleProcess, internalRuntimeRoleReady, error) {
			proc, done := newLeafwikiRuntimeRoleProcess(startup.Role, os.Getpid())
			ginkgo.DeferCleanup(func() {
				releaseLeafwikiRuntimeRoleProcesses([]chan error{done}, context.Canceled)
			})
			return proc, internalRuntimeRoleReady{Role: startup.Role, PID: os.Getpid(), URL: "http://workspace.local"}, nil
		}
		writeDescriptorErr := errors.New("write descriptor failed")
		writeFailManager.writeDescriptor = func(wikid.WorkspaceRecord, leafwikiRuntimeConfig, internalRuntimeRoleReady) error {
			return writeDescriptorErr
		}
		_, err = writeFailManager.Ensure(context.Background(), wikid.WorkspaceRecord{ID: "workspace-b", DataDir: leafwikiTempDir(), RootDir: leafwikiTempDir()})
		Expect(err).To(MatchError(writeDescriptorErr))

		previousHash := configHashForRuntime
		previousWriteDescriptor := writeDescriptorAtomicForRuntime
		hashErr := errors.New("hash failed")
		configHashForRuntime = func(projectdaemon.Config) (string, error) {
			return "", hashErr
		}
		ginkgo.DeferCleanup(func() {
			configHashForRuntime = previousHash
			writeDescriptorAtomicForRuntime = previousWriteDescriptor
		})
		descriptorManager := newFederatedWorkspaceManager(leafwikiRuntimeConfig{}, "daemon-token", "http://wikid.local", wikid.GlobalLayout(leafwikiTempDir()), wikid.NewWorkspaceSupervisor(wikid.WorkspaceSupervisorOptions{}))
		err = descriptorManager.writeWorkspaceDescriptor(
			wikid.WorkspaceRecord{ID: "workspace-c", DataDir: leafwikiTempDir(), RootDir: leafwikiTempDir()},
			leafwikiRuntimeConfig{Workspace: wiki.Workspace{DataDir: leafwikiTempDir(), RootDir: leafwikiTempDir()}},
			internalRuntimeRoleReady{Role: projectdaemon.RoleWorkspaced, PID: os.Getpid(), URL: "http://workspace.local"},
		)
		Expect(err).To(MatchError(hashErr))

		configHashForRuntime = previousHash
		descriptorWriteErr := errors.New("descriptor write failed")
		writeDescriptorAtomicForRuntime = func(string, *projectdaemon.Descriptor) error {
			return descriptorWriteErr
		}
		err = descriptorManager.writeWorkspaceDescriptor(
			wikid.WorkspaceRecord{ID: "workspace-d", DataDir: leafwikiTempDir(), RootDir: leafwikiTempDir()},
			leafwikiRuntimeConfig{Workspace: wiki.Workspace{DataDir: leafwikiTempDir(), RootDir: leafwikiTempDir()}},
			internalRuntimeRoleReady{Role: projectdaemon.RoleWorkspaced, PID: os.Getpid(), URL: "http://workspace.local"},
		)
		Expect(err).To(MatchError(descriptorWriteErr))

		writeDescriptorAtomicForRuntime = previousWriteDescriptor
		removeTargetManager := newFederatedWorkspaceManager(leafwikiRuntimeConfig{}, "daemon-token", "http://wikid.local", wikid.Layout{RuntimeDir: blockingFile}, wikid.NewWorkspaceSupervisor(wikid.WorkspaceSupervisorOptions{}))
		err = removeTargetManager.writeWorkspaceDescriptor(
			wikid.WorkspaceRecord{ID: "workspace-e", DataDir: leafwikiTempDir(), RootDir: leafwikiTempDir()},
			leafwikiRuntimeConfig{Workspace: wiki.Workspace{DataDir: leafwikiTempDir(), RootDir: leafwikiTempDir()}},
			internalRuntimeRoleReady{Role: projectdaemon.RoleWorkspaced, PID: os.Getpid(), URL: "http://workspace.local"},
		)
		Expect(err).To(MatchPathError())
	})

	ginkgo.It("orchestrates federated attach behavior through daemon boundaries", func() {
		cfg := leafwikiRuntimeConfig{
			Workspace: wiki.Workspace{
				DataDir: filepath.Join(leafwikiTempDir(), "workspace-data"),
				RootDir: filepath.Join(leafwikiTempDir(), "workspace-root"),
			},
			DisableAuth:  true,
			Logging:      leaflogging.Config{Target: leaflogging.TargetStderr},
			RuntimeStack: projectdaemon.RuntimeStackWikidFrontd,
		}
		requestCfg, err := daemonWorkspaceRequestConfigForRuntime(cfg)
		Expect(err).NotTo(HaveOccurred())
		globalCfg, err := daemonRequestConfigForRuntime(cfg)
		Expect(err).NotTo(HaveOccurred())
		globalDesc := &projectdaemon.Descriptor{
			SchemaVersion: projectdaemon.DescriptorSchemaVersion,
			Role:          projectdaemon.RoleWikid,
			DataDir:       globalCfg.DataDir,
			RootDir:       globalCfg.RootDir,
			ControlURL:    "http://127.0.0.1:1",
			ControlToken:  "daemon-token",
			Config:        globalCfg,
		}

		badRegistryLayout := wikid.GlobalLayout(leafwikiTempDir())
		badRegistryPath := blockingPathForLeafwikiTest()
		badRegistryLayout.DBPath = filepath.Join(badRegistryPath, "registry.db")
		_, err = registeredFederatedWorkspaceForRequestResult(badRegistryLayout, projectdaemon.Config{DataDir: leafwikiTempDir(), RootDir: leafwikiTempDir()})
		Expect(err).To(MatchPathError())

		firstContactLayout := wikid.GlobalLayout(leafwikiTempDir())
		Expect(registerFederatedFirstContactResult(firstContactLayout, projectdaemon.Config{DataDir: leafwikiTempDir(), RootDir: leafwikiTempDir()}, leafwikiRuntimeConfig{
			APIKey:        "lwk_key_missing",
			MCPTransports: mcpTransports{Stdio: true},
			JWTSecret:     "jwt",
			AdminPassword: "admin",
		})).To(MatchFederatedFirstContactError(coreauth.ErrInvalidToken))

		Expect(ensureFederatedWorkspace(context.Background(), nil, "workspace-a", leafwikiRuntimeConfig{})).To(MatchError(errGlobalWikidDescriptorUnavailable))
		invalidEnsureDesc := *globalDesc
		invalidEnsureDesc.ControlURL = "http://[::1"
		Expect(ensureFederatedWorkspace(context.Background(), &invalidEnsureDesc, "workspace-a", leafwikiRuntimeConfig{})).To(MatchURLError())
		ensureServer := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
			Expect(req.Header).To(HaveKeyWithValue(http.CanonicalHeaderKey(projectdaemon.ControlTokenHeader), ContainElement("daemon-token")))
			Expect(req.Header).To(HaveKeyWithValue("Authorization", ContainElement("Bearer stdio-key")))
			if strings.Contains(req.URL.Path, "denied") {
				writeRuntimeError(rw, http.StatusForbidden, runtimeErrorCodeWorkspaceGrantDenied)
				return
			}
			rw.WriteHeader(http.StatusNoContent)
		}))
		ginkgo.DeferCleanup(ensureServer.Close)
		ensureDesc := *globalDesc
		ensureDesc.ControlURL = ensureServer.URL
		Expect(ensureFederatedWorkspace(context.Background(), &ensureDesc, "workspace-denied", leafwikiRuntimeConfig{APIKey: "stdio-key"})).To(MatchWikidPrivateEndpoint(http.StatusForbidden, runtimeErrorCodeWorkspaceGrantDenied))
		Expect(ensureFederatedWorkspace(context.Background(), &ensureDesc, "workspace-ok", leafwikiRuntimeConfig{APIKey: "stdio-key"})).To(Succeed())

		previousReadHealthy := readHealthyProjectDaemonForAttach
		previousVerifyKey := verifyStdioAPIKeyFromStorageForAttach
		previousSpawn := spawnProjectDaemonOwnerForAttach
		previousWait := waitForProjectDaemonForAttach
		previousRegister := registerFederatedFirstContactForAttach
		previousEnsure := ensureFederatedWorkspaceForAttach
		ginkgo.DeferCleanup(func() {
			readHealthyProjectDaemonForAttach = previousReadHealthy
			verifyStdioAPIKeyFromStorageForAttach = previousVerifyKey
			spawnProjectDaemonOwnerForAttach = previousSpawn
			waitForProjectDaemonForAttach = previousWait
			registerFederatedFirstContactForAttach = previousRegister
			ensureFederatedWorkspaceForAttach = previousEnsure
		})

		stdioHomeCfg := cfg
		stdioHomeCfg.MCPTransports = mcpTransports{Stdio: true}
		directDescriptorReadErr := errors.New("direct descriptor read failed")
		readHealthyProjectDaemonForAttach = func(context.Context, string, projectdaemon.Config) (*projectdaemon.Descriptor, bool, error) {
			return nil, false, directDescriptorReadErr
		}
		_, err = attachOrStartFederatedProjectDaemon(context.Background(), stdioHomeCfg, globalCfg, filepath.Join(leafwikiTempDir(), "home-descriptor.json"))
		Expect(err).To(MatchError(directDescriptorReadErr))

		directReadCalls := 0
		readHealthyProjectDaemonForAttach = func(context.Context, string, projectdaemon.Config) (*projectdaemon.Descriptor, bool, error) {
			directReadCalls++
			if directReadCalls == 1 {
				return &projectdaemon.Descriptor{Config: globalCfg}, false, nil
			}
			return globalDesc, true, nil
		}
		desc, err := attachOrStartFederatedProjectDaemon(context.Background(), stdioHomeCfg, globalCfg, filepath.Join(leafwikiTempDir(), "stale-home-descriptor.json"))
		Expect(err).NotTo(HaveOccurred())
		Expect(desc).To(Equal(globalDesc))

		readHealthyProjectDaemonForAttach = func(context.Context, string, projectdaemon.Config) (*projectdaemon.Descriptor, bool, error) {
			return nil, false, nil
		}
		spawnProjectDaemonOwnerForAttach = func(leafwikiRuntimeConfig) (string, error) {
			return "startup.err", nil
		}
		waitForProjectDaemonForAttach = func(context.Context, string, string, projectdaemon.Config, mcpTransports) (*projectdaemon.Descriptor, error) {
			return globalDesc, nil
		}
		registerFederatedFirstContactForAttach = func(wikid.Layout, projectdaemon.Config, leafwikiRuntimeConfig) (wikid.WorkspaceRecord, bool, error) {
			return wikid.WorkspaceRecord{ID: wikid.HomeWorkspaceID}, true, nil
		}
		desc, err = attachOrStartFederatedProjectDaemon(context.Background(), cfg, requestCfg, filepath.Join(leafwikiTempDir(), "workspace-descriptor.json"))
		Expect(err).NotTo(HaveOccurred())
		Expect(desc).To(Equal(globalDesc))

		spawnErr := errors.New("spawn failed")
		spawnProjectDaemonOwnerForAttach = func(leafwikiRuntimeConfig) (string, error) {
			return "", spawnErr
		}
		_, err = attachOrStartFederatedProjectDaemon(context.Background(), cfg, requestCfg, filepath.Join(leafwikiTempDir(), "workspace-descriptor.json"))
		Expect(err).To(MatchError(spawnErr))

		spawnProjectDaemonOwnerForAttach = func(leafwikiRuntimeConfig) (string, error) {
			return "startup.err", nil
		}
		waitErr := errors.New("wait failed")
		waitForProjectDaemonForAttach = func(context.Context, string, string, projectdaemon.Config, mcpTransports) (*projectdaemon.Descriptor, error) {
			return nil, waitErr
		}
		_, err = attachOrStartFederatedProjectDaemon(context.Background(), cfg, requestCfg, filepath.Join(leafwikiTempDir(), "workspace-descriptor.json"))
		Expect(err).To(MatchError(waitErr))

		mismatchedDesc := *globalDesc
		mismatchedConfig := globalCfg
		mismatchedConfig.DataDir = filepath.Join(leafwikiTempDir(), "other")
		mismatchedDesc.Config = mismatchedConfig
		mismatchedDesc.DataDir = mismatchedConfig.DataDir
		readHealthyProjectDaemonForAttach = func(context.Context, string, projectdaemon.Config) (*projectdaemon.Descriptor, bool, error) {
			return &mismatchedDesc, true, nil
		}
		_, err = attachOrStartFederatedProjectDaemon(context.Background(), cfg, requestCfg, filepath.Join(leafwikiTempDir(), "workspace-descriptor.json"))
		Expect(err).To(MatchProjectDaemonConfigMismatch())

		readHealthyProjectDaemonForAttach = func(context.Context, string, projectdaemon.Config) (*projectdaemon.Descriptor, bool, error) {
			return globalDesc, true, nil
		}
		registerErr := errors.New("register failed")
		registerFederatedFirstContactForAttach = func(wikid.Layout, projectdaemon.Config, leafwikiRuntimeConfig) (wikid.WorkspaceRecord, bool, error) {
			return wikid.WorkspaceRecord{}, false, registerErr
		}
		_, err = attachOrStartFederatedProjectDaemon(context.Background(), cfg, requestCfg, filepath.Join(leafwikiTempDir(), "workspace-descriptor.json"))
		Expect(err).To(MatchError(registerErr))

		registerFederatedFirstContactForAttach = func(wikid.Layout, projectdaemon.Config, leafwikiRuntimeConfig) (wikid.WorkspaceRecord, bool, error) {
			return wikid.WorkspaceRecord{ID: "workspace-a"}, false, nil
		}
		ensureErr := errors.New("ensure failed")
		ensureFederatedWorkspaceForAttach = func(context.Context, *projectdaemon.Descriptor, workspaceid.WorkspaceID, leafwikiRuntimeConfig) error {
			return ensureErr
		}
		_, err = attachOrStartFederatedProjectDaemon(context.Background(), cfg, requestCfg, filepath.Join(leafwikiTempDir(), "workspace-descriptor.json"))
		Expect(err).To(MatchError(ensureErr))

		stdioCfg := cfg
		stdioCfg.DisableAuth = false
		stdioCfg.JWTSecret = "jwt"
		stdioCfg.AdminPassword = "admin"
		stdioCfg.APIKey = "bad-key"
		stdioCfg.MCPTransports = mcpTransports{Stdio: true}
		readHealthyProjectDaemonForAttach = func(context.Context, string, projectdaemon.Config) (*projectdaemon.Descriptor, bool, error) {
			return nil, false, nil
		}
		verifyStdioAPIKeyFromStorageForAttach = func(string, string) error {
			return coreauth.ErrInvalidToken
		}
		_, err = attachOrStartFederatedProjectDaemon(context.Background(), stdioCfg, requestCfg, filepath.Join(leafwikiTempDir(), "workspace-descriptor.json"))
		Expect(err).To(MatchError(projectdaemon.ErrInvalidAPIKey))

		badRuntimeCfg := cfg
		badRuntimeCfg.Workspace.DataDir = "bad\x00data"
		_, err = attachOrStartRuntimeDaemon(context.Background(), badRuntimeCfg)
		Expect(err).To(MatchPathError())

		readHealthyErr := errors.New("read healthy failed")
		readHealthyProjectDaemonForAttach = func(context.Context, string, projectdaemon.Config) (*projectdaemon.Descriptor, bool, error) {
			return nil, false, readHealthyErr
		}
		_, err = attachOrStartFederatedProjectDaemon(context.Background(), cfg, requestCfg, filepath.Join(leafwikiTempDir(), "workspace-descriptor.json"))
		Expect(err).To(MatchError(readHealthyErr))

		readHealthyProjectDaemonForAttach = func(context.Context, string, projectdaemon.Config) (*projectdaemon.Descriptor, bool, error) {
			return nil, false, nil
		}
		authRequiredCfg := cfg
		authRequiredCfg.DisableAuth = false
		_, err = attachOrStartFederatedProjectDaemon(context.Background(), authRequiredCfg, requestCfg, filepath.Join(leafwikiTempDir(), "workspace-descriptor.json"))
		Expect(err).To(MatchError(errAuthJWTSecretRequired))

		authStoreUnavailableErr := errors.New("auth store unavailable")
		verifyStdioAPIKeyFromStorageForAttach = func(string, string) error {
			return authStoreUnavailableErr
		}
		_, err = attachOrStartFederatedProjectDaemon(context.Background(), stdioCfg, requestCfg, filepath.Join(leafwikiTempDir(), "workspace-descriptor.json"))
		Expect(err).To(MatchError(authStoreUnavailableErr))

		previousHome := userHomeDirForRuntime
		userHomeDirForRuntime = func() (string, error) {
			return "", nil
		}
		ginkgo.DeferCleanup(func() {
			userHomeDirForRuntime = previousHome
		})
		_, err = attachOrStartFederatedProjectDaemon(context.Background(), cfg, requestCfg, filepath.Join(leafwikiTempDir(), "workspace-descriptor.json"))
		Expect(err).To(MatchError(errUserHomeEmpty))
	})
})

func leafwikiEdgeFlagSet() (*flag.FlagSet, *cliFlags) {
	ginkgo.GinkgoHelper()

	fs := flag.NewFlagSet("leafwiki-edge", flag.ContinueOnError)
	var errOut strings.Builder
	fs.SetOutput(&errOut)
	flags := registerFlags(fs)
	return fs, flags
}

func captureLeafwikiStdout(fn func()) string {
	ginkgo.GinkgoHelper()

	previous := os.Stdout
	reader, writer, err := os.Pipe()
	Expect(err).NotTo(HaveOccurred())
	restored := false
	ginkgo.DeferCleanup(func() {
		if !restored {
			os.Stdout = previous
		}
		_ = reader.Close()
		_ = writer.Close()
	})

	os.Stdout = writer
	fn()
	os.Stdout = previous
	restored = true
	Expect(writer.Close()).To(Succeed())

	output, err := io.ReadAll(reader)
	Expect(err).NotTo(HaveOccurred())
	return string(output)
}

type leafwikiFailWriter struct {
	err error
}

func (w leafwikiFailWriter) Write([]byte) (int, error) {
	return 0, w.err
}

type leafwikiFailAfterWriter struct {
	failAt int
	writes int
	err    error
}

func (w *leafwikiFailAfterWriter) Write(p []byte) (int, error) {
	w.writes++
	if w.writes == w.failAt {
		return 0, w.err
	}
	return len(p), nil
}

type leafwikiErrReader struct {
	err error
}

func (r leafwikiErrReader) Read([]byte) (int, error) {
	return 0, r.err
}

type leafwikiErrReadCloser struct {
	err      error
	closedMu sync.Mutex
	closed   bool
}

func (r *leafwikiErrReadCloser) Read([]byte) (int, error) {
	return 0, r.err
}

func (r *leafwikiErrReadCloser) Close() error {
	r.closedMu.Lock()
	defer r.closedMu.Unlock()
	r.closed = true
	return nil
}

func (r *leafwikiErrReadCloser) Closed() bool {
	r.closedMu.Lock()
	defer r.closedMu.Unlock()
	return r.closed
}

type leafwikiFakeTempFile struct {
	name     string
	chmodErr error
	writeErr error
	closeErr error
}

func (f *leafwikiFakeTempFile) Name() string {
	return f.name
}

func (f *leafwikiFakeTempFile) Chmod(os.FileMode) error {
	return f.chmodErr
}

func (f *leafwikiFakeTempFile) Write(p []byte) (int, error) {
	if f.writeErr != nil {
		return 0, f.writeErr
	}
	return len(p), nil
}

func (f *leafwikiFakeTempFile) Close() error {
	return f.closeErr
}

type leafwikiFakeRuntimeLock struct {
	releaseErr error
}

func (l leafwikiFakeRuntimeLock) Release() error {
	return l.releaseErr
}

type leafwikiFakeMCPTransport struct {
	conn sdkmcp.Connection
	err  error
}

func (t leafwikiFakeMCPTransport) Connect(context.Context) (sdkmcp.Connection, error) {
	if t.err != nil {
		return nil, t.err
	}
	return t.conn, nil
}

type leafwikiFakeMCPConnection struct {
	reads    chan sdkjsonrpc.Message
	writes   chan sdkjsonrpc.Message
	closed   chan struct{}
	close    sync.Once
	readErr  error
	writeErr error
}

func newLeafwikiFakeMCPConnection() *leafwikiFakeMCPConnection {
	return &leafwikiFakeMCPConnection{
		reads:  make(chan sdkjsonrpc.Message, 2),
		writes: make(chan sdkjsonrpc.Message, 1),
		closed: make(chan struct{}),
	}
}

func (c *leafwikiFakeMCPConnection) Read(ctx context.Context) (sdkjsonrpc.Message, error) {
	select {
	case msg := <-c.reads:
		if c.readErr != nil {
			return nil, c.readErr
		}
		return msg, nil
	case <-c.closed:
		if c.readErr != nil {
			return nil, c.readErr
		}
		return nil, io.EOF
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (c *leafwikiFakeMCPConnection) Write(ctx context.Context, msg sdkjsonrpc.Message) error {
	if c.writeErr != nil {
		return c.writeErr
	}
	select {
	case c.writes <- msg:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (c *leafwikiFakeMCPConnection) Close() error {
	c.close.Do(func() {
		close(c.closed)
	})
	return nil
}

func (c *leafwikiFakeMCPConnection) SessionID() string {
	return "leafwiki-test-session"
}

type leafwikiStringAddr string

func (a leafwikiStringAddr) Network() string {
	return "leafwiki-test"
}

func (a leafwikiStringAddr) String() string {
	return string(a)
}

type leafwikiFakeListener struct {
	addr net.Addr
}

func (l leafwikiFakeListener) Accept() (net.Conn, error) {
	return nil, net.ErrClosed
}

func (l leafwikiFakeListener) Close() error {
	return nil
}

func (l leafwikiFakeListener) Addr() net.Addr {
	return l.addr
}

type leafwikiErrorListener struct {
	addr net.Addr
	err  error
}

func (l leafwikiErrorListener) Accept() (net.Conn, error) {
	return nil, l.err
}

func (l leafwikiErrorListener) Close() error {
	return nil
}

func (l leafwikiErrorListener) Addr() net.Addr {
	return l.addr
}

type leafwikiFailingResponseWriter struct {
	err      error
	header   http.Header
	statuses []int
}

func (w *leafwikiFailingResponseWriter) Header() http.Header {
	if w.header == nil {
		w.header = http.Header{}
	}
	return w.header
}

func (w *leafwikiFailingResponseWriter) Write([]byte) (int, error) {
	return 0, w.err
}

func (w *leafwikiFailingResponseWriter) WriteHeader(statusCode int) {
	w.statuses = append(w.statuses, statusCode)
}

func resolveWithSDKToken(resolver func(*http.Request) (projectdaemon.ActorContext, error), bearer string, userID string) (projectdaemon.ActorContext, error) {
	ginkgo.GinkgoHelper()

	var actor projectdaemon.ActorContext
	var resolverErr error
	handler := sdkauth.RequireBearerToken(func(context.Context, string, *http.Request) (*sdkauth.TokenInfo, error) {
		return &sdkauth.TokenInfo{
			UserID:     userID,
			Scopes:     []string{"leafwiki:mcp"},
			Expiration: time.Now().Add(time.Hour),
		}, nil
	}, nil)(http.HandlerFunc(func(_ http.ResponseWriter, req *http.Request) {
		actor, resolverErr = resolver(req)
	}))
	req := httptest.NewRequest(http.MethodPost, "/mcp", nil)
	req.Header.Set("Authorization", "Bearer "+bearer)
	handler.ServeHTTP(httptest.NewRecorder(), req)
	return actor, resolverErr
}

func swapInternalRuntimeRoleStarter(fn func(internalRuntimeRoleStartupConfig) (*internalRuntimeRoleProcess, internalRuntimeRoleReady, error)) {
	ginkgo.GinkgoHelper()

	previous := startInternalRuntimeRoleProcessForRuntime
	startInternalRuntimeRoleProcessForRuntime = fn
	ginkgo.DeferCleanup(func() {
		startInternalRuntimeRoleProcessForRuntime = previous
	})
}

func newLeafwikiRuntimeRoleProcess(role projectdaemon.RoleName, pid int) (*internalRuntimeRoleProcess, chan error) {
	ginkgo.GinkgoHelper()

	done := make(chan error, 1)
	return &internalRuntimeRoleProcess{
		role:     role,
		pid:      pid,
		done:     done,
		waitDone: make(chan struct{}),
	}, done
}

func releaseLeafwikiRuntimeRoleProcesses(doneChans []chan error, err error) {
	ginkgo.GinkgoHelper()

	for _, done := range doneChans {
		select {
		case done <- err:
		default:
		}
	}
}

func waitForLeafwikiRoleState(runtime *wikidFrontdRuntime, role projectdaemon.RoleName, state projectdaemon.RoleState) {
	ginkgo.GinkgoHelper()

	Eventually(func() projectdaemon.RoleState {
		return runtime.supervisor.State(role).State
	}).WithTimeout(time.Second).WithPolling(time.Millisecond).Should(Equal(state))
}

func waitForLeafwikiDescriptor(path string) *projectdaemon.Descriptor {
	ginkgo.GinkgoHelper()

	var descriptor *projectdaemon.Descriptor
	Eventually(func(g Gomega) {
		desc, err := projectdaemon.ReadTrustedDescriptor(path)
		g.Expect(err).NotTo(HaveOccurred())
		descriptor = desc
	}).WithTimeout(3 * time.Second).WithPolling(10 * time.Millisecond).Should(Succeed())
	return descriptor
}

func waitForLeafwikiRuntimeReady(path string) internalRuntimeRoleReady {
	ginkgo.GinkgoHelper()

	var ready internalRuntimeRoleReady
	Eventually(func(g Gomega) {
		raw, err := os.ReadFile(path)
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(strings.TrimSpace(string(raw))).NotTo(BeEmpty())
		g.Expect(json.Unmarshal(raw, &ready)).To(Succeed())
	}).WithTimeout(3 * time.Second).WithPolling(10 * time.Millisecond).Should(Succeed())
	return ready
}

func newLeafwikiReadyOwnerRuntime(parent context.Context) *wikidFrontdRuntime {
	ginkgo.GinkgoHelper()

	ctx, cancel := context.WithCancel(parent)
	return &wikidFrontdRuntime{
		ctx:           ctx,
		cancel:        cancel,
		supervisor:    wikid.NewSupervisor(wikid.SupervisorOptions{}),
		processes:     map[projectdaemon.RoleName]*internalRuntimeRoleProcess{},
		workspacedURL: "http://workspaced.local",
		roles: []projectdaemon.RoleHealth{
			{Name: projectdaemon.RoleWikid, State: projectdaemon.RoleStateReady, PID: os.Getpid(), URL: "http://wikid.local"},
			{Name: projectdaemon.RoleWorkspaced, State: projectdaemon.RoleStateReady, PID: os.Getpid(), URL: "http://workspaced.local", Private: true},
			{Name: projectdaemon.RoleFrontd, State: projectdaemon.RoleStateReady, PID: os.Getpid(), URL: "http://frontd.local"},
		},
	}
}

func blockingPathForLeafwikiTest() string {
	ginkgo.GinkgoHelper()

	path := filepath.Join(leafwikiTempDir(), "not-a-dir")
	Expect(os.WriteFile(path, []byte("x"), 0o600)).To(Succeed())
	return path
}

type leafwikiExitPanic int

func PanicWithLeafwikiExit(code int) types.GomegaMatcher {
	ginkgo.GinkgoHelper()

	return gcustom.MakeMatcher(func(fn func()) (bool, error) {
		previous := leafwikiExit
		leafwikiExit = func(got int) {
			panic(leafwikiExitPanic(got))
		}
		defer func() {
			leafwikiExit = previous
		}()

		return PanicWith(leafwikiExitPanic(code)).Match(fn)
	}).WithTemplate("Expected:\n{{.FormattedActual}}\n{{.To}} panic with leafwiki exit code\n{{format .Data 1}}", code)
}
