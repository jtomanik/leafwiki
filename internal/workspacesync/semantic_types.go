package workspacesync

import (
	"github.com/perber/wiki/internal/core/identity"
	"github.com/perber/wiki/internal/core/revision"
)

type CommitHash = identity.CommitHash

func NewCommitHashUnchecked(raw string) CommitHash {
	return identity.NewCommitHashUnchecked(raw)
}

func CommitHashFromRevisionID(id revision.RevisionID) CommitHash {
	return identity.NewCommitHashUnchecked(id.CommitID())
}
