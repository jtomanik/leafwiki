package oauth

import (
	ginkgo "github.com/onsi/ginkgo/v2"
	"context"
	"reflect"
	"runtime"
	"time"

	"github.com/ory/fosite"
)

var _ = ginkgo.It("TestFositeConfigUsesLeafWikiOAuthDefaults", func() {
	t := ginkgo.GinkgoT()
	ctx := context.Background()
	cfg := ServiceConfig{
		AccessTokenTimeout:  15 * time.Minute,
		RefreshTokenTimeout: 7 * 24 * time.Hour,
	}

	fositeConfig := newFositeConfig(cfg, []byte("leafwiki-fosite-test-secret-32-bytes"))

	if got := fositeConfig.GetAccessTokenLifespan(ctx); got != cfg.AccessTokenTimeout {
		t.Fatalf("access token lifespan = %s, want %s", got, cfg.AccessTokenTimeout)
	}
	if got := fositeConfig.GetRefreshTokenLifespan(ctx); got != cfg.RefreshTokenTimeout {
		t.Fatalf("refresh token lifespan = %s, want %s", got, cfg.RefreshTokenTimeout)
	}
	if got := fositeConfig.GetAuthorizeCodeLifespan(ctx); got != defaultAuthorizeCodeTTL {
		t.Fatalf("authorize code lifespan = %s, want %s", got, defaultAuthorizeCodeTTL)
	}
	if got := fositeConfig.GetRefreshTokenScopes(ctx); got == nil || len(got) != 0 {
		t.Fatalf("refresh token scopes = %#v, want explicit empty slice", got)
	}
	if !fositeConfig.GetEnforcePKCE(ctx) {
		t.Fatalf("PKCE is not enforced")
	}
	if fositeConfig.GetEnablePKCEPlainChallengeMethod(ctx) {
		t.Fatalf("plain PKCE is enabled, want disabled")
	}
	if got := fositeConfig.GetMinParameterEntropy(ctx); got != fosite.MinParameterEntropy {
		t.Fatalf("min parameter entropy = %d, want %d", got, fosite.MinParameterEntropy)
	}

})

var _ = ginkgo.It("TestFositeConfigUsesHMACOpaqueStrategy", func() {
	t := ginkgo.GinkgoT()
	fositeConfig := newFositeConfig(ServiceConfig{}, []byte("leafwiki-fosite-test-secret-32-bytes"))

	strategy := newFositeStrategy(fositeConfig)

	if got := reflect.TypeOf(strategy).String(); got != "*oauth2.HMACSHAStrategy" {
		t.Fatalf("strategy has type %s, want *oauth2.HMACSHAStrategy", got)
	}

})

var _ = ginkgo.It("TestFositeConfigUsesNarrowProviderComposition", func() {
	t := ginkgo.GinkgoT()
	factories := newFositeFactories()

	got := make([]string, 0, len(factories))
	for _, factory := range factories {
		got = append(got, runtime.FuncForPC(reflect.ValueOf(factory).Pointer()).Name())
	}
	want := []string{
		"github.com/ory/fosite/compose.OAuth2AuthorizeExplicitFactory",
		"github.com/ory/fosite/compose.OAuth2RefreshTokenGrantFactory",
		"github.com/ory/fosite/compose.OAuth2PKCEFactory",
		"github.com/ory/fosite/compose.OAuth2TokenIntrospectionFactory",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("provider factories = %#v, want %#v", got, want)
	}

})
