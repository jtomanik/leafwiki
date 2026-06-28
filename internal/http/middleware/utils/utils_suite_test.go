package utils

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestMiddlewareUtils(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Middleware Utils Suite")
}
