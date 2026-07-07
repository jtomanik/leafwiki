package main

import (
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/onsi/gomega/types"
	"github.com/perber/wiki/internal/agenthooks"
	"github.com/perber/wiki/internal/projectdaemon"
)

func runLeafWikiHelperProcessForTest() bool {
	if os.Getenv("GO_WANT_LEAFWIKI_HELPER_PROCESS") != "1" {
		return false
	}

	args := []string{}
	for i, arg := range os.Args {
		if arg == "--" {
			args = os.Args[i+1:]
			break
		}
	}
	if os.Getenv("LEAFWIKI_TEST_RUNTIME_READY_WRONG_ROLE") == "1" && len(args) == 2 && args[0] == "--internal-runtime-role" {
		raw, err := os.ReadFile(args[1])
		if err != nil {
			fmt.Fprintf(os.Stderr, "read startup config: %v\n", err)
			os.Exit(2)
		}
		var startup internalRuntimeRoleStartupConfig
		if err := json.Unmarshal(raw, &startup); err != nil {
			fmt.Fprintf(os.Stderr, "decode startup config: %v\n", err)
			os.Exit(2)
		}
		if pidPath := os.Getenv("LEAFWIKI_TEST_RUNTIME_READY_WRONG_ROLE_PID_PATH"); pidPath != "" {
			if err := os.WriteFile(pidPath, []byte(strconv.Itoa(os.Getpid())), 0o600); err != nil {
				fmt.Fprintf(os.Stderr, "write pid file: %v\n", err)
				os.Exit(2)
			}
		}
		wrongRole := projectdaemon.RoleFrontd
		if startup.Role == projectdaemon.RoleFrontd {
			wrongRole = projectdaemon.RoleWorkspaced
		}
		if err := writeInternalRuntimeRoleReady(startup.ReadyPath, internalRuntimeRoleReady{
			Role: wrongRole,
			PID:  os.Getpid(),
			URL:  "http://127.0.0.1:1",
		}); err != nil {
			fmt.Fprintf(os.Stderr, "write wrong ready file: %v\n", err)
			os.Exit(2)
		}
		signals := make(chan os.Signal, 1)
		signal.Notify(signals, os.Interrupt, syscall.SIGTERM)
		<-signals
		os.Exit(0)
	}
	os.Args = append([]string{"leafwiki"}, args...)
	flag.CommandLine = flag.NewFlagSet(os.Args[0], flag.ExitOnError)
	main()
	os.Exit(0)
	return true
}

type leafwikiHelperProcess struct {
	cmd        *exec.Cmd
	cancel     context.CancelFunc
	stdoutPath string
	stderrPath string
	ready      bool
	stopped    bool
}

func startLeafwikiHelper(args []string, env map[string]string) *leafwikiHelperProcess {
	return startLeafwikiHelperWithStdin(args, env, nil)
}

func startLeafwikiHelperWithStdin(args []string, env map[string]string, stdin io.Reader) *leafwikiHelperProcess {
	return startLeafwikiHelperWithOptions(args, env, stdin, leafwikiHelperStartOptions{})
}

func startLeafwikiHelperInProcessGroup(args []string, env map[string]string) *leafwikiHelperProcess {
	return startLeafwikiHelperWithOptions(args, env, nil, leafwikiHelperStartOptions{processGroup: true})
}

type leafwikiHelperStartOptions struct {
	processGroup bool
}

func startLeafwikiHelperWithOptions(args []string, env map[string]string, stdin io.Reader, opts leafwikiHelperStartOptions) *leafwikiHelperProcess {
	ginkgo.GinkgoHelper()

	ctx, cancel := context.WithCancel(context.Background())
	stdoutPath := filepath.Join(leafwikiTempDir(), "leafwiki.stdout")
	stderrPath := filepath.Join(leafwikiTempDir(), "leafwiki.stderr")
	stdout, err := os.Create(stdoutPath)
	Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("create stdout file: %v", err))

	stderr, err := os.Create(stderrPath)
	Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("create stderr file: %v", err))

	cmdArgs := append([]string{"-test.run=TestLeafWikiSuite", "--"}, args...)
	cmd := exec.CommandContext(ctx, os.Args[0], cmdArgs...)
	cmd.Env = leafwikiHelperEnv(args, env)
	cmd.Stdin = stdin
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	if opts.processGroup {
		configureLeafwikiHelperProcessGroup(cmd)
	}
	err = cmd.Start()
	if err != nil {
		cancel()
		_ = stdout.Close()
		_ = stderr.Close()
	}
	Expect(err).NotTo(HaveOccurred())
	Expect(stdout.Close()).To(Succeed(), fmt.Sprintf("close parent stdout file: %v", err))
	Expect(stderr.Close()).To(Succeed(), fmt.Sprintf("close parent stderr file: %v", err))

	proc := &leafwikiHelperProcess{
		cmd:        cmd,
		cancel:     cancel,
		stdoutPath: stdoutPath,
		stderrPath: stderrPath,
	}
	ginkgo.DeferCleanup(func() {
		proc.stop()
	})
	return proc
}

func startLeafwikiHelperWithStdinPipe(args []string, env map[string]string) (*leafwikiHelperProcess, io.WriteCloser) {
	ginkgo.GinkgoHelper()

	ctx, cancel := context.WithCancel(context.Background())
	stdoutPath := filepath.Join(leafwikiTempDir(), "leafwiki.stdout")
	stderrPath := filepath.Join(leafwikiTempDir(), "leafwiki.stderr")
	stdout, err := os.Create(stdoutPath)
	Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("create stdout file: %v", err))

	stderr, err := os.Create(stderrPath)
	Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("create stderr file: %v", err))

	cmdArgs := append([]string{"-test.run=TestLeafWikiSuite", "--"}, args...)
	cmd := exec.CommandContext(ctx, os.Args[0], cmdArgs...)
	cmd.Env = leafwikiHelperEnv(args, env)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		cancel()
		_ = stdout.Close()
		_ = stderr.Close()
	}
	Expect(err).NotTo(HaveOccurred())
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	err = cmd.Start()
	if err != nil {
		cancel()
		_ = stdout.Close()
		_ = stderr.Close()
		_ = stdin.Close()
	}
	Expect(err).NotTo(HaveOccurred())
	Expect(stdout.Close()).To(Succeed(), fmt.Sprintf("close parent stdout file: %v", err))
	Expect(stderr.Close()).To(Succeed(), fmt.Sprintf("close parent stderr file: %v", err))

	proc := &leafwikiHelperProcess{
		cmd:        cmd,
		cancel:     cancel,
		stdoutPath: stdoutPath,
		stderrPath: stderrPath,
	}
	ginkgo.DeferCleanup(func() {
		_ = stdin.Close()
		proc.stop()
	})
	return proc, stdin
}

func (p *leafwikiHelperProcess) stop() {
	ginkgo.GinkgoHelper()
	if p.stopped {
		return
	}
	p.stopped = true
	done := make(chan error, 1)
	go func() {
		done <- p.cmd.Wait()
	}()
	if p.ready && supportsGracefulProcessSignal() && p.cmd.Process != nil {
		_ = signalLeafwikiProcess(p.cmd.Process)
		select {
		case err := <-done:
			Expect(leafwikiHelperStopResult{Err: err}).To(matchLeafwikiHelperStopResult(), fmt.Sprintf("wait leafwiki helper after graceful signal\nstdout:\n%s\nstderr:\n%s", readFileString(p.stdoutPath), readFileString(p.stderrPath)))
			return
		case <-time.After(2 * time.Second):
		}
	}
	p.cancel()
	var stopErr error
	Eventually(done).WithTimeout(5*time.Second).Should(Receive(&stopErr), fmt.Sprintf("leafwiki helper did not stop\nstdout:\n%s\nstderr:\n%s", readFileString(p.stdoutPath), readFileString(p.stderrPath)))
	Expect(leafwikiHelperStopResult{Err: stopErr}).To(matchLeafwikiHelperStopResult(), fmt.Sprintf("leafwiki helper did not stop cleanly\nstdout:\n%s\nstderr:\n%s", readFileString(p.stdoutPath), readFileString(p.stderrPath)))
}

type leafwikiHelperStopResult struct {
	Err error
}

func matchLeafwikiHelperStopResult() types.GomegaMatcher {
	return Satisfy(func(result leafwikiHelperStopResult) bool {
		if result.Err == nil {
			return true
		}
		var exitErr *exec.ExitError
		if errors.As(result.Err, &exitErr) && exitErr.ProcessState.ExitCode() == -1 {
			return true
		}
		if errors.Is(result.Err, context.Canceled) {
			return true
		}
		return false
	})
}

func (p *leafwikiHelperProcess) waitForExit() {
	ginkgo.GinkgoHelper()
	if p.stopped {
		return
	}
	done := make(chan error, 1)
	go func() {
		done <- p.cmd.Wait()
	}()
	Eventually(done).WithTimeout(15*time.Second).Should(Receive(Succeed()), fmt.Sprintf("leafwiki helper did not exit\nstdout:\n%s\nstderr:\n%s", readFileString(p.stdoutPath), readFileString(p.stderrPath)))
	p.stopped = true
	p.cancel()
}

func runLeafwikiHelper(args []string, env map[string]string) (string, string, error) {
	ginkgo.GinkgoHelper()

	return runLeafwikiHelperWithTimeout(args, env, 30*time.Second)
}

func runLeafwikiHelperWithTimeout(args []string, env map[string]string, timeout time.Duration) (string, string, error) {
	ginkgo.GinkgoHelper()

	return runLeafwikiHelperWithInputAndTimeout(args, env, "", timeout)
}

func runLeafwikiHelperWithInputAndTimeout(args []string, env map[string]string, stdin string, timeout time.Duration) (string, string, error) {
	ginkgo.GinkgoHelper()

	cmdArgs := append([]string{"-test.run=TestLeafWikiSuite", "--"}, args...)
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, os.Args[0], cmdArgs...)
	cmd.Env = leafwikiHelperEnv(args, env)
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

type leafwikiNativeStdioResponse struct {
	ID     json.RawMessage `json:"id"`
	Result json.RawMessage `json:"result"`
	Error  json.RawMessage `json:"error"`
}

func haveNativeStdioToolListResponse(id int, toolName agenthooks.AgentToolName) types.GomegaMatcher {
	ginkgo.GinkgoHelper()
	return WithTransform(func(stdout string) []agenthooks.AgentToolName {
		return nativeStdioToolNames(stdout, id)
	}, ContainElement(toolName))
}

func haveNativeStdioJSONTextResponse(id int, matcher types.GomegaMatcher) types.GomegaMatcher {
	ginkgo.GinkgoHelper()
	return WithTransform(func(stdout string) (map[string]any, error) {
		var payload map[string]any
		if err := json.Unmarshal([]byte(nativeStdioResultText(stdout, id)), &payload); err != nil {
			return nil, err
		}
		return payload, nil
	}, matcher)
}

func nativeStdioToolNames(stdout string, id int) []agenthooks.AgentToolName {
	response := nativeStdioResponseByID(stdout, id)
	if response == nil {
		return nil
	}
	var result struct {
		Tools []struct {
			Name string `json:"name"`
		} `json:"tools"`
	}
	if err := json.Unmarshal(response.Result, &result); err != nil {
		return nil
	}
	names := make([]agenthooks.AgentToolName, 0, len(result.Tools))
	for _, tool := range result.Tools {
		names = append(names, agenthooks.AgentToolNameFromString(tool.Name))
	}
	return names
}

func nativeStdioResultText(stdout string, id int) string {
	response := nativeStdioResponseByID(stdout, id)
	if response == nil {
		return ""
	}
	var result struct {
		Content []struct {
			Text string `json:"text"`
		} `json:"content"`
	}
	if err := json.Unmarshal(response.Result, &result); err != nil {
		return ""
	}
	parts := make([]string, 0, len(result.Content))
	for _, content := range result.Content {
		parts = append(parts, content.Text)
	}
	return strings.Join(parts, "\n")
}

func nativeStdioResponseByID(stdout string, id int) *leafwikiNativeStdioResponse {
	for _, response := range nativeStdioResponses(stdout) {
		if bytes.Equal(bytes.TrimSpace(response.ID), []byte(strconv.Itoa(id))) {
			return &response
		}
	}
	return nil
}

func nativeStdioResponses(stdout string) []leafwikiNativeStdioResponse {
	var responses []leafwikiNativeStdioResponse
	for _, line := range strings.Split(stdout, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var response leafwikiNativeStdioResponse
		if err := json.Unmarshal([]byte(line), &response); err == nil {
			responses = append(responses, response)
		}
	}
	return responses
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
		EnableWorkspaceSync:     true,
		EnableLinkRefactor:      true,
		EnableHTTPRemoteUser:    false,
		HTTPRemoteUserHeader:    "X-Remote-User",
		TrustedProxyIPs:         "",
		HTTPRemoteUserLogoutURL: "",
		DisableRequestLog:       false,
		DaemonIdleTimeout:       "10m0s",
	}
}

func leafwikiHelperEnv(args []string, overrides map[string]string) []string {
	env := []string{}
	for _, entry := range os.Environ() {
		if strings.HasPrefix(entry, "LEAFWIKI_") || strings.HasPrefix(entry, "GO_WANT_LEAFWIKI_HELPER_PROCESS=") {
			continue
		}
		if strings.HasPrefix(entry, "HOME=") {
			continue
		}
		env = append(env, entry)
	}
	env = append(env, "GO_WANT_LEAFWIKI_HELPER_PROCESS=1")
	if _, ok := overrides["HOME"]; !ok {
		env = append(env, "HOME="+leafwikiHelperHome(args))
	}
	if _, ok := overrides["LEAFWIKI_DAEMON_IDLE_TIMEOUT"]; !ok {
		env = append(env, "LEAFWIKI_DAEMON_IDLE_TIMEOUT=0")
	}
	for key, value := range overrides {
		env = append(env, key+"="+value)
	}
	return env
}
