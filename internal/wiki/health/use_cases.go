package health

import (
	"os"

	"github.com/perber/wiki/internal/projectdaemon"
	"github.com/perber/wiki/internal/search"
)

type checkResult struct {
	sqlite  string
	dataDir string
	search  string
}

type HealthUseCase struct {
	index         *search.SQLiteIndex
	status        *search.IndexingStatus
	storageDir    string
	requiredRoles []projectdaemon.RoleName
	roleHealth    func() []projectdaemon.RoleHealth
}

type HealthUseCaseOptions struct {
	RequiredRoles []projectdaemon.RoleName
	RoleHealth    func() []projectdaemon.RoleHealth
}

func NewHealthUseCase(index *search.SQLiteIndex, status *search.IndexingStatus, storageDir string, opts ...HealthUseCaseOptions) *HealthUseCase {
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
	return &HealthUseCase{
		index:      index,
		status:     status,
		storageDir: storageDir,
	}
}

func (uc *HealthUseCase) Execute() (bool, map[string]string) {
	r := checkResult{}

	if uc.index == nil {
		r.sqlite = "not_applicable"
	} else if err := uc.index.Ping(); err != nil {
		r.sqlite = "failed"
	} else {
		r.sqlite = "ok"
	}

	if info, err := os.Stat(uc.storageDir); err != nil || !info.IsDir() {
		r.dataDir = "failed"
	} else {
		r.dataDir = "ok"
	}

	switch {
	case uc.status == nil:
		r.search = "not_applicable"
	case uc.status.IsFailed():
		r.search = "failed"
	case uc.status.IsReady():
		r.search = "ok"
	default:
		r.search = "indexing"
	}

	checks := map[string]string{
		"sqlite":   r.sqlite,
		"data_dir": r.dataDir,
		"search":   r.search,
	}

	healthy := r.sqlite != "failed" && r.dataDir == "ok" && r.search != "failed"
	if uc.roleHealth != nil && len(uc.requiredRoles) > 0 {
		roleChecks, rolesHealthy := requiredRoleChecks(uc.requiredRoles, uc.roleHealth())
		for name, state := range roleChecks {
			checks[name] = state
		}
		healthy = healthy && rolesHealthy
	}
	return healthy, checks
}

func requiredRoleChecks(required []projectdaemon.RoleName, roles []projectdaemon.RoleHealth) (map[string]string, bool) {
	byName := make(map[projectdaemon.RoleName]projectdaemon.RoleHealth, len(roles))
	for _, role := range roles {
		byName[role.Name] = role
	}
	checks := make(map[string]string, len(required))
	healthy := true
	for _, name := range required {
		key := "role_" + string(name)
		role, ok := byName[name]
		if !ok {
			checks[key] = "missing"
			healthy = false
			continue
		}
		if role.State == projectdaemon.RoleStateReady {
			checks[key] = "ok"
			continue
		}
		if role.State == "" {
			checks[key] = "unknown"
		} else {
			checks[key] = string(role.State)
		}
		healthy = false
	}
	return checks, healthy
}
