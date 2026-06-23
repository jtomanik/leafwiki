package agenthooks

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
	"time"
)

type ProviderID string
type AgentEventName string
type AgentSource string
type AgentToolName string

const (
	ProviderCodex   ProviderID = "codex"
	ProviderClaude  ProviderID = "claude"
	ProviderCursor  ProviderID = "cursor"
	ProviderUnknown ProviderID = "unknown"
)

const (
	AgentEventSessionStart      AgentEventName = "SessionStart"
	AgentEventSessionEnd        AgentEventName = "SessionEnd"
	AgentEventPreToolUse        AgentEventName = "PreToolUse"
	AgentEventPermissionRequest AgentEventName = "PermissionRequest"
	AgentEventPostToolUse       AgentEventName = "PostToolUse"
	AgentEventUserPromptSubmit  AgentEventName = "UserPromptSubmit"
	AgentEventStop              AgentEventName = "Stop"
	AgentEventSubagentStart     AgentEventName = "SubagentStart"
	AgentEventSubagentStop      AgentEventName = "SubagentStop"
)

const (
	AgentSourceCLI     AgentSource = "cli"
	AgentSourceStartup AgentSource = "startup"
	AgentSourceHook    AgentSource = "hook"
	AgentSourceMCP     AgentSource = "mcp"
	AgentSourceTool    AgentSource = "tool"
	AgentSourceUser    AgentSource = "user"
	AgentSourceIDE     AgentSource = "ide"
	AgentSourceAgent   AgentSource = "agent"
	AgentSourceUnknown AgentSource = "unknown"
)

type Event struct {
	Provider      ProviderID
	SessionIDHash string
	EventName     AgentEventName
	Model         string
	Source        AgentSource
	ToolName      AgentToolName
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

func Normalize(provider ProviderID, raw []byte, seenAt time.Time) (Event, bool) {
	providerID := ProviderID(strings.ToLower(strings.TrimSpace(string(provider))))
	if !isSupportedProvider(providerID) {
		return Event{}, false
	}

	var payload envelope
	if err := json.Unmarshal(raw, &payload); err != nil {
		return Event{}, false
	}
	eventName := AgentEventName(strings.TrimSpace(payload.HookEventName))
	if !isSupportedEvent(providerID, eventName) {
		return Event{}, false
	}
	sessionID := sessionKey(providerID, eventName, payload)
	if strings.TrimSpace(sessionID) == "" {
		return Event{}, false
	}

	toolName := strings.TrimSpace(payload.ToolName)
	sanitizedToolName := AgentToolName(sanitizeMetadataValue(toolName, 160))
	return Event{
		Provider:      providerID,
		SessionIDHash: hashSessionID(providerID, sessionID),
		EventName:     eventName,
		Model:         sanitizeMetadataValue(payload.Model, 80),
		Source:        sanitizeSource(payload.Source),
		ToolName:      sanitizedToolName,
		IsMCPTool:     isMCPToolEvent(eventName, sanitizedToolName),
		SubagentDelta: subagentDelta(eventName),
		EndsSession:   endsSession(providerID, eventName),
		SeenAt:        seenAt,
	}, true
}

func sanitizeSource(raw string) AgentSource {
	source := strings.ToLower(sanitizeMetadataValue(raw, 40))
	switch source {
	case "":
		return ""
	case string(AgentSourceCLI):
		return AgentSourceCLI
	case string(AgentSourceStartup):
		return AgentSourceStartup
	case string(AgentSourceHook):
		return AgentSourceHook
	case string(AgentSourceMCP):
		return AgentSourceMCP
	case string(AgentSourceTool):
		return AgentSourceTool
	case string(AgentSourceUser):
		return AgentSourceUser
	case string(AgentSourceIDE):
		return AgentSourceIDE
	case string(AgentSourceAgent):
		return AgentSourceAgent
	default:
		return AgentSourceUnknown
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
	case string(ProviderCodex), string(ProviderClaude):
		return []byte("{}\n")
	case string(ProviderCursor):
		return []byte("{\"permission\":\"allow\"}\n")
	default:
		return nil
	}
}

func IsNormalizedEvent(event Event) bool {
	provider := ProviderID(strings.TrimSpace(string(event.Provider)))
	eventName := AgentEventName(strings.TrimSpace(string(event.EventName)))
	toolName := AgentToolName(strings.TrimSpace(string(event.ToolName)))
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

func isSupportedProvider(provider ProviderID) bool {
	switch provider {
	case ProviderCodex, ProviderClaude, ProviderCursor:
		return true
	default:
		return false
	}
}

func isSupportedEvent(provider ProviderID, eventName AgentEventName) bool {
	switch provider {
	case ProviderCodex:
		switch eventName {
		case AgentEventSessionStart, AgentEventPreToolUse, AgentEventPermissionRequest, AgentEventPostToolUse, AgentEventUserPromptSubmit, AgentEventStop, AgentEventSubagentStart, AgentEventSubagentStop:
			return true
		}
	case ProviderClaude:
		switch eventName {
		case AgentEventSessionStart, AgentEventSessionEnd, AgentEventPreToolUse, AgentEventPermissionRequest, AgentEventPostToolUse, AgentEventUserPromptSubmit, AgentEventStop, AgentEventSubagentStart, AgentEventSubagentStop:
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

func sessionKey(provider ProviderID, eventName AgentEventName, payload envelope) string {
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

func isMCPToolEvent(eventName AgentEventName, toolName AgentToolName) bool {
	return strings.HasPrefix(string(toolName), "mcp__") ||
		eventName == "beforeMCPExecution" ||
		eventName == "afterMCPExecution"
}

func subagentDelta(eventName AgentEventName) int {
	switch eventName {
	case "SubagentStart", "subagentStart":
		return 1
	case "SubagentStop", "subagentStop":
		return -1
	default:
		return 0
	}
}

func endsSession(provider ProviderID, eventName AgentEventName) bool {
	return (provider == ProviderClaude && eventName == "SessionEnd") ||
		(provider == ProviderCursor && eventName == "sessionEnd")
}

func hashSessionID(provider ProviderID, rawSessionID string) string {
	sum := sha256.Sum256([]byte(string(provider) + "\x00" + rawSessionID))
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
