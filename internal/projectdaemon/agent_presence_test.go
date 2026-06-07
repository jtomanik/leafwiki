package projectdaemon

import (
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

	registry.Record(agenthooks.Event{
		Provider:      agenthooks.ProviderCodex,
		SessionIDHash: "sha256:codex",
		EventName:     "PreToolUse",
		Model:         "gpt-5.4",
		Source:        "cli",
		ToolName:      "mcp__leafwiki__get_page",
		IsMCPTool:     true,
		SeenAt:        now,
	})

	if registry.Count() != 1 {
		t.Fatalf("Count = %d, want 1", registry.Count())
	}
	sessions := registry.List()
	if len(sessions) != 1 {
		t.Fatalf("List length = %d, want 1", len(sessions))
	}
	session := sessions[0]
	if session.Provider != agenthooks.ProviderCodex || session.SessionIDHash != "sha256:codex" {
		t.Fatalf("identity = %q/%q, want codex/sha256:codex", session.Provider, session.SessionIDHash)
	}
	if session.FirstSeenAt != now || session.LastSeenAt != now || session.LastEvent != "PreToolUse" {
		t.Fatalf("timestamps/event = %s/%s/%q", session.FirstSeenAt, session.LastSeenAt, session.LastEvent)
	}
	if session.Model != "gpt-5.4" || session.Source != "cli" || session.ToolName != "mcp__leafwiki__get_page" || !session.IsMCPTool {
		t.Fatalf("metadata = %#v", session)
	}
	if got, want := joinCounts(counts), "1"; got != want {
		t.Fatalf("counts = %s, want %s", got, want)
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

	registry.Record(agenthooks.Event{
		Provider:      agenthooks.ProviderClaude,
		SessionIDHash: "sha256:claude",
		EventName:     "SessionStart",
		Model:         "claude-opus-4",
		SeenAt:        now,
	})
	now = now.Add(10 * time.Second)
	registry.Record(agenthooks.Event{
		Provider:      agenthooks.ProviderClaude,
		SessionIDHash: "sha256:claude",
		EventName:     "PreToolUse",
		ToolName:      "Read",
		SeenAt:        now,
	})

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

	registry.Record(agenthooks.Event{
		Provider:      agenthooks.ProviderClaude,
		SessionIDHash: "sha256:claude",
		EventName:     "SubagentStop",
		SubagentDelta: -1,
		SeenAt:        now.Add(1 * time.Second),
	})
	if got := registry.List()[0].ActiveSubagents; got != 0 {
		t.Fatalf("ActiveSubagents after early stop = %d, want 0", got)
	}
	registry.Record(agenthooks.Event{
		Provider:      agenthooks.ProviderClaude,
		SessionIDHash: "sha256:claude",
		EventName:     "SubagentStart",
		SubagentDelta: 1,
		SeenAt:        now.Add(2 * time.Second),
	})
	registry.Record(agenthooks.Event{
		Provider:      agenthooks.ProviderClaude,
		SessionIDHash: "sha256:claude",
		EventName:     "SubagentStop",
		SubagentDelta: -1,
		SeenAt:        now.Add(3 * time.Second),
	})
	if got := registry.List()[0].ActiveSubagents; got != 0 {
		t.Fatalf("ActiveSubagents after start/stop = %d, want 0", got)
	}

	registry.Record(agenthooks.Event{
		Provider:      agenthooks.ProviderClaude,
		SessionIDHash: "sha256:claude",
		EventName:     "SessionEnd",
		EndsSession:   true,
		SeenAt:        now.Add(4 * time.Second),
	})
	if registry.Count() != 0 {
		t.Fatalf("Count after SessionEnd = %d, want 0", registry.Count())
	}

	registry.Record(agenthooks.Event{
		Provider:      agenthooks.ProviderCursor,
		SessionIDHash: "sha256:cursor",
		EventName:     "sessionStart",
		SeenAt:        now,
	})
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
		provider string
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
