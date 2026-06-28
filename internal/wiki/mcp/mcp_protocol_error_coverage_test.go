package mcp_test

import (
	"encoding/base64"

	. "github.com/onsi/ginkgo/v2"
	coreassets "github.com/perber/wiki/internal/core/assets"
	httpinternal "github.com/perber/wiki/internal/http"
)

var _ = It("LocalMCPProtocol_InvalidInputsExerciseToolErrorBranches", func() {
	t := GinkgoTB()
	w, _ := newLocalMCPTestWikiWithStorage(t)
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
	session := connectLocalMCP(t, router, "/mcp")

	page := nestedMap(t, callToolStructured(t, session, "wiki_create_page", map[string]any{
		"title": "Coverage Target",
		"slug":  "coverage-target",
		"kind":  "page",
	}), "page")
	pageID := stringField(t, page, "id")
	pageVersion := stringField(t, page, "version")

	for _, tc := range []struct {
		name string
		args map[string]any
	}{
		{name: "wiki_get_page", args: map[string]any{}},
		{name: "wiki_get_page", args: map[string]any{"pageId": "missing-page"}},
		{name: "wiki_get_page_by_path", args: map[string]any{"path": "../bad"}},
		{name: "wiki_lookup_path", args: map[string]any{"path": "coverage-target", "kind": "bad-kind"}},
		{name: "wiki_lookup_path", args: map[string]any{"path": "../bad"}},
		{name: "wiki_resolve_permalink", args: map[string]any{}},
		{name: "wiki_resolve_permalink", args: map[string]any{"pageId": "missing-page"}},
		{name: "wiki_suggest_slug", args: map[string]any{"title": ""}},
		{name: "wiki_suggest_slug", args: map[string]any{"title": "Child", "parentId": "missing-page"}},
		{name: "wiki_create_page", args: map[string]any{"title": "Bad", "slug": "bad", "kind": "bad-kind"}},
		{name: "wiki_create_page", args: map[string]any{"title": "Bad", "slug": "bad", "kind": "page", "parentId": "missing-page"}},
		{name: "wiki_update_page", args: map[string]any{"id": "missing-page", "version": "missing-version", "title": "Missing", "slug": "missing", "content": "body"}},
		{name: "wiki_update_page", args: map[string]any{"id": pageID, "version": "stale-version", "title": "Coverage Target", "slug": "coverage-target"}},
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
		{name: "wiki_get_subtree", args: map[string]any{"pageId": pageID, "path": "coverage-target"}},
		{name: "wiki_get_subtree", args: map[string]any{"depth": -1}},
		{name: "wiki_get_subtree", args: map[string]any{"pageId": "missing-page"}},
		{name: "wiki_search_pages", args: map[string]any{}},
		{name: "wiki_get_pages_by_tags", args: map[string]any{}},
		{name: "wiki_get_pages_by_property", args: map[string]any{"key": "", "value": "draft"}},
		{name: "wiki_get_pages_by_property", args: map[string]any{"key": "status", "value": ""}},
		{name: "wiki_update_page_metadata", args: map[string]any{}},
		{name: "wiki_update_page_metadata", args: map[string]any{"pageId": pageID}},
		{name: "wiki_replace_page_section", args: map[string]any{}},
		{name: "wiki_replace_page_section", args: map[string]any{"pageId": pageID, "version": pageVersion, "headingPath": []string{"Missing"}, "content": "replacement"}},
		{name: "wiki_validate_page", args: map[string]any{}},
		{name: "wiki_validate_content", args: map[string]any{"path": "../bad", "content": "body"}},
		{name: "wiki_refresh", args: map[string]any{"source": "bad-source"}},
		{name: "wiki_list_revisions", args: map[string]any{}},
		{name: "wiki_list_revisions", args: map[string]any{"pageId": "missing-page"}},
		{name: "wiki_get_latest_revision", args: map[string]any{}},
		{name: "wiki_restore_revision", args: map[string]any{}},
		{name: "wiki_preview_page_refactor", args: map[string]any{}},
		{name: "wiki_preview_page_refactor", args: map[string]any{"pageId": pageID, "kind": "bad-kind"}},
		{name: "wiki_apply_page_refactor", args: map[string]any{}},
		{name: "wiki_apply_page_refactor", args: map[string]any{"pageId": pageID, "version": pageVersion, "kind": "bad-kind"}},
	} {
		result := callToolErrorResult(t, session, tc.name, tc.args)
		if result.Text == "" {
			t.Fatalf("%s returned empty tool error for args %#v", tc.name, tc.args)
		}
	}

	if err := w.Close(); err != nil {
		t.Fatalf("Close wiki before backend-error probe failed: %v", err)
	}
	for _, tc := range []struct {
		name string
		args map[string]any
	}{
		{name: "wiki_search_pages", args: map[string]any{"query": "coverage"}},
		{name: "wiki_list_tags", args: map[string]any{"query": "coverage"}},
		{name: "wiki_list_property_keys", args: map[string]any{"query": "coverage"}},
	} {
		result := callToolErrorResult(t, session, tc.name, tc.args)
		if result.Text == "" {
			t.Fatalf("%s returned empty tool error after backend close for args %#v", tc.name, tc.args)
		}
	}
})
