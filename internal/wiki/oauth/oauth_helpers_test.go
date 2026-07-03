package oauth

import (
	"context"
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"net/http"
	"net/http/httptest"
	"os"

	"github.com/ory/fosite"
)

var _ = ginkgo.Describe("OAuth helper contracts", func() {
	ginkgo.It("derives base-path-aware metadata paths and URLs", func() {
		Expect(AuthorizationServerMetadataPath("/wiki")).To(Equal("/.well-known/oauth-authorization-server/wiki"))
		Expect(AuthorizationServerMetadataPaths("/wiki")).To(Equal([]string{"/.well-known/oauth-authorization-server", "/.well-known/oauth-authorization-server/wiki"}))
		Expect(ProtectedResourceMetadataPaths("/wiki")).To(Equal([]string{"/.well-known/oauth-protected-resource", "/.well-known/oauth-protected-resource/mcp", "/.well-known/oauth-protected-resource/wiki/mcp"}))

		req := httptest.NewRequest(http.MethodGet, "http://leafwiki.test/wiki/oauth/authorize?state=abc", nil)
		req.Header.Set("X-Forwarded-Proto", "https")
		Expect(requestOrigin(req)).To(Equal("http://leafwiki.test"))
		Expect(IssuerURL(req, "/wiki")).To(Equal("http://leafwiki.test/wiki"))
		Expect(MCPResourceURL(req, "/wiki")).To(Equal("http://leafwiki.test/wiki/mcp"))
		Expect(ProtectedResourceMetadataURL(req, "/wiki")).To(Equal("http://leafwiki.test/.well-known/oauth-protected-resource/wiki/mcp"))
	})

	ginkgo.It("applies LeafWiki defaults and rejects unsupported dynamic registration values", func() {
		redirects, err := normalizeRedirectURIs([]string{" http://127.0.0.1:49152/callback "})
		Expect(err).NotTo(HaveOccurred())
		Expect(redirects).To(Equal([]string{"http://127.0.0.1:49152/callback"}))
		_, err = normalizeRedirectURIs([]string{""})
		Expect(err).To(HaveOccurred())

		grants, err := normalizeRegistrationGrantTypes(nil)
		Expect(err).NotTo(HaveOccurred())
		Expect(grants).To(Equal([]string{"authorization_code", "refresh_token"}))
		_, err = normalizeRegistrationGrantTypes([]string{"refresh_token"})
		Expect(err).To(HaveOccurred())
		responses, err := normalizeRegistrationResponseTypes(nil)
		Expect(err).NotTo(HaveOccurred())
		Expect(responses).To(Equal([]string{"code"}))
		_, err = normalizeRegistrationResponseTypes([]string{"token"})
		Expect(err).To(HaveOccurred())
		scope, err := normalizeRegistrationScope("  " + ScopeMCP + "  ")
		Expect(err).NotTo(HaveOccurred())
		Expect(scope).To(Equal(ScopeMCP))
	})

	ginkgo.It("revokes access and refresh sessions by request ID", func() {
		ctx := context.Background()
		store := newFositeStore()
		requester := newStoreTestRequester("revoke-request")

		Expect(store.CreateAccessTokenSession(ctx, "access-revoke", requester)).To(Succeed())
		Expect(store.CreateRefreshTokenSession(ctx, "refresh-revoke", "access-revoke", requester)).To(Succeed())
		Expect(store.RevokeRefreshToken(ctx, "revoke-request")).To(Succeed())
		revokedRefresh, err := store.GetRefreshTokenSession(ctx, "refresh-revoke", newFositeSession("", ""))
		Expect(err).To(MatchError(fosite.ErrInactiveToken))
		Expect(revokedRefresh.GetID()).To(Equal("revoke-request"))
		Expect(store.RevokeAccessToken(ctx, "revoke-request")).To(Succeed())
		_, err = store.GetAccessTokenSession(ctx, "access-revoke", newFositeSession("", ""))
		Expect(err).To(MatchError(fosite.ErrNotFound))
	})
})

func oauthTempDir() string {
	ginkgo.GinkgoHelper()
	dir, err := os.MkdirTemp("", "leafwiki-oauth-*")
	Expect(err).NotTo(HaveOccurred())
	ginkgo.DeferCleanup(os.RemoveAll, dir)
	return dir
}
