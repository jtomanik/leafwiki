package markdownvalidation

import (
	"os"

	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/gstruct"
	"github.com/onsi/gomega/types"
	"github.com/perber/wiki/internal/core/tree"
)

func newFixturePageID[T ~string](raw T) tree.PageID {
	return tree.NewPageIDUnchecked(raw)
}

func markdownValidationTempDir() string {
	ginkgo.GinkgoHelper()

	dir, err := os.MkdirTemp("", "leafwiki-markdownvalidation-*")
	Expect(err).To(Succeed())
	ginkgo.DeferCleanup(os.RemoveAll, dir)
	return dir
}

func matchValidationIssueCode(code IssueCode) types.GomegaMatcher {
	return gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
		"Code":      Equal(code),
		"MessageID": Equal(code.MessageID()),
	})
}

func matchValidationIssueAtPath(code IssueCode, path string) types.GomegaMatcher {
	return SatisfyAll(
		matchValidationIssueCode(code),
		WithTransform(issuePathString, Equal(path)),
	)
}

func matchDuplicatePageIDIssue() types.GomegaMatcher {
	return gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
		"Code":      Equal(IssueCodeDuplicateLeafwikiID),
		"MessageID": Equal(MessageIDDuplicateLeafwikiID),
	})
}

func matchNormalizedRouteConflictIssue(paths ...string) types.GomegaMatcher {
	return SatisfyAll(
		gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
			"Code":      Equal(IssueCodePathConflict),
			"MessageID": Equal(MessageIDPathConflict),
		}),
		WithTransform(issuePathString, BeElementOf(paths)),
	)
}
