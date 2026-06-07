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
	return Event{
		Provider:      provider,
		SessionIDHash: hashSessionID(provider, sessionID),
		EventName:     eventName,
		Model:         strings.TrimSpace(payload.Model),
		Source:        strings.TrimSpace(payload.Source),
		ToolName:      toolName,
		IsMCPTool:     isMCPToolEvent(eventName, toolName),
		SubagentDelta: subagentDelta(eventName),
		EndsSession:   endsSession(provider, eventName),
		SeenAt:        seenAt,
	}, true
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
