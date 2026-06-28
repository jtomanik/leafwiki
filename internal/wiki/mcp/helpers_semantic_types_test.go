package mcp

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/perber/wiki/internal/core/tree"
)

var _ = Describe("Semantic helper types", func() {
	It("returns a semantic page ID from exactlyOneIDOrPageID", func() {
		pageID, err := exactlyOneIDOrPageID("page-1", "")
		Expect(err).NotTo(HaveOccurred())

		var semanticPageID tree.PageID = pageID
		Expect(semanticPageID).To(Equal(newFixturePageID("page-1")))
	})
})
