package assets

import (
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/gcustom"
	"github.com/onsi/gomega/types"
	"github.com/perber/wiki/internal/localization"
)

var _ = ginkgo.Describe("asset i18n", func() {
	ginkgo.It("keeps the delete success message ID in the localization catalog", func() {
		Expect(localization.English.Render(MessageIDAssetDeleteSuccess, "")).To(resolveCatalogMessage())
	})
})

func resolveCatalogMessage() types.GomegaMatcher {
	ginkgo.GinkgoHelper()

	return gcustom.MakeMatcher(func(rendered localization.Result) (bool, error) {
		return !rendered.Missing && rendered.Err == nil, nil
	}).WithMessage("resolve a catalog message")
}
