package auth

import (
	"crypto/tls"
	"net/http"
	"net/http/httptest"
	"time"

	"github.com/gin-gonic/gin"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/perber/wiki/internal/http/middleware/utils"
)

var _ = Describe("secure request detection", Label("unit"), func() {
	It("detects secure requests from TLS", func() {
		rec := performSecureDetectionRequest(false, func(req *http.Request) {
			req.TLS = &tls.ConnectionState{}
		})

		Expect(rec).To(HaveHTTPStatus(http.StatusOK))
	})

	DescribeTable("detects secure requests from X-Forwarded-Proto",
		func(value string, expectedStatus int) {
			rec := performSecureDetectionRequest(false, func(req *http.Request) {
				req.Header.Set("X-Forwarded-Proto", value)
			})

			Expect(rec).To(HaveHTTPStatus(expectedStatus))
		},
		Entry("accepts lowercase HTTPS", "https", http.StatusOK),
		Entry("accepts uppercase HTTPS", "HTTPS", http.StatusOK),
		Entry("accepts HTTPS scheme values", "https://example.com", http.StatusOK),
		Entry("rejects HTTP", "http", http.StatusBadRequest),
	)

	DescribeTable("detects secure requests from X-Forwarded-Ssl",
		func(value string, expectedStatus int) {
			rec := performSecureDetectionRequest(false, func(req *http.Request) {
				req.Header.Set("X-Forwarded-Ssl", value)
			})

			Expect(rec).To(HaveHTTPStatus(expectedStatus))
		},
		Entry("accepts lowercase on", "on", http.StatusOK),
		Entry("accepts uppercase on", "ON", http.StatusOK),
		Entry("accepts mixed-case on", "On", http.StatusOK),
		Entry("rejects off", "off", http.StatusBadRequest),
		Entry("rejects empty values", "", http.StatusBadRequest),
	)

	DescribeTable("detects secure requests from Front-End-Https",
		func(value string, expectedStatus int) {
			rec := performSecureDetectionRequest(false, func(req *http.Request) {
				req.Header.Set("Front-End-Https", value)
			})

			Expect(rec).To(HaveHTTPStatus(expectedStatus))
		},
		Entry("accepts lowercase on", "on", http.StatusOK),
		Entry("accepts uppercase on", "ON", http.StatusOK),
		Entry("accepts mixed-case on", "On", http.StatusOK),
		Entry("rejects off", "off", http.StatusBadRequest),
		Entry("rejects empty values", "", http.StatusBadRequest),
	)

	It("allows insecure requests when configured", func() {
		rec := performSecureDetectionRequest(true, nil)

		Expect(rec).To(HaveHTTPStatus(http.StatusOK))
	})

	It("rejects insecure requests when HTTPS is required", func() {
		rec := performSecureDetectionRequest(false, nil)

		Expect(rec).To(HaveHTTPStatus(http.StatusBadRequest))
	})
})

var _ = Describe("auth cookie lifecycle", Label("unit"), func() {
	It("uses host-prefixed cookie names for secure responses", func() {
		auth := NewAuthCookies(false, time.Hour, 24*time.Hour)

		accessName, refreshName := auth.cookieNames(true)

		Expect(accessName).To(Equal("__Host-leafwiki_at"))
		Expect(refreshName).To(Equal("__Host-leafwiki_rt"))
	})

	It("uses regular cookie names for insecure responses", func() {
		auth := NewAuthCookies(true, time.Hour, 24*time.Hour)

		accessName, refreshName := auth.cookieNames(false)

		Expect(accessName).To(Equal("leafwiki_at"))
		Expect(refreshName).To(Equal("leafwiki_rt"))
	})

	It("sets secure access and refresh cookies with host-prefixed names", func() {
		rec := performAuthCookieSet(NewAuthCookies(false, time.Hour, 24*time.Hour), true, "access-token-123", "refresh-token-456")

		Expect(rec).To(HaveHTTPStatus(http.StatusOK))
		Expect(rec.Result().Cookies()).To(HaveExactElements(
			matchAuthCookie("__Host-leafwiki_at", "access-token-123", true, int(time.Hour.Seconds())),
			matchAuthCookie("__Host-leafwiki_rt", "refresh-token-456", true, int((24*time.Hour).Seconds())),
		))
	})

	It("sets insecure access and refresh cookies without host prefixes", func() {
		rec := performAuthCookieSet(NewAuthCookies(true, time.Hour, 24*time.Hour), false, "access-token-789", "refresh-token-012")

		Expect(rec).To(HaveHTTPStatus(http.StatusOK))
		Expect(rec.Result().Cookies()).To(HaveExactElements(
			matchAuthCookie("leafwiki_at", "access-token-789", false, int(time.Hour.Seconds())),
			matchAuthCookie("leafwiki_rt", "refresh-token-012", false, int((24*time.Hour).Seconds())),
		))
	})

	It("rejects setting secure cookies on insecure requests", func() {
		rec := performAuthCookieSet(NewAuthCookies(false, time.Hour, 24*time.Hour), false, "access-token", "refresh-token")

		Expect(rec).To(HaveHTTPStatus(http.StatusBadRequest))
	})

	It("expires secure access and refresh cookies with host-prefixed names", func() {
		rec := performAuthCookieClear(NewAuthCookies(false, time.Hour, 24*time.Hour), true)

		Expect(rec).To(HaveHTTPStatus(http.StatusOK))
		Expect(rec.Result().Cookies()).To(HaveExactElements(
			matchExpiredAuthCookie("__Host-leafwiki_at"),
			matchExpiredAuthCookie("__Host-leafwiki_rt"),
		))
	})

	It("expires insecure access and refresh cookies without host prefixes", func() {
		rec := performAuthCookieClear(NewAuthCookies(true, time.Hour, 24*time.Hour), false)

		Expect(rec).To(HaveHTTPStatus(http.StatusOK))
		Expect(rec.Result().Cookies()).To(HaveExactElements(
			matchExpiredAuthCookie("leafwiki_at"),
			matchExpiredAuthCookie("leafwiki_rt"),
		))
	})

	It("reads secure access cookies from host-prefixed names", func() {
		rec := performAuthCookieReadAccess(NewAuthCookies(false, time.Hour, 24*time.Hour), true, "__Host-leafwiki_at", "test-access-token")

		Expect(rec).To(HaveHTTPStatus(http.StatusOK))
	})

	It("reads insecure access cookies from regular names", func() {
		rec := performAuthCookieReadAccess(NewAuthCookies(true, time.Hour, 24*time.Hour), false, "leafwiki_at", "test-access-token-insecure")

		Expect(rec).To(HaveHTTPStatus(http.StatusOK))
	})

	It("rejects missing access cookies", func() {
		rec := performAuthCookieReadAccess(NewAuthCookies(false, time.Hour, 24*time.Hour), true, "", "")

		Expect(rec).To(HaveHTTPStatus(http.StatusBadRequest))
	})

	It("reads secure refresh cookies from host-prefixed names", func() {
		rec := performAuthCookieReadRefresh(NewAuthCookies(false, time.Hour, 24*time.Hour), true, "__Host-leafwiki_rt", "test-refresh-token")

		Expect(rec).To(HaveHTTPStatus(http.StatusOK))
	})

	It("reads insecure refresh cookies from regular names", func() {
		rec := performAuthCookieReadRefresh(NewAuthCookies(true, time.Hour, 24*time.Hour), false, "leafwiki_rt", "test-refresh-token-insecure")

		Expect(rec).To(HaveHTTPStatus(http.StatusOK))
	})

	It("rejects missing refresh cookies", func() {
		rec := performAuthCookieReadRefresh(NewAuthCookies(false, time.Hour, 24*time.Hour), true, "", "")

		Expect(rec).To(HaveHTTPStatus(http.StatusBadRequest))
	})

	It("uses the configured access and refresh token lifetimes", func() {
		accessTTL := 30 * time.Minute
		refreshTTL := 7 * 24 * time.Hour
		rec := performAuthCookieSet(NewAuthCookies(false, accessTTL, refreshTTL), true, "access-token", "refresh-token")

		Expect(rec).To(HaveHTTPStatus(http.StatusOK))
		Expect(rec.Result().Cookies()).To(HaveExactElements(
			matchAuthCookie("__Host-leafwiki_at", "access-token", true, int(accessTTL.Seconds())),
			matchAuthCookie("__Host-leafwiki_rt", "refresh-token", true, int(refreshTTL.Seconds())),
		))
	})

	It("returns HTTPS-required errors from Clear and readers", func() {
		gin.SetMode(gin.TestMode)
		cookies := NewAuthCookies(false, time.Hour, time.Hour)
		rec := httptest.NewRecorder()
		ctx, _ := gin.CreateTestContext(rec)
		ctx.Request = httptest.NewRequest(http.MethodGet, "/test", nil)

		Expect(cookies.Clear(ctx)).To(MatchError(utils.ErrHTTPSRequired))

		access, err := cookies.ReadAccess(ctx)
		Expect(err).To(MatchError(utils.ErrHTTPSRequired))
		Expect(access).To(BeEmpty())

		refresh, err := cookies.ReadRefresh(ctx)
		Expect(err).To(MatchError(utils.ErrHTTPSRequired))
		Expect(refresh).To(BeEmpty())
	})
})

func performSecureDetectionRequest(allowInsecure bool, configure func(*http.Request)) *httptest.ResponseRecorder {
	GinkgoHelper()
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.GET("/test", func(c *gin.Context) {
		secure, err := utils.RequireSecure(c, allowInsecure)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"secure": secure})
	})

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	if configure != nil {
		configure(req)
	}
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec
}

func performAuthCookieSet(auth *AuthCookies, secure bool, accessToken string, refreshToken string) *httptest.ResponseRecorder {
	GinkgoHelper()
	return performAuthCookieRequest(secure, func(c *gin.Context) {
		err := auth.Set(c, accessToken, refreshToken)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		c.Status(http.StatusOK)
	})
}

func performAuthCookieClear(auth *AuthCookies, secure bool) *httptest.ResponseRecorder {
	GinkgoHelper()
	return performAuthCookieRequest(secure, func(c *gin.Context) {
		err := auth.Clear(c)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		c.Status(http.StatusOK)
	})
}

func performAuthCookieReadAccess(auth *AuthCookies, secure bool, name string, value string) *httptest.ResponseRecorder {
	GinkgoHelper()
	return performAuthCookieRead(secure, name, value, auth.ReadAccess)
}

func performAuthCookieReadRefresh(auth *AuthCookies, secure bool, name string, value string) *httptest.ResponseRecorder {
	GinkgoHelper()
	return performAuthCookieRead(secure, name, value, auth.ReadRefresh)
}

func performAuthCookieRead(secure bool, name string, value string, read func(*gin.Context) (string, error)) *httptest.ResponseRecorder {
	GinkgoHelper()
	return performAuthCookieRequestWithCookie(secure, name, value, func(c *gin.Context) {
		token, err := read(c)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"token": token})
	})
}

func performAuthCookieRequest(secure bool, handler gin.HandlerFunc) *httptest.ResponseRecorder {
	GinkgoHelper()
	return performAuthCookieRequestWithCookie(secure, "", "", handler)
}

func performAuthCookieRequestWithCookie(secure bool, name string, value string, handler gin.HandlerFunc) *httptest.ResponseRecorder {
	GinkgoHelper()
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.GET("/test", handler)

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	if secure {
		req.TLS = &tls.ConnectionState{}
	}
	if name != "" {
		req.AddCookie(&http.Cookie{Name: name, Value: value})
	}
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec
}

type authCookieContract struct {
	Name     string
	Value    string
	HttpOnly bool
	Secure   bool
	Path     string
	SameSite http.SameSite
	MaxAge   int
}

func matchAuthCookie(name string, value string, secure bool, maxAge int) OmegaMatcher {
	GinkgoHelper()
	return WithTransform(func(cookie *http.Cookie) authCookieContract {
		return authCookieContract{
			Name:     cookie.Name,
			Value:    cookie.Value,
			HttpOnly: cookie.HttpOnly,
			Secure:   cookie.Secure,
			Path:     cookie.Path,
			SameSite: cookie.SameSite,
			MaxAge:   cookie.MaxAge,
		}
	}, Equal(authCookieContract{
		Name:     name,
		Value:    value,
		HttpOnly: true,
		Secure:   secure,
		Path:     "/",
		SameSite: http.SameSiteLaxMode,
		MaxAge:   maxAge,
	}))
}

func matchExpiredAuthCookie(name string) OmegaMatcher {
	GinkgoHelper()
	return SatisfyAll(
		HaveField("Name", Equal(name)),
		HaveField("Value", BeEmpty()),
		HaveField("MaxAge", Equal(-1)),
	)
}
