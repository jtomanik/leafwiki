package auth

import (
	"github.com/perber/wiki/internal/core/tree"
)

func newFixtureUserID[T ~string](raw T) tree.UserID {
	return tree.NewUserIDUnchecked(string(raw))
}
