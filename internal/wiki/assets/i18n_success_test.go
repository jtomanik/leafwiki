package assets

import (
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/gstruct"
	"github.com/perber/wiki/internal/localization"
)

var _ = ginkgo.Describe("asset i18n", func() {
	ginkgo.It("renders delete success messages from the localization catalog", func() {
		rendered := localization.English.Render(MessageIDAssetDeleteSuccess, "")
		Expect(rendered).To(gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
			"Missing": BeFalse(),
			"Err":     Not(HaveOccurred()),
		}))
		Expect(apiSuccessMessage(MessageIDAssetDeleteSuccess)).To(Equal(rendered.Message))
	})
})
