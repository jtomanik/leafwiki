package security

import (
	"crypto/tls"
	"errors"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"net/http"
	"net/http/httptest"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/perber/wiki/internal/http/middleware/utils"
)

var _ = It("TestCSRFCookie_CookieName_Secure", func() {
	t := GinkgoT()
	csrf := NewCSRFCookie(false, time.Hour)

	name := csrf.cookieName(true)
	if name != "__Host-leafwiki_csrf" {
		t.Errorf("Expected secure CSRF cookie name '__Host-leafwiki_csrf', got '%s'", name)
	}

})

var _ = It("TestCSRFCookie_CookieName_Insecure", func() {
	t := GinkgoT()
	csrf := NewCSRFCookie(true, time.Hour)

	name := csrf.cookieName(false)
	if name != "leafwiki_csrf" {
		t.Errorf("Expected insecure CSRF cookie name 'leafwiki_csrf', got '%s'", name)
	}

})

var _ = It("TestCSRFCookie_Issue_Secure", func() {
	t := GinkgoT()
	gin.SetMode(gin.TestMode)
	csrf := NewCSRFCookie(false, time.Hour)

	router := gin.New()
	router.GET("/test", func(c *gin.Context) {
		_, err := csrf.Issue(c)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		c.Status(http.StatusOK)
	})

	req := httptest.NewRequest("GET", "/test", nil)
	req.TLS = &tls.ConnectionState{}
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("Expected status 200, got %d", w.Code)
	}

	// Check that CSRF cookie was set correctly
	cookies := w.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("Expected 1 cookie, got %d", len(cookies))
	}

	csrfCookie := cookies[0]
	if csrfCookie.Name != "__Host-leafwiki_csrf" {
		t.Errorf("Expected CSRF cookie name '__Host-leafwiki_csrf', got '%s'", csrfCookie.Name)
	}
	if csrfCookie.Value == "" {
		t.Error("Expected CSRF cookie to have a non-empty value")
	}
	if csrfCookie.HttpOnly {
		t.Error("Expected CSRF cookie to NOT be HttpOnly")
	}
	if !csrfCookie.Secure {
		t.Error("Expected CSRF cookie to be Secure in secure mode")
	}
	if csrfCookie.Path != "/" {
		t.Errorf("Expected CSRF cookie path '/', got '%s'", csrfCookie.Path)
	}
	if csrfCookie.SameSite != http.SameSiteLaxMode {
		t.Errorf("Expected CSRF cookie SameSite LaxMode, got %v", csrfCookie.SameSite)
	}
	if csrfCookie.MaxAge != int(time.Hour.Seconds()) {
		t.Errorf("Expected CSRF cookie MaxAge %d, got %d", int(time.Hour.Seconds()), csrfCookie.MaxAge)
	}

	// Header should also contain the same CSRF token
	headerToken := w.Result().Header.Get("X-CSRF-Token")
	if headerToken == "" {
		t.Error("Expected X-CSRF-Token header to be set")
	}
	if headerToken != csrfCookie.Value {
		t.Errorf("Expected header token '%s' to match cookie value '%s'", headerToken, csrfCookie.Value)
	}

})

var _ = It("TestCSRFCookie_Issue_Insecure", func() {
	t := GinkgoT()
	gin.SetMode(gin.TestMode)
	csrf := NewCSRFCookie(true, time.Hour)

	router := gin.New()
	router.GET("/test", func(c *gin.Context) {
		_, err := csrf.Issue(c)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		c.Status(http.StatusOK)
	})

	req := httptest.NewRequest("GET", "/test", nil)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("Expected status 200, got %d", w.Code)
	}

	cookies := w.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("Expected 1 cookie, got %d", len(cookies))
	}

	csrfCookie := cookies[0]
	if csrfCookie.Name != "leafwiki_csrf" {
		t.Errorf("Expected CSRF cookie name 'leafwiki_csrf', got '%s'", csrfCookie.Name)
	}
	if csrfCookie.Secure {
		t.Error("Expected CSRF cookie to NOT be Secure in insecure mode")
	}
	if csrfCookie.HttpOnly {
		t.Error("Expected CSRF cookie to NOT be HttpOnly")
	}

})

var _ = It("TestCSRFCookie_Issue_ErrorWhenHTTPSRequired", func() {
	t := GinkgoT()
	gin.SetMode(gin.TestMode)
	csrf := NewCSRFCookie(false, time.Hour)

	router := gin.New()
	router.GET("/test", func(c *gin.Context) {
		_, err := csrf.Issue(c)
		if err != nil {
			if err == utils.ErrHTTPSRequired {
				c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
				return
			}
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.Status(http.StatusOK)
	})

	// No TLS / HTTPS indicators, AllowInsecure=false
	req := httptest.NewRequest("GET", "/test", nil)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected status 400 when HTTPS required for CSRF cookie, got %d", w.Code)
	}

})

var _ = It("returns token generation errors from Issue", func() {
	gin.SetMode(gin.TestMode)
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
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	Expect(rec.Code).To(Equal(http.StatusTeapot))
	Expect(rec.Result().Cookies()).To(BeEmpty())
	Expect(rec.Result().Header.Get("X-CSRF-Token")).To(BeEmpty())
})

var _ = It("TestCSRFCookie_Read_Secure", func() {
	t := GinkgoT()
	gin.SetMode(gin.TestMode)
	csrf := NewCSRFCookie(false, time.Hour)

	router := gin.New()
	router.GET("/test", func(c *gin.Context) {
		token, err := csrf.Read(c)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"token": token})
	})

	req := httptest.NewRequest("GET", "/test", nil)
	req.TLS = &tls.ConnectionState{}
	req.AddCookie(&http.Cookie{
		Name:  "__Host-leafwiki_csrf",
		Value: "test-csrf-token",
	})
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("Expected status 200, got %d", w.Code)
	}

})

var _ = It("TestCSRFCookie_Read_Insecure", func() {
	t := GinkgoT()
	gin.SetMode(gin.TestMode)
	csrf := NewCSRFCookie(true, time.Hour)

	router := gin.New()
	router.GET("/test", func(c *gin.Context) {
		token, err := csrf.Read(c)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"token": token})
	})

	req := httptest.NewRequest("GET", "/test", nil)
	req.AddCookie(&http.Cookie{
		Name:  "leafwiki_csrf",
		Value: "test-csrf-token-insecure",
	})
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("Expected status 200, got %d", w.Code)
	}

})

var _ = It("TestCSRFCookie_Read_MissingCookie", func() {
	t := GinkgoT()
	gin.SetMode(gin.TestMode)
	csrf := NewCSRFCookie(false, time.Hour)

	router := gin.New()
	router.GET("/test", func(c *gin.Context) {
		token, err := csrf.Read(c)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"token": token})
	})

	req := httptest.NewRequest("GET", "/test", nil)
	req.TLS = &tls.ConnectionState{}
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected status 400 when CSRF cookie is missing, got %d", w.Code)
	}

})

var _ = It("TestCSRFCookie_Clear_Secure", func() {
	t := GinkgoT()
	gin.SetMode(gin.TestMode)
	csrf := NewCSRFCookie(false, time.Hour)

	router := gin.New()
	router.GET("/test", func(c *gin.Context) {
		err := csrf.Clear(c)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		c.Status(http.StatusOK)
	})

	req := httptest.NewRequest("GET", "/test", nil)
	req.TLS = &tls.ConnectionState{}
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("Expected status 200, got %d", w.Code)
	}

	cookies := w.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("Expected 1 cookie, got %d", len(cookies))
	}

	csrfCookie := cookies[0]
	if csrfCookie.Name != "__Host-leafwiki_csrf" {
		t.Errorf("Expected CSRF cookie name '__Host-leafwiki_csrf', got '%s'", csrfCookie.Name)
	}
	if csrfCookie.Value != "" {
		t.Errorf("Expected CSRF cookie value to be empty, got '%s'", csrfCookie.Value)
	}
	if csrfCookie.MaxAge != -1 {
		t.Errorf("Expected CSRF cookie MaxAge -1, got %d", csrfCookie.MaxAge)
	}

})

var _ = It("TestCSRFCookie_Clear_Insecure", func() {
	t := GinkgoT()
	gin.SetMode(gin.TestMode)
	csrf := NewCSRFCookie(true, time.Hour)

	router := gin.New()
	router.GET("/test", func(c *gin.Context) {
		err := csrf.Clear(c)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		c.Status(http.StatusOK)
	})

	req := httptest.NewRequest("GET", "/test", nil)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("Expected status 200, got %d", w.Code)
	}

	cookies := w.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("Expected 1 cookie, got %d", len(cookies))
	}

	csrfCookie := cookies[0]
	if csrfCookie.Name != "leafwiki_csrf" {
		t.Errorf("Expected CSRF cookie name 'leafwiki_csrf', got '%s'", csrfCookie.Name)
	}
	if csrfCookie.MaxAge != -1 {
		t.Errorf("Expected CSRF cookie MaxAge -1, got %d", csrfCookie.MaxAge)
	}

})

var _ = Describe("CSRF cookie edge coverage", func() {
	It("reuses an existing secure CSRF cookie when issuing", func() {
		gin.SetMode(gin.TestMode)
		csrf := NewCSRFCookie(false, time.Hour)
		router := gin.New()
		router.GET("/test", func(c *gin.Context) {
			token, err := csrf.Issue(c)
			Expect(err).NotTo(HaveOccurred())
			c.String(http.StatusOK, token)
		})

		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		req.TLS = &tls.ConnectionState{}
		req.AddCookie(&http.Cookie{Name: "__Host-leafwiki_csrf", Value: "existing-token"})
		w := httptest.NewRecorder()

		router.ServeHTTP(w, req)

		Expect(w.Code).To(Equal(http.StatusOK))
		Expect(w.Body.String()).To(Equal("existing-token"))
		Expect(w.Result().Header.Get("X-CSRF-Token")).To(Equal("existing-token"))
		Expect(w.Result().Cookies()).To(BeEmpty())
	})

	It("returns HTTPS-required errors from Read and Clear", func() {
		gin.SetMode(gin.TestMode)
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
