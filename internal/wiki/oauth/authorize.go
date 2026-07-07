package oauth

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/ory/fosite"
	coreauth "github.com/perber/wiki/internal/core/auth"
	httpinternal "github.com/perber/wiki/internal/http"
)

type fixedClientRedirectContextKey struct{}

func (r *Routes) handleAuthorize(ctx httpinternal.RouterContext) gin.HandlerFunc {
	return func(c *gin.Context) {
		redirectURI, state, err := r.validateAuthorizeRedirectTarget(c.Request)
		if err != nil {
			writeOAuthBadRequest(c, err)
			return
		}

		req, err := r.service.newAuthorizeRequest(c.Request, redirectURI, state)
		if err != nil {
			r.redirectAuthorizeError(c, redirectURI, state, err)
			return
		}
		if err := r.validateAuthorizeRequest(c.Request, req, ctx.Opts.BasePath); err != nil {
			r.redirectAuthorizeError(c, redirectURI, state, err)
			return
		}

		user := r.currentWebUser(c, ctx)
		if user == nil {
			loginURL := ctx.Opts.BasePath + "/login"
			original := absoluteRequestURL(c.Request)
			c.Redirect(http.StatusFound, loginURL+"?returnTo="+url.QueryEscape(original))
			return
		}

		approvalValues, approvalKey, err := oauthAuthorizeApprovalValues(c.Request)
		if err != nil {
			r.redirectAuthorizeError(c, redirectURI, state, fosite.ErrInvalidRequest)
			return
		}
		userID := coreauth.UserIDFromString(user.ID)
		switch c.PostForm("decision") {
		case "approve":
			if !r.service.consumeApproval(c.PostForm("approval_token"), userID, approvalKey) {
				writeOAuthBadRequest(c, fosite.ErrInvalidRequest)
				return
			}
		case "deny":
			_ = r.service.consumeApproval(c.PostForm("approval_token"), userID, approvalKey)
			r.redirectAuthorizeError(c, redirectURI, state, fosite.ErrAccessDenied)
			return
		default:
			details := r.service.approvalPageData(c.Request, req, ctx.Opts.BasePath)
			token, err := r.service.issueApproval(userID, approvalKey, details)
			if err != nil {
				writeOAuthBadRequest(c, err)
				return
			}
			approvalValues.Set("approval_token", token)
			c.Redirect(http.StatusFound, ctx.Opts.BasePath+"/oauth/approve?"+approvalValues.Encode())
			return
		}

		session := newFositeSession(user.ID.MetadataValue(), user.Username)
		req.SetSession(session)
		req.GrantScope(ScopeMCP)
		info, err := oauthNewAuthorizeResponse(r.service.fositeProvider, c.Request.Context(), req, session)
		if err != nil {
			r.redirectAuthorizeError(c, redirectURI, state, err)
			return
		}
		writeAuthorizeRedirect(c, req, info)
	}
}

func (r *Routes) currentWebUser(c *gin.Context, ctx httpinternal.RouterContext) *coreauth.User {
	user, err := oauthResolveRequestUser(c, r.service.auth, ctx.AuthCookies, false)
	if err != nil {
		return nil
	}
	return user
}

func (s *Service) newAuthorizeRequest(req *http.Request, redirectURI, state string) (fosite.AuthorizeRequester, error) {
	if err := req.ParseForm(); err != nil {
		return nil, fosite.ErrInvalidRequest
	}
	parseRequest := req
	if strings.TrimSpace(req.FormValue("client_id")) == ClientID {
		ctx := context.WithValue(req.Context(), fixedClientRedirectContextKey{}, redirectURI)
		parseRequest = req.WithContext(ctx)
	}
	return oauthNewAuthorizeRequest(s.fositeProvider, parseRequest.Context(), parseRequest)
}

func (r *Routes) validateAuthorizeRequest(req *http.Request, ar fosite.AuthorizeRequester, basePath string) error {
	clientID := ar.GetClient().GetID()
	client, ok := r.service.client(clientID)
	if !ok {
		return ErrOAuthUnknownClient
	}
	if !ar.GetResponseTypes().ExactOne(responseTypeCode) || !stringSliceContains(client.ResponseTypes, responseTypeCode) {
		return fosite.ErrUnsupportedResponseType
	}
	scope := strings.Join(ar.GetRequestedScopes(), " ")
	if !clientScopeAllowed(client, scope) {
		return fosite.ErrInvalidScope
	}
	if req.FormValue("code_challenge") == "" || req.FormValue("code_challenge_method") != "S256" {
		return fosite.ErrInvalidRequest
	}
	redirectURI := ar.GetRedirectURI().String()
	if err := validateLoopbackRedirectURI(redirectURI); err != nil {
		return err
	}
	if !clientRedirectURIAllowed(client, redirectURI) {
		return fosite.ErrInvalidRequest
	}
	if err := validateAuthorizeResource(req, basePath); err != nil {
		return fosite.ErrInvalidRequest
	}
	return nil
}

func validateAuthorizeResource(req *http.Request, basePath string) error {
	if err := req.ParseForm(); err != nil {
		return err
	}
	resources, ok := req.Form["resource"]
	if !ok || len(resources) == 0 {
		return nil
	}
	if len(resources) != 1 {
		return ErrOAuthResourceMustBeSingular
	}
	if resources[0] != MCPResourceURL(req, basePath) {
		return ErrOAuthResourceMismatch
	}
	return nil
}

func (r *Routes) validateAuthorizeRedirectTarget(req *http.Request) (string, string, error) {
	clientID := req.FormValue("client_id")
	client, ok := r.service.client(clientID)
	if !ok {
		return "", "", ErrOAuthUnknownClient
	}
	redirectURI := req.FormValue("redirect_uri")
	if err := validateLoopbackRedirectURI(redirectURI); err != nil {
		return "", "", err
	}
	if !clientRedirectURIAllowed(client, redirectURI) {
		return "", "", ErrOAuthRedirectURIUnregistered
	}
	return redirectURI, req.FormValue("state"), nil
}

func (r *Routes) redirectAuthorizeError(c *gin.Context, redirectURI, state string, err error) {
	target, parseErr := url.Parse(redirectURI)
	if parseErr != nil {
		writeOAuthBadRequest(c, parseErr)
		return
	}
	query := target.Query()
	query.Set("error", fosite.ErrorToRFC6749Error(err).ErrorField)
	if state != "" {
		query.Set("state", state)
	}
	target.RawQuery = query.Encode()
	c.Redirect(http.StatusFound, target.String())
}

func writeAuthorizeRedirect(c *gin.Context, req fosite.AuthorizeRequester, resp fosite.AuthorizeResponder) {
	for name, values := range resp.GetHeader() {
		for _, value := range values {
			c.Header(name, value)
		}
	}
	c.Header("Cache-Control", "no-store")
	c.Header("Pragma", "no-cache")
	target := *req.GetRedirectURI()
	query := target.Query()
	for name, values := range resp.GetParameters() {
		if len(values) > 0 {
			query.Set(name, values[0])
		}
	}
	target.RawQuery = query.Encode()
	c.Redirect(http.StatusFound, target.String())
}

func requestedScopeAllowed(scope string) bool {
	if strings.TrimSpace(scope) == "" {
		return true
	}
	for _, value := range strings.Fields(scope) {
		if value != ScopeMCP {
			return false
		}
	}
	return true
}

func validateLoopbackRedirectURI(raw string) error {
	u, err := url.Parse(raw)
	if err != nil || u == nil {
		return fmt.Errorf("%w: %w", ErrOAuthRedirectURIInvalid, err)
	}
	if u.Scheme != "http" {
		return ErrOAuthRedirectURIMustUseHTTP
	}
	if u.Port() == "" {
		return ErrOAuthRedirectURIPortRequired
	}
	if u.Fragment != "" {
		return ErrOAuthRedirectURIHasFragment
	}
	host := strings.ToLower(u.Hostname())
	if host == "localhost" || host == "127.0.0.1" || host == "::1" {
		return nil
	}
	return ErrOAuthRedirectURINotLoopback
}

func clientRedirectURIAllowed(client registeredClient, redirectURI string) bool {
	if len(client.RedirectURIs) == 0 {
		return true
	}
	return stringSliceContains(client.RedirectURIs, redirectURI)
}

func clientScopeAllowed(client registeredClient, scope string) bool {
	if !requestedScopeAllowed(scope) {
		return false
	}
	if strings.TrimSpace(scope) == "" || client.Scope == "" {
		return true
	}
	allowed := strings.Fields(client.Scope)
	for _, value := range strings.Fields(scope) {
		if !stringSliceContains(allowed, value) {
			return false
		}
	}
	return true
}

func stringSliceContains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
