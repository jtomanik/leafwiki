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
	"strings"
	"testing"
	"time"

	coreauth "github.com/perber/wiki/internal/core/auth"
	leaflogging "github.com/perber/wiki/internal/logging"
	"github.com/perber/wiki/internal/wiki"
)

func TestWriteUsage_UsesLongFlags(t *testing.T) {
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
		"--enable-mcp",
		"LEAFWIKI_ROOT_DIR",
		"LEAFWIKI_LOG_TARGET",
		"LEAFWIKI_LOG_FILE",
		"LEAFWIKI_ENABLE_MCP",
	} {
		if !strings.Contains(output, expected) {
			t.Fatalf("expected usage output to contain %q, got %q", expected, output)
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

func TestValidateMCPStartupOptions_RequiresLoopbackHost(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		enableMCP   bool
		disableAuth bool
		remoteUser  bool
		host        string
		wantErr     bool
	}{
		{name: "disabled MCP ignores host/auth", enableMCP: false, disableAuth: false, host: "0.0.0.0"},
		{name: "MCP allows normal auth on loopback", enableMCP: true, disableAuth: false, host: "127.0.0.1"},
		{name: "MCP allows legacy disabled auth on loopback", enableMCP: true, disableAuth: true, host: "127.0.0.1"},
		{name: "MCP rejects wildcard host", enableMCP: true, disableAuth: true, host: "0.0.0.0", wantErr: true},
		{name: "MCP allows remote user middleware on loopback", enableMCP: true, disableAuth: false, remoteUser: true, host: "127.0.0.1"},
		{name: "MCP allows localhost", enableMCP: true, disableAuth: true, host: "localhost"},
		{name: "MCP allows IPv4 loopback", enableMCP: true, disableAuth: true, host: "127.0.0.1"},
		{name: "MCP allows IPv6 loopback", enableMCP: true, disableAuth: true, host: "::1"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateLocalMCPOptions(localMCPOptions{
				EnableMCP:        tt.enableMCP,
				DisableAuth:      tt.disableAuth,
				HTTPRemoteUserOn: tt.remoteUser,
				Host:             tt.host,
			})
			if (err != nil) != tt.wantErr {
				t.Fatalf("validateLocalMCPOptions() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestResolveLocalMCPOptions_UsesFlagEnvPrecedenceBeforeValidation(t *testing.T) {
	t.Setenv("LEAFWIKI_ENABLE_MCP", "true")
	t.Setenv("LEAFWIKI_DISABLE_AUTH", "true")
	t.Setenv("LEAFWIKI_HOST", "0.0.0.0")

	opts := resolveLocalMCPOptionsForArgs(t, nil)
	if !opts.EnableMCP || !opts.DisableAuth || opts.Host != "0.0.0.0" {
		t.Fatalf("resolved MCP opts from env = %#v, want env-enabled MCP on wildcard host", opts)
	}
	if err := validateLocalMCPOptions(opts); err == nil || !strings.Contains(err.Error(), "loopback") {
		t.Fatalf("validate env-resolved MCP opts = %v, want loopback error", err)
	}

	opts = resolveLocalMCPOptionsForArgs(t, []string{"--host=127.0.0.1"})
	if opts.Host != "127.0.0.1" {
		t.Fatalf("CLI host did not override env host: %#v", opts)
	}
	if err := validateLocalMCPOptions(opts); err != nil {
		t.Fatalf("validate CLI-overridden MCP opts: %v", err)
	}
}

func TestResolveLocalMCPOptions_EnvRemoteUserCombinationIsAllowed(t *testing.T) {
	t.Setenv("LEAFWIKI_ENABLE_MCP", "true")
	t.Setenv("LEAFWIKI_DISABLE_AUTH", "false")
	t.Setenv("LEAFWIKI_HOST", "127.0.0.1")
	t.Setenv("LEAFWIKI_ENABLE_HTTP_REMOTE_USER", "true")

	opts := resolveLocalMCPOptionsForArgs(t, nil)
	if !opts.HTTPRemoteUserOn {
		t.Fatalf("resolved MCP opts = %#v, want remote-user enabled from env", opts)
	}
	if err := validateLocalMCPOptions(opts); err != nil {
		t.Fatalf("validate env-resolved MCP opts with remote-user auth: %v", err)
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

func resolveLocalMCPOptionsForArgs(t *testing.T, args []string) localMCPOptions {
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
	return resolveLocalMCPOptions(flags, visited)
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

func runLeafwikiHelper(t *testing.T, args []string, env map[string]string) (string, string, error) {
	t.Helper()

	cmdArgs := append([]string{"-test.run=TestLeafWikiHelperProcess", "--"}, args...)
	cmd := exec.Command(os.Args[0], cmdArgs...)
	cmd.Env = leafwikiHelperEnv(env)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
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
