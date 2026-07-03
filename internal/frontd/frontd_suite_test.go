package frontd

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestFrontdSuite(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Frontd Suite")
}
