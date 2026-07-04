package assets

import (
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/types"
	"github.com/perber/wiki/internal/localization"
)

type catalogMessageResolution int

const (
	catalogMessageResolved catalogMessageResolution = iota + 1
	catalogMessageMissing
	catalogMessageFailed
)

var _ = ginkgo.Describe("asset i18n", ginkgo.Label("unit"), func() {
	ginkgo.It("keeps the delete success message ID in the localization catalog", func() {
		Expect(localization.English.Render(MessageIDAssetDeleteSuccess, "")).To(resolveCatalogMessage())
	})
})

func resolveCatalogMessage() types.GomegaMatcher {
	ginkgo.GinkgoHelper()

	return WithTransform(classifyCatalogMessageResolution, Equal(catalogMessageResolved))
}

func classifyCatalogMessageResolution(rendered localization.Result) catalogMessageResolution {
	switch {
	case rendered.Err != nil:
		return catalogMessageFailed
	case rendered.Missing:
		return catalogMessageMissing
	default:
		return catalogMessageResolved
	}
}
