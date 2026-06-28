package agenthooks

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestAgenthooksSuite(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Agenthooks Suite")
}
