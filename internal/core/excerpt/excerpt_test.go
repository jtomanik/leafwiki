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

var _ = Describe("excerpt generation from content", func() {
	It("TestFromContent_PlainText", func() {
		raw := "---\ntitle: Hello\n---\n\nThis is the page body."

		Expect(FromContent(raw)).To(Equal("This is the page body."))
	})

	It("TestFromContent_NoFrontmatter", func() {
		raw := "Just plain content here."

		Expect(FromContent(raw)).To(Equal("Just plain content here."))
	})

	It("TestFromContent_EmptyBody", func() {
		raw := "---\ntitle: Hello\n---\n\n"

		Expect(FromContent(raw)).To(BeEmpty())
	})

	It("TestFromContent_EmptyContent", func() {
		Expect(FromContent("")).To(BeEmpty())
	})

	It("TestFromContent_StripsFencedCode", func() {
		raw := "---\ntitle: T\n---\n\nBefore.\n\n```go\nfunc main() {}\n```\n\nAfter."

		got := FromContent(raw)

		Expect(got).NotTo(ContainSubstring("func main"))
		Expect(got).To(ContainSubstring("Before."))
		Expect(got).To(ContainSubstring("After."))
	})

	It("TestFromContent_StripsMarkdownHeadings", func() {
		raw := "---\ntitle: T\n---\n\n# Heading One\n\nSome body text."

		got := FromContent(raw)

		Expect(got).NotTo(ContainSubstring("#"))
		Expect(got).To(ContainSubstring("Heading One"))
	})

	It("TestFromContent_StripsImageSyntax", func() {
		raw := "---\ntitle: T\n---\n\n![alt text](image.png) Some text."

		got := FromContent(raw)

		Expect(got).NotTo(ContainSubstring("!["))
		Expect(got).NotTo(ContainSubstring("image.png"))
		Expect(got).To(ContainSubstring("alt text"))
	})

	It("TestFromContent_StripsLinkSyntax", func() {
		raw := "---\ntitle: T\n---\n\n[Click here](https://example.com) for more."

		got := FromContent(raw)

		Expect(got).NotTo(ContainSubstring("https://example.com"))
		Expect(got).NotTo(ContainSubstring("]("))
		Expect(got).To(ContainSubstring("Click here"))
	})

	It("TestFromContent_TruncatesLongContent", func() {
		body := strings.Repeat("word ", 200)
		raw := "---\ntitle: T\n---\n\n" + body

		got := FromContent(raw)

		Expect(got).To(HaveSuffix("..."))
		Expect(len([]rune(got))).To(BeNumerically("<=", MaxRunes+10))
	})

	It("TestFromContent_ShortContentNotTruncated", func() {
		raw := "---\ntitle: T\n---\n\nShort body."

		Expect(FromContent(raw)).NotTo(HaveSuffix("..."))
	})

	It("TestFromContent_CollapsesWhitespace", func() {
		raw := "---\ntitle: T\n---\n\nLine one.\n\nLine two.\n\nLine three."

		got := FromContent(raw)

		Expect(got).NotTo(ContainSubstring("\n"))
		Expect(got).NotTo(ContainSubstring("  "))
	})

	It("TestFromContent_StripsHTMLTags", func() {
		raw := "---\ntitle: T\n---\n\n<strong>Bold</strong> text."

		got := FromContent(raw)

		Expect(got).NotTo(ContainSubstring("<strong>"))
		Expect(got).NotTo(ContainSubstring("</strong>"))
	})

	It("TestFromContent_StripsMarkdownEmphasisMarkers", func() {
		raw := "---\ntitle: T\n---\n\nLeafWiki **fett** und _kursiv_."

		got := FromContent(raw)

		Expect(got).NotTo(ContainSubstring("**"))
		Expect(got).NotTo(ContainSubstring("_"))
		Expect(got).To(ContainSubstring("LeafWiki fett und kursiv."))
	})
})

var _ = Describe("markdown normalization", func() {
	It("TestNormalizeMarkdownBody_KeepsLabelsAndRemovesShoutoutFenceSyntax", func() {
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

	It("TestNormalizeMarkdownBody_IgnoresCodeFences", func() {
		body := strings.Join([]string{
			"```md",
			"::: info",
			"literal",
			":::",
			"```",
		}, "\n")

		Expect(NormalizeMarkdownBody(body)).To(Equal(body))
	})

	It("TestFromBody_StripsHTMLAndMarkdown", func() {
		body := "<p><strong>Bold</strong> [link](https://example.com)</p>"

		Expect(FromBody(body)).To(Equal("Bold link"))
	})
})

var _ = Describe("excerpt edge coverage", func() {
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

	It("getFenceState leaves current state unchanged when a marker capture is empty", func() {
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
