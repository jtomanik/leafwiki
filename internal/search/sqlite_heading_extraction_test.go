package search

import (
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = ginkgo.Describe("SQLite search heading extraction", ginkgo.Label("unit"), func() {
	ginkgo.It("keeps linked heading text without indexing link destinations", func() {
		got := extractHeadings("## [Reference Guide](docs/reference.md)")

		Expect(got).To(Equal("Reference Guide"))
	})
})
