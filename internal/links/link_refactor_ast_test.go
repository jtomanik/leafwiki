package links

import (
	ginkgo "github.com/onsi/ginkgo/v2"
)

var _ = ginkgo.Describe("TestMarkdownRefactorEngine_Rewrite_SkipsInlineCodeAndCodeBlocks", func() {
	ginkgo.It("preserves behavior", func() {
		t := ginkgo.GinkgoT()
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

	})
})

var _ = ginkgo.Describe("TestMarkdownRefactorEngine_Rewrite_WarnsForReferenceLinks", func() {
	ginkgo.It("preserves behavior", func() {
		t := ginkgo.GinkgoT()
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

	})
})

var _ = ginkgo.Describe("TestMarkdownRefactorEngine_Rewrite_ReferenceCandidateDoesNotConsumeInlineOccurrence", func() {
	ginkgo.It("preserves behavior", func() {
		t := ginkgo.GinkgoT()
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

	})
})

var _ = ginkgo.Describe("TestMarkdownRefactorEngine_Rewrite_DoesNotRewriteEscapedPseudoLinks", func() {
	ginkgo.It("preserves behavior", func() {
		t := ginkgo.GinkgoT()
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

	})
})
