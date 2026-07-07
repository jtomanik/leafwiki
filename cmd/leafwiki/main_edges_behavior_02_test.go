package main

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"time"

	sdkauth "github.com/modelcontextprotocol/go-sdk/auth"
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/gstruct"

	coreauth "github.com/perber/wiki/internal/core/auth"
	sharederrors "github.com/perber/wiki/internal/core/shared/errors"
	leaflogging "github.com/perber/wiki/internal/logging"
	"github.com/perber/wiki/internal/projectdaemon"
	testmatchers "github.com/perber/wiki/internal/test_utils/matchers"
	"github.com/perber/wiki/internal/wiki"
	"github.com/perber/wiki/internal/wikid"
)

var _ = ginkgo.Describe("leafwiki command helper edges", func() {
	ginkgo.It("normalizes daemon runtime config and log paths for owner and workspace requests", ginkgo.Label("unit"), func() {
		dataDir := filepath.Join(leafwikiTempDir(), "data")
		rootDir := filepath.Join(leafwikiTempDir(), "root")
		logPath := filepath.Join(dataDir, ".leafwiki", "logs", "leafwiki.log")

		cfg := leafwikiRuntimeConfig{
			Workspace: wiki.Workspace{
				ID:      newFixtureWorkspaceID("home"),
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
		Expect(daemonCfg).To(MatchWorkspaceSyncEnabledDaemonConfig(gstruct.Fields{
			"WorkspaceID":            Equal(newFixtureWorkspaceID("home")),
			"DataDir":                Equal(expectedDataDir),
			"RootDir":                Equal(expectedRootDir),
			"LogFile":                Equal(filepath.Join(expectedDataDir, ".leafwiki", "logs", "leafwiki.log")),
			"InjectCodeInHeaderHash": Not(BeEmpty()),
			"MarkdownLinkRootPrefix": Equal("/docs"),
		}))

		cfg.APIKey = "sk-test"
		cfg.Logging = leaflogging.Config{Target: leaflogging.TargetStderr}
		cfg.MCPTransports = mcpTransports{Stdio: true}
		workspaceCfg, err := daemonWorkspaceRuntimeConfig(cfg)
		Expect(err).NotTo(HaveOccurred())
		Expect(workspaceCfg).To(gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
			"APIKey":       BeEmpty(),
			"RuntimeStack": Equal(projectdaemon.RuntimeStackWikidFrontd),
			"Logging": gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
				"Target":   Equal(leaflogging.TargetFile),
				"FilePath": Equal(filepath.Join(dataDir, ".leafwiki", "logs", "leafwiki.log")),
			}),
		}))

		rel, err := localRelativePathResult(dataDir, filepath.Join(dataDir, "nested", "leafwiki.log"))
		Expect(err).To(Succeed())
		Expect(rel).To(Equal(filepath.Join("nested", "leafwiki.log")))
		_, err = localRelativePathResult(dataDir, dataDir)
		Expect(err).To(MatchError(errRelativePathOutsideBase))
		_, err = localRelativePathResult(dataDir, filepath.Dir(dataDir))
		Expect(err).To(MatchError(errRelativePathOutsideBase))

		canonicalDataDir := filepath.Join(leafwikiTempDir(), "canonical")
		Expect(observeDaemonLogFileForConfig(leafwikiRuntimeConfig{}, canonicalDataDir)).To(Equal(daemonLogFileObservation{State: daemonLogFileAbsent}))
		cfg.Workspace.DataDir = filepath.Join(leafwikiTempDir(), "original")
		cfg.Logging = leaflogging.Config{Target: leaflogging.TargetFile, FilePath: filepath.Join(canonicalDataDir, ".leafwiki", "logs", "leafwiki.log")}
		Expect(observeDaemonLogFileForConfig(cfg, canonicalDataDir)).To(Equal(daemonLogFileObservation{State: daemonLogFileResolvedAbsolute, Path: filepath.Clean(cfg.Logging.FilePath)}))
		cfg.Logging.FilePath = filepath.Join(leafwikiTempDir(), "external.log")
		Expect(observeDaemonLogFileForConfig(cfg, canonicalDataDir)).To(Equal(daemonLogFileObservation{State: daemonLogFileResolvedAbsolute, Path: filepath.Clean(cfg.Logging.FilePath)}))
	})

	ginkgo.It("handles runtime role lookup, HTTP tokens, and response encoding errors", ginkgo.Label("unit"), func() {
		roles := []projectdaemon.RoleHealth{
			{Name: projectdaemon.RoleFrontd, URL: "http://127.0.0.1:8080"},
		}
		Expect(roleURL(roles, projectdaemon.RoleFrontd)).To(Equal("http://127.0.0.1:8080"))
		Expect(roleURL(roles, projectdaemon.RoleWorkspaced)).To(BeEmpty())
		role, err := runtimeRoleHealthResult(roles, projectdaemon.RoleFrontd)
		Expect(err).To(Succeed())
		Expect(role.Name).To(Equal(projectdaemon.RoleFrontd))
		_, err = runtimeRoleHealthResult(roles, projectdaemon.RoleWorkspaced)
		Expect(err).To(MatchError(errRuntimeRoleAbsent))

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
		Expect(rec).To(HaveHTTPHeaderWithValue("Content-Type", "application/json"))
		Expect(rec).To(HaveHTTPBody(ContainSubstring(`"ok":"true"`)))

		writeRuntimeJSON(httptest.NewRecorder(), func() {})
		failingWriter := &leafwikiFailingResponseWriter{err: errors.New("write failed")}
		writeRuntimeError(failingWriter, http.StatusForbidden, runtimeErrorCodeWorkspaceGrantDenied)
		Expect(failingWriter.statuses).To(ContainElement(http.StatusForbidden))
	})

	ginkgo.It("maps actor contexts, runtime grants, and home workspace status edges", ginkgo.Label("unit"), func() {
		_, err := actorContextForUser(nil, "api_key", leafwikiRuntimeConfig{})
		Expect(err).To(MatchError(errRuntimeActorUserRequired))

		actor, err := actorContextForUser(&coreauth.User{
			ID:       newFixtureUserID("u1"),
			Username: "ada",
			Email:    "ada@example.test",
			Role:     coreauth.RoleEditor,
		}, string(leafwikiActorAuthMethodAPIKey), leafwikiRuntimeConfig{Workspace: wiki.Workspace{ID: newFixtureWorkspaceID("workspace-a")}})
		Expect(err).NotTo(HaveOccurred())
		Expect(actor).To(SatisfyAll(
			HaveActorSubjectForUser(newFixtureUserID("u1")),
			HaveActorWorkspace(newFixtureWorkspaceID("workspace-a")),
			HaveField("Scopes", ContainElement("leafwiki:mcp")),
		))

		Expect(wikidGrantRoleForCoreRole(coreauth.RoleViewer)).To(Equal(wikid.GrantRoleViewer))
		Expect(wikidGrantRoleForCoreRole(coreauth.RoleEditor)).To(Equal(wikid.GrantRoleEditor))
		Expect(wikidGrantRoleForCoreRole(coreauth.RoleAdmin)).To(Equal(wikid.GrantRoleAdmin))
		Expect(wikidGrantRoleForCoreRole("owner")).To(BeEmpty())
		Expect(scopesForGrantRole(newFixtureGrantRole("owner"))).To(BeNil())
		Expect(ensureRuntimeHomeGrant(nil, nil)).To(MatchError(errRuntimeHomeGrantUserRequired))
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
		Expect(status).To(gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
			"State": Equal(wikid.WorkspaceStateStarting),
			"URL":   Equal("http://127.0.0.1:8001"),
		}))

		syncHomeWorkspaceStatus(supervisor, []projectdaemon.RoleHealth{{
			Name:      projectdaemon.RoleWorkspaced,
			State:     projectdaemon.RoleStateStopped,
			Error:     string(leafwikiRuntimeFailureStopped),
			UpdatedAt: now.Add(time.Second),
		}})
		status = supervisor.Status(wikid.HomeWorkspaceID)
		Expect(status).To(MatchWorkspaceStatusFailure(wikid.WorkspaceStateCrashed, leafwikiRuntimeFailureStopped))

		syncHomeWorkspaceStatus(supervisor, []projectdaemon.RoleHealth{{
			Name:      projectdaemon.RoleWorkspaced,
			State:     projectdaemon.RoleStateDegraded,
			UpdatedAt: now.Add(2 * time.Second),
		}})
		Expect(supervisor.Status(wikid.HomeWorkspaceID).State).To(Equal(wikid.WorkspaceStateRegistered))
	})

	ginkgo.It("preserves runtime tokens, actor resolution, restart handling, and readiness waits", ginkgo.Label("integration"), func() {
		w := newFrontdActorTestWiki()
		ginkgo.DeferCleanup(w.Close)
		cfg := leafwikiRuntimeConfig{
			Workspace:   wiki.Workspace{ID: newFixtureWorkspaceID("home")},
			DisableAuth: true,
		}

		req := httptest.NewRequest(http.MethodGet, "/mcp", nil)
		actor, err := frontdActorResolver(w, cfg)(req)
		Expect(err).NotTo(HaveOccurred())
		Expect(actor).To(SatisfyAll(
			HaveActorSubjectForUser(newFixtureUserID("public-editor")),
			HaveActorAuthMethod(leafwikiActorAuthMethodDisabled),
		))

		_, err = frontdMCPTokenVerifier(&wiki.Wiki{})(context.Background(), "not-an-api-key", req)
		Expect(err).To(MatchError(sdkauth.ErrInvalidToken))
		_, err = frontdMCPTokenVerifier(&wiki.Wiki{})(context.Background(), "lwk_key_secret", req)
		Expect(err).To(MatchError(sdkauth.ErrInvalidToken))

		editor, err := w.UserService().CreateUser("token-editor", "token-editor@example.com", "password", coreauth.RoleEditor)
		Expect(err).NotTo(HaveOccurred())
		editorID := coreauth.UserIDFromString(editor.ID)
		created, err := w.APIKeyService().CreateAPIKey(editorID, "MCP client", editorID)
		Expect(err).NotTo(HaveOccurred())
		info, err := frontdMCPTokenVerifier(w)(context.Background(), created.Secret, req)
		Expect(err).NotTo(HaveOccurred())
		Expect(info).To(MatchSDKTokenUserID(editorID))

		rec := httptest.NewRecorder()
		handleWikidTokenVerify(rec, httptest.NewRequest(http.MethodPost, "/__leafwiki/token/verify", nil), w)
		Expect(rec).To(HaveHTTPStatus(http.StatusUnauthorized))

		rec = httptest.NewRecorder()
		invalidReq := httptest.NewRequest(http.MethodPost, "/__leafwiki/token/verify", nil)
		invalidReq.Header.Set("Authorization", "Bearer invalid")
		handleWikidTokenVerify(rec, invalidReq, w)
		Expect(rec).To(HaveHTTPStatus(http.StatusUnauthorized))

		rec = httptest.NewRecorder()
		validReq := httptest.NewRequest(http.MethodPost, "/__leafwiki/token/verify", nil)
		validReq.Header.Set("Authorization", "Bearer "+created.Secret)
		handleWikidTokenVerify(rec, validReq, w)
		Expect(rec).To(HaveHTTPStatus(http.StatusOK))
		Expect(rec).To(HaveHTTPBody(MatchTokenVerifyUserID(editorID)))

		authenticatedResolver := frontdMCPActorResolver(&wiki.Wiki{}, leafwikiRuntimeConfig{})
		_, err = authenticatedResolver(req)
		Expect(err).To(MatchError(errFrontdMCPTokenInfoMissing))

		disabledResolver := frontdMCPActorResolver(w, cfg)
		actor, err = disabledResolver(req)
		Expect(err).NotTo(HaveOccurred())
		Expect(actor).To(HaveActorAuthMethod(leafwikiActorAuthMethodDisabled))

		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		Expect(waitForInternalRuntimeRoleSignal(ctx)).To(Succeed())

		sessions := projectdaemon.NewSessionRegistry(time.Millisecond, nil)
		_, err = sessions.Register()
		Expect(err).NotTo(HaveOccurred())
		Expect(sessions.Count()).To(Equal(1))
		Expect(waitForFirstProjectDaemonSession(context.Background(), sessions, time.Millisecond)).To(Succeed())
		Expect(waitForFirstProjectDaemonSession(context.Background(), sessions, 0)).To(Succeed())

		cancelCtx, cancelWait := context.WithCancel(context.Background())
		cancelWait()
		Expect(waitForFirstProjectDaemonSession(cancelCtx, projectdaemon.NewSessionRegistry(time.Millisecond, nil), time.Millisecond)).To(MatchError(context.Canceled))
		Expect(waitForFirstProjectDaemonActivity(context.Background(), sessions, projectdaemon.NewAgentPresenceRegistry(time.Millisecond, nil), time.Millisecond)).To(Succeed())
		Expect(waitForFirstProjectDaemonActivity(context.Background(), sessions, projectdaemon.NewAgentPresenceRegistry(time.Millisecond, nil), 0)).To(Succeed())
		Expect(waitForFirstProjectDaemonActivity(cancelCtx, projectdaemon.NewSessionRegistry(time.Millisecond, nil), projectdaemon.NewAgentPresenceRegistry(time.Millisecond, nil), time.Millisecond)).To(MatchError(context.Canceled))

		idleCtx, idleCancelContext := context.WithCancel(context.Background())
		idleCancel := func() {
			idleCancelContext()
		}
		idleCallback := idleShutdownCallback(idleCtx, idleCancel, time.Hour, nil)
		idleCallback(1)
		idleCallback(1)
		Consistently(idleCtx.Done()).WithTimeout(25 * time.Millisecond).ShouldNot(BeClosed())

		seenSessions := projectdaemon.NewSessionRegistry(time.Millisecond, nil)
		_, err = seenSessions.Register()
		Expect(err).NotTo(HaveOccurred())
		noSessionCtx, noSessionCancel := context.WithCancel(context.Background())
		cancelIfNoSessionAfterStartupGrace(context.Background(), noSessionCancel, seenSessions, 0)
		Consistently(noSessionCtx.Done()).WithTimeout(25 * time.Millisecond).ShouldNot(BeClosed())

		noActivityCtx, noActivityCancel := context.WithCancel(context.Background())
		cancelIfNoActivityAfterStartupGrace(context.Background(), noActivityCancel, seenSessions, projectdaemon.NewAgentPresenceRegistry(time.Millisecond, nil), 0)
		Consistently(noActivityCtx.Done()).WithTimeout(25 * time.Millisecond).ShouldNot(BeClosed())

		parentSessionCtx, parentSessionCancel := context.WithCancel(context.Background())
		cancelIfNoSessionAfterStartupGrace(cancelCtx, parentSessionCancel, projectdaemon.NewSessionRegistry(time.Millisecond, nil), time.Hour)
		Consistently(parentSessionCtx.Done()).WithTimeout(25 * time.Millisecond).ShouldNot(BeClosed())

		parentActivityCtx, parentActivityCancel := context.WithCancel(context.Background())
		cancelIfNoActivityAfterStartupGrace(cancelCtx, parentActivityCancel, projectdaemon.NewSessionRegistry(time.Millisecond, nil), projectdaemon.NewAgentPresenceRegistry(time.Millisecond, nil), time.Hour)
		Consistently(parentActivityCtx.Done()).WithTimeout(25 * time.Millisecond).ShouldNot(BeClosed())

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
		activeRuntime.restartRole(newFixtureRoleName("unsupported"))
	})

	ginkgo.It("resolves private endpoints, wikid tokens, and frontd actors", ginkgo.Label("integration"), func() {
		w := newFrontdActorTestWiki()
		ginkgo.DeferCleanup(w.Close)
		editor, err := w.UserService().CreateUser("edge-editor", "edge-editor@example.com", "password", coreauth.RoleEditor)
		Expect(err).NotTo(HaveOccurred())
		editorID := coreauth.UserIDFromString(editor.ID)
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
					"actor": projectdaemon.ActorContext{Subject: "user:" + editor.ID.String(), Username: editor.Username},
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
		Expect(info).To(gstruct.PointTo(gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
			"UserID": MatchUserIDString(coreauth.UserIDFromString(editor.ID)),
			"Scopes": ContainElement("leafwiki:mcp"),
		})))
		Expect(struct {
			Paths          []string
			Authorizations []string
			ControlTokens  []string
		}{seenPaths, seenAuthorization, seenControlTokens}).To(gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
			"Paths":          ContainElement("/__leafwiki/token/verify"),
			"Authorizations": ContainElement("Bearer edge-token"),
			"ControlTokens":  ContainElement("daemon-token"),
		}))
		_, err = wikidMCPTokenVerifier("http://[::1", "daemon-token")(context.Background(), "edge-token", nil)
		Expect(err).To(MatchError(sdkauth.ErrInvalidToken))

		req := httptest.NewRequest(http.MethodPatch, "/source", nil)
		req.RemoteAddr = "127.0.0.1:9191"
		req.Header.Set(projectdaemon.ActorContextHeader, "caller-supplied")
		var out struct {
			Actor projectdaemon.ActorContext `json:"actor"`
		}
		Expect(callWikidPrivateEndpoint(context.Background(), privateServer.URL, "daemon-token", "/__leafwiki/actor-context", req, &out)).To(Succeed())
		Expect(out.Actor).To(HaveActorSubjectForUser(coreauth.UserIDFromString(editor.ID)))
		Expect(callWikidPrivateEndpoint(context.Background(), privateServer.URL, "daemon-token", "/discard", nil, nil)).To(Succeed())
		nilHeaderSource := &http.Request{Method: http.MethodPut, URL: &url.URL{Path: "/nil-header"}}
		Expect(callWikidPrivateEndpoint(context.Background(), privateServer.URL, "daemon-token", "/discard", nilHeaderSource, nil)).To(Succeed())
		Expect(callWikidPrivateEndpoint(context.Background(), "http://127.0.0.1:1", "daemon-token", "/discard", nil, nil)).To(MatchURLError())

		actor, err := wikidActorResolver(privateServer.URL, "daemon-token")(httptest.NewRequest(http.MethodGet, "/mcp", nil))
		Expect(err).NotTo(HaveOccurred())
		Expect(actor).To(HaveActorSubjectForUser(coreauth.UserIDFromString(editor.ID)))
		_, err = wikidActorResolver("http://[::1", "daemon-token")(httptest.NewRequest(http.MethodGet, "/mcp", nil))
		Expect(err).To(MatchURLError())

		_, err = frontdPublicMCPHandler(leafwikiRuntimeConfig{}, "://bad-upstream", "daemon-token", privateServer.URL)
		Expect(err).To(MatchInvalidWorkspacedUpstream())

		err = callWikidPrivateEndpoint(context.Background(), privateServer.URL, "daemon-token", "/structured-error", nil, nil)
		Expect(err).To(MatchWikidPrivateEndpointStatus(http.StatusForbidden))
		Expect(classifyWikidPrivateAuthFailure(err)).To(Equal(wikidPrivateAuthFailureRejected))
		var endpointErr *wikidPrivateEndpointError
		Expect(err).To(Satisfy(func(err error) bool {
			return errors.As(err, &endpointErr)
		}))
		Expect(endpointErr).To(testmatchers.HaveStructuredError(errCodeStdioAuthAPIKeyInvalid, sharederrors.MessageIDForCode(errCodeStdioAuthAPIKeyInvalid)))
		Expect((*wikidPrivateEndpointError)(nil).Error()).To(BeEmpty())
		Expect(observeWikidPrivateEndpointStatus(&wikidPrivateEndpointError{Path: "/empty", StatusCode: 499})).To(Equal(wikidPrivateEndpointStatusObservation{Status: 499}))
		Expect(classifyWikidPrivateAuthFailure(errors.New("plain"))).To(Equal(wikidPrivateAuthFailureOther))
		Expect(classifyWikidPrivateAuthFailure(&wikidPrivateEndpointError{StatusCode: http.StatusInternalServerError})).To(Equal(wikidPrivateAuthFailureOther))

		_, err = wikidMCPTokenVerifier(privateServer.URL, "daemon-token")(context.Background(), "edge-token", httptest.NewRequest(http.MethodGet, "/mcp", nil))
		Expect(err).NotTo(HaveOccurred())
		_, err = wikidMCPTokenVerifier(privateServer.URL, "daemon-token")(context.Background(), "edge-token", httptest.NewRequest(http.MethodGet, "/missing", nil))
		Expect(err).NotTo(HaveOccurred())

		cfg := leafwikiRuntimeConfig{Workspace: wiki.Workspace{ID: newFixtureWorkspaceID("workspace-a")}}
		resolver := frontdMCPActorResolver(w, cfg)
		resolved, resolveErr := resolveWithSDKToken(resolver, apiKey.Secret, coreauth.UserIDFromString(editor.ID))
		Expect(resolveErr).NotTo(HaveOccurred())
		Expect(resolved).To(SatisfyAll(
			HaveActorSubjectForUser(coreauth.UserIDFromString(editor.ID)),
			HaveActorAuthMethod(leafwikiActorAuthMethodAPIKey),
		))

		resolved, resolveErr = resolveWithSDKToken(resolver, "oauth-token", coreauth.UserIDFromString(editor.ID))
		Expect(resolveErr).NotTo(HaveOccurred())
		Expect(resolved).To(HaveActorAuthMethod(leafwikiActorAuthMethodOAuth))

		_, resolveErr = resolveWithSDKToken(frontdMCPActorResolver(&wiki.Wiki{}, cfg), "oauth-token", coreauth.UserIDFromString(editor.ID))
		Expect(resolveErr).To(MatchError(errFrontdMCPUserServiceUnavailable))

		_, resolveErr = resolveWithSDKToken(resolver, "oauth-token", newFixtureUserID("missing-user"))
		Expect(resolveErr).To(MatchError(coreauth.ErrUserNotFound))

		_, _, err = frontdActorUser(httptest.NewRequest(http.MethodGet, "/mcp", nil), &wiki.Wiki{}, leafwikiRuntimeConfig{})
		Expect(err).To(MatchError(errFrontdWorkspaceCredentialsMissing))
		_, err = frontdActorResolver(w, leafwikiRuntimeConfig{})(httptest.NewRequest(http.MethodGet, "/mcp", nil))
		Expect(err).To(MatchError(errFrontdWorkspaceCredentialsMissing))

		mcpReq := httptest.NewRequest(http.MethodPost, "/mcp", nil)
		mcpReq.Header.Set("Authorization", "Bearer lwk_key_invalid")
		_, _, err = frontdActorUser(mcpReq, w, leafwikiRuntimeConfig{})
		Expect(err).To(MatchError(coreauth.ErrInvalidToken))

		oauthReq := httptest.NewRequest(http.MethodPost, "/mcp", nil)
		oauthReq.Header.Set("Authorization", "Bearer oauth-token")
		_, _, err = frontdActorUser(oauthReq, &wiki.Wiki{}, leafwikiRuntimeConfig{})
		Expect(err).To(MatchError(errFrontdOAuthActorServicesUnavailable))
		_, _, err = frontdActorUser(oauthReq, w, leafwikiRuntimeConfig{})
		Expect(err).To(MatchError(sdkauth.ErrInvalidToken))

		cookieReq := httptest.NewRequest(http.MethodGet, "/", nil)
		cookieReq.AddCookie(&http.Cookie{Name: "leafwiki_at", Value: "invalid-token"})
		_, _, err = frontdActorUser(cookieReq, w, leafwikiRuntimeConfig{})
		Expect(err).To(MatchError(coreauth.ErrInvalidToken))

		publicReq := httptest.NewRequest(http.MethodGet, "/", nil)
		publicUser, method, err := frontdActorUser(publicReq, &wiki.Wiki{}, leafwikiRuntimeConfig{PublicAccess: true})
		Expect(err).NotTo(HaveOccurred())
		Expect(publicUser).To(HaveCoreAuthUserID(newFixtureUserID("public-viewer")))
		Expect(method).To(Equal(string(leafwikiActorAuthMethodPublicAccess)))
	})
})
