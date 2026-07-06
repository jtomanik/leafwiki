package agenthooks

import (
	"encoding/json"
	"errors"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/gstruct"
	"github.com/onsi/gomega/types"
)

var errNormalizeRejected = errors.New("agent hook event rejected")

func normalizeEvent(provider ProviderID, raw []byte, seenAt time.Time) (Event, error) {
	GinkgoHelper()

	event, accepted := Normalize(provider, raw, seenAt)
	if !accepted {
		return Event{}, errNormalizeRejected
	}
	return event, nil
}

type normalizedAgentHookEventState string

const (
	normalizedAgentHookEventContractSatisfied      normalizedAgentHookEventState = "normalized agent hook event contract is satisfied"
	normalizedAgentHookEventProviderNotCanonical   normalizedAgentHookEventState = "provider is not canonical"
	normalizedAgentHookEventNameNotCanonical       normalizedAgentHookEventState = "event name is not canonical"
	normalizedAgentHookEventToolNameNotCanonical   normalizedAgentHookEventState = "tool name is not canonical"
	normalizedAgentHookEventUnsupportedProvider    normalizedAgentHookEventState = "provider is unsupported"
	normalizedAgentHookEventUnsupportedEvent       normalizedAgentHookEventState = "event is unsupported"
	normalizedAgentHookEventInvalidSessionHash     normalizedAgentHookEventState = "session hash is invalid"
	normalizedAgentHookEventMCPClassificationDrift normalizedAgentHookEventState = "MCP tool classification drifted"
	normalizedAgentHookEventSubagentDeltaDrift     normalizedAgentHookEventState = "subagent delta drifted"
	normalizedAgentHookEventSessionEndDrift        normalizedAgentHookEventState = "session-end classification drifted"
)

func beNormalizedAgentHookEvent() types.GomegaMatcher {
	return WithTransform(normalizedAgentHookEventStateFor, Equal(normalizedAgentHookEventContractSatisfied))
}

func normalizedAgentHookEventStateFor(event Event) normalizedAgentHookEventState {
	if IsNormalizedEvent(event) {
		return normalizedAgentHookEventContractSatisfied
	}

	provider := event.Provider.Normalize()
	eventName := event.EventName.Normalize()
	toolName := event.ToolName.Normalize()
	switch {
	case provider != event.Provider:
		return normalizedAgentHookEventProviderNotCanonical
	case eventName != event.EventName:
		return normalizedAgentHookEventNameNotCanonical
	case toolName != event.ToolName:
		return normalizedAgentHookEventToolNameNotCanonical
	case !isSupportedProvider(provider):
		return normalizedAgentHookEventUnsupportedProvider
	case !isSupportedEvent(provider, eventName):
		return normalizedAgentHookEventUnsupportedEvent
	case !isSessionIDHash(event.SessionIDHash):
		return normalizedAgentHookEventInvalidSessionHash
	case event.IsMCPTool != isMCPToolEvent(eventName, toolName):
		return normalizedAgentHookEventMCPClassificationDrift
	case event.SubagentDelta != subagentDelta(eventName):
		return normalizedAgentHookEventSubagentDeltaDrift
	case event.EndsSession != endsSession(provider, eventName):
		return normalizedAgentHookEventSessionEndDrift
	default:
		return normalizedAgentHookEventContractSatisfied
	}
}

var _ = Describe("agent hook normalization", Label("unit"), func() {
	It("hashes Codex session identity while preserving safe tool metadata", func() {
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

		event, err := normalizeEvent(ProviderCodex, raw, seenAt)

		Expect(err).To(Succeed())
		Expect(event).To(beNormalizedAgentHookEvent())
		Expect(event).To(gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
			"Provider":      Equal(ProviderCodex),
			"SessionIDHash": Equal(testSessionHash(ProviderCodex, "raw-codex-session")),
			"EventName":     Equal(AgentEventPreToolUse),
			"Model":         Equal("gpt-5.4"),
			"ToolName":      Equal(newFixtureAgentToolName("mcp__leafwiki__wiki_get_page")),
			"SeenAt":        BeTemporally("==", seenAt),
		}))
	})

	It("stores provider event source and tool fields as semantic types", func() {
		var provider ProviderID = ProviderCodex
		var eventName AgentEventName = AgentEventPreToolUse
		var source AgentSource = AgentSourceCLI
		var tool AgentToolName = newFixtureAgentToolName("mcp__leafwiki__wiki_get_page")

		event, err := normalizeEvent(provider, []byte(`{
			"hook_event_name":"PreToolUse",
			"session_id":"typed-contract-session",
			"source":"cli",
			"tool_name":"mcp__leafwiki__wiki_get_page"
		}`), time.Now())

		Expect(err).To(Succeed())
		Expect(event).To(gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
			"Provider":  Equal(provider),
			"EventName": Equal(eventName),
			"Source":    Equal(source),
			"ToolName":  Equal(tool),
		}))
	})

	It("redacts unsafe model source and tool metadata", func() {
		event, err := normalizeEvent(ProviderCodex, []byte(`{
			"hook_event_name":"PreToolUse",
			"session_id":"raw-codex-session",
			"model":"/Users/example/token-model",
			"source":"/Users/example/.codex/session.jsonl",
			"tool_name":"Read /secret/token"
		}`), time.Now())

		Expect(err).To(Succeed())
		Expect(event).To(beNormalizedAgentHookEvent())
		Expect(event).To(gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
			"EventName": Equal(AgentEventPreToolUse),
			"Model":     BeEmpty(),
			"Source":    BeEmpty(),
			"ToolName":  BeEmpty(),
		}))
	})

	It("hashes Cursor conversation IDs when session IDs are absent", func() {
		seenAt := time.Date(2026, 6, 7, 13, 0, 0, 0, time.UTC)
		raw := []byte(`{"hook_event_name":"sessionStart","conversation_id":"cursor-conversation","user_email":"secret@example.com"}`)

		event, err := normalizeEvent(ProviderCursor, raw, seenAt)

		Expect(err).To(Succeed())
		Expect(event).To(gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
			"SessionIDHash": Equal(testSessionHash(ProviderCursor, "cursor-conversation")),
		}))
		rawEvent, err := json.Marshal(event)
		Expect(err).NotTo(HaveOccurred())
		Expect(string(rawEvent)).NotTo(ContainSubstring("cursor-conversation"))
		Expect(string(rawEvent)).NotTo(ContainSubstring("secret@example.com"))
	})
})

type supportedProviderEventCase struct {
	provider          ProviderID
	payload           string
	wantEvent         AgentEventName
	wantToolName      AgentToolName
	wantMCPTool       bool
	wantSubagentDelta int
	wantEndsSession   bool
}

var _ = DescribeTable("supported provider event normalization",
	Label("unit"),
	func(tc supportedProviderEventCase) {
		seenAt := time.Date(2026, 6, 7, 12, 0, 0, 0, time.UTC)

		event, err := normalizeEvent(tc.provider, []byte(tc.payload), seenAt)

		Expect(err).To(Succeed())
		Expect(event).To(gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
			"Provider":      Equal(tc.provider),
			"EventName":     Equal(tc.wantEvent),
			"ToolName":      Equal(tc.wantToolName),
			"IsMCPTool":     Equal(tc.wantMCPTool),
			"SubagentDelta": Equal(tc.wantSubagentDelta),
			"EndsSession":   Equal(tc.wantEndsSession),
		}))
		rawEvent, err := json.Marshal(event)
		Expect(err).NotTo(HaveOccurred())
		for _, sensitive := range []string{"raw-codex-session", "claude-session", "cursor-session", "private", "/secret", "secret@example.com"} {
			Expect(string(rawEvent)).NotTo(ContainSubstring(sensitive), "normalized event leaked sensitive value %q: %s", sensitive, rawEvent)
		}
	},
	Entry("codex session start", supportedProviderEventCase{provider: ProviderCodex, payload: `{"hook_event_name":"SessionStart","session_id":"codex-session","model":"gpt-5.4","source":"cli"}`, wantEvent: AgentEventSessionStart}),
	Entry("codex permission request", supportedProviderEventCase{provider: ProviderCodex, payload: `{"hook_event_name":"PermissionRequest","session_id":"codex-session","tool_name":"Shell"}`, wantEvent: AgentEventPermissionRequest, wantToolName: newFixtureAgentToolName("Shell")}),
	Entry("codex post tool use", supportedProviderEventCase{provider: ProviderCodex, payload: `{"hook_event_name":"PostToolUse","session_id":"codex-session","tool_name":"mcp__leafwiki__wiki_update_page","tool_response":"secret output"}`, wantEvent: AgentEventPostToolUse, wantToolName: newFixtureAgentToolName("mcp__leafwiki__wiki_update_page"), wantMCPTool: true}),
	Entry("codex user prompt submit", supportedProviderEventCase{provider: ProviderCodex, payload: `{"hook_event_name":"UserPromptSubmit","session_id":"codex-session","prompt":"private prompt"}`, wantEvent: AgentEventUserPromptSubmit}),
	Entry("codex stop", supportedProviderEventCase{provider: ProviderCodex, payload: `{"hook_event_name":"Stop","session_id":"codex-session","last_assistant_message":"private output"}`, wantEvent: AgentEventStop}),
	Entry("codex subagent start", supportedProviderEventCase{provider: ProviderCodex, payload: `{"hook_event_name":"SubagentStart","session_id":"codex-session"}`, wantEvent: AgentEventSubagentStart, wantSubagentDelta: 1}),
	Entry("codex subagent stop", supportedProviderEventCase{provider: ProviderCodex, payload: `{"hook_event_name":"SubagentStop","session_id":"codex-session"}`, wantEvent: AgentEventSubagentStop, wantSubagentDelta: -1}),
	Entry("claude session start", supportedProviderEventCase{provider: ProviderClaude, payload: `{"hook_event_name":"SessionStart","session_id":"claude-session","model":"claude-opus-4","source":"startup"}`, wantEvent: AgentEventSessionStart}),
	Entry("claude session end", supportedProviderEventCase{provider: ProviderClaude, payload: `{"hook_event_name":"SessionEnd","session_id":"claude-session","reason":"clear"}`, wantEvent: AgentEventSessionEnd, wantEndsSession: true}),
	Entry("claude pre tool use", supportedProviderEventCase{provider: ProviderClaude, payload: `{"hook_event_name":"PreToolUse","session_id":"claude-session","tool_name":"Read","tool_input":{"file_path":"/secret"}}`, wantEvent: AgentEventPreToolUse, wantToolName: newFixtureAgentToolName("Read")}),
	Entry("claude post tool use", supportedProviderEventCase{provider: ProviderClaude, payload: `{"hook_event_name":"PostToolUse","session_id":"claude-session","tool_name":"mcp__leafwiki__wiki_get_page","tool_response":"private"}`, wantEvent: AgentEventPostToolUse, wantToolName: newFixtureAgentToolName("mcp__leafwiki__wiki_get_page"), wantMCPTool: true}),
	Entry("claude user prompt submit", supportedProviderEventCase{provider: ProviderClaude, payload: `{"hook_event_name":"UserPromptSubmit","session_id":"claude-session","prompt":"private prompt"}`, wantEvent: AgentEventUserPromptSubmit}),
	Entry("claude stop", supportedProviderEventCase{provider: ProviderClaude, payload: `{"hook_event_name":"Stop","session_id":"claude-session","last_assistant_message":"private"}`, wantEvent: AgentEventStop}),
	Entry("claude subagent start", supportedProviderEventCase{provider: ProviderClaude, payload: `{"hook_event_name":"SubagentStart","session_id":"claude-session","agent_transcript_path":"/secret"}`, wantEvent: AgentEventSubagentStart, wantSubagentDelta: 1}),
	Entry("claude subagent stop", supportedProviderEventCase{provider: ProviderClaude, payload: `{"hook_event_name":"SubagentStop","session_id":"claude-session","agent_transcript_path":"/secret"}`, wantEvent: AgentEventSubagentStop, wantSubagentDelta: -1}),
	Entry("cursor session start", supportedProviderEventCase{provider: ProviderCursor, payload: `{"hook_event_name":"sessionStart","session_id":"cursor-session","conversation_id":"cursor-conversation","model":"gpt-5.4","user_email":"secret@example.com"}`, wantEvent: newFixtureAgentEventName("sessionStart")}),
	Entry("cursor session end", supportedProviderEventCase{provider: ProviderCursor, payload: `{"hook_event_name":"sessionEnd","session_id":"cursor-session","reason":"stop"}`, wantEvent: newFixtureAgentEventName("sessionEnd"), wantEndsSession: true}),
	Entry("cursor pre tool use", supportedProviderEventCase{provider: ProviderCursor, payload: `{"hook_event_name":"preToolUse","session_id":"cursor-session","tool_name":"Read","tool_input":{"path":"/secret"}}`, wantEvent: newFixtureAgentEventName("preToolUse"), wantToolName: newFixtureAgentToolName("Read")}),
	Entry("cursor post tool use", supportedProviderEventCase{provider: ProviderCursor, payload: `{"hook_event_name":"postToolUse","session_id":"cursor-session","tool_name":"mcp__leafwiki__wiki_get_page","tool_output":"private"}`, wantEvent: newFixtureAgentEventName("postToolUse"), wantToolName: newFixtureAgentToolName("mcp__leafwiki__wiki_get_page"), wantMCPTool: true}),
	Entry("cursor before mcp execution", supportedProviderEventCase{provider: ProviderCursor, payload: `{"hook_event_name":"beforeMCPExecution","session_id":"cursor-session","tool_name":"leafwiki.get_page"}`, wantEvent: newFixtureAgentEventName("beforeMCPExecution"), wantToolName: newFixtureAgentToolName("leafwiki.get_page"), wantMCPTool: true}),
	Entry("cursor after mcp execution", supportedProviderEventCase{provider: ProviderCursor, payload: `{"hook_event_name":"afterMCPExecution","session_id":"cursor-session","tool_name":"leafwiki.get_page","tool_output":"private"}`, wantEvent: newFixtureAgentEventName("afterMCPExecution"), wantToolName: newFixtureAgentToolName("leafwiki.get_page"), wantMCPTool: true}),
	Entry("cursor before submit prompt", supportedProviderEventCase{provider: ProviderCursor, payload: `{"hook_event_name":"beforeSubmitPrompt","session_id":"cursor-session","prompt":"private prompt"}`, wantEvent: newFixtureAgentEventName("beforeSubmitPrompt")}),
	Entry("cursor stop", supportedProviderEventCase{provider: ProviderCursor, payload: `{"hook_event_name":"stop","session_id":"cursor-session","status":"done"}`, wantEvent: newFixtureAgentEventName("stop")}),
	Entry("cursor subagent start", supportedProviderEventCase{provider: ProviderCursor, payload: `{"hook_event_name":"subagentStart","session_id":"cursor-session","parent_conversation_id":"parent-conversation"}`, wantEvent: newFixtureAgentEventName("subagentStart"), wantSubagentDelta: 1}),
	Entry("cursor subagent stop", supportedProviderEventCase{provider: ProviderCursor, payload: `{"hook_event_name":"subagentStop","session_id":"cursor-session","parent_conversation_id":"parent-conversation"}`, wantEvent: newFixtureAgentEventName("subagentStop"), wantSubagentDelta: -1}),
)

type normalizeFailureCase struct {
	provider ProviderID
	payload  string
}

var _ = DescribeTable("normalization failure handling",
	Label("unit"),
	func(tc normalizeFailureCase) {
		seenAt := time.Date(2026, 6, 7, 12, 30, 0, 0, time.UTC)

		event, err := normalizeEvent(tc.provider, []byte(tc.payload), seenAt)

		Expect(err).To(MatchError(errNormalizeRejected), "Normalize accepted event %#v", event)
		Expect(event).To(Equal(Event{}))
	},
	Entry("malformed JSON", normalizeFailureCase{provider: ProviderCodex, payload: `{`}),
	Entry("missing event", normalizeFailureCase{provider: ProviderCodex, payload: `{"session_id":"s1"}`}),
	Entry("missing session", normalizeFailureCase{provider: ProviderClaude, payload: `{"hook_event_name":"SessionStart"}`}),
	Entry("null session", normalizeFailureCase{provider: ProviderClaude, payload: `{"hook_event_name":"SessionStart","session_id":null}`}),
	Entry("unknown event", normalizeFailureCase{provider: ProviderCursor, payload: `{"hook_event_name":"notARealEvent","session_id":"s1"}`}),
	Entry("unknown provider", normalizeFailureCase{provider: ProviderUnknown, payload: `{"hook_event_name":"SessionStart","session_id":"s1"}`}),
)

type allowResponseCase struct {
	provider string
	want     string
}

const (
	rawProviderCodexFixture       = "codex"
	rawProviderClaudeFixture      = "claude"
	rawProviderCursorFixture      = "cursor"
	rawProviderUnknownFixture     = "unknown"
	rawProviderUnsupportedFixture = "unsupported"
)

var _ = DescribeTable("provider permission response protocol",
	Label("unit"),
	func(tc allowResponseCase) {
		Expect(string(AllowResponse(tc.provider))).To(Equal(tc.want))
	},
	Entry("codex", allowResponseCase{provider: rawProviderCodexFixture, want: "{}\n"}),
	Entry("claude", allowResponseCase{provider: rawProviderClaudeFixture, want: "{}\n"}),
	Entry("cursor", allowResponseCase{provider: rawProviderCursorFixture, want: "{\"permission\":\"allow\"}\n"}),
	Entry("unknown", allowResponseCase{provider: rawProviderUnknownFixture, want: ""}),
	Entry("unsupported", allowResponseCase{provider: rawProviderUnsupportedFixture, want: ""}),
)

var _ = Describe("agent hook normalized event validation", Label("unit"), func() {
	It("accepts normalized events produced by Normalize", func() {
		event, err := normalizeEvent(ProviderCodex, []byte(`{
			"hook_event_name":"PreToolUse",
			"session_id":"normalized-session",
			"tool_name":"mcp__leafwiki__wiki_get_page"
		}`), time.Now())

		Expect(err).To(Succeed())
		Expect(event).To(beNormalizedAgentHookEvent())
	})

	It("collapses whitespace in safe metadata values", func() {
		event, err := normalizeEvent(ProviderCodex, []byte(`{
			"hook_event_name":"PreToolUse",
			"session_id":"metadata-session",
			"model":"  gpt   5.4  ",
			"tool_name":"  Shell   Command  "
		}`), time.Now())

		Expect(err).To(Succeed())
		Expect(event).To(gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
			"Model":    Equal("gpt 5.4"),
			"ToolName": Equal(newFixtureAgentToolName("Shell Command")),
		}))
	})

	It("truncates safe model and tool metadata at their public bounds", func() {
		event, err := normalizeEvent(ProviderCodex, []byte(`{
			"hook_event_name":"PreToolUse",
			"session_id":"metadata-session",
			"model":"abcdefghijklmnopqrstuvwxyzabcdefghijklmnopqrstuvwxyzabcdefghijklmnopqrstuvwxyzabcdefghijklmnopqrstuvwxyz",
			"tool_name":"abcdefghijklmnopqrstuvwxyzabcdefghijklmnopqrstuvwxyzabcdefghijklmnopqrstuvwxyzabcdefghijklmnopqrstuvwxyzabcdefghijklmnopqrstuvwxyzabcdefghijklmnopqrstuvwxyzabcdefghijklmnopqrstuvwxyzabcdefghijklmnopqrstuvwxyz"
		}`), time.Now())

		Expect(err).To(Succeed())
		Expect(event).To(gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
			"Model":    HaveLen(80),
			"ToolName": HaveLen(160),
		}))
	})

	It("uses cursor parent conversation ID for subagent session identity", func() {
		event, err := normalizeEvent(ProviderCursor, []byte(`{
			"hook_event_name":"subagentStart",
			"session_id":"child-session",
			"conversation_id":"child-conversation",
			"parent_conversation_id":"parent-conversation"
		}`), time.Now())

		Expect(err).To(Succeed())
		Expect(event).To(gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
			"SessionIDHash": Equal(testSessionHash(ProviderCursor, "parent-conversation")),
			"SubagentDelta": Equal(1),
		}))
	})

	DescribeTable("rejects malformed normalized-event contracts",
		func(mutator func(Event) Event) {
			event, err := normalizeEvent(ProviderCodex, []byte(`{
				"hook_event_name":"SubagentStart",
				"session_id":"normalized-session",
				"tool_name":"mcp__leafwiki__wiki_get_page"
			}`), time.Now())
			Expect(err).To(Succeed())

			Expect(mutator(event)).NotTo(beNormalizedAgentHookEvent())
		},
		Entry("provider with surrounding whitespace", func(event Event) Event {
			event.Provider = newFixtureProviderID(" codex")
			return event
		}),
		Entry("unsupported provider", func(event Event) Event {
			event.Provider = ProviderUnknown
			return event
		}),
		Entry("unsupported event", func(event Event) Event {
			event.EventName = newFixtureAgentEventName("NotARealEvent")
			return event
		}),
		Entry("trimmed tool name mismatch", func(event Event) Event {
			event.ToolName += newFixtureAgentToolName(" ")
			return event
		}),
		Entry("bad session hash prefix", func(event Event) Event {
			event.SessionIDHash = strings.TrimPrefix(event.SessionIDHash, "sha256:")
			return event
		}),
		Entry("bad session hash digest", func(event Event) Event {
			event.SessionIDHash = "sha256:not-hex"
			return event
		}),
		Entry("mismatched mcp flag", func(event Event) Event {
			event.IsMCPTool = false
			return event
		}),
		Entry("mismatched subagent delta", func(event Event) Event {
			event.SubagentDelta = 0
			return event
		}),
		Entry("mismatched ends-session flag", func(event Event) Event {
			event.Provider = ProviderClaude
			event.EventName = AgentEventSessionEnd
			event.EndsSession = false
			return event
		}),
	)

	DescribeTable("normalizes safe source values",
		func(rawSource string, want AgentSource) {
			event, err := normalizeEvent(ProviderCodex, []byte(`{
				"hook_event_name":"SessionStart",
				"session_id":"source-session",
				"source":`+rawSource+`
			}`), time.Now())

			Expect(err).To(Succeed())
			Expect(event.Source).To(Equal(want))
		},
		Entry("hook", `"hook"`, AgentSourceHook),
		Entry("mcp", `"mcp"`, AgentSourceMCP),
		Entry("tool", `"tool"`, AgentSourceTool),
		Entry("user", `"user"`, AgentSourceUser),
		Entry("ide", `"ide"`, AgentSourceIDE),
		Entry("agent", `"agent"`, AgentSourceAgent),
		Entry("unknown safe source", `"custom-integrator"`, AgentSourceUnknown),
	)
})

func testSessionHash(provider ProviderID, rawSessionID string) string {
	return hashSessionID(provider, SessionIDFromString(rawSessionID))
}
