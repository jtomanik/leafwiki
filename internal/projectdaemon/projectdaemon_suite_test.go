package projectdaemon

import (
	"os"
	"testing"

	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestProjectdaemonSuite(t *testing.T) {
	RegisterFailHandler(ginkgo.Fail)
	ginkgo.RunSpecs(t, "Projectdaemon Suite")
}

func tempProjectdaemonDir() string {
	ginkgo.GinkgoHelper()

	dir, err := os.MkdirTemp("", "leafwiki-projectdaemon-*")
	Expect(err).To(Succeed())
	ginkgo.DeferCleanup(func() {
		Expect(os.RemoveAll(dir)).To(Succeed())
	})
	return dir
}
