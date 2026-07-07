package main

import (
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/perber/wiki/internal/projectdaemon"
	"github.com/perber/wiki/internal/wikid"
	"github.com/perber/wiki/internal/workspaceid"
)

var _ = ginkgo.Describe("federated workspace manager", func() {
	ginkgo.It("ensure failure does not poison retry", ginkgo.Label("integration"), func() {
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

		processDone := make(chan error)
		var startMu sync.Mutex
		startCount := 0
		firstStartErr := errors.New("workspace startup failed")
		manager.startRole = func(internalRuntimeRoleStartupConfig) (*internalRuntimeRoleProcess, internalRuntimeRoleReady, error) {
			startMu.Lock()
			defer startMu.Unlock()
			startCount++
			if startCount == 1 {
				return nil, internalRuntimeRoleReady{}, firstStartErr
			}
			return testRuntimeRoleProcess(projectdaemon.RoleWorkspaced, 202, processDone), internalRuntimeRoleReady{
				Role: projectdaemon.RoleWorkspaced,
				PID:  202,
				URL:  "http://127.0.0.1:41002",
			}, nil
		}

		workspace := wikid.WorkspaceRecord{ID: newFixtureWorkspaceID("alpha"), DataDir: leafwikiTempDir(), RootDir: leafwikiTempDir()}
		_, err := manager.Ensure(context.Background(), workspace)
		Expect(err).To(MatchError(firstStartErr))
		status, err := manager.Ensure(context.Background(), workspace)
		Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("retry Ensure returned error: %v", err))
		Expect(status).To(SatisfyAll(
			HaveField("State", Equal(wikid.WorkspaceStateRunning)),
			HaveField("PID", Equal(202)),
		), fmt.Sprintf("retry status = %#v, want running pid 202", status))

		startMu.Lock()
		defer startMu.Unlock()
		Expect(startCount).To(Equal(2), fmt.Sprintf("start count after retry = %d, want 2", startCount))

	})
})

var _ = ginkgo.Describe("federated workspace manager", func() {
	ginkgo.It("ensure does not serialize different workspaces", ginkgo.Label("integration"), func() {
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

		started := make(chan workspaceid.WorkspaceID, 2)
		releaseStart := make(chan struct{})
		processDone := make(chan error)
		manager.startRole = func(startup internalRuntimeRoleStartupConfig) (*internalRuntimeRoleProcess, internalRuntimeRoleReady, error) {
			workspaceID := startup.Runtime.Workspace.ID
			started <- workspaceID
			select {
			case <-releaseStart:
			case <-time.After(2 * time.Second):
				return nil, internalRuntimeRoleReady{}, context.DeadlineExceeded
			}
			pid := 101
			url := "http://127.0.0.1:41001"
			if workspaceID == "beta" {
				pid = 202
				url = "http://127.0.0.1:41002"
			}
			return testRuntimeRoleProcess(projectdaemon.RoleWorkspaced, pid, processDone), internalRuntimeRoleReady{
				Role: projectdaemon.RoleWorkspaced,
				PID:  pid,
				URL:  url,
			}, nil
		}

		workspaces := []wikid.WorkspaceRecord{
			{ID: newFixtureWorkspaceID("alpha"), DataDir: leafwikiTempDir(), RootDir: leafwikiTempDir()},
			{ID: newFixtureWorkspaceID("beta"), DataDir: leafwikiTempDir(), RootDir: leafwikiTempDir()},
		}
		var wg sync.WaitGroup
		errs := make(chan error, len(workspaces))
		for _, workspace := range workspaces {
			workspace := workspace
			wg.Add(1)
			go func() {
				defer wg.Done()
				_, err := manager.Ensure(context.Background(), workspace)
				errs <- err
			}()
		}

		seen := map[workspaceid.WorkspaceID]bool{}
		Eventually(func(g Gomega) {
			for len(seen) < 2 {
				var workspaceID workspaceid.WorkspaceID
				g.Expect(started).To(Receive(&workspaceID))
				seen[workspaceID] = true
			}
		}).WithTimeout(2 * time.Second).Should(Succeed())
		Expect(seen).To(SatisfyAll(
			HaveKey(newFixtureWorkspaceID("alpha")),
			HaveKey(newFixtureWorkspaceID("beta")),
		), fmt.Sprintf("started workspaces = %#v, want alpha and beta", seen))

		close(releaseStart)
		wg.Wait()
		close(errs)
		for err := range errs {
			Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("Ensure returned error: %v", err))

		}

	})
})

var _ = ginkgo.Describe("federated workspace manager", func() {
	ginkgo.It("starts workspaced with ephemeral port", ginkgo.Label("integration"), func() {
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
		processDone := make(chan error)
		manager.startRole = func(startup internalRuntimeRoleStartupConfig) (*internalRuntimeRoleProcess, internalRuntimeRoleReady, error) {
			Expect(startup.Runtime.Port).To(Equal("0"), fmt.Sprintf("workspaced startup port = %q, want 0", startup.Runtime.Port))

			return testRuntimeRoleProcess(projectdaemon.RoleWorkspaced, 101, processDone), internalRuntimeRoleReady{
				Role: projectdaemon.RoleWorkspaced,
				PID:  101,
				URL:  "http://127.0.0.1:49152",
			}, nil
		}

		status, err := manager.Ensure(context.Background(), wikid.WorkspaceRecord{
			ID:      newFixtureWorkspaceID("alpha"),
			DataDir: leafwikiTempDir(),
			RootDir: leafwikiTempDir(),
		})
		Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("Ensure returned error: %v", err))
		Expect(status).To(SatisfyAll(
			HaveField("State", Equal(wikid.WorkspaceStateRunning)),
			HaveField("URL", Equal("http://127.0.0.1:49152")),
		), fmt.Sprintf("status = %#v, want running actual ready URL", status))

	})
})

var _ = ginkgo.Describe("federated workspace manager", func() {
	ginkgo.It("removes stale descriptors and restarts after crash", ginkgo.Label("integration"), func() {
		supervisor := wikid.NewWorkspaceSupervisor(wikid.WorkspaceSupervisorOptions{
			MaxRestarts: 1,
			Backoff:     time.Millisecond,
		})
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

		removed := make(chan string, 2)
		manager.removeDescriptor = func(path string) error {
			removed <- path
			return nil
		}
		restarted := make(chan struct{}, 1)
		restartedProcessDone := make(chan error)
		manager.startRole = func(internalRuntimeRoleStartupConfig) (*internalRuntimeRoleProcess, internalRuntimeRoleReady, error) {
			restarted <- struct{}{}
			return testRuntimeRoleProcess(projectdaemon.RoleWorkspaced, 202, restartedProcessDone), internalRuntimeRoleReady{
				Role: projectdaemon.RoleWorkspaced,
				PID:  202,
				URL:  "http://127.0.0.1:41002",
			}, nil
		}

		workspace := wikid.WorkspaceRecord{ID: newFixtureWorkspaceID("alpha"), DataDir: leafwikiTempDir(), RootDir: leafwikiTempDir()}
		processDone := make(chan error, 1)
		process := testRuntimeRoleProcess(projectdaemon.RoleWorkspaced, 101, processDone)
		manager.mu.Lock()
		manager.processes[workspace.ID] = process
		manager.workspaces[workspace.ID] = workspace
		manager.descriptors[workspace.ID] = []string{"/tmp/alpha-local.json", "/tmp/alpha-runtime.json"}
		manager.mu.Unlock()
		supervisor.MarkReady(workspace.ID, 101, "http://127.0.0.1:41001")

		go manager.monitorWorkspaceProcess(workspace.ID, process)
		processDone <- errors.New("exit status 2")

		seenRemoved := map[string]bool{}
		var removedPath string
		Eventually(removed).WithTimeout(2 * time.Second).Should(Receive(&removedPath))
		seenRemoved[removedPath] = true
		Eventually(removed).WithTimeout(2 * time.Second).Should(Receive(&removedPath))
		seenRemoved[removedPath] = true
		Expect(seenRemoved).To(HaveKey("/tmp/alpha-local.json"))
		Expect(seenRemoved).To(HaveKey("/tmp/alpha-runtime.json"))

		Eventually(restarted).WithTimeout(2 * time.Second).Should(Receive())
		Eventually(func() wikid.WorkspaceStatus {
			return supervisor.Status(workspace.ID)
		}).WithTimeout(2 * time.Second).Should(SatisfyAll(
			HaveField("State", Equal(wikid.WorkspaceStateRunning)),
			HaveField("PID", Equal(202)),
		))
		manager.mu.Lock()
		Expect(manager.descriptors).NotTo(HaveKey(workspace.ID), fmt.Sprintf("stale descriptors still tracked after crash"))
		manager.mu.Unlock()

	})
})

func testRuntimeRoleProcess(role projectdaemon.RoleName, pid int, done <-chan error) *internalRuntimeRoleProcess {
	return &internalRuntimeRoleProcess{
		role:     role,
		pid:      pid,
		done:     done,
		waitDone: make(chan struct{}),
	}
}

var _ = ginkgo.Describe("internal runtime role readiness timeout", func() {
	ginkgo.It("is thirty seconds", ginkgo.Label("unit"), func() {
		Expect(internalRuntimeRoleReadinessTimeout).To(Equal(30*time.Second), fmt.Sprintf("internal runtime role readiness timeout = %v, want 30s", internalRuntimeRoleReadinessTimeout))

	})
})

var _ = ginkgo.Describe("federated workspace ensure timeout", func() {
	ginkgo.It("is thirty seconds", ginkgo.Label("unit"), func() {
		Expect(federatedWorkspaceEnsureTimeout).To(Equal(30*time.Second), fmt.Sprintf("federated workspace ensure timeout = %v, want 30s", federatedWorkspaceEnsureTimeout))

	})
})

var _ = ginkgo.Describe("internal runtime role process", func() {
	ginkgo.It("stops child when ready role mismatches", ginkgo.Label("e2e"), func() {
		pidPath := filepath.Join(leafwikiTempDir(), "wrong-role.pid")
		leafwikiSetenv("GO_WANT_LEAFWIKI_HELPER_PROCESS", "1")
		leafwikiSetenv("LEAFWIKI_TEST_RUNTIME_READY_WRONG_ROLE", "1")
		leafwikiSetenv("LEAFWIKI_TEST_RUNTIME_READY_WRONG_ROLE_PID_PATH", pidPath)

		proc, ready, err := startInternalRuntimeRoleProcess(internalRuntimeRoleStartupConfig{
			Role:        projectdaemon.RoleWorkspaced,
			Runtime:     leafwikiRuntimeConfig{},
			DaemonToken: "daemon-token",
		})
		Expect(err).NotTo(MatchError(errRuntimeRoleInvalidPID))
		Expect(proc).To(BeNil(), fmt.Sprintf("startInternalRuntimeRoleProcess returned process %#v for wrong ready role", proc))
		Expect(ready).To(Equal(internalRuntimeRoleReady{}), fmt.Sprintf("startInternalRuntimeRoleProcess returned ready state %#v for wrong ready role", ready))
		raw, readErr := os.ReadFile(pidPath)
		Expect(readErr).NotTo(HaveOccurred())

		pid, parseErr := strconv.Atoi(strings.TrimSpace(string(raw)))
		Expect(parseErr).NotTo(HaveOccurred())

		if processExists(pid) {
			_ = syscall.Kill(pid, syscall.SIGKILL)
		}
		Expect(classifyProcessExists(pid)).To(Equal(processNotRunning))

	})
})

var _ = ginkgo.Describe("leafwiki main process", func() {
	ginkgo.It("wikidfrontd runtime restarts workspaced and updates descriptor", ginkgo.Label("e2e"), func() {
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
		initial, err := runtimeRoleHealthResult(desc.Roles, projectdaemon.RoleWorkspaced)
		Expect(err).To(Succeed())
		Expect(initial.PID).To(BeNumerically(">", 0), fmt.Sprintf("initial workspaced role = %#v, want child PID", initial))

		Expect(syscall.Kill(initial.PID, syscall.SIGTERM)).To(Succeed())

		descriptorPath := projectdaemon.DescriptorPath(dataDir)
		var restarted projectdaemon.RoleHealth
		waitForRuntimeCondition(10*time.Second, func() bool {
			raw, err := os.ReadFile(descriptorPath)
			if err != nil {
				return false
			}
			var current projectdaemon.Descriptor
			if err := json.Unmarshal(raw, &current); err != nil {
				return false
			}
			var ok bool
			restarted, ok = findRoleHealth(current.Roles, projectdaemon.RoleWorkspaced)
			return ok && restarted.State == projectdaemon.RoleStateReady && restarted.PID > 0 && restarted.PID != initial.PID && processExists(restarted.PID)
		})

		proc.stop()

	})
})

var _ = ginkgo.Describe("leafwiki main process", func() {
	ginkgo.It("wikidfrontd runtime uses fresh wikid auth stores", ginkgo.Label("e2e"), func() {
		var ownerPID int
		ginkgo.DeferCleanup(func() {
			terminateProjectDaemonProcess(ownerPID)
		})
		baseDir := leafwikiTempDir()
		dataDir := filepath.Join(baseDir, "data")
		rootDir := filepath.Join(baseDir, "content")
		port := freeTCPPort()
		proc := startLeafwikiHelper([]string{
			"--jwt-secret", "auth-store-secret",
			"--admin-password", "admin-pass",
			"--allow-insecure",
			"--data-dir", dataDir,
			"--root-dir", rootDir,
			"--host", "127.0.0.1",
			"--port", port,
			"--log-target", "stderr",
		}, map[string]string{})

		layout := leafwikiHelperGlobalLayoutForDataDir(dataDir)
		globalDesc := waitForGlobalWikidDescriptor(dataDir)
		ownerPID = globalDesc.PID
		waitForLeafwikiReady(proc, port)

		authDir := filepath.Join(layout.WikidDir, "auth")
		for _, path := range []string{
			filepath.Join(authDir, "users.db"),
			filepath.Join(authDir, "sessions.db"),
			filepath.Join(authDir, "api_keys.db"),
		} {
			_, err := os.Stat(path)
			Expect(err).NotTo(HaveOccurred())
		}
		for _, name := range []string{"users.db", "sessions.db", "api_keys.db"} {
			_, err := os.Stat(filepath.Join(dataDir, name))
			Expect(err).To(MatchError(os.ErrNotExist))
		}

		client := &http.Client{Timeout: 2 * time.Second}
		httpReq, err := http.NewRequest(http.MethodPost, "http://127.0.0.1:"+port+"/api/auth/login", strings.NewReader(`{"identifier":"admin","password":"admin-pass"}`))
		Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("build login request: %v", err))

		httpReq.Header.Set("Content-Type", "application/json")
		resp, err := client.Do(httpReq)
		Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("POST /api/auth/login: %v", err))

		defer func() { _ = resp.Body.Close() }()
		Expect(resp).To(HaveHTTPStatus(http.StatusOK))
		proc.stop()

	})
})

var _ = ginkgo.Describe("leafwiki main process", func() {
	ginkgo.It("project daemon descriptor includes workspace sync flag", ginkgo.Label("e2e"), func() {
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
		}, nil)
		desc := waitForProjectDaemonDescriptor(dataDir)
		ownerPID = desc.PID
		waitForLeafwikiReady(proc, port)
		Expect(desc.Config.EnableWorkspaceSync).To(BeTrue(), fmt.Sprintf("descriptor config EnableWorkspaceSync = false, want true"))

		proc.stop()

	})
})

var _ = ginkgo.Describe("leafwiki main process", func() {
	ginkgo.It("removed revision and workspace sync flags fail unknown", ginkgo.Label("e2e"), func() {
		baseDir := leafwikiTempDir()
		dataDir := filepath.Join(baseDir, "data")
		rootDir := filepath.Join(baseDir, "content")
		for _, removedFlag := range []string{"--enable-revision", "--enable-workspace-sync"} {
			func() {
				_ = removedFlag
				stdout, stderr, err := runLeafwikiHelperWithTimeout([]string{
					"--disable-auth",
					removedFlag,
					"--data-dir", dataDir,
					"--root-dir", rootDir,
					"--host", "127.0.0.1",
					"--port", freeTCPPort(),
					"--log-target", "stderr",
				}, nil, 5*time.Second)
				Expect(err).To(MatchProcessExitError(), fmt.Sprintf("startup with removed flag unexpectedly succeeded\nstdout:\n%s\nstderr:\n%s", stdout, stderr))
				Expect(err).NotTo(MatchError(context.DeadlineExceeded), fmt.Sprintf("startup with removed flag hung; expected immediate unknown flag error\nstdout:\n%s\nstderr:\n%s", stdout, stderr))
				Expect(stdout).To(BeEmpty(), fmt.Sprintf("stdout = %q, want empty for removed flag %s", stdout, removedFlag))

			}()
		}

	})
})
