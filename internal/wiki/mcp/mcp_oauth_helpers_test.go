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
	"strings"
	"time"

	sdkauth "github.com/modelcontextprotocol/go-sdk/auth"
	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/types"
	"github.com/perber/wiki/internal/core/assets"
	httpinternal "github.com/perber/wiki/internal/http"
	"github.com/perber/wiki/internal/wiki"
	wikimcp "github.com/perber/wiki/internal/wiki/mcp"
	xoauth2 "golang.org/x/oauth2"
)

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
