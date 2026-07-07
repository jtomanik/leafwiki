package auth_test

import (
	"net/http"
	"net/http/httptest"
	"os"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/gin-gonic/gin"
	coreauth "github.com/perber/wiki/internal/core/auth"
	sharederrors "github.com/perber/wiki/internal/core/shared/errors"
	authmw "github.com/perber/wiki/internal/http/middleware/auth"
	testmatchers "github.com/perber/wiki/internal/test_utils/matchers"
)

type authFixture struct {
	auth  *coreauth.AuthService
	close func() error
}

type requireAuthComprehensiveScenario struct {
	authDisabled   bool
	injectUser     bool
	provideToken   bool
	validToken     bool
	expectedStatus int
	expectedCode   sharederrors.ErrorCode
}

func createTestAuthFixture() *authFixture {
	GinkgoHelper()

	storageDir := authMiddlewareTempDir()
	userStore, err := coreauth.NewUserStore(storageDir)
	Expect(err).To(Succeed())
	sessionStore, err := coreauth.NewSessionStore(storageDir)
	if err != nil {
		_ = userStore.Close()
	}
	Expect(err).To(Succeed())

	userService := coreauth.NewUserService(userStore)
	if err := userService.InitDefaultAdmin("admin"); err != nil {
		_ = sessionStore.Close()
		_ = userStore.Close()
	}
	Expect(err).To(Succeed())

	return &authFixture{
		auth: coreauth.NewAuthService(userService, sessionStore, "test-secret-key-for-unit-tests-1", 15*time.Minute, 7*24*time.Hour),
		close: func() error {
			if err := sessionStore.Close(); err != nil {
				_ = userStore.Close()
				return err
			}
			return userStore.Close()
		},
	}
}

func authMiddlewareTempDir() string {
	GinkgoHelper()
	dir, err := os.MkdirTemp("", "leafwiki-auth-middleware-*")
	Expect(err).To(Succeed())
	DeferCleanup(os.RemoveAll, dir)
	return dir
}

func matchAuthMiddlewareError(code sharederrors.ErrorCode) OmegaMatcher {
	GinkgoHelper()
	return WithTransform(func(rec *httptest.ResponseRecorder) []byte {
		return rec.Body.Bytes()
	}, testmatchers.HaveStructuredError(code, sharederrors.MessageIDForCode(code)))
}

var _ = Describe("required authentication middleware", Label("integration"), func() {
	It("allows an injected public editor when authentication is disabled", func() {
		gin.SetMode(gin.TestMode)

		authCookies := authmw.NewAuthCookies(true, time.Hour, time.Hour*24)

		router := gin.New()

		// Middleware to inject user (simulating authmw.InjectPublicEditor)
		router.Use(func(c *gin.Context) {
			c.Set("user", &coreauth.User{
				ID:       coreauth.UserIDFromString("public-editor"),
				Username: "public-editor",
				Role:     coreauth.RoleEditor,
			})
			c.Next()
		})

		// Apply RequireAuth with authDisabled=true
		router.Use(authmw.RequireAuth(nil, authCookies, true))

		router.GET("/test", func(c *gin.Context) {
			userValue, exists := c.Get("user")
			if !exists {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "user not found"})
				return
			}

			user, ok := userValue.(*coreauth.User)
			if !ok {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "invalid user type"})
				return
			}

			c.JSON(http.StatusOK, gin.H{
				"username": user.Username,
				"role":     user.Role,
			})
		})

		req := httptest.NewRequest("GET", "/test", nil)
		w2 := httptest.NewRecorder()

		router.ServeHTTP(w2, req)

		Expect(w2).To(HaveHTTPStatus(http.StatusOK))
		Expect(w2.Body.String()).To(MatchJSON(`{"role":"editor","username":"public-editor"}`))
	})

	It("returns a structured missing-user error when authentication is disabled without an injected user", func() {
		gin.SetMode(gin.TestMode)

		authCookies := authmw.NewAuthCookies(true, time.Hour, time.Hour*24)

		router := gin.New()

		// Apply RequireAuth with authDisabled=true but no user injected
		router.Use(authmw.RequireAuth(nil, authCookies, true))

		router.GET("/test", func(c *gin.Context) {
			c.JSON(http.StatusOK, gin.H{"ok": true})
		})

		req := httptest.NewRequest("GET", "/test", nil)
		w2 := httptest.NewRecorder()

		router.ServeHTTP(w2, req)

		Expect(w2).To(matchAuthMiddlewareError(expectedAuthDisabledMissingUser))
		Expect(w2).To(HaveHTTPStatus(http.StatusUnauthorized))
	})

	It("returns a structured invalid-context error for a nil user value", func() {
		gin.SetMode(gin.TestMode)

		authCookies := authmw.NewAuthCookies(true, time.Hour, time.Hour*24)

		router := gin.New()
		router.Use(func(c *gin.Context) {
			c.Set("user", (*coreauth.User)(nil))
			c.Next()
		})
		router.Use(authmw.RequireAuth(nil, authCookies, true))

		router.GET("/test", func(c *gin.Context) {
			c.JSON(http.StatusOK, gin.H{"ok": true})
		})

		req := httptest.NewRequest("GET", "/test", nil)
		w := httptest.NewRecorder()

		router.ServeHTTP(w, req)

		Expect(w).To(matchAuthMiddlewareError(expectedAuthInvalidUserContext))
		Expect(w).To(HaveHTTPStatus(http.StatusInternalServerError))
	})

	It("returns a structured invalid-context error for a non-user context value", func() {
		gin.SetMode(gin.TestMode)

		authCookies := authmw.NewAuthCookies(true, time.Hour, time.Hour*24)

		router := gin.New()
		router.Use(func(c *gin.Context) {
			c.Set("user", "not-a-user")
			c.Next()
		})
		router.Use(authmw.RequireAuth(nil, authCookies, true))

		router.GET("/test", func(c *gin.Context) {
			c.JSON(http.StatusOK, gin.H{"ok": true})
		})

		req := httptest.NewRequest("GET", "/test", nil)
		w := httptest.NewRecorder()

		router.ServeHTTP(w, req)

		Expect(w).To(matchAuthMiddlewareError(expectedAuthInvalidUserContext))
		Expect(w).To(HaveHTTPStatus(http.StatusInternalServerError))
	})

	It("authenticates a valid access token and injects the admin user", func() {
		gin.SetMode(gin.TestMode)

		fixture := createTestAuthFixture()
		DeferCleanup(fixture.close)

		authCookies := authmw.NewAuthCookies(true, time.Hour, time.Hour*24)

		// Login to get a valid token
		authToken, err := fixture.auth.Login("admin", "admin")
		Expect(err).To(Succeed())

		router := gin.New()

		// Apply RequireAuth with authDisabled=false
		router.Use(authmw.RequireAuth(fixture.auth, authCookies, false))

		router.GET("/test", func(c *gin.Context) {
			userValue, exists := c.Get("user")
			if !exists {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "user not found"})
				return
			}

			u, ok := userValue.(*coreauth.User)
			if !ok {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "invalid user type"})
				return
			}

			c.JSON(http.StatusOK, gin.H{
				"username": u.Username,
				"role":     u.Role,
			})
		})

		req := httptest.NewRequest("GET", "/test", nil)
		req.AddCookie(&http.Cookie{
			Name:  "leafwiki_at",
			Value: authToken.Token,
		})
		w := httptest.NewRecorder()

		router.ServeHTTP(w, req)

		Expect(w).To(HaveHTTPStatus(http.StatusOK))
		Expect(w.Body.String()).To(MatchJSON(`{"role":"admin","username":"admin"}`))
	})

	It("returns a structured missing-token error when authentication is enabled without a token", func() {
		gin.SetMode(gin.TestMode)

		fixture := createTestAuthFixture()
		DeferCleanup(fixture.close)

		authCookies := authmw.NewAuthCookies(true, time.Hour, time.Hour*24)

		router := gin.New()

		// Apply RequireAuth with authDisabled=false
		router.Use(authmw.RequireAuth(fixture.auth, authCookies, false))

		router.GET("/test", func(c *gin.Context) {
			c.JSON(http.StatusOK, gin.H{"ok": true})
		})

		req := httptest.NewRequest("GET", "/test", nil)
		w := httptest.NewRecorder()

		router.ServeHTTP(w, req)

		Expect(w).To(matchAuthMiddlewareError(expectedAuthAccessTokenMissing))
		Expect(w).To(HaveHTTPStatus(http.StatusUnauthorized))
	})

	It("returns a structured service-unavailable error when token validation has no auth service", func() {
		gin.SetMode(gin.TestMode)

		authCookies := authmw.NewAuthCookies(true, time.Hour, time.Hour*24)

		router := gin.New()
		router.Use(authmw.RequireAuth(nil, authCookies, false))

		router.GET("/test", func(c *gin.Context) {
			c.JSON(http.StatusOK, gin.H{"ok": true})
		})

		req := httptest.NewRequest("GET", "/test", nil)
		req.AddCookie(&http.Cookie{
			Name:  "leafwiki_at",
			Value: "some-token",
		})
		w := httptest.NewRecorder()

		router.ServeHTTP(w, req)

		Expect(w).To(matchAuthMiddlewareError(expectedAuthServiceUnavailable))
		Expect(w).To(HaveHTTPStatus(http.StatusInternalServerError))
	})

	It("returns a structured invalid-token error for invalid access tokens", func() {
		gin.SetMode(gin.TestMode)

		fixture := createTestAuthFixture()
		DeferCleanup(fixture.close)

		authCookies := authmw.NewAuthCookies(true, time.Hour, time.Hour*24)

		router := gin.New()

		// Apply RequireAuth with authDisabled=false
		router.Use(authmw.RequireAuth(fixture.auth, authCookies, false))

		router.GET("/test", func(c *gin.Context) {
			c.JSON(http.StatusOK, gin.H{"ok": true})
		})

		req := httptest.NewRequest("GET", "/test", nil)
		req.AddCookie(&http.Cookie{
			Name:  "leafwiki_at",
			Value: "invalid-token-123",
		})
		w := httptest.NewRecorder()

		router.ServeHTTP(w, req)

		Expect(w).To(matchAuthMiddlewareError(expectedAuthTokenInvalid))
		Expect(w).To(HaveHTTPStatus(http.StatusUnauthorized))
	})

	It("sets the authenticated user in context for downstream handlers", func() {
		gin.SetMode(gin.TestMode)

		fixture := createTestAuthFixture()
		DeferCleanup(fixture.close)

		authCookies := authmw.NewAuthCookies(true, time.Hour, time.Hour*24)

		// Login to get a valid token
		authToken, err := fixture.auth.Login("admin", "admin")
		Expect(err).To(Succeed())

		router := gin.New()

		// Apply RequireAuth with authDisabled=false
		router.Use(authmw.RequireAuth(fixture.auth, authCookies, false))

		var downstreamUser *coreauth.User

		router.GET("/test", func(c *gin.Context) {
			downstreamUser = c.MustGet("user").(*coreauth.User)
			c.JSON(http.StatusOK, gin.H{"ok": true})
		})

		req := httptest.NewRequest("GET", "/test", nil)
		req.AddCookie(&http.Cookie{
			Name:  "leafwiki_at",
			Value: authToken.Token,
		})
		w := httptest.NewRecorder()

		router.ServeHTTP(w, req)

		Expect(w).To(HaveHTTPStatus(http.StatusOK))
		Expect(downstreamUser).To(SatisfyAll(
			HaveField("Username", Equal("admin")),
			HaveField("Role", Equal(coreauth.RoleAdmin)),
		))
	})

	It("exposes the authenticated user through downstream current-user helpers", func() {
		gin.SetMode(gin.TestMode)

		fixture := createTestAuthFixture()
		DeferCleanup(fixture.close)

		authCookies := authmw.NewAuthCookies(true, time.Hour, time.Hour*24)
		authToken, err := fixture.auth.Login("admin", "admin")
		Expect(err).To(Succeed())
		router := gin.New()
		router.Use(authmw.RequireAuth(fixture.auth, authCookies, false))
		var requiredUser *coreauth.User
		var optionalUser *coreauth.User
		router.GET("/me", func(c *gin.Context) {
			requiredUser = authmw.MustGetUser(c)
			optionalUser = authmw.TryGetUser(c)
			c.Status(http.StatusNoContent)
		})

		req := httptest.NewRequest(http.MethodGet, "/me", nil)
		req.AddCookie(&http.Cookie{
			Name:  "leafwiki_at",
			Value: authToken.Token,
		})
		w := httptest.NewRecorder()

		router.ServeHTTP(w, req)

		Expect(w).To(HaveHTTPStatus(http.StatusNoContent))
		Expect(requiredUser).To(SatisfyAll(
			HaveField("Username", Equal("admin")),
			HaveField("Role", Equal(coreauth.RoleAdmin)),
		))
		Expect(optionalUser).To(Equal(requiredUser))
	})

	It("stops the handler chain when authentication fails", func() {
		gin.SetMode(gin.TestMode)

		fixture := createTestAuthFixture()
		DeferCleanup(fixture.close)

		authCookies := authmw.NewAuthCookies(true, time.Hour, time.Hour*24)

		router := gin.New()

		// Apply RequireAuth with authDisabled=false
		router.Use(authmw.RequireAuth(fixture.auth, authCookies, false))

		next := &middlewareFlowProbe{}

		router.Use(next.Record)

		router.GET("/test", func(c *gin.Context) {
			c.JSON(http.StatusOK, gin.H{"ok": true})
		})

		req := httptest.NewRequest("GET", "/test", nil)
		w := httptest.NewRecorder()

		router.ServeHTTP(w, req)

		Expect(w).To(HaveHTTPStatus(http.StatusUnauthorized))
		Expect(next.reachedPaths).To(BeEmpty())
	})

	DescribeTable("required authentication scenario matrix",
		func(tc requireAuthComprehensiveScenario) {
			gin.SetMode(gin.TestMode)

			fixture := createTestAuthFixture()
			DeferCleanup(fixture.close)

			authCookies := authmw.NewAuthCookies(true, time.Hour, time.Hour*24)

			router := gin.New()

			// Inject user if needed
			if tc.injectUser {
				router.Use(func(c *gin.Context) {
					c.Set("user", &coreauth.User{
						ID:       coreauth.UserIDFromString("public-editor"),
						Username: "public-editor",
						Role:     coreauth.RoleEditor,
					})
					c.Next()
				})
			}

			// Apply RequireAuth
			router.Use(authmw.RequireAuth(fixture.auth, authCookies, tc.authDisabled))

			router.GET("/test", func(c *gin.Context) {
				c.JSON(http.StatusOK, gin.H{"ok": true})
			})

			req := httptest.NewRequest("GET", "/test", nil)

			// Add token if needed
			if tc.provideToken {
				var token string
				if tc.validToken {
					authToken, err := fixture.auth.Login("admin", "admin")
					Expect(err).To(Succeed())
					token = authToken.Token
				} else {
					token = "invalid-token"
				}
				req.AddCookie(&http.Cookie{
					Name:  "leafwiki_at",
					Value: token,
				})
			}

			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)

			Expect(w).To(HaveHTTPStatus(tc.expectedStatus))

			if tc.expectedCode != "" {
				Expect(w).To(matchAuthMiddlewareError(tc.expectedCode))
			}
		},
		Entry("allows an injected user when authentication is disabled", requireAuthComprehensiveScenario{
			authDisabled:   true,
			injectUser:     true,
			provideToken:   false,
			validToken:     false,
			expectedStatus: http.StatusOK,
		}),
		Entry("rejects disabled-auth requests without an injected user", requireAuthComprehensiveScenario{
			authDisabled:   true,
			injectUser:     false,
			provideToken:   false,
			validToken:     false,
			expectedStatus: http.StatusUnauthorized,
			expectedCode:   expectedAuthDisabledMissingUser,
		}),
		Entry("allows requests with a valid access token", requireAuthComprehensiveScenario{
			authDisabled:   false,
			injectUser:     false,
			provideToken:   true,
			validToken:     true,
			expectedStatus: http.StatusOK,
		}),
		Entry("rejects requests without an access token", requireAuthComprehensiveScenario{
			authDisabled:   false,
			injectUser:     false,
			provideToken:   false,
			validToken:     false,
			expectedStatus: http.StatusUnauthorized,
			expectedCode:   expectedAuthAccessTokenMissing,
		}),
		Entry("rejects requests with an invalid access token", requireAuthComprehensiveScenario{
			authDisabled:   false,
			injectUser:     false,
			provideToken:   true,
			validToken:     false,
			expectedStatus: http.StatusUnauthorized,
			expectedCode:   expectedAuthTokenInvalid,
		}),
	)

})

type middlewareFlowProbe struct {
	reachedPaths []string
}

func (p *middlewareFlowProbe) Record(c *gin.Context) {
	p.reachedPaths = append(p.reachedPaths, c.Request.URL.Path)
	c.Next()
}
