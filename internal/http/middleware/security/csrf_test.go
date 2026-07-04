package security

import (
	"crypto/tls"
	"net/http"
	"net/http/httptest"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/types"
	sharederrors "github.com/perber/wiki/internal/core/shared/errors"
	testmatchers "github.com/perber/wiki/internal/test_utils/matchers"
)

func haveStructuredSecurityError(code sharederrors.ErrorCode) types.GomegaMatcher {
	return testmatchers.HaveStructuredError(code, sharederrors.MessageIDForCode(code))
}

func performSecurityRequest(router *gin.Engine, req *http.Request) *httptest.ResponseRecorder {
	GinkgoHelper()

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec
}

var _ = Describe("CSRF middleware", Label("integration"), func() {
	BeforeEach(func() {
		gin.SetMode(gin.TestMode)
	})

	It("allows safe methods without a CSRF token", func() {
		csrf := NewCSRFCookie(false, time.Hour)
		router := gin.New()
		router.Use(CSRFMiddleware(csrf))
		router.GET("/safe", func(c *gin.Context) {
			c.Status(http.StatusOK)
		})

		req := httptest.NewRequest(http.MethodGet, "/safe", nil)
		rec := performSecurityRequest(router, req)

		Expect(rec).To(HaveHTTPStatus(http.StatusOK))
	})

	When("a mutating request has no readable CSRF cookie", func() {
		It("blocks the request with a structured missing-token error", func() {
			csrf := NewCSRFCookie(false, time.Hour)
			router := gin.New()
			router.Use(CSRFMiddleware(csrf))
			router.POST("/protected", func(c *gin.Context) {
				c.Status(http.StatusOK)
			})

			req := httptest.NewRequest(http.MethodPost, "/protected", nil)
			req.TLS = &tls.ConnectionState{}
			rec := performSecurityRequest(router, req)

			Expect(rec).To(HaveHTTPStatus(http.StatusForbidden))
			Expect(rec.Body.Bytes()).To(haveStructuredSecurityError(errCodeCSRFTokenMissing))
		})
	})

	When("a mutating request has a cookie but no submitted token", func() {
		It("blocks the request with a structured invalid-token error", func() {
			csrf := NewCSRFCookie(false, time.Hour)
			router := gin.New()
			router.Use(CSRFMiddleware(csrf))
			router.POST("/protected", func(c *gin.Context) {
				c.Status(http.StatusOK)
			})

			req := httptest.NewRequest(http.MethodPost, "/protected", nil)
			req.TLS = &tls.ConnectionState{}
			req.AddCookie(&http.Cookie{
				Name:  csrf.cookieName(true),
				Value: "test-token",
			})
			rec := performSecurityRequest(router, req)

			Expect(rec).To(HaveHTTPStatus(http.StatusForbidden))
			Expect(rec.Body.Bytes()).To(haveStructuredSecurityError(errCodeCSRFTokenInvalid))
		})
	})

	When("a mutating request submits a token that differs from the cookie", func() {
		It("blocks the request with a structured invalid-token error", func() {
			csrf := NewCSRFCookie(false, time.Hour)
			router := gin.New()
			router.Use(CSRFMiddleware(csrf))
			router.POST("/protected", func(c *gin.Context) {
				c.Status(http.StatusOK)
			})

			req := httptest.NewRequest(http.MethodPost, "/protected", strings.NewReader(""))
			req.TLS = &tls.ConnectionState{}
			req.AddCookie(&http.Cookie{
				Name:  csrf.cookieName(true),
				Value: "cookie-token",
			})
			req.Header.Set("X-CSRF-Token", "different-header-token")
			rec := performSecurityRequest(router, req)

			Expect(rec).To(HaveHTTPStatus(http.StatusForbidden))
			Expect(rec.Body.Bytes()).To(haveStructuredSecurityError(errCodeCSRFTokenInvalid))
		})
	})

	When("a mutating request submits the same token as the cookie", func() {
		It("allows the protected handler to run", func() {
			csrf := NewCSRFCookie(false, time.Hour)
			router := gin.New()
			router.Use(CSRFMiddleware(csrf))
			router.POST("/protected", func(c *gin.Context) {
				c.Status(http.StatusOK)
			})

			const token = "same-token"
			req := httptest.NewRequest(http.MethodPost, "/protected", strings.NewReader(""))
			req.TLS = &tls.ConnectionState{}
			req.AddCookie(&http.Cookie{
				Name:  csrf.cookieName(true),
				Value: token,
			})
			req.Header.Set("X-CSRF-Token", token)
			rec := performSecurityRequest(router, req)

			Expect(rec).To(HaveHTTPStatus(http.StatusOK))
		})
	})

	When("a mutating request comes through a trusted private channel", func() {
		It("allows the handler without requiring a CSRF token", func() {
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

			req := httptest.NewRequest(http.MethodPost, "/private", nil)
			rec := performSecurityRequest(router, req)

			Expect(rec).To(HaveHTTPStatus(http.StatusNoContent))
		})
	})
})
