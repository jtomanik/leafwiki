package auth

import (
	"bytes"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/gstruct"
	"github.com/onsi/gomega/types"

	coreauth "github.com/perber/wiki/internal/core/auth"
	sharederrors "github.com/perber/wiki/internal/core/shared/errors"
	httpinternal "github.com/perber/wiki/internal/http"
	authmw "github.com/perber/wiki/internal/http/middleware/auth"
	"github.com/perber/wiki/internal/http/middleware/security"
	testmatchers "github.com/perber/wiki/internal/test_utils/matchers"
)

const (
	authValidationFieldUsername    testmatchers.ValidationField = "username"
	authValidationFieldEmail       testmatchers.ValidationField = "email"
	authValidationFieldPassword    testmatchers.ValidationField = "password"
	authValidationFieldNewPassword testmatchers.ValidationField = "newPassword"
	authValidationFieldName        testmatchers.ValidationField = "name"
)

type authValidationResponse struct {
	Status     int
	Error      string
	FieldCount int
}

type authFieldErrorExpectation struct {
	Field     testmatchers.ValidationField
	Code      sharederrors.FieldErrorCode
	MessageID sharederrors.MessageID
}

func matchAuthRouteError(status int, code sharederrors.ErrorCode) types.GomegaMatcher {
	return testmatchers.HaveHTTPStructuredError(status, code, sharederrors.MessageIDForCode(code))
}

func matchAuthValidationResponse() types.GomegaMatcher {
	return WithTransform(func(rec *httptest.ResponseRecorder) authValidationResponse {
		var body struct {
			Error  string            `json:"error"`
			Fields []json.RawMessage `json:"fields"`
		}
		_ = json.Unmarshal(rec.Body.Bytes(), &body)
		return authValidationResponse{
			Status:     rec.Code,
			Error:      body.Error,
			FieldCount: len(body.Fields),
		}
	}, gstruct.MatchAllFields(gstruct.Fields{
		"Status":     Equal(http.StatusBadRequest),
		"Error":      Equal(authValidationErrorCode),
		"FieldCount": BeNumerically(">", 0),
	}))
}

type authJSONField string

const (
	authJSONFieldAuthDisabled authJSONField = "authDisabled"
	authJSONFieldRole         authJSONField = "role"
	authJSONFieldUsername     authJSONField = "username"
)

func matchAuthJSONBodyField(field authJSONField, value any) types.GomegaMatcher {
	return WithTransform(authJSONBodyFields, HaveKeyWithValue(string(field), value))
}

func matchAuthJSONArrayElement(elementMatcher types.GomegaMatcher) types.GomegaMatcher {
	return WithTransform(authJSONArrayBodyFields, ContainElement(elementMatcher))
}

func authJSONBodyFields(rec *httptest.ResponseRecorder) (map[string]any, error) {
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		return nil, err
	}
	return body, nil
}

func authJSONArrayBodyFields(rec *httptest.ResponseRecorder) ([]map[string]any, error) {
	var body []map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		return nil, err
	}
	return body, nil
}

func matchAuthUseCaseValidationError(fields ...authFieldErrorExpectation) types.GomegaMatcher {
	matchers := make([]types.GomegaMatcher, 0, len(fields))
	for _, field := range fields {
		field := field
		matchers = append(matchers, testmatchers.ContainFieldError(field.Field, field.Code, field.MessageID))
	}
	return WithTransform(func(err error) *sharederrors.ValidationErrors {
		var validation *sharederrors.ValidationErrors
		_ = errors.As(err, &validation)
		return validation
	}, SatisfyAll(matchers...))
}

func expectAuthFieldError(
	field testmatchers.ValidationField,
	code sharederrors.FieldErrorCode,
	messageID sharederrors.MessageID,
) authFieldErrorExpectation {
	return authFieldErrorExpectation{Field: field, Code: code, MessageID: messageID}
}

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

	sessionStore, err := coreauth.NewSessionStore(tempAuthStorageDir())
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

func authUserAPIKeyPath(userID coreauth.UserID, keyID coreauth.APIKeyID) string {
	return "/api/users/" + userID.MetadataValue() + "/mcp-api-keys/" + url.PathEscape(keyID.String())
}

func authOwnAPIKeyPath(keyID coreauth.APIKeyID) string {
	return "/api/users/me/mcp-api-keys/" + url.PathEscape(keyID.String())
}

func authUserAPIKeyParams(userID coreauth.UserID, keyID coreauth.APIKeyID) gin.Params {
	return gin.Params{{Key: "id", Value: userID.MetadataValue()}, authAPIKeyParam(keyID)}
}

func authOwnAPIKeyParams(keyID coreauth.APIKeyID) gin.Params {
	return gin.Params{authAPIKeyParam(keyID)}
}

func authAPIKeyParam(keyID coreauth.APIKeyID) gin.Param {
	return gin.Param{Key: "keyId", Value: keyID.String()}
}

func respondWithAuthErrorRecorder(err error) *httptest.ResponseRecorder {
	ginkgo.GinkgoHelper()

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	respondWithAuthError(c, err)
	c.Writer.WriteHeaderNow()
	return rec
}
