package main

import (
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"

	coreauth "github.com/perber/wiki/internal/core/auth"
	"github.com/perber/wiki/internal/localization"
	"github.com/perber/wiki/internal/wikid"
)

var _ = ginkgo.Describe("leafwiki main process", func() {
	ginkgo.It("config agent hook rejects unknown flag without fail open", ginkgo.Label("e2e"), func() {
		configPath := filepath.Join(leafwikiTempDir(), "leafwiki.yml")
		writeTestConfig(configPath, "data-dir: ./data\n")
		payload := `{"hook_event_name":"SessionStart","session_id":"unknown-config-flag-secret"}`

		stdout, stderr, err := runLeafwikiHelperWithInputAndTimeout([]string{
			"--config", configPath,
			"--not-a-real-flag",
			"agent-hook", "codex",
		}, nil, payload, 5*time.Second)
		Expect(err).To(MatchProcessExitError(), fmt.Sprintf("config mixed with unknown flag unexpectedly succeeded\nstdout:\n%s\nstderr:\n%s", stdout, stderr))
		Expect(stdout).NotTo(MatchCodexAgentHookAllowResponse())

	})
})

var _ = ginkgo.Describe("leafwiki main process", func() {
	ginkgo.It("agent hook fail open uses config", ginkgo.Label("e2e"), func() {
		baseDir := leafwikiTempDir()
		sameDir := filepath.Join(baseDir, "same")
		configPath := filepath.Join(baseDir, "leafwiki.yml")
		writeTestConfig(configPath, fmt.Sprintf(`disable-auth: true
data-dir: %s
root-dir: %s
log-target: stderr
`, sameDir, sameDir))
		payload := `{"hook_event_name":"SessionStart","session_id":"config-hook-secret"}`

		stdout, stderr, err := runLeafwikiHelperWithInputAndTimeout([]string{
			"--config", configPath,
			"agent-hook", "codex",
		}, nil, payload, 5*time.Second)
		Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("agent-hook invalid config should fail open, got %v\nstdout:\n%s\nstderr:\n%s", err, stdout, stderr))
		Expect(stdout).To(MatchCodexAgentHookAllowResponse())

	})
})

var _ = ginkgo.Describe("leafwiki main process", func() {
	ginkgo.It("reset admin password uses config data dir", ginkgo.Label("e2e"), func() {
		baseDir := leafwikiTempDir()
		dataDir := filepath.Join(baseDir, "data")
		initWikidAdminUser(dataDir)
		configPath := filepath.Join(baseDir, "leafwiki.yml")
		writeTestConfig(configPath, fmt.Sprintf("data-dir: %s\n", dataDir))

		_, stderr, err := runLeafwikiHelper([]string{
			"--config", configPath,
			"reset-admin-password",
		}, map[string]string{})
		Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("reset-admin-password process error = %v, stderr=%q", err, stderr))
		paths := wikid.AuthStoragePaths(dataDir)
		store, err := coreauth.NewUserStore(paths.AuthDir)
		Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("open wikid user store: %v", err))
		ginkgo.DeferCleanup(func() {
			Expect(store.Close()).To(Succeed())
		})
		service := coreauth.NewUserService(store)
		_, err = service.GetUserByEmailOrUsernameAndPassword(coreauth.DefaultAdminUsername, "old-password")
		Expect(err).To(MatchError(coreauth.ErrUserInvalidCredentials))

	})
})

var _ = ginkgo.Describe("leafwiki main process", func() {
	ginkgo.It("config path does not affect daemon identity", ginkgo.Label("e2e"), func() {
		stdinReader, stdinWriter := io.Pipe()
		defer closeBestEffort(stdinWriter)
		baseDir := leafwikiTempDir()
		dataDir := filepath.Join(baseDir, "data")
		rootDir := filepath.Join(baseDir, "content")
		port := freeTCPPort()
		configBody := fmt.Sprintf(`mcp: stdio
disable-auth: true
data-dir: %s
root-dir: %s
host: 127.0.0.1
port: %s
log-target: stderr
`, dataDir, rootDir, port)
		firstConfig := filepath.Join(baseDir, "first.yml")
		secondConfig := filepath.Join(baseDir, "second.yml")
		writeTestConfig(firstConfig, configBody)
		writeTestConfig(secondConfig, configBody)
		env := map[string]string{"HOME": filepath.Join(baseDir, "home")}
		first := startLeafwikiHelperWithStdin([]string{"--config", firstConfig}, env, stdinReader)
		waitForLeafwikiReady(first, port)

		stdout, stderr, err := runLeafwikiHelperWithTimeout([]string{"--config", secondConfig}, env, 5*time.Second)
		Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("second config path should attach and exit cleanly, got %v\nstdout:\n%s\nstderr:\n%s", err, stdout, stderr))
		Expect(stdout).To(BeEmpty(), fmt.Sprintf("stdout = %q, want empty without MCP frames", stdout))

		Expect(stdinWriter.Close()).To(Succeed())
		first.waitForExit()

	})
})

var _ = ginkgo.Describe("leafwiki main process", func() {
	ginkgo.It("rejects explicit log file for stream target", ginkgo.Label("e2e"), func() {
		stdout, stderr, err := runLeafwikiHelper([]string{
			"--disable-auth",
			"--data-dir", filepath.Join(leafwikiTempDir(), "data"),
			"--log-target", "stderr",
			"--log-file", "custom.log",
		}, nil)
		Expect(err).To(MatchProcessExitError(), fmt.Sprintf("expected --log-file with stderr target to exit non-zero"))
		Expect(stdout).To(BeEmpty(), fmt.Sprintf("stdout = %q, want empty", stdout))
		Expect(readJSONLogEntriesFromText(stderr)).To(ContainElement(haveJSONLogEntry(localization.MessageIDCLIErrorInvalidLoggingConfig)), fmt.Sprintf("stderr = %q, want invalid logging config log entry", stderr))

	})
})

var _ = ginkgo.Describe("leafwiki main process", func() {
	ginkgo.It("native STDIO auth enabled requires API key", ginkgo.Label("e2e"), func() {
		dataDir := filepath.Join(leafwikiTempDir(), "data")
		stdout, stderr, err := runLeafwikiHelper([]string{
			"--mcp=stdio",
			"--data-dir", dataDir,
			"--jwt-secret", "test-secret",
			"--admin-password", "admin-password",
		}, nil)
		Expect(err).To(MatchProcessExitError(), fmt.Sprintf("expected native stdio with auth enabled to exit non-zero"))
		Expect(stdout).To(BeEmpty(), fmt.Sprintf("stdout = %q, want empty", stdout))
		Expect(readJSONLogEntriesFromText(stderr)).To(ContainElement(haveJSONLogEntry(localization.MessageIDCLIErrorInvalidMCPConfig)))

	})
})

var _ = ginkgo.Describe("leafwiki main process", func() {
	ginkgo.It("native STDIO rejects invalid API key without leaking secret", ginkgo.Label("e2e"), func() {
		secret := "lwk_secret_bad"
		stdout, stderr, err := runLeafwikiHelper([]string{
			"--mcp=stdio",
			"--api-key", secret,
			"--data-dir", filepath.Join(leafwikiTempDir(), "data"),
			"--jwt-secret", "test-secret",
			"--admin-password", "admin-password",
			"--log-target", "stderr",
		}, nil)
		Expect(err).To(MatchProcessExitError(), fmt.Sprintf("expected native stdio with invalid API key to exit non-zero"))
		Expect(stdout).To(BeEmpty(), fmt.Sprintf("stdout = %q, want empty", stdout))
		Expect(readJSONLogEntriesFromText(stderr)).To(ContainElement(haveJSONLogEntry(localization.MessageIDCLIErrorLeafWikiStartupFailed)))

	})
})

var _ = ginkgo.Describe("leafwiki main process", func() {
	ginkgo.It("native STDIO rejects stdout logging", ginkgo.Label("e2e"), func() {
		stdout, stderr, err := runLeafwikiHelper([]string{
			"--mcp=stdio",
			"--disable-auth",
			"--data-dir", filepath.Join(leafwikiTempDir(), "data"),
			"--log-target", "stdout",
		}, nil)
		Expect(err).To(MatchProcessExitError(), fmt.Sprintf("expected native stdio with stdout logging to exit non-zero"))
		Expect(stdout).To(BeEmpty(), fmt.Sprintf("stdout = %q, want empty", stdout))
		Expect(readJSONLogEntriesFromText(stderr)).To(ContainElement(haveJSONLogEntry(localization.MessageIDCLIErrorInvalidMCPConfig)))

	})
})

var _ = ginkgo.Describe("leafwiki main process", func() {
	ginkgo.It("public HTTPMCP non loopback host starts web and keeps local MCP", ginkgo.Label("e2e"), func() {
		var ownerPID int
		baseDir := leafwikiTempDir()
		ginkgo.DeferCleanup(func() {
			terminateProjectDaemonProcess(ownerPID)
		})
		dataDir := filepath.Join(baseDir, "data")
		rootDir := filepath.Join(baseDir, "content")
		port := freeTCPPort()
		proc := startLeafwikiHelper([]string{
			"--mcp=http",
			"--disable-auth",
			"--host", "0.0.0.0",
			"--port", port,
			"--data-dir", dataDir,
			"--root-dir", rootDir,
			"--log-target", "stderr",
		}, nil)

		waitForLeafwikiReady(proc, port)
		globalDesc := waitForGlobalWikidDescriptor(dataDir)
		ownerPID = globalDesc.PID
		toolNames := listProcessHTTPMCPToolNames("http://127.0.0.1:" + port + "/mcp/workspaces/home")
		Expect(toolNames).To(matchToolNames(federatedRuntimeToolNames()))
		proc.stop()
		terminateProjectDaemonProcess(ownerPID)
		waitForLeafwikiUnavailable(port)
		waitForProjectLocksReusable(dataDir, rootDir, 15*time.Second)

	})
})

var _ = ginkgo.Describe("leafwiki main process", func() {
	ginkgo.It("native STDIO rejects positional command with stderr only", ginkgo.Label("e2e"), func() {
		stdout, stderr, err := runLeafwikiHelper([]string{
			"--mcp=stdio",
			"bogus",
		}, nil)
		Expect(err).To(MatchProcessExitError(), fmt.Sprintf("expected native stdio with a positional command to exit non-zero"))
		Expect(stdout).To(BeEmpty(), fmt.Sprintf("stdout = %q, want empty", stdout))
		Expect(readJSONLogEntriesFromText(stderr)).To(ContainElement(haveJSONLogEntry(localization.MessageIDCLIErrorInvalidNativeSTDIOConfig)))

	})
})

var _ = ginkgo.Describe("leafwiki main process", func() {
	ginkgo.It("native STDIO environment rejects positional command with stderr only", ginkgo.Label("e2e"), func() {
		stdout, stderr, err := runLeafwikiHelper([]string{
			"--disable-auth",
			"bogus",
		}, map[string]string{
			"LEAFWIKI_MCP": "stdio",
		})
		Expect(err).To(MatchProcessExitError(), fmt.Sprintf("expected env-enabled native stdio with a positional command to exit non-zero"))
		Expect(stdout).To(BeEmpty(), fmt.Sprintf("stdout = %q, want empty", stdout))
		Expect(readJSONLogEntriesFromText(stderr)).To(ContainElement(haveJSONLogEntry(localization.MessageIDCLIErrorInvalidNativeSTDIOConfig)))

	})
})

var _ = ginkgo.Describe("leafwiki main process", func() {
	ginkgo.It("native STDIO starts HTTP and stdin close stops server", ginkgo.Label("e2e"), func() {
		stdinReader, stdinWriter := io.Pipe()
		dataDir := filepath.Join(leafwikiTempDir(), "data")
		rootDir := filepath.Join(leafwikiTempDir(), "content")
		port := freeTCPPort()
		proc := startLeafwikiHelperWithStdin([]string{
			"--mcp=stdio",
			"--disable-auth",
			"--data-dir", dataDir,
			"--root-dir", rootDir,
			"--host", "127.0.0.1",
			"--port", port,
			"--log-target", "stderr",
		}, nil, stdinReader)

		waitForLeafwikiReady(proc, port)
		Expect(stdinWriter.Close()).To(Succeed())
		proc.waitForExit()

		Expect(readFileString(proc.stdoutPath)).To(BeEmpty())
		waitForLeafwikiUnavailable(port)

	})
})

var _ = ginkgo.Describe("leafwiki main process", func() {
	ginkgo.It("native STDIO only keeps HTTPMCP route disabled", ginkgo.Label("e2e"), func() {
		stdinReader, stdinWriter := io.Pipe()
		defer closeBestEffort(stdinWriter)
		port := freeTCPPort()
		proc := startLeafwikiHelperWithStdin([]string{
			"--mcp=stdio",
			"--disable-auth",
			"--data-dir", filepath.Join(leafwikiTempDir(), "data"),
			"--root-dir", filepath.Join(leafwikiTempDir(), "content"),
			"--host", "127.0.0.1",
			"--port", port,
			"--log-target", "stderr",
		}, nil, stdinReader)

		waitForLeafwikiReady(proc, port)
		resp, err := http.Get("http://127.0.0.1:" + port + "/mcp")
		Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("GET /mcp: %v", err))

		defer func() { _ = resp.Body.Close() }()
		Expect(resp).To(HaveHTTPStatus(http.StatusNotFound))

		Expect(stdinWriter.Close()).To(Succeed())
		proc.waitForExit()
		Expect(readFileString(proc.stdoutPath)).To(BeEmpty())

	})
})

var _ = ginkgo.Describe("leafwiki main process", func() {
	ginkgo.It("native STDIO second compatible startup attaches to project daemon", ginkgo.Label("e2e"), func() {
		stdinReader, stdinWriter := io.Pipe()
		defer closeBestEffort(stdinWriter)
		baseDir := leafwikiTempDir()
		dataDir := filepath.Join(baseDir, "data")
		rootDir := filepath.Join(baseDir, "content")
		port := freeTCPPort()
		first := startLeafwikiHelperWithStdin([]string{
			"--mcp=stdio",
			"--disable-auth",
			"--data-dir", dataDir,
			"--root-dir", rootDir,
			"--host", "127.0.0.1",
			"--port", port,
			"--log-target", "stderr",
		}, nil, stdinReader)
		waitForLeafwikiReady(first, port)
		_, err := io.WriteString(stdinWriter, nativeStdioListToolsInput())
		Expect(err).NotTo(HaveOccurred())
		waitForFileContaining(first.stdoutPath, `"id":2`)

		stdout, stderr, err := runLeafwikiHelperWithTimeout([]string{
			"--mcp=stdio",
			"--disable-auth",
			"--data-dir", dataDir,
			"--root-dir", rootDir,
			"--host", "127.0.0.1",
			"--port", port,
			"--log-target", "stderr",
		}, nil, 5*time.Second)
		Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("second compatible startup should attach and exit cleanly, got %v\nstdout:\n%s\nstderr:\n%s", err, stdout, stderr))
		Expect(stdout).To(BeEmpty(), fmt.Sprintf("stdout = %q, want empty without MCP frames", stdout))

		Expect(stderr).To(BeEmpty(), fmt.Sprintf("stderr = %q, want no ownership failure", stderr))
		waitForLeafwikiReady(first, port)

	})
})

var _ = ginkgo.Describe("leafwiki main process", func() {
	ginkgo.It("native STDIO only startup attaches to HTTP enabled project daemon", ginkgo.Label("e2e"), func() {
		baseDir := leafwikiTempDir()
		dataDir := filepath.Join(baseDir, "data")
		rootDir := filepath.Join(baseDir, "content")
		port := freeTCPPort()
		first := startLeafwikiHelper([]string{
			"--mcp=http",
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
			"--log-target", "file",
			"--disable-request-log",
		}, nil, 12*time.Second)
		Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("stdio-only startup should attach to HTTP-enabled daemon, got %v\nstdout:\n%s\nstderr:\n%s", err, stdout, stderr))
		Expect(stdout).To(BeEmpty(), fmt.Sprintf("stdout = %q, want empty without MCP frames", stdout))
		Expect(stderr).To(BeEmpty(), fmt.Sprintf("stderr = %q, want no public MCP, logging, or request-log config mismatch", stderr))

		toolNames := listProcessHTTPMCPToolNames("http://127.0.0.1:" + port + "/mcp/workspaces/home")
		Expect(toolNames).To(matchToolNames(federatedRuntimeToolNames()))

	})
})

var _ = ginkgo.Describe("leafwiki main process", func() {
	ginkgo.It("native STDIO owner stderr logging falls back to file", ginkgo.Label("e2e"), func() {
		stdinReader, stdinWriter := io.Pipe()
		defer closeBestEffort(stdinWriter)
		baseDir := leafwikiTempDir()
		dataDir := filepath.Join(baseDir, "data")
		rootDir := filepath.Join(baseDir, "content")
		port := freeTCPPort()
		proc := startLeafwikiHelperWithStdin([]string{
			"--mcp=stdio",
			"--disable-auth",
			"--data-dir", dataDir,
			"--root-dir", rootDir,
			"--host", "127.0.0.1",
			"--port", port,
			"--log-target", "stderr",
			"--daemon-idle-timeout", "0",
		}, nil, stdinReader)
		waitForLeafwikiReady(proc, port)

		logPath := filepath.Join(dataDir, ".leafwiki", "logs", "leafwiki.log")
		waitForFileContaining(logPath, "Starting LeafWiki")

		Expect(stdinWriter.Close()).To(Succeed())
		proc.waitForExit()
		Expect(readFileString(proc.stdoutPath)).To(BeEmpty())

	})
})

var _ = ginkgo.Describe("project daemon owner spawn", func() {
	ginkgo.It("removes secret startup config on executable failure", ginkgo.Label("e2e"), func() {
		oldExecutable := projectDaemonExecutable
		ginkgo.DeferCleanup(func() {
			projectDaemonExecutable = oldExecutable
		})

		baseDir := leafwikiTempDir()
		dataDir := filepath.Join(baseDir, "data")
		rootDir := filepath.Join(baseDir, "content")
		jwtSecret := fmt.Sprintf("cleanup-jwt-secret-%d", time.Now().UnixNano())
		adminPassword := "cleanup-admin-password"
		var startupPath string
		executableErr := errors.New("forced executable failure")
		projectDaemonExecutable = func() (string, error) {
			startupPath = findLeafwikiDaemonStartupConfigContaining(jwtSecret)
			Expect(startupPath).NotTo(BeEmpty(), fmt.Sprintf("startup config containing secret marker was not visible before executable lookup"))

			Expect(startupPath).To(haveFileMode(0o600))
			return "", executableErr
		}

		cfg := testRuntimeConfig(dataDir, rootDir, freeTCPPort(), mcpTransports{}, false)
		cfg.JWTSecret = jwtSecret
		cfg.AdminPassword = adminPassword
		_, err := spawnProjectDaemonOwner(cfg)

		Expect(err).To(MatchError(executableErr))
		_, err = os.Stat(startupPath)
		Expect(err).To(MatchError(os.ErrNotExist))

	})
})
