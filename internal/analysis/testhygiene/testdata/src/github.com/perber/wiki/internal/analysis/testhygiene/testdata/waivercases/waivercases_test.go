package waivercases

import ginkgo "github.com/onsi/ginkgo/v2"

type assertion struct{}

func Expect(actual any) assertion { return assertion{} }

func (assertion) To(matcher any, annotations ...any) {}

func Equal(want any) any { return nil }

// semh:allow ginkgo.top-level-it -- stale waiver should fail when the spec disappears // want "semh:waiver.stale: semh waiver for ginkgo.top-level-it did not match any diagnostic"
func ordinaryHelper() {}

// semh:allow ginkgo.top-level-it -- first duplicate package-level invariant // want "semh:waiver.duplicate: duplicate semh waiver for ginkgo.top-level-it; one waiver can suppress one diagnostic"
// semh:allow ginkgo.top-level-it -- second duplicate package-level invariant
var _ = ginkgo.It("documents duplicate waiver behavior", ginkgo.Label("unit"), func() {})

// semh:allow semantic.direct-cast -- hard semantic rules must stay non-waivable // want "semh:waiver.non-waivable-rule: semh waiver for semantic.direct-cast cannot suppress hard diagnostics"
func hardProjectRuleWaiver() {}

// semh:allow // want "semh:waiver.malformed: semh:allow waiver must name exactly one rule ID before --"
func malformedWaiver() {}

// semh:allow ginkgo.made-up -- unknown waiver rule should fail // want "semh:waiver.unknown-rule: unknown semh waiver rule ginkgo.made-up"
func unknownWaiverRule() {}

func oneWaiverSuppressesOneDiagnostic() {
	// semh:allow gomega.equal-zero -- first zero assertion is clearer in compatibility form
	Expect(0).To(Equal(0))
	Expect(0).To(Equal(0)) // want "semh:gomega.equal-zero: use BeZero matcher instead of Equal\\(0\\) for zero-value assertions"
}

var _ = ginkgo.Describe("waiver containment", ginkgo.Label("unit"), func() {
	// semh:allow gomega.equal-zero -- the spec node must not waive assertions in its body // want "semh:waiver.stale: semh waiver for gomega.equal-zero did not match any diagnostic"
	ginkgo.It("documents scoped waiver containment", func() {
		Expect(0).To(Equal(0)) // want "semh:gomega.equal-zero: use BeZero matcher instead of Equal\\(0\\) for zero-value assertions"
	})
})
