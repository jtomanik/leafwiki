package main

import (
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"bytes"
	"context"
	"errors"
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
	"github.com/perber/wiki/internal/localization"
	"github.com/perber/wiki/internal/locking"
	"github.com/perber/wiki/internal/projectdaemon"
)

var _ = ginkgo.Describe("leafwiki main process", func() {
	ginkgo.It("agent hook malformed JSON fails open", ginkgo.Label("e2e"), func() {
		baseDir := leafwikiTempDir()
		dataDir := filepath.Join(baseDir, "data")
		rootDir := filepath.Join(baseDir, "content")
		port := freeTCPPort()

		stdout, stderr, err := runLeafwikiHelperWithInputAndTimeout([]string{
			"agent-hook", "codex",
			"--disable-auth",
			"--data-dir", dataDir,
			"--root-dir", rootDir,
			"--host", "127.0.0.1",
			"--port", port,
			"--log-target", "stderr",
		}, nil, "{", 5*time.Second)
		Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("agent-hook malformed JSON err = %v\nstdout:\n%s\nstderr:\n%s", err, stdout, stderr))
		Expect(stdout).To(MatchCodexAgentHookAllowResponse())
		Expect(readJSONLogEntriesFromText(stderr)).NotTo(ContainElement(HaveKey("payload")), fmt.Sprintf("stderr leaked raw malformed payload: %s", stderr))

		_, err = os.Stat(projectdaemon.DescriptorPath(dataDir))
		Expect(err).To(MatchError(os.ErrNotExist))

	})
})

var _ = ginkgo.Describe("leafwiki main process", func() {
	ginkgo.It("agent hook starts daemon and records presence", ginkgo.Label("e2e"), func() {
		baseDir := leafwikiTempDir()
		dataDir := filepath.Join(baseDir, "data")
		rootDir := filepath.Join(baseDir, "content")
		port := freeTCPPort()
		sensitivePrompt := "private prompt"
		payload := fmt.Sprintf(`{"hook_event_name":"SessionStart","session_id":"raw-codex-session","model":"gpt-5.4","source":"startup","prompt":%q}`, sensitivePrompt)

		stdout, stderr, err := runLeafwikiHelperWithInputAndTimeout([]string{
			"agent-hook", "codex",
			"--disable-auth",
			"--data-dir", dataDir,
			"--root-dir", rootDir,
			"--host", "127.0.0.1",
			"--port", port,
			"--log-target", "stderr",
		}, nil, payload, 10*time.Second)
		Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("agent-hook valid payload err = %v\nstdout:\n%s\nstderr:\n%s", err, stdout, stderr))
		Expect(stdout).To(MatchCodexAgentHookAllowResponse())
		Expect(readJSONLogEntriesFromText(stderr)).NotTo(ContainElement(SatisfyAny(
			HaveKey("session_id"),
			HaveKey("prompt"),
		)), fmt.Sprintf("stderr leaked hook payload data: %s", stderr))

		desc := waitForProjectDaemonDescriptor(dataDir)
		ginkgo.DeferCleanup(func() {
			terminateProjectDaemonProcess(desc.PID)
		})
		client := projectdaemon.NewClient(desc.ControlURL, desc.ControlToken)
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		sessions, err := client.ListAgentPresence(ctx)
		Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("ListAgentPresence failed: %v", err))
		Expect(sessions).To(HaveLen(1), fmt.Sprintf("presence session count = %d, want 1: %#v", len(sessions), sessions))
		Expect(sessions).To(ContainElement(SatisfyAll(
			HaveField("SessionIDHash", Equal(agentHookSessionHash(agenthooks.ProviderCodex, "raw-codex-session"))),
			HaveField("Provider", Equal(agenthooks.ProviderCodex)),
			HaveField("LastEvent", Equal(agenthooks.AgentEventSessionStart)),
			HaveField("Model", Equal("gpt-5.4")),
			HaveField("Source", Equal(agenthooks.AgentSourceStartup)),
		)))
		Expect(fmt.Sprintf("%#v", sessions)).NotTo(ContainSubstring("raw-codex-session"))
		Expect(fmt.Sprintf("%#v", sessions)).NotTo(ContainSubstring(sensitivePrompt))

	})
})

var _ = ginkgo.Describe("leafwiki main process", func() {
	ginkgo.It("agent hook replaces stale descriptor and fails open", ginkgo.Label("e2e"), func() {
		baseDir := leafwikiTempDir()
		leafwikiSetenv("HOME", filepath.Join(baseDir, "home"))
		dataDir := filepath.Join(baseDir, "data")
		rootDir := filepath.Join(baseDir, "content")
		Expect(os.MkdirAll(dataDir, 0o755)).To(Succeed())
		Expect(os.MkdirAll(rootDir, 0o755)).To(Succeed())
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
		ginkgo.DeferCleanup(staleControl.Close)

		cfg := testRuntimeConfig(dataDir, rootDir, freeTCPPort(), mcpTransports{}, true)
		ownerCfg, err := daemonRequestConfigForRuntime(cfg)
		Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("daemonRequestConfigForRuntime: %v", err))

		hash, err := projectdaemon.ConfigHash(ownerCfg)
		Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("ConfigHash: %v", err))

		descriptorPath := projectdaemon.DescriptorPath(ownerCfg.DataDir)
		Expect(projectdaemon.WriteDescriptorAtomic(descriptorPath, &projectdaemon.Descriptor{
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
		})).To(Succeed())
		sensitivePrompt := "private prompt"
		payload := fmt.Sprintf(`{"hook_event_name":"SessionStart","session_id":"stale-descriptor-secret","prompt":%q}`, sensitivePrompt)
		stdout, stderr, err := runLeafwikiHelperWithInputAndTimeout([]string{
			"agent-hook", "codex",
			"--disable-auth",
			"--data-dir", dataDir,
			"--root-dir", rootDir,
			"--host", "127.0.0.1",
			"--port", ownerCfg.Port,
			"--log-target", "stderr",
		}, nil, payload, 10*time.Second)
		Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("agent-hook stale descriptor should fail open, got %v\nstdout:\n%s\nstderr:\n%s", err, stdout, stderr))
		Expect(stdout).To(MatchCodexAgentHookAllowResponse())
		Expect(readJSONLogEntriesFromText(stderr)).NotTo(ContainElement(SatisfyAny(
			HaveKey("session_id"),
			HaveKey("prompt"),
		)), fmt.Sprintf("stderr leaked hook payload data: %s", stderr))

		replaced := readFileString(descriptorPath)
		replacedDesc, err := projectdaemon.ReadTrustedDescriptor(descriptorPath)
		Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("read replaced descriptor: %v", err))
		Expect(replacedDesc).To(SatisfyAll(
			Not(HaveField("ControlURL", Equal(staleControl.URL))),
			Not(HaveField("ControlToken", Equal("stale-token"))),
		), fmt.Sprintf("descriptor was not replaced:\n%s", replaced))

		desc := waitForProjectDaemonDescriptor(dataDir)
		ginkgo.DeferCleanup(func() {
			terminateProjectDaemonProcess(desc.PID)
		})
		Consistently(received).WithTimeout(25 * time.Millisecond).ShouldNot(Receive())

	})
})

var _ = ginkgo.Describe("agent-hook command", func() {
	ginkgo.It("recovers panic and allows", ginkgo.Label("unit"), func() {
		var stdout bytes.Buffer
		err := runAgentHookCommand(context.Background(), testRuntimeConfig(leafwikiTempDir(), filepath.Join(leafwikiTempDir(), "root"), freeTCPPort(), mcpTransports{}, true), agenthooks.ProviderCodex, panicReader{}, &stdout)
		Expect(stdout.String()).To(MatchCodexAgentHookAllowResponse())
		_ = err

	})
})

var _ = ginkgo.Describe("agent-hook command", func() {
	ginkgo.It("read error fails open", ginkgo.Label("unit"), func() {
		var stdout bytes.Buffer
		readErr := errors.New("synthetic read failure")
		err := runAgentHookCommand(
			context.Background(),
			testRuntimeConfig(leafwikiTempDir(), filepath.Join(leafwikiTempDir(), "root"), freeTCPPort(), mcpTransports{}, true),
			agenthooks.ProviderClaude,
			errorReader{err: readErr},
			&stdout,
		)
		Expect(err).To(MatchError(readErr))
		Expect(stdout.String()).To(MatchAgentHookAllowResponse(agenthooks.ProviderClaude))

	})
})

var _ = ginkgo.Describe("leafwiki main process", func() {
	ginkgo.It("agent hook pre dispatch failures fail open", ginkgo.Label("e2e"), func() {
		baseDir := leafwikiTempDir()
		sameDir := filepath.Join(baseDir, "same")
		payload := `{"hook_event_name":"SessionStart","session_id":"pre-dispatch-secret"}`

		stdout, stderr, err := runLeafwikiHelperWithInputAndTimeout([]string{
			"agent-hook", "codex",
			"--disable-auth",
			"--data-dir", sameDir,
			"--root-dir", sameDir,
			"--log-target", "stderr",
		}, nil, payload, 5*time.Second)
		Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("agent-hook invalid workspace should fail open, got %v\nstdout:\n%s\nstderr:\n%s", err, stdout, stderr))
		Expect(stdout).To(MatchCodexAgentHookAllowResponse())
		Expect(readJSONLogEntriesFromText(stderr)).NotTo(ContainElement(HaveKey("session_id")), fmt.Sprintf("stderr leaked hook payload data: %s", stderr))

	})
})

var _ = ginkgo.Describe("leafwiki main process", func() {
	ginkgo.It("agent hook flag first pre dispatch failures fail open", ginkgo.Label("e2e"), func() {
		baseDir := leafwikiTempDir()
		sameDir := filepath.Join(baseDir, "same")
		payload := `{"hook_event_name":"SessionStart","session_id":"flag-first-secret"}`

		stdout, stderr, err := runLeafwikiHelperWithInputAndTimeout([]string{
			"--disable-auth",
			"--data-dir", sameDir,
			"--root-dir", sameDir,
			"--log-target", "stderr",
			"agent-hook", "codex",
		}, nil, payload, 5*time.Second)
		Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("flag-first agent-hook invalid workspace should fail open, got %v\nstdout:\n%s\nstderr:\n%s", err, stdout, stderr))
		Expect(stdout).To(MatchCodexAgentHookAllowResponse())
		Expect(readJSONLogEntriesFromText(stderr)).NotTo(ContainElement(HaveKey("session_id")), fmt.Sprintf("stderr leaked hook payload data: %s", stderr))

	})
})

var _ = ginkgo.Describe("leafwiki main process", func() {
	ginkgo.It("non hook flag value named agent hook does not fail open", ginkgo.Label("e2e"), func() {
		baseDir := leafwikiTempDir()
		sameDir := filepath.Join(baseDir, "same")
		Expect(os.MkdirAll(sameDir, 0o755)).To(Succeed())

		stdout, stderr, err := runLeafwikiHelperWithInputAndTimeout([]string{
			"--log-file", "agent-hook",
			"--disable-auth",
			"--data-dir", sameDir,
			"--root-dir", sameDir,
			"--log-target", "stderr",
		}, nil, `{"session_id":"should-not-be-hook"}`, 5*time.Second)
		Expect(err).To(MatchProcessExitError(), fmt.Sprintf("non-hook startup unexpectedly succeeded\nstdout:\n%s\nstderr:\n%s", stdout, stderr))
		Expect(stdout).NotTo(MatchCodexAgentHookAllowResponse())
		Expect(readJSONLogEntriesFromText(stderr)).To(ContainElement(haveJSONLogEntry(localization.MessageIDCLIErrorInvalidWorkspaceConfig)), fmt.Sprintf("stderr = %q, want workspace configuration error", stderr))

	})
})

var _ = ginkgo.Describe("leafwiki main process", func() {
	ginkgo.It("non hook flag value named agent hook parse error does not fail open", ginkgo.Label("e2e"), func() {
		stdout, stderr, err := runLeafwikiHelperWithInputAndTimeout([]string{
			"--log-file", "agent-hook",
			"--not-a-real-flag",
		}, nil, `{"session_id":"should-not-be-hook"}`, 5*time.Second)
		Expect(err).To(MatchProcessExitError(), fmt.Sprintf("non-hook parse error unexpectedly succeeded\nstdout:\n%s\nstderr:\n%s", stdout, stderr))
		Expect(stdout).NotTo(MatchCodexAgentHookAllowResponse())

	})
})

var _ = ginkgo.Describe("leafwiki main process", func() {
	ginkgo.It("agent hook flag first parse errors fail open", ginkgo.Label("e2e"), func() {
		payload := `{"hook_event_name":"SessionStart","session_id":"flag-parse-secret"}`

		stdout, stderr, err := runLeafwikiHelperWithInputAndTimeout([]string{
			"--not-a-real-flag",
			"agent-hook", "codex",
		}, nil, payload, 5*time.Second)
		Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("flag-first agent-hook parse error should fail open, got %v\nstdout:\n%s\nstderr:\n%s", err, stdout, stderr))
		Expect(stdout).To(MatchCodexAgentHookAllowResponse())
		Expect(readJSONLogEntriesFromText(stderr)).NotTo(ContainElement(HaveKey("session_id")), fmt.Sprintf("stderr leaked hook payload data: %s", stderr))

	})
})

var _ = ginkgo.Describe("leafwiki main process", func() {
	ginkgo.It("agent hook flag value named config does not disable fail open", ginkgo.Label("e2e"), func() {
		payload := `{"hook_event_name":"SessionStart","session_id":"flag-value-config-secret"}`

		stdout, stderr, err := runLeafwikiHelperWithInputAndTimeout([]string{
			"--data-dir", "--config",
			"--not-a-real-flag",
			"agent-hook", "codex",
		}, nil, payload, 5*time.Second)
		Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("non-config hook parse error should fail open, got %v\nstdout:\n%s\nstderr:\n%s", err, stdout, stderr))
		Expect(stdout).To(MatchCodexAgentHookAllowResponse())
		Expect(readJSONLogEntriesFromText(stderr)).NotTo(ContainElement(HaveKey("session_id")), fmt.Sprintf("stderr leaked hook payload data: %s", stderr))

	})
})

var _ = ginkgo.Describe("leafwiki main process", func() {
	ginkgo.It("agent hook malformed config flag does not disable fail open", ginkgo.Label("e2e"), func() {
		payload := `{"hook_event_name":"SessionStart","session_id":"malformed-config-flag-secret"}`

		stdout, stderr, err := runLeafwikiHelperWithInputAndTimeout([]string{
			"---config",
			"--not-a-real-flag",
			"agent-hook", "codex",
		}, nil, payload, 5*time.Second)
		Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("malformed non-config hook parse error should fail open, got %v\nstdout:\n%s\nstderr:\n%s", err, stdout, stderr))
		Expect(stdout).To(MatchCodexAgentHookAllowResponse())
		Expect(readJSONLogEntriesFromText(stderr)).NotTo(ContainElement(HaveKey("session_id")), fmt.Sprintf("stderr leaked hook payload data: %s", stderr))

	})
})

var _ = ginkgo.Describe("leafwiki main process", func() {
	ginkgo.It("agent hook provider allow responses fail open", ginkgo.Label("e2e"), func() {
		tests := []struct {
			name       string
			provider   agenthooks.ProviderID
			payload    string
			wantStdout types.GomegaMatcher
		}{
			{name: "claude malformed", provider: agenthooks.ProviderClaude, payload: "{", wantStdout: MatchAgentHookAllowResponse(agenthooks.ProviderClaude)},
			{name: "cursor malformed", provider: agenthooks.ProviderCursor, payload: "{", wantStdout: MatchAgentHookAllowResponse(agenthooks.ProviderCursor)},
			{name: "unknown provider", provider: agenthooks.ProviderUnknown, payload: `{"hook_event_name":"SessionStart","session_id":"unknown-secret"}`, wantStdout: BeEmpty()},
		}
		for _, tt := range tests {
			func() {
				_ = tt.name
				baseDir := leafwikiTempDir()
				dataDir := filepath.Join(baseDir, "data")
				rootDir := filepath.Join(baseDir, "content")
				stdout, stderr, err := runLeafwikiHelperWithInputAndTimeout([]string{
					"agent-hook", agentHookProviderCLIArg(tt.provider),
					"--disable-auth",
					"--data-dir", dataDir,
					"--root-dir", rootDir,
					"--host", "127.0.0.1",
					"--port", freeTCPPort(),
					"--log-target", "stderr",
				}, nil, tt.payload, 5*time.Second)
				Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("agent-hook should fail open, got %v\nstdout:\n%s\nstderr:\n%s", err, stdout, stderr))
				Expect(stdout).To(tt.wantStdout)
				Expect(readJSONLogEntriesFromText(stderr)).NotTo(ContainElement(HaveKey("session_id")), fmt.Sprintf("stderr leaked hook payload data: %s", stderr))

			}()
		}

	})
})

var _ = ginkgo.Describe("agent-hook command", func() {
	ginkgo.It("oversized payload fails open", ginkgo.Label("unit"), func() {
		var stdout bytes.Buffer
		baseDir := leafwikiTempDir()
		err := runAgentHookCommand(
			context.Background(),
			testRuntimeConfig(filepath.Join(baseDir, "data"), filepath.Join(baseDir, "root"), freeTCPPort(), mcpTransports{}, true),
			agenthooks.ProviderCursor,
			strings.NewReader(strings.Repeat("x", agentHookMaxPayloadBytes+1)),
			&stdout,
		)
		Expect(stdout.String()).To(MatchAgentHookAllowResponse(agenthooks.ProviderCursor))
		_ = err

	})
})

var _ = ginkgo.Describe("agent-hook command", func() {
	ginkgo.It("locked project fails open", ginkgo.Label("integration"), func() {
		baseDir := leafwikiTempDir()
		dataDir := filepath.Join(baseDir, "data")
		rootDir := filepath.Join(baseDir, "content")
		Expect(os.MkdirAll(dataDir, 0o755)).To(Succeed())
		Expect(os.MkdirAll(rootDir, 0o755)).To(Succeed())
		canonicalData, canonicalRoot, err := projectdaemon.CanonicalizeProject(dataDir, rootDir)
		Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("canonicalize project: %v", err))

		dataLock, err := locking.AcquireDataDirLock(canonicalData)
		Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("acquire data lock: %v", err))

		defer releaseRuntimeLockBestEffort(dataLock)
		rootLock, err := locking.AcquireRootDirLock(canonicalRoot)
		Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("acquire root lock: %v", err))

		defer releaseRuntimeLockBestEffort(rootLock)

		var stdout bytes.Buffer
		err = runAgentHookCommand(
			context.Background(),
			testRuntimeConfig(dataDir, rootDir, freeTCPPort(), mcpTransports{}, true),
			agenthooks.ProviderCodex,
			strings.NewReader(`{"hook_event_name":"SessionStart","session_id":"locked-secret"}`),
			&stdout,
		)
		Expect(stdout.String()).To(MatchCodexAgentHookAllowResponse())

		Expect(err).To(SatisfyAny(
			MatchProjectDaemonConfigMismatch(),
			MatchError(errProjectLockedNoAttachableDaemon),
			Satisfy(locking.IsLockHeld),
		))

	})
})

var _ = ginkgo.Describe("agent-hook command", func() {
	ginkgo.It("control record failures fail open", ginkgo.Label("integration"), func() {
		tests := []struct {
			name          string
			recordHandler func(http.ResponseWriter, *http.Request)
			parentTimeout time.Duration
			wantErr       types.GomegaMatcher
		}{
			{name: "control 401", recordHandler: func(w http.ResponseWriter, _ *http.Request) {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
			}, wantErr: MatchProjectDaemonControlStatus(http.StatusUnauthorized)},
			{name: "control 400", recordHandler: func(w http.ResponseWriter, _ *http.Request) {
				http.Error(w, "bad event", http.StatusBadRequest)
			}, wantErr: MatchProjectDaemonControlStatus(http.StatusBadRequest)},
			{name: "control 500", recordHandler: func(w http.ResponseWriter, _ *http.Request) {
				http.Error(w, "boom", http.StatusInternalServerError)
			}, wantErr: MatchProjectDaemonControlStatus(http.StatusInternalServerError)},
			{name: "control timeout", parentTimeout: 50 * time.Millisecond, recordHandler: func(w http.ResponseWriter, _ *http.Request) {
				time.Sleep(250 * time.Millisecond)
				w.WriteHeader(http.StatusNoContent)
			}, wantErr: MatchError(context.DeadlineExceeded)},
		}
		for _, tt := range tests {
			func() {
				_ = tt.name
				cfg, cleanup := testRuntimeConfigWithHealthyControlDescriptor(tt.recordHandler)
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
				Expect(stdout.String()).To(MatchCodexAgentHookAllowResponse())

				Expect(err).To(SatisfyAny(tt.wantErr, MatchError(errProjectLockedNoAttachableDaemon)))

			}()
		}

	})
})

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
