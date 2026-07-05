package architecturehygiene_test

import (
	"fmt"

	ginkgo "github.com/onsi/ginkgo/v2"
	architecturehygiene "github.com/perber/wiki/internal/analysis/architecturehygiene"
	analysischeck "golang.org/x/tools/go/analysis/analysistest"
)

type analysisFixtureFailures struct{}

func (analysisFixtureFailures) Errorf(format string, args ...any) {
	ginkgo.GinkgoHelper()
	ginkgo.Fail(fmt.Sprintf(format, args...))
}

var _ = ginkgo.DescribeTable("architecture hygiene analyzer fixtures",
	ginkgo.Label("unit"),
	func(pkg string) {
		analysischeck.Run(analysisFixtureFailures{}, analysischeck.TestData(), architecturehygiene.Analyzer, pkg)
	},
	ginkgo.Entry("e2e-proxy", "github.com/perber/wiki/e2e-proxy"),
	ginkgo.Entry("internal core architecture cases", "github.com/perber/wiki/internal/core/archcases"),
	ginkgo.Entry("internal wiki architecture cases", "github.com/perber/wiki/internal/wiki/archcases"),
	ginkgo.Entry("internal projectdaemon architecture cases", "github.com/perber/wiki/internal/projectdaemon/archcases"),
	ginkgo.Entry("internal workspaced architecture cases", "github.com/perber/wiki/internal/workspaced/archcases"),
)
