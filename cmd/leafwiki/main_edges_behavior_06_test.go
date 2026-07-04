package main

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"syscall"
	"time"

	sdkauth "github.com/modelcontextprotocol/go-sdk/auth"
	sdkjsonrpc "github.com/modelcontextprotocol/go-sdk/jsonrpc"
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	coreauth "github.com/perber/wiki/internal/core/auth"
	"github.com/perber/wiki/internal/frontd"
	httpinternal "github.com/perber/wiki/internal/http"
	"github.com/perber/wiki/internal/locking"
	leaflogging "github.com/perber/wiki/internal/logging"
	"github.com/perber/wiki/internal/projectdaemon"
	"github.com/perber/wiki/internal/wiki"
	"github.com/perber/wiki/internal/wikid"
	"github.com/perber/wiki/internal/workspaceid"
	sqlite3 "modernc.org/sqlite/lib"
)

var _ = ginkgo.Describe("leafwiki command helper edges", func() {
	ginkgo.It("reports SDK transport bridge connect and pump errors", ginkgo.Label("integration"), func() {
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
			Expect(req.Header).To(HaveKeyWithValue(http.CanonicalHeaderKey(projectdaemon.ControlTokenHeader), ContainElement("daemon-token")))
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
		Expect(resp).To(HaveHTTPStatus(http.StatusNoContent))
		Expect(resp.Body.Close()).To(Succeed())

		previousEncodeActorContext := encodeActorContextForRuntime
		ginkgo.DeferCleanup(func() {
			encodeActorContextForRuntime = previousEncodeActorContext
		})
		encodeActorContextErr := errors.New("round trip encode failed")
		encodeActorContextForRuntime = func(projectdaemon.ActorContext) (string, error) {
			return "", encodeActorContextErr
		}
		_, err = rt.actorContext(httptest.NewRequest(http.MethodGet, upstream.URL, nil))
		Expect(err).To(MatchError(encodeActorContextErr))
		encodeActorContextForRuntime = previousEncodeActorContext
	})

	ginkgo.It("reports project daemon owner and spawn failures", ginkgo.Label("integration"), func() {
		previousOwner := runWikidFrontdOwnerForProjectDaemon
		previousExecutable := projectDaemonExecutable
		ginkgo.DeferCleanup(func() {
			runWikidFrontdOwnerForProjectDaemon = previousOwner
			projectDaemonExecutable = previousExecutable
		})

		validCfg := leafwikiRuntimeConfig{
			Workspace: wiki.Workspace{
				DataDir: filepath.Join(leafwikiTempDir(), "data"),
				RootDir: filepath.Join(leafwikiTempDir(), "root"),
			},
			Logging:     leaflogging.Config{Target: leaflogging.TargetStderr},
			DisableAuth: true,
		}

		badPathCfg := validCfg
		badPathCfg.Workspace.DataDir = "bad\x00data"
		Expect(runProjectDaemonOwner(context.Background(), badPathCfg)).To(MatchPathError())

		Expect(os.MkdirAll(validCfg.Workspace.DataDir, 0o755)).To(Succeed())
		Expect(os.MkdirAll(validCfg.Workspace.RootDir, 0o755)).To(Succeed())
		dataLock, err := locking.AcquireDataDirLock(validCfg.Workspace.DataDir)
		Expect(err).NotTo(HaveOccurred())
		Expect(runProjectDaemonOwner(context.Background(), validCfg)).To(Satisfy(locking.IsDataDirLockHeld))
		Expect(dataLock.Release()).To(Succeed())

		rootLock, err := locking.AcquireRootDirLock(validCfg.Workspace.RootDir)
		Expect(err).NotTo(HaveOccurred())
		Expect(runProjectDaemonOwner(context.Background(), validCfg)).To(Satisfy(locking.IsRootDirLockHeld))
		Expect(rootLock.Release()).To(Succeed())

		badLogCfg := validCfg
		badLogCfg.Logging = leaflogging.Config{Target: leaflogging.Target("bad-target")}
		Expect(runProjectDaemonOwner(context.Background(), badLogCfg)).To(MatchError(leaflogging.ErrInvalidLogTarget))

		legacyDBDir := filepath.Join(validCfg.Workspace.DataDir, "users.db")
		Expect(os.MkdirAll(legacyDBDir, 0o755)).To(Succeed())
		Expect(os.WriteFile(filepath.Join(legacyDBDir, "child"), []byte("x"), 0o600)).To(Succeed())
		Expect(runProjectDaemonOwner(context.Background(), validCfg)).To(MatchPathError())
		Expect(os.Remove(filepath.Join(legacyDBDir, "child"))).To(Succeed())
		Expect(os.Remove(legacyDBDir)).To(Succeed())

		authDirFailureCfg := validCfg
		authDirFailureCfg.Workspace.DataDir = filepath.Join(leafwikiTempDir(), "auth-dir-data")
		authDirFailureCfg.Workspace.RootDir = filepath.Join(leafwikiTempDir(), "auth-dir-root")
		Expect(os.MkdirAll(authDirFailureCfg.Workspace.DataDir, 0o755)).To(Succeed())
		Expect(os.MkdirAll(authDirFailureCfg.Workspace.RootDir, 0o755)).To(Succeed())
		Expect(os.MkdirAll(filepath.Join(authDirFailureCfg.Workspace.DataDir, ".leafwiki"), 0o755)).To(Succeed())
		Expect(os.WriteFile(filepath.Join(authDirFailureCfg.Workspace.DataDir, ".leafwiki", "wikid"), []byte("not a directory"), 0o600)).To(Succeed())
		Expect(runProjectDaemonOwner(context.Background(), authDirFailureCfg)).To(MatchPathError())

		oauthDirFailureCfg := validCfg
		oauthDirFailureCfg.Workspace.DataDir = filepath.Join(leafwikiTempDir(), "oauth-dir-data")
		oauthDirFailureCfg.Workspace.RootDir = filepath.Join(leafwikiTempDir(), "oauth-dir-root")
		Expect(os.MkdirAll(oauthDirFailureCfg.Workspace.DataDir, 0o755)).To(Succeed())
		Expect(os.MkdirAll(oauthDirFailureCfg.Workspace.RootDir, 0o755)).To(Succeed())
		oauthPaths := wikid.AuthStoragePaths(oauthDirFailureCfg.Workspace.DataDir)
		Expect(os.MkdirAll(oauthPaths.AuthDir, 0o755)).To(Succeed())
		Expect(os.WriteFile(oauthPaths.OAuthDir, []byte("not a directory"), 0o600)).To(Succeed())
		Expect(runProjectDaemonOwner(context.Background(), oauthDirFailureCfg)).To(MatchPathError())

		wikidFrontdErr := errors.New("wikid-frontd failed")
		runWikidFrontdOwnerForProjectDaemon = func(context.Context, leafwikiRuntimeConfig, projectdaemon.Config) error {
			return wikidFrontdErr
		}
		Expect(runProjectDaemonOwner(context.Background(), validCfg)).To(MatchError(wikidFrontdErr))

		runWikidFrontdOwnerForProjectDaemon = func(context.Context, leafwikiRuntimeConfig, projectdaemon.Config) error {
			return nil
		}
		missingRootCfg := validCfg
		missingRootCfg.Workspace.DataDir = filepath.Join(leafwikiTempDir(), "missing-root-data")
		missingRootCfg.Workspace.RootDir = filepath.Join(leafwikiTempDir(), "missing-root")
		Expect(runProjectDaemonOwner(context.Background(), missingRootCfg)).To(Succeed())
		Expect(missingRootCfg.Workspace.RootDir).To(BeADirectory())

		validTempDir := leafwikiTempDir()
		missingExecutable := filepath.Join(leafwikiTempDir(), "missing-leafwiki")
		blockingFile := blockingPathForLeafwikiTest()
		leafwikiSetenv("TMPDIR", blockingFile)
		_, err = spawnProjectDaemonOwner(validCfg)
		Expect(err).To(MatchError(syscall.ENOTDIR))

		leafwikiSetenv("TMPDIR", validTempDir)
		executableUnavailableErr := errors.New("executable unavailable")
		projectDaemonExecutable = func() (string, error) {
			return "", executableUnavailableErr
		}
		_, err = spawnProjectDaemonOwner(validCfg)
		Expect(err).To(MatchError(executableUnavailableErr))

		projectDaemonExecutable = func() (string, error) {
			return missingExecutable, nil
		}
		_, err = spawnProjectDaemonOwner(validCfg)
		Expect(err).To(MatchError(os.ErrNotExist))

		scheduleProjectDaemonStartupConfigCleanup("")
	})

	ginkgo.It("reports wikid-frontd owner dependency failures", ginkgo.Label("integration"), func() {
		cfg := leafwikiRuntimeConfig{
			Workspace: wiki.Workspace{
				ID:      wikid.HomeWorkspaceID,
				DataDir: filepath.Join(leafwikiTempDir(), "data"),
				RootDir: filepath.Join(leafwikiTempDir(), "root"),
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

		listenErr := errors.New("listen failed")
		netListenForRuntime = func(string, string) (net.Listener, error) {
			return nil, listenErr
		}
		Expect(runWikidFrontdOwner(context.Background(), cfg, ownerCfg)).To(MatchError(listenErr))
		netListenForRuntime = previousNetListen

		tokenErr := errors.New("token failed")
		randomTokenForRuntime = func() (string, error) {
			return "", tokenErr
		}
		Expect(runWikidFrontdOwner(context.Background(), cfg, ownerCfg)).To(MatchError(tokenErr))
		randomTokenForRuntime = previousRandomToken

		hashErr := errors.New("hash failed")
		configHashForRuntime = func(projectdaemon.Config) (string, error) {
			return "", hashErr
		}
		Expect(runWikidFrontdOwner(context.Background(), cfg, ownerCfg)).To(MatchError(hashErr))
		configHashForRuntime = previousConfigHash

		wikiErr := errors.New("wiki failed")
		newRuntimeWikiForRuntime = func(leafwikiRuntimeConfig, projectdaemon.Config, runtimeWikiMode) (*wiki.Wiki, error) {
			return nil, wikiErr
		}
		Expect(runWikidFrontdOwner(context.Background(), cfg, ownerCfg)).To(MatchError(wikiErr))
		newRuntimeWikiForRuntime = func(leafwikiRuntimeConfig, projectdaemon.Config, runtimeWikiMode) (*wiki.Wiki, error) {
			w := newFrontdActorTestWiki()
			ginkgo.DeferCleanup(w.Close)
			return w, nil
		}

		routerOptionsErr := errors.New("router options failed")
		controlPlaneRouterOptionsForOwner = func(leafwikiRuntimeConfig, *wiki.Wiki) (httpinternal.RouterOptions, error) {
			return httpinternal.RouterOptions{}, routerOptionsErr
		}
		Expect(runWikidFrontdOwner(context.Background(), cfg, ownerCfg)).To(MatchError(routerOptionsErr))
		controlPlaneRouterOptionsForOwner = previousControlPlaneOptions

		runtimeErr := errors.New("runtime failed")
		startWikidFrontdRuntimeForOwner = func(context.Context, leafwikiRuntimeConfig, string, string) (*wikidFrontdRuntime, error) {
			return nil, runtimeErr
		}
		Expect(runWikidFrontdOwner(context.Background(), cfg, ownerCfg)).To(MatchError(runtimeErr))

		startWikidFrontdRuntimeForOwner = func(ctx context.Context, _ leafwikiRuntimeConfig, _ string, _ string) (*wikidFrontdRuntime, error) {
			return newLeafwikiReadyOwnerRuntime(ctx), nil
		}
		privateMCPErr := errors.New("private mcp failed")
		newMCPProxyWithActorForRuntime = func(frontd.WorkspaceProxyOptions) (http.Handler, error) {
			return nil, privateMCPErr
		}
		Expect(runWikidFrontdOwner(context.Background(), cfg, ownerCfg)).To(MatchError(privateMCPErr))
		newMCPProxyWithActorForRuntime = previousMCPProxy

		writeDescriptorErr := errors.New("write descriptor failed")
		writeDescriptorAtomicForRuntime = func(string, *projectdaemon.Descriptor) error {
			return writeDescriptorErr
		}
		Expect(runWikidFrontdOwner(context.Background(), cfg, ownerCfg)).To(MatchError(writeDescriptorErr))

		writeCalls := 0
		writeGlobalDescriptorErr := errors.New("write global descriptor failed")
		writeDescriptorAtomicForRuntime = func(path string, desc *projectdaemon.Descriptor) error {
			writeCalls++
			if writeCalls == 2 {
				return writeGlobalDescriptorErr
			}
			return previousWriteDescriptor(path, desc)
		}
		Expect(runWikidFrontdOwner(context.Background(), cfg, ownerCfg)).To(MatchError(writeGlobalDescriptorErr))
	})

	ginkgo.It("reports direct actor, token, and private endpoint errors", ginkgo.Label("integration"), func() {
		w := newFrontdActorTestWiki()
		ginkgo.DeferCleanup(w.Close)

		actor, err := wikidControlMCPActorResolver("", leafwikiRuntimeConfig{DisableAuth: true})(httptest.NewRequest(http.MethodPost, "/mcp", nil))
		Expect(err).NotTo(HaveOccurred())
		Expect(actor.Subject).To(Equal("user:public-editor"))

		_, err = wikidControlMCPActorResolver(leafwikiTempDir(), leafwikiRuntimeConfig{})(httptest.NewRequest(http.MethodPost, "/mcp", nil))
		Expect(err).To(MatchError(errNativeStdioAPIKeyRequired))
		missingKeyReq := httptest.NewRequest(http.MethodPost, "/mcp", nil)
		missingKeyReq.Header.Set("Authorization", "Bearer lwk_key_missing")
		_, err = wikidControlMCPActorResolver(leafwikiTempDir(), leafwikiRuntimeConfig{})(missingKeyReq)
		Expect(err).To(MatchError(coreauth.ErrInvalidToken))

		userStoreFailureDir := leafwikiTempDir()
		Expect(os.Mkdir(filepath.Join(userStoreFailureDir, "users.db"), 0o755)).To(Succeed())
		_, err = stdioAPIKeyUserFromStorage(userStoreFailureDir, "lwk_key_missing")
		Expect(err).To(MatchSQLitePrimaryError(sqlite3.SQLITE_CANTOPEN))
		apiKeyStoreFailureDir := leafwikiTempDir()
		Expect(os.Mkdir(filepath.Join(apiKeyStoreFailureDir, "api_keys.db"), 0o755)).To(Succeed())
		_, err = stdioAPIKeyUserFromStorage(apiKeyStoreFailureDir, "lwk_key_missing")
		Expect(err).To(MatchSQLitePrimaryError(sqlite3.SQLITE_CANTOPEN))
		_, err = stdioAPIKeyUserFromStorage(leafwikiTempDir(), "lwk_key_missing")
		Expect(err).To(MatchError(coreauth.ErrInvalidToken))

		_, err = frontdMCPTokenVerifier(&wiki.Wiki{})(context.Background(), "lwk_key_missing", httptest.NewRequest(http.MethodPost, "/mcp", nil))
		Expect(err).To(MatchError(sdkauth.ErrInvalidToken))

		noCodeErr := newWikidPrivateEndpointError("/plain", http.StatusBadGateway, nil)
		Expect(noCodeErr.Code).To(BeEmpty())
		Expect(noCodeErr.StatusCode).To(Equal(http.StatusBadGateway))

		err = callWikidPrivateEndpoint(context.Background(), "http://[::1", "token", "/private", nil, nil)
		Expect(err).To(MatchURLError())
		Expect(cloneWithOriginalRequest(nil)).To(BeNil())

		blockingFile := filepath.Join(leafwikiTempDir(), "not-a-dir")
		Expect(os.WriteFile(blockingFile, []byte("x"), 0o600)).To(Succeed())
		badRegistryLayout := wikid.GlobalLayout(leafwikiTempDir())
		badRegistry := wikid.NewRegistryService(wikid.NewRegistryStore(filepath.Join(blockingFile, "registry.db")), badRegistryLayout)
		rec := httptest.NewRecorder()
		handleWikidActorContext(rec, httptest.NewRequest(http.MethodPost, "/__leafwiki/actor-context", nil), w, leafwikiRuntimeConfig{DisableAuth: true, Workspace: wiki.Workspace{ID: "home"}}, badRegistry, nil)
		Expect(rec).To(HaveHTTPStatus(http.StatusInternalServerError))

		badGrantStore := wikid.NewGrantStore(filepath.Join(blockingFile, "grants.db"))
		rec = httptest.NewRecorder()
		handleWikidActorContext(rec, httptest.NewRequest(http.MethodPost, "/__leafwiki/actor-context", nil), w, leafwikiRuntimeConfig{DisableAuth: true, Workspace: wiki.Workspace{ID: "home"}}, nil, badGrantStore)
		Expect(rec).To(HaveHTTPStatus(http.StatusInternalServerError))
		previousGrantsForSubject := grantsForSubjectForRuntime
		previousActorContextForGrant := actorContextForWorkspaceGrantForRuntime
		ginkgo.DeferCleanup(func() {
			grantsForSubjectForRuntime = previousGrantsForSubject
			actorContextForWorkspaceGrantForRuntime = previousActorContextForGrant
		})
		grantsForSubjectForRuntime = func(*wikid.GrantStore, string) ([]wikid.Grant, error) {
			return nil, errors.New("grant lookup failed")
		}
		validGrantStore := wikid.NewGrantStore(filepath.Join(leafwikiTempDir(), "grants.db"))
		rec = httptest.NewRecorder()
		handleWikidActorContext(rec, httptest.NewRequest(http.MethodPost, "/__leafwiki/actor-context", nil), w, leafwikiRuntimeConfig{DisableAuth: true, Workspace: wiki.Workspace{ID: "home"}}, nil, validGrantStore)
		Expect(rec).To(HaveHTTPStatus(http.StatusInternalServerError))
		grantsForSubjectForRuntime = previousGrantsForSubject

		actorContextForWorkspaceGrantForRuntime = func(*coreauth.User, string, leafwikiRuntimeConfig, workspaceid.WorkspaceID, wikid.GrantRole) (projectdaemon.ActorContext, error) {
			return projectdaemon.ActorContext{}, errors.New("actor context failed")
		}
		rec = httptest.NewRecorder()
		handleWikidActorContext(rec, httptest.NewRequest(http.MethodPost, "/__leafwiki/actor-context", nil), w, leafwikiRuntimeConfig{DisableAuth: true, Workspace: wiki.Workspace{ID: "home"}}, nil, nil)
		Expect(rec).To(HaveHTTPStatus(http.StatusInternalServerError))
		actorContextForWorkspaceGrantForRuntime = previousActorContextForGrant

		Expect(seedRuntimeHomeGrants(badGrantStore, leafwikiRuntimeConfig{DisableAuth: true})).To(MatchPathError())
		Expect(seedRuntimeHomeGrants(badGrantStore, leafwikiRuntimeConfig{PublicAccess: true})).To(MatchPathError())
	})
})
