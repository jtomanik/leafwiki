package tags

import (
	"strings"

	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/perber/wiki/internal/core/excerpt"
)

var _ = ginkgo.Describe("tag page excerpt extraction", func() {
	ginkgo.It("returns plain body text after frontmatter", func() {
		raw := "---\ntitle: Hello\n---\n\nThis is the page body."

		got := ExtractExcerptFromContent(raw)

		Expect(got).To(Equal("This is the page body."))
	})

	ginkgo.It("uses the full content when frontmatter is absent", func() {
		raw := "Just plain content here."

		got := ExtractExcerptFromContent(raw)

		Expect(got).To(Equal("Just plain content here."))
	})

	ginkgo.It("returns empty text for pages with no body", func() {
		raw := "---\ntitle: Hello\n---\n\n"

		got := ExtractExcerptFromContent(raw)

		Expect(got).To(BeEmpty())
	})

	ginkgo.It("returns empty text for empty content", func() {
		Expect(ExtractExcerptFromContent("")).To(BeEmpty())
	})

	ginkgo.It("removes fenced code while preserving surrounding prose", func() {
		raw := "---\ntitle: T\n---\n\nBefore.\n\n```go\nfunc main() {}\n```\n\nAfter."

		got := ExtractExcerptFromContent(raw)

		Expect(got).NotTo(ContainSubstring("func main"))
		Expect(got).To(SatisfyAll(
			ContainSubstring("Before."),
			ContainSubstring("After."),
		))
	})

	ginkgo.It("strips markdown heading markers while preserving heading text", func() {
		raw := "---\ntitle: T\n---\n\n# Heading One\n\nSome body text."

		got := ExtractExcerptFromContent(raw)

		Expect(got).NotTo(ContainSubstring("#"))
		Expect(got).To(ContainSubstring("Heading One"))
	})

	ginkgo.It("strips image syntax while preserving alt text", func() {
		raw := "---\ntitle: T\n---\n\n![alt text](image.png) Some text."

		got := ExtractExcerptFromContent(raw)

		Expect(got).NotTo(Or(
			ContainSubstring("!["),
			ContainSubstring("image.png"),
		))
		Expect(got).To(ContainSubstring("alt text"))
	})

	ginkgo.It("strips markdown link URLs while preserving link text", func() {
		raw := "---\ntitle: T\n---\n\n[Click here](https://example.com) for more."

		got := ExtractExcerptFromContent(raw)

		Expect(got).NotTo(Or(
			ContainSubstring("https://example.com"),
			ContainSubstring("]("),
		))
		Expect(got).To(ContainSubstring("Click here"))
	})

	ginkgo.It("truncates long content with an ellipsis", func() {
		body := strings.Repeat("word ", 200)
		raw := "---\ntitle: T\n---\n\n" + body

		got := ExtractExcerptFromContent(raw)

		Expect(got).To(HaveSuffix("..."))
		Expect(len([]rune(got))).To(BeNumerically("<=", excerpt.MaxRunes+10))
	})

	ginkgo.It("leaves short content untruncated", func() {
		raw := "---\ntitle: T\n---\n\nShort body."

		got := ExtractExcerptFromContent(raw)

		Expect(got).NotTo(HaveSuffix("..."))
	})

	ginkgo.It("collapses paragraph breaks into single-line text", func() {
		raw := "---\ntitle: T\n---\n\nLine one.\n\nLine two.\n\nLine three."

		got := ExtractExcerptFromContent(raw)

		Expect(got).NotTo(Or(
			ContainSubstring("\n"),
			ContainSubstring("  "),
		))
	})

	ginkgo.It("strips HTML tags from excerpt text", func() {
		raw := "---\ntitle: T\n---\n\n<strong>Bold</strong> text."

		got := ExtractExcerptFromContent(raw)

		Expect(got).NotTo(Or(
			ContainSubstring("<strong>"),
			ContainSubstring("</strong>"),
		))
	})
})
