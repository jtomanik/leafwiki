package oauth

import (
	"time"

	"github.com/ory/fosite"
	"github.com/ory/fosite/compose"
)

const defaultAuthorizeCodeTTL = 10 * time.Minute

func newFositeConfig(cfg ServiceConfig, secret []byte) *fosite.Config {
	copiedSecret := append([]byte(nil), secret...)
	return &fosite.Config{
		AccessTokenLifespan:                 cfg.AccessTokenTimeout,
		RefreshTokenLifespan:                cfg.RefreshTokenTimeout,
		AuthorizeCodeLifespan:               defaultAuthorizeCodeTTL,
		RefreshTokenScopes:                  []string{},
		EnforcePKCE:                         true,
		EnablePKCEPlainChallengeMethod:      false,
		MinParameterEntropy:                 fosite.MinParameterEntropy,
		GlobalSecret:                        copiedSecret,
		ScopeStrategy:                       fosite.ExactScopeStrategy,
		AudienceMatchingStrategy:            fosite.DefaultAudienceMatchingStrategy,
		SendDebugMessagesToClients:          false,
		UseLegacyErrorFormat:                false,
		GrantTypeJWTBearerCanSkipClientAuth: false,
	}
}

func newFositeStrategy(config *fosite.Config) interface{} {
	return compose.NewOAuth2HMACStrategy(config)
}

func newFositeFactories() []compose.Factory {
	return []compose.Factory{
		compose.OAuth2AuthorizeExplicitFactory,
		compose.OAuth2RefreshTokenGrantFactory,
		compose.OAuth2PKCEFactory,
		compose.OAuth2TokenIntrospectionFactory,
	}
}

func newFositeProvider(config *fosite.Config, storage fosite.Storage) fosite.OAuth2Provider {
	return compose.Compose(config, storage, newFositeStrategy(config), newFositeFactories()...)
}
