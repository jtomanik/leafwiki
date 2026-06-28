package workspacesync

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
)

var _ = It("WorkspaceSyncCommitHashContracts", func() {
	var _ CommitHash = SyncStatus{}.LastCommitHash
	var _ CommitHash = Snapshot{}.ID
	var _ CommitHash = SnapshotList{}.NextCursor
	var _ func(*Service, context.Context, CommitHash, Actor) (SyncStatus, error) = (*Service).RestoreWorkspace
	var _ func(*Service, context.Context, CommitHash, Actor, Source) (SyncStatus, error) = (*Service).RestoreWorkspaceWithSource
})
