package auth

import (
	. "github.com/onsi/ginkgo/v2"
	"net/http"
	"net/http/httptest"

	"github.com/gin-gonic/gin"
	"github.com/perber/wiki/internal/core/auth"
)

type injectPublicEditorScenario struct {
	authDisabled   bool
	existingUser   bool
	expectUser     bool
	expectUsername string
	expectRole     string
}

var _ = DescribeTable("TestInjectPublicEditor_AuthDisabled",
	func(tc injectPublicEditorScenario) {
	t := GinkgoT()
	gin.SetMode(gin.TestMode)

	router := gin.New()

	// Set up existing user if needed
	if tc.existingUser {
		router.Use(func(c *gin.Context) {
			c.Set("user", &auth.User{
				ID:       "existing-user-id",
				Username: "existing-user",
				Role:     auth.RoleAdmin,
			})
			c.Next()
		})
	}

	// Add the middleware under test
	router.Use(InjectPublicEditor(tc.authDisabled))

	// Test endpoint
	router.GET("/test", func(c *gin.Context) {
		userValue, exists := c.Get("user")
		if !exists {
			c.JSON(http.StatusOK, gin.H{"user": nil})
			return
		}

		user, ok := userValue.(*auth.User)
		if !ok {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "invalid user type"})
			return
		}

		c.JSON(http.StatusOK, gin.H{
			"user": gin.H{
				"username": user.Username,
				"role":     user.Role,
			},
		})
	})

	req := httptest.NewRequest("GET", "/test", nil)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", w.Code)
	}

	// Verify response body
	if tc.expectUser {
		expectedBody := `{"user":{"role":"` + tc.expectRole + `","username":"` + tc.expectUsername + `"}}`
		if w.Body.String() != expectedBody {
			t.Errorf("Expected body %s, got %s", expectedBody, w.Body.String())
		}
	} else {
		expectedBody := `{"user":null}`
		if w.Body.String() != expectedBody {
			t.Errorf("Expected body %s, got %s", expectedBody, w.Body.String())
		}
	}
},
	Entry("auth disabled with no existing user - should inject public editor", injectPublicEditorScenario{
		authDisabled:   true,
		existingUser:   false,
		expectUser:     true,
		expectUsername: "public-editor",
		expectRole:     auth.RoleEditor,
	}),
	Entry("auth disabled with existing user - should not override", injectPublicEditorScenario{
		authDisabled:   true,
		existingUser:   true,
		expectUser:     true,
		expectUsername: "existing-user",
		expectRole:     auth.RoleAdmin,
	}),
	Entry("auth enabled with no existing user - should not inject", injectPublicEditorScenario{
		authDisabled: false,
		existingUser: false,
		expectUser:   false,
	}),
	Entry("auth enabled with existing user - should not change", injectPublicEditorScenario{
		authDisabled:   false,
		existingUser:   true,
		expectUser:     true,
		expectUsername: "existing-user",
		expectRole:     auth.RoleAdmin,
	}),
)

var _ = It("TestInjectPublicEditor_PublicEditorProperties", func() {
	t := GinkgoT()
	gin.SetMode(gin.TestMode)

	router := gin.New()
	router.Use(InjectPublicEditor(true))

	router.GET("/test", func(c *gin.Context) {
		userValue, exists := c.Get("user")
		if !exists {
			c.JSON(http.StatusNotFound, gin.H{"error": "no user found"})
			return
		}

		user, ok := userValue.(*auth.User)
		if !ok {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "invalid user type"})
			return
		}

		c.JSON(http.StatusOK, gin.H{
			"id":       user.ID,
			"username": user.Username,
			"role":     user.Role,
		})
	})

	req := httptest.NewRequest("GET", "/test", nil)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("Expected status 200, got %d", w.Code)
	}

	expectedBody := `{"id":"public-editor","role":"editor","username":"public-editor"}`
	if w.Body.String() != expectedBody {
		t.Errorf("Expected body %s, got %s", expectedBody, w.Body.String())
	}

})

var _ = It("TestInjectPublicEditor_NextCalled", func() {
	t := GinkgoT()
	gin.SetMode(gin.TestMode)

	nextCalled := false

	router := gin.New()
	router.Use(InjectPublicEditor(true))
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

	if !nextCalled {
		t.Error("Expected Next() to be called")
	}

})
