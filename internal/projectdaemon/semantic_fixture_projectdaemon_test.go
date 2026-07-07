package projectdaemon

import (
	"encoding/json"

	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/gstruct"
	"github.com/onsi/gomega/types"
	"github.com/perber/wiki/internal/agenthooks"
	"github.com/perber/wiki/internal/workspaceid"
)

func newFixtureSessionID[T ~string](raw T) SessionID {
	return SessionIDFromString(raw)
}

func mustDecodeWorkspaceID(raw string) workspaceid.WorkspaceID {
	ginkgo.GinkgoHelper()

	payload, err := json.Marshal(raw)
	Expect(err).To(Succeed())
	var id workspaceid.WorkspaceID
	Expect(json.Unmarshal(payload, &id)).To(Succeed())
	return id
}

func mustDecodeAgentEventName(raw string) agenthooks.AgentEventName {
	ginkgo.GinkgoHelper()

	payload, err := json.Marshal(raw)
	Expect(err).To(Succeed())
	var name agenthooks.AgentEventName
	Expect(json.Unmarshal(payload, &name)).To(Succeed())
	return name
}

func mustDecodeAgentToolName(raw string) agenthooks.AgentToolName {
	ginkgo.GinkgoHelper()

	payload, err := json.Marshal(raw)
	Expect(err).To(Succeed())
	var name agenthooks.AgentToolName
	Expect(json.Unmarshal(payload, &name)).To(Succeed())
	return name
}

func mustDecodeAgentSource(raw string) agenthooks.AgentSource {
	ginkgo.GinkgoHelper()

	payload, err := json.Marshal(raw)
	Expect(err).To(Succeed())
	var source agenthooks.AgentSource
	Expect(json.Unmarshal(payload, &source)).To(Succeed())
	return source
}

func mustDecodeProviderID(raw string) agenthooks.ProviderID {
	ginkgo.GinkgoHelper()

	payload, err := json.Marshal(raw)
	Expect(err).To(Succeed())
	var provider agenthooks.ProviderID
	Expect(json.Unmarshal(payload, &provider)).To(Succeed())
	return provider
}

func matchActorContextIdentity(subject string, role string, authMethod string) types.GomegaMatcher {
	return WithTransform(observeActorContextIdentity, gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
		"SubjectID":  Equal(subject),
		"Role":       Equal(role),
		"AuthMethod": Equal(authMethod),
	}))
}

type actorContextIdentity struct {
	SubjectID  string
	Role       string
	AuthMethod string
}

func observeActorContextIdentity(ctx ActorContext) actorContextIdentity {
	return actorContextIdentity{
		SubjectID:  ctx.SubjectID(),
		Role:       ctx.Role,
		AuthMethod: ctx.AuthMethod,
	}
}
