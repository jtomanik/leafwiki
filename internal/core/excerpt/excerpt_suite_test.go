package excerpt

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestExcerptSuite(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Excerpt Suite")
}
