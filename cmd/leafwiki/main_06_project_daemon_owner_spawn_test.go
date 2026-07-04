package main

import (
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/perber/wiki/internal/agenthooks"
	"github.com/perber/wiki/internal/locking"
	"github.com/perber/wiki/internal/projectdaemon"
	"github.com/perber/wiki/internal/wiki"
	"github.com/perber/wiki/internal/wikid"
)

var _ = ginkgo.Describe("project daemon owner spawn", func() {
	ginkgo.It("eventually removes secret startup config when child exits before read", ginkgo.Label("e2e"), func() {
		oldExecutable := projectDaemonExecutable
		oldCleanupDelay := projectDaemonStartupConfigPostStartCleanupDelay
		ginkgo.DeferCleanup(func() {
			projectDaemonExecutable = oldExecutable
			projectDaemonStartupConfigPostStartCleanupDelay = oldCleanupDelay
		})

		truePath, err := exec.LookPath("true")
		if err != nil {
			ginkgo.Skip(fmt.Sprint("true executable not available"))
		}
		baseDir := leafwikiTempDir()
		dataDir := filepath.Join(baseDir, "data")
		rootDir := filepath.Join(baseDir, "content")
		jwtSecret := fmt.Sprintf("post-start-cleanup-jwt-%d", time.Now().UnixNano())
		var startupPath string
		projectDaemonExecutable = func() (string, error) {
			startupPath = findLeafwikiDaemonStartupConfigContaining(jwtSecret)
			Expect(startupPath).NotTo(BeEmpty(), fmt.Sprintf("startup config containing secret marker was not visible before child start"))

			Expect(startupPath).To(haveFileMode(0o600))
			return truePath, nil
		}
		projectDaemonStartupConfigPostStartCleanupDelay = 25 * time.Millisecond

		cfg := testRuntimeConfig(dataDir, rootDir, freeTCPPort(), mcpTransports{}, false)
		cfg.JWTSecret = jwtSecret
		cfg.AdminPassword = "post-start-cleanup-admin"
		_, err = spawnProjectDaemonOwner(cfg)
		Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("spawnProjectDaemonOwner failed: %v", err))

		waitForFileRemoved(startupPath, 2*time.Second)

	})
})

var _ = ginkgo.Describe("leafwiki main process", func() {
	ginkgo.It("allows native tool attachment without owner bootstrap secrets", ginkgo.Label("e2e"), func() {
		baseDir := leafwikiTempDir()
		dataDir := filepath.Join(baseDir, "data")
		rootDir := filepath.Join(baseDir, "content")
		apiKey := createWikidMCPAPIKeyWithUser(dataDir)
		port := freeTCPPort()
		first := startLeafwikiHelper([]string{
			"--mcp=http",
			"--data-dir", dataDir,
			"--root-dir", rootDir,
			"--host", "127.0.0.1",
			"--port", port,
			"--jwt-secret", "owner-jwt-secret",
			"--admin-password", "owner-admin-password",
			"--allow-insecure",
			"--log-target", "stderr",
		}, map[string]string{})
		waitForLeafwikiReady(first, port)
		grantWikidWorkspaceAccessForDirs(dataDir, rootDir, apiKey.UserID, wikid.GrantRoleEditor)

		stdout, stderr, err := runLeafwikiHelperWithTimeout([]string{
			"--mcp=stdio",
			"--data-dir", dataDir,
			"--root-dir", rootDir,
			"--host", "127.0.0.1",
			"--port", port,
			"--allow-insecure",
			"--log-target", "stderr",
		}, map[string]string{
			"LEAFWIKI_MCP_API_KEY": apiKey.Secret,
		}, 5*time.Second)
		Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("stdio API-key startup should attach without owner bootstrap secrets, got %v\nstdout:\n%s\nstderr:\n%s", err, stdout, stderr))
		Expect(stdout).To(BeEmpty(), fmt.Sprintf("stdout = %q, want empty without MCP frames", stdout))

		Expect(stderr).To(BeEmpty(), fmt.Sprintf("stderr = %q, want no bootstrap-secret attach failure", stderr))

	})
})

var _ = ginkgo.Describe("leafwiki main process", func() {
	ginkgo.It("stale descriptor is replaced without sending API key", ginkgo.Label("e2e"), func() {
		baseDir := leafwikiTempDir()
		dataDir := filepath.Join(baseDir, "data")
		rootDir := filepath.Join(baseDir, "content")
		Expect(os.MkdirAll(dataDir, 0o755)).To(Succeed())
		Expect(os.MkdirAll(rootDir, 0o755)).To(Succeed())
		received := make(chan string, 4)
		staleControl := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			raw, _ := io.ReadAll(req.Body)
			select {
			case received <- req.URL.Path + " " + req.Header.Get("Authorization") + " " + string(raw):
			default:
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"ok":true}`)
		}))
		ginkgo.DeferCleanup(staleControl.Close)

		apiKey := createWikidMCPAPIKey(dataDir)
		port := freeTCPPort()
		runtimeCfg := testRuntimeConfig(dataDir, rootDir, port, mcpTransports{Stdio: true}, false)
		runtimeCfg.RuntimeStack = projectdaemon.RuntimeStackWikidFrontd
		runtimeCfg.JWTSecret = "owner-jwt-secret"
		runtimeCfg.AdminPassword = "owner-admin-password"
		layout := leafwikiHelperGlobalLayoutForDataDir(dataDir)
		ownerRuntimeCfg := runtimeCfg
		ownerRuntimeCfg.Workspace = wiki.Workspace{ID: wikid.HomeWorkspaceID, DataDir: layout.HomeDir, RootDir: layout.HomeRootDir}
		ownerCfg, err := daemonConfigForRuntime(ownerRuntimeCfg)
		Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("daemonConfigForRuntime: %v", err))

		hash, err := projectdaemon.ConfigHash(ownerCfg)
		Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("ConfigHash: %v", err))

		descriptorPath := projectdaemon.GlobalDescriptorPath(layout.RuntimeDir, projectdaemon.RoleWikid)
		Expect(projectdaemon.WriteDescriptorAtomic(descriptorPath, &projectdaemon.Descriptor{
			SchemaVersion:    projectdaemon.DescriptorSchemaVersion,
			PID:              os.Getpid(),
			StartedAt:        time.Now().UTC(),
			DataDir:          ownerCfg.DataDir,
			RootDir:          ownerCfg.RootDir,
			PublicURL:        "http://127.0.0.1:" + port,
			PublicMCPEnabled: false,
			ControlURL:       staleControl.URL,
			ConfigHash:       hash,
			IdleTimeout:      "0s",
			ControlToken:     "stale-token",
			Config:           ownerCfg,
		})).To(Succeed())

		stdinReader, stdinWriter := io.Pipe()
		defer closeBestEffort(stdinWriter)
		proc := startLeafwikiHelperWithStdin([]string{
			"--mcp=stdio",
			"--data-dir", dataDir,
			"--root-dir", rootDir,
			"--host", "127.0.0.1",
			"--port", port,
			"--allow-insecure",
			"--jwt-secret", "owner-jwt-secret",
			"--admin-password", "owner-admin-password",
			"--log-target", "stderr",
		}, map[string]string{
			"LEAFWIKI_MCP_API_KEY": apiKey,
		}, stdinReader)
		waitForLeafwikiReady(proc, port)

		_ = waitForGlobalWikidDescriptor(dataDir)
		replaced := readFileString(descriptorPath)
		replacedDesc, err := projectdaemon.ReadTrustedDescriptor(descriptorPath)
		Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("read replaced descriptor: %v", err))
		Expect(replacedDesc).To(SatisfyAll(
			Not(HaveField("ControlURL", Equal(staleControl.URL))),
			Not(HaveField("ControlToken", Equal("stale-token"))),
		), fmt.Sprintf("descriptor was not replaced:\n%s", replaced))

		Consistently(received).WithTimeout(25 * time.Millisecond).ShouldNot(Receive())
		Expect(readJSONLogEntriesFromText(readFileString(proc.stdoutPath))).NotTo(ContainElement(HaveKey("api_key")))
		Expect(readJSONLogEntriesFromText(readFileString(proc.stderrPath))).NotTo(ContainElement(HaveKey("api_key")))
		Expect(stdinWriter.Close()).To(Succeed(), fmt.Sprintf("close stdin writer: %v", err))
		proc.waitForExit()

	})
})

var _ = ginkgo.Describe("leafwiki main process", func() {
	ginkgo.It("untrusted stale descriptor is replaced when locks are free", ginkgo.Label("e2e"), func() {
		tests := []struct {
			name  string
			setup func(path string)
		}{
			{
				name: "corrupt json",
				setup: func(path string) {
					ginkgo.GinkgoHelper()
					Expect(os.WriteFile(path, []byte("{"), 0o600)).To(Succeed())
				},
			},
			{
				name: "wrong mode",
				setup: func(path string) {
					ginkgo.GinkgoHelper()
					Expect(os.WriteFile(path, []byte("{}"), 0o644)).To(Succeed())
				},
			},
			{
				name: "non regular path",
				setup: func(path string) {
					ginkgo.GinkgoHelper()
					Expect(os.Mkdir(path, 0o700)).To(Succeed())
				},
			},
		}

		for _, tt := range tests {
			func() {
				_ = tt.name
				baseDir := leafwikiTempDir()
				dataDir := filepath.Join(baseDir, "data")
				rootDir := filepath.Join(baseDir, "content")
				descriptorPath := filepath.Join(dataDir, ".leafwiki", projectdaemon.DescriptorFileName)
				Expect(os.MkdirAll(filepath.Dir(descriptorPath), 0o755)).To(Succeed())
				Expect(os.MkdirAll(rootDir, 0o755)).To(Succeed())
				tt.setup(descriptorPath)

				port := freeTCPPort()
				proc := startLeafwikiHelper([]string{
					"--disable-auth",
					"--data-dir", dataDir,
					"--root-dir", rootDir,
					"--host", "127.0.0.1",
					"--port", port,
					"--log-target", "stderr",
				}, nil)
				waitForLeafwikiReady(proc, port)

				desc := waitForProjectDaemonDescriptor(dataDir)
				Expect(desc).To(SatisfyAll(
					HaveField("PID", BeNumerically(">", 0)),
					HaveField("ControlURL", Not(BeEmpty())),
				), fmt.Sprintf("replacement descriptor = %#v, want live daemon descriptor", desc))

				info, err := os.Stat(descriptorPath)
				Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("stat replacement descriptor: %v", err))

				Expect(info.Mode().Perm()).To(Equal(os.FileMode(0o600)))

			}()
		}

	})
})

var _ = ginkgo.Describe("project daemon descriptor trust", func() {
	ginkgo.It("preserves untrusted descriptor when any project lock is held", ginkgo.Label("integration"), func() {
		tests := []struct {
			name string
			lock func(dataDir string, rootDir string) func()
		}{
			{
				name: "data lock held",
				lock: func(dataDir string, _ string) func() {
					ginkgo.GinkgoHelper()
					lock, err := locking.AcquireDataDirLock(dataDir)
					Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("acquire data lock: %v", err))

					return func() { _ = lock.Release() }
				},
			},
			{
				name: "root lock held",
				lock: func(_ string, rootDir string) func() {
					ginkgo.GinkgoHelper()
					lock, err := locking.AcquireRootDirLock(rootDir)
					Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("acquire root lock: %v", err))

					return func() { _ = lock.Release() }
				},
			},
		}

		for _, tt := range tests {
			func() {
				_ = tt.name
				baseDir := leafwikiTempDir()
				dataDir := filepath.Join(baseDir, "data")
				rootDir := filepath.Join(baseDir, "content")
				Expect(os.MkdirAll(filepath.Join(dataDir, ".leafwiki"), 0o755)).To(Succeed())
				Expect(os.MkdirAll(rootDir, 0o755)).To(Succeed())
				canonicalData, canonicalRoot, err := projectdaemon.CanonicalizeProject(dataDir, rootDir)
				Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("canonicalize project: %v", err))

				descriptorPath := projectdaemon.DescriptorPath(canonicalData)
				Expect(os.WriteFile(descriptorPath, []byte("{"), 0o600)).To(Succeed(), fmt.Sprintf("write corrupt descriptor: %v", err))
				release := tt.lock(canonicalData, canonicalRoot)
				defer release()

				descriptorRead := readHealthyProjectDaemonLockResult(context.Background(), descriptorPath, projectdaemon.Config{
					DataDir: canonicalData,
					RootDir: canonicalRoot,
				})
				Expect(descriptorRead).To(MatchAbsentProjectDaemonHealth(), fmt.Sprintf("descriptor = %#v, healthy=%t, want absent health for unreadable descriptor", descriptorRead.Descriptor, descriptorRead.Healthy))
				Expect(descriptorRead.Err).To(MatchJSONSyntaxError())

				_, statErr := os.Stat(descriptorPath)
				Expect(statErr).NotTo(HaveOccurred())

			}()
		}

	})
})

var _ = ginkgo.Describe("project daemon descriptor trust", func() {
	ginkgo.It("preserves trusted descriptor when locks held but control unreachable", ginkgo.Label("integration"), func() {
		baseDir := leafwikiTempDir()
		dataDir := filepath.Join(baseDir, "data")
		rootDir := filepath.Join(baseDir, "content")
		Expect(os.MkdirAll(dataDir, 0o755)).To(Succeed())
		Expect(os.MkdirAll(rootDir, 0o755)).To(Succeed())
		cfg := testRuntimeConfig(dataDir, rootDir, freeTCPPort(), mcpTransports{}, true)
		ownerCfg, err := daemonConfigForRuntime(cfg)
		Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("daemon config: %v", err))

		hash, err := projectdaemon.ConfigHash(ownerCfg)
		Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("config hash: %v", err))

		descriptorPath := projectdaemon.DescriptorPath(ownerCfg.DataDir)
		Expect(projectdaemon.WriteDescriptorAtomic(descriptorPath, &projectdaemon.Descriptor{
			SchemaVersion:    projectdaemon.DescriptorSchemaVersion,
			PID:              12345,
			StartedAt:        time.Now(),
			DataDir:          ownerCfg.DataDir,
			RootDir:          ownerCfg.RootDir,
			PublicURL:        "http://127.0.0.1:" + ownerCfg.Port,
			PublicMCPEnabled: ownerCfg.PublicMCPEnabled,
			ControlURL:       "http://127.0.0.1:" + freeTCPPort(),
			ConfigHash:       hash,
			IdleTimeout:      ownerCfg.DaemonIdleTimeout,
			ControlToken:     "control-token",
			Config:           ownerCfg,
		})).To(Succeed())
		dataLock, err := locking.AcquireDataDirLock(ownerCfg.DataDir)
		Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("acquire data lock: %v", err))

		defer releaseRuntimeLockBestEffort(dataLock)
		rootLock, err := locking.AcquireRootDirLock(ownerCfg.RootDir)
		Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("acquire root lock: %v", err))

		defer releaseRuntimeLockBestEffort(rootLock)

		Expect(readHealthyProjectDaemonLockResult(context.Background(), descriptorPath, ownerCfg)).To(MatchProjectDaemonDescriptorReadError(errControlHealthUnreachable))

		_, statErr := os.Stat(descriptorPath)
		Expect(statErr).NotTo(HaveOccurred())

	})
})

var _ = ginkgo.Describe("project daemon descriptor trust", func() {
	ginkgo.It("preserves trusted unsupported schema descriptor when project lock held", ginkgo.Label("integration"), func() {
		baseDir := leafwikiTempDir()
		dataDir := filepath.Join(baseDir, "data")
		rootDir := filepath.Join(baseDir, "content")
		Expect(os.MkdirAll(dataDir, 0o755)).To(Succeed())
		Expect(os.MkdirAll(rootDir, 0o755)).To(Succeed())
		cfg := testRuntimeConfig(dataDir, rootDir, freeTCPPort(), mcpTransports{}, true)
		ownerCfg, err := daemonConfigForRuntime(cfg)
		Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("daemon config: %v", err))

		hash, err := projectdaemon.ConfigHash(ownerCfg)
		Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("config hash: %v", err))

		descriptorPath := projectdaemon.DescriptorPath(ownerCfg.DataDir)
		Expect(projectdaemon.WriteDescriptorAtomic(descriptorPath, &projectdaemon.Descriptor{
			SchemaVersion:    projectdaemon.DescriptorSchemaVersion + 1,
			PID:              12345,
			StartedAt:        time.Now(),
			DataDir:          ownerCfg.DataDir,
			RootDir:          ownerCfg.RootDir,
			PublicURL:        "http://127.0.0.1:" + ownerCfg.Port,
			PublicMCPEnabled: ownerCfg.PublicMCPEnabled,
			ControlURL:       "http://127.0.0.1:" + freeTCPPort(),
			ConfigHash:       hash,
			IdleTimeout:      ownerCfg.DaemonIdleTimeout,
			ControlToken:     "control-token",
			Config:           ownerCfg,
		})).To(Succeed())
		dataLock, err := locking.AcquireDataDirLock(ownerCfg.DataDir)
		Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("acquire data lock: %v", err))

		defer releaseRuntimeLockBestEffort(dataLock)

		Expect(readHealthyProjectDaemonLockResult(context.Background(), descriptorPath, ownerCfg)).To(MatchProjectDaemonDescriptorReadError(projectdaemon.ErrDescriptorSchemaMismatch))

		_, statErr := os.Stat(descriptorPath)
		Expect(statErr).NotTo(HaveOccurred())

	})
})

var _ = ginkgo.Describe("leafwiki main process", func() {
	ginkgo.It("plain web owner supports later private STDIO attach", ginkgo.Label("e2e"), func() {
		baseDir := leafwikiTempDir()
		dataDir := filepath.Join(baseDir, "data")
		rootDir := filepath.Join(baseDir, "content")
		port := freeTCPPort()
		first := startLeafwikiHelper([]string{
			"--disable-auth",
			"--data-dir", dataDir,
			"--root-dir", rootDir,
			"--host", "127.0.0.1",
			"--port", port,
			"--log-target", "stderr",
		}, nil)
		waitForLeafwikiReady(first, port)

		stdout, stderr, err := runLeafwikiHelperWithTimeout([]string{
			"--mcp=stdio",
			"--disable-auth",
			"--data-dir", dataDir,
			"--root-dir", rootDir,
			"--host", "127.0.0.1",
			"--port", port,
			"--log-target", "stderr",
		}, nil, 5*time.Second)
		Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("stdio startup should attach to plain web owner, got %v\nstdout:\n%s\nstderr:\n%s", err, stdout, stderr))

		resp, err := http.Get("http://127.0.0.1:" + port + "/mcp")
		Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("GET /mcp: %v", err))

		defer func() { _ = resp.Body.Close() }()
		Expect(resp).To(HaveHTTPStatus(http.StatusNotFound))

	})
})

var _ = ginkgo.Describe("leafwiki main process", func() {
	ginkgo.It("agent presence control starts owner activity", ginkgo.Label("e2e"), func() {
		baseDir := leafwikiTempDir()
		dataDir := filepath.Join(baseDir, "data")
		rootDir := filepath.Join(baseDir, "content")
		port := freeTCPPort()
		first := startLeafwikiHelper([]string{
			"--disable-auth",
			"--data-dir", dataDir,
			"--root-dir", rootDir,
			"--host", "127.0.0.1",
			"--port", port,
			"--log-target", "stderr",
		}, nil)
		desc := waitForProjectDaemonDescriptor(dataDir)
		client := projectdaemon.NewClient(desc.ControlURL, desc.ControlToken)
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		Expect(client.RecordAgentPresence(ctx, agenthooks.Event{
			Provider:      agenthooks.ProviderCodex,
			SessionIDHash: agentHookSessionHash(agenthooks.ProviderCodex, "codex"),
			EventName:     "SessionStart",
			SeenAt:        time.Now(),
		})).To(Succeed(), fmt.Sprintf("stderr:\n%s", readFileString(first.stderrPath)))
		sessions, err := client.ListAgentPresence(ctx)
		Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("ListAgentPresence failed: %v", err))
		Expect(sessions).To(ContainElement(HaveField("SessionIDHash", Equal(agentHookSessionHash(agenthooks.ProviderCodex, "codex")))))

		waitForLeafwikiReady(first, port)

	})
})
