package main

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"syscall"
	"time"

	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/perber/wiki/internal/frontd"
	"github.com/perber/wiki/internal/locking"
	leaflogging "github.com/perber/wiki/internal/logging"
	"github.com/perber/wiki/internal/projectdaemon"
	"github.com/perber/wiki/internal/wiki"
	"github.com/perber/wiki/internal/wikid"
)

var _ = ginkgo.Describe("leafwiki command helper edges", func() {
	ginkgo.It("runs wikid owner server startup and shutdown behavior", ginkgo.Label("integration"), func() {
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
			Workspace:           wiki.Workspace{ID: newFixtureWorkspaceID("home"), DataDir: leafwikiTempDir(), RootDir: leafwikiTempDir()},
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

	ginkgo.It("validates descriptor health and manager cleanup behavior", ginkgo.Label("integration"), func() {
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
			wikid.WorkspaceRecord{ID: newFixtureWorkspaceID("workspace-a"), DataDir: "bad\x00data", RootDir: leafwikiTempDir()},
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
		_, err = writeFailManager.Ensure(context.Background(), wikid.WorkspaceRecord{ID: newFixtureWorkspaceID("workspace-b"), DataDir: leafwikiTempDir(), RootDir: leafwikiTempDir()})
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
			wikid.WorkspaceRecord{ID: newFixtureWorkspaceID("workspace-c"), DataDir: leafwikiTempDir(), RootDir: leafwikiTempDir()},
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
			wikid.WorkspaceRecord{ID: newFixtureWorkspaceID("workspace-d"), DataDir: leafwikiTempDir(), RootDir: leafwikiTempDir()},
			leafwikiRuntimeConfig{Workspace: wiki.Workspace{DataDir: leafwikiTempDir(), RootDir: leafwikiTempDir()}},
			internalRuntimeRoleReady{Role: projectdaemon.RoleWorkspaced, PID: os.Getpid(), URL: "http://workspace.local"},
		)
		Expect(err).To(MatchError(descriptorWriteErr))

		writeDescriptorAtomicForRuntime = previousWriteDescriptor
		removeTargetManager := newFederatedWorkspaceManager(leafwikiRuntimeConfig{}, "daemon-token", "http://wikid.local", wikid.Layout{RuntimeDir: blockingFile}, wikid.NewWorkspaceSupervisor(wikid.WorkspaceSupervisorOptions{}))
		err = removeTargetManager.writeWorkspaceDescriptor(
			wikid.WorkspaceRecord{ID: newFixtureWorkspaceID("workspace-e"), DataDir: leafwikiTempDir(), RootDir: leafwikiTempDir()},
			leafwikiRuntimeConfig{Workspace: wiki.Workspace{DataDir: leafwikiTempDir(), RootDir: leafwikiTempDir()}},
			internalRuntimeRoleReady{Role: projectdaemon.RoleWorkspaced, PID: os.Getpid(), URL: "http://workspace.local"},
		)
		Expect(err).To(MatchPathError())
	})
})
