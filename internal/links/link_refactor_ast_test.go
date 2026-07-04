package links

import (
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	testmatchers "github.com/perber/wiki/internal/test_utils/matchers"
)

var _ = ginkgo.Describe("markdown refactor engine", ginkgo.Label("unit"), func() {
	ginkgo.It("rewrites real inline links without changing code spans or fenced code blocks", ginkgo.Label("unit"), func() {
		content := "`[code](/docs/b)`\n\n```md\n[block](/docs/b)\n```\n\n[real](/docs/b)"

		result := NewMarkdownRefactorEngine().Rewrite(content, "/docs/a", []RewriteRule{{
			OldPath: "/docs/b",
			NewPath: "/guides/b",
		}})

		Expect(result.Count()).To(Equal(1))
		Expect(result.Content).To(Equal("`[code](/docs/b)`\n\n```md\n[block](/docs/b)\n```\n\n[real](/guides/b)"))
	})

	ginkgo.It("reports reference-style links without rewriting them inline", ginkgo.Label("unit"), func() {
		content := "[Ref][docs]\n\n[docs]: /docs/b"

		result := NewMarkdownRefactorEngine().Rewrite(content, "/docs/a", []RewriteRule{{
			OldPath: "/docs/b",
			NewPath: "/guides/b",
		}})

		Expect(result.Count()).To(BeZero())
		Expect(result.Warnings).To(ConsistOf(testmatchers.HaveMessageID(rewriteWarningUnsupportedSyntax)))
	})

	ginkgo.It("keeps later inline links rewriteable after a reference-link candidate", ginkgo.Label("unit"), func() {
		content := "[Ref][docs]\n\n[docs]: /docs/other\n\n[Inline](/docs/b)"

		result := NewMarkdownRefactorEngine().Rewrite(content, "/docs/a", []RewriteRule{{
			OldPath: "/docs/b",
			NewPath: "/guides/b",
		}})

		Expect(result.Count()).To(Equal(1))
		Expect(result.Content).To(Equal("[Ref][docs]\n\n[docs]: /docs/other\n\n[Inline](/guides/b)"))
	})

	ginkgo.It("leaves escaped pseudo-links unchanged", ginkgo.Label("unit"), func() {
		content := `\[Literal](/docs/b)
[Other](/docs/other)`

		result := NewMarkdownRefactorEngine().Rewrite(content, "/docs/a", []RewriteRule{{
			OldPath: "/docs/b",
			NewPath: "/guides/b",
		}})

		Expect(result.Count()).To(BeZero())
		Expect(result.Content).To(Equal(content))
	})
})
