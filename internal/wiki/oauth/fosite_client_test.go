package oauth

import (
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/format"
	"github.com/onsi/gomega/types"

	"github.com/ory/fosite"
)

var _ = ginkgo.Describe("Fosite OAuth clients", ginkgo.Label("unit"), func() {
	ginkgo.It("keeps the fixed loopback client public with redirect compatibility delegated to the adapter", func() {
		client := fixedOAuthClient().fositeClient()

		Expect(client).To(matchPublicOAuthClient(ClientID))
		Expect(client.GetRedirectURIs()).To(BeEmpty())
		Expect(client.GetGrantTypes()).To(haveFositeArguments("authorization_code", "refresh_token"))
		Expect(client.GetResponseTypes()).To(haveFositeArguments("code"))
		Expect(client.GetScopes()).To(haveFositeArguments(ScopeMCP))
		Expect(client.GetAudience()).To(haveFositeArguments())
	})

	ginkgo.It("maps dynamic registration metadata into public Fosite clients", func() {
		client := oauthClientFromRegistration("leafwiki-dcr-test", registeredClient{
			ClientName:    "Codex",
			RedirectURIs:  []string{"http://127.0.0.1:49152/callback"},
			GrantTypes:    []string{"authorization_code", "refresh_token"},
			ResponseTypes: []string{"code"},
			Scope:         ScopeMCP,
		}).fositeClient()

		Expect(client).To(matchPublicOAuthClient("leafwiki-dcr-test"))
		Expect(client.GetRedirectURIs()).To(Equal([]string{"http://127.0.0.1:49152/callback"}))
		Expect(client.GetGrantTypes()).To(haveFositeArguments("authorization_code", "refresh_token"))
		Expect(client.GetResponseTypes()).To(haveFositeArguments("code"))
		Expect(client.GetScopes()).To(haveFositeArguments(ScopeMCP))
		Expect(client.GetAudience()).To(haveFositeArguments())
	})

	ginkgo.It("stores the subject and username without a role snapshot in sessions", func() {
		session := newFositeSession("user-123", "admin")

		Expect(session.GetSubject()).To(Equal("user-123"))
		Expect(session.GetUsername()).To(Equal("admin"))
		Expect(session.Extra).NotTo(HaveKey("role"))
	})
})

func haveFositeArguments(want ...string) types.GomegaMatcher {
	return WithTransform(func(got fosite.Arguments) []string {
		return []string(got)
	}, Equal(want))
}

type publicOAuthClientMatcher struct {
	id string
}

func matchPublicOAuthClient(id string) types.GomegaMatcher {
	return publicOAuthClientMatcher{id: id}
}

func (m publicOAuthClientMatcher) Match(actual interface{}) (bool, error) {
	client, ok := actual.(fosite.Client)
	if !ok {
		return false, nil
	}
	return client.GetID() == m.id && client.IsPublic(), nil
}

func (m publicOAuthClientMatcher) FailureMessage(actual interface{}) string {
	return format.Message(actual, "to be a public OAuth client with ID", m.id)
}

func (m publicOAuthClientMatcher) NegatedFailureMessage(actual interface{}) string {
	return format.Message(actual, "not to be a public OAuth client with ID", m.id)
}
