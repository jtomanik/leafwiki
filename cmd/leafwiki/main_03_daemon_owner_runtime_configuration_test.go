package main

import (
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	coreauth "github.com/perber/wiki/internal/core/auth"
	sharederrors "github.com/perber/wiki/internal/core/shared/errors"
	"github.com/perber/wiki/internal/frontd"
	leaflogging "github.com/perber/wiki/internal/logging"
	"github.com/perber/wiki/internal/projectdaemon"
	testmatchers "github.com/perber/wiki/internal/test_utils/matchers"
	"github.com/perber/wiki/internal/wiki"
	"github.com/perber/wiki/internal/wikid"
	"github.com/perber/wiki/internal/workspaceid"
)

var _ = ginkgo.Describe("daemon owner runtime configuration", func() {
	ginkgo.It("clears home markdown link root prefix", ginkgo.Label("unit"), func() {
		baseDir := leafwikiTempDir()
		cfg := testRuntimeConfig(
			filepath.Join(baseDir, "workspace-data"),
			filepath.Join(baseDir, "workspace-root"),
			freeTCPPort(),
			mcpTransports{Stdio: true},
			true,
		)
		cfg.RuntimeStack = projectdaemon.RuntimeStackWikidFrontd
		cfg.MarkdownLinkRootPrefix = "/docs"

		ownerCfg, err := daemonOwnerRuntimeConfig(cfg)
		Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("daemonOwnerRuntimeConfig failed: %v", err))
		Expect(ownerCfg.Workspace.ID).To(Equal(wikid.HomeWorkspaceID), fmt.Sprintf("owner workspace ID = %q, want home", ownerCfg.Workspace.ID))
		Expect(ownerCfg.MarkdownLinkRootPrefix).To(BeEmpty())

	})
})

var _ = ginkgo.Describe("federated workspace manager", func() {
	ginkgo.It("uses semantic workspace ID state", ginkgo.Label("integration"), func() {
		manager := &federatedWorkspaceManager{}
		manager.processes = map[workspaceid.WorkspaceID]*internalRuntimeRoleProcess{}
		manager.descriptors = map[workspaceid.WorkspaceID][]string{}
		manager.workspaces = map[workspaceid.WorkspaceID]wikid.WorkspaceRecord{}

		var _ map[workspaceid.WorkspaceID]*internalRuntimeRoleProcess = manager.processes
		var _ map[workspaceid.WorkspaceID][]string = manager.descriptors
		var _ map[workspaceid.WorkspaceID]wikid.WorkspaceRecord = manager.workspaces
		var _ func(workspaceid.WorkspaceID, wikid.WorkspaceRecord) (wikid.WorkspaceStatus, error) = manager.ensureWorkspace
		var _ func(workspaceid.WorkspaceID, *internalRuntimeRoleProcess) = manager.monitorWorkspaceProcess
		var _ func(*coreauth.User, string, leafwikiRuntimeConfig, workspaceid.WorkspaceID, wikid.GrantRole) (projectdaemon.ActorContext, error) = actorContextForWorkspaceGrant

	})
})

var _ = ginkgo.Describe("home workspace status synchronization", func() {
	ginkgo.It("tracks workspaced role", ginkgo.Label("integration"), func() {
		supervisor := wikid.NewWorkspaceSupervisor(wikid.WorkspaceSupervisorOptions{})
		now := time.Now().UTC()

		syncHomeWorkspaceStatus(supervisor, []projectdaemon.RoleHealth{{
			Name:      projectdaemon.RoleWorkspaced,
			State:     projectdaemon.RoleStateReady,
			PID:       123,
			URL:       "http://127.0.0.1:41001",
			UpdatedAt: now,
		}})
		Expect(supervisor.Status(wikid.HomeWorkspaceID)).To(SatisfyAll(
			HaveField("State", Equal(wikid.WorkspaceStateRunning)),
			HaveField("PID", Equal(123)),
			HaveField("URL", Not(BeEmpty())),
		))

		syncHomeWorkspaceStatus(supervisor, []projectdaemon.RoleHealth{{
			Name:      projectdaemon.RoleWorkspaced,
			State:     projectdaemon.RoleStateRestarting,
			PID:       123,
			URL:       "http://127.0.0.1:41001",
			Error:     "exit status 2",
			UpdatedAt: now.Add(time.Second),
		}})
		Expect(supervisor.Status(wikid.HomeWorkspaceID)).To(SatisfyAll(
			HaveField("State", Equal(wikid.WorkspaceStateRestarting)),
			HaveField("Error", Equal("exit status 2")),
		))

		syncHomeWorkspaceStatus(supervisor, []projectdaemon.RoleHealth{{
			Name:      projectdaemon.RoleWorkspaced,
			State:     projectdaemon.RoleStateCrashed,
			Error:     "restart limit",
			UpdatedAt: now.Add(2 * time.Second),
		}})
		Expect(supervisor.Status(wikid.HomeWorkspaceID)).To(SatisfyAll(
			HaveField("State", Equal(wikid.WorkspaceStateCrashed)),
			HaveField("Error", Equal("restart limit")),
		))

	})
})

var _ = ginkgo.Describe("project daemon descriptor health", func() {
	ginkgo.It("probes workspaced private MCP", ginkgo.Label("integration"), func() {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			if req.Header.Get(projectdaemon.ControlTokenHeader) != "private-token" {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
			w.WriteHeader(http.StatusNotFound)
		}))
		defer server.Close()

		desc := &projectdaemon.Descriptor{
			Role:            projectdaemon.RoleWorkspaced,
			PID:             os.Getpid(),
			PrivateMCPURL:   server.URL + "/mcp",
			PrivateMCPToken: "private-token",
		}
		Expect(desc).To(BeHealthyProjectDaemonDescriptor(context.Background()), fmt.Sprintf("descriptor = %#v, want private MCP endpoint probe to pass", desc))

		wrongTokenDesc := *desc
		wrongTokenDesc.PrivateMCPToken = "wrong-token"
		Expect(&wrongTokenDesc).To(BeUnhealthyProjectDaemonDescriptor(context.Background()), fmt.Sprintf("descriptor = %#v, want wrong private MCP token rejected", wrongTokenDesc))

		server.Close()
		Expect(desc).To(BeUnhealthyProjectDaemonDescriptor(context.Background()), fmt.Sprintf("descriptor = %#v, want closed private MCP endpoint rejected", desc))

	})
})

var _ = ginkgo.Describe("wikid actor context handler", func() {
	ginkgo.It("resolves OAuth bearer for MCP", ginkgo.Label("integration"), func() {
		w := newFrontdActorTestWiki()
		defer closeBestEffort(w)
		cfg := leafwikiRuntimeConfig{
			Workspace:           wiki.Workspace{ID: "current"},
			Host:                "127.0.0.1",
			AllowInsecure:       true,
			AccessTokenTimeout:  15 * time.Minute,
			RefreshTokenTimeout: 7 * 24 * time.Hour,
			MCPTransports:       mcpTransports{HTTP: true},
		}
		opts, err := routerOptionsForRuntime(cfg, w, "", true, "127.0.0.1")
		Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("routerOptionsForRuntime failed: %v", err))

		router := frontd.NewRouter(w, opts)
		token := issueOAuthAccessTokenForTest(router)

		req := httptest.NewRequest(http.MethodPost, "/__leafwiki/actor-context", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("X-LeafWiki-Original-Path", "/mcp")
		rec := httptest.NewRecorder()
		handleWikidActorContext(rec, req, w, cfg, nil, nil)
		Expect(rec).To(HaveHTTPStatus(http.StatusOK))

		var body struct {
			Actor projectdaemon.ActorContext `json:"actor"`
		}
		Expect(json.Unmarshal(rec.Body.Bytes(), &body)).To(Succeed(), fmt.Sprintf("decode actor context: %v", err))
		Expect(body.Actor).To(SatisfyAll(
			HaveField("Username", Equal("admin")),
			HaveField("AuthMethod", Equal("oauth")),
		), fmt.Sprintf("actor = %#v, want OAuth admin actor", body.Actor))

	})
})

var _ = ginkgo.Describe("wikid actor context handler", func() {
	ginkgo.It("returns structured workspace grant denial", ginkgo.Label("integration"), func() {
		w := newFrontdActorTestWiki()
		defer closeBestEffort(w)
		cfg := leafwikiRuntimeConfig{
			Workspace:     wiki.Workspace{ID: "current"},
			PublicAccess:  true,
			AllowInsecure: true,
			MCPTransports: mcpTransports{HTTP: true},
		}
		layout := wikid.GlobalLayout(filepath.Join(leafwikiTempDir(), ".leafwiki"))
		_, err := wikid.NewRegistryService(wikid.NewRegistryStore(layout.DBPath), layout).BootstrapHome()
		Expect(err).NotTo(HaveOccurred())
		grants := wikid.NewGrantStore(layout.DBPath)
		req := httptest.NewRequest(http.MethodPost, "/__leafwiki/actor-context", nil)
		req.Header.Set("X-LeafWiki-Original-Method", http.MethodGet)
		req.Header.Set("X-LeafWiki-Original-Path", "/mcp")
		rec := httptest.NewRecorder()

		handleWikidActorContext(rec, req, w, cfg, nil, grants)
		Expect(rec).To(HaveHTTPStatus(http.StatusForbidden))

		Expect(rec.Body.Bytes()).To(testmatchers.HaveStructuredError(runtimeErrorCodeWorkspaceGrantDenied, sharederrors.MessageIDForCode(runtimeErrorCodeWorkspaceGrantDenied)))

	})
})

var _ = ginkgo.Describe("workspace grant role resolution", func() {
	ginkgo.It("caps grant by current user role", ginkgo.Label("unit"), func() {
		tests := []struct {
			name     string
			userRole wikid.GrantRole
			grant    wikid.GrantRole
			want     wikid.GrantRole
		}{
			{name: "viewer grant remains viewer", userRole: wikid.GrantRoleEditor, grant: wikid.GrantRoleViewer, want: wikid.GrantRoleViewer},
			{name: "editor grant capped by downgraded viewer", userRole: wikid.GrantRoleViewer, grant: wikid.GrantRoleEditor, want: wikid.GrantRoleViewer},
			{name: "admin user keeps editor grant", userRole: wikid.GrantRoleAdmin, grant: wikid.GrantRoleEditor, want: wikid.GrantRoleEditor},
			{name: "unknown user role denies effective grant", userRole: "", grant: wikid.GrantRoleEditor, want: ""},
		}
		for _, tt := range tests {
			func() {
				_ = tt.name
				Expect(effectiveWorkspaceGrantRole(tt.userRole, tt.grant)).To(Equal(tt.want))

			}()
		}

	})
})

func newFrontdActorTestWiki() *wiki.Wiki {
	ginkgo.GinkgoHelper()
	w, err := wiki.NewWiki(&wiki.WikiOptions{
		StorageDir:          leafwikiTempDir(),
		AdminPassword:       "admin",
		JWTSecret:           "test-secret-key-for-unit-tests-1",
		AccessTokenTimeout:  15 * time.Minute,
		RefreshTokenTimeout: 7 * 24 * time.Hour,
	})
	Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("NewWiki failed: %v", err))

	return w
}

func issueOAuthAccessTokenForTest(router http.Handler) string {
	ginkgo.GinkgoHelper()
	loginReq := httptest.NewRequest(http.MethodPost, "http://leafwiki.local/api/auth/login", strings.NewReader(`{"identifier":"admin","password":"admin"}`))
	loginReq.Header.Set("Content-Type", "application/json")
	loginRec := httptest.NewRecorder()
	router.ServeHTTP(loginRec, loginReq)
	Expect(loginRec).To(HaveHTTPStatus(http.StatusOK))

	cookies := loginRec.Result().Cookies()

	verifier := "oauth-test-verifier-abcdefghijklmnopqrstuvwxyz0123456789"
	sum := sha256.Sum256([]byte(verifier))
	challenge := base64.RawURLEncoding.EncodeToString(sum[:])
	q := url.Values{
		"client_id":             {"leafwiki-local-mcp"},
		"response_type":         {"code"},
		"redirect_uri":          {"http://127.0.0.1:49152/callback"},
		"scope":                 {"leafwiki:mcp"},
		"state":                 {"oauth-actor-context"},
		"resource":              {"http://leafwiki.local/mcp"},
		"code_challenge":        {challenge},
		"code_challenge_method": {"S256"},
	}
	authorizeReq := httptest.NewRequest(http.MethodGet, "http://leafwiki.local/oauth/authorize?"+q.Encode(), nil)
	for _, cookie := range cookies {
		authorizeReq.AddCookie(cookie)
	}
	authorizeRec := httptest.NewRecorder()
	router.ServeHTTP(authorizeRec, authorizeReq)
	Expect(authorizeRec).To(HaveHTTPStatus(http.StatusFound))

	approvalURL, err := url.Parse(authorizeRec.Header().Get("Location"))
	Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("parse approval redirect: %v", err))

	form := approvalURL.Query()
	form.Set("decision", "approve")
	approveReq := httptest.NewRequest(http.MethodPost, "http://leafwiki.local/oauth/authorize", strings.NewReader(form.Encode()))
	approveReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	for _, cookie := range cookies {
		approveReq.AddCookie(cookie)
	}
	approveRec := httptest.NewRecorder()
	router.ServeHTTP(approveRec, approveReq)
	Expect(approveRec).To(HaveHTTPStatus(http.StatusFound))

	callbackURL, err := url.Parse(approveRec.Header().Get("Location"))
	Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("parse authorize callback: %v", err))

	code := callbackURL.Query().Get("code")
	Expect(code).NotTo(BeEmpty(), fmt.Sprintf("authorize callback missing code: %s", callbackURL.String()))

	tokenForm := url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {code},
		"redirect_uri":  {"http://127.0.0.1:49152/callback"},
		"client_id":     {"leafwiki-local-mcp"},
		"code_verifier": {verifier},
	}
	tokenReq := httptest.NewRequest(http.MethodPost, "http://leafwiki.local/oauth/token", strings.NewReader(tokenForm.Encode()))
	tokenReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	tokenRec := httptest.NewRecorder()
	router.ServeHTTP(tokenRec, tokenReq)
	Expect(tokenRec).To(HaveHTTPStatus(http.StatusOK))

	var tokenBody struct {
		AccessToken string `json:"access_token"`
		TokenType   string `json:"token_type"`
	}
	Expect(json.Unmarshal(tokenRec.Body.Bytes(), &tokenBody)).To(Succeed(), fmt.Sprintf("decode token response: %v", err))
	Expect(tokenBody.TokenType).To(Equal("Bearer"), fmt.Sprintf("token response type = %q, want Bearer", tokenBody.TokenType))

	return tokenBody.AccessToken
}

var _ = ginkgo.Describe("boolean configuration resolution", func() {
	ginkgo.It("uses workspace sync environment when flag absent", ginkgo.Label("unit"), func() {
		leafwikiSetenv("LEAFWIKI_ENABLE_WORKSPACE_SYNC", "true")

		got := resolveBool("enable-workspace-sync", false, map[string]bool{}, "LEAFWIKI_ENABLE_WORKSPACE_SYNC")
		Expect(got).To(BeTrue(), fmt.Sprintf("enableWorkspaceSync from env = false, want true"))

	})
})

var _ = ginkgo.Describe("logging configuration resolution", func() {
	ginkgo.It("defaults to file under resolved data dir", ginkgo.Label("unit"), func() {
		dataDir := filepath.Join(leafwikiTempDir(), "data")

		cfg := resolveLoggingConfigForArgs([]string{"--data-dir=" + dataDir})
		Expect(cfg).To(haveLoggingConfig(leaflogging.TargetFile, Equal(filepath.Join(dataDir, ".leafwiki", "logs", "leafwiki.log"))))

	})
})

var _ = ginkgo.Describe("logging configuration resolution", func() {
	ginkgo.It("CLI overrides environment target", ginkgo.Label("unit"), func() {
		leafwikiSetenv("LEAFWIKI_LOG_TARGET", "file")

		cfg := resolveLoggingConfigForArgs([]string{"--log-target=stderr"})
		Expect(cfg).To(haveLoggingConfig(leaflogging.TargetStderr, BeEmpty()))

	})
})

var _ = ginkgo.Describe("logging configuration resolution", func() {
	ginkgo.It("uses environment when flag absent", ginkgo.Label("unit"), func() {
		leafwikiSetenv("LEAFWIKI_LOG_TARGET", "stdout")

		cfg := resolveLoggingConfigForArgs(nil)
		Expect(cfg.Target).To(Equal(leaflogging.TargetStdout), fmt.Sprintf("Target = %q, want %q", cfg.Target, leaflogging.TargetStdout))

	})
})

var _ = ginkgo.Describe("logging configuration resolution", func() {
	ginkgo.It("CLI stream target ignores inherited env log file", ginkgo.Label("unit"), func() {
		leafwikiSetenv("LEAFWIKI_LOG_FILE", "logs/from-env.log")

		cfg := resolveLoggingConfigForArgs([]string{"--log-target=stderr"})
		Expect(cfg).To(haveLoggingConfig(leaflogging.TargetStderr, BeEmpty()))

	})
})

var _ = ginkgo.Describe("logging configuration resolution", func() {
	ginkgo.It("rejects log file for stream target", ginkgo.Label("unit"), func() {
		_, err := resolveLoggingConfigForArgsAllowError([]string{
			"--log-target=stderr",
			"--log-file=custom.log",
		})
		Expect(err).To(MatchError(leaflogging.ErrLogFileRequiresFileTarget))

	})
})

var _ = ginkgo.Describe("leafwiki main process", func() {
	ginkgo.It("default server logging uses file for startup and request logs and keeps stdout clean", ginkgo.Label("e2e"), func() {
		dataDir := filepath.Join(leafwikiTempDir(), "data")
		port := freeTCPPort()
		proc := startLeafwikiHelper([]string{
			"--disable-auth",
			"--data-dir", dataDir,
			"--host", "127.0.0.1",
			"--port", port,
		}, map[string]string{})

		waitForLeafwikiReady(proc, port)
		globalDesc := waitForGlobalWikidDescriptor(dataDir)
		logPath := filepath.Join(dataDir, ".leafwiki", "logs", "leafwiki.log")
		waitForFileContaining(logPath, leafwikiStartupLogMessage)
		waitForFileContaining(logPath, leafwikiHTTPRequestLogMessage)
		proc.stop()
		terminateProjectDaemonProcess(globalDesc.PID)
		waitForLeafwikiUnavailable(port)
		waitForProjectLocksReusable(dataDir, filepath.Join(dataDir, "root"), 15*time.Second)

		stdout := readFileString(proc.stdoutPath)
		Expect(stdout).To(BeEmpty(), fmt.Sprintf("stdout contains server log: %q", stdout))

		Expect(readJSONLogEntries(logPath)).To(SatisfyAll(
			ContainElement(haveJSONLogEntry(leafwikiStartupLogMessage)),
			ContainElement(haveJSONLogEntry(leafwikiHTTPRequestLogMessage,
				HaveKeyWithValue("method", http.MethodGet),
				HaveKeyWithValue("path", "/api/health"),
				HaveKeyWithValue("status", float64(http.StatusOK)),
			)),
		))

	})
})

var _ = ginkgo.Describe("leafwiki main process", func() {
	ginkgo.It("default file logging records fresh data directory creation", ginkgo.Label("e2e"), func() {
		dataDir := filepath.Join(leafwikiTempDir(), "data")
		port := freeTCPPort()
		proc := startLeafwikiHelper([]string{
			"--disable-auth",
			"--data-dir", dataDir,
			"--host", "127.0.0.1",
			"--port", port,
		}, map[string]string{"LEAFWIKI_DAEMON_IDLE_TIMEOUT": "1s"})

		waitForLeafwikiReady(proc, port)
		globalDesc := waitForGlobalWikidDescriptor(dataDir)
		waitForFileContaining(filepath.Join(dataDir, ".leafwiki", "logs", "leafwiki.log"), leafwikiStartupLogMessage)
		proc.stop()
		terminateProjectDaemonProcess(globalDesc.PID)
		waitForLeafwikiUnavailable(port)
		waitForProjectLocksReusable(dataDir, filepath.Join(dataDir, "root"), 15*time.Second)

		wantPath := filepath.Join(filepath.Dir(filepath.Clean(dataDir)), "home", ".leafwiki")
		Expect(readJSONLogEntries(filepath.Join(dataDir, ".leafwiki", "logs", "leafwiki.log"))).To(ContainElement(haveJSONLogEntry(
			leafwikiDataDirectoryCreatedLogMsg,
			HaveKeyWithValue("path", wantPath),
		)))

	})
})

var _ = ginkgo.Describe("leafwiki main process", func() {
	ginkgo.It("CLI stderr target overrides env file target", ginkgo.Label("e2e"), func() {
		dataDir := filepath.Join(leafwikiTempDir(), "data")
		port := freeTCPPort()
		proc := startLeafwikiHelper([]string{
			"--disable-auth",
			"--data-dir", dataDir,
			"--host", "127.0.0.1",
			"--port", port,
			"--log-target", "stderr",
		}, map[string]string{
			"LEAFWIKI_LOG_TARGET": "file",
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
		stdout := readFileString(proc.stdoutPath)
		Expect(stdout).To(BeEmpty(), fmt.Sprintf("stdout = %q, want empty for stderr target", stdout))

	})
})
