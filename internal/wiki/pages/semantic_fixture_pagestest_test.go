package pages_test

import (
	"errors"
	"os"

	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
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
	return WithTransform(outgoingLinkObservationFor, Equal(outgoingLinkObservation{
		Path:        path,
		Health:      outgoingLinkBroken,
		TargetState: outgoingLinkTargetAbsent,
	}))
}

func HaveHealthyOutgoing(path tree.RoutePath, targetID tree.PageID) types.GomegaMatcher {
	return WithTransform(outgoingLinkObservationFor, Equal(outgoingLinkObservation{
		Path:        path,
		Health:      outgoingLinkHealthy,
		TargetState: outgoingLinkTargetPresent,
		TargetID:    targetID,
	}))
}

func HaveHealthyOutgoingWithAnyTarget(path tree.RoutePath) types.GomegaMatcher {
	return WithTransform(outgoingLinkObservationFor, SatisfyAll(
		HaveField("Path", Equal(path)),
		HaveField("Health", Equal(outgoingLinkHealthy)),
		HaveField("TargetState", Equal(outgoingLinkTargetPresent)),
	))
}

type outgoingLinkHealth uint8

const (
	outgoingLinkBroken outgoingLinkHealth = iota
	outgoingLinkHealthy
)

type outgoingLinkTargetState uint8

const (
	outgoingLinkTargetAbsent outgoingLinkTargetState = iota
	outgoingLinkTargetPresent
)

type outgoingLinkObservation struct {
	Path        tree.RoutePath
	Health      outgoingLinkHealth
	TargetState outgoingLinkTargetState
	TargetID    tree.PageID
}

func outgoingLinkObservationFor(outgoing links.OutgoingResultItem) outgoingLinkObservation {
	health := outgoingLinkHealthy
	if outgoing.Broken {
		health = outgoingLinkBroken
	}
	targetState := outgoingLinkTargetPresent
	if outgoing.ToPageID == "" {
		targetState = outgoingLinkTargetAbsent
	}
	return outgoingLinkObservation{
		Path:        outgoing.ToPath,
		Health:      health,
		TargetState: targetState,
		TargetID:    outgoing.ToPageID,
	}
}

func HavePageSaveContentChange() types.GomegaMatcher {
	return WithTransform(pageSaveContentStateFor, Equal(pageSaveContentChanged))
}

type pageSaveContentState uint8

const (
	pageSaveContentUnchanged pageSaveContentState = iota
	pageSaveContentChanged
)

func pageSaveContentStateFor(event pagesave.PageSaveEvent) pageSaveContentState {
	if event.ContentChanged {
		return pageSaveContentChanged
	}
	return pageSaveContentUnchanged
}
