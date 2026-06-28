package main

import (
	"testing"

	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestGrantWorkspacesSuite(t *testing.T) {
	RegisterFailHandler(ginkgo.Fail)
	ginkgo.RunSpecs(t, "grant-workspaces")
}

type grantWorkspaceErrorReader struct {
	err error
}

func (r grantWorkspaceErrorReader) Read([]byte) (int, error) {
	return 0, r.err
}
