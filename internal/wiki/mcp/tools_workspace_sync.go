package mcp

import (
	"context"
	"fmt"
	"strings"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/perber/wiki/internal/workspacesync"
)

func (r *Routes) registerWorkspaceSyncTools(server *sdkmcp.Server) {
	addEditorTool[refreshInput, refreshOutput](r, server, toolRefresh, func(ctx context.Context, actor toolActor, in refreshInput) (refreshOutput, error) {
		return r.refreshWorkspaceSync(ctx, actor, in)
	})
}

func (r *Routes) refreshWorkspaceSync(ctx context.Context, actor toolActor, in refreshInput) (refreshOutput, error) {
	if r.workspaceSyncRefresh == nil {
		return refreshOutput{}, fmt.Errorf("workspace sync is not enabled")
	}
	source, err := refreshSource(in.Source)
	if err != nil {
		return refreshOutput{}, err
	}
	status, err := r.workspaceSyncRefresh(ctx, workspacesync.SyncRequest{
		Reason: workspacesync.ReasonExplicitRefresh,
		Source: source,
		Actor:  workspaceActorForToolActor(actor),
	})
	if err != nil {
		status.LastError = err.Error()
		return refreshOutput{}, err
	}
	out := refreshOutput{
		SyncStatus:         r.syncStatusOutput(status),
		RecentChangedPaths: append([]string{}, status.RecentChangedMarkdownPaths...),
		LastCommitHash:     status.LastCommitHash,
	}
	if includeValidation(in.Validate) {
		validation := r.validationFromSyncStatus(status)
		out.Validation = &validation
	}
	return out, nil
}

func refreshSource(raw string) (workspacesync.Source, error) {
	switch strings.TrimSpace(raw) {
	case "", string(workspacesync.SourceMCP):
		return workspacesync.SourceMCP, nil
	case string(workspacesync.SourceFilesystem):
		return workspacesync.SourceFilesystem, nil
	default:
		return "", fmt.Errorf("source must be mcp or filesystem")
	}
}
