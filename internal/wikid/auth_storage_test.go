package wikid

import (
	"os"
	"path/filepath"

	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = ginkgo.Describe("wikid auth storage", func() {
	ginkgo.It("opens fresh stores under the wikid auth root and removes legacy root databases", func() {
		dataDir := wikidTestTempDir()

		stores, err := OpenAuthStores(dataDir)
		Expect(err).To(Succeed())
		ginkgo.DeferCleanup(func() {
			Expect(stores.Close()).To(Succeed())
		})

		paths := AuthStoragePaths(dataDir)
		for _, path := range []string{paths.UsersDB, paths.SessionsDB, paths.APIKeysDB} {
			_, err := os.Stat(path)
			Expect(err).To(Succeed())
		}
		for _, name := range []string{"users.db", "sessions.db", "api_keys.db"} {
			_, err := os.Stat(filepath.Join(dataDir, name))
			Expect(err).To(MatchError(os.ErrNotExist))
		}
	})

	ginkgo.It("uses the global wikid directory when the data root is the LeafWiki home", func() {
		globalRoot := filepath.Join(wikidTestTempDir(), ".leafwiki")

		paths := AuthStoragePaths(globalRoot)

		Expect(paths).To(SatisfyAll(
			HaveField("AuthDir", Equal(filepath.Join(globalRoot, "wikid", "auth"))),
			HaveField("OAuthDir", Equal(filepath.Join(globalRoot, "wikid", "oauth"))),
		))
	})

	ginkgo.It("deletes only legacy auth databases from the data root", func() {
		dataDir := wikidTestTempDir()
		for _, name := range []string{"users.db", "sessions.db", "api_keys.db", "pages.db"} {
			Expect(os.WriteFile(filepath.Join(dataDir, name), []byte(name), 0o600)).To(Succeed())
		}
		paths := AuthStoragePaths(dataDir)
		Expect(os.MkdirAll(paths.AuthDir, 0o755)).To(Succeed())
		Expect(os.WriteFile(paths.UsersDB, []byte("new users"), 0o600)).To(Succeed())

		Expect(CleanupLegacyAuthDBs(dataDir)).To(Succeed())

		for _, name := range []string{"users.db", "sessions.db", "api_keys.db"} {
			_, err := os.Stat(filepath.Join(dataDir, name))
			Expect(err).To(MatchError(os.ErrNotExist))
		}
		for _, path := range []string{filepath.Join(dataDir, "pages.db"), paths.UsersDB} {
			_, err := os.Stat(path)
			Expect(err).To(Succeed())
		}
	})
})
