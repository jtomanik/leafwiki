package architecturehygiene_test

import (
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	architecturehygiene "github.com/perber/wiki/internal/analysis/architecturehygiene"
)

var _ = ginkgo.DescribeTable("dependency direction defensive import handling",
	ginkgo.Label("unit"),
	func(importerPath string, importLiteral *string) {
		Expect(architecturehygiene.DependencyDirectionDiagnosticsForTest(importerPath, importLiteral)).To(BeEmpty())
	},
	ginkgo.Entry("ignores imports before package identity is available", "", ptrToImportLiteral("\"github.com/perber/wiki/internal/core\"")),
	ginkgo.Entry("ignores malformed import literals", "github.com/perber/wiki/e2e-proxy", ptrToImportLiteral("\"github.com/perber/wiki/internal/core")),
	ginkgo.Entry("ignores import specs without an import path", "github.com/perber/wiki/e2e-proxy", nil),
)

func ptrToImportLiteral(value string) *string {
	return &value
}
