package markdown

import (
	"os"
	"path/filepath"

	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/gstruct"
	"github.com/onsi/gomega/types"
)

func matchPageMetadata(fields gstruct.Fields) types.GomegaMatcher {
	return gstruct.MatchFields(gstruct.IgnoreExtras, fields)
}

func matchPageMetadataPage(fields gstruct.Fields) types.GomegaMatcher {
	return gstruct.MatchFields(gstruct.IgnoreExtras, fields)
}

func matchPageDocument(fields gstruct.Fields) types.GomegaMatcher {
	return gstruct.MatchFields(gstruct.IgnoreExtras, fields)
}

var _ = ginkgo.Describe("metadata and file helpers", ginkgo.Label("unit"), func() {
	ginkgo.It("NewMarkdownFile constructs canonical metadata from explicit legacy frontmatter", func() {
		mf := NewMarkdownFile("/tmp/page.md", "# Body\n", Frontmatter{
			LeafWikiID:        " page-123 ",
			LeafWikiTitle:     " Example ",
			LeafWikiCreatedAt: " 2026-06-13T10:00:00Z ",
			ExtraFields: map[string]interface{}{
				"tags":            []interface{}{"research", "draft"},
				"status":          "open",
				"aliases":         []interface{}{"old"},
				"leafwiki_status": "reserved",
			},
		})

		meta := mf.GetMetadata()
		Expect(meta).To(matchPageMetadata(gstruct.Fields{
			"Version": Equal(1),
			"Page": SatisfyAll(
				matchPageMetadataPageID(newFixturePageMetadataID("page-123")),
				matchPageMetadataPage(gstruct.Fields{
					"Title":     Equal("Example"),
					"CreatedAt": Equal("2026-06-13T10:00:00Z"),
				}),
			),
			"Tags":   Equal([]string{"research", "draft"}),
			"Fields": HaveKeyWithValue("status", "open"),
			"Extra": SatisfyAll(
				HaveKeyWithValue("aliases", []interface{}{"old"}),
				HaveKeyWithValue("leafwiki_status", "reserved"),
			),
		}))
	})

	ginkgo.It("MarkdownFile accessors update content and expose path and writeback state", func() {
		mf := NewMarkdownFile("/tmp/page.md", "old body", Frontmatter{LeafWikiID: "page-123"})

		Expect(mf.GetPath()).To(Equal("/tmp/page.md"))
		Expect(mf).To(matchMarkdownFileWithoutWriteback("old body", matchPageMetadata(gstruct.Fields{
			"Page": matchPageMetadataPageID(newFixturePageMetadataID("page-123")),
		})))

		mf.SetContent("new body")
		Expect(mf.GetContent()).To(Equal("new body"))
	})

	ginkgo.It("GetMetadata returns an isolated copy of slices and maps", func() {
		mf := NewMarkdownFile("/tmp/page.md", "body", Frontmatter{
			LeafWikiID: "page-123",
			ExtraFields: map[string]interface{}{
				"tags":    []interface{}{"original"},
				"status":  "draft",
				"aliases": []interface{}{"old"},
			},
		})

		meta := mf.GetMetadata()
		meta.Tags[0] = "changed"
		meta.Fields["status"] = "changed"
		meta.Extra["aliases"] = []interface{}{"changed"}

		fresh := mf.GetMetadata()
		Expect(fresh).To(matchPageMetadata(gstruct.Fields{
			"Tags":   Equal([]string{"original"}),
			"Fields": HaveKeyWithValue("status", "draft"),
			"Extra":  HaveKeyWithValue("aliases", []interface{}{"old"}),
		}))
	})

	ginkgo.It("SetRawContentReplacingManagedMetadata creates empty versioned metadata for raw body content", func() {
		mf := NewMarkdownFile("/tmp/page.md", "old body", Frontmatter{LeafWikiID: "page-123"})

		Expect(mf.SetRawContentReplacingManagedMetadata("plain body")).To(Succeed())

		Expect(mf).To(matchMarkdownFileWithoutWriteback(
			"plain body",
			matchPageMetadata(gstruct.Fields{
				"Version": Equal(1),
				"Page":    matchEmptyPageMetadataPageID(),
			}),
		))
	})

	ginkgo.It("SetRawContentReplacingManagedMetadata replaces identity from parse result and preserves writeback requirement", func() {
		mf := NewMarkdownFile("/tmp/page.md", "old body", Frontmatter{LeafWikiID: "old-page"})

		err := mf.SetRawContentReplacingManagedMetadata(`---
leafwiki_id: new-page
leafwiki_title: New Title
---
New body`)

		Expect(err).NotTo(HaveOccurred())
		Expect(mf).To(matchMarkdownFileRequiringWriteback(
			"New body",
			matchPageMetadata(gstruct.Fields{
				"Page": SatisfyAll(
					matchPageMetadataPageID(newFixturePageMetadataID("new-page")),
					matchPageMetadataPage(gstruct.Fields{
						"Title": Equal("New Title"),
					}),
				),
			}),
		))
	})

	ginkgo.It("SetLeafWikiMetadataIdentity trims values and initializes metadata", func() {
		mf := &MarkdownFile{}

		mf.SetLeafWikiMetadataIdentity(" page-123 ", " Example Title ")

		meta := mf.GetMetadata()
		Expect(meta).To(matchPageMetadata(gstruct.Fields{
			"Version": Equal(1),
			"Page": SatisfyAll(
				matchPageMetadataPageID(newFixturePageMetadataID("page-123")),
				matchPageMetadataPage(gstruct.Fields{
					"Title": Equal("Example Title"),
				}),
			),
		}))
	})

	ginkgo.It("SetLeafWikiMetadata trims audit fields and initializes metadata", func() {
		mf := &MarkdownFile{}

		mf.SetLeafWikiMetadata(" 2026-06-13T10:00:00Z ", " 2026-06-13T11:00:00Z ", " alice ", " bob ")

		meta := mf.GetMetadata()
		Expect(meta).To(matchPageMetadata(gstruct.Fields{
			"Version": Equal(1),
			"Page": matchPageMetadataPage(gstruct.Fields{
				"CreatedAt":    Equal("2026-06-13T10:00:00Z"),
				"UpdatedAt":    Equal("2026-06-13T11:00:00Z"),
				"CreatorID":    Equal("alice"),
				"LastAuthorID": Equal("bob"),
			}),
		}))
	})

	ginkgo.It("BuildMarkdownWithMetadata renders frontmatter-derived canonical metadata", func() {
		raw, err := BuildMarkdownWithMetadata(Frontmatter{
			LeafWikiID:    "page-123",
			LeafWikiTitle: "Example",
			ExtraFields: map[string]interface{}{
				"tags":   []interface{}{"demo"},
				"status": "ready",
			},
		}, "# Body\n")

		Expect(err).NotTo(HaveOccurred())
		doc, result, err := ParsePageDocument(raw)
		Expect(err).NotTo(HaveOccurred())
		Expect(parsedPageDocumentFor(doc, result)).To(matchParsedDocumentWithoutWriteback(matchPageDocument(gstruct.Fields{
			"Metadata": matchPageMetadata(gstruct.Fields{
				"Page": SatisfyAll(
					matchPageMetadataPageID(newFixturePageMetadataID("page-123")),
					matchPageMetadataPage(gstruct.Fields{
						"Title": Equal("Example"),
					}),
				),
				"Tags":   Equal([]string{"demo"}),
				"Fields": HaveKeyWithValue("status", "ready"),
			}),
			"Body": Equal("# Body\n"),
		})))
	})

	ginkgo.It("BuildMarkdownWithMetadata returns metadata validation errors", func() {
		_, err := BuildMarkdownWithMetadata(Frontmatter{}, "body")

		Expect(err).To(MatchError(ErrMetadataPageIDRequired))
		Expect(err).To(MatchError(ErrMetadataParse))
	})

	ginkgo.It("LoadMarkdownFile rejects non-markdown extensions", func() {
		path := filepath.Join(markdownTempDir(), "page.txt")
		Expect(os.WriteFile(path, []byte("body"), 0o644)).To(Succeed())

		mf, err := LoadMarkdownFile(path)

		Expect(mf).To(BeNil())
		Expect(err).To(MatchError(ErrNotMarkdownFile))
	})

	ginkgo.It("LoadMarkdownFile propagates missing file errors", func() {
		path := filepath.Join(markdownTempDir(), "missing.md")

		mf, err := LoadMarkdownFile(path)

		Expect(mf).To(BeNil())
		Expect(err).To(matchMarkdownPathError())
	})
})
