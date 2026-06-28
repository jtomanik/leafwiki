package frontd

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

type frontdTestTB interface {
	Helper()
	TempDir() string
	Fatal(args ...any)
	Fatalf(format string, args ...any)
	Error(args ...any)
	Errorf(format string, args ...any)
}

func TestFrontdSuite(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Frontd Suite")
}
