package health

import (
	"os"

	"github.com/perber/wiki/internal/projectdaemon"
	"github.com/perber/wiki/internal/search"
)

type checkResult struct {
	sqlite  healthStatus
	dataDir healthStatus
	search  healthStatus
}

type healthCheckKey string
type healthStatus string

const (
	healthCheckDataDir        healthCheckKey = "data_dir"
	healthCheckSearch         healthCheckKey = "search"
	healthCheckSQLite         healthCheckKey = "sqlite"
	healthCheckRoleWikid      healthCheckKey = "role_wikid"
	healthCheckRoleFrontd     healthCheckKey = "role_frontd"
	healthCheckRoleWorkspaced healthCheckKey = "role_workspaced"
	healthCheckRoleUnknown    healthCheckKey = "role_unknown"

	healthStatusCrashed       healthStatus = "crashed"
	healthStatusDegraded      healthStatus = "degraded"
	healthStatusFailed        healthStatus = "failed"
	healthStatusIndexing      healthStatus = "indexing"
	healthStatusMissing       healthStatus = "missing"
	healthStatusNotApplicable healthStatus = "not_applicable"
	healthStatusOK            healthStatus = "ok"
	healthStatusRestarting    healthStatus = "restarting"
	healthStatusStarting      healthStatus = "starting"
	healthStatusStopped       healthStatus = "stopped"
	healthStatusUnknown       healthStatus = "unknown"
)

type healthChecks map[healthCheckKey]healthStatus

func (key healthCheckKey) String() string {
	return string(key)
}

func (status healthStatus) String() string {
	return string(status)
}

func (checks healthChecks) HTTPMap() map[string]string {
	out := make(map[string]string, len(checks))
	for key, state := range checks {
		out[key.String()] = state.String()
	}
	return out
}

type HealthUseCase struct {
	index         healthIndexPinger
	status        *search.IndexingStatus
	storageDir    string
	requiredRoles []projectdaemon.RoleName
	roleHealth    func() []projectdaemon.RoleHealth
}

type healthIndexPinger interface {
	Ping() error
}

type HealthUseCaseOptions struct {
	RequiredRoles []projectdaemon.RoleName
	RoleHealth    func() []projectdaemon.RoleHealth
}

func NewHealthUseCase(index *search.SQLiteIndex, status *search.IndexingStatus, storageDir string, opts ...HealthUseCaseOptions) *HealthUseCase {
	var pinger healthIndexPinger
	if index != nil {
		pinger = index
	}
	return newHealthUseCase(pinger, status, storageDir, opts...)
}

func newHealthUseCase(index healthIndexPinger, status *search.IndexingStatus, storageDir string, opts ...HealthUseCaseOptions) *HealthUseCase {
	uc := &HealthUseCase{
		index:      index,
		status:     status,
		storageDir: storageDir,
	}
	if len(opts) > 0 {
		uc.requiredRoles = append([]projectdaemon.RoleName(nil), opts[0].RequiredRoles...)
		uc.roleHealth = opts[0].RoleHealth
	}
	return uc
}

func (uc *HealthUseCase) SetRoleHealth(required []projectdaemon.RoleName, roleHealth func() []projectdaemon.RoleHealth) {
	uc.requiredRoles = append([]projectdaemon.RoleName(nil), required...)
	uc.roleHealth = roleHealth
}

func NewLegacyHealthUseCase(index *search.SQLiteIndex, status *search.IndexingStatus, storageDir string) *HealthUseCase {
	var pinger healthIndexPinger
	if index != nil {
		pinger = index
	}
	return newHealthUseCase(pinger, status, storageDir)
}

func (uc *HealthUseCase) Execute() (bool, healthChecks) {
	r := checkResult{}

	if uc.index == nil {
		r.sqlite = healthStatusNotApplicable
	} else if err := uc.index.Ping(); err != nil {
		r.sqlite = healthStatusFailed
	} else {
		r.sqlite = healthStatusOK
	}

	if info, err := os.Stat(uc.storageDir); err != nil || !info.IsDir() {
		r.dataDir = healthStatusFailed
	} else {
		r.dataDir = healthStatusOK
	}

	switch {
	case uc.status == nil:
		r.search = healthStatusNotApplicable
	case uc.status.IsFailed():
		r.search = healthStatusFailed
	case uc.status.IsReady():
		r.search = healthStatusOK
	default:
		r.search = healthStatusIndexing
	}

	checks := healthChecks{
		healthCheckSQLite:  r.sqlite,
		healthCheckDataDir: r.dataDir,
		healthCheckSearch:  r.search,
	}

	healthy := r.sqlite != healthStatusFailed && r.dataDir == healthStatusOK && r.search != healthStatusFailed
	if uc.roleHealth != nil && len(uc.requiredRoles) > 0 {
		roleChecks, rolesHealthy := requiredRoleChecks(uc.requiredRoles, uc.roleHealth())
		for name, state := range roleChecks {
			checks[name] = state
		}
		healthy = healthy && rolesHealthy
	}
	return healthy, checks
}

func requiredRoleChecks(required []projectdaemon.RoleName, roles []projectdaemon.RoleHealth) (healthChecks, bool) {
	byName := make(map[projectdaemon.RoleName]projectdaemon.RoleHealth, len(roles))
	for _, role := range roles {
		byName[role.Name] = role
	}
	checks := make(healthChecks, len(required))
	healthy := true
	for _, name := range required {
		key := roleHealthCheckName(name)
		role, ok := byName[name]
		if !ok {
			checks[key] = healthStatusMissing
			healthy = false
			continue
		}
		if role.State == projectdaemon.RoleStateReady {
			checks[key] = healthStatusOK
			continue
		}
		if role.State == "" {
			checks[key] = healthStatusUnknown
		} else {
			checks[key] = healthStatusForRoleState(role.State)
		}
		healthy = false
	}
	return checks, healthy
}

func roleHealthCheckName(role projectdaemon.RoleName) healthCheckKey {
	switch role {
	case projectdaemon.RoleWikid:
		return healthCheckRoleWikid
	case projectdaemon.RoleFrontd:
		return healthCheckRoleFrontd
	case projectdaemon.RoleWorkspaced:
		return healthCheckRoleWorkspaced
	default:
		return healthCheckRoleUnknown
	}
}

func healthStatusForRoleState(state projectdaemon.RoleState) healthStatus {
	switch state {
	case projectdaemon.RoleStateStarting:
		return healthStatusStarting
	case projectdaemon.RoleStateReady:
		return healthStatusOK
	case projectdaemon.RoleStateDegraded:
		return healthStatusDegraded
	case projectdaemon.RoleStateRestarting:
		return healthStatusRestarting
	case projectdaemon.RoleStateCrashed:
		return healthStatusCrashed
	case projectdaemon.RoleStateStopped:
		return healthStatusStopped
	default:
		return healthStatusUnknown
	}
}
