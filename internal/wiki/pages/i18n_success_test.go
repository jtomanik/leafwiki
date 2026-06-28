package pages

import (
	ginkgo "github.com/onsi/ginkgo/v2"
	sharederrors "github.com/perber/wiki/internal/core/shared/errors"
)

type apiSuccessMessageCase struct {
	messageID sharederrors.MessageID
	want      string
}

var _ = ginkgo.DescribeTable("TestAPISuccessMessagesRenderFromCatalog",
	func(tc apiSuccessMessageCase) {
		t := ginkgo.GinkgoT()
		if got := apiSuccessMessage(tc.messageID); got != tc.want {
			t.Fatalf("apiSuccessMessage(%q) = %q, want %q", tc.messageID, got, tc.want)
		}
	},
	ginkgo.Entry("delete", apiSuccessMessageCase{messageID: MessageIDAPIPagesDeleteSuccess, want: "Page deleted"}),
	ginkgo.Entry("move", apiSuccessMessageCase{messageID: MessageIDAPIPagesMoveSuccess, want: "Page moved"}),
	ginkgo.Entry("sort", apiSuccessMessageCase{messageID: MessageIDAPIPagesSortSuccess, want: "Pages sorted successfully"}),
)
