package auth

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/perber/wiki/internal/core/auth"
)

// MustGetUser returns the authenticated user from context or aborts the request.
func MustGetUser(c *gin.Context) *auth.User {
	v, exists := c.Get("user")
	if !exists {
		abortAuthMiddlewareError(c, http.StatusForbidden, errCodeAuthUserNotAuthenticated, "User not authenticated")
		return nil
	}

	user, ok := v.(*auth.User)
	if !ok || user == nil {
		abortAuthMiddlewareError(c, http.StatusInternalServerError, errCodeAuthInvalidUserContext, "Invalid user context")
		return nil
	}

	return user
}

// TryGetUser returns the authenticated user from context, or nil if not set.
func TryGetUser(c *gin.Context) *auth.User {
	v, exists := c.Get("user")
	if !exists {
		return nil
	}
	user, ok := v.(*auth.User)
	if !ok {
		return nil
	}
	return user
}
