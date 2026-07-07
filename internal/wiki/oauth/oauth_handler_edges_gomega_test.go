package oauth

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/gstruct"
	"github.com/ory/fosite"
	coreauth "github.com/perber/wiki/internal/core/auth"
	httpinternal "github.com/perber/wiki/internal/http"
)

func newFixtureOAuthUserID[T ~string](raw T) coreauth.UserID {
	return coreauth.NewUserIDUnchecked(string(raw))
}

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
			ID:       newFixtureOAuthUserID("user-1"),
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

		withOAuthAccessRequest(fosite.NewAccessRequest(newFositeSession(user.ID.MetadataValue(), user.Username)), nil)
		withOAuthAccessResponse(nil, fosite.ErrServerError)

		rec = performOAuthRequest(routes.handleToken, httptest.NewRequest(http.MethodPost, "/oauth/token", nil))

		Expect(rec).To(SatisfyAll(
			HaveHTTPStatus(http.StatusInternalServerError),
			HaveHTTPBody(ContainSubstring(oauthErrorServerError)),
		))

		withOAuthAccessRequest(fosite.NewAccessRequest(newFositeSession(user.ID.MetadataValue(), user.Username)), nil)
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
			return fosite.RefreshToken, fosite.NewAccessRequest(newFositeSession(user.ID.MetadataValue(), user.Username)), nil
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

		requester := fosite.NewAccessRequest(newFositeSession(user.ID.MetadataValue(), user.Username))
		requester.GrantScope(ScopeMCP)
		withOAuthIntrospectToken(func(fosite.OAuth2Provider, context.Context, string, fosite.TokenUse, fosite.Session, ...string) (fosite.TokenUse, fosite.AccessRequester, error) {
			return fosite.AccessToken, requester, nil
		})

		info, err = service.VerifyBearerToken(context.Background(), "opaque", req)

		Expect(err).NotTo(HaveOccurred())
		Expect(info).To(gstruct.PointTo(gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
			"UserID": Equal(user.ID.MetadataValue()),
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
