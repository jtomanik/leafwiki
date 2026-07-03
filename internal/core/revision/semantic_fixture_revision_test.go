package revision

import (
	"errors"
	"os"
	"reflect"

	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/gcustom"
	"github.com/onsi/gomega/types"
	sharederrors "github.com/perber/wiki/internal/core/shared/errors"
	"github.com/perber/wiki/internal/core/tree"
)

func newFixturePageID[T ~string](raw T) tree.PageID {
	return tree.NewPageIDUnchecked(raw)
}

func newFixtureRevisionID[T ~string](raw T) tree.RevisionID {
	return tree.NewRevisionIDUnchecked(string(raw))
}

func newFixtureSlug[T ~string](raw T) tree.Slug {
	return tree.NewSlugUnchecked(raw)
}

func newFixtureUserID[T ~string](raw T) tree.UserID {
	return tree.NewUserIDUnchecked(string(raw))
}

func revisionTempDir() string {
	ginkgo.GinkgoHelper()

	dir, err := os.MkdirTemp("", "leafwiki-revision-*")
	Expect(err).To(Succeed())
	ginkgo.DeferCleanup(os.RemoveAll, dir)
	return dir
}

func haveCanonicalRevisionRawStorage() types.GomegaMatcher {
	return SatisfyAll(
		HavePrefix("<!-- leafwiki\n"),
		Not(HavePrefix("---\n")),
	)
}

func matchLocalizedRevisionErrorDetails(code sharederrors.ErrorCode, args ...string) types.GomegaMatcher {
	return gcustom.MakeMatcher(func(err error) (bool, error) {
		localized, ok := sharederrors.AsLocalizedError(err)
		return ok &&
			localized.Code == code &&
			localized.MessageID == sharederrors.MessageIDForCode(code) &&
			reflect.DeepEqual(localized.Args, args), nil
	}).WithTemplate("Expected:\n{{.FormattedActual}}\n{{.To}} match localized revision error details\n{{format .Data 1}}", code)
}

func matchRevisionErrorCause(want error) types.GomegaMatcher {
	return gcustom.MakeMatcher(func(err error) (bool, error) {
		return errors.Is(err, want), nil
	}).WithTemplate("Expected:\n{{.FormattedActual}}\n{{.To}} wrap revision error\n{{format .Data 1}}", want)
}

func matchRevisionIntegrityIssue(code sharederrors.ErrorCode) types.GomegaMatcher {
	return gcustom.MakeMatcher(func(issue RevisionIntegrityIssue) (bool, error) {
		return issue.Code == code && issue.MessageID == sharederrors.MessageIDForCode(code), nil
	}).WithTemplate("Expected:\n{{.FormattedActual}}\n{{.To}} match revision integrity issue\n{{format .Data 1}}", code)
}
