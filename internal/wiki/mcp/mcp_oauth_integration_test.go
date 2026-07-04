package mcp_test

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	sdkauth "github.com/modelcontextprotocol/go-sdk/auth"
	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/types"
	"github.com/ory/fosite"
	"github.com/perber/wiki/internal/core/assets"
	coreauth "github.com/perber/wiki/internal/core/auth"
	sharederrors "github.com/perber/wiki/internal/core/shared/errors"
	httpinternal "github.com/perber/wiki/internal/http"
	authmw "github.com/perber/wiki/internal/http/middleware/auth"
	testmatchers "github.com/perber/wiki/internal/test_utils/matchers"
	"github.com/perber/wiki/internal/wiki"
	wikimcp "github.com/perber/wiki/internal/wiki/mcp"
	xoauth2 "golang.org/x/oauth2"
)

const (
	oauthClientID = "leafwiki-local-mcp"
	oauthScope    = "leafwiki:mcp"
)

type oauthErrorField string

const oauthErrorInvalidClientMetadata oauthErrorField = "invalid_client_metadata"

type oauthMetadataCase struct {
	basePath          string
	authMetadataPaths []string
	prMetadataPaths   []string
	issuer            string
	resource          string
}

var _ = DescribeTable("LocalMCPOAuthMetadata", Label("integration"),
	func(tc oauthMetadataCase) {
		w := newLocalMCPAuthTestWiki()
		router := newLocalMCPTestRouter(w, httpinternal.RouterOptions{
			AllowInsecure:           true,
			BasePath:                tc.basePath,
			AccessTokenTimeout:      15 * time.Minute,
			RefreshTokenTimeout:     7 * 24 * time.Hour,
			MaxAssetUploadSizeBytes: assets.DefaultMaxUploadSizeBytes,
			MCPEnabled:              true,
		})

		for _, path := range tc.authMetadataPaths {
			authMeta := getJSONMap(router, "http://leafwiki.local"+path)
			Expect(authMeta).To(HaveKeyWithValue("issuer", tc.issuer))
			Expect(authMeta).To(HaveKeyWithValue("authorization_endpoint", tc.issuer+"/oauth/authorize"))
			Expect(authMeta).To(HaveKeyWithValue("token_endpoint", tc.issuer+"/oauth/token"))
			Expect(authMeta).To(HaveKeyWithValue("registration_endpoint", tc.issuer+"/oauth/register"))
			Expect(authMeta).To(HaveKeyWithValue("response_types_supported", HaveExactElements("code")))
			Expect(authMeta).To(HaveKeyWithValue("grant_types_supported", HaveExactElements("authorization_code", "refresh_token")))
			Expect(authMeta).To(HaveKeyWithValue("code_challenge_methods_supported", HaveExactElements("S256")))
			Expect(authMeta).To(HaveKeyWithValue("scopes_supported", HaveExactElements(oauthScope)))
			Expect(authMeta).To(HaveKeyWithValue("token_endpoint_auth_methods_supported", HaveExactElements("none")))
			Expect(authMeta).NotTo(HaveKey("revocation_endpoint"))
			Expect(authMeta).NotTo(HaveKey("introspection_endpoint"))
		}

		for _, path := range tc.prMetadataPaths {
			rec := performRequest(router, http.MethodGet, "http://leafwiki.local"+path, nil, nil)
			Expect(rec.Header().Get("Content-Type")).To(HavePrefix("application/json"), "protected resource metadata should be JSON for %s", path)
			prMeta := decodeJSONResponse(rec, http.StatusOK)
			Expect(prMeta).To(HaveKeyWithValue("resource", tc.resource))
			Expect(prMeta).To(HaveKeyWithValue("authorization_servers", HaveExactElements(tc.issuer)))
			Expect(prMeta).To(HaveKeyWithValue("scopes_supported", HaveExactElements(oauthScope)))

			optionsRec := performRequest(router, http.MethodOptions, "http://leafwiki.local"+path, nil, nil)
			Expect(optionsRec).To(HaveHTTPStatus(http.StatusNoContent), optionsRec.Body.String())
		}
	},
	Entry(
		"root",
		oauthMetadataCase{
			basePath:          "",
			authMetadataPaths: []string{"/.well-known/oauth-authorization-server"},
			prMetadataPaths:   []string{"/.well-known/oauth-protected-resource", "/.well-known/oauth-protected-resource/mcp"},
			issuer:            "http://leafwiki.local",
			resource:          "http://leafwiki.local/mcp",
		},
	),
	Entry(
		"base path",
		oauthMetadataCase{
			basePath:          "/wiki",
			authMetadataPaths: []string{"/.well-known/oauth-authorization-server", "/.well-known/oauth-authorization-server/wiki"},
			prMetadataPaths:   []string{"/.well-known/oauth-protected-resource", "/.well-known/oauth-protected-resource/mcp", "/.well-known/oauth-protected-resource/wiki/mcp"},
			issuer:            "http://leafwiki.local/wiki",
			resource:          "http://leafwiki.local/wiki/mcp",
		},
	),
)

var _ = DescribeTable("LocalMCPOAuthDynamicClientRegistration invalid registrations", Label("integration"),
	func(body string) {
		w := newLocalMCPAuthTestWiki()
		router := newLocalMCPTestRouter(w, oauthRouterOptions(""))

		rec := performJSON(router, "http://leafwiki.local/oauth/register", body)
		payload := decodeJSONResponse(rec, http.StatusBadRequest)
		Expect(payload).To(matchOAuthErrorField(oauthErrorInvalidClientMetadata))
	},
	Entry("missing redirect uris", `{"token_endpoint_auth_method":"none"}`),
	Entry("non loopback redirect uri", `{"redirect_uris":["http://example.com/callback"],"token_endpoint_auth_method":"none"}`),
	Entry("confidential token auth method", `{"redirect_uris":["http://127.0.0.1:49152/callback"],"token_endpoint_auth_method":"client_secret_basic"}`),
	Entry("client secret", `{"redirect_uris":["http://127.0.0.1:49152/callback"],"token_endpoint_auth_method":"none","client_secret":"secret"}`),
	Entry("unsupported grant", `{"redirect_uris":["http://127.0.0.1:49152/callback"],"grant_types":["client_credentials"],"token_endpoint_auth_method":"none"}`),
	Entry("unsupported response type", `{"redirect_uris":["http://127.0.0.1:49152/callback"],"response_types":["token"],"token_endpoint_auth_method":"none"}`),
	Entry("unsupported scope", `{"redirect_uris":["http://127.0.0.1:49152/callback"],"scope":"leafwiki:mcp other","token_endpoint_auth_method":"none"}`),
)

var _ = Describe("OAuth dynamic client registration", Label("integration"), func() {
	It("registers loopback public clients and exchanges authorization codes", func() {
		w := newLocalMCPAuthTestWiki()
		router := newLocalMCPTestRouter(w, oauthRouterOptions(""))

		registration := registerOAuthClient(router, "", `{
		"client_name":"codex",
		"redirect_uris":["http://127.0.0.1:49152/callback"],
		"grant_types":["authorization_code","refresh_token"],
		"response_types":["code"],
		"token_endpoint_auth_method":"none",
		"scope":"leafwiki:mcp"
	}`)
		clientID := stringFromMap(registration, "client_id")
		Expect(clientID).To(SatisfyAll(Not(BeEmpty()), Not(Equal(oauthClientID))))
		Expect(registration).To(HaveKeyWithValue("redirect_uris", HaveExactElements("http://127.0.0.1:49152/callback")))
		Expect(registration).To(HaveKeyWithValue("token_endpoint_auth_method", "none"))
		Expect(registration).To(HaveKeyWithValue("grant_types", HaveExactElements("authorization_code", "refresh_token")))
		Expect(registration).To(HaveKeyWithValue("response_types", HaveExactElements("code")))
		Expect(registration).To(HaveKeyWithValue("scope", oauthScope))

		cookies := loginCookies(router, "admin", "admin")
		redirectURI := "http://127.0.0.1:49152/callback"
		verifier := "oauth-dynamic-client-verifier-abcdefghijklmnopqrstuvwxyz0123456789"

		mismatchQ := validAuthorizeQueryForClient(clientID, "http://127.0.0.1:49153/callback", "dynamic-client-mismatch-state", pkceS256(verifier), "http://leafwiki.local/mcp")
		mismatch := performRequest(router, http.MethodGet, "http://leafwiki.local/oauth/authorize?"+mismatchQ.Encode(), cookies, nil)
		Expect(mismatch).To(HaveHTTPStatus(http.StatusBadRequest), mismatch.Body.String())

		q := validAuthorizeQueryForClient(clientID, redirectURI, "dynamic-client-state", pkceS256(verifier), "http://leafwiki.local/mcp")
		rec := performRequest(router, http.MethodGet, "http://leafwiki.local/oauth/authorize?"+q.Encode(), cookies, nil)
		form := approvalFormFromAuthorizeRedirect(rec, "")
		approved := performFormWithCookiesAndHeaders(router, "http://leafwiki.local/oauth/authorize", form, cookies, nil)
		Expect(approved).To(HaveHTTPStatus(http.StatusFound), approved.Body.String())
		redirected, err := url.Parse(approved.Header().Get("Location"))
		Expect(err).NotTo(HaveOccurred())
		Expect(redirected.Query().Get("state")).To(Equal("dynamic-client-state"))
		code := redirected.Query().Get("code")
		Expect(code).NotTo(BeEmpty(), "dynamic client authorize redirect should include code: %s", redirected.String())

		token := exchangeCodeForClient(router, "", clientID, code, redirectURI, verifier)
		Expect(token).To(HaveKeyWithValue("token_type", "Bearer"))
		Expect(token).To(HaveKeyWithValue("scope", oauthScope))
		Expect(token).To(SatisfyAll(
			HaveKeyWithValue("access_token", Not(BeEmpty())),
			HaveKeyWithValue("refresh_token", Not(BeEmpty())),
		))
	})
})

var _ = Describe("OAuth dynamic client registration defaults", Label("integration"), func() {
	It("adds refresh grants and binds refresh tokens to the registered client", func() {
		w := newLocalMCPAuthTestWiki()
		router := newLocalMCPTestRouter(w, oauthRouterOptions(""))
		redirectURI := "http://127.0.0.1:49152/callback"

		registration := registerOAuthClient(router, "", `{
		"client_name":"Codex CLI",
		"redirect_uris":["http://127.0.0.1:49152/callback"],
		"response_types":["code"],
		"token_endpoint_auth_method":"none",
		"scope":"leafwiki:mcp"
	}`)
		clientID := stringFromMap(registration, "client_id")
		Expect(registration).To(HaveKeyWithValue("grant_types", HaveExactElements("authorization_code", "refresh_token")))

		cookies := loginCookies(router, "admin", "admin")
		verifier := "oauth-dcr-default-refresh-verifier-abcdefghijklmnopqrstuvwxyz0123456789"
		q := validAuthorizeQueryForClient(clientID, redirectURI, "dcr-default-refresh-state", pkceS256(verifier), "http://leafwiki.local/mcp")
		rec := performRequest(router, http.MethodGet, "http://leafwiki.local/oauth/authorize?"+q.Encode(), cookies, nil)
		form := approvalFormFromAuthorizeRedirect(rec, "")
		details := approvalDetails(router, "", form.Get("approval_token"), cookies, nil)
		Expect(details).To(HaveKeyWithValue("clientLabel", "Codex CLI"))
		Expect(details).To(HaveKeyWithValue("clientId", clientID))
		Expect(details).To(HaveKeyWithValue("redirectUri", redirectURI))
		Expect(details).To(HaveKeyWithValue("scope", oauthScope))
		Expect(details).To(HaveKeyWithValue("resource", "http://leafwiki.local/mcp"))

		approved := performFormWithCookiesAndHeaders(router, "http://leafwiki.local/oauth/authorize", form, cookies, nil)
		Expect(approved).To(HaveHTTPStatus(http.StatusFound), approved.Body.String())
		redirected, err := url.Parse(approved.Header().Get("Location"))
		Expect(err).NotTo(HaveOccurred())
		code := redirected.Query().Get("code")
		Expect(code).NotTo(BeEmpty(), "dynamic default client authorize redirect should include code: %s", redirected.String())
		token := exchangeCodeForClient(router, "", clientID, code, redirectURI, verifier)
		refreshToken := stringFromMap(token, "refresh_token")
		Expect(refreshToken).NotTo(BeEmpty())

		wrongClientRefresh := performForm(router, "http://leafwiki.local/oauth/token", url.Values{
			"grant_type":    {"refresh_token"},
			"client_id":     {oauthClientID},
			"refresh_token": {refreshToken},
		})
		wrongClientError := decodeJSONResponse(wrongClientRefresh, http.StatusUnauthorized)
		Expect(wrongClientError).To(matchOAuthErrorField(oauthErrorField(fosite.ErrInvalidGrant.ErrorField)))

		refreshed := decodeJSONResponse(performForm(router, "http://leafwiki.local/oauth/token", url.Values{
			"grant_type":    {"refresh_token"},
			"client_id":     {clientID},
			"refresh_token": {refreshToken},
		}), http.StatusOK)
		Expect(refreshed).To(HaveKeyWithValue("access_token", Not(BeEmpty())))
	})
})

var _ = Describe("OAuth dynamic client registration scopes", Label("integration"), func() {
	It("allows clients without a stored scope to request the advertised scope", func() {
		w := newLocalMCPAuthTestWiki()
		router := newLocalMCPTestRouter(w, oauthRouterOptions(""))
		redirectURI := "http://127.0.0.1:49152/callback"

		registration := registerOAuthClient(router, "", `{
		"client_name":"SDK style client",
		"redirect_uris":["http://127.0.0.1:49152/callback"],
		"grant_types":["authorization_code","refresh_token"],
		"response_types":["code"],
		"token_endpoint_auth_method":"none"
	}`)
		clientID := stringFromMap(registration, "client_id")
		Expect(registration).NotTo(HaveKey("scope"))

		cookies := loginCookies(router, "admin", "admin")
		verifier := "oauth-dcr-omitted-scope-verifier-abcdefghijklmnopqrstuvwxyz0123456789"
		q := validAuthorizeQueryForClient(clientID, redirectURI, "dcr-omitted-scope-state", pkceS256(verifier), "http://leafwiki.local/mcp")
		rec := performRequest(router, http.MethodGet, "http://leafwiki.local/oauth/authorize?"+q.Encode(), cookies, nil)
		form := approvalFormFromAuthorizeRedirect(rec, "")
		approved := performFormWithCookiesAndHeaders(router, "http://leafwiki.local/oauth/authorize", form, cookies, nil)
		Expect(approved).To(HaveHTTPStatus(http.StatusFound), approved.Body.String())
		redirected, err := url.Parse(approved.Header().Get("Location"))
		Expect(err).NotTo(HaveOccurred())
		Expect(redirected.Query().Get("state")).To(Equal("dcr-omitted-scope-state"))
		code := redirected.Query().Get("code")
		Expect(code).NotTo(BeEmpty(), "omitted-scope DCR authorize redirect should include code: %s", redirected.String())

		token := exchangeCodeForClient(router, "", clientID, code, redirectURI, verifier)
		Expect(token).To(HaveKeyWithValue("scope", oauthScope))
		Expect(token).To(SatisfyAll(
			HaveKeyWithValue("access_token", Not(BeEmpty())),
			HaveKeyWithValue("refresh_token", Not(BeEmpty())),
		))
	})
})

var _ = Describe("OAuth dynamic client registration grant restrictions", Label("integration"), func() {
	It("omits refresh tokens for authorization-code-only clients", func() {
		w := newLocalMCPAuthTestWiki()
		router := newLocalMCPTestRouter(w, oauthRouterOptions(""))
		redirectURI := "http://127.0.0.1:49152/callback"

		registration := registerOAuthClient(router, "", `{
		"client_name":"Auth Code Only Client",
		"redirect_uris":["http://127.0.0.1:49152/callback"],
		"grant_types":["authorization_code"],
		"response_types":["code"],
		"token_endpoint_auth_method":"none",
		"scope":"leafwiki:mcp"
	}`)
		clientID := stringFromMap(registration, "client_id")
		Expect(registration).To(HaveKeyWithValue("grant_types", HaveExactElements("authorization_code")))

		cookies := loginCookies(router, "admin", "admin")
		verifier := "oauth-dcr-auth-code-only-verifier-abcdefghijklmnopqrstuvwxyz0123456789"
		q := validAuthorizeQueryForClient(clientID, redirectURI, "dcr-auth-code-only-state", pkceS256(verifier), "http://leafwiki.local/mcp")
		rec := performRequest(router, http.MethodGet, "http://leafwiki.local/oauth/authorize?"+q.Encode(), cookies, nil)
		form := approvalFormFromAuthorizeRedirect(rec, "")
		approved := performFormWithCookiesAndHeaders(router, "http://leafwiki.local/oauth/authorize", form, cookies, nil)
		Expect(approved).To(HaveHTTPStatus(http.StatusFound), approved.Body.String())
		redirected, err := url.Parse(approved.Header().Get("Location"))
		Expect(err).NotTo(HaveOccurred())
		code := redirected.Query().Get("code")
		Expect(code).NotTo(BeEmpty(), "auth-code-only authorize redirect should include code: %s", redirected.String())
		token := exchangeCodeForClient(router, "", clientID, code, redirectURI, verifier)
		Expect(token).To(HaveKeyWithValue("access_token", Not(BeEmpty())))
		Expect(token).NotTo(HaveKey("refresh_token"))
	})
})

var _ = DescribeTable("LocalMCPOAuthAuthorizeValidationAndLoginRedirect bad requests", Label("integration"),
	func(override func(url.Values)) {
		w := newLocalMCPAuthTestWiki()
		router := newLocalMCPTestRouter(w, oauthRouterOptions(""))
		verifier := "oauth-test-verifier-abcdefghijklmnopqrstuvwxyz0123456789"
		q := validAuthorizeQuery("http://localhost:49152/callback", "state-1", pkceS256(verifier), "http://leafwiki.local/mcp")
		override(q)

		rec := performRequest(router, http.MethodGet, "http://leafwiki.local/oauth/authorize?"+q.Encode(), nil, nil)

		Expect(rec).To(HaveHTTPStatus(http.StatusBadRequest), rec.Body.String())
	},
	Entry("unknown client", func(q url.Values) { q.Set("client_id", "unknown-client") }),
	Entry("non loopback redirect", func(q url.Values) { q.Set("redirect_uri", "http://example.com/callback") }),
	Entry("unsupported loopback redirect", func(q url.Values) { q.Set("redirect_uri", "http://127.0.0.2:49152/callback") }),
	Entry("redirect fragment", func(q url.Values) { q.Set("redirect_uri", "http://localhost:49152/callback#frag") }),
)

var _ = DescribeTable("LocalMCPOAuthAuthorizeValidationAndLoginRedirect redirect errors", Label("integration"),
	func(override func(url.Values), wantError *fosite.RFC6749Error, wantState string) {
		w := newLocalMCPAuthTestWiki()
		router := newLocalMCPTestRouter(w, oauthRouterOptions(""))
		verifier := "oauth-test-verifier-abcdefghijklmnopqrstuvwxyz0123456789"
		validRedirect := "http://localhost:49152/callback"
		q := validAuthorizeQuery(validRedirect, "redirect-error-state", pkceS256(verifier), "http://leafwiki.local/mcp")
		override(q)

		rec := performRequest(router, http.MethodGet, "http://leafwiki.local/oauth/authorize?"+q.Encode(), nil, nil)

		Expect(rec).To(HaveHTTPStatus(http.StatusFound), rec.Body.String())
		redirected, err := url.Parse(rec.Header().Get("Location"))
		Expect(err).NotTo(HaveOccurred())
		Expect(redirected.Scheme + "://" + redirected.Host + redirected.Path).To(Equal(validRedirect))
		Expect(redirected.Query().Get("state")).To(Equal(wantState))
		Expect(redirected.Query().Get("error")).To(Equal(wantError.ErrorField))
		Expect(redirected.Query().Get("code")).To(BeEmpty())
	},
	Entry("missing state redirects to client", func(q url.Values) { q.Del("state") }, fosite.ErrInvalidState, ""),
	Entry("short state redirects to client", func(q url.Values) { q.Set("state", "short") }, fosite.ErrInvalidState, "short"),
	Entry("missing pkce redirects to client", func(q url.Values) { q.Del("code_challenge") }, fosite.ErrInvalidRequest, "redirect-error-state"),
	Entry("plain pkce redirects to client", func(q url.Values) { q.Set("code_challenge_method", "plain") }, fosite.ErrInvalidRequest, "redirect-error-state"),
	Entry("resource mismatch redirects to client", func(q url.Values) { q.Set("resource", "http://leafwiki.local/not-mcp") }, fosite.ErrInvalidRequest, "redirect-error-state"),
	Entry("mixed duplicate resource redirects to client", func(q url.Values) { q.Add("resource", "http://leafwiki.local/not-mcp") }, fosite.ErrInvalidRequest, "redirect-error-state"),
	Entry("unsupported scope redirects to client", func(q url.Values) { q.Set("scope", "leafwiki:mcp other") }, fosite.ErrInvalidScope, "redirect-error-state"),
)

var _ = DescribeTable("LocalMCPOAuthAuthorizeValidationAndLoginRedirect authenticated approval", Label("integration"),
	func(redirectURI string) {
		w := newLocalMCPAuthTestWiki()
		router := newLocalMCPTestRouter(w, oauthRouterOptions(""))
		verifier := "oauth-test-verifier-abcdefghijklmnopqrstuvwxyz0123456789"
		challenge := pkceS256(verifier)
		cookies := loginCookies(router, "admin", "admin")
		q := validAuthorizeQuery(redirectURI, "roundtrip-state", challenge, "")

		rec := performRequest(router, http.MethodGet, "http://leafwiki.local/oauth/authorize?"+q.Encode(), cookies, nil)
		form := approvalFormFromAuthorizeRedirect(rec, "")
		approved := performFormWithCookiesAndHeaders(router, "http://leafwiki.local/oauth/authorize", form, cookies, nil)

		Expect(approved).To(HaveHTTPStatus(http.StatusFound), approved.Body.String())
		redirected, err := url.Parse(approved.Header().Get("Location"))
		Expect(err).NotTo(HaveOccurred())
		Expect(redirected.Query().Get("state")).To(Equal("roundtrip-state"))
		Expect(redirected.Query().Get("code")).NotTo(BeEmpty(), "authorize redirect should include code: %s", redirected.String())
	},
	Entry("authenticated approval http://localhost:49152/callback", "http://localhost:49152/callback"),
	Entry("authenticated approval http://127.0.0.1:49152/callback", "http://127.0.0.1:49152/callback"),
	Entry("authenticated approval http://[::1]:49152/callback", "http://[::1]:49152/callback"),
)

var _ = Describe("OAuth authorization redirects", Label("integration"), func() {
	It("sends unauthenticated clients through login before approval", func() {
		w := newLocalMCPAuthTestWiki()
		router := newLocalMCPTestRouter(w, oauthRouterOptions(""))
		verifier := "oauth-test-verifier-abcdefghijklmnopqrstuvwxyz0123456789"
		challenge := pkceS256(verifier)
		validRedirect := "http://localhost:49152/callback"

		q := validAuthorizeQuery(validRedirect, "login-state", challenge, "http://leafwiki.local/mcp")
		authorizeURL := "http://leafwiki.local/oauth/authorize?" + q.Encode()
		rec := performRequest(router, http.MethodGet, authorizeURL, nil, nil)
		Expect(rec).To(HaveHTTPStatus(http.StatusFound), rec.Body.String())
		location := rec.Header().Get("Location")
		Expect(location).To(HavePrefix("/login?"))
		loginURL, err := url.Parse(location)
		Expect(err).NotTo(HaveOccurred(), "login redirect should parse: %s", location)
		Expect(loginURL.Query().Get("returnTo")).To(Equal(authorizeURL))
	})
})

var _ = Describe("OAuth authorization with trusted remote users", Label("integration"), func() {
	It("still requires explicit approval before issuing an authorization code", func() {
		w := newLocalMCPAuthTestWiki()
		trustedProxies, err := authmw.ParseTrustedProxies("192.0.2.1")
		Expect(err).NotTo(HaveOccurred())
		opts := oauthRouterOptions("")
		opts.HTTPRemoteUser = httpinternal.HTTPRemoteUserConfig{
			Enabled:        true,
			HeaderName:     "X-Remote-User",
			TrustedProxies: trustedProxies,
			UserService:    w.UserService(),
		}
		router := newLocalMCPTestRouter(w, opts)
		verifier := "oauth-remote-user-verifier-abcdefghijklmnopqrstuvwxyz0123456789"
		q := validAuthorizeQuery("http://localhost:49152/callback", "remote-user-state", pkceS256(verifier), "http://leafwiki.local/mcp")
		headers := map[string]string{"X-Remote-User": "admin"}

		rec := performRequestWithHeaders(router, http.MethodGet, "http://leafwiki.local/oauth/authorize?"+q.Encode(), nil, nil, headers)
		Expect(rec.Header().Get("Location")).NotTo(HavePrefix("/login"))

		form := approvalFormFromAuthorizeRedirect(rec, "")
		approved := performFormWithCookiesAndHeaders(router, "http://leafwiki.local/oauth/authorize", form, nil, headers)
		Expect(approved).To(HaveHTTPStatus(http.StatusFound), approved.Body.String())
		redirected, err := url.Parse(approved.Header().Get("Location"))
		Expect(err).NotTo(HaveOccurred())
		Expect(redirected.Query().Get("state")).To(Equal("remote-user-state"))
		Expect(redirected.Query().Get("code")).NotTo(BeEmpty(), "remote-user authorize redirect should include code: %s", redirected.String())
	})
})

var _ = Describe("OAuth token exchange", Label("integration"), func() {
	It("rejects invalid grants and refreshes valid sessions", func() {
		w := newLocalMCPAuthTestWiki()
		router := newLocalMCPTestRouter(w, oauthRouterOptions(""))
		cookies := loginCookies(router, "admin", "admin")
		redirectURI := "http://localhost:49152/callback"
		verifier := "oauth-token-verifier-abcdefghijklmnopqrstuvwxyz0123456789"
		resource := "http://leafwiki.local/mcp"

		badCode := authorizeCode(router, cookies, redirectURI, "bad-verifier", verifier, resource)
		badForm := url.Values{
			"grant_type":    {"authorization_code"},
			"client_id":     {oauthClientID},
			"redirect_uri":  {redirectURI},
			"code":          {badCode},
			"code_verifier": {"wrong-verifier"},
		}
		badRec := performForm(router, "http://leafwiki.local/oauth/token", badForm)
		Expect(badRec).To(HaveHTTPStatus(http.StatusUnauthorized), badRec.Body.String())
		Expect(badRec.Header().Get("Content-Type")).To(HavePrefix("application/json"))
		badTokenError := decodeJSONResponse(badRec, http.StatusUnauthorized)
		Expect(badTokenError).To(matchOAuthErrorField(oauthErrorField(fosite.ErrInvalidGrant.ErrorField)))

		code := authorizeCode(router, cookies, redirectURI, "token-state", verifier, resource)
		token := exchangeCode(router, code, redirectURI, verifier)
		accessToken := stringFromMap(token, "access_token")
		refreshToken := stringFromMap(token, "refresh_token")
		Expect(token).To(HaveKeyWithValue("token_type", "Bearer"))
		Expect(token).To(HaveKeyWithValue("scope", oauthScope))
		Expect(token).To(SatisfyAll(
			HaveKeyWithValue("access_token", accessToken),
			HaveKeyWithValue("refresh_token", refreshToken),
		))
		Expect(mcpBearerAuthorizationAttempt(router, "/mcp", refreshToken)).To(HaveHTTPStatus(http.StatusUnauthorized))

		refreshForm := url.Values{
			"grant_type":    {"refresh_token"},
			"client_id":     {oauthClientID},
			"refresh_token": {refreshToken},
		}
		refreshed := decodeJSONResponse(performForm(router, "http://leafwiki.local/oauth/token", refreshForm), http.StatusOK)
		refreshedAccessToken := stringFromMap(refreshed, "access_token")
		refreshedRefreshToken := stringFromMap(refreshed, "refresh_token")
		Expect(refreshed).To(SatisfyAll(
			HaveKeyWithValue("access_token", refreshedAccessToken),
			HaveKeyWithValue("refresh_token", refreshedRefreshToken),
		))
		Expect(refreshedRefreshToken).NotTo(Equal(refreshToken))
		Expect(mcpBearerAuthorizationAttempt(router, "/mcp", accessToken)).To(HaveHTTPStatus(http.StatusUnauthorized))
		refreshedSession := connectLocalMCPWithToken(router, "/mcp", refreshedAccessToken)
		current := callToolStructured(refreshedSession, "wiki_get_current_user", nil)
		user := nestedMap(current, "user")
		Expect(user).To(HaveKeyWithValue("username", "admin"))

		secondRefresh := decodeJSONResponse(performForm(router, "http://leafwiki.local/oauth/token", url.Values{
			"grant_type":    {"refresh_token"},
			"client_id":     {oauthClientID},
			"refresh_token": {refreshedRefreshToken},
		}), http.StatusOK)
		Expect(secondRefresh).To(HaveKeyWithValue("access_token", Not(BeEmpty())))

		reusedRefreshError := decodeJSONResponse(performForm(router, "http://leafwiki.local/oauth/token", refreshForm), http.StatusUnauthorized)
		Expect(reusedRefreshError).To(matchOAuthErrorField(oauthErrorField(fosite.ErrInvalidGrant.ErrorField)))

		reuseVerifier := "oauth-code-reuse-verifier-abcdefghijklmnopqrstuvwxyz0123456789"
		reuseCode := authorizeCode(router, cookies, redirectURI, "code-reuse-state", reuseVerifier, resource)
		reuseToken := exchangeCode(router, reuseCode, redirectURI, reuseVerifier)
		reuseAccessToken := stringFromMap(reuseToken, "access_token")
		reuseRefreshToken := stringFromMap(reuseToken, "refresh_token")
		reuseCodeError := decodeJSONResponse(performForm(router, "http://leafwiki.local/oauth/token", url.Values{
			"grant_type":    {"authorization_code"},
			"client_id":     {oauthClientID},
			"redirect_uri":  {redirectURI},
			"code":          {reuseCode},
			"code_verifier": {reuseVerifier},
		}), http.StatusUnauthorized)
		Expect(reuseCodeError).To(matchOAuthErrorField(oauthErrorField(fosite.ErrInvalidGrant.ErrorField)))
		Expect(mcpBearerAuthorizationAttempt(router, "/mcp", reuseAccessToken)).To(HaveHTTPStatus(http.StatusUnauthorized))
		reuseRefreshError := decodeJSONResponse(performForm(router, "http://leafwiki.local/oauth/token", url.Values{
			"grant_type":    {"refresh_token"},
			"client_id":     {oauthClientID},
			"refresh_token": {reuseRefreshToken},
		}), http.StatusUnauthorized)
		Expect(reuseRefreshError).To(matchOAuthErrorField(oauthErrorField(fosite.ErrInvalidGrant.ErrorField)))

		deleted, err := w.UserService().CreateUser("refresh-deleted", "refresh-deleted@example.com", "deletedpass", coreauth.RoleEditor)
		Expect(err).NotTo(HaveOccurred())
		deletedCookies := loginCookies(router, "refresh-deleted", "deletedpass")
		deletedCode := authorizeCode(router, deletedCookies, redirectURI, "refresh-deleted-state", verifier+"2", resource)
		deletedToken := exchangeCode(router, deletedCode, redirectURI, verifier+"2")
		deletedRefresh := stringFromMap(deletedToken, "refresh_token")
		Expect(w.UserService().DeleteUser(coreauth.UserIDFromString(deleted.ID))).To(Succeed())
		deletedRefreshForm := url.Values{
			"grant_type":    {"refresh_token"},
			"client_id":     {oauthClientID},
			"refresh_token": {deletedRefresh},
		}
		deletedRefreshError := decodeJSONResponse(performForm(router, "http://leafwiki.local/oauth/token", deletedRefreshForm), http.StatusUnauthorized)
		Expect(deletedRefreshError).To(matchOAuthErrorField(oauthErrorField(fosite.ErrInvalidGrant.ErrorField)))

		rec := performRequest(router, http.MethodPost, "http://leafwiki.local/oauth/revoke", nil, strings.NewReader(""))
		Expect(rec).To(HaveHTTPStatus(http.StatusNotFound))
		rec = performRequest(router, http.MethodPost, "http://leafwiki.local/oauth/introspect", nil, strings.NewReader(""))
		Expect(rec).To(HaveHTTPStatus(http.StatusNotFound))
	})
})

var _ = Describe("OAuth token lifetimes", Label("integration"), func() {
	It("uses wiki options for access token expiry", func() {
		w := newLocalMCPAuthTestWikiWithOptions(wiki.WikiOptions{
			AccessTokenTimeout:  15 * time.Minute,
			RefreshTokenTimeout: 7 * 24 * time.Hour,
		})
		opts := oauthRouterOptions("")
		opts.AccessTokenTimeout = time.Minute
		opts.RefreshTokenTimeout = 2 * time.Minute
		router := newLocalMCPTestRouter(w, opts)

		cookies := loginCookies(router, "admin", "admin")
		verifier := "oauth-lifetime-verifier-abcdefghijklmnopqrstuvwxyz0123456789"
		code := authorizeCode(router, cookies, "http://localhost:49152/callback", "lifetime-state", verifier, "http://leafwiki.local/mcp")
		token := exchangeCode(router, code, "http://localhost:49152/callback", verifier)

		Expect(token).To(HaveKeyWithValue("expires_in", BeNumerically(">=", (14*time.Minute).Seconds())))
	})

	It("rejects bearer tokens after configured expiry", func() {
		w := newLocalMCPAuthTestWikiWithOptions(wiki.WikiOptions{AccessTokenTimeout: -time.Minute})
		trustedProxies, err := authmw.ParseTrustedProxies("192.0.2.1")
		Expect(err).NotTo(HaveOccurred())
		opts := oauthRouterOptions("")
		opts.HTTPRemoteUser = httpinternal.HTTPRemoteUserConfig{
			Enabled:        true,
			HeaderName:     "X-Remote-User",
			TrustedProxies: trustedProxies,
			UserService:    w.UserService(),
		}
		router := newLocalMCPTestRouter(w, opts)
		headers := map[string]string{"X-Remote-User": "admin"}
		verifier := "oauth-expired-verifier-abcdefghijklmnopqrstuvwxyz0123456789"
		q := validAuthorizeQuery("http://localhost:49152/callback", "expired-state", pkceS256(verifier), "http://leafwiki.local/mcp")
		rec := performRequestWithHeaders(router, http.MethodGet, "http://leafwiki.local/oauth/authorize?"+q.Encode(), nil, nil, headers)
		form := approvalFormFromAuthorizeRedirect(rec, "")
		approved := performFormWithCookiesAndHeaders(router, "http://leafwiki.local/oauth/authorize", form, nil, headers)
		Expect(approved).To(HaveHTTPStatus(http.StatusFound), approved.Body.String())
		redirected, err := url.Parse(approved.Header().Get("Location"))
		Expect(err).NotTo(HaveOccurred())
		code := redirected.Query().Get("code")
		Expect(code).NotTo(BeEmpty(), "expired-token authorize redirect should include code: %s", redirected.String())
		token := stringFromMap(exchangeCode(router, code, "http://localhost:49152/callback", verifier), "access_token")

		req := httptest.NewRequest(http.MethodPost, "http://leafwiki.local/mcp", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`))
		req.RemoteAddr = "127.0.0.1:12345"
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "application/json, text/event-stream")
		rec = httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		Expect(rec).To(HaveHTTPStatus(http.StatusUnauthorized), rec.Body.String())
	})
})

var _ = Describe("OAuth-authenticated writes", Label("integration"), func() {
	It("preserves CSRF protection and author metadata across HTTP and MCP mutations", func() {
		w := newLocalMCPAuthTestWiki()
		router := newLocalMCPTestRouter(w, oauthRouterOptions(""))
		admin, err := w.UserService().GetUserByUsername("admin")
		Expect(err).NotTo(HaveOccurred())
		adminCookies := loginCookies(router, "admin", "admin")
		adminToken := oauthAccessTokenWithCookies(router, adminCookies, "admin-parity-state")
		adminSession := connectLocalMCPWithToken(router, "/mcp", adminToken)

		noCSRFBody := strings.NewReader(`{"title":"HTTP Missing CSRF","slug":"http-missing-csrf","kind":"page"}`)
		noCSRF := performRequest(router, http.MethodPost, "http://leafwiki.local/api/pages", adminCookies, noCSRFBody)
		Expect(noCSRF).To(HaveHTTPStatus(http.StatusForbidden), noCSRF.Body.String())

		page := exerciseOAuthWriterCRUD(router, adminSession, adminCookies, "admin", admin.ID)
		metadata := nestedMap(page, "metadata")
		creator := nestedMap(metadata, "creator")
		lastAuthor := nestedMap(metadata, "lastAuthor")
		Expect(metadata).To(HaveKeyWithValue("creatorId", admin.ID))
		Expect(metadata).To(HaveKeyWithValue("lastAuthorId", admin.ID))
		Expect(creator).To(HaveKeyWithValue("username", "admin"))
		Expect(lastAuthor).To(HaveKeyWithValue("username", "admin"))

		editor, err := w.UserService().CreateUser("oauth-editor", "oauth-editor@example.com", "editorpass", coreauth.RoleEditor)
		Expect(err).NotTo(HaveOccurred())
		editorCookies := loginCookies(router, "oauth-editor", "editorpass")
		editorToken := oauthAccessTokenWithCookies(router, editorCookies, "editor-crud-state")
		editorSession := connectLocalMCPWithToken(router, "/mcp", editorToken)
		_ = exerciseOAuthWriterCRUD(router, editorSession, editorCookies, "editor", editor.ID)
	})
})

func exerciseOAuthWriterCRUD(router http.Handler, session *sdkmcp.ClientSession, cookies []*http.Cookie, label, userID string) map[string]any {
	GinkgoHelper()

	titlePrefix := strings.ToUpper(label[:1]) + label[1:]
	created := callToolStructured(session, "wiki_create_page", map[string]any{
		"title": titlePrefix + " OAuth Page",
		"slug":  label + "-oauth-page",
		"kind":  "page",
	})
	createdPage := nestedMap(created, "page")
	pageID := stringField(createdPage, "id")
	createdVersion := stringField(createdPage, "version")
	createdMetadata := nestedMap(createdPage, "metadata")
	Expect(createdMetadata).To(HaveKeyWithValue("creatorId", userID))
	Expect(createdMetadata).To(HaveKeyWithValue("lastAuthorId", userID))

	httpPage := decodeJSONResponse(performRequest(router, http.MethodGet, "http://leafwiki.local/api/pages/"+pageID, cookies, nil), http.StatusOK)
	httpMetadata := nestedMap(httpPage, "metadata")
	Expect(httpMetadata).To(HaveKeyWithValue("creatorId", userID))
	Expect(httpMetadata).To(HaveKeyWithValue("lastAuthorId", userID))
	Expect(httpPage).To(HaveKeyWithValue("id", pageID))

	updated := callToolStructured(session, "wiki_update_page", map[string]any{
		"id":      pageID,
		"version": createdVersion,
		"title":   titlePrefix + " OAuth Page Updated",
		"slug":    label + "-oauth-page",
		"content": "Updated over authenticated MCP\n",
	})
	updatedPage := nestedMap(updated, "page")
	updatedMetadata := nestedMap(updatedPage, "metadata")
	Expect(updatedMetadata).To(HaveKeyWithValue("lastAuthorId", userID))
	updatedVersion := stringField(updatedPage, "version")

	deleted := callToolStructured(session, "wiki_delete_page", map[string]any{
		"id":        pageID,
		"version":   updatedVersion,
		"recursive": false,
	})
	Expect(deleted).To(HaveKey("message"))

	notFound := performRequest(router, http.MethodGet, "http://leafwiki.local/api/pages/"+pageID, cookies, nil)
	Expect(notFound).To(HaveHTTPStatus(http.StatusNotFound), "deleted %s page should be absent through HTTP", label)
	return updatedPage
}

var _ = Describe("local MCP OAuth bearer protection", Label("integration"), func() {
	It("challenges unauthenticated requests and rejects stale bearer identities", func() {
		w := newLocalMCPAuthTestWikiWithOptions(wiki.WikiOptions{})
		opts := oauthRouterOptions("")
		opts.EnableLinkRefactor = true
		opts.EnableWorkspaceSync = true
		router := newLocalMCPTestRouter(w, opts)

		rec := performRequest(router, http.MethodPost, "http://leafwiki.local/mcp", nil, strings.NewReader("{}"))
		Expect(rec).To(HaveHTTPStatus(http.StatusUnauthorized), rec.Body.String())
		Expect(rec.Header().Get("WWW-Authenticate")).To(SatisfyAll(
			ContainSubstring(`resource_metadata="http://leafwiki.local/.well-known/oauth-protected-resource/mcp"`),
			ContainSubstring(`scope="leafwiki:mcp"`),
		))

		rec = performRequest(router, http.MethodPost, "http://leafwiki.local/mcp", nil, strings.NewReader("{}"))
		rec.Result().Body.Close()
		req := httptest.NewRequest(http.MethodPost, "http://leafwiki.local/mcp", strings.NewReader("{}"))
		req.RemoteAddr = "127.0.0.1:12345"
		req.Header.Set("Authorization", "Bearer invalid-token")
		rec = httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		Expect(rec).To(HaveHTTPStatus(http.StatusUnauthorized), rec.Body.String())

		adminToken := oauthAccessTokenForUser(router, "admin", "admin", "admin-state")
		adminSession := connectLocalMCPWithToken(router, "/mcp", adminToken)
		current := callToolStructured(adminSession, "wiki_get_current_user", nil)
		user := nestedMap(current, "user")
		Expect(user).To(HaveKeyWithValue("username", "admin"))
		expectedTools := append(append([]string{}, baseToolNames...), wikimcp.WorkspaceSyncToolNames()...)
		expectedTools = append(expectedTools, wikimcp.RevisionToolNames()...)
		expectedTools = append(expectedTools, wikimcp.LinkRefactorToolNames()...)
		Expect(listAllToolNames(adminSession)).To(matchToolNames(expectedTools))

		editor, err := w.UserService().CreateUser("editor", "editor@example.com", "editorpass", coreauth.RoleEditor)
		Expect(err).NotTo(HaveOccurred())
		editorToken := oauthAccessTokenForUser(router, "editor", "editorpass", "editor-state")
		_, err = w.UserService().UpdateUser(coreauth.UserIDFromString(editor.ID), editor.Username, editor.Email, "", coreauth.RoleViewer)
		Expect(err).NotTo(HaveOccurred())

		downgradedSession := connectLocalMCPWithToken(router, "/mcp", editorToken)
		downgradedErr := callTypedToolStructuredError(downgradedSession, wikimcp.ToolCreatePage, map[string]any{
			"title": "Downgraded Write",
			"slug":  "downgraded-write",
		})
		Expect(downgradedErr).To(testmatchers.HaveMCPStructuredError(wikimcp.ErrCodeMCPEditorRoleRequired, sharederrors.MessageIDForCode(wikimcp.ErrCodeMCPEditorRoleRequired)))

		deleted, err := w.UserService().CreateUser("deleted", "deleted@example.com", "deletedpass", coreauth.RoleEditor)
		Expect(err).NotTo(HaveOccurred())
		deletedToken := oauthAccessTokenForUser(router, "deleted", "deletedpass", "deleted-state")
		Expect(w.UserService().DeleteUser(coreauth.UserIDFromString(deleted.ID))).To(Succeed())

		req = httptest.NewRequest(http.MethodPost, "http://leafwiki.local/mcp", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`))
		req.RemoteAddr = "127.0.0.1:12345"
		req.Header.Set("Authorization", "Bearer "+deletedToken)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "application/json, text/event-stream")
		rec = httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		Expect(rec).To(HaveHTTPStatus(http.StatusUnauthorized), rec.Body.String())
	})
})

var _ = DescribeTable("OAuth-authenticated viewers are denied editor MCP tools", Label("integration"),
	func(toolName wikimcp.ToolID, buildArgs func(pageID, currentVersion, latestRevisionID string) map[string]any) {
		w := newLocalMCPAuthTestWikiWithOptions(wiki.WikiOptions{})
		opts := oauthRouterOptions("")
		opts.EnableLinkRefactor = true
		opts.EnableWorkspaceSync = true
		router := newLocalMCPTestRouter(w, opts)

		adminToken := oauthAccessTokenForUser(router, "admin", "admin", "admin-state")
		adminSession := connectLocalMCPWithToken(router, "/mcp", adminToken)

		_, err := w.UserService().CreateUser("viewer", "viewer@example.com", "viewerpass", coreauth.RoleViewer)
		Expect(err).NotTo(HaveOccurred())
		viewerToken := oauthAccessTokenForUser(router, "viewer", "viewerpass", "viewer-state")
		viewerSession := connectLocalMCPWithToken(router, "/mcp", viewerToken)
		_ = callToolStructured(viewerSession, "wiki_get_tree", nil)

		page := nestedMap(callToolStructured(adminSession, "wiki_create_page", map[string]any{
			"title": "Viewer Gate Fixture",
			"slug":  "viewer-gate-fixture",
			"kind":  "page",
		}), "page")
		pageID := stringField(page, "id")
		pageVersion := stringField(page, "version")
		updatedContent := "viewer gate fixture revision"
		updated := nestedMap(callToolStructured(adminSession, "wiki_update_page", map[string]any{
			"id":      pageID,
			"version": pageVersion,
			"title":   "Viewer Gate Fixture",
			"slug":    "viewer-gate-fixture",
			"content": updatedContent,
		}), "page")
		currentVersion := stringField(updated, "version")
		_ = callToolStructured(adminSession, "wiki_upload_asset", map[string]any{
			"pageId":        pageID,
			"filename":      "viewer-gate.txt",
			"contentBase64": base64.StdEncoding.EncodeToString([]byte("viewer gate asset")),
		})
		latestRevision := nestedMap(callToolStructured(adminSession, "wiki_get_latest_revision", map[string]any{"pageId": pageID}), "revision")
		latestRevisionID := stringField(latestRevision, "id")

		errResult := callTypedToolStructuredError(viewerSession, toolName, buildArgs(pageID, currentVersion, latestRevisionID))
		Expect(errResult).To(testmatchers.HaveMCPStructuredError(wikimcp.ErrCodeMCPEditorRoleRequired, sharederrors.MessageIDForCode(wikimcp.ErrCodeMCPEditorRoleRequired)))

		afterViewerDenied := nestedMap(callToolStructured(adminSession, "wiki_get_page", map[string]any{"pageId": pageID}), "page")
		Expect(afterViewerDenied).To(SatisfyAll(
			HaveKeyWithValue("version", currentVersion),
			HaveKeyWithValue("content", updatedContent),
		))
	},
	Entry("suggesting a slug", wikimcp.ToolSuggestSlug, func(pageID, currentVersion, latestRevisionID string) map[string]any {
		return map[string]any{"title": "Viewer Slug"}
	}),
	Entry("refreshing the workspace", wikimcp.ToolRefresh, func(pageID, currentVersion, latestRevisionID string) map[string]any {
		return map[string]any{"source": "filesystem"}
	}),
	Entry("creating a page", wikimcp.ToolCreatePage, func(pageID, currentVersion, latestRevisionID string) map[string]any {
		return map[string]any{"title": "Viewer Write", "slug": "viewer-write"}
	}),
	Entry("updating page content", wikimcp.ToolUpdatePage, func(pageID, currentVersion, latestRevisionID string) map[string]any {
		return map[string]any{"id": pageID, "version": currentVersion, "title": "Viewer Gate Fixture", "slug": "viewer-gate-fixture", "content": "viewer update"}
	}),
	Entry("updating page metadata", wikimcp.ToolUpdatePageMetadata, func(pageID, currentVersion, latestRevisionID string) map[string]any {
		return map[string]any{"pageId": pageID, "version": currentVersion, "addTags": []any{"viewer"}}
	}),
	Entry("replacing a page section", wikimcp.ToolReplacePageSection, func(pageID, currentVersion, latestRevisionID string) map[string]any {
		return map[string]any{"pageId": pageID, "version": currentVersion, "headingPath": []any{"Missing"}, "content": "viewer section"}
	}),
	Entry("deleting a page", wikimcp.ToolDeletePage, func(pageID, currentVersion, latestRevisionID string) map[string]any {
		return map[string]any{"id": pageID, "version": currentVersion, "recursive": false}
	}),
	Entry("moving a page", wikimcp.ToolMovePage, func(pageID, currentVersion, latestRevisionID string) map[string]any {
		return map[string]any{"id": pageID, "version": currentVersion}
	}),
	Entry("sorting pages", wikimcp.ToolSortPages, func(pageID, currentVersion, latestRevisionID string) map[string]any {
		return map[string]any{"parentId": "", "orderedIds": []any{pageID}}
	}),
	Entry("ensuring a page", wikimcp.ToolEnsurePage, func(pageID, currentVersion, latestRevisionID string) map[string]any {
		return map[string]any{"path": "viewer/ensured", "title": "Viewer Ensured"}
	}),
	Entry("converting a page kind", wikimcp.ToolConvertPage, func(pageID, currentVersion, latestRevisionID string) map[string]any {
		return map[string]any{"id": pageID, "version": currentVersion, "targetKind": "section"}
	}),
	Entry("copying a page", wikimcp.ToolCopyPage, func(pageID, currentVersion, latestRevisionID string) map[string]any {
		return map[string]any{"id": pageID, "title": "Viewer Copy", "slug": "viewer-copy"}
	}),
	Entry("uploading an asset", wikimcp.ToolUploadAsset, func(pageID, currentVersion, latestRevisionID string) map[string]any {
		return map[string]any{"pageId": pageID, "filename": "viewer.txt", "contentBase64": base64.StdEncoding.EncodeToString([]byte("viewer"))}
	}),
	Entry("renaming an asset", wikimcp.ToolRenameAsset, func(pageID, currentVersion, latestRevisionID string) map[string]any {
		return map[string]any{"pageId": pageID, "oldFilename": "viewer-gate.txt", "newFilename": "viewer-renamed.txt"}
	}),
	Entry("deleting an asset", wikimcp.ToolDeleteAsset, func(pageID, currentVersion, latestRevisionID string) map[string]any {
		return map[string]any{"pageId": pageID, "filename": "viewer-gate.txt"}
	}),
	Entry("restoring a revision", wikimcp.ToolRestoreRevision, func(pageID, currentVersion, latestRevisionID string) map[string]any {
		return map[string]any{"pageId": pageID, "revisionId": latestRevisionID}
	}),
	Entry("previewing a page refactor", wikimcp.ToolPreviewRefactor, func(pageID, currentVersion, latestRevisionID string) map[string]any {
		return map[string]any{"pageId": pageID, "kind": "page", "title": "Viewer Preview", "slug": "viewer-preview"}
	}),
	Entry("applying a page refactor", wikimcp.ToolApplyRefactor, func(pageID, currentVersion, latestRevisionID string) map[string]any {
		return map[string]any{"pageId": pageID, "version": currentVersion, "kind": "page", "title": "Viewer Apply", "slug": "viewer-apply"}
	}),
)

var _ = Describe("local MCP API-key bearer protection", Label("integration"), func() {
	It("allows editor keys and rejects viewer downgraded revoked and deleted credentials", func() {
		w := newLocalMCPAuthTestWikiWithOptions(wiki.WikiOptions{})
		opts := oauthRouterOptions("")
		opts.EnableWorkspaceSync = true
		router := newLocalMCPTestRouter(w, opts)

		editor, err := w.UserService().CreateUser("api-editor", "api-editor@example.com", "editorpass", coreauth.RoleEditor)
		Expect(err).NotTo(HaveOccurred())
		editorID := coreauth.UserIDFromString(editor.ID)
		editorKey, err := w.APIKeyService().CreateAPIKey(editorID, "Editor MCP", editorID)
		Expect(err).NotTo(HaveOccurred())

		editorSession := connectLocalMCPWithToken(router, "/mcp", editorKey.Secret)
		current := callToolStructured(editorSession, "wiki_get_current_user", nil)
		user := nestedMap(current, "user")
		Expect(user).To(HaveKeyWithValue("username", "api-editor"))
		created := nestedMap(callToolStructured(editorSession, "wiki_create_page", map[string]any{
			"title": "API Key Editor Page",
			"slug":  "api-key-editor-page",
		}), "page")
		pageID := stringField(created, "id")
		pageVersion := stringField(created, "version")
		updated := nestedMap(callToolStructured(editorSession, "wiki_update_page", map[string]any{
			"id":      pageID,
			"version": pageVersion,
			"title":   "API Key Editor Page",
			"slug":    "api-key-editor-page",
			"content": "updated through api key",
		}), "page")
		currentVersion := stringField(updated, "version")

		viewer, err := w.UserService().CreateUser("api-viewer", "api-viewer@example.com", "viewerpass", coreauth.RoleViewer)
		Expect(err).NotTo(HaveOccurred())
		viewerID := coreauth.UserIDFromString(viewer.ID)
		viewerKey, err := w.APIKeyService().CreateAPIKey(viewerID, "Viewer MCP", viewerID)
		Expect(err).NotTo(HaveOccurred())

		viewerSession := connectLocalMCPWithToken(router, "/mcp", viewerKey.Secret)
		_ = callToolStructured(viewerSession, "wiki_get_tree", nil)
		for _, tt := range []struct {
			name wikimcp.ToolID
			args map[string]any
		}{
			{name: wikimcp.ToolRefresh, args: map[string]any{"source": "filesystem"}},
			{name: wikimcp.ToolCreatePage, args: map[string]any{"title": "Viewer API Key Write", "slug": "viewer-api-key-write"}},
			{name: wikimcp.ToolUpdatePageMetadata, args: map[string]any{"pageId": pageID, "version": currentVersion, "addTags": []any{"viewer"}}},
			{name: wikimcp.ToolReplacePageSection, args: map[string]any{"pageId": pageID, "version": currentVersion, "headingPath": []any{"Missing"}, "content": "viewer section"}},
		} {
			viewerErr := callTypedToolStructuredError(viewerSession, tt.name, tt.args)
			Expect(viewerErr).To(testmatchers.HaveMCPStructuredError(wikimcp.ErrCodeMCPEditorRoleRequired, sharederrors.MessageIDForCode(wikimcp.ErrCodeMCPEditorRoleRequired)))
		}
		afterViewerDenied := nestedMap(callToolStructured(editorSession, "wiki_get_page", map[string]any{"pageId": pageID}), "page")
		Expect(afterViewerDenied).To(SatisfyAll(
			HaveKeyWithValue("version", currentVersion),
			HaveKeyWithValue("content", "updated through api key"),
		))

		Expect(w.APIKeyService().RevokeAPIKey(editorID, editorKey.Key.ID)).To(Succeed())
		Expect(mcpBearerAuthorizationAttempt(router, "/mcp", editorKey.Secret)).To(HaveHTTPStatus(http.StatusUnauthorized))

		roleUser, err := w.UserService().CreateUser("api-role-change", "api-role-change@example.com", "editorpass", coreauth.RoleEditor)
		Expect(err).NotTo(HaveOccurred())
		roleUserID := coreauth.UserIDFromString(roleUser.ID)
		roleKey, err := w.APIKeyService().CreateAPIKey(roleUserID, "Role MCP", roleUserID)
		Expect(err).NotTo(HaveOccurred())
		_, err = w.UserService().UpdateUser(roleUserID, roleUser.Username, roleUser.Email, "", coreauth.RoleViewer)
		Expect(err).NotTo(HaveOccurred())
		downgradedSession := connectLocalMCPWithToken(router, "/mcp", roleKey.Secret)
		downgradedErr := callTypedToolStructuredError(downgradedSession, wikimcp.ToolCreatePage, map[string]any{
			"title": "Downgraded API Key Write",
			"slug":  "downgraded-api-key-write",
		})
		Expect(downgradedErr).To(testmatchers.HaveMCPStructuredError(wikimcp.ErrCodeMCPEditorRoleRequired, sharederrors.MessageIDForCode(wikimcp.ErrCodeMCPEditorRoleRequired)))

		deleted, err := w.UserService().CreateUser("api-deleted", "api-deleted@example.com", "editorpass", coreauth.RoleEditor)
		Expect(err).NotTo(HaveOccurred())
		deletedID := coreauth.UserIDFromString(deleted.ID)
		deletedKey, err := w.APIKeyService().CreateAPIKey(deletedID, "Deleted MCP", deletedID)
		Expect(err).NotTo(HaveOccurred())
		Expect(w.UserService().DeleteUser(deletedID)).To(Succeed())
		Expect(mcpBearerAuthorizationAttempt(router, "/mcp", deletedKey.Secret)).To(HaveHTTPStatus(http.StatusUnauthorized))
		Expect(mcpBearerAuthorizationAttempt(router, "/mcp", deletedKey.Secret+"_wrongsecret")).To(HaveHTTPStatus(http.StatusUnauthorized))
	})
})

var _ = Describe("local MCP context for viewers", Label("integration"), func() {
	It("skips forced workspace refresh while still returning context state", func() {
		rootDir := filepath.Join(oauthTestTempDir(), "content")
		w := newLocalMCPAuthTestWikiWithOptions(wiki.WikiOptions{
			Workspace: wiki.Workspace{RootDir: rootDir},
		})
		opts := oauthRouterOptions("")
		opts.EnableWorkspaceSync = true
		router := newLocalMCPTestRouter(w, opts)

		viewer, err := w.UserService().CreateUser("context-viewer", "context-viewer@example.com", "viewerpass", coreauth.RoleViewer)
		Expect(err).NotTo(HaveOccurred())
		viewerID := coreauth.UserIDFromString(viewer.ID)
		viewerKey, err := w.APIKeyService().CreateAPIKey(viewerID, "Viewer Context MCP", viewerID)
		Expect(err).NotTo(HaveOccurred())
		viewerSession := connectLocalMCPWithToken(router, "/mcp", viewerKey.Secret)

		out := callToolStructured(viewerSession, "wiki_get_context", map[string]any{
			"syncMode": "force",
		})

		Expect(out).To(SatisfyAll(
			HaveKeyWithValue("warnings", ContainElement(ContainSubstring("not an editor or admin"))),
			HaveKey("syncStatus"),
			HaveKeyWithValue("recommendedTools", Not(ContainElements(
				"wiki_refresh",
				"wiki_update_page",
				"wiki_create_page",
				"wiki_update_page_metadata",
				"wiki_replace_page_section",
			))),
		))
	})
})

var _ = Describe("private MCP API-key sessions", Label("integration"), func() {
	It("blocks read-only tools after the backing API key is revoked", func() {
		w := newLocalMCPAuthTestWiki()

		editor, err := w.UserService().CreateUser("private-stdio-editor", "private-stdio-editor@example.com", "editorpass", coreauth.RoleEditor)
		Expect(err).NotTo(HaveOccurred())
		editorID := coreauth.UserIDFromString(editor.ID)
		apiKey, err := w.APIKeyService().CreateAPIKey(editorID, "Private STDIO MCP", editorID)
		Expect(err).NotTo(HaveOccurred())

		handler := w.PrivateMCPHTTPHandler(oauthRouterOptions(""))
		session := connectLocalMCPWithToken(handler, "/mcp", apiKey.Secret)
		_ = callToolStructured(session, "wiki_get_tree", nil)

		Expect(w.APIKeyService().RevokeAPIKey(editorID, apiKey.Key.ID)).To(Succeed())
		Expect(mcpBearerAuthorizationAttempt(handler, "/mcp", apiKey.Secret)).To(HaveHTTPStatus(http.StatusUnauthorized))
	})
})

var _ = Describe("local MCP OAuth base paths", Label("integration"), func() {
	It("accepts API-key sessions on the configured base path", func() {
		w := newLocalMCPAuthTestWiki()
		router := newLocalMCPTestRouter(w, oauthRouterOptions("/wiki"))

		admin, err := w.UserService().GetUserByUsername("admin")
		Expect(err).NotTo(HaveOccurred())
		adminID := coreauth.UserIDFromString(admin.ID)
		apiKey, err := w.APIKeyService().CreateAPIKey(adminID, "Base Path MCP", adminID)
		Expect(err).NotTo(HaveOccurred())
		session := connectLocalMCPWithToken(router, "/wiki/mcp", apiKey.Secret)
		current := callToolStructured(session, "wiki_get_current_user", nil)
		user := nestedMap(current, "user")
		Expect(user).To(HaveKeyWithValue("username", "admin"))

		config := callToolStructured(session, "wiki_get_config", nil)
		Expect(config).To(HaveKeyWithValue("basePath", "/wiki"))
	})

	It("advertises the protected resource metadata for base-path challenges", func() {
		w := newLocalMCPAuthTestWiki()
		router := newLocalMCPTestRouter(w, oauthRouterOptions("/wiki"))

		rec := performRequest(router, http.MethodPost, "http://leafwiki.local/wiki/mcp", nil, strings.NewReader("{}"))

		Expect(rec).To(HaveHTTPStatus(http.StatusUnauthorized), rec.Body.String())
		Expect(rec.Header().Get("WWW-Authenticate")).To(ContainSubstring(`resource_metadata="http://leafwiki.local/.well-known/oauth-protected-resource/wiki/mcp"`))
	})

	It("accepts OAuth sessions on the configured base path", func() {
		w := newLocalMCPAuthTestWiki()
		router := newLocalMCPTestRouter(w, oauthRouterOptions("/wiki"))
		cookies := loginCookiesAt(router, "/wiki", "admin", "admin")
		token := oauthAccessTokenWithCookiesAt(router, cookies, "/wiki", "base-path-session-state", "http://leafwiki.local/wiki/mcp")

		session := connectLocalMCPWithToken(router, "/wiki/mcp", token)
		current := callToolStructured(session, "wiki_get_current_user", nil)
		user := nestedMap(current, "user")
		Expect(user).To(HaveKeyWithValue("username", "admin"))

		config := callToolStructured(session, "wiki_get_config", nil)
		Expect(config).To(HaveKeyWithValue("basePath", "/wiki"))
		Expect(listAllToolNames(session)).To(matchToolNames(federatedToolNames()))
	})
})

type staticOAuthHandler struct {
	token string
}

func (h staticOAuthHandler) TokenSource(context.Context) (xoauth2.TokenSource, error) {
	return xoauth2.StaticTokenSource(&xoauth2.Token{AccessToken: h.token}), nil
}

func (h staticOAuthHandler) Authorize(context.Context, *http.Request, *http.Response) error {
	return fmt.Errorf("unexpected oauth authorize callback")
}

func newLocalMCPAuthTestWiki() *wiki.Wiki {
	GinkgoHelper()

	return newLocalMCPAuthTestWikiWithOptions(wiki.WikiOptions{})
}

func newLocalMCPAuthTestWikiWithOptions(overrides wiki.WikiOptions) *wiki.Wiki {
	GinkgoHelper()

	options := wiki.WikiOptions{
		StorageDir:          oauthTestTempDir(),
		Workspace:           overrides.Workspace,
		AdminPassword:       "admin",
		JWTSecret:           "secretkey",
		AccessTokenTimeout:  15 * time.Minute,
		RefreshTokenTimeout: 7 * 24 * time.Hour,
	}
	if overrides.AccessTokenTimeout != 0 {
		options.AccessTokenTimeout = overrides.AccessTokenTimeout
	}
	if overrides.RefreshTokenTimeout != 0 {
		options.RefreshTokenTimeout = overrides.RefreshTokenTimeout
	}

	w, err := wiki.NewWiki(&options)
	Expect(err).NotTo(HaveOccurred())
	DeferCleanup(func() {
		Expect(w.Close()).To(Succeed())
	})
	return w
}

func oauthTestTempDir() string {
	GinkgoHelper()
	dir, err := os.MkdirTemp("", "leafwiki-mcp-oauth-test-*")
	Expect(err).To(Succeed())
	DeferCleanup(os.RemoveAll, dir)
	return dir
}

func oauthRouterOptions(basePath string) httpinternal.RouterOptions {
	return httpinternal.RouterOptions{
		AllowInsecure:           true,
		BasePath:                basePath,
		AccessTokenTimeout:      15 * time.Minute,
		RefreshTokenTimeout:     7 * 24 * time.Hour,
		MaxAssetUploadSizeBytes: assets.DefaultMaxUploadSizeBytes,
		MCPEnabled:              true,
		MCPToolListPageSize:     200,
	}
}

func getJSONMap(router http.Handler, target string) map[string]any {
	GinkgoHelper()

	return decodeJSONResponse(performRequest(router, http.MethodGet, target, nil, nil), http.StatusOK)
}

func decodeJSONResponse(rec *httptest.ResponseRecorder, wantStatus int) map[string]any {
	GinkgoHelper()

	Expect(rec).To(HaveHTTPStatus(wantStatus), rec.Body.String())
	var out map[string]any
	Expect(json.Unmarshal(rec.Body.Bytes(), &out)).To(Succeed(), "response body should decode as JSON: %s", rec.Body.String())
	return out
}

func performRequest(router http.Handler, method, target string, cookies []*http.Cookie, body io.Reader) *httptest.ResponseRecorder {
	GinkgoHelper()

	return performRequestWithHeaders(router, method, target, cookies, body, nil)
}

func performRequestWithHeaders(router http.Handler, method, target string, cookies []*http.Cookie, body io.Reader, headers map[string]string) *httptest.ResponseRecorder {
	GinkgoHelper()

	req := httptest.NewRequest(method, target, body)
	markLoopbackMCPTestRequest(req)
	for _, cookie := range cookies {
		req.AddCookie(cookie)
	}
	for name, value := range headers {
		req.Header.Set(name, value)
	}
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec
}

func markLoopbackMCPTestRequest(req *http.Request) {
	if req == nil || req.URL == nil {
		return
	}
	path := req.URL.Path
	if path == "/mcp" || strings.HasSuffix(path, "/mcp") || strings.Contains(path, "/mcp/") {
		req.RemoteAddr = "127.0.0.1:12345"
	}
}

func performJSON(router http.Handler, target, body string) *httptest.ResponseRecorder {
	GinkgoHelper()

	req := httptest.NewRequest(http.MethodPost, target, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec
}

func registerOAuthClient(router http.Handler, basePath, body string) map[string]any {
	GinkgoHelper()

	return decodeJSONResponse(performJSON(router, "http://leafwiki.local"+basePath+"/oauth/register", body), http.StatusCreated)
}

func performForm(router http.Handler, target string, form url.Values) *httptest.ResponseRecorder {
	GinkgoHelper()

	return performFormWithCookiesAndHeaders(router, target, form, nil, nil)
}

func performFormWithCookiesAndHeaders(router http.Handler, target string, form url.Values, cookies []*http.Cookie, headers map[string]string) *httptest.ResponseRecorder {
	GinkgoHelper()

	req := httptest.NewRequest(http.MethodPost, target, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	for _, cookie := range cookies {
		req.AddCookie(cookie)
	}
	for name, value := range headers {
		req.Header.Set(name, value)
	}
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec
}

func approvalFormFromAuthorizeRedirect(rec *httptest.ResponseRecorder, basePath string) url.Values {
	GinkgoHelper()

	Expect(rec).To(HaveHTTPStatus(http.StatusFound), rec.Body.String())
	location := rec.Header().Get("Location")
	redirected, err := url.Parse(location)
	Expect(err).NotTo(HaveOccurred(), "approval redirect should parse: %s", location)
	Expect(redirected.Path).To(Equal(basePath+"/oauth/approve"), "approval redirect should target the approve route: %s", location)
	form := redirected.Query()
	Expect(form.Get("approval_token")).NotTo(BeEmpty(), "approval redirect should include an approval token: %s", location)
	form.Set("decision", "approve")
	return form
}

func approvalDetails(router http.Handler, basePath, token string, cookies []*http.Cookie, headers map[string]string) map[string]any {
	GinkgoHelper()

	target := "http://leafwiki.local" + basePath + "/oauth/approval?approval_token=" + url.QueryEscape(token)
	return decodeJSONResponse(performRequestWithHeaders(router, http.MethodGet, target, cookies, nil, headers), http.StatusOK)
}

func validAuthorizeQuery(redirectURI, state, challenge, resource string) url.Values {
	return validAuthorizeQueryForClient(oauthClientID, redirectURI, state, challenge, resource)
}

func validAuthorizeQueryForClient(clientID, redirectURI, state, challenge, resource string) url.Values {
	q := url.Values{
		"client_id":             {clientID},
		"response_type":         {"code"},
		"redirect_uri":          {redirectURI},
		"scope":                 {oauthScope},
		"state":                 {state},
		"code_challenge":        {challenge},
		"code_challenge_method": {"S256"},
	}
	if resource != "" {
		q.Set("resource", resource)
	}
	return q
}

func pkceS256(verifier string) string {
	sum := sha256.Sum256([]byte(verifier))
	return strings.TrimRight(base64.URLEncoding.EncodeToString(sum[:]), "=")
}

func loginCookies(router http.Handler, identifier, password string) []*http.Cookie {
	GinkgoHelper()

	return loginCookiesAt(router, "", identifier, password)
}

func loginCookiesAt(router http.Handler, basePath, identifier, password string) []*http.Cookie {
	GinkgoHelper()

	body := fmt.Sprintf(`{"identifier":%q,"password":%q}`, identifier, password)
	req := httptest.NewRequest(http.MethodPost, "http://leafwiki.local"+basePath+"/api/auth/login", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	Expect(rec).To(HaveHTTPStatus(http.StatusOK), "login for %s should succeed: %s", identifier, rec.Body.String())
	return rec.Result().Cookies()
}

func authorizeCode(router http.Handler, cookies []*http.Cookie, redirectURI, state, verifier, resource string) string {
	GinkgoHelper()

	return authorizeCodeAt(router, cookies, "", redirectURI, state, verifier, resource)
}

func authorizeCodeAt(router http.Handler, cookies []*http.Cookie, basePath, redirectURI, state, verifier, resource string) string {
	GinkgoHelper()

	q := validAuthorizeQuery(redirectURI, state, pkceS256(verifier), resource)
	authorizeURL := "http://leafwiki.local" + basePath + "/oauth/authorize"
	rec := performRequest(router, http.MethodGet, authorizeURL+"?"+q.Encode(), cookies, nil)
	form := approvalFormFromAuthorizeRedirect(rec, basePath)
	approved := performFormWithCookiesAndHeaders(router, authorizeURL, form, cookies, nil)
	Expect(approved).To(HaveHTTPStatus(http.StatusFound), approved.Body.String())
	redirected, err := url.Parse(approved.Header().Get("Location"))
	Expect(err).NotTo(HaveOccurred())
	code := redirected.Query().Get("code")
	Expect(code).NotTo(BeEmpty(), "authorize redirect should include a code: %s", redirected.String())
	Expect(redirected.Query().Get("state")).To(Equal(state))
	return code
}

func exchangeCode(router http.Handler, code, redirectURI, verifier string) map[string]any {
	GinkgoHelper()

	return exchangeCodeAt(router, "", code, redirectURI, verifier)
}

func exchangeCodeAt(router http.Handler, basePath, code, redirectURI, verifier string) map[string]any {
	GinkgoHelper()

	return exchangeCodeForClient(router, basePath, oauthClientID, code, redirectURI, verifier)
}

func exchangeCodeForClient(router http.Handler, basePath, clientID, code, redirectURI, verifier string) map[string]any {
	GinkgoHelper()

	form := url.Values{
		"grant_type":    {"authorization_code"},
		"client_id":     {clientID},
		"redirect_uri":  {redirectURI},
		"code":          {code},
		"code_verifier": {verifier},
	}
	return decodeJSONResponse(performForm(router, "http://leafwiki.local"+basePath+"/oauth/token", form), http.StatusOK)
}

func oauthAccessTokenForUser(router http.Handler, username, password, state string) string {
	GinkgoHelper()

	cookies := loginCookies(router, username, password)
	return oauthAccessTokenWithCookies(router, cookies, state)
}

func oauthAccessTokenWithCookies(router http.Handler, cookies []*http.Cookie, state string) string {
	GinkgoHelper()

	return oauthAccessTokenWithCookiesAt(router, cookies, "", state, "http://leafwiki.local/mcp")
}

func oauthAccessTokenWithCookiesAt(router http.Handler, cookies []*http.Cookie, basePath, state, resource string) string {
	GinkgoHelper()

	verifier := "oauth-access-verifier-" + state + "-abcdefghijklmnopqrstuvwxyz0123456789"
	code := authorizeCodeAt(router, cookies, basePath, "http://localhost:49152/callback", state, verifier, resource)
	return stringFromMap(exchangeCodeAt(router, basePath, code, "http://localhost:49152/callback", verifier), "access_token")
}

func connectLocalMCPWithToken(handler http.Handler, path, token string) *sdkmcp.ClientSession {
	GinkgoHelper()

	server := httptest.NewServer(handler)
	DeferCleanup(server.Close)

	client := sdkmcp.NewClient(&sdkmcp.Implementation{Name: "leafwiki-test", Version: "test"}, nil)
	session, err := client.Connect(context.Background(), &sdkmcp.StreamableClientTransport{
		Endpoint:             server.URL + path,
		HTTPClient:           server.Client(),
		DisableStandaloneSSE: true,
		OAuthHandler:         staticOAuthHandler{token: token},
	}, nil)
	Expect(err).NotTo(HaveOccurred(), "MCP client should connect with OAuth token")
	DeferCleanup(func() { _ = session.Close() })
	return session
}

func callTypedToolStructuredError(session *sdkmcp.ClientSession, name wikimcp.ToolID, args map[string]any) mcpToolErrorResult {
	GinkgoHelper()

	result, err := session.CallTool(context.Background(), &sdkmcp.CallToolParams{
		Name:      name.String(),
		Arguments: args,
	})
	Expect(err).NotTo(HaveOccurred(), "CallTool %s should return a result", name)
	return toolErrorResultFromCallResult(name, result)
}

func mcpBearerAuthorizationAttempt(router http.Handler, path, token string) *httptest.ResponseRecorder {
	GinkgoHelper()

	return performRequestWithHeaders(router, http.MethodPost, "http://leafwiki.local"+path, nil, strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`), map[string]string{
		"Authorization": "Bearer " + token,
		"Content-Type":  "application/json",
		"Accept":        "application/json, text/event-stream",
	})
}

func matchOAuthErrorField(want oauthErrorField) types.GomegaMatcher {
	GinkgoHelper()
	return HaveKeyWithValue("error", string(want))
}

func stringFromMap(got map[string]any, field string) string {
	GinkgoHelper()

	Expect(got).To(HaveKeyWithValue(field, BeAssignableToTypeOf("")))
	return got[field].(string)
}

var _ sdkauth.OAuthHandler = staticOAuthHandler{}
