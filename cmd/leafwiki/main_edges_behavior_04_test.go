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
	"time"

	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/gstruct"

	coreauth "github.com/perber/wiki/internal/core/auth"
	sharederrors "github.com/perber/wiki/internal/core/shared/errors"
	authmw "github.com/perber/wiki/internal/http/middleware/auth"
	"github.com/perber/wiki/internal/locking"
	leaflogging "github.com/perber/wiki/internal/logging"
	"github.com/perber/wiki/internal/projectdaemon"
	testmatchers "github.com/perber/wiki/internal/test_utils/matchers"
	"github.com/perber/wiki/internal/wiki"
	"github.com/perber/wiki/internal/wikid"
)

var _ = ginkgo.Describe("leafwiki command helper edges", func() {
	ginkgo.It("derives wikid actor context from remote-user requests", ginkgo.Label("integration"), func() {
		w := newFrontdActorTestWiki()
		ginkgo.DeferCleanup(w.Close)
		editor, err := w.UserService().CreateUser("remote-editor", "remote-editor@example.com", "password", coreauth.RoleEditor)
		Expect(err).NotTo(HaveOccurred())

		cfg := leafwikiRuntimeConfig{Workspace: wiki.Workspace{ID: newFixtureWorkspaceID("home")}}
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
		req.Header.Set(projectdaemon.WorkspaceIDHeader, newFixtureWorkspaceID("missing-workspace").HTTPHeaderValue())
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
		adminReq.Header.Set(projectdaemon.WorkspaceIDHeader, newFixtureWorkspaceID("admin-workspace").HTTPHeaderValue())
		rec = httptest.NewRecorder()
		handleWikidActorContext(rec, adminReq, w, cfg, nil, grants)
		Expect(rec).To(HaveHTTPStatus(http.StatusOK))
		Expect(json.Unmarshal(rec.Body.Bytes(), &actorContextBody)).To(Succeed())
		Expect(actorContextBody.Actor.Scopes).To(ContainElement("leafwiki:workspace:admin"))

		_, err = actorContextForWorkspaceGrant(nil, string(leafwikiActorAuthMethodAPIKey), cfg, newFixtureWorkspaceID("workspace-a"), wikid.GrantRoleViewer)
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
		Expect(user).To(HaveCoreAuthUserID(coreauth.UserIDFromString(editor.ID)))
		Expect(method).To(Equal(string(leafwikiActorAuthMethodRemoteUser)))

		remoteReq.Header.Set("Remote-User", "missing-user")
		_, _, err = frontdRemoteUserResult(remoteReq, w, leafwikiRuntimeConfig{EnableHTTPRemoteUser: true, TrustedProxyIPsRaw: "127.0.0.1"})
		Expect(err).To(MatchError(coreauth.ErrUserNotFound))
	})

	ginkgo.It("cancels wikid-frontd owner boot through runtime role boundaries", ginkgo.Label("integration"), func() {
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

	ginkgo.It("coordinates project daemon locks, descriptors, and foreground waits", ginkgo.Label("integration"), func() {
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

	ginkgo.It("starts direct runtime role processes through startup helpers", ginkgo.Label("integration"), func() {

		Expect((*wikidFrontdRuntime)(nil).stop(context.Background())).To(Succeed())
		Expect((*internalRuntimeRoleProcess)(nil).wait()).To(Succeed())
		Expect(classifyRuntimeRoleProcessDone((*internalRuntimeRoleProcess)(nil))).To(Equal(runtimeRoleProcessDone))
		Expect((*internalRuntimeRoleProcess)(nil).stop(context.Background())).To(Succeed())

		done := make(chan error, 1)
		proc := &internalRuntimeRoleProcess{done: done, waitDone: make(chan struct{})}
		doneErr := errors.New("role exited")
		done <- doneErr
		Expect(proc.wait()).To(MatchError(doneErr))
		Expect(classifyRuntimeRoleProcessDone(proc)).To(Equal(runtimeRoleProcessDone))
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
})
