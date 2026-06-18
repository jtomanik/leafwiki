package runtimeconfig

import (
	"strings"
	"testing"
)

func TestParseMCPTransports(t *testing.T) {
	got, err := ParseMCPTransports("http,stdio")
	if err != nil {
		t.Fatalf("ParseMCPTransports failed: %v", err)
	}
	if !got.HTTP || !got.Stdio {
		t.Fatalf("transports = %#v, want http and stdio", got)
	}
}

func TestParseMCPTransportsRejectsInvalidValues(t *testing.T) {
	tests := []struct {
		name      string
		raw       string
		wantError string
	}{
		{name: "unknown", raw: "websocket", wantError: "invalid MCP transport"},
		{name: "none combined", raw: "none,stdio", wantError: "none cannot be combined"},
		{name: "duplicate", raw: "stdio,stdio", wantError: "duplicate MCP transport"},
		{name: "empty part", raw: "stdio,", wantError: "invalid MCP transport"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := ParseMCPTransports(tt.raw); err == nil || !strings.Contains(err.Error(), tt.wantError) {
				t.Fatalf("ParseMCPTransports(%q) error = %v, want %q", tt.raw, err, tt.wantError)
			}
		})
	}
}
