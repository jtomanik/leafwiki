package links

import (
	"os"

	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func linksTempDir() string {
	ginkgo.GinkgoHelper()
	dir, err := os.MkdirTemp("", "leafwiki-links-test-*")
	Expect(err).NotTo(HaveOccurred())
	ginkgo.DeferCleanup(os.RemoveAll, dir)
	return dir
}

func closeLinksStoreForTest(store *LinksStore) {
	ginkgo.GinkgoHelper()
	ginkgo.DeferCleanup(func() {
		Expect(store.Close()).To(Succeed())
	})
}
