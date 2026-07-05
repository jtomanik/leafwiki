package semantichygiene_test

import (
	"fmt"

	ginkgo "github.com/onsi/ginkgo/v2"
	semantichygiene "github.com/perber/wiki/internal/analysis/semantichygiene"
	analysischeck "golang.org/x/tools/go/analysis/analysistest"
)

type analysisFixtureFailures struct{}

func (analysisFixtureFailures) Errorf(format string, args ...any) {
	ginkgo.GinkgoHelper()
	ginkgo.Fail(fmt.Sprintf(format, args...))
}

var _ = ginkgo.DescribeTable("semantic hygiene analyzer fixtures",
	ginkgo.Label("unit"),
	func(pkg string) {
		analysischeck.Run(analysisFixtureFailures{}, analysischeck.TestData(), semantichygiene.Analyzer, pkg)
	},
	ginkgo.Entry("semanticcases", "github.com/perber/wiki/internal/analysis/semantichygiene/testdata/semanticcases"),
	ginkgo.Entry("identity", "github.com/perber/wiki/internal/core/identity"),
	ginkgo.Entry("agenthooks", "github.com/perber/wiki/internal/agenthooks"),
	ginkgo.Entry("tree", "github.com/perber/wiki/internal/core/tree"),
	ginkgo.Entry("markdown", "github.com/perber/wiki/internal/core/markdown"),
	ginkgo.Entry("fakeadapter", "github.com/perber/wiki/internal/fakeadapter"),
	ginkgo.Entry("test support package fixtures", "github.com/perber/wiki/internal/test_utils"),
	ginkgo.Entry("wiki/mcp", "github.com/perber/wiki/internal/wiki/mcp"),
	ginkgo.Entry("e2e/semanticfixture", "github.com/perber/wiki/e2e/semanticfixture"),
)
