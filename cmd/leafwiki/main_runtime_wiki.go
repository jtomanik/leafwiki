package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	sdkauth "github.com/modelcontextprotocol/go-sdk/auth"
	coreauth "github.com/perber/wiki/internal/core/auth"
	sharederrors "github.com/perber/wiki/internal/core/shared/errors"
	"github.com/perber/wiki/internal/frontd"
	httpinternal "github.com/perber/wiki/internal/http"
	authmw "github.com/perber/wiki/internal/http/middleware/auth"
	"github.com/perber/wiki/internal/projectdaemon"
	"github.com/perber/wiki/internal/wiki"
	wikioauth "github.com/perber/wiki/internal/wiki/oauth"
)

type runtimeWikiMode string

const (
	runtimeWikiFull             runtimeWikiMode = "full"
	runtimeWikiWorkspaceOnly    runtimeWikiMode = "workspace-only"
	runtimeWikiControlPlaneOnly runtimeWikiMode = "control-plane-only"
)

func newRuntimeWiki(cfg leafwikiRuntimeConfig, ownerCfg projectdaemon.Config, mode runtimeWikiMode) (*wiki.Wiki, error) {
	authStorageDir := ""
	if cfg.RuntimeStack == projectdaemon.RuntimeStackWikidFrontd {
		authStorageDir = authStorageDirForRuntime(ownerCfg.DataDir)
	}
	w, err := wiki.NewWiki(&wiki.WikiOptions{
		Workspace:              wiki.Workspace{ID: cfg.Workspace.ID, DataDir: ownerCfg.DataDir, RootDir: ownerCfg.RootDir},
		StorageDir:             ownerCfg.DataDir,
		AuthStorageDir:         authStorageDir,
		WorkspaceOnly:          mode == runtimeWikiWorkspaceOnly,
		ControlPlaneOnly:       mode == runtimeWikiControlPlaneOnly,
		AdminPassword:          cfg.AdminPassword,
		JWTSecret:              cfg.JWTSecret,
		AccessTokenTimeout:     cfg.AccessTokenTimeout,
		RefreshTokenTimeout:    cfg.RefreshTokenTimeout,
		AuthDisabled:           cfg.DisableAuth,
		MarkdownLinkRootPrefix: cfg.MarkdownLinkRootPrefix,
	})
	if err != nil {
		return nil, fmt.Errorf("initialize Wiki: %w", err)
	}
	return w, nil
}

func routerOptionsForRuntime(cfg leafwikiRuntimeConfig, w *wiki.Wiki, basePath string, enableMCP bool, mcpBindHost string) (httpinternal.RouterOptions, error) {
	return routerOptionsForRuntimeWithUserService(cfg, w.UserService(), basePath, enableMCP, mcpBindHost)
}

func controlPlaneRouterOptionsForRuntime(cfg leafwikiRuntimeConfig, w *wiki.Wiki) (httpinternal.RouterOptions, error) {
	return routerOptionsForRuntime(cfg, w, cfg.BasePath, cfg.MCPTransports.HTTP, cfg.Host)
}

func routerOptionsForRuntimeWithUserService(cfg leafwikiRuntimeConfig, userService *coreauth.UserService, basePath string, enableMCP bool, mcpBindHost string) (httpinternal.RouterOptions, error) {
	trustedProxies, err := authmw.ParseTrustedProxies(cfg.TrustedProxyIPsRaw)
	if err != nil {
		return httpinternal.RouterOptions{}, fmt.Errorf("invalid trusted proxies: %w", err)
	}
	return buildHTTPRouterOptions(httpRouterOptionsInput{
		publicAccess:            cfg.PublicAccess,
		injectCodeInHeader:      cfg.InjectCodeInHeader,
		customStylesheet:        cfg.CustomStylesheet,
		allowInsecure:           cfg.AllowInsecure,
		hideLinkMetadataSection: cfg.HideLinkMetadataSection,
		accessTokenTimeout:      cfg.AccessTokenTimeout,
		refreshTokenTimeout:     cfg.RefreshTokenTimeout,
		authDisabled:            cfg.DisableAuth,
		basePath:                basePath,
		markdownLinkRootPrefix:  cfg.MarkdownLinkRootPrefix,
		maxAssetUploadSize:      cfg.MaxAssetUploadSize,
		enableWorkspaceSync:     true,
		enableLinkRefactor:      cfg.EnableLinkRefactor,
		enableMCP:               enableMCP,
		host:                    mcpBindHost,
		httpRemoteUser: httpinternal.HTTPRemoteUserConfig{
			Enabled:        cfg.EnableHTTPRemoteUser,
			HeaderName:     cfg.HTTPRemoteUserHeader,
			TrustedProxies: trustedProxies,
			UserService:    userService,
			LogoutURL:      cfg.HTTPRemoteUserLogoutURL,
		},
		disableRequestLog: cfg.DisableRequestLog,
	}), nil
}

func frontdActorResolver(w *wiki.Wiki, cfg leafwikiRuntimeConfig) func(*http.Request) (projectdaemon.ActorContext, error) {
	return func(req *http.Request) (projectdaemon.ActorContext, error) {
		user, method, err := frontdActorUser(req, w, cfg)
		if err != nil {
			return projectdaemon.ActorContext{}, err
		}
		return actorContextForUser(user, method, cfg)
	}
}

func frontdPublicMCPHandler(cfg leafwikiRuntimeConfig, workspacedURL string, daemonToken string, wikidURL string) (http.Handler, error) {
	proxy, err := frontd.NewMCPProxyWithActor(frontd.WorkspaceProxyOptions{
		Upstream:    workspacedURL,
		DaemonToken: daemonToken,
		Actor:       wikidActorResolver(wikidURL, daemonToken),
	})
	if err != nil {
		return nil, err
	}
	if cfg.DisableAuth {
		return proxy, nil
	}
	return frontdMCPBearerAuthHandler(cfg, wikidURL, daemonToken, proxy), nil
}

func frontdMCPBearerAuthHandler(cfg leafwikiRuntimeConfig, wikidURL string, daemonToken string, next http.Handler) http.Handler {
	if cfg.DisableAuth {
		return next
	}
	return http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		authenticated := sdkauth.RequireBearerToken(wikidMCPTokenVerifier(wikidURL, daemonToken), &sdkauth.RequireBearerTokenOptions{
			ResourceMetadataURL: wikioauth.ProtectedResourceMetadataURL(req, cfg.BasePath),
			Scopes:              []string{wikioauth.ScopeMCP},
		})(next)
		authenticated.ServeHTTP(rw, req)
	})
}

func localOnlyHTTPMCPHandler(next http.Handler) http.Handler {
	return httpinternal.LocalOnlyHandler(next)
}

func wikidActorResolver(wikidURL string, daemonToken string) func(*http.Request) (projectdaemon.ActorContext, error) {
	return func(req *http.Request) (projectdaemon.ActorContext, error) {
		out := struct {
			Actor projectdaemon.ActorContext `json:"actor"`
		}{}
		if err := callWikidPrivateEndpoint(req.Context(), wikidURL, daemonToken, "/__leafwiki/actor-context", req, &out); err != nil {
			return projectdaemon.ActorContext{}, err
		}
		return out.Actor, nil
	}
}

func wikidMCPTokenVerifier(wikidURL string, daemonToken string) sdkauth.TokenVerifier {
	return func(ctx context.Context, token string, req *http.Request) (*sdkauth.TokenInfo, error) {
		verifyReq := req
		if verifyReq == nil {
			verifyReq = &http.Request{Header: http.Header{}}
		}
		clone := verifyReq.Clone(ctx)
		clone.Header = verifyReq.Header.Clone()
		clone.Header.Set("Authorization", "Bearer "+strings.TrimSpace(token))
		out := struct {
			UserID     string    `json:"userId"`
			Scopes     []string  `json:"scopes"`
			Expiration time.Time `json:"expiration"`
		}{}
		if err := callWikidPrivateEndpoint(ctx, wikidURL, daemonToken, "/__leafwiki/token/verify", clone, &out); err != nil {
			return nil, fmt.Errorf("%w: %v", sdkauth.ErrInvalidToken, err)
		}
		return &sdkauth.TokenInfo{UserID: out.UserID, Scopes: out.Scopes, Expiration: out.Expiration}, nil
	}
}

type wikidPrivateEndpointError struct {
	Path       string
	StatusCode int
	Code       sharederrors.ErrorCode
	MessageID  sharederrors.MessageID
	Message    string
}

func (e *wikidPrivateEndpointError) Error() string {
	if e == nil {
		return ""
	}
	msg := strings.TrimSpace(e.Message)
	if msg == "" {
		msg = http.StatusText(e.StatusCode)
	}
	if e.Code != "" {
		return fmt.Sprintf("wikid private endpoint %s failed: status %d code %s: %s", e.Path, e.StatusCode, e.Code, msg)
	}
	return fmt.Sprintf("wikid private endpoint %s failed: status %d: %s", e.Path, e.StatusCode, msg)
}

func newWikidPrivateEndpointError(path string, statusCode int, raw []byte) *wikidPrivateEndpointError {
	msg := strings.TrimSpace(string(raw))
	if msg == "" {
		msg = http.StatusText(statusCode)
	}
	detail := struct {
		Error struct {
			Code      sharederrors.ErrorCode `json:"code"`
			MessageID sharederrors.MessageID `json:"messageId"`
			Message   string                 `json:"message"`
		} `json:"error"`
	}{}
	if err := json.Unmarshal(raw, &detail); err == nil && (detail.Error.Code != "" || detail.Error.Message != "") {
		msg = detail.Error.Message
	}
	return &wikidPrivateEndpointError{
		Path:       path,
		StatusCode: statusCode,
		Code:       detail.Error.Code,
		MessageID:  detail.Error.MessageID,
		Message:    msg,
	}
}

func isWikidPrivateAuthFailure(err error) bool {
	var endpointErr *wikidPrivateEndpointError
	if !errors.As(err, &endpointErr) {
		return false
	}
	return endpointErr.StatusCode == http.StatusUnauthorized ||
		endpointErr.Code == errCodeStdioAuthAPIKeyInvalid ||
		endpointErr.Code == errCodeMCPActorContextInvalid
}

func callWikidPrivateEndpoint(ctx context.Context, wikidURL string, daemonToken string, path string, source *http.Request, out any) error {
	endpoint := strings.TrimRight(wikidURL, "/") + path
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, nil)
	if err != nil {
		return err
	}
	if source != nil {
		req.Header = source.Header.Clone()
		if req.Header == nil {
			req.Header = http.Header{}
		}
		req.Header.Set("X-LeafWiki-Original-Method", originalMethod(source))
		req.Header.Set("X-LeafWiki-Original-Path", originalPath(source))
		req.Header.Set("X-LeafWiki-Original-Remote-Addr", originalRemoteAddr(source))
	}
	req.Header.Set(projectdaemon.ControlTokenHeader, daemonToken)
	req.Header.Del(projectdaemon.ActorContextHeader)
	resp, err := (&http.Client{Timeout: 5 * time.Second}).Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return newWikidPrivateEndpointError(path, resp.StatusCode, raw)
	}
	if out == nil {
		_, _ = io.Copy(io.Discard, resp.Body)
		return nil
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

func frontdMCPTokenVerifier(w *wiki.Wiki) sdkauth.TokenVerifier {
	return func(ctx context.Context, token string, req *http.Request) (*sdkauth.TokenInfo, error) {
		if coreauth.IsAPIKeyBearer(token) {
			if w.APIKeyService() == nil {
				return nil, fmt.Errorf("%w: api key verifier unavailable", sdkauth.ErrInvalidToken)
			}
			verified, err := verifyFrontdAPIKeyForRuntime(w, token)
			if err != nil {
				if !errors.Is(err, coreauth.ErrInvalidToken) {
					return nil, fmt.Errorf("api key verifier failed: %w", err)
				}
				return nil, fmt.Errorf("%w: invalid api key", sdkauth.ErrInvalidToken)
			}
			return &sdkauth.TokenInfo{
				UserID:     leafwikiSDKTokenUserID(verified.User.ID),
				Scopes:     []string{wikioauth.ScopeMCP},
				Expiration: time.Date(9999, 12, 31, 23, 59, 59, 0, time.UTC),
			}, nil
		}
		if w.OAuthService() == nil {
			return nil, fmt.Errorf("%w: oauth verifier unavailable", sdkauth.ErrInvalidToken)
		}
		return verifyFrontdOAuthBearerTokenForRuntime(w, ctx, token, req)
	}
}

func verifyFrontdAPIKey(w *wiki.Wiki, token string) (*coreauth.APIKeyVerification, error) {
	return w.APIKeyService().VerifyAPIKey(token)
}

func verifyFrontdOAuthBearerToken(w *wiki.Wiki, ctx context.Context, token string, req *http.Request) (*sdkauth.TokenInfo, error) {
	return w.OAuthService().VerifyBearerToken(ctx, token, req)
}

func getFrontdUserByID(w *wiki.Wiki, userID coreauth.UserID) (*coreauth.User, error) {
	return w.UserService().GetUserByID(userID)
}
