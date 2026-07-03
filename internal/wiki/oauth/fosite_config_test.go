package oauth

import (
	"context"
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"reflect"
	"runtime"
	"time"

	"github.com/ory/fosite"
)

var _ = ginkgo.Describe("Fosite OAuth configuration", func() {
	ginkgo.It("uses LeafWiki token lifetimes, PKCE policy, and entropy defaults", func() {
		ctx := context.Background()
		cfg := ServiceConfig{
			AccessTokenTimeout:  15 * time.Minute,
			RefreshTokenTimeout: 7 * 24 * time.Hour,
		}

		fositeConfig := newFositeConfig(cfg, []byte("leafwiki-fosite-test-secret-32-bytes"))

		Expect(fositeConfig.GetAccessTokenLifespan(ctx)).To(Equal(cfg.AccessTokenTimeout))
		Expect(fositeConfig.GetRefreshTokenLifespan(ctx)).To(Equal(cfg.RefreshTokenTimeout))
		Expect(fositeConfig.GetAuthorizeCodeLifespan(ctx)).To(Equal(defaultAuthorizeCodeTTL))
		Expect(fositeConfig.GetRefreshTokenScopes(ctx)).To(BeEmpty())
		Expect(fositeConfig.GetEnforcePKCE(ctx)).To(BeTrue())
		Expect(fositeConfig.GetEnablePKCEPlainChallengeMethod(ctx)).To(BeFalse())
		Expect(fositeConfig.GetMinParameterEntropy(ctx)).To(Equal(fosite.MinParameterEntropy))
	})

	ginkgo.It("uses an HMAC SHA strategy for opaque token handling", func() {
		fositeConfig := newFositeConfig(ServiceConfig{}, []byte("leafwiki-fosite-test-secret-32-bytes"))

		strategy := newFositeStrategy(fositeConfig)

		Expect(reflect.TypeOf(strategy).String()).To(Equal("*oauth2.HMACSHAStrategy"))
	})

	ginkgo.It("composes only the OAuth providers LeafWiki supports", func() {
		factories := newFositeFactories()

		got := make([]string, 0, len(factories))
		for _, factory := range factories {
			got = append(got, runtime.FuncForPC(reflect.ValueOf(factory).Pointer()).Name())
		}
		Expect(got).To(Equal([]string{
			"github.com/ory/fosite/compose.OAuth2AuthorizeExplicitFactory",
			"github.com/ory/fosite/compose.OAuth2RefreshTokenGrantFactory",
			"github.com/ory/fosite/compose.OAuth2PKCEFactory",
			"github.com/ory/fosite/compose.OAuth2TokenIntrospectionFactory",
		}))
	})
})
