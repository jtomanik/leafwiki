package main

import (
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/onsi/gomega/types"
	"github.com/perber/wiki/internal/agenthooks"
	coreauth "github.com/perber/wiki/internal/core/auth"
	"github.com/perber/wiki/internal/localization"
	"github.com/perber/wiki/internal/projectdaemon"
)

var _ = ginkgo.Describe("daemon STDIO bridge HTTP client", func() {
	ginkgo.It("has no full request timeout", ginkgo.Label("integration"), func() {
		client := daemonStdioBridgeHTTPClient(daemonStdioBridge{
			ControlToken: "control-token",
			APIKey:       "stdio-api-key",
		})
		Expect(client).To(haveDaemonStdioBridgeHTTPClient("control-token", "stdio-api-key"))

	})
})

var _ = ginkgo.Describe("daemon STDIO bridge HTTP client", func() {
	ginkgo.It("refreshes actor context before forwarding", ginkgo.Label("integration"), func() {
		verifyCalls := 0
		revoked := false
		now := time.Date(2026, 6, 17, 12, 0, 0, 0, time.UTC)
		control := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			if req.URL.Path != "/__leafwiki/actor-context" {
				http.NotFound(w, req)
				return
			}
			if req.Header.Get(projectdaemon.ControlTokenHeader) != "control-token" {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
			Expect(req.Header).To(HaveKeyWithValue(http.CanonicalHeaderKey("Authorization"), ContainElement("Bearer stdio-api-key")))
			Expect(req.Header).To(HaveKeyWithValue(http.CanonicalHeaderKey(projectdaemon.WorkspaceIDHeader), ContainElement("workspace-a")))

			verifyCalls++
			if revoked {
				http.Error(w, "access denied", http.StatusUnauthorized)
				return
			}
			writeRuntimeJSON(w, map[string]any{"actor": projectdaemon.ActorContext{
				Version:     1,
				Issuer:      projectdaemon.ActorContextIssuerWikid,
				Subject:     "user:editor",
				Username:    "editor",
				Role:        coreauth.RoleEditor,
				WorkspaceID: newFixtureWorkspaceID("workspace-a"),
				AuthMethod:  "api_key",
				IssuedAt:    now,
				ExpiresAt:   now.Add(5 * time.Minute),
			}})
		}))
		ginkgo.DeferCleanup(control.Close)

		upstreamCalls := 0
		upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			upstreamCalls++
			Expect(req.Header).To(HaveKeyWithValue(http.CanonicalHeaderKey(projectdaemon.ControlTokenHeader), ContainElement("private-token")))
			Expect(req.Header).To(HaveKeyWithValue(http.CanonicalHeaderKey("Authorization"), ContainElement("Bearer stdio-api-key")))

			actor, err := projectdaemon.DecodeActorContext(req.Header.Get(projectdaemon.ActorContextHeader), projectdaemon.ActorContextValidation{
				Now:         now.Add(time.Minute),
				WorkspaceID: newFixtureWorkspaceID("workspace-a"),
			})
			Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("private MCP actor context invalid: %v", err))
			Expect(actor).To(SatisfyAll(
				HaveActorSubjectForUser(newFixtureUserID("editor")),
				HaveField("Role", Equal(coreauth.RoleEditor)),
			), fmt.Sprintf("private MCP actor context = %#v, want refreshed editor actor", actor))

			w.WriteHeader(http.StatusNoContent)
		}))
		ginkgo.DeferCleanup(upstream.Close)

		client := daemonStdioBridgeHTTPClient(daemonStdioBridge{
			ControlToken:     "private-token",
			AuthControlURL:   control.URL,
			AuthControlToken: "control-token",
			WorkspaceID:      newFixtureWorkspaceID("workspace-a"),
			APIKey:           "stdio-api-key",
			ActorContext:     "stale-actor-context",
		})

		resp, err := client.Get(upstream.URL + "/mcp")
		Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("first bridge request failed: %v", err))

		_ = resp.Body.Close()
		Expect(resp).To(HaveHTTPStatus(http.StatusNoContent))
		Expect(verifyCalls).To(Equal(1), fmt.Sprintf("first bridge verification calls = %d, want 1", verifyCalls))
		Expect(upstreamCalls).To(Equal(1), fmt.Sprintf("first bridge upstream calls = %d, want 1", upstreamCalls))

		revoked = true
		resp, err = client.Get(upstream.URL + "/mcp")
		if resp != nil {
			_ = resp.Body.Close()
		}
		Expect(err).To(MatchWikidPrivateEndpoint(http.StatusUnauthorized, newFixtureErrorCode("")))
		Expect(verifyCalls).To(Equal(2), fmt.Sprintf("revoked bridge verification calls = %d, want 2", verifyCalls))
		Expect(upstreamCalls).To(Equal(1), fmt.Sprintf("revoked bridge upstream calls = %d, want 1", upstreamCalls))

	})
})

var _ = ginkgo.Describe("daemon STDIO actor context", func() {
	ginkgo.It("preserves workspace grant denial", ginkgo.Label("integration"), func() {
		control := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			if req.URL.Path != "/__leafwiki/actor-context" {
				http.NotFound(w, req)
				return
			}
			writeRuntimeError(w, http.StatusForbidden, runtimeErrorCodeWorkspaceGrantDenied)
		}))
		ginkgo.DeferCleanup(control.Close)

		transport := stdioActorContextRoundTripper{
			AuthControlURL:   control.URL,
			AuthControlToken: "control-token",
			WorkspaceID:      newFixtureWorkspaceID("workspace-b"),
			APIKey:           "valid-but-ungranted-key",
		}
		_, err := transport.actorContext(httptest.NewRequest(http.MethodGet, "/mcp", nil))
		Expect(err).To(MatchWikidPrivateEndpoint(http.StatusForbidden, runtimeErrorCodeWorkspaceGrantDenied))

	})
})

var _ = ginkgo.Describe("daemon heartbeat", func() {
	ginkgo.It("returns control errors", ginkgo.Label("unit"), func() {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			http.Error(w, "session not found", http.StatusNotFound)
		}))
		ginkgo.DeferCleanup(server.Close)

		client := projectdaemon.NewClient(server.URL, "control-token")
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		err := runDaemonHeartbeat(ctx, client, newFixtureSessionID("missing-session"), 10*time.Millisecond)
		Expect(err).To(MatchProjectDaemonControlStatus(http.StatusNotFound))

	})
})

var _ = ginkgo.Describe("idle shutdown activity tracking", func() {
	ginkgo.It("ignores stale zero notification with active session", ginkgo.Label("unit"), func() {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		currentCount := 1
		callback := idleShutdownCallback(ctx, cancel, 0, func() int {
			return currentCount
		})

		callback(1)
		callback(0)
		Consistently(ctx.Done()).WithTimeout(25 * time.Millisecond).ShouldNot(BeClosed())

		currentCount = 0
		callback(0)
		Eventually(ctx.Done()).WithTimeout(time.Second).Should(BeClosed())

	})
})

var _ = ginkgo.Describe("leafwiki command behavior", func() {
	ginkgo.It("cancel if no session after startup grace waits for first session", ginkgo.Label("unit"), func() {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		registry := projectdaemon.NewSessionRegistry(time.Second, nil)

		done := make(chan struct{})
		go func() {
			cancelIfNoSessionAfterStartupGrace(ctx, cancel, registry, 25*time.Millisecond)
			close(done)
		}()

		id, err := registry.Register()
		Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("register first session: %v", err))

		registry.Release(id)
		Consistently(ctx.Done()).WithTimeout(25 * time.Millisecond).ShouldNot(BeClosed())
		Eventually(done).WithTimeout(100 * time.Millisecond).Should(BeClosed())

	})
})

var _ = ginkgo.Describe("leafwiki command behavior", func() {
	ginkgo.It("cancel if no session after startup grace cancels when no session registers", ginkgo.Label("unit"), func() {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		registry := projectdaemon.NewSessionRegistry(time.Second, nil)
		go cancelIfNoSessionAfterStartupGrace(ctx, cancel, registry, 10*time.Millisecond)

		Eventually(ctx.Done()).WithTimeout(250 * time.Millisecond).Should(BeClosed())

	})
})

var _ = ginkgo.Describe("project daemon activity count", func() {
	ginkgo.It("combines sessions and agent presence", ginkgo.Label("unit"), func() {
		sessions := projectdaemon.NewSessionRegistry(time.Second, nil)
		presence := projectdaemon.NewAgentPresenceRegistry(time.Minute, nil)

		Expect(projectDaemonActivityCount(sessions, presence)).To(BeZero())
		handle, err := sessions.Register()
		Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("register session: %v", err))

		presence.Record(agenthooks.Event{
			Provider:      agenthooks.ProviderCodex,
			SessionIDHash: agentHookSessionHash(agenthooks.ProviderCodex, "codex"),
			EventName:     newFixtureAgentEventName("SessionStart"),
			SeenAt:        time.Now(),
		})
		Expect(projectDaemonActivityCount(sessions, presence)).To(Equal(2))
		sessions.Release(handle)
		Expect(projectDaemonActivityCount(sessions, presence)).To(Equal(1))

	})
})

var _ = ginkgo.Describe("leafwiki command behavior", func() {
	ginkgo.It("cancel if no activity after startup grace waits for first agent presence", ginkgo.Label("unit"), func() {
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
			EventName:     newFixtureAgentEventName("SessionStart"),
			SeenAt:        time.Now(),
		})
		Consistently(ctx.Done()).WithTimeout(25 * time.Millisecond).ShouldNot(BeClosed())
		Eventually(done).WithTimeout(100 * time.Millisecond).Should(BeClosed())

	})
})

var _ = ginkgo.Describe("leafwiki command behavior", func() {
	ginkgo.It("cancel if no activity after startup grace ignores missing agent end", ginkgo.Label("unit"), func() {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		sessions := projectdaemon.NewSessionRegistry(time.Second, nil)
		presence := projectdaemon.NewAgentPresenceRegistry(time.Minute, nil)
		event, err := normalizedAgentHookEventResult(
			agenthooks.ProviderClaude,
			[]byte(`{"hook_event_name":"SessionEnd","session_id":"ended-before-start"}`),
			time.Now(),
		)
		Expect(err).To(Succeed())
		Expect(event).To(SatisfyAll(
			HaveField("Provider", Equal(agenthooks.ProviderClaude)),
			HaveField("EventName", Equal(agenthooks.AgentEventSessionEnd)),
			HaveField("SessionIDHash", Not(BeEmpty())),
		), fmt.Sprintf("normalized event = %#v", event))

		presence.Record(event)

		go cancelIfNoActivityAfterStartupGrace(ctx, cancel, sessions, presence, 10*time.Millisecond)

		Eventually(ctx.Done()).WithTimeout(250 * time.Millisecond).Should(BeClosed())

	})
})

var _ = ginkgo.Describe("daemon owner environment", func() {
	ginkgo.It("omits session and bootstrap secrets", ginkgo.Label("unit"), func() {
		leafwikiSetenv("LEAFWIKI_MCP_API_KEY", "lwk_secret")
		leafwikiSetenv("LEAFWIKI_RUN_MCP_API_KEY", "lwk_run_secret")
		leafwikiSetenv("LEAFWIKI_JWT_SECRET", "jwt-secret")
		leafwikiSetenv("LEAFWIKI_RUN_MCP_JWT_SECRET", "run-jwt-secret")
		leafwikiSetenv("LEAFWIKI_ADMIN_PASSWORD", "admin-password")
		leafwikiSetenv("LEAFWIKI_RUN_MCP_ADMIN_PASSWORD", "run-admin-password")
		leafwikiSetenv("LEAFWIKI_BASE_PATH", "/wiki")

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
			Expect(joined).NotTo(ContainSubstring(unexpected), fmt.Sprintf("daemon owner env retained secret %q:\n%s", unexpected, joined))

		}
		Expect(joined).To(ContainSubstring("LEAFWIKI_BASE_PATH=/wiki"), fmt.Sprintf("daemon owner env lost non-secret LeafWiki setting:\n%s", joined))

	})
})

var _ = ginkgo.Describe("leafwiki main process", func() {
	ginkgo.It("native stdiosigterm releases data dir lock", ginkgo.Label("e2e"), func() {
		if !supportsGracefulProcessSignal() {
			ginkgo.Skip(fmt.Sprint("SIGTERM-style graceful process signaling is not available on this platform"))
		}

		baseDir := leafwikiTempDir()
		dataDir := filepath.Join(baseDir, "data")
		rootDir := filepath.Join(baseDir, "content")
		firstPort := freeTCPPort()
		first, firstStdinWriter := startLeafwikiHelperWithStdinPipe([]string{
			"--mcp=stdio",
			"--disable-auth",
			"--data-dir", dataDir,
			"--root-dir", rootDir,
			"--host", "127.0.0.1",
			"--port", firstPort,
			"--log-target", "stderr",
		}, map[string]string{})
		waitForLeafwikiReady(first, firstPort)

		waitForForegroundSignalHandler()
		Expect(signalLeafwikiProcess(first.cmd.Process)).To(Succeed())
		first.waitForExit()
		_ = firstStdinWriter.Close()
		waitForLeafwikiUnavailable(firstPort)

		secondStdinReader, secondStdinWriter := io.Pipe()
		secondPort := freeTCPPort()
		second := startLeafwikiHelperWithStdin([]string{
			"--mcp=stdio",
			"--disable-auth",
			"--data-dir", dataDir,
			"--root-dir", rootDir,
			"--host", "127.0.0.1",
			"--port", secondPort,
			"--log-target", "stderr",
		}, map[string]string{}, secondStdinReader)
		waitForLeafwikiReady(second, secondPort)

		Expect(secondStdinWriter.Close()).To(Succeed())
		second.waitForExit()

	})
})

var _ = ginkgo.Describe("leafwiki main process", func() {
	ginkgo.It("foreground server signal leaves detached owner until idle timeout", ginkgo.Label("e2e"), func() {
		if !supportsProcessGroupSignal() {
			ginkgo.Skip(fmt.Sprint("process-group signaling is not available on this platform"))
		}

		var ownerPID int
		ginkgo.DeferCleanup(func() {
			terminateProjectDaemonProcess(ownerPID)
		})
		baseDir := leafwikiTempDir()
		dataDir := filepath.Join(baseDir, "data")
		rootDir := filepath.Join(baseDir, "content")
		port := freeTCPPort()
		proc := startLeafwikiHelperInProcessGroup([]string{
			"--disable-auth",
			"--data-dir", dataDir,
			"--root-dir", rootDir,
			"--host", "127.0.0.1",
			"--port", port,
			"--log-target", "stderr",
		}, map[string]string{"LEAFWIKI_DAEMON_IDLE_TIMEOUT": "1s"})
		ownerPID = waitForProjectDaemonDescriptor(dataDir).PID

		waitForLeafwikiReady(proc, port)
		waitForForegroundSignalHandler()
		Expect(signalLeafwikiProcessGroup(proc.cmd.Process)).To(Succeed())
		proc.waitForExit()
		waitForLeafwikiReady(proc, port)
		waitForLeafwikiUnavailableWithin(port, 15*time.Second)

	})
})

var _ = ginkgo.Describe("leafwiki main process", func() {
	ginkgo.It("file target startup failure also reaches stderr", ginkgo.Label("e2e"), func() {
		dataDir := filepath.Join(leafwikiTempDir(), "data")
		stdout, stderr, err := runLeafwikiHelper([]string{
			"--data-dir", dataDir,
			"--admin-password", "admin-password",
		}, nil)
		Expect(err).To(MatchProcessExitError(), fmt.Sprintf("expected missing JWT secret to exit non-zero"))
		Expect(stdout).To(BeEmpty(), fmt.Sprintf("stdout = %q, want empty", stdout))

		startupFailureLogEntry := func() types.GomegaMatcher {
			return SatisfyAny(
				haveJSONLogEntry(string(cliMessageID(localization.MessageIDCLIErrorLeafWikiStartupFailed))),
				SatisfyAll(
					HaveKeyWithValue("level", "ERROR"),
					HaveKeyWithValue("source", HaveKeyWithValue("function", "github.com/perber/wiki/cmd/leafwiki.logStartupValidationFailure")),
				),
			)
		}
		Expect(readJSONLogEntriesFromText(stderr)).To(ContainElement(startupFailureLogEntry()))
		Expect(readJSONLogEntries(filepath.Join(dataDir, ".leafwiki", "logs", "leafwiki.log"))).To(ContainElement(startupFailureLogEntry()))

	})
})

var _ = ginkgo.Describe("leafwiki main process", func() {
	ginkgo.It("help stays on stdout", ginkgo.Label("e2e"), func() {
		stdout, stderr, err := runLeafwikiHelper([]string{"--help"}, nil)
		Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("help process error = %v, stderr=%q", err, stderr))

		Expect(readJSONLogEntriesFromText(stdout)).To(BeEmpty(), fmt.Sprintf("stdout contains log output: %q", stdout))
		Expect(readJSONLogEntriesFromText(stderr)).To(BeEmpty(), fmt.Sprintf("stderr contains log output: %q", stderr))

	})
})

var _ = ginkgo.Describe("leafwiki main process", func() {
	ginkgo.It("daemon help stays on stdout", ginkgo.Label("e2e"), func() {
		stdout, stderr, err := runLeafwikiHelper([]string{"daemon", "--help"}, nil)
		Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("daemon help process error = %v, stderr=%q", err, stderr))

		Expect(readJSONLogEntriesFromText(stdout)).To(BeEmpty(), fmt.Sprintf("stdout contains log output: %q", stdout))
		Expect(readJSONLogEntriesFromText(stderr)).To(BeEmpty(), fmt.Sprintf("stderr contains log output: %q", stderr))

	})
})

var _ = ginkgo.Describe("leafwiki main process", func() {
	ginkgo.It("daemon requires default service config", ginkgo.Label("e2e"), func() {
		homeDir := leafwikiTempDir()
		stdout, stderr, err := runLeafwikiHelper([]string{"daemon"}, map[string]string{
			"HOME": homeDir,
		})
		Expect(err).To(MatchProcessExitError(), fmt.Sprintf("daemon without service config unexpectedly succeeded\nstdout:\n%s\nstderr:\n%s", stdout, stderr))
		Expect(stdout).To(BeEmpty(), fmt.Sprintf("stdout = %q, want empty", stdout))

		Expect(readJSONLogEntriesFromText(stderr)).To(ContainElement(haveJSONLogEntry(localization.MessageIDCLIErrorServiceConfigRequired)), fmt.Sprintf("stderr = %q, want required service config log", stderr))

	})
})

var _ = ginkgo.Describe("leafwiki main process", func() {
	ginkgo.It("daemon rejects invalid default service config", ginkgo.Label("e2e"), func() {
		homeDir := leafwikiTempDir()
		serviceDir := filepath.Join(homeDir, ".leafwiki")
		Expect(os.MkdirAll(serviceDir, 0o755)).To(Succeed())
		configPath := filepath.Join(serviceDir, "leafwiki.yml")
		writeTestConfig(configPath, ":\n")

		stdout, stderr, err := runLeafwikiHelper([]string{"daemon"}, map[string]string{
			"HOME": homeDir,
		})
		Expect(err).To(MatchProcessExitError(), fmt.Sprintf("daemon with invalid service config unexpectedly succeeded\nstdout:\n%s\nstderr:\n%s", stdout, stderr))
		Expect(stdout).To(BeEmpty(), fmt.Sprintf("stdout = %q, want empty", stdout))
		Expect(readJSONLogEntriesFromText(stderr)).To(ContainElement(haveJSONLogEntry(localization.MessageIDCLIErrorInvalidServiceConfigFile)), fmt.Sprintf("stderr = %q, want invalid service config log for %s", stderr, configPath))

	})
})

var _ = ginkgo.Describe("leafwiki main process", func() {
	ginkgo.It("daemon rejects internal service config keys", ginkgo.Label("e2e"), func() {
		homeDir := leafwikiTempDir()
		serviceDir := filepath.Join(homeDir, ".leafwiki")
		Expect(os.MkdirAll(serviceDir, 0o755)).To(Succeed())
		configPath := filepath.Join(serviceDir, "leafwiki.yml")
		writeTestConfig(configPath, "internal-project-daemon: /tmp/startup.json\n")

		stdout, stderr, err := runLeafwikiHelper([]string{"daemon"}, map[string]string{
			"HOME": homeDir,
		})
		Expect(err).To(MatchProcessExitError(), fmt.Sprintf("daemon with internal service config key unexpectedly succeeded\nstdout:\n%s\nstderr:\n%s", stdout, stderr))
		Expect(stdout).To(BeEmpty(), fmt.Sprintf("stdout = %q, want empty", stdout))
		Expect(readJSONLogEntriesFromText(stderr)).To(ContainElement(haveJSONLogEntry(localization.MessageIDCLIErrorInvalidServiceConfigFile)), fmt.Sprintf("stderr = %q, want invalid internal service config log", stderr))

	})
})
