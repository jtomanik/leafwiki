package localization

import "github.com/nicksnyder/go-i18n/v2/i18n"

const (
	MessageIDCLIHelpUsage                            = "cli.help.usage"
	MessageIDCLIHelpBody                             = "cli.help.body"
	MessageIDCLIErrorByteSizeTooLarge                = "cli.error.byte_size_too_large"
	MessageIDCLIErrorByteSizeMustBePositive          = "cli.error.byte_size_must_be_positive"
	MessageIDCLIErrorInvalidAgentHookArguments       = "cli.error.invalid_agent_hook_arguments"
	MessageIDCLIErrorInvalidByteSizeValue            = "cli.error.invalid_byte_size_value"
	MessageIDCLIErrorInvalidConfigArguments          = "cli.error.invalid_config_arguments"
	MessageIDCLIErrorInvalidConfigFile               = "cli.error.invalid_config_file"
	MessageIDCLIErrorInvalidEnvironment              = "cli.error.invalid_environment"
	MessageIDCLIErrorInvalidEnvironmentVariableValue = "cli.error.invalid_environment_variable_value"
	MessageIDCLIErrorInvalidHTTPRemoteUserConfig     = "cli.error.invalid_http_remote_user_config"
	MessageIDCLIErrorInvalidLoggingConfig            = "cli.error.invalid_logging_config"
	MessageIDCLIErrorInvalidMarkdownLinkRootPrefix   = "cli.error.invalid_markdown_link_root_prefix"
	MessageIDCLIErrorInvalidMCPConfig                = "cli.error.invalid_mcp_config"
	MessageIDCLIErrorInvalidNativeSTDIOConfig        = "cli.error.invalid_native_stdio_config"
	MessageIDCLIErrorInvalidServiceConfigFile        = "cli.error.invalid_service_config_file"
	MessageIDCLIErrorInvalidTrustedProxyIPs          = "cli.error.invalid_trusted_proxy_ips"
	MessageIDCLIErrorInvalidWorkspaceConfig          = "cli.error.invalid_workspace_config"
	MessageIDCLIErrorLeafWikiDaemonFailed            = "cli.error.leafwiki_daemon_failed"
	MessageIDCLIErrorLeafWikiStartupFailed           = "cli.error.leafwiki_startup_failed"
	MessageIDCLIErrorPasswordResetFailed             = "cli.error.password_reset_failed"
	MessageIDCLIErrorProjectDaemonFailed             = "cli.error.project_daemon_failed"
	MessageIDCLIErrorRuntimeRoleFailed               = "cli.error.runtime_role_failed"
	MessageIDCLIErrorServiceConfigRequired           = "cli.error.service_config_required"
	MessageIDShellRunUsage                           = "shell.run.usage"
	MessageIDShellRunHelpBody                        = "shell.run.help_body"
	MessageIDShellRunErrorPrefix                     = "shell.run.error_prefix"
	MessageIDShellRunDryRunMCPConfig                 = "shell.run.dry_run.mcp_config"
	MessageIDShellRunDryRunMCPNative                 = "shell.run.dry_run.mcp_native"
	MessageIDShellRunDryRunSTDIOAttach               = "shell.run.dry_run.stdio_attach"
	MessageIDShellRunDryRunAgentHook                 = "shell.run.dry_run.agent_hook"
	MessageIDShellRunDryRunHTTPConfig                = "shell.run.dry_run.http_config"
	MessageIDShellRunDryRunHTTPURL                   = "shell.run.dry_run.http_url"
	MessageIDShellRunErrorAgentHookRequiresProvider  = "shell.run.error.agent_hook_requires_provider"
	MessageIDShellRunErrorArgumentContainsSpace      = "shell.run.error.argument_contains_space"
	MessageIDShellRunErrorConfigCannotCombine        = "shell.run.error.config_cannot_combine"
	MessageIDShellRunErrorConfigRequiresPath         = "shell.run.error.config_requires_path"
	MessageIDShellRunErrorDisableAuthAPIKeyConflict  = "shell.run.error.disable_auth_api_key_conflict"
	MessageIDShellRunErrorExecutableNotFound         = "shell.run.error.executable_not_found"
	MessageIDShellRunErrorExecutableNotFoundOnPath   = "shell.run.error.executable_not_found_on_path"
	MessageIDShellRunErrorLeafWikiBinRequiresPath    = "shell.run.error.leafwiki_bin_requires_path"
	MessageIDShellRunErrorOptionRequiresArgument     = "shell.run.error.option_requires_argument"
	MessageIDShellRunErrorOptionRequiresDuration     = "shell.run.error.option_requires_duration"
	MessageIDShellRunErrorOptionRequiresPassword     = "shell.run.error.option_requires_password"
	MessageIDShellRunErrorOptionRequiresPath         = "shell.run.error.option_requires_path"
	MessageIDShellRunErrorOptionRequiresSecret       = "shell.run.error.option_requires_secret"
	MessageIDShellRunErrorOptionRequiresValue        = "shell.run.error.option_requires_value"
	MessageIDShellRunErrorRunModeRequired            = "shell.run.error.run_mode_required"
	MessageIDShellRunErrorUnknownOption              = "shell.run.error.unknown_option"
	messageIDAPIPagesDeleteSuccess                   = "api.pages.delete.success"
	messageIDAPIPagesMoveSuccess                     = "api.pages.move.success"
	messageIDAPIPagesSortSuccess                     = "api.pages.sort.success"
	messageIDAPIAssetsDeleteSuccess                  = "api.assets.delete.success"
	messageIDAPIAuthLoginSuccess                     = "api.auth.login.success"
	messageIDAPIAuthLogoutSuccess                    = "api.auth.logout.success"
	messageIDAPIAuthRefreshTokenSuccess              = "api.auth.refresh_token.success"
	messageIDUIPageSaveSuccess                       = "ui.page.save.success"
	messageIDPageVersionConflict                     = "errors.page.version_conflict"
	messageIDWorkspaceGrantDenied                    = "errors.workspace.grant_denied"
	messageIDMCPWorkspaceUnavailable                 = "errors.mcp.workspace_unavailable"
	messageIDPrivateMCPTokenInvalid                  = "errors.private.mcp_control_token_invalid"
	messageIDAuthInvalidCreds                        = "errors.auth.invalid_credentials"
	messageIDMCPDeleteAssetSuccess                   = "mcp.tools.wiki_delete_asset.success"
	messageIDMCPDeletePageSuccess                    = "mcp.tools.wiki_delete_page.success"
	messageIDMCPGetConfigDesc                        = "mcp.tools.wiki_get_config.description"
	messageIDMCPGetCurrentUserDesc                   = "mcp.tools.wiki_get_current_user.description"
	messageIDMCPGetContextDesc                       = "mcp.tools.wiki_get_context.description"
	messageIDMCPRefreshDesc                          = "mcp.tools.wiki_refresh.description"
	messageIDMCPGetSubtreeDesc                       = "mcp.tools.wiki_get_subtree.description"
	messageIDMCPValidatePageDesc                     = "mcp.tools.wiki_validate_page.description"
	messageIDMCPValidateContentDesc                  = "mcp.tools.wiki_validate_content.description"
	messageIDMCPValidateWikiDesc                     = "mcp.tools.wiki_validate_wiki.description"
	messageIDMCPUpdateMetadataDesc                   = "mcp.tools.wiki_update_page_metadata.description"
	messageIDMCPReplaceSectionDesc                   = "mcp.tools.wiki_replace_page_section.description"
	messageIDMCPGetTreeDesc                          = "mcp.tools.wiki_get_tree.description"
	messageIDMCPGetPageDesc                          = "mcp.tools.wiki_get_page.description"
	messageIDMCPGetPageByPathDesc                    = "mcp.tools.wiki_get_page_by_path.description"
	messageIDMCPLookupPathDesc                       = "mcp.tools.wiki_lookup_path.description"
	messageIDMCPResolvePermalinkDesc                 = "mcp.tools.wiki_resolve_permalink.description"
	messageIDMCPSuggestSlugDesc                      = "mcp.tools.wiki_suggest_slug.description"
	messageIDMCPCreatePageDesc                       = "mcp.tools.wiki_create_page.description"
	messageIDMCPUpdatePageDesc                       = "mcp.tools.wiki_update_page.description"
	messageIDMCPDeletePageDesc                       = "mcp.tools.wiki_delete_page.description"
	messageIDMCPMovePageDesc                         = "mcp.tools.wiki_move_page.description"
	messageIDMCPSortPagesDesc                        = "mcp.tools.wiki_sort_pages.description"
	messageIDMCPEnsurePageDesc                       = "mcp.tools.wiki_ensure_page.description"
	messageIDMCPConvertPageDesc                      = "mcp.tools.wiki_convert_page.description"
	messageIDMCPCopyPageDesc                         = "mcp.tools.wiki_copy_page.description"
	messageIDMCPSearchPagesDesc                      = "mcp.tools.wiki_search_pages.description"
	messageIDMCPGetSearchStatusDesc                  = "mcp.tools.wiki_get_search_status.description"
	messageIDMCPListTagsDesc                         = "mcp.tools.wiki_list_tags.description"
	messageIDMCPGetPagesByTagsDesc                   = "mcp.tools.wiki_get_pages_by_tags.description"
	messageIDMCPListPropertyKeysDesc                 = "mcp.tools.wiki_list_property_keys.description"
	messageIDMCPGetPagesByPropertyDesc               = "mcp.tools.wiki_get_pages_by_property.description"
	messageIDMCPGetLinkStatusDesc                    = "mcp.tools.wiki_get_link_status.description"
	messageIDMCPUploadAssetDesc                      = "mcp.tools.wiki_upload_asset.description"
	messageIDMCPGetAssetDesc                         = "mcp.tools.wiki_get_asset.description"
	messageIDMCPListAssetsDesc                       = "mcp.tools.wiki_list_assets.description"
	messageIDMCPRenameAssetDesc                      = "mcp.tools.wiki_rename_asset.description"
	messageIDMCPDeleteAssetDesc                      = "mcp.tools.wiki_delete_asset.description"
	messageIDMCPListRevisionsDesc                    = "mcp.tools.wiki_list_revisions.description"
	messageIDMCPGetLatestRevisionDesc                = "mcp.tools.wiki_get_latest_revision.description"
	messageIDMCPGetRevisionDesc                      = "mcp.tools.wiki_get_revision.description"
	messageIDMCPCompareRevisionsDesc                 = "mcp.tools.wiki_compare_revisions.description"
	messageIDMCPGetRevisionAssetDesc                 = "mcp.tools.wiki_get_revision_asset.description"
	messageIDMCPRestoreRevisionDesc                  = "mcp.tools.wiki_restore_revision.description"
	messageIDMCPPreviewRefactorDesc                  = "mcp.tools.wiki_preview_page_refactor.description"
	messageIDMCPApplyRefactorDesc                    = "mcp.tools.wiki_apply_page_refactor.description"
	messageIDMCPMovePageSuccess                      = "mcp.tools.wiki_move_page.success"
	messageIDMCPSortPagesSuccess                     = "mcp.tools.wiki_sort_pages.success"
	messageIDMCPConvertPageSuccess                   = "mcp.tools.wiki_convert_page.success"
	messageIDValidationFieldError                    = "validation.field.validation_error"
	messageIDValidationBrandingNameReq               = "validation.branding.site_name_required"
	messageIDValidationBrandingNameLong              = "validation.branding.site_name_too_long"
	messageIDValidationBrandingNameControl           = "validation.branding.site_name_control_characters"
	messageIDValidationPageTitleReq                  = "validation.page.title_required"
	messageIDValidationPageKindReq                   = "validation.page.kind_required"
	messageIDValidationPageKindInvalid               = "validation.page.kind_invalid"
	messageIDValidationPageSlugInvalid               = "validation.page.slug_invalid"
	messageIDValidationPagePathReq                   = "validation.page.path_required"
	messageIDValidationPagePathInvalid               = "validation.page.path_invalid"
	messageIDValidationPageTagReq                    = "validation.page.tag_required"
	messageIDValidationPageTagWhitespace             = "validation.page.tag_whitespace"
	messageIDValidationPageTagDuplicate              = "validation.page.tag_duplicate"
	messageIDValidationPagePropertyKeyReq            = "validation.page.property_key_required"
	messageIDValidationPagePropertyKeyWhitespace     = "validation.page.property_key_whitespace"
	messageIDValidationPagePropertyKeyReserved       = "validation.page.property_key_reserved"
	messageIDValidationPagePropertyKeyReservedPrefix = "validation.page.property_key_reserved_prefix"
	messageIDValidationAuthUsernameReq               = "validation.auth.username_required"
	messageIDValidationAuthEmailReq                  = "validation.auth.email_required"
	messageIDAuthEmailInvalid                        = "validation.auth.email_invalid"
	messageIDValidationAuthPasswordReq               = "validation.auth.password_required"
	messageIDValidationAuthPasswordShort             = "validation.auth.password_too_short"
	messageIDValidationAuthRoleInvalid               = "validation.auth.role_invalid"
	messageIDValidationAuthNewPasswordReq            = "validation.auth.new_password_required"
	messageIDValidationAuthNewPasswordShort          = "validation.auth.new_password_too_short"
	messageIDValidationAuthOldPasswordIncorrect      = "validation.auth.old_password_incorrect"
	messageIDValidationAuthAPIKeyNameReq             = "validation.auth.api_key_name_required"
	messageIDValidationAuthAPIKeyNameLong            = "validation.auth.api_key_name_too_long"
	messageIDValidationAuthCurrentPasswordReq        = "validation.auth.current_password_required"
	messageIDValidationAuthCurrentPasswordIncorrect  = "validation.auth.current_password_incorrect"
)

type Definition struct {
	ID          string
	Default     string
	Description string
}

var registryMessages = []*i18n.Message{
	{
		ID:          MessageIDCLIErrorByteSizeMustBePositive,
		Description: "Printed when a byte-size CLI setting is zero or negative.",
		Other:       "Byte size value must be greater than zero",
	},
	{
		ID:          MessageIDCLIErrorByteSizeTooLarge,
		Description: "Printed when a byte-size CLI setting is too large.",
		Other:       "Byte size value is too large",
	},
	{
		ID:          MessageIDCLIErrorInvalidAgentHookArguments,
		Description: "Printed when agent-hook CLI arguments are invalid.",
		Other:       "Invalid agent hook arguments",
	},
	{
		ID:          MessageIDCLIErrorInvalidByteSizeValue,
		Description: "Printed when a byte-size CLI setting cannot be parsed.",
		Other:       "Invalid byte size value",
	},
	{
		ID:          MessageIDCLIErrorInvalidConfigArguments,
		Description: "Printed when YAML config mode is combined with incompatible CLI arguments.",
		Other:       "Invalid config arguments",
	},
	{
		ID:          MessageIDCLIErrorInvalidConfigFile,
		Description: "Printed when the configured YAML file cannot be used.",
		Other:       "Invalid config file",
	},
	{
		ID:          MessageIDCLIErrorInvalidEnvironment,
		Description: "Printed when LeafWiki startup rejects the process environment.",
		Other:       "Invalid environment",
	},
	{
		ID:          MessageIDCLIErrorInvalidEnvironmentVariableValue,
		Description: "Printed when a LeafWiki environment variable has an invalid value.",
		Other:       "Invalid environment variable value",
	},
	{
		ID:          MessageIDCLIErrorInvalidHTTPRemoteUserConfig,
		Description: "Printed when HTTP remote-user authentication config is invalid.",
		Other:       "Invalid HTTP remote user configuration",
	},
	{
		ID:          MessageIDCLIErrorInvalidLoggingConfig,
		Description: "Printed when logging config is invalid.",
		Other:       "Invalid logging configuration",
	},
	{
		ID:          MessageIDCLIErrorInvalidMarkdownLinkRootPrefix,
		Description: "Printed when markdown link root prefix config is invalid.",
		Other:       "Invalid markdown link root prefix",
	},
	{
		ID:          MessageIDCLIErrorInvalidMCPConfig,
		Description: "Printed when MCP config is invalid.",
		Other:       "Invalid MCP configuration",
	},
	{
		ID:          MessageIDCLIErrorInvalidNativeSTDIOConfig,
		Description: "Printed when native STDIO config is invalid.",
		Other:       "Invalid native STDIO configuration",
	},
	{
		ID:          MessageIDCLIErrorInvalidServiceConfigFile,
		Description: "Printed when service-mode config is invalid.",
		Other:       "Invalid service config file",
	},
	{
		ID:          MessageIDCLIErrorInvalidTrustedProxyIPs,
		Description: "Printed when trusted proxy IP config is invalid.",
		Other:       "invalid --trusted-proxy-ips value",
	},
	{
		ID:          MessageIDCLIErrorInvalidWorkspaceConfig,
		Description: "Printed when workspace config is invalid.",
		Other:       "Invalid workspace configuration",
	},
	{
		ID:          MessageIDCLIErrorLeafWikiDaemonFailed,
		Description: "Printed when daemon startup fails.",
		Other:       "LeafWiki daemon failed",
	},
	{
		ID:          MessageIDCLIErrorLeafWikiStartupFailed,
		Description: "Printed when LeafWiki startup fails.",
		Other:       "LeafWiki startup failed",
	},
	{
		ID:          MessageIDCLIErrorPasswordResetFailed,
		Description: "Printed when admin password reset fails.",
		Other:       "Password reset failed",
	},
	{
		ID:          MessageIDCLIErrorProjectDaemonFailed,
		Description: "Printed when the project daemon command fails.",
		Other:       "Project daemon failed",
	},
	{
		ID:          MessageIDCLIErrorRuntimeRoleFailed,
		Description: "Printed when a runtime role command fails.",
		Other:       "Runtime role failed",
	},
	{
		ID:          MessageIDCLIErrorServiceConfigRequired,
		Description: "Printed when service mode requires a config file.",
		Other:       "Service config file is required for service mode",
	},
	{
		ID:          MessageIDShellRunErrorAgentHookRequiresProvider,
		Description: "Printed when scripts/run.sh agent-hook mode is missing a provider.",
		Other:       "agent-hook requires a provider",
	},
	{
		ID:          MessageIDShellRunErrorArgumentContainsSpace,
		Description: "Printed when scripts/run.sh receives a combined MCP JSON argument containing a space.",
		Other:       "argument contains a space; MCP JSON args must split flags and values into separate args, for example \"--root-dir\", \"./wiki\", or use --root-dir=./wiki",
	},
	{
		ID:          MessageIDShellRunErrorConfigCannotCombine,
		Description: "Printed when scripts/run.sh --config is combined with another wrapper option.",
		Other:       "--config cannot be combined with",
	},
	{
		ID:          MessageIDShellRunErrorConfigRequiresPath,
		Description: "Printed when scripts/run.sh --config is missing a path.",
		Other:       "--config requires a path",
	},
	{
		ID:          MessageIDShellRunErrorDisableAuthAPIKeyConflict,
		Description: "Printed when disabled auth is combined with an MCP API key.",
		Other:       "--disable-auth cannot be combined with --api-key or LEAFWIKI_MCP_API_KEY",
	},
	{
		ID:          MessageIDShellRunErrorExecutableNotFound,
		Description: "Printed when scripts/run.sh receives a non-executable binary path.",
		Other:       "executable not found or not executable",
	},
	{
		ID:          MessageIDShellRunErrorExecutableNotFoundOnPath,
		Description: "Printed when scripts/run.sh cannot find a binary on PATH.",
		Other:       "executable not found on PATH",
	},
	{
		ID:          MessageIDShellRunErrorLeafWikiBinRequiresPath,
		Description: "Printed when scripts/run.sh --leafwiki-bin is missing a path.",
		Other:       "--leafwiki-bin requires a path",
	},
	{
		ID:          MessageIDShellRunErrorOptionRequiresArgument,
		Description: "Printed when scripts/run.sh option is missing a generic argument.",
		Other:       "requires an argument",
	},
	{
		ID:          MessageIDShellRunErrorOptionRequiresDuration,
		Description: "Printed when scripts/run.sh option is missing a duration.",
		Other:       "requires a duration",
	},
	{
		ID:          MessageIDShellRunErrorOptionRequiresPassword,
		Description: "Printed when scripts/run.sh option is missing a password.",
		Other:       "requires a password",
	},
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
	{
		ID:          messageIDMCPGetRevisionAssetDesc,
		Description: "Descriptor for the MCP revision asset tool.",
		Other:       "Read a revision asset as base64 content",
	},
	{
		ID:          messageIDMCPRestoreRevisionDesc,
		Description: "Descriptor for the MCP restore-revision tool.",
		Other:       "Restore a page revision",
	},
	{
		ID:          messageIDMCPPreviewRefactorDesc,
		Description: "Descriptor for the MCP refactor preview tool.",
		Other:       "Preview a page rename or move refactor",
	},
	{
		ID:          messageIDMCPApplyRefactorDesc,
		Description: "Descriptor for the MCP refactor apply tool.",
		Other:       "Apply a page rename or move refactor",
	},
	{
		ID:          messageIDMCPMovePageSuccess,
		Description: "Returned after a page is moved through MCP.",
		Other:       "Page moved",
	},
	{
		ID:          messageIDMCPSortPagesSuccess,
		Description: "Returned after child pages are sorted through MCP.",
		Other:       "Pages sorted successfully",
	},
	{
		ID:          messageIDAuthInvalidCreds,
		Description: "Returned when login credentials are not accepted.",
		Other:       "Invalid credentials",
	},
	{
		ID:          messageIDAuthEmailInvalid,
		Description: "Returned when an auth email field is not syntactically valid.",
		Other:       "Email is not valid",
	},
	{
		ID:          messageIDValidationFieldError,
		Description: "Fallback field validation message.",
		Other:       "Validation error",
	},
	{
		ID:          messageIDValidationBrandingNameReq,
		Description: "Returned when branding site name is empty.",
		Other:       "Site name must not be empty",
	},
	{
		ID:          messageIDValidationBrandingNameLong,
		Description: "Returned when branding site name is too long.",
		Other:       "Site name must not exceed the maximum length",
	},
	{
		ID:          messageIDValidationBrandingNameControl,
		Description: "Returned when branding site name has control characters.",
		Other:       "Site name contains invalid control characters",
	},
	{
		ID:          messageIDValidationPageTitleReq,
		Description: "Returned when a page title is empty.",
		Other:       "Title must not be empty",
	},
	{
		ID:          messageIDValidationPageKindReq,
		Description: "Returned when a page kind is missing.",
		Other:       "Kind must be specified",
	},
	{
		ID:          messageIDValidationPageKindInvalid,
		Description: "Returned when a page kind is invalid.",
		Other:       "Kind must be either 'page' or 'section'",
	},
	{
		ID:          messageIDValidationPageSlugInvalid,
		Description: "Returned when a page slug is invalid.",
		Other:       "Slug is invalid",
	},
	{
		ID:          messageIDValidationPagePathReq,
		Description: "Returned when a page path is empty.",
		Other:       "Path must not be empty",
	},
	{
		ID:          messageIDValidationPagePathInvalid,
		Description: "Returned when a page path is invalid.",
		Other:       "Path is invalid",
	},
	{
		ID:          messageIDValidationPageTagReq,
		Description: "Returned when a page tag is empty.",
		Other:       "Tag must not be empty",
	},
	{
		ID:          messageIDValidationPageTagWhitespace,
		Description: "Returned when a page tag has leading or trailing whitespace.",
		Other:       "Tag must not contain leading or trailing whitespace",
	},
	{
		ID:          messageIDValidationPageTagDuplicate,
		Description: "Returned when a page tag is duplicated.",
		Other:       "Tag must be unique",
	},
	{
		ID:          messageIDValidationPagePropertyKeyReq,
		Description: "Returned when a page property key is empty.",
		Other:       "Property key must not be empty",
	},
	{
		ID:          messageIDValidationPagePropertyKeyWhitespace,
		Description: "Returned when a page property key has leading or trailing whitespace.",
		Other:       "Property key must not contain leading or trailing whitespace",
	},
	{
		ID:          messageIDValidationPagePropertyKeyReserved,
		Description: "Returned when a page property key is reserved.",
		Other:       "Property key is reserved",
	},
	{
		ID:          messageIDValidationPagePropertyKeyReservedPrefix,
		Description: "Returned when a page property key uses a reserved prefix.",
		Other:       "Property key uses a reserved prefix",
	},
	{
		ID:          messageIDValidationAuthUsernameReq,
		Description: "Returned when username is empty.",
		Other:       "Username must not be empty",
	},
	{
		ID:          messageIDValidationAuthEmailReq,
		Description: "Returned when email is empty.",
		Other:       "Email must not be empty",
	},
	{
		ID:          messageIDValidationAuthPasswordReq,
		Description: "Returned when password is empty.",
		Other:       "Password must not be empty",
	},
	{
		ID:          messageIDValidationAuthPasswordShort,
		Description: "Returned when password is too short.",
		Other:       "Password must be at least 8 characters long",
	},
	{
		ID:          messageIDValidationAuthRoleInvalid,
		Description: "Returned when auth role is invalid.",
		Other:       "Invalid role",
	},
	{
		ID:          messageIDValidationAuthNewPasswordReq,
		Description: "Returned when new password is empty.",
		Other:       "New password must not be empty",
	},
	{
		ID:          messageIDValidationAuthNewPasswordShort,
		Description: "Returned when new password is too short.",
		Other:       "New password must be at least 8 characters long",
	},
	{
		ID:          messageIDValidationAuthOldPasswordIncorrect,
		Description: "Returned when old password is incorrect.",
		Other:       "Old password is incorrect",
	},
	{
		ID:          messageIDValidationAuthAPIKeyNameReq,
		Description: "Returned when API key name is empty.",
		Other:       "Name must not be empty",
	},
	{
		ID:          messageIDValidationAuthAPIKeyNameLong,
		Description: "Returned when API key name is too long.",
		Other:       "Name must be at most 80 characters long",
	},
	{
		ID:          messageIDValidationAuthCurrentPasswordReq,
		Description: "Returned when current password is empty.",
		Other:       "Current password must not be empty",
	},
	{
		ID:          messageIDValidationAuthCurrentPasswordIncorrect,
		Description: "Returned when current password is incorrect.",
		Other:       "Current password is incorrect",
	},
	{
		ID:          MessageIDCLIHelpUsage,
		Description: "Top-level LeafWiki CLI usage line.",
		Other:       "Usage: leafwiki [command]",
	},
	{
		ID:          MessageIDCLIHelpBody,
		Description: "Top-level LeafWiki CLI help body.",
		Other: `LeafWiki – lightweight selfhosted wiki 🌿

	Usage:
	leafwiki --jwt-secret <SECRET> --admin-password <PASSWORD> [--host <HOST>] [--port <PORT>] [--data-dir <DIR>] [--root-dir <DIR>]
	leafwiki --disable-auth [--host <HOST>] [--port <PORT>] [--data-dir <DIR>] [--root-dir <DIR>]
	leafwiki --mcp=stdio --disable-auth [--host <HOST>] [--port <PORT>] [--data-dir <DIR>] [--root-dir <DIR>]
	leafwiki agent-hook <codex|claude|cursor|unknown> [--host <HOST>] [--port <PORT>] [--data-dir <DIR>] [--root-dir <DIR>]
	leafwiki daemon
	leafwiki reset-admin-password
	leafwiki --help

	Service mode:
	leafwiki daemon reads ~/.leafwiki/leafwiki.yml and runs the install-wide wikid/frontd/workspaced runtime in the foreground.

	Options:
	--host             Host/IP address to bind the server to (default: 127.0.0.1)
	--port             Port to run the server on (default: 8080)
	--data-dir         Path to data directory (default: ./data)
	--root-dir         Path to managed markdown content directory (default: <data-dir>/root)
	--markdown-link-root-prefix Repository-root prefix for absolute Markdown links (for example /docs) (default: "")
	--admin-password   Initial admin password (used only if no admin exists)
	--jwt-secret       Secret for signing auth tokens (JWT) (required)
	--public-access    Allow public access to the wiki only with read access (default: false)
	--allow-insecure   Allow insecure HTTP connections (default: false)
	--access-token-timeout  Access token timeout duration (e.g. 24h, 15m) (default: 15m)
	--refresh-token-timeout Refresh token timeout duration (e.g. 168h, 7d) (default: 7d)
	--inject-code-in-header  Raw HTML/JS code injected into <head> tag (e.g., analytics, custom CSS) (default: "")
	                         WARNING: Use only with trusted code to avoid XSS vulnerabilities. No sanitization is performed.
	--custom-stylesheet      Path to a .css file inside the data dir, served publicly as /custom.css
	                         (or <base-path>/custom.css when --base-path is set) (default: "")
	--log-target             Log target: file, stderr, or stdout (default: file)
	--log-file               Log file path when --log-target=file; relative paths resolve under --data-dir
	                         (default: <data-dir>/.leafwiki/logs/leafwiki.log)
	--disable-auth                Disable authentication completely (default: false) (WARNING: only use in trusted networks!)
	--hide-link-metadata-section  Hide link metadata section in the frontend UI (default: false)
	--base-path                   URL prefix when served behind a reverse proxy (e.g. /wiki) (default: "")
	--max-asset-upload-size       Maximum size for asset uploads (for example 50MiB, 50MB, 52428800) (default: 50MiB)
	--enable-link-refactor        Enable the link refactoring dialog and rewrite flow (default: false)
	--mcp                         MCP transports: none, http, stdio, http,stdio, or stdio,http (default: none)
	--api-key                     Native STDIO MCP API key convenience flag; prefer LEAFWIKI_MCP_API_KEY
	--daemon-idle-timeout         Federated runtime idle timeout after the last session or presence record exits; 0 stops immediately (default: 10m)
	--enable-http-remote-user       Enable reverse-proxy authentication via HTTP header (default: false)
	--http-remote-user-header-name  HTTP header carrying the username from a trusted proxy (default: Remote-User)
	--trusted-proxy-ips             Comma-separated trusted proxy IPs/CIDRs (e.g. 127.0.0.1,172.18.0.0/16)
	--http-remote-user-logout-url   URL the frontend redirects to after logout in proxy-auth mode (default: "")
	--disable-request-log           Suppress per-request HTTP access log lines (default: false)
	--config                        Path to flat YAML config file; mutually exclusive with other CLI flags

	Environment variables:
	LEAFWIKI_HOST
	LEAFWIKI_PORT
	LEAFWIKI_DATA_DIR
	LEAFWIKI_ROOT_DIR
	LEAFWIKI_MARKDOWN_LINK_ROOT_PREFIX
	LEAFWIKI_JWT_SECRET
	LEAFWIKI_LOG_LEVEL
	LEAFWIKI_ADMIN_PASSWORD
	LEAFWIKI_PUBLIC_ACCESS
	LEAFWIKI_ALLOW_INSECURE
	LEAFWIKI_INJECT_CODE_IN_HEADER
	LEAFWIKI_CUSTOM_STYLESHEET
	LEAFWIKI_LOG_TARGET
	LEAFWIKI_LOG_FILE
	LEAFWIKI_ACCESS_TOKEN_TIMEOUT
	LEAFWIKI_REFRESH_TOKEN_TIMEOUT
	LEAFWIKI_DISABLE_AUTH
	LEAFWIKI_HIDE_LINK_METADATA_SECTION
	LEAFWIKI_BASE_PATH
	LEAFWIKI_MAX_ASSET_UPLOAD_SIZE
	LEAFWIKI_ENABLE_LINK_REFACTOR
	LEAFWIKI_MCP
	LEAFWIKI_MCP_API_KEY
	LEAFWIKI_DAEMON_IDLE_TIMEOUT
	LEAFWIKI_ENABLE_HTTP_REMOTE_USER
	LEAFWIKI_HTTP_REMOTE_USER_HEADER_NAME
	LEAFWIKI_TRUSTED_PROXY_IPS
	LEAFWIKI_HTTP_REMOTE_USER_LOGOUT_URL
	LEAFWIKI_DISABLE_REQUEST_LOG
	`,
	},
	{
		ID:          MessageIDShellRunUsage,
		Description: "Usage line for scripts/run.sh.",
		Other:       "Usage: scripts/run.sh <mcp|agent-hook> [options]",
	},
	{
		ID:          MessageIDShellRunHelpBody,
		Description: "Help body for scripts/run.sh.",
		Other: `Modes:
  mcp                     Run native LeafWiki MCP STDIO frontend
  agent-hook <provider>   Run one LeafWiki agent hook invocation

Options:
  --leafwiki-bin <path>     LeafWiki executable (default: leafwiki)
  --scheme <scheme>         Informational URL scheme for dry-run output (default: http)
  --host <host>             LeafWiki bind host (default: 127.0.0.1)
  --port <port>             LeafWiki port (default: 8080)
  --base-path <path>        LeafWiki base path, if any
  --data-dir <path>         LeafWiki data directory (default: ./.wiki)
  --root-dir <path>         LeafWiki root markdown directory (default: ./wiki)
  --markdown-link-root-prefix <path>
                            Repository-root Markdown href prefix, such as /docs
  --jwt-secret <secret>     JWT secret only when this run must bootstrap an auth-enabled owner
  --admin-password <pass>   Admin password only when this run must bootstrap an auth-enabled owner
  --disable-auth            Force disabled-auth STDIO identity
  --allow-insecure          Pass --allow-insecure to LeafWiki (default)
  --no-allow-insecure       Do not pass --allow-insecure
  --request-log             Keep LeafWiki request logs enabled
  --disable-request-log     Pass --disable-request-log to LeafWiki (default)
  --daemon-idle-timeout <d> Federated runtime idle timeout after the last session or presence record exits (default: 10m)
  --api-key <key>           Native STDIO API key; passed as LEAFWIKI_MCP_API_KEY
  --config <path>           Pass a LeafWiki YAML config file without wrapper defaults
  --server-arg <arg>        Extra argument passed to leafwiki; repeatable
  --dry-run                 Print the planned command without starting anything
  -h, --help                Show this help

Environment overrides use LEAFWIKI_RUN_MCP_* names matching the option names.
Use LEAFWIKI_RUN_MCP_API_KEY or LEAFWIKI_MCP_API_KEY to provide the native
STDIO API key without putting the secret in the child command line.`,
	},
	{
		ID:          MessageIDShellRunErrorPrefix,
		Description: "Error prefix for scripts/run.sh.",
		Other:       "Error:",
	},
	{
		ID:          MessageIDShellRunDryRunMCPConfig,
		Description: "Dry-run status when scripts/run.sh starts MCP from YAML config.",
		Other:       "Would run LeafWiki with YAML config for MCP",
	},
	{
		ID:          MessageIDShellRunDryRunMCPNative,
		Description: "Dry-run status when scripts/run.sh starts native MCP STDIO.",
		Other:       "Would run LeafWiki native MCP STDIO",
	},
	{
		ID:          MessageIDShellRunDryRunSTDIOAttach,
		Description: "Dry-run detail for native MCP STDIO descriptor attach.",
		Other:       "STDIO attach: descriptor-first attach via <data-dir>/.leafwiki/project-daemon.json; wikid ensure/control for missing or stale descriptors",
	},
	{
		ID:          MessageIDShellRunDryRunAgentHook,
		Description: "Dry-run status when scripts/run.sh starts an agent hook.",
		Other:       "Would run LeafWiki agent hook",
	},
	{
		ID:          MessageIDShellRunDryRunHTTPConfig,
		Description: "Dry-run HTTP UI status for YAML-configured scripts/run.sh.",
		Other:       "HTTP UI: configured by",
	},
	{
		ID:          MessageIDShellRunDryRunHTTPURL,
		Description: "Dry-run HTTP UI status for URL-configured scripts/run.sh.",
		Other:       "HTTP UI:",
	},
}

func Definitions() []Definition {
	messages := make([]*i18n.Message, 0, len(registryMessages)+len(derivedErrorMessages))
	messages = append(messages, registryMessages...)
	messages = append(messages, derivedErrorMessages...)

	definitions := make([]Definition, 0, len(messages))
	for _, message := range messages {
		if message == nil {
			continue
		}
		definitions = append(definitions, Definition{
			ID:          message.ID,
			Default:     message.Other,
			Description: message.Description,
		})
	}
	return definitions
}
