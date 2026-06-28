package tags

import (
	ginkgo "github.com/onsi/ginkgo/v2"


	"github.com/perber/wiki/internal/core/tree"
)

// ─── ExtractTagsFromContent ──────────────────────────────────────────────────

var _ = ginkgo.Describe("TestExtractTagsFromContent_BlockListSyntax", func() {
	ginkgo.It("preserves behavior", func() {
		t := ginkgo.GinkgoT()
	content := "---\ntags:\n  - react\n  - typescript\n---\n\n# Page"
	got := ExtractTagsFromContent(content)
	assertStringSliceEqual(t, got, []string{"react", "typescript"})

	})
})

var _ = ginkgo.Describe("TestExtractTagsFromContent_InlineListSyntax", func() {
	ginkgo.It("preserves behavior", func() {
		t := ginkgo.GinkgoT()
	content := "---\ntags: [react, typescript]\n---\n\n# Page"
	got := ExtractTagsFromContent(content)
	assertStringSliceEqual(t, got, []string{"react", "typescript"})

	})
})

var _ = ginkgo.Describe("TestExtractTagsFromContent_NormalizesToLowercase", func() {
	ginkgo.It("preserves behavior", func() {
		t := ginkgo.GinkgoT()
	content := "---\ntags:\n  - React\n  - TypeScript\n  - GO\n---\n"
	got := ExtractTagsFromContent(content)
	assertStringSliceEqual(t, got, []string{"react", "typescript", "go"})

	})
})

var _ = ginkgo.Describe("TestExtractTagsFromContent_DeduplicatesCaseInsensitive", func() {
	ginkgo.It("preserves behavior", func() {
		t := ginkgo.GinkgoT()
	content := "---\ntags:\n  - react\n  - React\n  - REACT\n---\n"
	got := ExtractTagsFromContent(content)
	if len(got) != 1 {
		t.Errorf("expected 1 deduplicated tag, got %d: %v", len(got), got)
	}
	if len(got) > 0 && got[0] != "react" {
		t.Errorf("got[0] = %q, want 'react'", got[0])
	}

	})
})

var _ = ginkgo.Describe("TestExtractTagsFromContent_TrimsWhitespace", func() {
	ginkgo.It("preserves behavior", func() {
		t := ginkgo.GinkgoT()
	content := "---\ntags:\n  - \" react \"\n  - \" go \"\n---\n"
	got := ExtractTagsFromContent(content)
	for _, tag := range got {
		if tag != trimmed(tag) {
			t.Errorf("tag %q has surrounding whitespace", tag)
		}
	}

	})
})

var _ = ginkgo.Describe("TestExtractTagsFromContent_NoFrontmatterReturnsNil", func() {
	ginkgo.It("preserves behavior", func() {
		t := ginkgo.GinkgoT()
	content := "# Page\n\nJust content, no frontmatter."
	got := ExtractTagsFromContent(content)
	if got != nil {
		t.Errorf("expected nil, got %v", got)
	}

	})
})

var _ = ginkgo.Describe("TestExtractTagsFromContent_EmptyContentReturnsNil", func() {
	ginkgo.It("preserves behavior", func() {
		t := ginkgo.GinkgoT()
	got := ExtractTagsFromContent("")
	if got != nil {
		t.Errorf("expected nil for empty content, got %v", got)
	}

	})
})

var _ = ginkgo.Describe("TestExtractTagsFromContent_FrontmatterWithoutTagsFieldReturnsNil", func() {
	ginkgo.It("preserves behavior", func() {
		t := ginkgo.GinkgoT()
	content := "---\ntitle: My Page\nauthor: Alice\n---\n\n# Content"
	got := ExtractTagsFromContent(content)
	if got != nil {
		t.Errorf("expected nil when tags key absent, got %v", got)
	}

	})
})

var _ = ginkgo.Describe("TestExtractTagsFromContent_EmptyTagsListReturnsNil", func() {
	ginkgo.It("preserves behavior", func() {
		t := ginkgo.GinkgoT()
	content := "---\ntags: []\n---\n\n# Content"
	got := ExtractTagsFromContent(content)
	if len(got) != 0 {
		t.Errorf("expected empty, got %v", got)
	}

	})
})

var _ = ginkgo.Describe("TestExtractTagsFromContent_SkipsEmptyTagEntries", func() {
	ginkgo.It("preserves behavior", func() {
		t := ginkgo.GinkgoT()
	content := "---\ntags:\n  - react\n  - \"\"\n  - typescript\n---\n"
	got := ExtractTagsFromContent(content)
	for _, tag := range got {
		if tag == "" {
			t.Errorf("empty tag should be filtered out")
		}
	}
	if len(got) != 2 {
		t.Errorf("expected 2 non-empty tags, got %d: %v", len(got), got)
	}

	})
})

var _ = ginkgo.Describe("TestExtractTagsFromContent_TagsKeyIsCaseInsensitive", func() {
	ginkgo.It("preserves behavior", func() {
		t := ginkgo.GinkgoT()
	// Frontmatter keys like "Tags" or "TAGS" should still be found.
	content := "---\nTags:\n  - react\n---\n"
	got := ExtractTagsFromContent(content)
	if len(got) != 1 || got[0] != "react" {
		t.Errorf("expected [react] for upper-case Tags key, got %v", got)
	}

	})
})

// ─── TagsService integration (with real tree + store) ────────────────────────

func setupTagsService(t tagsTestT) (*TagsService, *tree.TreeService) {
	t.Helper()

	dir := t.TempDir()
	ts := tree.NewTreeService(dir)
	if err := ts.LoadTree(); err != nil {
		t.Fatalf("LoadTree: %v", err)
	}

	store, err := NewTagsStore(dir)
	if err != nil {
		t.Fatalf("NewTagsStore: %v", err)
	}
	t.Cleanup(func() { closeTagsStoreForTest(store, t) })

	return NewTagsService(store), ts
}

func pageKind() *tree.NodeKind {
	k := tree.NodeKindPage
	return &k
}

func createPageWithTags(t tagsTestT, ts *tree.TreeService, title, slug string, tags []string) tree.PageID {
	t.Helper()

	idPtr, err := ts.CreateNode("system", nil, title, newFixtureSlug(slug), pageKind())
	if err != nil {
		t.Fatalf("CreateNode %q: %v", slug, err)
	}

	fm := "---\ntags:\n"
	for _, tag := range tags {
		fm += "  - " + tag + "\n"
	}
	fm += "---\n\n# " + title

	if err := ts.UpdateNodeUncheckedVersion(newFixtureUserID("system"), *idPtr, title, newFixtureSlug(slug), &fm, true); err != nil {
		t.Fatalf("UpdateNode %q: %v", slug, err)
	}

	return *idPtr
}

func indexAllPages(t tagsTestT, svc *TagsService, ts *tree.TreeService) {
	t.Helper()
	var ids []tree.PageID
	if err := ts.WalkNodes(func(id tree.PageID) error {
		ids = append(ids, id)
		return nil
	}); err != nil {
		t.Fatalf("WalkNodes: %v", err)
	}
	pages, errs := ts.GetPages(ids)
	for i, page := range pages {
		if errs[i] != nil {
			t.Fatalf("GetPages[%d]: %v", i, errs[i])
		}
		if err := svc.IndexPageContent(page.ID, page.RawContent); err != nil {
			t.Fatalf("IndexPageContent %s: %v", page.ID, err)
		}
	}
}

var _ = ginkgo.Describe("TestTagsService_IndexAllPages_BuildsIndex", func() {
	ginkgo.It("preserves behavior", func() {
		t := ginkgo.GinkgoT()
	svc, ts := setupTagsService(t)

	id1 := createPageWithTags(t, ts, "Page React", "react-page", []string{"react", "typescript"})
	id2 := createPageWithTags(t, ts, "Page Go", "go-page", []string{"go"})

	indexAllPages(t, svc, ts)

	pageIDs, err := svc.GetPageIDsByTags([]string{"react"})
	if err != nil {
		t.Fatalf("GetPageIDsByTags: %v", err)
	}
	if len(pageIDs) != 1 || pageIDs[0] != id1 {
		t.Errorf("expected [%s], got %v", id1, pageIDs)
	}

	pageIDs2, err := svc.GetPageIDsByTags([]string{"go"})
	if err != nil {
		t.Fatalf("GetPageIDsByTags: %v", err)
	}
	if len(pageIDs2) != 1 || pageIDs2[0] != id2 {
		t.Errorf("expected [%s], got %v", id2, pageIDs2)
	}

	})
})

var _ = ginkgo.Describe("TestTagsService_IndexAllPages_IsIdempotent", func() {
	ginkgo.It("preserves behavior", func() {
		t := ginkgo.GinkgoT()
	svc, ts := setupTagsService(t)
	createPageWithTags(t, ts, "Page A", "page-a", []string{"go"})

	for i := 0; i < 3; i++ {
		if err := svc.ClearIndex(); err != nil {
			t.Fatalf("ClearIndex (run %d): %v", i, err)
		}
		indexAllPages(t, svc, ts)
	}

	allTags, err := svc.GetAllTags("", 50)
	if err != nil {
		t.Fatalf("GetAllTags: %v", err)
	}
	if len(allTags) != 1 || allTags[0].Tag != "go" || allTags[0].Count != 1 {
		t.Errorf("expected [{go 1}], got %v", allTags)
	}

	})
})

var _ = ginkgo.Describe("TestTagsService_IndexAllPages_PagesWithoutTagsAreSkipped", func() {
	ginkgo.It("preserves behavior", func() {
		t := ginkgo.GinkgoT()
	svc, ts := setupTagsService(t)

	idPtr, err := ts.CreateNode("system", nil, "No Tags Page", "no-tags", pageKind())
	if err != nil {
		t.Fatalf("CreateNode: %v", err)
	}
	content := "# No Tags Page\n\nNo frontmatter."
	if err := ts.UpdateNodeUncheckedVersion(newFixtureUserID("system"), *idPtr, "No Tags Page", newFixtureSlug("no-tags"), &content, false); err != nil {
		t.Fatalf("UpdateNode: %v", err)
	}

	indexAllPages(t, svc, ts)

	allTags, err := svc.GetAllTags("", 50)
	if err != nil {
		t.Fatalf("GetAllTags: %v", err)
	}
	if len(allTags) != 0 {
		t.Errorf("expected no tags indexed for page without frontmatter, got %v", allTags)
	}

	})
})

var _ = ginkgo.Describe("TestTagsService_IndexAllPages_NormalizesTagsToLowercase", func() {
	ginkgo.It("preserves behavior", func() {
		t := ginkgo.GinkgoT()
	svc, ts := setupTagsService(t)
	createPageWithTags(t, ts, "Mixed Case", "mixed", []string{"React", "TypeScript"})

	indexAllPages(t, svc, ts)

	allTags, err := svc.GetAllTags("", 50)
	if err != nil {
		t.Fatalf("GetAllTags: %v", err)
	}

	for _, tc := range allTags {
		if tc.Tag != lower(tc.Tag) {
			t.Errorf("tag %q is not lowercase", tc.Tag)
		}
	}

	})
})

var _ = ginkgo.Describe("TestTagsService_IndexAllPages_ReadsTagsFromRawFrontmatter", func() {
	ginkgo.It("preserves behavior", func() {
		t := ginkgo.GinkgoT()
	svc, ts := setupTagsService(t)
	pageID := createPageWithTags(t, ts, "Tagged Page", "tagged-page", []string{"react"})

	page, err := ts.GetPage(pageID)
	if err != nil {
		t.Fatalf("GetPage: %v", err)
	}
	if got := ExtractTagsFromContent(page.Content); got != nil {
		t.Fatalf("expected parsed page content to exclude frontmatter tags, got %v", got)
	}

	indexAllPages(t, svc, ts)

	pageIDs, err := svc.GetPageIDsByTags([]string{"react"})
	if err != nil {
		t.Fatalf("GetPageIDsByTags: %v", err)
	}
	if len(pageIDs) != 1 || pageIDs[0] != pageID {
		t.Fatalf("expected [%s], got %v", pageID, pageIDs)
	}

	})
})

var _ = ginkgo.Describe("TestTagsService_SetAndDeleteTagsForPage", func() {
	ginkgo.It("preserves behavior", func() {
		t := ginkgo.GinkgoT()
	svc, _ := setupTagsService(t)

	if err := svc.SetTagsForPage("page-x", []string{"go", "test"}); err != nil {
		t.Fatalf("SetTagsForPage: %v", err)
	}
	if err := svc.DeleteTagsForPage("page-x"); err != nil {
		t.Fatalf("DeleteTagsForPage: %v", err)
	}

	allTags, err := svc.GetAllTags("", 50)
	if err != nil {
		t.Fatalf("GetAllTags: %v", err)
	}
	if len(allTags) != 0 {
		t.Errorf("expected no tags after delete, got %v", allTags)
	}

	})
})

var _ = ginkgo.Describe("TestTagsService_GetTagsForPages_ReturnsCorrectTags", func() {
	ginkgo.It("preserves behavior", func() {
		t := ginkgo.GinkgoT()
	svc, _ := setupTagsService(t)

	_ = svc.SetTagsForPage("p1", []string{"go", "testing"})
	_ = svc.SetTagsForPage("p2", []string{"typescript"})

	got, err := svc.GetTagsForPages(testPageIDs("p1", "p2"))
	if err != nil {
		t.Fatalf("GetTagsForPages: %v", err)
	}

	assertStringSliceEqual(t, got["p1"], []string{"go", "testing"})
	assertStringSliceEqual(t, got["p2"], []string{"typescript"})

	})
})

// ─── IndexPageContent ─────────────────────────────────────────────────────────

var _ = ginkgo.Describe("TestTagsService_IndexPageContent_StoresTagsAndExcerpt", func() {
	ginkgo.It("preserves behavior", func() {
		t := ginkgo.GinkgoT()
	svc, _ := setupTagsService(t)

	raw := "---\ntags:\n  - go\n  - testing\n---\n\nThis is the page body."
	if err := svc.IndexPageContent("page-1", raw); err != nil {
		t.Fatalf("IndexPageContent: %v", err)
	}

	tags, err := svc.GetAllTags("", 50)
	if err != nil {
		t.Fatalf("GetAllTags: %v", err)
	}
	if len(tags) != 2 {
		t.Fatalf("expected 2 tags, got %v", tags)
	}

	exc, err := svc.GetExcerptsForPages(testPageIDs("page-1"))
	if err != nil {
		t.Fatalf("GetExcerptsForPages: %v", err)
	}
	if exc["page-1"] != "This is the page body." {
		t.Errorf("excerpt = %q", exc["page-1"])
	}

	})
})

var _ = ginkgo.Describe("TestTagsService_IndexPageContent_NoFrontmatterStoresEmptyTags", func() {
	ginkgo.It("preserves behavior", func() {
		t := ginkgo.GinkgoT()
	svc, _ := setupTagsService(t)

	raw := "# Just a page\n\nSome content without frontmatter."
	if err := svc.IndexPageContent("page-1", raw); err != nil {
		t.Fatalf("IndexPageContent: %v", err)
	}

	tags, err := svc.GetAllTags("", 50)
	if err != nil {
		t.Fatalf("GetAllTags: %v", err)
	}
	if len(tags) != 0 {
		t.Errorf("expected no tags, got %v", tags)
	}

	exc, err := svc.GetExcerptsForPages(testPageIDs("page-1"))
	if err != nil {
		t.Fatalf("GetExcerptsForPages: %v", err)
	}
	if exc["page-1"] == "" {
		t.Error("expected non-empty excerpt for page without frontmatter")
	}

	})
})

var _ = ginkgo.Describe("TestTagsService_IndexPageContent_UpdatesExistingEntry", func() {
	ginkgo.It("preserves behavior", func() {
		t := ginkgo.GinkgoT()
	svc, _ := setupTagsService(t)

	if err := svc.IndexPageContent("page-1", "---\ntags:\n  - go\n---\n\nFirst version."); err != nil {
		t.Fatalf("IndexPageContent (first): %v", err)
	}
	if err := svc.IndexPageContent("page-1", "---\ntags:\n  - rust\n---\n\nSecond version."); err != nil {
		t.Fatalf("IndexPageContent (second): %v", err)
	}

	tags, err := svc.GetPageIDsByTags([]string{"rust"})
	if err != nil {
		t.Fatalf("GetPageIDsByTags: %v", err)
	}
	if len(tags) != 1 {
		t.Fatalf("expected [page-1] for rust, got %v", tags)
	}

	oldTags, err := svc.GetPageIDsByTags([]string{"go"})
	if err != nil {
		t.Fatalf("GetPageIDsByTags: %v", err)
	}
	if len(oldTags) != 0 {
		t.Errorf("old tag 'go' should be gone after update, got %v", oldTags)
	}

	})
})

var _ = ginkgo.Describe("TestTagsService_IndexAllPages_StoresExcerpts", func() {
	ginkgo.It("preserves behavior", func() {
		t := ginkgo.GinkgoT()
	svc, ts := setupTagsService(t)

	pageID := createPageWithTags(t, ts, "Excerpt Page", "excerpt-page", []string{"go"})

	indexAllPages(t, svc, ts)

	exc, err := svc.GetExcerptsForPages(testPageIDs(pageID))
	if err != nil {
		t.Fatalf("GetExcerptsForPages: %v", err)
	}
	if exc[pageID] == "" {
		t.Errorf("expected non-empty excerpt after indexing")
	}

	})
})

// ─── helpers ─────────────────────────────────────────────────────────────────

func trimmed(s string) string {
	result := s
	for len(result) > 0 && (result[0] == ' ' || result[0] == '\t') {
		result = result[1:]
	}
	for len(result) > 0 && (result[len(result)-1] == ' ' || result[len(result)-1] == '\t') {
		result = result[:len(result)-1]
	}
	return result
}

func lower(s string) string {
	b := []byte(s)
	for i, c := range b {
		if c >= 'A' && c <= 'Z' {
			b[i] = c + 32
		}
	}
	return string(b)
}
