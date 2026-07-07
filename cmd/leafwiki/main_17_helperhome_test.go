package main

import (
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"syscall"
	"time"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/onsi/gomega/types"
	"github.com/perber/wiki/internal/agenthooks"
	"github.com/perber/wiki/internal/locking"
	leaflogging "github.com/perber/wiki/internal/logging"
	"github.com/perber/wiki/internal/projectdaemon"
	"github.com/perber/wiki/internal/wiki"
	wikimcp "github.com/perber/wiki/internal/wiki/mcp"
	"github.com/perber/wiki/internal/wikid"
)

func leafwikiHelperHome(args []string) string {
	dataDir := ""
	for i := 0; i < len(args); i++ {
		if args[i] == "--data-dir" && i+1 < len(args) {
			dataDir = args[i+1]
			break
		}
		if value, ok := strings.CutPrefix(args[i], "--data-dir="); ok {
			dataDir = value
			break
		}
	}
	if strings.TrimSpace(dataDir) == "" {
		home, err := os.MkdirTemp("", "leafwiki-helper-home-*")
		if err == nil {
			return home
		}
		return os.TempDir()
	}
	return filepath.Join(filepath.Dir(filepath.Clean(dataDir)), "home")
}

func leafwikiHelperGlobalLayoutForDataDir(dataDir string) wikid.Layout {
	homeDir := filepath.Join(filepath.Dir(filepath.Clean(dataDir)), "home", ".leafwiki")
	rootDir := filepath.Join(homeDir, "root")
	canonicalHome, _, err := projectdaemon.CanonicalizeProject(homeDir, rootDir)
	if err == nil {
		homeDir = canonicalHome
	}
	return wikid.GlobalLayout(homeDir)
}

func waitForProjectDaemonDescriptor(dataDir string) *projectdaemon.Descriptor {
	ginkgo.GinkgoHelper()

	return waitForProjectDaemonDescriptorAtPath(projectdaemon.DescriptorPath(dataDir))
}

func waitForProjectDaemonDescriptorAtPath(path string) *projectdaemon.Descriptor {
	ginkgo.GinkgoHelper()

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
	Expect(lastErr).NotTo(HaveOccurred(), fmt.Sprintf("project daemon descriptor %q was not readable before timeout", path))
	return nil
}

func waitForGlobalWikidDescriptor(dataDir string) *projectdaemon.Descriptor {
	ginkgo.GinkgoHelper()

	layout := leafwikiHelperGlobalLayoutForDataDir(dataDir)
	path := projectdaemon.GlobalDescriptorPath(layout.RuntimeDir, projectdaemon.RoleWikid)
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
	Expect(lastErr).NotTo(HaveOccurred(), fmt.Sprintf("global wikid descriptor %q was not readable before timeout", path))
	return nil
}

func terminateProjectDaemonProcess(pid int) {
	ginkgo.GinkgoHelper()
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
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) && processExists(pid) {
		time.Sleep(50 * time.Millisecond)
	}
	if !processExists(pid) {
		return
	}
	_ = process.Kill()
	Eventually(func() processLivenessState {
		return classifyProcessExists(pid)
	}).WithTimeout(5 * time.Second).WithPolling(50 * time.Millisecond).Should(Equal(processNotRunning))
}

func processExists(pid int) bool {
	if pid <= 0 {
		return false
	}
	process, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	if runtime.GOOS == "windows" {
		return true
	}
	return process.Signal(syscall.Signal(0)) == nil
}

func leafwikiInternalProcessPID(pid int) bool {
	ginkgo.GinkgoHelper()
	if pid <= 0 || runtime.GOOS == "windows" {
		return false
	}
	raw, err := exec.Command("ps", "-p", fmt.Sprint(pid), "-o", "command=").Output()
	if err != nil {
		return false
	}
	command := string(raw)
	return strings.Contains(command, "--internal-project-daemon") ||
		strings.Contains(command, "--internal-runtime-role")
}

func findRoleHealth(roles []projectdaemon.RoleHealth, name projectdaemon.RoleName) (projectdaemon.RoleHealth, bool) {
	for _, role := range roles {
		if role.Name == name {
			return role, true
		}
	}
	return projectdaemon.RoleHealth{}, false
}

func waitForRuntimeCondition(timeout time.Duration, condition func() bool) {
	ginkgo.GinkgoHelper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(25 * time.Millisecond)
	}
	if condition() {
		return
	}
	Expect(condition()).To(BeTrue(), fmt.Sprintf("condition not met within %s", timeout))
}

func waitForFileContaining(path string, want string) {
	ginkgo.GinkgoHelper()

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
	Expect(last).To(ContainSubstring(want), fmt.Sprintf("%s did not contain %q before timeout", path, want))
}

func waitForFileRemoved(path string, timeout time.Duration) {
	ginkgo.GinkgoHelper()

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
	Expect(lastErr).To(MatchError(os.ErrNotExist), fmt.Sprintf("%s still existed before timeout", path))
}

func supportsGracefulProcessSignal() bool {
	return runtime.GOOS != "windows"
}

func signalLeafwikiProcess(process *os.Process) error {
	return process.Signal(os.Interrupt)
}

func waitForForegroundSignalHandler() {
	time.Sleep(200 * time.Millisecond)
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

func waitForLeafwikiReady(proc *leafwikiHelperProcess, port string) {
	ginkgo.GinkgoHelper()
	waitForLeafwikiReadyPathWithDiagnostics(port, "/api/health", readFileString(proc.stdoutPath), readFileString(proc.stderrPath))
	proc.ready = true
}

func waitForLeafwikiReadyAtBasePath(proc *leafwikiHelperProcess, port string, basePath string) {
	ginkgo.GinkgoHelper()
	waitForLeafwikiReadyPathWithDiagnostics(port, strings.TrimRight(basePath, "/")+"/api/health", readFileString(proc.stdoutPath), readFileString(proc.stderrPath))
	proc.ready = true
}

func waitForLeafwikiReadyWithDiagnostics(port string, stdout string, stderr string) {
	ginkgo.GinkgoHelper()
	waitForLeafwikiReadyPathWithDiagnostics(port, "/api/health", stdout, stderr)
}

func waitForLeafwikiReadyPathWithDiagnostics(port string, path string, stdout string, stderr string) {
	ginkgo.GinkgoHelper()
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
	Expect(lastErr).NotTo(HaveOccurred(), fmt.Sprintf("LeafWiki did not become ready at %s\nstdout:\n%s\nstderr:\n%s", url, stdout, stderr))
}

func waitForLeafwikiUnavailable(port string) {
	ginkgo.GinkgoHelper()

	waitForLeafwikiUnavailableWithin(port, 15*time.Second)
}

func waitForLeafwikiUnavailableWithin(port string, timeout time.Duration) {
	ginkgo.GinkgoHelper()

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
	Expect(url).To(BeEmpty(), fmt.Sprintf("LeafWiki stayed reachable at %s after shutdown", url))
}

func waitForProjectLocksReusable(dataDir string, rootDir string, timeout time.Duration) {
	ginkgo.GinkgoHelper()

	canonicalData, canonicalRoot, err := projectdaemon.CanonicalizeProject(dataDir, rootDir)
	Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("canonicalize project locks: %v", err))

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
	Expect(lastErr).NotTo(HaveOccurred(), fmt.Sprintf("project locks were not reusable before timeout"))
}

func federatedRuntimeToolNames() []string {
	names := append([]string{}, wikimcp.BaseToolNames()...)
	names = append(names, wikimcp.WorkspaceSyncToolNames()...)
	names = append(names, wikimcp.RevisionToolNames()...)
	return names
}

func listProcessHTTPMCPToolNames(endpoint string) []string {
	ginkgo.GinkgoHelper()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	client := sdkmcp.NewClient(&sdkmcp.Implementation{Name: "leafwiki-main-test", Version: "test"}, nil)
	session, err := client.Connect(ctx, &sdkmcp.StreamableClientTransport{
		Endpoint:             endpoint,
		HTTPClient:           &http.Client{Timeout: 5 * time.Second},
		DisableStandaloneSSE: true,
	}, nil)
	Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("connect HTTP MCP client: %v", err))

	defer closeBestEffort(session)

	var names []string
	cursor := ""
	for {
		result, err := session.ListTools(ctx, &sdkmcp.ListToolsParams{Cursor: cursor})
		Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("list HTTP MCP tools: %v", err))

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

func matchToolNames(want []string) types.GomegaMatcher {
	sortedWant := append([]string{}, want...)
	sort.Strings(sortedWant)
	expected := make([]any, 0, len(sortedWant))
	for _, name := range sortedWant {
		expected = append(expected, name)
	}
	return ConsistOf(expected...)
}

func freeTCPPort() string {
	ginkgo.GinkgoHelper()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("find free port: %v", err))

	defer func() {
		Expect(listener.Close()).To(Succeed(), fmt.Sprintf("close free port listener: %v", err))
	}()
	return fmt.Sprintf("%d", listener.Addr().(*net.TCPAddr).Port)
}

func readFileString(path string) string {
	ginkgo.GinkgoHelper()

	raw, err := os.ReadFile(path)
	Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("read %s: %v", path, err))

	return string(raw)
}

func haveFileMode(want os.FileMode) types.GomegaMatcher {
	return WithTransform(func(path string) os.FileMode {
		ginkgo.GinkgoHelper()
		info, err := os.Stat(path)
		Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("stat %s: %v", path, err))
		return info.Mode().Perm()
	}, Equal(want))
}

func haveLoggingConfig(target leaflogging.Target, filePath types.GomegaMatcher) types.GomegaMatcher {
	return SatisfyAll(
		HaveField("Target", Equal(target)),
		HaveField("FilePath", filePath),
	)
}

func findLeafwikiDaemonStartupConfigContaining(marker string) string {
	ginkgo.GinkgoHelper()

	matches, err := filepath.Glob(filepath.Join(os.TempDir(), "leafwiki-project-daemon-*.json"))
	Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("glob daemon startup configs: %v", err))

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

func agentHookSessionHash(provider agenthooks.ProviderID, rawSessionID string) string {
	payload, err := json.Marshal(struct {
		HookEventName agenthooks.AgentEventName `json:"hook_event_name"`
		SessionID     agenthooks.SessionID      `json:"session_id"`
	}{
		HookEventName: agenthooks.AgentEventSessionStart,
		SessionID:     agenthooks.SessionIDFromString(rawSessionID),
	})
	Expect(err).NotTo(HaveOccurred())
	event, err := normalizedAgentHookEventResult(provider, payload, time.Now())
	Expect(err).To(Succeed())
	Expect(event.SessionIDHash).NotTo(BeEmpty())
	return event.SessionIDHash
}

func agentHookProviderCLIArg(provider agenthooks.ProviderID) string {
	switch provider {
	case agenthooks.ProviderClaude:
		return "claude"
	case agenthooks.ProviderCursor:
		return "cursor"
	case agenthooks.ProviderCodex:
		return "codex"
	case agenthooks.ProviderUnknown:
		return "unknown"
	default:
		return ""
	}
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
		HTTPRemoteUserHeader: "Remote-User",
		DaemonIdleTimeout:    0,
	}
}
