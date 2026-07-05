package ginkgocases

import (
	"context"

	ginkgo "github.com/onsi/ginkgo/v2"
)

type PageID string
type ErrorCode string
type treeTestT interface {
	Helper()
}
type ginkgoTreeT struct{}

func (ginkgoTreeT) Helper() {}
func treeSpecT() treeTestT  { return ginkgoTreeT{} }

var _ = ginkgo.It("documents a package invariant without a container", ginkgo.Label("unit"), func() {}) // want "semh:ginkgo.top-level-it: top-level It reads like a migrated unit test; place it under a behavior container or waive with a specific reason"

var _ = ginkgo.Describe("project daemon deterministic edges", func() { // want "semh:ginkgo.coverage-name: Ginkgo node name \"project daemon deterministic edges\" reads like a coverage bucket; describe observable behavior instead"
	ginkgo.It("canonicalizes missing project paths and redacts config mismatch secrets", ginkgo.Label("integration"), func() {})
})

var _ = ginkgo.Describe("Ginkgo policy", ginkgo.Label("unit"), func() {
	ginkgo.FIt("does not commit focused specs", func() {})                         // want "semh:ginkgo.focus: do not commit focused Ginkgo specs; remove Focus/F-prefixed node"
	ginkgo.It("does not commit pending decorators", ginkgo.Pending, func() {})     // want "semh:ginkgo.pending: do not commit pending Ginkgo specs; finish or delete the spec instead"
	ginkgo.It("does not commit flake retries", ginkgo.FlakeAttempts(2), func() {}) // want "semh:ginkgo.flake-attempts: do not commit Ginkgo flake retries; fix the flake or quarantine it outside the suite"
	ginkgo.It("does not commit serial escapes", ginkgo.Serial, func() {})          // want "semh:ginkgo.restricted-decorator: avoid Ginkgo Serial decorator unless the test suite policy explicitly allows it"

	var rowFromSetup string
	ginkgo.BeforeEach(func() {
		rowFromSetup = "setup"
	})
	ginkgo.DescribeTable("uses row structs for wide entries",
		func(name string, count int, enabled bool, code string, message string) {},
		ginkgo.Entry("wide row", "name", 1, true, "code", "message"), // want "semh:ginkgo.wide-entry: use a row struct for Ginkgo table entries with many parameters"
	)
	ginkgo.DescribeTable("does not capture setup values in entries",
		func(row string) {},
		ginkgo.Entry("setup row", rowFromSetup), // want "semh:ginkgo.entry-setup-value: Ginkgo Entry arguments are evaluated at construction time; pass stable row data instead of setup-initialized variables"
	)
	ginkgo.DescribeTable("rejects raw semantic table entry values",
		func(pageID PageID, code ErrorCode) {},
		ginkgo.Entry("raw semantic row", "page-5", "unknown"), // want "semh:ginkgo.semantic-entry-data: Ginkgo Entry data for pageID uses raw string for PageID; use a semantic fixture value or row struct" "semh:ginkgo.semantic-entry-data: Ginkgo Entry data for code uses raw string for ErrorCode; use a semantic fixture value or row struct"
	)

	ginkgo.It("uses context-aware async assertions", func(ctx context.Context) {
		Eventually(func() error { return nil }).Should(Succeed()) // want "semh:gomega.async-context: propagate the spec context into Eventually/Consistently with WithContext or positional context"
	})

	ginkgo.It("rejects local testing adapter helper calls in specs", func() {
		t := treeSpecT()
		t.Helper() // want "semh:ginkgo.testing-t-in-spec: avoid testing.T-like.Helper adapter inside Ginkgo specs; use Gomega expectations and Ginkgo helpers"
	})
})

type assertion struct{}
type asyncAssertion struct{}

func Eventually(actual any, args ...any) asyncAssertion       { return asyncAssertion{} }
func Succeed() any                                            { return nil }
func (asyncAssertion) Should(matcher any, annotations ...any) {}
