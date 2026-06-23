package revision

import "github.com/perber/wiki/internal/core/identity"

type RevisionID = identity.RevisionID

func NewRevisionIDUnchecked(raw string) RevisionID {
	return identity.NewRevisionIDUnchecked(raw)
}
