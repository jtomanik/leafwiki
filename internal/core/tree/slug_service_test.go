package tree

import (
	"time"

	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = ginkgo.Describe("unique child slug generation", ginkgo.Label("unit"), func() {
	ginkgo.It("uses the normalized title when no sibling conflicts", func() {
		parent := &PageNode{
			Children: []*PageNode{},
		}

		s := NewSlugService()
		result := s.GenerateUniqueChildSlug(parent, "", "My Page")

		Expect(result).To(Equal("my-page"))
	})

	ginkgo.It("adds a numeric suffix when a sibling already has the normalized slug", func() {
		parent := &PageNode{
			Children: []*PageNode{
				{ID: "id", Slug: "my-page"},
			},
		}

		s := NewSlugService()
		result := s.GenerateUniqueChildSlug(parent, "new-id-same-parent", "My Page")

		Expect(result).To(Equal("my-page-1"))
	})

	ginkgo.It("increments the suffix past all existing sibling collisions", func() {
		parent := &PageNode{
			Children: []*PageNode{
				{ID: "id1", Slug: "my-page"},
				{ID: "id2", Slug: "my-page-1"},
				{ID: "id3", Slug: "my-page-2"},
			},
		}

		s := NewSlugService()
		result := s.GenerateUniqueChildSlug(parent, "new-id", "My Page")

		Expect(result).To(Equal("my-page-3"))
	})

	ginkgo.It("keeps the existing slug when the collision belongs to the same child", func() {
		parent := &PageNode{
			Children: []*PageNode{
				{ID: "id1", Slug: "my-page"},
			},
		}

		s := NewSlugService()
		result := s.GenerateUniqueChildSlug(parent, "id1", "My Page")

		Expect(result).To(Equal("my-page"))
	})

	ginkgo.It("normalizes accented and punctuation-heavy titles", func() {
		parent := &PageNode{}

		s := NewSlugService()
		result := s.GenerateUniqueChildSlug(parent, "", "Äpfel & Bäume!")

		Expect(result).To(Equal("apfel-and-baume"))
	})

	ginkgo.It("falls back to a valid page slug when the desired title is blank", func() {
		s := NewSlugService()
		result := make(chan string, 1)

		go func() {
			result <- s.GenerateUniqueChildSlug(&PageNode{}, "", "   ")
		}()

		Eventually(result).WithTimeout(100 * time.Millisecond).Should(Receive(Equal("page")))
		Expect(s.IsValidSlug("page")).To(Succeed())

	})
})

var _ = ginkgo.Describe("path normalization", ginkgo.Label("unit"), func() {
	ginkgo.It("normalizes each path segment into a route-safe slug", func() {
		s := NewSlugService()

		tests := []struct {
			input    string
			expected string
		}{
			{"folder/subfolder/page.md", "folder/subfolder/page-md"},
			{"My Folder/Another Folder/Page Title.md", "my-folder/another-folder/page-title-md"},
			{"Äpfel & Bäume/Über uns.md", "apfel-and-baume/uber-uns-md"},
			{"folder//subfolder///page.md", "folder/subfolder/page-md"},
			{"/leading/slash/page.md", "leading/slash/page-md"},
			{"only-file.md", "only-file-md"},
		}

		for _, test := range tests {

			result, err := s.NormalizePath(test.input, true)
			Expect(err).NotTo(HaveOccurred())
			Expect(result).To(Equal(test.expected))
		}

	})
})

var _ = ginkgo.Describe("slug validation", ginkgo.Label("unit"), func() {
	ginkgo.It("accepts uppercase letters", func() {
		s := NewSlugService()

		Expect(s.IsValidSlug("ABCD-efg")).To(Succeed())
	})
})

var _ = ginkgo.Describe("reserved slug normalization", ginkgo.Label("unit"), func() {
	ginkgo.It("adds a suffix when generating a valid slug for a reserved segment", func() {
		s := NewSlugService()

		Expect(s.GenerateValidSlug("api")).To(Equal("api-1"))
	})

	ginkgo.It("adds a suffix for reserved path segments", func() {
		s := NewSlugService()

		got, err := s.NormalizePathToValidSlugs("Reference/API")
		Expect(err).NotTo(HaveOccurred())
		Expect(got).To(Equal("reference/api-1"))

	})

	ginkgo.It("adds a suffix for reserved filenames while preserving the extension", func() {
		s := NewSlugService()

		got, err := s.NormalizeFilenameToValidSlug("API.md")
		Expect(err).NotTo(HaveOccurred())
		Expect(got).To(Equal("api-1.md"))

	})

	ginkgo.It("normalizes underscores in filenames while preserving the extension", func() {
		s := NewSlugService()

		got, err := s.NormalizeFilenameToValidSlug("CODE_OF_CONDUCT.md")
		Expect(err).NotTo(HaveOccurred())
		Expect(got).To(Equal("code-of-conduct.md"))
	})
})
