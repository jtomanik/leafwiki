package tree

import (
	"time"

	ginkgo "github.com/onsi/ginkgo/v2"
)

var _ = ginkgo.Describe("TestGenerateUniqueChildSlug_NoConflict", func() {
	ginkgo.It("preserves behavior", func() {
		t := ginkgo.GinkgoT()
		parent := &PageNode{
			Children: []*PageNode{},
		}

		s := NewSlugService()
		result := s.GenerateUniqueChildSlug(parent, "", "My Page")

		if result != "my-page" {
			t.Errorf("Expected 'my-page', got '%s'", result)
		}

	})
})

var _ = ginkgo.Describe("TestGenerateUniqueChildSlug_WithConflict", func() {
	ginkgo.It("preserves behavior", func() {
		t := ginkgo.GinkgoT()
		parent := &PageNode{
			Children: []*PageNode{
				{ID: "id", Slug: "my-page"},
			},
		}

		s := NewSlugService()
		result := s.GenerateUniqueChildSlug(parent, "new-id-same-parent", "My Page")

		if result != "my-page-1" {
			t.Errorf("Expected 'my-page-1', got '%s'", result)
		}

	})
})

var _ = ginkgo.Describe("TestGenerateUniqueChildSlug_MultipleConflicts", func() {
	ginkgo.It("preserves behavior", func() {
		t := ginkgo.GinkgoT()
		parent := &PageNode{
			Children: []*PageNode{
				{ID: "id1", Slug: "my-page"},
				{ID: "id2", Slug: "my-page-1"},
				{ID: "id3", Slug: "my-page-2"},
			},
		}

		s := NewSlugService()
		result := s.GenerateUniqueChildSlug(parent, "new-id", "My Page")

		if result != "my-page-3" {
			t.Errorf("Expected 'my-page-3', got '%s'", result)
		}

	})
})

var _ = ginkgo.Describe("TestGenerateUniqueChildSlug_SlugShouldBeTheSame", func() {
	ginkgo.It("preserves behavior", func() {
		t := ginkgo.GinkgoT()
		parent := &PageNode{
			Children: []*PageNode{
				{ID: "id1", Slug: "my-page"},
			},
		}

		s := NewSlugService()
		result := s.GenerateUniqueChildSlug(parent, "id1", "My Page")

		if result != "my-page" {
			t.Errorf("Expected 'my-page', got '%s'", result)
		}

	})
})

var _ = ginkgo.Describe("TestGenerateUniqueChildSlug_SpecialCharacters", func() {
	ginkgo.It("preserves behavior", func() {
		t := ginkgo.GinkgoT()
		parent := &PageNode{}

		s := NewSlugService()
		result := s.GenerateUniqueChildSlug(parent, "", "Äpfel & Bäume!")

		if result != "apfel-and-baume" {
			t.Errorf("Expected 'aepfel-and-baume', got '%s'", result)
		}

	})
})

var _ = ginkgo.Describe("TestGenerateUniqueChildSlug_EmptyDesiredUsesValidFallback", func() {
	ginkgo.It("preserves behavior", func() {
		t := ginkgo.GinkgoT()
		s := NewSlugService()
		result := make(chan string, 1)

		go func() {
			result <- s.GenerateUniqueChildSlug(&PageNode{}, "", "   ")
		}()

		select {
		case got := <-result:
			if got != "page" {
				t.Fatalf("GenerateUniqueChildSlug(empty) = %q, want fallback slug page", got)
			}
			if err := s.IsValidSlug(got); err != nil {
				t.Fatalf("GenerateUniqueChildSlug(empty) returned invalid slug %q: %v", got, err)
			}
		case <-time.After(100 * time.Millisecond):
			t.Fatal("GenerateUniqueChildSlug(empty) did not return; empty normalized slug must not spin forever")
		}

	})
})

var _ = ginkgo.Describe("TestNormalizePath", func() {
	ginkgo.It("preserves behavior", func() {
		t := ginkgo.GinkgoT()
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
			if err != nil {
				t.Errorf("Unexpected error for input %v: %v", test.input, err)
				continue
			}

			if result != test.expected {
				t.Errorf("For input %v, expected %v but got %v", test.input, test.expected, result)
			}
		}

	})
})

var _ = ginkgo.Describe("TestIsValidSlug_AllowsUppercase", func() {
	ginkgo.It("preserves behavior", func() {
		t := ginkgo.GinkgoT()
		s := NewSlugService()

		if err := s.IsValidSlug("ABCD-efg"); err != nil {
			t.Fatalf("expected uppercase slug to be valid, got %v", err)
		}

	})
})

var _ = ginkgo.Describe("TestGenerateValidSlug_ReservedSlugGetsSuffix", func() {
	ginkgo.It("preserves behavior", func() {
		t := ginkgo.GinkgoT()
		s := NewSlugService()

		if got := s.GenerateValidSlug("api"); got != "api-1" {
			t.Fatalf("GenerateValidSlug(api) = %q, want api-1", got)
		}

	})
})

var _ = ginkgo.Describe("TestNormalizePathToValidSlugs_ReservedSegmentGetsSuffix", func() {
	ginkgo.It("preserves behavior", func() {
		t := ginkgo.GinkgoT()
		s := NewSlugService()

		got, err := s.NormalizePathToValidSlugs("Reference/API")
		if err != nil {
			t.Fatalf("NormalizePathToValidSlugs err: %v", err)
		}
		if got != "reference/api-1" {
			t.Fatalf("NormalizePathToValidSlugs = %q, want reference/api-1", got)
		}

	})
})

var _ = ginkgo.Describe("TestNormalizeFilenameToValidSlug_ReservedSlugGetsSuffix", func() {
	ginkgo.It("preserves behavior", func() {
		t := ginkgo.GinkgoT()
		s := NewSlugService()

		got, err := s.NormalizeFilenameToValidSlug("API.md")
		if err != nil {
			t.Fatalf("NormalizeFilenameToValidSlug err: %v", err)
		}
		if got != "api-1.md" {
			t.Fatalf("NormalizeFilenameToValidSlug = %q, want api-1.md", got)
		}

	})
})

var _ = ginkgo.Describe("TestNormalizeFilenameToValidSlug_UnderscoreFilename", func() {
	ginkgo.It("preserves behavior", func() {
		t := ginkgo.GinkgoT()
		s := NewSlugService()

		got, err := s.NormalizeFilenameToValidSlug("CODE_OF_CONDUCT.md")
		if err != nil {
			t.Fatalf("NormalizeFilenameToValidSlug err: %v", err)
		}
		if got != "code-of-conduct.md" {
			t.Fatalf("NormalizeFilenameToValidSlug = %q, want code-of-conduct.md", got)
		}

	})
})
