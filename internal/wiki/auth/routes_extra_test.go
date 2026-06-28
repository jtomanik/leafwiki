package auth

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	coreauth "github.com/perber/wiki/internal/core/auth"
	sharederrors "github.com/perber/wiki/internal/core/shared/errors"
	httpinternal "github.com/perber/wiki/internal/http"
	authmw "github.com/perber/wiki/internal/http/middleware/auth"
	"github.com/perber/wiki/internal/http/middleware/security"
	mwutils "github.com/perber/wiki/internal/http/middleware/utils"
)

var _ = ginkgo.Describe("auth routes", func() {
	ginkgo.It("RegisterRoutes wires auth endpoints for both refresh-token limiter modes", func() {
		gin.SetMode(gin.TestMode)
		original := DisableRefreshTokenRateLimit
		ginkgo.DeferCleanup(func() {
			DisableRefreshTokenRateLimit = original
		})

		DisableRefreshTokenRateLimit = "false"
		router := httpinternal.NewRouter(
			[]httpinternal.RouteRegistrar{NewRoutes(RoutesConfig{})},
			httpinternal.FrontendConfig{},
			httpinternal.RouterOptions{DisableFrontendRoutes: true},
		)
		Expect(router).NotTo(BeNil())

		DisableRefreshTokenRateLimit = "true"
		router = httpinternal.NewRouter(
			[]httpinternal.RouteRegistrar{NewRoutes(RoutesConfig{})},
			httpinternal.FrontendConfig{},
			httpinternal.RouterOptions{AuthDisabled: true, DisableFrontendRoutes: true},
		)
		Expect(router).NotTo(BeNil())
	})

	ginkgo.It("requireAuthEnabled allows enabled auth and rejects disabled auth", func() {
		rec := performAuthHandlerRequest(requireAuthEnabled(true), http.MethodGet, "/api/users/me/mcp-api-keys", nil, nil, nil, false)
		expectAuthRouteError(rec, http.StatusForbidden, ErrCodeAuthDisabled)

		rec = performAuthHandlerRequest(requireAuthEnabled(false), http.MethodGet, "/api/users/me/mcp-api-keys", nil, nil, nil, false)
		Expect(rec.Code).To(Equal(http.StatusOK), rec.Body.String())
	})

	ginkgo.It("writeAuthCookieError maps HTTPS and unexpected cookie failures", func() {
		rec := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(rec)
		writeAuthCookieError(c, mwutils.ErrHTTPSRequired, "https required", "internal", "log")
		expectAuthRouteError(rec, http.StatusBadRequest, ErrCodeAuthCookieFailed)

		rec = httptest.NewRecorder()
		c, _ = gin.CreateTestContext(rec)
		writeAuthCookieError(c, errors.New("cookie store down"), "https required", "internal", "log")
		expectAuthRouteError(rec, http.StatusInternalServerError, ErrCodeAuthInternalError)
	})

	ginkgo.It("respondWithAuthError maps localized, validation, core, API-key, and fallback errors", func() {
		localized := sharederrors.NewLocalizedErrorFromCode(ErrCodeAuthInvalidPayload, nil)
		rec := respondWithAuthErrorRecorder(localized)
		expectAuthRouteError(rec, http.StatusBadRequest, ErrCodeAuthInvalidPayload)

		validation := sharederrors.NewValidationErrors()
		validation.AddWithCode("username", FieldCodeAuthUsernameRequired, MessageIDAuthUsernameRequired)
		rec = respondWithAuthErrorRecorder(validation)
		expectAuthValidationError(rec)

		cases := []struct {
			err    error
			status int
			code   sharederrors.ErrorCode
		}{
			{coreauth.ErrInvalidToken, http.StatusUnprocessableEntity, ErrCodeAuthInvalidRefreshToken},
			{coreauth.ErrUserAccountLocked, http.StatusUnauthorized, ErrCodeAuthAccountLocked},
			{coreauth.ErrUserInvalidCredentials, http.StatusUnauthorized, ErrCodeAuthInvalidCredentials},
			{coreauth.ErrUserNotFound, http.StatusNotFound, ErrCodeAuthUserNotFound},
			{coreauth.ErrUserAlreadyExists, http.StatusConflict, ErrCodeAuthUserAlreadyExists},
			{coreauth.ErrUserInvalidRole, http.StatusBadRequest, ErrCodeAuthInvalidRole},
			{coreauth.ErrUserAdminCannotBeDeleted, http.StatusBadRequest, ErrCodeAuthAdminCannotDelete},
			{coreauth.ErrLastAdminCannotBeDemoted, http.StatusBadRequest, ErrCodeAuthLastAdminCannotBeDemoted},
			{coreauth.ErrAPIKeyNotFound, http.StatusNotFound, ErrCodeAuthUserNotFound},
			{coreauth.ErrAPIKeyInvalidName, http.StatusBadRequest, ErrCodeAuthInvalidRequest},
			{ErrAuthDisabled, http.StatusForbidden, ErrCodeAuthDisabled},
			{errors.New("unexpected"), http.StatusInternalServerError, ErrCodeAuthInternalError},
		}
		for _, tc := range cases {
			tc := tc
			rec = respondWithAuthErrorRecorder(tc.err)
			expectAuthRouteError(rec, tc.status, tc.code)
		}

		Expect(authErrorStatus(ErrCodeAuthTokenExpired)).To(Equal(http.StatusUnauthorized))
		Expect(authErrorStatus(ErrCodeAuthInvalidRefreshToken)).To(Equal(http.StatusUnprocessableEntity))
		Expect(authErrorStatus(ErrCodeAuthUserAlreadyExists)).To(Equal(http.StatusConflict))
		Expect(authErrorStatus(ErrCodeAuthAccountLocked)).To(Equal(http.StatusUnauthorized))
		Expect(authErrorStatus(ErrCodeAuthCsrfFailed)).To(Equal(http.StatusBadRequest))
		Expect(authErrorStatus(ErrCodeAuthForbidden)).To(Equal(http.StatusForbidden))
	})

	ginkgo.It("serves config and current-user responses with the expected cache and CSRF behavior", func() {
		fixture := newAuthRouteFixture()

		rec := performAuthHandlerRequest(
			fixture.routes.handleConfig(fixture.routerContext(true)),
			http.MethodGet,
			"/api/config",
			nil,
			nil,
			nil,
			false,
		)
		Expect(rec.Code).To(Equal(http.StatusOK), rec.Body.String())
		Expect(rec.Header().Get("X-CSRF-Token")).NotTo(BeEmpty())
		Expect(rec.Body.String()).To(ContainSubstring(`"authDisabled":false`))

		rec = performAuthHandlerRequest(
			fixture.routes.handleConfig(fixture.routerContext(false)),
			http.MethodGet,
			"/api/config",
			nil,
			nil,
			nil,
			false,
		)
		expectAuthRouteError(rec, http.StatusBadRequest, ErrCodeAuthCookieFailed)

		rec = performAuthHandlerRequest(fixture.routes.handleMe, http.MethodGet, "/api/auth/me", nil, nil, nil, false)
		Expect(rec.Code).To(Equal(http.StatusOK), rec.Body.String())
		Expect(strings.TrimSpace(rec.Body.String())).To(Equal("null"))
		Expect(rec.Header().Get("Cache-Control")).To(Equal("no-store"))

		rec = performAuthHandlerRequest(fixture.routes.handleMe, http.MethodGet, "/api/auth/me", nil, nil, fixture.admin, false)
		Expect(rec.Code).To(Equal(http.StatusOK), rec.Body.String())
		Expect(rec.Body.String()).To(ContainSubstring(`"username":"admin"`))
	})

	ginkgo.It("handles login, logout, and refresh-token requests", func() {
		fixture := newAuthRouteFixture()
		rctx := fixture.routerContext(true)

		rec := performAuthHandlerRequest(fixture.routes.handleLogin(rctx), http.MethodPost, "/api/auth/login", []byte(`{`), nil, nil, false)
		expectAuthRouteError(rec, http.StatusBadRequest, ErrCodeAuthInvalidPayload)

		rec = performAuthHandlerRequest(
			fixture.routes.handleLogin(rctx),
			http.MethodPost,
			"/api/auth/login",
			jsonBody(gin.H{"identifier": "admin", "password": "wrong-password"}),
			nil,
			nil,
			false,
		)
		expectAuthRouteError(rec, http.StatusUnauthorized, ErrCodeAuthInvalidCredentials)

		rec = performAuthHandlerRequest(
			fixture.routes.handleLogin(rctx),
			http.MethodPost,
			"/api/auth/login",
			jsonBody(gin.H{"identifier": "admin", "password": "password123"}),
			nil,
			nil,
			false,
		)
		Expect(rec.Code).To(Equal(http.StatusOK), rec.Body.String())
		Expect(rec.Body.String()).To(ContainSubstring(string(MessageIDAuthLoginSuccess)))

		rec = performAuthHandlerRequest(
			fixture.routes.handleLogin(fixture.routerContextWithCookieSecurity(true, false)),
			http.MethodPost,
			"/api/auth/login",
			jsonBody(gin.H{"identifier": "admin", "password": "password123"}),
			nil,
			nil,
			false,
		)
		expectAuthRouteError(rec, http.StatusBadRequest, ErrCodeAuthCookieFailed)

		rec = performAuthHandlerRequest(
			fixture.routes.handleLogin(fixture.routerContextWithCookieSecurity(false, true)),
			http.MethodPost,
			"/api/auth/login",
			jsonBody(gin.H{"identifier": "admin", "password": "password123"}),
			nil,
			nil,
			false,
		)
		expectAuthRouteError(rec, http.StatusBadRequest, ErrCodeAuthCookieFailed)

		originalSetAuthCookies := setAuthCookies
		ginkgo.DeferCleanup(func() {
			setAuthCookies = originalSetAuthCookies
		})
		setAuthCookies = func(_ *authmw.AuthCookies, _ *gin.Context, _ string, _ string) error {
			return errors.New("set failed")
		}
		rec = performAuthHandlerRequest(
			fixture.routes.handleLogin(rctx),
			http.MethodPost,
			"/api/auth/login",
			jsonBody(gin.H{"identifier": "admin", "password": "password123"}),
			nil,
			nil,
			false,
		)
		expectAuthRouteError(rec, http.StatusBadRequest, ErrCodeAuthCookieFailed)
		setAuthCookies = originalSetAuthCookies

		rec = performAuthHandlerRequest(fixture.routes.handleRefreshToken(rctx), http.MethodPost, "/api/auth/refresh-token", nil, nil, nil, false)
		expectAuthRouteError(rec, http.StatusUnprocessableEntity, ErrCodeAuthInvalidRefreshToken)

		rec = performAuthHandlerRequest(
			fixture.routes.handleRefreshToken(rctx),
			http.MethodPost,
			"/api/auth/refresh-token",
			nil,
			nil,
			nil,
			false,
			&http.Cookie{Name: "leafwiki_rt", Value: "invalid-refresh-token"},
		)
		expectAuthRouteError(rec, http.StatusUnprocessableEntity, ErrCodeAuthInvalidRefreshToken)

		token, err := fixture.authService.Login("admin", "password123")
		Expect(err).NotTo(HaveOccurred())
		rec = performAuthHandlerRequest(
			fixture.routes.handleRefreshToken(rctx),
			http.MethodPost,
			"/api/auth/refresh-token",
			nil,
			nil,
			nil,
			false,
			&http.Cookie{Name: "leafwiki_rt", Value: token.RefreshToken},
		)
		Expect(rec.Code).To(Equal(http.StatusOK), rec.Body.String())
		Expect(rec.Body.String()).To(ContainSubstring(string(MessageIDAuthRefreshTokenSuccess)))

		csrfFailureToken, err := fixture.authService.Login("admin", "password123")
		Expect(err).NotTo(HaveOccurred())
		rec = performAuthHandlerRequest(
			fixture.routes.handleRefreshToken(fixture.routerContextWithCookieSecurity(true, false)),
			http.MethodPost,
			"/api/auth/refresh-token",
			nil,
			nil,
			nil,
			false,
			&http.Cookie{Name: "leafwiki_rt", Value: csrfFailureToken.RefreshToken},
		)
		expectAuthRouteError(rec, http.StatusBadRequest, ErrCodeAuthCookieFailed)

		refreshHTTPSFailureToken, err := fixture.authService.Login("admin", "password123")
		Expect(err).NotTo(HaveOccurred())
		setAuthCookies = func(_ *authmw.AuthCookies, _ *gin.Context, _ string, _ string) error {
			return mwutils.ErrHTTPSRequired
		}
		rec = performAuthHandlerRequest(
			fixture.routes.handleRefreshToken(rctx),
			http.MethodPost,
			"/api/auth/refresh-token",
			nil,
			nil,
			nil,
			false,
			&http.Cookie{Name: "leafwiki_rt", Value: refreshHTTPSFailureToken.RefreshToken},
		)
		expectAuthRouteError(rec, http.StatusBadRequest, ErrCodeAuthCookieFailed)

		refreshSetFailureToken, err := fixture.authService.Login("admin", "password123")
		Expect(err).NotTo(HaveOccurred())
		setAuthCookies = func(_ *authmw.AuthCookies, _ *gin.Context, _ string, _ string) error {
			return errors.New("set failed")
		}
		rec = performAuthHandlerRequest(
			fixture.routes.handleRefreshToken(rctx),
			http.MethodPost,
			"/api/auth/refresh-token",
			nil,
			nil,
			nil,
			false,
			&http.Cookie{Name: "leafwiki_rt", Value: refreshSetFailureToken.RefreshToken},
		)
		expectAuthRouteError(rec, http.StatusInternalServerError, ErrCodeAuthCookieFailed)
		setAuthCookies = originalSetAuthCookies

		rec = performAuthHandlerRequest(
			fixture.routes.handleLogout(rctx),
			http.MethodPost,
			"/api/auth/logout",
			nil,
			nil,
			nil,
			false,
			&http.Cookie{Name: "leafwiki_rt", Value: token.RefreshToken},
		)
		Expect(rec.Code).To(Equal(http.StatusOK), rec.Body.String())

		logoutErrorRoutes := *fixture.routes
		logoutErrorRoutes.logout = NewLogoutUseCase(nil)
		rec = performAuthHandlerRequest(
			logoutErrorRoutes.handleLogout(rctx),
			http.MethodPost,
			"/api/auth/logout",
			nil,
			nil,
			nil,
			false,
			&http.Cookie{Name: "leafwiki_rt", Value: token.RefreshToken},
		)
		Expect(rec.Code).To(Equal(http.StatusOK), rec.Body.String())

		rec = performAuthHandlerRequest(fixture.routes.handleLogout(fixture.routerContextWithCookieSecurity(false, true)), http.MethodPost, "/api/auth/logout", nil, nil, nil, false)
		expectAuthRouteError(rec, http.StatusBadRequest, ErrCodeAuthCookieFailed)

		rec = performAuthHandlerRequest(fixture.routes.handleLogout(fixture.routerContextWithCookieSecurity(true, false)), http.MethodPost, "/api/auth/logout", nil, nil, nil, false)
		expectAuthRouteError(rec, http.StatusBadRequest, ErrCodeAuthCsrfFailed)

		rec = performAuthHandlerRequest(fixture.routes.handleLogout(rctx), http.MethodPost, "/api/auth/logout", nil, nil, nil, false)
		Expect(rec.Code).To(Equal(http.StatusOK), rec.Body.String())
		Expect(rec.Body.String()).To(ContainSubstring(string(MessageIDAuthLogoutSuccess)))
	})

	ginkgo.It("handles admin user CRUD and own password changes", func() {
		fixture := newAuthRouteFixture()

		rec := performAuthHandlerRequest(fixture.routes.handleCreateUser, http.MethodPost, "/api/users", []byte(`{`), nil, fixture.admin, false)
		expectAuthRouteError(rec, http.StatusBadRequest, ErrCodeAuthInvalidRequest)

		rec = performAuthHandlerRequest(
			fixture.routes.handleCreateUser,
			http.MethodPost,
			"/api/users",
			jsonBody(gin.H{"username": "bad", "email": "not-email", "password": "short", "role": "invalid"}),
			nil,
			fixture.admin,
			false,
		)
		expectAuthValidationError(rec)

		rec = performAuthHandlerRequest(
			fixture.routes.handleCreateUser,
			http.MethodPost,
			"/api/users",
			jsonBody(gin.H{"username": "new-user", "email": "new@example.test", "password": "password123", "role": coreauth.RoleViewer}),
			nil,
			fixture.admin,
			false,
		)
		Expect(rec.Code).To(Equal(http.StatusCreated), rec.Body.String())
		Expect(rec.Body.String()).To(ContainSubstring(`"username":"new-user"`))

		rec = performAuthHandlerRequest(
			fixture.routes.handleCreateUser,
			http.MethodPost,
			"/api/users",
			jsonBody(gin.H{"username": "new-user", "email": "new2@example.test", "password": "password123", "role": coreauth.RoleViewer}),
			nil,
			fixture.admin,
			false,
		)
		expectAuthRouteError(rec, http.StatusConflict, ErrCodeAuthUserAlreadyExists)

		rec = performAuthHandlerRequest(fixture.routes.handleGetUsers, http.MethodGet, "/api/users", nil, nil, fixture.admin, false)
		Expect(rec.Code).To(Equal(http.StatusOK), rec.Body.String())
		Expect(rec.Body.String()).To(ContainSubstring(`"username":"admin"`))

		errorRoutes := *fixture.routes
		errorRoutes.getUsers = NewGetUsersUseCase(setupUserServiceWithUnusableStorageDir())
		rec = performAuthHandlerRequest(errorRoutes.handleGetUsers, http.MethodGet, "/api/users", nil, nil, fixture.admin, false)
		expectAuthRouteError(rec, http.StatusInternalServerError, ErrCodeAuthInternalError)

		rec = performAuthHandlerRequest(
			fixture.routes.handleUpdateUser,
			http.MethodPut,
			"/api/users/"+fixture.editor.ID,
			jsonBody(gin.H{"username": "editor-updated", "email": "editor-updated@example.test", "role": coreauth.RoleAdmin}),
			gin.Params{{Key: "id", Value: fixture.editor.ID}},
			nil,
			false,
		)
		Expect(rec.Code).To(Equal(http.StatusForbidden), rec.Body.String())

		rec = performAuthHandlerRequest(
			fixture.routes.handleUpdateUser,
			http.MethodPut,
			"/api/users/"+fixture.editor.ID,
			[]byte(`{`),
			gin.Params{{Key: "id", Value: fixture.editor.ID}},
			fixture.admin,
			false,
		)
		expectAuthRouteError(rec, http.StatusBadRequest, ErrCodeAuthInvalidRequest)

		rec = performAuthHandlerRequest(
			fixture.routes.handleUpdateUser,
			http.MethodPut,
			"/api/users/"+fixture.editor.ID,
			jsonBody(gin.H{"username": "editor-updated", "email": "editor-updated@example.test", "role": coreauth.RoleAdmin}),
			gin.Params{{Key: "id", Value: fixture.editor.ID}},
			fixture.admin,
			false,
		)
		Expect(rec.Code).To(Equal(http.StatusOK), rec.Body.String())
		Expect(rec.Body.String()).To(ContainSubstring(`"role":"admin"`))

		rec = performAuthHandlerRequest(
			fixture.routes.handleUpdateUser,
			http.MethodPut,
			"/api/users/missing-user",
			jsonBody(gin.H{"username": "missing", "email": "missing@example.test"}),
			gin.Params{{Key: "id", Value: "missing-user"}},
			fixture.admin,
			false,
		)
		expectAuthRouteError(rec, http.StatusNotFound, ErrCodeAuthUserNotFound)

		deleteTarget, err := fixture.userService.CreateUser("delete-me", "delete@example.test", "password123", coreauth.RoleViewer)
		Expect(err).NotTo(HaveOccurred())
		rec = performAuthHandlerRequest(
			fixture.routes.handleDeleteUser,
			http.MethodDelete,
			"/api/users/"+deleteTarget.ID,
			nil,
			gin.Params{{Key: "id", Value: deleteTarget.ID}},
			fixture.admin,
			false,
		)
		Expect(rec.Code).To(Equal(http.StatusNoContent), rec.Body.String())

		rec = performAuthHandlerRequest(
			fixture.routes.handleDeleteUser,
			http.MethodDelete,
			"/api/users/"+fixture.admin.ID,
			nil,
			gin.Params{{Key: "id", Value: fixture.admin.ID}},
			fixture.admin,
			false,
		)
		expectAuthRouteError(rec, http.StatusBadRequest, ErrCodeAuthAdminCannotDelete)

		rec = performAuthHandlerRequest(fixture.routes.handleChangeOwnPassword, http.MethodPut, "/api/users/me/password", nil, nil, nil, false)
		Expect(rec.Code).To(Equal(http.StatusForbidden), rec.Body.String())

		rec = performAuthHandlerRequest(fixture.routes.handleChangeOwnPassword, http.MethodPut, "/api/users/me/password", []byte(`{`), nil, fixture.editor, false)
		expectAuthRouteError(rec, http.StatusBadRequest, ErrCodeAuthInvalidRequest)

		rec = performAuthHandlerRequest(
			fixture.routes.handleChangeOwnPassword,
			http.MethodPut,
			"/api/users/me/password",
			jsonBody(gin.H{"oldPassword": "wrong-password", "newPassword": "short"}),
			nil,
			fixture.editor,
			false,
		)
		expectAuthValidationError(rec)

		rec = performAuthHandlerRequest(
			fixture.routes.handleChangeOwnPassword,
			http.MethodPut,
			"/api/users/me/password",
			jsonBody(gin.H{"oldPassword": "password123", "newPassword": "new-password"}),
			nil,
			fixture.editor,
			false,
		)
		Expect(rec.Code).To(Equal(http.StatusNoContent), rec.Body.String())
	})

	ginkgo.It("handles admin and self API key flows", func() {
		fixture := newAuthRouteFixture()

		rec := performAuthHandlerRequest(
			fixture.routes.handleListUserAPIKeys,
			http.MethodGet,
			"/api/users/"+fixture.editor.ID+"/mcp-api-keys",
			nil,
			gin.Params{{Key: "id", Value: fixture.editor.ID}},
			fixture.admin,
			false,
		)
		Expect(rec.Code).To(Equal(http.StatusOK), rec.Body.String())
		Expect(strings.TrimSpace(rec.Body.String())).To(Equal("[]"))

		rec = performAuthHandlerRequest(
			fixture.routes.handleCreateUserAPIKey,
			http.MethodPost,
			"/api/users/"+fixture.editor.ID+"/mcp-api-keys",
			jsonBody(gin.H{"name": "admin-created"}),
			gin.Params{{Key: "id", Value: fixture.editor.ID}},
			nil,
			false,
		)
		Expect(rec.Code).To(Equal(http.StatusForbidden), rec.Body.String())

		rec = performAuthHandlerRequest(
			fixture.routes.handleCreateUserAPIKey,
			http.MethodPost,
			"/api/users/"+fixture.editor.ID+"/mcp-api-keys",
			jsonBody(gin.H{"name": "admin-created"}),
			gin.Params{{Key: "id", Value: fixture.editor.ID}},
			fixture.admin,
			false,
		)
		Expect(rec.Code).To(Equal(http.StatusCreated), rec.Body.String())

		rec = performAuthHandlerRequest(
			fixture.routes.handleCreateUserAPIKey,
			http.MethodPost,
			"/api/users/"+fixture.editor.ID+"/mcp-api-keys",
			[]byte(`{`),
			gin.Params{{Key: "id", Value: fixture.editor.ID}},
			fixture.admin,
			false,
		)
		expectAuthRouteError(rec, http.StatusBadRequest, ErrCodeAuthInvalidRequest)

		created, err := fixture.apiKeys.CreateAPIKey(newFixtureUserID(fixture.editor.ID), "to-revoke", newFixtureUserID(fixture.admin.ID))
		Expect(err).NotTo(HaveOccurred())
		rec = performAuthHandlerRequest(
			fixture.routes.handleRevokeUserAPIKey,
			http.MethodDelete,
			"/api/users/"+fixture.editor.ID+"/mcp-api-keys/"+created.Key.ID.String(),
			nil,
			gin.Params{{Key: "id", Value: fixture.editor.ID}, {Key: "keyId", Value: created.Key.ID.String()}},
			fixture.admin,
			false,
		)
		Expect(rec.Code).To(Equal(http.StatusNoContent), rec.Body.String())

		rec = performAuthHandlerRequest(fixture.routes.handleListOwnAPIKeys, http.MethodGet, "/api/users/me/mcp-api-keys", nil, nil, nil, false)
		Expect(rec.Code).To(Equal(http.StatusForbidden), rec.Body.String())

		rec = performAuthHandlerRequest(fixture.routes.handleListOwnAPIKeys, http.MethodGet, "/api/users/me/mcp-api-keys", nil, nil, fixture.editor, false)
		Expect(rec.Code).To(Equal(http.StatusOK), rec.Body.String())

		errorRoutes := *fixture.routes
		errorRoutes.listAPIKeys = NewListAPIKeysUseCase(nil)
		rec = performAuthHandlerRequest(
			errorRoutes.handleListUserAPIKeys,
			http.MethodGet,
			"/api/users/"+fixture.editor.ID+"/mcp-api-keys",
			nil,
			gin.Params{{Key: "id", Value: fixture.editor.ID}},
			fixture.admin,
			false,
		)
		expectAuthRouteError(rec, http.StatusNotFound, ErrCodeAuthUserNotFound)

		rec = performAuthHandlerRequest(errorRoutes.handleListOwnAPIKeys, http.MethodGet, "/api/users/me/mcp-api-keys", nil, nil, fixture.editor, false)
		expectAuthRouteError(rec, http.StatusNotFound, ErrCodeAuthUserNotFound)

		rec = performAuthHandlerRequest(fixture.routes.handleCreateOwnAPIKey, http.MethodPost, "/api/users/me/mcp-api-keys", jsonBody(gin.H{"name": "self"}), nil, nil, false)
		Expect(rec.Code).To(Equal(http.StatusForbidden), rec.Body.String())

		rec = performAuthHandlerRequest(fixture.routes.handleCreateOwnAPIKey, http.MethodPost, "/api/users/me/mcp-api-keys", jsonBody(gin.H{"name": "self"}), nil, fixture.editor, true)
		expectAuthRouteError(rec, http.StatusForbidden, ErrCodeAuthForbidden)

		rec = performAuthHandlerRequest(fixture.routes.handleCreateOwnAPIKey, http.MethodPost, "/api/users/me/mcp-api-keys", []byte(`{`), nil, fixture.editor, false)
		expectAuthRouteError(rec, http.StatusBadRequest, ErrCodeAuthInvalidRequest)

		rec = performAuthHandlerRequest(fixture.routes.handleCreateOwnAPIKey, http.MethodPost, "/api/users/me/mcp-api-keys", jsonBody(gin.H{"name": "self"}), nil, fixture.editor, false)
		expectAuthValidationError(rec)

		rec = performAuthHandlerRequest(
			fixture.routes.handleCreateOwnAPIKey,
			http.MethodPost,
			"/api/users/me/mcp-api-keys",
			jsonBody(gin.H{"name": "self", "currentPassword": "password123"}),
			nil,
			fixture.editor,
			false,
		)
		Expect(rec.Code).To(Equal(http.StatusCreated), rec.Body.String())
		Expect(rec.Header().Get("Cache-Control")).To(Equal("no-store"))

		ownKey, err := fixture.apiKeys.CreateAPIKey(newFixtureUserID(fixture.editor.ID), "own-revoke", newFixtureUserID(fixture.editor.ID))
		Expect(err).NotTo(HaveOccurred())
		rec = performAuthHandlerRequest(fixture.routes.handleRevokeOwnAPIKey, http.MethodDelete, "/api/users/me/mcp-api-keys/"+ownKey.Key.ID.String(), nil, gin.Params{{Key: "keyId", Value: ownKey.Key.ID.String()}}, nil, false)
		Expect(rec.Code).To(Equal(http.StatusForbidden), rec.Body.String())

		rec = performAuthHandlerRequest(fixture.routes.handleRevokeOwnAPIKey, http.MethodDelete, "/api/users/me/mcp-api-keys/"+ownKey.Key.ID.String(), nil, gin.Params{{Key: "keyId", Value: ownKey.Key.ID.String()}}, fixture.editor, false)
		Expect(rec.Code).To(Equal(http.StatusNoContent), rec.Body.String())

		errorRoutes = *fixture.routes
		errorRoutes.createAPIKey = NewCreateAPIKeyUseCase(nil, fixture.userService)
		rec = performAuthHandlerRequest(
			errorRoutes.handleCreateUserAPIKey,
			http.MethodPost,
			"/api/users/"+fixture.editor.ID+"/mcp-api-keys",
			jsonBody(gin.H{"name": "fails"}),
			gin.Params{{Key: "id", Value: fixture.editor.ID}},
			fixture.admin,
			false,
		)
		expectAuthRouteError(rec, http.StatusNotFound, ErrCodeAuthUserNotFound)

		errorRoutes = *fixture.routes
		errorRoutes.revokeAPIKey = NewRevokeAPIKeyUseCase(nil)
		rec = performAuthHandlerRequest(
			errorRoutes.handleRevokeUserAPIKey,
			http.MethodDelete,
			"/api/users/"+fixture.editor.ID+"/mcp-api-keys/missing-key",
			nil,
			gin.Params{{Key: "id", Value: fixture.editor.ID}, {Key: "keyId", Value: "missing-key"}},
			fixture.admin,
			false,
		)
		expectAuthRouteError(rec, http.StatusNotFound, ErrCodeAuthUserNotFound)

		rec = performAuthHandlerRequest(errorRoutes.handleRevokeOwnAPIKey, http.MethodDelete, "/api/users/me/mcp-api-keys/missing-key", nil, gin.Params{{Key: "keyId", Value: "missing-key"}}, fixture.editor, false)
		expectAuthRouteError(rec, http.StatusNotFound, ErrCodeAuthUserNotFound)
	})

	ginkgo.It("covers remaining auth use-case validation edges", func() {
		fixture := newAuthRouteFixture()

		_, err := fixture.routes.createUser.Execute(context.Background(), CreateUserInput{
			Username: "requires-fields",
			Role:     coreauth.RoleViewer,
		})
		expectAuthUseCaseValidationError(err, "email", "password")

		_, err = fixture.routes.updateUser.Execute(context.Background(), UpdateUserInput{
			ID:               newFixtureUserID(fixture.editor.ID),
			Email:            "not-email",
			Role:             coreauth.RoleViewer,
			RequesterIsAdmin: true,
		})
		expectAuthUseCaseValidationError(err, "username", "email")

		_, err = fixture.routes.updateUser.Execute(context.Background(), UpdateUserInput{
			ID:               newFixtureUserID(fixture.editor.ID),
			Username:         "editor-missing-email",
			RequesterIsAdmin: true,
		})
		expectAuthUseCaseValidationError(err, "email")

		err = fixture.routes.changeOwnPassword.Execute(context.Background(), ChangeOwnPasswordInput{
			UserID:      newFixtureUserID(fixture.editor.ID),
			OldPassword: "password123",
		})
		expectAuthUseCaseValidationError(err, "newPassword")

		err = fixture.routes.changeOwnPassword.Execute(context.Background(), ChangeOwnPasswordInput{
			UserID:      newFixtureUserID(fixture.editor.ID),
			OldPassword: "password123",
			NewPassword: "short",
		})
		expectAuthUseCaseValidationError(err, "newPassword")

		_, err = fixture.routes.getUserByID.Execute(context.Background(), GetUserByIDInput{ID: newFixtureUserID("missing-user")})
		Expect(err).To(MatchError(coreauth.ErrUserNotFound))

		_, err = fixture.routes.createAPIKey.Execute(context.Background(), CreateAPIKeyInput{
			UserID:          newFixtureUserID(fixture.editor.ID),
			Name:            strings.Repeat("x", maxAPIKeyNameLength+1),
			CreatedByUserID: newFixtureUserID(fixture.admin.ID),
		})
		expectAuthUseCaseValidationError(err, "name")
	})
})

type authRouteFixture struct {
	routes      *Routes
	userService *coreauth.UserService
	apiKeys     *coreauth.APIKeyService
	authService *coreauth.AuthService
	admin       *coreauth.User
	editor      *coreauth.User
}

func newAuthRouteFixture() authRouteFixture {
	ginkgo.GinkgoHelper()
	gin.SetMode(gin.TestMode)

	userService := setupUserService()
	admin, err := userService.CreateUser("admin", "admin@example.test", "password123", coreauth.RoleAdmin)
	Expect(err).NotTo(HaveOccurred())
	editor, err := userService.CreateUser("editor", "editor@example.test", "password123", coreauth.RoleEditor)
	Expect(err).NotTo(HaveOccurred())
	resolver, err := coreauth.NewUserResolver(userService)
	Expect(err).NotTo(HaveOccurred())

	sessionStore, err := coreauth.NewSessionStore(ginkgo.GinkgoT().TempDir())
	Expect(err).NotTo(HaveOccurred())
	ginkgo.DeferCleanup(func() {
		Expect(sessionStore.Close()).To(Succeed())
	})

	apiKeys := setupAPIKeyService(userService)
	authService := coreauth.NewAuthService(userService, sessionStore, strings.Repeat("s", 32), time.Minute, time.Hour)
	routes := NewRoutes(RoutesConfig{
		Login:             NewLoginUseCase(authService),
		Logout:            NewLogoutUseCase(authService),
		RefreshToken:      NewRefreshTokenUseCase(authService),
		CreateUser:        NewCreateUserUseCase(userService, resolver, slog.Default()),
		UpdateUser:        NewUpdateUserUseCase(userService, resolver, slog.Default()),
		ChangeOwnPassword: NewChangeOwnPasswordUseCase(userService),
		DeleteUser:        NewDeleteUserUseCase(userService, resolver, slog.Default()),
		GetUsers:          NewGetUsersUseCase(userService),
		GetUserByID:       NewGetUserByIDUseCase(userService),
		CreateAPIKey:      NewCreateAPIKeyUseCase(apiKeys, userService),
		ListAPIKeys:       NewListAPIKeysUseCase(apiKeys),
		RevokeAPIKey:      NewRevokeAPIKeyUseCase(apiKeys),
		AuthService:       authService,
	})

	return authRouteFixture{
		routes:      routes,
		userService: userService,
		apiKeys:     apiKeys,
		authService: authService,
		admin:       admin,
		editor:      editor,
	}
}

func (f authRouteFixture) routerContext(allowInsecure bool) httpinternal.RouterContext {
	ginkgo.GinkgoHelper()

	return f.routerContextWithCookieSecurity(allowInsecure, allowInsecure)
}

func (f authRouteFixture) routerContextWithCookieSecurity(authCookiesAllowInsecure bool, csrfCookieAllowInsecure bool) httpinternal.RouterContext {
	ginkgo.GinkgoHelper()

	return httpinternal.RouterContext{
		AuthCookies: authmw.NewAuthCookies(authCookiesAllowInsecure, time.Minute, time.Hour),
		CSRFCookie:  security.NewCSRFCookie(csrfCookieAllowInsecure, time.Minute),
		Opts: httpinternal.RouterOptions{
			AllowInsecure:           authCookiesAllowInsecure && csrfCookieAllowInsecure,
			DisableFrontendRoutes:   true,
			MarkdownLinkRootPrefix:  "/",
			EnableWorkspaceSync:     true,
			EnableLinkRefactor:      true,
			HideLinkMetadataSection: true,
		},
	}
}

func performAuthHandlerRequest(handler gin.HandlerFunc, method, target string, body []byte, params gin.Params, user *coreauth.User, remote bool, cookies ...*http.Cookie) *httptest.ResponseRecorder {
	ginkgo.GinkgoHelper()
	gin.SetMode(gin.TestMode)

	rec := httptest.NewRecorder()
	var reader *bytes.Reader
	if body == nil {
		reader = bytes.NewReader(nil)
	} else {
		reader = bytes.NewReader(body)
	}
	req := httptest.NewRequest(method, target, reader)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	for _, cookie := range cookies {
		req.AddCookie(cookie)
	}
	c, _ := gin.CreateTestContext(rec)
	c.Request = req
	c.Params = params
	if user != nil {
		c.Set("user", user)
	}
	if remote {
		c.Set(authmw.ContextAuthSource, authmw.AuthSourceRemoteUser)
	}

	handler(c)
	c.Writer.WriteHeaderNow()
	return rec
}

func jsonBody(v interface{}) []byte {
	ginkgo.GinkgoHelper()

	body, err := json.Marshal(v)
	Expect(err).NotTo(HaveOccurred())
	return body
}

func expectAuthRouteError(rec *httptest.ResponseRecorder, status int, code interface{}) {
	ginkgo.GinkgoHelper()

	Expect(rec.Code).To(Equal(status), rec.Body.String())
	var body AuthErrorResponse
	Expect(json.Unmarshal(rec.Body.Bytes(), &body)).To(Succeed())
	Expect(body.Error.Code).To(Equal(code))
}

func expectAuthValidationError(rec *httptest.ResponseRecorder) {
	ginkgo.GinkgoHelper()

	Expect(rec.Code).To(Equal(http.StatusBadRequest), rec.Body.String())
	var body map[string]interface{}
	Expect(json.Unmarshal(rec.Body.Bytes(), &body)).To(Succeed())
	Expect(body["error"]).To(Equal(authValidationErrorCode))
	Expect(body["fields"]).NotTo(BeEmpty())
}

func expectAuthUseCaseValidationError(err error, fields ...string) {
	ginkgo.GinkgoHelper()

	var validation *sharederrors.ValidationErrors
	Expect(errors.As(err, &validation)).To(BeTrue(), "error = %T %v", err, err)
	for _, field := range fields {
		field := field
		Expect(validation.Errors).To(ContainElement(HaveField("Field", field)))
	}
}

func respondWithAuthErrorRecorder(err error) *httptest.ResponseRecorder {
	ginkgo.GinkgoHelper()

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	respondWithAuthError(c, err)
	c.Writer.WriteHeaderNow()
	return rec
}
