package tags

import (
	ginkgo "github.com/onsi/ginkgo/v2"

	"strings"

	"github.com/perber/wiki/internal/core/excerpt"
)

var _ = ginkgo.Describe("TestExtractExcerptFromContent_PlainText", func() {
	ginkgo.It("preserves behavior", func() {
		t := ginkgo.GinkgoT()
	raw := "---\ntitle: Hello\n---\n\nThis is the page body."
	got := ExtractExcerptFromContent(raw)
	if got != "This is the page body." {
		t.Errorf("got %q", got)
	}

	})
})

var _ = ginkgo.Describe("TestExtractExcerptFromContent_NoFrontmatter", func() {
	ginkgo.It("preserves behavior", func() {
		t := ginkgo.GinkgoT()
	raw := "Just plain content here."
	got := ExtractExcerptFromContent(raw)
	if got != "Just plain content here." {
		t.Errorf("got %q", got)
	}

	})
})

var _ = ginkgo.Describe("TestExtractExcerptFromContent_EmptyBody", func() {
	ginkgo.It("preserves behavior", func() {
		t := ginkgo.GinkgoT()
	raw := "---\ntitle: Hello\n---\n\n"
	got := ExtractExcerptFromContent(raw)
	if got != "" {
		t.Errorf("expected empty, got %q", got)
	}

	})
})

var _ = ginkgo.Describe("TestExtractExcerptFromContent_EmptyContent", func() {
	ginkgo.It("preserves behavior", func() {
		t := ginkgo.GinkgoT()
	got := ExtractExcerptFromContent("")
	if got != "" {
		t.Errorf("expected empty, got %q", got)
	}

	})
})

var _ = ginkgo.Describe("TestExtractExcerptFromContent_StripsFencedCode", func() {
	ginkgo.It("preserves behavior", func() {
		t := ginkgo.GinkgoT()
	raw := "---\ntitle: T\n---\n\nBefore.\n\n```go\nfunc main() {}\n```\n\nAfter."
	got := ExtractExcerptFromContent(raw)
	if strings.Contains(got, "func main") {
		t.Errorf("excerpt should not contain fenced code, got %q", got)
	}
	if !strings.Contains(got, "Before.") || !strings.Contains(got, "After.") {
		t.Errorf("excerpt should contain surrounding text, got %q", got)
	}

	})
})

var _ = ginkgo.Describe("TestExtractExcerptFromContent_StripsMarkdownHeadings", func() {
	ginkgo.It("preserves behavior", func() {
		t := ginkgo.GinkgoT()
	raw := "---\ntitle: T\n---\n\n# Heading One\n\nSome body text."
	got := ExtractExcerptFromContent(raw)
	if strings.Contains(got, "#") {
		t.Errorf("excerpt should not contain # heading markers, got %q", got)
	}
	if !strings.Contains(got, "Heading One") {
		t.Errorf("heading text should be preserved, got %q", got)
	}

	})
})

var _ = ginkgo.Describe("TestExtractExcerptFromContent_StripsImageSyntax", func() {
	ginkgo.It("preserves behavior", func() {
		t := ginkgo.GinkgoT()
	raw := "---\ntitle: T\n---\n\n![alt text](image.png) Some text."
	got := ExtractExcerptFromContent(raw)
	if strings.Contains(got, "![") || strings.Contains(got, "image.png") {
		t.Errorf("image syntax should be stripped, got %q", got)
	}
	if !strings.Contains(got, "alt text") {
		t.Errorf("alt text should be preserved, got %q", got)
	}

	})
})

var _ = ginkgo.Describe("TestExtractExcerptFromContent_StripsLinkSyntax", func() {
	ginkgo.It("preserves behavior", func() {
		t := ginkgo.GinkgoT()
	raw := "---\ntitle: T\n---\n\n[Click here](https://example.com) for more."
	got := ExtractExcerptFromContent(raw)
	if strings.Contains(got, "https://example.com") || strings.Contains(got, "](") {
		t.Errorf("link URL should be stripped, got %q", got)
	}
	if !strings.Contains(got, "Click here") {
		t.Errorf("link text should be preserved, got %q", got)
	}

	})
})

var _ = ginkgo.Describe("TestExtractExcerptFromContent_TruncatesLongContent", func() {
	ginkgo.It("preserves behavior", func() {
		t := ginkgo.GinkgoT()
	body := strings.Repeat("word ", 200)
	raw := "---\ntitle: T\n---\n\n" + body
	got := ExtractExcerptFromContent(raw)
	if !strings.HasSuffix(got, "...") {
		t.Errorf("long content should be truncated with ellipsis, got %q", got)
	}
	runes := []rune(got)
	if len(runes) > excerpt.MaxRunes+10 {
		t.Errorf("excerpt too long: %d runes", len(runes))
	}

	})
})

var _ = ginkgo.Describe("TestExtractExcerptFromContent_ShortContentNotTruncated", func() {
	ginkgo.It("preserves behavior", func() {
		t := ginkgo.GinkgoT()
	raw := "---\ntitle: T\n---\n\nShort body."
	got := ExtractExcerptFromContent(raw)
	if strings.HasSuffix(got, "...") {
		t.Errorf("short content should not be truncated, got %q", got)
	}

	})
})

var _ = ginkgo.Describe("TestExtractExcerptFromContent_CollapsesWhitespace", func() {
	ginkgo.It("preserves behavior", func() {
		t := ginkgo.GinkgoT()
	raw := "---\ntitle: T\n---\n\nLine one.\n\nLine two.\n\nLine three."
	got := ExtractExcerptFromContent(raw)
	if strings.Contains(got, "\n") {
		t.Errorf("excerpt should have no newlines, got %q", got)
	}
	if strings.Contains(got, "  ") {
		t.Errorf("excerpt should have no double spaces, got %q", got)
	}

	})
})

var _ = ginkgo.Describe("TestExtractExcerptFromContent_StripsHTMLTags", func() {
	ginkgo.It("preserves behavior", func() {
		t := ginkgo.GinkgoT()
	raw := "---\ntitle: T\n---\n\n<strong>Bold</strong> text."
	got := ExtractExcerptFromContent(raw)
	if strings.Contains(got, "<strong>") || strings.Contains(got, "</strong>") {
		t.Errorf("HTML tags should be stripped, got %q", got)
	}

	})
})
