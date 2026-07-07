package markdown

import (
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/gstruct"
)

var _ = ginkgo.Describe("metadata migration", ginkgo.Label("unit"), func() {
	ginkgo.It("migrates legacy frontmatter into canonical metadata and requests writeback", func() {
		raw := `---
leafwiki_id: page-123
leafwiki_title: Example Page
leafwiki_created_at: "2026-06-13T10:00:00Z"
leafwiki_updated_at: "2026-06-13T11:00:00Z"
leafwiki_creator_id: alice
leafwiki_last_author_id: bob
tags:
  - research
status: open
priority: 2
published: false
aliases:
  - old-example
---
# Example Page
`

		doc, result, err := ParsePageDocument(raw)
		Expect(err).To(Succeed())
		Expect(parsedPageDocumentFor(doc, result)).To(matchParsedDocumentRequiringWriteback(matchExactPageDocument(PageDocument{
			Body: "# Example Page\n",
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
				Tags: []string{"research"},
				Fields: map[string]interface{}{
					"status":    "open",
					"priority":  2,
					"published": false,
				},
				Extra: map[string]interface{}{
					"aliases": []interface{}{"old-example"},
				},
			},
		})))
	})

	ginkgo.It("uses title aliases only when the managed title is absent", func() {
		raw := `---
leafwiki_title: Managed Title
title: User Title Field
---
# Body
`

		doc, _, err := ParsePageDocument(raw)
		Expect(err).To(Succeed())
		Expect(doc.Metadata.Page.Title).To(Equal("Managed Title"))
		Expect(doc.Metadata.Fields).To(HaveKeyWithValue("title", "User Title Field"))

		aliasOnly := `---
title: Alias Title
---
# Body
`
		doc, _, err = ParsePageDocument(aliasOnly)
		Expect(err).To(Succeed())
		Expect(doc.Metadata.Page.Title).To(Equal("Alias Title"))
		Expect(doc.Metadata.Fields).NotTo(HaveKey("title"))
	})

	ginkgo.It("keeps mixed-case reserved legacy keys out of canonical fields", func() {
		raw := `---
leafwiki_id: page-123
LeafWiki_status: hidden
status: public
---
# Body
`

		doc, _, err := ParsePageDocument(raw)
		Expect(err).To(Succeed())
		Expect(doc.Metadata).To(matchExactPageMetadata(PageMetadata{
			Version: 1,
			Page:    PageMetadataPage{ID: "page-123"},
			Fields: map[string]interface{}{
				"status": "public",
			},
			Extra: map[string]interface{}{
				"LeafWiki_status": "hidden",
			},
		}))
	})

	ginkgo.It("rejects malformed legacy frontmatter without generating page identity", func() {
		raw := `---
leafwiki_id: [broken
---
# Body
`

		doc, result, err := ParsePageDocument(raw)
		Expect(err).To(MatchError(ErrFrontmatterParse))
		Expect(parsedPageDocumentFor(doc, result)).To(matchParsedDocumentWithoutWriteback(HaveField("Metadata.Page.ID", BeEmpty())))
	})

	ginkgo.It("keeps unknown map and null legacy values in extra metadata", func() {
		raw := `---
leafwiki_id: page-123
nested:
  owner: docs
empty_value: null
---
# Body
`

		doc, _, err := ParsePageDocument(raw)
		Expect(err).To(Succeed())
		Expect(doc.Metadata).To(matchExactPageMetadata(PageMetadata{
			Version: 1,
			Page:    PageMetadataPage{ID: "page-123"},
			Fields:  map[string]interface{}{},
			Extra: map[string]interface{}{
				"nested":      map[string]interface{}{"owner": "docs"},
				"empty_value": nil,
			},
		}))
	})

	ginkgo.It("keeps legacy keys that collide with canonical top-level names in extra metadata", func() {
		raw := `---
leafwiki_id: page-123
version: 99
page:
  id: colliding
fields:
  status: hidden
extra:
  owner: docs
---
# Body
`

		doc, _, err := ParsePageDocument(raw)
		Expect(err).To(Succeed())
		Expect(doc.Metadata).To(SatisfyAll(
			matchPageMetadataPageIdentity(newFixturePageMetadataID("page-123")),
			matchPageMetadataLegacyExtra(newFixturePageMetadataLegacyExtra(newFixturePageMetadataID("colliding"))),
			matchPageMetadata(gstruct.Fields{
				"Version": Equal(1),
				"Fields":  Equal(map[string]interface{}{}),
			}),
		))
	})

	ginkgo.It("keeps canonical identity while stripping legacy frontmatter from the body", func() {
		raw := `<!-- leafwiki
version: 1
page:
  id: canonical-id
  title: Canonical Title
-->
---
leafwiki_id: legacy-id
leafwiki_title: Legacy Title
---
# Body
`

		doc, result, err := ParsePageDocument(raw)
		Expect(err).To(Succeed())
		Expect(parsedPageDocumentFor(doc, result)).To(matchParsedDocumentRequiringWriteback(SatisfyAll(
			HaveField("Metadata.Page", Equal(PageMetadataPage{
				ID:    "canonical-id",
				Title: "Canonical Title",
			})),
			HaveField("Body", Equal("# Body\n")),
		)))
	})
})
