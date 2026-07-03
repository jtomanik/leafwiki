package links

import (
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// Canonical Markdown links plan scenarios covered by tests in this file:
// - Parentheses in destinations do not corrupt the rewrite
// - Source page move recalculates relative links without changing absolute links

var _ = ginkgo.Describe("markdown refactor destination rewriting", func() {
	ginkgo.It("keeps query strings and fragments attached to rewritten destinations", func() {
		content := `[Link](/docs/b?mode=1#intro)`

		result := NewMarkdownRefactorEngine().Rewrite(content, "/docs/a", []RewriteRule{{
			OldPath: "/docs/b",
			NewPath: "/guides/b",
		}})

		Expect(result.Count()).To(Equal(1))
		Expect(result.Content).To(Equal(`[Link](/guides/b?mode=1#intro)`))
	})

	ginkgo.It("rewrites destinations containing parentheses without corrupting them", func() {
		content := `[Draft](./page_(draft))`

		result := NewMarkdownRefactorEngine().Rewrite(content, "/docs/a", []RewriteRule{{
			OldPath: "/docs/page_(draft)",
			NewPath: "/guides/page_(draft)",
		}})

		Expect(result.Count()).To(Equal(1))
		Expect(result.Content).To(Equal(`[Draft](../guides/page_(draft))`))
	})

	ginkgo.It("changes only the link destination segment when labels and titles contain punctuation", func() {
		content := `[Label with (parens)](/docs/b "Title")`

		result := NewMarkdownRefactorEngine().Rewrite(content, "/docs/a", []RewriteRule{{
			OldPath: "/docs/b",
			NewPath: "/guides/b",
		}})

		Expect(result.Count()).To(Equal(1))
		Expect(result.Content).To(ContainSubstring(`[Label with (parens)](/guides/b "Title")`))
	})
})

var _ = ginkgo.Describe("markdown refactor path-change rewriting", func() {
	ginkgo.It("uses file-relative semantics when a page moves across trees", func() {
		content := `[Target](./seite-a)`

		result := NewMarkdownRefactorEngine().RewriteRelativeLinksForPathChange(
			content,
			"/test-link-refactoring/seite-b",
			"/patrick/techtalk/seite-b",
			[]RewriteRule{
				{
					OldPath: "/test-link-refactoring/seite-b",
					NewPath: "/patrick/techtalk/seite-b",
				},
			},
		)

		Expect(result.Count()).To(Equal(1))
		Expect(result.Content).To(Equal(`[Target](../../test-link-refactoring/seite-a)`))
	})

	ginkgo.It("preserves canonical page markdown extensions after the source page moves", func() {
		content := `[Target](../b/target.md)`

		result := NewMarkdownRefactorEngine().RewriteRelativeLinksForPathChange(
			content,
			"/docs/a/current",
			"/archive/a/current",
			nil,
		)

		Expect(result.Count()).To(Equal(1))
		Expect(result.Content).To(Equal(`[Target](../../docs/b/target.md)`))
	})

	ginkgo.It("leaves relative asset links unchanged when a source page moves", func() {
		content := `[Asset](assets/abc/manual.pdf)`

		result := NewMarkdownRefactorEngine().RewriteRelativeLinksForPathChange(
			content,
			"/docs/a",
			"/guides/a",
			[]RewriteRule{
				{
					OldPath: "/docs/a",
					NewPath: "/guides/a",
				},
			},
		)

		Expect(result.Count()).To(BeZero())
		Expect(result.Content).To(Equal(content))
	})

	ginkgo.It("leaves escaped pseudo-links unchanged while recalculating real relative links", func() {
		content := `\[Literal](./target.md)
[Other](../shared/other.md)`

		result := NewMarkdownRefactorEngine().RewriteRelativeLinksForPathChange(
			content,
			"/docs/a/current",
			"/archive/a/current",
			[]RewriteRule{
				{
					OldPath: "/docs/a",
					NewPath: "/archive/a",
				},
			},
		)

		Expect(result.Count()).To(Equal(1))
		Expect(result.Content).NotTo(ContainSubstring(`\[Literal](../../docs/a/target.md)`))
		Expect(result.Content).To(ContainSubstring(`\[Literal](./target.md)`))
	})
})
