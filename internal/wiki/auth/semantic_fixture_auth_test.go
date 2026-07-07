package auth

import (
	sharederrors "github.com/perber/wiki/internal/core/shared/errors"
	"github.com/perber/wiki/internal/core/tree"
)

func newFixtureUserID[T ~string](raw T) tree.UserID {
	return tree.NewUserIDUnchecked(string(raw))
}

func newFixtureErrorCode[T ~string](raw T) sharederrors.ErrorCode {
	return sharederrors.ErrorCode(raw)
}
