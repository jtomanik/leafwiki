package plantrace

import (
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"os"
	"path/filepath"
	"runtime"
)

var _ = ginkgo.Describe("Fosite OAuth migration documentation", func() {
	ginkgo.It("points the migration plan at the implemented verifier location", func() {
		repoRoot := fositeOAuthMigrationRepoRoot()
		raw, err := os.ReadFile(filepath.Join(repoRoot, "docs", "plans", "fosite-oauth-migration.PLAN.md"))
		Expect(err).NotTo(HaveOccurred(), "read Fosite OAuth migration plan")
		plan := string(raw)

		Expect(plan).NotTo(ContainSubstring("internal/wiki/oauth/fosite_verifier.go"))
		for _, required := range []string{
			"internal/wiki/oauth/service.go",
			"VerifyBearerToken",
			"folded into `service.go`",
		} {
			Expect(plan).To(ContainSubstring(required), "migration plan should document implemented verifier location")
		}

	})

	ginkgo.It("keeps the discovery note scoped to the current v1 verifier contract", func() {
		repoRoot := fositeOAuthMigrationRepoRoot()
		raw, err := os.ReadFile(filepath.Join(repoRoot, "docs", "discovery", "fosite-oauth-migration.md"))
		Expect(err).NotTo(HaveOccurred(), "read Fosite OAuth migration discovery")
		discovery := string(raw)

		for _, stale := range []string{
			"subject, scopes, expiry, client, and optional audience/resource data",
			"subject, scopes, expiry, client, and resource/audience placeholder",
		} {
			Expect(discovery).NotTo(ContainSubstring(stale), "discovery should not overstate verifier output")
		}
		for _, required := range []string{
			"v1 MCP `TokenInfo` contract",
			"user ID, scopes, and expiration",
			"future seam",
		} {
			Expect(discovery).To(ContainSubstring(required), "discovery should document v1 verifier contract")
		}

	})
})

func fositeOAuthMigrationRepoRoot() string {
	ginkgo.GinkgoHelper()
	_, file, _, ok := runtime.Caller(0)
	Expect(ok).To(BeTrue(), "runtime.Caller should locate the Fosite migration doc test")
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
}
