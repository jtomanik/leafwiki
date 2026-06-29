package agenthooks

import "strings"

type ProviderID string
type AgentEventName string
type AgentToolName string

func (id ProviderID) Normalize() ProviderID {
	return ProviderID(strings.ToLower(strings.TrimSpace(string(id))))
}

func (name AgentEventName) Normalize() AgentEventName {
	return AgentEventName(strings.TrimSpace(string(name)))
}

func (name AgentToolName) Normalize() AgentToolName {
	return AgentToolName(strings.TrimSpace(string(name)))
}

func (name AgentToolName) SanitizedMetadataValue() AgentToolName {
	value := strings.TrimSpace(string(name))
	value = strings.Join(strings.Fields(value), " ")
	return AgentToolName(strings.ReplaceAll(value, "\t", " "))
}

func (name AgentToolName) HasMCPPrefix() bool {
	return strings.HasPrefix(string(name), "mcp__")
}

func leakProvider(id ProviderID) string {
	return strings.TrimSpace(string(id)) // want "semantic value ProviderID converted to string before internal call TrimSpace; make the callee accept ProviderID"
}
