package main

import (
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"github.com/perber/wiki/internal/localization"
	"github.com/perber/wiki/internal/projectdaemon"
)

var _ = ginkgo.Describe("leafwiki main process", func() {
	ginkgo.It("config endpoint reports workspace sync flag", ginkgo.Label("e2e"), func() {
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
			"--allow-insecure",
			"--data-dir", dataDir,
			"--root-dir", rootDir,
			"--host", "127.0.0.1",
			"--port", port,
			"--log-target", "stderr",
		}, nil)
		desc := waitForProjectDaemonDescriptor(dataDir)
		ownerPID = desc.PID
		waitForLeafwikiReady(proc, port)

		resp, err := http.Get("http://127.0.0.1:" + port + "/api/config")
		Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("GET /api/config: %v", err))

		defer func() { _ = resp.Body.Close() }()
		Expect(resp).To(HaveHTTPStatus(http.StatusOK))
		var config map[string]any
		Expect(json.NewDecoder(resp.Body).Decode(&config)).To(Succeed(), fmt.Sprintf("decode config: %v", err))
		Expect(config).To(HaveKeyWithValue("enableWorkspaceSync", true))

		proc.stop()

	})
})

var _ = ginkgo.Describe("leafwiki main process", func() {
	ginkgo.It("different root dir does not remove live project descriptor", ginkgo.Label("e2e"), func() {
		baseDir := leafwikiTempDir()
		dataDir := filepath.Join(baseDir, "data")
		ownerRootDir := filepath.Join(baseDir, "owner-content")
		requestedRootDir := filepath.Join(baseDir, "requested-content")
		port := freeTCPPort()
		owner := startLeafwikiHelper([]string{
			"--disable-auth",
			"--data-dir", dataDir,
			"--root-dir", ownerRootDir,
			"--host", "127.0.0.1",
			"--port", port,
			"--log-target", "stderr",
		}, map[string]string{})
		defer owner.stop()
		waitForLeafwikiReady(owner, port)
		descriptorPath := projectdaemon.DescriptorPath(dataDir)
		waitForFileContaining(descriptorPath, `"rootDir"`)

		stdout, stderr, err := runLeafwikiHelperWithTimeout([]string{
			"--disable-auth",
			"--data-dir", dataDir,
			"--root-dir", requestedRootDir,
			"--host", "127.0.0.1",
			"--port", port,
			"--log-target", "stderr",
		}, map[string]string{}, 12*time.Second)
		Expect(err).To(MatchProcessExitError(), fmt.Sprintf("different root-dir startup unexpectedly succeeded\nstdout:\n%s\nstderr:\n%s", stdout, stderr))
		Expect(readJSONLogEntriesFromText(stderr)).To(ContainElement(haveJSONLogEntry(string(cliMessageID(localization.MessageIDCLIErrorLeafWikiStartupFailed)))), fmt.Sprintf("stderr = %q, want startup failure log entry", stderr))

		_, err = os.Stat(descriptorPath)
		Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("stdout:\n%s\nstderr:\n%s", stdout, stderr))
		waitForLeafwikiReady(owner, port)

	})
})

var _ = ginkgo.Describe("leafwiki main process", func() {
	ginkgo.It("first startup writes secure project daemon descriptor", ginkgo.Label("e2e"), func() {
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
		}, nil, stdinReader)

		waitForLeafwikiReady(proc, port)
		descriptorPath := filepath.Join(dataDir, ".leafwiki", "project-daemon.json")
		waitForFileContaining(descriptorPath, `"controlToken"`)
		info, err := os.Stat(descriptorPath)
		Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("stat descriptor: %v", err))

		Expect(info.Mode().Perm()).To(Equal(os.FileMode(0o600)))
		desc, err := projectdaemon.ReadTrustedDescriptor(descriptorPath)
		Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("read descriptor: %v", err))
		Expect(desc).To(MatchProjectDaemonDescriptorPathIdentity(dataDir, rootDir), fmt.Sprintf("descriptor = %#v, want canonical data/root dirs", desc))

	})
})

var _ = ginkgo.Describe("leafwiki main process", func() {
	ginkgo.It("canonical path variants attach with default file logging", ginkgo.Label("e2e"), func() {
		if runtime.GOOS == "windows" {
			ginkgo.Skip(fmt.Sprint("symlink path canonicalization test is Unix-oriented"))
		}
		stdinReader, stdinWriter := io.Pipe()
		defer closeBestEffort(stdinWriter)
		baseDir := leafwikiTempDir()
		dataDir := filepath.Join(baseDir, "data")
		rootDir := filepath.Join(baseDir, "content")
		Expect(os.MkdirAll(dataDir, 0o755)).To(Succeed())
		Expect(os.MkdirAll(rootDir, 0o755)).To(Succeed())
		dataLink := filepath.Join(baseDir, "data-link")
		rootLink := filepath.Join(baseDir, "content-link")
		Expect(os.Symlink(dataDir, dataLink)).To(Succeed())
		Expect(os.Symlink(rootDir, rootLink)).To(Succeed())
		port := freeTCPPort()
		first := startLeafwikiHelperWithStdin([]string{
			"--mcp=stdio",
			"--disable-auth",
			"--data-dir", dataLink,
			"--root-dir", rootLink,
			"--host", "127.0.0.1",
			"--port", port,
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
		}, nil, 5*time.Second)
		Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("canonical path variant should attach, got %v\nstdout:\n%s\nstderr:\n%s", err, stdout, stderr))
		Expect(stderr).To(BeEmpty(), fmt.Sprintf("stderr = %q, want quiet canonical path attach", stderr))

	})
})

var _ = ginkgo.Describe("leafwiki main process", func() {
	ginkgo.It("auth enabled project daemon descriptor omits bootstrap secret fingerprints", ginkgo.Label("e2e"), func() {
		baseDir := leafwikiTempDir()
		dataDir := filepath.Join(baseDir, "data")
		rootDir := filepath.Join(baseDir, "content")
		jwtSecret := "descriptor-jwt-secret"
		adminPassword := "descriptor-admin-password"
		port := freeTCPPort()
		proc := startLeafwikiHelper([]string{
			"--data-dir", dataDir,
			"--root-dir", rootDir,
			"--host", "127.0.0.1",
			"--port", port,
			"--jwt-secret", jwtSecret,
			"--admin-password", adminPassword,
			"--allow-insecure",
			"--log-target", "stderr",
		}, nil)

		waitForLeafwikiReady(proc, port)
		layout := leafwikiHelperGlobalLayoutForDataDir(dataDir)
		descriptorPath := projectdaemon.GlobalDescriptorPath(layout.RuntimeDir, projectdaemon.RoleWikid)
		waitForFileContaining(descriptorPath, `"configHash"`)
		raw := readFileString(descriptorPath)
		for _, unexpected := range []string{
			jwtSecret,
			adminPassword,
			sha256Hex(jwtSecret),
			sha256Hex(adminPassword),
			"jwtSecretHash",
			"adminPasswordHash",
		} {
			Expect(raw).NotTo(ContainSubstring(unexpected), fmt.Sprintf("descriptor leaked bootstrap secret material %q:\n%s", unexpected, raw))

		}

	})
})

var _ = ginkgo.Describe("leafwiki main process", func() {
	ginkgo.It("native STDIO close keeps owner alive until idle timeout", ginkgo.Label("e2e"), func() {
		stdinReader, stdinWriter := io.Pipe()
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
		}, map[string]string{"LEAFWIKI_DAEMON_IDLE_TIMEOUT": "1s"}, stdinReader)

		waitForLeafwikiReady(proc, port)
		Expect(stdinWriter.Close()).To(Succeed())
		proc.waitForExit()
		waitForLeafwikiReady(proc, port)
		waitForLeafwikiUnavailable(port)
		waitForProjectLocksReusable(dataDir, rootDir, 15*time.Second)

	})
})

var _ = ginkgo.Describe("leafwiki main process", func() {
	ginkgo.It("crashed native STDIO frontend expires heartbeat and releases locks", ginkgo.Label("e2e"), func() {
		stdinReader, stdinWriter := io.Pipe()
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
		}, map[string]string{"LEAFWIKI_DAEMON_IDLE_TIMEOUT": "1s"}, stdinReader)
		waitForLeafwikiReady(proc, port)

		proc.cancel()
		_ = stdinWriter.Close()
		_ = proc.cmd.Wait()
		proc.stopped = true

		waitForLeafwikiUnavailableWithin(port, 25*time.Second)
		waitForProjectLocksReusable(dataDir, rootDir, 15*time.Second)

	})
})

var _ = ginkgo.Describe("leafwiki main process", func() {
	ginkgo.It("native STDIO piped output exits before owner idle timeout", ginkgo.Label("e2e"), func() {
		baseDir := leafwikiTempDir()
		dataDir := filepath.Join(baseDir, "data")
		rootDir := filepath.Join(baseDir, "content")
		port := freeTCPPort()
		stdout, stderr, err := runLeafwikiHelperWithTimeout([]string{
			"--mcp=stdio",
			"--disable-auth",
			"--data-dir", dataDir,
			"--root-dir", rootDir,
			"--host", "127.0.0.1",
			"--port", port,
			"--log-target", "stderr",
		}, map[string]string{"LEAFWIKI_DAEMON_IDLE_TIMEOUT": "3s"}, 1500*time.Millisecond)
		Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("native STDIO frontend should exit before owner idle timeout, got %v\nstdout:\n%s\nstderr:\n%s", err, stdout, stderr))
		Expect(stdout).To(BeEmpty(), fmt.Sprintf("stdout = %q, want empty without MCP frames", stdout))

		waitForLeafwikiReadyWithDiagnostics(port, stdout, stderr)
		waitForLeafwikiUnavailable(port)

	})
})

var _ = ginkgo.Describe("leafwiki main process", func() {
	ginkgo.It("exposes HTTP tools when native and HTTP transports run together", ginkgo.Label("e2e"), func() {
		stdinReader, stdinWriter := io.Pipe()
		defer closeBestEffort(stdinWriter)
		port := freeTCPPort()
		proc := startLeafwikiHelperWithStdin([]string{
			"--mcp=stdio,http",
			"--disable-auth",
			"--data-dir", filepath.Join(leafwikiTempDir(), "data"),
			"--root-dir", filepath.Join(leafwikiTempDir(), "content"),
			"--host", "127.0.0.1",
			"--port", port,
			"--log-target", "stderr",
		}, nil, stdinReader)

		waitForLeafwikiReady(proc, port)
		toolNames := listProcessHTTPMCPToolNames("http://127.0.0.1:" + port + "/mcp/workspaces/home")
		Expect(toolNames).To(matchToolNames(federatedRuntimeToolNames()))

		Expect(stdinWriter.Close()).To(Succeed())
		proc.waitForExit()
		Expect(readFileString(proc.stdoutPath)).To(BeEmpty())

	})
})

var _ = ginkgo.Describe("leafwiki main process", func() {
	ginkgo.It("reattaches combined transports while keeping logs on stderr", ginkgo.Label("e2e"), func() {
		stdinReader, stdinWriter := io.Pipe()
		defer closeBestEffort(stdinWriter)
		baseDir := leafwikiTempDir()
		dataDir := filepath.Join(baseDir, "data")
		rootDir := filepath.Join(baseDir, "content")
		port := freeTCPPort()
		first := startLeafwikiHelperWithStdin([]string{
			"--mcp=stdio,http",
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

		stdout, stderr, err := runLeafwikiHelperWithInputAndTimeout([]string{
			"--mcp=stdio,http",
			"--disable-auth",
			"--data-dir", dataDir,
			"--root-dir", rootDir,
			"--host", "127.0.0.1",
			"--port", port,
			"--log-target", "stderr",
		}, nil, nativeStdioListToolsInput(), 8*time.Second)
		Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("repeated combined STDIO+HTTP startup should attach, got %v\nstdout:\n%s\nstderr:\n%s", err, stdout, stderr))
		Expect(stdout).To(haveNativeStdioToolListResponse(2, "wiki_create_page"), fmt.Sprintf("stdout = %q, want tools/list response from repeated STDIO attach", stdout))
		Expect(stderr).To(BeEmpty(), fmt.Sprintf("stderr = %q, want quiet combined transport reattach", stderr))

	})
})

var _ = ginkgo.Describe("leafwiki main process", func() {
	ginkgo.It("legacy MCP flags fail unknown", ginkgo.Label("e2e"), func() {
		for _, removedFlag := range []string{"--enable-mcp", "--mcp-stdio"} {
			func() {
				_ = removedFlag
				stdout, stderr, err := runLeafwikiHelperWithTimeout([]string{
					removedFlag,
					"--disable-auth",
					"--data-dir", filepath.Join(leafwikiTempDir(), "data"),
					"--root-dir", filepath.Join(leafwikiTempDir(), "content"),
					"--host", "127.0.0.1",
					"--port", freeTCPPort(),
					"--log-target", "stderr",
				}, nil, 5*time.Second)
				Expect(err).To(MatchProcessExitError(), fmt.Sprintf("startup with removed MCP flag unexpectedly succeeded\nstdout:\n%s\nstderr:\n%s", stdout, stderr))
				Expect(stdout).To(BeEmpty(), fmt.Sprintf("stdout = %q, want empty for removed MCP flag %s", stdout, removedFlag))

			}()
		}

	})
})

var _ = ginkgo.Describe("leafwiki main process", func() {
	ginkgo.It("native STDIO malformed JSON returns parse error and continues", ginkgo.Label("e2e"), func() {
		proc, stdin := startLeafwikiHelperWithStdinPipe([]string{
			"--mcp=stdio",
			"--disable-auth",
			"--data-dir", filepath.Join(leafwikiTempDir(), "data"),
			"--root-dir", filepath.Join(leafwikiTempDir(), "content"),
			"--host", "127.0.0.1",
			"--port", freeTCPPort(),
			"--log-target", "stderr",
		}, nil)

		_, err := io.WriteString(stdin, "not-json\n")
		Expect(err).NotTo(HaveOccurred())
		_, err = io.WriteString(stdin, `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-11-25","capabilities":{},"clientInfo":{"name":"test","version":"0"}}}`+"\n")
		Expect(err).NotTo(HaveOccurred())
		waitForFileContaining(proc.stdoutPath, `"id":1`)
		Expect(stdin.Close()).To(Succeed(), fmt.Sprintf("close stdin: %v", err))
		proc.waitForExit()

		stdout := readFileString(proc.stdoutPath)
		Expect(stdout).To(MatchNativeStdioParseErrorFrame(), fmt.Sprintf("stdout = %q, want JSON-RPC parse error", stdout))
		Expect(nativeStdioResponseByID(stdout, 1)).NotTo(BeNil(), fmt.Sprintf("stdout = %q, want initialize response after malformed frame", stdout))

		Expect(readJSONLogEntriesFromText(readFileString(proc.stderrPath))).NotTo(ContainElement(haveJSONLogEntry(leafwikiMCPStdioFailedLogMessage)))

	})
})

var _ = ginkgo.Describe("leafwiki main process", func() {
	ginkgo.It("native STDIO rejects second process with config mismatch", ginkgo.Label("e2e"), func() {
		stdinReader, stdinWriter := io.Pipe()
		defer closeBestEffort(stdinWriter)
		baseDir := leafwikiTempDir()
		dataDir := filepath.Join(baseDir, "data")
		rootDir := filepath.Join(baseDir, "content")
		rawSecret := "jwt_secret_should_not_leak"
		firstPort := freeTCPPort()
		first := startLeafwikiHelperWithStdin([]string{
			"--mcp=stdio",
			"--disable-auth",
			"--data-dir", dataDir,
			"--root-dir", rootDir,
			"--host", "127.0.0.1",
			"--port", firstPort,
			"--log-target", "stderr",
		}, nil, stdinReader)
		waitForLeafwikiReady(first, firstPort)
		_ = waitForProjectDaemonDescriptor(dataDir)

		stdout, stderr, err := runLeafwikiHelperWithTimeout([]string{
			"--mcp=stdio",
			"--disable-auth",
			"--data-dir", dataDir,
			"--root-dir", rootDir,
			"--host", "127.0.0.1",
			"--port", freeTCPPort(),
			"--markdown-link-root-prefix", "/docs",
			"--log-target", "stderr",
		}, map[string]string{"LEAFWIKI_JWT_SECRET": rawSecret}, 5*time.Second)
		Expect(err).To(MatchProcessExitError(), fmt.Sprintf("expected second process with config mismatch to exit non-zero"))
		Expect(err).NotTo(MatchError(context.DeadlineExceeded), fmt.Sprintf("second process did not exit; expected config mismatch\nstdout:\n%s\nstderr:\n%s", stdout, stderr))
		Expect(stdout).To(BeEmpty(), fmt.Sprintf("stdout = %q, want empty", stdout))
		Expect(readJSONLogEntriesFromText(stderr)).To(ContainElement(haveJSONLogEntry(localization.MessageIDCLIErrorLeafWikiStartupFailed)), fmt.Sprintf("stderr = %q, want config mismatch failure", stderr))
		Expect(readJSONLogEntriesFromText(stderr)).NotTo(ContainElement(HaveKey("jwt_secret")), fmt.Sprintf("stderr = %q, want sanitized config mismatch failure", stderr))

		Expect(stdinWriter.Close()).To(Succeed(), fmt.Sprintf("close stdin writer: %v", err))
		first.waitForExit()

	})
})
