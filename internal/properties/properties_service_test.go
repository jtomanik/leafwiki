package properties

import (
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/perber/wiki/internal/core/tree"
)

// ─── ExtractPropertiesFromContent ────────────────────────────────────────────

var _ = ginkgo.It("TestExtractPropertiesFromContent_TextValue", func() {
	t := ginkgo.GinkgoT()
	content := "---\nstatus: draft\n---\n"
	got := ExtractPropertiesFromContent(content)
	assertEntry(t, got, "status", PropertyEntry{Value: "draft", Type: "text"})
})

var _ = ginkgo.It("TestExtractPropertiesFromContent_MultipleStringValues", func() {
	t := ginkgo.GinkgoT()
	content := "---\nstatus: draft\nauthor: alice\nenvironment: staging\n---\n"
	got := ExtractPropertiesFromContent(content)

	if len(got) != 3 {
		t.Fatalf("expected 3 properties, got %d: %v", len(got), got)
	}
	assertEntry(t, got, "status", PropertyEntry{Value: "draft", Type: "text"})
	assertEntry(t, got, "author", PropertyEntry{Value: "alice", Type: "text"})
	assertEntry(t, got, "environment", PropertyEntry{Value: "staging", Type: "text"})
})

var _ = ginkgo.It("TestExtractPropertiesFromContent_SkipsNumberValues", func() {
	t := ginkgo.GinkgoT()
	content := "---\nscore: 42\nrating: 4.5\nstatus: draft\n---\n"
	got := ExtractPropertiesFromContent(content)
	if _, ok := got["score"]; ok {
		t.Error("integer value must not be stored as a property")
	}
	if _, ok := got["rating"]; ok {
		t.Error("float value must not be stored as a property")
	}
	assertEntry(t, got, "status", PropertyEntry{Value: "draft", Type: "text"})
})

var _ = ginkgo.It("TestExtractPropertiesFromContent_SkipsBooleanValues", func() {
	t := ginkgo.GinkgoT()
	content := "---\nfeatured: true\narchived: false\nstatus: draft\n---\n"
	got := ExtractPropertiesFromContent(content)
	if _, ok := got["featured"]; ok {
		t.Error("boolean true must not be stored as a property")
	}
	if _, ok := got["archived"]; ok {
		t.Error("boolean false must not be stored as a property")
	}
	assertEntry(t, got, "status", PropertyEntry{Value: "draft", Type: "text"})
})

var _ = ginkgo.It("TestExtractPropertiesFromContent_SkipsEmptyStringValues", func() {
	t := ginkgo.GinkgoT()
	content := "---\nempty: \"\"\nstatus: draft\n---\n"
	got := ExtractPropertiesFromContent(content)
	if _, ok := got["empty"]; ok {
		t.Error("empty string value must not be stored as a property")
	}
	assertEntry(t, got, "status", PropertyEntry{Value: "draft", Type: "text"})
})

var _ = ginkgo.It("TestExtractPropertiesFromContent_SkipsWhitespaceOnlyValues", func() {
	t := ginkgo.GinkgoT()
	content := "---\nblank: \"   \"\nstatus: draft\n---\n"
	got := ExtractPropertiesFromContent(content)
	if _, ok := got["blank"]; ok {
		t.Error("whitespace-only string value must not be stored as a property")
	}
	assertEntry(t, got, "status", PropertyEntry{Value: "draft", Type: "text"})
})

var _ = ginkgo.It("TestExtractPropertiesFromContent_TrimsValueWhitespace", func() {
	t := ginkgo.GinkgoT()
	content := "---\nstatus: \"  draft  \"\n---\n"
	got := ExtractPropertiesFromContent(content)
	assertEntry(t, got, "status", PropertyEntry{Value: "draft", Type: "text"})
})

var _ = ginkgo.It("TestExtractPropertiesFromContent_SkipsMultilineStringValues", func() {
	t := ginkgo.GinkgoT()
	content := "---\ndescription: |\n  line one\n  line two\nstatus: draft\n---\n"
	got := ExtractPropertiesFromContent(content)
	if _, ok := got["description"]; ok {
		t.Error("multi-line string value must not be stored as a property")
	}
	assertEntry(t, got, "status", PropertyEntry{Value: "draft", Type: "text"})
})

// ─── Reserved keys must be skipped ───────────────────────────────────────────

var _ = ginkgo.It("TestExtractPropertiesFromContent_SkipsTagsKey", func() {
	t := ginkgo.GinkgoT()
	content := "---\ntags:\n  - react\nstatus: draft\n---\n"
	got := ExtractPropertiesFromContent(content)
	if _, ok := got["tags"]; ok {
		t.Error("'tags' must not be stored as a property")
	}
	assertEntry(t, got, "status", PropertyEntry{Value: "draft", Type: "text"})
})

var _ = ginkgo.It("TestExtractPropertiesFromContent_SkipsTagsKeyCaseInsensitive", func() {
	t := ginkgo.GinkgoT()
	content := "---\nTags:\n  - react\nstatus: draft\n---\n"
	got := ExtractPropertiesFromContent(content)
	if _, ok := got["Tags"]; ok {
		t.Error("'Tags' (mixed case) must not be stored as a property")
	}
})

var _ = ginkgo.It("TestExtractPropertiesFromContent_SkipsTitleKey", func() {
	t := ginkgo.GinkgoT()
	content := "---\ntitle: My Page\nstatus: draft\n---\n"
	got := ExtractPropertiesFromContent(content)
	if _, ok := got["title"]; ok {
		t.Error("'title' must not be stored as a property")
	}
})

var _ = ginkgo.It("TestExtractPropertiesFromContent_SkipsTitleKeyCaseInsensitive", func() {
	t := ginkgo.GinkgoT()
	content := "---\nTitle: My Page\nstatus: draft\n---\n"
	got := ExtractPropertiesFromContent(content)
	if _, ok := got["Title"]; ok {
		t.Error("'Title' (mixed case) must not be stored as a property")
	}
})

var _ = ginkgo.It("TestExtractPropertiesFromContent_SkipsLeafwikiPrefix", func() {
	t := ginkgo.GinkgoT()
	content := "---\nleafwiki_id: abc123\nleafwiki_created: 2024-01-01\nstatus: draft\n---\n"
	got := ExtractPropertiesFromContent(content)
	for key := range got {
		if len(key) >= 9 && key[:9] == "leafwiki_" {
			t.Errorf("key %q with leafwiki_ prefix must not be stored", key)
		}
	}
	assertEntry(t, got, "status", PropertyEntry{Value: "draft", Type: "text"})
})

var _ = ginkgo.It("TestExtractPropertiesFromContent_SkipsLeafwikiPrefixCaseInsensitive", func() {
	t := ginkgo.GinkgoT()
	content := "---\nLeafwiki_ID: abc\nstatus: ok\n---\n"
	got := ExtractPropertiesFromContent(content)
	if _, ok := got["Leafwiki_ID"]; ok {
		t.Error("'Leafwiki_ID' must not be stored (reserved prefix, any case)")
	}
})

// ─── Non-scalar values must be skipped ───────────────────────────────────────

var _ = ginkgo.It("TestExtractPropertiesFromContent_SkipsListValues", func() {
	t := ginkgo.GinkgoT()
	content := "---\nkeywords: [go, testing]\nstatus: draft\n---\n"
	got := ExtractPropertiesFromContent(content)
	if _, ok := got["keywords"]; ok {
		t.Error("list values must not be stored as properties")
	}
	assertEntry(t, got, "status", PropertyEntry{Value: "draft", Type: "text"})
})

var _ = ginkgo.It("TestExtractPropertiesFromContent_SkipsBlockListValues", func() {
	t := ginkgo.GinkgoT()
	content := "---\nkeywords:\n  - go\n  - testing\nstatus: draft\n---\n"
	got := ExtractPropertiesFromContent(content)
	if _, ok := got["keywords"]; ok {
		t.Error("block list values must not be stored as properties")
	}
})

var _ = ginkgo.It("TestExtractPropertiesFromContent_SkipsNilValues", func() {
	t := ginkgo.GinkgoT()
	content := "---\nempty:\nstatus: draft\n---\n"
	got := ExtractPropertiesFromContent(content)
	if _, ok := got["empty"]; ok {
		t.Error("nil/empty values must not be stored as properties")
	}
})

// ─── Edge cases ───────────────────────────────────────────────────────────────

var _ = ginkgo.It("TestExtractPropertiesFromContent_NoFrontmatterReturnsNil", func() {
	t := ginkgo.GinkgoT()
	got := ExtractPropertiesFromContent("# Page\n\nNo frontmatter.")
	if got != nil {
		t.Errorf("expected nil for content without frontmatter, got %v", got)
	}
})

var _ = ginkgo.It("TestExtractPropertiesFromContent_EmptyContentReturnsNil", func() {
	t := ginkgo.GinkgoT()
	got := ExtractPropertiesFromContent("")
	if got != nil {
		t.Errorf("expected nil for empty content, got %v", got)
	}
})

var _ = ginkgo.It("TestExtractPropertiesFromContent_OnlyReservedKeysReturnsNil", func() {
	t := ginkgo.GinkgoT()
	content := "---\ntags:\n  - go\ntitle: My Page\nleafwiki_id: abc\n---\n"
	got := ExtractPropertiesFromContent(content)
	if got != nil {
		t.Errorf("expected nil when all keys are reserved, got %v", got)
	}
})

var _ = ginkgo.It("TestExtractPropertiesFromContent_OnlyNonStringValuesReturnsNil", func() {
	t := ginkgo.GinkgoT()
	content := "---\nscore: 42\nfeatured: true\nrating: 4.5\n---\n"
	got := ExtractPropertiesFromContent(content)
	if got != nil {
		t.Errorf("expected nil when all values are non-string, got %v", got)
	}
})

// ─── PropertiesService integration (with real tree + store) ──────────────────

func setupPropertiesService(t propertiesTestT) (*PropertiesService, *tree.TreeService) {
	t.Helper()

	dir := t.TempDir()
	ts := tree.NewTreeService(dir)
	if err := ts.LoadTree(); err != nil {
		t.Fatalf("LoadTree: %v", err)
	}

	store, err := NewPropertiesStore(dir)
	if err != nil {
		t.Fatalf("NewPropertiesStore: %v", err)
	}
	closeStoreForTest(t, store)

	return NewPropertiesService(store), ts
}

func indexAllPages(t propertiesTestT, svc *PropertiesService, ts *tree.TreeService) {
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

func pageKind() *tree.NodeKind {
	k := tree.NodeKindPage
	return &k
}

func createPageWithContent(t propertiesTestT, ts *tree.TreeService, title, slug, content string) tree.PageID {
	t.Helper()
	idPtr, err := ts.CreateNode("system", nil, title, newFixtureSlug(slug), pageKind())
	if err != nil {
		t.Fatalf("CreateNode %q: %v", slug, err)
	}
	if err := ts.UpdateNodeUncheckedVersion(newFixtureUserID("system"), *idPtr, title, newFixtureSlug(slug), &content, true); err != nil {
		t.Fatalf("UpdateNode %q: %v", slug, err)
	}
	return *idPtr
}

var _ = ginkgo.It("TestPropertiesService_IndexAllPages_BuildsIndex", func() {
	t := ginkgo.GinkgoT()
	svc, ts := setupPropertiesService(t)

	id1 := createPageWithContent(t, ts, "Page A", "page-a", "---\nstatus: draft\n---\n# A")
	id2 := createPageWithContent(t, ts, "Page B", "page-b", "---\nstatus: published\n---\n# B")

	indexAllPages(t, svc, ts)

	ids, err := svc.GetPageIDsByProperty("status", "draft")
	if err != nil {
		t.Fatalf("GetPageIDsByProperty: %v", err)
	}
	if len(ids) != 1 || ids[0] != id1 {
		t.Errorf("expected [%s], got %v", id1, ids)
	}

	ids2, err := svc.GetPageIDsByProperty("status", "published")
	if err != nil {
		t.Fatalf("GetPageIDsByProperty: %v", err)
	}
	if len(ids2) != 1 || ids2[0] != id2 {
		t.Errorf("expected [%s], got %v", id2, ids2)
	}
})

var _ = ginkgo.It("TestPropertiesService_IndexAllPages_IsIdempotent", func() {
	t := ginkgo.GinkgoT()
	svc, ts := setupPropertiesService(t)
	createPageWithContent(t, ts, "Page A", "page-a", "---\nstatus: draft\n---\n# A")

	for i := 0; i < 3; i++ {
		if err := svc.ClearIndex(); err != nil {
			t.Fatalf("ClearIndex (run %d): %v", i, err)
		}
		indexAllPages(t, svc, ts)
	}

	keys, err := svc.GetAllPropertyKeys("", 50)
	if err != nil {
		t.Fatalf("GetAllPropertyKeys: %v", err)
	}
	if len(keys) != 1 || keys[0].Key != "status" || keys[0].Count != 1 {
		t.Errorf("expected [{status 1}], got %v", keys)
	}
})

var _ = ginkgo.It("TestPropertiesService_IndexAllPages_SkipsReservedKeys", func() {
	t := ginkgo.GinkgoT()
	svc, ts := setupPropertiesService(t)
	createPageWithContent(t, ts, "Page A", "page-a",
		"---\ntags:\n  - go\ntitle: Custom\nleafwiki_id: abc\nstatus: draft\n---\n# A")

	indexAllPages(t, svc, ts)

	keys, err := svc.GetAllPropertyKeys("", 50)
	if err != nil {
		t.Fatalf("GetAllPropertyKeys: %v", err)
	}

	for _, kc := range keys {
		switch kc.Key {
		case "tags", "title":
			t.Errorf("reserved key %q must not be indexed", kc.Key)
		default:
			if len(kc.Key) >= 9 && kc.Key[:9] == "leafwiki_" {
				t.Errorf("reserved prefix key %q must not be indexed", kc.Key)
			}
		}
	}
	if len(keys) != 1 || keys[0].Key != "status" {
		t.Errorf("expected only [status], got %v", keys)
	}
})

var _ = ginkgo.It("TestPropertiesService_IndexAllPages_SkipsListValues", func() {
	t := ginkgo.GinkgoT()
	svc, ts := setupPropertiesService(t)
	createPageWithContent(t, ts, "Page A", "page-a",
		"---\nkeywords: [go, testing]\nstatus: draft\n---\n# A")

	indexAllPages(t, svc, ts)

	keys, err := svc.GetAllPropertyKeys("", 50)
	if err != nil {
		t.Fatalf("GetAllPropertyKeys: %v", err)
	}
	for _, kc := range keys {
		if kc.Key == "keywords" {
			t.Errorf("list-valued property 'keywords' must not be indexed")
		}
	}
})

var _ = ginkgo.It("TestPropertiesService_IndexAllPages_PagesWithoutPropertiesAreSkipped", func() {
	t := ginkgo.GinkgoT()
	svc, ts := setupPropertiesService(t)
	createPageWithContent(t, ts, "No Props", "no-props", "# Just content")

	indexAllPages(t, svc, ts)

	keys, err := svc.GetAllPropertyKeys("", 50)
	if err != nil {
		t.Fatalf("GetAllPropertyKeys: %v", err)
	}
	if len(keys) != 0 {
		t.Errorf("expected no keys for page without properties, got %v", keys)
	}
})

var _ = ginkgo.It("TestPropertiesService_IndexAllPages_ReadsPropertiesFromRawFrontmatter", func() {
	t := ginkgo.GinkgoT()
	svc, ts := setupPropertiesService(t)
	pageID := createPageWithContent(t, ts, "Page A", "page-a", "---\nstatus: draft\n---\n# A")

	page, err := ts.GetPage(pageID)
	if err != nil {
		t.Fatalf("GetPage: %v", err)
	}
	if got := ExtractPropertiesFromContent(page.Content); got != nil {
		t.Fatalf("expected parsed page content to exclude frontmatter properties, got %v", got)
	}

	indexAllPages(t, svc, ts)

	ids, err := svc.GetPageIDsByProperty("status", "draft")
	if err != nil {
		t.Fatalf("GetPageIDsByProperty: %v", err)
	}
	if len(ids) != 1 || ids[0] != pageID {
		t.Fatalf("expected [%s], got %v", pageID, ids)
	}
})

// ─── IndexPageContent ─────────────────────────────────────────────────────────

var _ = ginkgo.It("TestPropertiesService_IndexPageContent_StoresProperties", func() {
	t := ginkgo.GinkgoT()
	svc, _ := setupPropertiesService(t)

	raw := "---\nstatus: draft\nauthor: alice\n---\n\n# Page"
	if err := svc.IndexPageContent("page-1", raw); err != nil {
		t.Fatalf("IndexPageContent: %v", err)
	}

	keys, err := svc.GetAllPropertyKeys("", 50)
	if err != nil {
		t.Fatalf("GetAllPropertyKeys: %v", err)
	}
	if len(keys) != 2 {
		t.Fatalf("expected 2 keys, got %v", keys)
	}

	ids, err := svc.GetPageIDsByProperty("status", "draft")
	if err != nil {
		t.Fatalf("GetPageIDsByProperty: %v", err)
	}
	if len(ids) != 1 || ids[0] != "page-1" {
		t.Errorf("expected [page-1], got %v", ids)
	}
})

var _ = ginkgo.It("TestPropertiesService_IndexPageContent_NoFrontmatterStoresNothing", func() {
	t := ginkgo.GinkgoT()
	svc, _ := setupPropertiesService(t)

	if err := svc.IndexPageContent("page-1", "# Just content"); err != nil {
		t.Fatalf("IndexPageContent: %v", err)
	}

	keys, err := svc.GetAllPropertyKeys("", 50)
	if err != nil {
		t.Fatalf("GetAllPropertyKeys: %v", err)
	}
	if len(keys) != 0 {
		t.Errorf("expected no keys, got %v", keys)
	}
})

var _ = ginkgo.It("TestPropertiesService_IndexPageContent_UpdatesExistingEntry", func() {
	t := ginkgo.GinkgoT()
	svc, _ := setupPropertiesService(t)

	if err := svc.IndexPageContent("page-1", "---\nstatus: draft\n---\n"); err != nil {
		t.Fatalf("IndexPageContent (first): %v", err)
	}
	if err := svc.IndexPageContent("page-1", "---\nstatus: published\n---\n"); err != nil {
		t.Fatalf("IndexPageContent (second): %v", err)
	}

	ids, err := svc.GetPageIDsByProperty("status", "published")
	if err != nil {
		t.Fatalf("GetPageIDsByProperty: %v", err)
	}
	if len(ids) != 1 || ids[0] != "page-1" {
		t.Errorf("expected [page-1] for published, got %v", ids)
	}

	old, err := svc.GetPageIDsByProperty("status", "draft")
	if err != nil {
		t.Fatalf("GetPageIDsByProperty: %v", err)
	}
	if len(old) != 0 {
		t.Errorf("old value 'draft' should be gone, got %v", old)
	}
})

var _ = ginkgo.Describe("PropertiesService store delegation", func() {
	ginkgo.It("sets and returns properties for selected pages", func() {
		t := ginkgo.GinkgoT()
		store := newTestStore(t)
		svc := NewPropertiesService(store)

		Expect(svc.SetPropertiesForPage(newFixturePageID("page-1"), props("status", "draft", "owner", "alice"))).To(Succeed())
		Expect(svc.SetPropertiesForPage(newFixturePageID("page-2"), props("status", "published"))).To(Succeed())

		got, err := svc.GetPropertiesForPages(testPageIDs("page-1", "page-2"))
		Expect(err).NotTo(HaveOccurred())
		Expect(got).To(Equal(map[tree.PageID]map[string]PropertyEntry{
			newFixturePageID("page-1"): props("owner", "alice", "status", "draft"),
			newFixturePageID("page-2"): props("status", "published"),
		}))
	})

	ginkgo.It("deletes one page through the service without removing other page properties", func() {
		t := ginkgo.GinkgoT()
		store := newTestStore(t)
		svc := NewPropertiesService(store)

		Expect(svc.SetPropertiesForPage(newFixturePageID("page-1"), props("status", "draft"))).To(Succeed())
		Expect(svc.SetPropertiesForPage(newFixturePageID("page-2"), props("status", "published"))).To(Succeed())

		Expect(svc.DeletePropertiesForPage(newFixturePageID("page-1"))).To(Succeed())

		got, err := svc.GetPropertiesForPages(testPageIDs("page-1", "page-2"))
		Expect(err).NotTo(HaveOccurred())
		Expect(got[newFixturePageID("page-1")]).To(BeNil())
		Expect(got[newFixturePageID("page-2")]).To(Equal(props("status", "published")))
	})

	ginkgo.It("returns an empty properties map for an empty service selection", func() {
		t := ginkgo.GinkgoT()
		store := newTestStore(t)
		svc := NewPropertiesService(store)

		got, err := svc.GetPropertiesForPages(nil)
		Expect(err).NotTo(HaveOccurred())
		Expect(got).To(Equal(map[tree.PageID]map[string]PropertyEntry{}))
	})
})

// ─── helpers ─────────────────────────────────────────────────────────────────

func assertEntry(t propertiesTestT, got map[string]PropertyEntry, key string, want PropertyEntry) {
	t.Helper()
	entry, ok := got[key]
	if !ok {
		t.Errorf("key %q missing from result %v", key, got)
		return
	}
	if entry != want {
		t.Errorf("key %q: got %+v, want %+v", key, entry, want)
	}
}
