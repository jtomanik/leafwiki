package links

import (
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// Canonical Markdown links plan scenarios covered by tests in this file:
// - Parentheses in destinations do not corrupt the rewrite
// - Source page move recalculates relative links without changing absolute links

var _ = ginkgo.Describe("markdown refactor destination rewriting", ginkgo.Label("unit"), func() {
	ginkgo.It("keeps query strings and fragments attached to rewritten destinations", ginkgo.Label("unit"), func() {
		content := `[Link](/docs/b?mode=1#intro)`

		result := NewMarkdownRefactorEngine().Rewrite(content, newFixtureRoutePath("/docs/a"), []RewriteRule{{
			OldPath: newFixtureRoutePath("/docs/b"),
			NewPath: newFixtureRoutePath("/guides/b"),
		}})

		Expect(result.Count()).To(Equal(1))
		Expect(result.Content).To(Equal(`[Link](/guides/b?mode=1#intro)`))
	})

	ginkgo.It("rewrites destinations containing parentheses without corrupting them", ginkgo.Label("unit"), func() {
		content := `[Draft](./page_(draft))`

		result := NewMarkdownRefactorEngine().Rewrite(content, newFixtureRoutePath("/docs/a"), []RewriteRule{{
			OldPath: newFixtureRoutePath("/docs/page_(draft)"),
			NewPath: newFixtureRoutePath("/guides/page_(draft)"),
		}})

		Expect(result.Count()).To(Equal(1))
		Expect(result.Content).To(Equal(`[Draft](../guides/page_(draft))`))
	})

	ginkgo.It("changes only the link destination segment when labels and titles contain punctuation", ginkgo.Label("unit"), func() {
		content := `[Label with (parens)](/docs/b "Title")`

		result := NewMarkdownRefactorEngine().Rewrite(content, newFixtureRoutePath("/docs/a"), []RewriteRule{{
			OldPath: newFixtureRoutePath("/docs/b"),
			NewPath: newFixtureRoutePath("/guides/b"),
		}})

		Expect(result.Count()).To(Equal(1))
		Expect(result.Content).To(ContainSubstring(`[Label with (parens)](/guides/b "Title")`))
	})
})

var _ = ginkgo.Describe("markdown refactor path-change rewriting", ginkgo.Label("unit"), func() {
	ginkgo.It("uses file-relative semantics when a page moves across trees", ginkgo.Label("unit"), func() {
		content := `[Target](./seite-a)`

		result := NewMarkdownRefactorEngine().RewriteRelativeLinksForPathChange(
			content,
			newFixtureRoutePath("/test-link-refactoring/seite-b"),
			newFixtureRoutePath("/patrick/techtalk/seite-b"),
			[]RewriteRule{
				{
					OldPath: newFixtureRoutePath("/test-link-refactoring/seite-b"),
					NewPath: newFixtureRoutePath("/patrick/techtalk/seite-b"),
				},
			},
		)

		Expect(result.Count()).To(Equal(1))
		Expect(result.Content).To(Equal(`[Target](../../test-link-refactoring/seite-a)`))
	})

	ginkgo.It("preserves canonical page markdown extensions after the source page moves", ginkgo.Label("unit"), func() {
		content := `[Target](../b/target.md)`

		result := NewMarkdownRefactorEngine().RewriteRelativeLinksForPathChange(
			content,
			newFixtureRoutePath("/docs/a/current"),
			newFixtureRoutePath("/archive/a/current"),
			nil,
		)

		Expect(result.Count()).To(Equal(1))
		Expect(result.Content).To(Equal(`[Target](../../docs/b/target.md)`))
	})

	ginkgo.It("leaves relative asset links unchanged when a source page moves", ginkgo.Label("unit"), func() {
		content := `[Asset](assets/abc/manual.pdf)`

		result := NewMarkdownRefactorEngine().RewriteRelativeLinksForPathChange(
			content,
			newFixtureRoutePath("/docs/a"),
			newFixtureRoutePath("/guides/a"),
			[]RewriteRule{
				{
					OldPath: newFixtureRoutePath("/docs/a"),
					NewPath: newFixtureRoutePath("/guides/a"),
				},
			},
		)

		Expect(result.Count()).To(BeZero())
		Expect(result.Content).To(Equal(content))
	})

	ginkgo.It("leaves escaped pseudo-links unchanged while recalculating real relative links", ginkgo.Label("unit"), func() {
		content := `\[Literal](./target.md)
[Other](../shared/other.md)`

		result := NewMarkdownRefactorEngine().RewriteRelativeLinksForPathChange(
			content,
			newFixtureRoutePath("/docs/a/current"),
			newFixtureRoutePath("/archive/a/current"),
			[]RewriteRule{
				{
					OldPath: newFixtureRoutePath("/docs/a"),
					NewPath: newFixtureRoutePath("/archive/a"),
				},
			},
		)

		Expect(result.Count()).To(Equal(1))
		Expect(result.Content).NotTo(ContainSubstring(`\[Literal](../../docs/a/target.md)`))
		Expect(result.Content).To(ContainSubstring(`\[Literal](./target.md)`))
	})
})
