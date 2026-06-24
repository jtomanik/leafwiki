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

func (w *Wiki) WorkspaceSyncSnapshots(ctx context.Context, pageSize workspacesync.SnapshotLimit) ([]workspacesync.Snapshot, error) {
	if w.workspaceSync == nil {
		return nil, fmt.Errorf("workspace sync is not enabled")
	}
	return w.workspaceSync.ListSnapshots(ctx, pageSize)
}

func (w *Wiki) WorkspaceSyncSnapshotPage(ctx context.Context, cursor workspacesync.CommitHash, pageSize workspacesync.SnapshotLimit) (workspacesync.SnapshotList, error) {
	if w.workspaceSync == nil {
		return workspacesync.SnapshotList{}, fmt.Errorf("workspace sync is not enabled")
	}
	return w.workspaceSync.ListSnapshotPage(ctx, cursor, pageSize)
}

func (w *Wiki) WorkspaceSyncRestoreWorkspace(ctx context.Context, commitID workspacesync.CommitHash, actor workspacesync.Actor, source workspacesync.Source) (workspacesync.SyncStatus, error) {
	if w.workspaceSync == nil {
		return workspacesync.SyncStatus{Enabled: false}, fmt.Errorf("workspace sync is not enabled")
	}
	return w.workspaceSync.RestoreWorkspaceWithSource(ctx, commitID, actor, source)
}

func (w *Wiki) WorkspaceSyncPageRevisions(ctx context.Context, page *tree.Page, cursor string, pageSize workspacesync.PageRevisionLimit) (workspacesync.PageRevisionList, error) {
	if w.workspaceSync == nil {
		return workspacesync.PageRevisionList{}, fmt.Errorf("workspace sync is not enabled")
	}
	return w.workspaceSync.ListPageRevisions(ctx, page, cursor, pageSize)
}

func (w *Wiki) WorkspaceSyncPageRevision(ctx context.Context, page *tree.Page, revisionID revision.RevisionID) (*revision.RevisionSnapshot, error) {
	if w.workspaceSync == nil {
		return nil, fmt.Errorf("workspace sync is not enabled")
	}
	return w.workspaceSync.GetPageRevisionSnapshot(ctx, page, workspacesync.CommitHashFromRevisionID(revisionID))
}

func (w *Wiki) WorkspaceSyncRestorePageRevision(ctx context.Context, page *tree.Page, revisionID revision.RevisionID, actor workspacesync.Actor, source workspacesync.Source) (*tree.Page, error) {
	if w.workspaceSync == nil {
		return nil, fmt.Errorf("workspace sync is not enabled")
	}
	if _, err := w.workspaceSync.RestoreDocumentWithSource(ctx, page, workspacesync.CommitHashFromRevisionID(revisionID), actor, source); err != nil {
		return nil, err
	}
	return w.tree.GetPage(page.ID)
}
