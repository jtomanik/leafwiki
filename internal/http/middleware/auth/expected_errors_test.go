package auth_test

import sharederrors "github.com/perber/wiki/internal/core/shared/errors"

const (
	expectedAuthInvalidUserContext        sharederrors.ErrorCode = "auth_invalid_user_context"
	expectedAuthReverseProxyMisconfigured sharederrors.ErrorCode = "auth_reverse_proxy_misconfigured"
	expectedAuthRemoteUserNotFound        sharederrors.ErrorCode = "auth_remote_user_not_found"
	expectedAuthDisabledMissingUser       sharederrors.ErrorCode = "auth_disabled_missing_user"
	expectedAuthAccessTokenMissing        sharederrors.ErrorCode = "auth_access_token_missing"
	expectedAuthServiceUnavailable        sharederrors.ErrorCode = "auth_service_unavailable"
	expectedAuthTokenInvalid              sharederrors.ErrorCode = "auth_token_invalid"
	expectedAuthAdminDisabled             sharederrors.ErrorCode = "auth_admin_disabled"
	expectedAuthUserNotAuthenticated      sharederrors.ErrorCode = "auth_user_not_authenticated"
	expectedAuthAdminPrivilegesRequired   sharederrors.ErrorCode = "auth_admin_privileges_required"
	expectedAuthInvalidUser               sharederrors.ErrorCode = "auth_invalid_user"
	expectedAuthEditorPrivilegesRequired  sharederrors.ErrorCode = "auth_editor_privileges_required"
	expectedAuthSelfRequired              sharederrors.ErrorCode = "auth_self_required"
)
