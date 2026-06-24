package mcp

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	wikivalidation "github.com/perber/wiki/internal/core/markdownvalidation"
	"github.com/perber/wiki/internal/core/tree"
)

func TestCachedValidationAssetExistsBuildsPredicateOncePerPage(t *testing.T) {
	calls := map[tree.PageID]int{}
	assetExists := cachedValidationAssetExists(func(pageID tree.PageID) func(string) bool {
		calls[pageID]++
		return func(destination string) bool {
			return pageID == newFixturePageID("page-1") && destination == "logo.png"
		}
	})

	if !assetExists(newFixturePageID("page-1"), "logo.png") {
		t.Fatalf("assetExists(page-1, logo.png) = false, want true")
	}
	if assetExists(newFixturePageID("page-1"), "other.png") {
		t.Fatalf("assetExists(page-1, other.png) = true, want false")
	}
	if assetExists(newFixturePageID("page-2"), "logo.png") {
		t.Fatalf("assetExists(page-2, logo.png) = true, want false")
	}
	assetExists(newFixturePageID("page-1"), "second.png")

	pageOneID := newFixturePageID("page-1")
	pageTwoID := newFixturePageID("page-2")
	if calls[pageOneID] != 1 {
		t.Fatalf("page-1 predicate factory calls = %d, want 1", calls[pageOneID])
	}
	if calls[pageTwoID] != 1 {
		t.Fatalf("page-2 predicate factory calls = %d, want 1", calls[pageTwoID])
	}
}

func TestValidateMarkdownContentUsesSectionSourceForSameBasenameTwin(t *testing.T) {
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
	if err := treeService.LoadTree(); err != nil {
		t.Fatalf("LoadTree failed: %v", err)
	}
	moveChildKindFirst(t, treeService.GetTree(), "docs", tree.NodeKindPage)
	section, err := treeService.GetPage("sync-section")
	if err != nil {
		t.Fatalf("GetPage section failed: %v", err)
	}
	routes := &Routes{treeService: treeService}

	routePath := newFixtureRoutePath(section.CalculatePath())
	result := routes.validateMarkdownContent(context.Background(), routePath, section.RawContent, newFixturePageID(section.ID), section.Kind)

	if !result.OK {
		t.Fatalf("validateMarkdownContent = %#v, want ok", result)
	}
	assertNoCoreValidationIssueCode(t, result, wikivalidation.IssueCodeBrokenLink)
}

func TestValidateWorkspaceMarkdownFilesResolvesMarkdownLinkRootPrefix(t *testing.T) {
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

	if !result.OK {
		t.Fatalf("validateWorkspaceMarkdownFiles = %#v, want ok", result)
	}
	assertNoCoreValidationIssueCode(t, result, wikivalidation.IssueCodeBrokenLink)
}

func moveChildKindFirst(t *testing.T, root *tree.PageNode, routePath string, kind tree.NodeKind) {
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

func writeValidationMarkdown(t *testing.T, filePath string, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(filePath), 0o755); err != nil {
		t.Fatalf("MkdirAll %s: %v", filepath.Dir(filePath), err)
	}
	if err := os.WriteFile(filePath, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile %s: %v", filePath, err)
	}
}

func assertNoCoreValidationIssueCode(t *testing.T, result wikivalidation.Result, code wikivalidation.IssueCode) {
	t.Helper()
	for _, issue := range result.Issues {
		if issue.Code == code {
			t.Fatalf("unexpected validation issue %q in %#v", code, result.Issues)
		}
	}
}
