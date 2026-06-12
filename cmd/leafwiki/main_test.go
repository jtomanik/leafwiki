package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"syscall"
	"testing"
	"time"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/perber/wiki/internal/agenthooks"
	coreauth "github.com/perber/wiki/internal/core/auth"
	"github.com/perber/wiki/internal/locking"
	leaflogging "github.com/perber/wiki/internal/logging"
	"github.com/perber/wiki/internal/projectdaemon"
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
		"--enable-workspace-sync",
		"--mcp",
		"--api-key",
		"--config",
		"leafwiki agent-hook <codex|claude|cursor|unknown>",
		"LEAFWIKI_ROOT_DIR",
		"LEAFWIKI_LOG_TARGET",
		"LEAFWIKI_LOG_FILE",
		"LEAFWIKI_ENABLE_WORKSPACE_SYNC",
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

func TestRegisterFlagsParsesEnableWorkspaceSync(t *testing.T) {
	fs := flag.NewFlagSet("leafwiki-test", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	flags := registerFlags(fs)

	if err := fs.Parse([]string{"--enable-workspace-sync"}); err != nil {
		t.Fatalf("parse --enable-workspace-sync: %v", err)
	}

	if !*flags.enableWorkspaceSync {
		t.Fatalf("enableWorkspaceSync = false, want true")
	}
}

func TestResolveBoolUsesWorkspaceSyncEnvironmentWhenFlagAbsent(t *testing.T) {
	t.Setenv("LEAFWIKI_ENABLE_WORKSPACE_SYNC", "true")

	got := resolveBool("enable-workspace-sync", false, map[string]bool{}, "LEAFWIKI_ENABLE_WORKSPACE_SYNC")

	if !got {
		t.Fatalf("enableWorkspaceSync from env = false, want true")
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

func TestMainProcess_ConfigYAMLValueOverridesEnvironment(t *testing.T) {
	baseDir := t.TempDir()
	dataDir := filepath.Join(baseDir, "data")
	rootDir := filepath.Join(baseDir, "content")
	port := freeTCPPort(t)
	configPath := filepath.Join(baseDir, "leafwiki.yml")
	writeTestConfig(t, configPath, fmt.Sprintf(`disable-auth: true
data-dir: %s
root-dir: %s
host: 127.0.0.1
port: %s
log-target: stderr
`, dataDir, rootDir, port))
	proc := startLeafwikiHelper(t, []string{"--config", configPath}, map[string]string{
		"LEAFWIKI_PORT": "1",
	})

	waitForLeafwikiReady(t, proc, port)
	proc.stop(t)
}

func TestApplyYAMLConfigFile_ResolutionPrecedenceAndExplicitScalars(t *testing.T) {
	t.Setenv("LEAFWIKI_PORT", "9999")
	t.Setenv("LEAFWIKI_HOST", "0.0.0.0")
	t.Setenv("LEAFWIKI_BASE_PATH", "/wiki")
	t.Setenv("LEAFWIKI_PUBLIC_ACCESS", "true")
	t.Setenv("LEAFWIKI_MAX_REVISION_HISTORY", "100")

	configPath := filepath.Join(t.TempDir(), "leafwiki.yml")
	writeTestConfig(t, configPath, `port: 8088
base-path: ""
public-access: false
max-revision-history: 0
`)
	flags, visited, _ := parseConfigFlagsForArgs(t, []string{"--config", configPath})

	if got := resolveString("port", *flags.port, visited, "LEAFWIKI_PORT", "8080"); got != "8088" {
		t.Fatalf("port = %q, want YAML value 8088", got)
	}
	if got := resolveString("host", *flags.host, visited, "LEAFWIKI_HOST", "127.0.0.1"); got != "0.0.0.0" {
		t.Fatalf("host = %q, want omitted YAML to use env", got)
	}
	if got := resolveString("data-dir", *flags.dataDir, visited, "LEAFWIKI_DATA_DIR", "./data"); got != "./data" {
		t.Fatalf("data-dir = %q, want default for omitted YAML/env key", got)
	}
	if got := resolveString("base-path", *flags.basePath, visited, "LEAFWIKI_BASE_PATH", ""); got != "" {
		t.Fatalf("base-path = %q, want explicit YAML empty string to override env", got)
	}
	if got := resolveBool("public-access", *flags.publicAccess, visited, "LEAFWIKI_PUBLIC_ACCESS"); got {
		t.Fatalf("public-access = true, want explicit YAML false to override env")
	}
	if got := resolveInt("max-revision-history", *flags.maxRevisionHistory, visited, "LEAFWIKI_MAX_REVISION_HISTORY", 100); got != 0 {
		t.Fatalf("max-revision-history = %d, want explicit YAML zero to override env", got)
	}
}

func TestConfigFileFlagNamesCoverPublicRegisteredFlags(t *testing.T) {
	fs := flag.NewFlagSet("leafwiki", flag.ContinueOnError)
	registerFlags(fs)

	excluded := map[string]bool{
		"config":                  true,
		"enable-mcp":              true,
		"internal-project-daemon": true,
		"mcp-stdio":               true,
	}
	allowed := configFileFlagNames()
	for name := range excluded {
		if _, ok := allowed[name]; ok {
			t.Fatalf("configFileFlagNames includes excluded flag %q", name)
		}
	}

	var missing []string
	fs.VisitAll(func(f *flag.Flag) {
		if excluded[f.Name] {
			return
		}
		if _, ok := allowed[f.Name]; !ok {
			missing = append(missing, f.Name)
		}
	})

	var extra []string
	for name := range allowed {
		if fs.Lookup(name) == nil {
			extra = append(extra, name)
		}
	}
	sort.Strings(missing)
	sort.Strings(extra)
	if len(missing) > 0 || len(extra) > 0 {
		t.Fatalf("configFileFlagNames mismatch: missing=%v extra=%v", missing, extra)
	}
}

func TestApplyYAMLConfigFile_AcceptsQuotedScalarCoercions(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "leafwiki.yml")
	writeTestConfig(t, configPath, `public-access: "false"
allow-insecure: "true"
max-revision-history: "0"
access-token-timeout: "30m"
`)

	flags, visited, _ := parseConfigFlagsForArgs(t, []string{"--config", configPath})

	if got := resolveBool("public-access", *flags.publicAccess, visited, "LEAFWIKI_PUBLIC_ACCESS"); got {
		t.Fatalf("public-access = true, want quoted YAML false")
	}
	if got := resolveBool("allow-insecure", *flags.allowInsecure, visited, "LEAFWIKI_ALLOW_INSECURE"); !got {
		t.Fatalf("allow-insecure = false, want quoted YAML true")
	}
	if got := resolveInt("max-revision-history", *flags.maxRevisionHistory, visited, "LEAFWIKI_MAX_REVISION_HISTORY", 100); got != 0 {
		t.Fatalf("max-revision-history = %d, want quoted YAML zero", got)
	}
	if got := resolveDuration("access-token-timeout", *flags.accessTokenTimeout, visited, "LEAFWIKI_ACCESS_TOKEN_TIMEOUT"); got != 30*time.Minute {
		t.Fatalf("access-token-timeout = %s, want quoted YAML 30m", got)
	}
}

func TestApplyYAMLConfigFile_RejectsInvalidKeysAndValues(t *testing.T) {
	tests := []struct {
		name      string
		yaml      string
		wantError string
	}{
		{name: "unknown key", yaml: "unknown-option: true\n", wantError: `unknown --config key "unknown-option"`},
		{name: "duplicate key", yaml: "port: 8080\nport: 8081\n", wantError: `duplicate --config key "port"`},
		{name: "non scalar value", yaml: "trusted-proxy-ips:\n  - 127.0.0.1\n", wantError: `requires a non-null scalar value`},
		{name: "null value", yaml: "base-path: null\n", wantError: `requires a non-null scalar value`},
		{name: "hidden compatibility key", yaml: "enable-mcp: true\n", wantError: `unknown --config key "enable-mcp"`},
		{name: "internal key", yaml: "internal-project-daemon: /tmp/startup.json\n", wantError: `unknown --config key "internal-project-daemon"`},
		{name: "config key", yaml: "config: other.yml\n", wantError: `unknown --config key "config"`},
		{name: "mcp stdio compatibility key", yaml: "mcp-stdio: true\n", wantError: `unknown --config key "mcp-stdio"`},
		{name: "bad bool scalar", yaml: "public-access: maybe\n", wantError: `invalid --config value for "public-access"`},
		{name: "bad int scalar", yaml: "max-revision-history: many\n", wantError: `invalid --config value for "max-revision-history"`},
		{name: "bad duration scalar", yaml: "access-token-timeout: soon\n", wantError: `invalid --config value for "access-token-timeout"`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			configPath := filepath.Join(t.TempDir(), "leafwiki.yml")
			writeTestConfig(t, configPath, tt.yaml)

			_, _, _, err := parseConfigFlagsForArgsAllowError(t, []string{"--config", configPath})

			if err == nil || !strings.Contains(err.Error(), tt.wantError) {
				t.Fatalf("applyYAMLConfigFile error = %v, want %q", err, tt.wantError)
			}
		})
	}
}

func TestApplyYAMLConfigFile_RejectsConfigMixedWithNormalCLIFlag(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "leafwiki.yml")
	writeTestConfig(t, configPath, "port: 8080\n")

	_, _, _, err := parseConfigFlagsForArgsAllowError(t, []string{"--config", configPath, "--port", "8081"})

	if err == nil || !strings.Contains(err.Error(), "--config cannot be combined with --port") {
		t.Fatalf("applyYAMLConfigFile error = %v, want config/CLI mutual exclusion", err)
	}
}

func TestApplyYAMLConfigFile_RejectsConfigMixedWithSubcommandTrailingCLIFlag(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "leafwiki.yml")
	writeTestConfig(t, configPath, "data-dir: ./data\n")

	tests := []struct {
		name string
		args []string
		want string
	}{
		{
			name: "reset password trailing flag",
			args: []string{"--config", configPath, "reset-admin-password", "--data-dir", "other"},
			want: "--config cannot be combined with --data-dir",
		},
		{
			name: "agent hook trailing flag",
			args: []string{"--config", configPath, "agent-hook", "codex", "--data-dir", "other"},
			want: "--config cannot be combined with --data-dir",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, _, _, err := parseConfigFlagsForArgsAllowError(t, tt.args)

			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("applyYAMLConfigFile error = %v, want %q", err, tt.want)
			}
		})
	}
}

func TestMainProcess_ConfigPathValueNamedAgentHookDoesNotFailOpen(t *testing.T) {
	stdout, stderr, err := runLeafwikiHelperWithInputAndTimeout(t, []string{
		"--config", "agent-hook",
		"--not-a-real-flag",
	}, nil, `{"session_id":"should-not-be-hook"}`, 5*time.Second)

	if err == nil {
		t.Fatalf("config path plus invalid flag unexpectedly succeeded\nstdout:\n%s\nstderr:\n%s", stdout, stderr)
	}
	if stdout == "{}\n" {
		t.Fatalf("stdout = %q, want no agent-hook fail-open response", stdout)
	}
	if !strings.Contains(stderr, "not-a-real-flag") {
		t.Fatalf("stderr = %q, want invalid flag error", stderr)
	}
}

func TestMainProcess_ConfigAgentHookRejectsTrailingCLIFlagWithoutFailOpen(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "leafwiki.yml")
	writeTestConfig(t, configPath, "data-dir: ./data\n")
	payload := `{"hook_event_name":"SessionStart","session_id":"config-conflict-secret"}`

	stdout, stderr, err := runLeafwikiHelperWithInputAndTimeout(t, []string{
		"--config", configPath,
		"agent-hook", "codex",
		"--data-dir", "other",
	}, nil, payload, 5*time.Second)

	if err == nil {
		t.Fatalf("config mixed with trailing agent-hook flag unexpectedly succeeded\nstdout:\n%s\nstderr:\n%s", stdout, stderr)
	}
	if stdout == "{}\n" {
		t.Fatalf("stdout = %q, want no agent-hook fail-open response", stdout)
	}
	if !strings.Contains(stderr, "--config cannot be combined with --data-dir") {
		t.Fatalf("stderr = %q, want config/CLI mutual exclusion error", stderr)
	}
	if strings.Contains(stderr, "config-conflict-secret") {
		t.Fatalf("stderr leaked hook payload data: %s", stderr)
	}
}

func TestMainProcess_ConfigAgentHookRejectsTrailingCLIFlagBeforeReadingConfig(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "missing.yml")
	payload := `{"hook_event_name":"SessionStart","session_id":"missing-config-conflict-secret"}`

	stdout, stderr, err := runLeafwikiHelperWithInputAndTimeout(t, []string{
		"--config", configPath,
		"agent-hook", "codex",
		"--data-dir", "other",
	}, nil, payload, 5*time.Second)

	if err == nil {
		t.Fatalf("config mixed with trailing agent-hook flag unexpectedly succeeded\nstdout:\n%s\nstderr:\n%s", stdout, stderr)
	}
	if stdout == "{}\n" {
		t.Fatalf("stdout = %q, want no agent-hook fail-open response", stdout)
	}
	if !strings.Contains(stderr, "--config cannot be combined with --data-dir") {
		t.Fatalf("stderr = %q, want config/CLI mutual exclusion error", stderr)
	}
	if strings.Contains(stderr, "missing-config-conflict-secret") {
		t.Fatalf("stderr leaked hook payload data: %s", stderr)
	}
}

func TestMainProcess_ConfigAgentHookMissingConfigFileFailsOpen(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "missing.yml")
	payload := `{"hook_event_name":"SessionStart","session_id":"missing-config-secret"}`

	stdout, stderr, err := runLeafwikiHelperWithInputAndTimeout(t, []string{
		"--config", configPath,
		"agent-hook", "codex",
	}, nil, payload, 5*time.Second)

	if err != nil {
		t.Fatalf("agent-hook missing config file should fail open, got %v\nstdout:\n%s\nstderr:\n%s", err, stdout, stderr)
	}
	if stdout != "{}\n" {
		t.Fatalf("stdout = %q, want Codex allow response", stdout)
	}
	if strings.Contains(stdout, "missing-config-secret") || strings.Contains(stderr, "missing-config-secret") {
		t.Fatalf("hook output leaked payload secret\nstdout:\n%s\nstderr:\n%s", stdout, stderr)
	}
}

func TestMainProcess_ConfigAgentHookRejectsFlagLookingConfigPathWithoutFailOpen(t *testing.T) {
	payload := `{"hook_event_name":"SessionStart","session_id":"flag-looking-config-secret"}`

	stdout, stderr, err := runLeafwikiHelperWithInputAndTimeout(t, []string{
		"--config", "--data-dir",
		"agent-hook", "codex",
	}, nil, payload, 5*time.Second)

	if err == nil {
		t.Fatalf("flag-looking config path unexpectedly succeeded\nstdout:\n%s\nstderr:\n%s", stdout, stderr)
	}
	if stdout == "{}\n" {
		t.Fatalf("stdout = %q, want no agent-hook fail-open response", stdout)
	}
	if !strings.Contains(stderr, "--config requires a path") {
		t.Fatalf("stderr = %q, want config path-shape error", stderr)
	}
	if strings.Contains(stderr, "flag-looking-config-secret") {
		t.Fatalf("stderr leaked hook payload data: %s", stderr)
	}
}

func TestMainProcess_ConfigAgentHookRejectsDashPrefixedConfigPathWithoutFailOpen(t *testing.T) {
	baseDir := t.TempDir()
	configPath := filepath.Join(baseDir, "---config")
	writeTestConfig(t, configPath, fmt.Sprintf(`disable-auth: true
data-dir: %s
root-dir: %s
log-target: stderr
`, baseDir, baseDir))
	previousDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("get working directory: %v", err)
	}
	if err := os.Chdir(baseDir); err != nil {
		t.Fatalf("chdir temp dir: %v", err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(previousDir); err != nil {
			t.Fatalf("restore working directory: %v", err)
		}
	})
	payload := `{"hook_event_name":"SessionStart","session_id":"dash-prefixed-config-secret"}`

	stdout, stderr, err := runLeafwikiHelperWithInputAndTimeout(t, []string{
		"--config", "---config",
		"agent-hook", "codex",
	}, nil, payload, 5*time.Second)

	if err == nil {
		t.Fatalf("dash-prefixed config path unexpectedly succeeded\nstdout:\n%s\nstderr:\n%s", stdout, stderr)
	}
	if stdout == "{}\n" {
		t.Fatalf("stdout = %q, want no agent-hook fail-open response", stdout)
	}
	if !strings.Contains(stderr, "--config requires a path") {
		t.Fatalf("stderr = %q, want config path-shape error", stderr)
	}
	if strings.Contains(stderr, "dash-prefixed-config-secret") {
		t.Fatalf("stderr leaked hook payload data: %s", stderr)
	}
}

func TestMainProcess_ConfigAgentHookRejectsEmptyConfigPathWithoutFailOpen(t *testing.T) {
	tests := []struct {
		name string
		args []string
	}{
		{name: "inline empty", args: []string{"--config=", "agent-hook", "codex"}},
		{name: "separate empty", args: []string{"--config", "", "agent-hook", "codex"}},
		{name: "trailing bare after agent hook", args: []string{"agent-hook", "codex", "--config"}},
		{name: "inline empty before help after agent hook", args: []string{"agent-hook", "codex", "--config=", "--help"}},
		{name: "single dash", args: []string{"--config", "-", "agent-hook", "codex"}},
		{name: "double dash", args: []string{"--config", "--", "agent-hook", "codex"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			payload := `{"hook_event_name":"SessionStart","session_id":"empty-config-secret"}`

			stdout, stderr, err := runLeafwikiHelperWithInputAndTimeout(t, tt.args, nil, payload, 5*time.Second)

			if err == nil {
				t.Fatalf("empty config path unexpectedly succeeded\nstdout:\n%s\nstderr:\n%s", stdout, stderr)
			}
			if stdout == "{}\n" {
				t.Fatalf("stdout = %q, want no agent-hook fail-open response", stdout)
			}
			if !strings.Contains(stderr, "--config requires a path") {
				t.Fatalf("stderr = %q, want empty config path error", stderr)
			}
			if strings.Contains(stderr, "empty-config-secret") {
				t.Fatalf("stderr leaked hook payload data: %s", stderr)
			}
		})
	}
}

func TestMainProcess_ConfigAgentHookRejectsHelpMixWithoutFailOpen(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "leafwiki.yml")
	writeTestConfig(t, configPath, "data-dir: ./data\n")
	payload := `{"hook_event_name":"SessionStart","session_id":"config-help-secret"}`

	stdout, stderr, err := runLeafwikiHelperWithInputAndTimeout(t, []string{
		"agent-hook", "codex",
		"--config", configPath,
		"--help",
	}, nil, payload, 5*time.Second)

	if err == nil {
		t.Fatalf("config mixed with help unexpectedly succeeded\nstdout:\n%s\nstderr:\n%s", stdout, stderr)
	}
	if stdout == "{}\n" {
		t.Fatalf("stdout = %q, want no agent-hook fail-open response", stdout)
	}
	if !strings.Contains(stderr, "Invalid config arguments") {
		t.Fatalf("stderr = %q, want config argument error", stderr)
	}
	if strings.Contains(stderr, "config-help-secret") {
		t.Fatalf("stderr leaked hook payload data: %s", stderr)
	}
}

func TestMainProcess_ConfigRejectsPositionalHelp(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "leafwiki.yml")
	writeTestConfig(t, configPath, "data-dir: ./data\n")

	stdout, stderr, err := runLeafwikiHelper(t, []string{
		"--config", configPath,
		"help",
	}, nil)

	if err == nil {
		t.Fatalf("config mixed with positional help unexpectedly succeeded\nstdout:\n%s\nstderr:\n%s", stdout, stderr)
	}
	if strings.Contains(stdout, "Usage:") {
		t.Fatalf("stdout = %q, want no successful usage output", stdout)
	}
	if !strings.Contains(stderr, "Invalid config file") || !strings.Contains(stderr, "--config cannot be combined with help") {
		t.Fatalf("stderr = %q, want config/help mix error", stderr)
	}
}

func TestMainProcess_ConfigAgentHookRejectsUnknownFlagWithoutFailOpen(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "leafwiki.yml")
	writeTestConfig(t, configPath, "data-dir: ./data\n")
	payload := `{"hook_event_name":"SessionStart","session_id":"unknown-config-flag-secret"}`

	stdout, stderr, err := runLeafwikiHelperWithInputAndTimeout(t, []string{
		"--config", configPath,
		"--not-a-real-flag",
		"agent-hook", "codex",
	}, nil, payload, 5*time.Second)

	if err == nil {
		t.Fatalf("config mixed with unknown flag unexpectedly succeeded\nstdout:\n%s\nstderr:\n%s", stdout, stderr)
	}
	if stdout == "{}\n" {
		t.Fatalf("stdout = %q, want no agent-hook fail-open response", stdout)
	}
	if !strings.Contains(stderr, "not-a-real-flag") {
		t.Fatalf("stderr = %q, want unknown flag error", stderr)
	}
	if strings.Contains(stderr, "unknown-config-flag-secret") {
		t.Fatalf("stderr leaked hook payload data: %s", stderr)
	}
}

func TestMainProcess_AgentHookFailOpenUsesConfig(t *testing.T) {
	baseDir := t.TempDir()
	sameDir := filepath.Join(baseDir, "same")
	configPath := filepath.Join(baseDir, "leafwiki.yml")
	writeTestConfig(t, configPath, fmt.Sprintf(`disable-auth: true
data-dir: %s
root-dir: %s
log-target: stderr
`, sameDir, sameDir))
	payload := `{"hook_event_name":"SessionStart","session_id":"config-hook-secret"}`

	stdout, stderr, err := runLeafwikiHelperWithInputAndTimeout(t, []string{
		"--config", configPath,
		"agent-hook", "codex",
	}, nil, payload, 5*time.Second)

	if err != nil {
		t.Fatalf("agent-hook invalid config should fail open, got %v\nstdout:\n%s\nstderr:\n%s", err, stdout, stderr)
	}
	if stdout != "{}\n" {
		t.Fatalf("stdout = %q, want Codex allow response", stdout)
	}
	if strings.Contains(stderr, "config-hook-secret") {
		t.Fatalf("stderr leaked hook payload data: %s", stderr)
	}
}

func TestMainProcess_ResetAdminPasswordUsesConfigDataDir(t *testing.T) {
	baseDir := t.TempDir()
	dataDir := filepath.Join(baseDir, "data")
	initAdminUser(t, dataDir)
	configPath := filepath.Join(baseDir, "leafwiki.yml")
	writeTestConfig(t, configPath, fmt.Sprintf("data-dir: %s\n", dataDir))

	stdout, stderr, err := runLeafwikiHelper(t, []string{
		"--config", configPath,
		"reset-admin-password",
	}, nil)

	if err != nil {
		t.Fatalf("reset-admin-password process error = %v, stderr=%q", err, stderr)
	}
	if !strings.Contains(stdout, "Admin password reset successfully") {
		t.Fatalf("stdout = %q, want reset output", stdout)
	}
}

func TestMainProcess_ConfigPathDoesNotAffectDaemonIdentity(t *testing.T) {
	stdinReader, stdinWriter := io.Pipe()
	defer stdinWriter.Close()
	baseDir := t.TempDir()
	dataDir := filepath.Join(baseDir, "data")
	rootDir := filepath.Join(baseDir, "content")
	port := freeTCPPort(t)
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
	writeTestConfig(t, firstConfig, configBody)
	writeTestConfig(t, secondConfig, configBody)
	first := startLeafwikiHelperWithStdin(t, []string{"--config", firstConfig}, nil, stdinReader)
	waitForLeafwikiReady(t, first, port)

	stdout, stderr, err := runLeafwikiHelperWithTimeout(t, []string{"--config", secondConfig}, nil, 5*time.Second)

	if err != nil {
		t.Fatalf("second config path should attach and exit cleanly, got %v\nstdout:\n%s\nstderr:\n%s", err, stdout, stderr)
	}
	if stdout != "" {
		t.Fatalf("stdout = %q, want empty without MCP frames", stdout)
	}
	if strings.Contains(stderr, "project daemon config mismatch") {
		t.Fatalf("stderr = %q, want no config mismatch from config path", stderr)
	}
	if err := stdinWriter.Close(); err != nil {
		t.Fatalf("close stdin writer: %v", err)
	}
	first.waitForExit(t)
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

func TestMainProcess_PublicHTTPMCPRejectsNonLoopbackHost(t *testing.T) {
	stdout, stderr, err := runLeafwikiHelper(t, []string{
		"--mcp=http",
		"--disable-auth",
		"--host", "0.0.0.0",
		"--data-dir", filepath.Join(t.TempDir(), "data"),
		"--log-target", "stderr",
	}, nil)

	if err == nil {
		t.Fatalf("expected public HTTP MCP on a non-loopback host to exit non-zero")
	}
	if stdout != "" {
		t.Fatalf("stdout = %q, want empty", stdout)
	}
	if !strings.Contains(stderr, "MCP requires a loopback host") {
		t.Fatalf("stderr = %q, want public MCP loopback host error", stderr)
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

func TestMainProcess_NativeStdioSecondCompatibleStartupAttachesToProjectDaemon(t *testing.T) {
	stdinReader, stdinWriter := io.Pipe()
	defer stdinWriter.Close()
	baseDir := t.TempDir()
	dataDir := filepath.Join(baseDir, "data")
	rootDir := filepath.Join(baseDir, "content")
	port := freeTCPPort(t)
	first := startLeafwikiHelperWithStdin(t, []string{
		"--mcp=stdio",
		"--disable-auth",
		"--data-dir", dataDir,
		"--root-dir", rootDir,
		"--host", "127.0.0.1",
		"--port", port,
		"--log-target", "stderr",
	}, nil, stdinReader)
	waitForLeafwikiReady(t, first, port)

	stdout, stderr, err := runLeafwikiHelperWithTimeout(t, []string{
		"--mcp=stdio",
		"--disable-auth",
		"--data-dir", dataDir,
		"--root-dir", rootDir,
		"--host", "127.0.0.1",
		"--port", port,
		"--log-target", "stderr",
	}, nil, 5*time.Second)

	if err != nil {
		t.Fatalf("second compatible startup should attach and exit cleanly, got %v\nstdout:\n%s\nstderr:\n%s", err, stdout, stderr)
	}
	if stdout != "" {
		t.Fatalf("stdout = %q, want empty without MCP frames", stdout)
	}
	for _, unexpected := range []string{"data directory is already in use", "root directory is already in use", "bind: address already in use"} {
		if strings.Contains(stderr, unexpected) {
			t.Fatalf("stderr = %q, want no old ownership failure %q", stderr, unexpected)
		}
	}
	waitForLeafwikiReady(t, first, port)
}

func TestMainProcess_NativeStdioOnlyStartupAttachesToHTTPEnabledProjectDaemon(t *testing.T) {
	baseDir := t.TempDir()
	dataDir := filepath.Join(baseDir, "data")
	rootDir := filepath.Join(baseDir, "content")
	port := freeTCPPort(t)
	first := startLeafwikiHelper(t, []string{
		"--mcp=http",
		"--disable-auth",
		"--data-dir", dataDir,
		"--root-dir", rootDir,
		"--host", "127.0.0.1",
		"--port", port,
		"--log-target", "stderr",
	}, nil)
	waitForLeafwikiReady(t, first, port)

	stdout, stderr, err := runLeafwikiHelperWithTimeout(t, []string{
		"--mcp=stdio",
		"--disable-auth",
		"--data-dir", dataDir,
		"--root-dir", rootDir,
		"--host", "127.0.0.1",
		"--port", port,
		"--log-target", "file",
		"--disable-request-log",
	}, nil, 12*time.Second)

	if err != nil {
		t.Fatalf("stdio-only startup should attach to HTTP-enabled daemon, got %v\nstdout:\n%s\nstderr:\n%s", err, stdout, stderr)
	}
	if stdout != "" {
		t.Fatalf("stdout = %q, want empty without MCP frames", stdout)
	}
	if strings.Contains(stderr, "project daemon config mismatch") {
		t.Fatalf("stderr = %q, want no public MCP, logging, or request-log config mismatch", stderr)
	}
	toolNames := listProcessHTTPMCPToolNames(t, "http://127.0.0.1:"+port+"/mcp")
	assertToolNamesMatch(t, toolNames, wikimcp.BaseToolNames())
}

func TestMainProcess_NativeStdioOwnerStderrLoggingFallsBackToFile(t *testing.T) {
	stdinReader, stdinWriter := io.Pipe()
	defer stdinWriter.Close()
	baseDir := t.TempDir()
	dataDir := filepath.Join(baseDir, "data")
	rootDir := filepath.Join(baseDir, "content")
	port := freeTCPPort(t)
	proc := startLeafwikiHelperWithStdin(t, []string{
		"--mcp=stdio",
		"--disable-auth",
		"--data-dir", dataDir,
		"--root-dir", rootDir,
		"--host", "127.0.0.1",
		"--port", port,
		"--log-target", "stderr",
		"--daemon-idle-timeout", "0",
	}, nil, stdinReader)
	waitForLeafwikiReady(t, proc, port)

	logPath := filepath.Join(dataDir, ".leafwiki", "logs", "leafwiki.log")
	waitForFileContaining(t, logPath, "Starting LeafWiki")

	if err := stdinWriter.Close(); err != nil {
		t.Fatalf("close stdin writer: %v", err)
	}
	proc.waitForExit(t)
	if stdout := readFileString(t, proc.stdoutPath); stdout != "" {
		t.Fatalf("stdout = %q, want empty without MCP frames", stdout)
	}
}

func TestSpawnProjectDaemonOwnerRemovesSecretStartupConfigOnExecutableFailure(t *testing.T) {
	oldExecutable := projectDaemonExecutable
	t.Cleanup(func() {
		projectDaemonExecutable = oldExecutable
	})

	baseDir := t.TempDir()
	dataDir := filepath.Join(baseDir, "data")
	rootDir := filepath.Join(baseDir, "content")
	jwtSecret := fmt.Sprintf("cleanup-jwt-secret-%d", time.Now().UnixNano())
	adminPassword := "cleanup-admin-password"
	var startupPath string
	projectDaemonExecutable = func() (string, error) {
		startupPath = findLeafwikiDaemonStartupConfigContaining(t, jwtSecret)
		if startupPath == "" {
			t.Fatalf("startup config containing secret marker was not visible before executable lookup")
		}
		assertFileMode(t, startupPath, 0o600)
		return "", errors.New("forced executable failure")
	}

	cfg := testRuntimeConfig(dataDir, rootDir, freeTCPPort(t), mcpTransports{}, false)
	cfg.JWTSecret = jwtSecret
	cfg.AdminPassword = adminPassword
	_, err := spawnProjectDaemonOwner(cfg)

	if err == nil || !strings.Contains(err.Error(), "forced executable failure") {
		t.Fatalf("spawnProjectDaemonOwner error = %v, want forced executable failure", err)
	}
	if _, err := os.Stat(startupPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("startup config %s still exists after pre-start failure: %v", startupPath, err)
	}
}

func TestSpawnProjectDaemonOwnerEventuallyRemovesSecretStartupConfigWhenChildExitsBeforeRead(t *testing.T) {
	oldExecutable := projectDaemonExecutable
	oldCleanupDelay := projectDaemonStartupConfigPostStartCleanupDelay
	t.Cleanup(func() {
		projectDaemonExecutable = oldExecutable
		projectDaemonStartupConfigPostStartCleanupDelay = oldCleanupDelay
	})

	truePath, err := exec.LookPath("true")
	if err != nil {
		t.Skip("true executable not available")
	}
	baseDir := t.TempDir()
	dataDir := filepath.Join(baseDir, "data")
	rootDir := filepath.Join(baseDir, "content")
	jwtSecret := fmt.Sprintf("post-start-cleanup-jwt-%d", time.Now().UnixNano())
	var startupPath string
	projectDaemonExecutable = func() (string, error) {
		startupPath = findLeafwikiDaemonStartupConfigContaining(t, jwtSecret)
		if startupPath == "" {
			t.Fatalf("startup config containing secret marker was not visible before child start")
		}
		assertFileMode(t, startupPath, 0o600)
		return truePath, nil
	}
	projectDaemonStartupConfigPostStartCleanupDelay = 25 * time.Millisecond

	cfg := testRuntimeConfig(dataDir, rootDir, freeTCPPort(t), mcpTransports{}, false)
	cfg.JWTSecret = jwtSecret
	cfg.AdminPassword = "post-start-cleanup-admin"
	_, err = spawnProjectDaemonOwner(cfg)
	if err != nil {
		t.Fatalf("spawnProjectDaemonOwner failed: %v", err)
	}
	waitForFileRemoved(t, startupPath, 2*time.Second)
}

func TestMainProcess_NativeStdioAPIKeyAttachDoesNotRequireOwnerBootstrapSecrets(t *testing.T) {
	baseDir := t.TempDir()
	dataDir := filepath.Join(baseDir, "data")
	rootDir := filepath.Join(baseDir, "content")
	apiKey := createMCPAPIKey(t, dataDir)
	port := freeTCPPort(t)
	first := startLeafwikiHelper(t, []string{
		"--mcp=http",
		"--data-dir", dataDir,
		"--root-dir", rootDir,
		"--host", "127.0.0.1",
		"--port", port,
		"--jwt-secret", "owner-jwt-secret",
		"--admin-password", "owner-admin-password",
		"--allow-insecure",
		"--log-target", "stderr",
	}, nil)
	waitForLeafwikiReady(t, first, port)

	stdout, stderr, err := runLeafwikiHelperWithTimeout(t, []string{
		"--mcp=stdio",
		"--data-dir", dataDir,
		"--root-dir", rootDir,
		"--host", "127.0.0.1",
		"--port", port,
		"--allow-insecure",
		"--log-target", "stderr",
	}, map[string]string{"LEAFWIKI_MCP_API_KEY": apiKey}, 5*time.Second)

	if err != nil {
		t.Fatalf("stdio API-key startup should attach without owner bootstrap secrets, got %v\nstdout:\n%s\nstderr:\n%s", err, stdout, stderr)
	}
	if stdout != "" {
		t.Fatalf("stdout = %q, want empty without MCP frames", stdout)
	}
	for _, unexpected := range []string{"JWT secret is required", "admin password is required", "project daemon config mismatch"} {
		if strings.Contains(stderr, unexpected) {
			t.Fatalf("stderr = %q, want no bootstrap-secret attach failure %q", stderr, unexpected)
		}
	}
}

func TestMainProcess_StaleDescriptorIsReplacedWithoutSendingAPIKey(t *testing.T) {
	baseDir := t.TempDir()
	dataDir := filepath.Join(baseDir, "data")
	rootDir := filepath.Join(baseDir, "content")
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		t.Fatalf("create data dir: %v", err)
	}
	if err := os.MkdirAll(rootDir, 0o755); err != nil {
		t.Fatalf("create root dir: %v", err)
	}
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
	t.Cleanup(staleControl.Close)

	apiKey := createMCPAPIKey(t, dataDir)
	port := freeTCPPort(t)
	runtimeCfg := testRuntimeConfig(dataDir, rootDir, port, mcpTransports{Stdio: true}, false)
	runtimeCfg.JWTSecret = "owner-jwt-secret"
	runtimeCfg.AdminPassword = "owner-admin-password"
	ownerCfg, err := daemonRequestConfigForRuntime(runtimeCfg)
	if err != nil {
		t.Fatalf("daemonRequestConfigForRuntime: %v", err)
	}
	hash, err := projectdaemon.ConfigHash(ownerCfg)
	if err != nil {
		t.Fatalf("ConfigHash: %v", err)
	}
	descriptorPath := projectdaemon.DescriptorPath(ownerCfg.DataDir)
	if err := projectdaemon.WriteDescriptorAtomic(descriptorPath, &projectdaemon.Descriptor{
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
	}); err != nil {
		t.Fatalf("write stale descriptor: %v", err)
	}

	stdinReader, stdinWriter := io.Pipe()
	defer stdinWriter.Close()
	proc := startLeafwikiHelperWithStdin(t, []string{
		"--mcp=stdio",
		"--data-dir", dataDir,
		"--root-dir", rootDir,
		"--host", "127.0.0.1",
		"--port", port,
		"--allow-insecure",
		"--jwt-secret", "owner-jwt-secret",
		"--admin-password", "owner-admin-password",
		"--log-target", "stderr",
	}, map[string]string{"LEAFWIKI_MCP_API_KEY": apiKey}, stdinReader)
	waitForLeafwikiReady(t, proc, port)

	replaced := readFileString(t, descriptorPath)
	if strings.Contains(replaced, staleControl.URL) || strings.Contains(replaced, "stale-token") {
		t.Fatalf("descriptor was not replaced:\n%s", replaced)
	}
	select {
	case got := <-received:
		if strings.Contains(got, apiKey) {
			t.Fatalf("stale descriptor endpoint received API key: %q", got)
		}
		t.Fatalf("stale descriptor endpoint received request before replacement: %q", got)
	default:
	}
	if stdout := readFileString(t, proc.stdoutPath); strings.Contains(stdout, apiKey) {
		t.Fatalf("process stdout leaked API key: %q", stdout)
	}
	if stderr := readFileString(t, proc.stderrPath); strings.Contains(stderr, apiKey) {
		t.Fatalf("process stderr leaked API key: %q", stderr)
	}
	if err := stdinWriter.Close(); err != nil {
		t.Fatalf("close stdin writer: %v", err)
	}
	proc.waitForExit(t)
}

func TestMainProcess_UntrustedStaleDescriptorIsReplacedWhenLocksAreFree(t *testing.T) {
	tests := []struct {
		name  string
		setup func(t *testing.T, path string)
	}{
		{
			name: "corrupt json",
			setup: func(t *testing.T, path string) {
				t.Helper()
				if err := os.WriteFile(path, []byte("{"), 0o600); err != nil {
					t.Fatalf("write corrupt descriptor: %v", err)
				}
			},
		},
		{
			name: "wrong mode",
			setup: func(t *testing.T, path string) {
				t.Helper()
				if err := os.WriteFile(path, []byte("{}"), 0o644); err != nil {
					t.Fatalf("write wrong-mode descriptor: %v", err)
				}
			},
		},
		{
			name: "non regular path",
			setup: func(t *testing.T, path string) {
				t.Helper()
				if err := os.Mkdir(path, 0o700); err != nil {
					t.Fatalf("create descriptor directory: %v", err)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			baseDir := t.TempDir()
			dataDir := filepath.Join(baseDir, "data")
			rootDir := filepath.Join(baseDir, "content")
			descriptorPath := filepath.Join(dataDir, ".leafwiki", projectdaemon.DescriptorFileName)
			if err := os.MkdirAll(filepath.Dir(descriptorPath), 0o755); err != nil {
				t.Fatalf("create descriptor dir: %v", err)
			}
			if err := os.MkdirAll(rootDir, 0o755); err != nil {
				t.Fatalf("create root dir: %v", err)
			}
			tt.setup(t, descriptorPath)

			port := freeTCPPort(t)
			proc := startLeafwikiHelper(t, []string{
				"--disable-auth",
				"--data-dir", dataDir,
				"--root-dir", rootDir,
				"--host", "127.0.0.1",
				"--port", port,
				"--log-target", "stderr",
			}, nil)
			waitForLeafwikiReady(t, proc, port)

			desc, err := projectdaemon.ReadTrustedDescriptor(descriptorPath)
			if err != nil {
				t.Fatalf("replacement descriptor is not trusted: %v", err)
			}
			if desc.PID == 0 || desc.ControlURL == "" {
				t.Fatalf("replacement descriptor = %#v, want live daemon descriptor", desc)
			}
			info, err := os.Stat(descriptorPath)
			if err != nil {
				t.Fatalf("stat replacement descriptor: %v", err)
			}
			if got := info.Mode().Perm(); got != 0o600 {
				t.Fatalf("replacement descriptor mode = %v, want 0600", got)
			}
		})
	}
}

func TestReadHealthyProjectDaemonPreservesUntrustedDescriptorWhenAnyProjectLockIsHeld(t *testing.T) {
	tests := []struct {
		name string
		lock func(t *testing.T, dataDir string, rootDir string) func()
	}{
		{
			name: "data lock held",
			lock: func(t *testing.T, dataDir string, _ string) func() {
				t.Helper()
				lock, err := locking.AcquireDataDirLock(dataDir)
				if err != nil {
					t.Fatalf("acquire data lock: %v", err)
				}
				return func() { _ = lock.Release() }
			},
		},
		{
			name: "root lock held",
			lock: func(t *testing.T, _ string, rootDir string) func() {
				t.Helper()
				lock, err := locking.AcquireRootDirLock(rootDir)
				if err != nil {
					t.Fatalf("acquire root lock: %v", err)
				}
				return func() { _ = lock.Release() }
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			baseDir := t.TempDir()
			dataDir := filepath.Join(baseDir, "data")
			rootDir := filepath.Join(baseDir, "content")
			if err := os.MkdirAll(filepath.Join(dataDir, ".leafwiki"), 0o755); err != nil {
				t.Fatalf("create descriptor dir: %v", err)
			}
			if err := os.MkdirAll(rootDir, 0o755); err != nil {
				t.Fatalf("create root dir: %v", err)
			}
			canonicalData, canonicalRoot, err := projectdaemon.CanonicalizeProject(dataDir, rootDir)
			if err != nil {
				t.Fatalf("canonicalize project: %v", err)
			}
			descriptorPath := projectdaemon.DescriptorPath(canonicalData)
			if err := os.WriteFile(descriptorPath, []byte("{"), 0o600); err != nil {
				t.Fatalf("write corrupt descriptor: %v", err)
			}
			release := tt.lock(t, canonicalData, canonicalRoot)
			defer release()

			_, healthy, err := readHealthyProjectDaemon(context.Background(), descriptorPath, projectdaemon.Config{
				DataDir: canonicalData,
				RootDir: canonicalRoot,
			})

			if err == nil {
				t.Fatalf("readHealthyProjectDaemon err = nil, want untrusted descriptor error while a project lock is held")
			}
			if healthy {
				t.Fatalf("healthy = true, want false")
			}
			if _, statErr := os.Stat(descriptorPath); statErr != nil {
				t.Fatalf("descriptor was removed while a project lock was held: %v", statErr)
			}
		})
	}
}

func TestReadHealthyProjectDaemonPreservesTrustedDescriptorWhenLocksHeldButControlUnreachable(t *testing.T) {
	baseDir := t.TempDir()
	dataDir := filepath.Join(baseDir, "data")
	rootDir := filepath.Join(baseDir, "content")
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		t.Fatalf("create data dir: %v", err)
	}
	if err := os.MkdirAll(rootDir, 0o755); err != nil {
		t.Fatalf("create root dir: %v", err)
	}
	cfg := testRuntimeConfig(dataDir, rootDir, freeTCPPort(t), mcpTransports{}, true)
	ownerCfg, err := daemonConfigForRuntime(cfg)
	if err != nil {
		t.Fatalf("daemon config: %v", err)
	}
	hash, err := projectdaemon.ConfigHash(ownerCfg)
	if err != nil {
		t.Fatalf("config hash: %v", err)
	}
	descriptorPath := projectdaemon.DescriptorPath(ownerCfg.DataDir)
	if err := projectdaemon.WriteDescriptorAtomic(descriptorPath, &projectdaemon.Descriptor{
		SchemaVersion:    projectdaemon.DescriptorSchemaVersion,
		PID:              12345,
		StartedAt:        time.Now(),
		DataDir:          ownerCfg.DataDir,
		RootDir:          ownerCfg.RootDir,
		PublicURL:        "http://127.0.0.1:" + ownerCfg.Port,
		PublicMCPEnabled: ownerCfg.PublicMCPEnabled,
		ControlURL:       "http://127.0.0.1:" + freeTCPPort(t),
		ConfigHash:       hash,
		IdleTimeout:      ownerCfg.DaemonIdleTimeout,
		ControlToken:     "control-token",
		Config:           ownerCfg,
	}); err != nil {
		t.Fatalf("write descriptor: %v", err)
	}
	dataLock, err := locking.AcquireDataDirLock(ownerCfg.DataDir)
	if err != nil {
		t.Fatalf("acquire data lock: %v", err)
	}
	defer dataLock.Release()
	rootLock, err := locking.AcquireRootDirLock(ownerCfg.RootDir)
	if err != nil {
		t.Fatalf("acquire root lock: %v", err)
	}
	defer rootLock.Release()

	_, healthy, err := readHealthyProjectDaemon(context.Background(), descriptorPath, ownerCfg)

	if err == nil {
		t.Fatalf("readHealthyProjectDaemon err = nil, want control health error while project locks are held")
	}
	if healthy {
		t.Fatalf("healthy = true, want false")
	}
	lowerErr := strings.ToLower(err.Error())
	if !strings.Contains(lowerErr, "control") && !strings.Contains(lowerErr, "health") {
		t.Fatalf("readHealthyProjectDaemon error = %v, want control/health context", err)
	}
	if _, statErr := os.Stat(descriptorPath); statErr != nil {
		t.Fatalf("descriptor was removed while project locks were held: %v", statErr)
	}
}

func TestReadHealthyProjectDaemonPreservesTrustedUnsupportedSchemaDescriptorWhenProjectLockHeld(t *testing.T) {
	baseDir := t.TempDir()
	dataDir := filepath.Join(baseDir, "data")
	rootDir := filepath.Join(baseDir, "content")
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		t.Fatalf("create data dir: %v", err)
	}
	if err := os.MkdirAll(rootDir, 0o755); err != nil {
		t.Fatalf("create root dir: %v", err)
	}
	cfg := testRuntimeConfig(dataDir, rootDir, freeTCPPort(t), mcpTransports{}, true)
	ownerCfg, err := daemonConfigForRuntime(cfg)
	if err != nil {
		t.Fatalf("daemon config: %v", err)
	}
	hash, err := projectdaemon.ConfigHash(ownerCfg)
	if err != nil {
		t.Fatalf("config hash: %v", err)
	}
	descriptorPath := projectdaemon.DescriptorPath(ownerCfg.DataDir)
	if err := projectdaemon.WriteDescriptorAtomic(descriptorPath, &projectdaemon.Descriptor{
		SchemaVersion:    projectdaemon.DescriptorSchemaVersion + 1,
		PID:              12345,
		StartedAt:        time.Now(),
		DataDir:          ownerCfg.DataDir,
		RootDir:          ownerCfg.RootDir,
		PublicURL:        "http://127.0.0.1:" + ownerCfg.Port,
		PublicMCPEnabled: ownerCfg.PublicMCPEnabled,
		ControlURL:       "http://127.0.0.1:" + freeTCPPort(t),
		ConfigHash:       hash,
		IdleTimeout:      ownerCfg.DaemonIdleTimeout,
		ControlToken:     "control-token",
		Config:           ownerCfg,
	}); err != nil {
		t.Fatalf("write descriptor: %v", err)
	}
	dataLock, err := locking.AcquireDataDirLock(ownerCfg.DataDir)
	if err != nil {
		t.Fatalf("acquire data lock: %v", err)
	}
	defer dataLock.Release()

	_, healthy, err := readHealthyProjectDaemon(context.Background(), descriptorPath, ownerCfg)

	if err == nil {
		t.Fatalf("readHealthyProjectDaemon err = nil, want unsupported schema error while project lock is held")
	}
	if healthy {
		t.Fatalf("healthy = true, want false")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "schema") {
		t.Fatalf("readHealthyProjectDaemon error = %v, want schema context", err)
	}
	if _, statErr := os.Stat(descriptorPath); statErr != nil {
		t.Fatalf("descriptor was removed while project lock was held: %v", statErr)
	}
}

func TestMainProcess_PlainWebOwnerSupportsLaterPrivateStdioAttach(t *testing.T) {
	baseDir := t.TempDir()
	dataDir := filepath.Join(baseDir, "data")
	rootDir := filepath.Join(baseDir, "content")
	port := freeTCPPort(t)
	first := startLeafwikiHelper(t, []string{
		"--disable-auth",
		"--data-dir", dataDir,
		"--root-dir", rootDir,
		"--host", "127.0.0.1",
		"--port", port,
		"--log-target", "stderr",
	}, nil)
	waitForLeafwikiReady(t, first, port)

	stdout, stderr, err := runLeafwikiHelperWithTimeout(t, []string{
		"--mcp=stdio",
		"--disable-auth",
		"--data-dir", dataDir,
		"--root-dir", rootDir,
		"--host", "127.0.0.1",
		"--port", port,
		"--log-target", "stderr",
	}, nil, 5*time.Second)

	if err != nil {
		t.Fatalf("stdio startup should attach to plain web owner, got %v\nstdout:\n%s\nstderr:\n%s", err, stdout, stderr)
	}
	resp, err := http.Get("http://127.0.0.1:" + port + "/mcp")
	if err != nil {
		t.Fatalf("GET /mcp: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("/mcp status = %d, want %d", resp.StatusCode, http.StatusNotFound)
	}
}

func TestMainProcess_AgentPresenceControlStartsOwnerActivity(t *testing.T) {
	baseDir := t.TempDir()
	dataDir := filepath.Join(baseDir, "data")
	rootDir := filepath.Join(baseDir, "content")
	port := freeTCPPort(t)
	first := startLeafwikiHelper(t, []string{
		"--disable-auth",
		"--data-dir", dataDir,
		"--root-dir", rootDir,
		"--host", "127.0.0.1",
		"--port", port,
		"--log-target", "stderr",
	}, nil)
	desc := waitForProjectDaemonDescriptor(t, dataDir)
	client := projectdaemon.NewClient(desc.ControlURL, desc.ControlToken)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := client.RecordAgentPresence(ctx, agenthooks.Event{
		Provider:      agenthooks.ProviderCodex,
		SessionIDHash: agentHookSessionHash(agenthooks.ProviderCodex, "codex"),
		EventName:     "SessionStart",
		SeenAt:        time.Now(),
	}); err != nil {
		t.Fatalf("RecordAgentPresence failed: %v\nstderr:\n%s", err, readFileString(t, first.stderrPath))
	}
	sessions, err := client.ListAgentPresence(ctx)
	if err != nil {
		t.Fatalf("ListAgentPresence failed: %v", err)
	}
	if len(sessions) != 1 || sessions[0].SessionIDHash != agentHookSessionHash(agenthooks.ProviderCodex, "codex") {
		t.Fatalf("agent presence sessions = %#v, want recorded codex presence", sessions)
	}
	waitForLeafwikiReady(t, first, port)
}

func TestMainProcessAgentHookMalformedJSONFailsOpen(t *testing.T) {
	baseDir := t.TempDir()
	dataDir := filepath.Join(baseDir, "data")
	rootDir := filepath.Join(baseDir, "content")
	port := freeTCPPort(t)

	stdout, stderr, err := runLeafwikiHelperWithInputAndTimeout(t, []string{
		"agent-hook", "codex",
		"--disable-auth",
		"--data-dir", dataDir,
		"--root-dir", rootDir,
		"--host", "127.0.0.1",
		"--port", port,
		"--log-target", "stderr",
	}, nil, "{", 5*time.Second)

	if err != nil {
		t.Fatalf("agent-hook malformed JSON err = %v\nstdout:\n%s\nstderr:\n%s", err, stdout, stderr)
	}
	if stdout != "{}\n" {
		t.Fatalf("stdout = %q, want Codex allow response", stdout)
	}
	if strings.Contains(stderr, "{") {
		t.Fatalf("stderr leaked raw malformed payload: %s", stderr)
	}
	if _, err := os.Stat(projectdaemon.DescriptorPath(dataDir)); !os.IsNotExist(err) {
		t.Fatalf("descriptor err = %v, want no daemon descriptor for malformed hook", err)
	}
}

func TestMainProcessAgentHookStartsDaemonAndRecordsPresence(t *testing.T) {
	baseDir := t.TempDir()
	dataDir := filepath.Join(baseDir, "data")
	rootDir := filepath.Join(baseDir, "content")
	port := freeTCPPort(t)
	payload := `{"hook_event_name":"SessionStart","session_id":"raw-codex-session","model":"gpt-5.4","source":"startup","prompt":"private prompt"}`

	stdout, stderr, err := runLeafwikiHelperWithInputAndTimeout(t, []string{
		"agent-hook", "codex",
		"--disable-auth",
		"--data-dir", dataDir,
		"--root-dir", rootDir,
		"--host", "127.0.0.1",
		"--port", port,
		"--log-target", "stderr",
	}, nil, payload, 10*time.Second)

	if err != nil {
		t.Fatalf("agent-hook valid payload err = %v\nstdout:\n%s\nstderr:\n%s", err, stdout, stderr)
	}
	if stdout != "{}\n" {
		t.Fatalf("stdout = %q, want Codex allow response", stdout)
	}
	if strings.Contains(stderr, "raw-codex-session") || strings.Contains(stderr, "private prompt") {
		t.Fatalf("stderr leaked hook payload data: %s", stderr)
	}

	desc := waitForProjectDaemonDescriptor(t, dataDir)
	t.Cleanup(func() {
		terminateProjectDaemonProcess(t, desc.PID)
	})
	client := projectdaemon.NewClient(desc.ControlURL, desc.ControlToken)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	sessions, err := client.ListAgentPresence(ctx)
	if err != nil {
		t.Fatalf("ListAgentPresence failed: %v", err)
	}
	if len(sessions) != 1 {
		t.Fatalf("presence session count = %d, want 1: %#v", len(sessions), sessions)
	}
	if sessions[0].SessionIDHash != agentHookSessionHash(agenthooks.ProviderCodex, "raw-codex-session") {
		t.Fatalf("session hash = %q, want hash of raw session id", sessions[0].SessionIDHash)
	}
	if sessions[0].Provider != agenthooks.ProviderCodex || sessions[0].LastEvent != "SessionStart" || sessions[0].Model != "gpt-5.4" || sessions[0].Source != "startup" {
		t.Fatalf("presence session = %#v", sessions[0])
	}
	if strings.Contains(fmt.Sprintf("%#v", sessions[0]), "raw-codex-session") || strings.Contains(fmt.Sprintf("%#v", sessions[0]), "private prompt") {
		t.Fatalf("presence session leaked raw payload data: %#v", sessions[0])
	}
}

func TestMainProcessAgentHookReplacesStaleDescriptorAndFailsOpen(t *testing.T) {
	baseDir := t.TempDir()
	dataDir := filepath.Join(baseDir, "data")
	rootDir := filepath.Join(baseDir, "content")
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		t.Fatalf("create data dir: %v", err)
	}
	if err := os.MkdirAll(rootDir, 0o755); err != nil {
		t.Fatalf("create root dir: %v", err)
	}
	received := make(chan string, 4)
	staleControl := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		raw, _ := io.ReadAll(req.Body)
		select {
		case received <- req.URL.Path + " " + req.Header.Get(projectdaemon.ControlTokenHeader) + " " + string(raw):
		default:
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"ok":true}`)
	}))
	t.Cleanup(staleControl.Close)

	cfg := testRuntimeConfig(dataDir, rootDir, freeTCPPort(t), mcpTransports{}, true)
	ownerCfg, err := daemonRequestConfigForRuntime(cfg)
	if err != nil {
		t.Fatalf("daemonRequestConfigForRuntime: %v", err)
	}
	hash, err := projectdaemon.ConfigHash(ownerCfg)
	if err != nil {
		t.Fatalf("ConfigHash: %v", err)
	}
	descriptorPath := projectdaemon.DescriptorPath(ownerCfg.DataDir)
	if err := projectdaemon.WriteDescriptorAtomic(descriptorPath, &projectdaemon.Descriptor{
		SchemaVersion:    projectdaemon.DescriptorSchemaVersion,
		PID:              os.Getpid(),
		StartedAt:        time.Now().UTC(),
		DataDir:          ownerCfg.DataDir,
		RootDir:          ownerCfg.RootDir,
		PublicURL:        "http://127.0.0.1:" + ownerCfg.Port,
		PublicMCPEnabled: false,
		ControlURL:       staleControl.URL,
		ConfigHash:       hash,
		IdleTimeout:      "0s",
		ControlToken:     "stale-token",
		Config:           ownerCfg,
	}); err != nil {
		t.Fatalf("write stale descriptor: %v", err)
	}
	payload := `{"hook_event_name":"SessionStart","session_id":"stale-descriptor-secret","prompt":"private prompt"}`
	stdout, stderr, err := runLeafwikiHelperWithInputAndTimeout(t, []string{
		"agent-hook", "codex",
		"--disable-auth",
		"--data-dir", dataDir,
		"--root-dir", rootDir,
		"--host", "127.0.0.1",
		"--port", ownerCfg.Port,
		"--log-target", "stderr",
	}, nil, payload, 10*time.Second)

	if err != nil {
		t.Fatalf("agent-hook stale descriptor should fail open, got %v\nstdout:\n%s\nstderr:\n%s", err, stdout, stderr)
	}
	if stdout != "{}\n" {
		t.Fatalf("stdout = %q, want Codex allow response", stdout)
	}
	if strings.Contains(stderr, "stale-descriptor-secret") || strings.Contains(stderr, "private prompt") {
		t.Fatalf("stderr leaked hook payload data: %s", stderr)
	}
	replaced := readFileString(t, descriptorPath)
	if strings.Contains(replaced, staleControl.URL) || strings.Contains(replaced, "stale-token") {
		t.Fatalf("descriptor was not replaced:\n%s", replaced)
	}
	desc := waitForProjectDaemonDescriptor(t, dataDir)
	t.Cleanup(func() {
		terminateProjectDaemonProcess(t, desc.PID)
	})
	select {
	case got := <-received:
		t.Fatalf("stale descriptor endpoint received request before replacement: %q", got)
	default:
	}
}

func TestRunAgentHookCommandRecoversPanicAndAllows(t *testing.T) {
	var stdout bytes.Buffer
	err := runAgentHookCommand(context.Background(), testRuntimeConfig(t.TempDir(), filepath.Join(t.TempDir(), "root"), freeTCPPort(t), mcpTransports{}, true), agenthooks.ProviderCodex, panicReader{}, &stdout)

	if err == nil {
		t.Fatalf("runAgentHookCommand err = nil, want panic surfaced as fail-open error")
	}
	if stdout.String() != "{}\n" {
		t.Fatalf("stdout = %q, want Codex allow response after panic", stdout.String())
	}
}

func TestRunAgentHookCommandReadErrorFailsOpen(t *testing.T) {
	var stdout bytes.Buffer
	err := runAgentHookCommand(
		context.Background(),
		testRuntimeConfig(t.TempDir(), filepath.Join(t.TempDir(), "root"), freeTCPPort(t), mcpTransports{}, true),
		agenthooks.ProviderClaude,
		errorReader{err: errors.New("synthetic read failure")},
		&stdout,
	)

	if err == nil {
		t.Fatalf("runAgentHookCommand err = nil, want read error")
	}
	if stdout.String() != "{}\n" {
		t.Fatalf("stdout = %q, want Claude allow response after read error", stdout.String())
	}
}

func TestMainProcessAgentHookPreDispatchFailuresFailOpen(t *testing.T) {
	baseDir := t.TempDir()
	sameDir := filepath.Join(baseDir, "same")
	payload := `{"hook_event_name":"SessionStart","session_id":"pre-dispatch-secret"}`

	stdout, stderr, err := runLeafwikiHelperWithInputAndTimeout(t, []string{
		"agent-hook", "codex",
		"--disable-auth",
		"--data-dir", sameDir,
		"--root-dir", sameDir,
		"--log-target", "stderr",
	}, nil, payload, 5*time.Second)

	if err != nil {
		t.Fatalf("agent-hook invalid workspace should fail open, got %v\nstdout:\n%s\nstderr:\n%s", err, stdout, stderr)
	}
	if stdout != "{}\n" {
		t.Fatalf("stdout = %q, want Codex allow response", stdout)
	}
	if strings.Contains(stderr, "pre-dispatch-secret") {
		t.Fatalf("stderr leaked hook payload data: %s", stderr)
	}
}

func TestMainProcessAgentHookFlagFirstPreDispatchFailuresFailOpen(t *testing.T) {
	baseDir := t.TempDir()
	sameDir := filepath.Join(baseDir, "same")
	payload := `{"hook_event_name":"SessionStart","session_id":"flag-first-secret"}`

	stdout, stderr, err := runLeafwikiHelperWithInputAndTimeout(t, []string{
		"--disable-auth",
		"--data-dir", sameDir,
		"--root-dir", sameDir,
		"--log-target", "stderr",
		"agent-hook", "codex",
	}, nil, payload, 5*time.Second)

	if err != nil {
		t.Fatalf("flag-first agent-hook invalid workspace should fail open, got %v\nstdout:\n%s\nstderr:\n%s", err, stdout, stderr)
	}
	if stdout != "{}\n" {
		t.Fatalf("stdout = %q, want Codex allow response", stdout)
	}
	if strings.Contains(stderr, "flag-first-secret") {
		t.Fatalf("stderr leaked hook payload data: %s", stderr)
	}
}

func TestMainProcessNonHookFlagValueNamedAgentHookDoesNotFailOpen(t *testing.T) {
	baseDir := t.TempDir()
	sameDir := filepath.Join(baseDir, "same")
	if err := os.MkdirAll(sameDir, 0o755); err != nil {
		t.Fatalf("mkdir sameDir: %v", err)
	}

	stdout, stderr, err := runLeafwikiHelperWithInputAndTimeout(t, []string{
		"--log-file", "agent-hook",
		"--disable-auth",
		"--data-dir", sameDir,
		"--root-dir", sameDir,
		"--log-target", "stderr",
	}, nil, `{"session_id":"should-not-be-hook"}`, 5*time.Second)

	if err == nil {
		t.Fatalf("non-hook startup unexpectedly succeeded\nstdout:\n%s\nstderr:\n%s", stdout, stderr)
	}
	if stdout == "{}\n" {
		t.Fatalf("stdout = %q, want no agent-hook fail-open response", stdout)
	}
	if !strings.Contains(stderr, "Invalid workspace configuration") {
		t.Fatalf("stderr = %q, want workspace configuration error", stderr)
	}
}

func TestMainProcessNonHookFlagValueNamedAgentHookParseErrorDoesNotFailOpen(t *testing.T) {
	stdout, stderr, err := runLeafwikiHelperWithInputAndTimeout(t, []string{
		"--log-file", "agent-hook",
		"--not-a-real-flag",
	}, nil, `{"session_id":"should-not-be-hook"}`, 5*time.Second)

	if err == nil {
		t.Fatalf("non-hook parse error unexpectedly succeeded\nstdout:\n%s\nstderr:\n%s", stdout, stderr)
	}
	if stdout == "{}\n" {
		t.Fatalf("stdout = %q, want no agent-hook fail-open response", stdout)
	}
	if !strings.Contains(stderr, "not-a-real-flag") {
		t.Fatalf("stderr = %q, want flag parse error", stderr)
	}
}

func TestMainProcessAgentHookFlagFirstParseErrorsFailOpen(t *testing.T) {
	payload := `{"hook_event_name":"SessionStart","session_id":"flag-parse-secret"}`

	stdout, stderr, err := runLeafwikiHelperWithInputAndTimeout(t, []string{
		"--not-a-real-flag",
		"agent-hook", "codex",
	}, nil, payload, 5*time.Second)

	if err != nil {
		t.Fatalf("flag-first agent-hook parse error should fail open, got %v\nstdout:\n%s\nstderr:\n%s", err, stdout, stderr)
	}
	if stdout != "{}\n" {
		t.Fatalf("stdout = %q, want Codex allow response", stdout)
	}
	if strings.Contains(stderr, "flag-parse-secret") {
		t.Fatalf("stderr leaked hook payload data: %s", stderr)
	}
}

func TestMainProcessAgentHookFlagValueNamedConfigDoesNotDisableFailOpen(t *testing.T) {
	payload := `{"hook_event_name":"SessionStart","session_id":"flag-value-config-secret"}`

	stdout, stderr, err := runLeafwikiHelperWithInputAndTimeout(t, []string{
		"--data-dir", "--config",
		"--not-a-real-flag",
		"agent-hook", "codex",
	}, nil, payload, 5*time.Second)

	if err != nil {
		t.Fatalf("non-config hook parse error should fail open, got %v\nstdout:\n%s\nstderr:\n%s", err, stdout, stderr)
	}
	if stdout != "{}\n" {
		t.Fatalf("stdout = %q, want Codex allow response", stdout)
	}
	if strings.Contains(stderr, "flag-value-config-secret") {
		t.Fatalf("stderr leaked hook payload data: %s", stderr)
	}
}

func TestMainProcessAgentHookMalformedConfigFlagDoesNotDisableFailOpen(t *testing.T) {
	payload := `{"hook_event_name":"SessionStart","session_id":"malformed-config-flag-secret"}`

	stdout, stderr, err := runLeafwikiHelperWithInputAndTimeout(t, []string{
		"---config",
		"--not-a-real-flag",
		"agent-hook", "codex",
	}, nil, payload, 5*time.Second)

	if err != nil {
		t.Fatalf("malformed non-config hook parse error should fail open, got %v\nstdout:\n%s\nstderr:\n%s", err, stdout, stderr)
	}
	if stdout != "{}\n" {
		t.Fatalf("stdout = %q, want Codex allow response", stdout)
	}
	if strings.Contains(stderr, "malformed-config-flag-secret") {
		t.Fatalf("stderr leaked hook payload data: %s", stderr)
	}
}

func TestMainProcessAgentHookProviderAllowResponsesFailOpen(t *testing.T) {
	tests := []struct {
		name       string
		provider   string
		payload    string
		wantStdout string
	}{
		{name: "claude malformed", provider: agenthooks.ProviderClaude, payload: "{", wantStdout: "{}\n"},
		{name: "cursor malformed", provider: agenthooks.ProviderCursor, payload: "{", wantStdout: "{\"permission\":\"allow\"}\n"},
		{name: "unknown provider", provider: agenthooks.ProviderUnknown, payload: `{"hook_event_name":"SessionStart","session_id":"unknown-secret"}`, wantStdout: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			baseDir := t.TempDir()
			dataDir := filepath.Join(baseDir, "data")
			rootDir := filepath.Join(baseDir, "content")
			stdout, stderr, err := runLeafwikiHelperWithInputAndTimeout(t, []string{
				"agent-hook", tt.provider,
				"--disable-auth",
				"--data-dir", dataDir,
				"--root-dir", rootDir,
				"--host", "127.0.0.1",
				"--port", freeTCPPort(t),
				"--log-target", "stderr",
			}, nil, tt.payload, 5*time.Second)

			if err != nil {
				t.Fatalf("agent-hook should fail open, got %v\nstdout:\n%s\nstderr:\n%s", err, stdout, stderr)
			}
			if stdout != tt.wantStdout {
				t.Fatalf("stdout = %q, want %q", stdout, tt.wantStdout)
			}
			if strings.Contains(stderr, "unknown-secret") {
				t.Fatalf("stderr leaked hook payload data: %s", stderr)
			}
		})
	}
}

func TestRunAgentHookCommandOversizedPayloadFailsOpen(t *testing.T) {
	var stdout bytes.Buffer
	baseDir := t.TempDir()
	err := runAgentHookCommand(
		context.Background(),
		testRuntimeConfig(filepath.Join(baseDir, "data"), filepath.Join(baseDir, "root"), freeTCPPort(t), mcpTransports{}, true),
		agenthooks.ProviderCursor,
		strings.NewReader(strings.Repeat("x", agentHookMaxPayloadBytes+1)),
		&stdout,
	)

	if err == nil {
		t.Fatalf("runAgentHookCommand err = nil, want oversized payload error")
	}
	if stdout.String() != "{\"permission\":\"allow\"}\n" {
		t.Fatalf("stdout = %q, want Cursor allow response", stdout.String())
	}
}

func TestRunAgentHookCommandLockedProjectFailsOpen(t *testing.T) {
	baseDir := t.TempDir()
	dataDir := filepath.Join(baseDir, "data")
	rootDir := filepath.Join(baseDir, "content")
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		t.Fatalf("create data dir: %v", err)
	}
	if err := os.MkdirAll(rootDir, 0o755); err != nil {
		t.Fatalf("create root dir: %v", err)
	}
	canonicalData, canonicalRoot, err := projectdaemon.CanonicalizeProject(dataDir, rootDir)
	if err != nil {
		t.Fatalf("canonicalize project: %v", err)
	}
	dataLock, err := locking.AcquireDataDirLock(canonicalData)
	if err != nil {
		t.Fatalf("acquire data lock: %v", err)
	}
	defer dataLock.Release()
	rootLock, err := locking.AcquireRootDirLock(canonicalRoot)
	if err != nil {
		t.Fatalf("acquire root lock: %v", err)
	}
	defer rootLock.Release()

	var stdout bytes.Buffer
	err = runAgentHookCommand(
		context.Background(),
		testRuntimeConfig(dataDir, rootDir, freeTCPPort(t), mcpTransports{}, true),
		agenthooks.ProviderCodex,
		strings.NewReader(`{"hook_event_name":"SessionStart","session_id":"locked-secret"}`),
		&stdout,
	)

	if err == nil {
		t.Fatalf("runAgentHookCommand err = nil, want locked project error")
	}
	if stdout.String() != "{}\n" {
		t.Fatalf("stdout = %q, want Codex allow response", stdout.String())
	}
	if strings.Contains(err.Error(), "locked-secret") {
		t.Fatalf("error leaked hook payload data: %v", err)
	}
}

func TestRunAgentHookCommandControlRecordFailuresFailOpen(t *testing.T) {
	tests := []struct {
		name          string
		recordHandler func(http.ResponseWriter, *http.Request)
		parentTimeout time.Duration
	}{
		{name: "control 401", recordHandler: func(w http.ResponseWriter, _ *http.Request) {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
		}},
		{name: "control 400", recordHandler: func(w http.ResponseWriter, _ *http.Request) {
			http.Error(w, "bad event", http.StatusBadRequest)
		}},
		{name: "control 500", recordHandler: func(w http.ResponseWriter, _ *http.Request) {
			http.Error(w, "boom", http.StatusInternalServerError)
		}},
		{name: "control timeout", parentTimeout: 50 * time.Millisecond, recordHandler: func(w http.ResponseWriter, _ *http.Request) {
			time.Sleep(250 * time.Millisecond)
			w.WriteHeader(http.StatusNoContent)
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg, cleanup := testRuntimeConfigWithHealthyControlDescriptor(t, tt.recordHandler)
			defer cleanup()
			ctx := context.Background()
			if tt.parentTimeout > 0 {
				var cancel context.CancelFunc
				ctx, cancel = context.WithTimeout(ctx, tt.parentTimeout)
				defer cancel()
			}

			var stdout bytes.Buffer
			err := runAgentHookCommand(
				ctx,
				cfg,
				agenthooks.ProviderCodex,
				strings.NewReader(`{"hook_event_name":"SessionStart","session_id":"control-secret"}`),
				&stdout,
			)

			if err == nil {
				t.Fatalf("runAgentHookCommand err = nil, want control failure")
			}
			if stdout.String() != "{}\n" {
				t.Fatalf("stdout = %q, want Codex allow response", stdout.String())
			}
			if strings.Contains(err.Error(), "control-secret") {
				t.Fatalf("error leaked hook payload data: %v", err)
			}
		})
	}
}

type panicReader struct{}

func (panicReader) Read([]byte) (int, error) {
	panic("boom")
}

type errorReader struct {
	err error
}

func (r errorReader) Read([]byte) (int, error) {
	return 0, r.err
}

func TestMainProcess_DisabledAuthOwnerRejectsAPIKeyStdioAttach(t *testing.T) {
	baseDir := t.TempDir()
	dataDir := filepath.Join(baseDir, "data")
	rootDir := filepath.Join(baseDir, "content")
	port := freeTCPPort(t)
	first := startLeafwikiHelper(t, []string{
		"--disable-auth",
		"--data-dir", dataDir,
		"--root-dir", rootDir,
		"--host", "127.0.0.1",
		"--port", port,
		"--log-target", "stderr",
	}, nil)
	waitForLeafwikiReady(t, first, port)

	apiKey := "lwk_disabled_auth_owner_process_secret"
	stdout, stderr, err := runLeafwikiHelperWithTimeout(t, []string{
		"--mcp=stdio",
		"--data-dir", dataDir,
		"--root-dir", rootDir,
		"--host", "127.0.0.1",
		"--port", port,
		"--log-target", "stderr",
	}, map[string]string{"LEAFWIKI_MCP_API_KEY": apiKey}, 5*time.Second)

	if err == nil {
		t.Fatalf("API-key STDIO attach to disabled-auth owner unexpectedly succeeded\nstdout:\n%s\nstderr:\n%s", stdout, stderr)
	}
	if stdout != "" {
		t.Fatalf("stdout = %q, want empty on rejected API-key attach", stdout)
	}
	if strings.Contains(stdout, apiKey) || strings.Contains(stderr, apiKey) {
		t.Fatalf("API key leaked in process output\nstdout:\n%s\nstderr:\n%s", stdout, stderr)
	}
	if !strings.Contains(stderr, "project daemon config mismatch") || !strings.Contains(stderr, "auth-disabled") {
		t.Fatalf("stderr = %q, want auth-disabled daemon config mismatch", stderr)
	}
}

func TestMainProcess_NonLoopbackPlainWebOwnerSupportsLaterPrivateStdioAttach(t *testing.T) {
	baseDir := t.TempDir()
	dataDir := filepath.Join(baseDir, "data")
	rootDir := filepath.Join(baseDir, "content")
	port := freeTCPPort(t)
	first := startLeafwikiHelper(t, []string{
		"--disable-auth",
		"--data-dir", dataDir,
		"--root-dir", rootDir,
		"--host", "0.0.0.0",
		"--port", port,
		"--log-target", "stderr",
	}, nil)
	waitForLeafwikiReady(t, first, port)

	stdout, stderr, err := runLeafwikiHelperWithInputAndTimeout(t, []string{
		"--mcp=stdio",
		"--disable-auth",
		"--data-dir", dataDir,
		"--root-dir", rootDir,
		"--host", "127.0.0.1",
		"--port", port,
		"--log-target", "stderr",
	}, nil, nativeStdioListToolsInput(), 8*time.Second)

	if err != nil {
		t.Fatalf("stdio startup should attach to non-loopback plain web owner, got %v\nstdout:\n%s\nstderr:\n%s", err, stdout, stderr)
	}
	if !strings.Contains(stdout, `"id":2`) || !strings.Contains(stdout, "create_page") {
		t.Fatalf("stdout = %q, want tools/list response from private MCP bridge", stdout)
	}
	if strings.Contains(stderr, "MCP requires a loopback host") || strings.Contains(stderr, "project daemon config mismatch") {
		t.Fatalf("stderr = %q, want no host validation/config mismatch failure", stderr)
	}
}

func TestMainProcess_PlainWebSecondStartupAttachesToExistingOwner(t *testing.T) {
	if !supportsGracefulProcessSignal() {
		t.Skip("graceful process signaling is required to assert foreground session release")
	}

	baseDir := t.TempDir()
	dataDir := filepath.Join(baseDir, "data")
	rootDir := filepath.Join(baseDir, "content")
	port := freeTCPPort(t)
	first := startLeafwikiHelper(t, []string{
		"--disable-auth",
		"--data-dir", dataDir,
		"--root-dir", rootDir,
		"--host", "127.0.0.1",
		"--port", port,
		"--log-target", "stderr",
	}, nil)
	waitForLeafwikiReady(t, first, port)

	second := startLeafwikiHelper(t, []string{
		"--disable-auth",
		"--data-dir", dataDir,
		"--root-dir", rootDir,
		"--host", "127.0.0.1",
		"--port", port,
		"--log-target", "stderr",
	}, nil)
	waitForLeafwikiReady(t, second, port)

	if err := signalLeafwikiProcess(first.cmd.Process); err != nil {
		t.Fatalf("signal first foreground process: %v", err)
	}
	first.waitForExit(t)
	waitForLeafwikiReady(t, second, port)
	stderr := readFileString(t, second.stderrPath)
	for _, unexpected := range []string{"data directory is already in use", "root directory is already in use", "bind: address already in use", "project daemon config mismatch"} {
		if strings.Contains(stderr, unexpected) {
			t.Fatalf("second foreground startup stderr = %q, want no ownership failure %q", stderr, unexpected)
		}
	}
	if err := signalLeafwikiProcess(second.cmd.Process); err != nil {
		t.Fatalf("signal second foreground process: %v", err)
	}
	second.waitForExit(t)
	waitForLeafwikiUnavailable(t, port)
}

func TestMainProcess_PlainWebOwnerHandlesLaterPrivateStdioMCPFrames(t *testing.T) {
	baseDir := t.TempDir()
	dataDir := filepath.Join(baseDir, "data")
	rootDir := filepath.Join(baseDir, "content")
	port := freeTCPPort(t)
	first := startLeafwikiHelper(t, []string{
		"--disable-auth",
		"--data-dir", dataDir,
		"--root-dir", rootDir,
		"--host", "127.0.0.1",
		"--port", port,
		"--log-target", "stderr",
	}, nil)
	waitForLeafwikiReady(t, first, port)

	stdout, stderr, err := runLeafwikiHelperWithInputAndTimeout(t, []string{
		"--mcp=stdio",
		"--disable-auth",
		"--data-dir", dataDir,
		"--root-dir", rootDir,
		"--host", "127.0.0.1",
		"--port", port,
		"--log-target", "stderr",
	}, nil, nativeStdioListToolsInput(), 8*time.Second)

	if err != nil {
		t.Fatalf("stdio startup should proxy MCP frames to existing plain owner, got %v\nstdout:\n%s\nstderr:\n%s", err, stdout, stderr)
	}
	if !strings.Contains(stdout, `"id":2`) || !strings.Contains(stdout, "create_page") {
		t.Fatalf("stdout = %q, want tools/list response from private MCP bridge", stdout)
	}
	if strings.Contains(stderr, "project daemon config mismatch") {
		t.Fatalf("stderr = %q, want no config mismatch", stderr)
	}
	resp, err := http.Get("http://127.0.0.1:" + port + "/mcp")
	if err != nil {
		t.Fatalf("GET /mcp: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("/mcp status = %d, want %d", resp.StatusCode, http.StatusNotFound)
	}
}

func TestMainProcess_AuthHTTPOwnerHandlesLaterPrivateStdioMCPUserContext(t *testing.T) {
	baseDir := t.TempDir()
	dataDir := filepath.Join(baseDir, "data")
	rootDir := filepath.Join(baseDir, "content")
	apiKey := createMCPAPIKey(t, dataDir)
	port := freeTCPPort(t)
	first := startLeafwikiHelper(t, []string{
		"--mcp=http",
		"--data-dir", dataDir,
		"--root-dir", rootDir,
		"--host", "127.0.0.1",
		"--port", port,
		"--jwt-secret", "owner-jwt-secret",
		"--admin-password", "owner-admin-password",
		"--allow-insecure",
		"--log-target", "stderr",
	}, nil)
	waitForLeafwikiReady(t, first, port)

	stdout, stderr, err := runLeafwikiHelperWithInputAndTimeout(t, []string{
		"--mcp=stdio",
		"--data-dir", dataDir,
		"--root-dir", rootDir,
		"--host", "127.0.0.1",
		"--port", port,
		"--allow-insecure",
		"--log-target", "stderr",
	}, map[string]string{"LEAFWIKI_MCP_API_KEY": apiKey}, nativeStdioToolCallInput(2, "wiki_get_current_user", map[string]any{}), 8*time.Second)

	if err != nil {
		t.Fatalf("auth STDIO startup should proxy MCP frames to HTTP owner, got %v\nstdout:\n%s\nstderr:\n%s", err, stdout, stderr)
	}
	if !strings.Contains(stdout, `"id":2`) || !strings.Contains(stdout, `"username":"editor"`) || !strings.Contains(stdout, `"role":"editor"`) {
		t.Fatalf("stdout = %q, want get_current_user response for API-key editor", stdout)
	}
	if strings.Contains(stderr, "JWT secret is required") || strings.Contains(stderr, "admin password is required") || strings.Contains(stderr, "project daemon config mismatch") {
		t.Fatalf("stderr = %q, want no owner bootstrap/config mismatch failure", stderr)
	}
}

func TestMainProcess_DisabledAuthStdioClientsCollaborateThroughOwner(t *testing.T) {
	baseDir := t.TempDir()
	dataDir := filepath.Join(baseDir, "data")
	rootDir := filepath.Join(baseDir, "content")
	port := freeTCPPort(t)
	slug := fmt.Sprintf("stdio-collaboration-%d", time.Now().UnixNano())
	title := "STDIO Collaboration Page"

	writerStdout, writerStderr, err := runLeafwikiHelperWithInputAndTimeout(t, []string{
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
	if err != nil {
		t.Fatalf("writer STDIO client failed: %v\nstdout:\n%s\nstderr:\n%s", err, writerStdout, writerStderr)
	}
	if !strings.Contains(writerStdout, `"id":2`) || !strings.Contains(writerStdout, slug) {
		t.Fatalf("writer stdout = %q, want create_page response with slug %q", writerStdout, slug)
	}

	readerStdout, readerStderr, err := runLeafwikiHelperWithInputAndTimeout(t, []string{
		"--mcp=stdio",
		"--disable-auth",
		"--data-dir", dataDir,
		"--root-dir", rootDir,
		"--host", "127.0.0.1",
		"--port", port,
		"--log-target", "stderr",
	}, map[string]string{"LEAFWIKI_DAEMON_IDLE_TIMEOUT": "3s"}, nativeStdioToolCallInput(2, "wiki_get_page_by_path", map[string]any{"path": slug}), 8*time.Second)
	if err != nil {
		t.Fatalf("reader STDIO client failed: %v\nstdout:\n%s\nstderr:\n%s", err, readerStdout, readerStderr)
	}
	if !strings.Contains(readerStdout, `"id":2`) || !strings.Contains(readerStdout, slug) || !strings.Contains(readerStdout, title) {
		t.Fatalf("reader stdout = %q, want get_page_by_path response for writer-created page", readerStdout)
	}
	waitForLeafwikiUnavailable(t, port)
}

func TestMainProcess_PlainWebOwnerRejectsLaterPublicMCPEnablement(t *testing.T) {
	baseDir := t.TempDir()
	dataDir := filepath.Join(baseDir, "data")
	rootDir := filepath.Join(baseDir, "content")
	port := freeTCPPort(t)
	first := startLeafwikiHelper(t, []string{
		"--disable-auth",
		"--data-dir", dataDir,
		"--root-dir", rootDir,
		"--host", "127.0.0.1",
		"--port", port,
		"--log-target", "stderr",
	}, nil)
	waitForLeafwikiReady(t, first, port)

	stdout, stderr, err := runLeafwikiHelperWithTimeout(t, []string{
		"--mcp=http",
		"--disable-auth",
		"--data-dir", dataDir,
		"--root-dir", rootDir,
		"--host", "127.0.0.1",
		"--port", port,
		"--log-target", "stderr",
	}, nil, 5*time.Second)

	if err == nil {
		t.Fatalf("later public MCP enablement unexpectedly succeeded\nstdout:\n%s\nstderr:\n%s", stdout, stderr)
	}
	if errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("later public MCP enablement hung; expected config mismatch\nstdout:\n%s\nstderr:\n%s", stdout, stderr)
	}
	if !strings.Contains(stderr, "project daemon config mismatch") || !strings.Contains(stderr, "public-mcp-enabled") {
		t.Fatalf("stderr = %q, want public MCP config mismatch", stderr)
	}
}

func TestMainProcess_BasePathOwnerRejectsLaterNoBasePathStartup(t *testing.T) {
	baseDir := t.TempDir()
	dataDir := filepath.Join(baseDir, "data")
	rootDir := filepath.Join(baseDir, "content")
	port := freeTCPPort(t)
	first := startLeafwikiHelper(t, []string{
		"--disable-auth",
		"--data-dir", dataDir,
		"--root-dir", rootDir,
		"--host", "127.0.0.1",
		"--port", port,
		"--base-path", "/wiki",
		"--log-target", "stderr",
	}, nil)
	desc := waitForProjectDaemonDescriptor(t, dataDir)
	if desc.BasePath != "/wiki" {
		t.Fatalf("descriptor base path = %q, want /wiki", desc.BasePath)
	}
	waitForLeafwikiReadyAtBasePath(t, first, port, "/wiki")

	stdout, stderr, err := runLeafwikiHelperWithTimeout(t, []string{
		"--disable-auth",
		"--data-dir", dataDir,
		"--root-dir", rootDir,
		"--host", "127.0.0.1",
		"--port", port,
		"--log-target", "stderr",
	}, nil, 5*time.Second)

	if err == nil {
		t.Fatalf("later no-base-path startup unexpectedly succeeded\nstdout:\n%s\nstderr:\n%s", stdout, stderr)
	}
	if errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("later no-base-path startup hung; expected config mismatch\nstdout:\n%s\nstderr:\n%s", stdout, stderr)
	}
	if !strings.Contains(stderr, "project daemon config mismatch") || !strings.Contains(stderr, "base-path") {
		t.Fatalf("stderr = %q, want base-path config mismatch", stderr)
	}
}

func TestMainProcess_AuthOwnerRejectsLaterDisableAuthStartup(t *testing.T) {
	baseDir := t.TempDir()
	dataDir := filepath.Join(baseDir, "data")
	rootDir := filepath.Join(baseDir, "content")
	port := freeTCPPort(t)
	first := startLeafwikiHelper(t, []string{
		"--data-dir", dataDir,
		"--root-dir", rootDir,
		"--host", "127.0.0.1",
		"--port", port,
		"--jwt-secret", "owner-jwt-secret",
		"--admin-password", "owner-admin-password",
		"--allow-insecure",
		"--log-target", "stderr",
	}, nil)
	waitForLeafwikiReady(t, first, port)

	stdout, stderr, err := runLeafwikiHelperWithTimeout(t, []string{
		"--disable-auth",
		"--data-dir", dataDir,
		"--root-dir", rootDir,
		"--host", "127.0.0.1",
		"--port", port,
		"--log-target", "stderr",
	}, nil, 5*time.Second)

	if err == nil {
		t.Fatalf("later disable-auth startup unexpectedly succeeded\nstdout:\n%s\nstderr:\n%s", stdout, stderr)
	}
	if errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("later disable-auth startup hung; expected config mismatch\nstdout:\n%s\nstderr:\n%s", stdout, stderr)
	}
	if !strings.Contains(stderr, "project daemon config mismatch") || !strings.Contains(stderr, "auth-disabled") {
		t.Fatalf("stderr = %q, want auth-disabled config mismatch", stderr)
	}
}

func TestMainProcess_ProjectDaemonDescriptorUsesDefaultIdleTimeoutWhenUnspecified(t *testing.T) {
	var ownerPID int
	t.Cleanup(func() {
		terminateProjectDaemonProcess(t, ownerPID)
	})
	baseDir := t.TempDir()
	dataDir := filepath.Join(baseDir, "data")
	rootDir := filepath.Join(baseDir, "content")
	port := freeTCPPort(t)
	proc := startLeafwikiHelper(t, []string{
		"--disable-auth",
		"--data-dir", dataDir,
		"--root-dir", rootDir,
		"--host", "127.0.0.1",
		"--port", port,
		"--log-target", "stderr",
	}, map[string]string{"LEAFWIKI_DAEMON_IDLE_TIMEOUT": ""})
	desc := waitForProjectDaemonDescriptor(t, dataDir)
	ownerPID = desc.PID
	waitForLeafwikiReady(t, proc, port)

	if desc.IdleTimeout != "10m0s" {
		t.Fatalf("descriptor idle timeout = %q, want core CLI default 10m0s", desc.IdleTimeout)
	}
	if desc.Config.DaemonIdleTimeout != "10m0s" {
		t.Fatalf("descriptor config daemon idle timeout = %q, want core CLI default 10m0s", desc.Config.DaemonIdleTimeout)
	}
	proc.stop(t)
}

func TestMainProcess_ProjectDaemonDescriptorIncludesWorkspaceSyncFlag(t *testing.T) {
	var ownerPID int
	t.Cleanup(func() {
		terminateProjectDaemonProcess(t, ownerPID)
	})
	baseDir := t.TempDir()
	dataDir := filepath.Join(baseDir, "data")
	rootDir := filepath.Join(baseDir, "content")
	port := freeTCPPort(t)
	proc := startLeafwikiHelper(t, []string{
		"--disable-auth",
		"--enable-workspace-sync",
		"--data-dir", dataDir,
		"--root-dir", rootDir,
		"--host", "127.0.0.1",
		"--port", port,
		"--log-target", "stderr",
	}, nil)
	desc := waitForProjectDaemonDescriptor(t, dataDir)
	ownerPID = desc.PID
	waitForLeafwikiReady(t, proc, port)

	if !desc.Config.EnableWorkspaceSync {
		t.Fatalf("descriptor config EnableWorkspaceSync = false, want true")
	}
	proc.stop(t)
}

func TestMainProcess_RejectsRevisionAndWorkspaceSyncTogether(t *testing.T) {
	baseDir := t.TempDir()
	dataDir := filepath.Join(baseDir, "data")
	rootDir := filepath.Join(baseDir, "content")
	stdout, stderr, err := runLeafwikiHelperWithTimeout(t, []string{
		"--disable-auth",
		"--enable-revision",
		"--enable-workspace-sync",
		"--data-dir", dataDir,
		"--root-dir", rootDir,
		"--host", "127.0.0.1",
		"--port", freeTCPPort(t),
		"--log-target", "stderr",
	}, nil, 5*time.Second)

	if err == nil {
		t.Fatalf("combined revision/workspace-sync startup unexpectedly succeeded\nstdout:\n%s\nstderr:\n%s", stdout, stderr)
	}
	if errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("combined revision/workspace-sync startup hung; expected immediate validation error\nstdout:\n%s\nstderr:\n%s", stdout, stderr)
	}
	if !strings.Contains(stderr, "enable-revision and enable-workspace-sync cannot be combined") {
		t.Fatalf("stderr = %q, want mutual exclusion error", stderr)
	}
}

func TestMainProcess_ConfigEndpointReportsWorkspaceSyncFlag(t *testing.T) {
	var ownerPID int
	t.Cleanup(func() {
		terminateProjectDaemonProcess(t, ownerPID)
	})
	baseDir := t.TempDir()
	dataDir := filepath.Join(baseDir, "data")
	rootDir := filepath.Join(baseDir, "content")
	port := freeTCPPort(t)
	proc := startLeafwikiHelper(t, []string{
		"--disable-auth",
		"--allow-insecure",
		"--enable-workspace-sync",
		"--data-dir", dataDir,
		"--root-dir", rootDir,
		"--host", "127.0.0.1",
		"--port", port,
		"--log-target", "stderr",
	}, nil)
	desc := waitForProjectDaemonDescriptor(t, dataDir)
	ownerPID = desc.PID
	waitForLeafwikiReady(t, proc, port)

	resp, err := http.Get("http://127.0.0.1:" + port + "/api/config")
	if err != nil {
		t.Fatalf("GET /api/config: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("GET /api/config = %d: %s", resp.StatusCode, body)
	}
	var config map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&config); err != nil {
		t.Fatalf("decode config: %v", err)
	}
	if config["enableWorkspaceSync"] != true {
		t.Fatalf("enableWorkspaceSync = %v, want true in /api/config", config["enableWorkspaceSync"])
	}
	proc.stop(t)
}

func TestMainProcess_DifferentRootDirDoesNotRemoveLiveProjectDescriptor(t *testing.T) {
	baseDir := t.TempDir()
	dataDir := filepath.Join(baseDir, "data")
	ownerRootDir := filepath.Join(baseDir, "owner-content")
	requestedRootDir := filepath.Join(baseDir, "requested-content")
	port := freeTCPPort(t)
	owner := startLeafwikiHelper(t, []string{
		"--disable-auth",
		"--data-dir", dataDir,
		"--root-dir", ownerRootDir,
		"--host", "127.0.0.1",
		"--port", port,
		"--log-target", "stderr",
	}, nil)
	waitForLeafwikiReady(t, owner, port)
	descriptorPath := projectdaemon.DescriptorPath(dataDir)
	waitForFileContaining(t, descriptorPath, `"rootDir"`)

	stdout, stderr, err := runLeafwikiHelperWithTimeout(t, []string{
		"--disable-auth",
		"--data-dir", dataDir,
		"--root-dir", requestedRootDir,
		"--host", "127.0.0.1",
		"--port", port,
		"--log-target", "stderr",
	}, nil, 12*time.Second)

	if err == nil {
		t.Fatalf("different root-dir startup unexpectedly succeeded\nstdout:\n%s\nstderr:\n%s", stdout, stderr)
	}
	if !strings.Contains(stderr, "project daemon config mismatch") || !strings.Contains(stderr, "root-dir") {
		t.Fatalf("stderr = %q, want root-dir config mismatch", stderr)
	}
	if _, err := os.Stat(descriptorPath); err != nil {
		t.Fatalf("live descriptor was removed after root-dir mismatch: %v\nstdout:\n%s\nstderr:\n%s", err, stdout, stderr)
	}
	waitForLeafwikiReady(t, owner, port)
	owner.stop(t)
}

func TestMainProcess_FirstStartupWritesSecureProjectDaemonDescriptor(t *testing.T) {
	stdinReader, stdinWriter := io.Pipe()
	defer stdinWriter.Close()
	baseDir := t.TempDir()
	dataDir := filepath.Join(baseDir, "data")
	rootDir := filepath.Join(baseDir, "content")
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
	descriptorPath := filepath.Join(dataDir, ".leafwiki", "project-daemon.json")
	waitForFileContaining(t, descriptorPath, `"controlToken"`)
	info, err := os.Stat(descriptorPath)
	if err != nil {
		t.Fatalf("stat descriptor: %v", err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("descriptor mode = %v, want 0600", got)
	}
	raw := readFileString(t, descriptorPath)
	if !strings.Contains(raw, filepath.Clean(dataDir)) || !strings.Contains(raw, filepath.Clean(rootDir)) {
		t.Fatalf("descriptor = %s, want canonical data/root dirs", raw)
	}
	if strings.Contains(raw, "LEAFWIKI_MCP_API_KEY") || strings.Contains(raw, "lwk_") {
		t.Fatalf("descriptor leaked API-key material: %s", raw)
	}
}

func TestMainProcess_CanonicalPathVariantsAttachWithDefaultFileLogging(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink path canonicalization test is Unix-oriented")
	}
	stdinReader, stdinWriter := io.Pipe()
	defer stdinWriter.Close()
	baseDir := t.TempDir()
	dataDir := filepath.Join(baseDir, "data")
	rootDir := filepath.Join(baseDir, "content")
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		t.Fatalf("create data dir: %v", err)
	}
	if err := os.MkdirAll(rootDir, 0o755); err != nil {
		t.Fatalf("create root dir: %v", err)
	}
	dataLink := filepath.Join(baseDir, "data-link")
	rootLink := filepath.Join(baseDir, "content-link")
	if err := os.Symlink(dataDir, dataLink); err != nil {
		t.Fatalf("symlink data dir: %v", err)
	}
	if err := os.Symlink(rootDir, rootLink); err != nil {
		t.Fatalf("symlink root dir: %v", err)
	}
	port := freeTCPPort(t)
	first := startLeafwikiHelperWithStdin(t, []string{
		"--mcp=stdio",
		"--disable-auth",
		"--data-dir", dataLink,
		"--root-dir", rootLink,
		"--host", "127.0.0.1",
		"--port", port,
	}, nil, stdinReader)
	waitForLeafwikiReady(t, first, port)

	stdout, stderr, err := runLeafwikiHelperWithTimeout(t, []string{
		"--mcp=stdio",
		"--disable-auth",
		"--data-dir", dataDir,
		"--root-dir", rootDir,
		"--host", "127.0.0.1",
		"--port", port,
	}, nil, 5*time.Second)

	if err != nil {
		t.Fatalf("canonical path variant should attach, got %v\nstdout:\n%s\nstderr:\n%s", err, stdout, stderr)
	}
	if strings.Contains(stderr, "log-file") || strings.Contains(stderr, "project daemon config mismatch") {
		t.Fatalf("stderr = %q, want no log-file config mismatch", stderr)
	}
}

func TestMainProcess_AuthEnabledProjectDaemonDescriptorOmitsBootstrapSecretFingerprints(t *testing.T) {
	baseDir := t.TempDir()
	dataDir := filepath.Join(baseDir, "data")
	rootDir := filepath.Join(baseDir, "content")
	jwtSecret := "descriptor-jwt-secret"
	adminPassword := "descriptor-admin-password"
	port := freeTCPPort(t)
	proc := startLeafwikiHelper(t, []string{
		"--data-dir", dataDir,
		"--root-dir", rootDir,
		"--host", "127.0.0.1",
		"--port", port,
		"--jwt-secret", jwtSecret,
		"--admin-password", adminPassword,
		"--allow-insecure",
		"--log-target", "stderr",
	}, nil)

	waitForLeafwikiReady(t, proc, port)
	descriptorPath := filepath.Join(dataDir, ".leafwiki", "project-daemon.json")
	waitForFileContaining(t, descriptorPath, `"configHash"`)
	raw := readFileString(t, descriptorPath)
	for _, unexpected := range []string{
		jwtSecret,
		adminPassword,
		sha256Hex(jwtSecret),
		sha256Hex(adminPassword),
		"jwtSecretHash",
		"adminPasswordHash",
	} {
		if strings.Contains(raw, unexpected) {
			t.Fatalf("descriptor leaked bootstrap secret material %q:\n%s", unexpected, raw)
		}
	}
}

func TestMainProcess_NativeStdioCloseKeepsOwnerAliveUntilIdleTimeout(t *testing.T) {
	stdinReader, stdinWriter := io.Pipe()
	baseDir := t.TempDir()
	dataDir := filepath.Join(baseDir, "data")
	rootDir := filepath.Join(baseDir, "content")
	port := freeTCPPort(t)
	proc := startLeafwikiHelperWithStdin(t, []string{
		"--mcp=stdio",
		"--disable-auth",
		"--data-dir", dataDir,
		"--root-dir", rootDir,
		"--host", "127.0.0.1",
		"--port", port,
		"--log-target", "stderr",
	}, map[string]string{"LEAFWIKI_DAEMON_IDLE_TIMEOUT": "1s"}, stdinReader)

	waitForLeafwikiReady(t, proc, port)
	if err := stdinWriter.Close(); err != nil {
		t.Fatalf("close stdin writer: %v", err)
	}
	proc.waitForExit(t)
	waitForLeafwikiReady(t, proc, port)
	waitForLeafwikiUnavailable(t, port)
	waitForProjectLocksReusable(t, dataDir, rootDir, 15*time.Second)
}

func TestMainProcess_CrashedNativeStdioFrontendExpiresHeartbeatAndReleasesLocks(t *testing.T) {
	stdinReader, stdinWriter := io.Pipe()
	baseDir := t.TempDir()
	dataDir := filepath.Join(baseDir, "data")
	rootDir := filepath.Join(baseDir, "content")
	port := freeTCPPort(t)
	proc := startLeafwikiHelperWithStdin(t, []string{
		"--mcp=stdio",
		"--disable-auth",
		"--data-dir", dataDir,
		"--root-dir", rootDir,
		"--host", "127.0.0.1",
		"--port", port,
		"--log-target", "stderr",
	}, map[string]string{"LEAFWIKI_DAEMON_IDLE_TIMEOUT": "1s"}, stdinReader)
	waitForLeafwikiReady(t, proc, port)

	proc.cancel()
	_ = stdinWriter.Close()
	_ = proc.cmd.Wait()
	proc.stopped = true

	waitForLeafwikiUnavailableWithin(t, port, 25*time.Second)
	waitForProjectLocksReusable(t, dataDir, rootDir, 15*time.Second)
}

func TestMainProcess_NativeStdioPipedOutputExitsBeforeOwnerIdleTimeout(t *testing.T) {
	baseDir := t.TempDir()
	dataDir := filepath.Join(baseDir, "data")
	rootDir := filepath.Join(baseDir, "content")
	port := freeTCPPort(t)
	stdout, stderr, err := runLeafwikiHelperWithTimeout(t, []string{
		"--mcp=stdio",
		"--disable-auth",
		"--data-dir", dataDir,
		"--root-dir", rootDir,
		"--host", "127.0.0.1",
		"--port", port,
		"--log-target", "stderr",
	}, map[string]string{"LEAFWIKI_DAEMON_IDLE_TIMEOUT": "3s"}, 1500*time.Millisecond)

	if err != nil {
		t.Fatalf("native STDIO frontend should exit before owner idle timeout, got %v\nstdout:\n%s\nstderr:\n%s", err, stdout, stderr)
	}
	if stdout != "" {
		t.Fatalf("stdout = %q, want empty without MCP frames", stdout)
	}
	waitForLeafwikiReadyWithDiagnostics(t, port, stdout, stderr)
	waitForLeafwikiUnavailable(t, port)
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

func TestMainProcess_RepeatedCombinedNativeStdioHTTPStderrLoggingAttaches(t *testing.T) {
	stdinReader, stdinWriter := io.Pipe()
	defer stdinWriter.Close()
	baseDir := t.TempDir()
	dataDir := filepath.Join(baseDir, "data")
	rootDir := filepath.Join(baseDir, "content")
	port := freeTCPPort(t)
	first := startLeafwikiHelperWithStdin(t, []string{
		"--mcp=stdio,http",
		"--disable-auth",
		"--data-dir", dataDir,
		"--root-dir", rootDir,
		"--host", "127.0.0.1",
		"--port", port,
		"--log-target", "stderr",
	}, nil, stdinReader)
	waitForLeafwikiReady(t, first, port)

	stdout, stderr, err := runLeafwikiHelperWithTimeout(t, []string{
		"--mcp=stdio,http",
		"--disable-auth",
		"--data-dir", dataDir,
		"--root-dir", rootDir,
		"--host", "127.0.0.1",
		"--port", port,
		"--log-target", "stderr",
	}, nil, 5*time.Second)

	if err != nil {
		t.Fatalf("repeated combined STDIO+HTTP startup should attach, got %v\nstdout:\n%s\nstderr:\n%s", err, stdout, stderr)
	}
	if stdout != "" {
		t.Fatalf("stdout = %q, want empty without MCP frames", stdout)
	}
	if strings.Contains(stderr, "project daemon config mismatch") {
		t.Fatalf("stderr = %q, want no logging config mismatch", stderr)
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

func TestMainProcess_NativeStdioRejectsSecondProcessWithConfigMismatch(t *testing.T) {
	stdinReader, stdinWriter := io.Pipe()
	defer stdinWriter.Close()
	baseDir := t.TempDir()
	dataDir := filepath.Join(baseDir, "data")
	rootDir := filepath.Join(baseDir, "content")
	rawSecret := "jwt_secret_should_not_leak"
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
	}, map[string]string{"LEAFWIKI_JWT_SECRET": rawSecret}, 5*time.Second)

	if err == nil {
		t.Fatalf("expected second process with config mismatch to exit non-zero")
	}
	if errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("second process did not exit; expected config mismatch\nstdout:\n%s\nstderr:\n%s", stdout, stderr)
	}
	if stdout != "" {
		t.Fatalf("stdout = %q, want empty", stdout)
	}
	if !strings.Contains(stderr, "project daemon config mismatch") || !strings.Contains(stderr, "port") {
		t.Fatalf("stderr = %q, want redacted port config mismatch", stderr)
	}
	if strings.Contains(stderr, rawSecret) {
		t.Fatalf("stderr leaked raw secret: %q", stderr)
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
	}, nil, 12*time.Second)

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

func TestWaitForProjectDaemonConcurrentStartupAttachesToWinningOwnerAfterSpawnError(t *testing.T) {
	baseDir := t.TempDir()
	dataDir := filepath.Join(baseDir, "data")
	rootDir := filepath.Join(baseDir, "content")
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		t.Fatalf("create data dir: %v", err)
	}
	if err := os.MkdirAll(rootDir, 0o755); err != nil {
		t.Fatalf("create root dir: %v", err)
	}
	canonicalData, canonicalRoot, err := projectdaemon.CanonicalizeProject(dataDir, rootDir)
	if err != nil {
		t.Fatalf("canonicalize project: %v", err)
	}
	ownerCfg := projectdaemon.Config{
		DataDir:           canonicalData,
		RootDir:           canonicalRoot,
		AuthDisabled:      true,
		PublicMCPEnabled:  false,
		Host:              "127.0.0.1",
		Port:              "8080",
		DaemonIdleTimeout: "10m0s",
	}
	hash, err := projectdaemon.ConfigHash(ownerCfg)
	if err != nil {
		t.Fatalf("ConfigHash: %v", err)
	}
	dataLock, err := locking.AcquireDataDirLock(canonicalData)
	if err != nil {
		t.Fatalf("acquire fake owner data lock: %v", err)
	}
	defer dataLock.Release()
	rootLock, err := locking.AcquireRootDirLock(canonicalRoot)
	if err != nil {
		t.Fatalf("acquire fake owner root lock: %v", err)
	}
	defer rootLock.Release()
	token := "control-token"
	control := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.Header.Get(projectdaemon.ControlTokenHeader) != token {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		if req.Method == http.MethodGet && req.URL.Path == "/health" {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(projectdaemon.DaemonHealth{
				OK:            true,
				SchemaVersion: projectdaemon.DescriptorSchemaVersion,
				PID:           os.Getpid(),
				DataDir:       canonicalData,
				RootDir:       canonicalRoot,
				ConfigHash:    hash,
			})
			return
		}
		http.NotFound(w, req)
	}))
	t.Cleanup(control.Close)
	descriptorPath := projectdaemon.DescriptorPath(canonicalData)
	errorPath := filepath.Join(t.TempDir(), "startup.err")
	if err := os.WriteFile(errorPath, []byte("acquire data directory lock: data directory is already in use"), 0o600); err != nil {
		t.Fatalf("write startup error: %v", err)
	}
	go func() {
		time.Sleep(2200 * time.Millisecond)
		_ = projectdaemon.WriteDescriptorAtomic(descriptorPath, &projectdaemon.Descriptor{
			SchemaVersion:    projectdaemon.DescriptorSchemaVersion,
			PID:              os.Getpid(),
			StartedAt:        time.Now().UTC(),
			DataDir:          canonicalData,
			RootDir:          canonicalRoot,
			PublicURL:        "http://127.0.0.1:8080",
			PublicMCPEnabled: false,
			ControlURL:       control.URL,
			ConfigHash:       hash,
			IdleTimeout:      "10m0s",
			ControlToken:     token,
			Config:           ownerCfg,
		})
	}()

	desc, err := waitForProjectDaemon(context.Background(), descriptorPath, errorPath, ownerCfg, mcpTransports{Stdio: true})
	if err != nil {
		t.Fatalf("waitForProjectDaemon should attach to winning owner after startup error, got %v", err)
	}
	if desc.ControlURL != control.URL {
		t.Fatalf("attached descriptor control URL = %q, want %q", desc.ControlURL, control.URL)
	}
}

func TestWaitForProjectDaemonConcurrentStartupHandlesStructuredLockError(t *testing.T) {
	baseDir := t.TempDir()
	dataDir := filepath.Join(baseDir, "data")
	rootDir := filepath.Join(baseDir, "content")
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		t.Fatalf("create data dir: %v", err)
	}
	if err := os.MkdirAll(rootDir, 0o755); err != nil {
		t.Fatalf("create root dir: %v", err)
	}
	canonicalData, canonicalRoot, err := projectdaemon.CanonicalizeProject(dataDir, rootDir)
	if err != nil {
		t.Fatalf("canonicalize project: %v", err)
	}
	ownerCfg := projectdaemon.Config{
		DataDir:           canonicalData,
		RootDir:           canonicalRoot,
		AuthDisabled:      true,
		Host:              "127.0.0.1",
		Port:              "8080",
		DaemonIdleTimeout: "10m0s",
	}
	hash, err := projectdaemon.ConfigHash(ownerCfg)
	if err != nil {
		t.Fatalf("ConfigHash: %v", err)
	}
	dataLock, err := locking.AcquireDataDirLock(canonicalData)
	if err != nil {
		t.Fatalf("acquire fake owner data lock: %v", err)
	}
	defer dataLock.Release()
	rootLock, err := locking.AcquireRootDirLock(canonicalRoot)
	if err != nil {
		t.Fatalf("acquire fake owner root lock: %v", err)
	}
	defer rootLock.Release()
	token := "control-token"
	control := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.Header.Get(projectdaemon.ControlTokenHeader) != token {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		if req.Method == http.MethodGet && req.URL.Path == "/health" {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(projectdaemon.DaemonHealth{
				OK:            true,
				SchemaVersion: projectdaemon.DescriptorSchemaVersion,
				PID:           os.Getpid(),
				DataDir:       canonicalData,
				RootDir:       canonicalRoot,
				ConfigHash:    hash,
			})
			return
		}
		http.NotFound(w, req)
	}))
	t.Cleanup(control.Close)
	descriptorPath := projectdaemon.DescriptorPath(canonicalData)
	errorPath := filepath.Join(t.TempDir(), "startup.err")
	rawErr, err := json.Marshal(projectDaemonStartupError{
		Kind:    projectDaemonStartupErrorKindLock,
		Message: "acquire data directory lock: data directory is already in use",
	})
	if err != nil {
		t.Fatalf("marshal startup error: %v", err)
	}
	if err := os.WriteFile(errorPath, rawErr, 0o600); err != nil {
		t.Fatalf("write startup error: %v", err)
	}
	go func() {
		time.Sleep(2200 * time.Millisecond)
		_ = projectdaemon.WriteDescriptorAtomic(descriptorPath, &projectdaemon.Descriptor{
			SchemaVersion:    projectdaemon.DescriptorSchemaVersion,
			PID:              os.Getpid(),
			StartedAt:        time.Now().UTC(),
			DataDir:          canonicalData,
			RootDir:          canonicalRoot,
			PublicURL:        "http://127.0.0.1:8080",
			PublicMCPEnabled: false,
			ControlURL:       control.URL,
			ConfigHash:       hash,
			IdleTimeout:      "10m0s",
			ControlToken:     token,
			Config:           ownerCfg,
		})
	}()

	desc, err := waitForProjectDaemon(context.Background(), descriptorPath, errorPath, ownerCfg, mcpTransports{})
	if err != nil {
		t.Fatalf("waitForProjectDaemon should attach after structured lock startup error, got %v", err)
	}
	if desc.ControlURL != control.URL {
		t.Fatalf("attached descriptor control URL = %q, want %q", desc.ControlURL, control.URL)
	}
}

func TestWaitForProjectDaemonReportsNonLockStartupErrorDirectly(t *testing.T) {
	baseDir := t.TempDir()
	dataDir := filepath.Join(baseDir, "data")
	rootDir := filepath.Join(baseDir, "content")
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		t.Fatalf("create data dir: %v", err)
	}
	if err := os.MkdirAll(rootDir, 0o755); err != nil {
		t.Fatalf("create root dir: %v", err)
	}
	canonicalData, canonicalRoot, err := projectdaemon.CanonicalizeProject(dataDir, rootDir)
	if err != nil {
		t.Fatalf("canonicalize project: %v", err)
	}
	ownerCfg := projectdaemon.Config{
		DataDir:           canonicalData,
		RootDir:           canonicalRoot,
		AuthDisabled:      true,
		Host:              "127.0.0.1",
		Port:              "8080",
		DaemonIdleTimeout: "10m0s",
	}
	errorPath := filepath.Join(t.TempDir(), "startup.err")
	if err := os.WriteFile(errorPath, []byte("start HTTP listener: listen tcp 127.0.0.1:8080: bind: address already in use"), 0o600); err != nil {
		t.Fatalf("write startup error: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	_, err = waitForProjectDaemon(ctx, projectdaemon.DescriptorPath(canonicalData), errorPath, ownerCfg, mcpTransports{})
	if err == nil {
		t.Fatalf("waitForProjectDaemon unexpectedly succeeded")
	}
	if strings.Contains(err.Error(), "project is locked but no attachable daemon was found") {
		t.Fatalf("error = %v, want direct non-lock startup failure", err)
	}
	if !strings.Contains(err.Error(), "project daemon failed to start") || !strings.Contains(err.Error(), "bind: address already in use") {
		t.Fatalf("error = %v, want bind startup failure", err)
	}
}

func TestWaitForProjectDaemonReportsStructuredNonLockStartupErrorDirectly(t *testing.T) {
	baseDir := t.TempDir()
	dataDir := filepath.Join(baseDir, "data")
	rootDir := filepath.Join(baseDir, "content")
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		t.Fatalf("create data dir: %v", err)
	}
	if err := os.MkdirAll(rootDir, 0o755); err != nil {
		t.Fatalf("create root dir: %v", err)
	}
	canonicalData, canonicalRoot, err := projectdaemon.CanonicalizeProject(dataDir, rootDir)
	if err != nil {
		t.Fatalf("canonicalize project: %v", err)
	}
	ownerCfg := projectdaemon.Config{
		DataDir:           canonicalData,
		RootDir:           canonicalRoot,
		AuthDisabled:      true,
		Host:              "127.0.0.1",
		Port:              "8080",
		DaemonIdleTimeout: "10m0s",
	}
	errorPath := filepath.Join(t.TempDir(), "startup.err")
	rawErr, err := json.Marshal(projectDaemonStartupError{
		Kind:    projectDaemonStartupErrorKindStartup,
		Message: "start HTTP listener: listen tcp 127.0.0.1:8080: bind: address already in use",
	})
	if err != nil {
		t.Fatalf("marshal startup error: %v", err)
	}
	if err := os.WriteFile(errorPath, rawErr, 0o600); err != nil {
		t.Fatalf("write startup error: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	_, err = waitForProjectDaemon(ctx, projectdaemon.DescriptorPath(canonicalData), errorPath, ownerCfg, mcpTransports{})
	if err == nil {
		t.Fatalf("waitForProjectDaemon unexpectedly succeeded")
	}
	if strings.Contains(err.Error(), "project is locked but no attachable daemon was found") {
		t.Fatalf("error = %v, want direct non-lock startup failure", err)
	}
	if strings.Contains(err.Error(), `"kind"`) || strings.Contains(err.Error(), `"message"`) {
		t.Fatalf("error = %v, want structured startup message without raw JSON", err)
	}
	if !strings.Contains(err.Error(), "project daemon failed to start") || !strings.Contains(err.Error(), "bind: address already in use") {
		t.Fatalf("error = %v, want bind startup failure", err)
	}
}

func TestCompareProjectDaemonConfigForStdioOnlyAttachIgnoresOwnerLoggingSettings(t *testing.T) {
	owner := projectdaemon.Config{
		DataDir:           "/tmp/leafwiki-data",
		RootDir:           "/tmp/leafwiki-root",
		AuthDisabled:      true,
		Host:              "127.0.0.1",
		Port:              "8080",
		LogTarget:         "stderr",
		LogFile:           "",
		DisableRequestLog: false,
		DaemonIdleTimeout: "10m0s",
	}
	requested := owner
	requested.LogTarget = "file"
	requested.LogFile = "/tmp/leafwiki-data/.leafwiki/logs/leafwiki.log"
	requested.DisableRequestLog = true

	mismatches := compareProjectDaemonConfigForRequest(owner, requested, mcpTransports{Stdio: true})

	if len(mismatches) > 0 {
		t.Fatalf("mismatches = %#v, want STDIO-only attach to ignore owner logging settings", mismatches)
	}
}

func TestCompareProjectDaemonConfigForPlainServerPreservesPublicMCPMismatch(t *testing.T) {
	owner := projectdaemon.Config{
		DataDir:          "/tmp/leafwiki-data",
		RootDir:          "/tmp/leafwiki-root",
		AuthDisabled:     true,
		PublicMCPEnabled: true,
		Host:             "127.0.0.1",
		Port:             "8080",
	}
	requested := owner
	requested.PublicMCPEnabled = false

	mismatches := compareProjectDaemonConfigForRequest(owner, requested, mcpTransports{})

	if len(mismatches) != 1 || mismatches[0].Field != "public-mcp-enabled" {
		t.Fatalf("mismatches = %#v, want public MCP mismatch for plain server startup", mismatches)
	}
}

func TestDaemonConfigForRuntimeIncludesWorkspaceSync(t *testing.T) {
	baseDir := t.TempDir()
	cfg := testRuntimeConfig(
		filepath.Join(baseDir, "data"),
		filepath.Join(baseDir, "content"),
		"8080",
		mcpTransports{},
		true,
	)
	cfg.EnableWorkspaceSync = true

	daemonCfg, err := daemonConfigForRuntime(cfg)
	if err != nil {
		t.Fatalf("daemon config: %v", err)
	}

	if !daemonCfg.EnableWorkspaceSync {
		t.Fatalf("EnableWorkspaceSync = false, want true")
	}
}

func TestCompareProjectDaemonConfigForRequestCoversDaemonRelevantFields(t *testing.T) {
	owner := completeDaemonCompareConfig()
	tests := []struct {
		name  string
		field string
		mut   func(*projectdaemon.Config)
	}{
		{name: "data dir", field: "data-dir", mut: func(cfg *projectdaemon.Config) { cfg.DataDir = "/tmp/other-data" }},
		{name: "root dir", field: "root-dir", mut: func(cfg *projectdaemon.Config) { cfg.RootDir = "/tmp/other-root" }},
		{name: "auth mode", field: "auth-disabled", mut: func(cfg *projectdaemon.Config) { cfg.AuthDisabled = !cfg.AuthDisabled }},
		{name: "public MCP", field: "public-mcp-enabled", mut: func(cfg *projectdaemon.Config) { cfg.PublicMCPEnabled = !cfg.PublicMCPEnabled }},
		{name: "host", field: "host", mut: func(cfg *projectdaemon.Config) { cfg.Host = "127.0.0.2" }},
		{name: "port", field: "port", mut: func(cfg *projectdaemon.Config) { cfg.Port = "9090" }},
		{name: "base path", field: "base-path", mut: func(cfg *projectdaemon.Config) { cfg.BasePath = "/docs" }},
		{name: "public access", field: "public-access", mut: func(cfg *projectdaemon.Config) { cfg.PublicAccess = !cfg.PublicAccess }},
		{name: "allow insecure", field: "allow-insecure", mut: func(cfg *projectdaemon.Config) { cfg.AllowInsecure = !cfg.AllowInsecure }},
		{name: "access token timeout", field: "access-token-timeout", mut: func(cfg *projectdaemon.Config) { cfg.AccessTokenTimeout = "2h0m0s" }},
		{name: "refresh token timeout", field: "refresh-token-timeout", mut: func(cfg *projectdaemon.Config) { cfg.RefreshTokenTimeout = "720h0m0s" }},
		{name: "injected header hash", field: "inject-code-in-header-hash", mut: func(cfg *projectdaemon.Config) { cfg.InjectCodeInHeaderHash = "other-hash" }},
		{name: "custom stylesheet", field: "custom-stylesheet", mut: func(cfg *projectdaemon.Config) { cfg.CustomStylesheet = "/tmp/custom.css" }},
		{name: "log target", field: "log-target", mut: func(cfg *projectdaemon.Config) { cfg.LogTarget = "file" }},
		{name: "log file", field: "log-file", mut: func(cfg *projectdaemon.Config) { cfg.LogFile = "/tmp/leafwiki.log" }},
		{name: "hide metadata", field: "hide-link-metadata-section", mut: func(cfg *projectdaemon.Config) { cfg.HideLinkMetadataSection = !cfg.HideLinkMetadataSection }},
		{name: "upload size", field: "max-asset-upload-size-bytes", mut: func(cfg *projectdaemon.Config) { cfg.MaxAssetUploadSizeBytes = 99 }},
		{name: "revision", field: "enable-revision", mut: func(cfg *projectdaemon.Config) { cfg.EnableRevision = !cfg.EnableRevision }},
		{name: "link refactor", field: "enable-link-refactor", mut: func(cfg *projectdaemon.Config) { cfg.EnableLinkRefactor = !cfg.EnableLinkRefactor }},
		{name: "revision history", field: "max-revision-history", mut: func(cfg *projectdaemon.Config) { cfg.MaxRevisionHistory = 7 }},
		{name: "remote user enabled", field: "enable-http-remote-user", mut: func(cfg *projectdaemon.Config) { cfg.EnableHTTPRemoteUser = !cfg.EnableHTTPRemoteUser }},
		{name: "remote user header", field: "http-remote-user-header", mut: func(cfg *projectdaemon.Config) { cfg.HTTPRemoteUserHeader = "X-User" }},
		{name: "trusted proxies", field: "trusted-proxy-ips", mut: func(cfg *projectdaemon.Config) { cfg.TrustedProxyIPs = "127.0.0.1/32" }},
		{name: "remote user logout", field: "http-remote-user-logout-url", mut: func(cfg *projectdaemon.Config) { cfg.HTTPRemoteUserLogoutURL = "https://example.test/logout" }},
		{name: "request log", field: "disable-request-log", mut: func(cfg *projectdaemon.Config) { cfg.DisableRequestLog = !cfg.DisableRequestLog }},
		{name: "idle timeout", field: "daemon-idle-timeout", mut: func(cfg *projectdaemon.Config) { cfg.DaemonIdleTimeout = "1m0s" }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			requested := owner
			tt.mut(&requested)

			mismatches := compareProjectDaemonConfigForRequest(owner, requested, mcpTransports{HTTP: true})

			if len(mismatches) != 1 || mismatches[0].Field != tt.field {
				t.Fatalf("mismatches = %#v, want one %q mismatch", mismatches, tt.field)
			}
		})
	}
}

func TestCompareProjectDaemonConfigForStdioOnlyAttachDocumentsIgnoredFields(t *testing.T) {
	owner := completeDaemonCompareConfig()
	requested := owner
	requested.PublicMCPEnabled = !owner.PublicMCPEnabled
	requested.Host = "0.0.0.0"
	requested.LogTarget = "file"
	requested.LogFile = "/tmp/leafwiki.log"
	requested.DisableRequestLog = !owner.DisableRequestLog

	mismatches := compareProjectDaemonConfigForRequest(owner, requested, mcpTransports{Stdio: true})

	if len(mismatches) > 0 {
		t.Fatalf("mismatches = %#v, want STDIO-only attach to inherit public MCP/logging/request-log settings", mismatches)
	}
}

func TestDaemonStdioBridgeHTTPClientHasNoFullRequestTimeout(t *testing.T) {
	client := daemonStdioBridgeHTTPClient(daemonStdioBridge{
		ControlToken: "control-token",
		APIKey:       "stdio-api-key",
	})

	if client == nil {
		t.Fatalf("daemonStdioBridgeHTTPClient returned nil")
	}
	if client.Timeout != 0 {
		t.Fatalf("HTTP client Timeout = %v, want zero so MCP request contexts control cancellation", client.Timeout)
	}
	authTransport, ok := client.Transport.(projectdaemon.AuthRoundTripper)
	if !ok {
		t.Fatalf("HTTP client transport = %T, want projectdaemon.AuthRoundTripper", client.Transport)
	}
	if authTransport.ControlToken != "control-token" || authTransport.BearerToken != "stdio-api-key" {
		t.Fatalf("auth transport = %#v, want bridge credentials installed", authTransport)
	}
}

func TestRunDaemonHeartbeatReturnsControlErrors(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		http.Error(w, "session not found", http.StatusNotFound)
	}))
	t.Cleanup(server.Close)

	client := projectdaemon.NewClient(server.URL, "control-token")
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	err := runDaemonHeartbeat(ctx, client, "missing-session", 10*time.Millisecond)

	if err == nil {
		t.Fatalf("runDaemonHeartbeat returned nil, want control error")
	}
	if !strings.Contains(err.Error(), "session not found") {
		t.Fatalf("heartbeat error = %v, want session not found", err)
	}
}

func TestIdleShutdownCallbackIgnoresStaleZeroNotificationWithActiveSession(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	currentCount := 1
	callback := idleShutdownCallback(ctx, cancel, 0, func() int {
		return currentCount
	})

	callback(1)
	callback(0)
	select {
	case <-ctx.Done():
		t.Fatalf("stale zero notification canceled active daemon")
	case <-time.After(25 * time.Millisecond):
	}

	currentCount = 0
	callback(0)
	select {
	case <-ctx.Done():
	case <-time.After(time.Second):
		t.Fatalf("current zero notification did not cancel daemon")
	}
}

func TestCancelIfNoSessionAfterStartupGraceWaitsForFirstSession(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	registry := projectdaemon.NewSessionRegistry(time.Second, nil)

	done := make(chan struct{})
	go func() {
		cancelIfNoSessionAfterStartupGrace(ctx, cancel, registry, 25*time.Millisecond)
		close(done)
	}()

	id, err := registry.Register()
	if err != nil {
		t.Fatalf("register first session: %v", err)
	}
	registry.Release(id)
	select {
	case <-ctx.Done():
		t.Fatalf("startup grace canceled after a first session registered")
	case <-done:
	case <-time.After(100 * time.Millisecond):
		t.Fatalf("startup grace goroutine did not exit after first session registered")
	}
}

func TestCancelIfNoSessionAfterStartupGraceCancelsWhenNoSessionRegisters(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	registry := projectdaemon.NewSessionRegistry(time.Second, nil)
	go cancelIfNoSessionAfterStartupGrace(ctx, cancel, registry, 10*time.Millisecond)

	select {
	case <-ctx.Done():
	case <-time.After(250 * time.Millisecond):
		t.Fatalf("startup grace did not cancel idle daemon with no sessions")
	}
}

func TestProjectDaemonActivityCountCombinesSessionsAndAgentPresence(t *testing.T) {
	sessions := projectdaemon.NewSessionRegistry(time.Second, nil)
	presence := projectdaemon.NewAgentPresenceRegistry(time.Minute, nil)

	if got := projectDaemonActivityCount(sessions, presence); got != 0 {
		t.Fatalf("activity count = %d, want 0", got)
	}
	handle, err := sessions.Register()
	if err != nil {
		t.Fatalf("register session: %v", err)
	}
	presence.Record(agenthooks.Event{
		Provider:      agenthooks.ProviderCodex,
		SessionIDHash: agentHookSessionHash(agenthooks.ProviderCodex, "codex"),
		EventName:     "SessionStart",
		SeenAt:        time.Now(),
	})
	if got := projectDaemonActivityCount(sessions, presence); got != 2 {
		t.Fatalf("activity count = %d, want 2", got)
	}
	sessions.Release(handle)
	if got := projectDaemonActivityCount(sessions, presence); got != 1 {
		t.Fatalf("activity count after session release = %d, want 1", got)
	}
}

func TestCancelIfNoActivityAfterStartupGraceWaitsForFirstAgentPresence(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	sessions := projectdaemon.NewSessionRegistry(time.Second, nil)
	presence := projectdaemon.NewAgentPresenceRegistry(time.Minute, nil)

	done := make(chan struct{})
	go func() {
		cancelIfNoActivityAfterStartupGrace(ctx, cancel, sessions, presence, 25*time.Millisecond)
		close(done)
	}()

	presence.Record(agenthooks.Event{
		Provider:      agenthooks.ProviderCodex,
		SessionIDHash: agentHookSessionHash(agenthooks.ProviderCodex, "codex"),
		EventName:     "SessionStart",
		SeenAt:        time.Now(),
	})
	select {
	case <-ctx.Done():
		t.Fatalf("startup grace canceled after first agent presence")
	case <-done:
	case <-time.After(100 * time.Millisecond):
		t.Fatalf("startup grace goroutine did not exit after first agent presence")
	}
}

func TestCancelIfNoActivityAfterStartupGraceIgnoresMissingAgentEnd(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	sessions := projectdaemon.NewSessionRegistry(time.Second, nil)
	presence := projectdaemon.NewAgentPresenceRegistry(time.Minute, nil)
	event, ok := agenthooks.Normalize(
		agenthooks.ProviderClaude,
		[]byte(`{"hook_event_name":"SessionEnd","session_id":"ended-before-start"}`),
		time.Now(),
	)
	if !ok {
		t.Fatalf("Normalize returned false")
	}
	presence.Record(event)

	go cancelIfNoActivityAfterStartupGrace(ctx, cancel, sessions, presence, 10*time.Millisecond)

	select {
	case <-ctx.Done():
	case <-time.After(250 * time.Millisecond):
		t.Fatalf("startup grace did not cancel after missing agent end event")
	}
}

func TestDaemonOwnerEnvOmitsSessionAndBootstrapSecrets(t *testing.T) {
	t.Setenv("LEAFWIKI_MCP_API_KEY", "lwk_secret")
	t.Setenv("LEAFWIKI_RUN_MCP_API_KEY", "lwk_run_secret")
	t.Setenv("LEAFWIKI_JWT_SECRET", "jwt-secret")
	t.Setenv("LEAFWIKI_RUN_MCP_JWT_SECRET", "run-jwt-secret")
	t.Setenv("LEAFWIKI_ADMIN_PASSWORD", "admin-password")
	t.Setenv("LEAFWIKI_RUN_MCP_ADMIN_PASSWORD", "run-admin-password")
	t.Setenv("LEAFWIKI_BASE_PATH", "/wiki")

	joined := strings.Join(daemonOwnerEnv(), "\n")
	for _, unexpected := range []string{
		"LEAFWIKI_MCP_API_KEY=",
		"LEAFWIKI_RUN_MCP_API_KEY=",
		"LEAFWIKI_JWT_SECRET=",
		"LEAFWIKI_RUN_MCP_JWT_SECRET=",
		"LEAFWIKI_ADMIN_PASSWORD=",
		"LEAFWIKI_RUN_MCP_ADMIN_PASSWORD=",
		"lwk_secret",
		"jwt-secret",
		"admin-password",
	} {
		if strings.Contains(joined, unexpected) {
			t.Fatalf("daemon owner env retained secret %q:\n%s", unexpected, joined)
		}
	}
	if !strings.Contains(joined, "LEAFWIKI_BASE_PATH=/wiki") {
		t.Fatalf("daemon owner env lost non-secret LeafWiki setting:\n%s", joined)
	}
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

func TestMainProcess_ForegroundServerSignalLeavesDetachedOwnerUntilIdleTimeout(t *testing.T) {
	if !supportsProcessGroupSignal() {
		t.Skip("process-group signaling is not available on this platform")
	}

	baseDir := t.TempDir()
	dataDir := filepath.Join(baseDir, "data")
	rootDir := filepath.Join(baseDir, "content")
	port := freeTCPPort(t)
	proc := startLeafwikiHelperInProcessGroup(t, []string{
		"--disable-auth",
		"--data-dir", dataDir,
		"--root-dir", rootDir,
		"--host", "127.0.0.1",
		"--port", port,
		"--log-target", "stderr",
	}, map[string]string{"LEAFWIKI_DAEMON_IDLE_TIMEOUT": "1s"})

	waitForLeafwikiReady(t, proc, port)
	if err := signalLeafwikiProcessGroup(proc.cmd.Process); err != nil {
		t.Fatalf("send process-group SIGTERM: %v", err)
	}
	proc.waitForExit(t)
	waitForLeafwikiReady(t, proc, port)
	waitForLeafwikiUnavailable(t, port)
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
			name: "STDIO allows non-loopback web host",
			opts: mcpTransportOptions{
				Transports:  mcpTransports{Stdio: true},
				DisableAuth: true,
				Host:        "0.0.0.0",
				LogTarget:   leaflogging.TargetStderr,
			},
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

func parseConfigFlagsForArgs(t *testing.T, args []string) (*cliFlags, map[string]bool, []string) {
	t.Helper()

	flags, visited, rest, err := parseConfigFlagsForArgsAllowError(t, args)
	if err != nil {
		t.Fatalf("applyYAMLConfigFile: %v", err)
	}
	return flags, visited, rest
}

func parseConfigFlagsForArgsAllowError(t *testing.T, args []string) (*cliFlags, map[string]bool, []string, error) {
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
	if visited["config"] {
		if err := validateConfigModeArgs(fs.Args()); err != nil {
			return flags, visited, fs.Args(), err
		}
		if err := applyYAMLConfigFile(fs, flags, visited); err != nil {
			return flags, visited, fs.Args(), err
		}
	}
	return flags, visited, fs.Args(), nil
}

func writeTestConfig(t *testing.T, path string, body string) {
	t.Helper()

	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write config %s: %v", path, err)
	}
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
	return startLeafwikiHelperWithOptions(t, args, env, stdin, leafwikiHelperStartOptions{})
}

func startLeafwikiHelperInProcessGroup(t *testing.T, args []string, env map[string]string) *leafwikiHelperProcess {
	return startLeafwikiHelperWithOptions(t, args, env, nil, leafwikiHelperStartOptions{processGroup: true})
}

type leafwikiHelperStartOptions struct {
	processGroup bool
}

func startLeafwikiHelperWithOptions(t *testing.T, args []string, env map[string]string, stdin io.Reader, opts leafwikiHelperStartOptions) *leafwikiHelperProcess {
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
	if opts.processGroup {
		configureLeafwikiHelperProcessGroup(cmd)
	}
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

	return runLeafwikiHelperWithInputAndTimeout(t, args, env, "", timeout)
}

func runLeafwikiHelperWithInputAndTimeout(t *testing.T, args []string, env map[string]string, stdin string, timeout time.Duration) (string, string, error) {
	t.Helper()

	cmdArgs := append([]string{"-test.run=TestLeafWikiHelperProcess", "--"}, args...)
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, os.Args[0], cmdArgs...)
	cmd.Env = leafwikiHelperEnv(env)
	cmd.Stdin = strings.NewReader(stdin)
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

func nativeStdioListToolsInput() string {
	return strings.Join([]string{
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-11-25","capabilities":{},"clientInfo":{"name":"leafwiki-main-test","version":"test"}}}`,
		`{"jsonrpc":"2.0","method":"notifications/initialized","params":{}}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{}}`,
		"",
	}, "\n")
}

func nativeStdioToolCallInput(id int, name string, args map[string]any) string {
	rawArgs, err := json.Marshal(args)
	if err != nil {
		panic(err)
	}
	return strings.Join([]string{
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-11-25","capabilities":{},"clientInfo":{"name":"leafwiki-main-test","version":"test"}}}`,
		`{"jsonrpc":"2.0","method":"notifications/initialized","params":{}}`,
		fmt.Sprintf(`{"jsonrpc":"2.0","id":%d,"method":"tools/call","params":{"name":%q,"arguments":%s}}`, id, name, string(rawArgs)),
		"",
	}, "\n")
}

func completeDaemonCompareConfig() projectdaemon.Config {
	return projectdaemon.Config{
		DataDir:                 "/tmp/leafwiki-data",
		RootDir:                 "/tmp/leafwiki-root",
		AuthDisabled:            false,
		PublicMCPEnabled:        false,
		Host:                    "127.0.0.1",
		Port:                    "8080",
		BasePath:                "/wiki",
		PublicAccess:            false,
		AllowInsecure:           false,
		AccessTokenTimeout:      "1h0m0s",
		RefreshTokenTimeout:     "168h0m0s",
		InjectCodeInHeaderHash:  "header-hash",
		CustomStylesheet:        "",
		LogTarget:               "stderr",
		LogFile:                 "",
		HideLinkMetadataSection: false,
		MaxAssetUploadSizeBytes: 50 << 20,
		EnableRevision:          true,
		EnableLinkRefactor:      true,
		MaxRevisionHistory:      100,
		EnableHTTPRemoteUser:    false,
		HTTPRemoteUserHeader:    "X-Remote-User",
		TrustedProxyIPs:         "",
		HTTPRemoteUserLogoutURL: "",
		DisableRequestLog:       false,
		DaemonIdleTimeout:       "10m0s",
	}
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
	if _, ok := overrides["LEAFWIKI_DAEMON_IDLE_TIMEOUT"]; !ok {
		env = append(env, "LEAFWIKI_DAEMON_IDLE_TIMEOUT=0")
	}
	for key, value := range overrides {
		env = append(env, key+"="+value)
	}
	return env
}

func waitForProjectDaemonDescriptor(t *testing.T, dataDir string) *projectdaemon.Descriptor {
	t.Helper()

	path := projectdaemon.DescriptorPath(dataDir)
	deadline := time.Now().Add(10 * time.Second)
	var lastErr error
	for time.Now().Before(deadline) {
		desc, err := projectdaemon.ReadTrustedDescriptor(path)
		if err == nil {
			return desc
		}
		lastErr = err
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatalf("project daemon descriptor %q was not readable before timeout; last error: %v", path, lastErr)
	return nil
}

func terminateProjectDaemonProcess(t *testing.T, pid int) {
	t.Helper()
	if pid <= 0 {
		return
	}
	process, err := os.FindProcess(pid)
	if err != nil {
		return
	}
	if runtime.GOOS == "windows" {
		_ = process.Kill()
		return
	}
	_ = process.Signal(os.Interrupt)
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

func waitForFileRemoved(t *testing.T, path string, timeout time.Duration) {
	t.Helper()

	deadline := time.Now().Add(timeout)
	var lastErr error
	for time.Now().Before(deadline) {
		_, err := os.Stat(path)
		if errors.Is(err, os.ErrNotExist) {
			return
		}
		lastErr = err
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatalf("%s still existed before timeout; last stat error: %v", path, lastErr)
}

func supportsGracefulProcessSignal() bool {
	return runtime.GOOS != "windows"
}

func signalLeafwikiProcess(process *os.Process) error {
	return process.Signal(os.Interrupt)
}

func supportsProcessGroupSignal() bool {
	if runtime.GOOS == "windows" {
		return false
	}
	_, err := exec.LookPath("kill")
	return err == nil
}

func configureLeafwikiHelperProcessGroup(cmd *exec.Cmd) {
	attr := &syscall.SysProcAttr{}
	if setSysProcAttrBool(attr, "Setpgid", true) {
		cmd.SysProcAttr = attr
	}
}

func signalLeafwikiProcessGroup(process *os.Process) error {
	return exec.Command("kill", "-TERM", fmt.Sprintf("-%d", process.Pid)).Run()
}

func waitForLeafwikiReady(t *testing.T, proc *leafwikiHelperProcess, port string) {
	t.Helper()
	waitForLeafwikiReadyPathWithDiagnostics(t, port, "/api/health", readFileString(t, proc.stdoutPath), readFileString(t, proc.stderrPath))
	proc.ready = true
}

func waitForLeafwikiReadyAtBasePath(t *testing.T, proc *leafwikiHelperProcess, port string, basePath string) {
	t.Helper()
	waitForLeafwikiReadyPathWithDiagnostics(t, port, strings.TrimRight(basePath, "/")+"/api/health", readFileString(t, proc.stdoutPath), readFileString(t, proc.stderrPath))
	proc.ready = true
}

func waitForLeafwikiReadyWithDiagnostics(t *testing.T, port string, stdout string, stderr string) {
	t.Helper()
	waitForLeafwikiReadyPathWithDiagnostics(t, port, "/api/health", stdout, stderr)
}

func waitForLeafwikiReadyPathWithDiagnostics(t *testing.T, port string, path string, stdout string, stderr string) {
	t.Helper()
	client := &http.Client{Timeout: 200 * time.Millisecond}
	url := "http://127.0.0.1:" + port + path
	deadline := time.Now().Add(10 * time.Second)
	var lastErr error
	for time.Now().Before(deadline) {
		resp, err := client.Get(url)
		if err == nil {
			_, _ = io.Copy(io.Discard, resp.Body)
			_ = resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return
			}
			lastErr = errors.New(resp.Status)
		} else {
			lastErr = err
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatalf("LeafWiki did not become ready at %s: %v\nstdout:\n%s\nstderr:\n%s", url, lastErr, stdout, stderr)
}

func waitForLeafwikiUnavailable(t *testing.T, port string) {
	t.Helper()

	waitForLeafwikiUnavailableWithin(t, port, 5*time.Second)
}

func waitForLeafwikiUnavailableWithin(t *testing.T, port string, timeout time.Duration) {
	t.Helper()

	client := &http.Client{Timeout: 200 * time.Millisecond}
	url := "http://127.0.0.1:" + port + "/api/health"
	deadline := time.Now().Add(timeout)
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

func waitForProjectLocksReusable(t *testing.T, dataDir string, rootDir string, timeout time.Duration) {
	t.Helper()

	canonicalData, canonicalRoot, err := projectdaemon.CanonicalizeProject(dataDir, rootDir)
	if err != nil {
		t.Fatalf("canonicalize project locks: %v", err)
	}
	deadline := time.Now().Add(timeout)
	var lastErr error
	for time.Now().Before(deadline) {
		dataLock, err := locking.AcquireDataDirLock(canonicalData)
		if err != nil {
			lastErr = err
			time.Sleep(25 * time.Millisecond)
			continue
		}
		rootLock, err := locking.AcquireRootDirLock(canonicalRoot)
		if err != nil {
			_ = dataLock.Release()
			lastErr = err
			time.Sleep(25 * time.Millisecond)
			continue
		}
		_ = rootLock.Release()
		_ = dataLock.Release()
		return
	}
	t.Fatalf("project locks were not reusable before timeout: %v", lastErr)
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

func assertFileMode(t *testing.T, path string, want os.FileMode) {
	t.Helper()

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat %s: %v", path, err)
	}
	if got := info.Mode().Perm(); got != want {
		t.Fatalf("%s mode = %04o, want %04o", path, got, want)
	}
}

func findLeafwikiDaemonStartupConfigContaining(t *testing.T, marker string) string {
	t.Helper()

	matches, err := filepath.Glob(filepath.Join(os.TempDir(), "leafwiki-project-daemon-*.json"))
	if err != nil {
		t.Fatalf("glob daemon startup configs: %v", err)
	}
	for _, path := range matches {
		raw, err := os.ReadFile(path)
		if err == nil && strings.Contains(string(raw), marker) {
			return path
		}
	}
	return ""
}

func sha256Hex(value string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(value)))
	return hex.EncodeToString(sum[:])
}

func agentHookSessionHash(provider, rawSessionID string) string {
	sum := sha256.Sum256([]byte(provider + "\x00" + rawSessionID))
	return "sha256:" + hex.EncodeToString(sum[:])
}

func testRuntimeConfig(dataDir string, rootDir string, port string, transports mcpTransports, disableAuth bool) leafwikiRuntimeConfig {
	return leafwikiRuntimeConfig{
		Workspace: wiki.Workspace{
			DataDir: dataDir,
			RootDir: rootDir,
		},
		Host:                 "127.0.0.1",
		Port:                 port,
		PublicAccess:         disableAuth,
		AllowInsecure:        true,
		Logging:              leaflogging.Config{Target: leaflogging.TargetStderr},
		DisableAuth:          disableAuth,
		AccessTokenTimeout:   15 * time.Minute,
		RefreshTokenTimeout:  7 * 24 * time.Hour,
		MaxAssetUploadSize:   50 * 1024 * 1024,
		MCPTransports:        transports,
		MaxRevisionHistory:   100,
		HTTPRemoteUserHeader: "Remote-User",
		DaemonIdleTimeout:    0,
	}
}

func testRuntimeConfigWithHealthyControlDescriptor(t *testing.T, recordHandler http.HandlerFunc) (leafwikiRuntimeConfig, func()) {
	t.Helper()

	baseDir := t.TempDir()
	dataDir := filepath.Join(baseDir, "data")
	rootDir := filepath.Join(baseDir, "content")
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		t.Fatalf("create data dir: %v", err)
	}
	if err := os.MkdirAll(rootDir, 0o755); err != nil {
		t.Fatalf("create root dir: %v", err)
	}
	cfg := testRuntimeConfig(dataDir, rootDir, freeTCPPort(t), mcpTransports{}, true)
	ownerCfg, err := daemonRequestConfigForRuntime(cfg)
	if err != nil {
		t.Fatalf("daemon request config: %v", err)
	}
	configHash, err := projectdaemon.ConfigHash(ownerCfg)
	if err != nil {
		t.Fatalf("config hash: %v", err)
	}
	dataLock, err := locking.AcquireDataDirLock(ownerCfg.DataDir)
	if err != nil {
		t.Fatalf("acquire data lock: %v", err)
	}
	rootLock, err := locking.AcquireRootDirLock(ownerCfg.RootDir)
	if err != nil {
		_ = dataLock.Release()
		t.Fatalf("acquire root lock: %v", err)
	}

	token := "control-token"
	pid := os.Getpid()
	control := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.Header.Get(projectdaemon.ControlTokenHeader) != token {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		if req.Method == http.MethodGet && req.URL.Path == "/health" {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(projectdaemon.DaemonHealth{
				OK:            true,
				SchemaVersion: projectdaemon.DescriptorSchemaVersion,
				PID:           pid,
				DataDir:       ownerCfg.DataDir,
				RootDir:       ownerCfg.RootDir,
				ConfigHash:    configHash,
			})
			return
		}
		if req.Method == http.MethodPost && req.URL.Path == "/agent-presence/events" {
			recordHandler(w, req)
			return
		}
		http.NotFound(w, req)
	}))
	if err := projectdaemon.WriteDescriptorAtomic(projectdaemon.DescriptorPath(ownerCfg.DataDir), &projectdaemon.Descriptor{
		SchemaVersion:    projectdaemon.DescriptorSchemaVersion,
		PID:              pid,
		StartedAt:        time.Now().UTC(),
		DataDir:          ownerCfg.DataDir,
		RootDir:          ownerCfg.RootDir,
		PublicURL:        "http://127.0.0.1:" + ownerCfg.Port,
		PublicMCPEnabled: ownerCfg.PublicMCPEnabled,
		ControlURL:       control.URL,
		ConfigHash:       configHash,
		IdleTimeout:      ownerCfg.DaemonIdleTimeout,
		ControlToken:     token,
		Config:           ownerCfg,
	}); err != nil {
		control.Close()
		_ = rootLock.Release()
		_ = dataLock.Release()
		t.Fatalf("write descriptor: %v", err)
	}

	cleanup := func() {
		control.Close()
		_ = rootLock.Release()
		_ = dataLock.Release()
	}
	return cfg, cleanup
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

func createMCPAPIKey(t *testing.T, dataDir string) string {
	t.Helper()

	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		t.Fatalf("create data dir: %v", err)
	}
	userStore, err := coreauth.NewUserStore(dataDir)
	if err != nil {
		t.Fatalf("create user store: %v", err)
	}
	defer func() {
		if err := userStore.Close(); err != nil {
			t.Fatalf("close user store: %v", err)
		}
	}()
	userService := coreauth.NewUserService(userStore)
	user, err := userService.CreateUser("editor", "editor@example.com", "password", coreauth.RoleEditor)
	if err != nil {
		t.Fatalf("create API-key user: %v", err)
	}
	apiKeyStore, err := coreauth.NewAPIKeyStore(dataDir)
	if err != nil {
		t.Fatalf("create api key store: %v", err)
	}
	apiKeyService := coreauth.NewAPIKeyService(apiKeyStore, userService)
	defer func() {
		if err := apiKeyService.Close(); err != nil {
			t.Fatalf("close api key service: %v", err)
		}
	}()
	created, err := apiKeyService.CreateAPIKey(user.ID, "Main process STDIO", user.ID)
	if err != nil {
		t.Fatalf("create API key: %v", err)
	}
	return created.Secret
}
