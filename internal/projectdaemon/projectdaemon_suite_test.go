package projectdaemon

import (
	"testing"

	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

type projectdaemonTestT interface {
	Helper()
	Fatal(args ...any)
	Fatalf(format string, args ...any)
}

func TestProjectdaemonSuite(t *testing.T) {
	RegisterFailHandler(ginkgo.Fail)
	ginkgo.RunSpecs(t, "Projectdaemon Suite")
}
