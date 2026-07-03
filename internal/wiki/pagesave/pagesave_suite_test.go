package pagesave

import (
	"os"
	"testing"

	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestPagesaveSuite(t *testing.T) {
	RegisterFailHandler(ginkgo.Fail)
	ginkgo.RunSpecs(t, "Pagesave Suite")
}

func tempPagesaveDir() string {
	ginkgo.GinkgoHelper()

	dir, err := os.MkdirTemp("", "leafwiki-pagesave-*")
	Expect(err).To(Succeed())
	ginkgo.DeferCleanup(func() {
		Expect(os.RemoveAll(dir)).To(Succeed())
	})
	return dir
}
