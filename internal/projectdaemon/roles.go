package projectdaemon

import "time"

const (
	RuntimeStackLegacy      = "legacy"
	RuntimeStackWikidFrontd = "wikid-frontd"
)

type RoleName string

const (
	RoleWikid      RoleName = "wikid"
	RoleFrontd     RoleName = "frontd"
	RoleWorkspaced RoleName = "workspaced"
)

type RoleState string

const (
	RoleStateStarting   RoleState = "starting"
	RoleStateReady      RoleState = "ready"
	RoleStateDegraded   RoleState = "degraded"
	RoleStateRestarting RoleState = "restarting"
	RoleStateCrashed    RoleState = "crashed"
	RoleStateStopped    RoleState = "stopped"
)

type RoleHealth struct {
	Name      RoleName  `json:"name"`
	State     RoleState `json:"state"`
	PID       int       `json:"pid,omitempty"`
	URL       string    `json:"url,omitempty"`
	Private   bool      `json:"private,omitempty"`
	UpdatedAt time.Time `json:"updatedAt"`
	Error     string    `json:"error,omitempty"`
}
