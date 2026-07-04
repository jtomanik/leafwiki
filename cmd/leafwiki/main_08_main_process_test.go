package main

import (
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"context"
	"fmt"
	"net/http"
	"path/filepath"
	"time"

	"github.com/perber/wiki/internal/localization"
	"github.com/perber/wiki/internal/wikid"
)

var _ = ginkgo.Describe("leafwiki main process", func() {
	ginkgo.It("disabled auth owner rejects API key STDIO attach", ginkgo.Label("e2e"), func() {
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
		globalDesc := waitForGlobalWikidDescriptor(dataDir)
		ginkgo.DeferCleanup(func() {
			first.stop()
			terminateProjectDaemonProcess(globalDesc.PID)
			waitForLeafwikiUnavailable(port)
			waitForProjectLocksReusable(dataDir, rootDir, 15*time.Second)
		})

		apiKey := "lwk_disabled_auth_owner_process_secret"
		stdout, stderr, err := runLeafwikiHelperWithTimeout([]string{
			"--mcp=stdio",
			"--data-dir", dataDir,
			"--root-dir", rootDir,
			"--host", "127.0.0.1",
			"--port", port,
			"--log-target", "stderr",
		}, map[string]string{"LEAFWIKI_MCP_API_KEY": apiKey}, 5*time.Second)
		Expect(err).To(MatchProcessExitError(), fmt.Sprintf("API-key STDIO attach to disabled-auth owner unexpectedly succeeded\nstdout:\n%s\nstderr:\n%s", stdout, stderr))
		Expect(stdout).To(BeEmpty(), fmt.Sprintf("stdout = %q, want empty on rejected API-key attach", stdout))
		Expect(readJSONLogEntriesFromText(stderr)).To(ContainElement(haveJSONLogEntry(localization.MessageIDCLIErrorLeafWikiStartupFailed)), fmt.Sprintf("stderr = %q, want startup failure log", stderr))
		Expect(readJSONLogEntriesFromText(stderr)).NotTo(ContainElement(HaveKey("api_key")), fmt.Sprintf("stderr = %q, want sanitized startup failure log", stderr))

	})
})

var _ = ginkgo.Describe("leafwiki main process", func() {
	ginkgo.It("non loopback plain web owner supports later private STDIO attach", ginkgo.Label("e2e"), func() {
		baseDir := leafwikiTempDir()
		dataDir := filepath.Join(baseDir, "data")
		rootDir := filepath.Join(baseDir, "content")
		port := freeTCPPort()
		first := startLeafwikiHelper([]string{
			"--disable-auth",
			"--data-dir", dataDir,
			"--root-dir", rootDir,
			"--host", "0.0.0.0",
			"--port", port,
			"--log-target", "stderr",
		}, nil)
		waitForLeafwikiReady(first, port)

		stdout, stderr, err := runLeafwikiHelperWithInputAndTimeout([]string{
			"--mcp=stdio",
			"--disable-auth",
			"--data-dir", dataDir,
			"--root-dir", rootDir,
			"--host", "127.0.0.1",
			"--port", port,
			"--log-target", "stderr",
		}, nil, nativeStdioListToolsInput(), 8*time.Second)
		Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("stdio startup should attach to non-loopback plain web owner, got %v\nstdout:\n%s\nstderr:\n%s", err, stdout, stderr))
		Expect(stdout).To(haveNativeStdioToolListResponse(2, "wiki_create_page"), fmt.Sprintf("stdout = %q, want tools/list response from private MCP bridge", stdout))
		Expect(stderr).To(BeEmpty(), fmt.Sprintf("stderr = %q, want quiet private MCP bridge attach", stderr))

	})
})

var _ = ginkgo.Describe("leafwiki main process", func() {
	ginkgo.It("plain web second startup attaches to existing owner", ginkgo.Label("e2e"), func() {
		if !supportsGracefulProcessSignal() {
			ginkgo.Skip(fmt.Sprint("graceful process signaling is required to assert foreground session release"))
		}

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
		}, map[string]string{})
		waitForLeafwikiReady(first, port)

		second := startLeafwikiHelper([]string{
			"--disable-auth",
			"--data-dir", dataDir,
			"--root-dir", rootDir,
			"--host", "127.0.0.1",
			"--port", port,
			"--log-target", "stderr",
		}, map[string]string{})
		waitForLeafwikiReady(second, port)

		waitForForegroundSignalHandler()
		Expect(signalLeafwikiProcess(first.cmd.Process)).To(Succeed())
		first.waitForExit()
		waitForLeafwikiReady(second, port)
		stderr := readFileString(second.stderrPath)
		Expect(stderr).To(BeEmpty(), fmt.Sprintf("second foreground startup stderr = %q, want no ownership failure", stderr))
		waitForForegroundSignalHandler()
		Expect(signalLeafwikiProcess(second.cmd.Process)).To(Succeed())
		second.waitForExit()
		waitForLeafwikiUnavailable(port)

	})
})

var _ = ginkgo.Describe("leafwiki main process", func() {
	ginkgo.It("proxies native tool frames through a plain web owner", ginkgo.Label("e2e"), func() {
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

		stdout, stderr, err := runLeafwikiHelperWithInputAndTimeout([]string{
			"--mcp=stdio",
			"--disable-auth",
			"--data-dir", dataDir,
			"--root-dir", rootDir,
			"--host", "127.0.0.1",
			"--port", port,
			"--log-target", "stderr",
		}, nil, nativeStdioListToolsInput(), 8*time.Second)
		Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("stdio startup should proxy MCP frames to existing plain owner, got %v\nstdout:\n%s\nstderr:\n%s", err, stdout, stderr))
		Expect(stdout).To(haveNativeStdioToolListResponse(2, "wiki_create_page"), fmt.Sprintf("stdout = %q, want tools/list response from private MCP bridge", stdout))
		Expect(stderr).To(BeEmpty(), fmt.Sprintf("stderr = %q, want quiet private MCP bridge proxy", stderr))

		resp, err := http.Get("http://127.0.0.1:" + port + "/mcp")
		Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("GET /mcp: %v", err))

		defer func() { _ = resp.Body.Close() }()
		Expect(resp).To(HaveHTTPStatus(http.StatusNotFound))

	})
})

var _ = ginkgo.Describe("leafwiki main process", func() {
	ginkgo.It("proxies native user context through an authenticated HTTP owner", ginkgo.Label("e2e"), func() {
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

		stdout, stderr, err := runLeafwikiHelperWithInputAndTimeout([]string{
			"--mcp=stdio",
			"--data-dir", dataDir,
			"--root-dir", rootDir,
			"--host", "127.0.0.1",
			"--port", port,
			"--allow-insecure",
			"--log-target", "stderr",
		}, map[string]string{
			"LEAFWIKI_MCP_API_KEY": apiKey.Secret,
		}, nativeStdioToolCallInput(2, "wiki_get_current_user", map[string]any{}), 8*time.Second)
		Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("auth STDIO startup should proxy MCP frames to HTTP owner, got %v\nstdout:\n%s\nstderr:\n%s", err, stdout, stderr))
		Expect(stdout).To(haveNativeStdioJSONTextResponse(2, HaveKeyWithValue("user", SatisfyAll(
			HaveKeyWithValue("username", "editor"),
			HaveKeyWithValue("role", "editor"),
		))), fmt.Sprintf("stdout = %q, want get_current_user response for API-key editor", stdout))
		Expect(stderr).To(BeEmpty(), fmt.Sprintf("stderr = %q, want quiet authenticated MCP bridge proxy", stderr))

	})
})

var _ = ginkgo.Describe("leafwiki main process", func() {
	ginkgo.It("disabled auth STDIO CLIents collaborate through owner", ginkgo.Label("e2e"), func() {
		baseDir := leafwikiTempDir()
		dataDir := filepath.Join(baseDir, "data")
		rootDir := filepath.Join(baseDir, "content")
		port := freeTCPPort()
		slug := fmt.Sprintf("stdio-collaboration-%d", time.Now().UnixNano())
		title := "STDIO Collaboration Page"

		writerStdout, writerStderr, err := runLeafwikiHelperWithInputAndTimeout([]string{
			"--mcp=stdio",
			"--disable-auth",
			"--data-dir", dataDir,
			"--root-dir", rootDir,
			"--host", "127.0.0.1",
			"--port", port,
			"--log-target", "stderr",
		}, map[string]string{"LEAFWIKI_DAEMON_IDLE_TIMEOUT": "3s"}, nativeStdioToolCallInput(2, "wiki_create_page", map[string]any{
			"title": title,
			"slug":  slug,
			"kind":  "page",
		}), 8*time.Second)
		Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("writer STDIO client failed: %v\nstdout:\n%s\nstderr:\n%s", err, writerStdout, writerStderr))
		Expect(writerStdout).To(haveNativeStdioJSONTextResponse(2, HaveKeyWithValue("page", HaveKeyWithValue("slug", slug))), fmt.Sprintf("writer stdout = %q, want create_page response with slug %q", writerStdout, slug))

		readerStdout, readerStderr, err := runLeafwikiHelperWithInputAndTimeout([]string{
			"--mcp=stdio",
			"--disable-auth",
			"--data-dir", dataDir,
			"--root-dir", rootDir,
			"--host", "127.0.0.1",
			"--port", port,
			"--log-target", "stderr",
		}, map[string]string{"LEAFWIKI_DAEMON_IDLE_TIMEOUT": "3s"}, nativeStdioToolCallInput(2, "wiki_get_page_by_path", map[string]any{"path": slug}), 8*time.Second)
		Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("reader STDIO client failed: %v\nstdout:\n%s\nstderr:\n%s", err, readerStdout, readerStderr))
		Expect(readerStdout).To(haveNativeStdioJSONTextResponse(2, HaveKeyWithValue("page", SatisfyAll(
			HaveKeyWithValue("slug", slug),
			HaveKeyWithValue("title", title),
		))), fmt.Sprintf("reader stdout = %q, want get_page_by_path response for writer-created page", readerStdout))

		waitForLeafwikiUnavailable(port)

	})
})

var _ = ginkgo.Describe("leafwiki main process", func() {
	ginkgo.It("plain web owner rejects later public MCP enablement", ginkgo.Label("e2e"), func() {
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
		globalDesc := waitForGlobalWikidDescriptor(dataDir)

		stdout, stderr, err := runLeafwikiHelperWithTimeout([]string{
			"--mcp=http",
			"--disable-auth",
			"--data-dir", dataDir,
			"--root-dir", rootDir,
			"--host", "127.0.0.1",
			"--port", port,
			"--log-target", "stderr",
		}, nil, 5*time.Second)
		Expect(err).To(MatchProcessExitError(), fmt.Sprintf("later public MCP enablement unexpectedly succeeded\nstdout:\n%s\nstderr:\n%s", stdout, stderr))
		Expect(err).NotTo(MatchError(context.DeadlineExceeded), fmt.Sprintf("later public MCP enablement hung; expected config mismatch\nstdout:\n%s\nstderr:\n%s", stdout, stderr))
		Expect(readJSONLogEntriesFromText(stderr)).To(ContainElement(haveJSONLogEntry(localization.MessageIDCLIErrorLeafWikiStartupFailed)), fmt.Sprintf("stderr = %q, want startup failure log entry", stderr))

		first.stop()
		terminateProjectDaemonProcess(globalDesc.PID)
		waitForLeafwikiUnavailable(port)
		waitForProjectLocksReusable(dataDir, rootDir, 15*time.Second)

	})
})

var _ = ginkgo.Describe("leafwiki main process", func() {
	ginkgo.It("base path owner rejects later no base path startup", ginkgo.Label("e2e"), func() {
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
			"--base-path", "/wiki",
			"--log-target", "stderr",
		}, nil)
		desc := waitForProjectDaemonDescriptor(dataDir)
		Expect(desc.BasePath).To(Equal("/wiki"), fmt.Sprintf("descriptor base path = %q, want /wiki", desc.BasePath))

		waitForLeafwikiReadyAtBasePath(first, port, "/wiki")

		stdout, stderr, err := runLeafwikiHelperWithTimeout([]string{
			"--disable-auth",
			"--data-dir", dataDir,
			"--root-dir", rootDir,
			"--host", "127.0.0.1",
			"--port", port,
			"--log-target", "stderr",
		}, nil, 5*time.Second)
		Expect(err).To(MatchProcessExitError(), fmt.Sprintf("later no-base-path startup unexpectedly succeeded\nstdout:\n%s\nstderr:\n%s", stdout, stderr))
		Expect(err).NotTo(MatchError(context.DeadlineExceeded), fmt.Sprintf("later no-base-path startup hung; expected config mismatch\nstdout:\n%s\nstderr:\n%s", stdout, stderr))
		Expect(readJSONLogEntriesFromText(stderr)).To(ContainElement(haveJSONLogEntry(localization.MessageIDCLIErrorLeafWikiStartupFailed)), fmt.Sprintf("stderr = %q, want startup failure log entry", stderr))

	})
})

var _ = ginkgo.Describe("leafwiki main process", func() {
	ginkgo.It("auth owner rejects later disable auth startup", ginkgo.Label("e2e"), func() {
		baseDir := leafwikiTempDir()
		dataDir := filepath.Join(baseDir, "data")
		rootDir := filepath.Join(baseDir, "content")
		port := freeTCPPort()
		first := startLeafwikiHelper([]string{
			"--data-dir", dataDir,
			"--root-dir", rootDir,
			"--host", "127.0.0.1",
			"--port", port,
			"--jwt-secret", "owner-jwt-secret",
			"--admin-password", "owner-admin-password",
			"--allow-insecure",
			"--log-target", "stderr",
		}, nil)
		waitForLeafwikiReady(first, port)

		stdout, stderr, err := runLeafwikiHelperWithTimeout([]string{
			"--disable-auth",
			"--data-dir", dataDir,
			"--root-dir", rootDir,
			"--host", "127.0.0.1",
			"--port", port,
			"--log-target", "stderr",
		}, nil, 5*time.Second)
		Expect(err).To(MatchProcessExitError(), fmt.Sprintf("later disable-auth startup unexpectedly succeeded\nstdout:\n%s\nstderr:\n%s", stdout, stderr))
		Expect(err).NotTo(MatchError(context.DeadlineExceeded), fmt.Sprintf("later disable-auth startup hung; expected config mismatch\nstdout:\n%s\nstderr:\n%s", stdout, stderr))
		Expect(readJSONLogEntriesFromText(stderr)).To(ContainElement(haveJSONLogEntry(localization.MessageIDCLIErrorLeafWikiStartupFailed)), fmt.Sprintf("stderr = %q, want startup failure log entry", stderr))

	})
})

var _ = ginkgo.Describe("leafwiki main process", func() {
	ginkgo.It("project daemon descriptor uses default idle timeout when unspecified", ginkgo.Label("e2e"), func() {
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
		}, map[string]string{"LEAFWIKI_DAEMON_IDLE_TIMEOUT": ""})
		desc := waitForProjectDaemonDescriptor(dataDir)
		ownerPID = desc.PID
		waitForLeafwikiReady(proc, port)
		Expect(desc.IdleTimeout).To(Equal("10m0s"), fmt.Sprintf("descriptor idle timeout = %q, want core CLI default 10m0s", desc.IdleTimeout))
		Expect(desc.Config.DaemonIdleTimeout).To(Equal("10m0s"), fmt.Sprintf("descriptor config daemon idle timeout = %q, want core CLI default 10m0s", desc.Config.DaemonIdleTimeout))

		proc.stop()

	})
})
