package oauth

import (
	"context"
	"crypto/rand"
	"net/http"

	"github.com/ory/fosite"
	authmw "github.com/perber/wiki/internal/http/middleware/auth"
)

var (
	oauthRandomRead              = rand.Read
	newOAuthFositeStore          = newFositeStore
	oauthResolveRequestUser      = authmw.ResolveRequestUser
	oauthAuthorizeApprovalValues = authorizeApprovalValues
	oauthNewAuthorizeRequest     = func(provider fosite.OAuth2Provider, ctx context.Context, req *http.Request) (fosite.AuthorizeRequester, error) {
		return provider.NewAuthorizeRequest(ctx, req)
	}
	oauthNewAuthorizeResponse = func(provider fosite.OAuth2Provider, ctx context.Context, req fosite.AuthorizeRequester, session fosite.Session) (fosite.AuthorizeResponder, error) {
		return provider.NewAuthorizeResponse(ctx, req, session)
	}
	oauthNewAccessRequest = func(provider fosite.OAuth2Provider, ctx context.Context, req *http.Request, session fosite.Session) (fosite.AccessRequester, error) {
		return provider.NewAccessRequest(ctx, req, session)
	}
	oauthNewAccessResponse = func(provider fosite.OAuth2Provider, ctx context.Context, req fosite.AccessRequester) (fosite.AccessResponder, error) {
		return provider.NewAccessResponse(ctx, req)
	}
	oauthIntrospectToken = func(provider fosite.OAuth2Provider, ctx context.Context, token string, tokenUse fosite.TokenUse, session fosite.Session, scopes ...string) (fosite.TokenUse, fosite.AccessRequester, error) {
		return provider.IntrospectToken(ctx, token, tokenUse, session, scopes...)
	}
)
