package leafwiki_test

import (
	"errors"
	"testing"

	"github.com/golangci/plugin-module-register/register"
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/perber/wiki/internal/analysis/semantichygiene"
	leafwiki "github.com/perber/wiki/tools/golangci/leafwiki"
	"golang.org/x/tools/go/analysis"
)

func TestLeafWikiGolangciPluginSuite(t *testing.T) {
	RegisterFailHandler(ginkgo.Fail)
	ginkgo.RunSpecs(t, "LeafWiki Golangci Plugin Suite")
}

var _ = ginkgo.Describe("LeafWiki golangci-lint plugin", ginkgo.Label("unit"), func() {
	ginkgo.It("exposes the semantic hygiene analyzer with type information", func() {
		plugin, err := leafwiki.New(map[string]any{})
		Expect(err).NotTo(HaveOccurred())

		analyzers, err := plugin.BuildAnalyzers()
		Expect(err).NotTo(HaveOccurred())

		Expect(analyzers).To(Equal([]*analysis.Analyzer{semantichygiene.Analyzer}))
		Expect(plugin.GetLoadMode()).To(Equal(register.LoadModeTypesInfo))
	})

	ginkgo.It("registers the LeafWiki plugin constructor for golangci-lint", func() {
		constructor, err := register.GetPlugin("leafwiki")
		Expect(err).NotTo(HaveOccurred())

		plugin, err := constructor(nil)
		Expect(err).NotTo(HaveOccurred())

		analyzers, err := plugin.BuildAnalyzers()
		Expect(err).NotTo(HaveOccurred())
		Expect(analyzers).To(Equal([]*analysis.Analyzer{semantichygiene.Analyzer}))
	})

	ginkgo.It("rejects unknown settings instead of silently ignoring them", func() {
		_, err := leafwiki.New(map[string]any{
			"unknown": "value",
		})

		Expect(err).To(Satisfy(func(actual error) bool {
			return errors.Is(actual, leafwiki.ErrInvalidSettings)
		}))
	})
})
