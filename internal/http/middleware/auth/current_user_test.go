package auth_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	coreauth "github.com/perber/wiki/internal/core/auth"
	authmw "github.com/perber/wiki/internal/http/middleware/auth"
)

func TestMustGetUserReturnsStructuredErrorWhenMissing(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.GET("/me", func(c *gin.Context) {
		if authmw.MustGetUser(c) != nil {
			t.Fatalf("MustGetUser returned user for missing context")
		}
	})

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/me", nil))

	assertAuthMiddlewareError(t, rec, "auth_user_not_authenticated", "errors.auth.user_not_authenticated", "User not authenticated")
}

func TestMustGetUserReturnsStructuredErrorForInvalidContext(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set("user", (*coreauth.User)(nil))
		c.Next()
	})
	router.GET("/me", func(c *gin.Context) {
		if authmw.MustGetUser(c) != nil {
			t.Fatalf("MustGetUser returned user for invalid context")
		}
	})

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/me", nil))

	assertAuthMiddlewareError(t, rec, "auth_invalid_user_context", "errors.auth.invalid_user_context", "Invalid user context")
}
