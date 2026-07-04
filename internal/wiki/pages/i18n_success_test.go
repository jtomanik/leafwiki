package pages

import (
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	sharederrors "github.com/perber/wiki/internal/core/shared/errors"
)

type apiSuccessMessageCase struct {
	messageID sharederrors.MessageID
}

var _ = ginkgo.DescribeTable("API success message catalog resolution",
	ginkgo.Label("unit"),
	func(tc apiSuccessMessageCase) {
		Expect(tc.messageID).To(ResolveCatalogMessage())
	},
	ginkgo.Entry("delete", apiSuccessMessageCase{messageID: MessageIDAPIPagesDeleteSuccess}),
	ginkgo.Entry("move", apiSuccessMessageCase{messageID: MessageIDAPIPagesMoveSuccess}),
	ginkgo.Entry("sort", apiSuccessMessageCase{messageID: MessageIDAPIPagesSortSuccess}),
)
