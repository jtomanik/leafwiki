package projectdaemon

import (
	"context"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/perber/wiki/internal/agenthooks"
)

type AgentPresenceSession struct {
	Provider        agenthooks.ProviderID     `json:"provider"`
	SessionIDHash   string                    `json:"sessionIdHash"`
	FirstSeenAt     time.Time                 `json:"firstSeenAt"`
	LastSeenAt      time.Time                 `json:"lastSeenAt"`
	LastEvent       agenthooks.AgentEventName `json:"lastEvent"`
	Model           string                    `json:"model,omitempty"`
	Source          agenthooks.AgentSource    `json:"source,omitempty"`
	ToolName        agenthooks.AgentToolName  `json:"toolName,omitempty"`
	IsMCPTool       bool                      `json:"isMcpTool"`
	ActiveSubagents int                       `json:"activeSubagents"`
}

type AgentPresenceRegistry struct {
	mu       sync.Mutex
	sessions map[string]AgentPresenceSession
	seen     bool
	now      func() time.Time
	ttl      time.Duration
	onChange func(count int)
}

func NewAgentPresenceRegistry(ttl time.Duration, onChange func(count int)) *AgentPresenceRegistry {
	if ttl < 0 {
		ttl = DefaultIdleTimeout
	}
	return &AgentPresenceRegistry{
		sessions: map[string]AgentPresenceSession{},
		now:      time.Now,
		ttl:      ttl,
		onChange: onChange,
	}
}

func (r *AgentPresenceRegistry) Record(event agenthooks.Event) {
	if !agenthooks.IsNormalizedEvent(event) {
		return
	}
	seenAt := event.SeenAt
	if seenAt.IsZero() {
		seenAt = r.now()
	}
	key := presenceKey(string(event.Provider), event.SessionIDHash)

	r.mu.Lock()
	before := len(r.sessions)
	if event.EndsSession {
		if _, ok := r.sessions[key]; !ok {
			r.mu.Unlock()
			return
		}
		r.seen = true
		delete(r.sessions, key)
		count := len(r.sessions)
		r.mu.Unlock()
		if count != before {
			r.notify(count)
		}
		return
	}
	r.seen = true
	session, ok := r.sessions[key]
	if !ok {
		session = AgentPresenceSession{
			Provider:      event.Provider,
			SessionIDHash: event.SessionIDHash,
			FirstSeenAt:   seenAt,
		}
	}
	session.LastSeenAt = seenAt
	session.LastEvent = event.EventName
	if model := safeAgentMetadata(event.Model, 80); model != "" {
		session.Model = model
	}
	if source := safeAgentSource(string(event.Source)); source != "" {
		session.Source = source
	}
	if toolName := safeAgentToolName(string(event.ToolName)); toolName != "" {
		session.ToolName = toolName
		session.IsMCPTool = event.IsMCPTool
	}
	session.ActiveSubagents += event.SubagentDelta
	if session.ActiveSubagents < 0 {
		session.ActiveSubagents = 0
	}
	r.sessions[key] = session
	count := len(r.sessions)
	r.mu.Unlock()

	if count != before {
		r.notify(count)
	}
}

func (r *AgentPresenceRegistry) Count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.sessions)
}

func (r *AgentPresenceRegistry) List() []AgentPresenceSession {
	r.mu.Lock()
	defer r.mu.Unlock()
	sessions := make([]AgentPresenceSession, 0, len(r.sessions))
	for _, session := range r.sessions {
		sessions = append(sessions, session)
	}
	sort.Slice(sessions, func(i, j int) bool {
		if sessions[i].Provider != sessions[j].Provider {
			return sessions[i].Provider < sessions[j].Provider
		}
		return sessions[i].SessionIDHash < sessions[j].SessionIDHash
	})
	return sessions
}

func (r *AgentPresenceRegistry) SeenPresence() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.seen
}

func (r *AgentPresenceRegistry) SeenPresenceCount() (bool, int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.seen, len(r.sessions)
}

func (r *AgentPresenceRegistry) PruneExpired() int {
	r.mu.Lock()
	before := len(r.sessions)
	now := r.now()
	for key, session := range r.sessions {
		if !session.LastSeenAt.Add(r.ttl).After(now) {
			delete(r.sessions, key)
		}
	}
	count := len(r.sessions)
	r.mu.Unlock()
	if count != before {
		r.notify(count)
	}
	return count
}

func (r *AgentPresenceRegistry) RunExpiryLoop(ctx context.Context, interval time.Duration) {
	if interval <= 0 {
		interval = r.ttl / 2
		if interval <= 0 {
			interval = time.Second
		}
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			r.PruneExpired()
		}
	}
}

func (r *AgentPresenceRegistry) notify(count int) {
	if r.onChange != nil {
		r.onChange(count)
	}
}

func presenceKey(provider string, sessionIDHash string) string {
	return provider + "\x00" + sessionIDHash
}

func safeAgentSource(raw string) agenthooks.AgentSource {
	source := strings.ToLower(safeAgentMetadata(raw, 40))
	switch source {
	case "":
		return ""
	case string(agenthooks.AgentSourceCLI):
		return agenthooks.AgentSourceCLI
	case string(agenthooks.AgentSourceStartup):
		return agenthooks.AgentSourceStartup
	case string(agenthooks.AgentSourceHook):
		return agenthooks.AgentSourceHook
	case string(agenthooks.AgentSourceMCP):
		return agenthooks.AgentSourceMCP
	case string(agenthooks.AgentSourceTool):
		return agenthooks.AgentSourceTool
	case string(agenthooks.AgentSourceUser):
		return agenthooks.AgentSourceUser
	case string(agenthooks.AgentSourceIDE):
		return agenthooks.AgentSourceIDE
	case string(agenthooks.AgentSourceAgent):
		return agenthooks.AgentSourceAgent
	default:
		return agenthooks.AgentSourceUnknown
	}
}

func safeAgentToolName(raw string) agenthooks.AgentToolName {
	return agenthooks.AgentToolName(safeAgentMetadata(raw, 160))
}

func safeAgentMetadata(raw string, maxLen int) string {
	value := strings.TrimSpace(raw)
	if value == "" {
		return ""
	}
	lower := strings.ToLower(value)
	if strings.Contains(value, "/") ||
		strings.Contains(value, `\`) ||
		strings.Contains(lower, "token") ||
		strings.Contains(lower, "secret") ||
		strings.Contains(lower, "api_key") ||
		strings.Contains(lower, "apikey") ||
		strings.Contains(lower, "bearer ") ||
		strings.Contains(lower, "password") {
		return ""
	}
	value = strings.Join(strings.Fields(value), " ")
	if len(value) > maxLen {
		value = value[:maxLen]
	}
	return value
}
