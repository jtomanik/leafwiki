package projectdaemon

import (
	"strings"
	"testing"
	"time"
)

func TestActorContextRoundTripValidatesPrivateEnvelope(t *testing.T) {
	now := time.Date(2026, 6, 16, 12, 0, 0, 0, time.UTC)
	ctx := ActorContext{
		Version:     1,
		Issuer:      ActorContextIssuerWikid,
		Subject:     "user:admin",
		Username:    "admin",
		Role:        "admin",
		Scopes:      []string{"leafwiki:workspace:read", "leafwiki:workspace:write", "leafwiki:mcp"},
		WorkspaceID: "current",
		AuthMethod:  "disabled",
		IssuedAt:    now,
		ExpiresAt:   now.Add(5 * time.Minute),
	}

	encoded, err := EncodeActorContext(ctx)
	if err != nil {
		t.Fatalf("EncodeActorContext failed: %v", err)
	}
	if encoded == "" || strings.Contains(encoded, "{") || strings.Contains(encoded, "+") || strings.Contains(encoded, "/") {
		t.Fatalf("encoded actor context = %q, want base64url payload", encoded)
	}

	decoded, err := DecodeActorContext(encoded, ActorContextValidation{
		Now:         now.Add(time.Minute),
		WorkspaceID: "current",
	})
	if err != nil {
		t.Fatalf("DecodeActorContext failed: %v", err)
	}
	if decoded.Subject != "user:admin" || decoded.Role != "admin" || decoded.AuthMethod != "disabled" {
		t.Fatalf("decoded actor context = %#v", decoded)
	}
}

func TestDecodeActorContextRejectsSpoofedOrStaleEnvelope(t *testing.T) {
	now := time.Date(2026, 6, 16, 12, 0, 0, 0, time.UTC)
	base := ActorContext{
		Version:     1,
		Issuer:      ActorContextIssuerWikid,
		Subject:     "user:admin",
		Username:    "admin",
		Role:        "admin",
		WorkspaceID: "current",
		AuthMethod:  "cookie",
		IssuedAt:    now,
		ExpiresAt:   now.Add(5 * time.Minute),
	}

	tests := []struct {
		name string
		mut  func(*ActorContext)
	}{
		{name: "expired", mut: func(ctx *ActorContext) { ctx.ExpiresAt = now.Add(-time.Second) }},
		{name: "wrong issuer", mut: func(ctx *ActorContext) { ctx.Issuer = "frontd" }},
		{name: "wrong workspace", mut: func(ctx *ActorContext) { ctx.WorkspaceID = "other" }},
		{name: "missing subject", mut: func(ctx *ActorContext) { ctx.Subject = "" }},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ctx := base
			tc.mut(&ctx)
			encoded, err := EncodeActorContext(ctx)
			if err != nil {
				t.Fatalf("EncodeActorContext failed: %v", err)
			}

			if _, err := DecodeActorContext(encoded, ActorContextValidation{Now: now, WorkspaceID: "current"}); err == nil {
				t.Fatalf("DecodeActorContext unexpectedly accepted %s context", tc.name)
			}
		})
	}

	if _, err := DecodeActorContext("not json", ActorContextValidation{Now: now, WorkspaceID: "current"}); err == nil {
		t.Fatalf("DecodeActorContext accepted malformed payload")
	}
}
