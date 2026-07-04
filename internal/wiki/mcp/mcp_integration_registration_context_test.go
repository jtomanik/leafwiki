package mcp_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/perber/wiki/internal/core/assets"
	sharederrors "github.com/perber/wiki/internal/core/shared/errors"
	httpinternal "github.com/perber/wiki/internal/http"
	authmw "github.com/perber/wiki/internal/http/middleware/auth"
	testmatchers "github.com/perber/wiki/internal/test_utils/matchers"
	"github.com/perber/wiki/internal/wiki"
	wikimcp "github.com/perber/wiki/internal/wiki/mcp"
)

// Canonical Markdown links plan scenarios covered by tests in this file:
// - MCP agent context returns canonical examples

var _ = Describe("local MCP registration", Label("integration"), func() {
	It("gates the endpoint by configuration and exposes the federated tool contract", func() {
		w := newLocalMCPTestWiki(false)

		embedFrontendOrig := httpinternal.EmbedFrontend
		httpinternal.EmbedFrontend = "true"
		DeferCleanup(func() {
			httpinternal.EmbedFrontend = embedFrontendOrig
		})

		disabledRouter := newLocalMCPTestRouter(w, httpinternal.RouterOptions{
			AuthDisabled:            true,
			PublicAccess:            true,
			AllowInsecure:           true,
			MaxAssetUploadSizeBytes: assets.DefaultMaxUploadSizeBytes,
		})
		for _, method := range []string{http.MethodGet, http.MethodPost, http.MethodDelete} {
			rec := httptest.NewRecorder()
			disabledRouter.ServeHTTP(rec, httptest.NewRequest(method, "/mcp", strings.NewReader("{}")))
			Expect(rec).To(HaveHTTPStatus(http.StatusNotFound))
		}
		for _, path := range []string{"/mcp/", "/mcp/anything"} {
			rec := httptest.NewRecorder()
			disabledRouter.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
			Expect(rec).To(HaveHTTPStatus(http.StatusNotFound))
		}

		authEnabledRouter := newLocalMCPTestRouter(w, httpinternal.RouterOptions{
			AuthDisabled:            false,
			PublicAccess:            true,
			AllowInsecure:           true,
			MaxAssetUploadSizeBytes: assets.DefaultMaxUploadSizeBytes,
			MCPEnabled:              true,
		})
		for _, method := range []string{http.MethodGet, http.MethodPost, http.MethodDelete} {
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(method, "/mcp", strings.NewReader("{}"))
			req.RemoteAddr = "127.0.0.1:12345"
			authEnabledRouter.ServeHTTP(rec, req)
			Expect(rec).To(HaveHTTPStatus(http.StatusUnauthorized))
			challenge := rec.Header().Get("WWW-Authenticate")
			Expect(challenge).To(SatisfyAll(
				ContainSubstring("Bearer"),
				ContainSubstring("resource_metadata=\"http://example.com/.well-known/oauth-protected-resource/mcp\""),
				ContainSubstring("scope=\"leafwiki:mcp\""),
			))
		}

		rec := httptest.NewRecorder()
		disabledRouter.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/.well-known/not-real", nil))
		Expect(rec).To(HaveHTTPStatus(http.StatusNotFound), rec.Body.String())
		Expect(rec.Header().Get("Content-Type")).NotTo(HavePrefix("text/html"))

		trustedProxies, err := authmw.ParseTrustedProxies("127.0.0.1")
		Expect(err).NotTo(HaveOccurred())
		remoteUserRouter := newLocalMCPTestRouter(w, httpinternal.RouterOptions{
			AuthDisabled:            true,
			PublicAccess:            true,
			AllowInsecure:           true,
			MaxAssetUploadSizeBytes: assets.DefaultMaxUploadSizeBytes,
			MCPEnabled:              true,
			HTTPRemoteUser: httpinternal.HTTPRemoteUserConfig{
				Enabled:        true,
				HeaderName:     "X-Remote-User",
				TrustedProxies: trustedProxies,
				UserService:    w.UserService(),
			},
		})
		remoteUserSession := connectLocalMCP(remoteUserRouter, "/mcp")
		current := callToolStructured(remoteUserSession, "wiki_get_current_user", nil)
		user := nestedMap(current, "user")
		Expect(user).To(SatisfyAll(
			HaveKeyWithValue("username", "public-editor"),
			HaveKeyWithValue("role", "editor"),
		))

		missingHostRouter := httpinternal.NewRouter(w.Registrars(), w.FrontendConfig(), httpinternal.RouterOptions{
			AuthDisabled:            true,
			PublicAccess:            true,
			AllowInsecure:           true,
			MaxAssetUploadSizeBytes: assets.DefaultMaxUploadSizeBytes,
			MCPEnabled:              true,
		})
		for _, method := range []string{http.MethodGet, http.MethodPost, http.MethodDelete} {
			rec := httptest.NewRecorder()
			missingHostRouter.ServeHTTP(rec, httptest.NewRequest(method, "/mcp", strings.NewReader("{}")))
			Expect(rec).To(HaveHTTPStatus(http.StatusNotFound))
		}
		nonLoopbackRouter := httpinternal.NewRouter(w.Registrars(), w.FrontendConfig(), httpinternal.RouterOptions{
			AuthDisabled:            true,
			PublicAccess:            true,
			AllowInsecure:           true,
			MaxAssetUploadSizeBytes: assets.DefaultMaxUploadSizeBytes,
			MCPEnabled:              true,
			MCPBindHost:             "0.0.0.0",
		})
		for _, method := range []string{http.MethodGet, http.MethodPost, http.MethodDelete} {
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(method, "/mcp", strings.NewReader("{}"))
			req.RemoteAddr = "100.64.0.10:12345"
			nonLoopbackRouter.ServeHTTP(rec, req)
			Expect(rec).To(HaveHTTPStatus(http.StatusNotFound))
		}
		loopbackRec := httptest.NewRecorder()
		loopbackReq := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader("{}"))
		loopbackReq.RemoteAddr = "127.0.0.1:12345"
		nonLoopbackRouter.ServeHTTP(loopbackRec, loopbackReq)
		Expect(loopbackRec).NotTo(HaveHTTPStatus(http.StatusNotFound))

		enabledRouter := newLocalMCPTestRouter(w, httpinternal.RouterOptions{
			AuthDisabled:            true,
			PublicAccess:            true,
			AllowInsecure:           true,
			MaxAssetUploadSizeBytes: assets.DefaultMaxUploadSizeBytes,
			MCPEnabled:              true,
			MCPToolListPageSize:     5,
		})
		session := connectLocalMCP(enabledRouter, "/mcp")

		firstPage, err := session.ListTools(context.Background(), &sdkmcp.ListToolsParams{})
		Expect(err).NotTo(HaveOccurred())
		Expect(firstPage).To(SatisfyAll(
			HaveField("Tools", HaveLen(5)),
			HaveField("NextCursor", Not(BeEmpty())),
		))

		got := listAllToolNames(session)
		Expect(got).To(matchToolNames(federatedToolNames()))
		Expect(got).To(ContainElement(wikimcp.ToolRefresh.ProtocolName().WireName()))
		for _, name := range got {
			Expect(name).To(HavePrefix("wiki_"))
		}

		tools := listAllTools(session)
		Expect(tools).To(matchInputSchemas(federatedInputProperties(), federatedRequiredProperties()))
		Expect(tools).To(matchOutputSchemas(federatedOutputProperties()))

		for _, legacyName := range []string{
			strings.Join([]string{"get", "page"}, "_"),
			strings.Join([]string{"get", "tree"}, "_"),
			strings.Join([]string{"update", "page"}, "_"),
			strings.Join([]string{"list", "revisions"}, "_"),
		} {
			legacyErr := callToolProtocolError(session, legacyName, map[string]any{"id": "missing"})
			Expect(legacyErr).To(matchJSONRPCErrorCode(jsonrpc.CodeInvalidParams))
		}

		ambiguousPageIDErr := callToolStructuredError(session, "wiki_get_page", map[string]any{
			"id":     "missing-page",
			"pageId": "missing-page",
		})
		Expect(ambiguousPageIDErr).To(testmatchers.HaveMCPStructuredError(wikimcp.ErrCodeMCPPageIdentifierAmbiguous, sharederrors.MessageIDForCode(wikimcp.ErrCodeMCPPageIdentifierAmbiguous)))
		missingPageIDErr := callToolStructuredError(session, "wiki_get_page", map[string]any{})
		Expect(missingPageIDErr).To(testmatchers.HaveMCPStructuredError(wikimcp.ErrCodeMCPPageIdentifierRequired, sharederrors.MessageIDForCode(wikimcp.ErrCodeMCPPageIdentifierRequired)))

		Expect(got).NotTo(ContainElement(HavePrefix("leafwiki_")))
		for _, forbidden := range []string{
			"create_import_plan",
			"get_import_plan",
			"execute_import_plan",
			"clear_import_plan",
			"get_branding",
			"get_branding_asset",
			"get_favicon",
			"login",
			"logout",
			"create_user",
		} {
			Expect(got).NotTo(ContainElement(forbidden))
		}
	})
})

var _ = DescribeTable("local MCP tool registration honors feature gates", Label("integration"),
	func(enableLinkRefactor bool, extraTools []string) {
		w := newLocalMCPTestWikiWithOptions(wiki.WikiOptions{
			AuthDisabled: true,
		})
		router := newLocalMCPTestRouter(w, httpinternal.RouterOptions{
			AuthDisabled:            true,
			PublicAccess:            true,
			AllowInsecure:           true,
			MaxAssetUploadSizeBytes: assets.DefaultMaxUploadSizeBytes,
			EnableLinkRefactor:      enableLinkRefactor,
			MCPEnabled:              true,
			MCPToolListPageSize:     200,
		})
		session := connectLocalMCP(router, "/mcp")

		want := federatedToolNames(extraTools)
		Expect(listAllToolNames(session)).To(matchToolNames(want))
		contextOut := callToolStructured(session, "wiki_get_context", map[string]any{"syncMode": "none"})
		server := nestedMap(contextOut, "server")
		Expect(stringSliceField(server, "tools")).To(matchStringSet(want))

		tools := listAllTools(session)
		Expect(tools).To(matchInputSchemas(federatedInputProperties(extraTools), federatedRequiredProperties(extraTools)))
		Expect(tools).To(matchOutputSchemas(federatedOutputProperties(extraTools)))
	},
	Entry("federated runtime", false, []string(nil)),
	Entry("link refactor", true, wikimcp.LinkRefactorToolNames()),
)

// - MCP agent context returns canonical examples
var _ = Describe("local MCP context", Label("integration"), func() {
	It("returns agent-ready context state and canonical link examples", func() {
		w := newLocalMCPTestWikiWithOptions(wiki.WikiOptions{
			AuthDisabled: true,
		})
		router := newLocalMCPTestRouter(w, httpinternal.RouterOptions{
			AuthDisabled:            true,
			PublicAccess:            true,
			AllowInsecure:           true,
			MaxAssetUploadSizeBytes: assets.DefaultMaxUploadSizeBytes,
			EnableWorkspaceSync:     true,
			MCPEnabled:              true,
			MCPToolListPageSize:     200,
		})
		session := connectLocalMCP(router, "/mcp")

		toolNames := listAllToolNames(session)
		Expect(toolNames).To(ContainElement("wiki_get_context"))

		out := callToolStructured(session, "wiki_get_context", nil)
		Expect(out).To(SatisfyAll(
			HaveKey("contextToken"),
			HaveKey("changesSincePreviousContext"),
			HaveKey("contextHistory"),
			HaveKey("user"),
			HaveKey("config"),
			HaveKey("server"),
			HaveKey("syncStatus"),
			HaveKey("validation"),
			HaveKey("recentChanges"),
			HaveKeyWithValue("activeSessions", BeEmpty()),
			HaveKey("presenceStatus"),
			HaveKey("tree"),
			HaveKey("recommendedTools"),
			HaveKey("canonicalLinkExamples"),
		))
		presence := nestedMap(out, "presenceStatus")
		Expect(presence).To(SatisfyAll(
			HaveKey("web"),
			HaveKey("agentHooks"),
		))
		status := nestedMap(out, "syncStatus")
		Expect(status).To(SatisfyAll(
			HaveKey("enabled"),
			Not(HaveKey("Enabled")),
		))
		for _, name := range stringSliceField(out, "recommendedTools") {
			Expect(toolNames).To(ContainElement(name))
		}
		tree := nestedMap(out, "tree")
		Expect(tree).To(SatisfyAll(
			HaveKey("id"),
			HaveKey("children"),
		))
		examples := stringSliceField(out, "canonicalLinkExamples")
		Expect(examples).To(ContainElement(ContainSubstring("](/docs/guide.md)")))
		Expect(examples).To(ContainElement(ContainSubstring("](/docs)")))
	})
})

var _ = Describe("local MCP context recommendations", Label("integration"), func() {
	It("recommends workspace refresh when federated runtime tools are available", func() {
		w := newLocalMCPTestWiki(false)
		router := newLocalMCPTestRouter(w, httpinternal.RouterOptions{
			AuthDisabled:            true,
			PublicAccess:            true,
			AllowInsecure:           true,
			MaxAssetUploadSizeBytes: assets.DefaultMaxUploadSizeBytes,
			MCPEnabled:              true,
			MCPToolListPageSize:     200,
		})
		session := connectLocalMCP(router, "/mcp")

		out := callToolStructured(session, "wiki_get_context", map[string]any{"syncMode": "none"})
		server := nestedMap(out, "server")

		Expect(server).To(HaveKeyWithValue("tools", ContainElement("wiki_refresh")))
		Expect(out).To(HaveKeyWithValue("recommendedTools", ContainElement("wiki_refresh")))
	})
})

var _ = Describe("local MCP context checkpoints", Label("integration"), func() {
	It("uses the calling session checkpoint when no since token is supplied", func() {
		w := newLocalMCPTestWikiWithOptions(wiki.WikiOptions{
			AuthDisabled: true,
		})
		router := newLocalMCPTestRouter(w, httpinternal.RouterOptions{
			AuthDisabled:            true,
			PublicAccess:            true,
			AllowInsecure:           true,
			MaxAssetUploadSizeBytes: assets.DefaultMaxUploadSizeBytes,
			EnableWorkspaceSync:     true,
			MCPEnabled:              true,
			MCPToolListPageSize:     200,
		})
		sessionA := connectLocalMCP(router, "/mcp")
		sessionB := connectLocalMCP(router, "/mcp")

		firstA := callToolStructured(sessionA, "wiki_get_context", map[string]any{"syncMode": "none"})
		firstAToken := stringField(firstA, "contextToken")

		postHTTPJSON(router, "/api/pages", map[string]any{
			"title": "Omitted Token Delta",
			"slug":  "omitted-token-delta",
			"kind":  "page",
		}, http.StatusCreated)

		secondA := callToolStructured(sessionA, "wiki_get_context", map[string]any{"syncMode": "none"})
		assertContextHistoryOpaque(secondA)
		Expect(secondA).To(HaveKeyWithValue("previousContextToken", firstAToken))
		Expect(changedPathsFromContext(secondA)).To(HaveKey("omitted-token-delta.md"))

		firstB := callToolStructured(sessionB, "wiki_get_context", map[string]any{"syncMode": "none"})
		Expect(firstB).To(SatisfyAll(
			HaveKeyWithValue("previousContextToken", ""),
			HaveKeyWithValue("changesSincePreviousContext", BeEmpty()),
			HaveKeyWithValue("contextHistory", HaveLen(1)),
		))
	})

	It("does not fall back to the previous checkpoint when an explicit since token is unknown", func() {
		w := newLocalMCPTestWikiWithOptions(wiki.WikiOptions{
			AuthDisabled: true,
		})
		router := newLocalMCPTestRouter(w, httpinternal.RouterOptions{
			AuthDisabled:            true,
			PublicAccess:            true,
			AllowInsecure:           true,
			MaxAssetUploadSizeBytes: assets.DefaultMaxUploadSizeBytes,
			EnableWorkspaceSync:     true,
			MCPEnabled:              true,
			MCPToolListPageSize:     200,
		})
		session := connectLocalMCP(router, "/mcp")

		first := callToolStructured(session, "wiki_get_context", map[string]any{"syncMode": "none"})
		firstToken := stringField(first, "contextToken")
		postHTTPJSON(router, "/api/pages", map[string]any{
			"title": "Unknown Token Delta",
			"slug":  "unknown-token-delta",
			"kind":  "page",
		}, http.StatusCreated)

		second := callToolStructured(session, "wiki_get_context", map[string]any{
			"syncMode":   "none",
			"sinceToken": "not-a-token",
		})

		Expect(second).To(SatisfyAll(
			HaveKeyWithValue("previousContextToken", ""),
			HaveKeyWithValue("changesSincePreviousContext", BeEmpty()),
			HaveKeyWithValue("warnings", ContainElement("unknown sinceToken; returned current context")),
		))
		Expect(stringField(first, "contextToken")).To(Equal(firstToken))
	})

	It("returns all checkpoint changes even when recent changes are display-limited", func() {
		w := newLocalMCPTestWikiWithOptions(wiki.WikiOptions{
			AuthDisabled: true,
		})
		router := newLocalMCPTestRouter(w, httpinternal.RouterOptions{
			AuthDisabled:            true,
			PublicAccess:            true,
			AllowInsecure:           true,
			MaxAssetUploadSizeBytes: assets.DefaultMaxUploadSizeBytes,
			EnableWorkspaceSync:     true,
			MCPEnabled:              true,
			MCPToolListPageSize:     200,
		})
		session := connectLocalMCP(router, "/mcp")

		first := callToolStructured(session, "wiki_get_context", map[string]any{
			"recentChangesLimit": float64(1),
			"syncMode":           "none",
		})
		token := stringField(first, "contextToken")
		for i := 0; i < 3; i++ {
			slug := fmt.Sprintf("delta-page-%d", i)
			callToolStructured(session, "wiki_create_page", map[string]any{
				"title": fmt.Sprintf("Delta Page %d", i),
				"slug":  slug,
				"kind":  "page",
			})
		}

		second := callToolStructured(session, "wiki_get_context", map[string]any{
			"sinceToken":         token,
			"recentChangesLimit": float64(1),
			"syncMode":           "none",
		})
		Expect(arrayField(second, "recentChanges")).To(HaveLen(1))
		changes := arrayField(second, "changesSincePreviousContext")
		changedPaths := changedPathsFromChanges(changes)
		Expect(changedPaths).To(SatisfyAll(
			HaveKey("delta-page-0.md"),
			HaveKey("delta-page-1.md"),
			HaveKey("delta-page-2.md"),
		))
		Expect(second).To(Or(
			Not(HaveKey("warnings")),
			HaveKeyWithValue("warnings", BeEmpty()),
		))

		tokenB := stringField(second, "contextToken")
		callToolStructured(session, "wiki_create_page", map[string]any{
			"title": "Delta Page After B",
			"slug":  "delta-page-after-b",
			"kind":  "page",
		})
		third := callToolStructured(session, "wiki_get_context", map[string]any{
			"sinceToken":         tokenB,
			"recentChangesLimit": float64(1),
			"syncMode":           "none",
		})
		changedPathsAfterB := changedPathsFromContext(third)
		Expect(changedPathsAfterB).To(HaveKey("delta-page-after-b.md"))
		for i := 0; i < 3; i++ {
			Expect(changedPathsAfterB).NotTo(HaveKey(fmt.Sprintf("delta-page-%d.md", i)))
		}
	})
})
