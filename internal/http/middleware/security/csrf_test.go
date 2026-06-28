package security

import (
	"crypto/tls"
	"encoding/json"
	. "github.com/onsi/ginkgo/v2"
	"net/http"
	"net/http/httptest"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

var _ = It("TestCSRFMiddleware_AllowsSafeMethodsWithoutToken", func() {
	t := GinkgoT()
	gin.SetMode(gin.TestMode)

	csrf := NewCSRFCookie(false, time.Hour)

	router := gin.New()
	router.Use(CSRFMiddleware(csrf))
	router.GET("/safe", func(c *gin.Context) {
		c.Status(http.StatusOK)
	})

	req := httptest.NewRequest("GET", "/safe", nil)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200 for GET without CSRF, got %d", w.Code)
	}

})

var _ = It("TestCSRFMiddleware_BlocksPostWithoutCookie", func() {
	t := GinkgoT()
	gin.SetMode(gin.TestMode)

	csrf := NewCSRFCookie(false, time.Hour)

	router := gin.New()
	router.Use(CSRFMiddleware(csrf))
	router.POST("/protected", func(c *gin.Context) {
		c.Status(http.StatusOK)
	})

	req := httptest.NewRequest("POST", "/protected", nil)
	req.TLS = &tls.ConnectionState{} // ensure requireSecure in Read treats the request as "secure"
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("expected status 403 for POST without CSRF cookie, got %d", w.Code)
	}

	assertCSRFStructuredError(t, w, "csrf_token_missing", "errors.csrf.token_missing", "CSRF token missing")

})

var _ = It("TestCSRFMiddleware_BlocksPostWithCookieButNoHeader", func() {
	t := GinkgoT()
	gin.SetMode(gin.TestMode)

	csrf := NewCSRFCookie(false, time.Hour)

	router := gin.New()
	router.Use(CSRFMiddleware(csrf))
	router.POST("/protected", func(c *gin.Context) {
		c.Status(http.StatusOK)
	})

	req := httptest.NewRequest("POST", "/protected", nil)
	req.TLS = &tls.ConnectionState{}
	// Cookie vorhanden, aber kein Header/Form-Token
	req.AddCookie(&http.Cookie{
		Name:  "__Host-leafwiki_csrf",
		Value: "test-token",
	})

	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("expected status 403 for POST with cookie but no header, got %d", w.Code)
	}

	assertCSRFStructuredError(t, w, "csrf_token_invalid", "errors.csrf.token_invalid", "Invalid CSRF token")

})

var _ = It("TestCSRFMiddleware_BlocksPostWithMismatchingTokens", func() {
	t := GinkgoT()
	gin.SetMode(gin.TestMode)

	csrf := NewCSRFCookie(false, time.Hour)

	router := gin.New()
	router.Use(CSRFMiddleware(csrf))
	router.POST("/protected", func(c *gin.Context) {
		c.Status(http.StatusOK)
	})

	body := strings.NewReader("")
	req := httptest.NewRequest("POST", "/protected", body)
	req.TLS = &tls.ConnectionState{}
	req.AddCookie(&http.Cookie{
		Name:  "__Host-leafwiki_csrf",
		Value: "cookie-token",
	})
	req.Header.Set("X-CSRF-Token", "different-header-token")

	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("expected status 403 for POST with mismatching tokens, got %d", w.Code)
	}

	assertCSRFStructuredError(t, w, "csrf_token_invalid", "errors.csrf.token_invalid", "Invalid CSRF token")

})

var _ = It("TestCSRFMiddleware_AllowsPostWithMatchingTokens", func() {
	t := GinkgoT()
	gin.SetMode(gin.TestMode)

	csrf := NewCSRFCookie(false, time.Hour)

	router := gin.New()
	router.Use(CSRFMiddleware(csrf))
	router.POST("/protected", func(c *gin.Context) {
		c.Status(http.StatusOK)
	})

	const token = "same-token"
	body := strings.NewReader("")
	req := httptest.NewRequest("POST", "/protected", body)
	req.TLS = &tls.ConnectionState{}
	req.AddCookie(&http.Cookie{
		Name:  "__Host-leafwiki_csrf",
		Value: token,
	})
	req.Header.Set("X-CSRF-Token", token)

	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200 for POST with valid CSRF, got %d", w.Code)
	}

})

var _ = It("allows trusted private-channel POST requests without a CSRF token", func() {
	t := GinkgoT()
	gin.SetMode(gin.TestMode)

	csrf := NewCSRFCookie(false, time.Hour)

	router := gin.New()
	router.Use(func(c *gin.Context) {
		TrustPrivateChannel(c)
		c.Next()
	})
	router.Use(CSRFMiddleware(csrf))
	router.POST("/private", func(c *gin.Context) {
		c.Status(http.StatusNoContent)
	})

	req := httptest.NewRequest("POST", "/private", nil)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	if w.Code != http.StatusNoContent {
		t.Fatalf("expected status 204 for trusted private channel without CSRF, got %d: %s", w.Code, w.Body.String())
	}
})

type securityTestTB interface {
	Helper()
	Fatalf(format string, args ...any)
}

func assertCSRFStructuredError(t securityTestTB, rec *httptest.ResponseRecorder, code string, messageID string, message string) {
	t.Helper()
	var body struct {
		Error struct {
			Code      string `json:"code"`
			MessageID string `json:"messageId"`
			Message   string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode csrf error: %v; body=%s", err, rec.Body.String())
	}
	if body.Error.Code != code || body.Error.MessageID != messageID || body.Error.Message != message {
		t.Fatalf("csrf error = %#v, want code=%q messageId=%q message=%q", body.Error, code, messageID, message)
	}
}
