package semantichygiene_test

import (
	"testing"

	"github.com/perber/wiki/internal/analysis/semantichygiene"
	"golang.org/x/tools/go/analysis/analysistest"
)

func TestAnalyzerFixtures(t *testing.T) {
	testdata := analysistest.TestData()
	analysistest.Run(t, testdata, semantichygiene.Analyzer,
		"github.com/perber/wiki/internal/analysis/semantichygiene/testdata/semanticcases",
		"github.com/perber/wiki/internal/core/identity",
		"github.com/perber/wiki/internal/core/tree",
		"github.com/perber/wiki/internal/core/markdown",
		"github.com/perber/wiki/internal/fakeadapter",
		"github.com/perber/wiki/internal/test_utils",
		"github.com/perber/wiki/internal/wiki/mcp",
		"github.com/perber/wiki/internal/analysis/semantichygiene/testdata/repotests",
		"github.com/perber/wiki/e2e/semanticfixture",
	)
}
