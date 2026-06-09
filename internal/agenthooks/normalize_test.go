package agenthooks

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestNormalizeCodexToolEventSanitizesSessionAndMetadata(t *testing.T) {
	seenAt := time.Date(2026, 6, 7, 10, 11, 12, 0, time.UTC)
	raw := []byte(`{
		"hook_event_name":"PreToolUse",
		"session_id":"raw-codex-session",
		"transcript_path":"/Users/example/.codex/transcript.jsonl",
		"cwd":"/Users/example/private-project",
		"model":"gpt-5.4",
		"tool_name":"mcp__leafwiki__wiki_get_page",
		"tool_input":{"path":"/secret/page","query":"private prompt"}
	}`)

	event, ok := Normalize(ProviderCodex, raw, seenAt)

	if !ok {
		t.Fatalf("Normalize returned ok=false for supported Codex PreToolUse")
	}
	if event.Provider != ProviderCodex {
		t.Fatalf("Provider = %q, want %q", event.Provider, ProviderCodex)
	}
	if event.SessionIDHash != testSessionHash(ProviderCodex, "raw-codex-session") {
		t.Fatalf("SessionIDHash = %q, want provider-scoped sha256 hash", event.SessionIDHash)
	}
	if event.EventName != "PreToolUse" || event.Model != "gpt-5.4" || event.ToolName != "mcp__leafwiki__wiki_get_page" {
		t.Fatalf("metadata = event:%q model:%q tool:%q", event.EventName, event.Model, event.ToolName)
	}
	if !event.IsMCPTool {
		t.Fatalf("IsMCPTool = false, want true for mcp__ tool")
	}
	if !event.SeenAt.Equal(seenAt) {
		t.Fatalf("SeenAt = %s, want %s", event.SeenAt, seenAt)
	}
}

func TestNormalizeRedactsUnsafeModelSourceAndToolMetadata(t *testing.T) {
	event, ok := Normalize(ProviderCodex, []byte(`{
		"hook_event_name":"PreToolUse",
		"session_id":"raw-codex-session",
		"model":"/Users/example/token-model",
		"source":"/Users/example/.codex/session.jsonl",
		"tool_name":"Read /secret/token"
	}`), time.Now())
	if !ok {
		t.Fatalf("Normalize returned ok=false")
	}
	if event.Model != "" || event.Source != "" || event.ToolName != "" || event.IsMCPTool {
		t.Fatalf("unsafe metadata was not redacted: %#v", event)
	}
}

func TestNormalizeSupportedProviderEvents(t *testing.T) {
	seenAt := time.Date(2026, 6, 7, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		name              string
		provider          string
		payload           string
		wantEvent         string
		wantToolName      string
		wantMCPTool       bool
		wantSubagentDelta int
		wantEndsSession   bool
	}{
		{name: "codex session start", provider: ProviderCodex, payload: `{"hook_event_name":"SessionStart","session_id":"codex-session","model":"gpt-5.4","source":"cli"}`, wantEvent: "SessionStart"},
		{name: "codex permission request", provider: ProviderCodex, payload: `{"hook_event_name":"PermissionRequest","session_id":"codex-session","tool_name":"Shell"}`, wantEvent: "PermissionRequest", wantToolName: "Shell"},
		{name: "codex post tool use", provider: ProviderCodex, payload: `{"hook_event_name":"PostToolUse","session_id":"codex-session","tool_name":"mcp__leafwiki__wiki_update_page","tool_response":"secret output"}`, wantEvent: "PostToolUse", wantToolName: "mcp__leafwiki__wiki_update_page", wantMCPTool: true},
		{name: "codex user prompt submit", provider: ProviderCodex, payload: `{"hook_event_name":"UserPromptSubmit","session_id":"codex-session","prompt":"private prompt"}`, wantEvent: "UserPromptSubmit"},
		{name: "codex stop", provider: ProviderCodex, payload: `{"hook_event_name":"Stop","session_id":"codex-session","last_assistant_message":"private output"}`, wantEvent: "Stop"},
		{name: "codex subagent start", provider: ProviderCodex, payload: `{"hook_event_name":"SubagentStart","session_id":"codex-session"}`, wantEvent: "SubagentStart", wantSubagentDelta: 1},
		{name: "codex subagent stop", provider: ProviderCodex, payload: `{"hook_event_name":"SubagentStop","session_id":"codex-session"}`, wantEvent: "SubagentStop", wantSubagentDelta: -1},
		{name: "claude session start", provider: ProviderClaude, payload: `{"hook_event_name":"SessionStart","session_id":"claude-session","model":"claude-opus-4","source":"startup"}`, wantEvent: "SessionStart"},
		{name: "claude session end", provider: ProviderClaude, payload: `{"hook_event_name":"SessionEnd","session_id":"claude-session","reason":"clear"}`, wantEvent: "SessionEnd", wantEndsSession: true},
		{name: "claude pre tool use", provider: ProviderClaude, payload: `{"hook_event_name":"PreToolUse","session_id":"claude-session","tool_name":"Read","tool_input":{"file_path":"/secret"}}`, wantEvent: "PreToolUse", wantToolName: "Read"},
		{name: "claude post tool use", provider: ProviderClaude, payload: `{"hook_event_name":"PostToolUse","session_id":"claude-session","tool_name":"mcp__leafwiki__wiki_get_page","tool_response":"private"}`, wantEvent: "PostToolUse", wantToolName: "mcp__leafwiki__wiki_get_page", wantMCPTool: true},
		{name: "claude user prompt submit", provider: ProviderClaude, payload: `{"hook_event_name":"UserPromptSubmit","session_id":"claude-session","prompt":"private prompt"}`, wantEvent: "UserPromptSubmit"},
		{name: "claude stop", provider: ProviderClaude, payload: `{"hook_event_name":"Stop","session_id":"claude-session","last_assistant_message":"private"}`, wantEvent: "Stop"},
		{name: "claude subagent start", provider: ProviderClaude, payload: `{"hook_event_name":"SubagentStart","session_id":"claude-session","agent_transcript_path":"/secret"}`, wantEvent: "SubagentStart", wantSubagentDelta: 1},
		{name: "claude subagent stop", provider: ProviderClaude, payload: `{"hook_event_name":"SubagentStop","session_id":"claude-session","agent_transcript_path":"/secret"}`, wantEvent: "SubagentStop", wantSubagentDelta: -1},
		{name: "cursor session start", provider: ProviderCursor, payload: `{"hook_event_name":"sessionStart","session_id":"cursor-session","conversation_id":"cursor-conversation","model":"gpt-5.4","user_email":"secret@example.com"}`, wantEvent: "sessionStart"},
		{name: "cursor session end", provider: ProviderCursor, payload: `{"hook_event_name":"sessionEnd","session_id":"cursor-session","reason":"stop"}`, wantEvent: "sessionEnd", wantEndsSession: true},
		{name: "cursor pre tool use", provider: ProviderCursor, payload: `{"hook_event_name":"preToolUse","session_id":"cursor-session","tool_name":"Read","tool_input":{"path":"/secret"}}`, wantEvent: "preToolUse", wantToolName: "Read"},
		{name: "cursor post tool use", provider: ProviderCursor, payload: `{"hook_event_name":"postToolUse","session_id":"cursor-session","tool_name":"mcp__leafwiki__wiki_get_page","tool_output":"private"}`, wantEvent: "postToolUse", wantToolName: "mcp__leafwiki__wiki_get_page", wantMCPTool: true},
		{name: "cursor before mcp execution", provider: ProviderCursor, payload: `{"hook_event_name":"beforeMCPExecution","session_id":"cursor-session","tool_name":"leafwiki.get_page"}`, wantEvent: "beforeMCPExecution", wantToolName: "leafwiki.get_page", wantMCPTool: true},
		{name: "cursor after mcp execution", provider: ProviderCursor, payload: `{"hook_event_name":"afterMCPExecution","session_id":"cursor-session","tool_name":"leafwiki.get_page","tool_output":"private"}`, wantEvent: "afterMCPExecution", wantToolName: "leafwiki.get_page", wantMCPTool: true},
		{name: "cursor before submit prompt", provider: ProviderCursor, payload: `{"hook_event_name":"beforeSubmitPrompt","session_id":"cursor-session","prompt":"private prompt"}`, wantEvent: "beforeSubmitPrompt"},
		{name: "cursor stop", provider: ProviderCursor, payload: `{"hook_event_name":"stop","session_id":"cursor-session","status":"done"}`, wantEvent: "stop"},
		{name: "cursor subagent start", provider: ProviderCursor, payload: `{"hook_event_name":"subagentStart","session_id":"cursor-session","parent_conversation_id":"parent-conversation"}`, wantEvent: "subagentStart", wantSubagentDelta: 1},
		{name: "cursor subagent stop", provider: ProviderCursor, payload: `{"hook_event_name":"subagentStop","session_id":"cursor-session","parent_conversation_id":"parent-conversation"}`, wantEvent: "subagentStop", wantSubagentDelta: -1},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			event, ok := Normalize(tc.provider, []byte(tc.payload), seenAt)
			if !ok {
				t.Fatalf("Normalize returned ok=false")
			}
			if event.Provider != tc.provider || event.EventName != tc.wantEvent {
				t.Fatalf("provider/event = %q/%q, want %q/%q", event.Provider, event.EventName, tc.provider, tc.wantEvent)
			}
			if event.ToolName != tc.wantToolName || event.IsMCPTool != tc.wantMCPTool {
				t.Fatalf("tool metadata = %q/%v, want %q/%v", event.ToolName, event.IsMCPTool, tc.wantToolName, tc.wantMCPTool)
			}
			if event.SubagentDelta != tc.wantSubagentDelta || event.EndsSession != tc.wantEndsSession {
				t.Fatalf("lifecycle = delta:%d ends:%v, want delta:%d ends:%v", event.SubagentDelta, event.EndsSession, tc.wantSubagentDelta, tc.wantEndsSession)
			}
			rawEvent, err := json.Marshal(event)
			if err != nil {
				t.Fatalf("marshal event: %v", err)
			}
			for _, sensitive := range []string{"raw-codex-session", "claude-session", "cursor-session", "private", "/secret", "secret@example.com"} {
				if contains := string(rawEvent); strings.Contains(contains, sensitive) {
					t.Fatalf("normalized event leaked sensitive value %q: %s", sensitive, contains)
				}
			}
		})
	}
}

func TestNormalizeFailsOpenForUnknownMalformedAndIncompletePayloads(t *testing.T) {
	seenAt := time.Date(2026, 6, 7, 12, 30, 0, 0, time.UTC)
	tests := []struct {
		name     string
		provider string
		payload  string
	}{
		{name: "malformed JSON", provider: ProviderCodex, payload: `{`},
		{name: "missing event", provider: ProviderCodex, payload: `{"session_id":"s1"}`},
		{name: "missing session", provider: ProviderClaude, payload: `{"hook_event_name":"SessionStart"}`},
		{name: "null session", provider: ProviderClaude, payload: `{"hook_event_name":"SessionStart","session_id":null}`},
		{name: "unknown event", provider: ProviderCursor, payload: `{"hook_event_name":"notARealEvent","session_id":"s1"}`},
		{name: "unknown provider", provider: ProviderUnknown, payload: `{"hook_event_name":"SessionStart","session_id":"s1"}`},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if event, ok := Normalize(tc.provider, []byte(tc.payload), seenAt); ok {
				t.Fatalf("Normalize returned ok=true with event %#v", event)
			}
		})
	}
}

func TestNormalizeCursorConversationFallbackHashesConversationID(t *testing.T) {
	seenAt := time.Date(2026, 6, 7, 13, 0, 0, 0, time.UTC)
	raw := []byte(`{"hook_event_name":"sessionStart","conversation_id":"cursor-conversation","user_email":"secret@example.com"}`)

	event, ok := Normalize(ProviderCursor, raw, seenAt)

	if !ok {
		t.Fatalf("Normalize returned ok=false")
	}
	if event.SessionIDHash != testSessionHash(ProviderCursor, "cursor-conversation") {
		t.Fatalf("SessionIDHash = %q, want conversation_id hash", event.SessionIDHash)
	}
	rawEvent, err := json.Marshal(event)
	if err != nil {
		t.Fatalf("marshal event: %v", err)
	}
	if strings.Contains(string(rawEvent), "cursor-conversation") || strings.Contains(string(rawEvent), "secret@example.com") {
		t.Fatalf("normalized Cursor fallback event leaked raw fields: %s", rawEvent)
	}
}

func TestAllowResponseUsesProviderProtocol(t *testing.T) {
	tests := []struct {
		provider string
		want     string
	}{
		{provider: ProviderCodex, want: "{}\n"},
		{provider: ProviderClaude, want: "{}\n"},
		{provider: ProviderCursor, want: "{\"permission\":\"allow\"}\n"},
		{provider: ProviderUnknown, want: ""},
		{provider: "unsupported", want: ""},
	}

	for _, tc := range tests {
		t.Run(tc.provider, func(t *testing.T) {
			if got := string(AllowResponse(tc.provider)); got != tc.want {
				t.Fatalf("AllowResponse(%q) = %q, want %q", tc.provider, got, tc.want)
			}
		})
	}
}

func testSessionHash(provider, rawSessionID string) string {
	sum := sha256.Sum256([]byte(provider + "\x00" + rawSessionID))
	return "sha256:" + hex.EncodeToString(sum[:])
}
