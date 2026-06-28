package assets

import (
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = ginkgo.Describe("asset i18n", func() {
	ginkgo.It("TestAssetDeleteSuccessMessageRendersFromCatalog", func() {
		Expect(apiSuccessMessage(MessageIDAssetDeleteSuccess)).To(Equal("Asset deleted"))
	})
})
