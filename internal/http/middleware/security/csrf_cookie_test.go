package security

import (
	"crypto/tls"
	"errors"
	"net/http"
	"net/http/httptest"
	"time"

	"github.com/gin-gonic/gin"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/types"
	"github.com/perber/wiki/internal/http/middleware/utils"
)

func singleResponseCookie(rec *httptest.ResponseRecorder) *http.Cookie {
	GinkgoHelper()

	cookies := rec.Result().Cookies()
	Expect(cookies).To(HaveLen(1))
	return cookies[0]
}

func matchIssuedCSRFCookie(name string, secure bool, maxAge int) types.GomegaMatcher {
	return SatisfyAll(
		HaveField("Name", name),
		HaveField("Value", Not(BeEmpty())),
		HaveField("HttpOnly", BeFalse()),
		HaveField("Secure", secure),
		HaveField("Path", "/"),
		HaveField("SameSite", http.SameSiteLaxMode),
		HaveField("MaxAge", maxAge),
	)
}

func matchClearedCSRFCookie(name string, secure bool) types.GomegaMatcher {
	return SatisfyAll(
		HaveField("Name", name),
		HaveField("Value", BeEmpty()),
		HaveField("Secure", secure),
		HaveField("MaxAge", -1),
	)
}

var _ = Describe("CSRF cookie", func() {
	BeforeEach(func() {
		gin.SetMode(gin.TestMode)
	})

	Describe("cookie naming", func() {
		It("uses the host-prefixed cookie name for secure requests", func() {
			csrf := NewCSRFCookie(false, time.Hour)

			Expect(csrf.cookieName(true)).To(HavePrefix("__Host-"))
		})

		It("keeps secure and insecure cookie names distinct", func() {
			csrf := NewCSRFCookie(true, time.Hour)

			Expect(csrf.cookieName(false)).NotTo(Equal(csrf.cookieName(true)))
		})
	})

	Describe("issuing tokens", func() {
		It("sets a readable secure cookie and mirrors the token in the response header", func() {
			csrf := NewCSRFCookie(false, time.Hour)
			router := gin.New()
			router.GET("/test", func(c *gin.Context) {
				token, err := csrf.Issue(c)
				Expect(err).To(Succeed())
				c.String(http.StatusOK, token)
			})

			req := httptest.NewRequest(http.MethodGet, "/test", nil)
			req.TLS = &tls.ConnectionState{}
			rec := performSecurityRequest(router, req)

			Expect(rec).To(HaveHTTPStatus(http.StatusOK))
			csrfCookie := singleResponseCookie(rec)
			Expect(csrfCookie).To(matchIssuedCSRFCookie(csrf.cookieName(true), true, int(time.Hour.Seconds())))
			Expect(rec).To(HaveHTTPHeaderWithValue("X-CSRF-Token", csrfCookie.Value))
		})

		It("sets a readable insecure cookie when insecure requests are allowed", func() {
			csrf := NewCSRFCookie(true, time.Hour)
			router := gin.New()
			router.GET("/test", func(c *gin.Context) {
				token, err := csrf.Issue(c)
				Expect(err).To(Succeed())
				c.String(http.StatusOK, token)
			})

			req := httptest.NewRequest(http.MethodGet, "/test", nil)
			rec := performSecurityRequest(router, req)

			Expect(rec).To(HaveHTTPStatus(http.StatusOK))
			csrfCookie := singleResponseCookie(rec)
			Expect(csrfCookie).To(matchIssuedCSRFCookie(csrf.cookieName(false), false, int(time.Hour.Seconds())))
		})

		It("requires HTTPS before issuing secure cookies", func() {
			csrf := NewCSRFCookie(false, time.Hour)
			router := gin.New()
			router.GET("/test", func(c *gin.Context) {
				token, err := csrf.Issue(c)
				Expect(token).To(BeEmpty())
				Expect(err).To(MatchError(utils.ErrHTTPSRequired))
				c.Status(http.StatusBadRequest)
			})

			req := httptest.NewRequest(http.MethodGet, "/test", nil)
			rec := performSecurityRequest(router, req)

			Expect(rec).To(HaveHTTPStatus(http.StatusBadRequest))
		})

		It("returns token generation errors without setting cookies or headers", func() {
			csrf := NewCSRFCookie(true, time.Hour)
			tokenErr := errors.New("entropy unavailable")
			originalRandRead := csrfRandRead
			csrfRandRead = func([]byte) (int, error) {
				return 0, tokenErr
			}
			DeferCleanup(func() {
				csrfRandRead = originalRandRead
			})
			router := gin.New()
			router.GET("/test", func(c *gin.Context) {
				token, err := csrf.Issue(c)
				Expect(token).To(BeEmpty())
				Expect(err).To(MatchError(tokenErr))
				c.Status(http.StatusTeapot)
			})

			req := httptest.NewRequest(http.MethodGet, "/test", nil)
			rec := performSecurityRequest(router, req)

			Expect(rec).To(HaveHTTPStatus(http.StatusTeapot))
			Expect(rec.Result().Cookies()).To(BeEmpty())
			Expect(rec.Result().Header.Get("X-CSRF-Token")).To(BeEmpty())
		})

		It("reuses an existing secure CSRF cookie when issuing", func() {
			csrf := NewCSRFCookie(false, time.Hour)
			router := gin.New()
			router.GET("/test", func(c *gin.Context) {
				token, err := csrf.Issue(c)
				Expect(err).To(Succeed())
				c.String(http.StatusOK, token)
			})

			req := httptest.NewRequest(http.MethodGet, "/test", nil)
			req.TLS = &tls.ConnectionState{}
			req.AddCookie(&http.Cookie{Name: csrf.cookieName(true), Value: "existing-token"})
			rec := performSecurityRequest(router, req)

			Expect(rec).To(HaveHTTPStatus(http.StatusOK))
			Expect(rec).To(HaveHTTPBody("existing-token"))
			Expect(rec).To(HaveHTTPHeaderWithValue("X-CSRF-Token", "existing-token"))
			Expect(rec.Result().Cookies()).To(BeEmpty())
		})
	})

	Describe("reading tokens", func() {
		It("reads the secure cookie token from HTTPS requests", func() {
			csrf := NewCSRFCookie(false, time.Hour)
			router := gin.New()
			router.GET("/test", func(c *gin.Context) {
				token, err := csrf.Read(c)
				Expect(err).To(Succeed())
				Expect(token).To(Equal("test-csrf-token"))
				c.Status(http.StatusOK)
			})

			req := httptest.NewRequest(http.MethodGet, "/test", nil)
			req.TLS = &tls.ConnectionState{}
			req.AddCookie(&http.Cookie{
				Name:  csrf.cookieName(true),
				Value: "test-csrf-token",
			})
			rec := performSecurityRequest(router, req)

			Expect(rec).To(HaveHTTPStatus(http.StatusOK))
		})

		It("reads the insecure cookie token when insecure requests are allowed", func() {
			csrf := NewCSRFCookie(true, time.Hour)
			router := gin.New()
			router.GET("/test", func(c *gin.Context) {
				token, err := csrf.Read(c)
				Expect(err).To(Succeed())
				Expect(token).To(Equal("test-csrf-token-insecure"))
				c.Status(http.StatusOK)
			})

			req := httptest.NewRequest(http.MethodGet, "/test", nil)
			req.AddCookie(&http.Cookie{
				Name:  csrf.cookieName(false),
				Value: "test-csrf-token-insecure",
			})
			rec := performSecurityRequest(router, req)

			Expect(rec).To(HaveHTTPStatus(http.StatusOK))
		})

		It("returns an error when the secure cookie is missing", func() {
			csrf := NewCSRFCookie(false, time.Hour)
			router := gin.New()
			router.GET("/test", func(c *gin.Context) {
				token, err := csrf.Read(c)
				Expect(token).To(BeEmpty())
				Expect(err).To(MatchError(http.ErrNoCookie))
				c.Status(http.StatusBadRequest)
			})

			req := httptest.NewRequest(http.MethodGet, "/test", nil)
			req.TLS = &tls.ConnectionState{}
			rec := performSecurityRequest(router, req)

			Expect(rec).To(HaveHTTPStatus(http.StatusBadRequest))
		})

		It("requires HTTPS before reading or clearing secure cookies", func() {
			csrf := NewCSRFCookie(false, time.Hour)
			rec := httptest.NewRecorder()
			ctx, _ := gin.CreateTestContext(rec)
			ctx.Request = httptest.NewRequest(http.MethodGet, "/test", nil)

			token, err := csrf.Read(ctx)
			Expect(err).To(MatchError(utils.ErrHTTPSRequired))
			Expect(token).To(BeEmpty())
			Expect(csrf.Clear(ctx)).To(MatchError(utils.ErrHTTPSRequired))
		})
	})

	Describe("clearing tokens", func() {
		It("expires the secure CSRF cookie", func() {
			csrf := NewCSRFCookie(false, time.Hour)
			router := gin.New()
			router.GET("/test", func(c *gin.Context) {
				Expect(csrf.Clear(c)).To(Succeed())
				c.Status(http.StatusOK)
			})

			req := httptest.NewRequest(http.MethodGet, "/test", nil)
			req.TLS = &tls.ConnectionState{}
			rec := performSecurityRequest(router, req)

			Expect(rec).To(HaveHTTPStatus(http.StatusOK))
			Expect(singleResponseCookie(rec)).To(matchClearedCSRFCookie(csrf.cookieName(true), true))
		})

		It("expires the insecure CSRF cookie when insecure requests are allowed", func() {
			csrf := NewCSRFCookie(true, time.Hour)
			router := gin.New()
			router.GET("/test", func(c *gin.Context) {
				Expect(csrf.Clear(c)).To(Succeed())
				c.Status(http.StatusOK)
			})

			req := httptest.NewRequest(http.MethodGet, "/test", nil)
			rec := performSecurityRequest(router, req)

			Expect(rec).To(HaveHTTPStatus(http.StatusOK))
			Expect(singleResponseCookie(rec)).To(matchClearedCSRFCookie(csrf.cookieName(false), false))
		})
	})
})
