package auth_test

import (
	"net/http"
	"net/http/httptest"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/gin-gonic/gin"
	coreauth "github.com/perber/wiki/internal/core/auth"
	authmw "github.com/perber/wiki/internal/http/middleware/auth"
)

type proxyFixture struct {
	userService *coreauth.UserService
	close       func() error
}

func cleanupWithErrorCheck(name string, closeFn func() error) {
	GinkgoHelper()

	DeferCleanup(func() {
		Expect(closeFn()).To(Succeed(), "close %s", name)
	})
}

func createProxyFixture() *proxyFixture {
	GinkgoHelper()

	storageDir := authMiddlewareTempDir()
	userStore, err := coreauth.NewUserStore(storageDir)
	Expect(err).To(Succeed())

	userService := coreauth.NewUserService(userStore)
	if err := userService.InitDefaultAdmin("admin"); err != nil {
		_ = userStore.Close()
		Expect(err).To(Succeed())
	}

	return &proxyFixture{
		userService: userService,
		close:       userStore.Close,
	}
}

func mustParseTrustedProxies(raw string) *authmw.TrustedProxies {
	GinkgoHelper()
	tp, err := authmw.ParseTrustedProxies(raw)
	Expect(err).To(Succeed())
	return tp
}

func proxyRouter(cfg authmw.RemoteUserConfig) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(authmw.InjectRemoteUser(cfg))
	r.GET("/test", func(c *gin.Context) {
		userVal, exists := c.Get("user")
		if !exists {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "no user"})
			return
		}
		u := userVal.(*coreauth.User)
		c.JSON(http.StatusOK, gin.H{"username": u.Username})
	})
	return r
}

var _ = Describe("reverse proxy user injection", Label("integration"), func() {
	It("does not inject remote users when the feature is disabled", func() {
		f := createProxyFixture()
		cleanupWithErrorCheck("proxy fixture", f.close)

		cfg := authmw.RemoteUserConfig{
			Enabled:        false,
			HeaderName:     "Remote-User",
			TrustedProxies: mustParseTrustedProxies("127.0.0.1"),
			UserService:    f.userService,
		}

		req := httptest.NewRequest("GET", "/test", nil)
		req.RemoteAddr = "127.0.0.1:1234"
		req.Header.Set("Remote-User", "admin")
		w := httptest.NewRecorder()

		proxyRouter(cfg).ServeHTTP(w, req)

		// Disabled → no user injected → handler returns 401
		Expect(w).To(HaveHTTPStatus(http.StatusUnauthorized))
	})

	It("ignores remote user headers from untrusted addresses", func() {
		f := createProxyFixture()
		cleanupWithErrorCheck("proxy fixture", f.close)

		cfg := authmw.RemoteUserConfig{
			Enabled:        true,
			HeaderName:     "Remote-User",
			TrustedProxies: mustParseTrustedProxies("10.0.0.1"),
			UserService:    f.userService,
		}

		req := httptest.NewRequest("GET", "/test", nil)
		req.RemoteAddr = "192.168.1.99:1234" // not trusted
		req.Header.Set("Remote-User", "admin")
		w := httptest.NewRecorder()

		proxyRouter(cfg).ServeHTTP(w, req)

		// Untrusted → header ignored → no user → 401
		Expect(w).To(HaveHTTPStatus(http.StatusUnauthorized))
	})

	It("requires the remote user header from trusted addresses", func() {
		f := createProxyFixture()
		cleanupWithErrorCheck("proxy fixture", f.close)

		cfg := authmw.RemoteUserConfig{
			Enabled:        true,
			HeaderName:     "Remote-User",
			TrustedProxies: mustParseTrustedProxies("127.0.0.1"),
			UserService:    f.userService,
		}

		req := httptest.NewRequest("GET", "/test", nil)
		req.RemoteAddr = "127.0.0.1:1234"
		// no Remote-User header
		w := httptest.NewRecorder()

		proxyRouter(cfg).ServeHTTP(w, req)

		// No header → no user injected → handler returns 401
		Expect(w).To(HaveHTTPStatus(http.StatusUnauthorized))
	})

	It("injects a known remote user from a trusted address", func() {
		f := createProxyFixture()
		cleanupWithErrorCheck("proxy fixture", f.close)

		cfg := authmw.RemoteUserConfig{
			Enabled:        true,
			HeaderName:     "Remote-User",
			TrustedProxies: mustParseTrustedProxies("127.0.0.1"),
			UserService:    f.userService,
		}

		req := httptest.NewRequest("GET", "/test", nil)
		req.RemoteAddr = "127.0.0.1:1234"
		req.Header.Set("Remote-User", "admin")
		w := httptest.NewRecorder()

		proxyRouter(cfg).ServeHTTP(w, req)

		Expect(w).To(HaveHTTPStatus(http.StatusOK))
		Expect(w.Body.String()).To(MatchJSON(`{"username":"admin"}`))
	})

	It("returns a structured not-found error for unknown remote users", func() {
		f := createProxyFixture()
		cleanupWithErrorCheck("proxy fixture", f.close)

		cfg := authmw.RemoteUserConfig{
			Enabled:        true,
			HeaderName:     "Remote-User",
			TrustedProxies: mustParseTrustedProxies("127.0.0.1"),
			UserService:    f.userService,
		}

		req := httptest.NewRequest("GET", "/test", nil)
		req.RemoteAddr = "127.0.0.1:1234"
		req.Header.Set("Remote-User", "ghost")
		w := httptest.NewRecorder()

		proxyRouter(cfg).ServeHTTP(w, req)

		Expect(w).To(HaveHTTPStatus(http.StatusUnauthorized))
		Expect(w).To(matchAuthMiddlewareError(expectedAuthRemoteUserNotFound))
	})

	It("uses the configured remote user header name", func() {
		f := createProxyFixture()
		cleanupWithErrorCheck("proxy fixture", f.close)

		cfg := authmw.RemoteUserConfig{
			Enabled:        true,
			HeaderName:     "X-Forwarded-User",
			TrustedProxies: mustParseTrustedProxies("127.0.0.1"),
			UserService:    f.userService,
		}

		req := httptest.NewRequest("GET", "/test", nil)
		req.RemoteAddr = "127.0.0.1:1234"
		req.Header.Set("X-Forwarded-User", "admin")
		w := httptest.NewRecorder()

		proxyRouter(cfg).ServeHTTP(w, req)

		Expect(w).To(HaveHTTPStatus(http.StatusOK))
	})

	It("trusts remote user headers from configured CIDR ranges", func() {
		f := createProxyFixture()
		cleanupWithErrorCheck("proxy fixture", f.close)

		cfg := authmw.RemoteUserConfig{
			Enabled:        true,
			HeaderName:     "Remote-User",
			TrustedProxies: mustParseTrustedProxies("172.18.0.0/16"),
			UserService:    f.userService,
		}

		req := httptest.NewRequest("GET", "/test", nil)
		req.RemoteAddr = "172.18.5.10:1234"
		req.Header.Set("Remote-User", "admin")
		w := httptest.NewRecorder()

		proxyRouter(cfg).ServeHTTP(w, req)

		Expect(w).To(HaveHTTPStatus(http.StatusOK))
	})

	It("returns a structured error when trusted proxy configuration is missing", func() {
		f := createProxyFixture()
		cleanupWithErrorCheck("proxy fixture", f.close)

		cfg := authmw.RemoteUserConfig{
			Enabled:        true,
			HeaderName:     "Remote-User",
			TrustedProxies: nil,
			UserService:    f.userService,
		}

		req := httptest.NewRequest("GET", "/test", nil)
		req.RemoteAddr = "127.0.0.1:1234"
		req.Header.Set("Remote-User", "admin")
		w := httptest.NewRecorder()

		proxyRouter(cfg).ServeHTTP(w, req)

		Expect(w).To(HaveHTTPStatus(http.StatusInternalServerError))
		Expect(w).To(matchAuthMiddlewareError(expectedAuthReverseProxyMisconfigured))
	})

	It("returns a structured error when user lookup service is missing", func() {
		cfg := authmw.RemoteUserConfig{
			Enabled:        true,
			HeaderName:     "Remote-User",
			TrustedProxies: mustParseTrustedProxies("127.0.0.1"),
			UserService:    nil,
		}

		req := httptest.NewRequest("GET", "/test", nil)
		req.RemoteAddr = "127.0.0.1:1234"
		req.Header.Set("Remote-User", "admin")
		w := httptest.NewRecorder()

		proxyRouter(cfg).ServeHTTP(w, req)

		Expect(w).To(HaveHTTPStatus(http.StatusInternalServerError))
		Expect(w).To(matchAuthMiddlewareError(expectedAuthReverseProxyMisconfigured))
	})

	// TestInjectRemoteUser_WithRequireAuth verifies the full middleware chain:
	// InjectRemoteUser sets the user, then RequireAuth short-circuits JWT validation.
	It("lets trusted remote users satisfy the required-auth middleware without a JWT cookie", func() {
		f := createProxyFixture()
		cleanupWithErrorCheck("proxy fixture", f.close)

		storageDir := authMiddlewareTempDir()
		sessionStore, err := coreauth.NewSessionStore(storageDir)
		Expect(err).To(Succeed())
		cleanupWithErrorCheck("session store", sessionStore.Close)

		authService := coreauth.NewAuthService(f.userService, sessionStore, "test-secret-key-for-unit-tests-1", 0, 0)
		authCookies := authmw.NewAuthCookies(true, 0, 0)

		cfg := authmw.RemoteUserConfig{
			Enabled:        true,
			HeaderName:     "Remote-User",
			TrustedProxies: mustParseTrustedProxies("127.0.0.1"),
			UserService:    f.userService,
		}

		gin.SetMode(gin.TestMode)
		r := gin.New()
		r.Use(authmw.InjectRemoteUser(cfg))
		r.Use(authmw.RequireAuth(authService, authCookies, false))
		r.GET("/test", func(c *gin.Context) {
			u := c.MustGet("user").(*coreauth.User)
			c.JSON(http.StatusOK, gin.H{"username": u.Username})
		})

		req := httptest.NewRequest("GET", "/test", nil)
		req.RemoteAddr = "127.0.0.1:1234"
		req.Header.Set("Remote-User", "admin")
		// no JWT cookie — proxy auth should take over
		w := httptest.NewRecorder()

		r.ServeHTTP(w, req)

		Expect(w).To(HaveHTTPStatus(http.StatusOK))
	})
})
