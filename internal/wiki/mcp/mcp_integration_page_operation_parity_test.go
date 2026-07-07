package mcp_test

import (
	"net/http"
	"net/http/httptest"
	"net/url"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/perber/wiki/internal/core/assets"
	sharederrors "github.com/perber/wiki/internal/core/shared/errors"
	"github.com/perber/wiki/internal/core/tree"
	httpinternal "github.com/perber/wiki/internal/http"
	wikimcp "github.com/perber/wiki/internal/wiki/mcp"
	wikipages "github.com/perber/wiki/internal/wiki/pages"
)

// Canonical Markdown links plan scenarios covered by tests in this file:
// - MCP agent context returns canonical examples

var _ = Describe("local MCP page operation parity", Label("integration"), func() {
	It("keeps page operation tools aligned with HTTP routes", func() {
		runLocalMCPProtocolPageOperationParity()
	})
})

func runLocalMCPProtocolPageOperationParity() {
	GinkgoHelper()

	w := newLocalMCPTestWiki(false)
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
	session := connectLocalMCP(router, "/mcp")

	current := callToolStructured(session, "wiki_get_current_user", nil)
	user := nestedMap(current, "user")
	Expect(user).To(SatisfyAll(
		HaveKeyWithValue("username", "public-editor"),
		HaveKeyWithValue("role", "editor"),
	))
	httpUser := getHTTPMap(router, "/api/auth/me")
	Expect(user).To(matchJSONEqual(httpUser), "wiki_get_current_user")
	recordHTTPMCPParity("wiki_get_current_user", "GET /api/auth/me")
	config := callToolStructured(session, "wiki_get_config", nil)
	Expect(config).To(SatisfyAll(
		HaveKeyWithValue("authDisabled", true),
		HaveKeyWithValue("maxAssetUploadSizeBytes", float64(assets.DefaultMaxUploadSizeBytes)),
		HaveKeyWithValue("enableWorkspaceSync", true),
		HaveKeyWithValue("markdownLinkRootPrefix", "/docs"),
	))
	httpConfig := getHTTPMap(router, "/api/config")
	Expect(config).To(matchMapFields(httpConfig, []string{
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
	}), "wiki_get_config")

	recordHTTPMCPParity("wiki_get_config", "GET /api/config")

	slug := callToolStructured(session, "wiki_suggest_slug", map[string]any{"title": "Parent Section"})
	httpSlug := getHTTPMap(router, "/api/pages/slug-suggestion?title=Parent+Section")
	Expect(slug).To(matchJSONEqual(httpSlug), "wiki_suggest_slug")
	recordHTTPMCPParity("wiki_suggest_slug", "GET /api/pages/slug-suggestion")
	Expect(slug).To(HaveKeyWithValue("slug", "parent-section"))
	blankSlugErr := callToolStructuredError(session, "wiki_suggest_slug", map[string]any{"title": "   "})
	Expect(blankSlugErr).To(matchMCPPageError(wikipages.ErrCodePageMissingTitle, sharederrors.MessageIDForCode(wikipages.ErrCodePageMissingTitle)))
	blankSlugHTTP := getHTTPStatus(router, "/api/pages/slug-suggestion?title=+++",
		http.StatusBadRequest)
	Expect(blankSlugHTTP).To(matchHTTPPageError(wikipages.ErrCodePageMissingTitle, sharederrors.MessageIDForCode(wikipages.ErrCodePageMissingTitle)), mcpLabelSuggestSlugBlankHTTP)
	punctuationSlugErr := callToolStructuredError(session, "wiki_suggest_slug", map[string]any{"title": "!!!"})
	Expect(punctuationSlugErr).To(matchMCPPageError(wikipages.ErrCodePageInvalidTitle, sharederrors.MessageIDForCode(wikipages.ErrCodePageInvalidTitle)))
	punctuationSlugHTTP := getHTTPStatus(router, "/api/pages/slug-suggestion?title=%21%21%21",
		http.StatusBadRequest)
	Expect(punctuationSlugHTTP).To(matchHTTPPageError(wikipages.ErrCodePageInvalidTitle, sharederrors.MessageIDForCode(wikipages.ErrCodePageInvalidTitle)), mcpLabelSuggestSlugPunctuationHTTP)

	parent := nestedMap(callToolStructured(session, "wiki_create_page", map[string]any{
		"title": "Parent Section",
		"slug":  "parent-section",
		"kind":  "section",
	}), "page")
	parentID := stringField(parent, "id")
	parentViaGet := nestedMap(callToolStructured(session, "wiki_get_page", map[string]any{"id": parentID}), "page")
	parentPageID := tree.PageIDFromString(parentID)
	Expect(parentViaGet).To(matchJSONEqual(getHTTPPageByID(router, parentPageID)), "wiki_get_page")
	recordHTTPMCPParity("wiki_get_page", "GET /api/pages/:id")
	treeResult := callToolStructured(session, "wiki_get_tree", map[string]any{"depth": float64(1)})
	Expect(treeResult).To(HaveKey("tree"))
	httpTree := getHTTPMap(router, "/api/tree?depth=1")
	Expect(treeResult).To(HaveKeyWithValue("tree", matchJSONEqual(httpTree)), "wiki_get_tree")
	recordHTTPMCPParity("wiki_get_tree", "GET /api/tree")

	pageByPath := nestedMap(callToolStructured(session, "wiki_get_page_by_path", map[string]any{"path": "parent-section"}), "page")
	httpPageByPath := getHTTPPageByPath(router, "parent-section")
	Expect(pageByPath).To(matchJSONEqual(httpPageByPath), "wiki_get_page_by_path")
	leadingSlashPageByPath := nestedMap(callToolStructured(session, "wiki_get_page_by_path", map[string]any{"path": "/parent-section"}), "page")
	Expect(leadingSlashPageByPath).To(matchJSONEqual(httpPageByPath), "wiki_get_page_by_path leading slash")
	recordHTTPMCPParity("wiki_get_page_by_path", "GET /api/pages/by-path")
	blankPathErr := callToolStructuredError(session, "wiki_get_page_by_path", map[string]any{"path": "  "})
	Expect(blankPathErr).To(matchMCPPageError(wikipages.ErrCodePageMissingPath, sharederrors.MessageIDForCode(wikipages.ErrCodePageMissingPath)), mcpLabelGetPageByPathBlankMCP)
	blankPathHTTP := getHTTPStatus(router, "/api/pages/by-path?path=++", http.StatusBadRequest)
	Expect(blankPathHTTP).To(matchHTTPPageError(wikipages.ErrCodePageMissingPath, sharederrors.MessageIDForCode(wikipages.ErrCodePageMissingPath)), mcpLabelGetPageByPathBlankHTTP)
	for _, invalidPath := range []string{"docs//intro", "docs/.", "docs/..", `docs\..\secret`} {
		mcpPathErr := callToolStructuredError(session, "wiki_get_page_by_path", map[string]any{"path": invalidPath})
		Expect(mcpPathErr).To(matchMCPPageError(wikipages.ErrCodePageInvalidPath, sharederrors.MessageIDForCode(wikipages.ErrCodePageInvalidPath)), "invalid MCP path %q", invalidPath)
		httpPathErr := getHTTPStatus(router, "/api/pages/by-path?path="+url.QueryEscape(invalidPath), http.StatusBadRequest)
		Expect(httpPathErr).To(matchHTTPPageError(wikipages.ErrCodePageInvalidPath, sharederrors.MessageIDForCode(wikipages.ErrCodePageInvalidPath)), "invalid HTTP path %q", invalidPath)
	}

	childA := nestedMap(callToolStructured(session, "wiki_create_page", map[string]any{
		"parentId": parentID,
		"title":    "Child A",
		"slug":     "child-a",
		"kind":     "page",
	}), "page")
	childB := nestedMap(callToolStructured(session, "wiki_create_page", map[string]any{
		"parentId": parentID,
		"title":    "Child B",
		"slug":     "child-b",
		"kind":     "page",
	}), "page")

	callToolStructured(session, "wiki_sort_pages", map[string]any{
		"parentId":   parentID,
		"orderedIds": []any{stringField(childB, "id"), stringField(childA, "id")},
	})
	httpSort := putHTTPJSON(router, "/api/pages/"+parentID+"/sort", map[string]any{
		"orderedIds": []string{stringField(childB, "id"), stringField(childA, "id")},
	}, http.StatusOK)
	mcpSort := callToolStructured(session, "wiki_sort_pages", map[string]any{
		"parentId":   parentID,
		"orderedIds": []any{stringField(childB, "id"), stringField(childA, "id")},
	})
	Expect(mcpSort).To(matchScopedSuccessPayload(httpSort, newFixtureToolMessageID("mcp.tools.wiki_sort_pages.success"), newFixtureMessageID("api.pages.sort.success")), "wiki_sort_pages")
	parentAfterSort := nestedMap(callToolStructured(session, "wiki_get_page", map[string]any{"id": parentID}), "page")
	Expect(parentAfterSort).To(matchChildOrder(tree.PageIDFromString(stringField(childB, "id")), tree.PageIDFromString(stringField(childA, "id"))), "sort_pages shared parent")
	mcpSortParent := nestedMap(callToolStructured(session, "wiki_create_page", map[string]any{
		"title": "MCP Sort Parent",
		"slug":  "mcp-sort-parent",
		"kind":  "section",
	}), "page")
	mcpSortA := nestedMap(callToolStructured(session, "wiki_create_page", map[string]any{
		"parentId": stringField(mcpSortParent, "id"),
		"title":    "MCP Sort A",
		"slug":     "mcp-sort-a",
		"kind":     "page",
	}), "page")
	mcpSortB := nestedMap(callToolStructured(session, "wiki_create_page", map[string]any{
		"parentId": stringField(mcpSortParent, "id"),
		"title":    "MCP Sort B",
		"slug":     "mcp-sort-b",
		"kind":     "page",
	}), "page")
	httpSortParent := postHTTPJSON(router, "/api/pages", map[string]any{
		"title": "HTTP Sort Parent",
		"slug":  "http-sort-parent",
		"kind":  "section",
	}, http.StatusCreated)
	httpSortA := postHTTPJSON(router, "/api/pages", map[string]any{
		"parentId": stringField(httpSortParent, "id"),
		"title":    "HTTP Sort A",
		"slug":     "http-sort-a",
		"kind":     "page",
	}, http.StatusCreated)
	httpSortB := postHTTPJSON(router, "/api/pages", map[string]any{
		"parentId": stringField(httpSortParent, "id"),
		"title":    "HTTP Sort B",
		"slug":     "http-sort-b",
		"kind":     "page",
	}, http.StatusCreated)
	callToolStructured(session, "wiki_sort_pages", map[string]any{
		"parentId":   stringField(mcpSortParent, "id"),
		"orderedIds": []any{stringField(mcpSortB, "id"), stringField(mcpSortA, "id")},
	})
	putHTTPJSON(router, "/api/pages/"+stringField(httpSortParent, "id")+"/sort", map[string]any{
		"orderedIds": []string{stringField(httpSortB, "id"), stringField(httpSortA, "id")},
	}, http.StatusOK)
	Expect(getHTTPPageByPath(router, "mcp-sort-parent")).To(matchChildOrder(tree.PageIDFromString(stringField(mcpSortB, "id")), tree.PageIDFromString(stringField(mcpSortA, "id"))), "MCP sort_pages parent")
	Expect(getHTTPPageByPath(router, "http-sort-parent")).To(matchChildOrder(tree.PageIDFromString(stringField(httpSortB, "id")), tree.PageIDFromString(stringField(httpSortA, "id"))), "HTTP sort_pages parent")
	recordHTTPMCPParity("wiki_sort_pages", "PUT /api/pages/:id/sort")
	parentByAlias := nestedMap(callToolStructured(session, "wiki_get_page", map[string]any{"pageId": parentID}), "page")
	Expect(parentByAlias).To(HaveKeyWithValue("id", parentID))

	ensured := nestedMap(callToolStructured(session, "wiki_ensure_page", map[string]any{
		"path":  "parent-section/ensured",
		"title": "Ensured Page",
		"kind":  "page",
	}), "page")
	ensuredID := stringField(ensured, "id")
	httpEnsured := postHTTPJSON(router, "/api/pages/ensure", map[string]any{
		"path":  "parent-section/ensured",
		"title": "Ensured Page",
		"kind":  "page",
	}, http.StatusOK)
	Expect(ensured).To(matchJSONEqual(httpEnsured), "wiki_ensure_page")
	mcpEnsuredIndependent := nestedMap(callToolStructured(session, "wiki_ensure_page", map[string]any{
		"path":  "parent-section/ensured-mcp",
		"title": "Ensured Independent",
		"kind":  "section",
	}), "page")
	httpEnsuredIndependent := postHTTPJSON(router, "/api/pages/ensure", map[string]any{
		"path":  "parent-section/ensured-http",
		"title": "Ensured Independent",
		"kind":  "section",
	}, http.StatusOK)
	Expect(getHTTPPageByPath(router, "parent-section/ensured-mcp")).To(matchPageState(tree.PageIDFromString(stringField(mcpEnsuredIndependent, "id")), "Ensured Independent", newFixtureSlug("ensured-mcp"), "parent-section/ensured-mcp", "section", newFixturePageID("")), "MCP ensure_page independent")
	Expect(getHTTPPageByPath(router, "parent-section/ensured-http")).To(matchPageState(tree.PageIDFromString(stringField(httpEnsuredIndependent, "id")), "Ensured Independent", newFixtureSlug("ensured-http"), "parent-section/ensured-http", "section", newFixturePageID("")), "HTTP ensure_page independent")
	nullKindEnsured := nestedMap(callToolStructured(session, "wiki_ensure_page", map[string]any{
		"path":  "parent-section/ensured-null-kind",
		"title": "Ensured Null Kind",
		"kind":  nil,
	}), "page")
	Expect(nullKindEnsured).To(HaveKeyWithValue("kind", "page"))

	mcpPageBase := postHTTPJSON(router, "/api/pages", map[string]any{
		"parentId": parentID,
		"title":    "MCP Section Twin Base",
		"slug":     "mcp-section-twin",
		"kind":     "page",
	}, http.StatusCreated)
	mcpEnsuredSectionTwin := nestedMap(callToolStructured(session, "wiki_ensure_page", map[string]any{
		"path":  "parent-section/mcp-section-twin",
		"title": "MCP Ensured Section Twin",
		"kind":  "section",
	}), "page")
	Expect(stringField(mcpEnsuredSectionTwin, "id")).NotTo(Equal(stringField(mcpPageBase, "id")))
	Expect(mcpEnsuredSectionTwin).To(HaveKeyWithValue("kind", "section"))

	httpPageBase := postHTTPJSON(router, "/api/pages", map[string]any{
		"parentId": parentID,
		"title":    "HTTP Section Twin Base",
		"slug":     "http-section-twin",
		"kind":     "page",
	}, http.StatusCreated)
	httpEnsuredSectionTwin := postHTTPJSON(router, "/api/pages/ensure", map[string]any{
		"path":  "parent-section/http-section-twin",
		"title": "HTTP Ensured Section Twin",
		"kind":  "section",
	}, http.StatusOK)
	Expect(stringField(httpEnsuredSectionTwin, "id")).NotTo(Equal(stringField(httpPageBase, "id")))
	Expect(httpEnsuredSectionTwin).To(HaveKeyWithValue("kind", "section"))
	recordHTTPMCPParity("wiki_ensure_page", "POST /api/pages/ensure")

	lookup := callToolStructured(session, "wiki_lookup_path", map[string]any{"path": "parent-section/ensured"})
	httpLookup := getHTTPMap(router, "/api/pages/lookup?path=parent-section%2Fensured")
	Expect(lookup).To(HaveKeyWithValue("lookup", matchJSONEqual(httpLookup)), "wiki_lookup_path")
	recordHTTPMCPParity("wiki_lookup_path", "GET /api/pages/lookup")
	Expect(lookup).To(HaveKey("lookup"))

	pageTwin := postHTTPJSON(router, "/api/pages", map[string]any{
		"parentId": parentID,
		"title":    "Lookup Page Twin",
		"slug":     "lookup-twin",
		"kind":     "page",
	}, http.StatusCreated)
	sectionTwin := postHTTPJSON(router, "/api/pages", map[string]any{
		"parentId": parentID,
		"title":    "Lookup Section Twin",
		"slug":     "lookup-twin",
		"kind":     "section",
	}, http.StatusCreated)
	mcpLookupPageTwin := nestedMap(callToolStructured(session, "wiki_lookup_path", map[string]any{
		"path": "parent-section/lookup-twin",
		"kind": "page",
	}), "lookup")
	httpLookupPageTwin := getHTTPMap(router, "/api/pages/lookup?path=parent-section%2Flookup-twin&kind=page")
	Expect(mcpLookupPageTwin).To(matchJSONEqual(httpLookupPageTwin), "wiki_lookup_path page twin")
	Expect(mcpLookupPageTwin).To(matchLookupFinalID(stringField(pageTwin, "id"), "page"), "wiki_lookup_path page twin")

	mcpLookupSectionTwin := nestedMap(callToolStructured(session, "wiki_lookup_path", map[string]any{
		"path": "parent-section/lookup-twin",
		"kind": "section",
	}), "lookup")
	httpLookupSectionTwin := getHTTPMap(router, "/api/pages/lookup?path=parent-section%2Flookup-twin&kind=section")
	Expect(mcpLookupSectionTwin).To(matchJSONEqual(httpLookupSectionTwin), "wiki_lookup_path section twin")
	Expect(mcpLookupSectionTwin).To(matchLookupFinalID(stringField(sectionTwin, "id"), "section"), "wiki_lookup_path section twin")

	invalidLookupKindErr := callToolStructuredError(session, "wiki_lookup_path", map[string]any{
		"path": "parent-section/lookup-twin",
		"kind": "folder",
	})
	Expect(invalidLookupKindErr).To(matchMCPPageError(wikipages.ErrCodePageInvalidKind, sharederrors.MessageIDForCode(wikipages.ErrCodePageInvalidKind)), mcpLabelLookupPathInvalidKind)
	invalidLookupKindHTTP := getHTTPStatus(router, "/api/pages/lookup?path=parent-section%2Flookup-twin&kind=folder", http.StatusBadRequest)
	Expect(invalidLookupKindHTTP).To(matchHTTPPageError(wikipages.ErrCodePageInvalidKind, sharederrors.MessageIDForCode(wikipages.ErrCodePageInvalidKind)))

	permalink := callToolStructured(session, "wiki_resolve_permalink", map[string]any{"id": ensuredID})
	httpPermalink := getHTTPMap(router, "/api/pages/permalink/"+ensuredID)
	Expect(permalink).To(HaveKeyWithValue("target", matchJSONEqual(httpPermalink)), "wiki_resolve_permalink")
	recordHTTPMCPParity("wiki_resolve_permalink", "GET /api/pages/permalink/:id")
	target := nestedMap(permalink, "target")
	Expect(target).To(HaveKeyWithValue("path", "parent-section/ensured"))
	permalinkByAlias := callToolStructured(session, "wiki_resolve_permalink", map[string]any{"pageId": ensuredID})
	targetByAlias := nestedMap(permalinkByAlias, "target")
	Expect(targetByAlias).To(HaveKeyWithValue("path", "parent-section/ensured"))

	invalidEnsureKindErr := callToolStructuredError(session, "wiki_ensure_page", map[string]any{
		"path":  "parent-section/invalid-kind",
		"title": "Invalid Ensure Kind",
		"kind":  "folder",
	})
	Expect(invalidEnsureKindErr).To(matchMCPPageError(wikipages.ErrCodePageInvalidKind, sharederrors.MessageIDForCode(wikipages.ErrCodePageInvalidKind)), mcpLabelEnsurePageInvalidKind)
	invalidEnsureKindHTTP := postHTTPJSONBody(router, "/api/pages/ensure", map[string]any{
		"path":  "parent-section/invalid-kind-http",
		"title": "Invalid Ensure Kind HTTP",
		"kind":  "folder",
	}, http.StatusBadRequest)
	Expect(invalidEnsureKindHTTP).To(matchHTTPPageError(wikipages.ErrCodePageInvalidKind, sharederrors.MessageIDForCode(wikipages.ErrCodePageInvalidKind)))

	mcpMove := callToolStructured(session, "wiki_move_page", map[string]any{
		"id":       stringField(childA, "id"),
		"version":  stringField(childA, "version"),
		"parentId": "",
	})
	httpMove := putHTTPJSON(router, "/api/pages/"+stringField(childB, "id")+"/move", map[string]any{
		"version":  stringField(childB, "version"),
		"parentId": "",
	}, http.StatusOK)
	Expect(mcpMove).To(matchScopedSuccessPayload(httpMove, newFixtureToolMessageID("mcp.tools.wiki_move_page.success"), newFixtureMessageID("api.pages.move.success")), "wiki_move_page")
	httpMoved := getHTTPPageByPath(router, "child-a")
	Expect(httpMoved).To(matchPageState(tree.PageIDFromString(stringField(childA, "id")), "Child A", newFixtureSlug("child-a"), "child-a", "page", newFixturePageID("")), "MCP moved child A")
	httpMovedB := getHTTPPageByPath(router, "child-b")
	Expect(httpMovedB).To(matchPageState(tree.PageIDFromString(stringField(childB, "id")), "Child B", newFixtureSlug("child-b"), "child-b", "page", newFixturePageID("")), "HTTP moved child B")
	parentAfterMove := getHTTPPageByPath(router, "parent-section")
	Expect(parentAfterMove).To(matchChildrenExcludingIDs(tree.PageIDFromString(stringField(childA, "id")), tree.PageIDFromString(stringField(childB, "id"))), "parent after move")
	staleMoveErr := callToolStructuredError(session, "wiki_move_page", map[string]any{
		"id":       stringField(childA, "id"),
		"version":  stringField(childA, "version"),
		"parentId": parentID,
	})
	staleMoveHTTP := putHTTPJSONBody(router, "/api/pages/"+stringField(childA, "id")+"/move", map[string]any{
		"version":  stringField(childA, "version"),
		"parentId": parentID,
	}, http.StatusConflict)
	Expect(staleMoveErr).To(matchMCPPageVersionConflict(), "stale move_page MCP")
	Expect(staleMoveHTTP).To(matchHTTPPageVersionConflict(), "stale move_page HTTP")
	mcpWhitespaceMove := nestedMap(callToolStructured(session, "wiki_create_page", map[string]any{
		"title": "MCP Whitespace Move",
		"slug":  "mcp-whitespace-move",
		"kind":  "page",
	}), "page")
	whitespaceMoveErr := callToolStructuredError(session, "wiki_move_page", map[string]any{
		"id":       stringField(mcpWhitespaceMove, "id"),
		"version":  stringField(mcpWhitespaceMove, "version"),
		"parentId": " ",
	})
	Expect(whitespaceMoveErr).To(matchMCPPageError(wikipages.ErrCodePageInvalidParentID, sharederrors.MessageIDForCode(wikipages.ErrCodePageInvalidParentID)), mcpLabelMovePageWhitespaceParentID)
	httpWhitespaceMove := postHTTPJSON(router, "/api/pages", map[string]any{
		"title": "HTTP Whitespace Move",
		"slug":  "http-whitespace-move",
		"kind":  "page",
	}, http.StatusCreated)
	whitespaceMoveHTTP := putHTTPJSONBody(router, "/api/pages/"+stringField(httpWhitespaceMove, "id")+"/move", map[string]any{
		"version":  stringField(httpWhitespaceMove, "version"),
		"parentId": " ",
	}, http.StatusBadRequest)
	Expect(whitespaceMoveHTTP).To(matchHTTPPageError(wikipages.ErrCodePageInvalidParentID, sharederrors.MessageIDForCode(wikipages.ErrCodePageInvalidParentID)))
	mcpMissingParentMove := nestedMap(callToolStructured(session, "wiki_create_page", map[string]any{
		"parentId": parentID,
		"title":    "MCP Missing Parent Move",
		"slug":     "mcp-missing-parent-move",
		"kind":     "page",
	}), "page")
	missingParentMove := callToolStructured(session, "wiki_move_page", map[string]any{
		"id":      stringField(mcpMissingParentMove, "id"),
		"version": stringField(mcpMissingParentMove, "version"),
	})
	Expect(messageOutputFromStructuredContent(missingParentMove)).To(HaveField("MessageID", Equal(wikimcp.ToolMessageMovePageSuccess)))
	Expect(getHTTPPageByPath(router, "mcp-missing-parent-move")).To(matchPageState(tree.PageIDFromString(stringField(mcpMissingParentMove, "id")), "MCP Missing Parent Move", newFixtureSlug("mcp-missing-parent-move"), "mcp-missing-parent-move", "page", newFixturePageID("")), "MCP move_page missing parentId")
	recordHTTPMCPParity("wiki_move_page", "PUT /api/pages/:id/move")

	convertMe := nestedMap(callToolStructured(session, "wiki_create_page", map[string]any{
		"title": "Convert Me",
		"slug":  "convert-me",
		"kind":  "section",
	}), "page")
	mcpConvert := callToolStructured(session, "wiki_convert_page", map[string]any{
		"id":         stringField(convertMe, "id"),
		"version":    stringField(convertMe, "version"),
		"targetKind": "page",
	})
	Expect(messageOutputFromStructuredContent(mcpConvert)).To(HaveField("MessageID", Equal(wikimcp.ToolMessageConvertPageSuccess)))
	httpConverted := getHTTPPageByPath(router, "convert-me")
	Expect(httpConverted).To(HaveKeyWithValue("kind", "page"))
	Expect(httpConverted).To(matchPageState(tree.PageIDFromString(stringField(convertMe, "id")), "Convert Me", newFixtureSlug("convert-me"), "convert-me", "page", newFixturePageID("")), "MCP converted page")
	convertHTTP := nestedMap(callToolStructured(session, "wiki_create_page", map[string]any{
		"title": "Convert HTTP",
		"slug":  "convert-http",
		"kind":  "section",
	}), "page")
	postHTTPJSONNoContent(router, "/api/pages/convert/"+stringField(convertHTTP, "id"), map[string]any{
		"version":    stringField(convertHTTP, "version"),
		"targetKind": "page",
	}, http.StatusNoContent)
	httpConvertedPeer := getHTTPPageByPath(router, "convert-http")
	Expect(httpConvertedPeer).To(matchPageState(tree.PageIDFromString(stringField(convertHTTP, "id")), "Convert HTTP", newFixtureSlug("convert-http"), "convert-http", "page", newFixturePageID("")), "HTTP converted page")
	staleConvertErr := callToolStructuredError(session, "wiki_convert_page", map[string]any{
		"id":         stringField(convertMe, "id"),
		"version":    stringField(convertMe, "version"),
		"targetKind": "section",
	})
	staleConvertHTTP := postHTTPJSONBody(router, "/api/pages/convert/"+stringField(convertMe, "id"), map[string]any{
		"version":    stringField(convertMe, "version"),
		"targetKind": "section",
	}, http.StatusConflict)
	Expect(staleConvertErr).To(matchMCPPageVersionConflict(), "stale convert_page MCP")
	Expect(staleConvertHTTP).To(matchHTTPPageVersionConflict(), "stale convert_page HTTP")
	invalidConvertErr := callToolStructuredError(session, "wiki_convert_page", map[string]any{
		"id":         stringField(convertMe, "id"),
		"version":    stringField(httpConverted, "version"),
		"targetKind": "folder",
	})
	Expect(invalidConvertErr).To(matchMCPPageError(wikipages.ErrCodePageInvalidTargetKind, sharederrors.MessageIDForCode(wikipages.ErrCodePageInvalidTargetKind)), mcpLabelConvertPageInvalidTargetKind)
	paddedConvertErr := callToolStructuredError(session, "wiki_convert_page", map[string]any{
		"id":         stringField(convertMe, "id"),
		"version":    stringField(httpConverted, "version"),
		"targetKind": " page ",
	})
	Expect(paddedConvertErr).To(matchMCPPageError(wikipages.ErrCodePageInvalidTargetKind, sharederrors.MessageIDForCode(wikipages.ErrCodePageInvalidTargetKind)), mcpLabelConvertPagePaddedTargetKind)
	invalidConvertHTTP := postHTTPJSONBody(router, "/api/pages/convert/"+stringField(convertMe, "id"), map[string]any{
		"version":    stringField(httpConverted, "version"),
		"targetKind": "folder",
	}, http.StatusBadRequest)
	Expect(invalidConvertHTTP).To(matchHTTPPageError(wikipages.ErrCodePageInvalidTargetKind, sharederrors.MessageIDForCode(wikipages.ErrCodePageInvalidTargetKind)), mcpLabelConvertPageInvalidTargetKindHTTP)
	paddedConvertHTTP := postHTTPJSONBody(router, "/api/pages/convert/"+stringField(convertMe, "id"), map[string]any{
		"version":    stringField(httpConverted, "version"),
		"targetKind": " page ",
	}, http.StatusBadRequest)
	Expect(paddedConvertHTTP).To(matchHTTPPageError(wikipages.ErrCodePageInvalidTargetKind, sharederrors.MessageIDForCode(wikipages.ErrCodePageInvalidTargetKind)), mcpLabelConvertPagePaddedTargetKindHTTP)
	recordHTTPMCPParity("wiki_convert_page", "POST /api/pages/convert/:id")

	copied := nestedMap(callToolStructured(session, "wiki_copy_page", map[string]any{
		"id":    stringField(childA, "id"),
		"title": "Child A Copy",
		"slug":  "child-a-copy",
	}), "page")
	httpCopied := postHTTPJSON(router, "/api/pages/copy/"+stringField(childA, "id"), map[string]any{
		"title": "Child A Copy",
		"slug":  "child-a-http-copy",
	}, http.StatusCreated)
	whitespaceCopyErr := callToolStructuredError(session, "wiki_copy_page", map[string]any{
		"id":             stringField(childA, "id"),
		"targetParentId": " ",
		"title":          "Whitespace Copy",
		"slug":           "whitespace-copy",
	})
	Expect(whitespaceCopyErr).To(matchMCPPageError(wikipages.ErrCodePageInvalidParentID, sharederrors.MessageIDForCode(wikipages.ErrCodePageInvalidParentID)), mcpLabelCopyPageWhitespaceTargetParentID)
	whitespaceCopyHTTP := postHTTPJSONBody(router, "/api/pages/copy/"+stringField(childB, "id"), map[string]any{
		"targetParentId": " ",
		"title":          "Whitespace Copy HTTP",
		"slug":           "whitespace-copy-http",
	}, http.StatusBadRequest)
	Expect(whitespaceCopyHTTP).To(matchHTTPPageError(wikipages.ErrCodePageInvalidParentID, sharederrors.MessageIDForCode(wikipages.ErrCodePageInvalidParentID)))
	paddedCopyErr := callToolStructuredError(session, "wiki_copy_page", map[string]any{
		"id":             stringField(childA, "id"),
		"targetParentId": " " + parentID + " ",
		"title":          "Padded Copy",
		"slug":           "padded-copy",
	})
	Expect(paddedCopyErr).To(matchMCPPageError(wikipages.ErrCodePageInvalidParentID, sharederrors.MessageIDForCode(wikipages.ErrCodePageInvalidParentID)), mcpLabelCopyPagePaddedTargetParentID)
	paddedCopyHTTP := postHTTPJSONBody(router, "/api/pages/copy/"+stringField(childB, "id"), map[string]any{
		"targetParentId": " " + parentID + " ",
		"title":          "Padded Copy HTTP",
		"slug":           "padded-copy-http",
	}, http.StatusBadRequest)
	Expect(paddedCopyHTTP).To(matchHTTPPageError(wikipages.ErrCodePageInvalidParentID, sharederrors.MessageIDForCode(wikipages.ErrCodePageInvalidParentID)))
	Expect(copied).To(matchMapFields(httpCopied, []string{"title", "kind", "content"}), "wiki_copy_page")
	Expect(getHTTPPageByPath(router, "child-a-copy")).To(matchPageState(tree.PageIDFromString(stringField(copied, "id")), "Child A Copy", newFixtureSlug("child-a-copy"), "child-a-copy", "page", newFixturePageID("")), "MCP copied page")
	Expect(getHTTPPageByPath(router, "child-a-http-copy")).To(matchPageState(tree.PageIDFromString(stringField(httpCopied, "id")), "Child A Copy", newFixtureSlug("child-a-http-copy"), "child-a-http-copy", "page", newFixturePageID("")), "HTTP copied page")
	Expect(getHTTPPageByPath(router, "child-a")).To(matchPageState(tree.PageIDFromString(stringField(childA, "id")), "Child A", newFixtureSlug("child-a"), "child-a", "page", newFixturePageID("")), "copy_page source preserved")
	recordHTTPMCPParity("wiki_copy_page", "POST /api/pages/copy/:id")

	missingDeleteHTTP := deleteHTTPStatus(router, "/api/pages/"+stringField(copied, "id"), http.StatusBadRequest)
	Expect(missingDeleteHTTP).To(matchHTTPPageError(wikipages.ErrCodePageVersionRequired, sharederrors.MessageIDForCode(wikipages.ErrCodePageVersionRequired)))

	staleDelete := nestedMap(callToolStructured(session, "wiki_update_page", map[string]any{
		"id":      stringField(copied, "id"),
		"version": stringField(copied, "version"),
		"title":   "Child A Copy Updated",
		"slug":    "child-a-copy",
		"content": "updated",
	}), "page")
	staleDeleteErr := callToolStructuredError(session, "wiki_delete_page", map[string]any{
		"id":      stringField(copied, "id"),
		"version": stringField(copied, "version"),
	})
	staleDeleteHTTP := deleteHTTPStatus(router, "/api/pages/"+stringField(copied, "id")+"?version="+url.QueryEscape(stringField(copied, "version")), http.StatusConflict)
	Expect(staleDeleteErr).To(matchMCPPageVersionConflict(), "stale delete_page MCP")
	Expect(staleDeleteHTTP).To(matchHTTPPageVersionConflict(), "stale delete_page HTTP")

	mcpDeletedPage := callToolStructured(session, "wiki_delete_page", map[string]any{
		"id":      stringField(copied, "id"),
		"version": stringField(staleDelete, "version"),
	})
	httpDeletedPageBody := deleteHTTPStatus(router, "/api/pages/"+stringField(httpCopied, "id")+"?version="+url.QueryEscape(stringField(httpCopied, "version")), http.StatusOK)
	Expect(mcpDeletedPage).To(matchScopedSuccessPayload(decodeJSONMap("HTTP delete_page", []byte(httpDeletedPageBody)), newFixtureToolMessageID("mcp.tools.wiki_delete_page.success"), newFixtureMessageID("api.pages.delete.success")), "wiki_delete_page")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/pages/by-path?path=child-a-copy", nil))
	Expect(rec).To(HaveHTTPStatus(http.StatusNotFound))
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/pages/by-path?path=child-a-http-copy", nil))
	Expect(rec).To(HaveHTTPStatus(http.StatusNotFound))
	recordHTTPMCPParity("wiki_delete_page", "DELETE /api/pages/:id")
}
