package wikid

import (
	"sort"
	"strings"
	"sync"
	"time"
)

type WorkspaceState string

const (
	WorkspaceStateRegistered WorkspaceState = "registered"
	WorkspaceStateStarting   WorkspaceState = "starting"
	WorkspaceStateRunning    WorkspaceState = "running"
	WorkspaceStateRestarting WorkspaceState = "restarting"
	WorkspaceStateCrashed    WorkspaceState = "crashed"
)

type WorkspaceStatus struct {
	WorkspaceID string         `json:"workspaceId"`
	State       WorkspaceState `json:"state"`
	PID         int            `json:"pid,omitempty"`
	URL         string         `json:"url,omitempty"`
	Error       string         `json:"error,omitempty"`
	UpdatedAt   time.Time      `json:"updatedAt"`
}

type WorkspaceSupervisorOptions struct {
	MaxRestarts int
	Backoff     time.Duration
	Now         func() time.Time
}

type WorkspaceSupervisor struct {
	mu       sync.Mutex
	opts     WorkspaceSupervisorOptions
	statuses map[string]WorkspaceStatus
	restarts map[string]int
}

func NewWorkspaceSupervisor(opts WorkspaceSupervisorOptions) *WorkspaceSupervisor {
	if opts.MaxRestarts <= 0 {
		opts.MaxRestarts = 1
	}
	if opts.Backoff <= 0 {
		opts.Backoff = time.Second
	}
	return &WorkspaceSupervisor{
		opts:     opts,
		statuses: map[string]WorkspaceStatus{},
		restarts: map[string]int{},
	}
}

func (s *WorkspaceSupervisor) MarkStarting(workspaceID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	workspaceID = cleanWorkspaceID(workspaceID)
	current := s.statuses[workspaceID]
	current.WorkspaceID = workspaceID
	current.State = WorkspaceStateStarting
	current.Error = ""
	current.UpdatedAt = s.now()
	s.statuses[workspaceID] = current
}

func (s *WorkspaceSupervisor) MarkReady(workspaceID string, pid int, url string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	workspaceID = cleanWorkspaceID(workspaceID)
	s.statuses[workspaceID] = WorkspaceStatus{
		WorkspaceID: workspaceID,
		State:       WorkspaceStateRunning,
		PID:         pid,
		URL:         strings.TrimSpace(url),
		UpdatedAt:   s.now(),
	}
}

func (s *WorkspaceSupervisor) MarkStatus(status WorkspaceStatus) {
	s.mu.Lock()
	defer s.mu.Unlock()
	status.WorkspaceID = cleanWorkspaceID(status.WorkspaceID)
	if status.WorkspaceID == "" {
		return
	}
	if status.UpdatedAt.IsZero() {
		status.UpdatedAt = s.now()
	}
	s.statuses[status.WorkspaceID] = status
}

func (s *WorkspaceSupervisor) RecordCrash(workspaceID string, message string) (time.Time, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	workspaceID = cleanWorkspaceID(workspaceID)
	s.restarts[workspaceID]++
	state := WorkspaceStateRestarting
	scheduled := true
	if s.restarts[workspaceID] > s.opts.MaxRestarts {
		state = WorkspaceStateCrashed
		scheduled = false
	}
	current := s.statuses[workspaceID]
	current.WorkspaceID = workspaceID
	current.State = state
	current.Error = message
	current.UpdatedAt = s.now()
	s.statuses[workspaceID] = current
	if !scheduled {
		return time.Time{}, false
	}
	return current.UpdatedAt.Add(s.opts.Backoff), true
}

func (s *WorkspaceSupervisor) Status(workspaceID string) WorkspaceStatus {
	s.mu.Lock()
	defer s.mu.Unlock()
	workspaceID = cleanWorkspaceID(workspaceID)
	status, ok := s.statuses[workspaceID]
	if !ok {
		return WorkspaceStatus{WorkspaceID: workspaceID, State: WorkspaceStateRegistered}
	}
	return status
}

func (s *WorkspaceSupervisor) Statuses() []WorkspaceStatus {
	s.mu.Lock()
	defer s.mu.Unlock()
	statuses := make([]WorkspaceStatus, 0, len(s.statuses))
	for _, status := range s.statuses {
		statuses = append(statuses, status)
	}
	sort.SliceStable(statuses, func(i, j int) bool {
		return statuses[i].WorkspaceID < statuses[j].WorkspaceID
	})
	return statuses
}

func (s *WorkspaceSupervisor) now() time.Time {
	if s.opts.Now != nil {
		return s.opts.Now()
	}
	return time.Now().UTC()
}

func cleanWorkspaceID(workspaceID string) string {
	return strings.TrimSpace(workspaceID)
}
