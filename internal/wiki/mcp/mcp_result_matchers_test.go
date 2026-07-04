package mcp_test

import (
	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
	. "github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega/gcustom"
	"github.com/onsi/gomega/types"
)

func matchSuccessfulToolResultWithStructuredContent(content types.GomegaMatcher) types.GomegaMatcher {
	GinkgoHelper()

	return gcustom.MakeMatcher(func(result *sdkmcp.CallToolResult) (bool, error) {
		if result == nil || result.IsError {
			return false, nil
		}
		return content.Match(result.StructuredContent)
	}).WithMessage("be a successful MCP tool result with matching structured content")
}
