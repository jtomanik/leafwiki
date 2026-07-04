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
	"github.com/ory/fosite"
	coreauth "github.com/perber/wiki/internal/core/auth"
	httpinternal "github.com/perber/wiki/internal/http"
	authmw "github.com/perber/wiki/internal/http/middleware/auth"
)

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
