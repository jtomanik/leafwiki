package main

import (
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/perber/wiki/internal/localization"
	"github.com/perber/wiki/internal/runtimeconfig"
)

var _ = ginkgo.Describe("leafwiki main process", func() {
	ginkgo.It("environment stderr target is used when flag absent", ginkgo.Label("e2e"), func() {
		dataDir := filepath.Join(leafwikiTempDir(), "data")
		port := freeTCPPort()
		proc := startLeafwikiHelper([]string{
			"--disable-auth",
			"--data-dir", dataDir,
			"--host", "127.0.0.1",
			"--port", port,
		}, map[string]string{
			"LEAFWIKI_LOG_TARGET": "stderr",
		})

		waitForLeafwikiReady(proc, port)
		globalDesc := waitForGlobalWikidDescriptor(dataDir)
		waitForFileContaining(proc.stderrPath, "Starting LeafWiki")
		waitForFileContaining(proc.stderrPath, "http request")
		proc.stop()
		terminateProjectDaemonProcess(globalDesc.PID)
		waitForLeafwikiUnavailable(port)
		waitForProjectLocksReusable(dataDir, filepath.Join(dataDir, "root"), 15*time.Second)

		defaultLogPath := filepath.Join(dataDir, ".leafwiki", "logs", "leafwiki.log")
		_, err := os.Stat(defaultLogPath)
		Expect(err).To(MatchError(os.ErrNotExist))

	})
})

var _ = ginkgo.Describe("leafwiki main process", func() {
	ginkgo.It("rejects invalid log target on stderr with no stdout", ginkgo.Label("e2e"), func() {
		stdout, stderr, err := runLeafwikiHelper([]string{
			"--disable-auth",
			"--data-dir", filepath.Join(leafwikiTempDir(), "data"),
		}, map[string]string{
			"LEAFWIKI_LOG_TARGET": "syslog",
		})
		Expect(err).To(MatchProcessExitError(), fmt.Sprintf("expected invalid log target to exit non-zero"))
		Expect(stdout).To(BeEmpty(), fmt.Sprintf("stdout = %q, want empty", stdout))
		Expect(readJSONLogEntriesFromText(stderr)).To(ContainElement(haveJSONLogEntry(localization.MessageIDCLIErrorInvalidLoggingConfig)))

	})
})

var _ = ginkgo.Describe("leafwiki main process", func() {
	ginkgo.It("config YAML value overrides environment", ginkgo.Label("e2e"), func() {
		baseDir := leafwikiTempDir()
		dataDir := filepath.Join(baseDir, "data")
		rootDir := filepath.Join(baseDir, "content")
		port := freeTCPPort()
		configPath := filepath.Join(baseDir, "leafwiki.yml")
		writeTestConfig(configPath, fmt.Sprintf(`disable-auth: true
data-dir: %s
root-dir: %s
host: 127.0.0.1
port: %s
log-target: stderr
`, dataDir, rootDir, port))
		proc := startLeafwikiHelper([]string{"--config", configPath}, map[string]string{
			"LEAFWIKI_PORT": "1",
		})

		waitForLeafwikiReady(proc, port)
		proc.stop()

	})
})

// Plantrace evidence: TestApplyYAMLConfigFile_ResolutionPrecedenceAndExplicitScalars.
var _ = ginkgo.Describe("YAML configuration loading", func() {
	ginkgo.It("resolution precedence and explicit scalars", ginkgo.Label("unit"), func() {
		leafwikiSetenv("LEAFWIKI_PORT", "9999")
		leafwikiSetenv("LEAFWIKI_HOST", "0.0.0.0")
		leafwikiSetenv("LEAFWIKI_BASE_PATH", "/wiki")
		leafwikiSetenv("LEAFWIKI_MARKDOWN_LINK_ROOT_PREFIX", "/wiki-docs")
		leafwikiSetenv("LEAFWIKI_PUBLIC_ACCESS", "true")

		configPath := filepath.Join(leafwikiTempDir(), "leafwiki.yml")
		writeTestConfig(configPath, `port: 8088
base-path: ""
markdown-link-root-prefix: docs/
public-access: false
`)
		flags, visited, _ := parseConfigFlagsForArgs([]string{"--config", configPath})

		Expect(resolveString("port", *flags.port, visited, "LEAFWIKI_PORT", "8080")).To(Equal("8088"))
		Expect(resolveString("host", *flags.host, visited, "LEAFWIKI_HOST", "127.0.0.1")).To(Equal("0.0.0.0"))
		Expect(resolveString("data-dir", *flags.dataDir, visited, "LEAFWIKI_DATA_DIR", "./data")).To(Equal("./data"))
		Expect(resolveString("base-path", *flags.basePath, visited, "LEAFWIKI_BASE_PATH", "")).To(BeEmpty())
		markdownLinkRootPrefix, err := resolveMarkdownLinkRootPrefix(flags, visited)
		Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("resolve markdown-link-root-prefix: %v", err))
		Expect(markdownLinkRootPrefix).To(Equal("/docs"), fmt.Sprintf("markdown-link-root-prefix = %q, want /docs", markdownLinkRootPrefix))

		Expect(resolveBool("public-access", *flags.publicAccess, visited, "LEAFWIKI_PUBLIC_ACCESS")).To(BeFalse())

	})
})

var _ = ginkgo.Describe("config file flag registry", func() {
	ginkgo.It("registers public runtime flags", ginkgo.Label("unit"), func() {
		fs := flag.NewFlagSet("leafwiki", flag.ContinueOnError)
		registerFlags(fs)

		excluded := map[string]bool{
			"config":                  true,
			"enable-mcp":              true,
			"internal-project-daemon": true,
			"internal-runtime-role":   true,
			"mcp-stdio":               true,
		}
		allowed := configFileFlagNames()
		for name := range excluded {
			Expect(allowed).NotTo(HaveKey(name))
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
		Expect(missing).To(BeEmpty(), fmt.Sprintf("configFileFlagNames missing keys: %v", missing))
		Expect(extra).To(BeEmpty(), fmt.Sprintf("configFileFlagNames extra keys: %v", extra))

	})
})

var _ = ginkgo.Describe("service example configuration", func() {
	ginkgo.It("parses active template", ginkgo.Label("unit"), func() {
		fs := flag.NewFlagSet("leafwiki", flag.ContinueOnError)
		registerFlags(fs)
		visited := map[string]bool{}

		Expect(applyYAMLConfigPath(fs, visited, serviceExampleConfigPath(), "service config example")).To(Succeed())

		for _, expected := range []string{
			"allow-insecure",
			"disable-auth",
			"host",
			"log-file",
			"log-target",
			"mcp",
			"port",
		} {
			Expect(visited).To(HaveKeyWithValue(expected, true), fmt.Sprintf("service config example active keys = %#v, want %q", visited, expected))

		}

	})
})

var _ = ginkgo.Describe("service example configuration", func() {
	ginkgo.It("documents every public YAML key", ginkgo.Label("unit"), func() {
		documented := serviceExampleConfigKeys()
		allowed := configFileFlagNames()

		var missing []string
		for name := range allowed {
			if _, ok := documented[name]; !ok {
				missing = append(missing, name)
			}
		}
		var extra []string
		for name := range documented {
			if _, ok := allowed[name]; !ok {
				extra = append(extra, name)
			}
		}
		sort.Strings(missing)
		sort.Strings(extra)
		Expect(missing).To(BeEmpty(), fmt.Sprintf("service config example missing keys: %v", missing))
		Expect(extra).To(BeEmpty(), fmt.Sprintf("service config example extra keys: %v", extra))

	})
})

var _ = ginkgo.Describe("YAML configuration loading", func() {
	ginkgo.It("accepts quoted scalar coercions", ginkgo.Label("unit"), func() {
		configPath := filepath.Join(leafwikiTempDir(), "leafwiki.yml")
		writeTestConfig(configPath, `public-access: "false"
allow-insecure: "true"
access-token-timeout: "30m"
`)

		flags, visited, _ := parseConfigFlagsForArgs([]string{"--config", configPath})

		Expect(resolveBool("public-access", *flags.publicAccess, visited, "LEAFWIKI_PUBLIC_ACCESS")).To(BeFalse())
		Expect(resolveBool("allow-insecure", *flags.allowInsecure, visited, "LEAFWIKI_ALLOW_INSECURE")).To(BeTrue())
		Expect(resolveDuration("access-token-timeout", *flags.accessTokenTimeout, visited, "LEAFWIKI_ACCESS_TOKEN_TIMEOUT")).To(Equal(30 * time.Minute))

	})
})

var _ = ginkgo.Describe("YAML configuration loading", func() {
	ginkgo.It("rejects invalid keys and values", ginkgo.Label("unit"), func() {
		tests := []struct {
			name   string
			yaml   string
			reason runtimeconfig.ConfigFileErrorReason
			key    string
		}{
			{name: "unknown key", yaml: "unknown-option: true\n", reason: runtimeconfig.ConfigFileErrorReasonUnknownKey, key: "unknown-option"},
			{name: "duplicate key", yaml: "port: 8080\nport: 8081\n", reason: runtimeconfig.ConfigFileErrorReasonDuplicateKey, key: "port"},
			{name: "non scalar value", yaml: "trusted-proxy-ips:\n  - 127.0.0.1\n", reason: runtimeconfig.ConfigFileErrorReasonScalarValue, key: "trusted-proxy-ips"},
			{name: "null value", yaml: "base-path: null\n", reason: runtimeconfig.ConfigFileErrorReasonScalarValue, key: "base-path"},
			{name: "hidden compatibility key", yaml: "enable-mcp: true\n", reason: runtimeconfig.ConfigFileErrorReasonUnknownKey, key: "enable-mcp"},
			{name: "removed revision key", yaml: "enable-revision: true\n", reason: runtimeconfig.ConfigFileErrorReasonUnknownKey, key: "enable-revision"},
			{name: "removed workspace sync key", yaml: "enable-workspace-sync: true\n", reason: runtimeconfig.ConfigFileErrorReasonUnknownKey, key: "enable-workspace-sync"},
			{name: "removed revision limit key", yaml: "max-revision-history: 0\n", reason: runtimeconfig.ConfigFileErrorReasonUnknownKey, key: "max-revision-history"},
			{name: "internal key", yaml: "internal-project-daemon: /tmp/startup.json\n", reason: runtimeconfig.ConfigFileErrorReasonUnknownKey, key: "internal-project-daemon"},
			{name: "config key", yaml: "config: other.yml\n", reason: runtimeconfig.ConfigFileErrorReasonUnknownKey, key: "config"},
			{name: "mcp stdio compatibility key", yaml: "mcp-stdio: true\n", reason: runtimeconfig.ConfigFileErrorReasonUnknownKey, key: "mcp-stdio"},
			{name: "bad bool scalar", yaml: "public-access: maybe\n", reason: runtimeconfig.ConfigFileErrorReasonInvalidFlagValue, key: "public-access"},
			{name: "bad duration scalar", yaml: "access-token-timeout: soon\n", reason: runtimeconfig.ConfigFileErrorReasonInvalidFlagValue, key: "access-token-timeout"},
		}
		for _, tt := range tests {
			func() {
				_ = tt.name
				configPath := filepath.Join(leafwikiTempDir(), "leafwiki.yml")
				writeTestConfig(configPath, tt.yaml)

				_, _, _, err := parseConfigFlagsForArgsAllowError([]string{"--config", configPath})

				Expect(err).To(MatchRuntimeConfigFileError(tt.reason, tt.key))

			}()
		}

	})
})

var _ = ginkgo.Describe("YAML configuration loading", func() {
	ginkgo.It("rejects config mixed with normal CLI flag", ginkgo.Label("unit"), func() {
		configPath := filepath.Join(leafwikiTempDir(), "leafwiki.yml")
		writeTestConfig(configPath, "port: 8080\n")

		_, _, _, err := parseConfigFlagsForArgsAllowError([]string{"--config", configPath, "--port", "8081"})

		Expect(err).To(MatchError(runtimeconfig.ConfigFlagMixError{Flag: "--port"}))

	})
})

var _ = ginkgo.Describe("YAML configuration loading", func() {
	ginkgo.It("rejects config mixed with subcommand trailing CLI flag", ginkgo.Label("unit"), func() {
		configPath := filepath.Join(leafwikiTempDir(), "leafwiki.yml")
		writeTestConfig(configPath, "data-dir: ./data\n")

		tests := []struct {
			name    string
			args    []string
			wantErr error
		}{
			{
				name: "reset password trailing flag",
				args: []string{"--config", configPath, "reset-admin-password", "--data-dir", "other"},
				wantErr: runtimeconfig.ConfigFlagMixError{
					Flag: "--data-dir",
				},
			},
			{
				name: "agent hook trailing flag",
				args: []string{"--config", configPath, "agent-hook", "codex", "--data-dir", "other"},
				wantErr: runtimeconfig.ConfigFlagMixError{
					Flag: "--data-dir",
				},
			},
		}
		for _, tt := range tests {
			func() {
				_ = tt.name
				_, _, _, err := parseConfigFlagsForArgsAllowError(tt.args)

				Expect(err).To(MatchError(tt.wantErr))

			}()
		}

	})
})

var _ = ginkgo.Describe("leafwiki main process", func() {
	ginkgo.It("config path value named agent hook does not fail open", ginkgo.Label("e2e"), func() {
		stdout, stderr, err := runLeafwikiHelperWithInputAndTimeout([]string{
			"--config", "agent-hook",
			"--not-a-real-flag",
		}, nil, `{"session_id":"should-not-be-hook"}`, 5*time.Second)
		Expect(err).To(MatchProcessExitError(), fmt.Sprintf("config path plus invalid flag unexpectedly succeeded\nstdout:\n%s\nstderr:\n%s", stdout, stderr))
		Expect(stdout).NotTo(MatchCodexAgentHookAllowResponse())

	})
})

var _ = ginkgo.Describe("leafwiki main process", func() {
	ginkgo.It("config agent hook rejects trailing CLI flag without fail open", ginkgo.Label("e2e"), func() {
		configPath := filepath.Join(leafwikiTempDir(), "leafwiki.yml")
		writeTestConfig(configPath, "data-dir: ./data\n")
		payload := `{"hook_event_name":"SessionStart","session_id":"config-conflict-secret"}`

		stdout, stderr, err := runLeafwikiHelperWithInputAndTimeout([]string{
			"--config", configPath,
			"agent-hook", "codex",
			"--data-dir", "other",
		}, nil, payload, 5*time.Second)
		Expect(err).To(MatchProcessExitError(), fmt.Sprintf("config mixed with trailing agent-hook flag unexpectedly succeeded\nstdout:\n%s\nstderr:\n%s", stdout, stderr))
		Expect(stdout).NotTo(MatchCodexAgentHookAllowResponse())
		Expect(readJSONLogEntriesFromText(stderr)).To(ContainElement(haveJSONLogEntry(localization.MessageIDCLIErrorInvalidConfigFile)))

	})
})

var _ = ginkgo.Describe("leafwiki main process", func() {
	ginkgo.It("config agent hook rejects trailing CLI flag before reading config", ginkgo.Label("e2e"), func() {
		configPath := filepath.Join(leafwikiTempDir(), "missing.yml")
		payload := `{"hook_event_name":"SessionStart","session_id":"missing-config-conflict-secret"}`

		stdout, stderr, err := runLeafwikiHelperWithInputAndTimeout([]string{
			"--config", configPath,
			"agent-hook", "codex",
			"--data-dir", "other",
		}, nil, payload, 5*time.Second)
		Expect(err).To(MatchProcessExitError(), fmt.Sprintf("config mixed with trailing agent-hook flag unexpectedly succeeded\nstdout:\n%s\nstderr:\n%s", stdout, stderr))
		Expect(stdout).NotTo(MatchCodexAgentHookAllowResponse())
		Expect(readJSONLogEntriesFromText(stderr)).To(ContainElement(haveJSONLogEntry(localization.MessageIDCLIErrorInvalidConfigFile)))

	})
})

var _ = ginkgo.Describe("leafwiki main process", func() {
	ginkgo.It("config agent hook missing config file fails open", ginkgo.Label("e2e"), func() {
		configPath := filepath.Join(leafwikiTempDir(), "missing.yml")
		payload := `{"hook_event_name":"SessionStart","session_id":"missing-config-secret"}`

		stdout, stderr, err := runLeafwikiHelperWithInputAndTimeout([]string{
			"--config", configPath,
			"agent-hook", "codex",
		}, nil, payload, 5*time.Second)
		Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("agent-hook missing config file should fail open, got %v\nstdout:\n%s\nstderr:\n%s", err, stdout, stderr))
		Expect(stdout).To(MatchCodexAgentHookAllowResponse())

	})
})

var _ = ginkgo.Describe("leafwiki main process", func() {
	ginkgo.It("config agent hook rejects flag looking config path without fail open", ginkgo.Label("e2e"), func() {
		payload := `{"hook_event_name":"SessionStart","session_id":"flag-looking-config-secret"}`

		stdout, stderr, err := runLeafwikiHelperWithInputAndTimeout([]string{
			"--config", "--data-dir",
			"agent-hook", "codex",
		}, nil, payload, 5*time.Second)
		Expect(err).To(MatchProcessExitError(), fmt.Sprintf("flag-looking config path unexpectedly succeeded\nstdout:\n%s\nstderr:\n%s", stdout, stderr))
		Expect(stdout).NotTo(MatchCodexAgentHookAllowResponse())
		Expect(readJSONLogEntriesFromText(stderr)).To(ContainElement(haveJSONLogEntry(localization.MessageIDCLIErrorInvalidConfigFile)))

	})
})

var _ = ginkgo.Describe("leafwiki main process", func() {
	ginkgo.It("config agent hook rejects dash prefixed config path without fail open", ginkgo.Label("e2e"), func() {
		baseDir := leafwikiTempDir()
		configPath := filepath.Join(baseDir, "---config")
		writeTestConfig(configPath, fmt.Sprintf(`disable-auth: true
data-dir: %s
root-dir: %s
log-target: stderr
`, baseDir, baseDir))
		previousDir, err := os.Getwd()
		Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("get working directory: %v", err))

		Expect(os.Chdir(baseDir)).To(Succeed(), fmt.Sprintf("chdir temp dir: %v", err))
		ginkgo.DeferCleanup(func() {
			Expect(os.Chdir(previousDir)).To(Succeed(), fmt.Sprintf("restore working directory: %v", err))
		})
		payload := `{"hook_event_name":"SessionStart","session_id":"dash-prefixed-config-secret"}`

		stdout, stderr, err := runLeafwikiHelperWithInputAndTimeout([]string{
			"--config", "---config",
			"agent-hook", "codex",
		}, nil, payload, 5*time.Second)
		Expect(err).To(MatchProcessExitError(), fmt.Sprintf("dash-prefixed config path unexpectedly succeeded\nstdout:\n%s\nstderr:\n%s", stdout, stderr))
		Expect(stdout).NotTo(MatchCodexAgentHookAllowResponse())
		Expect(readJSONLogEntriesFromText(stderr)).To(ContainElement(haveJSONLogEntry(localization.MessageIDCLIErrorInvalidConfigFile)))

	})
})

var _ = ginkgo.Describe("leafwiki main process", func() {
	ginkgo.It("config agent hook rejects empty config path without fail open", ginkgo.Label("e2e"), func() {
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
			func() {
				_ = tt.name
				payload := `{"hook_event_name":"SessionStart","session_id":"empty-config-secret"}`

				stdout, stderr, err := runLeafwikiHelperWithInputAndTimeout(tt.args, nil, payload, 5*time.Second)
				Expect(err).To(MatchProcessExitError(), fmt.Sprintf("empty config path unexpectedly succeeded\nstdout:\n%s\nstderr:\n%s", stdout, stderr))
				Expect(stdout).NotTo(MatchCodexAgentHookAllowResponse())
				Expect(readJSONLogEntriesFromText(stderr)).To(ContainElement(haveJSONLogEntry(localization.MessageIDCLIErrorInvalidConfigFile)))

			}()
		}

	})
})

var _ = ginkgo.Describe("leafwiki main process", func() {
	ginkgo.It("config agent hook rejects help mix without fail open", ginkgo.Label("e2e"), func() {
		configPath := filepath.Join(leafwikiTempDir(), "leafwiki.yml")
		writeTestConfig(configPath, "data-dir: ./data\n")
		payload := `{"hook_event_name":"SessionStart","session_id":"config-help-secret"}`

		stdout, stderr, err := runLeafwikiHelperWithInputAndTimeout([]string{
			"agent-hook", "codex",
			"--config", configPath,
			"--help",
		}, nil, payload, 5*time.Second)
		Expect(err).To(MatchProcessExitError(), fmt.Sprintf("config mixed with help unexpectedly succeeded\nstdout:\n%s\nstderr:\n%s", stdout, stderr))
		Expect(stdout).NotTo(MatchCodexAgentHookAllowResponse())
		Expect(readJSONLogEntriesFromText(stderr)).To(ContainElement(haveJSONLogEntry(localization.MessageIDCLIErrorInvalidConfigArguments)))

	})
})

var _ = ginkgo.Describe("leafwiki main process", func() {
	ginkgo.It("config rejects positional help", ginkgo.Label("e2e"), func() {
		configPath := filepath.Join(leafwikiTempDir(), "leafwiki.yml")
		writeTestConfig(configPath, "data-dir: ./data\n")

		stdout, stderr, err := runLeafwikiHelper([]string{
			"--config", configPath,
			"help",
		}, nil)
		Expect(err).To(MatchProcessExitError(), fmt.Sprintf("config mixed with positional help unexpectedly succeeded\nstdout:\n%s\nstderr:\n%s", stdout, stderr))
		Expect(stdout).To(BeEmpty(), fmt.Sprintf("stdout = %q, want no successful usage output", stdout))
		Expect(readJSONLogEntriesFromText(stderr)).To(ContainElement(haveJSONLogEntry(localization.MessageIDCLIErrorInvalidConfigFile)))

	})
})
