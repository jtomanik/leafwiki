package localization

import "github.com/nicksnyder/go-i18n/v2/i18n"

var derivedErrorMessagesPages = []*i18n.Message{
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
}
