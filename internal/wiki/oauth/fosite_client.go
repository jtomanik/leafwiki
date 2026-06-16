package oauth

import (
	"strings"

	"github.com/ory/fosite"
)

const responseTypeCode = "code"

type oauthClient struct {
	id            string
	name          string
	redirectURIs  []string
	grantTypes    []string
	responseTypes []string
	scopes        []string
	audience      []string
	public        bool
	fixedFallback bool
}

func fixedOAuthClient() oauthClient {
	return oauthClient{
		id:            ClientID,
		name:          "LeafWiki local MCP",
		grantTypes:    []string{string(fosite.GrantTypeAuthorizationCode), string(fosite.GrantTypeRefreshToken)},
		responseTypes: []string{responseTypeCode},
		scopes:        []string{ScopeMCP},
		public:        true,
		fixedFallback: true,
	}
}

func oauthClientFromRegistration(id string, client registeredClient) oauthClient {
	return oauthClient{
		id:            id,
		name:          strings.TrimSpace(client.ClientName),
		redirectURIs:  append([]string(nil), client.RedirectURIs...),
		grantTypes:    append([]string(nil), client.GrantTypes...),
		responseTypes: append([]string(nil), client.ResponseTypes...),
		scopes:        fositeScopesForRegisteredClient(client),
		public:        true,
	}
}

func fositeScopesForRegisteredClient(client registeredClient) []string {
	if strings.TrimSpace(client.Scope) == "" {
		return []string{ScopeMCP}
	}
	return strings.Fields(client.Scope)
}

func (c oauthClient) fositeClient() *fosite.DefaultClient {
	return &fosite.DefaultClient{
		ID:            c.id,
		RedirectURIs:  append([]string(nil), c.redirectURIs...),
		GrantTypes:    append([]string(nil), c.grantTypes...),
		ResponseTypes: append([]string(nil), c.responseTypes...),
		Scopes:        append([]string(nil), c.scopes...),
		Audience:      append([]string(nil), c.audience...),
		Public:        c.public,
	}
}
