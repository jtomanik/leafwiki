package branding

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

type brandingTestTB interface {
	Helper()
	TempDir() string
	Fatalf(format string, args ...any)
}

func TestBrandingSuite(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Branding Suite")
}
