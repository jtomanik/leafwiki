package main

import (
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/onsi/gomega/gstruct"
	coreauth "github.com/perber/wiki/internal/core/auth"
	sharederrors "github.com/perber/wiki/internal/core/shared/errors"
	"github.com/perber/wiki/internal/frontd"
	httpinternal "github.com/perber/wiki/internal/http"
	"github.com/perber/wiki/internal/projectdaemon"
	testmatchers "github.com/perber/wiki/internal/test_utils/matchers"
	"github.com/perber/wiki/internal/wiki"
	"github.com/perber/wiki/internal/wikid"
)

var _ = ginkgo.Describe("daemon STDIO bridge configuration", func() {
	ginkgo.It("prefers descriptor private MCP", ginkgo.Label("unit"), func() {
		desc := &projectdaemon.Descriptor{
			ControlURL:      "http://127.0.0.1:41000",
			ControlToken:    "control-token",
			PrivateMCPURL:   "http://127.0.0.1:42000/mcp",
			PrivateMCPToken: "private-token",
		}

		cfg := daemonStdioBridgeConfig(desc, leafwikiRuntimeConfig{APIKey: "api-key"})
		Expect(cfg).To(SatisfyAll(
			HaveField("EndpointURL", Equal(desc.PrivateMCPURL)),
			HaveField("ControlToken", Equal(desc.PrivateMCPToken)),
			HaveField("AuthControlURL", Equal(desc.ControlURL)),
			HaveField("AuthControlToken", Equal(desc.ControlToken)),
		))

	})
})

var _ = ginkgo.Describe("control-plane router", func() {
	ginkgo.It("registers OAuth when HTTPMCP enabled", ginkgo.Label("integration"), func() {
		dataDir := leafwikiTempDir()
		rootDir := leafwikiTempDir()
		cfg := leafwikiRuntimeConfig{
			Workspace:           wiki.Workspace{ID: newFixtureWorkspaceID("current"), DataDir: dataDir, RootDir: rootDir},
			Host:                "127.0.0.1",
			Port:                "8085",
			AdminPassword:       "admin",
			JWTSecret:           "secret",
			AllowInsecure:       true,
			AccessTokenTimeout:  15 * time.Minute,
			RefreshTokenTimeout: 7 * 24 * time.Hour,
			MCPTransports:       mcpTransports{HTTP: true},
			RuntimeStack:        projectdaemon.RuntimeStackWikidFrontd,
		}
		ownerCfg, err := daemonConfigForRuntime(cfg)
		Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("daemonConfigForRuntime failed: %v", err))

		stores, err := wikid.OpenAuthStores(ownerCfg.DataDir)
		Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("open wikid auth stores: %v", err))

		Expect(stores.Close()).To(Succeed(), fmt.Sprintf("close wikid auth stores: %v", err))
		w, err := newRuntimeWiki(cfg, ownerCfg, runtimeWikiControlPlaneOnly)
		Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("newRuntimeWiki failed: %v", err))

		defer closeBestEffort(w)
		opts, err := controlPlaneRouterOptionsForRuntime(cfg, w)
		Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("controlPlaneRouterOptionsForRuntime failed: %v", err))

		router := frontd.NewRouter(w, opts)

		q := url.Values{
			"client_id":             {"leafwiki-local-mcp"},
			"response_type":         {"code"},
			"redirect_uri":          {"http://127.0.0.1:49152/callback"},
			"scope":                 {"leafwiki:mcp"},
			"state":                 {"control-plane-oauth"},
			"resource":              {"http://127.0.0.1/mcp"},
			"code_challenge":        {"abcdefghijklmnopqrstuvwxyz0123456789abcdefghi"},
			"code_challenge_method": {"S256"},
		}
		req := httptest.NewRequest(http.MethodGet, "http://127.0.0.1/oauth/authorize?"+q.Encode(), nil)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		Expect(rec).To(HaveHTTPStatus(http.StatusFound))

		Expect(rec.Header().Get("Location")).To(HavePrefix("/login?returnTo="))

	})
})

var _ = ginkgo.Describe("frontd actor resolution", func() {
	ginkgo.It("allows public access reads as viewer", ginkgo.Label("integration"), func() {
		w := newFrontdActorTestWiki()
		defer closeBestEffort(w)
		req := httptest.NewRequest(http.MethodGet, "/api/tree", nil)

		user, method, err := frontdActorUser(req, w, leafwikiRuntimeConfig{
			PublicAccess: true,
			Workspace:    wiki.Workspace{ID: newFixtureWorkspaceID("current")},
		})
		Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("frontdActorUser public read failed: %v", err))
		Expect(method).To(Equal(string(leafwikiActorAuthMethodPublicAccess)), fmt.Sprintf("auth method = %q, want public_access", method))
		Expect(user).To(SatisfyAll(
			HaveCoreAuthUserID(newFixtureUserID("public-viewer")),
			HaveField("Role", Equal(coreauth.RoleViewer)),
		), fmt.Sprintf("public actor = %#v, want public viewer", user))

	})
})

var _ = ginkgo.Describe("frontd actor resolution", func() {
	ginkgo.It("honors trusted remote user header", ginkgo.Label("integration"), func() {
		w := newFrontdActorTestWiki()
		defer closeBestEffort(w)
		created, err := w.UserService().CreateUser("editor", "editor@example.com", "password", coreauth.RoleEditor)
		Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("CreateUser failed: %v", err))

		req := httptest.NewRequest(http.MethodGet, "/api/tree", nil)
		req.RemoteAddr = "127.0.0.1:12345"
		req.Header.Set("Remote-User", "editor")

		user, method, err := frontdActorUser(req, w, leafwikiRuntimeConfig{
			EnableHTTPRemoteUser: true,
			HTTPRemoteUserHeader: "Remote-User",
			TrustedProxyIPsRaw:   "127.0.0.1",
			Workspace:            wiki.Workspace{ID: newFixtureWorkspaceID("current")},
		})
		Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("frontdActorUser remote user failed: %v", err))
		Expect(method).To(Equal(string(leafwikiActorAuthMethodRemoteUser)), fmt.Sprintf("auth method = %q, want remote_user", method))
		Expect(user).To(SatisfyAll(
			HaveCoreAuthUserID(coreauth.UserIDFromString(created.ID)),
			HaveField("Username", Equal("editor")),
			HaveField("Role", Equal(coreauth.RoleEditor)),
		), fmt.Sprintf("remote actor = %#v, want created editor %#v", user, created))

	})
})

var _ = ginkgo.Describe("frontd actor resolution", func() {
	ginkgo.It("rejects MCPAPI key for workspace API", ginkgo.Label("integration"), func() {
		w := newFrontdActorTestWiki()
		defer closeBestEffort(w)
		editor, err := w.UserService().CreateUser("mcp-editor", "mcp-editor@example.com", "password", coreauth.RoleEditor)
		Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("CreateUser failed: %v", err))

		editorID := coreauth.UserIDFromString(editor.ID)
		created, err := w.APIKeyService().CreateAPIKey(editorID, "MCP client", editorID)
		Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("CreateAPIKey failed: %v", err))

		req := httptest.NewRequest(http.MethodPost, "/api/pages", strings.NewReader(`{"title":"Via API key"}`))
		req.Header.Set("Authorization", "Bearer "+created.Secret)

		user, method, err := frontdActorUser(req, w, leafwikiRuntimeConfig{
			Workspace: wiki.Workspace{ID: newFixtureWorkspaceID("current")},
		})
		Expect(err).To(MatchError(errFrontdWorkspaceCredentialsMissing), fmt.Sprintf("frontdActorUser allowed MCP API key as workspace user %#v with method %q, want workspace credentials error", user, method))

	})
})

var _ = ginkgo.Describe("frontd frontend configuration", func() {
	ginkgo.It("uses branding for spahtml", ginkgo.Label("integration"), func() {
		dataDir := leafwikiTempDir()
		Expect(os.WriteFile(filepath.Join(dataDir, "branding.json"), []byte(`{"siteName":"Runtime Wiki","faviconFile":"favicon.ico"}`), 0o644)).To(Succeed())
		embedFrontendOrig := httpinternal.EmbedFrontend
		httpinternal.EmbedFrontend = "true"
		ginkgo.DeferCleanup(func() {
			httpinternal.EmbedFrontend = embedFrontendOrig
		})
		router := httpinternal.NewRouter(nil, frontendConfigForRuntimeStorage(dataDir), httpinternal.RouterOptions{})

		req := httptest.NewRequest(http.MethodGet, "/page", nil)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		Expect(rec).To(HaveHTTPStatus(http.StatusOK))

		body := rec.Body.String()
		Expect(body).To(ContainSubstring("<title>Runtime Wiki</title>"), fmt.Sprintf("SPA title did not use branding: %s", body))
		Expect(body).To(ContainSubstring(`href="/branding/favicon.ico"`), fmt.Sprintf("SPA favicon did not use branding: %s", body))

	})
})

var _ = ginkgo.Describe("wikid/frontd runtime", func() {
	ginkgo.It("with process lock serializes process map access", ginkgo.Label("unit"), func() {
		runtime := &wikidFrontdRuntime{}
		runtime.mu.Lock()
		entered := make(chan struct{})
		done := make(chan struct{})
		go func() {
			_ = runtime.withProcessLock(func() error {
				close(entered)
				return nil
			})
			close(done)
		}()

		Consistently(entered).WithTimeout(25 * time.Millisecond).ShouldNot(Receive())
		Consistently(done).WithTimeout(25 * time.Millisecond).ShouldNot(Receive())

		runtime.mu.Unlock()
		Eventually(entered).WithTimeout(time.Second).Should(BeClosed())
		Eventually(done).WithTimeout(time.Second).Should(BeClosed())

	})
})

var _ = ginkgo.Describe("frontd public MCP proxy", func() {
	ginkgo.It("requires bearer before proxying", ginkgo.Label("integration"), func() {
		upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			w.Header().Set("X-LeafWiki-Upstream", "workspaced")
			w.WriteHeader(http.StatusAccepted)
		}))
		defer upstream.Close()

		handler, err := frontdPublicMCPHandler(leafwikiRuntimeConfig{
			BasePath:  "",
			Workspace: wiki.Workspace{ID: newFixtureWorkspaceID("current")},
		}, upstream.URL, "private-token", "http://127.0.0.1:1")
		Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("frontdPublicMCPHandler failed: %v", err))

		req := httptest.NewRequest(http.MethodPost, "http://leafwiki.local/mcp", strings.NewReader("{}"))
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		Expect(rec).To(HaveHTTPStatus(http.StatusUnauthorized))
		Expect(rec).To(HaveHTTPHeaderWithValue("WWW-Authenticate", ContainSubstring(`resource_metadata="http://leafwiki.local/.well-known/oauth-protected-resource/mcp"`)))
		Expect(rec).NotTo(HaveHTTPHeaderWithValue("X-LeafWiki-Upstream", "workspaced"))

	})
})

var _ = ginkgo.Describe("frontd workspace MCP proxy", func() {
	ginkgo.It("requires bearer before proxying", ginkgo.Label("integration"), func() {
		handler := frontdMCPBearerAuthHandler(
			leafwikiRuntimeConfig{
				BasePath:  "",
				Workspace: wiki.Workspace{ID: newFixtureWorkspaceID("current")},
			},
			"http://127.0.0.1:1",
			"private-token",
			http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("X-LeafWiki-Workspace-Router", "reached")
				w.WriteHeader(http.StatusAccepted)
			}),
		)

		req := httptest.NewRequest(http.MethodPost, "http://leafwiki.local/mcp/workspaces/docs", strings.NewReader("{}"))
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		Expect(rec).To(HaveHTTPStatus(http.StatusUnauthorized))
		Expect(rec).To(HaveHTTPHeaderWithValue("WWW-Authenticate", ContainSubstring(`resource_metadata="http://leafwiki.local/.well-known/oauth-protected-resource/mcp"`)))
		Expect(rec).NotTo(HaveHTTPHeaderWithValue("X-LeafWiki-Workspace-Router", "reached"))

	})
})

var _ = ginkgo.Describe("workspace MCP unavailable handler", func() {
	ginkgo.It("returns structured error", ginkgo.Label("integration"), func() {
		rec := httptest.NewRecorder()

		workspaceMCPUnavailableHandler().ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/mcp/workspaces/docs", nil))

		Expect(rec).To(testmatchers.HaveHTTPStructuredError(http.StatusServiceUnavailable, runtimeErrorCodeMCPWorkspaceUnavailable, sharederrors.MessageIDForCode(runtimeErrorCodeMCPWorkspaceUnavailable)))

	})
})

var _ = ginkgo.Describe("private MCP unauthorized response", func() {
	ginkgo.It("returns structured error", ginkgo.Label("integration"), func() {
		rec := httptest.NewRecorder()

		writePrivateMCPUnauthorized(rec)

		Expect(rec).To(testmatchers.HaveHTTPStructuredError(http.StatusUnauthorized, runtimeErrorCodePrivateMCPControlTokenInvalid, sharederrors.MessageIDForCode(runtimeErrorCodePrivateMCPControlTokenInvalid)))

	})
})

var _ = ginkgo.Describe("local-only HTTP MCP handler", func() {
	ginkgo.It("rejects non loopback requests", ginkgo.Label("integration"), func() {
		calls := 0
		handler := localOnlyHTTPMCPHandler(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			calls++
			w.WriteHeader(http.StatusNoContent)
		}))

		remoteReq := httptest.NewRequest(http.MethodPost, "/mcp", nil)
		remoteReq.RemoteAddr = "100.64.0.10:12345"
		remoteRec := httptest.NewRecorder()
		handler.ServeHTTP(remoteRec, remoteReq)
		Expect(remoteRec).To(HaveHTTPStatus(http.StatusNotFound))
		Expect(calls).To(BeZero(), fmt.Sprintf("remote /mcp reached handler %d times, want 0", calls))

		loopbackReq := httptest.NewRequest(http.MethodPost, "/mcp", nil)
		loopbackReq.RemoteAddr = "127.0.0.1:12345"
		loopbackRec := httptest.NewRecorder()
		handler.ServeHTTP(loopbackRec, loopbackReq)
		Expect(loopbackRec).To(HaveHTTPStatus(http.StatusNoContent))
		Expect(calls).To(Equal(1), fmt.Sprintf("loopback /mcp reached handler %d times, want 1", calls))

	})
})

var _ = ginkgo.Describe("wikid control MCP actor resolver", func() {
	ginkgo.It("loads API key user from wikid auth store", ginkgo.Label("integration"), func() {
		authDir := leafwikiTempDir()
		userStore, err := coreauth.NewUserStore(authDir)
		Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("NewUserStore failed: %v", err))

		defer closeBestEffort(userStore)
		userService := coreauth.NewUserService(userStore)
		editor, err := userService.CreateUser("editor", "editor@example.com", "password", coreauth.RoleEditor)
		Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("CreateUser failed: %v", err))

		apiKeyStore, err := coreauth.NewAPIKeyStore(authDir)
		Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("NewAPIKeyStore failed: %v", err))

		apiKeyService := coreauth.NewAPIKeyService(apiKeyStore, userService)
		defer closeBestEffort(apiKeyService)
		editorID := coreauth.UserIDFromString(editor.ID)
		created, err := apiKeyService.CreateAPIKey(editorID, "Native STDIO", editorID)
		Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("CreateAPIKey failed: %v", err))

		req := httptest.NewRequest(http.MethodPost, "/mcp", nil)
		req.Header.Set("Authorization", "Bearer "+created.Secret)
		actor, err := wikidControlMCPActorResolver(authDir, leafwikiRuntimeConfig{
			Workspace: wiki.Workspace{ID: newFixtureWorkspaceID("current")},
		})(req)
		Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("wikidControlMCPActorResolver failed: %v", err))
		Expect(actor).To(SatisfyAll(
			HaveActorSubjectForUser(coreauth.UserIDFromString(editor.ID)),
			HaveField("Username", Equal("editor")),
			HaveActorAuthMethod(leafwikiActorAuthMethodAPIKey),
		), fmt.Sprintf("actor = %#v, want API-key editor actor", actor))

	})
})

var _ = ginkgo.Describe("federated first-contact registration", func() {
	ginkgo.It("grants the native STDIO API key editor access to a new federated workspace", ginkgo.Label("integration"), func() {
		baseDir := leafwikiTempDir()
		layout := wikid.GlobalLayout(filepath.Join(baseDir, ".leafwiki"))
		authDir := wikid.AuthStoragePaths(layout.HomeDir).AuthDir
		apiKey := createMCPAPIKeyInStorageDir(authDir)
		requestCfg := projectdaemon.Config{
			DataDir: filepath.Join(baseDir, "workspace-data"),
			RootDir: filepath.Join(baseDir, "workspace-root"),
		}
		Expect(os.MkdirAll(requestCfg.DataDir, 0o755)).To(Succeed())
		Expect(os.MkdirAll(requestCfg.RootDir, 0o755)).To(Succeed())

		firstContact := registerFederatedFirstContactResult(layout, requestCfg, leafwikiRuntimeConfig{
			RuntimeStack:  projectdaemon.RuntimeStackWikidFrontd,
			MCPTransports: mcpTransports{Stdio: true},
			APIKey:        apiKey,
		})
		workspace := firstContact.Workspace
		Expect(firstContact).To(MatchFederatedRegisteredWorkspace(gstruct.Fields{}), fmt.Sprintf("registered first-contact workspace = %#v, home=%t", workspace, firstContact.Home))
		Expect(workspace).To(MatchFederatedWorkspacePathIdentity(requestCfg.DataDir, requestCfg.RootDir), fmt.Sprintf("registered first-contact workspace paths = %#v", workspace))

		doc, err := wikid.NewGrantStore(layout.DBPath).Load()
		Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("load grants failed: %v", err))

		Expect(doc.Grants).To(ContainElement(HaveWikidGrantForWorkspace(workspace.ID, wikid.GrantRoleEditor)), fmt.Sprintf("grants = %#v, want editor grant for first-contact workspace %q", doc.Grants, workspace.ID))

	})
})

var _ = ginkgo.Describe("federated first-contact registration", func() {
	ginkgo.It("reuses an existing federated workspace without adding a native STDIO grant", ginkgo.Label("integration"), func() {
		baseDir := leafwikiTempDir()
		layout := wikid.GlobalLayout(filepath.Join(baseDir, ".leafwiki"))
		authDir := wikid.AuthStoragePaths(layout.HomeDir).AuthDir
		apiKey := createMCPAPIKeyInStorageDir(authDir)
		requestCfg := projectdaemon.Config{
			DataDir: filepath.Join(baseDir, "workspace-data"),
			RootDir: filepath.Join(baseDir, "workspace-root"),
		}
		registry := wikid.NewRegistryService(wikid.NewRegistryStore(layout.DBPath), layout)
		existing, err := registry.RegisterWorkspace(wikid.RegisterWorkspaceRequest{
			DisplayName: federatedWorkspaceDisplayName(requestCfg),
			DataDir:     requestCfg.DataDir,
			RootDir:     requestCfg.RootDir,
		})
		Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("RegisterWorkspace failed: %v", err))

		firstContact := registerFederatedFirstContactResult(layout, requestCfg, leafwikiRuntimeConfig{
			RuntimeStack:  projectdaemon.RuntimeStackWikidFrontd,
			MCPTransports: mcpTransports{Stdio: true},
			APIKey:        apiKey,
		})
		workspace := firstContact.Workspace
		Expect(firstContact).To(MatchFederatedRegisteredWorkspace(gstruct.Fields{
			"ID": Equal(existing.ID),
		}), fmt.Sprintf("registered existing workspace = %#v, home=%t, want existing %q", workspace, firstContact.Home, existing.ID))

		doc, err := wikid.NewGrantStore(layout.DBPath).Load()
		Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("load grants failed: %v", err))
		Expect(doc.Grants).To(HaveLen(0), fmt.Sprintf("grants = %#v, want no self-grant for existing workspace %q", doc.Grants, workspace.ID))

	})
})

var _ = ginkgo.Describe("federated workspace ensure", func() {
	ginkgo.It("preserves structured grant denial", ginkgo.Label("integration"), func() {
		control := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			if req.URL.Path != "/__leafwiki/workspaces/workspace-b/ensure" {
				http.NotFound(w, req)
				return
			}
			Expect(req.Header).To(HaveKeyWithValue(http.CanonicalHeaderKey(projectdaemon.ControlTokenHeader), ContainElement("control-token")))

			writeRuntimeError(w, http.StatusForbidden, runtimeErrorCodeWorkspaceGrantDenied)
		}))
		ginkgo.DeferCleanup(control.Close)

		err := ensureFederatedWorkspace(context.Background(), &projectdaemon.Descriptor{
			ControlURL:    control.URL,
			ControlToken:  "control-token",
			SchemaVersion: projectdaemon.DescriptorSchemaVersion,
		}, newFixtureWorkspaceID("workspace-b"), leafwikiRuntimeConfig{
			APIKey: "valid-but-ungranted-key",
		})
		Expect(err).To(MatchWikidPrivateEndpoint(http.StatusForbidden, runtimeErrorCodeWorkspaceGrantDenied))

	})
})

var _ = ginkgo.Describe("runtime error responses", func() {
	ginkgo.It("renders message from catalog", ginkgo.Label("integration"), func() {

		rec := httptest.NewRecorder()
		writeRuntimeError(rec, http.StatusForbidden, runtimeErrorCodeWorkspaceGrantDenied)
		Expect(rec).To(HaveHTTPStatus(http.StatusForbidden))

		var body runtimeErrorResponse
		Expect(json.Unmarshal(rec.Body.Bytes(), &body)).To(Succeed())
		Expect(body).To(testmatchers.HaveStructuredError(runtimeErrorCodeWorkspaceGrantDenied, sharederrors.MessageIDForCode(runtimeErrorCodeWorkspaceGrantDenied)))

	})
})

var _ = ginkgo.Describe("federated first-contact registration", func() {
	ginkgo.It("persists markdown link root prefix", ginkgo.Label("integration"), func() {
		baseDir := leafwikiTempDir()
		layout := wikid.GlobalLayout(filepath.Join(baseDir, ".leafwiki"))
		requestCfg := projectdaemon.Config{
			DataDir: filepath.Join(baseDir, "workspace-data"),
			RootDir: filepath.Join(baseDir, "workspace-root"),
		}
		cfg := testRuntimeConfig(requestCfg.DataDir, requestCfg.RootDir, freeTCPPort(), mcpTransports{Stdio: true}, true)
		cfg.RuntimeStack = projectdaemon.RuntimeStackWikidFrontd
		cfg.MarkdownLinkRootPrefix = "/docs"

		firstContact := registerFederatedFirstContactResult(layout, requestCfg, cfg)
		workspace := firstContact.Workspace
		Expect(firstContact).To(MatchFederatedRegisteredWorkspace(gstruct.Fields{
			"MarkdownLinkRootPrefix": Equal("/docs"),
		}), fmt.Sprintf("registered markdown-prefixed workspace = %#v, home=%t", workspace, firstContact.Home))

		loaded, err := registryWorkspaceResult(wikid.NewRegistryService(wikid.NewRegistryStore(layout.DBPath), layout), workspace.ID)
		Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("load workspace: %v", err))
		Expect(loaded.MarkdownLinkRootPrefix).To(Equal("/docs"), fmt.Sprintf("persisted markdown link root prefix = %q, want /docs", loaded.MarkdownLinkRootPrefix))

	})
})

var _ = ginkgo.Describe("federated workspace runtime configuration", func() {
	ginkgo.It("restores markdown link root prefix", ginkgo.Label("integration"), func() {
		baseDir := leafwikiTempDir()
		dataDir := filepath.Join(baseDir, "workspace-data")
		rootDir := filepath.Join(baseDir, "workspace-root")
		manager := &federatedWorkspaceManager{
			base: testRuntimeConfig(filepath.Join(baseDir, "home"), filepath.Join(baseDir, "home-root"), freeTCPPort(), mcpTransports{}, true),
		}

		cfg := manager.workspaceRuntimeConfig(wikid.WorkspaceRecord{
			ID:                     newFixtureWorkspaceID("docs"),
			DataDir:                dataDir,
			RootDir:                rootDir,
			MarkdownLinkRootPrefix: "/docs",
		}, "41000")
		Expect(cfg.MarkdownLinkRootPrefix).To(Equal("/docs"), fmt.Sprintf("runtime markdown link root prefix = %q, want /docs", cfg.MarkdownLinkRootPrefix))

	})
})
