package localization

import "github.com/nicksnyder/go-i18n/v2/i18n"

var derivedErrorMessagesCore = []*i18n.Message{
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
}
