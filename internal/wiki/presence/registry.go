package presence

import (
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	coreauth "github.com/perber/wiki/internal/core/auth"
)

const (
	DefaultWebPresenceTTL = 90 * time.Second
)

var validModes = map[string]struct{}{
	"view":     {},
	"edit":     {},
	"history":  {},
	"assets":   {},
	"settings": {},
	"import":   {},
	"unknown":  {},
}

type Heartbeat struct {
	SessionID string `json:"sessionId"`
	Mode      string `json:"mode"`
	PageID    string `json:"pageId,omitempty"`
	Path      string `json:"path,omitempty"`
	Dirty     bool   `json:"dirty"`
}

type PageRef struct {
	ID    string `json:"id"`
	Path  string `json:"path"`
	Title string `json:"title"`
}

type UserRef struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Role  string `json:"role"`
	Email string `json:"email,omitempty"`
}

type Session struct {
	Type            string    `json:"type"`
	SessionID       string    `json:"sessionId"`
	Provider        string    `json:"provider,omitempty"`
	Model           string    `json:"model,omitempty"`
	Mode            string    `json:"mode"`
	State           string    `json:"state"`
	User            UserRef   `json:"user,omitempty"`
	Page            *PageRef  `json:"page,omitempty"`
	Dirty           bool      `json:"dirty"`
	Source          string    `json:"source,omitempty"`
	LastEvent       string    `json:"lastEvent,omitempty"`
	ActiveSubagents int       `json:"activeSubagents,omitempty"`
	FirstSeenAt     time.Time `json:"firstSeenAt"`
	LastSeenAt      time.Time `json:"lastSeenAt"`
}

type storedSession struct {
	session Session
	email   string
	userID  string
}

type WebPresenceRegistry struct {
	mu       sync.Mutex
	sessions map[string]storedSession
	ttl      time.Duration
	now      func() time.Time
}

func NewWebPresenceRegistry(ttl time.Duration, now func() time.Time) *WebPresenceRegistry {
	if ttl <= 0 {
		ttl = DefaultWebPresenceTTL
	}
	if now == nil {
		now = time.Now
	}
	return &WebPresenceRegistry{
		sessions: map[string]storedSession{},
		ttl:      ttl,
		now:      now,
	}
}

func (r *WebPresenceRegistry) Record(heartbeat Heartbeat, user *coreauth.User, page *PageRef) error {
	if r == nil {
		return fmt.Errorf("web presence registry unavailable")
	}
	normalized, err := normalizeHeartbeat(heartbeat)
	if err != nil {
		return err
	}
	if user == nil {
		return fmt.Errorf("user is required")
	}
	seenAt := r.now().UTC()

	r.mu.Lock()
	defer r.mu.Unlock()
	pruneExpiredLocked(r.sessions, seenAt, r.ttl)
	current := r.sessions[normalized.SessionID]
	if current.userID != "" && current.userID != user.ID {
		return fmt.Errorf("sessionId belongs to a different user")
	}
	firstSeenAt := current.session.FirstSeenAt
	if firstSeenAt.IsZero() {
		firstSeenAt = seenAt
	}
	r.sessions[normalized.SessionID] = storedSession{
		email:  user.Email,
		userID: user.ID,
		session: Session{
			Type:        "web",
			SessionID:   normalized.SessionID,
			Mode:        normalized.Mode,
			State:       "active",
			User:        userRefForUser(user, false),
			Page:        page,
			Dirty:       normalized.Dirty,
			FirstSeenAt: firstSeenAt,
			LastSeenAt:  seenAt,
		},
	}
	return nil
}

func (r *WebPresenceRegistry) Remove(sessionID string, user *coreauth.User) bool {
	if r == nil {
		return false
	}
	trimmed := strings.TrimSpace(sessionID)
	if trimmed == "" {
		return false
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	stored, exists := r.sessions[trimmed]
	if !exists {
		return false
	}
	if user == nil || stored.userID != user.ID {
		return false
	}
	delete(r.sessions, trimmed)
	return true
}

func (r *WebPresenceRegistry) List(viewer *coreauth.User) []Session {
	if r == nil {
		return []Session{}
	}
	now := r.now().UTC()
	includeEmail := viewer != nil && viewer.HasRole(coreauth.RoleAdmin)

	r.mu.Lock()
	defer r.mu.Unlock()
	pruneExpiredLocked(r.sessions, now, r.ttl)
	out := make([]Session, 0, len(r.sessions))
	for _, stored := range r.sessions {
		session := stored.session
		if includeEmail {
			session.User.Email = stored.email
		} else {
			session.User.Email = ""
		}
		out = append(out, session)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].SessionID < out[j].SessionID
	})
	return out
}

func normalizeHeartbeat(heartbeat Heartbeat) (Heartbeat, error) {
	heartbeat.SessionID = strings.TrimSpace(heartbeat.SessionID)
	if heartbeat.SessionID == "" {
		return Heartbeat{}, fmt.Errorf("sessionId is required")
	}
	if len(heartbeat.SessionID) > 256 {
		return Heartbeat{}, fmt.Errorf("sessionId is too long")
	}
	heartbeat.Mode = strings.TrimSpace(heartbeat.Mode)
	if heartbeat.Mode == "" {
		heartbeat.Mode = "unknown"
	}
	if _, ok := validModes[heartbeat.Mode]; !ok {
		return Heartbeat{}, fmt.Errorf("mode must be view, edit, history, assets, settings, import, or unknown")
	}
	heartbeat.PageID = strings.TrimSpace(heartbeat.PageID)
	heartbeat.Path = normalizePagePath(heartbeat.Path)
	return heartbeat, nil
}

func normalizePagePath(path string) string {
	trimmed := strings.TrimSpace(path)
	if trimmed == "" || trimmed == "/" {
		return ""
	}
	return "/" + strings.Trim(trimmed, "/")
}

func userRefForUser(user *coreauth.User, includeEmail bool) UserRef {
	ref := UserRef{
		ID:   user.ID,
		Name: user.Username,
		Role: user.Role,
	}
	if includeEmail {
		ref.Email = user.Email
	}
	return ref
}

func pruneExpiredLocked(sessions map[string]storedSession, now time.Time, ttl time.Duration) {
	for key, stored := range sessions {
		if !stored.session.LastSeenAt.Add(ttl).After(now) {
			delete(sessions, key)
		}
	}
}
