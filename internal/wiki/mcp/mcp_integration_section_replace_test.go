package mcp_test

import (
	"os"
	"path/filepath"
	"strings"

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

var _ = Describe("local MCP page section replacement", Label("integration"), func() {
	It("preserves frontmatter while replacing a section body", func() {
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
			"title": "Section Preserve",
			"slug":  "section-preserve",
			"kind":  "page",
		}), "page")
		rawPath := filepath.Join(w.GetRootDir(), "section-preserve.md")
		Expect(os.WriteFile(rawPath, []byte(strings.Join([]string{
			"---",
			"tags:",
			"  - draft",
			"status: draft",
			"pinned: true",
			"audiences:",
			"  - internal",
			"  - external",
			"leafwiki_id: " + stringField(created, "id"),
			"leafwiki_title: Section Preserve",
			"---",
			"# Section Preserve",
			"",
			"## API",
			"",
			"old api",
		}, "\n")), 0o644)).To(Succeed())
		callToolStructured(session, "wiki_refresh", map[string]any{"source": "filesystem"})
		refreshed := nestedMap(callToolStructured(session, "wiki_get_page_by_path", map[string]any{"path": "/section-preserve"}), "page")

		result := callToolStructured(session, "wiki_replace_page_section", map[string]any{
			"path":              "/section-preserve",
			"version":           stringField(refreshed, "version"),
			"headingPath":       []any{"API"},
			"content":           "new api\n",
			"includePage":       true,
			"includeValidation": false,
		})
		page := nestedMap(result, "page")
		Expect(stringSliceField(page, "tags")).To(matchStringSet([]string{"draft"}))
		Expect(nestedMap(page, "properties")).To(HaveKeyWithValue("status", "draft"))

		raw := readPageMarkdownByRoutePath(w.GetRootDir(), newFixtureRoutePath("section-preserve"))
		for _, want := range []string{
			"pinned: true",
			"audiences:",
			"- internal",
			"- external",
			"status: draft",
			"## API\nnew api",
		} {
			Expect(raw).To(ContainSubstring(want))
		}
		Expect(raw).NotTo(ContainSubstring("old api"))
	})

	It("replaces only the targeted markdown section", func() {
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
			"title": "Section Target",
			"slug":  "section-target",
			"kind":  "page",
		}), "page")
		originalVersion := stringField(created, "version")
		body := "# Guide\n\nIntro\n\n```\n## API\nfake code heading\n```\n\n## API\n\nold api\n\n## Other\n\nkeep me\n"
		updated := nestedMap(callToolStructured(session, "wiki_update_page", map[string]any{
			"id":      stringField(created, "id"),
			"version": originalVersion,
			"title":   "Section Target",
			"slug":    "section-target",
			"content": body,
		}), "page")

		result := callToolStructured(session, "wiki_replace_page_section", map[string]any{
			"path":              "/section-target",
			"version":           stringField(updated, "version"),
			"headingPath":       []any{"API"},
			"content":           "new api\n",
			"includePage":       true,
			"includeValidation": false,
		})
		Expect(result).NotTo(HaveKey("validation"))
		content := stringField(nestedMap(result, "page"), "content")
		Expect(content).To(SatisfyAll(
			ContainSubstring("## API\nnew api\n"),
			ContainSubstring("```\n## API\nfake code heading\n```"),
			ContainSubstring("## Other\n\nkeep me"),
			Not(ContainSubstring("old api")),
		))

		missingTargetErr := callToolStructuredError(session, "wiki_replace_page_section", map[string]any{
			"version":     stringField(result, "version"),
			"headingPath": []any{"API"},
			"content":     "missing target",
		})
		Expect(missingTargetErr).To(matchMCPStructuredError(wikimcp.ErrCodeMCPPageTargetRequired, sharederrors.MessageIDForCode(wikimcp.ErrCodeMCPPageTargetRequired)), mcpLabelReplaceSectionMissingTarget)
		ambiguousTargetErr := callToolStructuredError(session, "wiki_replace_page_section", map[string]any{
			"pageId":      stringField(updated, "id"),
			"path":        "/section-target",
			"version":     stringField(result, "version"),
			"headingPath": []any{"API"},
			"content":     "ambiguous target",
		})
		Expect(ambiguousTargetErr).To(matchMCPStructuredError(wikimcp.ErrCodeMCPPageTargetAmbiguous, sharederrors.MessageIDForCode(wikimcp.ErrCodeMCPPageTargetAmbiguous)), mcpLabelReplaceSectionAmbiguousTarget)

		withValidation := callToolStructured(session, "wiki_replace_page_section", map[string]any{
			"path":              "/section-target",
			"version":           stringField(result, "version"),
			"headingPath":       []any{"API"},
			"content":           "new api with [Missing](/missing-section-target)\n",
			"includePage":       true,
			"includeValidation": true,
		})
		validation := nestedMap(withValidation, "validation")
		Expect(validation).To(matchValidationIssueCodes([]wikivalidation.IssueCode{wikivalidation.IssueCodeBrokenLink}))

		staleMissingHeadingErr := callToolStructuredError(session, "wiki_replace_page_section", map[string]any{
			"pageId":      stringField(updated, "id"),
			"version":     originalVersion,
			"headingPath": []any{"Missing"},
			"content":     "late missing",
		})
		Expect(staleMissingHeadingErr).To(testmatchers.HaveMCPStructuredError(wikipages.ErrCodePageVersionConflict, sharederrors.MessageIDForCode(wikipages.ErrCodePageVersionConflict)), mcpLabelReplaceSectionStaleBeforeMissing)

		staleErr := callToolStructuredError(session, "wiki_replace_page_section", map[string]any{
			"pageId":      stringField(updated, "id"),
			"version":     originalVersion,
			"headingPath": []any{"API"},
			"content":     "late change",
		})
		Expect(staleErr).To(matchMCPStructuredError(wikipages.ErrCodePageVersionConflict, sharederrors.MessageIDForCode(wikipages.ErrCodePageVersionConflict)), mcpLabelReplaceSectionStaleVersion)
		afterStale := nestedMap(callToolStructured(session, "wiki_get_page", map[string]any{
			"pageId": stringField(updated, "id"),
		}), "page")
		Expect(stringField(afterStale, "content")).To(SatisfyAll(
			Not(ContainSubstring("late change")),
			Not(ContainSubstring("old api")),
		))
	})

	It("leaves the page unchanged when section targeting fails", func() {
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
			"title": "Section Failure Target",
			"slug":  "section-failure-target",
			"kind":  "page",
		}), "page")
		body := "# Guide\n\n## Notes\n\nfirst\n\n## Notes\n\nsecond\n"
		updated := nestedMap(callToolStructured(session, "wiki_update_page", map[string]any{
			"id":      stringField(created, "id"),
			"version": stringField(created, "version"),
			"title":   "Section Failure Target",
			"slug":    "section-failure-target",
			"content": body,
		}), "page")

		ambiguousErr := callToolStructuredError(session, "wiki_replace_page_section", map[string]any{
			"pageId":      stringField(updated, "id"),
			"version":     stringField(updated, "version"),
			"headingPath": []any{"Notes"},
			"content":     "ambiguous mutation",
		})
		Expect(ambiguousErr).To(matchMCPStructuredError(wikimcp.ErrCodeMCPToolError, sharederrors.MessageIDForCode(wikimcp.ErrCodeMCPToolError)), mcpLabelReplaceSectionAmbiguousHeading)
		Expect(nestedMap(callToolStructured(session, "wiki_get_page", map[string]any{
			"pageId": stringField(updated, "id"),
		}), "page")).To(SatisfyAll(
			HaveKeyWithValue("version", stringField(updated, "version")),
			HaveKeyWithValue("content", body),
		))

		missingErr := callToolStructuredError(session, "wiki_replace_page_section", map[string]any{
			"pageId":      stringField(updated, "id"),
			"version":     stringField(updated, "version"),
			"headingPath": []any{"Missing"},
			"content":     "missing mutation",
		})
		Expect(missingErr).To(matchMCPStructuredError(wikimcp.ErrCodeMCPToolError, sharederrors.MessageIDForCode(wikimcp.ErrCodeMCPToolError)), mcpLabelReplaceSectionMissingHeading)
		Expect(nestedMap(callToolStructured(session, "wiki_get_page", map[string]any{
			"pageId": stringField(updated, "id"),
		}), "page")).To(SatisfyAll(
			HaveKeyWithValue("version", stringField(updated, "version")),
			HaveKeyWithValue("content", body),
		))

		replaced := callToolStructured(session, "wiki_replace_page_section", map[string]any{
			"pageId":      stringField(updated, "id"),
			"version":     stringField(updated, "version"),
			"headingPath": []any{"Notes"},
			"occurrence":  float64(2),
			"content":     "second updated\n",
			"includePage": true,
		})
		replacedContent := stringField(nestedMap(replaced, "page"), "content")
		Expect(replacedContent).To(SatisfyAll(
			ContainSubstring("## Notes\n\nfirst"),
			ContainSubstring("## Notes\nsecond updated"),
		))
	})
})
