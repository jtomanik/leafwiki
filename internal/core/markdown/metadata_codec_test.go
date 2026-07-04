package markdown

import (
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

type pageDocumentRawErrorCase struct {
	name string
	raw  string
}

var canonicalMetadataSchemaErrorCases = []pageDocumentRawErrorCase{
	{
		name: "unsupported top level key",
		raw: `<!-- leafwiki
version: 1
page:
  id: page-123
document:
  type: note
-->
Body`,
	},
	{
		name: "list field value",
		raw: `<!-- leafwiki
version: 1
page:
  id: page-123
fields:
  aliases:
    - old-example
-->
Body`,
	},
	{
		name: "reserved field prefix",
		raw: `<!-- leafwiki
version: 1
page:
  id: page-123
fields:
  leafwiki_status: hidden
-->
Body`,
	},
	{
		name: "reserved field prefix mixed case",
		raw: `<!-- leafwiki
version: 1
page:
  id: page-123
fields:
  LeafWiki_status: hidden
-->
Body`,
	},
	{
		name: "missing page id",
		raw: `<!-- leafwiki
version: 1
page:
  title: Missing ID
-->
Body`,
	},
	{
		name: "path separator page id",
		raw: `<!-- leafwiki
version: 1
page:
  id: ../other
-->
Body`,
	},
	{
		name: "dot page id",
		raw: `<!-- leafwiki
version: 1
page:
  id: .
-->
Body`,
	},
}

var metadataLookingMarkerVariantErrorCases = []pageDocumentRawErrorCase{
	{
		name: "opening marker with extra text",
		raw: `<!-- leafwiki extra
version: 1
page:
  id: page-123
-->
Body`,
	},
	{
		name: "indented opening marker",
		raw: ` <!-- leafwiki
version: 1
page:
  id: page-123
-->
Body`,
	},
	{
		name: "non standalone closing marker",
		raw: `<!-- leafwiki
version: 1
page:
  id: page-123
--> trailing
Body`,
	},
}

type renderPageDocumentErrorCase struct {
	name string
	doc  PageDocument
}

var invalidCanonicalMetadataRenderCases = []renderPageDocumentErrorCase{
	{
		name: "missing version",
		doc:  PageDocument{Metadata: PageMetadata{Page: PageMetadataPage{ID: "page-123"}}},
	},
	{
		name: "missing page id",
		doc:  PageDocument{Metadata: PageMetadata{Version: 1}},
	},
	{
		name: "list field value",
		doc: PageDocument{Metadata: PageMetadata{
			Version: 1,
			Page:    PageMetadataPage{ID: "page-123"},
			Fields:  map[string]interface{}{"aliases": []interface{}{"old-example"}},
		}},
	},
	{
		name: "reserved field prefix",
		doc: PageDocument{Metadata: PageMetadata{
			Version: 1,
			Page:    PageMetadataPage{ID: "page-123"},
			Fields:  map[string]interface{}{"leafwiki_status": "hidden"},
		}},
	},
}

var _ = ginkgo.Describe("metadata codec", ginkgo.Label("unit"), func() {
	ginkgo.It("parses canonical metadata comments without requiring writeback", func() {
		raw := `<!-- leafwiki
version: 1
page:
  id: page-123
  title: Example Page
  created_at: "2026-06-13T10:00:00Z"
  updated_at: "2026-06-13T11:00:00Z"
  creator_id: alice
  last_author_id: bob
tags:
  - research
  - draft
fields:
  status: open
  priority: 2
  published: false
extra:
  aliases:
    - old-example
-->

# Example Page

Body text.
`

		doc, result, err := ParsePageDocument(raw)
		Expect(err).To(Succeed())
		Expect(parsedPageDocumentFor(doc, result)).To(matchParsedDocumentWithoutWriteback(matchExactPageDocument(PageDocument{
			Body:     "# Example Page\n\nBody text.\n",
			Metadata: exampleCanonicalMetadata(),
		})))
	})

	ginkgo.It("renders canonical metadata comments in stable field order", func() {
		doc := PageDocument{
			Metadata: exampleCanonicalMetadata(),
			Body:     "# Example Page\n\nBody text.\n",
		}

		got, err := RenderPageDocument(doc)
		Expect(err).To(Succeed())

		want := `<!-- leafwiki
version: 1
page:
  id: page-123
  title: Example Page
  created_at: "2026-06-13T10:00:00Z"
  updated_at: "2026-06-13T11:00:00Z"
  creator_id: alice
  last_author_id: bob
tags:
  - research
  - draft
fields:
  priority: 2
  published: false
  status: open
extra:
  aliases:
    - old-example
-->

# Example Page

Body text.
`
		Expect(got).To(Equal(want))
	})

	ginkgo.Describe("canonical metadata schema validation", func() {
		for _, tt := range canonicalMetadataSchemaErrorCases {
			tt := tt
			ginkgo.It(tt.name, func() {
				_, _, err := ParsePageDocument(tt.raw)
				Expect(err).To(MatchError(ErrMetadataParse))
			})
		}
	})

	ginkgo.Describe("metadata-looking marker validation", func() {
		for _, tt := range metadataLookingMarkerVariantErrorCases {
			tt := tt
			ginkgo.It(tt.name, func() {
				_, _, err := ParsePageDocument(tt.raw)
				Expect(err).To(MatchError(ErrMetadataParse))
			})
		}
	})

	ginkgo.Describe("canonical metadata render validation", func() {
		for _, tt := range invalidCanonicalMetadataRenderCases {
			tt := tt
			ginkgo.It(tt.name, func() {
				_, err := RenderPageDocument(tt.doc)
				Expect(err).To(MatchError(ErrMetadataParse))
			})
		}
	})

	ginkgo.It("keeps invalid legacy-looking frontmatter when it belongs to the canonical body", func() {
		raw := `<!-- leafwiki
version: 1
page:
  id: page-123
-->
---
leafwiki_id: [broken
---
Body`

		doc, result, err := ParsePageDocument(raw)
		Expect(err).To(Succeed())
		Expect(parsedPageDocumentFor(doc, result)).To(matchParsedDocumentWithoutWriteback(HaveField("Body", Equal("---\nleafwiki_id: [broken\n---\nBody"))))
	})

	ginkgo.It("strips legacy frontmatter after a blank separator below canonical metadata", func() {
		raw := `<!-- leafwiki
version: 1
page:
  id: page-123
-->

---
leafwiki_id: legacy-page
leafwiki_title: Legacy Page
---
Body`

		doc, result, err := ParsePageDocument(raw)
		Expect(err).To(Succeed())
		Expect(parsedPageDocumentFor(doc, result)).To(matchParsedDocumentRequiringWriteback(HaveField("Body", Equal("Body"))))
	})

	ginkgo.It("strips tag and property-only legacy frontmatter below canonical metadata", func() {
		raw := `<!-- leafwiki
version: 1
page:
  id: page-123
-->

---
tags:
  - draft
status: ready
priority: 2
---
Body`

		doc, result, err := ParsePageDocument(raw)
		Expect(err).To(Succeed())
		Expect(parsedPageDocumentFor(doc, result)).To(matchParsedDocumentRequiringWriteback(HaveField("Body", Equal("Body"))))
	})

	ginkgo.It("strips legacy frontmatter with unknown non-scalar values below canonical metadata", func() {
		raw := `<!-- leafwiki
version: 1
page:
  id: page-123
-->

---
aliases:
  - old
nested:
  owner: docs
empty_value: null
---
Body`

		doc, result, err := ParsePageDocument(raw)
		Expect(err).To(Succeed())
		Expect(parsedPageDocumentFor(doc, result)).To(matchParsedDocumentRequiringWriteback(HaveField("Body", Equal("Body"))))
	})

	ginkgo.It("strips title-only legacy frontmatter below canonical metadata", func() {
		raw := `<!-- leafwiki
version: 1
page:
  id: page-123
-->

---
title: User Body
---
Body`

		doc, result, err := ParsePageDocument(raw)
		Expect(err).To(Succeed())
		Expect(parsedPageDocumentFor(doc, result)).To(matchParsedDocumentRequiringWriteback(HaveField("Body", Equal("Body"))))
	})

	ginkgo.It("protects literal body frontmatter when rendering canonical metadata", func() {
		body := "---\ntitle: User Body\n---\nBody"
		raw, err := RenderPageDocument(PageDocument{
			Body: body,
			Metadata: PageMetadata{
				Version: 1,
				Page:    PageMetadataPage{ID: "page-123"},
			},
		})
		Expect(err).To(Succeed())
		Expect(raw).To(ContainSubstring("-->\n\n\n---\ntitle: User Body"))

		doc, result, err := ParsePageDocument(raw)
		Expect(err).To(Succeed())
		Expect(parsedPageDocumentFor(doc, result)).To(matchParsedDocumentWithoutWriteback(HaveField("Body", Equal(body))))
	})

	ginkgo.It("keeps literal canonical comments when they belong to the body", func() {
		raw := `<!-- leafwiki
version: 1
page:
  id: page-123
-->

<!-- leafwiki
version: 1
page:
  id: body-comment
-->
Body`

		doc, result, err := ParsePageDocument(raw)
		Expect(err).To(Succeed())
		Expect(parsedPageDocumentFor(doc, result)).To(matchParsedDocumentWithoutWriteback(HaveField("Body", Equal("<!-- leafwiki\nversion: 1\npage:\n  id: body-comment\n-->\nBody"))))
	})
})
