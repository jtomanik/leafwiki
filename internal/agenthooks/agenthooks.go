package agenthooks

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
	"time"
)

const (
	ProviderCodex   = "codex"
	ProviderClaude  = "claude"
	ProviderCursor  = "cursor"
	ProviderUnknown = "unknown"
)

type Event struct {
	Provider      string
	SessionIDHash string
	EventName     string
	Model         string
	Source        string
	ToolName      string
	IsMCPTool     bool
	SubagentDelta int
	EndsSession   bool
	SeenAt        time.Time
}

type envelope struct {
	HookEventName        string `json:"hook_event_name"`
	SessionID            string `json:"session_id"`
	ConversationID       string `json:"conversation_id"`
	ParentConversationID string `json:"parent_conversation_id"`
	Model                string `json:"model"`
	Source               string `json:"source"`
	ToolName             string `json:"tool_name"`
}

func Normalize(provider string, raw []byte, seenAt time.Time) (Event, bool) {
	provider = strings.ToLower(strings.TrimSpace(provider))
	if !isSupportedProvider(provider) {
		return Event{}, false
	}

	var payload envelope
	if err := json.Unmarshal(raw, &payload); err != nil {
		return Event{}, false
	}
	eventName := strings.TrimSpace(payload.HookEventName)
	if !isSupportedEvent(provider, eventName) {
		return Event{}, false
	}
	sessionID := sessionKey(provider, eventName, payload)
	if strings.TrimSpace(sessionID) == "" {
		return Event{}, false
	}

	toolName := strings.TrimSpace(payload.ToolName)
	sanitizedToolName := sanitizeMetadataValue(toolName, 160)
	return Event{
		Provider:      provider,
		SessionIDHash: hashSessionID(provider, sessionID),
		EventName:     eventName,
		Model:         sanitizeMetadataValue(payload.Model, 80),
		Source:        sanitizeSource(payload.Source),
		ToolName:      sanitizedToolName,
		IsMCPTool:     isMCPToolEvent(eventName, sanitizedToolName),
		SubagentDelta: subagentDelta(eventName),
		EndsSession:   endsSession(provider, eventName),
		SeenAt:        seenAt,
	}, true
}

func sanitizeSource(raw string) string {
	source := strings.ToLower(sanitizeMetadataValue(raw, 40))
	switch source {
	case "", "cli", "startup", "hook", "mcp", "tool", "user", "ide", "agent":
		return source
	default:
		return "unknown"
	}
}

func sanitizeMetadataValue(raw string, maxLen int) string {
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

func AllowResponse(provider string) []byte {
	switch strings.ToLower(strings.TrimSpace(provider)) {
	case ProviderCodex, ProviderClaude:
		return []byte("{}\n")
	case ProviderCursor:
		return []byte("{\"permission\":\"allow\"}\n")
	default:
		return nil
	}
}

func IsNormalizedEvent(event Event) bool {
	provider := strings.TrimSpace(event.Provider)
	eventName := strings.TrimSpace(event.EventName)
	toolName := strings.TrimSpace(event.ToolName)
	if provider != event.Provider || eventName != event.EventName || toolName != event.ToolName {
		return false
	}
	if !isSupportedProvider(provider) || !isSupportedEvent(provider, eventName) {
		return false
	}
	if !isSessionIDHash(event.SessionIDHash) {
		return false
	}
	if event.IsMCPTool != isMCPToolEvent(eventName, toolName) {
		return false
	}
	if event.SubagentDelta != subagentDelta(eventName) {
		return false
	}
	return event.EndsSession == endsSession(provider, eventName)
}

func isSupportedProvider(provider string) bool {
	switch provider {
	case ProviderCodex, ProviderClaude, ProviderCursor:
		return true
	default:
		return false
	}
}

func isSupportedEvent(provider string, eventName string) bool {
	switch provider {
	case ProviderCodex:
		switch eventName {
		case "SessionStart", "PreToolUse", "PermissionRequest", "PostToolUse", "UserPromptSubmit", "Stop", "SubagentStart", "SubagentStop":
			return true
		}
	case ProviderClaude:
		switch eventName {
		case "SessionStart", "SessionEnd", "PreToolUse", "PermissionRequest", "PostToolUse", "UserPromptSubmit", "Stop", "SubagentStart", "SubagentStop":
			return true
		}
	case ProviderCursor:
		switch eventName {
		case "sessionStart", "sessionEnd", "preToolUse", "postToolUse", "beforeMCPExecution", "afterMCPExecution", "beforeSubmitPrompt", "stop", "subagentStart", "subagentStop":
			return true
		}
	}
	return false
}

func sessionKey(provider string, eventName string, payload envelope) string {
	if provider == ProviderCursor {
		if eventName == "subagentStart" || eventName == "subagentStop" {
			if strings.TrimSpace(payload.ParentConversationID) != "" {
				return payload.ParentConversationID
			}
		}
		if strings.TrimSpace(payload.SessionID) != "" {
			return payload.SessionID
		}
		return payload.ConversationID
	}
	return payload.SessionID
}

func isMCPToolEvent(eventName string, toolName string) bool {
	return strings.HasPrefix(toolName, "mcp__") ||
		eventName == "beforeMCPExecution" ||
		eventName == "afterMCPExecution"
}

func subagentDelta(eventName string) int {
	switch eventName {
	case "SubagentStart", "subagentStart":
		return 1
	case "SubagentStop", "subagentStop":
		return -1
	default:
		return 0
	}
}

func endsSession(provider string, eventName string) bool {
	return (provider == ProviderClaude && eventName == "SessionEnd") ||
		(provider == ProviderCursor && eventName == "sessionEnd")
}

func hashSessionID(provider string, rawSessionID string) string {
	sum := sha256.Sum256([]byte(provider + "\x00" + rawSessionID))
	return "sha256:" + hex.EncodeToString(sum[:])
}

func isSessionIDHash(value string) bool {
	if strings.TrimSpace(value) != value || !strings.HasPrefix(value, "sha256:") {
		return false
	}
	digest := strings.TrimPrefix(value, "sha256:")
	if len(digest) != sha256.Size*2 {
		return false
	}
	_, err := hex.DecodeString(digest)
	return err == nil
}
