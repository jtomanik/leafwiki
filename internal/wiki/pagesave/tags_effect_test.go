package pagesave

import (
	ginkgo "github.com/onsi/ginkgo/v2"

	"github.com/perber/wiki/internal/core/tree"
	"github.com/perber/wiki/internal/tags"
)

func setupTagsEffectTest(t pagesaveTestT) (*tree.TreeService, *tags.TagsService, *TagsSideEffect) {
	t.Helper()
	dir := t.TempDir()

	treeSvc := tree.NewTreeService(dir)
	if err := treeSvc.LoadTree(); err != nil {
		t.Fatalf("LoadTree: %v", err)
	}

	store, err := tags.NewTagsStore(dir)
	if err != nil {
		t.Fatalf("NewTagsStore: %v", err)
	}
	t.Cleanup(func() {
		if err := store.Close(); err != nil {
			t.Fatalf("TagsStore.Close: %v", err)
		}
	})

	svc := tags.NewTagsService(store)
	effect := NewTagsSideEffect(svc, nil)
	return treeSvc, svc, effect
}

// createPageWithFrontmatter creates a page whose frontmatter is set via the import path,
// so custom keys (tags, properties) survive the write.
func createPageWithFrontmatter(t pagesaveTestT, treeSvc *tree.TreeService, title, slug, raw string) *tree.Page {
	t.Helper()
	kind := tree.NodeKindPage
	id, err := treeSvc.CreateNode("system", nil, title, newFixtureSlug(slug), &kind)
	if err != nil {
		t.Fatalf("CreateNode(%q): %v", title, err)
	}
	if err := treeSvc.UpdateNodeUncheckedVersion(newFixtureUserID("system"), *id, title, newFixtureSlug(slug), &raw, true); err != nil {
		t.Fatalf("UpdateNode(%q): %v", title, err)
	}
	page, err := treeSvc.GetPage(*id)
	if err != nil {
		t.Fatalf("GetPage(%q): %v", title, err)
	}
	return page
}

// ─── TagsSideEffect ───────────────────────────────────────────────────────────

var _ = ginkgo.It("TestTagsSideEffect_Apply_Create_IndexesTagsFromRawContent", func() {
	t := ginkgo.GinkgoT()
	treeSvc, tagsSvc, effect := setupTagsEffectTest(t)

	raw := "---\ntags:\n  - golang\n  - testing\n---\n\nPage body."
	page := createPageWithFrontmatter(t, treeSvc, "Tagged Page", "tagged", raw)

	effect.Apply(PageSaveEvent{
		Operation: PageOperationCreate,
		After:     page,
	})

	ids, err := tagsSvc.GetPageIDsByTags([]string{"golang"})
	if err != nil {
		t.Fatalf("GetPageIDsByTags: %v", err)
	}
	if len(ids) != 1 || ids[0] != page.ID {
		t.Errorf("expected page %q to be indexed under 'golang', got %v", page.ID, ids)
	}

	ids2, err := tagsSvc.GetPageIDsByTags([]string{"testing"})
	if err != nil {
		t.Fatalf("GetPageIDsByTags: %v", err)
	}
	if len(ids2) != 1 || ids2[0] != page.ID {
		t.Errorf("expected page %q to be indexed under 'testing', got %v", page.ID, ids2)
	}

})

var _ = ginkgo.It("TestTagsSideEffect_Apply_Update_ReindexesTags", func() {
	t := ginkgo.GinkgoT()
	treeSvc, tagsSvc, effect := setupTagsEffectTest(t)

	raw := "---\ntags:\n  - oldtag\n---\n\nOriginal."
	page := createPageWithFrontmatter(t, treeSvc, "Update Tags", "update-tags", raw)

	effect.Apply(PageSaveEvent{Operation: PageOperationCreate, After: page})

	newRaw := "---\ntags:\n  - newtag\n---\n\nUpdated."
	if err := treeSvc.UpdateNodeUncheckedVersion(newFixtureUserID("system"), newFixturePageID(page.ID), "Update Tags", newFixtureSlug("update-tags"), &newRaw, true); err != nil {
		t.Fatalf("UpdateNode: %v", err)
	}
	updated, err := treeSvc.GetPage(newFixturePageID(page.ID))
	if err != nil {
		t.Fatalf("GetPage after update: %v", err)
	}

	effect.Apply(PageSaveEvent{Operation: PageOperationUpdate, After: updated})

	old, err := tagsSvc.GetPageIDsByTags([]string{"oldtag"})
	if err != nil {
		t.Fatalf("GetPageIDsByTags (old): %v", err)
	}
	if len(old) != 0 {
		t.Errorf("expected oldtag to be gone, got %v", old)
	}

	fresh, err := tagsSvc.GetPageIDsByTags([]string{"newtag"})
	if err != nil {
		t.Fatalf("GetPageIDsByTags (new): %v", err)
	}
	if len(fresh) != 1 || fresh[0] != updated.ID {
		t.Errorf("expected newtag to be indexed, got %v", fresh)
	}

})

var _ = ginkgo.It("TestTagsSideEffect_Apply_Delete_RemovesTags", func() {
	t := ginkgo.GinkgoT()
	treeSvc, tagsSvc, effect := setupTagsEffectTest(t)

	raw := "---\ntags:\n  - removeme\n---\n\nBody."
	page := createPageWithFrontmatter(t, treeSvc, "Delete Tags", "delete-tags", raw)

	effect.Apply(PageSaveEvent{Operation: PageOperationCreate, After: page})

	effect.Apply(PageSaveEvent{
		Operation:     PageOperationDelete,
		AffectedPages: []*tree.Page{page},
	})

	ids, err := tagsSvc.GetPageIDsByTags([]string{"removeme"})
	if err != nil {
		t.Fatalf("GetPageIDsByTags: %v", err)
	}
	if len(ids) != 0 {
		t.Errorf("expected tag to be removed after delete, got %v", ids)
	}

})
