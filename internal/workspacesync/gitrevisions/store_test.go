package gitrevisions

import (
	"context"
	"os"
	"path/filepath"
	"strconv"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/gstruct"

	"github.com/perber/wiki/internal/core/identity"
)

var _ = Describe("git revision store", func() {
	It("captures an initial snapshot with only managed markdown files", Label("integration"), func() {
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

	It("tracks markdown files with uppercase extensions", Label("integration"), func() {
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

	It("writes synchronization trailers and changed markdown paths into commits", Label("integration"), func() {
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

	It("keeps original changed markdown paths when amending metadata writeback commits", Label("integration"), func() {
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

	It("resumes commit listing after the cursor hash", Label("integration"), func() {
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

	It("returns workspace metadata for listed commits", Label("integration"), func() {
		dataDir := gitRevisionTempDir()
		rootDir := filepath.Join(gitRevisionTempDir(), "workspace")
		writeFile(filepath.Join(rootDir, "a.md"), "# A\n")
		store, err := Open(StoreOptions{DataDir: dataDir, RootDir: rootDir})
		Expect(err).NotTo(HaveOccurred())
		_, err = store.Capture(context.Background(), CommitRequest{
			Reason: ReasonExplicit,
			Source: SourceFilesystem,
			Actor:  Actor{ID: newFixtureActorID("alice"), Name: "Alice", Email: "alice@example.test"},
		})
		Expect(err).NotTo(HaveOccurred())

		commits, err := store.ListCommits(context.Background(), ListRequest{Limit: 1})
		Expect(err).NotTo(HaveOccurred())
		Expect(commits).To(ConsistOf(gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
			"Source":               Equal(SourceFilesystem),
			"Reason":               Equal(ReasonExplicit),
			"AuthorID":             Equal(newFixtureActorID("alice")),
			"AuthorName":           Equal("Alice"),
			"AuthorEmail":          Equal("alice@example.test"),
			"ChangedMarkdownCount": Equal(1),
			"CreatedAt":            Not(BeZero()),
		})))
	})

	It("records additional actors once while preserving the primary author", Label("integration"), func() {
		dataDir := gitRevisionTempDir()
		rootDir := filepath.Join(gitRevisionTempDir(), "workspace")
		writeFile(filepath.Join(rootDir, "a.md"), "# A\n")
		store, err := Open(StoreOptions{DataDir: dataDir, RootDir: rootDir})
		Expect(err).NotTo(HaveOccurred())

		_, err = store.Capture(context.Background(), CommitRequest{
			Reason: ReasonExplicit,
			Source: SourceWeb,
			Actor:  Actor{ID: newFixtureActorID("alice"), Name: "Alice", Email: "alice@example.test"},
			AdditionalActors: []Actor{
				{ID: newFixtureActorID("bob"), Name: "Bob", Email: "bob@example.test"},
				{ID: newFixtureActorID("alice"), Name: "Alice", Email: "alice@example.test"},
			},
		})
		Expect(err).NotTo(HaveOccurred())

		head, err := store.repo.Head()
		Expect(err).NotTo(HaveOccurred())
		headCommit, err := store.repo.CommitObject(head.Hash())
		Expect(err).NotTo(HaveOccurred())
		Expect(commitFromObject(headCommit)).To(gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
			"AuthorID": Equal(newFixtureActorID("alice")),
			"ActorIDs": WithTransform(actorIDStrings, Equal([]string{"alice", "bob"})),
		}))

		commits, err := store.ListCommits(context.Background(), ListRequest{Limit: 1})
		Expect(err).NotTo(HaveOccurred())
		Expect(commits).To(ConsistOf(gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
			"AuthorID": Equal(newFixtureActorID("alice")),
			"ActorIDs": WithTransform(actorIDStrings, Equal([]string{"alice", "bob"})),
		})))
	})

	It("stops commit iteration when the visitor declines to continue", Label("integration"), func() {
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

})
