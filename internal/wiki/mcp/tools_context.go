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
	wikivalidation "github.com/perber/wiki/internal/core/markdownvalidation"
	sharederrors "github.com/perber/wiki/internal/core/shared/errors"
	"github.com/perber/wiki/internal/core/tree"
	httpinternal "github.com/perber/wiki/internal/http"
	"github.com/perber/wiki/internal/http/dto"
	wikipages "github.com/perber/wiki/internal/wiki/pages"
	wikipresence "github.com/perber/wiki/internal/wiki/presence"
	"github.com/perber/wiki/internal/workspacesync"
)

const errCodeMCPWorkspaceSyncFailed sharederrors.ErrorCode = "workspace_sync_failed"

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

type treeDisplayDepth int

func (depth treeDisplayDepth) Int() int {
	return int(depth)
}

func (depth treeDisplayDepth) ChildDepth() treeDisplayDepth {
	if depth < 0 {
		return depth
	}
	return depth - 1
}

func (r *Routes) registerContextTools(server *sdkmcp.Server, opts httpinternal.RouterOptions) {
	addRequestTypedTool[getContextInput, contextOutput](server, toolGetContext, func(ctx context.Context, req *sdkmcp.CallToolRequest, in getContextInput) (contextOutput, error) {
		return r.getContextTool(ctx, req, opts, in)
	})
}

func (r *Routes) getContextTool(ctx context.Context, req *sdkmcp.CallToolRequest, opts httpinternal.RouterOptions, in getContextInput) (contextOutput, error) {
	actor, err := r.actorForRequest(req)
	if err != nil {
		return contextOutput{}, err
	}
	return r.getContext(ctx, req, toolActor{ID: actor.ID, User: actor}, opts, in)
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
					Type:            wikipresence.SessionTypeAgent,
					SessionID:       wikipresence.WebSessionIDFromString(agentSession.SessionIDHash),
					Provider:        agentSession.Provider,
					Model:           agentSession.Model,
					Mode:            wikipresence.SessionModeUnknown,
					State:           wikipresence.SessionStateActive,
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

func boundedContextTreeDepth(raw *int) treeDisplayDepth {
	if raw == nil || *raw <= 0 {
		return defaultContextTreeDepth
	}
	if *raw > maxContextTreeDepth {
		return maxContextTreeDepth
	}
	return treeDisplayDepth(*raw)
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

func (r *Routes) contextTree(levels treeDisplayDepth) *dto.Node {
	root := r.treeService.GetTree()
	if root == nil {
		return nil
	}
	out := dto.ToAPINodeWithDepth(root, "", r.userResolver, levels.Int())
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
		snapshots, err := r.listWorkspaceSnapshots(ctx, workspacesync.CommitHashFromString(""), workspacesync.SnapshotLimit(limit))
		if err == nil {
			for _, snapshot := range snapshots.Snapshots {
				changes = append(changes, r.recentChangeFromSnapshot(status, snapshot))
			}
		}
	}
	if len(changes) == 0 && len(status.RecentChangedMarkdownPaths) > 0 {
		paths := cappedChangedPaths(status.RecentChangedMarkdownPaths)
		changes = append(changes, recentChangeOutput{
			CommitID:     status.LastCommitHash.String(),
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

func (r *Routes) changesSinceCommit(ctx context.Context, status workspacesync.SyncStatus, commitHash workspacesync.CommitHash) ([]recentChangeOutput, bool) {
	if commitHash == "" {
		return r.recentChanges(ctx, status, maxContextDeltaSnapshots), true
	}
	if r.listWorkspaceSnapshots == nil {
		return nil, false
	}
	changes := []recentChangeOutput{}
	cursor := workspacesync.CommitHashFromString("")
	for {
		remaining := maxContextDeltaSnapshots - len(changes)
		pageSize := contextSnapshotPageSize(remaining)
		page, err := r.listWorkspaceSnapshots(ctx, cursor, workspacesync.SnapshotLimit(pageSize))
		if err != nil {
			return changes, false
		}
		if len(page.Snapshots) == 0 {
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
}

func contextSnapshotPageSize(remaining int) int {
	if remaining < 50 {
		return remaining
	}
	return 50
}

func (r *Routes) recentChangeFromSnapshot(status workspacesync.SyncStatus, snapshot workspacesync.Snapshot) recentChangeOutput {
	paths := append([]string{}, snapshot.ChangedMarkdownPaths...)
	if len(paths) == 0 && snapshot.ID == status.LastCommitHash {
		paths = append(paths, status.RecentChangedMarkdownPaths...)
	}
	paths = cappedChangedPaths(paths)
	return recentChangeOutput{
		CommitID:     snapshot.ID.String(),
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

func (r *Routes) pageIDsForMarkdownPaths(paths []string) []tree.PageID {
	if r == nil || r.treeService == nil || len(paths) == 0 {
		return nil
	}
	pageIDs := []tree.PageID{}
	seen := map[tree.PageID]struct{}{}
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

func (r *Routes) pageIDForMarkdownPath(markdownPath string) tree.PageID {
	trimmed := strings.Trim(strings.TrimSpace(filepath.ToSlash(markdownPath)), "/")
	if route, err := tree.MapWorkspaceMarkdownRoute(r.workspaceRootDir, trimmed, false); err == nil && !route.Skip {
		if pageID := r.pageIDForRecentChangeRoute(route.RoutePath, route.Kind); pageID != "" {
			return pageID
		}
	}
	if path.Base(trimmed) == "README.md" {
		sectionRouteRaw := strings.Trim(path.Dir(trimmed), ".")
		var sectionRoute tree.RoutePath
		if sectionRouteRaw != "" {
			parsedSectionRoute, err := tree.ParseRoutePath(sectionRouteRaw)
			if err != nil {
				return ""
			}
			sectionRoute = parsedSectionRoute
		}
		return r.pageIDForRecentChangeRoute(sectionRoute, tree.NodeKindSection)
	}
	routePath, err := tree.CleanMarkdownPath(trimmed).RoutePath().Validate()
	if err != nil {
		return ""
	}
	kind := wikipages.MarkdownPathInputKind(tree.MarkdownPathFromString(trimmed))
	return r.pageIDForRecentChangeRoute(routePath, kind)
}

func (r *Routes) pageIDForRecentChangeRoute(routePath tree.RoutePath, kind tree.NodeKind) tree.PageID {
	if routePath == "" && kind == tree.NodeKindSection {
		page, err := r.treeService.FindPageByID(tree.RootPageID)
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
	return ""
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
		severity := err.Severity.Normalize(wikivalidation.IssueSeverityError)
		if severity == wikivalidation.IssueSeverityWarning {
			summary.WarningCount++
		} else {
			summary.Errors++
		}
		path := err.Path
		message := err.Message
		if redact != nil {
			path = redact(path)
			message = redact(message)
		}
		code := err.Code.Normalize(wikivalidation.IssueCodeWorkspaceSyncValidation)
		issues = append(issues, validationIssueOutput{
			Severity:  severity,
			Code:      code,
			MessageID: code.MessageID(),
			Path:      path,
			Message:   message,
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
		ID:    workspacesync.NewActorIDUnchecked(actor.User.ID),
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
