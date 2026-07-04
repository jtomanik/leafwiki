package mcp_test

import (
	"os"
	"path/filepath"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/perber/wiki/internal/core/assets"
	sharederrors "github.com/perber/wiki/internal/core/shared/errors"
	httpinternal "github.com/perber/wiki/internal/http"
	testmatchers "github.com/perber/wiki/internal/test_utils/matchers"
	"github.com/perber/wiki/internal/wiki"
	wikimcp "github.com/perber/wiki/internal/wiki/mcp"
	wikipages "github.com/perber/wiki/internal/wiki/pages"
)

// Canonical Markdown links plan scenarios covered by tests in this file:
// - MCP agent context returns canonical examples

var _ = Describe("local MCP page metadata updates", Label("integration"), func() {
	It("patches metadata without changing the page body", func() {
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
			"title": "Metadata Target",
			"slug":  "metadata-target",
			"kind":  "page",
		}), "page")
		originalVersion := stringField(created, "version")
		updated := nestedMap(callToolStructured(session, "wiki_update_page", map[string]any{
			"id":      stringField(created, "id"),
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

		result := callToolStructured(session, "wiki_update_page_metadata", map[string]any{
			"pageId":           stringField(updated, "id"),
			"version":          stringField(updated, "version"),
			"addTags":          []any{"new"},
			"removeTags":       []any{"old"},
			"setProperties":    map[string]any{"status": "ready"},
			"removeProperties": []any{"owner"},
			"includePage":      true,
		})
		page := nestedMap(result, "page")
		Expect(page).To(HaveKeyWithValue("content", "Original body"))
		Expect(stringSliceField(page, "tags")).To(matchStringSet([]string{"keep", "new"}))
		Expect(nestedMap(page, "properties")).To(SatisfyAll(
			HaveKeyWithValue("status", "ready"),
			Not(HaveKey("owner")),
		))

		compact := callToolStructured(session, "wiki_update_page_metadata", map[string]any{
			"path":              "/metadata-target",
			"version":           stringField(result, "version"),
			"addTags":           []any{"quiet"},
			"includeValidation": false,
		})
		Expect(compact).NotTo(HaveKey("validation"))

		missingTargetErr := callToolStructuredError(session, "wiki_update_page_metadata", map[string]any{
			"version": stringField(compact, "version"),
			"addTags": []any{"missing-target"},
		})
		Expect(missingTargetErr).To(matchMCPStructuredError(wikimcp.ErrCodeMCPPageTargetRequired, sharederrors.MessageIDForCode(wikimcp.ErrCodeMCPPageTargetRequired)), mcpLabelUpdateMetadataMissingTarget)
		ambiguousTargetErr := callToolStructuredError(session, "wiki_update_page_metadata", map[string]any{
			"pageId":  stringField(updated, "id"),
			"path":    "/metadata-target",
			"version": stringField(compact, "version"),
			"addTags": []any{"ambiguous-target"},
		})
		Expect(ambiguousTargetErr).To(matchMCPStructuredError(wikimcp.ErrCodeMCPPageTargetAmbiguous, sharederrors.MessageIDForCode(wikimcp.ErrCodeMCPPageTargetAmbiguous)), mcpLabelUpdateMetadataAmbiguousTarget)

		beforeReserved := nestedMap(callToolStructured(session, "wiki_get_page", map[string]any{
			"pageId": stringField(updated, "id"),
		}), "page")
		reservedErr := callToolStructuredError(session, "wiki_update_page_metadata", map[string]any{
			"pageId":        stringField(updated, "id"),
			"version":       stringField(beforeReserved, "version"),
			"setProperties": map[string]any{"leafwiki_private": "true"},
		})
		Expect(reservedErr).To(matchMCPStructuredError(wikimcp.ErrCodeMCPToolError, sharederrors.MessageIDForCode(wikimcp.ErrCodeMCPToolError)), mcpLabelUpdateMetadataReservedKey)
		afterReserved := nestedMap(callToolStructured(session, "wiki_get_page", map[string]any{
			"pageId": stringField(updated, "id"),
		}), "page")
		Expect(afterReserved).To(SatisfyAll(
			HaveKeyWithValue("version", stringField(beforeReserved, "version")),
			HaveKeyWithValue("content", stringField(beforeReserved, "content")),
		))
		Expect(stringSliceField(afterReserved, "tags")).To(matchStringSet([]string{"keep", "new", "quiet"}))
		Expect(nestedMap(afterReserved, "properties")).To(SatisfyAll(
			Not(HaveKey("leafwiki_private")),
			HaveKeyWithValue("status", "ready"),
		))

		setTagsResult := callToolStructured(session, "wiki_update_page_metadata", map[string]any{
			"pageId":      stringField(updated, "id"),
			"version":     stringField(afterReserved, "version"),
			"setTags":     []any{"final"},
			"includePage": true,
		})
		setTagsPage := nestedMap(setTagsResult, "page")
		Expect(stringSliceField(setTagsPage, "tags")).To(matchStringSet([]string{"final"}))

		staleReservedErr := callToolStructuredError(session, "wiki_update_page_metadata", map[string]any{
			"pageId":        stringField(updated, "id"),
			"version":       originalVersion,
			"setProperties": map[string]any{"leafwiki_private": "true"},
		})
		Expect(staleReservedErr).To(testmatchers.HaveMCPStructuredError(wikipages.ErrCodePageVersionConflict, sharederrors.MessageIDForCode(wikipages.ErrCodePageVersionConflict)), mcpLabelUpdateMetadataStaleBeforeReserved)

		staleErr := callToolStructuredError(session, "wiki_update_page_metadata", map[string]any{
			"pageId":  stringField(updated, "id"),
			"version": originalVersion,
			"addTags": []any{"late"},
		})
		Expect(staleErr).To(matchMCPStructuredError(wikipages.ErrCodePageVersionConflict, sharederrors.MessageIDForCode(wikipages.ErrCodePageVersionConflict)), mcpLabelUpdateMetadataStaleVersion)
		afterStale := nestedMap(callToolStructured(session, "wiki_get_page", map[string]any{
			"pageId": stringField(updated, "id"),
		}), "page")
		Expect(stringSliceField(afterStale, "tags")).NotTo(ContainElement("late"))
		Expect(afterStale).To(HaveKeyWithValue("content", "Original body"))
	})
})

var _ = Describe("local MCP page frontmatter preservation", Label("integration"), func() {
	It("preserves unmanaged frontmatter while patching metadata", func() {
		w := newLocalMCPTestWikiWithOptions(wiki.WikiOptions{
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

		created := nestedMap(callToolStructured(session, "wiki_create_page", map[string]any{
			"title": "Metadata Preserve",
			"slug":  "metadata-preserve",
			"kind":  "page",
		}), "page")
		updated := nestedMap(callToolStructured(session, "wiki_update_page", map[string]any{
			"id":      stringField(created, "id"),
			"version": stringField(created, "version"),
			"title":   "Metadata Preserve",
			"slug":    "metadata-preserve",
			"content": "# Metadata Preserve\n\nBody",
			"tags":    []any{"draft"},
			"properties": map[string]any{
				"status": "draft",
			},
		}), "page")
		rawPath := filepath.Join(w.GetRootDir(), "metadata-preserve.md")
		Expect(os.WriteFile(rawPath, []byte(strings.Join([]string{
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
			"leafwiki_id: " + stringField(updated, "id"),
			"leafwiki_title: Metadata Preserve",
			"---",
			"# Metadata Preserve",
			"",
			"Body",
		}, "\n")), 0o644)).To(Succeed())
		callToolStructured(session, "wiki_refresh", map[string]any{"source": "filesystem"})
		refreshed := nestedMap(callToolStructured(session, "wiki_get_page_by_path", map[string]any{"path": "/metadata-preserve"}), "page")

		result := callToolStructured(session, "wiki_update_page_metadata", map[string]any{
			"path":          "/metadata-preserve",
			"version":       stringField(refreshed, "version"),
			"addTags":       []any{"ready"},
			"setProperties": map[string]any{"status": "published"},
			"includePage":   true,
		})
		Expect(nestedMap(result, "page")).To(HaveKeyWithValue("content", "# Metadata Preserve\n\nBody"))

		raw := readPageMarkdownByRoutePath(w.GetRootDir(), "metadata-preserve")
		doc := canonicalPageMarkdown("wiki_update_page_metadata raw markdown", raw)
		Expect(raw).NotTo(ContainSubstring("leafwiki_id:"))
		Expect(doc.Metadata.Fields).To(HaveKeyWithValue("status", "published"))
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
			Expect(raw).To(ContainSubstring(want))
		}
		contextOut := callToolStructured(session, "wiki_get_context", map[string]any{
			"syncMode":           "none",
			"recentChangesLimit": float64(5),
		})
		assertRecentChangesIncludePath(contextOut, "metadata-preserve.md")
	})

	It("preserves omitted metadata and clears explicit empty metadata", func() {
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
			"title": "MCP Metadata Preserve",
			"slug":  "mcp-metadata-preserve",
			"kind":  "page",
		}), "page")
		first := nestedMap(callToolStructured(session, "wiki_update_page", map[string]any{
			"id":      stringField(created, "id"),
			"version": stringField(created, "version"),
			"title":   "MCP Metadata Preserve",
			"slug":    "mcp-metadata-preserve",
			"content": "# MCP Metadata Preserve\n\nFirst",
			"tags":    []any{"draft"},
			"properties": map[string]any{
				"status": "draft",
			},
		}), "page")
		rawAfterFirst := readPageMarkdownByRoutePath(w.GetRootDir(), "mcp-metadata-preserve")
		firstDoc := canonicalPageMarkdown("wiki_update_page metadata preserve first update", rawAfterFirst)
		Expect(firstDoc.Metadata).To(SatisfyAll(
			HaveField("Tags", Equal([]string{"draft"})),
			HaveField("Fields", HaveKeyWithValue("status", "draft")),
		))

		metadataOnly := nestedMap(callToolStructured(session, "wiki_update_page", map[string]any{
			"id":      stringField(first, "id"),
			"version": stringField(first, "version"),
			"title":   "MCP Metadata Preserve",
			"slug":    "mcp-metadata-preserve",
			"tags":    []any{"ready"},
			"properties": map[string]any{
				"status": "ready",
			},
		}), "page")
		Expect(metadataOnly).To(HaveKeyWithValue("content", "# MCP Metadata Preserve\n\nFirst"))
		Expect(stringSliceField(metadataOnly, "tags")).To(matchStringSet([]string{"ready"}))
		Expect(nestedMap(metadataOnly, "properties")).To(HaveKeyWithValue("status", "ready"))

		omitted := nestedMap(callToolStructured(session, "wiki_update_page", map[string]any{
			"id":      stringField(metadataOnly, "id"),
			"version": stringField(metadataOnly, "version"),
			"title":   "MCP Metadata Preserve",
			"slug":    "mcp-metadata-preserve",
			"content": "# MCP Metadata Preserve\n\nSecond",
		}), "page")
		Expect(stringSliceField(omitted, "tags")).To(matchStringSet([]string{"ready"}))
		Expect(nestedMap(omitted, "properties")).To(HaveKeyWithValue("status", "ready"))

		cleared := nestedMap(callToolStructured(session, "wiki_update_page", map[string]any{
			"id":         stringField(omitted, "id"),
			"version":    stringField(omitted, "version"),
			"title":      "MCP Metadata Preserve",
			"slug":       "mcp-metadata-preserve",
			"content":    "# MCP Metadata Preserve\n\nThird",
			"tags":       []any{},
			"properties": map[string]any{},
		}), "page")
		Expect(stringSliceField(cleared, "tags")).To(matchStringSet(nil))
		Expect(nestedMap(cleared, "properties")).To(BeEmpty())
		rawAfterClear := readPageMarkdownByRoutePath(w.GetRootDir(), "mcp-metadata-preserve")
		clearDoc := canonicalPageMarkdown("wiki_update_page metadata preserve clear update", rawAfterClear)
		Expect(clearDoc.Metadata).To(SatisfyAll(
			HaveField("Tags", BeEmpty()),
			HaveField("Fields", BeEmpty()),
		))
	})
})
