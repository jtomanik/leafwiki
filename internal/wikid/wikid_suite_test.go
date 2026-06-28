package wikid

import (
	"testing"

	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestWikidSuite(t *testing.T) {
	if runWikidStoreHelperProcessForTest() {
		return
	}

	RegisterFailHandler(ginkgo.Fail)
	ginkgo.RunSpecs(t, "Wikid Suite")
}
