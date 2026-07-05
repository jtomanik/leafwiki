package localization

import "github.com/nicksnyder/go-i18n/v2/i18n"

var derivedErrorMessagesWorkspace = []*i18n.Message{
	{
		ID:          "errors.private.registry_load_failed",
		Description: "Derived from ErrorCode private_registry_load_failed.",
		Other:       "Private registry load failed",
	},
	{
		ID:          "errors.private.subject_resolve_failed",
		Description: "Derived from ErrorCode private_subject_resolve_failed.",
		Other:       "Private subject resolve failed",
	},
	{
		ID:          "errors.private.unauthorized",
		Description: "Derived from ErrorCode private_unauthorized.",
		Other:       "Private unauthorized",
	},
	{
		ID:          "errors.private.workspace_auth_failed",
		Description: "Derived from ErrorCode private_workspace_auth_failed.",
		Other:       "Private workspace auth failed",
	},
	{
		ID:          "errors.private.workspace_ensure_failed",
		Description: "Derived from ErrorCode private_workspace_ensure_failed.",
		Other:       "Private workspace ensure failed",
	},
	{
		ID:          "errors.properties.internal_error",
		Description: "Derived from ErrorCode properties_internal_error.",
		Other:       "Internal server error",
	},
	{
		ID:          "errors.properties.invalid_limit",
		Description: "Derived from ErrorCode properties_invalid_limit.",
		Other:       "Properties invalid limit",
	},
	{
		ID:          "errors.properties.missing_key",
		Description: "Derived from ErrorCode properties_missing_key.",
		Other:       "Query parameter 'key' is required",
	},
	{
		ID:          "errors.properties.missing_value",
		Description: "Derived from ErrorCode properties_missing_value.",
		Other:       "Query parameter 'value' is required",
	},
	{
		ID:          "errors.rate.limit_exceeded",
		Description: "Derived from ErrorCode rate_limit_exceeded.",
		Other:       "Too many requests, please try again later",
	},
	{
		ID:          "errors.runtime.config_usage",
		Description: "Derived from ErrorCode runtime_config_usage.",
		Other:       "Runtime config usage",
	},
	{
		ID:          "errors.revision.compare_invalid_request",
		Description: "Derived from ErrorCode revision_compare_invalid_request.",
		Other:       "Revision compare request is invalid",
	},
	{
		ID:          "errors.revision.internal_error",
		Description: "Derived from ErrorCode revision_internal_error.",
		Other:       "Failed to list revisions",
	},
	{
		ID:          "errors.revision.invalid_limit",
		Description: "Derived from ErrorCode revision_invalid_limit.",
		Other:       "Revision list limit is invalid",
	},
	{
		ID:          "errors.revision.invalid_page_id",
		Description: "Derived from ErrorCode revision_invalid_page_id.",
		Other:       "Page ID is required",
	},
	{
		ID:          "errors.revision.invalid_revision_id",
		Description: "Derived from ErrorCode revision_invalid_revision_id.",
		Other:       "Revision ID is required",
	},
	{
		ID:          "errors.revision.not_found",
		Description: "Derived from ErrorCode revision_not_found.",
		Other:       "Revision asset not found",
	},
	{
		ID:          "errors.revision.preview_asset_blob_unavailable",
		Description: "Derived from ErrorCode revision_preview_asset_blob_unavailable.",
		Other:       "Revision asset blob is unavailable",
	},
	{
		ID:          "errors.revision.preview_asset_invalid_name",
		Description: "Derived from ErrorCode revision_preview_asset_invalid_name.",
		Other:       "Revision asset name is invalid",
	},
	{
		ID:          "errors.revision.preview_asset_not_found",
		Description: "Derived from ErrorCode revision_preview_asset_not_found.",
		Other:       "Revision asset not found",
	},
	{
		ID:          "errors.revision.preview_assets_unavailable",
		Description: "Derived from ErrorCode revision_preview_assets_unavailable.",
		Other:       "Revision assets are unavailable",
	},
	{
		ID:          "errors.revision.preview_content_unavailable",
		Description: "Derived from ErrorCode revision_preview_content_unavailable.",
		Other:       "Revision content is unavailable",
	},
	{
		ID:          "errors.revision.restore_assets_missing",
		Description: "Derived from ErrorCode revision_restore_assets_missing.",
		Other:       "Restore assets are unavailable",
	},
	{
		ID:          "errors.revision.restore_content_missing",
		Description: "Derived from ErrorCode revision_restore_content_missing.",
		Other:       "Restore content is unavailable",
	},
	{
		ID:          "errors.revision.restore_failed",
		Description: "Derived from ErrorCode revision_restore_failed.",
		Other:       "Failed to restore page",
	},
	{
		ID:          "errors.revision.restore_invalid_page_id",
		Description: "Derived from ErrorCode revision_restore_invalid_page_id.",
		Other:       "Failed to restore page",
	},
	{
		ID:          "errors.revision.restore_invalid_revision",
		Description: "Derived from ErrorCode revision_restore_invalid_revision.",
		Other:       "Restore revision is invalid",
	},
	{
		ID:          "errors.revision.restore_page_not_found",
		Description: "Derived from ErrorCode revision_restore_page_not_found.",
		Other:       "Page not found",
	},
	{
		ID:          "errors.revision.restore_revision_not_found",
		Description: "Derived from ErrorCode revision_restore_revision_not_found.",
		Other:       "Restore revision not found",
	},
	{
		ID:          "errors.revision.service_unavailable",
		Description: "Derived from ErrorCode revision_service_unavailable.",
		Other:       "Revision service unavailable",
	},
	{
		ID:          "errors.search.internal_error",
		Description: "Derived from ErrorCode search_internal_error.",
		Other:       "Failed to perform search",
	},
	{
		ID:          "errors.search.invalid_limit",
		Description: "Derived from ErrorCode search_invalid_limit.",
		Other:       "Invalid limit value",
	},
	{
		ID:          "errors.search.invalid_offset",
		Description: "Derived from ErrorCode search_invalid_offset.",
		Other:       "Invalid offset value",
	},
	{
		ID:          "errors.search.missing_query",
		Description: "Derived from ErrorCode search_missing_query.",
		Other:       "Query parameter 'q' is required",
	},
	{
		ID:          "errors.search.unavailable",
		Description: "Derived from ErrorCode search_unavailable.",
		Other:       "Search is currently unavailable",
	},
	{
		ID:          "errors.stdio.auth_api_key_invalid",
		Description: "Derived from ErrorCode stdio_auth_api_key_invalid.",
		Other:       "STDIO auth API key invalid",
	},
	{
		ID:          "errors.stdio.auth_api_key_rejected",
		Description: "Derived from ErrorCode stdio_auth_api_key_rejected.",
		Other:       "STDIO auth API key rejected",
	},
	{
		ID:          "errors.stdio.auth_api_key_required",
		Description: "Derived from ErrorCode stdio_auth_api_key_required.",
		Other:       "STDIO auth API key required",
	},
	{
		ID:          "errors.stdio.auth_api_key_verifier_failed",
		Description: "Derived from ErrorCode stdio_auth_api_key_verifier_failed.",
		Other:       "STDIO auth API key verifier failed",
	},
	{
		ID:          "errors.stdio.auth_api_key_verifier_unavailable",
		Description: "Derived from ErrorCode stdio_auth_api_key_verifier_unavailable.",
		Other:       "STDIO auth API key verifier unavailable",
	},
	{
		ID:          "errors.stdio.auth_invalid_request",
		Description: "Derived from ErrorCode stdio_auth_invalid_request.",
		Other:       "STDIO auth invalid request",
	},
	{
		ID:          "errors.tags.internal_error",
		Description: "Derived from ErrorCode tags_internal_error.",
		Other:       "Internal server error",
	},
	{
		ID:          "errors.tags.invalid_limit",
		Description: "Derived from ErrorCode tags_invalid_limit.",
		Other:       "Tags invalid limit",
	},
	{
		ID:          "errors.tags.missing_param",
		Description: "Derived from ErrorCode tags_missing_param.",
		Other:       "Query parameter 'tags' is required",
	},
	{
		ID:          "errors.workspace.actor_context_encode_failed",
		Description: "Derived from ErrorCode workspace_actor_context_encode_failed.",
		Other:       "Workspace actor context encode failed",
	},
	{
		ID:          "errors.workspace.actor_context_failed",
		Description: "Derived from ErrorCode workspace_actor_context_failed.",
		Other:       "Workspace actor context failed",
	},
	{
		ID:          "errors.workspace.actor_context_unavailable",
		Description: "Derived from ErrorCode workspace_actor_context_unavailable.",
		Other:       "Workspace actor context unavailable",
	},
	{
		ID:          "errors.workspace.ambiguous",
		Description: "Derived from ErrorCode workspace_ambiguous.",
		Other:       "Workspace ambiguous",
	},
	{
		ID:          "errors.workspace.forbidden",
		Description: "Derived from ErrorCode workspace_forbidden.",
		Other:       "Workspace forbidden",
	},
	{
		ID:          "errors.workspace.id_invalid",
		Description: "Derived from ErrorCode workspace_id_invalid.",
		Other:       "Workspace ID invalid",
	},
	{
		ID:          "errors.workspace.id_required",
		Description: "Derived from ErrorCode workspace_id_required.",
		Other:       "Workspace ID required",
	},
	{
		ID:          "errors.workspace.id_whitespace",
		Description: "Derived from ErrorCode workspace_id_whitespace.",
		Other:       "Workspace ID whitespace",
	},
	{
		ID:          "errors.workspace.not_found",
		Description: "Derived from ErrorCode workspace_not_found.",
		Other:       "Workspace not found",
	},
	{
		ID:          "errors.workspace.resolver_unavailable",
		Description: "Derived from ErrorCode workspace_resolver_unavailable.",
		Other:       "Workspace resolver unavailable",
	},
	{
		ID:          "errors.workspace.sync_disabled",
		Description: "Derived from ErrorCode workspace_sync_disabled.",
		Other:       "workspace sync is not enabled",
	},
	{
		ID:          "errors.workspace.sync_failed",
		Description: "Derived from ErrorCode workspace_sync_failed.",
		Other:       "Workspace sync failed",
	},
	{
		ID:          "errors.workspace.sync_invalid_cursor",
		Description: "Derived from ErrorCode workspace_sync_invalid_cursor.",
		Other:       "invalid snapshot cursor",
	},
	{
		ID:          "errors.workspace.sync_invalid_limit",
		Description: "Derived from ErrorCode workspace_sync_invalid_limit.",
		Other:       "invalid snapshot limit",
	},
	{
		ID:          "errors.workspace.unavailable",
		Description: "Derived from ErrorCode workspace_unavailable.",
		Other:       "Workspace unavailable",
	},
	{
		ID:          "errors.workspaced.unavailable",
		Description: "Derived from ErrorCode workspaced_unavailable.",
		Other:       "Workspaced unavailable",
	},
}
