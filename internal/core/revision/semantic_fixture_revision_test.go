package revision

import (
	"errors"
	"os"

	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
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

func newFixtureErrorCode[T ~string](raw T) sharederrors.ErrorCode {
	return sharederrors.ErrorCode(raw)
}

func newFixtureAssetName[T ~string](raw T) tree.AssetName {
	return tree.AssetNameFromString(raw)
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
	return WithTransform(localizedRevisionErrorObservationFor, gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
		"State":     Equal(localizedRevisionErrorPresent),
		"Code":      Equal(code),
		"MessageID": Equal(sharederrors.MessageIDForCode(code)),
		"Args":      Equal(args),
	}))
}

func matchRevisionErrorCause(want error) types.GomegaMatcher {
	return MatchError(want)
}

func matchRevisionError(want error) types.GomegaMatcher {
	return MatchError(want)
}

func rejectRevisionValidation() types.GomegaMatcher {
	return matchRevisionError(ErrRevisionValidation)
}

func matchRevisionIntegrityIssue(code sharederrors.ErrorCode) types.GomegaMatcher {
	return gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
		"Code":      Equal(code),
		"MessageID": Equal(sharederrors.MessageIDForCode(code)),
	})
}

type localizedRevisionErrorState uint8

const (
	localizedRevisionErrorAbsent localizedRevisionErrorState = iota
	localizedRevisionErrorPresent
)

type localizedRevisionErrorObservation struct {
	State     localizedRevisionErrorState
	Code      sharederrors.ErrorCode
	MessageID sharederrors.MessageID
	Args      []string
}

func localizedRevisionErrorObservationFor(err error) localizedRevisionErrorObservation {
	localized, ok := sharederrors.AsLocalizedError(err)
	if !ok {
		return localizedRevisionErrorObservation{State: localizedRevisionErrorAbsent}
	}
	return localizedRevisionErrorObservation{
		State:     localizedRevisionErrorPresent,
		Code:      localized.Code,
		MessageID: localized.MessageID,
		Args:      localized.Args,
	}
}

type assetManifestPresence uint8

const (
	assetManifestMissing assetManifestPresence = iota
	assetManifestPresent
)

type assetManifestObservation struct {
	Hash  string
	State assetManifestPresence
}

func assetManifestObservationFor(store *FSStore, hash string) assetManifestObservation {
	state := assetManifestMissing
	if store != nil && store.AssetManifestExists(hash) {
		state = assetManifestPresent
	}
	return assetManifestObservation{Hash: hash, State: state}
}

func matchAssetManifestPresence(state assetManifestPresence, hash types.GomegaMatcher) types.GomegaMatcher {
	return gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
		"Hash":  hash,
		"State": Equal(state),
	})
}

func matchRevisionContentDelta(baseContent, targetContent types.GomegaMatcher) types.GomegaMatcher {
	return WithTransform(revisionContentDeltaObservationFor, gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
		"State":         Equal(revisionContentDeltaChanged),
		"BaseContent":   baseContent,
		"TargetContent": targetContent,
	}))
}

type revisionContentDeltaState uint8

const (
	revisionContentDeltaAbsent revisionContentDeltaState = iota
	revisionContentDeltaChanged
)

type revisionContentDeltaObservation struct {
	State         revisionContentDeltaState
	BaseContent   string
	TargetContent string
}

func revisionContentDeltaObservationFor(comparison *RevisionComparison) revisionContentDeltaObservation {
	if comparison == nil || comparison.Base == nil || comparison.Target == nil || !comparison.ContentChanged {
		return revisionContentDeltaObservation{State: revisionContentDeltaAbsent}
	}
	return revisionContentDeltaObservation{
		State:         revisionContentDeltaChanged,
		BaseContent:   comparison.Base.Content,
		TargetContent: comparison.Target.Content,
	}
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
