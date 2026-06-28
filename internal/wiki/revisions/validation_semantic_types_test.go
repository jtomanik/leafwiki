package revisions

import (
	"errors"

	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/perber/wiki/internal/core/revision"
	sharederrors "github.com/perber/wiki/internal/core/shared/errors"
	"github.com/perber/wiki/internal/core/tree"
)

var _ = ginkgo.Describe("revision validation", func() {
	ginkgo.It("TestValidateRevisionInputsReturnSemanticValues", func() {
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
		Expect(semanticAssetName).To(Equal(tree.AssetName("asset.png")))
	})

	ginkgo.It("ValidateRevisionCompareInput returns semantic values and trims IDs", func() {
		pageID, baseID, targetID, err := ValidateRevisionCompareInput(" page-1 ", " base-rev ", " target-rev ")
		Expect(err).NotTo(HaveOccurred())
		Expect(pageID).To(Equal(newFixturePageID("page-1")))
		Expect(baseID).To(Equal(newFixtureRevisionID("base-rev")))
		Expect(targetID).To(Equal(newFixtureRevisionID("target-rev")))
	})

	ginkgo.It("ValidateRevisionCompare reports invalid page and compare request errors", func() {
		_, _, _, err := ValidateRevisionCompare("", "base", "target")
		expectRevisionErrorCode(err, ErrCodeRevisionInvalidPageID)

		_, _, _, err = ValidateRevisionCompare(newFixturePageID("page-1"), "", "target")
		expectRevisionErrorCode(err, ErrCodeRevisionCompareInvalidRequest)

		_, _, _, err = ValidateRevisionCompare(newFixturePageID("page-1"), "base", "")
		expectRevisionErrorCode(err, ErrCodeRevisionCompareInvalidRequest)
	})

	ginkgo.It("ValidateRevisionLookup and ValidateRevisionAsset propagate invalid lookup errors", func() {
		_, _, err := ValidateRevisionLookup("", "rev-1")
		expectRevisionErrorCode(err, ErrCodeRevisionInvalidPageID)

		_, _, _, err = ValidateRevisionAsset("", "rev-1", "asset.png")
		expectRevisionErrorCode(err, ErrCodeRevisionInvalidPageID)
	})
})

func expectRevisionErrorCode(err error, code sharederrors.ErrorCode) {
	ginkgo.GinkgoHelper()
	var localized *sharederrors.LocalizedError
	Expect(errors.As(err, &localized)).To(BeTrue(), "error = %T %v", err, err)
	Expect(localized.Code).To(Equal(code))
}
