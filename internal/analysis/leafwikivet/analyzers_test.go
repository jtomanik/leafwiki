package leafwikivet_test

import (
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/perber/wiki/internal/analysis/architecturehygiene"
	"github.com/perber/wiki/internal/analysis/i18ncatalog"
	"github.com/perber/wiki/internal/analysis/leafwikivet"
	"github.com/perber/wiki/internal/analysis/semantichygiene"
	"github.com/perber/wiki/internal/analysis/testhygiene"
	"golang.org/x/tools/go/analysis"
)

var _ = ginkgo.Describe("LeafWiki analyzer families", ginkgo.Label("unit"), func() {
	ginkgo.It("exposes policy analyzers in gate order", func() {
		Expect(leafwikivet.PolicyAnalyzers()).To(Equal([]*analysis.Analyzer{
			semantichygiene.Analyzer,
			testhygiene.Analyzer,
			architecturehygiene.Analyzer,
		}))
	})

	ginkgo.It("extends policy analyzers with repository-wide project checks", func() {
		Expect(leafwikivet.ProjectAnalyzers()).To(Equal([]*analysis.Analyzer{
			semantichygiene.Analyzer,
			testhygiene.Analyzer,
			architecturehygiene.Analyzer,
			i18ncatalog.Analyzer,
		}))
	})
})
