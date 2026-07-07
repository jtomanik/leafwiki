package main

import (
	"net/http"
	"strings"
	"time"

	sdkauth "github.com/modelcontextprotocol/go-sdk/auth"
	coreauth "github.com/perber/wiki/internal/core/auth"
	authmw "github.com/perber/wiki/internal/http/middleware/auth"
	"github.com/perber/wiki/internal/projectdaemon"
	"github.com/perber/wiki/internal/wiki"
	"github.com/perber/wiki/internal/wikid"
	"github.com/perber/wiki/internal/workspaceid"
)

func frontdMCPActorResolver(w *wiki.Wiki, cfg leafwikiRuntimeConfig) func(*http.Request) (projectdaemon.ActorContext, error) {
	if cfg.DisableAuth {
		return frontdActorResolver(w, cfg)
	}
	return func(req *http.Request) (projectdaemon.ActorContext, error) {
		tokenInfo := sdkauth.TokenInfoFromContext(req.Context())
		if tokenInfo == nil || strings.TrimSpace(tokenInfo.UserID) == "" {
			return projectdaemon.ActorContext{}, errFrontdMCPTokenInfoMissing
		}
		if w.UserService() == nil {
			return projectdaemon.ActorContext{}, errFrontdMCPUserServiceUnavailable
		}
		user, err := getFrontdUserByIDForRuntime(w, coreauth.UserIDFromString(tokenInfo.UserID))
		if err != nil {
			return projectdaemon.ActorContext{}, err
		}
		method := "oauth"
		if coreauth.IsAPIKeyBearer(httpBearerToken(req)) {
			method = "api_key"
		}
		return actorContextForUser(user, method, cfg)
	}
}

func frontdActorUser(req *http.Request, w *wiki.Wiki, cfg leafwikiRuntimeConfig) (*coreauth.User, string, error) {
	if cfg.DisableAuth {
		return &coreauth.User{ID: "public-editor", Username: "public-editor", Role: coreauth.RoleEditor}, "disabled", nil
	}
	if user, method, ok, err := frontdRemoteUser(req, w, cfg); ok || err != nil {
		return user, method, err
	}
	if token := httpBearerToken(req); token != "" && coreauth.IsAPIKeyBearer(token) && isMCPActorPath(req.URL.Path) {
		verified, err := verifyFrontdAPIKeyForRuntime(w, token)
		if err != nil {
			return nil, "", err
		}
		return verified.User, "api_key", nil
	}
	if token := httpBearerToken(req); token != "" && isMCPActorPath(req.URL.Path) {
		if w.OAuthService() == nil || w.UserService() == nil {
			return nil, "", errFrontdOAuthActorServicesUnavailable
		}
		info, err := verifyFrontdOAuthBearerTokenForRuntime(w, req.Context(), token, req)
		if err != nil {
			return nil, "", err
		}
		user, err := getFrontdUserByIDForRuntime(w, coreauth.UserIDFromString(info.UserID))
		if err != nil {
			return nil, "", err
		}
		return user, "oauth", nil
	}
	if token := accessTokenFromHTTPRequest(req); token != "" {
		user, err := w.AuthService().ValidateToken(token)
		if err != nil {
			return nil, "", err
		}
		return user, "cookie", nil
	}
	if cfg.PublicAccess && req != nil && req.Method == http.MethodGet {
		return &coreauth.User{ID: "public-viewer", Username: "public-viewer", Role: coreauth.RoleViewer}, "public_access", nil
	}
	return nil, "", errFrontdWorkspaceCredentialsMissing
}

func leafwikiUserSubject(userID coreauth.UserID) string {
	return "user:" + userID.MetadataValue()
}

func leafwikiSDKTokenUserID(userID coreauth.UserID) string {
	return userID.MetadataValue()
}

func actorContextForUser(user *coreauth.User, method string, cfg leafwikiRuntimeConfig) (projectdaemon.ActorContext, error) {
	if user == nil {
		return projectdaemon.ActorContext{}, errRuntimeActorUserRequired
	}
	now := time.Now().UTC()
	return projectdaemon.ActorContext{
		Version:     1,
		Issuer:      projectdaemon.ActorContextIssuerWikid,
		Subject:     leafwikiUserSubject(user.ID),
		Username:    user.Username,
		Email:       user.Email,
		Role:        user.Role,
		Scopes:      []string{"leafwiki:workspace:read", "leafwiki:workspace:write", "leafwiki:mcp"},
		WorkspaceID: runtimeWorkspaceSemanticID(cfg.Workspace),
		AuthMethod:  method,
		IssuedAt:    now,
		ExpiresAt:   now.Add(5 * time.Minute),
	}, nil
}

func actorContextForWorkspaceGrant(user *coreauth.User, method string, cfg leafwikiRuntimeConfig, workspaceID workspaceid.WorkspaceID, role wikid.GrantRole) (projectdaemon.ActorContext, error) {
	actor, err := actorContextForUser(user, method, cfg)
	if err != nil {
		return projectdaemon.ActorContext{}, err
	}
	actor.WorkspaceID = workspaceID
	actor.Role = actorContextRoleForWorkspaceGrant(role)
	actor.Scopes = scopesForGrantRole(role)
	return actor, nil
}

func actorContextRoleForWorkspaceGrant(role wikid.GrantRole) string {
	switch role {
	case wikid.GrantRoleViewer:
		return "viewer"
	case wikid.GrantRoleEditor:
		return "editor"
	case wikid.GrantRoleAdmin:
		return "admin"
	default:
		return ""
	}
}

func scopesForGrantRole(role wikid.GrantRole) []string {
	caps, err := wikid.CapabilitiesForRole(role)
	if err != nil {
		return nil
	}
	var scopes []string
	if caps.ReadContent {
		scopes = append(scopes, "leafwiki:workspace:read", "leafwiki:mcp")
	}
	if caps.WriteContent {
		scopes = append(scopes, "leafwiki:workspace:write")
	}
	if caps.AdministerGrants {
		scopes = append(scopes, "leafwiki:workspace:admin")
	}
	return scopes
}

func frontdRemoteUser(req *http.Request, w *wiki.Wiki, cfg leafwikiRuntimeConfig) (*coreauth.User, string, bool, error) {
	if req == nil || !cfg.EnableHTTPRemoteUser {
		return nil, "", false, nil
	}
	trustedProxies, err := authmw.ParseTrustedProxies(cfg.TrustedProxyIPsRaw)
	if err != nil {
		return nil, "", false, err
	}
	if trustedProxies == nil || !trustedProxies.IsTrusted(req.RemoteAddr) {
		return nil, "", false, nil
	}
	headerName := strings.TrimSpace(cfg.HTTPRemoteUserHeader)
	if headerName == "" {
		headerName = "Remote-User"
	}
	username := strings.TrimSpace(req.Header.Get(headerName))
	if username == "" {
		return nil, "", false, nil
	}
	if w.UserService() == nil {
		return nil, "", true, errFrontdRemoteUserServiceUnavailable
	}
	user, err := w.UserService().GetUserByUsername(username)
	if err != nil {
		return nil, "", true, err
	}
	return user, "remote_user", true, nil
}

func httpBearerToken(req *http.Request) string {
	if req == nil {
		return ""
	}
	header := strings.TrimSpace(req.Header.Get("Authorization"))
	prefix := "Bearer "
	if len(header) < len(prefix) || !strings.EqualFold(header[:len(prefix)], prefix) {
		return ""
	}
	return strings.TrimSpace(header[len(prefix):])
}

func accessTokenFromHTTPRequest(req *http.Request) string {
	if req == nil {
		return ""
	}
	for _, name := range []string{"leafwiki_at", "__Host-leafwiki_at"} {
		cookie, err := req.Cookie(name)
		if err == nil && strings.TrimSpace(cookie.Value) != "" {
			return strings.TrimSpace(cookie.Value)
		}
	}
	return ""
}

func runtimeWorkspaceSemanticID(workspace wiki.Workspace) workspaceid.WorkspaceID {
	if workspace.ID != "" {
		return workspace.ID
	}
	return "current"
}

func originalRemoteAddr(req *http.Request) string {
	if value := strings.TrimSpace(req.Header.Get("X-LeafWiki-Original-Remote-Addr")); value != "" {
		return value
	}
	return req.RemoteAddr
}

func originalMethod(req *http.Request) string {
	if value := strings.TrimSpace(req.Header.Get("X-LeafWiki-Original-Method")); value != "" {
		return value
	}
	return req.Method
}

func originalPath(req *http.Request) string {
	if value := strings.TrimSpace(req.Header.Get("X-LeafWiki-Original-Path")); value != "" {
		return value
	}
	if req.URL != nil && req.URL.Path != "" {
		return req.URL.Path
	}
	return "/"
}

func cloneWithOriginalRequest(req *http.Request) *http.Request {
	if req == nil {
		return nil
	}
	clone := req.Clone(req.Context())
	clone.Body = req.Body
	clone.Method = originalMethod(req)
	clone.URL.Path = originalPath(req)
	clone.URL.RawPath = ""
	clone.RemoteAddr = originalRemoteAddr(req)
	clone.Header = req.Header.Clone()
	clone.Header.Del(projectdaemon.ControlTokenHeader)
	clone.Header.Del(projectdaemon.ActorContextHeader)
	return clone
}

func isMCPActorPath(path string) bool {
	return path == "/mcp" || strings.HasPrefix(path, "/mcp/")
}

func handleWikidActorContext(w http.ResponseWriter, req *http.Request, identity *wiki.Wiki, cfg leafwikiRuntimeConfig, registry *wikid.RegistryService, grants *wikid.GrantStore) {
	clone := cloneWithOriginalRequest(req)
	clone.Body = nil
	user, method, err := frontdActorUser(clone, identity, cfg)
	if err != nil {
		http.Error(w, "resolve actor context", http.StatusUnauthorized)
		return
	}
	workspaceID := runtimeWorkspaceSemanticID(cfg.Workspace)
	if rawWorkspaceID := req.Header.Get(projectdaemon.WorkspaceIDHeader); rawWorkspaceID != "" {
		parsedWorkspaceID, err := workspaceid.ParseWorkspaceID(rawWorkspaceID)
		if err != nil {
			http.Error(w, "workspace not found", http.StatusNotFound)
			return
		}
		workspaceID = parsedWorkspaceID
	}
	if registry != nil {
		if _, ok, err := registry.Workspace(workspaceID); err != nil {
			http.Error(w, "load workspace", http.StatusInternalServerError)
			return
		} else if !ok {
			http.Error(w, "workspace not found", http.StatusNotFound)
			return
		}
	}
	userRole := wikidGrantRoleForCoreRole(user.Role)
	role := userRole
	if grants != nil {
		if err := ensureRuntimeHomeGrant(grants, user); err != nil {
			http.Error(w, "seed home workspace grant", http.StatusInternalServerError)
			return
		}
		if userRole == wikid.GrantRoleAdmin {
			role = wikid.GrantRoleAdmin
		} else {
			subject := leafwikiUserSubject(user.ID)
			userGrants, err := grantsForSubjectForRuntime(grants, subject)
			if err != nil {
				http.Error(w, "load workspace grants", http.StatusInternalServerError)
				return
			}
			role = ""
			for _, grant := range userGrants {
				if grant.WorkspaceID == workspaceID {
					role = effectiveWorkspaceGrantRole(userRole, grant.Role)
					break
				}
			}
			if role == "" {
				writeRuntimeError(w, http.StatusForbidden, runtimeErrorCodeWorkspaceGrantDenied)
				return
			}
		}
	}
	actor, err := actorContextForWorkspaceGrantForRuntime(user, method, cfg, workspaceID, role)
	if err != nil {
		http.Error(w, "encode actor context", http.StatusInternalServerError)
		return
	}
	writeRuntimeJSON(w, map[string]any{"actor": actor})
}

func handleWikidTokenVerify(w http.ResponseWriter, req *http.Request, identity *wiki.Wiki) {
	token := httpBearerToken(req)
	if token == "" {
		http.Error(w, "missing bearer token", http.StatusUnauthorized)
		return
	}
	info, err := frontdMCPTokenVerifier(identity)(req.Context(), token, req)
	if err != nil {
		http.Error(w, "invalid bearer token", http.StatusUnauthorized)
		return
	}
	writeRuntimeJSON(w, map[string]any{
		"userId":     info.UserID,
		"scopes":     info.Scopes,
		"expiration": info.Expiration,
	})
}
