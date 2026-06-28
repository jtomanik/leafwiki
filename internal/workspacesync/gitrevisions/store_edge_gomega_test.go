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

	"github.com/perber/wiki/internal/core/identity"
)

var _ = Describe("git revision edge coverage", func() {
	It("covers helper defaults and parsing branches", func() {
		Expect(nonFilesystemInitStorage{}.Init()).To(Succeed())

		target, ok := parseGitDirFile("not-a-gitdir", "/workspace")
		Expect(ok).To(BeFalse())
		Expect(target).To(BeEmpty())

		target, ok = parseGitDirFile("gitdir:   \n", "/workspace")
		Expect(ok).To(BeFalse())
		Expect(target).To(BeEmpty())

		target, ok = parseGitDirFile("gitdir: /tmp/repo/.git\n", "/workspace")
		Expect(ok).To(BeTrue())
		Expect(target).To(Equal(filepath.Clean("/tmp/repo/.git")))

		Expect(mergeMarkdownPaths([]string{" a.md ", "", "nested/b.md"}, []string{"a.md"})).To(Equal([]string{"a.md", "nested/b.md"}))
		Expect(commitMessage(CommitRequest{}, "batch-1", nil)).To(ContainSubstring("LeafWiki-Source: unknown"))
		Expect(commitMessage(CommitRequest{Reason: ReasonStartup}, "batch-1", nil)).To(HavePrefix("LeafWiki initial workspace snapshot"))
		Expect(commitMessage(CommitRequest{Reason: ReasonRestore}, "batch-1", nil)).To(HavePrefix("LeafWiki workspace restore"))
		Expect(commitActorIDs(CommitRequest{AdditionalActors: []Actor{{}, {ID: "public-editor"}}})).To(Equal([]ActorID{"public-editor"}))

		restoreRand := setGitRevisionSeam(&gitRevisionRandRead, func([]byte) (int, error) {
			return 0, errors.New("rand failed")
		})
		Expect(newBatchID()).NotTo(BeEmpty())
		restoreRand()

		sig := signature(Actor{})
		Expect(sig.Name).To(Equal("Public Editor"))
		Expect(sig.Email).To(Equal("public-editor@leafwiki.local"))

		sig = signature(Actor{ID: "agent-1"})
		Expect(sig.Name).To(Equal("agent-1"))
		Expect(sig.Email).To(Equal("agent-1@leafwiki.local"))

		title, trailers, actors := parseCommitMessage("Title\nnot-a-trailer\nOther: value\nLeafWiki-Actor: alice\n")
		Expect(title).To(Equal("Title"))
		Expect(trailers["LeafWiki-Actor"]).To(Equal("alice"))
		Expect(actors).To(Equal([]ActorID{"alice"}))
	})

	It("covers Open validation and dependency failures", func() {
		_, err := Open(StoreOptions{})
		Expect(err).To(MatchError("data dir is required"))

		_, err = Open(StoreOptions{DataDir: "data"})
		Expect(err).To(MatchError("root dir is required"))

		restoreMkdir := setGitRevisionSeam(&gitRevisionMkdirAll, func(string, os.FileMode) error {
			return errors.New("mkdir internal failed")
		})
		_, err = Open(StoreOptions{DataDir: GinkgoT().TempDir(), RootDir: filepath.Join(GinkgoT().TempDir(), "root")})
		Expect(err).To(MatchError(ContainSubstring("create internal git dir")))
		restoreMkdir()

		mkdirCalls := 0
		restoreMkdir = setGitRevisionSeam(&gitRevisionMkdirAll, func(string, os.FileMode) error {
			mkdirCalls++
			if mkdirCalls == 2 {
				return errors.New("mkdir root failed")
			}
			return nil
		})
		_, err = Open(StoreOptions{DataDir: GinkgoT().TempDir(), RootDir: filepath.Join(GinkgoT().TempDir(), "root")})
		Expect(err).To(MatchError(ContainSubstring("create root dir")))
		restoreMkdir()

		restoreOpen := setGitRevisionSeam(&gitRevisionGitOpen, func(gitstorage.Storer, billy.Filesystem) (*git.Repository, error) {
			return nil, git.ErrRepositoryNotExists
		})
		restoreInit := setGitRevisionSeam(&gitRevisionGitInit, func(gitstorage.Storer, ...git.InitOption) (*git.Repository, error) {
			return nil, errors.New("init failed")
		})
		_, err = Open(StoreOptions{DataDir: GinkgoT().TempDir(), RootDir: filepath.Join(GinkgoT().TempDir(), "root")})
		Expect(err).To(MatchError(ContainSubstring("open internal git repository")))
		restoreInit()
		restoreOpen()

		openCalls := 0
		restoreOpen = setGitRevisionSeam(&gitRevisionGitOpen, func(gitstorage.Storer, billy.Filesystem) (*git.Repository, error) {
			openCalls++
			if openCalls == 1 {
				return nil, git.ErrRepositoryNotExists
			}
			return nil, errors.New("second open failed")
		})
		restoreInit = setGitRevisionSeam(&gitRevisionGitInit, func(gitstorage.Storer, ...git.InitOption) (*git.Repository, error) {
			return nil, nil
		})
		_, err = Open(StoreOptions{DataDir: GinkgoT().TempDir(), RootDir: filepath.Join(GinkgoT().TempDir(), "root")})
		Expect(err).To(MatchError(ContainSubstring("open internal git repository")))
		restoreInit()
		restoreOpen()

		restoreCleanup := setGitRevisionSeam(&gitRevisionRemoveInternalRootGitFile, func(string, string) error {
			return errors.New("remove root .git file: remove .git failed")
		})
		_, err = Open(StoreOptions{DataDir: GinkgoT().TempDir(), RootDir: filepath.Join(GinkgoT().TempDir(), "root")})
		Expect(err).To(MatchError(ContainSubstring("remove root .git file")))
		restoreCleanup()
	})

	It("covers root .git cleanup branches", func() {
		restoreLstat := setGitRevisionSeam(&gitRevisionLstat, func(string) (os.FileInfo, error) {
			return nil, errors.New("stat failed")
		})
		Expect(removeInternalRootGitFile(GinkgoT().TempDir(), "/internal/git")).To(MatchError(ContainSubstring("stat root .git")))
		restoreLstat()

		rootDir := GinkgoT().TempDir()
		internal := filepath.Join(GinkgoT().TempDir(), ".leafwiki", "git")
		Expect(os.MkdirAll(filepath.Join(rootDir, ".git"), 0o755)).To(Succeed())
		Expect(removeInternalRootGitFile(rootDir, internal)).To(Succeed())

		rootDir = GinkgoT().TempDir()
		Expect(os.WriteFile(filepath.Join(rootDir, ".git"), []byte("gitdir: ../elsewhere\n"), 0o644)).To(Succeed())
		Expect(removeInternalRootGitFile(rootDir, internal)).To(Succeed())
		Expect(os.ReadFile(filepath.Join(rootDir, ".git"))).To(Equal([]byte("gitdir: ../elsewhere\n")))

		rootDir = GinkgoT().TempDir()
		Expect(os.MkdirAll(internal, 0o755)).To(Succeed())
		Expect(os.WriteFile(filepath.Join(rootDir, ".git"), []byte("gitdir: "+internal+"\n"), 0o644)).To(Succeed())
		Expect(removeInternalRootGitFile(rootDir, internal)).To(Succeed())
		_, err := os.Stat(filepath.Join(rootDir, ".git"))
		Expect(os.IsNotExist(err)).To(BeTrue())

		rootDir = GinkgoT().TempDir()
		Expect(os.WriteFile(filepath.Join(rootDir, ".git"), []byte("gitdir: "+internal+"\n"), 0o644)).To(Succeed())
		restoreRead := setGitRevisionSeam(&gitRevisionReadFile, func(string) ([]byte, error) {
			return nil, errors.New("read failed")
		})
		Expect(removeInternalRootGitFile(rootDir, internal)).To(MatchError(ContainSubstring("read root .git file")))
		restoreRead()

		rootDir = GinkgoT().TempDir()
		Expect(os.WriteFile(filepath.Join(rootDir, ".git"), []byte("gitdir: "+internal+"\n"), 0o644)).To(Succeed())
		restoreRemove := setGitRevisionSeam(&gitRevisionRemove, func(string) error {
			return errors.New("remove failed")
		})
		Expect(removeInternalRootGitFile(rootDir, internal)).To(MatchError(ContainSubstring("remove root .git file")))
		restoreRemove()
	})

	It("covers commit fallback and error branches through seams", func() {
		store := &Store{}
		canceled, cancel := context.WithCancel(context.Background())
		cancel()
		_, err := store.Capture(canceled, CommitRequest{})
		Expect(err).To(Equal(context.Canceled))

		restoreWorktree := setGitRevisionSeam(&gitRevisionRepoWorktree, func(*git.Repository) (*git.Worktree, error) {
			return nil, errors.New("worktree failed")
		})
		_, err = store.Capture(context.Background(), CommitRequest{})
		Expect(err).To(MatchError("worktree failed"))
		restoreWorktree()

		restoreWorktree = setGitRevisionSeam(&gitRevisionRepoWorktree, func(*git.Repository) (*git.Worktree, error) {
			return nil, nil
		})
		restoreStage := setGitRevisionSeam(&gitRevisionStoreStageMarkdownChanges, func(*Store, context.Context, *git.Worktree) ([]string, error) {
			return nil, errors.New("stage failed")
		})
		_, err = store.Capture(context.Background(), CommitRequest{})
		Expect(err).To(MatchError("stage failed"))
		restoreStage()
		restoreStage = setGitRevisionSeam(&gitRevisionStoreStageMarkdownChanges, func(*Store, context.Context, *git.Worktree) ([]string, error) {
			return nil, nil
		})

		hash := plumbing.NewHash("1111111111111111111111111111111111111111")
		restoreCommit := setGitRevisionSeam(&gitRevisionWorktreeCommit, func(*git.Worktree, string, *git.CommitOptions) (plumbing.Hash, error) {
			return plumbing.ZeroHash, errors.New("commit failed")
		})
		_, err = store.Capture(context.Background(), CommitRequest{})
		Expect(err).To(MatchError(ContainSubstring("commit markdown snapshot")))
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
		Expect(commit.Created).To(BeTrue())
		Expect(commit.Hash).To(Equal(hash.String()))
		restoreHead()
		restoreCommit()

		commitCalls = 0
		restoreCommit = setGitRevisionSeam(&gitRevisionWorktreeCommit, func(*git.Worktree, string, *git.CommitOptions) (plumbing.Hash, error) {
			commitCalls++
			if commitCalls == 1 {
				return plumbing.ZeroHash, git.ErrEmptyCommit
			}
			return plumbing.ZeroHash, errors.New("empty restore failed")
		})
		restoreHead = setGitRevisionSeam(&gitRevisionRepoHead, func(*git.Repository) (*plumbing.Reference, error) {
			return nil, nil
		})
		_, err = store.Capture(context.Background(), CommitRequest{Reason: ReasonRestore})
		Expect(err).To(MatchError(ContainSubstring("commit empty restore snapshot")))
		restoreHead()
		restoreCommit()

		commitCalls = 0
		restoreCommit = setGitRevisionSeam(&gitRevisionWorktreeCommit, func(*git.Worktree, string, *git.CommitOptions) (plumbing.Hash, error) {
			commitCalls++
			if commitCalls == 1 {
				return plumbing.ZeroHash, git.ErrEmptyCommit
			}
			return plumbing.ZeroHash, errors.New("empty initial failed")
		})
		restoreHead = setGitRevisionSeam(&gitRevisionRepoHead, func(*git.Repository) (*plumbing.Reference, error) {
			return nil, plumbing.ErrReferenceNotFound
		})
		_, err = store.Capture(context.Background(), CommitRequest{})
		Expect(err).To(MatchError(ContainSubstring("commit empty initial markdown snapshot")))
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
		Expect(commit.Created).To(BeFalse())
		Expect(commit.Hash).To(Equal(hash.String()))
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

	It("covers index and tracked-file store errors", func() {
		store := &Store{}
		restoreIndex := setGitRevisionSeam(&gitRevisionRepositoryIndex, func(*git.Repository) (*index.Index, error) {
			return nil, errors.New("index failed")
		})
		Expect(store.removeFromIndexOnly("old.md")).To(MatchError("index failed"))
		restoreIndex()

		restoreIndex = setGitRevisionSeam(&gitRevisionRepositoryIndex, func(*git.Repository) (*index.Index, error) {
			return &index.Index{}, nil
		})
		restoreIndexRemove := setGitRevisionSeam(&gitRevisionIndexRemove, func(*index.Index, string) (*index.Entry, error) {
			return nil, errors.New("index remove failed")
		})
		Expect(store.removeFromIndexOnly("old.md")).To(MatchError("index remove failed"))
		restoreIndexRemove()

		restoreIndexRemove = setGitRevisionSeam(&gitRevisionIndexRemove, func(*index.Index, string) (*index.Entry, error) {
			return &index.Entry{}, nil
		})
		restoreSetIndex := setGitRevisionSeam(&gitRevisionRepositorySetIndex, func(*git.Repository, *index.Index) error {
			return errors.New("set index failed")
		})
		Expect(store.removeFromIndexOnly("old.md")).To(MatchError("set index failed"))
		restoreSetIndex()
		restoreIndexRemove()
		restoreIndex()

		head := plumbing.NewHashReference(plumbing.HEAD, plumbing.NewHash("1111111111111111111111111111111111111111"))
		restoreHead := setGitRevisionSeam(&gitRevisionRepoHead, func(*git.Repository) (*plumbing.Reference, error) {
			return nil, errors.New("head failed")
		})
		_, err := store.trackedMarkdownFiles()
		Expect(err).To(MatchError(ContainSubstring("read internal git HEAD")))
		restoreHead()

		restoreHead = setGitRevisionSeam(&gitRevisionRepoHead, func(*git.Repository) (*plumbing.Reference, error) {
			return head, nil
		})
		restoreCommitObject := setGitRevisionSeam(&gitRevisionRepoCommitObject, func(*git.Repository, plumbing.Hash) (*object.Commit, error) {
			return nil, errors.New("commit failed")
		})
		_, err = store.trackedMarkdownFiles()
		Expect(err).To(MatchError(ContainSubstring("load HEAD commit")))
		restoreCommitObject()

		restoreCommitObject = setGitRevisionSeam(&gitRevisionRepoCommitObject, func(*git.Repository, plumbing.Hash) (*object.Commit, error) {
			return &object.Commit{}, nil
		})
		restoreCommitTree := setGitRevisionSeam(&gitRevisionCommitTree, func(*object.Commit) (*object.Tree, error) {
			return nil, errors.New("tree failed")
		})
		_, err = store.trackedMarkdownFiles()
		Expect(err).To(MatchError(ContainSubstring("load HEAD tree")))
		restoreCommitTree()

		restoreCommitTree = setGitRevisionSeam(&gitRevisionCommitTree, func(*object.Commit) (*object.Tree, error) {
			return &object.Tree{}, nil
		})
		restoreTreeFiles := setGitRevisionSeam(&gitRevisionTreeFiles, func(*object.Tree) *object.FileIter {
			return &object.FileIter{}
		})
		restoreFileIter := setGitRevisionSeam(&gitRevisionFileIterForEach, func(*object.FileIter, func(*object.File) error) error {
			return errors.New("iter failed")
		})
		_, err = store.trackedMarkdownFiles()
		Expect(err).To(MatchError(ContainSubstring("list tracked markdown")))
		restoreFileIter()

		restoreFileIter = setGitRevisionSeam(&gitRevisionFileIterForEach, func(_ *object.FileIter, visit func(*object.File) error) error {
			return visit(&object.File{Name: "page.md"})
		})
		restoreFileReader := setGitRevisionSeam(&gitRevisionFileReader, func(*object.File) (io.ReadCloser, error) {
			return nil, errors.New("reader failed")
		})
		_, err = store.trackedMarkdownFiles()
		Expect(err).To(MatchError(ContainSubstring("list tracked markdown")))
		restoreFileReader()

		restoreFileReader = setGitRevisionSeam(&gitRevisionFileReader, func(*object.File) (io.ReadCloser, error) {
			return io.NopCloser(strings.NewReader("# Page\n")), nil
		})
		restoreReadAll := setGitRevisionSeam(&gitRevisionReadAll, func(io.Reader) ([]byte, error) {
			return nil, errors.New("read all failed")
		})
		_, err = store.trackedMarkdownFiles()
		Expect(err).To(MatchError(ContainSubstring("list tracked markdown")))
		restoreReadAll()
		restoreFileReader()
		restoreFileIter()
		restoreTreeFiles()
		restoreCommitTree()
		restoreCommitObject()
		restoreHead()
	})

	It("covers stageMarkdownChanges error branches through seams", func() {
		store := &Store{rootDir: "/workspace"}

		restoreCollect := setGitRevisionSeam(&gitRevisionCollectMarkdownPaths, func(string) ([]string, error) {
			return nil, errors.New("collect failed")
		})
		_, err := store.stageMarkdownChanges(context.Background(), nil)
		Expect(err).To(MatchError("collect failed"))
		restoreCollect()

		restoreCollect = setGitRevisionSeam(&gitRevisionCollectMarkdownPaths, func(string) ([]string, error) {
			return []string{"page.md"}, nil
		})
		restoreTracked := setGitRevisionSeam(&gitRevisionStoreTrackedMarkdownFiles, func(*Store) (map[string]string, error) {
			return nil, errors.New("tracked failed")
		})
		_, err = store.stageMarkdownChanges(context.Background(), nil)
		Expect(err).To(MatchError("tracked failed"))
		restoreTracked()

		restoreTracked = setGitRevisionSeam(&gitRevisionStoreTrackedMarkdownFiles, func(*Store) (map[string]string, error) {
			return map[string]string{}, nil
		})
		canceled, cancel := context.WithCancel(context.Background())
		cancel()
		_, err = store.stageMarkdownChanges(canceled, nil)
		Expect(err).To(Equal(context.Canceled))

		restoreRead := setGitRevisionSeam(&gitRevisionReadFile, func(string) ([]byte, error) {
			return nil, errors.New("read failed")
		})
		_, err = store.stageMarkdownChanges(context.Background(), nil)
		Expect(err).To(MatchError(ContainSubstring("read markdown page.md")))
		restoreRead()

		restoreRead = setGitRevisionSeam(&gitRevisionReadFile, func(string) ([]byte, error) {
			return []byte("# Page\n"), nil
		})
		restoreAdd := setGitRevisionSeam(&gitRevisionWorktreeAdd, func(*git.Worktree, string) (plumbing.Hash, error) {
			return plumbing.ZeroHash, errors.New("add failed")
		})
		_, err = store.stageMarkdownChanges(context.Background(), nil)
		Expect(err).To(MatchError(ContainSubstring("stage markdown page.md")))
		restoreAdd()

		restoreCollect()
		restoreCollect = setGitRevisionSeam(&gitRevisionCollectMarkdownPaths, func(string) ([]string, error) {
			return nil, nil
		})
		restoreTracked()
		restoreTracked = setGitRevisionSeam(&gitRevisionStoreTrackedMarkdownFiles, func(*Store) (map[string]string, error) {
			return map[string]string{"old.md": "# Old\n"}, nil
		})
		restoreRemove := setGitRevisionSeam(&gitRevisionWorktreeRemove, func(*git.Worktree, string) (plumbing.Hash, error) {
			return plumbing.ZeroHash, errors.New("remove failed")
		})
		_, err = store.stageMarkdownChanges(context.Background(), nil)
		Expect(err).To(MatchError(ContainSubstring("stage markdown delete old.md")))
		restoreRemove()

		restoreTracked()
		restoreTracked = setGitRevisionSeam(&gitRevisionStoreTrackedMarkdownFiles, func(*Store) (map[string]string, error) {
			return map[string]string{".obsidian/local.md": "# Local\n"}, nil
		})
		restoreRemoveIndex := setGitRevisionSeam(&gitRevisionStoreRemoveFromIndexOnly, func(*Store, string) error {
			return errors.New("remove index failed")
		})
		_, err = store.stageMarkdownChanges(context.Background(), nil)
		Expect(err).To(MatchError(ContainSubstring("stage unmanaged markdown delete .obsidian/local.md")))
		restoreRemoveIndex()
		restoreTracked()
		restoreCollect()
		restoreRead()
	})

	It("covers collectMarkdownPaths walk and rel errors", func() {
		walkErr := errors.New("walk failed")
		restoreWalk := setGitRevisionSeam(&gitRevisionWalkDir, func(root string, fn fs.WalkDirFunc) error {
			return fn(filepath.Join(root, "bad.md"), fakeDirEntry{name: "bad.md"}, walkErr)
		})
		_, err := collectMarkdownPaths("/workspace")
		Expect(err).To(MatchError(ContainSubstring("collect markdown paths")))
		restoreWalk()

		restoreWalk = setGitRevisionSeam(&gitRevisionWalkDir, func(root string, fn fs.WalkDirFunc) error {
			Expect(fn(root, fakeDirEntry{name: filepath.Base(root), dir: true}, nil)).To(Succeed())
			return fn(filepath.Join(root, "page.md"), fakeDirEntry{name: "page.md", typ: 0}, nil)
		})
		restoreRel := setGitRevisionSeam(&gitRevisionRel, func(string, string) (string, error) {
			return "", errors.New("rel failed")
		})
		_, err = collectMarkdownPaths("/workspace")
		Expect(err).To(MatchError(ContainSubstring("collect markdown paths")))
		restoreRel()
		restoreWalk()
	})

	It("covers list, get, restore, and file read validation branches", func() {
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
		Expect(err).To(MatchError(ContainSubstring("document restore path is invalid")))
		_, err = store.RestoreDocumentToPath(context.Background(), "page.md", "../bad.md", identity.CommitHashFromString("hash"), CommitRequest{})
		Expect(err).To(MatchError(ContainSubstring("document restore source path is invalid")))
		_, err = store.RestoreDocumentContentToPath(canceled, "page.md", "# Page\n", CommitRequest{})
		Expect(err).To(Equal(context.Canceled))
		_, err = store.RestoreDocumentContentToPath(context.Background(), "../bad.md", "# Page\n", CommitRequest{})
		Expect(err).To(MatchError(ContainSubstring("document restore path is invalid")))

		restoreFilesAt := setGitRevisionSeam(&gitRevisionRepoCommitObject, func(*git.Repository, plumbing.Hash) (*object.Commit, error) {
			return nil, errors.New("missing commit")
		})
		_, err = store.FilesAt(context.Background(), identity.CommitHashFromString("0000000000000000000000000000000000000000"))
		Expect(err).To(MatchError(ContainSubstring("load commit")))
		_, err = store.GetCommit(context.Background(), identity.CommitHashFromString("0000000000000000000000000000000000000000"))
		Expect(err).To(MatchError(ContainSubstring("load commit")))
		_, err = store.fileContentAt(context.Background(), identity.CommitHashFromString("0000000000000000000000000000000000000000"), "page.md")
		Expect(err).To(MatchError(ContainSubstring("load commit")))
		restoreFilesAt()

		restoreMkdir := setGitRevisionSeam(&gitRevisionMkdirAll, func(string, os.FileMode) error {
			return errors.New("mkdir restore failed")
		})
		_, err = store.RestoreDocumentContentToPath(context.Background(), "page.md", "# Page\n", CommitRequest{})
		Expect(err).To(MatchError(ContainSubstring("create restore parent page.md")))
		restoreMkdir()

		restoreWrite := setGitRevisionSeam(&gitRevisionWriteFile, func(string, []byte, os.FileMode) error {
			return errors.New("write restore failed")
		})
		_, err = store.RestoreDocumentContentToPath(context.Background(), "page.md", "# Page\n", CommitRequest{})
		Expect(err).To(MatchError(ContainSubstring("write restored markdown page.md")))
		restoreWrite()
	})

	It("covers commit iteration, changed entries, restore, and file content seams", func() {
		store := &Store{rootDir: GinkgoT().TempDir()}

		restoreLog := setGitRevisionSeam(&gitRevisionRepoLog, func(*git.Repository, *git.LogOptions) (object.CommitIter, error) {
			return nil, errors.New("log failed")
		})
		Expect(store.ForEachCommit(context.Background(), func(Commit) (bool, error) { return true, nil })).To(MatchError(ContainSubstring("list commits")))
		_, err := store.ListCommits(context.Background(), ListRequest{})
		Expect(err).To(MatchError(ContainSubstring("list commits")))
		restoreLog()

		restoreLog = setGitRevisionSeam(&gitRevisionRepoLog, func(*git.Repository, *git.LogOptions) (object.CommitIter, error) {
			return fakeCommitIter{}, nil
		})
		restoreCommitIter := setGitRevisionSeam(&gitRevisionCommitIterForEach, func(object.CommitIter, func(*object.Commit) error) error {
			return errors.New("iter failed")
		})
		Expect(store.ForEachCommit(context.Background(), func(Commit) (bool, error) { return true, nil })).To(MatchError(ContainSubstring("iterate commits")))
		restoreCommitIter()

		ctx, cancel := context.WithCancel(context.Background())
		restoreCommitIter = setGitRevisionSeam(&gitRevisionCommitIterForEach, func(_ object.CommitIter, visit func(*object.Commit) error) error {
			cancel()
			return visit(&object.Commit{})
		})
		err = store.ForEachCommit(ctx, func(Commit) (bool, error) { return true, nil })
		Expect(errors.Is(err, context.Canceled)).To(BeTrue())
		restoreCommitIter()

		restoreCommitIter = setGitRevisionSeam(&gitRevisionCommitIterForEach, func(_ object.CommitIter, visit func(*object.Commit) error) error {
			return visit(&object.Commit{})
		})
		Expect(store.ForEachCommit(context.Background(), func(Commit) (bool, error) {
			return false, errors.New("visit failed")
		})).To(MatchError(ContainSubstring("visit failed")))
		restoreCommitIter()
		restoreLog()

		hash := identity.CommitHashFromString("1111111111111111111111111111111111111111")
		restoreCommitObject := setGitRevisionSeam(&gitRevisionRepoCommitObject, func(*git.Repository, plumbing.Hash) (*object.Commit, error) {
			return &object.Commit{}, nil
		})
		restoreCommitTree := setGitRevisionSeam(&gitRevisionCommitTree, func(*object.Commit) (*object.Tree, error) {
			return nil, errors.New("tree failed")
		})
		_, _, err = store.changedMarkdownEntries(context.Background(), hash)
		Expect(err).To(MatchError(ContainSubstring("load commit tree")))
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
		Expect(contents["page.md"]).To(Equal("# page.md\n"))
		restoreContents()

		restoreContents = setGitRevisionSeam(&gitRevisionFileContents, func(*object.File) (string, error) {
			return "", errors.New("contents failed")
		})
		_, _, err = store.changedMarkdownEntries(context.Background(), hash)
		Expect(err).To(MatchError(ContainSubstring("list changed markdown for root commit")))
		restoreContents()
		restoreFileIter()

		restoreFileIter = setGitRevisionSeam(&gitRevisionFileIterForEach, func(*object.FileIter, func(*object.File) error) error {
			return errors.New("root iter failed")
		})
		_, _, err = store.changedMarkdownEntries(context.Background(), hash)
		Expect(err).To(MatchError(ContainSubstring("list changed markdown for root commit")))
		restoreFileIter()
		restoreTreeFiles()
		restoreCommitTree()
		restoreCommitObject()

		dataDir := GinkgoT().TempDir()
		rootDir := filepath.Join(GinkgoT().TempDir(), "workspace")
		writeFile(GinkgoT(), filepath.Join(rootDir, "page.md"), "# Old\n")
		realStore, err := Open(StoreOptions{DataDir: dataDir, RootDir: rootDir})
		Expect(err).NotTo(HaveOccurred())
		_, err = realStore.Capture(context.Background(), CommitRequest{Actor: PublicEditorActor()})
		Expect(err).NotTo(HaveOccurred())
		writeFile(GinkgoT(), filepath.Join(rootDir, "page.md"), "# New\n")
		second, err := realStore.Capture(context.Background(), CommitRequest{Actor: PublicEditorActor()})
		Expect(err).NotTo(HaveOccurred())
		secondHash := identity.CommitHashFromString(second.Hash)

		originalCommitTree := gitRevisionCommitTree
		treeCalls := 0
		restoreCommitTree = setGitRevisionSeam(&gitRevisionCommitTree, func(commit *object.Commit) (*object.Tree, error) {
			treeCalls++
			if treeCalls == 2 {
				return nil, errors.New("parent tree failed")
			}
			return originalCommitTree(commit)
		})
		_, _, err = realStore.changedMarkdownEntries(context.Background(), secondHash)
		Expect(err).To(MatchError(ContainSubstring("load parent tree")))
		restoreCommitTree()

		restoreDiff := setGitRevisionSeam(&gitRevisionTreeDiffContext, func(*object.Tree, context.Context, *object.Tree) (object.Changes, error) {
			return nil, errors.New("diff failed")
		})
		_, _, err = realStore.changedMarkdownEntries(context.Background(), secondHash)
		Expect(err).To(MatchError(ContainSubstring("diff commit")))
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
		restoreChangeFiles := setGitRevisionSeam(&gitRevisionChangeFiles, func(*object.Change) (*object.File, *object.File, error) {
			return nil, nil, errors.New("change files failed")
		})
		_, _, err = realStore.changedMarkdownEntries(context.Background(), secondHash)
		Expect(err).To(MatchError("change files failed"))
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
		restoreContents = setGitRevisionSeam(&gitRevisionFileContents, func(*object.File) (string, error) {
			return "", errors.New("to contents failed")
		})
		_, _, err = realStore.changedMarkdownEntries(context.Background(), secondHash)
		Expect(err).To(MatchError("to contents failed"))
		restoreContents()
		restoreChangeFiles()
		restoreDiff()

		restoreFilesAt := setGitRevisionSeam(&gitRevisionStoreFilesAt, func(*Store, context.Context, identity.CommitHash) (map[string]string, error) {
			return nil, errors.New("files at failed")
		})
		_, err = store.RestoreWorkspace(context.Background(), hash, CommitRequest{})
		Expect(err).To(MatchError("files at failed"))
		restoreFilesAt()

		restoreFilesAt = setGitRevisionSeam(&gitRevisionStoreFilesAt, func(*Store, context.Context, identity.CommitHash) (map[string]string, error) {
			return map[string]string{"image.png": "png", "page.md": "# Page\n"}, nil
		})
		ctx, cancel = context.WithCancel(context.Background())
		cancel()
		_, err = store.RestoreWorkspace(ctx, hash, CommitRequest{})
		Expect(err).To(Equal(context.Canceled))

		restoreMkdir := setGitRevisionSeam(&gitRevisionMkdirAll, func(string, os.FileMode) error {
			return errors.New("workspace mkdir failed")
		})
		_, err = store.RestoreWorkspace(context.Background(), hash, CommitRequest{})
		Expect(err).To(MatchError(ContainSubstring("create restore parent page.md")))
		restoreMkdir()

		restoreWrite := setGitRevisionSeam(&gitRevisionWriteFile, func(string, []byte, os.FileMode) error {
			return errors.New("workspace write failed")
		})
		_, err = store.RestoreWorkspace(context.Background(), hash, CommitRequest{})
		Expect(err).To(MatchError(ContainSubstring("write restored markdown page.md")))
		restoreWrite()

		restoreCollect := setGitRevisionSeam(&gitRevisionCollectMarkdownPaths, func(string) ([]string, error) {
			return nil, errors.New("collect restore failed")
		})
		_, err = store.RestoreWorkspace(context.Background(), hash, CommitRequest{})
		Expect(err).To(MatchError("collect restore failed"))
		restoreCollect()

		restoreFilesAt()
		restoreFilesAt = setGitRevisionSeam(&gitRevisionStoreFilesAt, func(*Store, context.Context, identity.CommitHash) (map[string]string, error) {
			return map[string]string{}, nil
		})
		restoreCollect = setGitRevisionSeam(&gitRevisionCollectMarkdownPaths, func(string) ([]string, error) {
			return []string{"gone.md"}, nil
		})
		restoreRemove := setGitRevisionSeam(&gitRevisionRemove, func(string) error {
			return errors.New("remove restore failed")
		})
		_, err = store.RestoreWorkspace(context.Background(), hash, CommitRequest{})
		Expect(err).To(MatchError(ContainSubstring("remove markdown absent from restore gone.md")))
		restoreRemove()

		restoreCollect()
		restoreCollect = setGitRevisionSeam(&gitRevisionCollectMarkdownPaths, func(string) ([]string, error) {
			return nil, nil
		})
		restoreCapture := setGitRevisionSeam(&gitRevisionStoreCapture, func(_ *Store, _ context.Context, req CommitRequest) (*Commit, error) {
			Expect(req.Reason).To(Equal(ReasonRestore))
			Expect(req.Source).To(Equal(SourceSystem))
			return &Commit{Created: true}, nil
		})
		commit, err := store.RestoreWorkspace(context.Background(), hash, CommitRequest{})
		Expect(err).NotTo(HaveOccurred())
		Expect(commit.Created).To(BeTrue())
		restoreCapture()
		restoreCollect()
		restoreFilesAt()

		restoreFileContent := setGitRevisionSeam(&gitRevisionStoreFileContentAt, func(*Store, context.Context, identity.CommitHash, string) (string, error) {
			return "", errors.New("content failed")
		})
		_, err = store.RestoreDocumentToPath(context.Background(), "page.md", "page.md", hash, CommitRequest{})
		Expect(err).To(MatchError("content failed"))
		restoreFileContent()

		restoreWrite = setGitRevisionSeam(&gitRevisionWriteFile, func(string, []byte, os.FileMode) error {
			return nil
		})
		restoreCapture = setGitRevisionSeam(&gitRevisionStoreCapture, func(_ *Store, _ context.Context, req CommitRequest) (*Commit, error) {
			Expect(req.Reason).To(Equal(ReasonRestore))
			Expect(req.Source).To(Equal(SourceSystem))
			return &Commit{Created: true}, nil
		})
		commit, err = store.RestoreDocumentContentToPath(context.Background(), "page.md", "# Page\n", CommitRequest{})
		Expect(err).NotTo(HaveOccurred())
		Expect(commit.Created).To(BeTrue())
		restoreCapture()
		restoreWrite()

		restoreCommitObject = setGitRevisionSeam(&gitRevisionRepoCommitObject, func(*git.Repository, plumbing.Hash) (*object.Commit, error) {
			return &object.Commit{}, nil
		})
		restoreCommitTree = setGitRevisionSeam(&gitRevisionCommitTree, func(*object.Commit) (*object.Tree, error) {
			return nil, errors.New("file tree failed")
		})
		_, err = store.fileContentAt(context.Background(), hash, "page.md")
		Expect(err).To(MatchError(ContainSubstring("load commit tree")))
		_, err = store.filesAtCommit(context.Background(), &object.Commit{})
		Expect(err).To(MatchError(ContainSubstring("load commit tree")))
		restoreCommitTree()

		restoreCommitTree = setGitRevisionSeam(&gitRevisionCommitTree, func(*object.Commit) (*object.Tree, error) {
			return &object.Tree{}, nil
		})
		restoreTreeFile := setGitRevisionSeam(&gitRevisionTreeFile, func(*object.Tree, string) (*object.File, error) {
			return nil, errors.New("file missing")
		})
		_, err = store.fileContentAt(context.Background(), hash, "page.md")
		Expect(err).To(MatchError(ContainSubstring("document page.md is not present")))
		restoreTreeFile()

		restoreTreeFile = setGitRevisionSeam(&gitRevisionTreeFile, func(*object.Tree, string) (*object.File, error) {
			return &object.File{Name: "page.md"}, nil
		})
		restoreContents = setGitRevisionSeam(&gitRevisionFileContents, func(*object.File) (string, error) {
			return "", errors.New("file contents failed")
		})
		_, err = store.fileContentAt(context.Background(), hash, "page.md")
		Expect(err).To(MatchError(ContainSubstring("read document page.md")))
		restoreContents()
		restoreTreeFile()

		restoreTreeFiles = setGitRevisionSeam(&gitRevisionTreeFiles, func(*object.Tree) *object.FileIter {
			return &object.FileIter{}
		})
		restoreFileIter = setGitRevisionSeam(&gitRevisionFileIterForEach, func(_ *object.FileIter, visit func(*object.File) error) error {
			Expect(visit(&object.File{Name: "image.png"})).To(Succeed())
			return visit(&object.File{Name: "page.md"})
		})
		restoreFileReader := setGitRevisionSeam(&gitRevisionFileReader, func(*object.File) (io.ReadCloser, error) {
			return nil, errors.New("file reader failed")
		})
		_, err = store.filesAtCommit(context.Background(), &object.Commit{})
		Expect(err).To(MatchError(ContainSubstring("read commit files")))
		restoreFileReader()

		restoreFileReader = setGitRevisionSeam(&gitRevisionFileReader, func(*object.File) (io.ReadCloser, error) {
			return io.NopCloser(strings.NewReader("# Page\n")), nil
		})
		restoreReadAll := setGitRevisionSeam(&gitRevisionReadAll, func(io.Reader) ([]byte, error) {
			return nil, errors.New("file read all failed")
		})
		_, err = store.filesAtCommit(context.Background(), &object.Commit{})
		Expect(err).To(MatchError(ContainSubstring("read commit files")))
		restoreReadAll()
		restoreFileReader()

		ctx, cancel = context.WithCancel(context.Background())
		restoreFileIter()
		restoreFileIter = setGitRevisionSeam(&gitRevisionFileIterForEach, func(_ *object.FileIter, visit func(*object.File) error) error {
			cancel()
			return visit(&object.File{Name: "page.md"})
		})
		_, err = store.filesAtCommit(ctx, &object.Commit{})
		Expect(err).To(MatchError(ContainSubstring("read commit files")))
		restoreFileIter()

		restoreFileIter = setGitRevisionSeam(&gitRevisionFileIterForEach, func(*object.FileIter, func(*object.File) error) error {
			return errors.New("files iter failed")
		})
		_, err = store.filesAtCommit(context.Background(), &object.Commit{})
		Expect(err).To(MatchError(ContainSubstring("read commit files")))
		restoreFileIter()
		restoreTreeFiles()
		restoreCommitTree()
		restoreCommitObject()
	})

	It("covers final changed-entry and file-content cancellation branches", func() {
		store := &Store{}
		hash := identity.CommitHashFromString("1111111111111111111111111111111111111111")

		restoreCommitObject := setGitRevisionSeam(&gitRevisionRepoCommitObject, func(*git.Repository, plumbing.Hash) (*object.Commit, error) {
			return nil, errors.New("load changed commit failed")
		})
		_, _, err := store.changedMarkdownEntries(context.Background(), hash)
		Expect(err).To(MatchError(ContainSubstring("load commit")))
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
		Expect(errors.Is(err, context.Canceled)).To(BeTrue())
		restoreFileIter()
		restoreTreeFiles()
		restoreCommitIterNext()

		restoreCommitIterNext = setGitRevisionSeam(&gitRevisionCommitIterNext, func(object.CommitIter) (*object.Commit, error) {
			return nil, errors.New("parent failed")
		})
		_, _, err = store.changedMarkdownEntries(context.Background(), hash)
		Expect(err).To(MatchError(ContainSubstring("load parent for commit")))
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

func expectErrorContaining(err error, text string) {
	GinkgoHelper()

	Expect(err).To(HaveOccurred())
	Expect(err.Error()).To(ContainSubstring(text))
}

func containsAll(haystack string, needles ...string) bool {
	for _, needle := range needles {
		if !strings.Contains(haystack, needle) {
			return false
		}
	}
	return true
}
