package tags

import ginkgo "github.com/onsi/ginkgo/v2"

type tagsTestT interface {
	Helper()
	TempDir() string
	Fatalf(format string, args ...any)
	Errorf(format string, args ...any)
	Error(args ...any)
}

func closeTagsStoreForTest(store *TagsStore, t tagsTestT) {
	t.Helper()
	ginkgo.DeferCleanup(func() {
		if err := store.Close(); err != nil {
			t.Fatalf("Close: %v", err)
		}
	})
}
