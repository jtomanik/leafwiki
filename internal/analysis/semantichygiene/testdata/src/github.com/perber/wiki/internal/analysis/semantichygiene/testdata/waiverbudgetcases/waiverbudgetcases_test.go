package waiverbudgetcases

import ginkgo "github.com/onsi/ginkgo/v2"

type assertion struct{}

func Expect(actual any) assertion { return assertion{} }

func (assertion) To(matcher any, annotations ...any) {}

func Equal(want any) any { return nil }

// semh:allow ginkgo.top-level-it -- first package-level invariant
var _ = ginkgo.It("documents first package invariant", ginkgo.Label("unit"), func() {})

// semh:allow ginkgo.top-level-it -- second package-level invariant
var _ = ginkgo.It("documents second package invariant", ginkgo.Label("unit"), func() {})

// semh:allow ginkgo.top-level-it -- third package-level invariant
var _ = ginkgo.It("documents third package invariant", ginkgo.Label("unit"), func() {})

var _ = ginkgo.DescribeTable("wide entry budget cases",
	ginkgo.Label("unit"),
	func(name string, count int, enabled bool, code string, message string) {},
	// semh:allow ginkgo.wide-entry -- first wide entry exception
	ginkgo.Entry("wide row 1", "name", 1, true, "code", "message"),
	// semh:allow ginkgo.wide-entry -- second wide entry exception
	ginkgo.Entry("wide row 2", "name", 1, true, "code", "message"),
	// semh:allow ginkgo.wide-entry -- third wide entry exception
	ginkgo.Entry("wide row 3", "name", 1, true, "code", "message"),
)

func equalMatcherBudgetCases() {
	// semh:allow gomega.equal-empty -- first empty matcher exception
	Expect([]string{}).To(Equal([]string{}))
	// semh:allow gomega.equal-empty -- second empty matcher exception
	Expect([]string{}).To(Equal([]string{}))
	// semh:allow gomega.equal-empty -- third empty matcher exception
	Expect([]string{}).To(Equal([]string{}))
	// semh:allow gomega.equal-zero -- first zero matcher exception
	Expect(0).To(Equal(0))
	// semh:allow gomega.equal-zero -- second zero matcher exception // want "semh:waiver.budget-exceeded: total waiver budget exceeded: used 11, budget 10"
	Expect(0).To(Equal(0))
}
