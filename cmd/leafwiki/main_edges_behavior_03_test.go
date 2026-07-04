package main

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"time"

	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/gstruct"

	coreauth "github.com/perber/wiki/internal/core/auth"
	sharederrors "github.com/perber/wiki/internal/core/shared/errors"
	"github.com/perber/wiki/internal/localization"
	leaflogging "github.com/perber/wiki/internal/logging"
	"github.com/perber/wiki/internal/projectdaemon"
	testmatchers "github.com/perber/wiki/internal/test_utils/matchers"
	"github.com/perber/wiki/internal/wikid"
	"github.com/perber/wiki/internal/workspaceid"
)

var _ = ginkgo.Describe("leafwiki command helper edges", func() {
	ginkgo.It("orchestrates wikid-frontd runtime roles through the starter boundary", ginkgo.Label("integration"), func() {
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

	ginkgo.It("reports wikid-frontd runtime startup and restart failures", ginkgo.Label("integration"), func() {
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

	ginkgo.It("restarts runtime roles and publishes crash details", ginkgo.Label("integration"), func() {
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

	ginkgo.It("authenticates daemon STDIO requests and keeps workspace descriptors scoped to local runtime state", ginkgo.Label("integration"), func() {
		dataDir := filepath.Join(leafwikiTempDir(), "data")
		rootDir := filepath.Join(leafwikiTempDir(), "root")
		Expect(os.MkdirAll(rootDir, 0o755)).To(Succeed())

		Expect(validateAuthStartupConfig(leafwikiRuntimeConfig{DisableAuth: true})).To(Succeed())
		Expect(validateAuthStartupConfig(leafwikiRuntimeConfig{})).To(MatchError(errAuthJWTSecretRequired))
		Expect(validateAuthStartupConfig(leafwikiRuntimeConfig{JWTSecret: "jwt"})).To(MatchError(errAuthAdminPasswordRequired))
		Expect(validateAuthStartupConfig(leafwikiRuntimeConfig{JWTSecret: "jwt", AdminPassword: "admin"})).To(Succeed())

		logPath := filepath.Join(dataDir, "startup.log")
		logStartupValidationFailure(leaflogging.Config{Target: leaflogging.TargetStderr}, "ignored")
		logStartupValidationFailure(leaflogging.Config{Target: leaflogging.TargetFile, FilePath: logPath}, localization.MessageIDCLIErrorLeafWikiStartupFailed)
		Expect(readJSONLogEntries(logPath)).To(ContainElement(haveJSONLogEntry(localization.MessageIDCLIErrorLeafWikiStartupFailed)))
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
})
