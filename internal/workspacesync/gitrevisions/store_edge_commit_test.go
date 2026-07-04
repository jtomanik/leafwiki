package gitrevisions

import (
	"context"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	git "github.com/go-git/go-git/v6"
	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/format/index"
	"github.com/go-git/go-git/v6/plumbing/object"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/perber/wiki/internal/core/identity"
)

var _ = Describe("git revision edge behavior", func() {
	It("reports commit fallback and dependency errors through seams", Label("unit"), func() {
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
			return &git.Worktree{}, nil
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
			return plumbing.NewHashReference(plumbing.HEAD, plumbing.ZeroHash), nil
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

	It("reports index and tracked-file store errors", Label("unit"), func() {
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

	It("reports staging errors through markdown change seams", Label("unit"), func() {
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

	It("reports markdown path collection walk and relative-path errors", Label("unit"), func() {
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

	It("reports canceled list, get, restore, and file read operations", Label("unit"), func() {
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

})
