package markdownvalidation

import (
	"os"

	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/gcustom"
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
	return gcustom.MakeMatcher(func(issue Issue) (bool, error) {
		return issue.Code == code && issue.MessageID == code.MessageID(), nil
	}).WithTemplate("Expected:\n{{.FormattedActual}}\n{{.To}} match markdown validation issue code\n{{format .Data 1}}", code)
}

func matchValidationIssueAtPath(code IssueCode, path string) types.GomegaMatcher {
	return gcustom.MakeMatcher(func(issue Issue) (bool, error) {
		return issue.Code == code &&
			issue.MessageID == code.MessageID() &&
			issuePathString(issue) == path, nil
	}).WithTemplate("Expected:\n{{.FormattedActual}}\n{{.To}} match markdown validation issue at path\n{{format .Data 1}}", code)
}

func matchDuplicatePageIDIssue() types.GomegaMatcher {
	return gcustom.MakeMatcher(func(issue Issue) (bool, error) {
		return issue.Code == IssueCodeDuplicateLeafwikiID &&
			issue.MessageID == MessageIDDuplicateLeafwikiID, nil
	}).WithMessage("match duplicate page ID issue")
}

func matchNormalizedRouteConflictIssue(paths ...string) types.GomegaMatcher {
	return gcustom.MakeMatcher(func(issue Issue) (bool, error) {
		if issue.Code != IssueCodePathConflict || issue.MessageID != MessageIDPathConflict {
			return false, nil
		}
		path := issuePathString(issue)
		for _, candidate := range paths {
			if path == candidate {
				return true, nil
			}
		}
		return false, nil
	}).WithTemplate("Expected:\n{{.FormattedActual}}\n{{.To}} match normalized route conflict issue for paths\n{{format .Data 1}}", paths)
}
