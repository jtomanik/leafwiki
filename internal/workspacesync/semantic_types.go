package workspacesync

import (
	"github.com/perber/wiki/internal/core/identity"
	"github.com/perber/wiki/internal/core/revision"
)

type CommitHash = identity.CommitHash

type SnapshotLimit int
type PageRevisionLimit int

func NewCommitHashUnchecked(raw string) CommitHash {
	return identity.NewCommitHashUnchecked(raw)
}

func CommitHashFromString[T ~string](raw T) CommitHash {
	return identity.CommitHashFromString(raw)
}

func CommitHashFromRevisionID(id revision.RevisionID) CommitHash {
	return identity.NewCommitHashUnchecked(id.CommitID())
}
