package wiki

import (
	"context"
	"fmt"

	"github.com/perber/wiki/internal/core/revision"
	"github.com/perber/wiki/internal/core/tree"
	"github.com/perber/wiki/internal/workspacesync"
)

func (w *Wiki) WorkspaceSyncStatus() workspacesync.SyncStatus {
	if w.workspaceSync == nil {
		return workspacesync.SyncStatus{Enabled: false}
	}
	return w.workspaceSync.Status()
}

func (w *Wiki) WorkspaceSyncRefresh(ctx context.Context, req workspacesync.SyncRequest) (workspacesync.SyncStatus, error) {
	if w.workspaceSync == nil {
		return workspacesync.SyncStatus{Enabled: false}, fmt.Errorf("workspace sync is not enabled")
	}
	return w.workspaceSync.SyncNow(ctx, req)
}

func (w *Wiki) WorkspaceSyncSnapshots(ctx context.Context, limit int) ([]workspacesync.Snapshot, error) {
	if w.workspaceSync == nil {
		return nil, fmt.Errorf("workspace sync is not enabled")
	}
	return w.workspaceSync.ListSnapshots(ctx, limit)
}

func (w *Wiki) WorkspaceSyncSnapshotPage(ctx context.Context, cursor string, limit int) (workspacesync.SnapshotList, error) {
	if w.workspaceSync == nil {
		return workspacesync.SnapshotList{}, fmt.Errorf("workspace sync is not enabled")
	}
	return w.workspaceSync.ListSnapshotPage(ctx, cursor, limit)
}

func (w *Wiki) WorkspaceSyncRestoreWorkspace(ctx context.Context, commitID string, actor workspacesync.Actor, source workspacesync.Source) (workspacesync.SyncStatus, error) {
	if w.workspaceSync == nil {
		return workspacesync.SyncStatus{Enabled: false}, fmt.Errorf("workspace sync is not enabled")
	}
	return w.workspaceSync.RestoreWorkspaceWithSource(ctx, commitID, actor, source)
}

func (w *Wiki) WorkspaceSyncPageRevisions(ctx context.Context, page *tree.Page, cursor string, limit int) (workspacesync.PageRevisionList, error) {
	if w.workspaceSync == nil {
		return workspacesync.PageRevisionList{}, fmt.Errorf("workspace sync is not enabled")
	}
	return w.workspaceSync.ListPageRevisions(ctx, page, cursor, limit)
}

func (w *Wiki) WorkspaceSyncPageRevision(ctx context.Context, page *tree.Page, revisionID string) (*revision.RevisionSnapshot, error) {
	if w.workspaceSync == nil {
		return nil, fmt.Errorf("workspace sync is not enabled")
	}
	return w.workspaceSync.GetPageRevisionSnapshot(ctx, page, revisionID)
}

func (w *Wiki) WorkspaceSyncRestorePageRevision(ctx context.Context, page *tree.Page, revisionID string, actor workspacesync.Actor, source workspacesync.Source) (*tree.Page, error) {
	if w.workspaceSync == nil {
		return nil, fmt.Errorf("workspace sync is not enabled")
	}
	if _, err := w.workspaceSync.RestoreDocumentWithSource(ctx, page, revisionID, actor, source); err != nil {
		return nil, err
	}
	return w.tree.GetPage(page.ID)
}
