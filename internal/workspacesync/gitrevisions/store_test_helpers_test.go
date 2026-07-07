package gitrevisions

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/go-git/go-git/v6/plumbing/object"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func actorIDStrings(ids []ActorID) []string {
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		out = append(out, id.String())
	}
	return out
}

func matchActorIDDerivedGitSignature(actorID ActorID) OmegaMatcher {
	GinkgoHelper()

	return WithTransform(gitSignatureActorObservationFor, Equal(actorIDDerivedGitIdentityObservation{
		State:   actorIDDerivedGitIdentityMatched,
		ActorID: actorID,
	}))
}

func matchListedCommitAuthorDerivedFromActorID(actorID ActorID) OmegaMatcher {
	GinkgoHelper()

	return WithTransform(listedCommitAuthorObservationFor, Equal(listedCommitAuthorObservation{
		AuthorID: actorID,
		Derived: actorIDDerivedGitIdentityObservation{
			State:   actorIDDerivedGitIdentityMatched,
			ActorID: actorID,
		},
	}))
}

type actorIDDerivedGitIdentityState uint8

const (
	actorIDDerivedGitIdentityUnexpected actorIDDerivedGitIdentityState = iota
	actorIDDerivedGitIdentityMatched
)

const actorIDDerivedGitEmailSuffix = "@leafwiki.local"

type actorIDDerivedGitIdentityObservation struct {
	State   actorIDDerivedGitIdentityState
	ActorID ActorID
}

type listedCommitAuthorObservation struct {
	AuthorID ActorID
	Derived  actorIDDerivedGitIdentityObservation
}

func gitSignatureActorObservationFor(signature object.Signature) actorIDDerivedGitIdentityObservation {
	return actorIDDerivedGitIdentityObservationFor(signature.Name, signature.Email)
}

func listedCommitAuthorObservationFor(commit Commit) listedCommitAuthorObservation {
	return listedCommitAuthorObservation{
		AuthorID: commit.AuthorID,
		Derived:  actorIDDerivedGitIdentityObservationFor(commit.AuthorName, commit.AuthorEmail),
	}
}

func actorIDDerivedGitIdentityObservationFor(name string, email string) actorIDDerivedGitIdentityObservation {
	emailActor, ok := strings.CutSuffix(email, actorIDDerivedGitEmailSuffix)
	if !ok {
		return actorIDDerivedGitIdentityObservation{State: actorIDDerivedGitIdentityUnexpected}
	}

	nameActorID := ParseActorID(name)
	emailActorID := ParseActorID(emailActor)
	if nameActorID == "" || nameActorID != emailActorID {
		return actorIDDerivedGitIdentityObservation{State: actorIDDerivedGitIdentityUnexpected}
	}

	return actorIDDerivedGitIdentityObservation{
		State:   actorIDDerivedGitIdentityMatched,
		ActorID: nameActorID,
	}
}

type managedMarkdownPathClass string

const (
	managedMarkdownRevisionPath          managedMarkdownPathClass = "managed markdown revision path"
	ignoredHiddenDirectoryMarkdownPath   managedMarkdownPathClass = "ignored hidden-directory markdown path"
	ignoredHiddenFilenameMarkdownPath    managedMarkdownPathClass = "ignored hidden-filename markdown path"
	ignoredSwapMarkdownPath              managedMarkdownPathClass = "ignored markdown swap path"
	ignoredNonMarkdownRevisionPath       managedMarkdownPathClass = "ignored non-markdown revision path"
	ignoredUnexpectedManagedPathDecision managedMarkdownPathClass = "unexpected managed markdown path decision"
)

func managedMarkdownPathClassFor(relPath string) managedMarkdownPathClass {
	if IsManagedMarkdownRelPath(relPath) {
		return managedMarkdownRevisionPath
	}

	slashPath := filepath.ToSlash(relPath)
	for _, segment := range strings.Split(slashPath, "/") {
		if strings.HasPrefix(segment, ".") {
			if strings.EqualFold(filepath.Ext(segment), ".md") {
				return ignoredHiddenFilenameMarkdownPath
			}
			return ignoredHiddenDirectoryMarkdownPath
		}
	}

	base := filepath.Base(slashPath)
	if strings.EqualFold(filepath.Ext(base), ".swp") && strings.EqualFold(filepath.Ext(strings.TrimSuffix(base, filepath.Ext(base))), ".md") {
		return ignoredSwapMarkdownPath
	}
	if !strings.EqualFold(filepath.Ext(base), ".md") {
		return ignoredNonMarkdownRevisionPath
	}
	return ignoredUnexpectedManagedPathDecision
}

func matchManagedMarkdownPathClass(expected managedMarkdownPathClass) OmegaMatcher {
	return WithTransform(managedMarkdownPathClassFor, Equal(expected))
}

func gitRevisionTempDir() string {
	GinkgoHelper()

	dir, err := os.MkdirTemp("", "leafwiki-gitrevisions-*")
	Expect(err).NotTo(HaveOccurred())
	DeferCleanup(os.RemoveAll, dir)
	return dir
}

func writeFile(path string, content string) {
	GinkgoHelper()

	Expect(os.MkdirAll(filepath.Dir(path), 0o755)).To(Succeed())
	Expect(os.WriteFile(path, []byte(content), 0o644)).To(Succeed())
}
