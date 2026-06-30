package runtimeconfig

import (
	"fmt"
	"strings"
)

type MCPTransports struct {
	HTTP  bool
	Stdio bool
}

type MCPTransportErrorReason string

const (
	MCPTransportErrorReasonInvalid   MCPTransportErrorReason = "invalid"
	MCPTransportErrorReasonDuplicate MCPTransportErrorReason = "duplicate"
	MCPTransportErrorReasonNoneMixed MCPTransportErrorReason = "none_mixed"
)

type MCPTransportError struct {
	Reason MCPTransportErrorReason
	Value  string
}

func (err MCPTransportError) Error() string {
	switch err.Reason {
	case MCPTransportErrorReasonDuplicate:
		return fmt.Sprintf("duplicate MCP transport %q", err.Value)
	case MCPTransportErrorReasonNoneMixed:
		return "none cannot be combined with other MCP transports"
	default:
		return fmt.Sprintf("invalid MCP transport %q", err.Value)
	}
}

func ParseMCPTransports(raw string) (MCPTransports, error) {
	value := strings.TrimSpace(strings.ToLower(raw))
	if value == "" {
		value = "none"
	}

	parts := strings.Split(value, ",")
	if len(parts) > 2 {
		return MCPTransports{}, MCPTransportError{Reason: MCPTransportErrorReasonInvalid, Value: raw}
	}

	var transports MCPTransports
	seen := map[string]bool{}
	for _, part := range parts {
		name := strings.TrimSpace(part)
		if name == "" {
			return MCPTransports{}, MCPTransportError{Reason: MCPTransportErrorReasonInvalid, Value: raw}
		}
		if seen[name] {
			return MCPTransports{}, MCPTransportError{Reason: MCPTransportErrorReasonDuplicate, Value: name}
		}
		seen[name] = true
		switch name {
		case "none":
		case "http":
			transports.HTTP = true
		case "stdio":
			transports.Stdio = true
		default:
			return MCPTransports{}, MCPTransportError{Reason: MCPTransportErrorReasonInvalid, Value: name}
		}
	}

	if seen["none"] && len(seen) > 1 {
		return MCPTransports{}, MCPTransportError{Reason: MCPTransportErrorReasonNoneMixed}
	}
	return transports, nil
}
