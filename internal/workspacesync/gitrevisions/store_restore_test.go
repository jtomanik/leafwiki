package gitrevisions

import (
	"context"
	"os"
	"path/filepath"

	"github.com/go-git/go-git/v6/plumbing"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/gstruct"

	"github.com/perber/wiki/internal/core/identity"
)

var _ = Describe("git revision store", func() {
	It("restores managed markdown from a snapshot while preserving unmanaged files", Label("integration"), func() {
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

	It("restores a document by writing historical content to its current path", Label("integration"), func() {
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

	It("restores historical source content to a different managed target path", Label("integration"), func() {
		dataDir := gitRevisionTempDir()
		rootDir := filepath.Join(gitRevisionTempDir(), "workspace")
		writeFile(filepath.Join(rootDir, "docs", "source.md"), "# Historical\n")
		store, err := Open(StoreOptions{DataDir: dataDir, RootDir: rootDir})
		Expect(err).To(Succeed())
		previous, err := store.Capture(context.Background(), CommitRequest{
			Reason: ReasonStartup,
			Source: SourceFilesystem,
			Actor:  PublicEditorActor(),
		})
		Expect(err).To(Succeed())
		writeFile(filepath.Join(rootDir, "docs", "source.md"), "# Current\n")

		restore, err := store.RestoreDocumentToPath(context.Background(), "archive/restored.md", "docs/source.md", previous.Hash, CommitRequest{
			Actor: PublicEditorActor(),
		})

		Expect(err).To(Succeed())
		Expect(os.ReadFile(filepath.Join(rootDir, "archive", "restored.md"))).To(Equal([]byte("# Historical\n")))
		Expect(os.ReadFile(filepath.Join(rootDir, "docs", "source.md"))).To(Equal([]byte("# Current\n")))
		Expect(restore).To(matchCreatedRevisionCommitWithMarkdownPaths("archive/restored.md", "docs/source.md"))
		files, err := store.FilesAt(context.Background(), restore.Hash)
		Expect(err).To(Succeed())
		Expect(files).To(HaveKeyWithValue("archive/restored.md", "# Historical\n"))
		Expect(files).To(HaveKeyWithValue("docs/source.md", "# Current\n"))
	})

	It("restores supplied document content to a target path", Label("integration"), func() {
		dataDir := gitRevisionTempDir()
		rootDir := filepath.Join(gitRevisionTempDir(), "workspace")
		store, err := Open(StoreOptions{DataDir: dataDir, RootDir: rootDir})
		Expect(err).To(Succeed())

		restore, err := store.RestoreDocumentContentToPath(context.Background(), " docs/restored.md ", "# Restored\n", CommitRequest{
			Actor: PublicEditorActor(),
		})

		Expect(err).To(Succeed())
		Expect(os.ReadFile(filepath.Join(rootDir, "docs", "restored.md"))).To(Equal([]byte("# Restored\n")))
		Expect(restore).To(matchCreatedRevisionCommitWithMarkdownPaths("docs/restored.md"))
		files, err := store.FilesAt(context.Background(), restore.Hash)
		Expect(err).To(Succeed())
		Expect(files).To(HaveKeyWithValue("docs/restored.md", "# Restored\n"))
		commit, err := store.GetCommit(context.Background(), restore.Hash)
		Expect(err).To(Succeed())
		Expect(commit).To(gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
			"Reason": Equal(ReasonRestore),
			"Source": Equal(SourceSystem),
		}))
	})

	It("classifies managed markdown paths by extension and hidden-name rules", Label("unit"), func() {
		Expect("docs/page.md").To(matchManagedMarkdownPathClass(managedMarkdownRevisionPath))
		Expect("docs/Page.MD").To(matchManagedMarkdownPathClass(managedMarkdownRevisionPath))
		Expect(".obsidian/page.md").To(matchManagedMarkdownPathClass(ignoredHiddenDirectoryMarkdownPath))
		Expect("docs/.draft.md").To(matchManagedMarkdownPathClass(ignoredHiddenFilenameMarkdownPath))
		Expect("docs/page.md.swp").To(matchManagedMarkdownPathClass(ignoredSwapMarkdownPath))
		Expect("docs/image.png").To(matchManagedMarkdownPathClass(ignoredNonMarkdownRevisionPath))
	})

	It("collects visible markdown files for snapshot staging", Label("unit"), func() {
		rootDir := gitRevisionTempDir()
		writeFile(filepath.Join(rootDir, "docs", "page.md"), "# Page\n")
		writeFile(filepath.Join(rootDir, "docs", ".draft.md"), "# Draft\n")
		writeFile(filepath.Join(rootDir, "docs", "page.md.swp"), "# Swap\n")
		writeFile(filepath.Join(rootDir, ".obsidian", "local.md"), "# Local\n")
		writeFile(filepath.Join(rootDir, "docs", "image.png"), "png")

		paths, err := collectMarkdownPaths(rootDir)

		Expect(err).To(Succeed())
		Expect(paths).To(Equal([]string{"docs/page.md"}))
	})

	It("returns captured commit metadata and reports missing commits", Label("integration"), func() {
		dataDir := gitRevisionTempDir()
		rootDir := filepath.Join(gitRevisionTempDir(), "workspace")
		writeFile(filepath.Join(rootDir, "docs", "page.md"), "# Page\n")
		store, err := Open(StoreOptions{DataDir: dataDir, RootDir: rootDir})
		Expect(err).NotTo(HaveOccurred())

		captured, err := store.Capture(context.Background(), CommitRequest{
			Reason: ReasonExplicit,
			Source: SourceMCP,
			Actor: Actor{
				ID:    newFixtureActorID("agent-1"),
				Name:  "Agent One",
				Email: "agent-1@example.test",
			},
		})
		Expect(err).NotTo(HaveOccurred())

		commit, err := store.GetCommit(context.Background(), identity.CommitHashFromString(captured.Hash))
		Expect(err).NotTo(HaveOccurred())
		Expect(commit).To(gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
			"Hash":     Equal(captured.Hash),
			"AuthorID": Equal(newFixtureActorID("agent-1")),
			"Source":   Equal(SourceMCP),
			"Reason":   Equal(ReasonExplicit),
		}))

		_, err = store.GetCommit(context.Background(), identity.CommitHashFromString("0000000000000000000000000000000000000000"))
		Expect(err).To(MatchError(plumbing.ErrObjectNotFound))
	})
})
