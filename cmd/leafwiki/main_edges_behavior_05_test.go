package main

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/perber/wiki/internal/agenthooks"
	authmw "github.com/perber/wiki/internal/http/middleware/auth"
	leaflogging "github.com/perber/wiki/internal/logging"
	"github.com/perber/wiki/internal/projectdaemon"
	"github.com/perber/wiki/internal/wiki"
)

var _ = ginkgo.Describe("leafwiki command helper edges", func() {
	ginkgo.It("reports runtime role startup process failures through existing boundaries", ginkgo.Label("integration"), func() {
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

	ginkgo.It("reports temp-file chmod, write, and close failures", ginkgo.Label("integration"), func() {
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

	ginkgo.It("reports runtime role readiness failures", ginkgo.Label("integration"), func() {

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

	ginkgo.It("reports frontd and workspaced role fast failures", ginkgo.Label("integration"), func() {
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

		_, err := newRuntimeWiki(leafwikiRuntimeConfig{Workspace: wiki.Workspace{ID: newFixtureWorkspaceID("home")}}, projectdaemon.Config{DataDir: "bad\x00data", RootDir: validRuntime.Workspace.RootDir}, runtimeWikiFull)
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

	ginkgo.It("dispatches runtime roles through project daemon launcher boundaries", ginkgo.Label("integration"), func() {
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
})
