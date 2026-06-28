package runtimeconfig

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("MCP transport parsing", func() {
	It("TestParseMCPTransports", func() {
		got, err := ParseMCPTransports("http,stdio")

		Expect(err).NotTo(HaveOccurred())
		Expect(got.HTTP).To(BeTrue())
		Expect(got.Stdio).To(BeTrue())
	})
})

type invalidMCPTransportCase struct {
	raw       string
	wantError string
}

var _ = DescribeTable("TestParseMCPTransportsRejectsInvalidValues",
	func(tc invalidMCPTransportCase) {
		_, err := ParseMCPTransports(tc.raw)

		Expect(err).To(MatchError(ContainSubstring(tc.wantError)))
	},
	Entry("unknown", invalidMCPTransportCase{raw: "websocket", wantError: "invalid MCP transport"}),
	Entry("none combined", invalidMCPTransportCase{raw: "none,stdio", wantError: "none cannot be combined"}),
	Entry("duplicate", invalidMCPTransportCase{raw: "stdio,stdio", wantError: "duplicate MCP transport"}),
	Entry("empty part", invalidMCPTransportCase{raw: "stdio,", wantError: "invalid MCP transport"}),
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

		Expect(err).To(MatchError(ContainSubstring(tc.wantError)))
	},
	Entry("mixed triple", invalidMCPTransportCase{raw: "http,stdio,none", wantError: "invalid MCP transport"}),
	Entry("duplicate with whitespace and case", invalidMCPTransportCase{raw: "stdio, STDIO", wantError: "duplicate MCP transport"}),
)
