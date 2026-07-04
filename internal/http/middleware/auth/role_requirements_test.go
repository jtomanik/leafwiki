package auth_test

import (
	"net/http"
	"net/http/httptest"

	"github.com/gin-gonic/gin"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	coreauth "github.com/perber/wiki/internal/core/auth"
	sharederrors "github.com/perber/wiki/internal/core/shared/errors"
	authmw "github.com/perber/wiki/internal/http/middleware/auth"
)

type roleRequirementScenario struct {
	user           any
	setUser        bool
	authDisabled   bool
	route          string
	target         string
	expectedStatus int
	expectedCode   sharederrors.ErrorCode
}

func performRoleRequirementRequest(middleware gin.HandlerFunc, scenario roleRequirementScenario) *httptest.ResponseRecorder {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	if scenario.setUser {
		router.Use(func(c *gin.Context) {
			c.Set("user", scenario.user)
			c.Next()
		})
	}
	router.Use(middleware)
	router.GET(scenario.route, func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"ok": true})
	})

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, scenario.target, nil))
	return rec
}

func testUser(id string, role string) *coreauth.User {
	return &coreauth.User{
		ID:       id,
		Username: id,
		Role:     role,
	}
}

var _ = DescribeTable("admin-only route authorization",
	Label("integration"),
	func(scenario roleRequirementScenario) {
		rec := performRoleRequirementRequest(authmw.RequireAdmin(scenario.authDisabled), scenario)
		Expect(rec).To(HaveHTTPStatus(scenario.expectedStatus))
		if scenario.expectedCode != "" {
			Expect(rec).To(matchAuthMiddlewareError(scenario.expectedCode))
		}
	},
	Entry("allows an admin user", roleRequirementScenario{
		user:           testUser("admin", coreauth.RoleAdmin),
		setUser:        true,
		route:          "/admin",
		target:         "/admin",
		expectedStatus: http.StatusOK,
	}),
	Entry("rejects admin operations while auth is disabled", roleRequirementScenario{
		authDisabled:   true,
		route:          "/admin",
		target:         "/admin",
		expectedStatus: http.StatusForbidden,
		expectedCode:   expectedAuthAdminDisabled,
	}),
	Entry("rejects a missing user", roleRequirementScenario{
		route:          "/admin",
		target:         "/admin",
		expectedStatus: http.StatusForbidden,
		expectedCode:   expectedAuthUserNotAuthenticated,
	}),
	Entry("rejects a non-admin user", roleRequirementScenario{
		user:           testUser("viewer", coreauth.RoleViewer),
		setUser:        true,
		route:          "/admin",
		target:         "/admin",
		expectedStatus: http.StatusForbidden,
		expectedCode:   expectedAuthAdminPrivilegesRequired,
	}),
	Entry("rejects an invalid user context", roleRequirementScenario{
		user:           "not-a-user",
		setUser:        true,
		route:          "/admin",
		target:         "/admin",
		expectedStatus: http.StatusForbidden,
		expectedCode:   expectedAuthAdminPrivilegesRequired,
	}),
)

var _ = DescribeTable("self-or-admin route authorization",
	Label("integration"),
	func(scenario roleRequirementScenario) {
		rec := performRoleRequirementRequest(authmw.RequireSelfOrAdmin(scenario.authDisabled), scenario)
		Expect(rec).To(HaveHTTPStatus(scenario.expectedStatus))
		if scenario.expectedCode != "" {
			Expect(rec).To(matchAuthMiddlewareError(scenario.expectedCode))
		}
	},
	Entry("allows a user to access themself", roleRequirementScenario{
		user:           testUser("alice", coreauth.RoleViewer),
		setUser:        true,
		route:          "/users/:id",
		target:         "/users/alice",
		expectedStatus: http.StatusOK,
	}),
	Entry("allows an admin to access another user", roleRequirementScenario{
		user:           testUser("admin", coreauth.RoleAdmin),
		setUser:        true,
		route:          "/users/:id",
		target:         "/users/alice",
		expectedStatus: http.StatusOK,
	}),
	Entry("rejects another non-admin user", roleRequirementScenario{
		user:           testUser("bob", coreauth.RoleViewer),
		setUser:        true,
		route:          "/users/:id",
		target:         "/users/alice",
		expectedStatus: http.StatusForbidden,
		expectedCode:   expectedAuthAdminPrivilegesRequired,
	}),
	Entry("rejects user management while auth is disabled", roleRequirementScenario{
		authDisabled:   true,
		route:          "/users/:id",
		target:         "/users/alice",
		expectedStatus: http.StatusForbidden,
		expectedCode:   expectedAuthAdminDisabled,
	}),
	Entry("rejects a missing user", roleRequirementScenario{
		route:          "/users/:id",
		target:         "/users/alice",
		expectedStatus: http.StatusForbidden,
		expectedCode:   expectedAuthUserNotAuthenticated,
	}),
	Entry("rejects an invalid user context", roleRequirementScenario{
		user:           "not-a-user",
		setUser:        true,
		route:          "/users/:id",
		target:         "/users/alice",
		expectedStatus: http.StatusForbidden,
		expectedCode:   expectedAuthInvalidUser,
	}),
)

var _ = DescribeTable("editor-or-admin route authorization",
	Label("integration"),
	func(scenario roleRequirementScenario) {
		rec := performRoleRequirementRequest(authmw.RequireEditorOrAdmin(), scenario)
		Expect(rec).To(HaveHTTPStatus(scenario.expectedStatus))
		if scenario.expectedCode != "" {
			Expect(rec).To(matchAuthMiddlewareError(scenario.expectedCode))
		}
	},
	Entry("allows an admin user", roleRequirementScenario{
		user:           testUser("admin", coreauth.RoleAdmin),
		setUser:        true,
		route:          "/edit",
		target:         "/edit",
		expectedStatus: http.StatusOK,
	}),
	Entry("allows an editor user", roleRequirementScenario{
		user:           testUser("editor", coreauth.RoleEditor),
		setUser:        true,
		route:          "/edit",
		target:         "/edit",
		expectedStatus: http.StatusOK,
	}),
	Entry("rejects a viewer user", roleRequirementScenario{
		user:           testUser("viewer", coreauth.RoleViewer),
		setUser:        true,
		route:          "/edit",
		target:         "/edit",
		expectedStatus: http.StatusForbidden,
		expectedCode:   expectedAuthEditorPrivilegesRequired,
	}),
	Entry("rejects a missing user", roleRequirementScenario{
		route:          "/edit",
		target:         "/edit",
		expectedStatus: http.StatusForbidden,
		expectedCode:   expectedAuthUserNotAuthenticated,
	}),
	Entry("rejects an invalid user context", roleRequirementScenario{
		user:           "not-a-user",
		setUser:        true,
		route:          "/edit",
		target:         "/edit",
		expectedStatus: http.StatusForbidden,
		expectedCode:   expectedAuthUserNotAuthenticated,
	}),
)

var _ = DescribeTable("self-only route authorization",
	Label("integration"),
	func(scenario roleRequirementScenario) {
		rec := performRoleRequirementRequest(authmw.RequireSelf(), scenario)
		Expect(rec).To(HaveHTTPStatus(scenario.expectedStatus))
		if scenario.expectedCode != "" {
			Expect(rec).To(matchAuthMiddlewareError(scenario.expectedCode))
		}
	},
	Entry("allows a user to access themself", roleRequirementScenario{
		user:           testUser("alice", coreauth.RoleViewer),
		setUser:        true,
		route:          "/users/:id",
		target:         "/users/alice",
		expectedStatus: http.StatusOK,
	}),
	Entry("rejects a different user", roleRequirementScenario{
		user:           testUser("bob", coreauth.RoleViewer),
		setUser:        true,
		route:          "/users/:id",
		target:         "/users/alice",
		expectedStatus: http.StatusForbidden,
		expectedCode:   expectedAuthSelfRequired,
	}),
	Entry("rejects a missing user", roleRequirementScenario{
		route:          "/users/:id",
		target:         "/users/alice",
		expectedStatus: http.StatusForbidden,
		expectedCode:   expectedAuthUserNotAuthenticated,
	}),
	Entry("rejects an invalid user context", roleRequirementScenario{
		user:           "not-a-user",
		setUser:        true,
		route:          "/users/:id",
		target:         "/users/alice",
		expectedStatus: http.StatusForbidden,
		expectedCode:   expectedAuthSelfRequired,
	}),
)

var _ = DescribeTable("optional user context lookup",
	Label("unit"),
	func(userValue any, setUser bool, expected *coreauth.User) {
		gin.SetMode(gin.TestMode)
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		if setUser {
			c.Set("user", userValue)
		}
		Expect(authmw.TryGetUser(c)).To(Equal(expected))
	},
	Entry("returns nil when user is missing", nil, false, nil),
	Entry("returns nil when user has the wrong type", "not-a-user", true, nil),
	Entry("returns the context user when valid", testUser("alice", coreauth.RoleEditor), true, testUser("alice", coreauth.RoleEditor)),
)

var _ = DescribeTable("remote-user auth source detection",
	Label("unit"),
	func(source any, setSource bool, expected bool) {
		gin.SetMode(gin.TestMode)
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		if setSource {
			c.Set(authmw.ContextAuthSource, source)
		}
		Expect(authmw.IsRemoteUser(c)).To(Equal(expected))
	},
	Entry("recognizes contexts populated by reverse-proxy authentication", authmw.AuthSourceRemoteUser, true, true),
	Entry("ignores session-authenticated contexts", "session", true, false),
	Entry("ignores contexts without an auth source", nil, false, false),
	Entry("ignores auth source values with the wrong type", 42, true, false),
)
