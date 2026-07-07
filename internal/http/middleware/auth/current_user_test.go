package auth_test

import (
	"net/http"
	"net/http/httptest"

	. "github.com/onsi/gomega"

	"github.com/gin-gonic/gin"
	. "github.com/onsi/ginkgo/v2"
	coreauth "github.com/perber/wiki/internal/core/auth"
	authmw "github.com/perber/wiki/internal/http/middleware/auth"
)

var _ = Describe("current user lookup", Label("unit"), func() {
	It("returns a structured unauthenticated error when the user context is missing", func() {
		gin.SetMode(gin.TestMode)
		router := gin.New()
		router.GET("/me", func(c *gin.Context) {
			Expect(authmw.MustGetUser(c)).To(BeNil())
		})

		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/me", nil))

		Expect(rec).To(matchAuthMiddlewareError(expectedAuthUserNotAuthenticated))
	})

	It("returns a structured invalid-context error when the user value is not usable", func() {
		gin.SetMode(gin.TestMode)
		router := gin.New()
		router.Use(func(c *gin.Context) {
			c.Set("user", (*coreauth.User)(nil))
			c.Next()
		})
		router.GET("/me", func(c *gin.Context) {
			Expect(authmw.MustGetUser(c)).To(BeNil())
		})

		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/me", nil))

		Expect(rec).To(matchAuthMiddlewareError(expectedAuthInvalidUserContext))
	})

	It("returns the typed user through optional lookup", func() {
		gin.SetMode(gin.TestMode)
		rec := httptest.NewRecorder()
		ctx, _ := gin.CreateTestContext(rec)
		user := &coreauth.User{ID: coreauth.UserIDFromString("user-1"), Username: "editor"}
		ctx.Set("user", user)

		Expect(authmw.TryGetUser(ctx)).To(BeIdenticalTo(user))
	})

	It("returns the typed user through required lookup", func() {
		gin.SetMode(gin.TestMode)
		rec := httptest.NewRecorder()
		ctx, _ := gin.CreateTestContext(rec)
		user := &coreauth.User{ID: coreauth.UserIDFromString("user-1"), Username: "editor"}
		ctx.Set("user", user)

		Expect(authmw.MustGetUser(ctx)).To(BeIdenticalTo(user))
		Expect(rec).To(HaveHTTPStatus(http.StatusOK))
	})
})
