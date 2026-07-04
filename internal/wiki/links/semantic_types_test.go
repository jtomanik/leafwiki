package links

import (
	ginkgo "github.com/onsi/ginkgo/v2"

	"github.com/perber/wiki/internal/core/tree"
)

var _ = ginkgo.Describe("link semantic types", ginkgo.Label("unit"), func() {
	ginkgo.It("keeps link use case inputs typed as semantic page IDs", func() {
		status := GetLinkStatusInput{PageID: newFixturePageID("page-1")}
		var _ tree.PageID = status.PageID

		backlinks := GetBacklinksInput{PageID: newFixturePageID("page-1")}
		var _ tree.PageID = backlinks.PageID

		outgoing := GetOutgoingLinksInput{PageID: newFixturePageID("page-1")}
		var _ tree.PageID = outgoing.PageID
	})
})
