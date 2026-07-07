package main

import (
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/perber/wiki/internal/projectdaemon"
	"github.com/perber/wiki/internal/wikid"
)

var _ = ginkgo.Describe("leafwiki main process", func() {
	ginkgo.It("wikidfrontd runtime writes role descriptor", ginkgo.Label("e2e"), func() {
		var ownerPID int
		ginkgo.DeferCleanup(func() {
			terminateProjectDaemonProcess(ownerPID)
		})
		baseDir := leafwikiTempDir()
		dataDir := filepath.Join(baseDir, "data")
		rootDir := filepath.Join(baseDir, "content")
		port := freeTCPPort()
		proc := startLeafwikiHelper([]string{
			"--disable-auth",
			"--data-dir", dataDir,
			"--root-dir", rootDir,
			"--host", "127.0.0.1",
			"--port", port,
			"--log-target", "stderr",
		}, map[string]string{})

		workspaceDesc := waitForProjectDaemonDescriptor(dataDir)
		waitForLeafwikiReady(proc, port)
		layout := leafwikiHelperGlobalLayoutForDataDir(dataDir)
		globalDesc := waitForGlobalWikidDescriptor(dataDir)
		ownerPID = globalDesc.PID
		Expect(globalDesc).To(SatisfyAll(
			HaveField("RuntimeStack", Equal(projectdaemon.RuntimeStackWikidFrontd)),
			HaveField("Role", Equal(projectdaemon.RoleWikid)),
			HaveField("WorkspaceID", Equal(wikid.HomeWorkspaceID)),
			HaveField("PrivateMCPURL", Not(BeEmpty())),
			HaveField("PrivateMCPToken", Not(BeEmpty())),
		), fmt.Sprintf("global descriptor runtime metadata = %#v, want wikid home descriptor", globalDesc))
		Expect(globalDesc.Config).To(SatisfyAll(
			HaveField("WorkspaceID", Equal(wikid.HomeWorkspaceID)),
			HaveField("DataDir", Equal(layout.HomeDir)),
			HaveField("RootDir", Equal(layout.HomeRootDir)),
		), fmt.Sprintf("global descriptor config = %#v, want home workspace config", globalDesc.Config))
		Expect(workspaceDesc).To(SatisfyAll(
			HaveField("Role", Equal(projectdaemon.RoleWorkspaced)),
			HaveField("WorkspaceID", Not(BeEmpty())),
			HaveField("WorkspaceID", Not(Equal(wikid.HomeWorkspaceID))),
		), fmt.Sprintf("workspace descriptor role/workspace = %q/%q, want non-home workspaced", workspaceDesc.Role, workspaceDesc.WorkspaceID))
		Expect(workspaceDesc.Config.Port).To(Equal("0"), fmt.Sprintf("workspace descriptor config port = %q, want ephemeral port 0", workspaceDesc.Config.Port))
		Expect(workspaceDesc.PrivateMCPURL).To(SatisfyAll(
			Not(BeEmpty()),
			Not(ContainSubstring(":0/")),
		), fmt.Sprintf("workspace descriptor private MCP URL = %q, want actual listener URL", workspaceDesc.PrivateMCPURL))

		gotRoles := map[projectdaemon.RoleName]projectdaemon.RoleHealth{}
		for _, role := range globalDesc.Roles {
			gotRoles[role.Name] = role
		}
		for _, name := range []projectdaemon.RoleName{projectdaemon.RoleWikid, projectdaemon.RoleFrontd, projectdaemon.RoleWorkspaced} {
			Expect(gotRoles[name].State).To(Equal(projectdaemon.RoleStateReady), fmt.Sprintf("role %s state = %q, want ready; all roles = %#v", name, gotRoles[name].State, globalDesc.Roles))
			Expect(gotRoles[name].PID).To(BeNumerically(">", 0), fmt.Sprintf("role %s PID = %d, want live role process; all roles = %#v", name, gotRoles[name].PID, globalDesc.Roles))
			Expect(classifyProcessExists(gotRoles[name].PID)).To(Equal(processRunning), fmt.Sprintf("role %s PID %d is not running; all roles = %#v", name, gotRoles[name].PID, globalDesc.Roles))

		}
		Expect(gotRoles[projectdaemon.RoleWikid].PID).To(Equal(globalDesc.PID), fmt.Sprintf("wikid PID = %d, want descriptor owner PID %d", gotRoles[projectdaemon.RoleWikid].PID, globalDesc.PID))
		Expect(gotRoles[projectdaemon.RoleFrontd].PID).NotTo(Equal(globalDesc.PID), fmt.Sprintf("frontd PID must differ from wikid descriptor PID; descriptor PID = %d roles = %#v", globalDesc.PID, globalDesc.Roles))
		Expect(gotRoles[projectdaemon.RoleWorkspaced].PID).NotTo(Equal(globalDesc.PID), fmt.Sprintf("workspaced PID must differ from wikid descriptor PID; descriptor PID = %d roles = %#v", globalDesc.PID, globalDesc.Roles))
		Expect(gotRoles[projectdaemon.RoleFrontd].PID).NotTo(Equal(gotRoles[projectdaemon.RoleWorkspaced].PID), fmt.Sprintf("frontd and workspaced PIDs must differ; roles = %#v", globalDesc.Roles))

		proc.stop()

	})
})

var _ = ginkgo.Describe("leafwiki main process", func() {
	ginkgo.It("wikidfrontd runtime lists home workspace", ginkgo.Label("e2e"), func() {
		var ownerPID int
		ginkgo.DeferCleanup(func() {
			terminateProjectDaemonProcess(ownerPID)
		})
		baseDir := leafwikiTempDir()
		dataDir := filepath.Join(baseDir, "data")
		rootDir := filepath.Join(baseDir, "content")
		port := freeTCPPort()
		proc := startLeafwikiHelper([]string{
			"--disable-auth",
			"--data-dir", dataDir,
			"--root-dir", rootDir,
			"--host", "127.0.0.1",
			"--port", port,
			"--log-target", "stderr",
		}, map[string]string{})

		desc := waitForProjectDaemonDescriptor(dataDir)
		ownerPID = desc.PID
		waitForLeafwikiReady(proc, port)

		resp, err := http.Get("http://127.0.0.1:" + port + "/api/workspaces")
		Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("GET /api/workspaces: %v", err))

		defer func() { _ = resp.Body.Close() }()
		Expect(resp).To(HaveHTTPStatus(http.StatusOK))
		body, err := io.ReadAll(resp.Body)
		Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("read /api/workspaces: %v", err))

		var out wikid.WorkspaceListResponse
		Expect(json.Unmarshal(body, &out)).To(Succeed(), fmt.Sprintf("decode /api/workspaces: %v", err))
		layout := leafwikiHelperGlobalLayoutForDataDir(dataDir)
		for _, leaked := range []string{"dataDir", "rootDir", layout.HomeDir, layout.HomeRootDir, dataDir, rootDir} {
			Expect(string(body)).NotTo(ContainSubstring(leaked), fmt.Sprintf("/api/workspaces leaked workspace path field %q: %s", leaked, body))

		}
		Expect(out.Workspaces).To(HaveLen(2), fmt.Sprintf("workspace list = %#v, want home plus first-contact workspace", out.Workspaces))
		Expect(out.Workspaces).To(ContainElement(HaveField("ID", Equal(wikid.HomeWorkspaceID))))
		Expect(out.Workspaces).To(ContainElement(HaveField("ID", Not(Equal(wikid.HomeWorkspaceID)))))

		proc.stop()

	})
})

var _ = ginkgo.Describe("leafwiki main process", func() {
	ginkgo.It("wikidfrontd runtime proxies workspace API by ID", ginkgo.Label("e2e"), func() {
		var ownerPID int
		ginkgo.DeferCleanup(func() {
			terminateProjectDaemonProcess(ownerPID)
		})
		baseDir := leafwikiTempDir()
		dataDir := filepath.Join(baseDir, "data")
		rootDir := filepath.Join(baseDir, "content")
		port := freeTCPPort()
		proc := startLeafwikiHelper([]string{
			"--disable-auth",
			"--data-dir", dataDir,
			"--root-dir", rootDir,
			"--host", "127.0.0.1",
			"--port", port,
			"--log-target", "stderr",
		}, map[string]string{})

		desc := waitForProjectDaemonDescriptor(dataDir)
		ownerPID = desc.PID
		waitForLeafwikiReady(proc, port)

		resp, err := http.Get("http://127.0.0.1:" + port + "/api/workspaces/home/tree")
		Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("GET /api/workspaces/home/tree: %v", err))

		defer func() { _ = resp.Body.Close() }()
		Expect(resp).To(HaveHTTPStatus(http.StatusOK))
		var tree map[string]any
		Expect(json.NewDecoder(resp.Body).Decode(&tree)).To(Succeed(), fmt.Sprintf("decode tree: %v", err))
		Expect(tree).To(HaveKey("children"))

		proc.stop()

	})
})

var _ = ginkgo.Describe("leafwiki main process", func() {
	ginkgo.It("wikidfrontd runtime ensures registered workspace by ID", ginkgo.Label("e2e"), func() {
		var ownerPID int
		ginkgo.DeferCleanup(func() {
			terminateProjectDaemonProcess(ownerPID)
		})
		baseDir := leafwikiTempDir()
		dataDir := filepath.Join(baseDir, "data")
		rootDir := filepath.Join(baseDir, "content")
		secondDataDir := filepath.Join(baseDir, "second-data")
		secondRootDir := filepath.Join(baseDir, "second-root")
		Expect(os.MkdirAll(secondRootDir, 0o755)).To(Succeed())
		Expect(os.WriteFile(filepath.Join(secondRootDir, "index.md"), []byte("# Second\n"), 0o644)).To(Succeed())
		port := freeTCPPort()
		proc := startLeafwikiHelper([]string{
			"--disable-auth",
			"--data-dir", dataDir,
			"--root-dir", rootDir,
			"--host", "127.0.0.1",
			"--port", port,
			"--log-target", "stderr",
		}, map[string]string{})

		_ = waitForProjectDaemonDescriptor(dataDir)
		waitForLeafwikiReady(proc, port)
		layout := leafwikiHelperGlobalLayoutForDataDir(dataDir)
		globalDesc, err := projectdaemon.ReadTrustedDescriptor(projectdaemon.GlobalDescriptorPath(layout.RuntimeDir, projectdaemon.RoleWikid))
		Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("read global wikid descriptor: %v", err))

		ownerPID = globalDesc.PID
		registry := wikid.NewRegistryService(wikid.NewRegistryStore(layout.DBPath), layout)
		second, err := registry.RegisterWorkspace(wikid.RegisterWorkspaceRequest{
			DisplayName: "Second",
			DataDir:     secondDataDir,
			RootDir:     secondRootDir,
		})
		Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("register second workspace: %v", err))

		grants := wikid.NewGrantStore(layout.DBPath)
		Expect(grants.Upsert(wikid.Grant{Subject: "user:public-editor", WorkspaceID: second.ID, Role: wikid.GrantRoleEditor})).To(Succeed(), fmt.Sprintf("grant second workspace: %v", err))

		resp, err := http.Get("http://127.0.0.1:" + port + "/api/workspaces/" + second.ID.URLPathSegment() + "/tree")
		Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("GET second workspace tree: %v", err))

		defer func() { _ = resp.Body.Close() }()
		Expect(resp).To(HaveHTTPStatus(http.StatusOK))
		var tree map[string]any
		Expect(json.NewDecoder(resp.Body).Decode(&tree)).To(Succeed(), fmt.Sprintf("decode second tree: %v", err))
		query := url.Values{}
		query.Set("path", "")
		query.Set("kind", "section")
		pageResp, err := http.Get("http://127.0.0.1:" + port + "/api/workspaces/" + second.ID.URLPathSegment() + "/pages/by-path?" + query.Encode())
		Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("GET second workspace root page: %v", err))

		defer func() { _ = pageResp.Body.Close() }()
		Expect(pageResp).To(HaveHTTPStatus(http.StatusOK))
		var page struct {
			Content string `json:"content"`
			Kind    string `json:"kind"`
		}
		Expect(json.NewDecoder(pageResp.Body).Decode(&page)).To(Succeed(), fmt.Sprintf("decode second root page: %v", err))
		Expect(page).To(SatisfyAll(
			HaveField("Kind", Equal("section")),
			HaveField("Content", ContainSubstring("Second")),
		), fmt.Sprintf("second root page = %#v, want section containing Second", page))

		secondDesc := waitForProjectDaemonDescriptor(secondDataDir)
		Expect(secondDesc).To(SatisfyAll(
			HaveField("Role", Equal(projectdaemon.RoleWorkspaced)),
			HaveField("WorkspaceID", Equal(second.ID)),
			HaveField("PrivateMCPURL", Not(BeEmpty())),
			HaveField("PrivateMCPToken", Not(BeEmpty())),
		), fmt.Sprintf("second descriptor role/workspace/private MCP fields = %#v, want workspaced/%q with private MCP", secondDesc, second.ID))

		proc.stop()

	})
})

var _ = ginkgo.Describe("federated STDIO attach", func() {
	ginkgo.It("rejects descriptor for different registered workspace", ginkgo.Label("integration"), func() {
		baseDir := leafwikiTempDir()
		leafwikiSetenv("HOME", filepath.Join(baseDir, "home"))
		dataDir := filepath.Join(baseDir, "beta-data")
		rootDir := filepath.Join(baseDir, "beta-root")
		Expect(os.MkdirAll(dataDir, 0o755)).To(Succeed())
		Expect(os.MkdirAll(rootDir, 0o755)).To(Succeed())

		cfg := testRuntimeConfig(dataDir, rootDir, "0", mcpTransports{Stdio: true}, true)
		cfg.RuntimeStack = projectdaemon.RuntimeStackWikidFrontd
		requestCfg, err := daemonWorkspaceRequestConfigForRuntime(cfg)
		Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("workspace request config: %v", err))

		globalCfg, err := daemonRequestConfigForRuntime(cfg)
		Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("global request config: %v", err))

		layout := wikid.GlobalLayout(globalCfg.DataDir)
		registry := wikid.NewRegistryService(wikid.NewRegistryStore(layout.DBPath), layout)
		registered, err := registry.RegisterWorkspace(wikid.RegisterWorkspaceRequest{
			DisplayName: "Beta",
			DataDir:     requestCfg.DataDir,
			RootDir:     requestCfg.RootDir,
		})
		Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("register beta workspace: %v", err))
		Expect(registered.ID).NotTo(Equal(newFixtureWorkspaceID("alpha")), fmt.Sprintf("registered workspace ID unexpectedly matched stale descriptor ID"))

		const privateToken = "private-token"
		privateMCP := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			if req.Header.Get(projectdaemon.ControlTokenHeader) != privateToken {
				http.Error(w, "bad token", http.StatusUnauthorized)
				return
			}
			w.WriteHeader(http.StatusOK)
		}))
		defer privateMCP.Close()

		alphaCfg := requestCfg
		alphaCfg.WorkspaceID = newFixtureWorkspaceID("alpha")
		configHash, err := projectdaemon.ConfigHash(alphaCfg)
		Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("config hash: %v", err))

		descriptorPath := projectdaemon.DescriptorPath(requestCfg.DataDir)
		Expect(projectdaemon.WriteDescriptorAtomic(descriptorPath, &projectdaemon.Descriptor{
			SchemaVersion:   projectdaemon.DescriptorSchemaVersion,
			RuntimeStack:    projectdaemon.RuntimeStackWikidFrontd,
			Role:            projectdaemon.RoleWorkspaced,
			WorkspaceID:     newFixtureWorkspaceID("alpha"),
			PID:             os.Getpid(),
			StartedAt:       time.Now().UTC(),
			DataDir:         requestCfg.DataDir,
			RootDir:         requestCfg.RootDir,
			PrivateMCPURL:   privateMCP.URL,
			PrivateMCPToken: privateToken,
			ConfigHash:      configHash,
			Config:          alphaCfg,
		})).To(Succeed())

		_, err = attachOrStartFederatedProjectDaemon(context.Background(), cfg, requestCfg, descriptorPath)
		Expect(err).To(MatchProjectDaemonWorkspaceIDMismatch(newFixtureWorkspaceID("alpha"), registered.ID))

	})
})

var _ = ginkgo.Describe("federated workspace manager", func() {
	ginkgo.It("ensure single flights concurrent startup", ginkgo.Label("integration"), func() {
		supervisor := wikid.NewWorkspaceSupervisor(wikid.WorkspaceSupervisorOptions{})
		manager := newFederatedWorkspaceManager(
			leafwikiRuntimeConfig{},
			"daemon-token",
			"http://127.0.0.1:1",
			wikid.GlobalLayout(leafwikiTempDir()),
			supervisor,
		)
		manager.writeDescriptor = func(wikid.WorkspaceRecord, leafwikiRuntimeConfig, internalRuntimeRoleReady) error {
			return nil
		}

		started := make(chan struct{}, 1)
		releaseStart := make(chan struct{})
		processDone := make(chan error)
		var startMu sync.Mutex
		startCount := 0
		manager.startRole = func(internalRuntimeRoleStartupConfig) (*internalRuntimeRoleProcess, internalRuntimeRoleReady, error) {
			startMu.Lock()
			startCount++
			startMu.Unlock()
			started <- struct{}{}
			select {
			case <-releaseStart:
			case <-time.After(2 * time.Second):
				return nil, internalRuntimeRoleReady{}, context.DeadlineExceeded
			}
			return testRuntimeRoleProcess(projectdaemon.RoleWorkspaced, 101, processDone), internalRuntimeRoleReady{
				Role: projectdaemon.RoleWorkspaced,
				PID:  101,
				URL:  "http://127.0.0.1:41001",
			}, nil
		}

		workspace := wikid.WorkspaceRecord{
			ID:      newFixtureWorkspaceID("alpha"),
			DataDir: leafwikiTempDir(),
			RootDir: leafwikiTempDir(),
		}
		var wg sync.WaitGroup
		results := make(chan wikid.WorkspaceStatus, 2)
		errs := make(chan error, 2)
		for i := 0; i < 2; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				status, err := manager.Ensure(context.Background(), workspace)
				results <- status
				errs <- err
			}()
		}
		Eventually(started).WithTimeout(2 * time.Second).Should(Receive())
		startMu.Lock()
		Expect(startCount).To(Equal(1), fmt.Sprintf("start count while first startup is running = %d, want 1", startCount))

		startMu.Unlock()
		close(releaseStart)
		wg.Wait()
		close(results)
		close(errs)

		for err := range errs {
			Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("Ensure returned error: %v", err))

		}
		var statuses []wikid.WorkspaceStatus
		for status := range results {
			statuses = append(statuses, status)
		}
		Expect(statuses).To(ConsistOf(
			SatisfyAll(HaveField("State", Equal(wikid.WorkspaceStateRunning)), HaveField("PID", Equal(101))),
			SatisfyAll(HaveField("State", Equal(wikid.WorkspaceStateRunning)), HaveField("PID", Equal(101))),
		))

		startMu.Lock()
		defer startMu.Unlock()
		Expect(startCount).To(Equal(1), fmt.Sprintf("start count after concurrent ensure = %d, want 1", startCount))

	})
})

var _ = ginkgo.Describe("federated workspace manager", func() {
	ginkgo.It("ensure canceled duplicate waiter returns context error", ginkgo.Label("integration"), func() {
		supervisor := wikid.NewWorkspaceSupervisor(wikid.WorkspaceSupervisorOptions{})
		manager := newFederatedWorkspaceManager(
			leafwikiRuntimeConfig{},
			"daemon-token",
			"http://127.0.0.1:1",
			wikid.GlobalLayout(leafwikiTempDir()),
			supervisor,
		)
		manager.writeDescriptor = func(wikid.WorkspaceRecord, leafwikiRuntimeConfig, internalRuntimeRoleReady) error {
			return nil
		}

		started := make(chan struct{}, 1)
		releaseStart := make(chan struct{})
		processDone := make(chan error)
		var startMu sync.Mutex
		startCount := 0
		manager.startRole = func(internalRuntimeRoleStartupConfig) (*internalRuntimeRoleProcess, internalRuntimeRoleReady, error) {
			startMu.Lock()
			startCount++
			startMu.Unlock()
			started <- struct{}{}
			select {
			case <-releaseStart:
			case <-time.After(2 * time.Second):
				return nil, internalRuntimeRoleReady{}, context.DeadlineExceeded
			}
			return testRuntimeRoleProcess(projectdaemon.RoleWorkspaced, 101, processDone), internalRuntimeRoleReady{
				Role: projectdaemon.RoleWorkspaced,
				PID:  101,
				URL:  "http://127.0.0.1:41001",
			}, nil
		}

		workspace := wikid.WorkspaceRecord{ID: newFixtureWorkspaceID("alpha"), DataDir: leafwikiTempDir(), RootDir: leafwikiTempDir()}
		firstDone := make(chan error, 1)
		go func() {
			_, err := manager.Ensure(context.Background(), workspace)
			firstDone <- err
		}()
		Eventually(started).WithTimeout(2 * time.Second).Should(Receive())

		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		_, err := manager.Ensure(ctx, workspace)
		Expect(err).To(MatchError(context.Canceled))

		startMu.Lock()
		Expect(startCount).To(Equal(1), fmt.Sprintf("start count after canceled duplicate waiter = %d, want 1", startCount))

		startMu.Unlock()

		close(releaseStart)
		Eventually(firstDone).WithTimeout(2 * time.Second).Should(Receive(Succeed()))
		startMu.Lock()
		defer startMu.Unlock()
		Expect(startCount).To(Equal(1), fmt.Sprintf("start count after first Ensure finished = %d, want 1", startCount))

	})
})
