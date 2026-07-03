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
	It("enables HTTP and stdio transports from a comma-separated runtime setting", func() {
		got, err := ParseMCPTransports("http,stdio")

		Expect(err).NotTo(HaveOccurred())
		Expect(got).To(matchMCPTransports(true, true))
	})
})

type invalidMCPTransportCase struct {
	raw    string
	reason MCPTransportErrorReason
}

var _ = DescribeTable("MCP transport validation returns stable reasons for invalid settings",
	func(tc invalidMCPTransportCase) {
		_, err := ParseMCPTransports(tc.raw)

		Expect(err).To(matchMCPTransportError(tc.reason))
	},
	Entry("rejects unknown transport names", invalidMCPTransportCase{raw: "websocket", reason: MCPTransportErrorReasonInvalid}),
	Entry("rejects none mixed with active transports", invalidMCPTransportCase{raw: "none,stdio", reason: MCPTransportErrorReasonNoneMixed}),
	Entry("rejects duplicate transport names", invalidMCPTransportCase{raw: "stdio,stdio", reason: MCPTransportErrorReasonDuplicate}),
	Entry("rejects empty transport entries", invalidMCPTransportCase{raw: "stdio,", reason: MCPTransportErrorReasonInvalid}),
)

type validMCPTransportCase struct {
	raw  string
	want MCPTransports
}

var _ = DescribeTable("MCP transport parsing accepts empty, single, and mixed runtime settings",
	func(tc validMCPTransportCase) {
		got, err := ParseMCPTransports(tc.raw)

		Expect(err).NotTo(HaveOccurred())
		Expect(got).To(Equal(tc.want))
	},
	Entry("empty defaults to none", validMCPTransportCase{raw: "", want: MCPTransports{}}),
	Entry("whitespace defaults to none", validMCPTransportCase{raw: " \t\n ", want: MCPTransports{}}),
	Entry("trims and case-normalizes mixed transports", validMCPTransportCase{raw: " HTTP , StDiO ", want: MCPTransports{HTTP: true, Stdio: true}}),
	Entry("enables only HTTP", validMCPTransportCase{raw: "http", want: MCPTransports{HTTP: true}}),
	Entry("enables only stdio", validMCPTransportCase{raw: "stdio", want: MCPTransports{Stdio: true}}),
)

var _ = DescribeTable("MCP transport parsing rejects ambiguous runtime settings",
	func(tc invalidMCPTransportCase) {
		_, err := ParseMCPTransports(tc.raw)

		Expect(err).To(matchMCPTransportError(tc.reason))
	},
	Entry("rejects none after active transports", invalidMCPTransportCase{raw: "http,stdio,none", reason: MCPTransportErrorReasonInvalid}),
	Entry("rejects duplicates after trimming and case normalization", invalidMCPTransportCase{raw: "stdio, STDIO", reason: MCPTransportErrorReasonDuplicate}),
)
