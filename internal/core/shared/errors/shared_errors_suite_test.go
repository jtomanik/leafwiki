package errors_test

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestSharedErrorsSuite(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Shared Errors Suite")
}
