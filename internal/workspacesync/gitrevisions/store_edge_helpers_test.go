package gitrevisions

import (
	"errors"
	"io"
	"io/fs"
	"sort"
	"time"

	"github.com/go-git/go-git/v6/plumbing/object"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/types"

	"github.com/perber/wiki/internal/core/identity"
)

func matchCreatedRevisionCommit(hash identity.CommitHash) types.GomegaMatcher {
	return WithTransform(commitRevisionOutcomeFor, Equal(commitRevisionOutcome{
		State: revisionCommitCreated,
		Hash:  hash,
	}))
}

func matchCreatedRevisionCommitWithMarkdownPaths(paths ...string) types.GomegaMatcher {
	return WithTransform(commitRevisionPathOutcomeFor, Equal(commitRevisionPathOutcome{
		State:                revisionCommitCreated,
		ChangedMarkdownCount: len(paths),
		ChangedMarkdownPaths: sortedGitRevisionStrings(paths),
	}))
}

func matchReusedRevisionHead(hash identity.CommitHash) types.GomegaMatcher {
	return WithTransform(commitRevisionOutcomeFor, Equal(commitRevisionOutcome{
		State: revisionCommitReusedHead,
		Hash:  hash,
	}))
}

type revisionCommitState string

const (
	revisionCommitMissing    revisionCommitState = "missing revision commit"
	revisionCommitCreated    revisionCommitState = "created revision commit"
	revisionCommitReusedHead revisionCommitState = "reused revision HEAD"
)

type commitRevisionOutcome struct {
	State                revisionCommitState
	Hash                 identity.CommitHash
	ChangedMarkdownCount int
	ChangedMarkdownPaths []string
}

type commitRevisionPathOutcome struct {
	State                revisionCommitState
	ChangedMarkdownCount int
	ChangedMarkdownPaths []string
}

func commitRevisionOutcomeFor(commit *Commit) commitRevisionOutcome {
	if commit == nil {
		return commitRevisionOutcome{State: revisionCommitMissing}
	}

	state := revisionCommitReusedHead
	if commit.Created {
		state = revisionCommitCreated
	}

	return commitRevisionOutcome{
		State:                state,
		Hash:                 commit.Hash,
		ChangedMarkdownCount: commit.ChangedMarkdownCount,
		ChangedMarkdownPaths: sortedGitRevisionStrings(commit.ChangedMarkdownPaths),
	}
}

func commitRevisionPathOutcomeFor(commit *Commit) commitRevisionPathOutcome {
	outcome := commitRevisionOutcomeFor(commit)
	return commitRevisionPathOutcome{
		State:                outcome.State,
		ChangedMarkdownCount: outcome.ChangedMarkdownCount,
		ChangedMarkdownPaths: outcome.ChangedMarkdownPaths,
	}
}

func sortedGitRevisionStrings(values []string) []string {
	out := append([]string(nil), values...)
	sort.Strings(out)
	return out
}

type fakeCommitIter struct{}

func (fakeCommitIter) Next() (*object.Commit, error) {
	return nil, io.EOF
}

func (fakeCommitIter) ForEach(func(*object.Commit) error) error {
	return nil
}

func (fakeCommitIter) Close() {}

type fakeDirEntry struct {
	name string
	dir  bool
	typ  fs.FileMode
}

func (e fakeDirEntry) Name() string {
	return e.name
}

func (e fakeDirEntry) IsDir() bool {
	return e.dir
}

func (e fakeDirEntry) Type() fs.FileMode {
	return e.typ
}

func (e fakeDirEntry) Info() (fs.FileInfo, error) {
	return fakeFileInfo{}, nil
}

type fakeFileInfo struct{}

func (fakeFileInfo) Name() string {
	return ""
}

func (fakeFileInfo) Size() int64 {
	return 0
}

func (fakeFileInfo) Mode() fs.FileMode {
	return 0
}

func (fakeFileInfo) ModTime() time.Time {
	return time.Time{}
}

func (fakeFileInfo) IsDir() bool {
	return false
}

func (fakeFileInfo) Sys() any {
	return nil
}

func setGitRevisionSeam[T any](target *T, replacement T) func() {
	GinkgoHelper()

	original := *target
	*target = replacement
	restored := false
	restore := func() {
		if restored {
			return
		}
		*target = original
		restored = true
	}
	DeferCleanup(restore)
	return restore
}

var errGitDirTargetAbsent = errors.New("gitdir target absent")

func gitDirFileTargetResult(raw string, rootDir string) (string, error) {
	target, ok := parseGitDirFile(raw, rootDir)
	if !ok {
		return "", errGitDirTargetAbsent
	}
	return target, nil
}
