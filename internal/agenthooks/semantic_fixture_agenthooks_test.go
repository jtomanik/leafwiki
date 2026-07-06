package agenthooks

func newFixtureProviderID[T ~string](raw T) ProviderID {
	return ProviderIDFromString(raw)
}

func newFixtureAgentEventName[T ~string](raw T) AgentEventName {
	return AgentEventName(raw)
}

func newFixtureAgentToolName[T ~string](raw T) AgentToolName {
	return AgentToolNameFromString(raw)
}
