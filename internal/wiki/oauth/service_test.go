package oauth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/ory/fosite"
)

func TestNewServiceConstructsFositeBackedService(t *testing.T) {
	service, err := NewService(ServiceConfig{
		AccessTokenTimeout:  15 * time.Minute,
		RefreshTokenTimeout: 7 * 24 * time.Hour,
	})
	if err != nil {
		t.Fatalf("NewService failed: %v", err)
	}

	if service.fositeConfig == nil {
		t.Fatalf("service fositeConfig is nil")
	}
	if service.fositeProvider == nil {
		t.Fatalf("service fositeProvider is nil")
	}
	if service.store == nil {
		t.Fatalf("service store is nil")
	}
	client, err := service.store.GetClient(context.Background(), ClientID)
	if err != nil {
		t.Fatalf("fixed client missing from Fosite store: %v", err)
	}
	if got := client.GetID(); got != ClientID {
		t.Fatalf("fixed client ID = %q, want %q", got, ClientID)
	}
}

func TestNewAuthorizeRequestUsesFositeValidationWithFixedRedirectAdapter(t *testing.T) {
	service, err := NewService(ServiceConfig{
		AccessTokenTimeout:  15 * time.Minute,
		RefreshTokenTimeout: 7 * 24 * time.Hour,
	})
	if err != nil {
		t.Fatalf("NewService failed: %v", err)
	}

	redirectURI := "http://localhost:49152/callback"
	values := validAuthorizeRequestValues(redirectURI)
	values.Set("response_type", "code token")
	req := httptest.NewRequest(http.MethodGet, "/oauth/authorize?"+values.Encode(), nil)

	if _, err := service.newAuthorizeRequest(req, redirectURI, values.Get("state")); err == nil {
		t.Fatalf("newAuthorizeRequest accepted unsupported response_type")
	} else if got := fosite.ErrorToRFC6749Error(err).ErrorField; got != "unsupported_response_type" {
		t.Fatalf("newAuthorizeRequest error = %q, want unsupported_response_type", got)
	}

	values.Set("response_type", responseTypeCode)
	req = httptest.NewRequest(http.MethodGet, "/oauth/authorize?"+values.Encode(), nil)
	parsed, err := service.newAuthorizeRequest(req, redirectURI, values.Get("state"))
	if err != nil {
		t.Fatalf("newAuthorizeRequest fixed redirect adapter failed: %v", err)
	}
	if got := parsed.GetRedirectURI().String(); got != redirectURI {
		t.Fatalf("parsed redirect URI = %q, want %q", got, redirectURI)
	}
	client, err := service.store.GetClient(context.Background(), ClientID)
	if err != nil {
		t.Fatalf("fixed client missing after authorize parse: %v", err)
	}
	if got := client.GetRedirectURIs(); len(got) != 0 {
		t.Fatalf("fixed client persisted redirect URIs = %#v, want empty fixed-client adapter state", got)
	}
}

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
