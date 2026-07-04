package main

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"

	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	coreauth "github.com/perber/wiki/internal/core/auth"
	leaflogging "github.com/perber/wiki/internal/logging"
	"github.com/perber/wiki/internal/projectdaemon"
	"github.com/perber/wiki/internal/wiki"
	"github.com/perber/wiki/internal/wikid"
	"github.com/perber/wiki/internal/workspaceid"
)

var _ = ginkgo.Describe("leafwiki command helper edges", func() {
	ginkgo.It("orchestrates federated attach behavior through daemon boundaries", ginkgo.Label("integration"), func() {
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
