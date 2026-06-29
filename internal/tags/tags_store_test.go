package tags

import (
	ginkgo "github.com/onsi/ginkgo/v2"

	"os"
	"path/filepath"
	"sort"

	"github.com/perber/wiki/internal/core/tree"
)

func newTestStore(t tagsTestT) *TagsStore {
	t.Helper()
	store, err := NewTagsStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewTagsStore: %v", err)
	}
	closeTagsStoreForTest(store, t)
	return store
}

func testPageIDs[T ~string](ids ...T) []tree.PageID {
	pageIDs := make([]tree.PageID, 0, len(ids))
	for _, id := range ids {
		pageIDs = append(pageIDs, newFixturePageID(id))
	}
	return pageIDs
}

func sortTestPageIDs(ids []tree.PageID) {
	sort.Slice(ids, func(i, j int) bool {
		return ids[i] < ids[j]
	})
}

func assertPageIDSliceEqual(t tagsTestT, got []tree.PageID, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("expected %v, got %v", want, got)
	}
	for i, w := range want {
		if got[i] != newFixturePageID(w) {
			t.Errorf("[%d] = %q, want %q", i, got[i], w)
		}
	}
}

// ─── DB lifecycle ────────────────────────────────────────────────────────────

var _ = ginkgo.Describe("TestTagsStore_CreatesDatabaseInStorageDir", func() {
	ginkgo.It("preserves behavior", func() {
		t := ginkgo.GinkgoT()
		tmp := t.TempDir()
		store, err := NewTagsStore(tmp)
		if err != nil {
			t.Fatalf("NewTagsStore: %v", err)
		}
		closeTagsStoreForTest(store, t)

		if _, err := os.Stat(filepath.Join(tmp, "tags.db")); err != nil {
			t.Fatalf("expected tags.db to exist: %v", err)
		}

	})
})

var _ = ginkgo.Describe("TestTagsStore_IdempotentSchema", func() {
	ginkgo.It("preserves behavior", func() {
		t := ginkgo.GinkgoT()
		tmp := t.TempDir()
		for i := 0; i < 3; i++ {
			store, err := NewTagsStore(tmp)
			if err != nil {
				t.Fatalf("NewTagsStore (run %d): %v", i, err)
			}
			if err := store.Close(); err != nil {
				t.Fatalf("Close (run %d): %v", i, err)
			}
		}

	})
})

// ─── SetTagsForPage ──────────────────────────────────────────────────────────

var _ = ginkgo.Describe("TestTagsStore_SetTagsForPage_StoresTags", func() {
	ginkgo.It("preserves behavior", func() {
		t := ginkgo.GinkgoT()
		store := newTestStore(t)

		if err := store.SetTagsForPage("page-1", []string{"go", "testing"}); err != nil {
			t.Fatalf("SetTagsForPage: %v", err)
		}

		got, err := store.GetTagsForPages(testPageIDs("page-1"))
		if err != nil {
			t.Fatalf("GetTagsForPages: %v", err)
		}

		want := []string{"go", "testing"}
		assertStringSliceEqual(t, got["page-1"], want)

	})
})

var _ = ginkgo.Describe("TestTagsStore_SetTagsForPage_ReplacesOnSecondCall", func() {
	ginkgo.It("preserves behavior", func() {
		t := ginkgo.GinkgoT()
		store := newTestStore(t)

		if err := store.SetTagsForPage("page-1", []string{"go", "testing"}); err != nil {
			t.Fatalf("first SetTagsForPage: %v", err)
		}
		if err := store.SetTagsForPage("page-1", []string{"typescript"}); err != nil {
			t.Fatalf("second SetTagsForPage: %v", err)
		}

		got, err := store.GetTagsForPages(testPageIDs("page-1"))
		if err != nil {
			t.Fatalf("GetTagsForPages: %v", err)
		}
		assertStringSliceEqual(t, got["page-1"], []string{"typescript"})

	})
})

var _ = ginkgo.Describe("TestTagsStore_SetTagsForPage_EmptyTagsClearsExisting", func() {
	ginkgo.It("preserves behavior", func() {
		t := ginkgo.GinkgoT()
		store := newTestStore(t)

		if err := store.SetTagsForPage("page-1", []string{"go", "testing"}); err != nil {
			t.Fatalf("SetTagsForPage: %v", err)
		}
		if err := store.SetTagsForPage("page-1", []string{}); err != nil {
			t.Fatalf("SetTagsForPage (clear): %v", err)
		}

		got, err := store.GetTagsForPages(testPageIDs("page-1"))
		if err != nil {
			t.Fatalf("GetTagsForPages: %v", err)
		}
		if len(got["page-1"]) != 0 {
			t.Fatalf("expected empty tags, got %v", got["page-1"])
		}

	})
})

var _ = ginkgo.Describe("TestTagsStore_SetTagsForPage_NilTagsClearsExisting", func() {
	ginkgo.It("preserves behavior", func() {
		t := ginkgo.GinkgoT()
		store := newTestStore(t)

		if err := store.SetTagsForPage("page-1", []string{"go"}); err != nil {
			t.Fatalf("SetTagsForPage: %v", err)
		}
		if err := store.SetTagsForPage("page-1", nil); err != nil {
			t.Fatalf("SetTagsForPage (nil): %v", err)
		}

		got, err := store.GetTagsForPages(testPageIDs("page-1"))
		if err != nil {
			t.Fatalf("GetTagsForPages: %v", err)
		}
		if len(got["page-1"]) != 0 {
			t.Fatalf("expected empty tags after nil set, got %v", got["page-1"])
		}

	})
})

// ─── DeleteTagsForPage ───────────────────────────────────────────────────────

var _ = ginkgo.Describe("TestTagsStore_DeleteTagsForPage_RemovesTags", func() {
	ginkgo.It("preserves behavior", func() {
		t := ginkgo.GinkgoT()
		store := newTestStore(t)

		if err := store.SetTagsForPage("page-1", []string{"go", "testing"}); err != nil {
			t.Fatalf("SetTagsForPage: %v", err)
		}
		if err := store.DeleteTagsForPage("page-1"); err != nil {
			t.Fatalf("DeleteTagsForPage: %v", err)
		}

		got, err := store.GetTagsForPages(testPageIDs("page-1"))
		if err != nil {
			t.Fatalf("GetTagsForPages: %v", err)
		}
		if len(got["page-1"]) != 0 {
			t.Fatalf("expected empty tags after delete, got %v", got["page-1"])
		}

	})
})

var _ = ginkgo.Describe("TestTagsStore_DeleteTagsForPage_NonExistentPageIsNoop", func() {
	ginkgo.It("preserves behavior", func() {
		t := ginkgo.GinkgoT()
		store := newTestStore(t)
		if err := store.DeleteTagsForPage("does-not-exist"); err != nil {
			t.Fatalf("DeleteTagsForPage on unknown page: %v", err)
		}

	})
})

var _ = ginkgo.Describe("TestTagsStore_DeleteTagsForPage_DoesNotAffectOtherPages", func() {
	ginkgo.It("preserves behavior", func() {
		t := ginkgo.GinkgoT()
		store := newTestStore(t)

		if err := store.SetTagsForPage("page-1", []string{"go"}); err != nil {
			t.Fatalf("SetTagsForPage page-1: %v", err)
		}
		if err := store.SetTagsForPage("page-2", []string{"typescript"}); err != nil {
			t.Fatalf("SetTagsForPage page-2: %v", err)
		}

		if err := store.DeleteTagsForPage("page-1"); err != nil {
			t.Fatalf("DeleteTagsForPage: %v", err)
		}

		got, err := store.GetTagsForPages(testPageIDs("page-2"))
		if err != nil {
			t.Fatalf("GetTagsForPages: %v", err)
		}
		assertStringSliceEqual(t, got["page-2"], []string{"typescript"})

	})
})

// ─── GetAllTags ──────────────────────────────────────────────────────────────

var _ = ginkgo.Describe("TestTagsStore_GetAllTags_EmptyDB", func() {
	ginkgo.It("preserves behavior", func() {
		t := ginkgo.GinkgoT()
		store := newTestStore(t)
		tags, err := store.GetAllTags("", 50)
		if err != nil {
			t.Fatalf("GetAllTags: %v", err)
		}
		if len(tags) != 0 {
			t.Fatalf("expected empty result, got %v", tags)
		}

	})
})

var _ = ginkgo.Describe("TestTagsStore_GetAllTags_ReturnsTagsWithCount", func() {
	ginkgo.It("preserves behavior", func() {
		t := ginkgo.GinkgoT()
		store := newTestStore(t)

		_ = store.SetTagsForPage("page-1", []string{"go", "testing"})
		_ = store.SetTagsForPage("page-2", []string{"go", "typescript"})
		_ = store.SetTagsForPage("page-3", []string{"typescript"})

		tags, err := store.GetAllTags("", 50)
		if err != nil {
			t.Fatalf("GetAllTags: %v", err)
		}

		byTag := make(map[string]int, len(tags))
		for _, tc := range tags {
			byTag[tc.Tag] = tc.Count
		}

		if byTag["go"] != 2 {
			t.Errorf("go count = %d, want 2", byTag["go"])
		}
		if byTag["typescript"] != 2 {
			t.Errorf("typescript count = %d, want 2", byTag["typescript"])
		}
		if byTag["testing"] != 1 {
			t.Errorf("testing count = %d, want 1", byTag["testing"])
		}

	})
})

var _ = ginkgo.Describe("TestTagsStore_GetAllTags_OrderByCountDescThenTagAsc", func() {
	ginkgo.It("preserves behavior", func() {
		t := ginkgo.GinkgoT()
		store := newTestStore(t)

		_ = store.SetTagsForPage("page-1", []string{"alpha", "beta", "gamma"})
		_ = store.SetTagsForPage("page-2", []string{"alpha", "beta"})
		_ = store.SetTagsForPage("page-3", []string{"alpha"})

		tags, err := store.GetAllTags("", 50)
		if err != nil {
			t.Fatalf("GetAllTags: %v", err)
		}

		if len(tags) != 3 {
			t.Fatalf("expected 3 tags, got %d", len(tags))
		}
		// alpha: 3, beta: 2, gamma: 1
		if tags[0].Tag != "alpha" || tags[0].Count != 3 {
			t.Errorf("tags[0] = %+v, want {alpha 3}", tags[0])
		}
		if tags[1].Tag != "beta" || tags[1].Count != 2 {
			t.Errorf("tags[1] = %+v, want {beta 2}", tags[1])
		}
		if tags[2].Tag != "gamma" || tags[2].Count != 1 {
			t.Errorf("tags[2] = %+v, want {gamma 1}", tags[2])
		}

	})
})

var _ = ginkgo.Describe("TestTagsStore_GetAllTags_OrderAlphaForSameCount", func() {
	ginkgo.It("preserves behavior", func() {
		t := ginkgo.GinkgoT()
		store := newTestStore(t)

		_ = store.SetTagsForPage("page-1", []string{"zebra", "apple"})

		tags, err := store.GetAllTags("", 50)
		if err != nil {
			t.Fatalf("GetAllTags: %v", err)
		}
		if len(tags) < 2 {
			t.Fatalf("expected 2 tags, got %d", len(tags))
		}
		if tags[0].Tag != "apple" {
			t.Errorf("expected apple first (same count, alphabetic), got %q", tags[0].Tag)
		}

	})
})

var _ = ginkgo.Describe("TestTagsStore_GetAllTags_FilterByPrefix", func() {
	ginkgo.It("preserves behavior", func() {
		t := ginkgo.GinkgoT()
		store := newTestStore(t)

		_ = store.SetTagsForPage("page-1", []string{"react", "redux", "rails", "node"})

		tags, err := store.GetAllTags("re", 50)
		if err != nil {
			t.Fatalf("GetAllTags: %v", err)
		}

		for _, tc := range tags {
			if len(tc.Tag) < 2 || tc.Tag[:2] != "re" {
				t.Errorf("tag %q does not start with 're'", tc.Tag)
			}
		}
		if len(tags) != 2 {
			t.Errorf("expected 2 tags matching 're', got %d: %v", len(tags), tags)
		}

	})
})

var _ = ginkgo.Describe("TestTagsStore_GetAllTags_EmptyFilterReturnsAll", func() {
	ginkgo.It("preserves behavior", func() {
		t := ginkgo.GinkgoT()
		store := newTestStore(t)

		_ = store.SetTagsForPage("page-1", []string{"alpha", "beta", "gamma"})

		tags, err := store.GetAllTags("", 50)
		if err != nil {
			t.Fatalf("GetAllTags: %v", err)
		}
		if len(tags) != 3 {
			t.Errorf("expected 3 tags, got %d", len(tags))
		}

	})
})

var _ = ginkgo.Describe("TestTagsStore_GetAllTags_RespectsLimit", func() {
	ginkgo.It("preserves behavior", func() {
		t := ginkgo.GinkgoT()
		store := newTestStore(t)

		_ = store.SetTagsForPage("page-1", []string{"a", "b", "c", "d", "e"})

		tags, err := store.GetAllTags("", 3)
		if err != nil {
			t.Fatalf("GetAllTags: %v", err)
		}
		if len(tags) != 3 {
			t.Errorf("expected 3 tags (limit), got %d", len(tags))
		}

	})
})

var _ = ginkgo.Describe("TestTagsStore_GetAllTags_ZeroLimitReturnsAll", func() {
	ginkgo.It("preserves behavior", func() {
		t := ginkgo.GinkgoT()
		store := newTestStore(t)

		_ = store.SetTagsForPage("page-1", []string{"a", "b", "c", "d", "e"})

		tags, err := store.GetAllTags("", 0)
		if err != nil {
			t.Fatalf("GetAllTags: %v", err)
		}
		if len(tags) != 5 {
			t.Errorf("expected all 5 tags, got %d", len(tags))
		}

	})
})

// ─── GetPageIDsByTags ────────────────────────────────────────────────────────

var _ = ginkgo.Describe("TestTagsStore_GetPageIDsByTags_ANDLogic", func() {
	ginkgo.It("preserves behavior", func() {
		t := ginkgo.GinkgoT()
		store := newTestStore(t)

		_ = store.SetTagsForPage("page-1", []string{"react", "typescript"})
		_ = store.SetTagsForPage("page-2", []string{"react"})
		_ = store.SetTagsForPage("page-3", []string{"typescript"})
		_ = store.SetTagsForPage("page-4", []string{"vue", "typescript"})

		// Only page-1 has BOTH react AND typescript
		ids, err := store.GetPageIDsByTags([]string{"react", "typescript"})
		if err != nil {
			t.Fatalf("GetPageIDsByTags: %v", err)
		}
		if len(ids) != 1 || ids[0] != "page-1" {
			t.Errorf("expected [page-1], got %v", ids)
		}

	})
})

var _ = ginkgo.Describe("TestTagsStore_GetPageIDsByTags_SingleTag", func() {
	ginkgo.It("preserves behavior", func() {
		t := ginkgo.GinkgoT()
		store := newTestStore(t)

		_ = store.SetTagsForPage("page-1", []string{"react", "typescript"})
		_ = store.SetTagsForPage("page-2", []string{"react"})
		_ = store.SetTagsForPage("page-3", []string{"vue"})

		ids, err := store.GetPageIDsByTags([]string{"react"})
		if err != nil {
			t.Fatalf("GetPageIDsByTags: %v", err)
		}

		sortTestPageIDs(ids)
		want := []string{"page-1", "page-2"}
		assertPageIDSliceEqual(t, ids, want)

	})
})

var _ = ginkgo.Describe("TestTagsStore_GetPageIDsByTags_NoMatch", func() {
	ginkgo.It("preserves behavior", func() {
		t := ginkgo.GinkgoT()
		store := newTestStore(t)

		_ = store.SetTagsForPage("page-1", []string{"react"})

		ids, err := store.GetPageIDsByTags([]string{"vue"})
		if err != nil {
			t.Fatalf("GetPageIDsByTags: %v", err)
		}
		if len(ids) != 0 {
			t.Errorf("expected no matches, got %v", ids)
		}

	})
})

var _ = ginkgo.Describe("TestTagsStore_GetPageIDsByTags_EmptyInputReturnsNil", func() {
	ginkgo.It("preserves behavior", func() {
		t := ginkgo.GinkgoT()
		store := newTestStore(t)
		_ = store.SetTagsForPage("page-1", []string{"react"})

		ids, err := store.GetPageIDsByTags([]string{})
		if err != nil {
			t.Fatalf("GetPageIDsByTags: %v", err)
		}
		if ids != nil {
			t.Errorf("expected nil, got %v", ids)
		}

	})
})

var _ = ginkgo.Describe("TestTagsStore_GetPageIDsByTags_ThreeTagAND", func() {
	ginkgo.It("preserves behavior", func() {
		t := ginkgo.GinkgoT()
		store := newTestStore(t)

		_ = store.SetTagsForPage("page-1", []string{"a", "b", "c"})
		_ = store.SetTagsForPage("page-2", []string{"a", "b"})
		_ = store.SetTagsForPage("page-3", []string{"a"})

		ids, err := store.GetPageIDsByTags([]string{"a", "b", "c"})
		if err != nil {
			t.Fatalf("GetPageIDsByTags: %v", err)
		}
		if len(ids) != 1 || ids[0] != "page-1" {
			t.Errorf("expected [page-1], got %v", ids)
		}

	})
})

// ─── GetTagsForPages ─────────────────────────────────────────────────────────

var _ = ginkgo.Describe("TestTagsStore_GetTagsForPages_ReturnsTagsForMultiplePages", func() {
	ginkgo.It("preserves behavior", func() {
		t := ginkgo.GinkgoT()
		store := newTestStore(t)

		_ = store.SetTagsForPage("page-1", []string{"go", "testing"})
		_ = store.SetTagsForPage("page-2", []string{"typescript"})
		_ = store.SetTagsForPage("page-3", []string{"react", "vue"})

		got, err := store.GetTagsForPages(testPageIDs("page-1", "page-3"))
		if err != nil {
			t.Fatalf("GetTagsForPages: %v", err)
		}

		assertStringSliceEqual(t, got["page-1"], []string{"go", "testing"})
		assertStringSliceEqual(t, got["page-3"], []string{"react", "vue"})
		if _, ok := got["page-2"]; ok {
			t.Errorf("page-2 should not be in result")
		}

	})
})

var _ = ginkgo.Describe("TestTagsStore_GetTagsForPages_EmptyInputReturnsEmptyMap", func() {
	ginkgo.It("preserves behavior", func() {
		t := ginkgo.GinkgoT()
		store := newTestStore(t)

		got, err := store.GetTagsForPages(testPageIDs[string]())
		if err != nil {
			t.Fatalf("GetTagsForPages: %v", err)
		}
		if got == nil {
			t.Fatalf("expected empty map, got nil")
		}
		if len(got) != 0 {
			t.Errorf("expected empty map, got %v", got)
		}

	})
})

var _ = ginkgo.Describe("TestTagsStore_GetTagsForPages_UnknownIDReturnsNoEntry", func() {
	ginkgo.It("preserves behavior", func() {
		t := ginkgo.GinkgoT()
		store := newTestStore(t)

		got, err := store.GetTagsForPages(testPageIDs("does-not-exist"))
		if err != nil {
			t.Fatalf("GetTagsForPages: %v", err)
		}
		if len(got) != 0 {
			t.Errorf("expected empty map for unknown IDs, got %v", got)
		}

	})
})

// ─── Clear ───────────────────────────────────────────────────────────────────

var _ = ginkgo.Describe("TestTagsStore_Clear_RemovesAllEntries", func() {
	ginkgo.It("preserves behavior", func() {
		t := ginkgo.GinkgoT()
		store := newTestStore(t)

		_ = store.SetTagsForPage("page-1", []string{"go"})
		_ = store.SetTagsForPage("page-2", []string{"typescript"})

		if err := store.Clear(); err != nil {
			t.Fatalf("Clear: %v", err)
		}

		tags, err := store.GetAllTags("", 50)
		if err != nil {
			t.Fatalf("GetAllTags after Clear: %v", err)
		}
		if len(tags) != 0 {
			t.Errorf("expected empty after Clear, got %v", tags)
		}

	})
})

// ─── SetPageIndex ─────────────────────────────────────────────────────────────

var _ = ginkgo.Describe("TestTagsStore_SetPageIndex_StoresTagsAndExcerpt", func() {
	ginkgo.It("preserves behavior", func() {
		t := ginkgo.GinkgoT()
		store := newTestStore(t)

		if err := store.SetPageIndex("page-1", []string{"go", "testing"}, "some excerpt"); err != nil {
			t.Fatalf("SetPageIndex: %v", err)
		}

		gotTags, err := store.GetTagsForPages(testPageIDs("page-1"))
		if err != nil {
			t.Fatalf("GetTagsForPages: %v", err)
		}
		assertStringSliceEqual(t, gotTags["page-1"], []string{"go", "testing"})

		gotExcerpts, err := store.GetExcerptsForPages(testPageIDs("page-1"))
		if err != nil {
			t.Fatalf("GetExcerptsForPages: %v", err)
		}
		if gotExcerpts["page-1"] != "some excerpt" {
			t.Errorf("excerpt = %q, want %q", gotExcerpts["page-1"], "some excerpt")
		}

	})
})

var _ = ginkgo.Describe("TestTagsStore_SetPageIndex_UpdatesExcerptOnSecondCall", func() {
	ginkgo.It("preserves behavior", func() {
		t := ginkgo.GinkgoT()
		store := newTestStore(t)

		_ = store.SetPageIndex("page-1", []string{"go"}, "first excerpt")
		_ = store.SetPageIndex("page-1", []string{"go"}, "updated excerpt")

		got, err := store.GetExcerptsForPages(testPageIDs("page-1"))
		if err != nil {
			t.Fatalf("GetExcerptsForPages: %v", err)
		}
		if got["page-1"] != "updated excerpt" {
			t.Errorf("excerpt = %q, want %q", got["page-1"], "updated excerpt")
		}

	})
})

var _ = ginkgo.Describe("TestTagsStore_SetPageIndex_EmptyTagsClearsTagsButKeepsExcerpt", func() {
	ginkgo.It("preserves behavior", func() {
		t := ginkgo.GinkgoT()
		store := newTestStore(t)

		_ = store.SetPageIndex("page-1", []string{"go"}, "excerpt here")
		_ = store.SetPageIndex("page-1", []string{}, "excerpt here")

		tags, err := store.GetTagsForPages(testPageIDs("page-1"))
		if err != nil {
			t.Fatalf("GetTagsForPages: %v", err)
		}
		if len(tags["page-1"]) != 0 {
			t.Errorf("expected no tags, got %v", tags["page-1"])
		}

		exc, err := store.GetExcerptsForPages(testPageIDs("page-1"))
		if err != nil {
			t.Fatalf("GetExcerptsForPages: %v", err)
		}
		if exc["page-1"] != "excerpt here" {
			t.Errorf("excerpt = %q, want 'excerpt here'", exc["page-1"])
		}

	})
})

// ─── DeletePageIndex ─────────────────────────────────────────────────────────

var _ = ginkgo.Describe("TestTagsStore_DeletePageIndex_RemovesTagsAndExcerpt", func() {
	ginkgo.It("preserves behavior", func() {
		t := ginkgo.GinkgoT()
		store := newTestStore(t)

		_ = store.SetPageIndex("page-1", []string{"go"}, "some excerpt")
		if err := store.DeletePageIndex("page-1"); err != nil {
			t.Fatalf("DeletePageIndex: %v", err)
		}

		tags, err := store.GetTagsForPages(testPageIDs("page-1"))
		if err != nil {
			t.Fatalf("GetTagsForPages: %v", err)
		}
		if len(tags["page-1"]) != 0 {
			t.Errorf("expected no tags after delete, got %v", tags["page-1"])
		}

		exc, err := store.GetExcerptsForPages(testPageIDs("page-1"))
		if err != nil {
			t.Fatalf("GetExcerptsForPages: %v", err)
		}
		if exc["page-1"] != "" {
			t.Errorf("expected empty excerpt after delete, got %q", exc["page-1"])
		}

	})
})

var _ = ginkgo.Describe("TestTagsStore_DeletePageIndex_NonExistentPageIsNoop", func() {
	ginkgo.It("preserves behavior", func() {
		t := ginkgo.GinkgoT()
		store := newTestStore(t)
		if err := store.DeletePageIndex("does-not-exist"); err != nil {
			t.Fatalf("DeletePageIndex on unknown page: %v", err)
		}

	})
})

// ─── GetExcerptsForPages ─────────────────────────────────────────────────────

var _ = ginkgo.Describe("TestTagsStore_GetExcerptsForPages_ReturnsCorrectExcerpts", func() {
	ginkgo.It("preserves behavior", func() {
		t := ginkgo.GinkgoT()
		store := newTestStore(t)

		_ = store.SetPageIndex("p1", []string{"go"}, "excerpt one")
		_ = store.SetPageIndex("p2", []string{"ts"}, "excerpt two")

		got, err := store.GetExcerptsForPages(testPageIDs("p1", "p2"))
		if err != nil {
			t.Fatalf("GetExcerptsForPages: %v", err)
		}
		if got["p1"] != "excerpt one" {
			t.Errorf("p1 = %q", got["p1"])
		}
		if got["p2"] != "excerpt two" {
			t.Errorf("p2 = %q", got["p2"])
		}

	})
})

var _ = ginkgo.Describe("TestTagsStore_GetExcerptsForPages_UnknownIDReturnsNoEntry", func() {
	ginkgo.It("preserves behavior", func() {
		t := ginkgo.GinkgoT()
		store := newTestStore(t)

		got, err := store.GetExcerptsForPages(testPageIDs("ghost"))
		if err != nil {
			t.Fatalf("GetExcerptsForPages: %v", err)
		}
		if _, ok := got["ghost"]; ok {
			t.Errorf("ghost should not be present")
		}

	})
})

var _ = ginkgo.Describe("TestTagsStore_GetExcerptsForPages_EmptyInputReturnsEmptyMap", func() {
	ginkgo.It("preserves behavior", func() {
		t := ginkgo.GinkgoT()
		store := newTestStore(t)

		got, err := store.GetExcerptsForPages(testPageIDs[string]())
		if err != nil {
			t.Fatalf("GetExcerptsForPages: %v", err)
		}
		if got == nil {
			t.Fatalf("expected non-nil empty map")
		}
		if len(got) != 0 {
			t.Errorf("expected empty map, got %v", got)
		}

	})
})

// ─── Clear (with page_meta) ───────────────────────────────────────────────────

var _ = ginkgo.Describe("TestTagsStore_Clear_AlsoRemovesPageMeta", func() {
	ginkgo.It("preserves behavior", func() {
		t := ginkgo.GinkgoT()
		store := newTestStore(t)

		_ = store.SetPageIndex("page-1", []string{"go"}, "some excerpt")

		if err := store.Clear(); err != nil {
			t.Fatalf("Clear: %v", err)
		}

		exc, err := store.GetExcerptsForPages(testPageIDs("page-1"))
		if err != nil {
			t.Fatalf("GetExcerptsForPages after Clear: %v", err)
		}
		if exc["page-1"] != "" {
			t.Errorf("expected empty excerpt after Clear, got %q", exc["page-1"])
		}

	})
})

// ─── GetAllTags — LIKE wildcard escaping ─────────────────────────────────────

var _ = ginkgo.Describe("TestTagsStore_GetAllTags_FilterEscapesLikeWildcards", func() {
	ginkgo.It("preserves behavior", func() {
		t := ginkgo.GinkgoT()
		store := newTestStore(t)

		// Tags that would accidentally match if % or _ were treated as wildcards.
		_ = store.SetTagsForPage("page-1", []string{"react", "redux"})

		// A filter of "%" would match everything without escaping.
		tags, err := store.GetAllTags("%", 50)
		if err != nil {
			t.Fatalf("GetAllTags with %% filter: %v", err)
		}
		if len(tags) != 0 {
			t.Errorf("filter '%%' should match no tags (literal), got %v", tags)
		}

		// A filter of "_eact" would match "react" without escaping.
		tags, err = store.GetAllTags("_eact", 50)
		if err != nil {
			t.Fatalf("GetAllTags with _ filter: %v", err)
		}
		if len(tags) != 0 {
			t.Errorf("filter '_eact' should match no tags (literal), got %v", tags)
		}

	})
})

// ─── helpers ─────────────────────────────────────────────────────────────────

// assertStringSliceEqual checks that got and want contain the same elements,
// sorting both to allow for any insertion order.
func assertStringSliceEqual(t tagsTestT, got, want []string) {
	t.Helper()

	gc := append([]string(nil), got...)
	wc := append([]string(nil), want...)
	sort.Strings(gc)
	sort.Strings(wc)

	if len(gc) != len(wc) {
		t.Errorf("len = %d, want %d\n got:  %v\n want: %v", len(gc), len(wc), gc, wc)
		return
	}
	for i := range gc {
		if gc[i] != wc[i] {
			t.Errorf("[%d] = %q, want %q\n got:  %v\n want: %v", i, gc[i], wc[i], gc, wc)
			return
		}
	}
}
