package projectdaemon

import (
	"encoding/json"
	"errors"
	"time"

	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/gstruct"
	"github.com/onsi/gomega/types"

	"github.com/perber/wiki/internal/agenthooks"
)

var errAgentHookEventRejected = errors.New("agent hook event rejected")

var _ = ginkgo.Describe("agent presence registry", func() {
	ginkgo.It("records sanitized session metadata for accepted agent events", func() {
		now := time.Date(2026, 6, 7, 14, 0, 0, 0, time.UTC)
		var counts []int
		registry := NewAgentPresenceRegistry(DefaultIdleTimeout, func(count int) {
			counts = append(counts, count)
		})
		registry.now = func() time.Time {
			return now
		}

		event := normalizedPresenceEvent(agenthooks.ProviderCodex, `{"hook_event_name":"PreToolUse","session_id":"codex-session","model":"gpt-5.4","source":"cli","tool_name":"mcp__leafwiki__wiki_get_page"}`)
		event.SeenAt = now
		registry.Record(event)

		Expect(registry.Count()).To(Equal(1))
		Expect(registry.List()).To(ConsistOf(matchAgentPresenceSession(gstruct.Fields{
			"Provider":      Equal(agenthooks.ProviderCodex),
			"SessionIDHash": Equal(event.SessionIDHash),
			"FirstSeenAt":   BeTemporally("==", now),
			"LastSeenAt":    BeTemporally("==", now),
			"LastEvent":     Equal(agenthooks.AgentEventName("PreToolUse")),
			"Model":         Equal("gpt-5.4"),
			"Source":        Equal(agenthooks.AgentSourceCLI),
			"ToolName":      Equal(agenthooks.AgentToolName("mcp__leafwiki__wiki_get_page")),
			"IsMCPTool":     BeTrue(),
		})))
		Expect(counts).To(Equal([]int{1}))

		raw, err := json.Marshal(registry.List()[0])
		Expect(err).NotTo(HaveOccurred())
		var wire struct {
			Provider  agenthooks.ProviderID     `json:"provider"`
			LastEvent agenthooks.AgentEventName `json:"lastEvent"`
			Source    agenthooks.AgentSource    `json:"source"`
			ToolName  agenthooks.AgentToolName  `json:"toolName"`
		}
		Expect(json.Unmarshal(raw, &wire)).To(Succeed())
		Expect(wire).To(gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
			"Provider":  Equal(agenthooks.ProviderCodex),
			"LastEvent": Equal(agenthooks.AgentEventPreToolUse),
			"Source":    Equal(agenthooks.AgentSourceCLI),
			"ToolName":  Equal(agenthooks.AgentToolName("mcp__leafwiki__wiki_get_page")),
		}))
	})

	ginkgo.It("redacts unsafe model and source metadata while preserving safe MCP tool names", func() {
		registry := NewAgentPresenceRegistry(DefaultIdleTimeout, nil)
		event := normalizedPresenceEvent(agenthooks.ProviderCodex, `{"hook_event_name":"PreToolUse","session_id":"codex-session","model":"gpt-5.4","tool_name":"mcp__leafwiki__wiki_get_page"}`)
		event.Model = "/Users/example/token-model"
		event.Source = "/Users/example/.codex/session.jsonl"

		registry.Record(event)

		Expect(registry.List()).To(ConsistOf(matchAgentPresenceSession(gstruct.Fields{
			"Model":     BeEmpty(),
			"Source":    BeEmpty(),
			"ToolName":  Equal(agenthooks.AgentToolName("mcp__leafwiki__wiki_get_page")),
			"IsMCPTool": BeTrue(),
		})))
	})

	ginkgo.It("reports seen state after accepted events", func() {
		registry := NewAgentPresenceRegistry(DefaultIdleTimeout, nil)
		Expect(registry.SeenPresence()).To(BeFalse())

		event := normalizedPresenceEvent(agenthooks.ProviderCodex, `{"hook_event_name":"SessionStart","session_id":"codex-session"}`)
		registry.Record(event)

		Expect(registry.SeenPresence()).To(BeTrue())
		Expect(registry).To(reportSeenPresenceState(true, 1))
	})

	ginkgo.It("updates lifecycle metadata and expires idle sessions", func() {
		now := time.Date(2026, 6, 7, 14, 0, 0, 0, time.UTC)
		var counts []int
		registry := NewAgentPresenceRegistry(time.Minute, func(count int) {
			counts = append(counts, count)
		})
		registry.now = func() time.Time {
			return now
		}

		event := normalizedPresenceEvent(agenthooks.ProviderClaude, `{"hook_event_name":"SessionStart","session_id":"claude-session","model":"claude-opus-4"}`)
		event.SeenAt = now
		registry.Record(event)
		now = now.Add(10 * time.Second)
		event = normalizedPresenceEvent(agenthooks.ProviderClaude, `{"hook_event_name":"PreToolUse","session_id":"claude-session","tool_name":"Read"}`)
		event.SeenAt = now
		registry.Record(event)

		Expect(registry.List()).To(ConsistOf(matchAgentPresenceSession(gstruct.Fields{
			"FirstSeenAt": BeTemporally("==", time.Date(2026, 6, 7, 14, 0, 0, 0, time.UTC)),
			"LastSeenAt":  BeTemporally("==", now),
			"LastEvent":   Equal(agenthooks.AgentEventName("PreToolUse")),
			"ToolName":    Equal(agenthooks.AgentToolName("Read")),
		})))
		Expect(counts).To(Equal([]int{1}))

		event = normalizedPresenceEvent(agenthooks.ProviderClaude, `{"hook_event_name":"SubagentStop","session_id":"claude-session"}`)
		event.SeenAt = now.Add(1 * time.Second)
		registry.Record(event)
		Expect(registry.List()).To(ConsistOf(HaveField("ActiveSubagents", 0)))
		event = normalizedPresenceEvent(agenthooks.ProviderClaude, `{"hook_event_name":"SubagentStart","session_id":"claude-session"}`)
		event.SeenAt = now.Add(2 * time.Second)
		registry.Record(event)
		event = normalizedPresenceEvent(agenthooks.ProviderClaude, `{"hook_event_name":"SubagentStop","session_id":"claude-session"}`)
		event.SeenAt = now.Add(3 * time.Second)
		registry.Record(event)
		Expect(registry.List()).To(ConsistOf(HaveField("ActiveSubagents", 0)))

		event = normalizedPresenceEvent(agenthooks.ProviderClaude, `{"hook_event_name":"SessionEnd","session_id":"claude-session"}`)
		event.SeenAt = now.Add(4 * time.Second)
		registry.Record(event)
		Expect(registry.Count()).To(BeZero())

		event = normalizedPresenceEvent(agenthooks.ProviderCursor, `{"hook_event_name":"sessionStart","session_id":"cursor-session"}`)
		event.SeenAt = now
		registry.Record(event)
		now = now.Add(2 * time.Minute)
		Expect(registry.PruneExpired()).To(BeZero())
		Expect(counts).To(Equal([]int{1, 0, 1, 0}))
	})

	ginkgo.DescribeTable("ignores end events before any matching start event",
		func(provider agenthooks.ProviderID, payload string) {
			var counts []int
			registry := NewAgentPresenceRegistry(time.Minute, func(count int) {
				counts = append(counts, count)
			})
			event := normalizedPresenceEvent(provider, payload)

			registry.Record(event)

			Expect(registry).To(reportSeenPresenceState(false, 0))
			Expect(counts).To(BeEmpty())
		},
		ginkgo.Entry("claude session end", agenthooks.ProviderClaude, `{"hook_event_name":"SessionEnd","session_id":"claude-ended-before-start"}`),
		ginkgo.Entry("cursor session end", agenthooks.ProviderCursor, `{"hook_event_name":"sessionEnd","session_id":"cursor-ended-before-start"}`),
	)

	ginkgo.DescribeTable("rejects unsafe control events",
		func(eventFactory func() agenthooks.Event) {
			var counts []int
			registry := NewAgentPresenceRegistry(time.Minute, func(count int) {
				counts = append(counts, count)
			})

			registry.Record(eventFactory())

			Expect(registry).To(reportSeenPresenceState(false, 0))
			Expect(counts).To(BeEmpty())
		},
		ginkgo.Entry("raw session id", func() agenthooks.Event {
			return agenthooks.Event{
				Provider:      agenthooks.ProviderCodex,
				SessionIDHash: "raw-session-secret",
				EventName:     "SessionStart",
			}
		}),
		ginkgo.Entry("unsupported provider", func() agenthooks.Event {
			event := normalizedPresenceEvent(agenthooks.ProviderCodex, `{"hook_event_name":"SessionStart","session_id":"safe-session"}`)
			event.Provider = "sidecar"
			return event
		}),
		ginkgo.Entry("unsupported event", func() agenthooks.Event {
			event := normalizedPresenceEvent(agenthooks.ProviderCodex, `{"hook_event_name":"SessionStart","session_id":"safe-session"}`)
			event.EventName = "MadeUpHook"
			return event
		}),
		ginkgo.Entry("mismatched subagent delta", func() agenthooks.Event {
			event := normalizedPresenceEvent(agenthooks.ProviderCodex, `{"hook_event_name":"SessionStart","session_id":"safe-session"}`)
			event.SubagentDelta = 1
			return event
		}),
	)

	ginkgo.It("retains newly recorded agent sessions until pruning removes expired sessions", func() {
		now := time.Date(2026, 6, 7, 14, 30, 0, 0, time.UTC)
		var counts []int
		registry := NewAgentPresenceRegistry(0, func(count int) {
			counts = append(counts, count)
		})
		registry.now = func() time.Time {
			return now
		}
		event := normalizedPresenceEvent(agenthooks.ProviderCodex, `{"hook_event_name":"SessionStart","session_id":"zero-ttl-session"}`)
		event.SeenAt = now

		registry.Record(event)
		Expect(registry.Count()).To(Equal(1))

		Expect(registry.PruneExpired()).To(BeZero())
		Expect(counts).To(Equal([]int{1, 0}))
	})
})

func normalizedPresenceEvent(provider agenthooks.ProviderID, payload string) agenthooks.Event {
	ginkgo.GinkgoHelper()

	event, err := normalizedAgentHookEventResult(provider, []byte(payload), time.Now())
	Expect(err).To(Succeed())
	Expect(event).To(gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
		"Provider":      Equal(provider.Normalize()),
		"SessionIDHash": Not(BeEmpty()),
		"EventName":     Not(BeEmpty()),
	}))
	return event
}

func normalizedAgentHookEventResult(provider agenthooks.ProviderID, raw []byte, seenAt time.Time) (agenthooks.Event, error) {
	event, accepted := agenthooks.Normalize(provider, raw, seenAt)
	if !accepted {
		return agenthooks.Event{}, errAgentHookEventRejected
	}
	return event, nil
}

func matchAgentPresenceSession(fields gstruct.Fields) types.GomegaMatcher {
	return gstruct.MatchFields(gstruct.IgnoreExtras, fields)
}

func reportSeenPresenceState(seen bool, count int) types.GomegaMatcher {
	return WithTransform(func(registry *AgentPresenceRegistry) agentPresenceSeenState {
		ginkgo.GinkgoHelper()
		gotSeen, gotCount := registry.SeenPresenceCount()
		return agentPresenceSeenState{seen: gotSeen, count: gotCount}
	}, Equal(agentPresenceSeenState{seen: seen, count: count}))
}

type agentPresenceSeenState struct {
	seen  bool
	count int
}
