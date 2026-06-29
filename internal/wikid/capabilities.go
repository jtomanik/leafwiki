package wikid

import "fmt"

type RoleCapabilities struct {
	ReadContent      bool
	WriteContent     bool
	AdministerGrants bool
}

func CapabilitiesForRole(role GrantRole) (RoleCapabilities, error) {
	switch role {
	case GrantRoleViewer:
		return RoleCapabilities{ReadContent: true}, nil
	case GrantRoleEditor:
		return RoleCapabilities{ReadContent: true, WriteContent: true}, nil
	case GrantRoleAdmin:
		return RoleCapabilities{ReadContent: true, WriteContent: true, AdministerGrants: true}, nil
	default:
		return RoleCapabilities{}, fmt.Errorf("unknown grant role %q: %w", role, ErrUnknownGrantRole)
	}
}
