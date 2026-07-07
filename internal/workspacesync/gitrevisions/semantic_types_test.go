package gitrevisions

import (
	"strings"

	"github.com/go-git/go-git/v6/plumbing"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/perber/wiki/internal/core/identity"
)

var _ = Describe("git revision semantic types", Label("unit"), func() {
	It("normalizes actor identity for revision metadata", func() {
		Expect(ParseActorID(" editor-1 ")).To(Equal(identity.UserIDFromString("editor-1")))
		Expect(TrimActorID(identity.UserIDFromString(" editor-3 "))).To(Equal(identity.UserIDFromString("editor-3")))
	})

	It("derives git signature fields from the semantic actor ID", func() {
		actorID := identity.UserIDFromString("editor-1")

		Expect(gitSignatureFieldsForActorID(actorID)).To(Equal(actorIDGitSignatureObservation{
			NameActorID:  actorID,
			EmailActorID: actorID,
			EmailDomain:  actorIDGitSignatureLeafWikiDomain,
		}))
	})

	It("round-trips commit hashes through go-git plumbing values", func() {
		hash := plumbing.NewHash("0123456789abcdef0123456789abcdef01234567")

		commitHash := CommitHashFromPlumbingHash(hash)

		Expect(commitHash).To(Equal(identity.CommitHashFromString("0123456789abcdef0123456789abcdef01234567")))
		Expect(PlumbingHashFromCommitHash(commitHash)).To(Equal(hash))
	})
})

type actorIDGitSignatureEmailDomain uint8

const (
	actorIDGitSignatureUnexpectedDomain actorIDGitSignatureEmailDomain = iota
	actorIDGitSignatureLeafWikiDomain
)

type actorIDGitSignatureObservation struct {
	NameActorID  ActorID
	EmailActorID ActorID
	EmailDomain  actorIDGitSignatureEmailDomain
}

func gitSignatureFieldsForActorID(actorID ActorID) actorIDGitSignatureObservation {
	emailActor, ok := strings.CutSuffix(ActorIDGitSignatureEmail(actorID), actorIDDerivedGitEmailSuffix)
	if !ok {
		return actorIDGitSignatureObservation{
			NameActorID: ParseActorID(ActorIDGitSignatureName(actorID)),
			EmailDomain: actorIDGitSignatureUnexpectedDomain,
		}
	}
	return actorIDGitSignatureObservation{
		NameActorID:  ParseActorID(ActorIDGitSignatureName(actorID)),
		EmailActorID: ParseActorID(emailActor),
		EmailDomain:  actorIDGitSignatureLeafWikiDomain,
	}
}
