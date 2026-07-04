package properties

import (
	"os"
	"path/filepath"
	"sort"

	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/types"
	"github.com/perber/wiki/internal/core/tree"
)

func propertiesTempDir() string {
	ginkgo.GinkgoHelper()

	dir, err := os.MkdirTemp("", "leafwiki-properties-*")
	Expect(err).To(Succeed())
	ginkgo.DeferCleanup(os.RemoveAll, dir)
	return dir
}

func closeStoreForTest(store *PropertiesStore) {
	ginkgo.GinkgoHelper()
	ginkgo.DeferCleanup(func() {
		Expect(store.Close()).To(Succeed())
	})
}

func newTestStore() *PropertiesStore {
	ginkgo.GinkgoHelper()

	store, err := NewPropertiesStore(propertiesTempDir())
	Expect(err).To(Succeed())
	closeStoreForTest(store)
	return store
}

func testPageIDs(ids ...string) []tree.PageID {
	pageIDs := make([]tree.PageID, 0, len(ids))
	for _, id := range ids {
		pageIDs = append(pageIDs, tree.PageIDFromString(id))
	}
	return pageIDs
}

func sortTestPageIDs(ids []tree.PageID) {
	sort.Slice(ids, func(i, j int) bool {
		return ids[i] < ids[j]
	})
}

func props(kv ...string) map[string]PropertyEntry {
	m := make(map[string]PropertyEntry, len(kv)/2)
	for i := 0; i+1 < len(kv); i += 2 {
		m[kv[i]] = PropertyEntry{Value: kv[i+1], Type: "text"}
	}
	return m
}

func matchExtractedProperties(kv ...string) types.GomegaMatcher {
	return Equal(props(kv...))
}

func havePropertyKeys(keys ...PropertyKeyCount) types.GomegaMatcher {
	return Equal(keys)
}

var _ = ginkgo.Describe("properties store", func() {
	ginkgo.When("the store is initialized", func() {
		ginkgo.It("creates the SQLite database in the storage directory", func() {
			storageDir := propertiesTempDir()

			store, err := NewPropertiesStore(storageDir)

			Expect(err).To(Succeed())
			closeStoreForTest(store)
			_, err = os.Stat(filepath.Join(storageDir, "properties.db"))
			Expect(err).To(Succeed())
		})

		ginkgo.It("can initialize the schema repeatedly in the same directory", func() {
			storageDir := propertiesTempDir()

			for range 3 {
				store, err := NewPropertiesStore(storageDir)
				Expect(err).To(Succeed())
				Expect(store.Close()).To(Succeed())
			}
		})
	})

	ginkgo.When("page properties are replaced", func() {
		ginkgo.It("stores all text entries for a page", func() {
			store := newTestStore()

			Expect(store.SetPropertiesForPage(newFixturePageID("page-1"), props("status", "draft", "author", "alice", "environment", "staging"))).To(Succeed())

			got, err := store.GetPropertiesForPages(testPageIDs("page-1"))
			Expect(err).To(Succeed())
			Expect(got).To(HaveKeyWithValue(newFixturePageID("page-1"), props(
				"status", "draft",
				"author", "alice",
				"environment", "staging",
			)))
		})

		ginkgo.It("removes entries that are not present in the second write", func() {
			store := newTestStore()

			Expect(store.SetPropertiesForPage(newFixturePageID("page-1"), props("status", "draft", "author", "alice"))).To(Succeed())
			Expect(store.SetPropertiesForPage(newFixturePageID("page-1"), props("status", "published"))).To(Succeed())

			got, err := store.GetPropertiesForPages(testPageIDs("page-1"))
			Expect(err).To(Succeed())
			Expect(got).To(HaveKeyWithValue(newFixturePageID("page-1"), props("status", "published")))
		})

		ginkgo.It("clears existing entries for empty and nil property sets", func() {
			store := newTestStore()

			Expect(store.SetPropertiesForPage(newFixturePageID("empty-page"), props("status", "draft"))).To(Succeed())
			Expect(store.SetPropertiesForPage(newFixturePageID("empty-page"), map[string]PropertyEntry{})).To(Succeed())
			Expect(store.SetPropertiesForPage(newFixturePageID("nil-page"), props("status", "draft"))).To(Succeed())
			Expect(store.SetPropertiesForPage(newFixturePageID("nil-page"), nil)).To(Succeed())

			got, err := store.GetPropertiesForPages(testPageIDs("empty-page", "nil-page"))
			Expect(err).To(Succeed())
			Expect(got).To(BeEmpty())
		})
	})

	ginkgo.When("page properties are deleted", func() {
		ginkgo.It("removes entries for the selected page", func() {
			store := newTestStore()

			Expect(store.SetPropertiesForPage(newFixturePageID("page-1"), props("status", "draft"))).To(Succeed())
			Expect(store.DeletePropertiesForPage(newFixturePageID("page-1"))).To(Succeed())

			got, err := store.GetPropertiesForPages(testPageIDs("page-1"))
			Expect(err).To(Succeed())
			Expect(got).To(BeEmpty())
		})

		ginkgo.It("treats unknown pages as a no-op", func() {
			store := newTestStore()

			Expect(store.DeletePropertiesForPage(newFixturePageID("does-not-exist"))).To(Succeed())
		})

		ginkgo.It("leaves other page properties intact", func() {
			store := newTestStore()

			Expect(store.SetPropertiesForPage(newFixturePageID("page-1"), props("status", "draft"))).To(Succeed())
			Expect(store.SetPropertiesForPage(newFixturePageID("page-2"), props("status", "published"))).To(Succeed())
			Expect(store.DeletePropertiesForPage(newFixturePageID("page-1"))).To(Succeed())

			got, err := store.GetPropertiesForPages(testPageIDs("page-2"))
			Expect(err).To(Succeed())
			Expect(got).To(HaveKeyWithValue(newFixturePageID("page-2"), props("status", "published")))
		})
	})

	ginkgo.When("property keys are listed", func() {
		ginkgo.It("returns no keys for an empty database", func() {
			store := newTestStore()

			keys, err := store.GetAllPropertyKeys("", 50)

			Expect(err).To(Succeed())
			Expect(keys).To(BeEmpty())
		})

		ginkgo.It("returns distinct keys with page counts", func() {
			store := newTestStore()

			Expect(store.SetPropertiesForPage(newFixturePageID("page-1"), props("status", "draft", "priority", "high"))).To(Succeed())
			Expect(store.SetPropertiesForPage(newFixturePageID("page-2"), props("status", "published"))).To(Succeed())
			Expect(store.SetPropertiesForPage(newFixturePageID("page-3"), props("status", "draft", "author", "alice"))).To(Succeed())

			keys, err := store.GetAllPropertyKeys("", 50)

			Expect(err).To(Succeed())
			Expect(keys).To(ConsistOf(
				PropertyKeyCount{Key: "status", Count: 3},
				PropertyKeyCount{Key: "priority", Count: 1},
				PropertyKeyCount{Key: "author", Count: 1},
			))
		})

		ginkgo.It("orders keys by page count descending and then key ascending", func() {
			store := newTestStore()

			Expect(store.SetPropertiesForPage(newFixturePageID("page-1"), props("alpha", "x", "beta", "x", "gamma", "x"))).To(Succeed())
			Expect(store.SetPropertiesForPage(newFixturePageID("page-2"), props("alpha", "x", "beta", "x"))).To(Succeed())
			Expect(store.SetPropertiesForPage(newFixturePageID("page-3"), props("alpha", "x"))).To(Succeed())

			keys, err := store.GetAllPropertyKeys("", 50)

			Expect(err).To(Succeed())
			Expect(keys).To(havePropertyKeys(
				PropertyKeyCount{Key: "alpha", Count: 3},
				PropertyKeyCount{Key: "beta", Count: 2},
				PropertyKeyCount{Key: "gamma", Count: 1},
			))
		})

		ginkgo.It("filters keys by literal prefix", func() {
			store := newTestStore()

			Expect(store.SetPropertiesForPage(newFixturePageID("page-1"), props("status", "x", "stage", "x", "score", "x", "author", "x"))).To(Succeed())

			keys, err := store.GetAllPropertyKeys("st", 50)

			Expect(err).To(Succeed())
			Expect(keys).To(havePropertyKeys(
				PropertyKeyCount{Key: "stage", Count: 1},
				PropertyKeyCount{Key: "status", Count: 1},
			))
		})

		ginkgo.It("respects a positive result limit", func() {
			store := newTestStore()

			Expect(store.SetPropertiesForPage(newFixturePageID("page-1"), props("a", "1", "b", "2", "c", "3", "d", "4", "e", "5"))).To(Succeed())

			keys, err := store.GetAllPropertyKeys("", 3)

			Expect(err).To(Succeed())
			Expect(keys).To(HaveLen(3))
		})

		ginkgo.It("treats zero limit as unbounded", func() {
			store := newTestStore()

			Expect(store.SetPropertiesForPage(newFixturePageID("page-1"), props("a", "1", "b", "2", "c", "3"))).To(Succeed())

			keys, err := store.GetAllPropertyKeys("", 0)

			Expect(err).To(Succeed())
			Expect(keys).To(HaveLen(3))
		})

		ginkgo.It("treats SQL wildcard characters as literal filter text", func() {
			store := newTestStore()

			Expect(store.SetPropertiesForPage(newFixturePageID("page-1"), props("status", "draft", "stage", "alpha"))).To(Succeed())

			percentKeys, err := store.GetAllPropertyKeys("%", 50)
			Expect(err).To(Succeed())
			Expect(percentKeys).To(BeEmpty())

			underscoreKeys, err := store.GetAllPropertyKeys("_tatus", 50)
			Expect(err).To(Succeed())
			Expect(underscoreKeys).To(BeEmpty())
		})
	})

	ginkgo.When("pages are queried by property value", func() {
		ginkgo.It("returns the pages with an exact key and value match", func() {
			store := newTestStore()

			Expect(store.SetPropertiesForPage(newFixturePageID("page-1"), props("status", "draft"))).To(Succeed())
			Expect(store.SetPropertiesForPage(newFixturePageID("page-2"), props("status", "published"))).To(Succeed())
			Expect(store.SetPropertiesForPage(newFixturePageID("page-3"), props("status", "draft"))).To(Succeed())

			ids, err := store.GetPageIDsByProperty("status", "draft")
			sortTestPageIDs(ids)

			Expect(err).To(Succeed())
			Expect(ids).To(Equal(testPageIDs("page-1", "page-3")))
		})

		ginkgo.It("returns no pages for unmatched values or unknown keys", func() {
			store := newTestStore()

			Expect(store.SetPropertiesForPage(newFixturePageID("page-1"), props("status", "draft"))).To(Succeed())

			publishedIDs, err := store.GetPageIDsByProperty("status", "published")
			Expect(err).To(Succeed())
			Expect(publishedIDs).To(BeEmpty())

			unknownKeyIDs, err := store.GetPageIDsByProperty("nonexistent", "draft")
			Expect(err).To(Succeed())
			Expect(unknownKeyIDs).To(BeEmpty())
		})

		ginkgo.It("matches values case-sensitively", func() {
			store := newTestStore()

			Expect(store.SetPropertiesForPage(newFixturePageID("page-1"), props("status", "Draft"))).To(Succeed())

			ids, err := store.GetPageIDsByProperty("status", "draft")

			Expect(err).To(Succeed())
			Expect(ids).To(BeEmpty())
		})
	})

	ginkgo.When("properties are loaded for selected pages", func() {
		ginkgo.It("returns only the requested pages", func() {
			store := newTestStore()

			Expect(store.SetPropertiesForPage(newFixturePageID("page-1"), props("status", "draft", "score", "10"))).To(Succeed())
			Expect(store.SetPropertiesForPage(newFixturePageID("page-2"), props("author", "alice"))).To(Succeed())
			Expect(store.SetPropertiesForPage(newFixturePageID("page-3"), props("status", "published"))).To(Succeed())

			got, err := store.GetPropertiesForPages(testPageIDs("page-1", "page-3"))

			Expect(err).To(Succeed())
			Expect(got).To(Equal(map[tree.PageID]map[string]PropertyEntry{
				newFixturePageID("page-1"): props("status", "draft", "score", "10"),
				newFixturePageID("page-3"): props("status", "published"),
			}))
		})

		ginkgo.It("returns an empty non-nil map for empty and unknown selections", func() {
			store := newTestStore()

			emptySelection, err := store.GetPropertiesForPages(testPageIDs())
			Expect(err).To(Succeed())
			Expect(emptySelection).To(SatisfyAll(Not(BeNil()), BeEmpty()))

			unknownSelection, err := store.GetPropertiesForPages(testPageIDs("does-not-exist"))
			Expect(err).To(Succeed())
			Expect(unknownSelection).To(BeEmpty())
		})
	})

	ginkgo.When("the store is cleared", func() {
		ginkgo.It("removes all property entries", func() {
			store := newTestStore()

			Expect(store.SetPropertiesForPage(newFixturePageID("page-1"), props("status", "draft"))).To(Succeed())
			Expect(store.SetPropertiesForPage(newFixturePageID("page-2"), props("author", "alice"))).To(Succeed())

			Expect(store.Clear()).To(Succeed())

			keys, err := store.GetAllPropertyKeys("", 50)
			Expect(err).To(Succeed())
			Expect(keys).To(BeEmpty())
		})
	})
})
