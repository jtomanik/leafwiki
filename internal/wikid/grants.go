package wikid

import (
	"fmt"
	"strings"

	"github.com/perber/wiki/internal/workspaceid"
)

const GrantSchemaVersion = 1

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
	Subject     string    `json:"subject"`
	WorkspaceID string    `json:"workspaceId"`
	Role        GrantRole `json:"role"`
}

func NewGrantDocument() GrantDocument {
	return GrantDocument{SchemaVersion: GrantSchemaVersion}
}

func (d GrantDocument) Validate() error {
	if d.SchemaVersion != GrantSchemaVersion {
		return fmt.Errorf("grant schema version = %d, want %d", d.SchemaVersion, GrantSchemaVersion)
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
		return fmt.Errorf("grant subject is required")
	}
	if err := workspaceid.ValidateWorkspaceID(grant.WorkspaceID); err != nil {
		return fmt.Errorf("grant workspace ID: %w", err)
	}
	if !grant.Role.Valid() {
		return fmt.Errorf("unknown grant role %q", grant.Role)
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
