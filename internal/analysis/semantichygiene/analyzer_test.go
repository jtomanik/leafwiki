package semantichygiene_test

import (
	ginkgo "github.com/onsi/ginkgo/v2"
	semantichygiene "github.com/perber/wiki/internal/analysis/semantichygiene"
	analysischeck "golang.org/x/tools/go/analysis/analysistest"
)

var _ = ginkgo.DescribeTable("TestAnalyzerFixtures",
	func(pkg string) {
		analysischeck.Run(ginkgo.GinkgoT(), analysischeck.TestData(), semantichygiene.Analyzer, pkg)
	},
	ginkgo.Entry("semanticcases", "github.com/perber/wiki/internal/analysis/semantichygiene/testdata/semanticcases"),
	ginkgo.Entry("identity", "github.com/perber/wiki/internal/core/identity"),
	ginkgo.Entry("agenthooks", "github.com/perber/wiki/internal/agenthooks"),
	ginkgo.Entry("tree", "github.com/perber/wiki/internal/core/tree"),
	ginkgo.Entry("markdown", "github.com/perber/wiki/internal/core/markdown"),
	ginkgo.Entry("fakeadapter", "github.com/perber/wiki/internal/fakeadapter"),
	ginkgo.Entry("test_utils", "github.com/perber/wiki/internal/test_utils"),
	ginkgo.Entry("wiki/mcp", "github.com/perber/wiki/internal/wiki/mcp"),
	ginkgo.Entry("repotests", "github.com/perber/wiki/internal/analysis/semantichygiene/testdata/repotests"),
	ginkgo.Entry("e2e/semanticfixture", "github.com/perber/wiki/e2e/semanticfixture"),
	ginkgo.Entry("e2e-proxy", "github.com/perber/wiki/e2e-proxy"),
)
