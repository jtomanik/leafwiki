package projectdaemon

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

const ActorContextIssuerWikid = "wikid"

type ActorContext struct {
	Version     int       `json:"version"`
	Issuer      string    `json:"issuer"`
	Subject     string    `json:"subject"`
	Username    string    `json:"username,omitempty"`
	Email       string    `json:"email,omitempty"`
	Role        string    `json:"role"`
	Scopes      []string  `json:"scopes,omitempty"`
	WorkspaceID string    `json:"workspaceId"`
	AuthMethod  string    `json:"authMethod"`
	SessionID   string    `json:"sessionId,omitempty"`
	IssuedAt    time.Time `json:"issuedAt"`
	ExpiresAt   time.Time `json:"expiresAt"`
}

type ActorContextValidation struct {
	Now         time.Time
	WorkspaceID string
}

func EncodeActorContext(ctx ActorContext) (string, error) {
	raw, err := json.Marshal(ctx)
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

func DecodeActorContext(encoded string, validation ActorContextValidation) (ActorContext, error) {
	trimmed := strings.TrimSpace(encoded)
	if trimmed == "" {
		return ActorContext{}, fmt.Errorf("actor context is required")
	}
	raw, err := base64.RawURLEncoding.DecodeString(trimmed)
	if err != nil {
		return ActorContext{}, fmt.Errorf("decode actor context: %w", err)
	}
	var ctx ActorContext
	if err := json.Unmarshal(raw, &ctx); err != nil {
		return ActorContext{}, fmt.Errorf("decode actor context json: %w", err)
	}
	if err := validateActorContext(ctx, validation); err != nil {
		return ActorContext{}, err
	}
	return ctx, nil
}

func (ctx ActorContext) SubjectID() string {
	subject := strings.TrimSpace(ctx.Subject)
	if id, ok := strings.CutPrefix(subject, "user:"); ok {
		return strings.TrimSpace(id)
	}
	return subject
}

func validateActorContext(ctx ActorContext, validation ActorContextValidation) error {
	if ctx.Version != 1 {
		return fmt.Errorf("actor context version = %d, want 1", ctx.Version)
	}
	if ctx.Issuer != ActorContextIssuerWikid {
		return fmt.Errorf("actor context issuer = %q, want %q", ctx.Issuer, ActorContextIssuerWikid)
	}
	if strings.TrimSpace(ctx.Subject) == "" {
		return fmt.Errorf("actor context subject is required")
	}
	if strings.TrimSpace(ctx.WorkspaceID) == "" {
		return fmt.Errorf("actor context workspace is required")
	}
	if validation.WorkspaceID != "" && ctx.WorkspaceID != validation.WorkspaceID {
		return fmt.Errorf("actor context workspace = %q, want %q", ctx.WorkspaceID, validation.WorkspaceID)
	}
	now := validation.Now
	if now.IsZero() {
		now = time.Now().UTC()
	}
	if ctx.ExpiresAt.IsZero() || !now.Before(ctx.ExpiresAt) {
		return fmt.Errorf("actor context is expired")
	}
	return nil
}
