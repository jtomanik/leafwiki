package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/gstruct"

	"github.com/perber/wiki/internal/agenthooks"
	"github.com/perber/wiki/internal/locking"
	leaflogging "github.com/perber/wiki/internal/logging"
	"github.com/perber/wiki/internal/projectdaemon"
	"github.com/perber/wiki/internal/wiki"
)

var _ = ginkgo.Describe("leafwiki command helper edges", func() {
	ginkgo.It("panics on usage write failures instead of silently truncating help", ginkgo.Label("unit"), func() {
		writeErr := errors.New("usage writer failed")
		Expect(func() {
			writeUsage(&leafwikiFailAfterWriter{failAt: 1, err: writeErr})
		}).To(PanicWith(writeErr))

		Expect(func() {
			writeUsage(&leafwikiFailAfterWriter{failAt: 2, err: writeErr})
		}).To(PanicWith(writeErr))
	})

	ginkgo.It("exits when internal startup validation rejects missing or invalid role inputs", ginkgo.Label("unit"), func() {

		_, flags := leafwikiEdgeFlagSet()
		*flags.internalProjectDaemon = filepath.Join(leafwikiTempDir(), "missing-daemon-startup.json")
		Expect(func() {
			_ = runInternalStartupCommand(flags)
		}).To(PanicWithLeafwikiExit(1))

		_, flags = leafwikiEdgeFlagSet()
		*flags.internalRuntimeRole = filepath.Join(leafwikiTempDir(), "missing-runtime-role.json")
		Expect(func() {
			_ = runInternalStartupCommand(flags)
		}).To(PanicWithLeafwikiExit(1))

		_, flags = leafwikiEdgeFlagSet()
		*flags.mcp = "invalid-transport"
		Expect(func() {
			_ = resolveStartupMCPTransports(flags, map[string]bool{"mcp": true}, false)
		}).To(PanicWithLeafwikiExit(1))

		Expect(func() {
			validateStartupCommandTransport(true, mcpTransports{Stdio: true}, nil)
		}).To(PanicWithLeafwikiExit(1))
		Expect(func() {
			validateStartupCommandTransport(false, mcpTransports{Stdio: true}, []string{"serve"})
		}).To(PanicWithLeafwikiExit(1))

		Expect(func() {
			validateProxyAuthSettings("not-a-cidr", false)
		}).To(PanicWithLeafwikiExit(1))
		Expect(func() {
			validateProxyAuthSettings("", true)
		}).To(PanicWithLeafwikiExit(1))

		_, flags = leafwikiEdgeFlagSet()
		*flags.markdownLinkRootPrefix = "https://example.test/wiki"
		Expect(func() {
			_ = buildRuntimeConfigForStartup(flags, map[string]bool{"markdown-link-root-prefix": true}, false, mcpTransports{}, leafwikiTempDir())
		}).To(PanicWithLeafwikiExit(1))
	})

	ginkgo.It("resolves startup data directories for service mode without ignoring explicit inputs", ginkgo.Label("unit"), func() {
		homeDir := leafwikiTempDir()
		leafwikiSetenv("HOME", homeDir)
		_, flags := leafwikiEdgeFlagSet()

		Expect(resolveStartupDataDir(flags, map[string]bool{}, true)).To(Equal(filepath.Join(homeDir, ".leafwiki")))

		leafwikiSetenv("LEAFWIKI_DATA_DIR", filepath.Join(leafwikiTempDir(), "env-data"))
		Expect(resolveStartupDataDir(flags, map[string]bool{}, true)).To(Equal(os.Getenv("LEAFWIKI_DATA_DIR")))

		*flags.dataDir = filepath.Join(leafwikiTempDir(), "flag-data")
		Expect(resolveStartupDataDir(flags, map[string]bool{"data-dir": true}, true)).To(Equal(*flags.dataDir))

		leafwikiSetenv("HOME", "")
		Expect(func() {
			_ = resolveStartupDataDir(flags, map[string]bool{}, true)
		}).To(PanicWithLeafwikiExit(1))
	})

	ginkgo.It("parses agent-hook commands without treating flag values as providers", ginkgo.Label("unit"), func() {
		provider, err := agentHookProviderFromArgsResult([]string{"--config", "leafwiki.yml"})
		Expect(err).To(MatchError(errAgentHookProviderAbsent))
		Expect(provider).To(BeEmpty())

		provider, err = agentHookProviderFromArgsResult([]string{"--config", "leafwiki.yml", "agent-hook", "cursor"})
		Expect(err).To(Succeed())
		Expect(provider).To(Equal(agenthooks.ProviderCursor))

		provider, err = agentHookProviderFromArgsResult([]string{"agent-hook"})
		Expect(err).To(Succeed())
		Expect(provider).To(Equal(agenthooks.ProviderUnknown))

		provider, err = agentHookProviderFromRawArgsResult([]string{"--config", "agent-hook"})
		Expect(err).To(MatchError(errAgentHookProviderAbsent))
		Expect(provider).To(BeEmpty())

		provider, err = agentHookProviderFromRawArgsResult([]string{"--config=leafwiki.yml", "agent-hook"})
		Expect(err).To(Succeed())
		Expect(provider).To(Equal(agenthooks.ProviderUnknown))
	})

	ginkgo.It("recognizes help commands before full startup dispatch", ginkgo.Label("unit"), func() {
		Expect(classifyUsageDispatch([]string{"help"})).To(Equal(usageDispatchPrintsUsage))
		Expect(classifyUsageDispatch([]string{"--help"})).To(Equal(usageDispatchPrintsUsage))
		Expect(classifyUsageDispatch([]string{"daemon"})).To(Equal(usageDispatchContinuesStartup))
	})

	ginkgo.It("handles startup help positional commands without launching runtime work", ginkgo.Label("unit"), func() {
		output := captureLeafwikiStdout(func() {
			Expect(classifyStartupPositionalCommand([]string{"help"}, false, "")).To(Equal(startupPositionalCommandHandledUsage))
		})

		Expect(output).To(ContainRenderedLeafwikiUsageMessage(leafwikiUsageMessageCLIHelpUsage))
	})

	ginkgo.It("resolves daemon service defaults from the current user home", ginkgo.Label("unit"), func() {
		homeDir := leafwikiTempDir()
		leafwikiSetenv("HOME", homeDir)

		dataDir, err := defaultDaemonServiceDataDir()
		Expect(err).NotTo(HaveOccurred())
		Expect(dataDir).To(Equal(filepath.Join(homeDir, ".leafwiki")))

		configPath, err := defaultDaemonServiceConfigPath()
		Expect(err).NotTo(HaveOccurred())
		Expect(configPath).To(Equal(filepath.Join(homeDir, ".leafwiki", "leafwiki.yml")))

		fs, flags := leafwikiEdgeFlagSet()
		visited := map[string]bool{}
		Expect(applyDaemonServiceDefaults(fs, flags, visited)).To(Succeed())
		Expect(*flags.dataDir).To(Equal(dataDir))
		Expect(*flags.rootDir).To(Equal(filepath.Join(dataDir, "root")))
		Expect(*flags.host).To(Equal("127.0.0.1"))
		Expect(*flags.port).To(Equal("8080"))
		Expect(classifyLoggingTarget(*flags.logTarget)).To(Equal(loggingTargetFile))
		Expect(visited).To(HaveKey("log-file"))
	})

	ginkgo.It("parses scalar config helpers without invoking failure exits", ginkgo.Label("unit"), func() {
		Expect(resolveInt("workers", 7, map[string]bool{"workers": true}, "LEAFWIKI_TEST_WORKERS", 3)).To(Equal(7))
		leafwikiSetenv("LEAFWIKI_TEST_WORKERS", "42")
		Expect(resolveInt("workers", 7, map[string]bool{}, "LEAFWIKI_TEST_WORKERS", 3)).To(Equal(42))
		leafwikiSetenv("LEAFWIKI_TEST_WORKERS", "")
		Expect(resolveInt("workers", 7, map[string]bool{}, "LEAFWIKI_TEST_WORKERS", 3)).To(Equal(3))

		Expect(parseByteSize("1MiB", "upload")).To(Equal(int64(1024 * 1024)))
		Expect(observeParsedBool(" ON ")).To(Equal(leafwikiParsedBoolEnabled))
		Expect(observeParsedBool(" off ")).To(Equal(leafwikiParsedBoolDisabled))
		Expect(observeParsedBool("maybe")).To(Equal(leafwikiParsedBoolRejected))

		duration, err := parseDurationResult("1500ms")
		Expect(err).To(Succeed())
		Expect(duration).To(Equal(1500 * time.Millisecond))
		duration, err = parseDurationResult("not-a-duration")
		Expect(err).To(MatchError(errDurationValueRejected))
		Expect(duration).To(BeZero())
	})

	ginkgo.It("fails fast for invalid scalar environment and byte-size values", ginkgo.Label("unit"), func() {

		leafwikiSetenv("LEAFWIKI_EDGE_BOOL", "bogus")
		Expect(func() {
			_ = resolveBool("edge-bool", false, map[string]bool{}, "LEAFWIKI_EDGE_BOOL")
		}).To(PanicWithLeafwikiExit(1))

		leafwikiSetenv("LEAFWIKI_EDGE_INT", "bogus")
		Expect(func() {
			_ = resolveInt("edge-int", 0, map[string]bool{}, "LEAFWIKI_EDGE_INT", 1)
		}).To(PanicWithLeafwikiExit(1))

		leafwikiSetenv("LEAFWIKI_EDGE_DURATION", "bogus")
		Expect(func() {
			_ = resolveDuration("edge-duration", 0, map[string]bool{}, "LEAFWIKI_EDGE_DURATION")
		}).To(PanicWithLeafwikiExit(1))

		Expect(func() {
			_ = parseByteSize("bogus", "edge size")
		}).To(PanicWithLeafwikiExit(1))
		Expect(func() {
			_ = parseByteSize("0B", "edge size")
		}).To(PanicWithLeafwikiExit(1))
		Expect(func() {
			_ = parseByteSize("16EiB", "edge size")
		}).To(PanicWithLeafwikiExit(1))
		Expect(func() {
			_ = parseByteSize("8EiB", "edge size")
		}).To(PanicWithLeafwikiExit(1))
	})

	ginkgo.It("returns logger setup errors without replacing the default logger", ginkgo.Label("unit"), func() {
		closer, err := setupLogger(leaflogging.Config{Target: leaflogging.Target("bogus")}, io.Discard, io.Discard)
		Expect(err).To(MatchError(leaflogging.ErrInvalidLogTarget))
		Expect(closer).To(BeNil())
	})

	ginkgo.It("formats project daemon identity and role snapshots", ginkgo.Label("unit"), func() {
		err := projectDaemonIdentityMismatch(&projectdaemon.Descriptor{
			DataDir: "/owner/data",
			RootDir: "/owner/root",
		}, projectdaemon.Config{
			DataDir: "/requested/data",
			RootDir: "/requested/root",
		})
		Expect(err).To(MatchProjectDaemonConfigMismatch(
			projectdaemon.Mismatch{Field: "data-dir", Want: "/owner/data", Got: "/requested/data"},
			projectdaemon.Mismatch{Field: "root-dir", Want: "/owner/root", Got: "/requested/root"},
		))

		Expect(projectDaemonDescriptorRole(projectdaemon.RuntimeStackWikidFrontd)).To(Equal(projectdaemon.RoleWikid))
		Expect(projectDaemonDescriptorRole("single-process")).To(BeEmpty())
		Expect(projectDaemonDescriptorRoles("single-process", 123, "127.0.0.1:8080", nil)).To(BeNil())

		Expect(projectDaemonDescriptorRoles(projectdaemon.RuntimeStackWikidFrontd, 123, "127.0.0.1:8080", nil)).To(HaveExactElements(
			MatchProjectDaemonRoleHealth(projectdaemon.RoleWikid, projectdaemon.RoleStateReady, gstruct.Fields{
				"PID": Equal(123),
			}),
			MatchProjectDaemonRoleHealth(projectdaemon.RoleFrontd, projectdaemon.RoleStateReady, gstruct.Fields{
				"PID": Equal(123),
				"URL": Equal("http://127.0.0.1:8080"),
			}),
			MatchPrivateProjectDaemonRoleHealth(projectdaemon.RoleWorkspaced, projectdaemon.RoleStateReady, gstruct.Fields{
				"PID": Equal(123),
			}),
		))

		roles := []projectdaemon.RoleHealth{{Name: projectdaemon.RoleWorkspaced, State: projectdaemon.RoleStateCrashed, Error: string(leafwikiRuntimeFailureBoom)}}
		copied := projectDaemonDescriptorRoles(projectdaemon.RuntimeStackWikidFrontd, 123, "127.0.0.1:8080", &wikidFrontdRuntime{roles: roles})
		Expect(copied).To(Equal(roles))
		copied[0].Error = "mutated"
		Expect(roles).To(HaveExactElements(MatchProjectDaemonRoleFailure(projectdaemon.RoleWorkspaced, projectdaemon.RoleStateCrashed, leafwikiRuntimeFailureBoom)))
	})

	ginkgo.It("derives workspace display names and original request paths", ginkgo.Label("unit"), func() {
		Expect(federatedWorkspaceDisplayName(projectdaemon.Config{RootDir: "/repo/docs", DataDir: "/data/wiki"})).To(Equal("docs"))
		Expect(federatedWorkspaceDisplayName(projectdaemon.Config{RootDir: string(filepath.Separator), DataDir: "/data/wiki"})).To(Equal("wiki"))
		Expect(federatedWorkspaceDisplayName(projectdaemon.Config{})).To(Equal("Workspace"))

		req := &http.Request{URL: &url.URL{Path: "/mcp"}}
		Expect(originalPath(req)).To(Equal("/mcp"))
		req.Header = make(http.Header)
		req.Header.Set("X-LeafWiki-Original-Path", " /original ")
		Expect(originalPath(req)).To(Equal("/original"))
		Expect(originalPath(&http.Request{})).To(Equal("/"))
	})

	ginkgo.It("writes structured project daemon startup errors", ginkgo.Label("integration"), func() {
		path := filepath.Join(leafwikiTempDir(), "startup-error.json")
		writeProjectDaemonStartupError(path, os.ErrPermission)

		var startupErr projectDaemonStartupError
		Expect(os.ReadFile(path)).To(WithTransform(func(raw []byte) error {
			return json.Unmarshal(raw, &startupErr)
		}, Succeed()))
		Expect(startupErr).To(MatchProjectDaemonStartupFailure())
		Expect(formatProjectDaemonStartupError(startupErr)).To(MatchError(errProjectDaemonStartupFailed))

		lockDir := filepath.Join(leafwikiTempDir(), "locked-data")
		dataLock, err := locking.AcquireDataDirLock(lockDir)
		Expect(err).NotTo(HaveOccurred())
		_, lockErr := locking.AcquireDataDirLock(lockDir)
		Expect(lockErr).To(MatchHeldRuntimeLockError())
		lockPath := filepath.Join(leafwikiTempDir(), "lock-startup-error.json")
		writeProjectDaemonStartupError(lockPath, lockErr)
		Expect(dataLock.Release()).To(Succeed())
		var lockStartupErr projectDaemonStartupError
		Expect(os.ReadFile(lockPath)).To(WithTransform(func(raw []byte) error {
			return json.Unmarshal(raw, &lockStartupErr)
		}, Succeed()))
		Expect(lockStartupErr).To(MatchProjectDaemonLockStartupFailure())

		blankPath := filepath.Join(leafwikiTempDir(), "blank.json")
		writeProjectDaemonStartupError(" ", os.ErrPermission)
		writeProjectDaemonStartupError(blankPath, nil)
		Expect(os.Stat(blankPath)).Error().To(MatchError(os.ErrNotExist))

		Expect(runInternalProjectDaemon(context.Background(), filepath.Join(leafwikiTempDir(), "missing.json"))).To(MatchError(os.ErrNotExist))
		badDaemonStartup := filepath.Join(leafwikiTempDir(), "bad-daemon.json")
		Expect(os.WriteFile(badDaemonStartup, []byte("{bad"), 0o600)).To(Succeed())
		Expect(runInternalProjectDaemon(context.Background(), badDaemonStartup)).To(MatchJSONSyntaxError())

		ownerErrPath := filepath.Join(leafwikiTempDir(), "owner-startup.err")
		ownerFailureStartup := filepath.Join(leafwikiTempDir(), "owner-failure.json")
		raw, err := json.Marshal(leafwikiRuntimeConfig{
			DaemonStartupErrorPath: ownerErrPath,
			Workspace:              wiki.Workspace{DataDir: "bad\x00data", RootDir: leafwikiTempDir()},
		})
		Expect(err).NotTo(HaveOccurred())
		Expect(os.WriteFile(ownerFailureStartup, raw, 0o600)).To(Succeed())
		Expect(runInternalProjectDaemon(context.Background(), ownerFailureStartup)).To(MatchPathError())
		var ownerStartupErr projectDaemonStartupError
		Expect(os.ReadFile(ownerErrPath)).To(WithTransform(func(raw []byte) error {
			return json.Unmarshal(raw, &ownerStartupErr)
		}, Succeed()))
		Expect(ownerStartupErr).To(MatchProjectDaemonStartupFailure())
	})

	ginkgo.It("filters native STDIO frames without leaking invalid JSON to the MCP stream", ginkgo.Label("unit"), func() {
		var stdout strings.Builder
		pr, pw := io.Pipe()

		Expect(filterNativeStdioJSON(strings.NewReader("{bad-json}\n"), pw, &stdout)).To(Succeed())
		forwarded, err := io.ReadAll(pr)
		Expect(err).NotTo(HaveOccurred())

		Expect(forwarded).To(BeEmpty())
		Expect(stdout.String()).To(MatchNativeStdioParseErrorFrame())
	})

	ginkgo.It("normalizes valid native STDIO frames and preserves IO failures", ginkgo.Label("unit"), func() {
		pr, pw := io.Pipe()
		done := make(chan error, 1)
		go func() {
			done <- filterNativeStdioJSON(strings.NewReader(`{"jsonrpc":"2.0"}`), pw, io.Discard)
		}()

		forwarded, err := io.ReadAll(pr)
		Expect(err).NotTo(HaveOccurred())
		Eventually(done).Should(Receive(Succeed()))
		Expect(string(forwarded)).To(Equal(`{"jsonrpc":"2.0"}` + "\n"))

		closedReader, closedForward := io.Pipe()
		readerClosedErr := errors.New("native STDIO reader closed")
		Expect(closedReader.CloseWithError(readerClosedErr)).To(Succeed())
		Expect(filterNativeStdioJSON(strings.NewReader(`{"jsonrpc":"2.0"}`+"\n"), closedForward, io.Discard)).To(MatchError(readerClosedErr))

		partialReader, partialForward := io.Pipe()
		done = make(chan error, 1)
		go func() {
			done <- filterNativeStdioJSON(strings.NewReader(`{"jsonrpc":"2.0"}`), partialForward, io.Discard)
		}()
		frame := make([]byte, len(`{"jsonrpc":"2.0"}`))
		_, err = io.ReadFull(partialReader, frame)
		Expect(err).NotTo(HaveOccurred())
		Expect(string(frame)).To(Equal(`{"jsonrpc":"2.0"}`))
		newlineErr := errors.New("newline rejected")
		Expect(partialReader.CloseWithError(newlineErr)).To(Succeed())
		Eventually(done).Should(Receive(MatchError(newlineErr)))

		stdoutErr := errors.New("stdout closed")
		_, failingStdoutForward := io.Pipe()
		Expect(filterNativeStdioJSON(strings.NewReader("{bad-json}\n"), failingStdoutForward, leafwikiFailWriter{err: stdoutErr})).To(MatchError(stdoutErr))

		readErr := errors.New("stdin failed")
		_, failingReadForward := io.Pipe()
		Expect(filterNativeStdioJSON(leafwikiErrReader{err: readErr}, failingReadForward, io.Discard)).To(MatchError(readErr))
	})

	ginkgo.It("classifies native STDIO close errors narrowly", ginkgo.Label("unit"), func() {
		Expect(error(nil)).To(BeCleanNativeStdioClose())
		Expect(io.EOF).To(BeCleanNativeStdioClose())
		Expect(errors.New("server is closing: EOF")).To(BeCleanNativeStdioClose())
		Expect(errors.New("broken pipe")).NotTo(BeCleanNativeStdioClose())
	})

	ginkgo.It("parses startup diagnostics and trusts only local daemon control URLs", ginkgo.Label("unit"), func() {
		structured := parseProjectDaemonStartupError([]byte(`{"message":" ` + leafwikiFixtureDaemonStopped + ` "}`))
		Expect(structured).To(MatchProjectDaemonStartupFailure())
		Expect(formatProjectDaemonStartupError(structured)).To(MatchError(errProjectDaemonStartupFailed))

		lockStartup := parseProjectDaemonStartupError([]byte("acquire data directory lock: /tmp/wiki"))
		Expect(formatProjectDaemonStartupError(lockStartup)).To(MatchError(errProjectLockedNoAttachableDaemon))

		Expect(" http://localhost:8080/control ").To(BeTrustedDaemonControlURL())
		Expect("http://127.0.0.1:8080/control").To(BeTrustedDaemonControlURL())
		Expect("http://[::1]:8080/control").To(BeTrustedDaemonControlURL())
		Expect("https://localhost:8080/control").NotTo(BeTrustedDaemonControlURL())
		Expect("http://example.com/control").NotTo(BeTrustedDaemonControlURL())
		Expect("http://%zz").NotTo(BeTrustedDaemonControlURL())
	})

	ginkgo.It("classifies project daemon lock states from real data and root locks", ginkgo.Label("integration"), func() {
		baseDir := leafwikiTempDir()
		dataDir := filepath.Join(baseDir, "data")
		rootDir := filepath.Join(baseDir, "root")
		Expect(os.MkdirAll(rootDir, 0o755)).To(Succeed())

		Expect(projectDaemonLocks(dataDir, rootDir)).To(HaveAvailableProjectDaemonLocks())
		Expect(projectDaemonLocks(dataDir, rootDir)).NotTo(HaveHeldProjectDaemonLocks())

		dataLock, err := locking.AcquireDataDirLock(dataDir)
		Expect(err).NotTo(HaveOccurred())
		ginkgo.DeferCleanup(dataLock.Release)
		rootLock, err := locking.AcquireRootDirLock(rootDir)
		Expect(err).NotTo(HaveOccurred())
		ginkgo.DeferCleanup(rootLock.Release)

		Expect(projectDaemonLocks(dataDir, rootDir)).To(HaveHeldProjectDaemonLocks())

		Expect(projectDaemonLocks(dataDir, rootDir)).NotTo(HaveAvailableProjectDaemonLocks())

		Expect(projectDaemonLocks(dataDir, rootDir)).NotTo(HaveFreeDataLockWithHeldRootLock())

		Expect(dataLock.Release()).To(Succeed())
		Expect(projectDaemonLocks(dataDir, rootDir)).To(HaveFreeDataLockWithHeldRootLock())

		Expect(rootLock.Release()).To(Succeed())
		Expect(projectDaemonLocks(dataDir, rootDir)).NotTo(HaveFreeDataLockWithHeldRootLock())

		fileDataDir := filepath.Join(baseDir, "file-data")
		Expect(os.WriteFile(fileDataDir, []byte("not a directory"), 0o600)).To(Succeed())
		Expect(projectDaemonLocks(fileDataDir, rootDir)).To(MatchProjectDaemonLockAvailabilityError(syscall.ENOTDIR))
	})

	ginkgo.It("compares daemon descriptor health and request identity variants", ginkgo.Label("unit"), func() {
		desc := &projectdaemon.Descriptor{
			SchemaVersion: 1,
			PID:           1234,
			DataDir:       "/data",
			RootDir:       "/root",
			ConfigHash:    "hash",
		}
		health := &projectdaemon.DaemonHealth{
			SchemaVersion: 1,
			PID:           1234,
			DataDir:       "/data",
			RootDir:       "/root",
			ConfigHash:    "hash",
		}
		Expect(health).To(MatchDaemonHealthDescriptor(desc))
		Expect(health).NotTo(MatchDaemonHealthDescriptor(nil))
		health.PID = 4321
		Expect(health).NotTo(MatchDaemonHealthDescriptor(desc))

		owner := completeDaemonCompareConfig()
		requested := owner
		requested.Host = "0.0.0.0"
		requested.Port = "9999"
		requested.PublicMCPEnabled = true
		requested.LogTarget = "file"
		requested.LogFile = "/tmp/leafwiki.log"
		requested.DisableRequestLog = true
		Expect(compareProjectDaemonConfigForRequest(owner, requested, mcpTransports{Stdio: true})).To(BeEmpty())
		Expect(compareProjectDaemonConfigForRequest(owner, requested, mcpTransports{Stdio: true, HTTP: true})).To(ContainElement(HaveField("Field", Equal("public-mcp-enabled"))))

		Expect(compareProjectDaemonDescriptorForRequest(nil, requested, mcpTransports{})).To(BeNil())
		desc = &projectdaemon.Descriptor{
			Role:          projectdaemon.RoleWorkspaced,
			PrivateMCPURL: "http://127.0.0.1/private",
			WorkspaceID:   newFixtureWorkspaceID("owner-workspace"),
			Config:        owner,
		}
		requested = owner
		requested.WorkspaceID = newFixtureWorkspaceID("requested-workspace")
		desc.Config.WorkspaceID = requested.WorkspaceID
		mismatches := compareProjectDaemonDescriptorForRequest(desc, requested, mcpTransports{Stdio: true})
		Expect(mismatches).To(ContainElement(Satisfy(func(mismatch projectdaemon.Mismatch) bool {
			return mismatch.Field == "workspace-id" &&
				mismatch.Want == "owner-workspace" &&
				mismatch.Got == "requested-workspace"
		})))
	})
})
