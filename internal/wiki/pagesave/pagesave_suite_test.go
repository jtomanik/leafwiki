package pagesave

import (
	"testing"

	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

type pagesaveTestT interface {
	Helper()
	TempDir() string
	Fatal(args ...any)
	Fatalf(format string, args ...any)
	Error(args ...any)
	Errorf(format string, args ...any)
}

func TestPagesaveSuite(t *testing.T) {
	RegisterFailHandler(ginkgo.Fail)
	ginkgo.RunSpecs(t, "Pagesave Suite")
}
