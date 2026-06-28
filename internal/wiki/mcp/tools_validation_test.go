package mcp

import (
	"context"
	"os"
	"path/filepath"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	wikivalidation "github.com/perber/wiki/internal/core/markdownvalidation"
	sharederrors "github.com/perber/wiki/internal/core/shared/errors"
	"github.com/perber/wiki/internal/core/tree"
	"github.com/perber/wiki/internal/workspacesync"
)

var _ = Describe("Validation tool helpers", func() {
	It("caches one asset predicate per page", func() {
		calls := map[tree.PageID]int{}
		assetExists := cachedValidationAssetExists(func(pageID tree.PageID) func(string) bool {
			calls[pageID]++
			return func(destination string) bool {
				return pageID == newFixturePageID("page-1") && destination == "logo.png"
			}
		})

		Expect(assetExists(newFixturePageID("page-1"), "logo.png")).To(BeTrue())
		Expect(assetExists(newFixturePageID("page-1"), "other.png")).To(BeFalse())
		Expect(assetExists(newFixturePageID("page-2"), "logo.png")).To(BeFalse())
		assetExists(newFixturePageID("page-1"), "second.png")

		Expect(calls[newFixturePageID("page-1")]).To(Equal(1))
		Expect(calls[newFixturePageID("page-2")]).To(Equal(1))
	})

	It("uses the section source for same-basename markdown twins", func() {
		t := GinkgoT()
		dataDir := t.TempDir()
		rootDir := filepath.Join(t.TempDir(), "workspace")
		writeValidationMarkdown(t, filepath.Join(rootDir, "docs", "sync.md"), `---
leafwiki_id: sync-page
leafwiki_title: Sync Page
---
# Sync Page
`)
		writeValidationMarkdown(t, filepath.Join(rootDir, "docs", "sync", "index.md"), `---
leafwiki_id: sync-section
leafwiki_title: Sync Section
---
# Sync Section

[Child](./child.md)
`)
		writeValidationMarkdown(t, filepath.Join(rootDir, "docs", "sync", "child.md"), `---
leafwiki_id: sync-child
leafwiki_title: Sync Child
---
# Sync Child
`)

		treeService := tree.NewTreeServiceWithOptions(tree.TreeOptions{DataDir: dataDir, RootDir: rootDir})
		Expect(treeService.LoadTree()).To(Succeed())
		moveChildKindFirst(t, treeService.GetTree(), "docs", tree.NodeKindPage)
		section, err := treeService.GetPage("sync-section")
		Expect(err).NotTo(HaveOccurred())
		routes := &Routes{treeService: treeService}

		routePath := newFixtureRoutePath(section.CalculatePath())
		result := routes.validateMarkdownContent(context.Background(), routePath, section.RawContent, newFixturePageID(section.ID), section.Kind)

		Expect(result.OK).To(BeTrue(), "validateMarkdownContent = %#v", result)
		assertNoCoreValidationIssueCode(t, result, wikivalidation.IssueCodeBrokenLink)
	})

	It("resolves markdown link root prefixes while validating workspace files", func() {
		t := GinkgoT()
		rootDir := filepath.Join(t.TempDir(), "repo", "docs")
		writeValidationMarkdown(t, filepath.Join(rootDir, "index.md"), `---
leafwiki_id: root
leafwiki_title: Root
---
# Root

[Glossary](/docs/sync/glossary.md)
`)
		writeValidationMarkdown(t, filepath.Join(rootDir, "sync", "glossary.md"), `---
leafwiki_id: glossary
leafwiki_title: Glossary
---
# Glossary
`)
		routes := &Routes{
			workspaceRootDir:       rootDir,
			markdownLinkRootPrefix: "/docs",
		}

		result := routes.validateWorkspaceMarkdownFiles(context.Background(), false)

		Expect(result.OK).To(BeTrue(), "validateWorkspaceMarkdownFiles = %#v", result)
		assertNoCoreValidationIssueCode(t, result, wikivalidation.IssueCodeBrokenLink)
	})

	It("validates loaded tree content when no workspace root is available", func() {
		t := GinkgoT()
		treeService := tree.NewTreeServiceWithOptions(tree.TreeOptions{DataDir: t.TempDir(), RootDir: t.TempDir()})
		Expect(treeService.LoadTree()).To(Succeed())
		pageID, err := treeService.CreateNode("system", nil, "Broken", "broken", nil)
		Expect(err).NotTo(HaveOccurred())
		content := "# Broken\n\n[Missing](missing.md)\n"
		Expect(treeService.UpdateNodeUncheckedVersion("system", *pageID, "Broken", "broken", &content, false)).To(Succeed())

		result := (&Routes{treeService: treeService}).validateLoadedTree(context.Background())

		Expect(result.OK).To(BeFalse(), "validateLoadedTree = %#v", result)
		Expect(validationIssueCodeCount(result, wikivalidation.IssueCodeBrokenLink)).To(BeNumerically(">", 0))
	})

	It("preserves workspace sync validation status fields for wiki validation", func() {
		issues := workspaceStatusIssues([]workspacesync.ValidationError{{
			Code:      wikivalidation.IssueCodeDuplicateLeafwikiID,
			Path:      "docs/a.md",
			MessageID: wikivalidation.IssueCodeDuplicateLeafwikiID.MessageID(),
			Message:   "duplicate leafwiki_id",
			Severity:  "error",
		}})

		Expect(issues).To(HaveLen(1))
		Expect(issues[0].Code).To(Equal(wikivalidation.IssueCodeDuplicateLeafwikiID))
		Expect(issues[0].Path).To(Equal("docs/a.md"))
		Expect(issues[0].MessageID).To(Equal(sharederrors.MessageID("validation.markdown.duplicate_leafwiki_id")))
		Expect(issues[0].Message).To(Equal("duplicate leafwiki_id"))
		Expect(issues[0].Severity).To(Equal(wikivalidation.IssueSeverity("error")))
	})

	It("resolves validation source markdown files and page IDs for sections", func() {
		t := GinkgoT()
		rootDir := filepath.Join(t.TempDir(), "workspace")
		writeValidationMarkdown(t, filepath.Join(rootDir, "docs", "index.md"), `---
leafwiki_id: docs-section
leafwiki_title: Docs
---
# Docs
`)
		treeService := tree.NewTreeServiceWithOptions(tree.TreeOptions{DataDir: t.TempDir(), RootDir: rootDir})
		Expect(treeService.LoadTree()).To(Succeed())
		routes := &Routes{treeService: treeService}

		source := routes.validationSourceMarkdownFile("docs")
		pageID, ok := routes.resolveValidationPageID("docs")

		Expect(source).To(Equal(tree.MarkdownPathFromString("docs/index.md")))
		Expect(ok).To(BeTrue())
		Expect(pageID).To(Equal(newFixturePageID("docs-section")))
	})

	It("normalizes validation asset destinations before lookup", func() {
		Expect(cleanValidationAssetDestination(" <logo.png?size=1#preview> ")).To(Equal("logo.png"))
		Expect(cleanValidationAssetDestination("assets/page/logo.png#preview")).To(Equal("assets/page/logo.png"))
	})
})

type validationTestT interface {
	Helper()
	Fatalf(format string, args ...any)
}

func moveChildKindFirst(t validationTestT, root *tree.PageNode, routePath string, kind tree.NodeKind) {
	t.Helper()
	parent := root
	for _, slug := range strings.Split(strings.Trim(routePath, "/"), "/") {
		if slug == "" {
			continue
		}
		parent = childBySlug(parent, slug)
		if parent == nil {
			t.Fatalf("route %q not found", routePath)
		}
	}
	for i, child := range parent.Children {
		if child.Kind != kind {
			continue
		}
		parent.Children = append([]*tree.PageNode{child}, append(parent.Children[:i], parent.Children[i+1:]...)...)
		return
	}
	t.Fatalf("child kind %q not found under %q", kind, parent.ID)
}

func childBySlug(parent *tree.PageNode, slug string) *tree.PageNode {
	if parent == nil {
		return nil
	}
	for _, child := range parent.Children {
		if child.Slug == newFixtureSlug(slug) {
			return child
		}
	}
	return nil
}

func writeValidationMarkdown(t validationTestT, filePath string, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(filePath), 0o755); err != nil {
		t.Fatalf("MkdirAll %s: %v", filepath.Dir(filePath), err)
	}
	if err := os.WriteFile(filePath, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile %s: %v", filePath, err)
	}
}

func assertNoCoreValidationIssueCode(t validationTestT, result wikivalidation.Result, code wikivalidation.IssueCode) {
	t.Helper()
	for _, issue := range result.Issues {
		if issue.Code == code {
			t.Fatalf("unexpected validation issue %q in %#v", code, result.Issues)
		}
	}
}

func validationIssueCodeCount(result wikivalidation.Result, code wikivalidation.IssueCode) int {
	count := 0
	for _, issue := range result.Issues {
		if issue.Code == code {
			count++
		}
	}
	return count
}
