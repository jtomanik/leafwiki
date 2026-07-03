package auth

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
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

var _ = Describe("public editor injection", func() {
	DescribeTable("authentication disabled behavior",
		func(tc injectPublicEditorScenario) {
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

			Expect(w).To(HaveHTTPStatus(http.StatusOK))

			if tc.expectUser {
				expectedBody := `{"user":{"role":"` + tc.expectRole + `","username":"` + tc.expectUsername + `"}}`
				Expect(w.Body.String()).To(MatchJSON(expectedBody))
			} else {
				Expect(w.Body.String()).To(MatchJSON(`{"user":null}`))
			}
		},
		Entry("injects a public editor when auth is disabled and no user exists", injectPublicEditorScenario{
			authDisabled:   true,
			existingUser:   false,
			expectUser:     true,
			expectUsername: "public-editor",
			expectRole:     auth.RoleEditor,
		}),
		Entry("preserves an existing user when auth is disabled", injectPublicEditorScenario{
			authDisabled:   true,
			existingUser:   true,
			expectUser:     true,
			expectUsername: "existing-user",
			expectRole:     auth.RoleAdmin,
		}),
		Entry("leaves user context empty when auth is enabled and no user exists", injectPublicEditorScenario{
			authDisabled: false,
			existingUser: false,
			expectUser:   false,
		}),
		Entry("preserves an existing user when auth is enabled", injectPublicEditorScenario{
			authDisabled:   false,
			existingUser:   true,
			expectUser:     true,
			expectUsername: "existing-user",
			expectRole:     auth.RoleAdmin,
		}),
	)

	It("injects stable public editor identity fields", func() {
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

		Expect(w).To(HaveHTTPStatus(http.StatusOK))
		Expect(w.Body.String()).To(MatchJSON(`{"id":"public-editor","role":"editor","username":"public-editor"}`))

	})

	It("continues the middleware chain after injecting the public editor", func() {
		gin.SetMode(gin.TestMode)

		next := &middlewareFlowProbe{}

		router := gin.New()
		router.Use(InjectPublicEditor(true))
		router.Use(next.Record)

		router.GET("/test", func(c *gin.Context) {
			c.JSON(http.StatusOK, gin.H{"ok": true})
		})

		req := httptest.NewRequest("GET", "/test", nil)
		w := httptest.NewRecorder()

		router.ServeHTTP(w, req)

		Expect(next.reachedPaths).To(ConsistOf("/test"))
	})
})

type middlewareFlowProbe struct {
	reachedPaths []string
}

func (p *middlewareFlowProbe) Record(c *gin.Context) {
	p.reachedPaths = append(p.reachedPaths, c.Request.URL.Path)
	c.Next()
}
