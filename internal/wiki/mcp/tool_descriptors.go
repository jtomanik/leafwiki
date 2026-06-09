package mcp

// ToolDescriptor is the single source for an MCP tool's protocol name and
// user-facing description.
type ToolDescriptor struct {
	Name        string
	Description string
}

const (
	ToolGetConfig          = "wiki_get_config"
	ToolGetCurrentUser     = "wiki_get_current_user"
	ToolGetContext         = "wiki_get_context"
	ToolRefresh            = "wiki_refresh"
	ToolGetSubtree         = "wiki_get_subtree"
	ToolValidatePage       = "wiki_validate_page"
	ToolValidateContent    = "wiki_validate_content"
	ToolValidateWiki       = "wiki_validate_wiki"
	ToolUpdatePageMetadata = "wiki_update_page_metadata"
	ToolReplacePageSection = "wiki_replace_page_section"
	ToolGetTree            = "wiki_get_tree"
	ToolGetPage            = "wiki_get_page"
	ToolGetPageByPath      = "wiki_get_page_by_path"
	ToolLookupPath         = "wiki_lookup_path"
	ToolResolvePermalink   = "wiki_resolve_permalink"
	ToolSuggestSlug        = "wiki_suggest_slug"
	ToolCreatePage         = "wiki_create_page"
	ToolUpdatePage         = "wiki_update_page"
	ToolDeletePage         = "wiki_delete_page"
	ToolMovePage           = "wiki_move_page"
	ToolSortPages          = "wiki_sort_pages"
	ToolEnsurePage         = "wiki_ensure_page"
	ToolConvertPage        = "wiki_convert_page"
	ToolCopyPage           = "wiki_copy_page"
	ToolSearchPages        = "wiki_search_pages"
	ToolGetSearchStatus    = "wiki_get_search_status"
	ToolListTags           = "wiki_list_tags"
	ToolGetPagesByTags     = "wiki_get_pages_by_tags"
	ToolListPropertyKeys   = "wiki_list_property_keys"
	ToolGetPagesByProperty = "wiki_get_pages_by_property"
	ToolGetLinkStatus      = "wiki_get_link_status"
	ToolUploadAsset        = "wiki_upload_asset"
	ToolGetAsset           = "wiki_get_asset"
	ToolListAssets         = "wiki_list_assets"
	ToolRenameAsset        = "wiki_rename_asset"
	ToolDeleteAsset        = "wiki_delete_asset"
	ToolListRevisions      = "wiki_list_revisions"
	ToolGetLatestRevision  = "wiki_get_latest_revision"
	ToolGetRevision        = "wiki_get_revision"
	ToolCompareRevisions   = "wiki_compare_revisions"
	ToolGetRevisionAsset   = "wiki_get_revision_asset"
	ToolRestoreRevision    = "wiki_restore_revision"
	ToolPreviewRefactor    = "wiki_preview_page_refactor"
	ToolApplyRefactor      = "wiki_apply_page_refactor"
)

var (
	toolGetConfig          = ToolDescriptor{Name: ToolGetConfig, Description: "Return local MCP-visible LeafWiki configuration"}
	toolGetCurrentUser     = ToolDescriptor{Name: ToolGetCurrentUser, Description: "Return the effective MCP user"}
	toolGetContext         = ToolDescriptor{Name: ToolGetContext, Description: "Return agent-ready wiki context, sync state, recent changes, and presence"}
	toolRefresh            = ToolDescriptor{Name: ToolRefresh, Description: "Synchronize direct Markdown changes into LeafWiki state"}
	toolGetSubtree         = ToolDescriptor{Name: ToolGetSubtree, Description: "Return a compact subtree rooted at a page, path, or the wiki root"}
	toolValidatePage       = ToolDescriptor{Name: ToolValidatePage, Description: "Validate an existing page by page ID or path"}
	toolValidateContent    = ToolDescriptor{Name: ToolValidateContent, Description: "Validate proposed Markdown content without writing it"}
	toolValidateWiki       = ToolDescriptor{Name: ToolValidateWiki, Description: "Validate the current wiki state"}
	toolUpdatePageMetadata = ToolDescriptor{Name: ToolUpdatePageMetadata, Description: "Safely patch page tags and properties without changing body content"}
	toolReplacePageSection = ToolDescriptor{Name: ToolReplacePageSection, Description: "Safely replace Markdown under a target heading"}
	toolGetTree            = ToolDescriptor{Name: ToolGetTree, Description: "Return the wiki page tree"}
	toolGetPage            = ToolDescriptor{Name: ToolGetPage, Description: "Return a page by ID with link status context"}
	toolGetPageByPath      = ToolDescriptor{Name: ToolGetPageByPath, Description: "Return a page by route path with link status context"}
	toolLookupPath         = ToolDescriptor{Name: ToolLookupPath, Description: "Resolve a route path into existing and missing path segments"}
	toolResolvePermalink   = ToolDescriptor{Name: ToolResolvePermalink, Description: "Resolve a stable page ID to its current route path"}
	toolSuggestSlug        = ToolDescriptor{Name: ToolSuggestSlug, Description: "Suggest a unique child slug for a title"}
	toolCreatePage         = ToolDescriptor{Name: ToolCreatePage, Description: "Create a wiki page or section"}
	toolUpdatePage         = ToolDescriptor{Name: ToolUpdatePage, Description: "Update page title, slug, content, tags, and properties"}
	toolDeletePage         = ToolDescriptor{Name: ToolDeletePage, Description: "Delete a page"}
	toolMovePage           = ToolDescriptor{Name: ToolMovePage, Description: "Move a page to a new parent"}
	toolSortPages          = ToolDescriptor{Name: ToolSortPages, Description: "Sort a parent's child pages"}
	toolEnsurePage         = ToolDescriptor{Name: ToolEnsurePage, Description: "Ensure a page exists at a route path"}
	toolConvertPage        = ToolDescriptor{Name: ToolConvertPage, Description: "Convert a page between page and section kinds"}
	toolCopyPage           = ToolDescriptor{Name: ToolCopyPage, Description: "Copy a page and its assets"}
	toolSearchPages        = ToolDescriptor{Name: ToolSearchPages, Description: "Search pages using LeafWiki offset and limit pagination"}
	toolGetSearchStatus    = ToolDescriptor{Name: ToolGetSearchStatus, Description: "Return the search indexing status"}
	toolListTags           = ToolDescriptor{Name: ToolListTags, Description: "List tag counts"}
	toolGetPagesByTags     = ToolDescriptor{Name: ToolGetPagesByTags, Description: "List pages matching all tags"}
	toolListPropertyKeys   = ToolDescriptor{Name: ToolListPropertyKeys, Description: "List property key counts"}
	toolGetPagesByProperty = ToolDescriptor{Name: ToolGetPagesByProperty, Description: "List pages with a property value"}
	toolGetLinkStatus      = ToolDescriptor{Name: ToolGetLinkStatus, Description: "Return link status for a page"}
	toolUploadAsset        = ToolDescriptor{Name: ToolUploadAsset, Description: "Upload an asset from base64 content"}
	toolGetAsset           = ToolDescriptor{Name: ToolGetAsset, Description: "Read an asset as base64 content"}
	toolListAssets         = ToolDescriptor{Name: ToolListAssets, Description: "List page assets"}
	toolRenameAsset        = ToolDescriptor{Name: ToolRenameAsset, Description: "Rename a page asset"}
	toolDeleteAsset        = ToolDescriptor{Name: ToolDeleteAsset, Description: "Delete a page asset"}
	toolListRevisions      = ToolDescriptor{Name: ToolListRevisions, Description: "List page revisions"}
	toolGetLatestRevision  = ToolDescriptor{Name: ToolGetLatestRevision, Description: "Get the latest page revision"}
	toolGetRevision        = ToolDescriptor{Name: ToolGetRevision, Description: "Get a page revision snapshot"}
	toolCompareRevisions   = ToolDescriptor{Name: ToolCompareRevisions, Description: "Compare two page revisions"}
	toolGetRevisionAsset   = ToolDescriptor{Name: ToolGetRevisionAsset, Description: "Read a revision asset as base64 content"}
	toolRestoreRevision    = ToolDescriptor{Name: ToolRestoreRevision, Description: "Restore a page revision"}
	toolPreviewRefactor    = ToolDescriptor{Name: ToolPreviewRefactor, Description: "Preview a page rename or move refactor"}
	toolApplyRefactor      = ToolDescriptor{Name: ToolApplyRefactor, Description: "Apply a page rename or move refactor"}
)

var baseToolDescriptors = []ToolDescriptor{
	toolGetContext, toolGetSubtree, toolValidatePage, toolValidateContent,
	toolValidateWiki, toolUpdatePageMetadata, toolReplacePageSection,
	toolGetConfig, toolGetCurrentUser, toolGetTree, toolGetPage, toolGetPageByPath,
	toolLookupPath, toolResolvePermalink, toolSuggestSlug, toolCreatePage,
	toolUpdatePage, toolDeletePage, toolMovePage, toolSortPages, toolEnsurePage,
	toolConvertPage, toolCopyPage, toolSearchPages, toolGetSearchStatus,
	toolListTags, toolGetPagesByTags, toolListPropertyKeys, toolGetPagesByProperty,
	toolGetLinkStatus, toolUploadAsset, toolGetAsset, toolListAssets, toolRenameAsset,
	toolDeleteAsset,
}

var workspaceSyncToolDescriptors = []ToolDescriptor{
	toolRefresh,
}

var revisionToolDescriptors = []ToolDescriptor{
	toolListRevisions, toolGetLatestRevision, toolGetRevision, toolCompareRevisions,
	toolGetRevisionAsset, toolRestoreRevision,
}

var linkRefactorToolDescriptors = []ToolDescriptor{
	toolPreviewRefactor, toolApplyRefactor,
}

func BaseToolNames() []string {
	return toolNames(baseToolDescriptors)
}

func WorkspaceSyncToolNames() []string {
	return toolNames(workspaceSyncToolDescriptors)
}

func RevisionToolNames() []string {
	return toolNames(revisionToolDescriptors)
}

func LinkRefactorToolNames() []string {
	return toolNames(linkRefactorToolDescriptors)
}

func toolNames(descriptors []ToolDescriptor) []string {
	names := make([]string, 0, len(descriptors))
	for _, descriptor := range descriptors {
		names = append(names, descriptor.Name)
	}
	return names
}
