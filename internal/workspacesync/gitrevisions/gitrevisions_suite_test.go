package gitrevisions

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestGitRevisionsSuite(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Git Revisions Suite")
}
