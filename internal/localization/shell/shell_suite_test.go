package shell

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestShellSuite(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Localization Shell Suite")
}
