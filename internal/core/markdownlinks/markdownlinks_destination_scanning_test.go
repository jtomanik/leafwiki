package markdownlinks

import (
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = ginkgo.Describe("markdown link destination scanning", ginkgo.Label("unit"), func() {
	ginkgo.It("skips image destinations by default and can include them when requested", func() {
		content := "![Logo](/assets/logo.png) [Page](/docs/page)"

		defaultDestinations := ScanInlineDestinations(content, InlineScanOptions{})
		withImages := ScanInlineDestinations(content, InlineScanOptions{IncludeImages: true})

		Expect(defaultDestinations).To(ConsistOf(matchInlineDestination("/docs/page", false)))
		Expect(withImages).To(ConsistOf(
			matchInlineDestination("/assets/logo.png", true),
			matchInlineDestination("/docs/page", false),
		))
	})

	ginkgo.It("skips code-span destinations by default and can include them when requested", func() {
		content := "`[Code](/docs/code)` [Page](/docs/page)"

		defaultDestinations := ScanInlineDestinations(content, InlineScanOptions{})
		withCode := ScanInlineDestinations(content, InlineScanOptions{IgnoreCodeRanges: true})

		Expect(defaultDestinations).To(ConsistOf(matchInlineDestination("/docs/page", false)))
		Expect(withCode).To(ConsistOf(
			matchInlineDestination("/docs/code", false),
			matchInlineDestination("/docs/page", false),
		))
	})
})
