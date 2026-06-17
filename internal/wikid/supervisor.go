package wikid

import (
	"sync"
	"time"

	"github.com/perber/wiki/internal/projectdaemon"
)

type SupervisorOptions struct {
	MaxRestarts int
	Backoff     time.Duration
	Now         func() time.Time
}

type Supervisor struct {
	mu       sync.Mutex
	opts     SupervisorOptions
	roles    map[projectdaemon.RoleName]projectdaemon.RoleHealth
	restarts map[projectdaemon.RoleName]int
}

func NewSupervisor(opts SupervisorOptions) *Supervisor {
	if opts.MaxRestarts <= 0 {
		opts.MaxRestarts = 1
	}
	if opts.Backoff <= 0 {
		opts.Backoff = time.Second
	}
	return &Supervisor{
		opts:     opts,
		roles:    map[projectdaemon.RoleName]projectdaemon.RoleHealth{},
		restarts: map[projectdaemon.RoleName]int{},
	}
}

func (s *Supervisor) MarkReady(name projectdaemon.RoleName, pid int, url string, private bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.roles[name] = projectdaemon.RoleHealth{
		Name:      name,
		State:     projectdaemon.RoleStateReady,
		PID:       pid,
		URL:       url,
		Private:   private,
		UpdatedAt: s.now(),
	}
}

func (s *Supervisor) RecordCrash(name projectdaemon.RoleName, message string) (time.Time, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.restarts[name]++
	state := projectdaemon.RoleStateRestarting
	scheduled := true
	if s.restarts[name] > s.opts.MaxRestarts {
		state = projectdaemon.RoleStateCrashed
		scheduled = false
	}
	current := s.roles[name]
	current.Name = name
	current.State = state
	current.Error = message
	current.UpdatedAt = s.now()
	s.roles[name] = current
	if !scheduled {
		return time.Time{}, false
	}
	return current.UpdatedAt.Add(s.opts.Backoff), true
}

func (s *Supervisor) State(name projectdaemon.RoleName) projectdaemon.RoleHealth {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.roles[name]
}

func (s *Supervisor) Roles() []projectdaemon.RoleHealth {
	s.mu.Lock()
	defer s.mu.Unlock()
	roles := make([]projectdaemon.RoleHealth, 0, len(s.roles))
	for _, role := range s.roles {
		roles = append(roles, role)
	}
	return roles
}

func (s *Supervisor) now() time.Time {
	if s.opts.Now != nil {
		return s.opts.Now()
	}
	return time.Now().UTC()
}
