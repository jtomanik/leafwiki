package markdown

import (
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"os"
)

var _ = ginkgo.Describe("markdown", func() {
	ginkgo.It("uses the standard frontmatter title before heading fallback", func() {
		tmp := markdownTempDir()
		abs := writeMarkdownTestFile(tmp, "t.md", "---\ntitle: FM Title\n---\n\n# Heading")

		mdFile, err := LoadMarkdownFile(abs)
		Expect(err).To(Succeed())
		title, err := mdFile.GetTitle()
		Expect(err).To(Succeed())
		Expect(title).To(Equal("FM Title"))
	})

	ginkgo.It("uses the LeafWiki frontmatter title before heading fallback", func() {
		tmp := markdownTempDir()
		abs := writeMarkdownTestFile(tmp, "t.md", "---\nleafwiki_title: Leaf\n---\n\n# Heading")

		mdFile, err := LoadMarkdownFile(abs)
		Expect(err).To(Succeed())
		title, err := mdFile.GetTitle()
		Expect(err).To(Succeed())
		Expect(title).To(Equal("Leaf"))
	})

	ginkgo.It("uses the first markdown heading when metadata has no title", func() {
		tmp := markdownTempDir()
		abs := writeMarkdownTestFile(tmp, "t.md", "no fm\n\n# Heading Only\nx")

		mdFile, err := LoadMarkdownFile(abs)
		Expect(err).To(Succeed())
		title, err := mdFile.GetTitle()
		Expect(err).To(Succeed())
		Expect(title).To(Equal("Heading Only"))
	})

	ginkgo.It("uses the filename stem when metadata and headings have no title", func() {
		tmp := markdownTempDir()
		abs := writeMarkdownTestFile(tmp, "some-file.md", "no title")

		mdFile, err := LoadMarkdownFile(abs)
		Expect(err).To(Succeed())
		title, err := mdFile.GetTitle()
		Expect(err).To(Succeed())
		Expect(title).To(Equal("some-file"))
	})

	ginkgo.It("uses the filename stem from Windows paths when no title exists", func() {
		mdFile, err := NewMarkdownFileFromRaw(`C:\Users\johnjkr\AppData\Local\Temp\import-1280817455\1999-07-23 - Memo to Staff.md`, "no title")
		Expect(err).To(Succeed())

		title, err := mdFile.GetTitle()
		Expect(err).To(Succeed())
		Expect(title).To(Equal("1999-07-23 - Memo to Staff"))
	})

	ginkgo.It("writes legacy frontmatter back as canonical metadata while preserving custom fields", func() {
		tmp := markdownTempDir()
		abs := writeMarkdownTestFile(tmp, "t.md", `---
custom_key: keep-me
aliases:
  - one
leafwiki_id: old-id
leafwiki_title: Old Title
---

# Heading`)

		mdFile, err := LoadMarkdownFile(abs)
		Expect(err).To(Succeed())

		mdFile.setMetadataID("new-id")
		Expect(mdFile.WriteToFile()).To(Succeed())

		rawBytes, err := os.ReadFile(abs)
		Expect(err).To(Succeed())
		raw := string(rawBytes)
		Expect(raw).To(HavePrefix("<!-- leafwiki\n"))
		Expect(raw).NotTo(HavePrefix("---\n"))

		doc, result, err := ParsePageDocument(raw)
		Expect(err).To(Succeed())
		Expect(result.RequiresWriteback).To(BeFalse())
		Expect(doc).To(matchExactPageDocument(PageDocument{
			Body: "\n# Heading",
			Metadata: PageMetadata{
				Version: 1,
				Page: PageMetadataPage{
					ID:    "new-id",
					Title: "Old Title",
				},
				Fields: map[string]interface{}{
					"custom_key": "keep-me",
				},
				Extra: map[string]interface{}{
					"aliases": []interface{}{"one"},
				},
			},
		}))
	})

	ginkgo.It("preserves canonical fields and extra boundaries during writeback", func() {
		tmp := markdownTempDir()
		abs := writeMarkdownTestFile(tmp, "t.md", `<!-- leafwiki
version: 1
page:
  id: page-123
  title: Boundary Test
tags:
  - keep
fields:
  priority: 2
  published: false
  status: draft
extra:
  source: imported
  aliases:
    - old
-->

Body`)

		mdFile, err := LoadMarkdownFile(abs)
		Expect(err).To(Succeed())
		Expect(mdFile.WriteToFile()).To(Succeed())

		rawBytes, err := os.ReadFile(abs)
		Expect(err).To(Succeed())
		doc, _, err := ParsePageDocument(string(rawBytes))
		Expect(err).To(Succeed())
		Expect(doc.Metadata).To(matchExactPageMetadata(PageMetadata{
			Version: 1,
			Page: PageMetadataPage{
				ID:    "page-123",
				Title: "Boundary Test",
			},
			Tags: []string{"keep"},
			Fields: map[string]interface{}{
				"priority":  2,
				"published": false,
				"status":    "draft",
			},
			Extra: map[string]interface{}{
				"source":  "imported",
				"aliases": []interface{}{"old"},
			},
		}))
	})

	ginkgo.It("preserves hidden metadata while replacing raw markdown content", func() {
		tmp := markdownTempDir()
		abs := writeMarkdownTestFile(tmp, "page.md", "")
		existing := `<!-- leafwiki
version: 1
page:
  id: page-123
  title: Existing
tags:
  - old
fields:
  priority: 2
  published: false
  status: draft
extra:
  source: imported
  aliases:
    - old
-->

Old body`
		mdFile, err := NewMarkdownFileFromRaw(abs, existing)
		Expect(err).To(Succeed())

		incoming := `<!-- leafwiki
version: 1
page:
  id: page-123
  title: Existing
tags:
  - new
fields:
  status: ready
-->

New body`
		Expect(mdFile.SetRawContentPreservingManagedMetadata(incoming)).To(Succeed())
		Expect(mdFile.WriteToFile()).To(Succeed())
		rawBytes, err := os.ReadFile(abs)
		Expect(err).To(Succeed())
		doc, _, err := ParsePageDocument(string(rawBytes))
		Expect(err).To(Succeed())
		Expect(doc).To(matchExactPageDocument(PageDocument{
			Body: "New body",
			Metadata: PageMetadata{
				Version: 1,
				Page: PageMetadataPage{
					ID:    "page-123",
					Title: "Existing",
				},
				Tags: []string{"new"},
				Fields: map[string]interface{}{
					"status":    "ready",
					"priority":  2,
					"published": false,
				},
				Extra: map[string]interface{}{
					"source":  "imported",
					"aliases": []interface{}{"old"},
				},
			},
		}))
	})

	ginkgo.It("loads markdown files with uppercase extensions", func() {
		tmp := markdownTempDir()
		abs := writeMarkdownTestFile(tmp, "README.MD", "# Uppercase Extension\n\nThis file has .MD extension")

		mdFile, err := LoadMarkdownFile(abs)
		Expect(err).To(Succeed())
		title, err := mdFile.GetTitle()
		Expect(err).To(Succeed())
		Expect(title).To(Equal("Uppercase Extension"))
	})

	ginkgo.It("preserves custom legacy frontmatter when parsing raw markdown", func() {
		mdFile, err := NewMarkdownFileFromRaw("/tmp/test.md", `---
custom_key: keep-me
leafwiki_id: p1
leafwiki_title: Existing Title
---
# Body
Hello
`)
		Expect(err).To(Succeed())

		Expect(mdFile.GetFrontmatter()).To(Equal(Frontmatter{
			LeafWikiID:    "p1",
			LeafWikiTitle: "Existing Title",
			ExtraFields: map[string]interface{}{
				"custom_key": "keep-me",
			},
		}))
		Expect(mdFile.GetContent()).To(Equal("# Body\nHello\n"))
	})

	ginkgo.It("rejects invalid legacy frontmatter in raw markdown", func() {
		_, err := NewMarkdownFileFromRaw("/tmp/test.md", `---
leafwiki_id: [broken
---
# Body
`)
		Expect(err).To(HaveOccurred())
	})
})
