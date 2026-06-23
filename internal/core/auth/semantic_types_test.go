package auth

import (
	"testing"

	"github.com/perber/wiki/internal/core/identity"
)

func TestAuthUserIDUsesNeutralIdentityType(t *testing.T) {
	t.Parallel()

	var _ identity.UserID = NewUserIDUnchecked("user-1")
}
