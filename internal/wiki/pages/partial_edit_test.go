package pages

import (
	"strings"

	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = ginkgo.Describe("markdown section replacement", ginkgo.Label("unit"), func() {
	ginkgo.It("replaces a nested heading while preserving sibling sections", func() {
		content := "# Guide\n\n## API\n\n### Auth\n\nold auth\n\n### Rate Limits\n\nkeep rate limits\n\n## Other\n\nkeep other\n"

		got, err := ReplaceMarkdownSection(content, []string{"Guide", "API", "Auth"}, 0, "new auth\n")

		Expect(err).To(Succeed())
		Expect(got).To(SatisfyAll(
			ContainSubstring("### Auth\nnew auth\n"),
			ContainSubstring("### Rate Limits\n\nkeep rate limits"),
			ContainSubstring("## Other\n\nkeep other"),
			Not(ContainSubstring("old auth")),
		))
	})

	ginkgo.It("ignores headings inside fenced code blocks", func() {
		content := "# Guide\n\n```\n## API\nfake code heading\n```\n\n## API\n\nold api\n\n## Other\n\nkeep other\n"

		got, err := ReplaceMarkdownSection(content, []string{"API"}, 0, "new api\n")

		Expect(err).To(Succeed())
		Expect(got).To(SatisfyAll(
			ContainSubstring("```\n## API\nfake code heading\n```"),
			ContainSubstring("## API\nnew api"),
			Not(ContainSubstring("old api")),
		))
	})

	ginkgo.It("ignores headings inside nested fenced code blocks", func() {
		content := "# Guide\n\n````\n```go\n## API\nfake nested code heading\n```\n````\n\n## API\n\nold api\n"

		got, err := ReplaceMarkdownSection(content, []string{"API"}, 0, "new api\n")

		Expect(err).To(Succeed())
		Expect(got).To(SatisfyAll(
			ContainSubstring("````\n```go\n## API\nfake nested code heading\n```\n````"),
			ContainSubstring("## API\nnew api"),
			Not(ContainSubstring("old api")),
		))
	})

	ginkgo.It("ignores headings inside indented code blocks", func() {
		content := "# Guide\n\n    ## API\n    fake indented code heading\n\n## API\n\nold api\n\n## Other\n\nkeep other\n"

		got, err := ReplaceMarkdownSection(content, []string{"API"}, 0, "new api\n")

		Expect(err).To(Succeed())
		Expect(got).To(SatisfyAll(
			ContainSubstring("    ## API\n    fake indented code heading"),
			ContainSubstring("## API\nnew api\n"),
			Not(ContainSubstring("old api")),
		))
	})

	ginkgo.It("requires an occurrence when a heading appears more than once", func() {
		content := "# Guide\n\n## Notes\n\nfirst\n\n## Notes\n\nsecond\n"

		_, err := ReplaceMarkdownSection(content, []string{"Notes"}, 0, "new notes\n")
		Expect(err).To(MatchError(ErrSectionAmbiguousHeading))

		got, err := ReplaceMarkdownSection(content, []string{"Notes"}, 2, "updated notes\n")
		Expect(err).To(Succeed())
		Expect(got).To(SatisfyAll(
			ContainSubstring("## Notes\n\nfirst\n\n## Notes\nupdated notes"),
			Not(ContainSubstring("\nsecond")),
		))
	})

	ginkgo.It("returns a heading-not-found error without changing content", func() {
		content := "# Guide\n\n## API\n\nold api\n"

		_, err := ReplaceMarkdownSection(content, []string{"Missing"}, 0, "new\n")

		Expect(err).To(MatchError(ErrSectionHeadingNotFound))
	})

	ginkgo.It("replaces the full section when replacement includes its heading", func() {
		content := "# Guide\n\n## API\n\nold api\n\n## Other\n\nkeep other\n"

		got, err := ReplaceMarkdownSection(content, []string{"API"}, 0, "## API\nnew api\n")

		Expect(err).To(Succeed())
		Expect(strings.Count(got, "## API")).To(Equal(1))
		Expect(got).To(SatisfyAll(
			ContainSubstring("## API\nnew api\n"),
			ContainSubstring("## Other\n\nkeep other"),
		))
	})
})
