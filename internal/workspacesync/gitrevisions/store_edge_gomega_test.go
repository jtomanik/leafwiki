package gitrevisions

import (
	"context"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/go-git/go-billy/v6"
	git "github.com/go-git/go-git/v6"
	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/format/index"
	"github.com/go-git/go-git/v6/plumbing/object"
	gitstorage "github.com/go-git/go-git/v6/storage"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/gcustom"
	"github.com/onsi/gomega/gstruct"
	"github.com/onsi/gomega/types"

	"github.com/perber/wiki/internal/core/identity"
)

func matchCreatedRevisionCommit(hash identity.CommitHash) types.GomegaMatcher {
	return gcustom.MakeMatcher(func(commit *Commit) (bool, error) {
		return commit != nil &&
			commit.Created &&
			commit.Hash == hash, nil
	}).WithMessage("create a revision commit")
}

func matchCreatedRevisionCommitWithMarkdownPaths(paths ...string) types.GomegaMatcher {
	return gcustom.MakeMatcher(func(commit *Commit) (bool, error) {
		if commit == nil || !commit.Created || commit.ChangedMarkdownCount != len(paths) {
			return false, nil
		}
		return ConsistOf(paths).Match(commit.ChangedMarkdownPaths)
	}).WithMessage("create a revision commit for changed markdown paths")
}

func matchReusedRevisionHead(hash identity.CommitHash) types.GomegaMatcher {
	return gcustom.MakeMatcher(func(commit *Commit) (bool, error) {
		return commit != nil &&
			!commit.Created &&
			commit.Hash == hash, nil
	}).WithMessage("reuse the current revision HEAD")
}

var _ = Describe("git revision edge behavior", func() {
	It("handles helper defaults and parser fallbacks", func() {
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
		Expect(commitActorIDs(CommitRequest{AdditionalActors: []Actor{{}, {ID: "public-editor"}}})).To(Equal([]ActorID{"public-editor"}))

		restoreRand := setGitRevisionSeam(&gitRevisionRandRead, func([]byte) (int, error) {
			return 0, errors.New("rand failed")
		})
		Expect(newBatchID()).NotTo(BeEmpty())
		restoreRand()

		Expect(signature(Actor{})).To(gstruct.PointTo(gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
			"Name":  Equal("Public Editor"),
			"Email": Equal("public-editor@leafwiki.local"),
		})))

		Expect(signature(Actor{ID: "agent-1"})).To(gstruct.PointTo(gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
			"Name":  Equal("agent-1"),
			"Email": Equal("agent-1@leafwiki.local"),
		})))

		title, _, actors := parseCommitMessage("Title\nnot-a-trailer\nOther: value\nLeafWiki-Actor: alice\n")
		Expect(title).To(Equal("Title"))
		Expect(actors).To(Equal([]ActorID{"alice"}))
	})

	It("reports Open validation and dependency failures", func() {
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
			return nil, nil
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

	It("cleans up only the internal root git file", func() {
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

	It("reports commit fallback and dependency errors through seams", func() {
		store := &Store{}
		canceled, cancel := context.WithCancel(context.Background())
		cancel()
		_, err := store.Capture(canceled, CommitRequest{})
		Expect(err).To(Equal(context.Canceled))

		errWorktreeFailed := errors.New("worktree failed")
		restoreWorktree := setGitRevisionSeam(&gitRevisionRepoWorktree, func(*git.Repository) (*git.Worktree, error) {
			return nil, errWorktreeFailed
		})
		_, err = store.Capture(context.Background(), CommitRequest{})
		Expect(err).To(MatchError(errWorktreeFailed))
		restoreWorktree()

		restoreWorktree = setGitRevisionSeam(&gitRevisionRepoWorktree, func(*git.Repository) (*git.Worktree, error) {
			return nil, nil
		})
		errStageFailed := errors.New("stage failed")
		restoreStage := setGitRevisionSeam(&gitRevisionStoreStageMarkdownChanges, func(*Store, context.Context, *git.Worktree) ([]string, error) {
			return nil, errStageFailed
		})
		_, err = store.Capture(context.Background(), CommitRequest{})
		Expect(err).To(MatchError(errStageFailed))
		restoreStage()
		restoreStage = setGitRevisionSeam(&gitRevisionStoreStageMarkdownChanges, func(*Store, context.Context, *git.Worktree) ([]string, error) {
			return nil, nil
		})

		hash := plumbing.NewHash("1111111111111111111111111111111111111111")
		errCommitFailed := errors.New("commit failed")
		restoreCommit := setGitRevisionSeam(&gitRevisionWorktreeCommit, func(*git.Worktree, string, *git.CommitOptions) (plumbing.Hash, error) {
			return plumbing.ZeroHash, errCommitFailed
		})
		_, err = store.Capture(context.Background(), CommitRequest{})
		Expect(err).To(MatchError(errCommitFailed))
		restoreCommit()

		commitCalls := 0
		restoreCommit = setGitRevisionSeam(&gitRevisionWorktreeCommit, func(*git.Worktree, string, *git.CommitOptions) (plumbing.Hash, error) {
			commitCalls++
			if commitCalls == 1 {
				return plumbing.ZeroHash, git.ErrEmptyCommit
			}
			return hash, nil
		})
		restoreHead := setGitRevisionSeam(&gitRevisionRepoHead, func(*git.Repository) (*plumbing.Reference, error) {
			return nil, plumbing.ErrReferenceNotFound
		})
		commit, err := store.Capture(context.Background(), CommitRequest{})
		Expect(err).NotTo(HaveOccurred())
		Expect(commit).To(matchCreatedRevisionCommit(identity.CommitHashFromString(hash.String())))
		restoreHead()
		restoreCommit()

		commitCalls = 0
		errEmptyRestoreFailed := errors.New("empty restore failed")
		restoreCommit = setGitRevisionSeam(&gitRevisionWorktreeCommit, func(*git.Worktree, string, *git.CommitOptions) (plumbing.Hash, error) {
			commitCalls++
			if commitCalls == 1 {
				return plumbing.ZeroHash, git.ErrEmptyCommit
			}
			return plumbing.ZeroHash, errEmptyRestoreFailed
		})
		restoreHead = setGitRevisionSeam(&gitRevisionRepoHead, func(*git.Repository) (*plumbing.Reference, error) {
			return nil, nil
		})
		_, err = store.Capture(context.Background(), CommitRequest{Reason: ReasonRestore})
		Expect(err).To(MatchError(errEmptyRestoreFailed))
		restoreHead()
		restoreCommit()

		commitCalls = 0
		errEmptyInitialFailed := errors.New("empty initial failed")
		restoreCommit = setGitRevisionSeam(&gitRevisionWorktreeCommit, func(*git.Worktree, string, *git.CommitOptions) (plumbing.Hash, error) {
			commitCalls++
			if commitCalls == 1 {
				return plumbing.ZeroHash, git.ErrEmptyCommit
			}
			return plumbing.ZeroHash, errEmptyInitialFailed
		})
		restoreHead = setGitRevisionSeam(&gitRevisionRepoHead, func(*git.Repository) (*plumbing.Reference, error) {
			return nil, plumbing.ErrReferenceNotFound
		})
		_, err = store.Capture(context.Background(), CommitRequest{})
		Expect(err).To(MatchError(errEmptyInitialFailed))
		restoreHead()
		restoreCommit()

		restoreCommit = setGitRevisionSeam(&gitRevisionWorktreeCommit, func(*git.Worktree, string, *git.CommitOptions) (plumbing.Hash, error) {
			return plumbing.ZeroHash, git.ErrEmptyCommit
		})
		restoreHead = setGitRevisionSeam(&gitRevisionRepoHead, func(*git.Repository) (*plumbing.Reference, error) {
			return plumbing.NewHashReference(plumbing.HEAD, hash), nil
		})
		commit, err = store.Capture(context.Background(), CommitRequest{})
		Expect(err).NotTo(HaveOccurred())
		Expect(commit).To(matchReusedRevisionHead(identity.CommitHashFromString(hash.String())))
		restoreHead()
		restoreCommit()

		restoreCommit = setGitRevisionSeam(&gitRevisionWorktreeCommit, func(*git.Worktree, string, *git.CommitOptions) (plumbing.Hash, error) {
			return plumbing.ZeroHash, git.ErrEmptyCommit
		})
		restoreHead = setGitRevisionSeam(&gitRevisionRepoHead, func(*git.Repository) (*plumbing.Reference, error) {
			return nil, errors.New("head failed")
		})
		_, err = store.Capture(context.Background(), CommitRequest{})
		Expect(err).To(Equal(git.ErrEmptyCommit))
		restoreHead()
		restoreCommit()
		restoreStage()
		restoreWorktree()
	})

	It("reports index and tracked-file store errors", func() {
		store := &Store{}
		indexErr := errors.New("index failed")
		restoreIndex := setGitRevisionSeam(&gitRevisionRepositoryIndex, func(*git.Repository) (*index.Index, error) {
			return nil, indexErr
		})
		Expect(store.removeFromIndexOnly("old.md")).To(MatchError(indexErr))
		restoreIndex()

		restoreIndex = setGitRevisionSeam(&gitRevisionRepositoryIndex, func(*git.Repository) (*index.Index, error) {
			return &index.Index{}, nil
		})
		indexRemoveErr := errors.New("index remove failed")
		restoreIndexRemove := setGitRevisionSeam(&gitRevisionIndexRemove, func(*index.Index, string) (*index.Entry, error) {
			return nil, indexRemoveErr
		})
		Expect(store.removeFromIndexOnly("old.md")).To(MatchError(indexRemoveErr))
		restoreIndexRemove()

		restoreIndexRemove = setGitRevisionSeam(&gitRevisionIndexRemove, func(*index.Index, string) (*index.Entry, error) {
			return &index.Entry{}, nil
		})
		setIndexErr := errors.New("set index failed")
		restoreSetIndex := setGitRevisionSeam(&gitRevisionRepositorySetIndex, func(*git.Repository, *index.Index) error {
			return setIndexErr
		})
		Expect(store.removeFromIndexOnly("old.md")).To(MatchError(setIndexErr))
		restoreSetIndex()
		restoreIndexRemove()
		restoreIndex()

		head := plumbing.NewHashReference(plumbing.HEAD, plumbing.NewHash("1111111111111111111111111111111111111111"))
		errHeadFailed := errors.New("head failed")
		restoreHead := setGitRevisionSeam(&gitRevisionRepoHead, func(*git.Repository) (*plumbing.Reference, error) {
			return nil, errHeadFailed
		})
		_, err := store.trackedMarkdownFiles()
		Expect(err).To(MatchError(errHeadFailed))
		restoreHead()

		restoreHead = setGitRevisionSeam(&gitRevisionRepoHead, func(*git.Repository) (*plumbing.Reference, error) {
			return head, nil
		})
		errCommitObjectFailed := errors.New("commit failed")
		restoreCommitObject := setGitRevisionSeam(&gitRevisionRepoCommitObject, func(*git.Repository, plumbing.Hash) (*object.Commit, error) {
			return nil, errCommitObjectFailed
		})
		_, err = store.trackedMarkdownFiles()
		Expect(err).To(MatchError(errCommitObjectFailed))
		restoreCommitObject()

		restoreCommitObject = setGitRevisionSeam(&gitRevisionRepoCommitObject, func(*git.Repository, plumbing.Hash) (*object.Commit, error) {
			return &object.Commit{}, nil
		})
		errCommitTreeFailed := errors.New("tree failed")
		restoreCommitTree := setGitRevisionSeam(&gitRevisionCommitTree, func(*object.Commit) (*object.Tree, error) {
			return nil, errCommitTreeFailed
		})
		_, err = store.trackedMarkdownFiles()
		Expect(err).To(MatchError(errCommitTreeFailed))
		restoreCommitTree()

		restoreCommitTree = setGitRevisionSeam(&gitRevisionCommitTree, func(*object.Commit) (*object.Tree, error) {
			return &object.Tree{}, nil
		})
		restoreTreeFiles := setGitRevisionSeam(&gitRevisionTreeFiles, func(*object.Tree) *object.FileIter {
			return &object.FileIter{}
		})
		errFileIterFailed := errors.New("iter failed")
		restoreFileIter := setGitRevisionSeam(&gitRevisionFileIterForEach, func(*object.FileIter, func(*object.File) error) error {
			return errFileIterFailed
		})
		_, err = store.trackedMarkdownFiles()
		Expect(err).To(MatchError(errFileIterFailed))
		restoreFileIter()

		restoreFileIter = setGitRevisionSeam(&gitRevisionFileIterForEach, func(_ *object.FileIter, visit func(*object.File) error) error {
			return visit(&object.File{Name: "page.md"})
		})
		errFileReaderFailed := errors.New("reader failed")
		restoreFileReader := setGitRevisionSeam(&gitRevisionFileReader, func(*object.File) (io.ReadCloser, error) {
			return nil, errFileReaderFailed
		})
		_, err = store.trackedMarkdownFiles()
		Expect(err).To(MatchError(errFileReaderFailed))
		restoreFileReader()

		restoreFileReader = setGitRevisionSeam(&gitRevisionFileReader, func(*object.File) (io.ReadCloser, error) {
			return io.NopCloser(strings.NewReader("# Page\n")), nil
		})
		errReadAllFailed := errors.New("read all failed")
		restoreReadAll := setGitRevisionSeam(&gitRevisionReadAll, func(io.Reader) ([]byte, error) {
			return nil, errReadAllFailed
		})
		_, err = store.trackedMarkdownFiles()
		Expect(err).To(MatchError(errReadAllFailed))
		restoreReadAll()
		restoreFileReader()
		restoreFileIter()
		restoreTreeFiles()
		restoreCommitTree()
		restoreCommitObject()
		restoreHead()
	})

	It("reports staging errors through markdown change seams", func() {
		store := &Store{rootDir: "/workspace"}

		errCollectFailed := errors.New("collect failed")
		restoreCollect := setGitRevisionSeam(&gitRevisionCollectMarkdownPaths, func(string) ([]string, error) {
			return nil, errCollectFailed
		})
		_, err := store.stageMarkdownChanges(context.Background(), nil)
		Expect(err).To(MatchError(errCollectFailed))
		restoreCollect()

		restoreCollect = setGitRevisionSeam(&gitRevisionCollectMarkdownPaths, func(string) ([]string, error) {
			return []string{"page.md"}, nil
		})
		errTrackedFailed := errors.New("tracked failed")
		restoreTracked := setGitRevisionSeam(&gitRevisionStoreTrackedMarkdownFiles, func(*Store) (map[string]string, error) {
			return nil, errTrackedFailed
		})
		_, err = store.stageMarkdownChanges(context.Background(), nil)
		Expect(err).To(MatchError(errTrackedFailed))
		restoreTracked()

		restoreTracked = setGitRevisionSeam(&gitRevisionStoreTrackedMarkdownFiles, func(*Store) (map[string]string, error) {
			return map[string]string{}, nil
		})
		canceled, cancel := context.WithCancel(context.Background())
		cancel()
		_, err = store.stageMarkdownChanges(canceled, nil)
		Expect(err).To(Equal(context.Canceled))

		errReadFailed := errors.New("read failed")
		restoreRead := setGitRevisionSeam(&gitRevisionReadFile, func(string) ([]byte, error) {
			return nil, errReadFailed
		})
		_, err = store.stageMarkdownChanges(context.Background(), nil)
		Expect(err).To(MatchError(errReadFailed))
		restoreRead()

		restoreRead = setGitRevisionSeam(&gitRevisionReadFile, func(string) ([]byte, error) {
			return []byte("# Page\n"), nil
		})
		errAddFailed := errors.New("add failed")
		restoreAdd := setGitRevisionSeam(&gitRevisionWorktreeAdd, func(*git.Worktree, string) (plumbing.Hash, error) {
			return plumbing.ZeroHash, errAddFailed
		})
		_, err = store.stageMarkdownChanges(context.Background(), nil)
		Expect(err).To(MatchError(errAddFailed))
		restoreAdd()

		restoreCollect()
		restoreCollect = setGitRevisionSeam(&gitRevisionCollectMarkdownPaths, func(string) ([]string, error) {
			return nil, nil
		})
		restoreTracked()
		restoreTracked = setGitRevisionSeam(&gitRevisionStoreTrackedMarkdownFiles, func(*Store) (map[string]string, error) {
			return map[string]string{"old.md": "# Old\n"}, nil
		})
		errRemoveFailed := errors.New("remove failed")
		restoreRemove := setGitRevisionSeam(&gitRevisionWorktreeRemove, func(*git.Worktree, string) (plumbing.Hash, error) {
			return plumbing.ZeroHash, errRemoveFailed
		})
		_, err = store.stageMarkdownChanges(context.Background(), nil)
		Expect(err).To(MatchError(errRemoveFailed))
		restoreRemove()

		restoreTracked()
		restoreTracked = setGitRevisionSeam(&gitRevisionStoreTrackedMarkdownFiles, func(*Store) (map[string]string, error) {
			return map[string]string{".obsidian/local.md": "# Local\n"}, nil
		})
		errRemoveIndexFailed := errors.New("remove index failed")
		restoreRemoveIndex := setGitRevisionSeam(&gitRevisionStoreRemoveFromIndexOnly, func(*Store, string) error {
			return errRemoveIndexFailed
		})
		_, err = store.stageMarkdownChanges(context.Background(), nil)
		Expect(err).To(MatchError(errRemoveIndexFailed))
		restoreRemoveIndex()
		restoreTracked()
		restoreCollect()
		restoreRead()
	})

	It("reports markdown path collection walk and relative-path errors", func() {
		walkErr := errors.New("walk failed")
		restoreWalk := setGitRevisionSeam(&gitRevisionWalkDir, func(root string, fn fs.WalkDirFunc) error {
			return fn(filepath.Join(root, "bad.md"), fakeDirEntry{name: "bad.md"}, walkErr)
		})
		_, err := collectMarkdownPaths("/workspace")
		Expect(err).To(MatchError(walkErr))
		restoreWalk()

		restoreWalk = setGitRevisionSeam(&gitRevisionWalkDir, func(root string, fn fs.WalkDirFunc) error {
			Expect(fn(root, fakeDirEntry{name: filepath.Base(root), dir: true}, nil)).To(Succeed())
			return fn(filepath.Join(root, "page.md"), fakeDirEntry{name: "page.md", typ: 0}, nil)
		})
		errRelFailed := errors.New("rel failed")
		restoreRel := setGitRevisionSeam(&gitRevisionRel, func(string, string) (string, error) {
			return "", errRelFailed
		})
		_, err = collectMarkdownPaths("/workspace")
		Expect(err).To(MatchError(errRelFailed))
		restoreRel()
		restoreWalk()
	})

	It("reports canceled list, get, restore, and file read operations", func() {
		canceled, cancel := context.WithCancel(context.Background())
		cancel()

		store := &Store{}
		restoreLog := setGitRevisionSeam(&gitRevisionRepoLog, func(*git.Repository, *git.LogOptions) (object.CommitIter, error) {
			return nil, plumbing.ErrReferenceNotFound
		})
		_, err := store.ListCommits(context.Background(), ListRequest{})
		Expect(err).NotTo(HaveOccurred())
		restoreLog()
		Expect(store.ForEachCommit(canceled, func(Commit) (bool, error) { return true, nil })).To(Equal(context.Canceled))
		Expect(store.ForEachCommit(context.Background(), nil)).To(Succeed())
		_, err = store.GetCommit(canceled, identity.CommitHashFromString("hash"))
		Expect(err).To(Equal(context.Canceled))
		_, err = store.ChangedMarkdownPaths(canceled, identity.CommitHashFromString("hash"))
		Expect(err).To(Equal(context.Canceled))
		_, err = store.FilesAt(canceled, identity.CommitHashFromString("hash"))
		Expect(err).To(Equal(context.Canceled))

		_, err = store.RestoreWorkspace(canceled, identity.CommitHashFromString("hash"), CommitRequest{})
		Expect(err).To(Equal(context.Canceled))
		_, err = store.RestoreDocumentToPath(canceled, "page.md", "page.md", identity.CommitHashFromString("hash"), CommitRequest{})
		Expect(err).To(Equal(context.Canceled))
		_, err = store.RestoreDocumentToPath(context.Background(), "../bad.md", "page.md", identity.CommitHashFromString("hash"), CommitRequest{})
		Expect(err).To(MatchError(ErrDocumentRestorePathInvalid))
		_, err = store.RestoreDocumentToPath(context.Background(), "page.md", "../bad.md", identity.CommitHashFromString("hash"), CommitRequest{})
		Expect(err).To(MatchError(ErrDocumentRestoreSourcePathInvalid))
		_, err = store.RestoreDocumentContentToPath(canceled, "page.md", "# Page\n", CommitRequest{})
		Expect(err).To(Equal(context.Canceled))
		_, err = store.RestoreDocumentContentToPath(context.Background(), "../bad.md", "# Page\n", CommitRequest{})
		Expect(err).To(MatchError(ErrDocumentRestorePathInvalid))

		errMissingCommit := errors.New("missing commit")
		restoreFilesAt := setGitRevisionSeam(&gitRevisionRepoCommitObject, func(*git.Repository, plumbing.Hash) (*object.Commit, error) {
			return nil, errMissingCommit
		})
		_, err = store.FilesAt(context.Background(), identity.CommitHashFromString("0000000000000000000000000000000000000000"))
		Expect(err).To(MatchError(errMissingCommit))
		_, err = store.GetCommit(context.Background(), identity.CommitHashFromString("0000000000000000000000000000000000000000"))
		Expect(err).To(MatchError(errMissingCommit))
		_, err = store.fileContentAt(context.Background(), identity.CommitHashFromString("0000000000000000000000000000000000000000"), "page.md")
		Expect(err).To(MatchError(errMissingCommit))
		restoreFilesAt()

		errMkdirRestoreFailed := errors.New("mkdir restore failed")
		restoreMkdir := setGitRevisionSeam(&gitRevisionMkdirAll, func(string, os.FileMode) error {
			return errMkdirRestoreFailed
		})
		_, err = store.RestoreDocumentContentToPath(context.Background(), "page.md", "# Page\n", CommitRequest{})
		Expect(err).To(MatchError(errMkdirRestoreFailed))
		restoreMkdir()

		errWriteRestoreFailed := errors.New("write restore failed")
		restoreWrite := setGitRevisionSeam(&gitRevisionWriteFile, func(string, []byte, os.FileMode) error {
			return errWriteRestoreFailed
		})
		_, err = store.RestoreDocumentContentToPath(context.Background(), "page.md", "# Page\n", CommitRequest{})
		Expect(err).To(MatchError(errWriteRestoreFailed))
		restoreWrite()
	})

	It("reports commit iteration, changed-entry, restore, and file-content seam errors", func() {
		store := &Store{rootDir: gitRevisionTempDir()}

		errLogFailed := errors.New("log failed")
		restoreLog := setGitRevisionSeam(&gitRevisionRepoLog, func(*git.Repository, *git.LogOptions) (object.CommitIter, error) {
			return nil, errLogFailed
		})
		Expect(store.ForEachCommit(context.Background(), func(Commit) (bool, error) { return true, nil })).To(MatchError(errLogFailed))
		_, err := store.ListCommits(context.Background(), ListRequest{})
		Expect(err).To(MatchError(errLogFailed))
		restoreLog()

		restoreLog = setGitRevisionSeam(&gitRevisionRepoLog, func(*git.Repository, *git.LogOptions) (object.CommitIter, error) {
			return fakeCommitIter{}, nil
		})
		errIterFailed := errors.New("iter failed")
		restoreCommitIter := setGitRevisionSeam(&gitRevisionCommitIterForEach, func(object.CommitIter, func(*object.Commit) error) error {
			return errIterFailed
		})
		Expect(store.ForEachCommit(context.Background(), func(Commit) (bool, error) { return true, nil })).To(MatchError(errIterFailed))
		restoreCommitIter()

		ctx, cancel := context.WithCancel(context.Background())
		restoreCommitIter = setGitRevisionSeam(&gitRevisionCommitIterForEach, func(_ object.CommitIter, visit func(*object.Commit) error) error {
			cancel()
			return visit(&object.Commit{})
		})
		err = store.ForEachCommit(ctx, func(Commit) (bool, error) { return true, nil })
		Expect(err).To(MatchError(context.Canceled))
		restoreCommitIter()

		errVisitFailed := errors.New("visit failed")
		restoreCommitIter = setGitRevisionSeam(&gitRevisionCommitIterForEach, func(_ object.CommitIter, visit func(*object.Commit) error) error {
			return visit(&object.Commit{})
		})
		Expect(store.ForEachCommit(context.Background(), func(Commit) (bool, error) {
			return false, errVisitFailed
		})).To(MatchError(errVisitFailed))
		restoreCommitIter()
		restoreLog()

		hash := identity.CommitHashFromString("1111111111111111111111111111111111111111")
		restoreCommitObject := setGitRevisionSeam(&gitRevisionRepoCommitObject, func(*git.Repository, plumbing.Hash) (*object.Commit, error) {
			return &object.Commit{}, nil
		})
		errTreeFailed := errors.New("tree failed")
		restoreCommitTree := setGitRevisionSeam(&gitRevisionCommitTree, func(*object.Commit) (*object.Tree, error) {
			return nil, errTreeFailed
		})
		_, _, err = store.changedMarkdownEntries(context.Background(), hash)
		Expect(err).To(MatchError(errTreeFailed))
		restoreCommitTree()

		restoreCommitTree = setGitRevisionSeam(&gitRevisionCommitTree, func(*object.Commit) (*object.Tree, error) {
			return &object.Tree{}, nil
		})
		restoreTreeFiles := setGitRevisionSeam(&gitRevisionTreeFiles, func(*object.Tree) *object.FileIter {
			return &object.FileIter{}
		})
		restoreFileIter := setGitRevisionSeam(&gitRevisionFileIterForEach, func(_ *object.FileIter, visit func(*object.File) error) error {
			Expect(visit(&object.File{Name: "image.png"})).To(Succeed())
			return visit(&object.File{Name: "page.md"})
		})
		restoreContents := setGitRevisionSeam(&gitRevisionFileContents, func(file *object.File) (string, error) {
			return "# " + file.Name + "\n", nil
		})
		paths, contents, err := store.changedMarkdownEntries(context.Background(), hash)
		Expect(err).NotTo(HaveOccurred())
		Expect(paths).To(Equal([]string{"page.md"}))
		Expect(contents).To(HaveKeyWithValue("page.md", "# page.md\n"))
		restoreContents()

		errContentsFailed := errors.New("contents failed")
		restoreContents = setGitRevisionSeam(&gitRevisionFileContents, func(*object.File) (string, error) {
			return "", errContentsFailed
		})
		_, _, err = store.changedMarkdownEntries(context.Background(), hash)
		Expect(err).To(MatchError(errContentsFailed))
		restoreContents()
		restoreFileIter()

		errRootIterFailed := errors.New("root iter failed")
		restoreFileIter = setGitRevisionSeam(&gitRevisionFileIterForEach, func(*object.FileIter, func(*object.File) error) error {
			return errRootIterFailed
		})
		_, _, err = store.changedMarkdownEntries(context.Background(), hash)
		Expect(err).To(MatchError(errRootIterFailed))
		restoreFileIter()
		restoreTreeFiles()
		restoreCommitTree()
		restoreCommitObject()

		dataDir := gitRevisionTempDir()
		rootDir := filepath.Join(gitRevisionTempDir(), "workspace")
		writeFile(filepath.Join(rootDir, "page.md"), "# Old\n")
		realStore, err := Open(StoreOptions{DataDir: dataDir, RootDir: rootDir})
		Expect(err).NotTo(HaveOccurred())
		_, err = realStore.Capture(context.Background(), CommitRequest{Actor: PublicEditorActor()})
		Expect(err).NotTo(HaveOccurred())
		writeFile(filepath.Join(rootDir, "page.md"), "# New\n")
		second, err := realStore.Capture(context.Background(), CommitRequest{Actor: PublicEditorActor()})
		Expect(err).NotTo(HaveOccurred())
		secondHash := identity.CommitHashFromString(second.Hash)

		originalCommitTree := gitRevisionCommitTree
		treeCalls := 0
		errParentTreeFailed := errors.New("parent tree failed")
		restoreCommitTree = setGitRevisionSeam(&gitRevisionCommitTree, func(commit *object.Commit) (*object.Tree, error) {
			treeCalls++
			if treeCalls == 2 {
				return nil, errParentTreeFailed
			}
			return originalCommitTree(commit)
		})
		_, _, err = realStore.changedMarkdownEntries(context.Background(), secondHash)
		Expect(err).To(MatchError(errParentTreeFailed))
		restoreCommitTree()

		errDiffFailed := errors.New("diff failed")
		restoreDiff := setGitRevisionSeam(&gitRevisionTreeDiffContext, func(*object.Tree, context.Context, *object.Tree) (object.Changes, error) {
			return nil, errDiffFailed
		})
		_, _, err = realStore.changedMarkdownEntries(context.Background(), secondHash)
		Expect(err).To(MatchError(errDiffFailed))
		restoreDiff()

		ctx, cancel = context.WithCancel(context.Background())
		restoreDiff = setGitRevisionSeam(&gitRevisionTreeDiffContext, func(*object.Tree, context.Context, *object.Tree) (object.Changes, error) {
			cancel()
			return object.Changes{&object.Change{}}, nil
		})
		_, _, err = realStore.changedMarkdownEntries(ctx, secondHash)
		Expect(err).To(Equal(context.Canceled))
		restoreDiff()

		restoreDiff = setGitRevisionSeam(&gitRevisionTreeDiffContext, func(*object.Tree, context.Context, *object.Tree) (object.Changes, error) {
			return object.Changes{&object.Change{}}, nil
		})
		errChangeFilesFailed := errors.New("change files failed")
		restoreChangeFiles := setGitRevisionSeam(&gitRevisionChangeFiles, func(*object.Change) (*object.File, *object.File, error) {
			return nil, nil, errChangeFilesFailed
		})
		_, _, err = realStore.changedMarkdownEntries(context.Background(), secondHash)
		Expect(err).To(MatchError(errChangeFilesFailed))
		restoreChangeFiles()

		restoreChangeFiles = setGitRevisionSeam(&gitRevisionChangeFiles, func(*object.Change) (*object.File, *object.File, error) {
			return nil, &object.File{Name: "image.png"}, nil
		})
		paths, contents, err = realStore.changedMarkdownEntries(context.Background(), secondHash)
		Expect(err).NotTo(HaveOccurred())
		Expect(paths).To(BeEmpty())
		Expect(contents).To(BeEmpty())
		restoreChangeFiles()

		restoreChangeFiles = setGitRevisionSeam(&gitRevisionChangeFiles, func(*object.Change) (*object.File, *object.File, error) {
			return nil, &object.File{Name: "page.md"}, nil
		})
		errToContentsFailed := errors.New("to contents failed")
		restoreContents = setGitRevisionSeam(&gitRevisionFileContents, func(*object.File) (string, error) {
			return "", errToContentsFailed
		})
		_, _, err = realStore.changedMarkdownEntries(context.Background(), secondHash)
		Expect(err).To(MatchError(errToContentsFailed))
		restoreContents()
		restoreChangeFiles()
		restoreDiff()

		errFilesAtFailed := errors.New("files at failed")
		restoreFilesAt := setGitRevisionSeam(&gitRevisionStoreFilesAt, func(*Store, context.Context, identity.CommitHash) (map[string]string, error) {
			return nil, errFilesAtFailed
		})
		_, err = store.RestoreWorkspace(context.Background(), hash, CommitRequest{})
		Expect(err).To(MatchError(errFilesAtFailed))
		restoreFilesAt()

		restoreFilesAt = setGitRevisionSeam(&gitRevisionStoreFilesAt, func(*Store, context.Context, identity.CommitHash) (map[string]string, error) {
			return map[string]string{"image.png": "png", "page.md": "# Page\n"}, nil
		})
		ctx, cancel = context.WithCancel(context.Background())
		cancel()
		_, err = store.RestoreWorkspace(ctx, hash, CommitRequest{})
		Expect(err).To(Equal(context.Canceled))

		errWorkspaceMkdirFailed := errors.New("workspace mkdir failed")
		restoreMkdir := setGitRevisionSeam(&gitRevisionMkdirAll, func(string, os.FileMode) error {
			return errWorkspaceMkdirFailed
		})
		_, err = store.RestoreWorkspace(context.Background(), hash, CommitRequest{})
		Expect(err).To(MatchError(errWorkspaceMkdirFailed))
		restoreMkdir()

		errWorkspaceWriteFailed := errors.New("workspace write failed")
		restoreWrite := setGitRevisionSeam(&gitRevisionWriteFile, func(string, []byte, os.FileMode) error {
			return errWorkspaceWriteFailed
		})
		_, err = store.RestoreWorkspace(context.Background(), hash, CommitRequest{})
		Expect(err).To(MatchError(errWorkspaceWriteFailed))
		restoreWrite()

		errCollectRestoreFailed := errors.New("collect restore failed")
		restoreCollect := setGitRevisionSeam(&gitRevisionCollectMarkdownPaths, func(string) ([]string, error) {
			return nil, errCollectRestoreFailed
		})
		_, err = store.RestoreWorkspace(context.Background(), hash, CommitRequest{})
		Expect(err).To(MatchError(errCollectRestoreFailed))
		restoreCollect()

		restoreFilesAt()
		restoreFilesAt = setGitRevisionSeam(&gitRevisionStoreFilesAt, func(*Store, context.Context, identity.CommitHash) (map[string]string, error) {
			return map[string]string{}, nil
		})
		restoreCollect = setGitRevisionSeam(&gitRevisionCollectMarkdownPaths, func(string) ([]string, error) {
			return []string{"gone.md"}, nil
		})
		errRemoveRestoreFailed := errors.New("remove restore failed")
		restoreRemove := setGitRevisionSeam(&gitRevisionRemove, func(string) error {
			return errRemoveRestoreFailed
		})
		_, err = store.RestoreWorkspace(context.Background(), hash, CommitRequest{})
		Expect(err).To(MatchError(errRemoveRestoreFailed))
		restoreRemove()

		restoreCollect()
		restoreCollect = setGitRevisionSeam(&gitRevisionCollectMarkdownPaths, func(string) ([]string, error) {
			return nil, nil
		})
		restoredWorkspaceHash := identity.CommitHashFromString("2222222222222222222222222222222222222222")
		restoreCapture := setGitRevisionSeam(&gitRevisionStoreCapture, func(_ *Store, _ context.Context, req CommitRequest) (*Commit, error) {
			Expect(req).To(gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
				"Reason": Equal(ReasonRestore),
				"Source": Equal(SourceSystem),
			}))
			return &Commit{Created: true, Hash: restoredWorkspaceHash}, nil
		})
		commit, err := store.RestoreWorkspace(context.Background(), hash, CommitRequest{})
		Expect(err).NotTo(HaveOccurred())
		Expect(commit).To(matchCreatedRevisionCommit(restoredWorkspaceHash))
		restoreCapture()
		restoreCollect()
		restoreFilesAt()

		errContentFailed := errors.New("content failed")
		restoreFileContent := setGitRevisionSeam(&gitRevisionStoreFileContentAt, func(*Store, context.Context, identity.CommitHash, string) (string, error) {
			return "", errContentFailed
		})
		_, err = store.RestoreDocumentToPath(context.Background(), "page.md", "page.md", hash, CommitRequest{})
		Expect(err).To(MatchError(errContentFailed))
		restoreFileContent()

		restoreWrite = setGitRevisionSeam(&gitRevisionWriteFile, func(string, []byte, os.FileMode) error {
			return nil
		})
		restoredDocumentHash := identity.CommitHashFromString("3333333333333333333333333333333333333333")
		restoreCapture = setGitRevisionSeam(&gitRevisionStoreCapture, func(_ *Store, _ context.Context, req CommitRequest) (*Commit, error) {
			Expect(req).To(gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
				"Reason": Equal(ReasonRestore),
				"Source": Equal(SourceSystem),
			}))
			return &Commit{Created: true, Hash: restoredDocumentHash}, nil
		})
		commit, err = store.RestoreDocumentContentToPath(context.Background(), "page.md", "# Page\n", CommitRequest{})
		Expect(err).NotTo(HaveOccurred())
		Expect(commit).To(matchCreatedRevisionCommit(restoredDocumentHash))
		restoreCapture()
		restoreWrite()

		restoreCommitObject = setGitRevisionSeam(&gitRevisionRepoCommitObject, func(*git.Repository, plumbing.Hash) (*object.Commit, error) {
			return &object.Commit{}, nil
		})
		errFileTreeFailed := errors.New("file tree failed")
		restoreCommitTree = setGitRevisionSeam(&gitRevisionCommitTree, func(*object.Commit) (*object.Tree, error) {
			return nil, errFileTreeFailed
		})
		_, err = store.fileContentAt(context.Background(), hash, "page.md")
		Expect(err).To(MatchError(errFileTreeFailed))
		_, err = store.filesAtCommit(context.Background(), &object.Commit{})
		Expect(err).To(MatchError(errFileTreeFailed))
		restoreCommitTree()

		restoreCommitTree = setGitRevisionSeam(&gitRevisionCommitTree, func(*object.Commit) (*object.Tree, error) {
			return &object.Tree{}, nil
		})
		errFileMissing := errors.New("file missing")
		restoreTreeFile := setGitRevisionSeam(&gitRevisionTreeFile, func(*object.Tree, string) (*object.File, error) {
			return nil, errFileMissing
		})
		_, err = store.fileContentAt(context.Background(), hash, "page.md")
		Expect(err).To(MatchError(errFileMissing))
		restoreTreeFile()

		restoreTreeFile = setGitRevisionSeam(&gitRevisionTreeFile, func(*object.Tree, string) (*object.File, error) {
			return &object.File{Name: "page.md"}, nil
		})
		errFileContentsFailed := errors.New("file contents failed")
		restoreContents = setGitRevisionSeam(&gitRevisionFileContents, func(*object.File) (string, error) {
			return "", errFileContentsFailed
		})
		_, err = store.fileContentAt(context.Background(), hash, "page.md")
		Expect(err).To(MatchError(errFileContentsFailed))
		restoreContents()
		restoreTreeFile()

		restoreTreeFiles = setGitRevisionSeam(&gitRevisionTreeFiles, func(*object.Tree) *object.FileIter {
			return &object.FileIter{}
		})
		restoreFileIter = setGitRevisionSeam(&gitRevisionFileIterForEach, func(_ *object.FileIter, visit func(*object.File) error) error {
			Expect(visit(&object.File{Name: "image.png"})).To(Succeed())
			return visit(&object.File{Name: "page.md"})
		})
		errFileReaderFailed := errors.New("file reader failed")
		restoreFileReader := setGitRevisionSeam(&gitRevisionFileReader, func(*object.File) (io.ReadCloser, error) {
			return nil, errFileReaderFailed
		})
		_, err = store.filesAtCommit(context.Background(), &object.Commit{})
		Expect(err).To(MatchError(errFileReaderFailed))
		restoreFileReader()

		restoreFileReader = setGitRevisionSeam(&gitRevisionFileReader, func(*object.File) (io.ReadCloser, error) {
			return io.NopCloser(strings.NewReader("# Page\n")), nil
		})
		errFileReadAllFailed := errors.New("file read all failed")
		restoreReadAll := setGitRevisionSeam(&gitRevisionReadAll, func(io.Reader) ([]byte, error) {
			return nil, errFileReadAllFailed
		})
		_, err = store.filesAtCommit(context.Background(), &object.Commit{})
		Expect(err).To(MatchError(errFileReadAllFailed))
		restoreReadAll()
		restoreFileReader()

		ctx, cancel = context.WithCancel(context.Background())
		restoreFileIter()
		restoreFileIter = setGitRevisionSeam(&gitRevisionFileIterForEach, func(_ *object.FileIter, visit func(*object.File) error) error {
			cancel()
			return visit(&object.File{Name: "page.md"})
		})
		_, err = store.filesAtCommit(ctx, &object.Commit{})
		Expect(err).To(MatchError(context.Canceled))
		restoreFileIter()

		errFilesIterFailed := errors.New("files iter failed")
		restoreFileIter = setGitRevisionSeam(&gitRevisionFileIterForEach, func(*object.FileIter, func(*object.File) error) error {
			return errFilesIterFailed
		})
		_, err = store.filesAtCommit(context.Background(), &object.Commit{})
		Expect(err).To(MatchError(errFilesIterFailed))
		restoreFileIter()
		restoreTreeFiles()
		restoreCommitTree()
		restoreCommitObject()
	})

	It("reports parent lookup and file-content cancellation failures", func() {
		store := &Store{}
		hash := identity.CommitHashFromString("1111111111111111111111111111111111111111")

		errLoadChangedCommitFailed := errors.New("load changed commit failed")
		restoreCommitObject := setGitRevisionSeam(&gitRevisionRepoCommitObject, func(*git.Repository, plumbing.Hash) (*object.Commit, error) {
			return nil, errLoadChangedCommitFailed
		})
		_, _, err := store.changedMarkdownEntries(context.Background(), hash)
		Expect(err).To(MatchError(errLoadChangedCommitFailed))
		restoreCommitObject()

		restoreCommitObject = setGitRevisionSeam(&gitRevisionRepoCommitObject, func(*git.Repository, plumbing.Hash) (*object.Commit, error) {
			return &object.Commit{}, nil
		})
		restoreCommitTree := setGitRevisionSeam(&gitRevisionCommitTree, func(*object.Commit) (*object.Tree, error) {
			return &object.Tree{}, nil
		})
		restoreCommitIterNext := setGitRevisionSeam(&gitRevisionCommitIterNext, func(object.CommitIter) (*object.Commit, error) {
			return nil, object.ErrParentNotFound
		})
		restoreTreeFiles := setGitRevisionSeam(&gitRevisionTreeFiles, func(*object.Tree) *object.FileIter {
			return &object.FileIter{}
		})
		ctx, cancel := context.WithCancel(context.Background())
		restoreFileIter := setGitRevisionSeam(&gitRevisionFileIterForEach, func(_ *object.FileIter, visit func(*object.File) error) error {
			cancel()
			return visit(&object.File{Name: "page.md"})
		})
		_, _, err = store.changedMarkdownEntries(ctx, hash)
		Expect(err).To(MatchError(context.Canceled))
		restoreFileIter()
		restoreTreeFiles()
		restoreCommitIterNext()

		errParentFailed := errors.New("parent failed")
		restoreCommitIterNext = setGitRevisionSeam(&gitRevisionCommitIterNext, func(object.CommitIter) (*object.Commit, error) {
			return nil, errParentFailed
		})
		_, _, err = store.changedMarkdownEntries(context.Background(), hash)
		Expect(err).To(MatchError(errParentFailed))
		restoreCommitIterNext()
		restoreCommitTree()
		restoreCommitObject()

		canceled, cancel := context.WithCancel(context.Background())
		cancel()
		_, err = store.fileContentAt(canceled, hash, "page.md")
		Expect(err).To(Equal(context.Canceled))
	})
})

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
	return nil, nil
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

func containsAll(haystack string, needles ...string) bool {
	for _, needle := range needles {
		if !strings.Contains(haystack, needle) {
			return false
		}
	}
	return true
}
