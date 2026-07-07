package presence

import (
	"encoding/json"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/perber/wiki/internal/agenthooks"
	coreauth "github.com/perber/wiki/internal/core/auth"
	sharederrors "github.com/perber/wiki/internal/core/shared/errors"
	"github.com/perber/wiki/internal/core/tree"
)

const (
	DefaultWebPresenceTTL = 90 * time.Second
)

const (
	ErrCodePresenceRegistryUnavailable sharederrors.ErrorCode = "presence_registry_unavailable"
	ErrCodePresenceInvalidRequest      sharederrors.ErrorCode = "presence_invalid_request"
	ErrCodePresenceUserRequired        sharederrors.ErrorCode = "presence_user_required"
	ErrCodePresenceSessionUserMismatch sharederrors.ErrorCode = "presence_session_user_mismatch"
	ErrCodePresenceSessionIDRequired   sharederrors.ErrorCode = "presence_session_id_required"
	ErrCodePresenceSessionIDTooLong    sharederrors.ErrorCode = "presence_session_id_too_long"
	ErrCodePresenceModeInvalid         sharederrors.ErrorCode = "presence_mode_invalid"
)

type Heartbeat struct {
	SessionID WebSessionID `json:"sessionId"`
	Mode      SessionMode  `json:"mode"`
	PageID    tree.PageID  `json:"pageId,omitempty"`
	Path      string       `json:"path,omitempty"`
	Dirty     bool         `json:"dirty"`
}

type PageRef struct {
	ID    tree.PageID `json:"id"`
	Path  string      `json:"path"`
	Title string      `json:"title"`
}

type UserRef struct {
	ID    coreauth.UserID `json:"id"`
	Name  string          `json:"name"`
	Role  string          `json:"role"`
	Email string          `json:"email,omitempty"`
}

type Session struct {
	Type            SessionType               `json:"type"`
	SessionID       WebSessionID              `json:"sessionId"`
	Provider        agenthooks.ProviderID     `json:"provider,omitempty"`
	Model           string                    `json:"model,omitempty"`
	Mode            SessionMode               `json:"mode"`
	State           SessionState              `json:"state"`
	User            UserRef                   `json:"user,omitempty"`
	Page            *PageRef                  `json:"page,omitempty"`
	Dirty           bool                      `json:"dirty"`
	Source          agenthooks.AgentSource    `json:"source,omitempty"`
	LastEvent       agenthooks.AgentEventName `json:"lastEvent,omitempty"`
	ActiveSubagents int                       `json:"activeSubagents,omitempty"`
	FirstSeenAt     time.Time                 `json:"firstSeenAt"`
	LastSeenAt      time.Time                 `json:"lastSeenAt"`
}

func (mode *SessionMode) UnmarshalJSON(raw []byte) error {
	var value string
	if err := json.Unmarshal(raw, &value); err != nil {
		return err
	}
	*mode = SessionModeFromString(value)
	return nil
}

type storedSession struct {
	session Session
	email   string
	userID  coreauth.UserID
}

type WebPresenceRegistry struct {
	mu       sync.Mutex
	sessions map[WebSessionID]storedSession
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
		sessions: map[WebSessionID]storedSession{},
		ttl:      ttl,
		now:      now,
	}
}

func (r *WebPresenceRegistry) Record(heartbeat Heartbeat, user *coreauth.User, page *PageRef) error {
	if r == nil {
		return sharederrors.NewLocalizedErrorFromCode(ErrCodePresenceRegistryUnavailable, nil)
	}
	normalized, err := normalizeHeartbeat(heartbeat)
	if err != nil {
		return err
	}
	if user == nil {
		return sharederrors.NewLocalizedErrorFromCode(ErrCodePresenceUserRequired, nil)
	}
	seenAt := r.now().UTC()

	r.mu.Lock()
	defer r.mu.Unlock()
	pruneExpiredLocked(r.sessions, seenAt, r.ttl)
	current := r.sessions[normalized.SessionID]
	userID := coreauth.UserIDFromString(user.ID)
	if current.userID != "" && current.userID != userID {
		return sharederrors.NewLocalizedErrorFromCode(ErrCodePresenceSessionUserMismatch, nil)
	}
	firstSeenAt := current.session.FirstSeenAt
	if firstSeenAt.IsZero() {
		firstSeenAt = seenAt
	}
	r.sessions[normalized.SessionID] = storedSession{
		email:  user.Email,
		userID: userID,
		session: Session{
			Type:        SessionTypeWeb,
			SessionID:   normalized.SessionID,
			Mode:        normalized.Mode,
			State:       SessionStateActive,
			User:        userRefForUser(user, false),
			Page:        page,
			Dirty:       normalized.Dirty,
			FirstSeenAt: firstSeenAt,
			LastSeenAt:  seenAt,
		},
	}
	return nil
}

func (r *WebPresenceRegistry) Remove(sessionID WebSessionID, user *coreauth.User) bool {
	if r == nil {
		return false
	}
	if sessionID.IsZero() {
		return false
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	stored, exists := r.sessions[sessionID]
	if !exists {
		return false
	}
	if user == nil || stored.userID != coreauth.UserIDFromString(user.ID) {
		return false
	}
	delete(r.sessions, sessionID)
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
		return out[i].SessionID.Less(out[j].SessionID)
	})
	return out
}

func normalizeHeartbeat(heartbeat Heartbeat) (Heartbeat, error) {
	heartbeat.SessionID = heartbeat.SessionID.Normalize()
	if heartbeat.SessionID.IsZero() {
		return Heartbeat{}, sharederrors.NewLocalizedErrorFromCode(ErrCodePresenceSessionIDRequired, nil)
	}
	if heartbeat.SessionID.Length() > 256 {
		return Heartbeat{}, sharederrors.NewLocalizedErrorFromCode(ErrCodePresenceSessionIDTooLong, nil)
	}
	heartbeat.Mode = heartbeat.Mode.Normalize()
	if heartbeat.Mode == "" {
		heartbeat.Mode = SessionModeUnknown
	}
	if !heartbeat.Mode.IsValid() {
		return Heartbeat{}, sharederrors.NewLocalizedErrorFromCode(ErrCodePresenceModeInvalid, nil)
	}
	heartbeat.PageID = tree.PageIDFromString(strings.TrimSpace(heartbeat.PageID.MetadataValue()))
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

func pruneExpiredLocked(sessions map[WebSessionID]storedSession, now time.Time, ttl time.Duration) {
	for key, stored := range sessions {
		if !stored.session.LastSeenAt.Add(ttl).After(now) {
			delete(sessions, key)
		}
	}
}
