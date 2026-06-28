package auth_test

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"net/http"
	"net/http/httptest"

	"github.com/gin-gonic/gin"
	coreauth "github.com/perber/wiki/internal/core/auth"
	authmw "github.com/perber/wiki/internal/http/middleware/auth"
)

var _ = It("TestMustGetUserReturnsStructuredErrorWhenMissing", func() {
	t := GinkgoT()
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

})

var _ = It("TestMustGetUserReturnsStructuredErrorForInvalidContext", func() {
	t := GinkgoT()
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

})

var _ = It("TryGetUser returns the typed user from context", func() {
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(rec)
	user := &coreauth.User{ID: "user-1", Username: "editor"}
	ctx.Set("user", user)

	Expect(authmw.TryGetUser(ctx)).To(BeIdenticalTo(user))
})

var _ = It("MustGetUser returns the typed user from context", func() {
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(rec)
	user := &coreauth.User{ID: "user-1", Username: "editor"}
	ctx.Set("user", user)

	Expect(authmw.MustGetUser(ctx)).To(BeIdenticalTo(user))
	Expect(rec.Code).To(Equal(http.StatusOK))
})
