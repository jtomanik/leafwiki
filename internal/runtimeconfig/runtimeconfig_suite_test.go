package runtimeconfig

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestRuntimeconfigSuite(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Runtimeconfig Suite")
}
