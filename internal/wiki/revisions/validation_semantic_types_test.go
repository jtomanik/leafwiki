package revisions

import (
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/perber/wiki/internal/core/revision"
	"github.com/perber/wiki/internal/core/tree"
)

var _ = ginkgo.Describe("revision validation", ginkgo.Label("unit"), func() {
	ginkgo.It("returns typed revision lookup and asset values from normalized inputs", func() {
		pageID, revisionID, err := ValidateRevisionLookupInput(" page-1 ", " rev-1 ")
		Expect(err).NotTo(HaveOccurred())

		var semanticPageID tree.PageID = pageID
		var semanticRevisionID revision.RevisionID = revisionID
		Expect(semanticPageID).To(Equal(newFixturePageID("page-1")))
		Expect(semanticRevisionID).To(Equal(newFixtureRevisionID("rev-1")))

		assetPageID, assetRevisionID, assetName, err := ValidateRevisionAssetInput("page-1", "rev-1", "/asset.png")
		Expect(err).NotTo(HaveOccurred())
		var semanticAssetPageID tree.PageID = assetPageID
		var semanticAssetRevisionID revision.RevisionID = assetRevisionID
		var semanticAssetName tree.AssetName = assetName
		Expect(semanticAssetPageID).To(Equal(newFixturePageID("page-1")))
		Expect(semanticAssetRevisionID).To(Equal(newFixtureRevisionID("rev-1")))
		Expect(semanticAssetName).To(Equal(newFixtureAssetName("asset.png")))
	})

	ginkgo.It("ValidateRevisionCompareInput returns semantic values and trims IDs", func() {
		pageID, baseID, targetID, err := ValidateRevisionCompareInput(" page-1 ", " base-rev ", " target-rev ")
		Expect(err).NotTo(HaveOccurred())
		Expect(pageID).To(Equal(newFixturePageID("page-1")))
		Expect(baseID).To(Equal(newFixtureRevisionID("base-rev")))
		Expect(targetID).To(Equal(newFixtureRevisionID("target-rev")))
	})

	ginkgo.It("ValidateRevisionCompare reports invalid page and compare request errors", func() {
		_, _, _, err := ValidateRevisionCompare(newFixturePageID(""), "base", "target")
		Expect(err).To(MatchRevisionErrorCode(ErrCodeRevisionInvalidPageID))

		_, _, _, err = ValidateRevisionCompare(newFixturePageID("page-1"), "", "target")
		Expect(err).To(MatchRevisionErrorCode(ErrCodeRevisionCompareInvalidRequest))

		_, _, _, err = ValidateRevisionCompare(newFixturePageID("page-1"), "base", "")
		Expect(err).To(MatchRevisionErrorCode(ErrCodeRevisionCompareInvalidRequest))
	})

	ginkgo.It("ValidateRevisionLookup and ValidateRevisionAsset propagate invalid lookup errors", func() {
		_, _, err := ValidateRevisionLookup(newFixturePageID(""), "rev-1")
		Expect(err).To(MatchRevisionErrorCode(ErrCodeRevisionInvalidPageID))

		_, _, _, err = ValidateRevisionAsset(newFixturePageID(""), "rev-1", "asset.png")
		Expect(err).To(MatchRevisionErrorCode(ErrCodeRevisionInvalidPageID))
	})
})
