package revision

import (
	"errors"
	"os"
	"reflect"

	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/gcustom"
	"github.com/onsi/gomega/gstruct"
	"github.com/onsi/gomega/types"
	"github.com/perber/wiki/internal/core/markdown"
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

func matchRevisionError(want error) types.GomegaMatcher {
	return gcustom.MakeMatcher(func(err error) (bool, error) {
		return errors.Is(err, want), nil
	}).WithTemplate("Expected:\n{{.FormattedActual}}\n{{.To}} match revision error\n{{format .Data 1}}", want)
}

func rejectRevisionValidation() types.GomegaMatcher {
	return matchRevisionError(ErrRevisionValidation)
}

func matchRevisionIntegrityIssue(code sharederrors.ErrorCode) types.GomegaMatcher {
	return gcustom.MakeMatcher(func(issue RevisionIntegrityIssue) (bool, error) {
		return issue.Code == code && issue.MessageID == sharederrors.MessageIDForCode(code), nil
	}).WithTemplate("Expected:\n{{.FormattedActual}}\n{{.To}} match revision integrity issue\n{{format .Data 1}}", code)
}

var (
	errRevisionNotCreated          = errors.New("revision was not created")
	errRevisionUnexpectedlyCreated = errors.New("revision was unexpectedly created")
	errRevisionRecordSucceeded     = errors.New("revision record unexpectedly succeeded")
	errRevisionMetadataReplaced    = errors.New("revision metadata was unexpectedly marked for replacement")
	errRevisionRawContentSucceeded = errors.New("revision raw content unexpectedly succeeded")
	errRevisionFrontmatterMissing  = errors.New("revision frontmatter missing")
)

type revisionRecordResult struct {
	Revision *Revision
	Err      error
}

func createdRevisionRecord(rev *Revision, created bool, err error) revisionRecordResult {
	if err != nil {
		return revisionRecordResult{Revision: rev, Err: err}
	}
	if !created {
		return revisionRecordResult{Revision: rev, Err: errRevisionNotCreated}
	}
	return revisionRecordResult{Revision: rev}
}

func reusedRevisionRecord(rev *Revision, created bool, err error) revisionRecordResult {
	if err != nil {
		return revisionRecordResult{Revision: rev, Err: err}
	}
	if created {
		return revisionRecordResult{Revision: rev, Err: errRevisionUnexpectedlyCreated}
	}
	return revisionRecordResult{Revision: rev}
}

func failedRevisionRecord(rev *Revision, created bool, err error) revisionRecordResult {
	if created {
		return revisionRecordResult{Revision: rev, Err: errRevisionUnexpectedlyCreated}
	}
	if err == nil {
		return revisionRecordResult{Revision: rev, Err: errRevisionRecordSucceeded}
	}
	return revisionRecordResult{Revision: rev, Err: err}
}

func haveRecordedRevision(revision types.GomegaMatcher) types.GomegaMatcher {
	return gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
		"Revision": revision,
		"Err":      Succeed(),
	})
}

func haveRevisionRecordError(err types.GomegaMatcher) types.GomegaMatcher {
	return gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
		"Err": err,
	})
}

type restoredRawContentResult struct {
	Raw string
	Err error
}

func restoredBodyOnlyRawContent(raw string, replaceMetadata bool, err error) restoredRawContentResult {
	if err != nil {
		return restoredRawContentResult{Raw: raw, Err: err}
	}
	if replaceMetadata {
		return restoredRawContentResult{Raw: raw, Err: errRevisionMetadataReplaced}
	}
	return restoredRawContentResult{Raw: raw}
}

func failedRestoredRawContent(raw string, _ bool, err error) restoredRawContentResult {
	if err == nil {
		return restoredRawContentResult{Raw: raw, Err: errRevisionRawContentSucceeded}
	}
	return restoredRawContentResult{Raw: raw, Err: err}
}

func haveRestoredRawContent(raw types.GomegaMatcher) types.GomegaMatcher {
	return gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
		"Raw": raw,
		"Err": Succeed(),
	})
}

func haveRestoredRawContentError(err types.GomegaMatcher) types.GomegaMatcher {
	return gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
		"Err": err,
	})
}

type revisionFrontmatterParseResult struct {
	Frontmatter markdown.Frontmatter
	Body        string
	Err         error
}

func parsedRevisionFrontmatter(fm markdown.Frontmatter, body string, has bool, err error) revisionFrontmatterParseResult {
	if err != nil {
		return revisionFrontmatterParseResult{Frontmatter: fm, Body: body, Err: err}
	}
	if !has {
		return revisionFrontmatterParseResult{Frontmatter: fm, Body: body, Err: errRevisionFrontmatterMissing}
	}
	return revisionFrontmatterParseResult{Frontmatter: fm, Body: body}
}

func haveParsedRevisionFrontmatter(frontmatter, body types.GomegaMatcher) types.GomegaMatcher {
	return gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
		"Frontmatter": frontmatter,
		"Body":        body,
		"Err":         Succeed(),
	})
}
