package mcp_test

import (
	"net/http"
	"net/url"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/ory/fosite"
	"github.com/perber/wiki/internal/core/assets"
	httpinternal "github.com/perber/wiki/internal/http"
	authmw "github.com/perber/wiki/internal/http/middleware/auth"
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
