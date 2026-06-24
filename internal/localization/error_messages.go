package localization

import "github.com/nicksnyder/go-i18n/v2/i18n"

// derivedErrorMessages covers MessageIDForCode-derived backend error IDs.
var derivedErrorMessages = []*i18n.Message{
	{
		ID:          "errors.asset.already_exists",
		Description: "Derived from ErrorCode asset_already_exists.",
		Other:       "Asset already exists",
	},
	{
		ID:          "errors.asset.blob_hash_mismatch",
		Description: "Derived from ErrorCode asset_blob_hash_mismatch.",
		Other:       "Asset blob hash mismatch",
	},
	{
		ID:          "errors.asset.blob_size_mismatch",
		Description: "Derived from ErrorCode asset_blob_size_mismatch.",
		Other:       "Asset blob size mismatch",
	},
	{
		ID:          "errors.asset.delete_failed",
		Description: "Derived from ErrorCode asset_delete_failed.",
		Other:       "Failed to delete asset",
	},
	{
		ID:          "errors.asset.file_too_large",
		Description: "Derived from ErrorCode asset_file_too_large.",
		Other:       "File is too large",
	},
	{
		ID:          "errors.asset.internal_error",
		Description: "Derived from ErrorCode asset_internal_error.",
		Other:       "Asset request failed",
	},
	{
		ID:          "errors.asset.invalid_extension",
		Description: "Derived from ErrorCode asset_invalid_extension.",
		Other:       "Asset extension must not change",
	},
	{
		ID:          "errors.asset.invalid_name",
		Description: "Derived from ErrorCode asset_invalid_name.",
		Other:       "Invalid asset name",
	},
	{
		ID:          "errors.asset.invalid_payload",
		Description: "Derived from ErrorCode asset_invalid_payload.",
		Other:       "Invalid asset payload",
	},
	{
		ID:          "errors.asset.missing_file",
		Description: "Derived from ErrorCode asset_missing_file.",
		Other:       "Missing file",
	},
	{
		ID:          "errors.asset.missing_name",
		Description: "Derived from ErrorCode asset_missing_name.",
		Other:       "Missing filename",
	},
	{
		ID:          "errors.asset.not_found",
		Description: "Derived from ErrorCode asset_not_found.",
		Other:       "Asset not found",
	},
	{
		ID:          "errors.asset.page_not_found",
		Description: "Derived from ErrorCode asset_page_not_found.",
		Other:       "Page not found",
	},
	{
		ID:          "errors.asset.read_failed",
		Description: "Derived from ErrorCode asset_read_failed.",
		Other:       "Failed to read asset",
	},
	{
		ID:          "errors.asset.rename_failed",
		Description: "Derived from ErrorCode asset_rename_failed.",
		Other:       "Failed to rename asset",
	},
	{
		ID:          "errors.asset.upload_failed",
		Description: "Derived from ErrorCode asset_upload_failed.",
		Other:       "Failed to upload asset",
	},
	{
		ID:          "errors.auth.access_token_missing",
		Description: "Derived from ErrorCode auth_access_token_missing.",
		Other:       "Missing or invalid access token",
	},
	{
		ID:          "errors.auth.account_locked",
		Description: "Derived from ErrorCode auth_account_locked.",
		Other:       "Account temporarily locked due to too many failed login attempts",
	},
	{
		ID:          "errors.auth.admin_cannot_delete",
		Description: "Derived from ErrorCode auth_admin_cannot_delete.",
		Other:       "Admin user cannot be deleted",
	},
	{
		ID:          "errors.auth.admin_disabled",
		Description: "Derived from ErrorCode auth_admin_disabled.",
		Other:       "Admin operations are not available when authentication is disabled",
	},
	{
		ID:          "errors.auth.admin_privileges_required",
		Description: "Derived from ErrorCode auth_admin_privileges_required.",
		Other:       "Admin privileges required",
	},
	{
		ID:          "errors.auth.cookie_failed",
		Description: "Derived from ErrorCode auth_cookie_failed.",
		Other:       "HTTPS is required for auth cookies. Use HTTPS or start LeafWiki with --allow-insecure for trusted plain HTTP setups.",
	},
	{
		ID:          "errors.auth.csrf_failed",
		Description: "Derived from ErrorCode auth_csrf_failed.",
		Other:       "Failed to clear CSRF cookie",
	},
	{
		ID:          "errors.auth.disabled",
		Description: "Derived from ErrorCode auth_disabled.",
		Other:       "Authentication is disabled",
	},
	{
		ID:          "errors.auth.disabled_missing_user",
		Description: "Derived from ErrorCode auth_disabled_missing_user.",
		Other:       "User not authenticated and auth is disabled",
	},
	{
		ID:          "errors.auth.editor_privileges_required",
		Description: "Derived from ErrorCode auth_editor_privileges_required.",
		Other:       "Editor or Admin role required",
	},
	{
		ID:          "errors.auth.forbidden",
		Description: "Derived from ErrorCode auth_forbidden.",
		Other:       "MCP API key self-creation is disabled for HTTP remote-user authentication",
	},
	{
		ID:          "errors.auth.internal_error",
		Description: "Derived from ErrorCode auth_internal_error.",
		Other:       "Authentication request failed",
	},
	{
		ID:          "errors.auth.invalid_payload",
		Description: "Derived from ErrorCode auth_invalid_payload.",
		Other:       "Invalid login payload",
	},
	{
		ID:          "errors.auth.invalid_refresh_token",
		Description: "Derived from ErrorCode auth_invalid_refresh_token.",
		Other:       "Missing or invalid refresh token",
	},
	{
		ID:          "errors.auth.invalid_request",
		Description: "Derived from ErrorCode auth_invalid_request.",
		Other:       "Invalid request",
	},
	{
		ID:          "errors.auth.invalid_role",
		Description: "Derived from ErrorCode auth_invalid_role.",
		Other:       "Invalid role",
	},
	{
		ID:          "errors.auth.invalid_user",
		Description: "Derived from ErrorCode auth_invalid_user.",
		Other:       "Invalid user",
	},
	{
		ID:          "errors.auth.invalid_user_context",
		Description: "Derived from ErrorCode auth_invalid_user_context.",
		Other:       "Invalid user context",
	},
	{
		ID:          "errors.auth.last_admin_cannot_be_demoted",
		Description: "Derived from ErrorCode auth_last_admin_cannot_be_demoted.",
		Other:       "Cannot remove admin role from the last admin user",
	},
	{
		ID:          "errors.auth.remote_user_not_found",
		Description: "Derived from ErrorCode auth_remote_user_not_found.",
		Other:       "reverse proxy auth: user not found",
	},
	{
		ID:          "errors.auth.reverse_proxy_misconfigured",
		Description: "Derived from ErrorCode auth_reverse_proxy_misconfigured.",
		Other:       "Reverse proxy authentication misconfigured",
	},
	{
		ID:          "errors.auth.self_required",
		Description: "Derived from ErrorCode auth_self_required.",
		Other:       "You can only access your own account",
	},
	{
		ID:          "errors.auth.service_unavailable",
		Description: "Derived from ErrorCode auth_service_unavailable.",
		Other:       "Authentication service unavailable",
	},
	{
		ID:          "errors.auth.token_expired",
		Description: "Derived from ErrorCode auth_token_expired.",
		Other:       "Auth token expired",
	},
	{
		ID:          "errors.auth.token_invalid",
		Description: "Derived from ErrorCode auth_token_invalid.",
		Other:       "Invalid or expired token",
	},
	{
		ID:          "errors.auth.user_already_exists",
		Description: "Derived from ErrorCode auth_user_already_exists.",
		Other:       "User already exists",
	},
	{
		ID:          "errors.auth.user_not_authenticated",
		Description: "Derived from ErrorCode auth_user_not_authenticated.",
		Other:       "User not authenticated",
	},
	{
		ID:          "errors.auth.user_not_found",
		Description: "Derived from ErrorCode auth_user_not_found.",
		Other:       "User not found",
	},
	{
		ID:          "errors.branding.config_unavailable",
		Description: "Derived from ErrorCode branding_config_unavailable.",
		Other:       "Failed to load branding config",
	},
	{
		ID:          "errors.branding.favicon_delete_failed",
		Description: "Derived from ErrorCode branding_favicon_delete_failed.",
		Other:       "Failed to delete favicon",
	},
	{
		ID:          "errors.branding.favicon_invalid_type",
		Description: "Derived from ErrorCode branding_favicon_invalid_type.",
		Other:       "Invalid favicon file type",
	},
	{
		ID:          "errors.branding.favicon_missing",
		Description: "Derived from ErrorCode branding_favicon_missing.",
		Other:       "Missing file",
	},
	{
		ID:          "errors.branding.favicon_too_large",
		Description: "Derived from ErrorCode branding_favicon_too_large.",
		Other:       "File too large",
	},
	{
		ID:          "errors.branding.favicon_upload_failed",
		Description: "Derived from ErrorCode branding_favicon_upload_failed.",
		Other:       "Failed to save favicon file",
	},
	{
		ID:          "errors.branding.internal_error",
		Description: "Derived from ErrorCode branding_internal_error.",
		Other:       "Branding request failed",
	},
	{
		ID:          "errors.branding.invalid_payload",
		Description: "Derived from ErrorCode branding_invalid_payload.",
		Other:       "Invalid payload",
	},
	{
		ID:          "errors.branding.logo_delete_failed",
		Description: "Derived from ErrorCode branding_logo_delete_failed.",
		Other:       "Failed to delete logo",
	},
	{
		ID:          "errors.branding.logo_invalid_type",
		Description: "Derived from ErrorCode branding_logo_invalid_type.",
		Other:       "Invalid logo file type",
	},
	{
		ID:          "errors.branding.logo_missing",
		Description: "Derived from ErrorCode branding_logo_missing.",
		Other:       "Missing file",
	},
	{
		ID:          "errors.branding.logo_too_large",
		Description: "Derived from ErrorCode branding_logo_too_large.",
		Other:       "File too large",
	},
	{
		ID:          "errors.branding.logo_upload_failed",
		Description: "Derived from ErrorCode branding_logo_upload_failed.",
		Other:       "Failed to save logo file",
	},
	{
		ID:          "errors.branding.update_failed",
		Description: "Derived from ErrorCode branding_update_failed.",
		Other:       "Failed to update branding",
	},
	{
		ID:          "errors.csrf.token_invalid",
		Description: "Derived from ErrorCode csrf_token_invalid.",
		Other:       "Invalid CSRF token",
	},
	{
		ID:          "errors.csrf.token_missing",
		Description: "Derived from ErrorCode csrf_token_missing.",
		Other:       "CSRF token missing",
	},
	{
		ID:          "errors.daemon.agent_presence_invalid_request",
		Description: "Derived from ErrorCode daemon_agent_presence_invalid_request.",
		Other:       "Daemon agent presence invalid request",
	},
	{
		ID:          "errors.daemon.control_unauthorized",
		Description: "Derived from ErrorCode daemon_control_unauthorized.",
		Other:       "Daemon control unauthorized",
	},
	{
		ID:          "errors.daemon.control_encode_response",
		Description: "Derived from ErrorCode daemon_control_encode_response.",
		Other:       "Daemon control encode response",
	},
	{
		ID:          "errors.daemon.session_not_found",
		Description: "Derived from ErrorCode daemon_session_not_found.",
		Other:       "Daemon session not found",
	},
	{
		ID:          "errors.daemon.session_register_failed",
		Description: "Derived from ErrorCode daemon_session_register_failed.",
		Other:       "Daemon session register failed",
	},
	{
		ID:          "errors.importer.execution_running",
		Description: "Derived from ErrorCode importer_execution_running.",
		Other:       "Import is already running",
	},
	{
		ID:          "errors.importer.file_open_failed",
		Description: "Derived from ErrorCode importer_file_open_failed.",
		Other:       "Failed to open uploaded file",
	},
	{
		ID:          "errors.importer.internal_error",
		Description: "Derived from ErrorCode importer_internal_error.",
		Other:       "Importer request failed",
	},
	{
		ID:          "errors.importer.missing_file",
		Description: "Derived from ErrorCode importer_missing_file.",
		Other:       "Missing file",
	},
	{
		ID:          "errors.importer.no_plan",
		Description: "Derived from ErrorCode importer_no_plan.",
		Other:       "No import plan available",
	},
	{
		ID:          "errors.importer.state_unavailable",
		Description: "Derived from ErrorCode importer_state_unavailable.",
		Other:       "Import state is unavailable",
	},
	{
		ID:          "errors.importer.upload_too_large",
		Description: "Derived from ErrorCode importer_upload_too_large.",
		Other:       "Upload exceeds maximum size limit of 500 MiB",
	},
	{
		ID:          "errors.link.internal_error",
		Description: "Derived from ErrorCode link_internal_error.",
		Other:       "Failed to load link status",
	},
	{
		ID:          "errors.link.page_not_found",
		Description: "Derived from ErrorCode link_page_not_found.",
		Other:       "Page not found",
	},
	{
		ID:          "errors.link.service_unavailable",
		Description: "Derived from ErrorCode link_service_unavailable.",
		Other:       "Link service is unavailable",
	},
	{
		ID:          "errors.mcp.actor_context_invalid",
		Description: "Derived from ErrorCode mcp_actor_context_invalid.",
		Other:       "MCP actor context invalid",
	},
	{
		ID:          "errors.mcp.actor_context_missing",
		Description: "Derived from ErrorCode mcp_actor_context_missing.",
		Other:       "MCP actor context missing",
	},
	{
		ID:          "errors.mcp.authenticated_user_lookup_failed",
		Description: "Derived from ErrorCode mcp_authenticated_user_lookup_failed.",
		Other:       "MCP authenticated user lookup failed",
	},
	{
		ID:          "errors.mcp.authenticated_user_not_found",
		Description: "Derived from ErrorCode mcp_authenticated_user_not_found.",
		Other:       "MCP authenticated user not found",
	},
	{
		ID:          "errors.mcp.authenticated_user_service_unavailable",
		Description: "Derived from ErrorCode mcp_authenticated_user_service_unavailable.",
		Other:       "MCP authenticated user service unavailable",
	},
	{
		ID:          "errors.mcp.editor_role_required",
		Description: "Derived from ErrorCode mcp_editor_role_required.",
		Other:       "editor or admin role required",
	},
	{
		ID:          "errors.mcp.page_identifier_ambiguous",
		Description: "Derived from ErrorCode mcp_page_identifier_ambiguous.",
		Other:       "id and pageId cannot both be supplied",
	},
	{
		ID:          "errors.mcp.page_identifier_required",
		Description: "Derived from ErrorCode mcp_page_identifier_required.",
		Other:       "id or pageId is required",
	},
	{
		ID:          "errors.mcp.page_target_ambiguous",
		Description: "Derived from ErrorCode mcp_page_target_ambiguous.",
		Other:       "pageId and path cannot both be supplied",
	},
	{
		ID:          "errors.mcp.page_target_required",
		Description: "Derived from ErrorCode mcp_page_target_required.",
		Other:       "pageId or path is required",
	},
	{
		ID:          "errors.mcp.session_workspace_mismatch",
		Description: "Derived from ErrorCode mcp_session_workspace_mismatch.",
		Other:       "MCP session workspace mismatch",
	},
	{
		ID:          "errors.mcp.token_info_missing",
		Description: "Derived from ErrorCode mcp_token_info_missing.",
		Other:       "MCP token info missing",
	},
	{
		ID:          "errors.mcp.tool_error",
		Description: "Derived from ErrorCode mcp_tool_error.",
		Other:       "MCP tool failed",
	},
	{
		ID:          "errors.mcp.workspace_router_unavailable",
		Description: "Derived from ErrorCode mcp_workspace_router_unavailable.",
		Other:       "MCP workspace router unavailable",
	},
	{
		ID:          "errors.missing.asset_blob",
		Description: "Derived from ErrorCode missing_asset_blob.",
		Other:       "Missing asset blob",
	},
	{
		ID:          "errors.missing.asset_manifest",
		Description: "Derived from ErrorCode missing_asset_manifest.",
		Other:       "Missing asset manifest",
	},
	{
		ID:          "errors.missing.content_blob",
		Description: "Derived from ErrorCode missing_content_blob.",
		Other:       "Missing content blob",
	},
	{
		ID:          "errors.page.cannot_move_to_self",
		Description: "Derived from ErrorCode page_cannot_move_to_self.",
		Other:       "Page cannot move to self",
	},
	{
		ID:          "errors.page.circular_move",
		Description: "Derived from ErrorCode page_circular_move.",
		Other:       "Page circular move",
	},
	{
		ID:          "errors.page.convert_not_allowed",
		Description: "Derived from ErrorCode page_convert_not_allowed.",
		Other:       "Page convert not allowed",
	},
	{
		ID:          "errors.page.has_children",
		Description: "Derived from ErrorCode page_has_children.",
		Other:       "Page has children",
	},
	{
		ID:          "errors.page.internal_error",
		Description: "Derived from ErrorCode page_internal_error.",
		Other:       "Failed to parse metadata",
	},
	{
		ID:          "errors.page.invalid_kind",
		Description: "Derived from ErrorCode page_invalid_kind.",
		Other:       "Invalid kind",
	},
	{
		ID:          "errors.page.invalid_parent_id",
		Description: "Derived from ErrorCode page_invalid_parent_id.",
		Other:       "Invalid parentId",
	},
	{
		ID:          "errors.page.invalid_path",
		Description: "Derived from ErrorCode page_invalid_path.",
		Other:       "Invalid path",
	},
	{
		ID:          "errors.page.invalid_payload",
		Description: "Derived from ErrorCode page_invalid_payload.",
		Other:       "Invalid payload",
	},
	{
		ID:          "errors.page.invalid_refactor_kind",
		Description: "Derived from ErrorCode page_invalid_refactor_kind.",
		Other:       "Invalid refactor kind",
	},
	{
		ID:          "errors.page.invalid_request",
		Description: "Derived from ErrorCode page_invalid_request.",
		Other:       "Invalid request",
	},
	{
		ID:          "errors.page.invalid_target_kind",
		Description: "Derived from ErrorCode page_invalid_target_kind.",
		Other:       "Invalid targetKind",
	},
	{
		ID:          "errors.page.invalid_title",
		Description: "Derived from ErrorCode page_invalid_title.",
		Other:       "Title must include at least one slug character",
	},
	{
		ID:          "errors.page.missing_id",
		Description: "Derived from ErrorCode page_missing_id.",
		Other:       "Page ID is required",
	},
	{
		ID:          "errors.page.missing_path",
		Description: "Derived from ErrorCode page_missing_path.",
		Other:       "Missing path",
	},
	{
		ID:          "errors.page.missing_title",
		Description: "Derived from ErrorCode page_missing_title.",
		Other:       "Title query param is required",
	},
	{
		ID:          "errors.page.not_found",
		Description: "Derived from ErrorCode page_not_found.",
		Other:       "Page not found",
	},
	{
		ID:          "errors.page.parent_not_found",
		Description: "Derived from ErrorCode page_parent_not_found.",
		Other:       "Page parent not found",
	},
	{
		ID:          "errors.page.root_operation",
		Description: "Derived from ErrorCode page_root_operation.",
		Other:       "Page root operation",
	},
	{
		ID:          "errors.page.slug_conflict",
		Description: "Derived from ErrorCode page_slug_conflict.",
		Other:       "Page slug conflict",
	},
	{
		ID:          "errors.page.version_required",
		Description: "Derived from ErrorCode page_version_required.",
		Other:       "Page version required",
	},
	{
		ID:          "errors.presence.mode_invalid",
		Description: "Derived from ErrorCode presence_mode_invalid.",
		Other:       "mode must be view, edit, history, assets, settings, import, or unknown",
	},
	{
		ID:          "errors.presence.invalid_request",
		Description: "Derived from ErrorCode presence_invalid_request.",
		Other:       "Invalid presence request",
	},
	{
		ID:          "errors.presence.registry_unavailable",
		Description: "Derived from ErrorCode presence_registry_unavailable.",
		Other:       "web presence registry unavailable",
	},
	{
		ID:          "errors.presence.session_id_required",
		Description: "Derived from ErrorCode presence_session_id_required.",
		Other:       "sessionId is required",
	},
	{
		ID:          "errors.presence.session_id_too_long",
		Description: "Derived from ErrorCode presence_session_id_too_long.",
		Other:       "sessionId is too long",
	},
	{
		ID:          "errors.presence.session_user_mismatch",
		Description: "Derived from ErrorCode presence_session_user_mismatch.",
		Other:       "sessionId belongs to a different user",
	},
	{
		ID:          "errors.presence.user_required",
		Description: "Derived from ErrorCode presence_user_required.",
		Other:       "user is required",
	},
	{
		ID:          "errors.private.actor_context_invalid",
		Description: "Derived from ErrorCode private_actor_context_invalid.",
		Other:       "Private actor context invalid",
	},
	{
		ID:          "errors.private.control_token_invalid",
		Description: "Derived from ErrorCode private_control_token_invalid.",
		Other:       "Private control token invalid",
	},
	{
		ID:          "errors.private.encode_response_failed",
		Description: "Derived from ErrorCode private_encode_response_failed.",
		Other:       "Private encode response failed",
	},
	{
		ID:          "errors.private.grants_load_failed",
		Description: "Derived from ErrorCode private_grants_load_failed.",
		Other:       "Private grants load failed",
	},
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
