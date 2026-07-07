package auth

import (
	"errors"
	"net/http"
	"net/http/httptest"

	"github.com/gin-gonic/gin"
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	coreauth "github.com/perber/wiki/internal/core/auth"
	sharederrors "github.com/perber/wiki/internal/core/shared/errors"
	httpinternal "github.com/perber/wiki/internal/http"
	authmw "github.com/perber/wiki/internal/http/middleware/auth"
	mwutils "github.com/perber/wiki/internal/http/middleware/utils"
	testmatchers "github.com/perber/wiki/internal/test_utils/matchers"
)

var _ = ginkgo.Describe("auth routes", ginkgo.Label("integration"), func() {
	ginkgo.It("exposes auth endpoints for both refresh-token limiter modes", func() {
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

	ginkgo.It("rejects protected route access when authentication is disabled and allows it when enabled", func() {
		rec := performAuthHandlerRequest(requireAuthEnabled(true), http.MethodGet, "/api/users/me/mcp-api-keys", nil, nil, nil, false)
		Expect(rec).To(matchAuthRouteError(http.StatusForbidden, ErrCodeAuthDisabled), rec.Body.String())

		rec = performAuthHandlerRequest(requireAuthEnabled(false), http.MethodGet, "/api/users/me/mcp-api-keys", nil, nil, nil, false)
		Expect(rec).To(HaveHTTPStatus(http.StatusOK))
	})

	ginkgo.It("returns cookie failure responses for secure-cookie setup errors and internal responses for unexpected cookie failures", func() {
		rec := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(rec)
		writeAuthCookieError(c, mwutils.ErrHTTPSRequired, "https required", "internal", "log")
		Expect(rec).To(matchAuthRouteError(http.StatusBadRequest, ErrCodeAuthCookieFailed), rec.Body.String())

		rec = httptest.NewRecorder()
		c, _ = gin.CreateTestContext(rec)
		writeAuthCookieError(c, errors.New("cookie store down"), "https required", "internal", "log")
		Expect(rec).To(matchAuthRouteError(http.StatusInternalServerError, ErrCodeAuthInternalError), rec.Body.String())
	})

	ginkgo.It("returns structured auth responses for localized validation core API key and fallback failures", func() {
		localized := sharederrors.NewLocalizedErrorFromCode(ErrCodeAuthInvalidPayload, nil)
		rec := respondWithAuthErrorRecorder(localized)
		Expect(rec).To(matchAuthRouteError(http.StatusBadRequest, ErrCodeAuthInvalidPayload), rec.Body.String())

		validation := sharederrors.NewValidationErrors()
		validation.AddWithCode("username", FieldCodeAuthUsernameRequired, MessageIDAuthUsernameRequired)
		rec = respondWithAuthErrorRecorder(validation)
		Expect(rec).To(matchAuthValidationResponse(), rec.Body.String())

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
			Expect(rec).To(matchAuthRouteError(tc.status, tc.code), rec.Body.String())
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
		Expect(rec).To(HaveHTTPStatus(http.StatusOK))
		Expect(rec).To(HaveHTTPHeaderWithValue("X-CSRF-Token", Not(BeEmpty())))
		Expect(rec).To(matchAuthJSONBodyField(authJSONFieldAuthDisabled, false))

		rec = performAuthHandlerRequest(
			fixture.routes.handleConfig(fixture.routerContext(false)),
			http.MethodGet,
			"/api/config",
			nil,
			nil,
			nil,
			false,
		)
		Expect(rec).To(matchAuthRouteError(http.StatusBadRequest, ErrCodeAuthCookieFailed), rec.Body.String())

		rec = performAuthHandlerRequest(fixture.routes.handleMe, http.MethodGet, "/api/auth/me", nil, nil, nil, false)
		Expect(rec).To(HaveHTTPStatus(http.StatusOK))
		Expect(rec).To(HaveHTTPBody("null"))
		Expect(rec).To(HaveHTTPHeaderWithValue("Cache-Control", "no-store"))

		rec = performAuthHandlerRequest(fixture.routes.handleMe, http.MethodGet, "/api/auth/me", nil, nil, fixture.admin, false)
		Expect(rec).To(HaveHTTPStatus(http.StatusOK))
		Expect(rec).To(matchAuthJSONBodyField(authJSONFieldUsername, "admin"))
	})

	ginkgo.It("handles login, logout, and refresh-token requests", func() {
		fixture := newAuthRouteFixture()
		rctx := fixture.routerContext(true)

		rec := performAuthHandlerRequest(fixture.routes.handleLogin(rctx), http.MethodPost, "/api/auth/login", []byte(`{`), nil, nil, false)
		Expect(rec).To(matchAuthRouteError(http.StatusBadRequest, ErrCodeAuthInvalidPayload), rec.Body.String())

		rec = performAuthHandlerRequest(
			fixture.routes.handleLogin(rctx),
			http.MethodPost,
			"/api/auth/login",
			jsonBody(gin.H{"identifier": "admin", "password": "wrong-password"}),
			nil,
			nil,
			false,
		)
		Expect(rec).To(matchAuthRouteError(http.StatusUnauthorized, ErrCodeAuthInvalidCredentials), rec.Body.String())

		rec = performAuthHandlerRequest(
			fixture.routes.handleLogin(rctx),
			http.MethodPost,
			"/api/auth/login",
			jsonBody(gin.H{"identifier": "admin", "password": "password123"}),
			nil,
			nil,
			false,
		)
		Expect(rec).To(HaveHTTPStatus(http.StatusOK))
		Expect(rec.Body.Bytes()).To(testmatchers.HaveMessageID(MessageIDAuthLoginSuccess))

		rec = performAuthHandlerRequest(
			fixture.routes.handleLogin(fixture.routerContextWithCookieSecurity(true, false)),
			http.MethodPost,
			"/api/auth/login",
			jsonBody(gin.H{"identifier": "admin", "password": "password123"}),
			nil,
			nil,
			false,
		)
		Expect(rec).To(matchAuthRouteError(http.StatusBadRequest, ErrCodeAuthCookieFailed), rec.Body.String())

		rec = performAuthHandlerRequest(
			fixture.routes.handleLogin(fixture.routerContextWithCookieSecurity(false, true)),
			http.MethodPost,
			"/api/auth/login",
			jsonBody(gin.H{"identifier": "admin", "password": "password123"}),
			nil,
			nil,
			false,
		)
		Expect(rec).To(matchAuthRouteError(http.StatusBadRequest, ErrCodeAuthCookieFailed), rec.Body.String())

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
		Expect(rec).To(matchAuthRouteError(http.StatusBadRequest, ErrCodeAuthCookieFailed), rec.Body.String())
		setAuthCookies = originalSetAuthCookies

		rec = performAuthHandlerRequest(fixture.routes.handleRefreshToken(rctx), http.MethodPost, "/api/auth/refresh-token", nil, nil, nil, false)
		Expect(rec).To(matchAuthRouteError(http.StatusUnprocessableEntity, ErrCodeAuthInvalidRefreshToken), rec.Body.String())

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
		Expect(rec).To(matchAuthRouteError(http.StatusUnprocessableEntity, ErrCodeAuthInvalidRefreshToken), rec.Body.String())

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
		Expect(rec).To(HaveHTTPStatus(http.StatusOK))
		Expect(rec.Body.Bytes()).To(testmatchers.HaveMessageID(MessageIDAuthRefreshTokenSuccess))

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
		Expect(rec).To(matchAuthRouteError(http.StatusBadRequest, ErrCodeAuthCookieFailed), rec.Body.String())

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
		Expect(rec).To(matchAuthRouteError(http.StatusBadRequest, ErrCodeAuthCookieFailed), rec.Body.String())

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
		Expect(rec).To(matchAuthRouteError(http.StatusInternalServerError, ErrCodeAuthCookieFailed), rec.Body.String())
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
		Expect(rec).To(HaveHTTPStatus(http.StatusOK))

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
		Expect(rec).To(HaveHTTPStatus(http.StatusOK))

		rec = performAuthHandlerRequest(fixture.routes.handleLogout(fixture.routerContextWithCookieSecurity(false, true)), http.MethodPost, "/api/auth/logout", nil, nil, nil, false)
		Expect(rec).To(matchAuthRouteError(http.StatusBadRequest, ErrCodeAuthCookieFailed), rec.Body.String())

		rec = performAuthHandlerRequest(fixture.routes.handleLogout(fixture.routerContextWithCookieSecurity(true, false)), http.MethodPost, "/api/auth/logout", nil, nil, nil, false)
		Expect(rec).To(matchAuthRouteError(http.StatusBadRequest, ErrCodeAuthCsrfFailed), rec.Body.String())

		rec = performAuthHandlerRequest(fixture.routes.handleLogout(rctx), http.MethodPost, "/api/auth/logout", nil, nil, nil, false)
		Expect(rec).To(HaveHTTPStatus(http.StatusOK))
		Expect(rec.Body.Bytes()).To(testmatchers.HaveMessageID(MessageIDAuthLogoutSuccess))
	})
})
