package mcp_test

import (
	"encoding/base64"
	"net/http"
	"os"
	"path/filepath"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/perber/wiki/internal/core/assets"
	sharederrors "github.com/perber/wiki/internal/core/shared/errors"
	"github.com/perber/wiki/internal/core/tree"
	httpinternal "github.com/perber/wiki/internal/http"
	testmatchers "github.com/perber/wiki/internal/test_utils/matchers"
	wikiassets "github.com/perber/wiki/internal/wiki/assets"
	wikimcp "github.com/perber/wiki/internal/wiki/mcp"
	wikisearch "github.com/perber/wiki/internal/wiki/search"
	wikitags "github.com/perber/wiki/internal/wiki/tags"
)

// Canonical Markdown links plan scenarios covered by tests in this file:
// - MCP agent context returns canonical examples

var _ = Describe("local MCP index and asset parity", Label("integration"), func() {
	It("keeps index and asset tools aligned with HTTP routes", func() {
		runLocalMCPProtocolIndexAndAssetParity()
	})
})

func runLocalMCPProtocolIndexAndAssetParity() {
	GinkgoHelper()

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

	searchErr := callToolStructuredError(session, "wiki_search_pages", map[string]any{})
	Expect(searchErr).To(matchMCPStructuredError(wikisearch.ErrCodeSearchMissingQuery, sharederrors.MessageIDForCode(wikisearch.ErrCodeSearchMissingQuery)))
	blankTagSearchErr := callToolStructuredError(session, "wiki_search_pages", map[string]any{"tags": []any{" "}})
	Expect(blankTagSearchErr).To(matchMCPStructuredError(wikisearch.ErrCodeSearchMissingQuery, sharederrors.MessageIDForCode(wikisearch.ErrCodeSearchMissingQuery)))

	target := nestedMap(callToolStructured(session, "wiki_create_page", map[string]any{
		"title": "Target",
		"slug":  "target",
		"kind":  "page",
	}), "page")
	source := nestedMap(callToolStructured(session, "wiki_create_page", map[string]any{
		"title": "Source",
		"slug":  "source",
		"kind":  "page",
	}), "page")
	sourceID := stringField(source, "id")
	content := "Tagged source with [Target](/target.md) and [Missing](/missing-target)"
	callToolStructured(session, "wiki_update_page", map[string]any{
		"id":      sourceID,
		"version": stringField(source, "version"),
		"title":   "Source",
		"slug":    "source",
		"content": content,
		"tags":    []any{"mcp", "assets"},
		"properties": map[string]any{
			"status": "draft",
		},
	})

	status := callToolStructured(session, "wiki_get_search_status", nil)
	httpStatus := getHTTPValue(router, "/api/search/status")
	Expect(status).To(HaveKeyWithValue("status", matchJSONEqual(httpStatus)), "wiki_get_search_status")
	recordHTTPMCPParity("wiki_get_search_status", "GET /api/search/status")
	Expect(status).To(HaveKey("status"))

	tags := callToolStructured(session, "wiki_list_tags", map[string]any{"q": "mc", "limit": float64(10)})
	httpTags := getHTTPValue(router, "/api/tags?q=mc&limit=10")
	Expect(tags).To(HaveKeyWithValue("tags", matchJSONEqual(httpTags)), "wiki_list_tags")
	recordHTTPMCPParity("wiki_list_tags", "GET /api/tags")
	Expect(arrayFieldFromMap(tags, "tags")).To(ContainElement(HaveKeyWithValue("tag", "mcp")))
	tagPages := callToolStructured(session, "wiki_get_pages_by_tags", map[string]any{"tags": []any{"mcp"}})
	httpTagPages := getHTTPValue(router, "/api/tags/pages?tags=mcp")
	Expect(tagPages).To(HaveKeyWithValue("pages", matchJSONEqual(httpTagPages)), "wiki_get_pages_by_tags")
	recordHTTPMCPParity("wiki_get_pages_by_tags", "GET /api/tags/pages")
	Expect(arrayFieldFromMap(tagPages, "pages")).To(ContainElement(HaveKeyWithValue("id", sourceID)))
	blankTagsErr := callToolStructuredError(session, "wiki_get_pages_by_tags", map[string]any{"tags": []any{" "}})
	Expect(blankTagsErr).To(matchMCPStructuredError(wikitags.ErrCodeTagsMissingParam, sharederrors.MessageIDForCode(wikitags.ErrCodeTagsMissingParam)), mcpLabelGetPagesByTagsBlankMCP)
	blankTagsHTTP := getHTTPStatus(router, "/api/tags/pages?tags=+", http.StatusBadRequest)
	Expect(blankTagsHTTP).To(matchHTTPPageError(wikitags.ErrCodeTagsMissingParam, sharederrors.MessageIDForCode(wikitags.ErrCodeTagsMissingParam)), mcpLabelGetPagesByTagsBlankHTTP)

	keys := callToolStructured(session, "wiki_list_property_keys", map[string]any{"q": "sta", "limit": float64(10)})
	httpKeys := getHTTPValue(router, "/api/properties?q=sta&limit=10")
	Expect(keys).To(HaveKeyWithValue("keys", matchJSONEqual(httpKeys)), "wiki_list_property_keys")
	recordHTTPMCPParity("wiki_list_property_keys", "GET /api/properties")
	Expect(arrayFieldFromMap(keys, "keys")).To(ContainElement(HaveKeyWithValue("key", "status")))
	propertyPages := callToolStructured(session, "wiki_get_pages_by_property", map[string]any{"key": "status", "value": "draft"})
	httpPropertyPages := getHTTPValue(router, "/api/properties/pages?key=status&value=draft")
	Expect(propertyPages).To(HaveKeyWithValue("pages", matchJSONEqual(httpPropertyPages)), "wiki_get_pages_by_property")
	recordHTTPMCPParity("wiki_get_pages_by_property", "GET /api/properties/pages")
	Expect(arrayFieldFromMap(propertyPages, "pages")).To(ContainElement(HaveKeyWithValue("id", sourceID)))

	links := callToolStructured(session, "wiki_get_link_status", map[string]any{"pageId": sourceID})
	linkStatus := nestedMap(links, "status")
	httpLinkStatus := getHTTPMap(router, "/api/pages/"+sourceID+"/links")
	Expect(linkStatus).To(matchJSONEqual(httpLinkStatus), "wiki_get_link_status")
	recordHTTPMCPParity("wiki_get_link_status", "GET /api/pages/:id/links")
	counts := nestedMap(linkStatus, "counts")
	Expect(counts).To(SatisfyAll(
		HaveKeyWithValue("outgoings", float64(1)),
		HaveKeyWithValue("broken_outgoings", float64(1)),
	))

	targetID := stringField(target, "id")
	targetStandaloneStatus := nestedMap(callToolStructured(session, "wiki_get_link_status", map[string]any{"pageId": targetID}), "status")
	targetFromGet := callToolStructured(session, "wiki_get_page", map[string]any{"id": targetID})
	Expect(nestedMap(targetFromGet, "linkStatus")).To(matchJSONEqual(targetStandaloneStatus), "wiki_get_page linkStatus")
	Expect(arrayFieldFromMap(nestedMap(targetFromGet, "linkStatus"), "backlinks")).To(ContainElement(HaveKeyWithValue("from_page_id", sourceID)))

	targetFromPath := callToolStructured(session, "wiki_get_page_by_path", map[string]any{"path": "target"})
	Expect(nestedMap(targetFromPath, "linkStatus")).To(matchJSONEqual(targetStandaloneStatus), "get_page_by_path linkStatus")

	sourceFromGet := callToolStructured(session, "wiki_get_page", map[string]any{"id": sourceID})
	Expect(nestedMap(sourceFromGet, "linkStatus")).To(matchJSONEqual(linkStatus), "wiki_get_page source linkStatus")

	sourceFromPath := callToolStructured(session, "wiki_get_page_by_path", map[string]any{"path": "source"})
	Expect(nestedMap(sourceFromPath, "linkStatus")).To(matchJSONEqual(linkStatus), "get_page_by_path source linkStatus")

	assetContent := []byte("asset content")
	cssContent := []byte("body { color: rebeccapurple; }\n")
	httpAssetContent := []byte("asset content from http")
	uploaded := callToolStructured(session, "wiki_upload_asset", map[string]any{
		"pageId":        sourceID,
		"filename":      "note.txt",
		"contentBase64": base64.StdEncoding.EncodeToString(assetContent),
	})
	Expect(uploaded).To(HaveKeyWithValue("file", "/assets/"+sourceID+"/note.txt"))
	sourcePageID := tree.PageIDFromString(sourceID)
	httpUploaded := uploadHTTPAsset(router, sourcePageID, newFixtureAssetName("http-note.txt"), httpAssetContent, http.StatusCreated)
	Expect(uploaded).To(matchAssetURLResult("file", sourcePageID), "wiki_upload_asset")
	Expect(httpUploaded).To(matchAssetURLResult("file", sourcePageID), "HTTP upload asset")
	asset := callToolStructured(session, "wiki_get_asset", map[string]any{"pageId": sourceID, "filename": "note.txt"})
	Expect(asset).To(SatisfyAll(
		HaveKeyWithValue("filename", "note.txt"),
		HaveKeyWithValue("contentBase64", base64.StdEncoding.EncodeToString(assetContent)),
	))
	httpNoteBody, httpNoteContentType := getHTTPAssetWithContentType(router, sourcePageID, newFixtureAssetName("note.txt"))
	Expect(httpNoteBody).To(Equal(string(assetContent)))
	Expect(httpNoteContentType).To(HavePrefix(asset["mimeType"].(string)))
	httpAsset := callToolStructured(session, "wiki_get_asset", map[string]any{"pageId": sourceID, "filename": "http-note.txt"})
	Expect(httpAsset).To(HaveKeyWithValue("contentBase64", base64.StdEncoding.EncodeToString(httpAssetContent)))
	httpAssetBody, httpAssetContentType := getHTTPAssetWithContentType(router, sourcePageID, newFixtureAssetName("http-note.txt"))
	Expect(httpAssetBody).To(Equal(string(httpAssetContent)))
	Expect(httpAssetContentType).To(HavePrefix(httpAsset["mimeType"].(string)))
	listed := callToolStructured(session, "wiki_list_assets", map[string]any{"pageId": sourceID})
	Expect(stringSliceField(listed, "files")).To(ContainElement("/assets/" + sourceID + "/note.txt"))
	httpListed := getHTTPAssets(router, sourcePageID)
	Expect(stringSliceField(httpListed, "files")).To(ContainElement("/assets/" + sourceID + "/note.txt"))
	Expect(listed).To(matchJSONEqual(httpListed), "wiki_list_assets")
	recordHTTPMCPParity("wiki_upload_asset", "POST /api/pages/:id/assets")
	recordHTTPMCPParity("wiki_list_assets", "GET /api/pages/:id/assets")
	recordHTTPMCPParity("wiki_get_asset", "GET /assets/:pageId/:filename")
	callToolStructured(session, "wiki_upload_asset", map[string]any{
		"pageId":        sourceID,
		"filename":      "style.css",
		"contentBase64": base64.StdEncoding.EncodeToString(cssContent),
	})
	cssAsset := callToolStructured(session, "wiki_get_asset", map[string]any{"pageId": sourceID, "filename": "style.css"})
	httpCSSBody, httpCSSContentType := getHTTPAssetWithContentType(router, sourcePageID, newFixtureAssetName("style.css"))
	Expect(httpCSSBody).To(Equal(string(cssContent)))
	Expect(httpCSSContentType).To(HavePrefix(cssAsset["mimeType"].(string)))

	renamed := callToolStructured(session, "wiki_rename_asset", map[string]any{
		"pageId":      sourceID,
		"oldFilename": "note.txt",
		"newFilename": "renamed.txt",
	})
	Expect(renamed).To(HaveKeyWithValue("url", "/assets/"+sourceID+"/renamed.txt"))
	httpRenamed := putHTTPJSON(router, "/api/pages/"+sourceID+"/assets/rename", map[string]any{
		"old_filename": "http-note.txt",
		"new_filename": "http-renamed.txt",
	}, http.StatusOK)
	Expect(renamed).To(matchAssetURLResult("url", sourcePageID), "wiki_rename_asset")
	Expect(httpRenamed).To(matchAssetURLResult("url", sourcePageID), "HTTP rename_asset")
	renamedAsset := callToolStructured(session, "wiki_get_asset", map[string]any{"pageId": sourceID, "filename": "renamed.txt"})
	Expect(renamedAsset).To(HaveKeyWithValue("contentBase64", base64.StdEncoding.EncodeToString(assetContent)))
	httpRenamedBody, httpRenamedContentType := getHTTPAssetWithContentType(router, sourcePageID, newFixtureAssetName("renamed.txt"))
	Expect(httpRenamedBody).To(Equal(string(assetContent)))
	Expect(httpRenamedContentType).To(HavePrefix(renamedAsset["mimeType"].(string)))
	Expect(callToolStructuredError(session, wikimcp.ToolGetAsset, map[string]any{"pageId": sourceID, "filename": "note.txt"})).To(matchMCPStructuredError(wikiassets.ErrCodeAssetNotFound, sharederrors.MessageIDForCode(wikiassets.ErrCodeAssetNotFound)))
	getHTTPStatus(router, "/assets/"+sourceID+"/note.txt", http.StatusNotFound)
	httpRenamedAsset := callToolStructured(session, "wiki_get_asset", map[string]any{"pageId": sourceID, "filename": "http-renamed.txt"})
	Expect(httpRenamedAsset).To(HaveKeyWithValue("contentBase64", base64.StdEncoding.EncodeToString(httpAssetContent)))
	Expect(getHTTPAsset(router, sourcePageID, newFixtureAssetName("http-renamed.txt"))).To(Equal(string(httpAssetContent)))
	Expect(callToolStructuredError(session, wikimcp.ToolGetAsset, map[string]any{"pageId": sourceID, "filename": "http-note.txt"})).To(matchMCPStructuredError(wikiassets.ErrCodeAssetNotFound, sharederrors.MessageIDForCode(wikiassets.ErrCodeAssetNotFound)))
	getHTTPStatus(router, "/assets/"+sourceID+"/http-note.txt", http.StatusNotFound)
	httpListed = getHTTPAssets(router, sourcePageID)
	listed = callToolStructured(session, "wiki_list_assets", map[string]any{"pageId": sourceID})
	Expect(listed).To(matchJSONEqual(httpListed), "list_assets after rename")
	Expect(stringSliceField(httpListed, "files")).To(ContainElements("/assets/"+sourceID+"/renamed.txt", "/assets/"+sourceID+"/http-renamed.txt"))
	recordHTTPMCPParity("wiki_rename_asset", "PUT /api/pages/:id/assets/rename")
	mcpDeleted := callToolStructured(session, "wiki_delete_asset", map[string]any{"pageId": sourceID, "filename": "renamed.txt"})
	httpDeletedBody := deleteHTTPStatus(router, "/api/pages/"+sourceID+"/assets/http-renamed.txt", http.StatusOK)
	httpDeleted := decodeJSONMap("HTTP delete_asset", []byte(httpDeletedBody))
	Expect(mcpDeleted).To(matchScopedSuccessPayload(httpDeleted, newFixtureToolMessageID("mcp.tools.wiki_delete_asset.success"), newFixtureMessageID("api.assets.delete.success")), "wiki_delete_asset")
	Expect(callToolStructuredError(session, wikimcp.ToolGetAsset, map[string]any{"pageId": sourceID, "filename": "renamed.txt"})).To(matchMCPStructuredError(wikiassets.ErrCodeAssetNotFound, sharederrors.MessageIDForCode(wikiassets.ErrCodeAssetNotFound)))
	getHTTPStatus(router, "/assets/"+sourceID+"/renamed.txt", http.StatusNotFound)
	Expect(callToolStructuredError(session, wikimcp.ToolGetAsset, map[string]any{"pageId": sourceID, "filename": "http-renamed.txt"})).To(matchMCPStructuredError(wikiassets.ErrCodeAssetNotFound, sharederrors.MessageIDForCode(wikiassets.ErrCodeAssetNotFound)))
	getHTTPStatus(router, "/assets/"+sourceID+"/http-renamed.txt", http.StatusNotFound)
	listed = callToolStructured(session, "wiki_list_assets", map[string]any{"pageId": sourceID})
	Expect(stringSliceField(listed, "files")).NotTo(ContainElement("/assets/" + sourceID + "/renamed.txt"))
	httpListed = getHTTPAssets(router, sourcePageID)
	Expect(listed).To(matchJSONEqual(httpListed), "list_assets after delete")
	Expect(stringSliceField(httpListed, "files")).NotTo(ContainElements("/assets/"+sourceID+"/renamed.txt", "/assets/"+sourceID+"/http-renamed.txt"))
	recordHTTPMCPParity("wiki_delete_asset", "DELETE /api/pages/:id/assets/:name")
}

var _ = Describe("local MCP asset protocol errors", Label("integration"), func() {
	It("rejects oversized uploads before page lookup", func() {
		w := newLocalMCPTestWiki(false)
		router := newLocalMCPTestRouter(w, httpinternal.RouterOptions{
			AuthDisabled:            true,
			PublicAccess:            true,
			AllowInsecure:           true,
			MaxAssetUploadSizeBytes: 2,
			MCPEnabled:              true,
			MCPToolListPageSize:     200,
		})
		session := connectLocalMCP(router, "/mcp")

		errResult := callToolStructuredError(session, "wiki_upload_asset", map[string]any{
			"pageId":        "missing-page",
			"filename":      "too-large.txt",
			"contentBase64": base64.StdEncoding.EncodeToString([]byte("abc")),
		})
		Expect(errResult).To(testmatchers.HaveMCPStructuredError(wikiassets.ErrCodeAssetFileTooLarge, sharederrors.MessageIDForCode(wikiassets.ErrCodeAssetFileTooLarge)))
	})

	It("reports malformed base64 as an asset payload error", func() {
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

		page := nestedMap(callToolStructured(session, "wiki_create_page", map[string]any{
			"title": "Asset Payload",
			"slug":  "asset-payload",
			"kind":  "page",
		}), "page")

		errResult := callToolStructuredError(session, "wiki_upload_asset", map[string]any{
			"pageId":        stringField(page, "id"),
			"filename":      "bad.txt",
			"contentBase64": "not base64 %",
		})
		Expect(errResult).To(testmatchers.HaveMCPStructuredError(wikiassets.ErrCodeAssetInvalidPayload, sharederrors.MessageIDForCode(wikiassets.ErrCodeAssetInvalidPayload)))
	})

	It("validates the owning page before serving stored asset blobs", func() {
		w, storageDir := newLocalMCPTestWikiWithStorage()
		router := newLocalMCPTestRouter(w, httpinternal.RouterOptions{
			AuthDisabled:            true,
			PublicAccess:            true,
			AllowInsecure:           true,
			MaxAssetUploadSizeBytes: assets.DefaultMaxUploadSizeBytes,
			MCPEnabled:              true,
			MCPToolListPageSize:     200,
		})
		session := connectLocalMCP(router, "/mcp")

		orphanAssetDir := filepath.Join(storageDir, "assets", "missing-page")
		Expect(os.MkdirAll(orphanAssetDir, 0o755)).To(Succeed())
		Expect(os.WriteFile(filepath.Join(orphanAssetDir, "orphan.txt"), []byte("orphan"), 0o644)).To(Succeed())

		errResult := callToolStructuredError(session, "wiki_get_asset", map[string]any{"pageId": "missing-page", "filename": "orphan.txt"})
		Expect(errResult).To(testmatchers.HaveMCPStructuredError(wikiassets.ErrCodeAssetPageNotFound, sharederrors.MessageIDForCode(wikiassets.ErrCodeAssetPageNotFound)))
	})
})
