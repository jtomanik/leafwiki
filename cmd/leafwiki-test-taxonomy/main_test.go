package main

import (
	"bytes"
	"os"
	"path/filepath"

	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/gstruct"
)

var _ = ginkgo.Describe("taxonomy inventory report", ginkgo.Label("unit"), func() {
	ginkgo.It("reports unlabeled Go/Ginkgo specs across roots without failing", func() {
		mainRoot := newSourceRoot()
		proxyRoot := newSourceRoot()

		writeSource(mainRoot, "internal/wiki/wiki_test.go", `package wiki

import . "github.com/onsi/ginkgo/v2"

var _ = Describe("workspace ID validation", Label("unit"), func() {
	It("keeps a local contract deterministic", func() {})
})

var _ = Describe("HTTP route behavior", func() {
	It("returns the workspace health response", func() {})
})

var _ = DescribeTable("import rows",
	func(value string) {},
	Entry("reports an unlabeled import row", "value"),
)
`)
		writeSource(proxyRoot, "proxy_auth_test.go", `package proxy

import ginkgo "github.com/onsi/ginkgo/v2"

var _ = ginkgo.Describe("proxy authentication", func() {
	ginkgo.It("rejects unauthenticated requests at the proxy boundary", func() {})
})
`)
		writeSource(mainRoot, "references/ignored_test.go", missingSpecSource())
		writeSource(mainRoot, "internal/wiki/testdata/ignored_test.go", missingSpecSource())
		writeSource(mainRoot, "ui/leafwiki-ui/node_modules/ignored_test.go", missingSpecSource())

		report, err := buildReport(reportOptions{Roots: []string{mainRoot, proxyRoot}})

		Expect(err).To(Succeed())
		Expect(report).To(gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
			"FilesScanned": Equal(2),
			"SpecsScanned": Equal(4),
			"LabeledSpecs": Equal(1),
			"MissingSpecs": ConsistOf(
				gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
					"File":        HaveSuffix("internal/wiki/wiki_test.go"),
					"Description": Equal("returns the workspace health response"),
				}),
				gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
					"File":        HaveSuffix("internal/wiki/wiki_test.go"),
					"Description": Equal("reports an unlabeled import row"),
				}),
				gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
					"File":        HaveSuffix("proxy_auth_test.go"),
					"Description": Equal("rejects unauthenticated requests at the proxy boundary"),
				}),
			),
		}))
	})

	ginkgo.It("prints a zero exit status for missing labels because the report is advisory", func() {
		root := newSourceRoot()
		writeSource(root, "internal/wiki/wiki_test.go", `package wiki

import . "github.com/onsi/ginkgo/v2"

var _ = Describe("unlabeled behavior", func() {
	It("still appears in the advisory report", func() {})
})
`)

		var stdout bytes.Buffer
		var stderr bytes.Buffer

		status := run([]string{root}, &stdout, &stderr)

		Expect(status).To(BeZero())
		Expect(stderr.String()).To(BeEmpty())
		Expect(stdout.String()).NotTo(BeEmpty())
	})

	ginkgo.It("does not double count nested roots when e2e-proxy is scanned explicitly", func() {
		mainRoot := newSourceRoot()
		proxyRoot := filepath.Join(mainRoot, "e2e-proxy")

		writeSource(mainRoot, "internal/wiki/wiki_test.go", `package wiki

import . "github.com/onsi/ginkgo/v2"

var _ = Describe("unlabeled main behavior", func() {
	It("appears once from the main root", func() {})
})
`)
		writeSource(proxyRoot, "proxy_auth_test.go", `package proxy

import . "github.com/onsi/ginkgo/v2"

var _ = Describe("unlabeled proxy behavior", func() {
	It("appears once from the proxy root", func() {})
})
`)

		report, err := buildReport(reportOptions{Roots: []string{mainRoot, proxyRoot}})

		Expect(err).To(Succeed())
		Expect(report).To(gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
			"FilesScanned": Equal(2),
			"SpecsScanned": Equal(2),
			"MissingSpecs": HaveLen(2),
		}))
	})
})

func newSourceRoot() string {
	ginkgo.GinkgoHelper()

	root, err := os.MkdirTemp("", "leafwiki-test-taxonomy-*")
	Expect(err).To(Succeed())
	ginkgo.DeferCleanup(os.RemoveAll, root)
	return root
}

func writeSource(root string, relativePath string, contents string) {
	ginkgo.GinkgoHelper()

	path := filepath.Join(root, relativePath)
	Expect(os.MkdirAll(filepath.Dir(path), 0o755)).To(Succeed())
	Expect(os.WriteFile(path, []byte(contents), 0o644)).To(Succeed())
}

func missingSpecSource() string {
	return `package ignored

import . "github.com/onsi/ginkgo/v2"

var _ = Describe("ignored behavior", func() {
	It("would be missing if this directory were scanned", func() {})
})
`
}
