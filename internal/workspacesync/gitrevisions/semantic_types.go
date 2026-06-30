package gitrevisions

import (
	"strings"

	"github.com/go-git/go-git/v6/plumbing"

	"github.com/perber/wiki/internal/core/identity"
)

type ActorID = identity.UserID

func NewActorIDUnchecked(raw string) ActorID {
	return ParseActorID(raw)
}

func ParseActorID(raw string) ActorID {
	return identity.UserIDFromString(strings.TrimSpace(raw))
}

func TrimActorID(id ActorID) ActorID {
	return identity.UserIDFromString(id.ActorID())
}

func ActorIDGitSignatureName(id ActorID) string {
	return id.ActorID()
}

func ActorIDGitSignatureEmail(id ActorID) string {
	return id.ActorID() + "@leafwiki.local"
}

func CommitHashFromPlumbingHash(hash plumbing.Hash) identity.CommitHash {
	return identity.CommitHashFromString(hash.String())
}

func PlumbingHashFromCommitHash(hash identity.CommitHash) plumbing.Hash {
	return plumbing.NewHash(hash.String())
}
