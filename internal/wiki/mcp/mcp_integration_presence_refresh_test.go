package mcp_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/perber/wiki/internal/agenthooks"
	"github.com/perber/wiki/internal/core/assets"
	wikivalidation "github.com/perber/wiki/internal/core/markdownvalidation"
	httpinternal "github.com/perber/wiki/internal/http"
	"github.com/perber/wiki/internal/projectdaemon"
	"github.com/perber/wiki/internal/wiki"
)

// Canonical Markdown links plan scenarios covered by tests in this file:
// - MCP agent context returns canonical examples

var _ = Describe("local MCP presence", Label("integration"), func() {
	It("includes authenticated web heartbeat state in context", func() {
		w := newLocalMCPTestWiki(false)
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

		page := nestedMap(callToolStructured(session, "wiki_create_page", map[string]any{
			"title": "Docs API",
			"slug":  "docs-api",
			"kind":  "page",
		}), "page")
		postHTTPJSON(router, "/api/presence/heartbeat", map[string]any{
			"sessionId": "tab-web-1",
			"mode":      "edit",
			"pageId":    stringField(page, "id"),
			"path":      "/docs-api",
			"dirty":     true,
		}, http.StatusOK)

		out := callToolStructured(session, "wiki_get_context", nil)
		Expect(nestedMap(out, "presenceStatus")).To(HaveKeyWithValue("web", "enabled"))
		Expect(out).To(HaveKeyWithValue("activeSessions", HaveExactElements(SatisfyAll(
			HaveKeyWithValue("type", "web"),
			HaveKeyWithValue("sessionId", "tab-web-1"),
			HaveKeyWithValue("mode", "edit"),
			HaveKeyWithValue("dirty", true),
			HaveKeyWithValue("user", SatisfyAll(
				HaveKeyWithValue("id", "public-editor"),
				HaveKeyWithValue("role", "editor"),
				Not(HaveKey("email")),
			)),
			HaveKeyWithValue("page", SatisfyAll(
				HaveKeyWithValue("id", stringField(page, "id")),
				HaveKeyWithValue("path", "/docs-api"),
				HaveKeyWithValue("title", "Docs API"),
			)),
			HaveKeyWithValue("lastSeenAt", SatisfyAll(BeAssignableToTypeOf(""), Not(BeEmpty()))),
		))))
	})

	It("merges agent hook presence into context", func() {
		w := newLocalMCPTestWiki(false)
		agentPresence := projectdaemon.NewAgentPresenceRegistry(time.Minute, nil)
		sessionHash := "sha256:" + strings.Repeat("a", 64)
		agentPresence.Record(agenthooks.Event{
			Provider:      agenthooks.ProviderCodex,
			SessionIDHash: sessionHash,
			EventName:     agenthooks.AgentEventSessionStart,
			Model:         "gpt-5",
			Source:        agenthooks.AgentSourceHook,
			SeenAt:        time.Date(2026, 6, 8, 12, 0, 0, 0, time.UTC),
		})
		agentPresence.Record(agenthooks.Event{
			Provider:      agenthooks.ProviderCodex,
			SessionIDHash: sessionHash,
			EventName:     agenthooks.AgentEventSubagentStart,
			SubagentDelta: 1,
			SeenAt:        time.Date(2026, 6, 8, 12, 0, 1, 0, time.UTC),
		})
		w.SetAgentPresenceRegistry(agentPresence)

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

		out := callToolStructured(session, "wiki_get_context", nil)
		Expect(nestedMap(out, "presenceStatus")).To(HaveKeyWithValue("agentHooks", "enabled"))
		Expect(out).To(HaveKeyWithValue("activeSessions", HaveExactElements(SatisfyAll(
			HaveKeyWithValue("type", "agent"),
			HaveKeyWithValue("sessionId", sessionHash),
			HaveKeyWithValue("provider", "codex"),
			HaveKeyWithValue("model", "gpt-5"),
			HaveKeyWithValue("source", "hook"),
			HaveKeyWithValue("lastEvent", "SubagentStart"),
			HaveKeyWithValue("activeSubagents", float64(1)),
		))))
	})

	It("requires authentication before accepting heartbeat updates", func() {
		w := newLocalMCPTestWiki(false)
		router := newLocalMCPTestRouter(w, httpinternal.RouterOptions{
			AuthDisabled:            false,
			PublicAccess:            false,
			AllowInsecure:           true,
			MaxAssetUploadSizeBytes: assets.DefaultMaxUploadSizeBytes,
		})

		req := httptest.NewRequest(http.MethodPost, "/api/presence/heartbeat", strings.NewReader(`{"sessionId":"tab-1","mode":"view"}`))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		Expect(rec).To(HaveHTTPStatus(http.StatusUnauthorized), rec.Body.String())
	})

	It("rejects missing CSRF and invalid heartbeat payloads without poisoning active sessions", func() {
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

		req := httptest.NewRequest(http.MethodPost, "/api/presence/heartbeat", strings.NewReader(`{"sessionId":"tab-no-csrf","mode":"view"}`))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		Expect(rec).To(HaveHTTPStatus(http.StatusForbidden), rec.Body.String())

		postHTTPJSON(router, "/api/presence/heartbeat", map[string]any{
			"sessionId": "tab-valid",
			"mode":      "view",
		}, http.StatusOK)
		postHTTPJSON(router, "/api/presence/heartbeat", map[string]any{
			"sessionId": "tab-invalid",
			"mode":      "invalid",
		}, http.StatusBadRequest)
		postHTTPJSON(router, "/api/presence/heartbeat", map[string]any{
			"sessionId": "tab-missing-page",
			"mode":      "view",
			"path":      "/deleted-page",
		}, http.StatusOK)

		out := callToolStructured(session, "wiki_get_context", map[string]any{"syncMode": "none"})
		Expect(out).To(HaveKeyWithValue("activeSessions", ConsistOf(
			HaveKeyWithValue("sessionId", "tab-valid"),
			SatisfyAll(
				HaveKeyWithValue("sessionId", "tab-missing-page"),
				Not(HaveKey("page")),
			),
		)))
	})
})

var _ = Describe("local MCP workspace refresh", Label("integration"), func() {
	It("syncs direct markdown files into page and context results", func() {
		rootDir := filepath.Join(mcpIntegrationTempDir(), "content")
		w := newLocalMCPTestWikiWithOptions(wiki.WikiOptions{
			Workspace:    wiki.Workspace{RootDir: rootDir},
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

		Expect(os.WriteFile(filepath.Join(rootDir, "direct.md"), []byte("---\nleafwiki_id: direct\nleafwiki_title: Direct\n---\n# Direct\n"), 0o644)).To(Succeed())

		out := callToolStructured(session, "wiki_refresh", nil)
		Expect(nestedMap(out, "syncStatus")).To(SatisfyAll(
			HaveKeyWithValue("lastCommitHash", BeAssignableToTypeOf("")),
			Not(HaveKey("LastCommitHash")),
		))
		Expect(out).To(HaveKey("validation"))

		readBack := callToolStructured(session, "wiki_get_page_by_path", map[string]any{"path": "direct"})
		Expect(nestedMap(readBack, "page")).To(SatisfyAll(
			HaveKeyWithValue("id", "direct"),
			HaveKeyWithValue("title", "Direct"),
		))

		contextOut := callToolStructured(session, "wiki_get_context", map[string]any{
			"syncMode":           "none",
			"recentChangesLimit": float64(1),
		})
		Expect(contextOut).To(HaveKeyWithValue("recentChanges", HaveExactElements(SatisfyAll(
			HaveKeyWithValue("source", "mcp"),
			HaveKeyWithValue("reason", "explicit_refresh"),
			HaveKeyWithValue("commitId", BeAssignableToTypeOf("")),
			HaveKeyWithValue("timestamp", BeAssignableToTypeOf("")),
			HaveKeyWithValue("actor", BeAssignableToTypeOf("")),
			HaveKeyWithValue("pageIds", ContainElement("direct")),
		))))
	})

	It("normalizes workspace routes before validation and refresh results", func() {
		rootDir := filepath.Join(mcpIntegrationTempDir(), "content")
		w := newLocalMCPTestWikiWithOptions(wiki.WikiOptions{
			Workspace:    wiki.Workspace{RootDir: rootDir},
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

		Expect(os.MkdirAll(filepath.Join(rootDir, "plans"), 0o755)).To(Succeed())
		Expect(os.WriteFile(filepath.Join(rootDir, "plans", "index.md"), []byte("---\nleafwiki_id: plans\nleafwiki_title: Plans\n---\n# Plans\n"), 0o644)).To(Succeed())
		Expect(os.WriteFile(filepath.Join(rootDir, "plans", "agent_hooks.PLAN.md"), []byte("---\nleafwiki_id: agent-hooks-plan\nleafwiki_title: Agent Hooks Plan\n---\n# Agent Hooks Plan\n\ncontent\n"), 0o644)).To(Succeed())

		validateOut := callToolStructured(session, "wiki_validate_wiki", map[string]any{"includeWarnings": false})
		Expect(validateOut).To(HaveKeyWithValue("ok", true))
		Expect(validateOut).To(matchValidationIssueCodesAbsent([]wikivalidation.IssueCode{wikivalidation.IssueCodeInvalidSlug}))

		refreshOut := callToolStructured(session, "wiki_refresh", map[string]any{"source": "filesystem"})
		validation := nestedMap(refreshOut, "validation")
		Expect(validation).To(HaveKeyWithValue("ok", true))
		Expect(validation).To(matchValidationIssueCodesAbsent([]wikivalidation.IssueCode{wikivalidation.IssueCodeInvalidSlug}))

		readBack := callToolStructured(session, "wiki_get_page_by_path", map[string]any{"path": "plans/agent-hooks-plan", "kind": "page"})
		Expect(nestedMap(readBack, "page")).To(SatisfyAll(
			HaveKeyWithValue("id", "agent-hooks-plan"),
			HaveKeyWithValue("title", "Agent Hooks Plan"),
		))
	})

	It("omits validation details when validation is disabled", func() {
		rootDir := filepath.Join(mcpIntegrationTempDir(), "content")
		w := newLocalMCPTestWikiWithOptions(wiki.WikiOptions{
			Workspace:    wiki.Workspace{RootDir: rootDir},
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

		Expect(os.WriteFile(filepath.Join(rootDir, "skip-validation.md"), []byte("---\nleafwiki_id: skip-validation\nleafwiki_title: Skip Validation\n---\n# Skip Validation\n"), 0o644)).To(Succeed())

		out := callToolStructured(session, "wiki_refresh", map[string]any{"validate": false})
		Expect(out).NotTo(HaveKey("validation"))
	})

	It("returns validation errors without disabling later MCP calls", func() {
		rootDir := filepath.Join(mcpIntegrationTempDir(), "content")
		w := newLocalMCPTestWikiWithOptions(wiki.WikiOptions{
			Workspace:    wiki.Workspace{RootDir: rootDir},
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

		Expect(os.WriteFile(filepath.Join(rootDir, "a.md"), []byte("---\nleafwiki_id: duplicate\nleafwiki_title: A\n---\n# A\n"), 0o644)).To(Succeed())
		Expect(os.WriteFile(filepath.Join(rootDir, "b.md"), []byte("---\nleafwiki_id: duplicate\nleafwiki_title: B\n---\n# B\n"), 0o644)).To(Succeed())

		out := callToolStructured(session, "wiki_refresh", map[string]any{"source": "filesystem"})
		validation := nestedMap(out, "validation")
		Expect(nestedMap(validation, "summary")).To(HaveKeyWithValue("errors", BeNumerically(">", 0)))
		Expect(validation).To(matchValidationIssueCodes([]wikivalidation.IssueCode{wikivalidation.IssueCodeDuplicateLeafwikiID}))

		currentUser := callToolStructured(session, "wiki_get_current_user", nil)
		Expect(nestedMap(currentUser, "user")).To(HaveKeyWithValue("username", BeAssignableToTypeOf("")))
	})

	It("records an explicit filesystem refresh reason and caps context paths", func() {
		rootDir := filepath.Join(mcpIntegrationTempDir(), "content")
		w := newLocalMCPTestWikiWithOptions(wiki.WikiOptions{
			Workspace:    wiki.Workspace{RootDir: rootDir},
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

		for i := 0; i < 25; i++ {
			slug := fmt.Sprintf("bulk-refresh-%02d", i)
			Expect(os.WriteFile(filepath.Join(rootDir, slug+".md"), []byte("---\nleafwiki_id: "+slug+"\nleafwiki_title: "+slug+"\n---\n# "+slug+"\n"), 0o644)).To(Succeed())
		}

		callToolStructured(session, "wiki_refresh", map[string]any{"source": "filesystem"})
		contextOut := callToolStructured(session, "wiki_get_context", map[string]any{
			"syncMode":           "none",
			"recentChangesLimit": float64(1),
		})
		Expect(contextOut).To(HaveKeyWithValue("recentChanges", HaveExactElements(SatisfyAll(
			HaveKeyWithValue("reason", "explicit_refresh"),
			HaveKeyWithValue("source", "filesystem"),
			HaveKeyWithValue("changedCount", BeNumerically(">=", 25)),
			HaveKeyWithValue("changedPaths", HaveLen(20)),
		))))
	})
})
