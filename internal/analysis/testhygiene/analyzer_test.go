package testhygiene_test

import (
	"fmt"

	ginkgo "github.com/onsi/ginkgo/v2"
	testhygiene "github.com/perber/wiki/internal/analysis/testhygiene"
	analysischeck "golang.org/x/tools/go/analysis/analysistest"
)

type analysisFixtureFailures struct{}

func (analysisFixtureFailures) Errorf(format string, args ...any) {
	ginkgo.GinkgoHelper()
	ginkgo.Fail(fmt.Sprintf(format, args...))
}

var _ = ginkgo.DescribeTable("test hygiene analyzer fixtures",
	ginkgo.Label("unit"),
	func(pkg string) {
		analysischeck.Run(analysisFixtureFailures{}, analysischeck.TestData(), testhygiene.Analyzer, pkg)
	},
	ginkgo.Entry("ginkgocases", "github.com/perber/wiki/internal/analysis/testhygiene/testdata/ginkgocases"),
	ginkgo.Entry("gomegacases", "github.com/perber/wiki/internal/analysis/testhygiene/testdata/gomegacases"),
	ginkgo.Entry("taxonomycases", "github.com/perber/wiki/internal/analysis/testhygiene/testdata/taxonomycases"),
	ginkgo.Entry("waivercases", "github.com/perber/wiki/internal/analysis/testhygiene/testdata/waivercases"),
	ginkgo.Entry("waiverbudgetcases", "github.com/perber/wiki/internal/analysis/testhygiene/testdata/waiverbudgetcases"),
)
