package oauth

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"time"

	sdkauth "github.com/modelcontextprotocol/go-sdk/auth"
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/types"
	"github.com/ory/fosite"
	coreauth "github.com/perber/wiki/internal/core/auth"
)

var _ = ginkgo.Describe("OAuth helper contracts", func() {
	ginkgo.It("derives base-path-aware metadata paths and URLs", ginkgo.Label("unit"), func() {
		Expect(AuthorizationServerMetadataPath("/wiki")).To(Equal("/.well-known/oauth-authorization-server/wiki"))
		Expect(AuthorizationServerMetadataPaths("/wiki")).To(Equal([]string{"/.well-known/oauth-authorization-server", "/.well-known/oauth-authorization-server/wiki"}))
		Expect(ProtectedResourceMetadataPaths("/wiki")).To(Equal([]string{"/.well-known/oauth-protected-resource", "/.well-known/oauth-protected-resource/mcp", "/.well-known/oauth-protected-resource/wiki/mcp"}))

		req := httptest.NewRequest(http.MethodGet, "http://leafwiki.test/wiki/oauth/authorize?state=abc", nil)
		req.Header.Set("X-Forwarded-Proto", "https")
		Expect(requestOrigin(req)).To(Equal("http://leafwiki.test"))
		Expect(IssuerURL(req, "/wiki")).To(Equal("http://leafwiki.test/wiki"))
		Expect(MCPResourceURL(req, "/wiki")).To(Equal("http://leafwiki.test/wiki/mcp"))
		Expect(ProtectedResourceMetadataURL(req, "/wiki")).To(Equal("http://leafwiki.test/.well-known/oauth-protected-resource/wiki/mcp"))

		req = httptest.NewRequest(http.MethodGet, "/wiki/oauth/authorize?state=abc", nil)
		req.Host = "leafwiki.test"
		req.Header.Set("X-Forwarded-Proto", "https")
		Expect(requestOrigin(req)).To(Equal("https://leafwiki.test"))
		Expect(absoluteRequestURL(req)).To(Equal("https://leafwiki.test/wiki/oauth/authorize?state=abc"))
	})

	ginkgo.It("applies LeafWiki defaults and rejects unsupported dynamic registration values", ginkgo.Label("unit"), func() {
		redirects, err := normalizeRedirectURIs([]string{" http://127.0.0.1:49152/callback "})
		Expect(err).To(Succeed())
		Expect(redirects).To(Equal([]string{"http://127.0.0.1:49152/callback"}))
		_, err = normalizeRedirectURIs(nil)
		Expect(err).To(matchOAuthErrorIs(ErrOAuthRedirectURIsRequired))

		grants, err := normalizeRegistrationGrantTypes(nil)
		Expect(err).To(Succeed())
		Expect(grants).To(Equal([]string{"authorization_code", "refresh_token"}))
		_, err = normalizeRegistrationGrantTypes([]string{"client_credentials"})
		Expect(err).To(matchOAuthErrorIs(ErrOAuthUnsupportedGrantType))
		grants, err = normalizeRegistrationGrantTypes([]string{string(fosite.GrantTypeRefreshToken), string(fosite.GrantTypeAuthorizationCode)})
		Expect(err).To(Succeed())
		Expect(grants).To(Equal([]string{"authorization_code", "refresh_token"}))
		grants, err = normalizeRegistrationGrantTypes([]string{string(fosite.GrantTypeAuthorizationCode)})
		Expect(err).To(Succeed())
		Expect(grants).To(Equal([]string{"authorization_code"}))

		responses, err := normalizeRegistrationResponseTypes(nil)
		Expect(err).To(Succeed())
		Expect(responses).To(Equal([]string{"code"}))
		responses, err = normalizeRegistrationResponseTypes([]string{"code", "code"})
		Expect(err).To(Succeed())
		Expect(responses).To(Equal([]string{"code"}))

		scope, err := normalizeRegistrationScope("  " + ScopeMCP + "  ")
		Expect(err).To(Succeed())
		Expect(scope).To(Equal(ScopeMCP))
		_, err = normalizeRegistrationScope(ScopeMCP + " other")
		Expect(err).To(matchOAuthErrorIs(ErrOAuthUnsupportedScope))

		Expect(validateLoopbackRedirectURI("http://localhost:49152/callback")).To(Succeed())
		Expect(validateLoopbackRedirectURI("http://[::1]:49152/callback")).To(Succeed())
		Expect(validateLoopbackRedirectURI("://bad")).To(matchOAuthErrorIs(ErrOAuthRedirectURIInvalid))
		Expect(validateLoopbackRedirectURI("https://127.0.0.1:49152/callback")).To(matchOAuthErrorIs(ErrOAuthRedirectURIMustUseHTTP))
		Expect(validateLoopbackRedirectURI("http://127.0.0.1/callback")).To(matchOAuthErrorIs(ErrOAuthRedirectURIPortRequired))
		Expect(validateLoopbackRedirectURI("http://127.0.0.1:49152/callback#fragment")).To(matchOAuthErrorIs(ErrOAuthRedirectURIHasFragment))
		Expect(validateLoopbackRedirectURI("http://example.com:49152/callback")).To(matchOAuthErrorIs(ErrOAuthRedirectURINotLoopback))
	})

	ginkgo.It("decides registered-client redirect and scope permissions", ginkgo.Label("unit"), func() {
		openClient := registeredClient{}
		restrictedClient := registeredClient{
			RedirectURIs: []string{"http://127.0.0.1:49152/callback"},
			Scope:        ScopeMCP,
		}

		Expect(openClient).To(allowOAuthRedirect("http://127.0.0.1:49153/callback"))
		Expect(restrictedClient).To(allowOAuthRedirect("http://127.0.0.1:49152/callback"))
		Expect(restrictedClient).To(rejectOAuthRedirect("http://127.0.0.1:49153/callback"))
		Expect(openClient).To(allowOAuthScope(""))
		Expect(openClient).To(allowOAuthScope(ScopeMCP))
		Expect(restrictedClient).To(allowOAuthScope(ScopeMCP))
		Expect(restrictedClient).To(rejectOAuthScope(ScopeMCP + " other"))
		Expect([]string{ScopeMCP}).To(containOAuthValue(ScopeMCP))
		Expect([]string{ScopeMCP}).To(missOAuthValue("other"))
	})

	ginkgo.It("issues inspectable approval tokens for the owning user and consumes them once", ginkgo.Label("unit"), func() {
		withOAuthRandomBytes(9)
		userID := newFixtureOAuthUserID("approval-user")
		requestKey := "client_id=leafwiki-local-mcp&response_type=code"
		details := approvalPageData{
			ClientLabel: "LeafWiki local MCP",
			ClientID:    ClientID,
			RedirectURI: "http://127.0.0.1:49152/callback",
			Scope:       ScopeMCP,
			Resource:    "http://leafwiki.test/mcp",
		}
		service := &Service{
			approvals: map[string]oauthApproval{
				"expired-approval": {
					UserID:     userID,
					RequestKey: requestKey,
					Details:    details,
					ExpiresAt:  time.Now().Add(-time.Minute),
				},
			},
		}

		token, err := service.issueApproval(userID, requestKey, details)
		Expect(err).To(Succeed())

		Expect(service).To(haveIssuedApprovalDetails(token, userID, details))
		Expect(service.approvals).NotTo(HaveKey("expired-approval"))
		Expect(service).To(consumeApprovalOnce(token, userID, requestKey))
		Expect(service).To(missIssuedApprovalDetails(token, userID))
	})

	ginkgo.It("revokes access and refresh sessions by request ID", ginkgo.Label("integration"), func() {
		ctx := context.Background()
		store := newFositeStore()
		requester := newStoreTestRequester("revoke-request")

		Expect(store.CreateAccessTokenSession(ctx, "access-revoke", requester)).To(Succeed())
		Expect(store.CreateRefreshTokenSession(ctx, "refresh-revoke", "access-revoke", requester)).To(Succeed())
		Expect(store.RevokeRefreshToken(ctx, "revoke-request")).To(Succeed())
		revokedRefresh, err := store.GetRefreshTokenSession(ctx, "refresh-revoke", newFositeSession("", ""))
		Expect(err).To(MatchError(fosite.ErrInactiveToken))
		Expect(revokedRefresh.GetID()).To(Equal("revoke-request"))
		Expect(store.RevokeAccessToken(ctx, "revoke-request")).To(Succeed())
		_, err = store.GetAccessTokenSession(ctx, "access-revoke", newFositeSession("", ""))
		Expect(err).To(MatchError(fosite.ErrNotFound))
	})
})

func oauthTempDir() string {
	ginkgo.GinkgoHelper()
	dir, err := os.MkdirTemp("", "leafwiki-oauth-*")
	Expect(err).NotTo(HaveOccurred())
	ginkgo.DeferCleanup(os.RemoveAll, dir)
	return dir
}

func matchOAuthErrorIs(target error) types.GomegaMatcher {
	ginkgo.GinkgoHelper()
	return Satisfy(func(err error) bool {
		return errors.Is(err, target)
	})
}

func matchOAuthInvalidTokenError() types.GomegaMatcher {
	ginkgo.GinkgoHelper()
	return matchOAuthErrorIs(sdkauth.ErrInvalidToken)
}

func matchMalformedOAuthQuery() types.GomegaMatcher {
	ginkgo.GinkgoHelper()
	return Satisfy(func(err error) bool {
		var escapeErr url.EscapeError
		return errors.As(err, &escapeErr)
	})
}

type oauthPermissionDecision uint8

const (
	oauthPermissionRejected oauthPermissionDecision = iota
	oauthPermissionAllowed
)

type oauthApprovalLookupState uint8

const (
	oauthApprovalMissing oauthApprovalLookupState = iota
	oauthApprovalFound
)

type oauthApprovalConsumptionState uint8

const (
	oauthApprovalRejected oauthApprovalConsumptionState = iota
	oauthApprovalAcceptedOnce
)

func allowOAuthRedirect(redirectURI string) types.GomegaMatcher {
	ginkgo.GinkgoHelper()
	return WithTransform(func(client registeredClient) oauthPermissionDecision {
		return oauthPermissionDecisionFor(clientRedirectURIAllowed(client, redirectURI))
	}, Equal(oauthPermissionAllowed))
}

func rejectOAuthRedirect(redirectURI string) types.GomegaMatcher {
	ginkgo.GinkgoHelper()
	return WithTransform(func(client registeredClient) oauthPermissionDecision {
		return oauthPermissionDecisionFor(clientRedirectURIAllowed(client, redirectURI))
	}, Equal(oauthPermissionRejected))
}

func allowOAuthScope(scope string) types.GomegaMatcher {
	ginkgo.GinkgoHelper()
	return WithTransform(func(client registeredClient) oauthPermissionDecision {
		return oauthPermissionDecisionFor(clientScopeAllowed(client, scope))
	}, Equal(oauthPermissionAllowed))
}

func rejectOAuthScope(scope string) types.GomegaMatcher {
	ginkgo.GinkgoHelper()
	return WithTransform(func(client registeredClient) oauthPermissionDecision {
		return oauthPermissionDecisionFor(clientScopeAllowed(client, scope))
	}, Equal(oauthPermissionRejected))
}

func containOAuthValue(value string) types.GomegaMatcher {
	ginkgo.GinkgoHelper()
	return WithTransform(func(values []string) oauthPermissionDecision {
		return oauthPermissionDecisionFor(stringSliceContains(values, value))
	}, Equal(oauthPermissionAllowed))
}

func missOAuthValue(value string) types.GomegaMatcher {
	ginkgo.GinkgoHelper()
	return WithTransform(func(values []string) oauthPermissionDecision {
		return oauthPermissionDecisionFor(stringSliceContains(values, value))
	}, Equal(oauthPermissionRejected))
}

func oauthPermissionDecisionFor(allowed bool) oauthPermissionDecision {
	if allowed {
		return oauthPermissionAllowed
	}
	return oauthPermissionRejected
}

func haveIssuedApprovalDetails(token string, userID coreauth.UserID, details approvalPageData) types.GomegaMatcher {
	ginkgo.GinkgoHelper()
	return WithTransform(func(service *Service) oauthApprovalLookup {
		return oauthApprovalLookupFor(service, token, userID)
	}, Equal(oauthApprovalLookup{State: oauthApprovalFound, Details: details}))
}

func missIssuedApprovalDetails(token string, userID coreauth.UserID) types.GomegaMatcher {
	ginkgo.GinkgoHelper()
	return WithTransform(func(service *Service) oauthApprovalLookup {
		return oauthApprovalLookupFor(service, token, userID)
	}, Equal(oauthApprovalLookup{State: oauthApprovalMissing}))
}

func consumeApprovalOnce(token string, userID coreauth.UserID, requestKey string) types.GomegaMatcher {
	ginkgo.GinkgoHelper()
	return WithTransform(func(service *Service) oauthApprovalConsumptionState {
		if service.consumeApproval(" "+token+" ", userID, requestKey) && !service.consumeApproval(token, userID, requestKey) {
			return oauthApprovalAcceptedOnce
		}
		return oauthApprovalRejected
	}, Equal(oauthApprovalAcceptedOnce))
}

type oauthApprovalLookup struct {
	State   oauthApprovalLookupState
	Details approvalPageData
}

func oauthApprovalLookupFor(service *Service, token string, userID coreauth.UserID) oauthApprovalLookup {
	details, found := service.approvalDetails(" "+token+" ", userID)
	if found {
		return oauthApprovalLookup{State: oauthApprovalFound, Details: details}
	}
	return oauthApprovalLookup{State: oauthApprovalMissing}
}
