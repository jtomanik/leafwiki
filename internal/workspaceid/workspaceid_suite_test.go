package workspaceid

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestWorkspaceIDSuite(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "WorkspaceID Suite")
}
