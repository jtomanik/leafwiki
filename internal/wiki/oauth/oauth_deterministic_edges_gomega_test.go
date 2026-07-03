package oauth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/ory/fosite"
	coreauth "github.com/perber/wiki/internal/core/auth"
)

var _ = Describe("OAuth deterministic service behavior", func() {
	It("covers fixed-client redirect adapters and client assertion replay guards", func() {
		ctx := context.Background()
		store := newFositeStore()
		Expect(store.setClient(fixedOAuthClient())).To(Succeed())

		redirectURI := "http://127.0.0.1:49152/callback"
		_, ok := fixedClientRedirectFromContext(ctx, ClientID)
		Expect(ok).To(BeFalse())
		_, ok = fixedClientRedirectFromContext(context.WithValue(ctx, fixedClientRedirectContextKey{}, redirectURI), "other-client")
		Expect(ok).To(BeFalse())

		fixedCtx := context.WithValue(ctx, fixedClientRedirectContextKey{}, redirectURI)
		redirect, ok := fixedClientRedirectFromContext(fixedCtx, ClientID)
		Expect(ok).To(BeTrue())
		Expect(redirect).To(Equal(redirectURI))

		client, err := store.GetClient(fixedCtx, ClientID)
		Expect(err).NotTo(HaveOccurred())
		Expect(client.GetRedirectURIs()).To(Equal([]string{redirectURI}))

		Expect(store.ClientAssertionJWTValid(ctx, "jti-1")).To(Succeed())
		Expect(store.SetClientAssertionJWT(ctx, "expired", time.Now().Add(-time.Minute))).To(Succeed())
		Expect(store.SetClientAssertionJWT(ctx, "jti-1", time.Now().Add(time.Hour))).To(Succeed())
		Expect(store.ClientAssertionJWTValid(ctx, "jti-1")).To(MatchError(fosite.ErrJTIKnown))
		Expect(store.SetClientAssertionJWT(ctx, "jti-1", time.Now().Add(time.Hour))).To(MatchError(fosite.ErrJTIKnown))
		Expect(store.SetClientAssertionJWT(ctx, "jti-2", time.Now().Add(time.Hour))).To(Succeed())
		Expect(store.clientAssertionJTIs).NotTo(HaveKey("expired"))
	})

	It("keeps fosite store nil and missing-token contracts explicit", func() {
		ctx := context.Background()
		requester := newStoreTestRequester("nil-branch")
		session := newFositeSession("", "")
		var nilStore *fositeStore

		Expect(nilStore.setClient(fixedOAuthClient())).To(MatchError(fosite.ErrServerError))
		_, err := nilStore.GetClient(ctx, ClientID)
		Expect(err).To(MatchError(fosite.ErrNotFound))
		Expect(nilStore.ClientAssertionJWTValid(ctx, "jti")).To(MatchError(fosite.ErrNotFound))
		Expect(nilStore.SetClientAssertionJWT(ctx, "jti", time.Now().Add(time.Hour))).To(MatchError(fosite.ErrServerError))

		Expect(nilStore.CreateAuthorizeCodeSession(ctx, "code", requester)).To(MatchError(fosite.ErrServerError))
		_, err = nilStore.GetAuthorizeCodeSession(ctx, "code", session)
		Expect(err).To(MatchError(fosite.ErrNotFound))
		Expect(nilStore.InvalidateAuthorizeCodeSession(ctx, "code")).To(MatchError(fosite.ErrNotFound))

		Expect(nilStore.CreatePKCERequestSession(ctx, "pkce", requester)).To(MatchError(fosite.ErrServerError))
		_, err = nilStore.GetPKCERequestSession(ctx, "pkce", session)
		Expect(err).To(MatchError(fosite.ErrNotFound))
		Expect(nilStore.DeletePKCERequestSession(ctx, "pkce")).To(MatchError(fosite.ErrServerError))

		Expect(nilStore.CreateAccessTokenSession(ctx, "access", requester)).To(MatchError(fosite.ErrServerError))
		_, err = nilStore.GetAccessTokenSession(ctx, "access", session)
		Expect(err).To(MatchError(fosite.ErrNotFound))
		Expect(nilStore.DeleteAccessTokenSession(ctx, "access")).To(MatchError(fosite.ErrServerError))

		Expect(nilStore.CreateRefreshTokenSession(ctx, "refresh", "access", requester)).To(MatchError(fosite.ErrServerError))
		_, err = nilStore.GetRefreshTokenSession(ctx, "refresh", session)
		Expect(err).To(MatchError(fosite.ErrNotFound))
		Expect(nilStore.DeleteRefreshTokenSession(ctx, "refresh")).To(MatchError(fosite.ErrServerError))
		Expect(nilStore.RevokeRefreshToken(ctx, "request")).To(MatchError(fosite.ErrServerError))
		Expect(nilStore.RevokeAccessToken(ctx, "request")).To(MatchError(fosite.ErrServerError))
		Expect(nilStore.RotateRefreshToken(ctx, "request", "refresh")).To(MatchError(fosite.ErrServerError))

		store := newFositeStore()
		_, err = store.GetAuthorizeCodeSession(ctx, "missing-code", session)
		Expect(err).To(MatchError(fosite.ErrNotFound))
		Expect(store.InvalidateAuthorizeCodeSession(ctx, "missing-code")).To(MatchError(fosite.ErrNotFound))
		Expect(store.RevokeRefreshToken(ctx, "missing-request")).To(Succeed())
		Expect(store.RevokeAccessToken(ctx, "missing-request")).To(Succeed())
		Expect(store.RotateRefreshToken(ctx, "missing-request", "missing-refresh")).To(MatchError(fosite.ErrNotFound))

		store.requestIDsToRefresh["dangling-request"] = "dangling-refresh"
		Expect(store.RevokeRefreshToken(ctx, "dangling-request")).To(MatchError(fosite.ErrNotFound))

		Expect(store.CreateRefreshTokenSession(ctx, "refresh-1", "", requester)).To(Succeed())
		Expect(store.RotateRefreshToken(ctx, "different-request", "refresh-1")).To(MatchError(fosite.ErrInactiveToken))
		Expect(store.DeleteRefreshTokenSession(ctx, "refresh-1")).To(Succeed())
		Expect(store.requestIDsToRefresh).NotTo(HaveKey(requester.GetID()))
	})

	It("normalizes registration edges and validates authorize redirect targets", func() {
		Expect(fositeScopesForRegisteredClient(registeredClient{})).To(Equal([]string{ScopeMCP}))
		Expect(fositeScopesForRegisteredClient(registeredClient{Scope: "  alpha   beta  "})).To(Equal([]string{"alpha", "beta"}))

		_, err := normalizeRedirectURIs(nil)
		Expect(err).To(MatchError(ErrOAuthRedirectURIsRequired))
		_, err = normalizeRedirectURIs([]string{"http://example.com:49152/callback"})
		Expect(err).To(MatchError(ErrOAuthRedirectURINotLoopback))

		grants, err := normalizeRegistrationGrantTypes([]string{string(fosite.GrantTypeAuthorizationCode)})
		Expect(err).NotTo(HaveOccurred())
		Expect(grants).To(Equal([]string{string(fosite.GrantTypeAuthorizationCode)}))
		grants, err = normalizeRegistrationGrantTypes([]string{string(fosite.GrantTypeAuthorizationCode), string(fosite.GrantTypeRefreshToken)})
		Expect(err).NotTo(HaveOccurred())
		Expect(grants).To(Equal([]string{string(fosite.GrantTypeAuthorizationCode), string(fosite.GrantTypeRefreshToken)}))
		_, err = normalizeRegistrationGrantTypes([]string{"client_credentials"})
		Expect(err).To(MatchError(ErrOAuthUnsupportedGrantType))

		scope, err := normalizeRegistrationScope("")
		Expect(err).NotTo(HaveOccurred())
		Expect(scope).To(BeEmpty())
		_, err = normalizeRegistrationScope(ScopeMCP + " other")
		Expect(err).To(MatchError(ErrOAuthUnsupportedScope))

		service, err := NewService(ServiceConfig{})
		Expect(err).NotTo(HaveOccurred())
		routes := NewRoutes(service)

		req := httptest.NewRequest(http.MethodGet, "/oauth/authorize?client_id=missing&redirect_uri=http://127.0.0.1:49152/callback", nil)
		_, _, err = routes.validateAuthorizeRedirectTarget(req)
		Expect(err).To(MatchError(ErrOAuthUnknownClient))

		req = httptest.NewRequest(http.MethodGet, "/oauth/authorize?client_id="+ClientID+"&redirect_uri=https://127.0.0.1:49152/callback", nil)
		_, _, err = routes.validateAuthorizeRedirectTarget(req)
		Expect(err).To(MatchError(ErrOAuthRedirectURIMustUseHTTP))

		service.clients["restricted-client"] = registeredClient{RedirectURIs: []string{"http://127.0.0.1:49152/callback"}}
		req = httptest.NewRequest(http.MethodGet, "/oauth/authorize?client_id=restricted-client&redirect_uri=http://127.0.0.1:49153/callback", nil)
		_, _, err = routes.validateAuthorizeRedirectTarget(req)
		Expect(err).To(MatchError(ErrOAuthRedirectURIUnregistered))

		req = httptest.NewRequest(http.MethodGet, "/oauth/authorize?client_id=restricted-client&redirect_uri=http://127.0.0.1:49152/callback&state=ok", nil)
		redirectURI, state, err := routes.validateAuthorizeRedirectTarget(req)
		Expect(err).NotTo(HaveOccurred())
		Expect(redirectURI).To(Equal("http://127.0.0.1:49152/callback"))
		Expect(state).To(Equal("ok"))

		Expect(validateLoopbackRedirectURI("%")).To(MatchError(ErrOAuthRedirectURIInvalid))
	})

	It("validates parsed authorize requests without invoking the full handler", func() {
		service, err := NewService(ServiceConfig{})
		Expect(err).NotTo(HaveOccurred())
		routes := NewRoutes(service)
		redirectURI, err := url.Parse("http://127.0.0.1:49152/callback")
		Expect(err).NotTo(HaveOccurred())

		newRequest := func() *fosite.AuthorizeRequest {
			ar := fosite.NewAuthorizeRequest()
			ar.Client = fixedOAuthClient().fositeClient()
			ar.ResponseTypes = fosite.Arguments{responseTypeCode}
			ar.RedirectURI = redirectURI
			ar.AppendRequestedScope(ScopeMCP)
			return ar
		}
		httpReq := httptest.NewRequest(http.MethodGet, "http://leafwiki.test/oauth/authorize?code_challenge=abcdefghijklmnopqrstuvwxyz0123456789ABCDEFGHIJKLMNOPQRSTUV&code_challenge_method=S256&resource=http://leafwiki.test/mcp", nil)
		Expect(routes.validateAuthorizeRequest(httpReq, newRequest(), "")).To(Succeed())

		ar := newRequest()
		ar.Client = &fosite.DefaultClient{
			ID:            "missing-client",
			GrantTypes:    []string{string(fosite.GrantTypeAuthorizationCode)},
			ResponseTypes: []string{responseTypeCode},
			Scopes:        []string{ScopeMCP},
			Public:        true,
		}
		Expect(routes.validateAuthorizeRequest(httpReq, ar, "")).To(MatchError(ErrOAuthUnknownClient))

		ar = newRequest()
		ar.ResponseTypes = fosite.Arguments{"token"}
		Expect(routes.validateAuthorizeRequest(httpReq, ar, "")).To(MatchError(fosite.ErrUnsupportedResponseType))

		ar = newRequest()
		ar.RequestedScope = fosite.Arguments{"other"}
		Expect(routes.validateAuthorizeRequest(httpReq, ar, "")).To(MatchError(fosite.ErrInvalidScope))

		ar = newRequest()
		missingChallengeReq := httptest.NewRequest(http.MethodGet, "http://leafwiki.test/oauth/authorize?code_challenge_method=S256", nil)
		Expect(routes.validateAuthorizeRequest(missingChallengeReq, ar, "")).To(MatchError(fosite.ErrInvalidRequest))

		ar = newRequest()
		ar.RedirectURI, err = url.Parse("https://127.0.0.1:49152/callback")
		Expect(err).NotTo(HaveOccurred())
		Expect(routes.validateAuthorizeRequest(httpReq, ar, "")).To(MatchError(ErrOAuthRedirectURIMustUseHTTP))

		service.clients["restricted-validate-client"] = registeredClient{
			RedirectURIs:  []string{"http://127.0.0.1:49152/allowed"},
			ResponseTypes: []string{responseTypeCode},
			Scope:         ScopeMCP,
		}
		ar = newRequest()
		ar.Client = &fosite.DefaultClient{
			ID:            "restricted-validate-client",
			GrantTypes:    []string{string(fosite.GrantTypeAuthorizationCode)},
			ResponseTypes: []string{responseTypeCode},
			Scopes:        []string{ScopeMCP},
			Public:        true,
		}
		Expect(routes.validateAuthorizeRequest(httpReq, ar, "")).To(MatchError(fosite.ErrInvalidRequest))

		badResourceReq := httptest.NewRequest(http.MethodGet, "http://leafwiki.test/oauth/authorize?code_challenge=abcdefghijklmnopqrstuvwxyz0123456789ABCDEFGHIJKLMNOPQRSTUV&code_challenge_method=S256&resource=http://wrong.test/mcp", nil)
		Expect(routes.validateAuthorizeRequest(badResourceReq, newRequest(), "")).To(MatchError(fosite.ErrInvalidRequest))
	})

	It("covers registration handler error exits", func() {
		service, err := NewService(ServiceConfig{})
		Expect(err).NotTo(HaveOccurred())
		routes := NewRoutes(service)

		rec := performOAuthJSONRequest(routes.handleRegister, http.MethodPost, "/oauth/register", oauthJSONBody(map[string]any{}))
		Expect(rec).To(SatisfyAll(
			HaveHTTPStatus(http.StatusBadRequest),
			HaveHTTPBody(ContainSubstring(oauthErrorInvalidClientMetadata)),
		))

		rec = performOAuthJSONRequest(routes.handleRegister, http.MethodPost, "/oauth/register", oauthJSONBody(map[string]any{
			"redirect_uris": []string{"http://127.0.0.1:49152/callback"},
			"grant_types":   []string{"client_credentials"},
		}))
		Expect(rec).To(SatisfyAll(
			HaveHTTPStatus(http.StatusBadRequest),
			HaveHTTPBody(ContainSubstring(oauthErrorInvalidClientMetadata)),
		))

		rec = performOAuthJSONRequest(routes.handleRegister, http.MethodPost, "/oauth/register", oauthJSONBody(map[string]any{
			"redirect_uris":  []string{"http://127.0.0.1:49152/callback"},
			"response_types": []string{"token"},
			"grant_types":    []string{string(fosite.GrantTypeAuthorizationCode)},
			"client_name":    "Unsupported Response",
		}))
		Expect(rec).To(SatisfyAll(
			HaveHTTPStatus(http.StatusBadRequest),
			HaveHTTPBody(ContainSubstring(oauthErrorInvalidClientMetadata)),
		))

		rec = performOAuthJSONRequest(routes.handleRegister, http.MethodPost, "/oauth/register", oauthJSONBody(map[string]any{
			"redirect_uris": []string{"http://127.0.0.1:49152/callback"},
			"scope":         ScopeMCP + " other",
		}))
		Expect(rec).To(SatisfyAll(
			HaveHTTPStatus(http.StatusBadRequest),
			HaveHTTPBody(ContainSubstring(oauthErrorInvalidClientMetadata)),
		))

		brokenService, err := NewService(ServiceConfig{})
		Expect(err).NotTo(HaveOccurred())
		brokenService.store = nil
		rec = performOAuthJSONRequest(NewRoutes(brokenService).handleRegister, http.MethodPost, "/oauth/register", oauthJSONBody(map[string]any{
			"redirect_uris": []string{"http://127.0.0.1:49152/callback"},
		}))
		Expect(rec).To(SatisfyAll(
			HaveHTTPStatus(http.StatusInternalServerError),
			HaveHTTPBody(ContainSubstring(oauthErrorServerError)),
		))
	})

	It("covers approval cleanup and malformed form parsing", func() {
		service, err := NewService(ServiceConfig{})
		Expect(err).NotTo(HaveOccurred())
		userID := coreauth.UserIDFromString("approval-user")
		service.approvals["expired"] = oauthApproval{
			UserID:     userID,
			RequestKey: "expired",
			ExpiresAt:  time.Now().Add(-time.Minute),
		}
		service.approvals["active"] = oauthApproval{
			UserID:     userID,
			RequestKey: "active",
			ExpiresAt:  time.Now().Add(time.Minute),
		}

		token, err := service.issueApproval(userID, "fresh", approvalPageData{ClientID: ClientID})
		Expect(err).NotTo(HaveOccurred())
		Expect(token).NotTo(BeEmpty())
		Expect(service.approvals).NotTo(HaveKey("expired"))
		Expect(service.approvals).To(HaveKey("active"))
		Expect(service.approvals).To(HaveKey(token))

		badApprovalReq := httptest.NewRequest(http.MethodGet, "/oauth/authorize", nil)
		badApprovalReq.URL.RawQuery = "%"
		_, _, err = authorizeApprovalValues(badApprovalReq)
		Expect(err).To(HaveOccurred())

		badResourceReq := httptest.NewRequest(http.MethodGet, "/oauth/authorize", nil)
		badResourceReq.URL.RawQuery = "%"
		Expect(validateAuthorizeResource(badResourceReq, "")).To(HaveOccurred())
	})
})
