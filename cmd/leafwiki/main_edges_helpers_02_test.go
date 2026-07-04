package main

import (
	"context"
	"errors"
	"net/http"
	"path/filepath"
	"syscall"

	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/gcustom"
	"github.com/onsi/gomega/gstruct"
	"github.com/onsi/gomega/types"

	sharederrors "github.com/perber/wiki/internal/core/shared/errors"
	leaflogging "github.com/perber/wiki/internal/logging"
	"github.com/perber/wiki/internal/projectdaemon"
	"github.com/perber/wiki/internal/wiki"
	"github.com/perber/wiki/internal/wikid"
	"github.com/perber/wiki/internal/workspaceid"
	sqlite "modernc.org/sqlite"
)

const (
	workspaceSyncConfigDisabled workspaceSyncConfigState = iota
	workspaceSyncConfigEnabled
)

func classifyWorkspaceSyncConfig(cfg projectdaemon.Config) workspaceSyncConfigState {
	if cfg.EnableWorkspaceSync {
		return workspaceSyncConfigEnabled
	}
	return workspaceSyncConfigDisabled
}

type federatedEnsureUnexpectedResultObservation struct {
	WorkspaceID workspaceid.WorkspaceID
	ResultType  string
}

func observeFederatedEnsureUnexpectedResult(err error) federatedEnsureUnexpectedResultObservation {
	var unexpectedErr *federatedEnsureUnexpectedResultError
	if !errors.As(err, &unexpectedErr) {
		return federatedEnsureUnexpectedResultObservation{}
	}
	return federatedEnsureUnexpectedResultObservation{
		WorkspaceID: unexpectedErr.WorkspaceID,
		ResultType:  unexpectedErr.ResultType,
	}
}

type projectDaemonControlStatusObservation struct {
	Status int
}

func observeProjectDaemonControlStatus(err error) projectDaemonControlStatusObservation {
	for _, status := range []int{
		http.StatusBadRequest,
		http.StatusUnauthorized,
		http.StatusForbidden,
		http.StatusNotFound,
		http.StatusInternalServerError,
		http.StatusBadGateway,
		http.StatusServiceUnavailable,
	} {
		if projectdaemon.IsControlStatus(err, status) {
			return projectDaemonControlStatusObservation{Status: status}
		}
	}
	return projectDaemonControlStatusObservation{}
}

type usageDispatchState uint8

const (
	usageDispatchContinuesStartup usageDispatchState = iota
	usageDispatchPrintsUsage
)

func classifyUsageDispatch(args []string) usageDispatchState {
	if shouldPrintUsage(args) {
		return usageDispatchPrintsUsage
	}
	return usageDispatchContinuesStartup
}

type startupPositionalCommandState uint8

const (
	startupPositionalCommandContinues startupPositionalCommandState = iota
	startupPositionalCommandHandledUsage
)

func classifyStartupPositionalCommand(args []string, daemon bool, command string) startupPositionalCommandState {
	if handleStartupPositionalCommand(args, daemon, command) {
		return startupPositionalCommandHandledUsage
	}
	return startupPositionalCommandContinues
}

type daemonLogFileState uint8

const (
	daemonLogFileAbsent daemonLogFileState = iota
	daemonLogFileResolvedAbsolute
	daemonLogFileRelativeFallback
)

type daemonLogFileObservation struct {
	State daemonLogFileState
	Path  string
}

func observeDaemonLogFileForConfig(cfg leafwikiRuntimeConfig, canonicalDataDir string) daemonLogFileObservation {
	path := daemonLogFileForConfig(cfg, canonicalDataDir)
	switch {
	case path == "":
		return daemonLogFileObservation{State: daemonLogFileAbsent}
	case filepath.IsAbs(path):
		return daemonLogFileObservation{State: daemonLogFileResolvedAbsolute, Path: path}
	default:
		return daemonLogFileObservation{State: daemonLogFileRelativeFallback, Path: path}
	}
}

type loggingTargetState uint8

const (
	loggingTargetOther loggingTargetState = iota
	loggingTargetFile
)

func classifyLoggingTarget(raw string) loggingTargetState {
	if leaflogging.Target(raw) == leaflogging.TargetFile {
		return loggingTargetFile
	}
	return loggingTargetOther
}

type wikidPrivateAuthFailureState uint8

const (
	wikidPrivateAuthFailureOther wikidPrivateAuthFailureState = iota
	wikidPrivateAuthFailureRejected
)

func classifyWikidPrivateAuthFailure(err error) wikidPrivateAuthFailureState {
	var endpointErr *wikidPrivateEndpointError
	if !errors.As(err, &endpointErr) {
		return wikidPrivateAuthFailureOther
	}
	if endpointErr.StatusCode == http.StatusUnauthorized ||
		endpointErr.Code == errCodeStdioAuthAPIKeyInvalid ||
		endpointErr.Code == errCodeMCPActorContextInvalid {
		return wikidPrivateAuthFailureRejected
	}
	return wikidPrivateAuthFailureOther
}

type runtimeRoleProcessDoneState uint8

const (
	runtimeRoleProcessRunning runtimeRoleProcessDoneState = iota
	runtimeRoleProcessDone
)

func classifyRuntimeRoleProcessDone(proc *internalRuntimeRoleProcess) runtimeRoleProcessDoneState {
	if proc.isDone() {
		return runtimeRoleProcessDone
	}
	return runtimeRoleProcessRunning
}

type sysProcAttrMutationState uint8

const (
	sysProcAttrMutationUnsupported sysProcAttrMutationState = iota
	sysProcAttrMutationApplied
)

func observeSysProcAttrBool(attr *syscall.SysProcAttr, field string, value bool) sysProcAttrMutationState {
	if setSysProcAttrBool(attr, field, value) {
		return sysProcAttrMutationApplied
	}
	return sysProcAttrMutationUnsupported
}

type processLivenessState uint8

const (
	processNotRunning processLivenessState = iota
	processRunning
)

func classifyProcessExists(pid int) processLivenessState {
	if processExists(pid) {
		return processRunning
	}
	return processNotRunning
}

func MatchPathError() types.GomegaMatcher {
	return WithTransform(classifyCLIError, Equal(cliErrorPath))
}

func MatchPathErrorIs(target error) types.GomegaMatcher {
	ginkgo.GinkgoHelper()

	return WithTransform(func(err error) cliErrorObservation {
		return observeCLIErrorTarget(err, target)
	}, Equal(cliErrorObservation{Kind: cliErrorPath, Target: target}))
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

	return WithTransform(classifyStartupWorkspaceResolution, Equal(startupWorkspaceResolutionSubcommandSkip))
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

	return WithTransform(classifyProjectDaemonDescriptorRead, Equal(projectDaemonDescriptorReadObservation{State: projectDaemonDescriptorReadPreservedByLock}))
}

func MatchWikidPrivateEndpointStatus(statusCode int) types.GomegaMatcher {
	return WithTransform(observeWikidPrivateEndpointStatus, Equal(wikidPrivateEndpointStatusObservation{Status: statusCode}))
}

func MatchJSONSyntaxError() types.GomegaMatcher {
	return WithTransform(classifyCLIError, Equal(cliErrorJSONSyntax))
}

func BeCleanNativeStdioClose() types.GomegaMatcher {
	ginkgo.GinkgoHelper()

	return WithTransform(classifyCLIError, Equal(cliErrorCleanNativeStdioClose))
}

func BeTrustedDaemonControlURL() types.GomegaMatcher {
	ginkgo.GinkgoHelper()

	return WithTransform(classifyDaemonControlURL, Equal(daemonControlURLTrusted))
}

func MatchDaemonHealthDescriptor(desc *projectdaemon.Descriptor) types.GomegaMatcher {
	ginkgo.GinkgoHelper()

	return WithTransform(func(health *projectdaemon.DaemonHealth) daemonHealthDescriptorState {
		return classifyDaemonHealthDescriptor(desc, health)
	}, Equal(daemonHealthMatchesExpectedDescriptor))
}

func BeHealthyProjectDaemonDescriptor(ctx context.Context) types.GomegaMatcher {
	ginkgo.GinkgoHelper()

	return WithTransform(func(desc *projectdaemon.Descriptor) projectDaemonDescriptorHealthState {
		return classifyProjectDaemonDescriptorHealth(ctx, desc)
	}, Equal(projectDaemonDescriptorHealthyState))
}

func BeUnhealthyProjectDaemonDescriptor(ctx context.Context) types.GomegaMatcher {
	ginkgo.GinkgoHelper()

	return WithTransform(func(desc *projectdaemon.Descriptor) projectDaemonDescriptorHealthState {
		return classifyProjectDaemonDescriptorHealth(ctx, desc)
	}, Equal(projectDaemonDescriptorUnhealthyState))
}

func MatchInvalidWorkspacedUpstream() types.GomegaMatcher {
	ginkgo.GinkgoHelper()

	return WithTransform(classifyCLIError, Equal(cliErrorInvalidWorkspacedUpstream))
}

func MatchInvalidWikidUpstream() types.GomegaMatcher {
	ginkgo.GinkgoHelper()

	return WithTransform(classifyCLIError, Equal(cliErrorInvalidWikidUpstream))
}

func MatchHeldRuntimeLockError() types.GomegaMatcher {
	ginkgo.GinkgoHelper()

	return WithTransform(classifyCLIError, Equal(cliErrorHeldRuntimeLock))
}

func HaveAvailableProjectDaemonLocks() types.GomegaMatcher {
	ginkgo.GinkgoHelper()

	return WithTransform(observeProjectDaemonLockAvailability, Equal(projectDaemonLockObservation{State: projectDaemonLocksAvailable}))
}

func HaveHeldProjectDaemonLocks() types.GomegaMatcher {
	ginkgo.GinkgoHelper()

	return WithTransform(observeProjectDaemonHeldLocks, Equal(projectDaemonLockObservation{State: projectDaemonLocksHeldState}))
}

func HaveFreeDataLockWithHeldRootLock() types.GomegaMatcher {
	ginkgo.GinkgoHelper()

	return WithTransform(observeProjectDaemonDataRootLocks, Equal(projectDaemonLockObservation{State: projectDaemonDataLockFreeRootLockHeldState}))
}

func MatchProjectDaemonLockAvailabilityError(target error) types.GomegaMatcher {
	ginkgo.GinkgoHelper()

	return WithTransform(func(probe projectDaemonLockProbe) projectDaemonLockObservation {
		return observeProjectDaemonLockAvailabilityTarget(probe, target)
	}, Equal(projectDaemonLockObservation{State: projectDaemonLockAvailabilityError, Target: target}))
}

func MatchProjectDaemonHeldLockProbeError(target error) types.GomegaMatcher {
	ginkgo.GinkgoHelper()

	return WithTransform(func(probe projectDaemonLockProbe) projectDaemonLockObservation {
		return observeProjectDaemonHeldLocksTarget(probe, target)
	}, Equal(projectDaemonLockObservation{State: projectDaemonHeldLockProbeError, Target: target}))
}

func MatchProjectDaemonDataRootLockProbeError(target error) types.GomegaMatcher {
	ginkgo.GinkgoHelper()

	return WithTransform(func(probe projectDaemonLockProbe) projectDaemonLockObservation {
		return observeProjectDaemonDataRootLocksTarget(probe, target)
	}, Equal(projectDaemonLockObservation{State: projectDaemonDataRootLockProbeError, Target: target}))
}

func MatchHomeFederatedFirstContact() types.GomegaMatcher {
	ginkgo.GinkgoHelper()

	return WithTransform(classifyFederatedFirstContact, Equal(federatedFirstContactObservation{State: federatedFirstContactHomeWorkspace}))
}

func MatchAbsentProjectDaemonHealth() types.GomegaMatcher {
	ginkgo.GinkgoHelper()

	return WithTransform(observeAbsentProjectDaemonHealth, Equal(projectDaemonDescriptorReadObservation{State: projectDaemonDescriptorReadAbsentHealth}))
}

func MatchStaleProjectDaemonHealth() types.GomegaMatcher {
	ginkgo.GinkgoHelper()

	return WithTransform(observeStaleProjectDaemonHealth, Equal(projectDaemonDescriptorReadObservation{State: projectDaemonDescriptorReadStaleHealth}))
}

func MatchProjectDaemonDescriptorReadError(target error) types.GomegaMatcher {
	ginkgo.GinkgoHelper()

	return WithTransform(func(result projectDaemonDescriptorReadResult) projectDaemonDescriptorReadObservation {
		return classifyProjectDaemonDescriptorReadTarget(result, target)
	}, Equal(projectDaemonDescriptorReadObservation{State: projectDaemonDescriptorReadError, Target: target}))
}

func projectDaemonDescriptorHealthResult(ctx context.Context, desc *projectdaemon.Descriptor) projectDaemonDescriptorReadResult {
	healthy, err := projectDaemonDescriptorHealthy(ctx, desc)
	return projectDaemonDescriptorReadResult{Descriptor: desc, Healthy: healthy, Err: err}
}

func MatchProjectDaemonDescriptorHealthError(target error) types.GomegaMatcher {
	ginkgo.GinkgoHelper()

	return WithTransform(func(result projectDaemonDescriptorReadResult) projectDaemonDescriptorReadObservation {
		return classifyProjectDaemonDescriptorReadTarget(result, target)
	}, Equal(projectDaemonDescriptorReadObservation{State: projectDaemonDescriptorHealthError, Target: target}))
}

func MatchUnreachableWorkspacedPrivateMCP() types.GomegaMatcher {
	ginkgo.GinkgoHelper()

	return WithTransform(func(desc *projectdaemon.Descriptor) projectDaemonDescriptorHealthState {
		return classifyProjectDaemonDescriptorHealth(context.Background(), desc)
	}, Equal(projectDaemonDescriptorUnhealthyState))
}

func MatchUnhealthyProjectDaemonDescriptor() types.GomegaMatcher {
	ginkgo.GinkgoHelper()

	return WithTransform(observeUnhealthyProjectDaemonDescriptor, Equal(projectDaemonDescriptorReadObservation{State: projectDaemonDescriptorReadUnhealthy}))
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

	return WithTransform(func(result federatedFirstContactResult) federatedFirstContactObservation {
		return classifyFederatedFirstContactTarget(result, target)
	}, Equal(federatedFirstContactObservation{State: federatedFirstContactError, Target: target}))
}

func MatchFederatedRegisteredWorkspace(fields gstruct.Fields) types.GomegaMatcher {
	ginkgo.GinkgoHelper()

	return gcustom.MakeMatcher(func(result federatedFirstContactResult) (bool, error) {
		if result.Err != nil {
			return false, result.Err
		}
		if result.Home {
			return false, nil
		}
		return gstruct.MatchFields(gstruct.IgnoreExtras, fields).Match(result.Workspace)
	}).WithTemplate("Expected:\n{{.FormattedActual}}\n{{.To}} match non-home federated workspace registration\n{{format .Data 1}}", fields)
}

func MatchFederatedWorkspacePathIdentity(dataDir string, rootDir string) types.GomegaMatcher {
	ginkgo.GinkgoHelper()

	return WithTransform(func(workspace wikid.WorkspaceRecord) workspacePathIdentity {
		return observeFederatedWorkspacePathIdentity(workspace, dataDir, rootDir)
	}, Equal(workspacePathIdentity{DataDir: workspacePathSameFile, RootDir: workspacePathSameFile}))
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
		WithTransform(classifyProjectDaemonRoleVisibility, Equal(projectDaemonRolePrivate)),
	)
}

func MatchWorkspaceSyncEnabledDaemonConfig(fields gstruct.Fields) types.GomegaMatcher {
	return SatisfyAll(
		gstruct.MatchFields(gstruct.IgnoreExtras, fields),
		WithTransform(classifyWorkspaceSyncConfig, Equal(workspaceSyncConfigEnabled)),
	)
}

func MatchFederatedEnsureUnexpectedResult(workspaceID workspaceid.WorkspaceID, resultType string) types.GomegaMatcher {
	expected := federatedEnsureUnexpectedResultObservation{
		WorkspaceID: workspaceID,
		ResultType:  resultType,
	}
	return WithTransform(observeFederatedEnsureUnexpectedResult, Equal(expected))
}

func MatchProjectDaemonControlStatus(status int) types.GomegaMatcher {
	ginkgo.GinkgoHelper()

	return WithTransform(observeProjectDaemonControlStatus, Equal(projectDaemonControlStatusObservation{Status: status}))
}

func MatchNetOpError() types.GomegaMatcher {
	ginkgo.GinkgoHelper()

	return WithTransform(classifyNetOpError, Equal(cliErrorNetOp))
}

func MatchURLError() types.GomegaMatcher {
	ginkgo.GinkgoHelper()

	return WithTransform(classifyURLError, Equal(cliErrorURL))
}

func MatchProcessExitError() types.GomegaMatcher {
	ginkgo.GinkgoHelper()

	return WithTransform(classifyCLIError, Equal(cliErrorProcessExit))
}

func MatchWikidPrivateEndpoint(status int, code sharederrors.ErrorCode) types.GomegaMatcher {
	ginkgo.GinkgoHelper()

	return WithTransform(observeWikidPrivateEndpoint, Equal(wikidPrivateEndpointObservation{Status: status, Code: code}))
}
