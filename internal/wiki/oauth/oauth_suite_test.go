package oauth

import (
	"testing"

	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

type oauthTestT interface {
	Helper()
	Fatal(args ...any)
	Fatalf(format string, args ...any)
}

func TestOAuthSuite(t *testing.T) {
	RegisterFailHandler(ginkgo.Fail)
	ginkgo.RunSpecs(t, "OAuth Suite")
}
