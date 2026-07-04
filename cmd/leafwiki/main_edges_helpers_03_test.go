package main

import (
	"context"
	"encoding/json"
	"flag"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	sdkauth "github.com/modelcontextprotocol/go-sdk/auth"
	sdkjsonrpc "github.com/modelcontextprotocol/go-sdk/jsonrpc"
	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/gcustom"
	"github.com/onsi/gomega/types"

	"github.com/perber/wiki/internal/projectdaemon"
	"github.com/perber/wiki/internal/wikid"
)

func waitForLeafwikiContextCancellation(ctx context.Context) error {
	ginkgo.GinkgoHelper()

	deadline := time.Now().Add(3 * time.Second)
	for ctx.Err() == nil {
		if time.Now().After(deadline) {
			return context.DeadlineExceeded
		}
		time.Sleep(10 * time.Millisecond)
	}
	return ctx.Err()
}

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

	Eventually(func() projectdaemon.RoleState {
		return runtime.supervisor.State(role).State
	}).WithTimeout(time.Second).WithPolling(time.Millisecond).Should(Equal(state))
}

func waitForLeafwikiDescriptor(path string) *projectdaemon.Descriptor {
	ginkgo.GinkgoHelper()

	var descriptor *projectdaemon.Descriptor
	Eventually(func(g Gomega) {
		desc, err := projectdaemon.ReadTrustedDescriptor(path)
		g.Expect(err).NotTo(HaveOccurred())
		descriptor = desc
	}).WithTimeout(3 * time.Second).WithPolling(10 * time.Millisecond).Should(Succeed())
	return descriptor
}

func waitForLeafwikiRuntimeReady(path string) internalRuntimeRoleReady {
	ginkgo.GinkgoHelper()

	var ready internalRuntimeRoleReady
	Eventually(func(g Gomega) {
		raw, err := os.ReadFile(path)
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(strings.TrimSpace(string(raw))).NotTo(BeEmpty())
		g.Expect(json.Unmarshal(raw, &ready)).To(Succeed())
	}).WithTimeout(3 * time.Second).WithPolling(10 * time.Millisecond).Should(Succeed())
	return ready
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

func blockingPathForLeafwikiTest() string {
	ginkgo.GinkgoHelper()

	path := filepath.Join(leafwikiTempDir(), "not-a-dir")
	Expect(os.WriteFile(path, []byte("x"), 0o600)).To(Succeed())
	return path
}

type leafwikiExitPanic int

func PanicWithLeafwikiExit(code int) types.GomegaMatcher {
	ginkgo.GinkgoHelper()

	return gcustom.MakeMatcher(func(fn func()) (bool, error) {
		previous := leafwikiExit
		leafwikiExit = func(got int) {
			panic(leafwikiExitPanic(got))
		}
		defer func() {
			leafwikiExit = previous
		}()

		return PanicWith(leafwikiExitPanic(code)).Match(fn)
	}).WithTemplate("Expected:\n{{.FormattedActual}}\n{{.To}} panic with leafwiki exit code\n{{format .Data 1}}", code)
}
