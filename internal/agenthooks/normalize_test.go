package agenthooks

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("agent hook normalization", func() {
	It("TestNormalizeCodexToolEventSanitizesSessionAndMetadata", func() {
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

		Expect(ok).To(BeTrue())
		Expect(event.Provider).To(Equal(ProviderCodex))
		Expect(event.SessionIDHash).To(Equal(testSessionHash(ProviderCodex, "raw-codex-session")))
		Expect(event.EventName).To(Equal(AgentEventName("PreToolUse")))
		Expect(event.Model).To(Equal("gpt-5.4"))
		Expect(event.ToolName).To(Equal(AgentToolName("mcp__leafwiki__wiki_get_page")))
		Expect(event.IsMCPTool).To(BeTrue())
		Expect(event.SeenAt).To(Equal(seenAt))
	})

	It("TestNormalizeUsesSemanticProviderEventSourceAndToolTypes", func() {
		var provider ProviderID = ProviderCodex
		var eventName AgentEventName = AgentEventPreToolUse
		var source AgentSource = AgentSourceCLI
		var tool AgentToolName = AgentToolName("mcp__leafwiki__wiki_get_page")

		event, ok := Normalize(provider, []byte(`{
			"hook_event_name":"PreToolUse",
			"session_id":"typed-contract-session",
			"source":"cli",
			"tool_name":"mcp__leafwiki__wiki_get_page"
		}`), time.Now())

		Expect(ok).To(BeTrue())
		Expect(event.Provider).To(Equal(provider))
		Expect(event.EventName).To(Equal(eventName))
		Expect(event.Source).To(Equal(source))
		Expect(event.ToolName).To(Equal(tool))
	})

	It("TestNormalizeRedactsUnsafeModelSourceAndToolMetadata", func() {
		event, ok := Normalize(ProviderCodex, []byte(`{
			"hook_event_name":"PreToolUse",
			"session_id":"raw-codex-session",
			"model":"/Users/example/token-model",
			"source":"/Users/example/.codex/session.jsonl",
			"tool_name":"Read /secret/token"
		}`), time.Now())

		Expect(ok).To(BeTrue())
		Expect(event.Model).To(BeEmpty())
		Expect(event.Source).To(BeEmpty())
		Expect(event.ToolName).To(BeEmpty())
		Expect(event.IsMCPTool).To(BeFalse())
	})

	It("TestNormalizeCursorConversationFallbackHashesConversationID", func() {
		seenAt := time.Date(2026, 6, 7, 13, 0, 0, 0, time.UTC)
		raw := []byte(`{"hook_event_name":"sessionStart","conversation_id":"cursor-conversation","user_email":"secret@example.com"}`)

		event, ok := Normalize(ProviderCursor, raw, seenAt)

		Expect(ok).To(BeTrue())
		Expect(event.SessionIDHash).To(Equal(testSessionHash(ProviderCursor, "cursor-conversation")))
		rawEvent, err := json.Marshal(event)
		Expect(err).NotTo(HaveOccurred())
		Expect(string(rawEvent)).NotTo(ContainSubstring("cursor-conversation"))
		Expect(string(rawEvent)).NotTo(ContainSubstring("secret@example.com"))
	})
})

type supportedProviderEventCase struct {
	provider          ProviderID
	payload           string
	wantEvent         string
	wantToolName      string
	wantMCPTool       bool
	wantSubagentDelta int
	wantEndsSession   bool
}

var _ = DescribeTable("TestNormalizeSupportedProviderEvents",
	func(tc supportedProviderEventCase) {
		seenAt := time.Date(2026, 6, 7, 12, 0, 0, 0, time.UTC)

		event, ok := Normalize(tc.provider, []byte(tc.payload), seenAt)

		Expect(ok).To(BeTrue())
		Expect(event.Provider).To(Equal(tc.provider))
		Expect(string(event.EventName)).To(Equal(tc.wantEvent))
		Expect(string(event.ToolName)).To(Equal(tc.wantToolName))
		Expect(event.IsMCPTool).To(Equal(tc.wantMCPTool))
		Expect(event.SubagentDelta).To(Equal(tc.wantSubagentDelta))
		Expect(event.EndsSession).To(Equal(tc.wantEndsSession))
		rawEvent, err := json.Marshal(event)
		Expect(err).NotTo(HaveOccurred())
		for _, sensitive := range []string{"raw-codex-session", "claude-session", "cursor-session", "private", "/secret", "secret@example.com"} {
			Expect(string(rawEvent)).NotTo(ContainSubstring(sensitive), "normalized event leaked sensitive value %q: %s", sensitive, rawEvent)
		}
	},
	Entry("codex session start", supportedProviderEventCase{provider: ProviderCodex, payload: `{"hook_event_name":"SessionStart","session_id":"codex-session","model":"gpt-5.4","source":"cli"}`, wantEvent: "SessionStart"}),
	Entry("codex permission request", supportedProviderEventCase{provider: ProviderCodex, payload: `{"hook_event_name":"PermissionRequest","session_id":"codex-session","tool_name":"Shell"}`, wantEvent: "PermissionRequest", wantToolName: "Shell"}),
	Entry("codex post tool use", supportedProviderEventCase{provider: ProviderCodex, payload: `{"hook_event_name":"PostToolUse","session_id":"codex-session","tool_name":"mcp__leafwiki__wiki_update_page","tool_response":"secret output"}`, wantEvent: "PostToolUse", wantToolName: "mcp__leafwiki__wiki_update_page", wantMCPTool: true}),
	Entry("codex user prompt submit", supportedProviderEventCase{provider: ProviderCodex, payload: `{"hook_event_name":"UserPromptSubmit","session_id":"codex-session","prompt":"private prompt"}`, wantEvent: "UserPromptSubmit"}),
	Entry("codex stop", supportedProviderEventCase{provider: ProviderCodex, payload: `{"hook_event_name":"Stop","session_id":"codex-session","last_assistant_message":"private output"}`, wantEvent: "Stop"}),
	Entry("codex subagent start", supportedProviderEventCase{provider: ProviderCodex, payload: `{"hook_event_name":"SubagentStart","session_id":"codex-session"}`, wantEvent: "SubagentStart", wantSubagentDelta: 1}),
	Entry("codex subagent stop", supportedProviderEventCase{provider: ProviderCodex, payload: `{"hook_event_name":"SubagentStop","session_id":"codex-session"}`, wantEvent: "SubagentStop", wantSubagentDelta: -1}),
	Entry("claude session start", supportedProviderEventCase{provider: ProviderClaude, payload: `{"hook_event_name":"SessionStart","session_id":"claude-session","model":"claude-opus-4","source":"startup"}`, wantEvent: "SessionStart"}),
	Entry("claude session end", supportedProviderEventCase{provider: ProviderClaude, payload: `{"hook_event_name":"SessionEnd","session_id":"claude-session","reason":"clear"}`, wantEvent: "SessionEnd", wantEndsSession: true}),
	Entry("claude pre tool use", supportedProviderEventCase{provider: ProviderClaude, payload: `{"hook_event_name":"PreToolUse","session_id":"claude-session","tool_name":"Read","tool_input":{"file_path":"/secret"}}`, wantEvent: "PreToolUse", wantToolName: "Read"}),
	Entry("claude post tool use", supportedProviderEventCase{provider: ProviderClaude, payload: `{"hook_event_name":"PostToolUse","session_id":"claude-session","tool_name":"mcp__leafwiki__wiki_get_page","tool_response":"private"}`, wantEvent: "PostToolUse", wantToolName: "mcp__leafwiki__wiki_get_page", wantMCPTool: true}),
	Entry("claude user prompt submit", supportedProviderEventCase{provider: ProviderClaude, payload: `{"hook_event_name":"UserPromptSubmit","session_id":"claude-session","prompt":"private prompt"}`, wantEvent: "UserPromptSubmit"}),
	Entry("claude stop", supportedProviderEventCase{provider: ProviderClaude, payload: `{"hook_event_name":"Stop","session_id":"claude-session","last_assistant_message":"private"}`, wantEvent: "Stop"}),
	Entry("claude subagent start", supportedProviderEventCase{provider: ProviderClaude, payload: `{"hook_event_name":"SubagentStart","session_id":"claude-session","agent_transcript_path":"/secret"}`, wantEvent: "SubagentStart", wantSubagentDelta: 1}),
	Entry("claude subagent stop", supportedProviderEventCase{provider: ProviderClaude, payload: `{"hook_event_name":"SubagentStop","session_id":"claude-session","agent_transcript_path":"/secret"}`, wantEvent: "SubagentStop", wantSubagentDelta: -1}),
	Entry("cursor session start", supportedProviderEventCase{provider: ProviderCursor, payload: `{"hook_event_name":"sessionStart","session_id":"cursor-session","conversation_id":"cursor-conversation","model":"gpt-5.4","user_email":"secret@example.com"}`, wantEvent: "sessionStart"}),
	Entry("cursor session end", supportedProviderEventCase{provider: ProviderCursor, payload: `{"hook_event_name":"sessionEnd","session_id":"cursor-session","reason":"stop"}`, wantEvent: "sessionEnd", wantEndsSession: true}),
	Entry("cursor pre tool use", supportedProviderEventCase{provider: ProviderCursor, payload: `{"hook_event_name":"preToolUse","session_id":"cursor-session","tool_name":"Read","tool_input":{"path":"/secret"}}`, wantEvent: "preToolUse", wantToolName: "Read"}),
	Entry("cursor post tool use", supportedProviderEventCase{provider: ProviderCursor, payload: `{"hook_event_name":"postToolUse","session_id":"cursor-session","tool_name":"mcp__leafwiki__wiki_get_page","tool_output":"private"}`, wantEvent: "postToolUse", wantToolName: "mcp__leafwiki__wiki_get_page", wantMCPTool: true}),
	Entry("cursor before mcp execution", supportedProviderEventCase{provider: ProviderCursor, payload: `{"hook_event_name":"beforeMCPExecution","session_id":"cursor-session","tool_name":"leafwiki.get_page"}`, wantEvent: "beforeMCPExecution", wantToolName: "leafwiki.get_page", wantMCPTool: true}),
	Entry("cursor after mcp execution", supportedProviderEventCase{provider: ProviderCursor, payload: `{"hook_event_name":"afterMCPExecution","session_id":"cursor-session","tool_name":"leafwiki.get_page","tool_output":"private"}`, wantEvent: "afterMCPExecution", wantToolName: "leafwiki.get_page", wantMCPTool: true}),
	Entry("cursor before submit prompt", supportedProviderEventCase{provider: ProviderCursor, payload: `{"hook_event_name":"beforeSubmitPrompt","session_id":"cursor-session","prompt":"private prompt"}`, wantEvent: "beforeSubmitPrompt"}),
	Entry("cursor stop", supportedProviderEventCase{provider: ProviderCursor, payload: `{"hook_event_name":"stop","session_id":"cursor-session","status":"done"}`, wantEvent: "stop"}),
	Entry("cursor subagent start", supportedProviderEventCase{provider: ProviderCursor, payload: `{"hook_event_name":"subagentStart","session_id":"cursor-session","parent_conversation_id":"parent-conversation"}`, wantEvent: "subagentStart", wantSubagentDelta: 1}),
	Entry("cursor subagent stop", supportedProviderEventCase{provider: ProviderCursor, payload: `{"hook_event_name":"subagentStop","session_id":"cursor-session","parent_conversation_id":"parent-conversation"}`, wantEvent: "subagentStop", wantSubagentDelta: -1}),
)

type normalizeFailureCase struct {
	provider ProviderID
	payload  string
}

var _ = DescribeTable("TestNormalizeFailsOpenForUnknownMalformedAndIncompletePayloads",
	func(tc normalizeFailureCase) {
		seenAt := time.Date(2026, 6, 7, 12, 30, 0, 0, time.UTC)

		event, ok := Normalize(tc.provider, []byte(tc.payload), seenAt)

		Expect(ok).To(BeFalse(), "Normalize returned ok=true with event %#v", event)
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

var _ = DescribeTable("TestAllowResponseUsesProviderProtocol",
	func(tc allowResponseCase) {
		Expect(string(AllowResponse(tc.provider))).To(Equal(tc.want))
	},
	Entry("codex", allowResponseCase{provider: string(ProviderCodex), want: "{}\n"}),
	Entry("claude", allowResponseCase{provider: string(ProviderClaude), want: "{}\n"}),
	Entry("cursor", allowResponseCase{provider: string(ProviderCursor), want: "{\"permission\":\"allow\"}\n"}),
	Entry("unknown", allowResponseCase{provider: string(ProviderUnknown), want: ""}),
	Entry("unsupported", allowResponseCase{provider: "unsupported", want: ""}),
)

var _ = Describe("agent hook validation coverage", func() {
	It("accepts normalized events produced by Normalize", func() {
		event, ok := Normalize(ProviderCodex, []byte(`{
			"hook_event_name":"PreToolUse",
			"session_id":"normalized-session",
			"tool_name":"mcp__leafwiki__wiki_get_page"
		}`), time.Now())

		Expect(ok).To(BeTrue())
		Expect(IsNormalizedEvent(event)).To(BeTrue())
	})

	It("collapses whitespace in safe metadata values", func() {
		event, ok := Normalize(ProviderCodex, []byte(`{
			"hook_event_name":"PreToolUse",
			"session_id":"metadata-session",
			"model":"  gpt   5.4  ",
			"tool_name":"  Shell   Command  "
		}`), time.Now())

		Expect(ok).To(BeTrue())
		Expect(event.Model).To(Equal("gpt 5.4"))
		Expect(event.ToolName).To(Equal(AgentToolName("Shell Command")))
	})

	It("truncates safe model and tool metadata at their public bounds", func() {
		event, ok := Normalize(ProviderCodex, []byte(`{
			"hook_event_name":"PreToolUse",
			"session_id":"metadata-session",
			"model":"abcdefghijklmnopqrstuvwxyzabcdefghijklmnopqrstuvwxyzabcdefghijklmnopqrstuvwxyzabcdefghijklmnopqrstuvwxyz",
			"tool_name":"abcdefghijklmnopqrstuvwxyzabcdefghijklmnopqrstuvwxyzabcdefghijklmnopqrstuvwxyzabcdefghijklmnopqrstuvwxyzabcdefghijklmnopqrstuvwxyzabcdefghijklmnopqrstuvwxyzabcdefghijklmnopqrstuvwxyzabcdefghijklmnopqrstuvwxyz"
		}`), time.Now())

		Expect(ok).To(BeTrue())
		Expect(event.Model).To(HaveLen(80))
		Expect(event.ToolName).To(HaveLen(160))
	})

	It("uses cursor parent conversation ID for subagent session identity", func() {
		event, ok := Normalize(ProviderCursor, []byte(`{
			"hook_event_name":"subagentStart",
			"session_id":"child-session",
			"conversation_id":"child-conversation",
			"parent_conversation_id":"parent-conversation"
		}`), time.Now())

		Expect(ok).To(BeTrue())
		Expect(event.SessionIDHash).To(Equal(testSessionHash(ProviderCursor, "parent-conversation")))
		Expect(event.SubagentDelta).To(Equal(1))
	})

	DescribeTable("rejects malformed normalized-event contracts",
		func(mutator func(Event) Event) {
			event, ok := Normalize(ProviderCodex, []byte(`{
				"hook_event_name":"SubagentStart",
				"session_id":"normalized-session",
				"tool_name":"mcp__leafwiki__wiki_get_page"
			}`), time.Now())
			Expect(ok).To(BeTrue())

			Expect(IsNormalizedEvent(mutator(event))).To(BeFalse())
		},
		Entry("provider with surrounding whitespace", func(event Event) Event {
			event.Provider = " codex"
			return event
		}),
		Entry("unsupported provider", func(event Event) Event {
			event.Provider = ProviderUnknown
			return event
		}),
		Entry("unsupported event", func(event Event) Event {
			event.EventName = "NotARealEvent"
			return event
		}),
		Entry("trimmed tool name mismatch", func(event Event) Event {
			event.ToolName = AgentToolName(string(event.ToolName) + " ")
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
			event, ok := Normalize(ProviderCodex, []byte(`{
				"hook_event_name":"SessionStart",
				"session_id":"source-session",
				"source":`+rawSource+`
			}`), time.Now())

			Expect(ok).To(BeTrue())
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
	sum := sha256.Sum256([]byte(string(provider) + "\x00" + rawSessionID))
	return "sha256:" + hex.EncodeToString(sum[:])
}
