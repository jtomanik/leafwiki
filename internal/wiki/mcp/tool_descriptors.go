package mcp

type ToolID string

func (id ToolID) String() string {
	return string(id)
}

func (id ToolID) ProtocolName() ToolProtocolName {
	return ToolProtocolName(id)
}

type ToolProtocolName string

func (name ToolProtocolName) String() string {
	return string(name)
}

type ToolDescriptionID string

func (id ToolDescriptionID) String() string {
	return string(id)
}

// ToolDescriptor is the single source for an MCP tool's protocol name and
// user-facing description.
type ToolDescriptor struct {
	Name          ToolID
	DescriptionID ToolDescriptionID
	Description   string
}

const (
	ToolGetConfig          ToolID = "wiki_get_config"
	ToolGetCurrentUser     ToolID = "wiki_get_current_user"
	ToolGetContext         ToolID = "wiki_get_context"
	ToolRefresh            ToolID = "wiki_refresh"
	ToolGetSubtree         ToolID = "wiki_get_subtree"
	ToolValidatePage       ToolID = "wiki_validate_page"
	ToolValidateContent    ToolID = "wiki_validate_content"
	ToolValidateWiki       ToolID = "wiki_validate_wiki"
	ToolUpdatePageMetadata ToolID = "wiki_update_page_metadata"
	ToolReplacePageSection ToolID = "wiki_replace_page_section"
	ToolGetTree            ToolID = "wiki_get_tree"
	ToolGetPage            ToolID = "wiki_get_page"
	ToolGetPageByPath      ToolID = "wiki_get_page_by_path"
	ToolLookupPath         ToolID = "wiki_lookup_path"
	ToolResolvePermalink   ToolID = "wiki_resolve_permalink"
	ToolSuggestSlug        ToolID = "wiki_suggest_slug"
	ToolCreatePage         ToolID = "wiki_create_page"
	ToolUpdatePage         ToolID = "wiki_update_page"
	ToolDeletePage         ToolID = "wiki_delete_page"
	ToolMovePage           ToolID = "wiki_move_page"
	ToolSortPages          ToolID = "wiki_sort_pages"
	ToolEnsurePage         ToolID = "wiki_ensure_page"
	ToolConvertPage        ToolID = "wiki_convert_page"
	ToolCopyPage           ToolID = "wiki_copy_page"
	ToolSearchPages        ToolID = "wiki_search_pages"
	ToolGetSearchStatus    ToolID = "wiki_get_search_status"
	ToolListTags           ToolID = "wiki_list_tags"
	ToolGetPagesByTags     ToolID = "wiki_get_pages_by_tags"
	ToolListPropertyKeys   ToolID = "wiki_list_property_keys"
	ToolGetPagesByProperty ToolID = "wiki_get_pages_by_property"
	ToolGetLinkStatus      ToolID = "wiki_get_link_status"
	ToolUploadAsset        ToolID = "wiki_upload_asset"
	ToolGetAsset           ToolID = "wiki_get_asset"
	ToolListAssets         ToolID = "wiki_list_assets"
	ToolRenameAsset        ToolID = "wiki_rename_asset"
	ToolDeleteAsset        ToolID = "wiki_delete_asset"
	ToolListRevisions      ToolID = "wiki_list_revisions"
	ToolGetLatestRevision  ToolID = "wiki_get_latest_revision"
	ToolGetRevision        ToolID = "wiki_get_revision"
	ToolCompareRevisions   ToolID = "wiki_compare_revisions"
	ToolGetRevisionAsset   ToolID = "wiki_get_revision_asset"
	ToolRestoreRevision    ToolID = "wiki_restore_revision"
	ToolPreviewRefactor    ToolID = "wiki_preview_page_refactor"
	ToolApplyRefactor      ToolID = "wiki_apply_page_refactor"
)

const (
	ToolDescriptionMovePage ToolDescriptionID = "mcp.tools.wiki_move_page.description"
)

var (
	toolGetConfig          = newToolDescriptor(ToolGetConfig, "Return local MCP-visible LeafWiki configuration")
	toolGetCurrentUser     = newToolDescriptor(ToolGetCurrentUser, "Return the effective MCP user")
	toolGetContext         = newToolDescriptor(ToolGetContext, "Return agent-ready wiki context, sync state, recent changes, and presence")
	toolRefresh            = newToolDescriptor(ToolRefresh, "Synchronize direct Markdown changes into LeafWiki state")
	toolGetSubtree         = newToolDescriptor(ToolGetSubtree, "Return a compact subtree rooted at a page, path, or the wiki root")
	toolValidatePage       = newToolDescriptor(ToolValidatePage, "Validate an existing page by page ID or path")
	toolValidateContent    = newToolDescriptor(ToolValidateContent, "Validate proposed Markdown content without writing it")
	toolValidateWiki       = newToolDescriptor(ToolValidateWiki, "Validate the current wiki state")
	toolUpdatePageMetadata = newToolDescriptor(ToolUpdatePageMetadata, "Safely patch page tags and properties without changing body content")
	toolReplacePageSection = newToolDescriptor(ToolReplacePageSection, "Safely replace Markdown under a target heading")
	toolGetTree            = newToolDescriptor(ToolGetTree, "Return the wiki page tree")
	toolGetPage            = newToolDescriptor(ToolGetPage, "Return a page by ID with link status context")
	toolGetPageByPath      = newToolDescriptor(ToolGetPageByPath, "Return a page by route path with link status context")
	toolLookupPath         = newToolDescriptor(ToolLookupPath, "Resolve a route path into existing and missing path segments; pass kind page or section to disambiguate same-route twins")
	toolResolvePermalink   = newToolDescriptor(ToolResolvePermalink, "Resolve a stable page ID to its current route path")
	toolSuggestSlug        = newToolDescriptor(ToolSuggestSlug, "Suggest a unique child slug for a title")
	toolCreatePage         = newToolDescriptor(ToolCreatePage, "Create a wiki page or section")
	toolUpdatePage         = newToolDescriptor(ToolUpdatePage, "Update page title, slug, content, tags, and properties")
	toolDeletePage         = newToolDescriptor(ToolDeletePage, "Delete a page")
	toolMovePage           = newToolDescriptor(ToolMovePage, "Move a page to a new parent")
	toolSortPages          = newToolDescriptor(ToolSortPages, "Sort a parent's child pages")
	toolEnsurePage         = newToolDescriptor(ToolEnsurePage, "Ensure a page exists at a route path")
	toolConvertPage        = newToolDescriptor(ToolConvertPage, "Convert a page between page and section kinds")
	toolCopyPage           = newToolDescriptor(ToolCopyPage, "Copy a page and its assets")
	toolSearchPages        = newToolDescriptor(ToolSearchPages, "Search pages using LeafWiki offset and limit pagination")
	toolGetSearchStatus    = newToolDescriptor(ToolGetSearchStatus, "Return the search indexing status")
	toolListTags           = newToolDescriptor(ToolListTags, "List tag counts")
	toolGetPagesByTags     = newToolDescriptor(ToolGetPagesByTags, "List pages matching all tags")
	toolListPropertyKeys   = newToolDescriptor(ToolListPropertyKeys, "List property key counts")
	toolGetPagesByProperty = newToolDescriptor(ToolGetPagesByProperty, "List pages with a property value")
	toolGetLinkStatus      = newToolDescriptor(ToolGetLinkStatus, "Return link status for a page")
	toolUploadAsset        = newToolDescriptor(ToolUploadAsset, "Upload an asset from base64 content")
	toolGetAsset           = newToolDescriptor(ToolGetAsset, "Read an asset as base64 content")
	toolListAssets         = newToolDescriptor(ToolListAssets, "List page assets")
	toolRenameAsset        = newToolDescriptor(ToolRenameAsset, "Rename a page asset")
	toolDeleteAsset        = newToolDescriptor(ToolDeleteAsset, "Delete a page asset")
	toolListRevisions      = newToolDescriptor(ToolListRevisions, "List page revisions")
	toolGetLatestRevision  = newToolDescriptor(ToolGetLatestRevision, "Get the latest page revision")
	toolGetRevision        = newToolDescriptor(ToolGetRevision, "Get a page revision snapshot")
	toolCompareRevisions   = newToolDescriptor(ToolCompareRevisions, "Compare two page revisions")
	toolGetRevisionAsset   = newToolDescriptor(ToolGetRevisionAsset, "Read a revision asset as base64 content")
	toolRestoreRevision    = newToolDescriptor(ToolRestoreRevision, "Restore a page revision")
	toolPreviewRefactor    = newToolDescriptor(ToolPreviewRefactor, "Preview a page rename or move refactor")
	toolApplyRefactor      = newToolDescriptor(ToolApplyRefactor, "Apply a page rename or move refactor")
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
		names = append(names, descriptor.Name.ProtocolName().String())
	}
	return names
}

func newToolDescriptor(name ToolID, description string) ToolDescriptor {
	return ToolDescriptor{
		Name:          name,
		DescriptionID: ToolDescriptionIDForTool(name),
		Description:   description,
	}
}

func ToolDescriptionIDForTool(name ToolID) ToolDescriptionID {
	return ToolDescriptionID("mcp.tools." + name.ProtocolName().String() + ".description")
}
