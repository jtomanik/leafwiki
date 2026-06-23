package projectdaemon

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/perber/wiki/internal/workspaceid"
)

const ActorContextIssuerWikid = "wikid"

type ActorContext struct {
	Version     int                     `json:"version"`
	Issuer      string                  `json:"issuer"`
	Subject     string                  `json:"subject"`
	Username    string                  `json:"username,omitempty"`
	Email       string                  `json:"email,omitempty"`
	Role        string                  `json:"role"`
	Scopes      []string                `json:"scopes,omitempty"`
	WorkspaceID workspaceid.WorkspaceID `json:"workspaceId"`
	AuthMethod  string                  `json:"authMethod"`
	SessionID   string                  `json:"sessionId,omitempty"`
	IssuedAt    time.Time               `json:"issuedAt"`
	ExpiresAt   time.Time               `json:"expiresAt"`
}

type ActorContextValidation struct {
	Now         time.Time
	WorkspaceID workspaceid.WorkspaceID
}

type actorContextWire struct {
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
	var wire actorContextWire
	if err := json.Unmarshal(raw, &wire); err != nil {
		return ActorContext{}, fmt.Errorf("decode actor context json: %w", err)
	}
	ctx, err := actorContextFromWire(wire)
	if err != nil {
		return ActorContext{}, err
	}
	if err := validateActorContext(ctx, validation); err != nil {
		return ActorContext{}, err
	}
	return ctx, nil
}

func actorContextFromWire(wire actorContextWire) (ActorContext, error) {
	workspaceID, err := workspaceid.ParseWorkspaceID(wire.WorkspaceID)
	if err != nil {
		return ActorContext{}, fmt.Errorf("actor context workspace: %w", err)
	}
	return ActorContext{
		Version:     wire.Version,
		Issuer:      wire.Issuer,
		Subject:     wire.Subject,
		Username:    wire.Username,
		Email:       wire.Email,
		Role:        wire.Role,
		Scopes:      append([]string(nil), wire.Scopes...),
		WorkspaceID: workspaceID,
		AuthMethod:  wire.AuthMethod,
		SessionID:   wire.SessionID,
		IssuedAt:    wire.IssuedAt,
		ExpiresAt:   wire.ExpiresAt,
	}, nil
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
	if ctx.WorkspaceID == "" {
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
