package main

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"time"

	sdkauth "github.com/modelcontextprotocol/go-sdk/auth"
	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	coreauth "github.com/perber/wiki/internal/core/auth"
	"github.com/perber/wiki/internal/frontd"
	leaflogging "github.com/perber/wiki/internal/logging"
	"github.com/perber/wiki/internal/projectdaemon"
	"github.com/perber/wiki/internal/wiki"
	"github.com/perber/wiki/internal/wikid"
	"github.com/perber/wiki/internal/workspaceid"
)

var _ = ginkgo.Describe("leafwiki command helper edges", func() {
	ginkgo.It("reports process, wait, bridge, and role dependency failures", ginkgo.Label("integration"), func() {
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
		Expect(runDaemonHeartbeat(canceledHeartbeat, nil, newFixtureSessionID(""), 0)).To(MatchError(context.Canceled))

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
			return leafwikiFakeFileInfo{name: filepath.Base(path), mode: os.ModeDir | 0o755}, nil
		}
		mkdirDataErr := errors.New("mkdir data failed")
		mkdirAllForRuntime = func(string, os.FileMode) error { return mkdirDataErr }
		Expect(runProjectDaemonOwner(context.Background(), ownerCfg)).To(MatchError(mkdirDataErr))

		statPathForRuntime = func(path string) (os.FileInfo, error) {
			if path == ownerDaemonCfg.RootDir {
				return nil, os.ErrNotExist
			}
			return leafwikiFakeFileInfo{name: filepath.Base(path), mode: os.ModeDir | 0o755}, nil
		}
		mkdirRootErr := errors.New("mkdir root failed")
		mkdirAllForRuntime = func(string, os.FileMode) error { return mkdirRootErr }
		Expect(runProjectDaemonOwner(context.Background(), ownerCfg)).To(MatchError(mkdirRootErr))
	})

	ginkgo.It("normalizes workspace ensure results and daemon auth callbacks", ginkgo.Label("integration"), func() {

		status, err := federatedEnsureResultStatus(newFixtureWorkspaceID("workspace-a"), wikid.WorkspaceStatus{State: wikid.WorkspaceStateRunning}, nil)
		Expect(err).NotTo(HaveOccurred())
		Expect(status.State).To(Equal(wikid.WorkspaceStateRunning))
		_, err = federatedEnsureResultStatus(newFixtureWorkspaceID("workspace-a"), "unexpected", nil)
		Expect(err).To(MatchFederatedEnsureUnexpectedResult(newFixtureWorkspaceID("workspace-a"), "string"))
		resultErr := errors.New("ensure failed")
		_, err = federatedEnsureResultStatus(newFixtureWorkspaceID("workspace-a"), "unexpected", resultErr)
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
			WorkspaceID: newFixtureWorkspaceID("workspace-a"),
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
				Expect(actor.WorkspaceID).To(Equal(newFixtureWorkspaceID("workspace-a")))
				w.WriteHeader(http.StatusNoContent)
			}), nil
		}
		rec = httptest.NewRecorder()
		frontdWorkspaceMCPProxy(frontd.WorkspaceRoute{
			WorkspaceID: newFixtureWorkspaceID("workspace-a"),
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
		_, err = runtimeWorkspaceSubject(httptest.NewRequest(http.MethodPost, "/__leafwiki/workspaces/home/ensure", nil), w, leafwikiRuntimeConfig{DisableAuth: true, Workspace: wiki.Workspace{ID: newFixtureWorkspaceID("home")}}, nil)
		Expect(err).To(MatchError(homeGrantErr))
		ensureRuntimeHomeGrantForOwner = previousEnsureHomeGrant
		ensureRuntimeHomeGrantForOwner = func(*wikid.GrantStore, *coreauth.User) error {
			return nil
		}
		subject, err := runtimeWorkspaceSubject(httptest.NewRequest(http.MethodPost, "/__leafwiki/workspaces/home/ensure", nil), w, leafwikiRuntimeConfig{DisableAuth: true, Workspace: wiki.Workspace{ID: newFixtureWorkspaceID("home")}}, nil)
		Expect(err).NotTo(HaveOccurred())
		Expect(subject).To(HaveActorSubjectForUser(newFixtureUserID("public-editor")))
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
})
