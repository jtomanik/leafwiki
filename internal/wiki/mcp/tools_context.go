package mcp

import (
	"context"
	"fmt"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/perber/wiki/internal/core/auth"
	"github.com/perber/wiki/internal/core/tree"
	httpinternal "github.com/perber/wiki/internal/http"
	"github.com/perber/wiki/internal/http/dto"
	wikipages "github.com/perber/wiki/internal/wiki/pages"
	wikipresence "github.com/perber/wiki/internal/wiki/presence"
	"github.com/perber/wiki/internal/workspacesync"
)

const (
	contextSyncModeAuto  = "auto"
	contextSyncModeForce = "force"
	contextSyncModeNone  = "none"

	maxContextDeltaSnapshots = 200
	maxRecentChangePaths     = 20

	defaultContextTreeDepth          = 2
	maxContextTreeDepth              = 4
	defaultContextRecentChangesLimit = 20
	maxContextRecentChangesLimit     = 50
)

func (r *Routes) registerContextTools(server *sdkmcp.Server, opts httpinternal.RouterOptions) {
	addRequestTypedTool[getContextInput, contextOutput](server, toolGetContext, func(ctx context.Context, req *sdkmcp.CallToolRequest, in getContextInput) (contextOutput, error) {
		actor, err := r.actorForRequest(req)
		if err != nil {
			return contextOutput{}, err
		}
		return r.getContext(ctx, req, toolActor{ID: actor.ID, User: actor}, opts, in)
	})
}

func (r *Routes) getContext(ctx context.Context, req *sdkmcp.CallToolRequest, actor toolActor, opts httpinternal.RouterOptions, in getContextInput) (contextOutput, error) {
	syncMode := strings.TrimSpace(in.SyncMode)
	if syncMode == "" {
		syncMode = contextSyncModeAuto
	}
	if syncMode != contextSyncModeAuto && syncMode != contextSyncModeForce && syncMode != contextSyncModeNone {
		return contextOutput{}, fmt.Errorf("syncMode must be auto, force, or none")
	}

	status := r.currentWorkspaceSyncStatus()
	skippedContextRefresh := false
	if syncMode != contextSyncModeNone && r.workspaceSyncRefresh != nil && shouldRefreshForContext(syncMode, status) {
		if canRefreshContext(actor.User) {
			refreshed, err := r.workspaceSyncRefresh(ctx, workspacesync.SyncRequest{
				Reason: workspacesync.ReasonExplicit,
				Source: workspacesync.SourceMCP,
				Actor:  workspaceActorForToolActor(actor),
			})
			status = refreshed
			if err != nil {
				status.LastError = err.Error()
			}
		} else {
			skippedContextRefresh = true
		}
	}

	depth := boundedContextTreeDepth(in.TreeDepth)
	limit := boundedRecentChangesLimit(in.RecentChangesLimit)

	recentChanges := r.recentChanges(ctx, status, limit)
	validation := r.validationFromSyncStatus(status)
	sessionKey := contextSessionKey(req, actor)
	warnings := []string{}
	if skippedContextRefresh {
		warnings = append(warnings, "sync refresh skipped because current MCP user is not an editor or admin")
	}
	previousToken := ""
	changesSincePrevious := []recentChangeOutput{}
	explicitSinceToken := strings.TrimSpace(in.SinceToken) != ""
	if token := strings.TrimSpace(in.SinceToken); token != "" {
		if checkpoint, ok := r.contextStore.find(sessionKey, token); ok {
			previousToken = checkpoint.Token
			if checkpoint.CommitHash != status.LastCommitHash {
				changesSincePrevious, ok = r.changesSinceCommit(ctx, status, checkpoint.CommitHash)
				if !ok {
					warnings = append(warnings, "changesSincePreviousContext truncated before sinceToken checkpoint")
				}
			}
		} else {
			warnings = append(warnings, "unknown sinceToken; returned current context")
		}
	}

	checkpoint := contextCheckpoint{
		CreatedAt:  time.Now().UTC(),
		CommitHash: status.LastCommitHash,
	}
	previous, history := r.contextStore.record(sessionKey, checkpoint)
	recorded := history[len(history)-1]
	if !explicitSinceToken && previousToken == "" && previous != nil {
		previousToken = previous.Token
		if previous.CommitHash != status.LastCommitHash {
			var ok bool
			changesSincePrevious, ok = r.changesSinceCommit(ctx, status, previous.CommitHash)
			if !ok {
				warnings = append(warnings, "changesSincePreviousContext truncated before previous context checkpoint")
			}
		}
	}

	tree := r.contextTree(depth)
	activeSessions, presenceStatus := r.activeSessionsForContext(actor.User)
	return contextOutput{
		ContextToken:                recorded.Token,
		PreviousContextToken:        previousToken,
		ChangesSincePreviousContext: changesSincePrevious,
		ContextHistory:              checkpointOutputs(history),
		User:                        actor.User.ToPublicUser(),
		Config:                      configOutputForOptions(opts),
		Server: map[string]any{
			"name":    "leafwiki",
			"version": "local",
			"tools":   serverToolNamesForOptions(opts),
		},
		SyncStatus:       r.syncStatusOutput(status),
		Validation:       validation,
		RecentChanges:    recentChanges,
		ActiveSessions:   activeSessions,
		PresenceStatus:   presenceStatus,
		Tree:             tree,
		RecommendedTools: recommendedToolsForContext(actor.User, validation, status.Enabled),
		CanonicalLinkExamples: []string{
			"Page links use .md: [Guide](/docs/guide.md)",
			"Section links omit .md: [Docs](/docs)",
		},
		Warnings: warnings,
	}, nil
}

func (r *Routes) activeSessionsForContext(viewer *auth.User) ([]wikipresence.Session, presenceStatusOutput) {
	status := presenceStatusOutput{Web: "unavailable", AgentHooks: "unavailable"}
	sessions := []wikipresence.Session{}
	if r.webPresenceProvider != nil {
		webSessions, err := r.webPresenceProvider(viewer)
		if err == nil {
			status.Web = "enabled"
			sessions = append(sessions, webSessions...)
		}
	}
	if r.agentPresenceProvider != nil {
		agentSessions, err := r.agentPresenceProvider()
		if err == nil {
			status.AgentHooks = "enabled"
			for _, agentSession := range agentSessions {
				sessions = append(sessions, wikipresence.Session{
					Type:            "agent",
					SessionID:       agentSession.SessionIDHash,
					Provider:        agentSession.Provider,
					Model:           agentSession.Model,
					Mode:            "unknown",
					State:           "active",
					Source:          agentSession.Source,
					LastEvent:       agentSession.LastEvent,
					ActiveSubagents: agentSession.ActiveSubagents,
					FirstSeenAt:     agentSession.FirstSeenAt,
					LastSeenAt:      agentSession.LastSeenAt,
				})
			}
		}
	}
	sort.Slice(sessions, func(i, j int) bool {
		if sessions[i].Type != sessions[j].Type {
			return sessions[i].Type < sessions[j].Type
		}
		return sessions[i].SessionID < sessions[j].SessionID
	})
	return sessions, status
}

func canRefreshContext(user *auth.User) bool {
	return user != nil && (user.Role == auth.RoleEditor || user.Role == auth.RoleAdmin)
}

func (r *Routes) currentWorkspaceSyncStatus() workspacesync.SyncStatus {
	if r.workspaceSyncStatus == nil {
		return workspacesync.SyncStatus{Enabled: false}
	}
	return r.workspaceSyncStatus()
}

func shouldRefreshForContext(syncMode string, status workspacesync.SyncStatus) bool {
	if syncMode == contextSyncModeForce {
		return true
	}
	if syncMode != contextSyncModeAuto || !status.Enabled {
		return false
	}
	return status.PendingEventCount > 0 || status.LastError != "" || (status.WatcherEnabled && !status.WatcherRunning)
}

func boundedContextTreeDepth(raw *int) int {
	if raw == nil || *raw <= 0 {
		return defaultContextTreeDepth
	}
	if *raw > maxContextTreeDepth {
		return maxContextTreeDepth
	}
	return *raw
}

func boundedRecentChangesLimit(raw *int) int {
	if raw == nil || *raw <= 0 {
		return defaultContextRecentChangesLimit
	}
	if *raw > maxContextRecentChangesLimit {
		return maxContextRecentChangesLimit
	}
	return *raw
}

func (r *Routes) contextTree(depth int) *dto.Node {
	root := r.treeService.GetTree()
	if root == nil {
		return nil
	}
	out := dto.ToAPINodeWithDepth(root, "", r.userResolver, depth)
	ensureNodeChildrenArray(out)
	return out
}

func ensureNodeChildrenArray(node *dto.Node) {
	if node == nil {
		return
	}
	if node.Children == nil {
		node.Children = []*dto.Node{}
	}
	for _, child := range node.Children {
		ensureNodeChildrenArray(child)
	}
}

func (r *Routes) recentChanges(ctx context.Context, status workspacesync.SyncStatus, limit int) []recentChangeOutput {
	if limit <= 0 {
		return []recentChangeOutput{}
	}
	changes := []recentChangeOutput{}
	if r.listWorkspaceSnapshots != nil {
		snapshots, err := r.listWorkspaceSnapshots(ctx, "", limit)
		if err == nil {
			for _, snapshot := range snapshots.Snapshots {
				changes = append(changes, r.recentChangeFromSnapshot(status, snapshot))
			}
		}
	}
	if len(changes) == 0 && len(status.RecentChangedMarkdownPaths) > 0 {
		paths := cappedChangedPaths(status.RecentChangedMarkdownPaths)
		changes = append(changes, recentChangeOutput{
			CommitID:     status.LastCommitHash,
			Timestamp:    formatContextTime(status.LastSyncTime),
			Source:       string(workspacesync.SourceMCP),
			Reason:       string(workspacesync.ReasonExplicit),
			ChangedCount: len(status.RecentChangedMarkdownPaths),
			ChangedPaths: paths,
			PageIDs:      r.pageIDsForMarkdownPaths(paths),
		})
	}
	if len(changes) > limit {
		return changes[:limit]
	}
	return changes
}

func (r *Routes) changesSinceCommit(ctx context.Context, status workspacesync.SyncStatus, commitHash string) ([]recentChangeOutput, bool) {
	if strings.TrimSpace(commitHash) == "" {
		return r.recentChanges(ctx, status, maxContextDeltaSnapshots), true
	}
	if r.listWorkspaceSnapshots == nil {
		return nil, false
	}
	changes := []recentChangeOutput{}
	cursor := ""
	for len(changes) < maxContextDeltaSnapshots {
		remaining := maxContextDeltaSnapshots - len(changes)
		pageSize := 50
		if remaining < pageSize {
			pageSize = remaining
		}
		page, err := r.listWorkspaceSnapshots(ctx, cursor, pageSize)
		if err != nil {
			return changes, false
		}
		for _, snapshot := range page.Snapshots {
			if snapshot.ID == commitHash {
				return changes, true
			}
			changes = append(changes, r.recentChangeFromSnapshot(status, snapshot))
			if len(changes) >= maxContextDeltaSnapshots {
				return changes, false
			}
		}
		if page.NextCursor == "" {
			return changes, false
		}
		cursor = page.NextCursor
	}
	return changes, false
}

func (r *Routes) recentChangeFromSnapshot(status workspacesync.SyncStatus, snapshot workspacesync.Snapshot) recentChangeOutput {
	paths := append([]string{}, snapshot.ChangedMarkdownPaths...)
	if len(paths) == 0 && snapshot.ID == status.LastCommitHash {
		paths = append(paths, status.RecentChangedMarkdownPaths...)
	}
	paths = cappedChangedPaths(paths)
	return recentChangeOutput{
		CommitID:     snapshot.ID,
		Timestamp:    formatContextTime(snapshot.CreatedAt),
		Actor:        snapshot.AuthorName,
		Source:       snapshot.Source,
		Reason:       snapshot.Reason,
		ChangedCount: snapshot.ChangedMarkdownCount,
		ChangedPaths: paths,
		PageIDs:      r.pageIDsForMarkdownPaths(paths),
	}
}

func cappedChangedPaths(paths []string) []string {
	if len(paths) > maxRecentChangePaths {
		paths = paths[:maxRecentChangePaths]
	}
	return append([]string{}, paths...)
}

func (r *Routes) pageIDsForMarkdownPaths(paths []string) []string {
	if r == nil || r.treeService == nil || len(paths) == 0 {
		return nil
	}
	pageIDs := []string{}
	seen := map[string]struct{}{}
	for _, markdownPath := range paths {
		pageID := r.pageIDForMarkdownPath(markdownPath)
		if pageID == "" {
			continue
		}
		if _, exists := seen[pageID]; exists {
			continue
		}
		seen[pageID] = struct{}{}
		pageIDs = append(pageIDs, pageID)
	}
	return pageIDs
}

func (r *Routes) pageIDForMarkdownPath(markdownPath string) string {
	trimmed := strings.Trim(strings.TrimSpace(filepath.ToSlash(markdownPath)), "/")
	if route, err := tree.MapWorkspaceMarkdownRoute(r.workspaceRootDir, trimmed, false); err == nil && !route.Skip {
		if pageID := r.pageIDForRecentChangeRoute(route.RoutePath, route.Kind); pageID != "" {
			return pageID
		}
	}
	if path.Base(trimmed) == "README.md" {
		if page, err := r.treeService.FindPageByRoutePathAndKind(tree.MarkdownPathToRoutePath(trimmed), tree.NodeKindPage); err == nil && page != nil {
			return page.ID
		}
		sectionRoute := strings.Trim(path.Dir(trimmed), ".")
		return r.pageIDForRecentChangeRoute(sectionRoute, tree.NodeKindSection)
	}
	routePath := tree.MarkdownPathToRoutePath(trimmed)
	kind := wikipages.MarkdownPathInputKind(trimmed)
	return r.pageIDForRecentChangeRoute(routePath, kind)
}

func (r *Routes) pageIDForRecentChangeRoute(routePath string, kind tree.NodeKind) string {
	if routePath == "" && kind == tree.NodeKindSection {
		page, err := r.treeService.FindPageByID("root")
		if err != nil || page == nil {
			return ""
		}
		return page.ID
	}
	if kind != "" {
		page, err := r.treeService.FindPageByRoutePathAndKind(routePath, kind)
		if err != nil || page == nil {
			return ""
		}
		return page.ID
	}
	if page, err := r.treeService.FindPageByRoutePath(routePath); err == nil {
		return page.ID
	}
	page, err := r.treeService.FindPageByRoutePathAndKind(routePath, tree.NodeKindSection)
	if err != nil || page == nil {
		return ""
	}
	return page.ID
}

func (r *Routes) validationFromSyncStatus(status workspacesync.SyncStatus) validationOutput {
	return validationFromSyncStatusWithRedactor(status, r.redactWorkspacePaths)
}

func validationFromSyncStatus(status workspacesync.SyncStatus) validationOutput {
	return validationFromSyncStatusWithRedactor(status, nil)
}

func validationFromSyncStatusWithRedactor(status workspacesync.SyncStatus, redact func(string) string) validationOutput {
	issues := make([]validationIssueOutput, 0, len(status.ValidationErrors))
	summary := validationSummaryOutput{}
	for _, err := range status.ValidationErrors {
		severity := strings.TrimSpace(err.Severity)
		if severity == "" {
			severity = "error"
		}
		if severity == "warning" {
			summary.Warnings++
		} else {
			summary.Errors++
		}
		path := err.Path
		message := err.Message
		if redact != nil {
			path = redact(path)
			message = redact(message)
		}
		code := strings.TrimSpace(err.Code)
		if code == "" {
			code = "workspace_sync_validation"
		}
		issues = append(issues, validationIssueOutput{
			Severity: severity,
			Code:     code,
			Path:     path,
			Message:  message,
		})
	}
	return validationOutput{
		OK:      summary.Errors == 0,
		Summary: summary,
		Issues:  issues,
	}
}

func contextSessionKey(req *sdkmcp.CallToolRequest, actor toolActor) string {
	sessionID := ""
	if req != nil && req.Session != nil {
		sessionID = req.Session.ID()
	}
	if sessionID == "" {
		sessionID = "sessionless"
	}
	return actor.ID + ":" + sessionID
}

func workspaceActorForToolActor(actor toolActor) workspacesync.Actor {
	if actor.User == nil {
		return workspacesync.PublicEditorActor()
	}
	return workspacesync.Actor{
		ID:    actor.User.ID,
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

func recommendedToolsForContext(user *auth.User, validation validationOutput, workspaceSyncEnabled bool) []string {
	tools := []string{}
	add := func(names ...string) {
		for _, name := range names {
			if !containsString(tools, name) {
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

func containsString(values []string, needle string) bool {
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
		"lastError":                  r.redactWorkspacePaths(status.LastError),
		"lastCommitHash":             status.LastCommitHash,
		"recentChangedMarkdownPaths": append([]string{}, status.RecentChangedMarkdownPaths...),
		"validationErrors":           r.redactedValidationErrors(status.ValidationErrors),
	}
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
		if variant == "" || variant == "." || variant == string(filepath.Separator) {
			continue
		}
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
