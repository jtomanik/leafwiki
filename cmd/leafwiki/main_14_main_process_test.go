package main

import (
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/onsi/gomega/gstruct"
	"github.com/perber/wiki/internal/localization"
	leaflogging "github.com/perber/wiki/internal/logging"
	"github.com/perber/wiki/internal/projectdaemon"
	"github.com/perber/wiki/internal/wiki"
	"github.com/perber/wiki/internal/wikid"
)

var _ = ginkgo.Describe("leafwiki main process", func() {
	ginkgo.It("rejects service-mode native transport configuration", ginkgo.Label("e2e"), func() {
		homeDir := leafwikiTempDir()
		serviceDir := filepath.Join(homeDir, ".leafwiki")
		Expect(os.MkdirAll(serviceDir, 0o755)).To(Succeed())
		configPath := filepath.Join(serviceDir, "leafwiki.yml")
		writeTestConfig(configPath, `disable-auth: true
mcp: stdio
`)

		stdout, stderr, err := runLeafwikiHelper([]string{"daemon"}, map[string]string{
			"HOME": homeDir,
		})
		Expect(err).To(MatchProcessExitError(), fmt.Sprintf("daemon with stdio MCP service config unexpectedly succeeded\nstdout:\n%s\nstderr:\n%s", stdout, stderr))
		Expect(stdout).To(BeEmpty(), fmt.Sprintf("stdout = %q, want empty", stdout))
		Expect(readJSONLogEntriesFromText(stderr)).To(ContainElement(haveJSONLogEntry(localization.MessageIDCLIErrorInvalidServiceConfigFile)), fmt.Sprintf("stderr = %q, want invalid service transport config log", stderr))

	})
})

var _ = ginkgo.Describe("daemon service configuration", func() {
	ginkgo.It("uses defaults instead of environment", ginkgo.Label("unit"), func() {
		homeDir := leafwikiTempDir()
		leafwikiSetenv("HOME", homeDir)
		leafwikiSetenv("LEAFWIKI_HOST", "0.0.0.0")
		leafwikiSetenv("LEAFWIKI_PORT", "9999")
		leafwikiSetenv("LEAFWIKI_ROOT_DIR", filepath.Join(leafwikiTempDir(), "env-root"))
		leafwikiSetenv("LEAFWIKI_LOG_FILE", "env.log")
		serviceDir := filepath.Join(homeDir, ".leafwiki")
		Expect(os.MkdirAll(serviceDir, 0o755)).To(Succeed())
		writeTestConfig(filepath.Join(serviceDir, "leafwiki.yml"), "disable-auth: true\n")

		fs := flag.NewFlagSet("leafwiki", flag.ContinueOnError)
		fs.SetOutput(io.Discard)
		flags := registerFlags(fs)
		Expect(fs.Parse([]string{"daemon"})).To(Succeed())
		visited := map[string]bool{}
		fs.Visit(func(f *flag.Flag) { visited[f.Name] = true })
		Expect(applyDaemonServiceConfig(fs, flags, visited, fs.Args())).To(Succeed())

		Expect(resolveString("host", *flags.host, visited, "LEAFWIKI_HOST", "127.0.0.1")).To(Equal("127.0.0.1"))
		Expect(resolveString("port", *flags.port, visited, "LEAFWIKI_PORT", "8080")).To(Equal("8080"))
		workspace, err := resolveWorkspace(flags, visited)
		Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("resolveWorkspace: %v", err))
		Expect(workspace).To(SatisfyAll(
			HaveField("DataDir", Equal(serviceDir)),
			HaveField("RootDir", Equal(filepath.Join(serviceDir, "root"))),
		))

		loggingConfig, err := resolveLoggingConfig(flags, visited, workspace.DataDir)
		Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("resolveLoggingConfig: %v", err))

		wantLogFile := filepath.Join(serviceDir, ".leafwiki", "logs", "leafwiki.log")
		Expect(loggingConfig).To(haveLoggingConfig(leaflogging.TargetFile, Equal(wantLogFile)))

	})
})

var _ = ginkgo.Describe("leafwiki main process", func() {
	ginkgo.It("daemon runs foreground runtime until signal", ginkgo.Label("e2e"), func() {
		if !supportsGracefulProcessSignal() {
			ginkgo.Skip(fmt.Sprint("SIGTERM-style graceful process signaling is not available on this platform"))
		}

		homeDir := leafwikiTempDir()
		serviceDir := filepath.Join(homeDir, ".leafwiki")
		Expect(os.MkdirAll(serviceDir, 0o755)).To(Succeed())
		port := freeTCPPort()
		configPath := filepath.Join(serviceDir, "leafwiki.yml")
		writeTestConfig(configPath, fmt.Sprintf(`disable-auth: true
host: 127.0.0.1
port: %s
log-target: stderr
daemon-idle-timeout: 0
`, port))

		proc := startLeafwikiHelper([]string{"daemon"}, map[string]string{
			"HOME":                         homeDir,
			"LEAFWIKI_DAEMON_IDLE_TIMEOUT": "0",
		})

		waitForLeafwikiReady(proc, port)
		projectDescriptorPath := projectdaemon.DescriptorPath(serviceDir)
		projectDesc := waitForProjectDaemonDescriptor(serviceDir)
		Expect(projectDesc).To(SatisfyAll(
			HaveField("Role", Equal(projectdaemon.RoleWikid)),
			HaveField("RuntimeStack", Equal(projectdaemon.RuntimeStackWikidFrontd)),
		))

		canonicalDataDir, canonicalRootDir, err := projectdaemon.CanonicalizeProject(serviceDir, filepath.Join(serviceDir, "root"))
		Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("canonicalize service dirs: %v", err))
		Expect(projectDesc).To(SatisfyAll(
			HaveField("DataDir", Equal(canonicalDataDir)),
			HaveField("RootDir", Equal(canonicalRootDir)),
		))

		Expect(projectDesc.Roles).To(ContainElement(HaveField("Name", Equal(projectdaemon.RoleWikid))))
		Expect(projectDesc.Roles).To(ContainElement(HaveField("Name", Equal(projectdaemon.RoleFrontd))))
		Expect(projectDesc.Roles).To(ContainElement(HaveField("Name", Equal(projectdaemon.RoleWorkspaced))))

		globalDescriptorPath := projectdaemon.GlobalDescriptorPath(wikid.GlobalLayout(serviceDir).RuntimeDir, projectdaemon.RoleWikid)
		globalDesc := waitForProjectDaemonDescriptorAtPath(globalDescriptorPath)
		Expect(globalDesc).To(SatisfyAll(
			HaveField("PID", Equal(projectDesc.PID)),
			HaveField("Role", Equal(projectdaemon.RoleWikid)),
		))

		time.Sleep(projectdaemon.DefaultHeartbeatTTL + 500*time.Millisecond)
		waitForLeafwikiReady(proc, port)
		Expect(classifyProcessExists(proc.cmd.Process.Pid)).To(Equal(processRunning), fmt.Sprintf("daemon process exited before signal"))

		waitForForegroundSignalHandler()
		Expect(signalLeafwikiProcess(proc.cmd.Process)).To(Succeed(), fmt.Sprintf("send SIGTERM: %v", err))
		proc.waitForExit()
		waitForFileRemoved(projectDescriptorPath, 5*time.Second)
		waitForFileRemoved(globalDescriptorPath, 5*time.Second)
		waitForLeafwikiUnavailable(port)

	})
})

var _ = ginkgo.Describe("leafwiki main process", func() {
	ginkgo.It("daemon rejects runtime stack environment", ginkgo.Label("e2e"), func() {
		homeDir := leafwikiTempDir()
		serviceDir := filepath.Join(homeDir, ".leafwiki")
		Expect(os.MkdirAll(serviceDir, 0o755)).To(Succeed())
		port := freeTCPPort()
		writeTestConfig(filepath.Join(serviceDir, "leafwiki.yml"), fmt.Sprintf(`disable-auth: true
host: 127.0.0.1
port: %s
log-target: stderr
daemon-idle-timeout: 0
`, port))

		stdout, stderr, err := runLeafwikiHelper([]string{"daemon"}, map[string]string{
			"HOME":                   homeDir,
			"LEAFWIKI_RUNTIME_STACK": "bogus",
		})
		Expect(err).To(MatchProcessExitError(), fmt.Sprintf("daemon with removed runtime env unexpectedly succeeded\nstdout:\n%s\nstderr:\n%s", stdout, stderr))
		Expect(readJSONLogEntriesFromText(stderr)).To(ContainElement(haveJSONLogEntry(string(cliMessageID(localization.MessageIDCLIErrorInvalidEnvironment)))))

	})
})

var _ = ginkgo.Describe("leafwiki main process", func() {
	ginkgo.It("daemon runs from service example template", ginkgo.Label("e2e"), func() {
		if !supportsGracefulProcessSignal() {
			ginkgo.Skip(fmt.Sprint("SIGTERM-style graceful process signaling is not available on this platform"))
		}

		homeDir := leafwikiTempDir()
		serviceDir := filepath.Join(homeDir, ".leafwiki")
		Expect(os.MkdirAll(serviceDir, 0o755)).To(Succeed())
		port := freeTCPPort()
		raw := readFileString(serviceExampleConfigPath())
		raw = strings.Replace(raw, "port: 8080", "port: "+port, 1)
		configPath := filepath.Join(serviceDir, "leafwiki.yml")
		writeTestConfig(configPath, raw)

		proc := startLeafwikiHelper([]string{"daemon"}, map[string]string{
			"HOME": homeDir,
		})

		waitForLeafwikiReady(proc, port)
		desc := waitForProjectDaemonDescriptor(serviceDir)
		Expect(desc.Config).To(MatchPublicWorkspaceSyncDaemonConfig(gstruct.Fields{
			"Port": Equal(port),
		}))

		wantLogPath := filepath.Join(desc.DataDir, "logs", "leafwiki.log")
		Expect(desc.Config).To(SatisfyAll(
			HaveField("LogTarget", Equal("file")),
			HaveField("LogFile", Equal(wantLogPath)),
		))

		waitForFileContaining(wantLogPath, leafwikiStartupLogMessage)

		resp, err := http.Get("http://127.0.0.1:" + port + "/api/config")
		Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("GET /api/config: %v", err))

		defer func() { _ = resp.Body.Close() }()
		Expect(resp).To(HaveHTTPStatus(http.StatusOK))
		var config map[string]any
		Expect(json.NewDecoder(resp.Body).Decode(&config)).To(Succeed(), fmt.Sprintf("decode config: %v", err))
		Expect(config).To(HaveKeyWithValue("enableWorkspaceSync", true))

		toolNames := listProcessHTTPMCPToolNames("http://127.0.0.1:" + port + "/mcp/workspaces/home")
		Expect(toolNames).To(matchToolNames(federatedRuntimeToolNames()))

		waitForForegroundSignalHandler()
		Expect(signalLeafwikiProcess(proc.cmd.Process)).To(Succeed(), fmt.Sprintf("send SIGTERM: %v", err))
		proc.waitForExit()
		waitForLeafwikiUnavailable(port)

	})
})

var _ = ginkgo.Describe("leafwiki main process", func() {
	ginkgo.It("help flag after other flags stays on stdout", ginkgo.Label("e2e"), func() {
		stdout, stderr, err := runLeafwikiHelper([]string{
			"--log-target", "stderr",
			"--help",
		}, nil)
		Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("help process error = %v, stderr=%q", err, stderr))
		Expect(readJSONLogEntriesFromText(stdout)).To(BeEmpty(), fmt.Sprintf("stdout contains log output: %q", stdout))
		Expect(stderr).To(BeEmpty(), fmt.Sprintf("stderr = %q, want empty", stderr))

	})
})

var _ = ginkgo.Describe("leafwiki main process", func() {
	ginkgo.It("help flag value does not short circuit subcommand parsing", ginkgo.Label("e2e"), func() {
		stdout, stderr, err := runLeafwikiHelper([]string{
			"--admin-password", "help",
			"unknown-command",
		}, nil)
		Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("unknown command process error = %v, stderr=%q", err, stderr))
		Expect(readJSONLogEntriesFromText(stdout)).To(BeEmpty(), fmt.Sprintf("stdout contains log output: %q", stdout))

	})
})

var _ = ginkgo.Describe("leafwiki main process", func() {
	ginkgo.It("unknown command ignores dirty server only environment", ginkgo.Label("e2e"), func() {
		dataDir := filepath.Join(leafwikiTempDir(), "data")
		stdout, stderr, err := runLeafwikiHelper([]string{
			"--data-dir", dataDir,
			"unknown-command",
		}, map[string]string{
			"LEAFWIKI_MAX_ASSET_UPLOAD_SIZE": "bad",
		})
		Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("unknown command process error = %v, stderr=%q", err, stderr))
		Expect(readJSONLogEntriesFromText(stdout)).To(BeEmpty(), fmt.Sprintf("stdout contains log output: %q", stdout))
		Expect(stderr).To(BeEmpty(), fmt.Sprintf("stderr = %q, want empty", stderr))

	})
})

var _ = ginkgo.Describe("leafwiki main process", func() {
	ginkgo.It("unknown command rejects MCPstdio environment", ginkgo.Label("e2e"), func() {
		dataDir := filepath.Join(leafwikiTempDir(), "data")
		stdout, stderr, err := runLeafwikiHelper([]string{
			"--data-dir", dataDir,
			"unknown-command",
		}, map[string]string{
			"LEAFWIKI_MCP_STDIO": "true",
		})
		Expect(err).To(MatchProcessExitError(), fmt.Sprintf("unknown command with removed env unexpectedly succeeded\nstdout:\n%s\nstderr:\n%s", stdout, stderr))
		Expect(readJSONLogEntriesFromText(stderr)).To(ContainElement(haveJSONLogEntry(string(cliMessageID(localization.MessageIDCLIErrorInvalidEnvironment)))))

	})
})

var _ = ginkgo.Describe("leafwiki main process", func() {
	ginkgo.It("reset admin password ignores dirty server only environment", ginkgo.Label("e2e"), func() {
		dataDir := filepath.Join(leafwikiTempDir(), "data")
		initWikidAdminUser(dataDir)

		stdout, stderr, err := runLeafwikiHelper([]string{
			"--data-dir", dataDir,
			"reset-admin-password",
		}, map[string]string{
			"LEAFWIKI_MAX_ASSET_UPLOAD_SIZE": "bad",
		})
		Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("reset-admin-password process error = %v, stderr=%q", err, stderr))
		Expect(readJSONLogEntriesFromText(stdout)).To(BeEmpty(), fmt.Sprintf("stdout contains log output: %q", stdout))

	})
})

var _ = ginkgo.Describe("leafwiki main process", func() {
	ginkgo.It("unknown command stays user facing and does not create log file", ginkgo.Label("e2e"), func() {
		dataDir := filepath.Join(leafwikiTempDir(), "data")
		stdout, stderr, err := runLeafwikiHelper([]string{
			"--data-dir", dataDir,
			"unknown-command",
		}, nil)
		Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("unknown command process error = %v, stderr=%q", err, stderr))
		Expect(readJSONLogEntriesFromText(stdout)).To(BeEmpty(), fmt.Sprintf("stdout contains log output: %q", stdout))
		Expect(stderr).To(BeEmpty(), fmt.Sprintf("stderr contains server log: %q", stderr))

		_, err = os.Stat(filepath.Join(dataDir, ".leafwiki", "logs", "leafwiki.log"))
		Expect(err).To(MatchError(os.ErrNotExist))

	})
})

var _ = ginkgo.Describe("leafwiki main process", func() {
	ginkgo.It("reset admin password keeps credentials on stdout only", ginkgo.Label("e2e"), func() {
		dataDir := filepath.Join(leafwikiTempDir(), "data")
		initWikidAdminUser(dataDir)

		stdout, stderr, err := runLeafwikiHelper([]string{
			"--data-dir", dataDir,
			"reset-admin-password",
		}, map[string]string{})
		Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("reset-admin-password process error = %v, stderr=%q", err, stderr))
		stdoutLines := strings.Split(strings.TrimSpace(stdout), "\n")
		Expect(stdoutLines).To(HaveLen(2), fmt.Sprintf("stdout = %q, want reset credentials", stdout))
		Expect(readJSONLogEntriesFromText(stdout)).To(BeEmpty(), fmt.Sprintf("stdout contains log output: %q", stdout))

		_, err = os.Stat(filepath.Join(dataDir, ".leafwiki", "logs", "leafwiki.log"))
		Expect(err).To(MatchError(os.ErrNotExist))

	})
})

var _ = ginkgo.Describe("leafwiki main process", func() {
	ginkgo.It("wikidfrontd reset admin password uses wikid auth store", ginkgo.Label("e2e"), func() {
		dataDir := filepath.Join(leafwikiTempDir(), "data")
		initWikidAdminUser(dataDir)

		stdout, stderr, err := runLeafwikiHelper([]string{
			"--data-dir", dataDir,
			"reset-admin-password",
		}, map[string]string{})
		Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("reset-admin-password process error = %v, stderr=%q", err, stderr))
		stdoutLines := strings.Split(strings.TrimSpace(stdout), "\n")
		Expect(stdoutLines).To(HaveLen(2), fmt.Sprintf("stdout = %q, want reset credentials", stdout))

		_, err = os.Stat(filepath.Join(dataDir, "users.db"))
		Expect(err).To(MatchError(os.ErrNotExist))

	})
})

var _ = ginkgo.Describe("leafwiki main process", func() {
	ginkgo.It("explicit stdout target writes server logs to stdout", ginkgo.Label("e2e"), func() {
		dataDir := filepath.Join(leafwikiTempDir(), "data")
		port := freeTCPPort()
		proc := startLeafwikiHelper([]string{
			"--disable-auth",
			"--data-dir", dataDir,
			"--host", "127.0.0.1",
			"--port", port,
			"--log-target", "stdout",
		}, nil)

		waitForLeafwikiReady(proc, port)
		globalDesc := waitForGlobalWikidDescriptor(dataDir)
		waitForFileContaining(proc.stdoutPath, leafwikiStartupLogMessage)
		waitForFileContaining(proc.stdoutPath, leafwikiHTTPRequestLogMessage)
		proc.stop()
		terminateProjectDaemonProcess(globalDesc.PID)
		waitForLeafwikiUnavailable(port)
		waitForProjectLocksReusable(dataDir, filepath.Join(dataDir, "root"), 15*time.Second)

		stdout := readFileString(proc.stdoutPath)
		Expect(readJSONLogEntriesFromText(stdout)).To(SatisfyAll(
			ContainElement(haveJSONLogEntry(leafwikiStartupLogMessage)),
			ContainElement(haveJSONLogEntry(leafwikiHTTPRequestLogMessage)),
		), fmt.Sprintf("stdout = %q, want server and request logs", stdout))

		_, err := os.Stat(filepath.Join(dataDir, ".leafwiki", "logs", "leafwiki.log"))
		Expect(err).To(MatchError(os.ErrNotExist))

	})
})

var _ = ginkgo.Describe("leafwiki main process", func() {
	ginkgo.It("disable request log suppresses process request log", ginkgo.Label("e2e"), func() {
		dataDir := filepath.Join(leafwikiTempDir(), "data")
		port := freeTCPPort()
		proc := startLeafwikiHelper([]string{
			"--disable-auth",
			"--data-dir", dataDir,
			"--host", "127.0.0.1",
			"--port", port,
			"--log-target", "stderr",
			"--disable-request-log",
		}, nil)

		waitForLeafwikiReady(proc, port)
		globalDesc := waitForGlobalWikidDescriptor(dataDir)
		waitForFileContaining(proc.stderrPath, leafwikiStartupLogMessage)
		proc.stop()
		terminateProjectDaemonProcess(globalDesc.PID)
		waitForLeafwikiUnavailable(port)
		waitForProjectLocksReusable(dataDir, filepath.Join(dataDir, "root"), 15*time.Second)

		stderr := readFileString(proc.stderrPath)
		Expect(readJSONLogEntriesFromText(stderr)).NotTo(ContainElement(haveJSONLogEntry(leafwikiHTTPRequestLogMessage)), fmt.Sprintf("stderr = %q, want request log suppressed", stderr))

		stdout := readFileString(proc.stdoutPath)
		Expect(stdout).To(BeEmpty(), fmt.Sprintf("stdout = %q, want empty for stderr target", stdout))

	})
})

var _ = ginkgo.Describe("workspace resolution", func() {
	ginkgo.It("defaults root dir under data dir", ginkgo.Label("unit"), func() {
		dataDir := filepath.Join(leafwikiTempDir(), "data")

		workspace := resolveWorkspaceForArgs([]string{"--data-dir=" + dataDir})
		Expect(workspace).To(SatisfyAll(
			HaveField("DataDir", Equal(dataDir)),
			HaveField("RootDir", Equal(filepath.Join(dataDir, "root"))),
		))

	})
})

var _ = ginkgo.Describe("workspace resolution", func() {
	ginkgo.It("env root dir overrides default", ginkgo.Label("unit"), func() {
		dataDir := filepath.Join(leafwikiTempDir(), "data")
		rootDir := filepath.Join(leafwikiTempDir(), "content")
		leafwikiSetenv("LEAFWIKI_ROOT_DIR", rootDir)

		workspace := resolveWorkspaceForArgs([]string{"--data-dir=" + dataDir})
		Expect(workspace.RootDir).To(Equal(rootDir), fmt.Sprintf("RootDir = %q, want env root %q", workspace.RootDir, rootDir))

	})
})

var _ = ginkgo.Describe("workspace resolution", func() {
	ginkgo.It("CLI root dir overrides env", ginkgo.Label("unit"), func() {
		dataDir := filepath.Join(leafwikiTempDir(), "data")
		envRootDir := filepath.Join(leafwikiTempDir(), "env-content")
		cliRootDir := filepath.Join(leafwikiTempDir(), "cli-content")
		leafwikiSetenv("LEAFWIKI_ROOT_DIR", envRootDir)

		workspace := resolveWorkspaceForArgs([]string{
			"--data-dir=" + dataDir,
			"--root-dir=" + cliRootDir,
		})
		Expect(workspace.RootDir).To(Equal(cliRootDir), fmt.Sprintf("RootDir = %q, want CLI root %q", workspace.RootDir, cliRootDir))

	})
})

var _ = ginkgo.Describe("workspace resolution", func() {
	ginkgo.It("normalizes paths", ginkgo.Label("unit"), func() {
		baseDir := leafwikiTempDir()
		dataDir := filepath.Join(baseDir, "data")
		rootDir := filepath.Join(baseDir, "content")

		workspace := resolveWorkspaceForArgs([]string{
			"--data-dir= " + dataDir + string(filepath.Separator) + ". ",
			"--root-dir= " + rootDir + string(filepath.Separator) + ". ",
		})
		Expect(workspace).To(SatisfyAll(
			HaveField("DataDir", Equal(dataDir)),
			HaveField("RootDir", Equal(rootDir)),
		))

	})
})

var _ = ginkgo.Describe("workspace validation", func() {
	ginkgo.It("rejects same data and root dir", ginkgo.Label("unit"), func() {
		dir := leafwikiTempDir()

		err := validateWorkspaceDirs(dir, filepath.Clean(filepath.Join(dir, ".")))
		Expect(err).To(MatchError(wiki.ErrWorkspaceRootDirEqualsDataDir), fmt.Sprintf("expected RootDir == DataDir to be rejected"))

	})
})

var _ = ginkgo.Describe("workspace validation", func() {
	ginkgo.It("rejects root dir containing data dir", ginkgo.Label("unit"), func() {
		rootDir := filepath.Join(leafwikiTempDir(), "wiki")
		dataDir := filepath.Join(rootDir, "data")

		err := validateWorkspaceDirs(dataDir, rootDir)
		Expect(err).To(MatchError(wiki.ErrWorkspaceRootDirContainsDataDir), fmt.Sprintf("expected RootDir containing DataDir to be rejected"))

	})
})
