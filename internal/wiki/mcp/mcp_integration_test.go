package mcp_test

import (
	"sort"
	"sync"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/types"
	wikimcp "github.com/perber/wiki/internal/wiki/mcp"
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
	return WithTransform(func(seen map[string]map[string]struct{}) parityCoverageObservation {
		return parityCoverageObservationFor(seen, seenCases, expected)
	}, matchParityCoverageComplete())
}

type parityCoverageState string

const (
	parityCoverageComplete parityCoverageState = "complete"
	parityCoverageMissing  parityCoverageState = "missing"
)

type parityCoverageObservation struct {
	State   parityCoverageState
	Missing []string
}

func parityCoverageObservationFor(seen map[string]map[string]struct{}, seenCases map[string]httpMCPParityCase, expected []string) parityCoverageObservation {
	missing := make([]string, 0)
	for _, name := range expected {
		tc, ok := seenCases[name]
		if !ok {
			missing = append(missing, name)
			continue
		}
		if _, exercised := seen[name][tc.HTTPRoute]; !exercised {
			missing = append(missing, name+" "+tc.HTTPRoute)
		}
	}
	state := parityCoverageComplete
	if len(missing) > 0 {
		state = parityCoverageMissing
	}
	return parityCoverageObservation{State: state, Missing: missing}
}

func matchParityCoverageComplete() types.GomegaMatcher {
	GinkgoHelper()
	return SatisfyAll(
		HaveField("State", Equal(parityCoverageComplete)),
		HaveField("Missing", BeEmpty()),
	)
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
