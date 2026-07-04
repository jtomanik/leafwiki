package workspacesync

import (
	"context"
	"errors"
	"os"
	"path/filepath"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	wikivalidation "github.com/perber/wiki/internal/core/markdownvalidation"
	"github.com/perber/wiki/internal/core/tree"
	testmatchers "github.com/perber/wiki/internal/test_utils/matchers"
	"github.com/perber/wiki/internal/workspacesync/gitrevisions"
)

var _ = Describe("canonical markdown migration rollback", Label("integration"), func() {
	It("restores earlier rewrites when a later file cannot be written", func() {
		rootDir := filepath.Join(workspaceSyncTempDir(), "workspace")
		service := &Service{rootDir: rootDir}

		firstPath := filepath.Join(rootDir, "a", "source.md")
		secondDir := filepath.Join(rootDir, "readonly")
		secondPath := filepath.Join(secondDir, "source.md")
		firstOriginal := `---
leafwiki_id: first-source
leafwiki_title: First Source
---
# First Source

[Target](/targets/first)
`
		secondOriginal := `---
leafwiki_id: second-source
leafwiki_title: Second Source
---
# Second Source

[Target](/targets/second)
`
		writeMarkdownFile(firstPath, firstOriginal)
		writeMarkdownFile(secondPath, secondOriginal)
		writeMarkdownFile(filepath.Join(rootDir, "targets", "first.md"), `---
leafwiki_id: first-target
leafwiki_title: First Target
---
# First Target
`)
		writeMarkdownFile(filepath.Join(rootDir, "targets", "second.md"), `---
leafwiki_id: second-target
leafwiki_title: Second Target
---
# Second Target
`)
		Expect(os.Chmod(secondPath, 0o444)).To(Succeed())
		Expect(os.Chmod(secondDir, 0o555)).To(Succeed())
		DeferCleanup(func() {
			_ = os.Chmod(secondDir, 0o755)
			_ = os.Chmod(secondPath, 0o644)
		})
		probePath := filepath.Join(secondDir, ".probe")
		if err := os.WriteFile(probePath, []byte("probe"), 0o644); err == nil {
			_ = os.Remove(probePath)
			Skip("filesystem permits writes to read-only test directory")
		}

		Expect(canonicalMigrationFailsWithoutCommit(service)).To(Succeed())
		Expect(readFileStringGinkgo(firstPath)).To(Equal(firstOriginal))
		Expect(readFileStringGinkgo(secondPath)).To(Equal(secondOriginal))
	})

	It("rolls back committed renames when atomic rewrite fails", func() {
		rootDir := workspaceSyncTempDir()
		firstPath := filepath.Join(rootDir, "first.md")
		blockingDir := filepath.Join(rootDir, "blocking")
		firstOriginal := "# First\n\n[Target](/target)\n"
		firstCanonical := "# First\n\n[Target](/target.md)\n"
		writeMarkdownFile(firstPath, firstOriginal)
		Expect(os.MkdirAll(blockingDir, 0o755)).To(Succeed())

		rewriteErr := writeCanonicalMarkdownRewritesAtomically([]canonicalMarkdownRewrite{
			{
				Path:     firstPath,
				Original: []byte(firstOriginal),
				Content:  []byte(firstCanonical),
				Mode:     0o644,
			},
			{
				Path:    blockingDir,
				Content: []byte("not a markdown file"),
				Mode:    0o644,
			},
		})

		linkErr, err := atomicRewriteLinkError(rewriteErr)
		Expect(err).To(Succeed())
		Expect(linkErr).To(HaveField("New", Equal(blockingDir)))
		Expect(readFileStringGinkgo(firstPath)).To(Equal(firstOriginal))
	})

	It("restores file and tree content when writeback capture fails", func() {
		dataDir := workspaceSyncTempDir()
		rootDir := filepath.Join(workspaceSyncTempDir(), "workspace")
		treeService := tree.NewTreeServiceWithOptions(tree.TreeOptions{DataDir: dataDir, RootDir: rootDir})
		Expect(treeService.LoadTree()).To(Succeed())
		sourcePath := filepath.Join(rootDir, "docs", "a.md")
		sourceOriginal := `---
leafwiki_id: page-a
leafwiki_title: Page A
---
# Page A

[B](/docs/b)
`
		writeMarkdownFile(sourcePath, sourceOriginal)
		writeMarkdownFile(filepath.Join(rootDir, "docs", "b.md"), `---
leafwiki_id: page-b
leafwiki_title: Page B
---
# Page B
`)
		captureErr := errors.New("writeback capture failed")
		service, err := NewService(ServiceOptions{
			Enabled: true,
			DataDir: dataDir,
			RootDir: rootDir,
			Tree:    treeService,
			Store: &fakeRevisionStore{
				capture:        &gitrevisions.Commit{Hash: "initial-commit"},
				captureErr:     captureErr,
				captureErrCall: 2,
			},
		})
		Expect(err).To(Succeed())

		_, err = service.SyncNow(context.Background(), SyncRequest{
			Reason: ReasonExplicit,
			Source: SourceFilesystem,
			Actor:  PublicEditorActor(),
		})

		Expect(err).To(MatchError(captureErr))
		Expect(readFileStringGinkgo(sourcePath)).To(SatisfyAll(
			ContainSubstring("[B](/docs/b)\n"),
			Not(ContainSubstring("[B](/docs/b.md)")),
		))
		page, err := treeService.GetPage("page-a")
		Expect(err).To(Succeed())
		Expect(page.RawContent).To(SatisfyAll(
			ContainSubstring("[B](/docs/b)\n"),
			Not(ContainSubstring("[B](/docs/b.md)")),
		))
	})
})

var _ = Describe("workspace sync failure during canonical writeback", Label("integration"), func() {
	It("reports rewrite failure before derived indexes rebuild", func() {
		dataDir := workspaceSyncTempDir()
		rootDir := filepath.Join(workspaceSyncTempDir(), "workspace")
		treeService := tree.NewTreeServiceWithOptions(tree.TreeOptions{DataDir: dataDir, RootDir: rootDir})
		Expect(treeService.LoadTree()).To(Succeed())
		sourcePath := filepath.Join(rootDir, "docs", "a.md")
		sourceOriginal := `---
leafwiki_id: page-a
leafwiki_title: Page A
---
# Page A

[B](/docs/b)
`
		writeMarkdownFile(sourcePath, sourceOriginal)
		writeMarkdownFile(filepath.Join(rootDir, "docs", "b.md"), `---
leafwiki_id: page-b
leafwiki_title: Page B
---
# Page B
`)

		writeErr := errors.New("canonical migration write failed")
		previousWriter := canonicalMarkdownRewriteWriter
		canonicalMarkdownRewriteWriter = func([]canonicalMarkdownRewrite) error {
			return writeErr
		}
		DeferCleanup(func() {
			canonicalMarkdownRewriteWriter = previousWriter
		})

		derivedRebuilds := 0
		service, err := NewService(ServiceOptions{
			Enabled: true,
			DataDir: dataDir,
			RootDir: rootDir,
			Tree:    treeService,
			Store:   &fakeRevisionStore{capture: &gitrevisions.Commit{Hash: "initial-commit"}},
			AfterSync: func() error {
				derivedRebuilds++
				return nil
			},
		})
		Expect(err).To(Succeed())

		_, err = service.SyncNow(context.Background(), SyncRequest{
			Reason: ReasonExplicit,
			Source: SourceFilesystem,
			Actor:  PublicEditorActor(),
		})

		Expect(err).To(MatchError(writeErr))
		Expect(readFileStringGinkgo(sourcePath)).To(SatisfyAll(
			ContainSubstring("[B](/docs/b)\n"),
			Not(ContainSubstring("[B](/docs/b.md)")),
		))
		page, err := treeService.GetPage("page-a")
		Expect(err).To(Succeed())
		Expect(page.RawContent).To(SatisfyAll(
			ContainSubstring("[B](/docs/b)\n"),
			Not(ContainSubstring("[B](/docs/b.md)")),
		))
		Expect(derivedRebuilds).To(BeZero())
	})

	It("keeps failed metadata writeback out of tree state and revision capture", func() {
		dataDir := workspaceSyncTempDir()
		rootDir := filepath.Join(workspaceSyncTempDir(), "workspace")
		treeService := tree.NewTreeServiceWithOptions(tree.TreeOptions{DataDir: dataDir, RootDir: rootDir})
		Expect(treeService.LoadTree()).To(Succeed())
		sourcePath := filepath.Join(rootDir, "legacy-metadata.md")
		writeMarkdownFile(sourcePath, `---
leafwiki_id: legacy-metadata
leafwiki_title: Legacy Metadata
leafwiki_created_at: "2026-06-13T10:00:00Z"
leafwiki_updated_at: "2026-06-13T11:00:00Z"
---
# Legacy Metadata

body
`)
		Expect(os.Chmod(rootDir, 0o500)).To(Succeed())
		DeferCleanup(func() {
			_ = os.Chmod(rootDir, 0o700)
		})

		store := &fakeRevisionStore{capture: &gitrevisions.Commit{Hash: "initial-commit"}}
		derivedRebuilds := 0
		service, err := NewService(ServiceOptions{
			Enabled: true,
			DataDir: dataDir,
			RootDir: rootDir,
			Tree:    treeService,
			Store:   store,
			AfterSync: func() error {
				derivedRebuilds++
				return nil
			},
		})
		Expect(err).To(Succeed())

		status, err := service.SyncNow(context.Background(), SyncRequest{
			Reason: ReasonExplicit,
			Source: SourceFilesystem,
			Actor:  PublicEditorActor(),
		})

		Expect(err).To(Succeed())
		Expect(status.LastError).To(ContainSubstring(filepath.Base(sourcePath)))
		Expect(derivedRebuilds).To(BeZero())
		Expect(store.captureCalls).To(Equal(1))
		_, err = treeService.GetPage("legacy-metadata")
		Expect(err).To(MatchError(tree.ErrPageNotFound))
		Expect(readFileStringGinkgo(sourcePath)).NotTo(HavePrefix("<!-- leafwiki\n"))
	})
})

var _ = Describe("canonical link validation during workspace sync", Label("integration"), func() {
	It("does not create another migration revision after links are canonical", func() {
		dataDir := workspaceSyncTempDir()
		rootDir := filepath.Join(workspaceSyncTempDir(), "workspace")
		treeService := tree.NewTreeServiceWithOptions(tree.TreeOptions{DataDir: dataDir, RootDir: rootDir})
		Expect(treeService.LoadTree()).To(Succeed())
		writeMarkdownFile(filepath.Join(rootDir, "a.md"), `---
leafwiki_id: page-a
leafwiki_title: Page A
---
# Page A

[B](/b)
`)
		writeMarkdownFile(filepath.Join(rootDir, "b.md"), `---
leafwiki_id: page-b
leafwiki_title: Page B
---
# Page B
`)

		service, err := NewService(ServiceOptions{
			Enabled: true,
			DataDir: dataDir,
			RootDir: rootDir,
			Tree:    treeService,
		})
		Expect(err).To(Succeed())
		for range 2 {
			_, err := service.SyncNow(context.Background(), SyncRequest{
				Reason: ReasonExplicit,
				Source: SourceFilesystem,
				Actor:  PublicEditorActor(),
			})
			Expect(err).To(Succeed())
		}

		snapshots, err := service.ListSnapshots(context.Background(), 10)
		Expect(err).To(Succeed())
		Expect(snapshots).To(HaveLen(2))
	})

	It("keeps unresolved legacy links in content and reports a broken link issue", func() {
		dataDir := workspaceSyncTempDir()
		rootDir := filepath.Join(workspaceSyncTempDir(), "workspace")
		treeService := tree.NewTreeServiceWithOptions(tree.TreeOptions{DataDir: dataDir, RootDir: rootDir})
		Expect(treeService.LoadTree()).To(Succeed())
		sourcePath := filepath.Join(rootDir, "docs", "a.md")
		writeMarkdownFile(sourcePath, `---
leafwiki_id: page-a
leafwiki_title: Page A
---
# Page A

[Missing](/docs/missing)
`)

		service, err := NewService(ServiceOptions{
			Enabled: true,
			DataDir: dataDir,
			RootDir: rootDir,
			Tree:    treeService,
		})
		Expect(err).To(Succeed())

		status, err := service.SyncNow(context.Background(), SyncRequest{
			Reason: ReasonExplicit,
			Source: SourceFilesystem,
			Actor:  PublicEditorActor(),
		})
		Expect(err).To(Succeed())

		Expect(readFileStringGinkgo(sourcePath)).To(ContainSubstring("[Missing](/docs/missing)"))
		Expect(status.ValidationErrors).To(SatisfyAll(
			HaveLen(1),
			testmatchers.HaveValidationIssue(wikivalidation.IssueCodeBrokenLink),
			ContainElement(HaveField("Path", Equal("docs/a"))),
		))
	})

	It("leaves invalid canonical links unchanged and reports invalid link issues", func() {
		dataDir := workspaceSyncTempDir()
		workspaceParent := workspaceSyncTempDir()
		rootDir := filepath.Join(workspaceParent, "workspace")
		treeService := tree.NewTreeServiceWithOptions(tree.TreeOptions{DataDir: dataDir, RootDir: rootDir})
		Expect(treeService.LoadTree()).To(Succeed())
		Expect(os.WriteFile(filepath.Join(workspaceParent, "outside.md"), []byte("---\nleafwiki_id: outside\nleafwiki_title: Outside\n---\n# Outside\n"), 0o644)).To(Succeed())
		sourcePath := filepath.Join(rootDir, "docs", "a.md")
		sourceOriginal := `---
leafwiki_id: page-a
leafwiki_title: Page A
---
# Page A

[Bad Encoding](/docs/%zz)
[Escape](../../outside.md)
`
		writeMarkdownFile(sourcePath, sourceOriginal)

		service, err := NewService(ServiceOptions{
			Enabled: true,
			DataDir: dataDir,
			RootDir: rootDir,
			Tree:    treeService,
		})
		Expect(err).To(Succeed())

		status, err := service.SyncNow(context.Background(), SyncRequest{
			Reason: ReasonExplicit,
			Source: SourceFilesystem,
			Actor:  PublicEditorActor(),
		})
		Expect(err).To(Succeed())
		for _, originalLink := range []string{"[Bad Encoding](/docs/%zz)", "[Escape](../../outside.md)"} {
			Expect(readFileStringGinkgo(sourcePath)).To(ContainSubstring(originalLink))
		}
		page, err := treeService.GetPage("page-a")
		Expect(err).To(Succeed())
		for _, originalLink := range []string{"[Bad Encoding](/docs/%zz)", "[Escape](../../outside.md)"} {
			Expect(page.RawContent).To(ContainSubstring(originalLink))
		}
		Expect(status.ValidationErrors).To(SatisfyAll(
			HaveLen(2),
			testmatchers.HaveValidationIssues(wikivalidation.IssueCodeInvalidLink, wikivalidation.IssueCodeInvalidLink),
			HaveEach(HaveField("Path", Equal("docs/a"))),
		))
	})
})
