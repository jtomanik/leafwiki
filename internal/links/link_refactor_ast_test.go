package links

import "testing"

func TestMarkdownRefactorEngine_Rewrite_SkipsInlineCodeAndCodeBlocks(t *testing.T) {
	content := "`[code](/docs/b)`\n\n```md\n[block](/docs/b)\n```\n\n[real](/docs/b)"

	result := NewMarkdownRefactorEngine().Rewrite(content, "/docs/a", []RewriteRule{{
		OldPath: "/docs/b",
		NewPath: "/guides/b",
	}})

	if result.Count() != 1 {
		t.Fatalf("expected 1 rewrite for real markdown link, got %d", result.Count())
	}
	if result.Content != "`[code](/docs/b)`\n\n```md\n[block](/docs/b)\n```\n\n[real](/guides/b)" {
		t.Fatalf("unexpected rewritten content:\n%s", result.Content)
	}
}

func TestMarkdownRefactorEngine_Rewrite_WarnsForReferenceLinks(t *testing.T) {
	content := "[Ref][docs]\n\n[docs]: /docs/b"

	result := NewMarkdownRefactorEngine().Rewrite(content, "/docs/a", []RewriteRule{{
		OldPath: "/docs/b",
		NewPath: "/guides/b",
	}})

	if result.Count() != 0 {
		t.Fatalf("expected no inline rewrite for reference links, got %d", result.Count())
	}
	if len(result.Warnings) == 0 {
		t.Fatalf("expected warning for unsupported reference link syntax")
	}
}

func TestMarkdownRefactorEngine_Rewrite_ReferenceCandidateDoesNotConsumeInlineOccurrence(t *testing.T) {
	content := "[Ref][docs]\n\n[docs]: /docs/other\n\n[Inline](/docs/b)"

	result := NewMarkdownRefactorEngine().Rewrite(content, "/docs/a", []RewriteRule{{
		OldPath: "/docs/b",
		NewPath: "/guides/b",
	}})

	if result.Count() != 1 {
		t.Fatalf("expected inline link rewrite after reference candidate, got %d:\n%s", result.Count(), result.Content)
	}
	if result.Content != "[Ref][docs]\n\n[docs]: /docs/other\n\n[Inline](/guides/b)" {
		t.Fatalf("unexpected rewritten content:\n%s", result.Content)
	}
}

func TestMarkdownRefactorEngine_Rewrite_DoesNotRewriteEscapedPseudoLinks(t *testing.T) {
	content := `\[Literal](/docs/b)
[Other](/docs/other)`

	result := NewMarkdownRefactorEngine().Rewrite(content, "/docs/a", []RewriteRule{{
		OldPath: "/docs/b",
		NewPath: "/guides/b",
	}})

	if result.Count() != 0 {
		t.Fatalf("expected escaped pseudo-link to remain untouched, got %d changes:\n%s", result.Count(), result.Content)
	}
	if result.Content != content {
		t.Fatalf("unexpected rewritten content:\n%s", result.Content)
	}
}
