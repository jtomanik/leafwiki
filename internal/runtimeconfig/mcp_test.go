package runtimeconfig

import (
	"errors"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/gstruct"
	"github.com/onsi/gomega/types"
)

func matchMCPTransports(http bool, stdio bool) types.GomegaMatcher {
	return gstruct.MatchAllFields(gstruct.Fields{
		"HTTP":  Equal(http),
		"Stdio": Equal(stdio),
	})
}

func matchMCPTransportError(reason MCPTransportErrorReason) types.GomegaMatcher {
	return WithTransform(func(err error) MCPTransportError {
		var transportErr MCPTransportError
		_ = errors.As(err, &transportErr)
		return transportErr
	}, gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
		"Reason": Equal(reason),
	}))
}

var _ = Describe("MCP transport parsing", func() {
	It("TestParseMCPTransports", func() {
		got, err := ParseMCPTransports("http,stdio")

		Expect(err).NotTo(HaveOccurred())
		Expect(got).To(matchMCPTransports(true, true))
	})
})

type invalidMCPTransportCase struct {
	raw    string
	reason MCPTransportErrorReason
}

var _ = DescribeTable("TestParseMCPTransportsRejectsInvalidValues",
	func(tc invalidMCPTransportCase) {
		_, err := ParseMCPTransports(tc.raw)

		Expect(err).To(matchMCPTransportError(tc.reason))
	},
	Entry("unknown", invalidMCPTransportCase{raw: "websocket", reason: MCPTransportErrorReasonInvalid}),
	Entry("none combined", invalidMCPTransportCase{raw: "none,stdio", reason: MCPTransportErrorReasonNoneMixed}),
	Entry("duplicate", invalidMCPTransportCase{raw: "stdio,stdio", reason: MCPTransportErrorReasonDuplicate}),
	Entry("empty part", invalidMCPTransportCase{raw: "stdio,", reason: MCPTransportErrorReasonInvalid}),
)

type validMCPTransportCase struct {
	raw  string
	want MCPTransports
}

var _ = DescribeTable("ParseMCPTransports parser edge coverage",
	func(tc validMCPTransportCase) {
		got, err := ParseMCPTransports(tc.raw)

		Expect(err).NotTo(HaveOccurred())
		Expect(got).To(Equal(tc.want))
	},
	Entry("empty defaults to none", validMCPTransportCase{raw: "", want: MCPTransports{}}),
	Entry("whitespace defaults to none", validMCPTransportCase{raw: " \t\n ", want: MCPTransports{}}),
	Entry("target parsing trims and case-normalizes mixed transports", validMCPTransportCase{raw: " HTTP , StDiO ", want: MCPTransports{HTTP: true, Stdio: true}}),
	Entry("single http", validMCPTransportCase{raw: "http", want: MCPTransports{HTTP: true}}),
	Entry("single stdio", validMCPTransportCase{raw: "stdio", want: MCPTransports{Stdio: true}}),
)

var _ = DescribeTable("ParseMCPTransports rejects parser edge cases",
	func(tc invalidMCPTransportCase) {
		_, err := ParseMCPTransports(tc.raw)

		Expect(err).To(matchMCPTransportError(tc.reason))
	},
	Entry("mixed triple", invalidMCPTransportCase{raw: "http,stdio,none", reason: MCPTransportErrorReasonInvalid}),
	Entry("duplicate with whitespace and case", invalidMCPTransportCase{raw: "stdio, STDIO", reason: MCPTransportErrorReasonDuplicate}),
)
