package auth

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	coreauth "github.com/perber/wiki/internal/core/auth"
	sharederrors "github.com/perber/wiki/internal/core/shared/errors"
	"github.com/perber/wiki/internal/localization"
)

const (
	ErrCodeAuthDisabled                 sharederrors.ErrorCode = "auth_disabled"
	ErrCodeAuthInvalidCredentials       sharederrors.ErrorCode = "auth_invalid_credentials"
	ErrCodeAuthTokenExpired             sharederrors.ErrorCode = "auth_token_expired"
	ErrCodeAuthUserNotFound             sharederrors.ErrorCode = "auth_user_not_found"
	ErrCodeAuthUserAlreadyExists        sharederrors.ErrorCode = "auth_user_already_exists"
	ErrCodeAuthInvalidRole              sharederrors.ErrorCode = "auth_invalid_role"
	ErrCodeAuthForbidden                sharederrors.ErrorCode = "auth_forbidden"
	ErrCodeAuthAdminCannotDelete        sharederrors.ErrorCode = "auth_admin_cannot_delete"
	ErrCodeAuthLastAdminCannotBeDemoted sharederrors.ErrorCode = "auth_last_admin_cannot_be_demoted"
	ErrCodeAuthInternalError            sharederrors.ErrorCode = "auth_internal_error"
	ErrCodeAuthInvalidPayload           sharederrors.ErrorCode = "auth_invalid_payload"
	ErrCodeAuthCookieFailed             sharederrors.ErrorCode = "auth_cookie_failed"
	ErrCodeAuthCsrfFailed               sharederrors.ErrorCode = "auth_csrf_failed"
	ErrCodeAuthInvalidRefreshToken      sharederrors.ErrorCode = "auth_invalid_refresh_token"
	ErrCodeAuthInvalidRequest           sharederrors.ErrorCode = "auth_invalid_request"
	ErrCodeAuthAccountLocked            sharederrors.ErrorCode = "auth_account_locked"
)

const authValidationErrorCode = "validation_error"

const (
	FieldCodeAuthUsernameRequired         sharederrors.FieldErrorCode = "auth_username_required"
	FieldCodeAuthEmailRequired            sharederrors.FieldErrorCode = "auth_email_required"
	FieldCodeAuthEmailInvalid             sharederrors.FieldErrorCode = "auth_email_invalid"
	FieldCodeAuthPasswordRequired         sharederrors.FieldErrorCode = "auth_password_required"
	FieldCodeAuthPasswordTooShort         sharederrors.FieldErrorCode = "auth_password_too_short"
	FieldCodeAuthRoleInvalid              sharederrors.FieldErrorCode = "auth_role_invalid"
	FieldCodeAuthNewPasswordRequired      sharederrors.FieldErrorCode = "auth_new_password_required"
	FieldCodeAuthNewPasswordTooShort      sharederrors.FieldErrorCode = "auth_new_password_too_short"
	FieldCodeAuthOldPasswordIncorrect     sharederrors.FieldErrorCode = "auth_old_password_incorrect"
	FieldCodeAuthAPIKeyNameRequired       sharederrors.FieldErrorCode = "auth_api_key_name_required"
	FieldCodeAuthAPIKeyNameTooLong        sharederrors.FieldErrorCode = "auth_api_key_name_too_long"
	FieldCodeAuthCurrentPasswordRequired  sharederrors.FieldErrorCode = "auth_current_password_required"
	FieldCodeAuthCurrentPasswordIncorrect sharederrors.FieldErrorCode = "auth_current_password_incorrect"
)

const (
	MessageIDAuthUsernameRequired         sharederrors.MessageID = "validation.auth.username_required"
	MessageIDAuthEmailRequired            sharederrors.MessageID = "validation.auth.email_required"
	MessageIDAuthEmailInvalid             sharederrors.MessageID = "validation.auth.email_invalid"
	MessageIDAuthPasswordRequired         sharederrors.MessageID = "validation.auth.password_required"
	MessageIDAuthPasswordTooShort         sharederrors.MessageID = "validation.auth.password_too_short"
	MessageIDAuthRoleInvalid              sharederrors.MessageID = "validation.auth.role_invalid"
	MessageIDAuthNewPasswordRequired      sharederrors.MessageID = "validation.auth.new_password_required"
	MessageIDAuthNewPasswordTooShort      sharederrors.MessageID = "validation.auth.new_password_too_short"
	MessageIDAuthOldPasswordIncorrect     sharederrors.MessageID = "validation.auth.old_password_incorrect"
	MessageIDAuthAPIKeyNameRequired       sharederrors.MessageID = "validation.auth.api_key_name_required"
	MessageIDAuthAPIKeyNameTooLong        sharederrors.MessageID = "validation.auth.api_key_name_too_long"
	MessageIDAuthCurrentPasswordRequired  sharederrors.MessageID = "validation.auth.current_password_required"
	MessageIDAuthCurrentPasswordIncorrect sharederrors.MessageID = "validation.auth.current_password_incorrect"
	MessageIDAuthLoginSuccess             sharederrors.MessageID = "api.auth.login.success"
	MessageIDAuthLogoutSuccess            sharederrors.MessageID = "api.auth.logout.success"
	MessageIDAuthRefreshTokenSuccess      sharederrors.MessageID = "api.auth.refresh_token.success"
)

// AuthErrorResponse is the structured JSON error body returned by auth endpoints.
type AuthErrorResponse struct {
	Error AuthErrorDetail `json:"error"`
}

// AuthErrorDetail carries the localization-ready error data.
type AuthErrorDetail = sharederrors.LocalizedErrorDetail

func respondWithAuthStatusError(c *gin.Context, status int, code sharederrors.ErrorCode, message, template string, args ...string) {
	c.JSON(status, AuthErrorResponse{
		Error: sharederrors.NewLocalizedErrorDetail(code, message, template, args...),
	})
}

func apiSuccessMessage(messageID sharederrors.MessageID) string {
	return localization.English.Render(messageID, "").Message
}

// respondWithAuthError is the central error handler for auth endpoints.
func respondWithAuthError(c *gin.Context, err error) {
	if loc, ok := sharederrors.AsLocalizedError(err); ok {
		c.JSON(authErrorStatus(loc.Code), AuthErrorResponse{Error: sharederrors.LocalizedErrorDetailFromError(loc)})
		return
	}

	var vErr *sharederrors.ValidationErrors
	if errors.As(err, &vErr) {
		c.JSON(http.StatusBadRequest, gin.H{
			"error":  authValidationErrorCode,
			"fields": vErr.Errors,
		})
		return
	}

	switch {
	case errors.Is(err, coreauth.ErrInvalidToken):
		respondWithAuthStatusError(c, http.StatusUnprocessableEntity, ErrCodeAuthInvalidRefreshToken, "Missing or invalid refresh token", "missing or invalid refresh token")
	case errors.Is(err, coreauth.ErrUserAccountLocked):
		respondWithAuthStatusError(c, http.StatusUnauthorized, ErrCodeAuthAccountLocked, "Account temporarily locked due to too many failed login attempts", "account locked")
	case errors.Is(err, coreauth.ErrUserInvalidCredentials):
		respondWithAuthStatusError(c, http.StatusUnauthorized, ErrCodeAuthInvalidCredentials, "Invalid credentials", "invalid credentials")
	case errors.Is(err, coreauth.ErrUserNotFound):
		respondWithAuthStatusError(c, http.StatusNotFound, ErrCodeAuthUserNotFound, "User not found", "user not found")
	case errors.Is(err, coreauth.ErrUserAlreadyExists):
		respondWithAuthStatusError(c, http.StatusConflict, ErrCodeAuthUserAlreadyExists, "User already exists", "user already exists")
	case errors.Is(err, coreauth.ErrUserInvalidRole):
		respondWithAuthStatusError(c, http.StatusBadRequest, ErrCodeAuthInvalidRole, "Invalid role", "invalid role")
	case errors.Is(err, coreauth.ErrUserAdminCannotBeDeleted):
		respondWithAuthStatusError(c, http.StatusBadRequest, ErrCodeAuthAdminCannotDelete, "Admin user cannot be deleted", "admin user cannot be deleted")
	case errors.Is(err, coreauth.ErrLastAdminCannotBeDemoted):
		respondWithAuthStatusError(c, http.StatusBadRequest, ErrCodeAuthLastAdminCannotBeDemoted, "Cannot remove admin role from the last admin user", "cannot remove admin role from the last admin user")
	case errors.Is(err, coreauth.ErrAPIKeyNotFound):
		respondWithAuthStatusError(c, http.StatusNotFound, ErrCodeAuthUserNotFound, "API key not found", "api key not found")
	case errors.Is(err, coreauth.ErrAPIKeyInvalidName):
		respondWithAuthStatusError(c, http.StatusBadRequest, ErrCodeAuthInvalidRequest, "Invalid API key name", "invalid api key name")
	case errors.Is(err, ErrAuthDisabled):
		respondWithAuthStatusError(c, http.StatusForbidden, ErrCodeAuthDisabled, "Authentication is disabled", "authentication is disabled")
	default:
		respondWithAuthStatusError(c, http.StatusInternalServerError, ErrCodeAuthInternalError, "Authentication request failed", "authentication request failed")
	}
}

func authErrorStatus(code sharederrors.ErrorCode) int {
	switch code {
	case ErrCodeAuthUserNotFound:
		return http.StatusNotFound
	case ErrCodeAuthInvalidCredentials, ErrCodeAuthTokenExpired:
		return http.StatusUnauthorized
	case ErrCodeAuthInvalidRefreshToken:
		return http.StatusUnprocessableEntity
	case ErrCodeAuthUserAlreadyExists:
		return http.StatusConflict
	case ErrCodeAuthInvalidRole, ErrCodeAuthAdminCannotDelete, ErrCodeAuthLastAdminCannotBeDemoted,
		ErrCodeAuthInvalidPayload, ErrCodeAuthCookieFailed, ErrCodeAuthCsrfFailed,
		ErrCodeAuthInvalidRequest:
		return http.StatusBadRequest
	case ErrCodeAuthAccountLocked:
		return http.StatusUnauthorized
	case ErrCodeAuthDisabled, ErrCodeAuthForbidden:
		return http.StatusForbidden
	default:
		return http.StatusInternalServerError
	}
}
