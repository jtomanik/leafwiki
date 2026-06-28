package oauth

import (
	"context"
	"errors"
	ginkgo "github.com/onsi/ginkgo/v2"
	"net/http"
	"net/http/httptest"
	"reflect"

	"github.com/ory/fosite"
)

var _ = ginkgo.It("OAuth metadata helpers derive base-path-aware paths and URLs", func() {
	t := ginkgo.GinkgoT()
	if got, want := AuthorizationServerMetadataPath("/wiki"), "/.well-known/oauth-authorization-server/wiki"; got != want {
		t.Fatalf("AuthorizationServerMetadataPath = %q, want %q", got, want)
	}
	if got, want := AuthorizationServerMetadataPaths("/wiki"), []string{"/.well-known/oauth-authorization-server", "/.well-known/oauth-authorization-server/wiki"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("AuthorizationServerMetadataPaths = %#v, want %#v", got, want)
	}
	if got, want := ProtectedResourceMetadataPaths("/wiki"), []string{"/.well-known/oauth-protected-resource", "/.well-known/oauth-protected-resource/mcp", "/.well-known/oauth-protected-resource/wiki/mcp"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("ProtectedResourceMetadataPaths = %#v, want %#v", got, want)
	}

	req := httptest.NewRequest(http.MethodGet, "http://leafwiki.test/wiki/oauth/authorize?state=abc", nil)
	req.Header.Set("X-Forwarded-Proto", "https")
	if got, want := requestOrigin(req), "http://leafwiki.test"; got != want {
		t.Fatalf("requestOrigin = %q, want %q", got, want)
	}
	if got, want := IssuerURL(req, "/wiki"), "http://leafwiki.test/wiki"; got != want {
		t.Fatalf("IssuerURL = %q, want %q", got, want)
	}
	if got, want := MCPResourceURL(req, "/wiki"), "http://leafwiki.test/wiki/mcp"; got != want {
		t.Fatalf("MCPResourceURL = %q, want %q", got, want)
	}
	if got, want := ProtectedResourceMetadataURL(req, "/wiki"), "http://leafwiki.test/.well-known/oauth-protected-resource/wiki/mcp"; got != want {
		t.Fatalf("ProtectedResourceMetadataURL = %q, want %q", got, want)
	}
})

var _ = ginkgo.It("OAuth dynamic registration normalization applies LeafWiki defaults and rejects unsupported values", func() {
	t := ginkgo.GinkgoT()
	redirects, err := normalizeRedirectURIs([]string{" http://127.0.0.1:49152/callback "})
	if err != nil {
		t.Fatalf("normalizeRedirectURIs failed: %v", err)
	}
	if got, want := redirects, []string{"http://127.0.0.1:49152/callback"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("redirects = %#v, want %#v", got, want)
	}
	if _, err := normalizeRedirectURIs([]string{""}); err == nil {
		t.Fatalf("normalizeRedirectURIs accepted empty value")
	}

	grants, err := normalizeRegistrationGrantTypes(nil)
	if err != nil {
		t.Fatalf("normalizeRegistrationGrantTypes defaults failed: %v", err)
	}
	if got, want := grants, []string{"authorization_code", "refresh_token"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("grant defaults = %#v, want %#v", got, want)
	}
	if _, err := normalizeRegistrationGrantTypes([]string{"refresh_token"}); err == nil {
		t.Fatalf("normalizeRegistrationGrantTypes accepted missing authorization_code")
	}
	responses, err := normalizeRegistrationResponseTypes(nil)
	if err != nil {
		t.Fatalf("normalizeRegistrationResponseTypes defaults failed: %v", err)
	}
	if got, want := responses, []string{"code"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("response defaults = %#v, want %#v", got, want)
	}
	if _, err := normalizeRegistrationResponseTypes([]string{"token"}); err == nil {
		t.Fatalf("normalizeRegistrationResponseTypes accepted token")
	}
	scope, err := normalizeRegistrationScope("  " + ScopeMCP + "  ")
	if err != nil {
		t.Fatalf("normalizeRegistrationScope failed: %v", err)
	}
	if scope != ScopeMCP {
		t.Fatalf("scope = %q, want %q", scope, ScopeMCP)
	}
})

var _ = ginkgo.It("Fosite store revoke methods revoke access and refresh sessions by request ID", func() {
	t := ginkgo.GinkgoT()
	ctx := context.Background()
	store := newFositeStore()
	requester := newStoreTestRequester("revoke-request")

	if err := store.CreateAccessTokenSession(ctx, "access-revoke", requester); err != nil {
		t.Fatalf("CreateAccessTokenSession failed: %v", err)
	}
	if err := store.CreateRefreshTokenSession(ctx, "refresh-revoke", "access-revoke", requester); err != nil {
		t.Fatalf("CreateRefreshTokenSession failed: %v", err)
	}
	if err := store.RevokeRefreshToken(ctx, "revoke-request"); err != nil {
		t.Fatalf("RevokeRefreshToken failed: %v", err)
	}
	revokedRefresh, err := store.GetRefreshTokenSession(ctx, "refresh-revoke", newFositeSession("", ""))
	if !errors.Is(err, fosite.ErrInactiveToken) {
		t.Fatalf("revoked refresh error = %v, want ErrInactiveToken", err)
	}
	if revokedRefresh == nil || revokedRefresh.GetID() != "revoke-request" {
		t.Fatalf("revoked refresh requester = %#v, want stored requester", revokedRefresh)
	}
	if err := store.RevokeAccessToken(ctx, "revoke-request"); err != nil {
		t.Fatalf("RevokeAccessToken failed: %v", err)
	}
	if _, err := store.GetAccessTokenSession(ctx, "access-revoke", newFositeSession("", "")); !errors.Is(err, fosite.ErrNotFound) {
		t.Fatalf("revoked access error = %v, want ErrNotFound", err)
	}
})
