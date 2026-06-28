package links

type linksTestT interface {
	Helper()
	TempDir() string
	Cleanup(func())
	Fatalf(format string, args ...any)
	Errorf(format string, args ...any)
}

func closeLinksStoreForTest(store *LinksStore, t linksTestT) {
	t.Helper()
	if err := store.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
}
