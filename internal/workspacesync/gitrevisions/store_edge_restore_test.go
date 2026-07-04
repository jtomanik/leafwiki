package gitrevisions

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"

	git "github.com/go-git/go-git/v6"
	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/object"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/gstruct"

	"github.com/perber/wiki/internal/core/identity"
)

var _ = Describe("git revision edge behavior", func() {
	It("reports commit iteration, changed-entry, restore, and file-content seam errors", Label("integration"), func() {
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

	It("reports parent lookup and file-content cancellation failures", Label("unit"), func() {
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
