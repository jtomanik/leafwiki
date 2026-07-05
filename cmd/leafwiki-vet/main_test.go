package main

import (
	"testing"

	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/perber/wiki/internal/analysis/architecturehygiene"
	"github.com/perber/wiki/internal/analysis/i18ncatalog"
	"github.com/perber/wiki/internal/analysis/semantichygiene"
	"github.com/perber/wiki/internal/analysis/testhygiene"
	"golang.org/x/tools/go/analysis"
)

func TestLeafwikiVetSuite(t *testing.T) {
	RegisterFailHandler(ginkgo.Fail)
	ginkgo.RunSpecs(t, "LeafWiki Vet Suite")
}

var _ = ginkgo.Describe("leafwiki-vet command", ginkgo.Label("unit"), func() {
	ginkgo.It("delegates to the project checker analyzer family set", func() {
		previous := runCheckers
		ginkgo.DeferCleanup(func() {
			runCheckers = previous
		})

		var analyzers []*analysis.Analyzer
		runCheckers = func(received ...*analysis.Analyzer) {
			analyzers = append(analyzers, received...)
		}

		main()

		Expect(analyzers).To(Equal([]*analysis.Analyzer{
			semantichygiene.Analyzer,
			testhygiene.Analyzer,
			architecturehygiene.Analyzer,
			i18ncatalog.Analyzer,
		}))
	})
})
