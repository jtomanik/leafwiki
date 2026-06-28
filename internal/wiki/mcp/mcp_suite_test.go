package mcp

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestMCPSuite(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "MCP Suite")
}
