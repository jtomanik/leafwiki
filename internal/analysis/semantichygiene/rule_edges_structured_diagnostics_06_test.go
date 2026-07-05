package semantichygiene

import (
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = ginkgo.Describe("semantichygiene structured diagnostics", func() {
	ginkgo.It("reports project daemon control status predicate assertions", ginkgo.Label("unit"), func() {
		h := newRuleHarness("/repo/internal/projectdaemon/server_test.go", "github.com/perber/wiki/internal/projectdaemon", `package projectdaemon

type assertion struct{}
func Expect(actual any) assertion { return assertion{} }
func (assertion) To(matcher any, extra ...any) {}
func BeTrue() any { return nil }

func IsControlStatus(err error, status int) bool { return true }

func TestControlStatus(err error) {
	Expect(IsControlStatus(err, 409)).To(BeTrue())
}
`)
		for _, call := range h.findCalls("To") {
			checkGomegaSemanticMatcher(h.ctx, call)
		}

		Expect(h.diagnosticMessages()).To(ConsistOf(
			"semh:gomega.control-status-matcher: assert project daemon control errors with MatchError or a domain matcher instead of IsControlStatus with boolean matchers",
		))
	})
})
