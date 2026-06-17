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
		return nil, fmt.Errorf("authenticated MCP user service is unavailable")
	}
	user, err := r.userService.GetUserByID(tokenInfo.UserID)
	if err != nil {
		if !errors.Is(err, coreauth.ErrUserNotFound) {
			return nil, fmt.Errorf("authenticated MCP user lookup failed: %w", err)
		}
		return nil, fmt.Errorf("authenticated MCP user not found")
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
			return nil, true, fmt.Errorf("private MCP actor context missing")
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
		return nil, true, fmt.Errorf("private MCP actor context invalid: %w", err)
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
			return nil, fmt.Errorf("authenticated MCP user service is unavailable")
		}
		verified, err := r.apiKeys.VerifyAPIKey(r.stdioAPIKey)
		if err != nil {
			if !errors.Is(err, coreauth.ErrInvalidToken) {
				return nil, fmt.Errorf("authenticated MCP user lookup failed: %w", err)
			}
			return nil, fmt.Errorf("authenticated MCP user not found")
		}
		return verified.User, nil
	}
	return nil, fmt.Errorf("authenticated MCP token info missing")
}

func (r *Routes) editorActorForRequest(req *sdkmcp.CallToolRequest) (*coreauth.User, error) {
	user, err := r.actorForRequest(req)
	if err != nil {
		return nil, err
	}
	if user.Role != coreauth.RoleEditor && user.Role != coreauth.RoleAdmin {
		return nil, fmt.Errorf("editor or admin role required")
	}
	return user, nil
}

func mcpToolError(err error) error {
	if loc, ok := sharederrors.AsLocalizedError(err); ok {
		return fmt.Errorf("%s: %s", loc.Code, loc.Message)
	}
	if detail, _, ok := wikipages.PageErrorDetailForError(err); ok {
		return fmt.Errorf("%s: %s", detail.Code, detail.Message)
	}
	return err
}

func exactlyOneIDOrPageID(id string, pageID string) (string, error) {
	id = strings.TrimSpace(id)
	pageID = strings.TrimSpace(pageID)
	if id != "" && pageID != "" {
		return "", fmt.Errorf("id and pageId cannot both be supplied")
	}
	if id == "" && pageID == "" {
		return "", fmt.Errorf("id or pageId is required")
	}
	if pageID != "" {
		return pageID, nil
	}
	return id, nil
}

func normalizeToolRoutePath(path string) string {
	return strings.Trim(strings.TrimSpace(path), "/")
}

func normalizeToolPagePathInput(rawPath string, rawKind string) (string, tree.NodeKind, error) {
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
