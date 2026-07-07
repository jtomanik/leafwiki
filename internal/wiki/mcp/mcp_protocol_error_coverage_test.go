package mcp_test

import (
	"context"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"time"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	coreassets "github.com/perber/wiki/internal/core/assets"
	httpinternal "github.com/perber/wiki/internal/http"
	"github.com/perber/wiki/internal/wiki"
)

var _ = Describe("local MCP protocol errors", Label("integration"), func() {
	It("returns structured tool-error envelopes for invalid inputs and backend failures", func() {
		w, _ := newProtocolErrorWikiWithStorage()
		router := newLocalMCPTestRouter(w, httpinternal.RouterOptions{
			AuthDisabled:            true,
			PublicAccess:            true,
			AllowInsecure:           true,
			MaxAssetUploadSizeBytes: coreassets.DefaultMaxUploadSizeBytes,
			EnableWorkspaceSync:     true,
			EnableLinkRefactor:      true,
			MCPEnabled:              true,
			MCPToolListPageSize:     200,
		})
		session := connectProtocolErrorMCP(router, "/mcp")

		page := protocolNestedMap(protocolCallToolStructured(session, "wiki_create_page", map[string]any{
			"title": "Protocol Target",
			"slug":  "protocol-target",
			"kind":  "page",
		}), "page")
		pageID := protocolStringField(page, "id")
		pageVersion := protocolStringField(page, "version")

		for _, tc := range []struct {
			name string
			args map[string]any
		}{
			{name: "wiki_get_page", args: map[string]any{}},
			{name: "wiki_get_page", args: map[string]any{"pageId": "missing-page"}},
			{name: "wiki_get_page_by_path", args: map[string]any{"path": "../bad"}},
			{name: "wiki_lookup_path", args: map[string]any{"path": "protocol-target", "kind": "bad-kind"}},
			{name: "wiki_lookup_path", args: map[string]any{"path": "../bad"}},
			{name: "wiki_resolve_permalink", args: map[string]any{}},
			{name: "wiki_resolve_permalink", args: map[string]any{"pageId": "missing-page"}},
			{name: "wiki_suggest_slug", args: map[string]any{"title": ""}},
			{name: "wiki_suggest_slug", args: map[string]any{"title": "Child", "parentId": "missing-page"}},
			{name: "wiki_create_page", args: map[string]any{"title": "Bad", "slug": "bad", "kind": "bad-kind"}},
			{name: "wiki_create_page", args: map[string]any{"title": "Bad", "slug": "bad", "kind": "page", "parentId": "missing-page"}},
			{name: "wiki_update_page", args: map[string]any{"id": "missing-page", "version": "missing-version", "title": "Missing", "slug": "missing", "content": "body"}},
			{name: "wiki_update_page", args: map[string]any{"id": pageID, "version": "stale-version", "title": "Protocol Target", "slug": "protocol-target"}},
			{name: "wiki_delete_page", args: map[string]any{"id": "missing-page", "version": "missing-version"}},
			{name: "wiki_move_page", args: map[string]any{"id": "missing-page", "version": "missing-version"}},
			{name: "wiki_sort_pages", args: map[string]any{"parentId": "missing-page", "orderedIds": []string{pageID}}},
			{name: "wiki_ensure_page", args: map[string]any{"path": "ensured", "title": "Ensured", "kind": "bad-kind"}},
			{name: "wiki_ensure_page", args: map[string]any{"path": "../bad", "title": "Ensured", "kind": "page"}},
			{name: "wiki_convert_page", args: map[string]any{"id": pageID, "version": pageVersion, "targetKind": "bad-kind"}},
			{name: "wiki_convert_page", args: map[string]any{"id": "missing-page", "version": "missing-version", "targetKind": "section"}},
			{name: "wiki_copy_page", args: map[string]any{"id": "missing-page", "title": "Copy", "slug": "copy"}},
			{name: "wiki_get_link_status", args: map[string]any{}},
			{name: "wiki_get_link_status", args: map[string]any{"pageId": "missing-page"}},
			{name: "wiki_upload_asset", args: map[string]any{"pageId": "missing-page", "filename": "missing.txt", "contentBase64": base64.StdEncoding.EncodeToString([]byte("asset"))}},
			{name: "wiki_list_assets", args: map[string]any{}},
			{name: "wiki_list_assets", args: map[string]any{"pageId": "missing-page"}},
			{name: "wiki_rename_asset", args: map[string]any{"pageId": "missing-page", "oldFilename": "old.txt", "newFilename": "new.txt"}},
			{name: "wiki_delete_asset", args: map[string]any{"pageId": "missing-page", "filename": "missing.txt"}},
			{name: "wiki_get_subtree", args: map[string]any{"pageId": pageID, "path": "protocol-target"}},
			{name: "wiki_get_subtree", args: map[string]any{"depth": -1}},
			{name: "wiki_get_subtree", args: map[string]any{"pageId": "missing-page"}},
			{name: "wiki_get_pages_by_property", args: map[string]any{"key": "", "value": "draft"}},
			{name: "wiki_get_pages_by_property", args: map[string]any{"key": "status", "value": ""}},
			{name: "wiki_replace_page_section", args: map[string]any{"pageId": pageID, "version": pageVersion, "headingPath": []string{"Missing"}, "content": "replacement"}},
			{name: "wiki_validate_page", args: map[string]any{}},
			{name: "wiki_validate_content", args: map[string]any{"path": "../bad", "content": "body"}},
			{name: "wiki_refresh", args: map[string]any{"source": "bad-source"}},
			{name: "wiki_list_revisions", args: map[string]any{}},
			{name: "wiki_list_revisions", args: map[string]any{"pageId": "missing-page"}},
			{name: "wiki_get_latest_revision", args: map[string]any{}},
			{name: "wiki_preview_page_refactor", args: map[string]any{"pageId": pageID, "kind": "bad-kind"}},
			{name: "wiki_apply_page_refactor", args: map[string]any{"pageId": pageID, "version": pageVersion, "kind": "bad-kind"}},
		} {
			result := protocolToolErrorResult(session, tc.name, tc.args)
			Expect(result.Text).NotTo(BeEmpty())
		}

		Expect(w.Close()).To(Succeed())
	})
})

func newProtocolErrorWikiWithStorage() (*wiki.Wiki, string) {
	GinkgoHelper()
	storageDir := filepath.Join(protocolErrorTempDir(), "data")
	rootDir := filepath.Join(protocolErrorTempDir(), "content")
	w, err := wiki.NewWiki(&wiki.WikiOptions{
		Workspace: wiki.Workspace{
			ID:      newFixtureWorkspaceID("default"),
			DataDir: storageDir,
			RootDir: rootDir,
		},
		AdminPassword:       "admin",
		JWTSecret:           "secretkey",
		AccessTokenTimeout:  15 * time.Minute,
		RefreshTokenTimeout: 7 * 24 * time.Hour,
		AuthDisabled:        true,
	})
	Expect(err).NotTo(HaveOccurred())
	DeferCleanup(func() { _ = w.Close() })
	return w, storageDir
}

func protocolErrorTempDir() string {
	GinkgoHelper()
	dir, err := os.MkdirTemp("", "leafwiki-mcp-protocol-*")
	Expect(err).To(Succeed())
	DeferCleanup(os.RemoveAll, dir)
	return dir
}

func connectProtocolErrorMCP(handler http.Handler, path string) *sdkmcp.ClientSession {
	GinkgoHelper()
	server := httptest.NewServer(handler)
	DeferCleanup(server.Close)

	client := sdkmcp.NewClient(&sdkmcp.Implementation{Name: "leafwiki-test", Version: "test"}, nil)
	session, err := client.Connect(context.Background(), &sdkmcp.StreamableClientTransport{
		Endpoint:             server.URL + path,
		HTTPClient:           server.Client(),
		DisableStandaloneSSE: true,
	}, nil)
	Expect(err).NotTo(HaveOccurred())
	DeferCleanup(func() { _ = session.Close() })
	return session
}

func protocolCallToolStructured(session *sdkmcp.ClientSession, name string, args map[string]any) map[string]any {
	GinkgoHelper()
	result, err := session.CallTool(context.Background(), &sdkmcp.CallToolParams{
		Name:      name,
		Arguments: args,
	})
	Expect(err).NotTo(HaveOccurred())
	Expect(result).To(matchSuccessfulToolResultWithStructuredContent(BeAssignableToTypeOf(map[string]any{})))
	return result.StructuredContent.(map[string]any)
}

func protocolNestedMap(value map[string]any, key string) map[string]any {
	GinkgoHelper()
	Expect(value).To(HaveKeyWithValue(key, BeAssignableToTypeOf(map[string]any{})))
	return value[key].(map[string]any)
}

func protocolStringField(value map[string]any, key string) string {
	GinkgoHelper()
	Expect(value).To(HaveKeyWithValue(key, And(BeAssignableToTypeOf(""), Not(BeEmpty()))))
	return value[key].(string)
}

func protocolToolErrorResult(session *sdkmcp.ClientSession, name string, args map[string]any) mcpToolErrorResult {
	GinkgoHelper()
	result, err := session.CallTool(context.Background(), &sdkmcp.CallToolParams{
		Name:      name,
		Arguments: args,
	})
	Expect(err).NotTo(HaveOccurred())
	return toolErrorResultFromCallResult(name, result)
}
