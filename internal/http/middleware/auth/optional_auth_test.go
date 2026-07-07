package auth_test

import (
	"net/http"
	"net/http/httptest"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/gin-gonic/gin"
	coreauth "github.com/perber/wiki/internal/core/auth"
	authmw "github.com/perber/wiki/internal/http/middleware/auth"
)

var _ = Describe("optional authentication middleware", Label("integration"), func() {
	It("passes through requests without a token and leaves user context empty", func() {
		gin.SetMode(gin.TestMode)
		authCookies := authmw.NewAuthCookies(true, time.Hour, time.Hour*24)
		router := gin.New()
		router.Use(authmw.OptionalAuth(nil, authCookies))
		router.GET("/test", func(c *gin.Context) {
			_, exists := c.Get("user")
			if exists {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "unexpected user in context"})
				return
			}
			c.JSON(http.StatusOK, gin.H{"user": nil})
		})

		req := httptest.NewRequest("GET", "/test", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		Expect(w).To(HaveHTTPStatus(http.StatusOK))
	})

	It("sets the user context for a valid token", func() {
		gin.SetMode(gin.TestMode)
		fixture := createTestAuthFixture()
		DeferCleanup(fixture.close)

		authCookies := authmw.NewAuthCookies(true, time.Hour, time.Hour*24)
		authToken, err := fixture.auth.Login("admin", "admin")
		Expect(err).To(Succeed())

		router := gin.New()
		router.Use(authmw.OptionalAuth(fixture.auth, authCookies))
		router.GET("/test", func(c *gin.Context) {
			v, exists := c.Get("user")
			if !exists {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "user not in context"})
				return
			}
			u, ok := v.(*coreauth.User)
			if !ok {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "wrong type"})
				return
			}
			c.JSON(http.StatusOK, gin.H{"username": u.Username})
		})

		req := httptest.NewRequest("GET", "/test", nil)
		req.AddCookie(&http.Cookie{Name: "leafwiki_at", Value: authToken.Token})
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		Expect(w).To(HaveHTTPStatus(http.StatusOK))
		Expect(w.Body.String()).To(MatchJSON(`{"username":"admin"}`))
	})

	It("passes through invalid tokens without adding a user context", func() {
		gin.SetMode(gin.TestMode)
		fixture := createTestAuthFixture()
		DeferCleanup(fixture.close)

		authCookies := authmw.NewAuthCookies(true, time.Hour, time.Hour*24)
		router := gin.New()
		router.Use(authmw.OptionalAuth(fixture.auth, authCookies))
		router.GET("/test", func(c *gin.Context) {
			_, exists := c.Get("user")
			if exists {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "unexpected user for invalid token"})
				return
			}
			c.JSON(http.StatusOK, gin.H{"user": nil})
		})

		req := httptest.NewRequest("GET", "/test", nil)
		req.AddCookie(&http.Cookie{Name: "leafwiki_at", Value: "invalid-token"})
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		Expect(w).To(HaveHTTPStatus(http.StatusOK))
	})

	It("returns a structured service-unavailable error when token validation has no auth service", func() {
		gin.SetMode(gin.TestMode)
		authCookies := authmw.NewAuthCookies(true, time.Hour, time.Hour*24)
		router := gin.New()
		router.Use(authmw.OptionalAuth(nil, authCookies))
		router.GET("/test", func(c *gin.Context) {
			c.JSON(http.StatusOK, gin.H{"ok": true})
		})

		req := httptest.NewRequest("GET", "/test", nil)
		req.AddCookie(&http.Cookie{Name: "leafwiki_at", Value: "some-token"})
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		Expect(w).To(matchAuthMiddlewareError(expectedAuthServiceUnavailable))
		Expect(w).To(HaveHTTPStatus(http.StatusInternalServerError))
	})

	It("preserves an existing user context without token validation", func() {
		gin.SetMode(gin.TestMode)
		authCookies := authmw.NewAuthCookies(true, time.Hour, time.Hour*24)
		injected := &coreauth.User{ID: coreauth.UserIDFromString("proxy-user"), Username: "proxy", Role: coreauth.RoleViewer}

		router := gin.New()
		router.Use(func(c *gin.Context) {
			c.Set("user", injected)
			c.Next()
		})
		router.Use(authmw.OptionalAuth(nil, authCookies))
		router.GET("/test", func(c *gin.Context) {
			v, _ := c.Get("user")
			u := v.(*coreauth.User)
			c.JSON(http.StatusOK, gin.H{"username": u.Username})
		})

		req := httptest.NewRequest("GET", "/test", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		Expect(w).To(HaveHTTPStatus(http.StatusOK))
		Expect(w.Body.String()).To(MatchJSON(`{"username":"proxy"}`))
	})
})
