package security

import (
	"crypto/subtle"
	"log/slog"
	"net/http"

	"github.com/gin-gonic/gin"
	sharederrors "github.com/perber/wiki/internal/core/shared/errors"
)

const trustedPrivateChannelKey = "leafwiki.trustedPrivateChannel"

const (
	errCodeCSRFTokenMissing sharederrors.ErrorCode = "csrf_token_missing"
	errCodeCSRFTokenInvalid sharederrors.ErrorCode = "csrf_token_invalid"
)

func TrustPrivateChannel(c *gin.Context) {
	c.Set(trustedPrivateChannelKey, true)
}

func isTrustedPrivateChannel(c *gin.Context) bool {
	trusted, _ := c.Get(trustedPrivateChannelKey)
	value, _ := trusted.(bool)
	return value
}

// CSRFMiddleware is a Gin middleware that protects against CSRF attacks.
func CSRFMiddleware(csrf *CSRFCookie) gin.HandlerFunc {
	return func(c *gin.Context) {
		method := c.Request.Method

		// Only protect mutating methods (POST, PUT, PATCH, DELETE)
		if method == http.MethodGet || method == http.MethodHead || method == http.MethodOptions {
			c.Next()
			return
		}
		if isTrustedPrivateChannel(c) {
			c.Next()
			return
		}

		cookieToken, err := csrf.Read(c)
		if err != nil || cookieToken == "" {
			slog.Default().Warn("CSRF token missing or error reading token", "error", err)
			abortCSRFError(c, errCodeCSRFTokenMissing, "CSRF token missing")
			return
		}

		// Expect token in header X-CSRF-Token, alternatively in form field csrf_token
		headerToken := c.GetHeader("X-CSRF-Token")
		if headerToken == "" {
			headerToken = c.PostForm("csrf_token")
		}

		// No token in header/form or no match
		if headerToken == "" || subtle.ConstantTimeCompare([]byte(headerToken), []byte(cookieToken)) != 1 {
			slog.Default().Warn("CSRF token invalid or does not match cookie")
			abortCSRFError(c, errCodeCSRFTokenInvalid, "Invalid CSRF token")
			return
		}

		c.Next()
	}
}

func abortCSRFError(c *gin.Context, code sharederrors.ErrorCode, message string) {
	c.AbortWithStatusJSON(http.StatusForbidden, gin.H{
		"error": sharederrors.NewLocalizedErrorDetail(code, message, message),
	})
}
