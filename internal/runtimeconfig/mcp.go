package runtimeconfig

import (
	"fmt"
	"strings"
)

type MCPTransports struct {
	HTTP  bool
	Stdio bool
}

func ParseMCPTransports(raw string) (MCPTransports, error) {
	value := strings.TrimSpace(strings.ToLower(raw))
	if value == "" {
		value = "none"
	}

	parts := strings.Split(value, ",")
	if len(parts) > 2 {
		return MCPTransports{}, fmt.Errorf("invalid MCP transport %q", raw)
	}

	var transports MCPTransports
	seen := map[string]bool{}
	for _, part := range parts {
		name := strings.TrimSpace(part)
		if name == "" {
			return MCPTransports{}, fmt.Errorf("invalid MCP transport %q", raw)
		}
		if seen[name] {
			return MCPTransports{}, fmt.Errorf("duplicate MCP transport %q", name)
		}
		seen[name] = true
		switch name {
		case "none":
		case "http":
			transports.HTTP = true
		case "stdio":
			transports.Stdio = true
		default:
			return MCPTransports{}, fmt.Errorf("invalid MCP transport %q", name)
		}
	}

	if seen["none"] && len(seen) > 1 {
		return MCPTransports{}, fmt.Errorf("none cannot be combined with other MCP transports")
	}
	return transports, nil
}
