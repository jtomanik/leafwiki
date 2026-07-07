package projectdaemon

import (
	"io"

	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/gstruct"

	"github.com/perber/wiki/internal/agenthooks"
)

var errProjectdaemonRandomUnavailable = io.ErrUnexpectedEOF
var errProjectdaemonConfigMarshalUnavailable = io.ErrClosedPipe

var _ = ginkgo.Describe("project daemon configuration helper semantics", ginkgo.Label("unit"), func() {
	ginkgo.It("copies mismatch slices without leaking mutable input", func() {
		mismatches := []Mismatch{{Field: "flag-only"}}

		mismatchErr := NewConfigMismatchError(mismatches)
		mismatches[0].Field = "mutated"

		Expect(configMismatchObservationFor(mismatchErr)).To(Equal(configMismatchObservation{
			Fields: []string{"flag-only"},
		}))
	})

	ginkgo.It("redacts secret-bearing config mismatches while preserving ordinary empty values", func() {
		owner := Config{
			PrivateMCPToken:        "",
			InjectCodeInHeaderHash: HashSecret("owner-code"),
			CustomStylesheet:       "",
		}
		requested := owner
		requested.PrivateMCPToken = "runtime-secret"
		requested.InjectCodeInHeaderHash = ""
		requested.CustomStylesheet = "theme.css"

		Expect(CompareConfig(owner, requested)).To(ContainElements(
			gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
				"Field": Equal("private-mcp-token"),
				"Want":  Equal("empty"),
				"Got":   Equal("redacted"),
			}),
			gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
				"Field": Equal("inject-code-in-header-hash"),
				"Want":  Equal("redacted"),
				"Got":   Equal("empty"),
			}),
			gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
				"Field": Equal("custom-stylesheet"),
				"Want":  Equal(`""`),
				"Got":   Equal("theme.css"),
			}),
		))
	})

	ginkgo.It("surfaces entropy and marshaling failures from test seams", func() {
		originalReadRandom := readRandom
		readRandom = func([]byte) (int, error) {
			return 0, errProjectdaemonRandomUnavailable
		}
		ginkgo.DeferCleanup(func() {
			readRandom = originalReadRandom
		})

		_, err := RandomToken()
		Expect(err).To(MatchError(errProjectdaemonRandomUnavailable))

		originalMarshalConfigJSON := marshalConfigJSON
		marshalConfigJSON = func(any) ([]byte, error) {
			return nil, errProjectdaemonConfigMarshalUnavailable
		}
		ginkgo.DeferCleanup(func() {
			marshalConfigJSON = originalMarshalConfigJSON
		})

		_, err = ConfigHash(Config{})
		Expect(err).To(MatchError(errProjectdaemonConfigMarshalUnavailable))
	})
})

var _ = ginkgo.Describe("project daemon agent presence helper semantics", ginkgo.Label("unit"), func() {
	ginkgo.It("classifies seen state and sanitized source values", func() {
		registry := NewAgentPresenceRegistry(DefaultIdleTimeout, nil)
		Expect(agentPresenceSeenStateFor(registry)).To(Equal(agentPresenceNeverSeen))

		registry.Record(normalizedPresenceEvent(agenthooks.ProviderCodex, `{"hook_event_name":"SessionStart","session_id":"codex-session","source":"cli"}`))

		Expect(agentPresenceSeenStateFor(registry)).To(Equal(agentPresenceWasSeen))
		Expect(agentMetadataSourceFor("CLI")).To(Equal(agentMetadataSource{
			Source: agenthooks.AgentSourceCLI,
			State:  agentMetadataAccepted,
		}))
		Expect(agentMetadataSourceFor("bearer token")).To(Equal(agentMetadataSource{
			State: agentMetadataRejected,
		}))
	})
})

type configMismatchObservation struct {
	Fields []string
}

func configMismatchObservationFor(mismatchErr *ConfigMismatchError) configMismatchObservation {
	fields := make([]string, 0, len(mismatchErr.Mismatches))
	for _, mismatch := range mismatchErr.Mismatches {
		fields = append(fields, mismatch.Field)
	}
	return configMismatchObservation{
		Fields: fields,
	}
}

type agentPresenceSeenMarker uint8

const (
	agentPresenceNeverSeen agentPresenceSeenMarker = iota
	agentPresenceWasSeen
)

func agentPresenceSeenStateFor(registry *AgentPresenceRegistry) agentPresenceSeenMarker {
	if registry.SeenPresence() {
		return agentPresenceWasSeen
	}
	return agentPresenceNeverSeen
}

type agentMetadataState uint8

const (
	agentMetadataRejected agentMetadataState = iota
	agentMetadataAccepted
)

type agentMetadataSource struct {
	Source agenthooks.AgentSource
	State  agentMetadataState
}

func agentMetadataSourceFor(raw string) agentMetadataSource {
	source := safeAgentSource(raw)
	if source == "" {
		return agentMetadataSource{State: agentMetadataRejected}
	}
	return agentMetadataSource{Source: source, State: agentMetadataAccepted}
}
