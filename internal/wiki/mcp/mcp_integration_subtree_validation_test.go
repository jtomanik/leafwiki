package mcp_test

import (
	"encoding/base64"
	"fmt"
	"net/http"
	"os"
	"path/filepath"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/perber/wiki/internal/core/assets"
	wikivalidation "github.com/perber/wiki/internal/core/markdownvalidation"
	sharederrors "github.com/perber/wiki/internal/core/shared/errors"
	httpinternal "github.com/perber/wiki/internal/http"
	testmatchers "github.com/perber/wiki/internal/test_utils/matchers"
	"github.com/perber/wiki/internal/wiki"
	wikimcp "github.com/perber/wiki/internal/wiki/mcp"
	wikipages "github.com/perber/wiki/internal/wiki/pages"
)

// Canonical Markdown links plan scenarios covered by tests in this file:
// - MCP agent context returns canonical examples

var _ = Describe("local MCP subtree lookup", Label("integration"), func() {
	It("returns path roots with breadcrumbs and requested expansion details", func() {
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

		parentPage := postHTTPJSON(router, "/api/pages", map[string]any{
			"title": "Docs",
			"slug":  "docs",
			"kind":  "section",
		}, http.StatusCreated)
		childPage := postHTTPJSON(router, "/api/pages", map[string]any{
			"parentId": stringField(parentPage, "id"),
			"title":    "Reference",
			"slug":     "reference",
			"kind":     "page",
		}, http.StatusCreated)
		postHTTPJSON(router, "/api/pages", map[string]any{
			"title": "Target",
			"slug":  "target",
			"kind":  "page",
		}, http.StatusCreated)
		callToolStructured(session, "wiki_update_page", map[string]any{
			"id":      stringField(childPage, "id"),
			"version": stringField(childPage, "version"),
			"title":   "Reference",
			"slug":    "reference",
			"content": "Reference content with [Target](/target.md).",
		})

		out := callToolStructured(session, "wiki_get_subtree", map[string]any{
			"path":  "/docs",
			"depth": float64(1),
		})
		Expect(nestedMap(out, "root")).To(SatisfyAll(
			HaveKeyWithValue("path", "docs"),
			HaveKeyWithValue("title", "Docs"),
			HaveKeyWithValue("children", HaveExactElements(HaveKeyWithValue("title", "Reference"))),
		))
		Expect(arrayField(out, "breadcrumbs")).To(HaveLen(2))
		Expect(out).To(SatisfyAll(
			HaveKeyWithValue("depth", float64(1)),
			HaveKeyWithValue("truncated", false),
		))

		expanded := callToolStructured(session, "wiki_get_subtree", map[string]any{
			"path":                  "docs",
			"depth":                 float64(1),
			"includeMetadata":       false,
			"includeLinkCounts":     true,
			"includeContentPreview": true,
		})
		Expect(nestedMap(expanded, "root")).To(SatisfyAll(
			Not(HaveKey("metadata")),
			HaveKeyWithValue("children", HaveExactElements(SatisfyAll(
				Not(HaveKey("metadata")),
				HaveKeyWithValue("contentPreview", BeAssignableToTypeOf("")),
				HaveKeyWithValue("linkCounts", HaveKeyWithValue("outgoings", float64(1))),
			))),
		))

		rootOut := callToolStructured(session, "wiki_get_subtree", nil)
		Expect(nestedMap(rootOut, "root")).To(SatisfyAll(
			HaveKeyWithValue("slug", "root"),
			HaveKeyWithValue("path", ""),
		))
		byID := callToolStructured(session, "wiki_get_subtree", map[string]any{
			"pageId": stringField(parentPage, "id"),
			"depth":  float64(0),
		})
		Expect(nestedMap(byID, "root")).To(HaveKeyWithValue("id", stringField(parentPage, "id")))

		ambiguousSubtreeErr := callToolStructuredError(session, "wiki_get_subtree", map[string]any{
			"pageId": stringField(parentPage, "id"),
			"path":   "docs",
		})
		Expect(ambiguousSubtreeErr).To(matchMCPStructuredError(wikimcp.ErrCodeMCPPageTargetAmbiguous, sharederrors.MessageIDForCode(wikimcp.ErrCodeMCPPageTargetAmbiguous)), mcpLabelGetSubtreeAmbiguousTarget)
		missingSubtreeErr := callToolStructuredError(session, "wiki_get_subtree", map[string]any{"path": "missing-subtree"})
		Expect(missingSubtreeErr).To(testmatchers.HaveMCPStructuredError(wikipages.ErrCodePageNotFound, sharederrors.MessageIDForCode(wikipages.ErrCodePageNotFound)))
		negativeDepthErr := callToolStructuredError(session, "wiki_get_subtree", map[string]any{
			"path":  "docs",
			"depth": float64(-1),
		})
		Expect(negativeDepthErr).To(matchMCPStructuredError(wikimcp.ErrCodeMCPToolError, sharederrors.MessageIDForCode(wikimcp.ErrCodeMCPToolError)), mcpLabelGetSubtreeNegativeDepth)
		hugeDepth := callToolStructured(session, "wiki_get_subtree", map[string]any{
			"path":  "docs",
			"depth": float64(999),
		})
		Expect(hugeDepth).To(HaveKeyWithValue("depth", float64(4)))
	})
})

var _ = Describe("local MCP validation tools", Label("integration"), func() {
	It("validates the current filesystem snapshot instead of stale loaded tree links", func() {
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

		page := postHTTPJSON(router, "/api/pages", map[string]any{
			"title":   "Link Source",
			"slug":    "link-source",
			"kind":    "page",
			"content": "[Missing](/missing-target)",
		}, http.StatusCreated)
		callToolStructured(session, "wiki_update_page", map[string]any{
			"id":      stringField(page, "id"),
			"version": stringField(page, "version"),
			"title":   "Link Source",
			"slug":    "link-source",
			"content": "[Missing](/missing-target)",
		})
		Expect(os.WriteFile(filepath.Join(rootDir, "link-source.md"), []byte("---\nleafwiki_id: "+stringField(page, "id")+"\nleafwiki_title: Link Source\n---\n# Link Source\n\n[Fixed](/fixed-target.md)\n"), 0o644)).To(Succeed())
		Expect(os.WriteFile(filepath.Join(rootDir, "fixed-target.md"), []byte("---\nleafwiki_id: fixed-target\nleafwiki_title: Fixed Target\n---\n# Fixed Target\n"), 0o644)).To(Succeed())

		validation := callToolStructured(session, "wiki_validate_wiki", nil)
		Expect(validation).To(HaveKeyWithValue("ok", true))
	})

	It("reports broken links from the filesystem snapshot instead of stale loaded-tree pages", func() {
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

		postHTTPJSON(router, "/api/pages", map[string]any{
			"title": "Stale Target",
			"slug":  "stale-target",
			"kind":  "page",
		}, http.StatusCreated)
		Expect(os.WriteFile(filepath.Join(rootDir, "source.md"), []byte("---\nleafwiki_id: source\nleafwiki_title: Source\n---\n# Source\n\n[Stale](/stale-target)\n"), 0o644)).To(Succeed())
		Expect(os.Remove(filepath.Join(rootDir, "stale-target.md"))).To(Succeed())

		validation := callToolStructured(session, "wiki_validate_wiki", nil)
		Expect(validation).To(HaveKeyWithValue("ok", false))
		Expect(validation).To(matchValidationIssueCodes([]wikivalidation.IssueCode{wikivalidation.IssueCodeBrokenLink}))
	})

	It("validates stored and proposed page content with typed issue results", func() {
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

		created := nestedMap(callToolStructured(session, "wiki_create_page", map[string]any{
			"title": "Valid Page",
			"slug":  "valid-page",
			"kind":  "page",
		}), "page")

		pageValidation := callToolStructured(session, "wiki_validate_page", map[string]any{
			"pageId": stringField(created, "id"),
		})
		Expect(pageValidation).To(SatisfyAll(
			HaveKeyWithValue("ok", true),
			HaveKeyWithValue("issues", BeEmpty()),
		))

		ambiguousErr := callToolStructuredError(session, "wiki_validate_page", map[string]any{
			"pageId": stringField(created, "id"),
			"path":   "valid-page",
		})
		Expect(ambiguousErr).To(matchMCPStructuredError(wikimcp.ErrCodeMCPPageTargetAmbiguous, sharederrors.MessageIDForCode(wikimcp.ErrCodeMCPPageTargetAmbiguous)), mcpLabelValidatePageAmbiguousInput)
		missingTargetErr := callToolStructuredError(session, "wiki_validate_page", map[string]any{})
		Expect(missingTargetErr).To(matchMCPStructuredError(wikimcp.ErrCodeMCPPageTargetRequired, sharederrors.MessageIDForCode(wikimcp.ErrCodeMCPPageTargetRequired)), mcpLabelValidatePageMissingTarget)

		proposed := callToolStructured(session, "wiki_validate_content", map[string]any{
			"path":    "draft",
			"content": "---\nleafwiki_id: draft\nleafwiki_title: Draft\n---\n# Draft\n",
		})
		Expect(proposed).To(HaveKeyWithValue("ok", true))
		missingDraft := callToolStructuredError(session, "wiki_get_page_by_path", map[string]any{"path": "draft"})
		Expect(missingDraft).To(testmatchers.HaveMCPStructuredError(wikipages.ErrCodePageNotFound, sharederrors.MessageIDForCode(wikipages.ErrCodePageNotFound)), mcpLabelValidateContentDoesNotWrite)

		canonicalPageLink := callToolStructured(session, "wiki_validate_content", map[string]any{
			"path":    "draft-canonical",
			"content": "[Valid](/valid-page.md)\n",
		})
		Expect(canonicalPageLink).To(HaveKeyWithValue("ok", true))
		canonicalPagePath := callToolStructured(session, "wiki_validate_content", map[string]any{
			"path":    "/valid-page.md",
			"content": fmt.Sprintf("---\nleafwiki_id: %s\nleafwiki_title: Valid Page\n---\n[Valid](/valid-page.md)\n", stringField(created, "id")),
		})
		Expect(canonicalPagePath).To(HaveKeyWithValue("ok", true))
		rootSectionLink := callToolStructured(session, "wiki_validate_content", map[string]any{
			"path":    "draft-root-link",
			"content": "[Root](/)\n",
		})
		Expect(rootSectionLink).To(HaveKeyWithValue("ok", true))
		sectionTarget := nestedMap(callToolStructured(session, "wiki_create_page", map[string]any{
			"title": "Validation Section",
			"slug":  "validation-section",
			"kind":  "section",
		}), "page")
		sectionChildTarget := nestedMap(callToolStructured(session, "wiki_create_page", map[string]any{
			"parentId": stringField(sectionTarget, "id"),
			"title":    "Validation Section Child",
			"slug":     "child",
			"kind":     "page",
		}), "page")
		relativeSectionLink := callToolStructured(session, "wiki_validate_content", map[string]any{
			"existingPageId": stringField(sectionTarget, "id"),
			"path":           "validation-section",
			"content":        "[Child](./child.md)\n",
		})
		Expect(relativeSectionLink).To(HaveKeyWithValue("ok", true))
		draftSectionLink := callToolStructured(session, "wiki_validate_content", map[string]any{
			"path":    "validation-section",
			"kind":    "section",
			"content": "[Child](./child.md)\n",
		})
		Expect(draftSectionLink).To(HaveKeyWithValue("ok", true))
		omittedKindExistingSectionLink := callToolStructured(session, "wiki_validate_content", map[string]any{
			"path":    "validation-section",
			"content": "[Child](./child.md)\n",
		})
		Expect(omittedKindExistingSectionLink).To(HaveKeyWithValue("ok", true))
		pageTwin := nestedMap(callToolStructured(session, "wiki_create_page", map[string]any{
			"title": "Validation Twin Page",
			"slug":  "validation-twin",
			"kind":  "page",
		}), "page")
		sectionTwin := nestedMap(callToolStructured(session, "wiki_create_page", map[string]any{
			"title": "Validation Twin Section",
			"slug":  "validation-twin",
			"kind":  "section",
		}), "page")
		sectionTwinChild := nestedMap(callToolStructured(session, "wiki_create_page", map[string]any{
			"parentId": stringField(sectionTwin, "id"),
			"title":    "Validation Twin Child",
			"slug":     "child",
			"kind":     "page",
		}), "page")
		omittedKindExistingSectionTwinLink := callToolStructured(session, "wiki_validate_content", map[string]any{
			"path":    "validation-twin",
			"content": "[Child](./child.md)\n",
		})
		Expect(omittedKindExistingSectionTwinLink).To(HaveKeyWithValue("ok", true))
		_ = pageTwin
		_ = sectionTwinChild
		sectionMdLink := callToolStructured(session, "wiki_validate_content", map[string]any{
			"path":    "draft-section-md",
			"content": "[Section as md](/validation-section.md)\n",
		})
		Expect(sectionMdLink).To(HaveKeyWithValue("ok", false))
		Expect(sectionMdLink).To(matchValidationIssueCodes([]wikivalidation.IssueCode{wikivalidation.IssueCodeBrokenLink}))
		_ = sectionChildTarget

		invalid := callToolStructured(session, "wiki_validate_content", map[string]any{
			"path":    "broken",
			"content": "---\nleafwiki_title: [unterminated\n---\n# Broken\n",
		})
		Expect(invalid).To(HaveKeyWithValue("ok", false))
		Expect(invalid).To(matchValidationIssueCodes([]wikivalidation.IssueCode{wikivalidation.IssueCodeMetadataParseError}))

		semantic := callToolStructured(session, "wiki_validate_content", map[string]any{
			"path": "draft-dupe",
			"content": "---\nleafwiki_id: " + stringField(created, "id") + "\nleafwiki_private: true\n---\n" +
				"[Missing](/does-not-exist)\n[Missing asset](missing.png)\n",
		})
		Expect(semantic).To(HaveKeyWithValue("ok", false))
		Expect(semantic).To(matchValidationIssueCodes([]wikivalidation.IssueCode{
			wikivalidation.IssueCodeDuplicateLeafwikiID,
			wikivalidation.IssueCodeReservedMetadata,
			wikivalidation.IssueCodeBrokenLink,
			wikivalidation.IssueCodeMissingAsset,
		}))

		assetOwner := nestedMap(callToolStructured(session, "wiki_create_page", map[string]any{
			"title": "Asset Owner",
			"slug":  "asset-owner",
			"kind":  "page",
		}), "page")
		assetBorrower := nestedMap(callToolStructured(session, "wiki_create_page", map[string]any{
			"title": "Asset Borrower",
			"slug":  "asset-borrower",
			"kind":  "page",
		}), "page")
		callToolStructured(session, "wiki_upload_asset", map[string]any{
			"pageId":        stringField(assetOwner, "id"),
			"filename":      "logo.png",
			"contentBase64": base64.StdEncoding.EncodeToString([]byte("owner logo")),
		})
		wrongPageAsset := callToolStructured(session, "wiki_validate_content", map[string]any{
			"existingPageId": stringField(assetOwner, "id"),
			"path":           "asset-owner",
			"content":        "# Asset Owner\n\n![Wrong page asset](/assets/" + stringField(assetBorrower, "id") + "/logo.png)\n",
		})
		Expect(wrongPageAsset).To(HaveKeyWithValue("ok", false))
		Expect(wrongPageAsset).To(matchValidationIssueCodes([]wikivalidation.IssueCode{wikivalidation.IssueCodeMissingAsset}))

		pathConflict := callToolStructured(session, "wiki_validate_content", map[string]any{
			"path":    "valid-page",
			"content": "---\nleafwiki_id: draft-conflict\nleafwiki_title: Draft Conflict\n---\n# Draft\n",
		})
		Expect(pathConflict).To(matchValidationIssueCodes([]wikivalidation.IssueCode{wikivalidation.IssueCodePathConflict}))
	})
})
