package security

import (
	"net/http"
	"net/http/httptest"
	"time"

	"github.com/gin-gonic/gin"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func newRateLimiterRouter(limit int, window time.Duration, resetOnSuccess bool) *gin.Engine {
	GinkgoHelper()

	router := gin.New()
	router.Use(NewRateLimiter(limit, window, resetOnSuccess))
	router.GET("/test", func(c *gin.Context) {
		c.Status(http.StatusOK)
	})
	return router
}

func rateLimitedRequest(router *gin.Engine, remoteAddr string) *httptest.ResponseRecorder {
	GinkgoHelper()

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.RemoteAddr = remoteAddr
	return performSecurityRequest(router, req)
}

var _ = Describe("rate limiter", func() {
	BeforeEach(func() {
		gin.SetMode(gin.TestMode)
	})

	When("a client key has not used its quota", func() {
		It("allows the first request", func() {
			router := newRateLimiterRouter(3, time.Minute, false)

			Expect(rateLimitedRequest(router, "192.168.1.1:1234")).To(HaveHTTPStatus(http.StatusOK))
		})
	})

	When("a client key exceeds its quota", func() {
		It("returns too many requests", func() {
			router := newRateLimiterRouter(3, time.Minute, false)

			for range 3 {
				Expect(rateLimitedRequest(router, "192.168.1.2:1234")).To(HaveHTTPStatus(http.StatusOK))
			}

			Expect(rateLimitedRequest(router, "192.168.1.2:1234")).To(HaveHTTPStatus(http.StatusTooManyRequests))
		})

		It("returns a structured localized rate-limit error", func() {
			router := newRateLimiterRouter(1, time.Minute, false)

			Expect(rateLimitedRequest(router, "192.168.1.6:1234")).To(HaveHTTPStatus(http.StatusOK))
			rec := rateLimitedRequest(router, "192.168.1.6:1234")

			Expect(rec).To(HaveHTTPStatus(http.StatusTooManyRequests))
			Expect(rec.Body.Bytes()).To(haveStructuredSecurityError(ErrCodeRateLimitExceeded))
		})

		It("continues serving other client keys after a limit hit", func() {
			router := newRateLimiterRouter(1, time.Minute, false)

			Expect(rateLimitedRequest(router, "192.168.1.4:1234")).To(HaveHTTPStatus(http.StatusOK))
			Expect(rateLimitedRequest(router, "192.168.1.4:1234")).To(HaveHTTPStatus(http.StatusTooManyRequests))

			done := make(chan *httptest.ResponseRecorder, 1)
			go func() {
				done <- rateLimitedRequest(router, "192.168.1.5:1234")
			}()

			Eventually(done).
				WithTimeout(2*time.Second).
				Should(Receive(HaveHTTPStatus(http.StatusOK)), "request for a different key should not block after a limit hit")
		})
	})

	When("the rate-limit window expires", func() {
		It("allows the client again", func() {
			router := newRateLimiterRouter(2, 100*time.Millisecond, false)

			for range 2 {
				Expect(rateLimitedRequest(router, "192.168.1.3:1234")).To(HaveHTTPStatus(http.StatusOK))
			}

			Eventually(func() *httptest.ResponseRecorder {
				return rateLimitedRequest(router, "192.168.1.3:1234")
			}).
				WithTimeout(500*time.Millisecond).
				WithPolling(10*time.Millisecond).
				Should(HaveHTTPStatus(http.StatusOK), "request should succeed after the rate limit window expires")
		})
	})

	When("client keys come from non-host-port remote addresses", func() {
		It("uses the raw remote address as the rate-limit key", func() {
			router := newRateLimiterRouter(1, time.Minute, false)

			Expect(rateLimitedRequest(router, "192.0.2.10")).To(HaveHTTPStatus(http.StatusOK))
			Expect(rateLimitedRequest(router, "192.0.2.10")).To(HaveHTTPStatus(http.StatusTooManyRequests))
		})
	})

	When("successful responses reset rate-limit state", func() {
		It("allows consecutive successful requests for the same client key", func() {
			router := newRateLimiterRouter(1, time.Minute, true)

			for range 2 {
				Expect(rateLimitedRequest(router, "192.0.2.11:1234")).To(HaveHTTPStatus(http.StatusOK))
			}
		})
	})
})
