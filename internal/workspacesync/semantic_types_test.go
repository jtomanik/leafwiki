package workspacesync

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/perber/wiki/internal/core/tree"
)

var _ = Describe("workspace sync commit hash contracts", Label("unit"), func() {
	It("keeps status snapshots and restore APIs typed by commit hash", func() {
		var _ CommitHash = SyncStatus{}.LastCommitHash
		var _ CommitHash = Snapshot{}.ID
		var _ CommitHash = SnapshotList{}.NextCursor
		var _ func(*Service, context.Context, CommitHash, Actor) (SyncStatus, error) = (*Service).RestoreWorkspace
		var _ func(*Service, context.Context, CommitHash, Actor, Source) (SyncStatus, error) = (*Service).RestoreWorkspaceWithSource
	})

	It("derives semantic actor IDs at workspace sync boundaries", func() {
		Expect(actorIDPresenceOf(ActorIDFromUserID(tree.UserIDFromString("   ")))).To(Equal(actorIDAbsent))
		Expect(actorIDPresenceOf(ActorIDFromUserID(tree.UserIDFromString("editor-1")))).To(Equal(actorIDPresent))
	})
})

type actorIDPresence uint8

const (
	actorIDAbsent actorIDPresence = iota
	actorIDPresent
)

func actorIDPresenceOf(actorID ActorID) actorIDPresence {
	if ActorIDIsEmpty(actorID) {
		return actorIDAbsent
	}
	return actorIDPresent
}
