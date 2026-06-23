package mcp

import (
	"fmt"
	"sync"
	"time"

	"github.com/perber/wiki/internal/workspacesync"
)

const (
	defaultContextCheckpointSessions = 256
	defaultContextCheckpointTTL      = 30 * time.Minute
)

type contextCheckpoint struct {
	Token      string
	CreatedAt  time.Time
	CommitHash workspacesync.CommitHash
}

type contextCheckpointStore struct {
	mu          sync.Mutex
	limit       int
	maxSessions int
	ttl         time.Duration
	next        uint64
	sessions    map[string][]contextCheckpoint
}

func newContextCheckpointStore(limit int) *contextCheckpointStore {
	if limit <= 0 {
		limit = 10
	}
	return &contextCheckpointStore{
		limit:       limit,
		maxSessions: defaultContextCheckpointSessions,
		ttl:         defaultContextCheckpointTTL,
		sessions:    map[string][]contextCheckpoint{},
	}
}

func (s *contextCheckpointStore) record(sessionKey string, checkpoint contextCheckpoint) (previous *contextCheckpoint, history []contextCheckpoint) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if checkpoint.Token == "" {
		s.next++
		checkpoint.Token = fmt.Sprintf("ctx_%d", s.next)
	}
	if checkpoint.CreatedAt.IsZero() {
		checkpoint.CreatedAt = time.Now().UTC()
	}
	s.pruneExpiredLocked(checkpoint.CreatedAt)
	if _, exists := s.sessions[sessionKey]; !exists {
		s.evictForNewSessionLocked(sessionKey)
	}

	existing := append([]contextCheckpoint{}, s.sessions[sessionKey]...)
	if len(existing) > 0 {
		last := existing[len(existing)-1]
		previous = &last
	}
	existing = append(existing, checkpoint)
	if len(existing) > s.limit {
		existing = existing[len(existing)-s.limit:]
	}
	s.sessions[sessionKey] = existing
	s.evictOverflowLocked(sessionKey)
	return previous, append([]contextCheckpoint{}, existing...)
}

func (s *contextCheckpointStore) find(sessionKey, token string) (contextCheckpoint, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.pruneExpiredLocked(time.Now().UTC())
	for _, checkpoint := range s.sessions[sessionKey] {
		if checkpoint.Token == token {
			return checkpoint, true
		}
	}
	return contextCheckpoint{}, false
}

func (s *contextCheckpointStore) pruneExpiredLocked(now time.Time) {
	if s.ttl <= 0 {
		return
	}
	cutoff := now.Add(-s.ttl)
	for sessionKey, checkpoints := range s.sessions {
		if len(checkpoints) == 0 {
			continue
		}
		filtered := checkpoints[:0]
		for _, checkpoint := range checkpoints {
			if checkpoint.CreatedAt.After(cutoff) {
				filtered = append(filtered, checkpoint)
			}
		}
		if len(filtered) == 0 {
			delete(s.sessions, sessionKey)
			continue
		}
		s.sessions[sessionKey] = filtered
	}
}

func (s *contextCheckpointStore) evictOverflowLocked(keepSession string) {
	if s.maxSessions <= 0 {
		return
	}
	for len(s.sessions) > s.maxSessions {
		if !s.evictOldestSessionLocked(keepSession) {
			return
		}
	}
}

func (s *contextCheckpointStore) evictForNewSessionLocked(keepSession string) {
	if s.maxSessions <= 0 {
		return
	}
	for len(s.sessions) >= s.maxSessions {
		if !s.evictOldestSessionLocked(keepSession) {
			return
		}
	}
}

func (s *contextCheckpointStore) evictOldestSessionLocked(keepSession string) bool {
	oldestSession := ""
	var oldest time.Time
	for sessionKey, checkpoints := range s.sessions {
		if sessionKey == keepSession {
			continue
		}
		lastSeen := time.Time{}
		if len(checkpoints) > 0 {
			lastSeen = checkpoints[len(checkpoints)-1].CreatedAt
		}
		if oldestSession == "" || lastSeen.Before(oldest) {
			oldestSession = sessionKey
			oldest = lastSeen
		}
	}
	if oldestSession == "" {
		return false
	}
	delete(s.sessions, oldestSession)
	return true
}

func checkpointOutputs(checkpoints []contextCheckpoint) []contextCheckpointOutput {
	out := make([]contextCheckpointOutput, 0, len(checkpoints))
	for _, checkpoint := range checkpoints {
		out = append(out, checkpointOutput(checkpoint))
	}
	return out
}

func checkpointOutput(checkpoint contextCheckpoint) contextCheckpointOutput {
	return contextCheckpointOutput{
		Token:     checkpoint.Token,
		CreatedAt: checkpoint.CreatedAt.Format(time.RFC3339),
	}
}
