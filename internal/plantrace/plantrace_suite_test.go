package plantrace

import (
	"testing"

	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

type plantraceTestT interface {
	Helper()
	Fatal(args ...any)
	Fatalf(format string, args ...any)
}

func TestPlantraceSuite(t *testing.T) {
	RegisterFailHandler(ginkgo.Fail)
	ginkgo.RunSpecs(t, "Plantrace Suite")
}
