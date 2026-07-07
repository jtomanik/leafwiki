package mcp_test

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"time"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/ory/fosite"
	coreauth "github.com/perber/wiki/internal/core/auth"
	httpinternal "github.com/perber/wiki/internal/http"
	authmw "github.com/perber/wiki/internal/http/middleware/auth"
	"github.com/perber/wiki/internal/wiki"
)

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
			"code":          {string(badCode)},
			"code_verifier": {"wrong-verifier"},
		}
		badRec := performForm(router, "http://leafwiki.local/oauth/token", badForm)
		Expect(badRec).To(HaveHTTPStatus(http.StatusUnauthorized), badRec.Body.String())
		Expect(badRec.Header().Get("Content-Type")).To(HavePrefix("application/json"))
		badTokenError := decodeJSONResponse(badRec, http.StatusUnauthorized)
		Expect(badTokenError).To(matchOAuthErrorField(oauthErrorField(fosite.ErrInvalidGrant.ErrorField)))

		code := authorizeCode(router, cookies, redirectURI, "token-state", verifier, resource)
		token := exchangeCode(router, oauthAuthorizationCode(code), redirectURI, verifier)
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
			"code":          {string(reuseCode)},
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
		token := exchangeCode(router, oauthAuthorizationCode(code), "http://localhost:49152/callback", verifier)

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
		token := stringFromMap(exchangeCode(router, oauthAuthorizationCode(code), "http://localhost:49152/callback", verifier), "access_token")

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
		Expect(metadata).To(HaveKeyWithValue("creatorId", admin.ID.MetadataValue()))
		Expect(metadata).To(HaveKeyWithValue("lastAuthorId", admin.ID.MetadataValue()))
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

func exerciseOAuthWriterCRUD(router http.Handler, session *sdkmcp.ClientSession, cookies []*http.Cookie, label string, userID coreauth.UserID) map[string]any {
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
	Expect(createdMetadata).To(HaveKeyWithValue("creatorId", userID.MetadataValue()))
	Expect(createdMetadata).To(HaveKeyWithValue("lastAuthorId", userID.MetadataValue()))

	httpPage := decodeJSONResponse(performRequest(router, http.MethodGet, "http://leafwiki.local/api/pages/"+pageID, cookies, nil), http.StatusOK)
	httpMetadata := nestedMap(httpPage, "metadata")
	Expect(httpMetadata).To(HaveKeyWithValue("creatorId", userID.MetadataValue()))
	Expect(httpMetadata).To(HaveKeyWithValue("lastAuthorId", userID.MetadataValue()))
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
	Expect(updatedMetadata).To(HaveKeyWithValue("lastAuthorId", userID.MetadataValue()))
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
