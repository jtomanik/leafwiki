package mcp

import (
	"context"
	"os"
	"path/filepath"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/gcustom"
	"github.com/onsi/gomega/gstruct"
	"github.com/onsi/gomega/types"
	wikivalidation "github.com/perber/wiki/internal/core/markdownvalidation"
	"github.com/perber/wiki/internal/core/tree"
	"github.com/perber/wiki/internal/workspacesync"
)

const validationDuplicateLeafwikiIDMessage = "duplicate leafwiki_id"

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

		Expect(calls).To(HaveKeyWithValue(newFixturePageID("page-1"), 1))
		Expect(calls).To(HaveKeyWithValue(newFixturePageID("page-2"), 1))
	})

	It("uses the section source for same-basename markdown twins", func() {
		dataDir := mcpTestTempDir()
		rootDir := filepath.Join(mcpTestTempDir(), "workspace")
		writeValidationMarkdown(filepath.Join(rootDir, "docs", "sync.md"), `---
leafwiki_id: sync-page
leafwiki_title: Sync Page
---
# Sync Page
`)
		writeValidationMarkdown(filepath.Join(rootDir, "docs", "sync", "index.md"), `---
leafwiki_id: sync-section
leafwiki_title: Sync Section
---
# Sync Section

[Child](./child.md)
`)
		writeValidationMarkdown(filepath.Join(rootDir, "docs", "sync", "child.md"), `---
leafwiki_id: sync-child
leafwiki_title: Sync Child
---
# Sync Child
`)

		treeService := tree.NewTreeServiceWithOptions(tree.TreeOptions{DataDir: dataDir, RootDir: rootDir})
		Expect(treeService.LoadTree()).To(Succeed())
		Expect(moveChildKindFirst(treeService.GetTree(), "docs", tree.NodeKindPage)).To(Equal(validationChildKindMoved))
		section, err := treeService.GetPage("sync-section")
		Expect(err).NotTo(HaveOccurred())
		routes := &Routes{treeService: treeService}

		routePath := tree.RoutePathFromString(section.CalculatePath())
		result := routes.validateMarkdownContent(context.Background(), routePath, section.RawContent, tree.PageIDFromString(section.ID), section.Kind)

		Expect(result).To(matchMarkdownValidationWithoutIssue(wikivalidation.IssueCodeBrokenLink), "validateMarkdownContent = %#v", result)
	})

	It("resolves markdown link root prefixes while validating workspace files", func() {
		rootDir := filepath.Join(mcpTestTempDir(), "repo", "docs")
		writeValidationMarkdown(filepath.Join(rootDir, "index.md"), `---
leafwiki_id: root
leafwiki_title: Root
---
# Root

[Glossary](/docs/sync/glossary.md)
`)
		writeValidationMarkdown(filepath.Join(rootDir, "sync", "glossary.md"), `---
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

		Expect(result).To(matchMarkdownValidationWithoutIssue(wikivalidation.IssueCodeBrokenLink), "validateWorkspaceMarkdownFiles = %#v", result)
	})

	It("validates loaded tree content when no workspace root is available", func() {
		treeService := tree.NewTreeServiceWithOptions(tree.TreeOptions{DataDir: mcpTestTempDir(), RootDir: mcpTestTempDir()})
		Expect(treeService.LoadTree()).To(Succeed())
		pageID, err := treeService.CreateNode("system", nil, "Broken", "broken", nil)
		Expect(err).NotTo(HaveOccurred())
		content := "# Broken\n\n[Missing](missing.md)\n"
		Expect(treeService.UpdateNodeUncheckedVersion("system", *pageID, "Broken", "broken", &content, false)).To(Succeed())

		result := (&Routes{treeService: treeService}).validateLoadedTree(context.Background())

		Expect(result).To(matchMarkdownValidationWithIssue(wikivalidation.IssueCodeBrokenLink), "validateLoadedTree = %#v", result)
	})

	It("preserves workspace sync validation status fields for wiki validation", func() {
		issues := workspaceStatusIssues([]workspacesync.ValidationError{{
			Code:      wikivalidation.IssueCodeDuplicateLeafwikiID,
			Path:      "docs/a.md",
			MessageID: wikivalidation.IssueCodeDuplicateLeafwikiID.MessageID(),
			Message:   validationDuplicateLeafwikiIDMessage,
			Severity:  wikivalidation.IssueSeverityError,
		}})

		Expect(issues).To(HaveExactElements(gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
			"Code":      Equal(wikivalidation.IssueCodeDuplicateLeafwikiID),
			"Path":      Equal("docs/a.md"),
			"MessageID": Equal(wikivalidation.IssueCodeDuplicateLeafwikiID.MessageID()),
			"Message":   Equal(validationDuplicateLeafwikiIDMessage),
			"Severity":  Equal(wikivalidation.IssueSeverityError),
		})))
	})

	It("resolves validation source markdown files and page IDs for sections", func() {
		rootDir := filepath.Join(mcpTestTempDir(), "workspace")
		writeValidationMarkdown(filepath.Join(rootDir, "docs", "index.md"), `---
leafwiki_id: docs-section
leafwiki_title: Docs
---
# Docs
`)
		treeService := tree.NewTreeServiceWithOptions(tree.TreeOptions{DataDir: mcpTestTempDir(), RootDir: rootDir})
		Expect(treeService.LoadTree()).To(Succeed())
		routes := &Routes{treeService: treeService}

		source := routes.validationSourceMarkdownFile("docs")
		Expect(source).To(Equal(tree.MarkdownPathFromString("docs/index.md")))
		Expect(validationPageIDResolutionFor(routes, "docs")).To(matchValidationPageID(validationResolved, Equal(newFixturePageID("docs-section"))))
	})

	It("normalizes validation asset destinations before lookup", func() {
		Expect(cleanValidationAssetDestination(" <logo.png?size=1#preview> ")).To(Equal("logo.png"))
		Expect(cleanValidationAssetDestination("assets/page/logo.png#preview")).To(Equal("assets/page/logo.png"))
	})
})

type validationChildKindMoveState string

const (
	validationChildKindMoved      validationChildKindMoveState = "moved child kind first"
	validationChildRouteMissing   validationChildKindMoveState = "route missing"
	validationChildKindNotPresent validationChildKindMoveState = "child kind not present"
)

func moveChildKindFirst(root *tree.PageNode, routePath string, kind tree.NodeKind) validationChildKindMoveState {
	GinkgoHelper()
	parent := root
	for _, slug := range strings.Split(strings.Trim(routePath, "/"), "/") {
		if slug == "" {
			continue
		}
		parent = childBySlug(parent, slug)
		if parent == nil {
			return validationChildRouteMissing
		}
	}
	for i, child := range parent.Children {
		if child.Kind != kind {
			continue
		}
		parent.Children = append([]*tree.PageNode{child}, append(parent.Children[:i], parent.Children[i+1:]...)...)
		return validationChildKindMoved
	}
	return validationChildKindNotPresent
}

func childBySlug(parent *tree.PageNode, slug string) *tree.PageNode {
	if parent == nil {
		return nil
	}
	for _, child := range parent.Children {
		if child.Slug == tree.SlugFromString(slug) {
			return child
		}
	}
	return nil
}

func writeValidationMarkdown(filePath string, content string) {
	GinkgoHelper()
	Expect(os.MkdirAll(filepath.Dir(filePath), 0o755)).To(Succeed())
	Expect(os.WriteFile(filePath, []byte(content), 0o644)).To(Succeed())
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

func matchMarkdownValidationIssue(code wikivalidation.IssueCode) types.GomegaMatcher {
	GinkgoHelper()
	return gcustom.MakeMatcher(func(issue wikivalidation.Issue) (bool, error) {
		return issue.Code == code, nil
	}).WithTemplate("Expected:\n{{.FormattedActual}}\n{{.To}} match markdown validation issue")
}
