package taxonomycases

import ginkgo "github.com/onsi/ginkgo/v2"

var dynamicTaxonomyLabel = "unit"

var _ = ginkgo.Describe("taxonomy labels", ginkgo.Label("unit"), func() {
	ginkgo.It("accepts a unit label inherited from a container", func() {})
})

var _ = ginkgo.Describe("integration taxonomy labels", func() {
	ginkgo.It("accepts an integration label on a spec", ginkgo.Label("integration"), func() {})
})

var _ = ginkgo.Describe("e2e taxonomy labels", func() {
	ginkgo.It("accepts an e2e label on a spec", ginkgo.Label("e2e"), func() {})
})

var _ = ginkgo.Describe("unlabeled taxonomy rollout", func() {
	ginkgo.It("rejects a spec without a taxonomy label", func() {}) // want "semh:ginkgo.taxonomy-label.missing: Ginkgo spec must have exactly one primary taxonomy label: unit, integration, or e2e"
})

var _ = ginkgo.Describe("unknown taxonomy labels", func() {
	ginkgo.It("rejects an unknown label", ginkgo.Label("slow"), func() {}) // want "semh:ginkgo.taxonomy-label.unknown: Ginkgo label \"slow\" is not allowed by the LeafWiki test taxonomy; use unit, integration, or e2e"
})

var _ = ginkgo.Describe("dynamic taxonomy labels", func() {
	ginkgo.It("rejects a dynamic label", ginkgo.Label(dynamicTaxonomyLabel), func() {}) // want "semh:ginkgo.taxonomy-label.dynamic: Ginkgo taxonomy labels must be static string literals"
})

var _ = ginkgo.Describe("direct taxonomy conflicts", func() {
	ginkgo.It("rejects direct primary label conflicts", ginkgo.Label("unit", "integration"), func() {}) // want "semh:ginkgo.taxonomy-label.multiple: Ginkgo spec has multiple primary taxonomy labels \\(unit, integration\\); use exactly one of unit, integration, or e2e"
})

var _ = ginkgo.Describe("inherited taxonomy conflicts", ginkgo.Label("unit"), func() {
	ginkgo.It("rejects inherited primary label conflicts", ginkgo.Label("integration"), func() {}) // want "semh:ginkgo.taxonomy-label.multiple: Ginkgo spec has multiple primary taxonomy labels \\(unit, integration\\); use exactly one of unit, integration, or e2e"
})

var _ = ginkgo.DescribeTable("table taxonomy conflicts",
	func(value string) {},
	ginkgo.Label("integration"),
	ginkgo.Entry("rejects conflicting entry labels", "value", ginkgo.Label("unit")), // want "semh:ginkgo.taxonomy-label.multiple: Ginkgo spec has multiple primary taxonomy labels \\(integration, unit\\); use exactly one of unit, integration, or e2e"
)

var _ = ginkgo.DescribeTable("table taxonomy labels",
	func(value string) {},
	ginkgo.Label("integration"),
	ginkgo.Entry("accepts inherited table labels", "value"),
)
