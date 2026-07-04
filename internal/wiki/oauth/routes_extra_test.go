package oauth

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
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
	"github.com/ory/fosite"

	coreauth "github.com/perber/wiki/internal/core/auth"
	httpinternal "github.com/perber/wiki/internal/http"
)

var _ = ginkgo.Describe("OAuth routes and responses", ginkgo.Label("integration"), func() {
	ginkgo.It("RegisterRoutes exposes metadata endpoints only when local MCP OAuth is enabled", func() {
		gin.SetMode(gin.TestMode)

		inactiveRouter := httpinternal.NewRouter(
			[]httpinternal.RouteRegistrar{NewRoutes(nil)},
			httpinternal.FrontendConfig{},
			httpinternal.RouterOptions{MCPEnabled: true, MCPBindHost: "127.0.0.1", DisableFrontendRoutes: true},
		)
		rec := httptest.NewRecorder()
		inactiveRouter.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/.well-known/oauth-authorization-server", nil))
		Expect(rec).To(HaveHTTPStatus(http.StatusNotFound))

		service, err := NewService(ServiceConfig{})
		Expect(err).NotTo(HaveOccurred())
		router := httpinternal.NewRouter(
			[]httpinternal.RouteRegistrar{NewRoutes(service)},
			httpinternal.FrontendConfig{},
			httpinternal.RouterOptions{
				BasePath:              "/wiki",
				MCPEnabled:            true,
				MCPBindHost:           "127.0.0.1",
				DisableFrontendRoutes: true,
			},
		)

		rec = httptest.NewRecorder()
		router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/.well-known/oauth-authorization-server/wiki", nil))
		Expect(rec).To(SatisfyAll(
			HaveHTTPStatus(http.StatusOK),
			HaveHTTPBody(SatisfyAll(
				ContainSubstring(`"authorization_endpoint"`),
				ContainSubstring(`/wiki/oauth/authorize`),
			)),
		))

		rec = httptest.NewRecorder()
		router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/.well-known/oauth-protected-resource/wiki/mcp", nil))
		Expect(rec).To(SatisfyAll(
			HaveHTTPStatus(http.StatusOK),
			HaveHTTPHeaderWithValue("Access-Control-Allow-Origin", "*"),
			HaveHTTPBody(ContainSubstring(`"resource"`)),
		))

		rec = httptest.NewRecorder()
		router.ServeHTTP(rec, httptest.NewRequest(http.MethodOptions, "/.well-known/oauth-protected-resource/wiki/mcp", nil))
		Expect(rec).To(HaveHTTPStatus(http.StatusNoContent))
	})

	ginkgo.It("metadata URL helpers derive absolute URLs from TLS, URL, and host state", func() {
		req := httptest.NewRequest(http.MethodGet, "/wiki/oauth/authorize?state=abc", nil)
		req.Host = "leafwiki.test"
		req.TLS = &tls.ConnectionState{}
		Expect(requestOrigin(req)).To(Equal("https://leafwiki.test"))
		Expect(absoluteRequestURL(req)).To(Equal("https://leafwiki.test/wiki/oauth/authorize?state=abc"))

		req = httptest.NewRequest(http.MethodGet, "https://urlhost.test/wiki/oauth/authorize", nil)
		req.Host = ""
		Expect(requestOrigin(req)).To(Equal("https://urlhost.test"))
	})

	ginkgo.It("writes OAuth error and token responses with expected status and cache headers", func() {
		rec := performOAuthResponseRequest(func(c *gin.Context) {
			writeOAuthBadRequest(c, errors.New("bad oauth request"))
		})
		Expect(rec).To(SatisfyAll(
			HaveHTTPStatus(http.StatusBadRequest),
			HaveHTTPBody(ContainSubstring("bad oauth request")),
		))

		rec = performOAuthResponseRequest(func(c *gin.Context) {
			writeRegistrationError(c, "redirect_uris is required")
		})
		Expect(rec).To(SatisfyAll(
			HaveHTTPStatus(http.StatusBadRequest),
			HaveHTTPBody(ContainSubstring(oauthErrorInvalidClientMetadata)),
		))

		rec = performOAuthResponseRequest(func(c *gin.Context) {
			writeTokenError(c, fosite.ErrInvalidGrant)
		})
		Expect(rec).To(SatisfyAll(
			HaveHTTPStatus(http.StatusUnauthorized),
			HaveHTTPHeaderWithValue("Cache-Control", "no-store"),
			HaveHTTPBody(ContainSubstring(oauthErrorInvalidGrant)),
		))

		rec = performOAuthResponseRequest(func(c *gin.Context) {
			writeTokenResponse(c, &oauthAccessResponderStub{
				body: map[string]interface{}{
					"access_token": "access",
					"token_type":   fosite.BearerAccessToken,
				},
			})
		})
		Expect(rec).To(SatisfyAll(
			HaveHTTPStatus(http.StatusOK),
			HaveHTTPHeaderWithValue("Pragma", "no-cache"),
			HaveHTTPBody(ContainSubstring(`"token_type":"Bearer"`)),
		))
	})

	ginkgo.It("registers dynamic public clients and rejects unsupported metadata", func() {
		service, err := NewService(ServiceConfig{})
		Expect(err).NotTo(HaveOccurred())
		routes := NewRoutes(service)

		rec := performOAuthJSONRequest(routes.handleRegister, http.MethodPost, "/oauth/register", []byte(`{`))
		Expect(rec).To(SatisfyAll(
			HaveHTTPStatus(http.StatusBadRequest),
			HaveHTTPBody(ContainSubstring(oauthErrorInvalidClientMetadata)),
		))

		rec = performOAuthJSONRequest(routes.handleRegister, http.MethodPost, "/oauth/register", oauthJSONBody(gin.H{
			"client_secret": "unsupported",
			"redirect_uris": []string{"http://127.0.0.1:49152/callback"},
		}))
		Expect(rec).To(SatisfyAll(
			HaveHTTPStatus(http.StatusBadRequest),
			HaveHTTPBody(ContainSubstring("client_secret is not supported")),
		))

		rec = performOAuthJSONRequest(routes.handleRegister, http.MethodPost, "/oauth/register", oauthJSONBody(gin.H{
			"token_endpoint_auth_method": "client_secret_basic",
			"redirect_uris":              []string{"http://127.0.0.1:49152/callback"},
		}))
		Expect(rec).To(SatisfyAll(
			HaveHTTPStatus(http.StatusBadRequest),
			HaveHTTPBody(ContainSubstring("only public clients")),
		))

		rec = performOAuthJSONRequest(routes.handleRegister, http.MethodPost, "/oauth/register", oauthJSONBody(gin.H{
			"client_name":                "Native Client",
			"redirect_uris":              []string{"http://127.0.0.1:49152/callback"},
			"grant_types":                []string{"authorization_code"},
			"response_types":             []string{"code"},
			"scope":                      ScopeMCP,
			"token_endpoint_auth_method": "none",
		}))
		Expect(rec).To(SatisfyAll(
			HaveHTTPStatus(http.StatusCreated),
			HaveHTTPBody(SatisfyAll(
				ContainSubstring(`"client_name":"Native Client"`),
				ContainSubstring(`"scope":"`+ScopeMCP+`"`),
			)),
		))

		var body struct {
			ClientID string `json:"client_id"`
		}
		Expect(json.Unmarshal(rec.Body.Bytes(), &body)).To(Succeed())
		Expect(service.clients).To(HaveKeyWithValue(body.ClientID, HaveField("ClientName", Equal("Native Client"))))
		Expect(body.ClientID).To(HavePrefix("leafwiki-dcr-"))
	})

	ginkgo.It("manages approval tokens and approval request values", func() {
		service, err := NewService(ServiceConfig{})
		Expect(err).NotTo(HaveOccurred())
		userID := coreauth.UserIDFromString("user-1")
		details := approvalPageData{
			ClientLabel: "Client",
			ClientID:    "client-1",
			RedirectURI: "http://127.0.0.1:49152/callback",
			Scope:       ScopeMCP,
			Resource:    "http://leafwiki.test/mcp",
		}

		Expect(consumeOAuthApproval(service, "", userID, "request")).To(MatchError(errOAuthApprovalRejected))
		Expect(lookupApprovalDetails(service, "", userID)).To(haveNoApprovalDetails())

		token, err := service.issueApproval(userID, "request", details)
		Expect(err).NotTo(HaveOccurred())
		Expect(lookupApprovalDetails(service, " "+token+" ", userID)).To(haveApprovalDetails(details))
		Expect(lookupApprovalDetails(service, token, coreauth.UserIDFromString("other-user"))).To(haveNoApprovalDetails())
		Expect(consumeOAuthApproval(service, token, coreauth.UserIDFromString("other-user"), "request")).To(MatchError(errOAuthApprovalRejected))
		Expect(consumeOAuthApproval(service, token, userID, "request")).To(MatchError(errOAuthApprovalRejected))

		token, err = service.issueApproval(userID, "request", details)
		Expect(err).NotTo(HaveOccurred())
		Expect(consumeOAuthApproval(service, token, userID, "request")).To(Succeed())
		Expect(consumeOAuthApproval(service, token, userID, "request")).To(MatchError(errOAuthApprovalRejected))

		service.approvals["expired"] = oauthApproval{
			UserID:     userID,
			RequestKey: "expired-request",
			Details:    details,
			ExpiresAt:  time.Now().Add(-time.Minute),
		}
		Expect(lookupApprovalDetails(service, "expired", userID)).To(haveNoApprovalDetails())

		values := validAuthorizeRequestValues("http://127.0.0.1:49152/callback")
		values.Set("resource", "http://leafwiki.test/mcp")
		req := httptest.NewRequest(http.MethodPost, "/oauth/authorize", strings.NewReader(values.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		approvalValues, approvalKey, err := authorizeApprovalValues(req)
		Expect(err).NotTo(HaveOccurred())
		Expect(approvalValues.Get("client_id")).To(Equal(ClientID))
		Expect(approvalValues.Get("resource")).To(Equal("http://leafwiki.test/mcp"))
		Expect(approvalKey).To(ContainSubstring("client_id="))
	})

	ginkgo.It("validates authorize resources, redirect URIs, client scopes, and token service guards", func() {
		service, err := NewService(ServiceConfig{})
		Expect(err).NotTo(HaveOccurred())
		routes := NewRoutes(service)

		req := httptest.NewRequest(http.MethodGet, "/oauth/authorize", nil)
		Expect(validateAuthorizeResource(req, "")).To(Succeed())

		req = httptest.NewRequest(http.MethodGet, "/oauth/authorize?resource=a&resource=b", nil)
		Expect(validateAuthorizeResource(req, "")).To(MatchError(ErrOAuthResourceMustBeSingular))

		req = httptest.NewRequest(http.MethodGet, "http://leafwiki.test/oauth/authorize?resource=http://wrong.test/mcp", nil)
		Expect(validateAuthorizeResource(req, "")).To(MatchError(ErrOAuthResourceMismatch))

		req = httptest.NewRequest(http.MethodGet, "http://leafwiki.test/oauth/authorize?resource=http://leafwiki.test/mcp", nil)
		Expect(validateAuthorizeResource(req, "")).To(Succeed())

		Expect(validateLoopbackRedirectURI("https://127.0.0.1:49152/callback")).To(MatchError(ErrOAuthRedirectURIMustUseHTTP))
		Expect(validateLoopbackRedirectURI("http://127.0.0.1/callback")).To(MatchError(ErrOAuthRedirectURIPortRequired))
		Expect(validateLoopbackRedirectURI("http://127.0.0.1:49152/callback#fragment")).To(MatchError(ErrOAuthRedirectURIHasFragment))
		Expect(validateLoopbackRedirectURI("http://example.com:49152/callback")).To(MatchError(ErrOAuthRedirectURINotLoopback))

		service.clients["open-client"] = registeredClient{}
		req = httptest.NewRequest(http.MethodGet, "/oauth/authorize?client_id=open-client&redirect_uri=http://127.0.0.1:49154/callback", nil)
		redirectURI, _, err := routes.validateAuthorizeRedirectTarget(req)
		Expect(err).To(Succeed())
		Expect(redirectURI).To(Equal("http://127.0.0.1:49154/callback"))

		restrictedClient := registeredClient{
			RedirectURIs:  []string{"http://127.0.0.1:49152/callback"},
			ResponseTypes: []string{responseTypeCode},
			Scope:         ScopeMCP,
		}
		service.clients["restricted-client"] = restrictedClient
		req = httptest.NewRequest(http.MethodGet, "/oauth/authorize?client_id=restricted-client&redirect_uri=http://127.0.0.1:49152/callback", nil)
		redirectURI, _, err = routes.validateAuthorizeRedirectTarget(req)
		Expect(err).To(Succeed())
		Expect(redirectURI).To(Equal("http://127.0.0.1:49152/callback"))

		req = httptest.NewRequest(http.MethodGet, "/oauth/authorize?client_id=restricted-client&redirect_uri=http://127.0.0.1:49153/callback", nil)
		_, _, err = routes.validateAuthorizeRedirectTarget(req)
		Expect(err).To(MatchError(ErrOAuthRedirectURIUnregistered))

		authorizeRequest := func(clientID string, scopes ...string) fosite.AuthorizeRequester {
			parsedRedirectURI, parseErr := url.Parse("http://127.0.0.1:49152/callback")
			Expect(parseErr).NotTo(HaveOccurred())

			ar := fosite.NewAuthorizeRequest()
			ar.Client = &fosite.DefaultClient{
				ID:            clientID,
				GrantTypes:    []string{string(fosite.GrantTypeAuthorizationCode)},
				ResponseTypes: []string{responseTypeCode},
				Scopes:        []string{ScopeMCP},
				Public:        true,
			}
			ar.ResponseTypes = fosite.Arguments{responseTypeCode}
			ar.RedirectURI = parsedRedirectURI
			ar.RequestedScope = fosite.Arguments(scopes)
			return ar
		}
		validAuthorizeReq := httptest.NewRequest(http.MethodGet, "http://leafwiki.test/oauth/authorize?code_challenge=abcdefghijklmnopqrstuvwxyz0123456789ABCDEFGHIJKLMNOPQRSTUV&code_challenge_method=S256&resource=http://leafwiki.test/mcp", nil)
		Expect(routes.validateAuthorizeRequest(validAuthorizeReq, authorizeRequest("restricted-client"), "")).To(Succeed())
		Expect(routes.validateAuthorizeRequest(validAuthorizeReq, authorizeRequest("restricted-client", ScopeMCP), "")).To(Succeed())
		Expect(routes.validateAuthorizeRequest(validAuthorizeReq, authorizeRequest("restricted-client", "other"), "")).To(MatchError(fosite.ErrInvalidScope))

		service.clients["open-scope-client"] = registeredClient{ResponseTypes: []string{responseTypeCode}}
		Expect(routes.validateAuthorizeRequest(validAuthorizeReq, authorizeRequest("open-scope-client", ScopeMCP), "")).To(Succeed())

		service.clients["other-scope-client"] = registeredClient{
			ResponseTypes: []string{responseTypeCode},
			Scope:         "other",
		}
		Expect(routes.validateAuthorizeRequest(validAuthorizeReq, authorizeRequest("other-scope-client", ScopeMCP), "")).To(MatchError(fosite.ErrInvalidScope))

		_, err = ((*Service)(nil)).VerifyBearerToken(context.Background(), "token", httptest.NewRequest(http.MethodGet, "/mcp", nil))
		Expect(err).To(matchOAuthInvalidTokenError())
		_, err = service.VerifyBearerToken(context.Background(), "not-a-token", httptest.NewRequest(http.MethodGet, "/mcp", nil))
		Expect(err).To(matchOAuthInvalidTokenError())

		Expect(routes.service.clients).NotTo(HaveKey("missing-client"))
	})

	ginkgo.It("validates token subjects and rejects malformed token requests", func() {
		userStore, err := coreauth.NewUserStore(oauthTempDir())
		Expect(err).NotTo(HaveOccurred())
		ginkgo.DeferCleanup(func() {
			Expect(userStore.Close()).To(Succeed())
		})
		userService := coreauth.NewUserService(userStore)
		user, err := userService.CreateUser("alice", "alice@example.test", "password123", coreauth.RoleEditor)
		Expect(err).NotTo(HaveOccurred())
		service, err := NewService(ServiceConfig{UserService: userService})
		Expect(err).NotTo(HaveOccurred())

		Expect(((*Service)(nil)).validateTokenSubject(nil)).To(MatchError(fosite.ErrInvalidGrant))
		Expect((&Service{}).validateTokenSubject(nil)).To(MatchError(fosite.ErrInvalidGrant))
		Expect(service.validateTokenSubject(fosite.NewAccessRequest(nil))).To(MatchError(fosite.ErrInvalidGrant))

		missing := fosite.NewAccessRequest(newFositeSession("missing-user", "Missing"))
		Expect(service.validateTokenSubject(missing)).To(MatchError(fosite.ErrInvalidGrant))

		request := fosite.NewAccessRequest(newFositeSession(user.ID, user.Username))
		Expect(service.validateTokenSubject(request)).To(Succeed())

		rec := performOAuthResponseRequest(NewRoutes(service).handleToken)
		Expect(rec).NotTo(HaveHTTPStatus(http.StatusOK))
		Expect(rec).To(HaveHTTPHeaderWithValue("Cache-Control", "no-store"))
	})

	ginkgo.It("maps approval page data and writes authorization redirects", func() {
		service, err := NewService(ServiceConfig{})
		Expect(err).NotTo(HaveOccurred())
		routes := NewRoutes(service)

		redirectURI, err := url.Parse("http://127.0.0.1:49152/callback")
		Expect(err).NotTo(HaveOccurred())
		req := httptest.NewRequest(http.MethodGet, "http://leafwiki.test/wiki/oauth/authorize?resource=http://leafwiki.test/wiki/mcp", nil)
		ar := fosite.NewAuthorizeRequest()
		ar.Client = fixedOAuthClient().fositeClient()
		ar.RedirectURI = redirectURI
		ar.AppendRequestedScope(ScopeMCP)
		details := service.approvalPageData(req, ar, "/wiki")
		Expect(details).To(haveOAuthApprovalPageData(gstruct.Fields{
			"ClientID":    Equal(ClientID),
			"ClientLabel": Equal(fixedOAuthClient().name),
			"RedirectURI": Equal(redirectURI.String()),
			"Scope":       Equal(ScopeMCP),
			"Resource":    Equal(MCPResourceURL(req, "/wiki")),
		}))

		service.clients[ClientID] = registeredClient{}
		ar.RequestedScope = nil
		req = httptest.NewRequest(http.MethodGet, "http://leafwiki.test/wiki/oauth/authorize", nil)
		details = service.approvalPageData(req, ar, "/wiki")
		Expect(details).To(haveOAuthApprovalPageData(gstruct.Fields{
			"ClientLabel": Equal(ClientID),
			"Scope":       Equal(ScopeMCP),
			"Resource":    Equal(MCPResourceURL(req, "/wiki")),
		}))

		rec := performOAuthResponseRequest(func(c *gin.Context) {
			routes.redirectAuthorizeError(c, ":", "state", fosite.ErrInvalidRequest)
		})
		Expect(rec).To(HaveHTTPStatus(http.StatusBadRequest))

		rec = performOAuthResponseRequest(func(c *gin.Context) {
			routes.redirectAuthorizeError(c, "http://127.0.0.1:49152/callback?existing=1", "state", fosite.ErrInvalidRequest)
		})
		Expect(rec).To(HaveHTTPStatus(http.StatusFound))
		location := rec.Header().Get("Location")
		Expect(location).To(ContainSubstring("error=invalid_request"))
		Expect(location).To(ContainSubstring("state=state"))

		rec = performOAuthResponseRequest(func(c *gin.Context) {
			writeAuthorizeRedirect(c, ar, &oauthAuthorizeResponderStub{
				header: http.Header{"X-Test": []string{"yes"}},
				params: url.Values{
					"code":  []string{"code-1"},
					"state": []string{"state-1"},
				},
			})
		})
		Expect(rec).To(SatisfyAll(
			HaveHTTPStatus(http.StatusFound),
			HaveHTTPHeaderWithValue("X-Test", "yes"),
			HaveHTTPHeaderWithValue("Cache-Control", "no-store"),
		))
		location = rec.Header().Get("Location")
		Expect(location).To(ContainSubstring("code=code-1"))
		Expect(location).To(ContainSubstring("state=state-1"))
	})
})

func performOAuthResponseRequest(handler gin.HandlerFunc) *httptest.ResponseRecorder {
	ginkgo.GinkgoHelper()
	gin.SetMode(gin.TestMode)

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodGet, "/oauth/test", nil)
	handler(c)
	c.Writer.WriteHeaderNow()
	return rec
}

func performOAuthJSONRequest(handler gin.HandlerFunc, method, target string, body []byte) *httptest.ResponseRecorder {
	ginkgo.GinkgoHelper()
	gin.SetMode(gin.TestMode)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(method, target, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	c, _ := gin.CreateTestContext(rec)
	c.Request = req
	handler(c)
	c.Writer.WriteHeaderNow()
	return rec
}

func haveOAuthApprovalPageData(fields gstruct.Fields) types.GomegaMatcher {
	ginkgo.GinkgoHelper()
	return gstruct.MatchFields(gstruct.IgnoreExtras, fields)
}

var (
	errOAuthApprovalDetailsUnavailable = errors.New("oauth approval details unavailable")
	errOAuthApprovalRejected           = errors.New("oauth approval rejected")
)

type oauthApprovalLookupResult struct {
	Details approvalPageData
	Err     error
}

func lookupApprovalDetails(service *Service, token string, userID coreauth.UserID) oauthApprovalLookupResult {
	details, available := service.approvalDetails(token, userID)
	if !available {
		return oauthApprovalLookupResult{Err: errOAuthApprovalDetailsUnavailable}
	}
	return oauthApprovalLookupResult{Details: details}
}

func haveApprovalDetails(details approvalPageData) types.GomegaMatcher {
	return gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
		"Details": Equal(details),
		"Err":     Succeed(),
	})
}

func haveNoApprovalDetails() types.GomegaMatcher {
	return gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
		"Err": MatchError(errOAuthApprovalDetailsUnavailable),
	})
}

func consumeOAuthApproval(service *Service, token string, userID coreauth.UserID, requestKey string) error {
	if service.consumeApproval(token, userID, requestKey) {
		return nil
	}
	return errOAuthApprovalRejected
}

func oauthJSONBody(v interface{}) []byte {
	ginkgo.GinkgoHelper()

	body, err := json.Marshal(v)
	Expect(err).NotTo(HaveOccurred())
	return body
}

type oauthAccessResponderStub struct {
	body map[string]interface{}
}

func (s *oauthAccessResponderStub) SetExtra(key string, value interface{}) {
	s.body[key] = value
}

func (s *oauthAccessResponderStub) GetExtra(key string) interface{} {
	return s.body[key]
}

func (s *oauthAccessResponderStub) SetExpiresIn(time.Duration) {}

func (s *oauthAccessResponderStub) SetScopes(fosite.Arguments) {}

func (s *oauthAccessResponderStub) SetAccessToken(token string) {
	s.body["access_token"] = token
}

func (s *oauthAccessResponderStub) SetTokenType(tokenType string) {
	s.body["token_type"] = tokenType
}

func (s *oauthAccessResponderStub) GetAccessToken() string {
	token, _ := s.body["access_token"].(string)
	return token
}

func (s *oauthAccessResponderStub) GetTokenType() string {
	tokenType, _ := s.body["token_type"].(string)
	return tokenType
}

func (s *oauthAccessResponderStub) ToMap() map[string]interface{} {
	return s.body
}

var _ fosite.AccessResponder = (*oauthAccessResponderStub)(nil)

type oauthAuthorizeResponderStub struct {
	header http.Header
	params url.Values
}

func (s *oauthAuthorizeResponderStub) GetCode() string {
	return s.params.Get("code")
}

func (s *oauthAuthorizeResponderStub) GetHeader() http.Header {
	return s.header
}

func (s *oauthAuthorizeResponderStub) AddHeader(key, value string) {
	s.header.Add(key, value)
}

func (s *oauthAuthorizeResponderStub) GetParameters() url.Values {
	return s.params
}

func (s *oauthAuthorizeResponderStub) AddParameter(key, value string) {
	s.params.Add(key, value)
}

var _ fosite.AuthorizeResponder = (*oauthAuthorizeResponderStub)(nil)
