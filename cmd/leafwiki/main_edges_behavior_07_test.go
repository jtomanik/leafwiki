package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"time"

	sdkauth "github.com/modelcontextprotocol/go-sdk/auth"
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	leaflogging "github.com/perber/wiki/internal/logging"
	"github.com/perber/wiki/internal/projectdaemon"
	"github.com/perber/wiki/internal/wiki"
	"github.com/perber/wiki/internal/wikid"
)

var _ = ginkgo.Describe("leafwiki command helper edges", func() {
	ginkgo.It("keeps direct manager, storage, and environment helpers deterministic", ginkgo.Label("integration"), func() {

		var manager *federatedWorkspaceManager
		manager.MarkReady(newFixtureWorkspaceID("workspace-a"), 1, "http://workspace.local")
		_, err := manager.Ensure(context.Background(), wikid.WorkspaceRecord{ID: newFixtureWorkspaceID("workspace-a")})
		Expect(err).To(MatchError(errWorkspaceManagerUnavailable))

		manager = newFederatedWorkspaceManager(leafwikiRuntimeConfig{}, "daemon-token", "http://wikid.local", wikid.GlobalLayout(leafwikiTempDir()), wikid.NewWorkspaceSupervisor(wikid.WorkspaceSupervisorOptions{}))
		_, err = manager.Ensure(context.Background(), wikid.WorkspaceRecord{})
		Expect(err).To(MatchError(errWorkspaceIDRequired))

		workspace := wikid.WorkspaceRecord{ID: newFixtureWorkspaceID("workspace-a"), DataDir: leafwikiTempDir(), RootDir: leafwikiTempDir()}
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
		manager.restartWorkspaceAfter(wikid.WorkspaceRecord{ID: newFixtureWorkspaceID("workspace-a")}, time.Now().Add(-time.Second))

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
		Expect(observeSysProcAttrBool(nil, "Setpgid", true)).To(Equal(sysProcAttrMutationUnsupported))
		Expect(observeSysProcAttrBool(&syscall.SysProcAttr{}, "MissingField", true)).To(Equal(sysProcAttrMutationUnsupported))
		Expect(observeSysProcAttrBool(&syscall.SysProcAttr{}, "Pdeathsig", true)).To(Equal(sysProcAttrMutationUnsupported))

		w := newFrontdActorTestWiki()
		ginkgo.DeferCleanup(w.Close)
		_, err = frontdMCPTokenVerifier(w)(context.Background(), "lwk_key_invalid", httptest.NewRequest(http.MethodPost, "/mcp", nil))
		Expect(err).To(MatchError(sdkauth.ErrInvalidToken))

		_, _, err = frontdActorUser(httptest.NewRequest(http.MethodPost, "/mcp", nil), w, leafwikiRuntimeConfig{})
		Expect(err).To(MatchError(errFrontdWorkspaceCredentialsMissing))
	})

	ginkgo.It("reports portable system errors from runtime helpers", ginkgo.Label("integration"), func() {
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
		Expect(observeDaemonLogFileForConfig(logCfg, leafwikiTempDir())).To(Equal(daemonLogFileObservation{State: daemonLogFileRelativeFallback, Path: "leafwiki.log"}))
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
})
