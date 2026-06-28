package locking

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestLockingSuite(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Locking Suite")
}
