package projectdaemon

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/perber/wiki/internal/agenthooks"
)

func TestAgentPresenceRegistryRecordCreatesSanitizedSession(t *testing.T) {
	now := time.Date(2026, 6, 7, 14, 0, 0, 0, time.UTC)
	var counts []int
	registry := NewAgentPresenceRegistry(DefaultIdleTimeout, func(count int) {
		counts = append(counts, count)
	})
	registry.now = func() time.Time {
		return now
	}

	event := normalizedPresenceEvent(t, agenthooks.ProviderCodex, `{"hook_event_name":"PreToolUse","session_id":"codex-session","model":"gpt-5.4","source":"cli","tool_name":"mcp__leafwiki__wiki_get_page"}`)
	event.SeenAt = now
	registry.Record(event)

	if registry.Count() != 1 {
		t.Fatalf("Count = %d, want 1", registry.Count())
	}
	sessions := registry.List()
	if len(sessions) != 1 {
		t.Fatalf("List length = %d, want 1", len(sessions))
	}
	session := sessions[0]
	if session.Provider != agenthooks.ProviderCodex || session.SessionIDHash != event.SessionIDHash {
		t.Fatalf("identity = %q/%q, want codex/%s", session.Provider, session.SessionIDHash, event.SessionIDHash)
	}
	if session.FirstSeenAt != now || session.LastSeenAt != now || session.LastEvent != "PreToolUse" {
		t.Fatalf("timestamps/event = %s/%s/%q", session.FirstSeenAt, session.LastSeenAt, session.LastEvent)
	}
	if session.Model != "gpt-5.4" || session.Source != "cli" || session.ToolName != "mcp__leafwiki__wiki_get_page" || !session.IsMCPTool {
		t.Fatalf("metadata = %#v", session)
	}
	if got, want := joinCounts(counts), "1"; got != want {
		t.Fatalf("counts = %s, want %s", got, want)
	}
	raw, err := json.Marshal(session)
	if err != nil {
		t.Fatalf("marshal session: %v", err)
	}
	if !strings.Contains(string(raw), `"provider":"codex"`) ||
		!strings.Contains(string(raw), `"lastEvent":"PreToolUse"`) ||
		!strings.Contains(string(raw), `"source":"cli"`) ||
		!strings.Contains(string(raw), `"toolName":"mcp__leafwiki__wiki_get_page"`) {
		t.Fatalf("session JSON = %s, want string compatibility fields", raw)
	}
}

func TestAgentPresenceRegistryRedactsUnsafeMetadata(t *testing.T) {
	registry := NewAgentPresenceRegistry(DefaultIdleTimeout, nil)
	event := normalizedPresenceEvent(t, agenthooks.ProviderCodex, `{"hook_event_name":"PreToolUse","session_id":"codex-session","model":"gpt-5.4","tool_name":"mcp__leafwiki__wiki_get_page"}`)
	event.Model = "/Users/example/token-model"
	event.Source = "/Users/example/.codex/session.jsonl"

	registry.Record(event)

	sessions := registry.List()
	if len(sessions) != 1 {
		t.Fatalf("sessions = %#v, want one", sessions)
	}
	if sessions[0].Model != "" || sessions[0].Source != "" || sessions[0].ToolName != "mcp__leafwiki__wiki_get_page" || !sessions[0].IsMCPTool {
		t.Fatalf("unsafe metadata was not redacted: %#v", sessions[0])
	}
}

func TestAgentPresenceRegistryUpdatesLifecycleAndExpires(t *testing.T) {
	now := time.Date(2026, 6, 7, 14, 0, 0, 0, time.UTC)
	var counts []int
	registry := NewAgentPresenceRegistry(time.Minute, func(count int) {
		counts = append(counts, count)
	})
	registry.now = func() time.Time {
		return now
	}

	event := normalizedPresenceEvent(t, agenthooks.ProviderClaude, `{"hook_event_name":"SessionStart","session_id":"claude-session","model":"claude-opus-4"}`)
	event.SeenAt = now
	registry.Record(event)
	now = now.Add(10 * time.Second)
	event = normalizedPresenceEvent(t, agenthooks.ProviderClaude, `{"hook_event_name":"PreToolUse","session_id":"claude-session","tool_name":"Read"}`)
	event.SeenAt = now
	registry.Record(event)

	session := registry.List()[0]
	if session.FirstSeenAt != time.Date(2026, 6, 7, 14, 0, 0, 0, time.UTC) {
		t.Fatalf("FirstSeenAt changed to %s", session.FirstSeenAt)
	}
	if session.LastSeenAt != now || session.LastEvent != "PreToolUse" || session.ToolName != "Read" {
		t.Fatalf("updated session = %#v", session)
	}
	if got, want := joinCounts(counts), "1"; got != want {
		t.Fatalf("counts after update = %s, want %s", got, want)
	}

	event = normalizedPresenceEvent(t, agenthooks.ProviderClaude, `{"hook_event_name":"SubagentStop","session_id":"claude-session"}`)
	event.SeenAt = now.Add(1 * time.Second)
	registry.Record(event)
	if got := registry.List()[0].ActiveSubagents; got != 0 {
		t.Fatalf("ActiveSubagents after early stop = %d, want 0", got)
	}
	event = normalizedPresenceEvent(t, agenthooks.ProviderClaude, `{"hook_event_name":"SubagentStart","session_id":"claude-session"}`)
	event.SeenAt = now.Add(2 * time.Second)
	registry.Record(event)
	event = normalizedPresenceEvent(t, agenthooks.ProviderClaude, `{"hook_event_name":"SubagentStop","session_id":"claude-session"}`)
	event.SeenAt = now.Add(3 * time.Second)
	registry.Record(event)
	if got := registry.List()[0].ActiveSubagents; got != 0 {
		t.Fatalf("ActiveSubagents after start/stop = %d, want 0", got)
	}

	event = normalizedPresenceEvent(t, agenthooks.ProviderClaude, `{"hook_event_name":"SessionEnd","session_id":"claude-session"}`)
	event.SeenAt = now.Add(4 * time.Second)
	registry.Record(event)
	if registry.Count() != 0 {
		t.Fatalf("Count after SessionEnd = %d, want 0", registry.Count())
	}

	event = normalizedPresenceEvent(t, agenthooks.ProviderCursor, `{"hook_event_name":"sessionStart","session_id":"cursor-session"}`)
	event.SeenAt = now
	registry.Record(event)
	now = now.Add(2 * time.Minute)
	if got := registry.PruneExpired(); got != 0 {
		t.Fatalf("PruneExpired count = %d, want 0 after TTL expiry", got)
	}
	if got, want := joinCounts(counts), "1,0,1,0"; got != want {
		t.Fatalf("counts = %s, want %s", got, want)
	}
}

func TestAgentPresenceRegistryIgnoresMissingEndEventsAsFirstActivity(t *testing.T) {
	tests := []struct {
		name     string
		provider agenthooks.ProviderID
		payload  string
	}{
		{
			name:     "claude session end",
			provider: agenthooks.ProviderClaude,
			payload:  `{"hook_event_name":"SessionEnd","session_id":"claude-ended-before-start"}`,
		},
		{
			name:     "cursor session end",
			provider: agenthooks.ProviderCursor,
			payload:  `{"hook_event_name":"sessionEnd","session_id":"cursor-ended-before-start"}`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var counts []int
			registry := NewAgentPresenceRegistry(time.Minute, func(count int) {
				counts = append(counts, count)
			})
			event, ok := agenthooks.Normalize(tt.provider, []byte(tt.payload), time.Now())
			if !ok {
				t.Fatalf("Normalize(%s) returned false", tt.provider)
			}

			registry.Record(event)

			if seen, count := registry.SeenPresenceCount(); seen || count != 0 {
				t.Fatalf("SeenPresenceCount = %v/%d, want false/0", seen, count)
			}
			if got := joinCounts(counts); got != "" {
				t.Fatalf("counts = %s, want no notifications", got)
			}
		})
	}
}

func TestAgentPresenceRegistryRejectsUnsafeControlEvents(t *testing.T) {
	valid := normalizedPresenceEvent(t, agenthooks.ProviderCodex, `{"hook_event_name":"SessionStart","session_id":"safe-session"}`)
	tests := []struct {
		name  string
		event agenthooks.Event
	}{
		{
			name: "raw session id",
			event: agenthooks.Event{
				Provider:      agenthooks.ProviderCodex,
				SessionIDHash: "raw-session-secret",
				EventName:     "SessionStart",
			},
		},
		{
			name: "unsupported provider",
			event: func() agenthooks.Event {
				event := valid
				event.Provider = "sidecar"
				return event
			}(),
		},
		{
			name: "unsupported event",
			event: func() agenthooks.Event {
				event := valid
				event.EventName = "MadeUpHook"
				return event
			}(),
		},
		{
			name: "mismatched subagent delta",
			event: func() agenthooks.Event {
				event := valid
				event.SubagentDelta = 1
				return event
			}(),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var counts []int
			registry := NewAgentPresenceRegistry(time.Minute, func(count int) {
				counts = append(counts, count)
			})

			registry.Record(tt.event)

			if seen, count := registry.SeenPresenceCount(); seen || count != 0 {
				t.Fatalf("SeenPresenceCount = %v/%d, want false/0", seen, count)
			}
			if got := joinCounts(counts); got != "" {
				t.Fatalf("counts = %s, want no notifications", got)
			}
		})
	}
}

func TestAgentPresenceRegistryZeroTTLExpiresImmediately(t *testing.T) {
	now := time.Date(2026, 6, 7, 14, 30, 0, 0, time.UTC)
	var counts []int
	registry := NewAgentPresenceRegistry(0, func(count int) {
		counts = append(counts, count)
	})
	registry.now = func() time.Time {
		return now
	}
	event, ok := agenthooks.Normalize(
		agenthooks.ProviderCodex,
		[]byte(`{"hook_event_name":"SessionStart","session_id":"zero-ttl-session"}`),
		now,
	)
	if !ok {
		t.Fatalf("Normalize returned false")
	}

	registry.Record(event)
	if registry.Count() != 1 {
		t.Fatalf("Count after Record = %d, want 1 before pruning", registry.Count())
	}

	if got := registry.PruneExpired(); got != 0 {
		t.Fatalf("PruneExpired count = %d, want 0 for zero TTL", got)
	}
	if got, want := joinCounts(counts), "1,0"; got != want {
		t.Fatalf("counts = %s, want %s", got, want)
	}
}

func normalizedPresenceEvent(t *testing.T, provider agenthooks.ProviderID, payload string) agenthooks.Event {
	t.Helper()
	event, ok := agenthooks.Normalize(provider, []byte(payload), time.Now())
	if !ok {
		t.Fatalf("Normalize(%s, %s) returned false", provider, payload)
	}
	return event
}
