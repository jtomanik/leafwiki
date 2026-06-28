package wikid

import (
	ginkgo "github.com/onsi/ginkgo/v2"
	"os"
	"path/filepath"
)

var _ = ginkgo.It("TestAuthStorageOpensFreshStoresUnderWikidAuthRoot", func() {
	t := ginkgo.GinkgoT()
	dataDir := t.TempDir()

	stores, err := OpenAuthStores(dataDir)
	if err != nil {
		t.Fatalf("OpenAuthStores failed: %v", err)
	}
	defer stores.Close()

	paths := AuthStoragePaths(dataDir)
	for _, path := range []string{paths.UsersDB, paths.SessionsDB, paths.APIKeysDB} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("expected wikid auth DB %s to exist: %v", path, err)
		}
	}
	for _, name := range []string{"users.db", "sessions.db", "api_keys.db"} {
		if _, err := os.Stat(filepath.Join(dataDir, name)); !os.IsNotExist(err) {
			t.Fatalf("legacy auth DB %s stat err = %v, want not exist", name, err)
		}
	}
})

var _ = ginkgo.It("TestAuthStoragePathsUseGlobalWikidDirWhenDataRootIsLeafwiki", func() {
	t := ginkgo.GinkgoT()
	globalRoot := filepath.Join(t.TempDir(), ".leafwiki")

	paths := AuthStoragePaths(globalRoot)

	if got, want := paths.AuthDir, filepath.Join(globalRoot, "wikid", "auth"); got != want {
		t.Fatalf("AuthDir = %q, want %q", got, want)
	}
	if got, want := paths.OAuthDir, filepath.Join(globalRoot, "wikid", "oauth"); got != want {
		t.Fatalf("OAuthDir = %q, want %q", got, want)
	}
})

var _ = ginkgo.It("TestCleanupLegacyAuthDBsDeletesOnlyKnownRootFiles", func() {
	t := ginkgo.GinkgoT()
	dataDir := t.TempDir()
	for _, name := range []string{"users.db", "sessions.db", "api_keys.db", "pages.db"} {
		if err := os.WriteFile(filepath.Join(dataDir, name), []byte(name), 0o600); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
	paths := AuthStoragePaths(dataDir)
	if err := os.MkdirAll(paths.AuthDir, 0o755); err != nil {
		t.Fatalf("create wikid auth root: %v", err)
	}
	if err := os.WriteFile(paths.UsersDB, []byte("new users"), 0o600); err != nil {
		t.Fatalf("write new users db: %v", err)
	}

	if err := CleanupLegacyAuthDBs(dataDir); err != nil {
		t.Fatalf("CleanupLegacyAuthDBs failed: %v", err)
	}

	for _, name := range []string{"users.db", "sessions.db", "api_keys.db"} {
		if _, err := os.Stat(filepath.Join(dataDir, name)); !os.IsNotExist(err) {
			t.Fatalf("legacy auth DB %s stat err = %v, want not exist", name, err)
		}
	}
	for _, path := range []string{filepath.Join(dataDir, "pages.db"), paths.UsersDB} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("expected unrelated/new file %s to remain: %v", path, err)
		}
	}
})
