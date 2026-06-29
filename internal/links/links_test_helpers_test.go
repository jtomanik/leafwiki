package links

import ginkgo "github.com/onsi/ginkgo/v2"

type linksTestT interface {
	Helper()
	TempDir() string
	Fatalf(format string, args ...any)
	Errorf(format string, args ...any)
}

func closeLinksStoreForTest(store *LinksStore, t linksTestT) {
	t.Helper()
	ginkgo.DeferCleanup(func() {
		if err := store.Close(); err != nil {
			t.Fatalf("Close: %v", err)
		}
	})
}
