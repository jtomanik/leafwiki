package mcp_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/perber/wiki/internal/agenthooks"
	"github.com/perber/wiki/internal/core/assets"
	"github.com/perber/wiki/internal/core/markdown"
	httpinternal "github.com/perber/wiki/internal/http"
	authmw "github.com/perber/wiki/internal/http/middleware/auth"
	"github.com/perber/wiki/internal/projectdaemon"
	"github.com/perber/wiki/internal/wiki"
	wikiassets "github.com/perber/wiki/internal/wiki/assets"
	wikimcp "github.com/perber/wiki/internal/wiki/mcp"
	wikipages "github.com/perber/wiki/internal/wiki/pages"
	wikirevisions "github.com/perber/wiki/internal/wiki/revisions"
	wikitags "github.com/perber/wiki/internal/wiki/tags"
)

// Canonical Markdown links plan scenarios covered by tests in this file:
// - MCP agent context returns canonical examples

var baseToolNames = wikimcp.BaseToolNames()

func federatedToolNames(extra ...[]string) []string {
	names := append([]string{}, baseToolNames...)
	names = append(names, wikimcp.WorkspaceSyncToolNames()...)
	names = append(names, wikimcp.RevisionToolNames()...)
	for _, group := range extra {
		names = append(names, group...)
	}
	return names
}

func federatedInputProperties(extra ...[]string) map[string][]string {
	out := copyToolInputProperties(baseToolInputProperties)
	for _, name := range federatedToolNames(extra...) {
		if props, ok := featureToolInputProperties[name]; ok {
			out[name] = props
		}
	}
	return out
}

func federatedRequiredProperties(extra ...[]string) map[string][]string {
	out := copyToolInputProperties(baseToolInputRequiredProperties)
	for _, name := range federatedToolNames(extra...) {
		if props, ok := featureToolInputRequiredProperties[name]; ok {
			out[name] = props
		}
	}
	return out
}

func federatedOutputProperties(extra ...[]string) map[string][]string {
	out := copyToolInputProperties(baseToolOutputProperties)
	for _, name := range federatedToolNames(extra...) {
		if props, ok := featureToolOutputProperties[name]; ok {
			out[name] = props
		}
	}
	return out
}

var mcpOnlyToolNames = map[string]struct{}{
	wikimcp.ToolGetContext:         {},
	wikimcp.ToolRefresh:            {},
	wikimcp.ToolGetSubtree:         {},
	wikimcp.ToolValidatePage:       {},
	wikimcp.ToolValidateContent:    {},
	wikimcp.ToolValidateWiki:       {},
	wikimcp.ToolUpdatePageMetadata: {},
	wikimcp.ToolReplacePageSection: {},
}

var baseToolInputProperties = map[string][]string{
	"wiki_get_context":           {"sinceToken", "syncMode", "treeDepth", "recentChangesLimit"},
	"wiki_get_subtree":           {"pageId", "path", "depth", "includeMetadata", "includeLinkCounts", "includeContentPreview"},
	"wiki_validate_page":         {"kind", "pageId", "path"},
	"wiki_validate_content":      {"path", "content", "existingPageId", "kind"},
	"wiki_validate_wiki":         {"includeWarnings"},
	"wiki_update_page_metadata":  {"pageId", "path", "version", "setTags", "addTags", "removeTags", "setProperties", "removeProperties", "includePage", "includeValidation", "includeLinkStatus"},
	"wiki_replace_page_section":  {"pageId", "path", "version", "headingPath", "occurrence", "content", "includePage", "includeValidation", "includeLinkStatus"},
	"wiki_get_config":            {},
	"wiki_get_current_user":      {},
	"wiki_get_tree":              {"depth"},
	"wiki_get_page":              {"id", "pageId"},
	"wiki_get_page_by_path":      {"kind", "path"},
	"wiki_lookup_path":           {"path", "kind"},
	"wiki_resolve_permalink":     {"id", "pageId"},
	"wiki_suggest_slug":          {"parentId", "currentId", "title"},
	"wiki_create_page":           {"parentId", "title", "slug", "kind"},
	"wiki_update_page":           {"id", "version", "title", "slug", "content", "tags", "properties"},
	"wiki_delete_page":           {"id", "version", "recursive"},
	"wiki_move_page":             {"id", "version", "parentId"},
	"wiki_sort_pages":            {"parentId", "orderedIds"},
	"wiki_ensure_page":           {"path", "title", "kind"},
	"wiki_convert_page":          {"id", "version", "targetKind"},
	"wiki_copy_page":             {"id", "targetParentId", "title", "slug"},
	"wiki_search_pages":          {"q", "tags", "offset", "limit"},
	"wiki_get_search_status":     {},
	"wiki_list_tags":             {"q", "selected", "limit"},
	"wiki_get_pages_by_tags":     {"tags"},
	"wiki_list_property_keys":    {"q", "limit"},
	"wiki_get_pages_by_property": {"key", "value"},
	"wiki_get_link_status":       {"id", "pageId"},
	"wiki_upload_asset":          {"pageId", "filename", "contentBase64"},
	"wiki_get_asset":             {"pageId", "filename"},
	"wiki_list_assets":           {"id", "pageId"},
	"wiki_rename_asset":          {"pageId", "oldFilename", "newFilename"},
	"wiki_delete_asset":          {"pageId", "filename"},
}

var featureToolInputProperties = map[string][]string{
	"wiki_refresh":               {"validate", "source"},
	"wiki_list_revisions":        {"id", "pageId", "cursor", "limit"},
	"wiki_get_latest_revision":   {"id", "pageId"},
	"wiki_get_revision":          {"id", "pageId", "revisionId"},
	"wiki_compare_revisions":     {"id", "pageId", "baseRevisionId", "targetRevisionId"},
	"wiki_get_revision_asset":    {"id", "pageId", "revisionId", "assetName"},
	"wiki_restore_revision":      {"id", "pageId", "revisionId"},
	"wiki_preview_page_refactor": {"id", "pageId", "kind", "title", "slug", "content", "parentId"},
	"wiki_apply_page_refactor":   {"id", "pageId", "version", "kind", "title", "slug", "content", "parentId", "rewriteLinks"},
}

type httpMCPParityCase struct {
	Tool      string
	HTTPRoute string
	Assertion string
}

var mcpHTTPParityCases = []httpMCPParityCase{
	{Tool: "wiki_get_config", HTTPRoute: "GET /api/config", Assertion: "selected config fields match"},
	{Tool: "wiki_get_current_user", HTTPRoute: "GET /api/auth/me", Assertion: "current user payloads match"},
	{Tool: "wiki_get_tree", HTTPRoute: "GET /api/tree", Assertion: "tree payloads match"},
	{Tool: "wiki_get_page", HTTPRoute: "GET /api/pages/:id", Assertion: "page payloads match"},
	{Tool: "wiki_get_page_by_path", HTTPRoute: "GET /api/pages/by-path", Assertion: "page payloads match"},
	{Tool: "wiki_lookup_path", HTTPRoute: "GET /api/pages/lookup", Assertion: "lookup payloads match"},
	{Tool: "wiki_resolve_permalink", HTTPRoute: "GET /api/pages/permalink/:id", Assertion: "target payloads match"},
	{Tool: "wiki_suggest_slug", HTTPRoute: "GET /api/pages/slug-suggestion", Assertion: "slug payloads match"},
	{Tool: "wiki_create_page", HTTPRoute: "POST /api/pages", Assertion: "created page is visible through HTTP with matching payload"},
	{Tool: "wiki_update_page", HTTPRoute: "PUT /api/pages/:id", Assertion: "updated page is visible through HTTP and stale page_version_conflict errors match"},
	{Tool: "wiki_delete_page", HTTPRoute: "DELETE /api/pages/:id", Assertion: "success payloads, deleted state, and optimistic page_version_conflict errors match"},
	{Tool: "wiki_move_page", HTTPRoute: "PUT /api/pages/:id/move", Assertion: "success payloads, final route lookup, parent placement, and stale page_version_conflict errors match"},
	{Tool: "wiki_sort_pages", HTTPRoute: "PUT /api/pages/:id/sort", Assertion: "message payloads match"},
	{Tool: "wiki_ensure_page", HTTPRoute: "POST /api/pages/ensure", Assertion: "ensured page payloads match"},
	{Tool: "wiki_convert_page", HTTPRoute: "POST /api/pages/convert/:id", Assertion: "HTTP no-content result, final page, and stale page_version_conflict errors match"},
	{Tool: "wiki_copy_page", HTTPRoute: "POST /api/pages/copy/:id", Assertion: "copied page fields, route lookup, and source preservation match"},
	{Tool: "wiki_search_pages", HTTPRoute: "GET /api/search", Assertion: "count, pagination, facets, hasMore, and items match"},
	{Tool: "wiki_get_search_status", HTTPRoute: "GET /api/search/status", Assertion: "status payloads match"},
	{Tool: "wiki_list_tags", HTTPRoute: "GET /api/tags", Assertion: "tag payloads match"},
	{Tool: "wiki_get_pages_by_tags", HTTPRoute: "GET /api/tags/pages", Assertion: "page payloads match"},
	{Tool: "wiki_list_property_keys", HTTPRoute: "GET /api/properties", Assertion: "key payloads match"},
	{Tool: "wiki_get_pages_by_property", HTTPRoute: "GET /api/properties/pages", Assertion: "page payloads match"},
	{Tool: "wiki_get_link_status", HTTPRoute: "GET /api/pages/:id/links", Assertion: "link status payloads match"},
	{Tool: "wiki_upload_asset", HTTPRoute: "POST /api/pages/:id/assets", Assertion: "upload result, exact bytes, MIME type, and list visibility match"},
	{Tool: "wiki_get_asset", HTTPRoute: "GET /assets/:pageId/:filename", Assertion: "asset content and type match"},
	{Tool: "wiki_list_assets", HTTPRoute: "GET /api/pages/:id/assets", Assertion: "asset lists match"},
	{Tool: "wiki_rename_asset", HTTPRoute: "PUT /api/pages/:id/assets/rename", Assertion: "rename result, exact bytes, old-name absence, and new-name presence match"},
	{Tool: "wiki_delete_asset", HTTPRoute: "DELETE /api/pages/:id/assets/:name", Assertion: "delete payloads and exact asset absence match"},
	{Tool: "wiki_list_revisions", HTTPRoute: "GET /api/pages/:id/revisions", Assertion: "revision list payloads match"},
	{Tool: "wiki_get_latest_revision", HTTPRoute: "GET /api/pages/:id/revisions/latest", Assertion: "revision payloads match"},
	{Tool: "wiki_get_revision", HTTPRoute: "GET /api/pages/:id/revisions/:revisionId", Assertion: "snapshot payloads match"},
	{Tool: "wiki_compare_revisions", HTTPRoute: "GET /api/pages/:id/revisions/compare", Assertion: "comparison payloads match"},
	{Tool: "wiki_get_revision_asset", HTTPRoute: "GET /api/pages/:id/revisions/:revisionId/assets/:name", Assertion: "asset content and type match"},
	{Tool: "wiki_restore_revision", HTTPRoute: "POST /api/pages/:id/revisions/:revisionId/restore", Assertion: "stable restored page fields match"},
	{Tool: "wiki_preview_page_refactor", HTTPRoute: "POST /api/pages/:id/refactor/preview", Assertion: "preview payloads match"},
	{Tool: "wiki_apply_page_refactor", HTTPRoute: "POST /api/pages/:id/refactor/apply", Assertion: "successful apply payloads, rewritten links, and stale page_version_conflict errors match"},
}

var parityCoverage = struct {
	sync.Mutex
	seen map[string]map[string]struct{}
}{seen: map[string]map[string]struct{}{}}

func resetHTTPMCPParityCoverage() {
	parityCoverage.Lock()
	defer parityCoverage.Unlock()
	parityCoverage.seen = map[string]map[string]struct{}{}
}

func hasHTTPMCPParityRecorded(t *testing.T) bool {
	t.Helper()

	seenCases, expected := expectedHTTPMCPParityCases(t)
	parityCoverage.Lock()
	defer parityCoverage.Unlock()
	for _, name := range expected {
		tc := seenCases[name]
		if _, exercised := parityCoverage.seen[name][tc.HTTPRoute]; !exercised {
			return false
		}
	}
	return true
}

func recordHTTPMCPParity(t *testing.T, tool, httpRoute string) {
	t.Helper()
	found := false
	for _, tc := range mcpHTTPParityCases {
		if tc.Tool == tool {
			found = true
			if tc.HTTPRoute != httpRoute {
				t.Fatalf("parity record for %s used route %q, want %q", tool, httpRoute, tc.HTTPRoute)
			}
			break
		}
	}
	if !found {
		t.Fatalf("parity record for unknown tool %q", tool)
	}

	parityCoverage.Lock()
	defer parityCoverage.Unlock()
	routes := parityCoverage.seen[tool]
	if routes == nil {
		routes = map[string]struct{}{}
		parityCoverage.seen[tool] = routes
	}
	routes[httpRoute] = struct{}{}
}

func assertHTTPMCPParityRecorded(t *testing.T) {
	t.Helper()

	seenCases, expected := expectedHTTPMCPParityCases(t)
	parityCoverage.Lock()
	defer parityCoverage.Unlock()
	for _, name := range expected {
		tc := seenCases[name]
		if _, exercised := parityCoverage.seen[name][tc.HTTPRoute]; !exercised {
			t.Fatalf("parity case for tool %q route %q was not recorded by an executable assertion", name, tc.HTTPRoute)
		}
	}
}

func expectedHTTPMCPParityCases(t *testing.T) (map[string]httpMCPParityCase, []string) {
	t.Helper()

	seenCases := make(map[string]httpMCPParityCase, len(mcpHTTPParityCases))
	for _, tc := range mcpHTTPParityCases {
		if tc.Tool == "" || tc.HTTPRoute == "" || tc.Assertion == "" {
			t.Fatalf("parity case must declare tool, HTTP route, and assertion text: %#v", tc)
		}
		if prior, exists := seenCases[tc.Tool]; exists {
			t.Fatalf("duplicate parity case for %s: %#v and %#v", tc.Tool, prior, tc)
		}
		seenCases[tc.Tool] = tc
	}

	expected := make([]string, 0, len(baseToolNames)+len(wikimcp.RevisionToolNames())+len(wikimcp.LinkRefactorToolNames()))
	for _, name := range baseToolNames {
		if _, mcpOnly := mcpOnlyToolNames[name]; mcpOnly {
			continue
		}
		expected = append(expected, name)
	}
	expected = append(expected, wikimcp.RevisionToolNames()...)
	expected = append(expected, wikimcp.LinkRefactorToolNames()...)
	sort.Strings(expected)

	for _, name := range expected {
		_, exists := seenCases[name]
		if !exists {
			t.Fatalf("missing HTTP/MCP parity case for tool %q", name)
		}
	}
	return seenCases, expected
}

var baseToolInputRequiredProperties = map[string][]string{
	"wiki_get_context":           {},
	"wiki_get_subtree":           {},
	"wiki_validate_page":         {},
	"wiki_validate_content":      {"path", "content"},
	"wiki_validate_wiki":         {},
	"wiki_update_page_metadata":  {"version"},
	"wiki_replace_page_section":  {"version", "headingPath", "content"},
	"wiki_get_config":            {},
	"wiki_get_current_user":      {},
	"wiki_get_tree":              {},
	"wiki_get_page":              {},
	"wiki_get_page_by_path":      {"path"},
	"wiki_lookup_path":           {"path"},
	"wiki_resolve_permalink":     {},
	"wiki_suggest_slug":          {"title"},
	"wiki_create_page":           {"title", "slug"},
	"wiki_update_page":           {"id", "version", "title", "slug"},
	"wiki_delete_page":           {"id", "version"},
	"wiki_move_page":             {"id", "version"},
	"wiki_sort_pages":            {"parentId", "orderedIds"},
	"wiki_ensure_page":           {"path", "title"},
	"wiki_convert_page":          {"id", "version", "targetKind"},
	"wiki_copy_page":             {"id", "title", "slug"},
	"wiki_search_pages":          {},
	"wiki_get_search_status":     {},
	"wiki_list_tags":             {},
	"wiki_get_pages_by_tags":     {"tags"},
	"wiki_list_property_keys":    {},
	"wiki_get_pages_by_property": {"key", "value"},
	"wiki_get_link_status":       {},
	"wiki_upload_asset":          {"pageId", "filename", "contentBase64"},
	"wiki_get_asset":             {"pageId", "filename"},
	"wiki_list_assets":           {},
	"wiki_rename_asset":          {"pageId", "oldFilename", "newFilename"},
	"wiki_delete_asset":          {"pageId", "filename"},
}

var featureToolInputRequiredProperties = map[string][]string{
	"wiki_refresh":               {},
	"wiki_list_revisions":        {},
	"wiki_get_latest_revision":   {},
	"wiki_get_revision":          {"revisionId"},
	"wiki_compare_revisions":     {"baseRevisionId", "targetRevisionId"},
	"wiki_get_revision_asset":    {"revisionId", "assetName"},
	"wiki_restore_revision":      {"revisionId"},
	"wiki_preview_page_refactor": {"kind"},
	"wiki_apply_page_refactor":   {"version", "kind"},
}

var baseToolOutputProperties = map[string][]string{
	"wiki_get_context":           {"contextToken", "previousContextToken", "changesSincePreviousContext", "contextHistory", "user", "config", "server", "syncStatus", "validation", "recentChanges", "activeSessions", "presenceStatus", "tree", "recommendedTools", "canonicalLinkExamples", "warnings"},
	"wiki_get_subtree":           {"root", "breadcrumbs", "depth", "truncated"},
	"wiki_validate_page":         {"ok", "summary", "issues"},
	"wiki_validate_content":      {"ok", "summary", "issues"},
	"wiki_validate_wiki":         {"ok", "summary", "issues"},
	"wiki_update_page_metadata":  {"pageId", "path", "title", "version", "validation", "page", "linkStatus"},
	"wiki_replace_page_section":  {"pageId", "path", "title", "version", "validation", "page", "linkStatus"},
	"wiki_get_config":            {"publicAccess", "hideLinkMetadataSection", "authDisabled", "basePath", "maxAssetUploadSizeBytes", "enableWorkspaceSync", "enableLinkRefactor", "httpRemoteUserEnabled", "httpRemoteUserLogoutUrl", "markdownLinkRootPrefix"},
	"wiki_get_current_user":      {"user"},
	"wiki_get_tree":              {"tree"},
	"wiki_get_page":              {"linkStatus", "page"},
	"wiki_get_page_by_path":      {"linkStatus", "page"},
	"wiki_lookup_path":           {"lookup"},
	"wiki_resolve_permalink":     {"target"},
	"wiki_suggest_slug":          {"slug"},
	"wiki_create_page":           {"page"},
	"wiki_update_page":           {"page"},
	"wiki_delete_page":           {"message"},
	"wiki_move_page":             {"message"},
	"wiki_sort_pages":            {"message"},
	"wiki_ensure_page":           {"page"},
	"wiki_convert_page":          {"message"},
	"wiki_copy_page":             {"page"},
	"wiki_search_pages":          {"count", "items", "limit", "offset", "tagFacets", "hasMore"},
	"wiki_get_search_status":     {"status"},
	"wiki_list_tags":             {"tags"},
	"wiki_get_pages_by_tags":     {"pages"},
	"wiki_list_property_keys":    {"keys"},
	"wiki_get_pages_by_property": {"pages"},
	"wiki_get_link_status":       {"status"},
	"wiki_upload_asset":          {"file"},
	"wiki_get_asset":             {"filename", "mimeType", "contentBase64"},
	"wiki_list_assets":           {"files"},
	"wiki_rename_asset":          {"url"},
	"wiki_delete_asset":          {"message"},
}

var featureToolOutputProperties = map[string][]string{
	"wiki_refresh":               {"syncStatus", "recentChangedPaths", "validation", "lastCommitHash"},
	"wiki_list_revisions":        {"revisions", "nextCursor"},
	"wiki_get_latest_revision":   {"revision"},
	"wiki_get_revision":          {"revision", "content", "assets"},
	"wiki_compare_revisions":     {"base", "target", "contentChanged", "assetChanges"},
	"wiki_get_revision_asset":    {"filename", "mimeType", "contentBase64"},
	"wiki_restore_revision":      {"page"},
	"wiki_preview_page_refactor": {"kind", "pageId", "oldPath", "newPath", "affectedPages", "counts", "warnings"},
	"wiki_apply_page_refactor":   {"page"},
}

var toolOutputOptionalProperties = map[string][]string{
	"wiki_get_context":          {"warnings"},
	"wiki_refresh":              {"validation"},
	"wiki_update_page_metadata": {"validation", "page", "linkStatus"},
	"wiki_replace_page_section": {"validation", "page", "linkStatus"},
}

func TestLocalMCPRegistration_DisabledByDefaultAndToolListMatchesPlan(t *testing.T) {
	w := newLocalMCPTestWiki(t, false)

	embedFrontendOrig := httpinternal.EmbedFrontend
	httpinternal.EmbedFrontend = "true"
	t.Cleanup(func() {
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
		if rec.Code != http.StatusNotFound {
			t.Fatalf("%s /mcp without MCP enabled = %d, want 404", method, rec.Code)
		}
	}
	for _, path := range []string{"/mcp/", "/mcp/anything"} {
		rec := httptest.NewRecorder()
		disabledRouter.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		if rec.Code != http.StatusNotFound {
			t.Fatalf("GET %s without MCP enabled = %d, want 404", path, rec.Code)
		}
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
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("%s /mcp with auth enabled = %d, want 401", method, rec.Code)
		}
		challenge := rec.Header().Get("WWW-Authenticate")
		if !strings.Contains(challenge, "Bearer") ||
			!strings.Contains(challenge, "resource_metadata=\"http://example.com/.well-known/oauth-protected-resource/mcp\"") ||
			!strings.Contains(challenge, "scope=\"leafwiki:mcp\"") {
			t.Fatalf("%s /mcp challenge = %q, want OAuth bearer challenge", method, challenge)
		}
	}

	rec := httptest.NewRecorder()
	disabledRouter.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/.well-known/not-real", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("GET unknown well-known route = %d, want 404: %s", rec.Code, rec.Body.String())
	}
	if contentType := rec.Header().Get("Content-Type"); strings.HasPrefix(contentType, "text/html") {
		t.Fatalf("GET unknown well-known route content-type = %q, want non-HTML 404", contentType)
	}

	trustedProxies, err := authmw.ParseTrustedProxies("127.0.0.1")
	if err != nil {
		t.Fatalf("ParseTrustedProxies failed: %v", err)
	}
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
	remoteUserSession := connectLocalMCP(t, remoteUserRouter, "/mcp")
	current := callToolStructured(t, remoteUserSession, "wiki_get_current_user", nil)
	user := nestedMap(t, current, "user")
	if user["username"] != "public-editor" || user["role"] != "editor" {
		t.Fatalf("remote-user disabled-auth MCP current user = %#v, want public-editor editor", user)
	}

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
		if rec.Code != http.StatusNotFound {
			t.Fatalf("%s /mcp without validated loopback host = %d, want 404", method, rec.Code)
		}
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
		if rec.Code != http.StatusNotFound {
			t.Fatalf("%s /mcp from non-loopback client = %d, want 404", method, rec.Code)
		}
	}
	loopbackRec := httptest.NewRecorder()
	loopbackReq := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader("{}"))
	loopbackReq.RemoteAddr = "127.0.0.1:12345"
	nonLoopbackRouter.ServeHTTP(loopbackRec, loopbackReq)
	if loopbackRec.Code == http.StatusNotFound {
		t.Fatalf("loopback /mcp with non-loopback bind returned 404, want registered local MCP route")
	}

	enabledRouter := newLocalMCPTestRouter(w, httpinternal.RouterOptions{
		AuthDisabled:            true,
		PublicAccess:            true,
		AllowInsecure:           true,
		MaxAssetUploadSizeBytes: assets.DefaultMaxUploadSizeBytes,
		MCPEnabled:              true,
		MCPToolListPageSize:     5,
	})
	session := connectLocalMCP(t, enabledRouter, "/mcp")

	firstPage, err := session.ListTools(context.Background(), &sdkmcp.ListToolsParams{})
	if err != nil {
		t.Fatalf("ListTools first page failed: %v", err)
	}
	if len(firstPage.Tools) != 5 {
		t.Fatalf("first ListTools page length = %d, want 5", len(firstPage.Tools))
	}
	if firstPage.NextCursor == "" {
		t.Fatalf("first ListTools page did not include next cursor")
	}

	got := listAllToolNames(t, session)
	assertToolNames(t, got, federatedToolNames())
	if !contains(got, wikimcp.ToolRefresh) {
		t.Fatalf("%s was not registered in federated runtime", wikimcp.ToolRefresh)
	}
	for _, name := range got {
		if !strings.HasPrefix(name, "wiki_") {
			t.Fatalf("tool %q does not use required wiki_ prefix", name)
		}
	}

	tools := listAllTools(t, session)
	assertInputSchemasMatch(t, tools, federatedInputProperties(), federatedRequiredProperties())
	assertOutputSchemasMatch(t, tools, federatedOutputProperties())

	for _, legacyName := range []string{
		strings.Join([]string{"get", "page"}, "_"),
		strings.Join([]string{"get", "tree"}, "_"),
		strings.Join([]string{"update", "page"}, "_"),
		strings.Join([]string{"list", "revisions"}, "_"),
	} {
		legacyErr := callToolProtocolError(t, session, legacyName, map[string]any{"id": "missing"})
		if !strings.Contains(strings.ToLower(legacyErr), "unknown") {
			t.Fatalf("legacy %s error = %q, want unknown tool", legacyName, legacyErr)
		}
	}

	typeErr := callToolError(t, session, "wiki_get_page", map[string]any{"id": float64(12)})
	if !strings.Contains(strings.ToLower(typeErr), "validating") && !strings.Contains(strings.ToLower(typeErr), "string") {
		t.Fatalf("wiki_get_page invalid type error = %q, want schema validation detail", typeErr)
	}
	ambiguousPageIDErr := callToolError(t, session, "wiki_get_page", map[string]any{
		"id":     "missing-page",
		"pageId": "missing-page",
	})
	assertErrorContainsAny(t, "wiki_get_page ambiguous id/pageId", ambiguousPageIDErr, "id and pageId cannot both be supplied")
	missingPageIDErr := callToolError(t, session, "wiki_get_page", map[string]any{})
	assertErrorContainsAny(t, "wiki_get_page missing id/pageId", missingPageIDErr, "id or pageId is required")

	for _, name := range got {
		if strings.HasPrefix(name, "leafwiki_") {
			t.Fatalf("tool %q has forbidden leafwiki_ prefix", name)
		}
	}
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
		if contains(got, forbidden) {
			t.Fatalf("forbidden tool %q was registered in MCP tool list", forbidden)
		}
	}
}

func TestLocalMCPRegistration_FeatureGatedTools(t *testing.T) {
	refactorTools := wikimcp.LinkRefactorToolNames()

	tests := []struct {
		name               string
		enableLinkRefactor bool
		extraTools         []string
	}{
		{name: "federated runtime"},
		{name: "link refactor", enableLinkRefactor: true, extraTools: refactorTools},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := newLocalMCPTestWikiWithOptions(t, wiki.WikiOptions{
				AuthDisabled: true,
			})
			router := newLocalMCPTestRouter(w, httpinternal.RouterOptions{
				AuthDisabled:            true,
				PublicAccess:            true,
				AllowInsecure:           true,
				MaxAssetUploadSizeBytes: assets.DefaultMaxUploadSizeBytes,
				EnableLinkRefactor:      tt.enableLinkRefactor,
				MCPEnabled:              true,
				MCPToolListPageSize:     200,
			})
			session := connectLocalMCP(t, router, "/mcp")

			want := federatedToolNames(tt.extraTools)
			assertToolNames(t, listAllToolNames(t, session), want)
			contextOut := callToolStructured(t, session, "wiki_get_context", map[string]any{"syncMode": "none"})
			server := nestedMap(t, contextOut, "server")
			assertStringSet(t, "wiki_get_context server.tools", stringSliceField(t, server, "tools"), want)

			tools := listAllTools(t, session)
			assertInputSchemasMatch(t, tools, federatedInputProperties(tt.extraTools), federatedRequiredProperties(tt.extraTools))
			assertOutputSchemasMatch(t, tools, federatedOutputProperties(tt.extraTools))
		})
	}
}

// - MCP agent context returns canonical examples
func TestLocalMCPGetContext_ReturnsAgentReadyContext(t *testing.T) {
	w := newLocalMCPTestWikiWithOptions(t, wiki.WikiOptions{
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
	session := connectLocalMCP(t, router, "/mcp")

	toolNames := listAllToolNames(t, session)
	if !contains(toolNames, "wiki_get_context") {
		t.Fatalf("wiki_get_context was not listed")
	}

	out := callToolStructured(t, session, "wiki_get_context", nil)
	if token := stringField(t, out, "contextToken"); token == "" {
		t.Fatalf("contextToken is empty in %#v", out)
	}
	for _, key := range []string{
		"changesSincePreviousContext",
		"contextHistory",
		"user",
		"config",
		"server",
		"syncStatus",
		"validation",
		"recentChanges",
		"activeSessions",
		"presenceStatus",
		"tree",
		"recommendedTools",
		"canonicalLinkExamples",
	} {
		if _, ok := out[key]; !ok {
			t.Fatalf("wiki_get_context missing %s in %#v", key, out)
		}
	}
	if sessions, ok := out["activeSessions"].([]any); !ok {
		t.Fatalf("activeSessions has type %T, want array", out["activeSessions"])
	} else if len(sessions) != 0 {
		t.Fatalf("activeSessions = %#v, want no sessions in fresh fixture", sessions)
	}
	presence := nestedMap(t, out, "presenceStatus")
	if presence["web"] == "" || presence["agentHooks"] == "" {
		t.Fatalf("presenceStatus = %#v, want web and agentHooks states", presence)
	}
	status := nestedMap(t, out, "syncStatus")
	if _, ok := status["enabled"]; !ok {
		t.Fatalf("syncStatus = %#v, want camelCase enabled field", status)
	}
	if _, ok := status["Enabled"]; ok {
		t.Fatalf("syncStatus = %#v, did not expect exported Go struct field names", status)
	}
	recommended, ok := out["recommendedTools"].([]any)
	if !ok {
		t.Fatalf("recommendedTools has type %T, want array", out["recommendedTools"])
	}
	for _, value := range recommended {
		name, ok := value.(string)
		if !ok {
			t.Fatalf("recommendedTools contains non-string %T: %#v", value, recommended)
		}
		if !contains(toolNames, name) {
			t.Fatalf("recommendedTools = %#v, recommended unregistered tool %q", recommended, name)
		}
	}
	tree := nestedMap(t, out, "tree")
	if tree["id"] == "" || tree["children"] == nil {
		t.Fatalf("tree = %#v, want compact root node", tree)
	}
	examples, ok := out["canonicalLinkExamples"].([]any)
	if !ok || len(examples) == 0 {
		t.Fatalf("canonicalLinkExamples = %#v, want non-empty array", out["canonicalLinkExamples"])
	}
	exampleText := ""
	for _, example := range examples {
		text, ok := example.(string)
		if !ok {
			t.Fatalf("canonicalLinkExamples contains non-string %T: %#v", example, examples)
		}
		exampleText += "\n" + text
	}
	if !strings.Contains(exampleText, "](/docs/guide.md)") {
		t.Fatalf("canonicalLinkExamples = %q, want .md page link example", exampleText)
	}
	if !strings.Contains(exampleText, "](/docs)") {
		t.Fatalf("canonicalLinkExamples = %q, want extensionless section link example", exampleText)
	}
}

func TestLocalMCPGetContext_RecommendsRefreshInFederatedRuntime(t *testing.T) {
	w := newLocalMCPTestWiki(t, false)
	router := newLocalMCPTestRouter(w, httpinternal.RouterOptions{
		AuthDisabled:            true,
		PublicAccess:            true,
		AllowInsecure:           true,
		MaxAssetUploadSizeBytes: assets.DefaultMaxUploadSizeBytes,
		MCPEnabled:              true,
		MCPToolListPageSize:     200,
	})
	session := connectLocalMCP(t, router, "/mcp")

	out := callToolStructured(t, session, "wiki_get_context", map[string]any{"syncMode": "none"})
	server := nestedMap(t, out, "server")
	if !arrayContainsString(server["tools"], "wiki_refresh") {
		t.Fatalf("server.tools = %#v, want wiki_refresh in federated runtime", server["tools"])
	}
	recommended := arrayField(t, out, "recommendedTools")
	if !arrayContainsString(recommended, "wiki_refresh") {
		t.Fatalf("recommendedTools = %#v, want wiki_refresh in federated runtime", recommended)
	}
}

func TestLocalMCPGetContext_OmittedSinceTokenUsesSessionCheckpointOnly(t *testing.T) {
	w := newLocalMCPTestWikiWithOptions(t, wiki.WikiOptions{
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
	sessionA := connectLocalMCP(t, router, "/mcp")
	sessionB := connectLocalMCP(t, router, "/mcp")

	firstA := callToolStructured(t, sessionA, "wiki_get_context", map[string]any{"syncMode": "none"})
	firstAToken := stringField(t, firstA, "contextToken")

	postHTTPJSON(t, router, "/api/pages", map[string]any{
		"title": "Omitted Token Delta",
		"slug":  "omitted-token-delta",
		"kind":  "page",
	}, http.StatusCreated)

	secondA := callToolStructured(t, sessionA, "wiki_get_context", map[string]any{"syncMode": "none"})
	assertContextHistoryOpaque(t, secondA)
	if secondA["previousContextToken"] != firstAToken {
		t.Fatalf("session A previousContextToken = %#v, want first token %q", secondA["previousContextToken"], firstAToken)
	}
	pathsA := changedPathsFromContext(t, secondA)
	if !pathsA["omitted-token-delta.md"] {
		t.Fatalf("session A changesSincePreviousContext paths = %#v, missing omitted-token-delta.md", pathsA)
	}

	firstB := callToolStructured(t, sessionB, "wiki_get_context", map[string]any{"syncMode": "none"})
	if firstB["previousContextToken"] != "" {
		t.Fatalf("session B previousContextToken = %#v, want empty first-session checkpoint", firstB["previousContextToken"])
	}
	if changes := arrayField(t, firstB, "changesSincePreviousContext"); len(changes) != 0 {
		t.Fatalf("session B changesSincePreviousContext = %#v, want no inherited session A delta", changes)
	}
	if history := arrayField(t, firstB, "contextHistory"); len(history) != 1 {
		t.Fatalf("session B contextHistory len = %d, want isolated first checkpoint", len(history))
	}
}

func TestLocalMCPGetContext_UnknownExplicitSinceTokenDoesNotFallbackToPreviousContext(t *testing.T) {
	w := newLocalMCPTestWikiWithOptions(t, wiki.WikiOptions{
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
	session := connectLocalMCP(t, router, "/mcp")

	first := callToolStructured(t, session, "wiki_get_context", map[string]any{"syncMode": "none"})
	firstToken := stringField(t, first, "contextToken")
	postHTTPJSON(t, router, "/api/pages", map[string]any{
		"title": "Unknown Token Delta",
		"slug":  "unknown-token-delta",
		"kind":  "page",
	}, http.StatusCreated)

	second := callToolStructured(t, session, "wiki_get_context", map[string]any{
		"syncMode":   "none",
		"sinceToken": "not-a-token",
	})
	if second["previousContextToken"] != "" {
		t.Fatalf("previousContextToken = %#v, want no implicit fallback from explicit unknown token", second["previousContextToken"])
	}
	if changes := arrayField(t, second, "changesSincePreviousContext"); len(changes) != 0 {
		t.Fatalf("changesSincePreviousContext = %#v, want no implicit fallback delta from explicit unknown token", changes)
	}
	warnings := arrayField(t, second, "warnings")
	if !arrayContainsString(warnings, "unknown sinceToken; returned current context") {
		t.Fatalf("warnings = %#v, want unknown-token warning", warnings)
	}
	if stringField(t, first, "contextToken") != firstToken {
		t.Fatalf("first context token changed unexpectedly")
	}
}

func TestLocalMCPGetContext_ChangesSinceTokenIsNotClippedByRecentChangesLimit(t *testing.T) {
	w := newLocalMCPTestWikiWithOptions(t, wiki.WikiOptions{
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
	session := connectLocalMCP(t, router, "/mcp")

	first := callToolStructured(t, session, "wiki_get_context", map[string]any{
		"recentChangesLimit": float64(1),
		"syncMode":           "none",
	})
	token := stringField(t, first, "contextToken")
	for i := 0; i < 3; i++ {
		slug := fmt.Sprintf("delta-page-%d", i)
		callToolStructured(t, session, "wiki_create_page", map[string]any{
			"title": fmt.Sprintf("Delta Page %d", i),
			"slug":  slug,
			"kind":  "page",
		})
	}

	second := callToolStructured(t, session, "wiki_get_context", map[string]any{
		"sinceToken":         token,
		"recentChangesLimit": float64(1),
		"syncMode":           "none",
	})
	recentChanges := arrayField(t, second, "recentChanges")
	if len(recentChanges) != 1 {
		t.Fatalf("recentChanges len = %d, want display limit 1 in %#v", len(recentChanges), second["recentChanges"])
	}
	changes := arrayField(t, second, "changesSincePreviousContext")
	if len(changes) < 3 {
		t.Fatalf("changesSincePreviousContext len = %d, want at least 3 changes after checkpoint: %#v", len(changes), second["changesSincePreviousContext"])
	}
	changedPaths := map[string]bool{}
	for _, raw := range changes {
		change, ok := raw.(map[string]any)
		if !ok {
			t.Fatalf("change has type %T: %#v", raw, raw)
		}
		for _, value := range arrayFieldFromMap(t, change, "changedPaths") {
			path, ok := value.(string)
			if !ok {
				t.Fatalf("changed path has type %T: %#v", value, value)
			}
			changedPaths[path] = true
		}
	}
	for i := 0; i < 3; i++ {
		want := fmt.Sprintf("delta-page-%d.md", i)
		if !changedPaths[want] {
			t.Fatalf("changesSincePreviousContext paths = %#v, missing %s", changedPaths, want)
		}
	}
	if warnings, ok := second["warnings"].([]any); ok && len(warnings) > 0 {
		t.Fatalf("warnings = %#v, want no truncation warning when checkpoint is reachable", warnings)
	}

	tokenB := stringField(t, second, "contextToken")
	callToolStructured(t, session, "wiki_create_page", map[string]any{
		"title": "Delta Page After B",
		"slug":  "delta-page-after-b",
		"kind":  "page",
	})
	third := callToolStructured(t, session, "wiki_get_context", map[string]any{
		"sinceToken":         tokenB,
		"recentChangesLimit": float64(1),
		"syncMode":           "none",
	})
	changesAfterB := arrayField(t, third, "changesSincePreviousContext")
	changedPathsAfterB := map[string]bool{}
	for _, raw := range changesAfterB {
		change, ok := raw.(map[string]any)
		if !ok {
			t.Fatalf("change after B has type %T: %#v", raw, raw)
		}
		for _, value := range arrayFieldFromMap(t, change, "changedPaths") {
			path, ok := value.(string)
			if !ok {
				t.Fatalf("changed path after B has type %T: %#v", value, value)
			}
			changedPathsAfterB[path] = true
		}
	}
	if !changedPathsAfterB["delta-page-after-b.md"] {
		t.Fatalf("changesSincePreviousContext after token B paths = %#v, missing delta-page-after-b.md", changedPathsAfterB)
	}
	for i := 0; i < 3; i++ {
		oldPath := fmt.Sprintf("delta-page-%d.md", i)
		if changedPathsAfterB[oldPath] {
			t.Fatalf("changesSincePreviousContext after token B paths = %#v, did not expect old path %s", changedPathsAfterB, oldPath)
		}
	}
}

func TestLocalMCPGetContext_IncludesWebHeartbeatPresence(t *testing.T) {
	w := newLocalMCPTestWiki(t, false)
	router := newLocalMCPTestRouter(w, httpinternal.RouterOptions{
		AuthDisabled:            true,
		PublicAccess:            true,
		AllowInsecure:           true,
		MaxAssetUploadSizeBytes: assets.DefaultMaxUploadSizeBytes,
		EnableWorkspaceSync:     true,
		MCPEnabled:              true,
		MCPToolListPageSize:     200,
	})
	session := connectLocalMCP(t, router, "/mcp")

	page := nestedMap(t, callToolStructured(t, session, "wiki_create_page", map[string]any{
		"title": "Docs API",
		"slug":  "docs-api",
		"kind":  "page",
	}), "page")
	postHTTPJSON(t, router, "/api/presence/heartbeat", map[string]any{
		"sessionId": "tab-web-1",
		"mode":      "edit",
		"pageId":    stringField(t, page, "id"),
		"path":      "/docs-api",
		"dirty":     true,
	}, http.StatusOK)

	out := callToolStructured(t, session, "wiki_get_context", nil)
	presence := nestedMap(t, out, "presenceStatus")
	if presence["web"] != "enabled" {
		t.Fatalf("presenceStatus.web = %v, want enabled in %#v", presence["web"], presence)
	}
	sessions, ok := out["activeSessions"].([]any)
	if !ok || len(sessions) != 1 {
		t.Fatalf("activeSessions = %#v, want one web session", out["activeSessions"])
	}
	webSession, ok := sessions[0].(map[string]any)
	if !ok {
		t.Fatalf("activeSessions[0] = %T %#v, want object", sessions[0], sessions[0])
	}
	if webSession["type"] != "web" || webSession["sessionId"] != "tab-web-1" || webSession["mode"] != "edit" || webSession["dirty"] != true {
		t.Fatalf("web session = %#v, want typed edit dirty session", webSession)
	}
	user := nestedMap(t, webSession, "user")
	if user["id"] != "public-editor" || user["role"] != "editor" {
		t.Fatalf("web session user = %#v, want public editor", user)
	}
	if _, leaked := user["email"]; leaked {
		t.Fatalf("web session user = %#v, did not expect email for editor MCP caller", user)
	}
	pageOut := nestedMap(t, webSession, "page")
	if pageOut["id"] != stringField(t, page, "id") || pageOut["path"] != "/docs-api" || pageOut["title"] != "Docs API" {
		t.Fatalf("web session page = %#v, want resolved page context", pageOut)
	}
	if webSession["lastSeenAt"] == "" {
		t.Fatalf("web session = %#v, want lastSeenAt", webSession)
	}
}

func TestLocalMCPGetContext_MergesAgentHookPresence(t *testing.T) {
	w := newLocalMCPTestWiki(t, false)
	agentPresence := projectdaemon.NewAgentPresenceRegistry(time.Minute, nil)
	sessionHash := "sha256:" + strings.Repeat("a", 64)
	agentPresence.Record(agenthooks.Event{
		Provider:      agenthooks.ProviderCodex,
		SessionIDHash: sessionHash,
		EventName:     "SessionStart",
		Model:         "gpt-5",
		Source:        "hook",
		SeenAt:        time.Date(2026, 6, 8, 12, 0, 0, 0, time.UTC),
	})
	agentPresence.Record(agenthooks.Event{
		Provider:      agenthooks.ProviderCodex,
		SessionIDHash: sessionHash,
		EventName:     "SubagentStart",
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
	session := connectLocalMCP(t, router, "/mcp")

	out := callToolStructured(t, session, "wiki_get_context", nil)
	presence := nestedMap(t, out, "presenceStatus")
	if presence["agentHooks"] != "enabled" {
		t.Fatalf("presenceStatus.agentHooks = %v, want enabled in %#v", presence["agentHooks"], presence)
	}
	sessions, ok := out["activeSessions"].([]any)
	if !ok || len(sessions) != 1 {
		t.Fatalf("activeSessions = %#v, want one agent session", out["activeSessions"])
	}
	agentSession, ok := sessions[0].(map[string]any)
	if !ok {
		t.Fatalf("activeSessions[0] = %T %#v, want object", sessions[0], sessions[0])
	}
	if agentSession["type"] != "agent" || agentSession["sessionId"] != sessionHash || agentSession["provider"] != "codex" {
		t.Fatalf("agent session = %#v, want sanitized codex session", agentSession)
	}
	if agentSession["model"] != "gpt-5" || agentSession["source"] != "hook" || agentSession["lastEvent"] != "SubagentStart" || agentSession["activeSubagents"] != float64(1) {
		t.Fatalf("agent session = %#v, want hook metadata", agentSession)
	}
}

func TestPresenceHeartbeatRequiresAuthWhenAuthEnabled(t *testing.T) {
	w := newLocalMCPTestWiki(t, false)
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
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("POST /api/presence/heartbeat without auth = %d, want 401: %s", rec.Code, rec.Body.String())
	}
}

func TestPresenceHeartbeatRouteRejectsMissingCSRFAndInvalidPayloadWithoutPoisoning(t *testing.T) {
	w := newLocalMCPTestWiki(t, false)
	router := newLocalMCPTestRouter(w, httpinternal.RouterOptions{
		AuthDisabled:            true,
		PublicAccess:            true,
		AllowInsecure:           true,
		MaxAssetUploadSizeBytes: assets.DefaultMaxUploadSizeBytes,
		MCPEnabled:              true,
		MCPToolListPageSize:     200,
	})
	session := connectLocalMCP(t, router, "/mcp")

	req := httptest.NewRequest(http.MethodPost, "/api/presence/heartbeat", strings.NewReader(`{"sessionId":"tab-no-csrf","mode":"view"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("POST /api/presence/heartbeat without CSRF = %d, want 403: %s", rec.Code, rec.Body.String())
	}

	postHTTPJSON(t, router, "/api/presence/heartbeat", map[string]any{
		"sessionId": "tab-valid",
		"mode":      "view",
	}, http.StatusOK)
	postHTTPJSON(t, router, "/api/presence/heartbeat", map[string]any{
		"sessionId": "tab-invalid",
		"mode":      "invalid",
	}, http.StatusBadRequest)
	postHTTPJSON(t, router, "/api/presence/heartbeat", map[string]any{
		"sessionId": "tab-missing-page",
		"mode":      "view",
		"path":      "/deleted-page",
	}, http.StatusOK)

	out := callToolStructured(t, session, "wiki_get_context", map[string]any{"syncMode": "none"})
	sessions, ok := out["activeSessions"].([]any)
	if !ok || len(sessions) != 2 {
		t.Fatalf("activeSessions = %#v, want valid plus missing-page sessions", out["activeSessions"])
	}
	if !arrayContainsObjectField(sessions, "sessionId", "tab-valid") {
		t.Fatalf("activeSessions = %#v, want tab-valid", sessions)
	}
	missingPageSession := objectWithField(t, sessions, "sessionId", "tab-missing-page")
	if _, exists := missingPageSession["page"]; exists {
		t.Fatalf("missing-page session = %#v, did not expect unresolved page details", missingPageSession)
	}
}

func TestLocalMCPRefresh_SyncsDirectMarkdownCreate(t *testing.T) {
	rootDir := filepath.Join(t.TempDir(), "content")
	w := newLocalMCPTestWikiWithOptions(t, wiki.WikiOptions{
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
	session := connectLocalMCP(t, router, "/mcp")

	if err := os.WriteFile(filepath.Join(rootDir, "direct.md"), []byte("---\nleafwiki_id: direct\nleafwiki_title: Direct\n---\n# Direct\n"), 0o644); err != nil {
		t.Fatalf("write direct markdown: %v", err)
	}

	out := callToolStructured(t, session, "wiki_refresh", nil)
	status := nestedMap(t, out, "syncStatus")
	if status["lastCommitHash"] == "" {
		t.Fatalf("wiki_refresh syncStatus = %#v, want lastCommitHash", status)
	}
	if _, ok := status["LastCommitHash"]; ok {
		t.Fatalf("wiki_refresh syncStatus = %#v, did not expect exported Go struct field names", status)
	}
	if _, ok := out["validation"]; !ok {
		t.Fatalf("wiki_refresh missing validation in %#v", out)
	}

	readBack := callToolStructured(t, session, "wiki_get_page_by_path", map[string]any{"path": "direct"})
	page := nestedMap(t, readBack, "page")
	if page["id"] != "direct" || page["title"] != "Direct" {
		t.Fatalf("readBack page = %#v, want synced Direct page", page)
	}

	contextOut := callToolStructured(t, session, "wiki_get_context", map[string]any{
		"syncMode":           "none",
		"recentChangesLimit": float64(1),
	})
	recent := arrayField(t, contextOut, "recentChanges")
	if len(recent) != 1 {
		t.Fatalf("recentChanges = %#v, want one refresh entry", recent)
	}
	change, ok := recent[0].(map[string]any)
	if !ok {
		t.Fatalf("recentChanges[0] has type %T: %#v", recent[0], recent[0])
	}
	if change["source"] != "mcp" {
		t.Fatalf("recent change source = %#v, want mcp in %#v", change["source"], change)
	}
	if change["reason"] != "explicit_refresh" {
		t.Fatalf("recent change reason = %#v, want explicit_refresh in %#v", change["reason"], change)
	}
	if change["commitId"] == "" || change["timestamp"] == "" || change["actor"] == "" {
		t.Fatalf("recent change provenance incomplete: %#v", change)
	}
	pageIDs := arrayFieldFromMap(t, change, "pageIds")
	if !arrayContainsString(pageIDs, "direct") {
		t.Fatalf("recent change pageIds = %#v, want direct", pageIDs)
	}
}

func TestLocalMCPValidateAndRefreshNormalizeWorkspaceRoutes(t *testing.T) {
	rootDir := filepath.Join(t.TempDir(), "content")
	w := newLocalMCPTestWikiWithOptions(t, wiki.WikiOptions{
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
	session := connectLocalMCP(t, router, "/mcp")

	if err := os.MkdirAll(filepath.Join(rootDir, "plans"), 0o755); err != nil {
		t.Fatalf("create plans dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(rootDir, "plans", "index.md"), []byte("---\nleafwiki_id: plans\nleafwiki_title: Plans\n---\n# Plans\n"), 0o644); err != nil {
		t.Fatalf("write plans index: %v", err)
	}
	if err := os.WriteFile(filepath.Join(rootDir, "plans", "agent_hooks.PLAN.md"), []byte("---\nleafwiki_id: agent-hooks-plan\nleafwiki_title: Agent Hooks Plan\n---\n# Agent Hooks Plan\n\ncontent\n"), 0o644); err != nil {
		t.Fatalf("write plan markdown: %v", err)
	}

	validateOut := callToolStructured(t, session, "wiki_validate_wiki", map[string]any{"includeWarnings": false})
	if ok, _ := validateOut["ok"].(bool); !ok {
		t.Fatalf("wiki_validate_wiki = %#v, want normalizable plan filename to validate", validateOut)
	}
	assertValidationIssueCodesAbsent(t, validateOut, []string{"invalid_slug"})

	refreshOut := callToolStructured(t, session, "wiki_refresh", map[string]any{"source": "filesystem"})
	validation := nestedMap(t, refreshOut, "validation")
	if ok, _ := validation["ok"].(bool); !ok {
		t.Fatalf("wiki_refresh validation = %#v, want normalized route validation to agree", validation)
	}
	assertValidationIssueCodesAbsent(t, validation, []string{"invalid_slug"})

	readBack := callToolStructured(t, session, "wiki_get_page_by_path", map[string]any{"path": "plans/agent-hooks-plan", "kind": "page"})
	page := nestedMap(t, readBack, "page")
	if page["id"] != "agent-hooks-plan" || page["title"] != "Agent Hooks Plan" {
		t.Fatalf("readBack page = %#v, want normalized plan page", page)
	}
}

func TestLocalMCPRefresh_ValidateFalseOmitsValidation(t *testing.T) {
	rootDir := filepath.Join(t.TempDir(), "content")
	w := newLocalMCPTestWikiWithOptions(t, wiki.WikiOptions{
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
	session := connectLocalMCP(t, router, "/mcp")

	if err := os.WriteFile(filepath.Join(rootDir, "skip-validation.md"), []byte("---\nleafwiki_id: skip-validation\nleafwiki_title: Skip Validation\n---\n# Skip Validation\n"), 0o644); err != nil {
		t.Fatalf("write markdown: %v", err)
	}

	out := callToolStructured(t, session, "wiki_refresh", map[string]any{"validate": false})
	if _, exists := out["validation"]; exists {
		t.Fatalf("wiki_refresh validate=false output = %#v, did not expect validation", out)
	}
}

func TestLocalMCPRefresh_InvalidWorkspaceReturnsValidationAndKeepsMCPAvailable(t *testing.T) {
	rootDir := filepath.Join(t.TempDir(), "content")
	w := newLocalMCPTestWikiWithOptions(t, wiki.WikiOptions{
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
	session := connectLocalMCP(t, router, "/mcp")

	if err := os.WriteFile(filepath.Join(rootDir, "a.md"), []byte("---\nleafwiki_id: duplicate\nleafwiki_title: A\n---\n# A\n"), 0o644); err != nil {
		t.Fatalf("write a.md: %v", err)
	}
	if err := os.WriteFile(filepath.Join(rootDir, "b.md"), []byte("---\nleafwiki_id: duplicate\nleafwiki_title: B\n---\n# B\n"), 0o644); err != nil {
		t.Fatalf("write b.md: %v", err)
	}

	out := callToolStructured(t, session, "wiki_refresh", map[string]any{"source": "filesystem"})
	validation := nestedMap(t, out, "validation")
	summary := nestedMap(t, validation, "summary")
	if errors, ok := summary["errors"].(float64); !ok || errors == 0 {
		t.Fatalf("wiki_refresh validation summary = %#v, want validation errors for duplicate IDs", summary)
	}
	assertValidationIssueCodes(t, validation, []string{"workspace_sync_error"})

	currentUser := callToolStructured(t, session, "wiki_get_current_user", nil)
	user := nestedMap(t, currentUser, "user")
	if user["username"] == "" {
		t.Fatalf("wiki_get_current_user after failed refresh = %#v, want MCP still available", currentUser)
	}
}

func TestLocalMCPRefresh_RecordsExplicitRefreshReasonAndCapsContextPaths(t *testing.T) {
	rootDir := filepath.Join(t.TempDir(), "content")
	w := newLocalMCPTestWikiWithOptions(t, wiki.WikiOptions{
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
	session := connectLocalMCP(t, router, "/mcp")

	for i := 0; i < 25; i++ {
		slug := fmt.Sprintf("bulk-refresh-%02d", i)
		if err := os.WriteFile(filepath.Join(rootDir, slug+".md"), []byte("---\nleafwiki_id: "+slug+"\nleafwiki_title: "+slug+"\n---\n# "+slug+"\n"), 0o644); err != nil {
			t.Fatalf("write %s: %v", slug, err)
		}
	}

	callToolStructured(t, session, "wiki_refresh", map[string]any{"source": "filesystem"})
	contextOut := callToolStructured(t, session, "wiki_get_context", map[string]any{
		"syncMode":           "none",
		"recentChangesLimit": float64(1),
	})
	recent := arrayField(t, contextOut, "recentChanges")
	if len(recent) != 1 {
		t.Fatalf("recentChanges = %#v, want one entry", recent)
	}
	change, ok := recent[0].(map[string]any)
	if !ok {
		t.Fatalf("recentChanges[0] has type %T: %#v", recent[0], recent[0])
	}
	if change["reason"] != "explicit_refresh" {
		t.Fatalf("recent change reason = %#v, want explicit_refresh in %#v", change["reason"], change)
	}
	if change["source"] != "filesystem" {
		t.Fatalf("recent change source = %#v, want filesystem in %#v", change["source"], change)
	}
	if got := int(change["changedCount"].(float64)); got < 25 {
		t.Fatalf("changedCount = %d, want at least 25 in %#v", got, change)
	}
	if paths := arrayFieldFromMap(t, change, "changedPaths"); len(paths) != 20 {
		t.Fatalf("changedPaths len = %d, want cap 20 in %#v", len(paths), paths)
	}
}

func TestLocalMCPGetSubtree_ReturnsPathRootWithBreadcrumbs(t *testing.T) {
	w := newLocalMCPTestWiki(t, false)
	router := newLocalMCPTestRouter(w, httpinternal.RouterOptions{
		AuthDisabled:            true,
		PublicAccess:            true,
		AllowInsecure:           true,
		MaxAssetUploadSizeBytes: assets.DefaultMaxUploadSizeBytes,
		MCPEnabled:              true,
		MCPToolListPageSize:     200,
	})
	session := connectLocalMCP(t, router, "/mcp")

	parentPage := postHTTPJSON(t, router, "/api/pages", map[string]any{
		"title": "Docs",
		"slug":  "docs",
		"kind":  "section",
	}, http.StatusCreated)
	childPage := postHTTPJSON(t, router, "/api/pages", map[string]any{
		"parentId": stringField(t, parentPage, "id"),
		"title":    "Reference",
		"slug":     "reference",
		"kind":     "page",
	}, http.StatusCreated)
	targetPage := postHTTPJSON(t, router, "/api/pages", map[string]any{
		"title": "Target",
		"slug":  "target",
		"kind":  "page",
	}, http.StatusCreated)
	_ = targetPage
	callToolStructured(t, session, "wiki_update_page", map[string]any{
		"id":      stringField(t, childPage, "id"),
		"version": stringField(t, childPage, "version"),
		"title":   "Reference",
		"slug":    "reference",
		"content": "Reference content with [Target](/target.md).",
	})

	out := callToolStructured(t, session, "wiki_get_subtree", map[string]any{
		"path":  "/docs",
		"depth": float64(1),
	})
	root := nestedMap(t, out, "root")
	if root["path"] != "docs" || root["title"] != "Docs" {
		t.Fatalf("root = %#v, want Docs subtree", root)
	}
	children, ok := root["children"].([]any)
	if !ok || len(children) != 1 {
		t.Fatalf("root children = %#v, want one Reference child", root["children"])
	}
	breadcrumbs, ok := out["breadcrumbs"].([]any)
	if !ok || len(breadcrumbs) < 2 {
		t.Fatalf("breadcrumbs = %#v, want root and docs", out["breadcrumbs"])
	}
	if out["depth"] != float64(1) || out["truncated"] != false {
		t.Fatalf("depth/truncated = %#v/%#v, want 1/false", out["depth"], out["truncated"])
	}

	expanded := callToolStructured(t, session, "wiki_get_subtree", map[string]any{
		"path":                  "docs",
		"depth":                 float64(1),
		"includeMetadata":       false,
		"includeLinkCounts":     true,
		"includeContentPreview": true,
	})
	expandedRoot := nestedMap(t, expanded, "root")
	if _, exists := expandedRoot["metadata"]; exists {
		t.Fatalf("expanded root = %#v, did not expect metadata when includeMetadata=false", expandedRoot)
	}
	expandedChildren := expandedRoot["children"].([]any)
	expandedChild := expandedChildren[0].(map[string]any)
	if _, exists := expandedChild["metadata"]; exists {
		t.Fatalf("expanded child = %#v, did not expect metadata when includeMetadata=false", expandedChild)
	}
	if expandedChild["contentPreview"] == "" {
		t.Fatalf("expanded child = %#v, want contentPreview", expandedChild)
	}
	linkCounts := nestedMap(t, expandedChild, "linkCounts")
	if linkCounts["outgoings"] != float64(1) {
		t.Fatalf("linkCounts = %#v, want one outgoing link", linkCounts)
	}

	rootOut := callToolStructured(t, session, "wiki_get_subtree", nil)
	wikiRoot := nestedMap(t, rootOut, "root")
	if wikiRoot["slug"] != "root" || wikiRoot["path"] != "" {
		t.Fatalf("root subtree = %#v, want wiki root", wikiRoot)
	}
	byID := callToolStructured(t, session, "wiki_get_subtree", map[string]any{
		"pageId": stringField(t, parentPage, "id"),
		"depth":  float64(0),
	})
	byIDRoot := nestedMap(t, byID, "root")
	if byIDRoot["id"] != stringField(t, parentPage, "id") {
		t.Fatalf("pageId subtree root = %#v, want Docs id", byIDRoot)
	}
	if errText := callToolError(t, session, "wiki_get_subtree", map[string]any{
		"pageId": stringField(t, parentPage, "id"),
		"path":   "docs",
	}); !strings.Contains(errText, "pageId and path cannot both be supplied") {
		t.Fatalf("both pageId/path error = %q", errText)
	}
	if errText := callToolError(t, session, "wiki_get_subtree", map[string]any{"path": "missing-subtree"}); !strings.Contains(strings.ToLower(errText), "not found") {
		t.Fatalf("missing subtree error = %q, want not found detail", errText)
	}
	negativeDepthErr := callToolError(t, session, "wiki_get_subtree", map[string]any{
		"path":  "docs",
		"depth": float64(-1),
	})
	assertErrorContainsAny(t, "wiki_get_subtree negative depth", negativeDepthErr, "depth must be zero or greater")
	hugeDepth := callToolStructured(t, session, "wiki_get_subtree", map[string]any{
		"path":  "docs",
		"depth": float64(999),
	})
	if hugeDepth["depth"] != float64(4) {
		t.Fatalf("huge subtree depth = %#v, want clamped depth 4", hugeDepth["depth"])
	}
}

func TestLocalMCPValidateWikiUsesCurrentFilesystemSnapshot(t *testing.T) {
	rootDir := filepath.Join(t.TempDir(), "content")
	w := newLocalMCPTestWikiWithOptions(t, wiki.WikiOptions{
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
	session := connectLocalMCP(t, router, "/mcp")

	page := postHTTPJSON(t, router, "/api/pages", map[string]any{
		"title":   "Link Source",
		"slug":    "link-source",
		"kind":    "page",
		"content": "[Missing](/missing-target)",
	}, http.StatusCreated)
	callToolStructured(t, session, "wiki_update_page", map[string]any{
		"id":      stringField(t, page, "id"),
		"version": stringField(t, page, "version"),
		"title":   "Link Source",
		"slug":    "link-source",
		"content": "[Missing](/missing-target)",
	})
	if err := os.WriteFile(filepath.Join(rootDir, "link-source.md"), []byte("---\nleafwiki_id: "+stringField(t, page, "id")+"\nleafwiki_title: Link Source\n---\n# Link Source\n\n[Fixed](/fixed-target.md)\n"), 0o644); err != nil {
		t.Fatalf("write fixed source: %v", err)
	}
	if err := os.WriteFile(filepath.Join(rootDir, "fixed-target.md"), []byte("---\nleafwiki_id: fixed-target\nleafwiki_title: Fixed Target\n---\n# Fixed Target\n"), 0o644); err != nil {
		t.Fatalf("write fixed target: %v", err)
	}

	out := callToolStructured(t, session, "wiki_validate_wiki", nil)
	validation := out
	if ok, _ := validation["ok"].(bool); !ok {
		t.Fatalf("wiki_validate_wiki = %#v, want current filesystem snapshot to validate without stale loaded-tree link", validation)
	}
}

func TestLocalMCPValidateWikiDoesNotResolveLinksThroughStaleLoadedTree(t *testing.T) {
	rootDir := filepath.Join(t.TempDir(), "content")
	w := newLocalMCPTestWikiWithOptions(t, wiki.WikiOptions{
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
	session := connectLocalMCP(t, router, "/mcp")

	postHTTPJSON(t, router, "/api/pages", map[string]any{
		"title": "Stale Target",
		"slug":  "stale-target",
		"kind":  "page",
	}, http.StatusCreated)
	if err := os.WriteFile(filepath.Join(rootDir, "source.md"), []byte("---\nleafwiki_id: source\nleafwiki_title: Source\n---\n# Source\n\n[Stale](/stale-target)\n"), 0o644); err != nil {
		t.Fatalf("write source: %v", err)
	}
	if err := os.Remove(filepath.Join(rootDir, "stale-target.md")); err != nil {
		t.Fatalf("remove stale target from filesystem: %v", err)
	}

	out := callToolStructured(t, session, "wiki_validate_wiki", nil)
	if ok, _ := out["ok"].(bool); ok {
		t.Fatalf("wiki_validate_wiki = %#v, want broken link when target is absent from filesystem snapshot", out)
	}
	validation := out
	assertValidationIssueCodes(t, validation, []string{"broken_link"})
}

func TestLocalMCPValidationTools_ValidateStoredAndProposedContent(t *testing.T) {
	w := newLocalMCPTestWiki(t, false)
	router := newLocalMCPTestRouter(w, httpinternal.RouterOptions{
		AuthDisabled:            true,
		PublicAccess:            true,
		AllowInsecure:           true,
		MaxAssetUploadSizeBytes: assets.DefaultMaxUploadSizeBytes,
		MCPEnabled:              true,
		MCPToolListPageSize:     200,
	})
	session := connectLocalMCP(t, router, "/mcp")

	created := nestedMap(t, callToolStructured(t, session, "wiki_create_page", map[string]any{
		"title": "Valid Page",
		"slug":  "valid-page",
		"kind":  "page",
	}), "page")

	pageValidation := callToolStructured(t, session, "wiki_validate_page", map[string]any{
		"pageId": stringField(t, created, "id"),
	})
	if pageValidation["ok"] != true {
		t.Fatalf("wiki_validate_page = %#v, want ok", pageValidation)
	}
	issues, ok := pageValidation["issues"].([]any)
	if !ok || len(issues) != 0 {
		t.Fatalf("wiki_validate_page issues = %#v, want empty", pageValidation["issues"])
	}

	ambiguousErr := callToolError(t, session, "wiki_validate_page", map[string]any{
		"pageId": stringField(t, created, "id"),
		"path":   "valid-page",
	})
	assertErrorContainsAny(t, "wiki_validate_page ambiguous input", ambiguousErr, "pageId and path cannot both be supplied")
	missingTargetErr := callToolError(t, session, "wiki_validate_page", map[string]any{})
	assertErrorContainsAny(t, "wiki_validate_page missing target", missingTargetErr, "pageId or path is required")

	proposed := callToolStructured(t, session, "wiki_validate_content", map[string]any{
		"path":    "draft",
		"content": "---\nleafwiki_id: draft\nleafwiki_title: Draft\n---\n# Draft\n",
	})
	if proposed["ok"] != true {
		t.Fatalf("wiki_validate_content valid = %#v, want ok", proposed)
	}
	missingDraft := callToolError(t, session, "wiki_get_page_by_path", map[string]any{"path": "draft"})
	assertErrorContainsAny(t, "wiki_validate_content does not write", missingDraft, "not found", "page_not_found")

	canonicalPageLink := callToolStructured(t, session, "wiki_validate_content", map[string]any{
		"path":    "draft-canonical",
		"content": "[Valid](/valid-page.md)\n",
	})
	if canonicalPageLink["ok"] != true {
		t.Fatalf("wiki_validate_content canonical page link = %#v, want ok", canonicalPageLink)
	}
	canonicalPagePath := callToolStructured(t, session, "wiki_validate_content", map[string]any{
		"path":    "/valid-page.md",
		"content": fmt.Sprintf("---\nleafwiki_id: %s\nleafwiki_title: Valid Page\n---\n[Valid](/valid-page.md)\n", stringField(t, created, "id")),
	})
	if canonicalPagePath["ok"] != true {
		t.Fatalf("wiki_validate_content canonical page path = %#v, want ok", canonicalPagePath)
	}
	rootSectionLink := callToolStructured(t, session, "wiki_validate_content", map[string]any{
		"path":    "draft-root-link",
		"content": "[Root](/)\n",
	})
	if rootSectionLink["ok"] != true {
		t.Fatalf("wiki_validate_content root section link = %#v, want ok", rootSectionLink)
	}
	sectionTarget := nestedMap(t, callToolStructured(t, session, "wiki_create_page", map[string]any{
		"title": "Validation Section",
		"slug":  "validation-section",
		"kind":  "section",
	}), "page")
	sectionChildTarget := nestedMap(t, callToolStructured(t, session, "wiki_create_page", map[string]any{
		"parentId": stringField(t, sectionTarget, "id"),
		"title":    "Validation Section Child",
		"slug":     "child",
		"kind":     "page",
	}), "page")
	relativeSectionLink := callToolStructured(t, session, "wiki_validate_content", map[string]any{
		"existingPageId": stringField(t, sectionTarget, "id"),
		"path":           "validation-section",
		"content":        "[Child](./child.md)\n",
	})
	if relativeSectionLink["ok"] != true {
		t.Fatalf("wiki_validate_content section-relative child link = %#v, want ok", relativeSectionLink)
	}
	draftSectionLink := callToolStructured(t, session, "wiki_validate_content", map[string]any{
		"path":    "validation-section",
		"kind":    "section",
		"content": "[Child](./child.md)\n",
	})
	if draftSectionLink["ok"] != true {
		t.Fatalf("wiki_validate_content draft section-relative child link = %#v, want ok", draftSectionLink)
	}
	omittedKindExistingSectionLink := callToolStructured(t, session, "wiki_validate_content", map[string]any{
		"path":    "validation-section",
		"content": "[Child](./child.md)\n",
	})
	if omittedKindExistingSectionLink["ok"] != true {
		t.Fatalf("wiki_validate_content omitted-kind existing section link = %#v, want ok", omittedKindExistingSectionLink)
	}
	pageTwin := nestedMap(t, callToolStructured(t, session, "wiki_create_page", map[string]any{
		"title": "Validation Twin Page",
		"slug":  "validation-twin",
		"kind":  "page",
	}), "page")
	sectionTwin := nestedMap(t, callToolStructured(t, session, "wiki_create_page", map[string]any{
		"title": "Validation Twin Section",
		"slug":  "validation-twin",
		"kind":  "section",
	}), "page")
	sectionTwinChild := nestedMap(t, callToolStructured(t, session, "wiki_create_page", map[string]any{
		"parentId": stringField(t, sectionTwin, "id"),
		"title":    "Validation Twin Child",
		"slug":     "child",
		"kind":     "page",
	}), "page")
	omittedKindExistingSectionTwinLink := callToolStructured(t, session, "wiki_validate_content", map[string]any{
		"path":    "validation-twin",
		"content": "[Child](./child.md)\n",
	})
	if omittedKindExistingSectionTwinLink["ok"] != true {
		t.Fatalf("wiki_validate_content omitted-kind same-basename section link = %#v, want ok", omittedKindExistingSectionTwinLink)
	}
	_ = pageTwin
	_ = sectionTwinChild
	sectionMdLink := callToolStructured(t, session, "wiki_validate_content", map[string]any{
		"path":    "draft-section-md",
		"content": "[Section as md](/validation-section.md)\n",
	})
	if sectionMdLink["ok"] != false {
		t.Fatalf("wiki_validate_content section .md link = %#v, want broken link", sectionMdLink)
	}
	assertValidationIssueCodes(t, sectionMdLink, []string{"broken_link"})
	_ = sectionChildTarget

	invalid := callToolStructured(t, session, "wiki_validate_content", map[string]any{
		"path":    "broken",
		"content": "---\nleafwiki_title: [unterminated\n---\n# Broken\n",
	})
	if invalid["ok"] != false {
		t.Fatalf("wiki_validate_content invalid = %#v, want not ok", invalid)
	}
	invalidIssues, ok := invalid["issues"].([]any)
	if !ok || len(invalidIssues) == 0 {
		t.Fatalf("invalid issues = %#v, want parse issue", invalid["issues"])
	}

	semantic := callToolStructured(t, session, "wiki_validate_content", map[string]any{
		"path": "draft-dupe",
		"content": "---\nleafwiki_id: " + stringField(t, created, "id") + "\nleafwiki_private: true\n---\n" +
			"[Missing](/does-not-exist)\n[Missing asset](missing.png)\n",
	})
	if semantic["ok"] != false {
		t.Fatalf("semantic validation = %#v, want not ok", semantic)
	}
	assertValidationIssueCodes(t, semantic, []string{"duplicate_leafwiki_id", "reserved_metadata", "broken_link", "missing_asset"})

	assetOwner := nestedMap(t, callToolStructured(t, session, "wiki_create_page", map[string]any{
		"title": "Asset Owner",
		"slug":  "asset-owner",
		"kind":  "page",
	}), "page")
	assetBorrower := nestedMap(t, callToolStructured(t, session, "wiki_create_page", map[string]any{
		"title": "Asset Borrower",
		"slug":  "asset-borrower",
		"kind":  "page",
	}), "page")
	callToolStructured(t, session, "wiki_upload_asset", map[string]any{
		"pageId":        stringField(t, assetOwner, "id"),
		"filename":      "logo.png",
		"contentBase64": base64.StdEncoding.EncodeToString([]byte("owner logo")),
	})
	wrongPageAsset := callToolStructured(t, session, "wiki_validate_content", map[string]any{
		"existingPageId": stringField(t, assetOwner, "id"),
		"path":           "asset-owner",
		"content":        "# Asset Owner\n\n![Wrong page asset](/assets/" + stringField(t, assetBorrower, "id") + "/logo.png)\n",
	})
	if wrongPageAsset["ok"] != false {
		t.Fatalf("wrong-page asset validation = %#v, want not ok", wrongPageAsset)
	}
	assertValidationIssueCodes(t, wrongPageAsset, []string{"missing_asset"})

	pathConflict := callToolStructured(t, session, "wiki_validate_content", map[string]any{
		"path":    "valid-page",
		"content": "---\nleafwiki_id: draft-conflict\nleafwiki_title: Draft Conflict\n---\n# Draft\n",
	})
	assertValidationIssueCodes(t, pathConflict, []string{"path_conflict"})
}

func TestLocalMCPPathToolsResolveCanonicalSameBasenameTwins(t *testing.T) {
	w := newLocalMCPTestWikiWithOptions(t, wiki.WikiOptions{
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
	session := connectLocalMCP(t, router, "/mcp")

	rootDir := w.GetRootDir()
	if err := os.MkdirAll(filepath.Join(rootDir, "docs", "sync"), 0o755); err != nil {
		t.Fatalf("mkdir same-basename fixture: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(rootDir, "docs", "section-only"), 0o755); err != nil {
		t.Fatalf("mkdir section-only fixture: %v", err)
	}
	if err := os.WriteFile(filepath.Join(rootDir, "docs", "sync.md"), []byte(`---
leafwiki_id: mcp-sync-page
leafwiki_title: MCP Sync Page
---
# MCP Sync Page
`), 0o644); err != nil {
		t.Fatalf("write page twin: %v", err)
	}
	if err := os.WriteFile(filepath.Join(rootDir, "docs", "sync", "index.md"), []byte(`---
leafwiki_id: mcp-sync-section
leafwiki_title: MCP Sync Section
---
# MCP Sync Section
`), 0o644); err != nil {
		t.Fatalf("write section twin: %v", err)
	}
	if err := os.WriteFile(filepath.Join(rootDir, "docs", "section-only", "index.md"), []byte(`---
leafwiki_id: mcp-section-only
leafwiki_title: MCP Section Only
---
# MCP Section Only
`), 0o644); err != nil {
		t.Fatalf("write section-only fixture: %v", err)
	}
	callToolStructured(t, session, "wiki_refresh", map[string]any{"source": "filesystem"})

	pageByMarkdownPath := nestedMap(t, callToolStructured(t, session, "wiki_get_page_by_path", map[string]any{
		"path": "/docs/sync.md",
	}), "page")
	if stringField(t, pageByMarkdownPath, "id") != "mcp-sync-page" || stringField(t, pageByMarkdownPath, "kind") != "page" {
		t.Fatalf("wiki_get_page_by_path /docs/sync.md = %#v, want page twin", pageByMarkdownPath)
	}

	sectionByCanonicalPath := nestedMap(t, callToolStructured(t, session, "wiki_get_page_by_path", map[string]any{
		"path": "/docs/sync",
	}), "page")
	if stringField(t, sectionByCanonicalPath, "id") != "mcp-sync-section" || stringField(t, sectionByCanonicalPath, "kind") != "section" {
		t.Fatalf("wiki_get_page_by_path /docs/sync = %#v, want section twin", sectionByCanonicalPath)
	}

	pageSubtree := nestedMap(t, callToolStructured(t, session, "wiki_get_subtree", map[string]any{
		"path": "/docs/sync.md",
	}), "root")
	if stringField(t, pageSubtree, "id") != "mcp-sync-page" || stringField(t, pageSubtree, "kind") != "page" {
		t.Fatalf("wiki_get_subtree /docs/sync.md = %#v, want page twin", pageSubtree)
	}

	sectionSubtree := nestedMap(t, callToolStructured(t, session, "wiki_get_subtree", map[string]any{
		"path": "/docs/sync",
	}), "root")
	if stringField(t, sectionSubtree, "id") != "mcp-sync-section" || stringField(t, sectionSubtree, "kind") != "section" {
		t.Fatalf("wiki_get_subtree /docs/sync = %#v, want section twin", sectionSubtree)
	}

	pageValidation := callToolStructured(t, session, "wiki_validate_page", map[string]any{
		"path": "/docs/sync.md",
	})
	if pageValidation["ok"] != true {
		t.Fatalf("wiki_validate_page /docs/sync.md = %#v, want page twin ok", pageValidation)
	}

	sectionValidation := callToolStructured(t, session, "wiki_validate_page", map[string]any{
		"path": "/docs/sync",
	})
	if sectionValidation["ok"] != true {
		t.Fatalf("wiki_validate_page /docs/sync = %#v, want section twin ok", sectionValidation)
	}

	draftPageBesideSection := callToolStructured(t, session, "wiki_validate_content", map[string]any{
		"path":    "/docs/section-only.md",
		"content": "---\nleafwiki_id: mcp-section-only-draft-page\nleafwiki_title: MCP Section Only Draft Page\n---\n# Draft Page\n",
	})
	if draftPageBesideSection["ok"] != true {
		t.Fatalf("wiki_validate_content draft page beside section = %#v, want ok", draftPageBesideSection)
	}
	assertValidationIssueCodesAbsent(t, draftPageBesideSection, []string{"path_conflict"})
}

func TestLocalMCPPathToolsResolveReadmeFallbackMarkdownPath(t *testing.T) {
	w := newLocalMCPTestWikiWithOptions(t, wiki.WikiOptions{
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
	session := connectLocalMCP(t, router, "/mcp")

	rootDir := w.GetRootDir()
	if err := os.MkdirAll(filepath.Join(rootDir, "docs", "guides"), 0o755); err != nil {
		t.Fatalf("mkdir guides fixture: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(rootDir, "docs", "indexed"), 0o755); err != nil {
		t.Fatalf("mkdir indexed fixture: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(rootDir, "docs", "no-readme"), 0o755); err != nil {
		t.Fatalf("mkdir no-readme fixture: %v", err)
	}
	if err := os.WriteFile(filepath.Join(rootDir, "docs", "index.md"), []byte("---\nleafwiki_id: mcp-docs-section\nleafwiki_title: Docs\n---\n# Docs\n"), 0o644); err != nil {
		t.Fatalf("write docs index: %v", err)
	}
	if err := os.WriteFile(filepath.Join(rootDir, "README.md"), []byte("---\nleafwiki_id: mcp-root-section\nleafwiki_title: Root\n---\n# Root\n"), 0o644); err != nil {
		t.Fatalf("write root README fallback: %v", err)
	}
	if err := os.WriteFile(filepath.Join(rootDir, "docs", "guides", "README.md"), []byte("---\nleafwiki_id: mcp-guides-section\nleafwiki_title: Guides\n---\n# Guides\n"), 0o644); err != nil {
		t.Fatalf("write guides README fallback: %v", err)
	}
	if err := os.WriteFile(filepath.Join(rootDir, "docs", "indexed", "index.md"), []byte("---\nleafwiki_id: mcp-indexed-section\nleafwiki_title: Indexed\n---\n# Indexed\n"), 0o644); err != nil {
		t.Fatalf("write indexed index: %v", err)
	}
	if err := os.WriteFile(filepath.Join(rootDir, "docs", "indexed", "README.md"), []byte("---\nleafwiki_id: mcp-indexed-readme-page\nleafwiki_title: Indexed README\n---\n# Indexed README\n"), 0o644); err != nil {
		t.Fatalf("write indexed README page: %v", err)
	}
	if err := os.WriteFile(filepath.Join(rootDir, "docs", "no-readme", "index.md"), []byte("---\nleafwiki_id: mcp-no-readme-section\nleafwiki_title: No README\n---\n# No README\n"), 0o644); err != nil {
		t.Fatalf("write no-readme index: %v", err)
	}
	if err := os.WriteFile(filepath.Join(rootDir, "docs", "guides", "child.md"), []byte("---\nleafwiki_id: mcp-guides-child\nleafwiki_title: Guides Child\n---\n# Guides Child\n"), 0o644); err != nil {
		t.Fatalf("write guides child: %v", err)
	}
	callToolStructured(t, session, "wiki_refresh", map[string]any{"source": "filesystem"})

	fallbackPage := nestedMap(t, callToolStructured(t, session, "wiki_get_page_by_path", map[string]any{
		"path": "/docs/guides/README.md",
	}), "page")
	if stringField(t, fallbackPage, "id") != "mcp-guides-section" || stringField(t, fallbackPage, "kind") != "section" {
		t.Fatalf("wiki_get_page_by_path README fallback = %#v, want guides section", fallbackPage)
	}

	explicitFallbackPage := nestedMap(t, callToolStructured(t, session, "wiki_get_page_by_path", map[string]any{
		"path": "/docs/guides/README.md",
		"kind": "section",
	}), "page")
	if stringField(t, explicitFallbackPage, "id") != "mcp-guides-section" || stringField(t, explicitFallbackPage, "kind") != "section" {
		t.Fatalf("wiki_get_page_by_path explicit README fallback section = %#v, want guides section", explicitFallbackPage)
	}

	fallbackSubtree := nestedMap(t, callToolStructured(t, session, "wiki_get_subtree", map[string]any{
		"path": "/docs/guides/README.md",
	}), "root")
	if stringField(t, fallbackSubtree, "id") != "mcp-guides-section" || stringField(t, fallbackSubtree, "kind") != "section" {
		t.Fatalf("wiki_get_subtree README fallback = %#v, want guides section", fallbackSubtree)
	}

	readmeChildPage := nestedMap(t, callToolStructured(t, session, "wiki_get_page_by_path", map[string]any{
		"path": "/docs/indexed/README.md",
	}), "page")
	if stringField(t, readmeChildPage, "id") != "mcp-indexed-readme-page" || stringField(t, readmeChildPage, "kind") != "page" {
		t.Fatalf("wiki_get_page_by_path README page = %#v, want indexed README page", readmeChildPage)
	}

	inactiveExplicitSectionErr := callToolError(t, session, "wiki_get_page_by_path", map[string]any{
		"path": "/docs/indexed/README.md",
		"kind": "section",
	})
	assertErrorContainsAny(t, "wiki_get_page_by_path inactive explicit README section", inactiveExplicitSectionErr, "not found", "page_not_found")

	missingReadmeErr := callToolError(t, session, "wiki_get_page_by_path", map[string]any{
		"path": "/docs/no-readme/README.md",
	})
	assertErrorContainsAny(t, "wiki_get_page_by_path missing README", missingReadmeErr, "not found", "page_not_found")

	lowercaseReadmeErr := callToolError(t, session, "wiki_get_page_by_path", map[string]any{
		"path": "/docs/guides/readme.md",
	})
	assertErrorContainsAny(t, "wiki_get_page_by_path lowercase readme", lowercaseReadmeErr, "not found", "page_not_found")

	traversalReadmeErr := callToolError(t, session, "wiki_get_page_by_path", map[string]any{
		"path": "../README.md",
		"kind": "section",
	})
	assertErrorContainsAny(t, "wiki_get_page_by_path traversal README", traversalReadmeErr, "invalid", "invalid_path")

	fallbackValidation := callToolStructured(t, session, "wiki_validate_page", map[string]any{
		"path": "/docs/guides/README.md",
	})
	if fallbackValidation["ok"] != true {
		t.Fatalf("wiki_validate_page README fallback = %#v, want ok", fallbackValidation)
	}

	lowercaseValidationErr := callToolError(t, session, "wiki_validate_page", map[string]any{
		"path": "/docs/guides/readme.md",
	})
	assertErrorContainsAny(t, "wiki_validate_page lowercase readme", lowercaseValidationErr, "not found", "page_not_found")

	fallbackDraftValidation := callToolStructured(t, session, "wiki_validate_content", map[string]any{
		"path":    "/docs/guides/README.md",
		"content": "---\nleafwiki_id: mcp-guides-section\nleafwiki_title: Guides\n---\n[Child](./child.md)\n",
	})
	if fallbackDraftValidation["ok"] != true {
		t.Fatalf("wiki_validate_content README fallback = %#v, want ok", fallbackDraftValidation)
	}

	explicitFallbackDraftValidation := callToolStructured(t, session, "wiki_validate_content", map[string]any{
		"path":    "/docs/guides/README.md",
		"kind":    "section",
		"content": "[Child](./child.md)\n",
	})
	if explicitFallbackDraftValidation["ok"] != true {
		t.Fatalf("wiki_validate_content explicit README fallback section = %#v, want ok", explicitFallbackDraftValidation)
	}
}

func TestLocalMCPValidateWikiScansUnsyncedMarkdownFiles(t *testing.T) {
	w := newLocalMCPTestWikiWithOptions(t, wiki.WikiOptions{
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
	session := connectLocalMCP(t, router, "/mcp")

	created := nestedMap(t, callToolStructured(t, session, "wiki_create_page", map[string]any{
		"title": "Original Duplicate ID",
		"slug":  "original-duplicate-id",
		"kind":  "page",
	}), "page")
	duplicatePath := filepath.Join(w.GetRootDir(), "unsynced-duplicate-id.md")
	if err := os.WriteFile(duplicatePath, []byte(strings.Join([]string{
		"---",
		"leafwiki_id: " + stringField(t, created, "id"),
		"leafwiki_title: Unsynced Duplicate ID",
		"---",
		"# Unsynced Duplicate ID",
		"",
		"This file has not been refreshed into the tree yet.",
	}, "\n")), 0o644); err != nil {
		t.Fatalf("write unsynced duplicate: %v", err)
	}

	out := callToolStructured(t, session, "wiki_validate_wiki", nil)
	if out["ok"] != false {
		t.Fatalf("wiki_validate_wiki = %#v, want duplicate ID error before refresh", out)
	}
	assertValidationIssueCodes(t, out, []string{"duplicate_leafwiki_id"})
	duplicateIssue := validationIssueByCode(t, out, "duplicate_leafwiki_id")
	if duplicateIssue["path"] != "unsynced-duplicate-id.md" {
		t.Fatalf("duplicate_leafwiki_id path = %#v, want unsynced duplicate path in %#v", duplicateIssue["path"], duplicateIssue)
	}
	if duplicateIssue["pageId"] != stringField(t, created, "id") {
		t.Fatalf("duplicate_leafwiki_id pageId = %#v, want existing page ID %q in %#v", duplicateIssue["pageId"], stringField(t, created, "id"), duplicateIssue)
	}
	if message, ok := duplicateIssue["message"].(string); !ok || !strings.Contains(message, "original-duplicate-id.md") {
		t.Fatalf("duplicate_leafwiki_id message = %#v, want first path detail in %#v", duplicateIssue["message"], duplicateIssue)
	}

	if err := os.MkdirAll(filepath.Join(w.GetRootDir(), "guides"), 0o755); err != nil {
		t.Fatalf("mkdir guides: %v", err)
	}
	if err := os.WriteFile(filepath.Join(w.GetRootDir(), "guides", "README.md"), []byte(strings.Join([]string{
		"---",
		"leafwiki_id: guides",
		"leafwiki_title: Guides",
		"---",
		"# Guides",
	}, "\n")), 0o644); err != nil {
		t.Fatalf("write guides README: %v", err)
	}
	if err := os.WriteFile(filepath.Join(w.GetRootDir(), "readme-fallback-link.md"), []byte(strings.Join([]string{
		"---",
		"leafwiki_id: readme-fallback-link",
		"leafwiki_title: README Fallback Link",
		"---",
		"# README Fallback Link",
		"",
		"[Guides](/guides/README.md)",
	}, "\n")), 0o644); err != nil {
		t.Fatalf("write README fallback link source: %v", err)
	}
	readmeFallbackOut := callToolStructured(t, session, "wiki_validate_wiki", nil)
	assertValidationIssueCodesAbsent(t, readmeFallbackOut, []string{"broken_link"})

	if err := os.WriteFile(filepath.Join(w.GetRootDir(), ".hidden.md"), []byte("# Hidden\n"), 0o644); err != nil {
		t.Fatalf("write hidden markdown: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(w.GetRootDir(), ".scratch"), 0o755); err != nil {
		t.Fatalf("mkdir hidden scratch dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(w.GetRootDir(), ".scratch", "bad.md"), []byte("---\nleafwiki_private: true\n---\n[Missing](/missing-from-hidden-dir)\n"), 0o644); err != nil {
		t.Fatalf("write hidden scratch markdown: %v", err)
	}
	withoutWarnings := callToolStructured(t, session, "wiki_validate_wiki", map[string]any{"includeWarnings": false})
	assertValidationIssueCodesAbsent(t, withoutWarnings, []string{"hidden_markdown_path"})
	withWarnings := callToolStructured(t, session, "wiki_validate_wiki", map[string]any{"includeWarnings": true})
	assertValidationIssueCodes(t, withWarnings, []string{"hidden_markdown_path"})
	assertNoValidationIssuePath(t, withWarnings, ".scratch/bad.md")

	if err := os.WriteFile(filepath.Join(w.GetRootDir(), "!!!.md"), []byte("# Invalid Slug\n"), 0o644); err != nil {
		t.Fatalf("write invalid slug markdown: %v", err)
	}
	if err := os.WriteFile(filepath.Join(w.GetRootDir(), "missing-asset.md"), []byte(strings.Join([]string{
		"---",
		"leafwiki_id: missing-asset",
		"leafwiki_title: Missing Asset",
		"---",
		"# Missing Asset",
		"",
		"[Missing asset](nope.png)",
	}, "\n")), 0o644); err != nil {
		t.Fatalf("write missing asset markdown: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(w.GetRootDir(), "route-conflict"), 0o755); err != nil {
		t.Fatalf("mkdir route conflict: %v", err)
	}
	if err := os.WriteFile(filepath.Join(w.GetRootDir(), "route-conflict", "index.md"), []byte("# Route Conflict Section\n"), 0o644); err != nil {
		t.Fatalf("write route conflict index: %v", err)
	}
	if err := os.WriteFile(filepath.Join(w.GetRootDir(), "route-conflict.md"), []byte("# Route Conflict Page\n"), 0o644); err != nil {
		t.Fatalf("write route conflict page: %v", err)
	}
	if err := os.WriteFile(filepath.Join(w.GetRootDir(), "route-conflict-link.md"), []byte(strings.Join([]string{
		"---",
		"leafwiki_id: route-conflict-link",
		"leafwiki_title: Route Conflict Link",
		"---",
		"# Route Conflict Link",
		"",
		"[Ambiguous](/route-conflict)",
	}, "\n")), 0o644); err != nil {
		t.Fatalf("write route conflict link: %v", err)
	}
	conflicts := callToolStructured(t, session, "wiki_validate_wiki", map[string]any{"includeWarnings": false})
	assertValidationIssueCodes(t, conflicts, []string{"invalid_slug", "missing_asset"})
	assertValidationIssueCodesAbsent(t, conflicts, []string{"path_conflict", "ambiguous_legacy_link"})
	ambiguousContent := callToolStructured(t, session, "wiki_validate_content", map[string]any{
		"path":    "draft-route-conflict",
		"content": "[Ambiguous](/route-conflict)\n",
	})
	assertValidationIssueCodesAbsent(t, ambiguousContent, []string{"ambiguous_legacy_link"})

	broken := nestedMap(t, callToolStructured(t, session, "wiki_create_page", map[string]any{
		"title": "Broken Link",
		"slug":  "broken-link",
		"kind":  "page",
	}), "page")
	callToolStructured(t, session, "wiki_update_page", map[string]any{
		"id":      stringField(t, broken, "id"),
		"version": stringField(t, broken, "version"),
		"title":   "Broken Link",
		"slug":    "broken-link",
		"content": "[Missing](/missing-validation-target)\n",
	})
	duplicated := callToolStructured(t, session, "wiki_validate_wiki", map[string]any{"includeWarnings": false})
	if got := validationIssueCodeCount(t, duplicated, "broken_link", "broken-link"); got != 1 {
		t.Fatalf("broken_link issue count for broken-link = %d, want 1 in %#v", got, duplicated["issues"])
	}
}

func TestLocalMCPValidateWikiResolvesLinksBetweenUnsyncedMarkdownFiles(t *testing.T) {
	rootDir := filepath.Join(t.TempDir(), "content")
	w := newLocalMCPTestWikiWithOptions(t, wiki.WikiOptions{
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
	session := connectLocalMCP(t, router, "/mcp")

	if err := os.WriteFile(filepath.Join(rootDir, "unsynced-a.md"), []byte(strings.Join([]string{
		"---",
		"leafwiki_id: unsynced-a",
		"leafwiki_title: Unsynced A",
		"---",
		"# Unsynced A",
		"",
		"[Unsynced B route](/unsynced-b)",
		"[Unsynced B absolute markdown](/unsynced-b.md)",
		"[Unsynced B relative markdown](./unsynced-b.md)",
	}, "\n")), 0o644); err != nil {
		t.Fatalf("write unsynced-a.md: %v", err)
	}
	if err := os.WriteFile(filepath.Join(rootDir, "unsynced-b.md"), []byte(strings.Join([]string{
		"---",
		"leafwiki_id: unsynced-b",
		"leafwiki_title: Unsynced B",
		"---",
		"# Unsynced B",
	}, "\n")), 0o644); err != nil {
		t.Fatalf("write unsynced-b.md: %v", err)
	}

	out := callToolStructured(t, session, "wiki_validate_wiki", map[string]any{"includeWarnings": false})
	assertValidationIssueCodesAbsent(t, out, []string{"broken_link"})
}

func TestLocalMCPUpdatePageMetadata_PatchesMetadataWithoutChangingBody(t *testing.T) {
	w := newLocalMCPTestWiki(t, false)
	router := newLocalMCPTestRouter(w, httpinternal.RouterOptions{
		AuthDisabled:            true,
		PublicAccess:            true,
		AllowInsecure:           true,
		MaxAssetUploadSizeBytes: assets.DefaultMaxUploadSizeBytes,
		MCPEnabled:              true,
		MCPToolListPageSize:     200,
	})
	session := connectLocalMCP(t, router, "/mcp")

	created := nestedMap(t, callToolStructured(t, session, "wiki_create_page", map[string]any{
		"title": "Metadata Target",
		"slug":  "metadata-target",
		"kind":  "page",
	}), "page")
	originalVersion := stringField(t, created, "version")
	updated := nestedMap(t, callToolStructured(t, session, "wiki_update_page", map[string]any{
		"id":      stringField(t, created, "id"),
		"version": originalVersion,
		"title":   "Metadata Target",
		"slug":    "metadata-target",
		"content": "Original body",
		"tags":    []any{"old", "keep"},
		"properties": map[string]any{
			"status": "draft",
			"owner":  "team",
		},
	}), "page")

	result := callToolStructured(t, session, "wiki_update_page_metadata", map[string]any{
		"pageId":           stringField(t, updated, "id"),
		"version":          stringField(t, updated, "version"),
		"addTags":          []any{"new"},
		"removeTags":       []any{"old"},
		"setProperties":    map[string]any{"status": "ready"},
		"removeProperties": []any{"owner"},
		"includePage":      true,
	})
	page := nestedMap(t, result, "page")
	if page["content"] != "Original body" {
		t.Fatalf("content = %#v, want unchanged body", page["content"])
	}
	assertStringSet(t, "metadata tags", stringSliceField(t, page, "tags"), []string{"keep", "new"})
	props := nestedMap(t, page, "properties")
	if props["status"] != "ready" {
		t.Fatalf("properties = %#v, want status ready", props)
	}
	if _, exists := props["owner"]; exists {
		t.Fatalf("properties = %#v, want owner removed", props)
	}

	compact := callToolStructured(t, session, "wiki_update_page_metadata", map[string]any{
		"path":              "/metadata-target",
		"version":           stringField(t, result, "version"),
		"addTags":           []any{"quiet"},
		"includeValidation": false,
	})
	if _, exists := compact["validation"]; exists {
		t.Fatalf("compact metadata output = %#v, did not expect validation when includeValidation=false", compact)
	}

	missingTargetErr := callToolError(t, session, "wiki_update_page_metadata", map[string]any{
		"version": stringField(t, compact, "version"),
		"addTags": []any{"missing-target"},
	})
	assertErrorContainsAny(t, "wiki_update_page_metadata missing target", missingTargetErr, "pageId or path is required")
	ambiguousTargetErr := callToolError(t, session, "wiki_update_page_metadata", map[string]any{
		"pageId":  stringField(t, updated, "id"),
		"path":    "/metadata-target",
		"version": stringField(t, compact, "version"),
		"addTags": []any{"ambiguous-target"},
	})
	assertErrorContainsAny(t, "wiki_update_page_metadata ambiguous target", ambiguousTargetErr, "pageId and path cannot both be supplied")

	beforeReserved := nestedMap(t, callToolStructured(t, session, "wiki_get_page", map[string]any{
		"pageId": stringField(t, updated, "id"),
	}), "page")
	reservedErr := callToolError(t, session, "wiki_update_page_metadata", map[string]any{
		"pageId":        stringField(t, updated, "id"),
		"version":       stringField(t, beforeReserved, "version"),
		"setProperties": map[string]any{"leafwiki_private": "true"},
	})
	assertErrorContainsAny(t, "wiki_update_page_metadata reserved key", reservedErr, "reserved", "validation")
	afterReserved := nestedMap(t, callToolStructured(t, session, "wiki_get_page", map[string]any{
		"pageId": stringField(t, updated, "id"),
	}), "page")
	if afterReserved["version"] != beforeReserved["version"] || afterReserved["content"] != beforeReserved["content"] {
		t.Fatalf("page after reserved metadata edit = %#v, want unchanged %#v", afterReserved, beforeReserved)
	}
	assertStringSet(t, "metadata tags after reserved failure", stringSliceField(t, afterReserved, "tags"), []string{"keep", "new", "quiet"})
	if props := nestedMap(t, afterReserved, "properties"); props["leafwiki_private"] != nil || props["status"] != "ready" {
		t.Fatalf("properties after reserved metadata edit = %#v, want unchanged status and no reserved key", props)
	}

	setTagsResult := callToolStructured(t, session, "wiki_update_page_metadata", map[string]any{
		"pageId":      stringField(t, updated, "id"),
		"version":     stringField(t, afterReserved, "version"),
		"setTags":     []any{"final"},
		"includePage": true,
	})
	setTagsPage := nestedMap(t, setTagsResult, "page")
	assertStringSet(t, "metadata setTags replacement", stringSliceField(t, setTagsPage, "tags"), []string{"final"})

	staleReservedErr := callToolError(t, session, "wiki_update_page_metadata", map[string]any{
		"pageId":        stringField(t, updated, "id"),
		"version":       originalVersion,
		"setProperties": map[string]any{"leafwiki_private": "true"},
	})
	assertErrorContainsAny(t, "wiki_update_page_metadata stale version before reserved key", staleReservedErr, "page_version_conflict", "version conflict")
	assertErrorDoesNotContainAny(t, "wiki_update_page_metadata stale version before reserved key", staleReservedErr, "reserved", "validation")

	staleErr := callToolError(t, session, "wiki_update_page_metadata", map[string]any{
		"pageId":  stringField(t, updated, "id"),
		"version": originalVersion,
		"addTags": []any{"late"},
	})
	assertErrorContainsAny(t, "wiki_update_page_metadata stale version", staleErr, "page_version_conflict", "version conflict")
	assertErrorContainsAll(t, "wiki_update_page_metadata stale version context", staleErr, []string{
		"currentPageId=" + stringField(t, updated, "id"),
		"currentPath=metadata-target",
		"currentTitle=Metadata Target",
		"currentVersion=",
	})
	afterStale := nestedMap(t, callToolStructured(t, session, "wiki_get_page", map[string]any{
		"pageId": stringField(t, updated, "id"),
	}), "page")
	if slices := stringSliceField(t, afterStale, "tags"); contains(slices, "late") {
		t.Fatalf("tags after stale metadata edit = %#v, did not expect failed tag", slices)
	}
	if afterStale["content"] != "Original body" {
		t.Fatalf("content after stale metadata edit = %#v, want unchanged body", afterStale["content"])
	}
}

func TestLocalMCPUpdatePageMetadata_PreservesUnmanagedFrontmatter(t *testing.T) {
	w := newLocalMCPTestWikiWithOptions(t, wiki.WikiOptions{
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
	session := connectLocalMCP(t, router, "/mcp")

	created := nestedMap(t, callToolStructured(t, session, "wiki_create_page", map[string]any{
		"title": "Metadata Preserve",
		"slug":  "metadata-preserve",
		"kind":  "page",
	}), "page")
	updated := nestedMap(t, callToolStructured(t, session, "wiki_update_page", map[string]any{
		"id":      stringField(t, created, "id"),
		"version": stringField(t, created, "version"),
		"title":   "Metadata Preserve",
		"slug":    "metadata-preserve",
		"content": "# Metadata Preserve\n\nBody",
		"tags":    []any{"draft"},
		"properties": map[string]any{
			"status": "draft",
		},
	}), "page")
	rawPath := filepath.Join(w.GetRootDir(), "metadata-preserve.md")
	if err := os.WriteFile(rawPath, []byte(strings.Join([]string{
		"---",
		"tags:",
		"  - draft",
		"status: draft",
		"pinned: true",
		"audiences:",
		"  - internal",
		"  - external",
		"nested:",
		"  owner: docs",
		"leafwiki_id: " + stringField(t, updated, "id"),
		"leafwiki_title: Metadata Preserve",
		"---",
		"# Metadata Preserve",
		"",
		"Body",
	}, "\n")), 0o644); err != nil {
		t.Fatalf("write custom frontmatter: %v", err)
	}
	callToolStructured(t, session, "wiki_refresh", map[string]any{"source": "filesystem"})
	refreshed := nestedMap(t, callToolStructured(t, session, "wiki_get_page_by_path", map[string]any{"path": "/metadata-preserve"}), "page")

	result := callToolStructured(t, session, "wiki_update_page_metadata", map[string]any{
		"path":          "/metadata-preserve",
		"version":       stringField(t, refreshed, "version"),
		"addTags":       []any{"ready"},
		"setProperties": map[string]any{"status": "published"},
		"includePage":   true,
	})
	page := nestedMap(t, result, "page")
	if page["content"] != "# Metadata Preserve\n\nBody" {
		t.Fatalf("page body = %#v, want body without frontmatter", page["content"])
	}

	raw := readPageMarkdownByRoutePath(t, w.GetRootDir(), "metadata-preserve")
	doc := assertCanonicalPageMarkdown(t, "wiki_update_page_metadata raw markdown", raw)
	if strings.Contains(raw, "leafwiki_id:") {
		t.Fatalf("raw metadata after patch retained legacy leafwiki_id:\n%s", raw)
	}
	if doc.Metadata.Fields["status"] != "published" {
		t.Fatalf("canonical fields after patch = %#v, want status=published", doc.Metadata.Fields)
	}
	for _, want := range []string{
		"pinned: true",
		"audiences:",
		"- internal",
		"- external",
		"nested:",
		"owner: docs",
		"status: published",
		"- ready",
	} {
		if !strings.Contains(raw, want) {
			t.Fatalf("raw metadata after patch missing %q:\n%s", want, raw)
		}
	}
	contextOut := callToolStructured(t, session, "wiki_get_context", map[string]any{
		"syncMode":           "none",
		"recentChangesLimit": float64(5),
	})
	assertRecentChangesIncludePath(t, contextOut, "metadata-preserve.md")
}

func TestLocalMCPUpdatePage_PreservesTagsAndPropertiesWhenOmittedAndClearsWhenExplicitEmpty(t *testing.T) {
	w := newLocalMCPTestWiki(t, false)
	router := newLocalMCPTestRouter(w, httpinternal.RouterOptions{
		AuthDisabled:            true,
		PublicAccess:            true,
		AllowInsecure:           true,
		MaxAssetUploadSizeBytes: assets.DefaultMaxUploadSizeBytes,
		MCPEnabled:              true,
		MCPToolListPageSize:     200,
	})
	session := connectLocalMCP(t, router, "/mcp")

	created := nestedMap(t, callToolStructured(t, session, "wiki_create_page", map[string]any{
		"title": "MCP Metadata Preserve",
		"slug":  "mcp-metadata-preserve",
		"kind":  "page",
	}), "page")
	first := nestedMap(t, callToolStructured(t, session, "wiki_update_page", map[string]any{
		"id":      stringField(t, created, "id"),
		"version": stringField(t, created, "version"),
		"title":   "MCP Metadata Preserve",
		"slug":    "mcp-metadata-preserve",
		"content": "# MCP Metadata Preserve\n\nFirst",
		"tags":    []any{"draft"},
		"properties": map[string]any{
			"status": "draft",
		},
	}), "page")
	rawAfterFirst := readPageMarkdownByRoutePath(t, w.GetRootDir(), "mcp-metadata-preserve")
	firstDoc := assertCanonicalPageMarkdown(t, "wiki_update_page metadata preserve first update", rawAfterFirst)
	if len(firstDoc.Metadata.Tags) != 1 || firstDoc.Metadata.Tags[0] != "draft" {
		t.Fatalf("first raw tags = %#v, want [draft]", firstDoc.Metadata.Tags)
	}
	if firstDoc.Metadata.Fields["status"] != "draft" {
		t.Fatalf("first raw fields = %#v, want status=draft", firstDoc.Metadata.Fields)
	}

	metadataOnly := nestedMap(t, callToolStructured(t, session, "wiki_update_page", map[string]any{
		"id":      stringField(t, first, "id"),
		"version": stringField(t, first, "version"),
		"title":   "MCP Metadata Preserve",
		"slug":    "mcp-metadata-preserve",
		"tags":    []any{"ready"},
		"properties": map[string]any{
			"status": "ready",
		},
	}), "page")
	if metadataOnly["content"] != "# MCP Metadata Preserve\n\nFirst" {
		t.Fatalf("expected metadata-only wiki_update_page to preserve body, got %#v", metadataOnly["content"])
	}
	assertStringSet(t, "metadata-only wiki_update_page tags", stringSliceField(t, metadataOnly, "tags"), []string{"ready"})
	metadataOnlyProperties := nestedMap(t, metadataOnly, "properties")
	if metadataOnlyProperties["status"] != "ready" {
		t.Fatalf("expected metadata-only properties to update, got %#v", metadataOnlyProperties)
	}

	omitted := nestedMap(t, callToolStructured(t, session, "wiki_update_page", map[string]any{
		"id":      stringField(t, metadataOnly, "id"),
		"version": stringField(t, metadataOnly, "version"),
		"title":   "MCP Metadata Preserve",
		"slug":    "mcp-metadata-preserve",
		"content": "# MCP Metadata Preserve\n\nSecond",
	}), "page")
	assertStringSet(t, "omitted wiki_update_page tags", stringSliceField(t, omitted, "tags"), []string{"ready"})
	omittedProperties := nestedMap(t, omitted, "properties")
	if omittedProperties["status"] != "ready" {
		t.Fatalf("expected omitted properties to be preserved, got %#v", omittedProperties)
	}

	cleared := nestedMap(t, callToolStructured(t, session, "wiki_update_page", map[string]any{
		"id":         stringField(t, omitted, "id"),
		"version":    stringField(t, omitted, "version"),
		"title":      "MCP Metadata Preserve",
		"slug":       "mcp-metadata-preserve",
		"content":    "# MCP Metadata Preserve\n\nThird",
		"tags":       []any{},
		"properties": map[string]any{},
	}), "page")
	assertStringSet(t, "explicit empty wiki_update_page tags", stringSliceField(t, cleared, "tags"), nil)
	clearedProperties := nestedMap(t, cleared, "properties")
	if len(clearedProperties) != 0 {
		t.Fatalf("expected explicit empty properties to clear metadata, got %#v", clearedProperties)
	}
	rawAfterClear := readPageMarkdownByRoutePath(t, w.GetRootDir(), "mcp-metadata-preserve")
	clearDoc := assertCanonicalPageMarkdown(t, "wiki_update_page metadata preserve clear update", rawAfterClear)
	if len(clearDoc.Metadata.Tags) != 0 || len(clearDoc.Metadata.Fields) != 0 {
		t.Fatalf("clear raw metadata = tags %#v fields %#v, want both empty", clearDoc.Metadata.Tags, clearDoc.Metadata.Fields)
	}
}

func TestLocalMCPReplacePageSection_PreservesFrontmatter(t *testing.T) {
	w := newLocalMCPTestWikiWithOptions(t, wiki.WikiOptions{
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
	session := connectLocalMCP(t, router, "/mcp")

	created := nestedMap(t, callToolStructured(t, session, "wiki_create_page", map[string]any{
		"title": "Section Preserve",
		"slug":  "section-preserve",
		"kind":  "page",
	}), "page")
	rawPath := filepath.Join(w.GetRootDir(), "section-preserve.md")
	if err := os.WriteFile(rawPath, []byte(strings.Join([]string{
		"---",
		"tags:",
		"  - draft",
		"status: draft",
		"pinned: true",
		"audiences:",
		"  - internal",
		"  - external",
		"leafwiki_id: " + stringField(t, created, "id"),
		"leafwiki_title: Section Preserve",
		"---",
		"# Section Preserve",
		"",
		"## API",
		"",
		"old api",
	}, "\n")), 0o644); err != nil {
		t.Fatalf("write section preserve frontmatter: %v", err)
	}
	callToolStructured(t, session, "wiki_refresh", map[string]any{"source": "filesystem"})
	refreshed := nestedMap(t, callToolStructured(t, session, "wiki_get_page_by_path", map[string]any{"path": "/section-preserve"}), "page")

	result := callToolStructured(t, session, "wiki_replace_page_section", map[string]any{
		"path":              "/section-preserve",
		"version":           stringField(t, refreshed, "version"),
		"headingPath":       []any{"API"},
		"content":           "new api\n",
		"includePage":       true,
		"includeValidation": false,
	})
	page := nestedMap(t, result, "page")
	assertStringSet(t, "section replacement tags", stringSliceField(t, page, "tags"), []string{"draft"})
	props := nestedMap(t, page, "properties")
	if props["status"] != "draft" {
		t.Fatalf("section replacement properties = %#v, want status draft", props)
	}

	raw := readPageMarkdownByRoutePath(t, w.GetRootDir(), "section-preserve")
	for _, want := range []string{
		"pinned: true",
		"audiences:",
		"- internal",
		"- external",
		"status: draft",
		"## API\nnew api",
	} {
		if !strings.Contains(raw, want) {
			t.Fatalf("raw section replacement markdown missing %q:\n%s", want, raw)
		}
	}
	if strings.Contains(raw, "old api") {
		t.Fatalf("raw section replacement markdown kept old section body:\n%s", raw)
	}
}

func TestLocalMCPReplacePageSection_ReplacesTargetSectionOnly(t *testing.T) {
	w := newLocalMCPTestWiki(t, false)
	router := newLocalMCPTestRouter(w, httpinternal.RouterOptions{
		AuthDisabled:            true,
		PublicAccess:            true,
		AllowInsecure:           true,
		MaxAssetUploadSizeBytes: assets.DefaultMaxUploadSizeBytes,
		MCPEnabled:              true,
		MCPToolListPageSize:     200,
	})
	session := connectLocalMCP(t, router, "/mcp")

	created := nestedMap(t, callToolStructured(t, session, "wiki_create_page", map[string]any{
		"title": "Section Target",
		"slug":  "section-target",
		"kind":  "page",
	}), "page")
	originalVersion := stringField(t, created, "version")
	body := "# Guide\n\nIntro\n\n```\n## API\nfake code heading\n```\n\n## API\n\nold api\n\n## Other\n\nkeep me\n"
	updated := nestedMap(t, callToolStructured(t, session, "wiki_update_page", map[string]any{
		"id":      stringField(t, created, "id"),
		"version": originalVersion,
		"title":   "Section Target",
		"slug":    "section-target",
		"content": body,
	}), "page")

	result := callToolStructured(t, session, "wiki_replace_page_section", map[string]any{
		"path":              "/section-target",
		"version":           stringField(t, updated, "version"),
		"headingPath":       []any{"API"},
		"content":           "new api\n",
		"includePage":       true,
		"includeValidation": false,
	})
	if _, exists := result["validation"]; exists {
		t.Fatalf("compact section output = %#v, did not expect validation when includeValidation=false", result)
	}
	page := nestedMap(t, result, "page")
	content := stringField(t, page, "content")
	if !strings.Contains(content, "## API\nnew api\n") {
		t.Fatalf("content after section replace = %q, want new API body", content)
	}
	if !strings.Contains(content, "```\n## API\nfake code heading\n```") {
		t.Fatalf("content after section replace = %q, want fenced heading preserved", content)
	}
	if !strings.Contains(content, "## Other\n\nkeep me") {
		t.Fatalf("content after section replace = %q, want unrelated section preserved", content)
	}
	if strings.Contains(content, "old api") {
		t.Fatalf("content after section replace = %q, want old API body removed", content)
	}

	missingTargetErr := callToolError(t, session, "wiki_replace_page_section", map[string]any{
		"version":     stringField(t, result, "version"),
		"headingPath": []any{"API"},
		"content":     "missing target",
	})
	assertErrorContainsAny(t, "wiki_replace_page_section missing target", missingTargetErr, "pageId or path is required")
	ambiguousTargetErr := callToolError(t, session, "wiki_replace_page_section", map[string]any{
		"pageId":      stringField(t, updated, "id"),
		"path":        "/section-target",
		"version":     stringField(t, result, "version"),
		"headingPath": []any{"API"},
		"content":     "ambiguous target",
	})
	assertErrorContainsAny(t, "wiki_replace_page_section ambiguous target", ambiguousTargetErr, "pageId and path cannot both be supplied")

	withValidation := callToolStructured(t, session, "wiki_replace_page_section", map[string]any{
		"path":              "/section-target",
		"version":           stringField(t, result, "version"),
		"headingPath":       []any{"API"},
		"content":           "new api with [Missing](/missing-section-target)\n",
		"includePage":       true,
		"includeValidation": true,
	})
	validation := nestedMap(t, withValidation, "validation")
	assertValidationIssueCodes(t, validation, []string{"broken_link"})

	staleMissingHeadingErr := callToolError(t, session, "wiki_replace_page_section", map[string]any{
		"pageId":      stringField(t, updated, "id"),
		"version":     originalVersion,
		"headingPath": []any{"Missing"},
		"content":     "late missing",
	})
	assertErrorContainsAny(t, "wiki_replace_page_section stale version before missing heading", staleMissingHeadingErr, "page_version_conflict", "version conflict")
	assertErrorDoesNotContainAny(t, "wiki_replace_page_section stale version before missing heading", staleMissingHeadingErr, "heading_not_found")

	staleErr := callToolError(t, session, "wiki_replace_page_section", map[string]any{
		"pageId":      stringField(t, updated, "id"),
		"version":     originalVersion,
		"headingPath": []any{"API"},
		"content":     "late change",
	})
	assertErrorContainsAny(t, "wiki_replace_page_section stale version", staleErr, "page_version_conflict", "version conflict")
	assertErrorContainsAll(t, "wiki_replace_page_section stale version context", staleErr, []string{
		"currentPageId=" + stringField(t, updated, "id"),
		"currentPath=section-target",
		"currentTitle=Section Target",
		"currentVersion=",
	})
	afterStale := nestedMap(t, callToolStructured(t, session, "wiki_get_page", map[string]any{
		"pageId": stringField(t, updated, "id"),
	}), "page")
	afterContent := stringField(t, afterStale, "content")
	if strings.Contains(afterContent, "late change") || strings.Contains(afterContent, "old api") {
		t.Fatalf("content after stale section edit = %q, want previously successful edit only", afterContent)
	}
}

func TestLocalMCPReplacePageSection_FailuresDoNotMutate(t *testing.T) {
	w := newLocalMCPTestWiki(t, false)
	router := newLocalMCPTestRouter(w, httpinternal.RouterOptions{
		AuthDisabled:            true,
		PublicAccess:            true,
		AllowInsecure:           true,
		MaxAssetUploadSizeBytes: assets.DefaultMaxUploadSizeBytes,
		MCPEnabled:              true,
		MCPToolListPageSize:     200,
	})
	session := connectLocalMCP(t, router, "/mcp")

	created := nestedMap(t, callToolStructured(t, session, "wiki_create_page", map[string]any{
		"title": "Section Failure Target",
		"slug":  "section-failure-target",
		"kind":  "page",
	}), "page")
	body := "# Guide\n\n## Notes\n\nfirst\n\n## Notes\n\nsecond\n"
	updated := nestedMap(t, callToolStructured(t, session, "wiki_update_page", map[string]any{
		"id":      stringField(t, created, "id"),
		"version": stringField(t, created, "version"),
		"title":   "Section Failure Target",
		"slug":    "section-failure-target",
		"content": body,
	}), "page")

	ambiguousErr := callToolError(t, session, "wiki_replace_page_section", map[string]any{
		"pageId":      stringField(t, updated, "id"),
		"version":     stringField(t, updated, "version"),
		"headingPath": []any{"Notes"},
		"content":     "ambiguous mutation",
	})
	assertErrorContainsAny(t, "wiki_replace_page_section ambiguous heading", ambiguousErr, "ambiguous_heading")
	afterAmbiguous := nestedMap(t, callToolStructured(t, session, "wiki_get_page", map[string]any{
		"pageId": stringField(t, updated, "id"),
	}), "page")
	if afterAmbiguous["version"] != updated["version"] || afterAmbiguous["content"] != body {
		t.Fatalf("page after ambiguous heading = %#v, want unchanged %#v", afterAmbiguous, updated)
	}

	missingErr := callToolError(t, session, "wiki_replace_page_section", map[string]any{
		"pageId":      stringField(t, updated, "id"),
		"version":     stringField(t, updated, "version"),
		"headingPath": []any{"Missing"},
		"content":     "missing mutation",
	})
	assertErrorContainsAny(t, "wiki_replace_page_section missing heading", missingErr, "heading_not_found")
	afterMissing := nestedMap(t, callToolStructured(t, session, "wiki_get_page", map[string]any{
		"pageId": stringField(t, updated, "id"),
	}), "page")
	if afterMissing["version"] != updated["version"] || afterMissing["content"] != body {
		t.Fatalf("page after missing heading = %#v, want unchanged %#v", afterMissing, updated)
	}

	replaced := callToolStructured(t, session, "wiki_replace_page_section", map[string]any{
		"pageId":      stringField(t, updated, "id"),
		"version":     stringField(t, updated, "version"),
		"headingPath": []any{"Notes"},
		"occurrence":  float64(2),
		"content":     "second updated\n",
		"includePage": true,
	})
	replacedPage := nestedMap(t, replaced, "page")
	replacedContent := stringField(t, replacedPage, "content")
	if !strings.Contains(replacedContent, "## Notes\n\nfirst") || !strings.Contains(replacedContent, "## Notes\nsecond updated") {
		t.Fatalf("content after occurrence replace = %q, want second Notes replaced only", replacedContent)
	}
}

func TestLocalMCPRegistration_RespectsBasePath(t *testing.T) {
	w := newLocalMCPTestWiki(t, false)
	router := newLocalMCPTestRouter(w, httpinternal.RouterOptions{
		AuthDisabled:            true,
		PublicAccess:            true,
		AllowInsecure:           true,
		BasePath:                "/wiki",
		MaxAssetUploadSizeBytes: assets.DefaultMaxUploadSizeBytes,
		MCPEnabled:              true,
		MCPToolListPageSize:     200,
	})

	for _, method := range []string{http.MethodGet, http.MethodPost, http.MethodDelete} {
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, httptest.NewRequest(method, "/mcp", strings.NewReader("{}")))
		if rec.Code != http.StatusNotFound {
			t.Fatalf("%s /mcp without base path = %d, want 404", method, rec.Code)
		}
	}

	for _, method := range []string{http.MethodGet, http.MethodPost, http.MethodDelete} {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(method, "/wiki/mcp", strings.NewReader("{}"))
		req.RemoteAddr = "127.0.0.1:12345"
		router.ServeHTTP(rec, req)
		if rec.Code == http.StatusNotFound {
			t.Fatalf("%s /wiki/mcp = 404, want route mounted", method)
		}
	}

	session := connectLocalMCP(t, router, "/wiki/mcp")
	assertToolNames(t, listAllToolNames(t, session), federatedToolNames())
}

func TestLocalMCPProtocol_PageMutationParity(t *testing.T) {
	runLocalMCPProtocolPageMutationParity(t)
}

func runLocalMCPProtocolPageMutationParity(t *testing.T) {
	w, _ := newLocalMCPTestWikiWithStorage(t)
	router := newLocalMCPTestRouter(w, httpinternal.RouterOptions{
		AuthDisabled:            true,
		PublicAccess:            true,
		AllowInsecure:           true,
		MaxAssetUploadSizeBytes: assets.DefaultMaxUploadSizeBytes,
		EnableWorkspaceSync:     true,
		MCPEnabled:              true,
		MCPToolListPageSize:     200,
	})
	session := connectLocalMCP(t, router, "/mcp")

	invalidCreateKindErr := callToolError(t, session, "wiki_create_page", map[string]any{
		"title": "Invalid Kind",
		"slug":  "invalid-kind",
		"kind":  "folder",
	})
	assertErrorContainsAny(t, "MCP create_page invalid kind", invalidCreateKindErr, "page_invalid_kind", "invalid kind")
	assertErrorDoesNotContainAny(t, "MCP create_page invalid kind", invalidCreateKindErr, "enum", "validating")
	paddedCreateKindErr := callToolError(t, session, "wiki_create_page", map[string]any{
		"title": "Padded Kind",
		"slug":  "padded-kind",
		"kind":  " page ",
	})
	assertErrorContainsAny(t, "MCP create_page padded kind", paddedCreateKindErr, "page_invalid_kind", "invalid kind")
	assertErrorDoesNotContainAny(t, "MCP create_page padded kind", paddedCreateKindErr, "enum", "validating")
	invalidCreateKindHTTP := postHTTPJSONBody(t, router, "/api/pages", map[string]any{
		"title": "Invalid Kind HTTP",
		"slug":  "invalid-kind-http",
		"kind":  "folder",
	}, http.StatusBadRequest)
	if !strings.Contains(invalidCreateKindHTTP, "page_invalid_kind") {
		t.Fatalf("HTTP create_page invalid kind error = %q, want page_invalid_kind", invalidCreateKindHTTP)
	}
	paddedCreateKindHTTP := postHTTPJSONBody(t, router, "/api/pages", map[string]any{
		"title": "Padded Kind HTTP",
		"slug":  "padded-kind-http",
		"kind":  " page ",
	}, http.StatusBadRequest)
	if !strings.Contains(paddedCreateKindHTTP, "page_invalid_kind") {
		t.Fatalf("HTTP create_page padded kind error = %q, want page_invalid_kind", paddedCreateKindHTTP)
	}
	nullKindCreated := nestedMap(t, callToolStructured(t, session, "wiki_create_page", map[string]any{
		"title": "Null Kind",
		"slug":  "null-kind",
		"kind":  nil,
	}), "page")
	if got := nullKindCreated["kind"]; got != "page" {
		t.Fatalf("MCP create_page null kind = %v, want page", got)
	}

	created := callToolStructured(t, session, "wiki_create_page", map[string]any{
		"title": "MCP Draft",
		"slug":  "mcp-draft",
		"kind":  "page",
	})
	createdPage := nestedMap(t, created, "page")
	pageID := stringField(t, createdPage, "id")
	version := stringField(t, createdPage, "version")

	httpPage := getHTTPPageByPath(t, router, "mcp-draft")
	if httpPage["id"] != pageID {
		t.Fatalf("HTTP page id = %v, want MCP-created id %q", httpPage["id"], pageID)
	}
	assertJSONEqual(t, "create_page HTTP page", createdPage, getHTTPPageByID(t, router, pageID))

	mcpCreateParent := nestedMap(t, callToolStructured(t, session, "wiki_create_page", map[string]any{
		"title": "MCP Create Parent",
		"slug":  "mcp-create-parent",
		"kind":  "section",
	}), "page")
	httpCreateParent := postHTTPJSON(t, router, "/api/pages", map[string]any{
		"title": "HTTP Create Parent",
		"slug":  "http-create-parent",
		"kind":  "section",
	}, http.StatusCreated)
	whitespaceParentCreateErr := callToolError(t, session, "wiki_create_page", map[string]any{
		"parentId": " ",
		"title":    "Whitespace Parent",
		"slug":     "whitespace-parent",
		"kind":     "page",
	})
	assertErrorContainsAny(t, "MCP create_page whitespace parentId", whitespaceParentCreateErr, "page_invalid_parent_id", "parent")
	whitespaceParentCreateHTTP := postHTTPJSONBody(t, router, "/api/pages", map[string]any{
		"parentId": " ",
		"title":    "Whitespace Parent HTTP",
		"slug":     "whitespace-parent-http",
		"kind":     "page",
	}, http.StatusBadRequest)
	if !strings.Contains(whitespaceParentCreateHTTP, "page_invalid_parent_id") {
		t.Fatalf("HTTP create_page whitespace parentId error = %q, want page_invalid_parent_id", whitespaceParentCreateHTTP)
	}
	paddedParentCreateErr := callToolError(t, session, "wiki_create_page", map[string]any{
		"parentId": " " + stringField(t, mcpCreateParent, "id") + " ",
		"title":    "Padded Parent",
		"slug":     "padded-parent",
		"kind":     "page",
	})
	assertErrorContainsAny(t, "MCP create_page padded parentId", paddedParentCreateErr, "page_invalid_parent_id", "parent")
	paddedParentCreateHTTP := postHTTPJSONBody(t, router, "/api/pages", map[string]any{
		"parentId": " " + stringField(t, httpCreateParent, "id") + " ",
		"title":    "Padded Parent HTTP",
		"slug":     "padded-parent-http",
		"kind":     "page",
	}, http.StatusBadRequest)
	if !strings.Contains(paddedParentCreateHTTP, "page_invalid_parent_id") {
		t.Fatalf("HTTP create_page padded parentId error = %q, want page_invalid_parent_id", paddedParentCreateHTTP)
	}
	mcpCreatedChild := nestedMap(t, callToolStructured(t, session, "wiki_create_page", map[string]any{
		"parentId": stringField(t, mcpCreateParent, "id"),
		"title":    "Created Child",
		"slug":     "created-child",
		"kind":     "section",
	}), "page")
	httpCreatedChild := postHTTPJSON(t, router, "/api/pages", map[string]any{
		"parentId": stringField(t, httpCreateParent, "id"),
		"title":    "Created Child",
		"slug":     "created-child",
		"kind":     "section",
	}, http.StatusCreated)
	assertPageState(t, "MCP create_page child", getHTTPPageByPath(t, router, "mcp-create-parent/created-child"), stringField(t, mcpCreatedChild, "id"), "Created Child", "created-child", "mcp-create-parent/created-child", "section", "")
	assertPageState(t, "HTTP create_page child", getHTTPPageByPath(t, router, "http-create-parent/created-child"), stringField(t, httpCreatedChild, "id"), "Created Child", "created-child", "http-create-parent/created-child", "section", "")
	recordHTTPMCPParity(t, "wiki_create_page", "POST /api/pages")

	content := "Hello from MCP\n"
	updated := callToolStructured(t, session, "wiki_update_page", map[string]any{
		"id":      pageID,
		"version": version,
		"title":   "MCP Draft Updated",
		"slug":    "mcp-draft",
		"content": content,
		"tags":    []any{"mcp", "Parity"},
		"properties": map[string]any{
			"status": "draft",
		},
	})
	updatedPage := nestedMap(t, updated, "page")
	if got := updatedPage["content"]; got != content {
		t.Fatalf("updated MCP content = %v, want %q", got, content)
	}

	httpPage = getHTTPPageByPath(t, router, "mcp-draft")
	if got := httpPage["title"]; got != "MCP Draft Updated" {
		t.Fatalf("HTTP title after MCP update = %v, want updated title", got)
	}
	if got := httpPage["content"]; got != content {
		t.Fatalf("HTTP content after MCP update = %v, want %q", got, content)
	}
	assertJSONEqual(t, "wiki_update_page HTTP page", updatedPage, getHTTPPageByID(t, router, pageID))
	if got := stringSliceField(t, httpPage, "tags"); strings.Join(got, ",") != "mcp,parity" {
		t.Fatalf("HTTP tags after MCP update = %v, want [mcp parity]", got)
	}
	props := nestedMap(t, httpPage, "properties")
	if got := props["status"]; got != "draft" {
		t.Fatalf("HTTP properties.status after MCP update = %v, want draft", got)
	}
	rawMCPMetadata := readPageMarkdownByRoutePath(t, w.GetRootDir(), "mcp-draft")
	mcpDoc := assertCanonicalPageMarkdown(t, "MCP update raw markdown", rawMCPMetadata)
	if len(mcpDoc.Metadata.Tags) != 2 || mcpDoc.Metadata.Tags[0] != "mcp" || mcpDoc.Metadata.Tags[1] != "parity" {
		t.Fatalf("MCP update raw tags = %#v, want [mcp parity]", mcpDoc.Metadata.Tags)
	}
	if mcpDoc.Metadata.Fields["status"] != "draft" {
		t.Fatalf("MCP update raw fields = %#v, want status=draft", mcpDoc.Metadata.Fields)
	}
	if !strings.Contains(rawMCPMetadata, "tags:") || !strings.Contains(rawMCPMetadata, "- mcp") || !strings.Contains(rawMCPMetadata, "status: draft") {
		t.Fatalf("MCP update raw markdown missing metadata frontmatter:\n%s", rawMCPMetadata)
	}
	httpMetadataPage := postHTTPJSON(t, router, "/api/pages", map[string]any{
		"title": "HTTP Metadata",
		"slug":  "http-metadata",
		"kind":  "page",
	}, http.StatusCreated)
	httpMetadataUpdated := updateHTTPPage(t, router, stringField(t, httpMetadataPage, "id"), map[string]any{
		"version": stringField(t, httpMetadataPage, "version"),
		"title":   "HTTP Metadata Updated",
		"slug":    "http-metadata",
		"content": "HTTP metadata content\n",
		"tags":    []string{"HTTP", "Metadata"},
		"properties": map[string]string{
			"status": "review",
		},
	})
	if got := strings.Join(stringSliceField(t, httpMetadataUpdated, "tags"), ","); got != "http,metadata" {
		t.Fatalf("HTTP update tags = %v, want http,metadata", got)
	}
	httpMetadataProps := nestedMap(t, httpMetadataUpdated, "properties")
	if got := httpMetadataProps["status"]; got != "review" {
		t.Fatalf("HTTP update properties.status = %v, want review", got)
	}
	rawHTTPMetadata := readPageMarkdownByRoutePath(t, w.GetRootDir(), "http-metadata")
	httpDoc := assertCanonicalPageMarkdown(t, "HTTP update raw markdown", rawHTTPMetadata)
	if len(httpDoc.Metadata.Tags) != 2 || httpDoc.Metadata.Tags[0] != "http" || httpDoc.Metadata.Tags[1] != "metadata" {
		t.Fatalf("HTTP update raw tags = %#v, want [http metadata]", httpDoc.Metadata.Tags)
	}
	if httpDoc.Metadata.Fields["status"] != "review" {
		t.Fatalf("HTTP update raw fields = %#v, want status=review", httpDoc.Metadata.Fields)
	}
	if !strings.Contains(rawHTTPMetadata, "tags:") || !strings.Contains(rawHTTPMetadata, "- http") || !strings.Contains(rawHTTPMetadata, "status: review") {
		t.Fatalf("HTTP update raw markdown missing metadata frontmatter:\n%s", rawHTTPMetadata)
	}
	recordHTTPMCPParity(t, "wiki_update_page", "PUT /api/pages/:id")

	metadataErr := callToolError(t, session, "wiki_update_page", map[string]any{
		"id":      pageID,
		"version": stringField(t, updatedPage, "version"),
		"title":   "MCP Draft Updated",
		"slug":    "mcp-draft",
		"content": content,
		"tags":    []any{"mcp", "MCP"},
		"properties": map[string]any{
			"leafwiki_hidden": "forbidden",
		},
	})
	if !strings.Contains(strings.ToLower(metadataErr), "validation") && !strings.Contains(strings.ToLower(metadataErr), "reserved") {
		t.Fatalf("MCP metadata validation error = %q, want validation detail", metadataErr)
	}

	csrfToken, csrfCookies := issueHTTPCSRF(t, router)
	staleHTTPBody := strings.NewReader(`{"version":"` + version + `","title":"MCP Draft Stale","slug":"mcp-draft","content":"stale"}`)
	staleHTTPReq := httptest.NewRequest(http.MethodPut, "/api/pages/"+pageID, staleHTTPBody)
	staleHTTPReq.Header.Set("Content-Type", "application/json")
	staleHTTPReq.Header.Set("X-CSRF-Token", csrfToken)
	for _, cookie := range csrfCookies {
		staleHTTPReq.AddCookie(cookie)
	}
	staleHTTPRec := httptest.NewRecorder()
	router.ServeHTTP(staleHTTPRec, staleHTTPReq)
	if staleHTTPRec.Code != http.StatusConflict {
		t.Fatalf("HTTP stale update status = %d, want 409: %s", staleHTTPRec.Code, staleHTTPRec.Body.String())
	}

	mcpErr := callToolError(t, session, "wiki_update_page", map[string]any{
		"id":      pageID,
		"version": version,
		"title":   "MCP Draft Stale",
		"slug":    "mcp-draft",
		"content": "stale",
	})
	assertPageVersionConflictParity(t, "stale wiki_update_page", mcpErr, staleHTTPRec.Body.String())

	search := callToolStructured(t, session, "wiki_search_pages", map[string]any{
		"q":      "Hello",
		"offset": float64(0),
		"limit":  float64(10),
	})
	httpSearch := getHTTPSearch(t, router, url.Values{
		"q":      {"Hello"},
		"offset": {"0"},
		"limit":  {"10"},
	})
	assertSearchResultsMatch(t, search, httpSearch)
	if got := search["count"]; got != float64(1) {
		t.Fatalf("MCP search count = %v, want 1", got)
	}
	items, ok := search["items"].([]any)
	if !ok || len(items) != 1 {
		t.Fatalf("MCP search items = %#v, want one item", search["items"])
	}
	item, ok := items[0].(map[string]any)
	if !ok {
		t.Fatalf("MCP search item has type %T", items[0])
	}
	if got := item["page_id"]; got != pageID {
		t.Fatalf("MCP search page_id = %v, want %q", got, pageID)
	}
	tagSearch := callToolStructured(t, session, "wiki_search_pages", map[string]any{
		"tags":   []any{"mcp"},
		"offset": float64(0),
		"limit":  float64(10),
	})
	tagHTTPSearch := getHTTPSearch(t, router, url.Values{
		"tags":   {"mcp"},
		"offset": {"0"},
		"limit":  {"10"},
	})
	assertSearchResultsMatch(t, tagSearch, tagHTTPSearch)

	for i := 1; i <= 2; i++ {
		extra := nestedMap(t, callToolStructured(t, session, "wiki_create_page", map[string]any{
			"title": "Hello Extra " + strconv.Itoa(i),
			"slug":  "hello-extra-" + strconv.Itoa(i),
			"kind":  "page",
		}), "page")
		extraContent := "Hello paginated search " + strconv.Itoa(i)
		callToolStructured(t, session, "wiki_update_page", map[string]any{
			"id":      stringField(t, extra, "id"),
			"version": stringField(t, extra, "version"),
			"title":   extra["title"],
			"slug":    extra["slug"],
			"content": extraContent,
			"tags":    []any{"mcp"},
		})
	}
	paginatedSearch := callToolStructured(t, session, "wiki_search_pages", map[string]any{
		"q":      "Hello",
		"offset": float64(0),
		"limit":  float64(1),
	})
	paginatedHTTP := getHTTPSearch(t, router, url.Values{
		"q":      {"Hello"},
		"offset": {"0"},
		"limit":  {"1"},
	})
	assertSearchResultsMatch(t, paginatedSearch, paginatedHTTP)
	if paginatedSearch["hasMore"] != true {
		t.Fatalf("paginated MCP search hasMore = %v, want true", paginatedSearch["hasMore"])
	}
	recordHTTPMCPParity(t, "wiki_search_pages", "GET /api/search")
}

func TestLocalMCPProtocol_PageOperationParity(t *testing.T) {
	runLocalMCPProtocolPageOperationParity(t)
}

func runLocalMCPProtocolPageOperationParity(t *testing.T) {
	w := newLocalMCPTestWiki(t, false)
	router := newLocalMCPTestRouter(w, httpinternal.RouterOptions{
		AuthDisabled:            true,
		PublicAccess:            true,
		AllowInsecure:           true,
		MarkdownLinkRootPrefix:  "/docs",
		MaxAssetUploadSizeBytes: assets.DefaultMaxUploadSizeBytes,
		EnableWorkspaceSync:     true,
		MCPEnabled:              true,
		MCPToolListPageSize:     200,
	})
	session := connectLocalMCP(t, router, "/mcp")

	current := callToolStructured(t, session, "wiki_get_current_user", nil)
	user := nestedMap(t, current, "user")
	if user["username"] != "public-editor" || user["role"] != "editor" {
		t.Fatalf("current user = %#v, want public-editor editor", user)
	}
	httpUser := getHTTPMap(t, router, "/api/auth/me")
	assertJSONEqual(t, "wiki_get_current_user", user, httpUser)
	recordHTTPMCPParity(t, "wiki_get_current_user", "GET /api/auth/me")
	config := callToolStructured(t, session, "wiki_get_config", nil)
	if config["authDisabled"] != true {
		t.Fatalf("config authDisabled = %v, want true", config["authDisabled"])
	}
	if config["maxAssetUploadSizeBytes"] != float64(assets.DefaultMaxUploadSizeBytes) {
		t.Fatalf("config maxAssetUploadSizeBytes = %v, want default", config["maxAssetUploadSizeBytes"])
	}
	if config["enableWorkspaceSync"] != true {
		t.Fatalf("config enableWorkspaceSync = %v, want true", config["enableWorkspaceSync"])
	}
	if config["markdownLinkRootPrefix"] != "/docs" {
		t.Fatalf("config markdownLinkRootPrefix = %v, want /docs", config["markdownLinkRootPrefix"])
	}
	httpConfig := getHTTPMap(t, router, "/api/config")
	assertMapFieldsEqual(t, "wiki_get_config", config, httpConfig, []string{
		"publicAccess",
		"hideLinkMetadataSection",
		"authDisabled",
		"basePath",
		"markdownLinkRootPrefix",
		"maxAssetUploadSizeBytes",
		"enableWorkspaceSync",
		"enableLinkRefactor",
		"httpRemoteUserEnabled",
		"httpRemoteUserLogoutUrl",
	})
	recordHTTPMCPParity(t, "wiki_get_config", "GET /api/config")

	slug := callToolStructured(t, session, "wiki_suggest_slug", map[string]any{"title": "Parent Section"})
	httpSlug := getHTTPMap(t, router, "/api/pages/slug-suggestion?title=Parent+Section")
	assertJSONEqual(t, "wiki_suggest_slug", slug, httpSlug)
	recordHTTPMCPParity(t, "wiki_suggest_slug", "GET /api/pages/slug-suggestion")
	if got := slug["slug"]; got != "parent-section" {
		t.Fatalf("suggest_slug = %v, want parent-section", got)
	}
	blankSlugErr := callToolError(t, session, "wiki_suggest_slug", map[string]any{"title": "   "})
	if !strings.Contains(strings.ToLower(blankSlugErr), "title") {
		t.Fatalf("MCP blank suggest_slug error = %q, want title detail", blankSlugErr)
	}
	blankSlugHTTP := getHTTPStatus(t, router, "/api/pages/slug-suggestion?title=+++",
		http.StatusBadRequest)
	if !strings.Contains(blankSlugHTTP, wikipages.ErrCodePageMissingTitle) {
		t.Fatalf("HTTP blank suggest_slug error = %q, want %s", blankSlugHTTP, wikipages.ErrCodePageMissingTitle)
	}
	punctuationSlugErr := callToolError(t, session, "wiki_suggest_slug", map[string]any{"title": "!!!"})
	if !strings.Contains(strings.ToLower(punctuationSlugErr), "title") {
		t.Fatalf("MCP punctuation-only suggest_slug error = %q, want title detail", punctuationSlugErr)
	}
	punctuationSlugHTTP := getHTTPStatus(t, router, "/api/pages/slug-suggestion?title=%21%21%21",
		http.StatusBadRequest)
	if !strings.Contains(punctuationSlugHTTP, wikipages.ErrCodePageInvalidTitle) {
		t.Fatalf("HTTP punctuation-only suggest_slug error = %q, want %s", punctuationSlugHTTP, wikipages.ErrCodePageInvalidTitle)
	}

	parent := nestedMap(t, callToolStructured(t, session, "wiki_create_page", map[string]any{
		"title": "Parent Section",
		"slug":  "parent-section",
		"kind":  "section",
	}), "page")
	parentID := stringField(t, parent, "id")
	parentViaGet := nestedMap(t, callToolStructured(t, session, "wiki_get_page", map[string]any{"id": parentID}), "page")
	assertJSONEqual(t, "wiki_get_page", parentViaGet, getHTTPPageByID(t, router, parentID))
	recordHTTPMCPParity(t, "wiki_get_page", "GET /api/pages/:id")
	treeResult := callToolStructured(t, session, "wiki_get_tree", map[string]any{"depth": float64(1)})
	if treeResult["tree"] == nil {
		t.Fatalf("wiki_get_tree returned no tree: %#v", treeResult)
	}
	httpTree := getHTTPMap(t, router, "/api/tree?depth=1")
	assertJSONEqual(t, "wiki_get_tree", treeResult["tree"], httpTree)
	recordHTTPMCPParity(t, "wiki_get_tree", "GET /api/tree")

	pageByPath := nestedMap(t, callToolStructured(t, session, "wiki_get_page_by_path", map[string]any{"path": "parent-section"}), "page")
	httpPageByPath := getHTTPPageByPath(t, router, "parent-section")
	assertJSONEqual(t, "wiki_get_page_by_path", pageByPath, httpPageByPath)
	leadingSlashPageByPath := nestedMap(t, callToolStructured(t, session, "wiki_get_page_by_path", map[string]any{"path": "/parent-section"}), "page")
	assertJSONEqual(t, "wiki_get_page_by_path leading slash", leadingSlashPageByPath, httpPageByPath)
	recordHTTPMCPParity(t, "wiki_get_page_by_path", "GET /api/pages/by-path")
	blankPathErr := callToolError(t, session, "wiki_get_page_by_path", map[string]any{"path": "  "})
	if !strings.Contains(blankPathErr, wikipages.ErrCodePageMissingPath) && !strings.Contains(strings.ToLower(blankPathErr), "missing path") {
		t.Fatalf("MCP blank get_page_by_path error = %q, want %s", blankPathErr, wikipages.ErrCodePageMissingPath)
	}
	blankPathHTTP := getHTTPStatus(t, router, "/api/pages/by-path?path=++", http.StatusBadRequest)
	if !strings.Contains(blankPathHTTP, wikipages.ErrCodePageMissingPath) {
		t.Fatalf("HTTP blank get_page_by_path error = %q, want %s", blankPathHTTP, wikipages.ErrCodePageMissingPath)
	}
	for _, invalidPath := range []string{"docs//intro", "docs/.", "docs/..", `docs\..\secret`} {
		mcpPathErr := callToolError(t, session, "wiki_get_page_by_path", map[string]any{"path": invalidPath})
		if !strings.Contains(mcpPathErr, "page_invalid_path") && !strings.Contains(strings.ToLower(mcpPathErr), "invalid path") {
			t.Fatalf("MCP invalid get_page_by_path path %q error = %q, want invalid path detail", invalidPath, mcpPathErr)
		}
		httpPathErr := getHTTPStatus(t, router, "/api/pages/by-path?path="+url.QueryEscape(invalidPath), http.StatusBadRequest)
		if !strings.Contains(httpPathErr, "page_invalid_path") {
			t.Fatalf("HTTP invalid get_page_by_path path %q error = %q, want page_invalid_path", invalidPath, httpPathErr)
		}
	}

	childA := nestedMap(t, callToolStructured(t, session, "wiki_create_page", map[string]any{
		"parentId": parentID,
		"title":    "Child A",
		"slug":     "child-a",
		"kind":     "page",
	}), "page")
	childB := nestedMap(t, callToolStructured(t, session, "wiki_create_page", map[string]any{
		"parentId": parentID,
		"title":    "Child B",
		"slug":     "child-b",
		"kind":     "page",
	}), "page")

	callToolStructured(t, session, "wiki_sort_pages", map[string]any{
		"parentId":   parentID,
		"orderedIds": []any{stringField(t, childB, "id"), stringField(t, childA, "id")},
	})
	httpSort := putHTTPJSON(t, router, "/api/pages/"+parentID+"/sort", map[string]any{
		"orderedIds": []string{stringField(t, childB, "id"), stringField(t, childA, "id")},
	}, http.StatusOK)
	mcpSort := callToolStructured(t, session, "wiki_sort_pages", map[string]any{
		"parentId":   parentID,
		"orderedIds": []any{stringField(t, childB, "id"), stringField(t, childA, "id")},
	})
	assertJSONEqual(t, "wiki_sort_pages", mcpSort, httpSort)
	parentAfterSort := nestedMap(t, callToolStructured(t, session, "wiki_get_page", map[string]any{"id": parentID}), "page")
	assertChildOrder(t, "sort_pages shared parent", parentAfterSort, stringField(t, childB, "id"), stringField(t, childA, "id"))
	mcpSortParent := nestedMap(t, callToolStructured(t, session, "wiki_create_page", map[string]any{
		"title": "MCP Sort Parent",
		"slug":  "mcp-sort-parent",
		"kind":  "section",
	}), "page")
	mcpSortA := nestedMap(t, callToolStructured(t, session, "wiki_create_page", map[string]any{
		"parentId": stringField(t, mcpSortParent, "id"),
		"title":    "MCP Sort A",
		"slug":     "mcp-sort-a",
		"kind":     "page",
	}), "page")
	mcpSortB := nestedMap(t, callToolStructured(t, session, "wiki_create_page", map[string]any{
		"parentId": stringField(t, mcpSortParent, "id"),
		"title":    "MCP Sort B",
		"slug":     "mcp-sort-b",
		"kind":     "page",
	}), "page")
	httpSortParent := postHTTPJSON(t, router, "/api/pages", map[string]any{
		"title": "HTTP Sort Parent",
		"slug":  "http-sort-parent",
		"kind":  "section",
	}, http.StatusCreated)
	httpSortA := postHTTPJSON(t, router, "/api/pages", map[string]any{
		"parentId": stringField(t, httpSortParent, "id"),
		"title":    "HTTP Sort A",
		"slug":     "http-sort-a",
		"kind":     "page",
	}, http.StatusCreated)
	httpSortB := postHTTPJSON(t, router, "/api/pages", map[string]any{
		"parentId": stringField(t, httpSortParent, "id"),
		"title":    "HTTP Sort B",
		"slug":     "http-sort-b",
		"kind":     "page",
	}, http.StatusCreated)
	callToolStructured(t, session, "wiki_sort_pages", map[string]any{
		"parentId":   stringField(t, mcpSortParent, "id"),
		"orderedIds": []any{stringField(t, mcpSortB, "id"), stringField(t, mcpSortA, "id")},
	})
	putHTTPJSON(t, router, "/api/pages/"+stringField(t, httpSortParent, "id")+"/sort", map[string]any{
		"orderedIds": []string{stringField(t, httpSortB, "id"), stringField(t, httpSortA, "id")},
	}, http.StatusOK)
	assertChildOrder(t, "MCP sort_pages parent", getHTTPPageByPath(t, router, "mcp-sort-parent"), stringField(t, mcpSortB, "id"), stringField(t, mcpSortA, "id"))
	assertChildOrder(t, "HTTP sort_pages parent", getHTTPPageByPath(t, router, "http-sort-parent"), stringField(t, httpSortB, "id"), stringField(t, httpSortA, "id"))
	recordHTTPMCPParity(t, "wiki_sort_pages", "PUT /api/pages/:id/sort")
	parentByAlias := nestedMap(t, callToolStructured(t, session, "wiki_get_page", map[string]any{"pageId": parentID}), "page")
	if parentByAlias["id"] != parentID {
		t.Fatalf("wiki_get_page pageId alias returned id = %v, want %q", parentByAlias["id"], parentID)
	}

	ensured := nestedMap(t, callToolStructured(t, session, "wiki_ensure_page", map[string]any{
		"path":  "parent-section/ensured",
		"title": "Ensured Page",
		"kind":  "page",
	}), "page")
	ensuredID := stringField(t, ensured, "id")
	httpEnsured := postHTTPJSON(t, router, "/api/pages/ensure", map[string]any{
		"path":  "parent-section/ensured",
		"title": "Ensured Page",
		"kind":  "page",
	}, http.StatusOK)
	assertJSONEqual(t, "wiki_ensure_page", ensured, httpEnsured)
	mcpEnsuredIndependent := nestedMap(t, callToolStructured(t, session, "wiki_ensure_page", map[string]any{
		"path":  "parent-section/ensured-mcp",
		"title": "Ensured Independent",
		"kind":  "section",
	}), "page")
	httpEnsuredIndependent := postHTTPJSON(t, router, "/api/pages/ensure", map[string]any{
		"path":  "parent-section/ensured-http",
		"title": "Ensured Independent",
		"kind":  "section",
	}, http.StatusOK)
	assertPageState(t, "MCP ensure_page independent", getHTTPPageByPath(t, router, "parent-section/ensured-mcp"), stringField(t, mcpEnsuredIndependent, "id"), "Ensured Independent", "ensured-mcp", "parent-section/ensured-mcp", "section", "")
	assertPageState(t, "HTTP ensure_page independent", getHTTPPageByPath(t, router, "parent-section/ensured-http"), stringField(t, httpEnsuredIndependent, "id"), "Ensured Independent", "ensured-http", "parent-section/ensured-http", "section", "")
	nullKindEnsured := nestedMap(t, callToolStructured(t, session, "wiki_ensure_page", map[string]any{
		"path":  "parent-section/ensured-null-kind",
		"title": "Ensured Null Kind",
		"kind":  nil,
	}), "page")
	if got := nullKindEnsured["kind"]; got != "page" {
		t.Fatalf("MCP ensure_page null kind = %v, want page", got)
	}

	mcpPageBase := postHTTPJSON(t, router, "/api/pages", map[string]any{
		"parentId": parentID,
		"title":    "MCP Section Twin Base",
		"slug":     "mcp-section-twin",
		"kind":     "page",
	}, http.StatusCreated)
	mcpEnsuredSectionTwin := nestedMap(t, callToolStructured(t, session, "wiki_ensure_page", map[string]any{
		"path":  "parent-section/mcp-section-twin",
		"title": "MCP Ensured Section Twin",
		"kind":  "section",
	}), "page")
	if stringField(t, mcpEnsuredSectionTwin, "id") == stringField(t, mcpPageBase, "id") {
		t.Fatalf("MCP ensure_page returned existing page %q instead of section twin", stringField(t, mcpPageBase, "id"))
	}
	if got := mcpEnsuredSectionTwin["kind"]; got != "section" {
		t.Fatalf("MCP ensure_page section twin kind = %v, want section", got)
	}

	httpPageBase := postHTTPJSON(t, router, "/api/pages", map[string]any{
		"parentId": parentID,
		"title":    "HTTP Section Twin Base",
		"slug":     "http-section-twin",
		"kind":     "page",
	}, http.StatusCreated)
	httpEnsuredSectionTwin := postHTTPJSON(t, router, "/api/pages/ensure", map[string]any{
		"path":  "parent-section/http-section-twin",
		"title": "HTTP Ensured Section Twin",
		"kind":  "section",
	}, http.StatusOK)
	if stringField(t, httpEnsuredSectionTwin, "id") == stringField(t, httpPageBase, "id") {
		t.Fatalf("HTTP ensure returned existing page %q instead of section twin", stringField(t, httpPageBase, "id"))
	}
	if got := httpEnsuredSectionTwin["kind"]; got != "section" {
		t.Fatalf("HTTP ensure section twin kind = %v, want section", got)
	}
	recordHTTPMCPParity(t, "wiki_ensure_page", "POST /api/pages/ensure")

	lookup := callToolStructured(t, session, "wiki_lookup_path", map[string]any{"path": "parent-section/ensured"})
	httpLookup := getHTTPMap(t, router, "/api/pages/lookup?path=parent-section%2Fensured")
	assertJSONEqual(t, "wiki_lookup_path", lookup["lookup"], httpLookup)
	recordHTTPMCPParity(t, "wiki_lookup_path", "GET /api/pages/lookup")
	if lookup["lookup"] == nil {
		t.Fatalf("lookup_path returned no lookup: %#v", lookup)
	}

	pageTwin := postHTTPJSON(t, router, "/api/pages", map[string]any{
		"parentId": parentID,
		"title":    "Lookup Page Twin",
		"slug":     "lookup-twin",
		"kind":     "page",
	}, http.StatusCreated)
	sectionTwin := postHTTPJSON(t, router, "/api/pages", map[string]any{
		"parentId": parentID,
		"title":    "Lookup Section Twin",
		"slug":     "lookup-twin",
		"kind":     "section",
	}, http.StatusCreated)
	mcpLookupPageTwin := nestedMap(t, callToolStructured(t, session, "wiki_lookup_path", map[string]any{
		"path": "parent-section/lookup-twin",
		"kind": "page",
	}), "lookup")
	httpLookupPageTwin := getHTTPMap(t, router, "/api/pages/lookup?path=parent-section%2Flookup-twin&kind=page")
	assertJSONEqual(t, "wiki_lookup_path page twin", mcpLookupPageTwin, httpLookupPageTwin)
	assertLookupFinalID(t, "wiki_lookup_path page twin", mcpLookupPageTwin, stringField(t, pageTwin, "id"), "page")

	mcpLookupSectionTwin := nestedMap(t, callToolStructured(t, session, "wiki_lookup_path", map[string]any{
		"path": "parent-section/lookup-twin",
		"kind": "section",
	}), "lookup")
	httpLookupSectionTwin := getHTTPMap(t, router, "/api/pages/lookup?path=parent-section%2Flookup-twin&kind=section")
	assertJSONEqual(t, "wiki_lookup_path section twin", mcpLookupSectionTwin, httpLookupSectionTwin)
	assertLookupFinalID(t, "wiki_lookup_path section twin", mcpLookupSectionTwin, stringField(t, sectionTwin, "id"), "section")

	invalidLookupKindErr := callToolError(t, session, "wiki_lookup_path", map[string]any{
		"path": "parent-section/lookup-twin",
		"kind": "folder",
	})
	assertErrorContainsAny(t, "MCP lookup_path invalid kind", invalidLookupKindErr, "page_invalid_kind", "invalid kind")
	invalidLookupKindHTTP := getHTTPStatus(t, router, "/api/pages/lookup?path=parent-section%2Flookup-twin&kind=folder", http.StatusBadRequest)
	if !strings.Contains(invalidLookupKindHTTP, "page_invalid_kind") {
		t.Fatalf("HTTP lookup_path invalid kind error = %q, want page_invalid_kind", invalidLookupKindHTTP)
	}

	permalink := callToolStructured(t, session, "wiki_resolve_permalink", map[string]any{"id": ensuredID})
	httpPermalink := getHTTPMap(t, router, "/api/pages/permalink/"+ensuredID)
	assertJSONEqual(t, "wiki_resolve_permalink", permalink["target"], httpPermalink)
	recordHTTPMCPParity(t, "wiki_resolve_permalink", "GET /api/pages/permalink/:id")
	target := nestedMap(t, permalink, "target")
	if got := target["path"]; got != "parent-section/ensured" {
		t.Fatalf("resolve_permalink path = %v, want parent-section/ensured", got)
	}
	permalinkByAlias := callToolStructured(t, session, "wiki_resolve_permalink", map[string]any{"pageId": ensuredID})
	targetByAlias := nestedMap(t, permalinkByAlias, "target")
	if got := targetByAlias["path"]; got != "parent-section/ensured" {
		t.Fatalf("resolve_permalink pageId alias path = %v, want parent-section/ensured", got)
	}

	invalidEnsureKindErr := callToolError(t, session, "wiki_ensure_page", map[string]any{
		"path":  "parent-section/invalid-kind",
		"title": "Invalid Ensure Kind",
		"kind":  "folder",
	})
	assertErrorContainsAny(t, "MCP ensure_page invalid kind", invalidEnsureKindErr, "page_invalid_kind", "invalid kind", "enum")
	invalidEnsureKindHTTP := postHTTPJSONBody(t, router, "/api/pages/ensure", map[string]any{
		"path":  "parent-section/invalid-kind-http",
		"title": "Invalid Ensure Kind HTTP",
		"kind":  "folder",
	}, http.StatusBadRequest)
	if !strings.Contains(invalidEnsureKindHTTP, "page_invalid_kind") {
		t.Fatalf("HTTP ensure_page invalid kind error = %q, want page_invalid_kind", invalidEnsureKindHTTP)
	}

	mcpMove := callToolStructured(t, session, "wiki_move_page", map[string]any{
		"id":       stringField(t, childA, "id"),
		"version":  stringField(t, childA, "version"),
		"parentId": "",
	})
	httpMove := putHTTPJSON(t, router, "/api/pages/"+stringField(t, childB, "id")+"/move", map[string]any{
		"version":  stringField(t, childB, "version"),
		"parentId": "",
	}, http.StatusOK)
	assertJSONEqual(t, "wiki_move_page", mcpMove, httpMove)
	httpMoved := getHTTPPageByPath(t, router, "child-a")
	assertPageState(t, "MCP moved child A", httpMoved, stringField(t, childA, "id"), "Child A", "child-a", "child-a", "page", "")
	httpMovedB := getHTTPPageByPath(t, router, "child-b")
	assertPageState(t, "HTTP moved child B", httpMovedB, stringField(t, childB, "id"), "Child B", "child-b", "child-b", "page", "")
	parentAfterMove := getHTTPPageByPath(t, router, "parent-section")
	assertChildrenDoNotContain(t, "parent after move", parentAfterMove, stringField(t, childA, "id"), stringField(t, childB, "id"))
	staleMoveErr := callToolError(t, session, "wiki_move_page", map[string]any{
		"id":       stringField(t, childA, "id"),
		"version":  stringField(t, childA, "version"),
		"parentId": parentID,
	})
	staleMoveHTTP := putHTTPJSONBody(t, router, "/api/pages/"+stringField(t, childA, "id")+"/move", map[string]any{
		"version":  stringField(t, childA, "version"),
		"parentId": parentID,
	}, http.StatusConflict)
	assertPageVersionConflictParity(t, "stale move_page", staleMoveErr, staleMoveHTTP)
	mcpWhitespaceMove := nestedMap(t, callToolStructured(t, session, "wiki_create_page", map[string]any{
		"title": "MCP Whitespace Move",
		"slug":  "mcp-whitespace-move",
		"kind":  "page",
	}), "page")
	whitespaceMoveErr := callToolError(t, session, "wiki_move_page", map[string]any{
		"id":       stringField(t, mcpWhitespaceMove, "id"),
		"version":  stringField(t, mcpWhitespaceMove, "version"),
		"parentId": " ",
	})
	assertErrorContainsAny(t, "MCP whitespace move parentId", whitespaceMoveErr, "page_invalid_parent_id", "parent")
	httpWhitespaceMove := postHTTPJSON(t, router, "/api/pages", map[string]any{
		"title": "HTTP Whitespace Move",
		"slug":  "http-whitespace-move",
		"kind":  "page",
	}, http.StatusCreated)
	whitespaceMoveHTTP := putHTTPJSONBody(t, router, "/api/pages/"+stringField(t, httpWhitespaceMove, "id")+"/move", map[string]any{
		"version":  stringField(t, httpWhitespaceMove, "version"),
		"parentId": " ",
	}, http.StatusBadRequest)
	if !strings.Contains(whitespaceMoveHTTP, "page_invalid_parent_id") {
		t.Fatalf("HTTP whitespace move parentId error = %q, want page_invalid_parent_id", whitespaceMoveHTTP)
	}
	mcpMissingParentMove := nestedMap(t, callToolStructured(t, session, "wiki_create_page", map[string]any{
		"parentId": parentID,
		"title":    "MCP Missing Parent Move",
		"slug":     "mcp-missing-parent-move",
		"kind":     "page",
	}), "page")
	missingParentMove := callToolStructured(t, session, "wiki_move_page", map[string]any{
		"id":      stringField(t, mcpMissingParentMove, "id"),
		"version": stringField(t, mcpMissingParentMove, "version"),
	})
	if missingParentMove["message"] != "Page moved" {
		t.Fatalf("MCP move_page missing parentId message = %v, want Page moved", missingParentMove["message"])
	}
	assertPageState(t, "MCP move_page missing parentId", getHTTPPageByPath(t, router, "mcp-missing-parent-move"), stringField(t, mcpMissingParentMove, "id"), "MCP Missing Parent Move", "mcp-missing-parent-move", "mcp-missing-parent-move", "page", "")
	recordHTTPMCPParity(t, "wiki_move_page", "PUT /api/pages/:id/move")

	convertMe := nestedMap(t, callToolStructured(t, session, "wiki_create_page", map[string]any{
		"title": "Convert Me",
		"slug":  "convert-me",
		"kind":  "section",
	}), "page")
	mcpConvert := callToolStructured(t, session, "wiki_convert_page", map[string]any{
		"id":         stringField(t, convertMe, "id"),
		"version":    stringField(t, convertMe, "version"),
		"targetKind": "page",
	})
	if mcpConvert["message"] != "Page converted" {
		t.Fatalf("convert_page message = %v, want Page converted", mcpConvert["message"])
	}
	httpConverted := getHTTPPageByPath(t, router, "convert-me")
	if httpConverted["kind"] != "page" {
		t.Fatalf("converted kind = %v, want page", httpConverted["kind"])
	}
	assertPageState(t, "MCP converted page", httpConverted, stringField(t, convertMe, "id"), "Convert Me", "convert-me", "convert-me", "page", "")
	convertHTTP := nestedMap(t, callToolStructured(t, session, "wiki_create_page", map[string]any{
		"title": "Convert HTTP",
		"slug":  "convert-http",
		"kind":  "section",
	}), "page")
	postHTTPJSONNoContent(t, router, "/api/pages/convert/"+stringField(t, convertHTTP, "id"), map[string]any{
		"version":    stringField(t, convertHTTP, "version"),
		"targetKind": "page",
	}, http.StatusNoContent)
	httpConvertedPeer := getHTTPPageByPath(t, router, "convert-http")
	assertPageState(t, "HTTP converted page", httpConvertedPeer, stringField(t, convertHTTP, "id"), "Convert HTTP", "convert-http", "convert-http", "page", "")
	staleConvertErr := callToolError(t, session, "wiki_convert_page", map[string]any{
		"id":         stringField(t, convertMe, "id"),
		"version":    stringField(t, convertMe, "version"),
		"targetKind": "section",
	})
	staleConvertHTTP := postHTTPJSONBody(t, router, "/api/pages/convert/"+stringField(t, convertMe, "id"), map[string]any{
		"version":    stringField(t, convertMe, "version"),
		"targetKind": "section",
	}, http.StatusConflict)
	assertPageVersionConflictParity(t, "stale convert_page", staleConvertErr, staleConvertHTTP)
	invalidConvertErr := callToolError(t, session, "wiki_convert_page", map[string]any{
		"id":         stringField(t, convertMe, "id"),
		"version":    stringField(t, httpConverted, "version"),
		"targetKind": "folder",
	})
	assertErrorContainsAny(t, "MCP invalid convert_page targetKind", invalidConvertErr, wikipages.ErrCodePageInvalidTargetKind, "invalid target kind", "targetkind")
	assertErrorDoesNotContainAny(t, "MCP invalid convert_page targetKind", invalidConvertErr, "enum", "validating")
	paddedConvertErr := callToolError(t, session, "wiki_convert_page", map[string]any{
		"id":         stringField(t, convertMe, "id"),
		"version":    stringField(t, httpConverted, "version"),
		"targetKind": " page ",
	})
	assertErrorContainsAny(t, "MCP padded convert_page targetKind", paddedConvertErr, wikipages.ErrCodePageInvalidTargetKind, "invalid target kind", "targetkind")
	assertErrorDoesNotContainAny(t, "MCP padded convert_page targetKind", paddedConvertErr, "enum", "validating")
	invalidConvertHTTP := postHTTPJSONBody(t, router, "/api/pages/convert/"+stringField(t, convertMe, "id"), map[string]any{
		"version":    stringField(t, httpConverted, "version"),
		"targetKind": "folder",
	}, http.StatusBadRequest)
	if !strings.Contains(invalidConvertHTTP, wikipages.ErrCodePageInvalidTargetKind) {
		t.Fatalf("HTTP invalid convert_page targetKind error = %q, want %s", invalidConvertHTTP, wikipages.ErrCodePageInvalidTargetKind)
	}
	paddedConvertHTTP := postHTTPJSONBody(t, router, "/api/pages/convert/"+stringField(t, convertMe, "id"), map[string]any{
		"version":    stringField(t, httpConverted, "version"),
		"targetKind": " page ",
	}, http.StatusBadRequest)
	if !strings.Contains(paddedConvertHTTP, wikipages.ErrCodePageInvalidTargetKind) {
		t.Fatalf("HTTP padded convert_page targetKind error = %q, want %s", paddedConvertHTTP, wikipages.ErrCodePageInvalidTargetKind)
	}
	recordHTTPMCPParity(t, "wiki_convert_page", "POST /api/pages/convert/:id")

	copied := nestedMap(t, callToolStructured(t, session, "wiki_copy_page", map[string]any{
		"id":    stringField(t, childA, "id"),
		"title": "Child A Copy",
		"slug":  "child-a-copy",
	}), "page")
	httpCopied := postHTTPJSON(t, router, "/api/pages/copy/"+stringField(t, childA, "id"), map[string]any{
		"title": "Child A Copy",
		"slug":  "child-a-http-copy",
	}, http.StatusCreated)
	whitespaceCopyErr := callToolError(t, session, "wiki_copy_page", map[string]any{
		"id":             stringField(t, childA, "id"),
		"targetParentId": " ",
		"title":          "Whitespace Copy",
		"slug":           "whitespace-copy",
	})
	assertErrorContainsAny(t, "MCP copy_page whitespace targetParentId", whitespaceCopyErr, "page_invalid_parent_id", "parent")
	whitespaceCopyHTTP := postHTTPJSONBody(t, router, "/api/pages/copy/"+stringField(t, childB, "id"), map[string]any{
		"targetParentId": " ",
		"title":          "Whitespace Copy HTTP",
		"slug":           "whitespace-copy-http",
	}, http.StatusBadRequest)
	if !strings.Contains(whitespaceCopyHTTP, "page_invalid_parent_id") {
		t.Fatalf("HTTP copy_page whitespace targetParentId error = %q, want page_invalid_parent_id", whitespaceCopyHTTP)
	}
	paddedCopyErr := callToolError(t, session, "wiki_copy_page", map[string]any{
		"id":             stringField(t, childA, "id"),
		"targetParentId": " " + parentID + " ",
		"title":          "Padded Copy",
		"slug":           "padded-copy",
	})
	assertErrorContainsAny(t, "MCP copy_page padded targetParentId", paddedCopyErr, "page_invalid_parent_id", "parent")
	paddedCopyHTTP := postHTTPJSONBody(t, router, "/api/pages/copy/"+stringField(t, childB, "id"), map[string]any{
		"targetParentId": " " + parentID + " ",
		"title":          "Padded Copy HTTP",
		"slug":           "padded-copy-http",
	}, http.StatusBadRequest)
	if !strings.Contains(paddedCopyHTTP, "page_invalid_parent_id") {
		t.Fatalf("HTTP copy_page padded targetParentId error = %q, want page_invalid_parent_id", paddedCopyHTTP)
	}
	assertMapFieldsEqual(t, "wiki_copy_page", copied, httpCopied, []string{"title", "kind", "content"})
	assertPageState(t, "MCP copied page", getHTTPPageByPath(t, router, "child-a-copy"), stringField(t, copied, "id"), "Child A Copy", "child-a-copy", "child-a-copy", "page", "")
	assertPageState(t, "HTTP copied page", getHTTPPageByPath(t, router, "child-a-http-copy"), stringField(t, httpCopied, "id"), "Child A Copy", "child-a-http-copy", "child-a-http-copy", "page", "")
	assertPageState(t, "copy_page source preserved", getHTTPPageByPath(t, router, "child-a"), stringField(t, childA, "id"), "Child A", "child-a", "child-a", "page", "")
	recordHTTPMCPParity(t, "wiki_copy_page", "POST /api/pages/copy/:id")

	missingDeleteVersionErr := callToolError(t, session, "wiki_delete_page", map[string]any{"id": stringField(t, copied, "id")})
	if !strings.Contains(strings.ToLower(missingDeleteVersionErr), "version") {
		t.Fatalf("MCP delete without version error = %q, want version detail", missingDeleteVersionErr)
	}
	missingDeleteHTTP := deleteHTTPStatus(t, router, "/api/pages/"+stringField(t, copied, "id"), http.StatusBadRequest)
	if !strings.Contains(strings.ToLower(missingDeleteHTTP), "version") {
		t.Fatalf("HTTP delete without version error = %q, want version detail", missingDeleteHTTP)
	}

	staleDelete := nestedMap(t, callToolStructured(t, session, "wiki_update_page", map[string]any{
		"id":      stringField(t, copied, "id"),
		"version": stringField(t, copied, "version"),
		"title":   "Child A Copy Updated",
		"slug":    "child-a-copy",
		"content": "updated",
	}), "page")
	staleDeleteErr := callToolError(t, session, "wiki_delete_page", map[string]any{
		"id":      stringField(t, copied, "id"),
		"version": stringField(t, copied, "version"),
	})
	staleDeleteHTTP := deleteHTTPStatus(t, router, "/api/pages/"+stringField(t, copied, "id")+"?version="+url.QueryEscape(stringField(t, copied, "version")), http.StatusConflict)
	assertPageVersionConflictParity(t, "stale delete_page", staleDeleteErr, staleDeleteHTTP)

	mcpDeletedPage := callToolStructured(t, session, "wiki_delete_page", map[string]any{
		"id":      stringField(t, copied, "id"),
		"version": stringField(t, staleDelete, "version"),
	})
	httpDeletedPageBody := deleteHTTPStatus(t, router, "/api/pages/"+stringField(t, httpCopied, "id")+"?version="+url.QueryEscape(stringField(t, httpCopied, "version")), http.StatusOK)
	assertJSONEqual(t, "wiki_delete_page", mcpDeletedPage, decodeJSONMap(t, "HTTP delete_page", []byte(httpDeletedPageBody)))
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/pages/by-path?path=child-a-copy", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("GET deleted copy = %d, want 404", rec.Code)
	}
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/pages/by-path?path=child-a-http-copy", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("GET HTTP-deleted copy = %d, want 404", rec.Code)
	}
	recordHTTPMCPParity(t, "wiki_delete_page", "DELETE /api/pages/:id")
}

func TestLocalMCPProtocol_IndexAndAssetParity(t *testing.T) {
	runLocalMCPProtocolIndexAndAssetParity(t)
}

func runLocalMCPProtocolIndexAndAssetParity(t *testing.T) {
	w := newLocalMCPTestWiki(t, false)
	router := newLocalMCPTestRouter(w, httpinternal.RouterOptions{
		AuthDisabled:            true,
		PublicAccess:            true,
		AllowInsecure:           true,
		MaxAssetUploadSizeBytes: assets.DefaultMaxUploadSizeBytes,
		MCPEnabled:              true,
		MCPToolListPageSize:     200,
	})
	session := connectLocalMCP(t, router, "/mcp")

	searchErr := callToolError(t, session, "wiki_search_pages", map[string]any{})
	if !strings.Contains(searchErr, "search_missing_query") && !strings.Contains(strings.ToLower(searchErr), "query") {
		t.Fatalf("empty search_pages error = %q, want missing query detail", searchErr)
	}
	blankTagSearchErr := callToolError(t, session, "wiki_search_pages", map[string]any{"tags": []any{" "}})
	if !strings.Contains(blankTagSearchErr, "search_missing_query") && !strings.Contains(strings.ToLower(blankTagSearchErr), "query") {
		t.Fatalf("blank-tag search_pages error = %q, want missing query detail", blankTagSearchErr)
	}

	target := nestedMap(t, callToolStructured(t, session, "wiki_create_page", map[string]any{
		"title": "Target",
		"slug":  "target",
		"kind":  "page",
	}), "page")
	source := nestedMap(t, callToolStructured(t, session, "wiki_create_page", map[string]any{
		"title": "Source",
		"slug":  "source",
		"kind":  "page",
	}), "page")
	sourceID := stringField(t, source, "id")
	content := "Tagged source with [Target](/target.md) and [Missing](/missing-target)"
	callToolStructured(t, session, "wiki_update_page", map[string]any{
		"id":      sourceID,
		"version": stringField(t, source, "version"),
		"title":   "Source",
		"slug":    "source",
		"content": content,
		"tags":    []any{"mcp", "assets"},
		"properties": map[string]any{
			"status": "draft",
		},
	})

	status := callToolStructured(t, session, "wiki_get_search_status", nil)
	httpStatus := getHTTPValue(t, router, "/api/search/status")
	assertJSONEqual(t, "wiki_get_search_status", status["status"], httpStatus)
	recordHTTPMCPParity(t, "wiki_get_search_status", "GET /api/search/status")
	if status["status"] == nil {
		t.Fatalf("get_search_status returned no status: %#v", status)
	}

	tags := callToolStructured(t, session, "wiki_list_tags", map[string]any{"q": "mc", "limit": float64(10)})
	httpTags := getHTTPValue(t, router, "/api/tags?q=mc&limit=10")
	assertJSONEqual(t, "wiki_list_tags", tags["tags"], httpTags)
	recordHTTPMCPParity(t, "wiki_list_tags", "GET /api/tags")
	if !arrayContainsObjectField(tags["tags"], "tag", "mcp") {
		t.Fatalf("list_tags = %#v, want mcp tag", tags["tags"])
	}
	tagPages := callToolStructured(t, session, "wiki_get_pages_by_tags", map[string]any{"tags": []any{"mcp"}})
	httpTagPages := getHTTPValue(t, router, "/api/tags/pages?tags=mcp")
	assertJSONEqual(t, "wiki_get_pages_by_tags", tagPages["pages"], httpTagPages)
	recordHTTPMCPParity(t, "wiki_get_pages_by_tags", "GET /api/tags/pages")
	if !arrayContainsObjectField(tagPages["pages"], "id", sourceID) {
		t.Fatalf("get_pages_by_tags = %#v, want source page", tagPages["pages"])
	}
	blankTagsErr := callToolError(t, session, "wiki_get_pages_by_tags", map[string]any{"tags": []any{" "}})
	if !strings.Contains(blankTagsErr, wikitags.ErrCodeTagsMissingParam) && !strings.Contains(strings.ToLower(blankTagsErr), "tags") {
		t.Fatalf("MCP blank get_pages_by_tags error = %q, want tags missing detail", blankTagsErr)
	}
	blankTagsHTTP := getHTTPStatus(t, router, "/api/tags/pages?tags=+", http.StatusBadRequest)
	if !strings.Contains(blankTagsHTTP, wikitags.ErrCodeTagsMissingParam) {
		t.Fatalf("HTTP blank get_pages_by_tags error = %q, want %s", blankTagsHTTP, wikitags.ErrCodeTagsMissingParam)
	}

	keys := callToolStructured(t, session, "wiki_list_property_keys", map[string]any{"q": "sta", "limit": float64(10)})
	httpKeys := getHTTPValue(t, router, "/api/properties?q=sta&limit=10")
	assertJSONEqual(t, "wiki_list_property_keys", keys["keys"], httpKeys)
	recordHTTPMCPParity(t, "wiki_list_property_keys", "GET /api/properties")
	if !arrayContainsObjectField(keys["keys"], "key", "status") {
		t.Fatalf("list_property_keys = %#v, want status key", keys["keys"])
	}
	propertyPages := callToolStructured(t, session, "wiki_get_pages_by_property", map[string]any{"key": "status", "value": "draft"})
	httpPropertyPages := getHTTPValue(t, router, "/api/properties/pages?key=status&value=draft")
	assertJSONEqual(t, "wiki_get_pages_by_property", propertyPages["pages"], httpPropertyPages)
	recordHTTPMCPParity(t, "wiki_get_pages_by_property", "GET /api/properties/pages")
	if !arrayContainsObjectField(propertyPages["pages"], "id", sourceID) {
		t.Fatalf("get_pages_by_property = %#v, want source page", propertyPages["pages"])
	}

	links := callToolStructured(t, session, "wiki_get_link_status", map[string]any{"pageId": sourceID})
	linkStatus := nestedMap(t, links, "status")
	httpLinkStatus := getHTTPMap(t, router, "/api/pages/"+sourceID+"/links")
	assertJSONEqual(t, "wiki_get_link_status", linkStatus, httpLinkStatus)
	recordHTTPMCPParity(t, "wiki_get_link_status", "GET /api/pages/:id/links")
	counts := nestedMap(t, linkStatus, "counts")
	if counts["outgoings"] != float64(1) {
		t.Fatalf("link outgoing count = %v, want 1; target=%s", counts["outgoings"], target["id"])
	}
	if counts["broken_outgoings"] != float64(1) {
		t.Fatalf("broken outgoing count = %v, want 1", counts["broken_outgoings"])
	}

	targetID := stringField(t, target, "id")
	targetStandaloneStatus := nestedMap(t, callToolStructured(t, session, "wiki_get_link_status", map[string]any{"pageId": targetID}), "status")
	targetFromGet := callToolStructured(t, session, "wiki_get_page", map[string]any{"id": targetID})
	assertJSONEqual(t, "wiki_get_page linkStatus", nestedMap(t, targetFromGet, "linkStatus"), targetStandaloneStatus)
	if !arrayContainsObjectField(nestedMap(t, targetFromGet, "linkStatus")["backlinks"], "from_page_id", sourceID) {
		t.Fatalf("wiki_get_page linkStatus backlinks = %#v, want backlink from source %s", nestedMap(t, targetFromGet, "linkStatus")["backlinks"], sourceID)
	}

	targetFromPath := callToolStructured(t, session, "wiki_get_page_by_path", map[string]any{"path": "target"})
	assertJSONEqual(t, "get_page_by_path linkStatus", nestedMap(t, targetFromPath, "linkStatus"), targetStandaloneStatus)

	sourceFromGet := callToolStructured(t, session, "wiki_get_page", map[string]any{"id": sourceID})
	assertJSONEqual(t, "wiki_get_page source linkStatus", nestedMap(t, sourceFromGet, "linkStatus"), linkStatus)

	sourceFromPath := callToolStructured(t, session, "wiki_get_page_by_path", map[string]any{"path": "source"})
	assertJSONEqual(t, "get_page_by_path source linkStatus", nestedMap(t, sourceFromPath, "linkStatus"), linkStatus)

	assetContent := []byte("asset content")
	cssContent := []byte("body { color: rebeccapurple; }\n")
	httpAssetContent := []byte("asset content from http")
	uploaded := callToolStructured(t, session, "wiki_upload_asset", map[string]any{
		"pageId":        sourceID,
		"filename":      "note.txt",
		"contentBase64": base64.StdEncoding.EncodeToString(assetContent),
	})
	if uploaded["file"] != "/assets/"+sourceID+"/note.txt" {
		t.Fatalf("upload_asset file = %v, want note asset URL", uploaded["file"])
	}
	httpUploaded := uploadHTTPAsset(t, router, sourceID, "http-note.txt", httpAssetContent, http.StatusCreated)
	assertAssetURLResult(t, "wiki_upload_asset", uploaded, "file", sourceID)
	assertAssetURLResult(t, "HTTP upload asset", httpUploaded, "file", sourceID)
	asset := callToolStructured(t, session, "wiki_get_asset", map[string]any{"pageId": sourceID, "filename": "note.txt"})
	if asset["filename"] != "note.txt" {
		t.Fatalf("get_asset filename = %v, want note.txt", asset["filename"])
	}
	if got := asset["contentBase64"]; got != base64.StdEncoding.EncodeToString(assetContent) {
		t.Fatalf("get_asset contentBase64 = %v, want uploaded content", got)
	}
	httpNoteBody, httpNoteContentType := getHTTPAssetWithContentType(t, router, sourceID, "note.txt")
	if httpNoteBody != string(assetContent) {
		t.Fatalf("HTTP asset content = %q, want uploaded content", httpNoteBody)
	}
	if !strings.HasPrefix(httpNoteContentType, asset["mimeType"].(string)) {
		t.Fatalf("HTTP note content type = %q, want MCP mime type %q", httpNoteContentType, asset["mimeType"])
	}
	httpAsset := callToolStructured(t, session, "wiki_get_asset", map[string]any{"pageId": sourceID, "filename": "http-note.txt"})
	if got := httpAsset["contentBase64"]; got != base64.StdEncoding.EncodeToString(httpAssetContent) {
		t.Fatalf("MCP read of HTTP-uploaded asset = %v, want HTTP uploaded content", got)
	}
	httpAssetBody, httpAssetContentType := getHTTPAssetWithContentType(t, router, sourceID, "http-note.txt")
	if httpAssetBody != string(httpAssetContent) {
		t.Fatalf("HTTP-uploaded asset content = %q, want HTTP uploaded content", httpAssetBody)
	}
	if !strings.HasPrefix(httpAssetContentType, httpAsset["mimeType"].(string)) {
		t.Fatalf("HTTP-uploaded content type = %q, want MCP mime type %q", httpAssetContentType, httpAsset["mimeType"])
	}
	listed := callToolStructured(t, session, "wiki_list_assets", map[string]any{"pageId": sourceID})
	if !arrayContainsString(listed["files"], "/assets/"+sourceID+"/note.txt") {
		t.Fatalf("list_assets = %#v, want uploaded note", listed["files"])
	}
	httpListed := getHTTPAssets(t, router, sourceID)
	if !arrayContainsString(httpListed["files"], "/assets/"+sourceID+"/note.txt") {
		t.Fatalf("HTTP asset list = %#v, want uploaded note", httpListed["files"])
	}
	assertJSONEqual(t, "wiki_list_assets", listed, httpListed)
	recordHTTPMCPParity(t, "wiki_upload_asset", "POST /api/pages/:id/assets")
	recordHTTPMCPParity(t, "wiki_list_assets", "GET /api/pages/:id/assets")
	recordHTTPMCPParity(t, "wiki_get_asset", "GET /assets/:pageId/:filename")
	callToolStructured(t, session, "wiki_upload_asset", map[string]any{
		"pageId":        sourceID,
		"filename":      "style.css",
		"contentBase64": base64.StdEncoding.EncodeToString(cssContent),
	})
	cssAsset := callToolStructured(t, session, "wiki_get_asset", map[string]any{"pageId": sourceID, "filename": "style.css"})
	httpCSSBody, httpCSSContentType := getHTTPAssetWithContentType(t, router, sourceID, "style.css")
	if httpCSSBody != string(cssContent) {
		t.Fatalf("HTTP CSS asset content = %q, want uploaded CSS", httpCSSBody)
	}
	if !strings.HasPrefix(httpCSSContentType, cssAsset["mimeType"].(string)) {
		t.Fatalf("HTTP CSS asset content type = %q, want MCP mime type %q", httpCSSContentType, cssAsset["mimeType"])
	}

	renamed := callToolStructured(t, session, "wiki_rename_asset", map[string]any{
		"pageId":      sourceID,
		"oldFilename": "note.txt",
		"newFilename": "renamed.txt",
	})
	if renamed["url"] != "/assets/"+sourceID+"/renamed.txt" {
		t.Fatalf("rename_asset url = %v, want renamed URL", renamed["url"])
	}
	httpRenamed := putHTTPJSON(t, router, "/api/pages/"+sourceID+"/assets/rename", map[string]any{
		"old_filename": "http-note.txt",
		"new_filename": "http-renamed.txt",
	}, http.StatusOK)
	assertAssetURLResult(t, "wiki_rename_asset", renamed, "url", sourceID)
	assertAssetURLResult(t, "HTTP rename_asset", httpRenamed, "url", sourceID)
	renamedAsset := callToolStructured(t, session, "wiki_get_asset", map[string]any{"pageId": sourceID, "filename": "renamed.txt"})
	if got := renamedAsset["contentBase64"]; got != base64.StdEncoding.EncodeToString(assetContent) {
		t.Fatalf("MCP renamed asset content = %v, want original MCP asset content", got)
	}
	httpRenamedBody, httpRenamedContentType := getHTTPAssetWithContentType(t, router, sourceID, "renamed.txt")
	if httpRenamedBody != string(assetContent) {
		t.Fatalf("HTTP renamed asset content = %q, want original MCP asset content", httpRenamedBody)
	}
	if !strings.HasPrefix(httpRenamedContentType, renamedAsset["mimeType"].(string)) {
		t.Fatalf("HTTP renamed content type = %q, want MCP mime type %q", httpRenamedContentType, renamedAsset["mimeType"])
	}
	assertMCPToolErrorContains(t, session, "wiki_get_asset", map[string]any{"pageId": sourceID, "filename": "note.txt"}, "asset")
	getHTTPStatus(t, router, "/assets/"+sourceID+"/note.txt", http.StatusNotFound)
	httpRenamedAsset := callToolStructured(t, session, "wiki_get_asset", map[string]any{"pageId": sourceID, "filename": "http-renamed.txt"})
	if got := httpRenamedAsset["contentBase64"]; got != base64.StdEncoding.EncodeToString(httpAssetContent) {
		t.Fatalf("MCP read of HTTP-renamed asset content = %v, want HTTP asset content", got)
	}
	if got := getHTTPAsset(t, router, sourceID, "http-renamed.txt"); got != string(httpAssetContent) {
		t.Fatalf("HTTP-renamed asset content = %q, want HTTP asset content", got)
	}
	assertMCPToolErrorContains(t, session, "wiki_get_asset", map[string]any{"pageId": sourceID, "filename": "http-note.txt"}, "asset")
	getHTTPStatus(t, router, "/assets/"+sourceID+"/http-note.txt", http.StatusNotFound)
	httpListed = getHTTPAssets(t, router, sourceID)
	listed = callToolStructured(t, session, "wiki_list_assets", map[string]any{"pageId": sourceID})
	assertJSONEqual(t, "list_assets after rename", listed, httpListed)
	if !arrayContainsString(httpListed["files"], "/assets/"+sourceID+"/renamed.txt") || !arrayContainsString(httpListed["files"], "/assets/"+sourceID+"/http-renamed.txt") {
		t.Fatalf("HTTP asset list after rename = %#v, want renamed assets", httpListed["files"])
	}
	recordHTTPMCPParity(t, "wiki_rename_asset", "PUT /api/pages/:id/assets/rename")
	mcpDeleted := callToolStructured(t, session, "wiki_delete_asset", map[string]any{"pageId": sourceID, "filename": "renamed.txt"})
	httpDeletedBody := deleteHTTPStatus(t, router, "/api/pages/"+sourceID+"/assets/http-renamed.txt", http.StatusOK)
	httpDeleted := decodeJSONMap(t, "HTTP delete_asset", []byte(httpDeletedBody))
	assertJSONEqual(t, "wiki_delete_asset", mcpDeleted, httpDeleted)
	assertMCPToolErrorContains(t, session, "wiki_get_asset", map[string]any{"pageId": sourceID, "filename": "renamed.txt"}, "asset")
	getHTTPStatus(t, router, "/assets/"+sourceID+"/renamed.txt", http.StatusNotFound)
	assertMCPToolErrorContains(t, session, "wiki_get_asset", map[string]any{"pageId": sourceID, "filename": "http-renamed.txt"}, "asset")
	getHTTPStatus(t, router, "/assets/"+sourceID+"/http-renamed.txt", http.StatusNotFound)
	listed = callToolStructured(t, session, "wiki_list_assets", map[string]any{"pageId": sourceID})
	if arrayContainsString(listed["files"], "/assets/"+sourceID+"/renamed.txt") {
		t.Fatalf("delete_asset left renamed asset in list: %#v", listed["files"])
	}
	httpListed = getHTTPAssets(t, router, sourceID)
	assertJSONEqual(t, "list_assets after delete", listed, httpListed)
	if arrayContainsString(httpListed["files"], "/assets/"+sourceID+"/renamed.txt") || arrayContainsString(httpListed["files"], "/assets/"+sourceID+"/http-renamed.txt") {
		t.Fatalf("HTTP asset list after delete = %#v, want renamed assets absent", httpListed["files"])
	}
	recordHTTPMCPParity(t, "wiki_delete_asset", "DELETE /api/pages/:id/assets/:name")
}

func TestLocalMCPProtocol_UploadAssetRejectsOversizedInputBeforePageLookup(t *testing.T) {
	w := newLocalMCPTestWiki(t, false)
	router := newLocalMCPTestRouter(w, httpinternal.RouterOptions{
		AuthDisabled:            true,
		PublicAccess:            true,
		AllowInsecure:           true,
		MaxAssetUploadSizeBytes: 2,
		MCPEnabled:              true,
		MCPToolListPageSize:     200,
	})
	session := connectLocalMCP(t, router, "/mcp")

	errText := callToolError(t, session, "wiki_upload_asset", map[string]any{
		"pageId":        "missing-page",
		"filename":      "too-large.txt",
		"contentBase64": base64.StdEncoding.EncodeToString([]byte("abc")),
	})
	if !strings.Contains(errText, "asset_file_too_large") && !strings.Contains(strings.ToLower(errText), "too large") {
		t.Fatalf("oversized upload error = %q, want asset_file_too_large before page lookup", errText)
	}
}

func TestLocalMCPProtocol_UploadAssetRejectsMalformedBase64AsAssetPayload(t *testing.T) {
	w := newLocalMCPTestWiki(t, false)
	router := newLocalMCPTestRouter(w, httpinternal.RouterOptions{
		AuthDisabled:            true,
		PublicAccess:            true,
		AllowInsecure:           true,
		MaxAssetUploadSizeBytes: assets.DefaultMaxUploadSizeBytes,
		MCPEnabled:              true,
		MCPToolListPageSize:     200,
	})
	session := connectLocalMCP(t, router, "/mcp")

	page := nestedMap(t, callToolStructured(t, session, "wiki_create_page", map[string]any{
		"title": "Asset Payload",
		"slug":  "asset-payload",
		"kind":  "page",
	}), "page")

	errText := callToolError(t, session, "wiki_upload_asset", map[string]any{
		"pageId":        stringField(t, page, "id"),
		"filename":      "bad.txt",
		"contentBase64": "not base64 %",
	})
	if !strings.Contains(errText, wikiassets.ErrCodeAssetInvalidPayload) && !strings.Contains(strings.ToLower(errText), "invalid asset payload") {
		t.Fatalf("malformed base64 upload error = %q, want %s", errText, wikiassets.ErrCodeAssetInvalidPayload)
	}
}

func TestLocalMCPProtocol_GetAssetUsesPageBoundaryValidation(t *testing.T) {
	w, storageDir := newLocalMCPTestWikiWithStorage(t)
	router := newLocalMCPTestRouter(w, httpinternal.RouterOptions{
		AuthDisabled:            true,
		PublicAccess:            true,
		AllowInsecure:           true,
		MaxAssetUploadSizeBytes: assets.DefaultMaxUploadSizeBytes,
		MCPEnabled:              true,
		MCPToolListPageSize:     200,
	})
	session := connectLocalMCP(t, router, "/mcp")

	orphanAssetDir := filepath.Join(storageDir, "assets", "missing-page")
	if err := os.MkdirAll(orphanAssetDir, 0755); err != nil {
		t.Fatalf("create orphan asset dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(orphanAssetDir, "orphan.txt"), []byte("orphan"), 0644); err != nil {
		t.Fatalf("write orphan asset: %v", err)
	}

	errText := callToolError(t, session, "wiki_get_asset", map[string]any{"pageId": "missing-page", "filename": "orphan.txt"})
	if !strings.Contains(errText, "asset_page_not_found") && !strings.Contains(strings.ToLower(errText), "page not found") {
		t.Fatalf("orphan get_asset error = %q, want page boundary validation", errText)
	}
}

func TestLocalMCPProtocol_FeatureGatedToolParity(t *testing.T) {
	runLocalMCPProtocolFeatureGatedToolParity(t)
}

func runLocalMCPProtocolFeatureGatedToolParity(t *testing.T) {
	w, _ := newLocalMCPTestWikiWithStorage(t)
	router := newLocalMCPTestRouter(w, httpinternal.RouterOptions{
		AuthDisabled:            true,
		PublicAccess:            true,
		AllowInsecure:           true,
		MaxAssetUploadSizeBytes: assets.DefaultMaxUploadSizeBytes,
		EnableWorkspaceSync:     true,
		EnableLinkRefactor:      true,
		MCPEnabled:              true,
		MCPToolListPageSize:     200,
	})
	session := connectLocalMCP(t, router, "/mcp")

	missingLatestErr := callToolError(t, session, "wiki_get_latest_revision", map[string]any{"pageId": "missing-page"})
	if !strings.Contains(missingLatestErr, wikirevisions.ErrCodeRevisionNotFound) && !strings.Contains(strings.ToLower(missingLatestErr), "page_not_found") && !strings.Contains(strings.ToLower(missingLatestErr), "revision") {
		t.Fatalf("missing latest revision error = %q, want revision_not_found or page_not_found detail", missingLatestErr)
	}
	for _, tc := range []struct {
		name string
		args map[string]any
	}{
		{name: "wiki_get_revision", args: map[string]any{"pageId": "missing-page", "revisionId": "missing-revision"}},
		{name: "wiki_compare_revisions", args: map[string]any{"pageId": "missing-page", "baseRevisionId": "base-revision", "targetRevisionId": "target-revision"}},
		{name: "wiki_get_revision_asset", args: map[string]any{"pageId": "missing-page", "revisionId": "missing-revision", "assetName": "missing.txt"}},
	} {
		errText := callToolError(t, session, tc.name, tc.args)
		lowerErr := strings.ToLower(errText)
		if !strings.Contains(errText, wikirevisions.ErrCodeRevisionNotFound) && !strings.Contains(lowerErr, "page_not_found") && !strings.Contains(lowerErr, "revision not found") && !strings.Contains(lowerErr, "revision asset not found") {
			t.Fatalf("%s missing revision error = %q, want %s or page_not_found", tc.name, errText, wikirevisions.ErrCodeRevisionNotFound)
		}
		if strings.Contains(lowerErr, "file does not exist") || strings.Contains(lowerErr, "no such file") {
			t.Fatalf("%s missing revision error = %q, want structured revision error instead of raw storage error", tc.name, errText)
		}
	}
	for _, tc := range []struct {
		name     string
		args     map[string]any
		wantCode string
		wantText string
	}{
		{name: "wiki_get_revision", args: map[string]any{"pageId": "missing-page", "revisionId": " "}, wantCode: wikirevisions.ErrCodeRevisionInvalidRevisionID, wantText: "revision id is required"},
		{name: "wiki_get_revision_asset", args: map[string]any{"pageId": "missing-page", "revisionId": " ", "assetName": "missing.txt"}, wantCode: wikirevisions.ErrCodeRevisionInvalidRevisionID, wantText: "revision id is required"},
		{name: "wiki_compare_revisions", args: map[string]any{"pageId": "missing-page", "baseRevisionId": " ", "targetRevisionId": "target-revision"}, wantCode: wikirevisions.ErrCodeRevisionCompareInvalidRequest, wantText: "revision compare request is invalid"},
		{name: "wiki_compare_revisions", args: map[string]any{"pageId": "missing-page", "baseRevisionId": "base-revision", "targetRevisionId": " "}, wantCode: wikirevisions.ErrCodeRevisionCompareInvalidRequest, wantText: "revision compare request is invalid"},
	} {
		errText := callToolError(t, session, tc.name, tc.args)
		assertErrorContainsAny(t, tc.name+" blank revision input", errText, tc.wantCode, tc.wantText)
	}

	target := nestedMap(t, callToolStructured(t, session, "wiki_create_page", map[string]any{
		"title": "Target",
		"slug":  "target",
		"kind":  "page",
	}), "page")
	targetID := stringField(t, target, "id")
	ref := nestedMap(t, callToolStructured(t, session, "wiki_create_page", map[string]any{
		"title": "Ref",
		"slug":  "ref",
		"kind":  "page",
	}), "page")
	refID := stringField(t, ref, "id")
	callToolStructured(t, session, "wiki_update_page", map[string]any{
		"id":      refID,
		"version": stringField(t, ref, "version"),
		"title":   "Ref",
		"slug":    "ref",
		"content": "[Target](/target.md)",
	})

	first := nestedMap(t, callToolStructured(t, session, "wiki_update_page", map[string]any{
		"id":      targetID,
		"version": stringField(t, target, "version"),
		"title":   "Target",
		"slug":    "target",
		"content": "First content",
	}), "page")
	second := nestedMap(t, callToolStructured(t, session, "wiki_update_page", map[string]any{
		"id":      targetID,
		"version": stringField(t, first, "version"),
		"title":   "Target",
		"slug":    "target",
		"content": "Second content",
	}), "page")
	httpUpdated := updateHTTPPage(t, router, targetID, map[string]any{
		"version": stringField(t, second, "version"),
		"title":   "Target",
		"slug":    "target",
		"content": "Third content from HTTP",
	})

	limitErr := callToolError(t, session, "wiki_list_revisions", map[string]any{"pageId": targetID, "limit": float64(201)})
	if !strings.Contains(limitErr, "revision_invalid_limit") && !strings.Contains(strings.ToLower(limitErr), "limit") {
		t.Fatalf("invalid wiki_list_revisions limit error = %q, want invalid limit detail", limitErr)
	}

	revisions := callToolStructured(t, session, "wiki_list_revisions", map[string]any{"pageId": targetID, "limit": float64(20)})
	httpRevisions := getHTTPMap(t, router, "/api/pages/"+targetID+"/revisions?limit=20")
	assertJSONEqual(t, "wiki_list_revisions", revisions, httpRevisions)
	recordHTTPMCPParity(t, "wiki_list_revisions", "GET /api/pages/:id/revisions")
	revisionItems, ok := revisions["revisions"].([]any)
	if !ok || len(revisionItems) < 2 {
		t.Fatalf("wiki_list_revisions = %#v, want at least two revisions", revisions["revisions"])
	}
	firstRevision := revisionItems[0].(map[string]any)
	if _, exists := firstRevision["page_id"]; exists {
		t.Fatalf("wiki_list_revisions returned raw snake_case revision: %#v", firstRevision)
	}
	if firstRevision["pageId"] != targetID {
		t.Fatalf("wiki_list_revisions pageId = %v, want %q", firstRevision["pageId"], targetID)
	}
	latest := callToolStructured(t, session, "wiki_get_latest_revision", map[string]any{"pageId": targetID})
	latestRevision := nestedMap(t, latest, "revision")
	httpLatestRevision := getHTTPLatestRevision(t, router, targetID)
	assertJSONEqual(t, "wiki_get_latest_revision", latestRevision, httpLatestRevision)
	recordHTTPMCPParity(t, "wiki_get_latest_revision", "GET /api/pages/:id/revisions/latest")
	latestRevisionID := stringField(t, latestRevision, "id")
	if _, exists := latestRevision["page_id"]; exists {
		t.Fatalf("wiki_get_latest_revision returned raw snake_case revision: %#v", latestRevision)
	}
	if latestRevision["pageId"] != targetID {
		t.Fatalf("wiki_get_latest_revision pageId = %v, want %q", latestRevision["pageId"], targetID)
	}
	snapshot := callToolStructured(t, session, "wiki_get_revision", map[string]any{"pageId": targetID, "revisionId": latestRevisionID})
	httpSnapshotAtLatest := getHTTPRevision(t, router, targetID, latestRevisionID)
	assertJSONEqual(t, "wiki_get_revision", snapshot, httpSnapshotAtLatest)
	recordHTTPMCPParity(t, "wiki_get_revision", "GET /api/pages/:id/revisions/:revisionId")
	if !strings.Contains(stringField(t, snapshot, "content"), "Third content from HTTP") {
		t.Fatalf("wiki_get_revision content = %v, want HTTP-updated content in Git-backed document", snapshot["content"])
	}
	snapshotRevision := nestedMap(t, snapshot, "revision")
	if snapshotRevision["pageId"] != targetID {
		t.Fatalf("wiki_get_revision revision.pageId = %v, want %q", snapshotRevision["pageId"], targetID)
	}
	mcpAfterHTTP := nestedMap(t, callToolStructured(t, session, "wiki_update_page", map[string]any{
		"id":      targetID,
		"version": stringField(t, httpUpdated, "version"),
		"title":   "Target",
		"slug":    "target",
		"content": "Fourth content from MCP",
	}), "page")
	if mcpAfterHTTP["content"] != "Fourth content from MCP" {
		t.Fatalf("MCP update after HTTP content = %v, want MCP-updated content", mcpAfterHTTP["content"])
	}
	httpLatest := getHTTPLatestRevision(t, router, targetID)
	httpLatestRevisionID := stringField(t, httpLatest, "id")
	httpSnapshot := getHTTPRevision(t, router, targetID, httpLatestRevisionID)
	if !strings.Contains(stringField(t, httpSnapshot, "content"), "Fourth content from MCP") {
		t.Fatalf("HTTP revision content after MCP update = %v, want MCP-updated content in Git-backed document", httpSnapshot["content"])
	}

	olderRevision := revisionItems[1].(map[string]any)
	comparison := callToolStructured(t, session, "wiki_compare_revisions", map[string]any{
		"pageId":           targetID,
		"baseRevisionId":   stringField(t, olderRevision, "id"),
		"targetRevisionId": httpLatestRevisionID,
	})
	httpComparison := getHTTPMap(t, router, "/api/pages/"+targetID+"/revisions/compare?base="+url.QueryEscape(stringField(t, olderRevision, "id"))+"&target="+url.QueryEscape(httpLatestRevisionID))
	assertJSONEqual(t, "wiki_compare_revisions", comparison, httpComparison)
	recordHTTPMCPParity(t, "wiki_compare_revisions", "GET /api/pages/:id/revisions/compare")
	if comparison["contentChanged"] != true {
		t.Fatalf("wiki_compare_revisions contentChanged = %v, want true", comparison["contentChanged"])
	}

	callToolStructured(t, session, "wiki_upload_asset", map[string]any{
		"pageId":        targetID,
		"filename":      "style.css",
		"contentBase64": base64.StdEncoding.EncodeToString([]byte("body { color: green; }\n")),
	})
	assetRevision := nestedMap(t, callToolStructured(t, session, "wiki_get_latest_revision", map[string]any{"pageId": targetID}), "revision")
	assetRevisionID := stringField(t, assetRevision, "id")
	revisionAssetErr := callToolError(t, session, "wiki_get_revision_asset", map[string]any{
		"pageId":     targetID,
		"revisionId": assetRevisionID,
		"assetName":  "style.css",
	})
	assertErrorContainsAny(t, "MCP Git-backed revision asset", revisionAssetErr, wikirevisions.ErrCodeRevisionNotFound, "workspace sync revisions do not track assets")
	httpRevisionAssetErr := getHTTPStatus(t, router, "/api/pages/"+targetID+"/revisions/"+assetRevisionID+"/assets/style.css", http.StatusNotFound)
	if !strings.Contains(httpRevisionAssetErr, wikirevisions.ErrCodeRevisionPreviewAssetNotFound) && !strings.Contains(httpRevisionAssetErr, "workspace sync revisions do not track assets") {
		t.Fatalf("HTTP Git-backed revision asset error = %q, want unsupported revision asset detail", httpRevisionAssetErr)
	}
	recordHTTPMCPParity(t, "wiki_get_revision_asset", "GET /api/pages/:id/revisions/:revisionId/assets/:name")

	invalidRefactorKindErr := callToolError(t, session, "wiki_preview_page_refactor", map[string]any{
		"id":    targetID,
		"kind":  "copy",
		"title": "Target",
		"slug":  "target-copy",
	})
	assertErrorContainsAny(t, "MCP invalid wiki_preview_page_refactor kind", invalidRefactorKindErr, wikipages.ErrCodePageInvalidRefactorKind, "invalid refactor kind")
	assertErrorDoesNotContainAny(t, "MCP invalid wiki_preview_page_refactor kind", invalidRefactorKindErr, "enum", "validating")
	invalidRefactorKindHTTP := postHTTPJSONBody(t, router, "/api/pages/"+targetID+"/refactor/preview", map[string]any{
		"kind":  "copy",
		"title": "Target",
		"slug":  "target-copy",
	}, http.StatusBadRequest)
	if !strings.Contains(invalidRefactorKindHTTP, "page_invalid_refactor_kind") {
		t.Fatalf("HTTP invalid wiki_preview_page_refactor kind error = %q, want page_invalid_refactor_kind", invalidRefactorKindHTTP)
	}
	paddedRefactorKindErr := callToolError(t, session, "wiki_preview_page_refactor", map[string]any{
		"id":    targetID,
		"kind":  " rename ",
		"title": "Target",
		"slug":  "target-padded",
	})
	assertErrorContainsAny(t, "MCP padded wiki_preview_page_refactor kind", paddedRefactorKindErr, wikipages.ErrCodePageInvalidRefactorKind, "invalid refactor kind")
	assertErrorDoesNotContainAny(t, "MCP padded wiki_preview_page_refactor kind", paddedRefactorKindErr, "enum", "validating")
	paddedRefactorKindHTTP := postHTTPJSONBody(t, router, "/api/pages/"+targetID+"/refactor/preview", map[string]any{
		"kind":  " rename ",
		"title": "Target",
		"slug":  "target-padded",
	}, http.StatusBadRequest)
	if !strings.Contains(paddedRefactorKindHTTP, "page_invalid_refactor_kind") {
		t.Fatalf("HTTP padded wiki_preview_page_refactor kind error = %q, want page_invalid_refactor_kind", paddedRefactorKindHTTP)
	}
	currentForInvalidApply := nestedMap(t, callToolStructured(t, session, "wiki_get_page", map[string]any{"id": targetID}), "page")
	invalidApplyKindErr := callToolError(t, session, "wiki_apply_page_refactor", map[string]any{
		"id":      targetID,
		"version": stringField(t, currentForInvalidApply, "version"),
		"kind":    "copy",
		"title":   "Target",
		"slug":    "target-copy",
	})
	assertErrorContainsAny(t, "MCP invalid wiki_apply_page_refactor kind", invalidApplyKindErr, wikipages.ErrCodePageInvalidRefactorKind, "invalid refactor kind")
	assertErrorDoesNotContainAny(t, "MCP invalid wiki_apply_page_refactor kind", invalidApplyKindErr, "enum", "validating")
	invalidApplyKindHTTP := postHTTPJSONBody(t, router, "/api/pages/"+targetID+"/refactor/apply", map[string]any{
		"version": stringField(t, currentForInvalidApply, "version"),
		"kind":    "copy",
		"title":   "Target",
		"slug":    "target-copy",
	}, http.StatusBadRequest)
	if !strings.Contains(invalidApplyKindHTTP, "page_invalid_refactor_kind") {
		t.Fatalf("HTTP invalid wiki_apply_page_refactor kind error = %q, want page_invalid_refactor_kind", invalidApplyKindHTTP)
	}
	whitespaceRefactorParentErr := callToolError(t, session, "wiki_preview_page_refactor", map[string]any{
		"id":       targetID,
		"kind":     "move",
		"parentId": " ",
	})
	assertErrorContainsAny(t, "MCP wiki_preview_page_refactor whitespace parentId", whitespaceRefactorParentErr, "page_invalid_parent_id", "parent")
	whitespaceRefactorParentHTTP := postHTTPJSONBody(t, router, "/api/pages/"+targetID+"/refactor/preview", map[string]any{
		"kind":     "move",
		"parentId": " ",
	}, http.StatusBadRequest)
	if !strings.Contains(whitespaceRefactorParentHTTP, "page_invalid_parent_id") {
		t.Fatalf("HTTP wiki_preview_page_refactor whitespace parentId error = %q, want page_invalid_parent_id", whitespaceRefactorParentHTTP)
	}
	refactorPaddedParent := nestedMap(t, callToolStructured(t, session, "wiki_create_page", map[string]any{
		"title": "Refactor Padded Parent",
		"slug":  "refactor-padded-parent",
		"kind":  "section",
	}), "page")
	refactorPaddedTarget := nestedMap(t, callToolStructured(t, session, "wiki_create_page", map[string]any{
		"title": "Refactor Padded Target",
		"slug":  "refactor-padded-target",
		"kind":  "page",
	}), "page")
	paddedApplyParentErr := callToolError(t, session, "wiki_apply_page_refactor", map[string]any{
		"id":       stringField(t, refactorPaddedTarget, "id"),
		"version":  stringField(t, refactorPaddedTarget, "version"),
		"kind":     "move",
		"parentId": " " + stringField(t, refactorPaddedParent, "id") + " ",
	})
	assertErrorContainsAny(t, "MCP wiki_apply_page_refactor padded parentId", paddedApplyParentErr, "page_invalid_parent_id", "parent")
	paddedApplyParentHTTP := postHTTPJSONBody(t, router, "/api/pages/"+stringField(t, refactorPaddedTarget, "id")+"/refactor/apply", map[string]any{
		"version":  stringField(t, refactorPaddedTarget, "version"),
		"kind":     "move",
		"parentId": " " + stringField(t, refactorPaddedParent, "id") + " ",
	}, http.StatusBadRequest)
	if !strings.Contains(paddedApplyParentHTTP, "page_invalid_parent_id") {
		t.Fatalf("HTTP wiki_apply_page_refactor padded parentId error = %q, want page_invalid_parent_id", paddedApplyParentHTTP)
	}

	preview := callToolStructured(t, session, "wiki_preview_page_refactor", map[string]any{
		"id":    targetID,
		"kind":  "rename",
		"title": "Target",
		"slug":  "target-renamed",
	})
	httpPreview := postHTTPJSON(t, router, "/api/pages/"+targetID+"/refactor/preview", map[string]any{
		"kind":  "rename",
		"title": "Target",
		"slug":  "target-renamed",
	}, http.StatusOK)
	assertJSONEqual(t, "wiki_preview_page_refactor", preview, httpPreview)
	recordHTTPMCPParity(t, "wiki_preview_page_refactor", "POST /api/pages/:id/refactor/preview")
	counts := nestedMap(t, preview, "counts")
	if counts["affectedPages"] != float64(1) {
		t.Fatalf("preview affectedPages = %v, want 1", counts["affectedPages"])
	}

	staleRefactor := nestedMap(t, callToolStructured(t, session, "wiki_create_page", map[string]any{
		"title": "Stale Refactor",
		"slug":  "stale-refactor",
		"kind":  "page",
	}), "page")
	staleRefactorID := stringField(t, staleRefactor, "id")
	updateHTTPPage(t, router, staleRefactorID, map[string]any{
		"version": stringField(t, staleRefactor, "version"),
		"title":   "Stale Refactor",
		"slug":    "stale-refactor",
		"content": "newer version",
	})
	staleRefactorErr := callToolError(t, session, "wiki_apply_page_refactor", map[string]any{
		"id":           staleRefactorID,
		"version":      stringField(t, staleRefactor, "version"),
		"kind":         "rename",
		"title":        "Stale Refactor",
		"slug":         "stale-refactor-mcp",
		"rewriteLinks": true,
	})
	staleRefactorHTTP := postHTTPJSONBody(t, router, "/api/pages/"+staleRefactorID+"/refactor/apply", map[string]any{
		"version":      stringField(t, staleRefactor, "version"),
		"kind":         "rename",
		"title":        "Stale Refactor",
		"slug":         "stale-refactor-http",
		"rewriteLinks": true,
	}, http.StatusConflict)
	assertPageVersionConflictParity(t, "stale wiki_apply_page_refactor", staleRefactorErr, staleRefactorHTTP)

	httpApplyTarget := nestedMap(t, callToolStructured(t, session, "wiki_create_page", map[string]any{
		"title": "HTTP Apply Target",
		"slug":  "http-apply-target",
		"kind":  "page",
	}), "page")
	httpApplyRef := nestedMap(t, callToolStructured(t, session, "wiki_create_page", map[string]any{
		"title": "HTTP Apply Ref",
		"slug":  "http-apply-ref",
		"kind":  "page",
	}), "page")
	callToolStructured(t, session, "wiki_update_page", map[string]any{
		"id":      stringField(t, httpApplyRef, "id"),
		"version": stringField(t, httpApplyRef, "version"),
		"title":   "HTTP Apply Ref",
		"slug":    "http-apply-ref",
		"content": "[HTTP Apply Target](/http-apply-target.md)",
	})
	httpAppliedViaRoute := postHTTPJSON(t, router, "/api/pages/"+stringField(t, httpApplyTarget, "id")+"/refactor/apply", map[string]any{
		"version":      stringField(t, httpApplyTarget, "version"),
		"kind":         "rename",
		"title":        "HTTP Apply Target",
		"slug":         "http-apply-target-renamed",
		"rewriteLinks": true,
	}, http.StatusOK)
	assertPageState(t, "HTTP wiki_apply_page_refactor success", httpAppliedViaRoute, stringField(t, httpApplyTarget, "id"), "HTTP Apply Target", "http-apply-target-renamed", "http-apply-target-renamed", "page", "")
	httpApplyRefAfter := getHTTPPageByPath(t, router, "http-apply-ref")
	if httpApplyRefAfter["content"] != "[HTTP Apply Target](/http-apply-target-renamed.md)" {
		t.Fatalf("HTTP apply ref content = %v, want rewritten link", httpApplyRefAfter["content"])
	}

	currentTarget := nestedMap(t, callToolStructured(t, session, "wiki_get_page", map[string]any{"id": targetID}), "page")
	applied := nestedMap(t, callToolStructured(t, session, "wiki_apply_page_refactor", map[string]any{
		"id":           targetID,
		"version":      stringField(t, currentTarget, "version"),
		"kind":         "rename",
		"title":        "Target",
		"slug":         "target-renamed",
		"rewriteLinks": true,
	}), "page")
	if applied["slug"] != "target-renamed" {
		t.Fatalf("wiki_apply_page_refactor slug = %v, want target-renamed", applied["slug"])
	}
	httpApplied := getHTTPPageByID(t, router, targetID)
	assertJSONEqual(t, "wiki_apply_page_refactor", applied, httpApplied)
	assertPageState(t, "MCP wiki_apply_page_refactor success", applied, targetID, "Target", "target-renamed", "target-renamed", "page", "")
	refHTTP := getHTTPPageByPath(t, router, "ref")
	if refHTTP["content"] != "[Target](/target-renamed.md)" {
		t.Fatalf("ref content after refactor = %v, want rewritten link", refHTTP["content"])
	}
	recordHTTPMCPParity(t, "wiki_apply_page_refactor", "POST /api/pages/:id/refactor/apply")

	restored := nestedMap(t, callToolStructured(t, session, "wiki_restore_revision", map[string]any{
		"pageId":     targetID,
		"revisionId": latestRevisionID,
	}), "page")
	if restored["content"] != "Third content from HTTP" {
		t.Fatalf("wiki_restore_revision content = %v, want HTTP-updated content", restored["content"])
	}
	httpRestored := getHTTPPageByID(t, router, targetID)
	assertJSONEqual(t, "wiki_restore_revision", restored, httpRestored)

	mcpRestoreMeta := nestedMap(t, callToolStructured(t, session, "wiki_create_page", map[string]any{
		"title": "MCP Restore Metadata",
		"slug":  "mcp-restore-metadata",
		"kind":  "page",
	}), "page")
	mcpRestoreMetaID := stringField(t, mcpRestoreMeta, "id")
	mcpRestoreMetaRevision := nestedMap(t, callToolStructured(t, session, "wiki_update_page", map[string]any{
		"id":      mcpRestoreMetaID,
		"version": stringField(t, mcpRestoreMeta, "version"),
		"title":   "MCP Restore Metadata",
		"slug":    "mcp-restore-metadata",
		"content": "metadata revision\n",
		"tags":    []any{"restore", "metadata"},
		"properties": map[string]any{
			"status": "archived",
		},
	}), "page")
	mcpRestoreMetaLatest := nestedMap(t, callToolStructured(t, session, "wiki_get_latest_revision", map[string]any{"pageId": mcpRestoreMetaID}), "revision")
	callToolStructured(t, session, "wiki_update_page", map[string]any{
		"id":      mcpRestoreMetaID,
		"version": stringField(t, mcpRestoreMetaRevision, "version"),
		"title":   "MCP Restore Metadata",
		"slug":    "mcp-restore-metadata",
		"content": "current revision\n",
	})
	mcpRestoredMeta := nestedMap(t, callToolStructured(t, session, "wiki_restore_revision", map[string]any{
		"pageId":     mcpRestoreMetaID,
		"revisionId": stringField(t, mcpRestoreMetaLatest, "id"),
	}), "page")
	assertRestoredMetadata(t, "MCP restore metadata", mcpRestoredMeta)
	callToolStructured(t, session, "wiki_update_page", map[string]any{
		"id":      mcpRestoreMetaID,
		"version": stringField(t, mcpRestoredMeta, "version"),
		"title":   "MCP Restore Metadata",
		"slug":    "mcp-restore-metadata",
		"content": "current revision before HTTP restore\n",
	})
	httpRestoredMCPMeta := postHTTPJSON(t, router, "/api/pages/"+mcpRestoreMetaID+"/revisions/"+stringField(t, mcpRestoreMetaLatest, "id")+"/restore", nil, http.StatusOK)
	assertRestoredMetadata(t, "HTTP restore metadata on MCP fixture", httpRestoredMCPMeta)
	assertRestorePayloadsMatch(t, mcpRestoredMeta, httpRestoredMCPMeta)

	httpRestoreMeta := postHTTPJSON(t, router, "/api/pages", map[string]any{
		"title": "HTTP Restore Metadata",
		"slug":  "http-restore-metadata",
		"kind":  "page",
	}, http.StatusCreated)
	httpRestoreMetaID := stringField(t, httpRestoreMeta, "id")
	httpRestoreMetaRevision := updateHTTPPage(t, router, httpRestoreMetaID, map[string]any{
		"version": stringField(t, httpRestoreMeta, "version"),
		"title":   "HTTP Restore Metadata",
		"slug":    "http-restore-metadata",
		"content": "metadata revision\n",
		"tags":    []string{"restore", "metadata"},
		"properties": map[string]string{
			"status": "archived",
		},
	})
	httpRestoreMetaLatest := getHTTPLatestRevision(t, router, httpRestoreMetaID)
	updateHTTPPage(t, router, httpRestoreMetaID, map[string]any{
		"version": stringField(t, httpRestoreMetaRevision, "version"),
		"title":   "HTTP Restore Metadata",
		"slug":    "http-restore-metadata",
		"content": "current revision\n",
	})
	httpRestoredMeta := postHTTPJSON(t, router, "/api/pages/"+httpRestoreMetaID+"/revisions/"+stringField(t, httpRestoreMetaLatest, "id")+"/restore", nil, http.StatusOK)
	assertRestoredMetadata(t, "HTTP restore metadata", httpRestoredMeta)
	assertRestoredMetadata(t, "HTTP restore metadata persisted", getHTTPPageByID(t, router, httpRestoreMetaID))
	recordHTTPMCPParity(t, "wiki_restore_revision", "POST /api/pages/:id/revisions/:revisionId/restore")
}

func TestLocalMCPProtocol_HTTPParityCoverageRecordedForPlanTools(t *testing.T) {
	runHTTPMCPParityCoverage(t)
}

func runHTTPMCPParityCoverage(t *testing.T) {
	t.Helper()

	if !hasHTTPMCPParityRecorded(t) {
		resetHTTPMCPParityCoverage()
		t.Run("page mutation", runLocalMCPProtocolPageMutationParity)
		t.Run("page operation", runLocalMCPProtocolPageOperationParity)
		t.Run("index and asset", runLocalMCPProtocolIndexAndAssetParity)
		t.Run("feature gated", runLocalMCPProtocolFeatureGatedToolParity)
	}
	assertHTTPMCPParityRecorded(t)
}

func newLocalMCPTestWiki(t *testing.T, _ bool) *wiki.Wiki {
	t.Helper()

	w, _ := newLocalMCPTestWikiWithStorage(t)
	return w
}

func newLocalMCPTestWikiWithStorage(t *testing.T) (*wiki.Wiki, string) {
	t.Helper()

	return newLocalMCPTestWikiWithOptionsAndStorage(t, wiki.WikiOptions{
		AuthDisabled: true,
	})
}

func newLocalMCPTestWikiWithOptions(t *testing.T, opts wiki.WikiOptions) *wiki.Wiki {
	t.Helper()

	w, _ := newLocalMCPTestWikiWithOptionsAndStorage(t, opts)
	return w
}

func newLocalMCPTestWikiWithOptionsAndStorage(t *testing.T, opts wiki.WikiOptions) (*wiki.Wiki, string) {
	t.Helper()

	storageDir := filepath.Join(t.TempDir(), "data")
	rootDir := filepath.Join(t.TempDir(), "content")
	if opts.Workspace.ID == "" {
		opts.Workspace.ID = "default"
	}
	if opts.Workspace.DataDir == "" {
		opts.Workspace.DataDir = storageDir
	}
	if opts.Workspace.RootDir == "" {
		opts.Workspace.RootDir = rootDir
	}
	if opts.AdminPassword == "" {
		opts.AdminPassword = "admin"
	}
	if opts.JWTSecret == "" {
		opts.JWTSecret = "secretkey"
	}
	if opts.AccessTokenTimeout == 0 {
		opts.AccessTokenTimeout = 15 * time.Minute
	}
	if opts.RefreshTokenTimeout == 0 {
		opts.RefreshTokenTimeout = 7 * 24 * time.Hour
	}
	w, err := wiki.NewWiki(&wiki.WikiOptions{
		Workspace:           opts.Workspace,
		AdminPassword:       opts.AdminPassword,
		JWTSecret:           opts.JWTSecret,
		AccessTokenTimeout:  opts.AccessTokenTimeout,
		RefreshTokenTimeout: opts.RefreshTokenTimeout,
		AuthDisabled:        opts.AuthDisabled,
	})
	if err != nil {
		t.Fatalf("NewWiki failed: %v", err)
	}
	t.Cleanup(func() {
		if err := w.Close(); err != nil {
			t.Fatalf("Close wiki failed: %v", err)
		}
	})
	return w, storageDir
}

func newLocalMCPTestRouter(w *wiki.Wiki, opts httpinternal.RouterOptions) http.Handler {
	if opts.MCPEnabled && opts.MCPBindHost == "" {
		opts.MCPBindHost = "127.0.0.1"
	}
	return httpinternal.NewRouter(w.Registrars(), w.FrontendConfig(), opts)
}

func connectLocalMCP(t *testing.T, handler http.Handler, path string) *sdkmcp.ClientSession {
	t.Helper()

	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	client := sdkmcp.NewClient(&sdkmcp.Implementation{Name: "leafwiki-test", Version: "test"}, nil)
	session, err := client.Connect(context.Background(), &sdkmcp.StreamableClientTransport{
		Endpoint:             server.URL + path,
		HTTPClient:           server.Client(),
		DisableStandaloneSSE: true,
	}, nil)
	if err != nil {
		t.Fatalf("Connect MCP client failed: %v", err)
	}
	t.Cleanup(func() { session.Close() })
	return session
}

func listAllToolNames(t *testing.T, session *sdkmcp.ClientSession) []string {
	t.Helper()

	tools := listAllTools(t, session)
	var names []string
	for _, tool := range tools {
		names = append(names, tool.Name)
	}
	sort.Strings(names)
	return names
}

func listAllTools(t *testing.T, session *sdkmcp.ClientSession) []*sdkmcp.Tool {
	t.Helper()

	var tools []*sdkmcp.Tool
	cursor := ""
	for {
		result, err := session.ListTools(context.Background(), &sdkmcp.ListToolsParams{Cursor: cursor})
		if err != nil {
			t.Fatalf("ListTools failed: %v", err)
		}
		for _, tool := range result.Tools {
			tools = append(tools, tool)
		}
		if result.NextCursor == "" {
			break
		}
		cursor = result.NextCursor
	}
	sort.Slice(tools, func(i, j int) bool {
		return tools[i].Name < tools[j].Name
	})
	return tools
}

func assertInputSchemasMatch(t *testing.T, tools []*sdkmcp.Tool, expected, expectedRequired map[string][]string) {
	t.Helper()

	for _, tool := range tools {
		if _, ok := expected[tool.Name]; !ok {
			t.Fatalf("missing input schema expectation for tool %q", tool.Name)
		}
		if _, ok := expectedRequired[tool.Name]; !ok {
			t.Fatalf("missing input schema required expectation for tool %q", tool.Name)
		}
	}
	for name, props := range expected {
		tool := findTool(t, tools, name)
		schema := decodeToolSchema(t, "input", tool.Name, tool.InputSchema)
		assertNoRootSchemaCombinators(t, "input", name, schema)
		properties := schemaProperties(t, "input", tool.Name, schema)
		gotProps := make([]string, 0, len(properties))
		for prop := range properties {
			gotProps = append(gotProps, prop)
			assertSchemaPropertyHasType(t, "input", name, prop, properties[prop])
		}
		assertPreciseInputPropertySchemas(t, name, properties)
		sort.Strings(gotProps)
		assertSchemaPropertyOrderSorted(t, "input", name, schema)

		wantProps := append([]string{}, props...)
		sort.Strings(wantProps)
		if strings.Join(gotProps, "\n") != strings.Join(wantProps, "\n") {
			t.Fatalf("input schema for %s properties mismatch\n got: %v\nwant: %v", name, gotProps, wantProps)
		}

		assertStringSet(t, "input schema required for "+name, schemaStringSlice(schema["required"]), expectedRequired[name])
	}
}

func assertOutputSchemasMatch(t *testing.T, tools []*sdkmcp.Tool, expected map[string][]string) {
	t.Helper()

	for _, tool := range tools {
		if tool.OutputSchema == nil {
			t.Fatalf("tool %s has no output schema", tool.Name)
		}
		if _, ok := expected[tool.Name]; !ok {
			t.Fatalf("missing output schema expectation for tool %q", tool.Name)
		}
	}
	for name, props := range expected {
		tool := findTool(t, tools, name)
		if tool.OutputSchema == nil {
			t.Fatalf("tool %s has no output schema", tool.Name)
		}
		schema := decodeToolSchema(t, "output", tool.Name, tool.OutputSchema)
		assertNoRootSchemaCombinators(t, "output", name, schema)
		properties := schemaProperties(t, "output", tool.Name, schema)
		gotProps := make([]string, 0, len(properties))
		for prop := range properties {
			gotProps = append(gotProps, prop)
			assertSchemaPropertyHasType(t, "output", name, prop, properties[prop])
		}
		sort.Strings(gotProps)
		assertSchemaPropertyOrderSorted(t, "output", name, schema)

		wantProps := append([]string{}, props...)
		sort.Strings(wantProps)
		if strings.Join(gotProps, "\n") != strings.Join(wantProps, "\n") {
			t.Fatalf("output schema for %s properties mismatch\n got: %v\nwant: %v", name, gotProps, wantProps)
		}
		requiredProps := outputRequiredProperties(props, toolOutputOptionalProperties[name])
		assertStringSet(t, "output schema required for "+name, schemaStringSlice(schema["required"]), requiredProps)
	}
}

func outputRequiredProperties(props, optional []string) []string {
	required := make([]string, 0, len(props))
	for _, prop := range props {
		if !contains(optional, prop) {
			required = append(required, prop)
		}
	}
	return required
}

func findTool(t *testing.T, tools []*sdkmcp.Tool, name string) *sdkmcp.Tool {
	t.Helper()

	for _, tool := range tools {
		if tool.Name == name {
			return tool
		}
	}
	t.Fatalf("tool %q not found", name)
	return nil
}

func decodeToolSchema(t *testing.T, kind, name string, schemaValue any) map[string]any {
	t.Helper()

	raw, err := json.Marshal(schemaValue)
	if err != nil {
		t.Fatalf("marshal %s schema for %s: %v", kind, name, err)
	}
	var schema map[string]any
	if err := json.Unmarshal(raw, &schema); err != nil {
		t.Fatalf("decode %s schema for %s: %v: %s", kind, name, err, raw)
	}
	if schema["type"] != "object" {
		t.Fatalf("%s schema for %s type = %v, want object: %#v", kind, name, schema["type"], schema)
	}
	return schema
}

func schemaProperties(t *testing.T, kind, name string, schema map[string]any) map[string]any {
	t.Helper()

	properties, ok := schema["properties"].(map[string]any)
	if !ok {
		return map[string]any{}
	}
	return properties
}

func assertSchemaPropertyOrderSorted(t *testing.T, kind, name string, schema map[string]any) {
	t.Helper()

	order := schemaStringSlice(schema["propertyOrder"])
	if len(order) == 0 {
		return
	}
	sortedOrder := append([]string{}, order...)
	sort.Strings(sortedOrder)
	if !reflect.DeepEqual(order, sortedOrder) {
		t.Fatalf("%s schema for %s propertyOrder = %v, want deterministic sorted order %v", kind, name, order, sortedOrder)
	}
}

func assertSchemaPropertyHasType(t *testing.T, kind, toolName, prop string, property any) {
	t.Helper()

	schema, ok := property.(map[string]any)
	if !ok {
		t.Fatalf("%s schema for %s.%s is not an object: %#v", kind, toolName, prop, property)
	}
	if _, ok := schema["type"]; ok {
		return
	}
	for _, key := range []string{"$ref", "anyOf", "oneOf", "allOf"} {
		if _, ok := schema[key]; ok {
			return
		}
	}
	t.Fatalf("%s schema for %s.%s has no type/ref/union: %#v", kind, toolName, prop, schema)
}

func assertNoRootSchemaCombinators(t *testing.T, kind, name string, schema map[string]any) {
	t.Helper()

	for _, key := range []string{"anyOf", "oneOf", "allOf"} {
		if _, ok := schema[key]; ok {
			t.Fatalf("%s schema for %s has root-level %s: %#v", kind, name, key, schema)
		}
	}
}

func assertPreciseInputPropertySchemas(t *testing.T, toolName string, properties map[string]any) {
	t.Helper()

	switch toolName {
	case "wiki_update_page_metadata":
		for _, prop := range []string{"setTags", "addTags", "removeTags", "removeProperties"} {
			assertStringArrayPropertySchema(t, toolName, prop, properties[prop])
		}
		assertStringMapPropertySchema(t, toolName, "setProperties", properties["setProperties"])
	case "wiki_replace_page_section":
		assertStringArrayPropertySchema(t, toolName, "headingPath", properties["headingPath"])
	}
}

func assertStringArrayPropertySchema(t *testing.T, toolName, prop string, raw any) {
	t.Helper()

	schema, ok := raw.(map[string]any)
	if !ok {
		t.Fatalf("input schema for %s.%s is not an object: %#v", toolName, prop, raw)
	}
	if schema["type"] != "array" {
		t.Fatalf("input schema for %s.%s type = %#v, want array", toolName, prop, schema["type"])
	}
	items, ok := schema["items"].(map[string]any)
	if !ok {
		t.Fatalf("input schema for %s.%s missing string items schema: %#v", toolName, prop, schema)
	}
	if items["type"] != "string" {
		t.Fatalf("input schema for %s.%s items type = %#v, want string", toolName, prop, items["type"])
	}
}

func assertStringMapPropertySchema(t *testing.T, toolName, prop string, raw any) {
	t.Helper()

	schema, ok := raw.(map[string]any)
	if !ok {
		t.Fatalf("input schema for %s.%s is not an object: %#v", toolName, prop, raw)
	}
	if schema["type"] != "object" {
		t.Fatalf("input schema for %s.%s type = %#v, want object", toolName, prop, schema["type"])
	}
	additional, ok := schema["additionalProperties"].(map[string]any)
	if !ok {
		t.Fatalf("input schema for %s.%s missing string additionalProperties schema: %#v", toolName, prop, schema)
	}
	if additional["type"] != "string" {
		t.Fatalf("input schema for %s.%s additionalProperties type = %#v, want string", toolName, prop, additional["type"])
	}
}

func assertContextHistoryOpaque(t *testing.T, contextOut map[string]any) {
	t.Helper()

	for _, raw := range arrayField(t, contextOut, "contextHistory") {
		entry, ok := raw.(map[string]any)
		if !ok {
			t.Fatalf("contextHistory entry has type %T: %#v", raw, raw)
		}
		for _, internal := range []string{"commitHash", "validationHash"} {
			if _, exists := entry[internal]; exists {
				t.Fatalf("contextHistory entry leaks %s: %#v", internal, entry)
			}
		}
	}
}

func schemaStringSlice(value any) []string {
	rawItems, ok := value.([]any)
	if !ok {
		return nil
	}
	items := make([]string, 0, len(rawItems))
	for _, raw := range rawItems {
		item, ok := raw.(string)
		if ok {
			items = append(items, item)
		}
	}
	return items
}

func assertStringSet(t *testing.T, label string, got, want []string) {
	t.Helper()

	got = append([]string{}, got...)
	want = append([]string{}, want...)
	sort.Strings(got)
	sort.Strings(want)
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("%s mismatch\n got: %v\nwant: %v", label, got, want)
	}
}

func copyToolInputProperties(src map[string][]string) map[string][]string {
	out := make(map[string][]string, len(src))
	for name, props := range src {
		out[name] = append([]string{}, props...)
	}
	return out
}

func assertToolNames(t *testing.T, got []string, want []string) {
	t.Helper()

	sortedWant := append([]string{}, want...)
	sort.Strings(sortedWant)
	if strings.Join(got, "\n") != strings.Join(sortedWant, "\n") {
		t.Fatalf("tool names mismatch\n got:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(sortedWant, "\n"))
	}
}

func contains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func callToolStructured(t *testing.T, session *sdkmcp.ClientSession, name string, args map[string]any) map[string]any {
	t.Helper()

	result, err := session.CallTool(context.Background(), &sdkmcp.CallToolParams{
		Name:      name,
		Arguments: args,
	})
	if err != nil {
		t.Fatalf("CallTool %s failed: %v", name, err)
	}
	if result.IsError {
		t.Fatalf("CallTool %s returned tool error: %#v", name, result.Content)
	}

	structured, ok := result.StructuredContent.(map[string]any)
	if !ok {
		t.Fatalf("CallTool %s structured content has type %T: %#v", name, result.StructuredContent, result.StructuredContent)
	}
	return structured
}

func callToolError(t *testing.T, session *sdkmcp.ClientSession, name string, args map[string]any) string {
	t.Helper()

	result, err := session.CallTool(context.Background(), &sdkmcp.CallToolParams{
		Name:      name,
		Arguments: args,
	})
	if err != nil {
		t.Fatalf("CallTool %s failed: %v", name, err)
	}
	if !result.IsError {
		t.Fatalf("CallTool %s succeeded, want tool error: %#v", name, result.StructuredContent)
	}
	for _, content := range result.Content {
		if text, ok := content.(*sdkmcp.TextContent); ok {
			return text.Text
		}
	}
	t.Fatalf("CallTool %s returned error without text content: %#v", name, result.Content)
	return ""
}

func callToolProtocolError(t *testing.T, session *sdkmcp.ClientSession, name string, args map[string]any) string {
	t.Helper()

	result, err := session.CallTool(context.Background(), &sdkmcp.CallToolParams{
		Name:      name,
		Arguments: args,
	})
	if err == nil {
		t.Fatalf("CallTool %s result = %#v, want protocol error", name, result)
	}
	return err.Error()
}

func getHTTPPageByPath(t *testing.T, router http.Handler, path string) map[string]any {
	t.Helper()

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/pages/by-path?path="+path, nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET page by path %q = %d: %s", path, rec.Code, rec.Body.String())
	}
	var out map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode HTTP page by path %q: %v", path, err)
	}
	return out
}

func getHTTPPageByID(t *testing.T, router http.Handler, pageID string) map[string]any {
	t.Helper()

	return getHTTPMap(t, router, "/api/pages/"+pageID)
}

func getHTTPValue(t *testing.T, router http.Handler, path string) any {
	t.Helper()

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET %s = %d: %s", path, rec.Code, rec.Body.String())
	}
	return decodeJSONValue(t, "GET "+path, rec.Body.Bytes())
}

func getHTTPStatus(t *testing.T, router http.Handler, path string, wantStatus int) string {
	t.Helper()

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	if rec.Code != wantStatus {
		t.Fatalf("GET %s = %d, want %d: %s", path, rec.Code, wantStatus, rec.Body.String())
	}
	return rec.Body.String()
}

func getHTTPMap(t *testing.T, router http.Handler, path string) map[string]any {
	t.Helper()

	value := getHTTPValue(t, router, path)
	out, ok := value.(map[string]any)
	if !ok {
		t.Fatalf("GET %s decoded as %T: %#v, want object", path, value, value)
	}
	return out
}

func updateHTTPPage(t *testing.T, router http.Handler, pageID string, payload map[string]any) map[string]any {
	t.Helper()

	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal HTTP page update: %v", err)
	}
	csrfToken, csrfCookies := issueHTTPCSRF(t, router)
	req := httptest.NewRequest(http.MethodPut, "/api/pages/"+pageID, strings.NewReader(string(body)))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-CSRF-Token", csrfToken)
	for _, cookie := range csrfCookies {
		req.AddCookie(cookie)
	}
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("PUT page %q = %d: %s", pageID, rec.Code, rec.Body.String())
	}
	var out map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode HTTP page update %q: %v", pageID, err)
	}
	return out
}

func putHTTPJSON(t *testing.T, router http.Handler, path string, payload map[string]any, wantStatus int) map[string]any {
	t.Helper()

	return requestHTTPJSON(t, router, http.MethodPut, path, payload, wantStatus)
}

func putHTTPJSONBody(t *testing.T, router http.Handler, path string, payload map[string]any, wantStatus int) string {
	t.Helper()

	return requestHTTPJSONBody(t, router, http.MethodPut, path, payload, wantStatus)
}

func postHTTPJSON(t *testing.T, router http.Handler, path string, payload map[string]any, wantStatus int) map[string]any {
	t.Helper()

	return requestHTTPJSON(t, router, http.MethodPost, path, payload, wantStatus)
}

func postHTTPJSONBody(t *testing.T, router http.Handler, path string, payload map[string]any, wantStatus int) string {
	t.Helper()

	return requestHTTPJSONBody(t, router, http.MethodPost, path, payload, wantStatus)
}

func postHTTPJSONNoContent(t *testing.T, router http.Handler, path string, payload map[string]any, wantStatus int) {
	t.Helper()

	body := postHTTPJSONBody(t, router, path, payload, wantStatus)
	if strings.TrimSpace(body) != "" {
		t.Fatalf("POST %s body = %q, want empty body", path, body)
	}
}

func requestHTTPJSON(t *testing.T, router http.Handler, method, path string, payload map[string]any, wantStatus int) map[string]any {
	t.Helper()

	raw := requestHTTPJSONBody(t, router, method, path, payload, wantStatus)
	return decodeJSONMap(t, method+" "+path, []byte(raw))
}

func requestHTTPJSONBody(t *testing.T, router http.Handler, method, path string, payload map[string]any, wantStatus int) string {
	t.Helper()

	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal %s %s payload: %v", method, path, err)
	}
	csrfToken, csrfCookies := issueHTTPCSRF(t, router)
	req := httptest.NewRequest(method, path, strings.NewReader(string(body)))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-CSRF-Token", csrfToken)
	for _, cookie := range csrfCookies {
		req.AddCookie(cookie)
	}
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != wantStatus {
		t.Fatalf("%s %s = %d, want %d: %s", method, path, rec.Code, wantStatus, rec.Body.String())
	}
	return rec.Body.String()
}

func decodeJSONMap(t *testing.T, label string, raw []byte) map[string]any {
	t.Helper()

	value := decodeJSONValue(t, label, raw)
	out, ok := value.(map[string]any)
	if !ok {
		t.Fatalf("%s decoded as %T: %#v, want object", label, value, value)
	}
	return out
}

func deleteHTTPStatus(t *testing.T, router http.Handler, path string, wantStatus int) string {
	t.Helper()

	csrfToken, csrfCookies := issueHTTPCSRF(t, router)
	req := httptest.NewRequest(http.MethodDelete, path, nil)
	req.Header.Set("X-CSRF-Token", csrfToken)
	for _, cookie := range csrfCookies {
		req.AddCookie(cookie)
	}
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != wantStatus {
		t.Fatalf("DELETE %s = %d, want %d: %s", path, rec.Code, wantStatus, rec.Body.String())
	}
	return rec.Body.String()
}

func getHTTPSearch(t *testing.T, router http.Handler, values url.Values) map[string]any {
	t.Helper()

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/search?"+values.Encode(), nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /api/search = %d: %s", rec.Code, rec.Body.String())
	}
	var out map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode HTTP search: %v", err)
	}
	return out
}

func uploadHTTPAsset(t *testing.T, router http.Handler, pageID, filename string, content []byte, wantStatus int) map[string]any {
	t.Helper()

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("file", filename)
	if err != nil {
		t.Fatalf("create multipart file %q: %v", filename, err)
	}
	if _, err := part.Write(content); err != nil {
		t.Fatalf("write multipart file %q: %v", filename, err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close multipart writer: %v", err)
	}

	csrfToken, csrfCookies := issueHTTPCSRF(t, router)
	req := httptest.NewRequest(http.MethodPost, "/api/pages/"+pageID+"/assets", &body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("X-CSRF-Token", csrfToken)
	for _, cookie := range csrfCookies {
		req.AddCookie(cookie)
	}
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != wantStatus {
		t.Fatalf("POST asset %s/%s = %d, want %d: %s", pageID, filename, rec.Code, wantStatus, rec.Body.String())
	}
	return decodeJSONMap(t, "POST asset "+pageID+"/"+filename, rec.Body.Bytes())
}

func getHTTPAssets(t *testing.T, router http.Handler, pageID string) map[string]any {
	t.Helper()

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/pages/"+pageID+"/assets", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET asset list for page %q = %d: %s", pageID, rec.Code, rec.Body.String())
	}
	var out map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode HTTP asset list for page %q: %v", pageID, err)
	}
	return out
}

func getHTTPAsset(t *testing.T, router http.Handler, pageID, filename string) string {
	t.Helper()

	body, _ := getHTTPAssetWithContentType(t, router, pageID, filename)
	return body
}

func getHTTPAssetWithContentType(t *testing.T, router http.Handler, pageID, filename string) (string, string) {
	t.Helper()

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/assets/"+pageID+"/"+url.PathEscape(filename), nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET asset %s/%s = %d: %s", pageID, filename, rec.Code, rec.Body.String())
	}
	return rec.Body.String(), rec.Header().Get("Content-Type")
}

func getHTTPLatestRevision(t *testing.T, router http.Handler, pageID string) map[string]any {
	t.Helper()

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/pages/"+pageID+"/revisions/latest", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET latest revision for page %q = %d: %s", pageID, rec.Code, rec.Body.String())
	}
	var out map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode HTTP latest revision for page %q: %v", pageID, err)
	}
	return out
}

func getHTTPRevision(t *testing.T, router http.Handler, pageID, revisionID string) map[string]any {
	t.Helper()

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/pages/"+pageID+"/revisions/"+revisionID, nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET revision %s/%s = %d: %s", pageID, revisionID, rec.Code, rec.Body.String())
	}
	var out map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode HTTP revision %s/%s: %v", pageID, revisionID, err)
	}
	return out
}

func assertSearchResultsMatch(t *testing.T, mcpSearch, httpSearch map[string]any) {
	t.Helper()

	for _, field := range []string{"count", "offset", "limit"} {
		if mcpSearch[field] != httpSearch[field] {
			t.Fatalf("search %s mismatch: MCP=%v HTTP=%v", field, mcpSearch[field], httpSearch[field])
		}
	}
	mcpItems, ok := mcpSearch["items"].([]any)
	if !ok {
		t.Fatalf("MCP search items has type %T: %#v", mcpSearch["items"], mcpSearch["items"])
	}
	httpItems, ok := httpSearch["items"].([]any)
	if !ok {
		t.Fatalf("HTTP search items has type %T: %#v", httpSearch["items"], httpSearch["items"])
	}
	if len(mcpItems) != len(httpItems) {
		t.Fatalf("search item count mismatch: MCP=%d HTTP=%d", len(mcpItems), len(httpItems))
	}
	assertJSONEqual(t, "search items", mcpItems, httpItems)
	assertJSONEqual(t, "search tagFacets", mcpSearch["tagFacets"], httpSearch["tag_facets"])

	count, ok := httpSearch["count"].(float64)
	if !ok {
		t.Fatalf("HTTP search count has type %T: %#v", httpSearch["count"], httpSearch["count"])
	}
	offset, ok := httpSearch["offset"].(float64)
	if !ok {
		t.Fatalf("HTTP search offset has type %T: %#v", httpSearch["offset"], httpSearch["offset"])
	}
	wantHasMore := int(offset)+len(httpItems) < int(count)
	if mcpSearch["hasMore"] != wantHasMore {
		t.Fatalf("search hasMore mismatch: MCP=%v HTTP-derived=%v", mcpSearch["hasMore"], wantHasMore)
	}
}

func assertMapFieldsEqual(t *testing.T, label string, got, want map[string]any, fields []string) {
	t.Helper()

	for _, field := range fields {
		if !reflect.DeepEqual(normalizeJSON(t, got[field]), normalizeJSON(t, want[field])) {
			t.Fatalf("%s field %s mismatch:\n got: %#v\nwant: %#v", label, field, got[field], want[field])
		}
	}
}

func assertRestoredMetadata(t *testing.T, label string, page map[string]any) {
	t.Helper()

	if got := page["content"]; got != "metadata revision\n" {
		t.Fatalf("%s content = %v, want restored metadata revision content", label, got)
	}
	if got := strings.Join(stringSliceField(t, page, "tags"), ","); got != "restore,metadata" {
		t.Fatalf("%s tags = %v, want restore,metadata", label, got)
	}
	props := nestedMap(t, page, "properties")
	if got := props["status"]; got != "archived" {
		t.Fatalf("%s properties.status = %v, want archived", label, got)
	}
}

func assertPageVersionConflictParity(t *testing.T, label, mcpErr, httpBody string) {
	t.Helper()

	assertMCPPageError(t, label+" MCP", mcpErr, wikipages.ErrCodePageVersionConflict, "Page was changed by another request")
	assertHTTPPageError(t, label+" HTTP", httpBody, wikipages.ErrCodePageVersionConflict, "Page was changed by another request")
}

func assertMCPPageError(t *testing.T, label, errText, code, message string) {
	t.Helper()

	want := code + ": " + message
	if errText != want {
		t.Fatalf("%s error = %q, want %q", label, errText, want)
	}
}

func assertHTTPPageError(t *testing.T, label, body, code, message string) {
	t.Helper()

	payload := decodeJSONMap(t, label, []byte(body))
	errPayload := nestedMap(t, payload, "error")
	if got := errPayload["code"]; got != code {
		t.Fatalf("%s error.code = %v, want %q; body=%s", label, got, code, body)
	}
	if got := errPayload["message"]; got != message {
		t.Fatalf("%s error.message = %v, want %q; body=%s", label, got, message, body)
	}
}

func assertRestorePayloadsMatch(t *testing.T, mcpRestored, httpRestored map[string]any) {
	t.Helper()

	assertRestoreVolatileFieldsPresent(t, "MCP wiki_restore_revision", mcpRestored)
	assertRestoreVolatileFieldsPresent(t, "HTTP wiki_restore_revision", httpRestored)
	assertJSONEqual(t, "wiki_restore_revision response payload", normalizeRestorePayload(t, mcpRestored), normalizeRestorePayload(t, httpRestored))
}

func assertRestoreVolatileFieldsPresent(t *testing.T, label string, page map[string]any) {
	t.Helper()

	_ = stringField(t, page, "version")
	metadata := nestedMap(t, page, "metadata")
	updatedAt := stringField(t, metadata, "updatedAt")
	if _, err := time.Parse(time.RFC3339, updatedAt); err != nil {
		t.Fatalf("%s metadata.updatedAt = %q, want RFC3339 timestamp: %v", label, updatedAt, err)
	}
}

func normalizeRestorePayload(t *testing.T, page map[string]any) map[string]any {
	t.Helper()

	normalizedValue := normalizeJSON(t, page)
	normalized, ok := normalizedValue.(map[string]any)
	if !ok {
		t.Fatalf("restore payload normalized to %T, want object", normalizedValue)
	}
	delete(normalized, "version")
	if metadata, ok := normalized["metadata"].(map[string]any); ok {
		delete(metadata, "updatedAt")
	}
	return normalized
}

func assertPageState(t *testing.T, label string, page map[string]any, id, title, slug, pathValue, kind, parentID string) {
	t.Helper()

	want := map[string]string{
		"id":    id,
		"title": title,
		"slug":  slug,
		"path":  pathValue,
		"kind":  kind,
	}
	for field, expected := range want {
		if got := stringValue(page[field]); got != expected {
			t.Fatalf("%s field %s = %q, want %q; page=%#v", label, field, got, expected, page)
		}
	}
	if parentID == "" {
		if got := stringValue(page["parentId"]); got != "" {
			t.Fatalf("%s parentId = %q, want root parent; page=%#v", label, got, page)
		}
		return
	}
	if got := stringValue(page["parentId"]); got != parentID {
		t.Fatalf("%s parentId = %q, want %q; page=%#v", label, got, parentID, page)
	}
}

func assertChildrenDoNotContain(t *testing.T, label string, page map[string]any, childIDs ...string) {
	t.Helper()

	disallowed := map[string]struct{}{}
	for _, id := range childIDs {
		disallowed[id] = struct{}{}
	}
	children, _ := page["children"].([]any)
	for _, raw := range children {
		child, ok := raw.(map[string]any)
		if !ok {
			t.Fatalf("%s child has type %T: %#v", label, raw, raw)
		}
		id := stringValue(child["id"])
		if _, exists := disallowed[id]; exists {
			t.Fatalf("%s contains moved child %q: %#v", label, id, page["children"])
		}
	}
}

func assertChildOrder(t *testing.T, label string, page map[string]any, childIDs ...string) {
	t.Helper()

	children, ok := page["children"].([]any)
	if !ok {
		t.Fatalf("%s children has type %T: %#v", label, page["children"], page["children"])
	}
	if len(children) != len(childIDs) {
		t.Fatalf("%s child count = %d, want %d: %#v", label, len(children), len(childIDs), page["children"])
	}
	for i, wantID := range childIDs {
		child, ok := children[i].(map[string]any)
		if !ok {
			t.Fatalf("%s child %d has type %T: %#v", label, i, children[i], children[i])
		}
		if got := stringValue(child["id"]); got != wantID {
			t.Fatalf("%s child %d id = %q, want %q: %#v", label, i, got, wantID, page["children"])
		}
	}
}

func readPageMarkdownByRoutePath(t *testing.T, rootDir, routePath string) string {
	t.Helper()

	path := filepath.Join(append([]string{rootDir}, strings.Split(routePath, "/")...)...) + ".md"
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read markdown page %s: %v", path, err)
	}
	return string(raw)
}

func assertCanonicalPageMarkdown(t *testing.T, label, raw string) markdown.PageDocument {
	t.Helper()

	if !strings.HasPrefix(raw, "<!-- leafwiki\n") {
		t.Fatalf("%s should start with canonical LeafWiki metadata, got:\n%s", label, raw)
	}
	if strings.HasPrefix(raw, "---\n") {
		t.Fatalf("%s should not start with legacy YAML frontmatter, got:\n%s", label, raw)
	}
	doc, _, err := markdown.ParsePageDocument(raw)
	if err != nil {
		t.Fatalf("%s should parse with ParsePageDocument: %v\n%s", label, err, raw)
	}
	return doc
}

func assertMCPToolErrorContains(t *testing.T, session *sdkmcp.ClientSession, name string, args map[string]any, want string) {
	t.Helper()

	errText := callToolError(t, session, name, args)
	if !strings.Contains(strings.ToLower(errText), strings.ToLower(want)) {
		t.Fatalf("%s error = %q, want detail containing %q", name, errText, want)
	}
}

func assertErrorContainsAny(t *testing.T, label, errText string, wants ...string) {
	t.Helper()

	lower := strings.ToLower(errText)
	for _, want := range wants {
		if strings.Contains(lower, strings.ToLower(want)) {
			return
		}
	}
	t.Fatalf("%s error = %q, want one of %q", label, errText, wants)
}

func assertErrorContainsAll(t *testing.T, label, errText string, wants []string) {
	t.Helper()

	for _, want := range wants {
		if !strings.Contains(errText, want) {
			t.Fatalf("%s error = %q, missing %q", label, errText, want)
		}
	}
}

func assertErrorDoesNotContainAny(t *testing.T, label, errText string, rejects ...string) {
	t.Helper()

	lower := strings.ToLower(errText)
	for _, reject := range rejects {
		if strings.Contains(lower, strings.ToLower(reject)) {
			t.Fatalf("%s error = %q, did not want detail containing %q", label, errText, reject)
		}
	}
}

func stringValue(v any) string {
	if v == nil {
		return ""
	}
	if s, ok := v.(string); ok {
		return s
	}
	return strings.TrimSpace(fmt.Sprint(v))
}

func assertAssetURLResult(t *testing.T, label string, result map[string]any, field, pageID string) {
	t.Helper()

	raw, ok := result[field].(string)
	if !ok {
		t.Fatalf("%s %s has type %T: %#v, want string", label, field, result[field], result[field])
	}
	prefix := "/assets/" + pageID + "/"
	if !strings.HasPrefix(raw, prefix) || len(raw) <= len(prefix) {
		t.Fatalf("%s %s = %q, want asset URL under %s", label, field, raw, prefix)
	}
	if len(result) != 1 {
		t.Fatalf("%s result = %#v, want only %q field", label, result, field)
	}
}

func assertJSONEqual(t *testing.T, label string, got, want any) {
	t.Helper()

	normalizedGot := normalizeJSON(t, got)
	normalizedWant := normalizeJSON(t, want)
	if !reflect.DeepEqual(normalizedGot, normalizedWant) {
		gotJSON, _ := json.MarshalIndent(normalizedGot, "", "  ")
		wantJSON, _ := json.MarshalIndent(normalizedWant, "", "  ")
		t.Fatalf("%s mismatch:\n got: %s\nwant: %s", label, gotJSON, wantJSON)
	}
}

func normalizeJSON(t *testing.T, value any) any {
	t.Helper()

	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal JSON value %T: %v", value, err)
	}
	return decodeJSONValue(t, "normalize JSON", raw)
}

func decodeJSONValue(t *testing.T, label string, raw []byte) any {
	t.Helper()

	var out any
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("decode %s: %v: %s", label, err, raw)
	}
	return out
}

func issueHTTPCSRF(t *testing.T, router http.Handler) (string, []*http.Cookie) {
	t.Helper()

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/config", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /api/config for CSRF = %d: %s", rec.Code, rec.Body.String())
	}
	token := rec.Header().Get("X-CSRF-Token")
	result := rec.Result()
	defer result.Body.Close()
	if token == "" {
		for _, cookie := range result.Cookies() {
			if cookie.Name == "leafwiki_csrf" || cookie.Name == "__Host-leafwiki_csrf" {
				token = cookie.Value
				break
			}
		}
	}
	if token == "" {
		t.Fatalf("GET /api/config did not issue CSRF token")
	}
	return token, result.Cookies()
}

func nestedMap(t *testing.T, value map[string]any, key string) map[string]any {
	t.Helper()

	nested, ok := value[key].(map[string]any)
	if !ok {
		t.Fatalf("%q has type %T: %#v", key, value[key], value[key])
	}
	return nested
}

func stringField(t *testing.T, value map[string]any, key string) string {
	t.Helper()

	s, ok := value[key].(string)
	if !ok || s == "" {
		t.Fatalf("%q has type %T and value %#v, want non-empty string", key, value[key], value[key])
	}
	return s
}

func assertLookupFinalID(t *testing.T, label string, lookup map[string]any, wantID string, wantKind string) {
	t.Helper()

	segmentsRaw, ok := lookup["segments"].([]any)
	if !ok || len(segmentsRaw) == 0 {
		t.Fatalf("%s segments = %#v, want non-empty list", label, lookup["segments"])
	}
	final, ok := segmentsRaw[len(segmentsRaw)-1].(map[string]any)
	if !ok {
		t.Fatalf("%s final segment = %#v, want object", label, segmentsRaw[len(segmentsRaw)-1])
	}
	if got := final["id"]; got != wantID {
		t.Fatalf("%s final id = %v, want %q", label, got, wantID)
	}
	if got := final["kind"]; got != wantKind {
		t.Fatalf("%s final kind = %v, want %q", label, got, wantKind)
	}
}

func arrayField(t *testing.T, value map[string]any, key string) []any {
	t.Helper()

	return arrayFieldFromMap(t, value, key)
}

func arrayFieldFromMap(t *testing.T, value map[string]any, key string) []any {
	t.Helper()

	raw, ok := value[key].([]any)
	if !ok {
		t.Fatalf("%q has type %T: %#v", key, value[key], value[key])
	}
	return raw
}

func stringSliceField(t *testing.T, value map[string]any, key string) []string {
	t.Helper()

	raw, ok := value[key].([]any)
	if !ok {
		t.Fatalf("%q has type %T: %#v", key, value[key], value[key])
	}
	out := make([]string, 0, len(raw))
	for _, item := range raw {
		s, ok := item.(string)
		if !ok {
			t.Fatalf("%q item has type %T: %#v", key, item, item)
		}
		out = append(out, s)
	}
	return out
}

func arrayContainsObjectField(value any, field string, want any) bool {
	items, ok := value.([]any)
	if !ok {
		return false
	}
	for _, item := range items {
		obj, ok := item.(map[string]any)
		if ok && obj[field] == want {
			return true
		}
	}
	return false
}

func objectWithField(t *testing.T, value any, field string, want any) map[string]any {
	t.Helper()

	items, ok := value.([]any)
	if !ok {
		t.Fatalf("value = %T %#v, want array", value, value)
	}
	for _, item := range items {
		obj, ok := item.(map[string]any)
		if ok && obj[field] == want {
			return obj
		}
	}
	t.Fatalf("array = %#v, want object with %s=%#v", value, field, want)
	return nil
}

func changedPathsFromContext(t *testing.T, output map[string]any) map[string]bool {
	t.Helper()

	paths := map[string]bool{}
	for _, raw := range arrayField(t, output, "changesSincePreviousContext") {
		change, ok := raw.(map[string]any)
		if !ok {
			t.Fatalf("change has type %T: %#v", raw, raw)
		}
		for _, value := range arrayFieldFromMap(t, change, "changedPaths") {
			path, ok := value.(string)
			if !ok {
				t.Fatalf("changed path has type %T: %#v", value, value)
			}
			paths[path] = true
		}
	}
	return paths
}

func assertValidationIssueCodes(t *testing.T, output map[string]any, wantCodes []string) {
	t.Helper()

	issues, ok := output["issues"].([]any)
	if !ok {
		t.Fatalf("validation issues = %T %#v, want array", output["issues"], output["issues"])
	}
	got := map[string]struct{}{}
	for _, issue := range issues {
		item, ok := issue.(map[string]any)
		if !ok {
			t.Fatalf("validation issue = %T %#v, want object", issue, issue)
		}
		code, ok := item["code"].(string)
		if !ok {
			t.Fatalf("validation issue code = %T %#v, want string", item["code"], item["code"])
		}
		got[code] = struct{}{}
	}
	for _, want := range wantCodes {
		if _, exists := got[want]; !exists {
			t.Fatalf("validation issue codes = %#v, want %q in output %#v", got, want, output)
		}
	}
}

func validationIssueByCode(t *testing.T, output map[string]any, wantCode string) map[string]any {
	t.Helper()

	issues, ok := output["issues"].([]any)
	if !ok {
		t.Fatalf("validation issues = %T %#v, want array", output["issues"], output["issues"])
	}
	for _, issue := range issues {
		item, ok := issue.(map[string]any)
		if !ok {
			t.Fatalf("validation issue = %T %#v, want object", issue, issue)
		}
		if item["code"] == wantCode {
			return item
		}
	}
	t.Fatalf("validation issues = %#v, want issue code %q", issues, wantCode)
	return nil
}

func assertNoValidationIssuePath(t *testing.T, output map[string]any, unwantedPath string) {
	t.Helper()

	issues, ok := output["issues"].([]any)
	if !ok {
		t.Fatalf("validation issues = %T %#v, want array", output["issues"], output["issues"])
	}
	for _, issue := range issues {
		item, ok := issue.(map[string]any)
		if !ok {
			t.Fatalf("validation issue = %T %#v, want object", issue, issue)
		}
		if item["path"] == unwantedPath {
			t.Fatalf("validation issues = %#v, did not expect issue for hidden path %q", issues, unwantedPath)
		}
	}
}

func assertValidationIssueCodesAbsent(t *testing.T, output map[string]any, absentCodes []string) {
	t.Helper()

	issues, ok := output["issues"].([]any)
	if !ok {
		t.Fatalf("validation issues = %T %#v, want array", output["issues"], output["issues"])
	}
	for _, issue := range issues {
		item, ok := issue.(map[string]any)
		if !ok {
			t.Fatalf("validation issue = %T %#v, want object", issue, issue)
		}
		code, ok := item["code"].(string)
		if !ok {
			t.Fatalf("validation issue code = %T %#v, want string", item["code"], item["code"])
		}
		if contains(absentCodes, code) {
			t.Fatalf("validation issue codes include forbidden %q in output %#v", code, output)
		}
	}
}

func validationIssueCodeCount(t *testing.T, output map[string]any, wantCode string, wantPath string) int {
	t.Helper()

	issues, ok := output["issues"].([]any)
	if !ok {
		t.Fatalf("validation issues = %T %#v, want array", output["issues"], output["issues"])
	}
	count := 0
	for _, issue := range issues {
		item, ok := issue.(map[string]any)
		if !ok {
			t.Fatalf("validation issue = %T %#v, want object", issue, issue)
		}
		if item["code"] == wantCode && item["path"] == wantPath {
			count++
		}
	}
	return count
}

func assertRecentChangesIncludePath(t *testing.T, output map[string]any, wantPath string) {
	t.Helper()

	for _, rawChange := range arrayField(t, output, "recentChanges") {
		change, ok := rawChange.(map[string]any)
		if !ok {
			t.Fatalf("recent change = %T %#v, want object", rawChange, rawChange)
		}
		for _, rawPath := range arrayFieldFromMap(t, change, "changedPaths") {
			if rawPath == wantPath {
				return
			}
		}
	}
	t.Fatalf("recentChanges = %#v, missing changed path %q", output["recentChanges"], wantPath)
}

func arrayContainsString(value any, want string) bool {
	items, ok := value.([]any)
	if !ok {
		return false
	}
	for _, item := range items {
		if item == want {
			return true
		}
	}
	return false
}
