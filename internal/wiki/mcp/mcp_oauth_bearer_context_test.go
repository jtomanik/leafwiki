package mcp_test

import (
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	coreauth "github.com/perber/wiki/internal/core/auth"
	sharederrors "github.com/perber/wiki/internal/core/shared/errors"
	testmatchers "github.com/perber/wiki/internal/test_utils/matchers"
	"github.com/perber/wiki/internal/wiki"
	wikimcp "github.com/perber/wiki/internal/wiki/mcp"
)

var _ = Describe("local MCP OAuth bearer protection", Label("integration"), func() {
	It("challenges unauthenticated requests and rejects stale bearer identities", func() {
		w := newLocalMCPAuthTestWikiWithOptions(wiki.WikiOptions{})
		opts := oauthRouterOptions("")
		opts.EnableLinkRefactor = true
		opts.EnableWorkspaceSync = true
		router := newLocalMCPTestRouter(w, opts)

		rec := performRequest(router, http.MethodPost, "http://leafwiki.local/mcp", nil, strings.NewReader("{}"))
		Expect(rec).To(HaveHTTPStatus(http.StatusUnauthorized), rec.Body.String())
		Expect(rec.Header().Get("WWW-Authenticate")).To(SatisfyAll(
			ContainSubstring(`resource_metadata="http://leafwiki.local/.well-known/oauth-protected-resource/mcp"`),
			ContainSubstring(`scope="leafwiki:mcp"`),
		))

		rec = performRequest(router, http.MethodPost, "http://leafwiki.local/mcp", nil, strings.NewReader("{}"))
		result := rec.Result()
		Expect(result.Body.Close()).To(Succeed())
		req := httptest.NewRequest(http.MethodPost, "http://leafwiki.local/mcp", strings.NewReader("{}"))
		req.RemoteAddr = "127.0.0.1:12345"
		req.Header.Set("Authorization", "Bearer invalid-token")
		rec = httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		Expect(rec).To(HaveHTTPStatus(http.StatusUnauthorized), rec.Body.String())

		adminToken := oauthAccessTokenForUser(router, "admin", "admin", "admin-state")
		adminSession := connectLocalMCPWithToken(router, "/mcp", adminToken)
		current := callToolStructured(adminSession, "wiki_get_current_user", nil)
		user := nestedMap(current, "user")
		Expect(user).To(HaveKeyWithValue("username", "admin"))
		expectedTools := append(append([]string{}, baseToolNames...), wikimcp.WorkspaceSyncToolNames()...)
		expectedTools = append(expectedTools, wikimcp.RevisionToolNames()...)
		expectedTools = append(expectedTools, wikimcp.LinkRefactorToolNames()...)
		Expect(listAllToolNames(adminSession)).To(matchToolNames(expectedTools))

		editor, err := w.UserService().CreateUser("editor", "editor@example.com", "editorpass", coreauth.RoleEditor)
		Expect(err).NotTo(HaveOccurred())
		editorToken := oauthAccessTokenForUser(router, "editor", "editorpass", "editor-state")
		_, err = w.UserService().UpdateUser(coreauth.UserIDFromString(editor.ID), editor.Username, editor.Email, "", coreauth.RoleViewer)
		Expect(err).NotTo(HaveOccurred())

		downgradedSession := connectLocalMCPWithToken(router, "/mcp", editorToken)
		downgradedErr := callTypedToolStructuredError(downgradedSession, wikimcp.ToolCreatePage, map[string]any{
			"title": "Downgraded Write",
			"slug":  "downgraded-write",
		})
		Expect(downgradedErr).To(testmatchers.HaveMCPStructuredError(wikimcp.ErrCodeMCPEditorRoleRequired, sharederrors.MessageIDForCode(wikimcp.ErrCodeMCPEditorRoleRequired)))

		deleted, err := w.UserService().CreateUser("deleted", "deleted@example.com", "deletedpass", coreauth.RoleEditor)
		Expect(err).NotTo(HaveOccurred())
		deletedToken := oauthAccessTokenForUser(router, "deleted", "deletedpass", "deleted-state")
		Expect(w.UserService().DeleteUser(coreauth.UserIDFromString(deleted.ID))).To(Succeed())

		req = httptest.NewRequest(http.MethodPost, "http://leafwiki.local/mcp", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`))
		req.RemoteAddr = "127.0.0.1:12345"
		req.Header.Set("Authorization", "Bearer "+deletedToken)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "application/json, text/event-stream")
		rec = httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		Expect(rec).To(HaveHTTPStatus(http.StatusUnauthorized), rec.Body.String())
	})
})

var _ = DescribeTable("OAuth-authenticated viewers are denied editor MCP tools", Label("integration"),
	func(toolName wikimcp.ToolID, buildArgs func(pageID, currentVersion, latestRevisionID string) map[string]any) {
		w := newLocalMCPAuthTestWikiWithOptions(wiki.WikiOptions{})
		opts := oauthRouterOptions("")
		opts.EnableLinkRefactor = true
		opts.EnableWorkspaceSync = true
		router := newLocalMCPTestRouter(w, opts)

		adminToken := oauthAccessTokenForUser(router, "admin", "admin", "admin-state")
		adminSession := connectLocalMCPWithToken(router, "/mcp", adminToken)

		_, err := w.UserService().CreateUser("viewer", "viewer@example.com", "viewerpass", coreauth.RoleViewer)
		Expect(err).NotTo(HaveOccurred())
		viewerToken := oauthAccessTokenForUser(router, "viewer", "viewerpass", "viewer-state")
		viewerSession := connectLocalMCPWithToken(router, "/mcp", viewerToken)
		_ = callToolStructured(viewerSession, "wiki_get_tree", nil)

		page := nestedMap(callToolStructured(adminSession, "wiki_create_page", map[string]any{
			"title": "Viewer Gate Fixture",
			"slug":  "viewer-gate-fixture",
			"kind":  "page",
		}), "page")
		pageID := stringField(page, "id")
		pageVersion := stringField(page, "version")
		updatedContent := "viewer gate fixture revision"
		updated := nestedMap(callToolStructured(adminSession, "wiki_update_page", map[string]any{
			"id":      pageID,
			"version": pageVersion,
			"title":   "Viewer Gate Fixture",
			"slug":    "viewer-gate-fixture",
			"content": updatedContent,
		}), "page")
		currentVersion := stringField(updated, "version")
		_ = callToolStructured(adminSession, "wiki_upload_asset", map[string]any{
			"pageId":        pageID,
			"filename":      "viewer-gate.txt",
			"contentBase64": base64.StdEncoding.EncodeToString([]byte("viewer gate asset")),
		})
		latestRevision := nestedMap(callToolStructured(adminSession, "wiki_get_latest_revision", map[string]any{"pageId": pageID}), "revision")
		latestRevisionID := stringField(latestRevision, "id")

		errResult := callTypedToolStructuredError(viewerSession, toolName, buildArgs(pageID, currentVersion, latestRevisionID))
		Expect(errResult).To(testmatchers.HaveMCPStructuredError(wikimcp.ErrCodeMCPEditorRoleRequired, sharederrors.MessageIDForCode(wikimcp.ErrCodeMCPEditorRoleRequired)))

		afterViewerDenied := nestedMap(callToolStructured(adminSession, "wiki_get_page", map[string]any{"pageId": pageID}), "page")
		Expect(afterViewerDenied).To(SatisfyAll(
			HaveKeyWithValue("version", currentVersion),
			HaveKeyWithValue("content", updatedContent),
		))
	},
	Entry("suggesting a slug", wikimcp.ToolSuggestSlug, func(pageID, currentVersion, latestRevisionID string) map[string]any {
		return map[string]any{"title": "Viewer Slug"}
	}),
	Entry("refreshing the workspace", wikimcp.ToolRefresh, func(pageID, currentVersion, latestRevisionID string) map[string]any {
		return map[string]any{"source": "filesystem"}
	}),
	Entry("creating a page", wikimcp.ToolCreatePage, func(pageID, currentVersion, latestRevisionID string) map[string]any {
		return map[string]any{"title": "Viewer Write", "slug": "viewer-write"}
	}),
	Entry("updating page content", wikimcp.ToolUpdatePage, func(pageID, currentVersion, latestRevisionID string) map[string]any {
		return map[string]any{"id": pageID, "version": currentVersion, "title": "Viewer Gate Fixture", "slug": "viewer-gate-fixture", "content": "viewer update"}
	}),
	Entry("updating page metadata", wikimcp.ToolUpdatePageMetadata, func(pageID, currentVersion, latestRevisionID string) map[string]any {
		return map[string]any{"pageId": pageID, "version": currentVersion, "addTags": []any{"viewer"}}
	}),
	Entry("replacing a page section", wikimcp.ToolReplacePageSection, func(pageID, currentVersion, latestRevisionID string) map[string]any {
		return map[string]any{"pageId": pageID, "version": currentVersion, "headingPath": []any{"Missing"}, "content": "viewer section"}
	}),
	Entry("deleting a page", wikimcp.ToolDeletePage, func(pageID, currentVersion, latestRevisionID string) map[string]any {
		return map[string]any{"id": pageID, "version": currentVersion, "recursive": false}
	}),
	Entry("moving a page", wikimcp.ToolMovePage, func(pageID, currentVersion, latestRevisionID string) map[string]any {
		return map[string]any{"id": pageID, "version": currentVersion}
	}),
	Entry("sorting pages", wikimcp.ToolSortPages, func(pageID, currentVersion, latestRevisionID string) map[string]any {
		return map[string]any{"parentId": "", "orderedIds": []any{pageID}}
	}),
	Entry("ensuring a page", wikimcp.ToolEnsurePage, func(pageID, currentVersion, latestRevisionID string) map[string]any {
		return map[string]any{"path": "viewer/ensured", "title": "Viewer Ensured"}
	}),
	Entry("converting a page kind", wikimcp.ToolConvertPage, func(pageID, currentVersion, latestRevisionID string) map[string]any {
		return map[string]any{"id": pageID, "version": currentVersion, "targetKind": "section"}
	}),
	Entry("copying a page", wikimcp.ToolCopyPage, func(pageID, currentVersion, latestRevisionID string) map[string]any {
		return map[string]any{"id": pageID, "title": "Viewer Copy", "slug": "viewer-copy"}
	}),
	Entry("uploading an asset", wikimcp.ToolUploadAsset, func(pageID, currentVersion, latestRevisionID string) map[string]any {
		return map[string]any{"pageId": pageID, "filename": "viewer.txt", "contentBase64": base64.StdEncoding.EncodeToString([]byte("viewer"))}
	}),
	Entry("renaming an asset", wikimcp.ToolRenameAsset, func(pageID, currentVersion, latestRevisionID string) map[string]any {
		return map[string]any{"pageId": pageID, "oldFilename": "viewer-gate.txt", "newFilename": "viewer-renamed.txt"}
	}),
	Entry("deleting an asset", wikimcp.ToolDeleteAsset, func(pageID, currentVersion, latestRevisionID string) map[string]any {
		return map[string]any{"pageId": pageID, "filename": "viewer-gate.txt"}
	}),
	Entry("restoring a revision", wikimcp.ToolRestoreRevision, func(pageID, currentVersion, latestRevisionID string) map[string]any {
		return map[string]any{"pageId": pageID, "revisionId": latestRevisionID}
	}),
	Entry("previewing a page refactor", wikimcp.ToolPreviewRefactor, func(pageID, currentVersion, latestRevisionID string) map[string]any {
		return map[string]any{"pageId": pageID, "kind": "page", "title": "Viewer Preview", "slug": "viewer-preview"}
	}),
	Entry("applying a page refactor", wikimcp.ToolApplyRefactor, func(pageID, currentVersion, latestRevisionID string) map[string]any {
		return map[string]any{"pageId": pageID, "version": currentVersion, "kind": "page", "title": "Viewer Apply", "slug": "viewer-apply"}
	}),
)

var _ = Describe("local MCP API-key bearer protection", Label("integration"), func() {
	It("allows editor keys and rejects viewer downgraded revoked and deleted credentials", func() {
		w := newLocalMCPAuthTestWikiWithOptions(wiki.WikiOptions{})
		opts := oauthRouterOptions("")
		opts.EnableWorkspaceSync = true
		router := newLocalMCPTestRouter(w, opts)

		editor, err := w.UserService().CreateUser("api-editor", "api-editor@example.com", "editorpass", coreauth.RoleEditor)
		Expect(err).NotTo(HaveOccurred())
		editorID := coreauth.UserIDFromString(editor.ID)
		editorKey, err := w.APIKeyService().CreateAPIKey(editorID, "Editor MCP", editorID)
		Expect(err).NotTo(HaveOccurred())

		editorSession := connectLocalMCPWithToken(router, "/mcp", editorKey.Secret)
		current := callToolStructured(editorSession, "wiki_get_current_user", nil)
		user := nestedMap(current, "user")
		Expect(user).To(HaveKeyWithValue("username", "api-editor"))
		created := nestedMap(callToolStructured(editorSession, "wiki_create_page", map[string]any{
			"title": "API Key Editor Page",
			"slug":  "api-key-editor-page",
		}), "page")
		pageID := stringField(created, "id")
		pageVersion := stringField(created, "version")
		updated := nestedMap(callToolStructured(editorSession, "wiki_update_page", map[string]any{
			"id":      pageID,
			"version": pageVersion,
			"title":   "API Key Editor Page",
			"slug":    "api-key-editor-page",
			"content": "updated through api key",
		}), "page")
		currentVersion := stringField(updated, "version")

		viewer, err := w.UserService().CreateUser("api-viewer", "api-viewer@example.com", "viewerpass", coreauth.RoleViewer)
		Expect(err).NotTo(HaveOccurred())
		viewerID := coreauth.UserIDFromString(viewer.ID)
		viewerKey, err := w.APIKeyService().CreateAPIKey(viewerID, "Viewer MCP", viewerID)
		Expect(err).NotTo(HaveOccurred())

		viewerSession := connectLocalMCPWithToken(router, "/mcp", viewerKey.Secret)
		_ = callToolStructured(viewerSession, "wiki_get_tree", nil)
		for _, tt := range []struct {
			name wikimcp.ToolID
			args map[string]any
		}{
			{name: wikimcp.ToolRefresh, args: map[string]any{"source": "filesystem"}},
			{name: wikimcp.ToolCreatePage, args: map[string]any{"title": "Viewer API Key Write", "slug": "viewer-api-key-write"}},
			{name: wikimcp.ToolUpdatePageMetadata, args: map[string]any{"pageId": pageID, "version": currentVersion, "addTags": []any{"viewer"}}},
			{name: wikimcp.ToolReplacePageSection, args: map[string]any{"pageId": pageID, "version": currentVersion, "headingPath": []any{"Missing"}, "content": "viewer section"}},
		} {
			viewerErr := callTypedToolStructuredError(viewerSession, tt.name, tt.args)
			Expect(viewerErr).To(testmatchers.HaveMCPStructuredError(wikimcp.ErrCodeMCPEditorRoleRequired, sharederrors.MessageIDForCode(wikimcp.ErrCodeMCPEditorRoleRequired)))
		}
		afterViewerDenied := nestedMap(callToolStructured(editorSession, "wiki_get_page", map[string]any{"pageId": pageID}), "page")
		Expect(afterViewerDenied).To(SatisfyAll(
			HaveKeyWithValue("version", currentVersion),
			HaveKeyWithValue("content", "updated through api key"),
		))

		Expect(w.APIKeyService().RevokeAPIKey(editorID, editorKey.Key.ID)).To(Succeed())
		Expect(mcpBearerAuthorizationAttempt(router, "/mcp", editorKey.Secret)).To(HaveHTTPStatus(http.StatusUnauthorized))

		roleUser, err := w.UserService().CreateUser("api-role-change", "api-role-change@example.com", "editorpass", coreauth.RoleEditor)
		Expect(err).NotTo(HaveOccurred())
		roleUserID := coreauth.UserIDFromString(roleUser.ID)
		roleKey, err := w.APIKeyService().CreateAPIKey(roleUserID, "Role MCP", roleUserID)
		Expect(err).NotTo(HaveOccurred())
		_, err = w.UserService().UpdateUser(roleUserID, roleUser.Username, roleUser.Email, "", coreauth.RoleViewer)
		Expect(err).NotTo(HaveOccurred())
		downgradedSession := connectLocalMCPWithToken(router, "/mcp", roleKey.Secret)
		downgradedErr := callTypedToolStructuredError(downgradedSession, wikimcp.ToolCreatePage, map[string]any{
			"title": "Downgraded API Key Write",
			"slug":  "downgraded-api-key-write",
		})
		Expect(downgradedErr).To(testmatchers.HaveMCPStructuredError(wikimcp.ErrCodeMCPEditorRoleRequired, sharederrors.MessageIDForCode(wikimcp.ErrCodeMCPEditorRoleRequired)))

		deleted, err := w.UserService().CreateUser("api-deleted", "api-deleted@example.com", "editorpass", coreauth.RoleEditor)
		Expect(err).NotTo(HaveOccurred())
		deletedID := coreauth.UserIDFromString(deleted.ID)
		deletedKey, err := w.APIKeyService().CreateAPIKey(deletedID, "Deleted MCP", deletedID)
		Expect(err).NotTo(HaveOccurred())
		Expect(w.UserService().DeleteUser(deletedID)).To(Succeed())
		Expect(mcpBearerAuthorizationAttempt(router, "/mcp", deletedKey.Secret)).To(HaveHTTPStatus(http.StatusUnauthorized))
		Expect(mcpBearerAuthorizationAttempt(router, "/mcp", deletedKey.Secret+"_wrongsecret")).To(HaveHTTPStatus(http.StatusUnauthorized))
	})
})

var _ = Describe("local MCP context for viewers", Label("integration"), func() {
	It("skips forced workspace refresh while still returning context state", func() {
		rootDir := filepath.Join(oauthTestTempDir(), "content")
		w := newLocalMCPAuthTestWikiWithOptions(wiki.WikiOptions{
			Workspace: wiki.Workspace{RootDir: rootDir},
		})
		opts := oauthRouterOptions("")
		opts.EnableWorkspaceSync = true
		router := newLocalMCPTestRouter(w, opts)

		viewer, err := w.UserService().CreateUser("context-viewer", "context-viewer@example.com", "viewerpass", coreauth.RoleViewer)
		Expect(err).NotTo(HaveOccurred())
		viewerID := coreauth.UserIDFromString(viewer.ID)
		viewerKey, err := w.APIKeyService().CreateAPIKey(viewerID, "Viewer Context MCP", viewerID)
		Expect(err).NotTo(HaveOccurred())
		viewerSession := connectLocalMCPWithToken(router, "/mcp", viewerKey.Secret)

		out := callToolStructured(viewerSession, "wiki_get_context", map[string]any{
			"syncMode": "force",
		})

		Expect(out).To(SatisfyAll(
			HaveKeyWithValue("warnings", ContainElement(ContainSubstring("not an editor or admin"))),
			HaveKey("syncStatus"),
			HaveKeyWithValue("recommendedTools", Not(ContainElements(
				"wiki_refresh",
				"wiki_update_page",
				"wiki_create_page",
				"wiki_update_page_metadata",
				"wiki_replace_page_section",
			))),
		))
	})
})

var _ = Describe("private MCP API-key sessions", Label("integration"), func() {
	It("blocks read-only tools after the backing API key is revoked", func() {
		w := newLocalMCPAuthTestWiki()

		editor, err := w.UserService().CreateUser("private-stdio-editor", "private-stdio-editor@example.com", "editorpass", coreauth.RoleEditor)
		Expect(err).NotTo(HaveOccurred())
		editorID := coreauth.UserIDFromString(editor.ID)
		apiKey, err := w.APIKeyService().CreateAPIKey(editorID, "Private STDIO MCP", editorID)
		Expect(err).NotTo(HaveOccurred())

		handler := w.PrivateMCPHTTPHandler(oauthRouterOptions(""))
		session := connectLocalMCPWithToken(handler, "/mcp", apiKey.Secret)
		_ = callToolStructured(session, "wiki_get_tree", nil)

		Expect(w.APIKeyService().RevokeAPIKey(editorID, apiKey.Key.ID)).To(Succeed())
		Expect(mcpBearerAuthorizationAttempt(handler, "/mcp", apiKey.Secret)).To(HaveHTTPStatus(http.StatusUnauthorized))
	})
})

var _ = Describe("local MCP OAuth base paths", Label("integration"), func() {
	It("accepts API-key sessions on the configured base path", func() {
		w := newLocalMCPAuthTestWiki()
		router := newLocalMCPTestRouter(w, oauthRouterOptions("/wiki"))

		admin, err := w.UserService().GetUserByUsername("admin")
		Expect(err).NotTo(HaveOccurred())
		adminID := coreauth.UserIDFromString(admin.ID)
		apiKey, err := w.APIKeyService().CreateAPIKey(adminID, "Base Path MCP", adminID)
		Expect(err).NotTo(HaveOccurred())
		session := connectLocalMCPWithToken(router, "/wiki/mcp", apiKey.Secret)
		current := callToolStructured(session, "wiki_get_current_user", nil)
		user := nestedMap(current, "user")
		Expect(user).To(HaveKeyWithValue("username", "admin"))

		config := callToolStructured(session, "wiki_get_config", nil)
		Expect(config).To(HaveKeyWithValue("basePath", "/wiki"))
	})

	It("advertises the protected resource metadata for base-path challenges", func() {
		w := newLocalMCPAuthTestWiki()
		router := newLocalMCPTestRouter(w, oauthRouterOptions("/wiki"))

		rec := performRequest(router, http.MethodPost, "http://leafwiki.local/wiki/mcp", nil, strings.NewReader("{}"))

		Expect(rec).To(HaveHTTPStatus(http.StatusUnauthorized), rec.Body.String())
		Expect(rec.Header().Get("WWW-Authenticate")).To(ContainSubstring(`resource_metadata="http://leafwiki.local/.well-known/oauth-protected-resource/wiki/mcp"`))
	})

	It("accepts OAuth sessions on the configured base path", func() {
		w := newLocalMCPAuthTestWiki()
		router := newLocalMCPTestRouter(w, oauthRouterOptions("/wiki"))
		cookies := loginCookiesAt(router, "/wiki", "admin", "admin")
		token := oauthAccessTokenWithCookiesAt(router, cookies, "/wiki", "base-path-session-state", "http://leafwiki.local/wiki/mcp")

		session := connectLocalMCPWithToken(router, "/wiki/mcp", token)
		current := callToolStructured(session, "wiki_get_current_user", nil)
		user := nestedMap(current, "user")
		Expect(user).To(HaveKeyWithValue("username", "admin"))

		config := callToolStructured(session, "wiki_get_config", nil)
		Expect(config).To(HaveKeyWithValue("basePath", "/wiki"))
		Expect(listAllToolNames(session)).To(matchToolNames(federatedToolNames()))
	})
})
