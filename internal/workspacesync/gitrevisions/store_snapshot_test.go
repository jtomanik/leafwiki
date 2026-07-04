package gitrevisions

import (
	"context"
	"os"
	"path/filepath"

	git "github.com/go-git/go-git/v6"
	"github.com/go-git/go-git/v6/plumbing/object"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/gstruct"

	"github.com/perber/wiki/internal/core/identity"
)

var _ = Describe("git revision store", func() {
	It("returns current content only for changed markdown files that still exist", Label("integration"), func() {
		dataDir := gitRevisionTempDir()
		rootDir := filepath.Join(gitRevisionTempDir(), "workspace")
		store, err := Open(StoreOptions{DataDir: dataDir, RootDir: rootDir})
		Expect(err).NotTo(HaveOccurred())
		writeFile(filepath.Join(rootDir, "changed.md"), "# Old\n")
		writeFile(filepath.Join(rootDir, "deleted.md"), "# Deleted\n")
		writeFile(filepath.Join(rootDir, "unchanged.md"), "# Unchanged\n")
		_, err = store.Capture(context.Background(), CommitRequest{
			Reason: ReasonExplicit,
			Source: SourceFilesystem,
			Actor:  PublicEditorActor(),
		})
		Expect(err).NotTo(HaveOccurred())

		writeFile(filepath.Join(rootDir, "changed.md"), "# New\n")
		Expect(os.Remove(filepath.Join(rootDir, "deleted.md"))).To(Succeed())
		commit, err := store.Capture(context.Background(), CommitRequest{
			Reason: ReasonExplicit,
			Source: SourceFilesystem,
			Actor:  PublicEditorActor(),
		})
		Expect(err).NotTo(HaveOccurred())

		paths, err := store.ChangedMarkdownPaths(context.Background(), identity.CommitHashFromString(commit.Hash))
		Expect(err).NotTo(HaveOccurred())
		Expect(paths).To(Equal([]string{"changed.md", "deleted.md"}))
		contents, err := store.ChangedMarkdownContents(context.Background(), identity.CommitHashFromString(commit.Hash))
		Expect(err).NotTo(HaveOccurred())
		Expect(contents).To(HaveKeyWithValue("changed.md", "# New\n"))
		Expect(contents).NotTo(HaveKey("deleted.md"))
		Expect(contents).NotTo(HaveKey("unchanged.md"))
	})

	It("stores internal revisions without mutating a containing user git repository", Label("integration"), func() {
		dataDir := gitRevisionTempDir()
		userRepoDir := filepath.Join(gitRevisionTempDir(), "user-repo")
		rootDir := filepath.Join(userRepoDir, "wiki")
		userGitDir := filepath.Join(userRepoDir, ".git")
		Expect(os.MkdirAll(userGitDir, 0o755)).To(Succeed())
		userHead := filepath.Join(userGitDir, "HEAD")
		writeFile(userHead, "ref: refs/heads/main\n")
		writeFile(filepath.Join(rootDir, "page.md"), "# Page\n")

		store, err := Open(StoreOptions{DataDir: dataDir, RootDir: rootDir})
		Expect(err).NotTo(HaveOccurred())
		_, err = store.Capture(context.Background(), CommitRequest{
			Reason: ReasonStartup,
			Source: SourceFilesystem,
			Actor:  PublicEditorActor(),
		})
		Expect(err).NotTo(HaveOccurred())

		Expect(os.ReadFile(userHead)).To(Equal([]byte("ref: refs/heads/main\n")))
		Expect(os.Stat(filepath.Join(rootDir, ".git"))).Error().To(Satisfy(os.IsNotExist))
		Expect(os.Stat(filepath.Join(dataDir, ".leafwiki", "git"))).Error().NotTo(HaveOccurred())
	})

	It("preserves unrelated root gitdir files", Label("integration"), func() {
		dataDir := gitRevisionTempDir()
		rootDir := filepath.Join(gitRevisionTempDir(), "workspace")
		writeFile(filepath.Join(rootDir, "page.md"), "# Page\n")
		gitFile := filepath.Join(rootDir, ".git")
		gitFileContent := "gitdir: ../.git/modules/workspace\n"
		writeFile(gitFile, gitFileContent)

		store, err := Open(StoreOptions{DataDir: dataDir, RootDir: rootDir})
		Expect(err).NotTo(HaveOccurred())
		_, err = store.Capture(context.Background(), CommitRequest{
			Reason: ReasonStartup,
			Source: SourceFilesystem,
			Actor:  PublicEditorActor(),
		})
		Expect(err).NotTo(HaveOccurred())

		Expect(os.ReadFile(gitFile)).To(Equal([]byte(gitFileContent)))
	})

	It("prunes legacy dot-directory markdown from new revision snapshots", Label("integration"), func() {
		dataDir := gitRevisionTempDir()
		rootDir := filepath.Join(gitRevisionTempDir(), "workspace")
		writeFile(filepath.Join(rootDir, "page.md"), "# Page\n")
		writeFile(filepath.Join(rootDir, ".obsidian", "local.md"), "# Local\n")

		store, err := Open(StoreOptions{DataDir: dataDir, RootDir: rootDir})
		Expect(err).NotTo(HaveOccurred())
		wt, err := store.repo.Worktree()
		Expect(err).NotTo(HaveOccurred())
		_, err = wt.Add("page.md")
		Expect(err).NotTo(HaveOccurred())
		_, err = wt.Add(".obsidian/local.md")
		Expect(err).NotTo(HaveOccurred())
		legacyHash, err := wt.Commit("legacy dot-directory markdown", &git.CommitOptions{
			Author:    signature(PublicEditorActor()),
			Committer: leafWikiCommitter(),
		})
		Expect(err).NotTo(HaveOccurred())
		legacyCommit, err := store.repo.CommitObject(legacyHash)
		Expect(err).NotTo(HaveOccurred())
		legacyTree, err := legacyCommit.Tree()
		Expect(err).NotTo(HaveOccurred())
		_, err = legacyTree.File(".obsidian/local.md")
		Expect(err).NotTo(HaveOccurred())

		prune, err := store.Capture(context.Background(), CommitRequest{
			Reason: ReasonExplicit,
			Source: SourceFilesystem,
			Actor:  PublicEditorActor(),
		})
		Expect(err).NotTo(HaveOccurred())
		Expect(prune).To(matchCreatedRevisionCommitWithMarkdownPaths(".obsidian/local.md"))

		prunedCommit, err := store.repo.CommitObject(PlumbingHashFromCommitHash(prune.Hash))
		Expect(err).NotTo(HaveOccurred())
		prunedTree, err := prunedCommit.Tree()
		Expect(err).NotTo(HaveOccurred())
		_, err = prunedTree.File(".obsidian/local.md")
		Expect(err).To(MatchError(object.ErrFileNotFound))
		files, err := store.FilesAt(context.Background(), identity.CommitHashFromString(prune.Hash))
		Expect(err).NotTo(HaveOccurred())
		Expect(files).NotTo(HaveKey(".obsidian/local.md"))
		Expect(os.ReadFile(filepath.Join(rootDir, ".obsidian", "local.md"))).To(Equal([]byte("# Local\n")))
	})

	It("records deleted markdown files as removals in the revision snapshot", Label("integration"), func() {
		dataDir := gitRevisionTempDir()
		rootDir := filepath.Join(gitRevisionTempDir(), "workspace")
		writeFile(filepath.Join(rootDir, "keep.md"), "# Keep\n")
		writeFile(filepath.Join(rootDir, "remove.md"), "# Remove\n")

		store, err := Open(StoreOptions{DataDir: dataDir, RootDir: rootDir})
		Expect(err).NotTo(HaveOccurred())
		_, err = store.Capture(context.Background(), CommitRequest{
			Reason: ReasonStartup,
			Source: SourceFilesystem,
			Actor:  PublicEditorActor(),
		})
		Expect(err).NotTo(HaveOccurred())

		Expect(os.Remove(filepath.Join(rootDir, "remove.md"))).To(Succeed())
		commit, err := store.Capture(context.Background(), CommitRequest{
			Reason: ReasonExplicit,
			Source: SourceFilesystem,
			Actor:  PublicEditorActor(),
		})
		Expect(err).NotTo(HaveOccurred())

		files, err := store.FilesAt(context.Background(), identity.CommitHashFromString(commit.Hash))
		Expect(err).NotTo(HaveOccurred())
		Expect(files).NotTo(HaveKey("remove.md"))
		Expect(files).To(HaveKeyWithValue("keep.md", "# Keep\n"))
	})

	It("replaces the startup commit when metadata writeback is amended", Label("integration"), func() {
		dataDir := gitRevisionTempDir()
		rootDir := filepath.Join(gitRevisionTempDir(), "workspace")
		writeFile(filepath.Join(rootDir, "page.md"), "# Page\n")

		store, err := Open(StoreOptions{DataDir: dataDir, RootDir: rootDir})
		Expect(err).NotTo(HaveOccurred())
		first, err := store.Capture(context.Background(), CommitRequest{
			Reason: ReasonStartup,
			Source: SourceFilesystem,
			Actor:  PublicEditorActor(),
		})
		Expect(err).NotTo(HaveOccurred())

		writeFile(filepath.Join(rootDir, "page.md"), "---\nleafwiki_id: page\n---\n# Page\n")
		amended, err := store.Amend(context.Background(), CommitRequest{
			Reason: ReasonStartup,
			Source: SourceSystem,
			Actor:  PublicEditorActor(),
		})
		Expect(err).NotTo(HaveOccurred())

		Expect(amended.Hash).NotTo(Equal(first.Hash))
		commits, err := store.ListCommits(context.Background(), ListRequest{Limit: 10})
		Expect(err).NotTo(HaveOccurred())
		Expect(commits).To(ConsistOf(gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
			"Hash": Equal(amended.Hash),
		})))
		files, err := store.FilesAt(context.Background(), identity.CommitHashFromString(amended.Hash))
		Expect(err).NotTo(HaveOccurred())
		Expect(files).To(HaveKeyWithValue("page.md", "---\nleafwiki_id: page\n---\n# Page\n"))
	})

})
