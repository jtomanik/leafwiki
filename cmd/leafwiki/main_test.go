package main

import (
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/onsi/gomega/gstruct"
	"github.com/onsi/gomega/types"
	"github.com/perber/wiki/internal/agenthooks"
	coreauth "github.com/perber/wiki/internal/core/auth"
	sharederrors "github.com/perber/wiki/internal/core/shared/errors"
	"github.com/perber/wiki/internal/frontd"
	httpinternal "github.com/perber/wiki/internal/http"
	"github.com/perber/wiki/internal/localization"
	"github.com/perber/wiki/internal/locking"
	leaflogging "github.com/perber/wiki/internal/logging"
	"github.com/perber/wiki/internal/projectdaemon"
	"github.com/perber/wiki/internal/runtimeconfig"
	testmatchers "github.com/perber/wiki/internal/test_utils/matchers"
	"github.com/perber/wiki/internal/wiki"
	wikimcp "github.com/perber/wiki/internal/wiki/mcp"
	"github.com/perber/wiki/internal/wikid"
	"github.com/perber/wiki/internal/workspaceid"
)

const (
	leafwikiStartupLogMessage          = "Starting LeafWiki"
	leafwikiHTTPRequestLogMessage      = "http request"
	leafwikiDataDirectoryCreatedLogMsg = "Data directory created"
	leafwikiMCPStdioFailedLogMessage   = "MCP STDIO failed"
	leafwikiInvalidNativeStdioAPIKey   = "invalid native STDIO API key"
	leafwikiNativeStdioPositionalCmd   = "native STDIO does not support positional commands"
	leafwikiRootDirLockHeldMessage     = "root directory is already in use"
)

func TestLeafWikiSuite(t *testing.T) {
	if runLeafWikiHelperProcessForTest() {
		return
	}
	RegisterFailHandler(ginkgo.Fail)
	ginkgo.RunSpecs(t, "LeafWiki Suite")
}

func leafwikiTempDir() string {
	ginkgo.GinkgoHelper()
	dir, err := os.MkdirTemp("", "leafwiki-test-*")
	Expect(err).NotTo(HaveOccurred())
	ginkgo.DeferCleanup(os.RemoveAll, dir)
	return dir
}

func leafwikiSetenv(key string, value string) {
	ginkgo.GinkgoHelper()
	previous, existed := os.LookupEnv(key)
	Expect(os.Setenv(key, value)).To(Succeed())
	ginkgo.DeferCleanup(func() {
		if existed {
			Expect(os.Setenv(key, previous)).To(Succeed())
			return
		}
		Expect(os.Unsetenv(key)).To(Succeed())
	})
}

func localizedMessage(messageID string, args ...string) string {
	return localization.English.Render(messageID, "", args...).Message
}

var (
	errAgentHookEventRejected           = errors.New("agent hook event rejected")
	errAgentHookProviderAbsent          = errors.New("agent hook provider absent")
	errBoolValueRejected                = errors.New("bool value rejected")
	errDurationValueRejected            = errors.New("duration value rejected")
	errFederatedWorkspaceAbsent         = errors.New("federated workspace absent")
	errFrontdRemoteUserAbsent           = errors.New("frontd remote user absent")
	errProjectDaemonDescriptorUnhealthy = errors.New("project daemon descriptor unhealthy")
	errRelativePathOutsideBase          = errors.New("relative path outside base")
	errRegistryWorkspaceAbsent          = errors.New("registry workspace absent")
	errRuntimeRoleAbsent                = errors.New("runtime role absent")
	errWorkspaceGrantAbsent             = errors.New("workspace grant absent")
)

func agentHookProviderFromArgsResult(args []string) (agenthooks.ProviderID, error) {
	provider, ok := agentHookProviderFromArgs(args)
	if !ok {
		return "", errAgentHookProviderAbsent
	}
	return provider, nil
}

func agentHookProviderFromRawArgsResult(args []string) (agenthooks.ProviderID, error) {
	provider, ok := agentHookProviderFromRawArgs(args)
	if !ok {
		return "", errAgentHookProviderAbsent
	}
	return provider, nil
}

func normalizedAgentHookEventResult(provider agenthooks.ProviderID, raw []byte, seenAt time.Time) (agenthooks.Event, error) {
	event, accepted := agenthooks.Normalize(provider, raw, seenAt)
	if !accepted {
		return agenthooks.Event{}, errAgentHookEventRejected
	}
	return event, nil
}

func parseBoolResult(raw string) (bool, error) {
	value, ok := parseBool(raw)
	if !ok {
		return false, errBoolValueRejected
	}
	return value, nil
}

func parseDurationResult(raw string) (time.Duration, error) {
	value, ok := parseDuration(raw)
	if !ok {
		return 0, errDurationValueRejected
	}
	return value, nil
}

func localRelativePathResult(base string, target string) (string, error) {
	path, ok := localRelativePath(base, target)
	if !ok {
		return "", errRelativePathOutsideBase
	}
	return path, nil
}

func runtimeRoleHealthResult(roles []projectdaemon.RoleHealth, name projectdaemon.RoleName) (projectdaemon.RoleHealth, error) {
	role, ok := findRuntimeRoleHealth(roles, name)
	if !ok {
		return projectdaemon.RoleHealth{}, errRuntimeRoleAbsent
	}
	return role, nil
}

func registeredFederatedWorkspaceForRequestResult(layout wikid.Layout, requestCfg projectdaemon.Config) (wikid.WorkspaceRecord, error) {
	workspace, ok, err := registeredFederatedWorkspaceForRequest(layout, requestCfg)
	if err != nil {
		return wikid.WorkspaceRecord{}, err
	}
	if !ok {
		return wikid.WorkspaceRecord{}, errFederatedWorkspaceAbsent
	}
	return workspace, nil
}

func federatedStdioAPIKeyWorkspaceGrantResult(layout wikid.Layout, cfg leafwikiRuntimeConfig, workspaceID workspaceid.WorkspaceID) (wikid.Grant, error) {
	grant, ok, err := federatedStdioAPIKeyWorkspaceGrant(layout, cfg, workspaceID)
	if err != nil {
		return wikid.Grant{}, err
	}
	if !ok {
		return wikid.Grant{}, errWorkspaceGrantAbsent
	}
	return grant, nil
}

func frontdRemoteUserResult(req *http.Request, w *wiki.Wiki, cfg leafwikiRuntimeConfig) (*coreauth.User, string, error) {
	user, method, present, err := frontdRemoteUser(req, w, cfg)
	if err != nil {
		return user, method, err
	}
	if !present {
		return user, method, errFrontdRemoteUserAbsent
	}
	return user, method, nil
}

func registryWorkspaceResult(registry *wikid.RegistryService, id workspaceid.WorkspaceID) (wikid.WorkspaceRecord, error) {
	workspace, ok, err := registry.Workspace(id)
	if err != nil {
		return wikid.WorkspaceRecord{}, err
	}
	if !ok {
		return wikid.WorkspaceRecord{}, errRegistryWorkspaceAbsent
	}
	return workspace, nil
}

func readHealthyProjectDaemonResult(ctx context.Context, descriptorPath string, ownerCfg projectdaemon.Config) (*projectdaemon.Descriptor, error) {
	desc, healthy, err := readHealthyProjectDaemon(ctx, descriptorPath, ownerCfg)
	if err != nil {
		return desc, err
	}
	if !healthy {
		return desc, errProjectDaemonDescriptorUnhealthy
	}
	return desc, nil
}

func projectDaemonDescriptorHealthyResult(ctx context.Context, desc *projectdaemon.Descriptor) error {
	healthy, err := projectDaemonDescriptorHealthy(ctx, desc)
	if err != nil {
		return err
	}
	if !healthy {
		return errProjectDaemonDescriptorUnhealthy
	}
	return nil
}

func haveDaemonStdioBridgeHTTPClient(controlToken string, bearerToken string) types.GomegaMatcher {
	ginkgo.GinkgoHelper()

	return SatisfyAll(
		Not(BeNil()),
		HaveField("Timeout", BeZero()),
		HaveField("Transport", WithTransform(func(transport http.RoundTripper) (projectdaemon.AuthRoundTripper, error) {
			authTransport, ok := transport.(projectdaemon.AuthRoundTripper)
			if !ok {
				return projectdaemon.AuthRoundTripper{}, fmt.Errorf("expected projectdaemon.AuthRoundTripper, got %T", transport)
			}
			return authTransport, nil
		}, SatisfyAll(
			HaveField("ControlToken", Equal(controlToken)),
			HaveField("BearerToken", Equal(bearerToken)),
		))),
	)
}

func newFixtureUserID[T ~string](raw T) coreauth.UserID {
	return coreauth.UserIDFromString(raw)
}

var _ = ginkgo.Describe("leafwiki usage output", func() {
	ginkgo.It("documents MCP transport selector", func() {
		var buf bytes.Buffer

		writeUsage(&buf)

		output := buf.String()
		Expect(output).To(ContainSubstring("Usage: leafwiki [command]"), fmt.Sprintf("expected usage output to include catalog-backed usage line, got %q", output))
		Expect(output).To(ContainSubstring("leafwiki --jwt-secret <SECRET> --admin-password <PASSWORD> [--host <HOST>] [--port <PORT>] [--data-dir <DIR>] [--root-dir <DIR>]"), fmt.Sprintf("expected authenticated startup usage to include --root-dir, got %q", output))

		for _, expected := range []string{
			"--jwt-secret",
			"--admin-password",
			"--allow-insecure",
			"--data-dir",
			"--root-dir",
			"--log-target",
			"--log-file",
			"--mcp",
			"--api-key",
			"Federated runtime idle timeout",
			"leafwiki daemon reads ~/.leafwiki/leafwiki.yml",
			"--config",
			"leafwiki agent-hook <codex|claude|cursor|unknown>",
			"LEAFWIKI_ROOT_DIR",
			"LEAFWIKI_LOG_TARGET",
			"LEAFWIKI_LOG_FILE",
			"LEAFWIKI_MCP",
			"LEAFWIKI_MCP_API_KEY",
		} {
			Expect(output).To(ContainSubstring(expected), fmt.Sprintf("expected usage output to contain %q, got %q", expected, output))

		}
		for _, removed := range []string{
			"--enable-revision",
			"--enable-workspace-sync",
			"--max-revision-history",
			"--enable-mcp",
			"--mcp-stdio",
			"LEAFWIKI_ENABLE_REVISION",
			"LEAFWIKI_ENABLE_WORKSPACE_SYNC",
			"LEAFWIKI_MAX_REVISION_HISTORY",
			"LEAFWIKI_RUNTIME_STACK",
			"LEAFWIKI_ENABLE_MCP",
			"LEAFWIKI_MCP_STDIO",
		} {
			Expect(output).NotTo(ContainSubstring(removed), fmt.Sprintf("usage output contains removed MCP option %q: %q", removed, output))

		}

	})
})

var _ = ginkgo.Describe("leafwiki usage output", func() {
	ginkgo.It("renders help body from catalog", func() {
		var buf bytes.Buffer

		writeUsage(&buf)

		rendered := localization.English.Render("cli.help.body", "").Message
		Expect(rendered).NotTo(BeEmpty(), fmt.Sprintf("cli.help.body rendered empty"))
		Expect(buf.String()).To(ContainSubstring(rendered), fmt.Sprintf("usage output did not include catalog help body"))

	})
})

// Plantrace evidence: TestFailureMessageRendersCatalogBackedErrorBody.
var _ = ginkgo.Describe("CLI failure messages", func() {
	ginkgo.It("renders catalog backed error body", func() {
		got := failureMessage("cli.error.invalid_environment", "error", "bad env")
		want := "Invalid environment error=bad env"
		Expect(got).To(Equal(want), fmt.Sprintf("failureMessage = %q, want %q", got, want))

	})
})

var _ = ginkgo.Describe("CLI flag registration", func() {
	ginkgo.It("rejects removed startup flags", func() {
		for _, arg := range []string{
			"--enable-revision",
			"--enable-workspace-sync",
			"--enable-mcp",
			"--mcp-stdio",
			"--max-revision-history=0",
		} {
			func() {
				_ = arg
				flagName := removedStartupFlagName(arg)
				fs := flag.NewFlagSet("leafwiki-test", flag.ContinueOnError)
				var errOut bytes.Buffer
				fs.SetOutput(&errOut)
				registerFlags(fs)

				err := fs.Parse([]string{arg})
				Expect(err).To(HaveOccurred(), fmt.Sprintf("parse %s unexpectedly succeeded", arg))
				Expect(fs.Lookup(flagName)).To(BeNil(), fmt.Sprintf("removed flag %s is still registered", flagName))
				Expect(errOut.String()).To(ContainSubstring(flagName), fmt.Sprintf("parse %s stderr=%q, want removed flag name %s", arg, errOut.String(), flagName))

			}()
		}

	})
})

var _ = ginkgo.Describe("removed LeafWiki environment validation", func() {
	ginkgo.DescribeTable("rejects removed runtime environment variables",
		func(name string) {
			leafwikiSetenv(name, "")

			err := rejectRemovedLeafWikiEnv()
			Expect(err).To(HaveOccurred(), fmt.Sprintf("rejectRemovedLeafWikiEnv with %s unexpectedly succeeded", name))

			Expect(err).To(MatchError(removedEnvironmentVariableError{Name: name}))
		},
		ginkgo.Entry("runtime stack", "LEAFWIKI_RUNTIME_STACK"),
		ginkgo.Entry("enable revision", "LEAFWIKI_ENABLE_REVISION"),
		ginkgo.Entry("enable workspace sync", "LEAFWIKI_ENABLE_WORKSPACE_SYNC"),
		ginkgo.Entry("max revision history", "LEAFWIKI_MAX_REVISION_HISTORY"),
		ginkgo.Entry("enable MCP", "LEAFWIKI_ENABLE_MCP"),
		ginkgo.Entry("MCP stdio", "LEAFWIKI_MCP_STDIO"),
	)
})

var _ = ginkgo.Describe("daemon runtime configuration", func() {
	ginkgo.It("includes workspace ID", func() {
		cfg := leafwikiRuntimeConfig{
			Workspace: wiki.Workspace{
				ID:      "home",
				DataDir: filepath.Join(leafwikiTempDir(), "data"),
				RootDir: filepath.Join(leafwikiTempDir(), "root"),
			},
			RuntimeStack: projectdaemon.RuntimeStackWikidFrontd,
		}

		ownerCfg, err := daemonConfigForRuntime(cfg)
		Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("daemonConfigForRuntime failed: %v", err))
		Expect(ownerCfg.WorkspaceID).To(Equal(wikid.HomeWorkspaceID), fmt.Sprintf("WorkspaceID = %q, want home", ownerCfg.WorkspaceID))

	})
})

var _ = ginkgo.Describe("workspace resolution", func() {
	ginkgo.It("defaults to home workspace ID", func() {
		fs := flag.NewFlagSet("leafwiki-test", flag.ContinueOnError)
		fs.SetOutput(io.Discard)
		flags := registerFlags(fs)
		Expect(fs.Parse([]string{
			"--data-dir", filepath.Join(leafwikiTempDir(), "data"),
			"--root-dir", filepath.Join(leafwikiTempDir(), "root"),
		})).To(Succeed())
		visited := map[string]bool{}
		fs.Visit(func(f *flag.Flag) { visited[f.Name] = true })

		workspace, err := resolveWorkspace(flags, visited)
		Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("resolveWorkspace failed: %v", err))
		Expect(workspace.ID).To(Equal(wikid.HomeWorkspaceID), fmt.Sprintf("workspace ID = %q, want home", workspace.ID))

	})
})

var _ = ginkgo.Describe("daemon STDIO bridge configuration", func() {
	ginkgo.It("prefers descriptor private MCP", func() {
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
	ginkgo.It("registers OAuth when HTTPMCP enabled", func() {
		dataDir := leafwikiTempDir()
		rootDir := leafwikiTempDir()
		cfg := leafwikiRuntimeConfig{
			Workspace:           wiki.Workspace{ID: "current", DataDir: dataDir, RootDir: rootDir},
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

		defer w.Close()
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
	ginkgo.It("allows public access reads as viewer", func() {
		w := newFrontdActorTestWiki()
		defer w.Close()
		req := httptest.NewRequest(http.MethodGet, "/api/tree", nil)

		user, method, err := frontdActorUser(req, w, leafwikiRuntimeConfig{
			PublicAccess: true,
			Workspace:    wiki.Workspace{ID: "current"},
		})
		Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("frontdActorUser public read failed: %v", err))
		Expect(method).To(Equal("public_access"), fmt.Sprintf("auth method = %q, want public_access", method))
		Expect(user).To(SatisfyAll(
			HaveField("ID", Equal("public-viewer")),
			HaveField("Role", Equal(coreauth.RoleViewer)),
		), fmt.Sprintf("public actor = %#v, want public viewer", user))

	})
})

var _ = ginkgo.Describe("frontd actor resolution", func() {
	ginkgo.It("honors trusted remote user header", func() {
		w := newFrontdActorTestWiki()
		defer w.Close()
		created, err := w.UserService().CreateUser("editor", "editor@example.com", "password", coreauth.RoleEditor)
		Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("CreateUser failed: %v", err))

		req := httptest.NewRequest(http.MethodGet, "/api/tree", nil)
		req.RemoteAddr = "127.0.0.1:12345"
		req.Header.Set("Remote-User", "editor")

		user, method, err := frontdActorUser(req, w, leafwikiRuntimeConfig{
			EnableHTTPRemoteUser: true,
			HTTPRemoteUserHeader: "Remote-User",
			TrustedProxyIPsRaw:   "127.0.0.1",
			Workspace:            wiki.Workspace{ID: "current"},
		})
		Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("frontdActorUser remote user failed: %v", err))
		Expect(method).To(Equal("remote_user"), fmt.Sprintf("auth method = %q, want remote_user", method))
		Expect(user).To(SatisfyAll(
			HaveField("ID", Equal(created.ID)),
			HaveField("Username", Equal("editor")),
			HaveField("Role", Equal(coreauth.RoleEditor)),
		), fmt.Sprintf("remote actor = %#v, want created editor %#v", user, created))

	})
})

var _ = ginkgo.Describe("frontd actor resolution", func() {
	ginkgo.It("rejects MCPAPI key for workspace API", func() {
		w := newFrontdActorTestWiki()
		defer w.Close()
		editor, err := w.UserService().CreateUser("mcp-editor", "mcp-editor@example.com", "password", coreauth.RoleEditor)
		Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("CreateUser failed: %v", err))

		editorID := newFixtureUserID(editor.ID)
		created, err := w.APIKeyService().CreateAPIKey(editorID, "MCP client", editorID)
		Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("CreateAPIKey failed: %v", err))

		req := httptest.NewRequest(http.MethodPost, "/api/pages", strings.NewReader(`{"title":"Via API key"}`))
		req.Header.Set("Authorization", "Bearer "+created.Secret)

		user, method, err := frontdActorUser(req, w, leafwikiRuntimeConfig{
			Workspace: wiki.Workspace{ID: "current"},
		})
		Expect(err).To(HaveOccurred(), fmt.Sprintf("frontdActorUser allowed MCP API key as workspace user %#v with method %q, want error", user, method))

	})
})

var _ = ginkgo.Describe("frontd frontend configuration", func() {
	ginkgo.It("uses branding for spahtml", func() {
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
	ginkgo.It("with process lock serializes process map access", func() {
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
	ginkgo.It("requires bearer before proxying", func() {
		upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			w.Header().Set("X-LeafWiki-Upstream", "workspaced")
			w.WriteHeader(http.StatusAccepted)
		}))
		defer upstream.Close()

		handler, err := frontdPublicMCPHandler(leafwikiRuntimeConfig{
			BasePath:  "",
			Workspace: wiki.Workspace{ID: "current"},
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
	ginkgo.It("requires bearer before proxying", func() {
		handler := frontdMCPBearerAuthHandler(
			leafwikiRuntimeConfig{
				BasePath:  "",
				Workspace: wiki.Workspace{ID: "current"},
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
	ginkgo.It("returns structured error", func() {
		rec := httptest.NewRecorder()

		workspaceMCPUnavailableHandler().ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/mcp/workspaces/docs", nil))

		Expect(rec).To(testmatchers.HaveHTTPStructuredError(http.StatusServiceUnavailable, runtimeErrorCodeMCPWorkspaceUnavailable, sharederrors.MessageIDForCode(runtimeErrorCodeMCPWorkspaceUnavailable)))

	})
})

var _ = ginkgo.Describe("private MCP unauthorized response", func() {
	ginkgo.It("returns structured error", func() {
		rec := httptest.NewRecorder()

		writePrivateMCPUnauthorized(rec)

		Expect(rec).To(testmatchers.HaveHTTPStructuredError(http.StatusUnauthorized, runtimeErrorCodePrivateMCPControlTokenInvalid, sharederrors.MessageIDForCode(runtimeErrorCodePrivateMCPControlTokenInvalid)))

	})
})

var _ = ginkgo.Describe("local-only HTTP MCP handler", func() {
	ginkgo.It("rejects non loopback requests", func() {
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
	ginkgo.It("loads API key user from wikid auth store", func() {
		authDir := leafwikiTempDir()
		userStore, err := coreauth.NewUserStore(authDir)
		Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("NewUserStore failed: %v", err))

		defer userStore.Close()
		userService := coreauth.NewUserService(userStore)
		editor, err := userService.CreateUser("editor", "editor@example.com", "password", coreauth.RoleEditor)
		Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("CreateUser failed: %v", err))

		apiKeyStore, err := coreauth.NewAPIKeyStore(authDir)
		Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("NewAPIKeyStore failed: %v", err))

		apiKeyService := coreauth.NewAPIKeyService(apiKeyStore, userService)
		defer apiKeyService.Close()
		editorID := newFixtureUserID(editor.ID)
		created, err := apiKeyService.CreateAPIKey(editorID, "Native STDIO", editorID)
		Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("CreateAPIKey failed: %v", err))

		req := httptest.NewRequest(http.MethodPost, "/mcp", nil)
		req.Header.Set("Authorization", "Bearer "+created.Secret)
		actor, err := wikidControlMCPActorResolver(authDir, leafwikiRuntimeConfig{
			Workspace: wiki.Workspace{ID: "current"},
		})(req)
		Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("wikidControlMCPActorResolver failed: %v", err))
		Expect(actor).To(SatisfyAll(
			HaveField("Subject", Equal("user:"+editor.ID)),
			HaveField("Username", Equal("editor")),
			HaveField("AuthMethod", Equal("api_key")),
		), fmt.Sprintf("actor = %#v, want API-key editor actor", actor))

	})
})

var _ = ginkgo.Describe("federated first-contact registration", func() {
	ginkgo.It("seeds stdioAPI key workspace grant", func() {
		baseDir := leafwikiTempDir()
		layout := wikid.GlobalLayout(filepath.Join(baseDir, ".leafwiki"))
		authDir := wikid.AuthStoragePaths(layout.HomeDir).AuthDir
		apiKey := createMCPAPIKeyInStorageDir(authDir)
		requestCfg := projectdaemon.Config{
			DataDir: filepath.Join(baseDir, "workspace-data"),
			RootDir: filepath.Join(baseDir, "workspace-root"),
		}

		workspace, isHome, err := registerFederatedFirstContact(layout, requestCfg, leafwikiRuntimeConfig{
			RuntimeStack:  projectdaemon.RuntimeStackWikidFrontd,
			MCPTransports: mcpTransports{Stdio: true},
			APIKey:        apiKey,
		})
		Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("registerFederatedFirstContact failed: %v", err))
		Expect(isHome).To(BeFalse(), fmt.Sprintf("registered first-contact workspace as home"))

		doc, err := wikid.NewGrantStore(layout.DBPath).Load()
		Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("load grants failed: %v", err))

		Expect(doc.Grants).To(ContainElement(SatisfyAll(
			HaveField("Subject", HavePrefix("user:")),
			HaveField("WorkspaceID", Equal(workspace.ID)),
			HaveField("Role", Equal(wikid.GrantRoleEditor)),
		)), fmt.Sprintf("grants = %#v, want editor grant for first-contact workspace %q", doc.Grants, workspace.ID))

	})
})

var _ = ginkgo.Describe("federated first-contact registration", func() {
	ginkgo.It("does not seed stdioAPI key grant for existing workspace", func() {
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

		workspace, isHome, err := registerFederatedFirstContact(layout, requestCfg, leafwikiRuntimeConfig{
			RuntimeStack:  projectdaemon.RuntimeStackWikidFrontd,
			MCPTransports: mcpTransports{Stdio: true},
			APIKey:        apiKey,
		})
		Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("registerFederatedFirstContact failed: %v", err))
		Expect(isHome).To(BeFalse(), fmt.Sprintf("registered existing workspace as home"))
		Expect(workspace.ID).To(Equal(existing.ID), fmt.Sprintf("workspace ID = %q, want existing workspace %q", workspace.ID, existing.ID))

		doc, err := wikid.NewGrantStore(layout.DBPath).Load()
		Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("load grants failed: %v", err))
		Expect(doc.Grants).To(HaveLen(0), fmt.Sprintf("grants = %#v, want no self-grant for existing workspace %q", doc.Grants, workspace.ID))

	})
})

var _ = ginkgo.Describe("federated workspace ensure", func() {
	ginkgo.It("preserves structured grant denial", func() {
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
		}, workspaceid.WorkspaceID("workspace-b"), leafwikiRuntimeConfig{
			APIKey: "valid-but-ungranted-key",
		})
		Expect(err).To(HaveOccurred(), fmt.Sprint("ensureFederatedWorkspace returned nil, want workspace grant denial"))

		Expect(err).To(MatchWikidPrivateEndpoint(http.StatusForbidden, runtimeErrorCodeWorkspaceGrantDenied))

	})
})

var _ = ginkgo.Describe("runtime error responses", func() {
	ginkgo.It("renders message from catalog", func() {

		rec := httptest.NewRecorder()
		writeRuntimeError(rec, http.StatusForbidden, runtimeErrorCodeWorkspaceGrantDenied)
		Expect(rec).To(HaveHTTPStatus(http.StatusForbidden))

		var body runtimeErrorResponse
		Expect(json.Unmarshal(rec.Body.Bytes(), &body)).To(Succeed())
		Expect(body).To(testmatchers.HaveStructuredError(runtimeErrorCodeWorkspaceGrantDenied, sharederrors.MessageIDForCode(runtimeErrorCodeWorkspaceGrantDenied)))

	})
})

var _ = ginkgo.Describe("federated first-contact registration", func() {
	ginkgo.It("persists markdown link root prefix", func() {
		baseDir := leafwikiTempDir()
		layout := wikid.GlobalLayout(filepath.Join(baseDir, ".leafwiki"))
		requestCfg := projectdaemon.Config{
			DataDir: filepath.Join(baseDir, "workspace-data"),
			RootDir: filepath.Join(baseDir, "workspace-root"),
		}
		cfg := testRuntimeConfig(requestCfg.DataDir, requestCfg.RootDir, freeTCPPort(), mcpTransports{Stdio: true}, true)
		cfg.RuntimeStack = projectdaemon.RuntimeStackWikidFrontd
		cfg.MarkdownLinkRootPrefix = "/docs"

		workspace, isHome, err := registerFederatedFirstContact(layout, requestCfg, cfg)
		Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("registerFederatedFirstContact failed: %v", err))
		Expect(isHome).To(BeFalse(), fmt.Sprintf("registered first-contact workspace as home"))
		Expect(workspace.MarkdownLinkRootPrefix).To(Equal("/docs"), fmt.Sprintf("workspace markdown link root prefix = %q, want /docs", workspace.MarkdownLinkRootPrefix))

		loaded, err := registryWorkspaceResult(wikid.NewRegistryService(wikid.NewRegistryStore(layout.DBPath), layout), workspace.ID)
		Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("load workspace: %v", err))
		Expect(loaded.MarkdownLinkRootPrefix).To(Equal("/docs"), fmt.Sprintf("persisted markdown link root prefix = %q, want /docs", loaded.MarkdownLinkRootPrefix))

	})
})

var _ = ginkgo.Describe("federated workspace runtime configuration", func() {
	ginkgo.It("restores markdown link root prefix", func() {
		baseDir := leafwikiTempDir()
		dataDir := filepath.Join(baseDir, "workspace-data")
		rootDir := filepath.Join(baseDir, "workspace-root")
		manager := &federatedWorkspaceManager{
			base: testRuntimeConfig(filepath.Join(baseDir, "home"), filepath.Join(baseDir, "home-root"), freeTCPPort(), mcpTransports{}, true),
		}

		cfg := manager.workspaceRuntimeConfig(wikid.WorkspaceRecord{
			ID:                     "docs",
			DataDir:                dataDir,
			RootDir:                rootDir,
			MarkdownLinkRootPrefix: "/docs",
		}, "41000")
		Expect(cfg.MarkdownLinkRootPrefix).To(Equal("/docs"), fmt.Sprintf("runtime markdown link root prefix = %q, want /docs", cfg.MarkdownLinkRootPrefix))

	})
})

var _ = ginkgo.Describe("daemon owner runtime configuration", func() {
	ginkgo.It("clears home markdown link root prefix", func() {
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
	ginkgo.It("uses semantic workspace ID state", func() {
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
	ginkgo.It("tracks workspaced role", func() {
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
	ginkgo.It("probes workspaced private MCP", func() {
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
		healthy, err := projectDaemonDescriptorHealthy(context.Background(), desc)
		Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("projectDaemonDescriptorHealthy failed: %v", err))
		Expect(healthy).To(BeTrue(), fmt.Sprintf("healthy = false, want private MCP endpoint probe to pass"))

		wrongTokenDesc := *desc
		wrongTokenDesc.PrivateMCPToken = "wrong-token"
		healthy, err = projectDaemonDescriptorHealthy(context.Background(), &wrongTokenDesc)
		Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("projectDaemonDescriptorHealthy with wrong token failed: %v", err))
		Expect(healthy).To(BeFalse(), fmt.Sprintf("healthy = true with wrong private MCP token"))

		server.Close()
		healthy, err = projectDaemonDescriptorHealthy(context.Background(), desc)
		Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("projectDaemonDescriptorHealthy after close failed: %v", err))
		Expect(healthy).To(BeFalse(), fmt.Sprintf("healthy = true after private MCP endpoint closed"))

	})
})

var _ = ginkgo.Describe("wikid actor context handler", func() {
	ginkgo.It("resolves OAuth bearer for MCP", func() {
		w := newFrontdActorTestWiki()
		defer w.Close()
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
	ginkgo.It("returns structured workspace grant denial", func() {
		w := newFrontdActorTestWiki()
		defer w.Close()
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
	ginkgo.It("caps grant by current user role", func() {
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
	}
	Expect(json.Unmarshal(tokenRec.Body.Bytes(), &tokenBody)).To(Succeed(), fmt.Sprintf("decode token response: %v", err))
	Expect(tokenBody.AccessToken).NotTo(BeEmpty(), fmt.Sprintf("token response missing access_token: %s", tokenRec.Body.String()))

	return tokenBody.AccessToken
}

var _ = ginkgo.Describe("boolean configuration resolution", func() {
	ginkgo.It("uses workspace sync environment when flag absent", func() {
		leafwikiSetenv("LEAFWIKI_ENABLE_WORKSPACE_SYNC", "true")

		got := resolveBool("enable-workspace-sync", false, map[string]bool{}, "LEAFWIKI_ENABLE_WORKSPACE_SYNC")
		Expect(got).To(BeTrue(), fmt.Sprintf("enableWorkspaceSync from env = false, want true"))

	})
})

var _ = ginkgo.Describe("logging configuration resolution", func() {
	ginkgo.It("defaults to file under resolved data dir", func() {
		dataDir := filepath.Join(leafwikiTempDir(), "data")

		cfg := resolveLoggingConfigForArgs([]string{"--data-dir=" + dataDir})
		Expect(cfg).To(haveLoggingConfig(leaflogging.TargetFile, Equal(filepath.Join(dataDir, ".leafwiki", "logs", "leafwiki.log"))))

	})
})

var _ = ginkgo.Describe("logging configuration resolution", func() {
	ginkgo.It("CLI overrides environment target", func() {
		leafwikiSetenv("LEAFWIKI_LOG_TARGET", "file")

		cfg := resolveLoggingConfigForArgs([]string{"--log-target=stderr"})
		Expect(cfg).To(haveLoggingConfig(leaflogging.TargetStderr, BeEmpty()))

	})
})

var _ = ginkgo.Describe("logging configuration resolution", func() {
	ginkgo.It("uses environment when flag absent", func() {
		leafwikiSetenv("LEAFWIKI_LOG_TARGET", "stdout")

		cfg := resolveLoggingConfigForArgs(nil)
		Expect(cfg.Target).To(Equal(leaflogging.TargetStdout), fmt.Sprintf("Target = %q, want %q", cfg.Target, leaflogging.TargetStdout))

	})
})

var _ = ginkgo.Describe("logging configuration resolution", func() {
	ginkgo.It("CLI stream target ignores inherited env log file", func() {
		leafwikiSetenv("LEAFWIKI_LOG_FILE", "logs/from-env.log")

		cfg := resolveLoggingConfigForArgs([]string{"--log-target=stderr"})
		Expect(cfg).To(haveLoggingConfig(leaflogging.TargetStderr, BeEmpty()))

	})
})

var _ = ginkgo.Describe("logging configuration resolution", func() {
	ginkgo.It("rejects log file for stream target", func() {
		_, err := resolveLoggingConfigForArgsAllowError([]string{
			"--log-target=stderr",
			"--log-file=custom.log",
		})
		Expect(err).To(MatchError(leaflogging.ErrLogFileRequiresFileTarget))

	})
})

var _ = ginkgo.Describe("leafwiki main process", func() {
	ginkgo.It("default server logging uses file for startup and request logs and keeps stdout clean", func() {
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
		Expect(stdout).NotTo(ContainSubstring(leafwikiStartupLogMessage), fmt.Sprintf("stdout contains server log: %q", stdout))

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
	ginkgo.It("default file logging records fresh data directory creation", func() {
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
	ginkgo.It("CLI stderr target overrides env file target", func() {
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

var _ = ginkgo.Describe("leafwiki main process", func() {
	ginkgo.It("environment stderr target is used when flag absent", func() {
		dataDir := filepath.Join(leafwikiTempDir(), "data")
		port := freeTCPPort()
		proc := startLeafwikiHelper([]string{
			"--disable-auth",
			"--data-dir", dataDir,
			"--host", "127.0.0.1",
			"--port", port,
		}, map[string]string{
			"LEAFWIKI_LOG_TARGET": "stderr",
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

	})
})

var _ = ginkgo.Describe("leafwiki main process", func() {
	ginkgo.It("rejects invalid log target on stderr with no stdout", func() {
		stdout, stderr, err := runLeafwikiHelper([]string{
			"--disable-auth",
			"--data-dir", filepath.Join(leafwikiTempDir(), "data"),
		}, map[string]string{
			"LEAFWIKI_LOG_TARGET": "syslog",
		})
		Expect(err).To(HaveOccurred(), fmt.Sprintf("expected invalid log target to exit non-zero"))
		Expect(stdout).To(BeEmpty(), fmt.Sprintf("stdout = %q, want empty", stdout))
		Expect(stderr).To(ContainSubstring(leaflogging.ErrInvalidLogTarget.Error()), fmt.Sprintf("stderr = %q, want invalid log target", stderr))

	})
})

var _ = ginkgo.Describe("leafwiki main process", func() {
	ginkgo.It("config YAML value overrides environment", func() {
		baseDir := leafwikiTempDir()
		dataDir := filepath.Join(baseDir, "data")
		rootDir := filepath.Join(baseDir, "content")
		port := freeTCPPort()
		configPath := filepath.Join(baseDir, "leafwiki.yml")
		writeTestConfig(configPath, fmt.Sprintf(`disable-auth: true
data-dir: %s
root-dir: %s
host: 127.0.0.1
port: %s
log-target: stderr
`, dataDir, rootDir, port))
		proc := startLeafwikiHelper([]string{"--config", configPath}, map[string]string{
			"LEAFWIKI_PORT": "1",
		})

		waitForLeafwikiReady(proc, port)
		proc.stop()

	})
})

// Plantrace evidence: TestApplyYAMLConfigFile_ResolutionPrecedenceAndExplicitScalars.
var _ = ginkgo.Describe("YAML configuration loading", func() {
	ginkgo.It("resolution precedence and explicit scalars", func() {
		leafwikiSetenv("LEAFWIKI_PORT", "9999")
		leafwikiSetenv("LEAFWIKI_HOST", "0.0.0.0")
		leafwikiSetenv("LEAFWIKI_BASE_PATH", "/wiki")
		leafwikiSetenv("LEAFWIKI_MARKDOWN_LINK_ROOT_PREFIX", "/wiki-docs")
		leafwikiSetenv("LEAFWIKI_PUBLIC_ACCESS", "true")

		configPath := filepath.Join(leafwikiTempDir(), "leafwiki.yml")
		writeTestConfig(configPath, `port: 8088
base-path: ""
markdown-link-root-prefix: docs/
public-access: false
`)
		flags, visited, _ := parseConfigFlagsForArgs([]string{"--config", configPath})

		Expect(resolveString("port", *flags.port, visited, "LEAFWIKI_PORT", "8080")).To(Equal("8088"))
		Expect(resolveString("host", *flags.host, visited, "LEAFWIKI_HOST", "127.0.0.1")).To(Equal("0.0.0.0"))
		Expect(resolveString("data-dir", *flags.dataDir, visited, "LEAFWIKI_DATA_DIR", "./data")).To(Equal("./data"))
		Expect(resolveString("base-path", *flags.basePath, visited, "LEAFWIKI_BASE_PATH", "")).To(BeEmpty())
		markdownLinkRootPrefix, err := resolveMarkdownLinkRootPrefix(flags, visited)
		Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("resolve markdown-link-root-prefix: %v", err))
		Expect(markdownLinkRootPrefix).To(Equal("/docs"), fmt.Sprintf("markdown-link-root-prefix = %q, want /docs", markdownLinkRootPrefix))

		Expect(resolveBool("public-access", *flags.publicAccess, visited, "LEAFWIKI_PUBLIC_ACCESS")).To(BeFalse())

	})
})

var _ = ginkgo.Describe("config file flag registry", func() {
	ginkgo.It("registers public runtime flags", func() {
		fs := flag.NewFlagSet("leafwiki", flag.ContinueOnError)
		registerFlags(fs)

		excluded := map[string]bool{
			"config":                  true,
			"enable-mcp":              true,
			"internal-project-daemon": true,
			"internal-runtime-role":   true,
			"mcp-stdio":               true,
		}
		allowed := configFileFlagNames()
		for name := range excluded {
			Expect(allowed).NotTo(HaveKey(name))
		}

		var missing []string
		fs.VisitAll(func(f *flag.Flag) {
			if excluded[f.Name] {
				return
			}
			if _, ok := allowed[f.Name]; !ok {
				missing = append(missing, f.Name)
			}
		})

		var extra []string
		for name := range allowed {
			if fs.Lookup(name) == nil {
				extra = append(extra, name)
			}
		}
		sort.Strings(missing)
		sort.Strings(extra)
		Expect(missing).To(BeEmpty(), fmt.Sprintf("configFileFlagNames missing keys: %v", missing))
		Expect(extra).To(BeEmpty(), fmt.Sprintf("configFileFlagNames extra keys: %v", extra))

	})
})

var _ = ginkgo.Describe("service example configuration", func() {
	ginkgo.It("parses active template", func() {
		fs := flag.NewFlagSet("leafwiki", flag.ContinueOnError)
		registerFlags(fs)
		visited := map[string]bool{}

		Expect(applyYAMLConfigPath(fs, visited, serviceExampleConfigPath(), "service config example")).To(Succeed())

		for _, expected := range []string{
			"allow-insecure",
			"disable-auth",
			"host",
			"log-file",
			"log-target",
			"mcp",
			"port",
		} {
			Expect(visited[expected]).To(BeTrue(), fmt.Sprintf("service config example active keys = %#v, want %q", visited, expected))

		}

	})
})

var _ = ginkgo.Describe("service example configuration", func() {
	ginkgo.It("documents every public YAML key", func() {
		documented := serviceExampleConfigKeys()
		allowed := configFileFlagNames()

		var missing []string
		for name := range allowed {
			if _, ok := documented[name]; !ok {
				missing = append(missing, name)
			}
		}
		var extra []string
		for name := range documented {
			if _, ok := allowed[name]; !ok {
				extra = append(extra, name)
			}
		}
		sort.Strings(missing)
		sort.Strings(extra)
		Expect(missing).To(BeEmpty(), fmt.Sprintf("service config example missing keys: %v", missing))
		Expect(extra).To(BeEmpty(), fmt.Sprintf("service config example extra keys: %v", extra))

	})
})

var _ = ginkgo.Describe("YAML configuration loading", func() {
	ginkgo.It("accepts quoted scalar coercions", func() {
		configPath := filepath.Join(leafwikiTempDir(), "leafwiki.yml")
		writeTestConfig(configPath, `public-access: "false"
allow-insecure: "true"
access-token-timeout: "30m"
`)

		flags, visited, _ := parseConfigFlagsForArgs([]string{"--config", configPath})

		Expect(resolveBool("public-access", *flags.publicAccess, visited, "LEAFWIKI_PUBLIC_ACCESS")).To(BeFalse())
		Expect(resolveBool("allow-insecure", *flags.allowInsecure, visited, "LEAFWIKI_ALLOW_INSECURE")).To(BeTrue())
		Expect(resolveDuration("access-token-timeout", *flags.accessTokenTimeout, visited, "LEAFWIKI_ACCESS_TOKEN_TIMEOUT")).To(Equal(30 * time.Minute))

	})
})

var _ = ginkgo.Describe("YAML configuration loading", func() {
	ginkgo.It("rejects invalid keys and values", func() {
		tests := []struct {
			name   string
			yaml   string
			reason runtimeconfig.ConfigFileErrorReason
			key    string
		}{
			{name: "unknown key", yaml: "unknown-option: true\n", reason: runtimeconfig.ConfigFileErrorReasonUnknownKey, key: "unknown-option"},
			{name: "duplicate key", yaml: "port: 8080\nport: 8081\n", reason: runtimeconfig.ConfigFileErrorReasonDuplicateKey, key: "port"},
			{name: "non scalar value", yaml: "trusted-proxy-ips:\n  - 127.0.0.1\n", reason: runtimeconfig.ConfigFileErrorReasonScalarValue, key: "trusted-proxy-ips"},
			{name: "null value", yaml: "base-path: null\n", reason: runtimeconfig.ConfigFileErrorReasonScalarValue, key: "base-path"},
			{name: "hidden compatibility key", yaml: "enable-mcp: true\n", reason: runtimeconfig.ConfigFileErrorReasonUnknownKey, key: "enable-mcp"},
			{name: "removed revision key", yaml: "enable-revision: true\n", reason: runtimeconfig.ConfigFileErrorReasonUnknownKey, key: "enable-revision"},
			{name: "removed workspace sync key", yaml: "enable-workspace-sync: true\n", reason: runtimeconfig.ConfigFileErrorReasonUnknownKey, key: "enable-workspace-sync"},
			{name: "removed revision limit key", yaml: "max-revision-history: 0\n", reason: runtimeconfig.ConfigFileErrorReasonUnknownKey, key: "max-revision-history"},
			{name: "internal key", yaml: "internal-project-daemon: /tmp/startup.json\n", reason: runtimeconfig.ConfigFileErrorReasonUnknownKey, key: "internal-project-daemon"},
			{name: "config key", yaml: "config: other.yml\n", reason: runtimeconfig.ConfigFileErrorReasonUnknownKey, key: "config"},
			{name: "mcp stdio compatibility key", yaml: "mcp-stdio: true\n", reason: runtimeconfig.ConfigFileErrorReasonUnknownKey, key: "mcp-stdio"},
			{name: "bad bool scalar", yaml: "public-access: maybe\n", reason: runtimeconfig.ConfigFileErrorReasonInvalidFlagValue, key: "public-access"},
			{name: "bad duration scalar", yaml: "access-token-timeout: soon\n", reason: runtimeconfig.ConfigFileErrorReasonInvalidFlagValue, key: "access-token-timeout"},
		}
		for _, tt := range tests {
			func() {
				_ = tt.name
				configPath := filepath.Join(leafwikiTempDir(), "leafwiki.yml")
				writeTestConfig(configPath, tt.yaml)

				_, _, _, err := parseConfigFlagsForArgsAllowError([]string{"--config", configPath})

				Expect(err).To(MatchRuntimeConfigFileError(tt.reason, tt.key))

			}()
		}

	})
})

var _ = ginkgo.Describe("YAML configuration loading", func() {
	ginkgo.It("rejects config mixed with normal CLI flag", func() {
		configPath := filepath.Join(leafwikiTempDir(), "leafwiki.yml")
		writeTestConfig(configPath, "port: 8080\n")

		_, _, _, err := parseConfigFlagsForArgsAllowError([]string{"--config", configPath, "--port", "8081"})

		Expect(err).To(MatchError(runtimeconfig.ConfigFlagMixError{Flag: "--port"}))

	})
})

var _ = ginkgo.Describe("YAML configuration loading", func() {
	ginkgo.It("rejects config mixed with subcommand trailing CLI flag", func() {
		configPath := filepath.Join(leafwikiTempDir(), "leafwiki.yml")
		writeTestConfig(configPath, "data-dir: ./data\n")

		tests := []struct {
			name    string
			args    []string
			wantErr error
		}{
			{
				name: "reset password trailing flag",
				args: []string{"--config", configPath, "reset-admin-password", "--data-dir", "other"},
				wantErr: runtimeconfig.ConfigFlagMixError{
					Flag: "--data-dir",
				},
			},
			{
				name: "agent hook trailing flag",
				args: []string{"--config", configPath, "agent-hook", "codex", "--data-dir", "other"},
				wantErr: runtimeconfig.ConfigFlagMixError{
					Flag: "--data-dir",
				},
			},
		}
		for _, tt := range tests {
			func() {
				_ = tt.name
				_, _, _, err := parseConfigFlagsForArgsAllowError(tt.args)

				Expect(err).To(MatchError(tt.wantErr))

			}()
		}

	})
})

var _ = ginkgo.Describe("leafwiki main process", func() {
	ginkgo.It("config path value named agent hook does not fail open", func() {
		stdout, stderr, err := runLeafwikiHelperWithInputAndTimeout([]string{
			"--config", "agent-hook",
			"--not-a-real-flag",
		}, nil, `{"session_id":"should-not-be-hook"}`, 5*time.Second)
		Expect(err).To(HaveOccurred(), fmt.Sprintf("config path plus invalid flag unexpectedly succeeded\nstdout:\n%s\nstderr:\n%s", stdout, stderr))
		Expect(stdout).NotTo(Equal("{}\n"), fmt.Sprintf("stdout = %q, want no agent-hook fail-open response", stdout))
		Expect(stderr).To(ContainSubstring("not-a-real-flag"), fmt.Sprintf("stderr = %q, want invalid flag error", stderr))

	})
})

var _ = ginkgo.Describe("leafwiki main process", func() {
	ginkgo.It("config agent hook rejects trailing CLI flag without fail open", func() {
		configPath := filepath.Join(leafwikiTempDir(), "leafwiki.yml")
		writeTestConfig(configPath, "data-dir: ./data\n")
		payload := `{"hook_event_name":"SessionStart","session_id":"config-conflict-secret"}`

		stdout, stderr, err := runLeafwikiHelperWithInputAndTimeout([]string{
			"--config", configPath,
			"agent-hook", "codex",
			"--data-dir", "other",
		}, nil, payload, 5*time.Second)
		Expect(err).To(HaveOccurred(), fmt.Sprintf("config mixed with trailing agent-hook flag unexpectedly succeeded\nstdout:\n%s\nstderr:\n%s", stdout, stderr))
		Expect(stdout).NotTo(Equal("{}\n"), fmt.Sprintf("stdout = %q, want no agent-hook fail-open response", stdout))
		Expect(stderr).To(ContainSubstring(localizedMessage(localization.MessageIDShellRunErrorConfigCannotCombine)+" --data-dir"), fmt.Sprintf("stderr = %q, want config/CLI mutual exclusion error", stderr))
		Expect(stderr).NotTo(ContainSubstring("config-conflict-secret"), fmt.Sprintf("stderr leaked hook payload data: %s", stderr))

	})
})

var _ = ginkgo.Describe("leafwiki main process", func() {
	ginkgo.It("config agent hook rejects trailing CLI flag before reading config", func() {
		configPath := filepath.Join(leafwikiTempDir(), "missing.yml")
		payload := `{"hook_event_name":"SessionStart","session_id":"missing-config-conflict-secret"}`

		stdout, stderr, err := runLeafwikiHelperWithInputAndTimeout([]string{
			"--config", configPath,
			"agent-hook", "codex",
			"--data-dir", "other",
		}, nil, payload, 5*time.Second)
		Expect(err).To(HaveOccurred(), fmt.Sprintf("config mixed with trailing agent-hook flag unexpectedly succeeded\nstdout:\n%s\nstderr:\n%s", stdout, stderr))
		Expect(stdout).NotTo(Equal("{}\n"), fmt.Sprintf("stdout = %q, want no agent-hook fail-open response", stdout))
		Expect(stderr).To(ContainSubstring(localizedMessage(localization.MessageIDShellRunErrorConfigCannotCombine)+" --data-dir"), fmt.Sprintf("stderr = %q, want config/CLI mutual exclusion error", stderr))
		Expect(stderr).NotTo(ContainSubstring("missing-config-conflict-secret"), fmt.Sprintf("stderr leaked hook payload data: %s", stderr))

	})
})

var _ = ginkgo.Describe("leafwiki main process", func() {
	ginkgo.It("config agent hook missing config file fails open", func() {
		configPath := filepath.Join(leafwikiTempDir(), "missing.yml")
		payload := `{"hook_event_name":"SessionStart","session_id":"missing-config-secret"}`

		stdout, stderr, err := runLeafwikiHelperWithInputAndTimeout([]string{
			"--config", configPath,
			"agent-hook", "codex",
		}, nil, payload, 5*time.Second)
		Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("agent-hook missing config file should fail open, got %v\nstdout:\n%s\nstderr:\n%s", err, stdout, stderr))
		Expect(stdout).To(Equal("{}\n"), fmt.Sprintf("stdout = %q, want Codex allow response", stdout))
		Expect(stdout).NotTo(ContainSubstring("missing-config-secret"), fmt.Sprintf("stdout leaked hook payload secret: %s", stdout))
		Expect(stderr).NotTo(ContainSubstring("missing-config-secret"), fmt.Sprintf("stderr leaked hook payload secret: %s", stderr))

	})
})

var _ = ginkgo.Describe("leafwiki main process", func() {
	ginkgo.It("config agent hook rejects flag looking config path without fail open", func() {
		payload := `{"hook_event_name":"SessionStart","session_id":"flag-looking-config-secret"}`

		stdout, stderr, err := runLeafwikiHelperWithInputAndTimeout([]string{
			"--config", "--data-dir",
			"agent-hook", "codex",
		}, nil, payload, 5*time.Second)
		Expect(err).To(HaveOccurred(), fmt.Sprintf("flag-looking config path unexpectedly succeeded\nstdout:\n%s\nstderr:\n%s", stdout, stderr))
		Expect(stdout).NotTo(Equal("{}\n"), fmt.Sprintf("stdout = %q, want no agent-hook fail-open response", stdout))
		Expect(stderr).To(ContainSubstring(localizedMessage(localization.MessageIDShellRunErrorConfigRequiresPath)), fmt.Sprintf("stderr = %q, want config path-shape error", stderr))
		Expect(stderr).NotTo(ContainSubstring("flag-looking-config-secret"), fmt.Sprintf("stderr leaked hook payload data: %s", stderr))

	})
})

var _ = ginkgo.Describe("leafwiki main process", func() {
	ginkgo.It("config agent hook rejects dash prefixed config path without fail open", func() {
		baseDir := leafwikiTempDir()
		configPath := filepath.Join(baseDir, "---config")
		writeTestConfig(configPath, fmt.Sprintf(`disable-auth: true
data-dir: %s
root-dir: %s
log-target: stderr
`, baseDir, baseDir))
		previousDir, err := os.Getwd()
		Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("get working directory: %v", err))

		Expect(os.Chdir(baseDir)).To(Succeed(), fmt.Sprintf("chdir temp dir: %v", err))
		ginkgo.DeferCleanup(func() {
			Expect(os.Chdir(previousDir)).To(Succeed(), fmt.Sprintf("restore working directory: %v", err))
		})
		payload := `{"hook_event_name":"SessionStart","session_id":"dash-prefixed-config-secret"}`

		stdout, stderr, err := runLeafwikiHelperWithInputAndTimeout([]string{
			"--config", "---config",
			"agent-hook", "codex",
		}, nil, payload, 5*time.Second)
		Expect(err).To(HaveOccurred(), fmt.Sprintf("dash-prefixed config path unexpectedly succeeded\nstdout:\n%s\nstderr:\n%s", stdout, stderr))
		Expect(stdout).NotTo(Equal("{}\n"), fmt.Sprintf("stdout = %q, want no agent-hook fail-open response", stdout))
		Expect(stderr).To(ContainSubstring(localizedMessage(localization.MessageIDShellRunErrorConfigRequiresPath)), fmt.Sprintf("stderr = %q, want config path-shape error", stderr))
		Expect(stderr).NotTo(ContainSubstring("dash-prefixed-config-secret"), fmt.Sprintf("stderr leaked hook payload data: %s", stderr))

	})
})

var _ = ginkgo.Describe("leafwiki main process", func() {
	ginkgo.It("config agent hook rejects empty config path without fail open", func() {
		tests := []struct {
			name string
			args []string
		}{
			{name: "inline empty", args: []string{"--config=", "agent-hook", "codex"}},
			{name: "separate empty", args: []string{"--config", "", "agent-hook", "codex"}},
			{name: "trailing bare after agent hook", args: []string{"agent-hook", "codex", "--config"}},
			{name: "inline empty before help after agent hook", args: []string{"agent-hook", "codex", "--config=", "--help"}},
			{name: "single dash", args: []string{"--config", "-", "agent-hook", "codex"}},
			{name: "double dash", args: []string{"--config", "--", "agent-hook", "codex"}},
		}
		for _, tt := range tests {
			func() {
				_ = tt.name
				payload := `{"hook_event_name":"SessionStart","session_id":"empty-config-secret"}`

				stdout, stderr, err := runLeafwikiHelperWithInputAndTimeout(tt.args, nil, payload, 5*time.Second)
				Expect(err).To(HaveOccurred(), fmt.Sprintf("empty config path unexpectedly succeeded\nstdout:\n%s\nstderr:\n%s", stdout, stderr))
				Expect(stdout).NotTo(Equal("{}\n"), fmt.Sprintf("stdout = %q, want no agent-hook fail-open response", stdout))
				Expect(stderr).To(ContainSubstring(localizedMessage(localization.MessageIDShellRunErrorConfigRequiresPath)), fmt.Sprintf("stderr = %q, want empty config path error", stderr))
				Expect(stderr).NotTo(ContainSubstring("empty-config-secret"), fmt.Sprintf("stderr leaked hook payload data: %s", stderr))

			}()
		}

	})
})

var _ = ginkgo.Describe("leafwiki main process", func() {
	ginkgo.It("config agent hook rejects help mix without fail open", func() {
		configPath := filepath.Join(leafwikiTempDir(), "leafwiki.yml")
		writeTestConfig(configPath, "data-dir: ./data\n")
		payload := `{"hook_event_name":"SessionStart","session_id":"config-help-secret"}`

		stdout, stderr, err := runLeafwikiHelperWithInputAndTimeout([]string{
			"agent-hook", "codex",
			"--config", configPath,
			"--help",
		}, nil, payload, 5*time.Second)
		Expect(err).To(HaveOccurred(), fmt.Sprintf("config mixed with help unexpectedly succeeded\nstdout:\n%s\nstderr:\n%s", stdout, stderr))
		Expect(stdout).NotTo(Equal("{}\n"), fmt.Sprintf("stdout = %q, want no agent-hook fail-open response", stdout))
		Expect(stderr).To(ContainSubstring(localizedMessage(localization.MessageIDCLIErrorInvalidConfigArguments)), fmt.Sprintf("stderr = %q, want config argument error", stderr))
		Expect(stderr).NotTo(ContainSubstring("config-help-secret"), fmt.Sprintf("stderr leaked hook payload data: %s", stderr))

	})
})

var _ = ginkgo.Describe("leafwiki main process", func() {
	ginkgo.It("config rejects positional help", func() {
		configPath := filepath.Join(leafwikiTempDir(), "leafwiki.yml")
		writeTestConfig(configPath, "data-dir: ./data\n")

		stdout, stderr, err := runLeafwikiHelper([]string{
			"--config", configPath,
			"help",
		}, nil)
		Expect(err).To(HaveOccurred(), fmt.Sprintf("config mixed with positional help unexpectedly succeeded\nstdout:\n%s\nstderr:\n%s", stdout, stderr))
		Expect(stdout).NotTo(ContainSubstring("Usage:"), fmt.Sprintf("stdout = %q, want no successful usage output", stdout))
		Expect(stderr).To(SatisfyAll(
			ContainSubstring(localizedMessage(localization.MessageIDCLIErrorInvalidConfigFile)),
			ContainSubstring(localizedMessage(localization.MessageIDShellRunErrorConfigCannotCombine)+" help"),
		), fmt.Sprintf("stderr = %q, want config/help mix error", stderr))

	})
})

var _ = ginkgo.Describe("leafwiki main process", func() {
	ginkgo.It("config agent hook rejects unknown flag without fail open", func() {
		configPath := filepath.Join(leafwikiTempDir(), "leafwiki.yml")
		writeTestConfig(configPath, "data-dir: ./data\n")
		payload := `{"hook_event_name":"SessionStart","session_id":"unknown-config-flag-secret"}`

		stdout, stderr, err := runLeafwikiHelperWithInputAndTimeout([]string{
			"--config", configPath,
			"--not-a-real-flag",
			"agent-hook", "codex",
		}, nil, payload, 5*time.Second)
		Expect(err).To(HaveOccurred(), fmt.Sprintf("config mixed with unknown flag unexpectedly succeeded\nstdout:\n%s\nstderr:\n%s", stdout, stderr))
		Expect(stdout).NotTo(Equal("{}\n"), fmt.Sprintf("stdout = %q, want no agent-hook fail-open response", stdout))
		Expect(stderr).To(ContainSubstring("not-a-real-flag"), fmt.Sprintf("stderr = %q, want unknown flag error", stderr))
		Expect(stderr).NotTo(ContainSubstring("unknown-config-flag-secret"), fmt.Sprintf("stderr leaked hook payload data: %s", stderr))

	})
})

var _ = ginkgo.Describe("leafwiki main process", func() {
	ginkgo.It("agent hook fail open uses config", func() {
		baseDir := leafwikiTempDir()
		sameDir := filepath.Join(baseDir, "same")
		configPath := filepath.Join(baseDir, "leafwiki.yml")
		writeTestConfig(configPath, fmt.Sprintf(`disable-auth: true
data-dir: %s
root-dir: %s
log-target: stderr
`, sameDir, sameDir))
		payload := `{"hook_event_name":"SessionStart","session_id":"config-hook-secret"}`

		stdout, stderr, err := runLeafwikiHelperWithInputAndTimeout([]string{
			"--config", configPath,
			"agent-hook", "codex",
		}, nil, payload, 5*time.Second)
		Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("agent-hook invalid config should fail open, got %v\nstdout:\n%s\nstderr:\n%s", err, stdout, stderr))
		Expect(stdout).To(Equal("{}\n"), fmt.Sprintf("stdout = %q, want Codex allow response", stdout))
		Expect(stderr).NotTo(ContainSubstring("config-hook-secret"), fmt.Sprintf("stderr leaked hook payload data: %s", stderr))

	})
})

var _ = ginkgo.Describe("leafwiki main process", func() {
	ginkgo.It("reset admin password uses config data dir", func() {
		baseDir := leafwikiTempDir()
		dataDir := filepath.Join(baseDir, "data")
		initWikidAdminUser(dataDir)
		configPath := filepath.Join(baseDir, "leafwiki.yml")
		writeTestConfig(configPath, fmt.Sprintf("data-dir: %s\n", dataDir))

		stdout, stderr, err := runLeafwikiHelper([]string{
			"--config", configPath,
			"reset-admin-password",
		}, map[string]string{})
		Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("reset-admin-password process error = %v, stderr=%q", err, stderr))
		Expect(stdout).To(ContainSubstring(localizedMessage(localization.MessageIDCLIStatusAdminPasswordReset)), fmt.Sprintf("stdout = %q, want reset output", stdout))

	})
})

var _ = ginkgo.Describe("leafwiki main process", func() {
	ginkgo.It("config path does not affect daemon identity", func() {
		stdinReader, stdinWriter := io.Pipe()
		defer stdinWriter.Close()
		baseDir := leafwikiTempDir()
		dataDir := filepath.Join(baseDir, "data")
		rootDir := filepath.Join(baseDir, "content")
		port := freeTCPPort()
		configBody := fmt.Sprintf(`mcp: stdio
disable-auth: true
data-dir: %s
root-dir: %s
host: 127.0.0.1
port: %s
log-target: stderr
`, dataDir, rootDir, port)
		firstConfig := filepath.Join(baseDir, "first.yml")
		secondConfig := filepath.Join(baseDir, "second.yml")
		writeTestConfig(firstConfig, configBody)
		writeTestConfig(secondConfig, configBody)
		env := map[string]string{"HOME": filepath.Join(baseDir, "home")}
		first := startLeafwikiHelperWithStdin([]string{"--config", firstConfig}, env, stdinReader)
		waitForLeafwikiReady(first, port)

		stdout, stderr, err := runLeafwikiHelperWithTimeout([]string{"--config", secondConfig}, env, 5*time.Second)
		Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("second config path should attach and exit cleanly, got %v\nstdout:\n%s\nstderr:\n%s", err, stdout, stderr))
		Expect(stdout).To(BeEmpty(), fmt.Sprintf("stdout = %q, want empty without MCP frames", stdout))
		Expect(stderr).NotTo(ContainSubstring(projectdaemon.FormatConfigMismatch(nil)), fmt.Sprintf("stderr = %q, want no config mismatch from config path", stderr))

		Expect(stdinWriter.Close()).To(Succeed())
		first.waitForExit()

	})
})

var _ = ginkgo.Describe("leafwiki main process", func() {
	ginkgo.It("rejects explicit log file for stream target", func() {
		stdout, stderr, err := runLeafwikiHelper([]string{
			"--disable-auth",
			"--data-dir", filepath.Join(leafwikiTempDir(), "data"),
			"--log-target", "stderr",
			"--log-file", "custom.log",
		}, nil)
		Expect(err).To(HaveOccurred(), fmt.Sprintf("expected --log-file with stderr target to exit non-zero"))
		Expect(stdout).To(BeEmpty(), fmt.Sprintf("stdout = %q, want empty", stdout))
		Expect(stderr).To(ContainSubstring(leaflogging.ErrLogFileRequiresFileTarget.Error()), fmt.Sprintf("stderr = %q, want --log-file target error", stderr))

	})
})

var _ = ginkgo.Describe("leafwiki main process", func() {
	ginkgo.It("native STDIO auth enabled requires API key", func() {
		dataDir := filepath.Join(leafwikiTempDir(), "data")
		stdout, stderr, err := runLeafwikiHelper([]string{
			"--mcp=stdio",
			"--data-dir", dataDir,
			"--jwt-secret", "test-secret",
			"--admin-password", "admin-password",
		}, nil)
		Expect(err).To(HaveOccurred(), fmt.Sprintf("expected native stdio with auth enabled to exit non-zero"))
		Expect(stdout).To(BeEmpty(), fmt.Sprintf("stdout = %q, want empty", stdout))
		Expect(stderr).To(ContainSubstring(localizedMessage(localization.MessageIDCLIErrorStdioAuthIdentityRequired)), fmt.Sprintf("stderr = %q, want native stdio API-key requirement", stderr))

	})
})

var _ = ginkgo.Describe("leafwiki main process", func() {
	ginkgo.It("native STDIO rejects invalid API key without leaking secret", func() {
		secret := "lwk_secret_bad"
		stdout, stderr, err := runLeafwikiHelper([]string{
			"--mcp=stdio",
			"--api-key", secret,
			"--data-dir", filepath.Join(leafwikiTempDir(), "data"),
			"--jwt-secret", "test-secret",
			"--admin-password", "admin-password",
			"--log-target", "stderr",
		}, nil)
		Expect(err).To(HaveOccurred(), fmt.Sprintf("expected native stdio with invalid API key to exit non-zero"))
		Expect(stdout).To(BeEmpty(), fmt.Sprintf("stdout = %q, want empty", stdout))
		Expect(stdout).NotTo(ContainSubstring(secret), fmt.Sprintf("stdout leaked API key: %q", stdout))
		Expect(stderr).NotTo(ContainSubstring(secret), fmt.Sprintf("stderr leaked API key: %q", stderr))
		Expect(stderr).To(ContainSubstring(leafwikiInvalidNativeStdioAPIKey), fmt.Sprintf("stderr = %q, want invalid API-key error", stderr))

	})
})

var _ = ginkgo.Describe("leafwiki main process", func() {
	ginkgo.It("native STDIO rejects stdout logging", func() {
		stdout, stderr, err := runLeafwikiHelper([]string{
			"--mcp=stdio",
			"--disable-auth",
			"--data-dir", filepath.Join(leafwikiTempDir(), "data"),
			"--log-target", "stdout",
		}, nil)
		Expect(err).To(HaveOccurred(), fmt.Sprintf("expected native stdio with stdout logging to exit non-zero"))
		Expect(stdout).To(BeEmpty(), fmt.Sprintf("stdout = %q, want empty", stdout))
		Expect(stderr).To(ContainSubstring(localizedMessage(localization.MessageIDCLIErrorStdoutReservedForMCPStdio)), fmt.Sprintf("stderr = %q, want stdout reserved error", stderr))

	})
})

var _ = ginkgo.Describe("leafwiki main process", func() {
	ginkgo.It("public HTTPMCP non loopback host starts web and keeps local MCP", func() {
		var ownerPID int
		baseDir := leafwikiTempDir()
		ginkgo.DeferCleanup(func() {
			terminateProjectDaemonProcess(ownerPID)
		})
		dataDir := filepath.Join(baseDir, "data")
		rootDir := filepath.Join(baseDir, "content")
		port := freeTCPPort()
		proc := startLeafwikiHelper([]string{
			"--mcp=http",
			"--disable-auth",
			"--host", "0.0.0.0",
			"--port", port,
			"--data-dir", dataDir,
			"--root-dir", rootDir,
			"--log-target", "stderr",
		}, nil)

		waitForLeafwikiReady(proc, port)
		globalDesc := waitForGlobalWikidDescriptor(dataDir)
		ownerPID = globalDesc.PID
		toolNames := listProcessHTTPMCPToolNames("http://127.0.0.1:" + port + "/mcp/workspaces/home")
		Expect(toolNames).To(matchToolNames(federatedRuntimeToolNames()))
		proc.stop()
		terminateProjectDaemonProcess(ownerPID)
		waitForLeafwikiUnavailable(port)
		waitForProjectLocksReusable(dataDir, rootDir, 15*time.Second)

	})
})

var _ = ginkgo.Describe("leafwiki main process", func() {
	ginkgo.It("native STDIO rejects positional command with stderr only", func() {
		stdout, stderr, err := runLeafwikiHelper([]string{
			"--mcp=stdio",
			"bogus",
		}, nil)
		Expect(err).To(HaveOccurred(), fmt.Sprintf("expected native stdio with a positional command to exit non-zero"))
		Expect(stdout).To(BeEmpty(), fmt.Sprintf("stdout = %q, want empty", stdout))
		Expect(stderr).To(ContainSubstring(leafwikiNativeStdioPositionalCmd), fmt.Sprintf("stderr = %q, want positional-command error", stderr))

	})
})

var _ = ginkgo.Describe("leafwiki main process", func() {
	ginkgo.It("native STDIO environment rejects positional command with stderr only", func() {
		stdout, stderr, err := runLeafwikiHelper([]string{
			"--disable-auth",
			"bogus",
		}, map[string]string{
			"LEAFWIKI_MCP": "stdio",
		})
		Expect(err).To(HaveOccurred(), fmt.Sprintf("expected env-enabled native stdio with a positional command to exit non-zero"))
		Expect(stdout).To(BeEmpty(), fmt.Sprintf("stdout = %q, want empty", stdout))
		Expect(stderr).To(ContainSubstring(leafwikiNativeStdioPositionalCmd), fmt.Sprintf("stderr = %q, want positional-command error", stderr))

	})
})

var _ = ginkgo.Describe("leafwiki main process", func() {
	ginkgo.It("native STDIO starts HTTP and stdin close stops server", func() {
		stdinReader, stdinWriter := io.Pipe()
		dataDir := filepath.Join(leafwikiTempDir(), "data")
		rootDir := filepath.Join(leafwikiTempDir(), "content")
		port := freeTCPPort()
		proc := startLeafwikiHelperWithStdin([]string{
			"--mcp=stdio",
			"--disable-auth",
			"--data-dir", dataDir,
			"--root-dir", rootDir,
			"--host", "127.0.0.1",
			"--port", port,
			"--log-target", "stderr",
		}, nil, stdinReader)

		waitForLeafwikiReady(proc, port)
		Expect(stdinWriter.Close()).To(Succeed())
		proc.waitForExit()

		Expect(readFileString(proc.stdoutPath)).To(BeEmpty())
		waitForLeafwikiUnavailable(port)

	})
})

var _ = ginkgo.Describe("leafwiki main process", func() {
	ginkgo.It("native STDIO only keeps HTTPMCP route disabled", func() {
		stdinReader, stdinWriter := io.Pipe()
		defer stdinWriter.Close()
		port := freeTCPPort()
		proc := startLeafwikiHelperWithStdin([]string{
			"--mcp=stdio",
			"--disable-auth",
			"--data-dir", filepath.Join(leafwikiTempDir(), "data"),
			"--root-dir", filepath.Join(leafwikiTempDir(), "content"),
			"--host", "127.0.0.1",
			"--port", port,
			"--log-target", "stderr",
		}, nil, stdinReader)

		waitForLeafwikiReady(proc, port)
		resp, err := http.Get("http://127.0.0.1:" + port + "/mcp")
		Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("GET /mcp: %v", err))

		defer resp.Body.Close()
		Expect(resp).To(HaveHTTPStatus(http.StatusNotFound))

		Expect(stdinWriter.Close()).To(Succeed())
		proc.waitForExit()
		Expect(readFileString(proc.stdoutPath)).To(BeEmpty())

	})
})

var _ = ginkgo.Describe("leafwiki main process", func() {
	ginkgo.It("native STDIO second compatible startup attaches to project daemon", func() {
		stdinReader, stdinWriter := io.Pipe()
		defer stdinWriter.Close()
		baseDir := leafwikiTempDir()
		dataDir := filepath.Join(baseDir, "data")
		rootDir := filepath.Join(baseDir, "content")
		port := freeTCPPort()
		first := startLeafwikiHelperWithStdin([]string{
			"--mcp=stdio",
			"--disable-auth",
			"--data-dir", dataDir,
			"--root-dir", rootDir,
			"--host", "127.0.0.1",
			"--port", port,
			"--log-target", "stderr",
		}, nil, stdinReader)
		waitForLeafwikiReady(first, port)
		_, err := io.WriteString(stdinWriter, nativeStdioListToolsInput())
		Expect(err).NotTo(HaveOccurred())
		waitForFileContaining(first.stdoutPath, `"id":2`)

		stdout, stderr, err := runLeafwikiHelperWithTimeout([]string{
			"--mcp=stdio",
			"--disable-auth",
			"--data-dir", dataDir,
			"--root-dir", rootDir,
			"--host", "127.0.0.1",
			"--port", port,
			"--log-target", "stderr",
		}, nil, 5*time.Second)
		Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("second compatible startup should attach and exit cleanly, got %v\nstdout:\n%s\nstderr:\n%s", err, stdout, stderr))
		Expect(stdout).To(BeEmpty(), fmt.Sprintf("stdout = %q, want empty without MCP frames", stdout))

		for _, unexpected := range []string{"data directory is already in use", "root directory is already in use", "bind: address already in use"} {
			Expect(stderr).NotTo(ContainSubstring(unexpected), fmt.Sprintf("stderr = %q, want no old ownership failure %q", stderr, unexpected))

		}
		waitForLeafwikiReady(first, port)

	})
})

var _ = ginkgo.Describe("leafwiki main process", func() {
	ginkgo.It("native STDIO only startup attaches to HTTP enabled project daemon", func() {
		baseDir := leafwikiTempDir()
		dataDir := filepath.Join(baseDir, "data")
		rootDir := filepath.Join(baseDir, "content")
		port := freeTCPPort()
		first := startLeafwikiHelper([]string{
			"--mcp=http",
			"--disable-auth",
			"--data-dir", dataDir,
			"--root-dir", rootDir,
			"--host", "127.0.0.1",
			"--port", port,
			"--log-target", "stderr",
		}, nil)
		waitForLeafwikiReady(first, port)

		stdout, stderr, err := runLeafwikiHelperWithTimeout([]string{
			"--mcp=stdio",
			"--disable-auth",
			"--data-dir", dataDir,
			"--root-dir", rootDir,
			"--host", "127.0.0.1",
			"--port", port,
			"--log-target", "file",
			"--disable-request-log",
		}, nil, 12*time.Second)
		Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("stdio-only startup should attach to HTTP-enabled daemon, got %v\nstdout:\n%s\nstderr:\n%s", err, stdout, stderr))
		Expect(stdout).To(BeEmpty(), fmt.Sprintf("stdout = %q, want empty without MCP frames", stdout))
		Expect(stderr).NotTo(ContainSubstring(projectdaemon.FormatConfigMismatch(nil)), fmt.Sprintf("stderr = %q, want no public MCP, logging, or request-log config mismatch", stderr))

		toolNames := listProcessHTTPMCPToolNames("http://127.0.0.1:" + port + "/mcp/workspaces/home")
		Expect(toolNames).To(matchToolNames(federatedRuntimeToolNames()))

	})
})

var _ = ginkgo.Describe("leafwiki main process", func() {
	ginkgo.It("native STDIO owner stderr logging falls back to file", func() {
		stdinReader, stdinWriter := io.Pipe()
		defer stdinWriter.Close()
		baseDir := leafwikiTempDir()
		dataDir := filepath.Join(baseDir, "data")
		rootDir := filepath.Join(baseDir, "content")
		port := freeTCPPort()
		proc := startLeafwikiHelperWithStdin([]string{
			"--mcp=stdio",
			"--disable-auth",
			"--data-dir", dataDir,
			"--root-dir", rootDir,
			"--host", "127.0.0.1",
			"--port", port,
			"--log-target", "stderr",
			"--daemon-idle-timeout", "0",
		}, nil, stdinReader)
		waitForLeafwikiReady(proc, port)

		logPath := filepath.Join(dataDir, ".leafwiki", "logs", "leafwiki.log")
		waitForFileContaining(logPath, "Starting LeafWiki")

		Expect(stdinWriter.Close()).To(Succeed())
		proc.waitForExit()
		Expect(readFileString(proc.stdoutPath)).To(BeEmpty())

	})
})

var _ = ginkgo.Describe("project daemon owner spawn", func() {
	ginkgo.It("removes secret startup config on executable failure", func() {
		oldExecutable := projectDaemonExecutable
		ginkgo.DeferCleanup(func() {
			projectDaemonExecutable = oldExecutable
		})

		baseDir := leafwikiTempDir()
		dataDir := filepath.Join(baseDir, "data")
		rootDir := filepath.Join(baseDir, "content")
		jwtSecret := fmt.Sprintf("cleanup-jwt-secret-%d", time.Now().UnixNano())
		adminPassword := "cleanup-admin-password"
		var startupPath string
		executableErr := errors.New("forced executable failure")
		projectDaemonExecutable = func() (string, error) {
			startupPath = findLeafwikiDaemonStartupConfigContaining(jwtSecret)
			Expect(startupPath).NotTo(BeEmpty(), fmt.Sprintf("startup config containing secret marker was not visible before executable lookup"))

			Expect(startupPath).To(haveFileMode(0o600))
			return "", executableErr
		}

		cfg := testRuntimeConfig(dataDir, rootDir, freeTCPPort(), mcpTransports{}, false)
		cfg.JWTSecret = jwtSecret
		cfg.AdminPassword = adminPassword
		_, err := spawnProjectDaemonOwner(cfg)

		Expect(err).To(MatchError(executableErr))
		_, err = os.Stat(startupPath)
		Expect(err).To(MatchError(os.ErrNotExist))

	})
})

var _ = ginkgo.Describe("project daemon owner spawn", func() {
	ginkgo.It("eventually removes secret startup config when child exits before read", func() {
		oldExecutable := projectDaemonExecutable
		oldCleanupDelay := projectDaemonStartupConfigPostStartCleanupDelay
		ginkgo.DeferCleanup(func() {
			projectDaemonExecutable = oldExecutable
			projectDaemonStartupConfigPostStartCleanupDelay = oldCleanupDelay
		})

		truePath, err := exec.LookPath("true")
		if err != nil {
			ginkgo.Skip(fmt.Sprint("true executable not available"))
		}
		baseDir := leafwikiTempDir()
		dataDir := filepath.Join(baseDir, "data")
		rootDir := filepath.Join(baseDir, "content")
		jwtSecret := fmt.Sprintf("post-start-cleanup-jwt-%d", time.Now().UnixNano())
		var startupPath string
		projectDaemonExecutable = func() (string, error) {
			startupPath = findLeafwikiDaemonStartupConfigContaining(jwtSecret)
			Expect(startupPath).NotTo(BeEmpty(), fmt.Sprintf("startup config containing secret marker was not visible before child start"))

			Expect(startupPath).To(haveFileMode(0o600))
			return truePath, nil
		}
		projectDaemonStartupConfigPostStartCleanupDelay = 25 * time.Millisecond

		cfg := testRuntimeConfig(dataDir, rootDir, freeTCPPort(), mcpTransports{}, false)
		cfg.JWTSecret = jwtSecret
		cfg.AdminPassword = "post-start-cleanup-admin"
		_, err = spawnProjectDaemonOwner(cfg)
		Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("spawnProjectDaemonOwner failed: %v", err))

		waitForFileRemoved(startupPath, 2*time.Second)

	})
})

var _ = ginkgo.Describe("leafwiki main process", func() {
	ginkgo.It("native stdioAPI key attach does not require owner bootstrap secrets", func() {
		baseDir := leafwikiTempDir()
		dataDir := filepath.Join(baseDir, "data")
		rootDir := filepath.Join(baseDir, "content")
		apiKey := createWikidMCPAPIKeyWithUser(dataDir)
		port := freeTCPPort()
		first := startLeafwikiHelper([]string{
			"--mcp=http",
			"--data-dir", dataDir,
			"--root-dir", rootDir,
			"--host", "127.0.0.1",
			"--port", port,
			"--jwt-secret", "owner-jwt-secret",
			"--admin-password", "owner-admin-password",
			"--allow-insecure",
			"--log-target", "stderr",
		}, map[string]string{})
		waitForLeafwikiReady(first, port)
		grantWikidWorkspaceAccessForDirs(dataDir, rootDir, apiKey.UserID, wikid.GrantRoleEditor)

		stdout, stderr, err := runLeafwikiHelperWithTimeout([]string{
			"--mcp=stdio",
			"--data-dir", dataDir,
			"--root-dir", rootDir,
			"--host", "127.0.0.1",
			"--port", port,
			"--allow-insecure",
			"--log-target", "stderr",
		}, map[string]string{
			"LEAFWIKI_MCP_API_KEY": apiKey.Secret,
		}, 5*time.Second)
		Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("stdio API-key startup should attach without owner bootstrap secrets, got %v\nstdout:\n%s\nstderr:\n%s", err, stdout, stderr))
		Expect(stdout).To(BeEmpty(), fmt.Sprintf("stdout = %q, want empty without MCP frames", stdout))

		for _, unexpected := range []string{"JWT secret is required", "admin password is required", "project daemon config mismatch"} {
			Expect(stderr).NotTo(ContainSubstring(unexpected), fmt.Sprintf("stderr = %q, want no bootstrap-secret attach failure %q", stderr, unexpected))

		}

	})
})

var _ = ginkgo.Describe("leafwiki main process", func() {
	ginkgo.It("stale descriptor is replaced without sending API key", func() {
		baseDir := leafwikiTempDir()
		dataDir := filepath.Join(baseDir, "data")
		rootDir := filepath.Join(baseDir, "content")
		Expect(os.MkdirAll(dataDir, 0o755)).To(Succeed())
		Expect(os.MkdirAll(rootDir, 0o755)).To(Succeed())
		received := make(chan string, 4)
		staleControl := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			raw, _ := io.ReadAll(req.Body)
			select {
			case received <- req.URL.Path + " " + req.Header.Get("Authorization") + " " + string(raw):
			default:
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"ok":true}`)
		}))
		ginkgo.DeferCleanup(staleControl.Close)

		apiKey := createWikidMCPAPIKey(dataDir)
		port := freeTCPPort()
		runtimeCfg := testRuntimeConfig(dataDir, rootDir, port, mcpTransports{Stdio: true}, false)
		runtimeCfg.RuntimeStack = projectdaemon.RuntimeStackWikidFrontd
		runtimeCfg.JWTSecret = "owner-jwt-secret"
		runtimeCfg.AdminPassword = "owner-admin-password"
		layout := leafwikiHelperGlobalLayoutForDataDir(dataDir)
		ownerRuntimeCfg := runtimeCfg
		ownerRuntimeCfg.Workspace = wiki.Workspace{ID: wikid.HomeWorkspaceID, DataDir: layout.HomeDir, RootDir: layout.HomeRootDir}
		ownerCfg, err := daemonConfigForRuntime(ownerRuntimeCfg)
		Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("daemonConfigForRuntime: %v", err))

		hash, err := projectdaemon.ConfigHash(ownerCfg)
		Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("ConfigHash: %v", err))

		descriptorPath := projectdaemon.GlobalDescriptorPath(layout.RuntimeDir, projectdaemon.RoleWikid)
		Expect(projectdaemon.WriteDescriptorAtomic(descriptorPath, &projectdaemon.Descriptor{
			SchemaVersion:    projectdaemon.DescriptorSchemaVersion,
			PID:              os.Getpid(),
			StartedAt:        time.Now().UTC(),
			DataDir:          ownerCfg.DataDir,
			RootDir:          ownerCfg.RootDir,
			PublicURL:        "http://127.0.0.1:" + port,
			PublicMCPEnabled: false,
			ControlURL:       staleControl.URL,
			ConfigHash:       hash,
			IdleTimeout:      "0s",
			ControlToken:     "stale-token",
			Config:           ownerCfg,
		})).To(Succeed())

		stdinReader, stdinWriter := io.Pipe()
		defer stdinWriter.Close()
		proc := startLeafwikiHelperWithStdin([]string{
			"--mcp=stdio",
			"--data-dir", dataDir,
			"--root-dir", rootDir,
			"--host", "127.0.0.1",
			"--port", port,
			"--allow-insecure",
			"--jwt-secret", "owner-jwt-secret",
			"--admin-password", "owner-admin-password",
			"--log-target", "stderr",
		}, map[string]string{
			"LEAFWIKI_MCP_API_KEY": apiKey,
		}, stdinReader)
		waitForLeafwikiReady(proc, port)

		_ = waitForGlobalWikidDescriptor(dataDir)
		replaced := readFileString(descriptorPath)
		replacedDesc, err := projectdaemon.ReadTrustedDescriptor(descriptorPath)
		Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("read replaced descriptor: %v", err))
		Expect(replacedDesc).To(SatisfyAll(
			Not(HaveField("ControlURL", Equal(staleControl.URL))),
			Not(HaveField("ControlToken", Equal("stale-token"))),
		), fmt.Sprintf("descriptor was not replaced:\n%s", replaced))

		Consistently(received).WithTimeout(25 * time.Millisecond).ShouldNot(Receive())
		Expect(readFileString(proc.stdoutPath)).NotTo(ContainSubstring(apiKey))
		Expect(readFileString(proc.stderrPath)).NotTo(ContainSubstring(apiKey))
		Expect(stdinWriter.Close()).To(Succeed(), fmt.Sprintf("close stdin writer: %v", err))
		proc.waitForExit()

	})
})

var _ = ginkgo.Describe("leafwiki main process", func() {
	ginkgo.It("untrusted stale descriptor is replaced when locks are free", func() {
		tests := []struct {
			name  string
			setup func(path string)
		}{
			{
				name: "corrupt json",
				setup: func(path string) {
					ginkgo.GinkgoHelper()
					Expect(os.WriteFile(path, []byte("{"), 0o600)).To(Succeed())
				},
			},
			{
				name: "wrong mode",
				setup: func(path string) {
					ginkgo.GinkgoHelper()
					Expect(os.WriteFile(path, []byte("{}"), 0o644)).To(Succeed())
				},
			},
			{
				name: "non regular path",
				setup: func(path string) {
					ginkgo.GinkgoHelper()
					Expect(os.Mkdir(path, 0o700)).To(Succeed())
				},
			},
		}

		for _, tt := range tests {
			func() {
				_ = tt.name
				baseDir := leafwikiTempDir()
				dataDir := filepath.Join(baseDir, "data")
				rootDir := filepath.Join(baseDir, "content")
				descriptorPath := filepath.Join(dataDir, ".leafwiki", projectdaemon.DescriptorFileName)
				Expect(os.MkdirAll(filepath.Dir(descriptorPath), 0o755)).To(Succeed())
				Expect(os.MkdirAll(rootDir, 0o755)).To(Succeed())
				tt.setup(descriptorPath)

				port := freeTCPPort()
				proc := startLeafwikiHelper([]string{
					"--disable-auth",
					"--data-dir", dataDir,
					"--root-dir", rootDir,
					"--host", "127.0.0.1",
					"--port", port,
					"--log-target", "stderr",
				}, nil)
				waitForLeafwikiReady(proc, port)

				desc := waitForProjectDaemonDescriptor(dataDir)
				Expect(desc).To(SatisfyAll(
					HaveField("PID", BeNumerically(">", 0)),
					HaveField("ControlURL", Not(BeEmpty())),
				), fmt.Sprintf("replacement descriptor = %#v, want live daemon descriptor", desc))

				info, err := os.Stat(descriptorPath)
				Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("stat replacement descriptor: %v", err))

				Expect(info.Mode().Perm()).To(Equal(os.FileMode(0o600)))

			}()
		}

	})
})

var _ = ginkgo.Describe("project daemon descriptor trust", func() {
	ginkgo.It("preserves untrusted descriptor when any project lock is held", func() {
		tests := []struct {
			name string
			lock func(dataDir string, rootDir string) func()
		}{
			{
				name: "data lock held",
				lock: func(dataDir string, _ string) func() {
					ginkgo.GinkgoHelper()
					lock, err := locking.AcquireDataDirLock(dataDir)
					Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("acquire data lock: %v", err))

					return func() { _ = lock.Release() }
				},
			},
			{
				name: "root lock held",
				lock: func(_ string, rootDir string) func() {
					ginkgo.GinkgoHelper()
					lock, err := locking.AcquireRootDirLock(rootDir)
					Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("acquire root lock: %v", err))

					return func() { _ = lock.Release() }
				},
			},
		}

		for _, tt := range tests {
			func() {
				_ = tt.name
				baseDir := leafwikiTempDir()
				dataDir := filepath.Join(baseDir, "data")
				rootDir := filepath.Join(baseDir, "content")
				Expect(os.MkdirAll(filepath.Join(dataDir, ".leafwiki"), 0o755)).To(Succeed())
				Expect(os.MkdirAll(rootDir, 0o755)).To(Succeed())
				canonicalData, canonicalRoot, err := projectdaemon.CanonicalizeProject(dataDir, rootDir)
				Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("canonicalize project: %v", err))

				descriptorPath := projectdaemon.DescriptorPath(canonicalData)
				Expect(os.WriteFile(descriptorPath, []byte("{"), 0o600)).To(Succeed(), fmt.Sprintf("write corrupt descriptor: %v", err))
				release := tt.lock(canonicalData, canonicalRoot)
				defer release()

				_, healthy, err := readHealthyProjectDaemon(context.Background(), descriptorPath, projectdaemon.Config{
					DataDir: canonicalData,
					RootDir: canonicalRoot,
				})
				Expect(err).To(HaveOccurred(), fmt.Sprintf("readHealthyProjectDaemon err = nil, want untrusted descriptor error while a project lock is held"))
				Expect(healthy).To(BeFalse(), fmt.Sprintf("healthy = true, want false"))

				_, statErr := os.Stat(descriptorPath)
				Expect(statErr).NotTo(HaveOccurred())

			}()
		}

	})
})

var _ = ginkgo.Describe("project daemon descriptor trust", func() {
	ginkgo.It("preserves trusted descriptor when locks held but control unreachable", func() {
		baseDir := leafwikiTempDir()
		dataDir := filepath.Join(baseDir, "data")
		rootDir := filepath.Join(baseDir, "content")
		Expect(os.MkdirAll(dataDir, 0o755)).To(Succeed())
		Expect(os.MkdirAll(rootDir, 0o755)).To(Succeed())
		cfg := testRuntimeConfig(dataDir, rootDir, freeTCPPort(), mcpTransports{}, true)
		ownerCfg, err := daemonConfigForRuntime(cfg)
		Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("daemon config: %v", err))

		hash, err := projectdaemon.ConfigHash(ownerCfg)
		Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("config hash: %v", err))

		descriptorPath := projectdaemon.DescriptorPath(ownerCfg.DataDir)
		Expect(projectdaemon.WriteDescriptorAtomic(descriptorPath, &projectdaemon.Descriptor{
			SchemaVersion:    projectdaemon.DescriptorSchemaVersion,
			PID:              12345,
			StartedAt:        time.Now(),
			DataDir:          ownerCfg.DataDir,
			RootDir:          ownerCfg.RootDir,
			PublicURL:        "http://127.0.0.1:" + ownerCfg.Port,
			PublicMCPEnabled: ownerCfg.PublicMCPEnabled,
			ControlURL:       "http://127.0.0.1:" + freeTCPPort(),
			ConfigHash:       hash,
			IdleTimeout:      ownerCfg.DaemonIdleTimeout,
			ControlToken:     "control-token",
			Config:           ownerCfg,
		})).To(Succeed())
		dataLock, err := locking.AcquireDataDirLock(ownerCfg.DataDir)
		Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("acquire data lock: %v", err))

		defer dataLock.Release()
		rootLock, err := locking.AcquireRootDirLock(ownerCfg.RootDir)
		Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("acquire root lock: %v", err))

		defer rootLock.Release()

		_, healthy, err := readHealthyProjectDaemon(context.Background(), descriptorPath, ownerCfg)
		Expect(err).To(HaveOccurred(), fmt.Sprintf("readHealthyProjectDaemon err = nil, want control health error while project locks are held"))
		Expect(healthy).To(BeFalse(), fmt.Sprintf("healthy = true, want false"))

		lowerErr := strings.ToLower(err.Error())
		Expect(lowerErr).To(SatisfyAny(
			ContainSubstring("control"),
			ContainSubstring("health"),
		), fmt.Sprintf("readHealthyProjectDaemon error = %v, want control/health context", err))

		_, statErr := os.Stat(descriptorPath)
		Expect(statErr).NotTo(HaveOccurred())

	})
})

var _ = ginkgo.Describe("project daemon descriptor trust", func() {
	ginkgo.It("preserves trusted unsupported schema descriptor when project lock held", func() {
		baseDir := leafwikiTempDir()
		dataDir := filepath.Join(baseDir, "data")
		rootDir := filepath.Join(baseDir, "content")
		Expect(os.MkdirAll(dataDir, 0o755)).To(Succeed())
		Expect(os.MkdirAll(rootDir, 0o755)).To(Succeed())
		cfg := testRuntimeConfig(dataDir, rootDir, freeTCPPort(), mcpTransports{}, true)
		ownerCfg, err := daemonConfigForRuntime(cfg)
		Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("daemon config: %v", err))

		hash, err := projectdaemon.ConfigHash(ownerCfg)
		Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("config hash: %v", err))

		descriptorPath := projectdaemon.DescriptorPath(ownerCfg.DataDir)
		Expect(projectdaemon.WriteDescriptorAtomic(descriptorPath, &projectdaemon.Descriptor{
			SchemaVersion:    projectdaemon.DescriptorSchemaVersion + 1,
			PID:              12345,
			StartedAt:        time.Now(),
			DataDir:          ownerCfg.DataDir,
			RootDir:          ownerCfg.RootDir,
			PublicURL:        "http://127.0.0.1:" + ownerCfg.Port,
			PublicMCPEnabled: ownerCfg.PublicMCPEnabled,
			ControlURL:       "http://127.0.0.1:" + freeTCPPort(),
			ConfigHash:       hash,
			IdleTimeout:      ownerCfg.DaemonIdleTimeout,
			ControlToken:     "control-token",
			Config:           ownerCfg,
		})).To(Succeed())
		dataLock, err := locking.AcquireDataDirLock(ownerCfg.DataDir)
		Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("acquire data lock: %v", err))

		defer dataLock.Release()

		_, healthy, err := readHealthyProjectDaemon(context.Background(), descriptorPath, ownerCfg)
		Expect(err).To(HaveOccurred(), fmt.Sprintf("readHealthyProjectDaemon err = nil, want unsupported schema error while project lock is held"))
		Expect(healthy).To(BeFalse(), fmt.Sprintf("healthy = true, want false"))
		Expect(strings.ToLower(err.Error())).To(ContainSubstring("schema"), fmt.Sprintf("readHealthyProjectDaemon error = %v, want schema context", err))

		_, statErr := os.Stat(descriptorPath)
		Expect(statErr).NotTo(HaveOccurred())

	})
})

var _ = ginkgo.Describe("leafwiki main process", func() {
	ginkgo.It("plain web owner supports later private STDIO attach", func() {
		baseDir := leafwikiTempDir()
		dataDir := filepath.Join(baseDir, "data")
		rootDir := filepath.Join(baseDir, "content")
		port := freeTCPPort()
		first := startLeafwikiHelper([]string{
			"--disable-auth",
			"--data-dir", dataDir,
			"--root-dir", rootDir,
			"--host", "127.0.0.1",
			"--port", port,
			"--log-target", "stderr",
		}, nil)
		waitForLeafwikiReady(first, port)

		stdout, stderr, err := runLeafwikiHelperWithTimeout([]string{
			"--mcp=stdio",
			"--disable-auth",
			"--data-dir", dataDir,
			"--root-dir", rootDir,
			"--host", "127.0.0.1",
			"--port", port,
			"--log-target", "stderr",
		}, nil, 5*time.Second)
		Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("stdio startup should attach to plain web owner, got %v\nstdout:\n%s\nstderr:\n%s", err, stdout, stderr))

		resp, err := http.Get("http://127.0.0.1:" + port + "/mcp")
		Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("GET /mcp: %v", err))

		defer resp.Body.Close()
		Expect(resp).To(HaveHTTPStatus(http.StatusNotFound))

	})
})

var _ = ginkgo.Describe("leafwiki main process", func() {
	ginkgo.It("agent presence control starts owner activity", func() {
		baseDir := leafwikiTempDir()
		dataDir := filepath.Join(baseDir, "data")
		rootDir := filepath.Join(baseDir, "content")
		port := freeTCPPort()
		first := startLeafwikiHelper([]string{
			"--disable-auth",
			"--data-dir", dataDir,
			"--root-dir", rootDir,
			"--host", "127.0.0.1",
			"--port", port,
			"--log-target", "stderr",
		}, nil)
		desc := waitForProjectDaemonDescriptor(dataDir)
		client := projectdaemon.NewClient(desc.ControlURL, desc.ControlToken)
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		Expect(client.RecordAgentPresence(ctx, agenthooks.Event{
			Provider:      agenthooks.ProviderCodex,
			SessionIDHash: agentHookSessionHash(agenthooks.ProviderCodex, "codex"),
			EventName:     "SessionStart",
			SeenAt:        time.Now(),
		})).To(Succeed(), fmt.Sprintf("stderr:\n%s", readFileString(first.stderrPath)))
		sessions, err := client.ListAgentPresence(ctx)
		Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("ListAgentPresence failed: %v", err))
		Expect(sessions).To(ContainElement(HaveField("SessionIDHash", Equal(agentHookSessionHash(agenthooks.ProviderCodex, "codex")))))

		waitForLeafwikiReady(first, port)

	})
})

var _ = ginkgo.Describe("leafwiki main process", func() {
	ginkgo.It("agent hook malformed JSON fails open", func() {
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
		Expect(stdout).To(Equal("{}\n"), fmt.Sprintf("stdout = %q, want Codex allow response", stdout))
		Expect(stderr).NotTo(ContainSubstring("{"), fmt.Sprintf("stderr leaked raw malformed payload: %s", stderr))

		_, err = os.Stat(projectdaemon.DescriptorPath(dataDir))
		Expect(err).To(MatchError(os.ErrNotExist))

	})
})

var _ = ginkgo.Describe("leafwiki main process", func() {
	ginkgo.It("agent hook starts daemon and records presence", func() {
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
		Expect(stdout).To(Equal("{}\n"), fmt.Sprintf("stdout = %q, want Codex allow response", stdout))
		Expect(stderr).NotTo(ContainSubstring("raw-codex-session"), fmt.Sprintf("stderr leaked hook session data: %s", stderr))
		Expect(stderr).NotTo(ContainSubstring(sensitivePrompt), fmt.Sprintf("stderr leaked hook prompt data: %s", stderr))

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
	ginkgo.It("agent hook replaces stale descriptor and fails open", func() {
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
		Expect(stdout).To(Equal("{}\n"), fmt.Sprintf("stdout = %q, want Codex allow response", stdout))
		Expect(stderr).NotTo(ContainSubstring("stale-descriptor-secret"), fmt.Sprintf("stderr leaked hook session data: %s", stderr))
		Expect(stderr).NotTo(ContainSubstring(sensitivePrompt), fmt.Sprintf("stderr leaked hook prompt data: %s", stderr))

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
	ginkgo.It("recovers panic and allows", func() {
		var stdout bytes.Buffer
		err := runAgentHookCommand(context.Background(), testRuntimeConfig(leafwikiTempDir(), filepath.Join(leafwikiTempDir(), "root"), freeTCPPort(), mcpTransports{}, true), agenthooks.ProviderCodex, panicReader{}, &stdout)
		Expect(err).To(HaveOccurred(), fmt.Sprintf("runAgentHookCommand err = nil, want panic surfaced as fail-open error"))
		Expect(stdout.String()).To(Equal("{}\n"), fmt.Sprintf("stdout = %q, want Codex allow response after panic", stdout.String()))

	})
})

var _ = ginkgo.Describe("agent-hook command", func() {
	ginkgo.It("read error fails open", func() {
		var stdout bytes.Buffer
		err := runAgentHookCommand(
			context.Background(),
			testRuntimeConfig(leafwikiTempDir(), filepath.Join(leafwikiTempDir(), "root"), freeTCPPort(), mcpTransports{}, true),
			agenthooks.ProviderClaude,
			errorReader{err: errors.New("synthetic read failure")},
			&stdout,
		)
		Expect(err).To(HaveOccurred(), fmt.Sprintf("runAgentHookCommand err = nil, want read error"))
		Expect(stdout.String()).To(Equal("{}\n"), fmt.Sprintf("stdout = %q, want Claude allow response after read error", stdout.String()))

	})
})

var _ = ginkgo.Describe("leafwiki main process", func() {
	ginkgo.It("agent hook pre dispatch failures fail open", func() {
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
		Expect(stdout).To(Equal("{}\n"), fmt.Sprintf("stdout = %q, want Codex allow response", stdout))
		Expect(stderr).NotTo(ContainSubstring("pre-dispatch-secret"), fmt.Sprintf("stderr leaked hook payload data: %s", stderr))

	})
})

var _ = ginkgo.Describe("leafwiki main process", func() {
	ginkgo.It("agent hook flag first pre dispatch failures fail open", func() {
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
		Expect(stdout).To(Equal("{}\n"), fmt.Sprintf("stdout = %q, want Codex allow response", stdout))
		Expect(stderr).NotTo(ContainSubstring("flag-first-secret"), fmt.Sprintf("stderr leaked hook payload data: %s", stderr))

	})
})

var _ = ginkgo.Describe("leafwiki main process", func() {
	ginkgo.It("non hook flag value named agent hook does not fail open", func() {
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
		Expect(err).To(HaveOccurred(), fmt.Sprintf("non-hook startup unexpectedly succeeded\nstdout:\n%s\nstderr:\n%s", stdout, stderr))
		Expect(stdout).NotTo(Equal("{}\n"), fmt.Sprintf("stdout = %q, want no agent-hook fail-open response", stdout))
		Expect(stderr).To(ContainSubstring(localizedMessage(localization.MessageIDCLIErrorInvalidWorkspaceConfig)), fmt.Sprintf("stderr = %q, want workspace configuration error", stderr))

	})
})

var _ = ginkgo.Describe("leafwiki main process", func() {
	ginkgo.It("non hook flag value named agent hook parse error does not fail open", func() {
		stdout, stderr, err := runLeafwikiHelperWithInputAndTimeout([]string{
			"--log-file", "agent-hook",
			"--not-a-real-flag",
		}, nil, `{"session_id":"should-not-be-hook"}`, 5*time.Second)
		Expect(err).To(HaveOccurred(), fmt.Sprintf("non-hook parse error unexpectedly succeeded\nstdout:\n%s\nstderr:\n%s", stdout, stderr))
		Expect(stdout).NotTo(Equal("{}\n"), fmt.Sprintf("stdout = %q, want no agent-hook fail-open response", stdout))
		Expect(stderr).To(ContainSubstring("not-a-real-flag"), fmt.Sprintf("stderr = %q, want flag parse error", stderr))

	})
})

var _ = ginkgo.Describe("leafwiki main process", func() {
	ginkgo.It("agent hook flag first parse errors fail open", func() {
		payload := `{"hook_event_name":"SessionStart","session_id":"flag-parse-secret"}`

		stdout, stderr, err := runLeafwikiHelperWithInputAndTimeout([]string{
			"--not-a-real-flag",
			"agent-hook", "codex",
		}, nil, payload, 5*time.Second)
		Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("flag-first agent-hook parse error should fail open, got %v\nstdout:\n%s\nstderr:\n%s", err, stdout, stderr))
		Expect(stdout).To(Equal("{}\n"), fmt.Sprintf("stdout = %q, want Codex allow response", stdout))
		Expect(stderr).NotTo(ContainSubstring("flag-parse-secret"), fmt.Sprintf("stderr leaked hook payload data: %s", stderr))

	})
})

var _ = ginkgo.Describe("leafwiki main process", func() {
	ginkgo.It("agent hook flag value named config does not disable fail open", func() {
		payload := `{"hook_event_name":"SessionStart","session_id":"flag-value-config-secret"}`

		stdout, stderr, err := runLeafwikiHelperWithInputAndTimeout([]string{
			"--data-dir", "--config",
			"--not-a-real-flag",
			"agent-hook", "codex",
		}, nil, payload, 5*time.Second)
		Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("non-config hook parse error should fail open, got %v\nstdout:\n%s\nstderr:\n%s", err, stdout, stderr))
		Expect(stdout).To(Equal("{}\n"), fmt.Sprintf("stdout = %q, want Codex allow response", stdout))
		Expect(stderr).NotTo(ContainSubstring("flag-value-config-secret"), fmt.Sprintf("stderr leaked hook payload data: %s", stderr))

	})
})

var _ = ginkgo.Describe("leafwiki main process", func() {
	ginkgo.It("agent hook malformed config flag does not disable fail open", func() {
		payload := `{"hook_event_name":"SessionStart","session_id":"malformed-config-flag-secret"}`

		stdout, stderr, err := runLeafwikiHelperWithInputAndTimeout([]string{
			"---config",
			"--not-a-real-flag",
			"agent-hook", "codex",
		}, nil, payload, 5*time.Second)
		Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("malformed non-config hook parse error should fail open, got %v\nstdout:\n%s\nstderr:\n%s", err, stdout, stderr))
		Expect(stdout).To(Equal("{}\n"), fmt.Sprintf("stdout = %q, want Codex allow response", stdout))
		Expect(stderr).NotTo(ContainSubstring("malformed-config-flag-secret"), fmt.Sprintf("stderr leaked hook payload data: %s", stderr))

	})
})

var _ = ginkgo.Describe("leafwiki main process", func() {
	ginkgo.It("agent hook provider allow responses fail open", func() {
		tests := []struct {
			name       string
			provider   agenthooks.ProviderID
			payload    string
			wantStdout string
		}{
			{name: "claude malformed", provider: agenthooks.ProviderClaude, payload: "{", wantStdout: "{}\n"},
			{name: "cursor malformed", provider: agenthooks.ProviderCursor, payload: "{", wantStdout: "{\"permission\":\"allow\"}\n"},
			{name: "unknown provider", provider: agenthooks.ProviderUnknown, payload: `{"hook_event_name":"SessionStart","session_id":"unknown-secret"}`, wantStdout: ""},
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
				Expect(stdout).To(Equal(tt.wantStdout), fmt.Sprintf("stdout = %q, want %q", stdout, tt.wantStdout))
				Expect(stderr).NotTo(ContainSubstring("unknown-secret"), fmt.Sprintf("stderr leaked hook payload data: %s", stderr))

			}()
		}

	})
})

var _ = ginkgo.Describe("agent-hook command", func() {
	ginkgo.It("oversized payload fails open", func() {
		var stdout bytes.Buffer
		baseDir := leafwikiTempDir()
		err := runAgentHookCommand(
			context.Background(),
			testRuntimeConfig(filepath.Join(baseDir, "data"), filepath.Join(baseDir, "root"), freeTCPPort(), mcpTransports{}, true),
			agenthooks.ProviderCursor,
			strings.NewReader(strings.Repeat("x", agentHookMaxPayloadBytes+1)),
			&stdout,
		)
		Expect(err).To(HaveOccurred(), fmt.Sprintf("runAgentHookCommand err = nil, want oversized payload error"))
		Expect(stdout.String()).To(Equal("{\"permission\":\"allow\"}\n"), fmt.Sprintf("stdout = %q, want Cursor allow response", stdout.String()))

	})
})

var _ = ginkgo.Describe("agent-hook command", func() {
	ginkgo.It("locked project fails open", func() {
		baseDir := leafwikiTempDir()
		dataDir := filepath.Join(baseDir, "data")
		rootDir := filepath.Join(baseDir, "content")
		Expect(os.MkdirAll(dataDir, 0o755)).To(Succeed())
		Expect(os.MkdirAll(rootDir, 0o755)).To(Succeed())
		canonicalData, canonicalRoot, err := projectdaemon.CanonicalizeProject(dataDir, rootDir)
		Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("canonicalize project: %v", err))

		dataLock, err := locking.AcquireDataDirLock(canonicalData)
		Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("acquire data lock: %v", err))

		defer dataLock.Release()
		rootLock, err := locking.AcquireRootDirLock(canonicalRoot)
		Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("acquire root lock: %v", err))

		defer rootLock.Release()

		var stdout bytes.Buffer
		err = runAgentHookCommand(
			context.Background(),
			testRuntimeConfig(dataDir, rootDir, freeTCPPort(), mcpTransports{}, true),
			agenthooks.ProviderCodex,
			strings.NewReader(`{"hook_event_name":"SessionStart","session_id":"locked-secret"}`),
			&stdout,
		)
		Expect(err).To(HaveOccurred(), fmt.Sprintf("runAgentHookCommand err = nil, want locked project error"))
		Expect(stdout.String()).To(Equal("{}\n"), fmt.Sprintf("stdout = %q, want Codex allow response", stdout.String()))

		Expect(err).To(SatisfyAny(
			MatchProjectDaemonConfigMismatch(),
			MatchError(errProjectLockedNoAttachableDaemon),
			Satisfy(locking.IsLockHeld),
		))

	})
})

var _ = ginkgo.Describe("agent-hook command", func() {
	ginkgo.It("control record failures fail open", func() {
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
				Expect(err).To(HaveOccurred(), fmt.Sprintf("runAgentHookCommand err = nil, want control failure"))
				Expect(stdout.String()).To(Equal("{}\n"), fmt.Sprintf("stdout = %q, want Codex allow response", stdout.String()))

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

var _ = ginkgo.Describe("leafwiki main process", func() {
	ginkgo.It("disabled auth owner rejects API key STDIO attach", func() {
		baseDir := leafwikiTempDir()
		dataDir := filepath.Join(baseDir, "data")
		rootDir := filepath.Join(baseDir, "content")
		port := freeTCPPort()
		first := startLeafwikiHelper([]string{
			"--disable-auth",
			"--data-dir", dataDir,
			"--root-dir", rootDir,
			"--host", "127.0.0.1",
			"--port", port,
			"--log-target", "stderr",
		}, nil)
		waitForLeafwikiReady(first, port)
		globalDesc := waitForGlobalWikidDescriptor(dataDir)
		ginkgo.DeferCleanup(func() {
			first.stop()
			terminateProjectDaemonProcess(globalDesc.PID)
			waitForLeafwikiUnavailable(port)
			waitForProjectLocksReusable(dataDir, rootDir, 15*time.Second)
		})

		apiKey := "lwk_disabled_auth_owner_process_secret"
		stdout, stderr, err := runLeafwikiHelperWithTimeout([]string{
			"--mcp=stdio",
			"--data-dir", dataDir,
			"--root-dir", rootDir,
			"--host", "127.0.0.1",
			"--port", port,
			"--log-target", "stderr",
		}, map[string]string{"LEAFWIKI_MCP_API_KEY": apiKey}, 5*time.Second)
		Expect(err).To(HaveOccurred(), fmt.Sprintf("API-key STDIO attach to disabled-auth owner unexpectedly succeeded\nstdout:\n%s\nstderr:\n%s", stdout, stderr))
		Expect(stdout).To(BeEmpty(), fmt.Sprintf("stdout = %q, want empty on rejected API-key attach", stdout))
		Expect(stdout).NotTo(ContainSubstring(apiKey), fmt.Sprintf("stdout leaked API key: %s", stdout))
		Expect(stderr).NotTo(ContainSubstring(apiKey), fmt.Sprintf("stderr leaked API key: %s", stderr))
		Expect(stderr).To(ContainSubstring(projectdaemon.FormatConfigMismatch([]projectdaemon.Mismatch{{
			Field: "auth-disabled",
		}})), fmt.Sprintf("stderr = %q, want auth-disabled daemon config mismatch", stderr))

	})
})

var _ = ginkgo.Describe("leafwiki main process", func() {
	ginkgo.It("non loopback plain web owner supports later private STDIO attach", func() {
		baseDir := leafwikiTempDir()
		dataDir := filepath.Join(baseDir, "data")
		rootDir := filepath.Join(baseDir, "content")
		port := freeTCPPort()
		first := startLeafwikiHelper([]string{
			"--disable-auth",
			"--data-dir", dataDir,
			"--root-dir", rootDir,
			"--host", "0.0.0.0",
			"--port", port,
			"--log-target", "stderr",
		}, nil)
		waitForLeafwikiReady(first, port)

		stdout, stderr, err := runLeafwikiHelperWithInputAndTimeout([]string{
			"--mcp=stdio",
			"--disable-auth",
			"--data-dir", dataDir,
			"--root-dir", rootDir,
			"--host", "127.0.0.1",
			"--port", port,
			"--log-target", "stderr",
		}, nil, nativeStdioListToolsInput(), 8*time.Second)
		Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("stdio startup should attach to non-loopback plain web owner, got %v\nstdout:\n%s\nstderr:\n%s", err, stdout, stderr))
		Expect(stdout).To(haveNativeStdioToolListResponse(2, "wiki_create_page"), fmt.Sprintf("stdout = %q, want tools/list response from private MCP bridge", stdout))
		Expect(stderr).NotTo(ContainSubstring(localizedMessage(localization.MessageIDCLIErrorInvalidMCPConfig)), fmt.Sprintf("stderr = %q, want no MCP config validation failure", stderr))
		Expect(stderr).NotTo(ContainSubstring(projectdaemon.FormatConfigMismatch(nil)), fmt.Sprintf("stderr = %q, want no config mismatch failure", stderr))

	})
})

var _ = ginkgo.Describe("leafwiki main process", func() {
	ginkgo.It("plain web second startup attaches to existing owner", func() {
		if !supportsGracefulProcessSignal() {
			ginkgo.Skip(fmt.Sprint("graceful process signaling is required to assert foreground session release"))
		}

		baseDir := leafwikiTempDir()
		dataDir := filepath.Join(baseDir, "data")
		rootDir := filepath.Join(baseDir, "content")
		port := freeTCPPort()
		first := startLeafwikiHelper([]string{
			"--disable-auth",
			"--data-dir", dataDir,
			"--root-dir", rootDir,
			"--host", "127.0.0.1",
			"--port", port,
			"--log-target", "stderr",
		}, map[string]string{})
		waitForLeafwikiReady(first, port)

		second := startLeafwikiHelper([]string{
			"--disable-auth",
			"--data-dir", dataDir,
			"--root-dir", rootDir,
			"--host", "127.0.0.1",
			"--port", port,
			"--log-target", "stderr",
		}, map[string]string{})
		waitForLeafwikiReady(second, port)

		waitForForegroundSignalHandler()
		Expect(signalLeafwikiProcess(first.cmd.Process)).To(Succeed())
		first.waitForExit()
		waitForLeafwikiReady(second, port)
		stderr := readFileString(second.stderrPath)
		for _, unexpected := range []string{"data directory is already in use", "root directory is already in use", "bind: address already in use", "project daemon config mismatch"} {
			Expect(stderr).NotTo(ContainSubstring(unexpected), fmt.Sprintf("second foreground startup stderr = %q, want no ownership failure %q", stderr, unexpected))

		}
		waitForForegroundSignalHandler()
		Expect(signalLeafwikiProcess(second.cmd.Process)).To(Succeed())
		second.waitForExit()
		waitForLeafwikiUnavailable(port)

	})
})

var _ = ginkgo.Describe("leafwiki main process", func() {
	ginkgo.It("plain web owner handles later private stdioMCP frames", func() {
		baseDir := leafwikiTempDir()
		dataDir := filepath.Join(baseDir, "data")
		rootDir := filepath.Join(baseDir, "content")
		port := freeTCPPort()
		first := startLeafwikiHelper([]string{
			"--disable-auth",
			"--data-dir", dataDir,
			"--root-dir", rootDir,
			"--host", "127.0.0.1",
			"--port", port,
			"--log-target", "stderr",
		}, nil)
		waitForLeafwikiReady(first, port)

		stdout, stderr, err := runLeafwikiHelperWithInputAndTimeout([]string{
			"--mcp=stdio",
			"--disable-auth",
			"--data-dir", dataDir,
			"--root-dir", rootDir,
			"--host", "127.0.0.1",
			"--port", port,
			"--log-target", "stderr",
		}, nil, nativeStdioListToolsInput(), 8*time.Second)
		Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("stdio startup should proxy MCP frames to existing plain owner, got %v\nstdout:\n%s\nstderr:\n%s", err, stdout, stderr))
		Expect(stdout).To(haveNativeStdioToolListResponse(2, "wiki_create_page"), fmt.Sprintf("stdout = %q, want tools/list response from private MCP bridge", stdout))
		Expect(stderr).NotTo(ContainSubstring(projectdaemon.FormatConfigMismatch(nil)), fmt.Sprintf("stderr = %q, want no config mismatch", stderr))

		resp, err := http.Get("http://127.0.0.1:" + port + "/mcp")
		Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("GET /mcp: %v", err))

		defer resp.Body.Close()
		Expect(resp).To(HaveHTTPStatus(http.StatusNotFound))

	})
})

var _ = ginkgo.Describe("leafwiki main process", func() {
	ginkgo.It("auth HTTP owner handles later private stdioMCP user context", func() {
		baseDir := leafwikiTempDir()
		dataDir := filepath.Join(baseDir, "data")
		rootDir := filepath.Join(baseDir, "content")
		apiKey := createWikidMCPAPIKeyWithUser(dataDir)
		port := freeTCPPort()
		first := startLeafwikiHelper([]string{
			"--mcp=http",
			"--data-dir", dataDir,
			"--root-dir", rootDir,
			"--host", "127.0.0.1",
			"--port", port,
			"--jwt-secret", "owner-jwt-secret",
			"--admin-password", "owner-admin-password",
			"--allow-insecure",
			"--log-target", "stderr",
		}, map[string]string{})
		waitForLeafwikiReady(first, port)
		grantWikidWorkspaceAccessForDirs(dataDir, rootDir, apiKey.UserID, wikid.GrantRoleEditor)

		stdout, stderr, err := runLeafwikiHelperWithInputAndTimeout([]string{
			"--mcp=stdio",
			"--data-dir", dataDir,
			"--root-dir", rootDir,
			"--host", "127.0.0.1",
			"--port", port,
			"--allow-insecure",
			"--log-target", "stderr",
		}, map[string]string{
			"LEAFWIKI_MCP_API_KEY": apiKey.Secret,
		}, nativeStdioToolCallInput(2, "wiki_get_current_user", map[string]any{}), 8*time.Second)
		Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("auth STDIO startup should proxy MCP frames to HTTP owner, got %v\nstdout:\n%s\nstderr:\n%s", err, stdout, stderr))
		Expect(stdout).To(haveNativeStdioTextResponse(2, SatisfyAll(
			ContainSubstring(`"username":"editor"`),
			ContainSubstring(`"role":"editor"`),
		)), fmt.Sprintf("stdout = %q, want get_current_user response for API-key editor", stdout))
		Expect(stderr).NotTo(ContainSubstring(errAuthJWTSecretRequired.Error()), fmt.Sprintf("stderr = %q, want no JWT bootstrap failure", stderr))
		Expect(stderr).NotTo(ContainSubstring(errAuthAdminPasswordRequired.Error()), fmt.Sprintf("stderr = %q, want no admin password bootstrap failure", stderr))
		Expect(stderr).NotTo(ContainSubstring(projectdaemon.FormatConfigMismatch(nil)), fmt.Sprintf("stderr = %q, want no config mismatch failure", stderr))

	})
})

var _ = ginkgo.Describe("leafwiki main process", func() {
	ginkgo.It("disabled auth STDIO CLIents collaborate through owner", func() {
		baseDir := leafwikiTempDir()
		dataDir := filepath.Join(baseDir, "data")
		rootDir := filepath.Join(baseDir, "content")
		port := freeTCPPort()
		slug := fmt.Sprintf("stdio-collaboration-%d", time.Now().UnixNano())
		title := "STDIO Collaboration Page"

		writerStdout, writerStderr, err := runLeafwikiHelperWithInputAndTimeout([]string{
			"--mcp=stdio",
			"--disable-auth",
			"--data-dir", dataDir,
			"--root-dir", rootDir,
			"--host", "127.0.0.1",
			"--port", port,
			"--log-target", "stderr",
		}, map[string]string{"LEAFWIKI_DAEMON_IDLE_TIMEOUT": "3s"}, nativeStdioToolCallInput(2, "wiki_create_page", map[string]any{
			"title": title,
			"slug":  slug,
			"kind":  "page",
		}), 8*time.Second)
		Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("writer STDIO client failed: %v\nstdout:\n%s\nstderr:\n%s", err, writerStdout, writerStderr))
		Expect(writerStdout).To(haveNativeStdioTextResponse(2, ContainSubstring(slug)), fmt.Sprintf("writer stdout = %q, want create_page response with slug %q", writerStdout, slug))

		readerStdout, readerStderr, err := runLeafwikiHelperWithInputAndTimeout([]string{
			"--mcp=stdio",
			"--disable-auth",
			"--data-dir", dataDir,
			"--root-dir", rootDir,
			"--host", "127.0.0.1",
			"--port", port,
			"--log-target", "stderr",
		}, map[string]string{"LEAFWIKI_DAEMON_IDLE_TIMEOUT": "3s"}, nativeStdioToolCallInput(2, "wiki_get_page_by_path", map[string]any{"path": slug}), 8*time.Second)
		Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("reader STDIO client failed: %v\nstdout:\n%s\nstderr:\n%s", err, readerStdout, readerStderr))
		Expect(readerStdout).To(haveNativeStdioTextResponse(2, SatisfyAll(
			ContainSubstring(slug),
			ContainSubstring(title),
		)), fmt.Sprintf("reader stdout = %q, want get_page_by_path response for writer-created page", readerStdout))

		waitForLeafwikiUnavailable(port)

	})
})

var _ = ginkgo.Describe("leafwiki main process", func() {
	ginkgo.It("plain web owner rejects later public MCP enablement", func() {
		baseDir := leafwikiTempDir()
		dataDir := filepath.Join(baseDir, "data")
		rootDir := filepath.Join(baseDir, "content")
		port := freeTCPPort()
		first := startLeafwikiHelper([]string{
			"--disable-auth",
			"--data-dir", dataDir,
			"--root-dir", rootDir,
			"--host", "127.0.0.1",
			"--port", port,
			"--log-target", "stderr",
		}, nil)
		waitForLeafwikiReady(first, port)
		globalDesc := waitForGlobalWikidDescriptor(dataDir)

		stdout, stderr, err := runLeafwikiHelperWithTimeout([]string{
			"--mcp=http",
			"--disable-auth",
			"--data-dir", dataDir,
			"--root-dir", rootDir,
			"--host", "127.0.0.1",
			"--port", port,
			"--log-target", "stderr",
		}, nil, 5*time.Second)
		Expect(err).To(HaveOccurred(), fmt.Sprintf("later public MCP enablement unexpectedly succeeded\nstdout:\n%s\nstderr:\n%s", stdout, stderr))
		Expect(err).NotTo(MatchError(context.DeadlineExceeded), fmt.Sprintf("later public MCP enablement hung; expected config mismatch\nstdout:\n%s\nstderr:\n%s", stdout, stderr))
		Expect(stderr).To(ContainSubstring(projectdaemon.FormatConfigMismatch([]projectdaemon.Mismatch{{
			Field: "public-mcp-enabled",
		}})), fmt.Sprintf("stderr = %q, want public MCP config mismatch", stderr))

		first.stop()
		terminateProjectDaemonProcess(globalDesc.PID)
		waitForLeafwikiUnavailable(port)
		waitForProjectLocksReusable(dataDir, rootDir, 15*time.Second)

	})
})

var _ = ginkgo.Describe("leafwiki main process", func() {
	ginkgo.It("base path owner rejects later no base path startup", func() {
		baseDir := leafwikiTempDir()
		dataDir := filepath.Join(baseDir, "data")
		rootDir := filepath.Join(baseDir, "content")
		port := freeTCPPort()
		first := startLeafwikiHelper([]string{
			"--disable-auth",
			"--data-dir", dataDir,
			"--root-dir", rootDir,
			"--host", "127.0.0.1",
			"--port", port,
			"--base-path", "/wiki",
			"--log-target", "stderr",
		}, nil)
		desc := waitForProjectDaemonDescriptor(dataDir)
		Expect(desc.BasePath).To(Equal("/wiki"), fmt.Sprintf("descriptor base path = %q, want /wiki", desc.BasePath))

		waitForLeafwikiReadyAtBasePath(first, port, "/wiki")

		stdout, stderr, err := runLeafwikiHelperWithTimeout([]string{
			"--disable-auth",
			"--data-dir", dataDir,
			"--root-dir", rootDir,
			"--host", "127.0.0.1",
			"--port", port,
			"--log-target", "stderr",
		}, nil, 5*time.Second)
		Expect(err).To(HaveOccurred(), fmt.Sprintf("later no-base-path startup unexpectedly succeeded\nstdout:\n%s\nstderr:\n%s", stdout, stderr))
		Expect(err).NotTo(MatchError(context.DeadlineExceeded), fmt.Sprintf("later no-base-path startup hung; expected config mismatch\nstdout:\n%s\nstderr:\n%s", stdout, stderr))
		Expect(stderr).To(ContainSubstring(projectdaemon.FormatConfigMismatch([]projectdaemon.Mismatch{{
			Field: "base-path",
		}})), fmt.Sprintf("stderr = %q, want base-path config mismatch", stderr))

	})
})

var _ = ginkgo.Describe("leafwiki main process", func() {
	ginkgo.It("auth owner rejects later disable auth startup", func() {
		baseDir := leafwikiTempDir()
		dataDir := filepath.Join(baseDir, "data")
		rootDir := filepath.Join(baseDir, "content")
		port := freeTCPPort()
		first := startLeafwikiHelper([]string{
			"--data-dir", dataDir,
			"--root-dir", rootDir,
			"--host", "127.0.0.1",
			"--port", port,
			"--jwt-secret", "owner-jwt-secret",
			"--admin-password", "owner-admin-password",
			"--allow-insecure",
			"--log-target", "stderr",
		}, nil)
		waitForLeafwikiReady(first, port)

		stdout, stderr, err := runLeafwikiHelperWithTimeout([]string{
			"--disable-auth",
			"--data-dir", dataDir,
			"--root-dir", rootDir,
			"--host", "127.0.0.1",
			"--port", port,
			"--log-target", "stderr",
		}, nil, 5*time.Second)
		Expect(err).To(HaveOccurred(), fmt.Sprintf("later disable-auth startup unexpectedly succeeded\nstdout:\n%s\nstderr:\n%s", stdout, stderr))
		Expect(err).NotTo(MatchError(context.DeadlineExceeded), fmt.Sprintf("later disable-auth startup hung; expected config mismatch\nstdout:\n%s\nstderr:\n%s", stdout, stderr))
		Expect(stderr).To(ContainSubstring(projectdaemon.FormatConfigMismatch([]projectdaemon.Mismatch{{
			Field: "auth-disabled",
		}})), fmt.Sprintf("stderr = %q, want auth-disabled config mismatch", stderr))

	})
})

var _ = ginkgo.Describe("leafwiki main process", func() {
	ginkgo.It("project daemon descriptor uses default idle timeout when unspecified", func() {
		var ownerPID int
		ginkgo.DeferCleanup(func() {
			terminateProjectDaemonProcess(ownerPID)
		})
		baseDir := leafwikiTempDir()
		dataDir := filepath.Join(baseDir, "data")
		rootDir := filepath.Join(baseDir, "content")
		port := freeTCPPort()
		proc := startLeafwikiHelper([]string{
			"--disable-auth",
			"--data-dir", dataDir,
			"--root-dir", rootDir,
			"--host", "127.0.0.1",
			"--port", port,
			"--log-target", "stderr",
		}, map[string]string{"LEAFWIKI_DAEMON_IDLE_TIMEOUT": ""})
		desc := waitForProjectDaemonDescriptor(dataDir)
		ownerPID = desc.PID
		waitForLeafwikiReady(proc, port)
		Expect(desc.IdleTimeout).To(Equal("10m0s"), fmt.Sprintf("descriptor idle timeout = %q, want core CLI default 10m0s", desc.IdleTimeout))
		Expect(desc.Config.DaemonIdleTimeout).To(Equal("10m0s"), fmt.Sprintf("descriptor config daemon idle timeout = %q, want core CLI default 10m0s", desc.Config.DaemonIdleTimeout))

		proc.stop()

	})
})

var _ = ginkgo.Describe("leafwiki main process", func() {
	ginkgo.It("wikidfrontd runtime writes role descriptor", func() {
		var ownerPID int
		ginkgo.DeferCleanup(func() {
			terminateProjectDaemonProcess(ownerPID)
		})
		baseDir := leafwikiTempDir()
		dataDir := filepath.Join(baseDir, "data")
		rootDir := filepath.Join(baseDir, "content")
		port := freeTCPPort()
		proc := startLeafwikiHelper([]string{
			"--disable-auth",
			"--data-dir", dataDir,
			"--root-dir", rootDir,
			"--host", "127.0.0.1",
			"--port", port,
			"--log-target", "stderr",
		}, map[string]string{})

		workspaceDesc := waitForProjectDaemonDescriptor(dataDir)
		waitForLeafwikiReady(proc, port)
		layout := leafwikiHelperGlobalLayoutForDataDir(dataDir)
		globalDesc := waitForGlobalWikidDescriptor(dataDir)
		ownerPID = globalDesc.PID
		Expect(globalDesc).To(SatisfyAll(
			HaveField("RuntimeStack", Equal(projectdaemon.RuntimeStackWikidFrontd)),
			HaveField("Role", Equal(projectdaemon.RoleWikid)),
			HaveField("WorkspaceID", Equal(wikid.HomeWorkspaceID)),
			HaveField("PrivateMCPURL", Not(BeEmpty())),
			HaveField("PrivateMCPToken", Not(BeEmpty())),
		), fmt.Sprintf("global descriptor runtime metadata = %#v, want wikid home descriptor", globalDesc))
		Expect(globalDesc.Config).To(SatisfyAll(
			HaveField("WorkspaceID", Equal(wikid.HomeWorkspaceID)),
			HaveField("DataDir", Equal(layout.HomeDir)),
			HaveField("RootDir", Equal(layout.HomeRootDir)),
		), fmt.Sprintf("global descriptor config = %#v, want home workspace config", globalDesc.Config))
		Expect(workspaceDesc).To(SatisfyAll(
			HaveField("Role", Equal(projectdaemon.RoleWorkspaced)),
			HaveField("WorkspaceID", Not(BeEmpty())),
			HaveField("WorkspaceID", Not(Equal(wikid.HomeWorkspaceID))),
		), fmt.Sprintf("workspace descriptor role/workspace = %q/%q, want non-home workspaced", workspaceDesc.Role, workspaceDesc.WorkspaceID))
		Expect(workspaceDesc.Config.Port).To(Equal("0"), fmt.Sprintf("workspace descriptor config port = %q, want ephemeral port 0", workspaceDesc.Config.Port))
		Expect(workspaceDesc.PrivateMCPURL).To(SatisfyAll(
			Not(BeEmpty()),
			Not(ContainSubstring(":0/")),
		), fmt.Sprintf("workspace descriptor private MCP URL = %q, want actual listener URL", workspaceDesc.PrivateMCPURL))

		gotRoles := map[projectdaemon.RoleName]projectdaemon.RoleHealth{}
		for _, role := range globalDesc.Roles {
			gotRoles[role.Name] = role
		}
		for _, name := range []projectdaemon.RoleName{projectdaemon.RoleWikid, projectdaemon.RoleFrontd, projectdaemon.RoleWorkspaced} {
			Expect(gotRoles[name].State).To(Equal(projectdaemon.RoleStateReady), fmt.Sprintf("role %s state = %q, want ready; all roles = %#v", name, gotRoles[name].State, globalDesc.Roles))
			Expect(gotRoles[name].PID).To(BeNumerically(">", 0), fmt.Sprintf("role %s PID = %d, want live role process; all roles = %#v", name, gotRoles[name].PID, globalDesc.Roles))
			Expect(processExists(gotRoles[name].PID)).To(BeTrue(), fmt.Sprintf("role %s PID %d is not running; all roles = %#v", name, gotRoles[name].PID, globalDesc.Roles))

		}
		Expect(gotRoles[projectdaemon.RoleWikid].PID).To(Equal(globalDesc.PID), fmt.Sprintf("wikid PID = %d, want descriptor owner PID %d", gotRoles[projectdaemon.RoleWikid].PID, globalDesc.PID))
		Expect(gotRoles[projectdaemon.RoleFrontd].PID).NotTo(Equal(globalDesc.PID), fmt.Sprintf("frontd PID must differ from wikid descriptor PID; descriptor PID = %d roles = %#v", globalDesc.PID, globalDesc.Roles))
		Expect(gotRoles[projectdaemon.RoleWorkspaced].PID).NotTo(Equal(globalDesc.PID), fmt.Sprintf("workspaced PID must differ from wikid descriptor PID; descriptor PID = %d roles = %#v", globalDesc.PID, globalDesc.Roles))
		Expect(gotRoles[projectdaemon.RoleFrontd].PID).NotTo(Equal(gotRoles[projectdaemon.RoleWorkspaced].PID), fmt.Sprintf("frontd and workspaced PIDs must differ; roles = %#v", globalDesc.Roles))

		proc.stop()

	})
})

var _ = ginkgo.Describe("leafwiki main process", func() {
	ginkgo.It("wikidfrontd runtime lists home workspace", func() {
		var ownerPID int
		ginkgo.DeferCleanup(func() {
			terminateProjectDaemonProcess(ownerPID)
		})
		baseDir := leafwikiTempDir()
		dataDir := filepath.Join(baseDir, "data")
		rootDir := filepath.Join(baseDir, "content")
		port := freeTCPPort()
		proc := startLeafwikiHelper([]string{
			"--disable-auth",
			"--data-dir", dataDir,
			"--root-dir", rootDir,
			"--host", "127.0.0.1",
			"--port", port,
			"--log-target", "stderr",
		}, map[string]string{})

		desc := waitForProjectDaemonDescriptor(dataDir)
		ownerPID = desc.PID
		waitForLeafwikiReady(proc, port)

		resp, err := http.Get("http://127.0.0.1:" + port + "/api/workspaces")
		Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("GET /api/workspaces: %v", err))

		defer resp.Body.Close()
		Expect(resp).To(HaveHTTPStatus(http.StatusOK))
		body, err := io.ReadAll(resp.Body)
		Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("read /api/workspaces: %v", err))

		var out wikid.WorkspaceListResponse
		Expect(json.Unmarshal(body, &out)).To(Succeed(), fmt.Sprintf("decode /api/workspaces: %v", err))
		layout := leafwikiHelperGlobalLayoutForDataDir(dataDir)
		for _, leaked := range []string{"dataDir", "rootDir", layout.HomeDir, layout.HomeRootDir, dataDir, rootDir} {
			Expect(string(body)).NotTo(ContainSubstring(leaked), fmt.Sprintf("/api/workspaces leaked workspace path field %q: %s", leaked, body))

		}
		Expect(out.Workspaces).To(HaveLen(2), fmt.Sprintf("workspace list = %#v, want home plus first-contact workspace", out.Workspaces))
		Expect(out.Workspaces).To(ContainElement(HaveField("ID", Equal(wikid.HomeWorkspaceID))))
		Expect(out.Workspaces).To(ContainElement(HaveField("ID", Not(Equal(wikid.HomeWorkspaceID)))))

		proc.stop()

	})
})

var _ = ginkgo.Describe("leafwiki main process", func() {
	ginkgo.It("wikidfrontd runtime proxies workspace API by ID", func() {
		var ownerPID int
		ginkgo.DeferCleanup(func() {
			terminateProjectDaemonProcess(ownerPID)
		})
		baseDir := leafwikiTempDir()
		dataDir := filepath.Join(baseDir, "data")
		rootDir := filepath.Join(baseDir, "content")
		port := freeTCPPort()
		proc := startLeafwikiHelper([]string{
			"--disable-auth",
			"--data-dir", dataDir,
			"--root-dir", rootDir,
			"--host", "127.0.0.1",
			"--port", port,
			"--log-target", "stderr",
		}, map[string]string{})

		desc := waitForProjectDaemonDescriptor(dataDir)
		ownerPID = desc.PID
		waitForLeafwikiReady(proc, port)

		resp, err := http.Get("http://127.0.0.1:" + port + "/api/workspaces/home/tree")
		Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("GET /api/workspaces/home/tree: %v", err))

		defer resp.Body.Close()
		Expect(resp).To(HaveHTTPStatus(http.StatusOK))
		var tree map[string]any
		Expect(json.NewDecoder(resp.Body).Decode(&tree)).To(Succeed(), fmt.Sprintf("decode tree: %v", err))
		Expect(tree).To(HaveKey("children"))

		proc.stop()

	})
})

var _ = ginkgo.Describe("leafwiki main process", func() {
	ginkgo.It("wikidfrontd runtime ensures registered workspace by ID", func() {
		var ownerPID int
		ginkgo.DeferCleanup(func() {
			terminateProjectDaemonProcess(ownerPID)
		})
		baseDir := leafwikiTempDir()
		dataDir := filepath.Join(baseDir, "data")
		rootDir := filepath.Join(baseDir, "content")
		secondDataDir := filepath.Join(baseDir, "second-data")
		secondRootDir := filepath.Join(baseDir, "second-root")
		Expect(os.MkdirAll(secondRootDir, 0o755)).To(Succeed())
		Expect(os.WriteFile(filepath.Join(secondRootDir, "index.md"), []byte("# Second\n"), 0o644)).To(Succeed())
		port := freeTCPPort()
		proc := startLeafwikiHelper([]string{
			"--disable-auth",
			"--data-dir", dataDir,
			"--root-dir", rootDir,
			"--host", "127.0.0.1",
			"--port", port,
			"--log-target", "stderr",
		}, map[string]string{})

		_ = waitForProjectDaemonDescriptor(dataDir)
		waitForLeafwikiReady(proc, port)
		layout := leafwikiHelperGlobalLayoutForDataDir(dataDir)
		globalDesc, err := projectdaemon.ReadTrustedDescriptor(projectdaemon.GlobalDescriptorPath(layout.RuntimeDir, projectdaemon.RoleWikid))
		Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("read global wikid descriptor: %v", err))

		ownerPID = globalDesc.PID
		registry := wikid.NewRegistryService(wikid.NewRegistryStore(layout.DBPath), layout)
		second, err := registry.RegisterWorkspace(wikid.RegisterWorkspaceRequest{
			DisplayName: "Second",
			DataDir:     secondDataDir,
			RootDir:     secondRootDir,
		})
		Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("register second workspace: %v", err))

		grants := wikid.NewGrantStore(layout.DBPath)
		Expect(grants.Upsert(wikid.Grant{Subject: "user:public-editor", WorkspaceID: second.ID, Role: wikid.GrantRoleEditor})).To(Succeed(), fmt.Sprintf("grant second workspace: %v", err))

		resp, err := http.Get("http://127.0.0.1:" + port + "/api/workspaces/" + second.ID.URLPathSegment() + "/tree")
		Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("GET second workspace tree: %v", err))

		defer resp.Body.Close()
		Expect(resp).To(HaveHTTPStatus(http.StatusOK))
		var tree map[string]any
		Expect(json.NewDecoder(resp.Body).Decode(&tree)).To(Succeed(), fmt.Sprintf("decode second tree: %v", err))
		query := url.Values{}
		query.Set("path", "")
		query.Set("kind", "section")
		pageResp, err := http.Get("http://127.0.0.1:" + port + "/api/workspaces/" + second.ID.URLPathSegment() + "/pages/by-path?" + query.Encode())
		Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("GET second workspace root page: %v", err))

		defer pageResp.Body.Close()
		Expect(pageResp).To(HaveHTTPStatus(http.StatusOK))
		var page struct {
			Content string `json:"content"`
			Kind    string `json:"kind"`
		}
		Expect(json.NewDecoder(pageResp.Body).Decode(&page)).To(Succeed(), fmt.Sprintf("decode second root page: %v", err))
		Expect(page).To(SatisfyAll(
			HaveField("Kind", Equal("section")),
			HaveField("Content", ContainSubstring("Second")),
		), fmt.Sprintf("second root page = %#v, want section containing Second", page))

		secondDesc := waitForProjectDaemonDescriptor(secondDataDir)
		Expect(secondDesc).To(SatisfyAll(
			HaveField("Role", Equal(projectdaemon.RoleWorkspaced)),
			HaveField("WorkspaceID", Equal(second.ID)),
			HaveField("PrivateMCPURL", Not(BeEmpty())),
			HaveField("PrivateMCPToken", Not(BeEmpty())),
		), fmt.Sprintf("second descriptor role/workspace/private MCP fields = %#v, want workspaced/%q with private MCP", secondDesc, second.ID))

		proc.stop()

	})
})

var _ = ginkgo.Describe("federated STDIO attach", func() {
	ginkgo.It("rejects descriptor for different registered workspace", func() {
		baseDir := leafwikiTempDir()
		leafwikiSetenv("HOME", filepath.Join(baseDir, "home"))
		dataDir := filepath.Join(baseDir, "beta-data")
		rootDir := filepath.Join(baseDir, "beta-root")
		Expect(os.MkdirAll(dataDir, 0o755)).To(Succeed())
		Expect(os.MkdirAll(rootDir, 0o755)).To(Succeed())

		cfg := testRuntimeConfig(dataDir, rootDir, "0", mcpTransports{Stdio: true}, true)
		cfg.RuntimeStack = projectdaemon.RuntimeStackWikidFrontd
		requestCfg, err := daemonWorkspaceRequestConfigForRuntime(cfg)
		Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("workspace request config: %v", err))

		globalCfg, err := daemonRequestConfigForRuntime(cfg)
		Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("global request config: %v", err))

		layout := wikid.GlobalLayout(globalCfg.DataDir)
		registry := wikid.NewRegistryService(wikid.NewRegistryStore(layout.DBPath), layout)
		registered, err := registry.RegisterWorkspace(wikid.RegisterWorkspaceRequest{
			DisplayName: "Beta",
			DataDir:     requestCfg.DataDir,
			RootDir:     requestCfg.RootDir,
		})
		Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("register beta workspace: %v", err))
		Expect(registered.ID).NotTo(Equal(workspaceid.WorkspaceID("alpha")), fmt.Sprintf("registered workspace ID unexpectedly matched stale descriptor ID"))

		const privateToken = "private-token"
		privateMCP := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			if req.Header.Get(projectdaemon.ControlTokenHeader) != privateToken {
				http.Error(w, "bad token", http.StatusUnauthorized)
				return
			}
			w.WriteHeader(http.StatusOK)
		}))
		defer privateMCP.Close()

		alphaCfg := requestCfg
		alphaCfg.WorkspaceID = "alpha"
		configHash, err := projectdaemon.ConfigHash(alphaCfg)
		Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("config hash: %v", err))

		descriptorPath := projectdaemon.DescriptorPath(requestCfg.DataDir)
		Expect(projectdaemon.WriteDescriptorAtomic(descriptorPath, &projectdaemon.Descriptor{
			SchemaVersion:   projectdaemon.DescriptorSchemaVersion,
			RuntimeStack:    projectdaemon.RuntimeStackWikidFrontd,
			Role:            projectdaemon.RoleWorkspaced,
			WorkspaceID:     "alpha",
			PID:             os.Getpid(),
			StartedAt:       time.Now().UTC(),
			DataDir:         requestCfg.DataDir,
			RootDir:         requestCfg.RootDir,
			PrivateMCPURL:   privateMCP.URL,
			PrivateMCPToken: privateToken,
			ConfigHash:      configHash,
			Config:          alphaCfg,
		})).To(Succeed())

		_, err = attachOrStartFederatedProjectDaemon(context.Background(), cfg, requestCfg, descriptorPath)
		Expect(err).To(HaveOccurred(), fmt.Sprintf("attach with wrong workspace descriptor unexpectedly succeeded"))

		Expect(err).To(MatchProjectDaemonWorkspaceIDMismatch("alpha", registered.ID))

	})
})

var _ = ginkgo.Describe("federated workspace manager", func() {
	ginkgo.It("ensure single flights concurrent startup", func() {
		supervisor := wikid.NewWorkspaceSupervisor(wikid.WorkspaceSupervisorOptions{})
		manager := newFederatedWorkspaceManager(
			leafwikiRuntimeConfig{},
			"daemon-token",
			"http://127.0.0.1:1",
			wikid.GlobalLayout(leafwikiTempDir()),
			supervisor,
		)
		manager.writeDescriptor = func(wikid.WorkspaceRecord, leafwikiRuntimeConfig, internalRuntimeRoleReady) error {
			return nil
		}

		started := make(chan struct{}, 1)
		releaseStart := make(chan struct{})
		processDone := make(chan error)
		var startMu sync.Mutex
		startCount := 0
		manager.startRole = func(internalRuntimeRoleStartupConfig) (*internalRuntimeRoleProcess, internalRuntimeRoleReady, error) {
			startMu.Lock()
			startCount++
			startMu.Unlock()
			started <- struct{}{}
			select {
			case <-releaseStart:
			case <-time.After(2 * time.Second):
				return nil, internalRuntimeRoleReady{}, context.DeadlineExceeded
			}
			return testRuntimeRoleProcess(projectdaemon.RoleWorkspaced, 101, processDone), internalRuntimeRoleReady{
				Role: projectdaemon.RoleWorkspaced,
				PID:  101,
				URL:  "http://127.0.0.1:41001",
			}, nil
		}

		workspace := wikid.WorkspaceRecord{
			ID:      "alpha",
			DataDir: leafwikiTempDir(),
			RootDir: leafwikiTempDir(),
		}
		var wg sync.WaitGroup
		results := make(chan wikid.WorkspaceStatus, 2)
		errs := make(chan error, 2)
		for i := 0; i < 2; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				status, err := manager.Ensure(context.Background(), workspace)
				results <- status
				errs <- err
			}()
		}
		Eventually(started).WithTimeout(2 * time.Second).Should(Receive())
		startMu.Lock()
		Expect(startCount).To(Equal(1), fmt.Sprintf("start count while first startup is running = %d, want 1", startCount))

		startMu.Unlock()
		close(releaseStart)
		wg.Wait()
		close(results)
		close(errs)

		for err := range errs {
			Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("Ensure returned error: %v", err))

		}
		var statuses []wikid.WorkspaceStatus
		for status := range results {
			statuses = append(statuses, status)
		}
		Expect(statuses).To(ConsistOf(
			SatisfyAll(HaveField("State", Equal(wikid.WorkspaceStateRunning)), HaveField("PID", Equal(101))),
			SatisfyAll(HaveField("State", Equal(wikid.WorkspaceStateRunning)), HaveField("PID", Equal(101))),
		))

		startMu.Lock()
		defer startMu.Unlock()
		Expect(startCount).To(Equal(1), fmt.Sprintf("start count after concurrent ensure = %d, want 1", startCount))

	})
})

var _ = ginkgo.Describe("federated workspace manager", func() {
	ginkgo.It("ensure canceled duplicate waiter returns context error", func() {
		supervisor := wikid.NewWorkspaceSupervisor(wikid.WorkspaceSupervisorOptions{})
		manager := newFederatedWorkspaceManager(
			leafwikiRuntimeConfig{},
			"daemon-token",
			"http://127.0.0.1:1",
			wikid.GlobalLayout(leafwikiTempDir()),
			supervisor,
		)
		manager.writeDescriptor = func(wikid.WorkspaceRecord, leafwikiRuntimeConfig, internalRuntimeRoleReady) error {
			return nil
		}

		started := make(chan struct{}, 1)
		releaseStart := make(chan struct{})
		processDone := make(chan error)
		var startMu sync.Mutex
		startCount := 0
		manager.startRole = func(internalRuntimeRoleStartupConfig) (*internalRuntimeRoleProcess, internalRuntimeRoleReady, error) {
			startMu.Lock()
			startCount++
			startMu.Unlock()
			started <- struct{}{}
			select {
			case <-releaseStart:
			case <-time.After(2 * time.Second):
				return nil, internalRuntimeRoleReady{}, context.DeadlineExceeded
			}
			return testRuntimeRoleProcess(projectdaemon.RoleWorkspaced, 101, processDone), internalRuntimeRoleReady{
				Role: projectdaemon.RoleWorkspaced,
				PID:  101,
				URL:  "http://127.0.0.1:41001",
			}, nil
		}

		workspace := wikid.WorkspaceRecord{ID: "alpha", DataDir: leafwikiTempDir(), RootDir: leafwikiTempDir()}
		firstDone := make(chan error, 1)
		go func() {
			_, err := manager.Ensure(context.Background(), workspace)
			firstDone <- err
		}()
		Eventually(started).WithTimeout(2 * time.Second).Should(Receive())

		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		_, err := manager.Ensure(ctx, workspace)
		Expect(err).To(MatchError(context.Canceled))

		startMu.Lock()
		Expect(startCount).To(Equal(1), fmt.Sprintf("start count after canceled duplicate waiter = %d, want 1", startCount))

		startMu.Unlock()

		close(releaseStart)
		Eventually(firstDone).WithTimeout(2 * time.Second).Should(Receive(Succeed()))
		startMu.Lock()
		defer startMu.Unlock()
		Expect(startCount).To(Equal(1), fmt.Sprintf("start count after first Ensure finished = %d, want 1", startCount))

	})
})

var _ = ginkgo.Describe("federated workspace manager", func() {
	ginkgo.It("ensure failure does not poison retry", func() {
		supervisor := wikid.NewWorkspaceSupervisor(wikid.WorkspaceSupervisorOptions{})
		manager := newFederatedWorkspaceManager(
			leafwikiRuntimeConfig{},
			"daemon-token",
			"http://127.0.0.1:1",
			wikid.GlobalLayout(leafwikiTempDir()),
			supervisor,
		)
		manager.writeDescriptor = func(wikid.WorkspaceRecord, leafwikiRuntimeConfig, internalRuntimeRoleReady) error {
			return nil
		}

		processDone := make(chan error)
		var startMu sync.Mutex
		startCount := 0
		manager.startRole = func(internalRuntimeRoleStartupConfig) (*internalRuntimeRoleProcess, internalRuntimeRoleReady, error) {
			startMu.Lock()
			defer startMu.Unlock()
			startCount++
			if startCount == 1 {
				return nil, internalRuntimeRoleReady{}, errors.New("boom")
			}
			return testRuntimeRoleProcess(projectdaemon.RoleWorkspaced, 202, processDone), internalRuntimeRoleReady{
				Role: projectdaemon.RoleWorkspaced,
				PID:  202,
				URL:  "http://127.0.0.1:41002",
			}, nil
		}

		workspace := wikid.WorkspaceRecord{ID: "alpha", DataDir: leafwikiTempDir(), RootDir: leafwikiTempDir()}
		_, err := manager.Ensure(context.Background(), workspace)
		Expect(err).To(HaveOccurred())
		status, err := manager.Ensure(context.Background(), workspace)
		Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("retry Ensure returned error: %v", err))
		Expect(status).To(SatisfyAll(
			HaveField("State", Equal(wikid.WorkspaceStateRunning)),
			HaveField("PID", Equal(202)),
		), fmt.Sprintf("retry status = %#v, want running pid 202", status))

		startMu.Lock()
		defer startMu.Unlock()
		Expect(startCount).To(Equal(2), fmt.Sprintf("start count after retry = %d, want 2", startCount))

	})
})

var _ = ginkgo.Describe("federated workspace manager", func() {
	ginkgo.It("ensure does not serialize different workspaces", func() {
		supervisor := wikid.NewWorkspaceSupervisor(wikid.WorkspaceSupervisorOptions{})
		manager := newFederatedWorkspaceManager(
			leafwikiRuntimeConfig{},
			"daemon-token",
			"http://127.0.0.1:1",
			wikid.GlobalLayout(leafwikiTempDir()),
			supervisor,
		)
		manager.writeDescriptor = func(wikid.WorkspaceRecord, leafwikiRuntimeConfig, internalRuntimeRoleReady) error {
			return nil
		}

		started := make(chan workspaceid.WorkspaceID, 2)
		releaseStart := make(chan struct{})
		processDone := make(chan error)
		manager.startRole = func(startup internalRuntimeRoleStartupConfig) (*internalRuntimeRoleProcess, internalRuntimeRoleReady, error) {
			workspaceID := startup.Runtime.Workspace.ID
			started <- workspaceID
			select {
			case <-releaseStart:
			case <-time.After(2 * time.Second):
				return nil, internalRuntimeRoleReady{}, context.DeadlineExceeded
			}
			pid := 101
			url := "http://127.0.0.1:41001"
			if workspaceID == "beta" {
				pid = 202
				url = "http://127.0.0.1:41002"
			}
			return testRuntimeRoleProcess(projectdaemon.RoleWorkspaced, pid, processDone), internalRuntimeRoleReady{
				Role: projectdaemon.RoleWorkspaced,
				PID:  pid,
				URL:  url,
			}, nil
		}

		workspaces := []wikid.WorkspaceRecord{
			{ID: "alpha", DataDir: leafwikiTempDir(), RootDir: leafwikiTempDir()},
			{ID: "beta", DataDir: leafwikiTempDir(), RootDir: leafwikiTempDir()},
		}
		var wg sync.WaitGroup
		errs := make(chan error, len(workspaces))
		for _, workspace := range workspaces {
			workspace := workspace
			wg.Add(1)
			go func() {
				defer wg.Done()
				_, err := manager.Ensure(context.Background(), workspace)
				errs <- err
			}()
		}

		seen := map[workspaceid.WorkspaceID]bool{}
		Eventually(func(g Gomega) {
			for len(seen) < 2 {
				var workspaceID workspaceid.WorkspaceID
				g.Expect(started).To(Receive(&workspaceID))
				seen[workspaceID] = true
			}
		}).WithTimeout(2 * time.Second).Should(Succeed())
		Expect(seen).To(SatisfyAll(
			HaveKey(workspaceid.WorkspaceID("alpha")),
			HaveKey(workspaceid.WorkspaceID("beta")),
		), fmt.Sprintf("started workspaces = %#v, want alpha and beta", seen))

		close(releaseStart)
		wg.Wait()
		close(errs)
		for err := range errs {
			Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("Ensure returned error: %v", err))

		}

	})
})

var _ = ginkgo.Describe("federated workspace manager", func() {
	ginkgo.It("starts workspaced with ephemeral port", func() {
		supervisor := wikid.NewWorkspaceSupervisor(wikid.WorkspaceSupervisorOptions{})
		manager := newFederatedWorkspaceManager(
			leafwikiRuntimeConfig{},
			"daemon-token",
			"http://127.0.0.1:1",
			wikid.GlobalLayout(leafwikiTempDir()),
			supervisor,
		)
		manager.writeDescriptor = func(wikid.WorkspaceRecord, leafwikiRuntimeConfig, internalRuntimeRoleReady) error {
			return nil
		}
		processDone := make(chan error)
		manager.startRole = func(startup internalRuntimeRoleStartupConfig) (*internalRuntimeRoleProcess, internalRuntimeRoleReady, error) {
			Expect(startup.Runtime.Port).To(Equal("0"), fmt.Sprintf("workspaced startup port = %q, want 0", startup.Runtime.Port))

			return testRuntimeRoleProcess(projectdaemon.RoleWorkspaced, 101, processDone), internalRuntimeRoleReady{
				Role: projectdaemon.RoleWorkspaced,
				PID:  101,
				URL:  "http://127.0.0.1:49152",
			}, nil
		}

		status, err := manager.Ensure(context.Background(), wikid.WorkspaceRecord{
			ID:      "alpha",
			DataDir: leafwikiTempDir(),
			RootDir: leafwikiTempDir(),
		})
		Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("Ensure returned error: %v", err))
		Expect(status).To(SatisfyAll(
			HaveField("State", Equal(wikid.WorkspaceStateRunning)),
			HaveField("URL", Equal("http://127.0.0.1:49152")),
		), fmt.Sprintf("status = %#v, want running actual ready URL", status))

	})
})

var _ = ginkgo.Describe("federated workspace manager", func() {
	ginkgo.It("removes stale descriptors and restarts after crash", func() {
		supervisor := wikid.NewWorkspaceSupervisor(wikid.WorkspaceSupervisorOptions{
			MaxRestarts: 1,
			Backoff:     time.Millisecond,
		})
		manager := newFederatedWorkspaceManager(
			leafwikiRuntimeConfig{},
			"daemon-token",
			"http://127.0.0.1:1",
			wikid.GlobalLayout(leafwikiTempDir()),
			supervisor,
		)
		manager.writeDescriptor = func(wikid.WorkspaceRecord, leafwikiRuntimeConfig, internalRuntimeRoleReady) error {
			return nil
		}

		removed := make(chan string, 2)
		manager.removeDescriptor = func(path string) error {
			removed <- path
			return nil
		}
		restarted := make(chan struct{}, 1)
		restartedProcessDone := make(chan error)
		manager.startRole = func(internalRuntimeRoleStartupConfig) (*internalRuntimeRoleProcess, internalRuntimeRoleReady, error) {
			restarted <- struct{}{}
			return testRuntimeRoleProcess(projectdaemon.RoleWorkspaced, 202, restartedProcessDone), internalRuntimeRoleReady{
				Role: projectdaemon.RoleWorkspaced,
				PID:  202,
				URL:  "http://127.0.0.1:41002",
			}, nil
		}

		workspace := wikid.WorkspaceRecord{ID: "alpha", DataDir: leafwikiTempDir(), RootDir: leafwikiTempDir()}
		processDone := make(chan error, 1)
		process := testRuntimeRoleProcess(projectdaemon.RoleWorkspaced, 101, processDone)
		manager.mu.Lock()
		manager.processes[workspace.ID] = process
		manager.workspaces[workspace.ID] = workspace
		manager.descriptors[workspace.ID] = []string{"/tmp/alpha-local.json", "/tmp/alpha-runtime.json"}
		manager.mu.Unlock()
		supervisor.MarkReady(workspace.ID, 101, "http://127.0.0.1:41001")

		go manager.monitorWorkspaceProcess(workspace.ID, process)
		processDone <- errors.New("exit status 2")

		seenRemoved := map[string]bool{}
		var removedPath string
		Eventually(removed).WithTimeout(2 * time.Second).Should(Receive(&removedPath))
		seenRemoved[removedPath] = true
		Eventually(removed).WithTimeout(2 * time.Second).Should(Receive(&removedPath))
		seenRemoved[removedPath] = true
		Expect(seenRemoved).To(HaveKey("/tmp/alpha-local.json"))
		Expect(seenRemoved).To(HaveKey("/tmp/alpha-runtime.json"))

		Eventually(restarted).WithTimeout(2 * time.Second).Should(Receive())
		Eventually(func() wikid.WorkspaceStatus {
			return supervisor.Status(workspace.ID)
		}).WithTimeout(2 * time.Second).Should(SatisfyAll(
			HaveField("State", Equal(wikid.WorkspaceStateRunning)),
			HaveField("PID", Equal(202)),
		))
		manager.mu.Lock()
		Expect(manager.descriptors).NotTo(HaveKey(workspace.ID), fmt.Sprintf("stale descriptors still tracked after crash"))
		manager.mu.Unlock()

	})
})

func testRuntimeRoleProcess(role projectdaemon.RoleName, pid int, done <-chan error) *internalRuntimeRoleProcess {
	return &internalRuntimeRoleProcess{
		role:     role,
		pid:      pid,
		done:     done,
		waitDone: make(chan struct{}),
	}
}

var _ = ginkgo.Describe("internal runtime role readiness timeout", func() {
	ginkgo.It("is thirty seconds", func() {
		Expect(internalRuntimeRoleReadinessTimeout).To(Equal(30*time.Second), fmt.Sprintf("internal runtime role readiness timeout = %v, want 30s", internalRuntimeRoleReadinessTimeout))

	})
})

var _ = ginkgo.Describe("federated workspace ensure timeout", func() {
	ginkgo.It("is thirty seconds", func() {
		Expect(federatedWorkspaceEnsureTimeout).To(Equal(30*time.Second), fmt.Sprintf("federated workspace ensure timeout = %v, want 30s", federatedWorkspaceEnsureTimeout))

	})
})

var _ = ginkgo.Describe("internal runtime role process", func() {
	ginkgo.It("stops child when ready role mismatches", func() {
		pidPath := filepath.Join(leafwikiTempDir(), "wrong-role.pid")
		leafwikiSetenv("GO_WANT_LEAFWIKI_HELPER_PROCESS", "1")
		leafwikiSetenv("LEAFWIKI_TEST_RUNTIME_READY_WRONG_ROLE", "1")
		leafwikiSetenv("LEAFWIKI_TEST_RUNTIME_READY_WRONG_ROLE_PID_PATH", pidPath)

		_, _, err := startInternalRuntimeRoleProcess(internalRuntimeRoleStartupConfig{
			Role:        projectdaemon.RoleWorkspaced,
			Runtime:     leafwikiRuntimeConfig{},
			DaemonToken: "daemon-token",
		})
		Expect(err).To(HaveOccurred(), fmt.Sprintf("startInternalRuntimeRoleProcess unexpectedly accepted wrong ready role"))

		Expect(err).NotTo(MatchError(errRuntimeRoleInvalidPID))
		raw, readErr := os.ReadFile(pidPath)
		Expect(readErr).NotTo(HaveOccurred())

		pid, parseErr := strconv.Atoi(strings.TrimSpace(string(raw)))
		Expect(parseErr).NotTo(HaveOccurred())

		if processExists(pid) {
			_ = syscall.Kill(pid, syscall.SIGKILL)
		}
		Expect(processExists(pid)).To(BeFalse())

	})
})

var _ = ginkgo.Describe("leafwiki main process", func() {
	ginkgo.It("wikidfrontd runtime restarts workspaced and updates descriptor", func() {
		var ownerPID int
		ginkgo.DeferCleanup(func() {
			terminateProjectDaemonProcess(ownerPID)
		})
		baseDir := leafwikiTempDir()
		dataDir := filepath.Join(baseDir, "data")
		rootDir := filepath.Join(baseDir, "content")
		port := freeTCPPort()
		proc := startLeafwikiHelper([]string{
			"--disable-auth",
			"--data-dir", dataDir,
			"--root-dir", rootDir,
			"--host", "127.0.0.1",
			"--port", port,
			"--log-target", "stderr",
		}, map[string]string{})

		desc := waitForProjectDaemonDescriptor(dataDir)
		ownerPID = desc.PID
		waitForLeafwikiReady(proc, port)
		initial, err := runtimeRoleHealthResult(desc.Roles, projectdaemon.RoleWorkspaced)
		Expect(err).To(Succeed())
		Expect(initial.PID).To(BeNumerically(">", 0), fmt.Sprintf("initial workspaced role = %#v, want child PID", initial))

		Expect(syscall.Kill(initial.PID, syscall.SIGTERM)).To(Succeed())

		descriptorPath := projectdaemon.DescriptorPath(dataDir)
		var restarted projectdaemon.RoleHealth
		waitForRuntimeCondition(10*time.Second, func() bool {
			raw, err := os.ReadFile(descriptorPath)
			if err != nil {
				return false
			}
			var current projectdaemon.Descriptor
			if err := json.Unmarshal(raw, &current); err != nil {
				return false
			}
			var ok bool
			restarted, ok = findRoleHealth(current.Roles, projectdaemon.RoleWorkspaced)
			return ok && restarted.State == projectdaemon.RoleStateReady && restarted.PID > 0 && restarted.PID != initial.PID && processExists(restarted.PID)
		})

		proc.stop()

	})
})

var _ = ginkgo.Describe("leafwiki main process", func() {
	ginkgo.It("wikidfrontd runtime uses fresh wikid auth stores", func() {
		var ownerPID int
		ginkgo.DeferCleanup(func() {
			terminateProjectDaemonProcess(ownerPID)
		})
		baseDir := leafwikiTempDir()
		dataDir := filepath.Join(baseDir, "data")
		rootDir := filepath.Join(baseDir, "content")
		port := freeTCPPort()
		proc := startLeafwikiHelper([]string{
			"--jwt-secret", "auth-store-secret",
			"--admin-password", "admin-pass",
			"--allow-insecure",
			"--data-dir", dataDir,
			"--root-dir", rootDir,
			"--host", "127.0.0.1",
			"--port", port,
			"--log-target", "stderr",
		}, map[string]string{})

		layout := leafwikiHelperGlobalLayoutForDataDir(dataDir)
		globalDesc := waitForGlobalWikidDescriptor(dataDir)
		ownerPID = globalDesc.PID
		waitForLeafwikiReady(proc, port)

		authDir := filepath.Join(layout.WikidDir, "auth")
		for _, path := range []string{
			filepath.Join(authDir, "users.db"),
			filepath.Join(authDir, "sessions.db"),
			filepath.Join(authDir, "api_keys.db"),
		} {
			_, err := os.Stat(path)
			Expect(err).NotTo(HaveOccurred())
		}
		for _, name := range []string{"users.db", "sessions.db", "api_keys.db"} {
			_, err := os.Stat(filepath.Join(dataDir, name))
			Expect(err).To(MatchError(os.ErrNotExist))
		}

		client := &http.Client{Timeout: 2 * time.Second}
		httpReq, err := http.NewRequest(http.MethodPost, "http://127.0.0.1:"+port+"/api/auth/login", strings.NewReader(`{"identifier":"admin","password":"admin-pass"}`))
		Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("build login request: %v", err))

		httpReq.Header.Set("Content-Type", "application/json")
		resp, err := client.Do(httpReq)
		Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("POST /api/auth/login: %v", err))

		defer resp.Body.Close()
		Expect(resp).To(HaveHTTPStatus(http.StatusOK))
		proc.stop()

	})
})

var _ = ginkgo.Describe("leafwiki main process", func() {
	ginkgo.It("project daemon descriptor includes workspace sync flag", func() {
		var ownerPID int
		ginkgo.DeferCleanup(func() {
			terminateProjectDaemonProcess(ownerPID)
		})
		baseDir := leafwikiTempDir()
		dataDir := filepath.Join(baseDir, "data")
		rootDir := filepath.Join(baseDir, "content")
		port := freeTCPPort()
		proc := startLeafwikiHelper([]string{
			"--disable-auth",
			"--data-dir", dataDir,
			"--root-dir", rootDir,
			"--host", "127.0.0.1",
			"--port", port,
			"--log-target", "stderr",
		}, nil)
		desc := waitForProjectDaemonDescriptor(dataDir)
		ownerPID = desc.PID
		waitForLeafwikiReady(proc, port)
		Expect(desc.Config.EnableWorkspaceSync).To(BeTrue(), fmt.Sprintf("descriptor config EnableWorkspaceSync = false, want true"))

		proc.stop()

	})
})

var _ = ginkgo.Describe("leafwiki main process", func() {
	ginkgo.It("removed revision and workspace sync flags fail unknown", func() {
		baseDir := leafwikiTempDir()
		dataDir := filepath.Join(baseDir, "data")
		rootDir := filepath.Join(baseDir, "content")
		for _, removedFlag := range []string{"--enable-revision", "--enable-workspace-sync"} {
			func() {
				_ = removedFlag
				stdout, stderr, err := runLeafwikiHelperWithTimeout([]string{
					"--disable-auth",
					removedFlag,
					"--data-dir", dataDir,
					"--root-dir", rootDir,
					"--host", "127.0.0.1",
					"--port", freeTCPPort(),
					"--log-target", "stderr",
				}, nil, 5*time.Second)
				Expect(err).To(HaveOccurred(), fmt.Sprintf("startup with removed flag unexpectedly succeeded\nstdout:\n%s\nstderr:\n%s", stdout, stderr))
				Expect(err).NotTo(MatchError(context.DeadlineExceeded), fmt.Sprintf("startup with removed flag hung; expected immediate unknown flag error\nstdout:\n%s\nstderr:\n%s", stdout, stderr))
				Expect(stderr).To(ContainSubstring(strings.TrimLeft(removedFlag, "-")), fmt.Sprintf("stderr = %q, want unknown flag error for %s", stderr, removedFlag))

			}()
		}

	})
})

var _ = ginkgo.Describe("leafwiki main process", func() {
	ginkgo.It("config endpoint reports workspace sync flag", func() {
		var ownerPID int
		ginkgo.DeferCleanup(func() {
			terminateProjectDaemonProcess(ownerPID)
		})
		baseDir := leafwikiTempDir()
		dataDir := filepath.Join(baseDir, "data")
		rootDir := filepath.Join(baseDir, "content")
		port := freeTCPPort()
		proc := startLeafwikiHelper([]string{
			"--disable-auth",
			"--allow-insecure",
			"--data-dir", dataDir,
			"--root-dir", rootDir,
			"--host", "127.0.0.1",
			"--port", port,
			"--log-target", "stderr",
		}, nil)
		desc := waitForProjectDaemonDescriptor(dataDir)
		ownerPID = desc.PID
		waitForLeafwikiReady(proc, port)

		resp, err := http.Get("http://127.0.0.1:" + port + "/api/config")
		Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("GET /api/config: %v", err))

		defer resp.Body.Close()
		Expect(resp).To(HaveHTTPStatus(http.StatusOK))
		var config map[string]any
		Expect(json.NewDecoder(resp.Body).Decode(&config)).To(Succeed(), fmt.Sprintf("decode config: %v", err))
		Expect(config).To(HaveKeyWithValue("enableWorkspaceSync", true))

		proc.stop()

	})
})

var _ = ginkgo.Describe("leafwiki main process", func() {
	ginkgo.It("different root dir does not remove live project descriptor", func() {
		baseDir := leafwikiTempDir()
		dataDir := filepath.Join(baseDir, "data")
		ownerRootDir := filepath.Join(baseDir, "owner-content")
		requestedRootDir := filepath.Join(baseDir, "requested-content")
		port := freeTCPPort()
		owner := startLeafwikiHelper([]string{
			"--disable-auth",
			"--data-dir", dataDir,
			"--root-dir", ownerRootDir,
			"--host", "127.0.0.1",
			"--port", port,
			"--log-target", "stderr",
		}, map[string]string{})
		defer owner.stop()
		waitForLeafwikiReady(owner, port)
		descriptorPath := projectdaemon.DescriptorPath(dataDir)
		waitForFileContaining(descriptorPath, `"rootDir"`)

		stdout, stderr, err := runLeafwikiHelperWithTimeout([]string{
			"--disable-auth",
			"--data-dir", dataDir,
			"--root-dir", requestedRootDir,
			"--host", "127.0.0.1",
			"--port", port,
			"--log-target", "stderr",
		}, map[string]string{}, 12*time.Second)
		Expect(err).To(HaveOccurred(), fmt.Sprintf("different root-dir startup unexpectedly succeeded\nstdout:\n%s\nstderr:\n%s", stdout, stderr))
		Expect(stderr).To(SatisfyAny(
			ContainSubstring(projectdaemon.FormatConfigMismatch([]projectdaemon.Mismatch{{
				Field: "root-dir",
			}})),
			ContainSubstring(wikid.ErrWorkspaceDataDirAlreadyInUse.Error()),
		), fmt.Sprintf("stderr = %q, want root-dir config mismatch or workspace registry conflict", stderr))

		_, err = os.Stat(descriptorPath)
		Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("stdout:\n%s\nstderr:\n%s", stdout, stderr))
		waitForLeafwikiReady(owner, port)

	})
})

var _ = ginkgo.Describe("leafwiki main process", func() {
	ginkgo.It("first startup writes secure project daemon descriptor", func() {
		stdinReader, stdinWriter := io.Pipe()
		defer stdinWriter.Close()
		baseDir := leafwikiTempDir()
		dataDir := filepath.Join(baseDir, "data")
		rootDir := filepath.Join(baseDir, "content")
		port := freeTCPPort()
		proc := startLeafwikiHelperWithStdin([]string{
			"--mcp=stdio",
			"--disable-auth",
			"--data-dir", dataDir,
			"--root-dir", rootDir,
			"--host", "127.0.0.1",
			"--port", port,
			"--log-target", "stderr",
		}, nil, stdinReader)

		waitForLeafwikiReady(proc, port)
		descriptorPath := filepath.Join(dataDir, ".leafwiki", "project-daemon.json")
		waitForFileContaining(descriptorPath, `"controlToken"`)
		info, err := os.Stat(descriptorPath)
		Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("stat descriptor: %v", err))

		Expect(info.Mode().Perm()).To(Equal(os.FileMode(0o600)))
		raw := readFileString(descriptorPath)
		Expect(raw).To(SatisfyAll(
			ContainSubstring(filepath.Clean(dataDir)),
			ContainSubstring(filepath.Clean(rootDir)),
		), fmt.Sprintf("descriptor = %s, want canonical data/root dirs", raw))
		Expect(raw).NotTo(ContainSubstring("LEAFWIKI_MCP_API_KEY"), fmt.Sprintf("descriptor leaked API-key env name: %s", raw))
		Expect(raw).NotTo(ContainSubstring("lwk_"), fmt.Sprintf("descriptor leaked API-key material: %s", raw))

	})
})

var _ = ginkgo.Describe("leafwiki main process", func() {
	ginkgo.It("canonical path variants attach with default file logging", func() {
		if runtime.GOOS == "windows" {
			ginkgo.Skip(fmt.Sprint("symlink path canonicalization test is Unix-oriented"))
		}
		stdinReader, stdinWriter := io.Pipe()
		defer stdinWriter.Close()
		baseDir := leafwikiTempDir()
		dataDir := filepath.Join(baseDir, "data")
		rootDir := filepath.Join(baseDir, "content")
		Expect(os.MkdirAll(dataDir, 0o755)).To(Succeed())
		Expect(os.MkdirAll(rootDir, 0o755)).To(Succeed())
		dataLink := filepath.Join(baseDir, "data-link")
		rootLink := filepath.Join(baseDir, "content-link")
		Expect(os.Symlink(dataDir, dataLink)).To(Succeed())
		Expect(os.Symlink(rootDir, rootLink)).To(Succeed())
		port := freeTCPPort()
		first := startLeafwikiHelperWithStdin([]string{
			"--mcp=stdio",
			"--disable-auth",
			"--data-dir", dataLink,
			"--root-dir", rootLink,
			"--host", "127.0.0.1",
			"--port", port,
		}, nil, stdinReader)
		waitForLeafwikiReady(first, port)
		_, err := io.WriteString(stdinWriter, nativeStdioListToolsInput())
		Expect(err).NotTo(HaveOccurred())
		waitForFileContaining(first.stdoutPath, `"id":2`)

		stdout, stderr, err := runLeafwikiHelperWithTimeout([]string{
			"--mcp=stdio",
			"--disable-auth",
			"--data-dir", dataDir,
			"--root-dir", rootDir,
			"--host", "127.0.0.1",
			"--port", port,
		}, nil, 5*time.Second)
		Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("canonical path variant should attach, got %v\nstdout:\n%s\nstderr:\n%s", err, stdout, stderr))
		Expect(stderr).NotTo(ContainSubstring(projectdaemon.FormatConfigMismatch([]projectdaemon.Mismatch{{
			Field: "log-file",
		}})), fmt.Sprintf("stderr = %q, want no log-file config mismatch", stderr))

	})
})

var _ = ginkgo.Describe("leafwiki main process", func() {
	ginkgo.It("auth enabled project daemon descriptor omits bootstrap secret fingerprints", func() {
		baseDir := leafwikiTempDir()
		dataDir := filepath.Join(baseDir, "data")
		rootDir := filepath.Join(baseDir, "content")
		jwtSecret := "descriptor-jwt-secret"
		adminPassword := "descriptor-admin-password"
		port := freeTCPPort()
		proc := startLeafwikiHelper([]string{
			"--data-dir", dataDir,
			"--root-dir", rootDir,
			"--host", "127.0.0.1",
			"--port", port,
			"--jwt-secret", jwtSecret,
			"--admin-password", adminPassword,
			"--allow-insecure",
			"--log-target", "stderr",
		}, nil)

		waitForLeafwikiReady(proc, port)
		layout := leafwikiHelperGlobalLayoutForDataDir(dataDir)
		descriptorPath := projectdaemon.GlobalDescriptorPath(layout.RuntimeDir, projectdaemon.RoleWikid)
		waitForFileContaining(descriptorPath, `"configHash"`)
		raw := readFileString(descriptorPath)
		for _, unexpected := range []string{
			jwtSecret,
			adminPassword,
			sha256Hex(jwtSecret),
			sha256Hex(adminPassword),
			"jwtSecretHash",
			"adminPasswordHash",
		} {
			Expect(raw).NotTo(ContainSubstring(unexpected), fmt.Sprintf("descriptor leaked bootstrap secret material %q:\n%s", unexpected, raw))

		}

	})
})

var _ = ginkgo.Describe("leafwiki main process", func() {
	ginkgo.It("native STDIO close keeps owner alive until idle timeout", func() {
		stdinReader, stdinWriter := io.Pipe()
		baseDir := leafwikiTempDir()
		dataDir := filepath.Join(baseDir, "data")
		rootDir := filepath.Join(baseDir, "content")
		port := freeTCPPort()
		proc := startLeafwikiHelperWithStdin([]string{
			"--mcp=stdio",
			"--disable-auth",
			"--data-dir", dataDir,
			"--root-dir", rootDir,
			"--host", "127.0.0.1",
			"--port", port,
			"--log-target", "stderr",
		}, map[string]string{"LEAFWIKI_DAEMON_IDLE_TIMEOUT": "1s"}, stdinReader)

		waitForLeafwikiReady(proc, port)
		Expect(stdinWriter.Close()).To(Succeed())
		proc.waitForExit()
		waitForLeafwikiReady(proc, port)
		waitForLeafwikiUnavailable(port)
		waitForProjectLocksReusable(dataDir, rootDir, 15*time.Second)

	})
})

var _ = ginkgo.Describe("leafwiki main process", func() {
	ginkgo.It("crashed native STDIO frontend expires heartbeat and releases locks", func() {
		stdinReader, stdinWriter := io.Pipe()
		baseDir := leafwikiTempDir()
		dataDir := filepath.Join(baseDir, "data")
		rootDir := filepath.Join(baseDir, "content")
		port := freeTCPPort()
		proc := startLeafwikiHelperWithStdin([]string{
			"--mcp=stdio",
			"--disable-auth",
			"--data-dir", dataDir,
			"--root-dir", rootDir,
			"--host", "127.0.0.1",
			"--port", port,
			"--log-target", "stderr",
		}, map[string]string{"LEAFWIKI_DAEMON_IDLE_TIMEOUT": "1s"}, stdinReader)
		waitForLeafwikiReady(proc, port)

		proc.cancel()
		_ = stdinWriter.Close()
		_ = proc.cmd.Wait()
		proc.stopped = true

		waitForLeafwikiUnavailableWithin(port, 25*time.Second)
		waitForProjectLocksReusable(dataDir, rootDir, 15*time.Second)

	})
})

var _ = ginkgo.Describe("leafwiki main process", func() {
	ginkgo.It("native STDIO piped output exits before owner idle timeout", func() {
		baseDir := leafwikiTempDir()
		dataDir := filepath.Join(baseDir, "data")
		rootDir := filepath.Join(baseDir, "content")
		port := freeTCPPort()
		stdout, stderr, err := runLeafwikiHelperWithTimeout([]string{
			"--mcp=stdio",
			"--disable-auth",
			"--data-dir", dataDir,
			"--root-dir", rootDir,
			"--host", "127.0.0.1",
			"--port", port,
			"--log-target", "stderr",
		}, map[string]string{"LEAFWIKI_DAEMON_IDLE_TIMEOUT": "3s"}, 1500*time.Millisecond)
		Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("native STDIO frontend should exit before owner idle timeout, got %v\nstdout:\n%s\nstderr:\n%s", err, stdout, stderr))
		Expect(stdout).To(BeEmpty(), fmt.Sprintf("stdout = %q, want empty without MCP frames", stdout))

		waitForLeafwikiReadyWithDiagnostics(port, stdout, stderr)
		waitForLeafwikiUnavailable(port)

	})
})

var _ = ginkgo.Describe("leafwiki main process", func() {
	ginkgo.It("combined native stdioHTTP exposes HTTPMCP tool surface", func() {
		stdinReader, stdinWriter := io.Pipe()
		defer stdinWriter.Close()
		port := freeTCPPort()
		proc := startLeafwikiHelperWithStdin([]string{
			"--mcp=stdio,http",
			"--disable-auth",
			"--data-dir", filepath.Join(leafwikiTempDir(), "data"),
			"--root-dir", filepath.Join(leafwikiTempDir(), "content"),
			"--host", "127.0.0.1",
			"--port", port,
			"--log-target", "stderr",
		}, nil, stdinReader)

		waitForLeafwikiReady(proc, port)
		toolNames := listProcessHTTPMCPToolNames("http://127.0.0.1:" + port + "/mcp/workspaces/home")
		Expect(toolNames).To(matchToolNames(federatedRuntimeToolNames()))

		Expect(stdinWriter.Close()).To(Succeed())
		proc.waitForExit()
		Expect(readFileString(proc.stdoutPath)).To(BeEmpty())

	})
})

var _ = ginkgo.Describe("leafwiki main process", func() {
	ginkgo.It("repeated combined native stdioHTTP stderr logging attaches", func() {
		stdinReader, stdinWriter := io.Pipe()
		defer stdinWriter.Close()
		baseDir := leafwikiTempDir()
		dataDir := filepath.Join(baseDir, "data")
		rootDir := filepath.Join(baseDir, "content")
		port := freeTCPPort()
		first := startLeafwikiHelperWithStdin([]string{
			"--mcp=stdio,http",
			"--disable-auth",
			"--data-dir", dataDir,
			"--root-dir", rootDir,
			"--host", "127.0.0.1",
			"--port", port,
			"--log-target", "stderr",
		}, nil, stdinReader)
		waitForLeafwikiReady(first, port)
		_, err := io.WriteString(stdinWriter, nativeStdioListToolsInput())
		Expect(err).NotTo(HaveOccurred())
		waitForFileContaining(first.stdoutPath, `"id":2`)

		stdout, stderr, err := runLeafwikiHelperWithInputAndTimeout([]string{
			"--mcp=stdio,http",
			"--disable-auth",
			"--data-dir", dataDir,
			"--root-dir", rootDir,
			"--host", "127.0.0.1",
			"--port", port,
			"--log-target", "stderr",
		}, nil, nativeStdioListToolsInput(), 8*time.Second)
		Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("repeated combined STDIO+HTTP startup should attach, got %v\nstdout:\n%s\nstderr:\n%s", err, stdout, stderr))
		Expect(stdout).To(haveNativeStdioToolListResponse(2, "wiki_create_page"), fmt.Sprintf("stdout = %q, want tools/list response from repeated STDIO attach", stdout))
		Expect(stderr).NotTo(ContainSubstring(projectdaemon.FormatConfigMismatch(nil)), fmt.Sprintf("stderr = %q, want no logging config mismatch", stderr))

	})
})

var _ = ginkgo.Describe("leafwiki main process", func() {
	ginkgo.It("legacy MCP flags fail unknown", func() {
		for _, removedFlag := range []string{"--enable-mcp", "--mcp-stdio"} {
			func() {
				_ = removedFlag
				stdout, stderr, err := runLeafwikiHelperWithTimeout([]string{
					removedFlag,
					"--disable-auth",
					"--data-dir", filepath.Join(leafwikiTempDir(), "data"),
					"--root-dir", filepath.Join(leafwikiTempDir(), "content"),
					"--host", "127.0.0.1",
					"--port", freeTCPPort(),
					"--log-target", "stderr",
				}, nil, 5*time.Second)
				Expect(err).To(HaveOccurred(), fmt.Sprintf("startup with removed MCP flag unexpectedly succeeded\nstdout:\n%s\nstderr:\n%s", stdout, stderr))
				Expect(stderr).To(SatisfyAll(
					ContainSubstring(strings.TrimLeft(removedFlag, "-")),
				), fmt.Sprintf("stderr = %q, want unknown flag error for %s", stderr, removedFlag))

			}()
		}

	})
})

var _ = ginkgo.Describe("leafwiki main process", func() {
	ginkgo.It("native STDIO malformed JSON returns parse error and continues", func() {
		proc, stdin := startLeafwikiHelperWithStdinPipe([]string{
			"--mcp=stdio",
			"--disable-auth",
			"--data-dir", filepath.Join(leafwikiTempDir(), "data"),
			"--root-dir", filepath.Join(leafwikiTempDir(), "content"),
			"--host", "127.0.0.1",
			"--port", freeTCPPort(),
			"--log-target", "stderr",
		}, nil)

		_, err := io.WriteString(stdin, "not-json\n")
		Expect(err).NotTo(HaveOccurred())
		_, err = io.WriteString(stdin, `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-11-25","capabilities":{},"clientInfo":{"name":"test","version":"0"}}}`+"\n")
		Expect(err).NotTo(HaveOccurred())
		waitForFileContaining(proc.stdoutPath, `"id":1`)
		Expect(stdin.Close()).To(Succeed(), fmt.Sprintf("close stdin: %v", err))
		proc.waitForExit()

		stdout := readFileString(proc.stdoutPath)
		Expect(stdout).To(ContainSubstring(`"code":-32700`), fmt.Sprintf("stdout = %q, want JSON-RPC parse error", stdout))
		Expect(stdout).To(ContainSubstring(`"id":null`), fmt.Sprintf("stdout = %q, want parse error id null", stdout))
		Expect(stdout).To(ContainSubstring(`"id":1`), fmt.Sprintf("stdout = %q, want initialize response after malformed frame", stdout))

		Expect(readFileString(proc.stderrPath)).NotTo(ContainSubstring(leafwikiMCPStdioFailedLogMessage))

	})
})

var _ = ginkgo.Describe("leafwiki main process", func() {
	ginkgo.It("native STDIO rejects second process with config mismatch", func() {
		stdinReader, stdinWriter := io.Pipe()
		defer stdinWriter.Close()
		baseDir := leafwikiTempDir()
		dataDir := filepath.Join(baseDir, "data")
		rootDir := filepath.Join(baseDir, "content")
		rawSecret := "jwt_secret_should_not_leak"
		firstPort := freeTCPPort()
		first := startLeafwikiHelperWithStdin([]string{
			"--mcp=stdio",
			"--disable-auth",
			"--data-dir", dataDir,
			"--root-dir", rootDir,
			"--host", "127.0.0.1",
			"--port", firstPort,
			"--log-target", "stderr",
		}, nil, stdinReader)
		waitForLeafwikiReady(first, firstPort)
		_ = waitForProjectDaemonDescriptor(dataDir)

		stdout, stderr, err := runLeafwikiHelperWithTimeout([]string{
			"--mcp=stdio",
			"--disable-auth",
			"--data-dir", dataDir,
			"--root-dir", rootDir,
			"--host", "127.0.0.1",
			"--port", freeTCPPort(),
			"--markdown-link-root-prefix", "/docs",
			"--log-target", "stderr",
		}, map[string]string{"LEAFWIKI_JWT_SECRET": rawSecret}, 5*time.Second)
		Expect(err).To(HaveOccurred(), fmt.Sprintf("expected second process with config mismatch to exit non-zero"))
		Expect(err).NotTo(MatchError(context.DeadlineExceeded), fmt.Sprintf("second process did not exit; expected config mismatch\nstdout:\n%s\nstderr:\n%s", stdout, stderr))
		Expect(stdout).To(BeEmpty(), fmt.Sprintf("stdout = %q, want empty", stdout))
		Expect(stderr).To(ContainSubstring(projectdaemon.FormatConfigMismatch([]projectdaemon.Mismatch{{
			Field: "markdown-link-root-prefix",
		}})), fmt.Sprintf("stderr = %q, want markdown-link-root-prefix config mismatch", stderr))
		Expect(stderr).NotTo(ContainSubstring(rawSecret), fmt.Sprintf("stderr leaked raw secret: %q", stderr))

		Expect(stdinWriter.Close()).To(Succeed(), fmt.Sprintf("close stdin writer: %v", err))
		first.waitForExit()

	})
})

var _ = ginkgo.Describe("leafwiki main process", func() {
	ginkgo.It("native STDIO rejects second process with same root dir", func() {
		stdinReader, stdinWriter := io.Pipe()
		defer stdinWriter.Close()
		baseDir := leafwikiTempDir()
		rootDir := filepath.Join(baseDir, "content")
		firstPort := freeTCPPort()
		first := startLeafwikiHelperWithStdin([]string{
			"--mcp=stdio",
			"--disable-auth",
			"--data-dir", filepath.Join(baseDir, "data-a"),
			"--root-dir", rootDir,
			"--host", "127.0.0.1",
			"--port", firstPort,
			"--log-target", "stderr",
		}, nil, stdinReader)
		waitForLeafwikiReady(first, firstPort)
		_ = waitForProjectDaemonDescriptor(filepath.Join(baseDir, "data-a"))

		stdout, stderr, err := runLeafwikiHelperWithTimeout([]string{
			"--mcp=stdio",
			"--disable-auth",
			"--data-dir", filepath.Join(baseDir, "data-b"),
			"--root-dir", rootDir,
			"--host", "127.0.0.1",
			"--port", freeTCPPort(),
			"--log-target", "stderr",
		}, nil, 12*time.Second)
		Expect(err).To(HaveOccurred(), fmt.Sprintf("expected second process with same root dir to exit non-zero"))
		Expect(err).NotTo(MatchError(context.DeadlineExceeded), fmt.Sprintf("second process did not exit; expected root directory lock rejection\nstdout:\n%s\nstderr:\n%s", stdout, stderr))
		Expect(stdout).To(BeEmpty(), fmt.Sprintf("stdout = %q, want empty", stdout))
		Expect(stderr).To(ContainSubstring(leafwikiRootDirLockHeldMessage), fmt.Sprintf("stderr = %q, want root directory lock error", stderr))

		Expect(stdinWriter.Close()).To(Succeed(), fmt.Sprintf("close stdin writer: %v", err))
		first.waitForExit()

	})
})

var _ = ginkgo.Describe("concurrent project daemon startup", func() {
	ginkgo.It("attaches to winning owner after spawn error", func() {
		baseDir := leafwikiTempDir()
		dataDir := filepath.Join(baseDir, "data")
		rootDir := filepath.Join(baseDir, "content")
		Expect(os.MkdirAll(dataDir, 0o755)).To(Succeed())
		Expect(os.MkdirAll(rootDir, 0o755)).To(Succeed())
		canonicalData, canonicalRoot, err := projectdaemon.CanonicalizeProject(dataDir, rootDir)
		Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("canonicalize project: %v", err))

		ownerCfg := projectdaemon.Config{
			DataDir:           canonicalData,
			RootDir:           canonicalRoot,
			AuthDisabled:      true,
			PublicMCPEnabled:  false,
			Host:              "127.0.0.1",
			Port:              "8080",
			DaemonIdleTimeout: "10m0s",
		}
		hash, err := projectdaemon.ConfigHash(ownerCfg)
		Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("ConfigHash: %v", err))

		dataLock, err := locking.AcquireDataDirLock(canonicalData)
		Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("acquire fake owner data lock: %v", err))

		defer dataLock.Release()
		rootLock, err := locking.AcquireRootDirLock(canonicalRoot)
		Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("acquire fake owner root lock: %v", err))

		defer rootLock.Release()
		token := "control-token"
		control := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			if req.Header.Get(projectdaemon.ControlTokenHeader) != token {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
			if req.Method == http.MethodGet && req.URL.Path == "/health" {
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(projectdaemon.DaemonHealth{
					OK:            true,
					SchemaVersion: projectdaemon.DescriptorSchemaVersion,
					PID:           os.Getpid(),
					DataDir:       canonicalData,
					RootDir:       canonicalRoot,
					ConfigHash:    hash,
				})
				return
			}
			http.NotFound(w, req)
		}))
		ginkgo.DeferCleanup(control.Close)
		descriptorPath := projectdaemon.DescriptorPath(canonicalData)
		errorPath := filepath.Join(leafwikiTempDir(), "startup.err")
		Expect(os.WriteFile(errorPath, []byte("acquire data directory lock: data directory is already in use"), 0o600)).To(Succeed(), fmt.Sprintf("write startup error: %v", err))
		go func() {
			time.Sleep(2200 * time.Millisecond)
			_ = projectdaemon.WriteDescriptorAtomic(descriptorPath, &projectdaemon.Descriptor{
				SchemaVersion:    projectdaemon.DescriptorSchemaVersion,
				PID:              os.Getpid(),
				StartedAt:        time.Now().UTC(),
				DataDir:          canonicalData,
				RootDir:          canonicalRoot,
				PublicURL:        "http://127.0.0.1:8080",
				PublicMCPEnabled: false,
				ControlURL:       control.URL,
				ConfigHash:       hash,
				IdleTimeout:      "10m0s",
				ControlToken:     token,
				Config:           ownerCfg,
			})
		}()

		desc, err := waitForProjectDaemon(context.Background(), descriptorPath, errorPath, ownerCfg, mcpTransports{Stdio: true})
		Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("waitForProjectDaemon should attach to winning owner after startup error, got %v", err))
		Expect(desc.ControlURL).To(Equal(control.URL), fmt.Sprintf("attached descriptor control URL = %q, want %q", desc.ControlURL, control.URL))

	})
})

var _ = ginkgo.Describe("concurrent project daemon startup", func() {
	ginkgo.It("handles structured lock error", func() {
		baseDir := leafwikiTempDir()
		dataDir := filepath.Join(baseDir, "data")
		rootDir := filepath.Join(baseDir, "content")
		Expect(os.MkdirAll(dataDir, 0o755)).To(Succeed())
		Expect(os.MkdirAll(rootDir, 0o755)).To(Succeed())
		canonicalData, canonicalRoot, err := projectdaemon.CanonicalizeProject(dataDir, rootDir)
		Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("canonicalize project: %v", err))

		ownerCfg := projectdaemon.Config{
			DataDir:           canonicalData,
			RootDir:           canonicalRoot,
			AuthDisabled:      true,
			Host:              "127.0.0.1",
			Port:              "8080",
			DaemonIdleTimeout: "10m0s",
		}
		hash, err := projectdaemon.ConfigHash(ownerCfg)
		Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("ConfigHash: %v", err))

		dataLock, err := locking.AcquireDataDirLock(canonicalData)
		Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("acquire fake owner data lock: %v", err))

		defer dataLock.Release()
		rootLock, err := locking.AcquireRootDirLock(canonicalRoot)
		Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("acquire fake owner root lock: %v", err))

		defer rootLock.Release()
		token := "control-token"
		control := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			if req.Header.Get(projectdaemon.ControlTokenHeader) != token {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
			if req.Method == http.MethodGet && req.URL.Path == "/health" {
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(projectdaemon.DaemonHealth{
					OK:            true,
					SchemaVersion: projectdaemon.DescriptorSchemaVersion,
					PID:           os.Getpid(),
					DataDir:       canonicalData,
					RootDir:       canonicalRoot,
					ConfigHash:    hash,
				})
				return
			}
			http.NotFound(w, req)
		}))
		ginkgo.DeferCleanup(control.Close)
		descriptorPath := projectdaemon.DescriptorPath(canonicalData)
		errorPath := filepath.Join(leafwikiTempDir(), "startup.err")
		rawErr, err := json.Marshal(projectDaemonStartupError{
			Kind:            projectDaemonStartupErrorKindLock,
			RenderedMessage: "acquire data directory lock: data directory is already in use",
		})
		Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("marshal startup error: %v", err))

		Expect(os.WriteFile(errorPath, rawErr, 0o600)).To(Succeed(), fmt.Sprintf("write startup error: %v", err))
		go func() {
			time.Sleep(2200 * time.Millisecond)
			_ = projectdaemon.WriteDescriptorAtomic(descriptorPath, &projectdaemon.Descriptor{
				SchemaVersion:    projectdaemon.DescriptorSchemaVersion,
				PID:              os.Getpid(),
				StartedAt:        time.Now().UTC(),
				DataDir:          canonicalData,
				RootDir:          canonicalRoot,
				PublicURL:        "http://127.0.0.1:8080",
				PublicMCPEnabled: false,
				ControlURL:       control.URL,
				ConfigHash:       hash,
				IdleTimeout:      "10m0s",
				ControlToken:     token,
				Config:           ownerCfg,
			})
		}()

		desc, err := waitForProjectDaemon(context.Background(), descriptorPath, errorPath, ownerCfg, mcpTransports{})
		Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("waitForProjectDaemon should attach after structured lock startup error, got %v", err))
		Expect(desc.ControlURL).To(Equal(control.URL), fmt.Sprintf("attached descriptor control URL = %q, want %q", desc.ControlURL, control.URL))

	})
})

var _ = ginkgo.Describe("project daemon startup", func() {
	ginkgo.It("reports non lock startup error directly", func() {
		baseDir := leafwikiTempDir()
		dataDir := filepath.Join(baseDir, "data")
		rootDir := filepath.Join(baseDir, "content")
		Expect(os.MkdirAll(dataDir, 0o755)).To(Succeed())
		Expect(os.MkdirAll(rootDir, 0o755)).To(Succeed())
		canonicalData, canonicalRoot, err := projectdaemon.CanonicalizeProject(dataDir, rootDir)
		Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("canonicalize project: %v", err))

		ownerCfg := projectdaemon.Config{
			DataDir:           canonicalData,
			RootDir:           canonicalRoot,
			AuthDisabled:      true,
			Host:              "127.0.0.1",
			Port:              "8080",
			DaemonIdleTimeout: "10m0s",
		}
		errorPath := filepath.Join(leafwikiTempDir(), "startup.err")
		Expect(os.WriteFile(errorPath, []byte("start HTTP listener: listen tcp 127.0.0.1:8080: bind: address already in use"), 0o600)).To(Succeed(), fmt.Sprintf("write startup error: %v", err))
		ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
		defer cancel()

		_, err = waitForProjectDaemon(ctx, projectdaemon.DescriptorPath(canonicalData), errorPath, ownerCfg, mcpTransports{})
		Expect(err).To(HaveOccurred(), fmt.Sprintf("waitForProjectDaemon unexpectedly succeeded"))

		Expect(err).To(MatchError(errProjectDaemonStartupFailed))
		Expect(err).NotTo(MatchError(errProjectLockedNoAttachableDaemon))

	})
})

var _ = ginkgo.Describe("project daemon startup", func() {
	ginkgo.It("reports structured non lock startup error directly", func() {
		baseDir := leafwikiTempDir()
		dataDir := filepath.Join(baseDir, "data")
		rootDir := filepath.Join(baseDir, "content")
		Expect(os.MkdirAll(dataDir, 0o755)).To(Succeed())
		Expect(os.MkdirAll(rootDir, 0o755)).To(Succeed())
		canonicalData, canonicalRoot, err := projectdaemon.CanonicalizeProject(dataDir, rootDir)
		Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("canonicalize project: %v", err))

		ownerCfg := projectdaemon.Config{
			DataDir:           canonicalData,
			RootDir:           canonicalRoot,
			AuthDisabled:      true,
			Host:              "127.0.0.1",
			Port:              "8080",
			DaemonIdleTimeout: "10m0s",
		}
		errorPath := filepath.Join(leafwikiTempDir(), "startup.err")
		rawErr, err := json.Marshal(projectDaemonStartupError{
			Kind:            projectDaemonStartupErrorKindStartup,
			RenderedMessage: "start HTTP listener: listen tcp 127.0.0.1:8080: bind: address already in use",
		})
		Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("marshal startup error: %v", err))

		Expect(os.WriteFile(errorPath, rawErr, 0o600)).To(Succeed(), fmt.Sprintf("write startup error: %v", err))
		ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
		defer cancel()

		_, err = waitForProjectDaemon(ctx, projectdaemon.DescriptorPath(canonicalData), errorPath, ownerCfg, mcpTransports{})
		Expect(err).To(HaveOccurred(), fmt.Sprintf("waitForProjectDaemon unexpectedly succeeded"))

		Expect(err).To(MatchError(errProjectDaemonStartupFailed))
		Expect(err).NotTo(MatchError(errProjectLockedNoAttachableDaemon))

	})
})

var _ = ginkgo.Describe("STDIO attach daemon config comparison", func() {
	ginkgo.It("ignores owner logging settings", func() {
		owner := projectdaemon.Config{
			DataDir:           "/tmp/leafwiki-data",
			RootDir:           "/tmp/leafwiki-root",
			AuthDisabled:      true,
			Host:              "127.0.0.1",
			Port:              "8080",
			LogTarget:         "stderr",
			LogFile:           "",
			DisableRequestLog: false,
			DaemonIdleTimeout: "10m0s",
		}
		requested := owner
		requested.LogTarget = "file"
		requested.LogFile = "/tmp/leafwiki-data/.leafwiki/logs/leafwiki.log"
		requested.DisableRequestLog = true

		mismatches := compareProjectDaemonConfigForRequest(owner, requested, mcpTransports{Stdio: true})
		Expect(len(mismatches)).To(BeNumerically("<=", 0), fmt.Sprintf("mismatches = %#v, want STDIO-only attach to ignore owner logging settings", mismatches))

	})
})

var _ = ginkgo.Describe("plain server daemon config comparison", func() {
	ginkgo.It("preserves public MCP mismatch", func() {
		owner := projectdaemon.Config{
			DataDir:          "/tmp/leafwiki-data",
			RootDir:          "/tmp/leafwiki-root",
			AuthDisabled:     true,
			PublicMCPEnabled: true,
			Host:             "127.0.0.1",
			Port:             "8080",
		}
		requested := owner
		requested.PublicMCPEnabled = false

		mismatches := compareProjectDaemonConfigForRequest(owner, requested, mcpTransports{})
		Expect(mismatches).To(ConsistOf(HaveField("Field", Equal("public-mcp-enabled"))), fmt.Sprintf("mismatches = %#v, want public MCP mismatch for plain server startup", mismatches))

	})
})

var _ = ginkgo.Describe("daemon runtime configuration", func() {
	ginkgo.It("includes workspace sync", func() {
		baseDir := leafwikiTempDir()
		cfg := testRuntimeConfig(
			filepath.Join(baseDir, "data"),
			filepath.Join(baseDir, "content"),
			"8080",
			mcpTransports{},
			true,
		)
		cfg.EnableWorkspaceSync = true

		daemonCfg, err := daemonConfigForRuntime(cfg)
		Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("daemon config: %v", err))
		Expect(daemonCfg.EnableWorkspaceSync).To(BeTrue(), fmt.Sprintf("EnableWorkspaceSync = false, want true"))

	})
})

var _ = ginkgo.Describe("daemon owner runtime configuration", func() {
	ginkgo.It("for wikidfrontd forces workspace sync", func() {
		baseDir := leafwikiTempDir()
		cfg := testRuntimeConfig(
			filepath.Join(baseDir, "data"),
			filepath.Join(baseDir, "content"),
			"8080",
			mcpTransports{},
			true,
		)
		cfg.RuntimeStack = projectdaemon.RuntimeStackWikidFrontd
		cfg.EnableWorkspaceSync = false

		ownerCfg, err := daemonOwnerRuntimeConfig(cfg)
		Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("daemonOwnerRuntimeConfig failed: %v", err))

		daemonCfg, err := daemonConfigForRuntime(ownerCfg)
		Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("daemonConfigForRuntime failed: %v", err))
		Expect(ownerCfg.EnableWorkspaceSync).To(BeTrue(), fmt.Sprintf("owner workspace sync = false, want true"))
		Expect(daemonCfg.EnableWorkspaceSync).To(BeTrue(), fmt.Sprintf("daemon workspace sync = false, want true"))

	})
})

var _ = ginkgo.Describe("project daemon request comparison", func() {
	// Plantrace evidence: TestCompareProjectDaemonConfigForRequestCoversDaemonRelevantFields.
	ginkgo.It("reports daemon relevant descriptor fields", func() {
		owner := completeDaemonCompareConfig()
		tests := []struct {
			name  string
			field string
			mut   func(*projectdaemon.Config)
		}{
			{name: "data dir", field: "data-dir", mut: func(cfg *projectdaemon.Config) { cfg.DataDir = "/tmp/other-data" }},
			{name: "root dir", field: "root-dir", mut: func(cfg *projectdaemon.Config) { cfg.RootDir = "/tmp/other-root" }},
			{name: "auth mode", field: "auth-disabled", mut: func(cfg *projectdaemon.Config) { cfg.AuthDisabled = !cfg.AuthDisabled }},
			{name: "public MCP", field: "public-mcp-enabled", mut: func(cfg *projectdaemon.Config) { cfg.PublicMCPEnabled = !cfg.PublicMCPEnabled }},
			{name: "host", field: "host", mut: func(cfg *projectdaemon.Config) { cfg.Host = "127.0.0.2" }},
			{name: "port", field: "port", mut: func(cfg *projectdaemon.Config) { cfg.Port = "9090" }},
			{name: "base path", field: "base-path", mut: func(cfg *projectdaemon.Config) { cfg.BasePath = "/docs" }},
			{name: "markdown link root prefix", field: "markdown-link-root-prefix", mut: func(cfg *projectdaemon.Config) { cfg.MarkdownLinkRootPrefix = "/docs" }},
			{name: "public access", field: "public-access", mut: func(cfg *projectdaemon.Config) { cfg.PublicAccess = !cfg.PublicAccess }},
			{name: "allow insecure", field: "allow-insecure", mut: func(cfg *projectdaemon.Config) { cfg.AllowInsecure = !cfg.AllowInsecure }},
			{name: "access token timeout", field: "access-token-timeout", mut: func(cfg *projectdaemon.Config) { cfg.AccessTokenTimeout = "2h0m0s" }},
			{name: "refresh token timeout", field: "refresh-token-timeout", mut: func(cfg *projectdaemon.Config) { cfg.RefreshTokenTimeout = "720h0m0s" }},
			{name: "injected header hash", field: "inject-code-in-header-hash", mut: func(cfg *projectdaemon.Config) { cfg.InjectCodeInHeaderHash = "other-hash" }},
			{name: "custom stylesheet", field: "custom-stylesheet", mut: func(cfg *projectdaemon.Config) { cfg.CustomStylesheet = "/tmp/custom.css" }},
			{name: "log target", field: "log-target", mut: func(cfg *projectdaemon.Config) { cfg.LogTarget = "file" }},
			{name: "log file", field: "log-file", mut: func(cfg *projectdaemon.Config) { cfg.LogFile = "/tmp/leafwiki.log" }},
			{name: "hide metadata", field: "hide-link-metadata-section", mut: func(cfg *projectdaemon.Config) { cfg.HideLinkMetadataSection = !cfg.HideLinkMetadataSection }},
			{name: "upload size", field: "max-asset-upload-size-bytes", mut: func(cfg *projectdaemon.Config) { cfg.MaxAssetUploadSizeBytes = 99 }},
			{name: "link refactor", field: "enable-link-refactor", mut: func(cfg *projectdaemon.Config) { cfg.EnableLinkRefactor = !cfg.EnableLinkRefactor }},
			{name: "remote user enabled", field: "enable-http-remote-user", mut: func(cfg *projectdaemon.Config) { cfg.EnableHTTPRemoteUser = !cfg.EnableHTTPRemoteUser }},
			{name: "remote user header", field: "http-remote-user-header", mut: func(cfg *projectdaemon.Config) { cfg.HTTPRemoteUserHeader = "X-User" }},
			{name: "trusted proxies", field: "trusted-proxy-ips", mut: func(cfg *projectdaemon.Config) { cfg.TrustedProxyIPs = "127.0.0.1/32" }},
			{name: "remote user logout", field: "http-remote-user-logout-url", mut: func(cfg *projectdaemon.Config) { cfg.HTTPRemoteUserLogoutURL = "https://example.test/logout" }},
			{name: "request log", field: "disable-request-log", mut: func(cfg *projectdaemon.Config) { cfg.DisableRequestLog = !cfg.DisableRequestLog }},
			{name: "idle timeout", field: "daemon-idle-timeout", mut: func(cfg *projectdaemon.Config) { cfg.DaemonIdleTimeout = "1m0s" }},
		}

		for _, tt := range tests {
			func() {
				_ = tt.name
				requested := owner
				tt.mut(&requested)

				mismatches := compareProjectDaemonConfigForRequest(owner, requested, mcpTransports{HTTP: true})
				Expect(mismatches).To(ConsistOf(HaveField("Field", Equal(tt.field))), fmt.Sprintf("mismatches = %#v, want one %q mismatch", mismatches, tt.field))

			}()
		}

	})
})

var _ = ginkgo.Describe("STDIO attach daemon config comparison", func() {
	ginkgo.It("documents ignored fields", func() {
		owner := completeDaemonCompareConfig()
		requested := owner
		requested.PublicMCPEnabled = !owner.PublicMCPEnabled
		requested.Host = "0.0.0.0"
		requested.LogTarget = "file"
		requested.LogFile = "/tmp/leafwiki.log"
		requested.DisableRequestLog = !owner.DisableRequestLog

		mismatches := compareProjectDaemonConfigForRequest(owner, requested, mcpTransports{Stdio: true})
		Expect(len(mismatches)).To(BeNumerically("<=", 0), fmt.Sprintf("mismatches = %#v, want STDIO-only attach to inherit public MCP/logging/request-log settings", mismatches))

	})
})

var _ = ginkgo.Describe("project daemon descriptor comparison", func() {
	ginkgo.It("checks top level workspace ID", func() {
		requested := completeDaemonCompareConfig()
		requested.WorkspaceID = "beta"
		descriptorConfig := requested
		desc := &projectdaemon.Descriptor{
			Role:            projectdaemon.RoleWorkspaced,
			WorkspaceID:     "alpha",
			PrivateMCPURL:   "http://127.0.0.1:1/mcp",
			PrivateMCPToken: "token",
			Config:          descriptorConfig,
		}

		mismatches := compareProjectDaemonDescriptorForRequest(desc, requested, mcpTransports{Stdio: true})
		Expect(mismatches).To(ConsistOf(SatisfyAll(
			HaveField("Field", Equal("workspace-id")),
			HaveField("Want", Equal("alpha")),
			HaveField("Got", Equal("beta")),
		)), fmt.Sprintf("mismatches = %#v, want top-level workspace-id mismatch alpha -> beta", mismatches))

	})
})

var _ = ginkgo.Describe("daemon STDIO bridge HTTP client", func() {
	ginkgo.It("has no full request timeout", func() {
		client := daemonStdioBridgeHTTPClient(daemonStdioBridge{
			ControlToken: "control-token",
			APIKey:       "stdio-api-key",
		})
		Expect(client).To(haveDaemonStdioBridgeHTTPClient("control-token", "stdio-api-key"))

	})
})

var _ = ginkgo.Describe("daemon STDIO bridge HTTP client", func() {
	ginkgo.It("refreshes actor context before forwarding", func() {
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
				WorkspaceID: "workspace-a",
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
				WorkspaceID: "workspace-a",
			})
			Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("private MCP actor context invalid: %v", err))
			Expect(actor).To(SatisfyAll(
				HaveField("Subject", Equal("user:editor")),
				HaveField("Role", Equal(coreauth.RoleEditor)),
			), fmt.Sprintf("private MCP actor context = %#v, want refreshed editor actor", actor))

			w.WriteHeader(http.StatusNoContent)
		}))
		ginkgo.DeferCleanup(upstream.Close)

		client := daemonStdioBridgeHTTPClient(daemonStdioBridge{
			ControlToken:     "private-token",
			AuthControlURL:   control.URL,
			AuthControlToken: "control-token",
			WorkspaceID:      "workspace-a",
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
		Expect(err).To(MatchWikidPrivateEndpoint(http.StatusUnauthorized, ""))
		Expect(verifyCalls).To(Equal(2), fmt.Sprintf("revoked bridge verification calls = %d, want 2", verifyCalls))
		Expect(upstreamCalls).To(Equal(1), fmt.Sprintf("revoked bridge upstream calls = %d, want 1", upstreamCalls))

	})
})

var _ = ginkgo.Describe("daemon STDIO actor context", func() {
	ginkgo.It("preserves workspace grant denial", func() {
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
			WorkspaceID:      "workspace-b",
			APIKey:           "valid-but-ungranted-key",
		}
		_, err := transport.actorContext(httptest.NewRequest(http.MethodGet, "/mcp", nil))
		Expect(err).To(HaveOccurred(), fmt.Sprintf("actorContext returned nil, want workspace grant denial"))

		Expect(err).To(MatchWikidPrivateEndpoint(http.StatusForbidden, runtimeErrorCodeWorkspaceGrantDenied))
		Expect(err).To(MatchWikidPrivateEndpoint(http.StatusForbidden, runtimeErrorCodeWorkspaceGrantDenied))

	})
})

var _ = ginkgo.Describe("daemon heartbeat", func() {
	ginkgo.It("returns control errors", func() {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			http.Error(w, "session not found", http.StatusNotFound)
		}))
		ginkgo.DeferCleanup(server.Close)

		client := projectdaemon.NewClient(server.URL, "control-token")
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		err := runDaemonHeartbeat(ctx, client, "missing-session", 10*time.Millisecond)
		Expect(err).To(HaveOccurred(), fmt.Sprintf("runDaemonHeartbeat returned nil, want control error"))

		Expect(err).To(MatchProjectDaemonControlStatus(http.StatusNotFound))

	})
})

var _ = ginkgo.Describe("idle shutdown activity tracking", func() {
	ginkgo.It("ignores stale zero notification with active session", func() {
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
	ginkgo.It("cancel if no session after startup grace waits for first session", func() {
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
	ginkgo.It("cancel if no session after startup grace cancels when no session registers", func() {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		registry := projectdaemon.NewSessionRegistry(time.Second, nil)
		go cancelIfNoSessionAfterStartupGrace(ctx, cancel, registry, 10*time.Millisecond)

		Eventually(ctx.Done()).WithTimeout(250 * time.Millisecond).Should(BeClosed())

	})
})

var _ = ginkgo.Describe("project daemon activity count", func() {
	ginkgo.It("combines sessions and agent presence", func() {
		sessions := projectdaemon.NewSessionRegistry(time.Second, nil)
		presence := projectdaemon.NewAgentPresenceRegistry(time.Minute, nil)

		Expect(projectDaemonActivityCount(sessions, presence)).To(BeZero())
		handle, err := sessions.Register()
		Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("register session: %v", err))

		presence.Record(agenthooks.Event{
			Provider:      agenthooks.ProviderCodex,
			SessionIDHash: agentHookSessionHash(agenthooks.ProviderCodex, "codex"),
			EventName:     "SessionStart",
			SeenAt:        time.Now(),
		})
		Expect(projectDaemonActivityCount(sessions, presence)).To(Equal(2))
		sessions.Release(handle)
		Expect(projectDaemonActivityCount(sessions, presence)).To(Equal(1))

	})
})

var _ = ginkgo.Describe("leafwiki command behavior", func() {
	ginkgo.It("cancel if no activity after startup grace waits for first agent presence", func() {
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
			EventName:     "SessionStart",
			SeenAt:        time.Now(),
		})
		Consistently(ctx.Done()).WithTimeout(25 * time.Millisecond).ShouldNot(BeClosed())
		Eventually(done).WithTimeout(100 * time.Millisecond).Should(BeClosed())

	})
})

var _ = ginkgo.Describe("leafwiki command behavior", func() {
	ginkgo.It("cancel if no activity after startup grace ignores missing agent end", func() {
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
	ginkgo.It("omits session and bootstrap secrets", func() {
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
	ginkgo.It("native stdiosigterm releases data dir lock", func() {
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
	ginkgo.It("foreground server signal leaves detached owner until idle timeout", func() {
		if !supportsProcessGroupSignal() {
			ginkgo.Skip(fmt.Sprint("process-group signaling is not available on this platform"))
		}

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

		waitForLeafwikiReady(proc, port)
		waitForForegroundSignalHandler()
		Expect(signalLeafwikiProcessGroup(proc.cmd.Process)).To(Succeed())
		proc.waitForExit()
		waitForLeafwikiReady(proc, port)
		waitForLeafwikiUnavailableWithin(port, 15*time.Second)

	})
})

var _ = ginkgo.Describe("leafwiki main process", func() {
	ginkgo.It("file target startup failure also reaches stderr", func() {
		dataDir := filepath.Join(leafwikiTempDir(), "data")
		stdout, stderr, err := runLeafwikiHelper([]string{
			"--data-dir", dataDir,
			"--admin-password", "admin-password",
		}, nil)
		Expect(err).To(HaveOccurred(), fmt.Sprintf("expected missing JWT secret to exit non-zero"))
		Expect(stdout).To(BeEmpty(), fmt.Sprintf("stdout = %q, want empty", stdout))
		Expect(stderr).To(ContainSubstring(errAuthJWTSecretRequired.Error()), fmt.Sprintf("stderr = %q, want JWT secret error", stderr))

		Expect(readJSONLogEntries(filepath.Join(dataDir, ".leafwiki", "logs", "leafwiki.log"))).To(ContainElement(haveJSONLogEntry(errAuthJWTSecretRequired.Error())))

	})
})

var _ = ginkgo.Describe("leafwiki main process", func() {
	ginkgo.It("help stays on stdout", func() {
		stdout, stderr, err := runLeafwikiHelper([]string{"--help"}, nil)
		Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("help process error = %v, stderr=%q", err, stderr))

		for _, expected := range []string{"Usage:", "leafwiki daemon", "--log-target", "--log-file"} {
			Expect(stdout).To(ContainSubstring(expected), fmt.Sprintf("stdout = %q, want %q", stdout, expected))

		}
		Expect(stderr).NotTo(ContainSubstring(`"msg"`), fmt.Sprintf("stderr contains log output: %q", stderr))

	})
})

var _ = ginkgo.Describe("leafwiki main process", func() {
	ginkgo.It("daemon help stays on stdout", func() {
		stdout, stderr, err := runLeafwikiHelper([]string{"daemon", "--help"}, nil)
		Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("daemon help process error = %v, stderr=%q", err, stderr))

		for _, expected := range []string{"Usage:", "leafwiki daemon", "~/.leafwiki/leafwiki.yml"} {
			Expect(stdout).To(ContainSubstring(expected), fmt.Sprintf("stdout = %q, want %q", stdout, expected))

		}
		Expect(stderr).NotTo(ContainSubstring(`"msg"`), fmt.Sprintf("stderr contains log output: %q", stderr))

	})
})

var _ = ginkgo.Describe("leafwiki main process", func() {
	ginkgo.It("daemon requires default service config", func() {
		homeDir := leafwikiTempDir()
		stdout, stderr, err := runLeafwikiHelper([]string{"daemon"}, map[string]string{
			"HOME": homeDir,
		})
		Expect(err).To(HaveOccurred(), fmt.Sprintf("daemon without service config unexpectedly succeeded\nstdout:\n%s\nstderr:\n%s", stdout, stderr))
		Expect(stdout).To(BeEmpty(), fmt.Sprintf("stdout = %q, want empty", stdout))

		wantPath := filepath.Join(homeDir, ".leafwiki", "leafwiki.yml")
		Expect(stderr).To(SatisfyAll(
			ContainSubstring(localizedMessage(localization.MessageIDCLIErrorServiceConfigRequired)),
			ContainSubstring(wantPath),
		), fmt.Sprintf("stderr = %q, want required service config path %q", stderr, wantPath))

	})
})

var _ = ginkgo.Describe("leafwiki main process", func() {
	ginkgo.It("daemon rejects invalid default service config", func() {
		homeDir := leafwikiTempDir()
		serviceDir := filepath.Join(homeDir, ".leafwiki")
		Expect(os.MkdirAll(serviceDir, 0o755)).To(Succeed())
		configPath := filepath.Join(serviceDir, "leafwiki.yml")
		writeTestConfig(configPath, ":\n")

		stdout, stderr, err := runLeafwikiHelper([]string{"daemon"}, map[string]string{
			"HOME": homeDir,
		})
		Expect(err).To(HaveOccurred(), fmt.Sprintf("daemon with invalid service config unexpectedly succeeded\nstdout:\n%s\nstderr:\n%s", stdout, stderr))
		Expect(stdout).To(BeEmpty(), fmt.Sprintf("stdout = %q, want empty", stdout))
		Expect(stderr).To(SatisfyAll(
			ContainSubstring(localizedMessage(localization.MessageIDCLIErrorInvalidServiceConfigFile)),
			ContainSubstring(configPath),
		), fmt.Sprintf("stderr = %q, want invalid service config path %q", stderr, configPath))

	})
})

var _ = ginkgo.Describe("leafwiki main process", func() {
	ginkgo.It("daemon rejects internal service config keys", func() {
		homeDir := leafwikiTempDir()
		serviceDir := filepath.Join(homeDir, ".leafwiki")
		Expect(os.MkdirAll(serviceDir, 0o755)).To(Succeed())
		configPath := filepath.Join(serviceDir, "leafwiki.yml")
		writeTestConfig(configPath, "internal-project-daemon: /tmp/startup.json\n")

		stdout, stderr, err := runLeafwikiHelper([]string{"daemon"}, map[string]string{
			"HOME": homeDir,
		})
		Expect(err).To(HaveOccurred(), fmt.Sprintf("daemon with internal service config key unexpectedly succeeded\nstdout:\n%s\nstderr:\n%s", stdout, stderr))
		Expect(stdout).To(BeEmpty(), fmt.Sprintf("stdout = %q, want empty", stdout))
		Expect(stderr).To(SatisfyAll(
			ContainSubstring(localizedMessage(localization.MessageIDCLIErrorInvalidServiceConfigFile)),
			ContainSubstring("internal-project-daemon"),
		), fmt.Sprintf("stderr = %q, want unknown internal service config key error", stderr))

	})
})

var _ = ginkgo.Describe("leafwiki main process", func() {
	ginkgo.It("daemon rejects stdioMCP service config", func() {
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
		Expect(err).To(HaveOccurred(), fmt.Sprintf("daemon with stdio MCP service config unexpectedly succeeded\nstdout:\n%s\nstderr:\n%s", stdout, stderr))
		Expect(stdout).To(BeEmpty(), fmt.Sprintf("stdout = %q, want empty", stdout))
		Expect(stderr).To(SatisfyAll(
			ContainSubstring(localizedMessage(localization.MessageIDCLIErrorInvalidServiceConfigFile)),
			ContainSubstring("mcp"),
		), fmt.Sprintf("stderr = %q, want stdio MCP service config error", stderr))

	})
})

var _ = ginkgo.Describe("daemon service configuration", func() {
	ginkgo.It("uses defaults instead of environment", func() {
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
	ginkgo.It("daemon runs foreground runtime until signal", func() {
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
		Expect(processExists(proc.cmd.Process.Pid)).To(BeTrue(), fmt.Sprintf("daemon process exited before signal"))

		waitForForegroundSignalHandler()
		Expect(signalLeafwikiProcess(proc.cmd.Process)).To(Succeed(), fmt.Sprintf("send SIGTERM: %v", err))
		proc.waitForExit()
		waitForFileRemoved(projectDescriptorPath, 5*time.Second)
		waitForFileRemoved(globalDescriptorPath, 5*time.Second)
		waitForLeafwikiUnavailable(port)

	})
})

var _ = ginkgo.Describe("leafwiki main process", func() {
	ginkgo.It("daemon rejects runtime stack environment", func() {
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
		Expect(err).To(HaveOccurred(), fmt.Sprintf("daemon with removed runtime env unexpectedly succeeded\nstdout:\n%s\nstderr:\n%s", stdout, stderr))
		Expect(stderr).To(ContainSubstring((removedEnvironmentVariableError{Name: "LEAFWIKI_RUNTIME_STACK"}).Error()), fmt.Sprintf("stderr = %q, want removed runtime env error", stderr))

	})
})

var _ = ginkgo.Describe("leafwiki main process", func() {
	ginkgo.It("daemon runs from service example template", func() {
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
		Expect(desc.Config).To(SatisfyAll(
			HaveField("Port", Equal(port)),
			HaveField("PublicMCPEnabled", BeTrue()),
			HaveField("EnableWorkspaceSync", BeTrue()),
		))

		wantLogPath := filepath.Join(desc.DataDir, "logs", "leafwiki.log")
		Expect(desc.Config).To(SatisfyAll(
			HaveField("LogTarget", Equal("file")),
			HaveField("LogFile", Equal(wantLogPath)),
		))

		waitForFileContaining(wantLogPath, leafwikiStartupLogMessage)

		resp, err := http.Get("http://127.0.0.1:" + port + "/api/config")
		Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("GET /api/config: %v", err))

		defer resp.Body.Close()
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
	ginkgo.It("help flag after other flags stays on stdout", func() {
		stdout, stderr, err := runLeafwikiHelper([]string{
			"--log-target", "stderr",
			"--help",
		}, nil)
		Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("help process error = %v, stderr=%q", err, stderr))
		Expect(stdout).To(SatisfyAll(
			ContainSubstring(localizedMessage(localization.MessageIDCLIHelpUsage)),
			ContainSubstring("--log-target"),
		), fmt.Sprintf("stdout = %q, want help output", stdout))
		Expect(stderr).To(BeEmpty(), fmt.Sprintf("stderr = %q, want empty", stderr))

	})
})

var _ = ginkgo.Describe("leafwiki main process", func() {
	ginkgo.It("help flag value does not short circuit subcommand parsing", func() {
		stdout, stderr, err := runLeafwikiHelper([]string{
			"--admin-password", "help",
			"unknown-command",
		}, nil)
		Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("unknown command process error = %v, stderr=%q", err, stderr))
		Expect(stdout).To(ContainSubstring(localizedMessage(localization.MessageIDCLIStatusUnknownCommand, "unknown-command")), fmt.Sprintf("stdout = %q, want unknown command handling", stdout))

	})
})

var _ = ginkgo.Describe("leafwiki main process", func() {
	ginkgo.It("unknown command ignores dirty server only environment", func() {
		dataDir := filepath.Join(leafwikiTempDir(), "data")
		stdout, stderr, err := runLeafwikiHelper([]string{
			"--data-dir", dataDir,
			"unknown-command",
		}, map[string]string{
			"LEAFWIKI_MAX_ASSET_UPLOAD_SIZE": "bad",
		})
		Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("unknown command process error = %v, stderr=%q", err, stderr))
		Expect(stdout).To(ContainSubstring(localizedMessage(localization.MessageIDCLIStatusUnknownCommand, "unknown-command")), fmt.Sprintf("stdout = %q, want unknown command handling", stdout))
		Expect(stderr).To(BeEmpty(), fmt.Sprintf("stderr = %q, want empty", stderr))

	})
})

var _ = ginkgo.Describe("leafwiki main process", func() {
	ginkgo.It("unknown command rejects MCPstdio environment", func() {
		dataDir := filepath.Join(leafwikiTempDir(), "data")
		stdout, stderr, err := runLeafwikiHelper([]string{
			"--data-dir", dataDir,
			"unknown-command",
		}, map[string]string{
			"LEAFWIKI_MCP_STDIO": "true",
		})
		Expect(err).To(HaveOccurred(), fmt.Sprintf("unknown command with removed env unexpectedly succeeded\nstdout:\n%s\nstderr:\n%s", stdout, stderr))
		Expect(stderr).To(ContainSubstring((removedEnvironmentVariableError{Name: "LEAFWIKI_MCP_STDIO"}).Error()), fmt.Sprintf("stderr = %q, want removed env error", stderr))

	})
})

var _ = ginkgo.Describe("leafwiki main process", func() {
	ginkgo.It("reset admin password ignores dirty server only environment", func() {
		dataDir := filepath.Join(leafwikiTempDir(), "data")
		initWikidAdminUser(dataDir)

		stdout, stderr, err := runLeafwikiHelper([]string{
			"--data-dir", dataDir,
			"reset-admin-password",
		}, map[string]string{
			"LEAFWIKI_MAX_ASSET_UPLOAD_SIZE": "bad",
		})
		Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("reset-admin-password process error = %v, stderr=%q", err, stderr))
		Expect(stdout).To(ContainSubstring(localizedMessage(localization.MessageIDCLIStatusAdminPasswordReset)), fmt.Sprintf("stdout = %q, want reset output", stdout))

	})
})

var _ = ginkgo.Describe("leafwiki main process", func() {
	ginkgo.It("unknown command stays user facing and does not create log file", func() {
		dataDir := filepath.Join(leafwikiTempDir(), "data")
		stdout, stderr, err := runLeafwikiHelper([]string{
			"--data-dir", dataDir,
			"unknown-command",
		}, nil)
		Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("unknown command process error = %v, stderr=%q", err, stderr))
		Expect(stdout).To(SatisfyAll(
			ContainSubstring(localizedMessage(localization.MessageIDCLIStatusUnknownCommand, "unknown-command")),
			ContainSubstring(localizedMessage(localization.MessageIDCLIHelpUsage)),
		), fmt.Sprintf("stdout = %q, want unknown command and usage", stdout))
		Expect(stderr).NotTo(ContainSubstring(leafwikiStartupLogMessage), fmt.Sprintf("stderr contains server log: %q", stderr))

		_, err = os.Stat(filepath.Join(dataDir, ".leafwiki", "logs", "leafwiki.log"))
		Expect(err).To(MatchError(os.ErrNotExist))

	})
})

var _ = ginkgo.Describe("leafwiki main process", func() {
	ginkgo.It("reset admin password keeps credentials on stdout only", func() {
		dataDir := filepath.Join(leafwikiTempDir(), "data")
		initWikidAdminUser(dataDir)

		stdout, stderr, err := runLeafwikiHelper([]string{
			"--data-dir", dataDir,
			"reset-admin-password",
		}, map[string]string{})
		Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("reset-admin-password process error = %v, stderr=%q", err, stderr))
		Expect(stdout).To(SatisfyAll(
			ContainSubstring(localizedMessage(localization.MessageIDCLIStatusAdminPasswordReset)),
			ContainSubstring(localizedMessage(localization.MessageIDCLIStatusAdminPasswordValue, coreauth.DefaultAdminUsername, "")),
		), fmt.Sprintf("stdout = %q, want reset credentials", stdout))
		Expect(stdout).To(SatisfyAll(
			Not(ContainSubstring(`"msg"`)),
			Not(ContainSubstring(leafwikiStartupLogMessage)),
		), fmt.Sprintf("stdout contains log output: %q", stdout))

		_, err = os.Stat(filepath.Join(dataDir, ".leafwiki", "logs", "leafwiki.log"))
		Expect(err).To(MatchError(os.ErrNotExist))

	})
})

var _ = ginkgo.Describe("leafwiki main process", func() {
	ginkgo.It("wikidfrontd reset admin password uses wikid auth store", func() {
		dataDir := filepath.Join(leafwikiTempDir(), "data")
		initWikidAdminUser(dataDir)

		stdout, stderr, err := runLeafwikiHelper([]string{
			"--data-dir", dataDir,
			"reset-admin-password",
		}, map[string]string{})
		Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("reset-admin-password process error = %v, stderr=%q", err, stderr))
		Expect(stdout).To(SatisfyAll(
			ContainSubstring(localizedMessage(localization.MessageIDCLIStatusAdminPasswordReset)),
			ContainSubstring(localizedMessage(localization.MessageIDCLIStatusAdminPasswordValue, coreauth.DefaultAdminUsername, "")),
		), fmt.Sprintf("stdout = %q, want reset credentials", stdout))

		_, err = os.Stat(filepath.Join(dataDir, "users.db"))
		Expect(err).To(MatchError(os.ErrNotExist))

	})
})

var _ = ginkgo.Describe("leafwiki main process", func() {
	ginkgo.It("explicit stdout target writes server logs to stdout", func() {
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
		Expect(stdout).To(SatisfyAll(
			ContainSubstring(leafwikiStartupLogMessage),
			ContainSubstring(leafwikiHTTPRequestLogMessage),
		), fmt.Sprintf("stdout = %q, want server and request logs", stdout))

		_, err := os.Stat(filepath.Join(dataDir, ".leafwiki", "logs", "leafwiki.log"))
		Expect(err).To(MatchError(os.ErrNotExist))

	})
})

var _ = ginkgo.Describe("leafwiki main process", func() {
	ginkgo.It("disable request log suppresses process request log", func() {
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
		Expect(stderr).NotTo(ContainSubstring(leafwikiHTTPRequestLogMessage), fmt.Sprintf("stderr = %q, want request log suppressed", stderr))

		stdout := readFileString(proc.stdoutPath)
		Expect(stdout).To(BeEmpty(), fmt.Sprintf("stdout = %q, want empty for stderr target", stdout))

	})
})

var _ = ginkgo.Describe("workspace resolution", func() {
	ginkgo.It("defaults root dir under data dir", func() {
		dataDir := filepath.Join(leafwikiTempDir(), "data")

		workspace := resolveWorkspaceForArgs([]string{"--data-dir=" + dataDir})
		Expect(workspace).To(SatisfyAll(
			HaveField("DataDir", Equal(dataDir)),
			HaveField("RootDir", Equal(filepath.Join(dataDir, "root"))),
		))

	})
})

var _ = ginkgo.Describe("workspace resolution", func() {
	ginkgo.It("env root dir overrides default", func() {
		dataDir := filepath.Join(leafwikiTempDir(), "data")
		rootDir := filepath.Join(leafwikiTempDir(), "content")
		leafwikiSetenv("LEAFWIKI_ROOT_DIR", rootDir)

		workspace := resolveWorkspaceForArgs([]string{"--data-dir=" + dataDir})
		Expect(workspace.RootDir).To(Equal(rootDir), fmt.Sprintf("RootDir = %q, want env root %q", workspace.RootDir, rootDir))

	})
})

var _ = ginkgo.Describe("workspace resolution", func() {
	ginkgo.It("CLI root dir overrides env", func() {
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
	ginkgo.It("normalizes paths", func() {
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
	ginkgo.It("rejects same data and root dir", func() {
		dir := leafwikiTempDir()

		err := validateWorkspaceDirs(dir, filepath.Clean(filepath.Join(dir, ".")))
		Expect(err).To(HaveOccurred(), fmt.Sprintf("expected RootDir == DataDir to be rejected"))

		Expect(err).To(MatchError(wiki.ErrWorkspaceRootDirEqualsDataDir))

	})
})

var _ = ginkgo.Describe("workspace validation", func() {
	ginkgo.It("rejects root dir containing data dir", func() {
		rootDir := filepath.Join(leafwikiTempDir(), "wiki")
		dataDir := filepath.Join(rootDir, "data")

		err := validateWorkspaceDirs(dataDir, rootDir)
		Expect(err).To(HaveOccurred(), fmt.Sprintf("expected RootDir containing DataDir to be rejected"))

		Expect(err).To(MatchError(wiki.ErrWorkspaceRootDirContainsDataDir))

	})
})

var _ = ginkgo.Describe("startup workspace resolution", func() {
	ginkgo.It("skips workspace validation for reset admin password", func() {
		dir := leafwikiTempDir()
		leafwikiSetenv("LEAFWIKI_ROOT_DIR", dir)

		fs := flag.NewFlagSet("leafwiki", flag.ContinueOnError)
		var errOut bytes.Buffer
		fs.SetOutput(&errOut)
		flags := registerFlags(fs)
		Expect(fs.Parse([]string{"--data-dir=" + dir, "reset-admin-password"})).To(Succeed(), errOut.String())
		visited := map[string]bool{}
		fs.Visit(func(f *flag.Flag) { visited[f.Name] = true })

		_, shouldStart, err := resolveStartupWorkspace(flags, visited, fs.Args())
		Expect(err).NotTo(HaveOccurred())
		Expect(shouldStart).To(BeFalse())

	})
})

var _ = ginkgo.Describe("MCP transport resolution", func() {
	ginkgo.It("default env CLI and selector", func() {
		func() {
			_ = "default none"
			got, err := resolveMCPTransportsForArgs(nil)
			Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("resolveMCPTransports: %v", err))
			Expect(got).To(SatisfyAll(
				HaveField("HTTP", BeFalse()),
				HaveField("Stdio", BeFalse()),
			), fmt.Sprintf("default transports = %#v, want none", got))

		}()

		func() {
			_ = "env enables http"
			leafwikiSetenv("LEAFWIKI_MCP", "http")
			got, err := resolveMCPTransportsForArgs(nil)
			Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("resolveMCPTransports: %v", err))
			Expect(got).To(SatisfyAll(
				HaveField("HTTP", BeTrue()),
				HaveField("Stdio", BeFalse()),
			), fmt.Sprintf("env transports = %#v, want http only", got))

		}()

		func() {
			_ = "cli overrides env"
			leafwikiSetenv("LEAFWIKI_MCP", "http")
			got, err := resolveMCPTransportsForArgs([]string{"--mcp=stdio"})
			Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("resolveMCPTransports: %v", err))
			Expect(got).To(SatisfyAll(
				HaveField("HTTP", BeFalse()),
				HaveField("Stdio", BeTrue()),
			), fmt.Sprintf("CLI transports = %#v, want stdio only", got))

		}()

		func() {
			_ = "combined orderings"
			for _, raw := range []string{"--mcp=stdio,http", "--mcp=http,stdio"} {
				got, err := resolveMCPTransportsForArgs([]string{raw})
				Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("resolveMCPTransports(%s): %v", raw, err))
				Expect(got).To(SatisfyAll(
					HaveField("HTTP", BeTrue()),
					HaveField("Stdio", BeTrue()),
				), fmt.Sprintf("%s transports = %#v, want both", raw, got))

			}

		}()

		func() {
			_ = "selector ignores removed legacy envs after env validation"
			got, err := resolveMCPTransportsForArgs([]string{"--mcp=none"})
			Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("resolveMCPTransports: %v", err))
			Expect(got).To(SatisfyAll(
				HaveField("HTTP", BeFalse()),
				HaveField("Stdio", BeFalse()),
			), fmt.Sprintf("selector transports = %#v, want none", got))

		}()

	})
})

var _ = ginkgo.Describe("MCP transport parsing", func() {
	ginkgo.It("rejects invalid values", func() {
		tests := []struct {
			name   string
			raw    string
			reason runtimeconfig.MCPTransportErrorReason
		}{
			{name: "unknown", raw: "websocket", reason: runtimeconfig.MCPTransportErrorReasonInvalid},
			{name: "none combined", raw: "none,stdio", reason: runtimeconfig.MCPTransportErrorReasonNoneMixed},
			{name: "duplicate", raw: "stdio,stdio", reason: runtimeconfig.MCPTransportErrorReasonDuplicate},
			{name: "empty part", raw: "stdio,", reason: runtimeconfig.MCPTransportErrorReasonInvalid},
		}
		for _, tt := range tests {
			func() {
				_ = tt.name
				_, err := parseMCPTransports(tt.raw)
				Expect(err).To(MatchMCPTransportError(tt.reason))

			}()
		}

	})
})

var _ = ginkgo.Describe("MCP transport validation", func() {
	ginkgo.It("accepts compatible transport settings and rejects invalid STDIO authentication combinations", func() {
		tests := []struct {
			name      string
			opts      mcpTransportOptions
			messageID cliMessageID
		}{
			{
				name: "HTTP allows non-loopback web host",
				opts: mcpTransportOptions{
					Transports: mcpTransports{HTTP: true},
					Host:       "0.0.0.0",
					LogTarget:  leaflogging.TargetStderr,
				},
			},
			{
				name: "STDIO allows non-loopback web host",
				opts: mcpTransportOptions{
					Transports:  mcpTransports{Stdio: true},
					DisableAuth: true,
					Host:        "0.0.0.0",
					LogTarget:   leaflogging.TargetStderr,
				},
			},
			{
				name: "STDIO rejects stdout logging",
				opts: mcpTransportOptions{
					Transports:  mcpTransports{Stdio: true},
					DisableAuth: true,
					Host:        "127.0.0.1",
					LogTarget:   leaflogging.TargetStdout,
				},
				messageID: cliMessageID(localization.MessageIDCLIErrorStdoutReservedForMCPStdio),
			},
			{
				name: "STDIO auth enabled requires key",
				opts: mcpTransportOptions{
					Transports: mcpTransports{Stdio: true},
					Host:       "127.0.0.1",
					LogTarget:  leaflogging.TargetStderr,
				},
				messageID: cliMessageID(localization.MessageIDCLIErrorStdioAuthIdentityRequired),
			},
			{
				name: "STDIO disabled auth rejects key",
				opts: mcpTransportOptions{
					Transports:  mcpTransports{Stdio: true},
					DisableAuth: true,
					APIKey:      "lwk_fake",
					Host:        "127.0.0.1",
					LogTarget:   leaflogging.TargetStderr,
				},
				messageID: cliMessageID(localization.MessageIDCLIErrorStdioAuthAPIKeyConflict),
			},
			{
				name: "HTTP ignores API key",
				opts: mcpTransportOptions{
					Transports: mcpTransports{HTTP: true},
					APIKey:     "lwk_invalid",
					Host:       "127.0.0.1",
					LogTarget:  leaflogging.TargetStderr,
				},
			},
		}

		for _, tt := range tests {
			func() {
				_ = tt.name
				err := validateMCPTransportOptions(tt.opts)
				if tt.messageID == "" {
					Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("validateMCPTransportOptions() error = %v, want nil", err))

					return
				}
				Expect(err).To(MatchCLIRenderedMessageError(tt.messageID))

			}()
		}

	})
})

var _ = ginkgo.Describe("HTTP router option building", func() {
	ginkgo.It("propagates MCP enablement", func() {
		opts := buildHTTPRouterOptions(httpRouterOptionsInput{
			publicAccess:        true,
			authDisabled:        true,
			enableMCP:           true,
			host:                "127.0.0.1",
			mcpToolListPageSize: 7,
		})
		Expect(opts).To(SatisfyAll(
			HaveField("MCPEnabled", BeTrue()),
			HaveField("MCPToolListPageSize", Equal(7)),
			HaveField("MCPBindHost", Equal("127.0.0.1")),
		))

	})
})

var _ = ginkgo.Describe("listen address building", func() {
	ginkgo.It("handles i pv6 loopback", func() {
		got := buildListenAddress("::1", "8080")
		Expect(got).To(Equal("[::1]:8080"), fmt.Sprintf("buildListenAddress(::1, 8080) = %q, want %q", got, "[::1]:8080"))

	})
})

var _ = ginkgo.Describe("CLI flag registration", func() {
	ginkgo.It("accepts single dash long flags", func() {
		fs := flag.NewFlagSet("leafwiki", flag.ContinueOnError)
		var errOut bytes.Buffer
		fs.SetOutput(&errOut)
		flags := registerFlags(fs)

		err := fs.Parse([]string{
			"-jwt-secret=test-secret",
			"-admin-password=test-password",
			"-allow-insecure=true",
		})
		Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("expected single-dash long flags to parse, got %v (%s)", err, errOut.String()))

		Expect(*flags.jwtSecret).To(Equal("test-secret"))
		Expect(*flags.adminPassword).To(Equal("test-password"))
		Expect(*flags.allowInsecure).To(BeTrue(), fmt.Sprintf("expected allow-insecure to be true"))

	})
})

var _ = ginkgo.Describe("HTTP remote-user configuration", func() {
	ginkgo.It("requires trusted proxy IPs only when remote-user auth is enabled", func() {
		tests := []struct {
			name            string
			enabled         bool
			trustedProxyIPs string
			wantErr         bool
		}{
			{"disabled, no IPs", false, "", false},
			{"disabled, with IPs", false, "127.0.0.1", false},
			{"enabled, with IPs", true, "127.0.0.1", false},
			{"enabled, multiple IPs", true, "127.0.0.1,172.18.0.0/16", false},
			{"enabled, no IPs", true, "", true},
			{"enabled, whitespace only", true, "   ", true},
			{"enabled, commas only", true, ",,,", true},
			{"enabled, commas and whitespace", true, " , , ", true},
		}
		for _, tc := range tests {
			func() {
				_ = tc.name
				err := validateHTTPRemoteUserConfig(tc.enabled, tc.trustedProxyIPs)
				Expect(err != nil).To(Equal(tc.wantErr), fmt.Sprintf("validateHTTPRemoteUserConfig(%v, %q) error = %v, wantErr %v", tc.enabled, tc.trustedProxyIPs, err, tc.wantErr))

			}()
		}

	})
})

var _ = ginkgo.Describe("CLI flag registration", func() {
	ginkgo.It("accepts double dash long flags", func() {
		fs := flag.NewFlagSet("leafwiki", flag.ContinueOnError)
		var errOut bytes.Buffer
		fs.SetOutput(&errOut)
		flags := registerFlags(fs)

		err := fs.Parse([]string{
			"--jwt-secret=test-secret",
			"--admin-password=test-password",
			"--allow-insecure=true",
		})
		Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("expected double-dash long flags to parse, got %v (%s)", err, errOut.String()))

		Expect(*flags.jwtSecret).To(Equal("test-secret"))
		Expect(*flags.adminPassword).To(Equal("test-password"))
		Expect(*flags.allowInsecure).To(BeTrue(), fmt.Sprintf("expected allow-insecure to be true"))

	})
})

var _ = ginkgo.Describe("CLI flag registration", func() {
	ginkgo.It("accepts root dir flag", func() {
		fs := flag.NewFlagSet("leafwiki", flag.ContinueOnError)
		var errOut bytes.Buffer
		fs.SetOutput(&errOut)
		flags := registerFlags(fs)

		err := fs.Parse([]string{"--root-dir=/tmp/leafwiki-content"})
		Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("expected root-dir flag to parse, got %v (%s)", err, errOut.String()))
		Expect(flags.rootDir).NotTo(BeNil(), fmt.Sprintf("expected root-dir to be parsed, got %#v", flags.rootDir))
		Expect(*flags.rootDir).To(Equal("/tmp/leafwiki-content"), fmt.Sprintf("expected root-dir to be parsed, got %#v", flags.rootDir))

	})
})

var _ = ginkgo.Describe("CLI flag registration", func() {
	ginkgo.It("accepts logging flags", func() {
		fs := flag.NewFlagSet("leafwiki", flag.ContinueOnError)
		var errOut bytes.Buffer
		fs.SetOutput(&errOut)
		flags := registerFlags(fs)

		err := fs.Parse([]string{
			"--log-target=stderr",
			"--log-file=logs/custom.log",
		})
		Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("expected logging flags to parse, got %v (%s)", err, errOut.String()))
		Expect(flags).To(WithTransform(func(flags *cliFlags) map[string]string {
			values := map[string]string{}
			if flags.logTarget != nil {
				values["log-target"] = *flags.logTarget
			}
			if flags.logFile != nil {
				values["log-file"] = *flags.logFile
			}
			return values
		}, SatisfyAll(
			HaveKeyWithValue("log-target", "stderr"),
			HaveKeyWithValue("log-file", "logs/custom.log"),
		)), fmt.Sprintf("expected logging flags to parse, got target=%#v file=%#v", flags.logTarget, flags.logFile))

	})
})

func resolveMCPTransportsForArgs(args []string) (mcpTransports, error) {
	ginkgo.GinkgoHelper()

	fs := flag.NewFlagSet("leafwiki", flag.ContinueOnError)
	var errOut bytes.Buffer
	fs.SetOutput(&errOut)
	flags := registerFlags(fs)
	Expect(fs.Parse(args)).To(Succeed(), errOut.String())
	visited := map[string]bool{}
	fs.Visit(func(f *flag.Flag) { visited[f.Name] = true })
	return resolveMCPTransports(flags, visited)
}

func parseConfigFlagsForArgs(args []string) (*cliFlags, map[string]bool, []string) {
	ginkgo.GinkgoHelper()

	flags, visited, rest, err := parseConfigFlagsForArgsAllowError(args)
	Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("applyYAMLConfigFile: %v", err))

	return flags, visited, rest
}

func parseConfigFlagsForArgsAllowError(args []string) (*cliFlags, map[string]bool, []string, error) {
	ginkgo.GinkgoHelper()

	fs := flag.NewFlagSet("leafwiki", flag.ContinueOnError)
	var errOut bytes.Buffer
	fs.SetOutput(&errOut)
	flags := registerFlags(fs)
	Expect(fs.Parse(args)).To(Succeed(), errOut.String())
	visited := map[string]bool{}
	fs.Visit(func(f *flag.Flag) { visited[f.Name] = true })
	if visited["config"] {
		if err := validateConfigModeArgs(fs.Args()); err != nil {
			return flags, visited, fs.Args(), err
		}
		if err := applyYAMLConfigFile(fs, flags, visited); err != nil {
			return flags, visited, fs.Args(), err
		}
	}
	return flags, visited, fs.Args(), nil
}

func writeTestConfig(path string, body string) {
	ginkgo.GinkgoHelper()

	Expect(os.WriteFile(path, []byte(body), 0o600)).To(Succeed())
}

func serviceExampleConfigPath() string {
	ginkgo.GinkgoHelper()
	path := filepath.Join("..", "..", "config", "leafwiki.service.example.yml")
	_, err := os.Stat(path)
	Expect(err).NotTo(HaveOccurred())
	return path
}

func serviceExampleConfigKeys() map[string]struct{} {
	ginkgo.GinkgoHelper()
	raw, err := os.ReadFile(serviceExampleConfigPath())
	Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("read service config example: %v", err))

	keys := map[string]struct{}{}
	for _, line := range strings.Split(string(raw), "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "#") {
			trimmed = strings.TrimSpace(strings.TrimPrefix(trimmed, "#"))
		}
		key, _, ok := strings.Cut(trimmed, ":")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		if isServiceExampleConfigKey(key) {
			keys[key] = struct{}{}
		}
	}
	return keys
}

func isServiceExampleConfigKey(key string) bool {
	if key == "" {
		return false
	}
	for _, r := range key {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' {
			continue
		}
		return false
	}
	return true
}

func resolveWorkspaceForArgs(args []string) wiki.Workspace {
	ginkgo.GinkgoHelper()

	fs := flag.NewFlagSet("leafwiki", flag.ContinueOnError)
	var errOut bytes.Buffer
	fs.SetOutput(&errOut)
	flags := registerFlags(fs)
	Expect(fs.Parse(args)).To(Succeed(), errOut.String())
	visited := map[string]bool{}
	fs.Visit(func(f *flag.Flag) { visited[f.Name] = true })
	workspace, err := resolveWorkspace(flags, visited)
	Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("resolveWorkspace: %v", err))

	return workspace
}

func resolveLoggingConfigForArgs(args []string) leaflogging.Config {
	ginkgo.GinkgoHelper()

	cfg, err := resolveLoggingConfigForArgsAllowError(args)
	Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("resolveLoggingConfig: %v", err))

	return cfg
}

func resolveLoggingConfigForArgsAllowError(args []string) (leaflogging.Config, error) {
	ginkgo.GinkgoHelper()

	fs := flag.NewFlagSet("leafwiki", flag.ContinueOnError)
	var errOut bytes.Buffer
	fs.SetOutput(&errOut)
	flags := registerFlags(fs)
	Expect(fs.Parse(args)).To(Succeed(), errOut.String())
	visited := map[string]bool{}
	fs.Visit(func(f *flag.Flag) { visited[f.Name] = true })
	workspace, err := resolveWorkspace(flags, visited)
	Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("resolveWorkspace: %v", err))

	return resolveLoggingConfig(flags, visited, workspace.DataDir)
}

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
			Expect(err).To(matchLeafwikiHelperStopError(p.ready), fmt.Sprintf("wait leafwiki helper after graceful signal\nstdout:\n%s\nstderr:\n%s", readFileString(p.stdoutPath), readFileString(p.stderrPath)))
			return
		case <-time.After(2 * time.Second):
		}
	}
	p.cancel()
	Eventually(done).WithTimeout(5*time.Second).Should(Receive(matchLeafwikiHelperStopError(p.ready)), fmt.Sprintf("leafwiki helper did not stop\nstdout:\n%s\nstderr:\n%s", readFileString(p.stdoutPath), readFileString(p.stderrPath)))
}

func matchLeafwikiHelperStopError(ready bool) types.GomegaMatcher {
	return Satisfy(func(err error) bool {
		if err == nil {
			return true
		}
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) && ready && exitErr.ProcessState.ExitCode() == -1 {
			return true
		}
		if errors.Is(err, context.Canceled) {
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

func haveNativeStdioToolListResponse(id int, toolName string) types.GomegaMatcher {
	ginkgo.GinkgoHelper()
	return WithTransform(func(stdout string) []string {
		return nativeStdioToolNames(stdout, id)
	}, ContainElement(toolName))
}

func haveNativeStdioTextResponse(id int, matcher types.GomegaMatcher) types.GomegaMatcher {
	ginkgo.GinkgoHelper()
	return WithTransform(func(stdout string) string {
		return nativeStdioResultText(stdout, id)
	}, matcher)
}

func nativeStdioToolNames(stdout string, id int) []string {
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
	names := make([]string, 0, len(result.Tools))
	for _, tool := range result.Tools {
		names = append(names, tool.Name)
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

	defer session.Close()

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

func testRuntimeConfigWithHealthyControlDescriptor(recordHandler http.HandlerFunc) (leafwikiRuntimeConfig, func()) {
	ginkgo.GinkgoHelper()

	baseDir := leafwikiTempDir()
	leafwikiSetenv("HOME", filepath.Join(baseDir, "home"))
	dataDir := filepath.Join(baseDir, "data")
	rootDir := filepath.Join(baseDir, "content")
	Expect(os.MkdirAll(dataDir, 0o755)).To(Succeed())
	Expect(os.MkdirAll(rootDir, 0o755)).To(Succeed())
	cfg := testRuntimeConfig(dataDir, rootDir, freeTCPPort(), mcpTransports{}, true)
	ownerCfg, err := daemonRequestConfigForRuntime(cfg)
	Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("daemon request config: %v", err))

	configHash, err := projectdaemon.ConfigHash(ownerCfg)
	Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("config hash: %v", err))

	dataLock, err := locking.AcquireDataDirLock(ownerCfg.DataDir)
	Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("acquire data lock: %v", err))

	rootLock, err := locking.AcquireRootDirLock(ownerCfg.RootDir)
	if err != nil {
		_ = dataLock.Release()
	}
	Expect(err).NotTo(HaveOccurred())

	token := "control-token"
	pid := os.Getpid()
	control := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.Header.Get(projectdaemon.ControlTokenHeader) != token {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		if req.Method == http.MethodGet && req.URL.Path == "/health" {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(projectdaemon.DaemonHealth{
				OK:            true,
				SchemaVersion: projectdaemon.DescriptorSchemaVersion,
				PID:           pid,
				DataDir:       ownerCfg.DataDir,
				RootDir:       ownerCfg.RootDir,
				ConfigHash:    configHash,
			})
			return
		}
		if req.Method == http.MethodPost && req.URL.Path == "/agent-presence/events" {
			recordHandler(w, req)
			return
		}
		http.NotFound(w, req)
	}))
	err = projectdaemon.WriteDescriptorAtomic(projectdaemon.DescriptorPath(ownerCfg.DataDir), &projectdaemon.Descriptor{
		SchemaVersion:    projectdaemon.DescriptorSchemaVersion,
		PID:              pid,
		StartedAt:        time.Now().UTC(),
		DataDir:          ownerCfg.DataDir,
		RootDir:          ownerCfg.RootDir,
		PublicURL:        "http://127.0.0.1:" + ownerCfg.Port,
		PublicMCPEnabled: ownerCfg.PublicMCPEnabled,
		ControlURL:       control.URL,
		ConfigHash:       configHash,
		IdleTimeout:      ownerCfg.DaemonIdleTimeout,
		ControlToken:     token,
		Config:           ownerCfg,
	})
	if err != nil {
		control.Close()
		_ = rootLock.Release()
		_ = dataLock.Release()
	}
	Expect(err).NotTo(HaveOccurred())

	cleanup := func() {
		control.Close()
		_ = rootLock.Release()
		_ = dataLock.Release()
	}
	return cfg, cleanup
}

func readJSONLogEntries(path string) []map[string]any {
	ginkgo.GinkgoHelper()

	entries := []map[string]any{}
	for _, line := range strings.Split(strings.TrimSpace(readFileString(path)), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var entry map[string]any
		Expect(json.Unmarshal([]byte(line), &entry)).To(Succeed(), line)
		entries = append(entries, entry)
	}
	return entries
}

func haveJSONLogEntry(msg string, matchers ...types.GomegaMatcher) types.GomegaMatcher {
	entryMatchers := []types.GomegaMatcher{
		HaveKey("time"),
		HaveKey("level"),
		HaveKey("source"),
		HaveKeyWithValue("msg", msg),
	}
	entryMatchers = append(entryMatchers, matchers...)
	return SatisfyAll(entryMatchers...)
}

func initAdminUser(dataDir string) {
	ginkgo.GinkgoHelper()

	Expect(os.MkdirAll(dataDir, 0o755)).To(Succeed())
	store, err := coreauth.NewUserStore(dataDir)
	Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("create user store: %v", err))

	defer func() {
		Expect(store.Close()).To(Succeed(), fmt.Sprintf("close user store: %v", err))
	}()

	service := coreauth.NewUserService(store)
	Expect(service.InitDefaultAdmin("old-password")).To(Succeed(), fmt.Sprintf("init admin user: %v", err))
}

func initWikidAdminUser(dataDir string) {
	ginkgo.GinkgoHelper()

	paths := wikid.AuthStoragePaths(dataDir)
	Expect(os.MkdirAll(paths.AuthDir, 0o755)).To(Succeed())
	store, err := coreauth.NewUserStore(paths.AuthDir)
	Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("create wikid user store: %v", err))

	defer func() {
		Expect(store.Close()).To(Succeed(), fmt.Sprintf("close wikid user store: %v", err))
	}()
	service := coreauth.NewUserService(store)
	Expect(service.InitDefaultAdmin("old-password")).To(Succeed(), fmt.Sprintf("init wikid admin user: %v", err))
}

type testMCPAPIKey struct {
	Secret string
	UserID coreauth.UserID
}

func createMCPAPIKey(dataDir string) string {
	ginkgo.GinkgoHelper()

	return createMCPAPIKeyInStorageDirWithUser(dataDir).Secret
}

func createWikidMCPAPIKey(dataDir string) string {
	ginkgo.GinkgoHelper()

	return createWikidMCPAPIKeyWithUser(dataDir).Secret
}

func createWikidMCPAPIKeyWithUser(dataDir string) testMCPAPIKey {
	ginkgo.GinkgoHelper()

	layout := leafwikiHelperGlobalLayoutForDataDir(dataDir)
	return createMCPAPIKeyInStorageDirWithUser(wikid.AuthStoragePaths(layout.HomeDir).AuthDir)
}

func createMCPAPIKeyInStorageDir(storageDir string) string {
	ginkgo.GinkgoHelper()

	return createMCPAPIKeyInStorageDirWithUser(storageDir).Secret
}

func createMCPAPIKeyInStorageDirWithUser(storageDir string) testMCPAPIKey {
	ginkgo.GinkgoHelper()

	Expect(os.MkdirAll(storageDir, 0o755)).To(Succeed())
	userStore, err := coreauth.NewUserStore(storageDir)
	Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("create user store: %v", err))

	defer func() {
		Expect(userStore.Close()).To(Succeed(), fmt.Sprintf("close user store: %v", err))
	}()
	userService := coreauth.NewUserService(userStore)
	user, err := userService.CreateUser("editor", "editor@example.com", "password", coreauth.RoleEditor)
	Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("create API-key user: %v", err))

	apiKeyStore, err := coreauth.NewAPIKeyStore(storageDir)
	Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("create api key store: %v", err))

	apiKeyService := coreauth.NewAPIKeyService(apiKeyStore, userService)
	defer func() {
		Expect(apiKeyService.Close()).To(Succeed(), fmt.Sprintf("close api key service: %v", err))
	}()
	userID := newFixtureUserID(user.ID)
	created, err := apiKeyService.CreateAPIKey(userID, "Main process STDIO", userID)
	Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("create API key: %v", err))

	return testMCPAPIKey{Secret: created.Secret, UserID: newFixtureUserID(user.ID)}
}

func grantWikidWorkspaceAccessForDirs(dataDir string, rootDir string, userID coreauth.UserID, role wikid.GrantRole) {
	ginkgo.GinkgoHelper()

	layout := leafwikiHelperGlobalLayoutForDataDir(dataDir)
	registry := wikid.NewRegistryService(wikid.NewRegistryStore(layout.DBPath), layout)
	requestCfg := projectdaemon.Config{DataDir: dataDir, RootDir: rootDir}
	workspace, err := registry.RegisterWorkspace(wikid.RegisterWorkspaceRequest{
		DisplayName: federatedWorkspaceDisplayName(requestCfg),
		DataDir:     dataDir,
		RootDir:     rootDir,
	})
	Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("register workspace for grant: %v", err))

	grants := wikid.NewGrantStore(layout.DBPath)
	Expect(grants.Upsert(wikid.Grant{Subject: "user:" + userID.String(), WorkspaceID: workspace.ID, Role: role})).To(Succeed(), fmt.Sprintf("grant workspace access: %v", err))
}

func MatchCLIRenderedMessageError(messageID cliMessageID) types.GomegaMatcher {
	ginkgo.GinkgoHelper()

	return WithTransform(func(err error) (cliRenderedMessageError, error) {
		var cliErr cliRenderedMessageError
		if !errors.As(err, &cliErr) {
			return cliRenderedMessageError{}, fmt.Errorf("expected CLI rendered message error, got %T", err)
		}
		return cliErr, nil
	}, gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
		"MessageID": Equal(messageID),
		"Message":   Equal(localization.English.Render(string(messageID), "").Message),
	}))
}

func MatchProjectDaemonWorkspaceIDMismatch(want, got workspaceid.WorkspaceID) types.GomegaMatcher {
	ginkgo.GinkgoHelper()

	return WithTransform(func(err error) (*projectdaemon.ConfigMismatchError, error) {
		var mismatchErr *projectdaemon.ConfigMismatchError
		if !errors.As(err, &mismatchErr) {
			return nil, fmt.Errorf("expected project daemon config mismatch, got %T", err)
		}
		return mismatchErr, nil
	}, HaveField("Mismatches", ContainElement(Satisfy(func(mismatch projectdaemon.Mismatch) bool {
		wantID, wantErr := workspaceid.ParseWorkspaceID(mismatch.Want)
		gotID, gotErr := workspaceid.ParseWorkspaceID(mismatch.Got)
		return mismatch.Field == "workspace-id" &&
			wantErr == nil &&
			gotErr == nil &&
			wantID == want &&
			gotID == got
	}))))
}

func expectRuntimeConfigUsageReason(err error, reason runtimeconfig.ConfigUsageReason) {
	ginkgo.GinkgoHelper()

	Expect(err).To(MatchRuntimeConfigUsageReason(reason))
}

func expectRuntimeConfigFileReason(err error, reason runtimeconfig.ConfigFileErrorReason) {
	ginkgo.GinkgoHelper()

	Expect(err).To(MatchRuntimeConfigFileError(reason, ""))
}

func MatchRuntimeConfigUsageReason(reason runtimeconfig.ConfigUsageReason) types.GomegaMatcher {
	ginkgo.GinkgoHelper()

	return WithTransform(func(err error) (runtimeconfig.ConfigUsageError, error) {
		var usage runtimeconfig.ConfigUsageError
		if !errors.As(err, &usage) {
			return runtimeconfig.ConfigUsageError{}, fmt.Errorf("expected runtime config usage error, got %T", err)
		}
		return usage, nil
	}, gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
		"Reason": Equal(reason),
	}))
}

func MatchRuntimeConfigFileError(reason runtimeconfig.ConfigFileErrorReason, key string) types.GomegaMatcher {
	ginkgo.GinkgoHelper()

	fields := gstruct.Fields{
		"Reason": Equal(reason),
	}
	if key != "" {
		fields["Key"] = Equal(key)
	}
	return WithTransform(func(err error) (runtimeconfig.ConfigFileError, error) {
		var configErr runtimeconfig.ConfigFileError
		if !errors.As(err, &configErr) {
			return runtimeconfig.ConfigFileError{}, fmt.Errorf("expected runtime config file error, got %T", err)
		}
		return configErr, nil
	}, gstruct.MatchFields(gstruct.IgnoreExtras, fields))
}

func MatchMCPTransportError(reason runtimeconfig.MCPTransportErrorReason) types.GomegaMatcher {
	ginkgo.GinkgoHelper()

	return WithTransform(func(err error) (runtimeconfig.MCPTransportError, error) {
		var transportErr runtimeconfig.MCPTransportError
		if !errors.As(err, &transportErr) {
			return runtimeconfig.MCPTransportError{}, fmt.Errorf("expected MCP transport error, got %T", err)
		}
		return transportErr, nil
	}, gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
		"Reason": Equal(reason),
	}))
}

func expectRuntimeConfigFileError(err error, reason runtimeconfig.ConfigFileErrorReason, key string) {
	ginkgo.GinkgoHelper()

	Expect(err).To(MatchRuntimeConfigFileError(reason, key))
}

func removedStartupFlagName(arg string) string {
	name := strings.TrimLeft(arg, "-")
	name, _, _ = strings.Cut(name, "=")
	return name
}

func expectAgentHookConfigRejection(args []string) {
	ginkgo.GinkgoHelper()

	normalizedArgs := normalizeAgentHookRawArgs(args)
	rawUsageErr := runtimeconfig.ValidateRawConfigFlagUsage(normalizedArgs)
	if rawUsageErr != nil {
		expectRuntimeConfigUsageReason(rawUsageErr, runtimeconfig.ConfigUsageReasonConfigPathRequired)
		return
	}

	_, _, _, err := parseConfigFlagsForArgsAllowError(normalizedArgs)
	expectRuntimeConfigFileReason(err, runtimeconfig.ConfigFileErrorReasonRead)
}

var _ = ginkgo.Describe("visible legacy subcases", func() {
	ginkgo.DescribeTable("rejects removed startup flags",
		func(arg string) {
			flagName := removedStartupFlagName(arg)
			fs := flag.NewFlagSet("leafwiki-test", flag.ContinueOnError)
			var errOut bytes.Buffer
			fs.SetOutput(&errOut)
			registerFlags(fs)

			err := fs.Parse([]string{arg})
			Expect(err).To(HaveOccurred())
			Expect(fs.Lookup(flagName)).To(BeNil())
			Expect(errOut.String()).To(ContainSubstring(flagName))
		},
		ginkgo.Entry("--enable-revision", "--enable-revision"),
		ginkgo.Entry("--enable-workspace-sync", "--enable-workspace-sync"),
		ginkgo.Entry("--enable-mcp", "--enable-mcp"),
		ginkgo.Entry("--mcp-stdio", "--mcp-stdio"),
		ginkgo.Entry("--max-revision-history=0", "--max-revision-history=0"),
	)

	ginkgo.DescribeTable("rejects removed runtime env",
		func(name string) {
			leafwikiSetenv(name, "")

			err := rejectRemovedLeafWikiEnv()
			Expect(err).To(HaveOccurred())
			Expect(err).To(MatchError(removedEnvironmentVariableError{Name: name}))
		},
		ginkgo.Entry("LEAFWIKI_RUNTIME_STACK", "LEAFWIKI_RUNTIME_STACK"),
		ginkgo.Entry("LEAFWIKI_ENABLE_REVISION", "LEAFWIKI_ENABLE_REVISION"),
		ginkgo.Entry("LEAFWIKI_ENABLE_WORKSPACE_SYNC", "LEAFWIKI_ENABLE_WORKSPACE_SYNC"),
		ginkgo.Entry("LEAFWIKI_MAX_REVISION_HISTORY", "LEAFWIKI_MAX_REVISION_HISTORY"),
		ginkgo.Entry("LEAFWIKI_ENABLE_MCP", "LEAFWIKI_ENABLE_MCP"),
		ginkgo.Entry("LEAFWIKI_MCP_STDIO", "LEAFWIKI_MCP_STDIO"),
	)

	ginkgo.DescribeTable("caps grant by current user role",
		func(userRole wikid.GrantRole, grant wikid.GrantRole, want wikid.GrantRole) {
			Expect(effectiveWorkspaceGrantRole(userRole, grant)).To(Equal(want))
		},
		ginkgo.Entry("viewer grant remains viewer", wikid.GrantRoleEditor, wikid.GrantRoleViewer, wikid.GrantRoleViewer),
		ginkgo.Entry("editor grant capped by downgraded viewer", wikid.GrantRoleViewer, wikid.GrantRoleEditor, wikid.GrantRoleViewer),
		ginkgo.Entry("admin user keeps editor grant", wikid.GrantRoleAdmin, wikid.GrantRoleEditor, wikid.GrantRoleEditor),
		ginkgo.Entry("unknown user role denies effective grant", wikid.GrantRole(""), wikid.GrantRoleEditor, wikid.GrantRole("")),
	)

	type configFileErrorCase struct {
		yaml   string
		reason runtimeconfig.ConfigFileErrorReason
		key    string
	}

	ginkgo.DescribeTable("rejects invalid keys and values",
		func(tc configFileErrorCase) {
			configPath := filepath.Join(leafwikiTempDir(), "leafwiki.yml")
			writeTestConfig(configPath, tc.yaml)

			_, _, _, err := parseConfigFlagsForArgsAllowError([]string{"--config", configPath})

			Expect(err).To(MatchRuntimeConfigFileError(tc.reason, tc.key))
		},
		ginkgo.Entry("unknown key", configFileErrorCase{yaml: "unknown-option: true\n", reason: runtimeconfig.ConfigFileErrorReasonUnknownKey, key: "unknown-option"}),
		ginkgo.Entry("duplicate key", configFileErrorCase{yaml: "port: 8080\nport: 8081\n", reason: runtimeconfig.ConfigFileErrorReasonDuplicateKey, key: "port"}),
		ginkgo.Entry("non scalar value", configFileErrorCase{yaml: "trusted-proxy-ips:\n  - 127.0.0.1\n", reason: runtimeconfig.ConfigFileErrorReasonScalarValue, key: "trusted-proxy-ips"}),
		ginkgo.Entry("null value", configFileErrorCase{yaml: "base-path: null\n", reason: runtimeconfig.ConfigFileErrorReasonScalarValue, key: "base-path"}),
		ginkgo.Entry("hidden compatibility key", configFileErrorCase{yaml: "enable-mcp: true\n", reason: runtimeconfig.ConfigFileErrorReasonUnknownKey, key: "enable-mcp"}),
		ginkgo.Entry("removed revision key", configFileErrorCase{yaml: "enable-revision: true\n", reason: runtimeconfig.ConfigFileErrorReasonUnknownKey, key: "enable-revision"}),
		ginkgo.Entry("removed workspace sync key", configFileErrorCase{yaml: "enable-workspace-sync: true\n", reason: runtimeconfig.ConfigFileErrorReasonUnknownKey, key: "enable-workspace-sync"}),
		ginkgo.Entry("removed revision limit key", configFileErrorCase{yaml: "max-revision-history: 0\n", reason: runtimeconfig.ConfigFileErrorReasonUnknownKey, key: "max-revision-history"}),
		ginkgo.Entry("internal key", configFileErrorCase{yaml: "internal-project-daemon: /tmp/startup.json\n", reason: runtimeconfig.ConfigFileErrorReasonUnknownKey, key: "internal-project-daemon"}),
		ginkgo.Entry("config key", configFileErrorCase{yaml: "config: other.yml\n", reason: runtimeconfig.ConfigFileErrorReasonUnknownKey, key: "config"}),
		ginkgo.Entry("mcp stdio compatibility key", configFileErrorCase{yaml: "mcp-stdio: true\n", reason: runtimeconfig.ConfigFileErrorReasonUnknownKey, key: "mcp-stdio"}),
		ginkgo.Entry("bad bool scalar", configFileErrorCase{yaml: "public-access: maybe\n", reason: runtimeconfig.ConfigFileErrorReasonInvalidFlagValue, key: "public-access"}),
		ginkgo.Entry("bad duration scalar", configFileErrorCase{yaml: "access-token-timeout: soon\n", reason: runtimeconfig.ConfigFileErrorReasonInvalidFlagValue, key: "access-token-timeout"}),
	)

	ginkgo.DescribeTable("rejects config mixed with subcommand trailing CLI flag",
		func(args []string, wantErr error) {
			configPath := filepath.Join(leafwikiTempDir(), "leafwiki.yml")
			writeTestConfig(configPath, "data-dir: ./data\n")
			for i, arg := range args {
				if arg == "$CONFIG" {
					args[i] = configPath
				}
			}

			_, _, _, err := parseConfigFlagsForArgsAllowError(args)

			Expect(err).To(HaveOccurred())
			Expect(err).To(MatchError(wantErr))
		},
		ginkgo.Entry("reset password trailing flag", []string{"--config", "$CONFIG", "reset-admin-password", "--data-dir", "other"}, runtimeconfig.ConfigFlagMixError{Flag: "--data-dir"}),
		ginkgo.Entry("agent hook trailing flag", []string{"--config", "$CONFIG", "agent-hook", "codex", "--data-dir", "other"}, runtimeconfig.ConfigFlagMixError{Flag: "--data-dir"}),
	)

	ginkgo.DescribeTable("config agent hook rejects empty config path without fail open",
		func(args []string) {
			payload := `{"hook_event_name":"SessionStart","session_id":"empty-config-secret"}`

			stdout, stderr, err := runLeafwikiHelperWithInputAndTimeout(args, nil, payload, 5*time.Second)

			expectAgentHookConfigRejection(args)
			Expect(err).To(HaveOccurred())
			Expect(stdout).NotTo(Equal("{}\n"))
			Expect(stderr).NotTo(ContainSubstring("empty-config-secret"))
		},
		ginkgo.Entry("inline empty", []string{"--config=", "agent-hook", "codex"}),
		ginkgo.Entry("separate empty", []string{"--config", "", "agent-hook", "codex"}),
		ginkgo.Entry("trailing bare after agent hook", []string{"agent-hook", "codex", "--config"}),
		ginkgo.Entry("inline empty before help after agent hook", []string{"agent-hook", "codex", "--config=", "--help"}),
		ginkgo.Entry("single dash", []string{"--config", "-", "agent-hook", "codex"}),
		ginkgo.Entry("double dash", []string{"--config", "--", "agent-hook", "codex"}),
	)

	ginkgo.DescribeTable("agent hook provider allow responses fail open",
		func(provider agenthooks.ProviderID, payload string, wantStdout string) {
			baseDir := leafwikiTempDir()
			stdout, stderr, err := runLeafwikiHelperWithInputAndTimeout([]string{
				"agent-hook", agentHookProviderCLIArg(provider),
				"--disable-auth",
				"--data-dir", filepath.Join(baseDir, "data"),
				"--root-dir", filepath.Join(baseDir, "content"),
				"--host", "127.0.0.1",
				"--port", freeTCPPort(),
				"--log-target", "stderr",
			}, nil, payload, 5*time.Second)

			Expect(err).NotTo(HaveOccurred())
			Expect(stdout).To(Equal(wantStdout))
			Expect(stderr).NotTo(ContainSubstring("unknown-secret"))
		},
		ginkgo.Entry("claude malformed", agenthooks.ProviderClaude, "{", "{}\n"),
		ginkgo.Entry("cursor malformed", agenthooks.ProviderCursor, "{", "{\"permission\":\"allow\"}\n"),
		ginkgo.Entry("unknown provider", agenthooks.ProviderUnknown, `{"hook_event_name":"SessionStart","session_id":"unknown-secret"}`, ""),
	)

	ginkgo.DescribeTable("control record failures fail open",
		func(recordHandler func(http.ResponseWriter, *http.Request), parentTimeout time.Duration, wantErr types.GomegaMatcher) {
			cfg, cleanup := testRuntimeConfigWithHealthyControlDescriptor(recordHandler)
			defer cleanup()
			ctx := context.Background()
			if parentTimeout > 0 {
				var cancel context.CancelFunc
				ctx, cancel = context.WithTimeout(ctx, parentTimeout)
				defer cancel()
			}

			var stdout bytes.Buffer
			err := runAgentHookCommand(ctx, cfg, agenthooks.ProviderCodex, strings.NewReader(`{"hook_event_name":"SessionStart","session_id":"control-secret"}`), &stdout)

			Expect(err).To(HaveOccurred())
			Expect(stdout.String()).To(Equal("{}\n"))
			Expect(err).To(SatisfyAny(wantErr, MatchError(errProjectLockedNoAttachableDaemon)))
		},
		ginkgo.Entry("control 401", func(w http.ResponseWriter, _ *http.Request) { http.Error(w, "unauthorized", http.StatusUnauthorized) }, time.Duration(0), MatchProjectDaemonControlStatus(http.StatusUnauthorized)),
		ginkgo.Entry("control 400", func(w http.ResponseWriter, _ *http.Request) { http.Error(w, "bad event", http.StatusBadRequest) }, time.Duration(0), MatchProjectDaemonControlStatus(http.StatusBadRequest)),
		ginkgo.Entry("control 500", func(w http.ResponseWriter, _ *http.Request) { http.Error(w, "boom", http.StatusInternalServerError) }, time.Duration(0), MatchProjectDaemonControlStatus(http.StatusInternalServerError)),
		ginkgo.Entry("control timeout", func(w http.ResponseWriter, _ *http.Request) {
			time.Sleep(250 * time.Millisecond)
			w.WriteHeader(http.StatusNoContent)
		}, 50*time.Millisecond, MatchError(context.DeadlineExceeded)),
	)

	ginkgo.DescribeTable("removed revision and workspace sync flags fail unknown",
		func(removedFlag string) {
			baseDir := leafwikiTempDir()
			stdout, stderr, err := runLeafwikiHelperWithTimeout([]string{
				"--disable-auth",
				removedFlag,
				"--data-dir", filepath.Join(baseDir, "data"),
				"--root-dir", filepath.Join(baseDir, "content"),
				"--host", "127.0.0.1",
				"--port", freeTCPPort(),
				"--log-target", "stderr",
			}, nil, 5*time.Second)

			Expect(err).To(HaveOccurred(), "stdout:\n%s\nstderr:\n%s", stdout, stderr)
			Expect(err).NotTo(MatchError(context.DeadlineExceeded))
			Expect(stderr).To(ContainSubstring(strings.TrimLeft(removedFlag, "-")))
		},
		ginkgo.Entry("--enable-revision", "--enable-revision"),
		ginkgo.Entry("--enable-workspace-sync", "--enable-workspace-sync"),
	)

	ginkgo.DescribeTable("legacy MCP flags fail unknown",
		func(removedFlag string) {
			stdout, stderr, err := runLeafwikiHelperWithTimeout([]string{
				removedFlag,
				"--disable-auth",
				"--data-dir", filepath.Join(leafwikiTempDir(), "data"),
				"--root-dir", filepath.Join(leafwikiTempDir(), "content"),
				"--host", "127.0.0.1",
				"--port", freeTCPPort(),
				"--log-target", "stderr",
			}, nil, 5*time.Second)

			Expect(err).To(HaveOccurred(), "stdout:\n%s\nstderr:\n%s", stdout, stderr)
			Expect(stderr).To(ContainSubstring(strings.TrimLeft(removedFlag, "-")))
		},
		ginkgo.Entry("--enable-mcp", "--enable-mcp"),
		ginkgo.Entry("--mcp-stdio", "--mcp-stdio"),
	)

	ginkgo.DescribeTable("daemon relevant descriptor fields",
		func(field string, mut func(*projectdaemon.Config)) {
			owner := completeDaemonCompareConfig()
			requested := owner
			mut(&requested)

			mismatches := compareProjectDaemonConfigForRequest(owner, requested, mcpTransports{HTTP: true})

			Expect(mismatches).To(ConsistOf(HaveField("Field", Equal(field))))
		},
		ginkgo.Entry("data dir", "data-dir", func(cfg *projectdaemon.Config) { cfg.DataDir = "/tmp/other-data" }),
		ginkgo.Entry("root dir", "root-dir", func(cfg *projectdaemon.Config) { cfg.RootDir = "/tmp/other-root" }),
		ginkgo.Entry("auth mode", "auth-disabled", func(cfg *projectdaemon.Config) { cfg.AuthDisabled = !cfg.AuthDisabled }),
		ginkgo.Entry("public MCP", "public-mcp-enabled", func(cfg *projectdaemon.Config) { cfg.PublicMCPEnabled = !cfg.PublicMCPEnabled }),
		ginkgo.Entry("host", "host", func(cfg *projectdaemon.Config) { cfg.Host = "127.0.0.2" }),
		ginkgo.Entry("port", "port", func(cfg *projectdaemon.Config) { cfg.Port = "9090" }),
		ginkgo.Entry("base path", "base-path", func(cfg *projectdaemon.Config) { cfg.BasePath = "/docs" }),
		ginkgo.Entry("markdown link root prefix", "markdown-link-root-prefix", func(cfg *projectdaemon.Config) { cfg.MarkdownLinkRootPrefix = "/docs" }),
		ginkgo.Entry("public access", "public-access", func(cfg *projectdaemon.Config) { cfg.PublicAccess = !cfg.PublicAccess }),
		ginkgo.Entry("allow insecure", "allow-insecure", func(cfg *projectdaemon.Config) { cfg.AllowInsecure = !cfg.AllowInsecure }),
		ginkgo.Entry("access token timeout", "access-token-timeout", func(cfg *projectdaemon.Config) { cfg.AccessTokenTimeout = "2h0m0s" }),
		ginkgo.Entry("refresh token timeout", "refresh-token-timeout", func(cfg *projectdaemon.Config) { cfg.RefreshTokenTimeout = "720h0m0s" }),
		ginkgo.Entry("injected header hash", "inject-code-in-header-hash", func(cfg *projectdaemon.Config) { cfg.InjectCodeInHeaderHash = "other-hash" }),
		ginkgo.Entry("custom stylesheet", "custom-stylesheet", func(cfg *projectdaemon.Config) { cfg.CustomStylesheet = "/tmp/custom.css" }),
		ginkgo.Entry("log target", "log-target", func(cfg *projectdaemon.Config) { cfg.LogTarget = "file" }),
		ginkgo.Entry("log file", "log-file", func(cfg *projectdaemon.Config) { cfg.LogFile = "/tmp/leafwiki.log" }),
		ginkgo.Entry("hide metadata", "hide-link-metadata-section", func(cfg *projectdaemon.Config) { cfg.HideLinkMetadataSection = !cfg.HideLinkMetadataSection }),
		ginkgo.Entry("upload size", "max-asset-upload-size-bytes", func(cfg *projectdaemon.Config) { cfg.MaxAssetUploadSizeBytes = 99 }),
		ginkgo.Entry("link refactor", "enable-link-refactor", func(cfg *projectdaemon.Config) { cfg.EnableLinkRefactor = !cfg.EnableLinkRefactor }),
		ginkgo.Entry("remote user enabled", "enable-http-remote-user", func(cfg *projectdaemon.Config) { cfg.EnableHTTPRemoteUser = !cfg.EnableHTTPRemoteUser }),
		ginkgo.Entry("remote user header", "http-remote-user-header", func(cfg *projectdaemon.Config) { cfg.HTTPRemoteUserHeader = "X-User" }),
		ginkgo.Entry("trusted proxies", "trusted-proxy-ips", func(cfg *projectdaemon.Config) { cfg.TrustedProxyIPs = "127.0.0.1/32" }),
		ginkgo.Entry("remote user logout", "http-remote-user-logout-url", func(cfg *projectdaemon.Config) { cfg.HTTPRemoteUserLogoutURL = "https://example.test/logout" }),
		ginkgo.Entry("request log", "disable-request-log", func(cfg *projectdaemon.Config) { cfg.DisableRequestLog = !cfg.DisableRequestLog }),
		ginkgo.Entry("idle timeout", "daemon-idle-timeout", func(cfg *projectdaemon.Config) { cfg.DaemonIdleTimeout = "1m0s" }),
	)

	ginkgo.DescribeTable("default env CLI and selector",
		func(args []string, envValue *string, want mcpTransports) {
			if envValue != nil {
				leafwikiSetenv("LEAFWIKI_MCP", *envValue)
			}

			got, err := resolveMCPTransportsForArgs(args)

			Expect(err).NotTo(HaveOccurred())
			Expect(got).To(Equal(want))
		},
		ginkgo.Entry("default none", []string(nil), (*string)(nil), mcpTransports{}),
		ginkgo.Entry("env enables http", []string(nil), stringPtr("http"), mcpTransports{HTTP: true}),
		ginkgo.Entry("cli overrides env", []string{"--mcp=stdio"}, stringPtr("http"), mcpTransports{Stdio: true}),
		ginkgo.Entry("combined orderings stdio,http", []string{"--mcp=stdio,http"}, (*string)(nil), mcpTransports{HTTP: true, Stdio: true}),
		ginkgo.Entry("combined orderings http,stdio", []string{"--mcp=http,stdio"}, (*string)(nil), mcpTransports{HTTP: true, Stdio: true}),
		ginkgo.Entry("selector ignores removed legacy envs after env validation", []string{"--mcp=none"}, (*string)(nil), mcpTransports{}),
	)

	ginkgo.DescribeTable("rejects invalid values",
		func(raw string, reason runtimeconfig.MCPTransportErrorReason) {
			_, err := parseMCPTransports(raw)
			Expect(err).To(MatchMCPTransportError(reason))
		},
		ginkgo.Entry("unknown", "websocket", runtimeconfig.MCPTransportErrorReasonInvalid),
		ginkgo.Entry("none combined", "none,stdio", runtimeconfig.MCPTransportErrorReasonNoneMixed),
		ginkgo.Entry("duplicate", "stdio,stdio", runtimeconfig.MCPTransportErrorReasonDuplicate),
		ginkgo.Entry("empty part", "stdio,", runtimeconfig.MCPTransportErrorReasonInvalid),
	)

	type mcpTransportOptionCase struct {
		opts      mcpTransportOptions
		messageID cliMessageID
	}

	ginkgo.DescribeTable("accepts compatible transport settings and rejects invalid STDIO authentication combinations",
		func(tc mcpTransportOptionCase) {
			err := validateMCPTransportOptions(tc.opts)
			if tc.messageID == "" {
				Expect(err).NotTo(HaveOccurred())
				return
			}
			Expect(err).To(MatchCLIRenderedMessageError(tc.messageID))
		},
		ginkgo.Entry("HTTP allows non-loopback web host", mcpTransportOptionCase{opts: mcpTransportOptions{Transports: mcpTransports{HTTP: true}, Host: "0.0.0.0", LogTarget: leaflogging.TargetStderr}}),
		ginkgo.Entry("STDIO allows non-loopback web host", mcpTransportOptionCase{opts: mcpTransportOptions{Transports: mcpTransports{Stdio: true}, DisableAuth: true, Host: "0.0.0.0", LogTarget: leaflogging.TargetStderr}}),
		ginkgo.Entry("STDIO rejects stdout logging", mcpTransportOptionCase{opts: mcpTransportOptions{Transports: mcpTransports{Stdio: true}, DisableAuth: true, Host: "127.0.0.1", LogTarget: leaflogging.TargetStdout}, messageID: cliMessageID(localization.MessageIDCLIErrorStdoutReservedForMCPStdio)}),
		ginkgo.Entry("STDIO auth enabled requires key", mcpTransportOptionCase{opts: mcpTransportOptions{Transports: mcpTransports{Stdio: true}, Host: "127.0.0.1", LogTarget: leaflogging.TargetStderr}, messageID: cliMessageID(localization.MessageIDCLIErrorStdioAuthIdentityRequired)}),
		ginkgo.Entry("STDIO disabled auth rejects key", mcpTransportOptionCase{opts: mcpTransportOptions{Transports: mcpTransports{Stdio: true}, DisableAuth: true, APIKey: "lwk_fake", Host: "127.0.0.1", LogTarget: leaflogging.TargetStderr}, messageID: cliMessageID(localization.MessageIDCLIErrorStdioAuthAPIKeyConflict)}),
		ginkgo.Entry("HTTP ignores API key", mcpTransportOptionCase{opts: mcpTransportOptions{Transports: mcpTransports{HTTP: true}, APIKey: "lwk_invalid", Host: "127.0.0.1", LogTarget: leaflogging.TargetStderr}}),
	)

	ginkgo.DescribeTable("requires trusted proxy IPs only when remote-user auth is enabled",
		func(enabled bool, trustedProxyIPs string, wantErr bool) {
			err := validateHTTPRemoteUserConfig(enabled, trustedProxyIPs)
			Expect(err != nil).To(Equal(wantErr))
		},
		ginkgo.Entry("disabled, no IPs", false, "", false),
		ginkgo.Entry("disabled, with IPs", false, "127.0.0.1", false),
		ginkgo.Entry("enabled, with IPs", true, "127.0.0.1", false),
		ginkgo.Entry("enabled, multiple IPs", true, "127.0.0.1,172.18.0.0/16", false),
		ginkgo.Entry("enabled, no IPs", true, "", true),
		ginkgo.Entry("enabled, whitespace only", true, "   ", true),
		ginkgo.Entry("enabled, commas only", true, ",,,", true),
		ginkgo.Entry("enabled, commas and whitespace", true, " , , ", true),
	)

	ginkgo.DescribeTable("preserves untrusted descriptor when any project lock is held",
		func(lock func(dataDir string, rootDir string) func()) {
			baseDir := leafwikiTempDir()
			dataDir := filepath.Join(baseDir, "data")
			rootDir := filepath.Join(baseDir, "content")
			Expect(os.MkdirAll(filepath.Join(dataDir, ".leafwiki"), 0o755)).To(Succeed())
			Expect(os.MkdirAll(rootDir, 0o755)).To(Succeed())
			canonicalData, canonicalRoot, err := projectdaemon.CanonicalizeProject(dataDir, rootDir)
			Expect(err).NotTo(HaveOccurred())
			descriptorPath := projectdaemon.DescriptorPath(canonicalData)
			Expect(os.WriteFile(descriptorPath, []byte("{"), 0o600)).To(Succeed())
			release := lock(canonicalData, canonicalRoot)
			defer release()

			_, healthy, err := readHealthyProjectDaemon(context.Background(), descriptorPath, projectdaemon.Config{DataDir: canonicalData, RootDir: canonicalRoot})

			Expect(err).To(HaveOccurred())
			Expect(healthy).To(BeFalse())
			_, statErr := os.Stat(descriptorPath)
			Expect(statErr).NotTo(HaveOccurred())
		},
		ginkgo.Entry("data lock held", func(dataDir string, _ string) func() {
			ginkgo.GinkgoHelper()
			lock, err := locking.AcquireDataDirLock(dataDir)
			Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("acquire data lock: %v", err))

			return func() { _ = lock.Release() }
		}),
		ginkgo.Entry("root lock held", func(_ string, rootDir string) func() {
			ginkgo.GinkgoHelper()
			lock, err := locking.AcquireRootDirLock(rootDir)
			Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("acquire root lock: %v", err))

			return func() { _ = lock.Release() }
		}),
	)

	ginkgo.DescribeTable("untrusted stale descriptor is replaced when locks are free",
		func(setup func(path string)) {
			baseDir := leafwikiTempDir()
			dataDir := filepath.Join(baseDir, "data")
			rootDir := filepath.Join(baseDir, "content")
			descriptorPath := filepath.Join(dataDir, ".leafwiki", projectdaemon.DescriptorFileName)
			Expect(os.MkdirAll(filepath.Dir(descriptorPath), 0o755)).To(Succeed())
			Expect(os.MkdirAll(rootDir, 0o755)).To(Succeed())
			setup(descriptorPath)

			port := freeTCPPort()
			proc := startLeafwikiHelper([]string{
				"--disable-auth",
				"--data-dir", dataDir,
				"--root-dir", rootDir,
				"--host", "127.0.0.1",
				"--port", port,
				"--log-target", "stderr",
			}, nil)
			waitForLeafwikiReady(proc, port)

			desc := waitForProjectDaemonDescriptor(dataDir)
			Expect(desc).To(gstruct.PointTo(gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
				"PID":        Not(BeZero()),
				"ControlURL": Not(BeEmpty()),
			})))
			info, err := os.Stat(descriptorPath)
			Expect(err).NotTo(HaveOccurred())
			Expect(info.Mode().Perm()).To(Equal(os.FileMode(0o600)))
		},
		ginkgo.Entry("missing", func(string) {}),
		ginkgo.Entry("corrupt", func(path string) {
			ginkgo.GinkgoHelper()
			Expect(os.WriteFile(path, []byte("{"), 0o600)).To(Succeed())
		}),
		ginkgo.Entry("wrong mode", func(path string) {
			ginkgo.GinkgoHelper()
			Expect(os.WriteFile(path, []byte("{}"), 0o644)).To(Succeed())
		}),
		ginkgo.Entry("non regular path", func(path string) {
			ginkgo.GinkgoHelper()
			Expect(os.Mkdir(path, 0o700)).To(Succeed())
		}),
	)
})

var _ = ginkgo.Describe("cmd leafwiki helper contracts", func() {
	ginkgo.DescribeTable("configModeFlagDisplay",
		func(arg string, name string, want string) {
			Expect(configModeFlagDisplay(arg, name)).To(Equal(want))
		},
		ginkgo.Entry("long flag", "--config", "config", "--config"),
		ginkgo.Entry("single dash flag", "-config", "config", "-config"),
		ginkgo.Entry("inline value", "--config=leafwiki.yml", "config", "--config"),
	)

	ginkgo.DescribeTable("isInvalidBareConfigPathValue",
		func(value string, want bool) {
			Expect(isInvalidBareConfigPathValue(value)).To(Equal(want))
		},
		ginkgo.Entry("empty", "", true),
		ginkgo.Entry("blank", "   ", true),
		ginkgo.Entry("dash", "-", true),
		ginkgo.Entry("double dash", "--", true),
		ginkgo.Entry("valid path", "leafwiki.yml", false),
	)
})

func stringPtr(value string) *string {
	return &value
}
