package pages

import (
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	sharederrors "github.com/perber/wiki/internal/core/shared/errors"
)

type apiSuccessMessageCase struct {
	messageID sharederrors.MessageID
	want      string
}

var _ = ginkgo.DescribeTable("API success message catalog rendering",
	func(tc apiSuccessMessageCase) {
		Expect(apiSuccessMessage(tc.messageID)).To(Equal(tc.want))
	},
	ginkgo.Entry("delete", apiSuccessMessageCase{messageID: MessageIDAPIPagesDeleteSuccess, want: "Page deleted"}),
	ginkgo.Entry("move", apiSuccessMessageCase{messageID: MessageIDAPIPagesMoveSuccess, want: "Page moved"}),
	ginkgo.Entry("sort", apiSuccessMessageCase{messageID: MessageIDAPIPagesSortSuccess, want: "Pages sorted successfully"}),
)
