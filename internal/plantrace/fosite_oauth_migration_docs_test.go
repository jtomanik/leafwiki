package plantrace

import (
	ginkgo "github.com/onsi/ginkgo/v2"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

var _ = ginkgo.It("TestFositeOAuthMigrationDocsPointAtImplementedVerifier", func() {
	t := ginkgo.GinkgoT()
	repoRoot := fositeOAuthMigrationRepoRoot(t)
	raw, err := os.ReadFile(filepath.Join(repoRoot, "docs", "plans", "fosite-oauth-migration.PLAN.md"))
	if err != nil {
		t.Fatalf("read Fosite OAuth migration plan: %v", err)
	}
	plan := string(raw)

	if strings.Contains(plan, "internal/wiki/oauth/fosite_verifier.go") {
		t.Fatal("Fosite OAuth migration plan still references unimplemented fosite_verifier.go")
	}
	for _, required := range []string{
		"internal/wiki/oauth/service.go",
		"VerifyBearerToken",
		"folded into `service.go`",
	} {
		if !strings.Contains(plan, required) {
			t.Fatalf("Fosite OAuth migration plan does not document implemented verifier location: missing %q", required)
		}
	}

})

var _ = ginkgo.It("TestFositeOAuthMigrationDiscoveryDocumentsV1VerifierContract", func() {
	t := ginkgo.GinkgoT()
	repoRoot := fositeOAuthMigrationRepoRoot(t)
	raw, err := os.ReadFile(filepath.Join(repoRoot, "docs", "discovery", "fosite-oauth-migration.md"))
	if err != nil {
		t.Fatalf("read Fosite OAuth migration discovery: %v", err)
	}
	discovery := string(raw)

	for _, stale := range []string{
		"subject, scopes, expiry, client, and optional audience/resource data",
		"subject, scopes, expiry, client, and resource/audience placeholder",
	} {
		if strings.Contains(discovery, stale) {
			t.Fatalf("Fosite OAuth migration discovery overstates verifier output: %q", stale)
		}
	}
	for _, required := range []string{
		"v1 MCP `TokenInfo` contract",
		"user ID, scopes, and expiration",
		"future seam",
	} {
		if !strings.Contains(discovery, required) {
			t.Fatalf("Fosite OAuth migration discovery does not document v1 verifier contract: missing %q", required)
		}
	}

})

func fositeOAuthMigrationRepoRoot(t plantraceTestT) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
}
