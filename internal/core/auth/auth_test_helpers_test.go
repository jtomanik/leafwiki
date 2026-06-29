package auth

import (
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

type authTestT interface {
	Helper()
	Cleanup(func())
	TempDir() string
	Fatal(args ...interface{})
	Fatalf(format string, args ...interface{})
}

func closeWithErrorCheck(closer func() error) {
	ginkgo.GinkgoHelper()
	Expect(closer()).To(Succeed())
}
