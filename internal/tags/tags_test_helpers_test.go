package tags

type tagsTestT interface {
	Helper()
	TempDir() string
	Cleanup(func())
	Fatalf(format string, args ...any)
	Errorf(format string, args ...any)
	Error(args ...any)
}

func closeTagsStoreForTest(store *TagsStore, t tagsTestT) {
	t.Helper()
	if err := store.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
}
