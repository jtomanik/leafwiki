package markdown

import (
	"errors"

	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/types"
)

type splitFrontmatterCase struct {
	name     string
	input    string
	wantFM   string
	wantBody string
	wantHas  bool
}

var errParsedFrontmatterMissing = errors.New("parsed frontmatter missing")

type frontmatterParseResult struct {
	Frontmatter Frontmatter
	Body        string
	Err         error
}

func parsedFrontmatterResult(fm Frontmatter, body string, has bool, err error) frontmatterParseResult {
	if err != nil {
		return frontmatterParseResult{Frontmatter: fm, Body: body, Err: err}
	}
	if !has {
		return frontmatterParseResult{Frontmatter: fm, Body: body, Err: errParsedFrontmatterMissing}
	}
	return frontmatterParseResult{Frontmatter: fm, Body: body}
}

func haveParsedFrontmatter(frontmatter, body types.GomegaMatcher) types.GomegaMatcher {
	return SatisfyAll(
		HaveField("Frontmatter", frontmatter),
		HaveField("Body", body),
		HaveField("Err", Succeed()),
	)
}

var splitFrontmatterCases = []splitFrontmatterCase{
	{
		name:     "no frontmatter",
		input:    "# Hello\nWorld\n",
		wantFM:   "",
		wantBody: "# Hello\nWorld\n",
		wantHas:  false,
	},
	{
		name:     "simple frontmatter",
		input:    "---\nleafwiki_id: abc123\n---\n# Title\n",
		wantFM:   "leafwiki_id: abc123",
		wantBody: "# Title\n",
		wantHas:  true,
	},
	{
		name:     "frontmatter with blank line",
		input:    "---\nleafwiki_id: abc123\n\n---\nBody\n",
		wantFM:   "leafwiki_id: abc123\n",
		wantBody: "Body\n",
		wantHas:  true,
	},
	{
		name:     "frontmatter with comments",
		input:    "---\n# comment\nleafwiki_id: abc123\n---\nBody\n",
		wantFM:   "# comment\nleafwiki_id: abc123",
		wantBody: "Body\n",
		wantHas:  true,
	},
	{
		name:     "only separator at top (no YAML)",
		input:    "---\nHello\nWorld\n---\nBody\n",
		wantFM:   "",
		wantBody: "---\nHello\nWorld\n---\nBody\n",
		wantHas:  false,
	},
	{
		name:     "horizontal rule later in document",
		input:    "# Title\n\n---\n\nText\n",
		wantFM:   "",
		wantBody: "# Title\n\n---\n\nText\n",
		wantHas:  false,
	},
	{
		name:     "unclosed frontmatter",
		input:    "---\nleafwiki_id: abc123\nBody\n",
		wantFM:   "",
		wantBody: "---\nleafwiki_id: abc123\nBody\n",
		wantHas:  false,
	},
	{
		name:     "empty frontmatter block",
		input:    "---\n---\nBody\n",
		wantFM:   "",
		wantBody: "---\n---\nBody\n",
		wantHas:  false,
	},
	{
		name:     "frontmatter with windows line endings",
		input:    "---\r\nleafwiki_id: abc123\r\n---\r\nBody\r\n",
		wantFM:   "leafwiki_id: abc123",
		wantBody: "Body\n",
		wantHas:  true,
	},
	{
		name:     "frontmatter with BOM",
		input:    "\ufeff---\nleafwiki_id: abc123\n---\nBody\n",
		wantFM:   "leafwiki_id: abc123",
		wantBody: "Body\n",
		wantHas:  true,
	},
	{
		name:     "yaml but no key colon (treated as no frontmatter)",
		input:    "---\n- item1\n- item2\n---\nBody\n",
		wantFM:   "",
		wantBody: "---\n- item1\n- item2\n---\nBody\n",
		wantHas:  false,
	},
	{
		name:     "markdown separator block with smiley is not frontmatter",
		input:    "---\n__Advertisement :)__\n---\nBody\n",
		wantFM:   "",
		wantBody: "---\n__Advertisement :)__\n---\nBody\n",
		wantHas:  false,
	},
	{
		name:     "reference-style link definition is not frontmatter",
		input:    "---\n[id]: https://example.com/demo\n---\nBody\n",
		wantFM:   "",
		wantBody: "---\n[id]: https://example.com/demo\n---\nBody\n",
		wantHas:  false,
	},
}

type parseFrontmatterCase struct {
	name        string
	input       string
	wantFM      Frontmatter
	wantBody    string
	wantHas     bool
	wantErr     bool
	wantErrType error
}

var parseFrontmatterCases = []parseFrontmatterCase{
	{
		name:     "no frontmatter",
		input:    "# Hello\nWorld\n",
		wantFM:   Frontmatter{},
		wantBody: "# Hello\nWorld\n",
		wantHas:  false,
		wantErr:  false,
	},
	{
		name:  "valid frontmatter with ID only",
		input: "---\nleafwiki_id: abc123\n---\n# Title\nContent",
		wantFM: Frontmatter{
			LeafWikiID: "abc123",
		},
		wantBody: "# Title\nContent",
		wantHas:  true,
		wantErr:  false,
	},
	{
		name:  "valid frontmatter with title only",
		input: "---\nleafwiki_title: My Title\n---\n# Title\nContent",
		wantFM: Frontmatter{
			LeafWikiTitle: "My Title",
		},
		wantBody: "# Title\nContent",
		wantHas:  true,
		wantErr:  false,
	},
	{
		name:  "title alias is mapped and preserved",
		input: "---\ntitle: My Title\n---\n# Title\nContent",
		wantFM: Frontmatter{
			LeafWikiTitle: "My Title",
			ExtraFields: map[string]interface{}{
				"title": "My Title",
			},
			TitleFromAlias: true,
		},
		wantBody: "# Title\nContent",
		wantHas:  true,
		wantErr:  false,
	},
	{
		name:  "valid frontmatter with both ID and title",
		input: "---\nleafwiki_id: abc123\nleafwiki_title: My Title\n---\n# Title\nContent",
		wantFM: Frontmatter{
			LeafWikiID:    "abc123",
			LeafWikiTitle: "My Title",
		},
		wantBody: "# Title\nContent",
		wantHas:  true,
		wantErr:  false,
	},
	{
		name:  "valid frontmatter with leafwiki metadata",
		input: "---\nleafwiki_id: abc123\nleafwiki_created_at: 2026-03-21T10:15:30Z\nleafwiki_updated_at: 2026-03-21T11:16:31Z\nleafwiki_creator_id: alice\nleafwiki_last_author_id: bob\n---\nBody",
		wantFM: Frontmatter{
			LeafWikiID:           "abc123",
			LeafWikiCreatedAt:    "2026-03-21T10:15:30Z",
			LeafWikiUpdatedAt:    "2026-03-21T11:16:31Z",
			LeafWikiCreatorID:    "alice",
			LeafWikiLastAuthorID: "bob",
		},
		wantBody: "Body",
		wantHas:  true,
		wantErr:  false,
	},
	{
		name:  "unknown fields are preserved",
		input: "---\nkey: value\n---\nBody",
		wantFM: Frontmatter{
			ExtraFields: map[string]interface{}{
				"key": "value",
			},
		},
		wantBody: "Body",
		wantHas:  true,
		wantErr:  false,
	},
	{
		name:        "invalid YAML in frontmatter",
		input:       "---\nleafwiki_id: [invalid: yaml: structure\n---\nBody",
		wantFM:      Frontmatter{},
		wantBody:    "---\nleafwiki_id: [invalid: yaml: structure\n---\nBody",
		wantHas:     true,
		wantErr:     true,
		wantErrType: ErrFrontmatterParse,
	},
	{
		name:        "malformed YAML - unclosed brackets",
		input:       "---\nleafwiki_id: {unclosed\n---\nBody",
		wantFM:      Frontmatter{},
		wantBody:    "---\nleafwiki_id: {unclosed\n---\nBody",
		wantHas:     true,
		wantErr:     true,
		wantErrType: ErrFrontmatterParse,
	},
	{
		name:  "frontmatter with extra fields",
		input: "---\nleafwiki_id: abc123\nextra_field: ignored\n---\nBody",
		wantFM: Frontmatter{
			LeafWikiID: "abc123",
			ExtraFields: map[string]interface{}{
				"extra_field": "ignored",
			},
		},
		wantBody: "Body",
		wantHas:  true,
		wantErr:  false,
	},
	{
		name:  "template placeholder scalar is treated as string",
		input: "---\nDatum: {{date}}\n---\nBody",
		wantFM: Frontmatter{
			ExtraFields: map[string]interface{}{
				"Datum": "{{date}}",
			},
		},
		wantBody: "Body",
		wantHas:  true,
		wantErr:  false,
	},
	{
		name:  "frontmatter with whitespace in values",
		input: "---\nleafwiki_id: \"  abc123  \"\nleafwiki_title: \"  My Title  \"\n---\nBody",
		wantFM: Frontmatter{
			LeafWikiID:    "abc123",
			LeafWikiTitle: "  My Title  ",
		},
		wantBody: "Body",
		wantHas:  true,
		wantErr:  false,
	},
}

var _ = ginkgo.Describe("frontmatter", func() {
	ginkgo.Describe("frontmatter block splitting", func() {
		for _, tt := range splitFrontmatterCases {
			tt := tt
			ginkgo.It(tt.name, func() {
				fm, body, has := splitFrontmatter(tt.input)

				Expect(has).To(Equal(tt.wantHas))
				Expect(fm).To(Equal(tt.wantFM))
				Expect(body).To(Equal(tt.wantBody))
			})
		}
	})

	ginkgo.Describe("legacy frontmatter parsing", func() {
		for _, tt := range parseFrontmatterCases {
			tt := tt
			ginkgo.It(tt.name, func() {
				fm, body, has, err := ParseFrontmatter(tt.input)

				if tt.wantErr {
					if tt.wantErrType != nil {
						Expect(err).To(MatchError(tt.wantErrType))
					} else {
						Expect(err).To(MatchError(ErrFrontmatterParse))
					}
				} else {
					Expect(err).To(Succeed())
				}
				Expect(has).To(Equal(tt.wantHas))
				Expect(fm).To(Equal(tt.wantFM))
				Expect(body).To(Equal(tt.wantBody))
			})
		}
	})

	ginkgo.It("preserves scalar LeafWiki values as frontmatter strings", func() {
		Expect(parsedFrontmatterResult(ParseFrontmatter(`---
leafwiki_id: 123
leafwiki_title: true
---
Body`))).To(haveParsedFrontmatter(
			Equal(Frontmatter{
				LeafWikiID:    "123",
				LeafWikiTitle: "true",
			}),
			Equal("Body"),
		))
	})

	ginkgo.It("adapts canonical metadata comments to legacy frontmatter callers", func() {
		Expect(parsedFrontmatterResult(ParseFrontmatter(`<!-- leafwiki
version: 1
page:
  id: abc123
  title: Canonical Title
tags:
  - demo
fields:
  status: open
-->
Body`))).To(haveParsedFrontmatter(
			Equal(Frontmatter{
				LeafWikiID:    "abc123",
				LeafWikiTitle: "Canonical Title",
				ExtraFields: map[string]interface{}{
					"tags":   []string{"demo"},
					"status": "open",
				},
			}),
			Equal("Body"),
		))
	})
})
