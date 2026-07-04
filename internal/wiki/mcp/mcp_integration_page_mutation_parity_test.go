package mcp_test

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/perber/wiki/internal/core/assets"
	sharederrors "github.com/perber/wiki/internal/core/shared/errors"
	httpinternal "github.com/perber/wiki/internal/http"
	wikimcp "github.com/perber/wiki/internal/wiki/mcp"
	wikipages "github.com/perber/wiki/internal/wiki/pages"
)

// Canonical Markdown links plan scenarios covered by tests in this file:
// - MCP agent context returns canonical examples

var _ = Describe("local MCP base path registration", Label("integration"), func() {
	It("mounts the endpoint only under the configured base path", func() {
		w := newLocalMCPTestWiki(false)
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
			Expect(rec).To(HaveHTTPStatus(http.StatusNotFound))
		}

		for _, method := range []string{http.MethodGet, http.MethodPost, http.MethodDelete} {
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(method, "/wiki/mcp", strings.NewReader("{}"))
			req.RemoteAddr = "127.0.0.1:12345"
			router.ServeHTTP(rec, req)
			Expect(rec).NotTo(HaveHTTPStatus(http.StatusNotFound))
		}

		session := connectLocalMCP(router, "/wiki/mcp")
		Expect(listAllToolNames(session)).To(matchToolNames(federatedToolNames()))
	})
})

var _ = Describe("local MCP page mutation parity", Label("integration"), func() {
	It("keeps page mutation tools aligned with HTTP routes", func() {
		runLocalMCPProtocolPageMutationParity()
	})
})

func runLocalMCPProtocolPageMutationParity() {
	GinkgoHelper()

	w, _ := newLocalMCPTestWikiWithStorage()
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

	invalidCreateKindErr := callToolStructuredError(session, "wiki_create_page", map[string]any{
		"title": "Invalid Kind",
		"slug":  "invalid-kind",
		"kind":  "folder",
	})
	Expect(invalidCreateKindErr).To(matchMCPPageError(wikipages.ErrCodePageInvalidKind, sharederrors.MessageIDForCode(wikipages.ErrCodePageInvalidKind)), mcpLabelCreatePageInvalidKind)
	paddedCreateKindErr := callToolStructuredError(session, "wiki_create_page", map[string]any{
		"title": "Padded Kind",
		"slug":  "padded-kind",
		"kind":  " page ",
	})
	Expect(paddedCreateKindErr).To(matchMCPPageError(wikipages.ErrCodePageInvalidKind, sharederrors.MessageIDForCode(wikipages.ErrCodePageInvalidKind)), mcpLabelCreatePagePaddedKind)
	invalidCreateKindHTTP := postHTTPJSONBody(router, "/api/pages", map[string]any{
		"title": "Invalid Kind HTTP",
		"slug":  "invalid-kind-http",
		"kind":  "folder",
	}, http.StatusBadRequest)
	Expect(invalidCreateKindHTTP).To(matchHTTPPageError(wikipages.ErrCodePageInvalidKind, sharederrors.MessageIDForCode(wikipages.ErrCodePageInvalidKind)))
	paddedCreateKindHTTP := postHTTPJSONBody(router, "/api/pages", map[string]any{
		"title": "Padded Kind HTTP",
		"slug":  "padded-kind-http",
		"kind":  " page ",
	}, http.StatusBadRequest)
	Expect(paddedCreateKindHTTP).To(matchHTTPPageError(wikipages.ErrCodePageInvalidKind, sharederrors.MessageIDForCode(wikipages.ErrCodePageInvalidKind)))
	nullKindCreated := nestedMap(callToolStructured(session, "wiki_create_page", map[string]any{
		"title": "Null Kind",
		"slug":  "null-kind",
		"kind":  nil,
	}), "page")
	Expect(nullKindCreated).To(HaveKeyWithValue("kind", "page"))

	created := callToolStructured(session, "wiki_create_page", map[string]any{
		"title": "MCP Draft",
		"slug":  "mcp-draft",
		"kind":  "page",
	})
	createdPage := nestedMap(created, "page")
	pageID := stringField(createdPage, "id")
	version := stringField(createdPage, "version")

	httpPage := getHTTPPageByPath(router, "mcp-draft")
	Expect(httpPage).To(HaveKeyWithValue("id", pageID))
	Expect(createdPage).To(matchJSONEqual(getHTTPPageByID(router, pageID)), "create_page HTTP page")

	mcpCreateParent := nestedMap(callToolStructured(session, "wiki_create_page", map[string]any{
		"title": "MCP Create Parent",
		"slug":  "mcp-create-parent",
		"kind":  "section",
	}), "page")
	httpCreateParent := postHTTPJSON(router, "/api/pages", map[string]any{
		"title": "HTTP Create Parent",
		"slug":  "http-create-parent",
		"kind":  "section",
	}, http.StatusCreated)
	whitespaceParentCreateErr := callToolStructuredError(session, "wiki_create_page", map[string]any{
		"parentId": " ",
		"title":    "Whitespace Parent",
		"slug":     "whitespace-parent",
		"kind":     "page",
	})
	Expect(whitespaceParentCreateErr).To(matchMCPPageError(wikipages.ErrCodePageInvalidParentID, sharederrors.MessageIDForCode(wikipages.ErrCodePageInvalidParentID)), mcpLabelCreatePageWhitespaceParentID)
	whitespaceParentCreateHTTP := postHTTPJSONBody(router, "/api/pages", map[string]any{
		"parentId": " ",
		"title":    "Whitespace Parent HTTP",
		"slug":     "whitespace-parent-http",
		"kind":     "page",
	}, http.StatusBadRequest)
	Expect(whitespaceParentCreateHTTP).To(matchHTTPPageError(wikipages.ErrCodePageInvalidParentID, sharederrors.MessageIDForCode(wikipages.ErrCodePageInvalidParentID)))
	paddedParentCreateErr := callToolStructuredError(session, "wiki_create_page", map[string]any{
		"parentId": " " + stringField(mcpCreateParent, "id") + " ",
		"title":    "Padded Parent",
		"slug":     "padded-parent",
		"kind":     "page",
	})
	Expect(paddedParentCreateErr).To(matchMCPPageError(wikipages.ErrCodePageInvalidParentID, sharederrors.MessageIDForCode(wikipages.ErrCodePageInvalidParentID)), mcpLabelCreatePagePaddedParentID)
	paddedParentCreateHTTP := postHTTPJSONBody(router, "/api/pages", map[string]any{
		"parentId": " " + stringField(httpCreateParent, "id") + " ",
		"title":    "Padded Parent HTTP",
		"slug":     "padded-parent-http",
		"kind":     "page",
	}, http.StatusBadRequest)
	Expect(paddedParentCreateHTTP).To(matchHTTPPageError(wikipages.ErrCodePageInvalidParentID, sharederrors.MessageIDForCode(wikipages.ErrCodePageInvalidParentID)))
	mcpCreatedChild := nestedMap(callToolStructured(session, "wiki_create_page", map[string]any{
		"parentId": stringField(mcpCreateParent, "id"),
		"title":    "Created Child",
		"slug":     "created-child",
		"kind":     "section",
	}), "page")
	httpCreatedChild := postHTTPJSON(router, "/api/pages", map[string]any{
		"parentId": stringField(httpCreateParent, "id"),
		"title":    "Created Child",
		"slug":     "created-child",
		"kind":     "section",
	}, http.StatusCreated)
	Expect(getHTTPPageByPath(router, "mcp-create-parent/created-child")).To(matchPageState(stringField(mcpCreatedChild, "id"), "Created Child", "created-child", "mcp-create-parent/created-child", "section", ""), "MCP create_page child")
	Expect(getHTTPPageByPath(router, "http-create-parent/created-child")).To(matchPageState(stringField(httpCreatedChild, "id"), "Created Child", "created-child", "http-create-parent/created-child", "section", ""), "HTTP create_page child")
	recordHTTPMCPParity("wiki_create_page", "POST /api/pages")

	content := "Hello from MCP\n"
	updated := callToolStructured(session, "wiki_update_page", map[string]any{
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
	updatedPage := nestedMap(updated, "page")
	Expect(updatedPage).To(HaveKeyWithValue("content", content))

	httpPage = getHTTPPageByPath(router, "mcp-draft")
	Expect(httpPage).To(SatisfyAll(
		HaveKeyWithValue("title", "MCP Draft Updated"),
		HaveKeyWithValue("content", content),
	))
	Expect(updatedPage).To(matchJSONEqual(getHTTPPageByID(router, pageID)), "wiki_update_page HTTP page")
	Expect(stringSliceField(httpPage, "tags")).To(Equal([]string{"mcp", "parity"}))
	props := nestedMap(httpPage, "properties")
	Expect(props).To(HaveKeyWithValue("status", "draft"))
	rawMCPMetadata := readPageMarkdownByRoutePath(w.GetRootDir(), "mcp-draft")
	mcpDoc := canonicalPageMarkdown("MCP update raw markdown", rawMCPMetadata)
	Expect(mcpDoc.Metadata).To(SatisfyAll(
		HaveField("Tags", Equal([]string{"mcp", "parity"})),
		HaveField("Fields", HaveKeyWithValue("status", "draft")),
	))
	Expect(rawMCPMetadata).To(SatisfyAll(
		ContainSubstring("tags:"),
		ContainSubstring("- mcp"),
		ContainSubstring("status: draft"),
	))
	httpMetadataPage := postHTTPJSON(router, "/api/pages", map[string]any{
		"title": "HTTP Metadata",
		"slug":  "http-metadata",
		"kind":  "page",
	}, http.StatusCreated)
	httpMetadataUpdated := updateHTTPPage(router, stringField(httpMetadataPage, "id"), map[string]any{
		"version": stringField(httpMetadataPage, "version"),
		"title":   "HTTP Metadata Updated",
		"slug":    "http-metadata",
		"content": "HTTP metadata content\n",
		"tags":    []string{"HTTP", "Metadata"},
		"properties": map[string]string{
			"status": "review",
		},
	})
	Expect(stringSliceField(httpMetadataUpdated, "tags")).To(Equal([]string{"http", "metadata"}))
	httpMetadataProps := nestedMap(httpMetadataUpdated, "properties")
	Expect(httpMetadataProps).To(HaveKeyWithValue("status", "review"))
	rawHTTPMetadata := readPageMarkdownByRoutePath(w.GetRootDir(), "http-metadata")
	httpDoc := canonicalPageMarkdown("HTTP update raw markdown", rawHTTPMetadata)
	Expect(httpDoc.Metadata).To(SatisfyAll(
		HaveField("Tags", Equal([]string{"http", "metadata"})),
		HaveField("Fields", HaveKeyWithValue("status", "review")),
	))
	Expect(rawHTTPMetadata).To(SatisfyAll(
		ContainSubstring("tags:"),
		ContainSubstring("- http"),
		ContainSubstring("status: review"),
	))
	recordHTTPMCPParity("wiki_update_page", "PUT /api/pages/:id")

	metadataErr := callToolStructuredError(session, "wiki_update_page", map[string]any{
		"id":      pageID,
		"version": stringField(updatedPage, "version"),
		"title":   "MCP Draft Updated",
		"slug":    "mcp-draft",
		"content": content,
		"tags":    []any{"mcp", "MCP"},
		"properties": map[string]any{
			"leafwiki_hidden": "forbidden",
		},
	})
	Expect(metadataErr).To(matchMCPStructuredError(wikimcp.ErrCodeMCPToolError, sharederrors.MessageIDForCode(wikimcp.ErrCodeMCPToolError)), mcpLabelMetadataValidationError)

	csrfToken, csrfCookies := issueHTTPCSRF(router)
	staleHTTPBody := strings.NewReader(`{"version":"` + version + `","title":"MCP Draft Stale","slug":"mcp-draft","content":"stale"}`)
	staleHTTPReq := httptest.NewRequest(http.MethodPut, "/api/pages/"+pageID, staleHTTPBody)
	staleHTTPReq.Header.Set("Content-Type", "application/json")
	staleHTTPReq.Header.Set("X-CSRF-Token", csrfToken)
	for _, cookie := range csrfCookies {
		staleHTTPReq.AddCookie(cookie)
	}
	staleHTTPRec := httptest.NewRecorder()
	router.ServeHTTP(staleHTTPRec, staleHTTPReq)
	Expect(staleHTTPRec).To(HaveHTTPStatus(http.StatusConflict), staleHTTPRec.Body.String())

	mcpErr := callToolStructuredError(session, "wiki_update_page", map[string]any{
		"id":      pageID,
		"version": version,
		"title":   "MCP Draft Stale",
		"slug":    "mcp-draft",
		"content": "stale",
	})
	Expect(mcpErr).To(matchMCPPageVersionConflict(), "stale wiki_update_page MCP")
	Expect(staleHTTPRec.Body.String()).To(matchHTTPPageVersionConflict(), "stale wiki_update_page HTTP")

	search := callToolStructured(session, "wiki_search_pages", map[string]any{
		"q":      "Hello",
		"offset": float64(0),
		"limit":  float64(10),
	})
	httpSearch := getHTTPSearch(router, url.Values{
		"q":      {"Hello"},
		"offset": {"0"},
		"limit":  {"10"},
	})
	Expect(search).To(matchSearchResults(httpSearch))
	Expect(search).To(HaveKeyWithValue("count", float64(1)))
	items := arrayFieldFromMap(search, "items")
	Expect(items).To(HaveExactElements(HaveKeyWithValue("page_id", pageID)))
	tagSearch := callToolStructured(session, "wiki_search_pages", map[string]any{
		"tags":   []any{"mcp"},
		"offset": float64(0),
		"limit":  float64(10),
	})
	tagHTTPSearch := getHTTPSearch(router, url.Values{
		"tags":   {"mcp"},
		"offset": {"0"},
		"limit":  {"10"},
	})
	Expect(tagSearch).To(matchSearchResults(tagHTTPSearch))

	for i := 1; i <= 2; i++ {
		extra := nestedMap(callToolStructured(session, "wiki_create_page", map[string]any{
			"title": "Hello Extra " + strconv.Itoa(i),
			"slug":  "hello-extra-" + strconv.Itoa(i),
			"kind":  "page",
		}), "page")
		extraContent := "Hello paginated search " + strconv.Itoa(i)
		callToolStructured(session, "wiki_update_page", map[string]any{
			"id":      stringField(extra, "id"),
			"version": stringField(extra, "version"),
			"title":   extra["title"],
			"slug":    extra["slug"],
			"content": extraContent,
			"tags":    []any{"mcp"},
		})
	}
	paginatedSearch := callToolStructured(session, "wiki_search_pages", map[string]any{
		"q":      "Hello",
		"offset": float64(0),
		"limit":  float64(1),
	})
	paginatedHTTP := getHTTPSearch(router, url.Values{
		"q":      {"Hello"},
		"offset": {"0"},
		"limit":  {"1"},
	})
	Expect(paginatedSearch).To(matchSearchResults(paginatedHTTP))
	Expect(paginatedSearch).To(HaveKeyWithValue("hasMore", true))
	recordHTTPMCPParity("wiki_search_pages", "GET /api/search")
}
