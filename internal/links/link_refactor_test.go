package links

import (
	"strings"
	"testing"
)

// Canonical Markdown links plan scenarios covered by tests in this file:
// - Existing canonical relative page links preserve relative style
// - Existing canonical absolute page links preserve absolute style
// - Section move keeps section links extensionless
// - Page and section with the same basename are not cross-rewritten
// - Broken non-canonical links are not silently rewritten by refactor

func TestMarkdownRefactorEngine_Rewrite_RewritesAbsoluteAndRelativeTargets(t *testing.T) {
	content := `
[Absolute](/docs/b)
	[Relative](./b)
[Nested](/docs/b/child#section)
[External](https://example.com/docs/b)
![Image](/docs/b.png)
`

	result := NewMarkdownRefactorEngine().Rewrite(content, "/docs/a", []RewriteRule{{
		OldPath: "/docs/b",
		NewPath: "/guides/b",
	}})

	if result.Count() != 3 {
		t.Fatalf("expected 3 changes, got %d", result.Count())
	}
	if !strings.Contains(result.Content, "[Absolute](/guides/b)") {
		t.Fatalf("expected absolute link rewrite, got:\n%s", result.Content)
	}
	if !strings.Contains(result.Content, "[Relative](../guides/b)") {
		t.Fatalf("expected relative link rewrite, got:\n%s", result.Content)
	}
	if !strings.Contains(result.Content, "[Nested](/guides/b/child#section)") {
		t.Fatalf("expected subtree link rewrite, got:\n%s", result.Content)
	}
	if !strings.Contains(result.Content, "[External](https://example.com/docs/b)") {
		t.Fatalf("external link should remain unchanged, got:\n%s", result.Content)
	}
	if !strings.Contains(result.Content, "![Image](/docs/b.png)") {
		t.Fatalf("image link should remain unchanged, got:\n%s", result.Content)
	}
}

// - Existing canonical relative page links preserve relative style
// - Existing canonical absolute page links preserve absolute style
func TestMarkdownRefactorEngine_RewriteCanonicalPageLinksKeepsMdAbsoluteAndRelative(t *testing.T) {
	content := "[Absolute](/docs/b.md)\n[Relative](./b.md)"

	result := NewMarkdownRefactorEngine().Rewrite(content, "/docs/a", []RewriteRule{{
		OldPath: "/docs/b",
		NewPath: "/guides/b",
	}})

	if result.Count() != 2 {
		t.Fatalf("expected 2 changes, got %d; content:\n%s", result.Count(), result.Content)
	}
	if !strings.Contains(result.Content, "[Absolute](/guides/b.md)") {
		t.Fatalf("expected absolute canonical page link rewrite, got:\n%s", result.Content)
	}
	if !strings.Contains(result.Content, "[Relative](../guides/b.md)") {
		t.Fatalf("expected relative canonical page link rewrite, got:\n%s", result.Content)
	}
}

func TestMarkdownRefactorEngine_RewriteCanonicalPageLinksPreservesExplicitDotSlashStyle(t *testing.T) {
	content := "[Relative](./b.md)"

	result := NewMarkdownRefactorEngine().Rewrite(content, "/docs/a", []RewriteRule{{
		OldPath: "/docs/b",
		NewPath: "/docs/c",
		Kind:    "page",
	}})

	if result.Count() != 1 {
		t.Fatalf("expected 1 change, got %d; content:\n%s", result.Count(), result.Content)
	}
	if result.Content != "[Relative](./c.md)" {
		t.Fatalf("expected explicit ./ style to be preserved, got:\n%s", result.Content)
	}
}

// - Broken non-canonical links are not silently rewritten by refactor
func TestMarkdownRefactorEngine_DoesNotRewriteLegacyExtensionlessPageLinks(t *testing.T) {
	content := "[Target](/target)\n[Canonical](/target.md)"

	result := NewMarkdownRefactorEngine().Rewrite(content, "/source", []RewriteRule{{
		OldPath: "/target",
		NewPath: "/renamed-target",
		Kind:    "page",
	}})

	if result.Count() != 1 {
		t.Fatalf("expected only the canonical page link to change, got %d changes:\n%s", result.Count(), result.Content)
	}
	if !strings.Contains(result.Content, "[Target](/target)") {
		t.Fatalf("legacy extensionless page link should remain unchanged, got:\n%s", result.Content)
	}
	if !strings.Contains(result.Content, "[Canonical](/renamed-target.md)") {
		t.Fatalf("canonical page link should be rewritten, got:\n%s", result.Content)
	}
}

func TestMarkdownRefactorEngine_DoesNotRewritePseudoLinksWithWhitespaceBeforeDestination(t *testing.T) {
	content := "[Space] (/docs/b.md)\n[Newline]\n(/docs/b.md)\n[Canonical](/docs/b.md)"

	result := NewMarkdownRefactorEngine().Rewrite(content, "/docs/source", []RewriteRule{{
		OldPath: "/docs/b",
		NewPath: "/docs/c",
		Kind:    "page",
	}})

	if result.Count() != 1 {
		t.Fatalf("expected only the canonical link to change, got %d changes:\n%s", result.Count(), result.Content)
	}
	if !strings.Contains(result.Content, "[Space] (/docs/b.md)") {
		t.Fatalf("space pseudo-link should remain unchanged, got:\n%s", result.Content)
	}
	if !strings.Contains(result.Content, "[Newline]\n(/docs/b.md)") {
		t.Fatalf("newline pseudo-link should remain unchanged, got:\n%s", result.Content)
	}
	if !strings.Contains(result.Content, "[Canonical](/docs/c.md)") {
		t.Fatalf("canonical link should be rewritten, got:\n%s", result.Content)
	}
}

func TestMarkdownRefactorEngine_DoesNotRewriteLinkLikeTextInsideTitle(t *testing.T) {
	content := `[Outer](/docs/a.md "[Inner](/docs/b.md)")`

	result := NewMarkdownRefactorEngine().Rewrite(content, "/docs/source", []RewriteRule{
		{
			OldPath: "/docs/a",
			NewPath: "/docs/renamed-a",
			Kind:    "page",
		},
		{
			OldPath: "/docs/b",
			NewPath: "/docs/renamed-b",
			Kind:    "page",
		},
	})

	if result.Count() != 1 {
		t.Fatalf("expected only the real link destination to change, got %d changes:\n%s", result.Count(), result.Content)
	}
	if result.Content != `[Outer](/docs/renamed-a.md "[Inner](/docs/b.md)")` {
		t.Fatalf("link-like title text should remain unchanged, got:\n%s", result.Content)
	}
}

// - Page and section with the same basename are not cross-rewritten
func TestMarkdownRefactorEngine_PageRefactorDoesNotRewriteSameBasenameSectionLink(t *testing.T) {
	content := "[Page](/docs/sync.md)\n[Section](/docs/sync)"

	result := NewMarkdownRefactorEngine().Rewrite(content, "/docs/source", []RewriteRule{{
		OldPath: "/docs/sync",
		NewPath: "/docs/sync-page",
		Kind:    "page",
	}})

	if result.Count() != 1 {
		t.Fatalf("expected only the canonical page link to change, got %d changes:\n%s", result.Count(), result.Content)
	}
	if !strings.Contains(result.Content, "[Page](/docs/sync-page.md)") {
		t.Fatalf("expected page link rewrite, got:\n%s", result.Content)
	}
	if !strings.Contains(result.Content, "[Section](/docs/sync)") {
		t.Fatalf("same-basename section link should remain unchanged, got:\n%s", result.Content)
	}
}

func TestMarkdownRefactorEngine_LegacyPageOverrideDoesNotRewriteDescendants(t *testing.T) {
	content := "[Sync page](/docs/sync)\n[Sync child](/docs/sync/child.md)"

	result := NewMarkdownRefactorEngine().Rewrite(content, "/docs/source", []RewriteRule{{
		OldPath:    "/docs/sync",
		NewPath:    "/docs/sync-page",
		Kind:       "section",
		OutputKind: "page",
	}})

	if result.Content != "[Sync page](/docs/sync-page.md)\n[Sync child](/docs/sync/child.md)" {
		t.Fatalf("legacy page override should only rewrite the exact healed target, got:\n%s", result.Content)
	}
}

// - Section move keeps section links extensionless
func TestMarkdownRefactorEngine_RewriteSectionLinksUsesFilesystemRelativeSemantics(t *testing.T) {
	content := "[Section](../b)"

	result := NewMarkdownRefactorEngine().Rewrite(content, "/docs/a/current", []RewriteRule{{
		OldPath: "/docs/b",
		NewPath: "/guides/b",
	}})

	if result.Count() != 1 {
		t.Fatalf("expected 1 change, got %d; content:\n%s", result.Count(), result.Content)
	}
	if result.Content != "[Section](../../guides/b)" {
		t.Fatalf("expected section link rewrite from source file directory, got %q", result.Content)
	}
}

func TestMarkdownRefactorEngine_Rewrite_UsesMovedSourcePathForRelativeLinks(t *testing.T) {
	content := `[Relative](../shared)`

	result := NewMarkdownRefactorEngine().Rewrite(content, "/docs/a/page", []RewriteRule{{
		OldPath: "/docs",
		NewPath: "/archive/docs",
	}})

	if result.Count() != 1 {
		t.Fatalf("expected 1 change, got %d", result.Count())
	}
	if result.Content != `[Relative](../shared)` {
		t.Fatalf("expected relative link to be recalculated against moved source path, got %q", result.Content)
	}
}

func TestMarkdownRefactorEngine_Rewrite_IgnoresAssetLinks(t *testing.T) {
	content := `
[AssetAbs](/assets/abc/manual.pdf)
[AssetRel](assets/abc/manual.pdf)
[Wiki](/docs/b)
`

	result := NewMarkdownRefactorEngine().Rewrite(content, "/docs/a", []RewriteRule{{
		OldPath: "/docs/b",
		NewPath: "/guides/b",
	}})

	if result.Count() != 1 {
		t.Fatalf("expected only wiki link rewrite, got %d", result.Count())
	}
	if !strings.Contains(result.Content, "[AssetAbs](/assets/abc/manual.pdf)") {
		t.Fatalf("absolute asset link should remain unchanged, got:\n%s", result.Content)
	}
	if !strings.Contains(result.Content, "[AssetRel](assets/abc/manual.pdf)") {
		t.Fatalf("relative asset link should remain unchanged, got:\n%s", result.Content)
	}
	if !strings.Contains(result.Content, "[Wiki](/guides/b)") {
		t.Fatalf("wiki link should be rewritten, got:\n%s", result.Content)
	}
}
