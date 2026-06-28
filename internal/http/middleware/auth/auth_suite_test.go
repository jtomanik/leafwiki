package auth

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestAuthMiddlewareSuite(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Auth Middleware Suite")
}
