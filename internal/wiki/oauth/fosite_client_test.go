package oauth

import (
	ginkgo "github.com/onsi/ginkgo/v2"
	"reflect"

	"github.com/ory/fosite"
)

var _ = ginkgo.It("TestFositeClientFixedFallbackPreservesLoopbackRedirectCompatibility", func() {
	t := ginkgo.GinkgoT()
	client := fixedOAuthClient().fositeClient()

	if got := client.GetID(); got != ClientID {
		t.Fatalf("client ID = %q, want %q", got, ClientID)
	}
	if !client.IsPublic() {
		t.Fatalf("fixed client is confidential, want public")
	}
	if got := client.GetRedirectURIs(); len(got) != 0 {
		t.Fatalf("fixed client redirect URIs = %#v, want empty list for LeafWiki compatibility adapter", got)
	}
	assertFositeArguments(t, client.GetGrantTypes(), []string{"authorization_code", "refresh_token"})
	assertFositeArguments(t, client.GetResponseTypes(), []string{"code"})
	assertFositeArguments(t, client.GetScopes(), []string{ScopeMCP})
	assertFositeArguments(t, client.GetAudience(), nil)

})

var _ = ginkgo.It("TestFositeClientMapsDynamicRegistration", func() {
	t := ginkgo.GinkgoT()
	client := oauthClientFromRegistration("leafwiki-dcr-test", registeredClient{
		ClientName:    "Codex",
		RedirectURIs:  []string{"http://127.0.0.1:49152/callback"},
		GrantTypes:    []string{"authorization_code", "refresh_token"},
		ResponseTypes: []string{"code"},
		Scope:         ScopeMCP,
	}).fositeClient()

	if got := client.GetID(); got != "leafwiki-dcr-test" {
		t.Fatalf("client ID = %q, want generated DCR ID", got)
	}
	if !client.IsPublic() {
		t.Fatalf("DCR client is confidential, want public")
	}
	if got := client.GetRedirectURIs(); !reflect.DeepEqual(got, []string{"http://127.0.0.1:49152/callback"}) {
		t.Fatalf("DCR redirect URIs = %#v", got)
	}
	assertFositeArguments(t, client.GetGrantTypes(), []string{"authorization_code", "refresh_token"})
	assertFositeArguments(t, client.GetResponseTypes(), []string{"code"})
	assertFositeArguments(t, client.GetScopes(), []string{ScopeMCP})
	assertFositeArguments(t, client.GetAudience(), nil)

})

var _ = ginkgo.It("TestFositeSessionStoresSubjectWithoutRoleSnapshot", func() {
	t := ginkgo.GinkgoT()
	session := newFositeSession("user-123", "admin")

	if got := session.GetSubject(); got != "user-123" {
		t.Fatalf("session subject = %q, want user ID", got)
	}
	if got := session.GetUsername(); got != "admin" {
		t.Fatalf("session username = %q, want admin", got)
	}
	if _, ok := session.Extra["role"]; ok {
		t.Fatalf("session stores role snapshot in Extra: %#v", session.Extra)
	}

})

func assertFositeArguments(t oauthTestT, got fosite.Arguments, want []string) {
	t.Helper()

	if !reflect.DeepEqual([]string(got), want) {
		t.Fatalf("arguments = %#v, want %#v", []string(got), want)
	}
}
