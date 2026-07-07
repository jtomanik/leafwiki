package mcp_test

import (
	"encoding/base64"
	"net/http"
	"net/url"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/perber/wiki/internal/core/assets"
	sharederrors "github.com/perber/wiki/internal/core/shared/errors"
	"github.com/perber/wiki/internal/core/tree"
	httpinternal "github.com/perber/wiki/internal/http"
	testmatchers "github.com/perber/wiki/internal/test_utils/matchers"
	wikipages "github.com/perber/wiki/internal/wiki/pages"
	wikirevisions "github.com/perber/wiki/internal/wiki/revisions"
)

// Canonical Markdown links plan scenarios covered by tests in this file:
// - MCP agent context returns canonical examples

var _ = Describe("local MCP feature-gated tool parity", Label("integration"), func() {
	It("keeps feature-gated tools aligned with HTTP routes", func() {
		runLocalMCPProtocolFeatureGatedToolParity()
	})
})

func runLocalMCPProtocolFeatureGatedToolParity() {
	GinkgoHelper()

	w, _ := newLocalMCPTestWikiWithStorage()
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
	session := connectLocalMCP(router, "/mcp")

	missingLatestErr := callToolStructuredError(session, "wiki_get_latest_revision", map[string]any{"pageId": "missing-page"})
	Expect(missingLatestErr).To(matchMCPStructuredError(wikipages.ErrCodePageNotFound, sharederrors.MessageIDForCode(wikipages.ErrCodePageNotFound)), mcpLabelGetLatestRevisionMissingPage)
	for _, tc := range []struct {
		name     string
		args     map[string]any
		wantCode sharederrors.ErrorCode
	}{
		{name: "wiki_get_revision", args: map[string]any{"pageId": "missing-page", "revisionId": "missing-revision"}, wantCode: wikipages.ErrCodePageNotFound},
		{name: "wiki_compare_revisions", args: map[string]any{"pageId": "missing-page", "baseRevisionId": "base-revision", "targetRevisionId": "target-revision"}, wantCode: wikipages.ErrCodePageNotFound},
		{name: "wiki_get_revision_asset", args: map[string]any{"pageId": "missing-page", "revisionId": "missing-revision", "assetName": "missing.txt"}, wantCode: wikirevisions.ErrCodeRevisionNotFound},
	} {
		errResult := callToolStructuredError(session, tc.name, tc.args)
		Expect(errResult).To(matchMCPStructuredError(tc.wantCode, sharederrors.MessageIDForCode(tc.wantCode)), mcpToolCaseLabel(tc.name, mcpLabelRevisionMissingSuffix))
	}
	for _, tc := range []struct {
		name     string
		args     map[string]any
		wantCode sharederrors.ErrorCode
	}{
		{name: "wiki_get_revision", args: map[string]any{"pageId": "missing-page", "revisionId": " "}, wantCode: wikirevisions.ErrCodeRevisionInvalidRevisionID},
		{name: "wiki_get_revision_asset", args: map[string]any{"pageId": "missing-page", "revisionId": " ", "assetName": "missing.txt"}, wantCode: wikirevisions.ErrCodeRevisionInvalidRevisionID},
		{name: "wiki_compare_revisions", args: map[string]any{"pageId": "missing-page", "baseRevisionId": " ", "targetRevisionId": "target-revision"}, wantCode: wikirevisions.ErrCodeRevisionCompareInvalidRequest},
		{name: "wiki_compare_revisions", args: map[string]any{"pageId": "missing-page", "baseRevisionId": "base-revision", "targetRevisionId": " "}, wantCode: wikirevisions.ErrCodeRevisionCompareInvalidRequest},
	} {
		errResult := callToolStructuredError(session, tc.name, tc.args)
		Expect(errResult).To(matchMCPStructuredError(tc.wantCode, sharederrors.MessageIDForCode(tc.wantCode)), mcpToolCaseLabel(tc.name, mcpLabelRevisionBlankInputSuffix))
	}

	target := nestedMap(callToolStructured(session, "wiki_create_page", map[string]any{
		"title": "Target",
		"slug":  "target",
		"kind":  "page",
	}), "page")
	targetID := stringField(target, "id")
	ref := nestedMap(callToolStructured(session, "wiki_create_page", map[string]any{
		"title": "Ref",
		"slug":  "ref",
		"kind":  "page",
	}), "page")
	refID := stringField(ref, "id")
	callToolStructured(session, "wiki_update_page", map[string]any{
		"id":      refID,
		"version": stringField(ref, "version"),
		"title":   "Ref",
		"slug":    "ref",
		"content": "[Target](/target.md)",
	})

	first := nestedMap(callToolStructured(session, "wiki_update_page", map[string]any{
		"id":      targetID,
		"version": stringField(target, "version"),
		"title":   "Target",
		"slug":    "target",
		"content": "First content",
	}), "page")
	second := nestedMap(callToolStructured(session, "wiki_update_page", map[string]any{
		"id":      targetID,
		"version": stringField(first, "version"),
		"title":   "Target",
		"slug":    "target",
		"content": "Second content",
	}), "page")
	targetPageID := tree.PageIDFromString(targetID)
	httpUpdated := updateHTTPPage(router, targetPageID, map[string]any{
		"version": stringField(second, "version"),
		"title":   "Target",
		"slug":    "target",
		"content": "Third content from HTTP",
	})

	limitErr := callToolStructuredError(session, "wiki_list_revisions", map[string]any{"pageId": targetID, "limit": float64(201)})
	Expect(limitErr).To(testmatchers.HaveMCPStructuredError(
		wikirevisions.ErrCodeRevisionInvalidLimit,
		sharederrors.MessageIDForCode(wikirevisions.ErrCodeRevisionInvalidLimit),
	))

	revisions := callToolStructured(session, "wiki_list_revisions", map[string]any{"pageId": targetID, "limit": float64(20)})
	httpRevisions := getHTTPMap(router, "/api/pages/"+targetID+"/revisions?limit=20")
	Expect(revisions).To(matchJSONEqual(httpRevisions), "wiki_list_revisions")
	recordHTTPMCPParity("wiki_list_revisions", "GET /api/pages/:id/revisions")
	revisionItems := arrayFieldFromMap(revisions, "revisions")
	Expect(revisionItems).To(ContainElements(
		BeAssignableToTypeOf(map[string]any{}),
		BeAssignableToTypeOf(map[string]any{}),
	))
	firstRevision := revisionItems[0].(map[string]any)
	Expect(firstRevision).NotTo(HaveKey("page_id"))
	Expect(firstRevision).To(HaveKeyWithValue("pageId", targetID))
	latest := callToolStructured(session, "wiki_get_latest_revision", map[string]any{"pageId": targetID})
	latestRevision := nestedMap(latest, "revision")
	httpLatestRevision := getHTTPLatestRevision(router, targetPageID)
	Expect(latestRevision).To(matchJSONEqual(httpLatestRevision), "wiki_get_latest_revision")
	recordHTTPMCPParity("wiki_get_latest_revision", "GET /api/pages/:id/revisions/latest")
	latestRevisionID := stringField(latestRevision, "id")
	Expect(latestRevision).NotTo(HaveKey("page_id"))
	Expect(latestRevision).To(HaveKeyWithValue("pageId", targetID))
	snapshot := callToolStructured(session, "wiki_get_revision", map[string]any{"pageId": targetID, "revisionId": latestRevisionID})
	httpSnapshotAtLatest := getHTTPRevision(router, targetPageID, tree.RevisionIDFromString(latestRevisionID))
	Expect(snapshot).To(matchJSONEqual(httpSnapshotAtLatest), "wiki_get_revision")
	recordHTTPMCPParity("wiki_get_revision", "GET /api/pages/:id/revisions/:revisionId")
	Expect(stringField(snapshot, "content")).To(ContainSubstring("Third content from HTTP"))
	snapshotRevision := nestedMap(snapshot, "revision")
	Expect(snapshotRevision).To(HaveKeyWithValue("pageId", targetID))
	mcpAfterHTTP := nestedMap(callToolStructured(session, "wiki_update_page", map[string]any{
		"id":      targetID,
		"version": stringField(httpUpdated, "version"),
		"title":   "Target",
		"slug":    "target",
		"content": "Fourth content from MCP",
	}), "page")
	Expect(mcpAfterHTTP).To(HaveKeyWithValue("content", "Fourth content from MCP"))
	httpLatest := getHTTPLatestRevision(router, targetPageID)
	httpLatestRevisionID := stringField(httpLatest, "id")
	httpSnapshot := getHTTPRevision(router, targetPageID, tree.RevisionIDFromString(httpLatestRevisionID))
	Expect(stringField(httpSnapshot, "content")).To(ContainSubstring("Fourth content from MCP"))

	olderRevision := revisionItems[1].(map[string]any)
	comparison := callToolStructured(session, "wiki_compare_revisions", map[string]any{
		"pageId":           targetID,
		"baseRevisionId":   stringField(olderRevision, "id"),
		"targetRevisionId": httpLatestRevisionID,
	})
	httpComparison := getHTTPMap(router, "/api/pages/"+targetID+"/revisions/compare?base="+url.QueryEscape(stringField(olderRevision, "id"))+"&target="+url.QueryEscape(httpLatestRevisionID))
	Expect(comparison).To(matchJSONEqual(httpComparison), "wiki_compare_revisions")
	recordHTTPMCPParity("wiki_compare_revisions", "GET /api/pages/:id/revisions/compare")
	Expect(comparison).To(HaveKeyWithValue("contentChanged", true))

	callToolStructured(session, "wiki_upload_asset", map[string]any{
		"pageId":        targetID,
		"filename":      "style.css",
		"contentBase64": base64.StdEncoding.EncodeToString([]byte("body { color: green; }\n")),
	})
	assetRevision := nestedMap(callToolStructured(session, "wiki_get_latest_revision", map[string]any{"pageId": targetID}), "revision")
	assetRevisionID := stringField(assetRevision, "id")
	revisionAssetErr := callToolStructuredError(session, "wiki_get_revision_asset", map[string]any{
		"pageId":     targetID,
		"revisionId": assetRevisionID,
		"assetName":  "style.css",
	})
	Expect(revisionAssetErr).To(testmatchers.HaveMCPStructuredError(
		wikirevisions.ErrCodeRevisionNotFound,
		sharederrors.MessageIDForCode(wikirevisions.ErrCodeRevisionNotFound),
	))
	httpRevisionAssetErr := getHTTPStatus(router, "/api/pages/"+targetID+"/revisions/"+assetRevisionID+"/assets/style.css", http.StatusNotFound)
	Expect(httpRevisionAssetErr).To(matchHTTPPageError(wikirevisions.ErrCodeRevisionPreviewAssetNotFound, sharederrors.MessageIDForCode(wikirevisions.ErrCodeRevisionPreviewAssetNotFound)), mcpLabelRevisionAssetGitBackedHTTP)
	recordHTTPMCPParity("wiki_get_revision_asset", "GET /api/pages/:id/revisions/:revisionId/assets/:name")

	invalidRefactorKindErr := callToolStructuredError(session, "wiki_preview_page_refactor", map[string]any{
		"id":    targetID,
		"kind":  "copy",
		"title": "Target",
		"slug":  "target-copy",
	})
	Expect(invalidRefactorKindErr).To(matchMCPPageError(wikipages.ErrCodePageInvalidRefactorKind, sharederrors.MessageIDForCode(wikipages.ErrCodePageInvalidRefactorKind)), mcpLabelPreviewRefactorInvalidKind)
	invalidRefactorKindHTTP := postHTTPJSONBody(router, "/api/pages/"+targetID+"/refactor/preview", map[string]any{
		"kind":  "copy",
		"title": "Target",
		"slug":  "target-copy",
	}, http.StatusBadRequest)
	Expect(invalidRefactorKindHTTP).To(matchHTTPPageError(wikipages.ErrCodePageInvalidRefactorKind, sharederrors.MessageIDForCode(wikipages.ErrCodePageInvalidRefactorKind)))
	paddedRefactorKindErr := callToolStructuredError(session, "wiki_preview_page_refactor", map[string]any{
		"id":    targetID,
		"kind":  " rename ",
		"title": "Target",
		"slug":  "target-padded",
	})
	Expect(paddedRefactorKindErr).To(matchMCPPageError(wikipages.ErrCodePageInvalidRefactorKind, sharederrors.MessageIDForCode(wikipages.ErrCodePageInvalidRefactorKind)), mcpLabelPreviewRefactorPaddedKind)
	paddedRefactorKindHTTP := postHTTPJSONBody(router, "/api/pages/"+targetID+"/refactor/preview", map[string]any{
		"kind":  " rename ",
		"title": "Target",
		"slug":  "target-padded",
	}, http.StatusBadRequest)
	Expect(paddedRefactorKindHTTP).To(matchHTTPPageError(wikipages.ErrCodePageInvalidRefactorKind, sharederrors.MessageIDForCode(wikipages.ErrCodePageInvalidRefactorKind)))
	currentForInvalidApply := nestedMap(callToolStructured(session, "wiki_get_page", map[string]any{"id": targetID}), "page")
	invalidApplyKindErr := callToolStructuredError(session, "wiki_apply_page_refactor", map[string]any{
		"id":      targetID,
		"version": stringField(currentForInvalidApply, "version"),
		"kind":    "copy",
		"title":   "Target",
		"slug":    "target-copy",
	})
	Expect(invalidApplyKindErr).To(matchMCPPageError(wikipages.ErrCodePageInvalidRefactorKind, sharederrors.MessageIDForCode(wikipages.ErrCodePageInvalidRefactorKind)), mcpLabelApplyRefactorInvalidKind)
	invalidApplyKindHTTP := postHTTPJSONBody(router, "/api/pages/"+targetID+"/refactor/apply", map[string]any{
		"version": stringField(currentForInvalidApply, "version"),
		"kind":    "copy",
		"title":   "Target",
		"slug":    "target-copy",
	}, http.StatusBadRequest)
	Expect(invalidApplyKindHTTP).To(matchHTTPPageError(wikipages.ErrCodePageInvalidRefactorKind, sharederrors.MessageIDForCode(wikipages.ErrCodePageInvalidRefactorKind)))
	whitespaceRefactorParentErr := callToolStructuredError(session, "wiki_preview_page_refactor", map[string]any{
		"id":       targetID,
		"kind":     "move",
		"parentId": " ",
	})
	Expect(whitespaceRefactorParentErr).To(matchMCPPageError(wikipages.ErrCodePageInvalidParentID, sharederrors.MessageIDForCode(wikipages.ErrCodePageInvalidParentID)), mcpLabelPreviewRefactorWhitespaceParentID)
	whitespaceRefactorParentHTTP := postHTTPJSONBody(router, "/api/pages/"+targetID+"/refactor/preview", map[string]any{
		"kind":     "move",
		"parentId": " ",
	}, http.StatusBadRequest)
	Expect(whitespaceRefactorParentHTTP).To(matchHTTPPageError(wikipages.ErrCodePageInvalidParentID, sharederrors.MessageIDForCode(wikipages.ErrCodePageInvalidParentID)))
	refactorPaddedParent := nestedMap(callToolStructured(session, "wiki_create_page", map[string]any{
		"title": "Refactor Padded Parent",
		"slug":  "refactor-padded-parent",
		"kind":  "section",
	}), "page")
	refactorPaddedTarget := nestedMap(callToolStructured(session, "wiki_create_page", map[string]any{
		"title": "Refactor Padded Target",
		"slug":  "refactor-padded-target",
		"kind":  "page",
	}), "page")
	paddedApplyParentErr := callToolStructuredError(session, "wiki_apply_page_refactor", map[string]any{
		"id":       stringField(refactorPaddedTarget, "id"),
		"version":  stringField(refactorPaddedTarget, "version"),
		"kind":     "move",
		"parentId": " " + stringField(refactorPaddedParent, "id") + " ",
	})
	Expect(paddedApplyParentErr).To(matchMCPPageError(wikipages.ErrCodePageInvalidParentID, sharederrors.MessageIDForCode(wikipages.ErrCodePageInvalidParentID)), mcpLabelApplyRefactorPaddedParentID)
	paddedApplyParentHTTP := postHTTPJSONBody(router, "/api/pages/"+stringField(refactorPaddedTarget, "id")+"/refactor/apply", map[string]any{
		"version":  stringField(refactorPaddedTarget, "version"),
		"kind":     "move",
		"parentId": " " + stringField(refactorPaddedParent, "id") + " ",
	}, http.StatusBadRequest)
	Expect(paddedApplyParentHTTP).To(matchHTTPPageError(wikipages.ErrCodePageInvalidParentID, sharederrors.MessageIDForCode(wikipages.ErrCodePageInvalidParentID)))

	preview := callToolStructured(session, "wiki_preview_page_refactor", map[string]any{
		"id":    targetID,
		"kind":  "rename",
		"title": "Target",
		"slug":  "target-renamed",
	})
	httpPreview := postHTTPJSON(router, "/api/pages/"+targetID+"/refactor/preview", map[string]any{
		"kind":  "rename",
		"title": "Target",
		"slug":  "target-renamed",
	}, http.StatusOK)
	Expect(preview).To(matchJSONEqual(httpPreview), "wiki_preview_page_refactor")
	recordHTTPMCPParity("wiki_preview_page_refactor", "POST /api/pages/:id/refactor/preview")
	counts := nestedMap(preview, "counts")
	Expect(counts).To(HaveKeyWithValue("affectedPages", float64(1)))

	staleRefactor := nestedMap(callToolStructured(session, "wiki_create_page", map[string]any{
		"title": "Stale Refactor",
		"slug":  "stale-refactor",
		"kind":  "page",
	}), "page")
	staleRefactorID := stringField(staleRefactor, "id")
	updateHTTPPage(router, tree.PageIDFromString(staleRefactorID), map[string]any{
		"version": stringField(staleRefactor, "version"),
		"title":   "Stale Refactor",
		"slug":    "stale-refactor",
		"content": "newer version",
	})
	staleRefactorErr := callToolStructuredError(session, "wiki_apply_page_refactor", map[string]any{
		"id":           staleRefactorID,
		"version":      stringField(staleRefactor, "version"),
		"kind":         "rename",
		"title":        "Stale Refactor",
		"slug":         "stale-refactor-mcp",
		"rewriteLinks": true,
	})
	staleRefactorHTTP := postHTTPJSONBody(router, "/api/pages/"+staleRefactorID+"/refactor/apply", map[string]any{
		"version":      stringField(staleRefactor, "version"),
		"kind":         "rename",
		"title":        "Stale Refactor",
		"slug":         "stale-refactor-http",
		"rewriteLinks": true,
	}, http.StatusConflict)
	Expect(staleRefactorErr).To(matchMCPPageVersionConflict(), "stale wiki_apply_page_refactor MCP")
	Expect(staleRefactorHTTP).To(matchHTTPPageVersionConflict(), "stale wiki_apply_page_refactor HTTP")

	httpApplyTarget := nestedMap(callToolStructured(session, "wiki_create_page", map[string]any{
		"title": "HTTP Apply Target",
		"slug":  "http-apply-target",
		"kind":  "page",
	}), "page")
	httpApplyRef := nestedMap(callToolStructured(session, "wiki_create_page", map[string]any{
		"title": "HTTP Apply Ref",
		"slug":  "http-apply-ref",
		"kind":  "page",
	}), "page")
	callToolStructured(session, "wiki_update_page", map[string]any{
		"id":      stringField(httpApplyRef, "id"),
		"version": stringField(httpApplyRef, "version"),
		"title":   "HTTP Apply Ref",
		"slug":    "http-apply-ref",
		"content": "[HTTP Apply Target](/http-apply-target.md)",
	})
	httpAppliedViaRoute := postHTTPJSON(router, "/api/pages/"+stringField(httpApplyTarget, "id")+"/refactor/apply", map[string]any{
		"version":      stringField(httpApplyTarget, "version"),
		"kind":         "rename",
		"title":        "HTTP Apply Target",
		"slug":         "http-apply-target-renamed",
		"rewriteLinks": true,
	}, http.StatusOK)
	Expect(httpAppliedViaRoute).To(matchPageState(tree.PageIDFromString(stringField(httpApplyTarget, "id")), "HTTP Apply Target", newFixtureSlug("http-apply-target-renamed"), "http-apply-target-renamed", "page", newFixturePageID("")), "HTTP wiki_apply_page_refactor success")
	httpApplyRefAfter := getHTTPPageByPath(router, "http-apply-ref")
	Expect(httpApplyRefAfter).To(HaveKeyWithValue("content", "[HTTP Apply Target](/http-apply-target-renamed.md)"))

	currentTarget := nestedMap(callToolStructured(session, "wiki_get_page", map[string]any{"id": targetID}), "page")
	applied := nestedMap(callToolStructured(session, "wiki_apply_page_refactor", map[string]any{
		"id":           targetID,
		"version":      stringField(currentTarget, "version"),
		"kind":         "rename",
		"title":        "Target",
		"slug":         "target-renamed",
		"rewriteLinks": true,
	}), "page")
	Expect(applied).To(HaveKeyWithValue("slug", "target-renamed"))
	httpApplied := getHTTPPageByID(router, targetPageID)
	Expect(applied).To(matchJSONEqual(httpApplied), "wiki_apply_page_refactor")
	Expect(applied).To(matchPageState(targetPageID, "Target", newFixtureSlug("target-renamed"), "target-renamed", "page", newFixturePageID("")), "MCP wiki_apply_page_refactor success")
	refHTTP := getHTTPPageByPath(router, "ref")
	Expect(refHTTP).To(HaveKeyWithValue("content", "[Target](/target-renamed.md)"))
	recordHTTPMCPParity("wiki_apply_page_refactor", "POST /api/pages/:id/refactor/apply")

	restored := nestedMap(callToolStructured(session, "wiki_restore_revision", map[string]any{
		"pageId":     targetID,
		"revisionId": latestRevisionID,
	}), "page")
	Expect(restored).To(HaveKeyWithValue("content", "Third content from HTTP"))
	httpRestored := getHTTPPageByID(router, targetPageID)
	Expect(restored).To(matchJSONEqual(httpRestored), "wiki_restore_revision")

	mcpRestoreMeta := nestedMap(callToolStructured(session, "wiki_create_page", map[string]any{
		"title": "MCP Restore Metadata",
		"slug":  "mcp-restore-metadata",
		"kind":  "page",
	}), "page")
	mcpRestoreMetaID := stringField(mcpRestoreMeta, "id")
	mcpRestoreMetaRevision := nestedMap(callToolStructured(session, "wiki_update_page", map[string]any{
		"id":      mcpRestoreMetaID,
		"version": stringField(mcpRestoreMeta, "version"),
		"title":   "MCP Restore Metadata",
		"slug":    "mcp-restore-metadata",
		"content": "metadata revision\n",
		"tags":    []any{"restore", "metadata"},
		"properties": map[string]any{
			"status": "archived",
		},
	}), "page")
	mcpRestoreMetaLatest := nestedMap(callToolStructured(session, "wiki_get_latest_revision", map[string]any{"pageId": mcpRestoreMetaID}), "revision")
	callToolStructured(session, "wiki_update_page", map[string]any{
		"id":      mcpRestoreMetaID,
		"version": stringField(mcpRestoreMetaRevision, "version"),
		"title":   "MCP Restore Metadata",
		"slug":    "mcp-restore-metadata",
		"content": "current revision\n",
	})
	mcpRestoredMeta := nestedMap(callToolStructured(session, "wiki_restore_revision", map[string]any{
		"pageId":     mcpRestoreMetaID,
		"revisionId": stringField(mcpRestoreMetaLatest, "id"),
	}), "page")
	Expect(mcpRestoredMeta).To(matchRestoredMetadata(), "MCP restore metadata")
	callToolStructured(session, "wiki_update_page", map[string]any{
		"id":      mcpRestoreMetaID,
		"version": stringField(mcpRestoredMeta, "version"),
		"title":   "MCP Restore Metadata",
		"slug":    "mcp-restore-metadata",
		"content": "current revision before HTTP restore\n",
	})
	httpRestoredMCPMeta := postHTTPJSON(router, "/api/pages/"+mcpRestoreMetaID+"/revisions/"+stringField(mcpRestoreMetaLatest, "id")+"/restore", nil, http.StatusOK)
	Expect(httpRestoredMCPMeta).To(matchRestoredMetadata(), "HTTP restore metadata on MCP fixture")
	Expect(mcpRestoredMeta).To(matchRestoreVolatileFields(), "MCP wiki_restore_revision")
	Expect(httpRestoredMCPMeta).To(matchRestoreVolatileFields(), "HTTP wiki_restore_revision")
	Expect(normalizeRestorePayload(mcpRestoredMeta)).To(matchJSONEqual(normalizeRestorePayload(httpRestoredMCPMeta)), "wiki_restore_revision response payload")

	httpRestoreMeta := postHTTPJSON(router, "/api/pages", map[string]any{
		"title": "HTTP Restore Metadata",
		"slug":  "http-restore-metadata",
		"kind":  "page",
	}, http.StatusCreated)
	httpRestoreMetaID := stringField(httpRestoreMeta, "id")
	httpRestoreMetaPageID := tree.PageIDFromString(httpRestoreMetaID)
	httpRestoreMetaRevision := updateHTTPPage(router, httpRestoreMetaPageID, map[string]any{
		"version": stringField(httpRestoreMeta, "version"),
		"title":   "HTTP Restore Metadata",
		"slug":    "http-restore-metadata",
		"content": "metadata revision\n",
		"tags":    []string{"restore", "metadata"},
		"properties": map[string]string{
			"status": "archived",
		},
	})
	httpRestoreMetaLatest := getHTTPLatestRevision(router, httpRestoreMetaPageID)
	updateHTTPPage(router, httpRestoreMetaPageID, map[string]any{
		"version": stringField(httpRestoreMetaRevision, "version"),
		"title":   "HTTP Restore Metadata",
		"slug":    "http-restore-metadata",
		"content": "current revision\n",
	})
	httpRestoredMeta := postHTTPJSON(router, "/api/pages/"+httpRestoreMetaID+"/revisions/"+stringField(httpRestoreMetaLatest, "id")+"/restore", nil, http.StatusOK)
	Expect(httpRestoredMeta).To(matchRestoredMetadata(), "HTTP restore metadata")
	Expect(getHTTPPageByID(router, httpRestoreMetaPageID)).To(matchRestoredMetadata(), "HTTP restore metadata persisted")
	recordHTTPMCPParity("wiki_restore_revision", "POST /api/pages/:id/revisions/:revisionId/restore")
}
