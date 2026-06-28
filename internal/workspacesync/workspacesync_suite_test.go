package workspacesync

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestWorkspaceSyncSuite(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Workspace Sync Suite")
}
