package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	sdkauth "github.com/modelcontextprotocol/go-sdk/auth"
	sdkjsonrpc "github.com/modelcontextprotocol/go-sdk/jsonrpc"
	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/perber/wiki/internal/agenthooks"
	coreauth "github.com/perber/wiki/internal/core/auth"
	"github.com/perber/wiki/internal/frontd"
	httpinternal "github.com/perber/wiki/internal/http"
	"github.com/perber/wiki/internal/locking"
	leaflogging "github.com/perber/wiki/internal/logging"
	"github.com/perber/wiki/internal/projectdaemon"
	"github.com/perber/wiki/internal/wiki"
	"github.com/perber/wiki/internal/wikid"
	"github.com/perber/wiki/internal/workspaceid"
)

var _ = ginkgo.Describe("leafwiki command helper edges", func() {
	ginkgo.It("panics on usage write failures instead of silently truncating help", func() {
		writeErr := errors.New("usage writer failed")
		Expect(func() {
			writeUsage(&leafwikiFailAfterWriter{failAt: 1, err: writeErr})
		}).To(PanicWith(writeErr))

		Expect(func() {
			writeUsage(&leafwikiFailAfterWriter{failAt: 2, err: writeErr})
		}).To(PanicWith(writeErr))
	})

	ginkgo.It("exercises fail-fast startup validation through the exit seam", func() {
		t := ginkgo.GinkgoT()

		_, flags := leafwikiEdgeFlagSet()
		*flags.internalProjectDaemon = filepath.Join(t.TempDir(), "missing-daemon-startup.json")
		expectLeafwikiExit(1, func() {
			_ = runInternalStartupCommand(flags)
		})

		_, flags = leafwikiEdgeFlagSet()
		*flags.internalRuntimeRole = filepath.Join(t.TempDir(), "missing-runtime-role.json")
		expectLeafwikiExit(1, func() {
			_ = runInternalStartupCommand(flags)
		})

		_, flags = leafwikiEdgeFlagSet()
		*flags.mcp = "invalid-transport"
		expectLeafwikiExit(1, func() {
			_ = resolveStartupMCPTransports(flags, map[string]bool{"mcp": true}, false)
		})

		expectLeafwikiExit(1, func() {
			validateStartupCommandTransport(true, mcpTransports{Stdio: true}, nil)
		})
		expectLeafwikiExit(1, func() {
			validateStartupCommandTransport(false, mcpTransports{Stdio: true}, []string{"serve"})
		})

		expectLeafwikiExit(1, func() {
			validateProxyAuthSettings("not-a-cidr", false)
		})
		expectLeafwikiExit(1, func() {
			validateProxyAuthSettings("", true)
		})

		_, flags = leafwikiEdgeFlagSet()
		*flags.markdownLinkRootPrefix = "https://example.test/wiki"
		expectLeafwikiExit(1, func() {
			_ = buildRuntimeConfigForStartup(flags, map[string]bool{"markdown-link-root-prefix": true}, false, mcpTransports{}, t.TempDir())
		})
	})

	ginkgo.It("resolves startup data directories for service mode without ignoring explicit inputs", func() {
		t := ginkgo.GinkgoT()
		homeDir := t.TempDir()
		t.Setenv("HOME", homeDir)
		_, flags := leafwikiEdgeFlagSet()

		Expect(resolveStartupDataDir(flags, map[string]bool{}, true)).To(Equal(filepath.Join(homeDir, ".leafwiki")))

		t.Setenv("LEAFWIKI_DATA_DIR", filepath.Join(t.TempDir(), "env-data"))
		Expect(resolveStartupDataDir(flags, map[string]bool{}, true)).To(Equal(os.Getenv("LEAFWIKI_DATA_DIR")))

		*flags.dataDir = filepath.Join(t.TempDir(), "flag-data")
		Expect(resolveStartupDataDir(flags, map[string]bool{"data-dir": true}, true)).To(Equal(*flags.dataDir))

		t.Setenv("HOME", "")
		expectLeafwikiExit(1, func() {
			_ = resolveStartupDataDir(flags, map[string]bool{}, true)
		})
	})

	ginkgo.It("parses agent-hook commands without treating flag values as providers", func() {
		provider, ok := agentHookProviderFromArgs([]string{"--config", "leafwiki.yml"})
		Expect(ok).To(BeFalse())
		Expect(provider).To(BeEmpty())

		provider, ok = agentHookProviderFromArgs([]string{"--config", "leafwiki.yml", "agent-hook", "cursor"})
		Expect(ok).To(BeTrue())
		Expect(provider).To(Equal("cursor"))

		provider, ok = agentHookProviderFromArgs([]string{"agent-hook"})
		Expect(ok).To(BeTrue())
		Expect(provider).To(Equal("unknown"))

		provider, ok = agentHookProviderFromRawArgs([]string{"--config", "agent-hook"})
		Expect(ok).To(BeFalse())
		Expect(provider).To(BeEmpty())

		provider, ok = agentHookProviderFromRawArgs([]string{"--config=leafwiki.yml", "agent-hook"})
		Expect(ok).To(BeTrue())
		Expect(provider).To(Equal("unknown"))
	})

	ginkgo.It("recognizes help commands before full startup dispatch", func() {
		Expect(shouldPrintUsage([]string{"help"})).To(BeTrue())
		Expect(shouldPrintUsage([]string{"--help"})).To(BeTrue())
		Expect(shouldPrintUsage([]string{"daemon"})).To(BeFalse())
	})

	ginkgo.It("handles startup help positional commands without launching runtime work", func() {
		output := captureLeafwikiStdout(func() {
			Expect(handleStartupPositionalCommand([]string{"help"}, false, "")).To(BeTrue())
		})

		Expect(output).To(ContainSubstring("Usage: leafwiki [command]"))
	})

	ginkgo.It("resolves daemon service defaults from the current user home", func() {
		t := ginkgo.GinkgoT()
		homeDir := t.TempDir()
		t.Setenv("HOME", homeDir)

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
		Expect(*flags.logTarget).To(Equal("file"))
		Expect(visited).To(HaveKey("log-file"))
	})

	ginkgo.It("parses scalar config helpers without invoking failure exits", func() {
		t := ginkgo.GinkgoT()

		Expect(resolveInt("workers", 7, map[string]bool{"workers": true}, "LEAFWIKI_TEST_WORKERS", 3)).To(Equal(7))
		t.Setenv("LEAFWIKI_TEST_WORKERS", "42")
		Expect(resolveInt("workers", 7, map[string]bool{}, "LEAFWIKI_TEST_WORKERS", 3)).To(Equal(42))
		t.Setenv("LEAFWIKI_TEST_WORKERS", "")
		Expect(resolveInt("workers", 7, map[string]bool{}, "LEAFWIKI_TEST_WORKERS", 3)).To(Equal(3))

		Expect(parseByteSize("1MiB", "upload")).To(Equal(int64(1024 * 1024)))
		parsed, ok := parseBool(" ON ")
		Expect(parsed).To(BeTrue())
		Expect(ok).To(BeTrue())
		parsed, ok = parseBool(" off ")
		Expect(parsed).To(BeFalse())
		Expect(ok).To(BeTrue())
		parsed, ok = parseBool("maybe")
		Expect(parsed).To(BeFalse())
		Expect(ok).To(BeFalse())

		duration, ok := parseDuration("1500ms")
		Expect(ok).To(BeTrue())
		Expect(duration).To(Equal(1500 * time.Millisecond))
		duration, ok = parseDuration("not-a-duration")
		Expect(duration).To(BeZero())
		Expect(ok).To(BeFalse())
	})

	ginkgo.It("fails fast for invalid scalar environment and byte-size values", func() {
		t := ginkgo.GinkgoT()

		t.Setenv("LEAFWIKI_EDGE_BOOL", "bogus")
		expectLeafwikiExit(1, func() {
			_ = resolveBool("edge-bool", false, map[string]bool{}, "LEAFWIKI_EDGE_BOOL")
		})

		t.Setenv("LEAFWIKI_EDGE_INT", "bogus")
		expectLeafwikiExit(1, func() {
			_ = resolveInt("edge-int", 0, map[string]bool{}, "LEAFWIKI_EDGE_INT", 1)
		})

		t.Setenv("LEAFWIKI_EDGE_DURATION", "bogus")
		expectLeafwikiExit(1, func() {
			_ = resolveDuration("edge-duration", 0, map[string]bool{}, "LEAFWIKI_EDGE_DURATION")
		})

		expectLeafwikiExit(1, func() {
			_ = parseByteSize("bogus", "edge size")
		})
		expectLeafwikiExit(1, func() {
			_ = parseByteSize("0B", "edge size")
		})
		expectLeafwikiExit(1, func() {
			_ = parseByteSize("16EiB", "edge size")
		})
		expectLeafwikiExit(1, func() {
			_ = parseByteSize("8EiB", "edge size")
		})
	})

	ginkgo.It("returns logger setup errors without replacing the default logger", func() {
		closer, err := setupLogger(leaflogging.Config{Target: leaflogging.Target("bogus")}, io.Discard, io.Discard)
		Expect(err).To(MatchError(ContainSubstring("invalid log target")))
		Expect(closer).To(BeNil())
	})

	ginkgo.It("formats project daemon identity and role snapshots", func() {
		err := projectDaemonIdentityMismatch(&projectdaemon.Descriptor{
			DataDir: "/owner/data",
			RootDir: "/owner/root",
		}, projectdaemon.Config{
			DataDir: "/requested/data",
			RootDir: "/requested/root",
		})
		Expect(err).To(MatchError(And(
			ContainSubstring("data-dir owner=/owner/data requested=/requested/data"),
			ContainSubstring("root-dir owner=/owner/root requested=/requested/root"),
		)))

		Expect(projectDaemonDescriptorRole(projectdaemon.RuntimeStackWikidFrontd)).To(Equal(projectdaemon.RoleWikid))
		Expect(projectDaemonDescriptorRole("single-process")).To(BeEmpty())
		Expect(projectDaemonDescriptorRoles("single-process", 123, "127.0.0.1:8080", nil)).To(BeNil())

		snapshot := projectDaemonDescriptorRoles(projectdaemon.RuntimeStackWikidFrontd, 123, "127.0.0.1:8080", nil)
		Expect(snapshot).To(HaveLen(3))
		Expect(snapshot[0].Name).To(Equal(projectdaemon.RoleWikid))
		Expect(snapshot[1].URL).To(Equal("http://127.0.0.1:8080"))
		Expect(snapshot[2].Private).To(BeTrue())

		roles := []projectdaemon.RoleHealth{{Name: projectdaemon.RoleWorkspaced, State: projectdaemon.RoleStateCrashed, Error: "boom"}}
		copied := projectDaemonDescriptorRoles(projectdaemon.RuntimeStackWikidFrontd, 123, "127.0.0.1:8080", &wikidFrontdRuntime{roles: roles})
		Expect(copied).To(Equal(roles))
		copied[0].Error = "mutated"
		Expect(roles[0].Error).To(Equal("boom"))
	})

	ginkgo.It("derives workspace display names and original request paths", func() {
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

	ginkgo.It("writes structured project daemon startup errors", func() {
		path := filepath.Join(ginkgo.GinkgoT().TempDir(), "startup-error.json")
		writeProjectDaemonStartupError(path, os.ErrPermission)

		var startupErr projectDaemonStartupError
		Expect(os.ReadFile(path)).To(WithTransform(func(raw []byte) error {
			return json.Unmarshal(raw, &startupErr)
		}, Succeed()))
		Expect(startupErr.Kind).To(Equal(projectDaemonStartupErrorKindStartup))
		Expect(startupErr.RenderedMessage).To(ContainSubstring(os.ErrPermission.Error()))

		lockDir := filepath.Join(ginkgo.GinkgoT().TempDir(), "locked-data")
		dataLock, err := locking.AcquireDataDirLock(lockDir)
		Expect(err).NotTo(HaveOccurred())
		_, lockErr := locking.AcquireDataDirLock(lockDir)
		Expect(lockErr).To(HaveOccurred())
		lockPath := filepath.Join(ginkgo.GinkgoT().TempDir(), "lock-startup-error.json")
		writeProjectDaemonStartupError(lockPath, lockErr)
		Expect(dataLock.Release()).To(Succeed())
		var lockStartupErr projectDaemonStartupError
		Expect(os.ReadFile(lockPath)).To(WithTransform(func(raw []byte) error {
			return json.Unmarshal(raw, &lockStartupErr)
		}, Succeed()))
		Expect(lockStartupErr.Kind).To(Equal(projectDaemonStartupErrorKindLock))

		blankPath := filepath.Join(ginkgo.GinkgoT().TempDir(), "blank.json")
		writeProjectDaemonStartupError(" ", os.ErrPermission)
		writeProjectDaemonStartupError(blankPath, nil)
		_, err = os.Stat(blankPath)
		Expect(os.IsNotExist(err)).To(BeTrue())

		Expect(runInternalProjectDaemon(context.Background(), filepath.Join(ginkgo.GinkgoT().TempDir(), "missing.json"))).To(MatchError(ContainSubstring("read daemon startup config")))
		badDaemonStartup := filepath.Join(ginkgo.GinkgoT().TempDir(), "bad-daemon.json")
		Expect(os.WriteFile(badDaemonStartup, []byte("{bad"), 0o600)).To(Succeed())
		Expect(runInternalProjectDaemon(context.Background(), badDaemonStartup)).To(MatchError(ContainSubstring("decode daemon startup config")))

		ownerErrPath := filepath.Join(ginkgo.GinkgoT().TempDir(), "owner-startup.err")
		ownerFailureStartup := filepath.Join(ginkgo.GinkgoT().TempDir(), "owner-failure.json")
		raw, err := json.Marshal(leafwikiRuntimeConfig{
			DaemonStartupErrorPath: ownerErrPath,
			Workspace:              wiki.Workspace{DataDir: "bad\x00data", RootDir: ginkgo.GinkgoT().TempDir()},
		})
		Expect(err).NotTo(HaveOccurred())
		Expect(os.WriteFile(ownerFailureStartup, raw, 0o600)).To(Succeed())
		Expect(runInternalProjectDaemon(context.Background(), ownerFailureStartup)).To(HaveOccurred())
		Expect(os.ReadFile(ownerErrPath)).To(ContainSubstring("resolve data dir"))
	})

	ginkgo.It("filters native STDIO frames without leaking invalid JSON to the MCP stream", func() {
		var stdout strings.Builder
		pr, pw := io.Pipe()

		Expect(filterNativeStdioJSON(strings.NewReader("{bad-json}\n"), pw, &stdout)).To(Succeed())
		forwarded, err := io.ReadAll(pr)
		Expect(err).NotTo(HaveOccurred())

		Expect(forwarded).To(BeEmpty())
		Expect(stdout.String()).To(Equal(`{"jsonrpc":"2.0","id":null,"error":{"code":-32700,"message":"Parse error"}}` + "\n"))
	})

	ginkgo.It("normalizes valid native STDIO frames and preserves IO failures", func() {
		pr, pw := io.Pipe()
		done := make(chan error, 1)
		go func() {
			done <- filterNativeStdioJSON(strings.NewReader(`{"jsonrpc":"2.0"}`), pw, io.Discard)
		}()

		forwarded, err := io.ReadAll(pr)
		Expect(err).NotTo(HaveOccurred())
		Expect(<-done).To(Succeed())
		Expect(string(forwarded)).To(Equal(`{"jsonrpc":"2.0"}` + "\n"))

		closedReader, closedForward := io.Pipe()
		Expect(closedReader.CloseWithError(errors.New("reader closed"))).To(Succeed())
		Expect(filterNativeStdioJSON(strings.NewReader(`{"jsonrpc":"2.0"}`+"\n"), closedForward, io.Discard)).To(HaveOccurred())

		partialReader, partialForward := io.Pipe()
		done = make(chan error, 1)
		go func() {
			done <- filterNativeStdioJSON(strings.NewReader(`{"jsonrpc":"2.0"}`), partialForward, io.Discard)
		}()
		frame := make([]byte, len(`{"jsonrpc":"2.0"}`))
		_, err = io.ReadFull(partialReader, frame)
		Expect(err).NotTo(HaveOccurred())
		Expect(string(frame)).To(Equal(`{"jsonrpc":"2.0"}`))
		Expect(partialReader.CloseWithError(errors.New("newline rejected"))).To(Succeed())
		Expect(<-done).To(HaveOccurred())

		stdoutErr := errors.New("stdout closed")
		_, failingStdoutForward := io.Pipe()
		Expect(filterNativeStdioJSON(strings.NewReader("{bad-json}\n"), failingStdoutForward, leafwikiFailWriter{err: stdoutErr})).To(MatchError(stdoutErr))

		readErr := errors.New("stdin failed")
		_, failingReadForward := io.Pipe()
		Expect(filterNativeStdioJSON(leafwikiErrReader{err: readErr}, failingReadForward, io.Discard)).To(MatchError(readErr))
	})

	ginkgo.It("classifies native STDIO close errors narrowly", func() {
		Expect(isCleanNativeStdioClose(nil)).To(BeTrue())
		Expect(isCleanNativeStdioClose(io.EOF)).To(BeTrue())
		Expect(isCleanNativeStdioClose(errors.New("server is closing: EOF"))).To(BeTrue())
		Expect(isCleanNativeStdioClose(errors.New("broken pipe"))).To(BeFalse())
	})

	ginkgo.It("parses startup diagnostics and trusts only local daemon control URLs", func() {
		structured := parseProjectDaemonStartupError([]byte(`{"message":" daemon stopped "}`))
		Expect(structured.Kind).To(Equal(projectDaemonStartupErrorKindStartup))
		Expect(structured.RenderedMessage).To(Equal("daemon stopped"))
		Expect(formatProjectDaemonStartupError(structured)).To(MatchError(ContainSubstring("project daemon failed to start")))

		lockStartup := parseProjectDaemonStartupError([]byte("acquire data directory lock: /tmp/wiki"))
		Expect(lockStartup.IsLock()).To(BeTrue())
		Expect(formatProjectDaemonStartupError(lockStartup)).To(MatchError(ContainSubstring("project is locked")))

		Expect(isTrustedDaemonControlURL(" http://localhost:8080/control ")).To(BeTrue())
		Expect(isTrustedDaemonControlURL("http://127.0.0.1:8080/control")).To(BeTrue())
		Expect(isTrustedDaemonControlURL("http://[::1]:8080/control")).To(BeTrue())
		Expect(isTrustedDaemonControlURL("https://localhost:8080/control")).To(BeFalse())
		Expect(isTrustedDaemonControlURL("http://example.com/control")).To(BeFalse())
		Expect(isTrustedDaemonControlURL("http://%zz")).To(BeFalse())
	})

	ginkgo.It("classifies project daemon lock states from real data and root locks", func() {
		t := ginkgo.GinkgoT()
		baseDir := t.TempDir()
		dataDir := filepath.Join(baseDir, "data")
		rootDir := filepath.Join(baseDir, "root")
		Expect(os.MkdirAll(rootDir, 0o755)).To(Succeed())

		free, err := projectDaemonLocksFree(dataDir, rootDir)
		Expect(err).NotTo(HaveOccurred())
		Expect(free).To(BeTrue())

		held, err := projectDaemonLocksHeld(dataDir, rootDir)
		Expect(err).NotTo(HaveOccurred())
		Expect(held).To(BeFalse())

		dataLock, err := locking.AcquireDataDirLock(dataDir)
		Expect(err).NotTo(HaveOccurred())
		ginkgo.DeferCleanup(dataLock.Release)
		rootLock, err := locking.AcquireRootDirLock(rootDir)
		Expect(err).NotTo(HaveOccurred())
		ginkgo.DeferCleanup(rootLock.Release)

		held, err = projectDaemonLocksHeld(dataDir, rootDir)
		Expect(err).NotTo(HaveOccurred())
		Expect(held).To(BeTrue())

		free, err = projectDaemonLocksFree(dataDir, rootDir)
		Expect(err).NotTo(HaveOccurred())
		Expect(free).To(BeFalse())

		dataFreeRootHeld, err := projectDaemonDataLockFreeRootLockHeld(dataDir, rootDir)
		Expect(err).NotTo(HaveOccurred())
		Expect(dataFreeRootHeld).To(BeFalse())

		Expect(dataLock.Release()).To(Succeed())
		dataFreeRootHeld, err = projectDaemonDataLockFreeRootLockHeld(dataDir, rootDir)
		Expect(err).NotTo(HaveOccurred())
		Expect(dataFreeRootHeld).To(BeTrue())

		Expect(rootLock.Release()).To(Succeed())
		dataFreeRootHeld, err = projectDaemonDataLockFreeRootLockHeld(dataDir, rootDir)
		Expect(err).NotTo(HaveOccurred())
		Expect(dataFreeRootHeld).To(BeFalse())

		fileDataDir := filepath.Join(baseDir, "file-data")
		Expect(os.WriteFile(fileDataDir, []byte("not a directory"), 0o600)).To(Succeed())
		free, err = projectDaemonLocksFree(fileDataDir, rootDir)
		Expect(err).To(HaveOccurred())
		Expect(free).To(BeFalse())
	})

	ginkgo.It("compares daemon descriptor health and request identity variants", func() {
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
		Expect(daemonHealthMatchesDescriptor(desc, health)).To(BeTrue())
		Expect(daemonHealthMatchesDescriptor(nil, health)).To(BeFalse())
		health.PID = 4321
		Expect(daemonHealthMatchesDescriptor(desc, health)).To(BeFalse())

		owner := completeDaemonCompareConfig()
		requested := owner
		requested.Host = "0.0.0.0"
		requested.Port = "9999"
		requested.PublicMCPEnabled = true
		requested.LogTarget = "file"
		requested.LogFile = "/tmp/leafwiki.log"
		requested.DisableRequestLog = true
		Expect(compareProjectDaemonConfigForRequest(owner, requested, mcpTransports{Stdio: true})).To(BeEmpty())
		Expect(compareProjectDaemonConfigForRequest(owner, requested, mcpTransports{Stdio: true, HTTP: true})).NotTo(BeEmpty())

		Expect(compareProjectDaemonDescriptorForRequest(nil, requested, mcpTransports{})).To(BeNil())
		desc = &projectdaemon.Descriptor{
			Role:          projectdaemon.RoleWorkspaced,
			PrivateMCPURL: "http://127.0.0.1/private",
			WorkspaceID:   "owner-workspace",
			Config:        owner,
		}
		requested = owner
		requested.WorkspaceID = "requested-workspace"
		desc.Config.WorkspaceID = requested.WorkspaceID
		mismatches := compareProjectDaemonDescriptorForRequest(desc, requested, mcpTransports{Stdio: true})
		Expect(mismatches).To(ContainElement(Satisfy(func(mismatch projectdaemon.Mismatch) bool {
			return mismatch.Field == "workspace-id" &&
				mismatch.Want == "owner-workspace" &&
				mismatch.Got == "requested-workspace"
		})))
	})

	ginkgo.It("normalizes daemon runtime config and log paths for owner and workspace requests", func() {
		t := ginkgo.GinkgoT()
		dataDir := filepath.Join(t.TempDir(), "data")
		rootDir := filepath.Join(t.TempDir(), "root")
		logPath := filepath.Join(dataDir, ".leafwiki", "logs", "leafwiki.log")

		cfg := leafwikiRuntimeConfig{
			Workspace: wiki.Workspace{
				ID:      "home",
				DataDir: dataDir,
				RootDir: rootDir,
			},
			RuntimeStack:            projectdaemon.RuntimeStackWikidFrontd,
			Host:                    "127.0.0.1",
			Port:                    "8080",
			PublicAccess:            true,
			AllowInsecure:           true,
			AccessTokenTimeout:      time.Hour,
			RefreshTokenTimeout:     2 * time.Hour,
			InjectCodeInHeader:      "secret-header",
			Logging:                 leaflogging.Config{Target: leaflogging.TargetFile, FilePath: logPath},
			DisableAuth:             true,
			MaxAssetUploadSize:      1234,
			MCPTransports:           mcpTransports{HTTP: true},
			EnableLinkRefactor:      true,
			EnableHTTPRemoteUser:    true,
			HTTPRemoteUserHeader:    "Remote-User",
			TrustedProxyIPsRaw:      "127.0.0.1",
			HTTPRemoteUserLogoutURL: "/logout",
			DisableRequestLog:       true,
			DaemonIdleTimeout:       5 * time.Minute,
			MarkdownLinkRootPrefix:  "/docs",
		}

		daemonCfg, err := daemonConfigForRuntime(cfg)
		Expect(err).NotTo(HaveOccurred())
		expectedDataDir, expectedRootDir, err := projectdaemon.CanonicalizeProject(dataDir, rootDir)
		Expect(err).NotTo(HaveOccurred())
		Expect(daemonCfg.WorkspaceID).To(Equal(workspaceid.WorkspaceID("home")))
		Expect(daemonCfg.DataDir).To(Equal(expectedDataDir))
		Expect(daemonCfg.RootDir).To(Equal(expectedRootDir))
		Expect(daemonCfg.LogFile).To(Equal(filepath.Join(expectedDataDir, ".leafwiki", "logs", "leafwiki.log")))
		Expect(daemonCfg.InjectCodeInHeaderHash).NotTo(BeEmpty())
		Expect(daemonCfg.EnableWorkspaceSync).To(BeTrue())
		Expect(daemonCfg.MarkdownLinkRootPrefix).To(Equal("/docs"))

		cfg.APIKey = "sk-test"
		cfg.Logging = leaflogging.Config{Target: leaflogging.TargetStderr}
		cfg.MCPTransports = mcpTransports{Stdio: true}
		workspaceCfg, err := daemonWorkspaceRuntimeConfig(cfg)
		Expect(err).NotTo(HaveOccurred())
		Expect(workspaceCfg.APIKey).To(BeEmpty())
		Expect(workspaceCfg.RuntimeStack).To(Equal(projectdaemon.RuntimeStackWikidFrontd))
		Expect(workspaceCfg.Logging.Target).To(Equal(leaflogging.TargetFile))
		Expect(workspaceCfg.Logging.FilePath).To(Equal(filepath.Join(dataDir, ".leafwiki", "logs", "leafwiki.log")))

		rel, ok := localRelativePath(dataDir, filepath.Join(dataDir, "nested", "leafwiki.log"))
		Expect(ok).To(BeTrue())
		Expect(rel).To(Equal(filepath.Join("nested", "leafwiki.log")))
		_, ok = localRelativePath(dataDir, dataDir)
		Expect(ok).To(BeFalse())
		_, ok = localRelativePath(dataDir, filepath.Dir(dataDir))
		Expect(ok).To(BeFalse())

		canonicalDataDir := filepath.Join(t.TempDir(), "canonical")
		Expect(daemonLogFileForConfig(leafwikiRuntimeConfig{}, canonicalDataDir)).To(BeEmpty())
		cfg.Workspace.DataDir = filepath.Join(t.TempDir(), "original")
		cfg.Logging = leaflogging.Config{Target: leaflogging.TargetFile, FilePath: filepath.Join(canonicalDataDir, ".leafwiki", "logs", "leafwiki.log")}
		Expect(daemonLogFileForConfig(cfg, canonicalDataDir)).To(Equal(filepath.Clean(cfg.Logging.FilePath)))
		cfg.Logging.FilePath = filepath.Join(t.TempDir(), "external.log")
		Expect(daemonLogFileForConfig(cfg, canonicalDataDir)).To(Equal(filepath.Clean(cfg.Logging.FilePath)))
	})

	ginkgo.It("handles runtime role lookup, HTTP tokens, and response encoding errors", func() {
		roles := []projectdaemon.RoleHealth{
			{Name: projectdaemon.RoleFrontd, URL: "http://127.0.0.1:8080"},
		}
		Expect(roleURL(roles, projectdaemon.RoleFrontd)).To(Equal("http://127.0.0.1:8080"))
		Expect(roleURL(roles, projectdaemon.RoleWorkspaced)).To(BeEmpty())
		role, ok := findRuntimeRoleHealth(roles, projectdaemon.RoleFrontd)
		Expect(ok).To(BeTrue())
		Expect(role.Name).To(Equal(projectdaemon.RoleFrontd))
		_, ok = findRuntimeRoleHealth(roles, projectdaemon.RoleWorkspaced)
		Expect(ok).To(BeFalse())

		Expect(httpBearerToken(nil)).To(BeEmpty())
		req := httptest.NewRequest(http.MethodGet, "http://leafwiki.local/mcp", nil)
		req.Header.Set("Authorization", "bearer token-123 ")
		Expect(httpBearerToken(req)).To(Equal("token-123"))
		req.Header.Set("Authorization", "Basic token-123")
		Expect(httpBearerToken(req)).To(BeEmpty())

		Expect(accessTokenFromHTTPRequest(nil)).To(BeEmpty())
		req = httptest.NewRequest(http.MethodGet, "http://leafwiki.local/", nil)
		req.AddCookie(&http.Cookie{Name: "leafwiki_at", Value: " "})
		req.AddCookie(&http.Cookie{Name: "__Host-leafwiki_at", Value: " cookie-token "})
		Expect(accessTokenFromHTTPRequest(req)).To(Equal("cookie-token"))

		rec := httptest.NewRecorder()
		writeRuntimeJSON(rec, map[string]string{"ok": "true"})
		Expect(rec.Header().Get("Content-Type")).To(Equal("application/json"))
		Expect(rec.Body.String()).To(ContainSubstring(`"ok":"true"`))

		writeRuntimeJSON(httptest.NewRecorder(), func() {})
		failingWriter := &leafwikiFailingResponseWriter{err: errors.New("write failed")}
		writeRuntimeError(failingWriter, http.StatusForbidden, runtimeErrorCodeWorkspaceGrantDenied)
		Expect(failingWriter.statuses).To(ContainElement(http.StatusForbidden))
	})

	ginkgo.It("maps actor contexts, runtime grants, and home workspace status edges", func() {
		_, err := actorContextForUser(nil, "api_key", leafwikiRuntimeConfig{})
		Expect(err).To(MatchError("actor user is required"))

		actor, err := actorContextForUser(&coreauth.User{
			ID:       "u1",
			Username: "ada",
			Email:    "ada@example.test",
			Role:     coreauth.RoleEditor,
		}, "api_key", leafwikiRuntimeConfig{Workspace: wiki.Workspace{ID: "workspace-a"}})
		Expect(err).NotTo(HaveOccurred())
		Expect(actor.Subject).To(Equal("user:u1"))
		Expect(actor.WorkspaceID).To(Equal(workspaceid.WorkspaceID("workspace-a")))
		Expect(actor.Scopes).To(ContainElement("leafwiki:mcp"))

		Expect(wikidGrantRoleForCoreRole(coreauth.RoleViewer)).To(Equal(wikid.GrantRoleViewer))
		Expect(wikidGrantRoleForCoreRole(coreauth.RoleEditor)).To(Equal(wikid.GrantRoleEditor))
		Expect(wikidGrantRoleForCoreRole(coreauth.RoleAdmin)).To(Equal(wikid.GrantRoleAdmin))
		Expect(wikidGrantRoleForCoreRole("owner")).To(BeEmpty())
		Expect(scopesForGrantRole("owner")).To(BeNil())
		Expect(ensureRuntimeHomeGrant(nil, nil)).To(MatchError("user is required"))
		Expect(ensureRuntimeHomeGrant(nil, &coreauth.User{Role: "owner"})).To(Succeed())

		now := time.Now().UTC()
		syncHomeWorkspaceStatus(nil, nil)
		supervisor := wikid.NewWorkspaceSupervisor(wikid.WorkspaceSupervisorOptions{Now: func() time.Time { return now }})
		syncHomeWorkspaceStatus(supervisor, nil)
		Expect(supervisor.Status(wikid.HomeWorkspaceID).State).To(Equal(wikid.WorkspaceStateRegistered))

		syncHomeWorkspaceStatus(supervisor, []projectdaemon.RoleHealth{{
			Name:      projectdaemon.RoleWorkspaced,
			State:     projectdaemon.RoleStateStarting,
			PID:       22,
			URL:       " http://127.0.0.1:8001 ",
			UpdatedAt: now,
		}})
		status := supervisor.Status(wikid.HomeWorkspaceID)
		Expect(status.State).To(Equal(wikid.WorkspaceStateStarting))
		Expect(status.URL).To(Equal("http://127.0.0.1:8001"))

		syncHomeWorkspaceStatus(supervisor, []projectdaemon.RoleHealth{{
			Name:      projectdaemon.RoleWorkspaced,
			State:     projectdaemon.RoleStateStopped,
			Error:     "stopped",
			UpdatedAt: now.Add(time.Second),
		}})
		status = supervisor.Status(wikid.HomeWorkspaceID)
		Expect(status.State).To(Equal(wikid.WorkspaceStateCrashed))
		Expect(status.Error).To(Equal("stopped"))

		syncHomeWorkspaceStatus(supervisor, []projectdaemon.RoleHealth{{
			Name:      projectdaemon.RoleWorkspaced,
			State:     projectdaemon.RoleStateDegraded,
			UpdatedAt: now.Add(2 * time.Second),
		}})
		Expect(supervisor.Status(wikid.HomeWorkspaceID).State).To(Equal(wikid.WorkspaceStateRegistered))
	})

	ginkgo.It("covers runtime token, actor resolver, restart, and wait helper edges", func() {
		t := ginkgo.GinkgoT()
		w := newFrontdActorTestWiki(t)
		ginkgo.DeferCleanup(w.Close)
		cfg := leafwikiRuntimeConfig{
			Workspace:   wiki.Workspace{ID: "home"},
			DisableAuth: true,
		}

		req := httptest.NewRequest(http.MethodGet, "/mcp", nil)
		actor, err := frontdActorResolver(w, cfg)(req)
		Expect(err).NotTo(HaveOccurred())
		Expect(actor.Subject).To(Equal("user:public-editor"))
		Expect(actor.AuthMethod).To(Equal("disabled"))

		_, err = frontdMCPTokenVerifier(&wiki.Wiki{})(context.Background(), "not-an-api-key", req)
		Expect(err).To(MatchError(ContainSubstring("oauth verifier unavailable")))
		_, err = frontdMCPTokenVerifier(&wiki.Wiki{})(context.Background(), "lwk_key_secret", req)
		Expect(err).To(MatchError(ContainSubstring("api key verifier unavailable")))

		editor, err := w.UserService().CreateUser("token-editor", "token-editor@example.com", "password", coreauth.RoleEditor)
		Expect(err).NotTo(HaveOccurred())
		editorID := newFixtureUserID(editor.ID)
		created, err := w.APIKeyService().CreateAPIKey(editorID, "MCP client", editorID)
		Expect(err).NotTo(HaveOccurred())
		info, err := frontdMCPTokenVerifier(w)(context.Background(), created.Secret, req)
		Expect(err).NotTo(HaveOccurred())
		Expect(info.UserID).To(Equal(string(editor.ID)))

		rec := httptest.NewRecorder()
		handleWikidTokenVerify(rec, httptest.NewRequest(http.MethodPost, "/__leafwiki/token/verify", nil), w)
		Expect(rec.Code).To(Equal(http.StatusUnauthorized))

		rec = httptest.NewRecorder()
		invalidReq := httptest.NewRequest(http.MethodPost, "/__leafwiki/token/verify", nil)
		invalidReq.Header.Set("Authorization", "Bearer invalid")
		handleWikidTokenVerify(rec, invalidReq, w)
		Expect(rec.Code).To(Equal(http.StatusUnauthorized))

		rec = httptest.NewRecorder()
		validReq := httptest.NewRequest(http.MethodPost, "/__leafwiki/token/verify", nil)
		validReq.Header.Set("Authorization", "Bearer "+created.Secret)
		handleWikidTokenVerify(rec, validReq, w)
		Expect(rec.Code).To(Equal(http.StatusOK))
		Expect(rec.Body.String()).To(ContainSubstring(string(editor.ID)))

		authenticatedResolver := frontdMCPActorResolver(&wiki.Wiki{}, leafwikiRuntimeConfig{})
		_, err = authenticatedResolver(req)
		Expect(err).To(MatchError(ContainSubstring("token info missing")))

		disabledResolver := frontdMCPActorResolver(w, cfg)
		actor, err = disabledResolver(req)
		Expect(err).NotTo(HaveOccurred())
		Expect(actor.AuthMethod).To(Equal("disabled"))

		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		Expect(waitForInternalRuntimeRoleSignal(ctx)).To(Succeed())

		sessions := projectdaemon.NewSessionRegistry(time.Millisecond, nil)
		sessionID, err := sessions.Register()
		Expect(err).NotTo(HaveOccurred())
		Expect(sessionID).NotTo(BeEmpty())
		Expect(waitForFirstProjectDaemonSession(context.Background(), sessions, time.Millisecond)).To(Succeed())
		Expect(waitForFirstProjectDaemonSession(context.Background(), sessions, 0)).To(Succeed())

		cancelCtx, cancelWait := context.WithCancel(context.Background())
		cancelWait()
		Expect(waitForFirstProjectDaemonSession(cancelCtx, projectdaemon.NewSessionRegistry(time.Millisecond, nil), time.Millisecond)).To(MatchError(context.Canceled))
		Expect(waitForFirstProjectDaemonActivity(context.Background(), sessions, projectdaemon.NewAgentPresenceRegistry(time.Millisecond, nil), time.Millisecond)).To(Succeed())
		Expect(waitForFirstProjectDaemonActivity(context.Background(), sessions, projectdaemon.NewAgentPresenceRegistry(time.Millisecond, nil), 0)).To(Succeed())
		Expect(waitForFirstProjectDaemonActivity(cancelCtx, projectdaemon.NewSessionRegistry(time.Millisecond, nil), projectdaemon.NewAgentPresenceRegistry(time.Millisecond, nil), time.Millisecond)).To(MatchError(context.Canceled))

		idleCtx, idleCancelContext := context.WithCancel(context.Background())
		idleCanceled := false
		idleCancel := func() {
			idleCanceled = true
			idleCancelContext()
		}
		idleCallback := idleShutdownCallback(idleCtx, idleCancel, time.Hour, nil)
		idleCallback(1)
		idleCallback(1)
		Expect(idleCanceled).To(BeFalse())

		seenSessions := projectdaemon.NewSessionRegistry(time.Millisecond, nil)
		_, err = seenSessions.Register()
		Expect(err).NotTo(HaveOccurred())
		cancelIfNoSessionAfterStartupGrace(context.Background(), func() { ginkgo.Fail("seen session should not cancel") }, seenSessions, 0)
		cancelIfNoActivityAfterStartupGrace(context.Background(), func() { ginkgo.Fail("seen activity should not cancel") }, seenSessions, projectdaemon.NewAgentPresenceRegistry(time.Millisecond, nil), 0)
		cancelIfNoSessionAfterStartupGrace(cancelCtx, func() { ginkgo.Fail("canceled context should not invoke startup grace cancel") }, projectdaemon.NewSessionRegistry(time.Millisecond, nil), time.Hour)
		cancelIfNoActivityAfterStartupGrace(cancelCtx, func() { ginkgo.Fail("canceled context should not invoke activity grace cancel") }, projectdaemon.NewSessionRegistry(time.Millisecond, nil), projectdaemon.NewAgentPresenceRegistry(time.Millisecond, nil), time.Hour)

		runtimeCtx, runtimeCancel := context.WithCancel(context.Background())
		runtimeCancel()
		runtime := &wikidFrontdRuntime{
			ctx:        runtimeCtx,
			supervisor: wikid.NewSupervisor(wikid.SupervisorOptions{}),
			processes:  map[projectdaemon.RoleName]*internalRuntimeRoleProcess{},
		}
		runtime.restartRole(projectdaemon.RoleFrontd)

		activeRuntime := &wikidFrontdRuntime{
			ctx:        context.Background(),
			supervisor: wikid.NewSupervisor(wikid.SupervisorOptions{}),
			processes:  map[projectdaemon.RoleName]*internalRuntimeRoleProcess{},
		}
		activeRuntime.restartRole(projectdaemon.RoleName("unsupported"))
	})

	ginkgo.It("covers private endpoint, wikid token, and frontd actor resolution edges", func() {
		t := ginkgo.GinkgoT()
		w := newFrontdActorTestWiki(t)
		ginkgo.DeferCleanup(w.Close)
		editor, err := w.UserService().CreateUser("edge-editor", "edge-editor@example.com", "password", coreauth.RoleEditor)
		Expect(err).NotTo(HaveOccurred())
		editorID := newFixtureUserID(editor.ID)
		apiKey, err := w.APIKeyService().CreateAPIKey(editorID, "edge mcp", editorID)
		Expect(err).NotTo(HaveOccurred())

		var seenPaths []string
		var seenAuthorization []string
		var seenControlTokens []string
		privateServer := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
			seenPaths = append(seenPaths, req.URL.Path)
			seenAuthorization = append(seenAuthorization, req.Header.Get("Authorization"))
			seenControlTokens = append(seenControlTokens, req.Header.Get(projectdaemon.ControlTokenHeader))
			Expect(req.Header.Get(projectdaemon.ActorContextHeader)).To(BeEmpty())
			switch req.URL.Path {
			case "/__leafwiki/token/verify":
				writeRuntimeJSON(rw, map[string]any{
					"userId":     editor.ID,
					"scopes":     []string{"leafwiki:mcp"},
					"expiration": time.Now().UTC().Add(time.Hour),
				})
			case "/__leafwiki/actor-context":
				writeRuntimeJSON(rw, map[string]any{
					"actor": projectdaemon.ActorContext{Subject: "user:" + editor.ID, Username: editor.Username},
				})
			case "/discard":
				rw.WriteHeader(http.StatusNoContent)
			case "/structured-error":
				writeRuntimeError(rw, http.StatusForbidden, errCodeStdioAuthAPIKeyInvalid)
			default:
				http.Error(rw, "missing", http.StatusNotFound)
			}
		}))
		ginkgo.DeferCleanup(privateServer.Close)

		info, err := wikidMCPTokenVerifier(privateServer.URL+"/", "daemon-token")(context.Background(), " edge-token ", nil)
		Expect(err).NotTo(HaveOccurred())
		Expect(info.UserID).To(Equal(editor.ID))
		Expect(info.Scopes).To(ContainElement("leafwiki:mcp"))
		Expect(seenPaths).To(ContainElement("/__leafwiki/token/verify"))
		Expect(seenAuthorization).To(ContainElement("Bearer edge-token"))
		Expect(seenControlTokens).To(ContainElement("daemon-token"))
		_, err = wikidMCPTokenVerifier("http://[::1", "daemon-token")(context.Background(), "edge-token", nil)
		Expect(err).To(MatchError(ContainSubstring("invalid token")))

		req := httptest.NewRequest(http.MethodPatch, "/source", nil)
		req.RemoteAddr = "127.0.0.1:9191"
		req.Header.Set(projectdaemon.ActorContextHeader, "caller-supplied")
		var out struct {
			Actor projectdaemon.ActorContext `json:"actor"`
		}
		Expect(callWikidPrivateEndpoint(context.Background(), privateServer.URL, "daemon-token", "/__leafwiki/actor-context", req, &out)).To(Succeed())
		Expect(out.Actor.Subject).To(Equal("user:" + editor.ID))
		Expect(callWikidPrivateEndpoint(context.Background(), privateServer.URL, "daemon-token", "/discard", nil, nil)).To(Succeed())
		nilHeaderSource := &http.Request{Method: http.MethodPut, URL: &url.URL{Path: "/nil-header"}}
		Expect(callWikidPrivateEndpoint(context.Background(), privateServer.URL, "daemon-token", "/discard", nilHeaderSource, nil)).To(Succeed())
		Expect(callWikidPrivateEndpoint(context.Background(), "http://127.0.0.1:1", "daemon-token", "/discard", nil, nil)).To(HaveOccurred())

		actor, err := wikidActorResolver(privateServer.URL, "daemon-token")(httptest.NewRequest(http.MethodGet, "/mcp", nil))
		Expect(err).NotTo(HaveOccurred())
		Expect(actor.Subject).To(Equal("user:" + editor.ID))
		_, err = wikidActorResolver("http://[::1", "daemon-token")(httptest.NewRequest(http.MethodGet, "/mcp", nil))
		Expect(err).To(HaveOccurred())

		_, err = frontdPublicMCPHandler(leafwikiRuntimeConfig{}, "://bad-upstream", "daemon-token", privateServer.URL)
		Expect(err).To(HaveOccurred())

		err = callWikidPrivateEndpoint(context.Background(), privateServer.URL, "daemon-token", "/structured-error", nil, nil)
		Expect(err).To(HaveOccurred())
		Expect(isWikidPrivateAuthFailure(err)).To(BeTrue())
		var endpointErr *wikidPrivateEndpointError
		Expect(errors.As(err, &endpointErr)).To(BeTrue())
		Expect(endpointErr.Error()).To(ContainSubstring(string(errCodeStdioAuthAPIKeyInvalid)))
		Expect((*wikidPrivateEndpointError)(nil).Error()).To(BeEmpty())
		Expect((&wikidPrivateEndpointError{Path: "/empty", StatusCode: 499}).Error()).To(ContainSubstring("status 499"))
		Expect(isWikidPrivateAuthFailure(errors.New("plain"))).To(BeFalse())
		Expect(isWikidPrivateAuthFailure(&wikidPrivateEndpointError{StatusCode: http.StatusInternalServerError})).To(BeFalse())

		_, err = wikidMCPTokenVerifier(privateServer.URL, "daemon-token")(context.Background(), "edge-token", httptest.NewRequest(http.MethodGet, "/mcp", nil))
		Expect(err).NotTo(HaveOccurred())
		_, err = wikidMCPTokenVerifier(privateServer.URL, "daemon-token")(context.Background(), "edge-token", httptest.NewRequest(http.MethodGet, "/missing", nil))
		Expect(err).NotTo(HaveOccurred())

		cfg := leafwikiRuntimeConfig{Workspace: wiki.Workspace{ID: "workspace-a"}}
		resolver := frontdMCPActorResolver(w, cfg)
		resolved, resolveErr := resolveWithSDKToken(resolver, apiKey.Secret, editor.ID)
		Expect(resolveErr).NotTo(HaveOccurred())
		Expect(resolved.Subject).To(Equal("user:" + editor.ID))
		Expect(resolved.AuthMethod).To(Equal("api_key"))

		resolved, resolveErr = resolveWithSDKToken(resolver, "oauth-token", editor.ID)
		Expect(resolveErr).NotTo(HaveOccurred())
		Expect(resolved.AuthMethod).To(Equal("oauth"))

		_, resolveErr = resolveWithSDKToken(frontdMCPActorResolver(&wiki.Wiki{}, cfg), "oauth-token", editor.ID)
		Expect(resolveErr).To(MatchError(ContainSubstring("user service is unavailable")))

		_, resolveErr = resolveWithSDKToken(resolver, "oauth-token", "missing-user")
		Expect(resolveErr).To(HaveOccurred())

		_, _, err = frontdActorUser(httptest.NewRequest(http.MethodGet, "/mcp", nil), &wiki.Wiki{}, leafwikiRuntimeConfig{})
		Expect(err).To(MatchError(ContainSubstring("missing credentials")))
		_, err = frontdActorResolver(w, leafwikiRuntimeConfig{})(httptest.NewRequest(http.MethodGet, "/mcp", nil))
		Expect(err).To(MatchError(ContainSubstring("missing credentials")))

		mcpReq := httptest.NewRequest(http.MethodPost, "/mcp", nil)
		mcpReq.Header.Set("Authorization", "Bearer lwk_key_invalid")
		_, _, err = frontdActorUser(mcpReq, w, leafwikiRuntimeConfig{})
		Expect(err).To(HaveOccurred())

		oauthReq := httptest.NewRequest(http.MethodPost, "/mcp", nil)
		oauthReq.Header.Set("Authorization", "Bearer oauth-token")
		_, _, err = frontdActorUser(oauthReq, &wiki.Wiki{}, leafwikiRuntimeConfig{})
		Expect(err).To(MatchError(ContainSubstring("oauth actor services are unavailable")))
		_, _, err = frontdActorUser(oauthReq, w, leafwikiRuntimeConfig{})
		Expect(err).To(HaveOccurred())

		cookieReq := httptest.NewRequest(http.MethodGet, "/", nil)
		cookieReq.AddCookie(&http.Cookie{Name: "leafwiki_at", Value: "invalid-token"})
		_, _, err = frontdActorUser(cookieReq, w, leafwikiRuntimeConfig{})
		Expect(err).To(HaveOccurred())

		publicReq := httptest.NewRequest(http.MethodGet, "/", nil)
		publicUser, method, err := frontdActorUser(publicReq, &wiki.Wiki{}, leafwikiRuntimeConfig{PublicAccess: true})
		Expect(err).NotTo(HaveOccurred())
		Expect(publicUser.ID).To(Equal("public-viewer"))
		Expect(method).To(Equal("public_access"))
	})

	ginkgo.It("covers wikid-frontd runtime role orchestration through a starter seam", func() {
		var calls []internalRuntimeRoleStartupConfig
		var doneChans []chan error
		swapInternalRuntimeRoleStarter(func(startup internalRuntimeRoleStartupConfig) (*internalRuntimeRoleProcess, internalRuntimeRoleReady, error) {
			calls = append(calls, startup)
			proc, done := newLeafwikiRuntimeRoleProcess(startup.Role, 100+len(calls))
			doneChans = append(doneChans, done)
			switch startup.Role {
			case projectdaemon.RoleWorkspaced:
				return proc, internalRuntimeRoleReady{Role: startup.Role, PID: proc.pid, URL: "http://workspaced.local", Private: true}, nil
			case projectdaemon.RoleFrontd:
				return proc, internalRuntimeRoleReady{Role: startup.Role, PID: proc.pid, URL: "http://frontd.local"}, nil
			default:
				return nil, internalRuntimeRoleReady{}, errors.New("unexpected role")
			}
		})

		runtime, err := startWikidFrontdRuntime(context.Background(), leafwikiRuntimeConfig{}, "daemon-token", "http://wikid.local")
		Expect(err).NotTo(HaveOccurred())
		ginkgo.DeferCleanup(func() {
			Expect(runtime.stop(context.Background())).To(Succeed())
			releaseLeafwikiRuntimeRoleProcesses(doneChans, context.Canceled)
		})

		Expect(calls).To(HaveLen(2))
		Expect(calls[0].Role).To(Equal(projectdaemon.RoleWorkspaced))
		Expect(calls[0].Runtime.Host).To(Equal("127.0.0.1"))
		Expect(calls[0].Runtime.Port).To(Equal("0"))
		Expect(calls[1].Role).To(Equal(projectdaemon.RoleFrontd))
		Expect(calls[1].WorkspacedURL).To(Equal("http://workspaced.local"))
		Expect(calls[1].WikidURL).To(Equal("http://wikid.local"))
		Expect(runtime.workspacedURL).To(Equal("http://workspaced.local"))
		Expect(runtime.roleHealthSnapshot()).To(ContainElement(Satisfy(func(role projectdaemon.RoleHealth) bool {
			return role.Name == projectdaemon.RoleFrontd && role.URL == "http://frontd.local"
		})))
	})

	ginkgo.It("covers wikid-frontd runtime startup and restart failures", func() {
		swapInternalRuntimeRoleStarter(func(startup internalRuntimeRoleStartupConfig) (*internalRuntimeRoleProcess, internalRuntimeRoleReady, error) {
			return nil, internalRuntimeRoleReady{}, errors.New(string(startup.Role) + " failed")
		})
		runtime, err := startWikidFrontdRuntime(context.Background(), leafwikiRuntimeConfig{}, "daemon-token", "http://wikid.local")
		Expect(runtime).To(BeNil())
		Expect(err).To(MatchError("workspaced failed"))

		var doneChans []chan error
		swapInternalRuntimeRoleStarter(func(startup internalRuntimeRoleStartupConfig) (*internalRuntimeRoleProcess, internalRuntimeRoleReady, error) {
			if startup.Role == projectdaemon.RoleFrontd {
				return nil, internalRuntimeRoleReady{}, errors.New("frontd failed")
			}
			proc, done := newLeafwikiRuntimeRoleProcess(startup.Role, 201)
			doneChans = append(doneChans, done)
			return proc, internalRuntimeRoleReady{Role: startup.Role, PID: proc.pid, URL: "http://workspaced.local"}, nil
		})
		runtime, err = startWikidFrontdRuntime(context.Background(), leafwikiRuntimeConfig{}, "daemon-token", "http://wikid.local")
		Expect(runtime).To(BeNil())
		Expect(err).To(MatchError("frontd failed"))
		releaseLeafwikiRuntimeRoleProcesses(doneChans, context.Canceled)

		runtime = &wikidFrontdRuntime{
			ctx:        context.Background(),
			supervisor: wikid.NewSupervisor(wikid.SupervisorOptions{}),
			processes:  map[projectdaemon.RoleName]*internalRuntimeRoleProcess{},
		}
		Expect(runtime.startFrontdLocked()).To(MatchError("workspaced URL is unavailable"))
	})

	ginkgo.It("covers runtime role restart and crash publishing paths", func() {
		var doneChans []chan error
		swapInternalRuntimeRoleStarter(func(startup internalRuntimeRoleStartupConfig) (*internalRuntimeRoleProcess, internalRuntimeRoleReady, error) {
			proc, done := newLeafwikiRuntimeRoleProcess(startup.Role, 300+len(doneChans))
			doneChans = append(doneChans, done)
			ready := internalRuntimeRoleReady{Role: startup.Role, PID: proc.pid, URL: "http://" + string(startup.Role)}
			if startup.Role == projectdaemon.RoleWorkspaced {
				ready.Private = true
			}
			return proc, ready, nil
		})

		runtime := &wikidFrontdRuntime{
			cfg:           leafwikiRuntimeConfig{},
			daemonToken:   "daemon-token",
			ctx:           context.Background(),
			supervisor:    wikid.NewSupervisor(wikid.SupervisorOptions{}),
			processes:     map[projectdaemon.RoleName]*internalRuntimeRoleProcess{},
			workspacedURL: "http://workspaced.local",
			wikidURL:      "http://wikid.local",
		}
		ginkgo.DeferCleanup(func() {
			Expect(runtime.stop(context.Background())).To(Succeed())
			releaseLeafwikiRuntimeRoleProcesses(doneChans, context.Canceled)
		})
		runtime.restartRole(projectdaemon.RoleWorkspaced)
		runtime.restartRole(projectdaemon.RoleFrontd)
		Expect(runtime.roleHealthSnapshot()).To(ContainElements(
			Satisfy(func(role projectdaemon.RoleHealth) bool {
				return role.Name == projectdaemon.RoleWorkspaced && role.State == projectdaemon.RoleStateReady
			}),
			Satisfy(func(role projectdaemon.RoleHealth) bool {
				return role.Name == projectdaemon.RoleFrontd && role.State == projectdaemon.RoleStateReady
			}),
		))

		crashDoneChans := []chan error{}
		crashingProc, crashDone := newLeafwikiRuntimeRoleProcess(projectdaemon.RoleFrontd, 401)
		crashDoneChans = append(crashDoneChans, crashDone)
		crashRuntime := &wikidFrontdRuntime{
			ctx:        context.Background(),
			supervisor: wikid.NewSupervisor(wikid.SupervisorOptions{MaxRestarts: 1}),
			processes:  map[projectdaemon.RoleName]*internalRuntimeRoleProcess{projectdaemon.RoleFrontd: crashingProc},
		}
		crashRuntime.supervisor.MarkReady(projectdaemon.RoleFrontd, crashingProc.pid, "http://frontd.local", false)
		crashRuntime.supervisor.RecordCrash(projectdaemon.RoleFrontd, "previous crash")
		var published [][]projectdaemon.RoleHealth
		crashRuntime.onRolesChanged = func(roles []projectdaemon.RoleHealth) {
			published = append(published, roles)
		}
		go crashRuntime.monitorRoleProcess(crashingProc)
		crashDone <- errors.New("frontd exited")
		waitForLeafwikiRoleState(crashRuntime, projectdaemon.RoleFrontd, projectdaemon.RoleStateCrashed)
		Expect(published).NotTo(BeEmpty())
		Expect(crashRuntime.supervisor.State(projectdaemon.RoleFrontd).Error).To(Equal("frontd exited"))
		releaseLeafwikiRuntimeRoleProcesses(crashDoneChans, context.Canceled)

		scheduledProc, scheduledDone := newLeafwikiRuntimeRoleProcess(projectdaemon.RoleFrontd, 402)
		scheduledRuntime := &wikidFrontdRuntime{
			cfg:           leafwikiRuntimeConfig{},
			daemonToken:   "daemon-token",
			ctx:           context.Background(),
			supervisor:    wikid.NewSupervisor(wikid.SupervisorOptions{MaxRestarts: 2, Backoff: time.Nanosecond}),
			processes:     map[projectdaemon.RoleName]*internalRuntimeRoleProcess{projectdaemon.RoleFrontd: scheduledProc},
			workspacedURL: "http://workspaced.local",
			wikidURL:      "http://wikid.local",
		}
		scheduledRuntime.supervisor.MarkReady(projectdaemon.RoleFrontd, scheduledProc.pid, "http://frontd.local", false)
		go scheduledRuntime.monitorRoleProcess(scheduledProc)
		scheduledDone <- errors.New("frontd exited once")
		waitForLeafwikiRoleState(scheduledRuntime, projectdaemon.RoleFrontd, projectdaemon.RoleStateReady)
	})

	ginkgo.It("covers daemon auth, registry, descriptor, and private STDIO helper edges", func() {
		t := ginkgo.GinkgoT()
		dataDir := filepath.Join(t.TempDir(), "data")
		rootDir := filepath.Join(t.TempDir(), "root")
		Expect(os.MkdirAll(rootDir, 0o755)).To(Succeed())

		Expect(validateAuthStartupConfig(leafwikiRuntimeConfig{DisableAuth: true})).To(Succeed())
		Expect(validateAuthStartupConfig(leafwikiRuntimeConfig{})).To(MatchError(ContainSubstring("JWT secret is required")))
		Expect(validateAuthStartupConfig(leafwikiRuntimeConfig{JWTSecret: "jwt"})).To(MatchError(ContainSubstring("admin password is required")))
		Expect(validateAuthStartupConfig(leafwikiRuntimeConfig{JWTSecret: "jwt", AdminPassword: "admin"})).To(Succeed())

		logPath := filepath.Join(dataDir, "startup.log")
		logStartupValidationFailure(leaflogging.Config{Target: leaflogging.TargetStderr}, "ignored")
		logStartupValidationFailure(leaflogging.Config{Target: leaflogging.TargetFile, FilePath: logPath}, "startup failed")
		Expect(os.ReadFile(logPath)).To(ContainSubstring("startup failed"))
		parentFile := filepath.Join(t.TempDir(), "not-a-dir")
		Expect(os.WriteFile(parentFile, []byte("file"), 0o600)).To(Succeed())
		logStartupValidationFailure(leaflogging.Config{Target: leaflogging.TargetFile, FilePath: filepath.Join(parentFile, "startup.log")}, "ignored")

		Expect(os.MkdirAll(authStorageDirForRuntime(dataDir), 0o755)).To(Succeed())
		output := captureLeafwikiStdout(func() {
			resetAdminPasswordCommand(dataDir)
		})
		Expect(output).To(ContainSubstring("admin"))
		cleanupFailureDir := filepath.Join(t.TempDir(), "cleanup-failure")
		Expect(os.MkdirAll(filepath.Join(cleanupFailureDir, "users.db", "child"), 0o755)).To(Succeed())
		expectLeafwikiExit(1, func() {
			resetAdminPasswordCommand(cleanupFailureDir)
		})
		resetFailureDir := filepath.Join(t.TempDir(), "reset-failure")
		Expect(os.MkdirAll(resetFailureDir, 0o755)).To(Succeed())
		Expect(os.WriteFile(filepath.Join(resetFailureDir, ".leafwiki"), []byte("not a directory"), 0o600)).To(Succeed())
		expectLeafwikiExit(1, func() {
			resetAdminPasswordCommand(resetFailureDir)
		})

		now := time.Now().UTC()
		var privateControlTokens []string
		var privateAuthorizations []string
		var privateWorkspaceIDs []string
		privateServer := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
			privateControlTokens = append(privateControlTokens, req.Header.Get(projectdaemon.ControlTokenHeader))
			switch req.URL.Path {
			case "/__leafwiki/actor-context":
				if req.Header.Get("Authorization") == "Bearer rejected" {
					writeRuntimeError(rw, http.StatusUnauthorized, errCodeStdioAuthAPIKeyInvalid)
					return
				}
				if req.Header.Get("Authorization") == "Bearer server-error" {
					http.Error(rw, "actor context failed", http.StatusInternalServerError)
					return
				}
				privateAuthorizations = append(privateAuthorizations, req.Header.Get("Authorization"))
				privateWorkspaceIDs = append(privateWorkspaceIDs, req.Header.Get(projectdaemon.WorkspaceIDHeader))
				writeRuntimeJSON(rw, map[string]any{"actor": projectdaemon.ActorContext{
					Version:     1,
					Issuer:      projectdaemon.ActorContextIssuerWikid,
					Subject:     "user:stdio",
					Username:    "stdio",
					Role:        coreauth.RoleEditor,
					Scopes:      []string{"leafwiki:workspace:read", "leafwiki:workspace:write"},
					WorkspaceID: "workspace-a",
					AuthMethod:  "api_key",
					IssuedAt:    now,
					ExpiresAt:   now.Add(time.Hour),
				}})
			case "/mcp":
				http.Error(rw, "not ready", http.StatusInternalServerError)
			default:
				http.NotFound(rw, req)
			}
		}))
		ginkgo.DeferCleanup(privateServer.Close)

		desc := &projectdaemon.Descriptor{ControlURL: privateServer.URL, ControlToken: "daemon-token", WorkspaceID: "workspace-a"}
		encoded, err := daemonStdioActorContext(context.Background(), desc, leafwikiRuntimeConfig{APIKey: " stdio-key "})
		Expect(err).NotTo(HaveOccurred())
		decoded, err := projectdaemon.DecodeActorContext(encoded, projectdaemon.ActorContextValidation{Now: now, WorkspaceID: "workspace-a"})
		Expect(err).NotTo(HaveOccurred())
		Expect(decoded.Subject).To(Equal("user:stdio"))
		Expect(privateControlTokens).To(ContainElement("daemon-token"))
		Expect(privateAuthorizations).To(ContainElement("Bearer stdio-key"))
		Expect(privateWorkspaceIDs).To(ContainElement(workspaceid.WorkspaceID("workspace-a").HTTPHeaderValue()))

		_, err = daemonStdioActorContext(context.Background(), desc, leafwikiRuntimeConfig{APIKey: "rejected"})
		Expect(err).To(MatchError(ContainSubstring("unauthorized native STDIO API key")))
		_, err = daemonStdioActorContext(context.Background(), desc, leafwikiRuntimeConfig{APIKey: "server-error"})
		Expect(err).To(MatchError(ContainSubstring("resolve native STDIO actor context")))
		previousEncodeActorContext := encodeActorContextForRuntime
		ginkgo.DeferCleanup(func() {
			encodeActorContextForRuntime = previousEncodeActorContext
		})
		encodeActorContextForRuntime = func(projectdaemon.ActorContext) (string, error) {
			return "", errors.New("encode failed")
		}
		_, err = daemonStdioActorContext(context.Background(), desc, leafwikiRuntimeConfig{APIKey: "stdio-key"})
		Expect(err).To(MatchError(ContainSubstring("encode native STDIO actor context")))
		encodeActorContextForRuntime = previousEncodeActorContext

		layout := wikid.GlobalLayout(dataDir)
		homeCfg := projectdaemon.Config{DataDir: layout.HomeDir, RootDir: layout.HomeRootDir}
		workspace, ok, err := registeredFederatedWorkspaceForRequest(layout, homeCfg)
		Expect(err).NotTo(HaveOccurred())
		Expect(ok).To(BeTrue())
		Expect(workspace.ID).To(Equal(wikid.HomeWorkspaceID))

		registry := wikid.NewRegistryService(wikid.NewRegistryStore(layout.DBPath), layout)
		registered, isHome, err := registerFederatedFirstContact(layout, homeCfg, leafwikiRuntimeConfig{})
		Expect(err).NotTo(HaveOccurred())
		Expect(isHome).To(BeTrue())
		Expect(registered.ID).To(Equal(wikid.HomeWorkspaceID))
		grant, grantOK, err := federatedStdioAPIKeyWorkspaceGrant(layout, leafwikiRuntimeConfig{}, "workspace-a")
		Expect(err).NotTo(HaveOccurred())
		Expect(grantOK).To(BeFalse())
		Expect(grant).To(Equal(wikid.Grant{}))
		_, _, err = federatedStdioAPIKeyWorkspaceGrant(layout, leafwikiRuntimeConfig{APIKey: "lwk_key_missing"}, "workspace-a")
		Expect(err).To(MatchError(ContainSubstring("resolve native STDIO API-key grant user")))
		unsupportedRoleAuthDir := authStorageDirForRuntime(layout.HomeDir)
		Expect(os.MkdirAll(unsupportedRoleAuthDir, 0o755)).To(Succeed())
		userStore, err := coreauth.NewUserStore(unsupportedRoleAuthDir)
		Expect(err).NotTo(HaveOccurred())
		ginkgo.DeferCleanup(userStore.Close)
		unsupportedRoleUser := &coreauth.User{ID: "unsupported-role-user", Username: "unsupported-role", Email: "unsupported@example.test", Password: "hash", Role: "owner"}
		Expect(userStore.CreateUser(unsupportedRoleUser)).To(Succeed())
		apiKeyStore, err := coreauth.NewAPIKeyStore(unsupportedRoleAuthDir)
		Expect(err).NotTo(HaveOccurred())
		apiKeyService := coreauth.NewAPIKeyService(apiKeyStore, coreauth.NewUserService(userStore))
		ginkgo.DeferCleanup(apiKeyService.Close)
		unsupportedRoleKey, err := apiKeyService.CreateAPIKey(coreauth.UserIDFromString(unsupportedRoleUser.ID), "unsupported role", coreauth.UserIDFromString(unsupportedRoleUser.ID))
		Expect(err).NotTo(HaveOccurred())
		_, _, err = federatedStdioAPIKeyWorkspaceGrant(layout, leafwikiRuntimeConfig{APIKey: unsupportedRoleKey.Secret}, "workspace-a")
		Expect(err).To(MatchError(ContainSubstring("cannot access workspaces")))

		workspaceData := filepath.Join(t.TempDir(), "workspace-data")
		workspaceRoot := filepath.Join(t.TempDir(), "workspace-root")
		registered, err = registry.RegisterWorkspace(wikid.RegisterWorkspaceRequest{
			DisplayName: "Workspace A",
			DataDir:     workspaceData,
			RootDir:     workspaceRoot,
		})
		Expect(err).NotTo(HaveOccurred())
		workspace, ok, err = registeredFederatedWorkspaceForRequest(layout, projectdaemon.Config{DataDir: registered.DataDir, RootDir: registered.RootDir})
		Expect(err).NotTo(HaveOccurred())
		Expect(ok).To(BeTrue())
		Expect(workspace.ID).To(Equal(registered.ID))

		descriptorPath := filepath.Join(t.TempDir(), "descriptor.json")
		ownerCfg := projectdaemon.Config{DataDir: dataDir, RootDir: rootDir}
		desc, healthy, err := readHealthyProjectDaemon(context.Background(), descriptorPath, ownerCfg)
		Expect(err).NotTo(HaveOccurred())
		Expect(desc).To(BeNil())
		Expect(healthy).To(BeFalse())

		Expect(os.WriteFile(descriptorPath, []byte("{bad"), 0o600)).To(Succeed())
		desc, healthy, err = readHealthyProjectDaemon(context.Background(), descriptorPath, ownerCfg)
		Expect(err).NotTo(HaveOccurred())
		Expect(desc).To(BeNil())
		Expect(healthy).To(BeFalse())
		_, err = os.Stat(descriptorPath)
		Expect(os.IsNotExist(err)).To(BeTrue())

		staleDesc := &projectdaemon.Descriptor{
			SchemaVersion: 0,
			DataDir:       ownerCfg.DataDir,
			RootDir:       ownerCfg.RootDir,
			ControlURL:    "http://127.0.0.1:1",
		}
		Expect(projectdaemon.WriteDescriptorAtomic(descriptorPath, staleDesc)).To(Succeed())
		desc, healthy, err = readHealthyProjectDaemon(context.Background(), descriptorPath, ownerCfg)
		Expect(err).NotTo(HaveOccurred())
		Expect(desc.SchemaVersion).To(BeZero())
		Expect(healthy).To(BeFalse())

		healthyDesc := &projectdaemon.Descriptor{
			SchemaVersion:   projectdaemon.DescriptorSchemaVersion,
			Role:            projectdaemon.RoleWorkspaced,
			PID:             os.Getpid(),
			DataDir:         ownerCfg.DataDir,
			RootDir:         ownerCfg.RootDir,
			PrivateMCPURL:   privateServer.URL + "/mcp",
			PrivateMCPToken: "token",
		}
		Expect(projectDaemonDescriptorHealthy(context.Background(), healthyDesc)).To(BeFalse())
		healthyDesc.PrivateMCPURL = "https://example.com/mcp"
		_, err = projectDaemonDescriptorHealthy(context.Background(), healthyDesc)
		Expect(err).To(MatchError(ContainSubstring("private MCP URL is not trusted")))
	})

	ginkgo.It("covers wikid actor-context and remote-user branches", func() {
		t := ginkgo.GinkgoT()
		w := newFrontdActorTestWiki(t)
		ginkgo.DeferCleanup(w.Close)
		editor, err := w.UserService().CreateUser("remote-editor", "remote-editor@example.com", "password", coreauth.RoleEditor)
		Expect(err).NotTo(HaveOccurred())

		cfg := leafwikiRuntimeConfig{Workspace: wiki.Workspace{ID: "home"}}
		rec := httptest.NewRecorder()
		handleWikidActorContext(rec, httptest.NewRequest(http.MethodPost, "/__leafwiki/actor-context", nil), &wiki.Wiki{}, cfg, nil, nil)
		Expect(rec.Code).To(Equal(http.StatusUnauthorized))

		rec = httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/__leafwiki/actor-context", nil)
		req.Header.Set(projectdaemon.WorkspaceIDHeader, "not valid")
		handleWikidActorContext(rec, req, w, leafwikiRuntimeConfig{DisableAuth: true}, nil, nil)
		Expect(rec.Code).To(Equal(http.StatusNotFound))

		layout := wikid.GlobalLayout(t.TempDir())
		registry := wikid.NewRegistryService(wikid.NewRegistryStore(layout.DBPath), layout)
		rec = httptest.NewRecorder()
		req = httptest.NewRequest(http.MethodPost, "/__leafwiki/actor-context", nil)
		req.Header.Set(projectdaemon.WorkspaceIDHeader, workspaceid.WorkspaceID("missing-workspace").HTTPHeaderValue())
		handleWikidActorContext(rec, req, w, leafwikiRuntimeConfig{DisableAuth: true}, registry, nil)
		Expect(rec.Code).To(Equal(http.StatusNotFound))

		grantLayout := wikid.GlobalLayout(t.TempDir())
		grantRegistry := wikid.NewRegistryService(wikid.NewRegistryStore(grantLayout.DBPath), grantLayout)
		_, err = grantRegistry.BootstrapHomeWorkspace(grantLayout.HomeDir, grantLayout.HomeRootDir)
		Expect(err).NotTo(HaveOccurred())
		grantedWorkspace, err := grantRegistry.RegisterWorkspace(wikid.RegisterWorkspaceRequest{
			DisplayName: "Workspace B",
			DataDir:     filepath.Join(t.TempDir(), "workspace-b-data"),
			RootDir:     filepath.Join(t.TempDir(), "workspace-b-root"),
		})
		Expect(err).NotTo(HaveOccurred())
		grants := wikid.NewGrantStore(grantLayout.DBPath)
		rec = httptest.NewRecorder()
		req = httptest.NewRequest(http.MethodPost, "/__leafwiki/actor-context", nil)
		req.Header.Set(projectdaemon.WorkspaceIDHeader, grantedWorkspace.ID.HTTPHeaderValue())
		handleWikidActorContext(rec, req, w, leafwikiRuntimeConfig{DisableAuth: true}, nil, grants)
		Expect(rec.Code).To(Equal(http.StatusForbidden))
		Expect(rec.Body.String()).To(ContainSubstring(string(runtimeErrorCodeWorkspaceGrantDenied)))

		Expect(grants.Upsert(wikid.Grant{Subject: "user:public-editor", WorkspaceID: grantedWorkspace.ID, Role: wikid.GrantRoleViewer})).To(Succeed())
		rec = httptest.NewRecorder()
		handleWikidActorContext(rec, req, w, leafwikiRuntimeConfig{DisableAuth: true}, nil, grants)
		Expect(rec.Code).To(Equal(http.StatusOK))
		Expect(rec.Body.String()).To(ContainSubstring(grantedWorkspace.ID.String()))

		previousGrantsForSubject := grantsForSubjectForRuntime
		grantsForSubjectForRuntime = func(*wikid.GrantStore, string) ([]wikid.Grant, error) {
			return nil, errors.New("grant lookup failed")
		}
		rec = httptest.NewRecorder()
		handleWikidActorContext(rec, req, w, leafwikiRuntimeConfig{DisableAuth: true}, nil, grants)
		Expect(rec.Code).To(Equal(http.StatusInternalServerError))
		grantsForSubjectForRuntime = previousGrantsForSubject

		admin, err := w.UserService().CreateUser("remote-admin", "remote-admin@example.com", "password", coreauth.RoleAdmin)
		Expect(err).NotTo(HaveOccurred())
		token, err := w.AuthService().Login(admin.Username, "password")
		Expect(err).NotTo(HaveOccurred())
		adminReq := httptest.NewRequest(http.MethodPost, "/__leafwiki/actor-context", nil)
		adminReq.AddCookie(&http.Cookie{Name: "leafwiki_at", Value: token.Token})
		adminReq.Header.Set(projectdaemon.WorkspaceIDHeader, workspaceid.WorkspaceID("admin-workspace").HTTPHeaderValue())
		rec = httptest.NewRecorder()
		handleWikidActorContext(rec, adminReq, w, cfg, nil, grants)
		Expect(rec.Code).To(Equal(http.StatusOK))
		Expect(rec.Body.String()).To(ContainSubstring("leafwiki:workspace:admin"))

		_, err = actorContextForWorkspaceGrant(nil, "api_key", cfg, "workspace-a", wikid.GrantRoleViewer)
		Expect(err).To(MatchError("actor user is required"))
		Expect(seedRuntimeHomeGrants(grants, leafwikiRuntimeConfig{DisableAuth: true, PublicAccess: true})).To(Succeed())

		remoteReq := httptest.NewRequest(http.MethodGet, "/", nil)
		_, _, _, err = frontdRemoteUser(remoteReq, w, leafwikiRuntimeConfig{EnableHTTPRemoteUser: true, TrustedProxyIPsRaw: "bad-cidr"})
		Expect(err).To(MatchError(ContainSubstring("bad-cidr")))
		remoteReq.RemoteAddr = "192.0.2.10:1111"
		user, method, ok, err := frontdRemoteUser(remoteReq, w, leafwikiRuntimeConfig{EnableHTTPRemoteUser: true, TrustedProxyIPsRaw: "127.0.0.1"})
		Expect(err).NotTo(HaveOccurred())
		Expect(ok).To(BeFalse())
		Expect(user).To(BeNil())
		Expect(method).To(BeEmpty())

		remoteReq.RemoteAddr = "127.0.0.1:1111"
		user, method, ok, err = frontdRemoteUser(remoteReq, w, leafwikiRuntimeConfig{EnableHTTPRemoteUser: true, TrustedProxyIPsRaw: "127.0.0.1"})
		Expect(err).NotTo(HaveOccurred())
		Expect(ok).To(BeFalse())
		Expect(user).To(BeNil())
		Expect(method).To(BeEmpty())

		remoteReq.Header.Set("Remote-User", editor.Username)
		_, _, ok, err = frontdRemoteUser(remoteReq, &wiki.Wiki{}, leafwikiRuntimeConfig{EnableHTTPRemoteUser: true, TrustedProxyIPsRaw: "127.0.0.1"})
		Expect(ok).To(BeTrue())
		Expect(err).To(MatchError("remote user service is unavailable"))

		user, method, ok, err = frontdRemoteUser(remoteReq, w, leafwikiRuntimeConfig{EnableHTTPRemoteUser: true, TrustedProxyIPsRaw: "127.0.0.1"})
		Expect(err).NotTo(HaveOccurred())
		Expect(ok).To(BeTrue())
		Expect(user.ID).To(Equal(editor.ID))
		Expect(method).To(Equal("remote_user"))

		remoteReq.Header.Set("Remote-User", "missing-user")
		_, _, ok, err = frontdRemoteUser(remoteReq, w, leafwikiRuntimeConfig{EnableHTTPRemoteUser: true, TrustedProxyIPsRaw: "127.0.0.1"})
		Expect(ok).To(BeTrue())
		Expect(err).To(HaveOccurred())
	})

	ginkgo.It("covers cancellable wikid-frontd owner boot with fake runtime roles", func() {
		t := ginkgo.GinkgoT()
		dataDir := filepath.Join(t.TempDir(), "data")
		rootDir := filepath.Join(t.TempDir(), "root")

		var doneChans []chan error
		swapInternalRuntimeRoleStarter(func(startup internalRuntimeRoleStartupConfig) (*internalRuntimeRoleProcess, internalRuntimeRoleReady, error) {
			proc, done := newLeafwikiRuntimeRoleProcess(startup.Role, 900+len(doneChans))
			doneChans = append(doneChans, done)
			switch startup.Role {
			case projectdaemon.RoleWorkspaced:
				return proc, internalRuntimeRoleReady{Role: startup.Role, PID: proc.pid, URL: "http://workspaced.local", Private: true}, nil
			case projectdaemon.RoleFrontd:
				return proc, internalRuntimeRoleReady{Role: startup.Role, PID: proc.pid, URL: "http://frontd.local"}, nil
			default:
				return nil, internalRuntimeRoleReady{}, errors.New("unexpected runtime role")
			}
		})

		ctx, cancel := context.WithCancel(context.Background())
		ginkgo.DeferCleanup(cancel)
		done := make(chan error, 1)
		go func() {
			done <- runProjectDaemonOwner(ctx, leafwikiRuntimeConfig{
				Workspace: wiki.Workspace{
					DataDir: dataDir,
					RootDir: rootDir,
				},
				Host:                 "127.0.0.1",
				Port:                 "0",
				DisableAuth:          true,
				PublicAccess:         true,
				AllowInsecure:        true,
				EnableHTTPRemoteUser: true,
				HTTPRemoteUserHeader: "Remote-User",
				TrustedProxyIPsRaw:   "127.0.0.1",
				Logging:              leaflogging.Config{Target: leaflogging.TargetStderr},
				DaemonIdleTimeout:    time.Hour,
				RuntimeStack:         projectdaemon.RuntimeStackWikidFrontd,
			})
		}()

		desc := waitForLeafwikiDescriptor(projectdaemon.DescriptorPath(dataDir))
		Expect(desc.ControlURL).To(HavePrefix("http://127.0.0.1:"))
		Expect(desc.PrivateMCPURL).To(Equal("http://workspaced.local/mcp"))
		Expect(desc.PublicURL).To(Equal("http://frontd.local"))
		Expect(desc.Roles).To(ContainElements(
			Satisfy(func(role projectdaemon.RoleHealth) bool {
				return role.Name == projectdaemon.RoleWorkspaced && role.Private && role.URL == "http://workspaced.local"
			}),
			Satisfy(func(role projectdaemon.RoleHealth) bool {
				return role.Name == projectdaemon.RoleFrontd && role.URL == "http://frontd.local"
			}),
		))

		cancel()
		releaseLeafwikiRuntimeRoleProcesses(doneChans, context.Canceled)
		select {
		case err := <-done:
			Expect(err).NotTo(HaveOccurred())
		case <-time.After(3 * time.Second):
			ginkgo.Fail("runProjectDaemonOwner did not stop after context cancellation")
		}
	})

	ginkgo.It("covers project daemon lock, descriptor, and foreground wait branches", func() {
		t := ginkgo.GinkgoT()
		dataDir := filepath.Join(t.TempDir(), "data")
		rootDir := filepath.Join(t.TempDir(), "root")
		Expect(os.MkdirAll(dataDir, 0o755)).To(Succeed())
		Expect(os.MkdirAll(rootDir, 0o755)).To(Succeed())

		held, err := projectDaemonLocksHeld(dataDir, rootDir)
		Expect(err).NotTo(HaveOccurred())
		Expect(held).To(BeFalse())

		dataLock, err := locking.AcquireDataDirLock(dataDir)
		Expect(err).NotTo(HaveOccurred())
		held, err = projectDaemonLocksHeld(dataDir, rootDir)
		Expect(err).NotTo(HaveOccurred())
		Expect(held).To(BeFalse())
		free, err := projectDaemonLocksFree(dataDir, rootDir)
		Expect(err).NotTo(HaveOccurred())
		Expect(free).To(BeFalse())
		Expect(dataLock.Release()).To(Succeed())

		rootLock, err := locking.AcquireRootDirLock(rootDir)
		Expect(err).NotTo(HaveOccurred())
		disjoint, err := projectDaemonDataLockFreeRootLockHeld(dataDir, rootDir)
		Expect(err).NotTo(HaveOccurred())
		Expect(disjoint).To(BeTrue())

		dataLock, err = locking.AcquireDataDirLock(dataDir)
		Expect(err).NotTo(HaveOccurred())
		held, err = projectDaemonLocksHeld(dataDir, rootDir)
		Expect(err).NotTo(HaveOccurred())
		Expect(held).To(BeTrue())

		descriptorPath := filepath.Join(t.TempDir(), "descriptor.json")
		Expect(os.WriteFile(descriptorPath, []byte("{bad"), 0o600)).To(Succeed())
		_, _, err = readHealthyProjectDaemon(context.Background(), descriptorPath, projectdaemon.Config{DataDir: dataDir, RootDir: rootDir})
		Expect(err).To(MatchError(ContainSubstring("read project daemon descriptor")))

		stalePath := filepath.Join(t.TempDir(), "stale-descriptor.json")
		Expect(projectdaemon.WriteDescriptorAtomic(stalePath, &projectdaemon.Descriptor{
			SchemaVersion: 0,
			DataDir:       dataDir,
			RootDir:       rootDir,
		})).To(Succeed())
		_, _, err = readHealthyProjectDaemon(context.Background(), stalePath, projectdaemon.Config{DataDir: dataDir, RootDir: rootDir})
		Expect(err).To(MatchError(ContainSubstring("schema version")))

		Expect(dataLock.Release()).To(Succeed())
		Expect(rootLock.Release()).To(Succeed())

		Expect(projectDaemonDescriptorHealthy(context.Background(), nil)).To(BeFalse())
		Expect(processPIDAlive(-1)).To(BeFalse())
		Expect(processPIDAlive(os.Getpid())).To(BeTrue())
		Expect(workspacedPrivateMCPEndpointReachable(context.Background(), &projectdaemon.Descriptor{PrivateMCPURL: "http://[::1"})).To(BeFalse())
		Expect(projectDaemonDescriptorHealthy(context.Background(), &projectdaemon.Descriptor{
			SchemaVersion:   projectdaemon.DescriptorSchemaVersion,
			Role:            projectdaemon.RoleWorkspaced,
			PID:             -1,
			DataDir:         dataDir,
			RootDir:         rootDir,
			PrivateMCPURL:   "http://127.0.0.1:1/mcp",
			PrivateMCPToken: "token",
		})).To(BeFalse())

		heartbeatErr := make(chan error, 1)
		heartbeatErr <- nil
		Expect(waitForForegroundSession(context.Background(), heartbeatErr)).To(Succeed())
		heartbeatErr <- context.Canceled
		Expect(waitForForegroundSession(context.Background(), heartbeatErr)).To(Succeed())
		heartbeatErr <- errors.New("heartbeat failed")
		Expect(waitForForegroundSession(context.Background(), heartbeatErr)).To(MatchError(ContainSubstring("project daemon heartbeat failed")))

		canceled, cancel := context.WithCancel(context.Background())
		cancel()
		_, err = waitForProjectDaemon(canceled, filepath.Join(t.TempDir(), "missing.json"), "", projectdaemon.Config{DataDir: dataDir, RootDir: rootDir}, mcpTransports{})
		Expect(err).To(MatchError(context.Canceled))

		previousAcquireDataLock := acquireDataDirLockForRuntime
		previousAcquireRootLock := acquireRootDirLockForRuntime
		ginkgo.DeferCleanup(func() {
			acquireDataDirLockForRuntime = previousAcquireDataLock
			acquireRootDirLockForRuntime = previousAcquireRootLock
		})
		dataLock, err = locking.AcquireDataDirLock(dataDir)
		Expect(err).NotTo(HaveOccurred())
		acquireRootDirLockForRuntime = func(string) (leafwikiRuntimeLock, error) {
			return nil, errors.New("root acquire failed")
		}
		_, err = projectDaemonLocksHeld(dataDir, rootDir)
		Expect(err).To(HaveOccurred())
		Expect(dataLock.Release()).To(Succeed())

		acquireDataDirLockForRuntime = func(string) (leafwikiRuntimeLock, error) {
			return leafwikiFakeRuntimeLock{}, nil
		}
		acquireRootDirLockForRuntime = func(string) (leafwikiRuntimeLock, error) {
			return nil, errors.New("root acquire failed")
		}
		_, err = projectDaemonLocksFree(dataDir, rootDir)
		Expect(err).To(HaveOccurred())
		acquireRootDirLockForRuntime = func(string) (leafwikiRuntimeLock, error) {
			return leafwikiFakeRuntimeLock{releaseErr: errors.New("root release failed")}, nil
		}
		_, err = projectDaemonLocksFree(dataDir, rootDir)
		Expect(err).To(HaveOccurred())

		acquireDataDirLockForRuntime = func(string) (leafwikiRuntimeLock, error) {
			return nil, errors.New("data acquire failed")
		}
		_, err = projectDaemonDataLockFreeRootLockHeld(dataDir, rootDir)
		Expect(err).To(HaveOccurred())
		acquireDataDirLockForRuntime = func(string) (leafwikiRuntimeLock, error) {
			return leafwikiFakeRuntimeLock{releaseErr: errors.New("data release failed")}, nil
		}
		_, err = projectDaemonDataLockFreeRootLockHeld(dataDir, rootDir)
		Expect(err).To(HaveOccurred())
		acquireDataDirLockForRuntime = func(string) (leafwikiRuntimeLock, error) {
			return leafwikiFakeRuntimeLock{}, nil
		}
		acquireRootDirLockForRuntime = func(string) (leafwikiRuntimeLock, error) {
			return nil, errors.New("root acquire failed")
		}
		_, err = projectDaemonDataLockFreeRootLockHeld(dataDir, rootDir)
		Expect(err).To(HaveOccurred())
		acquireDataDirLockForRuntime = previousAcquireDataLock
		acquireRootDirLockForRuntime = previousAcquireRootLock

		lockedRoot, err := locking.AcquireRootDirLock(rootDir)
		Expect(err).NotTo(HaveOccurred())
		lockStartupPath := filepath.Join(t.TempDir(), "lock-startup.txt")
		Expect(os.WriteFile(lockStartupPath, []byte("acquire data directory lock: held"), 0o600)).To(Succeed())
		_, err = waitForProjectDaemon(context.Background(), filepath.Join(t.TempDir(), "missing-descriptor.json"), lockStartupPath, projectdaemon.Config{DataDir: dataDir, RootDir: rootDir}, mcpTransports{})
		Expect(err).To(MatchError(ContainSubstring("project is locked")))
		Expect(lockedRoot.Release()).To(Succeed())

		privateMCPServer := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
			rw.WriteHeader(http.StatusNoContent)
		}))
		ginkgo.DeferCleanup(privateMCPServer.Close)
		mismatchDescriptorPath := filepath.Join(t.TempDir(), "workspaced-descriptor.json")
		descriptorCfg := projectdaemon.Config{DataDir: dataDir, RootDir: rootDir, Host: "owner"}
		Expect(projectdaemon.WriteDescriptorAtomic(mismatchDescriptorPath, &projectdaemon.Descriptor{
			SchemaVersion:   projectdaemon.DescriptorSchemaVersion,
			Role:            projectdaemon.RoleWorkspaced,
			PID:             os.Getpid(),
			DataDir:         dataDir,
			RootDir:         rootDir,
			PrivateMCPURL:   privateMCPServer.URL,
			PrivateMCPToken: "token",
			Config:          descriptorCfg,
		})).To(Succeed())
		_, err = waitForProjectDaemon(context.Background(), mismatchDescriptorPath, "", projectdaemon.Config{DataDir: dataDir, RootDir: rootDir, Host: "requested"}, mcpTransports{})
		Expect(err).To(MatchError(ContainSubstring("project daemon config mismatch")))
	})

	ginkgo.It("covers direct runtime role process and startup helper branches", func() {
		t := ginkgo.GinkgoT()

		Expect((*wikidFrontdRuntime)(nil).stop(context.Background())).To(Succeed())
		Expect((*internalRuntimeRoleProcess)(nil).wait()).To(Succeed())
		Expect((*internalRuntimeRoleProcess)(nil).isDone()).To(BeTrue())
		Expect((*internalRuntimeRoleProcess)(nil).stop(context.Background())).To(Succeed())

		done := make(chan error, 1)
		proc := &internalRuntimeRoleProcess{done: done, waitDone: make(chan struct{})}
		doneErr := errors.New("role exited")
		done <- doneErr
		Expect(proc.wait()).To(MatchError(doneErr))
		Expect(proc.isDone()).To(BeTrue())
		process, err := os.FindProcess(os.Getpid())
		Expect(err).NotTo(HaveOccurred())
		proc.process = process
		Expect(proc.stop(context.Background())).To(MatchError(doneErr))

		for _, tc := range []struct {
			name     string
			lateDone bool
		}{
			{name: "returns joined context and process error", lateDone: true},
			{name: "returns the context error when wait never completes"},
		} {
			cmd := exec.Command("sleep", "10")
			Expect(cmd.Start()).To(Succeed(), tc.name)
			processDone := make(chan error)
			blockingProc := &internalRuntimeRoleProcess{
				role:     projectdaemon.RoleFrontd,
				pid:      cmd.Process.Pid,
				process:  cmd.Process,
				done:     processDone,
				waitDone: make(chan struct{}),
			}
			canceledStop, cancelStop := context.WithCancel(context.Background())
			cancelStop()
			if tc.lateDone {
				go func() {
					time.Sleep(10 * time.Millisecond)
					processDone <- errors.New("role exited after kill")
				}()
			}
			err = blockingProc.stop(canceledStop)
			Expect(err).To(HaveOccurred(), tc.name)
			if !tc.lateDone {
				processDone <- errors.New("late role exit")
			}
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
		}

		startupPath, err := writeInternalRuntimeRoleStartupConfig(internalRuntimeRoleStartupConfig{
			Role:        projectdaemon.RoleFrontd,
			DaemonToken: "token",
			Runtime:     leafwikiRuntimeConfig{Workspace: wiki.Workspace{DataDir: t.TempDir(), RootDir: t.TempDir()}},
		})
		Expect(err).NotTo(HaveOccurred())
		ginkgo.DeferCleanup(os.Remove, startupPath)
		Expect(startupPath).NotTo(BeEmpty())
		Expect(os.ReadFile(startupPath)).To(ContainSubstring(`"role":"frontd"`))

		Expect(writeInternalRuntimeRoleReady("", internalRuntimeRoleReady{Role: projectdaemon.RoleFrontd})).To(Succeed())
		readyPath := filepath.Join(t.TempDir(), "ready.json")
		Expect(writeInternalRuntimeRoleReady(readyPath, internalRuntimeRoleReady{Role: projectdaemon.RoleFrontd, PID: os.Getpid(), URL: "http://frontd.local"})).To(Succeed())
		readyProc, readyDone := newLeafwikiRuntimeRoleProcess(projectdaemon.RoleFrontd, os.Getpid())
		ready, err := waitForInternalRuntimeRoleReady(readyPath, readyProc, 100*time.Millisecond)
		Expect(err).NotTo(HaveOccurred())
		Expect(ready.Role).To(Equal(projectdaemon.RoleFrontd))
		releaseLeafwikiRuntimeRoleProcesses([]chan error{readyDone}, context.Canceled)

		badReadyPath := filepath.Join(t.TempDir(), "bad-ready.json")
		Expect(os.WriteFile(badReadyPath, []byte("{bad"), 0o600)).To(Succeed())
		badProc, badDone := newLeafwikiRuntimeRoleProcess(projectdaemon.RoleFrontd, os.Getpid())
		_, err = waitForInternalRuntimeRoleReady(badReadyPath, badProc, 100*time.Millisecond)
		Expect(err).To(HaveOccurred())
		releaseLeafwikiRuntimeRoleProcesses([]chan error{badDone}, context.Canceled)

		unsupportedPath := filepath.Join(t.TempDir(), "unsupported.json")
		Expect(os.WriteFile(unsupportedPath, []byte(`{"role":"unknown"}`), 0o600)).To(Succeed())
		Expect(runInternalRuntimeRole(context.Background(), unsupportedPath)).To(MatchError(ContainSubstring("unsupported runtime role")))
		badRuntimeStartup := filepath.Join(t.TempDir(), "bad-runtime.json")
		Expect(os.WriteFile(badRuntimeStartup, []byte("{bad"), 0o600)).To(Succeed())
		Expect(runInternalRuntimeRole(context.Background(), badRuntimeStartup)).To(MatchError(ContainSubstring("decode runtime role startup config")))
		wikidRuntimeStartup := filepath.Join(t.TempDir(), "wikid-runtime.json")
		Expect(os.WriteFile(wikidRuntimeStartup, []byte(`{"role":"wikid"}`), 0o600)).To(Succeed())
		wikidCanceled, cancelWikid := context.WithCancel(context.Background())
		cancelWikid()
		Expect(runInternalRuntimeRole(wikidCanceled, wikidRuntimeStartup)).To(Succeed())

		canceled, cancel := context.WithCancel(context.Background())
		cancel()
		Expect(runInternalRuntimeRole(canceled, string(projectdaemon.RoleWikid))).To(Succeed())
	})

	ginkgo.It("covers runtime role startup process failures through existing seams", func() {
		t := ginkgo.GinkgoT()
		blockingFile := filepath.Join(t.TempDir(), "not-a-dir")
		validTempDir := t.TempDir()
		missingExecutable := filepath.Join(t.TempDir(), "missing-leafwiki")
		Expect(os.WriteFile(blockingFile, []byte("x"), 0o600)).To(Succeed())

		t.Setenv("TMPDIR", blockingFile)
		_, _, err := startInternalRuntimeRoleProcess(internalRuntimeRoleStartupConfig{Role: projectdaemon.RoleFrontd})
		Expect(err).To(MatchError(ContainSubstring("create frontd ready file")))
		_, err = writeInternalRuntimeRoleStartupConfig(internalRuntimeRoleStartupConfig{Role: projectdaemon.RoleFrontd})
		Expect(err).To(MatchError(ContainSubstring("create frontd startup config")))

		t.Setenv("TMPDIR", validTempDir)
		previousExecutable := projectDaemonExecutable
		projectDaemonExecutable = func() (string, error) {
			return "", errors.New("executable unavailable")
		}
		ginkgo.DeferCleanup(func() {
			projectDaemonExecutable = previousExecutable
		})
		_, _, err = startInternalRuntimeRoleProcess(internalRuntimeRoleStartupConfig{Role: projectdaemon.RoleFrontd})
		Expect(err).To(MatchError("executable unavailable"))

		projectDaemonExecutable = previousExecutable
		previousReadinessTimeout := internalRuntimeRoleReadinessTimeoutForProcess
		internalRuntimeRoleReadinessTimeoutForProcess = 20 * time.Millisecond
		ginkgo.DeferCleanup(func() {
			internalRuntimeRoleReadinessTimeoutForProcess = previousReadinessTimeout
		})
		t.Setenv("GO_WANT_LEAFWIKI_HELPER_PROCESS", "1")
		_, _, err = startInternalRuntimeRoleProcess(internalRuntimeRoleStartupConfig{Role: projectdaemon.RoleWikid})
		Expect(err).To(MatchError("runtime role did not become ready before timeout"))

		projectDaemonExecutable = func() (string, error) {
			return missingExecutable, nil
		}
		_, _, err = startInternalRuntimeRoleProcess(internalRuntimeRoleStartupConfig{Role: projectdaemon.RoleFrontd})
		Expect(err).To(MatchError(ContainSubstring("start frontd role process")))
	})

	ginkgo.It("covers temp-file chmod, write, and close failure branches", func() {
		t := ginkgo.GinkgoT()
		previousCreateTemp := createTempFileForRuntime
		previousExecutable := projectDaemonExecutable
		ginkgo.DeferCleanup(func() {
			createTempFileForRuntime = previousCreateTemp
			projectDaemonExecutable = previousExecutable
		})

		for _, tc := range []struct {
			name string
			file *leafwikiFakeTempFile
		}{
			{name: "chmod", file: &leafwikiFakeTempFile{name: filepath.Join(t.TempDir(), "chmod.json"), chmodErr: errors.New("chmod failed")}},
			{name: "write", file: &leafwikiFakeTempFile{name: filepath.Join(t.TempDir(), "write.json"), writeErr: errors.New("write failed")}},
			{name: "close", file: &leafwikiFakeTempFile{name: filepath.Join(t.TempDir(), "close.json"), closeErr: errors.New("close failed")}},
		} {
			tempFile := tc.file
			createTempFileForRuntime = func(string, string) (leafwikiTempFile, error) {
				return tempFile, nil
			}
			_, err := writeInternalRuntimeRoleStartupConfig(internalRuntimeRoleStartupConfig{Role: projectdaemon.RoleFrontd})
			Expect(err).To(MatchError(ContainSubstring(tc.name + " failed")))
		}

		validCfg := leafwikiRuntimeConfig{Logging: leaflogging.Config{Target: leaflogging.TargetStderr}, DisableAuth: true}
		errTemp := &leafwikiFakeTempFile{name: filepath.Join(t.TempDir(), "daemon.err")}
		startupTemp := &leafwikiFakeTempFile{name: filepath.Join(t.TempDir(), "daemon.json"), writeErr: errors.New("daemon startup write failed")}
		createTempFileForRuntime = func(_ string, pattern string) (leafwikiTempFile, error) {
			if strings.Contains(pattern, "*.err") {
				return errTemp, nil
			}
			return startupTemp, nil
		}
		_, err := spawnProjectDaemonOwner(validCfg)
		Expect(err).To(MatchError("daemon startup write failed"))
		for _, tc := range []struct {
			name string
			file *leafwikiFakeTempFile
		}{
			{name: "daemon startup chmod", file: &leafwikiFakeTempFile{name: filepath.Join(t.TempDir(), "daemon-chmod.json"), chmodErr: errors.New("daemon startup chmod failed")}},
			{name: "daemon startup close", file: &leafwikiFakeTempFile{name: filepath.Join(t.TempDir(), "daemon-close.json"), closeErr: errors.New("daemon startup close failed")}},
		} {
			startupTemp := tc.file
			createTempFileForRuntime = func(_ string, pattern string) (leafwikiTempFile, error) {
				if strings.Contains(pattern, "*.err") {
					return errTemp, nil
				}
				return startupTemp, nil
			}
			_, err = spawnProjectDaemonOwner(validCfg)
			Expect(err).To(MatchError(ContainSubstring(tc.name + " failed")))
		}

		projectDaemonExecutable = func() (string, error) {
			return "", errors.New("executable should not be reached")
		}
		readyTemp := &leafwikiFakeTempFile{name: filepath.Join(t.TempDir(), "ready.json")}
		roleTemp := &leafwikiFakeTempFile{name: filepath.Join(t.TempDir(), "role.json"), closeErr: errors.New("role startup close failed")}
		createTempFileForRuntime = func(_ string, pattern string) (leafwikiTempFile, error) {
			if strings.Contains(pattern, "runtime-ready") {
				return readyTemp, nil
			}
			return roleTemp, nil
		}
		_, _, err = startInternalRuntimeRoleProcess(internalRuntimeRoleStartupConfig{Role: projectdaemon.RoleFrontd})
		Expect(err).To(MatchError("role startup close failed"))
	})

	ginkgo.It("covers runtime role readiness failure branches", func() {
		t := ginkgo.GinkgoT()

		exitedProc, exitedDone := newLeafwikiRuntimeRoleProcess(projectdaemon.RoleFrontd, os.Getpid())
		exitedDone <- nil
		_, err := waitForInternalRuntimeRoleReady(filepath.Join(t.TempDir(), "missing-ready.json"), exitedProc, time.Second)
		Expect(err).To(MatchError("process exited before readiness"))

		timeoutProc, timeoutDone := newLeafwikiRuntimeRoleProcess(projectdaemon.RoleFrontd, os.Getpid())
		_, err = waitForInternalRuntimeRoleReady(filepath.Join(t.TempDir(), "missing-ready.json"), timeoutProc, time.Millisecond)
		Expect(err).To(MatchError("runtime role did not become ready before timeout"))
		releaseLeafwikiRuntimeRoleProcesses([]chan error{timeoutDone}, context.Canceled)

		blockingFile := filepath.Join(t.TempDir(), "not-a-dir")
		Expect(os.WriteFile(blockingFile, []byte("x"), 0o600)).To(Succeed())
		readErrProc, readErrDone := newLeafwikiRuntimeRoleProcess(projectdaemon.RoleFrontd, os.Getpid())
		_, err = waitForInternalRuntimeRoleReady(filepath.Join(blockingFile, "ready.json"), readErrProc, time.Second)
		Expect(err).To(HaveOccurred())
		releaseLeafwikiRuntimeRoleProcesses([]chan error{readErrDone}, context.Canceled)

		blankPath := filepath.Join(t.TempDir(), "blank-ready.json")
		Expect(os.WriteFile(blankPath, []byte("  \n"), 0o600)).To(Succeed())
		blankProc, blankDone := newLeafwikiRuntimeRoleProcess(projectdaemon.RoleFrontd, os.Getpid())
		go func() {
			defer ginkgo.GinkgoRecover()
			time.Sleep(30 * time.Millisecond)
			Expect(os.WriteFile(blankPath, []byte(`{"role":"frontd","pid":0}`), 0o600)).To(Succeed())
		}()
		_, err = waitForInternalRuntimeRoleReady(blankPath, blankProc, time.Second)
		Expect(err).To(MatchError("runtime role reported invalid PID"))
		releaseLeafwikiRuntimeRoleProcesses([]chan error{blankDone}, context.Canceled)
	})

	ginkgo.It("covers frontd and workspaced role fast-failure branches", func() {
		t := ginkgo.GinkgoT()
		validRuntime := leafwikiRuntimeConfig{
			Workspace: wiki.Workspace{
				DataDir: filepath.Join(t.TempDir(), "data"),
				RootDir: filepath.Join(t.TempDir(), "root"),
			},
			Host:        "127.0.0.1",
			Port:        "0",
			DisableAuth: true,
			Logging:     leaflogging.Config{Target: leaflogging.TargetStderr},
		}
		Expect(os.MkdirAll(validRuntime.Workspace.DataDir, 0o755)).To(Succeed())
		Expect(os.MkdirAll(validRuntime.Workspace.RootDir, 0o755)).To(Succeed())

		cancelWhenParentExits(context.Background(), func() { ginkgo.Fail("parent pid zero should not cancel") }, 0, 0)
		parentCanceled, parentCancel := context.WithCancel(context.Background())
		parentCancel()
		cancelWhenParentExits(parentCanceled, func() { ginkgo.Fail("canceled context should stop parent polling") }, os.Getpid(), 0)
		Expect(serveInternalRuntimeHTTP(context.Background(), projectdaemon.RoleFrontd, leafwikiErrorListener{
			addr: leafwikiStringAddr("127.0.0.1:0"),
			err:  errors.New("accept failed"),
		}, http.NotFoundHandler(), 0)).To(MatchError(ContainSubstring("frontd server failed")))

		_, err := newRuntimeWiki(leafwikiRuntimeConfig{Workspace: wiki.Workspace{ID: "home"}}, projectdaemon.Config{DataDir: "bad\x00data", RootDir: validRuntime.Workspace.RootDir}, runtimeWikiFull)
		Expect(err).To(MatchError(ContainSubstring("initialize Wiki")))

		badPathRuntime := validRuntime
		badPathRuntime.Workspace.DataDir = "bad\x00data"
		Expect(runFrontdRole(context.Background(), internalRuntimeRoleStartupConfig{Role: projectdaemon.RoleFrontd, Runtime: badPathRuntime})).To(MatchError(ContainSubstring("resolve data dir")))
		Expect(runWorkspacedRole(context.Background(), internalRuntimeRoleStartupConfig{Role: projectdaemon.RoleWorkspaced, Runtime: badPathRuntime})).To(MatchError(ContainSubstring("resolve data dir")))

		badLogRuntime := validRuntime
		badLogRuntime.Logging = leaflogging.Config{Target: leaflogging.Target("not-a-target")}
		Expect(runFrontdRole(context.Background(), internalRuntimeRoleStartupConfig{Role: projectdaemon.RoleFrontd, Runtime: badLogRuntime})).To(MatchError(ContainSubstring("invalid logging configuration")))
		Expect(runWorkspacedRole(context.Background(), internalRuntimeRoleStartupConfig{Role: projectdaemon.RoleWorkspaced, Runtime: badLogRuntime})).To(MatchError(ContainSubstring("invalid logging configuration")))

		badProxyRuntime := validRuntime
		badProxyRuntime.TrustedProxyIPsRaw = "bad-cidr"
		_, err = routerOptionsForRuntimeWithUserService(badProxyRuntime, nil, "", false, "127.0.0.1")
		Expect(err).To(MatchError(ContainSubstring("invalid trusted proxies")))
		Expect(runFrontdRole(context.Background(), internalRuntimeRoleStartupConfig{Role: projectdaemon.RoleFrontd, Runtime: badProxyRuntime})).To(MatchError(ContainSubstring("invalid trusted proxies")))
		Expect(runWorkspacedRole(context.Background(), internalRuntimeRoleStartupConfig{Role: projectdaemon.RoleWorkspaced, Runtime: badProxyRuntime})).To(MatchError(ContainSubstring("invalid trusted proxies")))

		startup := internalRuntimeRoleStartupConfig{
			Role:          projectdaemon.RoleFrontd,
			Runtime:       validRuntime,
			DaemonToken:   "daemon-token",
			WikidURL:      "http://127.0.0.1:1",
			WorkspacedURL: "http://127.0.0.1:2",
			ReadyPath:     filepath.Join(t.TempDir(), "ready.json"),
			ParentPID:     os.Getpid(),
		}
		badWikid := startup
		badWikid.WikidURL = "http://[::1"
		Expect(runFrontdRole(context.Background(), badWikid)).To(MatchError(ContainSubstring("invalid wikid upstream")))

		badWorkspaced := startup
		badWorkspaced.WorkspacedURL = "http://[::1"
		Expect(runFrontdRole(context.Background(), badWorkspaced)).To(MatchError(ContainSubstring("invalid workspaced upstream")))

		badHost := startup
		badHost.Runtime.Host = "bad host"
		Expect(runFrontdRole(context.Background(), badHost)).To(MatchError(ContainSubstring("start frontd listener")))

		badReady := startup
		badReady.ReadyPath = filepath.Join(blockingPathForLeafwikiTest(t), "ready.json")
		Expect(runFrontdRole(context.Background(), badReady)).To(HaveOccurred())

		workspacedStartup := internalRuntimeRoleStartupConfig{
			Role:        projectdaemon.RoleWorkspaced,
			Runtime:     validRuntime,
			DaemonToken: "daemon-token",
			ReadyPath:   filepath.Join(t.TempDir(), "workspaced-ready.json"),
			ParentPID:   os.Getpid(),
		}
		badWorkspacedHost := workspacedStartup
		badWorkspacedHost.Runtime.Port = "not-a-port"
		Expect(runWorkspacedRole(context.Background(), badWorkspacedHost)).To(MatchError(ContainSubstring("start workspaced listener")))

		badWorkspacedReady := workspacedStartup
		badWorkspacedReady.ReadyPath = filepath.Join(blockingPathForLeafwikiTest(t), "ready.json")
		Expect(runWorkspacedRole(context.Background(), badWorkspacedReady)).To(HaveOccurred())

		successRuntime := validRuntime
		successRuntime.Workspace.DataDir = filepath.Join(t.TempDir(), "success-data")
		successRuntime.Workspace.RootDir = filepath.Join(t.TempDir(), "success-root")
		Expect(os.MkdirAll(successRuntime.Workspace.DataDir, 0o755)).To(Succeed())
		Expect(os.MkdirAll(successRuntime.Workspace.RootDir, 0o755)).To(Succeed())
		successStartup := workspacedStartup
		successStartup.Runtime = successRuntime
		successStartup.ReadyPath = filepath.Join(t.TempDir(), "workspaced-success-ready.json")
		successCtx, successCancel := context.WithCancel(context.Background())
		done := make(chan error, 1)
		go func() {
			done <- runWorkspacedRole(successCtx, successStartup)
		}()
		ready := waitForLeafwikiRuntimeReady(successStartup.ReadyPath)
		resp, err := http.Get(strings.TrimRight(ready.URL, "/") + "/mcp")
		Expect(err).NotTo(HaveOccurred())
		Expect(resp.StatusCode).To(Equal(http.StatusUnauthorized))
		Expect(resp.Body.Close()).To(Succeed())
		successCancel()
		Eventually(done).WithTimeout(3 * time.Second).Should(Receive(Succeed()))
	})

	ginkgo.It("covers dispatch and project daemon launcher seam branches", func() {
		previousRunDaemonService := runDaemonServiceForDispatch
		previousRunAgentHookCommand := runAgentHookCommandForDispatch
		previousRunProjectDaemonLauncher := runProjectDaemonLauncherForDispatch
		previousAttach := attachOrStartRuntimeDaemonForLaunch
		previousAgentHookAttach := attachOrStartRuntimeDaemonForAgentHook
		previousHeartbeat := runDaemonHeartbeatForLaunch
		previousBridge := runDaemonStdioBridgeForLaunch
		previousActorContext := daemonStdioActorContextForLaunch
		ginkgo.DeferCleanup(func() {
			runDaemonServiceForDispatch = previousRunDaemonService
			runAgentHookCommandForDispatch = previousRunAgentHookCommand
			runProjectDaemonLauncherForDispatch = previousRunProjectDaemonLauncher
			attachOrStartRuntimeDaemonForLaunch = previousAttach
			attachOrStartRuntimeDaemonForAgentHook = previousAgentHookAttach
			runDaemonHeartbeatForLaunch = previousHeartbeat
			runDaemonStdioBridgeForLaunch = previousBridge
			daemonStdioActorContextForLaunch = previousActorContext
		})

		runDaemonServiceForDispatch = func(context.Context, leafwikiRuntimeConfig) error {
			return errors.New("service failed")
		}
		expectLeafwikiExit(1, func() {
			dispatchRuntimeCommand(nil, true, false, leafwikiRuntimeConfig{})
		})

		var hookProvider string
		runAgentHookCommandForDispatch = func(_ context.Context, _ leafwikiRuntimeConfig, provider agenthooks.ProviderID, _ io.Reader, _ io.Writer) error {
			hookProvider = string(provider)
			return errors.New("hook failed open")
		}
		dispatchRuntimeCommand([]string{"agent-hook", "cursor"}, false, true, leafwikiRuntimeConfig{})
		Expect(hookProvider).To(Equal("cursor"))
		Expect(runAgentHookCommand(context.Background(), leafwikiRuntimeConfig{}, agenthooks.ProviderCodex, strings.NewReader(`{}`), leafwikiFailWriter{err: errors.New("stdout failed")})).To(MatchError(ContainSubstring("write hook allow response")))
		attachOrStartRuntimeDaemonForAgentHook = func(context.Context, leafwikiRuntimeConfig) (*projectdaemon.Descriptor, error) {
			return &projectdaemon.Descriptor{ControlURL: "http://127.0.0.1:1", ControlToken: "daemon-token"}, nil
		}
		Expect(runAgentHookCommand(context.Background(), leafwikiRuntimeConfig{}, agenthooks.ProviderCodex, strings.NewReader(`{"hook_event_name":"SessionStart","session_id":"session-a"}`), io.Discard)).To(MatchError(ContainSubstring("record agent presence")))
		attachOrStartRuntimeDaemonForAgentHook = previousAgentHookAttach

		runProjectDaemonLauncherForDispatch = func(context.Context, leafwikiRuntimeConfig) error {
			return errors.New("launcher failed")
		}
		expectLeafwikiExit(1, func() {
			dispatchRuntimeCommand(nil, false, false, leafwikiRuntimeConfig{})
		})

		sessions := projectdaemon.NewSessionRegistry(time.Minute, nil)
		controlServer := httptest.NewServer(projectdaemon.NewControlServer(projectdaemon.ControlServerOptions{
			Token:    "daemon-token",
			Sessions: sessions,
			VerifyAPIKey: func(key string) error {
				switch key {
				case "invalid":
					return projectdaemon.ErrInvalidAPIKey
				case "broken":
					return errors.New("verifier down")
				default:
					return nil
				}
			},
		}))
		ginkgo.DeferCleanup(controlServer.Close)
		descriptor := &projectdaemon.Descriptor{ControlURL: controlServer.URL, ControlToken: "daemon-token"}

		attachOrStartRuntimeDaemonForLaunch = func(ctx context.Context, _ leafwikiRuntimeConfig) (*projectdaemon.Descriptor, error) {
			return descriptor, nil
		}
		runDaemonHeartbeatForLaunch = func(context.Context, *projectdaemon.Client, projectdaemon.SessionID, time.Duration) error {
			return nil
		}
		Expect(runProjectDaemonLauncher(context.Background(), leafwikiRuntimeConfig{})).To(Succeed())

		canceledVerify, cancelVerify := context.WithCancel(context.Background())
		cancelVerify()
		Expect(runProjectDaemonLauncher(canceledVerify, leafwikiRuntimeConfig{MCPTransports: mcpTransports{Stdio: true}, APIKey: "valid"})).To(Succeed())
		canceledRegister, cancelRegister := context.WithCancel(context.Background())
		cancelRegister()
		Expect(runProjectDaemonLauncher(canceledRegister, leafwikiRuntimeConfig{})).To(Succeed())

		badTokenDescriptor := *descriptor
		badTokenDescriptor.ControlToken = "wrong-token"
		attachOrStartRuntimeDaemonForLaunch = func(ctx context.Context, _ leafwikiRuntimeConfig) (*projectdaemon.Descriptor, error) {
			return &badTokenDescriptor, nil
		}
		Expect(runProjectDaemonLauncher(context.Background(), leafwikiRuntimeConfig{})).To(MatchError(ContainSubstring("register project daemon session")))
		attachOrStartRuntimeDaemonForLaunch = func(ctx context.Context, _ leafwikiRuntimeConfig) (*projectdaemon.Descriptor, error) {
			return descriptor, nil
		}

		runDaemonHeartbeatForLaunch = func(context.Context, *projectdaemon.Client, projectdaemon.SessionID, time.Duration) error {
			return errors.New("heartbeat failed")
		}
		Expect(runProjectDaemonLauncher(context.Background(), leafwikiRuntimeConfig{})).To(MatchError(ContainSubstring("project daemon heartbeat failed")))

		attachOrStartRuntimeDaemonForLaunch = func(ctx context.Context, _ leafwikiRuntimeConfig) (*projectdaemon.Descriptor, error) {
			return nil, errors.New("attach failed")
		}
		Expect(runProjectDaemonLauncher(context.Background(), leafwikiRuntimeConfig{})).To(MatchError("attach failed"))
		canceled, cancel := context.WithCancel(context.Background())
		cancel()
		Expect(runProjectDaemonLauncher(canceled, leafwikiRuntimeConfig{})).To(Succeed())

		attachOrStartRuntimeDaemonForLaunch = func(ctx context.Context, _ leafwikiRuntimeConfig) (*projectdaemon.Descriptor, error) {
			return descriptor, nil
		}
		runDaemonHeartbeatForLaunch = func(ctx context.Context, _ *projectdaemon.Client, _ projectdaemon.SessionID, _ time.Duration) error {
			<-ctx.Done()
			return ctx.Err()
		}
		runDaemonStdioBridgeForLaunch = func(context.Context, daemonStdioBridge) error {
			return errors.New("bridge failed")
		}
		Expect(runProjectDaemonLauncher(context.Background(), leafwikiRuntimeConfig{DisableAuth: true, MCPTransports: mcpTransports{Stdio: true}})).To(MatchError("bridge failed"))

		runDaemonHeartbeatForLaunch = func(context.Context, *projectdaemon.Client, projectdaemon.SessionID, time.Duration) error {
			return errors.New("stdio heartbeat failed")
		}
		runDaemonStdioBridgeForLaunch = func(ctx context.Context, _ daemonStdioBridge) error {
			<-ctx.Done()
			return ctx.Err()
		}
		Expect(runProjectDaemonLauncher(context.Background(), leafwikiRuntimeConfig{DisableAuth: true, MCPTransports: mcpTransports{Stdio: true}})).To(MatchError(ContainSubstring("project daemon heartbeat failed")))

		descriptor.PrivateMCPURL = "http://private.local/mcp"
		descriptor.PrivateMCPToken = "private-token"
		daemonStdioActorContextForLaunch = func(context.Context, *projectdaemon.Descriptor, leafwikiRuntimeConfig) (string, error) {
			return "", errors.New("actor context failed")
		}
		Expect(runProjectDaemonLauncher(context.Background(), leafwikiRuntimeConfig{DisableAuth: true, MCPTransports: mcpTransports{Stdio: true}})).To(MatchError("actor context failed"))
		daemonStdioActorContextForLaunch = func(context.Context, *projectdaemon.Descriptor, leafwikiRuntimeConfig) (string, error) {
			return "", context.Canceled
		}
		Expect(runProjectDaemonLauncher(context.Background(), leafwikiRuntimeConfig{DisableAuth: true, MCPTransports: mcpTransports{Stdio: true}})).To(Succeed())
		descriptor.PrivateMCPURL = ""
		descriptor.PrivateMCPToken = ""
		daemonStdioActorContextForLaunch = previousActorContext

		Expect(runProjectDaemonLauncher(context.Background(), leafwikiRuntimeConfig{MCPTransports: mcpTransports{Stdio: true}, APIKey: "invalid"})).To(MatchError("invalid native STDIO API key"))
		Expect(runProjectDaemonLauncher(context.Background(), leafwikiRuntimeConfig{MCPTransports: mcpTransports{Stdio: true}, APIKey: "broken"})).To(MatchError(ContainSubstring("verify native STDIO API key")))

		runDaemonHeartbeatForLaunch = func(context.Context, *projectdaemon.Client, projectdaemon.SessionID, time.Duration) error {
			return nil
		}
		runDaemonStdioBridgeForLaunch = func(ctx context.Context, _ daemonStdioBridge) error {
			<-ctx.Done()
			return ctx.Err()
		}
		Expect(runProjectDaemonLauncher(context.Background(), leafwikiRuntimeConfig{DisableAuth: true, MCPTransports: mcpTransports{Stdio: true}})).To(Succeed())
	})

	ginkgo.It("covers SDK transport bridge connect and pump errors", func() {
		connectErr := errors.New("left connect failed")
		Expect(bridgeTransports(context.Background(), leafwikiFakeMCPTransport{err: connectErr}, leafwikiFakeMCPTransport{})).To(MatchError(connectErr))

		leftConn := newLeafwikiFakeMCPConnection()
		rightConnectErr := errors.New("right connect failed")
		Expect(bridgeTransports(context.Background(), leafwikiFakeMCPTransport{conn: leftConn}, leafwikiFakeMCPTransport{err: rightConnectErr})).To(MatchError(rightConnectErr))

		leftConn = newLeafwikiFakeMCPConnection()
		rightConn := newLeafwikiFakeMCPConnection()
		writeErr := errors.New("right write failed")
		rightConn.writeErr = writeErr
		leftConn.reads <- &sdkjsonrpc.Request{Method: "test/method"}
		Expect(bridgeTransports(context.Background(), leafwikiFakeMCPTransport{conn: leftConn}, leafwikiFakeMCPTransport{conn: rightConn})).To(MatchError(writeErr))

		leftConn = newLeafwikiFakeMCPConnection()
		rightConn = newLeafwikiFakeMCPConnection()
		leftConn.readErr = io.EOF
		leftConn.reads <- nil
		Expect(bridgeTransports(context.Background(), leafwikiFakeMCPTransport{conn: leftConn}, leafwikiFakeMCPTransport{conn: rightConn})).To(Succeed())

		leftConn = newLeafwikiFakeMCPConnection()
		rightConn = newLeafwikiFakeMCPConnection()
		leftConn.reads <- &sdkjsonrpc.Request{Method: "test/method"}
		rightConn.readErr = io.EOF
		go func() {
			<-rightConn.writes
			leftConn.readErr = io.EOF
			leftConn.reads <- nil
			time.Sleep(10 * time.Millisecond)
			rightConn.reads <- nil
		}()
		Expect(bridgeTransports(context.Background(), leafwikiFakeMCPTransport{conn: leftConn}, leafwikiFakeMCPTransport{conn: rightConn})).To(MatchError(io.EOF))

		now := time.Now().UTC()
		authServer := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
			Expect(req.Header.Get(projectdaemon.ControlTokenHeader)).To(Equal("daemon-token"))
			writeRuntimeJSON(rw, map[string]any{"actor": projectdaemon.ActorContext{
				Version:     1,
				Issuer:      projectdaemon.ActorContextIssuerWikid,
				Subject:     "user:stdio",
				Username:    "stdio",
				Role:        coreauth.RoleEditor,
				Scopes:      []string{"leafwiki:mcp"},
				WorkspaceID: "workspace-a",
				AuthMethod:  "api_key",
				IssuedAt:    now,
				ExpiresAt:   now.Add(time.Hour),
			}})
		}))
		ginkgo.DeferCleanup(authServer.Close)
		upstream := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
			Expect(req.Header.Get(projectdaemon.ActorContextHeader)).NotTo(BeEmpty())
			rw.WriteHeader(http.StatusNoContent)
		}))
		ginkgo.DeferCleanup(upstream.Close)
		rt := stdioActorContextRoundTripper{
			AuthControlURL:   authServer.URL,
			AuthControlToken: "daemon-token",
			WorkspaceID:      "workspace-a",
			APIKey:           "stdio-key",
		}
		resp, err := rt.RoundTrip(httptest.NewRequest(http.MethodGet, upstream.URL, nil))
		Expect(err).NotTo(HaveOccurred())
		Expect(resp.StatusCode).To(Equal(http.StatusNoContent))
		Expect(resp.Body.Close()).To(Succeed())

		previousEncodeActorContext := encodeActorContextForRuntime
		ginkgo.DeferCleanup(func() {
			encodeActorContextForRuntime = previousEncodeActorContext
		})
		encodeActorContextForRuntime = func(projectdaemon.ActorContext) (string, error) {
			return "", errors.New("round trip encode failed")
		}
		_, err = rt.actorContext(httptest.NewRequest(http.MethodGet, upstream.URL, nil))
		Expect(err).To(MatchError(ContainSubstring("encode native STDIO actor context")))
		encodeActorContextForRuntime = previousEncodeActorContext
	})

	ginkgo.It("covers project daemon owner and spawn failure branches", func() {
		t := ginkgo.GinkgoT()
		previousOwner := runWikidFrontdOwnerForProjectDaemon
		previousExecutable := projectDaemonExecutable
		ginkgo.DeferCleanup(func() {
			runWikidFrontdOwnerForProjectDaemon = previousOwner
			projectDaemonExecutable = previousExecutable
		})

		validCfg := leafwikiRuntimeConfig{
			Workspace: wiki.Workspace{
				DataDir: filepath.Join(t.TempDir(), "data"),
				RootDir: filepath.Join(t.TempDir(), "root"),
			},
			Logging:     leaflogging.Config{Target: leaflogging.TargetStderr},
			DisableAuth: true,
		}

		badPathCfg := validCfg
		badPathCfg.Workspace.DataDir = "bad\x00data"
		Expect(runProjectDaemonOwner(context.Background(), badPathCfg)).To(MatchError(ContainSubstring("resolve data dir")))

		Expect(os.MkdirAll(validCfg.Workspace.DataDir, 0o755)).To(Succeed())
		Expect(os.MkdirAll(validCfg.Workspace.RootDir, 0o755)).To(Succeed())
		dataLock, err := locking.AcquireDataDirLock(validCfg.Workspace.DataDir)
		Expect(err).NotTo(HaveOccurred())
		Expect(runProjectDaemonOwner(context.Background(), validCfg)).To(MatchError(ContainSubstring("acquire data directory lock")))
		Expect(dataLock.Release()).To(Succeed())

		rootLock, err := locking.AcquireRootDirLock(validCfg.Workspace.RootDir)
		Expect(err).NotTo(HaveOccurred())
		Expect(runProjectDaemonOwner(context.Background(), validCfg)).To(MatchError(ContainSubstring("acquire root directory lock")))
		Expect(rootLock.Release()).To(Succeed())

		badLogCfg := validCfg
		badLogCfg.Logging = leaflogging.Config{Target: leaflogging.Target("bad-target")}
		Expect(runProjectDaemonOwner(context.Background(), badLogCfg)).To(MatchError(ContainSubstring("invalid logging configuration")))

		legacyDBDir := filepath.Join(validCfg.Workspace.DataDir, "users.db")
		Expect(os.MkdirAll(legacyDBDir, 0o755)).To(Succeed())
		Expect(os.WriteFile(filepath.Join(legacyDBDir, "child"), []byte("x"), 0o600)).To(Succeed())
		Expect(runProjectDaemonOwner(context.Background(), validCfg)).To(MatchError(ContainSubstring("cleanup legacy auth DBs")))
		Expect(os.Remove(filepath.Join(legacyDBDir, "child"))).To(Succeed())
		Expect(os.Remove(legacyDBDir)).To(Succeed())

		authDirFailureCfg := validCfg
		authDirFailureCfg.Workspace.DataDir = filepath.Join(t.TempDir(), "auth-dir-data")
		authDirFailureCfg.Workspace.RootDir = filepath.Join(t.TempDir(), "auth-dir-root")
		Expect(os.MkdirAll(authDirFailureCfg.Workspace.DataDir, 0o755)).To(Succeed())
		Expect(os.MkdirAll(authDirFailureCfg.Workspace.RootDir, 0o755)).To(Succeed())
		Expect(os.MkdirAll(filepath.Join(authDirFailureCfg.Workspace.DataDir, ".leafwiki"), 0o755)).To(Succeed())
		Expect(os.WriteFile(filepath.Join(authDirFailureCfg.Workspace.DataDir, ".leafwiki", "wikid"), []byte("not a directory"), 0o600)).To(Succeed())
		Expect(runProjectDaemonOwner(context.Background(), authDirFailureCfg)).To(MatchError(ContainSubstring("create wikid auth dir")))

		oauthDirFailureCfg := validCfg
		oauthDirFailureCfg.Workspace.DataDir = filepath.Join(t.TempDir(), "oauth-dir-data")
		oauthDirFailureCfg.Workspace.RootDir = filepath.Join(t.TempDir(), "oauth-dir-root")
		Expect(os.MkdirAll(oauthDirFailureCfg.Workspace.DataDir, 0o755)).To(Succeed())
		Expect(os.MkdirAll(oauthDirFailureCfg.Workspace.RootDir, 0o755)).To(Succeed())
		oauthPaths := wikid.AuthStoragePaths(oauthDirFailureCfg.Workspace.DataDir)
		Expect(os.MkdirAll(oauthPaths.AuthDir, 0o755)).To(Succeed())
		Expect(os.WriteFile(oauthPaths.OAuthDir, []byte("not a directory"), 0o600)).To(Succeed())
		Expect(runProjectDaemonOwner(context.Background(), oauthDirFailureCfg)).To(MatchError(ContainSubstring("create wikid oauth dir")))

		runWikidFrontdOwnerForProjectDaemon = func(context.Context, leafwikiRuntimeConfig, projectdaemon.Config) error {
			return errors.New("wikid-frontd failed")
		}
		Expect(runProjectDaemonOwner(context.Background(), validCfg)).To(MatchError("wikid-frontd failed"))

		runWikidFrontdOwnerForProjectDaemon = func(context.Context, leafwikiRuntimeConfig, projectdaemon.Config) error {
			return nil
		}
		missingRootCfg := validCfg
		missingRootCfg.Workspace.DataDir = filepath.Join(t.TempDir(), "missing-root-data")
		missingRootCfg.Workspace.RootDir = filepath.Join(t.TempDir(), "missing-root")
		Expect(runProjectDaemonOwner(context.Background(), missingRootCfg)).To(Succeed())
		Expect(missingRootCfg.Workspace.RootDir).To(BeADirectory())

		validTempDir := t.TempDir()
		missingExecutable := filepath.Join(t.TempDir(), "missing-leafwiki")
		blockingFile := blockingPathForLeafwikiTest(t)
		t.Setenv("TMPDIR", blockingFile)
		_, err = spawnProjectDaemonOwner(validCfg)
		Expect(err).To(MatchError(ContainSubstring("create daemon startup error file")))

		t.Setenv("TMPDIR", validTempDir)
		projectDaemonExecutable = func() (string, error) {
			return "", errors.New("executable unavailable")
		}
		_, err = spawnProjectDaemonOwner(validCfg)
		Expect(err).To(MatchError("executable unavailable"))

		projectDaemonExecutable = func() (string, error) {
			return missingExecutable, nil
		}
		_, err = spawnProjectDaemonOwner(validCfg)
		Expect(err).To(MatchError(ContainSubstring("start project daemon")))

		scheduleProjectDaemonStartupConfigCleanup("")
	})

	ginkgo.It("covers wikid-frontd owner dependency failure seams", func() {
		t := ginkgo.GinkgoT()
		cfg := leafwikiRuntimeConfig{
			Workspace: wiki.Workspace{
				ID:      wikid.HomeWorkspaceID,
				DataDir: filepath.Join(t.TempDir(), "data"),
				RootDir: filepath.Join(t.TempDir(), "root"),
			},
			Host:         "127.0.0.1",
			Port:         "0",
			DisableAuth:  true,
			Logging:      leaflogging.Config{Target: leaflogging.TargetStderr},
			RuntimeStack: projectdaemon.RuntimeStackWikidFrontd,
		}
		ownerCfg := projectdaemon.Config{
			RuntimeStack: projectdaemon.RuntimeStackWikidFrontd,
			DataDir:      cfg.Workspace.DataDir,
			RootDir:      cfg.Workspace.RootDir,
			Host:         cfg.Host,
			Port:         cfg.Port,
		}
		Expect(os.MkdirAll(ownerCfg.DataDir, 0o755)).To(Succeed())
		Expect(os.MkdirAll(ownerCfg.RootDir, 0o755)).To(Succeed())

		previousNetListen := netListenForRuntime
		previousRandomToken := randomTokenForRuntime
		previousConfigHash := configHashForRuntime
		previousNewRuntimeWiki := newRuntimeWikiForRuntime
		previousControlPlaneOptions := controlPlaneRouterOptionsForOwner
		previousStartRuntime := startWikidFrontdRuntimeForOwner
		previousMCPProxy := newMCPProxyWithActorForRuntime
		previousWriteDescriptor := writeDescriptorAtomicForRuntime
		ginkgo.DeferCleanup(func() {
			netListenForRuntime = previousNetListen
			randomTokenForRuntime = previousRandomToken
			configHashForRuntime = previousConfigHash
			newRuntimeWikiForRuntime = previousNewRuntimeWiki
			controlPlaneRouterOptionsForOwner = previousControlPlaneOptions
			startWikidFrontdRuntimeForOwner = previousStartRuntime
			newMCPProxyWithActorForRuntime = previousMCPProxy
			writeDescriptorAtomicForRuntime = previousWriteDescriptor
		})

		netListenForRuntime = func(string, string) (net.Listener, error) {
			return nil, errors.New("listen failed")
		}
		Expect(runWikidFrontdOwner(context.Background(), cfg, ownerCfg)).To(MatchError(ContainSubstring("start control listener")))
		netListenForRuntime = previousNetListen

		randomTokenForRuntime = func() (string, error) {
			return "", errors.New("token failed")
		}
		Expect(runWikidFrontdOwner(context.Background(), cfg, ownerCfg)).To(MatchError("token failed"))
		randomTokenForRuntime = previousRandomToken

		configHashForRuntime = func(projectdaemon.Config) (string, error) {
			return "", errors.New("hash failed")
		}
		Expect(runWikidFrontdOwner(context.Background(), cfg, ownerCfg)).To(MatchError("hash failed"))
		configHashForRuntime = previousConfigHash

		newRuntimeWikiForRuntime = func(leafwikiRuntimeConfig, projectdaemon.Config, runtimeWikiMode) (*wiki.Wiki, error) {
			return nil, errors.New("wiki failed")
		}
		Expect(runWikidFrontdOwner(context.Background(), cfg, ownerCfg)).To(MatchError("wiki failed"))
		newRuntimeWikiForRuntime = func(leafwikiRuntimeConfig, projectdaemon.Config, runtimeWikiMode) (*wiki.Wiki, error) {
			w := newFrontdActorTestWiki(t)
			ginkgo.DeferCleanup(w.Close)
			return w, nil
		}

		controlPlaneRouterOptionsForOwner = func(leafwikiRuntimeConfig, *wiki.Wiki) (httpinternal.RouterOptions, error) {
			return httpinternal.RouterOptions{}, errors.New("router options failed")
		}
		Expect(runWikidFrontdOwner(context.Background(), cfg, ownerCfg)).To(MatchError("router options failed"))
		controlPlaneRouterOptionsForOwner = previousControlPlaneOptions

		startWikidFrontdRuntimeForOwner = func(context.Context, leafwikiRuntimeConfig, string, string) (*wikidFrontdRuntime, error) {
			return nil, errors.New("runtime failed")
		}
		Expect(runWikidFrontdOwner(context.Background(), cfg, ownerCfg)).To(MatchError("runtime failed"))

		startWikidFrontdRuntimeForOwner = func(ctx context.Context, _ leafwikiRuntimeConfig, _ string, _ string) (*wikidFrontdRuntime, error) {
			return newLeafwikiReadyOwnerRuntime(ctx), nil
		}
		newMCPProxyWithActorForRuntime = func(frontd.WorkspaceProxyOptions) (http.Handler, error) {
			return nil, errors.New("private mcp failed")
		}
		Expect(runWikidFrontdOwner(context.Background(), cfg, ownerCfg)).To(MatchError("private mcp failed"))
		newMCPProxyWithActorForRuntime = previousMCPProxy

		writeDescriptorAtomicForRuntime = func(string, *projectdaemon.Descriptor) error {
			return errors.New("write descriptor failed")
		}
		Expect(runWikidFrontdOwner(context.Background(), cfg, ownerCfg)).To(MatchError("write descriptor failed"))

		writeCalls := 0
		writeDescriptorAtomicForRuntime = func(path string, desc *projectdaemon.Descriptor) error {
			writeCalls++
			if writeCalls == 2 {
				return errors.New("write global descriptor failed")
			}
			return previousWriteDescriptor(path, desc)
		}
		Expect(runWikidFrontdOwner(context.Background(), cfg, ownerCfg)).To(MatchError("write global descriptor failed"))
	})

	ginkgo.It("covers direct actor, token, and private endpoint error branches", func() {
		t := ginkgo.GinkgoT()
		w := newFrontdActorTestWiki(t)
		ginkgo.DeferCleanup(w.Close)

		actor, err := wikidControlMCPActorResolver("", leafwikiRuntimeConfig{DisableAuth: true})(httptest.NewRequest(http.MethodPost, "/mcp", nil))
		Expect(err).NotTo(HaveOccurred())
		Expect(actor.Subject).To(Equal("user:public-editor"))

		_, err = wikidControlMCPActorResolver(t.TempDir(), leafwikiRuntimeConfig{})(httptest.NewRequest(http.MethodPost, "/mcp", nil))
		Expect(err).To(MatchError("native STDIO requires an API key"))
		missingKeyReq := httptest.NewRequest(http.MethodPost, "/mcp", nil)
		missingKeyReq.Header.Set("Authorization", "Bearer lwk_key_missing")
		_, err = wikidControlMCPActorResolver(t.TempDir(), leafwikiRuntimeConfig{})(missingKeyReq)
		Expect(err).To(HaveOccurred())

		userStoreFailureDir := t.TempDir()
		Expect(os.Mkdir(filepath.Join(userStoreFailureDir, "users.db"), 0o755)).To(Succeed())
		_, err = stdioAPIKeyUserFromStorage(userStoreFailureDir, "lwk_key_missing")
		Expect(err).To(HaveOccurred())
		apiKeyStoreFailureDir := t.TempDir()
		Expect(os.Mkdir(filepath.Join(apiKeyStoreFailureDir, "api_keys.db"), 0o755)).To(Succeed())
		_, err = stdioAPIKeyUserFromStorage(apiKeyStoreFailureDir, "lwk_key_missing")
		Expect(err).To(HaveOccurred())
		_, err = stdioAPIKeyUserFromStorage(t.TempDir(), "lwk_key_missing")
		Expect(err).To(HaveOccurred())

		_, err = frontdMCPTokenVerifier(&wiki.Wiki{})(context.Background(), "lwk_key_missing", httptest.NewRequest(http.MethodPost, "/mcp", nil))
		Expect(err).To(MatchError(ContainSubstring("api key verifier unavailable")))

		noCodeErr := newWikidPrivateEndpointError("/plain", http.StatusBadGateway, nil)
		Expect(noCodeErr.Code).To(BeEmpty())
		Expect(noCodeErr.Error()).To(ContainSubstring("status 502"))

		err = callWikidPrivateEndpoint(context.Background(), "http://[::1", "token", "/private", nil, nil)
		Expect(err).To(HaveOccurred())
		Expect(cloneWithOriginalRequest(nil)).To(BeNil())

		blockingFile := filepath.Join(t.TempDir(), "not-a-dir")
		Expect(os.WriteFile(blockingFile, []byte("x"), 0o600)).To(Succeed())
		badRegistryLayout := wikid.GlobalLayout(t.TempDir())
		badRegistry := wikid.NewRegistryService(wikid.NewRegistryStore(filepath.Join(blockingFile, "registry.db")), badRegistryLayout)
		rec := httptest.NewRecorder()
		handleWikidActorContext(rec, httptest.NewRequest(http.MethodPost, "/__leafwiki/actor-context", nil), w, leafwikiRuntimeConfig{DisableAuth: true, Workspace: wiki.Workspace{ID: "home"}}, badRegistry, nil)
		Expect(rec.Code).To(Equal(http.StatusInternalServerError))

		badGrantStore := wikid.NewGrantStore(filepath.Join(blockingFile, "grants.db"))
		rec = httptest.NewRecorder()
		handleWikidActorContext(rec, httptest.NewRequest(http.MethodPost, "/__leafwiki/actor-context", nil), w, leafwikiRuntimeConfig{DisableAuth: true, Workspace: wiki.Workspace{ID: "home"}}, nil, badGrantStore)
		Expect(rec.Code).To(Equal(http.StatusInternalServerError))
		previousGrantsForSubject := grantsForSubjectForRuntime
		previousActorContextForGrant := actorContextForWorkspaceGrantForRuntime
		ginkgo.DeferCleanup(func() {
			grantsForSubjectForRuntime = previousGrantsForSubject
			actorContextForWorkspaceGrantForRuntime = previousActorContextForGrant
		})
		grantsForSubjectForRuntime = func(*wikid.GrantStore, string) ([]wikid.Grant, error) {
			return nil, errors.New("grant lookup failed")
		}
		validGrantStore := wikid.NewGrantStore(filepath.Join(t.TempDir(), "grants.db"))
		rec = httptest.NewRecorder()
		handleWikidActorContext(rec, httptest.NewRequest(http.MethodPost, "/__leafwiki/actor-context", nil), w, leafwikiRuntimeConfig{DisableAuth: true, Workspace: wiki.Workspace{ID: "home"}}, nil, validGrantStore)
		Expect(rec.Code).To(Equal(http.StatusInternalServerError))
		grantsForSubjectForRuntime = previousGrantsForSubject

		actorContextForWorkspaceGrantForRuntime = func(*coreauth.User, string, leafwikiRuntimeConfig, workspaceid.WorkspaceID, wikid.GrantRole) (projectdaemon.ActorContext, error) {
			return projectdaemon.ActorContext{}, errors.New("actor context failed")
		}
		rec = httptest.NewRecorder()
		handleWikidActorContext(rec, httptest.NewRequest(http.MethodPost, "/__leafwiki/actor-context", nil), w, leafwikiRuntimeConfig{DisableAuth: true, Workspace: wiki.Workspace{ID: "home"}}, nil, nil)
		Expect(rec.Code).To(Equal(http.StatusInternalServerError))
		actorContextForWorkspaceGrantForRuntime = previousActorContextForGrant

		Expect(seedRuntimeHomeGrants(badGrantStore, leafwikiRuntimeConfig{DisableAuth: true})).To(MatchError(ContainSubstring("seed disabled-auth home grant")))
		Expect(seedRuntimeHomeGrants(badGrantStore, leafwikiRuntimeConfig{PublicAccess: true})).To(MatchError(ContainSubstring("seed public home grant")))
	})

	ginkgo.It("covers direct manager, storage, and environment helper branches", func() {
		t := ginkgo.GinkgoT()

		var manager *federatedWorkspaceManager
		manager.MarkReady("workspace-a", 1, "http://workspace.local")
		_, err := manager.Ensure(context.Background(), wikid.WorkspaceRecord{ID: "workspace-a"})
		Expect(err).To(MatchError("workspace manager is unavailable"))

		manager = newFederatedWorkspaceManager(leafwikiRuntimeConfig{}, "daemon-token", "http://wikid.local", wikid.GlobalLayout(t.TempDir()), wikid.NewWorkspaceSupervisor(wikid.WorkspaceSupervisorOptions{}))
		_, err = manager.Ensure(context.Background(), wikid.WorkspaceRecord{})
		Expect(err).To(MatchError("workspace ID is required"))

		workspace := wikid.WorkspaceRecord{ID: "workspace-a", DataDir: t.TempDir(), RootDir: t.TempDir()}
		manager.supervisor.MarkReady(workspace.ID, os.Getpid(), "http://workspace.local")
		status, err := manager.ensureWorkspace(workspace.ID, workspace)
		Expect(err).NotTo(HaveOccurred())
		Expect(status.State).To(Equal(wikid.WorkspaceStateRunning))

		manager.removeDescriptor = func(string) error {
			return errors.New("remove failed")
		}
		manager.removeDescriptors([]string{"descriptor.json"})
		Expect((*federatedWorkspaceManager)(nil).stop(context.Background())).To(Succeed())
		manager.stopped = true
		manager.restartWorkspaceAfter(wikid.WorkspaceRecord{ID: "workspace-a"}, time.Now().Add(-time.Second))

		stoppedProcDone := make(chan error, 1)
		stoppedProcDone <- errors.New("process stop failed")
		stoppedProc := &internalRuntimeRoleProcess{
			role:     projectdaemon.RoleWorkspaced,
			done:     stoppedProcDone,
			waitDone: make(chan struct{}),
		}
		Expect(stoppedProc.wait()).To(MatchError("process stop failed"))
		process, err := os.FindProcess(os.Getpid())
		Expect(err).NotTo(HaveOccurred())
		stoppedProc.process = process
		manager = newFederatedWorkspaceManager(leafwikiRuntimeConfig{}, "daemon-token", "http://wikid.local", wikid.GlobalLayout(t.TempDir()), wikid.NewWorkspaceSupervisor(wikid.WorkspaceSupervisorOptions{}))
		manager.processes["workspace-stop"] = stoppedProc
		Expect(manager.stop(context.Background())).To(MatchError("process stop failed"))

		blockingFile := blockingPathForLeafwikiTest(t)
		Expect(removeNonRegularDescriptor(filepath.Join(blockingFile, "descriptor.json"))).To(MatchError(ContainSubstring("inspect descriptor target")))
		_, err = stdioAPIKeyUserFromStorage(filepath.Join(blockingFile, "auth"), "lwk_key_missing")
		Expect(err).To(HaveOccurred())
		_, err = daemonConfigForRuntime(leafwikiRuntimeConfig{Workspace: wiki.Workspace{DataDir: "bad\x00data", RootDir: t.TempDir()}})
		Expect(err).To(MatchError(ContainSubstring("resolve data dir")))
		_, err = daemonWorkspaceRequestConfigForRuntime(leafwikiRuntimeConfig{Workspace: wiki.Workspace{DataDir: "bad\x00data", RootDir: t.TempDir()}})
		Expect(err).To(MatchError(ContainSubstring("resolve data dir")))
		oldHome, hadHome := os.LookupEnv("HOME")
		Expect(os.Setenv("HOME", "")).To(Succeed())
		_, err = globalRuntimeHomeDir()
		Expect(err).To(HaveOccurred())
		_, err = globalRuntimeWorkspace()
		Expect(err).To(HaveOccurred())
		_, err = daemonOwnerRuntimeConfig(leafwikiRuntimeConfig{})
		Expect(err).To(HaveOccurred())
		_, err = daemonRequestConfigForRuntime(leafwikiRuntimeConfig{})
		Expect(err).To(HaveOccurred())
		if hadHome {
			Expect(os.Setenv("HOME", oldHome)).To(Succeed())
		} else {
			Expect(os.Unsetenv("HOME")).To(Succeed())
		}

		opts := frontendConfigForRuntimeStorage(filepath.Join(t.TempDir(), "missing"))
		Expect(opts.GetSiteName()).To(Equal("LeafWiki"))
		Expect(opts.GetFaviconFile()).To(BeEmpty())
		blockedFrontendOpts := frontendConfigForRuntimeStorage(filepath.Join(blockingFile, "frontend"))
		Expect(blockedFrontendOpts.GetSiteName()).To(BeEmpty())
		Expect(blockedFrontendOpts.GetFaviconFile()).To(BeEmpty())

		Expect(processAlive(-1)).To(BeFalse())
		Expect(processAlive(os.Getpid())).To(BeTrue())
		Expect(publicURLForListener("127.0.0.1", leafwikiFakeListener{addr: leafwikiStringAddr("listener-without-port")}, "/base")).To(Equal("http://listener-without-port/base"))
		Expect(setSysProcAttrBool(nil, "Setpgid", true)).To(BeFalse())
		Expect(setSysProcAttrBool(&syscall.SysProcAttr{}, "MissingField", true)).To(BeFalse())
		Expect(setSysProcAttrBool(&syscall.SysProcAttr{}, "Pdeathsig", true)).To(BeFalse())

		w := newFrontdActorTestWiki(t)
		ginkgo.DeferCleanup(w.Close)
		_, err = frontdMCPTokenVerifier(w)(context.Background(), "lwk_key_invalid", httptest.NewRequest(http.MethodPost, "/mcp", nil))
		Expect(err).To(MatchError(ContainSubstring("invalid api key")))

		_, _, err = frontdActorUser(httptest.NewRequest(http.MethodPost, "/mcp", nil), w, leafwikiRuntimeConfig{})
		Expect(err).To(MatchError(ContainSubstring("missing credentials")))
	})

	ginkgo.It("covers portable system-error seams for runtime helpers", func() {
		t := ginkgo.GinkgoT()
		previousAbs := filepathAbsForRuntime
		previousRel := filepathRelForRuntime
		previousHome := userHomeDirForRuntime
		previousOpenNull := openDaemonNullDeviceForRuntime
		previousMarshal := jsonMarshalForRuntime
		previousResolveLogging := resolveLoggingForRuntime
		previousCleanupDelay := projectDaemonStartupConfigPostStartCleanupDelay
		ginkgo.DeferCleanup(func() {
			filepathAbsForRuntime = previousAbs
			filepathRelForRuntime = previousRel
			userHomeDirForRuntime = previousHome
			openDaemonNullDeviceForRuntime = previousOpenNull
			jsonMarshalForRuntime = previousMarshal
			resolveLoggingForRuntime = previousResolveLogging
			projectDaemonStartupConfigPostStartCleanupDelay = previousCleanupDelay
		})

		filepathAbsForRuntime = func(string) (string, error) {
			return "", errors.New("abs failed")
		}
		logCfg := leafwikiRuntimeConfig{Logging: leaflogging.Config{Target: leaflogging.TargetFile, FilePath: "leafwiki.log"}}
		Expect(daemonLogFileForConfig(logCfg, t.TempDir())).To(Equal("leafwiki.log"))
		filepathAbsForRuntime = previousAbs

		filepathRelForRuntime = func(string, string) (string, error) {
			return "", errors.New("rel failed")
		}
		_, ok := localRelativePath(t.TempDir(), filepath.Join(t.TempDir(), "leafwiki.log"))
		Expect(ok).To(BeFalse())
		filepathRelForRuntime = previousRel

		userHomeDirForRuntime = func() (string, error) {
			return "", nil
		}
		_, err := globalRuntimeHomeDir()
		Expect(err).To(MatchError("user home is empty"))
		userHomeDirForRuntime = func() (string, error) {
			return "", errors.New("home failed")
		}
		_, err = globalRuntimeHomeDir()
		Expect(err).To(MatchError(ContainSubstring("resolve user home")))
		userHomeDirForRuntime = previousHome

		openDaemonNullDeviceForRuntime = func() (*os.File, error) {
			return nil, errors.New("open null failed")
		}
		_, _, err = openDaemonNullDevice()
		Expect(err).To(MatchError(ContainSubstring("open daemon null device")))
		_, err = configureDaemonOwnerIO(&exec.Cmd{}, leafwikiRuntimeConfig{MCPTransports: mcpTransports{Stdio: true}})
		Expect(err).To(MatchError(ContainSubstring("open daemon null device")))
		_, err = configureInternalRuntimeRoleIO(&exec.Cmd{}, leafwikiRuntimeConfig{MCPTransports: mcpTransports{Stdio: true}})
		Expect(err).To(MatchError(ContainSubstring("open daemon null device")))
		_, _, err = startInternalRuntimeRoleProcess(internalRuntimeRoleStartupConfig{Role: projectdaemon.RoleFrontd, Runtime: leafwikiRuntimeConfig{MCPTransports: mcpTransports{Stdio: true}}})
		Expect(err).To(MatchError(ContainSubstring("open daemon null device")))
		_, err = spawnProjectDaemonOwner(leafwikiRuntimeConfig{
			Workspace:     wiki.Workspace{DataDir: t.TempDir(), RootDir: t.TempDir()},
			DisableAuth:   true,
			MCPTransports: mcpTransports{Stdio: true},
			Logging:       leaflogging.Config{Target: leaflogging.TargetStderr},
		})
		Expect(err).To(MatchError(ContainSubstring("open daemon null device")))
		openDaemonNullDeviceForRuntime = previousOpenNull

		jsonMarshalForRuntime = func(any) ([]byte, error) {
			return nil, errors.New("marshal failed")
		}
		startupErrPath := filepath.Join(t.TempDir(), "startup.err")
		writeProjectDaemonStartupError(startupErrPath, errors.New("plain startup error"))
		Expect(os.ReadFile(startupErrPath)).To(Equal([]byte("plain startup error")))
		_, err = spawnProjectDaemonOwner(leafwikiRuntimeConfig{DisableAuth: true, Logging: leaflogging.Config{Target: leaflogging.TargetStderr}})
		Expect(err).To(MatchError("marshal failed"))
		_, err = writeInternalRuntimeRoleStartupConfig(internalRuntimeRoleStartupConfig{Role: projectdaemon.RoleFrontd})
		Expect(err).To(MatchError("marshal failed"))
		err = writeInternalRuntimeRoleReady(filepath.Join(t.TempDir(), "ready.json"), internalRuntimeRoleReady{Role: projectdaemon.RoleFrontd})
		Expect(err).To(MatchError("marshal failed"))
		jsonMarshalForRuntime = previousMarshal

		projectDaemonStartupConfigPostStartCleanupDelay = 0
		scheduleProjectDaemonStartupConfigCleanup(filepath.Join(t.TempDir(), "startup.json"))

		resolveLoggingForRuntime = func(leaflogging.ConfigInput) (leaflogging.Config, error) {
			return leaflogging.Config{}, errors.New("logging resolve failed")
		}
		badWorkspaceCfg := leafwikiRuntimeConfig{
			Workspace:     wiki.Workspace{DataDir: t.TempDir(), RootDir: t.TempDir()},
			MCPTransports: mcpTransports{Stdio: true},
			Logging:       leaflogging.Config{Target: leaflogging.TargetStderr},
		}
		_, err = daemonWorkspaceRuntimeConfig(badWorkspaceCfg)
		Expect(err).To(MatchError("logging resolve failed"))
		_, err = daemonWorkspaceRequestConfigForRuntime(badWorkspaceCfg)
		Expect(err).To(MatchError("logging resolve failed"))
		_, err = daemonOwnerRuntimeConfig(badWorkspaceCfg)
		Expect(err).To(MatchError("logging resolve failed"))
		_, err = spawnProjectDaemonOwner(badWorkspaceCfg)
		Expect(err).To(MatchError("logging resolve failed"))
		resolveLoggingForRuntime = previousResolveLogging
	})

	ginkgo.It("covers process, wait, bridge, and role dependency seam failures", func() {
		t := ginkgo.GinkgoT()
		previousWaitTimeout := projectDaemonWaitTimeout
		previousFindProcess := processFindProcessForRuntime
		previousStartCommand := startCommandForRuntime
		previousReleaseProcess := releaseProcessForRuntime
		previousCreateTempFile := createTempFileForRuntime
		previousBridgeTransports := bridgeTransportsForRuntime
		previousDefaultStdin := defaultDaemonStdinForRuntime
		previousDefaultStdout := defaultDaemonStdoutForRuntime
		previousNotifySignals := notifyRuntimeSignalsForRuntime
		previousStopSignals := stopRuntimeSignalsForRuntime
		previousShutdownInternalHTTP := shutdownInternalRuntimeHTTPServerForRuntime
		previousNewRuntimeWiki := newRuntimeWikiForRuntime
		previousWorkspacesAPI := newWorkspacesAPIForRuntime
		previousWorkspaceResolver := newWikidWorkspaceResolverForRuntime
		previousPublicMCP := frontdPublicMCPHandlerForRuntime
		previousSingleResolver := newWikidSingleWorkspaceResolverForRuntime
		previousAcquireDataLock := acquireDataDirLockForRuntime
		previousAcquireRootLock := acquireRootDirLockForRuntime
		previousStatPath := statPathForRuntime
		previousMkdirAll := mkdirAllForRuntime
		previousOwner := runWikidFrontdOwnerForProjectDaemon
		ginkgo.DeferCleanup(func() {
			projectDaemonWaitTimeout = previousWaitTimeout
			processFindProcessForRuntime = previousFindProcess
			startCommandForRuntime = previousStartCommand
			releaseProcessForRuntime = previousReleaseProcess
			createTempFileForRuntime = previousCreateTempFile
			bridgeTransportsForRuntime = previousBridgeTransports
			defaultDaemonStdinForRuntime = previousDefaultStdin
			defaultDaemonStdoutForRuntime = previousDefaultStdout
			notifyRuntimeSignalsForRuntime = previousNotifySignals
			stopRuntimeSignalsForRuntime = previousStopSignals
			shutdownInternalRuntimeHTTPServerForRuntime = previousShutdownInternalHTTP
			newRuntimeWikiForRuntime = previousNewRuntimeWiki
			newWorkspacesAPIForRuntime = previousWorkspacesAPI
			newWikidWorkspaceResolverForRuntime = previousWorkspaceResolver
			frontdPublicMCPHandlerForRuntime = previousPublicMCP
			newWikidSingleWorkspaceResolverForRuntime = previousSingleResolver
			acquireDataDirLockForRuntime = previousAcquireDataLock
			acquireRootDirLockForRuntime = previousAcquireRootLock
			statPathForRuntime = previousStatPath
			mkdirAllForRuntime = previousMkdirAll
			runWikidFrontdOwnerForProjectDaemon = previousOwner
		})

		Expect(defaultDaemonStdin()).To(Equal(os.Stdin))
		Expect(defaultDaemonStdout()).To(Equal(os.Stdout))

		processFindProcessForRuntime = func(int) (*os.Process, error) {
			return nil, errors.New("find process failed")
		}
		Expect(processPIDAlive(12345)).To(BeFalse())
		processFindProcessForRuntime = previousFindProcess

		canceledHeartbeat, cancelHeartbeat := context.WithCancel(context.Background())
		cancelHeartbeat()
		Expect(runDaemonHeartbeat(canceledHeartbeat, nil, "", 0)).To(MatchError(context.Canceled))

		projectDaemonWaitTimeout = time.Millisecond
		waitErrPath := filepath.Join(t.TempDir(), "startup.err")
		Expect(os.WriteFile(waitErrPath, []byte("acquire data directory lock: held"), 0o600)).To(Succeed())
		acquireDataDirLockForRuntime = func(string) (leafwikiRuntimeLock, error) {
			return nil, errors.New("data lock probe failed")
		}
		_, err := waitForProjectDaemon(context.Background(), filepath.Join(t.TempDir(), "missing.json"), waitErrPath, projectdaemon.Config{DataDir: t.TempDir(), RootDir: t.TempDir()}, mcpTransports{})
		Expect(err).To(MatchError(ContainSubstring("project is locked but no attachable daemon was found")))
		acquireDataDirLockForRuntime = previousAcquireDataLock

		waitLockPath := filepath.Join(t.TempDir(), "startup-lock.err")
		Expect(os.WriteFile(waitLockPath, []byte("acquire data directory lock: held"), 0o600)).To(Succeed())
		_, err = waitForProjectDaemon(context.Background(), filepath.Join(t.TempDir(), "missing.json"), waitLockPath, projectdaemon.Config{DataDir: t.TempDir(), RootDir: t.TempDir()}, mcpTransports{})
		Expect(err).To(MatchError(ContainSubstring("project is locked")))

		acquireDataDirLockForRuntime = func(string) (leafwikiRuntimeLock, error) {
			return nil, errors.New("descriptor lock probe failed")
		}
		badDescriptorPath := filepath.Join(t.TempDir(), "bad-descriptor.json")
		Expect(os.WriteFile(badDescriptorPath, []byte("{bad"), 0o600)).To(Succeed())
		_, err = waitForProjectDaemon(context.Background(), badDescriptorPath, "", projectdaemon.Config{DataDir: t.TempDir(), RootDir: t.TempDir()}, mcpTransports{})
		Expect(err).To(MatchError(ContainSubstring("descriptor lock probe failed")))
		acquireDataDirLockForRuntime = previousAcquireDataLock

		startCommandForRuntime = func(*exec.Cmd) error {
			return nil
		}
		_, _, err = startInternalRuntimeRoleProcess(internalRuntimeRoleStartupConfig{Role: projectdaemon.RoleFrontd})
		Expect(err).To(MatchError(ContainSubstring("missing process handle")))

		createCalls := 0
		createTempFileForRuntime = func(dir string, pattern string) (leafwikiTempFile, error) {
			createCalls++
			if createCalls == 1 {
				file, err := os.CreateTemp(dir, pattern)
				if err != nil {
					return nil, err
				}
				return file, nil
			}
			return nil, errors.New("startup config temp failed")
		}
		_, err = spawnProjectDaemonOwner(leafwikiRuntimeConfig{DisableAuth: true, Logging: leaflogging.Config{Target: leaflogging.TargetStderr}})
		Expect(err).To(MatchError(ContainSubstring("create daemon startup config")))
		createTempFileForRuntime = previousCreateTempFile

		releaseProcessForRuntime = func(*os.Process) error {
			return errors.New("release failed")
		}
		startCommandForRuntime = func(cmd *exec.Cmd) error {
			cmd.Process = &os.Process{Pid: os.Getpid()}
			return nil
		}
		_, err = spawnProjectDaemonOwner(leafwikiRuntimeConfig{DisableAuth: true, Logging: leaflogging.Config{Target: leaflogging.TargetStderr}})
		Expect(err).To(MatchError(ContainSubstring("release project daemon process")))
		startCommandForRuntime = previousStartCommand
		releaseProcessForRuntime = previousReleaseProcess

		stopDone := make(chan error, 1)
		stopDone <- errors.New("role stop failed")
		stopProc := &internalRuntimeRoleProcess{done: stopDone, waitDone: make(chan struct{})}
		Expect(stopProc.wait()).To(MatchError("role stop failed"))
		process, err := os.FindProcess(os.Getpid())
		Expect(err).NotTo(HaveOccurred())
		stopProc.process = process
		runtime := &wikidFrontdRuntime{processes: map[projectdaemon.RoleName]*internalRuntimeRoleProcess{projectdaemon.RoleFrontd: stopProc}}
		Expect(runtime.stop(context.Background())).To(MatchError("role stop failed"))

		defaultInput := &leafwikiErrReadCloser{err: io.EOF}
		defaultDaemonStdinForRuntime = func() io.ReadCloser { return defaultInput }
		defaultDaemonStdoutForRuntime = func() io.Writer { return io.Discard }
		bridgeTransportsForRuntime = func(context.Context, sdkmcp.Transport, sdkmcp.Transport) error {
			return errors.New("bridge failed")
		}
		Expect(runDaemonStdioBridge(context.Background(), daemonStdioBridge{EndpointURL: "http://127.0.0.1:1/mcp"})).To(MatchError(ContainSubstring("MCP STDIO failed")))
		Eventually(defaultInput.Closed).WithTimeout(200 * time.Millisecond).Should(BeTrue())

		filterInput := &leafwikiErrReadCloser{err: errors.New("stdin failed")}
		bridgeTransportsForRuntime = func(context.Context, sdkmcp.Transport, sdkmcp.Transport) error {
			time.Sleep(20 * time.Millisecond)
			return nil
		}
		Expect(runDaemonStdioBridge(context.Background(), daemonStdioBridge{EndpointURL: "http://127.0.0.1:1/mcp", Stdin: filterInput, Stdout: io.Discard})).To(MatchError(ContainSubstring("MCP STDIO input failed")))
		bridgeTransportsForRuntime = previousBridgeTransports
		defaultDaemonStdinForRuntime = previousDefaultStdin
		defaultDaemonStdoutForRuntime = previousDefaultStdout

		notifyRuntimeSignalsForRuntime = func(c chan<- os.Signal, sig ...os.Signal) {
			c <- os.Interrupt
		}
		stopRuntimeSignalsForRuntime = func(chan<- os.Signal) {}
		Expect(waitForInternalRuntimeRoleSignal(context.Background())).To(Succeed())
		notifyRuntimeSignalsForRuntime = previousNotifySignals
		stopRuntimeSignalsForRuntime = previousStopSignals

		shutdownInternalRuntimeHTTPServerForRuntime = func(*http.Server, context.Context) error {
			return errors.New("shutdown failed")
		}
		Expect(serveInternalRuntimeHTTP(context.Background(), projectdaemon.RoleFrontd, leafwikiFakeListener{addr: leafwikiStringAddr("127.0.0.1:0")}, http.NotFoundHandler(), 0)).To(MatchError("shutdown failed"))
		shutdownInternalRuntimeHTTPServerForRuntime = previousShutdownInternalHTTP

		validRuntime := leafwikiRuntimeConfig{
			Workspace: wiki.Workspace{DataDir: t.TempDir(), RootDir: t.TempDir()},
			Host:      "127.0.0.1",
			Port:      "0",
			Logging:   leaflogging.Config{Target: leaflogging.TargetStderr},
		}
		startup := internalRuntimeRoleStartupConfig{
			Role:          projectdaemon.RoleFrontd,
			Runtime:       validRuntime,
			DaemonToken:   "daemon-token",
			WikidURL:      "http://127.0.0.1:1",
			WorkspacedURL: "http://127.0.0.1:2",
			ReadyPath:     filepath.Join(t.TempDir(), "ready.json"),
		}
		newWorkspacesAPIForRuntime = func(string, string) (http.Handler, error) {
			return nil, errors.New("workspaces api failed")
		}
		Expect(runFrontdRole(context.Background(), startup)).To(MatchError("workspaces api failed"))
		newWorkspacesAPIForRuntime = previousWorkspacesAPI

		newWikidWorkspaceResolverForRuntime = func(string, string) (func(*http.Request, workspaceid.WorkspaceID) (frontd.WorkspaceRoute, error), error) {
			return nil, errors.New("workspace resolver failed")
		}
		Expect(runFrontdRole(context.Background(), startup)).To(MatchError("workspace resolver failed"))
		newWikidWorkspaceResolverForRuntime = previousWorkspaceResolver

		httpStartup := startup
		httpStartup.Runtime.MCPTransports = mcpTransports{HTTP: true}
		frontdPublicMCPHandlerForRuntime = func(leafwikiRuntimeConfig, string, string, string) (http.Handler, error) {
			return nil, errors.New("public mcp failed")
		}
		Expect(runFrontdRole(context.Background(), httpStartup)).To(MatchError("public mcp failed"))
		frontdPublicMCPHandlerForRuntime = previousPublicMCP

		newWikidSingleWorkspaceResolverForRuntime = func(string, string) (func(*http.Request) (workspaceid.WorkspaceID, error), error) {
			return nil, errors.New("single resolver failed")
		}
		Expect(runFrontdRole(context.Background(), httpStartup)).To(MatchError("single resolver failed"))
		newWikidSingleWorkspaceResolverForRuntime = previousSingleResolver

		newRuntimeWikiForRuntime = func(leafwikiRuntimeConfig, projectdaemon.Config, runtimeWikiMode) (*wiki.Wiki, error) {
			return nil, errors.New("runtime wiki failed")
		}
		Expect(runWorkspacedRole(context.Background(), internalRuntimeRoleStartupConfig{Role: projectdaemon.RoleWorkspaced, Runtime: validRuntime})).To(MatchError("runtime wiki failed"))
		newRuntimeWikiForRuntime = previousNewRuntimeWiki

		ownerCfg := validRuntime
		ownerCfg.DisableAuth = true
		ownerDaemonCfg, err := daemonConfigForRuntime(ownerCfg)
		Expect(err).NotTo(HaveOccurred())
		runWikidFrontdOwnerForProjectDaemon = func(context.Context, leafwikiRuntimeConfig, projectdaemon.Config) error { return nil }
		acquireDataDirLockForRuntime = func(string) (leafwikiRuntimeLock, error) { return leafwikiFakeRuntimeLock{}, nil }
		acquireRootDirLockForRuntime = func(string) (leafwikiRuntimeLock, error) { return leafwikiFakeRuntimeLock{}, nil }
		statPathForRuntime = func(path string) (os.FileInfo, error) {
			if path == ownerDaemonCfg.DataDir {
				return nil, os.ErrNotExist
			}
			return nil, nil
		}
		mkdirAllForRuntime = func(string, os.FileMode) error { return errors.New("mkdir data failed") }
		Expect(runProjectDaemonOwner(context.Background(), ownerCfg)).To(MatchError(ContainSubstring("create data directory")))

		statPathForRuntime = func(path string) (os.FileInfo, error) {
			if path == ownerDaemonCfg.RootDir {
				return nil, os.ErrNotExist
			}
			return nil, nil
		}
		mkdirAllForRuntime = func(string, os.FileMode) error { return errors.New("mkdir root failed") }
		Expect(runProjectDaemonOwner(context.Background(), ownerCfg)).To(MatchError(ContainSubstring("create root directory")))
	})

	ginkgo.It("covers extracted runtime callback and auth helper branches", func() {
		t := ginkgo.GinkgoT()

		status, err := federatedEnsureResultStatus("workspace-a", wikid.WorkspaceStatus{State: wikid.WorkspaceStateRunning}, nil)
		Expect(err).NotTo(HaveOccurred())
		Expect(status.State).To(Equal(wikid.WorkspaceStateRunning))
		_, err = federatedEnsureResultStatus("workspace-a", "unexpected", nil)
		Expect(err).To(MatchError(ContainSubstring(`ensure workspace "workspace-a" returned unexpected result string`)))
		resultErr := errors.New("ensure failed")
		_, err = federatedEnsureResultStatus("workspace-a", "unexpected", resultErr)
		Expect(err).To(MatchError(resultErr))

		var routed []string
		mux := frontdWorkspaceMux(
			http.HandlerFunc(func(http.ResponseWriter, *http.Request) { routed = append(routed, "workspace-router") }),
			http.HandlerFunc(func(http.ResponseWriter, *http.Request) { routed = append(routed, "workspace-proxy") }),
		)
		mux.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, frontd.PublicWorkspacesPrefix+"/workspace-a/status", nil))
		mux.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/pages", nil))
		Expect(routed).To(Equal([]string{"workspace-router", "workspace-proxy"}))

		mcpRouted := []string{}
		mcpMux := frontdMCPMux(
			http.HandlerFunc(func(http.ResponseWriter, *http.Request) { mcpRouted = append(mcpRouted, "base") }),
			http.HandlerFunc(func(http.ResponseWriter, *http.Request) { mcpRouted = append(mcpRouted, "workspace") }),
		)
		workspaceMCPReq := httptest.NewRequest(http.MethodPost, "/mcp/workspaces/workspace-a", nil)
		workspaceMCPReq.RemoteAddr = "127.0.0.1:1234"
		mcpMux.ServeHTTP(httptest.NewRecorder(), workspaceMCPReq)
		baseMCPReq := httptest.NewRequest(http.MethodPost, "/other-mcp", nil)
		baseMCPReq.RemoteAddr = "127.0.0.1:1234"
		mcpMux.ServeHTTP(httptest.NewRecorder(), baseMCPReq)
		Expect(mcpRouted).To(Equal([]string{"workspace", "base"}))

		previousMCPProxy := newMCPProxyWithActorForRuntime
		previousVerifyAPIKey := verifyFrontdAPIKeyForRuntime
		previousVerifyOAuth := verifyFrontdOAuthBearerTokenForRuntime
		previousGetUser := getFrontdUserByIDForRuntime
		previousEnsureHomeGrant := ensureRuntimeHomeGrantForOwner
		previousWriteDescriptor := writeDescriptorAtomicForRuntime
		previousRegisteredWorkspace := registeredFederatedWorkspaceForAttach
		ginkgo.DeferCleanup(func() {
			newMCPProxyWithActorForRuntime = previousMCPProxy
			verifyFrontdAPIKeyForRuntime = previousVerifyAPIKey
			verifyFrontdOAuthBearerTokenForRuntime = previousVerifyOAuth
			getFrontdUserByIDForRuntime = previousGetUser
			ensureRuntimeHomeGrantForOwner = previousEnsureHomeGrant
			writeDescriptorAtomicForRuntime = previousWriteDescriptor
			registeredFederatedWorkspaceForAttach = previousRegisteredWorkspace
		})

		newMCPProxyWithActorForRuntime = func(frontd.WorkspaceProxyOptions) (http.Handler, error) {
			return nil, errors.New("mcp proxy failed")
		}
		rec := httptest.NewRecorder()
		frontdWorkspaceMCPProxy(frontd.WorkspaceRoute{
			WorkspaceID: "workspace-a",
			Upstream:    "http://127.0.0.1:1",
			DaemonToken: "daemon-token",
		}, func(*http.Request) (projectdaemon.ActorContext, error) {
			return projectdaemon.ActorContext{}, nil
		}).ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/mcp", nil))
		Expect(rec.Code).To(Equal(http.StatusServiceUnavailable))

		newMCPProxyWithActorForRuntime = func(opts frontd.WorkspaceProxyOptions) (http.Handler, error) {
			return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				actor, actorErr := opts.Actor(req)
				Expect(actorErr).NotTo(HaveOccurred())
				Expect(actor.WorkspaceID).To(Equal(workspaceid.WorkspaceID("workspace-a")))
				w.WriteHeader(http.StatusNoContent)
			}), nil
		}
		rec = httptest.NewRecorder()
		frontdWorkspaceMCPProxy(frontd.WorkspaceRoute{
			WorkspaceID: "workspace-a",
			Upstream:    "http://127.0.0.1:1",
			DaemonToken: "daemon-token",
		}, func(req *http.Request) (projectdaemon.ActorContext, error) {
			workspaceID, parseErr := workspaceid.ParseWorkspaceID(req.Header.Get(projectdaemon.WorkspaceIDHeader))
			Expect(parseErr).NotTo(HaveOccurred())
			return projectdaemon.ActorContext{WorkspaceID: workspaceID}, nil
		}).ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/mcp", nil))
		Expect(rec.Code).To(Equal(http.StatusNoContent))
		newMCPProxyWithActorForRuntime = previousMCPProxy

		w := newFrontdActorTestWiki(t)
		ginkgo.DeferCleanup(w.Close)
		verifyFrontdAPIKeyForRuntime = func(*wiki.Wiki, string) (*coreauth.APIKeyVerification, error) {
			return nil, errors.New("api key backend failed")
		}
		_, err = frontdMCPTokenVerifier(w)(context.Background(), "lwk_key_backend", httptest.NewRequest(http.MethodPost, "/mcp", nil))
		Expect(err).To(MatchError(ContainSubstring("api key verifier failed")))
		verifyFrontdAPIKeyForRuntime = previousVerifyAPIKey

		verifyFrontdOAuthBearerTokenForRuntime = func(*wiki.Wiki, context.Context, string, *http.Request) (*sdkauth.TokenInfo, error) {
			return &sdkauth.TokenInfo{UserID: "missing-user"}, nil
		}
		getFrontdUserByIDForRuntime = func(*wiki.Wiki, coreauth.UserID) (*coreauth.User, error) {
			return nil, errors.New("oauth user lookup failed")
		}
		oauthReq := httptest.NewRequest(http.MethodPost, "/mcp", nil)
		oauthReq.Header.Set("Authorization", "Bearer oauth-token")
		_, _, err = frontdActorUser(oauthReq, w, leafwikiRuntimeConfig{})
		Expect(err).To(MatchError("oauth user lookup failed"))
		getFrontdUserByIDForRuntime = previousGetUser
		verifyFrontdOAuthBearerTokenForRuntime = previousVerifyOAuth

		_, err = runtimeWorkspaceSubject(httptest.NewRequest(http.MethodPost, "/__leafwiki/workspaces/workspace-a/ensure", nil), w, leafwikiRuntimeConfig{}, nil)
		Expect(err).To(MatchError(ContainSubstring("missing credentials")))
		ensureRuntimeHomeGrantForOwner = func(*wikid.GrantStore, *coreauth.User) error {
			return errors.New("home grant failed")
		}
		_, err = runtimeWorkspaceSubject(httptest.NewRequest(http.MethodPost, "/__leafwiki/workspaces/home/ensure", nil), w, leafwikiRuntimeConfig{DisableAuth: true, Workspace: wiki.Workspace{ID: "home"}}, nil)
		Expect(err).To(MatchError("home grant failed"))
		ensureRuntimeHomeGrantForOwner = previousEnsureHomeGrant
		ensureRuntimeHomeGrantForOwner = func(*wikid.GrantStore, *coreauth.User) error {
			return nil
		}
		subject, err := runtimeWorkspaceSubject(httptest.NewRequest(http.MethodPost, "/__leafwiki/workspaces/home/ensure", nil), w, leafwikiRuntimeConfig{DisableAuth: true, Workspace: wiki.Workspace{ID: "home"}}, nil)
		Expect(err).NotTo(HaveOccurred())
		Expect(subject.Subject).To(Equal("user:public-editor"))
		ensureRuntimeHomeGrantForOwner = previousEnsureHomeGrant

		rec = httptest.NewRecorder()
		runtimeTokenVerifyHandler(w).ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/__leafwiki/token/verify", nil))
		Expect(rec.Code).To(Equal(http.StatusUnauthorized))
		err = verifyOwnerControlAPIKey(projectdaemon.Config{DataDir: t.TempDir()}, "lwk_key_missing")
		Expect(errors.Is(err, projectdaemon.ErrInvalidAPIKey)).To(BeTrue())

		writeDescriptorAtomicForRuntime = func(string, *projectdaemon.Descriptor) error {
			return errors.New("descriptor write failed")
		}
		desc := &projectdaemon.Descriptor{}
		supervisor := wikid.NewWorkspaceSupervisor(wikid.WorkspaceSupervisorOptions{})
		updateRuntimeRoleDescriptors(&sync.Mutex{}, desc, supervisor, "descriptor.json", "global.json", []projectdaemon.RoleHealth{{
			Name:  projectdaemon.RoleFrontd,
			URL:   "http://127.0.0.1:4321",
			State: projectdaemon.RoleStateReady,
		}})
		Expect(desc.PublicURL).To(Equal("http://127.0.0.1:4321"))
		writeDescriptorAtomicForRuntime = previousWriteDescriptor

		registeredFederatedWorkspaceForAttach = func(wikid.Layout, projectdaemon.Config) (wikid.WorkspaceRecord, bool, error) {
			return wikid.WorkspaceRecord{}, false, errors.New("registered workspace failed")
		}
		cfg := leafwikiRuntimeConfig{
			Workspace:     wiki.Workspace{DataDir: t.TempDir(), RootDir: t.TempDir()},
			MCPTransports: mcpTransports{Stdio: true},
		}
		requestCfg, err := daemonWorkspaceRequestConfigForRuntime(cfg)
		Expect(err).NotTo(HaveOccurred())
		_, err = attachOrStartFederatedProjectDaemon(context.Background(), cfg, requestCfg, filepath.Join(t.TempDir(), "descriptor.json"))
		Expect(err).To(MatchError("registered workspace failed"))
	})

	ginkgo.It("covers wikid owner server and shutdown seam branches", func() {
		t := ginkgo.GinkgoT()
		previousNewRuntimeWiki := newRuntimeWikiForRuntime
		previousStartRuntime := startWikidFrontdRuntimeForOwner
		previousMCPProxy := newMCPProxyWithActorForRuntime
		previousServeControl := serveWikidControlServerForOwner
		previousShutdownControl := shutdownWikidControlServerForRuntime
		previousSeedHomeGrants := seedRuntimeHomeGrantsForOwner
		previousWorkspaceManager := newFederatedWorkspaceManagerForOwner
		previousWriteDescriptor := writeDescriptorAtomicForRuntime
		ginkgo.DeferCleanup(func() {
			newRuntimeWikiForRuntime = previousNewRuntimeWiki
			startWikidFrontdRuntimeForOwner = previousStartRuntime
			newMCPProxyWithActorForRuntime = previousMCPProxy
			serveWikidControlServerForOwner = previousServeControl
			shutdownWikidControlServerForRuntime = previousShutdownControl
			seedRuntimeHomeGrantsForOwner = previousSeedHomeGrants
			newFederatedWorkspaceManagerForOwner = previousWorkspaceManager
			writeDescriptorAtomicForRuntime = previousWriteDescriptor
		})

		baseCfg := leafwikiRuntimeConfig{
			Workspace:           wiki.Workspace{ID: "home", DataDir: t.TempDir(), RootDir: t.TempDir()},
			Host:                "127.0.0.1",
			Port:                "0",
			DisableAuth:         true,
			DisableIdleShutdown: true,
			DaemonIdleTimeout:   time.Minute,
			Logging:             leaflogging.Config{Target: leaflogging.TargetStderr},
		}
		ownerCfg, err := daemonConfigForRuntime(baseCfg)
		Expect(err).NotTo(HaveOccurred())
		newRuntimeWikiForRuntime = func(leafwikiRuntimeConfig, projectdaemon.Config, runtimeWikiMode) (*wiki.Wiki, error) {
			return newFrontdActorTestWiki(t), nil
		}
		newMCPProxyWithActorForRuntime = func(frontd.WorkspaceProxyOptions) (http.Handler, error) {
			return http.NotFoundHandler(), nil
		}
		fakeRuntime := func(stopErr error) *wikidFrontdRuntime {
			runtimeCtx, runtimeCancel := context.WithCancel(context.Background())
			supervisor := wikid.NewSupervisor(wikid.SupervisorOptions{})
			supervisor.MarkReady(projectdaemon.RoleWikid, os.Getpid(), "", false)
			supervisor.MarkReady(projectdaemon.RoleWorkspaced, 123, "http://127.0.0.1:65535", true)
			processes := map[projectdaemon.RoleName]*internalRuntimeRoleProcess{}
			if stopErr != nil {
				process, findErr := os.FindProcess(os.Getpid())
				Expect(findErr).NotTo(HaveOccurred())
				done := make(chan error, 1)
				done <- stopErr
				proc := &internalRuntimeRoleProcess{
					role:     projectdaemon.RoleFrontd,
					process:  process,
					done:     done,
					waitDone: make(chan struct{}),
				}
				Expect(proc.wait()).To(MatchError(stopErr))
				processes[projectdaemon.RoleFrontd] = proc
			}
			return &wikidFrontdRuntime{
				ctx:           runtimeCtx,
				cancel:        runtimeCancel,
				supervisor:    supervisor,
				processes:     processes,
				roles:         []projectdaemon.RoleHealth{{Name: projectdaemon.RoleWorkspaced, State: projectdaemon.RoleStateReady, PID: 123, URL: "http://127.0.0.1:65535", Private: true}},
				workspacedURL: "http://127.0.0.1:65535",
			}
		}

		startWikidFrontdRuntimeForOwner = func(context.Context, leafwikiRuntimeConfig, string, string) (*wikidFrontdRuntime, error) {
			return fakeRuntime(errors.New("runtime stop failed")), nil
		}
		newFederatedWorkspaceManagerForOwner = func(base leafwikiRuntimeConfig, daemonToken string, wikidURL string, layout wikid.Layout, supervisor *wikid.WorkspaceSupervisor) *federatedWorkspaceManager {
			manager := newFederatedWorkspaceManager(base, daemonToken, wikidURL, layout, supervisor)
			process, findErr := os.FindProcess(os.Getpid())
			Expect(findErr).NotTo(HaveOccurred())
			done := make(chan error, 1)
			done <- errors.New("workspace stop failed")
			proc := &internalRuntimeRoleProcess{
				role:     projectdaemon.RoleWorkspaced,
				process:  process,
				done:     done,
				waitDone: make(chan struct{}),
			}
			Expect(proc.wait()).To(MatchError("workspace stop failed"))
			manager.processes["workspace-stop"] = proc
			return manager
		}
		writeCount := 0
		writeDescriptorAtomicForRuntime = func(string, *projectdaemon.Descriptor) error {
			writeCount++
			if writeCount > 2 {
				return errors.New("descriptor update failed")
			}
			return nil
		}
		serveWikidControlServerForOwner = func(*http.Server, net.Listener) <-chan error {
			done := make(chan error, 1)
			done <- errors.New("control server failed")
			return done
		}
		err = runWikidFrontdOwner(context.Background(), baseCfg, ownerCfg)
		Expect(err).To(MatchError("control server failed"))
		Expect(writeCount).To(BeNumerically(">=", 4))
		newFederatedWorkspaceManagerForOwner = previousWorkspaceManager

		bootstrapOwnerCfg := ownerCfg
		bootstrapOwnerCfg.DataDir = filepath.Join(blockingPathForLeafwikiTest(t), "data")
		err = runWikidFrontdOwner(context.Background(), baseCfg, bootstrapOwnerCfg)
		Expect(err).To(MatchError(ContainSubstring("bootstrap home workspace")))

		seedRuntimeHomeGrantsForOwner = func(*wikid.GrantStore, leafwikiRuntimeConfig) error {
			return errors.New("seed failed")
		}
		writeDescriptorAtomicForRuntime = previousWriteDescriptor
		serveWikidControlServerForOwner = previousServeControl
		startWikidFrontdRuntimeForOwner = func(context.Context, leafwikiRuntimeConfig, string, string) (*wikidFrontdRuntime, error) {
			return fakeRuntime(nil), nil
		}
		err = runWikidFrontdOwner(context.Background(), baseCfg, ownerCfg)
		Expect(err).To(MatchError("seed failed"))
		seedRuntimeHomeGrantsForOwner = previousSeedHomeGrants

		canceledCtx, cancel := context.WithCancel(context.Background())
		cancel()
		serveWikidControlServerForOwner = func(*http.Server, net.Listener) <-chan error {
			return make(chan error)
		}
		shutdownWikidControlServerForRuntime = func(*http.Server, context.Context) error {
			return errors.New("control shutdown failed")
		}
		err = runWikidFrontdOwner(canceledCtx, baseCfg, ownerCfg)
		Expect(err).To(MatchError("control shutdown failed"))
		shutdownWikidControlServerForRuntime = previousShutdownControl

		err = <-serveWikidControlServer(&http.Server{Handler: http.NotFoundHandler()}, leafwikiFakeListener{addr: leafwikiStringAddr("127.0.0.1:0")})
		Expect(err).NotTo(HaveOccurred())
		Expect(shutdownHTTPServer(&http.Server{}, context.Background())).To(Succeed())
	})

	ginkgo.It("covers descriptor health and manager cleanup branches", func() {
		t := ginkgo.GinkgoT()
		dataDir := filepath.Join(t.TempDir(), "data")
		rootDir := filepath.Join(t.TempDir(), "root")
		Expect(os.MkdirAll(dataDir, 0o755)).To(Succeed())
		Expect(os.MkdirAll(rootDir, 0o755)).To(Succeed())
		blockingFile := blockingPathForLeafwikiTest(t)

		descriptorPath := filepath.Join(t.TempDir(), "descriptor.json")
		Expect(os.WriteFile(descriptorPath, []byte("{bad"), 0o600)).To(Succeed())
		_, _, err := readHealthyProjectDaemon(context.Background(), descriptorPath, projectdaemon.Config{DataDir: filepath.Join(blockingFile, "data"), RootDir: rootDir})
		Expect(err).To(HaveOccurred())

		untrustedWorkspacedDescriptorPath := filepath.Join(t.TempDir(), "untrusted-workspaced.json")
		Expect(projectdaemon.WriteDescriptorAtomic(untrustedWorkspacedDescriptorPath, &projectdaemon.Descriptor{
			SchemaVersion:   projectdaemon.DescriptorSchemaVersion,
			Role:            projectdaemon.RoleWorkspaced,
			PID:             os.Getpid(),
			DataDir:         filepath.Join(t.TempDir(), "other-data"),
			RootDir:         filepath.Join(t.TempDir(), "other-root"),
			PrivateMCPURL:   "https://example.com/mcp",
			PrivateMCPToken: "private-token",
		})).To(Succeed())
		_, _, err = readHealthyProjectDaemon(context.Background(), untrustedWorkspacedDescriptorPath, projectdaemon.Config{DataDir: dataDir, RootDir: rootDir})
		Expect(err).To(MatchError(ContainSubstring("private MCP URL is not trusted")))

		mismatchedDescriptorPath := filepath.Join(t.TempDir(), "mismatched.json")
		Expect(projectdaemon.WriteDescriptorAtomic(mismatchedDescriptorPath, &projectdaemon.Descriptor{
			SchemaVersion: projectdaemon.DescriptorSchemaVersion,
			Role:          projectdaemon.RoleWikid,
			PID:           os.Getpid(),
			DataDir:       t.TempDir(),
			RootDir:       t.TempDir(),
		})).To(Succeed())
		desc, healthy, err := readHealthyProjectDaemon(context.Background(), mismatchedDescriptorPath, projectdaemon.Config{DataDir: dataDir, RootDir: rootDir})
		Expect(err).NotTo(HaveOccurred())
		Expect(healthy).To(BeFalse())
		Expect(desc).NotTo(BeNil())

		staleDescriptorPath := filepath.Join(t.TempDir(), "stale.json")
		Expect(projectdaemon.WriteDescriptorAtomic(staleDescriptorPath, &projectdaemon.Descriptor{
			SchemaVersion: 0,
			DataDir:       dataDir,
			RootDir:       rootDir,
		})).To(Succeed())
		_, _, err = readHealthyProjectDaemon(context.Background(), staleDescriptorPath, projectdaemon.Config{DataDir: filepath.Join(blockingFile, "data"), RootDir: rootDir})
		Expect(err).To(HaveOccurred())

		_, err = projectDaemonDescriptorHealthy(context.Background(), &projectdaemon.Descriptor{DataDir: filepath.Join(blockingFile, "data"), RootDir: rootDir})
		Expect(err).To(HaveOccurred())

		for _, tc := range []struct {
			status int
			want   bool
		}{
			{status: http.StatusUnauthorized, want: false},
			{status: http.StatusNoContent, want: true},
		} {
			privateMCPServer := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
				Expect(req.Header.Get(projectdaemon.ControlTokenHeader)).To(Equal("private-token"))
				rw.WriteHeader(tc.status)
			}))
			desc := &projectdaemon.Descriptor{
				SchemaVersion:   projectdaemon.DescriptorSchemaVersion,
				Role:            projectdaemon.RoleWorkspaced,
				PID:             os.Getpid(),
				DataDir:         dataDir,
				RootDir:         rootDir,
				PrivateMCPURL:   privateMCPServer.URL,
				PrivateMCPToken: "private-token",
			}
			healthy, err := projectDaemonDescriptorHealthy(context.Background(), desc)
			Expect(err).NotTo(HaveOccurred())
			Expect(healthy).To(Equal(tc.want))
			privateMCPServer.Close()
		}

		dataLock, err := locking.AcquireDataDirLock(dataDir)
		Expect(err).NotTo(HaveOccurred())
		rootLock, err := locking.AcquireRootDirLock(rootDir)
		Expect(err).NotTo(HaveOccurred())
		ginkgo.DeferCleanup(func() {
			Expect(dataLock.Release()).To(Succeed())
			Expect(rootLock.Release()).To(Succeed())
		})

		_, err = projectDaemonDescriptorHealthy(context.Background(), &projectdaemon.Descriptor{
			SchemaVersion: projectdaemon.DescriptorSchemaVersion,
			PID:           os.Getpid(),
			DataDir:       dataDir,
			RootDir:       rootDir,
			ControlURL:    "https://example.com",
		})
		Expect(err).To(MatchError(ContainSubstring("control URL is not trusted")))

		unreachableDesc := &projectdaemon.Descriptor{
			SchemaVersion: projectdaemon.DescriptorSchemaVersion,
			PID:           os.Getpid(),
			DataDir:       dataDir,
			RootDir:       rootDir,
			ControlURL:    "http://127.0.0.1:1",
			ControlToken:  "control-token",
		}
		_, err = projectDaemonDescriptorHealthy(context.Background(), unreachableDesc)
		Expect(err).To(MatchError(ContainSubstring("control health is unreachable")))

		mismatchServer := httptest.NewServer(projectdaemon.NewControlServer(projectdaemon.ControlServerOptions{
			Token:        "control-token",
			Sessions:     projectdaemon.NewSessionRegistry(time.Minute, nil),
			AuthDisabled: true,
			Health: projectdaemon.DaemonHealth{
				SchemaVersion: projectdaemon.DescriptorSchemaVersion,
				PID:           os.Getpid() + 1,
				DataDir:       dataDir,
				RootDir:       rootDir,
			},
		}))
		ginkgo.DeferCleanup(mismatchServer.Close)
		mismatchDesc := *unreachableDesc
		mismatchDesc.ControlURL = mismatchServer.URL
		_, err = projectDaemonDescriptorHealthy(context.Background(), &mismatchDesc)
		Expect(err).To(MatchError(ContainSubstring("control health does not match descriptor")))

		healthyServer := httptest.NewServer(projectdaemon.NewControlServer(projectdaemon.ControlServerOptions{
			Token:        "control-token",
			Sessions:     projectdaemon.NewSessionRegistry(time.Minute, nil),
			AuthDisabled: true,
			Health: projectdaemon.DaemonHealth{
				SchemaVersion: projectdaemon.DescriptorSchemaVersion,
				PID:           os.Getpid(),
				DataDir:       dataDir,
				RootDir:       rootDir,
			},
		}))
		ginkgo.DeferCleanup(healthyServer.Close)
		healthyDesc := *unreachableDesc
		healthyDesc.ControlURL = healthyServer.URL
		Expect(projectDaemonDescriptorHealthy(context.Background(), &healthyDesc)).To(BeTrue())

		mismatchPath := filepath.Join(t.TempDir(), "healthy-mismatch.json")
		Expect(projectdaemon.WriteDescriptorAtomic(mismatchPath, &healthyDesc)).To(Succeed())
		_, _, err = readHealthyProjectDaemon(context.Background(), mismatchPath, projectdaemon.Config{DataDir: filepath.Join(t.TempDir(), "other-data"), RootDir: rootDir})
		Expect(err).To(MatchError(ContainSubstring("project daemon config mismatch")))

		manager := newFederatedWorkspaceManager(leafwikiRuntimeConfig{}, "daemon-token", "http://wikid.local", wikid.GlobalLayout(t.TempDir()), wikid.NewWorkspaceSupervisor(wikid.WorkspaceSupervisorOptions{}))
		manager.descriptors["workspace-a"] = []string{"descriptor-a.json"}
		manager.removeDescriptor = func(string) error {
			return errors.New("remove failed")
		}
		Expect(manager.stop(context.Background())).To(MatchError(ContainSubstring("remove failed")))

		descriptorDir := filepath.Join(t.TempDir(), "descriptor-dir")
		Expect(os.MkdirAll(filepath.Join(descriptorDir, "child"), 0o755)).To(Succeed())
		Expect(removeNonRegularDescriptor(descriptorDir)).To(MatchError(ContainSubstring("remove non-regular descriptor")))

		badDescriptorManager := newFederatedWorkspaceManager(leafwikiRuntimeConfig{}, "daemon-token", "http://wikid.local", wikid.GlobalLayout(t.TempDir()), wikid.NewWorkspaceSupervisor(wikid.WorkspaceSupervisorOptions{}))
		err = badDescriptorManager.writeWorkspaceDescriptor(
			wikid.WorkspaceRecord{ID: "workspace-a", DataDir: "bad\x00data", RootDir: t.TempDir()},
			leafwikiRuntimeConfig{Workspace: wiki.Workspace{DataDir: "bad\x00data", RootDir: t.TempDir()}},
			internalRuntimeRoleReady{Role: projectdaemon.RoleWorkspaced, PID: os.Getpid(), URL: "http://workspace.local"},
		)
		Expect(err).To(HaveOccurred())

		writeFailManager := newFederatedWorkspaceManager(leafwikiRuntimeConfig{}, "daemon-token", "http://wikid.local", wikid.GlobalLayout(t.TempDir()), wikid.NewWorkspaceSupervisor(wikid.WorkspaceSupervisorOptions{}))
		writeFailManager.startRole = func(startup internalRuntimeRoleStartupConfig) (*internalRuntimeRoleProcess, internalRuntimeRoleReady, error) {
			proc, done := newLeafwikiRuntimeRoleProcess(startup.Role, os.Getpid())
			ginkgo.DeferCleanup(func() {
				releaseLeafwikiRuntimeRoleProcesses([]chan error{done}, context.Canceled)
			})
			return proc, internalRuntimeRoleReady{Role: startup.Role, PID: os.Getpid(), URL: "http://workspace.local"}, nil
		}
		writeFailManager.writeDescriptor = func(wikid.WorkspaceRecord, leafwikiRuntimeConfig, internalRuntimeRoleReady) error {
			return errors.New("write descriptor failed")
		}
		_, err = writeFailManager.Ensure(context.Background(), wikid.WorkspaceRecord{ID: "workspace-b", DataDir: t.TempDir(), RootDir: t.TempDir()})
		Expect(err).To(MatchError("write descriptor failed"))

		previousHash := configHashForRuntime
		previousWriteDescriptor := writeDescriptorAtomicForRuntime
		configHashForRuntime = func(projectdaemon.Config) (string, error) {
			return "", errors.New("hash failed")
		}
		ginkgo.DeferCleanup(func() {
			configHashForRuntime = previousHash
			writeDescriptorAtomicForRuntime = previousWriteDescriptor
		})
		descriptorManager := newFederatedWorkspaceManager(leafwikiRuntimeConfig{}, "daemon-token", "http://wikid.local", wikid.GlobalLayout(t.TempDir()), wikid.NewWorkspaceSupervisor(wikid.WorkspaceSupervisorOptions{}))
		err = descriptorManager.writeWorkspaceDescriptor(
			wikid.WorkspaceRecord{ID: "workspace-c", DataDir: t.TempDir(), RootDir: t.TempDir()},
			leafwikiRuntimeConfig{Workspace: wiki.Workspace{DataDir: t.TempDir(), RootDir: t.TempDir()}},
			internalRuntimeRoleReady{Role: projectdaemon.RoleWorkspaced, PID: os.Getpid(), URL: "http://workspace.local"},
		)
		Expect(err).To(MatchError("hash failed"))

		configHashForRuntime = previousHash
		writeDescriptorAtomicForRuntime = func(string, *projectdaemon.Descriptor) error {
			return errors.New("descriptor write failed")
		}
		err = descriptorManager.writeWorkspaceDescriptor(
			wikid.WorkspaceRecord{ID: "workspace-d", DataDir: t.TempDir(), RootDir: t.TempDir()},
			leafwikiRuntimeConfig{Workspace: wiki.Workspace{DataDir: t.TempDir(), RootDir: t.TempDir()}},
			internalRuntimeRoleReady{Role: projectdaemon.RoleWorkspaced, PID: os.Getpid(), URL: "http://workspace.local"},
		)
		Expect(err).To(MatchError("descriptor write failed"))

		writeDescriptorAtomicForRuntime = previousWriteDescriptor
		removeTargetManager := newFederatedWorkspaceManager(leafwikiRuntimeConfig{}, "daemon-token", "http://wikid.local", wikid.Layout{RuntimeDir: blockingFile}, wikid.NewWorkspaceSupervisor(wikid.WorkspaceSupervisorOptions{}))
		err = removeTargetManager.writeWorkspaceDescriptor(
			wikid.WorkspaceRecord{ID: "workspace-e", DataDir: t.TempDir(), RootDir: t.TempDir()},
			leafwikiRuntimeConfig{Workspace: wiki.Workspace{DataDir: t.TempDir(), RootDir: t.TempDir()}},
			internalRuntimeRoleReady{Role: projectdaemon.RoleWorkspaced, PID: os.Getpid(), URL: "http://workspace.local"},
		)
		Expect(err).To(MatchError(ContainSubstring("inspect descriptor target")))
	})

	ginkgo.It("covers federated attach orchestration seam branches", func() {
		t := ginkgo.GinkgoT()
		cfg := leafwikiRuntimeConfig{
			Workspace: wiki.Workspace{
				DataDir: filepath.Join(t.TempDir(), "workspace-data"),
				RootDir: filepath.Join(t.TempDir(), "workspace-root"),
			},
			DisableAuth:  true,
			Logging:      leaflogging.Config{Target: leaflogging.TargetStderr},
			RuntimeStack: projectdaemon.RuntimeStackWikidFrontd,
		}
		requestCfg, err := daemonWorkspaceRequestConfigForRuntime(cfg)
		Expect(err).NotTo(HaveOccurred())
		globalCfg, err := daemonRequestConfigForRuntime(cfg)
		Expect(err).NotTo(HaveOccurred())
		globalDesc := &projectdaemon.Descriptor{
			SchemaVersion: projectdaemon.DescriptorSchemaVersion,
			Role:          projectdaemon.RoleWikid,
			DataDir:       globalCfg.DataDir,
			RootDir:       globalCfg.RootDir,
			ControlURL:    "http://127.0.0.1:1",
			ControlToken:  "daemon-token",
			Config:        globalCfg,
		}

		badRegistryLayout := wikid.GlobalLayout(t.TempDir())
		badRegistryPath := blockingPathForLeafwikiTest(t)
		badRegistryLayout.DBPath = filepath.Join(badRegistryPath, "registry.db")
		_, _, err = registeredFederatedWorkspaceForRequest(badRegistryLayout, projectdaemon.Config{DataDir: t.TempDir(), RootDir: t.TempDir()})
		Expect(err).To(HaveOccurred())

		firstContactLayout := wikid.GlobalLayout(t.TempDir())
		_, _, err = registerFederatedFirstContact(firstContactLayout, projectdaemon.Config{DataDir: t.TempDir(), RootDir: t.TempDir()}, leafwikiRuntimeConfig{
			APIKey:        "lwk_key_missing",
			MCPTransports: mcpTransports{Stdio: true},
			JWTSecret:     "jwt",
			AdminPassword: "admin",
		})
		Expect(err).To(MatchError(ContainSubstring("resolve native STDIO API-key grant user")))

		Expect(ensureFederatedWorkspace(context.Background(), nil, "workspace-a", leafwikiRuntimeConfig{})).To(MatchError("global wikid descriptor is unavailable"))
		invalidEnsureDesc := *globalDesc
		invalidEnsureDesc.ControlURL = "http://[::1"
		Expect(ensureFederatedWorkspace(context.Background(), &invalidEnsureDesc, "workspace-a", leafwikiRuntimeConfig{})).To(HaveOccurred())
		ensureServer := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
			Expect(req.Header.Get(projectdaemon.ControlTokenHeader)).To(Equal("daemon-token"))
			Expect(req.Header.Get("Authorization")).To(Equal("Bearer stdio-key"))
			if strings.Contains(req.URL.Path, "denied") {
				writeRuntimeError(rw, http.StatusForbidden, runtimeErrorCodeWorkspaceGrantDenied)
				return
			}
			rw.WriteHeader(http.StatusNoContent)
		}))
		ginkgo.DeferCleanup(ensureServer.Close)
		ensureDesc := *globalDesc
		ensureDesc.ControlURL = ensureServer.URL
		Expect(ensureFederatedWorkspace(context.Background(), &ensureDesc, "workspace-denied", leafwikiRuntimeConfig{APIKey: "stdio-key"})).To(MatchError(ContainSubstring("ensure workspace")))
		Expect(ensureFederatedWorkspace(context.Background(), &ensureDesc, "workspace-ok", leafwikiRuntimeConfig{APIKey: "stdio-key"})).To(Succeed())

		previousReadHealthy := readHealthyProjectDaemonForAttach
		previousVerifyKey := verifyStdioAPIKeyFromStorageForAttach
		previousSpawn := spawnProjectDaemonOwnerForAttach
		previousWait := waitForProjectDaemonForAttach
		previousRegister := registerFederatedFirstContactForAttach
		previousEnsure := ensureFederatedWorkspaceForAttach
		ginkgo.DeferCleanup(func() {
			readHealthyProjectDaemonForAttach = previousReadHealthy
			verifyStdioAPIKeyFromStorageForAttach = previousVerifyKey
			spawnProjectDaemonOwnerForAttach = previousSpawn
			waitForProjectDaemonForAttach = previousWait
			registerFederatedFirstContactForAttach = previousRegister
			ensureFederatedWorkspaceForAttach = previousEnsure
		})

		stdioHomeCfg := cfg
		stdioHomeCfg.MCPTransports = mcpTransports{Stdio: true}
		readHealthyProjectDaemonForAttach = func(context.Context, string, projectdaemon.Config) (*projectdaemon.Descriptor, bool, error) {
			return nil, false, errors.New("direct descriptor read failed")
		}
		_, err = attachOrStartFederatedProjectDaemon(context.Background(), stdioHomeCfg, globalCfg, filepath.Join(t.TempDir(), "home-descriptor.json"))
		Expect(err).To(MatchError("direct descriptor read failed"))

		directReadCalls := 0
		readHealthyProjectDaemonForAttach = func(context.Context, string, projectdaemon.Config) (*projectdaemon.Descriptor, bool, error) {
			directReadCalls++
			if directReadCalls == 1 {
				return &projectdaemon.Descriptor{Config: globalCfg}, false, nil
			}
			return globalDesc, true, nil
		}
		desc, err := attachOrStartFederatedProjectDaemon(context.Background(), stdioHomeCfg, globalCfg, filepath.Join(t.TempDir(), "stale-home-descriptor.json"))
		Expect(err).NotTo(HaveOccurred())
		Expect(desc).To(Equal(globalDesc))

		readHealthyProjectDaemonForAttach = func(context.Context, string, projectdaemon.Config) (*projectdaemon.Descriptor, bool, error) {
			return nil, false, nil
		}
		spawnProjectDaemonOwnerForAttach = func(leafwikiRuntimeConfig) (string, error) {
			return "startup.err", nil
		}
		waitForProjectDaemonForAttach = func(context.Context, string, string, projectdaemon.Config, mcpTransports) (*projectdaemon.Descriptor, error) {
			return globalDesc, nil
		}
		registerFederatedFirstContactForAttach = func(wikid.Layout, projectdaemon.Config, leafwikiRuntimeConfig) (wikid.WorkspaceRecord, bool, error) {
			return wikid.WorkspaceRecord{ID: wikid.HomeWorkspaceID}, true, nil
		}
		desc, err = attachOrStartFederatedProjectDaemon(context.Background(), cfg, requestCfg, filepath.Join(t.TempDir(), "workspace-descriptor.json"))
		Expect(err).NotTo(HaveOccurred())
		Expect(desc).To(Equal(globalDesc))

		spawnProjectDaemonOwnerForAttach = func(leafwikiRuntimeConfig) (string, error) {
			return "", errors.New("spawn failed")
		}
		_, err = attachOrStartFederatedProjectDaemon(context.Background(), cfg, requestCfg, filepath.Join(t.TempDir(), "workspace-descriptor.json"))
		Expect(err).To(MatchError("spawn failed"))

		spawnProjectDaemonOwnerForAttach = func(leafwikiRuntimeConfig) (string, error) {
			return "startup.err", nil
		}
		waitForProjectDaemonForAttach = func(context.Context, string, string, projectdaemon.Config, mcpTransports) (*projectdaemon.Descriptor, error) {
			return nil, errors.New("wait failed")
		}
		_, err = attachOrStartFederatedProjectDaemon(context.Background(), cfg, requestCfg, filepath.Join(t.TempDir(), "workspace-descriptor.json"))
		Expect(err).To(MatchError("wait failed"))

		mismatchedDesc := *globalDesc
		mismatchedConfig := globalCfg
		mismatchedConfig.DataDir = filepath.Join(t.TempDir(), "other")
		mismatchedDesc.Config = mismatchedConfig
		mismatchedDesc.DataDir = mismatchedConfig.DataDir
		readHealthyProjectDaemonForAttach = func(context.Context, string, projectdaemon.Config) (*projectdaemon.Descriptor, bool, error) {
			return &mismatchedDesc, true, nil
		}
		_, err = attachOrStartFederatedProjectDaemon(context.Background(), cfg, requestCfg, filepath.Join(t.TempDir(), "workspace-descriptor.json"))
		Expect(err).To(MatchError(ContainSubstring("project daemon config mismatch")))

		readHealthyProjectDaemonForAttach = func(context.Context, string, projectdaemon.Config) (*projectdaemon.Descriptor, bool, error) {
			return globalDesc, true, nil
		}
		registerFederatedFirstContactForAttach = func(wikid.Layout, projectdaemon.Config, leafwikiRuntimeConfig) (wikid.WorkspaceRecord, bool, error) {
			return wikid.WorkspaceRecord{}, false, errors.New("register failed")
		}
		_, err = attachOrStartFederatedProjectDaemon(context.Background(), cfg, requestCfg, filepath.Join(t.TempDir(), "workspace-descriptor.json"))
		Expect(err).To(MatchError("register failed"))

		registerFederatedFirstContactForAttach = func(wikid.Layout, projectdaemon.Config, leafwikiRuntimeConfig) (wikid.WorkspaceRecord, bool, error) {
			return wikid.WorkspaceRecord{ID: "workspace-a"}, false, nil
		}
		ensureFederatedWorkspaceForAttach = func(context.Context, *projectdaemon.Descriptor, workspaceid.WorkspaceID, leafwikiRuntimeConfig) error {
			return errors.New("ensure failed")
		}
		_, err = attachOrStartFederatedProjectDaemon(context.Background(), cfg, requestCfg, filepath.Join(t.TempDir(), "workspace-descriptor.json"))
		Expect(err).To(MatchError("ensure failed"))

		stdioCfg := cfg
		stdioCfg.DisableAuth = false
		stdioCfg.JWTSecret = "jwt"
		stdioCfg.AdminPassword = "admin"
		stdioCfg.APIKey = "bad-key"
		stdioCfg.MCPTransports = mcpTransports{Stdio: true}
		readHealthyProjectDaemonForAttach = func(context.Context, string, projectdaemon.Config) (*projectdaemon.Descriptor, bool, error) {
			return nil, false, nil
		}
		verifyStdioAPIKeyFromStorageForAttach = func(string, string) error {
			return coreauth.ErrInvalidToken
		}
		_, err = attachOrStartFederatedProjectDaemon(context.Background(), stdioCfg, requestCfg, filepath.Join(t.TempDir(), "workspace-descriptor.json"))
		Expect(err).To(MatchError("invalid native STDIO API key"))

		badRuntimeCfg := cfg
		badRuntimeCfg.Workspace.DataDir = "bad\x00data"
		_, err = attachOrStartRuntimeDaemon(context.Background(), badRuntimeCfg)
		Expect(err).To(HaveOccurred())

		oldHome, hadHome := os.LookupEnv("HOME")
		Expect(os.Setenv("HOME", "")).To(Succeed())
		_, err = attachOrStartFederatedProjectDaemon(context.Background(), cfg, requestCfg, filepath.Join(t.TempDir(), "workspace-descriptor.json"))
		Expect(err).To(HaveOccurred())
		if hadHome {
			Expect(os.Setenv("HOME", oldHome)).To(Succeed())
		} else {
			Expect(os.Unsetenv("HOME")).To(Succeed())
		}

		readHealthyProjectDaemonForAttach = func(context.Context, string, projectdaemon.Config) (*projectdaemon.Descriptor, bool, error) {
			return nil, false, errors.New("read healthy failed")
		}
		_, err = attachOrStartFederatedProjectDaemon(context.Background(), cfg, requestCfg, filepath.Join(t.TempDir(), "workspace-descriptor.json"))
		Expect(err).To(MatchError("read healthy failed"))

		readHealthyProjectDaemonForAttach = func(context.Context, string, projectdaemon.Config) (*projectdaemon.Descriptor, bool, error) {
			return nil, false, nil
		}
		authRequiredCfg := cfg
		authRequiredCfg.DisableAuth = false
		_, err = attachOrStartFederatedProjectDaemon(context.Background(), authRequiredCfg, requestCfg, filepath.Join(t.TempDir(), "workspace-descriptor.json"))
		Expect(err).To(MatchError(ContainSubstring("JWT secret is required")))

		verifyStdioAPIKeyFromStorageForAttach = func(string, string) error {
			return errors.New("auth store unavailable")
		}
		_, err = attachOrStartFederatedProjectDaemon(context.Background(), stdioCfg, requestCfg, filepath.Join(t.TempDir(), "workspace-descriptor.json"))
		Expect(err).To(MatchError(ContainSubstring("verify native STDIO API key")))
	})
})

func leafwikiEdgeFlagSet() (*flag.FlagSet, *cliFlags) {
	ginkgo.GinkgoHelper()

	fs := flag.NewFlagSet("leafwiki-edge", flag.ContinueOnError)
	var errOut strings.Builder
	fs.SetOutput(&errOut)
	flags := registerFlags(fs)
	return fs, flags
}

func captureLeafwikiStdout(fn func()) string {
	ginkgo.GinkgoHelper()

	previous := os.Stdout
	reader, writer, err := os.Pipe()
	Expect(err).NotTo(HaveOccurred())
	restored := false
	ginkgo.DeferCleanup(func() {
		if !restored {
			os.Stdout = previous
		}
		_ = reader.Close()
		_ = writer.Close()
	})

	os.Stdout = writer
	fn()
	os.Stdout = previous
	restored = true
	Expect(writer.Close()).To(Succeed())

	output, err := io.ReadAll(reader)
	Expect(err).NotTo(HaveOccurred())
	return string(output)
}

type leafwikiFailWriter struct {
	err error
}

func (w leafwikiFailWriter) Write([]byte) (int, error) {
	return 0, w.err
}

type leafwikiFailAfterWriter struct {
	failAt int
	writes int
	err    error
}

func (w *leafwikiFailAfterWriter) Write(p []byte) (int, error) {
	w.writes++
	if w.writes == w.failAt {
		return 0, w.err
	}
	return len(p), nil
}

type leafwikiErrReader struct {
	err error
}

func (r leafwikiErrReader) Read([]byte) (int, error) {
	return 0, r.err
}

type leafwikiErrReadCloser struct {
	err      error
	closedMu sync.Mutex
	closed   bool
}

func (r *leafwikiErrReadCloser) Read([]byte) (int, error) {
	return 0, r.err
}

func (r *leafwikiErrReadCloser) Close() error {
	r.closedMu.Lock()
	defer r.closedMu.Unlock()
	r.closed = true
	return nil
}

func (r *leafwikiErrReadCloser) Closed() bool {
	r.closedMu.Lock()
	defer r.closedMu.Unlock()
	return r.closed
}

type leafwikiFakeTempFile struct {
	name     string
	chmodErr error
	writeErr error
	closeErr error
}

func (f *leafwikiFakeTempFile) Name() string {
	return f.name
}

func (f *leafwikiFakeTempFile) Chmod(os.FileMode) error {
	return f.chmodErr
}

func (f *leafwikiFakeTempFile) Write(p []byte) (int, error) {
	if f.writeErr != nil {
		return 0, f.writeErr
	}
	return len(p), nil
}

func (f *leafwikiFakeTempFile) Close() error {
	return f.closeErr
}

type leafwikiFakeRuntimeLock struct {
	releaseErr error
}

func (l leafwikiFakeRuntimeLock) Release() error {
	return l.releaseErr
}

type leafwikiFakeMCPTransport struct {
	conn sdkmcp.Connection
	err  error
}

func (t leafwikiFakeMCPTransport) Connect(context.Context) (sdkmcp.Connection, error) {
	if t.err != nil {
		return nil, t.err
	}
	return t.conn, nil
}

type leafwikiFakeMCPConnection struct {
	reads    chan sdkjsonrpc.Message
	writes   chan sdkjsonrpc.Message
	closed   chan struct{}
	close    sync.Once
	readErr  error
	writeErr error
}

func newLeafwikiFakeMCPConnection() *leafwikiFakeMCPConnection {
	return &leafwikiFakeMCPConnection{
		reads:  make(chan sdkjsonrpc.Message, 2),
		writes: make(chan sdkjsonrpc.Message, 1),
		closed: make(chan struct{}),
	}
}

func (c *leafwikiFakeMCPConnection) Read(ctx context.Context) (sdkjsonrpc.Message, error) {
	select {
	case msg := <-c.reads:
		if c.readErr != nil {
			return nil, c.readErr
		}
		return msg, nil
	case <-c.closed:
		if c.readErr != nil {
			return nil, c.readErr
		}
		return nil, io.EOF
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (c *leafwikiFakeMCPConnection) Write(ctx context.Context, msg sdkjsonrpc.Message) error {
	if c.writeErr != nil {
		return c.writeErr
	}
	select {
	case c.writes <- msg:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (c *leafwikiFakeMCPConnection) Close() error {
	c.close.Do(func() {
		close(c.closed)
	})
	return nil
}

func (c *leafwikiFakeMCPConnection) SessionID() string {
	return "leafwiki-test-session"
}

type leafwikiStringAddr string

func (a leafwikiStringAddr) Network() string {
	return "leafwiki-test"
}

func (a leafwikiStringAddr) String() string {
	return string(a)
}

type leafwikiFakeListener struct {
	addr net.Addr
}

func (l leafwikiFakeListener) Accept() (net.Conn, error) {
	return nil, net.ErrClosed
}

func (l leafwikiFakeListener) Close() error {
	return nil
}

func (l leafwikiFakeListener) Addr() net.Addr {
	return l.addr
}

type leafwikiErrorListener struct {
	addr net.Addr
	err  error
}

func (l leafwikiErrorListener) Accept() (net.Conn, error) {
	return nil, l.err
}

func (l leafwikiErrorListener) Close() error {
	return nil
}

func (l leafwikiErrorListener) Addr() net.Addr {
	return l.addr
}

type leafwikiFailingResponseWriter struct {
	err      error
	header   http.Header
	statuses []int
}

func (w *leafwikiFailingResponseWriter) Header() http.Header {
	if w.header == nil {
		w.header = http.Header{}
	}
	return w.header
}

func (w *leafwikiFailingResponseWriter) Write([]byte) (int, error) {
	return 0, w.err
}

func (w *leafwikiFailingResponseWriter) WriteHeader(statusCode int) {
	w.statuses = append(w.statuses, statusCode)
}

func resolveWithSDKToken(resolver func(*http.Request) (projectdaemon.ActorContext, error), bearer string, userID string) (projectdaemon.ActorContext, error) {
	ginkgo.GinkgoHelper()

	var actor projectdaemon.ActorContext
	var resolverErr error
	handler := sdkauth.RequireBearerToken(func(context.Context, string, *http.Request) (*sdkauth.TokenInfo, error) {
		return &sdkauth.TokenInfo{
			UserID:     userID,
			Scopes:     []string{"leafwiki:mcp"},
			Expiration: time.Now().Add(time.Hour),
		}, nil
	}, nil)(http.HandlerFunc(func(_ http.ResponseWriter, req *http.Request) {
		actor, resolverErr = resolver(req)
	}))
	req := httptest.NewRequest(http.MethodPost, "/mcp", nil)
	req.Header.Set("Authorization", "Bearer "+bearer)
	handler.ServeHTTP(httptest.NewRecorder(), req)
	return actor, resolverErr
}

func swapInternalRuntimeRoleStarter(fn func(internalRuntimeRoleStartupConfig) (*internalRuntimeRoleProcess, internalRuntimeRoleReady, error)) {
	ginkgo.GinkgoHelper()

	previous := startInternalRuntimeRoleProcessForRuntime
	startInternalRuntimeRoleProcessForRuntime = fn
	ginkgo.DeferCleanup(func() {
		startInternalRuntimeRoleProcessForRuntime = previous
	})
}

func newLeafwikiRuntimeRoleProcess(role projectdaemon.RoleName, pid int) (*internalRuntimeRoleProcess, chan error) {
	ginkgo.GinkgoHelper()

	done := make(chan error, 1)
	return &internalRuntimeRoleProcess{
		role:     role,
		pid:      pid,
		done:     done,
		waitDone: make(chan struct{}),
	}, done
}

func releaseLeafwikiRuntimeRoleProcesses(doneChans []chan error, err error) {
	ginkgo.GinkgoHelper()

	for _, done := range doneChans {
		select {
		case done <- err:
		default:
		}
	}
}

func waitForLeafwikiRoleState(runtime *wikidFrontdRuntime, role projectdaemon.RoleName, state projectdaemon.RoleState) {
	ginkgo.GinkgoHelper()

	deadline := time.After(time.Second)
	ticker := time.NewTicker(time.Millisecond)
	defer ticker.Stop()
	for {
		if runtime.supervisor.State(role).State == state {
			return
		}
		select {
		case <-deadline:
			ginkgo.Fail("role state did not reach " + string(state))
		case <-ticker.C:
		}
	}
}

func waitForLeafwikiDescriptor(path string) *projectdaemon.Descriptor {
	ginkgo.GinkgoHelper()

	deadline := time.After(3 * time.Second)
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		desc, err := projectdaemon.ReadTrustedDescriptor(path)
		if err == nil {
			return desc
		}
		if !errors.Is(err, os.ErrNotExist) {
			ginkgo.Fail("descriptor did not become readable: " + err.Error())
		}
		select {
		case <-deadline:
			ginkgo.Fail("descriptor was not written: " + path)
		case <-ticker.C:
		}
	}
}

func waitForLeafwikiRuntimeReady(path string) internalRuntimeRoleReady {
	ginkgo.GinkgoHelper()

	deadline := time.After(3 * time.Second)
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		raw, err := os.ReadFile(path)
		if err == nil && len(strings.TrimSpace(string(raw))) > 0 {
			var ready internalRuntimeRoleReady
			Expect(json.Unmarshal(raw, &ready)).To(Succeed())
			return ready
		}
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			ginkgo.Fail("runtime ready file did not become readable: " + err.Error())
		}
		select {
		case <-deadline:
			ginkgo.Fail("runtime ready file was not written: " + path)
		case <-ticker.C:
		}
	}
}

func newLeafwikiReadyOwnerRuntime(parent context.Context) *wikidFrontdRuntime {
	ginkgo.GinkgoHelper()

	ctx, cancel := context.WithCancel(parent)
	return &wikidFrontdRuntime{
		ctx:           ctx,
		cancel:        cancel,
		supervisor:    wikid.NewSupervisor(wikid.SupervisorOptions{}),
		processes:     map[projectdaemon.RoleName]*internalRuntimeRoleProcess{},
		workspacedURL: "http://workspaced.local",
		roles: []projectdaemon.RoleHealth{
			{Name: projectdaemon.RoleWikid, State: projectdaemon.RoleStateReady, PID: os.Getpid(), URL: "http://wikid.local"},
			{Name: projectdaemon.RoleWorkspaced, State: projectdaemon.RoleStateReady, PID: os.Getpid(), URL: "http://workspaced.local", Private: true},
			{Name: projectdaemon.RoleFrontd, State: projectdaemon.RoleStateReady, PID: os.Getpid(), URL: "http://frontd.local"},
		},
	}
}

func blockingPathForLeafwikiTest(t ginkgo.FullGinkgoTInterface) string {
	ginkgo.GinkgoHelper()

	path := filepath.Join(t.TempDir(), "not-a-dir")
	Expect(os.WriteFile(path, []byte("x"), 0o600)).To(Succeed())
	return path
}

type leafwikiExitPanic int

func expectLeafwikiExit(code int, fn func()) {
	ginkgo.GinkgoHelper()

	previous := leafwikiExit
	leafwikiExit = func(got int) {
		panic(leafwikiExitPanic(got))
	}
	defer func() {
		leafwikiExit = previous
	}()

	Expect(fn).To(PanicWith(leafwikiExitPanic(code)))
}
