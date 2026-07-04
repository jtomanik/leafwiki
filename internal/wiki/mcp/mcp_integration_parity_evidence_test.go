package mcp_test

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// Canonical Markdown links plan scenarios covered by tests in this file:
// - MCP agent context returns canonical examples

var _ = Describe("local MCP HTTP parity evidence", Label("integration"), func() {
	It("confirms plan-traced MCP tools have matching HTTP route evidence", func() {
		runHTTPMCPParityCoverage()
	})
})

func runHTTPMCPParityCoverage() {
	GinkgoHelper()

	if !hasHTTPMCPParityRecorded() {
		resetHTTPMCPParityCoverage()
		runLocalMCPProtocolPageMutationParity()
		runLocalMCPProtocolPageOperationParity()
		runLocalMCPProtocolIndexAndAssetParity()
		runLocalMCPProtocolFeatureGatedToolParity()
	}
	Expect(snapshotHTTPMCPParityCoverage()).To(matchRecordedHTTPMCPParity())
}
