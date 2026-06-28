package auth_test

import (
	"encoding/json"
	. "github.com/onsi/ginkgo/v2"
	"net/http"
	"net/http/httptest"
	"time"

	"github.com/gin-gonic/gin"
	coreauth "github.com/perber/wiki/internal/core/auth"
	authmw "github.com/perber/wiki/internal/http/middleware/auth"
)

type authFixture struct {
	auth  *coreauth.AuthService
	close func() error
}

type testTB interface {
	Helper()
	Cleanup(func())
	TempDir() string
	Errorf(format string, args ...any)
	Fatalf(format string, args ...any)
}

type requireAuthComprehensiveScenario struct {
	authDisabled   bool
	injectUser     bool
	provideToken   bool
	validToken     bool
	expectedStatus int
	expectedError  string
}

func createTestAuthFixture(t testTB) *authFixture {
	t.Helper()

	storageDir := t.TempDir()
	userStore, err := coreauth.NewUserStore(storageDir)
	if err != nil {
		t.Fatalf("Failed to create user store: %v", err)
	}
	sessionStore, err := coreauth.NewSessionStore(storageDir)
	if err != nil {
		_ = userStore.Close()
		t.Fatalf("Failed to create session store: %v", err)
	}

	userService := coreauth.NewUserService(userStore)
	if err := userService.InitDefaultAdmin("admin"); err != nil {
		_ = sessionStore.Close()
		_ = userStore.Close()
		t.Fatalf("Failed to init default admin: %v", err)
	}

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

var _ = It("TestRequireAuth_WithAuthDisabled_UserExists", func() {
	t := GinkgoT()
	gin.SetMode(gin.TestMode)

	authCookies := authmw.NewAuthCookies(true, time.Hour, time.Hour*24)

	router := gin.New()

	// Middleware to inject user (simulating authmw.InjectPublicEditor)
	router.Use(func(c *gin.Context) {
		c.Set("user", &coreauth.User{
			ID:       "public-editor",
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

	if w2.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d - %s", w2.Code, w2.Body.String())
	}

	expectedBody := `{"role":"editor","username":"public-editor"}`
	if w2.Body.String() != expectedBody {
		t.Errorf("Expected body %s, got %s", expectedBody, w2.Body.String())
	}

})

var _ = It("TestRequireAuth_WithAuthDisabled_NoUser", func() {
	t := GinkgoT()
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

	if w2.Code != http.StatusUnauthorized {
		t.Errorf("Expected status 401 when authDisabled=true but no user, got %d", w2.Code)
	}

	assertAuthMiddlewareError(t, w2, "auth_disabled_missing_user", "errors.auth.disabled_missing_user", "User not authenticated and auth is disabled")

})

var _ = It("TestRequireAuth_WithInvalidUserContext_NilUser", func() {
	t := GinkgoT()
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

	if w.Code != http.StatusInternalServerError {
		t.Errorf("Expected status 500 for nil user context, got %d", w.Code)
	}

	assertAuthMiddlewareError(t, w, "auth_invalid_user_context", "errors.auth.invalid_user_context", "Invalid user context")

})

var _ = It("TestRequireAuth_WithInvalidUserContext_WrongType", func() {
	t := GinkgoT()
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

	if w.Code != http.StatusInternalServerError {
		t.Errorf("Expected status 500 for invalid user type, got %d", w.Code)
	}

	assertAuthMiddlewareError(t, w, "auth_invalid_user_context", "errors.auth.invalid_user_context", "Invalid user context")

})

var _ = It("TestRequireAuth_WithAuthEnabled_ValidToken", func() {
	t := GinkgoT()
	gin.SetMode(gin.TestMode)

	fixture := createTestAuthFixture(t)
	defer func() {
		if err := fixture.close(); err != nil {
			t.Fatalf("close auth fixture: %v", err)
		}
	}()

	authCookies := authmw.NewAuthCookies(true, time.Hour, time.Hour*24)

	// Login to get a valid token
	authToken, err := fixture.auth.Login("admin", "admin")
	if err != nil {
		t.Fatalf("Failed to login: %v", err)
	}

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

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d - %s", w.Code, w.Body.String())
	}

	expectedBody := `{"role":"admin","username":"admin"}`
	if w.Body.String() != expectedBody {
		t.Errorf("Expected body %s, got %s", expectedBody, w.Body.String())
	}

})

var _ = It("TestRequireAuth_WithAuthEnabled_MissingToken", func() {
	t := GinkgoT()
	gin.SetMode(gin.TestMode)

	fixture := createTestAuthFixture(t)
	defer func() {
		if err := fixture.close(); err != nil {
			t.Fatalf("close auth fixture: %v", err)
		}
	}()

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

	if w.Code != http.StatusUnauthorized {
		t.Errorf("Expected status 401 when no token provided, got %d", w.Code)
	}

	assertAuthMiddlewareError(t, w, "auth_access_token_missing", "errors.auth.access_token_missing", "Missing or invalid access token")

})

var _ = It("TestRequireAuth_WithAuthEnabled_NilAuthService", func() {
	t := GinkgoT()
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

	if w.Code != http.StatusInternalServerError {
		t.Errorf("Expected status 500 when auth service is nil, got %d", w.Code)
	}

	assertAuthMiddlewareError(t, w, "auth_service_unavailable", "errors.auth.service_unavailable", "Authentication service unavailable")

})

var _ = It("TestRequireAuth_WithAuthEnabled_InvalidToken", func() {
	t := GinkgoT()
	gin.SetMode(gin.TestMode)

	fixture := createTestAuthFixture(t)
	defer func() {
		if err := fixture.close(); err != nil {
			t.Fatalf("close auth fixture: %v", err)
		}
	}()

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

	if w.Code != http.StatusUnauthorized {
		t.Errorf("Expected status 401 when invalid token provided, got %d", w.Code)
	}

	assertAuthMiddlewareError(t, w, "auth_token_invalid", "errors.auth.token_invalid", "Invalid or expired token")

})

var _ = It("TestRequireAuth_WithAuthEnabled_UserSetInContext", func() {
	t := GinkgoT()
	gin.SetMode(gin.TestMode)

	fixture := createTestAuthFixture(t)
	defer func() {
		if err := fixture.close(); err != nil {
			t.Fatalf("close auth fixture: %v", err)
		}
	}()

	authCookies := authmw.NewAuthCookies(true, time.Hour, time.Hour*24)

	// Login to get a valid token
	authToken, err := fixture.auth.Login("admin", "admin")
	if err != nil {
		t.Fatalf("Failed to login: %v", err)
	}

	router := gin.New()

	// Apply RequireAuth with authDisabled=false
	router.Use(authmw.RequireAuth(fixture.auth, authCookies, false))

	userSetInContext := false

	router.GET("/test", func(c *gin.Context) {
		_, exists := c.Get("user")
		if exists {
			userSetInContext = true
		}
		c.JSON(http.StatusOK, gin.H{"ok": true})
	})

	req := httptest.NewRequest("GET", "/test", nil)
	req.AddCookie(&http.Cookie{
		Name:  "leafwiki_at",
		Value: authToken.Token,
	})
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", w.Code)
	}

	if !userSetInContext {
		t.Error("Expected user to be set in context")
	}

})

var _ = It("TestRequireAuth_NextNotCalledOnFailure", func() {
	t := GinkgoT()
	gin.SetMode(gin.TestMode)

	fixture := createTestAuthFixture(t)
	defer func() {
		if err := fixture.close(); err != nil {
			t.Fatalf("close auth fixture: %v", err)
		}
	}()

	authCookies := authmw.NewAuthCookies(true, time.Hour, time.Hour*24)

	router := gin.New()

	// Apply RequireAuth with authDisabled=false
	router.Use(authmw.RequireAuth(fixture.auth, authCookies, false))

	nextCalled := false

	router.Use(func(c *gin.Context) {
		nextCalled = true
		c.Next()
	})

	router.GET("/test", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"ok": true})
	})

	req := httptest.NewRequest("GET", "/test", nil)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("Expected status 401, got %d", w.Code)
	}

	if nextCalled {
		t.Error("Expected Next() not to be called when authentication fails")
	}

})

var _ = DescribeTable("TestRequireAuth_ComprehensiveScenarios",
	func(tc requireAuthComprehensiveScenario) {
	t := GinkgoT()
	gin.SetMode(gin.TestMode)

	fixture := createTestAuthFixture(t)
	defer func() {
		if err := fixture.close(); err != nil {
			t.Fatalf("close auth fixture: %v", err)
		}
	}()

	authCookies := authmw.NewAuthCookies(true, time.Hour, time.Hour*24)

	router := gin.New()

	// Inject user if needed
	if tc.injectUser {
		router.Use(func(c *gin.Context) {
			c.Set("user", &coreauth.User{
				ID:       "public-editor",
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
			if err != nil {
				t.Fatalf("Failed to login: %v", err)
			}
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

	if w.Code != tc.expectedStatus {
		t.Errorf("Expected status %d, got %d - %s", tc.expectedStatus, w.Code, w.Body.String())
	}

	if tc.expectedError != "" {
		assertAuthMiddlewareErrorMessage(t, w, tc.expectedError)
	}
},
	Entry("authDisabled=true, user injected - should pass", requireAuthComprehensiveScenario{
		authDisabled:   true,
		injectUser:     true,
		provideToken:   false,
		validToken:     false,
		expectedStatus: http.StatusOK,
	}),
	Entry("authDisabled=true, no user - should fail", requireAuthComprehensiveScenario{
		authDisabled:   true,
		injectUser:     false,
		provideToken:   false,
		validToken:     false,
		expectedStatus: http.StatusUnauthorized,
		expectedError:  "User not authenticated and auth is disabled",
	}),
	Entry("authDisabled=false, valid token - should pass", requireAuthComprehensiveScenario{
		authDisabled:   false,
		injectUser:     false,
		provideToken:   true,
		validToken:     true,
		expectedStatus: http.StatusOK,
	}),
	Entry("authDisabled=false, no token - should fail", requireAuthComprehensiveScenario{
		authDisabled:   false,
		injectUser:     false,
		provideToken:   false,
		validToken:     false,
		expectedStatus: http.StatusUnauthorized,
		expectedError:  "Missing or invalid access token",
	}),
	Entry("authDisabled=false, invalid token - should fail", requireAuthComprehensiveScenario{
		authDisabled:   false,
		injectUser:     false,
		provideToken:   true,
		validToken:     false,
		expectedStatus: http.StatusUnauthorized,
		expectedError:  "Invalid or expired token",
	}),
)

var _ = It("TestOptionalAuth_NoToken_PassesThrough", func() {
	t := GinkgoT()
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

	if w.Code != http.StatusOK {
		t.Errorf("expected 200 for no token, got %d: %s", w.Code, w.Body.String())
	}

})

var _ = It("TestOptionalAuth_ValidToken_SetsUser", func() {
	t := GinkgoT()
	gin.SetMode(gin.TestMode)
	fixture := createTestAuthFixture(t)
	defer func() {
		if err := fixture.close(); err != nil {
			t.Fatalf("close: %v", err)
		}
	}()

	authCookies := authmw.NewAuthCookies(true, time.Hour, time.Hour*24)
	authToken, err := fixture.auth.Login("admin", "admin")
	if err != nil {
		t.Fatalf("login: %v", err)
	}

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

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if w.Body.String() != `{"username":"admin"}` {
		t.Errorf("unexpected body: %s", w.Body.String())
	}

})

var _ = It("TestOptionalAuth_InvalidToken_PassesThrough", func() {
	t := GinkgoT()
	gin.SetMode(gin.TestMode)
	fixture := createTestAuthFixture(t)
	defer func() {
		if err := fixture.close(); err != nil {
			t.Fatalf("close: %v", err)
		}
	}()

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

	if w.Code != http.StatusOK {
		t.Errorf("expected 200 for invalid token, got %d: %s", w.Code, w.Body.String())
	}

})

var _ = It("TestOptionalAuth_NilAuthService_WithToken_Returns500", func() {
	t := GinkgoT()
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

	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected 500 for nil authService with token, got %d: %s", w.Code, w.Body.String())
	}
	assertAuthMiddlewareError(t, w, "auth_service_unavailable", "errors.auth.service_unavailable", "Authentication service unavailable")

})

var _ = It("TestOptionalAuth_UserAlreadyInContext_ShortCircuits", func() {
	t := GinkgoT()
	gin.SetMode(gin.TestMode)
	authCookies := authmw.NewAuthCookies(true, time.Hour, time.Hour*24)
	injected := &coreauth.User{ID: "proxy-user", Username: "proxy", Role: coreauth.RoleViewer}

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

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
	if w.Body.String() != `{"username":"proxy"}` {
		t.Errorf("unexpected body: %s", w.Body.String())
	}

})

func assertAuthMiddlewareError(t testTB, rec *httptest.ResponseRecorder, code string, messageID string, message string) {
	t.Helper()
	var body struct {
		Error struct {
			Code      string `json:"code"`
			MessageID string `json:"messageId"`
			Message   string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode auth middleware error: %v; body=%s", err, rec.Body.String())
	}
	if body.Error.Code != code || body.Error.MessageID != messageID || body.Error.Message != message {
		t.Fatalf("auth middleware error = %#v, want code=%q messageId=%q message=%q", body.Error, code, messageID, message)
	}
}

func assertAuthMiddlewareErrorMessage(t testTB, rec *httptest.ResponseRecorder, message string) {
	t.Helper()
	var body struct {
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode auth middleware error: %v; body=%s", err, rec.Body.String())
	}
	if body.Error.Message != message {
		t.Fatalf("auth middleware message = %q, want %q; body=%s", body.Error.Message, message, rec.Body.String())
	}
}
