package main

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/url"
	"os"
	"os/exec"
	"time"

	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/gcustom"
	"github.com/onsi/gomega/types"

	sharederrors "github.com/perber/wiki/internal/core/shared/errors"
	"github.com/perber/wiki/internal/frontd"
	"github.com/perber/wiki/internal/localization"
	"github.com/perber/wiki/internal/locking"
	"github.com/perber/wiki/internal/projectdaemon"
	"github.com/perber/wiki/internal/wiki"
	"github.com/perber/wiki/internal/wikid"
)

const (
	leafwikiNativeStdioParseErrorFrame = `{"jsonrpc":"2.0","id":null,"error":{"code":-32700,"message":"Parse error"}}`
	leafwikiFixtureDaemonStopped       = "daemon stopped"
	leafwikiFixtureFrontdExited        = "frontd exited"
)

type leafwikiFakeFileInfo struct {
	name string
	mode os.FileMode
}

func (info leafwikiFakeFileInfo) Name() string       { return info.name }
func (info leafwikiFakeFileInfo) Size() int64        { return 0 }
func (info leafwikiFakeFileInfo) Mode() os.FileMode  { return info.mode }
func (info leafwikiFakeFileInfo) ModTime() time.Time { return time.Time{} }
func (info leafwikiFakeFileInfo) IsDir() bool        { return info.mode.IsDir() }
func (info leafwikiFakeFileInfo) Sys() any           { return nil }

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

type cliErrorKind uint8

const (
	cliErrorUnknown cliErrorKind = iota
	cliErrorPath
	cliErrorJSONSyntax
	cliErrorCleanNativeStdioClose
	cliErrorInvalidWorkspacedUpstream
	cliErrorInvalidWikidUpstream
	cliErrorHeldRuntimeLock
	cliErrorNetOp
	cliErrorURL
	cliErrorProcessExit
)

type cliErrorObservation struct {
	Kind   cliErrorKind
	Target error
}

func classifyCLIError(err error) cliErrorKind {
	var pathErr *os.PathError
	var syntaxErr *json.SyntaxError
	var exitErr *exec.ExitError
	switch {
	case errors.As(err, &pathErr):
		return cliErrorPath
	case errors.As(err, &syntaxErr):
		return cliErrorJSONSyntax
	case isCleanNativeStdioClose(err):
		return cliErrorCleanNativeStdioClose
	case frontd.IsInvalidWorkspacedUpstream(err):
		return cliErrorInvalidWorkspacedUpstream
	case frontd.IsInvalidWikidUpstream(err):
		return cliErrorInvalidWikidUpstream
	case locking.IsLockHeld(err):
		return cliErrorHeldRuntimeLock
	case errors.As(err, &exitErr):
		return cliErrorProcessExit
	default:
		return cliErrorUnknown
	}
}

func classifyNetOpError(err error) cliErrorKind {
	var opErr *net.OpError
	if errors.As(err, &opErr) {
		return cliErrorNetOp
	}
	return cliErrorUnknown
}

func classifyURLError(err error) cliErrorKind {
	var urlErr *url.Error
	if errors.As(err, &urlErr) {
		return cliErrorURL
	}
	return cliErrorUnknown
}

func observeCLIErrorTarget(err error, target error) cliErrorObservation {
	observed := cliErrorObservation{Kind: classifyCLIError(err)}
	if errors.Is(err, target) {
		observed.Target = target
	}
	return observed
}

type daemonControlURLState uint8

const (
	daemonControlURLUntrusted daemonControlURLState = iota
	daemonControlURLTrusted
)

func classifyDaemonControlURL(raw string) daemonControlURLState {
	if isTrustedDaemonControlURL(raw) {
		return daemonControlURLTrusted
	}
	return daemonControlURLUntrusted
}

type startupWorkspaceResolutionState uint8

const (
	startupWorkspaceResolutionOther startupWorkspaceResolutionState = iota
	startupWorkspaceResolutionSubcommandSkip
)

func classifyStartupWorkspaceResolution(result startupWorkspaceResolution) startupWorkspaceResolutionState {
	if result.Err == nil && result.Workspace == (wiki.Workspace{}) && !result.StartsRuntime {
		return startupWorkspaceResolutionSubcommandSkip
	}
	return startupWorkspaceResolutionOther
}

type projectDaemonDescriptorReadState uint8

const (
	projectDaemonDescriptorReadOther projectDaemonDescriptorReadState = iota
	projectDaemonDescriptorReadPreservedByLock
	projectDaemonDescriptorReadAbsentHealth
	projectDaemonDescriptorReadStaleHealth
	projectDaemonDescriptorReadError
	projectDaemonDescriptorHealthError
	projectDaemonDescriptorReadUnhealthy
)

type projectDaemonDescriptorReadObservation struct {
	State  projectDaemonDescriptorReadState
	Target error
}

func classifyProjectDaemonDescriptorRead(result projectDaemonDescriptorReadResult) projectDaemonDescriptorReadObservation {
	var syntaxErr *json.SyntaxError
	switch {
	case result.Descriptor == nil && !result.Healthy && errors.As(result.Err, &syntaxErr):
		return projectDaemonDescriptorReadObservation{State: projectDaemonDescriptorReadPreservedByLock}
	case result.Descriptor == nil && !result.Healthy && result.Err == nil:
		return projectDaemonDescriptorReadObservation{State: projectDaemonDescriptorReadAbsentHealth}
	case result.Descriptor != nil && result.Descriptor.SchemaVersion == 0 && !result.Healthy:
		return projectDaemonDescriptorReadObservation{State: projectDaemonDescriptorReadStaleHealth}
	case result.Descriptor == nil && !result.Healthy && result.Err != nil:
		return projectDaemonDescriptorReadObservation{State: projectDaemonDescriptorReadError, Target: result.Err}
	case result.Descriptor != nil && !result.Healthy && result.Err != nil:
		return projectDaemonDescriptorReadObservation{State: projectDaemonDescriptorHealthError, Target: result.Err}
	case result.Descriptor != nil && !result.Healthy:
		return projectDaemonDescriptorReadObservation{State: projectDaemonDescriptorReadUnhealthy}
	default:
		return projectDaemonDescriptorReadObservation{State: projectDaemonDescriptorReadOther}
	}
}

func classifyProjectDaemonDescriptorReadTarget(result projectDaemonDescriptorReadResult, target error) projectDaemonDescriptorReadObservation {
	observed := classifyProjectDaemonDescriptorRead(result)
	if errors.Is(observed.Target, target) {
		observed.Target = target
	}
	return observed
}

func observeAbsentProjectDaemonHealth(result projectDaemonDescriptorReadResult) projectDaemonDescriptorReadObservation {
	if result.Descriptor == nil && !result.Healthy {
		return projectDaemonDescriptorReadObservation{State: projectDaemonDescriptorReadAbsentHealth}
	}
	return projectDaemonDescriptorReadObservation{State: projectDaemonDescriptorReadOther}
}

func observeStaleProjectDaemonHealth(result projectDaemonDescriptorReadResult) projectDaemonDescriptorReadObservation {
	if result.Descriptor != nil && result.Descriptor.SchemaVersion == 0 && !result.Healthy {
		return projectDaemonDescriptorReadObservation{State: projectDaemonDescriptorReadStaleHealth}
	}
	return projectDaemonDescriptorReadObservation{State: projectDaemonDescriptorReadOther}
}

func observeUnhealthyProjectDaemonDescriptor(result projectDaemonDescriptorReadResult) projectDaemonDescriptorReadObservation {
	if result.Descriptor != nil && !result.Healthy {
		return projectDaemonDescriptorReadObservation{State: projectDaemonDescriptorReadUnhealthy}
	}
	return projectDaemonDescriptorReadObservation{State: projectDaemonDescriptorReadOther}
}

type projectDaemonDescriptorHealthState uint8

const (
	projectDaemonDescriptorHealthUnknown projectDaemonDescriptorHealthState = iota
	projectDaemonDescriptorHealthyState
	projectDaemonDescriptorUnhealthyState
)

func classifyProjectDaemonDescriptorHealth(ctx context.Context, desc *projectdaemon.Descriptor) projectDaemonDescriptorHealthState {
	healthy, err := projectDaemonDescriptorHealthy(ctx, desc)
	switch {
	case err == nil && healthy:
		return projectDaemonDescriptorHealthyState
	case err == nil && !healthy:
		return projectDaemonDescriptorUnhealthyState
	default:
		return projectDaemonDescriptorHealthUnknown
	}
}

type daemonHealthDescriptorState uint8

const (
	daemonHealthDescriptorUnknown daemonHealthDescriptorState = iota
	daemonHealthMatchesExpectedDescriptor
)

func classifyDaemonHealthDescriptor(desc *projectdaemon.Descriptor, health *projectdaemon.DaemonHealth) daemonHealthDescriptorState {
	if daemonHealthMatchesDescriptor(desc, health) {
		return daemonHealthMatchesExpectedDescriptor
	}
	return daemonHealthDescriptorUnknown
}

type projectDaemonLockState uint8

const (
	projectDaemonLockUnknown projectDaemonLockState = iota
	projectDaemonLocksAvailable
	projectDaemonLocksHeldState
	projectDaemonDataLockFreeRootLockHeldState
	projectDaemonLockAvailabilityError
	projectDaemonHeldLockProbeError
	projectDaemonDataRootLockProbeError
)

type projectDaemonLockObservation struct {
	State  projectDaemonLockState
	Target error
}

func observeProjectDaemonLockAvailability(probe projectDaemonLockProbe) projectDaemonLockObservation {
	available, err := projectDaemonLocksFree(probe.DataDir, probe.RootDir)
	if available && err == nil {
		return projectDaemonLockObservation{State: projectDaemonLocksAvailable}
	}
	if err != nil {
		return projectDaemonLockObservation{State: projectDaemonLockAvailabilityError, Target: err}
	}
	return projectDaemonLockObservation{State: projectDaemonLockUnknown}
}

func observeProjectDaemonLockAvailabilityTarget(probe projectDaemonLockProbe, target error) projectDaemonLockObservation {
	observed := observeProjectDaemonLockAvailability(probe)
	if errors.Is(observed.Target, target) {
		observed.Target = target
	}
	return observed
}

func observeProjectDaemonHeldLocks(probe projectDaemonLockProbe) projectDaemonLockObservation {
	held, err := projectDaemonLocksHeld(probe.DataDir, probe.RootDir)
	if held && err == nil {
		return projectDaemonLockObservation{State: projectDaemonLocksHeldState}
	}
	if err != nil {
		return projectDaemonLockObservation{State: projectDaemonHeldLockProbeError, Target: err}
	}
	return projectDaemonLockObservation{State: projectDaemonLockUnknown}
}

func observeProjectDaemonHeldLocksTarget(probe projectDaemonLockProbe, target error) projectDaemonLockObservation {
	observed := observeProjectDaemonHeldLocks(probe)
	if errors.Is(observed.Target, target) {
		observed.Target = target
	}
	return observed
}

func observeProjectDaemonDataRootLocks(probe projectDaemonLockProbe) projectDaemonLockObservation {
	disjoint, err := projectDaemonDataLockFreeRootLockHeld(probe.DataDir, probe.RootDir)
	if disjoint && err == nil {
		return projectDaemonLockObservation{State: projectDaemonDataLockFreeRootLockHeldState}
	}
	if err != nil {
		return projectDaemonLockObservation{State: projectDaemonDataRootLockProbeError, Target: err}
	}
	return projectDaemonLockObservation{State: projectDaemonLockUnknown}
}

func observeProjectDaemonDataRootLocksTarget(probe projectDaemonLockProbe, target error) projectDaemonLockObservation {
	observed := observeProjectDaemonDataRootLocks(probe)
	if errors.Is(observed.Target, target) {
		observed.Target = target
	}
	return observed
}

type wikidPrivateEndpointObservation struct {
	Status int
	Code   sharederrors.ErrorCode
}

type wikidPrivateEndpointStatusObservation struct {
	Status int
}

func observeWikidPrivateEndpoint(err error) wikidPrivateEndpointObservation {
	var endpointErr *wikidPrivateEndpointError
	if !errors.As(err, &endpointErr) {
		return wikidPrivateEndpointObservation{}
	}
	return wikidPrivateEndpointObservation{Status: endpointErr.StatusCode, Code: endpointErr.Code}
}

func observeWikidPrivateEndpointStatus(err error) wikidPrivateEndpointStatusObservation {
	endpoint := observeWikidPrivateEndpoint(err)
	return wikidPrivateEndpointStatusObservation{Status: endpoint.Status}
}

type federatedFirstContactState uint8

const (
	federatedFirstContactOther federatedFirstContactState = iota
	federatedFirstContactHomeWorkspace
	federatedFirstContactError
)

type federatedFirstContactObservation struct {
	State  federatedFirstContactState
	Target error
}

func classifyFederatedFirstContact(result federatedFirstContactResult) federatedFirstContactObservation {
	switch {
	case result.Err == nil &&
		result.Home &&
		result.Workspace.ID == wikid.HomeWorkspaceID &&
		result.Workspace.DataDir != "" &&
		result.Workspace.RootDir != "":
		return federatedFirstContactObservation{State: federatedFirstContactHomeWorkspace}
	case result.Workspace == (wikid.WorkspaceRecord{}) && !result.Home && result.Err != nil:
		return federatedFirstContactObservation{State: federatedFirstContactError, Target: result.Err}
	default:
		return federatedFirstContactObservation{State: federatedFirstContactOther}
	}
}

func classifyFederatedFirstContactTarget(result federatedFirstContactResult, target error) federatedFirstContactObservation {
	observed := classifyFederatedFirstContact(result)
	if errors.Is(observed.Target, target) {
		observed.Target = target
	}
	return observed
}

type workspacePathState uint8

const (
	workspacePathMissing workspacePathState = iota
	workspacePathDifferent
	workspacePathSameFile
)

type workspacePathIdentity struct {
	DataDir workspacePathState
	RootDir workspacePathState
}

func observeFederatedWorkspacePathIdentity(workspace wikid.WorkspaceRecord, dataDir string, rootDir string) workspacePathIdentity {
	return workspacePathIdentity{
		DataDir: classifySameFilePath(workspace.DataDir, dataDir),
		RootDir: classifySameFilePath(workspace.RootDir, rootDir),
	}
}

func classifySameFilePath(got string, want string) workspacePathState {
	gotInfo, err := os.Stat(got)
	if err != nil {
		return workspacePathMissing
	}
	wantInfo, err := os.Stat(want)
	if err != nil {
		return workspacePathMissing
	}
	if os.SameFile(gotInfo, wantInfo) {
		return workspacePathSameFile
	}
	return workspacePathDifferent
}

type projectDaemonRoleVisibility uint8

const (
	projectDaemonRolePublic projectDaemonRoleVisibility = iota
	projectDaemonRolePrivate
)

func classifyProjectDaemonRoleVisibility(role projectdaemon.RoleHealth) projectDaemonRoleVisibility {
	if role.Private {
		return projectDaemonRolePrivate
	}
	return projectDaemonRolePublic
}

type workspaceSyncConfigState uint8
