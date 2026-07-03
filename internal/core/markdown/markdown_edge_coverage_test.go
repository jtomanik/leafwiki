package markdown

import (
	"errors"
	"io"
	"os"
	"path/filepath"

	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	yaml "gopkg.in/yaml.v3"
)

var _ = ginkgo.Describe("markdown metadata parse render and writeback failures", func() {
	ginkgo.It("returns stable errors for malformed legacy frontmatter fallbacks", func() {
		_, err := parseFrontmatterYAML("leafwiki_title: {{title}}\nbroken: [")
		Expect(err).To(HaveOccurred())
		Expect(err).To(MatchError(ErrFrontmatterParse))

		fm, err := parseFrontmatterYAML("")
		Expect(err).NotTo(HaveOccurred())
		Expect(fm.ExtraFields).To(BeNil())
		Expect(valueToString(nil)).To(BeEmpty())

		split, err := markdownBodyWithoutFrontmatterResult("---")
		Expect(err).NotTo(HaveOccurred())
		Expect(split).To(SatisfyAll(
			HaveField("YAML", BeEmpty()),
			HaveField("Body", Equal("---")),
		))

		err = parseFrontmatterWithMetadataResult("<!-- leafwiki")
		Expect(err).To(HaveOccurred())
		Expect(err).To(MatchError(ErrMetadataParse))

		_, err = toYAMLNode(failingYAMLValue{})
		Expect(err).To(HaveOccurred())
	})

	ginkgo.It("preserves or replaces raw content while reporting parse errors", func() {
		mf := NewMarkdownFile("/tmp/page.md", "old body", Frontmatter{
			LeafWikiID: "page-123",
			ExtraFields: map[string]interface{}{
				"status": "draft",
			},
		})

		Expect(mf.SetRawContentPreservingManagedMetadata("plain body")).To(Succeed())
		Expect(mf.GetContent()).To(Equal("plain body"))
		Expect(mf.GetMetadata().Page.ID).To(Equal("page-123"))

		Expect(mf.SetRawContentPreservingManagedMetadata("<!-- leafwiki")).To(MatchError(ErrMetadataParse))
		Expect(mf.SetRawContentReplacingManagedMetadata("<!-- leafwiki")).To(MatchError(ErrMetadataParse))

		canonicalNoFields := `<!-- leafwiki
version: 1
page:
  id: page-456
-->
Body`
		Expect(mf.SetRawContentPreservingManagedMetadata(canonicalNoFields)).To(Succeed())
		Expect(mf.GetMetadata().Fields).To(BeNil())
		Expect(mf.GetMetadata().Extra).To(BeNil())

		canonicalWithExtra := `<!-- leafwiki
version: 1
page:
  id: page-789
extra:
  aliases:
    - old-page
-->
Body`
		Expect(mf.SetRawContentPreservingManagedMetadata(canonicalWithExtra)).To(Succeed())
		Expect(mf.GetMetadata().Extra).To(HaveKey("aliases"))

		legacyRaw := `---
leafwiki_id: legacy-page
leafwiki_title: Legacy Page
---
Legacy body`
		Expect(mf.SetRawContentPreservingManagedMetadata(legacyRaw)).To(Succeed())
		Expect(mf.RequiresWriteback()).To(BeTrue())
		Expect(mf.GetContent()).To(Equal("Legacy body"))
	})

	ginkgo.It("returns write errors before mutating writeback state", func() {
		invalid := &MarkdownFile{
			path: filepath.Join(markdownTempDir(), "page.md"),
			doc:  PageDocument{Metadata: PageMetadata{Version: 1}},
		}
		Expect(invalid.WriteToFile()).To(MatchError(ErrMetadataPageIDRequired))

		blocker := filepath.Join(markdownTempDir(), "not-a-directory")
		Expect(os.WriteFile(blocker, []byte("file"), 0o600)).To(Succeed())
		mf := &MarkdownFile{
			path: filepath.Join(blocker, "page.md"),
			doc: PageDocument{
				Body: "body",
				Metadata: PageMetadata{
					Version: 1,
					Page:    PageMetadataPage{ID: "page-123"},
				},
			},
			requiresWriteback: true,
		}

		Expect(mf.WriteToFile()).To(HaveOccurred())
		Expect(mf.RequiresWriteback()).To(BeTrue())
	})

	ginkgo.It("reports canonical metadata parser and renderer edge errors", func() {
		_, _, err := ParsePageDocument("<!-- leafwiki")
		Expect(err).To(HaveOccurred())
		Expect(err).To(MatchError(ErrMetadataParse))

		_, _, err = ParsePageDocument(`<!-- leafwiki
version: 2
page:
  id: page-123
-->
Body`)
		Expect(err).To(MatchError(ErrUnsupportedMetadataVersion))

		_, err = RenderPageDocument(PageDocument{
			Body: "body",
			Metadata: PageMetadata{
				Version: 1,
				Page:    PageMetadataPage{ID: "page-123"},
				Extra: map[string]interface{}{
					"bad": failingYAMLValue{},
				},
			},
		})
		Expect(err).To(HaveOccurred())

		_, err = renderCanonicalMetadataYAML(PageMetadata{
			Version: 1,
			Page:    PageMetadataPage{ID: "page-123"},
			Fields: map[string]interface{}{
				"bad": failingYAMLValue{},
			},
		})
		Expect(err).To(HaveOccurred())

		mapping := &yaml.Node{Kind: yaml.MappingNode}
		appendYAMLScalar(mapping, "bad", failingYAMLValue{})
		Expect(mapping.Content).To(BeEmpty())
	})

	ginkgo.It("reports canonical metadata encoder failures", func() {
		previousEncoder := newCanonicalMetadataYAMLEncoder
		ginkgo.DeferCleanup(func() {
			newCanonicalMetadataYAMLEncoder = previousEncoder
		})
		validMetadata := PageMetadata{
			Version: 1,
			Page:    PageMetadataPage{ID: "page-123"},
		}

		encodeErr := errors.New("encode failed")
		newCanonicalMetadataYAMLEncoder = func(io.Writer) canonicalMetadataYAMLEncoder {
			return failingCanonicalMetadataYAMLEncoder{encodeErr: encodeErr}
		}
		_, err := renderCanonicalMetadataYAML(validMetadata)
		Expect(err).To(HaveOccurred())
		Expect(err).To(MatchError(ErrMetadataParse))
		Expect(err).To(MatchError(encodeErr))

		closeErr := errors.New("close failed")
		newCanonicalMetadataYAMLEncoder = func(io.Writer) canonicalMetadataYAMLEncoder {
			return failingCanonicalMetadataYAMLEncoder{closeErr: closeErr}
		}
		_, err = renderCanonicalMetadataYAML(validMetadata)
		Expect(err).To(HaveOccurred())
		Expect(err).To(MatchError(ErrMetadataParse))
		Expect(err).To(MatchError(closeErr))
	})

	ginkgo.It("round-trips metadata migration helpers for empty, tag, and default cases", func() {
		fm := pageMetadataToFrontmatter(PageMetadata{
			Page: PageMetadataPage{ID: "page-123", Title: "Page"},
			Tags: []string{"go"},
			Fields: map[string]interface{}{
				"status": "draft",
			},
			Extra: map[string]interface{}{
				"aliases": []interface{}{"old"},
			},
		})
		Expect(fm.ExtraFields).To(HaveKeyWithValue("tags", []string{"go"}))
		Expect(fm.ExtraFields).To(HaveKeyWithValue("status", "draft"))
		Expect(fm.ExtraFields).To(HaveKeyWithValue("aliases", []interface{}{"old"}))

		empty := pageMetadataToFrontmatter(PageMetadata{})
		Expect(empty.ExtraFields).To(BeNil())

		Expect(legacyTags([]string{"go", "docs"})).To(Equal([]string{"go", "docs"}))
		Expect(legacyTags(42)).To(BeNil())
	})
})

type failingYAMLValue struct{}

func (failingYAMLValue) MarshalYAML() (interface{}, error) {
	return nil, errors.New("yaml failed")
}

var errFrontmatterPresent = errors.New("frontmatter present")
var errFrontmatterAbsent = errors.New("frontmatter absent")

type markdownBodyWithoutFrontmatter struct {
	YAML string
	Body string
}

func markdownBodyWithoutFrontmatterResult(md string) (markdownBodyWithoutFrontmatter, error) {
	yamlPart, body, has := splitFrontmatter(md)
	if has {
		return markdownBodyWithoutFrontmatter{}, errFrontmatterPresent
	}
	return markdownBodyWithoutFrontmatter{
		YAML: yamlPart,
		Body: body,
	}, nil
}

func parseFrontmatterWithMetadataResult(md string) error {
	fm, body, has, err := ParseFrontmatter(md)
	if err != nil {
		return err
	}
	if !has {
		return errFrontmatterAbsent
	}
	if fm.LeafWikiID == "" && body == "" {
		return errFrontmatterAbsent
	}
	return nil
}

type failingCanonicalMetadataYAMLEncoder struct {
	encodeErr error
	closeErr  error
}

func (failingCanonicalMetadataYAMLEncoder) SetIndent(int) {}

func (e failingCanonicalMetadataYAMLEncoder) Encode(interface{}) error {
	return e.encodeErr
}

func (e failingCanonicalMetadataYAMLEncoder) Close() error {
	return e.closeErr
}
