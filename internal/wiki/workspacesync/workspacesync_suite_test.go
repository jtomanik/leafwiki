package workspacesyncapi

import (
	"testing"

	. "github.com/onsi/gomega"
	ginkgo "github.com/onsi/ginkgo/v2"
)

func TestWorkspacesyncSuite(t *testing.T) {
	RegisterFailHandler(ginkgo.Fail)
	ginkgo.RunSpecs(t, "Workspacesync Suite")
}
