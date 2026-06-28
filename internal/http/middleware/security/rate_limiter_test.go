package security

import (
	"encoding/json"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"net/http"
	"net/http/httptest"
	"time"

	"github.com/gin-gonic/gin"
	sharederrors "github.com/perber/wiki/internal/core/shared/errors"
)

var _ = It("TestRateLimiter_NewKey", func() {
	t := GinkgoT()
	// This test ensures that the rate limiter doesn't panic when encountering a new key
	gin.SetMode(gin.TestMode)

	limiter := NewRateLimiter(3, time.Minute, false)

	// Create a test router with the rate limiter
	router := gin.New()
	router.Use(limiter)
	router.GET("/test", func(c *gin.Context) {
		c.Status(http.StatusOK)
	})

	// Make a request with a new IP (this would panic with the old code)
	req := httptest.NewRequest("GET", "/test", nil)
	req.RemoteAddr = "192.168.1.1:1234"
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", w.Code)
	}

})

var _ = It("TestRateLimiter_ExceedsLimit", func() {
	t := GinkgoT()
	gin.SetMode(gin.TestMode)

	limiter := NewRateLimiter(3, time.Minute, false)

	router := gin.New()
	router.Use(limiter)
	router.GET("/test", func(c *gin.Context) {
		c.Status(http.StatusOK)
	})

	// Make requests up to the limit
	for i := 0; i < 3; i++ {
		req := httptest.NewRequest("GET", "/test", nil)
		req.RemoteAddr = "192.168.1.2:1234"
		w := httptest.NewRecorder()

		router.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("Request %d: Expected status 200, got %d", i+1, w.Code)
		}
	}

	// The next request should be rate limited
	req := httptest.NewRequest("GET", "/test", nil)
	req.RemoteAddr = "192.168.1.2:1234"
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	if w.Code != http.StatusTooManyRequests {
		t.Errorf("Expected status 429, got %d", w.Code)
	}

})

var _ = It("TestRateLimiter_ExceedsLimitReturnsStructuredLocalizedError", func() {
	t := GinkgoT()
	gin.SetMode(gin.TestMode)

	limiter := NewRateLimiter(1, time.Minute, false)

	router := gin.New()
	router.Use(limiter)
	router.GET("/test", func(c *gin.Context) {
		c.Status(http.StatusOK)
	})

	req := httptest.NewRequest("GET", "/test", nil)
	req.RemoteAddr = "192.168.1.6:1234"
	router.ServeHTTP(httptest.NewRecorder(), req)

	req = httptest.NewRequest("GET", "/test", nil)
	req.RemoteAddr = "192.168.1.6:1234"
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusTooManyRequests, w.Body.String())
	}
	var body struct {
		Error sharederrors.LocalizedErrorDetail `json:"error"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v; body=%s", err, w.Body.String())
	}
	if body.Error.Code != ErrCodeRateLimitExceeded {
		t.Fatalf("error.code = %q, want %q; body=%s", body.Error.Code, ErrCodeRateLimitExceeded, w.Body.String())
	}
	if body.Error.MessageID != "errors.rate.limit_exceeded" {
		t.Fatalf("error.messageId = %q, want errors.rate.limit_exceeded", body.Error.MessageID)
	}
	if body.Error.Message != "Too many requests, please try again later" {
		t.Fatalf("error.message = %q, want catalog-rendered rate-limit message", body.Error.Message)
	}

})

var _ = It("TestRateLimiter_ReleasesLockAfterLimit", func() {
	t := GinkgoT()
	gin.SetMode(gin.TestMode)

	limiter := NewRateLimiter(1, time.Minute, false)

	router := gin.New()
	router.Use(limiter)
	router.GET("/test", func(c *gin.Context) {
		c.Status(http.StatusOK)
	})

	req := httptest.NewRequest("GET", "/test", nil)
	req.RemoteAddr = "192.168.1.4:1234"
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("Expected status 200, got %d", w.Code)
	}

	req = httptest.NewRequest("GET", "/test", nil)
	req.RemoteAddr = "192.168.1.4:1234"
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("Expected status 429, got %d", w.Code)
	}

	done := make(chan int, 1)

	go func() {
		req := httptest.NewRequest("GET", "/test", nil)
		req.RemoteAddr = "192.168.1.5:1234"
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		done <- w.Code
	}()

	timeout := 2 * time.Second
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case code := <-done:
		if code != http.StatusOK {
			t.Fatalf("Expected status 200 for different key after limit hit, got %d", code)
		}
	case <-timer.C:
		t.Fatal("Request blocked after limit hit; mutex was not released")
	}

})

var _ = It("TestRateLimiter_WindowExpires", func() {
	t := GinkgoT()
	gin.SetMode(gin.TestMode)

	// Use a very short window for testing
	limiter := NewRateLimiter(2, 100*time.Millisecond, false)

	router := gin.New()
	router.Use(limiter)
	router.GET("/test", func(c *gin.Context) {
		c.Status(http.StatusOK)
	})

	// Make requests up to the limit
	for i := 0; i < 2; i++ {
		req := httptest.NewRequest("GET", "/test", nil)
		req.RemoteAddr = "192.168.1.3:1234"
		w := httptest.NewRecorder()

		router.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("Request %d: Expected status 200, got %d", i+1, w.Code)
		}
	}

	// Wait for the window to expire
	time.Sleep(150 * time.Millisecond)

	// Should be able to make another request
	req := httptest.NewRequest("GET", "/test", nil)
	req.RemoteAddr = "192.168.1.3:1234"
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200 after window expired, got %d", w.Code)
	}

})

var _ = Describe("rate limiter edge coverage", func() {
	It("uses the raw remote address when no port is present", func() {
		gin.SetMode(gin.TestMode)
		limiter := NewRateLimiter(1, time.Minute, false)
		router := gin.New()
		router.Use(limiter)
		router.GET("/test", func(c *gin.Context) {
			c.Status(http.StatusOK)
		})

		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		req.RemoteAddr = "192.0.2.10"
		router.ServeHTTP(httptest.NewRecorder(), req)

		req = httptest.NewRequest(http.MethodGet, "/test", nil)
		req.RemoteAddr = "192.0.2.10"
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		Expect(w.Code).To(Equal(http.StatusTooManyRequests))
	})

	It("resets the request count after successful responses when configured", func() {
		gin.SetMode(gin.TestMode)
		limiter := NewRateLimiter(1, time.Minute, true)
		router := gin.New()
		router.Use(limiter)
		router.GET("/test", func(c *gin.Context) {
			c.Status(http.StatusOK)
		})

		for i := 0; i < 2; i++ {
			req := httptest.NewRequest(http.MethodGet, "/test", nil)
			req.RemoteAddr = "192.0.2.11:1234"
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)
			Expect(w.Code).To(Equal(http.StatusOK))
		}
	})
})
