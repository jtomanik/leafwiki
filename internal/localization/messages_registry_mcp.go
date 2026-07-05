package localization

import "github.com/nicksnyder/go-i18n/v2/i18n"

var registryMessagesMCP = []*i18n.Message{
	{
		ID:          MessageIDShellRunErrorOptionRequiresPath,
		Description: "Printed when scripts/run.sh option is missing a path.",
		Other:       "requires a path",
	},
	{
		ID:          MessageIDShellRunErrorOptionRequiresSecret,
		Description: "Printed when scripts/run.sh option is missing a secret.",
		Other:       "requires a secret",
	},
	{
		ID:          MessageIDShellRunErrorOptionRequiresValue,
		Description: "Printed when scripts/run.sh option is missing a value.",
		Other:       "requires a value",
	},
	{
		ID:          MessageIDShellRunErrorRunModeRequired,
		Description: "Printed when scripts/run.sh is called without mcp or agent-hook mode.",
		Other:       "first argument must be mcp or agent-hook",
	},
	{
		ID:          MessageIDShellRunErrorUnknownOption,
		Description: "Printed when scripts/run.sh receives an unknown option.",
		Other:       "unknown option",
	},
	{
		ID:          messageIDAPIAssetsDeleteSuccess,
		Description: "Returned after an asset is deleted through the API.",
		Other:       "Asset deleted",
	},
	{
		ID:          messageIDAPIAuthLoginSuccess,
		Description: "Returned after an auth login succeeds through the API.",
		Other:       "Login successful",
	},
	{
		ID:          messageIDAPIAuthLogoutSuccess,
		Description: "Returned after an auth logout succeeds through the API.",
		Other:       "Logout successful",
	},
	{
		ID:          messageIDAPIAuthRefreshTokenSuccess,
		Description: "Returned after an auth token refresh succeeds through the API.",
		Other:       "Token refreshed",
	},
	{
		ID:          messageIDAPIPagesDeleteSuccess,
		Description: "Returned after a page is deleted through the API.",
		Other:       "Page deleted",
	},
	{
		ID:          messageIDAPIPagesMoveSuccess,
		Description: "Returned after a page is moved through the API.",
		Other:       "Page moved",
	},
	{
		ID:          messageIDAPIPagesSortSuccess,
		Description: "Returned after child pages are sorted through the API.",
		Other:       "Pages sorted successfully",
	},
	{
		ID:          messageIDUIPageSaveSuccess,
		Description: "Displayed after a page is saved in the browser editor.",
		Other:       "Page saved successfully",
	},
	{
		ID:          messageIDPageVersionConflict,
		Description: "Returned when a page update uses an outdated version.",
		Other:       "Page {{.Arg0}} was changed by another request before {{.Arg1}} could be saved.",
	},
	{
		ID:          messageIDWorkspaceGrantDenied,
		Description: "Returned when a user lacks a workspace grant.",
		Other:       "workspace access denied",
	},
	{
		ID:          messageIDMCPWorkspaceUnavailable,
		Description: "Returned when a workspace MCP endpoint is unavailable.",
		Other:       "Workspace MCP unavailable",
	},
	{
		ID:          messageIDPrivateMCPTokenInvalid,
		Description: "Returned when a private MCP control token is invalid.",
		Other:       "Unauthorized",
	},
	{
		ID:          messageIDMCPConvertPageSuccess,
		Description: "Returned after a page is converted through MCP.",
		Other:       "Page converted",
	},
	{
		ID:          messageIDMCPDeleteAssetSuccess,
		Description: "Returned after an asset is deleted through MCP.",
		Other:       "Asset deleted",
	},
	{
		ID:          messageIDMCPDeletePageSuccess,
		Description: "Returned after a page is deleted through MCP.",
		Other:       "Page deleted",
	},
	{
		ID:          messageIDMCPGetConfigDesc,
		Description: "Descriptor for the MCP configuration tool.",
		Other:       "Return local MCP-visible LeafWiki configuration",
	},
	{
		ID:          messageIDMCPGetCurrentUserDesc,
		Description: "Descriptor for the MCP current-user tool.",
		Other:       "Return the effective MCP user",
	},
	{
		ID:          messageIDMCPGetContextDesc,
		Description: "Descriptor for the MCP context tool.",
		Other:       "Return agent-ready wiki context, sync state, recent changes, and presence",
	},
	{
		ID:          messageIDMCPRefreshDesc,
		Description: "Descriptor for the MCP refresh tool.",
		Other:       "Synchronize direct Markdown changes into LeafWiki state",
	},
	{
		ID:          messageIDMCPGetSubtreeDesc,
		Description: "Descriptor for the MCP subtree tool.",
		Other:       "Return a compact subtree rooted at a page, path, or the wiki root",
	},
	{
		ID:          messageIDMCPValidatePageDesc,
		Description: "Descriptor for the MCP page validation tool.",
		Other:       "Validate an existing page by page ID or path",
	},
	{
		ID:          messageIDMCPValidateContentDesc,
		Description: "Descriptor for the MCP content validation tool.",
		Other:       "Validate proposed Markdown content without writing it",
	},
	{
		ID:          messageIDMCPValidateWikiDesc,
		Description: "Descriptor for the MCP wiki validation tool.",
		Other:       "Validate the current wiki state",
	},
	{
		ID:          messageIDMCPUpdateMetadataDesc,
		Description: "Descriptor for the MCP metadata update tool.",
		Other:       "Safely patch page tags and properties without changing body content",
	},
	{
		ID:          messageIDMCPReplaceSectionDesc,
		Description: "Descriptor for the MCP section replacement tool.",
		Other:       "Safely replace Markdown under a target heading",
	},
	{
		ID:          messageIDMCPGetTreeDesc,
		Description: "Descriptor for the MCP tree tool.",
		Other:       "Return the wiki page tree",
	},
	{
		ID:          messageIDMCPGetPageDesc,
		Description: "Descriptor for the MCP get-page tool.",
		Other:       "Return a page by ID with link status context",
	},
	{
		ID:          messageIDMCPGetPageByPathDesc,
		Description: "Descriptor for the MCP get-page-by-path tool.",
		Other:       "Return a page by route path with link status context",
	},
	{
		ID:          messageIDMCPLookupPathDesc,
		Description: "Descriptor for the MCP path lookup tool.",
		Other:       "Resolve a route path into existing and missing path segments; pass kind page or section to disambiguate same-route twins",
	},
	{
		ID:          messageIDMCPResolvePermalinkDesc,
		Description: "Descriptor for the MCP permalink resolver.",
		Other:       "Resolve a stable page ID to its current route path",
	},
	{
		ID:          messageIDMCPSuggestSlugDesc,
		Description: "Descriptor for the MCP slug suggestion tool.",
		Other:       "Suggest a unique child slug for a title",
	},
	{
		ID:          messageIDMCPCreatePageDesc,
		Description: "Descriptor for the MCP page creation tool.",
		Other:       "Create a wiki page or section",
	},
	{
		ID:          messageIDMCPUpdatePageDesc,
		Description: "Descriptor for the MCP page update tool.",
		Other:       "Update page title, slug, content, tags, and properties",
	},
	{
		ID:          messageIDMCPDeletePageDesc,
		Description: "Descriptor for the MCP page deletion tool.",
		Other:       "Delete a page",
	},
	{
		ID:          messageIDMCPMovePageDesc,
		Description: "Descriptor for the MCP page move tool.",
		Other:       "Move a page to a new parent",
	},
	{
		ID:          messageIDMCPSortPagesDesc,
		Description: "Descriptor for the MCP page sorting tool.",
		Other:       "Sort a parent's child pages",
	},
	{
		ID:          messageIDMCPEnsurePageDesc,
		Description: "Descriptor for the MCP ensure-page tool.",
		Other:       "Ensure a page exists at a route path",
	},
	{
		ID:          messageIDMCPConvertPageDesc,
		Description: "Descriptor for the MCP page conversion tool.",
		Other:       "Convert a page between page and section kinds",
	},
	{
		ID:          messageIDMCPCopyPageDesc,
		Description: "Descriptor for the MCP page copy tool.",
		Other:       "Copy a page and its assets",
	},
	{
		ID:          messageIDMCPSearchPagesDesc,
		Description: "Descriptor for the MCP page search tool.",
		Other:       "Search pages using LeafWiki offset and limit pagination",
	},
	{
		ID:          messageIDMCPGetSearchStatusDesc,
		Description: "Descriptor for the MCP search status tool.",
		Other:       "Return the search indexing status",
	},
	{
		ID:          messageIDMCPListTagsDesc,
		Description: "Descriptor for the MCP tag-listing tool.",
		Other:       "List tag counts",
	},
	{
		ID:          messageIDMCPGetPagesByTagsDesc,
		Description: "Descriptor for the MCP pages-by-tags tool.",
		Other:       "List pages matching all tags",
	},
	{
		ID:          messageIDMCPListPropertyKeysDesc,
		Description: "Descriptor for the MCP property-key listing tool.",
		Other:       "List property key counts",
	},
	{
		ID:          messageIDMCPGetPagesByPropertyDesc,
		Description: "Descriptor for the MCP pages-by-property tool.",
		Other:       "List pages with a property value",
	},
	{
		ID:          messageIDMCPGetLinkStatusDesc,
		Description: "Descriptor for the MCP link status tool.",
		Other:       "Return link status for a page",
	},
	{
		ID:          messageIDMCPUploadAssetDesc,
		Description: "Descriptor for the MCP asset upload tool.",
		Other:       "Upload an asset from base64 content",
	},
	{
		ID:          messageIDMCPGetAssetDesc,
		Description: "Descriptor for the MCP get-asset tool.",
		Other:       "Read an asset as base64 content",
	},
	{
		ID:          messageIDMCPListAssetsDesc,
		Description: "Descriptor for the MCP asset-listing tool.",
		Other:       "List page assets",
	},
	{
		ID:          messageIDMCPRenameAssetDesc,
		Description: "Descriptor for the MCP asset rename tool.",
		Other:       "Rename a page asset",
	},
	{
		ID:          messageIDMCPDeleteAssetDesc,
		Description: "Descriptor for the MCP asset deletion tool.",
		Other:       "Delete a page asset",
	},
	{
		ID:          messageIDMCPListRevisionsDesc,
		Description: "Descriptor for the MCP revision-listing tool.",
		Other:       "List page revisions",
	},
	{
		ID:          messageIDMCPGetLatestRevisionDesc,
		Description: "Descriptor for the MCP latest-revision tool.",
		Other:       "Get the latest page revision",
	},
	{
		ID:          messageIDMCPGetRevisionDesc,
		Description: "Descriptor for the MCP get-revision tool.",
		Other:       "Get a page revision snapshot",
	},
	{
		ID:          messageIDMCPCompareRevisionsDesc,
		Description: "Descriptor for the MCP revision comparison tool.",
		Other:       "Compare two page revisions",
	},
}
