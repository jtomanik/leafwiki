package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
	coreauth "github.com/perber/wiki/internal/core/auth"
	leaflogging "github.com/perber/wiki/internal/logging"
	"github.com/perber/wiki/internal/wiki"
	wikimcp "github.com/perber/wiki/internal/wiki/mcp"
)

func TestWriteUsage_DocumentsMCPTransportSelector(t *testing.T) {
	var buf bytes.Buffer

	writeUsage(&buf)

	output := buf.String()
	if !strings.Contains(output, "leafwiki --jwt-secret <SECRET> --admin-password <PASSWORD> [--host <HOST>] [--port <PORT>] [--data-dir <DIR>] [--root-dir <DIR>]") {
		t.Fatalf("expected authenticated startup usage to include --root-dir, got %q", output)
	}
	for _, expected := range []string{
		"--jwt-secret",
		"--admin-password",
		"--allow-insecure",
		"--data-dir",
		"--root-dir",
		"--log-target",
		"--log-file",
		"--mcp",
		"--api-key",
		"LEAFWIKI_ROOT_DIR",
		"LEAFWIKI_LOG_TARGET",
		"LEAFWIKI_LOG_FILE",
		"LEAFWIKI_MCP",
		"LEAFWIKI_MCP_API_KEY",
	} {
		if !strings.Contains(output, expected) {
			t.Fatalf("expected usage output to contain %q, got %q", expected, output)
		}
	}
	for _, removed := range []string{
		"--enable-mcp",
		"--mcp-stdio",
		"LEAFWIKI_ENABLE_MCP",
		"LEAFWIKI_MCP_STDIO",
	} {
		if strings.Contains(output, removed) {
			t.Fatalf("usage output contains removed MCP option %q: %q", removed, output)
		}
	}
}

func TestResolveLoggingConfig_DefaultsToFileUnderResolvedDataDir(t *testing.T) {
	dataDir := filepath.Join(t.TempDir(), "data")

	cfg := resolveLoggingConfigForArgs(t, []string{"--data-dir=" + dataDir})

	if cfg.Target != leaflogging.TargetFile {
		t.Fatalf("Target = %q, want %q", cfg.Target, leaflogging.TargetFile)
	}
	if got, want := cfg.FilePath, filepath.Join(dataDir, ".leafwiki", "logs", "leafwiki.log"); got != want {
		t.Fatalf("FilePath = %q, want %q", got, want)
	}
}

func TestResolveLoggingConfig_CLIOverridesEnvironmentTarget(t *testing.T) {
	t.Setenv("LEAFWIKI_LOG_TARGET", "file")

	cfg := resolveLoggingConfigForArgs(t, []string{"--log-target=stderr"})

	if cfg.Target != leaflogging.TargetStderr {
		t.Fatalf("Target = %q, want %q", cfg.Target, leaflogging.TargetStderr)
	}
	if cfg.FilePath != "" {
		t.Fatalf("FilePath = %q, want empty for stderr target", cfg.FilePath)
	}
}

func TestResolveLoggingConfig_UsesEnvironmentWhenFlagAbsent(t *testing.T) {
	t.Setenv("LEAFWIKI_LOG_TARGET", "stdout")

	cfg := resolveLoggingConfigForArgs(t, nil)

	if cfg.Target != leaflogging.TargetStdout {
		t.Fatalf("Target = %q, want %q", cfg.Target, leaflogging.TargetStdout)
	}
}

func TestResolveLoggingConfig_CLIStreamTargetIgnoresInheritedEnvLogFile(t *testing.T) {
	t.Setenv("LEAFWIKI_LOG_FILE", "logs/from-env.log")

	cfg := resolveLoggingConfigForArgs(t, []string{"--log-target=stderr"})

	if cfg.Target != leaflogging.TargetStderr {
		t.Fatalf("Target = %q, want %q", cfg.Target, leaflogging.TargetStderr)
	}
	if cfg.FilePath != "" {
		t.Fatalf("FilePath = %q, want empty for stderr target", cfg.FilePath)
	}
}

func TestResolveLoggingConfig_RejectsLogFileForStreamTarget(t *testing.T) {
	_, err := resolveLoggingConfigForArgsAllowError(t, []string{
		"--log-target=stderr",
		"--log-file=custom.log",
	})

	if err == nil {
		t.Fatalf("expected log-file with stderr target to fail")
	}
	if !strings.Contains(err.Error(), "--log-file requires --log-target file") {
		t.Fatalf("error = %v, want log-file target message", err)
	}
}

func TestMainProcess_DefaultServerLoggingUsesFileForStartupAndRequestLogsAndKeepsStdoutClean(t *testing.T) {
	dataDir := filepath.Join(t.TempDir(), "data")
	port := freeTCPPort(t)
	proc := startLeafwikiHelper(t, []string{
		"--disable-auth",
		"--data-dir", dataDir,
		"--host", "127.0.0.1",
		"--port", port,
	}, nil)

	waitForLeafwikiReady(t, proc, port)
	logPath := filepath.Join(dataDir, ".leafwiki", "logs", "leafwiki.log")
	waitForFileContaining(t, logPath, "Starting LeafWiki")
	waitForFileContaining(t, logPath, "http request")
	proc.stop(t)

	stdout := readFileString(t, proc.stdoutPath)
	if strings.Contains(stdout, "Starting LeafWiki") {
		t.Fatalf("stdout contains server log: %q", stdout)
	}
	assertJSONLogContains(t, logPath, "Starting LeafWiki")
	requestEntry := assertJSONLogContains(t, logPath, "http request")
	if requestEntry["method"] != http.MethodGet || requestEntry["path"] != "/api/health" || requestEntry["status"] != float64(http.StatusOK) {
		t.Fatalf("http request entry = %#v, want GET /api/health 200", requestEntry)
	}
}

func TestMainProcess_DefaultFileLoggingRecordsFreshDataDirectoryCreation(t *testing.T) {
	dataDir := filepath.Join(t.TempDir(), "data")
	port := freeTCPPort(t)
	proc := startLeafwikiHelper(t, []string{
		"--disable-auth",
		"--data-dir", dataDir,
		"--host", "127.0.0.1",
		"--port", port,
	}, nil)

	waitForLeafwikiReady(t, proc, port)
	waitForFileContaining(t, filepath.Join(dataDir, ".leafwiki", "logs", "leafwiki.log"), "Starting LeafWiki")
	proc.stop(t)

	entry := assertJSONLogContains(t, filepath.Join(dataDir, ".leafwiki", "logs", "leafwiki.log"), "Data directory created")
	if entry["path"] != dataDir {
		t.Fatalf("data directory log entry = %#v, want path %q", entry, dataDir)
	}
}

func TestMainProcess_CLIStderrTargetOverridesEnvFileTarget(t *testing.T) {
	dataDir := filepath.Join(t.TempDir(), "data")
	port := freeTCPPort(t)
	proc := startLeafwikiHelper(t, []string{
		"--disable-auth",
		"--data-dir", dataDir,
		"--host", "127.0.0.1",
		"--port", port,
		"--log-target", "stderr",
	}, map[string]string{
		"LEAFWIKI_LOG_TARGET": "file",
	})

	waitForLeafwikiReady(t, proc, port)
	waitForFileContaining(t, proc.stderrPath, "Starting LeafWiki")
	waitForFileContaining(t, proc.stderrPath, "http request")
	proc.stop(t)

	defaultLogPath := filepath.Join(dataDir, ".leafwiki", "logs", "leafwiki.log")
	if _, err := os.Stat(defaultLogPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("default log file stat error = %v, want not exist", err)
	}
	stdout := readFileString(t, proc.stdoutPath)
	if stdout != "" {
		t.Fatalf("stdout = %q, want empty for stderr target", stdout)
	}
}

func TestMainProcess_EnvironmentStderrTargetIsUsedWhenFlagAbsent(t *testing.T) {
	dataDir := filepath.Join(t.TempDir(), "data")
	port := freeTCPPort(t)
	proc := startLeafwikiHelper(t, []string{
		"--disable-auth",
		"--data-dir", dataDir,
		"--host", "127.0.0.1",
		"--port", port,
	}, map[string]string{
		"LEAFWIKI_LOG_TARGET": "stderr",
	})

	waitForLeafwikiReady(t, proc, port)
	waitForFileContaining(t, proc.stderrPath, "Starting LeafWiki")
	waitForFileContaining(t, proc.stderrPath, "http request")
	proc.stop(t)

	defaultLogPath := filepath.Join(dataDir, ".leafwiki", "logs", "leafwiki.log")
	if _, err := os.Stat(defaultLogPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("default log file stat error = %v, want not exist", err)
	}
}

func TestMainProcess_RejectsInvalidLogTargetOnStderrWithNoStdout(t *testing.T) {
	stdout, stderr, err := runLeafwikiHelper(t, []string{
		"--disable-auth",
		"--data-dir", filepath.Join(t.TempDir(), "data"),
	}, map[string]string{
		"LEAFWIKI_LOG_TARGET": "syslog",
	})

	if err == nil {
		t.Fatalf("expected invalid log target to exit non-zero")
	}
	if stdout != "" {
		t.Fatalf("stdout = %q, want empty", stdout)
	}
	if !strings.Contains(stderr, "invalid log target") {
		t.Fatalf("stderr = %q, want invalid log target", stderr)
	}
}

func TestMainProcess_RejectsExplicitLogFileForStreamTarget(t *testing.T) {
	stdout, stderr, err := runLeafwikiHelper(t, []string{
		"--disable-auth",
		"--data-dir", filepath.Join(t.TempDir(), "data"),
		"--log-target", "stderr",
		"--log-file", "custom.log",
	}, nil)

	if err == nil {
		t.Fatalf("expected --log-file with stderr target to exit non-zero")
	}
	if stdout != "" {
		t.Fatalf("stdout = %q, want empty", stdout)
	}
	if !strings.Contains(stderr, "--log-file requires --log-target file") {
		t.Fatalf("stderr = %q, want --log-file target error", stderr)
	}
}

func TestMainProcess_NativeStdioAuthEnabledRequiresAPIKey(t *testing.T) {
	dataDir := filepath.Join(t.TempDir(), "data")
	stdout, stderr, err := runLeafwikiHelper(t, []string{
		"--mcp=stdio",
		"--data-dir", dataDir,
		"--jwt-secret", "test-secret",
		"--admin-password", "admin-password",
	}, nil)

	if err == nil {
		t.Fatalf("expected native stdio with auth enabled to exit non-zero")
	}
	if stdout != "" {
		t.Fatalf("stdout = %q, want empty", stdout)
	}
	if !strings.Contains(stderr, "native STDIO requires either disabled auth or an API key") {
		t.Fatalf("stderr = %q, want native stdio API-key requirement", stderr)
	}
}

func TestMainProcess_NativeStdioRejectsInvalidAPIKeyWithoutLeakingSecret(t *testing.T) {
	secret := "lwk_secret_bad"
	stdout, stderr, err := runLeafwikiHelper(t, []string{
		"--mcp=stdio",
		"--api-key", secret,
		"--data-dir", filepath.Join(t.TempDir(), "data"),
		"--jwt-secret", "test-secret",
		"--admin-password", "admin-password",
		"--log-target", "stderr",
	}, nil)

	if err == nil {
		t.Fatalf("expected native stdio with invalid API key to exit non-zero")
	}
	if stdout != "" {
		t.Fatalf("stdout = %q, want empty", stdout)
	}
	if strings.Contains(stdout, secret) || strings.Contains(stderr, secret) {
		t.Fatalf("process output leaked API key\nstdout=%q\nstderr=%q", stdout, stderr)
	}
	if !strings.Contains(stderr, "invalid native STDIO API key") {
		t.Fatalf("stderr = %q, want invalid API-key error", stderr)
	}
}

func TestMainProcess_NativeStdioRejectsStdoutLogging(t *testing.T) {
	stdout, stderr, err := runLeafwikiHelper(t, []string{
		"--mcp=stdio",
		"--disable-auth",
		"--data-dir", filepath.Join(t.TempDir(), "data"),
		"--log-target", "stdout",
	}, nil)

	if err == nil {
		t.Fatalf("expected native stdio with stdout logging to exit non-zero")
	}
	if stdout != "" {
		t.Fatalf("stdout = %q, want empty", stdout)
	}
	if !strings.Contains(stderr, "stdout is reserved for MCP STDIO") {
		t.Fatalf("stderr = %q, want stdout reserved error", stderr)
	}
}

func TestMainProcess_NativeStdioRejectsNonLoopbackHost(t *testing.T) {
	stdout, stderr, err := runLeafwikiHelper(t, []string{
		"--mcp=stdio",
		"--disable-auth",
		"--host", "0.0.0.0",
		"--data-dir", filepath.Join(t.TempDir(), "data"),
		"--log-target", "stderr",
	}, nil)

	if err == nil {
		t.Fatalf("expected native stdio on a non-loopback host to exit non-zero")
	}
	if stdout != "" {
		t.Fatalf("stdout = %q, want empty", stdout)
	}
	if !strings.Contains(stderr, "MCP requires a loopback host") {
		t.Fatalf("stderr = %q, want unified MCP loopback host error", stderr)
	}
}

func TestMainProcess_NativeStdioRejectsPositionalCommandWithStderrOnly(t *testing.T) {
	stdout, stderr, err := runLeafwikiHelper(t, []string{
		"--mcp=stdio",
		"bogus",
	}, nil)

	if err == nil {
		t.Fatalf("expected native stdio with a positional command to exit non-zero")
	}
	if stdout != "" {
		t.Fatalf("stdout = %q, want empty", stdout)
	}
	if !strings.Contains(stderr, "native STDIO does not support positional commands") {
		t.Fatalf("stderr = %q, want positional-command error", stderr)
	}
}

func TestMainProcess_NativeStdioEnvironmentRejectsPositionalCommandWithStderrOnly(t *testing.T) {
	stdout, stderr, err := runLeafwikiHelper(t, []string{
		"--disable-auth",
		"bogus",
	}, map[string]string{
		"LEAFWIKI_MCP": "stdio",
	})

	if err == nil {
		t.Fatalf("expected env-enabled native stdio with a positional command to exit non-zero")
	}
	if stdout != "" {
		t.Fatalf("stdout = %q, want empty", stdout)
	}
	if !strings.Contains(stderr, "native STDIO does not support positional commands") {
		t.Fatalf("stderr = %q, want positional-command error", stderr)
	}
}

func TestMainProcess_NativeStdioStartsHTTPAndStdinCloseStopsServer(t *testing.T) {
	stdinReader, stdinWriter := io.Pipe()
	dataDir := filepath.Join(t.TempDir(), "data")
	rootDir := filepath.Join(t.TempDir(), "content")
	port := freeTCPPort(t)
	proc := startLeafwikiHelperWithStdin(t, []string{
		"--mcp=stdio",
		"--disable-auth",
		"--data-dir", dataDir,
		"--root-dir", rootDir,
		"--host", "127.0.0.1",
		"--port", port,
		"--log-target", "stderr",
	}, nil, stdinReader)

	waitForLeafwikiReady(t, proc, port)
	if err := stdinWriter.Close(); err != nil {
		t.Fatalf("close stdin writer: %v", err)
	}
	proc.waitForExit(t)

	if stdout := readFileString(t, proc.stdoutPath); stdout != "" {
		t.Fatalf("stdout = %q, want empty without MCP frames", stdout)
	}
	waitForLeafwikiUnavailable(t, port)
}

func TestMainProcess_NativeStdioOnlyKeepsHTTPMCPRouteDisabled(t *testing.T) {
	stdinReader, stdinWriter := io.Pipe()
	defer stdinWriter.Close()
	port := freeTCPPort(t)
	proc := startLeafwikiHelperWithStdin(t, []string{
		"--mcp=stdio",
		"--disable-auth",
		"--data-dir", filepath.Join(t.TempDir(), "data"),
		"--root-dir", filepath.Join(t.TempDir(), "content"),
		"--host", "127.0.0.1",
		"--port", port,
		"--log-target", "stderr",
	}, nil, stdinReader)

	waitForLeafwikiReady(t, proc, port)
	resp, err := http.Get("http://127.0.0.1:" + port + "/mcp")
	if err != nil {
		t.Fatalf("GET /mcp: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("/mcp status = %d, want %d", resp.StatusCode, http.StatusNotFound)
	}

	if err := stdinWriter.Close(); err != nil {
		t.Fatalf("close stdin writer: %v", err)
	}
	proc.waitForExit(t)
	if stdout := readFileString(t, proc.stdoutPath); stdout != "" {
		t.Fatalf("stdout = %q, want empty without MCP frames", stdout)
	}
}

func TestMainProcess_CombinedNativeStdioHTTPExposesHTTPMCPToolSurface(t *testing.T) {
	stdinReader, stdinWriter := io.Pipe()
	defer stdinWriter.Close()
	port := freeTCPPort(t)
	proc := startLeafwikiHelperWithStdin(t, []string{
		"--mcp=stdio,http",
		"--disable-auth",
		"--data-dir", filepath.Join(t.TempDir(), "data"),
		"--root-dir", filepath.Join(t.TempDir(), "content"),
		"--host", "127.0.0.1",
		"--port", port,
		"--log-target", "stderr",
	}, nil, stdinReader)

	waitForLeafwikiReady(t, proc, port)
	toolNames := listProcessHTTPMCPToolNames(t, "http://127.0.0.1:"+port+"/mcp")
	assertToolNamesMatch(t, toolNames, wikimcp.BaseToolNames())

	if err := stdinWriter.Close(); err != nil {
		t.Fatalf("close stdin writer: %v", err)
	}
	proc.waitForExit(t)
	if stdout := readFileString(t, proc.stdoutPath); stdout != "" {
		t.Fatalf("stdout = %q, want empty without MCP frames", stdout)
	}
}

func TestMainProcess_LegacyMCPFlagsAreIgnored(t *testing.T) {
	port := freeTCPPort(t)
	proc := startLeafwikiHelper(t, []string{
		"--enable-mcp",
		"--mcp-stdio",
		"--disable-auth",
		"--data-dir", filepath.Join(t.TempDir(), "data"),
		"--root-dir", filepath.Join(t.TempDir(), "content"),
		"--host", "127.0.0.1",
		"--port", port,
		"--log-target", "stderr",
	}, nil)

	waitForLeafwikiReady(t, proc, port)
	resp, err := http.Get("http://127.0.0.1:" + port + "/mcp")
	if err != nil {
		t.Fatalf("GET /mcp: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("/mcp status = %d, want %d", resp.StatusCode, http.StatusNotFound)
	}
	proc.stop(t)

	stdout := readFileString(t, proc.stdoutPath)
	if stdout != "" {
		t.Fatalf("stdout = %q, want empty", stdout)
	}
	stderr := readFileString(t, proc.stderrPath)
	if strings.Contains(stderr, "LeafWiki HTTP listening") {
		t.Fatalf("stderr = %q, want no native STDIO HTTP diagnostic", stderr)
	}
}

func TestMainProcess_NativeStdioMalformedJSONReturnsParseErrorAndContinues(t *testing.T) {
	proc, stdin := startLeafwikiHelperWithStdinPipe(t, []string{
		"--mcp=stdio",
		"--disable-auth",
		"--data-dir", filepath.Join(t.TempDir(), "data"),
		"--root-dir", filepath.Join(t.TempDir(), "content"),
		"--host", "127.0.0.1",
		"--port", freeTCPPort(t),
		"--log-target", "stderr",
	}, nil)

	if _, err := io.WriteString(stdin, "not-json\n"); err != nil {
		t.Fatalf("write malformed frame: %v", err)
	}
	if _, err := io.WriteString(stdin, `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-11-25","capabilities":{},"clientInfo":{"name":"test","version":"0"}}}`+"\n"); err != nil {
		t.Fatalf("write initialize frame: %v", err)
	}
	waitForFileContaining(t, proc.stdoutPath, `"id":1`)
	if err := stdin.Close(); err != nil {
		t.Fatalf("close stdin: %v", err)
	}
	proc.waitForExit(t)

	stdout := readFileString(t, proc.stdoutPath)
	if !strings.Contains(stdout, `"code":-32700`) {
		t.Fatalf("stdout = %q, want JSON-RPC parse error", stdout)
	}
	if !strings.Contains(stdout, `"id":null`) {
		t.Fatalf("stdout = %q, want parse error id null", stdout)
	}
	if !strings.Contains(stdout, `"id":1`) {
		t.Fatalf("stdout = %q, want initialize response after malformed frame", stdout)
	}
	if stderr := readFileString(t, proc.stderrPath); strings.Contains(stderr, "MCP STDIO failed") {
		t.Fatalf("stderr = %q, want malformed JSON to stay protocol-level", stderr)
	}
}

func TestMainProcess_NativeStdioRejectsSecondProcessWithSameDataDir(t *testing.T) {
	stdinReader, stdinWriter := io.Pipe()
	defer stdinWriter.Close()
	baseDir := t.TempDir()
	dataDir := filepath.Join(baseDir, "data")
	rootDir := filepath.Join(baseDir, "content")
	firstPort := freeTCPPort(t)
	first := startLeafwikiHelperWithStdin(t, []string{
		"--mcp=stdio",
		"--disable-auth",
		"--data-dir", dataDir,
		"--root-dir", rootDir,
		"--host", "127.0.0.1",
		"--port", firstPort,
		"--log-target", "stderr",
	}, nil, stdinReader)
	waitForLeafwikiReady(t, first, firstPort)

	stdout, stderr, err := runLeafwikiHelperWithTimeout(t, []string{
		"--mcp=stdio",
		"--disable-auth",
		"--data-dir", dataDir,
		"--root-dir", rootDir,
		"--host", "127.0.0.1",
		"--port", freeTCPPort(t),
		"--log-target", "stderr",
	}, nil, 5*time.Second)

	if err == nil {
		t.Fatalf("expected second process with same data dir to exit non-zero")
	}
	if errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("second process did not exit; expected data directory lock rejection\nstdout:\n%s\nstderr:\n%s", stdout, stderr)
	}
	if stdout != "" {
		t.Fatalf("stdout = %q, want empty", stdout)
	}
	if !strings.Contains(stderr, "data directory is already in use") {
		t.Fatalf("stderr = %q, want data directory lock error", stderr)
	}

	if err := stdinWriter.Close(); err != nil {
		t.Fatalf("close stdin writer: %v", err)
	}
	first.waitForExit(t)
}

func TestMainProcess_NativeStdioRejectsSecondProcessWithSameRootDir(t *testing.T) {
	stdinReader, stdinWriter := io.Pipe()
	defer stdinWriter.Close()
	baseDir := t.TempDir()
	rootDir := filepath.Join(baseDir, "content")
	firstPort := freeTCPPort(t)
	first := startLeafwikiHelperWithStdin(t, []string{
		"--mcp=stdio",
		"--disable-auth",
		"--data-dir", filepath.Join(baseDir, "data-a"),
		"--root-dir", rootDir,
		"--host", "127.0.0.1",
		"--port", firstPort,
		"--log-target", "stderr",
	}, nil, stdinReader)
	waitForLeafwikiReady(t, first, firstPort)

	stdout, stderr, err := runLeafwikiHelperWithTimeout(t, []string{
		"--mcp=stdio",
		"--disable-auth",
		"--data-dir", filepath.Join(baseDir, "data-b"),
		"--root-dir", rootDir,
		"--host", "127.0.0.1",
		"--port", freeTCPPort(t),
		"--log-target", "stderr",
	}, nil, 5*time.Second)

	if err == nil {
		t.Fatalf("expected second process with same root dir to exit non-zero")
	}
	if errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("second process did not exit; expected root directory lock rejection\nstdout:\n%s\nstderr:\n%s", stdout, stderr)
	}
	if stdout != "" {
		t.Fatalf("stdout = %q, want empty", stdout)
	}
	if !strings.Contains(stderr, "root directory is already in use") {
		t.Fatalf("stderr = %q, want root directory lock error", stderr)
	}

	if err := stdinWriter.Close(); err != nil {
		t.Fatalf("close stdin writer: %v", err)
	}
	first.waitForExit(t)
}

func TestMainProcess_NativeStdioSIGTERMReleasesDataDirLock(t *testing.T) {
	if !supportsGracefulProcessSignal() {
		t.Skip("SIGTERM-style graceful process signaling is not available on this platform")
	}

	baseDir := t.TempDir()
	dataDir := filepath.Join(baseDir, "data")
	rootDir := filepath.Join(baseDir, "content")
	firstPort := freeTCPPort(t)
	first, firstStdinWriter := startLeafwikiHelperWithStdinPipe(t, []string{
		"--mcp=stdio",
		"--disable-auth",
		"--data-dir", dataDir,
		"--root-dir", rootDir,
		"--host", "127.0.0.1",
		"--port", firstPort,
		"--log-target", "stderr",
	}, nil)
	waitForLeafwikiReady(t, first, firstPort)

	if err := signalLeafwikiProcess(first.cmd.Process); err != nil {
		t.Fatalf("send SIGTERM: %v", err)
	}
	first.waitForExit(t)
	_ = firstStdinWriter.Close()
	waitForLeafwikiUnavailable(t, firstPort)

	secondStdinReader, secondStdinWriter := io.Pipe()
	secondPort := freeTCPPort(t)
	second := startLeafwikiHelperWithStdin(t, []string{
		"--mcp=stdio",
		"--disable-auth",
		"--data-dir", dataDir,
		"--root-dir", rootDir,
		"--host", "127.0.0.1",
		"--port", secondPort,
		"--log-target", "stderr",
	}, nil, secondStdinReader)
	waitForLeafwikiReady(t, second, secondPort)

	if err := secondStdinWriter.Close(); err != nil {
		t.Fatalf("close second stdin writer: %v", err)
	}
	second.waitForExit(t)
}

func TestMainProcess_FileTargetStartupFailureAlsoReachesStderr(t *testing.T) {
	dataDir := filepath.Join(t.TempDir(), "data")
	stdout, stderr, err := runLeafwikiHelper(t, []string{
		"--data-dir", dataDir,
		"--admin-password", "admin-password",
	}, nil)

	if err == nil {
		t.Fatalf("expected missing JWT secret to exit non-zero")
	}
	if stdout != "" {
		t.Fatalf("stdout = %q, want empty", stdout)
	}
	if !strings.Contains(stderr, "JWT secret is required") {
		t.Fatalf("stderr = %q, want JWT secret error", stderr)
	}
	assertJSONLogContains(t, filepath.Join(dataDir, ".leafwiki", "logs", "leafwiki.log"), "JWT secret is required. Set it using --jwt-secret or LEAFWIKI_JWT_SECRET environment variable.")
}

func TestMainProcess_HelpStaysOnStdout(t *testing.T) {
	stdout, stderr, err := runLeafwikiHelper(t, []string{"--help"}, nil)

	if err != nil {
		t.Fatalf("help process error = %v, stderr=%q", err, stderr)
	}
	for _, expected := range []string{"Usage:", "--log-target", "--log-file"} {
		if !strings.Contains(stdout, expected) {
			t.Fatalf("stdout = %q, want %q", stdout, expected)
		}
	}
	if strings.Contains(stderr, `"msg"`) {
		t.Fatalf("stderr contains log output: %q", stderr)
	}
}

func TestMainProcess_HelpFlagAfterOtherFlagsStaysOnStdout(t *testing.T) {
	stdout, stderr, err := runLeafwikiHelper(t, []string{
		"--log-target", "stderr",
		"--help",
	}, nil)

	if err != nil {
		t.Fatalf("help process error = %v, stderr=%q", err, stderr)
	}
	if !strings.Contains(stdout, "Usage:") || !strings.Contains(stdout, "--log-target") {
		t.Fatalf("stdout = %q, want help output", stdout)
	}
	if stderr != "" {
		t.Fatalf("stderr = %q, want empty", stderr)
	}
}

func TestMainProcess_HelpFlagValueDoesNotShortCircuitSubcommandParsing(t *testing.T) {
	stdout, stderr, err := runLeafwikiHelper(t, []string{
		"--admin-password", "help",
		"unknown-command",
	}, nil)

	if err != nil {
		t.Fatalf("unknown command process error = %v, stderr=%q", err, stderr)
	}
	if !strings.Contains(stdout, "Unknown command: unknown-command") {
		t.Fatalf("stdout = %q, want unknown command handling", stdout)
	}
}

func TestMainProcess_UnknownCommandIgnoresDirtyServerOnlyEnvironment(t *testing.T) {
	dataDir := filepath.Join(t.TempDir(), "data")
	stdout, stderr, err := runLeafwikiHelper(t, []string{
		"--data-dir", dataDir,
		"unknown-command",
	}, map[string]string{
		"LEAFWIKI_MAX_ASSET_UPLOAD_SIZE": "bad",
	})

	if err != nil {
		t.Fatalf("unknown command process error = %v, stderr=%q", err, stderr)
	}
	if !strings.Contains(stdout, "Unknown command: unknown-command") {
		t.Fatalf("stdout = %q, want unknown command handling", stdout)
	}
	if stderr != "" {
		t.Fatalf("stderr = %q, want empty", stderr)
	}
}

func TestMainProcess_UnknownCommandIgnoresMCPStdioEnvironment(t *testing.T) {
	dataDir := filepath.Join(t.TempDir(), "data")
	stdout, stderr, err := runLeafwikiHelper(t, []string{
		"--data-dir", dataDir,
		"unknown-command",
	}, map[string]string{
		"LEAFWIKI_MCP_STDIO": "true",
	})

	if err != nil {
		t.Fatalf("unknown command process error = %v, stderr=%q", err, stderr)
	}
	if !strings.Contains(stdout, "Unknown command: unknown-command") {
		t.Fatalf("stdout = %q, want unknown command handling", stdout)
	}
	if stderr != "" {
		t.Fatalf("stderr = %q, want empty", stderr)
	}
}

func TestMainProcess_ResetAdminPasswordIgnoresDirtyServerOnlyEnvironment(t *testing.T) {
	dataDir := filepath.Join(t.TempDir(), "data")
	initAdminUser(t, dataDir)

	stdout, stderr, err := runLeafwikiHelper(t, []string{
		"--data-dir", dataDir,
		"reset-admin-password",
	}, map[string]string{
		"LEAFWIKI_MAX_ASSET_UPLOAD_SIZE": "bad",
	})

	if err != nil {
		t.Fatalf("reset-admin-password process error = %v, stderr=%q", err, stderr)
	}
	if !strings.Contains(stdout, "Admin password reset successfully") {
		t.Fatalf("stdout = %q, want reset output", stdout)
	}
}

func TestMainProcess_UnknownCommandStaysUserFacingAndDoesNotCreateLogFile(t *testing.T) {
	dataDir := filepath.Join(t.TempDir(), "data")
	stdout, stderr, err := runLeafwikiHelper(t, []string{
		"--data-dir", dataDir,
		"unknown-command",
	}, nil)

	if err != nil {
		t.Fatalf("unknown command process error = %v, stderr=%q", err, stderr)
	}
	if !strings.Contains(stdout, "Unknown command: unknown-command") || !strings.Contains(stdout, "Usage:") {
		t.Fatalf("stdout = %q, want unknown command and usage", stdout)
	}
	if strings.Contains(stderr, "Starting LeafWiki") {
		t.Fatalf("stderr contains server log: %q", stderr)
	}
	if _, err := os.Stat(filepath.Join(dataDir, ".leafwiki", "logs", "leafwiki.log")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("log file stat error = %v, want not exist", err)
	}
}

func TestMainProcess_ResetAdminPasswordKeepsCredentialsOnStdoutOnly(t *testing.T) {
	dataDir := filepath.Join(t.TempDir(), "data")
	initAdminUser(t, dataDir)

	stdout, stderr, err := runLeafwikiHelper(t, []string{
		"--data-dir", dataDir,
		"reset-admin-password",
	}, nil)

	if err != nil {
		t.Fatalf("reset-admin-password process error = %v, stderr=%q", err, stderr)
	}
	if !strings.Contains(stdout, "Admin password reset successfully") || !strings.Contains(stdout, "New password") {
		t.Fatalf("stdout = %q, want reset credentials", stdout)
	}
	if strings.Contains(stdout, `"msg"`) || strings.Contains(stdout, "Starting LeafWiki") {
		t.Fatalf("stdout contains log output: %q", stdout)
	}
	if _, err := os.Stat(filepath.Join(dataDir, ".leafwiki", "logs", "leafwiki.log")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("log file stat error = %v, want not exist", err)
	}
}

func TestMainProcess_ExplicitStdoutTargetWritesServerLogsToStdout(t *testing.T) {
	dataDir := filepath.Join(t.TempDir(), "data")
	port := freeTCPPort(t)
	proc := startLeafwikiHelper(t, []string{
		"--disable-auth",
		"--data-dir", dataDir,
		"--host", "127.0.0.1",
		"--port", port,
		"--log-target", "stdout",
	}, nil)

	waitForLeafwikiReady(t, proc, port)
	waitForFileContaining(t, proc.stdoutPath, "Starting LeafWiki")
	waitForFileContaining(t, proc.stdoutPath, "http request")
	proc.stop(t)

	stdout := readFileString(t, proc.stdoutPath)
	if !strings.Contains(stdout, "Starting LeafWiki") {
		t.Fatalf("stdout = %q, want server log", stdout)
	}
	if !strings.Contains(stdout, "http request") {
		t.Fatalf("stdout = %q, want request log", stdout)
	}
	if _, err := os.Stat(filepath.Join(dataDir, ".leafwiki", "logs", "leafwiki.log")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("default log file stat error = %v, want not exist", err)
	}
}

func TestMainProcess_DisableRequestLogSuppressesProcessRequestLog(t *testing.T) {
	dataDir := filepath.Join(t.TempDir(), "data")
	port := freeTCPPort(t)
	proc := startLeafwikiHelper(t, []string{
		"--disable-auth",
		"--data-dir", dataDir,
		"--host", "127.0.0.1",
		"--port", port,
		"--log-target", "stderr",
		"--disable-request-log",
	}, nil)

	waitForLeafwikiReady(t, proc, port)
	waitForFileContaining(t, proc.stderrPath, "Starting LeafWiki")
	proc.stop(t)

	stderr := readFileString(t, proc.stderrPath)
	if strings.Contains(stderr, "http request") {
		t.Fatalf("stderr = %q, want request log suppressed", stderr)
	}
	stdout := readFileString(t, proc.stdoutPath)
	if stdout != "" {
		t.Fatalf("stdout = %q, want empty for stderr target", stdout)
	}
}

func TestResolveWorkspace_DefaultsRootDirUnderDataDir(t *testing.T) {
	dataDir := filepath.Join(t.TempDir(), "data")

	workspace := resolveWorkspaceForArgs(t, []string{"--data-dir=" + dataDir})

	if workspace.DataDir != dataDir {
		t.Fatalf("DataDir = %q, want %q", workspace.DataDir, dataDir)
	}
	if got, want := workspace.RootDir, filepath.Join(dataDir, "root"); got != want {
		t.Fatalf("RootDir = %q, want %q", got, want)
	}
}

func TestResolveWorkspace_EnvRootDirOverridesDefault(t *testing.T) {
	dataDir := filepath.Join(t.TempDir(), "data")
	rootDir := filepath.Join(t.TempDir(), "content")
	t.Setenv("LEAFWIKI_ROOT_DIR", rootDir)

	workspace := resolveWorkspaceForArgs(t, []string{"--data-dir=" + dataDir})

	if workspace.RootDir != rootDir {
		t.Fatalf("RootDir = %q, want env root %q", workspace.RootDir, rootDir)
	}
}

func TestResolveWorkspace_CLIRootDirOverridesEnv(t *testing.T) {
	dataDir := filepath.Join(t.TempDir(), "data")
	envRootDir := filepath.Join(t.TempDir(), "env-content")
	cliRootDir := filepath.Join(t.TempDir(), "cli-content")
	t.Setenv("LEAFWIKI_ROOT_DIR", envRootDir)

	workspace := resolveWorkspaceForArgs(t, []string{
		"--data-dir=" + dataDir,
		"--root-dir=" + cliRootDir,
	})

	if workspace.RootDir != cliRootDir {
		t.Fatalf("RootDir = %q, want CLI root %q", workspace.RootDir, cliRootDir)
	}
}

func TestResolveWorkspace_NormalizesPaths(t *testing.T) {
	baseDir := t.TempDir()
	dataDir := filepath.Join(baseDir, "data")
	rootDir := filepath.Join(baseDir, "content")

	workspace := resolveWorkspaceForArgs(t, []string{
		"--data-dir= " + dataDir + string(filepath.Separator) + ". ",
		"--root-dir= " + rootDir + string(filepath.Separator) + ". ",
	})

	if workspace.DataDir != dataDir {
		t.Fatalf("DataDir = %q, want normalized %q", workspace.DataDir, dataDir)
	}
	if workspace.RootDir != rootDir {
		t.Fatalf("RootDir = %q, want normalized %q", workspace.RootDir, rootDir)
	}
}

func TestValidateWorkspaceRejectsSameDataAndRootDir(t *testing.T) {
	dir := t.TempDir()

	err := validateWorkspaceDirs(dir, filepath.Clean(filepath.Join(dir, ".")))
	if err == nil {
		t.Fatalf("expected RootDir == DataDir to be rejected")
	}
	if !strings.Contains(err.Error(), "root dir must be different from data dir") {
		t.Fatalf("unexpected validation error: %v", err)
	}
}

func TestValidateWorkspaceRejectsRootDirContainingDataDir(t *testing.T) {
	rootDir := filepath.Join(t.TempDir(), "wiki")
	dataDir := filepath.Join(rootDir, "data")

	err := validateWorkspaceDirs(dataDir, rootDir)
	if err == nil {
		t.Fatalf("expected RootDir containing DataDir to be rejected")
	}
	if !strings.Contains(err.Error(), "root dir must not contain data dir") {
		t.Fatalf("unexpected validation error: %v", err)
	}
}

func TestResolveStartupWorkspace_SkipsWorkspaceValidationForResetAdminPassword(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("LEAFWIKI_ROOT_DIR", dir)

	fs := flag.NewFlagSet("leafwiki", flag.ContinueOnError)
	var errOut bytes.Buffer
	fs.SetOutput(&errOut)
	flags := registerFlags(fs)
	if err := fs.Parse([]string{"--data-dir=" + dir, "reset-admin-password"}); err != nil {
		t.Fatalf("parse flags: %v (%s)", err, errOut.String())
	}
	visited := map[string]bool{}
	fs.Visit(func(f *flag.Flag) { visited[f.Name] = true })

	if _, shouldStart, err := resolveStartupWorkspace(flags, visited, fs.Args()); err != nil || shouldStart {
		t.Fatalf("resolveStartupWorkspace reset = shouldStart %v err %v, want no validation and no startup", shouldStart, err)
	}
}

func TestResolveMCPTransports_DefaultEnvCLIAndOldOptions(t *testing.T) {
	t.Run("default none", func(t *testing.T) {
		got, err := resolveMCPTransportsForArgs(t, nil)
		if err != nil {
			t.Fatalf("resolveMCPTransports: %v", err)
		}
		if got.HTTP || got.Stdio {
			t.Fatalf("default transports = %#v, want none", got)
		}
	})

	t.Run("env enables http", func(t *testing.T) {
		t.Setenv("LEAFWIKI_MCP", "http")
		got, err := resolveMCPTransportsForArgs(t, nil)
		if err != nil {
			t.Fatalf("resolveMCPTransports: %v", err)
		}
		if !got.HTTP || got.Stdio {
			t.Fatalf("env transports = %#v, want http only", got)
		}
	})

	t.Run("cli overrides env", func(t *testing.T) {
		t.Setenv("LEAFWIKI_MCP", "http")
		got, err := resolveMCPTransportsForArgs(t, []string{"--mcp=stdio"})
		if err != nil {
			t.Fatalf("resolveMCPTransports: %v", err)
		}
		if got.HTTP || !got.Stdio {
			t.Fatalf("CLI transports = %#v, want stdio only", got)
		}
	})

	t.Run("combined orderings", func(t *testing.T) {
		for _, raw := range []string{"--mcp=stdio,http", "--mcp=http,stdio"} {
			got, err := resolveMCPTransportsForArgs(t, []string{raw})
			if err != nil {
				t.Fatalf("resolveMCPTransports(%s): %v", raw, err)
			}
			if !got.HTTP || !got.Stdio {
				t.Fatalf("%s transports = %#v, want both", raw, got)
			}
		}
	})

	t.Run("old flags and env ignored", func(t *testing.T) {
		t.Setenv("LEAFWIKI_ENABLE_MCP", "true")
		t.Setenv("LEAFWIKI_MCP_STDIO", "true")
		got, err := resolveMCPTransportsForArgs(t, []string{"--enable-mcp", "--mcp-stdio", "--disable-auth"})
		if err != nil {
			t.Fatalf("resolveMCPTransports: %v", err)
		}
		if got.HTTP || got.Stdio {
			t.Fatalf("old MCP options transports = %#v, want none", got)
		}
	})

	t.Run("selector overrides legacy env", func(t *testing.T) {
		t.Setenv("LEAFWIKI_ENABLE_MCP", "true")
		t.Setenv("LEAFWIKI_MCP_STDIO", "true")
		got, err := resolveMCPTransportsForArgs(t, []string{"--mcp=none"})
		if err != nil {
			t.Fatalf("resolveMCPTransports: %v", err)
		}
		if got.HTTP || got.Stdio {
			t.Fatalf("selector transports = %#v, want none", got)
		}
	})
}

func TestParseMCPTransports_RejectsInvalidValues(t *testing.T) {
	tests := []struct {
		name      string
		raw       string
		wantError string
	}{
		{name: "unknown", raw: "websocket", wantError: "invalid MCP transport"},
		{name: "none combined", raw: "none,stdio", wantError: "none cannot be combined"},
		{name: "duplicate", raw: "stdio,stdio", wantError: "duplicate MCP transport"},
		{name: "empty part", raw: "stdio,", wantError: "invalid MCP transport"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := parseMCPTransports(tt.raw); err == nil || !strings.Contains(err.Error(), tt.wantError) {
				t.Fatalf("parseMCPTransports(%q) error = %v, want %q", tt.raw, err, tt.wantError)
			}
		})
	}
}

func TestValidateMCPTransportOptions(t *testing.T) {
	tests := []struct {
		name      string
		opts      mcpTransportOptions
		wantError string
	}{
		{
			name: "HTTP requires loopback",
			opts: mcpTransportOptions{
				Transports: mcpTransports{HTTP: true},
				Host:       "0.0.0.0",
				LogTarget:  leaflogging.TargetStderr,
			},
			wantError: "MCP requires a loopback host",
		},
		{
			name: "STDIO requires loopback",
			opts: mcpTransportOptions{
				Transports:  mcpTransports{Stdio: true},
				DisableAuth: true,
				Host:        "0.0.0.0",
				LogTarget:   leaflogging.TargetStderr,
			},
			wantError: "MCP requires a loopback host",
		},
		{
			name: "STDIO rejects stdout logging",
			opts: mcpTransportOptions{
				Transports:  mcpTransports{Stdio: true},
				DisableAuth: true,
				Host:        "127.0.0.1",
				LogTarget:   leaflogging.TargetStdout,
			},
			wantError: "stdout is reserved for MCP STDIO",
		},
		{
			name: "STDIO auth enabled requires key",
			opts: mcpTransportOptions{
				Transports: mcpTransports{Stdio: true},
				Host:       "127.0.0.1",
				LogTarget:  leaflogging.TargetStderr,
			},
			wantError: "native STDIO requires either disabled auth or an API key",
		},
		{
			name: "STDIO disabled auth rejects key",
			opts: mcpTransportOptions{
				Transports:  mcpTransports{Stdio: true},
				DisableAuth: true,
				APIKey:      "lwk_fake",
				Host:        "127.0.0.1",
				LogTarget:   leaflogging.TargetStderr,
			},
			wantError: "disabled auth and API-key STDIO identity cannot be combined",
		},
		{
			name: "HTTP ignores API key",
			opts: mcpTransportOptions{
				Transports: mcpTransports{HTTP: true},
				APIKey:     "lwk_invalid",
				Host:       "127.0.0.1",
				LogTarget:  leaflogging.TargetStderr,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateMCPTransportOptions(tt.opts)
			if tt.wantError == "" {
				if err != nil {
					t.Fatalf("validateMCPTransportOptions() error = %v, want nil", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantError) {
				t.Fatalf("validateMCPTransportOptions() error = %v, want %q", err, tt.wantError)
			}
		})
	}
}

func TestBuildHTTPRouterOptions_PropagatesMCPEnablement(t *testing.T) {
	opts := buildHTTPRouterOptions(httpRouterOptionsInput{
		publicAccess:        true,
		authDisabled:        true,
		enableMCP:           true,
		host:                "127.0.0.1",
		mcpToolListPageSize: 7,
	})

	if !opts.MCPEnabled {
		t.Fatalf("expected MCPEnabled to be true")
	}
	if opts.MCPToolListPageSize != 7 {
		t.Fatalf("expected MCPToolListPageSize 7, got %d", opts.MCPToolListPageSize)
	}
	if opts.MCPBindHost != "127.0.0.1" {
		t.Fatalf("expected MCPBindHost 127.0.0.1, got %q", opts.MCPBindHost)
	}
}

func TestBuildListenAddress_HandlesIPv6Loopback(t *testing.T) {
	got := buildListenAddress("::1", "8080")
	if got != "[::1]:8080" {
		t.Fatalf("buildListenAddress(::1, 8080) = %q, want %q", got, "[::1]:8080")
	}
}

func TestRegisterFlags_AcceptsSingleDashLongFlags(t *testing.T) {
	fs := flag.NewFlagSet("leafwiki", flag.ContinueOnError)
	var errOut bytes.Buffer
	fs.SetOutput(&errOut)
	flags := registerFlags(fs)

	err := fs.Parse([]string{
		"-jwt-secret=test-secret",
		"-admin-password=test-password",
		"-allow-insecure=true",
	})
	if err != nil {
		t.Fatalf("expected single-dash long flags to parse, got %v (%s)", err, errOut.String())
	}

	if got := *flags.jwtSecret; got != "test-secret" {
		t.Fatalf("expected jwt secret %q, got %q", "test-secret", got)
	}
	if got := *flags.adminPassword; got != "test-password" {
		t.Fatalf("expected admin password %q, got %q", "test-password", got)
	}
	if !*flags.allowInsecure {
		t.Fatalf("expected allow-insecure to be true")
	}
}

func TestValidateHTTPRemoteUserConfig(t *testing.T) {
	tests := []struct {
		name            string
		enabled         bool
		trustedProxyIPs string
		wantErr         bool
	}{
		{"disabled, no IPs", false, "", false},
		{"disabled, with IPs", false, "127.0.0.1", false},
		{"enabled, with IPs", true, "127.0.0.1", false},
		{"enabled, multiple IPs", true, "127.0.0.1,172.18.0.0/16", false},
		{"enabled, no IPs", true, "", true},
		{"enabled, whitespace only", true, "   ", true},
		{"enabled, commas only", true, ",,,", true},
		{"enabled, commas and whitespace", true, " , , ", true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := validateHTTPRemoteUserConfig(tc.enabled, tc.trustedProxyIPs)
			if (err != nil) != tc.wantErr {
				t.Fatalf("validateHTTPRemoteUserConfig(%v, %q) error = %v, wantErr %v", tc.enabled, tc.trustedProxyIPs, err, tc.wantErr)
			}
		})
	}
}

func TestRegisterFlags_AcceptsDoubleDashLongFlags(t *testing.T) {
	fs := flag.NewFlagSet("leafwiki", flag.ContinueOnError)
	var errOut bytes.Buffer
	fs.SetOutput(&errOut)
	flags := registerFlags(fs)

	err := fs.Parse([]string{
		"--jwt-secret=test-secret",
		"--admin-password=test-password",
		"--allow-insecure=true",
	})
	if err != nil {
		t.Fatalf("expected double-dash long flags to parse, got %v (%s)", err, errOut.String())
	}

	if got := *flags.jwtSecret; got != "test-secret" {
		t.Fatalf("expected jwt secret %q, got %q", "test-secret", got)
	}
	if got := *flags.adminPassword; got != "test-password" {
		t.Fatalf("expected admin password %q, got %q", "test-password", got)
	}
	if !*flags.allowInsecure {
		t.Fatalf("expected allow-insecure to be true")
	}
}

func TestRegisterFlags_AcceptsEnableMCPFlag(t *testing.T) {
	fs := flag.NewFlagSet("leafwiki", flag.ContinueOnError)
	var errOut bytes.Buffer
	fs.SetOutput(&errOut)
	flags := registerFlags(fs)

	err := fs.Parse([]string{"--enable-mcp=true"})
	if err != nil {
		t.Fatalf("expected enable-mcp flag to parse, got %v (%s)", err, errOut.String())
	}

	if flags.enableMCP == nil || !*flags.enableMCP {
		t.Fatalf("expected enable-mcp to be true")
	}
}

func TestRegisterFlags_AcceptsMCPStdioFlag(t *testing.T) {
	fs := flag.NewFlagSet("leafwiki", flag.ContinueOnError)
	var errOut bytes.Buffer
	fs.SetOutput(&errOut)
	registerFlags(fs)

	if err := fs.Parse([]string{"--mcp-stdio=true"}); err != nil {
		t.Fatalf("expected mcp-stdio flag to parse, got %v (%s)", err, errOut.String())
	}
}

func TestRegisterFlags_AcceptsRootDirFlag(t *testing.T) {
	fs := flag.NewFlagSet("leafwiki", flag.ContinueOnError)
	var errOut bytes.Buffer
	fs.SetOutput(&errOut)
	flags := registerFlags(fs)

	err := fs.Parse([]string{"--root-dir=/tmp/leafwiki-content"})
	if err != nil {
		t.Fatalf("expected root-dir flag to parse, got %v (%s)", err, errOut.String())
	}

	if flags.rootDir == nil || *flags.rootDir != "/tmp/leafwiki-content" {
		t.Fatalf("expected root-dir to be parsed, got %#v", flags.rootDir)
	}
}

func TestRegisterFlags_AcceptsLoggingFlags(t *testing.T) {
	fs := flag.NewFlagSet("leafwiki", flag.ContinueOnError)
	var errOut bytes.Buffer
	fs.SetOutput(&errOut)
	flags := registerFlags(fs)

	err := fs.Parse([]string{
		"--log-target=stderr",
		"--log-file=logs/custom.log",
	})
	if err != nil {
		t.Fatalf("expected logging flags to parse, got %v (%s)", err, errOut.String())
	}

	if flags.logTarget == nil || *flags.logTarget != "stderr" {
		t.Fatalf("expected log-target stderr, got %#v", flags.logTarget)
	}
	if flags.logFile == nil || *flags.logFile != "logs/custom.log" {
		t.Fatalf("expected log-file logs/custom.log, got %#v", flags.logFile)
	}
}

func resolveMCPTransportsForArgs(t *testing.T, args []string) (mcpTransports, error) {
	t.Helper()

	fs := flag.NewFlagSet("leafwiki", flag.ContinueOnError)
	var errOut bytes.Buffer
	fs.SetOutput(&errOut)
	flags := registerFlags(fs)
	if err := fs.Parse(args); err != nil {
		t.Fatalf("parse flags: %v (%s)", err, errOut.String())
	}
	visited := map[string]bool{}
	fs.Visit(func(f *flag.Flag) { visited[f.Name] = true })
	return resolveMCPTransports(flags, visited)
}

func resolveWorkspaceForArgs(t *testing.T, args []string) wiki.Workspace {
	t.Helper()

	fs := flag.NewFlagSet("leafwiki", flag.ContinueOnError)
	var errOut bytes.Buffer
	fs.SetOutput(&errOut)
	flags := registerFlags(fs)
	if err := fs.Parse(args); err != nil {
		t.Fatalf("parse flags: %v (%s)", err, errOut.String())
	}
	visited := map[string]bool{}
	fs.Visit(func(f *flag.Flag) { visited[f.Name] = true })
	workspace, err := resolveWorkspace(flags, visited)
	if err != nil {
		t.Fatalf("resolveWorkspace: %v", err)
	}
	return workspace
}

func resolveLoggingConfigForArgs(t *testing.T, args []string) leaflogging.Config {
	t.Helper()

	cfg, err := resolveLoggingConfigForArgsAllowError(t, args)
	if err != nil {
		t.Fatalf("resolveLoggingConfig: %v", err)
	}
	return cfg
}

func resolveLoggingConfigForArgsAllowError(t *testing.T, args []string) (leaflogging.Config, error) {
	t.Helper()

	fs := flag.NewFlagSet("leafwiki", flag.ContinueOnError)
	var errOut bytes.Buffer
	fs.SetOutput(&errOut)
	flags := registerFlags(fs)
	if err := fs.Parse(args); err != nil {
		t.Fatalf("parse flags: %v (%s)", err, errOut.String())
	}
	visited := map[string]bool{}
	fs.Visit(func(f *flag.Flag) { visited[f.Name] = true })
	workspace, err := resolveWorkspace(flags, visited)
	if err != nil {
		t.Fatalf("resolveWorkspace: %v", err)
	}
	return resolveLoggingConfig(flags, visited, workspace.DataDir)
}

func TestLeafWikiHelperProcess(t *testing.T) {
	if os.Getenv("GO_WANT_LEAFWIKI_HELPER_PROCESS") != "1" {
		return
	}

	args := []string{}
	for i, arg := range os.Args {
		if arg == "--" {
			args = os.Args[i+1:]
			break
		}
	}
	os.Args = append([]string{"leafwiki"}, args...)
	flag.CommandLine = flag.NewFlagSet(os.Args[0], flag.ExitOnError)
	main()
	os.Exit(0)
}

type leafwikiHelperProcess struct {
	cmd        *exec.Cmd
	cancel     context.CancelFunc
	stdoutPath string
	stderrPath string
	ready      bool
	stopped    bool
}

func startLeafwikiHelper(t *testing.T, args []string, env map[string]string) *leafwikiHelperProcess {
	return startLeafwikiHelperWithStdin(t, args, env, nil)
}

func startLeafwikiHelperWithStdin(t *testing.T, args []string, env map[string]string, stdin io.Reader) *leafwikiHelperProcess {
	t.Helper()

	ctx, cancel := context.WithCancel(context.Background())
	stdoutPath := filepath.Join(t.TempDir(), "leafwiki.stdout")
	stderrPath := filepath.Join(t.TempDir(), "leafwiki.stderr")
	stdout, err := os.Create(stdoutPath)
	if err != nil {
		t.Fatalf("create stdout file: %v", err)
	}
	stderr, err := os.Create(stderrPath)
	if err != nil {
		t.Fatalf("create stderr file: %v", err)
	}

	cmdArgs := append([]string{"-test.run=TestLeafWikiHelperProcess", "--"}, args...)
	cmd := exec.CommandContext(ctx, os.Args[0], cmdArgs...)
	cmd.Env = leafwikiHelperEnv(env)
	cmd.Stdin = stdin
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	if err := cmd.Start(); err != nil {
		cancel()
		_ = stdout.Close()
		_ = stderr.Close()
		t.Fatalf("start leafwiki helper: %v", err)
	}
	if err := stdout.Close(); err != nil {
		t.Fatalf("close parent stdout file: %v", err)
	}
	if err := stderr.Close(); err != nil {
		t.Fatalf("close parent stderr file: %v", err)
	}

	proc := &leafwikiHelperProcess{
		cmd:        cmd,
		cancel:     cancel,
		stdoutPath: stdoutPath,
		stderrPath: stderrPath,
	}
	t.Cleanup(func() {
		proc.stop(t)
	})
	return proc
}

func startLeafwikiHelperWithStdinPipe(t *testing.T, args []string, env map[string]string) (*leafwikiHelperProcess, io.WriteCloser) {
	t.Helper()

	ctx, cancel := context.WithCancel(context.Background())
	stdoutPath := filepath.Join(t.TempDir(), "leafwiki.stdout")
	stderrPath := filepath.Join(t.TempDir(), "leafwiki.stderr")
	stdout, err := os.Create(stdoutPath)
	if err != nil {
		t.Fatalf("create stdout file: %v", err)
	}
	stderr, err := os.Create(stderrPath)
	if err != nil {
		t.Fatalf("create stderr file: %v", err)
	}

	cmdArgs := append([]string{"-test.run=TestLeafWikiHelperProcess", "--"}, args...)
	cmd := exec.CommandContext(ctx, os.Args[0], cmdArgs...)
	cmd.Env = leafwikiHelperEnv(env)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		cancel()
		_ = stdout.Close()
		_ = stderr.Close()
		t.Fatalf("create stdin pipe: %v", err)
	}
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	if err := cmd.Start(); err != nil {
		cancel()
		_ = stdout.Close()
		_ = stderr.Close()
		_ = stdin.Close()
		t.Fatalf("start leafwiki helper: %v", err)
	}
	if err := stdout.Close(); err != nil {
		t.Fatalf("close parent stdout file: %v", err)
	}
	if err := stderr.Close(); err != nil {
		t.Fatalf("close parent stderr file: %v", err)
	}

	proc := &leafwikiHelperProcess{
		cmd:        cmd,
		cancel:     cancel,
		stdoutPath: stdoutPath,
		stderrPath: stderrPath,
	}
	t.Cleanup(func() {
		_ = stdin.Close()
		proc.stop(t)
	})
	return proc, stdin
}

func (p *leafwikiHelperProcess) stop(t *testing.T) {
	t.Helper()
	if p.stopped {
		return
	}
	p.stopped = true
	p.cancel()
	err := p.cmd.Wait()
	if err == nil {
		return
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) && p.ready && exitErr.ProcessState.ExitCode() == -1 {
		return
	}
	if errors.Is(err, context.Canceled) {
		return
	}
	t.Fatalf("wait leafwiki helper: %v", err)
}

func (p *leafwikiHelperProcess) waitForExit(t *testing.T) {
	t.Helper()
	if p.stopped {
		return
	}
	done := make(chan error, 1)
	go func() {
		done <- p.cmd.Wait()
	}()
	select {
	case err := <-done:
		p.stopped = true
		p.cancel()
		if err != nil {
			t.Fatalf("wait leafwiki helper exit: %v\nstdout:\n%s\nstderr:\n%s", err, readFileString(t, p.stdoutPath), readFileString(t, p.stderrPath))
		}
	case <-time.After(15 * time.Second):
		t.Fatalf("leafwiki helper did not exit\nstdout:\n%s\nstderr:\n%s", readFileString(t, p.stdoutPath), readFileString(t, p.stderrPath))
	}
}

func runLeafwikiHelper(t *testing.T, args []string, env map[string]string) (string, string, error) {
	t.Helper()

	return runLeafwikiHelperWithTimeout(t, args, env, 30*time.Second)
}

func runLeafwikiHelperWithTimeout(t *testing.T, args []string, env map[string]string, timeout time.Duration) (string, string, error) {
	t.Helper()

	cmdArgs := append([]string{"-test.run=TestLeafWikiHelperProcess", "--"}, args...)
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, os.Args[0], cmdArgs...)
	cmd.Env = leafwikiHelperEnv(env)
	cmd.Stdin = strings.NewReader("")
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		err = ctx.Err()
	}
	return stdout.String(), stderr.String(), err
}

func leafwikiHelperEnv(overrides map[string]string) []string {
	env := []string{}
	for _, entry := range os.Environ() {
		if strings.HasPrefix(entry, "LEAFWIKI_") || strings.HasPrefix(entry, "GO_WANT_LEAFWIKI_HELPER_PROCESS=") {
			continue
		}
		env = append(env, entry)
	}
	env = append(env, "GO_WANT_LEAFWIKI_HELPER_PROCESS=1")
	for key, value := range overrides {
		env = append(env, key+"="+value)
	}
	return env
}

func waitForFileContaining(t *testing.T, path string, want string) {
	t.Helper()

	deadline := time.Now().Add(10 * time.Second)
	var last string
	for time.Now().Before(deadline) {
		raw, err := os.ReadFile(path)
		if err == nil {
			last = string(raw)
			if strings.Contains(last, want) {
				return
			}
		} else if !errors.Is(err, os.ErrNotExist) {
			last = err.Error()
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatalf("%s did not contain %q before timeout; last content/error: %q", path, want, last)
}

func waitForLeafwikiReady(t *testing.T, proc *leafwikiHelperProcess, port string) {
	t.Helper()

	client := &http.Client{Timeout: 200 * time.Millisecond}
	url := "http://127.0.0.1:" + port + "/api/health"
	deadline := time.Now().Add(10 * time.Second)
	var lastErr error
	for time.Now().Before(deadline) {
		resp, err := client.Get(url)
		if err == nil {
			_, _ = io.Copy(io.Discard, resp.Body)
			_ = resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				proc.ready = true
				return
			}
			lastErr = errors.New(resp.Status)
		} else {
			lastErr = err
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatalf("LeafWiki did not become ready at %s: %v\nstdout:\n%s\nstderr:\n%s", url, lastErr, readFileString(t, proc.stdoutPath), readFileString(t, proc.stderrPath))
}

func waitForLeafwikiUnavailable(t *testing.T, port string) {
	t.Helper()

	client := &http.Client{Timeout: 200 * time.Millisecond}
	url := "http://127.0.0.1:" + port + "/api/health"
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		resp, err := client.Get(url)
		if err != nil {
			return
		}
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatalf("LeafWiki stayed reachable at %s after shutdown", url)
}

func listProcessHTTPMCPToolNames(t *testing.T, endpoint string) []string {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	client := sdkmcp.NewClient(&sdkmcp.Implementation{Name: "leafwiki-main-test", Version: "test"}, nil)
	session, err := client.Connect(ctx, &sdkmcp.StreamableClientTransport{
		Endpoint:             endpoint,
		HTTPClient:           &http.Client{Timeout: 5 * time.Second},
		DisableStandaloneSSE: true,
	}, nil)
	if err != nil {
		t.Fatalf("connect HTTP MCP client: %v", err)
	}
	defer session.Close()

	var names []string
	cursor := ""
	for {
		result, err := session.ListTools(ctx, &sdkmcp.ListToolsParams{Cursor: cursor})
		if err != nil {
			t.Fatalf("list HTTP MCP tools: %v", err)
		}
		for _, tool := range result.Tools {
			names = append(names, tool.Name)
		}
		if result.NextCursor == "" {
			break
		}
		cursor = result.NextCursor
	}
	sort.Strings(names)
	return names
}

func assertToolNamesMatch(t *testing.T, got []string, want []string) {
	t.Helper()

	sortedWant := append([]string{}, want...)
	sort.Strings(sortedWant)
	if strings.Join(got, "\n") != strings.Join(sortedWant, "\n") {
		t.Fatalf("tool names mismatch\n got:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(sortedWant, "\n"))
	}
}

func freeTCPPort(t *testing.T) string {
	t.Helper()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("find free port: %v", err)
	}
	defer func() {
		if err := listener.Close(); err != nil {
			t.Fatalf("close free port listener: %v", err)
		}
	}()
	return fmt.Sprintf("%d", listener.Addr().(*net.TCPAddr).Port)
}

func readFileString(t *testing.T, path string) string {
	t.Helper()

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(raw)
}

func assertJSONLogContains(t *testing.T, path string, msg string) map[string]any {
	t.Helper()

	for _, line := range strings.Split(strings.TrimSpace(readFileString(t, path)), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var entry map[string]any
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			t.Fatalf("log line is not JSON: %v\n%s", err, line)
		}
		if entry["msg"] == msg {
			for _, key := range []string{"time", "level", "msg", "source"} {
				if _, ok := entry[key]; !ok {
					t.Fatalf("log entry missing %q: %#v", key, entry)
				}
			}
			return entry
		}
	}
	t.Fatalf("log file %s did not contain msg %q", path, msg)
	return nil
}

func initAdminUser(t *testing.T, dataDir string) {
	t.Helper()

	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		t.Fatalf("create data dir: %v", err)
	}
	store, err := coreauth.NewUserStore(dataDir)
	if err != nil {
		t.Fatalf("create user store: %v", err)
	}
	defer func() {
		if err := store.Close(); err != nil {
			t.Fatalf("close user store: %v", err)
		}
	}()

	service := coreauth.NewUserService(store)
	if err := service.InitDefaultAdmin("old-password"); err != nil {
		t.Fatalf("init admin user: %v", err)
	}
}
