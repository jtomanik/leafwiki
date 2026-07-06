package gitrevisions

import (
	"errors"
	"os"
	"path/filepath"

	"github.com/go-git/go-billy/v6"
	git "github.com/go-git/go-git/v6"
	"github.com/go-git/go-git/v6/plumbing/object"
	gitstorage "github.com/go-git/go-git/v6/storage"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/gstruct"
)

var _ = Describe("git revision edge behavior", func() {
	It("handles helper defaults and parser fallbacks", Label("unit"), func() {
		Expect(nonFilesystemInitStorage{}.Init()).To(Succeed())

		target, err := gitDirFileTargetResult("not-a-gitdir", "/workspace")
		Expect(err).To(MatchError(errGitDirTargetAbsent))
		Expect(target).To(BeEmpty())

		target, err = gitDirFileTargetResult("gitdir:   \n", "/workspace")
		Expect(err).To(MatchError(errGitDirTargetAbsent))
		Expect(target).To(BeEmpty())

		target, err = gitDirFileTargetResult("gitdir: /tmp/repo/.git\n", "/workspace")
		Expect(err).NotTo(HaveOccurred())
		Expect(target).To(Equal(filepath.Clean("/tmp/repo/.git")))

		Expect(mergeMarkdownPaths([]string{" a.md ", "", "nested/b.md"}, []string{"a.md"})).To(Equal([]string{"a.md", "nested/b.md"}))
		startupCommit := commitFromObject(&object.Commit{Message: commitMessage(CommitRequest{Reason: ReasonStartup}, "batch-1", nil)})
		Expect(startupCommit.Reason).To(Equal(ReasonStartup))
		restoreCommit := commitFromObject(&object.Commit{Message: commitMessage(CommitRequest{Reason: ReasonRestore}, "batch-1", nil)})
		Expect(restoreCommit.Reason).To(Equal(ReasonRestore))
		Expect(commitActorIDs(CommitRequest{AdditionalActors: []Actor{{}, {ID: newFixtureActorID("public-editor")}}})).To(Equal([]ActorID{newFixtureActorID("public-editor")}))

		restoreRand := setGitRevisionSeam(&gitRevisionRandRead, func([]byte) (int, error) {
			return 0, errors.New("rand failed")
		})
		Expect(newBatchID()).NotTo(BeEmpty())
		restoreRand()

		Expect(signature(Actor{})).To(gstruct.PointTo(gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
			"Name":  Equal("Public Editor"),
			"Email": Equal("public-editor@leafwiki.local"),
		})))

		Expect(signature(Actor{ID: newFixtureActorID("agent-1")})).To(gstruct.PointTo(gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
			"Name":  Equal("agent-1"),
			"Email": Equal("agent-1@leafwiki.local"),
		})))

		title, _, actors := parseCommitMessage("Title\nnot-a-trailer\nOther: value\nLeafWiki-Actor: alice\n")
		Expect(title).To(Equal("Title"))
		Expect(actors).To(Equal([]ActorID{newFixtureActorID("alice")}))
	})

	It("reports Open validation and dependency failures", Label("unit"), func() {
		_, err := Open(StoreOptions{})
		Expect(err).To(MatchError(ErrDataDirRequired))

		_, err = Open(StoreOptions{DataDir: "data"})
		Expect(err).To(MatchError(ErrRootDirRequired))

		errMkdirInternalFailed := errors.New("mkdir internal failed")
		restoreMkdir := setGitRevisionSeam(&gitRevisionMkdirAll, func(string, os.FileMode) error {
			return errMkdirInternalFailed
		})
		_, err = Open(StoreOptions{DataDir: gitRevisionTempDir(), RootDir: filepath.Join(gitRevisionTempDir(), "root")})
		Expect(err).To(MatchError(errMkdirInternalFailed))
		restoreMkdir()

		errMkdirRootFailed := errors.New("mkdir root failed")
		mkdirCalls := 0
		restoreMkdir = setGitRevisionSeam(&gitRevisionMkdirAll, func(string, os.FileMode) error {
			mkdirCalls++
			if mkdirCalls == 2 {
				return errMkdirRootFailed
			}
			return nil
		})
		_, err = Open(StoreOptions{DataDir: gitRevisionTempDir(), RootDir: filepath.Join(gitRevisionTempDir(), "root")})
		Expect(err).To(MatchError(errMkdirRootFailed))
		restoreMkdir()

		restoreOpen := setGitRevisionSeam(&gitRevisionGitOpen, func(gitstorage.Storer, billy.Filesystem) (*git.Repository, error) {
			return nil, git.ErrRepositoryNotExists
		})
		errInitFailed := errors.New("init failed")
		restoreInit := setGitRevisionSeam(&gitRevisionGitInit, func(gitstorage.Storer, ...git.InitOption) (*git.Repository, error) {
			return nil, errInitFailed
		})
		_, err = Open(StoreOptions{DataDir: gitRevisionTempDir(), RootDir: filepath.Join(gitRevisionTempDir(), "root")})
		Expect(err).To(MatchError(errInitFailed))
		restoreInit()
		restoreOpen()

		errSecondOpenFailed := errors.New("second open failed")
		openCalls := 0
		restoreOpen = setGitRevisionSeam(&gitRevisionGitOpen, func(gitstorage.Storer, billy.Filesystem) (*git.Repository, error) {
			openCalls++
			if openCalls == 1 {
				return nil, git.ErrRepositoryNotExists
			}
			return nil, errSecondOpenFailed
		})
		restoreInit = setGitRevisionSeam(&gitRevisionGitInit, func(gitstorage.Storer, ...git.InitOption) (*git.Repository, error) {
			return &git.Repository{}, nil
		})
		_, err = Open(StoreOptions{DataDir: gitRevisionTempDir(), RootDir: filepath.Join(gitRevisionTempDir(), "root")})
		Expect(err).To(MatchError(errSecondOpenFailed))
		restoreInit()
		restoreOpen()

		errRemoveRootGitFileFailed := errors.New("remove .git failed")
		restoreCleanup := setGitRevisionSeam(&gitRevisionRemoveInternalRootGitFile, func(string, string) error {
			return errRemoveRootGitFileFailed
		})
		_, err = Open(StoreOptions{DataDir: gitRevisionTempDir(), RootDir: filepath.Join(gitRevisionTempDir(), "root")})
		Expect(err).To(MatchError(errRemoveRootGitFileFailed))
		restoreCleanup()
	})

	It("cleans up only the internal root git file", Label("unit"), func() {
		errStatFailed := errors.New("stat failed")
		restoreLstat := setGitRevisionSeam(&gitRevisionLstat, func(string) (os.FileInfo, error) {
			return nil, errStatFailed
		})
		Expect(removeInternalRootGitFile(gitRevisionTempDir(), "/internal/git")).To(MatchError(errStatFailed))
		restoreLstat()

		rootDir := gitRevisionTempDir()
		internal := filepath.Join(gitRevisionTempDir(), ".leafwiki", "git")
		Expect(os.MkdirAll(filepath.Join(rootDir, ".git"), 0o755)).To(Succeed())
		Expect(removeInternalRootGitFile(rootDir, internal)).To(Succeed())

		rootDir = gitRevisionTempDir()
		Expect(os.WriteFile(filepath.Join(rootDir, ".git"), []byte("gitdir: ../elsewhere\n"), 0o644)).To(Succeed())
		Expect(removeInternalRootGitFile(rootDir, internal)).To(Succeed())
		Expect(os.ReadFile(filepath.Join(rootDir, ".git"))).To(Equal([]byte("gitdir: ../elsewhere\n")))

		rootDir = gitRevisionTempDir()
		Expect(os.MkdirAll(internal, 0o755)).To(Succeed())
		Expect(os.WriteFile(filepath.Join(rootDir, ".git"), []byte("gitdir: "+internal+"\n"), 0o644)).To(Succeed())
		Expect(removeInternalRootGitFile(rootDir, internal)).To(Succeed())
		_, err := os.Stat(filepath.Join(rootDir, ".git"))
		Expect(err).To(MatchError(os.ErrNotExist))

		rootDir = gitRevisionTempDir()
		Expect(os.WriteFile(filepath.Join(rootDir, ".git"), []byte("gitdir: "+internal+"\n"), 0o644)).To(Succeed())
		errReadFailed := errors.New("read failed")
		restoreRead := setGitRevisionSeam(&gitRevisionReadFile, func(string) ([]byte, error) {
			return nil, errReadFailed
		})
		Expect(removeInternalRootGitFile(rootDir, internal)).To(MatchError(errReadFailed))
		restoreRead()

		rootDir = gitRevisionTempDir()
		Expect(os.WriteFile(filepath.Join(rootDir, ".git"), []byte("gitdir: "+internal+"\n"), 0o644)).To(Succeed())
		errRemoveFailed := errors.New("remove failed")
		restoreRemove := setGitRevisionSeam(&gitRevisionRemove, func(string) error {
			return errRemoveFailed
		})
		Expect(removeInternalRootGitFile(rootDir, internal)).To(MatchError(errRemoveFailed))
		restoreRemove()
	})

})
