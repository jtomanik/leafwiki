package auth_test

import (
	"encoding/json"

	sharederrors "github.com/perber/wiki/internal/core/shared/errors"
)

var (
	expectedAuthInvalidUserContext        = mustDecodeAuthErrorCode("auth_invalid_user_context")
	expectedAuthReverseProxyMisconfigured = mustDecodeAuthErrorCode("auth_reverse_proxy_misconfigured")
	expectedAuthRemoteUserNotFound        = mustDecodeAuthErrorCode("auth_remote_user_not_found")
	expectedAuthDisabledMissingUser       = mustDecodeAuthErrorCode("auth_disabled_missing_user")
	expectedAuthAccessTokenMissing        = mustDecodeAuthErrorCode("auth_access_token_missing")
	expectedAuthServiceUnavailable        = mustDecodeAuthErrorCode("auth_service_unavailable")
	expectedAuthTokenInvalid              = mustDecodeAuthErrorCode("auth_token_invalid")
	expectedAuthAdminDisabled             = mustDecodeAuthErrorCode("auth_admin_disabled")
	expectedAuthUserNotAuthenticated      = mustDecodeAuthErrorCode("auth_user_not_authenticated")
	expectedAuthAdminPrivilegesRequired   = mustDecodeAuthErrorCode("auth_admin_privileges_required")
	expectedAuthInvalidUser               = mustDecodeAuthErrorCode("auth_invalid_user")
	expectedAuthEditorPrivilegesRequired  = mustDecodeAuthErrorCode("auth_editor_privileges_required")
	expectedAuthSelfRequired              = mustDecodeAuthErrorCode("auth_self_required")
)

func mustDecodeAuthErrorCode(raw string) sharederrors.ErrorCode {
	payload, err := json.Marshal(raw)
	if err != nil {
		panic(err)
	}
	var code sharederrors.ErrorCode
	if err := json.Unmarshal(payload, &code); err != nil {
		panic(err)
	}
	return code
}
