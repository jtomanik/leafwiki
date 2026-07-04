package gitrevisions

import (
	"context"
	"os"
	"path/filepath"
	"strconv"

	git "github.com/go-git/go-git/v6"
	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/object"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/gstruct"

	"github.com/perber/wiki/internal/core/identity"
)

func actorIDStrings(ids []ActorID) []string {
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		out = append(out, id.String())
	}
	return out
}

var _ = Describe("git revision store", func() {
	It("captures an initial snapshot with only managed markdown files", func() {
		dataDir := gitRevisionTempDir()
		rootDir := filepath.Join(gitRevisionTempDir(), "workspace")
		Expect(os.MkdirAll(filepath.Join(rootDir, "nested"), 0o755)).To(Succeed())
		writeFile(filepath.Join(rootDir, "a.md"), "# A\n")
		writeFile(filepath.Join(rootDir, "nested", "b.md"), "# B\n")
		writeFile(filepath.Join(rootDir, ".obsidian", "local.md"), "# Local\n")
		writeFile(filepath.Join(rootDir, "image.png"), "png")
		writeFile(filepath.Join(rootDir, "draft.md.swp"), "# swap\n")
		writeFile(filepath.Join(rootDir, ".DS_Store"), "ds")

		store, err := Open(StoreOptions{DataDir: dataDir, RootDir: rootDir})
		Expect(err).NotTo(HaveOccurred())

		commit, err := store.Capture(context.Background(), CommitRequest{
			Reason: ReasonStartup,
			Source: SourceFilesystem,
			Actor:  PublicEditorActor(),
		})
		Expect(err).NotTo(HaveOccurred())

		head, err := store.repo.Head()
		Expect(err).NotTo(HaveOccurred())
		Expect(commit.Hash).To(Equal(CommitHashFromPlumbingHash(head.Hash())))
		Expect(os.Stat(filepath.Join(dataDir, ".leafwiki", "git"))).Error().NotTo(HaveOccurred())
		Expect(os.Stat(filepath.Join(rootDir, ".git"))).Error().To(Satisfy(os.IsNotExist))

		files, err := store.FilesAt(context.Background(), identity.CommitHashFromString(commit.Hash))
		Expect(err).NotTo(HaveOccurred())
		Expect(files).To(Equal(map[string]string{
			"a.md":        "# A\n",
			"nested/b.md": "# B\n",
		}))
		for _, ignored := range []string{"image.png", "draft.md.swp", ".DS_Store", ".obsidian/local.md"} {
			Expect(files).NotTo(HaveKey(ignored))
		}
	})

	It("tracks markdown files with uppercase extensions", func() {
		dataDir := gitRevisionTempDir()
		rootDir := filepath.Join(gitRevisionTempDir(), "workspace")
		writeFile(filepath.Join(rootDir, "Page.MD"), "# Page\n")

		store, err := Open(StoreOptions{DataDir: dataDir, RootDir: rootDir})
		Expect(err).NotTo(HaveOccurred())

		commit, err := store.Capture(context.Background(), CommitRequest{
			Reason: ReasonExplicit,
			Source: SourceFilesystem,
			Actor:  PublicEditorActor(),
		})
		Expect(err).NotTo(HaveOccurred())

		Expect(commit.ChangedMarkdownPaths).To(Equal([]string{"Page.MD"}))
		files, err := store.FilesAt(context.Background(), identity.CommitHashFromString(commit.Hash))
		Expect(err).NotTo(HaveOccurred())
		Expect(files).To(HaveKeyWithValue("Page.MD", "# Page\n"))
	})

	It("writes synchronization trailers and changed markdown paths into commits", func() {
		dataDir := gitRevisionTempDir()
		rootDir := filepath.Join(gitRevisionTempDir(), "workspace")
		writeFile(filepath.Join(rootDir, "a.md"), "# A\n")

		store, err := Open(StoreOptions{DataDir: dataDir, RootDir: rootDir})
		Expect(err).NotTo(HaveOccurred())

		commit, err := store.Capture(context.Background(), CommitRequest{
			Reason: ReasonExplicit,
			Source: SourceFilesystem,
			Actor:  PublicEditorActor(),
		})
		Expect(err).NotTo(HaveOccurred())

		Expect(commit).To(gstruct.PointTo(gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
			"ChangedMarkdownCount": Equal(1),
			"ChangedMarkdownPaths": Equal([]string{"a.md"}),
		})))

		head, err := store.repo.Head()
		Expect(err).NotTo(HaveOccurred())
		headCommit, err := store.repo.CommitObject(head.Hash())
		Expect(err).NotTo(HaveOccurred())
		Expect(commitFromObject(headCommit)).To(gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
			"Source":               Equal(SourceFilesystem),
			"Reason":               Equal(ReasonExplicit),
			"BatchID":              Not(BeEmpty()),
			"ActorIDs":             Equal([]ActorID{PublicEditorActor().ID}),
			"ChangedMarkdownCount": Equal(1),
		}))
	})

	It("keeps original changed markdown paths when amending metadata writeback commits", func() {
		dataDir := gitRevisionTempDir()
		rootDir := filepath.Join(gitRevisionTempDir(), "workspace")
		writeFile(filepath.Join(rootDir, "a.md"), "# A\n")
		writeFile(filepath.Join(rootDir, "b.md"), "# B\n")

		store, err := Open(StoreOptions{DataDir: dataDir, RootDir: rootDir})
		Expect(err).NotTo(HaveOccurred())
		first, err := store.Capture(context.Background(), CommitRequest{
			Reason: ReasonExplicit,
			Source: SourceFilesystem,
			Actor:  PublicEditorActor(),
		})
		Expect(err).NotTo(HaveOccurred())
		writeFile(filepath.Join(rootDir, "b.md"), "# B\n\nmetadata writeback\n")

		amended, err := store.Amend(context.Background(), CommitRequest{
			Reason:               ReasonExplicit,
			Source:               SourceFilesystem,
			Actor:                PublicEditorActor(),
			BatchID:              first.BatchID,
			ChangedMarkdownPaths: first.ChangedMarkdownPaths,
		})
		Expect(err).NotTo(HaveOccurred())

		Expect(amended).To(gstruct.PointTo(gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
			"ChangedMarkdownCount": Equal(2),
			"ChangedMarkdownPaths": Equal([]string{"a.md", "b.md"}),
		})))
		head, err := store.repo.Head()
		Expect(err).NotTo(HaveOccurred())
		headCommit, err := store.repo.CommitObject(head.Hash())
		Expect(err).NotTo(HaveOccurred())
		Expect(commitFromObject(headCommit)).To(gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
			"ChangedMarkdownCount": Equal(2),
		}))
	})

	It("resumes commit listing after the cursor hash", func() {
		dataDir := gitRevisionTempDir()
		rootDir := filepath.Join(gitRevisionTempDir(), "workspace")
		store, err := Open(StoreOptions{DataDir: dataDir, RootDir: rootDir})
		Expect(err).NotTo(HaveOccurred())
		writeFile(filepath.Join(rootDir, "page.md"), "# Page 1\n")
		oldest, err := store.Capture(context.Background(), CommitRequest{
			Reason: ReasonExplicit,
			Source: SourceFilesystem,
			Actor:  PublicEditorActor(),
		})
		Expect(err).NotTo(HaveOccurred())
		writeFile(filepath.Join(rootDir, "page.md"), "# Page 2\n")
		cursor, err := store.Capture(context.Background(), CommitRequest{
			Reason: ReasonExplicit,
			Source: SourceFilesystem,
			Actor:  PublicEditorActor(),
		})
		Expect(err).NotTo(HaveOccurred())
		writeFile(filepath.Join(rootDir, "page.md"), "# Page 3\n")
		_, err = store.Capture(context.Background(), CommitRequest{
			Reason: ReasonExplicit,
			Source: SourceFilesystem,
			Actor:  PublicEditorActor(),
		})
		Expect(err).NotTo(HaveOccurred())

		commits, err := store.ListCommits(context.Background(), ListRequest{Cursor: identity.CommitHashFromString(cursor.Hash), Limit: 2})
		Expect(err).NotTo(HaveOccurred())

		Expect(commits).To(ConsistOf(gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
			"Hash": Equal(oldest.Hash),
		})))
	})

	It("returns workspace metadata for listed commits", func() {
		dataDir := gitRevisionTempDir()
		rootDir := filepath.Join(gitRevisionTempDir(), "workspace")
		writeFile(filepath.Join(rootDir, "a.md"), "# A\n")
		store, err := Open(StoreOptions{DataDir: dataDir, RootDir: rootDir})
		Expect(err).NotTo(HaveOccurred())
		_, err = store.Capture(context.Background(), CommitRequest{
			Reason: ReasonExplicit,
			Source: SourceFilesystem,
			Actor:  Actor{ID: "alice", Name: "Alice", Email: "alice@example.test"},
		})
		Expect(err).NotTo(HaveOccurred())

		commits, err := store.ListCommits(context.Background(), ListRequest{Limit: 1})
		Expect(err).NotTo(HaveOccurred())
		Expect(commits).To(ConsistOf(gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
			"Source":               Equal(SourceFilesystem),
			"Reason":               Equal(ReasonExplicit),
			"AuthorID":             Equal(ParseActorID("alice")),
			"AuthorName":           Equal("Alice"),
			"AuthorEmail":          Equal("alice@example.test"),
			"ChangedMarkdownCount": Equal(1),
			"CreatedAt":            Not(BeZero()),
		})))
	})

	It("records additional actors once while preserving the primary author", func() {
		dataDir := gitRevisionTempDir()
		rootDir := filepath.Join(gitRevisionTempDir(), "workspace")
		writeFile(filepath.Join(rootDir, "a.md"), "# A\n")
		store, err := Open(StoreOptions{DataDir: dataDir, RootDir: rootDir})
		Expect(err).NotTo(HaveOccurred())

		_, err = store.Capture(context.Background(), CommitRequest{
			Reason: ReasonExplicit,
			Source: SourceWeb,
			Actor:  Actor{ID: "alice", Name: "Alice", Email: "alice@example.test"},
			AdditionalActors: []Actor{
				{ID: "bob", Name: "Bob", Email: "bob@example.test"},
				{ID: "alice", Name: "Alice", Email: "alice@example.test"},
			},
		})
		Expect(err).NotTo(HaveOccurred())

		head, err := store.repo.Head()
		Expect(err).NotTo(HaveOccurred())
		headCommit, err := store.repo.CommitObject(head.Hash())
		Expect(err).NotTo(HaveOccurred())
		Expect(commitFromObject(headCommit)).To(gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
			"AuthorID": Equal(ParseActorID("alice")),
			"ActorIDs": WithTransform(actorIDStrings, Equal([]string{"alice", "bob"})),
		}))

		commits, err := store.ListCommits(context.Background(), ListRequest{Limit: 1})
		Expect(err).NotTo(HaveOccurred())
		Expect(commits).To(ConsistOf(gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
			"AuthorID": Equal(ParseActorID("alice")),
			"ActorIDs": WithTransform(actorIDStrings, Equal([]string{"alice", "bob"})),
		})))
	})

	It("stops commit iteration when the visitor declines to continue", func() {
		dataDir := gitRevisionTempDir()
		rootDir := filepath.Join(gitRevisionTempDir(), "workspace")
		store, err := Open(StoreOptions{DataDir: dataDir, RootDir: rootDir})
		Expect(err).NotTo(HaveOccurred())
		for i := 0; i < 3; i++ {
			writeFile(filepath.Join(rootDir, "page.md"), "# Page "+strconv.Itoa(i)+"\n")
			_, err = store.Capture(context.Background(), CommitRequest{
				Reason: ReasonExplicit,
				Source: SourceFilesystem,
				Actor:  PublicEditorActor(),
			})
			Expect(err).NotTo(HaveOccurred())
		}

		visited := 0
		Expect(store.ForEachCommit(context.Background(), func(Commit) (bool, error) {
			visited++
			return false, nil
		})).To(Succeed())

		Expect(visited).To(Equal(1))
	})

	It("returns current content only for changed markdown files that still exist", func() {
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

	It("stores internal revisions without mutating a containing user git repository", func() {
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

	It("preserves unrelated root gitdir files", func() {
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

	It("prunes legacy dot-directory markdown from new revision snapshots", func() {
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

	It("records deleted markdown files as removals in the revision snapshot", func() {
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

	It("replaces the startup commit when metadata writeback is amended", func() {
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

	It("restores managed markdown from a snapshot while preserving unmanaged files", func() {
		dataDir := gitRevisionTempDir()
		rootDir := filepath.Join(gitRevisionTempDir(), "workspace")
		writeFile(filepath.Join(rootDir, "one.md"), "# One A\n")
		writeFile(filepath.Join(rootDir, "two.md"), "# Two A\n")
		store, err := Open(StoreOptions{DataDir: dataDir, RootDir: rootDir})
		Expect(err).NotTo(HaveOccurred())
		snapshot, err := store.Capture(context.Background(), CommitRequest{
			Reason: ReasonStartup,
			Source: SourceFilesystem,
			Actor:  PublicEditorActor(),
		})
		Expect(err).NotTo(HaveOccurred())

		writeFile(filepath.Join(rootDir, "one.md"), "# One current\n")
		writeFile(filepath.Join(rootDir, "three.md"), "# Three current\n")
		writeFile(filepath.Join(rootDir, "image.png"), "png")
		restore, err := store.RestoreWorkspace(context.Background(), identity.CommitHashFromString(snapshot.Hash), CommitRequest{
			Reason: ReasonRestore,
			Source: SourceSystem,
			Actor:  PublicEditorActor(),
		})
		Expect(err).NotTo(HaveOccurred())

		Expect(restore.Hash).NotTo(Equal(snapshot.Hash))
		Expect(os.ReadFile(filepath.Join(rootDir, "one.md"))).To(Equal([]byte("# One A\n")))
		Expect(os.ReadFile(filepath.Join(rootDir, "two.md"))).To(Equal([]byte("# Two A\n")))
		Expect(os.Stat(filepath.Join(rootDir, "three.md"))).Error().To(Satisfy(os.IsNotExist))
		Expect(os.ReadFile(filepath.Join(rootDir, "image.png"))).To(Equal([]byte("png")))
		files, err := store.FilesAt(context.Background(), identity.CommitHashFromString(restore.Hash))
		Expect(err).NotTo(HaveOccurred())
		Expect(files).NotTo(HaveKey("three.md"))
		Expect(files).To(HaveKeyWithValue("one.md", "# One A\n"))
		Expect(files).To(HaveKeyWithValue("two.md", "# Two A\n"))
	})

	It("restores a document by writing historical content to its current path", func() {
		dataDir := gitRevisionTempDir()
		rootDir := filepath.Join(gitRevisionTempDir(), "workspace")
		writeFile(filepath.Join(rootDir, "docs", "page.md"), "# Previous\n")
		store, err := Open(StoreOptions{DataDir: dataDir, RootDir: rootDir})
		Expect(err).NotTo(HaveOccurred())
		previous, err := store.Capture(context.Background(), CommitRequest{
			Reason: ReasonStartup,
			Source: SourceFilesystem,
			Actor:  PublicEditorActor(),
		})
		Expect(err).NotTo(HaveOccurred())

		writeFile(filepath.Join(rootDir, "docs", "page.md"), "# Current\n")
		_, err = store.Capture(context.Background(), CommitRequest{
			Reason: ReasonExplicit,
			Source: SourceFilesystem,
			Actor:  PublicEditorActor(),
		})
		Expect(err).NotTo(HaveOccurred())

		restore, err := store.RestoreDocument(context.Background(), "docs/page.md", identity.CommitHashFromString(previous.Hash), CommitRequest{
			Reason: ReasonRestore,
			Source: SourceSystem,
			Actor:  PublicEditorActor(),
		})
		Expect(err).NotTo(HaveOccurred())

		Expect(restore.Hash).NotTo(Equal(previous.Hash))
		Expect(os.ReadFile(filepath.Join(rootDir, "docs", "page.md"))).To(Equal([]byte("# Previous\n")))
		files, err := store.FilesAt(context.Background(), identity.CommitHashFromString(restore.Hash))
		Expect(err).NotTo(HaveOccurred())
		Expect(files).To(HaveKeyWithValue("docs/page.md", "# Previous\n"))
	})

	It("classifies managed markdown paths by extension and hidden-name rules", func() {
		Expect(IsManagedMarkdownRelPath("docs/page.md")).To(BeTrue())
		Expect(IsManagedMarkdownRelPath("docs/Page.MD")).To(BeTrue())
		Expect(IsManagedMarkdownRelPath(".obsidian/page.md")).To(BeFalse())
		Expect(IsManagedMarkdownRelPath("docs/.draft.md")).To(BeFalse())
		Expect(IsManagedMarkdownRelPath("docs/page.md.swp")).To(BeFalse())
		Expect(IsManagedMarkdownRelPath("docs/image.png")).To(BeFalse())
	})

	It("returns captured commit metadata and reports missing commits", func() {
		dataDir := gitRevisionTempDir()
		rootDir := filepath.Join(gitRevisionTempDir(), "workspace")
		writeFile(filepath.Join(rootDir, "docs", "page.md"), "# Page\n")
		store, err := Open(StoreOptions{DataDir: dataDir, RootDir: rootDir})
		Expect(err).NotTo(HaveOccurred())

		captured, err := store.Capture(context.Background(), CommitRequest{
			Reason: ReasonExplicit,
			Source: SourceMCP,
			Actor: Actor{
				ID:    ParseActorID("agent-1"),
				Name:  "Agent One",
				Email: "agent-1@example.test",
			},
		})
		Expect(err).NotTo(HaveOccurred())

		commit, err := store.GetCommit(context.Background(), identity.CommitHashFromString(captured.Hash))
		Expect(err).NotTo(HaveOccurred())
		Expect(commit).To(gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
			"Hash":     Equal(captured.Hash),
			"AuthorID": Equal(ParseActorID("agent-1")),
			"Source":   Equal(SourceMCP),
			"Reason":   Equal(ReasonExplicit),
		}))

		_, err = store.GetCommit(context.Background(), identity.CommitHashFromString("0000000000000000000000000000000000000000"))
		Expect(err).To(MatchError(plumbing.ErrObjectNotFound))
	})
})

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
