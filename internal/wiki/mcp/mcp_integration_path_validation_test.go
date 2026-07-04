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
	wikipages "github.com/perber/wiki/internal/wiki/pages"
)

// Canonical Markdown links plan scenarios covered by tests in this file:
// - MCP agent context returns canonical examples

var _ = Describe("local MCP path tools", Label("integration"), func() {
	It("resolve same-basename page and section twins by canonical paths", func() {
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

		rootDir := w.GetRootDir()
		Expect(os.MkdirAll(filepath.Join(rootDir, "docs", "sync"), 0o755)).To(Succeed())
		Expect(os.MkdirAll(filepath.Join(rootDir, "docs", "section-only"), 0o755)).To(Succeed())
		Expect(os.WriteFile(filepath.Join(rootDir, "docs", "sync.md"), []byte(`---
leafwiki_id: mcp-sync-page
leafwiki_title: MCP Sync Page
---
# MCP Sync Page
`), 0o644)).To(Succeed())
		Expect(os.WriteFile(filepath.Join(rootDir, "docs", "sync", "index.md"), []byte(`---
leafwiki_id: mcp-sync-section
leafwiki_title: MCP Sync Section
---
# MCP Sync Section
`), 0o644)).To(Succeed())
		Expect(os.WriteFile(filepath.Join(rootDir, "docs", "section-only", "index.md"), []byte(`---
leafwiki_id: mcp-section-only
leafwiki_title: MCP Section Only
---
# MCP Section Only
`), 0o644)).To(Succeed())
		callToolStructured(session, "wiki_refresh", map[string]any{"source": "filesystem"})

		Expect(nestedMap(callToolStructured(session, "wiki_get_page_by_path", map[string]any{
			"path": "/docs/sync.md",
		}), "page")).To(SatisfyAll(
			HaveKeyWithValue("id", "mcp-sync-page"),
			HaveKeyWithValue("kind", "page"),
		))
		Expect(nestedMap(callToolStructured(session, "wiki_get_page_by_path", map[string]any{
			"path": "/docs/sync",
		}), "page")).To(SatisfyAll(
			HaveKeyWithValue("id", "mcp-sync-section"),
			HaveKeyWithValue("kind", "section"),
		))
		Expect(nestedMap(callToolStructured(session, "wiki_get_subtree", map[string]any{
			"path": "/docs/sync.md",
		}), "root")).To(SatisfyAll(
			HaveKeyWithValue("id", "mcp-sync-page"),
			HaveKeyWithValue("kind", "page"),
		))
		Expect(nestedMap(callToolStructured(session, "wiki_get_subtree", map[string]any{
			"path": "/docs/sync",
		}), "root")).To(SatisfyAll(
			HaveKeyWithValue("id", "mcp-sync-section"),
			HaveKeyWithValue("kind", "section"),
		))
		Expect(callToolStructured(session, "wiki_validate_page", map[string]any{
			"path": "/docs/sync.md",
		})).To(HaveKeyWithValue("ok", true))
		Expect(callToolStructured(session, "wiki_validate_page", map[string]any{
			"path": "/docs/sync",
		})).To(HaveKeyWithValue("ok", true))

		draftPageBesideSection := callToolStructured(session, "wiki_validate_content", map[string]any{
			"path":    "/docs/section-only.md",
			"content": "---\nleafwiki_id: mcp-section-only-draft-page\nleafwiki_title: MCP Section Only Draft Page\n---\n# Draft Page\n",
		})
		Expect(draftPageBesideSection).To(HaveKeyWithValue("ok", true))
		Expect(draftPageBesideSection).To(matchValidationIssueCodesAbsent([]wikivalidation.IssueCode{wikivalidation.IssueCodePathConflict}))
	})

	It("uses README markdown paths as section fallbacks", func() {
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

		rootDir := w.GetRootDir()
		Expect(os.MkdirAll(filepath.Join(rootDir, "docs", "guides"), 0o755)).To(Succeed())
		Expect(os.MkdirAll(filepath.Join(rootDir, "docs", "indexed"), 0o755)).To(Succeed())
		Expect(os.MkdirAll(filepath.Join(rootDir, "docs", "no-readme"), 0o755)).To(Succeed())
		Expect(os.WriteFile(filepath.Join(rootDir, "docs", "index.md"), []byte("---\nleafwiki_id: mcp-docs-section\nleafwiki_title: Docs\n---\n# Docs\n"), 0o644)).To(Succeed())
		Expect(os.WriteFile(filepath.Join(rootDir, "README.md"), []byte("---\nleafwiki_id: mcp-root-section\nleafwiki_title: Root\n---\n# Root\n"), 0o644)).To(Succeed())
		Expect(os.WriteFile(filepath.Join(rootDir, "docs", "guides", "README.md"), []byte("---\nleafwiki_id: mcp-guides-section\nleafwiki_title: Guides\n---\n# Guides\n"), 0o644)).To(Succeed())
		Expect(os.WriteFile(filepath.Join(rootDir, "docs", "indexed", "index.md"), []byte("---\nleafwiki_id: mcp-indexed-section\nleafwiki_title: Indexed\n---\n# Indexed\n"), 0o644)).To(Succeed())
		Expect(os.WriteFile(filepath.Join(rootDir, "docs", "indexed", "README.md"), []byte("---\nleafwiki_id: mcp-indexed-readme-page\nleafwiki_title: Indexed README\n---\n# Indexed README\n"), 0o644)).To(Succeed())
		Expect(os.WriteFile(filepath.Join(rootDir, "docs", "no-readme", "index.md"), []byte("---\nleafwiki_id: mcp-no-readme-section\nleafwiki_title: No README\n---\n# No README\n"), 0o644)).To(Succeed())
		Expect(os.WriteFile(filepath.Join(rootDir, "docs", "guides", "child.md"), []byte("---\nleafwiki_id: mcp-guides-child\nleafwiki_title: Guides Child\n---\n# Guides Child\n"), 0o644)).To(Succeed())
		callToolStructured(session, "wiki_refresh", map[string]any{"source": "filesystem"})

		Expect(nestedMap(callToolStructured(session, "wiki_get_page_by_path", map[string]any{
			"path": "/docs/guides/README.md",
		}), "page")).To(SatisfyAll(
			HaveKeyWithValue("id", "mcp-guides-section"),
			HaveKeyWithValue("kind", "section"),
		))
		Expect(nestedMap(callToolStructured(session, "wiki_get_page_by_path", map[string]any{
			"path": "/docs/guides/README.md",
			"kind": "section",
		}), "page")).To(SatisfyAll(
			HaveKeyWithValue("id", "mcp-guides-section"),
			HaveKeyWithValue("kind", "section"),
		))
		Expect(nestedMap(callToolStructured(session, "wiki_get_subtree", map[string]any{
			"path": "/docs/guides/README.md",
		}), "root")).To(SatisfyAll(
			HaveKeyWithValue("id", "mcp-guides-section"),
			HaveKeyWithValue("kind", "section"),
		))
		Expect(nestedMap(callToolStructured(session, "wiki_get_page_by_path", map[string]any{
			"path": "/docs/indexed/README.md",
		}), "page")).To(SatisfyAll(
			HaveKeyWithValue("id", "mcp-indexed-readme-page"),
			HaveKeyWithValue("kind", "page"),
		))

		inactiveExplicitSectionErr := callToolStructuredError(session, "wiki_get_page_by_path", map[string]any{
			"path": "/docs/indexed/README.md",
			"kind": "section",
		})
		Expect(inactiveExplicitSectionErr).To(testmatchers.HaveMCPStructuredError(wikipages.ErrCodePageNotFound, sharederrors.MessageIDForCode(wikipages.ErrCodePageNotFound)), mcpLabelGetPageByPathInactiveReadmeSection)

		missingReadmeErr := callToolStructuredError(session, "wiki_get_page_by_path", map[string]any{
			"path": "/docs/no-readme/README.md",
		})
		Expect(missingReadmeErr).To(testmatchers.HaveMCPStructuredError(wikipages.ErrCodePageNotFound, sharederrors.MessageIDForCode(wikipages.ErrCodePageNotFound)), mcpLabelGetPageByPathMissingReadme)

		lowercaseReadmeErr := callToolStructuredError(session, "wiki_get_page_by_path", map[string]any{
			"path": "/docs/guides/readme.md",
		})
		Expect(lowercaseReadmeErr).To(testmatchers.HaveMCPStructuredError(wikipages.ErrCodePageNotFound, sharederrors.MessageIDForCode(wikipages.ErrCodePageNotFound)), mcpLabelGetPageByPathLowercaseReadme)

		traversalReadmeErr := callToolStructuredError(session, "wiki_get_page_by_path", map[string]any{
			"path": "../README.md",
			"kind": "section",
		})
		Expect(traversalReadmeErr).To(testmatchers.HaveMCPStructuredError(wikipages.ErrCodePageInvalidPath, sharederrors.MessageIDForCode(wikipages.ErrCodePageInvalidPath)), mcpLabelGetPageByPathTraversalReadme)

		Expect(callToolStructured(session, "wiki_validate_page", map[string]any{
			"path": "/docs/guides/README.md",
		})).To(HaveKeyWithValue("ok", true))

		lowercaseValidationErr := callToolStructuredError(session, "wiki_validate_page", map[string]any{
			"path": "/docs/guides/readme.md",
		})
		Expect(lowercaseValidationErr).To(testmatchers.HaveMCPStructuredError(wikipages.ErrCodePageNotFound, sharederrors.MessageIDForCode(wikipages.ErrCodePageNotFound)), mcpLabelValidatePageLowercaseReadme)

		Expect(callToolStructured(session, "wiki_validate_content", map[string]any{
			"path":    "/docs/guides/README.md",
			"content": "---\nleafwiki_id: mcp-guides-section\nleafwiki_title: Guides\n---\n[Child](./child.md)\n",
		})).To(HaveKeyWithValue("ok", true))

		Expect(callToolStructured(session, "wiki_validate_content", map[string]any{
			"path":    "/docs/guides/README.md",
			"kind":    "section",
			"content": "[Child](./child.md)\n",
		})).To(HaveKeyWithValue("ok", true))
	})
})

var _ = Describe("local MCP wiki validation", Label("integration"), func() {
	It("reports unsynced markdown issues through structured validation codes", func() {
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
			"title": "Original Duplicate ID",
			"slug":  "original-duplicate-id",
			"kind":  "page",
		}), "page")
		duplicatePath := filepath.Join(w.GetRootDir(), "unsynced-duplicate-id.md")
		Expect(os.WriteFile(duplicatePath, []byte(strings.Join([]string{
			"---",
			"leafwiki_id: " + stringField(created, "id"),
			"leafwiki_title: Unsynced Duplicate ID",
			"---",
			"# Unsynced Duplicate ID",
			"",
			"This file has not been refreshed into the tree yet.",
		}, "\n")), 0o644)).To(Succeed())

		out := callToolStructured(session, "wiki_validate_wiki", nil)
		Expect(out).To(HaveKeyWithValue("ok", false))
		Expect(out).To(matchValidationIssueCodes([]wikivalidation.IssueCode{wikivalidation.IssueCodeDuplicateLeafwikiID}))
		Expect(validationIssueByCode(out, wikivalidation.IssueCodeDuplicateLeafwikiID)).To(SatisfyAll(
			HaveKeyWithValue("path", "unsynced-duplicate-id.md"),
			HaveKeyWithValue("pageId", stringField(created, "id")),
		))
		Expect(os.MkdirAll(filepath.Join(w.GetRootDir(), "guides"), 0o755)).To(Succeed())
		Expect(os.WriteFile(filepath.Join(w.GetRootDir(), "guides", "README.md"), []byte(strings.Join([]string{
			"---",
			"leafwiki_id: guides",
			"leafwiki_title: Guides",
			"---",
			"# Guides",
		}, "\n")), 0o644)).To(Succeed())
		Expect(os.WriteFile(filepath.Join(w.GetRootDir(), "readme-fallback-link.md"), []byte(strings.Join([]string{
			"---",
			"leafwiki_id: readme-fallback-link",
			"leafwiki_title: README Fallback Link",
			"---",
			"# README Fallback Link",
			"",
			"[Guides](/guides/README.md)",
		}, "\n")), 0o644)).To(Succeed())
		readmeFallbackOut := callToolStructured(session, "wiki_validate_wiki", nil)
		Expect(readmeFallbackOut).To(matchValidationIssueCodesAbsent([]wikivalidation.IssueCode{wikivalidation.IssueCodeBrokenLink}))

		Expect(os.WriteFile(filepath.Join(w.GetRootDir(), ".hidden.md"), []byte("# Hidden\n"), 0o644)).To(Succeed())
		Expect(os.MkdirAll(filepath.Join(w.GetRootDir(), ".scratch"), 0o755)).To(Succeed())
		Expect(os.WriteFile(filepath.Join(w.GetRootDir(), ".scratch", "bad.md"), []byte("---\nleafwiki_private: true\n---\n[Missing](/missing-from-hidden-dir)\n"), 0o644)).To(Succeed())
		withoutWarnings := callToolStructured(session, "wiki_validate_wiki", map[string]any{"includeWarnings": false})
		Expect(withoutWarnings).To(matchValidationIssueCodesAbsent([]wikivalidation.IssueCode{wikivalidation.IssueCodeHiddenMarkdownPath}))
		withWarnings := callToolStructured(session, "wiki_validate_wiki", map[string]any{"includeWarnings": true})
		Expect(withWarnings).To(matchValidationIssueCodes([]wikivalidation.IssueCode{wikivalidation.IssueCodeHiddenMarkdownPath}))
		assertNoValidationIssuePath(withWarnings, ".scratch/bad.md")

		Expect(os.WriteFile(filepath.Join(w.GetRootDir(), "!!!.md"), []byte("# Invalid Slug\n"), 0o644)).To(Succeed())
		Expect(os.WriteFile(filepath.Join(w.GetRootDir(), "missing-asset.md"), []byte(strings.Join([]string{
			"---",
			"leafwiki_id: missing-asset",
			"leafwiki_title: Missing Asset",
			"---",
			"# Missing Asset",
			"",
			"[Missing asset](nope.png)",
		}, "\n")), 0o644)).To(Succeed())
		Expect(os.MkdirAll(filepath.Join(w.GetRootDir(), "route-conflict"), 0o755)).To(Succeed())
		Expect(os.WriteFile(filepath.Join(w.GetRootDir(), "route-conflict", "index.md"), []byte("# Route Conflict Section\n"), 0o644)).To(Succeed())
		Expect(os.WriteFile(filepath.Join(w.GetRootDir(), "route-conflict.md"), []byte("# Route Conflict Page\n"), 0o644)).To(Succeed())
		Expect(os.WriteFile(filepath.Join(w.GetRootDir(), "route-conflict-link.md"), []byte(strings.Join([]string{
			"---",
			"leafwiki_id: route-conflict-link",
			"leafwiki_title: Route Conflict Link",
			"---",
			"# Route Conflict Link",
			"",
			"[Ambiguous](/route-conflict)",
		}, "\n")), 0o644)).To(Succeed())
		conflicts := callToolStructured(session, "wiki_validate_wiki", map[string]any{"includeWarnings": false})
		Expect(conflicts).To(matchValidationIssueCodes([]wikivalidation.IssueCode{
			wikivalidation.IssueCodeInvalidSlug,
			wikivalidation.IssueCodeMissingAsset,
		}))

		Expect(conflicts).To(matchValidationIssueCodesAbsent([]wikivalidation.IssueCode{
			wikivalidation.IssueCodePathConflict,
			wikivalidation.IssueCodeAmbiguousLegacyLink,
		}))

		ambiguousContent := callToolStructured(session, "wiki_validate_content", map[string]any{
			"path":    "draft-route-conflict",
			"content": "[Ambiguous](/route-conflict)\n",
		})
		Expect(ambiguousContent).To(matchValidationIssueCodesAbsent([]wikivalidation.IssueCode{wikivalidation.IssueCodeAmbiguousLegacyLink}))

		broken := nestedMap(callToolStructured(session, "wiki_create_page", map[string]any{
			"title": "Broken Link",
			"slug":  "broken-link",
			"kind":  "page",
		}), "page")
		callToolStructured(session, "wiki_update_page", map[string]any{
			"id":      stringField(broken, "id"),
			"version": stringField(broken, "version"),
			"title":   "Broken Link",
			"slug":    "broken-link",
			"content": "[Missing](/missing-validation-target)\n",
		})
		duplicated := callToolStructured(session, "wiki_validate_wiki", map[string]any{"includeWarnings": false})
		Expect(validationIssueCodeCount(duplicated, wikivalidation.IssueCodeBrokenLink, "broken-link")).To(Equal(1))
	})

	It("resolves links between unsynced markdown files before refresh", func() {
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

		Expect(os.WriteFile(filepath.Join(rootDir, "unsynced-a.md"), []byte(strings.Join([]string{
			"---",
			"leafwiki_id: unsynced-a",
			"leafwiki_title: Unsynced A",
			"---",
			"# Unsynced A",
			"",
			"[Unsynced B route](/unsynced-b)",
			"[Unsynced B absolute markdown](/unsynced-b.md)",
			"[Unsynced B relative markdown](./unsynced-b.md)",
		}, "\n")), 0o644)).To(Succeed())
		Expect(os.WriteFile(filepath.Join(rootDir, "unsynced-b.md"), []byte(strings.Join([]string{
			"---",
			"leafwiki_id: unsynced-b",
			"leafwiki_title: Unsynced B",
			"---",
			"# Unsynced B",
		}, "\n")), 0o644)).To(Succeed())

		out := callToolStructured(session, "wiki_validate_wiki", map[string]any{"includeWarnings": false})
		Expect(out).To(matchValidationIssueCodesAbsent([]wikivalidation.IssueCode{wikivalidation.IssueCodeBrokenLink}))
	})
})
