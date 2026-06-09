package markdownvalidation

import (
	"os"
	"path/filepath"
	"testing"
)

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
}
