package workspacesync

import (
	"context"
	"path/filepath"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/perber/wiki/internal/core/markdownlinks"
	wikivalidation "github.com/perber/wiki/internal/core/markdownvalidation"
	"github.com/perber/wiki/internal/core/tree"
	testmatchers "github.com/perber/wiki/internal/test_utils/matchers"
)

var _ = Describe("ambiguous legacy markdown links during workspace sync", Label("integration"), func() {
	It("keeps ambiguous extensionless links unchanged and reports an ambiguity issue", func() {
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

[Sync](/docs/sync)
`
		writeMarkdownFile(sourcePath, sourceOriginal)
		writeMarkdownFile(filepath.Join(rootDir, "docs", "sync.md"), `---
leafwiki_id: sync-page
leafwiki_title: Sync Page
---
# Sync Page
`)
		writeMarkdownFile(filepath.Join(rootDir, "docs", "sync", "index.md"), `---
leafwiki_id: sync-section
leafwiki_title: Sync Section
---
# Sync Section
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
		Expect(readFileStringGinkgo(sourcePath)).To(ContainSubstring("[Sync](/docs/sync)"))
		page, err := treeService.GetPage(newFixturePageID("page-a"))
		Expect(err).To(Succeed())
		Expect(page.RawContent).To(ContainSubstring("[Sync](/docs/sync)"))
		Expect(status.ValidationErrors).To(SatisfyAll(
			testmatchers.HaveValidationIssue(wikivalidation.IssueCodeAmbiguousLegacyLink),
			ContainElement(HaveField("Path", Equal("docs/a"))),
		))
	})

	It("keeps ambiguity diagnostics when normal validation also finds broken links", func() {
		dataDir := workspaceSyncTempDir()
		rootDir := filepath.Join(workspaceSyncTempDir(), "workspace")
		treeService := tree.NewTreeServiceWithOptions(tree.TreeOptions{DataDir: dataDir, RootDir: rootDir})
		Expect(treeService.LoadTree()).To(Succeed())
		writeMarkdownFile(filepath.Join(rootDir, "docs", "a.md"), `---
leafwiki_id: page-a
leafwiki_title: Page A
---
# Page A

[Sync](/docs/sync)
[Missing](/docs/missing)
`)
		writeMarkdownFile(filepath.Join(rootDir, "docs", "sync.md"), `---
leafwiki_id: sync-page
leafwiki_title: Sync Page
---
# Sync Page
`)
		writeMarkdownFile(filepath.Join(rootDir, "docs", "sync", "index.md"), `---
leafwiki_id: sync-section
leafwiki_title: Sync Section
---
# Sync Section
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

		Expect(status.ValidationErrors).To(testmatchers.HaveValidationIssues(
			wikivalidation.IssueCodeAmbiguousLegacyLink,
			wikivalidation.IssueCodeBrokenLink,
		))
	})

	It("normalizes ambiguous link validation paths to route paths", func() {
		validationErrors := canonicalMigrationValidationErrors(workspaceSyncTempDir(), "plans/agent_hooks.PLAN.md", []markdownlinks.Issue{{
			Code:        markdownlinks.IssueCodeAmbiguousLegacyLink,
			Destination: "/plans/sync",
		}})

		Expect(validationErrors).To(SatisfyAll(
			HaveLen(1),
			testmatchers.HaveValidationIssue(wikivalidation.IssueCodeAmbiguousLegacyLink),
			ContainElement(HaveField("Path", Equal("plans/agent-hooks-plan"))),
		))
	})
})
