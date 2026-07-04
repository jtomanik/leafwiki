package tags

import (
	"os"
	"sort"

	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/types"
	"github.com/perber/wiki/internal/core/tree"
)

func tempTagsDir() string {
	ginkgo.GinkgoHelper()

	dir, err := os.MkdirTemp("", "leafwiki-tags-*")
	Expect(err).NotTo(HaveOccurred())
	ginkgo.DeferCleanup(os.RemoveAll, dir)

	return dir
}

func closeTagsStoreForTest(store *TagsStore) {
	ginkgo.GinkgoHelper()
	ginkgo.DeferCleanup(func() {
		Expect(store.Close()).To(Succeed())
	})
}

func newTestStore() *TagsStore {
	ginkgo.GinkgoHelper()

	store, err := NewTagsStore(tempTagsDir())
	Expect(err).NotTo(HaveOccurred())
	closeTagsStoreForTest(store)

	return store
}

func testPageIDs[T ~string](ids ...T) []tree.PageID {
	ginkgo.GinkgoHelper()

	pageIDs := make([]tree.PageID, 0, len(ids))
	for _, id := range ids {
		pageIDs = append(pageIDs, newFixturePageID(id))
	}
	return pageIDs
}

func matchTagSet(tags ...string) types.GomegaMatcher {
	ginkgo.GinkgoHelper()

	values := make([]any, 0, len(tags))
	for _, tag := range tags {
		values = append(values, tag)
	}
	return ConsistOf(values...)
}

func matchPageIDSet(ids ...tree.PageID) types.GomegaMatcher {
	ginkgo.GinkgoHelper()

	values := make([]any, 0, len(ids))
	for _, id := range ids {
		values = append(values, id)
	}
	return ConsistOf(values...)
}

func matchFixturePageIDSet(ids ...string) types.GomegaMatcher {
	ginkgo.GinkgoHelper()

	pageIDs := make([]tree.PageID, 0, len(ids))
	for _, id := range ids {
		pageIDs = append(pageIDs, tree.PageIDFromString(id))
	}
	return matchPageIDSet(pageIDs...)
}

func matchPageIDsInOrder(ids ...string) types.GomegaMatcher {
	ginkgo.GinkgoHelper()

	pageIDs := make([]tree.PageID, 0, len(ids))
	for _, id := range ids {
		pageIDs = append(pageIDs, tree.PageIDFromString(id))
	}
	return Equal(pageIDs)
}

func matchTagCounts(want map[string]int) types.GomegaMatcher {
	ginkgo.GinkgoHelper()

	return WithTransform(func(counts []TagCount) map[string]int {
		got := make(map[string]int, len(counts))
		for _, count := range counts {
			got[count.Tag] = count.Count
		}
		return got
	}, Equal(want))
}

func sortedPageIDs(ids []tree.PageID) []tree.PageID {
	ginkgo.GinkgoHelper()

	sorted := append([]tree.PageID(nil), ids...)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i] < sorted[j]
	})
	return sorted
}
