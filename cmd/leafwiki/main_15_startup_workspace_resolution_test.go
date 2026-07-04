package main

import (
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"bytes"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/onsi/gomega/gstruct"
	"github.com/perber/wiki/internal/localization"
	leaflogging "github.com/perber/wiki/internal/logging"
	"github.com/perber/wiki/internal/runtimeconfig"
	"github.com/perber/wiki/internal/wiki"
)

var _ = ginkgo.Describe("startup workspace resolution", func() {
	ginkgo.It("skips workspace validation for reset admin password", ginkgo.Label("unit"), func() {
		dir := leafwikiTempDir()
		leafwikiSetenv("LEAFWIKI_ROOT_DIR", dir)

		fs := flag.NewFlagSet("leafwiki", flag.ContinueOnError)
		var errOut bytes.Buffer
		fs.SetOutput(&errOut)
		flags := registerFlags(fs)
		Expect(fs.Parse([]string{"--data-dir=" + dir, "reset-admin-password"})).To(Succeed(), errOut.String())
		visited := map[string]bool{}
		fs.Visit(func(f *flag.Flag) { visited[f.Name] = true })

		Expect(resolveStartupWorkspaceResult(flags, visited, fs.Args())).To(MatchStartupSubcommandWorkspaceSkip())

	})
})

var _ = ginkgo.Describe("MCP transport resolution", func() {
	ginkgo.It("default env CLI and selector", ginkgo.Label("unit"), func() {
		func() {
			_ = "default none"
			got, err := resolveMCPTransportsForArgs(nil)
			Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("resolveMCPTransports: %v", err))
			Expect(got).To(MatchNoMCPTransports(), fmt.Sprintf("default transports = %#v, want none", got))

		}()

		func() {
			_ = "env enables http"
			leafwikiSetenv("LEAFWIKI_MCP", "http")
			got, err := resolveMCPTransportsForArgs(nil)
			Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("resolveMCPTransports: %v", err))
			Expect(got).To(MatchHTTPMCPTransport(), fmt.Sprintf("env transports = %#v, want http only", got))

		}()

		func() {
			_ = "cli overrides env"
			leafwikiSetenv("LEAFWIKI_MCP", "http")
			got, err := resolveMCPTransportsForArgs([]string{"--mcp=stdio"})
			Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("resolveMCPTransports: %v", err))
			Expect(got).To(MatchStdioMCPTransport(), fmt.Sprintf("CLI transports = %#v, want stdio only", got))

		}()

		func() {
			_ = "combined orderings"
			for _, raw := range []string{"--mcp=stdio,http", "--mcp=http,stdio"} {
				got, err := resolveMCPTransportsForArgs([]string{raw})
				Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("resolveMCPTransports(%s): %v", raw, err))
				Expect(got).To(MatchCombinedMCPTransports(), fmt.Sprintf("%s transports = %#v, want both", raw, got))

			}

		}()

		func() {
			_ = "selector ignores removed legacy envs after env validation"
			got, err := resolveMCPTransportsForArgs([]string{"--mcp=none"})
			Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("resolveMCPTransports: %v", err))
			Expect(got).To(MatchNoMCPTransports(), fmt.Sprintf("selector transports = %#v, want none", got))

		}()

	})
})

var _ = ginkgo.Describe("MCP transport parsing", func() {
	ginkgo.It("rejects invalid values", ginkgo.Label("unit"), func() {
		tests := []struct {
			name   string
			raw    string
			reason runtimeconfig.MCPTransportErrorReason
		}{
			{name: "unknown", raw: "websocket", reason: runtimeconfig.MCPTransportErrorReasonInvalid},
			{name: "none combined", raw: "none,stdio", reason: runtimeconfig.MCPTransportErrorReasonNoneMixed},
			{name: "duplicate", raw: "stdio,stdio", reason: runtimeconfig.MCPTransportErrorReasonDuplicate},
			{name: "empty part", raw: "stdio,", reason: runtimeconfig.MCPTransportErrorReasonInvalid},
		}
		for _, tt := range tests {
			func() {
				_ = tt.name
				_, err := parseMCPTransports(tt.raw)
				Expect(err).To(MatchMCPTransportError(tt.reason))

			}()
		}

	})
})

var _ = ginkgo.Describe("MCP transport validation", func() {
	ginkgo.It("accepts compatible transport settings and rejects invalid STDIO authentication combinations", ginkgo.Label("unit"), func() {
		tests := []struct {
			name      string
			opts      mcpTransportOptions
			messageID cliMessageID
		}{
			{
				name: "HTTP allows non-loopback web host",
				opts: mcpTransportOptions{
					Transports: mcpTransports{HTTP: true},
					Host:       "0.0.0.0",
					LogTarget:  leaflogging.TargetStderr,
				},
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
				messageID: cliMessageID(localization.MessageIDCLIErrorStdoutReservedForMCPStdio),
			},
			{
				name: "STDIO auth enabled requires key",
				opts: mcpTransportOptions{
					Transports: mcpTransports{Stdio: true},
					Host:       "127.0.0.1",
					LogTarget:  leaflogging.TargetStderr,
				},
				messageID: cliMessageID(localization.MessageIDCLIErrorStdioAuthIdentityRequired),
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
				messageID: cliMessageID(localization.MessageIDCLIErrorStdioAuthAPIKeyConflict),
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
			func() {
				_ = tt.name
				err := validateMCPTransportOptions(tt.opts)
				if tt.messageID == "" {
					Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("validateMCPTransportOptions() error = %v, want nil", err))

					return
				}
				Expect(err).To(MatchCLIRenderedMessageError(tt.messageID))

			}()
		}

	})
})

var _ = ginkgo.Describe("HTTP router option building", func() {
	ginkgo.It("propagates MCP enablement", ginkgo.Label("integration"), func() {
		opts := buildHTTPRouterOptions(httpRouterOptionsInput{
			publicAccess:        true,
			authDisabled:        true,
			enableMCP:           true,
			host:                "127.0.0.1",
			mcpToolListPageSize: 7,
		})
		Expect(opts).To(MatchPublicMCPRouterOptions(gstruct.Fields{
			"MCPToolListPageSize": Equal(7),
			"MCPBindHost":         Equal("127.0.0.1"),
		}))

	})
})

var _ = ginkgo.Describe("listen address building", func() {
	ginkgo.It("handles i pv6 loopback", ginkgo.Label("unit"), func() {
		got := buildListenAddress("::1", "8080")
		Expect(got).To(Equal("[::1]:8080"), fmt.Sprintf("buildListenAddress(::1, 8080) = %q, want %q", got, "[::1]:8080"))

	})
})

var _ = ginkgo.Describe("CLI flag registration", func() {
	ginkgo.It("accepts single dash long flags", ginkgo.Label("unit"), func() {
		fs := flag.NewFlagSet("leafwiki", flag.ContinueOnError)
		var errOut bytes.Buffer
		fs.SetOutput(&errOut)
		flags := registerFlags(fs)

		err := fs.Parse([]string{
			"-jwt-secret=test-secret",
			"-admin-password=test-password",
			"-allow-insecure=true",
		})
		Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("expected single-dash long flags to parse, got %v (%s)", err, errOut.String()))

		Expect(*flags.jwtSecret).To(Equal("test-secret"))
		Expect(*flags.adminPassword).To(Equal("test-password"))
		Expect(*flags.allowInsecure).To(BeTrue(), fmt.Sprintf("expected allow-insecure to be true"))

	})
})

var _ = ginkgo.Describe("HTTP remote-user configuration", func() {
	ginkgo.It("requires trusted proxy IPs only when remote-user auth is enabled", ginkgo.Label("unit"), func() {
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
			func() {
				_ = tc.name
				err := validateHTTPRemoteUserConfig(tc.enabled, tc.trustedProxyIPs)
				Expect(err != nil).To(Equal(tc.wantErr), fmt.Sprintf("validateHTTPRemoteUserConfig(%v, %q) error = %v, wantErr %v", tc.enabled, tc.trustedProxyIPs, err, tc.wantErr))

			}()
		}

	})
})

var _ = ginkgo.Describe("CLI flag registration", func() {
	ginkgo.It("accepts double dash long flags", ginkgo.Label("unit"), func() {
		fs := flag.NewFlagSet("leafwiki", flag.ContinueOnError)
		var errOut bytes.Buffer
		fs.SetOutput(&errOut)
		flags := registerFlags(fs)

		err := fs.Parse([]string{
			"--jwt-secret=test-secret",
			"--admin-password=test-password",
			"--allow-insecure=true",
		})
		Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("expected double-dash long flags to parse, got %v (%s)", err, errOut.String()))

		Expect(*flags.jwtSecret).To(Equal("test-secret"))
		Expect(*flags.adminPassword).To(Equal("test-password"))
		Expect(*flags.allowInsecure).To(BeTrue(), fmt.Sprintf("expected allow-insecure to be true"))

	})
})

var _ = ginkgo.Describe("CLI flag registration", func() {
	ginkgo.It("accepts root dir flag", ginkgo.Label("unit"), func() {
		fs := flag.NewFlagSet("leafwiki", flag.ContinueOnError)
		var errOut bytes.Buffer
		fs.SetOutput(&errOut)
		flags := registerFlags(fs)

		err := fs.Parse([]string{"--root-dir=/tmp/leafwiki-content"})
		Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("expected root-dir flag to parse, got %v (%s)", err, errOut.String()))
		Expect(flags.rootDir).NotTo(BeNil(), fmt.Sprintf("expected root-dir to be parsed, got %#v", flags.rootDir))
		Expect(*flags.rootDir).To(Equal("/tmp/leafwiki-content"), fmt.Sprintf("expected root-dir to be parsed, got %#v", flags.rootDir))

	})
})

var _ = ginkgo.Describe("CLI flag registration", func() {
	ginkgo.It("accepts logging flags", ginkgo.Label("unit"), func() {
		fs := flag.NewFlagSet("leafwiki", flag.ContinueOnError)
		var errOut bytes.Buffer
		fs.SetOutput(&errOut)
		flags := registerFlags(fs)

		err := fs.Parse([]string{
			"--log-target=stderr",
			"--log-file=logs/custom.log",
		})
		Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("expected logging flags to parse, got %v (%s)", err, errOut.String()))
		Expect(flags).To(WithTransform(func(flags *cliFlags) map[string]string {
			values := map[string]string{}
			if flags.logTarget != nil {
				values["log-target"] = *flags.logTarget
			}
			if flags.logFile != nil {
				values["log-file"] = *flags.logFile
			}
			return values
		}, SatisfyAll(
			HaveKeyWithValue("log-target", "stderr"),
			HaveKeyWithValue("log-file", "logs/custom.log"),
		)), fmt.Sprintf("expected logging flags to parse, got target=%#v file=%#v", flags.logTarget, flags.logFile))

	})
})

func resolveMCPTransportsForArgs(args []string) (mcpTransports, error) {
	ginkgo.GinkgoHelper()

	fs := flag.NewFlagSet("leafwiki", flag.ContinueOnError)
	var errOut bytes.Buffer
	fs.SetOutput(&errOut)
	flags := registerFlags(fs)
	Expect(fs.Parse(args)).To(Succeed(), errOut.String())
	visited := map[string]bool{}
	fs.Visit(func(f *flag.Flag) { visited[f.Name] = true })
	return resolveMCPTransports(flags, visited)
}

func parseConfigFlagsForArgs(args []string) (*cliFlags, map[string]bool, []string) {
	ginkgo.GinkgoHelper()

	flags, visited, rest, err := parseConfigFlagsForArgsAllowError(args)
	Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("applyYAMLConfigFile: %v", err))

	return flags, visited, rest
}

func parseConfigFlagsForArgsAllowError(args []string) (*cliFlags, map[string]bool, []string, error) {
	ginkgo.GinkgoHelper()

	fs := flag.NewFlagSet("leafwiki", flag.ContinueOnError)
	var errOut bytes.Buffer
	fs.SetOutput(&errOut)
	flags := registerFlags(fs)
	Expect(fs.Parse(args)).To(Succeed(), errOut.String())
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

func writeTestConfig(path string, body string) {
	ginkgo.GinkgoHelper()

	Expect(os.WriteFile(path, []byte(body), 0o600)).To(Succeed())
}

func serviceExampleConfigPath() string {
	ginkgo.GinkgoHelper()
	path := filepath.Join("..", "..", "config", "leafwiki.service.example.yml")
	_, err := os.Stat(path)
	Expect(err).NotTo(HaveOccurred())
	return path
}

func serviceExampleConfigKeys() map[string]struct{} {
	ginkgo.GinkgoHelper()
	raw, err := os.ReadFile(serviceExampleConfigPath())
	Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("read service config example: %v", err))

	keys := map[string]struct{}{}
	for _, line := range strings.Split(string(raw), "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "#") {
			trimmed = strings.TrimSpace(strings.TrimPrefix(trimmed, "#"))
		}
		key, _, ok := strings.Cut(trimmed, ":")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		if isServiceExampleConfigKey(key) {
			keys[key] = struct{}{}
		}
	}
	return keys
}

func isServiceExampleConfigKey(key string) bool {
	if key == "" {
		return false
	}
	for _, r := range key {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' {
			continue
		}
		return false
	}
	return true
}

func resolveWorkspaceForArgs(args []string) wiki.Workspace {
	ginkgo.GinkgoHelper()

	fs := flag.NewFlagSet("leafwiki", flag.ContinueOnError)
	var errOut bytes.Buffer
	fs.SetOutput(&errOut)
	flags := registerFlags(fs)
	Expect(fs.Parse(args)).To(Succeed(), errOut.String())
	visited := map[string]bool{}
	fs.Visit(func(f *flag.Flag) { visited[f.Name] = true })
	workspace, err := resolveWorkspace(flags, visited)
	Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("resolveWorkspace: %v", err))

	return workspace
}

func resolveLoggingConfigForArgs(args []string) leaflogging.Config {
	ginkgo.GinkgoHelper()

	cfg, err := resolveLoggingConfigForArgsAllowError(args)
	Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("resolveLoggingConfig: %v", err))

	return cfg
}

func resolveLoggingConfigForArgsAllowError(args []string) (leaflogging.Config, error) {
	ginkgo.GinkgoHelper()

	fs := flag.NewFlagSet("leafwiki", flag.ContinueOnError)
	var errOut bytes.Buffer
	fs.SetOutput(&errOut)
	flags := registerFlags(fs)
	Expect(fs.Parse(args)).To(Succeed(), errOut.String())
	visited := map[string]bool{}
	fs.Visit(func(f *flag.Flag) { visited[f.Name] = true })
	workspace, err := resolveWorkspace(flags, visited)
	Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("resolveWorkspace: %v", err))

	return resolveLoggingConfig(flags, visited, workspace.DataDir)
}
