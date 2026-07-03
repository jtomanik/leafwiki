package tree

import (
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = ginkgo.DescribeTable("markdown paths map to wiki route paths",
	func(path string, want string) {
		Expect(MarkdownPathToRoutePath(path)).To(Equal(want))
	},
	ginkgo.Entry("root index", "index.md", ""),
	ginkgo.Entry("root uppercase index", "INDEX.MD", ""),
	ginkgo.Entry("section index", "docs/index.md", "docs"),
	ginkgo.Entry("section uppercase index", "docs/INDEX.MD", "docs"),
	ginkgo.Entry("regular page", "docs/guide.md", "docs/guide"),
	ginkgo.Entry("leading slash", "/docs/guide.md", "docs/guide"),
	ginkgo.Entry("already route-like", "docs/guide", "docs/guide"),
)
