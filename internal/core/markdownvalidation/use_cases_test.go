package markdownvalidation

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/perber/wiki/internal/core/tree"
)

// Canonical Markdown links plan scenarios covered by tests in this file:
// - Malformed percent-encoding reports an invalid link instead of panicking
// - Case mismatch is invalid
// - Old extensionless page link is reported as non-canonical when migration cannot resolve it

func TestValidateWorkspaceMarkdownFilesResolvesSectionDirectoryRoutes(t *testing.T) {
	rootDir := t.TempDir()
	docsDir := filepath.Join(rootDir, "docs")
	if err := os.MkdirAll(docsDir, 0o755); err != nil {
		t.Fatalf("create docs dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(docsDir, "child.md"), []byte("---\nleafwiki_id: docs-child\nleafwiki_title: Docs Child\n---\n# Docs Child\n\n[Docs section](/docs)\n"), 0o644); err != nil {
		t.Fatalf("write child markdown: %v", err)
	}

	result := ValidateWorkspaceMarkdownFiles(WorkspaceMarkdownValidationOptions{RootDir: rootDir})

	if !result.OK {
		t.Fatalf("validation = %#v, want link to section directory route to resolve", result)
	}
}

func TestValidateWorkspaceMarkdownFilesAllowsRootIndexRoute(t *testing.T) {
	rootDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(rootDir, "index.md"), []byte("---\nleafwiki_id: root-page\nleafwiki_title: Root Page\n---\n# Root Page\n\nRoot index content\n"), 0o644); err != nil {
		t.Fatalf("write root index markdown: %v", err)
	}

	result := ValidateWorkspaceMarkdownFiles(WorkspaceMarkdownValidationOptions{RootDir: rootDir})

	if !result.OK {
		t.Fatalf("validation = %#v, want root index.md to validate as root route", result)
	}
}

func TestValidateWorkspaceMarkdownFilesAllowsRootReadmeFallbackRoute(t *testing.T) {
	rootDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(rootDir, "README.md"), []byte("---\nleafwiki_id: root\nleafwiki_title: Root Page\n---\n# Root Page\n\nRoot README content\n"), 0o644); err != nil {
		t.Fatalf("write root README markdown: %v", err)
	}

	result := ValidateWorkspaceMarkdownFiles(WorkspaceMarkdownValidationOptions{RootDir: rootDir})

	if !result.OK {
		t.Fatalf("validation = %#v, want root README.md fallback to validate as root route", result)
	}
}

func TestValidateWorkspaceMarkdownFiles_ResolvesCanonicalPageMdAndSectionLinks(t *testing.T) {
	rootDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(rootDir, "docs", "sync"), 0o755); err != nil {
		t.Fatalf("create sync dir: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(rootDir, "guides"), 0o755); err != nil {
		t.Fatalf("create guides dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(rootDir, "docs", "a.md"), []byte("---\nleafwiki_id: docs-a\nleafwiki_title: Docs A\n---\n# Docs A\n\n[B](/docs/b.md)\n[Sync](/docs/sync)\n[Sync Index](/docs/sync/index.md)\n[Guide Readme](/guides/README.md)\n"), 0o644); err != nil {
		t.Fatalf("write source markdown: %v", err)
	}
	if err := os.WriteFile(filepath.Join(rootDir, "docs", "b.md"), []byte("---\nleafwiki_id: docs-b\nleafwiki_title: Docs B\n---\n# Docs B\n"), 0o644); err != nil {
		t.Fatalf("write page target markdown: %v", err)
	}
	if err := os.WriteFile(filepath.Join(rootDir, "docs", "sync", "index.md"), []byte("---\nleafwiki_id: docs-sync\nleafwiki_title: Docs Sync\n---\n# Docs Sync\n"), 0o644); err != nil {
		t.Fatalf("write section index markdown: %v", err)
	}
	if err := os.WriteFile(filepath.Join(rootDir, "guides", "README.md"), []byte("---\nleafwiki_id: guides\nleafwiki_title: Guides\n---\n# Guides\n"), 0o644); err != nil {
		t.Fatalf("write README fallback markdown: %v", err)
	}

	result := ValidateWorkspaceMarkdownFiles(WorkspaceMarkdownValidationOptions{RootDir: rootDir})

	if !result.OK {
		t.Fatalf("validation = %#v, want canonical page and section links to resolve", result)
	}
}

func TestValidateWorkspaceMarkdownFiles_IgnoresProtocolRelativeAndSchemedURLs(t *testing.T) {
	rootDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(rootDir, "source.md"), []byte("---\nleafwiki_id: source\nleafwiki_title: Source\n---\n# Source\n\n[CDN](//cdn.example.com/lib.md)\n[Obsidian](obsidian://open?vault=wiki)\n"), 0o644); err != nil {
		t.Fatalf("write source markdown: %v", err)
	}

	result := ValidateWorkspaceMarkdownFiles(WorkspaceMarkdownValidationOptions{RootDir: rootDir})

	if !result.OK {
		t.Fatalf("validation = %#v, want protocol-relative and schemed URLs to be external", result)
	}
}

func TestValidateWorkspaceMarkdownFilesRequiresFilesystemTargets(t *testing.T) {
	rootDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(rootDir, "source.md"), []byte("---\nleafwiki_id: source\nleafwiki_title: Source\n---\n# Source\n\n[Stale](/stale-target)\n"), 0o644); err != nil {
		t.Fatalf("write source markdown: %v", err)
	}

	result := ValidateWorkspaceMarkdownFiles(WorkspaceMarkdownValidationOptions{RootDir: rootDir})

	if result.OK {
		t.Fatalf("validation = %#v, want missing filesystem target to fail", result)
	}
	if len(result.Issues) != 1 || result.Issues[0].Code != "broken_link" {
		t.Fatalf("issues = %#v, want one broken_link", result.Issues)
	}
	if result.Issues[0].Path != "source" {
		t.Fatalf("issue path = %q, want source page path", result.Issues[0].Path)
	}
}

// - Malformed percent-encoding reports an invalid link instead of panicking
func TestValidateWorkspaceMarkdownFilesReportsInvalidCanonicalLinks(t *testing.T) {
	workspaceParent := t.TempDir()
	rootDir := filepath.Join(workspaceParent, "workspace")
	if err := os.MkdirAll(rootDir, 0o755); err != nil {
		t.Fatalf("create workspace root: %v", err)
	}
	if err := os.WriteFile(filepath.Join(workspaceParent, "outside.md"), []byte("---\nleafwiki_id: outside\nleafwiki_title: Outside\n---\n# Outside\n"), 0o644); err != nil {
		t.Fatalf("write outside markdown: %v", err)
	}
	if err := os.WriteFile(filepath.Join(rootDir, "source.md"), []byte("---\nleafwiki_id: source\nleafwiki_title: Source\n---\n# Source\n\n[Bad Encoding](/docs/%zz)\n[Escape](../outside.md)\n"), 0o644); err != nil {
		t.Fatalf("write source markdown: %v", err)
	}

	result := ValidateWorkspaceMarkdownFiles(WorkspaceMarkdownValidationOptions{RootDir: rootDir})

	if result.OK {
		t.Fatalf("validation = %#v, want invalid links to fail", result)
	}
	if len(result.Issues) != 2 {
		t.Fatalf("issues = %#v, want two invalid_link issues", result.Issues)
	}
	for _, issue := range result.Issues {
		if issue.Code != "invalid_link" {
			t.Fatalf("issue = %#v, want invalid_link", issue)
		}
		if issue.Path != "source" {
			t.Fatalf("issue path = %q, want source", issue.Path)
		}
	}
}

func TestValidateWorkspaceMarkdownFiles_DistinguishesSectionMdPageFromSectionDefault(t *testing.T) {
	rootDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(rootDir, "docs", "sync"), 0o755); err != nil {
		t.Fatalf("create sync dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(rootDir, "docs", "a.md"), []byte("---\nleafwiki_id: docs-a\nleafwiki_title: Docs A\n---\n# Docs A\n\n[Sync page](/docs/sync.md)\n"), 0o644); err != nil {
		t.Fatalf("write source markdown: %v", err)
	}
	if err := os.WriteFile(filepath.Join(rootDir, "docs", "sync", "index.md"), []byte("---\nleafwiki_id: docs-sync\nleafwiki_title: Docs Sync\n---\n# Docs Sync\n"), 0o644); err != nil {
		t.Fatalf("write section index markdown: %v", err)
	}

	result := ValidateWorkspaceMarkdownFiles(WorkspaceMarkdownValidationOptions{RootDir: rootDir})

	if result.OK {
		t.Fatalf("validation = %#v, want /docs/sync.md to require a page file, not alias a section", result)
	}
	if len(result.Issues) != 1 || result.Issues[0].Code != "broken_link" {
		t.Fatalf("issues = %#v, want one broken_link", result.Issues)
	}
}

func TestValidateWorkspaceMarkdownFiles_AllowsSameBasenamePageAndSectionCanonicalLinks(t *testing.T) {
	rootDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(rootDir, "docs", "sync"), 0o755); err != nil {
		t.Fatalf("create sync dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(rootDir, "docs", "a.md"), []byte("---\nleafwiki_id: docs-a\nleafwiki_title: Docs A\n---\n# Docs A\n\n[Sync section](/docs/sync)\n[Sync page](/docs/sync.md)\n"), 0o644); err != nil {
		t.Fatalf("write source markdown: %v", err)
	}
	if err := os.WriteFile(filepath.Join(rootDir, "docs", "sync.md"), []byte("---\nleafwiki_id: page-sync\nleafwiki_title: Sync Page\n---\n# Sync Page\n"), 0o644); err != nil {
		t.Fatalf("write sync page markdown: %v", err)
	}
	if err := os.WriteFile(filepath.Join(rootDir, "docs", "sync", "index.md"), []byte("---\nleafwiki_id: section-sync\nleafwiki_title: Sync Section\n---\n# Sync Section\n"), 0o644); err != nil {
		t.Fatalf("write sync section markdown: %v", err)
	}

	result := ValidateWorkspaceMarkdownFiles(WorkspaceMarkdownValidationOptions{RootDir: rootDir})

	if !result.OK {
		t.Fatalf("validation = %#v, want canonical same-basename page and section links to resolve", result)
	}
	for _, issue := range result.Issues {
		if issue.Code == "path_conflict" || issue.Code == "ambiguous_legacy_link" {
			t.Fatalf("issues = %#v, want no conflict or ambiguity for canonical same-basename links", result.Issues)
		}
	}
}

func TestValidateWorkspaceMarkdownFiles_AcceptsCanonicalSectionLinkWithSameBasenamePage(t *testing.T) {
	rootDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(rootDir, "docs", "sync"), 0o755); err != nil {
		t.Fatalf("create sync dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(rootDir, "docs", "a.md"), []byte("---\nleafwiki_id: docs-a\nleafwiki_title: Docs A\n---\n# Docs A\n\n[Sync section](/docs/sync)\n[Sync page](/docs/sync.md)\n"), 0o644); err != nil {
		t.Fatalf("write source markdown: %v", err)
	}
	if err := os.WriteFile(filepath.Join(rootDir, "docs", "sync.md"), []byte("---\nleafwiki_id: page-sync\nleafwiki_title: Sync Page\n---\n# Sync Page\n"), 0o644); err != nil {
		t.Fatalf("write sync page markdown: %v", err)
	}
	if err := os.WriteFile(filepath.Join(rootDir, "docs", "sync", "index.md"), []byte("---\nleafwiki_id: section-sync\nleafwiki_title: Sync Section\n---\n# Sync Section\n"), 0o644); err != nil {
		t.Fatalf("write sync section markdown: %v", err)
	}

	result := ValidateWorkspaceMarkdownFiles(WorkspaceMarkdownValidationOptions{RootDir: rootDir})

	if !result.OK {
		t.Fatalf("validation = %#v, want canonical same-basename page and section links to resolve", result)
	}
}

// - Case mismatch is invalid
func TestValidateWorkspaceMarkdownFiles_UsesExactCaseSensitiveTargetMatching(t *testing.T) {
	rootDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(rootDir, "docs"), 0o755); err != nil {
		t.Fatalf("create docs dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(rootDir, "docs", "a.md"), []byte("---\nleafwiki_id: docs-a\nleafwiki_title: Docs A\n---\n# Docs A\n\n[Sync](/docs/sync.md)\n"), 0o644); err != nil {
		t.Fatalf("write source markdown: %v", err)
	}
	if err := os.WriteFile(filepath.Join(rootDir, "docs", "Sync.md"), []byte("---\nleafwiki_id: docs-sync\nleafwiki_title: Docs Sync\n---\n# Docs Sync\n"), 0o644); err != nil {
		t.Fatalf("write target markdown: %v", err)
	}

	result := ValidateWorkspaceMarkdownFiles(WorkspaceMarkdownValidationOptions{RootDir: rootDir})

	found := false
	for _, issue := range result.Issues {
		if issue.Code == "broken_link" && issue.Path == "docs/a" {
			found = true
		}
	}
	if !found {
		t.Fatalf("issues = %#v, want source broken_link for exact-case mismatch", result.Issues)
	}
}

// - Old extensionless page link is reported as non-canonical when migration cannot resolve it
func TestValidateWorkspaceMarkdownFiles_RejectsUnmigratedExtensionlessPageLink(t *testing.T) {
	rootDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(rootDir, "docs"), 0o755); err != nil {
		t.Fatalf("create docs dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(rootDir, "docs", "a.md"), []byte("---\nleafwiki_id: docs-a\nleafwiki_title: Docs A\n---\n# Docs A\n\n[B](/docs/b)\n"), 0o644); err != nil {
		t.Fatalf("write source markdown: %v", err)
	}
	if err := os.WriteFile(filepath.Join(rootDir, "docs", "b.md"), []byte("---\nleafwiki_id: docs-b\nleafwiki_title: Docs B\n---\n# Docs B\n"), 0o644); err != nil {
		t.Fatalf("write target markdown: %v", err)
	}

	result := ValidateWorkspaceMarkdownFiles(WorkspaceMarkdownValidationOptions{RootDir: rootDir})

	if result.OK {
		t.Fatalf("validation = %#v, want extensionless page link to fail", result)
	}
	if len(result.Issues) != 1 || result.Issues[0].Code != "non_canonical_link" {
		t.Fatalf("issues = %#v, want one non_canonical_link", result.Issues)
	}
	if result.Issues[0].Path != "docs/a" {
		t.Fatalf("issue path = %q, want source page path", result.Issues[0].Path)
	}
}

func TestValidateMarkdownContent_UsesResolveMarkdownLinkWithoutLegacyResolver(t *testing.T) {
	result := ValidateMarkdownContentWithOptions("docs/a", "[B](/docs/b)\n", ContentValidationOptions{
		ResolveMarkdownLink: func(destination string) (string, tree.NodeKind, bool, string) {
			if destination == "/docs/b" {
				return "docs/b", tree.NodeKindPage, true, ""
			}
			return "", "", false, "broken_link"
		},
	})

	if result.OK {
		t.Fatalf("validation = %#v, want non-canonical page link issue", result)
	}
	if len(result.Issues) != 1 || result.Issues[0].Code != "non_canonical_link" {
		t.Fatalf("issues = %#v, want one non_canonical_link", result.Issues)
	}
}
