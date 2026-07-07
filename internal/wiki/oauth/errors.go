package oauth

import "errors"

var (
	ErrOAuthRedirectURIsRequired           = errors.New("redirect_uris is required")
	ErrOAuthRedirectURIEmpty               = errors.New("redirect_uris must not contain empty values")
	ErrOAuthUnsupportedGrantType           = errors.New("unsupported grant_type")
	ErrOAuthAuthorizationCodeGrantRequired = errors.New("authorization_code grant_type is required")
	ErrOAuthUnsupportedScope               = errors.New("unsupported scope")
	ErrOAuthUnknownClient                  = errors.New("unknown oauth client")
	ErrOAuthRedirectURIUnregistered        = errors.New("redirect_uri is not registered for this client")
	ErrOAuthRedirectURIInvalid             = errors.New("invalid redirect_uri")
	ErrOAuthRedirectURIMustUseHTTP         = errors.New("redirect_uri must use http")
	ErrOAuthRedirectURIPortRequired        = errors.New("redirect_uri must include an explicit port")
	ErrOAuthRedirectURIHasFragment         = errors.New("redirect_uri must not include a fragment")
	ErrOAuthRedirectURINotLoopback         = errors.New("redirect_uri must be loopback")
	ErrOAuthResourceMustBeSingular         = errors.New("resource must be supplied once")
	ErrOAuthResourceMismatch               = errors.New("resource must match MCP resource URL")
	ErrOAuthClientIDUnavailable            = errors.New("generate unique oauth client id")
)
