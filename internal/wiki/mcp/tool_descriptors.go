package mcp

import "github.com/perber/wiki/internal/localization"

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
	toolGetConfig          = newToolDescriptor(ToolGetConfig)
	toolGetCurrentUser     = newToolDescriptor(ToolGetCurrentUser)
	toolGetContext         = newToolDescriptor(ToolGetContext)
	toolRefresh            = newToolDescriptor(ToolRefresh)
	toolGetSubtree         = newToolDescriptor(ToolGetSubtree)
	toolValidatePage       = newToolDescriptor(ToolValidatePage)
	toolValidateContent    = newToolDescriptor(ToolValidateContent)
	toolValidateWiki       = newToolDescriptor(ToolValidateWiki)
	toolUpdatePageMetadata = newToolDescriptor(ToolUpdatePageMetadata)
	toolReplacePageSection = newToolDescriptor(ToolReplacePageSection)
	toolGetTree            = newToolDescriptor(ToolGetTree)
	toolGetPage            = newToolDescriptor(ToolGetPage)
	toolGetPageByPath      = newToolDescriptor(ToolGetPageByPath)
	toolLookupPath         = newToolDescriptor(ToolLookupPath)
	toolResolvePermalink   = newToolDescriptor(ToolResolvePermalink)
	toolSuggestSlug        = newToolDescriptor(ToolSuggestSlug)
	toolCreatePage         = newToolDescriptor(ToolCreatePage)
	toolUpdatePage         = newToolDescriptor(ToolUpdatePage)
	toolDeletePage         = newToolDescriptor(ToolDeletePage)
	toolMovePage           = newToolDescriptor(ToolMovePage)
	toolSortPages          = newToolDescriptor(ToolSortPages)
	toolEnsurePage         = newToolDescriptor(ToolEnsurePage)
	toolConvertPage        = newToolDescriptor(ToolConvertPage)
	toolCopyPage           = newToolDescriptor(ToolCopyPage)
	toolSearchPages        = newToolDescriptor(ToolSearchPages)
	toolGetSearchStatus    = newToolDescriptor(ToolGetSearchStatus)
	toolListTags           = newToolDescriptor(ToolListTags)
	toolGetPagesByTags     = newToolDescriptor(ToolGetPagesByTags)
	toolListPropertyKeys   = newToolDescriptor(ToolListPropertyKeys)
	toolGetPagesByProperty = newToolDescriptor(ToolGetPagesByProperty)
	toolGetLinkStatus      = newToolDescriptor(ToolGetLinkStatus)
	toolUploadAsset        = newToolDescriptor(ToolUploadAsset)
	toolGetAsset           = newToolDescriptor(ToolGetAsset)
	toolListAssets         = newToolDescriptor(ToolListAssets)
	toolRenameAsset        = newToolDescriptor(ToolRenameAsset)
	toolDeleteAsset        = newToolDescriptor(ToolDeleteAsset)
	toolListRevisions      = newToolDescriptor(ToolListRevisions)
	toolGetLatestRevision  = newToolDescriptor(ToolGetLatestRevision)
	toolGetRevision        = newToolDescriptor(ToolGetRevision)
	toolCompareRevisions   = newToolDescriptor(ToolCompareRevisions)
	toolGetRevisionAsset   = newToolDescriptor(ToolGetRevisionAsset)
	toolRestoreRevision    = newToolDescriptor(ToolRestoreRevision)
	toolPreviewRefactor    = newToolDescriptor(ToolPreviewRefactor)
	toolApplyRefactor      = newToolDescriptor(ToolApplyRefactor)
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

func allToolDescriptors() []ToolDescriptor {
	descriptors := make([]ToolDescriptor, 0, len(baseToolDescriptors)+len(workspaceSyncToolDescriptors)+len(revisionToolDescriptors)+len(linkRefactorToolDescriptors))
	descriptors = append(descriptors, baseToolDescriptors...)
	descriptors = append(descriptors, workspaceSyncToolDescriptors...)
	descriptors = append(descriptors, revisionToolDescriptors...)
	descriptors = append(descriptors, linkRefactorToolDescriptors...)
	return descriptors
}

func toolNames(descriptors []ToolDescriptor) []string {
	names := make([]string, 0, len(descriptors))
	for _, descriptor := range descriptors {
		names = append(names, descriptor.Name.ProtocolName().String())
	}
	return names
}

func newToolDescriptor(name ToolID) ToolDescriptor {
	descriptionID := ToolDescriptionIDForTool(name)
	return ToolDescriptor{
		Name:          name,
		DescriptionID: descriptionID,
		Description:   localization.English.Render(descriptionID, "").Message,
	}
}

func ToolDescriptionIDForTool(name ToolID) ToolDescriptionID {
	return ToolDescriptionID("mcp.tools." + name.ProtocolName().String() + ".description")
}
