package pages_test

import (
	"errors"
	"os"

	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/gcustom"
	"github.com/onsi/gomega/types"

	sharederrors "github.com/perber/wiki/internal/core/shared/errors"
	"github.com/perber/wiki/internal/core/tree"
	"github.com/perber/wiki/internal/links"
	testmatchers "github.com/perber/wiki/internal/test_utils/matchers"
	"github.com/perber/wiki/internal/wiki/pagesave"
)

func newFixturePageID[T ~string](raw T) tree.PageID {
	return tree.NewPageIDUnchecked(raw)
}

func newFixturePageVersion[T ~string](raw T) tree.PageVersion {
	return tree.NewPageVersionUnchecked(raw)
}

func newFixtureSlug[T ~string](raw T) tree.Slug {
	return tree.NewSlugUnchecked(raw)
}

func newFixtureUserID[T ~string](raw T) tree.UserID {
	return tree.NewUserIDUnchecked(string(raw))
}

func pagesTestTempDir() string {
	ginkgo.GinkgoHelper()

	dir, err := os.MkdirTemp("", "leafwiki-pages-test-*")
	Expect(err).To(Succeed())
	ginkgo.DeferCleanup(os.RemoveAll, dir)
	return dir
}

func HavePageValidationFieldError(field testmatchers.ValidationField, code sharederrors.FieldErrorCode, messageID sharederrors.MessageID) types.GomegaMatcher {
	return WithTransform(func(err error) *sharederrors.ValidationErrors {
		var validation *sharederrors.ValidationErrors
		if !errors.As(err, &validation) {
			return nil
		}
		return validation
	}, testmatchers.ContainFieldError(field, code, messageID))
}

func MatchPageLocalizedCode(code sharederrors.ErrorCode) types.GomegaMatcher {
	return testmatchers.MatchLocalizedError(code, sharederrors.MessageIDForCode(code))
}

func HaveBrokenOutgoing(path tree.RoutePath) types.GomegaMatcher {
	return gcustom.MakeMatcher(func(outgoing links.OutgoingResultItem) (bool, error) {
		return outgoing.ToPath == path &&
			outgoing.Broken &&
			outgoing.ToPageID == "", nil
	}).WithMessage("describe a broken outgoing link")
}

func HaveHealthyOutgoing(path tree.RoutePath, targetID tree.PageID) types.GomegaMatcher {
	return gcustom.MakeMatcher(func(outgoing links.OutgoingResultItem) (bool, error) {
		return outgoing.ToPath == path &&
			!outgoing.Broken &&
			outgoing.ToPageID == targetID, nil
	}).WithMessage("describe a healthy outgoing link")
}

func HaveHealthyOutgoingWithAnyTarget(path tree.RoutePath) types.GomegaMatcher {
	return gcustom.MakeMatcher(func(outgoing links.OutgoingResultItem) (bool, error) {
		return outgoing.ToPath == path &&
			!outgoing.Broken &&
			outgoing.ToPageID != "", nil
	}).WithMessage("describe a healthy outgoing link with any target")
}

func HavePageSaveContentChange() types.GomegaMatcher {
	return gcustom.MakeMatcher(func(event pagesave.PageSaveEvent) (bool, error) {
		return event.ContentChanged, nil
	}).WithMessage("record a page-save content change")
}
