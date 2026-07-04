package mcp_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
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
	"time"

	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/gcustom"
	"github.com/onsi/gomega/types"
	"github.com/perber/wiki/internal/agenthooks"
	"github.com/perber/wiki/internal/core/assets"
	"github.com/perber/wiki/internal/core/markdown"
	wikivalidation "github.com/perber/wiki/internal/core/markdownvalidation"
	sharederrors "github.com/perber/wiki/internal/core/shared/errors"
	httpinternal "github.com/perber/wiki/internal/http"
	authmw "github.com/perber/wiki/internal/http/middleware/auth"
	"github.com/perber/wiki/internal/projectdaemon"
	testmatchers "github.com/perber/wiki/internal/test_utils/matchers"
	"github.com/perber/wiki/internal/wiki"
	wikiassets "github.com/perber/wiki/internal/wiki/assets"
	wikimcp "github.com/perber/wiki/internal/wiki/mcp"
	wikipages "github.com/perber/wiki/internal/wiki/pages"
	wikirevisions "github.com/perber/wiki/internal/wiki/revisions"
	wikisearch "github.com/perber/wiki/internal/wiki/search"
	wikitags "github.com/perber/wiki/internal/wiki/tags"
)

// Canonical Markdown links plan scenarios covered by tests in this file:
// - MCP agent context returns canonical examples

var baseToolNames = wikimcp.BaseToolNames()

const (
	mcpLabelGetPageByPathInactiveReadmeSection = "get_page_by_path_inactive_readme_section"
	mcpLabelGetPageByPathMissingReadme         = "get_page_by_path_missing_readme"
	mcpLabelGetPageByPathLowercaseReadme       = "get_page_by_path_lowercase_readme"
	mcpLabelGetPageByPathTraversalReadme       = "get_page_by_path_traversal_readme"
	mcpLabelValidatePageLowercaseReadme        = "validate_page_lowercase_readme"
	mcpLabelCreatePageInvalidKind              = "create_page_invalid_kind"
	mcpLabelCreatePagePaddedKind               = "create_page_padded_kind"
	mcpLabelCreatePageWhitespaceParentID       = "create_page_whitespace_parent_id"
	mcpLabelCreatePagePaddedParentID           = "create_page_padded_parent_id"
	mcpLabelLookupPathInvalidKind              = "lookup_path_invalid_kind"
	mcpLabelEnsurePageInvalidKind              = "ensure_page_invalid_kind"
	mcpLabelMovePageWhitespaceParentID         = "move_page_whitespace_parent_id"
	mcpLabelConvertPageInvalidTargetKind       = "convert_page_invalid_target_kind"
	mcpLabelConvertPagePaddedTargetKind        = "convert_page_padded_target_kind"
	mcpLabelCopyPageWhitespaceTargetParentID   = "copy_page_whitespace_target_parent_id"
	mcpLabelCopyPagePaddedTargetParentID       = "copy_page_padded_target_parent_id"
	mcpLabelPreviewRefactorInvalidKind         = "preview_refactor_invalid_kind"
	mcpLabelPreviewRefactorPaddedKind          = "preview_refactor_padded_kind"
	mcpLabelApplyRefactorInvalidKind           = "apply_refactor_invalid_kind"
	mcpLabelPreviewRefactorWhitespaceParentID  = "preview_refactor_whitespace_parent_id"
	mcpLabelApplyRefactorPaddedParentID        = "apply_refactor_padded_parent_id"
	mcpLabelGetPageAmbiguousIdentifier         = "get_page_ambiguous_identifier"
	mcpLabelGetPageMissingIdentifier           = "get_page_missing_identifier"
	mcpLabelGetSubtreeAmbiguousTarget          = "get_subtree_ambiguous_target"
	mcpLabelGetSubtreeNegativeDepth            = "get_subtree_negative_depth"
	mcpLabelValidatePageAmbiguousInput         = "validate_page_ambiguous_input"
	mcpLabelValidatePageMissingTarget          = "validate_page_missing_target"
	mcpLabelValidateContentDoesNotWrite        = "validate_content_does_not_write"
	mcpLabelUpdateMetadataMissingTarget        = "update_metadata_missing_target"
	mcpLabelUpdateMetadataAmbiguousTarget      = "update_metadata_ambiguous_target"
	mcpLabelUpdateMetadataReservedKey          = "update_metadata_reserved_key"
	mcpLabelUpdateMetadataStaleBeforeReserved  = "update_metadata_stale_before_reserved"
	mcpLabelUpdateMetadataStaleVersion         = "update_metadata_stale_version"
	mcpLabelReplaceSectionMissingTarget        = "replace_section_missing_target"
	mcpLabelReplaceSectionAmbiguousTarget      = "replace_section_ambiguous_target"
	mcpLabelReplaceSectionAmbiguousHeading     = "replace_section_ambiguous_heading"
	mcpLabelReplaceSectionMissingHeading       = "replace_section_missing_heading"
	mcpLabelReplaceSectionStaleBeforeMissing   = "replace_section_stale_before_missing_heading"
	mcpLabelReplaceSectionStaleVersion         = "replace_section_stale_version"
	mcpLabelMetadataValidationError            = "metadata_validation_error"
	mcpLabelGetLatestRevisionMissingPage       = "get_latest_revision_missing_page"
	mcpLabelRevisionMissingSuffix              = "missing_revision"
	mcpLabelRevisionBlankInputSuffix           = "blank_revision_input"
	mcpLabelSuggestSlugBlankHTTP               = "mcp.parity.suggest_slug.blank.http"
	mcpLabelSuggestSlugPunctuationHTTP         = "mcp.parity.suggest_slug.punctuation.http"
	mcpLabelGetPageByPathBlankMCP              = "mcp.parity.get_page_by_path.blank.mcp"
	mcpLabelGetPageByPathBlankHTTP             = "mcp.parity.get_page_by_path.blank.http"
	mcpLabelConvertPageInvalidTargetKindHTTP   = "mcp.parity.convert_page.invalid_target_kind.http"
	mcpLabelConvertPagePaddedTargetKindHTTP    = "mcp.parity.convert_page.padded_target_kind.http"
	mcpLabelGetPagesByTagsBlankMCP             = "mcp.parity.get_pages_by_tags.blank.mcp"
	mcpLabelGetPagesByTagsBlankHTTP            = "mcp.parity.get_pages_by_tags.blank.http"
	mcpLabelRevisionAssetGitBackedHTTP         = "mcp.parity.revision_asset.git_backed.http"
)

func mcpToolCaseLabel(tool string, suffix string) string {
	return tool + "_" + suffix
}

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

var mcpOnlyToolNames = map[wikimcp.ToolProtocolName]struct{}{
	wikimcp.ToolGetContext.ProtocolName():         {},
	wikimcp.ToolRefresh.ProtocolName():            {},
	wikimcp.ToolGetSubtree.ProtocolName():         {},
	wikimcp.ToolValidatePage.ProtocolName():       {},
	wikimcp.ToolValidateContent.ProtocolName():    {},
	wikimcp.ToolValidateWiki.ProtocolName():       {},
	wikimcp.ToolUpdatePageMetadata.ProtocolName(): {},
	wikimcp.ToolReplacePageSection.ProtocolName(): {},
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

func hasHTTPMCPParityRecorded() bool {
	GinkgoHelper()

	seenCases, expected := expectedHTTPMCPParityCases()
	parityCoverage.Lock()
	defer parityCoverage.Unlock()
	for _, name := range expected {
		tc, ok := seenCases[name]
		if !ok {
			return false
		}
		if _, exercised := parityCoverage.seen[name][tc.HTTPRoute]; !exercised {
			return false
		}
	}
	return true
}

func recordHTTPMCPParity(tool, httpRoute string) {
	GinkgoHelper()
	for _, tc := range mcpHTTPParityCases {
		if tc.Tool == tool {
			Expect(httpRoute).To(Equal(tc.HTTPRoute), "parity record for %s used route", tool)
			parityCoverage.Lock()
			defer parityCoverage.Unlock()
			routes := parityCoverage.seen[tool]
			if routes == nil {
				routes = map[string]struct{}{}
				parityCoverage.seen[tool] = routes
			}
			routes[httpRoute] = struct{}{}
			return
		}
	}
	Expect(mcpHTTPParityCases).To(ContainElement(HaveField("Tool", tool)), "parity record for unknown tool %q", tool)
}

func snapshotHTTPMCPParityCoverage() map[string]map[string]struct{} {
	GinkgoHelper()

	parityCoverage.Lock()
	defer parityCoverage.Unlock()
	snapshot := make(map[string]map[string]struct{}, len(parityCoverage.seen))
	for tool, routes := range parityCoverage.seen {
		snapshot[tool] = make(map[string]struct{}, len(routes))
		for route := range routes {
			snapshot[tool][route] = struct{}{}
		}
	}
	return snapshot
}

func matchRecordedHTTPMCPParity() types.GomegaMatcher {
	GinkgoHelper()

	seenCases, expected := expectedHTTPMCPParityCases()
	return gcustom.MakeMatcher(func(seen map[string]map[string]struct{}) (bool, error) {
		for _, name := range expected {
			tc, ok := seenCases[name]
			if !ok {
				return false, nil
			}
			if _, exercised := seen[name][tc.HTTPRoute]; !exercised {
				return false, nil
			}
		}
		return true, nil
	}).WithMessage("record expected HTTP/MCP route parity evidence")
}

func expectedHTTPMCPParityCases() (map[string]httpMCPParityCase, []string) {
	seenCases := make(map[string]httpMCPParityCase, len(mcpHTTPParityCases))
	for _, tc := range mcpHTTPParityCases {
		seenCases[tc.Tool] = tc
	}

	expected := make([]string, 0, len(baseToolNames)+len(wikimcp.RevisionToolNames())+len(wikimcp.LinkRefactorToolNames()))
	for _, name := range baseToolNames {
		if _, mcpOnly := mcpOnlyToolNames[wikimcp.ToolProtocolNameFromWireName(name)]; mcpOnly {
			continue
		}
		expected = append(expected, name)
	}
	expected = append(expected, wikimcp.RevisionToolNames()...)
	expected = append(expected, wikimcp.LinkRefactorToolNames()...)
	sort.Strings(expected)

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
	"wiki_delete_page":           {"messageId", "message"},
	"wiki_move_page":             {"messageId", "message"},
	"wiki_sort_pages":            {"messageId", "message"},
	"wiki_ensure_page":           {"page"},
	"wiki_convert_page":          {"messageId", "message"},
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
	"wiki_delete_asset":          {"messageId", "message"},
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

var _ = Describe("local MCP registration", func() {
	It("gates the endpoint by configuration and exposes the federated tool contract", func() {
		w := newLocalMCPTestWiki(false)

		embedFrontendOrig := httpinternal.EmbedFrontend
		httpinternal.EmbedFrontend = "true"
		DeferCleanup(func() {
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
			Expect(rec).To(HaveHTTPStatus(http.StatusNotFound))
		}
		for _, path := range []string{"/mcp/", "/mcp/anything"} {
			rec := httptest.NewRecorder()
			disabledRouter.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
			Expect(rec).To(HaveHTTPStatus(http.StatusNotFound))
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
			Expect(rec).To(HaveHTTPStatus(http.StatusUnauthorized))
			challenge := rec.Header().Get("WWW-Authenticate")
			Expect(challenge).To(SatisfyAll(
				ContainSubstring("Bearer"),
				ContainSubstring("resource_metadata=\"http://example.com/.well-known/oauth-protected-resource/mcp\""),
				ContainSubstring("scope=\"leafwiki:mcp\""),
			))
		}

		rec := httptest.NewRecorder()
		disabledRouter.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/.well-known/not-real", nil))
		Expect(rec).To(HaveHTTPStatus(http.StatusNotFound), rec.Body.String())
		Expect(rec.Header().Get("Content-Type")).NotTo(HavePrefix("text/html"))

		trustedProxies, err := authmw.ParseTrustedProxies("127.0.0.1")
		Expect(err).NotTo(HaveOccurred())
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
		remoteUserSession := connectLocalMCP(remoteUserRouter, "/mcp")
		current := callToolStructured(remoteUserSession, "wiki_get_current_user", nil)
		user := nestedMap(current, "user")
		Expect(user).To(SatisfyAll(
			HaveKeyWithValue("username", "public-editor"),
			HaveKeyWithValue("role", "editor"),
		))

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
			Expect(rec).To(HaveHTTPStatus(http.StatusNotFound))
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
			Expect(rec).To(HaveHTTPStatus(http.StatusNotFound))
		}
		loopbackRec := httptest.NewRecorder()
		loopbackReq := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader("{}"))
		loopbackReq.RemoteAddr = "127.0.0.1:12345"
		nonLoopbackRouter.ServeHTTP(loopbackRec, loopbackReq)
		Expect(loopbackRec).NotTo(HaveHTTPStatus(http.StatusNotFound))

		enabledRouter := newLocalMCPTestRouter(w, httpinternal.RouterOptions{
			AuthDisabled:            true,
			PublicAccess:            true,
			AllowInsecure:           true,
			MaxAssetUploadSizeBytes: assets.DefaultMaxUploadSizeBytes,
			MCPEnabled:              true,
			MCPToolListPageSize:     5,
		})
		session := connectLocalMCP(enabledRouter, "/mcp")

		firstPage, err := session.ListTools(context.Background(), &sdkmcp.ListToolsParams{})
		Expect(err).NotTo(HaveOccurred())
		Expect(firstPage).To(SatisfyAll(
			HaveField("Tools", HaveLen(5)),
			HaveField("NextCursor", Not(BeEmpty())),
		))

		got := listAllToolNames(session)
		Expect(got).To(matchToolNames(federatedToolNames()))
		Expect(got).To(ContainElement(wikimcp.ToolRefresh.ProtocolName().WireName()))
		for _, name := range got {
			Expect(name).To(HavePrefix("wiki_"))
		}

		tools := listAllTools(session)
		Expect(tools).To(matchInputSchemas(federatedInputProperties(), federatedRequiredProperties()))
		Expect(tools).To(matchOutputSchemas(federatedOutputProperties()))

		for _, legacyName := range []string{
			strings.Join([]string{"get", "page"}, "_"),
			strings.Join([]string{"get", "tree"}, "_"),
			strings.Join([]string{"update", "page"}, "_"),
			strings.Join([]string{"list", "revisions"}, "_"),
		} {
			legacyErr := callToolProtocolError(session, legacyName, map[string]any{"id": "missing"})
			Expect(legacyErr).To(matchJSONRPCErrorCode(jsonrpc.CodeInvalidParams))
		}

		ambiguousPageIDErr := callToolStructuredError(session, "wiki_get_page", map[string]any{
			"id":     "missing-page",
			"pageId": "missing-page",
		})
		Expect(ambiguousPageIDErr).To(testmatchers.HaveMCPStructuredError(wikimcp.ErrCodeMCPPageIdentifierAmbiguous, sharederrors.MessageIDForCode(wikimcp.ErrCodeMCPPageIdentifierAmbiguous)))
		missingPageIDErr := callToolStructuredError(session, "wiki_get_page", map[string]any{})
		Expect(missingPageIDErr).To(testmatchers.HaveMCPStructuredError(wikimcp.ErrCodeMCPPageIdentifierRequired, sharederrors.MessageIDForCode(wikimcp.ErrCodeMCPPageIdentifierRequired)))

		Expect(got).NotTo(ContainElement(HavePrefix("leafwiki_")))
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
			Expect(got).NotTo(ContainElement(forbidden))
		}
	})
})

var _ = DescribeTable("local MCP tool registration honors feature gates",
	func(enableLinkRefactor bool, extraTools []string) {
		w := newLocalMCPTestWikiWithOptions(wiki.WikiOptions{
			AuthDisabled: true,
		})
		router := newLocalMCPTestRouter(w, httpinternal.RouterOptions{
			AuthDisabled:            true,
			PublicAccess:            true,
			AllowInsecure:           true,
			MaxAssetUploadSizeBytes: assets.DefaultMaxUploadSizeBytes,
			EnableLinkRefactor:      enableLinkRefactor,
			MCPEnabled:              true,
			MCPToolListPageSize:     200,
		})
		session := connectLocalMCP(router, "/mcp")

		want := federatedToolNames(extraTools)
		Expect(listAllToolNames(session)).To(matchToolNames(want))
		contextOut := callToolStructured(session, "wiki_get_context", map[string]any{"syncMode": "none"})
		server := nestedMap(contextOut, "server")
		Expect(stringSliceField(server, "tools")).To(matchStringSet(want))

		tools := listAllTools(session)
		Expect(tools).To(matchInputSchemas(federatedInputProperties(extraTools), federatedRequiredProperties(extraTools)))
		Expect(tools).To(matchOutputSchemas(federatedOutputProperties(extraTools)))
	},
	Entry("federated runtime", false, []string(nil)),
	Entry("link refactor", true, wikimcp.LinkRefactorToolNames()),
)

// - MCP agent context returns canonical examples
var _ = Describe("local MCP context", func() {
	It("returns agent-ready context state and canonical link examples", func() {
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

		toolNames := listAllToolNames(session)
		Expect(toolNames).To(ContainElement("wiki_get_context"))

		out := callToolStructured(session, "wiki_get_context", nil)
		Expect(out).To(SatisfyAll(
			HaveKey("contextToken"),
			HaveKey("changesSincePreviousContext"),
			HaveKey("contextHistory"),
			HaveKey("user"),
			HaveKey("config"),
			HaveKey("server"),
			HaveKey("syncStatus"),
			HaveKey("validation"),
			HaveKey("recentChanges"),
			HaveKeyWithValue("activeSessions", BeEmpty()),
			HaveKey("presenceStatus"),
			HaveKey("tree"),
			HaveKey("recommendedTools"),
			HaveKey("canonicalLinkExamples"),
		))
		presence := nestedMap(out, "presenceStatus")
		Expect(presence).To(SatisfyAll(
			HaveKey("web"),
			HaveKey("agentHooks"),
		))
		status := nestedMap(out, "syncStatus")
		Expect(status).To(SatisfyAll(
			HaveKey("enabled"),
			Not(HaveKey("Enabled")),
		))
		for _, name := range stringSliceField(out, "recommendedTools") {
			Expect(toolNames).To(ContainElement(name))
		}
		tree := nestedMap(out, "tree")
		Expect(tree).To(SatisfyAll(
			HaveKey("id"),
			HaveKey("children"),
		))
		examples := stringSliceField(out, "canonicalLinkExamples")
		Expect(examples).To(ContainElement(ContainSubstring("](/docs/guide.md)")))
		Expect(examples).To(ContainElement(ContainSubstring("](/docs)")))
	})
})

var _ = Describe("local MCP context recommendations", func() {
	It("recommends workspace refresh when federated runtime tools are available", func() {
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

		out := callToolStructured(session, "wiki_get_context", map[string]any{"syncMode": "none"})
		server := nestedMap(out, "server")

		Expect(server).To(HaveKeyWithValue("tools", ContainElement("wiki_refresh")))
		Expect(out).To(HaveKeyWithValue("recommendedTools", ContainElement("wiki_refresh")))
	})
})

var _ = Describe("local MCP context checkpoints", func() {
	It("uses the calling session checkpoint when no since token is supplied", func() {
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
		sessionA := connectLocalMCP(router, "/mcp")
		sessionB := connectLocalMCP(router, "/mcp")

		firstA := callToolStructured(sessionA, "wiki_get_context", map[string]any{"syncMode": "none"})
		firstAToken := stringField(firstA, "contextToken")

		postHTTPJSON(router, "/api/pages", map[string]any{
			"title": "Omitted Token Delta",
			"slug":  "omitted-token-delta",
			"kind":  "page",
		}, http.StatusCreated)

		secondA := callToolStructured(sessionA, "wiki_get_context", map[string]any{"syncMode": "none"})
		assertContextHistoryOpaque(secondA)
		Expect(secondA).To(HaveKeyWithValue("previousContextToken", firstAToken))
		Expect(changedPathsFromContext(secondA)).To(HaveKey("omitted-token-delta.md"))

		firstB := callToolStructured(sessionB, "wiki_get_context", map[string]any{"syncMode": "none"})
		Expect(firstB).To(SatisfyAll(
			HaveKeyWithValue("previousContextToken", ""),
			HaveKeyWithValue("changesSincePreviousContext", BeEmpty()),
			HaveKeyWithValue("contextHistory", HaveLen(1)),
		))
	})

	It("does not fall back to the previous checkpoint when an explicit since token is unknown", func() {
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

		first := callToolStructured(session, "wiki_get_context", map[string]any{"syncMode": "none"})
		firstToken := stringField(first, "contextToken")
		postHTTPJSON(router, "/api/pages", map[string]any{
			"title": "Unknown Token Delta",
			"slug":  "unknown-token-delta",
			"kind":  "page",
		}, http.StatusCreated)

		second := callToolStructured(session, "wiki_get_context", map[string]any{
			"syncMode":   "none",
			"sinceToken": "not-a-token",
		})

		Expect(second).To(SatisfyAll(
			HaveKeyWithValue("previousContextToken", ""),
			HaveKeyWithValue("changesSincePreviousContext", BeEmpty()),
			HaveKeyWithValue("warnings", ContainElement("unknown sinceToken; returned current context")),
		))
		Expect(stringField(first, "contextToken")).To(Equal(firstToken))
	})

	It("returns all checkpoint changes even when recent changes are display-limited", func() {
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

		first := callToolStructured(session, "wiki_get_context", map[string]any{
			"recentChangesLimit": float64(1),
			"syncMode":           "none",
		})
		token := stringField(first, "contextToken")
		for i := 0; i < 3; i++ {
			slug := fmt.Sprintf("delta-page-%d", i)
			callToolStructured(session, "wiki_create_page", map[string]any{
				"title": fmt.Sprintf("Delta Page %d", i),
				"slug":  slug,
				"kind":  "page",
			})
		}

		second := callToolStructured(session, "wiki_get_context", map[string]any{
			"sinceToken":         token,
			"recentChangesLimit": float64(1),
			"syncMode":           "none",
		})
		Expect(arrayField(second, "recentChanges")).To(HaveLen(1))
		changes := arrayField(second, "changesSincePreviousContext")
		changedPaths := changedPathsFromChanges(changes)
		Expect(changedPaths).To(SatisfyAll(
			HaveKey("delta-page-0.md"),
			HaveKey("delta-page-1.md"),
			HaveKey("delta-page-2.md"),
		))
		Expect(second).To(Or(
			Not(HaveKey("warnings")),
			HaveKeyWithValue("warnings", BeEmpty()),
		))

		tokenB := stringField(second, "contextToken")
		callToolStructured(session, "wiki_create_page", map[string]any{
			"title": "Delta Page After B",
			"slug":  "delta-page-after-b",
			"kind":  "page",
		})
		third := callToolStructured(session, "wiki_get_context", map[string]any{
			"sinceToken":         tokenB,
			"recentChangesLimit": float64(1),
			"syncMode":           "none",
		})
		changedPathsAfterB := changedPathsFromContext(third)
		Expect(changedPathsAfterB).To(HaveKey("delta-page-after-b.md"))
		for i := 0; i < 3; i++ {
			Expect(changedPathsAfterB).NotTo(HaveKey(fmt.Sprintf("delta-page-%d.md", i)))
		}
	})
})

var _ = Describe("local MCP presence", func() {
	It("includes authenticated web heartbeat state in context", func() {
		w := newLocalMCPTestWiki(false)
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

		page := nestedMap(callToolStructured(session, "wiki_create_page", map[string]any{
			"title": "Docs API",
			"slug":  "docs-api",
			"kind":  "page",
		}), "page")
		postHTTPJSON(router, "/api/presence/heartbeat", map[string]any{
			"sessionId": "tab-web-1",
			"mode":      "edit",
			"pageId":    stringField(page, "id"),
			"path":      "/docs-api",
			"dirty":     true,
		}, http.StatusOK)

		out := callToolStructured(session, "wiki_get_context", nil)
		Expect(nestedMap(out, "presenceStatus")).To(HaveKeyWithValue("web", "enabled"))
		Expect(out).To(HaveKeyWithValue("activeSessions", HaveExactElements(SatisfyAll(
			HaveKeyWithValue("type", "web"),
			HaveKeyWithValue("sessionId", "tab-web-1"),
			HaveKeyWithValue("mode", "edit"),
			HaveKeyWithValue("dirty", true),
			HaveKeyWithValue("user", SatisfyAll(
				HaveKeyWithValue("id", "public-editor"),
				HaveKeyWithValue("role", "editor"),
				Not(HaveKey("email")),
			)),
			HaveKeyWithValue("page", SatisfyAll(
				HaveKeyWithValue("id", stringField(page, "id")),
				HaveKeyWithValue("path", "/docs-api"),
				HaveKeyWithValue("title", "Docs API"),
			)),
			HaveKeyWithValue("lastSeenAt", SatisfyAll(BeAssignableToTypeOf(""), Not(BeEmpty()))),
		))))
	})

	It("merges agent hook presence into context", func() {
		w := newLocalMCPTestWiki(false)
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
		session := connectLocalMCP(router, "/mcp")

		out := callToolStructured(session, "wiki_get_context", nil)
		Expect(nestedMap(out, "presenceStatus")).To(HaveKeyWithValue("agentHooks", "enabled"))
		Expect(out).To(HaveKeyWithValue("activeSessions", HaveExactElements(SatisfyAll(
			HaveKeyWithValue("type", "agent"),
			HaveKeyWithValue("sessionId", sessionHash),
			HaveKeyWithValue("provider", "codex"),
			HaveKeyWithValue("model", "gpt-5"),
			HaveKeyWithValue("source", "hook"),
			HaveKeyWithValue("lastEvent", "SubagentStart"),
			HaveKeyWithValue("activeSubagents", float64(1)),
		))))
	})

	It("requires authentication before accepting heartbeat updates", func() {
		w := newLocalMCPTestWiki(false)
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
		Expect(rec).To(HaveHTTPStatus(http.StatusUnauthorized), rec.Body.String())
	})

	It("rejects missing CSRF and invalid heartbeat payloads without poisoning active sessions", func() {
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

		req := httptest.NewRequest(http.MethodPost, "/api/presence/heartbeat", strings.NewReader(`{"sessionId":"tab-no-csrf","mode":"view"}`))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		Expect(rec).To(HaveHTTPStatus(http.StatusForbidden), rec.Body.String())

		postHTTPJSON(router, "/api/presence/heartbeat", map[string]any{
			"sessionId": "tab-valid",
			"mode":      "view",
		}, http.StatusOK)
		postHTTPJSON(router, "/api/presence/heartbeat", map[string]any{
			"sessionId": "tab-invalid",
			"mode":      "invalid",
		}, http.StatusBadRequest)
		postHTTPJSON(router, "/api/presence/heartbeat", map[string]any{
			"sessionId": "tab-missing-page",
			"mode":      "view",
			"path":      "/deleted-page",
		}, http.StatusOK)

		out := callToolStructured(session, "wiki_get_context", map[string]any{"syncMode": "none"})
		Expect(out).To(HaveKeyWithValue("activeSessions", ConsistOf(
			HaveKeyWithValue("sessionId", "tab-valid"),
			SatisfyAll(
				HaveKeyWithValue("sessionId", "tab-missing-page"),
				Not(HaveKey("page")),
			),
		)))
	})
})

var _ = Describe("local MCP workspace refresh", func() {
	It("syncs direct markdown files into page and context results", func() {
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

		Expect(os.WriteFile(filepath.Join(rootDir, "direct.md"), []byte("---\nleafwiki_id: direct\nleafwiki_title: Direct\n---\n# Direct\n"), 0o644)).To(Succeed())

		out := callToolStructured(session, "wiki_refresh", nil)
		Expect(nestedMap(out, "syncStatus")).To(SatisfyAll(
			HaveKeyWithValue("lastCommitHash", BeAssignableToTypeOf("")),
			Not(HaveKey("LastCommitHash")),
		))
		Expect(out).To(HaveKey("validation"))

		readBack := callToolStructured(session, "wiki_get_page_by_path", map[string]any{"path": "direct"})
		Expect(nestedMap(readBack, "page")).To(SatisfyAll(
			HaveKeyWithValue("id", "direct"),
			HaveKeyWithValue("title", "Direct"),
		))

		contextOut := callToolStructured(session, "wiki_get_context", map[string]any{
			"syncMode":           "none",
			"recentChangesLimit": float64(1),
		})
		Expect(contextOut).To(HaveKeyWithValue("recentChanges", HaveExactElements(SatisfyAll(
			HaveKeyWithValue("source", "mcp"),
			HaveKeyWithValue("reason", "explicit_refresh"),
			HaveKeyWithValue("commitId", BeAssignableToTypeOf("")),
			HaveKeyWithValue("timestamp", BeAssignableToTypeOf("")),
			HaveKeyWithValue("actor", BeAssignableToTypeOf("")),
			HaveKeyWithValue("pageIds", ContainElement("direct")),
		))))
	})

	It("normalizes workspace routes before validation and refresh results", func() {
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

		Expect(os.MkdirAll(filepath.Join(rootDir, "plans"), 0o755)).To(Succeed())
		Expect(os.WriteFile(filepath.Join(rootDir, "plans", "index.md"), []byte("---\nleafwiki_id: plans\nleafwiki_title: Plans\n---\n# Plans\n"), 0o644)).To(Succeed())
		Expect(os.WriteFile(filepath.Join(rootDir, "plans", "agent_hooks.PLAN.md"), []byte("---\nleafwiki_id: agent-hooks-plan\nleafwiki_title: Agent Hooks Plan\n---\n# Agent Hooks Plan\n\ncontent\n"), 0o644)).To(Succeed())

		validateOut := callToolStructured(session, "wiki_validate_wiki", map[string]any{"includeWarnings": false})
		Expect(validateOut).To(HaveKeyWithValue("ok", true))
		Expect(validateOut).To(matchValidationIssueCodesAbsent([]wikivalidation.IssueCode{wikivalidation.IssueCodeInvalidSlug}))

		refreshOut := callToolStructured(session, "wiki_refresh", map[string]any{"source": "filesystem"})
		validation := nestedMap(refreshOut, "validation")
		Expect(validation).To(HaveKeyWithValue("ok", true))
		Expect(validation).To(matchValidationIssueCodesAbsent([]wikivalidation.IssueCode{wikivalidation.IssueCodeInvalidSlug}))

		readBack := callToolStructured(session, "wiki_get_page_by_path", map[string]any{"path": "plans/agent-hooks-plan", "kind": "page"})
		Expect(nestedMap(readBack, "page")).To(SatisfyAll(
			HaveKeyWithValue("id", "agent-hooks-plan"),
			HaveKeyWithValue("title", "Agent Hooks Plan"),
		))
	})

	It("omits validation details when validation is disabled", func() {
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

		Expect(os.WriteFile(filepath.Join(rootDir, "skip-validation.md"), []byte("---\nleafwiki_id: skip-validation\nleafwiki_title: Skip Validation\n---\n# Skip Validation\n"), 0o644)).To(Succeed())

		out := callToolStructured(session, "wiki_refresh", map[string]any{"validate": false})
		Expect(out).NotTo(HaveKey("validation"))
	})

	It("returns validation errors without disabling later MCP calls", func() {
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

		Expect(os.WriteFile(filepath.Join(rootDir, "a.md"), []byte("---\nleafwiki_id: duplicate\nleafwiki_title: A\n---\n# A\n"), 0o644)).To(Succeed())
		Expect(os.WriteFile(filepath.Join(rootDir, "b.md"), []byte("---\nleafwiki_id: duplicate\nleafwiki_title: B\n---\n# B\n"), 0o644)).To(Succeed())

		out := callToolStructured(session, "wiki_refresh", map[string]any{"source": "filesystem"})
		validation := nestedMap(out, "validation")
		Expect(nestedMap(validation, "summary")).To(HaveKeyWithValue("errors", BeNumerically(">", 0)))
		Expect(validation).To(matchValidationIssueCodes([]wikivalidation.IssueCode{wikivalidation.IssueCodeDuplicateLeafwikiID}))

		currentUser := callToolStructured(session, "wiki_get_current_user", nil)
		Expect(nestedMap(currentUser, "user")).To(HaveKeyWithValue("username", BeAssignableToTypeOf("")))
	})

	It("records an explicit filesystem refresh reason and caps context paths", func() {
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

		for i := 0; i < 25; i++ {
			slug := fmt.Sprintf("bulk-refresh-%02d", i)
			Expect(os.WriteFile(filepath.Join(rootDir, slug+".md"), []byte("---\nleafwiki_id: "+slug+"\nleafwiki_title: "+slug+"\n---\n# "+slug+"\n"), 0o644)).To(Succeed())
		}

		callToolStructured(session, "wiki_refresh", map[string]any{"source": "filesystem"})
		contextOut := callToolStructured(session, "wiki_get_context", map[string]any{
			"syncMode":           "none",
			"recentChangesLimit": float64(1),
		})
		Expect(contextOut).To(HaveKeyWithValue("recentChanges", HaveExactElements(SatisfyAll(
			HaveKeyWithValue("reason", "explicit_refresh"),
			HaveKeyWithValue("source", "filesystem"),
			HaveKeyWithValue("changedCount", BeNumerically(">=", 25)),
			HaveKeyWithValue("changedPaths", HaveLen(20)),
		))))
	})
})

var _ = Describe("local MCP subtree lookup", func() {
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

var _ = Describe("local MCP validation tools", func() {
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

var _ = Describe("local MCP path tools", func() {
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

var _ = Describe("local MCP wiki validation", func() {
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

var _ = Describe("local MCP page metadata updates", func() {
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

var _ = Describe("local MCP page frontmatter preservation", func() {
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

var _ = Describe("local MCP page section replacement", func() {
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

		raw := readPageMarkdownByRoutePath(w.GetRootDir(), "section-preserve")
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

var _ = Describe("local MCP base path registration", func() {
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

var _ = Describe("local MCP page mutation parity", func() {
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

var _ = Describe("local MCP page operation parity", func() {
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
	Expect(parentViaGet).To(matchJSONEqual(getHTTPPageByID(router, parentID)), "wiki_get_page")
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
	Expect(mcpSort).To(matchScopedSuccessPayload(httpSort, "mcp.tools.wiki_sort_pages.success", "api.pages.sort.success"), "wiki_sort_pages")
	parentAfterSort := nestedMap(callToolStructured(session, "wiki_get_page", map[string]any{"id": parentID}), "page")
	Expect(parentAfterSort).To(matchChildOrder(stringField(childB, "id"), stringField(childA, "id")), "sort_pages shared parent")
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
	Expect(getHTTPPageByPath(router, "mcp-sort-parent")).To(matchChildOrder(stringField(mcpSortB, "id"), stringField(mcpSortA, "id")), "MCP sort_pages parent")
	Expect(getHTTPPageByPath(router, "http-sort-parent")).To(matchChildOrder(stringField(httpSortB, "id"), stringField(httpSortA, "id")), "HTTP sort_pages parent")
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
	Expect(getHTTPPageByPath(router, "parent-section/ensured-mcp")).To(matchPageState(stringField(mcpEnsuredIndependent, "id"), "Ensured Independent", "ensured-mcp", "parent-section/ensured-mcp", "section", ""), "MCP ensure_page independent")
	Expect(getHTTPPageByPath(router, "parent-section/ensured-http")).To(matchPageState(stringField(httpEnsuredIndependent, "id"), "Ensured Independent", "ensured-http", "parent-section/ensured-http", "section", ""), "HTTP ensure_page independent")
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
	Expect(mcpMove).To(matchScopedSuccessPayload(httpMove, "mcp.tools.wiki_move_page.success", "api.pages.move.success"), "wiki_move_page")
	httpMoved := getHTTPPageByPath(router, "child-a")
	Expect(httpMoved).To(matchPageState(stringField(childA, "id"), "Child A", "child-a", "child-a", "page", ""), "MCP moved child A")
	httpMovedB := getHTTPPageByPath(router, "child-b")
	Expect(httpMovedB).To(matchPageState(stringField(childB, "id"), "Child B", "child-b", "child-b", "page", ""), "HTTP moved child B")
	parentAfterMove := getHTTPPageByPath(router, "parent-section")
	Expect(parentAfterMove).To(matchChildrenExcludingIDs(stringField(childA, "id"), stringField(childB, "id")), "parent after move")
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
	Expect(getHTTPPageByPath(router, "mcp-missing-parent-move")).To(matchPageState(stringField(mcpMissingParentMove, "id"), "MCP Missing Parent Move", "mcp-missing-parent-move", "mcp-missing-parent-move", "page", ""), "MCP move_page missing parentId")
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
	Expect(httpConverted).To(matchPageState(stringField(convertMe, "id"), "Convert Me", "convert-me", "convert-me", "page", ""), "MCP converted page")
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
	Expect(httpConvertedPeer).To(matchPageState(stringField(convertHTTP, "id"), "Convert HTTP", "convert-http", "convert-http", "page", ""), "HTTP converted page")
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
	Expect(getHTTPPageByPath(router, "child-a-copy")).To(matchPageState(stringField(copied, "id"), "Child A Copy", "child-a-copy", "child-a-copy", "page", ""), "MCP copied page")
	Expect(getHTTPPageByPath(router, "child-a-http-copy")).To(matchPageState(stringField(httpCopied, "id"), "Child A Copy", "child-a-http-copy", "child-a-http-copy", "page", ""), "HTTP copied page")
	Expect(getHTTPPageByPath(router, "child-a")).To(matchPageState(stringField(childA, "id"), "Child A", "child-a", "child-a", "page", ""), "copy_page source preserved")
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
	Expect(mcpDeletedPage).To(matchScopedSuccessPayload(decodeJSONMap("HTTP delete_page", []byte(httpDeletedPageBody)), "mcp.tools.wiki_delete_page.success", "api.pages.delete.success"), "wiki_delete_page")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/pages/by-path?path=child-a-copy", nil))
	Expect(rec).To(HaveHTTPStatus(http.StatusNotFound))
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/pages/by-path?path=child-a-http-copy", nil))
	Expect(rec).To(HaveHTTPStatus(http.StatusNotFound))
	recordHTTPMCPParity("wiki_delete_page", "DELETE /api/pages/:id")
}

var _ = Describe("local MCP index and asset parity", func() {
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
	httpUploaded := uploadHTTPAsset(router, sourceID, "http-note.txt", httpAssetContent, http.StatusCreated)
	Expect(uploaded).To(matchAssetURLResult("file", sourceID), "wiki_upload_asset")
	Expect(httpUploaded).To(matchAssetURLResult("file", sourceID), "HTTP upload asset")
	asset := callToolStructured(session, "wiki_get_asset", map[string]any{"pageId": sourceID, "filename": "note.txt"})
	Expect(asset).To(SatisfyAll(
		HaveKeyWithValue("filename", "note.txt"),
		HaveKeyWithValue("contentBase64", base64.StdEncoding.EncodeToString(assetContent)),
	))
	httpNoteBody, httpNoteContentType := getHTTPAssetWithContentType(router, sourceID, "note.txt")
	Expect(httpNoteBody).To(Equal(string(assetContent)))
	Expect(httpNoteContentType).To(HavePrefix(asset["mimeType"].(string)))
	httpAsset := callToolStructured(session, "wiki_get_asset", map[string]any{"pageId": sourceID, "filename": "http-note.txt"})
	Expect(httpAsset).To(HaveKeyWithValue("contentBase64", base64.StdEncoding.EncodeToString(httpAssetContent)))
	httpAssetBody, httpAssetContentType := getHTTPAssetWithContentType(router, sourceID, "http-note.txt")
	Expect(httpAssetBody).To(Equal(string(httpAssetContent)))
	Expect(httpAssetContentType).To(HavePrefix(httpAsset["mimeType"].(string)))
	listed := callToolStructured(session, "wiki_list_assets", map[string]any{"pageId": sourceID})
	Expect(stringSliceField(listed, "files")).To(ContainElement("/assets/" + sourceID + "/note.txt"))
	httpListed := getHTTPAssets(router, sourceID)
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
	httpCSSBody, httpCSSContentType := getHTTPAssetWithContentType(router, sourceID, "style.css")
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
	Expect(renamed).To(matchAssetURLResult("url", sourceID), "wiki_rename_asset")
	Expect(httpRenamed).To(matchAssetURLResult("url", sourceID), "HTTP rename_asset")
	renamedAsset := callToolStructured(session, "wiki_get_asset", map[string]any{"pageId": sourceID, "filename": "renamed.txt"})
	Expect(renamedAsset).To(HaveKeyWithValue("contentBase64", base64.StdEncoding.EncodeToString(assetContent)))
	httpRenamedBody, httpRenamedContentType := getHTTPAssetWithContentType(router, sourceID, "renamed.txt")
	Expect(httpRenamedBody).To(Equal(string(assetContent)))
	Expect(httpRenamedContentType).To(HavePrefix(renamedAsset["mimeType"].(string)))
	Expect(callToolStructuredError(session, wikimcp.ToolGetAsset, map[string]any{"pageId": sourceID, "filename": "note.txt"})).To(matchMCPStructuredError(wikiassets.ErrCodeAssetNotFound, sharederrors.MessageIDForCode(wikiassets.ErrCodeAssetNotFound)))
	getHTTPStatus(router, "/assets/"+sourceID+"/note.txt", http.StatusNotFound)
	httpRenamedAsset := callToolStructured(session, "wiki_get_asset", map[string]any{"pageId": sourceID, "filename": "http-renamed.txt"})
	Expect(httpRenamedAsset).To(HaveKeyWithValue("contentBase64", base64.StdEncoding.EncodeToString(httpAssetContent)))
	Expect(getHTTPAsset(router, sourceID, "http-renamed.txt")).To(Equal(string(httpAssetContent)))
	Expect(callToolStructuredError(session, wikimcp.ToolGetAsset, map[string]any{"pageId": sourceID, "filename": "http-note.txt"})).To(matchMCPStructuredError(wikiassets.ErrCodeAssetNotFound, sharederrors.MessageIDForCode(wikiassets.ErrCodeAssetNotFound)))
	getHTTPStatus(router, "/assets/"+sourceID+"/http-note.txt", http.StatusNotFound)
	httpListed = getHTTPAssets(router, sourceID)
	listed = callToolStructured(session, "wiki_list_assets", map[string]any{"pageId": sourceID})
	Expect(listed).To(matchJSONEqual(httpListed), "list_assets after rename")
	Expect(stringSliceField(httpListed, "files")).To(ContainElements("/assets/"+sourceID+"/renamed.txt", "/assets/"+sourceID+"/http-renamed.txt"))
	recordHTTPMCPParity("wiki_rename_asset", "PUT /api/pages/:id/assets/rename")
	mcpDeleted := callToolStructured(session, "wiki_delete_asset", map[string]any{"pageId": sourceID, "filename": "renamed.txt"})
	httpDeletedBody := deleteHTTPStatus(router, "/api/pages/"+sourceID+"/assets/http-renamed.txt", http.StatusOK)
	httpDeleted := decodeJSONMap("HTTP delete_asset", []byte(httpDeletedBody))
	Expect(mcpDeleted).To(matchScopedSuccessPayload(httpDeleted, "mcp.tools.wiki_delete_asset.success", "api.assets.delete.success"), "wiki_delete_asset")
	Expect(callToolStructuredError(session, wikimcp.ToolGetAsset, map[string]any{"pageId": sourceID, "filename": "renamed.txt"})).To(matchMCPStructuredError(wikiassets.ErrCodeAssetNotFound, sharederrors.MessageIDForCode(wikiassets.ErrCodeAssetNotFound)))
	getHTTPStatus(router, "/assets/"+sourceID+"/renamed.txt", http.StatusNotFound)
	Expect(callToolStructuredError(session, wikimcp.ToolGetAsset, map[string]any{"pageId": sourceID, "filename": "http-renamed.txt"})).To(matchMCPStructuredError(wikiassets.ErrCodeAssetNotFound, sharederrors.MessageIDForCode(wikiassets.ErrCodeAssetNotFound)))
	getHTTPStatus(router, "/assets/"+sourceID+"/http-renamed.txt", http.StatusNotFound)
	listed = callToolStructured(session, "wiki_list_assets", map[string]any{"pageId": sourceID})
	Expect(stringSliceField(listed, "files")).NotTo(ContainElement("/assets/" + sourceID + "/renamed.txt"))
	httpListed = getHTTPAssets(router, sourceID)
	Expect(listed).To(matchJSONEqual(httpListed), "list_assets after delete")
	Expect(stringSliceField(httpListed, "files")).NotTo(ContainElements("/assets/"+sourceID+"/renamed.txt", "/assets/"+sourceID+"/http-renamed.txt"))
	recordHTTPMCPParity("wiki_delete_asset", "DELETE /api/pages/:id/assets/:name")
}

var _ = Describe("local MCP asset protocol errors", func() {
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

var _ = Describe("local MCP feature-gated tool parity", func() {
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
	httpUpdated := updateHTTPPage(router, targetID, map[string]any{
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
	httpLatestRevision := getHTTPLatestRevision(router, targetID)
	Expect(latestRevision).To(matchJSONEqual(httpLatestRevision), "wiki_get_latest_revision")
	recordHTTPMCPParity("wiki_get_latest_revision", "GET /api/pages/:id/revisions/latest")
	latestRevisionID := stringField(latestRevision, "id")
	Expect(latestRevision).NotTo(HaveKey("page_id"))
	Expect(latestRevision).To(HaveKeyWithValue("pageId", targetID))
	snapshot := callToolStructured(session, "wiki_get_revision", map[string]any{"pageId": targetID, "revisionId": latestRevisionID})
	httpSnapshotAtLatest := getHTTPRevision(router, targetID, latestRevisionID)
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
	httpLatest := getHTTPLatestRevision(router, targetID)
	httpLatestRevisionID := stringField(httpLatest, "id")
	httpSnapshot := getHTTPRevision(router, targetID, httpLatestRevisionID)
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
	updateHTTPPage(router, staleRefactorID, map[string]any{
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
	Expect(httpAppliedViaRoute).To(matchPageState(stringField(httpApplyTarget, "id"), "HTTP Apply Target", "http-apply-target-renamed", "http-apply-target-renamed", "page", ""), "HTTP wiki_apply_page_refactor success")
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
	httpApplied := getHTTPPageByID(router, targetID)
	Expect(applied).To(matchJSONEqual(httpApplied), "wiki_apply_page_refactor")
	Expect(applied).To(matchPageState(targetID, "Target", "target-renamed", "target-renamed", "page", ""), "MCP wiki_apply_page_refactor success")
	refHTTP := getHTTPPageByPath(router, "ref")
	Expect(refHTTP).To(HaveKeyWithValue("content", "[Target](/target-renamed.md)"))
	recordHTTPMCPParity("wiki_apply_page_refactor", "POST /api/pages/:id/refactor/apply")

	restored := nestedMap(callToolStructured(session, "wiki_restore_revision", map[string]any{
		"pageId":     targetID,
		"revisionId": latestRevisionID,
	}), "page")
	Expect(restored).To(HaveKeyWithValue("content", "Third content from HTTP"))
	httpRestored := getHTTPPageByID(router, targetID)
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
	httpRestoreMetaRevision := updateHTTPPage(router, httpRestoreMetaID, map[string]any{
		"version": stringField(httpRestoreMeta, "version"),
		"title":   "HTTP Restore Metadata",
		"slug":    "http-restore-metadata",
		"content": "metadata revision\n",
		"tags":    []string{"restore", "metadata"},
		"properties": map[string]string{
			"status": "archived",
		},
	})
	httpRestoreMetaLatest := getHTTPLatestRevision(router, httpRestoreMetaID)
	updateHTTPPage(router, httpRestoreMetaID, map[string]any{
		"version": stringField(httpRestoreMetaRevision, "version"),
		"title":   "HTTP Restore Metadata",
		"slug":    "http-restore-metadata",
		"content": "current revision\n",
	})
	httpRestoredMeta := postHTTPJSON(router, "/api/pages/"+httpRestoreMetaID+"/revisions/"+stringField(httpRestoreMetaLatest, "id")+"/restore", nil, http.StatusOK)
	Expect(httpRestoredMeta).To(matchRestoredMetadata(), "HTTP restore metadata")
	Expect(getHTTPPageByID(router, httpRestoreMetaID)).To(matchRestoredMetadata(), "HTTP restore metadata persisted")
	recordHTTPMCPParity("wiki_restore_revision", "POST /api/pages/:id/revisions/:revisionId/restore")
}

var _ = Describe("local MCP HTTP parity evidence", func() {
	It("confirms plan-traced MCP tools have matching HTTP route evidence", func() {
		runHTTPMCPParityCoverage()
	})
})

func runHTTPMCPParityCoverage() {
	GinkgoHelper()

	if !hasHTTPMCPParityRecorded() {
		resetHTTPMCPParityCoverage()
		runLocalMCPProtocolPageMutationParity()
		runLocalMCPProtocolPageOperationParity()
		runLocalMCPProtocolIndexAndAssetParity()
		runLocalMCPProtocolFeatureGatedToolParity()
	}
	Expect(snapshotHTTPMCPParityCoverage()).To(matchRecordedHTTPMCPParity())
}

func newLocalMCPTestWiki(_ bool) *wiki.Wiki {
	GinkgoHelper()

	w, _ := newLocalMCPTestWikiWithStorage()
	return w
}

func newLocalMCPTestWikiWithStorage() (*wiki.Wiki, string) {
	GinkgoHelper()

	return newLocalMCPTestWikiWithOptionsAndStorage(wiki.WikiOptions{
		AuthDisabled: true,
	})
}

func newLocalMCPTestWikiWithOptions(opts wiki.WikiOptions) *wiki.Wiki {
	GinkgoHelper()

	w, _ := newLocalMCPTestWikiWithOptionsAndStorage(opts)
	return w
}

func newLocalMCPTestWikiWithOptionsAndStorage(opts wiki.WikiOptions) (*wiki.Wiki, string) {
	GinkgoHelper()

	storageDir := filepath.Join(mcpIntegrationTempDir(), "data")
	rootDir := filepath.Join(mcpIntegrationTempDir(), "content")
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
	Expect(err).NotTo(HaveOccurred())
	DeferCleanup(func() {
		Expect(w.Close()).To(Succeed())
	})
	return w, storageDir
}

func mcpIntegrationTempDir() string {
	GinkgoHelper()

	dir, err := os.MkdirTemp("", "leafwiki-mcp-integration-*")
	Expect(err).NotTo(HaveOccurred())
	DeferCleanup(os.RemoveAll, dir)
	return dir
}

func newLocalMCPTestRouter(w *wiki.Wiki, opts httpinternal.RouterOptions) http.Handler {
	if opts.MCPEnabled && opts.MCPBindHost == "" {
		opts.MCPBindHost = "127.0.0.1"
	}
	return httpinternal.NewRouter(w.Registrars(), w.FrontendConfig(), opts)
}

func connectLocalMCP(handler http.Handler, path string) *sdkmcp.ClientSession {
	GinkgoHelper()

	server := httptest.NewServer(handler)
	DeferCleanup(server.Close)

	client := sdkmcp.NewClient(&sdkmcp.Implementation{Name: "leafwiki-test", Version: "test"}, nil)
	session, err := client.Connect(context.Background(), &sdkmcp.StreamableClientTransport{
		Endpoint:             server.URL + path,
		HTTPClient:           server.Client(),
		DisableStandaloneSSE: true,
	}, nil)
	Expect(err).NotTo(HaveOccurred(), "MCP client should connect")
	DeferCleanup(func() { _ = session.Close() })
	return session
}

func listAllToolNames(session *sdkmcp.ClientSession) []string {
	GinkgoHelper()

	tools := listAllTools(session)
	var names []string
	for _, tool := range tools {
		names = append(names, tool.Name)
	}
	sort.Strings(names)
	return names
}

func listAllTools(session *sdkmcp.ClientSession) []*sdkmcp.Tool {
	GinkgoHelper()

	var tools []*sdkmcp.Tool
	cursor := ""
	for {
		result, err := session.ListTools(context.Background(), &sdkmcp.ListToolsParams{Cursor: cursor})
		Expect(err).NotTo(HaveOccurred(), "ListTools should return a page")
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

func matchInputSchemas(expected, expectedRequired map[string][]string) types.GomegaMatcher {
	GinkgoHelper()

	return gcustom.MakeMatcher(func(tools []*sdkmcp.Tool) (bool, error) {
		if err := inputSchemasMatch(tools, expected, expectedRequired); err != nil {
			return false, err
		}
		return true, nil
	}).WithMessage("match MCP input schema contracts")
}

func matchOutputSchemas(expected map[string][]string) types.GomegaMatcher {
	GinkgoHelper()

	return gcustom.MakeMatcher(func(tools []*sdkmcp.Tool) (bool, error) {
		if err := outputSchemasMatch(tools, expected); err != nil {
			return false, err
		}
		return true, nil
	}).WithMessage("match MCP output schema contracts")
}

func inputSchemasMatch(tools []*sdkmcp.Tool, expected, expectedRequired map[string][]string) error {
	toolByName := toolSchemaIndex(tools)
	for _, tool := range tools {
		if _, ok := expected[tool.Name]; !ok {
			return fmt.Errorf("input schema contract missing expected tool %s", tool.Name)
		}
		if _, ok := expectedRequired[tool.Name]; !ok {
			return fmt.Errorf("input schema contract missing required-property list for %s", tool.Name)
		}
	}
	for name, props := range expected {
		tool, ok := toolByName[name]
		if !ok {
			return fmt.Errorf("input schema contract missing listed tool %s", name)
		}
		schema, err := decodeToolSchemaValue("input", tool.Name, tool.InputSchema)
		if err != nil {
			return err
		}
		if err := rootSchemaHasNoCombinators("input", name, schema); err != nil {
			return err
		}
		properties := schemaProperties(schema)
		gotProps := make([]string, 0, len(properties))
		for prop, property := range properties {
			gotProps = append(gotProps, prop)
			if err := schemaPropertyHasType("input", name, prop, property); err != nil {
				return err
			}
		}
		if err := preciseInputPropertySchemas(name, properties); err != nil {
			return err
		}
		if err := schemaPropertyOrderSorted("input", name, schema); err != nil {
			return err
		}
		if !sameStringSet(gotProps, props) {
			return fmt.Errorf("input schema for %s properties = %v, want %v", name, sortedStrings(gotProps), sortedStrings(props))
		}
		if !sameStringSet(schemaStringSlice(schema["required"]), expectedRequired[name]) {
			return fmt.Errorf("input schema for %s required properties = %v, want %v", name, sortedStrings(schemaStringSlice(schema["required"])), sortedStrings(expectedRequired[name]))
		}
	}
	return nil
}

func outputSchemasMatch(tools []*sdkmcp.Tool, expected map[string][]string) error {
	toolByName := toolSchemaIndex(tools)
	for _, tool := range tools {
		if tool.OutputSchema == nil {
			return fmt.Errorf("output schema for %s should be present", tool.Name)
		}
		if _, ok := expected[tool.Name]; !ok {
			return fmt.Errorf("output schema contract missing expected tool %s", tool.Name)
		}
	}
	for name, props := range expected {
		tool, ok := toolByName[name]
		if !ok {
			return fmt.Errorf("output schema contract missing listed tool %s", name)
		}
		schema, err := decodeToolSchemaValue("output", tool.Name, tool.OutputSchema)
		if err != nil {
			return err
		}
		if err := rootSchemaHasNoCombinators("output", name, schema); err != nil {
			return err
		}
		properties := schemaProperties(schema)
		gotProps := make([]string, 0, len(properties))
		for prop, property := range properties {
			gotProps = append(gotProps, prop)
			if err := schemaPropertyHasType("output", name, prop, property); err != nil {
				return err
			}
		}
		if err := schemaPropertyOrderSorted("output", name, schema); err != nil {
			return err
		}
		if !sameStringSet(gotProps, props) {
			return fmt.Errorf("output schema for %s properties = %v, want %v", name, sortedStrings(gotProps), sortedStrings(props))
		}
		requiredProps := outputRequiredProperties(props, toolOutputOptionalProperties[name])
		if !sameStringSet(schemaStringSlice(schema["required"]), requiredProps) {
			return fmt.Errorf("output schema for %s required properties = %v, want %v", name, sortedStrings(schemaStringSlice(schema["required"])), sortedStrings(requiredProps))
		}
	}
	return nil
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

func toolSchemaIndex(tools []*sdkmcp.Tool) map[string]*sdkmcp.Tool {
	toolByName := make(map[string]*sdkmcp.Tool, len(tools))
	for _, tool := range tools {
		toolByName[tool.Name] = tool
	}
	return toolByName
}

func decodeToolSchemaValue(kind, name string, schemaValue any) (map[string]any, error) {
	raw, err := json.Marshal(schemaValue)
	if err != nil {
		return nil, fmt.Errorf("%s schema for %s should marshal: %w", kind, name, err)
	}
	var schema map[string]any
	if err := json.Unmarshal(raw, &schema); err != nil {
		return nil, fmt.Errorf("%s schema for %s should decode: %w", kind, name, err)
	}
	if schema["type"] != "object" {
		return nil, fmt.Errorf("%s schema for %s should be an object schema", kind, name)
	}
	return schema, nil
}

func schemaProperties(schema map[string]any) map[string]any {
	GinkgoHelper()

	properties, ok := schema["properties"].(map[string]any)
	if !ok {
		return map[string]any{}
	}
	return properties
}

func schemaPropertyOrderSorted(kind, name string, schema map[string]any) error {
	order := schemaStringSlice(schema["propertyOrder"])
	if len(order) == 0 {
		return nil
	}
	sortedOrder := append([]string{}, order...)
	sort.Strings(sortedOrder)
	if !stringSlicesEqual(order, sortedOrder) {
		return fmt.Errorf("%s schema for %s propertyOrder should be deterministic", kind, name)
	}
	return nil
}

func schemaPropertyHasType(kind, toolName, prop string, property any) error {
	schema, ok := property.(map[string]any)
	if !ok {
		return fmt.Errorf("%s schema for %s.%s should be an object", kind, toolName, prop)
	}
	if _, ok := schema["type"]; ok {
		return nil
	}
	for _, key := range []string{"$ref", "anyOf", "oneOf", "allOf"} {
		if _, ok := schema[key]; ok {
			return nil
		}
	}
	return fmt.Errorf("%s schema for %s.%s should declare a type or combinator", kind, toolName, prop)
}

func rootSchemaHasNoCombinators(kind, name string, schema map[string]any) error {
	for _, key := range []string{"anyOf", "oneOf", "allOf"} {
		if _, ok := schema[key]; ok {
			return fmt.Errorf("%s schema for %s should not use root-level combinator %s", kind, name, key)
		}
	}
	return nil
}

func preciseInputPropertySchemas(toolName string, properties map[string]any) error {
	switch toolName {
	case "wiki_update_page_metadata":
		for _, prop := range []string{"setTags", "addTags", "removeTags", "removeProperties"} {
			if err := stringArrayPropertySchema(toolName, prop, properties[prop]); err != nil {
				return err
			}
		}
		if err := stringMapPropertySchema(toolName, "setProperties", properties["setProperties"]); err != nil {
			return err
		}
	case "wiki_replace_page_section":
		if err := stringArrayPropertySchema(toolName, "headingPath", properties["headingPath"]); err != nil {
			return err
		}
	}
	return nil
}

func stringArrayPropertySchema(toolName, prop string, raw any) error {
	schema, ok := raw.(map[string]any)
	if !ok {
		return fmt.Errorf("input schema for %s.%s should be an object", toolName, prop)
	}
	if schema["type"] != "array" {
		return fmt.Errorf("input schema for %s.%s should be an array", toolName, prop)
	}
	items, ok := schema["items"].(map[string]any)
	if !ok {
		return fmt.Errorf("input schema for %s.%s should declare item schema", toolName, prop)
	}
	if items["type"] != "string" {
		return fmt.Errorf("input schema for %s.%s should contain string items", toolName, prop)
	}
	return nil
}

func stringMapPropertySchema(toolName, prop string, raw any) error {
	schema, ok := raw.(map[string]any)
	if !ok {
		return fmt.Errorf("input schema for %s.%s should be an object", toolName, prop)
	}
	if schema["type"] != "object" {
		return fmt.Errorf("input schema for %s.%s should be an object map", toolName, prop)
	}
	additional, ok := schema["additionalProperties"].(map[string]any)
	if !ok {
		return fmt.Errorf("input schema for %s.%s should declare additional properties", toolName, prop)
	}
	if additional["type"] != "string" {
		return fmt.Errorf("input schema for %s.%s should contain string values", toolName, prop)
	}
	return nil
}

func assertContextHistoryOpaque(contextOut map[string]any) {
	GinkgoHelper()

	for _, raw := range arrayField(contextOut, "contextHistory") {
		Expect(raw).To(BeAssignableToTypeOf(map[string]any{}))
		entry := raw.(map[string]any)
		for _, internal := range []string{"commitHash", "validationHash"} {
			Expect(entry).NotTo(HaveKey(internal))
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

func matchStringSet(want []string) types.GomegaMatcher {
	GinkgoHelper()

	want = append([]string{}, want...)
	sort.Strings(want)
	return WithTransform(func(got []string) []string {
		sorted := append([]string{}, got...)
		sort.Strings(sorted)
		return sorted
	}, Equal(want))
}

func sameStringSet(got, want []string) bool {
	return stringSlicesEqual(sortedStrings(got), sortedStrings(want))
}

func sortedStrings(values []string) []string {
	out := append([]string{}, values...)
	sort.Strings(out)
	return out
}

func stringSlicesEqual(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

func copyToolInputProperties(src map[string][]string) map[string][]string {
	out := make(map[string][]string, len(src))
	for name, props := range src {
		out[name] = append([]string{}, props...)
	}
	return out
}

func matchToolNames(want []string) types.GomegaMatcher {
	GinkgoHelper()

	sortedWant := append([]string{}, want...)
	sort.Strings(sortedWant)
	return Equal(sortedWant)
}

func matchJSONRPCErrorCode(code int64) types.GomegaMatcher {
	GinkgoHelper()

	return Satisfy(func(err error) bool {
		var rpcErr *jsonrpc.Error
		return errors.As(err, &rpcErr) && rpcErr.Code == code
	})
}

func contains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func containsToolProtocolName(values []string, want wikimcp.ToolProtocolName) bool {
	for _, value := range values {
		if wikimcp.ToolProtocolNameFromWireName(value) == want {
			return true
		}
	}
	return false
}

func callToolStructured(session *sdkmcp.ClientSession, name string, args map[string]any) map[string]any {
	GinkgoHelper()

	result, err := session.CallTool(context.Background(), &sdkmcp.CallToolParams{
		Name:      name,
		Arguments: args,
	})
	Expect(err).NotTo(HaveOccurred(), "CallTool %s should return a result", name)
	Expect(result).To(matchSuccessfulToolResultWithStructuredContent(BeAssignableToTypeOf(map[string]any{})), "CallTool %s should return structured content, got error content %#v and structured content %T: %#v", name, result.Content, result.StructuredContent, result.StructuredContent)
	return result.StructuredContent.(map[string]any)
}

type toolMessageOutput struct {
	MessageID wikimcp.ToolMessageID `json:"messageId"`
	Message   string                `json:"message"`
}

func messageOutputFromStructuredContent(value map[string]any) toolMessageOutput {
	GinkgoHelper()

	raw, err := json.Marshal(value)
	Expect(err).NotTo(HaveOccurred(), "message output should marshal")
	var out toolMessageOutput
	Expect(json.Unmarshal(raw, &out)).To(Succeed(), "message output should decode")
	return out
}

type mcpToolErrorResult struct {
	Text      string
	Code      sharederrors.ErrorCode
	MessageID sharederrors.MessageID
	Message   string
	Args      []string
}

type mcpToolErrorPayloadWire struct {
	Code      sharederrors.ErrorCode `json:"code"`
	MessageID sharederrors.MessageID `json:"messageId"`
	Message   string                 `json:"message"`
	Args      []string               `json:"args"`
}

func mcpToolErrorPayloadFromWire(name any, payload map[string]any) mcpToolErrorResult {
	GinkgoHelper()

	raw, err := json.Marshal(payload)
	Expect(err).NotTo(HaveOccurred(), "CallTool %s structured error should marshal", name)
	var typed mcpToolErrorPayloadWire
	Expect(json.Unmarshal(raw, &typed)).To(Succeed(), "CallTool %s structured error should decode", name)
	return mcpToolErrorResult{
		Code:      typed.Code,
		MessageID: typed.MessageID,
		Message:   typed.Message,
		Args:      typed.Args,
	}
}

func callToolStructuredError(session *sdkmcp.ClientSession, name any, args map[string]any) mcpToolErrorResult {
	GinkgoHelper()

	return callToolErrorResult(session, name, args)
}

func matchMCPStructuredError(code sharederrors.ErrorCode, messageID sharederrors.MessageID) types.GomegaMatcher {
	GinkgoHelper()

	return testmatchers.HaveMCPStructuredError(code, messageID)
}

func callToolErrorResult(session *sdkmcp.ClientSession, name any, args map[string]any) mcpToolErrorResult {
	GinkgoHelper()

	switch typed := name.(type) {
	case wikimcp.ToolID:
		return callToolErrorResultByToolID(session, typed, args)
	case string:
		result, err := session.CallTool(context.Background(), &sdkmcp.CallToolParams{
			Name:      typed,
			Arguments: args,
		})
		Expect(err).NotTo(HaveOccurred(), "CallTool %s should return a tool result", name)
		return toolErrorResultFromCallResult(name, result)
	default:
		result, err := session.CallTool(context.Background(), &sdkmcp.CallToolParams{
			Name:      fmt.Sprint(typed),
			Arguments: args,
		})
		Expect(err).NotTo(HaveOccurred(), "CallTool %s should return a tool result", name)
		return toolErrorResultFromCallResult(name, result)
	}
}

func callToolErrorResultByToolID(session *sdkmcp.ClientSession, name wikimcp.ToolID, args map[string]any) mcpToolErrorResult {
	GinkgoHelper()

	result, err := session.CallTool(context.Background(), &sdkmcp.CallToolParams{
		Name:      name.String(),
		Arguments: args,
	})
	Expect(err).NotTo(HaveOccurred(), "CallTool %s should return a tool result", name)
	return toolErrorResultFromCallResult(name, result)
}

func toolErrorResultFromCallResult(name any, result *sdkmcp.CallToolResult) mcpToolErrorResult {
	GinkgoHelper()

	Expect(result).NotTo(BeNil(), "CallTool %s should return a tool result", name)

	out := mcpToolErrorResult{}
	for _, content := range result.Content {
		if text, ok := content.(*sdkmcp.TextContent); ok {
			out.Text = text.Text
			break
		}
	}
	Expect(out.Text).NotTo(BeEmpty(), "CallTool %s should return text error content", name)
	errorPayload, err := structuredToolErrorPayload(result)
	Expect(err).To(Succeed(), "CallTool %s should expose a structured error payload", name)
	decoded := mcpToolErrorPayloadFromWire(name, errorPayload)
	out.Code = decoded.Code
	out.MessageID = decoded.MessageID
	out.Message = decoded.Message
	out.Args = decoded.Args
	return out
}

func structuredToolErrorPayload(result *sdkmcp.CallToolResult) (map[string]any, error) {
	GinkgoHelper()

	if result == nil {
		return nil, fmt.Errorf("missing tool result")
	}
	if payload, ok := result.Meta["error"].(map[string]any); ok {
		return payload, nil
	}
	if payload, ok := result.StructuredContent.(map[string]any); ok {
		if errorPayload, ok := payload["error"].(map[string]any); ok {
			return errorPayload, nil
		}
		return payload, nil
	}
	return nil, fmt.Errorf("missing structured tool error payload")
}

func callToolProtocolError(session *sdkmcp.ClientSession, name string, args map[string]any) error {
	GinkgoHelper()

	_, err := session.CallTool(context.Background(), &sdkmcp.CallToolParams{
		Name:      name,
		Arguments: args,
	})
	return err
}

func getHTTPPageByPath(router http.Handler, path string) map[string]any {
	GinkgoHelper()

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/pages/by-path?path="+path, nil))
	Expect(rec).To(HaveHTTPStatus(http.StatusOK), rec.Body.String())
	var out map[string]any
	Expect(json.Unmarshal(rec.Body.Bytes(), &out)).To(Succeed(), "decode HTTP page by path %q", path)
	return out
}

func getHTTPPageByID(router http.Handler, pageID string) map[string]any {
	GinkgoHelper()

	return getHTTPMap(router, "/api/pages/"+pageID)
}

func getHTTPValue(router http.Handler, path string) any {
	GinkgoHelper()

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	Expect(rec).To(HaveHTTPStatus(http.StatusOK), rec.Body.String())
	return decodeJSONValue("GET "+path, rec.Body.Bytes())
}

func getHTTPStatus(router http.Handler, path string, wantStatus int) string {
	GinkgoHelper()

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	Expect(rec).To(HaveHTTPStatus(wantStatus), rec.Body.String())
	return rec.Body.String()
}

func getHTTPMap(router http.Handler, path string) map[string]any {
	GinkgoHelper()

	value := getHTTPValue(router, path)
	Expect(value).To(BeAssignableToTypeOf(map[string]any{}), "GET %s should decode as object", path)
	return value.(map[string]any)
}

func updateHTTPPage(router http.Handler, pageID string, payload map[string]any) map[string]any {
	GinkgoHelper()

	body, err := json.Marshal(payload)
	Expect(err).NotTo(HaveOccurred())
	csrfToken, csrfCookies := issueHTTPCSRF(router)
	req := httptest.NewRequest(http.MethodPut, "/api/pages/"+pageID, strings.NewReader(string(body)))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-CSRF-Token", csrfToken)
	for _, cookie := range csrfCookies {
		req.AddCookie(cookie)
	}
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	Expect(rec).To(HaveHTTPStatus(http.StatusOK), rec.Body.String())
	var out map[string]any
	Expect(json.Unmarshal(rec.Body.Bytes(), &out)).To(Succeed(), "decode HTTP page update %q", pageID)
	return out
}

func putHTTPJSON(router http.Handler, path string, payload map[string]any, wantStatus int) map[string]any {
	GinkgoHelper()

	return requestHTTPJSON(router, http.MethodPut, path, payload, wantStatus)
}

func putHTTPJSONBody(router http.Handler, path string, payload map[string]any, wantStatus int) string {
	GinkgoHelper()

	return requestHTTPJSONBody(router, http.MethodPut, path, payload, wantStatus)
}

func postHTTPJSON(router http.Handler, path string, payload map[string]any, wantStatus int) map[string]any {
	GinkgoHelper()

	return requestHTTPJSON(router, http.MethodPost, path, payload, wantStatus)
}

func postHTTPJSONBody(router http.Handler, path string, payload map[string]any, wantStatus int) string {
	GinkgoHelper()

	return requestHTTPJSONBody(router, http.MethodPost, path, payload, wantStatus)
}

func postHTTPJSONNoContent(router http.Handler, path string, payload map[string]any, wantStatus int) {
	GinkgoHelper()

	body := postHTTPJSONBody(router, path, payload, wantStatus)
	Expect(strings.TrimSpace(body)).To(BeEmpty())
}

func requestHTTPJSON(router http.Handler, method, path string, payload map[string]any, wantStatus int) map[string]any {
	GinkgoHelper()

	raw := requestHTTPJSONBody(router, method, path, payload, wantStatus)
	return decodeJSONMap(method+" "+path, []byte(raw))
}

func requestHTTPJSONBody(router http.Handler, method, path string, payload map[string]any, wantStatus int) string {
	GinkgoHelper()

	body, err := json.Marshal(payload)
	Expect(err).NotTo(HaveOccurred())
	csrfToken, csrfCookies := issueHTTPCSRF(router)
	req := httptest.NewRequest(method, path, strings.NewReader(string(body)))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-CSRF-Token", csrfToken)
	for _, cookie := range csrfCookies {
		req.AddCookie(cookie)
	}
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	Expect(rec).To(HaveHTTPStatus(wantStatus), rec.Body.String())
	return rec.Body.String()
}

func decodeJSONMap(label string, raw []byte) map[string]any {
	GinkgoHelper()

	value := decodeJSONValue(label, raw)
	Expect(value).To(BeAssignableToTypeOf(map[string]any{}), "%s should decode to a JSON object", label)
	return value.(map[string]any)
}

func deleteHTTPStatus(router http.Handler, path string, wantStatus int) string {
	GinkgoHelper()

	csrfToken, csrfCookies := issueHTTPCSRF(router)
	req := httptest.NewRequest(http.MethodDelete, path, nil)
	req.Header.Set("X-CSRF-Token", csrfToken)
	for _, cookie := range csrfCookies {
		req.AddCookie(cookie)
	}
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	Expect(rec).To(HaveHTTPStatus(wantStatus), rec.Body.String())
	return rec.Body.String()
}

func getHTTPSearch(router http.Handler, values url.Values) map[string]any {
	GinkgoHelper()

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/search?"+values.Encode(), nil))
	Expect(rec).To(HaveHTTPStatus(http.StatusOK), rec.Body.String())
	return decodeJSONMap("GET /api/search", rec.Body.Bytes())
}

func uploadHTTPAsset(router http.Handler, pageID, filename string, content []byte, wantStatus int) map[string]any {
	GinkgoHelper()

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("file", filename)
	Expect(err).NotTo(HaveOccurred())
	_, err = part.Write(content)
	Expect(err).NotTo(HaveOccurred())
	Expect(writer.Close()).To(Succeed())

	csrfToken, csrfCookies := issueHTTPCSRF(router)
	req := httptest.NewRequest(http.MethodPost, "/api/pages/"+pageID+"/assets", &body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("X-CSRF-Token", csrfToken)
	for _, cookie := range csrfCookies {
		req.AddCookie(cookie)
	}
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	Expect(rec).To(HaveHTTPStatus(wantStatus), rec.Body.String())
	return decodeJSONMap("POST asset "+pageID+"/"+filename, rec.Body.Bytes())
}

func getHTTPAssets(router http.Handler, pageID string) map[string]any {
	GinkgoHelper()

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/pages/"+pageID+"/assets", nil))
	Expect(rec).To(HaveHTTPStatus(http.StatusOK), rec.Body.String())
	return decodeJSONMap("GET asset list for page "+pageID, rec.Body.Bytes())
}

func getHTTPAsset(router http.Handler, pageID, filename string) string {
	GinkgoHelper()

	body, _ := getHTTPAssetWithContentType(router, pageID, filename)
	return body
}

func getHTTPAssetWithContentType(router http.Handler, pageID, filename string) (string, string) {
	GinkgoHelper()

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/assets/"+pageID+"/"+url.PathEscape(filename), nil))
	Expect(rec).To(HaveHTTPStatus(http.StatusOK), rec.Body.String())
	return rec.Body.String(), rec.Header().Get("Content-Type")
}

func getHTTPLatestRevision(router http.Handler, pageID string) map[string]any {
	GinkgoHelper()

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/pages/"+pageID+"/revisions/latest", nil))
	Expect(rec).To(HaveHTTPStatus(http.StatusOK), rec.Body.String())
	return decodeJSONMap("GET latest revision for page "+pageID, rec.Body.Bytes())
}

func getHTTPRevision(router http.Handler, pageID, revisionID string) map[string]any {
	GinkgoHelper()

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/pages/"+pageID+"/revisions/"+revisionID, nil))
	Expect(rec).To(HaveHTTPStatus(http.StatusOK), rec.Body.String())
	return decodeJSONMap("GET revision "+pageID+"/"+revisionID, rec.Body.Bytes())
}

func matchSearchResults(httpSearch map[string]any) types.GomegaMatcher {
	GinkgoHelper()

	return gcustom.MakeMatcher(func(mcpSearch map[string]any) (bool, error) {
		for _, field := range []string{"count", "offset", "limit"} {
			if !jsonValuesEqual(mcpSearch[field], httpSearch[field]) {
				return false, nil
			}
		}
		if !jsonValuesEqual(arrayFieldFromMap(mcpSearch, "items"), arrayFieldFromMap(httpSearch, "items")) {
			return false, nil
		}
		if !jsonValuesEqual(mcpSearch["tagFacets"], httpSearch["tag_facets"]) {
			return false, nil
		}
		count, countOK := httpSearch["count"].(float64)
		offset, offsetOK := httpSearch["offset"].(float64)
		if !countOK || !offsetOK {
			return false, fmt.Errorf("HTTP search response should expose numeric count and offset")
		}
		wantHasMore := int(offset)+len(arrayFieldFromMap(httpSearch, "items")) < int(count)
		return mcpSearch["hasMore"] == wantHasMore, nil
	}).WithMessage("match HTTP search result semantics")
}

func matchMapFields(want map[string]any, fields []string) types.GomegaMatcher {
	GinkgoHelper()

	return gcustom.MakeMatcher(func(got map[string]any) (bool, error) {
		for _, field := range fields {
			if !jsonValuesEqual(got[field], want[field]) {
				return false, nil
			}
		}
		return true, nil
	}).WithMessage("match selected map fields").WithTemplateData(fields)
}

func matchRestoredMetadata() types.GomegaMatcher {
	GinkgoHelper()

	return SatisfyAll(
		HaveKeyWithValue("content", "metadata revision\n"),
		WithTransform(func(page map[string]any) []string {
			return stringSliceField(page, "tags")
		}, matchStringSet([]string{"restore", "metadata"})),
		HaveKeyWithValue("properties", HaveKeyWithValue("status", "archived")),
	)
}

func matchMCPPageError(code sharederrors.ErrorCode, messageID sharederrors.MessageID) types.GomegaMatcher {
	GinkgoHelper()

	return SatisfyAll(
		testmatchers.HaveMCPStructuredError(code, messageID),
		HaveField("Message", Not(BeEmpty())),
	)
}

func matchHTTPPageError(code sharederrors.ErrorCode, messageID sharederrors.MessageID) types.GomegaMatcher {
	GinkgoHelper()

	return WithTransform(func(body string) httpPageErrorPayloadWire {
		payload := decodeJSONMap("HTTP page error", []byte(body))
		errPayload := nestedMap(payload, "error")
		return httpPageErrorPayloadFromWire("HTTP page error", errPayload)
	}, SatisfyAll(
		testmatchers.HaveStructuredError(code, messageID),
		HaveField("Message", Not(BeEmpty())),
		HaveField("Template", Not(BeEmpty())),
	))
}

func matchMCPPageVersionConflict() types.GomegaMatcher {
	GinkgoHelper()

	return matchMCPPageError(wikipages.ErrCodePageVersionConflict, sharederrors.MessageIDForCode(wikipages.ErrCodePageVersionConflict))
}

func matchHTTPPageVersionConflict() types.GomegaMatcher {
	GinkgoHelper()

	return matchHTTPPageError(wikipages.ErrCodePageVersionConflict, sharederrors.MessageIDForCode(wikipages.ErrCodePageVersionConflict))
}

func matchRestoreVolatileFields() types.GomegaMatcher {
	GinkgoHelper()

	return gcustom.MakeMatcher(func(page map[string]any) (bool, error) {
		if stringField(page, "version") == "" {
			return false, nil
		}
		updatedAt := stringField(nestedMap(page, "metadata"), "updatedAt")
		if _, err := time.Parse(time.RFC3339, updatedAt); err != nil {
			return false, err
		}
		return true, nil
	}).WithMessage("expose restore volatile fields")
}

func matchPageState(id, title, slug, pathValue, kind, parentID string) types.GomegaMatcher {
	GinkgoHelper()

	return SatisfyAll(
		HaveKeyWithValue("id", id),
		HaveKeyWithValue("title", title),
		HaveKeyWithValue("slug", slug),
		HaveKeyWithValue("path", pathValue),
		HaveKeyWithValue("kind", kind),
		WithTransform(func(page map[string]any) string {
			return stringValue(page["parentId"])
		}, Equal(parentID)),
	)
}

func matchChildOrder(childIDs ...string) types.GomegaMatcher {
	GinkgoHelper()

	return HaveKeyWithValue("children", HaveExactElements(childIDMatchers(childIDs)...))
}

func matchChildrenExcludingIDs(childIDs ...string) types.GomegaMatcher {
	GinkgoHelper()

	disallowed := map[string]struct{}{}
	for _, id := range childIDs {
		disallowed[id] = struct{}{}
	}
	return gcustom.MakeMatcher(func(page map[string]any) (bool, error) {
		for _, raw := range arrayFieldFromMap(page, "children") {
			child, ok := raw.(map[string]any)
			if !ok {
				return false, fmt.Errorf("child should be an object")
			}
			if _, denied := disallowed[stringValue(child["id"])]; denied {
				return false, nil
			}
		}
		return true, nil
	}).WithMessage("exclude moved child pages").WithTemplateData(childIDs)
}

func childIDMatchers(childIDs []string) []any {
	matchers := make([]any, 0, len(childIDs))
	for _, id := range childIDs {
		matchers = append(matchers, HaveKeyWithValue("id", id))
	}
	return matchers
}

func canonicalPageMarkdown(label, raw string) markdown.PageDocument {
	GinkgoHelper()

	Expect(raw).To(HavePrefix("<!-- leafwiki\n"), "%s should start with canonical LeafWiki metadata", label)
	Expect(raw).NotTo(HavePrefix("---\n"), "%s should not start with legacy YAML frontmatter", label)
	doc, _, err := markdown.ParsePageDocument(raw)
	Expect(err).NotTo(HaveOccurred(), "%s should parse with ParsePageDocument", label)
	return doc
}

func matchAssetURLResult(field, pageID string) types.GomegaMatcher {
	GinkgoHelper()

	prefix := "/assets/" + pageID + "/"
	return SatisfyAll(
		HaveKeyWithValue(field, SatisfyAll(HavePrefix(prefix), Not(Equal(prefix)))),
		HaveLen(1),
	)
}

func matchScopedSuccessPayload(httpPayload map[string]any, mcpMessageID, httpMessageID string) types.GomegaMatcher {
	GinkgoHelper()

	httpComparable := mapWithoutField(httpPayload, "messageId")
	return gcustom.MakeMatcher(func(mcpPayload map[string]any) (bool, error) {
		if !jsonValuesEqual(mcpPayload["messageId"], mcpMessageID) {
			return false, nil
		}
		if !jsonValuesEqual(httpPayload["messageId"], httpMessageID) {
			return false, nil
		}
		return jsonValuesEqual(mapWithoutField(mcpPayload, "messageId"), httpComparable), nil
	}).WithMessage("match scoped MCP and HTTP success payloads")
}

func matchLookupFinalID(wantID string, wantKind string) types.GomegaMatcher {
	GinkgoHelper()

	return gcustom.MakeMatcher(func(lookup map[string]any) (bool, error) {
		segments := arrayFieldFromMap(lookup, "segments")
		if len(segments) == 0 {
			return false, nil
		}
		final, ok := segments[len(segments)-1].(map[string]any)
		if !ok {
			return false, fmt.Errorf("final lookup segment should be an object")
		}
		return stringValue(final["id"]) == wantID && stringValue(final["kind"]) == wantKind, nil
	}).WithMessage("end at the requested lookup segment")
}

func matchValidationIssueCodes(wantCodes []wikivalidation.IssueCode) types.GomegaMatcher {
	GinkgoHelper()

	return gcustom.MakeMatcher(func(output map[string]any) (bool, error) {
		codes, err := validationIssueCodes(output)
		if err != nil {
			return false, err
		}
		for _, want := range wantCodes {
			if !containsIssueCode(codes, want) {
				return false, nil
			}
		}
		return true, nil
	}).WithMessage("contain validation issue codes").WithTemplateData(wantCodes)
}

func jsonValuesEqual(got, want any) bool {
	return reflect.DeepEqual(normalizeJSON(got), normalizeJSON(want))
}

type httpPageErrorPayloadWire struct {
	Code      sharederrors.ErrorCode `json:"code"`
	MessageID sharederrors.MessageID `json:"messageId"`
	Message   string                 `json:"message"`
	Template  string                 `json:"template"`
}

func httpPageErrorPayloadFromWire(label string, payload map[string]any) httpPageErrorPayloadWire {
	GinkgoHelper()

	raw, err := json.Marshal(payload)
	Expect(err).NotTo(HaveOccurred(), "%s error payload should marshal", label)
	var decoded httpPageErrorPayloadWire
	Expect(json.Unmarshal(raw, &decoded)).To(Succeed(), "%s error payload should decode", label)
	return decoded
}

func normalizeRestorePayload(page map[string]any) map[string]any {
	GinkgoHelper()

	normalizedValue := normalizeJSON(page)
	Expect(normalizedValue).To(BeAssignableToTypeOf(map[string]any{}), "restore payload should normalize to object")
	normalized := normalizedValue.(map[string]any)
	delete(normalized, "version")
	if metadata, ok := normalized["metadata"].(map[string]any); ok {
		delete(metadata, "updatedAt")
	}
	return normalized
}

func readPageMarkdownByRoutePath(rootDir, routePath string) string {
	GinkgoHelper()

	path := filepath.Join(append([]string{rootDir}, strings.Split(routePath, "/")...)...) + ".md"
	raw, err := os.ReadFile(path)
	Expect(err).NotTo(HaveOccurred(), "read markdown page %s", path)
	return string(raw)
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

func matchJSONEqual(want any) types.GomegaMatcher {
	GinkgoHelper()

	normalizedWant := normalizeJSON(want)
	return WithTransform(func(got any) any {
		return normalizeJSON(got)
	}, Equal(normalizedWant))
}

func mapWithoutField(payload map[string]any, field string) map[string]any {
	out := make(map[string]any, len(payload))
	for key, value := range payload {
		if key == field {
			continue
		}
		out[key] = value
	}
	return out
}

func normalizeJSON(value any) any {
	GinkgoHelper()

	raw, err := json.Marshal(value)
	Expect(err).NotTo(HaveOccurred())
	return decodeJSONValue("normalize JSON", raw)
}

func decodeJSONValue(label string, raw []byte) any {
	GinkgoHelper()

	var out any
	Expect(json.Unmarshal(raw, &out)).To(Succeed(), "decode %s", label)
	return out
}

func issueHTTPCSRF(router http.Handler) (string, []*http.Cookie) {
	GinkgoHelper()

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/config", nil))
	Expect(rec).To(HaveHTTPStatus(http.StatusOK), rec.Body.String())
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
	Expect(token).NotTo(BeEmpty())
	return token, result.Cookies()
}

func nestedMap(value map[string]any, key string) map[string]any {
	GinkgoHelper()

	Expect(value).To(HaveKeyWithValue(key, BeAssignableToTypeOf(map[string]any{})))
	return value[key].(map[string]any)
}

func stringField(value map[string]any, key string) string {
	GinkgoHelper()

	Expect(value).To(HaveKeyWithValue(key, SatisfyAll(BeAssignableToTypeOf(""), Not(BeEmpty()))))
	return value[key].(string)
}

func arrayField(value map[string]any, key string) []any {
	GinkgoHelper()

	return arrayFieldFromMap(value, key)
}

func arrayFieldFromMap(value map[string]any, key string) []any {
	GinkgoHelper()

	Expect(value).To(HaveKeyWithValue(key, BeAssignableToTypeOf([]any{})))
	return value[key].([]any)
}

func stringSliceField(value map[string]any, key string) []string {
	GinkgoHelper()

	raw := arrayField(value, key)
	out := make([]string, 0, len(raw))
	for _, item := range raw {
		Expect(item).To(BeAssignableToTypeOf(""))
		out = append(out, item.(string))
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

func objectWithField(value any, field string, want any) map[string]any {
	GinkgoHelper()

	Expect(value).To(BeAssignableToTypeOf([]any{}), "value should be an array")
	items := value.([]any)
	for _, item := range items {
		obj, ok := item.(map[string]any)
		if ok && obj[field] == want {
			return obj
		}
	}
	Expect(items).To(ContainElement(HaveKeyWithValue(field, want)), "array should include object with %s=%#v", field, want)
	return nil
}

func changedPathsFromContext(output map[string]any) map[string]bool {
	GinkgoHelper()

	return changedPathsFromChanges(arrayField(output, "changesSincePreviousContext"))
}

func changedPathsFromChanges(changes []any) map[string]bool {
	GinkgoHelper()

	paths := map[string]bool{}
	for _, raw := range changes {
		Expect(raw).To(BeAssignableToTypeOf(map[string]any{}))
		change := raw.(map[string]any)
		for _, value := range arrayFieldFromMap(change, "changedPaths") {
			Expect(value).To(BeAssignableToTypeOf(""))
			paths[value.(string)] = true
		}
	}
	return paths
}

type validationIssueWire struct {
	Code wikivalidation.IssueCode `json:"code"`
	Path string                   `json:"path"`
}

func validationIssueFromWire(item map[string]any) validationIssueWire {
	GinkgoHelper()

	raw, err := json.Marshal(item)
	Expect(err).NotTo(HaveOccurred(), "validation issue should marshal")
	var decoded validationIssueWire
	Expect(json.Unmarshal(raw, &decoded)).To(Succeed(), "validation issue should decode")
	return decoded
}

func validationIssueByCode(output map[string]any, wantCode wikivalidation.IssueCode) map[string]any {
	GinkgoHelper()

	issues := arrayField(output, "issues")
	Expect(issues).To(ContainElement(Satisfy(func(issue any) bool {
		item, ok := issue.(map[string]any)
		return ok && validationIssueFromWire(item).Code == wantCode
	})))
	matches := []map[string]any{}
	for _, issue := range issues {
		Expect(issue).To(BeAssignableToTypeOf(map[string]any{}))
		item := issue.(map[string]any)
		if validationIssueFromWire(item).Code == wantCode {
			matches = append(matches, item)
		}
	}
	return matches[0]
}

func assertNoValidationIssuePath(output map[string]any, unwantedPath string) {
	GinkgoHelper()

	issues := arrayField(output, "issues")
	paths := []string{}
	for _, issue := range issues {
		Expect(issue).To(BeAssignableToTypeOf(map[string]any{}))
		paths = append(paths, validationIssueFromWire(issue.(map[string]any)).Path)
	}
	Expect(paths).NotTo(ContainElement(unwantedPath))
}

func matchValidationIssueCodesAbsent(absentCodes []wikivalidation.IssueCode) types.GomegaMatcher {
	GinkgoHelper()

	absent := make(map[wikivalidation.IssueCode]struct{}, len(absentCodes))
	for _, code := range absentCodes {
		absent[code] = struct{}{}
	}
	return gcustom.MakeMatcher(func(output map[string]any) (bool, error) {
		codes, err := validationIssueCodes(output)
		if err != nil {
			return false, err
		}
		for _, code := range codes {
			if _, forbidden := absent[code]; forbidden {
				return false, nil
			}
		}
		return true, nil
	}).WithMessage("exclude validation issue codes").WithTemplateData(absentCodes)
}

func validationIssueCodes(output map[string]any) ([]wikivalidation.IssueCode, error) {
	rawIssues, ok := output["issues"].([]any)
	if !ok {
		return nil, fmt.Errorf("validation output should expose issues as an array")
	}
	codes := make([]wikivalidation.IssueCode, 0, len(rawIssues))
	for _, rawIssue := range rawIssues {
		issue, ok := rawIssue.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("validation issue should be an object")
		}
		payload, err := json.Marshal(issue)
		if err != nil {
			return nil, fmt.Errorf("marshal validation issue: %w", err)
		}
		var decoded validationIssueWire
		if err := json.Unmarshal(payload, &decoded); err != nil {
			return nil, fmt.Errorf("decode validation issue: %w", err)
		}
		codes = append(codes, decoded.Code)
	}
	return codes, nil
}

func containsIssueCode(codes []wikivalidation.IssueCode, want wikivalidation.IssueCode) bool {
	for _, code := range codes {
		if code == want {
			return true
		}
	}
	return false
}

func validationIssueCodeCount(output map[string]any, wantCode wikivalidation.IssueCode, wantPath string) int {
	GinkgoHelper()

	issues := arrayField(output, "issues")
	count := 0
	for _, issue := range issues {
		Expect(issue).To(BeAssignableToTypeOf(map[string]any{}))
		decoded := validationIssueFromWire(issue.(map[string]any))
		if decoded.Code == wantCode && decoded.Path == wantPath {
			count++
		}
	}
	return count
}

func assertRecentChangesIncludePath(output map[string]any, wantPath string) {
	GinkgoHelper()

	Expect(changedPathsFromChanges(arrayField(output, "recentChanges"))).To(HaveKey(wantPath))
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
