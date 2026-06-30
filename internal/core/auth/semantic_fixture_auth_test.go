package auth

import (
	"github.com/perber/wiki/internal/core/tree"
)

func newFixtureSessionID[T ~string](raw T) SessionID {
	return NewSessionIDUnchecked(string(raw))
}

func newFixtureAPIKeyID[T ~string](raw T) APIKeyID {
	return NewAPIKeyIDUnchecked(string(raw))
}

func newFixtureUserID[T ~string](raw T) tree.UserID {
	return tree.NewUserIDUnchecked(string(raw))
}
