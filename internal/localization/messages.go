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
	MessageIDCLIStatusAdminPasswordReset             = "cli.status.admin_password_reset"
	MessageIDCLIStatusAdminPasswordValue             = "cli.status.admin_password_value"
	MessageIDCLIStatusRemoveWorkspaceDescriptor      = "cli.status.remove_workspace_descriptor"
	MessageIDCLIStatusUnknownCommand                 = "cli.status.unknown_command"
	MessageIDCLIErrorStdioAuthAPIKeyConflict         = "cli.error.stdio_auth_api_key_conflict"
	MessageIDCLIErrorStdioAuthIdentityRequired       = "cli.error.stdio_auth_identity_required"
	MessageIDCLIErrorStdoutReservedForMCPStdio       = "cli.error.stdout_reserved_for_mcp_stdio"
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

var registryMessages = combineLocalizationMessages(
	registryMessagesCore,
	registryMessagesMCP,
	registryMessagesValidation,
)

func combineLocalizationMessages(parts ...[]*i18n.Message) []*i18n.Message {
	count := 0
	for _, part := range parts {
		count += len(part)
	}
	messages := make([]*i18n.Message, 0, count)
	for _, part := range parts {
		messages = append(messages, part...)
	}
	return messages
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
