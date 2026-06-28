package auth

import . "github.com/onsi/gomega"

type authTestT interface {
	Helper()
	Cleanup(func())
	TempDir() string
	Fatal(args ...interface{})
	Fatalf(format string, args ...interface{})
}

func closeWithErrorCheck(closer func() error) {
	Expect(closer()).To(Succeed())
}
