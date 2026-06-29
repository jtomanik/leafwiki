package wikid

import (
	"errors"
	"fmt"
	"strings"

	"github.com/perber/wiki/internal/workspaceid"
)

const GrantSchemaVersion = 1

var (
	ErrGrantSchemaVersion   = errors.New("grant schema version mismatch")
	ErrGrantSubjectRequired = errors.New("grant subject is required")
	ErrUnknownGrantRole     = errors.New("unknown grant role")
)

type GrantRole string

const (
	GrantRoleViewer GrantRole = "viewer"
	GrantRoleEditor GrantRole = "editor"
	GrantRoleAdmin  GrantRole = "admin"
)

type GrantDocument struct {
	SchemaVersion int     `json:"schemaVersion"`
	Grants        []Grant `json:"grants"`
}

type Grant struct {
	Subject     string                  `json:"subject"`
	WorkspaceID workspaceid.WorkspaceID `json:"workspaceId"`
	Role        GrantRole               `json:"role"`
}

func NewGrantDocument() GrantDocument {
	return GrantDocument{SchemaVersion: GrantSchemaVersion}
}

func (d GrantDocument) Validate() error {
	if d.SchemaVersion != GrantSchemaVersion {
		return fmt.Errorf("grant schema version = %d, want %d: %w", d.SchemaVersion, GrantSchemaVersion, ErrGrantSchemaVersion)
	}
	for _, grant := range d.Grants {
		if err := validateGrant(grant); err != nil {
			return err
		}
	}
	return nil
}

func validateGrant(grant Grant) error {
	if strings.TrimSpace(grant.Subject) == "" {
		return ErrGrantSubjectRequired
	}
	if err := grant.WorkspaceID.Validate(); err != nil {
		return fmt.Errorf("grant workspace ID: %w", err)
	}
	if !grant.Role.Valid() {
		return fmt.Errorf("unknown grant role %q: %w", grant.Role, ErrUnknownGrantRole)
	}
	return nil
}

func (r GrantRole) Valid() bool {
	switch r {
	case GrantRoleViewer, GrantRoleEditor, GrantRoleAdmin:
		return true
	default:
		return false
	}
}
