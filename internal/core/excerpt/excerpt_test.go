package excerpt

import (
	"errors"
	"io"
	"regexp"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/renderer"
)

var _ = Describe("excerpt generation from content", Label("unit"), func() {
	It("extracts plain body text after frontmatter", func() {
		raw := "---\ntitle: Hello\n---\n\nThis is the page body."

		Expect(FromContent(raw)).To(Equal("This is the page body."))
	})

	It("uses raw content when frontmatter is absent", func() {
		raw := "Just plain content here."

		Expect(FromContent(raw)).To(Equal("Just plain content here."))
	})

	It("returns empty text when frontmatter leaves no body", func() {
		raw := "---\ntitle: Hello\n---\n\n"

		Expect(FromContent(raw)).To(BeEmpty())
	})

	It("returns empty text for empty content", func() {
		Expect(FromContent("")).To(BeEmpty())
	})

	It("excludes fenced code blocks while keeping surrounding prose", func() {
		raw := "---\ntitle: T\n---\n\nBefore.\n\n```go\nfunc main() {}\n```\n\nAfter."

		got := FromContent(raw)

		Expect(got).NotTo(ContainSubstring("func main"))
		Expect(got).To(ContainSubstring("Before."))
		Expect(got).To(ContainSubstring("After."))
	})

	It("turns markdown headings into plain heading text", func() {
		raw := "---\ntitle: T\n---\n\n# Heading One\n\nSome body text."

		got := FromContent(raw)

		Expect(got).NotTo(ContainSubstring("#"))
		Expect(got).To(ContainSubstring("Heading One"))
	})

	It("keeps image alt text without image targets", func() {
		raw := "---\ntitle: T\n---\n\n![alt text](image.png) Some text."

		got := FromContent(raw)

		Expect(got).NotTo(ContainSubstring("!["))
		Expect(got).NotTo(ContainSubstring("image.png"))
		Expect(got).To(ContainSubstring("alt text"))
	})

	It("keeps link labels without link targets", func() {
		raw := "---\ntitle: T\n---\n\n[Click here](https://example.com) for more."

		got := FromContent(raw)

		Expect(got).NotTo(ContainSubstring("https://example.com"))
		Expect(got).NotTo(ContainSubstring("]("))
		Expect(got).To(ContainSubstring("Click here"))
	})

	It("truncates long bodies with an ellipsis", func() {
		body := strings.Repeat("w", MaxRunes+20)
		raw := "---\ntitle: T\n---\n\n" + body

		got := FromContent(raw)

		Expect(got).To(HaveSuffix("..."))
		Expect([]rune(strings.TrimSuffix(got, "..."))).To(HaveLen(MaxRunes))
	})

	It("leaves short bodies unmarked by truncation", func() {
		raw := "---\ntitle: T\n---\n\nShort body."

		Expect(FromContent(raw)).NotTo(HaveSuffix("..."))
	})

	It("collapses paragraph breaks into single-line text", func() {
		raw := "---\ntitle: T\n---\n\nLine one.\n\nLine two.\n\nLine three."

		got := FromContent(raw)

		Expect(got).NotTo(ContainSubstring("\n"))
		Expect(got).NotTo(ContainSubstring("  "))
	})

	It("removes HTML tag markup", func() {
		raw := "---\ntitle: T\n---\n\n<strong>Bold</strong> text."

		got := FromContent(raw)

		Expect(got).NotTo(ContainSubstring("<strong>"))
		Expect(got).NotTo(ContainSubstring("</strong>"))
	})

	It("removes markdown emphasis markers while keeping emphasized words", func() {
		raw := "---\ntitle: T\n---\n\nLeafWiki **fett** und _kursiv_."

		got := FromContent(raw)

		Expect(got).NotTo(ContainSubstring("**"))
		Expect(got).NotTo(ContainSubstring("_"))
		Expect(got).To(ContainSubstring("LeafWiki fett und kursiv."))
	})
})

var _ = Describe("markdown normalization", Label("unit"), func() {
	It("preserves shoutout labels while removing fence markers", func() {
		body := strings.Join([]string{
			"::: info",
			"Helpful details.",
			":::",
			"",
			"::: custom-banner",
			"Custom text.",
			":::",
		}, "\n")

		got := NormalizeMarkdownBody(body)

		Expect(got).NotTo(ContainSubstring(":::"))
		for _, want := range []string{"info", "custom-banner", "Helpful details.", "Custom text."} {
			Expect(got).To(ContainSubstring(want))
		}
	})

	It("leaves shoutout fence syntax unchanged inside code fences", func() {
		body := strings.Join([]string{
			"```md",
			"::: info",
			"literal",
			":::",
			"```",
		}, "\n")

		Expect(NormalizeMarkdownBody(body)).To(Equal(body))
	})

	It("renders HTML and markdown links as plain body text", func() {
		body := "<p><strong>Bold</strong> [link](https://example.com)</p>"

		Expect(FromBody(body)).To(Equal("Bold link"))
	})
})

var _ = Describe("excerpt boundary behavior", Label("unit"), func() {
	It("FromContent falls back to raw content when frontmatter parsing fails", func() {
		raw := "---\ntitle: [unterminated\n---\n\nBody after invalid frontmatter."

		got := FromContent(raw)

		Expect(got).To(ContainSubstring("Body after invalid frontmatter."))
	})

	It("FromBody does not truncate exactly MaxRunes runes", func() {
		body := strings.Repeat("a", MaxRunes)

		got := FromBody(body)

		Expect(got).To(Equal(body))
		Expect(got).NotTo(HaveSuffix("..."))
	})

	It("FromBody truncates at a word boundary when a late space exists", func() {
		body := strings.Repeat("a", 130) + " " + strings.Repeat("b", 80)

		got := FromBody(body)

		Expect(got).To(Equal(strings.Repeat("a", 130) + "..."))
	})

	It("FromBody truncation keeps multi-byte runes intact", func() {
		body := strings.Repeat("é", MaxRunes+20)

		got := FromBody(body)

		Expect(got).To(HaveSuffix("..."))
		Expect(got).NotTo(ContainSubstring("�"))
		Expect([]rune(strings.TrimSuffix(got, "..."))).To(HaveLen(MaxRunes))
	})

	It("PlainTextFromMarkdown strips list, quote, backtick, CRLF, image, and link syntax", func() {
		body := "1. `code` and ![alt](image.png)\r\n> [link](https://example.com)\r\n"

		got := PlainTextFromMarkdown(body)

		Expect(got).To(Equal("code and alt link"))
	})

	It("NormalizeMarkdownBody handles tilde fences and preserves shoutout markers inside them", func() {
		body := strings.Join([]string{
			"~~~md",
			"::: info",
			"literal",
			":::",
			"~~~",
		}, "\n")

		Expect(NormalizeMarkdownBody(body)).To(Equal(body))
	})

	It("NormalizeMarkdownBody only closes fences with matching marker characters", func() {
		body := strings.Join([]string{
			"```md",
			"::: info",
			"~~~",
			":::",
			"```",
		}, "\n")

		Expect(NormalizeMarkdownBody(body)).To(Equal(body))
	})

	It("NormalizeMarkdownBody only closes fences with sufficient marker length", func() {
		body := strings.Join([]string{
			"````md",
			"::: info",
			"```",
			":::",
			"````",
		}, "\n")

		Expect(NormalizeMarkdownBody(body)).To(Equal(body))
	})

	It("PlainTextFromMarkdown falls back to normalized markdown when rendering fails", func() {
		previous := mdRenderer
		mdRenderer = failingMarkdownRenderer{}
		DeferCleanup(func() {
			mdRenderer = previous
		})

		Expect(PlainTextFromMarkdown("plain **markdown**")).To(Equal("plain **markdown**"))
	})

	It("keeps the current fence state when a malformed fence marker is empty", func() {
		previous := fencePattern
		fencePattern = regexp.MustCompile(`(?P<marker>)`)
		DeferCleanup(func() {
			fencePattern = previous
		})
		current := &fenceState{markerChar: '`', markerLength: 3}

		Expect(getFenceState("not a fence", current)).To(BeIdenticalTo(current))
	})
})

type failingMarkdownRenderer struct{}

func (failingMarkdownRenderer) Convert([]byte, io.Writer, ...parser.ParseOption) error {
	return errors.New("renderer unavailable")
}

func (failingMarkdownRenderer) Parser() parser.Parser {
	return nil
}

func (failingMarkdownRenderer) SetParser(parser.Parser) {}

func (failingMarkdownRenderer) Renderer() renderer.Renderer {
	return nil
}

func (failingMarkdownRenderer) SetRenderer(renderer.Renderer) {}
