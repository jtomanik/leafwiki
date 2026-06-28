package markdown

import (
	"errors"
	"os"
	"path/filepath"

	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = ginkgo.Describe("metadata and file helpers", func() {
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
		Expect(meta.Version).To(Equal(1))
		Expect(meta.Page.ID).To(Equal("page-123"))
		Expect(meta.Page.Title).To(Equal("Example"))
		Expect(meta.Page.CreatedAt).To(Equal("2026-06-13T10:00:00Z"))
		Expect(meta.Tags).To(Equal([]string{"research", "draft"}))
		Expect(meta.Fields).To(HaveKeyWithValue("status", "open"))
		Expect(meta.Extra).To(HaveKeyWithValue("aliases", []interface{}{"old"}))
		Expect(meta.Extra).To(HaveKeyWithValue("leafwiki_status", "reserved"))
	})

	ginkgo.It("MarkdownFile accessors update content and expose path and writeback state", func() {
		mf := NewMarkdownFile("/tmp/page.md", "old body", Frontmatter{LeafWikiID: "page-123"})

		Expect(mf.GetPath()).To(Equal("/tmp/page.md"))
		Expect(mf.GetContent()).To(Equal("old body"))
		Expect(mf.RequiresWriteback()).To(BeFalse())

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
		Expect(fresh.Tags).To(Equal([]string{"original"}))
		Expect(fresh.Fields).To(HaveKeyWithValue("status", "draft"))
		Expect(fresh.Extra).To(HaveKeyWithValue("aliases", []interface{}{"old"}))
	})

	ginkgo.It("SetRawContentReplacingManagedMetadata creates empty versioned metadata for raw body content", func() {
		mf := NewMarkdownFile("/tmp/page.md", "old body", Frontmatter{LeafWikiID: "page-123"})

		Expect(mf.SetRawContentReplacingManagedMetadata("plain body")).To(Succeed())

		Expect(mf.GetContent()).To(Equal("plain body"))
		Expect(mf.RequiresWriteback()).To(BeFalse())
		Expect(mf.GetMetadata().Version).To(Equal(1))
		Expect(mf.GetMetadata().Page.ID).To(BeEmpty())
	})

	ginkgo.It("SetRawContentReplacingManagedMetadata replaces identity from parse result and preserves writeback requirement", func() {
		mf := NewMarkdownFile("/tmp/page.md", "old body", Frontmatter{LeafWikiID: "old-page"})

		err := mf.SetRawContentReplacingManagedMetadata(`---
leafwiki_id: new-page
leafwiki_title: New Title
---
New body`)

		Expect(err).NotTo(HaveOccurred())
		Expect(mf.GetMetadata().Page.ID).To(Equal("new-page"))
		Expect(mf.GetMetadata().Page.Title).To(Equal("New Title"))
		Expect(mf.GetContent()).To(Equal("New body"))
		Expect(mf.RequiresWriteback()).To(BeTrue())
	})

	ginkgo.It("SetLeafWikiMetadataIdentity trims values and initializes metadata", func() {
		mf := &MarkdownFile{}

		mf.SetLeafWikiMetadataIdentity(" page-123 ", " Example Title ")

		meta := mf.GetMetadata()
		Expect(meta.Version).To(Equal(1))
		Expect(meta.Page.ID).To(Equal("page-123"))
		Expect(meta.Page.Title).To(Equal("Example Title"))
	})

	ginkgo.It("SetLeafWikiMetadata trims audit fields and initializes metadata", func() {
		mf := &MarkdownFile{}

		mf.SetLeafWikiMetadata(" 2026-06-13T10:00:00Z ", " 2026-06-13T11:00:00Z ", " alice ", " bob ")

		meta := mf.GetMetadata()
		Expect(meta.Version).To(Equal(1))
		Expect(meta.Page.CreatedAt).To(Equal("2026-06-13T10:00:00Z"))
		Expect(meta.Page.UpdatedAt).To(Equal("2026-06-13T11:00:00Z"))
		Expect(meta.Page.CreatorID).To(Equal("alice"))
		Expect(meta.Page.LastAuthorID).To(Equal("bob"))
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
		Expect(result.RequiresWriteback).To(BeFalse())
		Expect(doc.Metadata.Page.ID).To(Equal("page-123"))
		Expect(doc.Metadata.Page.Title).To(Equal("Example"))
		Expect(doc.Metadata.Tags).To(Equal([]string{"demo"}))
		Expect(doc.Metadata.Fields).To(HaveKeyWithValue("status", "ready"))
		Expect(doc.Body).To(Equal("# Body\n"))
	})

	ginkgo.It("BuildMarkdownWithMetadata returns metadata validation errors", func() {
		_, err := BuildMarkdownWithMetadata(Frontmatter{}, "body")

		Expect(err).To(MatchError(ContainSubstring("page.id is required")))
		Expect(errors.Is(err, ErrMetadataParse)).To(BeTrue())
	})

	ginkgo.It("LoadMarkdownFile rejects non-markdown extensions", func() {
		path := filepath.Join(ginkgo.GinkgoT().TempDir(), "page.txt")
		Expect(os.WriteFile(path, []byte("body"), 0o644)).To(Succeed())

		mf, err := LoadMarkdownFile(path)

		Expect(mf).To(BeNil())
		Expect(err).To(MatchError("file is not a markdown file"))
	})

	ginkgo.It("LoadMarkdownFile propagates missing file errors", func() {
		path := filepath.Join(ginkgo.GinkgoT().TempDir(), "missing.md")

		mf, err := LoadMarkdownFile(path)

		Expect(mf).To(BeNil())
		Expect(err).To(HaveOccurred())
	})
})
