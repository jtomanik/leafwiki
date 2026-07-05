package testhygiene

import (
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = ginkgo.Describe("testhygiene fixture-preservation edges", ginkgo.Label("unit"), func() {
	ginkgo.It("reports raw semantic values in Ginkgo table entries", func() {
		h := newRuleHarness("/repo/internal/wiki/page_test.go", "github.com/perber/wiki/internal/wiki", `package wiki

type PageID string
type ErrorCode string
type bddDSL struct{}
var ginkgo bddDSL
func (bddDSL) DescribeTable(text string, body func(PageID, ErrorCode), entries ...any) bool { return true }
func (bddDSL) Entry(text string, args ...any) bool { return true }

var _ = ginkgo.DescribeTable("rejects raw semantic table entry values",
	func(pageID PageID, code ErrorCode) {},
	ginkgo.Entry("raw semantic row", "page-5", "unknown"),
)
`)

		checkGinkgoSpecQualityCall(h.ctx, h.findCall("Entry"))

		Expect(h.diagnosticMessages()).To(ConsistOf(
			"semh:ginkgo.semantic-entry-data: Ginkgo Entry data for pageID uses raw string for PageID; use a semantic fixture value or row struct",
			"semh:ginkgo.semantic-entry-data: Ginkgo Entry data for code uses raw string for ErrorCode; use a semantic fixture value or row struct",
		))
	})

	ginkgo.It("reports raw process status-code assertions", func() {
		h := newRuleHarness("/repo/internal/cli/exit_status_test.go", "github.com/perber/wiki/internal/cli", `package cli

type assertion struct{}
func Expect(actual any) assertion { return assertion{} }
func (assertion) To(matcher any, extra ...any) {}
func Equal(expected any) any { return nil }

func TestExitStatus() {
	exitCode := 1
	Expect(exitCode).To(Equal(1))
}
`)

		checkGomegaSemanticMatcher(h.ctx, h.findCall("To"))

		Expect(h.diagnosticMessages()).To(ConsistOf(
			"semh:gomega.raw-status-code: assert process/domain status with a semantic matcher or named status value instead of raw numeric status codes",
		))
	})
})
