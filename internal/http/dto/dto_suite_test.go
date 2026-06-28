package dto

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestDTOSuite(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "HTTP DTO Suite")
}
