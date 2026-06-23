package mcp

import (
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
	coreauth "github.com/perber/wiki/internal/core/auth"
	sharederrors "github.com/perber/wiki/internal/core/shared/errors"
	"github.com/perber/wiki/internal/core/tree"
	"github.com/perber/wiki/internal/http/dto"
	"github.com/perber/wiki/internal/projectdaemon"
	wikipages "github.com/perber/wiki/internal/wiki/pages"
)

const publicEditorID = "public-editor"

const (
	errCodeMCPAuthenticatedUserServiceUnavailable sharederrors.ErrorCode = "mcp_authenticated_user_service_unavailable"
	errCodeMCPAuthenticatedUserLookupFailed       sharederrors.ErrorCode = "mcp_authenticated_user_lookup_failed"
	errCodeMCPAuthenticatedUserNotFound           sharederrors.ErrorCode = "mcp_authenticated_user_not_found"
	errCodeMCPActorContextMissing                 sharederrors.ErrorCode = "mcp_actor_context_missing"
	errCodeMCPActorContextInvalid                 sharederrors.ErrorCode = "mcp_actor_context_invalid"
	errCodeMCPTokenInfoMissing                    sharederrors.ErrorCode = "mcp_token_info_missing"
	errCodeMCPEditorRoleRequired                  sharederrors.ErrorCode = "mcp_editor_role_required"
	errCodeMCPToolError                           sharederrors.ErrorCode = "mcp_tool_error"
	errCodeMCPPageIdentifierAmbiguous             sharederrors.ErrorCode = "mcp_page_identifier_ambiguous"
	errCodeMCPPageIdentifierRequired              sharederrors.ErrorCode = "mcp_page_identifier_required"
	errCodeMCPPageTargetAmbiguous                 sharederrors.ErrorCode = "mcp_page_target_ambiguous"
	errCodeMCPPageTargetRequired                  sharederrors.ErrorCode = "mcp_page_target_required"
)

func newMCPHelperError(code sharederrors.ErrorCode, message string, cause error) *sharederrors.LocalizedError {
	return sharederrors.NewLocalizedError(code, message, message, cause)
}

func (r *Routes) apiPage(page *tree.Page, depth int) *dto.Page {
	var apiPage *dto.Page
	if depth == 0 {
		apiPage = dto.ToAPIPage(page, r.userResolver)
	} else {
		apiPage = dto.ToAPIPageWithDepth(page, r.userResolver, depth)
	}
	wikipages.EnrichPageMetadata(apiPage, r.treeService.ReadPageRaw)
	return apiPage
}

func publicEditor() *coreauth.User {
	return &coreauth.User{
		ID:       publicEditorID,
		Username: publicEditorID,
		Role:     coreauth.RoleEditor,
	}
}

func (r *Routes) actorForRequest(req *sdkmcp.CallToolRequest) (*coreauth.User, error) {
	if req == nil {
		return r.actorForMissingTokenInfo()
	}
	extra := req.GetExtra()
	if extra == nil {
		return r.actorForMissingTokenInfo()
	}
	if actor, ok, err := r.actorFromPrivateContextHeader(extra.Header); ok || err != nil {
		return actor, err
	}
	tokenInfo := extra.TokenInfo
	if tokenInfo == nil {
		return r.actorForMissingTokenInfo()
	}
	if r.userService == nil {
		return nil, newMCPHelperError(errCodeMCPAuthenticatedUserServiceUnavailable, "authenticated MCP user service is unavailable", nil)
	}
	user, err := r.userService.GetUserByID(coreauth.NewUserIDUnchecked(tokenInfo.UserID))
	if err != nil {
		if !errors.Is(err, coreauth.ErrUserNotFound) {
			return nil, newMCPHelperError(errCodeMCPAuthenticatedUserLookupFailed, "authenticated MCP user lookup failed", err)
		}
		return nil, newMCPHelperError(errCodeMCPAuthenticatedUserNotFound, "authenticated MCP user not found", nil)
	}
	return user, nil
}

func (r *Routes) actorFromPrivateContextHeader(header http.Header) (*coreauth.User, bool, error) {
	if !r.actorContextAllowed {
		return nil, false, nil
	}
	encoded := strings.TrimSpace(header.Get(projectdaemon.ActorContextHeader))
	if encoded == "" {
		if r.actorContextRequired {
			return nil, true, newMCPHelperError(errCodeMCPActorContextMissing, "private MCP actor context missing", nil)
		}
		return nil, false, nil
	}
	now := time.Now().UTC()
	if r.now != nil {
		now = r.now()
	}
	actor, err := projectdaemon.DecodeActorContext(encoded, projectdaemon.ActorContextValidation{
		Now:         now,
		WorkspaceID: r.workspaceID,
	})
	if err != nil {
		return nil, true, newMCPHelperError(errCodeMCPActorContextInvalid, "private MCP actor context invalid", err)
	}
	return &coreauth.User{
		ID:       actor.SubjectID(),
		Username: actor.Username,
		Email:    actor.Email,
		Role:     actor.Role,
	}, true, nil
}

func (r *Routes) actorForMissingTokenInfo() (*coreauth.User, error) {
	if r.authDisabled {
		return publicEditor(), nil
	}
	if strings.TrimSpace(r.stdioAPIKey) != "" {
		if r.apiKeys == nil {
			return nil, newMCPHelperError(errCodeMCPAuthenticatedUserServiceUnavailable, "authenticated MCP user service is unavailable", nil)
		}
		verified, err := r.apiKeys.VerifyAPIKey(r.stdioAPIKey)
		if err != nil {
			if !errors.Is(err, coreauth.ErrInvalidToken) {
				return nil, newMCPHelperError(errCodeMCPAuthenticatedUserLookupFailed, "authenticated MCP user lookup failed", err)
			}
			return nil, newMCPHelperError(errCodeMCPAuthenticatedUserNotFound, "authenticated MCP user not found", nil)
		}
		return verified.User, nil
	}
	return nil, newMCPHelperError(errCodeMCPTokenInfoMissing, "authenticated MCP token info missing", nil)
}

func (r *Routes) editorActorForRequest(req *sdkmcp.CallToolRequest) (*coreauth.User, error) {
	user, err := r.actorForRequest(req)
	if err != nil {
		return nil, err
	}
	if user.Role != coreauth.RoleEditor && user.Role != coreauth.RoleAdmin {
		return nil, newMCPHelperError(errCodeMCPEditorRoleRequired, "editor or admin role required", nil)
	}
	return user, nil
}

func mcpToolErrorResult(err error) (*sdkmcp.CallToolResult, bool) {
	if loc, ok := sharederrors.AsLocalizedError(err); ok {
		detail := sharederrors.LocalizedErrorDetailFromError(loc)
		return &sdkmcp.CallToolResult{
			Content: []sdkmcp.Content{
				&sdkmcp.TextContent{Text: fmt.Sprintf("%s: %s", detail.Code, detail.Message)},
			},
			Meta:    mcpToolErrorMeta(detail),
			IsError: true,
		}, true
	}
	if detail, _, ok := wikipages.PageErrorDetailForError(err); ok {
		return &sdkmcp.CallToolResult{
			Content: []sdkmcp.Content{
				&sdkmcp.TextContent{Text: fmt.Sprintf("%s: %s", detail.Code, detail.Message)},
			},
			Meta:    mcpToolErrorMeta(detail),
			IsError: true,
		}, true
	}
	detail := sharederrors.NewLocalizedErrorDetail(
		errCodeMCPToolError,
		err.Error(),
		"mcp tool error",
	)
	return &sdkmcp.CallToolResult{
		Content: []sdkmcp.Content{
			&sdkmcp.TextContent{Text: fmt.Sprintf("%s: %s", detail.Code, detail.Message)},
		},
		Meta:    mcpToolErrorMeta(detail),
		IsError: true,
	}, true
}

func mcpToolErrorMeta(detail sharederrors.LocalizedErrorDetail) sdkmcp.Meta {
	errorMeta := map[string]any{
		"code":      detail.Code,
		"messageId": detail.MessageID,
		"message":   detail.Message,
		"template":  detail.Template,
	}
	if len(detail.Args) > 0 {
		errorMeta["args"] = append([]string(nil), detail.Args...)
	}
	return sdkmcp.Meta{"error": errorMeta}
}

func exactlyOneIDOrPageID(id string, pageID string) (tree.PageID, error) {
	id = strings.TrimSpace(id)
	pageID = strings.TrimSpace(pageID)
	if id != "" && pageID != "" {
		return "", sharederrors.NewLocalizedError(
			errCodeMCPPageIdentifierAmbiguous,
			"id and pageId cannot both be supplied",
			"id and pageId cannot both be supplied",
			nil,
		)
	}
	if id == "" && pageID == "" {
		return "", sharederrors.NewLocalizedError(
			errCodeMCPPageIdentifierRequired,
			"id or pageId is required",
			"id or pageId is required",
			nil,
		)
	}
	if pageID != "" {
		return tree.NewPageIDUnchecked(pageID), nil
	}
	return tree.NewPageIDUnchecked(id), nil
}

func normalizeToolRoutePath(path string) string {
	return strings.Trim(strings.TrimSpace(path), "/")
}

func normalizeToolPagePathInput(rawPath string, rawKind string) (tree.RoutePath, tree.NodeKind, error) {
	return wikipages.NormalizePagePathInput(rawPath, rawKind)
}

func base64DecodedSize(encoded string) int64 {
	trimmed := strings.TrimSpace(encoded)
	size := base64.StdEncoding.DecodedLen(len(trimmed))
	switch {
	case strings.HasSuffix(trimmed, "=="):
		size -= 2
	case strings.HasSuffix(trimmed, "="):
		size--
	}
	if size < 0 {
		return 0
	}
	return int64(size)
}
