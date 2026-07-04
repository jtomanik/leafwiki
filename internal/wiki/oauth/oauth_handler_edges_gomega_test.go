package oauth

import (
	"context"
	"encoding/base64"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/gstruct"
	"github.com/ory/fosite"
	coreauth "github.com/perber/wiki/internal/core/auth"
	httpinternal "github.com/perber/wiki/internal/http"
	authmw "github.com/perber/wiki/internal/http/middleware/auth"
)

var _ = Describe("OAuth authorization handler behavior", Label("integration"), func() {
	const redirectURI = "http://127.0.0.1:49152/callback"

	var (
		service     *Service
		routes      *Routes
		routerCtx   httpinternal.RouterContext
		currentUser *coreauth.User
	)

	BeforeEach(func() {
		service = newOAuthServiceForSpec(ServiceConfig{})
		routes = NewRoutes(service)
		routerCtx = newOAuthRouterContext("/wiki")
		currentUser = &coreauth.User{
			ID:       "user-1",
			Username: "alice",
			Email:    "alice@example.test",
			Role:     coreauth.RoleEditor,
		}
	})

	It("rejects invalid authorize redirect targets before provider work", func() {
		values := validAuthorizeRequestValues(redirectURI)
		values.Set("client_id", "missing-client")

		rec := performOAuthRequest(routes.handleAuthorize(routerCtx), newOAuthAuthorizeGET(values))

		Expect(rec).To(SatisfyAll(
			HaveHTTPStatus(http.StatusBadRequest),
			HaveHTTPBody(ContainSubstring("unknown oauth client")),
		))
	})

	It("redirects authorize request creation and validation failures back to the client", func() {
		values := validAuthorizeRequestValues(redirectURI)
		withOAuthAuthorizeRequest(nil, fosite.ErrInvalidRequest)

		rec := performOAuthRequest(routes.handleAuthorize(routerCtx), newOAuthAuthorizeGET(values))

		Expect(rec).To(SatisfyAll(
			HaveHTTPStatus(http.StatusFound),
			HaveHTTPHeaderWithValue("Location", SatisfyAll(
				ContainSubstring("error="+fosite.ErrInvalidRequest.ErrorField),
				ContainSubstring("state=native-parser-state"),
			)),
		))

		values = validAuthorizeRequestValues(redirectURI)
		values.Del("code_challenge")
		withOAuthAuthorizeRequest(newValidAuthorizeRequester(redirectURI), nil)

		rec = performOAuthRequest(routes.handleAuthorize(routerCtx), newOAuthAuthorizeGET(values))

		Expect(rec).To(SatisfyAll(
			HaveHTTPStatus(http.StatusFound),
			HaveHTTPHeaderWithValue("Location", ContainSubstring("error="+fosite.ErrInvalidRequest.ErrorField)),
		))
	})

	It("redirects anonymous authorize requests to login", func() {
		withOAuthAuthorizeRequest(newValidAuthorizeRequester(redirectURI), nil)
		withOAuthResolvedUser(nil, nil)

		rec := performOAuthRequest(routes.handleAuthorize(routerCtx), newOAuthAuthorizeGET(validAuthorizeRequestValues(redirectURI)))

		Expect(rec).To(SatisfyAll(
			HaveHTTPStatus(http.StatusFound),
			HaveHTTPHeaderWithValue("Location", HavePrefix("/wiki/login?returnTo=")),
		))
	})

	It("treats resolver failures as anonymous web users", func() {
		withOAuthResolvedUser(nil, errors.New("resolver failed"))

		c := newOAuthGinContext(httptest.NewRequest(http.MethodGet, "/oauth/authorize", nil))

		Expect(routes.currentWebUser(c, routerCtx)).To(BeNil())
	})

	It("handles approval extraction and explicit approval decisions", func() {
		withOAuthAuthorizeRequest(newValidAuthorizeRequester(redirectURI), nil)
		withOAuthResolvedUser(currentUser, nil)
		withOAuthApprovalValues(nil, "", errors.New("malformed approval form"))

		rec := performOAuthRequest(routes.handleAuthorize(routerCtx), newOAuthAuthorizeGET(validAuthorizeRequestValues(redirectURI)))

		Expect(rec).To(SatisfyAll(
			HaveHTTPStatus(http.StatusFound),
			HaveHTTPHeaderWithValue("Location", ContainSubstring("error="+fosite.ErrInvalidRequest.ErrorField)),
		))
		oauthAuthorizeApprovalValues = authorizeApprovalValues

		values := validAuthorizeRequestValues(redirectURI)
		values.Set("decision", "approve")
		values.Set("approval_token", "missing")
		withOAuthAuthorizeRequest(newValidAuthorizeRequester(redirectURI), nil)
		withOAuthResolvedUser(currentUser, nil)

		rec = performOAuthRequest(routes.handleAuthorize(routerCtx), newOAuthAuthorizePOST(values))

		Expect(rec).To(SatisfyAll(
			HaveHTTPStatus(http.StatusBadRequest),
			HaveHTTPBody(ContainSubstring(fosite.ErrInvalidRequest.ErrorField)),
		))

		values = validAuthorizeRequestValues(redirectURI)
		values.Set("decision", "deny")
		withOAuthAuthorizeRequest(newValidAuthorizeRequester(redirectURI), nil)
		withOAuthResolvedUser(currentUser, nil)

		rec = performOAuthRequest(routes.handleAuthorize(routerCtx), newOAuthAuthorizePOST(values))

		Expect(rec).To(SatisfyAll(
			HaveHTTPStatus(http.StatusFound),
			HaveHTTPHeaderWithValue("Location", ContainSubstring("error="+fosite.ErrAccessDenied.ErrorField)),
		))
	})

	It("issues approval redirects and surfaces approval-token entropy failures", func() {
		withOAuthAuthorizeRequest(newValidAuthorizeRequester(redirectURI), nil)
		withOAuthResolvedUser(currentUser, nil)
		withOAuthRandomRead(func([]byte) (int, error) {
			return 0, errors.New("entropy exhausted")
		})

		rec := performOAuthRequest(routes.handleAuthorize(routerCtx), newOAuthAuthorizeGET(validAuthorizeRequestValues(redirectURI)))

		Expect(rec).To(SatisfyAll(
			HaveHTTPStatus(http.StatusBadRequest),
			HaveHTTPBody(ContainSubstring("create oauth approval token")),
		))

		withOAuthAuthorizeRequest(newValidAuthorizeRequester(redirectURI), nil)
		withOAuthResolvedUser(currentUser, nil)
		withOAuthRandomBytes(7)

		rec = performOAuthRequest(routes.handleAuthorize(routerCtx), newOAuthAuthorizeGET(validAuthorizeRequestValues(redirectURI)))

		Expect(rec).To(SatisfyAll(
			HaveHTTPStatus(http.StatusFound),
			HaveHTTPHeaderWithValue("Location", SatisfyAll(
				ContainSubstring("/wiki/oauth/approve?"),
				ContainSubstring("approval_token="),
			)),
		))
		Expect(oauthApprovalGrants(service)).To(ConsistOf(gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
			"UserID":     Equal(coreauth.UserIDFromString(currentUser.ID)),
			"RequestKey": Equal(oauthApprovalKeyFor(validAuthorizeRequestValues(redirectURI))),
			"Details": gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
				"ClientID":    Equal(ClientID),
				"RedirectURI": Equal(redirectURI),
				"Scope":       Equal(ScopeMCP),
				"Resource":    Equal("http://leafwiki.test/wiki/mcp"),
			}),
		})))
	})

	It("exchanges approved authorize requests for redirect responses", func() {
		values := validAuthorizeRequestValues(redirectURI)
		seedOAuthApproval(service, "approved-error", currentUser, values)
		values.Set("decision", "approve")
		values.Set("approval_token", "approved-error")
		withOAuthAuthorizeRequest(newValidAuthorizeRequester(redirectURI), nil)
		withOAuthResolvedUser(currentUser, nil)
		withOAuthAuthorizeResponse(nil, fosite.ErrServerError)

		rec := performOAuthRequest(routes.handleAuthorize(routerCtx), newOAuthAuthorizePOST(values))

		Expect(rec).To(SatisfyAll(
			HaveHTTPStatus(http.StatusFound),
			HaveHTTPHeaderWithValue("Location", ContainSubstring("error="+oauthErrorServerError)),
		))

		values = validAuthorizeRequestValues(redirectURI)
		seedOAuthApproval(service, "approved-success", currentUser, values)
		values.Set("decision", "approve")
		values.Set("approval_token", "approved-success")
		withOAuthAuthorizeRequest(newValidAuthorizeRequester(redirectURI), nil)
		withOAuthResolvedUser(currentUser, nil)
		withOAuthAuthorizeResponse(&oauthAuthorizeResponderStub{
			header: http.Header{"X-Authorize": []string{"ok"}},
			params: url.Values{"code": []string{"code-1"}, "state": []string{"state-1"}},
		}, nil)

		rec = performOAuthRequest(routes.handleAuthorize(routerCtx), newOAuthAuthorizePOST(values))

		Expect(rec).To(SatisfyAll(
			HaveHTTPStatus(http.StatusFound),
			HaveHTTPHeaderWithValue("X-Authorize", "ok"),
			HaveHTTPHeaderWithValue("Location", SatisfyAll(
				ContainSubstring("code=code-1"),
				ContainSubstring("state=state-1"),
			)),
		))
	})

	It("returns approval details only for authenticated matching approval tokens", func() {
		withOAuthResolvedUser(nil, nil)

		rec := performOAuthRequest(routes.handleApprovalDetails(routerCtx), httptest.NewRequest(http.MethodGet, "/wiki/oauth/approval?approval_token=missing", nil))

		Expect(rec).To(SatisfyAll(
			HaveHTTPStatus(http.StatusUnauthorized),
			HaveHTTPBody(ContainSubstring(oauthErrorUnauthorized)),
		))

		withOAuthResolvedUser(currentUser, nil)

		rec = performOAuthRequest(routes.handleApprovalDetails(routerCtx), httptest.NewRequest(http.MethodGet, "/wiki/oauth/approval?approval_token=missing", nil))

		Expect(rec).To(SatisfyAll(
			HaveHTTPStatus(http.StatusBadRequest),
			HaveHTTPBody(ContainSubstring(oauthErrorInvalidApproval)),
		))

		details := approvalPageData{
			ClientLabel: "Native Client",
			ClientID:    "client-1",
			RedirectURI: redirectURI,
			Scope:       ScopeMCP,
			Resource:    "http://leafwiki.test/wiki/mcp",
		}
		service.approvals["approval-details"] = oauthApproval{
			UserID:     coreauth.UserIDFromString(currentUser.ID),
			RequestKey: "approval-key",
			Details:    details,
			ExpiresAt:  time.Now().Add(time.Minute),
		}
		withOAuthResolvedUser(currentUser, nil)

		rec = performOAuthRequest(routes.handleApprovalDetails(routerCtx), httptest.NewRequest(http.MethodGet, "/wiki/oauth/approval?approval_token=approval-details", nil))

		Expect(rec).To(SatisfyAll(
			HaveHTTPStatus(http.StatusOK),
			HaveHTTPBody(SatisfyAll(
				ContainSubstring(`"clientLabel":"Native Client"`),
				ContainSubstring(`"redirectUri":"`+redirectURI+`"`),
			)),
		))
	})
})

var _ = Describe("OAuth token and bearer behavior", Label("integration"), func() {
	It("handles token subject failures, access response failures, and successful token responses", func() {
		userService, user := newOAuthUserServiceForSpec("token-user")
		service := newOAuthServiceForSpec(ServiceConfig{UserService: userService})
		routes := NewRoutes(service)

		withOAuthAccessRequest(fosite.NewAccessRequest(newFositeSession("missing-user", "Missing")), nil)

		rec := performOAuthRequest(routes.handleToken, httptest.NewRequest(http.MethodPost, "/oauth/token", nil))

		Expect(rec).To(SatisfyAll(
			HaveHTTPStatus(http.StatusUnauthorized),
			HaveHTTPBody(ContainSubstring(oauthErrorInvalidGrant)),
		))

		withOAuthAccessRequest(fosite.NewAccessRequest(newFositeSession(user.ID, user.Username)), nil)
		withOAuthAccessResponse(nil, fosite.ErrServerError)

		rec = performOAuthRequest(routes.handleToken, httptest.NewRequest(http.MethodPost, "/oauth/token", nil))

		Expect(rec).To(SatisfyAll(
			HaveHTTPStatus(http.StatusInternalServerError),
			HaveHTTPBody(ContainSubstring(oauthErrorServerError)),
		))

		withOAuthAccessRequest(fosite.NewAccessRequest(newFositeSession(user.ID, user.Username)), nil)
		withOAuthAccessResponse(&oauthAccessResponderStub{
			body: map[string]interface{}{
				"access_token": "access-1",
				"token_type":   fosite.BearerAccessToken,
			},
		}, nil)

		rec = performOAuthRequest(routes.handleToken, httptest.NewRequest(http.MethodPost, "/oauth/token", nil))

		Expect(rec).To(SatisfyAll(
			HaveHTTPStatus(http.StatusOK),
			HaveHTTPBody(SatisfyAll(
				ContainSubstring(`"access_token":"access-1"`),
				ContainSubstring(`"token_type":"Bearer"`),
			)),
		))
	})

	It("verifies bearer tokens through every introspection outcome", func() {
		userService, user := newOAuthUserServiceForSpec("bearer-user")
		service := newOAuthServiceForSpec(ServiceConfig{UserService: userService})
		req := httptest.NewRequest(http.MethodGet, "/mcp", nil)

		withOAuthIntrospectToken(func(fosite.OAuth2Provider, context.Context, string, fosite.TokenUse, fosite.Session, ...string) (fosite.TokenUse, fosite.AccessRequester, error) {
			return "", nil, nil
		})

		info, err := service.VerifyBearerToken(context.Background(), "opaque", req)

		Expect(info).To(BeNil())
		Expect(err).To(matchOAuthInvalidTokenError())

		withOAuthIntrospectToken(func(fosite.OAuth2Provider, context.Context, string, fosite.TokenUse, fosite.Session, ...string) (fosite.TokenUse, fosite.AccessRequester, error) {
			return fosite.RefreshToken, fosite.NewAccessRequest(newFositeSession(user.ID, user.Username)), nil
		})

		info, err = service.VerifyBearerToken(context.Background(), "opaque", req)

		Expect(info).To(BeNil())
		Expect(err).To(matchOAuthInvalidTokenError())

		withOAuthIntrospectToken(func(fosite.OAuth2Provider, context.Context, string, fosite.TokenUse, fosite.Session, ...string) (fosite.TokenUse, fosite.AccessRequester, error) {
			return fosite.AccessToken, fosite.NewAccessRequest(newFositeSession("missing-user", "Missing")), nil
		})

		info, err = service.VerifyBearerToken(context.Background(), "opaque", req)

		Expect(info).To(BeNil())
		Expect(err).To(matchOAuthInvalidTokenError())

		requester := fosite.NewAccessRequest(newFositeSession(user.ID, user.Username))
		requester.GrantScope(ScopeMCP)
		withOAuthIntrospectToken(func(fosite.OAuth2Provider, context.Context, string, fosite.TokenUse, fosite.Session, ...string) (fosite.TokenUse, fosite.AccessRequester, error) {
			return fosite.AccessToken, requester, nil
		})

		info, err = service.VerifyBearerToken(context.Background(), "opaque", req)

		Expect(err).NotTo(HaveOccurred())
		Expect(info).To(gstruct.PointTo(gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
			"UserID": Equal(user.ID),
			"Scopes": Equal([]string{ScopeMCP}),
		})))
	})
})

var _ = Describe("OAuth deterministic service seams", Label("integration"), func() {
	It("surfaces random source failures from service, client ID, and approval token generation", func() {
		entropyExhaustedErr := errors.New("entropy exhausted")
		withOAuthRandomRead(func([]byte) (int, error) {
			return 0, entropyExhaustedErr
		})

		service, err := NewService(ServiceConfig{})
		Expect(service).To(BeNil())
		Expect(err).To(MatchError(entropyExhaustedErr))

		clientID, err := randomClientID()
		Expect(clientID).To(BeEmpty())
		Expect(err).To(MatchError(entropyExhaustedErr))

		service = &Service{approvals: map[string]oauthApproval{}}
		token, err := service.issueApproval(coreauth.UserIDFromString("user-1"), "request", approvalPageData{})
		Expect(token).To(BeEmpty())
		Expect(err).To(MatchError(entropyExhaustedErr))
	})

	It("surfaces fixed-client store initialization failures", func() {
		original := newOAuthFositeStore
		newOAuthFositeStore = func() *fositeStore { return nil }
		DeferCleanup(func() { newOAuthFositeStore = original })

		service, err := NewService(ServiceConfig{})

		Expect(service).To(BeNil())
		Expect(err).To(MatchError(fosite.ErrServerError))
	})

	It("retries dynamic client ID collisions and reports exhausted or failed registration", func() {
		client := registeredClient{
			ClientName:    "Native Client",
			RedirectURIs:  []string{"http://127.0.0.1:49152/callback"},
			GrantTypes:    []string{string(fosite.GrantTypeAuthorizationCode)},
			ResponseTypes: []string{responseTypeCode},
			Scope:         ScopeMCP,
		}

		service := newOAuthServiceForSpec(ServiceConfig{})
		service.clients[oauthClientIDForByte(1)] = registeredClient{}
		withOAuthRandomBytes(1, 2)

		clientID, err := service.registerDynamicClient(client)

		Expect(err).NotTo(HaveOccurred())
		Expect(clientID).To(Equal(oauthClientIDForByte(2)))

		service = newOAuthServiceForSpec(ServiceConfig{})
		service.clients[oauthClientIDForByte(3)] = registeredClient{}
		withOAuthRandomBytes(3)

		clientID, err = service.registerDynamicClient(client)

		Expect(clientID).To(BeEmpty())
		Expect(err).To(MatchError(ErrOAuthClientIDUnavailable))

		service = newOAuthServiceForSpec(ServiceConfig{})
		entropyExhaustedErr := errors.New("entropy exhausted")
		withOAuthRandomRead(func([]byte) (int, error) {
			return 0, entropyExhaustedErr
		})

		clientID, err = service.registerDynamicClient(client)

		Expect(clientID).To(BeEmpty())
		Expect(err).To(MatchError(entropyExhaustedErr))

		withOAuthRandomBytes(4)
		service = newOAuthServiceForSpec(ServiceConfig{})
		service.store = nil

		clientID, err = service.registerDynamicClient(client)

		Expect(clientID).To(BeEmpty())
		Expect(err).To(MatchError(fosite.ErrServerError))
	})

	It("keeps default Fosite seam wrappers delegated to the provider", func() {
		service := newOAuthServiceForSpec(ServiceConfig{})
		session := newFositeSession("user-1", "alice")
		authorizeRequester := newValidAuthorizeRequester("http://127.0.0.1:49152/callback")
		authorizeRequester.SetSession(session)
		authorizeRequester.GrantScope(ScopeMCP)

		_, _ = oauthNewAuthorizeResponse(service.fositeProvider, context.Background(), authorizeRequester, session)
		_, _ = oauthNewAccessResponse(service.fositeProvider, context.Background(), fosite.NewAccessRequest(session))
		_, _, _ = oauthIntrospectToken(service.fositeProvider, context.Background(), "missing-token", fosite.AccessToken, newFositeSession("", ""), ScopeMCP)
	})

	It("rejects malformed form bodies while constructing authorize requests", func() {
		service := newOAuthServiceForSpec(ServiceConfig{})
		req := httptest.NewRequest(http.MethodPost, "/oauth/authorize", errReader{})
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

		requester, err := service.newAuthorizeRequest(req, "http://127.0.0.1:49152/callback", "state")

		Expect(requester).To(BeNil())
		Expect(err).To(MatchError(fosite.ErrInvalidRequest))
	})
})

func newOAuthServiceForSpec(cfg ServiceConfig) *Service {
	GinkgoHelper()

	service, err := NewService(cfg)
	Expect(err).NotTo(HaveOccurred())
	return service
}

func newOAuthUserServiceForSpec(username string) (*coreauth.UserService, *coreauth.User) {
	GinkgoHelper()

	store, err := coreauth.NewUserStore(oauthTempDir())
	Expect(err).NotTo(HaveOccurred())
	DeferCleanup(func() {
		Expect(store.Close()).To(Succeed())
	})
	userService := coreauth.NewUserService(store)
	user, err := userService.CreateUser(username, username+"@example.test", "password123", coreauth.RoleEditor)
	Expect(err).NotTo(HaveOccurred())
	return userService, user
}

func newOAuthRouterContext(basePath string) httpinternal.RouterContext {
	GinkgoHelper()

	return httpinternal.RouterContext{
		AuthCookies: authmw.NewAuthCookies(true, time.Minute, time.Hour),
		Opts: httpinternal.RouterOptions{
			BasePath: basePath,
		},
	}
}

func newOAuthAuthorizeGET(values url.Values) *http.Request {
	GinkgoHelper()

	return httptest.NewRequest(http.MethodGet, "http://leafwiki.test/wiki/oauth/authorize?"+values.Encode(), nil)
}

func newOAuthAuthorizePOST(values url.Values) *http.Request {
	GinkgoHelper()

	req := httptest.NewRequest(http.MethodPost, "http://leafwiki.test/wiki/oauth/authorize", strings.NewReader(values.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	return req
}

func newOAuthGinContext(req *http.Request) *gin.Context {
	GinkgoHelper()
	gin.SetMode(gin.TestMode)

	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = req
	return c
}

func performOAuthRequest(handler gin.HandlerFunc, req *http.Request) *httptest.ResponseRecorder {
	GinkgoHelper()
	gin.SetMode(gin.TestMode)

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = req
	handler(c)
	c.Writer.WriteHeaderNow()
	return rec
}

func newValidAuthorizeRequester(redirectURI string) fosite.AuthorizeRequester {
	GinkgoHelper()

	parsedRedirectURI, err := url.Parse(redirectURI)
	Expect(err).NotTo(HaveOccurred())

	requester := fosite.NewAuthorizeRequest()
	requester.Client = fixedOAuthClient().fositeClient()
	requester.ResponseTypes = fosite.Arguments{responseTypeCode}
	requester.RedirectURI = parsedRedirectURI
	requester.AppendRequestedScope(ScopeMCP)
	return requester
}

func seedOAuthApproval(service *Service, token string, user *coreauth.User, values url.Values) {
	GinkgoHelper()

	service.approvals[token] = oauthApproval{
		UserID:     coreauth.UserIDFromString(user.ID),
		RequestKey: oauthApprovalKeyFor(values),
		Details: approvalPageData{
			ClientLabel: "LeafWiki local MCP",
			ClientID:    ClientID,
			RedirectURI: values.Get("redirect_uri"),
			Scope:       ScopeMCP,
			Resource:    "http://leafwiki.test/wiki/mcp",
		},
		ExpiresAt: time.Now().Add(time.Minute),
	}
}

func oauthApprovalGrants(service *Service) []oauthApproval {
	GinkgoHelper()

	grants := make([]oauthApproval, 0, len(service.approvals))
	for _, approval := range service.approvals {
		grants = append(grants, approval)
	}
	return grants
}

func oauthApprovalKeyFor(values url.Values) string {
	GinkgoHelper()

	req := newOAuthAuthorizePOST(values)
	_, key, err := authorizeApprovalValues(req)
	Expect(err).NotTo(HaveOccurred())
	return key
}

func withOAuthRandomRead(fn func([]byte) (int, error)) {
	GinkgoHelper()

	original := oauthRandomRead
	oauthRandomRead = fn
	DeferCleanup(func() { oauthRandomRead = original })
}

func withOAuthRandomBytes(sequence ...byte) {
	GinkgoHelper()

	call := 0
	withOAuthRandomRead(func(out []byte) (int, error) {
		value := sequence[len(sequence)-1]
		if call < len(sequence) {
			value = sequence[call]
		}
		call++
		for i := range out {
			out[i] = value
		}
		return len(out), nil
	})
}

func oauthClientIDForByte(value byte) string {
	raw := make([]byte, 32)
	for i := range raw {
		raw[i] = value
	}
	return "leafwiki-dcr-" + base64.RawURLEncoding.EncodeToString(raw)
}

func withOAuthResolvedUser(user *coreauth.User, err error) {
	GinkgoHelper()

	original := oauthResolveRequestUser
	oauthResolveRequestUser = func(*gin.Context, *coreauth.AuthService, *authmw.AuthCookies, bool) (*coreauth.User, error) {
		return user, err
	}
	DeferCleanup(func() { oauthResolveRequestUser = original })
}

func withOAuthApprovalValues(values url.Values, key string, err error) {
	GinkgoHelper()

	original := oauthAuthorizeApprovalValues
	oauthAuthorizeApprovalValues = func(*http.Request) (url.Values, string, error) {
		return values, key, err
	}
	DeferCleanup(func() { oauthAuthorizeApprovalValues = original })
}

func withOAuthAuthorizeRequest(requester fosite.AuthorizeRequester, err error) {
	GinkgoHelper()

	original := oauthNewAuthorizeRequest
	oauthNewAuthorizeRequest = func(fosite.OAuth2Provider, context.Context, *http.Request) (fosite.AuthorizeRequester, error) {
		return requester, err
	}
	DeferCleanup(func() { oauthNewAuthorizeRequest = original })
}

func withOAuthAuthorizeResponse(response fosite.AuthorizeResponder, err error) {
	GinkgoHelper()

	original := oauthNewAuthorizeResponse
	oauthNewAuthorizeResponse = func(fosite.OAuth2Provider, context.Context, fosite.AuthorizeRequester, fosite.Session) (fosite.AuthorizeResponder, error) {
		return response, err
	}
	DeferCleanup(func() { oauthNewAuthorizeResponse = original })
}

func withOAuthAccessRequest(requester fosite.AccessRequester, err error) {
	GinkgoHelper()

	original := oauthNewAccessRequest
	oauthNewAccessRequest = func(fosite.OAuth2Provider, context.Context, *http.Request, fosite.Session) (fosite.AccessRequester, error) {
		return requester, err
	}
	DeferCleanup(func() { oauthNewAccessRequest = original })
}

func withOAuthAccessResponse(response fosite.AccessResponder, err error) {
	GinkgoHelper()

	original := oauthNewAccessResponse
	oauthNewAccessResponse = func(fosite.OAuth2Provider, context.Context, fosite.AccessRequester) (fosite.AccessResponder, error) {
		return response, err
	}
	DeferCleanup(func() { oauthNewAccessResponse = original })
}

func withOAuthIntrospectToken(fn func(fosite.OAuth2Provider, context.Context, string, fosite.TokenUse, fosite.Session, ...string) (fosite.TokenUse, fosite.AccessRequester, error)) {
	GinkgoHelper()

	original := oauthIntrospectToken
	oauthIntrospectToken = fn
	DeferCleanup(func() { oauthIntrospectToken = original })
}

type errReader struct{}

func (errReader) Read([]byte) (int, error) {
	return 0, errors.New("read failed")
}
