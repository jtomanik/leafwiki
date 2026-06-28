package auth

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	coreauth "github.com/perber/wiki/internal/core/auth"
	sharederrors "github.com/perber/wiki/internal/core/shared/errors"
)

var (
	ErrInvalidUserContext      = errors.New("invalid user context")
	ErrAuthDisabledMissingUser = errors.New("user not authenticated and auth is disabled")
	ErrMissingAccessToken      = errors.New("missing or invalid access token")
	ErrAuthServiceUnavailable  = errors.New("authentication service unavailable")
	ErrInvalidOrExpiredToken   = errors.New("invalid or expired token")
)

const (
	errCodeAuthInvalidUserContext        sharederrors.ErrorCode = "auth_invalid_user_context"
	errCodeAuthReverseProxyMisconfigured sharederrors.ErrorCode = "auth_reverse_proxy_misconfigured"
	errCodeAuthRemoteUserNotFound        sharederrors.ErrorCode = "auth_remote_user_not_found"
	errCodeAuthDisabledMissingUser       sharederrors.ErrorCode = "auth_disabled_missing_user"
	errCodeAuthAccessTokenMissing        sharederrors.ErrorCode = "auth_access_token_missing"
	errCodeAuthServiceUnavailable        sharederrors.ErrorCode = "auth_service_unavailable"
	errCodeAuthTokenInvalid              sharederrors.ErrorCode = "auth_token_invalid"
	errCodeAuthAdminDisabled             sharederrors.ErrorCode = "auth_admin_disabled"
	errCodeAuthUserNotAuthenticated      sharederrors.ErrorCode = "auth_user_not_authenticated"
	errCodeAuthAdminPrivilegesRequired   sharederrors.ErrorCode = "auth_admin_privileges_required"
	errCodeAuthInvalidUser               sharederrors.ErrorCode = "auth_invalid_user"
	errCodeAuthEditorPrivilegesRequired  sharederrors.ErrorCode = "auth_editor_privileges_required"
	errCodeAuthSelfRequired              sharederrors.ErrorCode = "auth_self_required"
)

func ResolveRequestUser(c *gin.Context, authService *coreauth.AuthService, authCookies *AuthCookies, authDisabled bool) (*coreauth.User, error) {
	if userValue, exists := c.Get("user"); exists {
		user, ok := userValue.(*coreauth.User)
		if !ok || user == nil {
			return nil, ErrInvalidUserContext
		}
		return user, nil
	}

	if authDisabled {
		return nil, ErrAuthDisabledMissingUser
	}

	token, err := authCookies.ReadAccess(c)
	if err != nil || token == "" {
		return nil, ErrMissingAccessToken
	}

	if authService == nil {
		return nil, ErrAuthServiceUnavailable
	}

	user, err := authService.ValidateToken(token)
	if err != nil {
		return nil, ErrInvalidOrExpiredToken
	}

	c.Set("user", user)
	return user, nil
}

func RequireAuth(authService *coreauth.AuthService, authCookies *AuthCookies, authDisabled bool) gin.HandlerFunc {
	return func(c *gin.Context) {
		_, err := ResolveRequestUser(c, authService, authCookies, authDisabled)
		if err == nil {
			c.Next()
			return
		}

		abortRequireAuthError(c, err)
	}
}

func abortRequireAuthError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, ErrInvalidUserContext):
		abortAuthMiddlewareError(c, http.StatusInternalServerError, errCodeAuthInvalidUserContext, "Invalid user context")
	case errors.Is(err, ErrAuthDisabledMissingUser):
		abortAuthMiddlewareError(c, http.StatusUnauthorized, errCodeAuthDisabledMissingUser, "User not authenticated and auth is disabled")
	case errors.Is(err, ErrMissingAccessToken):
		abortAuthMiddlewareError(c, http.StatusUnauthorized, errCodeAuthAccessTokenMissing, "Missing or invalid access token")
	case errors.Is(err, ErrAuthServiceUnavailable):
		abortAuthMiddlewareError(c, http.StatusInternalServerError, errCodeAuthServiceUnavailable, "Authentication service unavailable")
	case errors.Is(err, ErrInvalidOrExpiredToken):
		abortAuthMiddlewareError(c, http.StatusUnauthorized, errCodeAuthTokenInvalid, "Invalid or expired token")
	default:
		abortAuthMiddlewareError(c, http.StatusInternalServerError, errCodeAuthTokenInvalid, "Authentication failed")
	}
}

func RequireAdmin(authDisabled bool) gin.HandlerFunc {
	return func(c *gin.Context) {
		// Explicitly block admin operations when authentication is disabled
		if authDisabled {
			abortAuthMiddlewareError(c, http.StatusForbidden, errCodeAuthAdminDisabled, "Admin operations are not available when authentication is disabled")
			return
		}

		userValue, exists := c.Get("user")
		if !exists {
			abortAuthMiddlewareError(c, http.StatusForbidden, errCodeAuthUserNotAuthenticated, "User not authenticated")
			return
		}

		user, ok := userValue.(*coreauth.User)
		if !ok || !user.HasRole(coreauth.RoleAdmin) {
			abortAuthMiddlewareError(c, http.StatusForbidden, errCodeAuthAdminPrivilegesRequired, "Admin privileges required")
			return
		}

		c.Next()
	}
}

func RequireSelfOrAdmin(authDisabled bool) gin.HandlerFunc {
	return func(c *gin.Context) {
		// Block all user management operations when authentication is disabled
		if authDisabled {
			abortAuthMiddlewareError(c, http.StatusForbidden, errCodeAuthAdminDisabled, "User management is not available when authentication is disabled")
			return
		}

		userValue, exists := c.Get("user")
		if !exists {
			abortAuthMiddlewareError(c, http.StatusForbidden, errCodeAuthUserNotAuthenticated, "User not authenticated")
			return
		}

		user, ok := userValue.(*coreauth.User)
		if !ok {
			abortAuthMiddlewareError(c, http.StatusForbidden, errCodeAuthInvalidUser, "Invalid user")
			return
		}

		// Check if user is trying to access their own resource
		isSelf := user.ID == c.Param("id")

		// Allow users to access their own resources
		if isSelf {
			c.Next()
			return
		}

		// Check if user has admin privileges for accessing other users
		if !user.HasRole(coreauth.RoleAdmin) {
			abortAuthMiddlewareError(c, http.StatusForbidden, errCodeAuthAdminPrivilegesRequired, "Admin privileges required")
			return
		}

		c.Next()
	}
}

// OptionalAuth validates the session cookie if present and stores the user in context,
// but unlike RequireAuth it does not abort the request for unauthenticated callers.
// Exception: a token IS present but authService is nil — that is a misconfiguration
// and aborts with 500, matching RequireAuth's behaviour for the same case.
func OptionalAuth(authService *coreauth.AuthService, authCookies *AuthCookies) gin.HandlerFunc {
	return func(c *gin.Context) {
		if _, exists := c.Get("user"); exists {
			c.Next()
			return
		}
		token, err := authCookies.ReadAccess(c)
		if err != nil || token == "" {
			c.Next()
			return
		}
		if authService == nil {
			abortAuthMiddlewareError(c, http.StatusInternalServerError, errCodeAuthServiceUnavailable, "Authentication service unavailable")
			return
		}
		if user, err := authService.ValidateToken(token); err == nil {
			c.Set("user", user)
		}
		c.Next()
	}
}

func RequireEditorOrAdmin() gin.HandlerFunc {
	return func(c *gin.Context) {
		userValue, exists := c.Get("user")
		if !exists {
			abortAuthMiddlewareError(c, http.StatusForbidden, errCodeAuthUserNotAuthenticated, "User not authenticated")
			return
		}

		user, ok := userValue.(*coreauth.User)
		if !ok {
			abortAuthMiddlewareError(c, http.StatusForbidden, errCodeAuthUserNotAuthenticated, "User not authenticated")
			return
		}

		if user.HasRole(coreauth.RoleAdmin) || user.HasRole(coreauth.RoleEditor) {
			c.Next()
			return
		}

		abortAuthMiddlewareError(c, http.StatusForbidden, errCodeAuthEditorPrivilegesRequired, "Editor or Admin role required")
	}
}

func RequireSelf() gin.HandlerFunc {
	return func(c *gin.Context) {
		userValue, exists := c.Get("user")
		if !exists {
			abortAuthMiddlewareError(c, http.StatusForbidden, errCodeAuthUserNotAuthenticated, "User not authenticated")
			return
		}

		user, ok := userValue.(*coreauth.User)
		if !ok || user.ID != c.Param("id") {
			abortAuthMiddlewareError(c, http.StatusForbidden, errCodeAuthSelfRequired, "You can only access your own account")
			return
		}

		c.Next()
	}
}

func abortAuthMiddlewareError(c *gin.Context, status int, code sharederrors.ErrorCode, message string) {
	c.AbortWithStatusJSON(status, authMiddlewareErrorResponse{
		Error: sharederrors.NewLocalizedErrorDetailFromCode(code),
	})
}

type authMiddlewareErrorResponse struct {
	Error sharederrors.LocalizedErrorDetail `json:"error"`
}
