package main

import (
	"testing"

	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/perber/wiki/internal/analysis/semantichygiene"
	"golang.org/x/tools/go/analysis"
)

func TestLeafwikiVetSuite(t *testing.T) {
	RegisterFailHandler(ginkgo.Fail)
	ginkgo.RunSpecs(t, "LeafWiki Vet Suite")
}

var _ = ginkgo.Describe("leafwiki-vet command", func() {
	ginkgo.It("delegates to the semantic hygiene analyzer", func() {
		previous := runSingleChecker
		ginkgo.DeferCleanup(func() {
			runSingleChecker = previous
		})

		var analyzers []*analysis.Analyzer
		runSingleChecker = func(received *analysis.Analyzer) {
			analyzers = append(analyzers, received)
		}

		main()

		Expect(analyzers).To(Equal([]*analysis.Analyzer{semantichygiene.Analyzer}))
	})
})
