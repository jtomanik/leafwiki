package markdown

import (
	ginkgo "github.com/onsi/ginkgo/v2"
	"strings"
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

var _ = ginkgo.Describe("metadata codec", func() {
	ginkgo.It("TestParsePageDocument_CanonicalMetadataComment", func() {
		t := ginkgo.GinkgoT()
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
		if err != nil {
			t.Fatalf("ParsePageDocument() error = %v", err)
		}
		if result.RequiresWriteback {
			t.Fatalf("canonical metadata should not require writeback")
		}
		if doc.Body != "# Example Page\n\nBody text.\n" {
			t.Fatalf("body = %q", doc.Body)
		}

		meta := doc.Metadata
		if meta.Version != 1 {
			t.Fatalf("version = %d, want 1", meta.Version)
		}
		if meta.Page.ID != "page-123" {
			t.Fatalf("page id = %q", meta.Page.ID)
		}
		if meta.Page.Title != "Example Page" {
			t.Fatalf("page title = %q", meta.Page.Title)
		}
		if meta.Page.CreatedAt != "2026-06-13T10:00:00Z" {
			t.Fatalf("created_at = %q", meta.Page.CreatedAt)
		}
		if meta.Page.UpdatedAt != "2026-06-13T11:00:00Z" {
			t.Fatalf("updated_at = %q", meta.Page.UpdatedAt)
		}
		if meta.Page.CreatorID != "alice" {
			t.Fatalf("creator_id = %q", meta.Page.CreatorID)
		}
		if meta.Page.LastAuthorID != "bob" {
			t.Fatalf("last_author_id = %q", meta.Page.LastAuthorID)
		}
		if len(meta.Tags) != 2 || meta.Tags[0] != "research" || meta.Tags[1] != "draft" {
			t.Fatalf("tags = %#v", meta.Tags)
		}
		if got := meta.Fields["status"]; got != "open" {
			t.Fatalf("status field = %#v", got)
		}
		if got := meta.Fields["priority"]; got != 2 {
			t.Fatalf("priority field = %#v", got)
		}
		if got := meta.Fields["published"]; got != false {
			t.Fatalf("published field = %#v", got)
		}
		aliases, ok := meta.Extra["aliases"].([]interface{})
		if !ok || len(aliases) != 1 || aliases[0] != "old-example" {
			t.Fatalf("aliases extra = %#v", meta.Extra["aliases"])
		}
	})

	ginkgo.It("TestRenderPageDocument_CanonicalMetadataComment", func() {
		t := ginkgo.GinkgoT()
		doc := PageDocument{
			Metadata: PageMetadata{
				Version: 1,
				Page: PageMetadataPage{
					ID:           "page-123",
					Title:        "Example Page",
					CreatedAt:    "2026-06-13T10:00:00Z",
					UpdatedAt:    "2026-06-13T11:00:00Z",
					CreatorID:    "alice",
					LastAuthorID: "bob",
				},
				Tags: []string{"research", "draft"},
				Fields: map[string]interface{}{
					"status":    "open",
					"priority":  2,
					"published": false,
				},
				Extra: map[string]interface{}{
					"aliases": []interface{}{"old-example"},
				},
			},
			Body: "# Example Page\n\nBody text.\n",
		}

		got, err := RenderPageDocument(doc)
		if err != nil {
			t.Fatalf("RenderPageDocument() error = %v", err)
		}

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
		if got != want {
			t.Fatalf("RenderPageDocument() =\n%q\nwant:\n%q", got, want)
		}
	})

	ginkgo.Describe("TestParsePageDocument_CanonicalMetadataSchemaErrors", func() {
		for _, tt := range canonicalMetadataSchemaErrorCases {
			tt := tt
			ginkgo.It(tt.name, func() {
				t := ginkgo.GinkgoT()
				doc, _, err := ParsePageDocument(tt.raw)
				if err == nil {
					t.Fatalf("ParsePageDocument() error = nil, doc = %#v", doc)
				}
			})
		}
	})

	ginkgo.Describe("TestParsePageDocument_MetadataLookingMarkerVariantsFail", func() {
		for _, tt := range metadataLookingMarkerVariantErrorCases {
			tt := tt
			ginkgo.It(tt.name, func() {
				t := ginkgo.GinkgoT()
				if _, _, err := ParsePageDocument(tt.raw); err == nil {
					t.Fatalf("ParsePageDocument() error = nil")
				}
			})
		}
	})

	ginkgo.Describe("TestRenderPageDocument_RejectsInvalidCanonicalMetadata", func() {
		for _, tt := range invalidCanonicalMetadataRenderCases {
			tt := tt
			ginkgo.It(tt.name, func() {
				t := ginkgo.GinkgoT()
				if got, err := RenderPageDocument(tt.doc); err == nil {
					t.Fatalf("RenderPageDocument() error = nil, got %q", got)
				}
			})
		}
	})

	ginkgo.It("TestParsePageDocument_CanonicalMetadataKeepsInvalidLegacyLookingBody", func() {
		t := ginkgo.GinkgoT()
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
		if err != nil {
			t.Fatalf("ParsePageDocument() error = %v", err)
		}
		if result.RequiresWriteback {
			t.Fatalf("literal body content should not require metadata writeback")
		}
		wantBody := "---\nleafwiki_id: [broken\n---\nBody"
		if doc.Body != wantBody {
			t.Fatalf("body = %q, want %q", doc.Body, wantBody)
		}
	})

	ginkgo.It("TestParsePageDocument_CanonicalMetadataStripsLegacyFrontmatterAfterBlankSeparator", func() {
		t := ginkgo.GinkgoT()
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
		if err != nil {
			t.Fatalf("ParsePageDocument() error = %v", err)
		}
		if !result.RequiresWriteback {
			t.Fatalf("mixed canonical/legacy metadata should require writeback")
		}
		wantBody := "Body"
		if doc.Body != wantBody {
			t.Fatalf("body = %q, want %q", doc.Body, wantBody)
		}
	})

	ginkgo.It("TestParsePageDocument_CanonicalMetadataStripsTagsAndPropertiesOnlyLegacyFrontmatter", func() {
		t := ginkgo.GinkgoT()
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
		if err != nil {
			t.Fatalf("ParsePageDocument() error = %v", err)
		}
		if !result.RequiresWriteback {
			t.Fatalf("mixed canonical/legacy metadata should require writeback")
		}
		if doc.Body != "Body" {
			t.Fatalf("body = %q, want Body", doc.Body)
		}
	})

	ginkgo.It("TestParsePageDocument_CanonicalMetadataStripsLegacyFrontmatterWithUnknownNonScalarValues", func() {
		t := ginkgo.GinkgoT()
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
		if err != nil {
			t.Fatalf("ParsePageDocument() error = %v", err)
		}
		if !result.RequiresWriteback {
			t.Fatalf("mixed canonical/legacy metadata should require writeback")
		}
		if doc.Body != "Body" {
			t.Fatalf("body = %q, want Body", doc.Body)
		}
	})

	ginkgo.It("TestParsePageDocument_CanonicalMetadataStripsTitleOnlyLegacyFrontmatterAfterBlankSeparator", func() {
		t := ginkgo.GinkgoT()
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
		if err != nil {
			t.Fatalf("ParsePageDocument() error = %v", err)
		}
		if !result.RequiresWriteback {
			t.Fatalf("mixed canonical/legacy metadata should require writeback")
		}
		if doc.Body != "Body" {
			t.Fatalf("body = %q, want Body", doc.Body)
		}
	})

	ginkgo.It("TestRenderPageDocument_ProtectsLiteralBodyFrontmatter", func() {
		t := ginkgo.GinkgoT()
		body := "---\ntitle: User Body\n---\nBody"
		raw, err := RenderPageDocument(PageDocument{
			Body: body,
			Metadata: PageMetadata{
				Version: 1,
				Page:    PageMetadataPage{ID: "page-123"},
			},
		})
		if err != nil {
			t.Fatalf("RenderPageDocument() error = %v", err)
		}
		if !strings.Contains(raw, "-->\n\n\n---\ntitle: User Body") {
			t.Fatalf("rendered document did not protect body frontmatter:\n%s", raw)
		}

		doc, result, err := ParsePageDocument(raw)
		if err != nil {
			t.Fatalf("ParsePageDocument() error = %v", err)
		}
		if result.RequiresWriteback {
			t.Fatalf("renderer-protected body frontmatter should not require writeback")
		}
		if doc.Body != body {
			t.Fatalf("body = %q, want %q", doc.Body, body)
		}
	})

	ginkgo.It("TestParsePageDocument_CanonicalMetadataKeepsLiteralCanonicalCommentBody", func() {
		t := ginkgo.GinkgoT()
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
		if err != nil {
			t.Fatalf("ParsePageDocument() error = %v", err)
		}
		if result.RequiresWriteback {
			t.Fatalf("literal body comment should not require metadata writeback")
		}
		wantBody := "<!-- leafwiki\nversion: 1\npage:\n  id: body-comment\n-->\nBody"
		if doc.Body != wantBody {
			t.Fatalf("body = %q, want %q", doc.Body, wantBody)
		}
	})
})
