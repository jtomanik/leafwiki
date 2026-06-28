package auth

import (
	ginkgo "github.com/onsi/ginkgo/v2"

	"github.com/perber/wiki/internal/core/identity"
)

var _ = ginkgo.Describe("semantic types", func() {
	ginkgo.It("TestAuthUserIDUsesNeutralIdentityType", func() {
		t := ginkgo.GinkgoT()
		t.Parallel()

		var _ identity.UserID = newFixtureUserID("user-1")
	})
})
