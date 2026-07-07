package mcp

import (
	"path/filepath"
	"sort"
	"strings"
	"time"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/perber/wiki/internal/core/auth"
	sharederrors "github.com/perber/wiki/internal/core/shared/errors"
	httpinternal "github.com/perber/wiki/internal/http"
	"github.com/perber/wiki/internal/workspacesync"
)

func contextSessionKey(req *sdkmcp.CallToolRequest, actor toolActor) contextSessionScope {
	var sessionID contextSessionID
	if req != nil && req.Session != nil {
		sessionID = contextSessionIDFromString(req.Session.ID())
	}
	if sessionID.IsEmpty() {
		sessionID = contextSessionIDFromString("sessionless")
	}
	return contextSessionScope{ActorID: actor.ID, SessionID: sessionID}
}

func workspaceActorForToolActor(actor toolActor) workspacesync.Actor {
	if actor.User == nil {
		return workspacesync.PublicEditorActor()
	}
	return workspacesync.Actor{
		ID:    workspacesync.ActorIDFromUserID(actor.ID),
		Name:  actor.User.Username,
		Email: actor.User.Email,
	}
}

func serverToolNamesForOptions(opts httpinternal.RouterOptions) []string {
	tools := append([]string{}, BaseToolNames()...)
	for _, gate := range optionalToolGatesForOptions(opts) {
		tools = append(tools, toolNamesForGate(gate)...)
	}
	return tools
}

func recommendedToolsForContext(user *auth.User, validation validationOutput, workspaceSyncEnabled bool) []ToolID {
	tools := []ToolID{}
	add := func(names ...ToolID) {
		for _, name := range names {
			if !containsToolID(tools, name) {
				tools = append(tools, name)
			}
		}
	}
	if validation.Summary.Errors > 0 {
		add(ToolValidateWiki)
	}
	add(ToolGetSubtree, ToolSearchPages, ToolGetPage, ToolGetPageByPath)
	if user != nil && (user.Role == auth.RoleEditor || user.Role == auth.RoleAdmin) {
		add(ToolUpdatePage, ToolCreatePage)
		if workspaceSyncEnabled {
			add(ToolRefresh)
		}
	}
	return tools
}

func containsToolID(values []ToolID, needle ToolID) bool {
	for _, value := range values {
		if value == needle {
			return true
		}
	}
	return false
}

func (r *Routes) syncStatusOutput(status workspacesync.SyncStatus) map[string]any {
	return map[string]any{
		"enabled":                    status.Enabled,
		"watcherEnabled":             status.WatcherEnabled,
		"watcherRunning":             status.WatcherRunning,
		"pendingEventCount":          status.PendingEventCount,
		"lastSyncTime":               status.LastSyncTime,
		"lastErrorDetail":            mcpWorkspaceSyncLastErrorDetail(r.redactWorkspacePaths(status.LastError)),
		"lastCommitHash":             status.LastCommitHash,
		"recentChangedMarkdownPaths": append([]string{}, status.RecentChangedMarkdownPaths...),
		"validationErrorDetails":     r.redactedValidationErrors(status.ValidationErrors),
	}
}

func mcpWorkspaceSyncLastErrorDetail(lastError string) *sharederrors.LocalizedErrorDetail {
	if strings.TrimSpace(lastError) == "" {
		return nil
	}
	detail := sharederrors.NewLocalizedErrorDetailFromCode(errCodeMCPWorkspaceSyncFailed)
	return &detail
}

func (r *Routes) redactedValidationErrors(errors []workspacesync.ValidationError) []workspacesync.ValidationError {
	out := make([]workspacesync.ValidationError, 0, len(errors))
	for _, validationError := range errors {
		validationError.Path = r.redactWorkspacePaths(validationError.Path)
		validationError.Message = r.redactWorkspacePaths(validationError.Message)
		out = append(out, validationError)
	}
	return out
}

type workspacePathRedaction struct {
	path  string
	label string
}

func (r *Routes) redactWorkspacePaths(text string) string {
	if text == "" || r == nil {
		return text
	}
	redactions := []workspacePathRedaction{
		{path: r.workspaceRootDir, label: "<root-dir>"},
		{path: r.workspaceDataDir, label: "<data-dir>"},
	}
	sort.SliceStable(redactions, func(i, j int) bool {
		return len(redactions[i].path) > len(redactions[j].path)
	})
	out := text
	for _, redaction := range redactions {
		out = redactWorkspacePath(out, redaction.path, redaction.label)
	}
	return out
}

func redactWorkspacePath(text string, rawPath string, label string) string {
	clean := strings.TrimSpace(rawPath)
	if clean == "" {
		return text
	}
	clean = filepath.Clean(clean)
	if clean == "." || clean == string(filepath.Separator) {
		return text
	}
	variants := []string{clean, filepath.ToSlash(clean)}
	out := text
	for _, variant := range variants {
		out = strings.ReplaceAll(out, variant, label)
	}
	return out
}

func formatContextTime(ts time.Time) string {
	if ts.IsZero() {
		return ""
	}
	return ts.UTC().Format(time.RFC3339)
}
