package auth

import (
	ginkgo "github.com/onsi/ginkgo/v2"

	"github.com/perber/wiki/internal/core/identity"
)

var _ = ginkgo.Describe("semantic types", func() {
	ginkgo.It("uses the neutral identity type for auth user IDs", func() {
		var _ identity.UserID = newFixtureUserID("user-1")
	})
})
