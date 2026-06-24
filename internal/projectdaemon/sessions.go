package projectdaemon

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"sync"
	"time"
)

type SessionID string

func (id SessionID) String() string {
	return string(id)
}

func SessionIDFromString[T ~string](raw T) SessionID {
	return SessionID(raw)
}

type SessionRegistry struct {
	mu       sync.Mutex
	handles  map[SessionID]time.Time
	seen     bool
	now      func() time.Time
	ttl      time.Duration
	onChange func(count int)
}

func NewSessionRegistry(ttl time.Duration, onChange func(count int)) *SessionRegistry {
	if ttl <= 0 {
		ttl = DefaultHeartbeatTTL
	}
	return &SessionRegistry{
		handles:  map[SessionID]time.Time{},
		now:      time.Now,
		ttl:      ttl,
		onChange: onChange,
	}
}

func (r *SessionRegistry) Register() (SessionID, error) {
	id, err := randomID()
	if err != nil {
		return "", err
	}
	sessionID := SessionIDFromString(id)
	r.mu.Lock()
	r.seen = true
	r.handles[sessionID] = r.now().Add(r.ttl)
	count := len(r.handles)
	r.mu.Unlock()
	r.notify(count)
	return sessionID, nil
}

func (r *SessionRegistry) Heartbeat(id SessionID) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.handles[id]; !ok {
		return false
	}
	r.handles[id] = r.now().Add(r.ttl)
	return true
}

func (r *SessionRegistry) Release(id SessionID) {
	r.mu.Lock()
	before := len(r.handles)
	if _, ok := r.handles[id]; ok {
		delete(r.handles, id)
	}
	count := len(r.handles)
	r.mu.Unlock()
	if count != before {
		r.notify(count)
	}
}

func (r *SessionRegistry) Count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.handles)
}

func (r *SessionRegistry) SeenSession() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.seen
}

func (r *SessionRegistry) SeenSessionCount() (bool, int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.seen, len(r.handles)
}

func (r *SessionRegistry) PruneExpired() int {
	r.mu.Lock()
	before := len(r.handles)
	now := r.now()
	for id, deadline := range r.handles {
		if !deadline.After(now) {
			delete(r.handles, id)
		}
	}
	count := len(r.handles)
	r.mu.Unlock()
	if count != before {
		r.notify(count)
	}
	return count
}

func (r *SessionRegistry) RunExpiryLoop(ctx context.Context, interval time.Duration) {
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

func (r *SessionRegistry) notify(count int) {
	if r.onChange != nil {
		r.onChange(count)
	}
}

func randomID() (string, error) {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(raw[:]), nil
}
