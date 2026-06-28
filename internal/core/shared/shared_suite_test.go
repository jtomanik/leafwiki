package shared

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestSharedSuite(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Shared Suite")
}
