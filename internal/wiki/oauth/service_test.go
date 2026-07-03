package oauth

import (
	"context"
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/gcustom"
	"github.com/onsi/gomega/types"
	"net/http"
	"net/http/httptest"
	"net/url"
	"time"

	"github.com/ory/fosite"
)

var _ = ginkgo.Describe("OAuth service construction", func() {
	ginkgo.It("installs Fosite configuration, provider, store, and fixed client", func() {
		service, err := NewService(ServiceConfig{
			AccessTokenTimeout:  15 * time.Minute,
			RefreshTokenTimeout: 7 * 24 * time.Hour,
		})
		Expect(err).NotTo(HaveOccurred())

		Expect(service).To(haveInstalledFositeServiceComponents())
		client, err := service.store.GetClient(context.Background(), ClientID)
		Expect(err).NotTo(HaveOccurred())
		Expect(client.GetID()).To(Equal(ClientID))
	})

	ginkgo.It("uses Fosite validation with the fixed redirect adapter for authorize requests", func() {
		service, err := NewService(ServiceConfig{
			AccessTokenTimeout:  15 * time.Minute,
			RefreshTokenTimeout: 7 * 24 * time.Hour,
		})
		Expect(err).NotTo(HaveOccurred())

		redirectURI := "http://localhost:49152/callback"
		values := validAuthorizeRequestValues(redirectURI)
		values.Set("response_type", "code token")
		req := httptest.NewRequest(http.MethodGet, "/oauth/authorize?"+values.Encode(), nil)

		_, err = service.newAuthorizeRequest(req, redirectURI, values.Get("state"))
		// Plantrace evidence: Fosite preserves the RFC error field unsupported_response_type.
		Expect(fosite.ErrorToRFC6749Error(err).ErrorField).To(Equal(fosite.ErrUnsupportedResponseType.ErrorField))

		values.Set("response_type", responseTypeCode)
		req = httptest.NewRequest(http.MethodGet, "/oauth/authorize?"+values.Encode(), nil)
		parsed, err := service.newAuthorizeRequest(req, redirectURI, values.Get("state"))
		Expect(err).NotTo(HaveOccurred())
		Expect(parsed.GetRedirectURI().String()).To(Equal(redirectURI))

		client, err := service.store.GetClient(context.Background(), ClientID)
		Expect(err).NotTo(HaveOccurred())
		Expect(client.GetRedirectURIs()).To(BeEmpty())
	})
})

func validAuthorizeRequestValues(redirectURI string) url.Values {
	return url.Values{
		"client_id":             {ClientID},
		"redirect_uri":          {redirectURI},
		"response_type":         {responseTypeCode},
		"scope":                 {ScopeMCP},
		"state":                 {"native-parser-state"},
		"code_challenge":        {"abcdefghijklmnopqrstuvwxyz0123456789ABCDEFGHIJKLMNOPQRSTUV"},
		"code_challenge_method": {"S256"},
	}
}

func haveInstalledFositeServiceComponents() types.GomegaMatcher {
	return gcustom.MakeMatcher(func(service *Service) (bool, error) {
		if service == nil {
			return false, nil
		}
		return service.fositeConfig != nil && service.fositeProvider != nil && service.store != nil, nil
	}).WithMessage("have installed Fosite configuration, provider, and store")
}
